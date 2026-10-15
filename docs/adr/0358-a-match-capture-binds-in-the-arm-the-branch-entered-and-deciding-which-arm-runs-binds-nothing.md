# ADR 0358 — A `match` capture binds in the arm the branch entered; deciding which arm runs binds nothing

Status: accepted
Date: 2026-10-17
Depends on: [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md), [ADR 0228](0228-an-unwritten-slot-raises-it-does-not-answer.md), [ADR 0235](0235-a-class-pattern-is-one-question-about-a-class-and-the-instance.md), [ADR 0212](0212-built-in-traps-are-typed-raises.md), [ADR 0217](0217-a-compound-statement-is-not-a-scope.md), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md), [ADR 0351](0351-a-module-name-has-one-slot-whichever-statement-binds-it-and-a-parameters-name-is-not-a-module-binding.md)
Roadmap rows: Gap R.68 (paid), Gap R.36 (its `match` row), Gap B (the capture-binding edge it left), Gap R.306 (paid), Gap R.301, Gap R.302, Gap R.303, Gap R.304, Gap R.305, Gap R.307

## The gap, as measured

A `match` has two acts that the compiled backend had fused into one: **deciding** which arm runs, and
**binding** the names the arm's patterns capture. `matchPattern` is called to answer the first question — every
arm's pattern is lowered, in order, before any arm's body — and it was also performing the second. A store that
runs on the way to a decision runs for the arms that lost as much as for the one that won.

Six programs, measured against the binary at HEAD (`c427648`) before anything was touched, beside CPython:

| program | HEAD | CPython |
|---|---|---|
| `v = 5` / `case [y]: pass` / `case 5: pass` / `print(y)` | `0` at **exit 0** | `NameError: name 'y' is not defined` |
| `v = {}` / `case {"k": got}: pass` / `case _: pass` / `print(got)` | `0` at **exit 0** | `NameError: name 'got' is not defined` |
| `match 5: case Point(x): pass` / `case 5: pass` / `print(x)` | `0` at **exit 0** | `NameError: name 'x' is not defined` |
| `def go(v): case [a]: pass` / `case 5: pass` / `return a`, called with `5` | `0` at **exit 0** | `UnboundLocalError: cannot access local variable 'a' …` |
| `case [a] \| {"k": a}:` over `[3]`, `{"k": 8}` | `0`, `8` at exit 0 | `3`, `8` |
| `d = {"k": 1}` / `case [a]: print("list", a)` / `case _: print("no")` | `list 0` at exit 0 | `no` |

Every row is the same class of failure: a program the reference stops with a named exception exits 0 here having
printed a word from memory. The class the handler matches on, the exit code a script branches on and the answer
itself are all wrong, and nothing is printed to say so.

The written-flag machinery of ADR 0228 was present, correct and unmodified throughout. The flag was simply being
set by an act that is not a binding.

## Three causes, not one

Reading `matchPattern` and `MatchStmt`'s lowering side by side with `boundflags.go` and `semantic.go` found three
separate defects, each of which alone would have left a silent zero:

1. **The store sat in the decision.** `bindPat` emitted `store i32 %t, i32* %_x` and `markBound` while the pattern
   was being lowered — above the `br i1 <pattern>`, in the block that runs whichever arm the branch picks.
2. **Three roads kept three lists of what a capture is.** The checker's census (`matchPatternNames`), the
   eligibility rule (`boundEligible`) and the emitter (`matchPattern`) disagreed about the class pattern and the
   mapping pattern. A name one lists and another does not is either a silent zero — no flag was ever attached —
   or a spurious trap — a flag attached for a name no writer ever sets.
3. **A module-level capture was not a local of anything.** The checker called `markLocal` for a match capture only
   when `inFunc` was set, so at module level `isDefinite` answered "certain", `boundEligible` handed the name no
   flag, and the read printed the top-level alloca. That is why the first three rows of the table are module-level
   and print `0`, and the fourth — the same shape inside a function — already had the machinery to trap.

## The decision

