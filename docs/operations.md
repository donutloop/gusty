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
| `--verify <src>` | run the front end (lex + parse + semantic analysis) and report diagnostics, without executing |
| `--verify-llvm <src>` | compile `<src>` and report LLVM's module-verifier verdict for the emitted module (L8.2) |
| `--verify-llvm-file <path>` | same, reading the program from a file |
| `--no-verify` | with `--build`, skip the module-verifier stage (it runs by default) |
| `--target <triple>` | target triple for codegen |
| `--opt-level <n>` | optimization level |
| `--build <out>` | compile the positional source files into a native binary at `<out>` |
| `--build <out> --shared` | emit a position-independent shared library (`.so`/`.dylib`) carrying the stable extern-fn ABI (L10.3) instead of a native binary |
| `--version` | print version |
| `--lang` | list the supported language surface (self-describing) |
| `--schema` | print the machine-readable JSON schema for the AST/IR/diagnostic dumps |
| `--abi` | print the versioned extern-fn C ABI schema as JSON |
| `--variance` | print the generic variance table as JSON (L6.6) |


## Building a binary from multiple files

`gustyc --build <out> <file1> <file2> ...` compiles a **set of source files**
into a single native executable. Pipeline:

1. read + parse each file, merge the statement lists into one program
2. semantic analysis over the merged program
3. LLVM IR codegen + optimization (dead-global elimination at `--opt-level=1`; always-on escape analysis skips dead heap list-literal allocations, ADR 0134)
4. **module verification** (L8.2): `opt-20 -passes=verify` (plus the requested
   `-O` pipeline) accepts the module, falling back to `llc-20 -filetype=null` when
   `opt` is unavailable. Override with `--no-verify`.
5. `llc-20` lowers the module to an object file
6. `cc` links it into the binary at `<out>`

**Failure output contract.** A failed build always prints *both* halves of the story on
stderr — every source diagnostic (warnings included) *and* the line saying what failed:

```
warning at 1:7: arithmetic on non-numeric operands (int, str)
gustyc: build: codegen: unsupported call "enumerate"
```

and it exits `1`. With `--json` the same information arrives together in the *partial*
`BuildResult` on stdout (`diagnostics`, `ir`, `verification`) even though the exit code is
non-zero — earlier versions returned no result at all for codegen and `llc` failures, so
the diagnostics were unreachable for scripts while being printed for humans (roadmap Gap
K.7). Never scrape stderr for the reason: read `verification`/`diagnostics` from the JSON.

**A source typo is a compile error, never a verifier failure.** `print(undefined_thing)`
reports `error at 1:7: undefined name "undefined_thing"` and exits `1`; it must not reach LLVM
as a dangling slot, which would exit `2` and blame the compiler (roadmap Gap K.10). Built-in
names come from one table (`pkg/lang/predeclared.go`) shared by the checker, the codegen
unbound-name guard and LSP completion, so a real built-in (`sum`, `enumerate`, `zip`, …) is
never "undefined" and a name that is not bound is never lowered.

The produced binary is a real native executable: `./prog` runs the program
(its `print` output goes to stdout).

Structured machine-readable outcome (with `--json`):

```json
{"output": "prog", "ir": "...", "objects": ["..."], "commands": ["llc ...", "cc ..."], "diagnostics": [],
 "verification": {"ok": true, "tool": "/usr/bin/opt-20", "skipped": false, "pipeline": ["verify"], "toolchain": "LLVM 20"}}
```

### Module verification (`irVerification`, L8.2)

The AOT backend emits *textual* IR, so the only trustworthy statement that a
module is well-formed is LLVM's own module verifier. It is a pipeline stage of its
own rather than a side effect of linking, so a codegen bug is attributed to the
compiler stage that produced it and reported in the verifier's own words.

```console
$ gustyc --verify-llvm --json 'print(1)'
{"ok":true,"tool":"/usr/bin/opt-20","skipped":false,"pipeline":["verify"],"toolchain":"LLVM 20"}
```

| Field | Meaning |
|-------|---------|
| `ok` | the verifier ran **and** accepted the module |
| `tool` | which binary decided (`opt-20`, or `llc-20 -filetype=null` as fallback) |
| `skipped` | no verification toolchain was found — an unverified module is never reported as verified |
| `pipeline` | passes that ran, e.g. `["verify"]` or `["verify", "-O2"]` (follows `--opt-level`) |
| `errors` | verifier diagnostics, normalised to `prog.ll:line:col: error: …` (no temp paths, capped at 8 lines) |
| `note` | machine-matchable guidance; a rejection says `LLVM rejected the module; this is a compiler bug, not a source error` |
| `toolchain` | pinned LLVM version, e.g. `LLVM 20` |

