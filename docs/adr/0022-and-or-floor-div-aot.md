# ADR 0022: Boolean `and`/`or` operators + `//` floor division in the LLVM AOT codegen

## Decision

The parser, semantic analyzer, and interpreter already support the boolean
`and`/`or` operators and `//` floor division, but the LLVM AOT codegen rejected
them as unsupported operators (falling through to
`unsupported operator %q`). Mirror the interpreter semantics in
`pkg/lang/codegen.go` so these constructs ship in **both** backends.

## Codegen/IR implications

- `and`/`or` evaluate both operands and return a boolean `0`/`1`, matching the
  interpreter's `evalBin` behavior (which does not short-circuit). Lowering:
  `icmp ne i32 %l, 0` + `icmp ne i32 %r, 0`, combine with `and`/`or i1`, then
  `zext i1 to i32`.
- `//` lowers to `sdiv`, also matching the interpreter (which treats `/` and
  `//` identically as integer division).
- Literal operands are constant-folded in the existing folding switch:
  `and` = `lv != 0 && rv != 0`, `or` = `lv != 0 || rv != 0`, `//` = `lv / rv`.

## Alternatives rejected

- Implementing short-circuit `and`/`or` (evaluate only the left operand when it
  decides the result) — would diverge from the interpreter and require control
  flow in a straight-line codegen; the interpreter's evaluate-both semantics is
  the source of truth per the two-path contract.
- Floor-division rounding toward negative infinity for `//` — the interpreter
  does not floor; mirroring it with `sdiv` keeps the backends in sync.
