# ADR 0349 — A signed constant is stated by its bits: the sign of −0.0 is a property of the number, not of the road that made it

Status: accepted
Date: 2026-10-15
Decomposes: [ADR 0264](0264-a-float-literal-that-cannot-be-spelled-is-a-float-that-has-no-name.md),
[ADR 0263](0263-the-digit-count-of-round-is-a-compile-time-question-with-a-run-time-answer.md),
[ADR 0236](0236-one-rule-two-implementers-is-how-a-trap-gets-two-answers.md),
[ADR 0266](0266-the-signless-operations-ask-one-kind-question-and-answer-with-cpythons-own-sentence.md)
Roadmap row: Gap R.132
Refuses to be: a special case for zero.

## The gap, as measured

Four lines, one program, two answers:

```gusty
print(-0.0)             # CPython -0.0 · compiled 0.0   ← the compiler wrote it
y = -0.0
print(y)                # CPython -0.0 · compiled 0.0   ← the compiler wrote it
print(0.0 * -1)         # CPython -0.0 · compiled -0.0  ← the target computed it
print(round(-0.5, 0))   # CPython -0.0 · compiled -0.0  ← the target computed it
```

Where the target *computed* a double the sign survived; where the *compiler* wrote one it did not. Not a
rounding rule, not a formatter: one instruction, repeated at thirteen sites in `pkg/lang/codegen.go`, that
materialises a compile-time double into an SSA value.

```llvm
  %t1 = fadd double 0.0, -0.000000e+00
```

