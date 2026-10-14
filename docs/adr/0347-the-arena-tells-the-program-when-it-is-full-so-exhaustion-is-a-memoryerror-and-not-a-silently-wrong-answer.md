# ADR 0347 — The arena tells the program when it is full: exhaustion is a `MemoryError`, not a silently wrong answer

Status: accepted
Date: 2026-10-15
Decomposes: [ADR 0337](0337-a-lowering-that-claims-a-constructor-s-own-trap-s-answers-is-served-by-a-call-to-the-one-function-that-owns-them.md)
Amends: [ADR 0334](0334-a-handle-is-a-value-the-roots-can-name-so-a-container-variable-reads-through-the-same-slot-an-argument-reads-through.md)
Depends on: [ADR 0333](0333-a-loop-becomes-two-registers-and-a-backedge-so-the-body-runs-as-many-times-as-the-program-needs.md), [ADR 0335](0335-a-loop-becomes-a-loop-the-compiler-can-analyze-when-it-can-count-the-iterations-and-keeps-the-roads-when-it-cannot.md)
Roadmap row: Gap Q.2

## The gap, as measured

`@heap` is `[1024 x {i32, i32, [256 x i32]}]`: 1024 containers, each 256 payload words plus a
refcount and a tag. A program that wants a 1025th gets `rt_alloc`'s `full:` arm, which answers
`-1`. Nothing upstream of that arm ever looked at the answer, so `-1` went into a root slot and the
program continued. `rt_list_len(-1)` answers 0, `rt_list_get(-1, i)` clamps, and the printers read
whatever the slot arithmetic landed on.

The measured case was a retention loop — build a list, keep it, read it back:

```
def push_n(grown, n):
    i = 0
    while i < n:
        grown.append([1, 2, 3])
        i = i + 1
    return i

bucket = []
push_n(bucket, 4000)
print(len(bucket))
```

