# 0199. A built-in name is a name: the program's `def` wins over the compiler's reading

## Status

Accepted. Implemented this cycle in `pkg/lang/codegen.go` (with the fold helpers' signatures
threaded through); pinned by `integration/programs/shadowed_builtins.gy`,
`pkg/lang/shadowed_builtins_test.go` and `integration/shadowed_builtins_test.go`. Roadmap Gap R.6.

## Context

`abs`, `str`, `float`, `len` are names, not keywords — a program may define them, and then the
program's meaning is the meaning, in the interpreter and in CPython alike. The compiled backend
disagreed, and not in one place or one way. Measured with `def NAME(x): return x + 7` then
`print(NAME(1))`, expected `8`:

| program writes | interpreted | compiled (before) |
|---|---|---|
| `float`, `sqrt`, `floor`, `ceil` | `8` | `1.0` — folded as the float conversion |
| `str`, `chr` | `8` | **build failure** — the call was emitted as the program's, then *used* as the built-in's string: `printf("%s", i32 %t5)`, which `llc` rejects |
| `int`, `ord`, `round`, `abs`, `sum`, `len`, `min`, `max`, `sorted`, `any`, `all` | `8` | `8` ✓ |

The pattern behind the split: an LLVM backend has to know things about a call that the source does
not say — is its result a float, does it fold to a constant, is it a string, what does `print` emit
for it — and this codegen answers many of those questions **from the callee's name**. The
user-function dispatch was already correctly ordered ahead of the built-in `switch`; what was not
guarded were the *shape* decisions: `isFloat` returned true for a call to a name spelled `float`,
`floatValue`/`floatEval` folded it, `stringConst`/`stringVal` folded `str(...)`/`chr(...)` to
text, and each of those consulted no program state at all.

So the bug was not "built-ins shadow user functions" in the dispatch sense. It was that the
compiler read the *meaning* of a built-in into a call the program had claimed for itself.

## Decision

**One question, asked once: does the program define this name?**

1. **`builtinShadowed(name)`** (`pkg/lang/codegen.go`) reports whether the program defines that
   name itself (`g.funcs`/`g.fds`). `builtinCallAs(c, name)` is its call-shaped form: the callee
   must be that bare name *and* the program must not own it.

2. **Every shape decision keyed on a built-in name goes through it** — `isFloat`'s call case, the
   `float`/`abs`/`min`/`max`/`sum`/`sqrt`/`floor`/`ceil` readings in `floatValue` and `floatEval`,
   and the `str`/`chr` folds in `stringConst`/`stringVal`. A built-in reading that survives is one
   the program has not claimed.

3. **Precedence: a fact about the program outranks a fact about the built-in.** A function the
   program defined that returns a float (`def half(x): return x / 2.0`) is registered in
   `floatFuncs`, and that must be consulted *before* the shadow guard — an over-broad first patch
   put the guard first, and `programs/floatfn.gy` (four lines long, in the ledger since long
   before this ADR) failed parity immediately. Correct order: `floatFuncs` → shadow guard →
   built-in name rules.

4. **Lift, don't assume.** With the fold gone, the program's call returns `i32` like any other
   call, and float arithmetic that had been fed a folded double must be fed `sitofp i32 … to
   double`, the same lift any plain int expression gets. Skipping this produced the mirror-image
   failure — an `i32` operand inside an `fadd`.

5. **Folding stays where nothing is shadowed.** `print(str(42))` still folds to a constant;
   `print(abs(-3))` still folds. The guard removes a reading, not an optimisation, and the
   ordinary program pays nothing.

6. **Non-`g` folds take the question as a parameter.** `stringConst`/`stringConstLen` are pure
   helpers with no `irGen`, and they are where the `str`/`chr` folds live, so they now accept a
   `shadowed func(string) bool` and callers pass `g.builtinShadowed` (or `nil` where no program is
   in scope). Threading the predicate through was preferred to making the folds methods: it keeps
   the helpers testable and makes the dependency on "which program am I folding" explicit.

## What this does not do

- It does not make `def print` or `def range` legal: those names fail at the parser (`def print(`
  → `expected identifier`), consistently on every path, so it is a refusal rather than a wrong
  answer. Recorded as roadmap R.9, because CPython accepts them and the difference will surprise
  somebody.
- It does not touch the checker's *own* keying defects: a module function and a method sharing a
  name still collide in the checker's bare-name table (roadmap R.8).

## Consequences

- `programs/shadowed_builtins.gy` is in the ledger with `oracle: "match"`: six shadowed built-in
  names, a call in print position, a call whose result enters float arithmetic, a shadowed `len`
  over a real list — `8 9 8 9.5 3 4 9 3 10` identically on the interpreter, in the compiled binary,
  and in CPython.
- `TestShadowedBuiltinBatteryCompiles` compiles and runs one program per name (18 names), because
  the folds live in different helpers: passing for `float` said nothing about `str`, and it was
  exactly that gap that hid `chr` and `str` behind `float` in the first measurement.
- `TestProgramFloatFunctionKeepsItsShape` exists because of the regression above: it pins that a
  program-defined float function is still float, and it is the reason the precedence rule is
  written down rather than left to memory.
- Assertions about *how a value prints* now follow the register and then the format global
  (`printFormatFor`). Whole-module substring searches found the GC report's `snprintf` and other
  prints instead — a lesson about IR-level assertions in a module that contains a runtime.

## Alternatives considered

- **Refuse to compile a program that shadows a built-in.** Rejected: CPython runs these programs,
  the interpreter runs them, and a compiler that refuses what its own interpreter executes is the
  same class of defect as Gap R.5 (the checker refusing mutual recursion) — a false refusal is a
  bug, not a guard rail.
- **Disable folding and shape inference for built-in names always.** Rejected: it would slow down
  every ordinary program to fix a case almost no program writes. The guard is per-name-per-program,
  and unshadowed `str(42)` still folds.
- **Make the shape decisions data-driven (ask the function's own inferred signature) instead of
  guarded.** Genuinely better and partially done (`floatFuncs`, `strFuncs` already come from the
  program); the remaining name-keyed folds are shortcut constant-folds that stay useful, and the
  full answer is the tagged value word of L11.6, which removes the need to guess a call's type from
  its name at all. This ADR is the correctness half; L11.6 is the representation half.
