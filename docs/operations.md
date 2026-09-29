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
| `--file <path>` | run a source file (with the interpreter unless `--aot`/`--jit` is given; the payload says which) |
| `--eval <src>` | compile-and-run source from argv |
| `--aot` | run through the compiled LLVM backend (alias of `--jit`) |
| `--interp` | run through the AST interpreter explicitly; conflicts with `--aot`/`--jit` (usage error, exit 4) |
| `--show-backend` | print `gustyc: backend <interpreter\|aot>` on stderr (stdout stays the program's) |
| `--gc-stats` | report what the garbage collector did while the program ran (`collections`, `roots`, `skipped`, `marked`, `freed`, `live`, and for the compiled backend `top`), on stderr for either backend; with `--json` the same numbers arrive as a `gc` member of the payload (L7.2, ADR 0181) |
| `--emit-llvm` | print the emitted LLVM IR |
| `--emit-ast` | print the JSON AST dump |
| `--verify <src>` | run the front end (lex + parse + semantic analysis) and report diagnostics, without executing |
| `--oracle <src>` | run `<src>` through **three** engines — AST interpreter, compiled backend, CPython — and report whether gusty behaves like Python (`--json` for the leg-by-leg report; exit 6 divergence, 7 no verdict) (L11.9, ADR 0186) |
| `--oracle-file <path>` | same, reading the program from a file |
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
| `--effects <src>` | print each function's **effect signature** (effects performed, return shape, control-flow termination) for a source string; `gusty effects <file>...` for files, `--json` for the schema-declared document (L7.6, ADR 0195) |


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

**The reserved words are only the grammatical ones** (Gap R.9, ADR 0203). A built-in name — `print`,
`range`, `len`, `str`, `int`, `abs`, `sum`, … — is an ordinary identifier: generating a function, a
parameter, a keyword-argument name or a method called `print` or `range` is legal and never produced
`parse error: expected identifier`, so an agent need not keep a list of taboo names. Real keywords
(`def`, `if`, `while`, `for`, `in`, `class`, `return`, `match`, `case`, `try`, `except`, `finally`,
`yield`, `lambda`, `import`, `with`, `as`, `not`, `and`, `or`, `is`, `None`, `True`, `False`) stay
reserved, and a name that collides with one is reported as `expected identifier` at the definition.

**A diagnostic is never repeated** (Gap R.7, ADR 0202). The analyzer records each fact once, keyed
by level, position, code and message, so `diagnostics` in `--json` is a set: its length is a count
of findings, grouping by `code` is meaningful, and the CLI prints no line twice. Distinct findings
are preserved by contract — two messages at one position, one message at two positions, the same
sentence as a warning and as an error, and one message under two codes are all separate entries. Do
not post-process to remove duplicates, and do not treat a repeated count as "the same issue in two
places" — if two positions are wrong, you are told twice.

**A built-in name claimed by a `def` must be defined above every module-level use** (Gap R.12,
ADR 0205). `def print`, `def range`, `def len` are legal, and the program's definition wins from its
`def` onwards. A module-level call to that name *above* the definition is refused —
`"range" is a built-in here, but this module defines it below, at line 5: … move the definition above
every use, or rename it` — because that is the one place the interpreter (execution order) and the
compiled backend (definitions visible module-wide) would run different programs from one file. Calls
inside function bodies and methods named after built-ins are unaffected. For a code generator: emit
your `def`s before the code that calls them, or rename.

**A compiled backend that cannot do something says so, and never emits a broken module** (Gap R.2,
ADR 0209). Which runtime helper definitions a module carries is derived from what the emitted code
*references*, so `llc` should never be the one to notice a missing `@rt_*` helper — an LLVM rejection is
classified as a compiler bug by the exit-code contract, and shapes that used to trigger one are either
lowered or refused. Related: iterating a string computed at run time is refused with a message naming
the interpreter as the backend that runs it, and pointing at the shapes that do work (string literals,
constant indexing) rather than compiling to a loop that silently runs zero times (Gap R.16).

**`for x in <integer>` is a repeat count** (Gap R.14, ADR 0207): it binds `0 … n-1` in both backends,
`n` may be any integer expression, and `n <= 0` runs the body zero times. CPython rejects the construct,
so `programs/for_int_count.gy` carries an oracle-excluded ledger row. For a code generator the useful
fact is that it needs no built-in: a module that has claimed the name `range` for itself (ADR 0205) can
still write a counted loop. Inside a comprehension the integer form is interpreter-only — the compiled
backend asks for `range(n)` there, with that as its message.

**Parameter order is free: a default may sit anywhere in a signature** (Gap R.11, ADR 0206).
`def f(a, b=1, c)` is legal and callable here — positionally (`f(1, 2, 3)`) and by keyword
(`f(1, b=2, c=3)`) — where CPython refuses the definition. A generator that emits signatures should
not sort its parameters to satisfy a Python rule this language does not have; it only has to give
every parameter a way to be filled, which the checker verifies.

**The calling contract is enforced in both directions** (Gap R.10, ADR 0201). A user-defined
function is checked for arity at the call: `function "f" expects N arguments, got M` (positional
shortfall), `function "f" accepts N arguments, got more` (too many), `function "f" is missing
argument "p"` (a keyword call that skipped a name), and `method "m" ...` for attribute calls — the
callee is always named, so an agent can act on the message without re-reading the file. A
parameter with a default counts as supplied. Built-ins keep their own rules and are not measured
against user definitions. Once arity fails, the callee's body is not analysed for that call, so a
single mistake yields exactly one diagnostic — do not expect, or program against, follow-on
`undefined name` errors inside the callee.

**Name-based lookups are keyed by where the definition lives, not only by what it is called**
(Gap R.5/R.8, ADRs 0197, 0200). A `def` in a class body is a method of that class and never a
binding of the file; a `def` in a function body is visible to that whole body, wherever it is
written; a bare-name call resolves to module functions and nested defs only. For an agent this means
a refusal like `undefined name "x"` pointing *inside a callee* is a resolution story about which
definition the call was measured against, not a scoping fact about the source — and the fix belongs
under `Analyze`, not in the program.

**A built-in name is shadowable, and the program's definition wins** (Gap R.6, ADR 0199).
`def str`, `def float`, `def len`, `def abs` are legal, and after them `str(1)` calls the program's
function on both backends. Agents should not warn themselves out of this: it is not a lint, it is
the language (CPython behaves the same way). What changed is that codegen no longer reads a call
through the built-in's *meaning* — its float shape, its constant fold, its `%s` print — for a name
the program owns; the fold still applies when nothing shadows the name, so an ordinary program pays
nothing. `def print` / `def range` remain parse-time refusals, and a module function colliding with
a *method* name is the open R.8. The predeclared-name table lives in one place
(`pkg/lang/predeclared.go`), shared by the checker, the codegen guard and LSP completion; the shadow
decision lives in `builtinShadowed`, so "is this name the program's?" has exactly one answer.

**`undefined name` follows the declaration-order rule, not the file's order** (Gap R.5,
ADR 0197). Inside a function body — and inside a class body — a name resolves to any `def` of
that scope, wherever it is written, so mutually recursive functions and helpers declared
below their callers check clean and compile. What runs where it is written still demands the
name above it: a call at module or class top level, and a decorator expression. An agent
therefore never has to order declarations to satisfy the checker, and a refusal with this
message always means the name does not exist in the scope, not that it appears later in the
file.

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

