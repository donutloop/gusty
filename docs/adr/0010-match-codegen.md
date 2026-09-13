# ADR 0010: match lowers to a comparison chain

## Decision

`match` statements lower in the textual IR emitter to a chain of `icmp eq`
comparisons against the subject. Each case produces a body block terminated by
`br %end`; non-final cases branch the no-match path to the next case, and the
final case branches it directly to the end label so every basic block has a
terminator (llc-clean).

## Agentic rationale

The interpreter evaluates match by comparing each literal pattern to the
subject in order. Keeping the IR lowering structurally identical (first-match
wins, single end merge) gives humans and agents a single mental model and lets
the same integration test (`TestIRMatchCompilesWithLLC`) guard both paths.

## Codegen/IR implications

- Subject and pattern values are `i32`; comparison is `icmp eq`.
- The last case's fall-through is the end label (no empty, unterminated block),
  satisfying `llc -opaque-pointers` verification.

## Alternatives rejected

- Emitting every case to a switch-like dispatch: overkill for literal-only
  patterns and harder to keep llc-clean.
- Leaving `match` unlowered (interpreter-only): the compiler would not cover a
  mission-list feature end-to-end.
