# ADR 0141: Docstrings and `__doc__`

Status: accepted

## Context

Python attaches a docstring to callables and classes: a leading bare string
literal in a `def`/`class` body is captured as `__doc__` at definition time.
This is an introspection feature that completes the Python-like ergonomics of
the language surface (Phase 8 roadmap: "Docstrings / `__doc__`").

## Decision

- **Parser/lexer**: no new tokens. `parseFuncDef` and `parseClassDef` pull a
  leading `ExprStmt(*StrLit)` out of the body into `FuncDef.Doc` /
  `ClassDef.Doc`. A string literal that is *not* the first statement is a
  normal expression statement, not a docstring.
- **AST**: `FuncDef` and `ClassDef` gain a `Doc string` field (JSON `doc`).
- **Interpreter** (`jit.go`):
  - Top-level functions live in `e.funcs` as AST nodes; `f.__doc__` resolves
    directly to `e.funcs[name].Doc` (no runtime object exists for them).
  - Closures carry `obj.doc` set from `fn.Doc` at `allocClosure`.
  - Classes carry `obj.doc` set at `ClassDef` evaluation.
  - Attribute reads check `__doc__` before the kind-specific attr logic.
- **Formatter** (`fmt.go`): docstrings are re-emitted as the first body
  statement (`writeDoc`) so formatting round-trips preserve them.

## AOT limitation

The AOT/codegen backend lowers functions and classes to compile-time artifacts
(class ids, separate method functions) with no runtime introspection objects.
`__doc__` is therefore **interpreter-only**. This mirrors the existing
interpreter-only limits for decorators and nested closures (ADR 0131). Users
of the AOT backend get docstring *capture* (the leading literal is dropped from
the body) but not `__doc__` reads.

## Consequences

- `f.__doc__` returns the docstring or `""` if none; `cls.__doc__` likewise.
- Docstrings are not re-evaluated as no-op statements at runtime.
- The formatter preserves docstrings through parse/format round-trips.
