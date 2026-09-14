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
```

More features (classes, try/except, generators/yield, lists, `len`, `and`/`or`,
`elif`, **closures**) are implemented in the interpreter (`EvalExpr`) but are
not yet lowered by the AOT LLVM backend.

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
