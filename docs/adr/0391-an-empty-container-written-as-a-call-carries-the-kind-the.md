# An empty container written as a call is labelled with the kind the object already has

**Roadmap:** Gap R.360 (closed here — the set, the list and the dict spelled as `set()` / `list()` / `dict()`
in a slot, a print and a condition) ·
Gap R.364 (filed here — the doors that still decide the same value's kind from its spelling: `len`, the
comparison, `abs`) ·
Gap R.81 (a container key refuses; the call spelling now refuses with the literal) ·
Gap R.300 (a built set prints its arrival order — untouched by this row) ·
L11.1 (the tagged value word in the positions that store a value and read it back) ·
ADR 0232 (a container's facts are kind + per-slot tag + what the slots hold; one question, asked once) ·
ADR 0259 (a bool found its own tag in only one of three spellings — the same disease, named) ·
ADR 0310 (a member or key the pair road bound raises CPython's unhashable sentence from its tag) ·
ADR 0331 / ADR 0388 (`@rt_pair_truth`, the door a literal's element and a dict entry already decide with) ·
ADR 0199 (a built-in name the program defines belongs to the program) ·
ADR 0166 (a wrong value printed at exit 0 is the defect this repo files rather than ships)

## Status

Accepted. Landed 2026-10-21.

## Context

Six programs, all answered at **exit 0**, none of them answering the language:

```python
xs = [set()]
print(xs[0])                            # CPython: set()   — backend: 1
d = {None: set()}
print(d[None])                          # CPython: set()   — backend: 3
print([set(), list(), dict()])          # CPython: [set(), [], {}] — backend: [1, 2, 3]
print(bool({None: set()}[None]))        # CPython: False    — backend: True
print([bool(x) for x in [set()]])       # CPython: [False]  — backend: [True]
xs = [set()]
print(len(xs[0]))                       # CPython: 0         — backend: TypeError: object of type 'int' has no len()
```

The numbers were heap handles. The last line is the tell: `len` did not fail to find the object, it was
**told** the slot held an integer, and it obliged by naming `'int'`.

The tag table has carried a set since the first commit (`value.go`'s `TagSet`), ADR 0331 gave a literal's
element the pair table, and ADR 0388 gave a dict entry's truth to it. What was missing was not a runtime
helper but a **label**. A slot is two words — the payload and the tag that says what the payload means — and
the roads that write a slot ask `elemKindTag` for the second one. That predicate reads spellings:
`*FloatLit`, `*ListLit`, `*DictLit`, `*SetLit`, `*BoolLit`, `*NoneLit`, `*StrLit`, `*IntLit`, a name the
container analysis describes, an instance, a call that answers text or a float. `set()` is none of those: it
is a `*Call` whose callee happens to be a built-in name, so it fell through to the last line of the
predicate — `return int32(TagInt), true` — and the handle went into the slot labelled as an integer.

Downstream, nobody re-asks. The printer dispatches on the tag; `@rt_pair_truth`'s container arm never runs
because tag 0 is the numeric arm; `len` trusts the label the store wrote. ADR 0259's bool is the exact
precedent — "the tag table grew a bool and only one of the three spellings noticed" — and ADR 0232 is the
rule that was broken: the gates that DECIDE whether a value is a container (`taggableNestedElem`,
`foldArgCarriesAContainer`, the print gate, `isContainerExpr`) all asked `emptyContainerLiteral`; the roads
that LABEL it asked the spellings. One question, answered in two places, drifting.

## Decision

**Ask the one door.** `emptyContainerLiteral` — the shape that maps a zero-argument `set` / `list` / `dict`
to the empty literal it means — becomes the source of the label as well as the gate, through one new
codegen predicate:

- `(*irGen).emptyContainerCall(e)` is `emptyContainerLiteral` **plus** `builtinShadowed`: a program that
  defines `set` itself owns the name (ADR 0199), and labelling its call with a set's tag would be a
  fabricated fact about an integer. Every road that is about to *label* a value asks this one; a road that
  only recognises the shape may still ask the bare door.
- `elemKindTag` gains the arm: an unshadowed `set()` / `list()` / `dict()` answers `TagSet` / `TagList` /
  `TagDict`. This is the store side — `rt_tag_elem`, `rt_append_tagged`, `rt_dict_put_tagged`,
  `rt_set_add_tagged` all take the tag from it, and `elemsTaggable` now sees a self-describing member, so
  the container is built with the `rt_mark_estr` bit 8 that makes the printer read the slot tags at all.
- `literalNeedsHeap` and `literalNeedsTags` — the two gates beside every literal road — count the call
  spelling as the container it is. `print([set(), list(), dict()])` printed `[1, 2, 3]` specifically because
  `literalNeedsHeap` asks "does some element need a word the static int array cannot hold", and a call was
  not on the list. `literalNeedsHeap` moves from a package function to a method for exactly this reason:
  the shadow question needs the generator.
- `dictKeyIsContainer` (the hashability question, now one method instead of an in-line switch) counts the
  call spelling too, so `{set(): 1}` refuses with the sentence `{[1]: 1}` refuses with, naming **"an empty
  set"** rather than `*lang.Call` — `exprSnippet` gained the arm for the same "words the program wrote" rule
  its `*FString` case carries (Gap R.249).

Nothing else moved. No runtime helper was added, no tag was invented, and the tag vocabulary in `value.go`
is untouched: this row writes the tag that was always there.

## The tag, asserted in the emitted language

`pkg/lang/empty_container_tag_test.go` asserts the store, not the story:

```
xs = [set()]          →  call void @rt_tag_elem(i32 %h1, i32 0, i32 7)
d = {None: set()}     →  call void @rt_dict_put_tagged(i32 %t1, i32 0, i32 %h1, i32 3, i32 7)
xs.append(list())     →  call void @rt_append_tagged(i32 %h3, i32 %h2, i32 5)
print({"k": dict()})  →  call void @rt_dict_put_tagged(i32 %t1, i32 %t3, i32 %h1, i32 4, i32 6)
[bool(x) …  [set()]]  →  %t3 = call i32 @rt_pair_truth(i32 %_x.ld1, i32 7)
```

and its negative controls, because a gate that widened past what it needs is the next row's bug: an integer
slot still emits `rt_dict_put` with tag `0`, an all-integer literal still folds to `@.lst` with no tagged
append at all, and

```python
def set():
    return 1

xs = [set()]
print(xs)              # [1] — the program's definition wins, and no `i32 7` reaches the module
```

## Codegen / IR implications

- `mixed` in `heapDictFrom` / `heapSetFrom` is reached by more programs: a member or value whose kind is not
  the integer word is self-describing, so bit 8 is set and the builders take the tagged doors. That is the
  whole of the print fix — the container printer already read slot tags; it had never been told to.
- `staticSlotTag` → `staticElemExpr` → `elemKindTag` is the read side, so the entry read that ADR 0353 and
  ADR 0388 route through `entryReadNeedsTheTag` now sees tag 5/6/7 for an entry written by the call spelling,
  which is what makes `print(d[None])` and `bool(d[None])` ask the object.
- Refusal surfaces move only where a wrong answer used to be: the four one-word positions this row does not
  own (`len` of the call, `list() == []`, `abs(list())`, `str()` of a loop variable over them) still refuse
  or still answer as they did, pinned with the reference beside them in the filed table.

## Alternatives rejected

- **Special-case `set()` in the printer.** Fixes one line of the six and leaves `len`, `==` and the truth
  table reading a tag that lies. A label corrected at the read site is a label still wrong for every other
  reader.
- **Write the tag from the object at run time** (`rt_tag_of` consulting the arena's kind word). The arena
  already knows the kind of a *container*; the compiler is the one that said "integer" at the store, and a
  run-time read cannot fix a slot whose payload was already chosen by the label (`rt_lift_num` unboxes a
  tag-0 payload as a number).
- **Refuse the empty-container call in a container slot.** Honest, and it is what the sibling spellings that
  cannot be labelled do — but this value is labellable, the reference answers all six programs, and a
  refusal where the language has an answer is the defect ADR 0166's sibling rows exist to remove.
- **Leave `literalNeedsHeap` a package function and add a call-site-by-call-site widening.** Eight call
  sites already pair it with a `g.`-method; widening the eight roads separately is how `set()`'s print gate
  and its store door came to disagree in the first place.

## Consequences

- 40 sources went to `tools/recmerge` — every one accepted, which is the record leg confirming that the
  compiled answer moved **toward** the reference and not merely somewhere new (the recorder refuses a
  source whose two legs disagree).
- The three rows `TestTheTruthOfASetEntryIsStillAskedOfTheHandle` pinned are **deleted**, as their own text
  required: they said "flip these to answers", and a pinned wrong answer kept after the fix is a second
  truth about the language.
- Filed for the doors this row does not reach: **Gap R.364** — `staticContainerKind` and
  `containerHandleOf`, and the `len` / comparison / `abs` gates that consult them, still decide an empty
  container's kind from its spelling; `print(set() == set())` answers `False` and `print(abs(list()))`
  answers `0`, both at exit 0.
