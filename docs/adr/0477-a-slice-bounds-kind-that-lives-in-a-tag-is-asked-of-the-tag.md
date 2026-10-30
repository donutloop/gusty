# ADR 0477 — a slice bound whose kind lives in a tag is asked of the tag, and a bound the program wrote is run before it is refused

**Status:** Accepted
**Date:** 2026-11-03
**Decides:** roadmap **Gap R.237** (a bound whose kind lives in a slot, in a call's answer, in a class's field or in what the CALLER handed a parameter is read as a position — `ys = ["a"]` / `print(xs[ys[0]:2])` answered `[1, 2]` at **exit 0** where the reference raises `TypeError: slice indices must be integers or None or have an __index__ method`, and the float spelling reached `llc` as a `double` in an `i32` argument and left through **exit 2**). Filed 2026-10-14 by **ADR 0336** as the half its static gate could not see.
**Files:** nothing new — the three shapes the row named are answered, and the residues the measurement found beside them belong to rows that already exist (**Gap R.243** a text slice whose step is a value, **Gap R.150**/**Gap R.151** a function name used as a value, **Gap R.146** a pair-valued loop element in a one-word position), each asserted as a refusal rather than silently absorbed.
**Keeps open:** **L11.1** (the tagged value word all of this is a workaround for), **Gap R.82** (the same one-word position for a slot used arithmetically), **Gap R.243**, **Gap R.402**, **Gap R.567**
**Related:** ADR 0336 (the static half of this door, and the order it settled: bounds are asked before the container is consulted), ADR 0337 (the step's own value question, which this row had to stop accusing), ADR 0335 / ADR 0333 / ADR 0334 (the slice as one road, the container answer, the index door), ADR 0232 (one question asked once — the legal-tag table lives in one helper, not in each road), ADR 0247 / ADR 0267 / ADR 0368 / ADR 0474 (the pair roads this row asks rather than a fourth door), ADR 0273 (a door that opens may not speak over the road that named what it could not carry), ADR 0224 / ADR 0229 (a text's word is an `@str_tab` index; the slot is the truth), ADR 0166 (**exit 2** is this compiler's bug; a plausible list at **exit 0** is worse), ADR 0214 (a trap stays the program's, catchable and reported by the program's own `except`), ADR 0192 (a fix reaches the position that asked), ADR 0302 (the two witness legs)

## The rule

**A bound is asked what it is where its kind actually is.** A kind written in the source is answered by the
compiler (**ADR 0336**); a kind that lives in an object's tag, in the word a callee stored beside its `ret`, in a
parameter the caller filled, or in a field a class wrote is answered by **that tag**, through the pair roads the
print, comparison, binding and membership doors already ask — and one helper holds the legal-tag set for all three
bounds of both slice roads. A bound the compiler cannot see into is not a bound the compiler may kind.

The rule has a second half, and it is about *when* the door speaks:

**A bound that is an expression the program wrote runs before the slice is refused.** The reference builds a slice
object from the three values and only then asks whether they are positions; a bound that is a call has therefore
already run — printed, mutated, opened — by the time the `TypeError` arrives. Refusing the shape at compile time
prints less than the reference does, and stdout is part of the answer.

## Context

```
$ gustyc --eval 'xs = [1, 2]
  ys = ["a"]
  print(xs[ys[0]:2])'
[1, 2]                                   # CPython: TypeError: slice indices must be integers or None or have an __index__ method

$ gustyc --eval 'xs = [1, 2, 3]
  def g():
      return 1.5
  print(xs[g():2])'
gustyc: jit: codegen: cannot …           # exit 2 territory: a `double` where rt_slice's i32 argument lives

$ gustyc --eval 'def at(k):
      xs = [1, 2, 3]
      return xs[0:k]
  print(at(1.5))'
1                                        # CPython: the same TypeError; `1` is the copy's heap handle

$ gustyc --eval 'xs = [1, 2, 3]
  ys = [None]
  print(xs[0:ys[0]])'
[]                                       # CPython: [1, 2, 3] — None is the side the program did not write
```

**ADR 0336**'s gate asks a bound what it IS, and it can answer where the SOURCE wrote the kind: `xs["a":2]`,
`xs[1.5:2]`, `xs[i:2]` over `i = "a"`. Where the kind is not in the source the gate stays silent — which is the
correct answer for a name the records describe as an integer, and the whole defect for a bound whose kind the
compiler never held. A slot read's kind lives in the object's tag, a call's answer in the word the callee stored
beside its `ret`, a caller-chosen parameter in the pair the call convention carries, a class field in the census
that wrote it. Each of those four was lowered through the generic expression door and handed to `rt_slice` as a
position: a text's `@str_tab` index counted as slot 0, a float box handle as slot 1, and `None` — the absence of a
bound — as the position 0.

The float spelling is the expensive one: `rt_slice`'s arguments are `i32`, and a `double` in one of them is `llc`
refusing the module, which **ADR 0166** counts as this compiler's own bug (**exit 2**), paid for a program whose
only fault is writing `xs[g():2]`.

## Decision

**Ask the pair roads, then the tag; keep the ordinary road for everything the compiler already kinds.** Three
pieces, none of them a new value representation:

1. **`sliceBoundPair`** (`pkg/lang/slice_bound_pair.go`) asks the bound's `(payload, tag)` words of the doors that
   already answer them — the numeric pair registers a name's family bound, the tagged loop variable read,
   `answerCallPair` for a call whose answer the answer direction carries, and `runtimeSlotPairDeep` for a slot of
   an object the run time built. It is asked in the render door's order and for its reason (a name the arithmetic
   door bound is asked before the container probe, whose number-shaped read would refuse first), and it declines
   anything whose kind the compiler already names, so `xs[1:k]` over `k = 1` emits no test at all and keeps the road
   it always took (**ADR 0273**).
2. **`rt_bound_is_position`** (`pkg/lang/codegen_ir_slice.go`) reads the tag and answers the reference's *three*
   verdicts, because the reference words both legal answers in one sentence: `1` for a word that counts (int, or
   bool — `TagInt`/`TagBool`), `0` for the absence of a bound (`None`), `-1` for everything else. A boolean would
   have been enough to raise and is not enough to *not* raise: the verdict that says `None` is what turns
   `rt_slice`'s presence flag off, and what turns a `None` step into the stride 1 — without which **ADR 0337**'s
   zero-step question accuses a program of a step it never wrote. One helper, both slice roads, all three bounds: a
   legality table copied per road is how the roads stop agreeing (**ADR 0232**).
3. **The static gate gains the kinds it could not see** — the kind a call answers (`staticContainerKind`, the text
   census, the float census), the kind a caller hands a parameter (`floatParamNames`), and the kind a class field
   holds (`strAttrs`, the census the truth road already asks). It does not gain a fourth census of callables or a
   per-road copy of any of them.

**And the gate lowers the bound it refuses when that bound is a call.** The reference evaluates the three bound
expressions, builds the slice, and only then refuses it. So a bound that is a call to a body the text or float
tables own is lowered first — its effects reach stdout — and the raise follows. The word it answers is discarded,
because nothing else may read it: a text's word is an interned index and a float's a box handle. A bound that is a
*literal*, a slot or a container display is still refused unread, because lowering a container-valued bound is the
one thing this door must never do: `rt_slice`'s i32 argument would carry a `@.lstN` global address and the module
would not verify — the exit-2 shape **ADR 0336** retired, and the reason the lowering is limited to the answers that
lower as one honest word.

**Refusals stayed refusals.** Three shapes the measurement touched are not this row's and are asserted as what they
are: `print("abcd"[::k])` (a text's stepped slice with a value step, **Gap R.243**), `xs[0:g]` for a function name
used as a value (**Gap R.150**/**Gap R.151**), and `for x in xs[0:k]` where the loop element is a pair
(**Gap R.146**). One of them has a shape this row brushes against and does not take: `ys = ["a"]` /
`print(s[::ys[0]])` over a text still refuses with the road's own `cannot fold string slice`, where the reference
raises the slice-indices `TypeError` — because `rt_str_slice` takes a low, a high and no stride, the road cannot
build a stepped text at all, and a door that "answered" by raising on every non-`None` tag would be refusing the
integers it cannot answer either. That is **Gap R.243**'s, and the guard emitted before it means the day the stride
road lands, the kind question has already been asked. None of these regressed; none was quietly widened to keep a
table green.

