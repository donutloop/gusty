# A comprehension over a literal container asks the items what they are, not whether they are integers

**Roadmap:** Gap R.358 (closed here — `[x for x in ["a", "b"]]`, `{k: 1 for k in ["a", "b"]}`, and a heterogeneous literal refused with `comprehension iterable must be constant integers`) ·
L11.1 (the tagged value word in the positions that read it) ·
ADR 0192 (the list comprehension's runtime loop) ·
ADR 0234 (a comprehension that folds **is** the literal it folds to) ·
ADR 0233 / ADR 0259 (the unrolled `for` records each item's tag and verdict) ·
ADR 0384 (the set and dict tagged builders this widens from the element to the iterable) ·
ADR 0273 (a gate may decide only what the program states; a position with one word does not gain a second word by guesswork)

## Status

Accepted. Landed 2026-10-20.

## Context

The comprehension road has always had two ways to walk an iterable:

- if the compiler can enumerate it, derive its items and evaluate the element under a compile-time binding; and
- otherwise build a real loop over a heap container and bind the loop variable from the object.

For a literal list the first road began by asking every item to fold to an **integer**. That question made sense when the result was a static array of `i32`s, but the comprehension had long since grown element builders that know floats, `None`, verdicts, texts and containers. The iterable road had not followed:

```python
print([x for x in ["a", "b"]])             # CPython ['a', 'b'] — backend: refusal
print({k: 1 for k in ["a", "b"]})           # CPython {'a': 1, 'b': 1} — backend: refusal
print([x for x in [None, True, False]])     # CPython [None, True, False] — backend: [0, True, False]
```

The last line is worse than a refusal. `None` was passed through the integer folder, stored as `0`, and printed at the exit code of success. The same program written as a `for` loop had the answer, because the unrolled `for` binds the loop variable **item by item** and records the kind of each binding.

ADR 0384 had already given set and dict comprehensions a tagged builder for a computed *member*, *key* or *value*. This is the other coordinate: the object being iterated is itself a heterogeneous container.

## Decision

**An integer iterable keeps the fold. A literal iterable that is not an integer list uses the same unrolled, tagged builders the computed-element road already used.**

1. A `ListLit` whose every item is an integer literal is unchanged. `foldedContainerCompLiteral`, `min`, `max`, `sum`, and the static `@.lst`/`@.set`/`@.dict` globals still depend on this road.
2. A `ListLit` with a text, float, void, verdict, tuple, or container item — or with an item that is not constant — routes to:
   - `runtimeCompList` for a list comprehension, and
   - `runtimeCompSetDict` for a set or dict comprehension.
3. Those builders bind one item exactly as the `for` road does:
   - the loop variable gets the payload the heap-element door computes (`heapElemKind`), including an interned text index or a float box handle;
   - `loopElemTag` records the item's tag for that iteration;
   - interned, digit-claim and verdict state follow the latest item, and are restored on the way out.
4. The object the loop fills is marked self-describing when a slot cannot speak for itself. A text written from the loop variable also gets the interned-text container mark, even when the operand's static name did not say “text”.
5. A one-word position still refuses. Printing and container writes can ask the per-iteration tag; arithmetic, calls and other positions that keep one word cannot carry it, and ADR 0273 forbids inventing a value for the missing half.

## What answers, and what still refuses

| Program | CPython | Backend after ADR 0386 |
|---|---:|---|
| `[x for x in ["a", "b"]]` | `['a', 'b']` | `['a', 'b']` |
| `[x for x in [1, "a", 2.5, None, True]]` | `[1, 'a', 2.5, None, True]` | same |
| `[x for x in [None, True, False]]` | `[None, True, False]` | same — no longer `[0, True, False]` |
| `{k: 1 for k in ["a", "b"]}` | `{'a': 1, 'b': 1}` | same |
| `[[x] for x in [None, 1.5, True]]` | `[[None], [1.5], [True]]` | same |
| `{x: x for x in ["a", 1, 2.5]}` | `{'a': 'a', 1: 1, 2.5: 2.5}` | same |
| `[x.upper() for x in ["a", "b"]]` | `['A', 'B']` | same |
| `[x for x in [[1], [2]]]` | `[[1], [2]]` | same |
| `print([x for x in [1, 2]])` | `[1, 2]` | still folds to `@.lst1` |
| `{x for x in [[1], [2]]}` | `TypeError: unhashable type: 'list'` | same raise at exit 3 |
| `[x + 1 for x in ["a", "b"]]` | `TypeError` | honest refusal; no one-word text→number bridge |

## Machine path

No new capability code is required. The old refusal string disappears with the shapes it described; the answers are CLI output, and the unhashable positions go through the same tagged hashing door as `s.add(v)` and `d[k] = v`. The conformance corpus gained `programs/comprehension_literal_iterable.gy`, asserted against CPython; fifteen sources — including the former wrong-answer rows — are on the record leg through `tools/recmerge`, each written only where CPython and the compiled backend agreed.

## Consequences

- The fold is now unambiguously an optimization for literals whose values it can name, not the definition of what a comprehension iterable may contain.
- The loop variable of an unrolled comprehension is a per-item binding, not one variable whose kind was inferred once from the first or last item. The same `loopElemTag` the `for` road had used is now read by container builders as well as by the print door.
- The remaining boundary is not the iterable but the **position** that receives the value. `[x]`, `{x: x}`, `s.add(x)`, `print(x)` and `x.upper()` ask the tag; `x + 1`, `x * 2`, and an argument to a one-word call do not yet, and refuse rather than printing the low word of a float box or an interned index.
- Set `repr` order for text members is not asserted. CPython randomizes string hashes; the rows assert the members and the fact that every member carries its tag.

## Alternatives rejected

* **Keep the integer check and reject every non-integer literal.** That preserves the old error but preserves the old asymmetry with `for`, and it leaves the void answer at exit 0. A refusal is not acceptable where the reference answers and this backend already has every needed runtime door.
* **Force every literal iterable through the dynamic heap loop.** That removes the fold and breaks `min`/`max`/`sum`, folded subscripts, and the identity between a folded comprehension and its equivalent literal (ADR 0234). The all-integer case must stay static.
* **Mark every generated container self-describing unconditionally.** Safe but loses the one-kind printing and representation paths that already work; the mark should record what the loop actually wrote.
* **Pretend the loop variable is always an integer and keep its old static name.** This is the bug's original mechanism. The item's kind, not the variable's name or its first binding, owns the tag.
* **Open arithmetic over a tagged unrolled loop variable by loading one word.** A float box handle and a string-table index are not numbers. That reproduces the exact class of wrong answer at exit 0 that this row exists to remove.
