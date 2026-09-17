# ADR 0089: reference-counted mark-and-sweep memory model (Collect)

## Decision

Add a public, explicit garbage-collection primitive to the evaluator:
`ev.Collect()`. It implements a mark-and-sweep memory model pass over the
heap:

- Roots are the top-level environment bindings (`ev.Vars`).
- Mark walks every reachable object through container fields (`elems` for
  list/set, `elems`+`dvals` for dict), closure environments (`env`), and
  attribute tables (`attrs`).
- Sweep frees only **pure-data** objects (kinds `list`, `dict`, `set`, `str`,
  `int`, `float`) that were never marked.

`Collect()` is **conservative**: class, method, closure, import, and module
objects may hold references outside this heap (e.g. class method tables), so
they are never freed by the sweep.

## Motivation

The mission's "memory model" item was unimplemented: the evaluator allocated
heap objects (`e.heap map[int64]*obj`) but never freed them, so long-running
programs leaked unreachable lists/dicts/sets/strings. This adds a safe,
testable collection primitive.

## Why explicit, not automatic

The evaluator is a tree-walking interpreter with recursive evaluation paths:
top-level statements, function bodies, generator bodies, and for-loop bodies
share statement/expression drivers. An automatic per-statement GC injected
into the top-level loop ran inside those recursive paths too, freeing
generator yield-lists and closure envs mid-execution (regression in
generator-sum tests). Making collection an explicit, end-of-program API
avoids that hazard: no collection runs during evaluation, so no live
temporary (generator list, closure env, return value) is ever freed.

## Tests

`pkg/lang/memory_test.go`:

- Rebinding `x = [1,2,3]` then `x = [4]`, then `Collect()`: the dead `[1,2,3]`
  list is gone; the live `[4]` list remains.
- Two live variables `x=[1,2,3]` and `y=[4]`, then `Collect()`: both lists
  remain.
- A closure capturing `x=[1,2,3]` in `f = lambda: x`, then `Collect()`: the
  captured list survives (closure env is marked).

## Alternatives rejected

- Automatic per-statement GC: rejected (see Motivation — freed live generator
  temporaries).
- Full reference counting on every container op: rejected — invasive and
  error-prone; mark-and-sweep from `Vars` roots is simpler and conservative.
