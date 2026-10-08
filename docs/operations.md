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
| `--file <path>` | compile and run a source file — the only thing `--file` has ever done since ADR 0302 |
| `--eval <src>` | compile and run source from argv (same backend, in-process; the REPL is this path with a prompt) |
| `--aot` | accepted and ignored: the compiled LLVM backend is the only backend. Kept so no existing script breaks — a flag that silently selects nothing is honest here, a flag that *disappears* is not |
| `--jit` | accepted and ignored, as `--aot` |
| `--interp` | **retired** (ADR 0302): a usage error naming the retirement and pointing at `--aot`, not a silent no-op — a script that believes it chose the interpreter must find out at the flag, not from a wrong answer |
| `--show-backend` | print `gustyc: backend aot` on stderr, so a transcript records which engine answered even though there is only one (stdout stays the program's) |
| `--gc-stats` | report what the garbage collector did while the program ran (`collections`, `roots`, `skipped`, `marked`, `freed`, `live`, and for the compiled backend `top`), on stderr for either backend; with `--json` the same numbers arrive as a `gc` member of the payload (L7.2, ADR 0181) |
| `--emit-llvm` | print the emitted LLVM IR, from a **source string** (`--emit-llvm "$(cat prog.gy)"`); a path is parsed as source and answers a parse error |
| `--emit-ast` | print the JSON AST dump |
| `--verify <src>` | run the front end (lex + parse + semantic analysis) and report diagnostics, without executing |
| `--oracle <src>` | run `<src>` through **two legs** — the compiled backend and CPython — and report whether gusty behaves like Python (`--json` for the leg-by-leg report; exit 6 divergence, 7 no verdict) (L11.9, ADR 0186; the interpreter leg retired with ADR 0302, and the verdict is now `oracle: match` with no `parity` member, because parity was an engine-vs-engine claim and there is one engine) |
| `--oracle-file <path>` | same, reading the program from a file |
| `--verify-llvm <src>` | compile `<src>` and report LLVM's module-verifier verdict for the emitted module (L8.2) |
| `--verify-llvm-file <path>` | same, reading the program from a file |
| `--no-verify` | with `--build`, skip the module-verifier stage (it runs by default) |
| `--target <triple>` | target triple for codegen |
| `--opt-level <n>` | optimization level |
| `--debug` | compile with DWARF: `!dbg` line records in the module and a `.debug_line` table in the object, read back and reported (`--build`, `--emit-llvm`, `--jit`; § Debug info, L8.5, ADR 0231) |
| `--debug-info <src>` | print the DWARF line-table document for a source string — compile unit, one entry per program function, IR-line-to-source-line rows; `--json` for the `debugInfo` schema document (§ Debug info) |
| `--debug-info-file <path>` | same, reading the program from a file |
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
every use, or rename it` — because that is the one place the record (execution order) and the
compiled backend (definitions visible module-wide) would run different programs from one file. Calls
inside function bodies and methods named after built-ins are unaffected. For a code generator: emit
your `def`s before the code that calls them, or rename.

**A compiled backend that cannot do something says so, and never emits a broken module** (Gap R.2,
ADR 0209). Which runtime helper definitions a module carries is derived from what the emitted code
*references*, so `llc` should never be the one to notice a missing `@rt_*` helper — an LLVM rejection is
classified as a compiler bug by the exit-code contract, and shapes that used to trigger one are either
lowered or refused. Related: iterating a string computed at run time is refused with a message naming
the record as the backend that runs it, and pointing at the shapes that do work (string literals,
constant indexing) rather than compiling to a loop that silently runs zero times (Gap R.16).

**`for x in <integer>` is a repeat count** (Gap R.14, ADR 0207): it binds `0 … n-1` in the compiled backend,
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
function on the compiled backend. Agents should not warn themselves out of this: it is not a lint, it is
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

**Runtime failures (the compiled backend).** An exception that escapes the program is reported on
**stderr** and exits non-zero — never exit 0 with silence, which is how a trapped program
previously looked successful to a script:

```
Traceback (most recent call last):
  File "prog", line 3, in boom
IndexError: index out of range
```

The frame line names the file, the line of the raise, and the enclosing function
(`<module>` at top level), and the compiled backend agrees on it: the record prints one frame
per stack level (so a raise inside a called function shows the call site too), while the
compiled report shows the raise site's own frame — the call-stack frames need the line
tables of L8.5 (roadmap Gap K.8). The last line is identical on both. stdout stays clean, so
`prog 2>/dev/null | ...` sees only program output. With `--json` the eval path emits
`{"error": ..., "traceback": ..., "exit": 3}` on stdout. Runtime errors are typed
(`IndexError` / `KeyError` / `TypeError` / …), so `except IndexError:` catches them on
the compiled backend — see `docs/language.md` § Exceptions.


## Debug symbols / source maps (AOT)

`gustyc` emits machine-readable source maps and real DWARF for AOT builds:

- `--emit-source-map <src>` prints a JSON source map: each user function
  (top-level, nested, and class methods) mapped to its emitted LLVM symbol
  and 1-based IR line, plus the source line/col. Class methods are mangled to
  `<class>_<method>`. A value that names a file on disk is read as a file, and anything
  else stays source text — the same rule every other value-taking flag in this CLI follows.
- `--build out.bin --source-map-out a.smap.json src.gy` writes the same JSON
  source map alongside the binary.
- `--build out.bin --debug src.gy` compiles with debug info: the module carries `!dbg`
  line records, `llc` turns them into `.debug_line` in the object, and the link keeps them.
  It is *not* a `-g` on the link step — DWARF is decided by `llc`, from the module's metadata,
  before the linker is involved (L8.5, ADR 0231).

Example source map entry (source map v2 — `version: 2`, and `lines` carries the module's
IR-line-to-source-line table):
```json
{ "name": "sync", "symbol": "gy_sync", "irLine": 9, "line": 5, "col": 5 }
```

## Debug info: what the module claims and what the artifact carries (L8.5, ADR 0231)

Two statements are kept apart on purpose. **What the module claims** is read out of the emitted
IR's own metadata nodes. **What the artifact carries** is read out of the linked object with
`llvm-dwarfdump`. Neither is taken from what the compiler hoped to write.

```
$ gustyc --build prog.bin prog.gy --debug
built prog.bin (1 source files, 1 object file(s))
  verified by /usr/bin/opt-20 (verify)
  DWARF: prog.gy (DW_LANG_Python), 3 subprogram(s), 57/57 instruction(s) tagged, 8 location(s)
  DWARF line table: 21 row(s) over 8 source line(s) for prog.gy
```

`--debug` is honoured by `--build`, `--emit-llvm` (which then prints the very module a debug build
links, records included) and `--jit`. A build that does not ask for debug info gets no metadata at
all, and the `debug` / `dwarf` members are absent from its `--json` payload rather than empty.

### Reading a line table without a toolchain

`gustyc --debug-info "<src>"` (or `--debug-info-file <path>`) compiles the program with debug info
and reports the table the compiler put in the module. It needs no LLVM toolchain, because the
compiler is the author of that answer:

```json
{
  "schema_version": 1, "file": "prog.gy", "directory": "/tmp", "producer": "gusty 0.10.0",
  "language": "DW_LANG_Python", "emission_kind": "FullDebug", "is_optimized": false,
  "compile_unit": "!3",
  "functions": [{ "name": "total", "symbol": "gy_total", "line": 1,
                  "instructions": 38, "locations": 5 }],
  "lines": [{ "irLine": 142, "line": 1, "col": 1, "function": "gy_total" }],
  "instructions": 57, "tagged": 57, "locations": 8, "subprograms": 3,
  "lines_truncated": false
}
```

Shaped by `definitions.debugInfo` in `gustyc --schema`. Read from the module's metadata: a record
LLVM would ignore is reported as *missing*, not as coverage. `tagged`/`instructions` is the
coverage claim, `subprograms` is how many program functions are described, `locations` counts the
`DILocation` nodes (one per distinct line/column/function), and `lines` maps each tagged IR line to
the source line it was written for. `defect` is set when the module's metadata disagrees with what
the emitter meant to write; empty means the two agree.

Only program functions appear. The compiler's own blocks — GC frame bookkeeping, exception landing
pads, runtime helpers — have no subprogram and no rows: they are not code the program wrote, and a
debugger that stops in `rt_frame_open` and blames it for a user statement is worse than a debugger
with nothing to show.

### Reading the artifact

`--build --debug --json` adds a `dwarf` member, produced by running `llvm-dwarfdump --debug-line`
over the object that was just linked — shaped by `definitions.dwarfReport`:

| Field | Meaning |
|-------|---------|
| `tool`, `toolchain` | the `llvm-dwarfdump` that read the table, and the pinned LLVM version |
| `ran` | the tool ran, whatever it concluded |
| `skipped` | no toolchain was found; **never** reported as a pass |
| `ok` | true only when the artifact really carries `.debug_line` rows |
| `line_rows` | rows in the table, including rows that name no source line (the compiler's runtime blocks) |
| `source_lines` | the distinct source lines the table covers, ascending — "can a debugger stop at line N?" |
| `files` | the file names the table names, as a debugger will print them |
| `note` | why `ok` is false: no toolchain, no such file, the tool failed, or the artifact has no rows |

The integration suite goes one step further and asks `llvm-addr2line` where `gy_total` lives, plus
`llvm-dwarfdump --debug-info` that `DW_AT_language` says `DW_LANG_Python` — the questions a debugger
will actually ask, asked of a real binary (`integration/debug_info_test.go`).

### What is not claimed

Columns are where a statement *starts*: the parser records statement positions, not subexpression
positions, so `col` is a statement column and column-precise stepping needs positions on
expressions. A `!dbg` record is attached to instructions, definitions and terminators — never to
landing pads, catch switches or the metadata block. `DebugOptions.LineTableCap` bounds the `lines`
array of a huge module; when it cuts, `lines_truncated` says so and the counts above stay exact.


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

**Module bindings in the module** (Gap R.35, ADR 0227). A name the module binds once to a literal and
never rebinds is emitted as a constant wherever a function body reads it, so it leaves no symbol. A
module name that the module rebinds *and* a body reads becomes module state:
`@gy_mod_<name> = global i32 0`, stored by the top level and loaded by the callee at call time. A tool
that wants to know which of a program's names are module state can grep `@gy_mod_`; the name after the
prefix is the source spelling, and it is prefixed like every other program-owned symbol (ADR 0198).

A closure body that could not be lowered, in the single case where it provably does not run — a closure
nested in a function used as a decorator, whose decorated call goes through the trampoline — is said in
the module text rather than hidden:

```llvm
; note: closure wrap: body not lowered (codegen: unsupported call "g"); a decorated call runs the trampoline instead
```

That line is not an error and does not fail the build; it is the compiler recording a capability it did
not emit. `--emit-llvm` prints it, and a script that must refuse such programs can grep for
`; note: closure`.

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
| 2 | **LLVM rejected the module we emitted** — a compiler bug, not a source error (see ADR 0164/0166). Reached through `errors.As(err, *lang.ToolchainRejectionError)`, never by matching message text; a toolchain that is *not installed* is not a rejection and stays class 1 | `--build` (when the verifier stage rejects), `--verify-llvm`, `--aot`/`--jit` (when `llc` refuses what codegen produced) |
| 3 | **runtime error** — the program compiled and ran, then trapped (an uncaught exception, a failed built-in). Every run path reports it identically: the compiled backend's status comes from the generated `main`'s return value, which `lang.JITResult.Code` carries (ADR 0211), and since ADR 0228 so does the *linked binary itself* — `./prog` and `gustyc --aot prog.gy` agree, because 1 is the compile-error code and a trap must never borrow it | `--eval`, `--file`, `--repl`, `--aot`/`--jit`, and the binary `--build` leaves behind |
| 4 | **usage error** — bad/unknown flags, no source given, unreadable file, empty `--bench-dir`, missing baseline file | any mode |
| 5 | **benchmark regression** (`--bench-baseline` gate fired; see `docs/benchmark.md`) | `--bench-*` |
| 6 | **divergence from CPython** — the program compiled, ran, and printed something other than what CPython prints for the same source (a wrong value, or a leg that refused it) | `--oracle`, `--oracle-file` |
| 7 | **no verdict** — the CPython leg could not run the source (gusty-only surface such as `await` at module scope, a positional set subscript, or a stdlib attribute Python has no name for), so nothing was checked | `--oracle`, `--oracle-file` |
| 8 | **the toolchain never answered** — an external call (`llc`, `cc`, `opt`, `llvm-as`, `llvm-dwarfdump`) ran out of its budget and was killed (ADR 0312). Neither the program's fault (1) nor the compiler's (2): the module was never accepted or refused, the tool stopped. Reached through `errors.As(err, *lang.ToolTimeoutError)`, and the `--json` payload says `"phase": "toolchain"`. A tool that is *not installed* is not a timeout either — that stays class 1, because the advice is "install LLVM 20", not "raise `GUSTY_TOOL_TIMEOUT`" | `--eval`, `--file`, `--aot`/`--jit`, `--build`, `--verify-llvm`. The oracle leg is the exception: a killed `python3` is a **no-verdict (7)** whose note names the budget, because "the reference gave up" and "the reference said no" are different findings and only one of them indicts the compiler |

Two distinctions this table exists to make:

- **1 vs 3** — "my program is malformed" and "my program crashed" are different events, and
  before this they shared code 1 (and usage errors shared 2 with LLVM rejections), so
  neither could be handled separately (roadmap Gap J.3).
- **1 vs 2** — a source error and a compiler bug must never look alike. Codegen refuses what
  it cannot lower (1, with an actionable message); only LLVM's own verifier saying *no* to
  what we produced is 2.
- **1 vs 2 vs 8** — three different suspects, and the caller has to know which one to go and look at.
  Code 1 is *your program*, code 2 is *our compiler* (LLVM said no to a module we emitted), code 8 is
  *the machine you are standing on*: a tool that was killed for not answering. Before ADR 0312 the last
  of these had no code, so a hung `llc` on a loaded runner arrived as "compile error" and sent the reader
  to look for a bug in their own source.
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

Code 2 is a bug report rather than a result, and the roadmap tracks each measured class of it
until the class is empty (ADR 0166). One closed this round: a trapping element in a runtime
comprehension — `xs.append(6)` then `print([v / 2 for v in xs])` — was `llc` refusing the module
(exit 2), and now exits 0 with `[3.0]`, or 3 with the `ZeroDivisionError` the element earns
(ADR 0255, Gap R.100). A script that was special-casing that rejection should stop: the program
answers.

A second class closed, and four named as what is left of it (ADR 0263, roadmap Gap R.131). A
built-in called with no argument used to be reached before the question "is there an argument?"
was asked, so `gustyc --interp --eval 'print(round())'` died with a Go stack trace and **exit 2**
— the compiler-bug code spent on a program with a typo. `round` now answers its arity in words on
both engines, one sentence written once in `pkg/lang/round_digits.go` and read by both backends:
exit 1 compiled (`round expects 1 or 2 arguments, none given: …`, the program never ran) and exit 3
interpreted (the raise is a runtime event). Four built-ins still take the panic path on that shape
— `int()`, `float()`, `ord()`, `chr()`, `chr()` panicking the compiler as well as the evaluator —
and `int()`/`float()` are programs CPython runs (they are the conversions of zero) while codegen
refuses them. Until that row closes, exit 2 on a program whose only mistake is a missing argument
is Gap R.131, not a new finding.

The other half of `round`, the digit count, is the case where *not* refusing is the contract: a
digit count that is not an integer (`print(round(2.345, 1.5))`) is a program CPython stops on, so
both legs raise its `TypeError` — exit 3, catchable by `except TypeError` on each — and neither
returns exit 1, which belongs to programs the reference rejects. `--oracle` scores such a program
`not_applicable`: with the reference raising, there is no stdout opinion to match.

The third pair of the same family is `floor` / `ceil` / `sqrt` (ADR 0264, roadmap Gap R.51), and it moved
four programs from a wrong answer to a classified one. `sqrt(-1)`, `floor("a")`, `ceil(None)`,
`floor([1])`, `floor(float("nan"))` and `ceil(float("inf"))` are all programs the reference runs and stops
on, so both legs **raise** — exit 3, with CPython's own sentence (`math domain error`,
`must be real number, not str`, `cannot convert float NaN to integer`,
`cannot convert float infinity to integer`), each catchable by its class (`except ValueError` /
`except TypeError` / `except OverflowError`, verified on both legs). None of them is exit 1, and the arity
mistakes (`floor()`, `sqrt(1, 2)`) are exit 1 compiled and exit 3 interpreted from one shared sentence in
`pkg/lang/math_names.go`, never exit 2. Two engine-split rows are pinned in the matrix rather than
averaged: `programs/probe_whole_number_beyond_the_int_word` (the interpreted leg prints
`3000000000`, the compiled leg exits 3 with an `OverflowError` naming L12.12) and two new `oracle: debt`
rows found by walking the predeclared table and choosing test values — `programs/probe_float_literal_with_exponent`
(`1e18` does not lex; both legs report `parse error at 1:8: expected ")"`, exit 1) and
`programs/probe_predeclared_name_not_callable` (`pow(2, 3)` is CPython's `8`, our NameError interpreted and
our `unsupported call "pow"` compiled). A module that would die in `llc` is now caught in `pkg/lang` before
anyone links: `inf.0e+00` and `nan.0e+00` are on the shared `forbiddenIR` blacklist (Gap R.134).

Arithmetic on a container slot whose kind only the run time can describe is answered by the compiled leg as
well, in the print position (`print(xs[0][0] + 1)`, `print(xs[0][0] * 2)`, `print(-xs[0][0])` after
`xs.append([7, 8])`), and it is the one place where a *program-wide* property decides whether the compiler
will build the module at all: `+` and `*` on such a slot are emitted only when no text and no container
holding one can reach any container slot in the program (`ADR 0265`). A program that fails that proof keeps
the refusal it has always had — exit 1, `index cannot reach into xs's slots`, naming the missing half —
which means adding a string to a container can turn a working compiled arithmetic into a refusal. That is
deliberate and coarse (the reference *answers* `"a" + "b"` and `[1] * 2`; this backend cannot, so the door
must not open there), and the same gate serves the binding: `n = xs[0][0] * 2` / `print(n)` prints `14`
compiled too, through the `_n`/`_n_tag` pair the print dispatch already reads, and a program that fails the
proof keeps a refusal which now names the arithmetic itself — `the answer of arithmetic over a slot the run
time describes cannot reach a binding: …`, reported at the assignment's own line — instead of the `index
cannot reach into xs's slots` sentence blamed on the container-append two lines below. Binding the answer is
parity surface (`programs/probe_arith_result_bound_to_a_name.gy`, three legs, including its rebinding rows,
where `Gap R.142`'s wrong answer lived: a name the pair road had bound, rebound to a list, printed the heap
handle); the answer **handed to a function** is parity surface since
(`programs/probe_slot_read_handed_to_a_function.gy`, eight lines, three legs — the argument arrives as the
`(payload, tag)` pair and the answer's kind comes back in the word the callee stores beside its own `return`,
`Gap R.139`, ADR 0273); what stays filed beside them is the same name read back where the position asks for one
static number (`Gap R.143`, `programs/probe_pair_bound_name_as_a_number`), a pair-shaped value handed to a
position that keeps one word (`Gap R.146`, `programs/probe_pair_bound_name_takes_a_value` — the list element
`[n]` left that family when the container builders learned to ask the tag, ADR 0306, and is parity surface in
`pair_container_test.go`; the **dict entry and the set member** left it the same way, into the two builders
that already take `(payload, tag)` — `rt_dict_put_tagged` and `rt_set_add_tagged` — and are parity surface in
`pair_dict_set_test.go`, `dict_set_test.go` and
`programs/probe_pair_bound_dict_entry_and_set_member.gy`, ADR 0310; what is left inside that family is a
literal a builtin folds into a static array, the mutation roads and a pair handed across a call), and the tuple
unpacking that has not taken the pair (`Gap R.144`, `programs/probe_pair_from_a_tuple_unpack`) — each CPython's
and the interpreted leg's answer against the compiled leg's exit 1. The same shape one statement earlier — the
assignment that changes a variable's state from int to float — is parity surface since
(`programs/probe_int_state_becomes_float.gy`, twelve lines, three legs: `x /= 2` and `x = 2.5` over an int
variable box the double and bind the name to the `(payload, tag)` pair, `Gap P.1`, `Gap R.155`, ADR 0274);
before it, the compiled leg printed `3` for `7 /= 2` and printed the *neighbour* of a rebound variable as a
different number (`y = 12345` beside `x = 2.5` answered `1074003968`), both at **exit 0**, because the module
stored a double into the four-byte slot the first binding chose and an opaque pointer hides that from the
verifier. The state travelling further is parity surface since
(`pkg/lang/float_arg_test.go`, `integration/float_argument_test.go`, and the promoted
`programs/probe_float_numeric.gy` — the argument that can end on a double arrives as the pair, so
`twice(2.5)` prints `5.0` where the module wrote one `i32` into the parameter and printed `4`, and the answer of
a pair-returning callee handed through a `double`-returning caller is lifted at `@rt_lift_num` instead of
reading `2` for CPython's `5.0`: `Gap P.1`'s last line, `Gap R.154`, `Gap R.157`, `Gap R.158`, ADR 0276).
The same door reaches one frame further now: the scan brackets a `def`'s body while it walks, so a call site
knows *whose* parameter it is handing over, and a parameter forwarded into a callee position that needs the
pair is marked from that edge in either direction — `def outer(x): return twice(x)` prints `5.0` where the
module read the pair as one `i32` and printed `4` at exit 0 (`Gap R.161`, ADR 0277). A mark made only by an
edge records what it rests on and is closed when that support is closed (a callee the body rounds reject
closes its caller with it, and a forwarding cycle proves nothing), so no caller is left handing a two-word
argument to a body that cannot read one; and the answer direction — a function that returns a pair-returning
callee's result is pair-returning — is answered by the scan and its tag word declared at the callee's own
`define`, which is why `def f(v): return other(v)` compiles the same written above or below `def other(w):
return w * 2` (the read and the write of one global cannot share a one-shot guard).
Before that door the same family answered a truncated number at **exit 0** — `dbl(0.1)` was `0`, `bump(1.5)` was
`2`, `area(2.5, 2)` was `4`, `greet("a")` over a `times=1.5` default was `1` — and a float-state name handed to
a call was an exit-1 refusal, which is the outcome a widened gate must never produce for a program that already
answered: the body gate serves a *condition* over a pair (`if v > 10:`) and declines a *comparison as a value*
(`return v > 1.5`, `True`/`False` unchanged), and a function whose return word belongs to ADR 0274's `double` or
ADR 0174's string index keeps its parameter words too. What that row still leaves filed is the state travelling
further still: returned from a function (`Gap R.156`), stored as a container element (`Gap R.159`), ordered
against a float literal (`Gap R.160`), bound to a name inside a forwarding frame (`Gap R.164`), and formatted by
the reference's text-`%` (`Gap R.165`, `"%.2f" % 3.5` prints `0.0`) — each
pinned with its exit class in `integration/float_state_test.go` and `integration/float_argument_test.go`.
Three of that list are paid surface now: the forwarded parameter (`Gap R.161`, ADR 0277), the flooring
operators (`Gap R.162`, ADR 0278) and combining a floored answer with more arithmetic (`Gap R.166`, ADR 0279) —
`print(floorit(5.0))` is `2.0` and `print(modop(7.5, 2))` is `1.5` on both legs, `floorit(5)` still `2`, and
`def identity(v): return (v // 2) * 2 + (v % 2)` answers `7.5` for `identity(7.5)`; and so is the state bound to
a name inside a forwarding frame (`Gap R.164`, ADR 0280) — `def outer(x): y = twice(x); return y` answers
`5.0`, `return (v // 2) + other(v)` answers `4.5`, and a name the pair road bound is rooted for as long as the
frame holds it, which is what `--emit-llvm 'def f(v): return half(v) + twice(v)'` shows as an `rt_root_put`
beside the call. the pair door asks
`@rt_num_arith` with operator codes `4` and `5`, which
`--emit-llvm 'def f(v): return v // 2'` shows without a source to read. The divide-by-zero sentence is chosen
by the tag the argument arrived with, so one `def f(v): return v // 0` says "integer division or modulo by
zero" for `f(5)` and "float floor division by zero" for `f(5.0)` (`%` has its own pair: "integer modulo by
zero" and "float modulo"); all four exit non-zero, are catchable by `except ZeroDivisionError:`, and are the
wording the `--json` report's `message` member carries unchanged from the reference. There is no longer a
choice of engine to record: `--file`, `--eval` and the REPL all compile, and `--json`'s `backend` member (or
`--show-backend`) says `aot` so a transcript still carries which engine produced a number — worth keeping for
exactly the reason this paragraph used to exist, and as the field an agent pins its expectations against. The
negation of a text was worse and is now paid:
`print(-"hi")` used to answer `-281474976710658` interpreted and `0` compiled at exit 0, and both engines now
raise the reference's `TypeError: bad operand type for unary -: 'str'` at **exit 3**, catchable by
`except TypeError:` on each leg, for a text, `None`, a container literal, an instance (which names its own
class) and a slot the literal says holds no number — the operator asks the operand's kind before it writes an
instruction (`Gap R.89`, `Gap R.137`, ADR 0266; the three-engine program is
`programs/negation_names_the_kind.gy`). `abs` asked the same question of the same operand and was answered
with a number: `abs("hi")` printed `hi` and `abs(None)` printed `0` at exit 0, and `abs([1])`, `abs({"a": 1})`
and `abs({1})` each wrote `sub i32 0, <heap global>` for `llc` to reject — **exit 2** on a program the
reference merely stops on. It is paid by the same door rather than a new one: one predicate names the operand
for `-x` and `abs(x)`, and the raise leaves with CPython's own `TypeError: bad operand type for abs():
'<kind>'` at **exit 3** on both legs, catchable, an instance naming its class (`Gap R.140`, ADR 0271; the
three-engine program is `programs/abs_names_its_kind.gy`). What the same sweep found and did not fix is
filed with its own ID: a tuple's operand-type sentence says `'list'` on the interpreted leg (`Gap R.141`,
waiting on L11.3), a builtin or an imported module used as a *value* is exit 2 compiled and `NameError`
interpreted (`Gap R.150`), and a `lambda` in a numeric position reaches `sub i32 0, lambda_0` — also exit 2
(`Gap R.151`). Before ADR 0302 each of these had a second number, one per engine, and reproducing one meant
forcing the leg with `--interp`/`--aot`; there is one number now and `gustyc --file <path>` produces it. Where
a shape's history matters — an answer the record gave and the compiler refuses or gets wrong — the two
answers live side by side in the ledgers (`testdata/interpreter-golden-drift.json`,
`integration/testdata/cpython-debt.json`) rather than in a flag.
path is parsed as that flag's value rather than as the compiled leg.

## Emitted IR

Functions, control flow (`if`/`while`/`for`/`match`), the `pass` no-op
statement, integer arithmetic, comparisons, and `print` (via `printf`) are all
lowered to opaque-pointer IR.

## Lambda (compiled backend)

`lambda params: expr` is an anonymous single-expression function:
- inline call: `(lambda x: int: x + 1)(5)` -> 6
- bound form: `f = lambda x: int: x * 2` then `f(3)` -> 6
- codegen emits an anonymous FuncDef (`lambda_N`) at module level and a call to it. The generated name stays
  where it belongs: an arity diagnostic says `<lambda>`, the name the reference uses and the only one the
  programmer recognises — `too many arguments for lambda_0` blames a name nobody wrote (ADR 0302).
- a lambda is a *call site*, not a value: passing one to a higher-order function needs a function value, and
  that is the `L11.1`/function-as-value debt, refused by name.

## Dict methods (constant receivers)

`{1: 2, 3: 4}.keys()` and `.values()` on constant dict literals fold to
lists in the AOT codegen (`len`/`sum` work); `.items()` now ships in both paths on constant dict literals
(`len({1: 2, 3: 4}.items())` -> 2); a receiver whose contents the compiler cannot see is refused by naming
the missing runtime container door (`Gap I.2`) — the ADR 0301 method table answers `keys`/`values`/`items` on a
variable only once the container has a run-time shape to ask about.

## One backend, and what it does not lower

There used to be a section here called "Interpreter-only language surface", listing the constructs the LLVM
path could not build and telling the reader to use `--eval`, where the AST interpreter answered them. That
escape hatch is gone (ADR 0302), so the list became the language's real capability boundary and is stated as
one:

| Construct | State on the compiled backend | Owner |
|---|---|---|
| classes, inheritance, `super()`, methods, dunder methods | works | — |
| decorators (`@dec def f`) | works — `f = dec(f)` at def time, and the decorated body is the one that runs | ADR 0301's method/table work |
| closures over a captured scope, at module level | works | — |
| a **nested** `def` capturing the enclosing scope (`m = make_counter(10); m(2)`) | **refused**: the returned function is a value, and calling a value whose kind is only known at run time needs the tagged word | `L11.1`, function-as-value |
| higher-order calls (`apply(f, x)`, `sorted(xs, key=f)`, lambdas as arguments) | **refused**, by name | `L11.1` |
| gradual runtime type checking (`def adopt(a: Animal)` rejecting a `Rock`) | enforced — by the shared front end, at check time, on every path (`--eval`, `--file`, `--verify`, `--check`) | `L6.6` |
| iteration/comprehension over a *run-time* container or text | literal containers fold; a computed one is refused naming its missing door | `Gap I.2`, `L11.1` |
| `sorted`/`len`/indexing of a container built at run time | partially: the tagged read answers what it was taught, `sorted` over a non-literal is refused | `L11.1`, `Gap I.2` |

Refused means: exit class **1**, a sentence naming the missing door, what the reference does there, and a
roadmap row (ADR 0166's rule). It never means exit 2, and the count of refusals taken during a test run is
printed at the end of the suite so a green build cannot quietly be a build of refusals.

The historical note that section carried is still true and still belongs here: nested `def`s capturing the
enclosing scope, and the dynamic dispatch the record did by asking the value, were implemented there and
never lowered here — the retired engine's answers for those programs are in
`pkg/lang/testdata/interpreter-golden.json`, and where the compiler disagrees with one it is a ledger row, not
a passing test.

**Variance rules are static (L6.6).** The invariant/covariant/contravariant
rules run in the semantic checker (`--check`, `--verify`, the LSP), not in the
backends. Nominal class annotations are enforced there too (`a: Animal` rejects a `Rock`, accepts
`Dog`/`Puppy`), and the read-only protocols are checked structurally (`Sequence[T]` accepts any container, a
tuple annotation accepts the runtime list representation); the rest of the annotation surface stays
static-only.
`docs/language.md`), **modules/imports** (`import mod` loads `mod.gy` and
binds `mod` as a namespace with `mod.name` / `mod.fn(args)` access),
`try`/`except`, generators (`yield`), lists, `len`, **closures**
They are fully represented in the JSON AST dump (`--emit-ast`) with no schema change.

## CLI / REPL

`gustyc` (in `cmd/gustyc`) provides:

- `--eval <src>` / `--file <path>`: run the program. Both go through the compiled backend (ADR 0302),
  and stdout is **only** what the program printed: the echo of the last value is keyed on *where the
  source came from*, not on what the last statement looks like (ADR 0204), and it is written to **fd 2**
  (`gusty: result <kind> <repr>`) so the tool channel and the program's output never share a stream.
  - **A file** (`--file`) echoes nothing at all, so a program ending in `f(5)` prints exactly what it
    printed — byte-identical to `python prog.py`. Nothing is ever appended to piped stdout.
  - **A snippet** (`--eval`, and each REPL turn) keeps the prompt courtesy: a final bare expression
    echoes through the same `str` the program's own `print` uses, so `--eval "x = 1 + 2\nx"` echoes `3`
    and `--eval "s = 'cba'\ns"` echoes `cba` (not `'cba'`), while `--eval "print(7)"` prints just `7`.
  - **A call is not echoed unless it is a pure builtin** (`str`, `len`, `abs`, `min`, `round`, `sorted`,
    …, and only while the program has not shadowed the name). Rendering a final expression means lowering
    it, and lowering the user's own call a second time runs its effects twice — `show(3)` after
    `def show(v): print(v)` would print `3` twice. A quiet prompt is the honest failure, and the gap is
    `roadmap L13.1`; `TestREPLCallResultEchoIsFiledNotFixed` pins the silence so it cannot be mistaken
    for `None`.
  - **A void snippet echoes no visible value** (a program whose final expression is a `print` call has
    nothing to show), and `--json` says so as `"result": null, "type": "None"`.
  - **`--json` reports `result` either way.** For a file it is the evaluated value of the last
    statement — metadata about the evaluation, *not* program output; do not concatenate it onto
    what the program printed.
- `--verify <src>`: parse + analyze, exit 1 on diagnostics
- `--emit-llvm <src>` / `--emit-ast <src>`: machine-readable IR / AST JSON
- `--lang`: self-describing feature list for agents — one line per aspect of the surface, including
  `patterns:` (every `match` case form: literal, `_`, capture, or-pattern, guard, sequence, mapping,
  and the class pattern with its aliasing rule; ADR 0235)
- REPL (interactive): accumulates input line by line so multi-line suites (functions, classes, `if`/`for`/`while` bodies) can be entered; shows `> ` primary and `... ` continuation prompts on a terminal; recovers from parser/eval panics so a bug never kills the session.

Exit codes are the table above — `--repl` has no per-session status to report, and the rest of
the contract (0/1/2/3/4) holds wherever a program is run.

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

Parity between two gusty engines used to be the necessary contract, and it was never a sufficient one:
two backends that share a bug agree. `print(True)` printed `1` on both sides of a green build for a
hundred ADRs (ADR 0257 pays the print rule, ADR 0259 the container tag — `print([True, 1])` is
`[True, 1]` on both legs now), and so did `len("café") == 5`, `"abc"[1] == 98`, and a trap where Python
answers `3` for `xs[-1]` (L11.4 closed the last of those, ADR 0210). The CPython leg is what closes that
hole, and since ADR 0302 it is the *only* external witness a run has: **a program is conformant when what
the compiled backend prints is what CPython prints for the same source**, with the retired engine's
record (`pkg/lang/testdata/interpreter-golden.json`) as the second, weaker witness — an answer about the
language rather than a definition of it (roadmap Gap R.190 states what that trade costs).

### From the command line

```console
$ gustyc --oracle 'print(1 + 1)'
oracle: match
  aot         ok       matches CPython
      | 2
  python      ok       the oracle
      | 2
  rules: set-order

