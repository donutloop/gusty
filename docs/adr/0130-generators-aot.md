# ADR 0130: Generators (yield + generator expressions) in AOT codegen

## Context
ADR 0081 landed generator expressions in the interpreter (eager, collecting
all yielded values into a list). The AOT/LLVM codegen path had no generator
lowering: a `yield` statement fell through to the default unsupported-error,
and generator-expression results printed as raw heap handles. Per the
`Dynamic dispatch + method tables` roadmap item, generators are the final
construct to land in AOT (after exceptions, modules/imports, and classes).

## Decision
Lower generator functions (`def g(): yield a; yield b`) and generator
expressions `(elem for var in iter [if cond])` to **runtime heap lists**,
mirroring the interpreter's eager semantics exactly.

- **Generator functions.** `funcDef` detects a generator via `containsYield`
  (a recursive scan of the body for `YieldStmt`, including inside `if`/
  `for`/`while`/`try` blocks). On entry it emits `rt_alloc(1)` into a fresh
  handle `%ghN`; each `yield expr` statement lowers to
  `call void @rt_append(i32 %ghN, i32 <expr>)`. The function `ret`s the
  heap-list handle on both the normal and the raise-exit paths.
- **Call-site tracking.** `genFuncs[fnName]` records generator function
  names; when a `Call` resolves to one, the result operand is registered in
  `listOperands` so downstream `print`/indexing treat it as a list handle.
- **Generator expressions.** `genExpr` unrolls a constant `range(...)` call
  or `ListLit` iterable at codegen time (mirroring `comp()` for list
  comprehensions), binds `ForVar` to each element in `constBindings`, filters
  by a foldable `Cond`, and `rt_append`s each folded element to a fresh
  `rt_alloc(1)` handle `%gxN`. The result handle is registered in
  `listOperands`.
- **Print.** The `print` lowering checks `listOperands`; a generator result
  emits `call void @rt_print_list(i32 <v>)` instead of the `%d` printf path.

## Consequences
- Pure generators (no `return`/`break`) evaluate eagerly to a runtime heap
  list in AOT, matching `Collect()`-tracked interpreter lists; `for x in g():`
  and `list(gen)` consume them as lists.
- No new runtime object kind is needed — generator results reuse the existing
  list kind (`rt_alloc(1)` / `rt_append` / `rt_print_list`).
- Arbitrary `return`-early or control-flow-in-yield generators are outside
  this eager model (documented limitation, same as the interpreter).
- Added `TestAOTGeneratorFuncsAndExpressions` (IR-validity via `llcCompiles`,
  asserting the IR contains `@rt_alloc`/`@rt_append`/`@rt_print_list`) and the
  whole-program `TestGeneratorFunctionsAndExpressions` in `integration/`.

## Alternatives Rejected
- **Lazy generator objects** (a `%obj`-tagged generator consumed on demand):
  rejected for parity with the interpreter's eager list model; the runtime
  dispatch / `%obj`-tagged value representation is a follow-on roadmap item.
- **Compile-time global list literals** for generator results: rejected —
  generator bodies may depend on runtime params (`yield n * 2`), so results
  must be heap lists allocated per call.
