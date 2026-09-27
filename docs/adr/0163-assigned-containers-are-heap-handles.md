# 0163. Assigned containers are heap handles

Status: Accepted
Date: 2026-09-27
Audience: compiler maintainers, agents auditing codegen

## Context

ADR 0161 fixed containers *at the call boundary*: literals are materialised into the
runtime heap and callee parameters are typed by whole-module inference. An assignment
was still lowered by a different rule set, and the new LLVM module verifier (L8.2)
immediately found three defects in it:

1. **A folded comprehension stored an aggregate as an integer.** A comprehension whose
   elements are all constants is compiled to a module global
   `@.lst1 = global {i32, [2 x i32]} …`. Assigning it to a variable emitted
   `store i32 @.lst1, i32* %_ys`, which LLVM rejects (`global variable reference must
   have pointer type`). The same program with a literal — `ys = [2, 4]` — was fine, so
   the failure looked comprehension-specific while the real rule was "a container value
   is never an `i32` scalar".
2. **A module container variable had no slot of its own.** `xs = []` at module scope
   stored handle `0` without a root; a later `xs.append(i)` stored through `%_xs`, which
   the definition site never allocated. `llc` then failed with `use of undefined value
   '%_xs'` — and even when a same-named function parameter happened to allocate one,
   `rt_gc` could not see the live handle.
3. **Function-body state leaked into `main`.** `funcDef` resets per-body state; module
   code did not. So after a function with a parameter `xs`, module-level `xs = [1, 2, 3]`
   saw `allocd["xs"]` already set and skipped its own alloca, emitting a reference to the
   alloca that lives inside the function.

A related nondeterminism surfaced at the same time: the parameter-kind fixed point merged
parameters into the variable-kind map in Go map order, so two functions whose same-named
parameters disagreed could classify differently between runs.

## Decision

**One invariant: a variable that holds a container holds a runtime heap handle, at every
binding site, in every scope.** Concretely in `pkg/lang/codegen.go` / `heapargs.go`:

- Assigning a *folded* list (a compile-time `@.lstN`) materialises the folded elements
  with `rt_alloc` + `rt_set_elem` and stores the handle, exactly as a list literal does.
  The folded global stays in the module for consumers that read it structurally
  (constant indexing), so the optimisation is kept; only the scalar store changes.
- `xs = []` at module scope emits its entry-block slot and `gc.roots` entry at the
  definition, so mutation and collection-visible rooting are both correct.
- Module-level code starts a fresh variable-binding scope (`g.allocd`, `g.gcRootSeen`
  reset at the head of `main`), mirroring `funcDef`.
- The parameter-kind fixed point merges in sorted (function, index) order — determinism
  is part of the CLI contract.

## Rationale (agentic)

An agent using the compiler as a tool needs the *shape* of the bug to be predictable:
same program shape → same failure, reproducible across runs, and never a case where a
literal works but a comprehension does not. All three defects were invisible to the
existing tests because none of them exercised "assign a container, in this shape, at
module scope" — the class of program an agent writes when it does not know the backend's
hidden rules. Determinism of the inference is now asserted by the existing JSON-output
tests being stable.

## Codegen / IR implications

- Assignment of a folded list emits `rt_alloc` + `rt_set_elem` (verified by
  `TestFoldedListAssignmentMaterializesToHeap`).
- Module-level `xs = []` emits `%_xs = alloca i32` plus `store i32* %_xs, i32** %gc.slotN`
  exactly once (`TestModuleContainerDefinitionIsRooted`).
- `main` no longer references allocas belonging to function bodies
  (`TestFunctionParamSlotsDoNotLeakIntoMain`).
- Rebinding a container variable still frees the previous slot, so GC discipline is
  unchanged (`TestFoldedListRebindFreesPreviousSlot`).

## Alternatives rejected

- **Keep folded lists as globals and teach every consumer to read the aggregate** —
  larger change, and print/append/len/iteration all need runtime semantics anyway; the
  interpreter has reference semantics, so a compile-time list can never be the whole
  story.
- **Fix only the store site for comprehensions** — leaves `xs = []` + `append` broken and
  leaves module code sharing function scope.
- **Allocate module containers lazily at first mutation** — the alloca would land in a
  loop body (dominated by a branch) while later reads are outside it.

## Consequences

- Comprehension/list assignment matches the interpreter for print/len/index/iteration and
  for passing to functions; `integration/programs/folded_lists.gy` is in the conformance
  corpus.
- Container values are uniformly `i32` heap handles, which keeps Gap I.2 (strings inside
  containers) a single, well-scoped runtime-representation change.
