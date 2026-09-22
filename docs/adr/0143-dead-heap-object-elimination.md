# ADR 0143: IR-level dead-heap-object elimination (liveness + escape analysis)

## Context
The AOT codegen lowers heap lists/dicts/sets to `rt_alloc` + mutating op calls
(`rt_set_elem`/`rt_append`/`rt_dict_put`/`rt_set_add`) against a shared `@heap`
global. `escape.go` already skips *top-level* dead list allocations at emit
time, but objects built *inside function bodies* (e.g. a list appended to in a
loop and then discarded) are still emitted. The roadmap wants the optimizer
(`opt.go`) to eliminate **whole dead heap objects** at IR level, not just dead
instructions/blocks.

## Decision
Add an IR-level pass `fn.deadHeapElim()` that runs to a fixpoint inside the
per-function optimizer loop. It:
1. Collects every `rt_alloc` def register (the heap handle).
2. Classifies each use by callee + argument position:
   - receiver (`arg 0`) of a mutating op → `write` (unobservable if dead);
   - receiver of `rt_get_elem`/`rt_list_len`/`rt_dict_get`/`rt_dict_len`/`rt_set_len`/`rt_contains` → `read`;
   - receiver of `rt_print_list`/`rt_print_dict`/`rt_print_set` → `print`;
   - any other use (value operand, `rt_mkobj`, `rt_slice`, return, store,
     arithmetic, phi, gep, ...) → `escape`.
3. An object is **dead** iff every use is a `write` (or none). Dead objects'
   `rt_alloc` and all of their write-call lines are marked `deleted` and are
   not re-serialized.

This is conservative: any read, print, slice, `%obj` conversion, or non-call
use keeps the object alive. It is also the IR-level generalization of the
source-level `escape.go` elision and catches function-local dead objects.

## Consequences
- Emitted programs no longer allocate or write dead heap objects inside
  functions, reducing heap pressure and runtime work.
- Removing an allocation only *lowers* `@heap_count` (frees capacity); the
  dead object's handle is never observed, so no read/print path changes. This
  matches the assumption the source-level elision already makes.
- The pass is exercised by `TestDeadHeapElim` (dead/multi-dead/read/print/
  escape cases). The `integration` parity runner is currently unreliable in
  this environment (pre-existing segfault independent of this change), so
  correctness is validated at the unit level.
