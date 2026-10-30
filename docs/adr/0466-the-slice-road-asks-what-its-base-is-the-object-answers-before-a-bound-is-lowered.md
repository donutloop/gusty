# ADR 0466 — the slice road asks what its BASE is: the object answers before a bound is lowered

**Status:** Accepted
**Date:** 2026-10-31
**Decides:** roadmap **Gap R.234** (the slice road never asked what kind the base is)
**Files:** **Gap R.554** (a base spelled by a constructor has no kind record, so the gate declines and the road keeps its refusal), **Gap R.555** (a dict sliced by bounds the compiler cannot read — the miss needs a run-time slice key), **Gap R.556** (the word a slice-base raise hands back carries no kind, so a read of it refuses), **Gap R.557** (a ternary with a SET arm puts the literal's global in a value position again — **Gap R.128**'s class, reopened for the arm ADR 0405 did not reach), **Gap R.558** (a slice is not an assignment target, so `d[0:1] = 3` is a parse error where the reference stores under the slice key)
**Keeps open:** **Gap R.217**, **Gap R.235**, **Gap R.237**, **Gap R.256** (the subscript twin: `print(s[0])` over a set), **Gap R.540**
**Related:** ADR 0335 (the copy's gate, which turned these shapes' exit 2 into a refusal), ADR 0336 (the bounds door, whose sentence this row must not borrow), ADR 0337 (a step of zero), ADR 0328 (the hashing door: a key is asked whether it can be hashed before the dict is asked whether it holds it), ADR 0329 (Gap R.222's gate, the shape this one copies), ADR 0361 (a tuple's repr is the key's repr), Gap R.189 (a KeyError names its key), ADR 0232 (one question asked once, and a kind claim imported from a writer), ADR 0273 (an exit-0 wrong value is the defect this repo files rather than ships; a gate may not cost a program that compiles today), ADR 0166 (exit 2 is our bug), ADR 0211 (the exit-code contract), ADR 0302 (the two witness legs), ADR 0406 (`str(err)` is the raise's arg)

## The rule

**A subscript's complaint belongs to the object being subscripted, and the object is asked first.** A slice is a
read of something that HAS positions; before a bound is lowered, the road asks what its base is, and a base with
no positions gets the object's own sentence — never the bounds' sentence, never the copy helper's argument slot.

## Context

Four programs, and the toolchain had four stories for them:

```
$ gustyc --eval 'print({1, 2}[0:1])'   # was: llc rejects the module — exit 2
$ gustyc --eval 'print({1: 2}[0:1])'   # was: the same refusal
$ gustyc --eval 's = {1, 2}            # was: prints [1]  at exit 0  (before ADR 0335's gate)
               print(s[0:1])'
$ gustyc --eval 'd = {1: 2}            # was: prints [1]  at exit 0  (before ADR 0335's gate)
               print(d[0:1])'
```

against:

```
$ python3 -c 'print({1, 2}[0:1])'   # TypeError: 'set' object is not subscriptable
$ python3 -c 'print({1: 2}[0:1])'   # KeyError: slice(0, 1, None)
```

`rt_slice` walks a heap list and copies its slots with their tags (**ADR 0294**, **ADR 0330**). The road that
calls it had asked everything about the bounds and nothing about the base: for a **literal** it handed the
literal's module-level global (`@.set1`, `@.dict1`) to a parameter whose type is an i32 register and `llc`
turned the module down — **exit 2**, the code **ADR 0166** reserves for a compiler bug; for a base in a **name**
it copied the words and the program printed `[1]`, a list of a set, at the exit code of success. **ADR 0335**
stopped all four with a gate that names the missing record (`sliceBaseIsDescribed`), which was the honest side
of the line: nothing fabricated, nothing handed to `llc`. A refusal is not the reference's behaviour either. The
reference answers, and it answers with the object.

The interesting half of the row is not the two sentences; it is their **order**, which the row only discovers
when it tries to write them beside the door **ADR 0336** already built:

| program | the reference | what the door beside this one says |
|---|---|---|
| `print([1, 2]["a":"b"])` | `TypeError: slice indices must be integers or None or have an __index__ method` | ADR 0336's bounds door owns it, and keeps it |
| `print({1, 2}["a":"b"])` | `TypeError: 'set' object is not subscriptable` | — a set has nothing to count WITH, so the bounds question is never asked |
| `print({1: 2}["a":"b"])` | `KeyError: slice('a', 'b', None)` | — a dict hashes the slice; a text is hashable, so the miss is the answer |
| `print({1: 2}[[1]:2])` | `TypeError: unhashable type: 'list'` | **ADR 0328**: hashing is asked before the lookup, so the bound that cannot be hashed owns the sentence |
| `print({1: 2}[0:1:0])` | `KeyError: slice(0, 1, 0)` | — and **not** **ADR 0337**'s step-of-zero complaint: the step is hashed, never applied |

