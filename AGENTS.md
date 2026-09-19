# Agent Workflow

This is the root-level prompt for the coding agent building **Pyre** (or whatever name is chosen), a Python-like programming language — familiar indentation-based syntax and dynamic-feeling ergonomics, compiled ahead-of-time through LLVM. The agent must follow these rules indefinitely — this is a loop, not a one-off.

## Two execution paths — both are first-class

The language has **two supported execution backends**, and they must stay in sync:

- **Interpreter** (`pkg/lang/jit.go`, entry `EvalExpr`) — the REPL / `--eval` / `--verify` path. Fast feedback, rich diagnostics, and the place where the full language surface (closures, decorators, classes, generators/`yield`, exceptions, comprehensions) is implemented and tested first.
- **LLVM AOT codegen** (`pkg/lang/codegen.go` + `pkg/lang/closure.go`, entry `Compile`) — the `--file` / `--emit-llvm` / link-and-run path. Textual IR verified by `llc`/`llvm-as`.

Every feature must ship in **both** paths unless an ADR explicitly documents the interpreter-only or codegen-only limitation (e.g. arbitrary decorators and nested closures are currently interpreter-only AOT limits). When a feature is added, implement it in the interpreter first (fast, testable), then mirror it in the LLVM codegen and verify the emitted module. Keep `docs/language.md` accurate about which path supports each construct.

This compiler toolchain is built for BOTH humans and agentic workflows. The interface must reflect that duality at every layer: humans get a friendly CLI/REPL that feels like `python`/`ipython` (help, discoverable commands, readable diagnostics), while agents and scripts get structured, predictable, self-describing access (machine-readable JSON diagnostics, a JSON schema for AST/IR, stable CLI flags, self-describing commands, and deterministic exit codes). No feature ships until it has a machine consumption path as well as a human one.

## Mission

Build the best Python-like language ever known by humanity, compiled through LLVM. Think like a true programming-language and compiler expert who also loves Python's ergonomics. Every feature must consider the whole system:

