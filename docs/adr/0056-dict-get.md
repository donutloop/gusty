# ADR 0056: Dict `.get(key[, default])` in interpreter

## Decision

Add dict `.get(key[, default])` — return the value for `key`, or `default`
when absent (default is 0 when omitted) — mirroring Python's `dict.get`.

## Details

- **Interpreter**: the `callDictMethod` `get` case evaluates the key arg
  (and optional default arg), scans the dict's `elems`/`dvals`, returns the
  matching `dvals[i]` or the default. It takes 1 or 2 arguments.
- **Bug fix**: dict key lookups previously compared boxed ids, but string
  keys are not interned (each `"a"` literal gets a fresh id), so `d["a"]`
  and `.get("a", ...)` failed to find keys. Added `dictKeyEq(k, idx)` which
  compares string keys by content and int keys by value; used in both dict
  indexing and `.get`.

## Scope

- Interpreter path only: codegen dict support is partial (constant-key dict
  indexing only), so `.get` is not folded in codegen yet.

## Alternatives rejected

- Interning strings in `allocStr`: rejected — broader change affecting all
  string boxing; `dictKeyEq` is scoped to dict key comparison.
