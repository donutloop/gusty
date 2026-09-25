# ADR 0157 — Arbitrary (fnptr-valued) wrapping decorators in AOT

## Status
Accepted

## Context
Gap C required arbitrary decorators (where the decorator body returns a nested
closure that captures the decorated function) to work in the AOT codegen instead
of being rejected. The canonical case is `def dec(g): def wrap(x): return g(x)+1;
return wrap` applied to `def f`.

## Decision
Emit the decorated function via compile-time specialization:
1. The decorator body is emitted as a stub (`ret i32 0`) — it is only used as a
   decorator, never called directly.
2. The decorated original body is emitted as `@f_orig`.
3. The wrapping closure body is emitted as `@f_impl`, and the decorator's
   function parameter (`g`) is bound to `@f_orig` via a new `funcBind` map so
   `g(x)` inside the closure resolves to a static call to `@f_orig`.
4. Decorated calls dispatch to `@f_impl` through the existing `g.decorated`
   mechanism.

This avoids emitting general first-class function values and indirect calls for
the canonical wrapping case. Non-canonical transform decorators are still
rejected.

## Consequences
- The canonical wrapping decorator now compiles and runs correctly in AOT
  (verified by JIT output `7` and AOT parity output `41`).
- A `funcBind` map was added to the IR generator and consulted at call sites.
- A `wrapping_decorator` conformance parity case was added.
