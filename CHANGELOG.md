## [v0.??] - Phase 2: real IR optimizer
- Replaced the regex-ish `opt.go` text transform with a genuine IR pass
  pipeline (ADR 0088): a lightweight textual-LLVM-IR parser + CFG
  construction, then per-function passes to a fixed point:
  constant propagation + folding (arithmetic/compare/select/zext/sitofp/
  fptosi, plus branch folding), mem2reg-style alloca promotion (single-store
  dominating loads, plus dead-alloca/dead-store removal), dead-instruction
  elimination, and dead-block (unreachable CFG block) elimination.
- Output is re-serialized as valid textual IR for the whole emitted subset,
  verified end-to-end through `llvm-as`/`llc` in `TestOptimizedIRValidForLLC`.

## [v0.10.31] - reversed-list parity: interpreter vs AOT length
- Adds TestIRImportReversedListParity: `len(reversed(cfg.l))` runs through
  the interpreter (EvalExpr -> 3) and the AOT folds `len(cfg.l)` to 3,
  documenting that `reversed(cfg.l)` preserves the source list length in
  AOT data imports.

## [v0.10.30] - arithmetic on indexed imported dict globals
- `cfg.d[k] + cfg.d[j]` where `cfg.d` is an imported dict module global now
  folds to the sum in the AOT (Index resolves `*Attr` to folded dicts and
  BinOp folds the resulting literals), with an interpreter-vs-AOT parity
  unit test (TestIRImportDictArithInterpVsAOT).

## [v0.10.29] - len of imported dict module globals
- `len(cfg.d)` where `cfg.d` is an imported dict module global now returns
  the folded dict's entry count in the AOT `len` builtin (an `*Attr`
  expression resolves against the imports registry to a folded `DictLit`).

## [v0.10.28] - arithmetic on indexed imported list globals
- `cfg.l[i] + cfg.l[j]` where `cfg.l` is an imported list module global now
  folds to the sum in the AOT (Index resolves `*Attr` to folded lists, and
  BinOp folds the resulting literals), with an interpreter-vs-AOT parity
  unit test (TestIRImportListArithInterpVsAOT).

## [v0.10.27] - reversed of imported list module globals
- `reversed(cfg.l)` where `cfg.l` is an imported list module global now
  folds the reversed list in the AOT `reversed` builtin (an `*Attr`
  expression resolves against the imports registry to a folded `ListLit`).

## [v0.10.26] - indexing imported dict module globals
- `cfg.d[k]` where `cfg.d` is an imported dict module global now indexes
  into the folded dict in the AOT `Index` path (an `*Attr` expression
  resolves against the imports registry to a folded `DictLit`, comparing
  integer keys).

## [v0.10.25] - sorted of imported list module globals
- `sorted(cfg.l)` where `cfg.l` is an imported list module global now folds
  the sorted list in the AOT `sorted` builtin (an `*Attr` expression
  resolves against the imports registry to a folded `ListLit`).

## [v0.10.24] - len of imported list module globals
- `len(cfg.l)` where `cfg.l` is an imported list module global now returns
  the folded list's length in the AOT `len` builtin (an `*Attr` expression
  resolves against the imports registry to a folded `ListLit`).

## [v0.10.23] - dict module globals in AOT data imports
- AOT data imports now constant-fold **dict** module globals: `foldConst`
  accepts a `DictLit` whose keys and values fold recursively, so `mod.d`
  reads compile to folded dicts (e.g. `d = {1: 10}`).

## [v0.10.22] - indexing imported list module globals
- `cfg.l[i]` where `cfg.l` is an imported list module global now indexes into
  the folded list in the AOT `Index` path (an `*Attr` expression resolves
  against the imports registry before the literal-list path).

## [v0.10.21] - list module globals in AOT data imports
- AOT data imports now constant-fold **list** module globals: `foldConst`
  accepts a `ListLit` whose elements fold recursively, so `mod.list` reads
  compile to folded lists (e.g. `l = [1, 2, 3]`).

## [v0.10.20] - interpreter-vs-AOT parity for reversed import strings
- Adds TestIRImportReversedInterpVsAOT: `import msg; len(reversed(msg.msg))`
  runs through the interpreter (EvalExpr -> 6) and through the AOT compiler
  (IR folds len(reversed("hello!")) to 6), asserting both agree.

## [v0.10.19] - reversed of imported string module globals
- `reversed(mod.str)` where `mod.str` is an imported string module global now
  folds to the reversed string in the AOT `reversed` builtin (an `*Attr`
  expression resolves against the imports registry before the generic
  string-reversal path).

