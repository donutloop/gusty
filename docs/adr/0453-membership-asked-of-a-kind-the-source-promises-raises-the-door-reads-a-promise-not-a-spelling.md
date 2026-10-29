# ADR 0453 — membership asked of a kind the SOURCE promises raises: the door reads a promise, not a spelling

- **Status**: Accepted
- **Date**: 2026-11-01
- **Roadmap**: **Gap R.522** done — filed 2026-10-30 by **ADR 0452**, from the sweep that closed **Gap R.521**.
  Three defects the sweep for this row measured are filed beside it: **Gap R.523** (a list literal asked for an
  element kind a NAME does not hand it, and only under `in`), **Gap R.524** (a tuple-unpacking target storing a
  spelled container emits IR `llc` rejects), **Gap R.525** (`except … as name` leaves the name bound where the
  reference unbinds it).
- **Builds measured**: parent `0ad5933` (**ADR 0452**) and this commit, every byte from the gate's
  `-tags=llvm20`.

## Context

**ADR 0452** gave the membership door the kinds the source writes AT the question. `print(1 in 5)` raises
`TypeError: argument of type 'int' is not iterable` at exit 3, because the `5` is spelled there, it is one of the
four `notIterableKinds`, and a value of that kind has no members. What that row left standing — and pinned with
today's bytes, so that the residue was a measurement rather than a rumour — was the same question asked of a
value the source only NAMES:

```
$ gustyc --eval 'x = 5
print(1 in x)'
False                                    reference: TypeError: argument of type 'int' is not iterable

$ gustyc --eval 'def f():
    return 5

print(1 in f())'
False                                    reference: … 'int' …
```

Both at the exit code that means the program ran. The same `int`, handed over by a container slot
(`xs = [1]` / `print(1 in xs[0])`), raised the reference's own sentence — because there the builder left a
`(payload, tag)` pair beside the payload and the OBJECT gets asked. **Gap R.521**'s row read this as a defect
about a word rather than about a door, and **ADR 0452** said so in its own text: the compiler's kind maps for a
name are *a summary of the assignments a pass has seen, not a promise about the value the read receives*.

That sentence is the whole design space of this row. The counterexample is one statement long:

```
x = [1]
if cond():
    x = 5

print(1 in x)
```

The last binding the emitter lowered says `int`. Whether the run is standing in that arm is a run-time fact, and
a door that read the summary would trade the reference's `True` for a raise. The house forbids that trade in as
many words — *"a fold that raises too eagerly turns a working language feature into a trap, which is worse than
the bug this row filed"* (`membership_fold_test.go`), *"a gate that guessed is worse than a gate that declines"*
(`answer_kind.go`) — and **ADR 0299** measured the same cliff on the verdict table and rejected stepping off it.

## Decision

**1. Ask a different question, about the program rather than about a name.** Not *"what kind does the compiler
currently think `x` is"*, but:

> did every road that could ever have written this name write **this literal**?

`pkg/lang/membership_kind.go` states that rule and reads it in two passes over the frame that owns the name.
`countStmtBinders` counts **every** binder road anywhere in the frame — plain assignment, augmented assignment,
`for`/`while`-`else` targets, comprehension targets (including the ones that hide inside an expression), walrus,
`match` captures, `with … as`, `except … as`, `import`, function and class definitions. A candidate promise is a
top-level `x = <no-member literal>` and it survives only if the count for that name is exactly one. Anything else
retires the name and the door stays out.

**2. A name is read in the frame that owns it.** `spelledScalarFrame`s chain, exactly as the float, slot and
frame-target tables do, and the walk up them stops at the first frame that knows the name **at all** — a body
that binds its own `x` settles nothing about the module's, and an inner binding hides an outer promise rather
than merging with it. `global` is read off the declarations themselves (`globalDeclsIn`, `programGlobalWrites`)
in both directions: a name a body declares global is written where the scan cannot see it, so it is not the
module's promise to make (**ADR 0448**'s road is the newest in the tree and the one a name-keyed summary would
never have noticed).

**3. A call answers when every road answers.** `bodySpellsOneNoMemberKind` asks a module `def` whether every
`return` road — through `if`/`elif`/`else`, `for`, `while`, `try`/`except`/`finally`/`else`, `match`, every arm
required, a fall-off-the-end spelling the void — spells a literal of one no-member kind. It reuses the answer
direction's own arm classifier and the ONE body walk (`answerBodyReturns`) rather than writing a second one
(**ADR 0232**), and the ONE table of kinds (`notIterableKinds`). Two guards keep the shape honest: the name must
not be a parameter at any call site, and the program must not assign to the name anywhere
(`assignedNamesEverywhere`) — a program that re-points `f` at a lambda has taken the promise back, and the door
says so by emitting nothing rather than by raising and then explaining.

**4. The reading is installed where every lowering path can see it.** `GenerateIRReport` installs the module's
table *before* the pass that lowers function bodies, and `enterBody` installs a frame's table with the same
save/restore closure as the other three per-frame tables. Read from inside `enterBody`, a `def` whose body reads
the module's `x` would find no frame to walk up to, and whether the promise reached a body would depend on which
loop the emitter was standing in — the shape **ADR 0343** exists to outlaw, caught here by a test
(`TestTheMembershipPromiseDoorIsAskedAsAFunction`) rather than by argument.

