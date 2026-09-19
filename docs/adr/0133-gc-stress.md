# ADR 0133: GC stress/leak tests

## Context
The interpreter's tracing GC (ADR 0132, generational) needs regression tests
that prove the heap stays bounded under allocation pressure and that promoted
(old) objects survive full collections. Without such tests, a GC regression
(over-aggressive sweep, promotion bug, or full-GC threshold drift) silently
corrupts the heap.

## Decision
Add two stress/leak tests to `memory_test.go`:

- `TestGCStressBoundedHeap`: allocates 2000 short-lived nursery objects with a
  young GC after each; verifies the reachable global survives every collection
  and promoted survivors survive stress young GCs.
- `TestGCFullGCBoundsOldGen`: promotes two globals to old, drops one (unmarks
  it), allocates 600 nursery objects to push the old generation past the
  full-GC threshold (512 old objects), and verifies the full GC reclaims the
  unreachable old object while keeping the reachable old global.

## Consequences
- The generational GC's nursery reclamation, promotion, and full-GC threshold
  are covered by regression tests.
- These tests are pure-interpreter (no stack), so they run at explicit
  collection points only, matching the GC's safety contract.
