# 0472 — the membership, equality, element-slot and chain doors ask a `sorted`/`reversed` what it IS

**Status:** Accepted
**Date:** 2026-10-14
**Amends:** [ADR 0333](0333-a-container-answer-is-a-pair-of-words-not-a-word.md), [ADR 0339](0339-the-for-loop-asks-its-iterable-what-it-is-before-choosing-a-road.md)
**Roadmap rows:** **Gap R.253** (closed), **Gaps R.568–R.575** (filed); pays the measurement ADR 0339 recorded and deliberately left
**Related:** [ADR 0166](0166-exit-code-contract.md), [ADR 0335](0335-a-text-answer-means-a-real-string-table-entry-and-a-kind-that-admits-it.md), [ADR 0258](0258-reversed-answers-a-real-heap-list-and-the-same-roads-answer-it.md), [ADR 0234](0234-), [ADR 0339](0339-), [ADR 0233](0233-), [ADR 0457](0457-)

> Shape borrowed from ADR 0001; the Argument-as-questions body is ADR 0197's, the
> falsifiable-gate §4 is ADR 0470's, the "one table or one word" rule is ADR 0258's.
> This ADR ships with the repair it decides.

## Context

`for` asked its iterable what it is before choosing a road (**ADR 0339**). The four doors beside
it — `in`, `==`, an element slot, a comparison chain — never did. `sorted`/`reversed` sit in the
**value road** (`builtinCallShape` → `foldBuiltins`), which answers these two builtins by writing
their elements down as a module constant:

```
@.lst1 = private global {i32, [3 x i32]} { i32 3, [3 x i32] … }
```

A container road wants a *handle* to a heap container. The doors never asked which of the two
they were holding, so the constant went out in an `i32` argument slot:

```llvm
%t2 = call i32 @rt_contains(i32 @.lst1, i32 1)            ; print(1 in sorted([3,1,2]))
%t2 = call i32 @rt_contains_tagged(i32 %t1, i32 @.lst1, i32 0)
%t1 = icmp eq i32 @.lst1, @.lst2                           ; sorted(…) == sorted(…)
call void @rt_append_tagged(i32 %h2, i32 @.lst1, i32 0)    ; xs[1] = sorted([4,5])
call void @rt_put_elem(i32 %_xs.ld1, i32 %t5, i32 @.lst2)  ; xs[0] = sorted([4,5])
call void @rt_dict_put(i32 %t1, i32 %t3, i32 @.lst1)       ; d["k"] = sorted(…)
store i32 @.lst1, i32* %_chain1                            ; sorted(…) == True == True
```

and `llc-20` answered every one of them:

```
llc: error: llc-20 <tmp>.ll -filetype=null: global variable reference must have pointer type
```

That is `gustyc`'s **exit 2** — the code ADR 0166 reserves for *a compiler bug* — for programs the
reference answers with `True`. `print(1 in sorted([3, 1, 2]))` was enough to reach it.

**The measurement ADR 0339 left behind.** Closing the loop did not close these doors, and it
recorded why it could not close them by widening the pair:

> Deliberately NOT through `containerHandleOf`'s tagged arm, and the pair was NOT widened: doing
> so turned `print(1 in sorted(xs))` — which answers `True` — into a refusal, the measurement filed
> as **Gap R.253**.

That measurement was right about the pair and wrong about what it implied. The pair has **three**
roads — the decider, the handle, and the operand — and teaching two of three is what turned an
answered program into a refusal. The row's own "what is owed" cell had already named it: *"give
`containerOperand` the arm `containerHandleOf` has."*

## Decision

### 1. A builtin call that answers a container goes through the pair like any other container

The doors now ask **one** question — "is this expression a container?" — and the answer is built by
the roads the pair already has, in the order ADR 0333 requires them to be asked (decide → name →
build), each sharing an implementation with its neighbour rather than restating it:

| road | function | shared with |
|---|---|---|
| decide | `sortedReversedIsAContainer` | `isContainerExpr`'s `*Call` arm |
| name — payload | `sortedReversedContainerIsNameable` | **ADR 0339**'s `sortedReversedOverWalkableList` |
| name — tag | `sortedReversedContainerElemKind` | the loop's own `forIterSortedElemKind` |
| refuse | `sortedReversedIsBeyondTheDoor` → `sortedReversedDoorRefusal` | one sentence, asked by `sortedReversedDoorIsAskedOf` at every door |
| build | `sortedReversedContainerOf` | the pair's `containerHandleOf`/`containerOperand` arms, and `containerSortHandle` |

