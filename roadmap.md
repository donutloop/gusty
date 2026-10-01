# Pyre (gusty repo) — Roadmap tracker

This file is the **tracker**. It is the single source of truth for *what exists*, *what is
next*, and *what is gap-shaped*: every item, gap, status, ADR and piece of evidence is a
row, so state is scannable at a glance and editable one cell at a time.

The **narrative** half — how a gap was found, what the wrong answer looked like, which
measurement settled it, what was rejected — lives in
[`docs/roadmap-details.md`](docs/roadmap-details.md). Each row's `Record` column links
straight to its section. Nothing is deleted from the record; statuses simply stop living
in it.

> Read `AGENTS.md` first: it fixes the loop, the two execution paths (interpreter + LLVM
> AOT), and the machine-consumption contract. This file is what the loop reads and writes
> each cycle.

## How to use this file

- **Pick work** from the [Open queue](#open-queue--the-working-list), top row first.
- **Finish work** by changing exactly one cell — the row's `Status` — plus its `Evidence`,
  in the same commit as the code, and appending the story to `docs/roadmap-details.md`.
- **Add work** by appending a row with a fresh, never-reused ID. Never renumber, never
  reuse: comments in `pkg/lang` and the ADRs cite these IDs (`Gap R.33`, `L11.1`, `L11.6`).
- **Do not** add status-bearing free text. A fact that is not a cell does not get
  maintained; a fact that belongs in prose goes to the record with its ID.

### Status vocabulary (the only markers this file uses)

| Marker | Meaning | Gate |
|---|---|---|
| ✅ `DONE` | shipped and verified on every path the item requires | nothing owed; reopening needs a new probe |
| 🟢 `IN PROGRESS` | the active item; steps landed | the row names the next step |
| 🟨 `PARTIAL` | a named subset ships; the remainder is written in the cell | the remainder keeps its own ID where it has one |
| ⏳ `PLANNED` / `OPEN` | not started (planned) or reproduced and unfixed (open) | sits in the queue |
| 🔴 `BLOCKED` | cannot move until the named dependency lands | the cell names dependency + owner |
| 🚫 `NOT A DEFECT` | measured, decided, nothing owed | the cell cites the ADR |

### Column contract

| Column | Meaning |
|---|---|
| `Path` | `both` = interpreter + LLVM AOT · `interp` / `aot` = one backend only · `checker`, `cli`, `tooling`, `docs` |
| `ADR` | decision record in `docs/adr/` (one per feature; `—` when the decision was obvious) |
| `Evidence` | the tests / programs that make the status a measured claim rather than a claim |
| `Record` | `docs/roadmap-details.md#anchor` — the verbatim history of the row |

## Snapshot (measured, not remembered)

| Measure | Value | Measured by |
|---|---|---|
| Language / CLI version | `0.10.0` | `pkg/lang/compile.go`, `gustyc --version` |
| Pinned LLVM toolchain | LLVM 20 (`llc-20`, `opt-20`, `llvm-dwarfdump-20`) | `makefile`, `docs/operations.md` |
| ADRs | 224 records, highest `0232` | `docs/adr/` |
| Conformance programs | 110 in `integration/programs/*.gy`, 21 of them probes | that directory |
| Matrix rows | 101 → 78 parity-asserted + 23 recorded divergences | `integration/conformance-matrix.json` |
| Parity failures / oracle drift | 0 / 0 | same artifact |
| Oracle verdicts | 60 `match` · 25 `debt` · 16 `not_applicable` | same artifact |
| Pinned oracle | CPython 3.12.3 (host LLVM Ubuntu LLVM 20.1.2) | artifact `toolchain`, `lang.OracleMinPython` |
| Test suite | `go test -tags=llvm20 ./...` green; every emitted module passes `opt -passes=verify` | `make test`, L8.2 |
| Rows owed | 40 — the [Open queue](#open-queue--the-working-list) is the only list of owed work | this file |

The ledger behind the oracle column is `integration/conformance_cases.go`: the **absence**
of a row means "this program must behave like CPython", and a `debt` row needs a reason, an
**owner ID from this file**, and a pin per leg. A debt that gets paid fails the build until
the row is deleted — that promotion is the definition of done.

## Component map (state verified against the code)

| Component | File(s) | State |
|---|---|---|
| Lexer (INDENT/DEDENT, error recovery, Unicode identifiers) | `pkg/lang/lexer.go` | done |
| Parser → AST (Pratt, panic-mode recovery, incremental) | `pkg/lang/parser.go`, `ast.go`, `token.go`, `types.go` | done |
| Semantic analysis / gradual checker | `pkg/lang/semantic.go`, `variance.go`, `effects.go` | real gradual checker + effect signatures |
| Interpreter backend + precise-root GC | `pkg/lang/jit.go`, `value.go` | full dynamic surface |
| AOT codegen (textual IR) | `pkg/lang/codegen.go`, `closure.go`, `heapargs.go` | tagged element words; the rest is Phase 11 |
| Optimizer | `pkg/lang/opt.go` | real `opt` when installed + textual fallback (mem2reg, SROA, dead-elim) |
| Multi-file build | `pkg/lang/build.go` | done |
| Source maps / DWARF line tables | `pkg/lang/sourcemap.go`, `debug.go` | module-carried `!dbg` + read-back report (ADR 0231) |
| Standalone type-check (`gusty check`) | `pkg/lang/check.go` | mypy-style, ADR 0152 |
| Canonical formatter (`gusty fmt`) | `pkg/lang/fmt.go` | round-trips docstrings, raw/triple strings |
| Language server / LSP | `pkg/lang/lsp.go` | stdio; hover + completion + diagnostics + incremental-parse self-report |
| JSON schema / machine output | `pkg/lang/schema.go` | `--json` AST/IR/oracle/gc/optimization reports |
| Property/fuzz testing | `pkg/lang/proptest.go`, `proptest_test.go` | seeded cross-backend parity |
| CLI | `cmd/gustyc/main.go` | parse → semantic → (eval \| codegen → `opt` → `llc` → `cc`) |
| Version constant | `pkg/lang/compile.go` | `0.10.0` |

## Open queue — the working list

Top row is next work. `Blocked on` is the only legitimate reason to skip a row, and the
queue order encodes the dependency rules (Phase 11 before the items that read its tag).
Every open row is also pinned in the conformance ledger, so landing it must break a pin.

| Pri | ID | Item | Status | Blocked on | Next concrete action | Definition of done |
|---|---|---|---|---|---|---|
| 1 | L11.1 | Tagged value word (both backends) | 🟢 `IN PROGRESS` | — | floats in container elements (a float branch in `rt_print_mixed_value`) and nested containers (the collector must mark an element handle) | `programs/nested_data.gy (planned)` + `programs/probe_nested_list.gy` + `programs/probe_heterogeneous.gy` byte-identical on both backends **and** equal to CPython; `runtime_ir_test.go` asserts module-wide that no `i32 @.(str\|lst\|dict\|set)` sits in a value position |
| 2 | L11.1 | …then bools are values (`print(True)` → `True`) | ⏳ `PLANNED` | L11.1 step 1c | give bool its own `ValueTag`; one line of `elemKindTag` and every container follows | `probe_bool_value` promoted to `conformanceStandalone()`; `--json` reports the bool type |
| 3 | L11.2 | `str()` vs `repr()` are one pair per backend | ⏳ `PLANNED` | L11.1 bool step | move the rendering table into one shared pair — `str` for `print`/f-strings, `repr` inside containers | a table test drives every value form through both backends + CPython; `print(set())` is `set()`; Gap L.2 closed |
| 4 | L11.6 | Numeric truth in the compiled backend | ⏳ `PLANNED` | — | float-parameter inference in ADR 0174's shape, then `/=` → float, the `round` tie rule, `floor`/`ceil` → `int`, typed stdlib constants | `probe_math_const`, `probe_float_numeric`, `probe_float_param_rebind` promoted; closes Gaps P.1, P.2, R.3c, R.50, R.51; goldens re-derived in the same commit |
| 5 | Gap R.50 | `round` ties away from zero (Python ties to even) | ⏳ `OPEN` | — | one shared `round` per backend pair with round-half-to-even, and keep the result an `int` | a **new oracle row** (not a parity row) prints 2 / 4 / 0 / 0 on all three engines |
| 6 | Gap R.51 | `floor` / `ceil` / `sqrt` differ on all three engines | ⏳ `OPEN` | — | predeclared table ↔ interpreter ↔ codegen sweep: implement on both paths or refuse on both, and return `int` | `print(floor(3.7))` is `3` compiled and interpreted; `--check` and both legs agree on every name in `predeclared.go` |
| 7 | L11.3 | Tuples are values, not syntax sugar | ⏳ `PLANNED` | L11.1 | immutable tuple object in both backends: index, unpack, hashable as a dict key, printed `(1, 2)` / `(1,)` / `()` | `probe_tuple` + `probe_enumerate` promoted; feeds L6.6's covariant `tuple[…]` rule |
| 8 | L11.5 | Code-point strings (closes Gap N.2) | ⏳ `PLANNED` | L11.1 | one UTF-8 decode/measure helper shared by `len`, `s[i]`, `s[i:j]`, `for c in s`, the intern table and the printers | `len("café")` = 4, `"héllo"[1]` = `é`, `for c in "aé"` iterates characters compiled; `TestStringLengthIsBytesForNow` becomes the oracle test |
| 9 | L11.7 | Functions are values that compile | 🟨 `PARTIAL` | — | fnptr operands + indirect `call` lowering in `closure.go` | `def apply(f, xs): return f(x)` and a `lambda` through a parameter compile; `enumerate`/`zip`/`reversed`/`min`/`max(key=)` listed in `--lang`; `probe_fn_value`, `probe_fn_name`, `probe_comp_runtime_reduce` promoted |
| 10 | L11.8 | Refusal is part of the model — stable codes | 🟨 `PARTIAL` | — | schema'd `lower.unsupported.<shape>` codes for `list(<container>)`, `tuple(…)`, a compiled container of containers | every capability refusal appears in `--json` + `--schema`, and `TestCLIExitCodeContract` covers each exit class |
| 11 | L7.6a · Gap R.1 | Deferred coroutines in codegen | ⏳ `PLANNED` | L11.1 (tagged value) | lower an `async def` call to a coroutine object and run it at the `await` | `probe_async_eager` promoted: both backends print `between / effect 1 / 2` |
| 12 | Gap Q.1 | Container variable rebound to another kind keeps the old kind | ⏳ `OPEN` | L11.1 | settle a variable's kind from the object's tag instead of the `listVars`/`runtimeDicts`/`runtimeSets` maps | `d = {1: 10}; d = [3, 4]; print(d[0])` answers 3 on all three engines (re-measured 2026-10-01: interp 3, python 3, **aot 0**) |
| 13 | Gap Q.2 | 1024-object heap ceiling; exhaustion is silent (`rt_alloc` → `-1`) | ⏳ `OPEN` | — | raise a `MemoryError`-class trap instead of returning `-1`, then grow/segment `@heap` | capacity + headroom in `--gc-stats`; a heap-stress program traps with exit 3 instead of running on an invalid handle |
| 14 | Gap R.49 | A module variable shadowed in name by an earlier `def`'s parameter gets no slot → `llc` rejects the module (exit 2) | ⏳ `OPEN` | — | make module allocas per-scope and hoisted (the ADR 0163 / ADR 0181 rule) so a parameter's name never counts as a module binding | `def f(x): return x * 2` above `x = 8; x /= 2; print(x)` builds and prints `4.0`; an IR-shape test asserts the module's slots are allocated once, per scope |
| 15 | Gap J.2 | Set/dict comprehension **assignment** in AOT | 🟨 `PARTIAL` | — | extend ADR 0163's binding rule to set/dict comprehensions; teach the parser `{x for x in xs if c}` | `sa = {x for x in [3, 1, 2]}` and `da = {k: k * 2 for k in [1, 2]}` lower and print Python's answer |
| 16 | Gap K.8 | Tracebacks: the AOT frame stack | 🟨 `PARTIAL` | — | walk the frames an unwinder can read (the metadata landed with ADR 0231) | an uncaught AOT exception prints every frame with file:line, like the interpreter |
| 17 | Gap L.5 | `print` is not atomic | ⏳ `PLANNED` | — | evaluate all arguments (+ `sep`/`end`) to values, then emit the line as one unit, on both backends | `probe_print_atomic` promoted and the interleaving paragraph leaves `docs/language.md` |
| 18 | Gap M.2 | Flip `gustyc --file` to the compiled backend | ⏳ `PLANNED` | corpus green through the compiled leg | decide the divergence policy per construct, then flip the default | `--file` runs AOT, `--interp` stays the escape hatch, `--json` still names the leg that ran |
| 19 | Gap B | AOT `match`: class-pattern aliases + binding edges | 🟨 `PARTIAL` | — | resolve `Alias = Point` in pattern position; close the remaining bare-name binding edges | `case Alias(a, b):` matches identically (measured 2026-10-01: interp prints `no`, AOT prints `0`) |
| 20 | Gap R.16 | Iterating a string computed at run time is AOT-unsupported | ⏳ `OPEN` | L11.5 | iterate the interned `@str_tab` value through the `rt_str_*` helpers | `for c in s:` over a variable prints characters compiled; the refusal disappears |
| 21 | Gap R.31 | `%` on a string is not formatting | ⏳ `PLANNED` | — | implement `%`-formatting, or refuse it with a stable code and say so in `docs/language.md` | `probe_percent_format` promoted to parity |
| 22 | Gap R.33 | Sequence operations in the compiled backend | ⏳ `OPEN` | L11.1 (element words) | lower `list + list`, `list * int`, `str + str`, `str * int` to runtime helpers | `sequence_ops.gy` leaves the debt ledger |
| 23 | Gap R.35 | A function reads module-level **containers/floats** | 🟨 `PARTIAL` | — | extend ADR 0227's module globals to the container and float shapes | `programs/module_scope_in_functions.gy` carries the container/float cases on both legs |
| 24 | Gap R.37 | A constant operation that should trap is refused at compile time | ⏳ `OPEN` | — | emit the trap and let the runtime raise (ADR 0211's classes) | no tested shape turns a Python `ZeroDivisionError`/`IndexError` into a compile-time refusal |
| 25 | Gap R.38 | A refusal message claims something false about the other backend | ⏳ `OPEN` | — | derive the "the interpreter answers this program" sentence from the probe's recorded legs | every refusal test asserts the message **and** the other leg's measured behaviour |
| 26 | Gap R.46 | Printing an element of a freshly built comprehension list prints its index | ⏳ `OPEN` | L11.1 (tag at the read site) | route comprehension-built elements through the tagged read (`rt_get_elem` + `rt_tag_of`) | `print(xs[0])` on a runtime comprehension prints the element |
| 27 | Gap R.48 | There is no `global` statement | ⏳ `PLANNED` | — | surface `global x` over the `@gy_mod_<name>` storage ADR 0227 already built | `probe_global_statement` promoted; the checker gets the mirror of ADR 0228's local rule |
| 28 | L7.3 | Tagged pointers / NaN-boxing | ⏳ `PLANNED` | L11.1 | immediate int/float/bool where the tag says it is safe | `--gc-stats` reports the immediate share and an allocation-free int/float path is measured |
| 29 | L7.4 | Refcount + cycle-collector hybrid | ⏳ `PLANNED` | L11.1 | refcount acyclic data, trace cycles, keep a bounded deterministic pause | a program frees acyclic data at drop and cycles at the safe point |
| 30 | L7.5 | Algebraic effects: one lowering for `raise` / `yield` / `await` | ⏳ `PLANNED` | L7.6a | one effect machinery shared by exceptions, generators and async | the three constructs share one runtime path; ADR + IR-shape test |
| 31 | L8.1 | Generic monomorphization of `list[T]`/`dict[K,V]` | ⏳ `PLANNED` | L11.1 | instantiate per concrete type at compile time | monomorphic instances visible in `--emit-llvm`; unblocks L8.3 and L7.3 |
| 32 | L8.3 | Autovectorization + `--report=vector` | ⏳ `PLANNED` | L8.1 | annotate hot numeric loops; add the report | one corpus loop reports vectorized, another reports why not, both in `--json` |
| 33 | L9.1 | Incremental JIT REPL | 🟨 `PARTIAL` | — | reuse the span-keyed parse tree (L5.8) across REPL turns and JIT only the changed range | a REPL edit recompiles one range (statements reused, measured) and `--repl --jit` keeps state |
| 34 | L9.2 | Package manager (`gusty install`/`publish`) | ⏳ `PLANNED` | — | registry format + lockfile + per-package FFI bindings | `gusty install <pkg>` is reproducible from a lockfile; `--json` lists installed packages |
| 35 | L9.3 | Incremental build cache (`gusty build --cache`) | ⏳ `PLANNED` | — | key module IR/objects by source hash + the L6.7 dependency graph | only dirty subgraphs rebuild; a cached run reports its hits in `--build --json` |
| 36 | L9.4 | Richer LSP: definition, references, rename | 🟨 `PARTIAL` | — | implement `textDocument/definition`, `/references`, `/rename` over the checker's symbol tables | each request is answered for a real buffer in `lsp_test.go` |
| 37 | L9.5 | Formatting on save (LSP provider over `gusty fmt`) | ⏳ `PLANNED` | — | expose the canonical formatter as `textDocument/formatting` | formatting a buffer round-trips f-strings, trailing commas and docstrings |
| 38 | L9.6 | Fuzz/parity CI: parse → format → reparse | 🟨 `PARTIAL` | — | differential-test the front end as well as the two back ends, on unions/async/walrus | a seeded, deterministic diff test runs in CI |
| 39 | L10.1 | WASM target (`--target=wasm`) | ⏳ `PLANNED` | L11.1, L7.1 | lower AOT to WebAssembly; map the scheduler to the wasm event loop | a corpus subset builds and runs under a wasm runtime, and `--target` reports what it supports |
| 40 | Gap R.52 | stdlib discovery is cwd-relative, so an installed binary cannot `import math` | ⏳ `OPEN` | — | also search beside the executable (and `../share/pyre/stdlib`), and report the root searched in `--json` | `import math` works from any directory with an installed binary, no flags |

## Phase tables

A row's `Status` states whether **that row's** Definition of Done is met (see the two DoD
sections at the bottom). An open follow-on is named inside the cell and carried exactly
once in the [queue](#open-queue--the-working-list) above.

### Phase 0 — hygiene

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| P0.1 | Version reconciled — `0.10.0` in `compile.go`, printed by the CLI | ✅ `DONE` | cli | — | `pkg/lang/compile.go`, `gustyc --version` | — |
| P0.2 | Renumber the duplicated ADR `0143` → `0152` (standalone type-check) | ✅ `DONE` | docs | 0152 | `docs/adr/0152-standalone-type-check-mode.md` | — |

### Phase 1 — AOT/interpreter parity

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| P1.1 | Floats in AOT: real `double` IR, `sitofp` promotion, `fcmp`, `%.17g` print, `float()` folding | ✅ `DONE` | both | 0086, 0106 | `floatrepr_test.go`, `division_test.go`, `floatfn.gy` | — |
| P1.2 | Runtime heap + GC in AOT: boxed lists/dicts/sets, append/index/rebind, free-list reuse, bounds sentinel, mark-and-sweep, rooted envs | ✅ `DONE` | aot | 0009, 0134, 0151, 0163, 0181 | `memory_test.go`, `gc_roots_ir_test.go`, `runtime_ir_test.go`, `dispatch_gc_stress.gy` | [→](docs/roadmap-details.md#gap-a) |

### Phase 2 — language surface

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| P2.1 | Gradual typing (`--verify` / `gusty check` check assignment annotations) | ✅ `DONE` | checker | 0152 | `semantic_test.go`, `check_test.go` | — |
| P2.2 | Comprehensions (list / dict / set), incl. calls in the element and filter | 🟨 `PARTIAL` — AOT: set/dict comprehension **assignment** is Gap J.2 | both | 0192 | `comprehension_test.go`, `comprehension_calls.gy`, `probe_comp_runtime_reduce` | [→](docs/roadmap-details.md#l11-7) |
| P2.3 | Slicing (`s[a:b]`, `s[::step]`, negative indices) | ✅ `DONE` — string **variables** included; re-measured 2026-10-01 (`s[1:4]`, `s[::3]`, `s[-5:]` identical on both legs + CPython) | both | 0137, 0210, 0225 | `negative_index_test.go`, `string_subscript_test.go`, `subscript_assign_test.go` | [→](docs/roadmap-details.md#gap-e) |
| P2.4 | Augmented assignment (`+= -= *= /= //= %=`) | ✅ `DONE` | both | 0216, 0221 | `floor_division_test.go`, `truthiness_test.go` | — |
| P2.5 | Tuple unpacking / multi-assign (`a, b = b, a`, `for a, b in …`) | ✅ `DONE` — tuples *as values* is L11.3 | both | — | `tuple_test.go` | [→](docs/roadmap-details.md#l11-3) |
| P2.6 | Membership + identity (`in` / `not in`, `is` / `is not`) | ✅ `DONE` | both | 0156, 0232 | `mixed_container_test.go`, `container_equality_test.go` | [→](docs/roadmap-details.md#gap-g) |
| P2.7 | Power `**` (right-assoc; `@llvm.pow.f64` in AOT) | ✅ `DONE` | both | — | `jit_llvm_test.go`, `features_a.gy` | — |
| P2.8 | `match`: guards, or-patterns, dict patterns, `_` | ✅ `DONE` | both | 0010, 0154 | `match_test.go`, `match_literal.gy` | [→](docs/roadmap-details.md#gap-b) |
| P2.9 | `match`: class patterns (subclass walk + attribute binding) | 🟨 `PARTIAL` — aliases and bare-name binding edges open (Gap B) | both | 0010 | `match_test.go`, `match_baren.gy` | [→](docs/roadmap-details.md#gap-b) |
| P2.10 | Operator overloading / dunder dispatch | ✅ `DONE` — AOT dispatch is static (`varClasses` / `receiverClass`) | both | 0139 | `dunder.gy`, `operator_operand_test.go` | [→](docs/roadmap-details.md#gap-f) |
| P2.11 | `with` / context managers + `yield from` | ✅ `DONE` | both | 0140 | `TestParityYieldFromAcrossGC`, `TestParityYieldFromLiteral` | [→](docs/roadmap-details.md#gap-d) |
| P2.12 | f-strings with `{}` interpolation | ✅ `DONE` | both | — | `fstr.gy`, `print_semantics_test.go` | — |
| P2.13 | Docstrings + `__doc__` | ✅ `DONE` — folded to a string constant in AOT too | both | 0141 | `TestJITDocstrings`, `TestParityDocstrings` | [→](docs/roadmap-details.md#gap-e) |
| P2.14 | FFI / `extern fn` → C calls | ✅ `DONE` | both | 0147 | `ffi_test.go` | — |
| P2.15 | On-disk stdlib modules (`math`, `string`, `collections`, `json`) | 🟨 `PARTIAL` — stdlib float constants fold to `int` in AOT (L11.6) | both | 0090 | `stdlib_test.go`, `probe_math_const` | [→](docs/roadmap-details.md#l11-6) |

### Phase 3 — tooling

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| P3.1 | CLI pipeline (eval \| codegen → `opt` → `llc` → `cc`) | ✅ `DONE` | cli | 0164 | `pipeline_test.go`, `build_test.go` | — |
| P3.2 | Multi-file build | ✅ `DONE` | cli | — | `merged/*` matrix rows, `build_test.go` | — |
| P3.3 | Source maps + DWARF debug info | ✅ `DONE` | aot | 0231 | `sourcemap_test.go`, `debug_test.go`, `debug_info_test.go` | — |
| P3.4 | Standalone type-check mode (`gusty check`) | ✅ `DONE` | checker | 0152 | `check_test.go` | — |
| P3.5 | Canonical formatter (`gusty fmt`) | ✅ `DONE` | cli | — | `fmt_test.go` | — |
| P3.6 | LSP server (stdio; hover, completion, diagnostics) | ✅ `DONE` | tooling | 0145 | `lsp_test.go` | — |
| P3.7 | `--json` schema for AST/IR dumps | ✅ `DONE` | cli | 0087, 0211 | `schema_test.go`, `types_json_test.go` | — |
| P3.8 | Seeded property/fuzz testing across both backends | ✅ `DONE` | both | 0148 | `proptest_test.go` (unit + integration) | — |

### Phase 4 — lexer modernization

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| L4.1 | Error-recovering lexer: `TokError` + resume, several diagnostics per run, parse-partial still checked | ✅ `DONE` | both | 0177 | `lexer_test.go`, `check_test.go` | [→](docs/roadmap-details.md#l4-1) |
| L4.2 | Rich token spans (byte **and** rune offsets, multi-line flag) | ✅ `DONE` | both | — | probed not assumed: a triple-quoted token reports its rune range | [→](docs/roadmap-details.md#l4-2) |
| L4.3 | Unicode identifiers (`ID_Start`/`ID_Continue`, NFC, homoglyph warning) | ✅ `DONE` | both | 0155 | `lexer_test.go` (`café`, `Ο` vs `O`) | [→](docs/roadmap-details.md#l4-3) |
| L4.4 | Numeric literals: `0x` / `0b` / `0o`, `_` separators, `_`-misuse rejected | ✅ `DONE` | both | 0153 | `literal_test.go` | [→](docs/roadmap-details.md#l4-4) |
| L4.5 | Raw + triple-quoted + raw-triple strings; docstrings reuse them; the formatter keeps the form | ✅ `DONE` | both | — | `strings_test.go`, `fmt_test.go` | [→](docs/roadmap-details.md#l4-5) |
| L4.6 | Line continuation with a trailing `\` | ✅ `DONE` | both | — | `lexer_test.go` | [→](docs/roadmap-details.md#l4-6) |
| L4.7 | Token-stream `Cursor` shared by parser, formatter and LSP | ✅ `DONE` | tooling | — | `cursor_test.go` | [→](docs/roadmap-details.md#l4-7) |

### Phase 5 — parser modernization

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| L5.1 | Pratt / precedence-climbing expression parser | ✅ `DONE` | both | — | `parser_pratt_test.go` | [→](docs/roadmap-details.md#l5-1) |
| L5.2 | Panic-mode error recovery → a forest of `ParseError`s, partial AST kept | ✅ `DONE` | both | 0177 | `lsp_test.go`, `check_test.go` | [→](docs/roadmap-details.md#l5-2) |
| L5.3 | Trailing commas in calls, literals, tuples, `match` args; formatter normalizes them | ✅ `DONE` | both | — | `trailing_comma_test.go` | [→](docs/roadmap-details.md#l5-3) |
| L5.4 | Walrus `:=` usable in conditions and comprehensions | ✅ `DONE` | both | — | `parser_pratt_test.go`, `features_a.gy` | [→](docs/roadmap-details.md#l5-4) |
| L5.5 | Union-type syntax `int \| str` in annotation and pattern position | ✅ `DONE` | both | 0158 | `union_aot_test.go`, `types_json_test.go` | [→](docs/roadmap-details.md#l5-5) |
| L5.6 | `async def` / `await` / `async for` / `async with` as first-class syntax | ✅ `DONE` | both | 0195 | `async_basic.gy`, `async_for.gy`, `async_multi.gy` | [→](docs/roadmap-details.md#l5-6) |
| L5.7 | Type aliases `type X = …` (structural, compile-time no-op) | ✅ `DONE` | both | 0159 | `typealias_test.go`, `typealias.gy` | [→](docs/roadmap-details.md#l5-7) |
| L5.8 | Incremental parse: three-region splice + a `parseCache` self-report | ✅ `DONE` | tooling | 0176 | `incremental_test.go`, `lsp_test.go` | [→](docs/roadmap-details.md#l5-8) |

### Phase 6 — semantics & type system

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| L6.1 | Exhaustiveness checking for `match` (the semantic half of Gap B) | ✅ `DONE` | checker | 0154 | `match_exhaustiveness_check_test.go` | [→](docs/roadmap-details.md#l6-1) |
| L6.2 | Definite-assignment analysis; possibly-unbound reads warn before codegen | ✅ `DONE` | checker | 0228 | `semantic_test.go`, `unwritten_slot_test.go` | [→](docs/roadmap-details.md#l6-2) |
| L6.3 | Union types across assignment + call boundaries; AOT tagged-union slots | ✅ `DONE` | both | 0158 | `union_aot_test.go` | [→](docs/roadmap-details.md#l6-3) |
| L6.4 | Literal types (`Literal[1, 2]`) feeding exhaustiveness + narrowing | ✅ `DONE` | checker | — | `semantic_test.go` | — |
| L6.5 | Type narrowing / refinement (`isinstance` branches, match-literal narrowing) | ✅ `DONE` | checker | — | `TestNarrowIsInstanceThen/Else/Not` | [→](docs/roadmap-details.md#l6-5) |
| L6.6 | Variance + generics: one structural `subType`, invariant / covariant / contravariant tables, stable diagnostic codes, `--variance` | ✅ `DONE` — `Callable` parameters stay checker-only until L11.7 | checker | 0146, 0160 | `variance_test.go`, `variance_check_test.go`, `variance.gy` | [→](docs/roadmap-details.md#l6-6) |
| L6.7 | Call graph + reachability driving dead-global elimination and docstring folding | ✅ `DONE` | tooling | — | `opt_test.go` | [→](docs/roadmap-details.md#l6-7) |

### Phase 7 — runtime: concurrency, effects, memory

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| L7.1 | Async runtime: `async def` returns a coroutine object, `await` runs it to completion, `async for` awaits each element | ✅ `DONE` — mid-body suspension and `__aenter__`/`__aexit__` are future work (→ L7.6a) | interp (AOT stays eager) | 0195 | `async_test.go`, `effects_test.go`, `probe_async_eager` | [→](docs/roadmap-details.md#l7-1) |
| L7.2 | Precise stack roots: interpreter frame set + watermark; AOT root stack (`@gc.roots`, `rt_root_put`, `rt_frame_open/close`) | ✅ `DONE` | both | 0181 | `gc_roots_ir_test.go`, `gc_stress_test.go`, `gc_precise.gy`, `GUSTY_GC_STRESS=1` | [→](docs/roadmap-details.md#l7-2) |
| L7.3 | Tagged pointers / NaN-boxing (allocation-free small values) | ⏳ `PLANNED` | both | — | — | [→](docs/roadmap-details.md#l7-3) |
| L7.4 | Refcount + cycle-collect hybrid | ⏳ `PLANNED` | both | — | — | [→](docs/roadmap-details.md#l7-4) |
| L7.5 | Algebraic effects: one lowering for `raise` / `yield` / `await` | ⏳ `PLANNED` | both | — | — | [→](docs/roadmap-details.md#l7-5) |
| L7.6 | Effect / async exhaustiveness as a semantic check (`async.coro.*`, `async.await.*`, `--effects`) | ✅ `DONE` | checker | 0195 | `effects_test.go` (unit + integration) | [→](docs/roadmap-details.md#l7-6) |
| L7.6a | Deferred coroutines in codegen | ⏳ `PLANNED` | aot | 0195 | `probe_async_eager` (interp `between / effect 1 / 2`, compiled `effect 1 / between / 2`) | [→](docs/roadmap-details.md#l7-6a) |

### Phase 8 — codegen

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| L8.1 | Generic monomorphization of `list[T]` / `dict[K,V]` | ⏳ `PLANNED` | aot | — | — | [→](docs/roadmap-details.md#l8-1) |
| L8.2 | `verifyModule`-driven pipeline: `opt -passes=verify` (fallback `llc -filetype=null`), `--verify-llvm`, a missing toolchain is `skipped`, never `ok` | ✅ `DONE` | cli | 0164 | `verify_llvm_test.go`, `BuildResult.verification` | [→](docs/roadmap-details.md#l8-2) |
| L8.3 | Autovectorization + `--report=vector` | ⏳ `PLANNED` | aot | — | — | [→](docs/roadmap-details.md#l8-3) |
| L8.4 | SROA / scalar replacement of non-escaping heap objects | ✅ `DONE` — `scalarRepl` in the textual pass; the monomorphization-driven form is L8.1 | aot | 0134 | `opt_test.go`; single-block only, escapes/appends/unwritten indices skipped | [→](docs/roadmap-details.md#gap-h) |
| L8.5 | Debug line tables in the module: `DICompileUnit` / `DISubprogram` / `DILocation`, read back via `--debug-info` and `--build --debug` | ✅ `DONE` | aot | 0231 | `debug_test.go`, `debug_info_test.go` | [→](docs/roadmap-details.md#l8-5) |

### Phase 9 — developer loop

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| L9.1 | Incremental JIT REPL | 🟨 `PARTIAL` — `--repl` (interpreter) and `--repl --jit` (whole-program `lang.JIT` per turn) ship; there is no span-keyed incremental recompile | cli | — | `cmd/gustyc/repl.go` | [→](docs/roadmap-details.md#l9-1) |
| L9.2 | Package manager (`gusty install` / `publish`, registry, lockfile, per-package FFI) | ⏳ `PLANNED` | cli | — | — | [→](docs/roadmap-details.md#l9-2) |
| L9.3 | Incremental build cache (`gusty build --cache`, source-hash + dependency keying) | ⏳ `PLANNED` | cli | — | — | [→](docs/roadmap-details.md#l9-3) |
| L9.4 | Richer LSP | 🟨 `PARTIAL` — hover, completion, diagnostics (with the incremental-parse self-report) ship; go-to-definition, find-references and rename do not | tooling | 0145 | `lsp_test.go` | [→](docs/roadmap-details.md#l9-4) |
| L9.5 | Formatting on save (`textDocument/formatting` over `gusty fmt`) | ⏳ `PLANNED` | tooling | — | — | [→](docs/roadmap-details.md#l9-5) |
| L9.6 | Fuzz / parity CI over the new surface | 🟨 `PARTIAL` — seeded cross-backend parity (`TestPropParity`, seeds) ships; the parse → format → reparse differential does not | both | 0148 | `proptest_test.go` | [→](docs/roadmap-details.md#l9-6) |

### Phase 10 — targets, ABI, portability

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| L10.1 | WASM target (`--target=wasm`) | ⏳ `PLANNED` | aot | — | `--target <triple>` exists; no wasm lowering | [→](docs/roadmap-details.md#l10-1) |
| L10.2 | Versioned, documented C ABI for `extern fn` exports | ✅ `DONE` | aot | 0147 | `abi.go`, `abi_test.go` | [→](docs/roadmap-details.md#l10-2) |
| L10.3 | Shared-library export (`--build <out> --shared`: PIC object + `cc -shared -fPIC`, ABI marker carried) | ✅ `DONE` | cli | 0147 | `BuildShared`, `build_test.go` | [→](docs/roadmap-details.md#l10-3) |
| L10.4 | Benchmark harness (`--bench-suite`, `--bench-baseline`, best-of-N gate, exit 5 on regression) | ✅ `DONE` | cli | 0162 | `bench_test.go`, `bench_suite_test.go`, `docs/benchmark.md` | [→](docs/roadmap-details.md#l10-4) |

### Phase 11 — one tagged value model

An untagged `i32` cannot say what it is, so it cannot nest, cannot mix kinds, and cannot
carry `True` vs `1`: every remaining invalid-module, crash or "unsupported" defect has that
one root. Do not start L11.3 / L11.5 / L11.6 before L11.1 — they would re-decide the
representation locally. The measured divergence table that motivated the phase is in the
[record](docs/roadmap-details.md#phase-11); the live state of each of its rows is the pin in
`integration/conformance_cases.go`, not a paragraph here.

| ID | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| L11.1 | Tagged value word — one tag table, then tags on every element path | 🟢 `IN PROGRESS` — **landed:** one tag table + heap-kind projection (0182), a tag per element (0184), mixed-list loops bind `(value, tag)` (0185), payload+tag written and read as pairs (0187), containers compare by value (0189), mixed dicts/sets + tagged lookup + promotion (0232). **Open:** nested containers (element handles are not marked by the collector), floats in containers (`rt_print_mixed_value` has no float branch), `@estr[h]` retirement, bools as values | both | 0182, 0184, 0185, 0187, 0188, 0189, 0232 | `value_tags_test.go`, `heapargs_test.go`, `mixed_list_test.go`, `mixed_dict_set_test.go`, `container_eq_test.go`, `container_element_test.go`, `mixed_container_test.go`, `string_containers_test.go`, `mixed_element_reads.gy`, `mixed_element_writes.gy`, `container_equality.gy` | [→](docs/roadmap-details.md#l11-1) |
| L11.2 | `str()` vs `repr()` are one pair per backend (closes Gap L.2) | ⏳ `PLANNED` — gated on L11.1's bool step: `--json` still reports `"type": "int"` for `True` | both | 0183 (`str(None)` fold) | `probe_bool_value`, `container_methods` / `none_values` / `string_containers` / `string_escapes` / `string_params` debt rows, `print_containers_test.go`, `str_fold_test.go` | [→](docs/roadmap-details.md#l11-2) |
| L11.3 | Tuples are values, not syntax sugar (indexable, unpackable, hashable, printed `(1, 2)`) | ⏳ `PLANNED` | both | — | `probe_tuple`, `probe_enumerate`, `tuple_test.go` (unpacking only) | [→](docs/roadmap-details.md#l11-3) |
| L11.4 | Python-shaped indexing: negatives, bounds, one rule — dicts/sets exempt because a key is a key | ✅ `DONE` | both | 0210 | `negative_index_test.go`, `negative_index.gy`, `negative_literal_index.gy` | [→](docs/roadmap-details.md#l11-4) |
| L11.5 | Code-point strings (closes Gap N.2) | ⏳ `PLANNED` | both | — | `TestStringLengthIsBytesForNow`, `string_escapes.gy` debt row | [→](docs/roadmap-details.md#l11-5) |
| L11.6 | Numeric truth in the compiled backend (closes Gaps P.1 + P.2: `/=` → float, float through an untyped parameter, typed stdlib constants; owns Gaps R.50 + R.51: the `round` tie rule and `floor`/`ceil` returning `int`) — the floored `//` and float `%` half already landed with ADR 0216 | ⏳ `PLANNED` | both | 0216, 0221 | `probe_math_const`, `probe_float_numeric`, `probe_float_param_rebind`, `probe_float_list_equal` | [→](docs/roadmap-details.md#l11-6) |
| L11.7 | Functions are values that compile | 🟨 `PARTIAL` — sorting is surface (0191) and comprehensions build their own lists (0192); fnptr operands, indirect calls, `enumerate`/`zip`/`reversed`/`min`/`max(key=)`, and `insert`/`index`/`remove`/`extend`/`clear` are not | both | 0191, 0192 | `sorting_test.go`, `sort_test.go`, `sorting.gy`, `sorting_literals.gy`, `comprehension_calls.gy`, `probe_fn_value`, `probe_fn_name`, `probe_comp_runtime_reduce`, `probe_comp_folded_iter` | [→](docs/roadmap-details.md#l11-7) |
| L11.8 | Refusal is part of the model, and so is its exit code | 🟨 `PARTIAL` — the exit-code half is done (0211: `llc` rejection 2, trap 3 on every path, refusal 1, missing toolchain ≠ rejection); capability diagnostics with stable `lower.unsupported.<shape>` codes are open | cli | 0166, 0211 | `TestCLIExitCodeContract`, `trap_exit_test.go`, `toolchain_error_test.go` | [→](docs/roadmap-details.md#l11-8) |
| L11.9 | The corpus is the spec: three-leg matrix (interp / compiled / CPython), one classifier, ledger-as-spec, exit 6 + 7 | ✅ `DONE` | both | 0186, 0193 | `oracle_test.go` (unit + integration), `cmd/gustyc/oracle_test.go`, `tools/oracleprobe`, `conformance-matrix.json` | [→](docs/roadmap-details.md#l11-9) |

(`Gap L.5` and `Gap L.6` were found by this phase's oracle leg and live in the
[Gap L table](#gap-l--values-and-rendering), so they are not repeated here.)

**Machine path (AGENTS.md, non-negotiable).** The tag enum is published as `gustyc --schema`
→ `definitions.valueTag` and named in `--lang`; every refusal carries a stable
`Diagnostic.Code` and appears in `--json`; the exit-code table in `docs/operations.md` is the
implementation for both legs.

## Gap ledger

Gaps are defects found while closing other work. Same rule as the phases: one row, one
status, one piece of evidence — the story stays in the record.

### Gaps A–I

| Gap | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| Gap A | AOT dynamic-dispatch correctness: handle slots rooted on every assignment form; the collector marks reachable heap data transitively | ✅ `DONE` | aot | 0151 | `dispatch_gc_stress.gy` (1024-slot churn under 2000 throwaway lists), `dispatch_gc.gy`, `dispatch_nested.gy` | [→](docs/roadmap-details.md#gap-a) |
| Gap B | AOT `match`: list / dict / class-pattern lowering, guards, `_`, or-patterns, bare-name capture | 🟨 `PARTIAL` — class-pattern **aliases** (`Alias = Point`) and bare-name binding edges open | both | 0010, 0154 | `match_test.go` (`TestMatchBareNameCapture*`), `match_baren.gy`, `match_exhaustiveness_check_test.go` | [→](docs/roadmap-details.md#gap-b) |
| Gap C | Arbitrary fnptr-valued decorators: the canonical wrapping decorator compiles via `@f_orig` + `@f_impl` + funcBind specialization | ✅ `DONE` | aot | 0157 | `wrapping_decorator.gy` | [→](docs/roadmap-details.md#gap-c) |
| Gap D | AOT `with` / `yield from` runtime: generator handles rooted across body-statement boundaries; literal `yield from` unrolls | ✅ `DONE` | both | 0140 | `TestParityYieldFromAcrossGC`, `TestParityYieldFromLiteral` | [→](docs/roadmap-details.md#gap-d) |
| Gap E | AOT float / `__doc__` / string-slicing leftovers: conversion builtins emit `double`, `__doc__` folds to a constant, string **variable** slicing lowers through `rt_slice` | ✅ `DONE` — last leftover re-measured 2026-10-01: `s[1:4]`, `s[::3]`, `s[-5:]`, `t[1:7:2]` over variables are identical on both legs and CPython | both | 0104, 0141 | `TestParityConversionBuiltins`, `TestJITDocstrings`, `rt_slice` in `codegen.go` | [→](docs/roadmap-details.md#gap-e) |
| Gap F | AOT operator overloading (dunder dispatch, left then reflected, builtin fallback) | ✅ `DONE` — static dispatch: fires on operands proven to be instances at compile time | both | 0139 | `dunder.gy` | [→](docs/roadmap-details.md#gap-f) |
| Gap G | AOT `in` / `not in` on inline literal containers (unrolled comparisons, empty literals fold, `not in` inverts) | ✅ `DONE` | both | 0156 | `TestParityLiteralMembership` | [→](docs/roadmap-details.md#gap-g) |
| Gap H | LLVM `opt` invoked when installed; deterministic textual fallback (const-fold, mem2reg, deadHeapElim, SROA, dead-block, dead-global) | ✅ `DONE` | cli | 0088 | `optimize_report_test.go`, `opt_llvm_test.go`, `--build --json` `optimization` | [→](docs/roadmap-details.md#gap-h) |
| — | SROA follow-on: `scalarRepl` promotes a non-escaping heap list with constant indices to registers | ✅ `DONE` | aot | 0134 | `opt_test.go` | [→](docs/roadmap-details.md#gap-h) |
| Gap I.1 | Heap containers across function boundaries: literal args materialised by handle; whole-module parameter-kind inference to a fixed point | ✅ `DONE` | aot | 0161 | `heapargs_test.go`, `heap_args_test.go`, `heap_containers.gy` | [→](docs/roadmap-details.md#gap-i-1) |
| Gap I.2 | Strings inside runtime containers: interned string table + parallel repr table, one printer helper for list / set / dict | ✅ `DONE` | both | 0173 | `string_containers_test.go` (unit + integration), `string_containers.gy` | [→](docs/roadmap-details.md#gap-i-2) |
| Gap I.3 | Assigned containers are heap handles: folded comprehensions materialise, module definitions allocate + root their slot, per-body binding scope, sorted fixed-point merge | ✅ `DONE` | aot | 0163 | `folded_lists_test.go`, `folded_lists.gy` | [→](docs/roadmap-details.md#gap-i-3) |

### Gap J — found while closing earlier gaps

| Gap | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| Gap J.1 | Multi-argument `print(*args, sep=, end=)` on both backends; the container printers take the terminator as a flag | ✅ `DONE` | both | 0165 | `print_args.gy`, `print_semantics_test.go` | [→](docs/roadmap-details.md#gap-j-1) |
| Gap J.2 | Set/dict comprehension **assignment** in AOT, and parsing `{x for x in xs if …}` | 🟨 `PARTIAL` — set rendering landed with 0165; module-scope set/dict comprehension assignment still does not lower | both | 0165, 0163 | `sa = {x for x in [3, 1, 2]}` / `da = {k: k * 2 for k in [1, 2]}` (both open) | [→](docs/roadmap-details.md#gap-j-2) |
| Gap J.3 | The exit-code table became the implementation (1 compile · 2 LLVM rejected our module · 3 trap · 4 usage · 5 benchmark regression) | ✅ `DONE` | cli | 0164, 0166 | `TestCLIExitCodeContract`, `TestExitCodesAreDistinctAndDocumented` | [→](docs/roadmap-details.md#gap-j-3) |
| Gap J.4 | The `opt` fallback stopped being silent: an `Optimization` report (`tool`, `pipeline`, `applied`, `fallback`, `note`); flags after positionals get reordered | ✅ `DONE` | cli | — | `optimize_report_test.go`, `TestCLIBuildReportsOptimization` | [→](docs/roadmap-details.md#gap-j-4) |
| Gap J.5 | String arguments to user functions (AOT): interned at the call site, `strArgKinds` inference, `@estr` object flags | ✅ `DONE` | aot | 0174 | `string_args_test.go` (unit + integration), `string_params.gy` | [→](docs/roadmap-details.md#gap-j-5) |
| Gap J.6 | dict/set literals with strings; one element kind per position; heterogeneous contents became a diagnostic, then per-element tagging | ✅ `DONE` | both | 0175 | `TestContainerLiteralsMatchPython`, `TestMixedContainersAreADiagnosticNotAMisprint` | [→](docs/roadmap-details.md#gap-j-6) |

### Gap K — truthiness, containers, exceptions

| Gap | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| Gap K.1 | Truthiness on both backends (int conditions, `and`/`or`/`not`, ternary, containers by length, heap-aware `truthy`); every free guarded because handle 0 is a legal slot | ✅ `DONE` | both | 0167 | `truthiness_test.go` (38 cases vs Python), `truthiness.gy`, `TestEmptyBindingNeverFreesHandleZero` | [→](docs/roadmap-details.md#gap-k-1) |
| Gap K.2 | `for k in d:` over a dict in AOT asked the runtime for length and yielded keys from the pair layout | ✅ `DONE` | aot | — | `dict_iteration_test.go` (17 cases), `subscript_assign.gy` | [→](docs/roadmap-details.md#gap-k-2) |
| Gap K.3 | `list.pop`, `set()`, and the set/list/dict methods became language surface | ✅ `DONE` | both | 0170 | `containers_test.go`, `container_methods.gy` | [→](docs/roadmap-details.md#gap-k-3) |
| Gap K.4 | Item assignment was silently dropped by the parser (`d[1] = 2` produced no statement) | ✅ `DONE` | both | 0168 | `subscript_assign_test.go` | [→](docs/roadmap-details.md#gap-k-4) |
| Gap K.5 | `{}` was a set in the interpreter and a dict in AOT | ✅ `DONE` | both | — | `containers_test.go` | [→](docs/roadmap-details.md#gap-k-5) |
| Gap K.6 | Unhandled exceptions reported nothing in AOT; runtime errors were not exceptions in the interpreter | ✅ `DONE` | both | 0169 | `uncaught_exception_test.go` | [→](docs/roadmap-details.md#gap-k-6) |
| Gap K.7 | `--build` could fail with no stated reason | ✅ `DONE` | cli | — | `build_test.go` | [→](docs/roadmap-details.md#gap-k-7) |
| Gap K.8 | Tracebacks had no usable source location (`raise` carried no span) | 🟨 `PARTIAL` — spans + module line tables landed (0171, 0231); the AOT frame stack is the open half | both | 0171 | `sourcemap_test.go`, `debug_test.go` | [→](docs/roadmap-details.md#gap-k-8) |
| Gap K.10 | `print(<undefined name>)` reached LLVM, so an ordinary typo reported a compiler bug | ✅ `DONE` | both | 0166 | `undefined_test.go`, `TestCLIBuildTypoIsACompileErrorNotACompilerBug` | [→](docs/roadmap-details.md#gap-k-10) |

### Gap L — values and rendering

| Gap | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| Gap L.1 | `None` was the integer 0 and every program printed a stray `0`: singleton value in both backends, CLI echo rule, bare `return` yields None | ✅ `DONE` | both | 0172 | `none_test.go`, `none_values.gy`, `TestCLIEvalDoesNotEchoVoid` | [→](docs/roadmap-details.md#gap-l-1) |
| Gap L.2 | `str()` vs `repr()` decided in two places (bools, quoting inside containers, the empty-set rule) | ⏳ `PLANNED` — owner: L11.2 | both | — | `probe_bool_value`, four rendering debt rows | [→](docs/roadmap-details.md#l11-2) |
| Gap L.5 | `print` is not atomic: it writes each argument while evaluating the arguments | ⏳ `PLANNED` | both | found by 0186 | `probe_print_atomic`, the `print_args` debt row | [→](docs/roadmap-details.md#gap-l-5) |
| Gap L.6 | A container printed as its handle, its global, or its predecessor's strings | ✅ `DONE` | both | 0188 | `empty_containers.gy`, `empty_set.gy`, `TestNoContainerGlobalInAValuePosition` | [→](docs/roadmap-details.md#gap-l-6) |

### Gap M, N, P, Q — CLI, strings, numerics, memory

| Gap | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| Gap M.1 | `gustyc prog.gy` was a silent no-op (a positional now means `--file`; a missing `.gy` is exit 4) | ✅ `DONE` | cli | — | `TestCLIPositionalSourcePathRuns` + siblings in `build_test.go` | [→](docs/roadmap-details.md#gap-m-1) |
| Gap M.2 | `gustyc --file` runs the interpreter: the backend is now reported (`--json`, `--show-backend`); flipping the default to compiled is owed | 🟨 `PARTIAL` | cli | 0179 | `pipeline_test.go` | [→](docs/roadmap-details.md#gap-m-2) |
| Gap N | String literals were bytes and escapes were letters: one decoder implementing Python's escape rules | ✅ `DONE` | both | 0178 | `escapes_test.go` (unit + integration), `string_escapes.gy` | [→](docs/roadmap-details.md#gap-n) |
| Gap N.2 | Strings measure bytes, not code points (`len("café")` = 5, `s[1]` = a byte) | ⏳ `OPEN` — owner: L11.5 | both | — | `string_escapes.gy` debt row | [→](docs/roadmap-details.md#gap-n-2) |
| Gap P | `/` truncated and floats printed as ints | ✅ `DONE` — the sub-gaps stay open | both | 0180 | `division_test.go`, `floatrepr_test.go` | [→](docs/roadmap-details.md#gap-p) |
| Gap P.1 | Compiled float *state*: `x /= 2` leaves an int (prints `4`, CPython `4.0`) and a float through an untyped parameter arrives as `0` — the `-7 // 2` half of this row closed with ADR 0216 | ⏳ `OPEN` — owner: L11.6; re-measured 2026-10-01 | aot | 0216 | `probe_float_numeric` | [→](docs/roadmap-details.md#gap-p-1) |
| Gap P.2 | Numeric builtins Python types differently: float `%` is now right on both backends (`-3.5 % 2.0` → `0.5`, ADR 0216); what stays wrong is the tie rule (Gap R.50) and `floor`/`ceil` returning a float instead of an `int` (Gap R.51) | ⏳ `OPEN` — owner: L11.6 | both | 0216 | `probe_float_numeric` | [→](docs/roadmap-details.md#gap-p-2) |
| Gap Q.1 | A container variable rebound to another kind keeps the old kind | ⏳ `OPEN` — owner: L11.1; re-measured 2026-10-01 (interp 3, python 3, **aot 0**) | aot | — | `d = {1: 10}; d = [3, 4]; print(d[0])` | [→](docs/roadmap-details.md#gap-q-1) |
| Gap Q.2 | The runtime heap is a fixed 1024 objects and exhaustion is silent (`rt_alloc` → `-1`) | ⏳ `OPEN` | aot | — | `--gc-stats` reports `live`/`top`, never capacity | [→](docs/roadmap-details.md#gap-q-2) |

### Gap R — measured on three engines, one row each

The largest family: defects found by running a construct on the interpreter, the compiled
binary and CPython instead of reasoning about it. Narrative in the
[record](docs/roadmap-details.md#gap-r). Rows `R.19`, `R.22`, `R.24`, `R.25`, `R.26`,
`R.27`, `R.28`, `R.29` are cited by `pkg/lang` comments, the ledger and the ADRs but had no
row before this tabulation — they are recorded here so every cited ID resolves.

| Gap | Item | Status | Path | ADR | Evidence | Record |
|---|---|---|---|---|---|---|
| Gap R.1 | The compiled backend runs a coroutine at the call, not at the `await` | ⏳ `OPEN` — owner: L7.6a | aot | 0195 | `probe_async_eager` | [→](docs/roadmap-details.md#gap-r-1) |
| Gap R.2 | A module that interned without saying so emitted a call to an undefined helper | ✅ `DONE` | aot | 0209 | `runtime_block_emit_test.go`, `string_value_test.go` | [→](docs/roadmap-details.md#gap-r-2-2) |
| Gap R.3 | A parameter was read-only in the compiled backend (assignment was ignored) | ✅ `DONE` | aot | 0196 | `params_test.go`, `param_rebind.gy` | [→](docs/roadmap-details.md#gap-r-3-2) |
| Gap R.3b | `for … in range()` drove iteration through the user's loop variable | ✅ `DONE` | aot | 0196 | `for_int_test.go` | [→](docs/roadmap-details.md#gap-r-3b) |
| Gap R.3c | A parameter rebound to a float, returned as a bare name | ⏳ `OPEN` — owner: L11.6 | aot | 0196 | `probe_float_param_rebind` | [→](docs/roadmap-details.md#gap-r-3c) |
| Gap R.4 | An emitted function name could collide with a host C symbol (`sync`, `exit`, `write`, `time`, `main`) | ✅ `DONE` | aot | 0198 | `host_symbol_names.gy`, `host_symbols_test.go` | [→](docs/roadmap-details.md#gap-r-4-2) |
| Gap R.5 | A `def` below the code that uses it was refused as an undefined name | ✅ `DONE` | checker | 0197 | `forward_defs.gy`, `forward_defs_test.go` | [→](docs/roadmap-details.md#gap-r-5-2) |
| Gap R.6 | A built-in name shadowed by a `def` was read through the built-in | ✅ `DONE` | both | 0199 | `shadowed_builtins.gy`, `shadowed_builtins_test.go`, `floatfn.gy` | [→](docs/roadmap-details.md#gap-r-6-2) |
| Gap R.7 | One source line could report the same diagnostic two or three times | ✅ `DONE` | checker | 0202 | `diag_dedupe_test.go` | [→](docs/roadmap-details.md#gap-r-7-2) |
| Gap R.8 | A module function and a method of one name shared the checker's key | ✅ `DONE` | checker | 0200 | `method_function_name_clash.gy`, `method_function_key_test.go` | [→](docs/roadmap-details.md#gap-r-8-2) |
| Gap R.9 | `def print` / `def range` did not parse (the names were lexer keywords) | ✅ `DONE` | both | 0203 | `builtin_name_tokens_test.go` | [→](docs/roadmap-details.md#gap-r-9-2) |
| Gap R.10 | Too few arguments was reported by nobody | ✅ `DONE` | checker | 0201 | `arity_test.go`, `arity_defaults.gy` | [→](docs/roadmap-details.md#gap-r-10-2) |
| Gap R.11 | A non-default parameter after a defaulted one | 🚫 `NOT A DEFECT` — the language has no such rule; a call that leaves a parameter unfilled is refused by name | checker | 0206 | `param_order_test.go`, `param_default_order.gy` | [→](docs/roadmap-details.md#gap-r-11-2) |
| Gap R.12 | A program-defined built-in name was visible to codegen *above* its definition | ✅ `DONE` | both | 0205 | `predeclared_shadow_test.go` | [→](docs/roadmap-details.md#gap-r-12) |
| Gap R.13 | A file's final bare expression statement was echoed by the interpreter only | ✅ `DONE` | cli | 0204 | `TestCLIEvalDoesNotEchoVoid` | [→](docs/roadmap-details.md#gap-r-13-2) |
| Gap R.14 | `for x in 5` iterated on both backends, undocumented | 🚫 `NOT A DEFECT` — declared feature: an integer iterable is a repeat count | both | 0207 | `for_int_count.gy`, `for_int_test.go` | [→](docs/roadmap-details.md#gap-r-14-2) |
| Gap R.15 | Iterating a string literal emitted a module `llc` rejects | ✅ `DONE` | aot | 0208 | `for_string_chars.gy`, `for_string_test.go` | [→](docs/roadmap-details.md#gap-r-15-2) |
| Gap R.16 | Iterating a string computed at run time is AOT-unsupported (now a refusal, not bad IR) | ⏳ `OPEN` — owner: L11.5 | aot | 0210 | the refusal names the working path (literals iterate, variables do not) | [→](docs/roadmap-details.md#gap-r-16) |
| Gap R.17 | An uncaught exception exited 0 through `--aot` | ✅ `DONE` | cli | 0211 | `uncaught_exception_test.go`, `TestCLIExitCodeContract` | [→](docs/roadmap-details.md#gap-r-17) |
| Gap R.18 | Division by zero answered `inf` instead of raising | ✅ `DONE` | both | 0212 | `zero_division_test.go` | [→](docs/roadmap-details.md#gap-r-18) |
| Gap R.19 | A missing attribute answers `0` in the compiled backend instead of raising `AttributeError` | ⏳ `OPEN` | aot | 0214 | `integration/builtin_trap_classes_test.go` (self-deleting when it traps), the `probe_builtin_traps_untyped` pin | [→](docs/roadmap-details.md#gap-r-25) |
| Gap R.20 | The compiled backend dispatched only the first `except` arm | ✅ `DONE` | aot | 0213 | `except_arm_order.gy`, `except_dispatch_test.go` | [→](docs/roadmap-details.md#gap-r-20) |
| Gap R.21 | "A `return` inside `try:` loses its value" — the reading was wrong; the compiled half (`@exn_flag` never cleared on an accepting edge) was fixed under 0218 | ✅ `DONE` | aot | 0218 | `exception_clear_test.go`, `method_exceptions_test.go` | [→](docs/roadmap-details.md#gap-r-21-2) |
| Gap R.22 | Returns of differing types share one lowering (the real defect behind the R.21 reading) | ⏳ `OPEN` | aot | 0213, 0221 | `probe_mixed_return_value` pin | [→](docs/roadmap-details.md#gap-r-21) |
| Gap R.23 | `finally` did not run when the body returned, broke, continued, or raised | ✅ `DONE` | both | 0222 | `deferred_bodies_test.go`, `deferred_bodies.gy` | [→](docs/roadmap-details.md#gap-r-23) |
| Gap R.24 | A name bound inside a compound statement belongs to the enclosing scope (the checker refused legal programs) | ✅ `DONE` | checker | 0217 | `scoping_test.go`, `compound_scoping.gy` | [→](docs/roadmap-details.md#gap-r-24) |
| Gap R.25 | A built-in trap carries the exception class its handler matches on | 🟨 `PARTIAL` — interpreter done (0214); the compiled half stays pinned | both | 0212, 0214 | `builtin_trap_classes_test.go`, `probe_builtin_traps_untyped` | [→](docs/roadmap-details.md#gap-r-25) |
| Gap R.26 | An operator applied to operands it cannot apply used to answer a number (interpreter operand gate) | ✅ `DONE` | interp | 0215 | `operator_operand_test.go`, `probe_operand_types` | [→](docs/roadmap-details.md#gap-r-26) |
| Gap R.27 | The compiled backend has no operand-kind check (the AOT mirror of R.26) | ⏳ `OPEN` | aot | 0215 | `integration/operator_operand_test.go` (self-deleting when `1 + None` traps) | [→](docs/roadmap-details.md#gap-r-27) |
| Gap R.28 | `//` and `%` followed Go's truncation in the interpreter | ✅ `DONE` | both | 0216 | `floor_division_test.go` (unit + integration) | [→](docs/roadmap-details.md#gap-r-28) |
| Gap R.29 | `==` between an int and a float is one question in either order | ✅ `DONE` | both | 0221 | `number_equality_test.go`, `numeric_equality.gy` | [→](docs/roadmap-details.md#gap-r-29) |
| Gap R.30 | The compiled backend truncated `//` toward zero | ✅ `DONE` | aot | 0216 | `floor_division_test.go` | [→](docs/roadmap-details.md#gap-r-30) |
| Gap R.31 | `%` on a string is not formatting | ⏳ `PLANNED` | both | 0215 | `probe_percent_format` | [→](docs/roadmap-details.md#gap-r-31) |
| Gap R.33 | The compiled backend cannot lower sequence operations | ⏳ `OPEN` | aot | 0215 | `sequence_ops.gy` debt row + `TestCompiledSequenceOpsNeverAnswerWrong` | [→](docs/roadmap-details.md#gap-r-33) |
| Gap R.35 | A function cannot read a module-level name | 🟨 `PARTIAL` — closed for scalars (0220 + 0227); containers and floats still refuse | both | 0220, 0227 | `module_scope_test.go`, `module_scope_in_functions.gy`, `module_calltime_lookup.gy`, `probe_float_container_equality` | [→](docs/roadmap-details.md#gap-r-35) |
| Gap R.36 | An unwritten variable slot read as raw memory instead of raising | ✅ `DONE` | both | 0228 | `unwritten_slot_test.go`, `unwritten_slot_trap.gy` | [→](docs/roadmap-details.md#gap-r-36) |
| Gap R.37 | A constant operation that should trap is refused at compile time | ⏳ `OPEN` — one instance closed by 0228 | aot | 0211, 0228 | the four R.36 loop shapes that were this bug in a costume | [→](docs/roadmap-details.md#gap-r-37) |
| Gap R.38 | A refusal message claims something false about the other backend | ⏳ `OPEN` | cli | 0166 | the R.38 table in the record (each row measured on the other leg) | [→](docs/roadmap-details.md#gap-r-38) |
| Gap R.39 | Reading a name the body also assigns below should be `UnboundLocalError` | ✅ `DONE` | both | 0228 | `unwritten_slot_test.go` | [→](docs/roadmap-details.md#gap-r-39) |
| Gap R.40 | A container slot is a word: ask what fits before writing it | ✅ `DONE` | aot | 0226 | `kind_mismatch_equality.gy`, `container_element_test.go` | [→](docs/roadmap-details.md#gap-r-40) |
| Gap R.41 | A method is a call like any other: own unwind, checked call sites | ✅ `DONE` | aot | 0223 | `method_unwind_test.go`, `method_try.gy` | [→](docs/roadmap-details.md#gap-r-41) |
| Gap R.42 | A string value is an `@str_tab` index, not the address of a literal | ✅ `DONE` | aot | 0224 | `string_value_test.go`, `string_values.gy`, `str_loop_eq.gy`, `comp_str_filter.gy` | [→](docs/roadmap-details.md#gap-r-42) |
| Gap R.45 | A subscript of a string is a one-character string | ✅ `DONE` | both | 0225 | `string_subscript_test.go`, `string_subscript.gy` | [→](docs/roadmap-details.md#gap-r-45) |
| Gap R.46 | Printing an element of a freshly built comprehension list prints its index | ⏳ `OPEN` | aot | 0187 | the comprehension-element rows in `container_element_test.go` | [→](docs/roadmap-details.md#gap-r-46) |
| Gap R.47 | A compiled string is a compile-time value only (no run-time reads, no run-time construction) | ✅ `DONE` — no new representation: reads and writes go through the interned table | aot | 0229, 0230 | `runtime_string_ops.gy`, `runtime_string_writes.gy` | [→](docs/roadmap-details.md#gap-r-47) |
| Gap R.48 | There is no `global` statement (all three engines differ) | ⏳ `PLANNED` | both | 0227, 0228 | `probe_global_statement` | [→](docs/roadmap-details.md#gap-r-48) |
| Gap R.49 | A module variable whose name was also a parameter of an earlier `def` gets no slot: `store i32 %t5, i32* %_x` → `llc` rejects the module (exit 2, an ordinary program blamed on the compiler); renaming either binding compiles | ⏳ `OPEN` — found by the 2026-10-01 sweep | aot | 0163, 0211, 0228 | measured repro in the record | [→](docs/roadmap-details.md#gap-r-49) |
| Gap R.50 | `round` ties away from zero where CPython ties to even (`round(2.5)` = 3, Python 2) — identical on both backends, so parity cannot see it | ⏳ `OPEN` — found by the 2026-10-01 sweep; owner: L11.6 | both | 0104 | measured repro in the record (needs an oracle row, not a parity row) | [→](docs/roadmap-details.md#gap-r-50) |
| Gap R.51 | `floor` / `ceil` / `sqrt`: the checker predeclares them, the interpreter traps `NameError` (exit 3), the compiler answers — with a float where Python returns an `int` | ⏳ `OPEN` — found by the 2026-10-01 sweep; owner: L11.6 | both | 0166 | `gustyc check` says `ok`, `--interp` traps, `--aot` prints `3.0` | [→](docs/roadmap-details.md#gap-r-51) |
| Gap R.52 | stdlib discovery walks up from the **cwd**, so an installed `gustyc` cannot `import math` unless `--stdlib` / `$GUSTY_STDLIB_DIR` is set | ⏳ `OPEN` — found by the 2026-10-01 sweep; owner: L9.2 (the install story) | cli | — | `import math` fails outside a directory under a repo with `stdlib/`, succeeds inside one | [→](docs/roadmap-details.md#gap-r-52) |

## Recently closed (most recent first)

| ID / Gap | Closed by | Artifact |
|---|---|---|
| L11.1 (mixed dicts/sets + tagged lookup) | ADR 0232 | `pkg/lang/mixed_dict_set_test.go`, `integration/mixed_container_test.go` |
| L8.5 (line tables in the module) | ADR 0231 | `pkg/lang/debug.go`, `--debug-info`, `--build --debug` |
| Gap R.47 (run-time strings) | ADR 0230 + 0229 | `runtime_string_ops.gy`, `runtime_string_writes.gy` |
| Gap R.36 + R.39 (unwritten slot) | ADR 0228 | `unwritten_slot_trap.gy` |
| Gap R.35 scalars (module bindings) | ADR 0227 | `module_calltime_lookup.gy` |
| Gap R.40 (container slot is a word) | ADR 0226 | `kind_mismatch_equality.gy` |
| Gap R.45 (string subscript) | ADR 0225 | `string_subscript.gy` |
| Gap R.42 (string is an index) | ADR 0224 | `string_values.gy`, `str_loop_eq.gy` |
| Gap R.41 (a method is a call) | ADR 0223 | `method_try.gy` |
| Gap R.23 (`finally` on every exit) | ADR 0222 | `deferred_bodies.gy` |

## Definition of done per item

An item is `✅ DONE` when it ships:

- a unit test in `pkg/lang/` exercising the behaviour (and the failure mode — a stub that
  breaks the mechanism must make the test fail);
- an `integration/` whole-program compile-and-run case where applicable, asserting
  interpreter / AOT / JIT parity **and** the CPython verdict;
- a `docs/adr/` entry for any non-obvious decision (or a renumbering of an existing one);
- an update to `docs/language.md` and `docs/operations.md` when it changes user-visible
  syntax, flags, schemas or exit codes;
- a row in this file whose `Status`, `Evidence` and `Record` cells tell the truth —
  including the ledger row being *deleted* when the row was a debt.

## Definition of done for gap-shaped work

A gap is `✅ DONE` when the previously interpreter-only path also lowers on AOT (or the
both-backend parity program passes), no `interpreter-only` / `not lowered` comment remains
in `codegen.go` for it, and its pin is gone from `integration/conformance_cases.go`. A
`🟨 PARTIAL` row must name the remainder in its own cell, and that remainder appears once
in the [queue](#open-queue--the-working-list).

## Sequencing note

Gaps A–I are closed and Phases 3–10 are largely closed; the **current** work is Phase 11 —
the value model. L11.9 landed first (ADR 0186) because it is the harness that proves the
rest; L11.1 has taken its steps for lists, dicts and sets (ADRs 0182 / 0184 / 0185 / 0187 /
0189 / 0232), so the queue is now the **25 pinned `debt` rows**: floats and nested
containers inside elements, bools as values (L11.2), tuples (L11.3), code-point strings
(L11.5), numerics (L11.6), functions as values (L11.7), then the refusal diagnostics
(L11.8). L7.2 / L7.3 / L8.1 / L8.3 all read the tag — they follow L11.1. The still-open
gap-shaped items (Gap J.2, Gap K.8's frame stack, Gap M.2's default flip, Gap L.5, Gaps
N.2 / P.1 / P.2, Gaps Q.1 / Q.2 and the open `Gap R.*` rows) are absorbed by Phase 11 where
they are representation decisions and stay their own work where they are not; `--file`
flips to the compiled leg only once the corpus is green through it, which since L11.9 is a
measured claim rather than an assumption.
