# A container word means its payload and its tag

Status: accepted. Implements roadmap **L11.1 (1b)** — mixed dicts and mixed sets in the compiled
path — and closes the lookup-soundness hole that ADR 0189 documented and left open. Closes part of
Gap J.6. Cites: ADR 0184 (mixed lists: a tag per slot), ADR 0185 (a loop variable carries its tag
beside it), ADR 0187 (an element *read* carries its tag), ADR 0189 (every builder writes the tag —
the promise this ADR finally collects on), ADR 0166 (a refusal, never an invalid module), ADR 0226
(no float in a container until there is a word for it), ADR 0231 (read the artifact, not the
bookkeeping — applied here to interpreter/AOT/CPython triplets rather than to DWARF).

## What had been

Two separate things were wrong.

**The compiled path refused the family.** `{"a": 1, "b": "x"}` and `{1, "a"}` were hard errors —
"a compiled dict holds either strings or numbers, not both … (roadmap Gap J.6)" — while the
interpreter ran them, and CPython prints them in one line. The refusal was honest at the time (the
emitter had one kind per container and would have printed an index where a word belonged) but it had
become a wall around a feature whose design had already been worked out for lists.

**And the containers it did accept answered questions wrongly.** A container word was an `i32`, and
lookups compared `i32`s. Strings are `@str_tab` indices, so an interned string and an integer with
the same number are the same bits:

```gy
d = {1: "one"}
print(d["a"])      # compiled: one     — interpreter and CPython: KeyError
s = {"a"}
print(1 in s)      # compiled: 1       — interpreter and CPython: False
```

`{1: "one"}` is a *uniform* dict — all keys integers — so "extend tagging to mixed containers"
would not have touched it. The bug is not about mixing. It is about a comparison that ignores half
of what a slot means. ADR 0189 had already made every builder write a tag; this ADR makes the
runtime actually read them, which is the only reason to have written them.

## The decision

1. **A slot is the pair, everywhere, always.** `@heap` holds the payloads, `@heap_tags` the kinds,
   indexed by the same slot number. Any emitter that puts a word into a container slot writes its
   tag in the same breath — including `d[k] = v`, where the tag now comes from `elemKindTag` and,
   when that cannot describe the operand, from the one thing the site always knows: which word it
   just interned, an `@str_tab` index or a number. There is no path that leaves a tag to whatever
   the slot held before. That is what makes rule 5 legal.
2. **A container says whether its slots describe themselves.** `@estr` gains bit 8. Uniform
   containers do not set it and keep exactly what they had: the plain `rt_dict_put` / `rt_set_add`
   builders, the static printers, the untagged scans. Mixed containers set it, print through
   `rt_dict_print_mixed` / `rt_set_print_mixed` (dispatched from the same `rt_dict_print` /
   `rt_set_print` entry points, so call sites did not change), and are looked up by pair. What
   already worked pays nothing.
3. **The tagged runtime calls are the whole feature's surface.** `rt_dict_put_tagged`,
   `rt_dict_find` + `rt_dict_get_tagged` + `rt_dict_value_tag` + `rt_dict_has_tagged`,
   `rt_set_add_tagged`, `rt_set_discard_tagged` (which shifts tags with payloads — a discard that
   shifted only payloads relabelled every surviving member), `rt_set_contains_tagged`, and
   `rt_contains_tagged` for a list. Each takes the needle's tag as an argument, and `rt_slot_matches`
   is the one place that compares them.
