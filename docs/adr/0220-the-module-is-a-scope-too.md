# The module is a scope too

## Status

Accepted (cycle 168, Gap R.35) — interpreter and checker legs. The compiled leg is open and recorded.

## Context

This program does not exist in the language:

```gusty
v = 1
def g() -> int:
    return v
print(g())
```

CPython prints `1`. The interpreter raised `NameError: name 'v' is not defined` (exit 3) and the
compiled backend refused with `undefined name "v" (no binding for it; assign it before use)` (exit 1).
The refusal's own sentence claims the two backends agree; they did — on a wrong answer. Measured
across shapes, the interpreter failed on all of: reading a module int, reading one assigned *below*
the def (Python resolves at call time, so this is not even an ordering question), calling `len` on a
module list, a method reading a module name, and a nested def reaching past two frames to a module
constant. Closure reads from an enclosing *function* worked — the scope chain existed, the module
simply was not on the end of it.

A script cannot declare a constant it reads from a function. That is the single most common shape in
a program, and the loop had been building a language without it.

## Decision

**A name a function reads is looked for in its own frame, then in the closure environment captured
where the function was written, then in the module the function was *defined* in.** Two halves:

*Runtime.* `Evaluator` gains `moduleVars` (the top-level scope), `curModule` (the global scope of the
call in flight) and `funcModules`, a map from each `*FuncDef` to the scope it was executed in.
`rememberModuleScope` records that association when a `def` or a class method is registered — at the
top level that is `e.Vars`; inside a function body it is the *enclosing function's* global scope, so a
nested def reaches the module rather than its parent's frame; inside `importModule` it is the imported
module's own scope, so `mod.fn` resolves bare names against `mod`, not against the importer.
`callFunc` installs the callee's module for the duration of the call and restores the caller's after,
and the `*Name` case of `eval` consults it after the frame and the closure env. Assignment inside a
body still creates a local (Python's rule, and a test pins that the module keeps its own value), so
this is a lookup fallback, not a global alias.

*Checker.* A function body may name a module variable that is assigned **below** it. The analyzer
pre-collects the names a top-level statement tree can bind — including inside top-level `if`/`while`/
`for`/`try`/`with`/`match` — and consults that set only while `an.inFunc`, next to the existing
`lookupDeferred` for defs declared below their caller (ADR 0197); the argument is the same one,
applied to data: the body runs later. Module and class top level never consult it, so reading a module
variable above its assignment is still an `undefined name` error, which `TestHoistingIsNotAFreeForAll`
and `TestForwardReferenceIsNotARefusal` defend and this commit had to satisfy — my first attempt
pre-registered those names in the module scope and both tests failed within seconds, correctly,
because module code really does run line by line.

*Memory.* A module scope is not on the frame stack, and since a function may read it arbitrarily late,
its bindings are now **permanent roots**: `anchorScope` records each one (deduped by the map's
address — a Go map is not comparable, so identity is `reflect.ValueOf(m).Pointer()`) and
`rootHandles` scans `globalScopes`. Without that, a collection could sweep an object reachable only
through a module global, which in this GC's history means "the list reads back empty".

## Agentic rationale

`--check` said `undefined name "v"` about a program that runs, which is the most expensive kind of
front-end answer; the interpreter said the module's own constant was not defined; and the compiled
refusal told the reader the interpreter would report the same thing, which is now measurably untrue
(that sentence is Gap R.38, and this cycle adds a second untrue instance of it: the probe's refusal
claims an error the interpreter does not produce). `programs/probe_module_scope.gy` is a debt row:
interpreter pinned at `80 7 5 40 1`, compiled leg recorded missing, with the reason naming both the
refusals and the two shapes that compile and print `0`. `TestModuleScopeIsStillOutOfReachForCompiled
Code` asserts that state and says what to delete when it changes.

## Codegen / IR implications

None for this change — the compiled leg still refuses, and the debt is now measurable rather than
described: `undefined name "MAX" (no binding for it; assign it before use)` for a function reading a
module constant, and a silent `0` for a method or nested def reading one (an unwritten slot read, the
Gap R.36 signature). The compiled half needs module bindings to live somewhere a function body can
address — real global slots, registered with the collector the way `globalScopes` did here — which is
why it is its own cycle rather than a rider on this one.

## Alternatives rejected

- **Copy the module scope into each function's frame at def time.** Rejected: that is a snapshot, and
  it breaks the two programs that make Python's rule useful — a name assigned below the def, and a
  global rebound between calls (`print(get())` twice with `c = 5` between them prints `1` then `5`).
- **Resolve module reads only for names assigned above the def.** Rejected: it is the accident of one
  test's wording, not a rule; the checker's own justification for deferred defs (ADR 0197) already says
  a body runs later.
- **Apply the pre-registered names at module level as well.** Rejected, and *tested* as rejected: module
  statements execute in order, and `print(total)` before `total = 3` is a bug the front end must name.
- **Implement `UnboundLocalError` now.** Not done and recorded as Gap R.39: reading a name that the body
  also assigns below the read returns the module's value where CPython raises `UnboundLocalError`. The
  divergence is toward the more forgiving answer, in a shape the checker flags as `possibly unbound`;
  it needs the checker's local-set to reach the runtime (the frame would need to know which names are
  locals before it runs), which is a separate piece of plumbing and not what this cycle unblocked.
- **Wait for the compiled half before shipping the interpreter half.** Rejected: the interpreter is the
  REPL/`--eval` path humans live in, its behaviour is CPython-exact here, and leaving both paths wrong
  to keep them equal is not symmetry worth paying for — provided, as done here, that the compiled
  shortfall is a named row with pinned legs rather than a rumour.
