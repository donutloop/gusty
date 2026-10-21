# A condition asks a self-describing read its own kind, not its payload

**Roadmap:** Gap R.284 (closed here — the truth of a self-describing dict entry in `if`/`elif`/`while`/a ternary/`bool()`) ·
Gap R.359 (measured and closed by the same door — the truth of an unrolled comprehension or `for` item) ·
Gap R.360 (filed here — the truth and the print of a **set** entry, which the pair table never gets to answer) ·
Gap R.147's entry spelling (answered as a side of the same door) ·
L11.1 (the tagged value word in the positions that read it) ·
ADR 0275 (`@rt_pair_truth`, the door `and`/`or` already decide with) ·
ADR 0331 (a literal's element asks its tag the same question) ·
ADR 0353 (a dict entry carries its tag to the print door) ·
ADR 0386 (the unrolled item records its kind in `loopElemTag`) ·
ADR 0387 (the signless call and the folds ask the entry's tag) ·
ADR 0273 (a gate decides what the program states; it does not invent a missing half)

## Status

Accepted. Landed 2026-10-21.

## Context

`d = {None: 3.5}` has printed `3.5` since ADR 0353 and given its magnitude to `abs` since ADR 0387. Asking the
same entry a *yes-or-no* question was still answered by the wrong word:

```python
d = {None: "hi"}
if d[None]:            # CPython: then — backend: else, at exit 0
    print("t")
print(bool({1: []}[1]))  # CPython: False — backend: True, at exit 0
```

and, beside it, the item of an unrolled loop:

```python
print([bool(x) for x in [0.0, [0]]])   # CPython: [False, False] — backend: [True, True], at exit 0
```

Three roads decide a branch by testing a word against zero: the head of `if`/`elif`/`while`, the head of a
ternary, and the builtin `bool()`. Every one of them had an arm for the kinds whose word means itself and an
arm for the pair table for the kinds it had already met. A dict entry — whose payload is an interned text's
**table index**, a float box's **handle**, an empty container's **handle**, and `None`'s `0` — was not a shape
any of them had met, so it fell to the generic "the word is not zero" road. A handle is never zero and an index
is rarely zero, so every float `0.0`, every `""` and every `[]` came out true and every `None` came out false for
the wrong reason.

The `if` arm made the defect depend on nothing about the program's meaning: `d = {"k": ""}` / `if d["k"]:` took
the `then` arm because `"k"` had been interned before `""` and the index of `""` was therefore 1. Same source,
same value, different answer from the order two strings happened to be interned in.

What made this a gate problem rather than a runtime problem is that the runtime door already existed.
`@rt_pair_truth` (ADR 0275) answers "is the value this pair describes true" for the module's whole tag
vocabulary — a number in either family through `rt_lift_num`, a text through `rt_str_len`, a container through
`rt_heap_len`, `None` by its tag. ADR 0331 had already moved a literal's element onto it. The condition road
simply never asked for the pair.

## Decision

**One door, asked in two places, over two reads.** The condition road asks the same subscript pair that print,
`str`/`repr` and an f-string field ask, and sends it to `@rt_pair_truth`:

1. **`truthOperandErr`** — the head of `if`/`elif`/`while` and the head of a ternary. For a subscript gate
   (`entryReadNeedsTheTag` or `isPairElem`), ask `subscriptPairOf`; where the print door's dispatch declines
   because the compile-time fold can spell the read, ask `conditionPairOfRead` instead. That second call is the
   reason this is not a one-line change: the fold's answer is a payload, and a payload is exactly what a
   condition may not test.
2. **`builtinBool`** — the same two calls, the same table, the result widened to the builtin's `i32`.
3. **The unrolled item.** A comprehension or `for` over a literal binds its loop variable afresh per item and
   records the item's kind in `loopElemTag` (ADR 0386). `truthAsksTheLoopItem` reads that record and sends
   `(payload, tag)` to the same table. ADR 0386 taught the print, the binding and the container doors to read
   the record; the condition was the road it left out, which is why the item truth answered from the payload.

**`conditionPairOfRead` is asked when `subscriptPairOf` declines, not instead of it.** `subscriptPairOf` leaves
a read the fold can spell to the ordinary road — correct for a value position, where the printer is told the
kind and renders the payload. A condition has no printer: it tests the word. So the truth road walks the
container back to the literal its name was bound to (`literalBehind`) and asks the literal for the pair the fold
threw away.

**A provable integer keeps the one-word road, in both reads.** `d = {1: 2}` / `if d[1]:` and
`[bool(x) for x in [0, 1, 2]]` compare an `i32` against zero as they always did. The gate returns false before
any IR is emitted, and both cases are asserted with the IR claim that `@rt_pair_truth` is **absent** from the
program's own body — so a door that opens wider than it must fails this file rather than surviving review.

**A text item goes to the table too.** Its payload is the interned index, and `rt_pair_truth`'s text arm asks
`rt_str_len`, which reads that index. Testing the index against zero is what made
`print([x for x in ["", [0]] if x])` print `['', [0]]` as soon as another text was interned ahead of `""`.

## What answers, and what keeps its own row

| Program | CPython | Backend after ADR 0388 |
|---|---:|---|
| `if d[None]:` over `"hi"`, `d = {None: "hi"}` | `then` | `then` |
| `if d[None]:` over `""` | `else` | `else` |
| `if d["k"]:` over `""`, `d = {"k": ""}` | `else` | `else` |
| `bool(d[None])` over `0.0`, `-0.0` | `False` | `False` |
| `bool(d[None])` over `[]`, `{}`, `None` | `False` | `False` |
| `bool(d[None])` over `[0]`, `3.5`, `"x"`, `True` | `True` | `True` |
| `"y" if d[None] else "n"` over `0.0` | `n` | `n` |
| `while d[None]:` over `2`, then `0` | one iteration | one iteration |
| `d[None] and 1`, `d[None] or 2` over `"hi"` | `1`, `hi` | `1`, `hi` |
| `[bool(x) for x in [0.0, 1.5, "", "a", None, True, False, [0], []]]` | `[False, True, False, True, False, True, False, True, False]` | identical |
| `[x for x in [0.0, "", None, [0], [], "z"] if x]` | `[[0], 'z']` | `[[0], 'z']` |
| `bool(x)` in a `for` body over `[0.0, "", None, [0]]` | `[False, False, False, True]` | identical |
| `d = {1: 2}` / `if d[1]:` | `then`, on one word | `then`, no `@rt_pair_truth` in the body |
| `[bool(x) for x in [0, 1, 2]]` | `[False, True, True]` | identical, no `@rt_pair_truth` |
| `bool(set())` as an **entry** or an **item** | `False` | still `True` — **Gap R.360**, the set's tag is never written |
| `print(d[None])` over a set entry | `set()` | still `3` — **Gap R.360** |
| `bool(x)` of a **bound** entry (`x = d[None]`) | `False` | still refuses — Gap R.146's one-word binding |
| `f(d[None])` into a one-word call argument | `3.5` | still today's `3` — Gap R.220's boundary |
| `bool(d[None] and 1)` | `True` | still refuses — Gap R.147's operand choice, a one-word position |

## Machine path

No new refusal, so no new capability code. One registry entry had to be **re-aimed**:
`lower.unsupported.slot-read-of-rebound-name` sampled `xs = [] / xs.append([7, 8]) / if xs[0][0]:`, which this
door now answers. A sample that compiles is a claim about nothing — `TestTheCapabilityRegistryCoversWhatItClaimsToKnow`
fails it on purpose — so the sample is now the shape that still wants one word out of the same slot,
`print(xs[0][0] < xs[0][1])`, which still classifies under the same code.

The evidence is threefold, as every behavioural row here is:

* the **record leg** — 68 new sources through `tools/recmerge`, which asks CPython first and writes only the
  rows the compiled leg agrees with (the conformance program `integration/programs/dict_none_bool_key.gy`
  included, whose `.expected` is CPython's own bytes);
* the **reference leg** — `integration/dict_entry_truth_test.go` runs each program through `python3` and the
  CLI and compares, and the boundary rows assert today's answer with the row that owns it;
* the **IR leg** — the entry and item conditions must reach `@rt_pair_truth`/`@rt_dict_value_tag`, and the
  provable-integer controls must not.

## Consequences

* The condition road and the print road are now the **same question** over a self-describing read. A future
  position that reads an entry (a format spec, a membership test) has one gate to consult, not a new guess to
  make.
* `@rt_pair_truth` is the module's only truth table for a pair. A tag the table cannot read is now a *visible*
  gap rather than a silently-true branch — which is how Gap R.360 was found: the set is the one container whose
  tag the writers never store, so its truth is the one the table is never asked.
* The compile-time fold keeps its meaning — "the compiler can name the value" — and loses its privilege: a fold
  answer may feed a value position, never a test. `conditionPairOfRead` exists precisely because the fold's word
  and the tag's word are different words.
* Internment order is no longer observable through a branch. Before this row the answer for `{"k": ""}` differed
  from `{7: ""}` for a reason that had nothing to do with the program.

## Alternatives rejected

* **Widen `subscriptPairOf` so the fold never answers a dict read.** The fold is what keeps every ordinary
  all-integer program on one word; taking it away from the value roads to serve the test roads would have
  charged the wrong thing. `conditionPairOfRead` is asked only where a test is being made.
* **Test the payload with a per-kind `select` in the compiler.** That re-implements `@rt_pair_truth` in the
  one place the module already has a table for it, and the table is what `and`/`or`, the literal element and the
  printer already agree on.
* **Leave the loop item to a later row.** Its wrong answer is the one this cycle caused to be reachable: ADR
  0386's unrolled road made items of every kind legal, and only the print, binding and container doors learned
  the recorded tag. A feature that turns a refusal into a wrong answer at exit 0 is not finished.
* **Treat a falsy-looking payload as falsy when the tag is unknown.** `None`, `0.0`, `""` and `[]` are falsy for
  four different reasons and a handle for three of them; guessing is how this row began.
* **Record this backend's `True` and call the row consistent.** The record leg is evidence, not definition. Every
  expectation in this commit was read off CPython first (`tools/recmerge` refuses to write a row the compiled leg
  disagrees with), and the three shapes the backend still gets wrong are pinned as wrong in
  `TestTheTruthOfASetEntryIsStillAskedOfTheHandle` rather than recorded as answers.
