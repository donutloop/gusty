# ADR 0335: a slice hands back a copy of the base's slots, tags included, and the copy is a list the records know

Status: accepted (2026-10-14)
Amends: [ADR 0187](0187-tagged-slots-are-written-as-pairs.md) (a tagged slot is written as a pair — the slice road was the last place they were separated), [ADR 0294](0294-a-slice-of-a-container-answers-the-list-the-handle-is-not-the.md) (a slice of a container answers the list; this row makes the copy carry what the list carries), [ADR 0334](0334-a-subscript-of-a-container-handed-back-by-a-call-is-read-by-the-door-that-reads-a-variables-slot.md) (the row that named this one and left the slice refused rather than widen it into a wrong number)
Extends: [ADR 0232](0232-a-container-word-means-its-payload-and-its-tag.md) (the per-slot tag a read consults), [ADR 0175](0175-a-compiled-container-records-one-element-kind.md) (the notebooks a binding leaves), [ADR 0234](0234-a-comprehension-that-folds-is-the-literal-it-folds-to-and-a.md) (a binding records the kind it binds), [ADR 0333](0333-a-container-returned-from-a-call-is-a-value.md) (the answer a slice now copies)
Supersedes: nothing
Related: roadmap L11.1, Gap R.230 (closes), Gap R.33 (the slice half of sequence operations is paid here), Gap R.233 / R.234 / R.235 (newly measured here, filed here), Gap R.231 / R.229 / R.82 (the doors beside this one)
Owner: `pkg/lang/slice_answer_test.go`, `pkg/lang/container_return.go`, `pkg/lang/codegen.go`, `pkg/lang/heapargs.go`

## Context

`xs[i:j]` had one lowering for every base: ask `rt_slice` to copy the base's *words* into a fresh heap list.
For a list whose slots are all numbers that is the whole answer, and it printed `[1, 2]` correctly. For a list
whose slots describe themselves — a text slot, whose word is an `@str_tab` index; a float slot, whose word is a
box handle; a nested container, whose word is a heap handle — the copy carried words with no tag beside them,
and every read of the copy then read the word as the thing itself:

```
xs = [1.5, "a", [7]]
print(xs)            # [1.5, 'a']                                    — the name's road, tagged
print(xs[0:2])       # CPython: [1.5, 'a']
                     # this toolchain: [1, 0]   at exit 0             — the float's box, the text's index
ys = xs[0:2]
print(ys)            # CPython: [1.5, 'a']
                     # this toolchain: 1        at exit 0             — the copy's heap handle
```

Two wrong numbers, both written at the exit code that means *the program ran*. Neither was a missing feature:
the same list printed correctly a line earlier (`print(xs)`), and the same copy printed correctly one
statement longer (`ys = xs[0:2]` in a form the records happened to describe). The container was understood;
the copy of it was not.

Three roads were involved, and each had its own half of the truth:

- the **print** road chose its printer from the base's element notebook (`listElemStr`) and never asked the
  question the name arm beside it asks first — `mixedLists`, *do this object's slots describe themselves*;
- the road that turns the **sliced source** into a heap object asked only `literalNeedsHeap`, so a mixed
  literal was built untagged and `rt_slice` faithfully copied tags nobody had written;
- the **binding** road stored the copy's handle under a name and recorded nothing about it, so the name was a
  container no road could name — `print(ys)` answered `1`, `len(ys)` refused, `for v in ys:` printed one `0`.

## Decision

**A slice is a copy of the base's slots with their tags, and the copy is a list like any other.**

1. The print road asks the same question the name arm asks, in the same order: `mixedLists` first (the tagged
   printer), then the single-kind notebooks, then the plain printer. A copy of a tagged base is printed by
   `rt_print_list_mixed`; a copy of a list whose slots are all one kind keeps `rt_print_list` and the plain
   word copy — those rows are in the tables as the negative control, because a row that tags everything has
   asked the wrong question.
2. The road that builds the sliced literal asks **its own build site's predicate**, imported rather than
   restated: `literalNeedsHeap(lit) || literalNeedsTags(lit) || literalHasPairElement(lit)` picks
   `heapListFromTagged`, exactly as the literal road does wherever a list literal becomes an object (ADR 0187's
   pairing rule, which ADR 0294 applied to the copy and this row applies to the source).
