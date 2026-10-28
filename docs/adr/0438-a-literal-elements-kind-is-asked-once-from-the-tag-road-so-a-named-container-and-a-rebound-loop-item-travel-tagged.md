# ADR 0438 — a literal element's kind is asked once, from the tag road: a container the program NAMED travels as a tagged pair, and a loop item is its item rather than its name's history

## Status

Accepted, 2026-10-28. Closes **roadmap Gap R.486** (filed one commit earlier by **ADR 0437**), closes **Gap
R.439** (filed 2026-10-24 by **ADR 0421**, on this ADR's own prescription), and files-and-pays **Gap R.489**
(the stale container tag on a rebound loop variable), which this cycle found by landing the first fix and
could not be left unfixed.

Read after **ADR 0232** (the slots the builder wrote are the truth about the object), **ADR 0359** (the same
one-line-of-a-gate fix, made for the interpolation element), **ADR 0187**/**ADR 0189** (payload and tag are
written together; nested containers are slots the tag routes), **ADR 0239** (the static array the folds read),
**ADR 0233**/**Gap R.358**/**Gap R.440** (the unroll's per-item record), **ADR 0421** (the gate arm this widens
one further along), **ADR 0166** (exit 2 is a compiler bug, and a refusal is a sentence), **ADR 0424**.

## Context

Two programs, one door. Both printed the allocator's slot numbers at the exit code that means the program ran.

```gusty
xs = [1]
print([xs, 2])        #  reference [[1], 2]        this backend [0, 2]        Gap R.439 / Gap R.486
```

```gusty
xs = [7]
for xs in [1, 2]:
    print([xs, 1.5])  #  reference [1, 1.5]         this backend [, 1.5]      Gap R.489
                      #                             [2, 1.5]                  (invisible before this ADR)
```

A slot of a compiled container is a pair: one word and one tag that says what the word is. `heapListFrom`
writes both halves, and until this commit it asked the two halves **different questions**.

| what | where it was asked | what it asked | answer for `print([xs, 2])` |
|---|---|---|---|
| the slot's **word** | `heapElemKind` | is the element a container the builder can *see* (a literal)? | no — it is a `*Name`; the handle went in as an ordinary value (which happens to be right) |
| the slot's **tag** | `elemTagFor` → `elemKindTag` | what kind is the element? consulting the loop item, then the container tables | `TagList` — **already correct** |
| the object's **header** | `literalNeedsTags` / `literalMixedKinds` | does any element's *spelling* need a tag? | no — a name has no spelling, so bit 8 was never set |

The pair was right and the header was wrong, and the header is what the printer reads: `rt_print_list` (the
single-kind printer) printed the handle as an integer. The emitted IR at `84a552c` said, in so many lines:

```
  call void @rt_tag_elem(i32 %t2, i32 0, i32 5)      ; TagList — the tag road knew
  call void @rt_set_elem(i32 %t2, i32 0, i32 %t4)    ; the handle, in a register
  call void @rt_print_list(i32 %t2, i32 0)           ; …and the printer that never looks at the tag
```

**This is the third time this road was patched one question at a time.** **ADR 0359** fixed the same gate for
the interpolation element — "`f"a{n}"` is a text to the builder (`heapElemKind` interns it) and a non-text to
the pre-scan, so `[f"a{n}", 1]` was labelled bit 1 and answered `['a3', '3']`" — and its comment already
named the rule (**ADR 0232**: the slots are the truth) without the census being made to obey it for containers.
**Gap R.439**'s own `Free text` prescribed this fix almost verbatim ("give the gate the container-name arm
`elemKindTag` already has").

The second program was invisible until the first was fixed. `elemKindTag`'s *Name arm asks
`loopItemKindTag` first and the container tables second — but `loopItemKindTag` answers only for a kind it can
name (`slotTagSelfDescribing`), and a plain integer is not one, so for an int loop item the reading fell
through to the **cumulative** tables, where `xs = [7]` had enrolled `xs` in `listVars` for the rest of the
module. The slot stored the integer 1 tagged `TagList`. With the census now honest, the mixed printer honoured
that tag and printed nothing at all: `[, 1.5]`. So the census fix *required* the tag fix, and the tag fix was
measured on the build before it (`84a552c`, where `print([xs, 1.5])` printed `[, 1.5]` too — the wrongness
predates this ADR; only its visibility did not).

## Decision

**1 — The census asks the tag road's answer, not the element's spelling.** `literalNeedsTags`'
`slotsItself` gained `g.elementNameHoldsContainer(x)`, which is:

```go
t, ok := g.elemKindTag(nm)
return ok && (t == TagList || t == TagDict || t == TagSet)
```

One question, asked of the same function that writes the tag, so the header can no longer disagree with the
slots about what the slots hold. A numeric literal, a text and every element the records still describe are
untouched: no tag, no bit 8, `rt_print_list`, and `sum`/`min`/`max` keep **ADR 0239**'s static array.

**2 — A name the iteration rebound is its item.** `elemKindTag`'s *Name arm now asks
`loopItemExpr(nm)` before the container tables and reads the tag of the ITEM (`elemKindTag(item)`), falling
through only when the item's own kind cannot be asked. This is the per-iteration record **ADR 0233** and **Gap
R.358** already keep, asked at the one road that was not asking it.

**3 — Where a pinned row stops being wrong, it moves.** The two `Gap R.486` rows left
`container_element_and_member_test.go` (whose element door is now the comprehension half, **Gap R.487**) and
the two `Gap R.439` rows left `void_slot_literal_test.go`; all four now answer in
`pkg/lang/named_container_element_test.go` and `integration/named_container_element_test.go` — compiled leg,
record leg (`HasGoldenAnswer` + `runGoldenStdout`, so a missing record fails), CLI leg against a live CPython
with the exit code asserted, and the IR. `tools/recmerge` wrote 19 entries from the reference with the
compiled leg cross-checked (**9886 → 9905**, none refused): a row going from pinned to recorded is the
transition this suite is built to make.

**4 — What this ADR does not claim.** `print([[{"a": 1} for x in range(1)], 1])` still prints `[1, 1]`
(**Gap R.487**, the comprehension element — a different road into the same slot), and
`print([[[x for x in [1]]]])` still hands `llc` an `i32 @.lst1` operand and exits **2** (**Gap R.488**). Both
stay filed with their bytes pinned; the IR rows here deny `@.lst` in an operand position for every program
this ADR answers, so the fixed half cannot slide into the broken half unnoticed.

## Consequences

- The emitter change is two predicates in `pkg/lang/heapargs.go`: `literalNeedsTags`'s `slotsItself` (+1 arm)
  and `elemKindTag`'s *Name branch (+ the item reading). No runtime function, no tag, no ABI was added; the IR
  difference is the census call (`rt_mark_estr(_, 8)`) and the printer choice, plus one written tag.
- Objects that claim per-element tags are now more common, which is the honest direction and the expensive
  one: `rt_print_list_mixed` walks two words per slot. The numeric-literal IR row is the guard that keeps the
  fold road's static array from being paid for it.
- `docs/language.md` and `docs/operations.md` said "a container the program named prints its slot" as live
  behaviour; both now describe the answered shape, and the four filed rows that named these bytes were
  deleted rather than edited.
- The record grows to 9905 sources; `AGENTS.md`, `docs/operations.md`'s artifact table and `roadmap.md`'s
  Snapshot were moved to the file's own number by the guard that recomputes it (**Gap R.204**), which is the
  only reason the three stayed in step.

## Alternatives rejected

- **Set bit 8 at the print call.** Rejected outright: the object, not the call, decides how its slots are
  read, and `str()`, `repr()`, `==`, `in` and a `for` over the literal all read the same header. A print-time
  fix would leave `[xs, 1][0]` and `[xs] == [[1, 2]]` wrong while `print([xs, 1])` looked right — the exact
  shape of half-fix this file exists to record.
- **Widen `isContainerLiteral` to names.** Rejected: that predicate is asked all over the emitter by roads
  that mean "a literal I can lay out at compile time" (the static array, the fold gates, the copy door). The
  census needed a narrower question, and the narrow question is the one the tag already answers.
- **Unenrol a name from the container tables when a `for` rebinds it.** Rejected: the tables are
  program-scoped facts used by roads that run before the loop is emitted (the fold gates, the copy door, the
  escape census), and a name that WAS a list is still a list to those questions. What was wrong is not that
  the table remembers, it is that the table got the last word about an element the iteration owns.
- **Refuse a container element whose tag cannot be settled.** Rejected: the tag IS settled — the census was
  the missing half — and refusing would retire programs that answer today, in a language whose rule is that a
  refusal names a missing lowering (**ADR 0249**, **ADR 0166**).
- **Land the census fix and file the loop-item defect for a later cycle.** Rejected: it is how a wrong answer
  gets a green suite. The census fix silently turned `print([xs, 1.5])`'s existing wrongness visible; shipping
  without the tag fix would have made the suite redder and the language no better, and the loop would have
  filed a regression against itself.

## Note on this cycle's method (tool-trust)

Every byte above was read from two builds in this session — the pre-change `84a552c` binary and the post-change
one — beside a live `python3`, and the `[, 1.5]` reading of the pre-change build is what proves **Gap R.489**
predated this commit rather than being written by it. The regression check that mattered was not the new tests
(a row that starts passing proves nothing on its own) but the 27-program sweep run against both builds: five
mismatches against CPython, all identical on both builds, and so filed-or-known gaps rather than damage —
including `print([xs + [3]])` answering `[3]`, which this sweep found and which stays named
(`Gap R.487`'s neighbourhood).