## [v0.10.18] - interpreter-vs-AOT comparison tests for import string globals
- Adds TestIRImportStringInterpVsAOT: runs `import msg; len(msg.msg)` through
  the interpreter (EvalExpr) and the AOT compiler, asserting both fold to the
  same value (len("hello!") == 6), covering the string module-global data
  import path end-to-end.

## [v0.10.17] - ord of imported string module globals
- `ord(mod.str)` where `mod.str` is an imported string module global now
  returns the folded string's first byte value in the AOT `ord` builtin
  (an `*Attr` expression resolves against the imports registry).

## [v0.10.16] - len of imported string module globals
- `len(mod.str)` where `mod.str` is an imported string module global now
  returns the folded string's length in the AOT `len` builtin (an `*Attr`
  expression resolves against the imports registry before the generic
  string-length path).

## [v0.10.15] - print of imported string module globals
- `print(mod.str)` where `mod.str` is an imported string module global now
  emits `printf("%s", i8*)` and outputs the string. `stringVal` resolves an
  `*Attr` expression against the AOT imports registry (folded module
  globals), so folded string constants print correctly (previously they fell
  into the integer `%d` path and produced invalid IR).

## [v0.10.14] - import mod: string globals & concat in AOT data imports
- AOT data imports now constant-fold **string** module globals and
  **string concatenation** (`"a" + "b"` -> `"ab"`) in module global
  expressions, so layered string constants (e.g. `greet = "hello"`,
  `msg = greet + "!"`) compile to folded constants.

## [v0.10.13] - import mod: nested imports in AOT (data imports)
- AOT `import mod` now supports **nested imports**: a module that itself
  `import other` compiles by recursively constant-folding the nested module's
  globals, and `other.var` references resolve in the parent module's global
  expressions.
- `resolveImports` now ignores Analyze *warnings* (e.g. "arithmetic on
  non-numeric operands" on module-attr operands, which fold to numbers at
  compile time) and rejects only on true semantic errors.

## [v0.10.12] - import mod in AOT (data imports)
- `import mod` now compiles in the AOT backend: `mod.gy` is parsed, analyzed,
  and its top-level global variables are constant-folded to literals, so
  `mod.var` reads resolve statically to compile-time constants.
- Module function dispatch is deferred with a clear compile error
  ("module functions are not yet supported in AOT imports").
- The `deadGlobalElim` optimizer pass now keys function-body start on the
  first `define` line and counts every embedded reference in a global
  definition line, so literal globals (e.g. @.strN) after internal globals
  (@exn_flag, @env_store) are pruned when dead.

## [v0.10.11] - try/except/finally/raise in the AOT/LLVM backend
- Compile try/except/finally and raise statements to LLVM IR (ADR 0111).
- Add @exn_flag/@exn_code globals and a raise-exit label per function so
  exceptions propagate across user-function calls to the nearest except handler.
- Add integration tests (TestExecTryExcept) covering bare except, finally,
  specific except matching, and cross-function raise propagation.

# CHANGELOG


## [v0.10.10] - print(chr(const)) valid %s printf

- print(chr(65)) previously fed the chr string-global array as i32 to a
  %d printf (llc: global variable reference must have pointer type).
  stringVal now recognizes chr(IntLit) as a string, so print emits a %s
  printf with the chr global pointer. Adds TestIRPrintChrConst and
  TestExecPrintChrConst.

## [v0.10.9] - int(float var) / float(int var) AOT parity

- Locks AOT binary conversion parity: int(3.9) truncates to 3, int(-3.9)
  to -3, float(2) widens to 2.0. Adds TestExecIntFloatConv.

## [v0.10.8] - negative float floor/mod/neg AOT verification

- Locks AOT binary parity for negative floats: -3.5//2.0 == -2 (llvm.floor),
  -3.5%%2.0 == -1.5 (frem), abs(-3.5) == 3.5 (fabs), round(-3.5) == -4
  (llvm.round). Interpreter already matched; adds TestExecFloatFloorModNegNeg.

## [v0.10.7] - abs(float) interpreter parity

- Interpreter abs() now negates a negative float64 payload instead of
  returning the boxed heap handle unchanged (abs(-3.5) gave -3.5, now 3.5).
  AOT already emitted llvm.fabs.
- Tests: TestEvalAbsFloat, TestExecAbsFloat.

## [v0.10.6] - round(int variable) AOT parity

- AOT round() now passes an int variable through (identity) instead of
  erroring "round: codegen folds only a constant integer arg". round(a) for
  an int var emits the value unchanged; round of a string still errors.
- Tests: TestIRRoundIntVar, TestExecRoundIntVar.

## [v0.10.5] - round(float var) AOT parity

