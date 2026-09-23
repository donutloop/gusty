# ADR 0151 — AOT dynamic dispatch on variables: GC/instance-layout corruption (known bug)

## Status
Accepted (known limitation, documented).

## Context
The Phase 1 "all-call-site dispatch" work verified that AOT emits a runtime
dispatch switch for instance method calls whose receiver class is not
statically known, and that this switch works from every expression position
(method call as a value in arithmetic, as a function argument, in a return,
and assigned to a variable) — see the `dispatch_nested` conformance program.

A separate reproducer revealed a remaining correctness bug in the AOT backend:

```
a = make(1)   # returns Animal (class-id 0)
b = make(2)   # returns Dog    (class-id 1)
print(a.speak())   # interpreter: 1 ; AOT: 42 (WRONG)
```

The interpreter returns `1` (Animal.speak). The AOT JIT returns `42`
(Dog.speak): after a *second* instance allocation (`b = make(2)`) occurs, the
handle stored in the variable `a` appears to alias the freshly allocated Dog
instance, so the dynamic dispatch on `a.speak()` selects Dog.speak.

Narrowing:
- `a = make(1)` then `a.speak()` with no subsequent allocation: correct (1).
- `print(make(1).speak())` (receiver is the call result, not a variable):
  correct (1) in both backends.
- Only a *variable* holding an instance, followed by another allocation,
  triggers the corruption.

## Root-cause analysis (investigated)
- The AOT GC (`rt_gc`) marks live instances via roots registered with
  `gcReg`. The single-assign path (`a = <call>`) did not register the target
  variable slot as a GC root at creation (unlike the tuple-assign path),
  so the first instance is a GC candidate and the second allocation reuses
  its heap slot via the free-list.
- Registering the variable as a root (`g.gcReg(b, nm.Value)` after the
  general single-assign store) made the reproducer still fail and also
  produced a runtime segmentation fault in an existing dispatch conformance
  case, so the fix was reverted. The exact mark/sweep interaction with
  stack-allocated variable slots is not yet understood.

## Consequences
- Dynamic dispatch on a *variable* receiver across multiple allocations is
  not reliable in the AOT backend. The interpreter is correct.
- The `dispatch_nested` conformance program deliberately avoids the broken
  pattern (it exercises dynamic dispatch on call-result receivers and
  statically-known receivers at nested call sites), so parity holds.
- A future round should fix the GC rooting / instance-alias bug before
  relying on dispatch-on-variable in AOT.

## Alternatives considered
- Registering single-assign variables as GC roots (rejected: incomplete and
  caused a segfault).
- Disabling GC reuse (rejected: not investigated, higher risk).
