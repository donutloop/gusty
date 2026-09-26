# gusty extern-fn C ABI — v1

This document defines the *stable, versioned* C ABI for `extern fn` exports.
The ABI gives consumers a fixed struct layout for tagged/union values that
survives across releases, a version marker to check before linking, and a
machine-readable schema to validate against at build time.

## Version

The ABI version is `1`. Every generated module carries:

```llvm
@gusty_abi_version = internal constant i32 1
```

Consumers **must** check `@gusty_abi_version` against the ABI they were built
against before linking extern exports. Bump `ABI_VERSION` in `pkg/lang/abi.go`
only for incompatible layout changes; tag words never change.

## Stable struct layouts

The tagged-value ABI struct mirrors the interpreter `%obj` value:

```llvm
%gusty_value = type {i32, i32}
```

| offset | field   | meaning            |
|--------|---------|--------------------|
| 0      | `tag`   | tag word           |
| 1      | `payload` | payload word    |

```c
struct gusty_value { int32_t tag; int32_t payload; };
```

The widened union used by exports that pass floats or string pointers mirrors
the interpreter `%unionbox`:

```llvm
%gusty_union = type {i32, i32, double, i8*}
```

| offset | field | meaning                    |
|--------|-------|----------------------------|
| 0      | `tag` | tag word                   |
| 1      | `i`   | 32-bit payload word        |
| 2      | `f`   | f64 payload word           |
| 3      | `s`   | i8* string-literal pointer |

## Tag words

Tag words are **fixed** and never renumbered (see `pkg/lang/abi.go`):

`int=0 float=1 bool=2 none=3 str=4 list=5 dict=6 set=7 tuple=8 class=9
instance=10 method=11 closure=12 exn=13 module=14`

## Extern marshalling

- integer exports marshal as a plain `i32` word;
- string exports marshal as an `i8*` pointer to a literal;
- tagged exports marshal through `%gusty_value` (tag, payload).

## Machine-readable schema

`gustyc --abi` prints the full ABI contract as JSON (version, layouts, tag
words, marshalling rules, IR markers). Validate against it at build time; the
schema is emitted by `ABISchema()` in `pkg/lang/abi.go`.

## Stability contract

1. Tag words are fixed across releases.
2. `gusty_value` layout is `{i32 tag, i32 payload}`.
3. `gusty_union` layout is `{i32 tag, i32 word, double f, i8* s}`.
4. Extern ints marshal as plain `i32`.
5. Extern strings marshal as `i8*` literal pointers.
6. Tagged exports marshal through `%gusty_value`.

## Shared-library export (L10.3)

`gustyc --build libgusty.so --shared <file1> ...` emits a **position-independent
shared library** (`.so` on Linux, `.dylib` on macOS) carrying the stable
extern-fn ABI above. The same lex→parse→semantic→codegen→`opt`→`llc` pipeline as
the native executable build is used; the only difference is the final link:

```
cc -shared -fPIC prog.o -o libgusty.so -lm
```

The object is already PIC (`llc -relocation-model=pic`); `-fPIC` at link is
belt-and-suspenders so the `.so`/`.dylib` loads at any address. Because the
versioned ABI marker (`@gusty_abi_version`) is emitted by codegen, extern
exports stay ABI-stable across `dlopen`/loads and across releases.

Verify: `nm -D libgusty.so` shows the exported `main`/extern entry points, and
`dlopen` succeeds from any host process.