$ gustyc --oracle 'print([True, 1])'; echo $?
oracle: match
  aot         ok       matches CPython
      | [True, 1]
  python      ok       the oracle
      | [True, 1]
  rules: set-order
0
```

A slot that carries a bool tag is answered by the object, which is why the compiled leg and the oracle now
print the same line (ADR 0259). What the tag still cannot reach is a *boundary*: the same verdict passed
to a function is a fresh binding the caller's expression does not travel with, and that program is what
the debt class is for today.

```console
$ gustyc --oracle 'def show(f):
>     print(f)
>
> show(True)'; echo $?
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

The seed is not an optimisation and it is not optional at the call sites outside `lang.PythonRun`
either. A set holding a text is ordered by that text's hash, and CPython randomises string hashing per
process; measured, `python3 -c "s={1}; s.add('a'); print(s)"` printed `{'a', 1}` 3 times in 20 runs with
the seed unpinned and never with it pinned — while gusty prints insertion order, always. So a CLI-level
case that lets the seed vary is comparing the compiler against a different reference answer on each run,
and the case that fails is whichever set-shaped row the randomiser happens to disagree with that day.
Every reference-leg spawn in the tree therefore goes through one helper — `lang.PythonRun` in the
harness, `oracleCommand` in `integration`, `pythonTwin` in `pkg/lang` — and
`TestTheReferenceLegsSetOrderIsPinned` asks the reference the same question twenty times and fails if it
ever gets two answers.

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
matrix as `toolchain.min_python`, alongside `toolchain.python` = the banner of the record that
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
  (`lang.OracleTooOldHint`), because "line 1 SyntaxError" on a program the record accepts is
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

