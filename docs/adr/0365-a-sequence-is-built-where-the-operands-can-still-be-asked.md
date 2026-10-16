# ADR 0365 — a sequence is built where the operands can still be asked

**Family:** `codegen` · **Date:** 2026-07-29 · **Status:** Accepted · **Roadmap:** Gap R.33 (closed), Gap R.314 (opened), Gap R.315 (opened) · **Implements:** L11.1 (a container is a value, asked of the object) · **Co-:** ADR 0215 (the operand-kind rule this row finally applies), ADR 0232 (one door decides, the roads ask it), ADR 0234 (a container's shape and its printing are one object), ADR 0187 + ADR 0189 (payload and tag travel together), ADR 0224 (a text is an interned `@str_tab` index), ADR 0228 (a raise outside a function body is still catchable), ADR 0292 (Gap R.175's exit-2 half) · **Counter:** ADR 0273 (the door that answers one shape and stays silent about the rest) — declined for `1 + "a"` · **Counters:** — · **Extends:** ADR 0166 (an unsupported lowering is a diagnostic, not an invalid module), ADR 0246 (the tag decides which object is asked) · **Supersedes:** — · **Tier:** ladder, core (the arithmetic of the most ordinary list program in the language) · **Schema:** `--emit-llvm`, `--verify`, `--json` unchanged — three helpers added, no field.

## Context

`print([1] + [2])` did not compile. It emitted:

```llvm
%t1 = add i32 @.lst1, @.lst2
```

`llc-20` rejected the module with `global variable reference must have pointer type`, and the CLI reported the
compiler-bug class — **exit 2**, which is this toolchain's code for *our* bug. `print([1] * 3)` emitted
`mul i32 @.lst1, 3`; `print("ab" * 2)` refused in words. All three are programs CPython prints, and the record
leg has held their answers (`[1, 2]`, `[1, 1, 1]`, `abab`) since before the second engine was retired. The
shape is not exotic: it is the first thing a person writes when they have a list.

The cause was a position, not a missing helper. `value()` renders a container literal as the **address of a
compile-time global**, and by the time `+` looked, the operand was an `i32` that used to be a list: no road
below that point can tell "the number 3" from "the object that was `[1]`". Every attempt to answer it lower
down had to guess, and a guess in this backend is a wrong answer at exit 0 — the worst outcome on the ladder.

## Decision

One door, placed **before the operands are lowered**, in `pkg/lang/sequence_ops.go`; the IR it calls lives in
`pkg/lang/codegen_ir_sequence.go`.

1. **The question is asked once, and it is asked early.** `valueBinOp` asks `seqOperationRoad` after dunder
   dispatch and before the float, text and numeric roads. The operands are still `Expr`s there, which is the
   only place `is this a sequence` is answerable without a guess.
2. **The answer is a heap object the program never wrote, with a tag beside every slot.** `@rt_seq_concat` and
   `@rt_seq_repeat` copy payload *and* tag in one loop each — ADR 0187's rule for a value crossing a boundary,
   applied to a value being made. The object is reserved once, up front, at the size the door can see.
3. **A repeated text is a text.** `@rt_str_repeat` answers an interned `@str_tab` index, because every text
   road in this codebase is a `(index, length)` pair and ADR 0222 forbids the conversion nobody wrote.