A door that asked the bounds first would say `slice indices must be integers …` about a set — a sentence about a
container that counts, aimed at a container that does not — and a door that asked the dict's lookup first would
report a *missing key* for a key the reference refuses to hash at all. **ADR 0328** already states that rule for
the subscript door ("the key's kind is settled before anyone is told a key is missing"); this row pays it for
the third member of the family.

## The decision

`pkg/lang/slice_base_kind.go` adds one gate, called at the top of `valueSlice` (`pkg/lang/codegen_index.go`)
before any bound is lowered, and it is the ONLY place a slice road decides anything about the base:

```go
if g.sliceRaisesWhatTheBaseIs(b, n) {
    return "0", nil
}
```

1. **The kind is read, never inferred.** `staticContainerKind` — the predicate the comparison, the membership
   test and the loop already decide with — answers a kind only where a record names what the name holds. A
   literal answers from its own node; a name answers from the binding that wrote it, so
   `d = {1: 2}` / `d = [1, 2]` / `print(d[0:1])` slices a LIST, and `def m(): return {1, 2}` answers from the
   answer record (**ADR 0333**). Where no record names a kind the gate declines and the road keeps the refusal it
   had: a gate may not name a kind nobody wrote (**ADR 0232**), and it may not cost a program that compiles today
   (**ADR 0273**). This is why `print(set([1, 2])[0:1])` still refuses (**Gap R.554**) — `set(...)` is a call with
   no answer record — and why it is a row rather than an exception in the gate.
