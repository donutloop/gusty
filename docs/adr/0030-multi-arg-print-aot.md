# ADR 0030: Multi-argument `print` in the LLVM AOT codegen

## Decision

The interpreter's `print` (`pkg/lang/jit.go`) already iterates over every
argument and writes each to stdout on its own line via `fmt.Println(e.Repr(v))`.
The LLVM AOT codegen (`pkg/lang/codegen.go`) only emitted `printf` for the
first argument — `print(1, 2, 3)` silently dropped `2` and `3` in the AOT path,
a real interpreter-vs-AOT correctness gap. Emit one `printf("%d\n", v)` per
argument so multi-argument `print` behaves identically in both backends.

## Codegen/IR implications

- `print` now loops over all `Args`, lowering each to its `i32` register and
  emitting one `printf("%d\n", <reg>)` per argument, each on its own line.
- The call's result register is the last `printf`'s return (the interpreter
  treats `print` as void; callers ignore it).
- The AOT path stays i32-only and allocation-free — no new runtime support.
- Zero-argument `print` remains a codegen error (pre-existing), unchanged.

## Alternatives rejected

- Concatenating all args into a single `printf` with one format string —
  rejected: per-argument `printf` keeps the lowering simple, allocation-free,
  and exactly matches the interpreter's one-line-per-argument semantics.
- Documenting multi-argument `print` as an AOT limitation — rejected: dropping
  arguments is silent incorrectness, not a documented limitation; the runtime
  path should stay in sync with the interpreter.