### A bare program: `gustyc prog.gy`

```
gustyc prog.gy            # same as --file prog.gy: compile and run it
gustyc "print(6 * 7)"     # not a path: evaluated as source, like --eval
gustyc prog.gy --json     # flags may follow the program
```

A single positional argument is the source to run — a file that exists is compiled and
executed (identical to `--file`, including `--json` output and exit codes), and anything else
is treated as source text. A name ending in `.gy` that does **not** exist is a usage error
(exit `4`) saying `no such file`, rather than being compiled as a program and failing with a
runtime `undefined name prog` — a shell mistype should not be reported as a bug in the user's
code.

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
{ "name": "sync", "symbol": "gy_sync", "irLine": 9, "line": 5, "col": 5 }
```

**IR symbol naming** (Gap R.4, ADR 0198). An emitted module defines every function the program
wrote under a `gy_` prefix: `def sync(x)` becomes `define i32 @gy_sync(i32 %p0)`, a method
`Point.x` becomes `@gy_Point_x`, a generated lambda `@gy_lambda_0`, an imported module function
`@gy_lib$f`. Names the program does not define keep theirs — the runtime helpers (`@rt_alloc`,
`@rt_frame_open`, the container printers), the C library (`@printf`, `@snprintf`), the generated
entry point `@main`, and every `extern fn` (`declare i32 @strlen(i8*)`, called as `@strlen`).

For a tool that means: to find a program's function in `--emit-llvm` output, grep `@gy_<name>`;
to tell a program-defined function from a host or runtime symbol, look for the prefix; and in a
source map or a debugger, `name` is what the source says and `symbol` is what the linker sees —
never strip the prefix by hand, and never assume the two are equal.

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

The variance + generics rules (L6.6) and the parser report these codes from
`--check`, `--verify`, `--json`, and the LSP:

`--check` (and `gustyc check <files>`) reports **every** recovered error, not
just the first: the lexer recovers from a bad character (L4.1), the parser
recovers per statement, and the statements that did parse are still type-checked.
They arrive in document order, and a broken file still yields the type errors
below the broken line — one run tells you everything the toolchain knows.

| Code | Rule |
|------|------|
| `type.mismatch` | plain kind mismatch (`expected int, got str`) |
| `type.variance.invariant` | `list[T]` / `set[T]` / `dict[K, V]` type arguments must match exactly |
| `type.variance.covariant` | `Sequence[T]` / `iter[T]` / `tuple[...]` elements may be widened, not narrowed |
| `type.variance.contravariant` | a callable must accept everything the destination will pass |
| `type.variance.nominal` | a class annotation accepts only that class or a subclass |
| `type.callable.arity` | callable / tuple arity mismatch |
| `type.union.members` | no union member accepts the value |
| `parse.error` | the source did not parse — a parser error or a lexer error token that recovery turned into a diagnostic |
| `async.coro.never_awaited` | a coroutine was created and nothing awaited it, so its body never runs (error: refused) |
| `async.coro.awaited_twice` | one coroutine object is awaited twice on a path (error: refused) |
| `async.generator.unsupported` | an `async def` whose body yields — no backend lowers async generators (error: refused) |
| `async.await.outside_coroutine` | `await` inside a plain `def`: evaluated here, a `SyntaxError` in Python (warning) |
| `async.async_stmt.outside_coroutine` | `async for` / `async with` inside a plain `def`: behaves as the plain form (warning) |
| `async.await.not_coroutine` | `await` pointed at a value that provably is not a coroutine (warning) |
| `async.missing_return` | an `async def` path runs off the end while other paths promise a value (warning) |

The full table (which constructor is invariant/covariant/contravariant, and
why) is machine-readable: `gustyc --variance` prints it, and
`gustyc --schema` declares both the `diagnostic` and `varianceRule` shapes.

The `async.*` rules (L7.6, ADR 0195) are the await/return discipline of the
async surface — what must hold for `async def` to mean anything. Their input,
the per-function effect signature the rules were decided from, is machine-readable
too: `gustyc --effects` prints it and `--schema` declares `effectSummary`.

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
| 6 | **divergence from CPython** — the program compiled, ran, and printed something other than what CPython prints for the same source (a wrong value, or a leg that refused it) | `--oracle`, `--oracle-file` |
| 7 | **no verdict** — the CPython leg could not run the source (gusty-only surface such as `await` at module scope, a positional set subscript, or a stdlib attribute Python has no name for), so nothing was checked | `--oracle`, `--oracle-file` |

Two distinctions this table exists to make:

- **1 vs 3** — "my program is malformed" and "my program crashed" are different events, and
  before this they shared code 1 (and usage errors shared 2 with LLVM rejections), so
  neither could be handled separately (roadmap Gap J.3).
- **1 vs 2** — a source error and a compiler bug must never look alike. Codegen refuses what
  it cannot lower (1, with an actionable message); only LLVM's own verifier saying *no* to
  what we produced is 2.
- **1/3 vs 6** — "my program is malformed", "my program crashed", and "my program ran fine and
  gusty answered differently from Python" are three different events. Code 6 is a statement
  about the *toolchain's* correctness, not the caller's: the program was accepted, executed,
  and produced an answer that does not match the reference behaviour. It is the finding an
  agent iterating on a language bug needs, and it must not be folded into "your program is
  wrong" (ADR 0186).
- **6 vs 7** — a measured divergence and "the oracle could not judge this" are not the same
  outcome. Reporting "we never checked" as success is how a corpus turns into a rubber stamp.

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
  printed, in both backends: the echo of the last value is keyed on *where the source came from*,
  not on what the last statement looks like (ADR 0204).
  - **A file** (`--file`, the default interpreted run, `--interp`) echoes nothing at all, so
    a program ending in `f(5)` prints exactly what it printed — identical to `--aot` and to
    `python prog.py`. Nothing is ever appended to piped stdout.
  - **A snippet** (`--eval`) keeps the prompt courtesy: a final bare expression echoes, so
    `--eval "x = 1 + 2\nx"` prints `3`, while `--eval "print(7)"` prints just `7`.
  - **`--json` reports `result` either way.** For a file it is the evaluated value of the last
    statement — metadata about the evaluation, *not* program output; do not concatenate it onto
    what the program printed.
- `--verify <src>`: parse + analyze, exit 1 on diagnostics
- `--emit-llvm <src>` / `--emit-ast <src>`: machine-readable IR / AST JSON
- `--lang`: self-describing feature list for agents
- REPL (interactive): accumulates input line by line so multi-line suites (functions, classes, `if`/`for`/`while` bodies) can be entered; shows `> ` primary and `... ` continuation prompts on a terminal; recovers from parser/eval panics so a bug never kills the session.

Exit codes: 0 ok, 1 runtime/eval error, 2 parse/usage error.

## Incremental parsing (LSP)

The language server keeps a span-keyed `ParseCache` per document and re-parses
only the statements an edit touches (`gustyc --lsp`, `textDocument/didChange`).
Each `textDocument/publishDiagnostics` notification carries a self-report of
what that pass did, so a client never has to guess or time the server:

```json
{
  "method": "textDocument/publishDiagnostics",
  "params": {
    "uri": "file:///a.gy",
    "version": 2,
    "diagnostics": [],
    "parseCache": { "statements": 4, "reusedStatements": 3, "incremental": true }
  }
}
```

- `statements` — top-level statements in the current parse tree.
- `reusedStatements` — statements preserved as the **same AST nodes** across this
  update, on either side of the edit. `0` means the document was re-parsed whole.
- `incremental` — whether any reuse happened at all.

Reuse is deliberately conservative: it applies to a single edit confined to one
line that adds no line, and only when the text after the edit is byte-identical
apart from a shift. A reused node keeps its source spans, so an edit that moves
lines re-parses rather than leaving hover and squiggles pointing at stale lines.

If a change cannot be applied (an out-of-range or overlapping span), the server
keeps the previous buffer and publishes a severity-2 warning
(`could not apply the last incremental change; resend the full document`) rather
than replacing the document with the change's fragment. Send a `didChange` with
no `range` (full text) to force a clean re-parse.

## The CPython oracle leg (L11.9, ADR 0186)

Parity — the interpreter and the compiled backend printing the same bytes — is a necessary
contract, and it is not a sufficient one: two backends that share a bug agree. `print(True)`
printed `1` on both sides of a green build for a hundred ADRs, and so did `len("café") == 5`,
`"abc"[1] == 98`, and a trap where Python answers `3` for `xs[-1]`. The third leg closes that
hole: **a program is conformant when both backends agree *and* what they print is what
CPython prints for the same source.**

### From the command line

```console
$ gustyc --oracle 'print(1 + 1)'
oracle: match (parity yes)
  interpreter ok       matches CPython
      | 2
  aot         ok       matches CPython
      | 2
  python      ok       the oracle
      | 2
  rules: set-order

