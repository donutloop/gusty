# ADR 0135: Memory model

## Status: Accepted

## Context
Following the GC work (generational tracing GC ADR 0132, GC stress harness
ADR 0133, escape-analysis heap elision ADR 0134), the two runtime memory models
deserve an explicit, written contract. The interpreter and the AOT/IR backend
share a conceptual heap (collection/object slots) but implement very different
reclamation strategies. This ADR documents the memory model of each runtime so
future GC and allocation work has a stable reference.

## Decision

### Interpreter heap (boxed values, precise-ish conservative GC)

The interpreter stores every value as a boxed `value` (tagged with a heap
`kind`), so the GC can distinguish a heap reference from an integer. The heap
is a growable slot array:

- Allocation: a new slot is appended; `heapID` is the slot index.
- Roots: the interpreter's top-level environment bindings (`e.Vars`) and any
  live bindings in scope at a collection point.
- Mark: walk a reachable container's fields; a slot is marked once (a visited
  bitmap prevents cycles / repeated walks).
- Sweep: only **pure-data** objects (list/dict/set slots) are freed;
  class instances, closures, and method objects are never collected (they are
  roots / retained conservatively). Freed slots are recycled.
- Generational split (ADR 0132): young (recently allocated) slots are
  collected more frequently than tenured slots, trading a small amount of
  reclaimed garbage for a lower steady-state pause.
- A conservative mark accepts that an integer that happens to be in heap range
  may keep a dead object alive; this is a safe over-approximation (never frees
  a live object).

### AOT / IR heap (raw i32 slot handles, free-list)

The AOT backend lowers collection/object values to raw `i32` slot handles, so
the runtime cannot tell a handle from a plain integer without consulting the
slot table. The heap is a fixed array
`[1024 x {i32 kind, i32 len, [256 x i32] data}]`:

- Allocation (`rt_alloc`): pops a slot from the free-list; if the free-list is
  empty, allocates at `heap_count`; if the heap is full, returns `-1`.
- Free-list (`rt_free`): pushes the slot onto the free-list; the slot's `data`
  is reused to store the next free index.
- Rebinding a variable that holds a collection (cross-collection rebind,
  milestone 9) frees the old slot.
- Escape-analysis elision (ADR 0134): a collection literal whose result is
  provably dead (never read, never aliased) skips allocation entirely, so no
  heap slot is ever created.
- Because there is no runtime type tag, a tracing GC must be *conservative*:
  mark every variable location (globals, env slots, live allocas) whose value
  could be a slot handle, then walk container fields. Over-marking is safe;
  under-marking (a live handle reachable only from a stack temporary) is the
  correctness hazard the AOT model must guard against at GC points.
- The interpreter GC is the reference semantics: reachable-from-roots objects
  survive; unreachable pure-data objects are reclaimed; conservative marking
  never frees a live object.

### Collection points / GC safety

An allocation may trigger GC only at a point where every live heap handle is
reachable from a *markable* location (a variable slot, env slot, or global),
never from a transient SSA temporary. Statement boundaries are safe collection
points; expression interiors are not (element temporaries and function-return
values can hold live handles). The interpreter's boxed model makes every
temporary a tagged value, so it can mark conservatively anywhere; the AOT
model must choose safe points.

## Consequences
- The interpreter model is the correctness reference; the AOT model is a
  conservative approximation that must never reclaim a live object.
- Reclamation is strongest where values are tagged (interpreter) and weakest
  where values are raw i32 handles (AOT), which motivates the escape-analysis
  elision path (ADR 0134): avoid allocating garbage at all.
- Any future AOT tracing GC must (1) mark a conservative root set covering all
  live frames (globals + env slots + live allocas) and (2) only run at safe
  collection points, or it risks freeing a reachable temporary.
