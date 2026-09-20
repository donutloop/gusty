# Pyre — Roadmap (LLVM-core Python-like language)

This file is the living, concrete plan for building and evolving **Pyre** (the
`gusty` repo), a Python-like language whose core is compiled ahead-of-time
through LLVM. It lives next to `AGENTS.md` and is the single source of truth
for *what exists* and *what is next*.

## Documentation home (always keep this in sync)

The agent maintains **rich documentation under `docs/`** — this is the canonical
doc home:

- `docs/language.md` — single source of truth for the language surface
  (syntax, semantics, which constructs are interpreter-only vs AOT).
- `docs/operations.md` — CLI, agent operations, build/test pipeline.
- `docs/adr/` — architecture decision records (`0001`..`0128`); each new
  decision is recorded there.
- `docs/agentic/` — agent-facing notes (currently `ast-ir-schema.md`).
- `docs/roadmap.md` — pointer mirror of this `roadmap.md`.

Rule: every component change updates the matching doc; never let
`docs/language.md` and `roadmap.md` drift apart.

## Component map (state verified against the code)

| Component | File(s) | State |
|---|---|---|
| Lexer (INDENT/DEDENT) | `pkg/lang/lexer.go` | done |
| Parser → AST | `pkg/lang/parser.go`, `ast.go`, `token.go`, `types.go` | done |
| Semantic analysis / gradual typing | `pkg/lang/semantic.go` | static checks only |
| Interpreter backend + heap GC | `pkg/lang/jit.go` | full dynamic surface |
| AOT codegen (textual IR) | `pkg/lang/codegen.go`, `closure.go` | i32-only, see gaps |
| Optimizer | `pkg/lang/opt.go` | pure-Go textual dead-global elim. (not an LLVM `opt` pass) |
| Multi-file build | `pkg/lang/build.go` | done |
| CLI | `cmd/gustyc/main.go` | parse → semantic → (eval \| codegen → `llc` → `cc`) |
| Version constant | `pkg/lang/compile.go` | reconciled at `0.10.0` (compile.go), CHANGELOG at v0.10.32 (verified) |
| Unit tests | `pkg/lang/*_test.go` | green |
| Whole-program tests | `integration/` (CLI compile-and-run) | green |

## Roadmap — phased plan

