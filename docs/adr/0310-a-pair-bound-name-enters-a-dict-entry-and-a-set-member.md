# 0310. A pair-bound name enters a dict entry and a set member by way of its tag

Status: accepted. Roadmap: `L11.1` (the tagged value word), `Gap R.146` (the positions that keep **one word**
for a whole value — the dict entry and the set member are paid here; a builtin-folded static array, the
mutation roads and an f-string used as a VALUE still owe it), `Gap R.81` (the hashing rule this cycle asks
for the shapes it opened), `L12.11` (the container row whose owed list the dict/set entry was the last
language clause of). Continues the pair arc: ADR 0265 (the arithmetic door), ADR 0232 (the dict and the set),
ADR 0303 (the one printer), ADR 0304 (a pair-bound name's arithmetic), ADR 0305 (the double domain), ADR 0306
(the container element), ADR 0307 (the f-string field), ADR 0309 (the signless call), ADR 0308 (the witness
vocabulary the retired engine lives behind).

## Context

`xs = []` / `xs.append(7)` / `n = xs[0]` / `print({"k": n})` is CPython's `{'k': 7}`, and the compiled backend
refused it — while `print([n])` already answered. The same name answers `print(n)` (ADR 0303), `print(n - 1)`
and `print(-n)` (ADR 0304), `print(n / 4)` (ADR 0305), `print([n])` (ADR 0306), `print(f"{n}")` (ADR 0307)
and `print(abs(n))` (ADR 0309). The dict and the set were the last positions where the shape of the object
decided whether the pair was readable.

A pair-bound name is **two words** — the payload the slot held, and the tag that says what it means. Every
position paid in this arc has been a position taught to read both. The positions still in `Gap R.146` are the
ones that read **one**. A dict and a set are each filled by **one builder call that takes the tag as a
number** — `rt_dict_put_tagged(i32 h, i32 k, i32 v, i32 kt, i32 vt)` and
`rt_set_add_tagged(i32 h, i32 v, i32 t)` — and the roads that filled them asked the pre-pair helper
`heapElemKind` for the element's payload and `elemKindTag`/`dictKeyTag` for its label, **after** the tag
question had already been answered elsewhere. For a name those helpers have nothing to say: the label they
produce comes from a spelling, and a name has no spelling that describes what the objects hold.

The silent shape of the bug is worth restating, because it is the reason this row could not be closed by a
refusal table. A pair-bound text's payload is its `@str_tab` index; a float's is its box handle; a
container's is its entry count or a heap address. `print({"k": s})` where `s` is a text slot would print
`{'k': 0}`, and `print({f})` where `f` is a float slot would print `{140737488355328}` — a plausible
container, at the exit code of success, in a suite that counts refusals and not answers (`compiled refusals
this run` stays `0`). ADR 0302's ledger calls that the worst class of defect.

## Decision

**A pair's tag is a register, and the two builders take an `i32`.** `taggedSlotWords`
(`pkg/lang/heapargs.go`) returns `(payload, tag, isPair, ok)`: for a pair-bound name the payload is the value
the object wrote into the slot, the tag is the value it wrote into `_name_tag`, and both are registers; for
an element the compiler can read, the payload and tag come from its literal, and the tag is a constant. Both
go to the same call. `literalTakesPairSlots` is the gate, and it is deliberately a **mixed** one: a literal
that holds a pair goes to the tagged builder only if every other entry is still something
`elemKindTag`/`dictKeyTag` can label — `{"a": s, "b": 2}` answers, `{"a": s, 2.5: 1}` keeps its honest
refusal instead of half-answering.

**Nothing was added to the runtime, on purpose.** `rt_dict_put_tagged` and `rt_set_add_tagged` already
carried both words and already branch per kind (`dictKeyKind`, `dictValKind`, `setKind`), because the
heterogeneous literals ADR 0232 shipped needed them. A new helper would be a second road for the same object,
and a second road is where "a payload is never written without its tag" (ADR 0187) goes to die — the IR test
`TestAPairBoundDictAndSetRunThroughTheTaggedBuilders` fails if the entry stops asking those two calls, or if
the tag it passes is a constant when the element had a register to hand.

**The binding keeps the tags too.** `d2 = {"k": n}` is the same question asked by the assignment road, which
keeps its own record of a variable's key and value kinds beside its own tag slots (ADR 0306's list-binding
precedent, ADR 0281's "a rebinding retires the kind a check was built on"). A table row that pays the literal
while the binding keeps refusing is the trap `TestAPairBoundNameBoundIntoADictKeepsItsTagOnTheObject` and
`TestAPairBoundNameBoundIntoASetKeepsItsTagOnTheObject` close: `d2 = {"k": s}` then reads `d2["k"]`, `len(d2)`,
`k in d2`, `d2[k]`, `str(d2)`, `d2 == {"k": "a"}` and a `for k in d2` — every one of which asks the tag of an
entry the builder wrote.

**A key and a member ask a question no element is asked, and the tag is what answers it.** A dict key and a
set member have to be hashable; CPython refuses a list, a dict and a set with `TypeError: unhashable type:
'list'` and company. A payload alone answers "yes" to that question for **every value in the language** — a
list's handle is an i32 like any other — so the compiled leg would have built a set holding an address and
printed `{[1, 2]}` at exit 0. `guardHashableTag` therefore emits, per kind, a compare-and-raise **in the
program**: three `icmp eq` against the list/dict/set tags, each to a block that calls the trap raiser with
CPython's own sentence (`ADR 0271`: a trap says what the program wrote, and the program's `except TypeError:`
can catch it — `TestTheUnhashablePairRaiseReachesTheProgramsOwnArm`). The guard is emitted **only** where a
pair writes a key or a member, which `TestADictAndSetLiteralTheCompilerCanReadKeepsItsStaticRoad` pins from
the other side: a dict or set whose every kind is a constant keeps its old road and does not grow three
compares per entry for a question the compiler already answered. `Gap R.81` owns the general hashing rule;
this door asks the question for the shapes it opened, and the refusal table says so.