A rejected module exits `1` (a compiler bug, not a source error — fix the codegen
or report it); a missing toolchain exits `0` with `skipped: true`, because the
build legitimately proceeded without it. The same record is embedded in
`BuildResult` as `verification` (see above), and the human `--build` output prints
a `verified by <tool> (<pipeline>)` line. Schema: `gustyc --schema` →
`irVerification`.

### Optimization report

`--build` also says what the **optimizer** did, so an un-optimized build is never
mistaken for an optimized one (roadmap Gap J.4). `BuildResult.optimization` is absent when
nothing was requested (`--opt-level 0`); otherwise:

| field | meaning |
|---|---|
| `applied` | `true` only when the **real LLVM optimizer** ran on this module |
| `tool` | `opt-20`, or `gusty-textual` when only gusty's textual pass ran |
| `pipeline` | `-O1` / `-O2` / `-O3`, or `textual` / `none` |
| `level` | the level the build asked for |
| `fallback` | what ran instead of the real optimizer, e.g. `textual` |
| `note` | why the real optimizer did not run (missing toolchain, rejected the module, …) |
| `error` | the optimizer's own failure text, when it exists but failed |

Human output pairs the build line with `optimized by opt-20 (-O2)` or
`NOT LLVM-optimized: <note>`. A missing optimizer is not a build failure — exit stays `0` —
but it is now visible in both output paths instead of being silent. Schema:
`gustyc --schema` → `optimization`.

### Flags anywhere on the command line

Flags may appear before *or after* positional source paths:

```
gustyc --build out src.gy --opt-level=2 --json     # works
```

Go's `flag` package stops at the first positional, which used to turn the trailing
`--opt-level=2` into a source file named `--opt-level=2`. `reorderFlags` moves flag tokens
front while keeping each flag's value attached, so `--eval --help` still evaluates the text
`--help` rather than printing usage. Unrecognised `--flags` are still reported by the flag
package as a usage error (exit `4`). To pass a file whose name really begins with `--`, write
`./--weird.gy`.

Compile/link errors return diagnostics and exit code 1; missing sources or no
positional files are a usage error (exit 4). A semantic error in any source
file aborts the build before any toolchain step runs.

**Runtime failures (both backends).** An exception that escapes the program is reported on
**stderr** and exits non-zero — never exit 0 with silence, which is how a trapped program
previously looked successful to a script:

```
Traceback (most recent call last):
  File "prog", line 3, in boom
IndexError: index out of range
```

The frame line names the file, the line of the raise, and the enclosing function
(`<module>` at top level), and both backends agree on it: the interpreter prints one frame
per stack level (so a raise inside a called function shows the call site too), while the
compiled report shows the raise site's own frame — the call-stack frames need the line
tables of L8.5 (roadmap Gap K.8). The last line is identical on both. stdout stays clean, so
`prog 2>/dev/null | ...` sees only program output. With `--json` the eval path emits
`{"error": ..., "traceback": ..., "exit": 3}` on stdout. Runtime errors are typed
(`IndexError` / `KeyError` / `TypeError` / …), so `except IndexError:` catches them on
both backends — see `docs/language.md` § Exceptions.


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
[{"level": "error", "span": {"line": 1, "col": 5}, "msg": "undefined name \"b\"",
  "code": "", "suggestion": ""}]
