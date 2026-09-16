# ADR 0039: sum/min/max over set and dict literals in the AOT codegen

## Decision

Extend the `sum`/`min`/`max` builtins to fold **set literals** (their `Elems`)
and **dict literals** (their `Keys`) in the AOT LLVM codegen, in addition to
the existing list-literal and comprehension paths.

## Details

- The aggregate fold extracts element expressions from `ListLit.Elems`,
  `SetLit.Elems`, or `DictLit.Keys` into a local `elems []Expr`, then emits
  the same add/icmp-select accumulation for all three shapes.
- Dict aggregates fold over keys (interpreter semantics), not values.
- Error messages were widened to "inline list/set/dict literal".

## Alternatives rejected

- Leaving set/dict aggregates codegen-unsupported (they errored with
  "requires an inline list literal").
- Folding dict values instead of keys (would diverge from the interpreter).
