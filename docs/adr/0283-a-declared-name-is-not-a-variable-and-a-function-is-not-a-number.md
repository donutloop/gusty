# ADR 0283 — a declared name is not a variable, and a function is not a number

Date: 2026-10-06
Status: Accepted
Roadmap: closes the exit-2 half of **Gap R.150** and **Gap R.151**; files **Gap R.167** (function values
as a feature) and **Gap R.168** (the un-checked closure call road).
Depends on: ADR 0266 / 0271 (the numeric door that asks *what kind is this operand?*), ADR 0275's
`unsupportedNumberOp` table (one wording per operation), ADR 0211 / 0228 (a wrong **class** breaks
catchability), ADR 0166 (exit 2 is the compiler's bug), ADR 0181/0220 (a `def` emits a global and
allocates no slot).

## The measure

`python3` 3.12.3 is the oracle; `--interp` and `--aot` are the two engines. With
`head = def f(x): return x * 2` at module level:

| program | CPython | `--interp` before | `--aot` before | `--aot` after |
| --- | --- | --- | --- | --- |
| `print(f + 1)` | TypeError 'function' and 'int' | NameError | **exit 2** | exit 1, names the missing value |
| `xs = [f]` / `print(len(xs))` | `1` | NameError | **exit 2** | exit 1 |
| `print(str(f))` | `<function f at …>` | NameError | **exit 2** | exit 1 |
| `print(f)` | `<function f at …>` | NameError | **exit 2** | exit 1 |
| `print(math + 1)` after `import math` | TypeError 'module' | NameError | **exit 2** | exit 1 |
| `print(lambda x: x)` | `<function <lambda> …>` | `<closure>` | **exit 2** | exit 1 |
| `print(abs(f))`, `print(-f)` | TypeError 'function' | NameError (wrong class) | **exit 2** | **exit 3, CPython's sentence** |
| `print(abs(lambda x: x))`, `-(lambda x: x)` | TypeError 'function' | CPython's sentence | **exit 2** | **exit 3, CPython's sentence** |
| `print(abs(math))`, `-math` | TypeError 'module' | CPython's sentence | **exit 2** | **exit 3, CPython's sentence** |
| `g = f` / `print(g(21))` | `42` | NameError | exit 1 | `42` on the interpreter |
| `apply(twice, [1,2])` | `[2, 4]` | NameError | exit 1 | `[2, 4]` on the interpreter |
| `print(f(1, 2))`, `print(f())` | TypeError (arity) | refusal | refusal | refusal (**never a number**) |

Seven rows left the compiler through exit 2 — `llc` rejecting the module for a program the reference
answers in one line. That is the class ADR 0166 calls unforgivable, and it was the whole row.

## The diagnosis

Two mechanisms, one mistake.

**A declared name was read as an assigned one.** `value()`'s `*Name` case ends in
`%_f.ld1 = load i32, i32* %_f`, reached because `nameIsBound` answers *yes* for `g.funcs[nm]` — the same
map that keeps a call legal. But a `def` emits a global `@gy_f` and **allocates no slot**; there is
nothing to load, and `llc-20` says `use of undefined value '%_f'`. Gap R.150 had the identical mechanism
for built-ins (`%_len`) and imported modules (`%_math`); a function name is the third name that is not a
variable.

**A `lambda`'s global was used as its value.** `value()`'s `*Lambda` case returned the generated name
from `emitLambda` as if it were an i32, so `print(lambda x: x)` emitted
`printf(i8* …, i32 lambda_0)` and `-print(abs(lambda x: x))` reached
`%t4 = sub i32 0, lambda_0` — the instruction family ADR 0271 deleted for container globals, arriving
under a new name. Gap R.151 measured the arithmetic half; the printf half is the same bug wearing a
different operand slot.

The mistake was never "the program is unusual". It is that two roads (the numeric door, the print road)
ask *what kind is this?*, and this shape had no answer, so it fell through into arithmetic that had to
guess.

## The decision

**Ask the same question, and answer it in one place.** `nameIsAValueWithNoSign(name)` names the kinds a
program can put in a value position that have no slot, no payload and no sign — `function` for a `def`'d
name, `module` for an imported one — and it is consulted at **the read**, in `value()`'s `*Name` case,
not in each road. The first draft put the guard in the arithmetic roads and `f + 1` still exited 2:
the float/pair road lowers its operands *before* the operator asks anything, so by the time the guard
ran the load was already in the module. One choke point is the only position that covers every road.

Three consequences of that choice, each measured rather than assumed:

* **A call still resolves.** `nameIsAValueWithNoSign` asks `params` first, so a parameter holding a
  callable is the program's own value and keeps its meaning; `twice(lambda x: x * 3, 2)` answers `12` on
  both engines. Without that the "fix" would be a ban on functions as arguments.
* **A called `def` keeps the checked road.** See below.
* **A lambda that is *called* never reaches the read**: the callable positions register their `FuncDef`
  at their own sites (`emitLambda` is reached from three of them), so the refusal fires only for a lambda
  genuinely used as a value.

**In the numeric door, raise — don't refuse.** `-*` and `abs` already own a door that asks the operand's
kind (ADR 0266/0271), so `*Lambda` and a declared `*Name` answer `"function"` / `"module"` there and the
existing emitters write CPython's sentence with CPython's word: `bad operand type for abs(): 'function'`,
`bad operand type for unary -: 'module'`. A front-end refusal would be a *different verdict* from the
reference's, would escape `except TypeError:`, and would run the wrong arm on the two engines — ADR 0211's
misclassing. The binary roads raise too, source-ordered (`f + 1` quotes `'function'` then `'int'`).

