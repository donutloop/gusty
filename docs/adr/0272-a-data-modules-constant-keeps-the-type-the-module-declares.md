# 0272. A data module's constant keeps the type the module declares

## Status

Accepted. Ships as one transparent read — `foldedModuleAttr` / `foldedModuleFloat` in
`pkg/lang/module_const.go` — consulted by the two predicates that decide how a value is written: `isFloat`
(the print formatter, the arithmetic, the double road's `floatValue`, a binding's slot) and
`negationOperandKind`/`signlessOperandKind` (ADR 0271's door, which serves `-x`, `abs(x)` and now a text the
module declares).

Closes roadmap **L11.6**'s "typed stdlib constants" clause — the row the Phase 11 census recorded in one
cell: `import math; print(math.PI)` → CPython `3.141592653589793`, interpreter ✅, compiled **`3`**.

Files what the same probe found and this commit does not fix: **Gap R.152** (a float written into a container
slot the program recorded as ints reads back as garbage) and **Gap R.153** (a verdict a data module declares
prints `1` on both engines).

## Context

`stdlib/math.gy` is data:

```
PI = 3.141592653589793
E  = 2.718281828459045
```

`import math` folds those into the importing program (`resolveImports`), and the compiled backend resolves
`math.PI` to the folded `*FloatLit` in exactly one place: `value()`, the function that *writes* a value.
Every other question about the expression was asked of the `*Attr` node itself, and an attribute has no kind
to answer with:

| program | CPython | before · interpreter | before · compiled |
|---|---|---|---|
| `print(math.PI)` | `3.141592653589793` | ✅ | `3`, exit 0 |
| `print(math.PI * 2)` | `6.283185307179586` | ✅ | `6`, exit 0 |
| `print(-math.PI)` | `-3.141592653589793` | ✅ | `-3`, exit 0 |
| `x = math.PI` / `print(x > 3.14)` | `True` | ✅ | `False`, exit 0 |
| `print(abs(NAME))` (a text the module declares) | `TypeError: … abs(): 'str'` | that sentence | `0`, exit 0 |

Two things made it survive as a "stdlib bug". The interpreter was right — it evaluates the module and reads
the value back — so the two-backend matrix had one leg agreeing with the oracle and no complaint; and the
program one line away, `pi = 3.141592653589793` written by hand, was right on both legs, so the difference was
not the number, it was *where the kind came from*. When the compiler has to decide between an `i32` and a
`double` and the answer is written in a file it did not read, it guesses the word it always has.

The same shape at a container boundary is the reason this row was not fixed as a one-liner in `value()`: the
store and the read ask the same question two places, and moving one without the other changes a wrong answer
into a different wrong answer (see Consequences, Gap R.152).

## Decision

**One read, five callers.** `foldedModuleAttr(e)` resolves `mod.NAME` to the expression the module declares,
through the same `g.imports.Globals` map `value()` already consults — so the value that is written and the
kind that is asked about come from one bookkeeping, not two answers that can drift (`module_const.go`'s
header). `foldedModuleFloat` is the float question over that read, delegating to `isFloat`, which is the
predicate that already owns the answer for every other expression.

**The predicates ask; the lowering does not decide.** `isFloat` gained a `*Attr` arm that reads through the
fold, which is what simultaneously fixes the print formatter (`3.141592653589793`), the arithmetic (`* 2`,
`/ 2`, `// 1`), the binding that decides a slot's word (`x = math.PI` then `x > 3.14`), and the double road's
`floatValue` — whose generic fallback (`sitofp i32 <value>`) is what turned the double into `3.0` once the
formatter alone was fixed. The first draft fixed the formatter and printed `3.0`: half-fixed is a *new* wrong
answer, which is why the test table covers positions rather than operators.

**The signless door serves a raise too.** ADR 0271's `negationOperandKind` asks the same read, so a text the
module declared is a text for `-x` and for `abs(x)` and both raise CPython's sentence at the trap exit,
catchable — instead of the compiled leg writing `sub i32 0, <interned index>` and printing `0`.

**A name the program binds is the program's.** The read declines when the importing program owns the name
(`g.moduleNames` / `g.moduleConsts`), and declines when the module declares no such attribute — it never
invents a value. That is the same ordering the call path uses for a shadowed builtin (Gap R.6): the program's
own binding outranks the table.

## Measurement

