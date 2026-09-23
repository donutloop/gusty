# ADR 0147 — FFI / C interop via `extern fn`

## Status

Accepted (Round 15).

## Context

The language runs on an LLVM IR backend linked by `cc`, so it can already call
any C symbol if a prototype is emitted. We want "systems-language parity":
call C stdlib functions from gusty without writing C.

## Decision

Add an `extern fn` top-level declaration:

```text
extern fn abs(x: int) -> int
extern fn strlen(s: str) -> int
```

- The parser produces an `ExternDecl` statement node.
- The semantic pass registers externs and checks call arity + argument types.
- The AOT codegen emits a `declare i32 @abs(i32)` prototype in the IR preamble
  and lowers calls: `int` args marshal to `i32`, string-literal args marshal to
  `i8*` via `getelementptr` on the interned global string, and the native
  `i32` return is used directly as the value register.
- The AST interpreter dispatches extern calls to a small Go registry mirroring
  the C stdlib (`abs`, `getpid`, `rand`, `strlen`); unknown externs raise a
  clear "not available in the interpreter" error.
- The existing `cc` link pipeline resolves the symbol; stdlib functions need no
  extra `-l` flags.

## Consequences

- FFI is available in both backends; the integration test compiles and runs
  `abs(-5)` and `strlen("hello")` end-to-end.
- String FFI args are restricted to literals (variables need a heap-to-C
  pointer conversion — a follow-up).
- String-returning externs are parsed but their native `i8*` return is not yet
  wrapped back into the heap; only `int` returns are supported this round.