$ gustyc --oracle 'print(True)'; echo $?
oracle: debt (parity yes)
  interpreter ok       differs from CPython
      | 1
  aot         ok       differs from CPython
      | 1
  python      ok       the oracle
      | True
  note: interpreter stdout differs from CPython
  note: compiled stdout differs from CPython
  rules: set-order
6
```

`--oracle-file <path>` is the same mode for a file. `--json` prints the `oracleReport`
(`legs`/`parity`/`oracle`/`notes`/`rules`, described by `--schema` → `definitions.oracleReport`),
and the exit status classifies the outcome: **0** match, **6** debt, **7** no verdict. A leg that
fails to run — a codegen refusal, a trap, a Go panic in the compiler — is recorded with its
first error line and counts as *not matching*: a refusal is a divergence, never a pass
(ADR 0166's rule, applied to the matrix).

The oracle interpreter is `python3` unless `GUSTY_PYTHON` names another, and it runs with
`PYTHONHASHSEED=0` so a run is reproducible. The version that produced an artifact is recorded
in the artifact itself (`toolchain.python`, alongside `toolchain.llvm`), because "matches
Python" is a claim about a named toolchain, not an abstraction.

### The three pinned toolchains

Building and testing this repository depends on three external tools, and each is pinned,
**checked**, and printed by CI's toolchain step — a version that is merely logged is documentation,
a version that gates something is a contract:

| tool | pin | where declared | what a mismatch looks like |
|---|---|---|---|
| **Go** | >= 1.22 | `go.mod`'s `go` directive **and** `go-version` in `.github/workflows/go.yml`, kept equal (ADR 0194) | A stdlib call newer than the floor used to compile locally and fail only in CI: the `go` directive gates language features, **not** stdlib API availability, so `strings.ContainsFunc` (Go 1.21) built fine on a 1.22 laptop against a declared 1.20 floor. The two declarations now match the development toolchain, and the CI step exits non-zero below the floor. |
| **LLVM** | 20 (`llc-20`) | roadmap + the workflow's apt line (`apt.llvm.org/noble`, `llvm-toolchain-noble-20`) | Recorded per artifact as `toolchain.llvm`; a missing `llc-20` fails the install step rather than becoming "no toolchain found" skips, which are never counted as passes. |
| **CPython (oracle)** | >= 3.12 | `lang.OracleMinPython`, matrix `toolchain.min_python` (ADR 0193) | The conformance legs take their expectations from CPython, so an oracle too old to parse a program (PEP 695 `type X = int`) reported compiler drift; `requirePinnedOracle` now fails with the remedy instead. Override with `GUSTY_PYTHON`. |

When CI's version of any of these differs from yours, the fix is not to reason about the difference —
it is to eliminate it, either by removing the discrepancy or by running CI's toolchain locally (the
`golang.org/dl/go1.XX` SDKs make that a one-liner: `go1.20 build -tags=llvm20 ./...`).

### The oracle is a versioned toolchain, and the pin is enforced

The oracle is pinned the way LLVM is: **CPython >= 3.12** (`lang.OracleMinPython`, recorded in the
matrix as `toolchain.min_python`, alongside `toolchain.python` = the banner of the interpreter that
actually ran and `toolchain.llvm`). The pin is a requirement, not a preference — the corpus contains
rows whose expected answer can only come from a CPython that can *parse* the construct under test.
`programs/typealias.gy` opens with `type Count = int`, which is PEP 695 syntax: on Ubuntu 22.04's
Python 3.10 the oracle leg died with `SyntaxError` on line 1, the row flipped from `match` to
`not_applicable`, and CI reported it as **oracle drift on a compiler case** — the note said only
"the CPython leg did not complete". A stale oracle is an environment fault, and an environment fault
has to read like one, so:

- `TestConformanceMatrix` runs `requirePinnedOracle` first and fails with
  `the oracle is Python 3.10.12 but the corpus is validated against 3.12 … install a newer python3 or set GUSTY_PYTHON`,
  instead of emitting a pile of drift that points at the compiler.
- `.github/workflows/go.yml` pins the runner to `ubuntu-24.04` (Python 3.12) and has a setup step
  that prints `go` / `llc-20` / `python3` versions and exits non-zero if the oracle is below the
  pin. A red setup step is a better signal than a green build with a meaningless matrix.
- `lang.OracleVersion` parses a version banner, and an **unidentifiable** banner counts as unknown,
  never as too old — a machine whose oracle cannot be read must not be told it is unsupported.
- A python leg that fails with a `SyntaxError` gets an extra note naming the pin and the remedy
  (`lang.OracleTooOldHint`), because "line 1 SyntaxError" on a program the interpreter accepts is
  almost always the toolchain.

Override the oracle with `GUSTY_PYTHON=/path/to/python3.12` when the default `python3` is too old.

### The matrix (schema 1.2)

`go test ./integration/ -run TestConformanceMatrix` writes `integration/conformance-matrix.json`:
one row per program, three legs each. New in 1.2 — `toolchain.min_python`, the pinned minimum
CPython the ledger's declared verdicts presuppose (see "The oracle is a versioned toolchain"
below). Everything from 1.1 still stands: `python_stdout` / `python_ok` /
`python_error`, `interp_matches_python` / `aot_matches_python`, the computed `oracle` with its
`oracle_declared` counterpart, `oracle_reason` / `oracle_ref` / `oracle_rules` /
`oracle_notes` / `oracle_drift`, the `rows` / `skipped` counters, the `oracle_match` /
`oracle_debt` / `oracle_not_applicable` / `oracle_drift` counters, and the `toolchain` block.
`--schema` → `definitions.conformanceRow` documents the row shape.

### The ledger, and why absence means "must match"

`integration/conformance_cases.go` declares each case's state. A case with **no ledger row is
declared `match`** — new programs are expected to be conformant, and if they are not, the build
fails until the divergence is written down with:

- a **reason** (one sentence: what is wrong),
- a **ref** (the roadmap item that owns the fix — an unowned divergence is an unfixable one),
- and a **pin per leg**: the exact stdout that leg produces today, or `Missing: true` (optionally
  with an `Err` substring such as `compiler panic`) for a leg that does not complete.

`ConformanceCase.OracleCheck` compares the claim with the observation and returns *drift*, and
drift fails the build **in both directions**: a row that got worse, and a row that got better
(`oracle debt is paid: both backends now print CPython's answer — update the registry`). A
ledger that cannot be falsified is decoration.

