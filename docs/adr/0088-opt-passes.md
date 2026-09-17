# ADR 0088: compiler-level IR optimization passes (--opt-level)

## Decision

Make `--opt-level` a real optimization switch in the compiler, not just
informational. `pkg/lang.OptimizeIR(ir, level)` runs pure-Go, portable passes
over the emitted LLVM IR text before it is printed by `--emit-llvm`:

- level 0: returns IR unchanged.
- level >= 1: dead-global elimination — prunes global definitions
  (`@.strN`, `@.lstN`, `@.dictN`, `@.fmtN`, ...) that are never referenced by
  the function body.

## Motivation

The mission's "optimization passes" item was unimplemented: the CLI accepted
`--opt-level` but ignored it (it only printed an informational comment). The
codegen eagerly emits a global whenever a literal is lowered; if that
literal's value is unused (e.g. a pure ExprStmt like `"hi"`), the global is
dead. LLVM's optimizer would remove it; this pass does so at emission time.

## Portability

The compiler is pure Go (text IR emission; LLVM is invoked only as external
binaries like `llc`). The pass pipeline operates on IR text with regex
scanning, so no cgo/LLVM linking is required. This is deliberately smaller
than linking LLVM's `opt` into the compiler (ADR 0001 kept the compiler
portable).

## CLI usage

`--emit-llvm` is a string flag that consumes the next argument as its value,
so `--opt-level` must precede it:

    gustyc --opt-level=1 --emit-llvm='"hi"'

The only level-1 output difference for a program with no dead globals is the
informational `; opt-level = 1` comment line.

## Tests

- `pkg/lang/opt_test.go`: level 0 is identity; a dead `@.str0` global is
  pruned while a referenced `@.str1` is kept; a real emitted IR module keeps
  `main()` and the `printf` call.
- `cmd/gustyc/main_test.go` (`TestCLIEmitLLVMOptLevel`): `--opt-level=1`
  prunes the dead string global from `--emit-llvm='"hi"'`.

## Alternatives rejected

- Linking LLVM's pass manager (go-llvm) into the compiler: rejected — the
  compiler is pure Go; the external `llc`/`opt` binaries already apply full
  LLVM optimizations in the backend.
