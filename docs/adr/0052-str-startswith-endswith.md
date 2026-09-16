# ADR 0052: String `.startswith(sub)` / `.endswith(sub)` in interpreter + AOT codegen

## Decision

Add `str.startswith(sub)` and `str.endswith(sub)` — 1 if the string begins /
ends with `sub`, else 0 — in **both** paths, mirroring Python's
`str.startswith` / `str.endswith`. The interpreter applies
`strings.HasPrefix` / `strings.HasSuffix`; the codegen constant-folds them to
an `i32` literal when the receiver and argument are string constants.

## Details

- **Interpreter**: the `callStrMethod` `startswith`/`endswith` case requires
  exactly 1 argument, evaluates it to a `str` heap object, applies
  `HasPrefix`/`HasSuffix`, and returns 1 or 0.
- **Codegen**: the attr string-method path adds a shared `startswith`,
  `endswith` case folding the receiver `v` and the constant `stringVal`
  argument to `"1"` or `"0"`, returned as an `i32` literal (like `find`).

## Scope

- Only constant receivers/args fold in the codegen (matching the existing
  string-method constant-folding pattern); a non-constant argument errors as
  "<method>() argument must be a constant string". Interpreter path supports
  runtime string receivers.

## Alternatives rejected

- Emitting runtime helpers in the AOT path: deferred — the existing
  string-method AOT surface is constant-folding only.
