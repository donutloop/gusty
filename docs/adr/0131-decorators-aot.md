# ADR 0131: Arbitrary decorators in AOT codegen (identity + clear rejection)

## Context
The interpreter applies decorators to a function value in source order:
`@dec1 @dec2 def f: ...` == `f = dec2(dec1(f))`, where each decorator is a
callable that receives the current function value and returns a (possibly
wrapped) one. The AOT/LLVM codegen path previously emitted the decorated
function body as `@f_impl` and `@f_ptr = @f_impl`, treating *all* decorators as
identity — silently ignoring non-identity decorators and producing wrong
behavior. Per the `Dynamic dispatch + method tables` roadmap item, arbitrary
decorators are the final construct to land in AOT.

## Decision
Land decorator application machinery in AOT:

- `emitDecoratedFunc` now resolves the decorator list via `resolveDecorators`
  and emits `@f_ptr`/`@f_apply` pointing at the *resolved* function value.
- `resolveDecorators` walks the decorator list in source order (matching the
  interpreter). A decorator that is a known top-level function whose body is
  exactly `return <its sole param>` is an **identity** decorator and leaves the
  function value unchanged (`@f_impl`); the existing direct-call path
  (`call @f_impl`) remains correct.
- Any non-identity decorator body (wrapping closures, transforms that call the
  decorated function, factories) is **rejected at codegen time with a clear
  error** (`codegen: decorator %q for %q is not an identity decorator`), instead
  of being silently ignored.

## Consequences
- Identity decorators (and multiple identity decorators in source order) work
  end-to-end in AOT, matching the interpreter.
- Non-identity decorators are now detected and rejected loudly, fixing a
  correctness bug where codegen silently ignored them.
- Full arbitrary decorators (wrapping closures that call `g()` and return a
  closure) require function-pointer values / indirect calls, which the codegen
  model does not yet support; these are a documented follow-on.
- Tests: `TestAOTIdentityDecorator`, `TestAOTRejectsNonIdentityDecorator`
  (unit, IR-validity via `llcCompiles` + error-path via `Compile`), and
  `TestDecoratorIdentityRuntime` / `TestDecoratorNonIdentityRejected`
  (integration, whole-program execution).

## Alternatives Rejected
- **Function-pointer-valued decorator functions** (`i32(...)*` params/returns
  and indirect calls): rejected — the codegen function model returns `i32` and
  has no fnptr operand support; a follow-on runtime-dispatch item.
- **Emitting every decorator body directly as the decorated function**: rejected
  — cannot represent closure capture of the decorated function.