One field is quoted rather than computed, and quoting it has a rule attached. `python_error` is the
reference leg's own stderr, and CPython writes the script's absolute path into its warnings and
tracebacks — a path under a scratch directory the harness made for that one run. `lang.PythonRun`
scrubs the run directory out before it reaches a row, so a traceback reads `File "prog.py", line 5` and
a warning `prog.py:4: SyntaxWarning: …`: the file, the line and the error class are recorded, the
throwaway path is not. Without that the committed artifact differs on every regeneration by nothing but
a random number, and a ledger that always diffs is a ledger nobody reads — the parity failure that
matters arrives wearing the same diff as the noise (roadmap Gap R.126, ADR 0261;
`TestMatrixArtifactCarriesNoRunDirectory` fails whoever regenerates it into quoting a scratch
directory).

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
(`oracle debt is paid: the compiled backend now print CPython's answer — update the registry`). A
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

## The suite's own interface: the record and the ledgers (ADR 0302)

Deleting an engine deletes the tests that asked it questions, unless the answers were written down first.
They were, and the artifacts that hold them are part of the toolchain's interface — an agent can read them,
and a developer can regenerate them, but nobody can use them to make a red suite green.

| Artifact | What it holds | Who checks it |
|---|---|---|
| `pkg/lang/testdata/interpreter-golden.json` | 5623 sources with the answer the retired engine gave: value repr, type name, program stdout, trap class and message, whether the shared front end refused the source | every case that asks about a source; **a missing entry fails the case**, so deleting coverage is not possible by deleting a record |
| `pkg/lang/testdata/interpreter-golden-drift.json` | the sources where the compiled answer differs from the record (338 rows) | the package's `TestMain`, both ways: a new divergence fails, and a divergence that silently went away fails until its row is deleted |
| `integration/testdata/interpreter-golden-drift.json` | the same, for the programs the CLI suite asks about (21 rows) | `integration`'s `TestMain` |
| `integration/testdata/cpython-debt.json` | the sources where the compiled answer differs from **CPython** (7 rows), with the reference's answer, the compiled answer, a `why`, and the **roadmap row that owns the fix** | `TestMain`, both ways as above, plus: an unowned row fails, and a row whose case stopped running fails |