**A top-level `def` binds its own name** in the interpreter, to the same closure handle a `lambda` bound
to a name has always produced. Reading a name the program just declared was `NameError: name 'f' is not
defined` — the wrong class, catching nowhere — and is now `<closure>` for a print and a proper `TypeError`
for a numeric use. `apply(twice, [1, 2])` answers CPython's `[2, 4]`.

## The regression this cycle caught, and what it cost

Binding the name is what made `f(1, 2)` silently **answer `2`** at exit 0 (and `f()` answer `0`). The
cause was an ordering I introduced: the call road consults `e.Vars` for a closure value *before*
`e.funcs`, and the newly-bound name meant every `def`'d call fell into `callClosure`, which evaluates its
arguments and pads or drops them without asking. CPython raises `takes 1 positional argument but 2 were
given`; the program was printing a number.

That is the ladder's forbidden trade — a working answer becoming a wrong number — so the fix was not to
undo the binding but to reorder the roads: **a name the program declared with `def` calls the checked road
first**, and the closure value is consulted after, which is where `g = f` and `g = lambda …` live. The
pin is a test that fails on silence: `TestACalledDefNameKeepsTheCheckedRoadIsTheLadderRule` refuses to
pass if either program produces output at all, and the CLI row fails on `code == 0`.

The un-checked `callClosure` road itself remains, and is **Gap R.168**: `g = lambda x: x * 2` then
`g(1, 2)` prints `2` and `g()` prints `0` on both binaries today, so it is pre-existing — verified against
the pre-cycle binary rather than assumed.

## Alternatives rejected

* **Give functions real values** (a function object, printed `<function f at …>`, callable anywhere).
  Rejected for this cycle: `int64` handles have no spare kind for a callable the module declared, and
  half-implementing it is how a name becomes loadable-but-wrong. This is **Gap R.167**, and the exit-2
  removal is what makes it safe to build — the road is now refused where it cannot answer, so a partial
  feature can't reintroduce a silent load.
* **Refuse at the arithmetic roads only.** Rejected by measurement: `f + 1` still exited 2, because the
  operand is lowered before the operator is consulted.
* **Keep `e.Vars` consulted first and add an arity check inside `callClosure`.** Rejected: `callClosure`
  is the *lambda value* road, and its arity behaviour is Gap R.168's own measured defect — fixing it here
  would hide a filed row inside an unrelated commit and leave the two roads' differing arity vocabularies
  (`expects 1 argument, got 0` vs `missing argument x`) unexplained.
* **Leave the interpreter's `NameError`** as "close enough". Rejected: ADR 0211/0228 — a trap of the wrong
  class catches nowhere, so `except TypeError:` would run neither arm of a program that has an `except`.
* **Reuse `"object"`** as the kind for both. Rejected: the reference says `'function'` and `'module'`, and
  the interpreter's `operandKind` already produces both words, so one word per engine was free.

## Agentic rationale

Before: an agent that got exit 2 from `--aot` could not tell whether its program was unusual or the
compiler was broken, and `print(lambda x: x)` looked like a program bug. After: exit 2 is gone from this
family, exit 1 names the missing value and says what the reference answers (`the reference answers `f`
with a function object`), and exit 3 carries CPython's sentence verbatim so a harness can match on
message text. The refusal also states the boundary of what *does* work ("a lambda that is called … is
answered by both engines"), which is the sentence an agent needs to plan a rewrite without probing again.

## Codegen / IR notes

* No module in the corpus now contains `load i32, i32* %_<declared name>` — pinned by
  `undefinedSlotLoads` in the unit table, the same style as ADR 0271's module-wide assertion.
* `printf(…, i32 lambda_0)` and `%t4 = sub i32 0, lambda_0` are emitted nowhere; the closure global is
  only ever a `define`, never an operand.
* The raise path is the existing `rt_raise` one; the numeric rows assert `rt_raise` + `TypeError` in the
  module, so a future edit can't quietly convert the trap back into a refusal.

## Tests

* `pkg/lang/fn_value_test.go` — `TestADeclaredNameUsedAsAValueNeverReachesLLVM` (13 rows, incl. the
  never-allocated-slot assertion), `TestTheNumericDoorNamesAFunctionTheWayTheReferenceDoes` (6 rows ×
  both engines, class + message against CPython), `TestADeclaredNameReadIsNotANameErrorOnTheInterpreter`
  (6 rows incl. catchability of the new raise and a genuinely-undefined name),
  `TestACalledDefNameKeepsTheCheckedRoadIsTheLadderRule` (the arity rows, failing on silence),
  `TestAParameterHoldingACallableIsNotRefused` (the narrowing).
* `integration/fn_value_test.go` — the same families at the CLI with exit classes pinned (2 forbidden),
  each row recording what `python3` said.
* `integration/programs/probe_a_function_as_a_value.gy` — five CPython lines whole on the interpreter;
  registered as a **debt** row because the compiled leg still refuses the body that calls a parameter
  (`unsupported call "f"`, L11.7's higher-order limit — recorded, not hidden).
* `integration/programs/probe_fn_name.gy` promoted from *both legs fail* to *interpreter answers*: its
  pin moved from `Missing` to `[2, 4]`, which is the drift the matrix caught and the price of the fix
  being real. Matrix 151 → 152 rows, 100 oracle `match`, 0 fail, 0 drift; the 161-file corpus sweep moved
  only the two intended probes — no number or wording changed anywhere else, and the pre-existing
  `int()`/`float()` panic (Gap R.131) is byte-identical on both binaries.
