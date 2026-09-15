# ADR 0024: String-constant concatenation + `len` in the LLVM AOT codegen

## Decision

The interpreter already supports string concatenation (`"a" + "b"` → `"ab"`) and
`len` of a string (character count). Mirror this in the LLVM AOT codegen path
(`pkg/lang/codegen.go`) for the compile-time-known cases so string-constant
concatenation and `len` ship in **both** execution backends.

## Codegen/IR implications

- `+` on two string literals folds to a single concatenated string constant
  (`g.strConst(ls.Value + rs.Value)`), emitted as a `@.strN` global.
- `len` of a string-constant expression resolves at compile time: a helper
  (`stringConst`/`stringConstLen`) recursively folds a string literal or a chain
  of `+`-concatenated string literals to its concrete value, then returns the
  character count as an `i32` constant — no count-field load is emitted.
- The semantic analyzer types `str + str` as `str` (no "arithmetic on non-numeric
  operands" warning), matching the interpreter.

## Alternatives rejected

- Runtime string concatenation / allocation — the AOT path has no allocator;
  only compile-time-known string constants are foldable, keeping codegen
  straight-line and verifiable.
- Handling only a single `+` pair — a chain like `"a" + "b" + "c"` is a nested
  `BinOp`, so the folding helper recurses through the whole concat chain.