### Running the suite: budgets and shards (ADR 0312, ADR 0313)

Almost every case in this suite ends inside a subprocess, because that is what the two witness legs are:
the record leg compiles through `llc-20` and `cc` and runs the result in-process, the compiled leg runs the
module under `lli-20`, and the reference leg asks `python3`. Measured per case: `llc` 58 ms, `cc` 13 ms,
`lli` 57 ms, `llvm-as` 10 ms, the Go side of the case 1 ms. The toolchain is the suite, so the suite is
only as fast as the number of cores it is allowed to use.

| Command | What it does |
|---|---|
| `make test` | `go test ./...` — the plain serial run. **The only correct command for the artifact-writing modes** (`GUSTY_GOLDEN_UPDATE`, `GUSTY_GOLDEN_MISSING`): those write per-run ledgers, and N shards would each write their own subset over the file |
| `make testshards` | `go run ./tools/testshards -tags llvm20 ./pkg/... ./cmd/... ./tools/... ./integration/...` — the same cases in one process per core (what CI runs, split into two steps with a stated `-timeout 12m` each) |
| `make showshards` | prints the partition as JSON without running anything: which case is in which shard |
| `go run ./tools/testshards -tags llvm20 -json ./pkg/...` | machine-readable per-shard summary — `{pkg, index, of, tests, ok, duration, error, evidence}` per row, plus the run's `ok` and `elapsed`; `evidence` is the file that shard's golden evidence went to, so the merged verdict can be cross-read against it |
| `-shards N` / `-timeout D` | how many processes per package (default `GOMAXPROCS`), and each shard's `go test -timeout` — one shard's budget, stated rather than implicit |

A package that holds no test files is **reported and skipped** (`testshards: …/echoprobe has no tests to
shard`), because `./...` matches the tools and the command alongside the packages; a `go test -list` that
*fails* is a hard error at exit 2, because "zero cases, all green" for a package that does not compile is
the one thing this harness must never print.

Four claims the harness makes, each pinned by `tools/testshards/main_test.go`:

- **Coverage.** The partition is `sort` + round-robin over what `go test -list` reports — `-list` is the
  authority on which cases exist, so nothing can be dropped by a pattern. Every name lands in exactly one
  shard, and `-run` is anchored (`^(A|B)$`) because an unanchored `TestFoo` also matches `TestFooBar`,
  which covers one case twice and another not at all. Only `Test*` names are admitted: a `Benchmark` in a
  shard's `-run` runs nothing and looks green.
- **Overlap, measured.** The sum of shard durations must exceed twice the wall clock, or the run failed —
  that is the difference between a parallel harness and a serial one that prints nicely.
- **A red shard turns the run red**, and the other shards still run; output is prefixed `[lang shard 3/4]`
  so an interleaved CI log still attributes each line.
- **The drift ratchet is adjudicated once for the run, not once per shard** (ADR 0315). The ledger's two
  inputs — which sources diverged, which sources were asked about — are process-global state, so a shard
  that judges a ledger row at all judges it by a subset of the evidence. Each shard writes
  `{ledger, divergences, asked}` to a file (`GUSTY_GOLDEN_REPORT`), the runner merges them per ledger and
  calls the package's own rules — `lang.CheckDriftAgainst` — once. A green run prints the numbers so the
  merge is visible rather than assumed:

  ```
  testshards: /…/pkg/lang/testdata/interpreter-golden-drift.json adjudicated over 20 shard(s) —
    338 divergence(s) over 2765 source(s) asked, all on the ledger
  ```

  A shard that fails skips the adjudication: it already reddens the run, and its evidence may be truncated.
  With `GUSTY_GOLDEN_UPDATE` or `GUSTY_GOLDEN_MISSING` set, the runner does not take the judgement over at
  all, so the artifact-writing modes keep their single-process semantics exactly.

Why the cases are not simply `t.Parallel()`: the in-process runner dup2s fd 1 and fd 2 around the program
it loads (`captureFD`), so two programs in one process would print into each other's pipes; 46 sites in
four case files use `os.Chdir`/`t.Setenv`, which are process-wide; and Go runs a parallel test *beside*
the serial ones, not after them, so marking the offenders non-parallel does not protect their neighbours.
The process is already the unit of isolation the compiler has (its program-wide scan tables are plain
maps), so it is the unit of parallelism too.

Measured on this tree, both runs pinned to four CPUs so they describe the CI runner — the same command
line the workflow used to run, and the same one it runs now:

| run | wall | user | what it means |
|---|---|---|---|
| `go test -tags=llvm20 ./pkg/lang/` | **10 m 06 s** | 8m31s | the failure reproduced: `go test`'s default is **10 m 00 s** |
| `go run ./tools/testshards -tags llvm20 -shards 4 ./pkg/lang/` | **3 m 30 s** | 9m00s | the same 1248 cases, four processes |

