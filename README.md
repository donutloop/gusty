# gusty

> A statically-typed, Python-flavored programming language compiled ahead-of-time through LLVM.

gusty is a small, modern programming language that pairs Python's familiar
indentation-based syntax and ergonomic feel with the performance of native
compiled executables. Source goes through a clean, inspectable pipeline —
`lex → parse → semantic (type inference) → codegen` — down to LLVM IR, which
is verified, optimized, and lowered to a native binary, or JIT-executed in
the REPL.

The toolchain is built for **both humans and agents**: a friendly REPL/CLI for
people, plus structured, machine-readable output (JSON diagnostics, JSON AST/IR
dumps, a JSON Schema, stable flags, deterministic exit codes) so scripts and AI
workflows can discover and consume the language without guessing.

---

## A taste of the language

```python
print(40 + 2)                # -> 42

x = 5
print(x + 1)                 # -> 6

if x < 2:
    print(10)
else:
    print(20)

s = 0
for i in range(5):           # range(n) and range(a, b)
    s = s + i
print(s)                     # -> 10

def double(x):
    return x * 2
print(double(5))             # -> 10

x = 2
match x:
    case 1:
        print(1)
    case 2:
        print(2)             # -> 2

i = 0
while i < 100:
    i = i + 1
    if i == 3:
        break
print(i)                     # -> 3
```

## Language surface

gusty ships an indentation-based syntax covering:

- **Functions** — `def` with default/keyword args, inferred return types, and
  anonymous `lambda` functions (`lambda x: int: x + 1`) lowered to closures
  exactly like `def`.
- **Control flow** — `if` / `elif` / `else`, `while`, `for ... in range(n)`
  / `range(a, b)` / `range(a, b, step)`, `for x in [...]`, optional loop
  `else:` clauses, `break` / `continue`, and `pass`.
- **Pattern matching** — `match` with integer-literal equality, `_` wildcards,
  and list-destructuring patterns (`case [a, b]:`). Matches also support
  guards (`case x if cond:`), or-patterns (`case 1 | 2:`), dict patterns
  (`case {"k": v}:`), and class patterns (`case Point(x, y):` with subclass
  walk + attribute binding).
- **Data structures** — inline `list` / `dict` / `set` literals, indexing, and
  list / dict / set comprehensions.
- **Slicing** — `s[a:b]`, `s[::step]`, negative indices; supported in both the
  interpreter and the AOT backend (via the `rt_slice` runtime helper).
- **Generators** — `def g(): yield a; yield b` collects yielded values.
- **Context managers** — `with expr as name:` / `with expr:`, dispatching
  `__enter__` / `__exit__` (including exception suppression); supported in
  both backends.
- **Exceptions** — `try` / `except` / `finally` with typed built-in exception
  classes (`Exception`, `ValueError`, `TypeError`, `KeyError`, `IndexError`,
  `RuntimeError`, `StopIteration`, `ZeroDivisionError`) and `raise`.
- **Classes & inheritance** — `class Name:` with methods (`self`), instance
  attributes, `__init__`, `class Child(Base):` multi-level inheritance, and
  `super()` delegation.
- **Operator overloading** — binary operators dispatch to dunder methods
  (`__add__`, `__mul__`, `__lt__`, ...) with reflected fallbacks
  (`__radd__`, `__rmul__`, swapped comparisons), in both the interpreter and
  AOT codegen.
- **Decorators** — `@dec def f:` → `f = dec(f)` at definition time; wrapping
  (fnptr-valued) decorators compile in AOT via compile-time specialization.
- **Modules** — `import mod` loads `mod.gy` and binds `mod` as a namespace with
  `mod.name` / `mod.fn(args)` access.
- **Standard library** — data-only on-disk modules folded as AOT constants:
  - `import math` — `PI`, `E`, `TAU`, `PHI`, `SQRT2`, `LN2`, `LN10`.
  - `import string` — `DIGITS`, `LOWERCASE`, `UPPERCASE`, `HEXDIGITS`,
    `WHITESPACE`, `PUNCT`.
  - `import collections` — `EMPTY_DICT`, `EMPTY_LIST`, `ZERO`, `ONE`.
  - `import json` — `NULL` (`None`), `TRUE` (`True`), `FALSE` (`False`).

## Gradual typing & the type system

