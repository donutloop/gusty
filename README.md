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
  `else:` clauses, `break` / `continue`, and `pass`. A parameter is a local
  variable (assigning to one is ordinary and local), and a `for` loop binds its
  variable per element — it keeps the last value bound, and writing to it does not
  move the iteration (ADR 0196).
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
- **Async** — `async def` / `await` / `async for` / `async with`, with the await/return
  discipline checked in the shared front end: dropping a coroutine, awaiting one twice, or
  yielding inside an `async def` is a compile error, and each function's effect signature is
  readable with `gustyc --effects` (ADR 0195).
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

## The value model

Behind the annotations, a runtime value is one of fifteen kinds — `int`, `float`, `bool`,
`None`, `str`, `list`, `dict`, `set`, `tuple`, `class`, `instance`, `method`, `closure`,
`exn`, `module` — and one Go table says which. The interpreter's heap objects, the compiled
runtime's tagged values, the exported C ABI and the garbage collector's root tracing all
read those numbers, and the compiled heap's own object-header kind is a projection of them
(`list`, `dict`, `set`, `instance`, with 0 meaning "not allocated — an immediate or an
interned string"). `gustyc --lang` prints both tables and `--schema`'s `valueTag`
definition documents the numbering (ADR 0182).

A compiled list can mix numbers, strings and `None`: `print([1, "a", None])` prints
`[1, 'a', None]` on both backends, because each element slot carries its own tag (ADR 0184).

Where the tag does not decide yet, one rule does: `str(x)` folds to the same text on both
backends and on CPython — `str(None)` is `"None"`, `str(1.5)` is `"1.5"`, `str("x")` is `x`
(ADR 0183).

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
- **Variance + generics (L6.6)** — one subtyping relation implements a declared
  variance table: `list[T]` / `set[T]` / `dict[K, V]` are **invariant** (they are
  writable), `Sequence[T]` / `iter[T]` / `tuple[...]` are **covariant** (read-only,
  so an element type may widen), `Callable[[P...], R]` is **contravariant** in its
  parameters and covariant in its return, and user classes are **nominal** —
  `a: Animal` accepts a `Dog` because the declared base chain says so. A freshly
  built container literal may widen its element type to the destination
  (`x: list[int | str] = [1]`). Every rejection names its rule and carries a
  stable `code` (`type.variance.invariant`, `type.variance.contravariant`, …) plus
  an actionable `suggestion`; the whole model is machine-readable via
  `gustyc --variance`.
- **`print` behaves like Python's** — `print("n =", 42)` writes `n = 42`, not two
  lines: arguments are joined with `sep=" "` and terminated by `end="\n"` (both
  honoured for every argument kind, including runtime containers, whose printers
  take the newline as a flag rather than baking it in). Interpreter and AOT agree
  byte-for-byte, including how an argument that prints interleaves with its line
  (ADR 0165).
- **Containers are references everywhere** — pass a `list`, `dict` or `set` to a
  function as a literal, variable, keyword argument, default, comprehension or
  generator result and the callee sees the same live object on both backends:
  the AOT backend materialises container literals into the runtime heap and
  infers each parameter's container kind from annotations, defaults and call
  sites (forwarding included), so `for x in xs`, `len(xs)`, `xs[i]` and
  `xs.append(v)` work on parameters exactly as on variables. Binding one is the
  same story (ADR 0163): `ys = [x * 2 for x in [1, 2]]` — even constant-folded,
  even at module scope — yields a rooted heap handle, so `print`, `len`, indexing,
  iteration and calls all see the container, not a folded global's address.
- **The verifier is a pipeline stage (L8.2)** — the AOT backend emits textual IR, so
  `Build` runs LLVM's own module verifier (`opt -passes=verify`, `llc -filetype=null`
  fallback) over the module it is about to link and reports the verdict in
  `BuildResult.verification`; `gustyc --verify-llvm <src>` exposes it as a
  machine-readable record (`ok`/`tool`/`skipped`/`pipeline`/`errors`/`note`) so an
  agent can tell "the compiler emitted bad IR" apart from "my program is wrong" —
  without scraping `llc` output. A missing toolchain is reported as `skipped`, never
  as a pass. Turning it on is how Gap I.3 was found.
- **The corpus has a third opinion (L11.9)** — parity between the two backends can be satisfied
  by two implementations that share a bug, and for a hundred ADRs it was. The conformance matrix
  runs each program through the interpreter, the compiled binary **and CPython**, and each case
  declares its state in a ledger (`match` by default, `debt` with a reason, an owner and a pin of
  the wrong answer, or `not_applicable` for gusty-only surface). Drift fails the build in both
  directions. `gustyc --oracle '<src>'` exposes the same classifier interactively — `--json` for
  the leg-by-leg report, exit 6 when gusty disagrees with Python and 7 when the oracle could not
  judge the source (ADR 0186).
