# ADR 0053: String `.count(sub)` in interpreter + AOT codegen

## Decision

Add `str.count(sub)` — the number of non-overlapping occurrences of `sub` —
in **both** paths, mirroring Python's `str.count`. The interpreter evaluates
the argument as a string and returns `strings.Count`; the codegen
constant-folds it to an `i32` literal when the receiver and argument are
string constants.

## Details

- **Interpreter**: the `callStrMethod` `count` case requires exactly 1
  argument, evaluates it to a `str` heap object, and returns
  `int64(strings.Count(s, sub.sval))`.
- **Codegen**: the attr string-method path adds a `count` case folding the
  receiver `v` and the constant `stringVal` argument to
  `fmt.Sprintf("%d", strings.Count(v, subv))`, returned as an `i32` literal
  (like `find`).

## Scope

- Only constant receivers/args fold in the codegen (matching the existing
  string-method constant-folding pattern); a non-constant argument errors as
  "count() argument must be a constant string". Interpreter path supports
  runtime string receivers.

## Alternatives rejected

- Emitting a runtime `str.count` helper in the AOT path: deferred — the
  existing string-method AOT surface is constant-folding only.
