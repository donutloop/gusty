# 0197. A def is a binding of its whole scope: forward references check clean

## Status

Accepted. Implemented this cycle: the checker's name resolution and the call-resolution
path in `pkg/lang/semantic.go`; `integration/programs/forward_defs.gy` and
`integration/forward_defs_test.go` pin it. Roadmap Gap R.5.

## Context

Both backends could always run a program whose functions call each other. The checker
could not:

```bash
$ cat mutual.gy
def is_even(n):
    if n == 0:
        return True
    return is_odd(n - 1)

def is_odd(n):
    if n == 0:
        return False
    return is_even(n - 1)

print(is_even(4))
$ gustyc --check mutual.gy
error at 4:12: undefined name "is_odd"        # exit 1
$ gustyc --interp mutual.gy
1                                              # runs
$ gustyc --aot mutual.gy
gustyc: jit: 1 error(s) in source              # refuses to compile
```

and the same for a helper declared below its caller. The compiled path refuses to emit IR
for a program the front end rejected (ADR 0177), so a false positive in the checker is a
false refusal to compile — mutual recursion, the canonical Python shape, could not be
compiled at all.

The cause was structural: `Analyze` walked the statement list in order and defined each
`def` in the scope as it reached it, so any use above the definition line found nothing.
Classes had already been given a pre-pass for exactly this reason (`indexClasses`, with a
comment about declaration order); functions had not.

Two neighbouring behaviours had to stay exactly as they were, and measuring them is what
made the rule precise:

- a call **at module or class top level** to a def below it: that call runs the moment the
  file is reached, so the name genuinely does not exist yet — the interpreter raises, and
  the checker must report it;
- a **decorator expression**: `@identity` is evaluated where it is written, so naming a
  function defined further down is a real error. (A first implementation that defined the
  hoisted names in the scope up front silently swallowed this one.)

## Decision

**A function body may resolve a name to any `def` of its enclosing scope, wherever in that
scope the `def` is written; code that runs immediately may not.**

1. **Collect, do not define.** `collectFuncs` gathers the `def`s of a statement list into
   a table — `moduleDefs` for the file, one pushed table per function body — instead of
   defining them in the scope ahead of the walk. The traversal mirrors `indexClasses`: a
   def inside an `if` / `while` / `for` / `with` / `try` / `match` arm belongs to the
   containing scope, because those statements create no scope; a class body is a scope of
   its own, so nothing inside it hoists outward.

2. **Consult the table only where code is deferred.** The name lookup asks the deferred
   tables only while `an.inFunc` — inside a function body — and never at module or class
   top level. That single condition is what keeps the two behaviours above honest: a body
   runs later, when the name is bound; a top-level statement and a decorator run now.

3. **Innermost first.** Nested defs are visible to their own body (so sibling nested defs
   can call each other) and outward through the enclosing bodies' tables, never across to
   a sibling function.

4. **The signature travels with the name.** The table holds the `*FuncDef`, so
   `userFunc(name)` resolves a forward call to its definition and the ordinary
   arity/annotation checks run: `take("text")` where `take(n: int)` is declared below the
   call is still reported as `argument "n": expected int, got str`. A forward call cannot
   be told a return type the checker would have *inferred* from a body it has not walked;
   that stays dynamic, which is where gradual typing already places unannotated code.

5. **Nothing else is hoisted.** A variable is still not visible above its assignment, an
   undefined name is still an undefined name, and a misspelled mutual partner is still
   reported — the table contains declarations, not guesses.

## The rule in one paragraph

A name must exist when the call runs. Where the call is deferred, the whole scope is
visible; where the code runs now, only the lines above this one are.

## Codegen / interface implications

- No new diagnostic codes. The change *removes* false occurrences of the existing
  `undefined name` error; `--check`, `--file`, `--json`, the build pipeline and the schema
  are otherwise unchanged, and the schema's name-resolution example for `undefined name`
  stays valid.
- Codegen was never involved: it resolves calls by name against the functions the module
  defines, which is why the interpreter and the LLVM backend already ran these programs and
  why the fix belongs entirely in the shared front end.
- The conformance ledger gains `programs/forward_defs` (mutual recursion, a helper below its
  caller, `describe` → `classify` → `is_even`): `oracle: "match"` — interpreter, compiled
  binary and CPython byte-identical.

## Alternatives considered

- **Refuse forward references as a language rule.** Rejected: it would forbid the most
  idiomatic Python there is, and both backends already ran the programs; a checker that is
  stricter than the language is a bug generator for the agents that use it.
- **Define the hoisted names into the scope before the walk.** Rejected after measuring:
  it silently accepted `@identity` before `def identity`, where the decorator really is
  evaluated first. The deferred table plus `an.inFunc` is the same amount of code and gets
  both cases right.
- **Two-phase analysis: analyze all defs first, then the rest.** Rejected as heavier than
  needed and behaviour-changing: the walk already re-analyzes callee bodies per call site
  through `inferReturn`, so a forward call gets real inference; only *inferred* return types
  of not-yet-walked defs stay dynamic, which is the gradual-typing status quo.
- **Treat forward calls as fully dynamic (skip checking).** Rejected: it would have silenced
  arity and argument-type errors for exactly the functions whose definitions arrive later.

## Consequences

- Mutual recursion and below-the-line helpers compile. `programs/forward_defs.gy` is the
  third path that proves it, alongside the unit shapes in `pkg/lang/forward_defs_test.go`.
- The checker's verdict no longer depends on the order of declarations inside a scope,
  which is the property humans and agents assume when they move a helper to the bottom of
  a file.
- Measured while testing, and left as its own gap (roadmap R.7): diagnostics from
  per-call-site inference are emitted once per call site, so the same source line can
  report the same warning two or three times. That is noise in `--json` for an agent, and
  it predates this change.
- The two defects this round grew out of while measuring async/parameter behavior — the
  emitted symbol colliding with the host ABI (R.4) and a user `def abs` shadowed by the
  builtin's value (R.6) — remain open and are next.
