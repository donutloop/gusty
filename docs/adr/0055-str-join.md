# ADR 0055: String `.join(list)` in interpreter + AOT codegen

## Decision

Add `str.join(list)` — join a list of string elements with the receiver as
separator — in **both** paths, mirroring Python's `str.join`. The interpreter
evaluates the list arg and applies `strings.Join`; the codegen folds the
receiver and a constant list of string elements to a string global.

## Details

- **Interpreter**: the `callStrMethod` `join` case requires exactly 1
  argument, evaluates it to a `list` heap object, collects each element as a
  `str`, and returns `allocStr(strings.Join(parts, s))`.
- **Codegen**: `join` is folded in three places:
  - `stringConst` (package function) — folds the `ListLit` arg elements via
    `stringConst(el)` to `strings.Join(parts, v)`.
  - `stringVal` (method) — folds via `g.stringVal(el)`; this is what `print`
    uses to detect a join result as a string, so
    `print("-".join(["a", "b", "c"]))` emits the global "a-b-c".
  - the attr `call` dispatch — sets `v = strings.Join(parts, v)` so the
    runtime string-method tail emits the joined result.

## Scope

- Constant list literals fold in the codegen (matching the existing
  string-method constant-folding pattern); a non-constant list argument
  errors as "join() argument must be a constant list". Interpreter path
  supports runtime list receivers.

## Alternatives rejected

- Emitting a runtime `str.join` helper in the AOT path: deferred — the
  existing string-method AOT surface is constant-folding only.
