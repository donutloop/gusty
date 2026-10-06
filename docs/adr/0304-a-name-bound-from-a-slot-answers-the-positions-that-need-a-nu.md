# A name bound from a slot answers the positions that need a number

## Decision

A name bound from a container slot — `n = xs[0]` over a container the program **built** — travels as a
`(payload, tag)` pair (ADR 0303), and the compiled backend now lets that name reach the **tagged arithmetic
door** (`rt_num_arith`, `rt_lift_num`) in the positions that need one number:

```python
xs = []
xs.append(7)
n = xs[0]
print(n - 1)      # 6      (was refused: Gap R.146's sentence)
print(-n)         # -7     (was refused)
xs = []; xs.append(2.5); n = xs[0]
print(n - 1)      # 1.5    — a float slot keeps its float
xs = []; xs.append("a"); n = xs[0]
print(n - 1)      # raises TypeError: unsupported operand type(s) for -: 'str' and 'int'
```

The widening is **per operator, not per name**, and that is the decision worth recording. Three operators —
`-`, unary `-`, `//` — take the door whatever the slot holds, because no operand pair makes them answer a
non-number: they answer a number or they raise, and the raise is CPython's own sentence read from the tag
(`bad operand type for unary -: 'NoneType'`, `unsupported operand type(s) for -: 'set' and 'int'`).
`+`, `*` and `%` stay behind ADR 0265's proof that no text or container can reach a slot, because the
reference **answers** with those: `"a" + "b"` is `"ab"`, `[1] * 2` is `[1, 1]`, `"%d" % 3` is `"3"` — and this
backend builds none of those from a slot (Gap R.82, Gap R.165). Answering a raise where CPython answered a
value is the wrong-program family the roadmap keeps filing (Gap R.38, Gap R.95), so those three keep refusing,
in words (roadmap Gap R.146 remains open for them).

## Why the origin set was narrow at all

`numericPairVar` — the predicate that decides which names may enter the door — admitted only origins whose tag
is a **proof**: a name the arithmetic road bound (`taggedOriginArith`), a float it bound, and a parameter whose
argument arrived paired. Those tags can say `int` or `float` and nothing else, so every position that keeps one
word for an operand could be handed the lift and never wonder what the payload meant.

A slot's tag is a different animal: it is a **fact the objects decided**, and it can say `str`, `None`, `list`,
`dict`, `set`. Treating it as though it were a proof would have been the easy change and a wrong-language
change: `n - 1` over a text slot would have summed the interned index of `"a"`, printed a number, and exited 0.
Treating it as though it were nothing — the state this cycle started from — refused `-n` for a slot holding
`7`, which CPython answers.

So the origin check splits in two, and the two questions are asked separately:

| Question | Predicate | Where it is used |
|---|---|---|
| can this name enter the door at all? | `arithWouldRefuse` (now also `taggedVars`) | `+`, `-`, `*`, `//`, `%`, unary `-` |
| is its tag a proof of a number? | `numericPairVar` | the operators that must not raise where CPython answered |

`taggedArithPair` already carried the per-operator guard (`op == 0 || op == 2 || op == 5` requires
`slotArithmeticIsProven` and rejects a string operand), so the widening needed one predicate widened and no new
runtime code — the door had arms per kind since ADR 0265, and ADR 0266 had already given the raise its
per-kind sentences.

## What the raise has to be, exactly

A trap that a helper performed for itself is unreachable to the program, so the raise leaves through the
emitted door and stays catchable (ADR 0228):

```python
xs = []; xs.append("a"); n = xs[0]
try:
    print(n - 1)
except TypeError:
    print("caught")            # caught
```

The class and the message are compared **word for word** against CPython in both test files, per kind:
`str`, `NoneType`, `list`, `dict`, `set`, plus both `ZeroDivisionError` wordings reached through a slot
(`n // 0`). A test that only required "some TypeError" would pass on a door that raised `TypeError: x`, which
is how a diagnostic quietly becomes uncatchable prose.

## Alternatives rejected

- **Admit every tagged name to every operator.** `"a" - 1` would then answer a number at exit 0 — Gap R.38's
  family — or raise where CPython answered a value for `+`/`*`, which is worse: a program that refuses to
  concatenate two texts is not a Python-like language.
- **Refine the proof so `+` opens too.** `xs.append(7)` gives the compiler a literal for that slot and nothing
  about the next append; ADR 0265's pass is program-wide for a reason. `TestTheWiderOperatorsOpenOnlyWhereThe
  SlotsAreProven` asks the gate directly so a future widening fails a decision row rather than silently changing
  which programs compile.
- **Specialise the name at its binding.** Bind `n` as `int` when the slot read's literal says int, and refuse
  otherwise. That makes the *variable's* type a run-time accident: `n = xs[0]` / `xs.append("a")` / `print(n)`
  must still print what the slot holds, and ADR 0172's latest-binding rule already shows where pinned-at-binding
  kinds rot (Gap R.142 printed a heap handle for exactly this reason).
- **Let the message be generic.** "unsupported operand" without the kinds was the pre-ADR-0265 state and made
  `except` matching a guessing game; the messages here are asserted per kind.

## What this leaves owed

Gap R.146 stays open for `n + 1`, `n * 2`, `n % 3`, `abs(n)`, `[n]`, `min(n, 3)` — each refused in words that
name the value's real origin and the roadmap row that owes it. A pair-bound name still cannot enter the **float
domain** (`n - 1 + 0.5` refuses where CPython answers `6.5`), which is Gap R.148's row and is on the drift ledger
from this cycle's run with that attribution. The prompt still declines a user-defined call (L13.1) — two of this
cycle's three new ledger rows are that filed silence, not new breakage.

## References

`roadmap.md` L11.1 (the tagged value word), Gap R.146 (the one-word positions), Gap R.82 (what `+` and `*`
answer), Gap R.148 (a pair-bound name in the float domain), Gap R.165 (`%` formatting), Gap R.38 (a diagnostic
describes what is true), Gap R.142/ADR 0172 (latest-binding rule) · ADR 0265 (the tagged arithmetic door and its
proof gate), ADR 0266 (the negation sentence per kind), ADR 0253 (the empty-operand `fdiv`), ADR 0228 (a trap the
program can reach), ADR 0303 (the pair binding this leans on), ADR 0302 (one backend; the record and the ledgers) ·
`pkg/lang/pair_number_test.go`, `integration/pair_number_test.go`, `pkg/lang/heapargs.go`
