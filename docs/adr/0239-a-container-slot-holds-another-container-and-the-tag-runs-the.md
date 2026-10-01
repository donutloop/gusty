# A container slot holds another container, and the tag — not the builder — runs the print and the comparison

Status: accepted. Implements roadmap **L11.1 step 2** — nested containers, the named next piece of
the tagged value word after ADR 0238's float — and closes the compiled half of the nested shapes the
surface survey kept meeting. Cites: ADR 0184/0185/0187 (a tag per slot, a loop variable that carries
its tag, an element read that carries it), ADR 0189 (container equality is content, and every builder
writes the tag with the payload), ADR 0188 (the collector owns a container's elements), ADR 0232 (bit
8 means "the slots are the truth"), ADR 0238 (a float slot is the handle of a box), ADR 0166 (a
refusal, never an invalid module), ADR 0226 (ask what fits in the word before writing it).

## What had been

An element that was itself a container was refused by the compiled backend, and the refusal was
recorded as a collector problem: "an element that is a handle is not marked by the collector". That
reason had gone stale — `rt_gc` already marks every element word of every marked object and re-walks
newly marked objects to a fixed point, so a nested object was reachable all along. Behind the stale
gate sat two genuine holes and one wrong answer:

```gy
print([[1, 2], [3, 4]])            # compiled: refused; interpreter and CPython: [[1, 2], [3, 4]]
print([[1, "a"], [2, "b"]])        # built anyway: [['b', 'a'], [(null), 'b']]
for row in [[1, 2], [3, 4]]:       # compiled: 0, 1 — the handles, printed as numbers
    print(row)
print([[1, 2], [3, 4]][0])         # compiled: exit 2 — printf("%d", i32 @.lst1)
print({[1, 2]: 3})                 # compiled: {1: 3} — the key printed as a number
```

The `[[1, "a"]]` case is the one that matters most: it compiled, verified, exited 0, and printed
text that came out of the interned string table at the wrong indexes. A refusal had been protecting
against that, and the refusal was justified by a reason that was no longer true.

## The decision

1. **A nested slot holds the inner object's handle, tagged `TagList`/`TagDict`/`TagSet`.** Nothing
   new is stored — the slot is the same `i32` ADR 0226 asks about — and the collector already marks
   it. What changed is who is allowed to *ask* the question: `elemKindTag` admits a container element
   when every element inside it is taggable too (`taggableNestedElem`, recursive), so one untaggable
   value deep cannot hide behind correct-looking neighbours.

2. **The printer is chosen by the object, not by the builder.** `rt_print_mixed_value` grew the three
   container arms, all calling one new helper, `rt_print_container_value(h, quote)`, which reads the
   inner object's `@heap` kind and delegates to `rt_print_list` / `rt_dict_print` / `rt_set_print` —
   each of which already asks its own `@estr[h]` bits whether its elements are numbers, interned text
   or tagged. `rt_print_list` was missing that self-dispatch (only the dict and set printers had it)
   and gained it, so "read the tags" (bit 8) means the same thing on all three. A static choice is
   exactly what produced `[[1, "a"]]` → `[['b', 'a']]`: the outer builder had picked one element kind
   for a container that has none.

3. **Equality and membership for container slots go through the same one rule as everything else.**
   `rt_payload_eq` answers container-tagged pairs by calling `rt_container_eq`, which compares slots
   with `rt_slot_eq`, which calls back into `rt_payload_eq`: content equality to any depth, and
   `[[1, 2]] == [[1, 2]]` is True for two literals that build two objects. Because
   `rt_slot_matches` already delegates to `rt_payload_eq`, `in`, dict lookup and set dedup agree with
   `==` for free — the soundness rule of ADR 0232, extended one level down. A needle that is itself a
   container is materialised by `containerOperand` and matched with the *tagged* runtime helpers;
   the untagged `rt_contains(i32, i32)` would have compared one object's address with another's.

4. **A container element forces the tagged path.** `literalNeedsTags` counts a container element
   alongside a float box and None's nothing, because a handle read back without its tag is a number —
   which is how `for row in [[1, 2], [3, 4]]` printed `0` and `1`, and how `[[1, 2]]` would print
   `[[5]]`. The list builder now sets bit 8 whenever it writes tags, matching the dict and set
   builders, so the tag-aware printer is reachable through `rt_print_list` too rather than only when
   the codegen happened to select it.

