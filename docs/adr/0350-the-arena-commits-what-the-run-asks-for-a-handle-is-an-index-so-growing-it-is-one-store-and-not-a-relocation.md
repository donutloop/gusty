# ADR 0350 — The arena commits what the run asks for: a handle is an index, so growing it is one store and not a relocation

Status: accepted
Date: 2026-10-15
Amends: [ADR 0347](0347-the-arena-tells-the-program-when-it-is-full-so-exhaustion-is-a-memoryerror-and-not-a-silently-wrong-answer.md), [ADR 0334](0334-a-handle-is-a-value-the-roots-can-name-so-a-container-variable-reads-through-the-same-slot-an-argument-reads-through.md)
Roadmap row: Gap Q.2

## The gap, as measured

ADR 0347 made exhaustion speak — `MemoryError`, catchable, reported — and left the wall exactly where
it had been. `@heap` was `[1024 x {i32, i32, [256 x i32]}]`, and the honest trap it produced was still
a refusal to run a program the reference runs:

```gusty
def make_group(size, base):
    g = []
    i = 0
    while i < size:
        g.append([base + i, (base + i) * 2])
        i = i + 1
    return g

def fill(groups, count, size):
    k = 0
    while k < count:
        groups.append(make_group(size, k * size))
        k = k + 1
    return count

groups = []
fill(groups, 5, 205)
shown = 0
for gr in groups:
    shown = shown + 1
print(shown, len(groups[0]))
```

1025 containers, every one of them reachable from `groups`. CPython prints `5 205`. The compiled
backend printed, at exit 3:

```
MemoryError: out of memory: the compiled heap holds 1024 live containers (roadmap Gap Q.2)
```

The row's own Definition of done had named that program's shape all along — "a 1025-live-container
program that must succeed rather than trap" — and ADR 0347 had said so in its own words: the reference
leg diverges for every program whose live set exceeds the arena, "which is why the growing half is the
row's next action rather than a follow-up nicety." A diagnostic for a wall is worth having; it is not
worth keeping a language behind the wall.

## The decision

**The arena is a reservation, and the run commits from it as it goes.**

1. **Two numbers replace one.** `HeapCapacity` stays 8192 and becomes what the *tables* are declared
   with — the ceiling, the reservation. `HeapInitialCapacity = 1024` is the window a run starts in, and
   `HeapGrowthFactor = 2` is how the window widens. A new global, `@heap_cap`, holds the window this run
   has committed; `rt_alloc` bounds a fresh slot by `@heap_cap`, not by the ceiling. `@heap_grown` counts
   the widenings so the run can say how it got to its size.
2. **Growing is a commit, not a relocation.** When a program asks for a slot the window has not
   committed, and the window is below the reservation, `rt_alloc` doubles the window (clamped to the
   reservation), bumps `@heap_grown`, and hands out the slot. Nothing else happens. That fits in one
   store because **a handle in this runtime is an index into parallel arrays, not a pointer** (ADR 0334):
   no root, no slot word, no tag array holds an address that a move would invalidate. It is the same
   property that makes the seven parallel tables — `@heap`, `@estr`, `@heap_tags`, `@inst_set`,
   `@float_box`, `@free_next`, `@gc_mark` — impossible to drift apart: they are widened by one store to
   one global, so there is no window a program can be halfway inside.
3. **The reservation is free until it is used.** Every arena table is `internal global … zeroinitializer`,
   which is `.bss`: a module that reserves 8192 slots writes none of them at startup, and a program that
   never passes the initial window never touches the extra pages. `TestTheReservationIsAReservationAndNotAnAllocation`
   reads the emitted module and fails if any arena declaration stops being zero-filled — the day someone
   decides to pre-warm the arena with an initializer, that test says why the ceiling can no longer be
   generous.
4. **The collector's cost follows the window, not the reservation.** `rt_gc`'s mark-clear walk is bounded
   by `@heap_cap`, and the container-range question (`rt_container_eq`, the element-range guards) asks the
   committed window too: a handle above the window is a slot the allocator never handed out, whatever the
   reservation could have held. A program that fits is measured against 1024 in every direction.
5. **`grown=` joins the machine path.** `--gc-stats` and the `--json` `gc` member print
   `capacity=… headroom=… grown=… exhausted=…`. `capacity=` on its own cannot distinguish "this arena
   grew" from "this arena was always that big", and that is exactly the distinction an agent is trying to
   make when it asks how close a program came to the wall; `--schema`'s `definitions.gcStats` gained the
   field and its required list. `lang.GCStats.Grown` and `lang.ParseGCStatsLine` are the same line read
   both ways.
6. **The trap says which of the two facts it is.** The sentence is now `out of memory: the compiled heap
   holds 8192 live containers, having grown from 1024 (roadmap Gap Q.2)` — the ceiling reached, and the
   size it started from, because "you ran out at 1024" and "you ran out at 8192 after growing" are
   different diagnoses for the person reading the traceback.

## Why growth is cheap here, and what was rejected

- **`realloc` a flat block.** Rejected outright: a handle would have to be a pointer or the program's
  copies of it would have to be found and fixed, and the seven tables would each need their own
  relocation. The runtime has no handle table to bounce through, and adding one to make growth possible
  costs every read in the program to buy something only large programs use.
