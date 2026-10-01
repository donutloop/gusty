# A float in a container slot is the handle of a box, and the collector has to know

Status: accepted. Implements roadmap **L11.1 step 1** — floats in container elements, the named next
piece of the tagged value word — and pays the two oracle debt rows that pinned it. Closes the
compiled half of Gap R.40's remaining family, closes the float half of the refusals ADR 0226 left
standing, and fixes a use-after-free the float boxes exposed. Cites: ADR 0184/0185/0187 (a tag per
slot, a loop variable that carries its tag, an element read that carries it), ADR 0189 (every builder
writes the tag with the payload), ADR 0232 (a container word means its payload *and* its tag), ADR
0166 (a refusal, never an invalid module), ADR 0188 (the collector owns a container's elements), ADR
0181 (precise roots), ADR 0221/0226 (the float-in-a-container refusals this supersedes).

## What had been

The compiled path refused every container holding a float, and the refusals were covering a set of
wrong answers rather than preventing them:

```gy
xs = [1.5]                # compiled: refused; interpreter and CPython: [1.5]
d = {"a": 1.5}            # compiled: refused; interpreter and CPython: {'a': 1.5}
print([1] == [1.0])       # compiled: refused (before ADR 0226: 1 by truncation); CPython: True
print([None])             # compiled: [0]; interpreter and CPython: [None]
print(1.0 in [1])         # compiled: 0; CPython: True
xs = [1]; xs.append(1.5)  # compiled: [1, 1]; CPython: [1, 1.5]
```

The gates were honest — an element slot is one `i32` and a double does not fit in one, so the
choices were a truncated read or an invented operand — but "not yet" had become the permanent answer
for a construct the language prints every day. `None` was worse than refused: it compiled to a
payload of 0 with nobody to say so, which is the exact class of defect the roadmap's
diagnostic-quality contract forbids.

## The decision

1. **A float slot holds the handle of a float box.** `HeapKindFloat` is a new `rt_alloc` kind; the
   double lives in `@float_box`, a table indexed by handle, parallel to `@heap` rather than inside it,
   because the heap's element words are `i32` and a `double` store into them would be a misaligned
   write the allocator cannot promise against. The slot's payload is the handle and its tag is
   `TagFloat`, written together as one operation like every other slot write. The box is reclaimed
   with the container that holds it, which is why a float element costs the same lifecycle as a nested
   object and none of the machinery of one.

   It is deliberately **not** interned. The first implementation interned boxes by value, the way
   `rt_str_intern` interns text, and that was wrong twice over: two boxes for `-0.0` and `0.0` are
   two payloads for one value (so `[-0.0] == [0.0]` became False against Python's True), and an
   interned box is a value that can never be freed, with a capacity cliff at the end of the table.
   Equal-but-separate boxes plus a comparison that reads the doubles is both smaller and correct.

2. **One comparison answers every slot question.** `rt_payload_eq(payload, tag, payload, tag)` is the
   single place that decides whether two slots denote the same value: within a tag it is payload
   equality (which is what keeps a stored string and the number 1 apart), across the two numeric tags
   it is numeric equality with `fcmp oeq` — so `1 == 1.0` inside a container is True, `-0.0` equals
   `0.0`, and NaN is unequal to itself the way Python's own float comparison says.
   `rt_slot_eq` (container equality) and `rt_slot_matches` (dict lookup, set dedup, membership) both
   call it, so they cannot disagree about which entries are the same — the soundness rule ADR 0232
   established, extended to the number pair.

3. **One printer renders elements, and it now knows floats.** `rt_print_mixed_value` gained the float
   arm the roadmap named as the missing piece; it goes through `rt_fmt_double`, the same helper that
   prints a bare float, because repr and str of a float are the same text and a second formatter is a
   second place to be wrong. The float runtime block's inclusion gate now counts the heap block as a
   reference to it: a gate that reads only the emitted *body* omits the formatter for a program whose
   reference comes from the runtime's own globals, which is the referenced-but-undefined signature
   this file keeps having to write down.

4. **A literal containing a float or None is built tagged even when it is homogeneous.** Two questions
   were one predicate: "does this literal need the heap" and "does it mix kinds". Folding them diverted
   `sum([1.5, 2.5])` from the static float array that answers it correctly to a heap list that did not,
   and it made `print([None])` print `[0]`. `literalNeedsTags` is now the second question, answered by
   "can any slot's payload be read back without its tag?" — a box handle and nothing cannot.

5. **Growing a container out of its compiled kind promotes it, not refuses it.** `xs.append(1.5)` on
   an integer list, `s.add(None)`, `d["b"] = 1.5`: the slot carries its own tag, so the container stops
   claiming a kind (`promoteMixed`, ADR 0232's move) and the runtime printer reads tags. Before, the
   list printed through the number printer and showed `1` where CPython shows `1.5`.

6. **The collector walks a container's element words by kind.** A dict's `len` counts *entries* and an
   entry occupies two slots, key then value; the mark phase walked `len` words and so covered only the
   first entry. Every later entry's key and value went unmarked, were swept while the dict still
   referenced them, and the recycled slot handed back somebody else's bits:

   ```gy
   d = {1.5: "x", 2.5: "y"}
   print(1.5 in d)   # allocates a temporary box
   print(d)          # was: {1.5: 'x', 1.5: 'y'}
   ```

   This was latent for interned strings (a recycled index still names *some* string) and became visible
   the moment a slot could hold a float box. The walk is now `len * (kind == dict ? 2 : 1)`.

7. **A container is not a float.** `isFloat` used to answer *yes* for a list literal whose elements
   were floats, which was unreachable while such a literal was refused; once the element could be
   stored, `xs == [1.5, "a"]` compiled to `sitofp i32 <handle> to double` — comparing boxes instead of
   contents. A container answers "no" now, and the numeric folds (`sum`, `min`, `max`, `abs`) ask their
   own question about their argument's *elements*, out loud, instead of borrowing a predicate that
   claims a list is a number.

8. **An unrolled loop carries its element's tag.** `for v in [1.5, "a", None]` emits one body copy per
   element; the body learned to print the two tags nothing else can render (float, None) from the tag
   the element was built with. Integers and interned text keep the paths they had, so a loop over plain
   numbers still does not pull the string runtime into the module.

## Agentic rationale

No flag, schema or exit code changes: the feature removes refusals rather than adding an interface.
What an agent can still see is the artifact — `--emit-llvm 'xs = [1.5]'` contains `call i32
@rt_float_new(double …)` and a `rt_tag_elem(…, i32 1)` for the slot, and `integration/programs/
float_container_elements.gy` is the three-engine source that the matrix runs. The refusals that
remain (a nested container, a bool) name the missing mechanism and the roadmap item, and are pinned
by name rather than by prose: `TestMixedContainersStillRefuseWhatNoTagDescribes`,
`TestInterpreterAnswersWhatTheCompilerRefuses`.

## Codegen and IR implications

- New runtime globals and helpers: `@float_box`, `rt_float_new`, `rt_float_of`, `rt_payload_eq`.
- `rt_slot_eq` and `rt_slot_matches` are now delegations to `rt_payload_eq`; the pair comparison the
  container-equality test pinned ("`icmp eq i32`", "`and i1`") moved with it, and the test asks both
  halves: `rt_slot_eq` reads `@heap_tags` and calls `rt_payload_eq`, and `rt_payload_eq` contains the
  pair compare and the `fcmp oeq` arms.
- `rt_gc`'s mark walk reads the object's kind (the same `HeapKindList/Dict/Set` numbers codegen emits).
- Every promotion is `rt_mark_estr(h, 8)` — bit 8 continues to mean "read the tags".

## Alternatives rejected

- **Interned float table**, mirroring `@str_tab`. Rejected above: `-0.0`/`0.0` dedup is wrong, boxes
  are never freed, and the table has a capacity cliff whose failure mode is a wrong number.
- **64-bit slots** (or a slot that is a `{i32 tag, i32 payload}` pair widened to 64 bits). Touches
  every container layout, the GC walk, the static-global literal path and the tag array, to solve the
  one element kind that needs it; the boxed representation costs one allocation and reuses the
  collector.
- **`llvm.nan`/NaN-boxing into the payload.** There is no 32-bit encoding of an arbitrary double; this
  was tried in the scratch branch and lost bits, which is how `[1.5]` came to print `[1]`.
- **Keep refusing.** Rejected by the measured table: `[None]` and `print([None])` printed `[0]` while
  *refusing* floats, so the refusal was not keeping the answers honest, only the honest ones rare.
- **Nested containers in the same commit.** They need the same printer arms and the same GC rule, but
  their own gate, and shipping them together would hide which of the two the tests are actually
  judging. They are the next row of the queue.
