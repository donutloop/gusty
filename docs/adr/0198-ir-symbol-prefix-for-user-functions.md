# 0198. A program's function names are its own: emit them under a link prefix

## Status

Accepted. Implemented this cycle in `pkg/lang/codegen.go`, `pkg/lang/closure.go` and
`pkg/lang/sourcemap.go`; pinned by `integration/programs/host_symbol_names.gy`,
`pkg/lang/host_symbols_test.go` and `integration/host_symbols_test.go`. Roadmap Gap R.4.

## Context

An emitted LLVM function name is not a label. It is a symbol, with linkage, and the linker
resolves it against everything else in the link — the C library included. A program wrote:

```gusty
def sync():
    return 7

print(sync())
```

and the module contained `define i32 @sync()`. The link succeeded, the binary ran, and it
printed **`0`** — libc's `sync()` — where the interpreter printed `7`. Nothing failed, no
diagnostic existed to fail, and the program was ordinary: `sync` is a name a person chooses
deliberately.

Measured as a battery (`def NAME(x): return x + 7`, called through a wrapper, expected `8`):

| program writes | interpreted | compiled (before) |
|---|---|---|
| `sync`, `printf`, `exit`, `strlen`, `free`, `malloc`, `write`, `read`, `open`, `time`, `rand`, `system`, `abort` | `8` | `0`, `1`, garbage, or early death |
| `main` | `8` | build failure: duplicate `main` with the generated entry point |

Two things made this worse than a link error. First, silence: a wrong answer beats a
diagnostic in how long it survives. Second, `main` — the generated entry point is `@main`, so
a program that named a function `main` could not be built at all, which is a refusal to
compile an ordinary program rather than a wrong answer.

The obvious mitigations were both unsatisfying: a blocklist of forbidden names would refuse
names the language has always allowed, and a blocklist of *emitted* names is only as current
as the last libc release.

## Decision

**Every IR symbol that a program's own source defines is emitted under `gy_`, and everything
the program did not define keeps its name.**

1. **One helper, one rule.** `irSymbol(name)` (in `pkg/lang/codegen.go`) returns `gy_<name>`,
   idempotently. It is applied where a symbol is *minted* — the `define` of a module function,
   a method's symbol (`Class_method`), a generated lambda, a decorated body, an imported
   module function's `mod$fn` mangle — and everywhere those symbols are *referenced* (the
   matching `call`, the function-pointer global, the decorator's resolved label, the source
   map's `symbol` field). Minting and referencing through one function is what makes the pair
   impossible to desynchronise; hand-written concatenations at call sites are exactly what
   broke first during implementation (`@f_impl` defined, `@gy_f_impl` called).

2. **What is not prefixed, and why.**
   - the runtime's own helpers (`rt_*`, `rt_frame_open`, `rt_alloc`, the container printers)
     — names the *compiler* owns, not the program;
   - the C library (`printf`, `snprintf`, `write`) and any `declare` the runtime needs;
   - the generated entry point `@main` — the program's `def main` becomes `@gy_main` and the
     two coexist, which is what turned that build failure back into a working program;
   - `extern fn` declarations and their calls: an FFI surface's link name **is** the C name the
     program asked to bind. `extern fn abs(x: int) -> int` must keep `declare i32 @abs(i32)`
     and `call i32 @abs(`; prefixing it would defeat the one thing the declaration is for. This
     was caught by `pkg/lang/ffi_test.go` when a first attempt wrapped the extern call site too.

3. **The source map says both things.** `SourceMapEntry.Name` stays the name the program wrote
   (`sync`) and `.Symbol` becomes what the linker sees (`gy_sync`), built through the same
   `irSymbol`. A debug map whose `name` carries a compiler decoration answers neither question.

4. **Internal, derived names ride along for consistency** (`gy_f_impl`, `gy_lambda_0`): they
   cannot collide at link time on their own, but two spellings of the same symbol in one module
   is how this bug came back the first time.

## Consequences

- The whole collision class is closed with no list to maintain: no name in libc, in a linked
  archive, or in our own runtime can be reached by a program's `def`, and no future libc export
  reopens it.
- `programs/host_symbol_names.gy` is in the conformance ledger with `oracle: "match"`:
  `sync`, `write`, `read`, `open`, `time`, `exit`, `main`, a class with methods over them, and a
  wrapper that calls all seven print `1 2 3 4 5 6 7 28 6 7` identically on the interpreter, in
  the compiled binary, and in CPython.
- `TestBuiltBinaryCarriesThePrefixedSymbols` asserts on the **artifact**: `nm -defined-only` must
  show `gy_sync` and must not show `sync`. Stdout alone was never the property — the broken
  binary printed fine-printable numbers too.
- Existing IR-text expectations across ~20 test files name the emitted symbols, so they name
  `gy_*` now. `irFunctions`, the shared helper that splits a module into bodies, keys each body
  by *both* the source name and the link name, so a test asking for the body of `bump` means the
  function it wrote as `bump`.
- Documented in `docs/language.md` (a program's function names are always the program's own) and
  in `docs/operations.md`, where the IR-symbol and source-map sections now tell an agent what to
  grep for.

## Measured on the way, and not this gap

- **R.6 — a builtin is consulted before the program's own `def`.** `def abs(x): return x + 7`
  then `print(abs(1))` prints `8` interpreted and `1` compiled: the name is resolved against the
  builtin table and the user function is never emitted. Prefixing cannot help, because the
  program's function is not reached at all; the fix is the order of the two tables, matching the
  interpreter and CPython, where a `def` shadows a builtin.
- **R.8 — a module function and a method of the same name share the checker's key.** `def time(x)`
  plus `class Timer: def time(self, x)` makes `time(x)` inside a body report
  `undefined name "x"`: it is being checked against the method's parameters, whose `self` is not
  in scope. The evaluator and CPython both run the program; pinned as
  `programs/probe_method_function_name_clash.gy`.

## Alternatives considered

- **Refuse host-ABI names in the checker.** Rejected: it turns a legal program into an error and
  hands the user a renaming chore for a bug in the compiler; and the set is unbounded (`llvm.*`,
  LTO intrinsics, sanitizer symbols, whatever `cc` links by default).
- **Maintain a blocklist of names to prefix.** Rejected as unfinishable and, worse, silently
  incomplete: an unlisted name reproduces the exact silent-wrong-answer behaviour this ADR exists
  to remove.
- **Emit every user function `internal`** so the linker cannot resolve it elsewhere. Rejected:
  it breaks the callable surface the AOT build is for (imported module functions are called
  across emitted units, `--emit-object` consumers and the source map's symbol field all name the
  external symbol), and it hides the problem rather than removing it.
- **Hash or mangle every name** (`gy_sync_7f3a`). Rejected: `--emit-llvm` output is read by humans
  and grepped by agents; a readable, predictable prefix keeps `grep '@gy_sync'` meaningful, which
  is precisely how the `nm` test asserts the property.