The two `user` figures are the point: the work did not get cheaper (it got slightly more expensive, four
builds' worth of Go startup), only the wall clock did. Shards are balanced by **count**, not cost — no
cost is known before a case runs — and the four shards measured 2m13 / 2m15 / 2m48 / 3m28, so the run is
bounded by its heaviest shard; `-json` reports each duration, so a cost-ordered deal can be built from
what the harness already emits. Full-tree, all shards on 20 cores: `./pkg/... ./cmd/... ./tools/...` in
63 s and `./integration/...` in 60 s.

Every toolchain call carries a **budget**, and an expired budget is a class of its own (`ToolTimeoutError`,
exit **8**, `phase: "toolchain"` in `--json`) — a tool that was killed never said no, so it is not the
compiler's exit 2 and not the program's exit 1:

| Setting | Default | Applies to |
|---|---|---|
| `GUSTY_TOOL_TIMEOUT` | `5m` | `llc`, `cc`, `opt`, `llvm-as`, `llvm-dwarfdump` — the build-stage tools |
| `GUSTY_ORACLE_TIMEOUT` | `2m` | one CPython reference run |

A value that does not parse is reported on stderr and the pinned default is kept, because an operator who
set the variable and is still waiting has to learn it never took effect. A *missing* tool is never a
timeout (that would advise raising a budget instead of installing LLVM 20), and **no budget applies to a
user's program**: `--eval`, `--file` and the REPL execute in-process and `while True:` is a program, not a
bug. `kill` reaches the tool's whole process group, because `cc` is a driver and a stranded `as` holding
the output file is the same hang one layer down.

### The witness vocabulary, and the guard that keeps it honest (ADR 0302, ADR 0308)

There is one backend, so a claim is never "engine vs engine" — it is made against one of exactly two
**witness legs**, and every comment, failure message, tracker cell and help text says which:

| Phrase | The claim | Witness |
|---|---|---|
| **the record leg** | the compiled answer equals the retired engine's recorded answer for that source | `testdata/interpreter-golden.json` |
| **the reference leg** | the compiled answer equals CPython's | `gustyc --oracle`, the conformance matrix |
| **both legs** | both of the above | — |

`pkg/lang/witness_claim_test.go` makes that binding rather than stylistic. It scans the tests
(`pkg/lang`, `integration`), the CLI (`cmd/gustyc`) and the documents an agent reads instead of the source
(`roadmap.md`, `README.md`, `AGENTS.md`, `docs/operations.md`, `docs/language.md`,
`docs/shared-lowering-spec.md`, `docs/benchmark.md`, `docs/abi.md`, `docs/agentic/ast-ir-schema.md`).

A line fails when it matches an entry of `pkg/lang/testdata/witness-banned-phrases.txt` — the present-tense
claims that somebody ran the engine that was retired (engine-vs-engine phrases, "…the interpreter prints…",
and the deleted entry points `EvalExpr`, `EvalProgram`, `InterpreterRun`, `pkg/lang/jit.go`) — unless that
same line carries a marker from `testdata/witness-history-markers.txt` (`was`, `used to`, `before`, `retir`,
`measured 20`, …). A measurement taken while two engines ran stays in the corpus exactly as written: it is
evidence about those two engines, and the only record this project holds of the bugs two implementations
find by disagreeing. What may not survive is a **present-tense** claim that somebody ran the deleted engine.

Two more checks ride along, both aimed at the interface rather than the prose: a `gustyc` flag
description may mention the interpreter only to say it is retired (`--bench`, `--bench-suite`,
`--bench-gate` and `--oracle` all used to sell an interpreter leg), and `pkg/lang/jit.go` must stay
deleted. The two lists are data files, so a cycle tightens the rule by editing a ledger, and the guard
skips its own file by name — a mass restatement that rewrote the rules it enforces is one way this kind
of check dies, and the file it dies in is not allowed to be one of its own inputs.

Two facts in that machinery matter to an agent reading the JSON, because both are answers it would otherwise
have to guess at.

- **A snippet's announced value, and where its type comes from.** `--json --eval` reports `result` and `type`
  when the module can report a final expression, and `type` is asked of the value's **tag, at run time** — the
  same table a `TypeError` message reads — so `xs = [True, 1]` / `xs[0]` reports
  `{"result": "True", "type": "bool"}` and `xs[1]` reports `{"result": "1", "type": "int"}` (ADR 0303). A
  snippet whose value the module cannot render reports neither member: silence, never a guess, and never the
  word `object` standing in for a kind the compiler could not see.
- **The suite publishes how soft its landings were.** The run prints `compiled refusals this run: N`. An honest
  refusal is a filed gap, not a pass — a green suite whose `N` has grown has lost coverage. Read `N` as a
  coverage number, not a warning count.

Rules these files are built to obey, and the reason each exists:

- **A refusal can stand in for an answer only if it is honest.** `refusesHonestly` accepts an exit-1 message
  only when it names the missing door, says what the reference does there, and cites a roadmap row or ADR.
  The three-word refusals this suite used to accept (`unsupported call "f"`, `str on non-integer`,
  `unsupported list method index`, `list index out of range`) are not honest, and each was rewritten as part
  of this cycle.
- **Every refusal taken during a run is counted and printed** — `compiled refusals this run: 56 (filed gaps,
  not answers)`. That number rising while the suite stays green *is* the suite going soft, and this is the
  line that catches it.
- **A skipped case still carries the disagreement in its message**, and the ledger row holds both answers,
  so the file is a work list rather than a list of excuses.
- **A partial run cannot pay a debt.** `-run` subsets do not reach most rows, so "row not exercised" is only
  asserted on a full-package run (`isPartialRun`, keyed on the `test.run` flag).

Recording knobs, for the cycle that measures new surface — none of them is a way to pass:

| Environment | Effect |
|---|---|
| `GUSTY_GOLDEN_UPDATE=1` | rewrites a package's drift ledger from what the run measured; new rows still need their roadmap row, and the run is only trustworthy in full |
| `GUSTY_GOLDEN_MISSING=<path>` | writes the JSON list of sources the run asked about that the record does not hold — the input to the recorder, and the reason missing entries surface all at once instead of one per run |
| `GUSTY_DEBT_UPDATE=1` | rewrites `integration/testdata/cpython-debt.json` from measured reference-vs-compiled disagreements; a row with an empty `roadmap` field fails the next run |
| `GUSTY_GAP_LEDGER=<path>` | dumps every refusal the run took, with its sentence, for filing as roadmap rows |

The record was produced from the interpreter's own build (a `git worktree` at the pre-retirement commit, with
`EvalExpr`/`EvalProgram` instrumented), so its entries are what users saw rather than what the old engine's
internals could be made to say — which mattered enough to force three corrections: a compound-statement
snippet (`if x: ...`) has no value *to* echo, so the record holds the void the prompt displayed and not the
AST value the recorder was handed; a void has no visible repr, so its `repr` is empty and the CLI reports
`null`; and the type name is read from the recorded repr when the recorder's stored spelling was plain-wrong
(`print(10 ** 6 // 7)` is an `int`, not a `float`). Those corrections are data in the entries' `note` field,
not comments, so a future re-recording cannot silently undo them.

## JSON output for agents

`gustyc --json` emits machine-readable JSON on stdout:

- `--json --eval "x = 1 + 2\nx"` → `{"result": "3", "type": "int", "backend": "aot", "exit": 0}`
- `--json --eval "1 == 1"` → `{"result": "True", "type": "bool", "backend": "aot", "exit": 0}`,
  and plain `--eval '1 == 1'` echoes `True` (ADR 0257): the type is `bool` for an expression that
  answers a question — a bool literal, a comparison, membership or identity test, `not`, `all`/`any`,
  an `and`/`or` of two verdicts, a ternary with verdict arms, a call whose every `return` is one, or a
  name last bound to any of those. `and`/`or` of numbers stay `int`, because Python yields the operand.
- `--json --eval "xs = [True, 1]\nxs[0]"` → `{"result": "True", "type": "bool", …}` while the very next
  line, `xs[1]`, reports `"type": "int"`: a slot is asked what it holds, and the element tag answers
  (ADR 0259). An agent reading a container therefore gets the kind the object carries, not the number
  the verdict is stored under — and the number is still there for the questions CPython answers with it
  (`--json --eval "xs = [True, 1]\nxs[0] + 1"` is `{"result": "2", "type": "int", …}`).
- `--json --eval "max([True, 0])"` → `{"result": "True", "type": "bool", …}`. `min`/`max` **choose** a
  candidate, so the chosen candidate decides what the answer is and what it reports: the compiled fold and
  the verdict question pick the winner with one shared comparison (a strict one, so a tie keeps the first
  candidate — `max([True, 1])` is `True`, `max([1, True])` is `1`), and `print`, `str()`, `repr()`, an
  f-string, a container slot and this report all ask that one question (roadmap Gap R.117, ADR 0261). The
  number is still underneath: `--json --eval "max([True, 0]) + 1"` is `{"result": "2", "type": "int", …}`.
  What the report cannot say is a winner the compiler could not see — a candidate that is a name it has not
  folded prints and reports the number (roadmap Gap R.124), and so does a ternary whose test it cannot read
  (Gap R.125).
- `--json --eval "repr(\"hi\")"` → `{"result": "'hi'", "type": "str", "backend": "aot", "exit": 0}`,
  and `--json --eval "str([1, 2])"` → `{"result": "[1, 2]", "type": "str", …}`. `str()` and `repr()` are
  one pair over one renderer per backend — `print`, `str()` and a container element all ask the same
  table, so the two halves cannot disagree about a form and the machine path reports the same text the
  console does (roadmap L11.2, ADR 0258, closing Gap L.2). The pair's one disagreement is CPython's: a
  text writes its characters under `str` and its quoting under `repr`, quoted either way inside a
  container. Where an expression names no form at all the call is **refused** — exit 1, `"error"` in
  the JSON, the missing half named — and never answered with the decimal form of the handle, which is
  what `str([1, 2])` used to be: `0`, exit 0, on both backends.
- the same table answers a **function's** answer: `def g(): return str(42)` / `print(g())` is
  `{"result": "42", "type": "str", …}` on the compiled backend, where the compiled leg reported `0` of type
  `int` at exit 0 — the callee interned the rendering correctly and the caller printed its `@str_tab`
  index with `%d`, because the program-wide "which functions return text" predicate did not count a
  `str()`/`repr()` return. A caller that reaches `rt_str_ptr` prints text and one that reaches
  `printf`'s `%d` does not, so the rendering path is checkable from `--emit-llvm` without running the
  program (roadmap L11.2, ADR 0281, closing `Gap R.163`).
- **a ternary with text arms prints the text**: `print("y" if 1 else "n")` exits 0 with `y`
  (`Gap R.173` / ADR 0290, paying `Gap R.127`'s text half). The compiled leg printed the arm's **`@str_tab`
  position** — `0`, `1`, `2` — because four different "what kind is this expression?" predicates had no
  ternary arm, so nothing said the answer was text. Its `probe_ternary_text_arms.gy` row moved from recorded
  debt to parity. A **container** arm still exits non-zero in words (`Gap R.128`, owner L11.1).
- **a container has the methods the reference's containers have**: `--file` on
  `xs.extend([2,3])`, `xs.insert(0,9)`, `xs.index(2)`, `xs.remove(x)`, `xs.clear()`, `d.update(o)`,
  `d.pop(k)`, `d.setdefault(k, v)` and `d.clear()` exits 0 with the reference's answer (`Gap R.188` /
  `Gap R.63` / ADR 0301). All eight answered `no such list method` / `no such dict method` on **both**
  engines — a **missing** answer rather than a wrong one, which is the class a pin cannot catch: there was
  no output to compare, and the exit-1 sentence blamed the program for a feature the language lacked.
  `d.setdefault(k, []).append(v)` — how anybody groups rows — now works. `d.popitem()` **exists and
  refuses** naming the reason: it answers a pair, and there is no tuple value until `L11.3`; answering a
  list would print `[1, 2]` for what the reference renders `(1, 2)`. Raises carry the reference's own
  sentences (`ValueError: 5 is not in list`, `KeyError: 'z'`). The compiled leg still refuses these over
  a *variable* at exit 1 — owed to `L12.11`.