Optional annotations on variables, parameters, and returns are checked
statically by `--verify`, with `any` as the dynamic escape hatch; untyped code
falls back to dynamic dispatch.

- **Union types** — `int | str`, `int | float`, and `None | int` sugar for
  `Optional`; inferred and checked across assignments, call boundaries, and
  returns. In AOT, a union-annotated scalar variable gets a tagged `%unionbox`
  slot (runtime member tag 0=int, 1=float, 2=str) so `print` dispatches on the
  live member — an `int` member prints as `%d`, a `float` as `%f`, a `str` as
  `%s`, even after cross-member reassignment under branches/loops.
- **Literal types** — `Literal[1, 2]` annotations feed `match`
  exhaustiveness + narrowing on constants.
- **Type narrowing** — after `if isinstance(x, int):`, the checker narrows `x`
  from `any` to `int` in the then branch and away from it in the else branch;
  `not isinstance(x, T)` flips those; union complement narrowing uses
  `dropType`.
- **Walrus operator** — assignment expressions `name := expr` usable inside
  `if` conditions and comprehensions (`if (n := len(x)) > 0:`), scoped per
  Python 3.8+.

## Modern front-end (lexer & parser)

- **Error-recovering lexer** — on an unexpected character, emits a `TokError`
  token carrying the span + message and *resumes* instead of aborting the file,
  so the parser/semantic pass can report multiple diagnostics per run.
- **Rich token spans** — each token carries `start` AND `end` (byte + rune
  offsets) plus an optional multi-line flag, giving f-strings, slices, and
  `match` patterns exact ranges for hover/diagnostics/formatting.
- **Unicode identifiers** — identifiers scan by Unicode `ID_Start`/`ID_Continue`
  (not just ASCII), NFC-normalized via `golang.org/x/text` so decomposed and
  precomposed spellings are one symbol, and a `TokWarning` diagnostic flags
  Greek/Cyrillic homoglyph lookalikes (e.g. `Ο` U+039F vs Latin `O`).
- **Numeric-literal modernization** — hex (`0xFF`), binary (`0b101`), octal
  (`0o17`), and `_` digit separators (`1_000`, `0x_FF`), with exact integer
  semantics and rejection of misplaced separators.
- **Raw & triple-quoted strings** — `r"..."` / `R'...'` raw strings and
  `"""..."""` / `'''...'''` multi-line strings; docstring extraction reuses
  both forms.