2. **A set gets the object's sentence**, `notSubscriptableSentence("set")` — the same string the hashing door
   quotes, so whichever road reads a subscript, one program gets one message (**ADR 0232**'s one-question-once).
3. **A dict gets its own miss, and hashing comes first.** `unhashableSliceBound` asks the three bounds in the
   order the reference hashes them — low, high, step — and the first one the language refuses to hash owns the
   sentence (`d[[1]:2]` says `'list'`, `d[(1, [2]):1]` says `'list'` too, because the tuple hash walks left to
   right). Only then is the key rendered: `sliceAsDictKey` writes the repr the KeyError names, with an omitted
   bound and a `None` bound both spelled `None`, a bool spelled `True`/`False`, a text quoted the way `repr`
   quotes it, and a parenthesised bound written with the tuple's commas (**ADR 0361**, **Gap R.189**). The
   renderer reads the same notebooks `keyNotFoundMessage` consults, so the two doors that name a missing key
   cannot disagree.
4. **A raise, emitted where the read was.** Both arms go through `raiseTo` — the same non-local road every trap
   in this backend takes — so `except TypeError:` and `except KeyError:` take them, an uncaught one exits **3**
   with the reference's own two lines, and `except … as err` binds the key's repr without the caption
   (**ADR 0406**). No arm emits `rt_slice`: the module is asserted not to build a copy of a base the language
   refuses to slice.

## What the gate buys, measured

| shape | the reference | parent | this commit |
|---|---|---|---|
| `print({1, 2}[0:1])` | `TypeError: 'set' object is not subscriptable` | exit 1, a refusal | exit 3, catchable |
| `print({1: 2}[0:1])` | `KeyError: slice(0, 1, None)` | exit 1, a refusal | exit 3, catchable |
| `s = {1, 2}` / `print(s[0:1])` | the set's sentence | exit 1, a refusal | exit 3, catchable |
| `d = {1: 2}` / `print(d[0:1])` | the dict's miss | exit 1, a refusal | exit 3, catchable |
| `print({1, 2}[1.5:2])` | the set's sentence, NOT the bounds' | exit 1 | exit 3, the object's words |
| `print({1: 2}[[1]:2])` | `TypeError: unhashable type: 'list'` | exit 1 | exit 3, hashing's words |
| `print({1: 2}[0:1:0])` | `KeyError: slice(0, 1, 0)` | exit 1 | exit 3 — the step is hashed, not applied |
| `print({1: 2}[(1, "a"):2])` | `KeyError: slice((1, 'a'), 2, None)` | exit 1 | exit 3, the tuple's own repr |
| `d = {1: 2}` / `d = [1, 2]` / `print(d[0:1])` | `[1]` | `[1]` | `[1]` (the record, not the spelling) |
| `xs = [1, 2]` / `print(xs["a":"b"])` | the bounds sentence | same | same (ADR 0336 untouched) |

The witnesses, all three: `pkg/lang/slice_base_kind_test.go` (29 raise rows asked of `jitTrap` AND of
`goldenRunError`, 6 catchable rows asked of the compiler AND the record, the order table asked as three bases over
one bound with a distinctness check so a collapse onto one sentence fails, the rebinding table, the untouched-door
table, and the module asked for the raise's strings, the exception code and the ABSENCE of `rt_slice`);
`integration/slice_base_kind_test.go` (29 raises at the CLI against a live CPython whose `Class: message` line is
read out of the reference per row, 9 handler rows compared byte for byte, and the emitted module checked for a
global in a value position — the shape that made `llc` reject these programs); and the corpus, where
`integration/programs/slice_base_kind.gy` joins `conformanceStandalone` as a parity row (corpus 287 → 288, matrix
273 → 274, parity-asserted 237 → 238, oracle `match` 223 → 224, `fail` 0). `tools/recmerge` wrote **35 entries
(10714 → 10749)**, none refused — every raise row recorded from the reference with the compiled leg cross-checked
against the same sentence, and every handler row recorded from the reference's bytes.

Two rows of `TestAPositionThatCannotSliceRefusesThroughTheCLI` and six rows of the two filed-not-fixed tables in
`pkg/lang/slice_answer_test.go` / `pkg/lang/slice_bounds_kind_test.go` are **deleted by their own text**: the gate
fires, so the refusal assertions fail, which is how this repository knows a door grew rather than rotted. The
emptied slots say where the rows went, and the CLI table keeps the shapes the gate DECLINES — with a sentence each
that names the missing record, at exit 1, never exit 0.

## Why not the alternatives

- **Answer `d[0:1]` as an empty list.** It is what the parent's bound spelling did, and it is ADR 0273's defect
  class: the reference traps, and a program that traps is not a program that prints `[]`.
- **Teach the bounds door to recognise a set.** That leaves three doors guessing: the set arm would live beside
  the float arm, the dict arm would still report a miss for an unhashable key, and the order (object → hashing →
  miss) — the actual content of this row — would be spread over two files that can each be edited without
  contradicting the other. **ADR 0232**'s rule is one question, one place, asked before its dependents.
- **Give `rt_slice` a runtime kind check.** A runtime check cannot produce `KeyError: slice(0, 1, None)`: the
  repr of the key is a compile-time fact about the source, and the sentence must name the key the program WROTE.
  It would also make the raise a runtime abort rather than a catchable exception, costing `except KeyError:` for a
  row whose whole point is that the program may handle it.
- **Fold the raise into ADR 0335's `sliceBaseIsDescribed`.** That predicate answers "can this base be COPIED",
  which is the copy road's question. A set and a dict are precisely bases that can never be copied and yet have
  perfectly good answers — the row's whole discovery is that the gate's question and the reference's question are
  different questions.
- **Leave the four shapes as refusals.** **ADR 0335** earned them that right against exit 2, and this ADR does not
  undo it: a base whose kind no record names is refused still today (three rows above, at exit 1). What changed is
  that a refusal is now reserved for the shapes nobody can name, instead of covering the shapes the source spelled
  out.

## Consequences

- The slice road has two questions in a fixed order — what is the base, then what are the bounds — and the
  second is asked only of a base that has positions. A future arm (a bytes base, a class that defines
  `__getitem__`) enters through the gate, not beside it.
- Three refusals move from "the records do not describe it" to being *owed* with numbers: **Gap R.554** (a set
  spelled `set(...)`), **Gap R.555** (a dict sliced by expressions — the miss needs a `slice` object at run time,
  which is L11.1's tagged value word), **Gap R.556** (the word a raise hands back has no kind, so
  `print({1, 2}[0:1][0])` refuses although the reference never reaches the outer read).
- **Gap R.557** is measured, not fixed: with the mask of the refusal removed, `c = 1` /
  `x = {1, 2} if c else [1, 2]` / `print(x[0:1])` reaches the assembler's complaint
  `global variable reference must have pointer type` at **exit 2** — **Gap R.128**'s class, which **ADR 0405**
  closed for the arms it walked and which stayed open for a SET arm (`print(x)` over the same ternary is exit 2 on
  this commit's parent too, so this is a neighbour rather than a regression). **Gap R.558** files the store:
  `d[0:1] = 3` is a parse error here and a store under the slice key in the reference.
- `pkg/lang/capability.go`'s `lower.unsupported.slice-base-not-described` entry keeps its code and its sample (the
  sample's base is a slot read the records do not describe, and it still classifies); its `Rows` cell now names the
  refusals that remain.
- The record grows by 35 entries to **10749**, and the four places that quote it (`roadmap.md`'s snapshot,
  `docs/operations.md`'s artifact table, `AGENTS.md`, and the artifact guard's literal in
  `pkg/lang/leading_dot_float_test.go`) move together, because the file is the authority and not the memory.
