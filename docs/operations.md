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
used for codegen; the AOT backend emits textual IR verified by the external `llc`/`cc` toolchain.

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
| `--build <out>` | compile the positional source files into a native binary at `<out>` |
| `--version` | print version |


## Building a binary from multiple files

`gustyc --build <out> <file1> <file2> ...` compiles a **set of source files**
into a single native executable. Pipeline:

1. read + parse each file, merge the statement lists into one program
2. semantic analysis over the merged program
3. LLVM IR codegen + optimization (dead-global elimination at `--opt-level=1`; always-on escape analysis skips dead heap list-literal allocations, ADR 0134)
4. `llc-20` verifies/lowers the module to an object file
5. `cc` links it into the binary at `<out>`

The produced binary is a real native executable: `./prog` runs the program
(its `print` output goes to stdout).

Structured machine-readable outcome (with `--json`):

```json
{"output": "prog", "ir": "...", "objects": ["..."], "commands": ["llc ...", "cc ..."], "diagnostics": []}
```

Compile/link errors return diagnostics and exit code 1; missing sources or no
positional files are a usage error (exit 2). A semantic error in any source
file aborts the build before any toolchain step runs.


## Debug symbols / source maps (AOT)

`gustyc` can emit machine-readable source maps and DWARF debug info for AOT
builds:

- `--emit-source-map <src>` prints a JSON source map: each user function
  (top-level, nested, and class methods) mapped to its emitted LLVM symbol
  and 1-based IR line, plus the source line/col. Class methods are mangled to
  `<class>_<method>`.
- `--build out.bin --source-map-out a.smap.json src.gy` writes the same JSON
  source map alongside the binary.
- `--build out.bin --debug src.gy` passes `-g` to the final `cc` link so the
  binary carries DWARF debug info (line tables).

Example source map entry:
```json
{ "name": "C_m", "symbol": "C_m", "irLine": 9, "line": 5, "col": 5 }
```

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
- REPL (interactive): accumulates input line by line so multi-line suites (functions, classes, `if`/`for`/`while` bodies) can be entered; shows `> ` primary and `... ` continuation prompts on a terminal; recovers from parser/eval panics so a bug never kills the session.

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

- String equality `==`/`!=` compares contents in both paths
  (`"abc" == "abc"` -> 1), constant-folded in the codegen.


- `.split()` folds to the substring count (`len("a b c".split())` -> 3).


- String indexing `"abc"[1]` returns the char code (98) in both paths;
  the codegen constant-folds `*StrLit` index via `stringVal`.


- String methods `.upper()`, `.lower()`, `.strip()`, `.replace(old, new)`
  on constant string literals ship in **both** paths: the codegen
  constant-folds them via `stringConst`/`stringVal` Call-folding, so
  `len("AbC".upper())` -> 3 and `print("aXbXc".replace("X", "-"))`
  emits the folded global "a-b-c".
- `.find(sub)` constant-folds to an `i32` literal (`print("abcabc".find("bc"))`
  emits `i32 1`).
- `.startswith(sub)` / `.endswith(sub)` constant-fold to `i32 1`/`i32 0`
  (`print("hello".startswith("he"))` emits `i32 1`).
- `.count(sub)` constant-folds to an `i32` literal
  (`print("ababab".count("ab"))` emits `i32 3`).
- `.lstrip()` / `.rstrip()` constant-fold to a trimmed string constant
  (`print(len("  hi  ".lstrip()))` emits `i32 4`).
- `.join(list)` constant-folds a constant list of string elements to a
  string global (`print("-".join(["a", "b", "c"]))` emits "a-b-c").


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

## Source formatter (`gusty fmt`, Round 8)

- `--fmt <src>` — print the canonical formatted source.
- `--fmt-check <src>` — verify the source is already canonical; exit 0 if so,
  1 if not.
- `--fmt-file <path>` — format a source file.
The formatter round-trips docstrings (they are re-emitted as the first body
statement), so `--fmt` preserves `def`/`class` docstrings.

## Standalone type-check (`gusty check`, Round 13 + generics/protocols Round 14)

`gustyc --check <src>` (or `gusty check <file1> <file2> ...`) runs the semantic
pass mypy-style without executing: annotations are validated and mismatches
are reported. Exit codes are deterministic: `0` = clean, `1` = type errors,
`2` = usage. `--json` emits a machine-readable `{files, diagnostics, ok, exit}`
result.

Annotations are recursive generic expressions: `list[int]`,
`dict[str, int]`, `tuple[int, str]`, `list[list[int]]`,
`Callable[[int, str], bool]`, and structural protocol bounds `Sequence[T]`.
Assignment to a protocol bound checks the structural `assignable(got, want)`
relation: `Sequence[int] = [1, 2]` and `Sequence[int] = (1, 2)` pass; `str`
is `Sequence[str]` (not `Sequence[int]`), and an `int`/`dict` is not a
sequence, so those are rejected. A function-name reference (bare `fn`) is
assignable to any Callable bound under gradual typing.

## Benchmarking

`gustyc --bench '<src>' --bench-runs N --bench-opt L` runs the program through
both the AST interpreter and the AOT JIT and prints wall-clock totals, means,
bests, an interpreter phase profile, and a speedup ratio. Use `--bench-file
<path>` to benchmark a file, and `--json` for the machine-readable
`BenchResult`. See `docs/benchmark.md`.

## Property / fuzz testing (both backends)

gusty validates the interpreter and the LLVM AOT backend against **generated**
whole-program inputs, not just a fixed corpus.

- `pkg/lang/proptest.go` — `PropGrammar` + `DefaultPropGrammar()`: a seeded
  `rand.Rand`-driven grammar that builds random `*Program` ASTs over the
  shared surface and renders them to canonical source via `Format`.
- `PropSource(seed, n, g)` — deterministic: the same seed always yields the
  same `n` source strings, so failures/drift are reproducible.
  `PropPrograms(seed, n, g)` is the AST-level twin used by unit properties.
- Scope discipline keeps generated programs well-formed: top-level
  expressions read only top-level vars (bound before use), suite bodies
  (if/for/function) read only their own locals (loop var / params) plus
  literals — no undefined references, no forward bindings.
- `pkg/lang/proptest_test.go` — unit properties: corpus reproducibility,
  parse-cleanliness, interpreter validity (no undefined names / runtime
  errors), interpreter determinism (run-twice byte-identical stdout).
- `integration/proptest_test.go` — cross-backend parity harness: each
  generated source runs through `lang.InterpreterRun` and the
  `Compile` → `llc` → `cc` → run pipeline; stdout is diffed. Interpreter
  failures fail the build; AOT drift is logged (seed+index) so the suite
  stays green while drift is tracked.
- `FuzzPropInterpreter` — a Go-native fuzz target seeded from the
  deterministic corpus; asserts the interpreter never panics on arbitrary
  input.

Run them with the normal pipeline:

```sh
go test ./pkg/lang/ ./integration/
# long fuzz run (optional):
go test ./pkg/lang/ -fuzz=FuzzPropInterpreter -fuzztime 30s
```