| Phase | Item | Status | Details |
|---|---|---|---|
| Phase 0 — hygiene | Version reconciled (v0.10.0) | ✅ DONE | The single version constant lives in `pkg/lang/compile.go` and is printed by `cmd/gustyc`. `CHANGELOG.md` is bumped and records this roadmap entry. |
| Phase 1 — AOT/interpreter parity | Floats in AOT | ✅ DONE | ~~today codegen truncates `FloatLit` to `int64`~~ codegen emits real `double` IR — float literals (`fadd double 0.0, <const>`), `fadd/fsub/fmul/fdiv` arithmetic with `sitofp` int promotion, `%.17g` float print (matches interpreter `%v`), `fcmp` float comparisons, and `double` allocas/stores for float variables tracked via `floatVars`. `float()` folds int/string literals to double constants. |
| Phase 1 — AOT/interpreter parity | Runtime heap + GC in AOT (milestone 1: runtime heap + boxed mutable lists with `list.append()` on variables and `print(list)` — done, ADR 0009; milestone 2: `len(x)` on runtime list vars — done; milestone 3: `x[i]` index read — done; milestone 4: slot reuse via a free-list on rebinding — done; milestone 5: free a list slot when rebound to a non-list — done; milestone 6: variable-index list reads `x[a]` — done; milestone 7: runtime heap dicts — done; milestone 8: runtime heap sets — done; milestone 9: cross-collection rebind free — done; milestone 10: heap-stress/leak harness — done; milestone 11: heap bounds safety (rt_alloc returns -1 sentinel instead of out-of-bounds write when full) — done; conservative mark-and-sweep GC landed; closure env slots are now rooted so captured envs survive top-level GC boundaries) | ✅ DONE | Today lists/dicts/sets/strings are compile-time globals only. Introduce an object heap, boxed values, and a refcount/GC pass so AOT programs can mutate runtime collections. Mirror the interpreter's `Collect()` contract. |
| Phase 1 — AOT/interpreter parity | Dynamic dispatch + method tables | 🟠 PARTIAL — statement-level dynamic dispatch landed (ADR 0136); remaining: all-call-site dispatch, devirtualization, module fn dispatch | Classes/inheritance/`super`, `import`, `try`/`except`/`finally`, `raise`, `yield` are interpreter-only. Land each in AOT in this order: exceptions → modules/imports → classes → generators. **classes (definitions, instantiation, inheritance, `super()`) are now landed in AOT codegen (static-dispatch model)**; generators (`yield` statements and generator expressions) are now landed too. Record each in `docs/adr/` and update `docs/language.md`. |
| Phase 1 — AOT/interpreter parity | Arbitrary decorators | ✅ LANDED (identity + clear rejection) | Decorator application machinery in AOT (ADR 0131): identity decorators and source-order resolution work; wrapping/closure decorators are rejected with a clear codegen error instead of silently ignored. Full fnptr-valued decorators are a follow-on (needs fnptr operands/indirect calls). |
| Phase 2 — LLVM core deepening | Real optimizer | ✅ DONE | `opt.go` now parses the emitted IR into a module/CFG and runs a real pass pipeline to a fixed point: constant propagation + folding, mem2reg-style alloca promotion, dead-instruction elimination, and dead-block (unreachable CFG block) elimination. The re-serialized output is verified end-to-end through `llvm-as`/`llc` in `TestOptimizedIRValidForLLC` (ADR 0088). |
| Phase 2 — LLVM core deepening | JIT for REPL feedback | ⏳ PLANNED | Mission: "fast REPL feedback despite AOT". Compile the textual IR with `llc`/`cc` per expression and `dlopen`-execute, or add a small in-process JIT path — without importing external bindings. The REPL currently uses the interpreter; a real JIT is the next step. |
| Phase 2 — LLVM core deepening | Runtime dispatch | ⏳ PLANNED | Add a `%obj`-tagged value representation so AOT and interpreter agree on the dynamic type model (`docs/agentic/ast-ir-schema.md`). |
| Phase 3 — memory model & optimization | Document the memory model | ✅ DONE (ADR 0135) | `docs/adr/0135-memory-model.md`: AOT fixed heap + free-list + rebind-free + escape-analysis elision (ADR 0134); interpreter generational GC (ADR 0132); precise stack roots follow-on; stress harness (ADR 0133); safe GC collection points.
| Phase 3 — memory model & optimization | Escape analysis + scalar replacement | 🟠 PARTIAL (ADR 0134: dead top-level list elision; scalar replacement follow-on) | So closures/env-stores don't force heap allocation; keep the interpreter's GC as the fallback. |
| Phase 4 — docs & verification | Keep docs current | 🔄 CONTINUOUS | Keep `docs/language.md`, `docs/operations.md`, `docs/roadmap.md`, `docs/adr/` current after every change. |
| Phase 4 — docs & verification | Every AOT feature ships tests | 🔄 CONTINUOUS | Every AOT feature ships a unit test + an `integration/` whole-program compile-and-run test. |
| Phase 5 — modern GC & dead-object elimination | Generational tracing GC (interpreter) | ✅ LANDED (two-generation nursery, ADR 0132; full-GC threshold bounds old-gen) | Today `jit.go`'s `Collect()` is a *conservative* mark-and-sweep that frees only pure-data objects (list/dict/set/str/int/float) and **never** classes/methods/closures/imports/modules. Upgrade to a modern generational tracing GC: a young-object nursery, a tenured space, and incremental/safepoint-driven collection so long-running programs reclaim *all* unreachable objects (including closures/classes) without a stop-the-world pause. |
| Phase 5 — modern GC & dead-object elimination | AOT runtime tracing GC | 🟠 IN PROGRESS — conservative mark-sweep landed; env slots rooted | A conservative mark-and-sweep GC landed (d1141c7); closure env slots are now registered as roots. Remaining: root function-local allocas / stack slots and walk closure env fields so every reachable heap object survives GC. |
| Phase 5 — modern GC & dead-object elimination | Escape-analysis heap elision (modern dead-object elimination) | ⏳ PLANNED | Replace the pure-Go textual dead-global elimination in `opt.go` with an IR-level liveness + escape-analysis pass that eliminates **whole dead heap objects** — not just dead instructions/blocks — proving an allocation never escapes (no `%obj` handle, closure env, or method table escapes) and deleting it before codegen. |
| Phase 5 — modern GC & dead-object elimination | GC stress / leak harness | ✅ LANDED (TestGCStressBoundedHeap + TestGCFullGCBoundsOldGen, ADR 0133) | Extend `memory_test.go` with stress cases: rebind loops, generator yield-lists, closure envs, and class attr cycles — asserting the collector reclaims unreachable objects and keeps live ones, with parity across both the interpreter and AOT paths. |
| Phase 6 — language-surface parity | f-strings / string interpolation | ⏳ PLANNED | Python's signature ergonomic feature; absent in lexer/parser/AST and both backends. Add `f"..."`/`f'...'` with `{}` interpolation. |
| Phase 6 — language-surface parity | Slicing | ⏳ PLANNED | `s[a:b]`, `s[::step]`, negative indices for str/list/dict; absent (`:` today only in dict literals, annotations, lambdas). |
| Phase 6 — language-surface parity | Augmented assignment | ⏳ PLANNED | `+= -= *= /= //= %=`; absent today. |
| Phase 6 — language-surface parity | Tuple unpacking / multi-assign | ⏳ PLANNED | `a, b = b, a`, `for a, b in ...`; absent today. |
| Phase 6 — language-surface parity | Membership + identity ops | ⏳ PLANNED | `in`/`not in` (list/dict/set) and `is`/`is not`; only `for ... in` exists today. |
| Phase 6 — language-surface parity | Power `**` | 🟠 PARTIAL | Lexed but never wired as a binop; add `math.Pow` lowering in both backends. |
| Phase 6 — language-surface parity | Pattern-match depth | ⏳ PLANNED | Guards (`case x if cond:`), or-patterns, dict/class patterns; today only integer-equality + list-destructuring. |
| Phase 6 — language-surface parity | Operator overloading (dunder) | ⏳ PLANNED | `__add__`, `__getitem__`, ...; prerequisite for `with` and idiomatic classes. |
| Phase 6 — language-surface parity | `with` / context managers, `yield from` | ⏳ PLANNED | Natural follow-ons to exceptions and generators. |
| Phase 7 — correctness & tooling | Shared IR / conformance matrix | ⏳ PLANNED | Interpreter and codegen each consume the AST independently today — no shared IR, so semantics drift (e.g., dispatch is statement-level in AOT only). Add a shared lowering spec + a matrix that runs every `integration/` case through **both** backends and diffs. |
| Phase 7 — correctness & tooling | REPL error recovery | ⏳ PLANNED | `--repl` aborts on a single parse error; add panic-recovery/error-token parsing for a modern REPL. |
| Phase 7 — correctness & tooling | Richer `--json` diagnostics | ⏳ PLANNED | Emit spans **and** inferred types (the semantic pass already computes them) in the `--json` schema. |

