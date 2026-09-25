# ADR 0151 — AOT dynamic dispatch: instance-layout/GC rooting for polymorphic receivers

## Status
RESOLVED (Round 18). The GC-rooting fix landed earlier and Round 18 added a
real GC-stress conformance program that proves the receiver dispatches on the
live instance under genuine collection + slot reuse.

## Context (original bug)
The AOT GC is a conservative mark-and-sweep over a fixed 1024-slot heap. Each
object is `{kind, len, [256 x i32] data}` and a handle is a heap slot index.
Dynamic dispatch reads the runtime class-id from the receiver's slot
(`rt_inst_get(h, 0)`) and switches on it. The known bug: a polymorphic
receiver held in a *variable* was not rooted, so when a later allocation
triggered GC, the receiver's slot could be freed and reused, and
`rt_inst_get` on the stale handle read garbage → the dispatch switch
mis-tagged (e.g. an `Animal` receiver dispatched as a `Dog`).

## Decision / fix
- Register every variable slot that can hold an instance handle as a GC root:
  single-assign, augmented-assign, tuple-assign, loop variables, and function
  parameters all call `gcReg` after the store. A root slot tracks the
  variable's *current* value each GC, so the live instance stays alive.
- The GC transitively marks reachable heap data (list/dict/instance data
  elements are treated as handles), so instances stored in rooted collections
  survive.
- Every method call site emits the runtime class-id dispatch switch, including
  expression-level receivers (`v.speak()` in a function body,
  `make(1).speak()`, `lst[0].speak()`), not just statement-level calls.

## Verification (Round 18)
- `dispatch_gc_stress.gy` conformance case: allocate an `Animal` and a `Dog`
  receiver, then force the 1024-slot heap to fill/free/reuse repeatedly (2000
  throwaway lists, so real collection + slot reuse occurs), then dispatch on
  both receivers. Output 43 (= 1 + 42), interpreter == AOT (parity).
- `dispatch_gc.gy` (receiver survives a later allocation) and
  `dispatch_nested.gy` (expression-level receiver in a function body) also
  pass parity.
- Manual stress checks: receiver passed as an argument after GC, two
  receivers under pressure, list-element receivers, call-result receivers —
  all dispatch on the live instance.

## Consequences
- A polymorphic receiver that survives many real GC collections + slot reuse
  dispatches correctly on the live instance. DoD satisfied: parity program
  dispatches identically on interpreter + AOT + JIT.
