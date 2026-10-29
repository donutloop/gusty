# ADR 0452 — membership asked of a value with no members raises: the fold reads the same table the object is asked about

- **Status**: Accepted
- **Date**: 2026-10-30
- **Roadmap**: **Gap R.521** done — filed 2026-10-30 by **ADR 0450**, from the same sweep that closed **L12.1**
  and filed **Gap R.520** (closed the same day by **ADR 0451**). The residue this row cannot pay is filed as
  **Gap R.522**.
- **Builds measured**: parent `af0eace` (**ADR 0451**) and this commit, every byte from the gate's
  `-tags=llvm20`.

## Context

`x in y` is a question about `y`. Ask it of a value that has no members, keys, elements or characters and the
reference does not answer `False` — it ends the program:

```
$ python3 -c 'print(1 in 5)'
TypeError: argument of type 'int' is not iterable
$ python3 -c 'print(1 in None)'
TypeError: argument of type 'NoneType' is not iterable
$ python3 -c 'print("a" in 1.5)'
TypeError: argument of type 'float' is not iterable
```

The compiled backend answered `False` to all of them, at the **exit code that means the program ran**. It had
the right behaviour already, one door away: `xs = [1]` / `print(1 in xs[0])` ends at exit 3 with the reference's
own sentence, because there the haystack's kind is a run-time fact and the object is asked about it — the tagged
membership road walks `TagInt`, `TagFloat`, `TagBool`, `TagNone` and raises `notIterableSentence` for each, and a
kind it cannot name is told it does not contain things at all (`notIterableSentence("object")`, the word the
reference itself uses for an `object`).

What separated the two spellings was a **fold**. A spelled literal on the right of `in` never reaches a tag: it
reaches `rt_contains(i32 5, i32 1)`, and the runtime's untagged scan answers 0. The emitted module for
`print(1 in 5)` was, in full:

```llvm
  %t2 = call i32 @rt_contains(i32 5, i32 1)
  %t3 = icmp ne i32 %t2, 0
  %t4 = zext i1 %t3 to i32
  call void @rt_print_bool(i32 %t4, i32 0)
```

That is a fold substituting a verdict for a raise. The house has a standing rule about that shape: **ADR 0266**
and **ADR 0329** — when the compiler has the kind in hand, the fold may substitute an answer only when that
answer is the one the language gives. Here it is not.

## Decision

**1. The fold raises.** `pkg/lang/membership_fold.go` adds one gate, asked in `valueBinOp`'s membership block:
if the haystack is an `*IntLit`, `*FloatLit`, `*BoolLit` or `*NoneLit`, the door emits
`g.raiseTo(b, exnCode("TypeError"), "TypeError", notIterableSentence(kind), n.Span())` and hands back the
never-read word, exactly as the instance arm of the same block already does. The raise is the non-local kind,
so `except TypeError:` reaches it and the program continues afterwards — which is the difference between this
row paying a raise and refusing a program (**ADR 0166**'s ladder: a front-end refusal would stop a program the
reference runs with a handler on it).

**2. One table, read by both doors.** The four kinds were spelled inline in the runtime road
(`heapargs.go`) as a local struct literal. They are now the package-level `notIterableKinds`, which the tagged
run-time road iterates and the compile-time fold consults, so "which values have no members" is stated once.
This is **ADR 0232**'s one-question rule applied to a set rather than to a word, and the same instinct
`abs_kind_test.go` polices for the print, negation and index doors: two tables means a second drift.

**3. One sentence, from the door that already writes it.** The raise's text is `notIterableSentence(kind)` —
the same function the run-time arms call — and the sibling `notIterableLoopSentence` stays the loop's own,
because the reference phrases the two differently (`argument of type 'int' is not iterable` names the type;
`'int' object is not iterable` names the object). **ADR 0271** is the precedent for not letting one door write
the other's wording, and a row asserts the fold never speaks the loop's sentence.

**4. The needle is evaluated first.** The gate sits where both operands have already been lowered, so
`print(probe() in 5)` performs `probe`'s print and then raises, and performs it exactly once. The reference
evaluates the needle before it asks the haystack's type; a raise emitted earlier would drop an effect the
program performed, which is the **Gap R.520** mistake at the other end of the operator.

**5. The gate is asked where the chain's links are lowered, so it is asked once.** `1 == 1 in 5` is a chain
whose second link is `1 in 5`, and a link goes through the ordinary comparison road (**ADR 0288**) — so the
chain inherits the raise rather than needing its own arm. No chain-specific code, no second door.

## Agentic rationale

