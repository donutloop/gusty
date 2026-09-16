# ADR 0031: String-literal `print` arguments in the LLVM AOT codegen

## Decision

The interpreter's `print` writes every argument via `fmt.Println(e.Repr(v))`,
which handles strings as well as integers — `print("hi")` writes `hi`. The
LLVM AOT codegen lowered every `print` argument to `printf("%d\n", <reg>)`,
so a string-literal argument became `printf("%d\n", <str-global>)` — passing a
string pointer to a `%d` format (wrong output). Detect string-literal `print`
arguments and lower them to `printf("%s\n", <str-global>)`, keeping `%d\n` for
integer arguments, so the AOT path matches the interpreter exactly.

## Codegen/IR implications

- Each `print` argument is classified: `*StrLit` → `%s\n` format + `i8*`
  argument; everything else → `%d\n` format + `i32` argument.
- Multi-argument `print` (ADR 0030) still emits one `printf` per argument, each
  on its own line; mixed `print(1, "hi", 2)` emits one `%d\n` and one `%s\n`.
- The AOT path stays allocation-free: string args are global `i8*` constants
  already produced by `StrLit` lowering; `%s\n` needs no new runtime support.

## Alternatives rejected

- Lowering all string args via `%d` (status quo) — rejected: passing a string
  pointer to `%d` is a silent wrong-output bug, not a documented limitation.
- Lowering strings via `%s` but truncating/escaping them — rejected: the
  interpreter prints the raw string via `Repr`; the AOT path should print the
  raw string constant exactly, one line per argument.