```

| Field | Meaning |
|-------|---------|
| `level` | `info` \| `warning` \| `error` (only `error` changes the exit code) |
| `span` | `{line, col}` of the offending token |
| `msg` | human-readable message (wording may improve) |
| `code` | **stable** rule identifier — branch on this, not on `msg` |
| `suggestion` | actionable fix for the violated rule (may be empty) |

### Diagnostic codes (stable)

The variance + generics rules (L6.6) report these codes from `--check`,
`--verify`, `--json`, and the LSP:

| Code | Rule |
|------|------|
| `type.mismatch` | plain kind mismatch (`expected int, got str`) |
| `type.variance.invariant` | `list[T]` / `set[T]` / `dict[K, V]` type arguments must match exactly |
| `type.variance.covariant` | `Sequence[T]` / `iter[T]` / `tuple[...]` elements may be widened, not narrowed |
| `type.variance.contravariant` | a callable must accept everything the destination will pass |
| `type.variance.nominal` | a class annotation accepts only that class or a subclass |
| `type.callable.arity` | callable / tuple arity mismatch |
| `type.union.members` | no union member accepts the value |

The full table (which constructor is invariant/covariant/contravariant, and
why) is machine-readable: `gustyc --variance` prints it, and
`gustyc --schema` declares both the `diagnostic` and `varianceRule` shapes.

## Exit codes (deterministic)

Every failure *class* has its own code, so a script or agent can branch on what happened
without scraping stderr. These are the codes the binary emits — `TestCLIExitCodeContract`
asserts each row.

| Code | Meaning | Emitted by |
|------|---------|-----------|
| 0 | success | any mode |
| 1 | **compile error** — the program never ran: parse, analysis, a codegen refusal, or `llc`/`cc` failed. Diagnostics were emitted | `--eval`, `--check`, `--verify`, `--build`, `--emit-*`, `--fmt-check` |
| 2 | **LLVM rejected the module we emitted** — a compiler bug, not a source error (see ADR 0164/0166) | `--build` (when the verifier stage rejects), `--verify-llvm` |
| 3 | **runtime error** — the program compiled and ran, then trapped (an uncaught exception, a failed built-in) | `--eval`, `--file`, `--repl` |
| 4 | **usage error** — bad/unknown flags, no source given, unreadable file, empty `--bench-dir`, missing baseline file | any mode |
| 5 | **benchmark regression** (`--bench-baseline` gate fired; see `docs/benchmark.md`) | `--bench-*` |

Two distinctions this table exists to make:

- **1 vs 3** — "my program is malformed" and "my program crashed" are different events, and
  before this they shared code 1 (and usage errors shared 2 with LLVM rejections), so
  neither could be handled separately (roadmap Gap J.3).
- **1 vs 2** — a source error and a compiler bug must never look alike. Codegen refuses what
  it cannot lower (1, with an actionable message); only LLVM's own verifier saying *no* to
  what we produced is 2.

`--eval` is the interactive path: it parses and runs without the checker, so an undefined
name there is an execution failure (3), not a front-end one (1) — `--check`/`--build` are
the modes where the checker gates.

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

**Variance rules are static (L6.6).** The invariant/covariant/contravariant
rules run in the semantic checker (`--check`, `--verify`, the LSP), not in the
backends. The interpreter additionally enforces *nominal class annotations* at
runtime (`a: Animal` rejects a `Rock`, accepts `Dog`/`Puppy`) and treats the
read-only protocols structurally (`Sequence[T]` accepts any container, a tuple
annotation accepts the runtime list representation); the AOT backend treats
annotations as static-only, as it does for the rest of the annotation surface.

## CLI / REPL

`gustyc` (in `cmd/gustyc`) provides:

- `--eval <src>` / `--file <path>`: run the program. stdout is **only** what the program
  printed — the CLI no longer appends the value of the last statement (`print(1)` used to be
  followed by a stray `0`, the void `print` returned before ADR 0172). A *snippet* whose last
  statement is a bare expression still echoes its value, so `--eval "x = 1 + 2\nx"` prints
  `3`; a program ending in a call that yields `None` prints nothing extra.
- `--verify <src>`: parse + analyze, exit 1 on diagnostics
- `--emit-llvm <src>` / `--emit-ast <src>`: machine-readable IR / AST JSON
- `--lang`: self-describing feature list for agents
- REPL (interactive): accumulates input line by line so multi-line suites (functions, classes, `if`/`for`/`while` bodies) can be entered; shows `> ` primary and `... ` continuation prompts on a terminal; recovers from parser/eval panics so a bug never kills the session.

Exit codes: 0 ok, 1 runtime/eval error, 2 parse/usage error.

## JSON output for agents

`gustyc --json` emits machine-readable JSON on stdout:

- `--json --eval "x = 1 + 2\nx"` → `{"result": "3", "exit": 0}`
- `--json --verify <src>` → `{"ok": true, "exit": 0}` or `{"diagnostics": [...], "exit": 1}`
- parse errors → `{"ok": false, "phase": "parse", "error": "1:7: unexpected token",
  "errors": [{"line": 1, "col": 7, "msg": "unexpected token"}], "exit": 1}` — the spans
  are in the payload, so no agent has to parse `gustyc: parse error at 1:7: …` off stderr
- runtime errors → `{"error": "...", "traceback": "...", "exit": 3}`
- `--json --verify-llvm <src>` → the `irVerification` record, e.g.
  `{"ok":true,"tool":"/usr/bin/opt-20","skipped":false,"pipeline":["verify"],"toolchain":"LLVM 20"}`
  (see [Module verification](#module-verification-irverification-l82))
- `--json --emit-llvm <src>` / `--json --emit-ast <src>` on a compilation
  failure → `{"ok": false, "phase": "compile", "error": "<message>", "exit": 1}`
  (the human path prints `gustyc: <message>` on stderr; the exit code is the
  same either way, so an agent never has to parse stderr prose)
- `--json --bench-suite` (optionally with `--bench-baseline <path>`) → the
  benchmark suite artifact plus the gate verdict:
  `{"schema_version","generated_by","runs","opt_level","cases":[...],"totals":{...},"regressions":[...],"new_cases":[...],"exit":N}`
  — see `docs/benchmark.md` and the `benchSuite` / `benchRegression` /
  `benchBaseline` definitions in `--schema`.

### Benchmark suite flags (L10.4)

| Flag | Meaning |
|------|---------|
| `--bench-suite` | measure the built-in corpus on both backends |
| `--bench-dir <dir>` | measure every `*.gy` in a directory (the parity programs become benchmark cases) |
| `--bench-baseline <path>` | gate the run against a saved baseline; exit 5 on a regression |
| `--bench-baseline-update <path>` | write the measured suite as a baseline artifact |
| `--bench-gate aot\|interpreter\|both` | which leg the gate watches (default `aot`) |
| `--bench-tolerance <f>` | slowdown multiplier that trips the gate (default 1.5) |
| `--bench-min-ms <f>` | noise floor in ms; sub-floor baselines are never gated (default 0.25) |
| `--bench-runs <n>` / `--bench-opt <n>` | runs per backend (best-of-N wins) / AOT opt level |

Diagnostics serialize their `Msg`/`Span` fields for schema-driven tooling.

## Codegen capability messages (AOT)

When the LLVM backend cannot lower something that the language allows, it fails
as a *compile* error naming the limitation — never as IR that `llc` rejects.
Match these messages rather than scraping diagnostics prose:

| Message substring | Meaning | Workaround |
|---|---|---|
| `strings inside runtime containers are not supported by the AOT backend yet` | a `list[str]` / `dict[str, int]` element would have to store an `i8*` in an `i32` heap slot (roadmap Gap I.2) | run the program on the interpreter (`--eval`, `--file`), or keep container elements numeric |
| `unsupported call "` | a call the AOT backend cannot lower, e.g. calling through a `Callable` parameter (`def apply(f, x): return f(x)`) | interpreter path, or dispatch on a class with methods |
| `strings are not supported as function arguments in the AOT backend yet` | a string is an `i8*` constant, so passing one to a user function would emit `call i32 @f(i32 @.str1)`, which LLVM rejects (roadmap Gap J.5 / I.2). The message names the offending parameter | run the program on the interpreter, or pass numbers/containers and format the text at the call site |

