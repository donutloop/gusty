# ADR 0148 — Fuzz / property-based testing of both backends

## Status

Accepted (Round 16).

## Context

gusty ships **two independent lowering backends** that consume the same AST:
the tree-walking **interpreter** (`pkg/lang/jit.go`, entry `EvalExpr`) and the
**LLVM 20 AOT compiler** (`pkg/lang/codegen.go` + `closure.go`, entry
`Compile`). Because they share no intermediate representation, semantics can
silently drift between them (e.g. dispatch is statement-level in AOT only,
mixed-type conditions, float/bool arithmetic, tuple unpack).

The conformance matrix (ADR 0144) locked a fixed corpus of whole-program
cases, but a *fixed* corpus only covers what someone remembered to write. We
want **generated** coverage that explores the shared surface the way a human
test author never will.

## Decision

Add a deterministic, seeded **property-based whole-program generator** and a
**Go-native fuzz target** over the shared surface.

- `pkg/lang/proptest.go` — `PropGrammar` + `DefaultPropGrammar()`: a seeded
  `rand.Rand`-driven grammar that builds random `*Program` ASTs over the
  AOT+interpreter *shared* surface and renders them back to canonical source
  via `Format`, so one source string exercises both backends.
- Determinism: `PropSource(seed, n, g)` always yields the same n source
  strings; failures/drift are reproducible by re-running the printed seed.
  `PropPrograms(seed, n, g)` is the AST-level twin used by unit properties.
- Scope discipline keeps every generated program well-formed: top-level
  expressions may read only top-level variables (always bound before use
  because generation is sequential), and suite bodies (if/for/function) read
  only their own locals (loop var / params) plus literals — no undefined
  references, no forward bindings.
- The generated surface is deliberately constrained to constructs the AOT
  backend lowers cleanly (integer arithmetic, integer-only comparison
  conditions, list literals, `len`/`abs`/`min`/`max`/`range`, for-over-range,
  functions), because AOT still has real drift bugs (mixed-type conditions,
  float/bool arithmetic, tuple unpack) that the harness surfaces rather than
  hides.
- `integration/proptest_test.go` — cross-backend parity harness that runs
  each generated source through `lang.InterpreterRun` and the
  `Compile` → `llc` → `cc` → run pipeline and diffs stdout. Interpreter
  failures fail the build; AOT drift is *logged* (with seed+index for
  reproduction) so the suite stays green while drift is tracked.
- `pkg/lang/proptest_test.go` — unit properties: corpus reproducibility,
  parse-cleanliness of every generated source, interpreter validity
  (no undefined names / runtime errors), and interpreter determinism
  (run-twice byte-identical stdout).
- `FuzzPropInterpreter` — a Go-native fuzz target that seeds from the
  deterministic corpus and mutates source, asserting the interpreter never
  panics (a clean rejection is reported, not a crash).

## Consequences

- Generated, reproducible coverage across both backends instead of a fixed
  hand-written corpus only.
- A regression harness that catches two-backend semantics drift and
  interpreter panics under arbitrary input.
- Fuzz/property tests run in the normal `go test ./pkg/lang/ ./integration/`
  pipeline; no new build-time dependency.
- The generator is shared (single source of truth) so both backends see
  exactly the same inputs.