A no-op addition, except in the one case that matters: IEEE 754 answers `−0.0 + +0.0` with **+0.0** under
round-to-nearest. The sign was destroyed before the module existed, so `rt_fmt_double` — the runtime's float
renderer, which handles negative zero correctly, which has always handled it correctly — was never handed
anything to render. A wrong answer at exit 0, which is the class this compiler treats as its worst outcome
(AGENTS.md, ADR 0166's exit-2 rule being its neighbour).

## The decision

**A value with its sign bit set is stated by that bit pattern.** `materialiseFloat(b, t, v)` is the one
helper at all thirteen sites:

```go
func materialiseFloat(b *strings.Builder, t string, v float64) {
	if math.Signbit(v) {
		fmt.Fprintf(b, "  %s = bitcast i64 %d to double\n", t, math.Float64bits(v))
		return
	}
	fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(v))
}
```

Two choices inside four lines are the decision, and each has a reason that is not taste.

**Bits, not a decimal literal.** This is ADR 0264's road — that ADR already states `inf` and `nan` by their
IEEE patterns because LLVM's parser takes no keyword for them — extended to the case where the *meaning* of
the spelling is what breaks rather than the parser. A bit pattern cannot round, cannot be re-parsed, cannot be
re-associated by a later fold, and cannot lose a sign nobody thought to check. It is the number.

**Decimal digits for the `i64`, not `0x…`.** LLVM's lexer reads a bare hex token as a *floating-point*
constant whatever type token precedes it: `bitcast i64 0x8000000000000000 to double` fails in `llc` with
"floating point constant invalid for type" — an assembler failure on a module the emitter believed, which is
exit 2 and the one outcome this compiler is never allowed to produce. The first version of this patch shipped
that bug and `llc` found it in the JIT leg within seconds. Decimal is the only spelling that is an integer to
the parser.

**The unsigned cases keep the addition.** `+0.0 + x` is `x` exactly for every finite value, for infinity and
for NaN, so nothing about them was ever wrong; a large body of pinned IR (and several `--emit-llvm` lit rows)
knows that shape. This row promises something about the sign, and a change that re-earns 13 sites of IR to
make a promise it does not need is a change a reviewer cannot audit.

## The second half, which the record found and the fix did not

`abs` over a slot the run time owns is `rt_num_arith`'s signless call (ADR 0266). Its magnitude was:

```llvm
  %isnegv = fcmp olt double %af, 0.000000e+00
  %absv   = select i1 %isnegv, double %neg, double %af
```

`fcmp olt x, 0.0` is **false** for −0.0. The door handed −0.0 back where CPython answers `0.0`.

This was not introduced here, and that is the part worth keeping. It was *masked* by the bug above: while the
emitter turned every constant −0.0 into +0.0 on the way to the store, the float box never held a negative
zero, so the door was never asked. The moment ADR 0349 stated the sign honestly, the record leg caught it:

```
NEW divergence: "xs = []\nxs.append(-0.0)\nn = xs[0]\nprint(abs(n))\n" — owed "0.0\n", got "-0.0\n"
```

The magnitude is now the operand's bits with the sign cleared:

```llvm
  %abits = bitcast double %af to i64
  %amagbits = and i64 %abits, 9223372036854775807
  %absv = bitcast i64 %amagbits to double
```

— which is what `llvm.fabs` means, leaves NaN's payload alone, and unlike a comparison against zero has no
values it answers incorrectly. The comment above it in `rtNumArithRuntimeIR` records why a select over an
`fcmp` is not the same thing, because the instruction that looks equivalent is the reason it took two cycles.

**And the mask is spelled out as a constant in the test, because a hand-typed mask is a silent bug.** The
first version read `9218868437227405311` (`0x7FEFFFFFFFFFFFFF`) instead of `9223372036854775807`
(`0x7FFFFFFFFFFFFFFF`). It still printed `0.0` for `abs(-0.0)` — clearing more bits than intended still clears
the sign, which is the case the fix was written for — while printing `2` for `abs(-4)`, `3` for `abs(7)` and
`0` for `abs(True)`, because it had eaten the exponent as well. `TestAConstantNegativeZeroKeepsItsSignWhenPrinted`
pins the int and bool families of that same door beside the float one, and
`TestTheRuntimeAbsDoorClearsTheSignBit` pins the mask by name. A row that tests only the case it was written
for tests nothing.

## Behaviour

| program | before | CPython | after |
|---|---|---|---|
| `print(-0.0)` | `0.0` | `-0.0` | `-0.0` |
| `y = -0.0` / `print(y)` | `0.0` | `-0.0` | `-0.0` |
| `print(str(-0.0))` | `-0.0` | `-0.0` | `-0.0` |
| `print([-0.0, 0.0])` | `[0.0, 0.0]` | `[-0.0, 0.0]` | `[-0.0, 0.0]` |
| `def p(v): print(v)` / `p(-0.0)` | `0.0` | `-0.0` | `-0.0` |
| `xs=[]` `xs.append(-0.0)` `print(abs(xs[0]))` | `0.0` (right, by accident) | `0.0` | `0.0` |
| `xs=[]` `xs.append(-4)` `print(abs(xs[0]))` | `4` | `4` | `4` |

Nothing else about a float moved: `print(-1.5)`, `abs(-3.5)` over a slot, `round(-2.675, 2)`, `min`/`max`,
`sum`, `**` and the inf/nan renderings are byte-identical to before, and `print(0.0)` still has no sign.

## IR impact

Two shapes. A signed compile-time double is materialised by a `bitcast i64 … to double` instead of an
`fadd double 0.0, …`; `rt_num_arith`'s abs arm loses a compare-and-select and gains a bitcast, an `and` and a
bitcast — three instructions, no new block, no new global, no call, and no register pressure worth naming.
The module still verifies under `llc -passes=verify`, and the whole corpus is checked by the existing verify
legs rather than by an assertion added here.

No new runtime function, no new record field, no CLI surface, no schema change: `--emit-llvm` shows the
difference and `--oracle` agrees on all four probe lines now, which is the machine-readable statement of the
promise.

## Alternatives rejected

- **Return the constant as a bare literal (`return floatConst(v)`), as ADR 0263's fold does.** The cleanest
  answer in principle — a constant is a value and needs no instruction to become one — and the reason this
  row's `Free text` suggested it. It does not fit thirteen sites that have already minted a temp name, several
  of which feed a `phi`/`select`/`store` through a helper that takes an SSA *value name*; changing the
  contract is a wider diff than stating the number. Kept where it already fits: the digit-count fold still
  hands back the constant, and the unsigned sites keep the addition.
- **`llvm.fabs` at the thirteen materialisation sites.** `fabs` of a constant folds back to the constant — with
  no sign to clear it is not the operation being asked for, and it would have hidden the question rather than
  answered it.
- **A `select` on `fcmp oeq …, 0.0` added to the old idiom** (`select (x == 0 && signbit) …`) — the sign bit is
  what the question is, and an `fcmp` is how it got wrong the first time. `fcmp oeq` is true for both zeros,
  `fcmp olt` is false for −0.0, and neither says which one you have without a second instruction.
- **Only special-casing −0.0 (`if v == 0 && math.Signbit(v)`).** Correct today, and the shape that invites the
  next signed value to be wrong: `math.Signbit` is the predicate, and there is no reason to restrict it.
- **Boxing the constant through the float-box road.** Costs a heap slot per signed constant — slots are what
  Gap Q.2 (ADR 0347) just learned to run out of — to buy what a bitcast buys for free.

## Witness legs

- **Reference leg**: `programs/probe_negative_zero_constant.gy` — all four lines are CPython's `-0.0`, the
  program left the debt ledger (its `OracleDebt` row deleted) and was promoted into
  `conformanceStandalone` where every run compares it against `python3` (`match` on both legs). The
  contract table in `integration/bool_element_test.go` that had pointed at this debt lost its row for the
  reason that file already documents twice: a contract row aimed at a paid debt passes forever without
  asserting anything.
- **Record leg**: the program was recorded from the reference through `tools/recmerge` (the artifact's own
  `meta.added_after_0302` rule — the compiled leg cross-checks and never authors an answer): record 6880 → 6882, the
  second entry the program's header rewritten to describe the paid debt rather than the debt. And the record leg is what *caught the regression* described above; without it, the abs door's
  −0.0 would have shipped as a new wrong answer under a commit advertising wrong answers being fixed.
- **Unit**: `pkg/lang/negative_zero_test.go` — 22 compiled-answer rows (literal, binding, `str()`, list and
  dict slots, a loop, a call boundary, the sign-removing operations, the int/bool families of the runtime
  door), the IR rows, the module-wide tripwire over nine programs that no signed constant is materialised by
  adding zero, and `materialiseFloat` asserted on its own over six values including ±inf.

## Residuals this cycle does not pay

- **A float with an exponent still does not lex** (`print(1.0e-3)` is a parse error) — `programs/probe_float_literal_with_exponent`,
  roadmap Gap R.135, pre-existing and reproduced against the pre-cycle binary while writing the tables here.
- **`def f(v): return v * 2.0` / `print(f(0.5))` reaches `llc` with a `double` value stored through
  `store i32`** — the numeric door's double arriving at a context that stores an `i32` word, which is roadmap
  Gap R.98's (🟨 PARTIAL since ADR 0253). Reproduced identically before and after this change, so it is that
  row's business; it is named here because it is the exit-2 class and because a tripwire that cannot compile a
  program should say which row owns it rather than drop the program.
- The other places where a float's *kind* rather than its value is the question stay L11.6's.