Two comparisons would otherwise be meaningless and are normalised by one named rule, on by
default and echoed per row: `set-order` treats a bare `{…}` rendering with no `k: v` entry as an
unordered multiset, because CPython's set iteration order depends on hash seed and insertion
history. A dict rendering keeps its order — that one is observable in both languages. Any extra
rule a row declares is applied and listed, including names the compiler does not implement, so a
stale claim stays visible instead of silently passing.

### Probes, and the promotion rule

`conformanceProbes()` registers `integration/programs/probe_*.gy`: programs that reproduce a
roadmap Phase 11 defect on today's compiler (nested containers, heterogeneous elements, tuples,
negative indexing — including the one that panics the Go compiler — code-point strings, stdlib
constant types, floored `//` and `%`, `sorted`/`enumerate`, calling a function through a
parameter, `print(set())`, print atomicity). They are compared to CPython and pinned, but not
asserted for parity — a probe often fails on one leg by design. When a probe's pins stop
matching because the answers became Python's, the row fails with the promotion instruction:
delete the ledger row and move the program into `conformanceStandalone()`, where it becomes
permanent parity surface. Nothing may be marked DONE on parity alone while Phase 11 is open.

All three legs run behind `recover()`, so a compiler panic is one bad row instead of a dead
test binary.