- **an in-place container mutation answers the void**: `--file` on `print(xs.append(2))` exits 0
  with `None` (`Gap R.187` / ADR 0300). Before this the retired interpreter answered the
  container — `[1, 2]`, `{1, 2}`, `set()` — at **exit 0**, `sum([1,2,3].append(4))` answered **10** where
  the reference raises `TypeError: 'NoneType' object is not iterable`, and the compiled leg emitted
  `printf(i8* @.fmt1, i32 )` — a call with a **missing operand**, which `llc` rejects, spending **exit 2**,
  the forbidden class, on a one-line program. The mutation was never broken; only the answer the statement
  throws away was, which is why it survived: a program writes `xs.append(2)`, not `print(xs.append(2))`.
  `pop`/`popitem` still answer WITH what they removed (`while xs: x = xs.pop()` depends on it), and a user
  method named `append` keeps its own answer — the table keys on the call's shape, not the spelling.
- **an f-string's format spec formats**: `--eval 'print(f"{3.5:.2f}")'` exits 0 with `3.50`, and
  `f"{7:05d}"` with `00007`, `f"{255:x}"` with `ff`, `f"{3.5:>6}"` with the padding (`Gap R.186` /
  ADR 0299). Before this the spec was cut off at parse time and never stored, so **both** engines printed
  the plain number at **exit 0** — eleven shapes, engines in perfect agreement, which is exactly why
  parity could not see a single one: parity compares the engines to each other and only the oracle leg
  compares either to CPython. A spec the language cannot honour now **refuses with a sentence naming it**
  (`f"{[1,2]:>8}"` raises `TypeError: unsupported format string passed to list.__format__`, the
  reference's own words) rather than answering the unformatted value; on the compiled leg a field it
  cannot read at compile time, and any field that is a container, exits 1 naming the spec — owed to
  `L12.8` with `L11.1`. `f"{[1,2]}"` previously emitted `printf(..., i32 @.lst1)` and died in `llc`,
  which is exit 2, the forbidden class; it refuses now.
- **a text iterates one character at a time** (the road the retired interpreter took, and the one the
  compiled backend owes): `--eval 'print([c for c in "abc"])'` is the question, and the record's answer is
  `['a', 'b', 'c']`, `print(max("abc"))`'s is `c` (`Gap R.185` / ADR 0298). Before this the interpreter
  itself had answered `[]` and `abc` — a WRONG answer at exit 0, no
  refusal, no trap: an empty list is indistinguishable from an empty iterable, so a program iterated it and
  never entered. `for c in "abc"` had always been correct beside it, which is the whole defect: three roads
  asked "iterate this text" and two of them read the container store, where a text keeps its characters
  elsewhere. The compiled backend still refuses these at exit 1 naming what it cannot lower — owed to `L11.1` and
  `Gap I.2`; the record keeps the engine's answer so the gap stays measured rather than argued.
- **a text answers truth like any other value and a string method prints its text**: `--aot --eval
  'print(not "x")'` exits 0 with `False` and `'print("ab".zfill(5))'` with `000ab` (`Gap R.183` /
  `Gap R.184` / ADR 0297). Before this the compiled leg said `True` for `not "x"` while `if "x":` beside it
  answered correctly (the `not` road compared the INTERN SLOT against zero), and nine text-returning methods
  printed the intern INDEX — `0` — because the print road listed three of the fourteen the fold implements,
  in a list that existed twice. `"-42".zfill(5)` gave `00-42` on both legs, so parity could not see it.
- **a dict view prints as a view**: `./build/pyre --aot --eval 'print({"a": 1}.keys())'` exits 0 with
  `dict_keys(['a'])`, matching the interpreter and the reference (`Gap R.182` / ADR 0296). Before this the
  compiled leg printed the heap HANDLE (`0`), and `print({1: 2}.values())` emitted
  `rt_print_list_mixed(i32 @.lst1, i32 0)` — a global address handed to a heap walker, **exit 2**. Both
  engines had also printed a bare `['a']`, so parity was satisfied and only the oracle leg saw the missing
  wrapper word. A view stays a list-shaped object, so `sum`/`max`/`min`/`for`/`in` keep working on it.
- **a set counts distinct members and a `dict.get` prints the kind its slot holds**: `--aot --eval
  'print(len({1, 2, 2, 3}))'` exits 0 with `3` and `print({1: "a"}.get(1))` with `a` (`Gap R.181` /
  `Gap R.180` / ADR 0295). Before this the static set global reserved a slot per SOURCE element — `len({1, 1, 1})`
  answered `3`, so a set that counted duplicates was a list wearing braces — and `get` returned the WORD its
  slot holds, printing an interned text's index (`0`), the void (`0`) and a verdict (`1`). the record
  printed `1` for `{1: True}.get(9, True)` too, so even the two-backend matrix agreed on that one; only
  CPython on the same file said `True`. `len({1, 2})` answers `2` either way, which is exactly why a corpus
  of correct-looking programs never finds this and a sweep that repeats an element does.
- **a slice of a container answers the list, never its handle**: `./build/pyre --aot --eval 'print([1, 2, 3][1:])'`
  exits 0 with `[2, 3]`, and `print(["a", "b"][1:])` exits 0 with `['b']` (`Gap R.179` / ADR 0294). Before
  this the same source handed llc `call i32 @rt_slice(i32 @.lst1, …)` — a global address where a heap handle
  belongs, **exit 2** — and the two shapes that did compile printed `1`: printf's `%d` on the handle, and, for
  texts, the interned INDEX, because `rt_slice` copied element payloads and never their tags. the record
  answered every one of them correctly throughout, so the parity matrix saw nothing and only the oracle leg
  could.
- **a power answers the kind the reference answers with**: `print(2 ** -1)` exits 0 with `0.5` and
  `print(4 ** 0.5)` with `2.0` on both engines (`Gap R.176` / ADR 0293). Before this, both backends answered
  `0` for a negative int exponent (under a comment claiming Python does that) and the compiled leg pushed
  every power through `fptosi` + `%d` — `print(2.0 ** 10)` said `1024` where the reference says `1024.0`,
  `print(4 ** 0.5)` said `1`. `**` was simply absent from the operator list that tells `print` what kind an
  expression answers with, and both legs agreed on the truncation, so only the oracle leg could see it.
  `0 ** -1` exits **3** with the reference's own `ZeroDivisionError` (catchable, literal or bound base);
  `(-8) ** (1/3)` and `2 ** 100` exit **1** naming what is missing — a complex value, an unbounded integer —
  rather than printing the `nan` / wrapped `0` the machine would hand back.
- **a container in a numeric operand never spends exit 2**: `./build/pyre --aot --eval 'print([0] * 3)'` and
  fifteen siblings (`[1, 2] + [3]`, `[1] - [2]`, `[1] / 2`, `{1: 2} * 2`, `[1] + {}`, `[] < {}`, …) exited
  **2** — `llc-20` refusing OUR module (`mul i32 @.lst1, 3`, `add i32 @.lst1, @.lst2`,
  `sitofp i32 @.lst1 to double`) — which ADR 0166 reserves for a bug of ours, so an agent scripting the
  toolchain could not tell "my program is wrong" from "the compiler is broken" (`Gap R.175` / ADR 0292).
  Each shape now leaves at **3** where CPython raises (the reference's own sentence, catchable by
  `except TypeError:`) or at **1** where CPython answers and no runtime sequence helper exists. The
  interpreter answers the answered half (`[0, 0, 0]`, `[1, 2, 3]`, `True`); the compiled half waits for
  L11.1's tagged value word. A container as a **call argument** is unaffected — `half([1.5])` = `0.75`.
- **a missing dict key answers `None`**: `print({"a": 1}.get("z"))` exits 0 with `None` on both legs
  (`Gap R.174` / ADR 0291). the record returned the bare word `0` — so `print(v == 0)` also answered
  `True` where the reference answers `False` — and the compiled leg refused the program with
  `get: key not found and no default` although its own fold already knew the key was absent. A container
  method over a **name** exits **1**, and the refusal now says "the program bound `d` to a container" rather
  than the old "string method keys on non-constant string" (Gap R.38).
- **a text predicate prints a verdict**: `print("abc".startswith("ab"))` exits 0 with `True` on both
  engines, not with the `1` the fold holds (`Gap R.172` / ADR 0289). The eight methods already answered
  correctly and still answer correctly as numbers (`"1".isdigit() + 1` is `2`) — only the print road had
  never been asked about a *method* call, because its callee is an attribute rather than a name. Thirteen
  tests had pinned `1`/`0` as the expected output and moved with the fix. A **parameter** as the receiver
  exits **1** on the compiled leg naming the method.
- **comparison chains answer like the reference**: `a < b < c` is one construct asking two questions with
  the middle operand read once, so `print(1 > 2 < 3)` exits 0 with `False` on both legs (`L12.1` /
  `Gap R.53` / ADR 0288). Four of the six probe lines printed the opposite verdict at exit 0 before this,
  both legs agreeing with each other and not with Python — the class a harness reads as success, since
  there is no refusal, no crash and no odd digit to notice. A chain whose middle operand is a container
  exits **1** on the compiled leg naming the missing representation.
- **a builtin called with no argument never crashes the compiler**: `int()`, `float()`, `bool()` and
  `str()` answer the reference's `0`/`0.0`/`False`/empty text on both legs, and `ord()`, `chr()`, `abs()`
  and `repr()` raise the reference's own `TypeError` sentence at exit 3 (`Gap R.131` / ADR 0287). Before
  this all eight reached `Args[0]` first and died with a Go stack trace at **exit 2** — the code the contract
  reserves for a compiler bug — so a harness could not tell its own bad program from a broken toolchain. An
  integration table walks 21 spellings asserting exit 2 is unreachable; `--json` is unchanged.
- **a rendering the body bound to a name is not counted** on the compiled path: `def f(v): s = str(v);
  return s` printed `0` for `f(3)` at exit 0 (and `2` for `"x" + str(v)` where the reference prints `x3`)
  while the reference and the record printed the text (`Gap R.170` / ADR 0286). Exit 0 covers that
  shape now; where the renderer cannot name its operand the compiled leg exits **1** naming what is missing,
  and the `str()` digits road is gated on what the operand can be *seen* to be — it writes the digits of the
  word it is handed, which is a number for an int and a fabrication for a container handle.
- **a number bound from a parameter is not truncated** on the compiled path: `def f(x): y = x + 1; return y`
  printed `1` for `f(0.1)` at exit 0 (and `0`, `-1`, `5` for its siblings) while the reference and the
  interpreter answered `1.1`/`0.2`/`-0.9`/`5.0` — four believable digits from the AOT engine (`Gap R.169` /
  ADR 0285). Exit 0 now means the reference's answer through a bound name too; the three shapes that still
  need a tagged return word exit **1** and name the value they are stuck on and where it came from
  (`a parameter the call site handed a double`), so a harness can tell "rewrite this" from "the compiler
  cannot carry it yet" without reading IR.
- a **wrong-arity call stops the program**, and never answers a number: the retired engine printed
  `2` at exit 0 for `g = lambda x: x * 2` / `print(g(1, 2))` and `0` for `print(g())` — values computed from
  arguments that were never passed — while the reference raises and `--aot` already refused. Exit 0 now means
  the same thing on both paths (`Gap R.168` / ADR 0284), and the message carries the callee, the accepted
  count and the received count so a harness can compute the fix instead of re-probing
  (`too many arguments for <lambda>: it accepts 1 argument, got 2`).
- a **declared** name read as a value is refused at **exit 1**, never at exit 2: `print(f)`, `f + 1`,
  `xs = [f]` and `print(lambda x: x)` all reached `llc-20` and came back "use of undefined value
  `%_f`"/`printf(…, i32 lambda_0)` — the exit-code contract's compiler-bug code spent on a program the
  reference answers (ADR 0283, closing the exit-2 half of `Gap R.150` / `Gap R.151`). A function in a
  *numeric* position is a trap at **exit 3** carrying CPython's sentence verbatim
  (`TypeError: bad operand type for abs(): 'function'`), so a harness can match on message text and
  `except TypeError:` runs the arm the reference runs; a wrong-arity call is refused by either stage and
  must never produce a number (`Gap R.168` guards that).
- a feature this backend does not build is refused at **exit 1 with the reference's answer quoted**,
  which is what makes "not implemented" distinguishable from "wrong" without reading the IR:
  `--aot --file` on `print("%.2f" % 3.5)` reports
  `codegen: a string is a text, which has no double to widen from: printf-style formatting — the
  reference's `"%.2f" % 3.5` answers `3.50`, … (roadmap L11.2, Gap R.165)` where it once exited **0**
  having printed `0.0`. The three states an agent can branch on are therefore: exit 0 prints what
  CPython prints, exit 1 names a missing feature (and quotes what it should have been), exit 3 is a
  program that raised (roadmap L11.8, ADR 0166's exit-code contract, `Gap R.165` / ADR 0282).
- every execution result carries `"backend"`, and since ADR 0302 there is exactly one value for it:
  `"aot"`. It stays in the payload because it is a fact about the run rather than something to infer from
  a flag list — a consumer that pinned `"backend": "interpreter"` gets a mismatch it can detect instead of
  a silently reinterpreted result (roadmap Gap M.2). A run reports
  `{"output": "42\n", "backend": "aot", "exit": 0}`. The retired flag family behaves accordingly:
  `--aot`/`--jit` are accepted and ignored, and `--interp` is a **usage error (exit 4)** naming the
  retirement, because a script that believes it picked an engine must learn at the flag.
  For a program that trapped, `"exit"` is the very number the process exits with and the
  target's own diagnostics come along: `{"output": "", "backend": "aot", "exit": 3,
  "stderr": "Traceback (most recent call last):\n…IndexError: index out of range\n"}`. That
  status is the generated `main`'s return value, carried as `lang.JITResult.Code`; until it
  existed `--aot` answered `"exit": 0` for a program that had just died, on the path an agent
  scripts a compiled run through (roadmap Gap R.17, ADR 0211).
- `--json --aot <file>` when the program never ran → `{"error": "<message>", "backend": "aot",
  "exit": N}`: `N` is 1 for a front-end or codegen refusal, and **2** when `llc` rejected the
  module we emitted (the compiler-bug class, `*lang.ToolchainRejectionError`). A toolchain that
  is not installed says `could not be run` and stays 1 — it is not an LLVM rejection.
- `--json --verify <src>` → `{"ok": true, "exit": 0}` or `{"diagnostics": [...], "exit": 1}`
- parse errors → `{"ok": false, "phase": "parse", "error": "1:7: unexpected token",
  "errors": [{"line": 1, "col": 7, "msg": "unexpected token"}], "exit": 1}` — the spans
  are in the payload, so no agent has to parse `gustyc: parse error at 1:7: …` off stderr
- runtime errors → `{"error": "...", "traceback": "...", "exit": 3}` (the interpreted path
  renders the traceback into `traceback`; the compiled path forwards the program's fd 2 in
  `stderr`, which is where an uncaught-exception report belongs). A failure raised by the
  language itself also carries `"exception": "TypeError"` and `"exception_message": "…"`,
  decoded from `*lang.EvalError` rather than scraped out of the report — the class the program
  would have matched with `except TypeError:`, as data, so a caller branches on a name instead of
  on prose. Every built-in trap has a class now (roadmap Gap R.25, ADR 0214); where the field is
  absent the error came from a front-end gate, not from a running program
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
| `--bench-suite` | measure the built-in corpus on the compiled backend |
| `--bench-dir <dir>` | measure every `*.gy` in a directory (the parity programs become benchmark cases) |
| `--bench-baseline <path>` | gate the run against a saved baseline; exit 5 on a regression |
| `--bench-baseline-update <path>` | write the measured suite as a baseline artifact |
| `--bench-gate aot` | which measurement the gate watches. `aot` is the only gate: the others compared the run to the engine ADR 0302 retired, and any other name is a usage error (exit 4) rather than a gate that silently watches nothing |
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
| `strings inside runtime containers are not supported by the AOT backend yet` | a `list[str]` / `dict[str, int]` element would have to store an `i8*` in an `i32` heap slot (roadmap Gap I.2) | run the program on the record (`--eval`, `--file`), or keep container elements numeric |
| `unsupported call "` | a call the AOT backend cannot lower, e.g. calling through a `Callable` parameter (`def apply(f, x): return f(x)`) | interpreter path, or dispatch on a class with methods |
| `concatenating a runtime string is not supported in the AOT backend yet` | building a new string (`s + "!"` where a side is not a compile-time constant) needs a buffer allocation the compiled runtime does not have; passing the string itself is fine (ADR 0174) | run it on the record, or concatenate the constant parts and pass the result |
| `operator "<op>" on a string is not supported in the AOT backend` | arithmetic or ordering on a compiled string would compute with its string-table index, where the record raises `TypeError` (ADR 0174) | check the value before the operation, or run it on the record |
| `string method <name> on non-constant string` | a method that would build a new string (`upper`, `strip`, …) needs an allocator; `len`, `==` and container use of a string parameter are supported | run it on the record, or compare/measure instead |
| `cannot hold a float yet` | a float in a container slot: an element is one `i32` word and a compiled float is a `double` (roadmap L11.6, ADR 0226) | interpreter, or keep the float in a variable |
| `cannot hold another container yet` | a container inside a container: an element that is a handle is not marked by the collector, so its tag would be a guess (roadmap L11.1 (5), ADR 0188) | interpreter, or build the inner container separately and index it |
| `cannot prove one kind for` | an element the compiler cannot label — typically a call that returns text on one path and a number on another. `print` asks at print time; a slot is labelled once, and the wrong label prints `(null)` (ADR 0232) | interpreter, or give the function one return kind (the tagged value word, L11.1 (5), retires this) |
| `needs a needle whose kind the compiler can prove` | `x in c` where `c`'s slots describe themselves and `x`'s kind is not provable (ADR 0232) | interpreter, or compare a literal/tagged variable |
| `has no number the compiled backend can lift` | a numeric use of a slot whose kind only the object knows, where the operator cannot settle the result's kind — `xs[0] + 1`, `xs[0] ** 2`, `xs[0] / ys[0]` (roadmap L11.1, Gap R.82, Gap R.101). `/` alone answers, because true division is a float whatever arrives (ADR 0253) | bind the slot to a variable first and print/compare it, or run the record; `xs[0] / k` already answers |
| `is answered with a double by the tagged numeric door, and this context stores an i32 word` | the answer is a `double` and the sink is an `i32` store: a call argument, `str()`'s argument, a container slot written by key (roadmap Gap R.98, ADR 0253) | bind it to a variable that starts life a float (`half = xs[0] / 2`), then pass/print the variable, or run the record |
| `returns "x", which its own body binds to a float` | a function whose `return` is the bare name of a parameter its body rebound to a float, in a signature the `double` convention cannot carry — a parameter that is a container handle or interned text, which the body also reads (roadmap L11.6, Gap R.3c, ADR 0254). Emitted anyway it is `sitofp i32 @.lst1 to double`, which `llc` rejects | `return x + 0.0`, or bind the answer to a new name (`y = x + 0.0; return y`); the record answers all of these |
| `returns a ternary arm of "x", which its own body binds to a float` | `return x if cond else 0.0` of such a parameter: the answer's word is the arm's and there is no number-typed `select` to choose two doubles with (roadmap Gap R.102, ADR 0254) | take the branch with `if`/`else` and `return` the number on each arm, or add it to `0.0` on the arm |
| `winner's own kind needs the tagged value word` | `min` / `max` over runtime candidates whose static kinds mix `int` and `double`: the comparison is decidable, but the winner's kind is not, and forcing the double domain answers `min(2.5, 1)` as `1.0` (roadmap Gap R.109, ADR 0256) | keep candidates literal, use the record, or wait for L11.1's tagged value word |
| `takes values side by side or one container, not a container among values` | a `min` / `max` argument is a container literal beside another candidate; comparing containers by their handle words would answer with heap addresses (roadmap Gap R.107, ADR 0256) | flatten the candidates, or use the record's element-wise ordering |
| `chooses between two values whose kinds this pass cannot state in one word` | `a and b` / `a or b` hand back the **operand** the test chose (ADR 0269), and the position that holds the answer keeps one `i32` word while the two operands do not agree on what lives in it — a text among numbers, a float beside an int, a container among either | `print` it (the print door selects the payload *and* its tag and asks the module's tag-reading printer), take the branch with `if`/`else` and bind the operand on each arm, or run the record; the row the missing tag belongs to is L11.1 with Gap R.146 beside it |

Every container that crosses a function boundary is passed as a runtime heap
handle (see `docs/language.md` § Containers across function boundaries); the
parameter kinds are inferred from annotations, defaults and call sites, so no
extra syntax is required at the call site.

## Optimization: constant folding

The codegen folds integer-literal binary expressions at compile time:

    x = 1 + 2        # emits store i32 3 (no add instruction)

Folded ops: `+ - * / // %`, boolean `and`/`or` (folded to the **operand** the test chooses, never to a
verdict — ADR 0269), and comparisons `== < <= > >=`.
Division/modulo by a literal zero is left to runtime. Verify with
`gustyc --emit-llvm`.

## Boolean `and` / `or` and floor division (AOT codegen)

`and` / `or` are the two operators that hand back an **operand**, and the lowering is a choice between two
values, not a verdict (`Gap R.147`, ADR 0269) — made in a branch, so the operand the test rejected never
runs (`Gap R.149`, ADR 0275). Three roads, asked in `pkg/lang/logic_value.go`:

- a test the source wrote picks its operand at compile time — `print(True or 1)` is `True` and
  `print(1 or True)` is `1`, and the operand the test rejects is not in the module at all;
- two operands that share a word are merged from the two arms — the left operand is evaluated once in the
  block the expression starts in, `br i1` decides whether the right operand's block is entered at all, and
  the arms meet in `phi i32` (number door) or `phi double` (double door). The instruction that used to sit
  here was `select i1 %c, i32 …` (ADR 0262's), and a `select` has no way to not run the operand it is not
  choosing: `x and boom()` called `boom`, `x and (1 // 0)` raised, and `boom() and 2` called `boom` twice,
  because the truth and the value of the tested operand were each lowered separately;
- the print door merges the payload **and** the tag over one test (`phi i32` beside `phi i32`) and hands the
  pair to `rt_print_mixed_value`, which is why `print(x or "d")` prints `d` and not the `@str_tab` index
  underneath it, and `print(xs or "empty")` prints `[1, 2]`. The truth of that arm is the object's fact, so
  the branch asks `@rt_pair_truth(payload, tag)` — the same tag vocabulary (`value.go`'s `ValueTag`) the
  printer and the comparison read: a number in either family is true when it is not zero (a float slot is
  unboxed, not read as a handle), an empty text or an empty container is false, `None` is false, an object
  is true.

The two forwarding blocks the merge needs (`logic.lhs` → `logic.merge`, `logic.rhsfwd` → `logic.merge`) are
what let the `phi` name a real predecessor without the generator tracking which block it is emitting into;
`simplifycfg` folds them. Each road builds into a scratch builder and commits only when both arms answered,
so a door that changed its mind halfway cannot leave instructions after a terminator (`ADR 0166`).

A condition asks only for a verdict, and `truth(a and b)` is `truth(a) and truth(b)`, so `if`/`elif`/`while`
heads and ternary tests compose two predicates — by branching on the first, so `if x and boom():` with x
false never evaluates `boom()` — and keep working on operands that share no word. A value position whose
operands the pass cannot state is refused by naming both operands and the missing tag (the table above),
never answered with the `0`/`1` that stood here while the whole expression was an `and i1` plus a `zext`.

`//` floor division lowers to `sdiv`, also mirroring
the record. Literal operands are constant-folded.

## Builtins: sum / min / max / abs (AOT codegen)

`sum([1,2,3])`, `min([3,1,2])`, `max([3,1,2])`, and `abs(-5)` are lowered in
the LLVM AOT codegen path (previously interpreter-only). `sum`/`min`/`max`
fold over an **inline list literal** (unrolled `add` / `icmp`+`select` chains
over the list's global struct); `abs` accepts any integer expression and is
constant-folded for literal arguments.

`min(a, b, ...)` and `max(a, b, ...)` are one call surface on both legs (ADR 0256). The candidate that
wins keeps its own kind: `min(1.0, 2)` prints `1.0`, while `min(2.5, 1)` prints `1`. Text candidates go
through `rt_str_order`, and a source-visible text/`None`/container collision emits CPython's catchable
`TypeError`, naming `<` for `min` and `>` for `max`. A runtime int/double mix or a runtime container is a
named refusal pending L11.1's tagged value word; the record and `--eval` answer them today.

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
indexes dicts/sets at runtime.

**A repeated key is one entry on both paths** (ADR 0260, closing roadmap Gaps R.118 and R.120). The
compiled fold has always deduplicated the keys it unrolls, and the record now puts each entry
through the dict's own key lookup (`Evaluator.dictPut`) instead of appending it, so
`print({"a": 1, "a": 2})` is `{'a': 2}`, `{1: 2 for x in [1, 2]}` prints with a `len` of `1`, and the
`1`/`True`/`1.0` spellings collapse to one entry because key equality asks the numbers (ADR 0259).
Nothing in the JSON schemas changed — no new kind appeared — but this is where an agent can see it: the
compiled fold's key dedup and the record's `dictPut` are the same rule, and
`integration/programs/dict_key_rule.gy` is the parity row that keeps them that way. What the compiled
path still declines in words (exit 1, never a wrong answer) is a dict comprehension whose key it must
spell itself as text — `comprehension key must be constant`, roadmap Gap R.123 — and a display
unpacking (`{**d, "a": 2}`), which the parser rejects (Gap R.121, with Gap R.58).

A container variable is a heap object, and since ADR 0232 its slots are
`(payload, tag)` pairs: `d = {"a": 1, "b": "x", "c": None}` and `s = {1, "a",
None}` build, print, index, iterate, grow (`d[k] = v`, `s.add`) and compare
compiled, identically to the record and to CPython. Two runtime entry
points carry the whole surface for agents reading emitted IR: the tagged
writers (`rt_dict_put_tagged`, `rt_set_add_tagged`, `rt_tag_elem`) and the
tagged readers (`rt_dict_find`, `rt_dict_get_tagged`, `rt_dict_value_tag`,
`rt_dict_has_tagged`, `rt_set_contains_tagged`, `rt_contains_tagged`,
`rt_payload_eq`). A source-level `==`/`!=` between two tagged values — a slot
read, or a variable bound from one — is answered by `rt_payload_eq` too, the
same equality `rt_slot_eq` uses to walk two containers, so a slot cannot answer
`print` one way and `==` another (ADR 0247; the word-for-word `rt_mixed_eq` it
used to use is gone from the module, and a test fails if it returns). An
ordering of two texts — `<`, `<=`, `>`, `>=` between two values the compiler can
see are text, in either the value or the condition position — goes to
`rt_str_order`, which compares the bytes behind the interned indices and returns
-1/0/1; equality of texts stays an index comparison, because interning is
content-addressed (ADR 0248, closing Gap R.84 — an index records the order a text
was mentioned, and reading it as an ordering made `"b" > "a"` false). A numeric use of a slot read through
an index the program computes — `xs[i] + 1`, `xs[i] / 2`, `-xs[i]`, `1 > xs[i]` — comes out as its
(payload, tag) pair (`rt_get_elem` + `rt_tag_of`) and a tag dispatch: `rt_float_of` for a float slot,
`sitofp` for an int or bool, and one raise per non-numeric kind carrying CPython's own sentence for that
operator (ADR 0249, closing Gap R.88; the arm for the last kind is the unconditional `else`, so the merge
`phi` never has a predecessor that stores nothing, and an instruction with an empty operand is refused at
the front end instead of reaching `llc`). An ordering of slots asks the tag which pair it was (`rt_str_order` for two
texts, an `fcmp` on the unboxed numbers, `raiseTo` for the pair CPython refuses, its two type names
printed in source order because CPython always names the left operand first): `xs = [1, "a"]` /
`print(1 if xs[1] > "a" else 0)` prints `0` and `xs[i] > "z"` raises, on the compiled path as on the
interpreter (ADR 0250, closing Gap R.82). Only the arms that can run are emitted and the merge `phi`
names exactly those blocks — an arm no branch enters is the module `llc` rejects, which is exit 2,
ADR 0166's own bug class — and where the tags would have to be guessed the compiler refuses in words
(exit 1) rather than printing a verdict for a program the oracle kills. A container whose slots describe themselves is marked with
`@rt_mark_estr(h, 8)`, which is what routes `rt_dict_print`/`rt_set_print` to
the per-slot printers; a single-kind container does not set the bit and keeps
the static printers, so programs that worked before emit what they always
emitted. Lookups compare tags whenever the needle's kind is provable — that is
what makes `{1: "one"}` raise `KeyError` for `d["a"]` instead of answering
`one`, an interned string and an integer of the same number being the same bits
(see `docs/language.md` § Containers hold any value, and ADR 0189 for why the
tags can be trusted on read).

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

## Boxed heap handles and the immediate-integer rule

Boxed heap handles are allocated from a high base (`1 << 20`) so they never
collide with raw small integer literals stored in lists/dicts/vars. This keeps
`Repr` from misinterpreting a raw int as an object handle.

## Collector: precise roots and safe points (L7.2, ADR 0181)

The compiled backend trace an *enumerated* root set and never guess at machine words.

the record's roots are: the current environment, **every active call frame's
locals** (pushed on call, popped on return, so a dead frame retains nothing), the
root groups that constructs declare (a `for` loop's iterable, a loop's last value),
and the permanent roots (`None`, the generator accumulator, the `super()` receiver,
class objects). Two rules make collection sound in a tree-walking interpreter:

- **Watermark.** Everything allocated after the last safe point is unconditionally
  live, because the record may be holding it in a register.
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
corpus is run that way in `integration/gc_stress_test.go`, so a root the record
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

- **The tags** are `pkg/lang/value.go`'s `ValueTag` list. the record's heap objects
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

`print(True)` prints `True`, and `--json` reports `"type": "bool"` for it — the verdict is named by
the expression that produced it, which the compiled backend ask of one shared AST predicate rather than of
the storage that holds it (ADR 0257). Where the expression is gone — a slot in a container — the slot
carries the answer itself: an element whose value is a verdict is tagged `bool`, so `print([True, 1])`
is `[True, 1]`, `print({"k": True})` is `{'k': True}`, `print({True, 1})` is `{True}` and the ordering
trap names `'bool'`, on the compiled backend (ADR 0259, closing Gap R.112). What neither rule reaches is a
value crossing a binding the caller's expression does not travel with: **a bool passed to a function
prints `1`** (Gap R.111), because the parameter is a fresh slot and the tag would have to ride with the
argument.

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
on every path is accepted (definitely assigned); a name that only some paths bind is visible but
flagged at the use with `possibly unbound: "x" is not definitely assigned on all paths` — a warning,
because the name does exist on the other paths and only this one may not have assigned it (ADR 0217).
The warning is a hint, not the enforcement: the same read at runtime raises `UnboundLocalError` in a
function and `NameError` at module level, on the compiled backend, and exits 3 (ADR 0228). Reading a name the
body binds only *below* the read is in the same class — `possibly unbound: "v" is read before any path
assigns it` — and is likewise a runtime trap, not a compile error, because whether it raises depends on
the call (ADR 0228 closed Gaps R.36 and R.39 here; the old behaviour printed the frame's stale contents,
or refused the program).
A name no path binds at all is an `undefined name` error. A function-name reference (bare `fn`) is
assignable to any Callable bound under gradual typing.

### The compiled string table is bounded (ADR 0229)

A compiled string value is an index into `@str_tab`; literals are interned at compile time and a
program that builds strings while running (subscripting a string, `ord`, `upper`/`lower`) interns them
on first use. The table holds 4096 entries. Exceeding that is a program condition, not a toolchain one:
the runtime returns a "no value" sentinel and the generated code raises

```
RuntimeError: the program created too many distinct string values
```

through the ordinary unwind path, so `try: … except RuntimeError:` catches it and the exit code is the
usual 3 (ADR 0212, ADR 0214). The previous behaviour was worse than a trap: on overflow the runtime
reused the table's last entry, so a string printed as a *different* string and nothing said so.
Machine consumers get the same shape as any other trap — `--json` reports the class, the message and
`"exit": 3` — and no flag tunes the capacity; a program that needs more is outgrown by the table, not
configured into it.

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

## Property / fuzz testing (the compiled backend)

gusty validates the record and the LLVM AOT backend against **generated**
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
  parse-cleanliness, corpus validity (no undefined names / runtime
  errors on the record leg), and determinism (run-twice byte-identical stdout).
- `integration/proptest_test.go` — the generated-corpus harness, rebuilt for
  one backend (ADR 0302): each generated source goes through
  `Compile` → `llc` → `cc` → run, and the properties it can still assert are
  that the compiler does not panic, that the same source builds to the same
  answer every time, and that an accepted program terminates. There is no
  second engine to diff stdout against, and a generated program has no
  recorded answer — inventing one from the compiler's own output would be a
  self-comparison, so the harness does not do it.
- `FuzzPropCompiled` — a Go-native fuzz target seeded from the
  deterministic corpus; asserts the compiler never panics on arbitrary
  input.

Run them with the normal pipeline:

```sh
go test ./pkg/lang/ ./integration/
# long fuzz run (optional):
go test ./pkg/lang/ -fuzz=FuzzPropInterpreter -fuzztime 30s
```