**A position that is not a pair's position keeps its road.** An element the compiler can read keeps its
static-array or literal road; `TestADictAndSetLiteralTheCompilerCanReadKeepsItsStaticRoad` fails if a known
key or member starts consulting the run time for its kind. The refusal rows this cycle deliberately did not
touch are `sum([n])`, `min([n, 3])`, `max([n, 3])` (a literal a builtin **folds** into a static array has no
tag storage at all, and teaching the array means teaching the lookup that reads it back), the **mutation**
roads `ys.append(n)`, `s.add(n)`, `d["k"] = n`, `d[n] = 1` (the same entry question in a statement that
changes a container rather than an expression that builds one, ADR 0300's void-returning mutators), and
`{"k": k}` inside a function body over a pair handed through a parameter (the callee's parameter is not a
pair — `Gap R.154`'s boundary, not this door). Each keeps the sentence that names the value's origin, the
missing half and the row that owes it, at exit 1 and never at exit 2 (ADR 0166).

**A bug the row exposed on the way in.** `heapElemKind` claimed a list element was `("list", containerCount)`
with the tag of an `int` — the same pair of words ADR 0306 found it returning for a **set**, for the same
reason: the branch that reads the entry count is not the branch that labels the element, and a set has no
entry count. Both dead roads are now `unreachable` assertions with the lie written where the next reader will
find them. They had no live caller, which is exactly why they had to be fixed in the same commit: a dict
entry is the door that would have started calling it.

## Agentic rationale

The IR is the assertion, not the output. `TestAPairBoundDictAndSetRunThroughTheTaggedBuilders` fails if the
entry stops asking `@rt_dict_put_tagged`/`@rt_set_add_tagged`, if the tag stops being read out of
`%_n_tag`, or if a constant is passed where the pair had a register — every one of those is a silent
misread of one word, which no stdout comparison against the record can distinguish from a correct container.

The probe `programs/probe_pair_bound_dict_entry_and_set_member.gy` is registered `match`, so an agent reads
`{'v': 7} / {'v': 'a'} / {'v': 2.5} / … / caught-the-list-key` against CPython in the matrix rather than
reading this ADR to learn what the backend does. The record leg carries 61 new entries (60 from the tables
plus the probe), each CPython's own bytes; `TestThePairBoundDictAndSetProbeIsOnRecord` is the loud version of
"delete the record and the suite notices" that ADR 0302's missing-record rule otherwise discharges with a
skip, and it is also what stops a future cycle deleting a row to make a failure go away.

## Alternatives rejected

- **Lift the payload and let the builder guess the kind (`rt_lift_num` + a constant tag).** Rejected: that is
  the one-word reading this ADR exists to refuse. A text slot's index and a float slot's handle become a key
  and a member, and the container prints them at exit 0.
- **Grow a pair-aware dict/set builder in the runtime.** Rejected: `rt_dict_put_tagged`/`rt_set_add_tagged`
  already take `(payload, tag)` and already branch per kind; a second builder is a second road for one object,
  and the tag-reading invariant is only worth having if there is one road to read it on.
- **A compile-time refusal for every container-valued key or member.** Rejected: the reference raises, the
  program can catch it, and a refusal a program cannot `except` is a compiler opinion rather than a language
  answer (ADR 0166, ADR 0228). The guard is a typed raise, and the arm runs.
- **Guard every key and member, pair or not.** Rejected: for a literal the compiler already knows the kind,
  and three compares per entry that can never fire is both an instruction regression on the hot container path
  and a trap the program can trigger where CPython raises nothing.
- **Pay `min(n, 3)`, `sum([n])` and `d["k"] = n` in the same commit, since the helper already exists.**
  Rejected: a folded literal's static array has no tag storage and the mutation roads are a different road
  (a statement, ADR 0300) — bundling them would have moved rows out of `Gap R.146` that this cycle never
  measured. They stay refused, in words, with their own tests.