Before this cycle that program printed a length and exited 0. Its sibling, a churn loop that prints
`lst[0]` on every trip of a 2000-iteration `while`, printed **2120 lines where CPython prints 2060** —
and the extra lines were `[7]` and `[13]`, values the program had never built. Those numbers were not
fabricated: `print` was honestly rendering the object that lived at the slot the program believed it
held, because the arena had been recycled under it. Two properties were indefensible on their own, and
`roadmap.md` had said so since the row was written: an out-of-memory condition was **not observable**
(`--gc-stats` reported `live` and `top` but never a capacity, so a workload's headroom was invisible),
and an exhausted arena was **not a trap** (the program did not die, it got it wrong, and exited 0).

## The decision

**`@heap` is a resource, and running out of it is a language event.**

1. **The allocator counts; the caller raises.** `rt_alloc`'s `full:` arm increments `@heap_exhausted`
   and still returns `-1`. The raise lives in the *caller's* block — `emitHeapCall`,
   `emitRtAllocKind`, `emitRtFloatNew`, `emitRtAlloc`, `emitHeapHelperCall` in `pkg/lang/heap_guard.go` —
   which branches to a block that runs the program's own `rt_raise` and then the uncaught path. This is
   ADR 0337's seam chosen one door earlier: a helper that raised for its caller would own exception
   state codegen cannot see, would have no source position to print, and would still hand `-1` back to
   a store that runs anyway.
2. **The class is `MemoryError`.** Added to `pkg/lang/exceptions.go` with a numbered code, so the
   dispatch road can compare it and the matcher can see it, and `issubclass` declares
   `MemoryError <: Exception`. `except Exception:` catches it; `except MemoryError:` catches it more
   precisely. The sentence names the arena and the row that owns growing it:
   `out of memory: the compiled heap holds 1024 live containers (roadmap Gap Q.2)`.
3. **The capacity has one spelling.** `HeapCapacity = 1024` generates the arena's array types, the
   allocator's bound, the report's `capacity=` and the trap's sentence. `TestTheArenaSpellsItsCapacityOnce`
   reads the emitted module and fails if any declared array or bound disagrees with the constant, so a
   future cycle that grows the arena grows the diagnostic with it — which is the reason the diagnostic
   comes first.
4. **The guard is emitted only where the heap can run out.** `needsHeapGuard` declines for a constant
   text (interned in `@str_tab`, no slot taken), for `rt_none` (a cached handle in `@none_h`), and for
   the `*_unchecked` variants that exist precisely to answer a sentinel. A guard is a compare, a branch
   and a second edge to a raise block; paying that on a call that cannot fail doubles the hot path for
   a condition that cannot occur.
5. **The status roads carry a fourth status.** `rt_num_arith` answers a status, not a handle, and its
   `rt_float_new` can fail — so it now answers **status 4**, and the arithmetic call sites in
   `pkg/lang/heapargs.go` (`nummemchk`/`nummemraise`) and `pkg/lang/pairfold.go`
   (`foldmemchk`/`foldmemraise`) raise through the same `raiseHeapExhausted`. The collector stays on
   the unchecked path: it has no program counter to die at, which is the same reason ADR 0337's
   footnote keeps repeating itself.
6. **The loop body is a safe point.** The gap's real mechanism was never an allocation the source
   could see; it was ADR 0333's back-edge and ADR 0335's counter road, which allocate once *per trip*.
   `loopSafePoint` emits `rt_gc_collect` plus `rt_gc_check_roots` and the same `heapfull` guard at the
   top of every `while` body, every `for` body (literal, counter and container roads) and the
   generator's append loop. `loopMayAllocate` is the conservative static question that keeps the sweep
   out of a loop whose body cannot take a slot, so `while i < n: i = i + 1` still costs one compare.
7. **The collector's element walk asks the tag first.** `rt_gc_tag_is_obj` answers whether a slot's tag
   names something the arena owns; the four immediate kinds (int 0, bool 2, None 3, interned text 4) do
   not, and everything else — a float box, a nested container, an instance — does. An unrecognised tag
   answers "object": keeping a dead object leaks, sweeping a live one corrupts. Marking every payload
   *word* was the retention bug: `lst = [i]` marked slot `i`, so the loop pinned one dead object per
   iteration and walked the arena to its ceiling.
8. **`--gc-stats` reports the arena.** `capacity=`, `headroom=` and `exhausted=` joined the line and the
   `gc` member of the `--json` payload, and `rt_gc_report` now runs on `main.raiseexit` as well, so a
   program that dies on an uncaught trap still says how full the arena was when it happened.

## Behaviour

Before: an overflowing arena printed the empty string, compared objects that did not exist, printed
another iteration's list, and exited 0.

After — `programs/gc_heap_uncaught.gy`:

```
$ gustyc --file programs/gc_heap_uncaught.gy --gc-stats
gc: backend=aot collections=1028 roots=5 skipped=0 marked=1024 freed=0 total_freed=0 \
    live=1024 top=5 capacity=1024 headroom=0 exhausted=1
Traceback (most recent call last):
MemoryError: out of memory: the compiled heap holds 1024 live containers (roadmap Gap Q.2)
$ echo $?
3
```

and the handleable half, `programs/gc_heap_exhaustion.gy`:

```
try:
    bucket = []
    push_n(bucket, 4000)
except MemoryError:
    ...          # → "MemoryError caught"; `except Exception:` covers it too, exit 0
```

Handling the condition does not create capacity: the two buckets are still live, so the next container
the program builds raises again. That is the right semantics for an exhausted arena and the reason the
handleable witness ends at its two caught arms instead of demonstrating recovery.

## IR impact

Every guarded call site grows a `heapfull` block that calls `rt_raise` with `MemoryError`'s code and
takes the uncaught edge. The module gains `@heap_exhausted`, `@heap_live` and `@heap_capacity` — placed
in `rootGlobalsIR` beside `@heap`, **not** in `heapRuntimeIR`, because `rt_gc_report` is emitted for any
program that prints a float and asks for stats, and a report reading an undefined global is the ADR 0173
class of verifier failure. `rt_gc_tag_is_obj` is new. `rt_gc_report` grew from 10 fields to 13 and its
scratch buffer from `[192 x i8]` to `[256 x i8]`; `gcRootReportLine` gained three `@PLACEHOLDERS@`, one
of which (`@HEAPCAP@`) is substituted from `HeapCapacity` in Go rather than written as a literal.

## What this cycle does not fix

Three things the corpus surfaced that are **not** this promise, named so a later reader does not
mistake them for regressions from it:

- **`@gc_roots` is capped at `gcRootCap`.** `rt_root_put` saturates the counter past the cap, so a
  program whose live root set exceeds it has an arena whose headroom cannot shrink and will reach the
  ceiling for the wrong reason. L11.1's fixed-size root stack.
- **A container element's handle is not itself a root.** Reading an element out of a container under
  pressure can print the empty string, because the element lives in the payload and not in `@gc_roots`.
  L11.1's nested-element handles.
- **A `while` body's mid-trip garbage waits for the next trip.** The safe point is at the top of the
  body; values allocated and dropped mid-body are reclaimed on the following iteration. L11.1 again.

## Tests

`pkg/lang/heap_guard_test.go` — `TestEveryHandleRoadCarriesTheGuard` (each road's helper is called
through a guard in the emitted module), `TestEveryStatusRoadRaisesOnExhaustion` (the status roads,
including `rt_num_arith`'s status 4), `TestEveryHandleRoadCarriesTheGuardAcrossTheCorpus` (the same
question over `integration/programs/*.gy`, because a road guarded in isolation and naked in a real
program does not count), `TestTheGuardIsNotEmittedWhereTheHeapNeverRunsOut`,
`TestTheArenaSpellsItsCapacityOnce`, `TestLoopBodiesAreSafePoints`,
`TestCollectorMarksOnlyWhatTheTagCallsAnObject`, `TestMemoryErrorIsAClassTheProgramCanName`.

`integration/gc_heap_trap_test.go` — the four witnesses at the top of this file, including the reference
leg: the same allocation roads at a size that fits, checked against CPython, so a guard that makes a
working program raise cannot be green. `integration/gc_stress_test.go` keeps the bounded-heap property
and the `--json` payload green with the three new fields.

## Alternatives rejected

- **A larger arena.** 64K slots would have passed every test in this cycle and failed the next program.
  The ceiling is not the bug; the silence is. Kept as the roadmap's second half ("grow or segment
  `@heap`") precisely because a growing arena whose growth is invisible is the same bug with a longer
  time to manifest.
- **`rt_alloc` raises instead of returning `-1`.** One place to remember, but the collector reaches it
  through `rt_list_copy` and the collector has no traceback to build, no frame to pop, and no
  legitimate exit status; and an allocator cannot tell a caller that will handle the condition from one
  that will store the `-1`.
- **Raising inside each allocation helper rather than at each call site.** `emitHeapCall` knows the
  source span; the helper does not. The line is the only part of the diagnostic that lets a program fix
  itself.
- **A `0`-means-exhausted convention.** Slot 0 is a valid slot and `rt_list_len(0)` already answers 0 —
  which is how `[None]` prints the empty string today. The convention would make the common case
  invisible instead of the rare one visible.
- **A reserved "poison" object returned on failure.** The program would read a container that exists and
  is empty: a wrong-but-plausible answer, the exact class of bug this row exists to remove.
- **Exit 2 on exhaustion.** Exit 2 is the compiler's status (ADR 0166) — LLVM rejected the module we
  emitted. The arena filling up is the program's world, and the program's statuses are 0 and 3
  (ADR 0211).
- **Raising at the trap site instead of the arena check** (i.e. letting a later read fail). The read is
  where the *wrong answer* is produced, so the failure would be reported somewhere far from the cause,
  and ADR 0214's rule — a trap the program cannot name at the place it happens is not handleable —
  would be violated.
