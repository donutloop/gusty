# ADR 0009: Runtime heap + boxed mutable lists in AOT

## Status

Accepted (milestone 1). Full mark-and-sweep GC is a follow-on milestone.

## Context

The roadmap item "Runtime heap + GC in AOT" requires that lists/dicts/sets/strings
not be *compile-time globals only*: AOT programs must be able to *mutate runtime
collections*. The interpreter already owns a full heap with `Collect()`; the AOT
emitter previously folded every list literal to a static global and printed the
address of that global.

This ADR records the first, testable milestone: a runtime object heap for mutable
lists in the emitted LLVM IR, enabling `x = [1, 2]`, `x.append(3)`, and `print(x)`.

## Decision

Emit a small runtime heap into the module when a program assigns a list literal to
a variable (`heapUsed`):

- `@heap_count`, `@heap` (a fixed global array of list objects), and internal
  helpers `rt_alloc`, `rt_set_elem`, `rt_append`, `rt_print_list`.
- A list-variable is tracked in `irGen.listVars`. `x = [a, b]` lowers to
  `rt_alloc(1)` + `rt_set_elem` + `store handle`. `x.append(v)` lowers to
  `rt_append`. `print(x)` for a list-variable lowers to `rt_print_list`.
- Existing compile-time folding for *inline* literals (`len([1,2])`, index,
  sum, print([1,2])) is unchanged.

## Consequences

- AOT programs can now mutate runtime list objects and print their contents
  (`[1, 2, 3]`), verified end-to-end through llc-20/cc.
- The heap is a fixed-size global array; allocation is bump-allocated.
- Follow-on (milestone 2, this cycle): `len(x)` on runtime list variables reads
  the mutated length through the heap (`rt_list_len`), verified end-to-end.
- Remaining follow-on: refcount/mark-sweep GC mirroring the interpreter's `Collect()`,
  dict/set runtime objects, and index access on runtime list variables.
