# Containers compare by value: rt_container_eq and the tag that makes it sound

## Context

`==` on two containers was an integer comparison of heap handles. That produced wrong answers in
**both directions**, which is the worst property a comparison can have:

```gusty
xs = [1, 2]
ys = [1, 2]
print(xs == ys)        # both backends: False        CPython: True
print([1] == [1])      # both backends: False        CPython: True
print([0] == ["zero"]) # would be: True              CPython: False
if [1] == 1: ...       # llc refuses the module: icmp eq i32 @.lst1, 1
```

The first two came from comparing slots. The third is the interesting one: a stored string is an
index into `@str_tab`, so the container holding the number `0` and the container holding the first
interned string have the *same payload* — a payload-only comparison calls them equal. And the
fourth is the shape this repo keeps meeting: a container literal in a new position, its static
global handed to an instruction as an `i32`.

The interpreter agreed with the compiled backend on the first two, because `eqVal` compared
handles for everything that was not a float or a string, and `!=` was not even the negation of
`==` — it was a raw `l != r`, so `[1] == [1]` and `[1] != [1]` both answered False.

## Decision

**1. `==`/`!=` on containers call `rt_container_eq`.** Lists compare position by position; sets
and dicts compare by *containment*, because `{1, 2} == {2, 1}` and `{"a": 1, "b": 2} == {"b": 2,
"a": 1}` are True in Python and a positional walk makes them False. Kinds must match, sizes must
match, and a container compared with a non-container is simply unequal — which is also Python's
answer for `[1] == 1`.

**2. Element comparison reads the `(payload, tag)` pair, not the payload.** `rt_slot_eq` loads
both halves from `@heap_elems` and `@heap_tags` and requires both. The tag is not decoration here:
without it the payload collision above calls `[0]` and `["zero"]` equal. This is stub-proven —
delete the builder tag stores and `TestTaggedElementsDistinguishPayloadCollisions` answers True
where CPython answers False.

**3. Every container builder writes tags. This is ADR 0187's pairing rule stated for the whole
runtime, and it turned out to be the part that was still missing.** Tags were written by the
mixed-list literal path (ADR 0184) and by element writes (ADR 0187), but not by: the plain
list/dict/set literal builders in `heapargs.go`, the container-variable assignment builders in
`codegen.go`, or the call-argument builder `heapArg`, which hand-rolled its own
`rt_alloc`+`rt_dict_put` loop. An untagged slot is *uninitialised memory* — it holds whatever the
previous tenant of that heap slot left — so a container could compare unequal to an identical
container depending on which slot it landed on. Fix: those builders all write tags, and
`heapArg`'s set/dict branches now **delegate to `heapSetFrom`/`heapDictFrom`** instead of
re-implementing the loop, so there is one place where a container is built. Mutation goes through
tagged variants (`rt_append_tagged`, `rt_set_add_tagged`, `rt_dict_put_tagged`, and
`rt_put_elem`+`rt_tag_elem`), which also covers the update path of a dict entry — a key that
starts mapping to a different kind must not keep the old value tag.

**4. Comparison against an unknown kind refuses.** Container-vs-proven-scalar is decided
statically (and both operands are still evaluated, so `f() == xs` keeps `f`'s side effects); but
if the compiler cannot see what the other side is — `xs == make()`, `xs == echo(1)` — it refuses:
"comparing a container with X needs a tagged value". That is L11.2's gap named precisely, and
guessing there means comparing a handle with a number and calling the result a bool.

**5. `is` stays identity.** Python asks identity with `is` and value with `==`; the backend now
has the same two questions, and the corpus row checks both (`xs is xs` True, `xs is ys` False for
equal lists).

**6. The interpreter moved to the same rules**, including `!=` becoming the negation of `==`.
Set/dict equality in the interpreter is containment over `elems`/`dvals`; instances, classes and
closures stay identity-compared, as in Python without `__eq__`.

## Alternatives rejected

- **Leave `==` as identity and document it.** Python users write `xs == ys` expecting value
  equality; a language that advertises Python ergonomics and answers False there has chosen its
  side badly. Refusing would have been better than answering wrong — but here being right is
  cheap, since the tags already exist.
- **Compare payloads only, and treat the collision as unreachable.** It is not unreachable: the
  interned index of a string is a small non-negative integer, exactly the shape an int list holds.
  The AOT backend cannot currently put a string and a number in the same *container* (mixed sets
  refuse), but it can compare two containers built differently — which is what the test does.
  Related and still latent: dict key lookup and set dedup compare payloads only, so they become
  unsound the moment heterogeneous keys are allowed (L11.1 (1b) must carry the tag into
  `rt_dict_get`/`rt_set_add`, not just into equality).
- **Compare by rendering both containers to text and comparing strings.** It would have agreed
  with `print` by construction, and it is a lie for anything that prints ambiguously (`0` vs
  `"0"`, `None`), plus O(allocations) per comparison.
- **Widen `@heap` elements to `{payload, tag}` so the pair is inseparable.** Still the right
  endgame (roadmap L11.1 step 5), still a change to every container algorithm at once. The pairing
  rule plus `TestEveryContainerBuilderWritesTags` holds the line meanwhile.
- **Give values a tag word and dispatch everything on it (ADR 0168's tagged value).** That is
  L11.2, and it is what turns this refusal (point 4) into support.

## Consequences

- `@heap_tags` has a third consumer — printer (ADR 0184/0185), element reads (ADR 0187), equality
  (this one) — which makes "a builder that forgets the tag" a correctness bug, not a cosmetic one.
  `TestEveryContainerBuilderWritesTags` is the tripwire across ten build paths; the collision test
  above is the one with teeth.
- One less divergence class in the corpus: 63 rows, 48 parity, 0 fail; oracle 32 `match` /
  21 `debt` / 10 `not_applicable`, 0 drift, 0 skipped. `programs/container_equality.gy` reports
  verdicts through `if`, so the row tests equality and not the bool-rendering debt that
  `probe_bool_value` pins — the two debts stay separately visible.
- `heapArg`'s hand-rolled set/dict builders are gone (and `heapElem` with them): call arguments,
  literals and assignments now share one builder each, which is where a tag has to be written.
- The remaining comparison gaps are named, not silent: an unknown-kind operand (L11.2), and
  membership/dedup on untagged keys for heterogeneous dicts and sets (L11.1 (1b)).
