# ADR 0033: Zero-argument `print()` in the LLVM AOT codegen

## Decision

The interpreter's `print` iterates over its argument list and writes each via
`fmt.Println(e.Repr(v))`; with no arguments the loop is empty, so `print()`
writes nothing. The LLVM AOT codegen rejected `print()` with
`print needs an argument` — a codegen-only error for a program the interpreter
accepts and runs as a no-op. Accept zero-argument `print()` in the AOT codegen
and emit no `printf` call, returning a fresh void temp, so `print()` behaves
identically in both backends.

## Codegen/IR implications

- The `len(c.Args) < 1` guard no longer returns an error; instead it returns a
  fresh temp with no IR emission.
- The multi-argument and string-literal `print` lowering (ADR 0030, 0031) is
  unchanged for `len >= 1`.
- The AOT path stays i32-only and allocation-free — no new runtime support.

## Alternatives rejected

- Emitting `printf("\n")` for zero-arg `print()` (Python semantics) — rejected:
  this language's interpreter writes nothing for `print()`, so the AOT path
  must match the interpreter's no-op exactly.
- Keeping `print()` a codegen error — rejected: rejecting a program the
  interpreter accepts and runs (as a no-op) is a silent divergence, not a
  documented limitation.