### Writing or refreshing a row

`go run ./tools/oracleprobe probe_tuple merged:ctrl_a,ctrl_b,ctrl_c` prints the three legs and
the verdict for any program or merged group, in the same shape the harness computes — the ledger
is written from measured output, never from memory. Rows are checked by `integration/oracle_test.go`:
every row must describe a registered case, every exception must carry a reason and an owner,
debt rows must pin both legs, and a stubbed pin must produce drift (the harness is tested
against its own ability to fail).

## JSON output for agents

`gustyc --json` emits machine-readable JSON on stdout:

- `--json --eval "x = 1 + 2\nx"` → `{"result": "3", "type": "int", "backend": "interpreter", "exit": 0}`
- every execution result carries `"backend"`: `"interpreter"` or `"aot"`. It is a fact
  about the run, not something to infer from the flag list — `--file` without
  `--aot` reports `"backend": "interpreter"` (roadmap Gap M.2). Captured-output
  runs (the compiled backend) report `{"output": "42\n", "backend": "aot", "exit": 0}`.
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
| `concatenating a runtime string is not supported in the AOT backend yet` | building a new string (`s + "!"` where a side is not a compile-time constant) needs a buffer allocation the compiled runtime does not have; passing the string itself is fine (ADR 0174) | run it on the interpreter, or concatenate the constant parts and pass the result |
| `operator "<op>" on a string is not supported in the AOT backend` | arithmetic or ordering on a compiled string would compute with its string-table index, where the interpreter raises `TypeError` (ADR 0174) | check the value before the operation, or run it on the interpreter |
| `string method <name> on non-constant string` | a method that would build a new string (`upper`, `strip`, …) needs an allocator; `len`, `==` and container use of a string parameter are supported | run it on the interpreter, or compare/measure instead |

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