5. **A tag has to travel with a loop body.** The unrolled `for v in [<literals>]` emits one copy of
   the body per element and records each element's tag; the print hook now takes the container tags
   as well as float and None, and integer and string loops keep the IR they had (a loop over plain
   numbers still does not pull the string runtime into the module).

6. **A dict keyed by a container stays refused.** Python rejects it outright (`unhashable type:
   'list'`), and this backend has no hashing rule for a handle; building it anyway printed `{1: 3}`
   for `{[1, 2]: 3}`, which is a wrong answer *about the key*. The refusal names the shape, and
   `heapDictFrom` asks before building so the diagnostic cannot be reached after a half-written
   object.

7. **A fold that cannot name its elements' types refuses rather than reaches for their addresses.**
   `sum`, `min`, `max`, `any` and `all` fold an inline literal by asking each element for a number;
   with nested elements admitted, they began folding `@.lst1` into `add`/`icmp` operands — exit-2
   modules for programs CPython answers with a `TypeError`. Each fold now checks its elements and
   refuses with the element named. `sum` additionally asks each element what it *is*: an element is a
   float box, an integer, or neither, and text and containers are refused the way CPython raises them
   (`unsupported operand type(s) for +: 'int' and 'str'`), which the interpreter now reports too
   instead of adding handles and printing `562949953421319`.

## Agentic rationale

No flag, schema or exit-code change: the feature removes refusals. What an agent can still see is the
artifact — `--emit-llvm 'print([[1, 2]])'` contains a `rt_alloc(i32 1)` for the inner list, a
`rt_tag_elem(…, i32 5)` for its slot, and a call to `rt_print_container_value`; the refusals that
remain name the missing mechanism and the roadmap item that owns it. The shapes are pinned by name,
not by prose: `TestNestedContainersAnswerOnBothBackends` and
`TestNestedShapesThatStillRefuse` (integration, three engines with CPython as the oracle),
`TestNestedContainersPrintTheirOwnContents` (compiled module + interpreter), and
`programs/nested_data.gy` as ledger parity surface.

## Codegen and IR implications

- New runtime helper: `rt_print_container_value(i32 %h, i32 %quote)`. `rt_print_list` gained the
  bit-8 self-dispatch that `rt_dict_print` and `rt_set_print` already had, which changed that
  printer's `loop` phi predecessor from `%entry` to `%notMixed` — a phi names the blocks that
  actually branch to it, and llc calls a wrong one malformed.
- `rt_payload_eq` grew `TagList`/`TagDict`/`TagSet` arms delegating to `rt_container_eq`; because
  `rt_slot_eq` and `rt_slot_matches` call it, container equality, membership, dict lookup and set
  dedup are one rule.
- Gates that changed hands: `elemKindTag` (containers, and a container *variable*, are taggable),
  `taggableMixedList/Dict/Set` (a container element is a must-tag), `literalNeedsHeap` (a container
  element is not an int-array layout), `containerOperand` (a mixed list is built tagged),
  `taggedOperand` (a container needle is materialised, not folded to a global).
- `for <var> in <set/dict literal>` now reaches the runtime container loop with a real handle; see
  Gap R.70 for the bug this replaced.

## Alternatives rejected

- **Store nested containers in a flat, index-encoded layout** (elements as `(kind << 24) | index`).
  Saves nothing — the payload is already one `i32` — and makes every comparison a decoding exercise
  outside the tag table, where `rt_payload_eq` is the one place that decides.
- **Choose the inner printer at build time** from the literal the compiler saw. Rejected above: the
  outer container has no element kind, and the failure mode is text printed from the string table.
- **Compare containers by handle and refuse `==`.** `[1] == [1.0]` and `[[1]] == [[1]]` are ordinary
  programs; ADR 0189 already decided content equality is the answer, so the only question was who
  routes to it.
- **Let dict keys be containers and print whatever comes out.** Rejected by the measured `{1: 3}`.
- **Keep the whole family refused.** Rejected by the same evidence as ADR 0238 rejected it for
  floats: the gate's stated reason (the collector) was already false, and behind it were four wrong
  answers and two exit-2 shapes.
- **Fix the tagged element read in the same commit** (`xs[0]`, `m[0][1]`, `xs[0] + 1`). It needs a
  read that returns a (payload, tag) pair usable in a numeric and an indexing context, which is the
  last clause of L11.1 and keeps its own row; `probe_nested_list` and `probe_heterogeneous` stay in
  the debt ledger pinning exactly that.
