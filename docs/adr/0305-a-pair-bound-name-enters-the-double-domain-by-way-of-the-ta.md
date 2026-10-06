# A pair-bound name enters the double domain by way of the tag

## Decision

A name the pair road bound — `n = xs[0]` over a container the program built, a loop element over a container
that mixes kinds, an arithmetic answer, a paired parameter — may be used **wherever the reference wants a
double**, and it gets there by branching on its tag:

```python
xs = []; xs.append(7); n = xs[0]
print(n / 4)         # 1.75     (Gap R.148's shape; refused)
print(n > 7)         # False    (refused — 7 > 7)
print(n >= 7)        # True     (refused)
print(n + 2.5)       # 9.5      (refused)
print(2.5 - n)       # -4.5     (answered all along — the asymmetry the row was filed with)
xs = []; xs.append(7.5); n = xs[0]
print(n / 2)         # 3.75     — a float slot unboxes rather than reading its handle as a number
xs = []; xs.append("a"); n = xs[0]
print(n / 4)         # TypeError: unsupported operand type(s) for /: 'str' and 'int'
print(n > 7)         # TypeError: '>' not supported between instances of 'str' and 'int'
print(7 > n)         # TypeError: '>' not supported between instances of 'int' and 'str'
```

The rule: **a pair reaches a number position through a door that reads the tag, never through a door that
widens the payload.** `rt_lift_num` unboxes a float box and `sitofp`s *everything else* — so a lift-only
route prints the interned index of `"a"` as a number, at exit 0. Three of these rows were written against a
first implementation that did exactly that, and the file that failed was this ADR's test file, not the
compiler.

## Two doors, both pre-existing

The cycle added **no new runtime code**, which is the part worth recording. It routed two existing
tag-reading doors at names instead of at slot reads:

| Position | Door it now walks | Written for |
|---|---|---|
| `/`, `+`, `-`, `*` with a float operand; `float(x)`; `round(...)` | `taggedDoubleFromPair` — float arm unboxes (`rt_float_of`), int/bool arm converts, every other tag raises this operator's sentence for that kind | ADR 0253's true-division door, whose zero guard had to live inside each arm because which `ZeroDivisionError` wording applies is a tag fact |
| `<`, `<=`, `>`, `>=` against a static number | `emitTaggedOrder`'s three arms, now fed with a pair side (`orderSide.pairName`) | ADR 0250 and ADR 0252, where the raise chain names both operand types in source order |

`taggedDoubleFromObject` — a ~120-line function that reads a `(payload, tag)` pair out of an `*Index` — split
into the Index wrapper and `taggedDoubleFromPair`, which is what a name can supply. Same for the ordering
door: `orderShapeOf` now classifies a pair-bound name as an *object-like* side (`fromTag`, `canNum`,
`canText`), and the side's `asksTag()` predicate replaces six `s.ix == nil` tests. That second change is the
one to know about when reading the door later: a side that asks the tag is no longer the same thing as a side
that indexes, and the arm-folding code reads the former.

## The asymmetry, explained

Gap R.148 was filed with `print(n / 4)` refused and `print(2.5 - n)` answered, which looks arbitrary. It was
not: `n / 4` has an int literal on the right, so the expression was an i32-road BinOp whose pair-name operand
the i32 road refuses (correctly — one word cannot hold a pair), while `2.5 - n` is a float expression, so it
went to the double road, and the double road's *left* operand was a literal and its right operand got
lowered by a path that happened to reach the tag dispatch. The row's own warning — that the naive route
"stores a `double` into the i32 slot a tagged name owns, which `llc` rejects" — is why the fix had to be an
arm structure rather than a cast.

`n > 7` and `n > 2.5` split the same way for the same reason: a float on the right promotes the whole
comparison and reaches the double door; an int on the right stays on the comparison road, whose pair door
(`pairOrder`) admits only names whose tag is a *proof* (ADR 0304's rule). Opening that road with a bare lift
would have compared interned indices, so the pair went into the ordering's tag arms instead — where
`'>' not supported between instances of 'int' and 'str'` and `... of 'str' and 'int'` are different sentences
and the door already knew the difference.

## What is still refused, and why that is the honest answer

`float(n)`, `sum([n])`, `abs(n)`, `[n]`, `min(n, 3)` and an f-string field over a pair-bound name still
refuse (roadmap Gap R.146): each keeps **one word** for the value, and — unlike the operators above —
`float()` has no tag-dispatched arm to be pointed at a name. `float(n)` is the instructive one, because it
looks free: `rt_lift_num` would answer it, and for a slot holding `"a"` the answer is `0.0` where CPython has
`ValueError: could not convert string to float: 'a'`. The temptation to take that deal is the shape of every
Gap R.38 row this tracker has ever closed.

Two shapes also moved *out* of refusal tables in pre-existing tests, which is what paying a row looks like
when the ledger is honest:

- `pkg/lang/mixed_list_test.go`: `xs = [1, "a"]` / `for x in xs:` / `print(x > 2)` — the number element
  compares, the text element raises; the row moved to this file's trap table.
- `integration/pair_binding_test.go`: `n / 4` and `n > d` moved from the refusal table to the answer table.

## Alternatives rejected

- **Call `rt_lift_num` from the float road.** Prints an interned index as a number for a text slot at exit 0.
- **Widen `pairOrder`'s lift.** Same defect on the comparison road, plus the ordering's two-type sentence
  would become a guess about the other side.
- **Promote pair-bound names to a double slot at their binding.** A name holds one pair and its kind is one
  run-time fact (`xs.append(7)` then `xs.append("a")` — which slot's kind is `n`'s?); ADR 0172's latest-binding
  rule and Gap R.142's heap-handle print are the record of what happens when kinds are pinned early.
- **Answer `float(n)` with the lift and file the text case as debt.** A debt row for a wrong answer at exit 0
  is still a wrong answer at exit 0; refusals are countable (`compiled refusals this run`), wrong answers are not.
- **Leave the two paid rows pinned as refusals.** Cheaper by ten minutes, and it would have made two
  pre-existing tests assert a behaviour the compiler no longer has.

## References

`roadmap.md` L11.1, Gap R.148 (this row), Gap R.146 (the one-word positions), Gap R.82 (what `+`/`*` answer),
Gap R.38 (a diagnostic describes what is true), Gap R.142/ADR 0172 (latest binding) · ADR 0253 (the per-kind
float raise, the zero guard inside the arm), ADR 0252/ADR 0250 (the ordering's tag arms and the two-type
sentence), ADR 0304 (per-operator arithmetic gate), ADR 0303 (the pair binding), ADR 0166 (exit 2 is the
compiler's own bug), ADR 0228 (a trap the program can reach) · `pkg/lang/pair_number_float_test.go`,
`integration/pair_number_float_test.go`, `pkg/lang/heapargs.go`, `pkg/lang/codegen.go`
