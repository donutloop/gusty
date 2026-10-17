# 0370 — An applied `lambda` is a function this backend can build a call to

## Context

Two sentences about `lambda` were refused, and each was refused for a reason the other did not share.

The first is the pass's own. `(lambda f: f(3))(lambda x: x * 2)` is the reference's `6`; here it exited 1
naming `"f"` as "a callee this backend cannot build a call to" (roadmap Gap R.265, measured 2026-10-15
writing ADR 0345's tables). ADR 0345 already clones a higher-order `def`: `def twice(fn, v): return
fn(fn(v))` is specialised once per tuple of functions `fn` is asked to be, and the body's call names the
clone. A `lambda` with the same body had no declaration to clone, so the pass never looked at it — the
higher-order body was there, the application was there, and only the name was missing.

The second is the frame's. `def f(x): return (lambda y: y * 2)(x)` is the reference's `8`; here it died with
`undefined name "x"`, and the identical program written with a **nested `def`** answered `8` on the same
build (roadmap Gap R.276). That contrast is the diagnosis: a generated `define` cannot nest inside the
`define` the lambda is written in, so the lambda's body has no frame to read — and the road that emits a
`lambda` used to write its `define` into `g.globals`, the very stream the class road and the closure lift
are writing their own `define`s into.

Measured on the commit before this one, with the same programs:

| source | reference | this backend, before |
|---|---|---|
| `print((lambda f: f(3))(lambda x: x * 2))` | `6` | exit 1 — `"f" is called but is not a function this backend can build a call to` |
| `def apply1(fn, v): return fn(v)` / `print(apply1(lambda x: (lambda y: y * 2)(x), 4))` | `8` | exit 1 — `undefined name "x"` (the inner lambda was hoisted away from the parameter it read) |
| `def f(x): return (lambda y: y * 2)(x)` / `print(f(4))` | `8` | exit 1 — `undefined name "x"` |
| `def outer(x): y = x + 1 / return (lambda z: z + y)(2)` / `print(outer(3))` | `6` | exit 1 — `undefined name "y"` |
| `def outer(): b = 42 / f = lambda: b / return f()` / `print(outer())` | `42` | exit 1 — `undefined name "b"` |
| `def counter(): total = 0 / def bump(n): return (lambda m: m + total)(n) / return bump(5)` | `5` | exit 1 — `undefined name "total"` |
| `class Foo: def bar(self, x): return (lambda y: y + 1)(x)` | `4` | exit 1 — `undefined name "x"` |
| `print((lambda m: m / 2)(3))` | `1.5` | **exit 2** — `llc`: `'%t9' defined with type 'double' but expected 'i32'` |
| `def outer(): def bump(n): return 1 / 0 / return bump(5)` (no `lambda` at all) | `ZeroDivisionError` | **exit 2** — `llc`: `use of undefined value '%outer.raiseexit'` |
| `class Foo: def bar(self): return (lambda y: y + 1)(3)` (the same body, no capture) | `[ok]` | **exit 2** — `llc`: `multiple definition of local value named 'gc.frame.base'` |

The last three are the shape of the whole change: a `lambda` is emitted by *lowering it into a `define`*,
and every one of those three failures is a `define` that was emitted in the wrong place, with the wrong
word, or with the wrong unwind label — the ADR 0166 class, where the compiler's own output is the bug.

## Decision

**A `lambda` is a function declaration in disguise, and both roads that know functions get asked about it.**

1. **The pass clones an applied `lambda`'s body** (`pkg/lang/callable_value.go`). A lambda applied where it
   is written, whose body calls one of its own parameters, is registered as a higher-order function: it is
   given a declaration, cloned once per distinct tuple of functions its called parameters are asked to be,
   and the application is pointed at the clone. The clone is keyed on the **caller** node, not on the
   bodied callable — keying it on the inner `Lambda` was this cycle's first wrong turn: the plan then
   rewrote the call sites of a lambda nobody calls, the enclosing body was never cloned, and its parameter
   came back as `undefined name`. A lambda whose called parameter is asked to be a predeclared name is
   declined: the pass clones the program's functions, and a call of a builtin is the question the value word
   of L11.1 would answer.
2. **A reader meets the clone by the reference's own name.** The symbol the declaration carries is internal
   (`callableCloneInfix`/`callableLambdaInfix`); the reader-facing diagnostic — tracebacks, `refused`,
   `higher_order_functions` — says `<lambda>`, the word CPython prints for the same function
   (ADR 0320's rule for a rewritten call, applied to a rewritten definition).
3. **A `lambda`'s capture list is the set of names its body is free in** (`lambdaFreeNames` in
   `pkg/lang/codegen_func.go`), computed by a scope-aware scan: a nested `lambda`'s parameters and a
   comprehension's target belong to those scopes, and a nested `def` is that `def`'s scope and is not
   scanned at all. Each capture crosses as one extra parameter of the generated `define`, filled at the call
   site by the road that already builds those arguments (`lambdaCallArgs`), from **the caller's own reading**
   of the name — the same read the frame makes anywhere else in its body.
4. **The capture list is asked with the binding rule, not the table rule.** `closureInfoFor` — the nested
   `def` road's question — keeps only the names the enclosing frame's tables already list, which is right
   for a `def` whose captures the frame hands it and wrong for a `lambda`, whose enclosing frame is a
   `define` with no table for the names it owns. Asking the table rule deleted `x` from
   `(lambda x: (lambda y: (lambda z: x + y + z)(100))(10))(1)` before the frame was ever consulted. The
   companion fix is in `collectNames` (`pkg/lang/closure.go`), which had no `*Lambda` arm: a closure body
   whose only read of `total` sits inside a `lambda` was handed an **empty capture list**, which is why
   `def counter(): total = 0 / def bump(n): return (lambda m: m + total)(n)` could not see `total` even
   though the nested-`def` road owned it.
5. **A name the module binds is not a capture.** A generated `define` reaches module storage the way any
   other function does; taking module names as parameters would change what programs that compile today
   pass across the boundary (Gap R.35 owns module state).
6. **The frame goes in and comes back out unchanged.** `funcDef` is written as the outermost road for one
   function: it clears the parameter table, the current function, the generator handle, the environment
   captures and the root-frame flag on the way out. A `lambda` emits a `define` *in the middle of a body*, so
   it saves that state (`lambdaFrameState`, `enterFrame`/`leave`) — the same courtesy `emitClosureDef` has
   always paid. The closure and method roads, which emit a body without passing through `funcDef`, now carry
   the frame's own `curFD` (so a capture can ask that declaration what its calls hand to the parameter being
   captured), and the closure road gives its body a `raiseexit` label **of its own**: it used to inherit the
   enclosing function's, so a raise written in a nested `def` branched to a block that does not exist in its
   function.
7. **The generated `define` is emitted into a stream of its own** (`g.lambdaDefines`), written beside the
   other module-level text. Appending it to `g.globals` as soon as it was ready nested a `lambda` inside the
   `define` of the method or closure that contained it, and `llc` answered `multiple definition of local
   value named 'gc.frame.base'` for a program the reference runs.
8. **A `lambda`'s answer word is decided by the declaration, once.** `lambdaFuncDef` is the single spelling
   of what a `lambda` becomes; the emit road builds it, and `isFloat`/`floatValue` ask the *same* declaration
   what an applied lambda answers with. Before that, the call site emitted `call double @gy_lambda_0` while
   the print door handed the same value to `%d` — which is what made `print((lambda m: m / 2)(3))` an exit 2.
9. **A value that cannot travel as one word is refused, never answered with the word.** The gate
   (`captureCarriesOneWord`, `valueCarriesOneWord`, `captureParamWord`) asks the tables the print door and
   the arithmetic roads already read, of the captured name **and of every expression a call hands to the
   parameter that name is bound to**: a container, a tuple, a text, a bool, `None`, a tagged or union value
   and a `double` are refused, each named by its kind, with the door that does answer (`write the body as a
   nested def`) and the row that would carry it (L11.1). A float answered from inside a closure or a method
   is refused too — those roads emit an `i32` `define` whatever the body does, so the double has no word to
   return in. ADR 0249's rule: a door that cannot answer says so rather than matching its neighbour, even
   though the neighbouring `def` road has the same hole for a container argument (L11.1's, filed beside this
   row rather than patched here).

## Agentic rationale

`gustyc --callables` now reports `higher_order_lambdas`: how many `lambda`s the pass gave a declaration and
a clone to. A script can therefore tell "this program has no higher-order `lambda`" from "it has one and the
pass could not prove it" — the second says so in `refused`, with the row (`L11.7 · Gap R.265`) attached, and
the JSON schema (`callablePassReport`) declares the field. The `<lambda>` spelling is what a reader and an
agent alike see; the minted symbol never leaves the account's `symbols` list.

## Codegen / IR implications

- Every `lambda` now costs one `define` in the module's generated region, with `len(params)+len(captures)`
  `i32` parameters; an application passes the written arguments and then the captures, in the order the scan
  sorted them (the sort is what makes the module reproducible, ADR 0273).
- A higher-order `lambda` costs a clone per tuple of functions, named `<name>$callable$<hash>_<n>` under the
  `callableCloneInfix` spelling, exactly as a higher-order `def` does.
- No probe module introduces an indirect `call`: every `call` in the emitted module names a `@function`
  (asserted by `TestWhatAClonedLambdaIsEmittedAsAFunctionAndCalledByName`).
- Closure bodies gained their own `.raiseexit` block; a body that never raises pays one unreachable block.
- `g.lambdaDefines` is written in the same region as `g.strGlobals`, ahead of the `globals` region whose
  offsets the debug pass resolves (ADR 0231), so line tables are unaffected.

## Alternatives rejected

- **A real closure object (`fnptr`, ADR 0345's first rejected alternative).** It would answer
  `mk(1)(2)` and the two-lambda capture in one stroke, and it is L11.1's row, not this one: a callable that
  carries its own kind word is the same missing machinery as every other value that has to *be* a value
  here. What this ADR does instead is decide, in words, which values may cross into a generated function.
- **Reusing `closureInfoFor`'s capture set.** Rejected after measurement, not before: it is filtered by what
  the enclosing frame's tables list, and the frame a `lambda` is written in is a `define` that lists only
  what its own body binds. The filter is the road's, not the language's.
- **Answering the non-word captures with the raw word.** The first build of this road did, and printed `0`
  for `[1, 2]`, `1` for `True`, `0` for `None` and a truncated number for a float, each at exit 0. That is
  the failure mode ADR 0273 was written against; the gate is the reason those four shapes are in the refusal
  table and not in the answer table.
- **Answering `lambda` bodies by interpreting them.** There is no second engine to interpret with (ADR 0302),
  and a value decided twice, in two languages, is a program with two answers.

## What stays owed

Four rows, each measured against the reference in this cycle's tables and each refused here in words that name the
missing half and its row. None of them is answered by a word this ADR could have written and chose not to.

- **Gap R.329** — an application that omits a `lambda`'s defaulted parameter is not an entry the arity audit will
  build: `print((lambda f, y=10: f(y))(lambda x: x + 1))` refuses where the reference prints `11`, while the same body
  applied with both arguments answers `11` at exit 0. The entry tuple should be the one the *body* asks for.
- **Gap R.330** — a capture three frames up has no call site to ask:
  `print((lambda x: (lambda y: (lambda z: x + y + z)(100))(10))(1))` refuses (`that name holds a value no call in the
  program can be asked about`) where the reference prints `111`; the two-frame twin answers `3`. The fix is to carry
  the capture through the frame that owns it, the way a nested `def` does.
- **Gap R.331** — a `lambda` applied at module level whose body reads a module-level name of a non-number kind answers
  the payload word at exit 0: `t = "hey"` above `print((lambda: t)())` prints `0` where the reference prints `hey`, and
  `b = True` above `print((lambda: b)())` prints `1` for `True`. The pre-ADR-0370 binary prints the same two bytes, so
  this is a filed row and not a regression; the same programs written inside a `def` are refused by this ADR's gate,
  which is what localises it to the module road and to the return-kind registration `funcReturnsFloat` templates. They are
  not on the record either, and cannot be while the legs disagree — `tools/recmerge` cross-checks the compiled answer
  against the reference's before it writes one — so `TestWhatTheAppliedLambdaRowsLeaveStandingIsFiledNotFixed` carries the
  reference's bytes beside today's until the tag travels.
- **Gap R.332** — a captured name whose value is not a number has no word to travel in: the container, text, bool and
  `None` forms (`def f(): xs = [1, 2]` / `return (lambda: len(xs) + 1)()`) refuse where the reference prints `3`, `hey`,
  `True` and `None`. This is L11.1's tagged word seen from the capture side; ADR 0368's (payload, tag) pair, two words
  at the call site instead of one, is the shape of the fix.

- **Gap R.333** — the same module-level read is also a *broken module*: `t = "hey"` above `print((lambda: t)())` emits
  `call i32 @rt_str_intern2(i8* @.str1, i8* @.str2)` inside `define i32 @gy_lambda_0()` with no definition of that
  runtime entry anywhere in the file, `llc-20` rejects both the linked and the in-process-JIT module with
  `use of undefined value '@rt_str_intern2'` — and the toolchain prints `0` and exits **0**. ADR 0166 makes an `llc`
  rejection exit 2, the code that says the compiler is broken; here it was the run the program got. The body written as
  a `def` answers `hey`, which localises both halves (the missing runtime-block registration and the exit code that
  hid it) to the lambda road's emission, and the pre-ADR-0370 build prints the same `0` at the same exit code — which
  is why both this bullet and the one above are filed rows rather than part of this decision.

**Gap R.266** is unchanged and unapproached by this ADR: a `lambda` that is neither applied nor called — used as a
value, returned from a function, stored in a container — is still a value no door opens, and still says so.
