# ADR 0085: `for` loop over a string

## Decision

Allow `for x in "abc"` to iterate over each character of a string, binding
`x` to a single-character string per rune.

## Details

- **Interpreter**: `evalStmt`'s `ForStmt` case now accepts a `str` iterable
  (`o.kind == "str"`). It loops over `o.sval` runes, binding the loop variable
  (`n.Value`) to `allocStr(string(r))` per rune, and evaluates the body with
  the same `loopSignal` (break/continue) handling as the list branch.
- Example: `for x in "abc": ...` iterates `x` = `"a"`, `"b"`, `"c"`.

## Scope

- Interpreter and AOT codegen. The codegen's `ForStmt` case pre-processes a
  `*StrLit` iterable by replacing it with a synthetic `*ListLit` of single-char
  `*StrLit` elements, reusing the existing list unroll (ADR 0085 now ships
  string iteration in both paths).

## Alternatives Rejected

- **Codegen unrolling of a literal string** (emit a loop over each rune as a
  string constant): rejected for simplicity — the codegen `for` loop already
  handles lists and ranges; string unrolling is deferred.
