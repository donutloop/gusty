# ADR 0139: Operator overloading (dunder dispatch)

Status: accepted

## Context

Phase 6 of the roadmap lists operator overloading (dunder methods: `__add__`,
`__getitem__`, ...) as the prerequisite for `with`/context managers and
idiomatic classes. Binary operators in the interpreter currently fold to
numeric/string logic; class instances could not participate in `+ - * / // %
**` or comparisons. We need dispatch to dunder methods on instances.

## Decision

Add dunder dispatch for binary operators in the interpreter (jit.go):

- For `a OP b`, when `a` is a class instance with the matching dunder method
  (`__add__`, `__sub__`, `__mul__`, `__truediv__`, `__floordiv__`, `__mod__`,
  `__pow__`), invoke it with `a` bound as `self` and `b` as the argument.
- If `a` is not overloaded but `b` is an instance, fall back to the reflected
  method (`__radd__`, `__rsub__`, `__rmul__`, `__rtruediv__`, `__rfloordiv__`,
  `__rmod__`, `__rpow__`) on `b` with `b` as self and `a` as the argument.
- Comparisons dispatch to `__eq__`/`__ne__`/`__lt__`/`__le__`/`__gt__`/`__ge__`;
  the reflected fallback uses the swapped comparison (`__lt__` on `b` with `a`
  becomes `__gt__`, etc.).

A new helper `dunderCall` resolves the method via `resolveMethod(classIDs[class],
name)` and invokes it with self binding through the existing `callMethod`.

## Consequences

- Operator overloading is currently an **interpreter-only** feature. The
  AOT/codegen path (codegen.go) still folds binary ops to numeric arithmetic,
  so overloaded class instances do not participate in compiled binaries. This
  is an accepted AOT limitation, matching the precedent for arbitrary
  decorators (ADR) and nested-closure support. Codegen static-dispatch for
  dunder methods is planned future work.
- The interpreter resolves dunder methods via the class table
  (`Evaluator.classIDs`), so overloads participate in inheritance resolution.
- Tests: `TestEvalOperatorOverloading` covers left dispatch, reflected
  dispatch, and comparison dispatch.