## Collector: precise roots and safe points (L7.2, ADR 0181)

Both backends trace an *enumerated* root set and never guess at machine words.

The interpreter's roots are: the current environment, **every active call frame's
locals** (pushed on call, popped on return, so a dead frame retains nothing), the
root groups that constructs declare (a `for` loop's iterable, a loop's last value),
and the permanent roots (`None`, the generator accumulator, the `super()` receiver,
class objects). Two rules make collection sound in a tree-walking interpreter:

- **Watermark.** Everything allocated after the last safe point is unconditionally
  live, because the interpreter may be holding it in a register.
- **Safe point.** The watermark advances only at a statement boundary with no
  expression evaluation in flight, reached from a construct that declared its root
  groups — or from a call that *is* the statement (`work()`, `total = helper(x)`),
  whose caller side is therefore free of unrooted temporaries.

Collection runs at those safe points once an allocation threshold is crossed; the
threshold is a constant, so a given program collects the same number of times every
run (parity stays assertable). Known conservative boundary: garbage created inside a
call that is *nested in an expression* is not reclaimed until the enclosing statement
completes — see ADR 0181 for why, and roadmap Phase 11 for what removes it.

### Collector self-report (`--gc-stats`)

```
$ gustyc --eval 'acc = 0
for k in range(400):
    row = [k, k]
    acc = acc + row[1]
print(acc)' --gc-stats
79800
gc: backend=interpreter collections=1 roots=3 skipped=3 marked=2 freed=254 total_freed=254 live=2 frames=0 protected=0 kind=full
```