**5. A read must sit behind its binding.** `spelledScalar` carries the binding's line and the door declines a
read written above it: `def g(): print(1 in x)` / `x = 5` / `g()` is a program whose real complaint is a
`NameError`, and raising `TypeError` for it would replace one wrong answer with another. A span with no line on
either side declines too — a wrong line number is worse than no verdict, which is `atLocation`'s rule.

**6. The raise is the one the language gives.** `notIterableSentence(kind)` from **ADR 0271**, emitted through
`exnCode`/`raiseTo` — the same non-local road the run-time door uses — so `except TypeError:` reaches it, the
program continues afterwards, and the loop's sibling wording (`'int' object is not iterable`) stays the loop's.
The needle and the haystack's own call are lowered first, so `print(1 in probe())` performs `probe` exactly once
and then raises.

## Agentic rationale

An agent that writes `if item in config:` where `config` is a number wants one answer: the type, in the
reference's words, at the trap's exit class. `False` at exit 0 is the answer that hides the mistake — the
container branch is skipped, the program continues, and the failure surfaces three files later with no span
attached. The compiled leg now says `argument of type 'int' is not iterable` at exit 3 for the spelled literal,
the name and the call alike, which is the same diagnosis the reference gives for the same program, and a program
that means to catch the raise still can.

The machine path is unchanged and needs no new surface: no flag, no diagnostic code, no schema change. What is
new for an agent is that the three spellings of the question now cannot drift, because all three read
`notIterableKinds` and one sentence door. The record leg carries the new answers
(**34 entries, 10317 → 10351**, written from the reference by `tools/recmerge` with the compiled leg
cross-checked), and the reference leg reads each expectation out of a live `python3` inside the case, so a row
that stops being true stops being green. The file's own recorder
(`TestTheRecordSourcesThisRowAsksFor`, `GUSTY_RECORD_MEMBERSHIP=<file>`) dumps the sources its tables ask, so a
case cannot ship without a record under it (**ADR 0302**).

## Codegen / IR implications

For a promised kind, `@main` (or the `define` the question sits in) carries the raise and **no** container
question: no `@rt_contains`/`@rt_contains_tagged` call, an `@exn_flag` test, an `@exn_msg` store of the
reference's sentence, and a branch to the handler or the frame's `raiseexit` — the block has no edge into what
follows, which is why the word the door hands back is never read. For every shape the door declines the module
is byte-identical to the parent build's, and `@main` carries no membership sentence at all; that negative is
asserted at IR level for a container a name holds and for a call the program re-points. `rt_contains`, the tagged
membership road and the frame-metadata tables are untouched: no new runtime symbol, no new global, no new
section, and the four per-kind sentences the runtime carries are still generated from `notIterableKinds`.

The two program-wide readings (`callKinds`, `assignedNames`) and the per-frame table are compile-time only; none
of them reaches the module. The frame's table is built before the body is emitted, which adds one AST walk per
frame — the same shape and order as `auditFrameSlots`' pass, and the suite's timing is the measurement of that.

## Alternatives rejected

- **Reading the compiler's existing name→kind maps.** Fourteen name-keyed notebooks already exist, and every one
  of them is a summary of what a pass happened to see. The counterexample is in the Context: the arm that did not
  run, the loop that may run twice, the `def` that is never called. This is **ADR 0299**'s rejected table and
  **Gap R.521**'s own warning, applied here.
- **Reading the type checker's inferred type.** The checker runs on a different path from `GenerateIRReport` —
  `--emit-llvm` and `--eval` reach codegen through `Compile`, and an inference that is `object` wherever the
  program is unannotated is not a promise either. It also cannot see the binder roads that live inside
  expressions, which is where half of the retirement cases hide.
- **Raising on the LAST binding the emitter lowered.** That is what the existing kind summaries effectively
  encode, and it is exactly the arm-that-may-not-run bug: `x = [1]` / `if 1 < 2: x = 5` would raise at the
  reference's `True`.
- **Refusing at build time instead of raising.** The reference runs every one of these programs, including the
  ones with a handler around them, which run to completion and exit 0. A refusal trades a wrong answer for a
  build that will not happen (**ADR 0166**'s artifact class).
- **Reaching for the tagged value word (L11.1).** That is the right mechanism for the residue and the wrong one
  for this row: the promise cases need no tag, because the value IS the literal, and waiting for L11.1 would
  leave 17 shapes answering `False` at exit 0 in the meantime. The pair cases stay refused/pinned, owned by
  **Gap R.146**.
- **Keeping `TestWhatTheMembershipRowStillOwesIsFiledNotFixed` and editing its expectations.** A residue table is
  a defect list with today's bytes; when the row that owned it pays, the table moves rather than keeping a claim
  that is no longer true. It is deleted, and `TestWhatTheMembershipDoorStillDeclines` is the residue now — with
  three control rows the sweep found REMOVED rather than recorded, because the reference does not print what the
  old table claimed for them.

## What this row does not pay

The haystack whose kind is a run-time fact: a parameter's kind (only the call sites know it), a name bound inside
an arm, a copy binding (`y = x`), a call that forwards its parameter, a body whose arms answer two kinds, a
builtin no door names an answer for (`bool`), and any value that arrives as a `(payload, tag)` pair — the last is
**L11.1**'s tagged word at a one-word position (**Gap R.146**). Each is pinned with today's bytes, and the row
that owns it, in `TestWhatTheMembershipDoorStillDeclines`. Beside them sit three refusals this sweep measured and
filed rather than absorbed: **Gap R.523**, **Gap R.524**, **Gap R.525**.
