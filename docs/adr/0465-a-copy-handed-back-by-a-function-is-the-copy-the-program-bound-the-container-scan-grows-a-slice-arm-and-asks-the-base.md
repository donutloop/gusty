# ADR 0465 — a copy handed back by a function IS the copy the program bound: the container scan grows a slice arm, and asks the base

**Status:** Accepted
**Date:** 2026-10-30
**Decides:** roadmap **Gap R.238** (a slice handed back by a function answers one word)
**Files:** **Gap R.551** (one call read at two positions in a statement loses the concatenation — measured
identical on this row's parent for a body that forwards a literal answer) and **Gap R.552** (a slice of a
PARAMETER handed back — the record the copy's kind needs lives in the caller's frame)
**Keeps open:** **Gap R.237**, **Gap R.239**, **Gap R.240**, **Gap R.243**, **Gap R.261**, **Gap R.324**
**Related:** ADR 0333 (a container handed back by a call is a value), ADR 0335 (the copy is an object with the
base's tags), ADR 0330/ADR 0294 (`rt_slice` copies the slots WITH their tags), ADR 0230 (Gap R.230's binding
road for a copy), ADR 0344 (the two readers of an answer, and which one may read a slot), ADR 0232 (one question
asked once), ADR 0273 (an exit-0 wrong value is the defect this repo files rather than ships), ADR 0166, ADR 0302

## The rule

**A container the run time built is a value wherever it is handed over — binding it to a name and returning it
from a function are the same object arriving at two doors, and the scan that names it must have an arm for every
door or one of them prints a word.**

## Context

Three lines, and the answer was a digit:

```
$ gustyc --eval 'def n():
>     xs = [1, 2, 3]
>     return xs[1:3]
> print(n())'
1
$ python3 -c '...'
[2, 3]
```

at the **exit code of success**. `3` came out over a base whose slots mix kinds — the copy's heap handle, wearing
the program's answer. And the same call's answer BOUND to a name printed `[2, 3]` all along:

| program | the reference | this backend, before |
|---|---|---|
| `ys = n()` / `print(ys)` | `[2, 3]` | `[2, 3]` ✓ |
| `print(n())` | `[2, 3]` | **`1`** |
| `print(n())` over `[1.5, "a", 3]` | `[1.5, 'a']` | **`2`** |
| `for v in n(): print(v)` | `2` `3` | a single **`0`** |
| `print(len(n()))` | `2` | refused |
| `print(n() == [2, 3])` | `True` | refused |

That asymmetry is the whole diagnosis. `ys = xs[0:2]` had been a described list since **Gap R.230** —
`recordContainerSlice` settles the kind and asks the base whether its slots carry tags — and `return build()` had
been one since **ADR 0333**, which is the pass that decides, program-wide, which functions hand back a container.
The scan read a `return` of a literal, a comprehension and a `list()`. It had no arm for a `return` of a **slice**,
so `n` was not in the table at all, every door that asks the table fell back to the ordinary single-word road, and
the word was what `rt_slice` returns: an allocator handle. `print` printed it as a number.

## The decision

Three arms in one file, `pkg/lang/container_return.go`, plus the truth door in the builtin road:

1. **`containerKindOfIn`'s `ask` gains `case *Slice:`** — and the kind is asked of the BASE and nowhere else:
   a copy is a list exactly when what it was cut from is a list. A set, a dict or a text answers nothing (the
   reference raises on the first two, and the third is not a container), and a base the scan cannot name leaves the
   body on the roads it always had, refusal included. This is `recordContainerSlice`'s one judgement, not a second
   opinion written beside it (**ADR 0232**).
2. **`containerAnswerSlotReader.slot` gains `case *Slice:`**, and recurses into the base — the copy's slots ARE the
   base's slots, copied with their tags by `rt_slice` (**ADR 0294**). This is the difference between the print door
   answering and everything else refusing: `takesRead` sends an index, a comparison, a builtin argument and a loop
   to the described answer alone (**ADR 0344**), and a copy with no notebooks is an answer those doors may not read.
   The same case covers `case *Call:` — a copy cut from a CALL's answer has the callee's slots, and the permission
   is the one the table already grants a forwarding `return build()`: the callee is in the table's own described
   set, and the arguments are values this body can write down.
3. **`containerAnswerCarriesTags` asks a slice arm with `containerSlotsCarryTags`, not with the `return true` that
   is right for a name.** A copy of an untagged base has no tag array to read; claiming tags that nobody wrote is
   how `[1, "a"]` printed `[1, 0]` (**Gap R.67**), and claiming none for a tagged one prints an interned index
   where the reference prints the text. So the claim is imported from the writer, which is the predicate the copy's
   own builder asked.
