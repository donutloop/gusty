# ADR 0129 — Classes (inheritance + super) in AOT codegen

## Context
The interpreter already supports classes (definitions, instantiation, `self`
instance attributes, method dispatch, inheritance, and `super()`). The AOT
codegen path did not: `ClassDef` statements fell through to the default error.
Per the roadmap order (exceptions → modules/imports → classes → generators),
classes are the next planned item in AOT.

## Decision
Land classes in AOT codegen with a **static-dispatch model**:

- **Representation.** Class instances are runtime heap objects with `kind = 4`.
  Instance attributes are stored in the object's `data[]` array; each distinct
  attribute name is assigned a global slot index (`attrSlots`).
- **Class definitions.** `ClassDef` registers a `classInfo` (bases + method
  IR-function names) and emits each method as a top-level
  `define i32 @ClassName_method(...)` with `self` as param 0.
- **Instantiation.** `Point(args)` emits `rt_alloc(4)` then calls `__init__`
  (if present) with `self = handle`. The receiver's class is tracked per local
  variable (`varClasses`) from `p = Point(...)` assignments.
- **Method dispatch.** `recv.m(args)` resolves `m` across the base chain at
  codegen time and emits a direct call with `self = recvHandle`. `super().m`
  resolves `m` in the base class of the enclosing class.
- **Instance attrs.** `self.x` reads/writes become `rt_inst_get`/`rt_inst_put`
  on the heap object's slot.
- **Runtime helpers.** `rt_inst_get`/`rt_inst_put` were added to the heap
  runtime IR; `g.heapUsed` is set so the runtime block is emitted.

This is a faithful static model: dispatch is resolved at compile time because
receiver classes are statically known (self or locally-instantiated vars).

## Consequences
- Class programs (basics, inheritance, `super()`) now generate valid LLVM IR
  and run correctly (verified end-to-end via llc + cc).
- Method-call receivers that are not statically known still fall through to the
  existing unknown-method path.
- Added `TestAOTClassBasics` and `TestAOTClassInheritanceSuper` (IR-validity
  via `llcCompiles`).
