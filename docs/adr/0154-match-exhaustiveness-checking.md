# ADR 0154: Match exhaustiveness + definite-assignment checking

## Status
Accepted (implemented, roadmap Gap B semantic half — L6.1/L6.2).

## Context
`match` selects one case whose pattern matches the subject. A `match` whose
cases are all *refutable* patterns (e.g. literal `case 1:`) can reject some
subjects at runtime, leaving no case body to run. Python and mypy treat an
uncovered `match` as a non-exhaustive (fallthrough) case rather than an error;
mypy additionally flags non-exhaustive matches as a warning. Gusty needed the
same mypy-style signal so authors add an explicit `case _:` fallback.

Separately, a bare-name pattern (`case y:`) is *irrefutable*: it always
matches and binds the subject to `y`. After such a match, `y` is assigned on
every path and is safe to read — but only if the binding case fires on every
path (every case is irrefutable). A name bound by an irrefutable case that
does not cover all paths (some earlier case is refutable) is *not* definitely
assigned.

## Decision
In the semantic pass (`pkg/lang/semantic.go`, `MatchStmt`):

- **Exhaustiveness (L6.1)**: a `match` is exhaustive iff at least one case
  has an irrefutable pattern — a bare `Name` pattern (including `_`) or a
  list/dict pattern whose every element is irrefutable (this first pass
  treats any bare `Name` case as irrefutable). Otherwise the analyzer emits a
  mypy-style `match is not exhaustive` *warning* (`LevelWarning`). Warnings
  do not change `gusty check` exit codes (only `LevelError` does), matching
  the "warning, not error" contract.
- **Definite assignment (L6.2)**: per case, the pattern's bound names are
  collected (`matchPatternNames`) and defined in that case's scope. The names
  bound by *every* case are intersected (`boundAll`) and, after the match,
  defined in the enclosing scope — they are definitely assigned and readable.
  Names bound in only some cases are left undefined, so reading them after
  the match reports `undefined name`.
- `_` is treated as a wildcard: it never contributes a bound name (so it is
  never in `boundAll`) but is still irrefutable for exhaustiveness.

## Consequences
- Authors get an early, non-fatal warning when a `match` lacks a fallback;
  `gusty check` surfaces it in the human and `--json` machine paths.
- Reading a name bound by a bare-name case on every path is accepted; reading
  one bound on only some paths is rejected as `undefined name`.
- The runtime semantics are unchanged: the interpreter and AOT codegen
  already fall through to the next case when no pattern matches, and the
  exhaustiveness warning is advisory only.
- AOT lowering of guards/or-patterns/wildcard is already present
  (`codegen.go`); dict-pattern and class-pattern lowering remain
  interpreter-only (documented as such in `docs/language.md`).

## Alternatives rejected
- Making non-exhaustive `match` a hard error: would break valid programs that
  intentionally rely on runtime fallthrough; mypy treats it as a warning.
- Full literal-type exhaustiveness analysis (enumerating all subject shapes):
  requires a literal/union type lattice Gusty does not yet have; the
  "has an irrefutable case" heuristic is the mypy-style approximation and is
  a safe conservative signal.
