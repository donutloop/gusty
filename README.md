# gusty

> A statically-typed, Python-flavored programming language compiled ahead-of-time through LLVM.

gusty is a small, modern programming language that pairs Python's familiar
indentation-based syntax and ergonomic feel with the performance of native
compiled executables. Source goes through a clean, inspectable pipeline —
`lex → parse → semantic (type inference) → codegen` — down to LLVM IR, which
is verified, optimized, and lowered to a native binary, or JIT-executed in the
REPL.

The toolchain is built for **both humans and agents**: a friendly REPL/CLI for
people, plus a structured, machine-readable interface (JSON diagnostics,
JSON AST/IR dumps, a JSON Schema, stable flags, deterministic exit codes) so
scripts and AI workflows can discover and consume the language without
guessing.

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
  exactly like `def`
- **Control flow** — `if` / `elif` / `else`, `while`, `for ... in range(n)`
  / `range(a, b)` / `range(a, b, step)`, `for x in [...]`, optional loop
  `else:` clauses, `break` / `continue`, and `pass`
- **Pattern matching** — `match` with integer equality, `_` wildcards, and
  list-destructuring patterns (`case [a, b]:`)
- **Data structures** — inline `list` / `dict` / `set` literals, indexing,
  and list / dict / set comprehensions
  (`[x * 2 for x in ...]`, `{k: v for ...}`, `{x for ...}`)
- **Generators** — `def g(): yield a; yield b` collects yielded values
- **Exceptions** — `try` / `except` / `finally` with typed built-in exception
  classes (`Exception`, `ValueError`, `TypeError`, `KeyError`, `IndexError`,
  `RuntimeError`, `StopIteration`, `ZeroDivisionError`) and `raise`
- **Classes & inheritance** — `class Name:` with methods (`self`), instance
  attributes, `__init__`, `class Child(Base):` multi-level inheritance, and
  `super()` delegation
- **Decorators** — `@dec def f:` → `f = dec(f)` at definition time
- **Modules** — `import mod` loads `mod.gy` and binds `mod` as a namespace
- **Standard library** — `len`, `print`, `range`, `sum`, `min`, `max`,
  `abs`, `sorted` (+ `reverse`), `reversed`, `enumerate`, `zip`, `any`,
  `all`, `chr`, `ord`, `round`, and `int` / `float` / `str` conversions,
  plus string methods (`upper` / `lower` / `strip` / `replace` / `find` /
  `rfind` / `index` / `count` / `split` / `rsplit` / `join` / `partition` /
  `capitalize` / `title` / `swapcase` / `ljust` / `rjust` / `zfill` /
  `expandtabs` / `removeprefix` / `removesuffix` / `isdigit` / `isalpha` /
  `isalnum` / `isspace` / `islower` / `isupper`), dict methods (`keys` /
  `values` / `items` / `get`), and `list.append` / `list.count`
- **Gradual typing** — optional type annotations on variables, parameters,
  and returns, statically checked by `--verify` (with `any` as the dynamic
  escape hatch); untyped code falls back to dynamic dispatch

### Two execution backends, always in sync

Every feature ships in **both** paths:

- **Interpreter** (`pkg/lang/jit.go`) — the REPL / `--eval` / `--verify` path:
  fast feedback and rich diagnostics, the first place features are implemented
  and tested.
- **LLVM AOT codegen** (`pkg/lang/codegen.go` + `pkg/lang/closure.go`) — the
  `--file` / `--emit-llvm` / link-and-run path: emits deterministic,
  opaque-pointer LLVM IR verified by `llc` and lowered to a native executable.

The AOT backend lowers functions, `lambda` closures, control flow,
arithmetic, comparisons, boolean `and` / `or`, `print`, list/dict/set
literals, comprehensions, `len`, numeric builtins, and a wide range of
constant-folding string/dict operations (e.g. `len("AbC".upper())` → `3`,
`len("a b c".split())` → `3`, `{1: 2, 3: 4}.keys()` → `[1, 3]`).

---

## Requirements

- **LLVM 20** — `llc-20` (e.g. `apt.llvm.org/jammy/ llvm-toolchain-jammy-20`).
  Opaque pointers are the default in LLVM 20, so no `-opaque-pointers` flag
  is needed.
- **Go** — `go build -tags=llvm20 ./...`
- **A C linker** — `cc` / `gcc`

## Build & test

```sh
# build
go build -tags=llvm20 ./...

# unit tests (lexer / parser / semantic / codegen + LLVM 20 verification)
go test -tags=llvm20 ./pkg/...

# integration tests: compile and execute source end-to-end
go test -tags=llvm20 ./integration/...
```

The integration suite drives the real pipeline — `source → codegen (IR) →
llc-20` (module verification + object) `→ cc link → run` — and asserts the
native binary's stdout matches the expected output.

`make` targets:

```sh
make test          # go test -tags=llvm20 ./...
make build         # go build -tags=llvm20 ./...
make testllvmcode  # lower ./integration/expected/*.ll with llc-20 + cc and run each
```

## CLI & REPL

`gustyc` is the command-line interface and REPL:

```
gustyc --eval "x = 2 + 3\nx"            # evaluate source, print result
gustyc --file prog.gy                   # compile & run a source file
gustyc --build out a.gy b.gy            # compile a set of files into a native binary
gustyc --verify "def f(x): return x * 2"  # static analysis only
gustyc --emit-llvm "x = 1 + 2"          # print emitted LLVM IR
gustyc --emit-ast "x = 1"               # print the AST as JSON
gustyc --lang                           # list supported language features
gustyc --schema                         # print the JSON Schema for AST/IR dumps
gustyc                                  # start the REPL (stateful)
```

Flags: `--eval`, `--file`, `--verify`, `--emit-llvm`, `--emit-ast`,
`--target`, `--opt-level`, `--lang`, `--json`, `--schema`, `--version`,
`--repl`, `--help`.

Exit codes: `0` = ok, `1` = runtime/eval error, `2` = parse/usage error.

### Machine-readable interface

For agents and scripts, `gustyc` emits structured output:

- `--json --eval "x = 1 + 2\nx"` → `{"result": "3", "exit": 0}`
- `--json --verify <src>` → `{"ok": true, "exit": 0}` or
  `{"diagnostics": [...], "exit": 1}`
- `--schema` prints a draft-07 JSON Schema describing the `--emit-ast` AST
  dump (`{"stmts": [...]}`) and the `--emit-llvm` IR text dump
  (`definitions.irDump`, `text/plain`)
- Diagnostics carry source spans and messages, e.g.
  `[{"span": {"line": 1, "col": 5}, "msg": "undefined name \"b\"", "severity": "error"}]`

---

## Project layout

```
pkg/lang/              compiler: lexer, parser, semantic, codegen, interpreter
cmd/gustyc/            the CLI and REPL
integration/           end-to-end tests that compile AND execute generated code
integration/expected/  .ll artifacts lowered by `make testllvmcode`
docs/                  language spec, operations, architecture decision records
.github/workflows/     CI: installs LLVM 20, runs unit + integration tests
```

## Documentation

- `docs/language.md` — the single source of truth for gusty syntax and semantics
- `docs/operations.md` — the single source of truth for the CLI, flags, JSON
  schemas, and exit codes
- `docs/adr/` — architecture decision records documenting every design choice

## License

[MIT](LICENSE)
