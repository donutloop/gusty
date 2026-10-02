# ADR 0245: a comprehension's loop variable carries its element's tag

Status: accepted. Roadmap Gap R.76 (answer paid, not refused), Gap R.77 (a dict walked at the wrong
stride) and Gap R.78 (a dict comprehension wrote an interned key as an integer), all measured and closed
in this cycle. Related: L11.1/L11.2 (the tagged value word and its
loop variable), ADR 0185 (`for` binds a mixed element as a `(payload, tag)` pair), ADR 0188 (a dict
entry is two words; iterating a dict yields keys), ADR 0232 (a container stops claiming one element
kind), ADR 0241 (the tagged element read), ADR 0244 (a comprehension's element is written payload-and-tag
together — this ADR is the read side of the same rule).

## The measured starting point

ADR 0244 fixed what a comprehension *writes*. This is what it reads. Every row below is CPython's answer
against two measured binaries: `fa7c7b9` (ADR 0244's element rule, before any door on a mixed iterable)
and `c8b1373` (that door, list-set-and-dict wide). The interpreter agreed with CPython throughout, so
parity between this compiler's two backends had nothing to say about any of them:

| program | CPython | `fa7c7b9` | `c8b1373` |
|---|---|---|---|
| `sa = {1, "a", None}` → `print([x for x in sa])` | `[1, 'a', None]` | `[1, 0, 0]` | refused |
| `sa = set(); sa.add(1); sa.add("a")` → `print([x for x in sa])` | `[1, 'a']` | `[1, 0]` | refused |
| `d = {}; d["a"] = 1; d[2] = "b"` → `print([k for k in d])` | `['a', 2]` | `[0, 1]` — both keys as interned indices | refused |
| `d = {1: "x", "k": 2}` → `print([v for v in d])` | `[1, 'k']` | `['k', 'x']` — values, in the wrong order | refused |
| `xs` grown to `[1, 1.5, "a", None]` → `print([x for x in xs])` | `[1, 1.5, 'a', None]` | refused (the list-only door) | refused |
| `d = {}; d[1] = "x"; d[2] = "y"` → `print([k for k in d])` | `[1, 2]` | `[1, 0]` — a key and a value | `[1, 0]` — **still answered** |
| `d` (text keys) → `print({k: 1 for k in d})` | `{'a': 1}` | `{0: 1}` | `{0: 1}` — **still answered** |
| `d` (text keys) → `out = {k: 1 for k in d}; print(out["a"])` | `1` | `KeyError: key not found` | `KeyError: key not found` |
| `d` (mixed) → `print({k: 1 for k in d})` | `{'a': 1, 2: 1}` | `KeyError: key not found` | refused |

The last five rows are the point. The mixed-iterable door added between the two binaries made the mixed
rows *honest*, and did nothing at all for a dict whose keys are all of one kind — `[1, 0]` and `{0: 1}`
walked straight out of both binaries, because nothing there looked mixed to the guard.

Two independent causes, both in `runtimeCompLoop`, plus a third in the same function's dict entry.

**The loop variable was a plain load.** `for` over a container whose slots mix kinds binds its variable
as a pair — `%_x` for the payload and `%_x_tag` for the tag (ADR 0185) — because an i32 that reads `1` is
ambiguous: it is the integer 1, or the interned index of some text, and only the tag says which. The
comprehension loop never caught up, so it read the payload alone and every printer downstream guessed.
The door that kept this from being an answer consulted `mixedLists` and nothing else, which is why a
mixed *list* refused while a set and a dict answered `[1, 0, 0]`.

**A dict was walked at stride 1.** A dict entry occupies two words and iterating a dict yields its
**keys** (ADR 0188), so `for v in d:` scales the counter by 2 and asks `rt_dict_len`. The comprehension
loop did neither: it read slots 0 and 1 of a two-entry dict, which is one key and one value, and called
that the two keys. That is why `{1: "x", "k": 2}` printed `['k', 'x']` — it was reading slots 0..3 with
the wrong map entirely — and why the plain int-keyed `{1: "x", 2: "y"}` printed `[1, 0]` with no mixed
kind anywhere in the program to blame.

**A dict comprehension wrote an interned index as if it were an integer.** Iterating a text-keyed dict
binds its loop variable to an index into `@str_tab`; the entry write took the payload and asked
`elemKindTag` for a tag, which answered *int*. So `{k: 1 for k in d}` stored the key as the integer `0`,
printed `{0: 1}`, and `out["a"]` — which looks the text up by index *and* tag — died with `KeyError: key
not found` while the interpreter and CPython both answered `1`.

## Decision

1. **The comprehension's loop variable binds the pair, exactly as `for` does.** In the loop body, after
   `rt_get_elem`, also `rt_tag_of` into `%_<loopvar>_tag` and record the variable as tagged. The read side
   is the existing door: `elemPayloadAndTag` asks for the pair (a variable bound this way can only be read
   as a pair), and `taggedLoopVarRead` emits the two loads. Where the element is an ordinary expression
   the static kind still answers, unchanged.
2. **Iterating a dict means keys at stride 2, measured in entries.** The position is the counter doubled
   and the length is `rt_dict_len` — the same two facts `for v in d:` applies, taken from the same registries
   (`runtimeDicts`/`mixedDicts`) rather than from a second opinion. And the loop variable of a text dict (or
   text list, or text set) is registered as holding interned text, which `runtimeCompLoop` already did for
   lists and sets and had simply never asked about `dictKeyStr`.
3. **A dict comprehension writes its entry as two pairs, each with the tag its part really has.**
   `{k: 1 for k in d}` over a mixed dict was refused because the key had no static kind;
   `rt_dict_put_tagged` already takes `(key, keyTag, value, valueTag)`, so the key pair now comes from the
   loop variable's two allocas. Over a *text* dict the key is an interned index and is tagged `TagStr`, so
   the entry is findable by the text that wrote it. A non-dynamic, non-interned key or value keeps the
   static path and its message verbatim, so nothing that refused before refuses differently.
4. **The container a comprehension fills lets its slots speak.** When the element is a tagged loop
   variable, the compiler genuinely does not know what the list holds, so the object is marked
   self-describing (`rt_mark_estr(h, 8)`) and the *binding* records it as a mixed list with float printing
   available. ADR 0244's lesson applies: mark the object **and** the variable, because a table that only
   prints the container passes on half the fix.
5. **What is still out of reach refuses, and says what is missing.** `[x for x in xs]` where `xs` is a
   name the escape analysis kept as a compile-time list has no heap object to walk (refusal, ADR 0192);
   comparing a slot of a run-time-built mixed list with text (`out[1] == "a"`) refuses until the
   comparison lowering can build an operand from a carried tag (Gap R.79); `[x + 0 for x in xs]` over
   `[1, "a"]` raises CPython's `TypeError` in the interpreter and is a named compile-time refusal in the
   compiled backend — an asymmetry the trap table pins rather than smooths.

## Agentic rationale

A mixed comprehension is exactly the program an agent writes when it wants a filtered copy of a
dynamic collection, and `[1, 0, 0]` is the worst possible machine-consumable answer: it parses, it looks
like a list of counts, and nothing in the run distinguishes it from success. The refusal the previous
cycle shipped was honest but it refused *correct* programs too (`[k for k in d]` over a text-keyed dict).
Now the shape answers, and where it cannot the diagnostic names the container, the tag and the missing
piece — all still exit 1 with the same `--json` diagnostic shape, never exit 2.

## Codegen / IR implications

```text
  %iv  = call i32 @rt_get_elem(i32 %src, i32 %pos)     ; pos = idx, or idx*2 for a dict
  store i32 %iv, i32* %_x
  %lt  = call i32 @rt_tag_of(i32 %src, i32 %pos)       ; new: the element's own tag
  store i32 %lt, i32* %_x_tag                          ; new: bound per iteration, like `for`
  ; element append, unchanged door:
  %p   = load i32, i32* %_x
  %t   = load i32, i32* %_x_tag
  call void @rt_append_tagged(i32 %h, i32 %p, i32 %t)
  call void @rt_mark_estr(i32 %h, i32 8)               ; slots describe themselves
```

No new runtime helper: `rt_tag_of`, `rt_dict_len` and `rt_dict_put_tagged` already existed for `for`,
`in` and dict literals. The tag alloca lives in the loop body (it names *this* iteration), guarded by
`g.allocd[lv+"_tag"]` so two comprehensions sharing a variable name share one slot.

## Alternatives rejected

- **Keep refusing every mixed iterable.** That is literally what the previous commit did, and it is a
  regression in coverage, not just in politeness: `[k for k in d]` over a text-keyed dict is common and
  was refused. A refusal is for what the compiler cannot do, not for what it has not been asked to do yet.
- **Print the element by consulting the container at print time.** The printer would need the index it
  came from, which the loop variable no longer has. The pair has to exist at the binding, which is where
  `for` puts it.
- **Fix the dict stride in the read helpers instead of the loop.** The stride is a property of the
  iteration, not of `rt_get_elem`; making the helper stride would break the slot reads that legitimately
  address value words.
- **Make every container a runtime object now** (L11.1's endgame). Right direction, wrong size: it would
  change escape analysis, the folded-literal fast path and every golden module in the tree, and this row
  needs neither.

## Consequences

- `pkg/lang/comprehension_brace_element_test.go` —
  `TestComprehensionOverAMixedContainerTagsItsLoopVariable` (13 programs, both engines) replaces the
  refusal table the previous cycle shipped; `TestComprehensionOverAOneKindContainerTagsItsElement`
  stays as the non-mixed control.
- `integration/comprehension_brace_element_test.go` —
  `TestComprehensionOverAMixedContainerMatchesCPython` (13 rows × both engines, CPython-checked
  expectations, exit-2 guard), `TestComprehensionOverAMixedContainerStillRefusesHonestly` (the three
  shapes above), and the trap row for `[x + 0 for x in xs]`.
- `docs/language.md`: the comprehension section says the loop variable carries its element's tag, and
  names the remaining refusals.
- Roadmap: Gap R.76 re-titled to the answer rather than the refusal; Gap R.77 measured and closed here
  (dict entries walked at stride 1); Gap R.78 measured and closed here (a dict comprehension wrote an
  interned key as an integer); Gap R.79 opened for `out[1] == "a"`; L11.1's queue row loses the
  "loop variable over a mixed list" clause and keeps the two that need a dynamic tag elsewhere.
