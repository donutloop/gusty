# ADR 0113: nested imports in the AOT backend — data imports

## Context
ADR 0112 introduced data imports (constant module globals) in AOT. The first
implementation rejected nested imports inside a module with a clear error.
Modules that import other modules (config layered over base) are common and
valuable.

## Decision
AOT `import mod` now resolves nested imports recursively: `resolveModule` folds
a module's top-level globals, allowing `other.var` references to resolve
against a shared registry of folded module globals (`Attr{Name{other}, var}` in
`foldConst`). Analyze *warnings* (e.g. "arithmetic on non-numeric operands" on
module-attr operands that fold to numbers at compile time) are ignored;
`resolveImports` rejects only on `LevelError` diagnostics.

## Consequences
- Layered data modules (a module importing another module) compile correctly.
- The shared registry keeps folded constants across modules; the main program's
  `mod.var` reads still emit the folded constant via the codegen `value` case.
- Module function dispatch and non-constant globals remain deferred.
