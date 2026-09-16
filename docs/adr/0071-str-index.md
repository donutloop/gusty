# ADR 0071: String `.index(sub)` in interpreter

## Decision

Add `str.index(sub)` — the index of the first occurrence of `sub`, raising
"substring not found" when absent — mirroring Python's `str.index`. The
interpreter applies `strings.Index` and returns an error when absent
(like `find` but errors instead of returning -1).

## Details

- **Interpreter**: the `callStrMethod` `index` case requires exactly 1
  argument, evaluates it to a `str`, applies `strings.Index`, and returns
  `int64(idx)` on success or `&EvalError{Msg: "substring not found"}` when
  `idx < 0`.

## Scope

- Interpreter path only: error-raising string methods are not constant-folded
  in codegen.