**1. A capture is queued where the pattern computes it and stored where the arm runs.** `pendingMatchBind` is one
`(name, value)` pair; `bindPat` allocates the slot where the pattern is lowered — so the alloca and the register
holding the value both dominate the arm block — and appends the pair to `g.matchBinds` instead of storing.
`emitArmCaptures` is called exactly once per arm, first instruction of the block the `br i1 <pattern>` branches
to, and it is the only place a capture's value or its written-flag is written.

**2. The store goes in front of the guard, not after it.** Measured, not assumed: CPython keeps the binding of a
pattern that matched whose guard then failed — `match 5: case z if False: pass` / `case _: pass` / `print(z)`
prints `5`. A guard reads its own capture (`case z if z == 5`), so the capture must be bound before the guard is
lowered. `v = [7]` / `case [a] if False:` / `print("after", a)` printing `after 7` is the record's answer and this
one's.

**3. One census, written beside the emitter.** `matchCaptureNames` (in `codegen.go`, next to `matchPattern`) is the
single answer to "which positions of this pattern does `bindPat` get handed". The checker's `matchPatternNames` and
`boundEligible` both read it. It mirrors the emitter arm for arm — bare name, sequence name elements, mapping name
values, class name arguments — including the two places `_` is a wildcard rather than a name, and it is *not*
recursive into sub-patterns, because `matchPattern` never binds a nested sub-pattern's names. Listing them would
have had the checker vouch for a program the emitter cannot build (filed as Gap R.301, with the refusal pinned).

**4. A match capture is a local of the frame the `match` sits in — module frame included.** `semantic.go` marks it
unconditionally. A `match` at module level is a statement of the module frame, and Python's own answer distinguishes
the two frames precisely: `NameError` at the top level, `UnboundLocalError` inside a `def`. Both classes this row
raises are caught by `except NameError:` / `except UnboundLocalError:` respectively, which is ADR 0212's rule for a
compiler-generated trap rather than an exception this row invented.

**5. An or-pattern is decided one alternative at a time, and each alternative carries its own captures.** Folding
`case p0 | p1 | …` into one `or i1` and writing every alternative's captures made a name bound by the alternative
that did *not* match answer the value the other one computed: `case [a] | {"k": a}:` over `[3]` printed `0`, because
the mapping alternative's `rt_dict_get` ran last and won the slot. One arm block cannot hold that — the stores
differ per alternative — so each alternative now ends in its own entry block that emits its own captures and jumps
to the shared body.

**6. A pattern asks the object's kind before it asks the object anything.** `heapKindInCond` puts
`rt_heap_kind ∈ {list, set}` in front of a sequence pattern and `rt_heap_kind == dict` in front of a mapping one.
@heap keeps a dict's entry count in the same word a list keeps its element count, so `case [a]` over a one-entry
dict read "one element", took the arm and captured the element array's slot 0 (row 6 of the table). This is not a
second feature bolted on: which arm runs *is* the question this ADR decides, and a pattern that matched what it was
never written for answers that question wrongly. It is also what makes decision 5 survivable — with one block per
alternative, the sequence alternative's wrong match now decided the answer instead of being overwritten by the
last store. Filed and paid as Gap R.306.

**7. Every road that reads the name tests the flag.** A capture is an ordinary local afterwards, and a local that
became tagged- or pair-bound is read through roads ADR 0228 had not visited: `taggedLoopVarRead`, `numericPairRegs`
and the direct tagged-name loads in `heapargs.go`, `render.go`, `logic_value.go`, `paircall.go` now call
`checkBound` where they load a variable. A check whose flag some reader forgot is a silent zero wearing ADR 0228's
costume.

## What this row deliberately does not fix

Two things were found and left, because bundling them would have made this commit two features.

* **A capture binds one word, not a pair (Gap R.305).** `xs = ["eight"]` / `case [a]: print(a)` prints `0` where
  `a = xs[0]` prints `eight`: the assignment road binds a container's `(payload, tag)` pair and the pattern road
  binds one word. Binding the pair through `bindPat` was implemented, measured correct on the print, and reverted
  — it moves the capture onto the pair road, which lands it in Gap R.146's territory (the positions that keep one
  word for an operand) in arithmetic and call positions. The two decisions are not one commit.
