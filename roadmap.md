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
| Phase 2 — LLVM core deepening | JIT for REPL feedback | ✅ DONE | `pkg/lang/jit_llvm.go`: source → codegen → `llc` → `cc -shared` → `dlopen` into-process → dlsym `main` → run with fd 1 captured. `gustyc --jit` threads it through `--eval`/`--repl`; `TestJIT*` + `TestCLIJIT*` cover it. |
| Phase 2 — LLVM core deepening | Runtime dispatch | ✅ DONE (Round 12) | `%obj`-tagged value representation (`%obj = {i32 tag, i32 payload}`) + canonical kind-tag table (`pkg/lang/value.go`) shared by AOT IR and interpreter heap; helpers `rt_mkobj`/`rt_obj_tag`/`rt_obj_payload`/`rt_obj_is`; dispatch wraps+tag-checks receivers; documented (ast-ir-schema.md, ADR 0142). |
| Phase 3 — memory model & optimization | Document the memory model | ✅ DONE (ADR 0135) | `docs/adr/0135-memory-model.md`: AOT fixed heap + free-list + rebind-free + escape-analysis elision (ADR 0134); interpreter generational GC (ADR 0132); precise stack roots follow-on; stress harness (ADR 0133); safe GC collection points.
| Phase 3 — memory model & optimization | Escape analysis + scalar replacement | 🟠 PARTIAL (ADR 0134: dead top-level list elision; scalar replacement follow-on) | So closures/env-stores don't force heap allocation; keep the interpreter's GC as the fallback. |
| Phase 4 — docs & verification | Keep docs current | 🔄 CONTINUOUS | Keep `docs/language.md`, `docs/operations.md`, `docs/roadmap.md`, `docs/adr/` current after every change. |
| Phase 4 — docs & verification | Every AOT feature ships tests | 🔄 CONTINUOUS | Every AOT feature ships a unit test + an `integration/` whole-program compile-and-run test. |
| Phase 5 — modern GC & dead-object elimination | Generational tracing GC (interpreter) | ✅ LANDED (two-generation nursery, ADR 0132; full-GC threshold bounds old-gen) | Today `jit.go`'s `Collect()` is a *conservative* mark-and-sweep that frees only pure-data objects (list/dict/set/str/int/float) and **never** classes/methods/closures/imports/modules. Upgrade to a modern generational tracing GC: a young-object nursery, a tenured space, and incremental/safepoint-driven collection so long-running programs reclaim *all* unreachable objects (including closures/classes) without a stop-the-world pause. |
✅ DONE — GC runs inside function bodies at top-level statement boundaries; params are spilled into rooted stack slots and function-local allocas are registered as GC roots (via gcReg), so live heap objects survive GC while a function executes. The optimizer promote pass treats address-escaped allocas (gc-store roots) as non-promotable, keeping rooting stores valid under `opt`. Runtime tests verify a function with >1024 unreachable allocations completes and a live function-local list survives GC.
| Phase 5 — modern GC & dead-object elimination | Escape-analysis heap elision (modern dead-object elimination) | ✅ DONE (`fn.deadHeapElim` in `opt.go`, ADR 0143) | IR-level liveness + escape-analysis pass over the emitted heap IR: an `rt_alloc`'d object whose handle never escapes the function and is never read/printed/derived (`rt_slice`) is unobservable, so its allocation and **all** of its mutating ops (`rt_set_elem`/`rt_append`/`rt_dict_put`/`rt_set_add`) are eliminated together. A handle escapes if it is stored into another object (value operand), converted via `rt_mkobj`, returned, stored to memory, or used in any non-call instruction. This is the IR-level generalization of the source-level `escape.go` dead-list elision and also catches objects built inside function bodies. |
| Phase 5 — modern GC & dead-object elimination | GC stress / leak harness | ✅ LANDED (TestGCStressBoundedHeap + TestGCFullGCBoundsOldGen, ADR 0133) | Extend `memory_test.go` with stress cases: rebind loops, generator yield-lists, closure envs, and class attr cycles — asserting the collector reclaims unreachable objects and keeps live ones, with parity across both the interpreter and AOT paths. |
| Phase 6 — language-surface parity | f-strings / string interpolation | ✅ DONE | Python's signature ergonomic feature; absent in lexer/parser/AST and both backends. Add `f"..."`/`f'...'` with `{}` interpolation. |
| Phase 6 — language-surface parity | Slicing | ✅ DONE | `s[a:b]`, `s[::step]`, negative indices for str/list/dict; absent (`:` today only in dict literals, annotations, lambdas). |
| Phase 6 — language-surface parity | Augmented assignment | ✅ DONE | `+= -= *= /= //= %=`; absent today. |
| Phase 6 — language-surface parity | Tuple unpacking / multi-assign | ✅ DONE | `a, b = b, a`, `for a, b in ...`; interpreter + codegen tuple assignment. |
| Phase 6 — language-surface parity | Membership + identity ops | ✅ DONE | `in`/`not in` (list/dict/set/str) and `is`/`is not`; interpreter + codegen via runtime `rt_contains`. |
| Phase 6 — language-surface parity | Power `**` | ✅ DONE | Wired as a right-associative binop binding tighter than unary on the left. Interpreter: `math.Pow` for float, exact binary-exponentiation for int. Codegen: exact fold for int literals, `@llvm.pow.f64` (sitofp/fptosi) for non-literal ints, `@llvm.pow.f64` in `floatBinOp`/`floatEval`. Tests: `TestEvalPower`, `TestIRPowerInt`, `TestIRPowerConst`, `TestExecPower`. |
| Phase 6 — language-surface parity | Pattern-match depth | ✅ DONE | Guards (`case x if cond:`), or-patterns (`case 1 | 2:`), dict patterns (`case {"k": v}:`) delivered (ADR 0138); class patterns remain a follow-up |
| Phase 6 — language-surface parity | Operator overloading (dunder) | ✅ DONE (ADR 0139) | `__add__`/`__sub__`/`__mul__`/`__truediv__`/`__floordiv__`/`__mod__`/`__pow__` + comparisons dispatch in the interpreter, with reflected `__r*__` fallback; codegen static-dispatch is an AOT limit. |
| Phase 6 — language-surface parity | `with` / context managers, `yield from` | ✅ DONE | `with` / context managers and `yield from` are implemented (commits fa52b1c). |
| Phase 7 — correctness & tooling | Shared IR / conformance matrix | ✅ DONE | Interpreter and codegen each consume the AST independently today — no shared IR, so semantics drift (e.g., dispatch is statement-level in AOT only). Add a shared lowering spec + a matrix that runs every `integration/` case through **both** backends and diffs. |
| Phase 7 — correctness & tooling | REPL error recovery | ✅ DONE | `--repl` aborts on a single parse error; add panic-recovery/error-token parsing for a modern REPL. |
| Phase 7 — correctness & tooling | Richer `--json` diagnostics | ✅ DONE | Emit spans **and** inferred types (the semantic pass already computes them) in the `--json` schema. |
| Phase 8 — developer tooling & ecosystem | Formatter (`gusty fmt`) | ✅ DONE (Round 8) | `cmd/` ships only the compiler; add a gofmt/black-style source formatter so the language has a canonical style. |
| Phase 8 — developer tooling & ecosystem | Language server / LSP | ✅ DONE | Editors get completion + hover + diagnostics; build on the `--json` span/type groundwork (semantic already infers types). |
| Phase 8 — developer tooling & ecosystem | Standard library + package resolution | ⏳ PLANNED | No stdlib dir today; AOT `import` is data-only. Add `math`/`string`/`collections`/`json`-style stdlib and on-disk module/package resolution. |
| Phase 8 — developer tooling & ecosystem | Runtime tracebacks with source spans | ✅ DONE | Interpreter runtime errors now render a Python-style traceback with line/col call frames (module, function, and method frames, innermost-last), and the CLI emits a structured `traceback` field in JSON output. AOT has no runtime-error infrastructure (errors are compile-time folds), so tracebacks apply to the interpreter runtime path; codegen compile errors already carry source spans. |
| Phase 8 — developer tooling & ecosystem | Docstrings / `__doc__` | ✅ DONE (Round 9, ADR 0141; AOT `__doc__` interpreter-only) | Absent; add `def`/`class` docstrings and `__doc__` introspection. |
| Phase 8 — developer tooling & ecosystem | Benchmark + profiling suite | ⏳ PLANNED | No `Benchmark` tests today; add a perf harness that runs both backends (supports the "compiler is the product" principle). |
| Phase 9 — type system & runtime robustness | Tuple type + tuple unpacking | ✅ DONE (Round 11) | Semantic has no `Tuple` kind; add it as the type-system underpinning for Phase 6 unpacking and multi-return. |
| Phase 9 — type system & runtime robustness | Generics / protocols | ⏳ PLANNED | Gradual types stop at `Any`; add `Sequence[T]`/`Callable` bounds and structural protocols. |
| Phase 9 — type system & runtime robustness | Standalone type-check mode (`gusty check`) | ✅ DONE (Round 13) | `--check <src>` / `gusty check <files>` run the semantic pass (mypy-style) without executing; new arg-vs-annotation and return-vs-`->` checks; `CheckSource/CheckFile/CheckFiles` API; JSON + deterministic exit codes (0/1/2). ADR 0143. |
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