- **Segmented arena (`@heap_pages[ceil/1024][1024]`).** Rejected as a second representation of the same
  fact: every accessor in the runtime would become a divide and a modulo, and the guards that already
  read `@heap_count` would have to agree with a new page bound. Doubling a contiguous reservation gets
  the same user-visible behaviour for one new global.
- **Grow to the ceiling eagerly (one constant, `@heap_cap = 8192` always).** Rejected because it is a
  claim the run cannot support: the collector's mark-clear walk becomes 8× the work per collection for
  every program that never needed the room, `capacity=` stops describing the run, and "grew" and "did not
  need to" become indistinguishable. `TestAOTProgramThatFitsNeverGrows` is the negative control — a
  program the initial window holds reports `capacity=1024 grown=0`.
- **Give up on a ceiling and leave 8192 as the fixed number.** Rejected because it keeps ADR 0347's
  complaint at a larger radius: a fixed number is a wall someone has to know by heart, and it prices the
  module's `.bss` for the biggest program anyone might compile.
- **Raise `MemoryError` on the *first* doubling** (i.e. treat growth as the exhaustion point). Rejected
  as untestable nonsense, noted here only because it was the naive reading of "grow": growth is the road
  that answers, exhaustion is the road that refuses, and both still end in the same guard.

## The cost the ceiling had been hiding

`rt_gc`'s sweep had an outer counter walking to `@heap_count` with the whole object scan inside it —
`pass.loop` around `k.loop`, both bounded by the same number, an accidental O(slots²) per collection. Two
things hid it: the arena's own ceiling of 1024 bounded the outer walk, and a churn loop's sweep kept
pulling the high-water mark back down so most collections scanned tens of slots. Measured on a build with
8192 slots of room and the outer loop kept, the 12×200 program above — 2400 live containers — took
**15.76s** to print two numbers, and its 1200-container sibling **2.07s**; the same programs on this
cycle's build take **0.12s** each. Growth without that fix would have shipped a backend that can hold
your data and cannot finish counting it, which is the sort of "supported" that is worse than a trap
because the trap at least stops early. The tripwire in `TestTheCollectorScansTheArenaOncePerCollection`
fails if the outer loop returns.

## Codegen / IR implications

- Three runtime blocks (`heapRuntimeIR`, `numArithRuntimeIR`, `pairFoldRuntimeIR`) became templates
  filled by `fillRootTemplate` with `@HEAPSLOTS@` (the reservation), `@HEAPINIT@` (the initial window)
  and `@HEAPGROW@` (the factor). Arena-touching IR now has one spelling of each number, from
  `heap_guard.go`'s constants; a module cannot allocate from one arena and report another.
- `rt_alloc` gained three blocks — `maybe_grow`, `grow`, and the re-test after widening. The re-test is
  deliberate: one doubling is enough by construction, but the alternative is handing out an index outside
  the window on the first day someone's arithmetic is wrong.
- `@heap_cap` and `@heap_grown` are declared with the collector's other self-report numbers, so a module
  that never allocates still reports its window.
- Growth is invisible to a program that fits: the guard road from ADR 0347 is unchanged, `ret i32 -1` is
  still what `full:` answers, and `TestGrowthLeavesTheGuardInPlace` keeps both in the module that grew.

## Verification

- Unit (`pkg/lang/heap_growth_test.go`): five sizes × the record leg (`TestTheArenaGrowsForAProgramThatNeedsTheRoom`,
  whose five expectations are record entries recorded from CPython by `tools/recmerge`, not from this
  backend) — the window it starts in, one container past the old ceiling, and three doublings; the
  ceiling the arena grew to still traps `MemoryError` uncaught and is still catchable by
  `except MemoryError:`; the collector's single scan; the reservation's zero-fill; the guard beside the
  growth road.
- Unit (`pkg/lang/heap_guard_test.go`, amended): the tripwire now reads `@heap_cap`'s initialiser and the
  growth operations, and fails an allocator that hard-bounds the count to a literal.
- Integration (`integration/gc_heap_growth_test.go`): the same five sizes through the CLI against
  `python3`, asserting exit 0, the printed pair, the window reached, `grown=` agreeing with the window's
  distance from the initial capacity, `exhausted=0`, and a headroom inside the window; plus the negative
  control and the JSON path (`--gc-stats --json`) an agent reads.
- Corpus: `integration/programs/gc_heap_growth.gy` — 1025 containers, one more than the wall — joined the
  conformance matrix as a `match` row. The two exhaustion programs were rewritten at the new scale, since
  a program that used to overflow a 1024 arena now simply fits.

## What this does not buy

The arena is still finite, and the reservation is still a number a program can reach: 8192 live
containers is a trap, not a slow success, and the row keeps that as its own claim
(`TestTheArenaStillTrapsAtTheCeilingItGrewTo`). Growth is per-process and never shrinks; a program that
spikes and releases keeps its window. And the ceiling is a *container* count — 256 element words per object is a second,
unrelated limit, which is why the programs in this cycle's tables build in groups of 200 and 205 rather
than one 8000-element list. Reading a slot of a container whose siblings pushed the arena past one
object's 256-word row is a separate defect, measured while writing these tests and filed as Gap R.271.