4. **`bool(x)` asks a container answer the condition's question.** The truthiness of a container is its length, and
   `if build():` already asks it through `containerReadShape`; the builtin is the same question spelled as a call, so
   `builtinBool` now asks `containerAnswerOperandRead(..., containerReadShape)` and hands the name to the same truth
   road. It joins this ADR because it is the row's own risk made concrete: with the scan arm alone, `bool(n())`
   over an EMPTY copy compiled to `icmp ne i32 %handle, 0` and answered **`True`** where CPython answers `False` —
   and once the answer table can name the container, the one-word position would have refused instead, costing a
   program that printed the right bytes for the non-empty case. **ADR 0273**'s rule — a gate may not cost a program
   that compiles today — is what made that a fix in the same commit rather than a filing.

## What the arms buy, measured

Every row is the reference's own bytes, and every one of them is in the suite — 23 answered rows in
`pkg/lang/slice_bounds_kind_test.go`'s `sliceBoundsAnswered` (asked of the compiler, the reference, the module
verifier AND the record by four loops over one table), 11 at the CLI against a live `python3` in
`integration/slice_bounds_test.go`, a `match` row of the conformance matrix, and the `bool` pair moved into the
answered table of `pkg/lang/lambda_answer_test.go` out of the refusal table:

```
print(n())                                  [2, 3]          was 1
print(n())      # base [1.5,"a",[7]]        [1.5, 'a']      was 3
for v in n(): print(v)                      2 3             was a single 0
print(len(n()))                             2               was refused
print(n() == [2, 3])                        True            was refused
print(n()[0], n() + [4], n() == [2,3])      2 [2,3,4] True  was refused / wrong
print(none())   # xs[5:9]                   []              was 1
print(bool(n()))                            True            was True (by luck)
print(bool(none()))                         False           was True — a handle is never zero
print(head(), tail(), stride())             [1,2] [3,4] [1,3]
print(copy_of_text())                       ab              a text's copy stays a TEXT
print(cut(), len(cut()), cut()[0], cut() == [7,8])   [7, 8] 2 7 True   (base is a call's answer)
```

Two neighbours are pinned as **filed, not fixed**, each with the answer this backend gives today beside the
reference's, so the day a door opens the row fails rather than rots:

- **Gap R.551** — `print(n() + [4], bool(n()), str(n()))` writes `[2, 3] True [2, 3]`: the `+` door is handed the
  operand a DIFFERENT read of the same call materialised, so the concatenation is lost. The same bytes come out of
  this row's parent for a body that forwards a literal answer (`def b(): return b2()`), which is what makes it a
  neighbour and not this row's defect — and why it is filed with its measurement rather than fixed beside it.
- **Gap R.552** — `def n(xs): return xs[0:2]` / `print(n([1,2,3]))` writes `1`: the scan asks what the BODY bound a
  name to, and a parameter's object is the caller's, so the record a copy's kind needs is not in this frame. That is
  **Gap R.154**'s pair-parameter question one construct out; byte-identical on this row's parent.

The refusals that stayed refusals are the honest ones: a slice of a dict, a slice of a set, and a copy of a base
the records do not describe (a module name read inside a `def`) all refuse with the sentence that names the missing
record, byte-identical on the parent build and this one.

## Why not the alternatives

- **Materialise at the `ret` instead of the call site** — the callee would have to know every reader's question, and
  the answer table exists precisely so the `define` and its `call`s agree without either guessing (**ADR 0276**).
- **Answer `print(n())` by making the print door accept an undescribed handle** — that is the defect with a friendlier
  error message: `takesRead` refuses it because the printer that reads a word prints the allocator's index
  (**Gap R.261**).
- **Reuse `recordContainerSlice` at the return** — that road records what a NAME the statement bound; a `return`
  binds no name in this frame, and inventing one would put a second notebook beside the scan's, which is the drift
  **ADR 0232** forbids.
- **Fold the copy away when the base is a literal** — the fold has its own door (`TestTheFoldStillOwnsTheSliceThatFolds`)
  and this one must not steal it: the programs above bind their base precisely so the copy is real.
- **Leave `bool(n())` refusing** — ADR 0273's ranking, again: a refusal that costs a working program is a regression
  wearing a diagnostic.

## Consequences

- `docs/language.md` says a slice of a list is a list wherever it lands — bound, printed, looped, measured,
  compared, or handed back by a `return`.
- The record grew by **27 entries** (the family, the `bool` pair, the copy-of-a-call's-answer rows, and the corpus
  program itself), every one of them written only where CPython answered first and the compiled leg agreed second —
  `tools/recmerge` refuses an entry the compiled leg will not sign, so no row in the record is self-certifying
  (**ADR 0302**'s rule, and the reason a missing record fails a case rather than skipping it). The artifact
  counts in `AGENTS.md`, `docs/operations.md`, `README.md` and `roadmap.md`'s snapshot are recomputed by their
  guards (**Gap R.204**), not by this ADR's memory: the file holds **10714** sources.
- `containerAnswerSlotReader` is now asked by three doors rather than two — the answer table, the loop's key
  question, and the copy of a call's answer. One reader, three askers, is the point: the next container the run
  time builds (a sorted copy, a concatenated copy, **Gap R.300**'s set built by insertion) adds an arm to the same
  three tables and nothing else.
