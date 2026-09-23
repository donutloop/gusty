# 0146 — Generics / structural protocols (`Sequence[T]`, `Callable`)

- Status: accepted
- Date: current
- Deciders: agent loop
- Ticket: roadmap Phase 9 — "Generics / protocols"

## Context

Gradual typing previously stopped at `Any`: annotations accepted only the bare
atomic kinds (`int`, `float`, `bool`, `str`, `any`), and the type checker
compared concrete kinds by exact equality. The roadmap asks for
`Sequence[T]`/`Callable` bounds and structural protocols so annotations can
express element/parameter/return shapes.

## Decision

Add two structural protocol kinds to the type system:

- `KindSequence` (`Sequence[T]`) — a bound accepting any indexable/iterable
  sequence whose element type is compatible with `T`: `list[T]`, `set[T]`,
  `iter[T]`, a homogeneous `tuple[...]`, or `str` (as `Sequence[str]`).
- `KindCallable` (`Callable[[A, B], R]`) — a bound accepting any function whose
  arity matches, whose parameter kinds match, and whose return is assignable to
  `R`. A bare `fn` reference (function name with no resolved signature) is
  assignable to any Callable bound under gradual typing.

Annotations are now recursive generic expressions: `list[int]`,
`dict[str, int]`, `set[int]`, `tuple[int, str]`, `Sequence[int]`, nested
`list[list[int]]`, and `Callable[[int, str], bool]`.

The checker replaces exact-kind equality at three sites — assignment
annotations, call-argument-vs-annotation, and return-vs-`->` — with a single
`assignable(got, want)` structural relation. Concrete kinds keep exact-kind
equality; dynamic tolerates anything in either position; protocols recurse on
element/param/return shape.

## Consequences

- `Type` gains `KindSequence`/`KindCallable`; `Name()` renders
  `Sequence[int]` and `Callable[[int, bool], R]`; `Same()` compares protocol
  shape structurally. JSON round-trips carry these kinds via existing struct
  tags (`kind`, `elem`, `params`, `ret`), so the machine/CLI path is unchanged.
- Function-name arguments resolve to a bare `fn` (no params/ret); Callable
  checking therefore happens structurally at the bound rather than at call
  sites for named callees.
- Existing concrete-kind behavior is preserved; only protocol bounds and
  recursive annotation syntax are new.