The line goes to stderr (it describes the tool, not the program), so
`prog 2>/dev/null` still sees only program output. `--json` puts the same numbers in
the payload as `"gc": { … }` — see `definitions.gcStats` in `--schema`:

| Field | Meaning |
|---|---|
| `collections` | mark-and-sweep passes so far (cumulative) |
| `roots` | root entries the last collection traced that really named a heap object |
| `skipped` | root slots proved to hold raw immediates and never scanned — the number precision buys |
| `marked` / `freed` / `total_freed` / `live` | objects found reachable / reclaimed last pass / reclaimed overall / still resident |
| `frames` | call frames in the root set at the last collection (0 = a top-level boundary) |
| `protected` | objects the watermark kept alive without tracing |
| `generational` | true for a young (nursery) pass, false for a full sweep |
| `backend` | `interpreter` or `aot` |

### The compiled backend's report

The counters live inside the compiled program, so its own runtime prints the line
(`rt_gc_report` in `pkg/lang/codegen.go`), on fd 2 like every other tool-level line:

```
$ gustyc --aot --gc-stats --file integration/programs/gc_precise.gy
gc: backend=aot collections=102 roots=3 skipped=0 marked=51 freed=0 total_freed=4243 live=51 top=17
130
56850
719400
```

`top=` is the high-water mark of the compiled root stack (4096 entries available; a
program that exhausts it stops itself rather than run with an unrooted handle). The
`--aot`/`--jit` path captures the target's stderr, forwards it unchanged, and — with
`--json` — parses that line into the same `gc` member, so neither humans nor agents have
to scrape it. `lang.ParseGCStatsLine` and `GCStats.String` are one documented shape read
both ways, tested for round-tripping.

`GUSTY_GC_STRESS=1` forces a collection at every statement boundary: the conformance
corpus is run that way in `integration/gc_stress_test.go`, so a root the interpreter
forgot fails a test instead of appearing as a heisenbug.

`GUSTY_KEEP_LLVM=1` keeps the JIT's scratch directory instead of deleting it and prints
`keeping JIT scratch dir <path>` on stderr, so the `jit.ll` that `llc` rejected can be
read, minimised, and filed. Most compiler bugs in this project are *llc rejected this
module* failures, and the module was previously deleted with the temp directory: a
compiler whose IR failures cannot be inspected cannot be debugged, and an agent driving
it needs the artifact rather than a guess (ADR 0191). Note that `--emit-llvm <file>` is
a separate codegen entry point from the JIT and still refuses some shapes the JIT
compiles (`print(sorted([10, 2, 33]))` → `unsupported attr expression`); when the two
disagree, `GUSTY_KEEP_LLVM=1` is the way to see what the JIT actually built.

The compiled backend gets the same treatment from the other side: its roots are a stack
(`@gc.roots` + `@gc.kinds`, with `rt_root_put`/`rt_root_clear`/`rt_frame_open`/
`rt_frame_close`), every function — including class methods and closure helpers — pops
what it pushed, and every variable's slot is allocated once per call (`hoistAllocas`) so
a slot address identifies exactly one variable. `integration/gc_stress_test.go` pins
both: `live` must fall back after recursion returns, and `top` must stay small for a
program with few live variables. Both assertions were checked against stubs that remove
the mechanism.

## The value model: one tag table, one heap-kind projection

The number that says *what a value is* lives in exactly one Go table, and everything
machine-readable about it is generated from that table (ADR 0182):

```console
$ gustyc --lang | tail -2
values: int=0 float=1 bool=2 None=3 str=4 list=5 dict=6 set=7 tuple=8 class=9 instance=10 method=11 closure=12 exn=13 module=14
heap kinds (compiled runtime object headers): list dict set instance (0 = not heap-allocated)
```

- **The tags** are `pkg/lang/value.go`'s `ValueTag` list. The interpreter's heap objects
  (`obj.kind` → `obj.tag()`), the compiled runtime's tagged `%obj` values, and the
  extern-fn ABI's tag words all read it; `--abi` prints the tag words and
  `TestABITagsAgreeWithCanonicalTags` proves they are the same numbers.
- **The compiled heap's `kind` word** is a projection of it (`HeapKindFor`/`HeapTagFor`):
  `none=0, list=1, dict=2, set=3, instance=4`, because the compiled heap allocates only
  those four — an int, float, bool, `None` or interned string has no object header, which
  is what `HeapKindNone` means rather than "unknown". `rt_alloc`'s parameter, the
  collector's dispatch and `rt_inst_get`'s checks all use these, and codegen writes them
  through the named constants instead of literals.
