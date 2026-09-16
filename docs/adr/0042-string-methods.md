# ADR 0042: Constant-fold string methods in the AOT codegen

## Decision

Constant-fold `.upper()`, `.lower()`, `.strip()` on constant string literals
in the AOT LLVM codegen, matching the interpreter's `Attr` string-method
semantics, so `len("AbC".upper())` folds to 3 instead of erroring.

## Details

- The codegen previously rejected any `*Call` whose callee was an `*Attr`
  (string methods were interpreter-only). This ADR adds:
- A `case *Call:` in the package-level `stringConst(e) (string, bool)` that
  folds `attr.Obj` via `stringConst` and applies upper/lower/strip, so
  `len(...)` (which calls `stringConstLen`) folds method calls.
- A `case *Call:` in the `irGen` `stringVal(e)` method (same folding via
  `g.stringVal(attr.Obj)`) so string concatenation and length also fold
  method results.
- The `call()` builtin dispatch still constant-folds Attr string methods via
  `g.stringConst`-style folding for non-print contexts.

## Scope / limitation (resolved)

- `print(string-method-result)` is now lowered: `print` recognizes any
  constant-foldable string arg via `g.stringVal(a)` (not just `*StrLit`), so
  `print("AbC".upper())` emits the folded string global. Integer contexts
  (`len`, concatenation, equality) and print are all supported.