| Phase | Item | Status | Details |
|---|---|---|---|
| Phase 8 — developer tooling & ecosystem | Formatter (`gusty fmt`) | ⏳ PLANNED | `cmd/` ships only the compiler; add a gofmt/black-style source formatter so the language has a canonical style. |
| Phase 8 — developer tooling & ecosystem | Language server / LSP | ⏳ PLANNED | Editors get completion + hover + diagnostics; build on the `--json` span/type groundwork (semantic already infers types). |
| Phase 8 — developer tooling & ecosystem | Standard library + package resolution | ⏳ PLANNED | No stdlib dir today; AOT `import` is data-only. Add `math`/`string`/`collections`/`json`-style stdlib and on-disk module/package resolution. |
| Phase 8 — developer tooling & ecosystem | Runtime tracebacks with source spans | ⏳ PLANNED | Parser errors carry spans today, but runtime errors have no Python-style traceback with line/col; add one in the interpreter and AOT. |
| Phase 8 — developer tooling & ecosystem | Docstrings / `__doc__` | ⏳ PLANNED | Absent; add `def`/`class` docstrings and `__doc__` introspection. |
| Phase 8 — developer tooling & ecosystem | Benchmark + profiling suite | ⏳ PLANNED | No `Benchmark` tests today; add a perf harness that runs both backends (supports the "compiler is the product" principle). |
| Phase 9 — type system & runtime robustness | Tuple type + tuple unpacking | ⏳ PLANNED | Semantic has no `Tuple` kind; add it as the type-system underpinning for Phase 6 unpacking and multi-return. |
| Phase 9 — type system & runtime robustness | Generics / protocols | ⏳ PLANNED | Gradual types stop at `Any`; add `Sequence[T]`/`Callable` bounds and structural protocols. |
| Phase 9 — type system & runtime robustness | Standalone type-check mode (`gusty check`) | ⏳ PLANNED | Expose the semantic pass as a `mypy`-style checker that accepts annotated code without executing it. |
| Phase 9 — type system & runtime robustness | Debug symbols / source maps for AOT binaries | ⏳ PLANNED | `--build` executables need line/col + variable info for real stack traces and debuggers. |
| Phase 9 — type system & runtime robustness | FFI / C interop + embedding API | ⏳ PLANNED | Systems-language parity: call C from `gusty` and embed the interpreter/codegen as a library. |
| Phase 9 — type system & runtime robustness | Fuzz/property-based testing of both backends | ⏳ PLANNED | Given the two-backend drift risk, add `go-fuzz`/property tests over the AST → interpreter/codegen. |

## Definition of done per item
- Interpreter feature + unit test.
- AOT emitter feature + unit test.
- `integration/` whole-program compile-and-run (through `llc`/`cc`).
- `docs/language.md` + `docs/adr/` updated; `CHANGELOG.md` bumped.

## Quick build/test commands
```
go build ./...            # compile compiler + CLI
go test ./pkg/lang/       # unit tests (interpreter + codegen)
go test ./integration/    # whole-program compile-and-run
```