- **Benchmark suite + regression gate** — `gustyc --bench-suite` measures a
  corpus on both backends and prints (or `--json`-emits) a stable artifact;
  `--bench-baseline` gates a run against a saved baseline, so "the compiler got
  slower" is a number with its own exit code (5) instead of a hunch.
  `--bench-dir integration/programs` benchmarks the parity programs too.

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
gustyc --oracle '<src>' | --oracle-file prog.gy  # interpreter + compiled backend + CPython, one verdict
gustyc --json ...                            # machine-readable JSON output
gustyc --schema                              # print the JSON Schema for AST/IR dumps
gustyc --lang                                 # self-describing feature list
gustyc --variance                             # JSON variance table (list/dict invariant, Sequence covariant, Callable params contravariant)
gustyc --effects <src> | effects file1.gy ...  # per-function effect signatures: effects performed, return shape, termination (--json for the document)
gustyc --jit "..."                           # in-process dlopen JIT path
gustyc --gc-stats --file prog.gy             # report what the collector did (stderr; --json adds a gc member)
gustyc --bench '<src>' --bench-runs N --bench-opt L   # wall-clock benchmark
gustyc --fmt <src> | --fmt-check <src> | --fmt-file <path>  # canonical formatter
gustyc --lsp                                  # stdio language server (hover, completion, diagnostics)
gustyc --stdlib <dir>                        # set stdlib root (default: ./stdlib)
gustyc --version                             # compiler version
gustyc                                      # start the interactive REPL
```

Exit codes are deterministic (full contract in `docs/operations.md` § Exit codes):

| Code | Meaning |
|------|---------|
| 0    | success / clean (check, fmt-check, an `--oracle` run that matches CPython) |
| 1    | compile error — parse, analysis, a codegen refusal, or `llc`/`cc` failed; the program never ran |
| 2    | LLVM rejected the module *we* emitted (a compiler bug, not a source error) |
| 3    | runtime error — the program compiled, ran, then trapped |
| 4    | CLI usage error (bad/unknown flags, no source, unreadable file) |
| 5    | benchmark regression (the `--bench-baseline` gate fired) |
| 6    | the oracle leg: the program ran and printed something other than what CPython prints |
| 7    | the oracle leg: CPython could not run the source, so there is no verdict |

## Testing & verification

- **Unit tests** — lexer, parser, semantic, codegen, and runtime tests in
  `pkg/lang/`; benchmarks (`Benchmark*`) and Go-native fuzz targets
  (`Fuzz*`) for the interpreter.
- **Integration suite** — `integration/` drives the full pipeline
  (lex → parse → typecheck → codegen → run) and asserts stdout matches
  expected output.
- **Conformance matrix** — `integration/conformance_cases.go` +
  `conformance-matrix.json`: **66 rows over three legs** — the AST interpreter, the LLVM AOT
  binary, and **CPython** — for 48 parity cases plus 18 pinned probes. Parity (interpreter ==
  AOT) is necessary but not sufficient: two backends that share a bug agree, and for this
  project's history they did (`print(True)` printed `1` everywhere, `len("café")` printed `5`).
  A row is conformant when both backends print what CPython prints. Each case *declares* its
  state — `match` (the default), `debt` (with a reason, a roadmap owner, and a per-leg pin of
  the wrong answer), or `not_applicable` (gusty-only surface the oracle cannot run) — and drift
  fails the build in both directions, so a new divergence and an unrecorded fix are equally
  caught (roadmap L11.9, ADR 0186). Corpus growth follows a standing rule (ADR 0190): every feature
  ships its **least interesting** program — the tutorial one, `print([1, 2])`, `xs.sort()` — because
  a corpus grown from bug reports only tests what we already had reason to doubt. The oracle's first catch was not a refusal but a passing
  build: `xs[0] = "z"` on a mixed list answered `[1, 'a', None]`, the interned index printed
  through the slot's stale tag, and it is now ADR 0187 and two parity programs
  (`mixed_element_reads.gy`, `mixed_element_writes.gy`). Its third catch went the other way: `xs == ys`
  for two equal lists answered False on both backends (the comparison compared heap handles), while
  `[0] == ["zero"]` answered **True** — an interned index matching a number. Containers now compare
  by value, element by element, as `(payload, tag)` pairs (ADR 0189). The same sweep's second find
  is closed too: `xs.sort()`, `xs.reverse()` and `sorted(xs)` are now language surface on both
  backends, with one comparator that orders interned strings by their **text** rather than by the
  index they were interned at (ADR 0191). And `[f(x) for x in range(5)]` — a comprehension whose
  element is a call — now compiles: the AOT path had made the constant folder the *meaning* of a
  comprehension, and the ordinary list-building idiom refused with "comprehension element must be
  constant" while the interpreter ran it happily (ADR 0192). Its second catch was a crash in the most
  ordinary program in the corpus — `print([1, 2])` handed the static elements global to `printf`
  as an `i32` and `llc` refused the module, while `print(set())` printed the handle `0` and
  `print([["a"], ["b"]])` printed `[1, 2]` (ADR 0188). Parity cases include
  `programs/truthiness.gy` (Python's truthiness rules),
  `programs/subscript_assign.gy` (container iteration and `d[k] = v` /
  `xs[i] = v` item assignment), `programs/container_methods.gy`
  (`xs.pop()`, `set()`/`list()`/`dict()`, `s.add`/`s.discard`) and
  `programs/none_values.gy` (`None` as a singleton, void functions returning `None`,
  `f() == None`) — see `docs/language.md`
  § Truthiness / § None / § Iterating and mutating containers / § Container methods.
  The probes are the roadmap's measured TODO list: nested and heterogeneous containers, tuples,
  negative indexing (including the one that panics the compiler), code-point strings, stdlib
  constant types, floored `//`/`%`, `sorted`/`enumerate`, calling a function through a
  parameter, `print(set())`, and print atomicity. `tools/oracleprobe` prints the three legs for
  any program, which is how a ledger row is written from data.
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
