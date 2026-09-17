# CHANGELOG

All notable changes to gusty are documented here, newest first.
This project adheres to [Semantic Versioning](https://semver.org) with
pre-`1.0.0` releases (`v0.x.y`) while still in development.

## [v0.9.0] - 2026-09-17

The v0.9.0 release gathers every feature added since v0.8.2 into a single
milestone: a full Python-flavored language surface, an LLVM 20 AOT backend
that mirrors the interpreter, and a machine-readable CLI for agents.

### Added — language surface

- Indentation-aware lexer: `INDENT` / `DEDENT` tokens drive block nesting;
  `#` comments run to end of line.
- Functions: `def` with parameters, default and keyword arguments, and
  inferred return types.
- Anonymous functions: `lambda x: int: x + 1`, lowered to a closure exactly
  like `def` in both backends.
- Control flow: `if` / `elif` / `else`, `while`, `for ... in range(n)`,
  `range(a, b)`, `range(a, b, step)`, and `for x in [...]`; optional loop
  `else:` clauses; `break` / `continue`; `pass`.
- Pattern matching: `match` with integer equality, `_` wildcard cases, and
  list-destructuring patterns (`case [a, b]:`).
- Data structures: inline `list` / `dict` / `set` literals, indexing, and
  list / dict / set comprehensions.
- Generators: `def g(): yield a; yield b` collects the yielded values.
- Exceptions: `try` / `except` / `finally` with typed built-in exception
  classes (`Exception`, `ValueError`, `TypeError`, `KeyError`, `IndexError`,
  `RuntimeError`, `StopIteration`, `ZeroDivisionError`) and `raise`.
- Classes & inheritance: `class Name:` with `self` methods, instance
  attributes, `__init__`, multi-level `class Child(Base):` inheritance, and
  `super()` delegation.
- Decorators: `@dec def f:` applies `f = dec(f)` at definition time.
- Modules: `import mod` loads `mod.gy` and binds `mod` as a namespace.
- Gradual typing: optional type annotations on variables, parameters, and
  returns, statically checked by `--verify`; `any` is the dynamic escape
  hatch; untyped code falls back to dynamic dispatch.

### Added — standard library

- Core builtins: `len`, `print`, `range`, `sum`, `min`, `max`, `abs`,
  `sorted` (with `reverse`), `reversed`, `enumerate`, `zip`, `any`, `all`,
  `chr`, `ord`, `round`, and `int` / `float` / `str` conversions.
- String methods: `upper`, `lower`, `strip`, `replace`, `find`, `rfind`,
  `index`, `count`, `split`, `rsplit`, `join`, `partition`, `capitalize`,
  `title`, `swapcase`, `ljust`, `rjust`, `zfill`, `expandtabs`,
  `removeprefix`, `removesuffix`, `isdigit`, `isalpha`, `isalnum`,
  `isspace`, `islower`, `isupper`.
- Dict methods: `keys`, `values`, `items`, `get`.
- List methods: `append`, `count`.

### Added — LLVM 20 AOT codegen

- `codegen.go` + `closure.go` emit deterministic, opaque-pointer LLVM IR
  (LLVM 20 needs no `-opaque-pointers` flag) verified by `llc` and lowered to
  a native executable.
- The AOT backend lowers functions, `lambda` closures, control flow,
  arithmetic, comparisons, boolean `and` / `or`, `print`, list/dict/set
  literals, comprehensions, `len`, numeric builtins, and a wide range of
  constant-folding string/dict operations
  (e.g. `len("AbC".upper())` → `3`, `len("a b c".split())` → `3`,
  `{1: 2, 3: 4}.keys()` → `[1, 3]`).
- The AOT codegen folds constant-index element access into list-producing
  call expressions: `keys()`, `values()`, `sorted(...)` (including
  `reverse=True`), `reversed(...)` and `split(sep)` emit the exact selected
  element constant, matching the interpreter (see ADR 0109). `partition()`
  and `items()` stay compile-time errors (unrepresentable nested/string
  parts in the integer-element list model).
- The AOT codegen accepts a single scalar argument to `min`/`max`,
  treating it as a one-element collection: `min(5)` -> 5, `max(7)` -> 7
  (see ADR 0110).

### Added — CLI, REPL & agent interface

- `gustyc` CLI: `--eval`, `--file`, `--verify`, `--emit-llvm`, `--emit-ast`,
  `--target`, `--opt-level`, `--lang`, `--json`, `--schema`, `--version`,
  `--repl`, `--help`; plus a stateful REPL.
- Machine-readable output for agents: `--json` results/diagnostics,
  `--emit-ast` JSON AST dumps, `--emit-llvm` IR text dumps, and a `--schema`
  draft-07 JSON Schema describing the AST/IR dump shapes.
- Diagnostics carry source spans and messages
  (`{"span": {"line": 1, "col": 5}, "msg": "...", "severity": "error"}`).
- Deterministic exit codes: `0` ok, `1` runtime/eval error, `2` parse/usage
  error.
- Optimization: `--opt-level` runs compiler passes such as dead-global
  elimination over the emitted IR.

### Added — toolchain & tests

- Two execution backends kept in sync: the interpreter (`pkg/lang/jit.go`)
  and the LLVM AOT codegen (`pkg/lang/codegen.go` + `closure.go`).
- Integration suite drives the real pipeline — `source → codegen (IR) →
  llc-20` (module verification + object) `→ cc link → run` — and asserts the
  native binary's stdout matches the expected output.
- CI workflow installs LLVM 20 and runs the unit + integration suites.

## [v0.8.2] - 2023-05-08

- Base release of gusty with the indentation-based lexer, parser, semantic
  analysis, and LLVM codegen pipeline.
- CLI and integration-test scaffolding.

[Unreleased]: https://github.com/donutloop/gusty/compare/v0.9.0...HEAD
[v0.9.0]: https://github.com/donutloop/gusty/compare/v0.8.2...v0.9.0
[v0.8.2]: https://github.com/donutloop/gusty/releases/tag/v0.8.2