## Consequences

- The reference's sentence is now the answer of **50 raise rows** in `pkg/lang/slice_bounds_kind_test.go` (class
  **and** message asserted, **exit 2** forbidden) — 22 of them newly reached through a tag, a call's answer, a
  caller-chosen parameter and a class field — of **17 new answered rows** asked of the compiler, of CPython and of
  the record leg (71 answered shapes in the table, every one of them module-verified by `llc`), of **8 catchable
  rows** taken by the program's own `except` (four of them the dynamic shapes, one of which proves a `TypeError`
  arm is not reached by a `ValueError` arm written before it), and of **3 order rows** that assert the callee
  printed before the raise. The three shapes **Gap R.237** filed are deleted from
  `TestWhatTheBoundDoorStillCannotReachIsFiledNotFixed` by their own text, which leaves 4 rows in that file.
- The record leg grows **18 entries** (11117 → 11135): the 17 answered sources above plus the conformance program
  itself, every one written by `tools/recmerge` only where CPython and the compiled leg agreed.
- `badSliceBoundGate` returns `(raised, error)`: a `true` now means "a raise is written here; answer nothing and
  emit nothing after it", and the `error` is the compiler's own failure to lower a bound, not the program's
  exception. Two call sites (`valueSlice`, `valueTextSlice`) carry it, which is the price of the door being one road
  (**ADR 0335**).