3. A binding whose right-hand side is a slice of a list **binds a list**: `recordContainerSlice` writes the
   kind bit, the `mixedLists` fact (asked of the base with the same predicates, `containerSlotsCarryTags`), and
   clears the element notebooks when the copy is tagged — the rule ADR 0232 states for a container built by a
   call, applied to a container built by `rt_slice`. `print(ys)`, `len(ys)`, `for v in ys`, `ys[0]`, `not ys`,
   `in`, `==` and a second slice of the copy all follow from that one record, and all answer now.
4. A slice of a **call's container answer** rides ADR 0334's rewrite: `containerAnswerSubscript` grew the
   `*Slice` case beside its `*Index` case, so the base is lowered to the rooted name ADR 0333 recorded the
   answer's notebooks under and the roads that slice a container variable answer. `print(build()[0:2])` and
   `ys = build(); print(ys[0:2])` are one program written two ways — which was the whole point of ADR 0334, and
   the reason this row was the prerequisite it named.
5. **The gate.** In front of the copy sits the question *can the base's slots be described?* —
   `sliceBaseIsDescribed`, whose arms are `containerSlotsCarryTags`'s own. A base the records describe (a list
   literal, a name recorded as a list, a call that hands back a list, a slice of one) gets the copy. Anything
   else gets a refusal that names the missing record, and it is the decision's load-bearing part:
   without the gate the road emitted `rt_slice` over whatever word the base lowered to and the caller printed
   that word — `print(m()["k"][0:1])` answered the copy's heap handle as `3` at exit 0, and `print({1, 2}[0:1])`,
   `print({1: 2}[0:1])` and `print([x for x in [1, 2, 3]][0:2])` handed a compile-time global's address to a
   helper whose argument is an i32 register, which `llc` rejected: **exit 2**, the code ADR 0166 reserves for a
   compiler bug. Four shapes moved — two from a wrong number to a refusal, two from exit 2 to a refusal — and
   each now carries a roadmap row (R.234, R.235) saying which door owes it an answer.

