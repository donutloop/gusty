# ADR 0082: `reversed` builtin

## Decision

Add a `reversed(x)` builtin that returns a reversed copy of a list or a
string: `reversed([1, 2, 3])` -> `[3, 2, 1]`, `reversed("abc")` -> `"cba"`.

## Details

- **Interpreter**: `evalCall`'s builtins switch adds `case "reversed":`. It
  evaluates the argument; for a `list`/`set` object it allocates a new list
  with the elements reversed in place; for a `str` object it allocates a new
  string with `reverseStr(sval)` (a new rune-based `reverseStr` helper).
  Anything else is an `*EvalError{"reversed expects a list or string"}`.
- Example: `reversed([1, 2, 3])` evaluates to `[3, 2, 1]`.

## Scope

- Interpreter and AOT codegen. The codegen's builtins switch adds
  `case "reversed":`, which folds on a literal list (`*ListLit`) into a
  reversed `*ListLit` emitted via `emitList`, and on a literal string
  (`*StrLit`) into `g.strConst(reverseStr(...))`. Non-literal arguments are
  rejected at compile time (the codegen folds builtins only on literals).

## Alternatives Rejected

- **In-place mutation** (Python `list.reverse()` mutates the receiver): rejected
  for simplicity — `reversed(x)` returns a new copy and never mutates `x`.
