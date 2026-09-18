# ADR 0112: `import mod` in the AOT backend — data imports (constant globals)

## Context
The roadmap's AOT-parity list after exceptions is modules/imports → classes →
generators. The interpreter already supports `import mod` with full module
object semantics (top-level globals *and* functions). The AOT codegen had no
import support at all (`ImportStmt` fell through to "unsupported statement").

## Decision
Support **data imports** in the AOT backend: `import mod` loads `mod.gy`
(relative to CWD, mirroring the interpreter), parses + analyzes it, and
constant-folds the module's top-level global assignments (literals, arithmetic,
references to earlier module globals). `mod.var` reads are resolved statically
in the codegen `value` method: an `Attr{Name{mod}, name}` expression emits the
folded constant directly. `ImportStmt` emits no IR (skipped).

Module function dispatch, nested imports, and non-constant globals are deferred
with clear compile errors ("module functions are not yet supported in AOT
imports", "global X is not a compile-time constant"). This keeps the subset
crisp, correct, and fully testable while closing the biggest import gap.

## Consequences
- `import config; print(config.base + 2)` compiles to IR containing the folded
  constants; module data (config/constants) is now usable in compiled programs.
- Module functions remain interpreter-only; a clear error guides users.
- `resolveImports` runs inside `GenerateIR` (so `Build` multi-file paths also
  benefit), reading `.gy` files at compile time exactly like the interpreter.