- **Line continuation** — a trailing `\` joins the next physical line into one
  logical line (Python-compatible), skipping the continued line's leading
  indentation and blank/comment-only continuation lines.
- **async/await + effectful syntax (L5.6)** — `async def`, `async for`, `async with`, and `await expr` parse as first-class syntax; under the minimal synchronous-coroutine model (no suspension primitives yet) they lower identically to their sync counterparts in both the AST interpreter and the LLVM AOT/JIT backends, giving exact parity (see `async_basic.gy`). The cooperative event-loop runtime is Phase 7.
- **Pratt parser** — a precedence-climbing expression parser keyed off a
  precedence table (unary, `**` right-assoc, multiplicative, additive,
  comparison, `and`/`or`, ternary) with panic-mode recovery
  (`recoverStmt` — nest-aware `INDENT`/`DEDENT` skipping) producing a forest
  of `*ParseError`s and a partial AST.
- **Trailing commas** — `f(a, b,)`, `[1, 2,]`, `{1: 2,}` and `match` case arg
  lists tolerated, and normalized away by the canonical formatter.

## Two execution backends

Every feature ships in **both** paths:

- **Interpreter** — `pkg/lang/jit.go`, entry `EvalExpr`: the REPL / `--eval` /
  `--verify` path. Fast feedback, rich diagnostics; a two-generation
  (nursery + old) tracing GC (`ev.Collect()`) reclaims unreachable pure-data
  heap objects; roots are top-level bindings, walking container elements,
  dict values, closure envs, and attr tables. Classes/methods/closures/imports
  are never freed.
- **LLVM AOT codegen** — `pkg/lang/codegen.go` + `pkg/lang/closure.go`, entry
  `Compile`: the `--file` / `--emit-llvm` / link-and-run path. Emits
  deterministic opaque-pointer LLVM IR with real `double` float IR (float
  arithmetic via `sitofp` promotion, `%.17g` float print), tagged-union
  lowering, `__doc__` folding to string constants, string slicing via
  `rt_slice`, literal-container membership via `rt_contains`, and
  `with`/yield-from runtime protocols.

## Optimization pipeline

- **Constant folding** — integer-literal binops fold at codegen time
  (`x = 1 + 2` emits `store i32 3`, no `add`).
- **Escape-analysis heap elision** — never-read top-level list literals skip
  their runtime heap allocation (`[x * 2 for x in ...]`,
  `{k: v for ...}`, `{x for ...}`).
- **Dead-global / dead-object elimination** — unused `@.strN` / `@.lstN`
  globals and dead heap objects are pruned.
- **Real LLVM `opt` pipeline** — `pkg/lang/opt_llvm.go` drives the external
  `opt-20` tool over the raw module IR (instcombine, gvn, licm, sroa,
  simplifycfg, ...), so AOT emits verified, optimized IR — while preserving
  GC-correctness by rooting heap slots through module-global arrays
  (`@gc.roots` / `@gc_roots_used`).

## CLI

`gustyc` is the command-line interface and REPL:

```
gustyc --eval "x = 2 + 3\nx"                 # evaluate source, print result
gustyc --file prog.gy                        # compile & run a source file
gustyc --build out a.gy b.gy                 # compile a set of files into a binary
gustyc --verify "def f(x): return x * 2"     # static analysis only
gustyc --emit-llvm "x = 1 + 2"               # print emitted LLVM IR
gustyc --emit-ast "x = 1"                    # print the AST as JSON
gustyc --emit-source-map --file src.gy       # JSON source map (fn -> IR symbol+line)
gustyc --check <src> | check file1.gy ...    # mypy-style type-check without executing
gustyc --json ...                            # machine-readable JSON output
gustyc --schema                              # print the JSON Schema for AST/IR dumps
gustyc --lang                                 # self-describing feature list
gustyc --jit "..."                           # in-process dlopen JIT path
gustyc --bench '<src>' --bench-runs N --bench-opt L   # wall-clock benchmark
gustyc --fmt <src> | --fmt-check <src> | --fmt-file <path>  # canonical formatter
gustyc --lsp                                  # stdio language server (hover, completion, diagnostics)
gustyc --stdlib <dir>                        # set stdlib root (default: ./stdlib)
gustyc --version                             # compiler version
gustyc                                      # start the interactive REPL
```

Exit codes are deterministic:

| Code | Meaning |
|------|---------|
| 0    | success / clean (check, fmt-check) |
| 1    | compile / type error, runtime/eval error |
| 2    | LLVM/verification failure or parse/usage error |

## Testing & verification

- **Unit tests** — lexer, parser, semantic, codegen, and runtime tests in
  `pkg/lang/`; benchmarks (`Benchmark*`) and Go-native fuzz targets
  (`Fuzz*`) for the interpreter.
- **Integration suite** — `integration/` drives the full pipeline
  (lex → parse → typecheck → codegen → run) and asserts stdout matches
  expected output.
- **Conformance matrix** — `integration/conformance_cases.go` +
  `conformance-matrix.json`: **26 conformance cases**, all passing
  (`pass: 26, fail: 0`) across both backends (interpreter and AOT).
- **Property testing** — seeded deterministic whole-program generation.

## Requirements & build

- **LLVM 20** — `llc-20` for lowering and `opt-20` for the optimization
  pipeline (the AOT backend emits textual opaque-pointer IR verified by these
  external tools).
- **Go** — build with the LLVM 20 tag:

```sh
go build -tags=llvm20 ./...
go test -tags=llvm20 ./pkg/...        # unit tests
go test -tags=llvm20 ./integration/... # end-to-end pipeline
```

- **C linker** — `cc` / `gcc` to link the emitted native object into a binary.

## Repository layout

```
pkg/lang/          compiler: lexer, parser, semantic (types), codegen, jit runtime
cmd/gustyc/        the CLI + REPL
integration/       end-to-end pipeline + conformance suite
docs/              language spec (language.md) + operations guide (operations.md)
stdlib/            on-disk modules: math.gy, string.gy, collections.gy, json.gy
```

---

*The language spec lives in `docs/language.md`; the toolchain & CLI reference
in `docs/operations.md`.*
