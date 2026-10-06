# 0306. A pair-bound name enters a container by way of its tag

Status: accepted. Cycle: roadmap `L11.1` (tagged value word), `Gap R.146` (the positions that keep one
word for a whole value) — the **list element** half. Land: compiled backend only (`pkg/lang`), verified
through `llc-20`.

## The decision

**A container element is no longer a position that keeps one word for its operand.** A list literal whose
element is a name the pair road bound (`n = xs[0]` over a container the program built) is lowered to the
heap builder, and that element writes **both** of its words: the payload through `@rt_set_elem`, the tag
through `@rt_tag_elem`, the tag taken from the name's own tag alloca rather than from the builder's
constant. The literal is marked self-describing (bit 8) so the printer reads the slots instead of the list's
single declared kind.

This holds for `print([n])`, for every mix with literal elements (`[n, "x", 2, None]`), for the assignment
road (`y = [n]`), and for every position that reads the list back — `len`, subscript, `in`, `append`, `for`,
`str`, `repr`, `==`.

It does **not** extend to a dict entry, a set member, or a literal that a builtin (`sum`, `min`, `max`)
folds into a static array. Those roads ask every element for one word before any tag question is put, and
they keep Gap R.146's refusal, unchanged, owned by the same roadmap row.

## Why this shape and not the obvious one

A pair-bound name is two `i32` allocas (`_n`, `_n_tag`) whose payload *means a different thing per kind*:
the number itself (`int`, `bool`), a handle on an `@float_box`, an index into `@str_tab`, an entry count.
Only the tag says which (ADR 0252's closed tag set).

A list literal had two lowerings, and the pair fit neither:

- the **static** one, a `@.lstN` global — a compile-time constant, so an element whose shape is a run-time
  fact has no spelling in it;
- the **heap** one, opened by `literalNeedsHeap` (does a payload fit an `i32` slot?) or `literalNeedsTags`
  (can a slot say what it holds?) — and a pair element answers *both* questions "no" for the wrong reason:
  the payload fits, but nobody knows what it is.

So `print([n])` refused while `print(n)` had been answering since ADR 0303, which is the asymmetry
`Gap R.146` was filed to describe.

What the heap builder actually needed was never a new runtime. `@rt_tag_elem(i32 %h, i32 %i, i32 %tag)`
takes an `i32`, and **a register is an `i32`**: what a literal element states as a constant, a pair element
states as a fact the objects wrote. The three gates gained one disjunct each (`literalHasPairElement`), and
the element loop gained one arm each.

## The bit that decides the print

Marking the literal self-describing is not decoration. The list's `estrBits` tell the printer whether to ask
each slot or to trust the list's one declared kind. Without bit 8 the printer reads the payloads through that
single kind, and `["a"]` — one interned index — comes back as `[0]`. That is `Gap R.38`'s wrong-answer
family (a plausible number where the reference has a string) arriving through the very door this cycle
opened, which is why the text-slot rows are in both test files rather than only the unit one.

The same bit was already written for a mixed literal and, on the set and dict paths, for a mixed object all
along; the list assignment path had been recording the fact only in the compiler's own scope, which is what
ADR 0258 fixed for `str`/`repr`. This cycle keeps the object and the scope in step for a third reason.

## The rule this cycle restates

ADR 0187: **a payload is never written without its tag.** Every builder in `pkg/lang` obeys it; the pair
arm is the same rule with the tag supplied late. An arm that wrote `rt_set_elem` alone would leave the slot
holding whatever the previous tenant of that heap memory left behind, and equality (`rt_slot_eq`) reads the
pair.

## What stays refused, and why that is the honest answer

`{"k": n}`, `{n}`, `d = {"k": n}`, `sum([n])`, `min([n, 3])`, `max([n, 3])` still refuse.

Those roads are not the same road wearing a different hat. A dict interleaves key and value in one element
array and its builder asks `heapElemKind` for both before the tag question is put; a set asks it for each
member; `sum`/`min`/`max` over a literal fold the elements into a **static array** with no tag storage at
all. Teaching one of them means teaching the fold and the lookup that reads it, not adding an arm here — and
an arm added in a hurry is how a dict starts printing an interned index as an integer key at exit 0. Each
shape keeps the existing sentence, which names the value's origin, the missing half and the roadmap row.

## Codegen and IR implications

- `pkg/lang/heapargs.go` gains `pairElemPair` (the element-side question: is this name a pair, and what are
  its two words) and `literalHasPairElement`.
- `pkg/lang/codegen.go`: the three container-literal gates in `value()` and the list-assignment road's
  marking condition take the new disjunct; `heapListFrom`, `heapListFromTagged` and the assignment road's
  element loop take the pair arm.
- Emitted IR per pair element is exactly two calls — `rt_set_elem` with the payload register, `rt_tag_elem`
  with the tag register — plus one `rt_mark_estr` with bit 8 set on the object.
- No new runtime function, no new tag, no new object kind, no change to any signature. `llc-20` verifies
  every module the tests build; exit 2 is asserted absent.

## Alternatives rejected

- **`rt_lift_num` on the element.** It unboxes a float tag and `sitofp`s everything else, so a text element
  becomes the interned index of `"a"` and a container element becomes its handle — as a number, at exit 0.
  ADR 0305 already refused this route for the double domain; a container element is the same trap wearing an
  element's clothes.
- **Give the pair a wider (two-word) value representation everywhere.** This is what `L11.1` ultimately
  wants, but it re-types every container slot, every static array and every builtin at once. This cycle is
  the cheapest honest increment: the two words the pair already owns, written to the two slots the builder
  already has.
- **Store the payload and set the tag from `elemTagFor`'s constant.** Rejected on the spot: the constant is
  what the builder *guesses* from the literal's spelling, and a pair element has no spelling. It would print
  a float slot's box handle as `[140737488355328]`.
- **Refuse `y = [n]` but allow `print([n])`.** Rejected: they are one element question asked by two roads,
  and an element that works only unbound makes `len`, `in` and `for` over the same literal inexplicable.

## Evidence

- `pkg/lang/pair_container_test.go` — 14 answers against the record (int, text, float, `None`, `bool`, a
  nested container, two pairs, arithmetic over a slot, the loop variable, a dict slot by key) and 13
  binding/read-back answers (`len`, subscript, arithmetic on the read-back slot, `in`, `append`, `for`,
  `str`, `repr`, `==`), plus the 8 still-refused positions each asserted to name `roadmap L11.1`.
- `integration/pair_container_test.go` — the same programs through the CLI against **CPython** (the answer
  for these shapes is not in any record, so the reference is the only witness), and the refusal family
  pinned for wording, exit class 1, and exit 2 forbidden.
- Paid rows moved out of refusal tables rather than deleted: `pkg/lang/pair_binding_test.go` and
  `integration/pair_binding_test.go` lost their `[n]` rows to this cycle's answer tables, and
  `float_state_test.go` (unit + integration) turned its `[x, 1]` refusal row into an answer row that still
  rules out the payload answering for the box.
