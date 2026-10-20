# A set or dict comprehension asks the fold first, and what the fold cannot name is built through tagged slots

**Roadmap:** Gap R.353 (closed here — a set or dict comprehension whose member/value was a call refused while the list twin answered) ·
Gap R.357 (closed here — `{{1} for x in [1]}` let the inner brace steal the outer comprehension's `for` and emitted an invalid module) ·
Gap R.358 (newly measured and filed — a comprehension whose *iterable* is a literal of non-numbers still asks the fold for integer items) ·
Gap R.105 / R.116 / R.122 / R.123 / R.225 / R.226 (the dict-, text-, float-, and pair-shaped roads whose answers meet at this door) ·
L11.1 (the tagged value word) · ADR 0192 (the comprehension road: the list arm had both fold and runtime loop; the set and dict arms had only the fold) ·
ADR 0383 (the comprehension clause list that made the remaining gaps visible) · ADR 0232 / ADR 0187 (a payload never travels without its tag) ·
ADR 0244 / ADR 0259 / ADR 0310 / ADR 0311 (tags, folded globals, unhashable members, and the runtime slot doors this builder reuses) ·
ADR 0346 (a refusal names its row and enters the capability registry) · ADR 0166 (exit 2 is a compiler bug, not a user-facing answer) ·
ADR 0302 / ADR 0317 (one backend, and no cached witness)

## Status

Accepted. Landed 2026-10-20.

## Context

The list comprehension road has had two arms since ADR 0192. For set and dict comprehensions, only one arm existed: the compiler tried to fold the
whole container into a module global, and anything it could not name became a refusal.

```python
print({(lambda z: z + x)(1) for x in [10, 20]})   # reference: {11, 21}              here: comprehension element must be constant
print({x: (lambda z: z + x)(1) for x in [10, 20]}) # reference: {10: 11, 20: 21}     here: comprehension value must be constant
```

The list-shaped twin had answered since Gap R.348. The asymmetry was not cosmetic: a member and an element are the same expression position under the
language; a set member and a dict value are the same value position as a list element. Making them compile through a different story produced answers
worse than refusals.

The untagged fold could answer `None` as the integer its truthiness is, `True` as `1`, and an interned text as its `@str_tab` index:

```python
print({None for x in [1]})                         # reference: {None}                untagged fold: {0}
print({1: None for x in [1]})                      # reference: {1: None}             untagged fold: {1: 0}
print({True for x in [1]})                         # reference: {True}                untagged fold: {1}
xs = []
xs.append(6)
print({v: v / 2 for v in xs})                      # reference: {6: 3.0}              value-only lowering: {6: 3}
xs = ["a", "b"]
print({x: x.upper() for x in xs})                  # reference: {'a': 'A', ...}       untagged slot: {'a': 2, ...}
```

A dict entry is not merely a value. Both key and value need payload plus tag. A float value lowered to its integer bit pattern was enough to print an
integer; the entry itself had lost the fact that made the payload mean `3.0`.

A related parse failure turned one program into invalid IR:

```python
print({{1} for x in [1]})   # reference: TypeError: unhashable type: 'set'
```

The parser closed the inner brace display before it saw the outer `for`, making the program look like a set literal containing a set literal. The static
literal path then wrote a container global pointer into a slot typed `i32`, and `llc` exited 2. Exit 2 is a compiler bug; the program should have raised.

## Decision

**Ask the fold first, but let it answer only the question it can actually prove. Everything else uses the same runtime loop as the list road, and every
slot it writes carries payload and tag.**

1. **One fold gate, two answers.** `setCompFolds` and `dictCompFolds` ask `compOperandsFold` for the iterable, member/value, key, and filters. If every
   operand is a plain integer, the old constant container is emitted. If not, the comprehension goes to `runtimeCompSetDict`. The question and executor
   are one pair: `foldsToAnInteger` no longer treats `None`, verdicts, text, float literals, containers, or expressions that already print as one of those
   as integers.
2. **The runtime builder mirrors the list's loop.** It allocates one self-describing set or dict, allocates clause slots, walks the iterable through
   `runtimeCompLoop`, applies the filters, and adds one member or entry per accepted iteration through `rt_set_add_tagged` or `rt_dict_put_tagged`.
   Calls, lambdas, defs, methods, subscript reads, nested containers, None, verdicts, floats, and interned texts enter through the tag rather than through
   a guessed integer.
3. **A dict entry is a pair.** `elemPayloadAndTag` is the question for both key and value. This pays the float-value row (`{6: 3.0}`) and the text-key row
   (`{'k': 3}`) together, while leaving the fold in charge of the integer dictionary it could always build.
4. **Only the member or key is hashable.** A set member and dict key ask `guardHashableTag` and raise CPython's `TypeError: unhashable type: '<kind>'`.
   A dict value is allowed to be a list, set, dict, tuple, None, verdict, float, text, or another container. The guard is at the position the language
   states — not at an inferred guess about a name's run-time kind.
5. **A slot built through the tagged doors gets the self-describing mark.** `rt_mark_estr` is applied when a container's members or entries may hold
   floats, None, verdicts, containers, or dynamic tags. A container of plain interned text may use the compact text table; the rest asks the mixed printer.
6. **The literal-text question is shared.** `heapElemKind` now recognizes `printsAsInternedStr` operands, including string methods such as `x.upper()`.
   The comprehension builder, literal elements, and loop bindings store the interned index with the text tag. A slot is not allowed to answer an index
   as though the index were the value.
7. **The brace display's `for` belongs to the display whose brace is still open.** Parser state tracks whether it is inside a brace display; after an
   inner `}`, a `for` belongs to the outer display. The result is a set comprehension, whose member is a set literal and therefore raises the reference's
   unhashable `TypeError`.
8. **What cannot yet be named refuses and names its row.** A non-integer literal iterable is `Gap R.358`; unsupported element shapes remain tied to
   ADR 0383's clause-list rows. The refusals retain exit 1 and the capability registry. No program in this commit is allowed to reach `llc` invalidly.

## What is answered

| Program | CPython | Compiled backend |
|---|---|---|
| `print({(lambda z: z + x)(1) for x in [10, 20]})` | `{11, 21}` | `{11, 21}` |
| `print({x: (lambda z: z + x)(1) for x in [10, 20]})` | `{10: 11, 20: 21}` | `{10: 11, 20: 21}` |
| `print({None for x in [1]})` | `{None}` | `{None}` |
| `print({1: None for x in [1]})` | `{1: None}` | `{1: None}` |
| `print({True for x in [1]})` | `{True}` | `{True}` |
| `print({1: True for x in [1]})` | `{1: True}` | `{1: True}` |
| `xs = []` / `xs.append(6)` / `print({v: v / 2 for v in xs})` | `{6: 3.0}` | `{6: 3.0}` |
| `xs = ["a", "b"]` / `print({x: x.upper() for x in xs})` | `{'a': 'A', 'b': 'B'}` | same, in dict order |
| `d = {"k": v for v in [1, 2, 3]}` | `{'k': 3}` / `3 1` | same, with `len(d)` |
| `print({x: [x] for x in [1, 2]})` | `{1: [1], 2: [2]}` | same |
| `print({[1] for x in [1]})` | `TypeError: unhashable type: 'list'` | same raise, catchable |
| `print({{1} for x in [1]})` | `TypeError: unhashable type: 'set'` | same raise, catchable |
| `print({{1: 2} for x in [1]})` | `TypeError: unhashable type: 'dict'` | same raise, catchable |
| `print({x * 2 for x in [1, 2, 2]})` | `{2, 4}` | still folded |
| `print({x: x * 2 for x in [1, 2]})` | `{1: 2, 2: 4}` | still folded |

Text set membership is asserted as a set of members, not as a printed order: CPython randomizes string hashes, so `{'A', 'B'}` versus `{'B', 'A'}` is
not a semantic fact worth pinning. Dict order is a language-visible fact and is pinned.

## Machine path

The two builders emit the shared runtime calls by name: `@rt_set_add_tagged`, `@rt_dict_put_tagged`, and `@guardHashableTag`. IR tests assert the tag
door is used for the shapes that need it and that constant folds still produce static `@.set` / `@.dict` globals without calls to the tagged builder.
`--json` refusals for the still-open iterable shape route through the existing capability registry; no new refusal was introduced in this commit because
the two previously filed refusals that these rows paid were removed rather than newly hidden.

## Consequences

* The set, dict, and list comprehension roads now share the same question: can the operand's meaning be named at compile time? If yes, fold; if not,
  carry the tag at run time.
* Four previously filed capability rows left the registry: verdict members, text dict keys, dict float values, and text method values now answer rather
  than refuse. The registry is now `43` codes.
* Four drift ledger rows were removed from `pkg/lang/testdata/interpreter-golden-drift.json` because the compiled answer matches the record again.
* Twenty comprehension programs were asked through `tools/recmerge`; sixteen had not been on the record, bringing it to **8065**, and the four
  already recorded agreed and were not rewritten. The record leg now fails if these computed set/dict answers disappear.
* The two paid probes are matrix parity rows rather than known-divergence probes. The matrix is `189 / 219` conformant, with the oracle reporting
  `175 match`, `22 debt`, and `22 not_applicable`.
* The parser fix means nested brace syntax is now honest enough for the unhashable semantics to be reached at run time. This is what separates a language
  refusal from a compiler crash.

## Alternatives rejected

* **Fold `None`, `True`, and interned text into integers.** They have integer lowered payloads, but a slot's payload is meaningless without its tag. This
  was producing `{0}` for `{None}` and printing `2` instead of `'A'` — wrong values at exit 0.
* **Keep dict values as payload-only and infer tag at print time.** The same entry's key had a tag; a value did not. This made floats into integers and
  made print-time recovery impossible after the pair left the comprehension.
* **Guess whether a slot is dynamic and add tags only sometimes.** A container whose slots are written by tagged helpers must be marked self-describing
  as a whole. Selective marking makes printer behavior depend on insertion accident rather than a stated slot-kind fact.
* **Refuse every computed set/dict comprehension.** This was the status quo and is what made the list twin answer while the set and dict roads refused
  the same expression. The fold already had the constant cases; the missing piece was the runtime arm.
* **Use `llc` exit 2 as the answer to an unhashable nested brace.** Exit 2 is reserved for a compiler rejecting its own generated IR. CPython raises
  `TypeError`, and a program with an `except` must be able to catch our raise.
* **Pin CPython's text-set printed order.** String hashes are randomized per process. The members, tag, deduplication, and count are semantic; repr order
  is not.