The naming road conjoins BOTH halves of the pair, and that is load-bearing rather than tidy: the walk can
name a payload road for a shape whose element has no tag road — a dict literal, whose walked elements are its
KEYS while the tag road reads its VALUES, and a mixed or float literal, which is **Gap R.248**'s — so a door
consulting only the walkability half would build a container whose slots carry a kind its elements do not
have. The loop has always asked both; the doors ask the same function, in the same order (**ADR 0232**: one
question, one asking place).

The decider's three clauses are the three reasons an argument is walkable, and the name road is
literally ADR 0339's predicate:

* a **literal** of one kind — the elements can be written down;
* **reversed's literal** — integers or interned text, because `@rt_reverse` moves words and does
  not consult a tag (a float would come back 0, a text would compare payloads — the reference's
  own refusal of `sorted([1,"a"])` is a refusal of that same wrong answer);
* a **nameable list** — variable, slice or call, what the loop already accepts.

The two build roads are also ADR 0339's, chosen by the same switch: `@rt_sort` over the copy
(its mixed-list guard raises the builtin's own `TypeError`), `@rt_list_copy` + `@rt_reverse` for
the flip. **Nothing about the sort, the flip or the ordering is written again here.**

### 2. A door that already had a fold still builds (ADR 0234/0335's shape)

`sortedLiteralElems` answers the fold, and the fold stays where it was: `xs = sorted([3,1,2])` /
`print(xs[0])` still emits `@.lst` — a counter-row asserts it. But where a door needs a handle, the
door BUILDS the same elements through the literal roads:

```go
case g.sortedReversedDoorIsAskedOf(e):      // build …
    h, err := g.sortedReversedContainerOf(b, e)   // … or REFUSE, in words
    if err != nil {
        return "", err
    }
    return h, nil
```

The third clause is what the pair costs and what ADR 0333 does not say out loud: a door that recognises only
the shapes it can BUILD would walk past the shape it cannot and read the builtin's folded word instead, which
is this row's exit-2 class wearing the pair's clothes. `sortedReversedDoorIsAskedOf` is therefore
build-**or**-refuse, and the refusal is returned as an error by the same build function, so a door cannot
handle one without handling the other.

The fold is one spelling of the answer, not the answer — the same distinction ADR 0470 drew between
`rt_str_slice` and `rt_str_nchars`.

### 3. One ordering, three questions (ADR 0258's rule)

`sortedReversedContainerOf` asks **`sortedLiteralElems`** — the function `print(sorted(…))` renders
and, through `foldedForIterable`, the function **ADR 0339's loop** walks. A test prints the fold's
own bytes against a membership probe of the same list and against the loop's output, with lists
whose *first* element is the last one sorted and whose *last* element is the first one sorted, so a
door that ordered a copy its own way goes red. The reverse= road reuses the sort's own `reverse`
arm rather than flipping a copy twice.

### 4. `reversed` answers as what the reference says it is — an iterator (Gap R.195's word)

The loop may walk `reversed`'s flipped copy; a *comparison* may not compare it. The reference:

```python
>>> reversed([1,2]) == [2,1]                     → False
>>> reversed([1,2]) == reversed([1,2])           → False
>>> reversed([1,2]) in [[2,1]]                   → False
>>> 2 in reversed([1,2,3])                       → True     # the haystack is walkable
```

The needle roads take the reference's answer before anything is built — `iteratorEqualsAContainer`
(False, whatever the other side is, two sides or none) and `iteratorIsNoMember` — because an
iterator is not a list and is never a member of anything. The haystack roads still walk the copy:
`in` asks a sequence whether it holds the needle, and `@rt_contains` does not consult a tag, so the
same walkability guard as the loop applies. This is a decision about an answer, so it is pinned by
its own test rather than by the file's IR claims.

### 5. The element door asks for a tag, and declines rather than pretending

`sortedReversedSlotsAreTaggable` answers the question `[sorted(…)]`, `{k: sorted(…)}`,
`(sorted(…))` and `xs.append(sorted(…))` ask: a literal of one kind may be tagged (the same
`taggableNestedElem` question as any nested container), and anything else is refused naming
**Gap R.248** — the float comparator — rather than stored in a slot whose tag claims something else.

The same rule is what makes a MIXED argument refused rather than answered: the row below states it
as a decision, so the falsifiable gate is a *refusal*, not a source of values — the distinction
ADR 0335 §2/§5 and ADR 0339 §5 draw.

There are SIX roads that can store such an element, not one: `in`, `==`, a list literal's slot, a set
literal's element, `xs.append(...)`, and a dict value. Each of them has a gate of its own that fires first —
the mixed-element-kind gate, **Gap J.6** — and each of those gates describes this shape wrongly ("a compiled
list holds either strings or numbers, not both", said of a list holding one `sorted` call). All six now ask
`sortedReversedRefusalInside` first, so the program hears the container door's sentence whichever road it
arrives on (**ADR 0249**'s rule that a refusal names the missing half, applied across doors rather than inside
one).

### 6. The chain road refuses; it does not store a global

`1 in sorted([3,1,2]) == True` is a comparison **chain**, and a chain operand is a `store` into an
`i32` slot. `chainOperandIsContainer` now asks the container question, so the chain's existing
refusal fires — "would store a container global into an i32 slot" at exit 1 naming L11.1 /
Gap R.53 — instead of `llc` refusing the module at exit 2.

### 7. A text argument is a container of text (ADR 0335's shape, at this door)

`sorted("bca")` holds one-character strings, and `containerOf`'s TEXT arm interns each one. Which
lets `print("a" in sorted("bca"))` answer the reference's `True`. It does NOT let `"c" in
reversed("abc")` answer: the flip moves words and an interned text is not its payload — the same
word ADR 0335 §4 filed as Gap R.563, at this door, filed as **Gap R.570**.

## Argument

**1. Why not simply teach `isContainerExpr` the call, as the row's cell says?**
Because the row's own measurement says that is the change that breaks `print(1 in sorted(xs))`. The
decider is consulted by roads that will not build; when the decider says yes and the builder has no
arm, those roads do not fall back to the fold — they refuse. ADR 0333's pair is a *pair* because the
three roads must be widened together, and that is what §1 does: the decide/name/build triad, one
function per question, all three grown in the same commit.

**2. Why is `reversed` answered differently at the comparison doors than in the loop?**
Because the reference does. A `list_reverseiterator` has no `__eq__` against a list and is not
equal to a second iterator of the same list. Answering `reversed([1,2]) == [2,1]` with `True` was
not "the pair, applied consistently" — it was the pair answering a question the reference never
asks, because the value the comparison holds is not the value the loop holds. The loop reads a
sequence; the comparison holds a value. Gap R.195 says those are two things and this row is the
first to act on it at these doors.

**3. Why not fix the wrong answers found on the way — `1 in sorted([1.5, 0.5])`, `None in
sorted([1, None])`, `sa.add(sorted(…))`?**
Because each needs a word this backend does not have — a float comparator, a None tag, a hash —
and each is one of the nine rows ADR 0470 §2 lists under *"a refusal is a decision, a wrong answer
found beside it is a row"*. Widen the door to catch one and the door starts answering questions it
cannot ask: `sorted([None])` is refused at the loop door today, so a membership door that accepted
it would print the reference's `TypeError` as the payload `0`. They are filed as Gaps R.568–R.575
with their compiled and reference values pinned in the same table, and the guard that reads that
table goes red if either value changes.

**4. Why is the fold left in place rather than replaced by the build?**
Because the fold is cheaper and already correct where nothing needs a handle, and because replacing
it would move every module constant in the corpus — an unbounded change for a row about four doors.
A test asserts the fold is still emitted for `xs = sorted(…)` / `print(xs[0])`: this row adds a
second way to answer, it does not take the first one away.

**5. Why a new file rather than arms inside `heapargs.go`?**
Because the file's header is where the IR is written down: the reader who wants to know why
`print(1 in sorted([3,1,2]))` is not `rt_contains(i32 @.lst1, i32 1)` should find the instruction
there, and each road it must stay in step with gets its own paragraph. The arms in
`heapargs.go`, `codegen_binop.go`, `membership_literal.go` and `codegen_arith.go` stay two lines
each, which is what ADR 0333's "same question" rule asks for.

## Consequences

* `print(1 in sorted([3,1,2]))`, `print(5 not in sorted([3,1,2]))`, `print(2 in reversed([1,2,3]))`,
  `print(1 in sorted([]))`, `print(3 in sorted([1,2,3], reverse=True))` answer the reference at
  exit 0 — with the constant out of the helper's slot.
* `print(sorted([3,1,2]) == [1,2,3])` (both sides), `print(sorted([3,1,2]) == sorted([3,1,2]))` and
  `print(sorted([]) == [])` answer `True`; `print(sorted([3,1,2]) in [[1,2,3]])` answers `True`.
* `print(reversed([1,2]) == [2,1])`, `… == reversed([1,2])` and `… in [[2,1]]` answer the
  reference's `False` where they answered `True`, and no module builds a container for them.
* Element slots answer: `print([sorted([1])][0])` → `[[1]]`, `print({1: sorted([2])})` →
  `{1: [2]}`, `print((sorted([3,2]),))` → `[[2, 3]]`, `xs.append(sorted(…))`, `xs[0] = sorted(…)`,
  `d["k"] = sorted(…)` all print the reference's bytes.
* No exit 2 in the family: `print(1 in sorted([3,1,2]) == True)`, `print(1 == sorted([3,1,2]))` and
  every argument the builtins refuse leave the program at exit 1 with the door's own sentence — including
  `sorted(xs, True)`, whose second POSITIONAL argument is the reference's `key`: the value road still folds
  that call to `[3, 2, 1]` (a wrong answer already on record), and every one of the six roads that could
  store it now speaks one refusal instead of reading the fold's word as a handle.
* `gustyc --verify`/`--emit-llvm` accept every module the new suite compiles.
* 46 sources newly on the record leg (10931 → 10977), written by `tools/recmerge` from the
  reference with the compiled leg cross-checked; `integration/programs/sorted_container.gy` is a
  `parity`/`match` row of the matrix, compared line-for-line with CPython.

## Counter-rows

* **`xs = sorted([3,1,2])` / `print(xs[0])` still emits `@.lst`** — the fold is not deleted.
* **`print(1 in sorted([1.5, 0.5]))` still prints `True`** where CPython prints `False` (Gap R.248),
  and 8 further rows in `filedNotFixed` keep their compiled value beside the reference's. A row that
  fixes one of them must edit that table, not quietly move a value.
* **`len(reversed([1,2,3])) == 3` still answers `True`** at exit 0 where the reference raises
  `TypeError` (Gap R.251) — the row's iterator rule covers a comparison whose two sides are both
  lists, not one that compares a length.
* **The guard is not vacuous, and neither is any of the seven clauses.** Each deletion was measured; the
  table under *Verification* carries the numbers, and they are the reason the row's claim is a triad rather
  than a slogan: the decider 64 sub-cases, the fold clause 10, the nameable gate 11, the refusal clause 12,
  the build door 14, the iterator rule exactly 4 rows moving `False` → `True`, and the kind door caught
  lying in 9 of its own 14 rows. Two of those numbers carry the argument: the build door takes down one
  NEIGHBOUR and one FILED row beside the answered ones (so the roads are shared, not new), and the two
  clauses of the nameable gate cannot be separated by any program row (so the tag half is pinned where it
  answers instead).
* **The neighbours still answer**: `print(1 in [1,2])`, `print([1,2] == [1,2])`, `print(2 in [2,"a"])`
  (the tagged door), `print(len(sorted([3,1,2])))` and `print(2 in ([1,2] if 1 else [9]))` (ADR
  0405's arm road) go through unchanged, asserted by the same tests that assert the new rows.

## Verification

`pkg/lang/sorted_container_operand_test.go` NEW, **11** test functions:

* **44** answered programs (`membershipAndEqualityAnswered`), each read out of the record
  (`goldenLookup`, a missing entry failing the case), run on the compiled leg, and checked for the
  instruction shape — a membership or equality probe asks for `call i32 @rt_contains…(i32 %` /
  `@rt_container_eq(i32 %t…, i32 %…)` and forbids `@.lst`/`@.set`/`@.dict` in that slot; an
  element-slot row forbids a constant reaching `rt_append_tagged`/`rt_put_elem`/`rt_dict_put`/
  `rt_set_elem`/`rt_dict_put_tagged` and REQUIRES the `rt_alloc` that replaced it; a refused row
  requires no helper call at all. The rows whose list is nameable also run on the JIT, and the
  print-only rows are checked for emitting `@rt_print_list` rather than passing on an absent claim.
* **13** refused programs (`membershipAndEqualityRefused`), each asserting the sentence the door prints
  and whether that sentence cites a roadmap row — the builtin's mixed-list sentence, the chain door's,
  **ADR 0189**'s tagged-value sentence, the owed rows naming **Gap R.248**, and the one sentence the
  reference's-`key` argument meets at ALL FIVE roads that could store it (`in`, `==`, a list slot, a set
  literal, an `append`, a dict value). One shape, one sentence, whichever door meets it first
  (**ADR 0249**); the guard's job is to make sure none of them falls through to the mixed-kind gate
  (Gap J.6), which describes the shape wrongly.
* **10** neighbour rows, **3** ordering programs asked three ways (the fold's bytes, a membership probe of
  every position, the loop's output), and **8** filed-not-fixed rows each asked of the compiled leg AND of
  a live `python3`, so a value that moves without the row moving fails.
* **13** decide/build agreement rows over the node the parser built, asserting
  `container == (fold || name)`, `askedOf == (container || refuse)`, and, where a tag is claimed, that the
  element road and the container door agree on it (**ADR 0233**, **ADR 0457**).
* **14** rows for the kind door alone (`TestTheKindDoorNamesWhatTheElementsAre`), asserting the STRING it
  returns — `number`, `text`, or no kind at all — because the program rows below witness the CONJUNCTION of
  the nameable gate's two clauses, not the second one. This is the witness for the tag half's CONTENT:
  making the door lie about the kind (always `number`, always nameable) reddens **9** of its rows when no
  program row changes at all (roadmap **Gap R.248**'s tag road, **ADR 0233**).
* `TestTheDoorsNeverHandAContainerGlobalToAHelper` over **12** programs, and `TestTheGuardIsNotVacuous`
  carrying the guard's own soundness: ten instruction lines of the shape the row closed (each of the seven
  helpers, both orders, the tagged and untagged spellings) must match, and four register-shaped lines must
  not.

### The seven deletions, and what each one costs

A row closed by adding clauses has to be shown to need each of them. Each line below is one deletion, the
number of red sub-cases it turns, and WHICH table they come from — measured on this commit, not reasoned
about (roadmap **Gap R.253**; the discipline is **ADR 0258**'s):

| deletion | red | where |
|---|---|---|
| the decide door answers `false` | **64** | 38 answered, 13 guard, 6 pair, 3 ordering, 3 refusals, 1 filed |
| the fold clause answers `false` | **10** | 3 answered, 5 pair, 2 guard |
| the nameable gate answers `false` (either clause) | **11** | 7 answered, 4 pair |
| the kind door LIES about the kind it names | **9** | 9 of the kind door's own 14 rows, 0 program rows |
| the iterator identity rule answers `false` | **4** | 4 answered rows move from `False` to `True` |
| the refusal clause answers `false` | **12** | 6 refusals, 5 guard, 1 pair |
| the build door is disabled | **14** | 12 answered, 1 neighbour, 1 filed |

The last line is the reason the build half is not allowed to be a `bool`: with the build door disabled the
answered programs do not all fail the same way — one neighbour row and one filed row fail with it, which is
the evidence that these roads are shared and not new.

`integration/sorted_container_test.go` NEW, **4** test functions: **45** programs run through the CLI
against `python3` with the IR claim beside each — the IR is compiled from the same file the CLI ran, so
the sentence and the instruction cannot be about different programs — **6** refusal rows, the
no-container-global guard, and the corpus program's lines compared one for one.
`integration/programs/sorted_container.gy` + `integration/testdata/sorted_container.expected` are
registered `parity`/`match`.

The record leg: **46** sources came in from CPython with the compiled leg cross-checked
(**10931 → 10977**), none self-certifying (`tools/recmerge` refuses an entry the compiled leg will not
agree to, and a missing entry fails a row, so the coverage cannot rot by deletion).

Differential sweep: **6391 + 6321** corpus sources re-run through a parent-commit build (`5a610ff`,
**ADR 0470**) and this one, stdout / exit code / trap class compared — **0 sources moved**.

