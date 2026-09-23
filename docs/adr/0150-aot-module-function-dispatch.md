# ADR 0150 — AOT module-function dispatch

## Status

Accepted.

## Context

`import mod` previously folded module statements to data-only globals in the
AOT compiler, and any module `def` was rejected. The interpreter already
supported module functions via closure wrapping; AOT did not.

## Decision

Allow AOT imports to contain `def` statements:

- `resolveModule` now records module `*FuncDef`s into `ImportInfo.Funcs`
  (`module -> fn name -> *FuncDef`), alongside folded global constants.
- Each module function is lowered as a standalone IR define with a mangled,
  collision-free label `mod$fn` via `emitModuleFuncs`.
- `mod.fn(args)` calls dispatch to `@mod$fn` (direct static call).
- Inside a module function body, bare `Name` references to sibling module
  functions dispatch to `@mod$fn`, and bare `Name` references to module-global
  constants resolve to their folded values (closure-env capture), unless a
  parameter shadows the name.

## Consequences

- AOT and interpreter import behavior are now aligned for functions.
- Module functions are statically linked at compile time, so no runtime
  closure/registry overhead is needed for the common case.
- References to mutable module globals (non-constant) still require the data
  folding to be compile-time-constant; runtime-mutable module state is out of
  scope for the AOT path.
