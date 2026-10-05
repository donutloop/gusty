# 0271. `abs` asks the operand what kind it is, through the door the negation built

## Status

Accepted. Ships as one shared question, not one new door: `signlessOperandKind` in `pkg/lang/negation.go`
answers "what kind is this operand, and does it have a sign?" for both `-x` (ADR 0266) and `abs(x)`; the
interpreter's `(*Evaluator).absolute` asks the value's own `operandKind`; the sentence comes from the one
table, with `abs` spelled as its own operation (`case "abs"` in `unsupportedNumberOp`,
`pkg/lang/heapargs.go`); and both compiled lowering roads — the `i32` builtin door and the `double` road that
builds its operand through `floatValue` — ask the door before emitting anything and fall back to the emitted
raise.

Closes roadmap **Gap R.140** ("`abs` never asks its operand's kind": `abs("hi")` answers, `abs(None)` answers
`0`, `abs([1])` spends the contract's exit 2).

Depends on **ADR 0270** (Gap R.145), which landed first and had to: the door reads the same per-name status
records the print dispatch reads, and with a stale interned-text record in place `abs` *raised*
`bad operand type for abs(): 'str'` on `x = "text"` / `x = 5` / `print(abs(x))` — a program CPython answers
with `5`. Precedent for fixing a dependency before shipping the door that reads it: ADR 0267 fixed Gap R.142
in the same commit as the binding road that made it reachable; here the dependency is a commit ahead for the
same reason.

## Context

The reference does not have an absolute value for a text:

```python
>>> abs("hi")
TypeError: bad operand type for abs(): 'str'
```

…nor for `None`, a list, a dict, a set, or an instance of a user class. The unary minus had been the same
class of defect and was closed two cycles ago (ADR 0266): `-` is an operator, an operator is a question about
a **kind**, and a program whose operand has no sign stops rather than computes.

`abs` had never asked. What it did instead, on both engines, with exit code 0 — measured at `be1ea45`, before
this cycle — is in the table below. Two shapes went one step past wrong and had `llc` reject the module,
because the compiled path lowered `abs` through the negation's instruction: `%t1 = sub i32 0, @.lst1` — a
heap global under a subtract. Under the exit-code contract (ADR 0166) that is **exit 2**, the code that means
"the compiler is broken", paid by a program whose only fault is a bad argument.

Two things made the silence sticky:

- **The interpreter handed the operand to the integer evaluator.** A text in the interpreter's heap is a
  handle whose payload is an index into the interned table, and `print` dispatches on the tag — so
  `abs("hi")` returned the index and the print door, recognizing a text, rendered it back as `hi`. The
  program printed what it started with, and the test suite had nothing to complain about.
- **The compiled path had two roads into the same builtin.** The `i32` road and the `double` road each lower
  `abs`; the second builds its operand with `floatValue`, which produces nothing at all for a text, so the
  `@llvm.fabs.f64` intrinsic received an operand that had no value behind it and the module answered `0.0`
  for `print(abs("hi") * 2.5)`. Fixing one road and testing the other is how a defect like this survives a
  "fixed" commit.

The one sentence table (ADR 0265) was already the answer to "where does a `TypeError`'s wording come from",
and it had a case per operator (`unary -`, `/`, `in`, …) but no way to name an operator that is not an
operator: CPython writes `bad operand type for abs(): 'str'` — the *call*, with nothing between the colon and
the operand — not the negation's `bad operand type for unary -: 'str'`.

## Decision

**One predicate, two callers.** `negationOperandKind` (ADR 0266) and `signlessOperandKind` answer the kind for
an operand that must have a sign; `negationOperandKind` and `absOperandKind` are both thin wrappers over the
same body, so `-x` and `abs(x)` can never disagree about what `x` holds. The interpreter mirrors it with
`(*Evaluator).absolute`, which asks `operandKind` — the same table the binary operators, the negation and
`len` already read — instead of a second truthiness-shaped guess.

**The wording is the reference's, spelled as its own operation.** `unsupportedNumberOp` gains `case "abs"`,
whose sentence is CPython's verbatim (`bad operand type for abs(): '%s'`). `abs` is a new *op* in that table
rather than a borrow of the negation's row, because the reference's two sentences differ and an `except` that
matches one must not match the other.

**A raise, at the trap exit, emitted the way every other raise is emitted.** The compiled raise goes through
the store-and-branch the module owns (`@exn_flag`/`@exn_code`/`@exn_msg`), performed by the program rather
than by a helper calling `runtime.Goexit` (ADR 0228), so `try: print(abs("hi")) except TypeError:` runs the
arm on both engines and the CLI exits 3 (ADR 0166's trap class). A front-end refusal — exit 1 — would
misclassify a program the reference *runs until it stops*, and would escape the handler.

**Both roads ask.** The `i32` builtin door (`case "abs"`) and the `double` door (before `@llvm.fabs.f64`)
consult `absOperandKind`; each falls back to the shared `emitBadAbs`/`emitBadSignless` path when the slot
form declines to name a kind. An empty operand reaching the intrinsic is now impossible rather than
unlikely.

**The status records the door reads are the truthful ones.** `abs(x)` for a name asks the same `strVals` /
`internedVars` records the print dispatch asks, which is what made ADR 0270's fix a precondition. The
interim attempt — a program-wide scan (`scanSignlessNameKinds`) that declined to answer for any name bound
more than once — was implemented, measured, and deleted: it turned the wrong raise into the wrong answer
(`print(abs(x))` → `0`), which is the failure mode the whole line of work exists to end.

## Measurement

Same source, three ways, measured at `be1ea45` (before) and at this commit (after); `python3` beside them.
`interp`/`aot` are `gustyc --file <path> --interp` and `--aot`; a cell is the program's whole output with its
exit code.

| program | CPython | before · interpreter | before · compiled | after · both engines |
|---|---|---|---|---|
| `print(abs("hi"))` | `TypeError: … 'str'` | `hi`, exit 0 | `0`, exit 0 | `TypeError: bad operand type for abs(): 'str'`, exit 3 |
| `print(abs(None))` | `… 'NoneType'` | `None`, exit 0 | `0`, exit 0 | `… 'NoneType'`, exit 3 |
| `print(abs([1, 2]))` | `… 'list'` | `[1, 2]`, exit 0 | **exit 2** — `llc`: `global variable reference must have pointer type`, `%t1 = sub i32 0, @.lst1` | `… 'list'`, exit 3 |
| `print(abs({"a": 1}))` | `… 'dict'` | `{'a': 1}`, exit 0 | `0`, exit 0 | `… 'dict'`, exit 3 |
| `print(abs({1, 2}))` | `… 'set'` | `{1, 2}`, exit 0 | **exit 2** — same instruction over `@.set1` | `… 'set'`, exit 3 |
| `print(abs(Token()))` | `… 'Token'` | `<instance>`, exit 0 | `0`, exit 0 | `… 'Token'`, exit 3 |
| `print(abs("hi") * 2.5)` | `… 'str'` | wrong sentence (`can't multiply sequence by non-int of type 'float'`), exit 3 | `0.0`, exit 0 | `… 'str'`, exit 3 |
| `try: print(abs("hi")) except TypeError: print("caught")` | `caught` | `hi`, exit 0 — the handler was never reachable | `0`, exit 0 | `caught`, exit 0 |
| `x = "text"` / `x = -7` / `print(abs(x))` | `7` | `7`, exit 0 | `0`, exit 0 (ADR 0270's stale record) | `7`, exit 0 |
| `x = "text"` / `x = 5` / `print(abs(x))` | `5` | `5`, exit 0 | `0`, exit 0 | `5`, exit 0 |
| `print(abs(-3))`, `abs(-3.5)`, `abs(True)`, `abs(3 - 10)` | `3`, `3.5`, `1`, `7` | ✓ | ✓ | ✓ — the raise is not bought by breaking the answer |

## Consequences

- The `sub i32 0, <heap global>` family loses one of its two producers; the other (`-[1, 2]`) went in ADR
  0266, and `runtime_ir_test.go`'s module-wide "no `i32 @.` in a value position" assertion now covers the
  `abs` shapes too.
- `abs` is listed in `docs/language.md` beside the negation, with the kinds it stops on and the sentence each
  one earns; `docs/operations.md` records the trap's exit class and the three-engine program.
- `integration/programs/abs_names_its_kind.gy` joins the conformance corpus and is `match` on all three legs
  (the corpus counts in `README.md` move with it).
- What the door still cannot name stays a refusal and says so: a slot whose kind only the run time can
  describe (`xs = [1, 2]` / `xs.append("s")` / `abs(xs[0])`) refuses naming the missing tag and roadmap L11.1 —
  the same open clause that owns `abs(n)` for a pair-bound name (**Gap R.143**). It is exit 1 with both
  operands named, which is the honest answer, and the interpreted leg answers `1` as CPython does.
- Two exit-2 classes measured while probing the door's edges are filed with their own IDs rather than
  absorbed here: a builtin or an imported module used as a *value* (`print(len)`, `abs(math)` — compiled
  emits `load i32, i32* %_len` for a slot that was never allocated; interpreted says `NameError` where
  CPython has a value) is **Gap R.150**, and a `lambda` in a numeric position (`print(-(lambda x: x))` —
  compiled emits `sub i32 0, lambda_0`, the function global under the same instruction this ADR removed for
  containers) is **Gap R.151**. Both reproduce at `be1ea45`, so neither is this cycle's doing.

## Agentic rationale

- The trap is machine-consumable at the boundary agents use: exit **3**, the class on the traceback line an
  `except` matches, and the sentence verbatim, so `--json` and a script can key on
  `TypeError: bad operand type for abs(): 'list'` without scraping IR.
- The compiler's own verifier path is part of the test: `gustyc --verify-llvm-file <path>` must report the
  module verified for every trap shape, and the tests fail on exit 2 for them — the class of bug that hides
  behind "the program didn't print the right thing" is exactly the class the exit-code contract exists to
  make loud.
- One sentence table means one place for a tool to enumerate the diagnostics the language can raise; `abs`
  joining it (rather than a second `sprintf` in a builtin) keeps that enumeration complete.
- The tests are the documentation of the boundary: `pkg/lang/abs_kind_test.go` (parity, traps by class and
  message, catchability, IR shape, and a row that asks both doors about the same operand and fails if they
  name it differently) and `integration/abs_kind_test.go` (the same through the shipped CLI against
  `python3`, with the exit class pinned and exit 2 forbidden).

## Alternatives rejected

- **Fold `abs` of a non-number to a constant.** The folders had already produced the wrong answer once
  (`abs` of a text folded to the interned index at the print door); a folded `-None` is a silent `0` with no
  instruction that could have disagreed, which is Gap R.37's rule and it applies here word for word.
- **A front-end refusal (exit 1).** The reference *runs* this program until the value arrives and then stops;
  a compile-time verdict changes the exit class, escapes `except TypeError:`, and tells the author their
  program is unwritable rather than wrong. Refusals are reserved for what this pass genuinely cannot state —
  which is why the mixed-kind slot read keeps one.
- **An `abs`-specific sentence table, or reusing the negation's row.** The first is the drift ADR 0265 was
  written to end (three truthiness tables, one `HeapKind` renumbering); the second ships a sentence CPython
  does not write, and breaks an `except` that compares messages.
- **Declining to answer for any name with two bindings.** Implemented, measured, deleted: it converts a
  wrong raise into a wrong answer (`0`) at exit 0. A name answers with its latest binding (ADR 0172's rule,
  finally implemented for every status by ADR 0270); the answer is to retire the stale record at the binding
  that replaces the value, not to stop asking.
- **Fixing only the compiled road, or only the `i32` road.** The interpreter's `abs` was a third of the bug
  (it printed the operand), and the `double` road was a fourth (`0.0`). Both backends are first-class in this
  project; the test tables ask every row of both, in both lowering roads.
- **Leaving Gap R.145 to its own cycle.** It would have meant committing a door whose `abs(x)` verdict was
  wrong for any name that ever held a text — shipping a defect that the same commit's own test table can
  see.