4. **A read that can only mean one thing needs both halves.** `print(d[k])` and `v = d[k]` fetch the
   value *and* the value slot's tag, because `2` and `"x"` are the same word; `for x in s` / `for k
   in d` bind `%_x` and `%_x_tag`, the same companion slot ADR 0185 introduced for list loops.
   `rt_mixed_eq` is what `if x == "a":` becomes — tag first, payload second.
5. **Lookups compare tags whenever the needle's kind is provable — uniform container or not.** This
   is the rule that fixes `{1: "one"}["a"]`. `elemKindTag` proves a literal's, a string expression's
   or a tagged variable's kind; when it cannot, the old payload scan runs and the answer is what it
   always was. Bit 8 is *not* the gate here, precisely because the wrong answer was happening in
   containers that never mix.
6. **A container that grows a second kind is promoted, not refused and not relabelled.** Three
   shapes had been the refusal ("a compiled list holds either strings or numbers, not both"), and a
   fourth had an answer nobody could defend:

   ```gy
   xs = [1]; xs.append("a")      # refused before, prints [1, 'a']
   s = {1}; s.add("a")           # refused before, prints {1, 'a'}
   d = {"a": 1}; d["b"] = "x"   # printed {'a': 'x', 'b': 'x'} — 1 rendered as the string at index 1
   ```

   That last one is the interesting case: item assignment *overwrites a slot*, and the emitter read
   it as a statement about every slot, replacing the container's recorded kind. Now a contradiction
   promotes the container (bit 8 on the object, the static kind claims cleared), which is sound
   precisely because rule 1 holds — every word already in those slots came with its tag. Same for
   `xs[0] = "s"` on a list of numbers: the untouched elements keep their own kinds, where they used
   to be relabelled (`[1, 2]` with `xs[0] = "s"` printed `['s', 'b']`, the untouched 2 rendered as
   whatever its index names). A promotion that cannot happen — the incoming value is one no tag can
   describe — is a refusal, never a silent overwrite.
7. **A value whose kind the compiler cannot prove is not a slot label.** `print` asks what a value is
   *when it prints*; a container slot is labelled *once, when it is built*. A function returning text
   on one path and a number on another is exactly the difference between those two questions, and the
   answer was `(null)` — the integer 7 looked up in the string table:

   ```gy
   def pick(c):
       if c:
           return "z"
       return 7
   xs = [pick(1), "a", pick(0)]   # interpreter and CPython: ['z', 'a', 7]
   ```

   Such an element now refuses, with the reason and a roadmap pointer, rather than being labelled by
   coin flip. The stricter question — *only* strings, not *some* strings — lives in one place
   (`callReturnsOnlyStr`), so `print(f())` keeps the permissive answer, which is the right one there.
8. **What still refuses, refuses with the reason and a pointer.** A float in any slot: no `i32`
   holds it (L11.6, ADR 0226). A container inside a container: the escape analysis cannot mark an
   element, so its tag would be a guess. A needle whose kind cannot be proven *against a container
   that mixes*: `1 in s` where `s` prints as `{1, 'a'}` and the needle is a call whose kind nobody
   knows. Each names the tagged value word (L11.1) rather than "unsupported".
9. **A bool is tagged `TagInt`, because that is what both backends store.** The interpreter keeps a
   bool as `Int(1)` and renders it through the number path — the difference
   `TestBoolValueMatchesCPythonPinnedDifference` pins — so a compiled `[1, True]` printing `[1, 1]`
   agrees with the interpreter, which is the parity bar this cycle is held to. When L11.2 gives bool
   its own kind, one line of `elemKindTag` changes and every container follows; no container code
   needs to be revisited.
10. **Machine path.** No new CLI surface: the interface for this feature is that the *answers* are
   now comparable. The integration tests assert each program three ways — interpreter, compiled,
   CPython (`GUSTY_PYTHON`, the pinned oracle) — and the refusal rows assert exit 1 with the reason
   and a roadmap pointer, never exit 2 (the compiler rejecting its own module) and never exit 0 with
   an answer a tag could not justify.

## What the oracle caught that the pair did not

`{1, "a", None}` prints as `{1, 'a', None}` in both backends and as `{'a', 1, None}` in CPython:
gusty prints sets in **insertion order**, which is what makes two runs comparable at all
(docs/language.md, "Sets iterate in insertion order in both backends"). That row therefore tests
engine-vs-engine, and the oracle decides the *answers* — `len`, `in`, `discard` — not the rendering.
Recording which rows the oracle is allowed to judge, and which it is not, is part of the test file.

## Alternatives rejected

* **Tag only the mixed containers.** Rejected on the evidence above: the wrong answer happened to a
  uniform dict. Tagging is not a feature of mixing, it is a property of a slot.
* **Kind-specific dicts and sets** (a string-keyed table, an int-keyed table, a "dynamic" one for
  mixed programs, as the interpreter has). Rejected: it doubles the runtime and still needs a tag to
  decide which table a *lookup* should use, and it makes the choice depend on the literal rather than
  on the object — the same object drifting between tables as `d[k] = v` runs is the bug class this
  project keeps re-meeting.
* **Hash the containers.** Rejected for this cycle and deferred honestly: hashing buys nothing here
  (a scan of a ≤256-slot array is what `rt_dict_find` already is), and the interesting part of a hash
  is a *tag-aware* `hash` + `eq` pair, which is a design decision of its own, not a lookup detail.
* **Pack the tag into the payload word** (a NaN-box or a spare bit). That is the real fix — L11.1 (5),
  the tagged value word — and it deletes `@heap_tags`, the tag shifts, and the parallel-array
  invariant in one move. Doing tag-in-a-second-array first is the step that makes containers answer
  correctly today; pretending the layout change is required first would have kept the wrong answers.
* **Refuse mixed dicts and sets, and refuse a container that grows a second kind.** Rejected: the
  interpreter, CPython and every program in the roadmap's Phase-11 queue need them, and ADR
  0184/0185/0187 had already paid the design cost once for lists. The growth refusal had been
  justified by "the new slot's tag cannot be known" — rule 1 says it always could have been, and the
  print that motivated the rule (`[(null), 'a']`) was the bug the refusal was covering for.
* **Print a mixed container through the container's recorded kind.** That is what made the old code
  wrong rather than merely incomplete, and it is what bit 8 exists to prevent.

## Consequences, and the honest limits

Membership and lookup are linear scans that now read two arrays instead of one; a value *and* its
tag from a mixed dict costs two scans (`rt_dict_get_tagged`, `rt_dict_value_tag`), which is the
price of a parallel tag table and is listed as a reason to do L11.1 (5). Sets still print in
insertion order, which CPython does not; dicts yield keys in insertion order, which it does.
Untagged scans remain wherever the needle's kind cannot be proven — reachable, but only through a
needle the compiler cannot classify against a container that does not mix, and those are the same
programs that will be fixed by the tagged word, not by another flag. `discard` on a mixed set is the
one operation that has to move tags down with payloads; the tests pin that a surviving member keeps
its own kind rather than its predecessor's. Bool membership (`True in {1}`) answers the way both
backends store bools today, and the pinned test says so out loud until L11.2. Promotion is tracked
per *variable*, because that is how the escape analysis knows a container: an alias of the same
handle keeps whatever claim the compiler made for the name used to reach it — the same aliasing debt
ADR 0181 records, and one more reason the mixed paths, not the static ones, are where this feature
converges.