Every container that crosses a function boundary is passed as a runtime heap
handle (see `docs/language.md` § Containers across function boundaries); the
parameter kinds are inferred from annotations, defaults and call sites, so no
extra syntax is required at the call site.

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
`Callable[[int, str], bool]`, structural protocol bounds `Sequence[T]` /
`Iterator[T]`, and bare class names (`a: Animal`) for nominal class types.
Assignment, call arguments and returns are checked by the structural
`subType(got, want)` relation, which implements the variance table
(`gustyc --variance`): `list`/`set`/`dict` invariant, `Sequence`/`iter`/`tuple`
covariant, `Callable` parameters contravariant + return covariant, classes
nominal over the declared base chain. `Sequence[int] = [1, 2]` and
`Sequence[int] = (1, 2)` pass; `str` is `Sequence[str]` (not `Sequence[int]`),
and an `int`/`dict` is not a sequence, so those are rejected. Each rejection
carries a stable `code` and a `suggestion` (see the code table above), so an
agent can branch on the rule rather than the message text:

**Match exhaustiveness (ADR 0154).** A `match` with only refutable (literal)
cases and no irrefutable case (`case _:` or a bare-name `case y:`) is
non-exhaustive; `gusty check` emits a mypy-style *warning*. Warnings appear
in the human output and the `--json` machine path but do not change the exit
code (only `LevelError` does). Reading a name bound by an irrefutable case
on every path is accepted (definitely assigned); reading one bound on only
some paths is an `undefined name` error. A function-name reference (bare `fn`) is
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