Before/after are the same sources through `gustyc --file <path> --interp` and `--aot`; the reference is the
twin (a class with the same names and values, the program's body unchanged), asserted in
`integration/module_const_test.go`.

| program | reference | before · compiled | after · both engines |
|---|---|---|---|
| `print(consts.PI)` | `3.141592653589793` | `3` | `3.141592653589793` |
| `print(-consts.PI)` | `-3.141592653589793` | `-3` | `-3.141592653589793` |
| `print(consts.PI * 2)` | `6.283185307179586` | `6` | `6.283185307179586` |
| `print(consts.PI / 2)` | `1.5707963267948966` | `1.5` | `1.5707963267948966` |
| `x = consts.PI` / `print(x > 3.14)` | `True` | `False` | `True` |
| `print(consts.PI > consts.E)` | `True` | `False` | `True` |
| `print(str(consts.PI))`, `f"{consts.PI}"`, `round(consts.PI, 2)` | `3.141592653589793`, ditto, `3.14` | `3.0`, `3.0`, `3.14` | the reference's bytes |
| `print([consts.PI])` | `[3.141592653589793]` | `[3.0]` | `[3.141592653589793]` |
| `ys.append(consts.PI)` / `print(ys[0])` | `3.141592653589793` | `3` | `3.141592653589793` |
| `print(consts.NAME)`, `len(consts.NAME)`, `consts.NAME + "!"` | `red`, `3`, `red!` | already right | unchanged |
| `print(abs(consts.NAME))` / `print(-consts.NAME)` | `TypeError: … abs(): 'str'` / `… unary -: 'str'` | `0`, exit 0 | the reference's sentence, exit 3, catchable |
| `print(consts.N)` (an integer) | `7` | `7` | `7` |
| `import math` probe (`print(math.PI)`, `print(math.E)`) | `3.141592653589793`, `2.718281828459045` | `3`, `2` | the reference's own numbers |
| `x = 0` / `print(x or math.PI)` | `3.141592653589793` | refused, exit 1 ("no word") | `3.141592653589793` on both legs |

The last row is the interesting one for the roadmap: that refusal existed because the pass could not name the
kind. It could only ever have been a workaround for the missing read, and the read retired it — the row moved
from `TestTheShapesWhoseAnswerHasNoWordRefuseThemInWords` to the parity table in the same commit.

## Consequences

- `programs/probe_math_const.gy` keeps its `not_applicable` ledger row — the reference spells `math.pi`, so
  the source is not a CPython program — but its per-leg pins move from `3\n2` to the two real numbers, and
  `integration/module_const_test.go` asserts the twin's own spelling beside them.
- `docs/language.md` § On-disk stdlib modules states the rule and shows the four positions.
- **Gap R.152** is what this commit deliberately did *not* fix: `xs = [0]` / `xs[0] = 3.14159` / `print(xs[0])`
  prints `1` on the compiled leg — the store goes through the container's integer road while the tag says
  `float`, so the printer follows a box handle that was never a box. It is the same defect whether the double
  is written by hand or comes from a module, which is the evidence that the two roads disagreed before this
  commit and not because of it. `ys.append(f)` answers correctly, through ADR 0232/0233's promotion path, and
  is the model for the fix.
- **Gap R.153**: `ON = True` in a data module prints `1` on both engines — `IsBoolExpr` is an AST question and
  has no arm for a folded attribute. `BoolEnv` already carries four such hooks; the fifth is this read.
- The container-slot shape is why the fix is a read and not a widening of one predicate: the store and the
  read must ask the same door, or the payload arrives truncated while the tag claims a box.

## Agentic rationale

- The rule is discoverable: `gustyc --lang` and `docs/language.md` state that a data module's constant keeps
  its declared type, so an agent reading the surface does not have to discover the integer truncation by
  compiling `print(math.PI)`.
- The fold is one function, and it is asked *as a function* in the unit tests — `foldedModuleAttr` resolving a
  declared constant, declining an undeclared one, declining a name the program owns — which is the machine
  path for "what does the compiler believe about this expression" (`TestFoldedModuleAttrIsOneReadNotTwo`).
- Exit codes stay the contract: the traps in the table are exit 3 and catchable, no shape regressed to exit 2,
  and the tests forbid it (`assertNoForbiddenIR`, and the CLI table's exit-2 fatal).

## Alternatives rejected

- **Fix `value()` only.** It was already correct; the wrong word came from the predicates. Fixing the print
  formatter alone produced `3.0` — measured, not hypothetical.
- **A stdlib-specific table of constant types** (a hand-written list saying `math.PI` is a float). That is a
  second answer to a question the module already answers in its own source, and it drifts the moment anyone
  edits `stdlib/*.gy` — the failure mode ADR 0182 and ADR 0265 exist to end.
- **Refusing `mod.NAME` in a numeric position.** The pass *can* name the kind; a refusal would be a
  capability claim about a thing the fold already knows, and it would break the `x or math.PI` programs that
  now answer, one row after ADR 0269 retired an earlier refusal for the same reason.
- **Widening the container-slot read to match the store's truncation** (keeping `print(xs[0])` on the int
  formatter). It preserves a wrong answer to keep a diff small; the honest move is to file the disagreement
  (Gap R.152) with both spellings measured, which is what shipped.