`1 in 5` is the kind of program an agent writes when it has mixed up which operand a membership test is about,
and `False` at exit 0 is the answer that hides the mistake: the program continues, the container branch is
skipped, and the failure surfaces three files away. The reference's sentence names the type, which is the whole
diagnosis, and this backend now prints it at the trap's exit class — with the program's own handler still able
to reach it, so a program that intends to catch the raise still can.

The machine path is unchanged: no new flag, no new diagnostic code, no schema change (`--check` has named this
pair since **ADR 0450** as `type.compare.uncomparable`, warning level, and the run path now agrees with it
instead of contradicting it). The rows are all legs: the compiled leg's exit code and sentence, the record leg's
recorded trap, the reference leg's live `python3` traceback read inside the case, and the module — `@main` asks
no `@rt_contains` at all for a haystack the source says has no members, and carries the raise.

## Codegen / IR implications

`print(1 in 5)`'s `@main` now carries the raise (`@exn_flag` test, `@exn_msg` store of the reference's sentence,
branch to the handler or `main.raiseexit`) and no `@rt_contains` call. The runtime's untagged `rt_contains` is
untouched — it is still what a real container asks — and the tagged membership road emits the same four raises
it always did, now generated from `notIterableKinds` instead of an inline list. Nothing else in the module
changes: no new runtime symbol, no new global, no change to any answering membership program (the control table
— list, tuple, set, dict, text, container element — is byte-identical to its parent on both legs).

## Evidence

Two test files and one corpus program. `pkg/lang/membership_fold_test.go`: 6 trap rows asked of the compiled leg
AND the record leg, 4 catchable rows, the needle-order row, 11 control rows each on both legs, 4 rows that the fold
and the tag door read one table, 2 IR rows, and the filed-not-fixed table. `integration/membership_fold_test.go`:
7 CLI rows whose sentence is read out of a live `python3` traceback inside the case, 4 catchable rows, 7 control
rows, and the `--emit-llvm` row. `integration/programs/membership_not_iterable.gy` joined the conformance corpus as
a `match` row (279 programs, 265 matrix rows with 229 asserted, 215 `match`), and **21 record entries
(10296 → 10317)** were written from the reference by `tools/recmerge` with the compiled leg cross-checked — the
corpus program's own entry included, which `TestGCCorpusCollectsAndAgrees` demands of every asserted program.

## Alternatives rejected

- **Answering `False` with a warning.** A warning at `--check` and a wrong answer at run time is what
  **ADR 0450** shipped for the *checker*, and it is right there because the checker must not stop a build. It is
  wrong here: the run is the door that decides, and a program that never reaches the handler because the answer
  was fabricated has no way to find out.
- **Refusing the program at build time (`lower.unsupported.membership-scalar`).** The reference RUNS these
  programs — including the ones with `except TypeError:` around them, which run to completion and exit 0. A
  refusal trades a wrong answer for a build that will not happen, which is **ADR 0166**'s artifact class.
- **Extending the gate to names and call results.** `x = 5` / `print(1 in x)` is the same wrong answer, but the
  compiler's kind maps for a name are a summary of the assignments it has seen, not a promise about the value:
  the same name can hold a list where another statement put one, and a container's element is exactly the
  position where the run-time door is the only honest witness. The run-time road proves this by answering
  `1 in xs[0]` correctly today. Filed as **Gap R.522**, pinned with today's `False` in
  `TestWhatTheMembershipRowStillOwesIsFiledNotFixed`, and owned by **L11.1**'s tagged value word.
- **Raising inside `rt_contains` for a non-heap word.** A number is a legal `rt_contains` argument elsewhere
  (a container's untagged scan), and the runtime cannot tell the folded literal from a variable that happens to
  hold 5; only the front end knows the value was spelled. Semantics the compiler can decide belong in the
  compiler (**ADR 0166**'s division of labour), and a runtime change here would need the tag the word does not
  have.
- **A second sentence function for the fold.** `notIterableSentence` is already the door; a copy that the fold
  owns would be the drift `abs_kind_test.go` exists to catch.

## What this row does not pay

**Gap R.522** (a name's or a call's answer as the haystack still answers `False`) is filed with today's bytes
pinned. **Gap R.521**'s own Definition of done — `print(1 in 5)`, `print(1 in None)`, `print("a" in 1.5)` and
`print(1 == 1 in 5)` reaching exit 3 with the reference's sentence, `except TypeError:` reaching each, and
`1 in [1]`, `"k" in {"k": 1}`, `1 in {1}` and the run-time-slot row keeping their answers — is what the two test
files assert, on the compiled leg, the record leg and the reference leg.
