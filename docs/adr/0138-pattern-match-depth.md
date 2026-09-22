# ADR 0138: Pattern-match depth — guards, or-patterns, and dict patterns

Status: accepted

## Context

`match` previously supported integer-literal patterns, wildcard `_`, and list
destructuring (`case [a, b]:`) in the interpreter. The roadmap (Phase 6,
"Pattern-match depth") calls for guards (`case p if cond:`), or-patterns
(`case p1 | p2:`), and dict patterns (`case {"k": v}:`).

## Decision

Extend `match` cases with three new capabilities:

1. **Guards** — a `case` may be followed by `if <cond>` (a full expression).
   After the pattern binds (and matches), the guard is evaluated; if falsy,
   the case falls through to the next case.
2. **Or-patterns** — a `case` may list alternatives separated by `|`
   (`case 1 | 2 | 3:`). The case matches if the subject matches *any*
   alternative.
3. **Dict patterns** — a `case` may use a dict literal `case {"k": v}:`.
   The subject must be a dict containing each key; literal values compare by
   content, and `Name` values bind to the dict's value for that key.

### AST

`MatchCase` gains `Or []Expr` (remaining or-alternatives, the first lives in
`Pattern`) and `Guard Expr`.

### Parser

A dedicated `parseMatchPattern` / `parsePatternAtom` parses patterns as atoms
(Name/Int/Str/List/Dict), then consumes top-level `|` alternatives and an
optional `if` guard. This avoids treating a pattern's trailing `if` as a
ternary expression.

### Interpreter

The `case *MatchStmt` handler tries each alternative via a new
`matchPattern(sub, p)` helper that binds names for list/dict patterns,
matches wildcard, and compares literal values. After a successful pattern
match, an optional guard is evaluated and may reject the case.

### Codegen (LLVM)

The match lowering emits one `icmp eq` per alternative, ORs them with `or i1`
for or-patterns, and branches into the case body. A single wildcard still
emits the tautology `icmp eq sub, sub` to preserve prior IR shape. Guards are
evaluated at the case entry label via `truthyValue`, branching to a
`match.case.body` label on truthy and to the next case on falsy.

### Semantic analysis

The analyzer now registers pattern-bound names (`a`, `b`, `v` from list and
dict patterns) in each case scope so case bodies can reference them. Guards
are analyzed as expressions.

## Consequences

- Interpreter and codegen both support guards and or-patterns for
  literal/wildcard patterns; dict patterns are supported in the interpreter.
- Class patterns (`case Point(x, y):`) remain a follow-up (need instance/attr
  runtime in codegen).
- New tests cover or-patterns, guards (pass and fall-through), and dict
  patterns (present key and missing key).
