# A condition asks a parameter the kind its caller supplied

**Roadmap:** Gap R.361 (closed here — `if v:`, `elif v`, `while v:`, a ternary head, `not v` and `bool(v)`
over a pair-carrying parameter) ·
Gap R.365 (closed beside it by the same move — the operand of `not` ran twice) ·
Gap R.220 / **ADR 0389** (the argument that crosses as a pair; this ADR is what the callee does with it) ·
ADR 0388 (the condition door for a self-describing *read*: same table, other operand) ·
ADR 0275 (`@rt_pair_truth`, the table `and`/`or` already consult for the operand they choose) ·
ADR 0268 / Gap R.143 (the lift this row keeps for a name the arithmetic door bound) ·
ADR 0331 / ADR 0386 (the same table for a literal's element and a loop item) ·
ADR 0172 (the latest-binding rule, which is why a rebinding body is not served by this door) ·
ADR 0228 / Gap R.68 (the written-flag a pair read has to test) ·
Gap R.366, Gap R.367, Gap R.368, Gap R.369 (the residue, filed with today's bytes: the stale tag under a
rebinding and the one that never ends, the operand `and`/`or` and the ternary choose, the convention that has
no second word, and the empty container spelled as a call) ·
L11.1 (the tagged value word, in the position that only asks whether it is true) ·
ADR 0166 (a wrong branch at exit 0 is the defect this repo files rather than ships)

## Status

Accepted. Landed 2026-10-22.

## Context

ADR 0389 gave a call argument two words. `f(d["e"])` over `d = {"e": []}` crosses as a `(payload, tag)` pair,
the callee's `define` grew `%q<i>`, and `bindPairParams` stores the pair into the parameter's two slots.
Printing that parameter has been right ever since — `print(v)` writes `[]`.

Testing it was not. The same program with the parameter in a condition:

```python
def f(v):
    if v:
        return 1
    return 0

d = {"e": [], "t": "", "l": [1], "n": None}
print(f(d["e"]), f(d["t"]), f(d["l"]), f(d["n"]))
# CPython:  0 0 1 0        — backend: 1 1 1 0, at exit 0, no diagnostic
```

Six positions ask a value this question, and five of them came to the same line of code:
`if`/`elif`/`while` and a ternary head are lowered through `truthOperandErr`, `not` asks it `truthyValue`,
and the builtin `bool()` asks the same question on its own road beside `str()`. What `truthOperandErr` had
for a name the pair road bound was the arm ADR 0268 built for a name the **arithmetic** door bound:

```
numericPairLift(b, e)   →   %d = call double @rt_lift_num(i32 %payload, i32 %tag)
                            %t = fcmp one double %d, 0.0
```

That is exactly right for the kinds arithmetic can produce — int, float, bool, whose tag `@rt_num_arith`
wrote — and exactly wrong for the kinds a CALLER can produce. A text's payload is its `@str_tab` index; a
container's is a heap handle; `None`'s is `0`. `rt_lift_num` has three arms and none of them is "this is an
interned word": below tag 3 it lifts, otherwise it widens the payload as if it were the number. An empty
list is a nonzero handle, so `if []:` took the then-arm, `not []` said `False`, `bool([])` said `True`, and
`1 if [] else 0` said `1`. Measured on the parent build across the six positions × fifteen entry kinds:
**24 wrong answers at exit 0** out of the 90 programs — the empty list, the empty dict and the empty text in
all six positions, and `0.0` and `-0.0` in the three that reach the lift (`not`, `bool`, the ternary head) while
`if`/`elif`/`while` answered the same pair correctly. One pair deciding differently in `if v:` than in `not v:`
is the tell that the roads were asking different doors, and it is why the ladder is written as positions ×
kinds rather than as one table of truths.

The table that decides this already existed. `@rt_pair_truth` (ADR 0275) is what `and`/`or` consult for the
operand they test, what ADR 0388 put behind a self-describing dict entry in an `if`, and what ADR 0386 put
behind the item of an unrolled loop: a number in either family is true when it is not zero, an empty text or
an empty container is false, `None` is false, any other heap object is true. The parameter's truth is the
same fact about the same pair. The condition's head simply never asked it for a value whose kind arrived
from a call site.

Two things had to move, not one. `pairGate` — the scan that decides whether a function's parameters ride
the pair road at all — serves a body whose mentions of a parameter sit in positions the pair doors answer,
and its `expr` arm for `*UnOp` accepted `-` alone while its `*Call` arm listed `print`, `printf` and `str`.
A body that only ever asked `not v` or `bool(v)` was therefore *declined* the pair: it kept one word, and
the truth door that did grow the arm had nothing to ask. Serving the position without reordering `not` then
broke the program outright — `value()` lowers a unary operand for its **value** before the `not` branch runs,
and the one-word read of a pair-bound name is the position that refuses: `y = v` over such a parameter still
stops there today, naming Gap R.146. Three states of that one line, all measured: on the parent build
`print(not v)` over `d = {"k": []}` answered `False` at exit 0; with `pairGate` serving `not` and the operand
still lowered first, the program refused with the tagged-value-word sentence; with the truth asked first, it
prints `True` and the operand runs once.

## Decision

**One ask, one table, three gates.**

1. `(*irGen).pairSuppliedNameTruth` (new file `pkg/lang/pair_param_truth.go`) loads the parameter's two
   words through `taggedLoopVarRead` — the one door every pair-bound read uses, and the one place ADR
   0228's written-flag is tested for all of them — and hands them to `pairTruth`, which emits
   `@rt_pair_truth`. It is called from `truthOperandErr` (which covers `if`, `elif`, `while`, the ternary
   head and `not`) and from `builtinBool`, the only truth position with a road of its own.
2. The gate is the **origin**, not the shape: `g.taggedOrigin[name] == taggedOriginParam`. A name the
   arithmetic door bound keeps ADR 0268's lift — its tag cannot name anything but int, float or bool, and
   the module such a condition compiles to grows no call it has no reason to make. `taggedOriginSlot`,
   `taggedOriginLoop` and `taggedOriginFold` stay where ADR 0388 drew them: they are Gap R.146's, and a
   door that opens two rows at once is judged by neither.
3. The gate is also the **body**. The pair's two slot words have exactly one writer — the store
   `bindPairParams` emits at the top of the frame. A rebinding lowers through the plain scalar road, which
   stores the payload and calls `forgetTaggedBinding` (ADR 0172's latest-binding rule) but leaves the
   run-time tag word holding whatever the call site left there. Asking the table then reads a tag that
   describes no value the name holds: `while v: … v = []` asks `rt_heap_len` of a payload that is no longer
   a list and the loop never ends — a regression this row would have shipped, caught by three probes. So
   `bodyKeepsTheEntryPair` walks the current `def` for every form that can write the name — assignments,
   augmented assignments, `for` variables, `match` captures, `with as`, imports, nested `def`/`lambda`
   parameters and comprehension clause targets — and declines the door where anything writes it. An
   unknown statement form counts as a write: the two directions are not symmetric, because declining costs
   a program an answer it was getting wrong anyway, while standing on a writable name costs it a loop that
   never terminates.
4. `pairGate` learns the three positions the truth door answers: `not X` is served when `X` is (its answer
   is a verdict, so no word has to carry the kind), a ternary's **condition** is served while an **arm**
   that mentions a pair-carrying parameter is not (a `select` has one type for both operands — that answer
   needs the tagged value word, which is Gap R.147), and `bool` joins `print`/`printf`/`str` as a call whose
   argument may ride the pair.
5. `not` asks the truth road **before** the operand is lowered, because the operand's VALUE is never used.
   Lowering it first did two things: it ran the operand twice — `print(not boom())` printed `boom` twice at
   exit 0 where the reference prints it once, which is roadmap **Gap R.365** — and for a pair-bound name it
   produced the payload word, the same one-word read that leaves a ternary arm printing `3` (**Gap R.367**).
   Asking the truth first is what lets the pair road decide; the two halves are one ordering.

## What moved, measured

Baseline and post-change binaries, same sources, `python3` as the oracle:

| shape | before | after | reference |
|---|---|---|---|
| `if v:` over `[]`, `""`, `{}`, `None`, `0.0` | `1`/`t` | `0`/`f` | `0`/`f` |
| `not v` | `False` | `True` | `True` |
| `bool(v)` | `True` | `False` | `False` |
| `1 if v else 0` | `1` | `0` | `0` |
| `while v:` + `break` | body runs | body skipped | body skipped |
| `print(not boom())` | `boom` × 2 | `boom` × 1 | `boom` × 1 |
| `print(v)` (control) | `[]` | `[]` | `[]` |
| `if v:` over a provable `d[1] = 2` (control) | `t` | `t` | `t` |

The full ladder — six positions (`if`/`else`, `if`/`elif`/`else`, `not`, `bool`, a ternary head, a `while`
head) × fifteen entry kinds, plus the row program — is 91 sources, every one recorded from the reference by
`tools/recmerge` with the compiled leg cross-checked (none refused), every one asserted compiled-vs-record in
`TestTheTruthOfTheParameterAgreesWithTheRecord`. `tools/recmerge` wrote **98** entries in this cycle (the
record to **8432**): the 91 the ladder and its row program, the residue programs this ADR files rather than
hides, and the conformance program.

## The IR, asserted

`TestTheTruthOfTheParameterReachesThePairTable` reads the callee's own `define` and requires both
`call i32 @rt_pair_truth(i32, i32)` and the load of the tag slot the entry store wrote
(`load i32, i32* %_v_tag`) — for the `if`, the `not`, the `bool()`, the ternary head and the loop head.
`TestTheTruthOfTheParameterDoorOpensOnlyWhereItShould` is the negative control that makes that a measurement
rather than a widening, three ways:

* a **provable** integer argument (`f(d[1])` over `d = {1: 2}`) — no pair crosses, no table appears, the
  body keeps `icmp ne i32 %p0, 0`;
* a name the **arithmetic** door bound (`n = xs[0][0] * 2` / `if n:`) — `@rt_lift_num` stays and
  `@rt_pair_truth` does not arrive;
* a body that **rebinds** its own parameter (`while v: … v = 0`) — no `@rt_pair_truth`, because the tag
  would be a stale one by the second iteration.

## Agentic path

Nothing new is machine-visible: the row moves answers, not the interface. `gustyc --json` reports the ladder
programs at `"exit": 0` with the printed bytes, `--emit-llvm` carries the `@rt_pair_truth` call inside
`define i32 @gy_f(i32 %p0, i32 %q0)` for an agent that wants to see which door decided, and `--capabilities`
is unchanged — this row closed no refusal, and the residue it leaves standing is what Gap
R.366, Gap R.367, Gap R.368 and Gap R.369 file: two shapes that answer at exit 0 with no diagnostic, one
shape that refuses in words and one that does not terminate. All are pinned with today's bytes in both suites,
so the day one of them opens, a test fails and says which row to delete.

## Alternatives rejected

* **Ask the table unconditionally, for every origin.** Wrong on the numbers: `taggedOriginArith`'s tag can
  only ever hold 0, 1 or 2, and ADR 0268's lift is the door that was written for it. It is also wasteful —
  every `if n:` in a program that never saw a call would grow a call.
* **Refuse the position instead, as other pair doors do.** Gap R.146's positions refuse because the value
  has to *travel*; a condition only asks a verdict, so refusing would cost `if v:` — a program the reference
  runs and this backend already compiles — its only green answer. ADR 0273's rule again: a gate must not
  cost a program that compiles today.
* **Serve a ternary arm that reads the parameter.** The arm is the answer, and a `select` has one type; the
  pair would be split and the payload printed. That is Gap R.147's missing tagged value word; the ternary
  road does not ask whether the arm's name carries a second word, so today it prints the handle (`3` for an
  empty list against the reference's `z`), and **Gap R.367** pins that byte for byte rather than let it read
  as an answer. `x and 7` in the same position refuses, which is the proof that the missing thing is a word.
* **Track which slot writes the tag dynamically.** The frame has no per-path tag state; ADR 0172's rule is
  already positional at compile time. A run-time "is the tag still the entry's" bit is a third word per
  pair-bound parameter, bought for a family (Gap R.366) that is also wrong about the *payload* and belongs
  to the rebinding road, not to the truth door.
* **Let the rebinding family ride the door and file the hang.** Three probes hung — a run that never ends is
  strictly worse than the wrong number it replaced, and a hang introduced by a "wrong answers → right
  answers" commit is the shape ADR 0166 exists to catch. They were measured on a pre-change binary first,
  the gate was added, and the same probes are pinned as they now behave.
* **Fix `not` by making one-word reads of pair names answer.** `value()` lowering a unary operand is the
  general road; the operand of `not` is never used, and asking the truth door first is both cheaper and the
  reason Gap R.365's double call disappears.

## Consequences

`pkg/lang/pair_param_truth.go` is the file that owns "the truth of a name whose kind the caller supplied";
`truthOperandErr` and `builtinBool` are its two callers, and `pairGate`'s `expr`/`cond` arms are the scan's
share of the same decision — a position is served in exactly one list, and the condition door and the scan
agree by construction because the scan asks the truth door's own list. The residue is four rows with pinned
bytes, not a footnote: the rebinding family with its stale tag, whose worst spelling never terminates
(**Gap R.366**), the `and`/`or` operand and the ternary arm that are a caller-supplied parameter
(**Gap R.367**), the body emitted under the text- or double-returning convention, whose parameters never get a
tag to ask (**Gap R.368**, which also inherits the `str(v)` pin that rode under Gap R.361 until this commit),
and the empty container spelled as a CALL whose entry store never asked ADR 0391's door (**Gap R.369**).
