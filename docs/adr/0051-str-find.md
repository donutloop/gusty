# ADR 0051: String `.find(sub)` in interpreter + AOT codegen

## Decision

Add `str.find(sub)` — the index of the first occurrence of `sub`, or -1 if
absent — in **both** paths, mirroring Python's `str.find`. The interpreter
evaluates the argument as a string and returns `strings.Index`; the codegen
constant-folds it to an `i32` literal when the receiver and argument are
string constants.

## Details

- **Interpreter**: the `callStrMethod` `find` case requires exactly 1
  argument, evaluates it to a `str` heap object, and returns
  `int64(strings.Index(s, sub.sval))`.
- **Codegen**: the attr string-method path adds a `find` case folding the
  receiver `v` and the constant `stringVal` argument to
  `fmt.Sprintf("%d", strings.Index(v, subv))`, returning an `i32` literal
  instead of a string global (the rest of the path returns `strConst`).

## Scope

- Only constant receivers/args fold in the codegen (matching the existing
  string-method constant-folding pattern); a non-constant argument errors as
  "find() argument must be a constant string". Interpreter path supports
  runtime string receivers.

## Alternatives rejected

- Emitting a runtime `str.find` helper in the AOT path: deferred — the
  existing string-method AOT surface is constant-folding only.
