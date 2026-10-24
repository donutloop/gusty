# A dict is sorted by its keys, and the door that copies it is where the stride is decided

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.426** (`sorted` over a dict sorted the entry array's first `len` words
and printed `['a', 1]` for `{"a": 1, "b": 2}` at exit 0). Files two rows it measured beside itself and did not buy:
**Gap R.429** (the three key kinds the new arm refuses rather than orders — keys of two kinds, a fractional key, and a
key the front end holds no kind for) and **Gap R.430** (`sorted` over a SET echoes the set's own insertion order, and
`reverse=` changes nothing because `@rt_sort` is never reached for a set). `for k in sorted(d)` stays the `for` road's
refusal, which already names **Gap R.247**.

**ADR 0416** is left unissued in this commit on purpose: that number belongs to the set-member work in flight beside
this one (**Gap R.422**/**Gap R.428**). One commit, one feature: the dict arm of `sorted` in
`pkg/lang/codegen_container.go`, the `@rt_dict_keys` door in `pkg/lang/codegen_ir_dict.go`, the one inheritance rule in
`pkg/lang/codegen_assign.go`, and the two test files.

## Context

`sorted` has one runtime road. It takes the argument's handle, copies it, sorts the copy with `@rt_sort`, and flips it
with `@rt_reverse` when `reverse=` asks. For a list that is exactly right, and it is the road **ADR 0191** chose so that
`sorted(xs)` leaves `xs` in its own order — a difference a program can see, and the reason the copy exists.

For a dict it was exactly wrong, because a dict's payload array is not a sequence of elements:

```
d = {"a": 1, "b": 2}
print(sorted(d))                  #  ['a', 'b']   reference
                                  #  ['a', 1]     this backend, at exit 0
print(sorted(d, reverse=True))    #  ['b', 'a']   reference
                                  #  [1, 'a']     this backend, at exit 0
for k in d: print(k)              #  a, b         reference — and this backend agrees
```

The array holds `[key, value, key, value, …]`; the length word counts ENTRIES. The copy read `len` words positionally —
two words of a four-word array — and handed the sort one key and one value. Nothing raised, nothing refused, and the
`for` road over the same dict answered correctly in the same program, which is what made this a row rather than a
cosmetic quirk. The collector (`rt_gc`) and the dict printer (`rt_dict_print_mixed`) had each already been taught the
same 2-words-per-entry fact (**ADR 0350**'s stride note, `stride = isDict ? 2 : 1` read from the object's kind word); the
sort loop had simply never been asked.

There is a family lesson in the shape of the bug. Three doors walk a dict's array — the collector, the printer, and this
one — and each had to be told the stride *separately*, because none of them asks the object what it is before reaching
for a word. The row is the third one.

## Decision

**1. `sortedRuntime` asks its argument what it is, before it copies anything.** The first thing the road does now is
consult `sortedDictAnswers`; a dict takes `sortedDict`, and every other container takes the road it already had,
untouched. The question is asked once, at the top, in the one place three callers (`sorted(...)` as an expression,
`print`'s container path, and the `for` loop's materialisation) already funnel through — so an argument cannot be a dict
to one caller and a list to another.

**2. The keys are read by a door, not by a loop in the emitter.** `@rt_dict_keys(i32 %h, i32 %kt)` walks the entry array
at `%w = mul i32 %i, 2`, stops at `@rt_dict_len`, appends with the list's own `@rt_set_elem` + `@rt_tag_elem` pair (the
same idiom `@rt_list_copy` uses), and returns a fresh list. The stride and the bound are therefore one block in the
module that an IR row can read, rather than lines of `fmt.Sprintf` that a later edit can quietly put back to `len` words
at stride one.

**3. The comparator is decided by the record of the keys, not by the source text.** A dict's key kinds are already kept
by the front end — `dictKeyStr`, `dictKeyInt`, `mixedDicts`, the same facts the print road and `in` read a dict by — and
the sort asks those. That is what makes `g = {}` / `g["q"] = 1` / `g["p"] = 2` / `print(sorted(g))` answerable: the
literal the binding left on record is empty by then, and a door that consulted only the literal would answer `[]`. Where
the record cannot name the kinds (keys of two kinds, a fractional key, a name key with no recorded kind) the program is
refused in a sentence naming the missing half and **Gap R.429** — including the mixed-key case where the *reference
raises*, because a compiled answer there would be an ordering invented across kinds (**ADR 0191**'s precedent: the
mixed-kind list sort has always been reported, not guessed).

**4. A text key's tag comes from the record; every other key's comes from the slot.** This is the half the first
version got wrong and the CLI caught. A dict whose keys are all of one kind is stored through the UNTAGGED put
(`rt_dict_put`, not `rt_dict_put_tagged`) and keeps its kind in the compiler's tables, so the arena's tag word for a text
key says only who slept in that slot last — and `xs = sorted(d)` printed `['b', 'a']` as `[1, 0]`, the two indexes into
`@str_tab`. The door therefore takes `%kt`, uses it when the record says TEXT, and otherwise passes through the tag the
slot was written with, so a verdict key keeps printing `True` rather than `1`. One question to the record, one question
to the slot, both answered by the same codegen that chose the comparator: the order a key gets and the way it prints
cannot disagree (**ADR 0232**'s one-table rule, applied to a sort).

**5. The binding inherits the element kinds from the KEYS.** `xs = sorted(...)` asks the assignment road what its
elements are, and that road looked at a list source or a list literal only. It now asks `sortedDictElementFacts`, which
answers "texts" for a dict whose keys are texts and "tag-printed" for keys that are neither plain numbers nor texts. The
first version fixed the sort and left the print wrong, which is the general shape: a sorted dict is a *list*, and every
door that reads a list has to be told what its elements are.

**6. The controls are the point of the test file, not the decoration of it.** `sorted([3, 1, 2])`, `sorted(ys)` over a
text list, and the dict's own questions unsorted are pinned beside the answered family — a fix that read a dict by its
keys while breaking the list's elements would have failed there. `sorted(<set>)` was one of those controls, and it found
**Gap R.430**; the case was moved out of the green family into `TestWhatSortingASetStillOwesIsFiledNotFixed`, which
FAILS if the door starts answering, so the row has to be closed on purpose.

## Consequences

* A dict handed to `sorted` now answers its keys, in the reference's order, over texts, whole numbers and verdicts, at
  both doors (`print(sorted(d))` and `xs = sorted(d)` / `print(xs)`), with `reverse=` by name and by position, including
  the empty dict and a dict built by item stores.
* The module grows one internal door. `@rt_sort`, `@rt_reverse` and `@rt_list_copy` are unchanged, so the list road
  cannot drift with this one; the price is that a set still sorts nothing (**Gap R.430**) and the comparator is still
  mode-based (**Gap R.429**).
* The record grows 32 entries, written by `tools/recmerge` from CPython and cross-checked against the compiled leg — no
  answer here is certified by the backend that produced it (**ADR 0302**). The conformance corpus gains
  `integration/programs/sorted_dict_keys.gy` (matrix row `programs/sorted_dict_keys`, oracle `match`), and the ledger
  moves 235 → 236 rows, 206 → 207 passing, oracle `match` 192 → 193, debt and not-applicable unchanged, drift 0.
* `sorted(<set>)` is the next door of this shape, and the lesson is now stated once, here: any road that walks a
  container's arena has to ask the object what it is, or go through a door that does.

## Alternatives rejected

* **Refuse `sorted(<dict>)`.** The row's own definition of done offered this as the interim shape. It was rejected
  because the answerable half is the whole language-visible surface — the reference answers it, the record answers it,
  and the only thing standing in the way was a stride — and a refusal would have deleted correct behaviour that
  `sorted([3, 1, 2])`'s neighbours already have. Refusal is kept for the three shapes where an answer would be a guess.
* **Read the keys in the emitter, from the literal.** Rejected: it is the same source-text guess that makes
  `g = {}` / `g["q"] = 1` unanswerable, and it puts the stride in `fmt.Sprintf` where no test can see it.
* **Reuse the dict's own array as the sort's input, in place.** Rejected: `sorted` must leave its argument alone
  (**ADR 0191**), and sorting a dict's entry array in place would reorder the dict — which the walk-and-sort twin case
  in the test file exists to catch.
* **Have `@rt_dict_keys` read every key's tag from `@heap_tags`.** Rejected on measurement, not on principle: it prints
  `['b', 'a']` as `[1, 0]` for exactly the single-kind dicts that take the untagged put. Reading the slot is kept, but as
  a fallback behind the record's answer for a text.
* **Give the sort a tagged comparator in this commit.** Rejected as scope, not as direction — it is **Gap R.429**'s
  first concrete action, it changes the comparator every list sort shares, and it belongs with the ordering-door work
  (**ADR 0382**'s family) rather than riding in on a stride fix.