- A slice module that carries the guard carries a compare, a raise block and a continuation beside every
  `rt_slice` call whose bound is a value; the fold (`print("abcdef"[1:3])`) and the constant-bounded copy still emit
  none of it — `TestTheFoldStillOwnsTheSliceThatFolds` and `TestTheBoundGateLeavesTheUntaggedCopyAlone` are the
  negative controls that keep the guard from being emitted unconditionally.

## Alternatives rejected

- **Widen the static gate to slots and calls by guessing from the last binding.** The records are per-name and
  per-body; a slot's kind is the object's, a caller's argument is not in the body, and a name rebound on one path is
  two kinds. This is the same refusal **ADR 0476** made about a fold that defaults.
- **Have `rt_slice`/`rt_str_slice` raise on a bad bound.** A helper that raises owns exception state the codegen
  cannot see and produces a trap the program cannot catch (**ADR 0214**); a helper that returns an empty copy is a
  wrong answer wearing the shape of a fix (**ADR 0166**).
- **A boolean verdict (`is_position?`).** Not enough to *not* raise: the door must distinguish an absent bound from a
  position of 0, and the absence is exactly what the tag can say and the source cannot.
- **Refuse every dynamic bound at compile time** (the cheapest green). It spends **exit 1** on programs that answer
  today (`xs[1:k]` over `k = 1`), and **exit 1** on a program the reference merely runs is the expensive direction to
  be wrong.
- **A fourth "slice bound kind" census** beside `strFuncs`/`floatFuncs`/`staticContainerKind`. Two answers to one
  question is the defect class this loop keeps paying (**ADR 0232**).
- **Running the effects for every statically-refused bound, containers included.** Lowering a container answer into
  a one-word position is **Gap R.67**/**ADR 0333**'s refusal; the gate must not spend **exit 1** on a program the
  reference stops with a `TypeError`.