* **Nested sub-patterns, the tuple pattern, the class pattern's keyword and positional spellings** — Gap R.301,
  R.302, R.303, R.304. Each is a pattern form `matchPattern` has no arm for, and each is pinned as the honest
  refusal it is (ADR 0166) rather than as an answer. Gap R.307 pins the reverse: `case 1 | a:` and
  `case Point(x):` are forms **CPython** rejects (`alternative patterns bind different names`, `Point() accepts 0
  positional sub-patterns`) that this backend accepts. Those rows are parity failures in the other direction, and
  they are asserted so a future cycle cannot "fix" them by deleting the measurement.

## The machine path

No flag, no schema field, no exit code moved. What an agent can now read that it could not before:

* a never-bound capture is an **uncaught trap** — `exit 3`, a traceback naming the frame, `NameError` or
  `UnboundLocalError` in `@exn_msg` and in `--json`'s message — instead of `exit 0` with a fabricated integer on
  stdout, which is a result indistinguishable from a correct one;
* `gustyc --emit-llvm` shows the binding where it happens: the arm's `store i32 …, i32* %_x` and its
  `store i8 1, i8* %bnd_x` sit below the `br i1 <pattern>`, which is the artifact-level statement of this decision
  and the thing `TestTheCaptureStoreSitsInTheArmTheBranchEntered` asserts;
* the refusal sentences for the unfixed pattern forms are unchanged, and `--capabilities` still names each one.

## Alternatives rejected

* **Attach a written-flag to capture names and leave the store where it was.** The flag was already attached; the
  pattern evaluation set it. This is the fix that would have made the suite green while leaving every row of the
  table wrong, because the read would find the flag set by the arm that lost. Rejected on measurement: with the
  store in the decision, `store i8 1` is above the branch, and that line is the whole defect in one instruction.
* **Refuse every program whose capture is not bound on every path.** A definite-assignment *error* is a different
  event from a runtime trap, and CPython runs `case [a]: … case 5: …` / `print(a)` whenever the list arm is taken.
  That is what ADR 0228 settled for `if`/`while`/`try`, and a `match` arm is no different in kind.
* **Give a capture its own slot per arm.** Slots are per name per call (ADR 0181's root-stack rule: a variable is
  identified by slot address), so two slots for one name is not available here; and it would not have fixed the
  or-pattern, where the two alternatives agree on the name and disagree on the value.
* **Bind nested sub-pattern names too** (`case [[a]]`). The emitter has no lowering for a sub-pattern, so
  `matchCaptureNames` would vouch for a name nothing writes — the checker would call it possibly-bound, the flag
  would exist, and the read would trap *correctly* for a program the reference answers. Pinning the refusal
  (Gap R.301) keeps the two halves honest until the pattern form is really implemented.
* **Merge the pair-binding half in the same commit.** Right destination, wrong commit — see Gap R.305 above and
  ADR 0357's "right destination, wrong commit" clause.

## What was witnessed

Reference leg and record leg, in `pkg/lang/match_capture_test.go`: 6 trap rows and 13 answered rows asserted
against CPython's own text, 2 catchable-by-class rows, an or-pattern row run in both alternative orders, the IR
placement row (the capture's store and its flag store both below the match's own `br i1`, the flag cleared on entry
exactly once, and the flag set after the value is stored), an 8-row frame/flag table that asks the checker and the
emitter the same question, a 14-row unit table for `matchCaptureNames` that also fails if the checker's census and
the emitter's ever disagree, a 5-row census-through-the-parser table, 17 record-leg rows read out of
`testdata/interpreter-golden.json` (a missing entry fails the case), 5 refusal rows and 2 still-wrong rows carrying
today's answer beside the reference's so a fix cannot be claimed by editing a test. `integration/programs/match_binding_edges.gy`
is the whole-program row: both legs print `caught / 4 / 30 / 0 / -1 / 7 / 3 / 8` and then raise
`UnboundLocalError` from the same line. `integration/scoping_test.go` lost the pinned-garbage test Gap R.36 told it
to delete, and the `cpython-debt.json` row that said the compiled side prints `16` is gone rather than re-measured —
a paid debt fails the build until the row is deleted, and this is that promotion.