- `definitions.valueTag` in `--schema` documents the numbering and the projection, and a
  test compares the list quoted there with the table, so the schema cannot drift.
- `lang.ValueTagNames()`, `lang.HeapKindNames()`, `lang.HeapKindFor`, `lang.HeapTagFor`
  and `lang.HeapKindNameOf` are the Go API for tools; the numbers are a wire format and
  `TestValueTagTableIsPinned` fails if anyone renumbers them.

`print(True)` still printing `1` is the honest limit of this step: bools are not values
yet in either backend (`--json` reports `"type": "int"` for `True`), so there is no tag to
print from. That is the next move in L11.1.

What L11.1 has opened since, in the LLVM backend, is element-level tagging: a mixed list
literal tags each slot, `xs.append(v)` appends payload-and-tag together
(`rt_append_tagged`), `xs[i] = v` rewrites the slot's tag with it, and reading `xs[i]` produces
the `(value, tag)` pair that `print(xs[i])` dispatches on and `v = xs[i]` binds as a tagged
variable (ADR 0184, ADR 0185, ADR 0187). What still refuses — with a message naming what does
work — is a tagged element reaching a context that needs one static kind (`xs[i] + 1`,
`xs[i] > 2`, `f(xs[i])`, `xs[i]` in a format spec), dict/set element tags, and the tag-less
kinds (bool, float, nested container).

Container **printing** is total on the AOT path (ADR 0188): a literal or a zero-argument
constructor in print position builds the runtime object and calls the runtime printer, so
`print([1, 2])`, `print([])`, `print({})`, `print(set())`, `print(list())`, `print(dict())` match
CPython where they previously refused to compile or printed the handle `0`. `rt_alloc` clears the
per-container "elements are interned text" flag (`@estr[h]`) on both the fresh and the recycled
path — a recycled slot used to inherit it, which made a numeric set print through the string table
as `{(null), (null)}`. `TestNoContainerGlobalInAValuePosition` is the module-wide invariant: no
`i32 @.lstN` / `@.dictN` / `@.setN` / `@.strN` in a value position, ever.

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

## Effect signatures (`gustyc --effects`, L7.6, ADR 0195)

`gustyc --effects <src>` (or `gusty effects <file>...`) answers *what a function does*
without running it:

```
$ gustyc --effects "$(cat eff.gy)"
effect signatures for <src>
  <module>             def        line top    effects=none returns=no falls_through=yes
  fetch                async def  line 1      effects=await,raise returns=yes falls_through=no
  plain                def        line 9      effects=none returns=yes falls_through=no
```

for `eff.gy` = `async def fetch(n)` with a loop that `await`s and sometimes `raise`s and
then returns, followed by a plain `def plain(x)`. The `fetch` row says everything an agent
needs to schedule it: async (a call builds a coroutine), it awaits and can raise, it
returns a value on every path (`falls_through=no`).

`--json` prints the same rows as a document — `schema_version`, `language_version`,
`generated_by`, `source`, `functions[]`, plus `diagnostics`, `ok` and `exit` — shaped by
`definitions.effectDocument` (whose rows are `definitions.effectSummary`) in `gustyc --schema`.
The verdict travels with the facts that produced it, so one call answers both "what does this
file do" and "is it honest": `ok`/`exit` mean the same thing they mean in the `--check`
document, and an agent never has to re-read stderr to learn why a program was refused.

| Field | Meaning |
|-------|---------|
| `function` | declaration path: `fetch`, `outer.inner`, `Box.load`, or `<module>` |
| `async` | true for an `async def`: calling it builds a coroutine and runs nothing |
| `line` | declaration line (0 for `<module>`) |
| `effects` | sorted effect names performed by the body: `await`, `yield`, `raise` |
| `awaits` / `yields` / `raises` | effect sites (a signature, not a profile: a loop body counts once) |
| `coroutine_calls` | calls to an `async def`, i.e. coroutine constructions |
| `returns_value` / `returns_bare` | some path returns a value / returns None |
| `falls_through` | control flow can run off the end, so the call — or the await — answers None |
| `terminates` | `!falls_through`: every path leaves via return, raise, break or continue |

The rows are exactly what the async rules are decided from, so the table and the
diagnostics cannot drift: a body whose `falls_through` is true while it promises a value is
the `async.missing_return` warning, a `coroutine_calls` count with no matching await is
`async.coro.never_awaited`, and a non-zero `yields` under `async: true` is
`async.generator.unsupported`. `--effects` prints the diagnostics on stderr and exits 1
when one of them is an error, so "summarise this file" and "is this file honest" are one
call. It is the machine path for the same pass the LSP publishes and `--check` gates on.

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
