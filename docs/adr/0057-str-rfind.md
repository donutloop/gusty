# ADR 0057: String `.rfind(sub)` in interpreter + AOT codegen

## Decision

Add `str.rfind(sub)` — the index of the last occurrence of `sub`, or -1 if
absent — in **both** paths, mirroring Python's `str.rfind`. The interpreter
applies `strings.LastIndex`; the codegen constant-folds it to an `i32`
literal.

## Details

- **Interpreter**: the `callStrMethod` `rfind` case requires exactly 1
  argument, evaluates it to a `str` heap object, and returns
  `int64(strings.LastIndex(s, sub.sval))`.
- **Codegen**: the attr `call` dispatch adds an `rfind` case folding the
  receiver `v` and the constant `stringVal` argument to
  `fmt.Sprintf("%d", strings.LastIndex(v, subv))`, returned as an `i32`
  literal (like `find`).

## Scope

- Only constant receivers/args fold in the codegen (matching the existing
  string-method constant-folding pattern); interpreter path supports runtime
  string receivers.
