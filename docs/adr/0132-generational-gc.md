# ADR 0132: Generational tracing GC in the interpreter heap

## Context
The interpreter already had a tracing mark-and-sweep collector (`Collect()`):
mark from roots (`Vars` globals) and sweep every unreachable collectable object
(list/dict/set/str/int/float) on each call. That is a full-heap, non-generational
GC: every collection traces and sweeps the entire heap. Per the `Modern GC`
roadmap item, a generational tracing GC keeps young (short-lived) allocations
cheap and avoids re-tracing old, promoted objects.

## Decision
Land a two-generation tracing GC in `Collect()`:

- The **nursery** is the set of objects with `id >= nurseryBase`. New allocations
  (`allocObj`) increment `allocCount` and land in the nursery.
- A **young GC** (the common path) traces only the nursery: it marks reachable
  objects from roots (`Vars`), sweeps unreachable nursery objects of collectable
  kinds, and **promotes survivors** by advancing `nurseryBase` to the current
  maximum heap id — promoted objects become old and are no longer traced on the
  next young GC.
- A **full GC** runs when the old generation grows past a threshold (512 old
  objects) or on the first collection (`nurseryBase == 0`): it traces and sweeps
  the whole heap (the previous behavior) and resets the nursery.
- `maxHeapID` returns the new nursery boundary after a young GC.

## Consequences
- Short-lived objects are reclaimed by cheap young GCs without re-tracing the
  old generation.
- Promoted (surviving) objects are stable across subsequent young GCs; the
  full GC bounds the old generation's growth.
- Root set remains `Vars` (globals) as before; GC is still invoked explicitly
  (the interpreter has no stack, so mid-execution automatic GC would be unsafe).
- Tests: `TestGenerationalGC` verifies nursery reclamation, promotion, and
  survival of old objects across young GCs; the existing `Collect` tests still
  pass.

## Alternatives Rejected
- **Moving/compacting nursery** (copying survivors to a contiguous old space):
  rejected — heap ids are used as opaque values across the evaluator and would
  require global id-remapping at every use site.
- **Automatic GC between statements**: rejected — without an explicit stack/
  frame root set, mid-execution collection could sweep live locals.