- **Language surface** — indentation-based blocks, dynamic-feeling but gradually-typed values, expressions, variables, functions with default/keyword args, closures, classes and inheritance, control flow (`if`/`elif`/`else`, `for`/`while`, `match`), list/dict/set comprehensions, generators and `yield`, exceptions (`try`/`except`/`finally`), decorators, modules/imports, and a small standard library (strings, lists, dicts, iterators, `print`, file I/O).
- **Internal components** — lexer (including indentation/`INDENT`/`DEDENT` tracking), parser, AST, semantic analysis / type inference (gradual typing, optional annotations), a lightweight runtime (boxed values, reference counting or a simple GC, dynamic dispatch for methods), LLVM IR codegen, optimization pass pipeline, linker integration. Keep them clean, layered, and extensible.
- **User experience** — a great CLI and REPL for humans: clear help, discoverable commands, readable diagnostics with source spans and suggestions (in the spirit of Python's tracebacks, but better), sensible defaults for optimization levels and target triples, fast REPL feedback despite AOT compilation underneath (e.g. via a JIT execution engine for interactive use).
- **Agentic workflows** — this toolchain is not just for humans: it is also a compilation engine for agents and scripts. The interface must expose machine-readable output (JSON diagnostics, JSON AST/IR dumps, schema), stable CLI flags (`--emit-llvm`, `--emit-ast`, `--eval`, `--file`, `--verify`, `--target`, `--opt-level`, `--version`), and self-describing commands so an agent can discover the full language surface, plan its compilation, and consume results without guessing.
- **Correctness** — every feature ships with unit tests, IR verification (via `llvm::verifyModule` through the `tinygo.org/x/go-llvm` bindings), lit-style codegen tests, and verify coverage. `go test ./...` must pass before commit.

Because we build for both humans and agents, the interface must reflect both: humans get a friendly CLI/REPL; agents get structured, predictable, self-describing access. Every new feature should consider its machine consumption path (JSON output, exit codes, schema) as well as its human one.

Be creative: prefer language-level features (comprehensions, generators, classes, decorators, pattern matching, exception handling, gradual typing, module system) over plain builtin functions. Add builtins only when they genuinely expand the language, the way Python's own builtins do.

Think like somebody writing a brand-new Python-like, LLVM-compiled language in 2026: modern ergonomics, clean diagnostics, discoverable commands, correct and inspectable codegen, and a CLI/REPL that feels as good as Python's but runs like a compiled language.

## Toolchain

- **Implementation language**: Go.
- **LLVM binding**: `tinygo.org/x/go-llvm` — Cgo-based bindings to a system-installed LLVM (the fork used by TinyGo). Chosen over `github.com/llir/llvm` (pure-Go, text/bitcode-IR only, no direct optimizer/JIT/target API) and over the deprecated/unofficial `llvm.org/llvm/bindings/go/llvm` forks (unmaintained, no security audit), because the REPL's JIT execution path and the correctness contract's IR verification both need the real LLVM C API, not just IR text generation.
- **LLVM version**: pin one supported version via build tag (e.g. `-tags=llvm18`) and record it in `docs/operations.md`; do not silently float across LLVM versions.
- **Install/build**: `go get tinygo.org/x/go-llvm`, with LLVM installed from apt.llvm.org (Debian/Ubuntu) or Homebrew (macOS) matching the pinned tag.
- Record this choice as `docs/adr/0001-llvm-binding-choice.md` before any codegen work begins.

## The loop

1. Pick a new feature.
2. Implement across the stack:
   - lexer/parser/AST for syntax, including indentation handling where relevant.
   - semantic analysis / gradual type inference for semantics (respect optional type annotations; fall back to dynamic dispatch where untyped).
   - runtime support if the feature needs it (new boxed value kind, dispatch method, GC/refcount interaction).
   - interpreter support in `pkg/lang/jit.go` (the REPL/`--eval` path) — implement the feature here first when it touches runtime semantics.
   - LLVM IR codegen (and any new optimization pass) for lowering, mirroring the interpreter behavior.
   - `docs/help.go` (or equivalent) — one-line help per command/flag.
   - `verify/` — known-good verify case (source in, expected IR/output out).
   - docs — `docs/language.md`, `docs/operations.md`, `CHANGELOG.md`, `README.md`, an ADR if needed.
3. **Machine path** — if the feature is language/interface, make sure it is discoverable via schema/help and consumable as JSON where it makes sense (agents must not need to scrape prose or parse raw LLVM IR text blindly).
4. Add tests for the feature before committing: unit tests for the compiler pass, and a codegen/integration test that compiles and runs (or JIT-executes) real source. When the feature is supported in both paths, cover **both** the interpreter (`jit_test.go`, `EvalExpr`) and the LLVM codegen (`integration/`, `Compile` + `llc`).
5. Run tests before committing: full test suite must pass, and every emitted module must pass LLVM's module verifier.
6. Commit with a clear message (`feat(codegen): ...`, `feat(parser): ...`).
7. Always push your commits: `git push origin HEAD`.
8. If the remote diverged, `git pull --rebase origin HEAD` then push again.
9. Return to step 1 and repeat — never stop evolving the language.

## New requirements

- Keep `agents.md` at the repo root; do not store prompts under `setup/prompts/`.
- Every feature commit updates `docs/language.md` (language surface), `docs/operations.md` (CLI/agentic interface), `CHANGELOG.md`, `README.md`, `_001_session_learnings.md` (session learnings), and the ADR if relevant.
- Record each cycle's decisions, discoveries, and process lessons in `_001_session_learnings.md` at the repo root; update it in the same commit as the change it documents.
- `docs/operations.md` is the single source of truth for the CLI, flags, JSON schemas, and exit codes.
- `docs/language.md` is the single source of truth for language syntax and semantics.
- Always push every commit; never leave a feature unpushed.
- Preserve the CHANGELOG as a clean single list (deduplicate if it grows).
- Follow this document's direction on every cycle; it is the contract.

## Rules

- One commit per feature; do not bundle unrelated features.
- Never commit failing tests; fix or drop them first.
- Never commit a change that causes LLVM's IR verifier to fail on any test module.
- Continue the loop even after pushes.

## Iteration contract (never stop)

- Keep executing forever: after each feature is done, immediately begin the next one from the mission list (comprehensions, generators, classes/inheritance, decorators, exceptions, pattern matching, gradual typing, modules, standard library, memory model, optimization passes, agentic/JSON/schema paths) or any remaining gap.
- Push every completed feature to the remote before starting the next one: `git add -A && git commit -m "<feature>" && git push origin HEAD`.
- If the push fails or the remote diverged, `git pull --rebase origin HEAD` and push again.
- There is no terminal state; the loop continues indefinitely.
- Do not stop after step 9 or after a single feature — the loop must keep running. Every cycle must also keep the agentic interface honest: keep schema/JSON output and IR dumps current with every new feature added.

## Architecture Decision Records (ADR)

Every feature ships with an ADR in `docs/adr/` (e.g. `0004-json-diagnostic-schema.md`, `0006-exit-code-contract.md`, `0009-generics-monomorphization.md`). Each ADR records the decision, the agentic rationale, the codegen/IR implications, and the alternatives rejected — so future cycles and agents can reconstruct why the language and interface are shaped the way they are. Write the ADR for the feature before committing, and keep it one per feature.

## Testing rule (always)

Every feature or change ships with BOTH:
- **Unit tests** (in the package/module under change — e.g. parser tests, type-checker tests, codegen tests), and
- **Integration tests** (`tests/integration/`) that compile real source through the full pipeline (lex → parse → typecheck → codegen → optimize → JIT-execute or link-and-run) and check both the output and that the emitted LLVM module verifies cleanly.

Always add both — never a feature without unit + integration coverage. For features present in both backends, add an interpreter integration/unit case (`EvalExpr`) and an LLVM codegen case (`Compile` → `llc`), and keep both green before commit.