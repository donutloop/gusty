# Removing a set member asks the argument what it carries — one (payload, tag) pair, asked once, by both verbs

## Status

Accepted — 2026-10-27. Closes **roadmap Gap R.422** (filed by **ADR 0413**, measured beside the alias
battery of **Gap R.419**). Turns one wrong answer at exit 0 and one refusal at exit 1 into the reference's
answers, and leaves nothing owed that is this door's own: what the same probe found and did not fix is
**Gap R.170** (a set prints in insertion order), **Gap R.189** (naming a member the compiler cannot see)
and **Gap R.447** (the `for` road's `i32` store into the `double` slot the float road declared).

```
s = {1, 2}                        s = {1, 2}
s.discard("a")                    s.remove(9)
print(s)                          #  {1, 2}
#  {1}     this backend, at the exit code of SUCCESS
#  {1, 2}  reference               #  this backend REFUSED at exit 1 (Gap R.174's method-over-a-name);
                                   #  the reference says KeyError: 9
```

One commit, one feature: one question — *which `(payload, tag)` pair does this member carry?* — asked of the
ARGUMENT by both verbs, in `pkg/lang/set_member_removal.go` (new), plus three runtime doors in
`pkg/lang/codegen_ir_tagged_slots.go` and a sentence road widened in `pkg/lang/codegen_index.go`.

## Context

`s.add(v)` already asked the argument. **ADR 0187** and **ADR 0189** established the rule that a slot's
payload is never the discriminator — a stored text is an index into `@str_tab`, so the text `"0"` and the
number `0` are the same `i32` — and **ADR 0232**'s tagged-member road carries a second word beside every
member precisely so that `in`, `len`, `print` and `add` can agree about what a slot holds.

`s.discard(v)` asked the SET. The old road called `heapElemKind` for the argument's payload, consulted
`elemKindTag` only to decide whether the *add* should be tagged, and handed the removal door
`rt_set_discard(h, v)` **one word**. That door scanned the payload array and deleted the first member whose
payload happened to equal whatever the argument's payload happened to be:

| program | this backend | reference (CPython 3.12.3) |
|---|---|---|
| `s = {1, 2}` / `s.discard("a")` / `print(s)` | `{1}` — exit 0 | `{1, 2}` |
| `s = {1, 2}` / `s.discard(9)` / `print(s)` | `{1}` — exit 0 | `{1, 2}` |
| `s = {0, 1}` / `s.discard(None)` / `print(s)` | `{1}` — exit 0 | `{0, 1}` |
| `s = {1, 2}` / `s.discard([1])` | `{2}` — exit 0 | `TypeError: unhashable type: 'list'` |
| `s = {1, 2}` / `s.remove(9)` | refused at exit 1 | `KeyError: 9` |
| `s = set()` / `s.add(1)` / `s.discard(1)` / `s.discard(9)` / `print(len(s))` | `-1` — exit 0 | `0` |

The first four are the same lesion seen four ways: an argument the set has never held removed a member it
does hold. `discard(9)` is the sentence that makes this the door's row rather than the notebook's — even a
member the compiler can read is judged by its payload alone. The fifth and sixth are the two shapes the
missing verb left behind: a `remove` that fell past the set-method door to the string-method road (whose
name-holds-container gate refused it), and a `discard` of a non-member that underflowed the length word to
`-1`, because the payload-only door had no answer for "absent" and none of its callers could ask.

## Decision

**One question, asked of the argument, once.** `setMemberRemoval` resolves the member exactly the way `add`
does — `pairElemPair` first (the pair a subscript or a carried call left behind), `taggedOperand` next (the
pair a bound name keeps in its own tag word, a float in its box, a container in its materialised handle) —
and refuses at exit 1 if neither answers, rather than run a removal the payload alone would decide. Then
both verbs go to the same two runtime doors, in the same order:

```
rt_set_find(h, v, t)      →  the position of the pair, or -1        (rt_slot_matches: payload AND tag)
rt_set_remove_at(h, pos)  →  shift the tail of BOTH arrays, shrink the length
rt_set_discard_tagged(h, v, t) →  the two above, with an absent test between them
```

`discard` hands the pair to `rt_set_discard_tagged` — the no-op lives where the finding does, so no caller
can forget it. `remove` asks `rt_set_find` itself, raises `KeyError` naming the member on `-1`, and only
then calls `rt_set_remove_at`. One scan per verb; the finding is not asked twice from two doors that could
answer differently (**ADR 0232**'s one question), and the operand is built once (**ADR 0288**'s one
evaluation — every arm here emits into scratch and commits only after it has answered).

Hashability is asked **before** the set is consulted — `unhashableKeyGate` (**Gap R.217**, **ADR 0328**) —
because a member the language refuses to hash is not a missing member, and a scan that can never match it
would report `KeyError` where the reference raises `TypeError` naming the type. One trap, one wording
(**ADR 0271**): the sentence is the same whether the kind was a constant this pass wrote or a register the
pair road loaded.

The road never calls `recordElemKind`, `promoteMixed` or `rt_mark_estr`. Removing a member is not evidence
about the members that remain; the old road's second half-was was exactly that — it recorded the
argument's kind onto the set, which is why `print` after a discarded text rendered the survivors through
the kind of the value that had never been a member.

### The asymmetry the dict door does not have, measured cell by cell

The obvious implementation copied `guardHashableTag` from the dict-key road. The pinned reference says that
guard is wrong here. Asked one program per cell:

| program | reference |
|---|---|
| `s.discard([1])` | `TypeError: unhashable type: 'list'` |
| `s.discard({"a": 1})` | `TypeError: unhashable type: 'dict'` |
| `s.discard({1})` | **no error at all** — the set prints unchanged |
| `s.discard((1, [2]))` | `TypeError: unhashable type: 'list'` |
| `s.discard((1, {2}))` | `TypeError: unhashable type: 'set'` |
| `s.remove([1])`, `s.remove({"a": 1})` | `TypeError`, as above |
| `s.remove({1})` | `KeyError: {1}` — the reference's word is MISSING, not UNHASHABLE |

So a **bare set** is exempt from the `TypeError` for both verbs, while a set nested inside a tuple still
names itself: that is a property of CPython's two set entry points, which swallow a `TypeError` from
hashing a set argument and report an absent member instead. `memberIsABareSet` is exactly as wide as that
measurement and no wider — a `*SetLit`, the `set()` spelling of one, and a name whose record says it holds
a set. Nothing on this road can put a set *into* a set (the member door refuses an unhashable member), so
the scan cannot find one, which is why `discard` lands on its no-op and `remove` on its `KeyError` without
either having to know the exception.

A **tuple** is the other exemption, forced by the tag rather than by the reference: this backend gives the
two container spellings one layout (**ADR 0361**), so a tuple's slot carries `TagList` — verified in the
emitted module, where `s.discard((1, 2))` passes `i32 5`. A guard that read the tag would answer
`s.discard((1, 2))` with `TypeError: unhashable type: 'list'` where the reference prints the set untouched.
The static door asks the question the language actually has: it walks a tuple's elements left to right and
names the first it cannot hash, the way CPython does (**L11.3**, **Gap R.217**).

### The sentence names the member

`KeyError` carries the member's repr, because that is what a reader greps: `KeyError: 9`, `KeyError: 'z'`,
`KeyError: None`, `KeyError: False`, `KeyError: 1.5`, `KeyError: {1}`, `KeyError: set()`. Two readings were
missing, both in `keyNotFoundMessage` — the same door a dict subscript reads, so the two sentences cannot
disagree:

* a **set literal** and the **constructor spellings** (`set()`, `[]`) have reprs the pass can read, but
  `literalKeyRepr` had no arm for either;
* the **item of an unrolled loop** is a name whose value the pass does know — the unroll already keeps the
  element per iteration in `loopElemTag` (**ADR 0424**) — and now keeps *which* element it is beside it
  (`loopElem`, read by `loopItemExpr`). Without it, `for k in [1, 2, 9]: s.remove(k)` said
  `KeyError: key not found` where the reference says `KeyError: 9`.

## Alternatives rejected

* **Keep the payload-only door for the shapes the pair road cannot answer.** This is what the row shipped
  before, and it is what made it a row: it answered every such program with a removal it decided by
  accident, at exit 0. A refusal the reference does not answer is debt (**ADR 0166**); a wrong answer at
  exit 0 is a lie, and the difference is the whole content of this commit.
* **Copy `guardHashableTag`.** Rejected by the table above — it would invent `TypeError: unhashable type:
  'set'` for `s.discard({1})` and `s.remove({1})`, where the reference raises nothing and `KeyError`
  respectively. A wrong trap at exit 3 is the same class of defect as a wrong answer at exit 0.
* **Ask the set for its recorded element kind and match the argument against it.** That is the bug in
  another hat: the set's element kind is a claim about its members, and a `discard` is a question about a
  value the program is holding. It is also what made `print` lie about the survivors.
* **Do `remove` as `discard` + an `in` test.** Two scans, two doors, and the second can answer differently
  from the first the moment one of them learns something the other hasn't (**ADR 0232**). `remove` asks
  `rt_set_find` once and keeps the position.
* **Register the unhashable raise *after* the pair is built.** Then a member like `[1]` would be lowered —
  materialised into a heap slot — before the raise, spending an allocation on a program the language
  refuses. The gate answers first, on the AST, and emits nothing when it declines (**ADR 0288**).

## Consequences

* `pkg/lang/set_member_removal.go` — `setMemberRemoval`, `memberIsABareSet`, `guardRemovableMemberTag`.
* `pkg/lang/codegen_method.go` — the runtime-set door takes `add`, `discard` **and** `remove`; the two
  removal verbs leave for `setMemberRemoval` after argument-count validation.
* `pkg/lang/codegen_ir_tagged_slots.go` — `rt_set_discard_tagged` is `rt_set_find` + `rt_set_remove_at`
  with an absent test between them; `rt_set_remove_at` shifts the tail of the payload array **and** the tag
  array, which the payload-only shift never had to keep in step.
* `pkg/lang/codegen_index.go` — `literalKeyRepr` gained `*SetLit` (and `set()` for the empty spelling),
  `keyNotFoundMessage` gained the constructor spellings and the loop item.
* `pkg/lang/codegen_for.go`, `pkg/lang/codegen_comp.go`, `pkg/lang/codegen.go` — `loopElem`, the unroll's
  record of *which* element this body copy walks, read by the sentence road alone.
* The payload-only `rt_set_discard` stays defined (other callers, other questions), but no removal reaches
  it; `pkg/lang/set_member_removal_test.go` denies main that call.
* Evidence: `pkg/lang/set_member_removal_test.go` (26 answering rows on the compiled leg **and** the record
  leg, 3 IR rows, 11 trap rows judged on both legs, 3 bare-set rows, 9 sentence rows, the two doors asked
  directly of the AST, 1 owed-order row, 2 owed-sentence rows) and `integration/set_member_removal_test.go`
  (29 CLI rows against a live `python3`, 15 raise rows whose sentence is re-derived from the reference
  inside the case, 5 exit-code rows, 3 pinned wrong answers naming the row that owes each).
* `tools/recmerge` wrote **72** entries from the reference with the compiled leg cross-checked
  (**9454 → 9524**) and refused the ones whose compiled leg disagrees — those are the pinned rows above, not
  records.