- AOT round() now supports float variables: emits llvm.round.f64 + fptosi
  (round-half-away), matching interpreter math.Round. Previously round()
  errored with "folds only a constant integer arg" for a float variable.
- Tests: TestIRRoundFloatVar, TestExecRoundVar.

## [v0.10.4] - round() float parity

- Interpreter `round(float)` now uses math.Round (half-away-from-zero), matching
  the AOT codegen constant-folded math.Round. Previously it truncated toward
  zero (round(2.5) gave 2 instead of 3).
- Tests: TestEvalRound, TestExecRound.

## [v0.10.3] - float floor/mod/neg parity

- **Float `//` floor division, `%` remainder, unary `-`** (interpreter + AOT):
  interpreter now floors `//` (`math.Floor`), uses `math.Mod` for `%`, and
  negates the float64 payload for `-`. AOT emits `llvm.floor.f64` for `//`,
  `frem` for `%`, and `fsub double 0.0` for unary `-` on floats, with
  `isFloat`/`floatValue` extended to recognize `//`, `%`, and negation.
- **AOT float-variable alloca** fixed: float vars are now `alloca double`
  (previously `alloca i32` while storing doubles — latent type bug).
- Integration tests link with `-lm` (frem needs libm).
- Tests: `TestEvalFloatFloorModNeg`, `TestIRFloatFloorModNeg`,
  `TestExecFloatFloorModNeg`.

## [v0.10.2] - interpreter float sub/mul parity

- **Float `-` and `*` arithmetic** (roadmap floats gap): the interpreter now
  operates on the float64 payload of boxed floats instead of multiplying or
  subtracting the raw heap handles (small ints). `a = 1.5; b = 2.0` now gives
  `a-b == -0.5`, `b-a == 0.5`, `a*b == 3.0`, `b*a == 3.0`, `a+1 == 2.5`,
  `2*a == 3.0` — matching the AOT codegen (`--emit-llvm` already emitted
  `fsub`/`fmul`).
- Interpreter test `TestEvalFloatSubMul` asserts exact float payloads via
  `floatOf`.

## [v0.10.1] - float str() print parity

- **`print(str(float-const))` AOT parity** (roadmap gap): `str(3.5)` now
  folds to its `%g` decimal string constant (matching the interpreter's
  `repr`), so `print(str(3.5))`, `print(str(1.0 + 2.0))`, and mixed
  `print(1, str(3.5), 2)` emit a valid `%s` printf fed the string-global
  pointer. Previously the print path fell through to the `%d` branch and fed
  an `i8*` to printf, which `llc-20` rejected with "global variable reference
  must have pointer type".
- Unit tests (`TestIRPrintStrFloatIsValid`) and integration tests
  (`TestExecPrintStrFloat`) lock in the fix.

## [v0.10.0] - roadmap phase 0

- **Version hygiene (roadmap Phase 0)**: reconcile the stale compiler version
  constant (`pkg/lang/compile.go` said `0.1.0` while the changelog said
  `v0.9.0`). Bumped to a **new version `0.10.0`** for the roadmap work; the
  CLI prints `0.10.0`. The version lives in one place (`pkg/lang/compile.go`)
  and is used by `cmd/gustyc`.
- Added `roadmap.md` (LLVM-core component map, gaps, phased plan) and
  `docs/roadmap.md` pointer.
- Removed the unused vendored `go-llvm/` bindings and their `go.mod`/`go.sum`
  deps; codegen remains a textual IR emitter verified by external `llc`/`cc`.


## [Unreleased] - 2026-09-17

### Added — CLI multi-file build

- `gustyc --build <out> <file1> <file2> ...`: compile a set of source files
  into a single native executable. Pipeline: parse + merge the sources into
  one program, semantic analysis, LLVM IR codegen, `llc-20` lowers/verifies the
  module to an object file, `cc` links it into the binary at `<out>`.
- `--json --build` emits a machine-readable `BuildResult` (`output`, `ir`,
  `objects`, `commands`, `diagnostics`).
- `lang.Build(files, out, optLevel)` public API in `pkg/lang` for in-process
  builds; unit tests (`build_test.go`) and whole-program CLI integration tests
  (`integration/build_test.go`).

### Fixed — string-constant array sizes in LLVM codegen

- `fmtStr` returned the escaped IR length (`len(f)+1`) for the printf format
  GEP while emitting a `[len(format)+1 x i8]` global, producing a mismatched
  GEP (`[6 x i8]` over a `[4 x i8]` constant). It now returns the decoded byte
  count `len(format)+1` so the GEP matches the emitted array.
- `strConst` sized the emitted string global with the escaped length
  (`len(esc)+1`); it now uses the decoded raw length `len(s)+1`.


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
