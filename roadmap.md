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
| Version constant | `pkg/lang/compile.go` | **drifted**: says `0.1.0`, CHANGELOG says v0.9.0 |
| Unit tests | `pkg/lang/*_test.go` | green |
| Whole-program tests | `integration/` (CLI compile-and-run) | green |

## Roadmap — phased plan

| Phase | Item | Status | Details |
|---|---|---|---|
| Phase 0 — hygiene | Version reconciled (v0.10.0) | ✅ DONE | The single version constant lives in `pkg/lang/compile.go` and is printed by `cmd/gustyc`. `CHANGELOG.md` is bumped and records this roadmap entry. |
| Phase 1 — AOT/interpreter parity | Floats in AOT | ✅ DONE | ~~today codegen truncates `FloatLit` to `int64`~~ codegen emits real `double` IR — float literals (`fadd double 0.0, <const>`), `fadd/fsub/fmul/fdiv` arithmetic with `sitofp` int promotion, `%.17g` float print (matches interpreter `%v`), `fcmp` float comparisons, and `double` allocas/stores for float variables tracked via `floatVars`. `float()` folds int/string literals to double constants. |
| Phase 1 — AOT/interpreter parity | Runtime heap + GC in AOT (milestone 1: runtime heap + boxed mutable lists with `list.append()` on variables and `print(list)` — done, ADR 0009; milestone 2: `len(x)` on runtime list vars — done; milestone 3: `x[i]` index read — done; milestone 4: slot reuse via a free-list on rebinding — done; milestone 5: free a list slot when rebound to a non-list — done; milestone 6: variable-index list reads `x[a]` — done; milestone 7: runtime heap dicts — done; milestone 8: runtime heap sets — done; milestone 9: cross-collection rebind free — done; milestone 10: heap-stress/leak harness — done; milestone 11: heap bounds safety (rt_alloc returns -1 sentinel instead of out-of-bounds write when full) — done; full mark-and-sweep GC and runtime dict/set objects are follow-on) | ⏳ PLANNED | Today lists/dicts/sets/strings are compile-time globals only. Introduce an object heap, boxed values, and a refcount/GC pass so AOT programs can mutate runtime collections. Mirror the interpreter's `Collect()` contract. |
| Phase 1 — AOT/interpreter parity | Dynamic dispatch + method tables | ⏳ PLANNED | Classes/inheritance/`super`, `import`, `try`/`except`/`finally`, `raise`, `yield` are interpreter-only. Land each in AOT in this order: exceptions → modules/imports → classes → generators. Record each in `docs/adr/` and update `docs/language.md`. |
| Phase 1 — AOT/interpreter parity | Arbitrary decorators | ⏳ PLANNED | Codegen today emits only the identity form. Generalize to `f = dec(f)` lowering. |
| Phase 2 — LLVM core deepening | Real optimizer | ✅ DONE | `opt.go` now parses the emitted IR into a module/CFG and runs a real pass pipeline to a fixed point: constant propagation + folding, mem2reg-style alloca promotion, dead-instruction elimination, and dead-block (unreachable CFG block) elimination. The re-serialized output is verified end-to-end through `llvm-as`/`llc` in `TestOptimizedIRValidForLLC` (ADR 0088). |
| Phase 2 — LLVM core deepening | JIT for REPL feedback | ⏳ PLANNED | Mission: "fast REPL feedback despite AOT". Compile the textual IR with `llc`/`cc` per expression and `dlopen`-execute, or add a small in-process JIT path — without importing external bindings. The REPL currently uses the interpreter; a real JIT is the next step. |
| Phase 2 — LLVM core deepening | Runtime dispatch | ⏳ PLANNED | Add a `%obj`-tagged value representation so AOT and interpreter agree on the dynamic type model (`docs/agentic/ast-ir-schema.md`). |
| Phase 3 — memory model & optimization | Document the memory model | ⏳ PLANNED | `docs/adr/0090-memory-model.md`: heap ownership, refcount vs mark-and-sweep, stack-vs-heap escape analysis in AOT. |
| Phase 3 — memory model & optimization | Escape analysis + scalar replacement | ⏳ PLANNED | So closures/env-stores don't force heap allocation; keep the interpreter's GC as the fallback. |
| Phase 4 — docs & verification | Keep docs current | 🔄 CONTINUOUS | Keep `docs/language.md`, `docs/operations.md`, `docs/roadmap.md`, `docs/adr/` current after every change. |
| Phase 4 — docs & verification | Every AOT feature ships tests | 🔄 CONTINUOUS | Every AOT feature ships a unit test + an `integration/` whole-program compile-and-run test. |
| Phase 5 — modern GC & dead-object elimination | Generational tracing GC (interpreter) | ⏳ PLANNED | Today `jit.go`'s `Collect()` is a *conservative* mark-and-sweep that frees only pure-data objects (list/dict/set/str/int/float) and **never** classes/methods/closures/imports/modules. Upgrade to a modern generational tracing GC: a young-object nursery, a tenured space, and incremental/safepoint-driven collection so long-running programs reclaim *all* unreachable objects (including closures/classes) without a stop-the-world pause. |
| Phase 5 — modern GC & dead-object elimination | AOT runtime tracing GC | ⏳ PLANNED | The AOT path has no runtime heap/GC yet (beyond the Phase 1 boxed-value heap). Add a *tracing* collector — roots are AOT globals / env-stores / stack slots; mark walks container fields + closure envs; sweep unreachable heap objects — so AOT programs get leak-free mutation of runtime collections, mirroring `Collect()` semantics. |
| Phase 5 — modern GC & dead-object elimination | Escape-analysis heap elision (modern dead-object elimination) | ⏳ PLANNED | Replace the pure-Go textual dead-global elimination in `opt.go` with an IR-level liveness + escape-analysis pass that eliminates **whole dead heap objects** — not just dead instructions/blocks — proving an allocation never escapes (no `%obj` handle, closure env, or method table escapes) and deleting it before codegen. |
| Phase 5 — modern GC & dead-object elimination | GC stress / leak harness | ⏳ PLANNED | Extend `memory_test.go` with stress cases: rebind loops, generator yield-lists, closure envs, and class attr cycles — asserting the collector reclaims unreachable objects and keeps live ones, with parity across both the interpreter and AOT paths. |

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
