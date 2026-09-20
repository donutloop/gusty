# ADR 0136: Dynamic class method dispatch for polymorphic calls

## Status
Implemented.

## Context
The AOT compiler previously dispatched class method calls only when the
receiver's class was statically known (`receiverClass(attr.Obj)`). Calls like
`def f(x): x.speak()` where `x` is a parameter or the result of another
function were unsupported — the receiver's class is unknown until runtime.

## Decision
Store a runtime class-id in instance data slot 0 at instantiation and emit a
dispatch `switch` over that id for method calls whose receiver's class is
unknown at compile time:

1. Reserve instance slot 0 for the class-id; attribute slots now start at 1.
2. Register every class before compiling function bodies so that class
   constructor calls and polymorphic dispatch are recognized inside functions.
   `registerClass` is idempotent to avoid re-emitting method bodies.
3. For `recv.m(args)` with an unknown receiver class, load the runtime
   class-id, `switch` over every class that defines `m`, call the resolved
   method directly in each branch, and `phi`-merge the result. A miss branch
   returns 0 (the interpreter raises; AOT has no runtime error path).

## Consequences
- Polymorphic method dispatch works for unknown receiver classes.
- Dynamic dispatch requires uniform method signatures per method name across
  the class hierarchy (each branch passes the shared evaluated arg list).
- Instance attribute layout changed: slot 0 now holds the class-id, so attrs
  shifted up by one slot (internal, invisible to the language).