A tag is a claim about bytes some writer wrote. Every question above is asked of the site that wrote those
bytes — the literal road, the answer road, the binding road — never guessed at the read (ADR 0232's rule), and
never asked of the base's *spelling*, which is what stopped `print(build()[0:2])` being the same program as
`print(ys[0:2])`.

### The copy is not hoisted

The gate admits a base by what the records say, not by materialising the copy somewhere the roads can find
it. A rooted name for every slice (`__cansN`, the shape ADR 0333 gives a call's answer) was tried first and
rejected: the slice's bounds are values read in the block where the slice sits, so hoisting the copy to the
statement's head changes when the bounds are evaluated, and an unhoisted copy in an expression position is
exactly the unrecorded handle the gate now refuses. The bound form keeps its name because the *program* named
it, which is the difference that matters.

## What the row leaves standing, filed rather than bundled

| shape | CPython | this backend today | row |
| --- | --- | --- | --- |
| `xs = [1, 2]` / `print(xs["a":"b"])` | `TypeError: slice indices must be integers or None or have an __index__ method` | answers `[1]` at exit 0 | **Gap R.233** — the bounds are read as words whatever they hold |
| `print({1, 2}[0:1])`, `print({1: 2}[0:1])` | `TypeError: 'set' object is not subscriptable` / `KeyError: slice(0, 1, None)` (a dict hashes the slice and reports the miss by name) | refuses (was **exit 2**) | **Gap R.234** — the slice road never asked what kind the base is |
| `s = {1, 2}` / `print(s[0:1])`, `d = {1: 2}` / `print(d[0:1])` | the same two raises | refuses (was `[1]` at exit 0) | **Gap R.234** |
| `xs = [1.5, "a", [7]]` / `print(xs[0:2][0])` | `1.5` | refuses: `index requires an inline list/dict/set literal` | **Gap R.235** — the copy has slots and no road reads them direct |
| `print(xs[0:2][9])` | `IndexError: list index out of range` | refuses (same sentence) | **Gap R.235** |
| `print(m()["k"][0:1])`, `d = [[1, 2]]` / `print(d[0][0:1])` | `[1.5]`, `[1]` | refuses: the gate, naming the missing record | **Gap R.235** |
| `print([x for x in [1, 2, 3]][0:2])` | `[1, 2, 3]` | refuses: the gate (was **exit 2**) | **Gap R.235** |
| `xs[0:2][0] = 9` | writes the slot | refuses (item-assignment road wants a variable on the left) | Gap R.229 |
| `print(xs[0:2][0] + xs[0:2][1])` over text slots | `xy` | refuses (the pair road's numeric-use sentence) | Gap R.82 |

Every one of these is a row in `roadmap.md` and an assertion in
`pkg/lang/slice_answer_test.go::TestWhatTheSliceDoorStillCannotReachIsFiledNotFixed`: a row that stops refusing
fails the test and moves into the answered tables; it does not disappear.

## IR / codegen consequences

- A slice of a tagged base now emits the tagged builder beside `rt_slice`: `@rt_alloc` + `@rt_tag_elem` +
  `@rt_set_elem` per slot (what `heapListFromTagged` emits), then `call i32 @rt_slice(...)`, then
  `call void @rt_print_list_mixed(i32 %slN, i32 0)`. The assertion is mechanical in both packages:
  `@rt_slice` beside no `@rt_tag_elem` is the shape that prints table indices, and an untagged copy beside
  `rt_print_list_mixed` is the shape that invents a tag array nobody wrote.
- A binding of a slice emits the same allocation and root as a binding of a call's container answer (ADR 0333's
  shape), because `recordContainerSlice` sets `listVars`/`mixedLists` and clears the element notebooks — a
  tagged object has no single kind, and a notebook that says otherwise sends a reader to the untagged word road.
- `rt_slice`'s own signature is unchanged (seven i32s: handle, three bounds, three presence flags), and no
  runtime helper was added: the runtime already copied tags correctly (ADR 0294). What was missing was the
  compiler telling the truth about which objects have them.
- No module in the row's corpus hands a global to a value position. That sentence is a test at both altitudes
  (`TestEverySliceModuleVerifies`, `TestNoSliceModuleReturnsAGlobalToAValuePosition`) because this road's
  history is an `llc` rejection.

## Alternatives rejected

- **Tag every slice copy.** One `rt_print_list_mixed` for all copies is simpler and wrong: it sends a plain
  `[1, 2, 3]` copy to a tag array `heapListFrom` never wrote, and turns the one shape that worked into a
  garbage print. The gate and the door grow together or neither does (ADR 0249).
- **Open the slice of a call's answer without the tag fix**, as ADR 0334's reviewers feared: `print(b()[0:2])`
  would have printed `[1, 0]` at exit 0 instead of refusing at exit 1 — a defect moved *down* the ranking,
  which is why 0334 left the row refused instead.
- **Refuse every slice whose base is not a plain name**, the conservative reading of the gate. It deletes
  `print([1.5, "a", [7]][0:2])` and `print(b()[0:2])`, both of which answer today, to avoid a shape the gate
  already refuses by predicate.
- **Fix the bounds (Gap R.233) in this commit.** The bounds question — *is this bound an integer?* — is a
  different predicate from *does this slot describe itself*, it has its own wrong answer at exit 0, and bundling
  it would hide a fresh defect inside a closed row.
- **Give the slice road a set/dict arm** (so `{1,2}[0:1]` answers something): the reference raises for both —
  `TypeError: 'set' object is not subscriptable` for a set, `KeyError: slice(0, 1, None)` for a dict, which
  hashes the slice and reports the miss by name — so what those shapes owe is a raise, not a list. Filed as
  R.234 with both sentences named.

## Verification

- `pkg/lang/slice_answer_test.go` — 39 answered shapes (three kinds of base, the untagged control, the copy's
  reads, a copy of a copy, the text slice that never left its own road), each asserted against the reference's
  value; the raise rows by class *and* message with exit 2 forbidden; the bound form and the direct form
  compared for one answer (`TestTheBoundFormAndTheDirectFormAnswerTheSameThing`); the builder/printer pairing
  asked of the emitted module with its negative control; every module verified by `llc`; and the filed table
  above.
- `integration/slice_answer_test.go` + `integration/programs/slice_answer.gy` — the same doors through the CLI,
  expectations derived from CPython (`pythonOutput`), the refusal rows pinned by exit class, stdout silence and
  the sentence that names the missing record, and the whole program line-for-line against
  `integration/testdata/slice_answer.expected`.
- `pkg/lang/container_subscript_test.go` and `integration/container_subscript_test.go` — the rows ADR 0334
  filed as refusals for this shape **moved** into the answered tables rather than being deleted, and Gap R.33's
  slice half is now witnessed there (`print(len(b()[0:2]))` answers `2`).
