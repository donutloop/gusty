# ADR 0029: `%` modulo operator in the LLVM AOT codegen

## Decision

The interpreter (`pkg/lang/jit.go` `evalBin`) already supports `%` as a signed
remainder. The LLVM AOT codegen (`pkg/lang/codegen.go`) only folded `%` on
constant operands (`foldConstInt`) and rejected runtime operands with
`unsupported operator "%"` — a real interpreter-vs-AOT gap. Lower `%` to LLVM
`srem` (signed remainder) in the runtime `BinOp` lowering path so `x % 5`
compiles and runs in the AOT path exactly like `//` lowers to `sdiv`.

## Codegen/IR implications

- Runtime `BinOp` with `%` emits `srem i32 %l, %r`, mirroring the interpreter's
  signed `%` semantics (no separate division-by-zero guard, consistent with how
  `/` and `//` are already lowered).
- Constant operands still fold at compile time (`17 % 5` → `2`), so modulo on
  literals stays allocation-free and block-free.
- The AOT path remains i32-only and allocation-free; `%` needs no new runtime
  support.

## Alternatives rejected

- Lowering `%` via `sdiv` + `mul` + `sub` (remainder by definition) — rejected:
  `srem` is the single-instruction, allocation-free signed-remainder primitive
  and matches the interpreter exactly.
- Keeping `%` interpreter-only (documenting it as an AOT limitation) — rejected:
  modulo is a core arithmetic operator already in the language surface; the
  runtime path should stay in sync with the interpreter.
