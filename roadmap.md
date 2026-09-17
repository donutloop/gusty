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
- `docs/adr/` — architecture decision records (`0001`..`0110`); each new
  decision is recorded there.
- `docs/agentic/` — agent-facing notes (currently `ast-ir-schema.md`).
- `docs/roadmap.md` — pointer mirror of this `roadmap.md`.

Rule: every component change updates the matching doc; never let
`docs/language.md` and `roadmap.md` drift apart.

## Core principle

Two execution paths, one language:

1. **Interpreter** (`pkg/lang/jit.go`) — fast feedback, the full dynamic
   surface: closures, decorators, classes + inheritance + `super`, generators
   /`yield`, exceptions (`try`/`except`/`finally`/`raise`), comprehensions,
   imports, string/list/dict methods, float arithmetic. A heap
   (`map[int64]*obj`) with an explicit mark-and-sweep pass (`Collect()`)
   runs before each top-level statement.
2. **LLVM AOT codegen** (`pkg/lang/codegen.go` + `closure.go`) — a
   deterministic **textual LLVM IR emitter** that produces `i32`-only IR,
   verified by the external `llc-20` toolchain and linked by `cc`. This is the
   "LLVM core": the IR is emitted as text and lowered through real LLVM
   (`llc`) — not through in-tree bindings. Closures compile to an env-store
   global (`@env_store`/`@env_count`) plus an `%env` parameter.

AOT limits today (interpreter-only): classes/inheritance/`super`, `import`,
`try`/`except`/`finally`, `raise`, `yield`/generators, arbitrary decorators
(codegen emits the identity form only), and floats are truncated to ints.
Lists/dicts/sets/strings are compile-time constant globals; there is **no
runtime heap, GC, or dynamic dispatch in the AOT path** yet.

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

## Gaps → phased plan

### Phase 0 — hygiene (small, no semantics change)  ✅ DONE
- **Version reconciled (v0.10.0)**: the single version constant lives in
  `pkg/lang/compile.go` and is printed by `cmd/gustyc`. `CHANGELOG.md` is
  bumped and records this roadmap entry.

### Phase 1 — close the AOT/interpreter parity gaps (highest value)
Order matters: each feature lands in the interpreter first, then the AOT
emitter, then docs + integration tests.

1. **Floats in AOT** — ~~today codegen truncates `FloatLit` to `int64`~~ **DONE**:
   the codegen emits real `double` IR — float literals (`fadd double 0.0, <const>`),
   `fadd/fsub/fmul/fdiv` arithmetic with `sitofp` int promotion, `%.17g` float print
   (matches interpreter `%v`), `fcmp` float comparisons, and `double` allocas/stores
   for float variables tracked via `floatVars`. `float()` folds int/string literals
   to double constants.
2. **Runtime heap + GC in AOT** — today lists/dicts/sets/strings are
   compile-time globals only. Introduce an object heap, boxed values, and a
   refcount/GC pass so AOT programs can mutate runtime collections. Mirror
   the interpreter's `Collect()` contract.
3. **Dynamic dispatch + method tables** — classes/inheritance/`super`,
   `import`, `try`/`except`/`finally`, `raise`, `yield` are interpreter-only.
   Land each in AOT in this order: exceptions → modules/imports → classes →
   generators. Record each in `docs/adr/` and update `docs/language.md`.
4. **Arbitrary decorators** — codegen today emits only the identity form.
   Generalize to `f = dec(f)` lowering.

### Phase 2 — LLVM core deepening
- **Real optimizer**: keep the textual emitter but add an IR-level pass
  contract (CFG construction, dead-block elimination, constant folding,
  mem2reg-style promotion) verified against `llc` output. Today `opt.go` is a
  regex-ish text transform.
- **JIT for REPL feedback** (mission: "fast REPL feedback despite AOT"):
  compile the textual IR with `llc`/`cc` per expression and `dlopen`-execute,
  or add a small in-process JIT path — without importing external bindings.
  The REPL currently uses the interpreter; a real JIT is the next step.
- **Runtime dispatch**: add a `%obj`-tagged value representation so AOT and
  interpreter agree on the dynamic type model (`docs/agentic/ast-ir-schema.md`).

### Phase 3 — memory model & optimization
- Document the memory model (`docs/adr/0090-memory-model.md`): heap ownership,
  refcount vs mark-and-sweep, stack-vs-heap escape analysis in AOT.
- Add escape analysis + scalar replacement so closures/env-stores don't force
  heap allocation; keep the interpreter's GC as the fallback.

### Phase 4 — docs & verification (continuous, not a phase)
- Keep `docs/language.md`, `docs/operations.md`, `docs/roadmap.md`, `docs/adr/`
  current after every change.
- Every AOT feature ships a unit test + an `integration/` whole-program
  compile-and-run test.

## Definition of done per item
- Interpreter feature + unit test.
- AOT emitter feature + unit test.
- `integration/` whole-program compile-and-run (through `llc`/`cc`).
- `docs/language.md` + `docs/adr/` updated; `CHANGELOG.md` bumped.

## Quick build/test commands
```
go build ./...            # compile compiler + CLI
go test ./pkg/lang/       # unit tests (interpreter + codegen)
go test ./integration/    # whole-program llc/cc compile-and-run
```
