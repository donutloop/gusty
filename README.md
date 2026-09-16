# gusty

A tiny, statically-typed programming language with a textual LLVM IR backend.
gusty compiles source through `lex -> parse -> semantic -> codegen` and emits
LLVM IR, then lowers it to a native executable with the LLVM 20 toolchain.

## Language surface (AOT-compiled)

```
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

pass                          # no-op statement
```

More features (classes, try/except, generators/yield, lists, `len`, `and`/`or`,
`elif`, **closures**, **decorators**) are implemented in the interpreter
(`EvalExpr`); some are also lowered by the AOT LLVM backend. The AOT backend
already lowers functions, control flow (`if`/`while`/`for`/`match`), `pass`,
integer arithmetic (`+ - * / // %`), comparisons, the boolean operators
`and`/`or`, `print`, inline list literals, `len`, string-constant
concatenation + `len` (`"a" + "b"`, `len("ab" + "cd")`), inline dict/set
literals with constant-key indexing (`{1: 10, 2: 20}[1]`, `{1, 2, 3}[2]`), the
numeric builtins `sum`/`min`/`max`/`abs`, and `for` loops over inline list
literals
(`for x in [1, 2, 3]:`, unrolled per element). `lambda` anonymous functions
(inline `(lambda x: int: x + 1)(5)` and bound `f = lambda x: int: x * 2; f(3)`)
ship in **both** the interpreter and the AOT codegen, lowered to a closure
exactly like `def`. String methods `.upper()`, `.lower()`, `.strip()` on constant string
literals constant-fold in the codegen (`len("AbC".upper())` -> 3). `.replace(old, new)`
replaces every occurrence in both paths; `.find(sub)` folds to the index or -1;
`.startswith(sub)`/`.endswith(sub)` fold to 1 or 0; `.count(sub)` folds to the
occurrence count; `.lstrip()`/`.rstrip()` fold to trimmed strings. String indexing `"abc"[1]`
returns the char code (98) in both paths. `.split()` folds `len("a b c".split())` to 3. String equality `==`/`!=` compares contents in both paths. Dict `.keys()`/`.values()` on constant dict literals
fold to lists, and list `.append()` on constant literals folds
(`[1, 2, 3].append(4)` -> `[1, 2, 3, 4]`).
fold to lists (`{1: 2, 3: 4}.keys()` -> `[1, 3]`), and `.items()`
folds `len({1: 2, 3: 4}.items())` to 2. `min`/`max` also fold
dict-method args (`max({1: 2, 3: 4}.keys())` -> 3).

## Toolchain

gusty targets **LLVM 20**. Opaque pointers are the default in LLVM 20, so the
IR needs no `-opaque-pointers` flag; lowering uses `llc-20` + `cc`.

Requirements:

- LLVM 20 (`llc-20`, e.g. `apt.llvm.org/jammy/ llvm-toolchain-jammy-20`)
- Go (`go build -tags=llvm20 ./...`)
- a C linker (`cc` / `gcc`)

## Build & test

```sh
# build
go build -tags=llvm20 ./...

# unit tests (lexer/parser/semantic/codegen + LLVM 20 verification)
go test -tags=llvm20 ./pkg/...

# integration tests: execute the generated code end-to-end
go test -tags=llvm20 ./integration/...
```

The integration suite drives the real pipeline — `source -> codegen (IR)
-> llc-20` (module verification + object) `-> cc link -> run` — and asserts the
native binary's stdout matches the expected output.

`make` targets:

```sh
make test          # go test -tags=llvm20 ./...
make build         # go build -tags=llvm20 ./...
make testllvmcode  # lower ./integration/expected/*.ll with llc-20 + cc and run each
```

## Layout

```
pkg/lang/              compiler: lexer, parser, semantic, codegen, interpreter
integration/           end-to-end tests that compile AND execute generated code
integration/expected/  .ll artifacts lowered by `make testllvmcode`
.github/workflows/     CI: installs LLVM 20, runs unit + integration tests
```

## CLI & REPL

`gustyc` is the command-line interface and REPL:

    gustyc --eval "x = 2 + 3\nx"
    gustyc --verify "def f(x): return x * 2"
    gustyc --emit-llvm "x = 1 + 2"
    gustyc --emit-ast "x = 1"
    gustyc --lang        # list supported language features
    gustyc              # start the REPL (stateful)

Flags: `--eval`, `--file`, `--verify`, `--emit-llvm`, `--emit-ast`,
`--target`, `--opt-level`, `--lang`, `--version`, `--repl`, `--help`.
Exit codes: 0 = ok, 1 = runtime/eval error, 2 = parse/usage error.
