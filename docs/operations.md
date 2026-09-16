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

Functions, control flow (`if`/`while`/`for`/`match`), the `pass` no-op
statement, integer arithmetic, comparisons, and `print` (via `printf`) are all
lowered to opaque-pointer IR.

## Lambda (both interpreter and AOT codegen)

`lambda params: expr` is an anonymous single-expression function, lowered to
a closure exactly like `def`:
- inline call: `(lambda x: int: x + 1)(5)` -> 6
- bound form: `f = lambda x: int: x * 2` then `f(3)` -> 6
- the AOT codegen emits an anonymous FuncDef (`lambda_N`) at module level and
  a call to it; the interpreter allocs a closure capturing the environment.
- interpreter-only: `Attr` string/list/dict methods, classes, generators.

## Dict methods (both paths, constant receivers)

`{1: 2, 3: 4}.keys()` and `.values()` on constant dict literals fold to
lists in the AOT codegen (`len`/`sum` work); `.items()` now ships in both paths on constant dict literals
(`len({1: 2, 3: 4}.items())` -> 2); non-constant receivers stay
interpreter-only.

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

## CLI / REPL

`gustyc` (in `cmd/gustyc`) provides:

- `--eval <src>` / `--file <path>`: evaluate and print the result
- `--verify <src>`: parse + analyze, exit 1 on diagnostics
- `--emit-llvm <src>` / `--emit-ast <src>`: machine-readable IR / AST JSON
- `--lang`: self-describing feature list for agents
- REPL (stateful) via `gustyc` (TTY) or `gustyc --repl`

Exit codes: 0 ok, 1 runtime/eval error, 2 parse/usage error.

## JSON output for agents

`gustyc --json` emits machine-readable JSON on stdout:

- `--json --eval "x = 1 + 2\nx"` → `{"result": "3", "exit": 0}`
- `--json --verify <src>` → `{"ok": true, "exit": 0}` or `{"diagnostics": [...], "exit": 1}`
- parse/runtime errors → `{"error": "...", "exit": 2}` (exit 1 for runtime)

Diagnostics serialize their `Msg`/`Span` fields for schema-driven tooling.

## Optimization: constant folding

The codegen folds integer-literal binary expressions at compile time:

    x = 1 + 2        # emits store i32 3 (no add instruction)

Folded ops: `+ - * / // %`, boolean `and`/`or`, and comparisons `== < <= > >=`.
Division/modulo by a literal zero is left to runtime. Verify with
`gustyc --emit-llvm`.

## Boolean `and` / `or` and floor division (AOT codegen)

`and` / `or` lower to i1 boolean logic (`icmp ne` each operand, combine with
`and`/`or i1`, `zext` to `i32` 0/1), mirroring the interpreter's evaluate-both-
then-combine semantics. `//` floor division lowers to `sdiv`, also mirroring
the interpreter. Literal operands are constant-folded.

## Builtins: sum / min / max / abs (AOT codegen)

`sum([1,2,3])`, `min([3,1,2])`, `max([3,1,2])`, and `abs(-5)` are lowered in
the LLVM AOT codegen path (previously interpreter-only). `sum`/`min`/`max`
fold over an **inline list literal** (unrolled `add` / `icmp`+`select` chains
over the list's global struct); `abs` accepts any integer expression and is
constant-folded for literal arguments. Interpreter behavior is unchanged.

## for-over-list (AOT codegen)

`for x in [1, 2, 3]:` iterates an inline list literal's constant elements in
the LLVM AOT path (previously range-only). It is lowered by **unrolling one
body block per element** (`break`/`continue` and the `else:` clause behave
exactly like `range` loops). Interpreter already iterated boxed lists/sets;
both paths now agree on for-over-list semantics. Machine consumption:
`gustyc --emit-llvm 's = 0
for x in [1,2,3]: s = s + x
print(s)'` emits the unrolled IR.

## Dict & set literals (AOT codegen)

Inline dict/set literals with constant-key indexing and `len` are lowered in
the LLVM AOT path: `{1: 10, 2: 20}[1]`, `{1, 2, 3}[2]`, `len({1: 10, 2: 20})`.
Each literal becomes a dedicated global struct (dicts: `{i32 count, [n x i32]
keys, [n x i32] vals}`; sets: `{i32 count, [n x i32] elems}`). Constant-key
lookup folds at compile time; literals must be used inline (no assignment-to-
variable indirection), matching the list-literal limitation. Interpreter
indexes dicts/sets at runtime and is unchanged.

## String-constant concatenation + len (AOT codegen)

- `.split()` folds to the substring count (`len("a b c".split())` -> 3).


- String indexing `"abc"[1]` returns the char code (98) in both paths;
  the codegen constant-folds `*StrLit` index via `stringVal`.


- String methods `.upper()`, `.lower()`, `.strip()` on constant string
  literals ship in **both** paths: the codegen constant-folds them via
  `stringConst`/`stringVal` Call-folding, so `len("AbC".upper())` -> 3.


String literals are lowered to global constants. `+` on two string literals
folds to a single concatenated constant (e.g. `"a" + "b"` -> `@.strN` with
`"ab"`), and `len` of a string-constant expression (including a chain of
`+`-concats) folds to its character count: `print(len("hello"))` -> `5`,
`print(len("ab" + "cd"))` -> `4`. The semantic analyzer types `str + str` as
`str` (no arithmetic warning).

## Interpreter memory model

Boxed heap handles are allocated from a high base (`1 << 20`) so they never
collide with raw small integer literals stored in lists/dicts/vars. This keeps
`Repr` from misinterpreting a raw int as an object handle.

## String methods

`upper()`, `lower()`, `strip()`, `split(sep?)` dispatch on boxed strings in
`evalCall` before attr resolution. Machine consumption via `--json --eval
'"heLLo".upper()'` returns `{"result":"HELLO",...}`.

## List methods

`xs.append(x)` mutates a boxed list in place and returns it (REPL-friendly).
- On constant list literals the AOT codegen folds `[1, 2, 3].append(4)` to
  `[1, 2, 3, 4]` (ADR 0044), so `len`/`sum`/`min`/`max` work; mutating a bound variable
  stays interpreter-only.

Machine consumption via `--json --eval "xs = [1,2]
xs.append(3)
xs"`.
