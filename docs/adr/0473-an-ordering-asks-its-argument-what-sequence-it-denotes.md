# 0473 — an ordering asks its ARGUMENT what sequence it denotes: the text, the dict, the written-out range, and the comparator that comes with it

**Status:** Accepted
**Date:** 2026-10-30
**Amends:** [ADR 0339](0339-a-for-loop-asks-its-iterable-what-it-is-a-container-it-cannot-name-is-refused-never-read-as-a-count.md), [ADR 0472](0472-membership-equality-and-the-element-doors-ask-a-sorted-or-reversed-what-it-is.md)
**Roadmap rows:** **Gap R.247** (closed), **Gap R.576**, **Gap R.577**, **Gap R.578** (filed); neighbours **Gap R.248**, **Gap R.249**, **Gap R.250**, **Gap R.251**, **Gap R.575** left pinned with both legs' values; and **Gap R.579**, **Gap R.580** — two decorated-body defects the differential sweep found pre-existing on the parent and grandparent builds, filed with their bytes rather than fixed here
**Related:** [ADR 0417](0417-a-dict-is-sorted-by-its-keys-and-the-door-that-copies-it-is-where-the.md) (a dict's keys are its elements, strided by two), [ADR 0258](0258-the-rendering-pair-writes-to-a-switchable-sink-so-print-str-and-repr-are-one-table.md) (one table, one ordering), [ADR 0232](0232-a-container-word-means-its-payload-and-its-tag.md) (the slots are the truth), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md), [ADR 0249](0249-a-slot-read-through-an-index-the-program-computes-is-asked-of-the-tag.md), [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md) (the two witness legs), **L11.5** (code points, never bytes)

> Shape borrowed from ADR 0001; the falsifiable-gate discipline and the delete-the-gate non-vacuity run are
> ADR 0470's and ADR 0472's. This ADR ships with the repair it decides.

## The problem, measured

`sorted` and `reversed` were asked everything except what they were handed. The container tables answered that
question with a spelling — `staticContainerKind`, a record of what a NAME holds — which is true of a list
literal, a list variable and a numeric slice and of nothing else. Every iterable the reference builds a list
from, and this backend had no name for, fell out of every door at once. Measured against the pinned CPython
(3.12.3) and the parent build (`03f1bf0`), before this row:

| program | this backend | the reference |
|---|---|---|
| `for v in sorted("bca")` | refused: `its argument is a TEXT…` | `a`, `b`, `c` |
| `for k in sorted(d)` over `{"b": 1, "a": 2}` | refused: `its argument is a DICT…` | `a`, `b` |
| `for v in reversed({1: 2})` | refused | `1` |
| `for v in sorted(range(3))` | refused: `not a container this backend can name` | `0`, `1`, `2` |
| `print(sorted("bca"))` | **`0`** at exit 0 — the heap handle | `['a', 'b', 'c']` |
| `print(sorted(range(3)))` | **`printf("…%d…", i32 @.lst1)`** — the address of the fold's constant | `[0, 1, 2]` |
| `xs = ["b", "a"]` / `print(sorted(xs[0:2]))` | **`['b', 'a']`** at exit 0 (and `reverse=True` printed the OTHER wrong order) | `['a', 'b']` |
| `print(sorted(sorted("bca")))` | **`['b', 'c', 'a']`** at exit 0 | `['a', 'b', 'c']` |
| `xs = sorted("bca")` / `print(xs[0])` | **`2`** at exit 0 — the interned index of `"a"` | `a` |
| `xs = sorted("bca")` / `for v in xs: print(v)` | **`2`, `0`, `1`** | `a`, `b`, `c` |
| `print(sorted({"b": 1, "a": 2}))` | `['a', 'b']` ✓, but `print([sorted(…)])` leaked a handle as `[2]` | `[['a', 'b']]` |

Four of those eleven rows printed a number at exit 0 for a program CPython answers with a list or its
elements. **ADR 0166** ranks that worst; the refusals above it are the ladder's next rung, and a refusal that
misdescribes the shape (**`its argument is a DICT: sorted() would order its KEYS, which the runtime stores as
[key, value] pairs the sort does not walk`** — a sentence about an object the program never built) is the rung
after that (**ADR 0249**).

## The decision: substitute the list the reference builds, ONCE, and ask it as one question

The reference does not sort an iterable. It builds a container from the iterable and sorts that. So the
backend builds the same container, at compile time, in one place:

```go
// for_iterable.go — the sequence behind an ordering argument, spelled as a list literal, or nothing.
func (g *irGen) orderingSequenceArg(a Expr) (Expr, bool)
```

Three shapes, each spelled by the ONE road that already spells that sequence elsewhere:

| the argument | the list it denotes | spelled by |
|---|---|---|
| a TEXT (`StrLit`, or a Name the front end saw hold one string) | one string per **code point** | `runeElems` — the same per-rune literals `for c in "abc"` walks (**L11.5**) |
| a constant `range(...)` (1–3 arguments, negative step, empty range, `step == 0` declined) | the integers, in the order `range` yields them | `constRangeElems`, the reference's own `range` semantics |
| a DICT (literal, or a Name whose record is one) | its **KEYS**, in entry order | `sortedDictAnswers` / `@rt_dict_keys` — **ADR 0417**'s stride-by-two, never restated |

A dict is deliberately NOT substituted for the ordering itself: its keys come from `@rt_dict_keys` at run time,
and `sortedDictAnswers` remains the only road that decides what a dict's keys are and what comparator orders
them. The substitution and the kind door read that same road, so `print(sorted(d))` and `for k in sorted(d)`
cannot keep two answers about one dict (**Gap R.426**'s arrangement).

Every road that must recognise the argument asks the SAME question, and the four questions this row leaves
behind it are named doors rather than inline conditionals:

- `orderingSequenceArg` — the fold and the build (`sortedReversedContainerOf`, `containerSortHandle`,
  `sortedRuntime`, `builtinSorted`'s substitution),
- `orderingArgumentIsNameableSequence` — the walkable half of `sortedReversedOverWalkableList`, conjoined with
  `forIterSortedElemKind` exactly as **ADR 0339** conjoined the loop's two questions,
- `forIterSortedElemKind` — the kind half: what one element IS (`"text"`, `"number"`, `"object"`, or no answer),
- `printRoadSeesAnOrderingArgument` — does this argument reach the heap list `sortedRuntime` builds?

The last of those is why `print(sorted("bca"))` printed `0`: `builtinPrint`'s road into the container printers
was gated on `isContainerExpr(call.Args[0])`, a spelling again. A shape that road declines falls to the numeric
road, which lowers the builtin through its value answer and prints the result with `%d`. The gate is asked of
the ordering's own question now; `reversed` keeps the narrow gate, because a text handed to `reversed` is the
TEXT road's reversal and printing its iterator is **Gap R.251**'s row.

## The comparator is asked of the OBJECT, not of the argument's spelling

`@rt_sort` takes `(handle, mode)`: mode 0 compares payloads, mode 1 compares interned texts. It chose mode from
the ARGUMENT — a list variable's marker, or a literal's elements — so anything that reached the sort as neither
(a slice, a call, a substituted sequence) was ordered by payload, which for interned text is ARRIVAL order.
That is the mechanism behind three of the exit-0 wrong answers above, and it is why `sorted([3, 1, 2][0:2])`
looked fine while `sorted(xs[0:2])` of a list of text was not. The mode now comes from `forIterSortedElemKind`
asked of the argument the object denotes — through a slice (`*Slice` asks its base), through a nest (an ordering
handed another ordering asks the INNER argument), and through a substitution (a TEXT means the text
comparator, so `sorted("bca")` goes to `@rt_sort` with mode 1 rather than being folded by a compile-time copy
of an ordering this backend would have to keep in step with strcmp — **ADR 0258**'s one table).

## The binding door: a name that receives the list says what its slots hold

`xs = sorted("bca")` printed the list correctly and printed `2` for `xs[0]`, because the assignment road's
"what does this RHS bind" ladder had arms for a literal, a comprehension, a slice, a sequence the door builds
and a user function's container answer — and none for the two builtins. The name kept a handle no notebook
described, so the subscript read the payload without the tag (**ADR 0187**, **ADR 0232**). The ladder asks the
ordering now, and `recordOrderingBinding` writes the same three claims `xs = [...]` writes:

| the elements are | the claim the name gets | because |
|---|---|---|
| texts | `listElemStr` | that is what `heapListFrom` writes for a text list, and what sends the printer and the subscript to `rt_*_str` |
| integers | `listElemInt` | the plain untagged layout |
| anything else the kind door names | `mixedLists` | the builder wrote a tag per slot; the reader must ask them (**ADR 0232**) |

Asked of `forIterSortedElemKind` — the door that also picks the comparator and the loop's slot — so one list
cannot be a text list at the print door and an unlabelled word at the subscript.

## What still refuses, and in what words

A door that answers whatever it is handed fabricates verdicts, so the refusals are kept, and rewritten to
describe the shape that is actually missing (they had begun to lie: `its argument is a TEXT…` is no longer a
missing half):

| shape | the sentence says | row |
|---|---|---|
| a float or mixed element list | one loop variable holds one word; the pair is the value word this backend lacks | **Gap R.248** |
| a SET | its members live in the runtime's own table, which `@rt_sort` does not walk | **Gap R.247** (this row: the set is left) |
| a `range(...)` whose bound the program did not write | the road that spells a range's integers has no constant to read | **Gap R.250** |
| a second POSITIONAL argument | the reference's `key`, which the reference itself stops | **Gap R.249** |
| a subscript of the ordering's answer, or of an alias of a bound ordering | the elements the fold cannot see | filed **R.576**, **R.577**, **R.578** |

Filed with both legs' values, not fixed by deleting a row: `print(1 in sorted("bca"))` prints `True` where the
reference prints `False` (a number needle against interned payloads — **Gap R.575**'s family), and
`print(sorted([1.5, 0.5]))` prints arrival order (**Gap R.248**). `print(reversed("abc"))` prints `0`, the
handle, where the reference prints the iterator's word — pre-existing on both entry points, pinned here under
**Gap R.251**.

## Codegen / IR implications

- `builtinPrint`'s ordering arm: `printRoadSeesAnOrderingArgument("sorted", arg)` = container-shaped **or** a
  substituted sequence. Wider than the walkable gate on purpose: the road PROPAGATES the build door's refusal,
  which is what keeps `print(sorted([1.5, 0.5]))` a sentence instead of a `%d` of a handle (the refusal table
  is the witness, not the door table).
- `containerAnswerArgIsCarriable` answers yes for a constant `range(...)`: its answer is a list handle, the way
  `list(range(3))` builds one. Saying no is what sent `print(sorted(range(3)))` down the ordinary call road.
- A text sort emits `call void @rt_sort(i32 %h<N>, i32 1)`; an integer sort keeps mode 0. Both claims are
  asserted on the module, with the counter-rows that the value position still folds its `@.lstN` constant
  (**ADR 0472** left it alone and this row leaves it alone too).
- Handle registers stay `%h<N>` off `heapSeq`, never `%t<N>`: a register that reads as an ordinary value to the
  print and call lowerings gets printed as a number (the bug that naming was written for).

## Verification

Compiled leg, record leg, reference leg — never two engines (**ADR 0302**).

- `pkg/lang/sorted_ordering_argument_test.go` — **46** answered programs (compiler + JIT), the four-door
  agreement table over **17** expressions with the pair rule asserted in both directions, the binding door's
  CLAIM table (**7** programs — the only witness for the claim half's content, because a program that prints
  right can still read a slot wrong), **9** refusal rows carrying whole sentences, **9** neighbours, **4**
  filed divergences with both legs' values, the printf-of-a-global IR guard over every answered row with its
  three counter-rows, and the record leg over every answered row (**46**).
- `pkg/lang/for_iterable_test.go` — 9 loop rows moved from the refused table to the answered one; the owed
  table lost the five rows this row closed and kept **Gap R.248**'s and **Gap R.251**'s.
- `pkg/lang/sorted_container_operand_test.go` — two equality rows moved from refusal to answer (`sorted({"a": 1})
  == ["a"]`, `sorted(reversed([3, 1, 2])) == [1, 2, 3]`, both printing the reference's `True`), the two
  `in reversed("abc")` rows DELETED because the compiled leg now answers the reference's `True`, and the kind
  door's table widened to the text, the dict, the written-out `range` and the nest.
- `integration/sorted_ordering_argument_test.go` — **37** CLI rows re-run against live CPython in the same
  case that asserts them, **7** CLI refusals, the filed divergences at CLI altitude (a filed row whose legs
  silently converge fails, **3** of them), and the IR guard at the CLI's own compiler entry.
- `integration/programs/sorted_iterables.gy` + `testdata/sorted_iterables.expected` — 60 lines whose compiled
  output is byte-identical to CPython's, registered `parity` / `match` in the conformance matrix.
- 54 new record entries (10977 → **11031**, the 54th being the corpus program itself, recorded by the corpus recorder after `TestEveryAssertedCorpusProgramIsOnTheRecord` named it missing), each taken from the reference and cross-checked against the
  compiled leg by `tools/recmerge`; a missing record fails the row, so coverage cannot be deleted by deleting
  an entry.

**Non-vacuity**, taken by deleting each new gate and re-running the family (top-level cases / sub-cases):
the SUBSTITUTION gone → **51/44**; the KIND door blinded → **23/20**; the BINDING claim gone → **15/12**; the
PRINT door narrowed back to `isContainerExpr` → **21/17** unit and **12/11** CLI; the NESTED recursion removed
→ **8/5**; the dict's key KIND ignored (keys ordered as numbers) → **2/1** and **2/1**; the binding's TEXT
claim deleted → **12/9** and **6/4**. One mutation reddens nothing, and says why: letting the FOLD answer a
text — the alternative this ADR rejects — moves no row, because the compile-time order and `@rt_sort`'s text
comparator agree on every program in the tables today. The clause is a guard against a future drift, not an
answer road, and the row says so rather than claiming a witness it does not have.

**Differential sweep**, the parent build (`03f1bf0`) against this one, over all **11031** recorded sources:
**48** sources differ, and the classification is the claim — **44** moved from an exit-1 refusal to the reference's
answer, **3** moved from an exit-0 wrong answer to the reference's answer (the slice of a text list in both
directions and a dict's keys inside a container), and **1** differs only in the temp path printed inside its
`llc` error. Nothing moved from an answer to a refusal, nothing newly reaches **exit 2**, and every source the
child answers matches its record stdout — checked, not asserted, over all 48. The sweep also turned up two defects
this row does not own and did not cause — a decorated function whose body binds a list emits an undefined
`%f.raiseexit` (exit 2), and its twin that returns `[x][0]` prints `0` where the reference prints `3` — identical on
the parent and the grandparent builds, filed as **Gap R.579** and **Gap R.580** with their bytes rather than
quietly fixed here.

## Alternatives rejected

- **Fold text and range at compile time.** A folded text list needs a compile-time ordering of interned texts,
  i.e. a second implementation of strcmp ordering; the row's own defect (comparator chosen from a spelling) is
  what a second implementation buys you. The runtime door already had the comparator.
- **Widen the print gate's `isContainerExpr` list with the new shapes.** Same spelling test, one more entry —
  it would keep answering "is this a container?" about an argument that is an iterable, and the next iterable
  would need another entry.
- **Let the loop read the iterable's word as a count and print something.** Not the first time; that is what
  **ADR 0339** exists to forbid, and the sentences above quote the numbers it produced.
- **Fix the subscript of the ordering's answer in the same commit.** Its door (`indexListElems`) carries its own
  integer-only ordering implementation — a second ordering, the thing this ADR rejects. Widening it belongs to
  **Gap R.576**/**R.577** with that duplication removed; today it refuses, and a refusal is the ladder's good
  enough.
- **Give the alias door a claim-copy special case for the ordering binding.** The alias door is shared by four
  ADRs and fifteen record fields; a careless widening there costs more than the one program gains. Filed as
  **Gap R.578**.
