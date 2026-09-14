# gusty operations

Single source of truth for gusty's CLI, flags, JSON schemas, and exit codes.
This toolchain is built for both humans and agents: every interface has a
human path (readable help, discoverable commands, readable diagnostics) and a
machine path (structured JSON, stable flags, deterministic exit codes).

## Pipeline

lex -> parse -> semantic (type inference) -> codegen (textual LLVM IR) ->
llc compile -> link/run, or JIT-execute in the REPL.

The codegen pass emits deterministic, opaque-pointer LLVM IR text that
compiles cleanly with `llc -opaque-pointers`. No native LLVM C-API call path is
used for codegen (the go-llvm `CreateCall` path was abandoned for a
deterministic textual emitter).

## CLI flags (stable)

| Flag | Meaning |
|------|---------|
| `--file <path>` | compile a source file |
| `--eval <src>` | compile-and-run source from argv |
| `--emit-llvm` | print the emitted LLVM IR |
| `--emit-ast` | print the JSON AST dump |
| `--verify` | run `llvm::verifyModule` equivalent (llc compile) |
| `--target <triple>` | target triple for codegen |
| `--opt-level <n>` | optimization level |
| `--version` | print version |

## Diagnostics

Diagnostics carry source spans and messages. The machine-readable path is a
JSON array of diagnostics:

```json
[{"span": {"line": 1, "col": 5}, "msg": "undefined name \"b\"", "severity": "error"}]
```

## Exit codes (deterministic)

| Code | Meaning |
|------|---------|
| 0 | success |
| 1 | compile error (diagnostics emitted) |
| 2 | LLVM/llc verification failure |
| 3 | runtime error |
| 4 | usage error |

## Emitted IR

Functions, control flow (`if`/`while`/`for`/`match`), integer arithmetic,
comparisons, and `print` (via `printf`) are all lowered to opaque-pointer IR.

## Interpreter-only language surface

Classes (with **inheritance** via `class Child(Base):` and `super()`, see
`docs/language.md`), **modules/imports** (`import mod` loads `mod.gy` and
binds `mod` as a namespace with `mod.name` / `mod.fn(args)` access),
`try`/`except`, generators (`yield`), lists, `len`, **closures**
(nested `def`s capturing the enclosing scope, e.g. `m = add(1); m(2)`),
**decorators** (`@dec def f` -> `f = dec(f)` at def time), and **gradual
runtime type checking** (annotations on variables, parameters, and returns
are enforced with a `type mismatch` error; `any` accepts anything) are
implemented in the interpreter used by `--eval` and the REPL; they are not
yet lowered by the AOT LLVM backend. They are fully represented in the JSON
AST dump (`--emit-ast`) with no schema change.