4. **A pair this backend cannot claim is silence, not a refusal.** `1 + "a"` keeps the numeric road's
   `unsupported operand type(s)`; the door answers nothing it had to invent (ADR 0273's discipline).
5. **A pair the reference *stops* is a raise, not a refusal.** `[1] * 2.0`, `"a" * 1.5`, `"a" * "b"`,
   `[1] + "a"`, `[1] + (1, 2)`, `(1, 2) + [1]`, `{1:1} + {1:1}`, `{1} + {2}` go through the same raise door as
   every other trap (ADR 0228), with CPython's own sentence, so `except TypeError:` reaches them and the exit
   code is the raised-error code rather than the refusal's.
6. **A refusal is reserved for a fact, never a shape.** The three refusals that remain — the subscript of a
   built sequence, the order of two containers no name holds, and a repeat the compiler can see is past one
   object's 256-slot row — name the missing arm and the row that owns it (Gap R.314).

**Climbed** (the sequence matrix, three columns: the record's answer, the reference's, the compiled answer —
reported by the row's own test and promoted in place rather than deleted):

- **Tier 1 — already answered:** `xs = [1,2]` / `print(xs * 2)`, `print(2 * xs)`, the loop `for v in xs * 2:`,
  `len(xs + ys)` — the binding and the reading roads existed; the operation between them did not.
- **Tier 2 — the row's own wording:** `[1] + [2]`, `[1]+[2]+[3]`, `[] + [1]`, `[1] * 3`, `3 * [1]`,
  `[1] * 0`, `[1] * -2`, `"ab" * 2`, `2 * "ab"`, `(1, 2) + (3,)`, `(1, 2) * 2` — refused or exit-2, answered.
- **Tier 3 — read back out of the object:** `str([1] + [2])`, `repr(["a"] + [1])`, `f"{[1] + [2]}"`,
  `[1] + [2] == [1, 2]`, `3 in [1] + [3]`, `for v in [1] + [2]:` — these needed the door to be *one* door: the
  printer, the comparison, the membership test and the loop each ask the same `seqHandleOf`, because two
  predicates that can disagree about a value are one bug waiting to be filed (ADR 0232's rule).
- **Tier 4 — the reference stops it:** the eight raises above — refused at exit 1 before, raise at the
  raised-error exit code now, message checked character for character.
- **Tier 5 — declined:** `1 + "a"`, `[1] - [2]`, `[1] / 2` keep the numeric road's raise; `([1] + [2])[1]`,
  `[1, 2] < [2]`, `[1] * 300` keep honest refusals and are Gap R.314.

**Legs:** the **record** leg — the answer on `testdata/interpreter-golden.json`, cross-checked through
`tools/recmerge` against the reference for the 20 sources this cycle added; the **reference** leg — CPython,
through `programs/sequence_builds.gy` (agrees) and the three-leg matrix row.

## Consequences

- The exit-2 class this row belonged to (Gap R.175) stays shut for these shapes: `TestCompiledSequenceOpsNeverAnswerWrong`
  no longer *permits* a refusal or a verification failure — it requires the answer.
- The door is a position, so the next container road that wants an operand must ask `seqHandleOf`/`seqBuildsContainer`
  rather than write a fourth predicate. That is the debt this decision takes on, and it is the reason the
  subscript of a built sequence is still owed: the arm was not written, and the refusal says so.
- A repeat the compiler can see past 256 slots refuses rather than answering `[]`. That is a deliberate trade:
  a wrong answer at exit 0 would be invisible, and this one is a sentence with a row number in it.
- `@rt_seq_*` are gated on the door having been used, so a module with no sequence operation carries no slot
  copier (ADR 0173: a referenced helper that was never emitted is the module `llc` rejects).

## Machine path (Schema)

No field changed. A sequence refusal and a sequence raise are both already data:

```
$ gustyc --verify --json --file seq.gy        # exit 1, diagnostic with the roadmap row in `message`
$ gustyc --eval 'print([1] * 2.0)' --json     # exit 3, {"exception":{"type":"TypeError","msg":"can't multiply sequence by non-int of type 'float'"}}
```

`--emit-llvm` shows the three helpers by name (`rt_seq_concat`, `rt_seq_repeat`, `rt_str_repeat`), which is how
`TestTheSequenceRuntimeIsEmittedOnlyWhenTheProgramAsksForIt` reads them; the banned shapes
(`add i32 @.lst`, `mul i32 @.lst`) are asserted absent by name, so a regression to the invalid module is a
test failure rather than an `llc` error message.

## Evidence

- **unit** — `TestTheCompiledSequenceDoorAnswersTheProgramTheReferenceAnswers` (33 rows, record + compiled +
  IR), `TestTheSequenceDoorRaisesWhereTheReferenceRaises` (9, class and message),
  `TestASequenceRaiseReachesAnExceptHandler`, `TestTheSequenceRuntimeIsEmittedOnlyWhenTheProgramAsksForIt`,
  `TestTheSequenceDoorLeavesEveryOtherRoadAlone` (`7 % 3`, `"%s" % 3`, `2 * 3`, `"a" + "b"`),
  `TestTheSequenceDoorStillRefusesWhatItCannotBuild` (the three owed shapes refuse in words),
  `TestContainerConcatAndRepeatAnswersRatherThanRefuse` (promoted from the refusal test).
- **CLI** — `gustyc --file integration/programs/sequence_builds.gy` byte-identical to `python3` on the same
  file; `gustyc --eval 'print([1] + [2])'` exits 0 printing `[1, 2]`; `gustyc --eval 'print([1] * 2.0)'` exits 3
  with the reference's sentence.
- **record** — 7686 → 7738 entries (52 sources never asked before, each written from the reference by
  `tools/recmerge` with the compiled leg cross-checked, the corpus programs `sequence_builds.gy` and
  `probe_method_text_answer.gy` among them); seven paid rows removed from
  `pkg/lang/testdata/interpreter-golden-drift.json` (332 → 325) and three from
  `integration/testdata/interpreter-golden-drift.json` (23 → 20) — the text repeats and the dict addition the
  door now answers or raises the reference's own `TypeError` for.
- **filed, not fixed** — `integration/programs/probe_method_text_answer.gy` is a matrix row whose compiled leg
  prints `ab / x0 / abab / 0 / abab` against the reference's `ab / xhi / abab / hihi / abab`: a method that takes
  a parameter prints the `@str_tab` position of the text its body built, for BOTH text doors (the formatter's
  and this row's repeater), because one predicate now decides what a text is and that road registers only half
  of what its body builds (**Gap R.315**, recorded rather than asserted — a wrong value at exit 0 is not a
  parity row).
