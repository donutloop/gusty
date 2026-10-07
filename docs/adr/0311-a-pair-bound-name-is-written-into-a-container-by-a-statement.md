# 0311. A pair-bound name is written into a container by a statement

Status: accepted. Roadmap: `L11.1` (the tagged value word), `Gap R.146` (the positions that keep **one word**
for a whole value — the mutation roads are paid here; a builtin-folded static array, a pair handed across a
call and an f-string used as a value still owe it), `Gap R.81` (the hashing rule, asked here for the shapes
this door opened), `L12.11` (the container row). Continues the pair arc: ADR 0232 (the tagged dict/set slots),
ADR 0303 (the one printer), ADR 0304/0305 (the pair's arithmetic and the double domain), ADR 0306 (the
container element), ADR 0307 (the f-string field), ADR 0309 (the signless call), ADR 0310 (the dict entry and
the set member), ADR 0308 (the witness vocabulary), ADR 0187 (a payload is never written without its tag).

## Context

ADR 0310 paid the dict entry and the set member **in the expression that builds a container**. The same four
words in the statement that *changes* one still refused:

```
xs = []          # n is a (payload, tag) pair — ADR 0303's print, 0304's arithmetic, 0306's element,
xs.append(7)     # 0307's field, 0309's abs and 0310's dict entry all answer for it
n = xs[0]
ys = []
ys.append(n)     # CPython [7]  · compiled exit 1
s.add(n)         # {7}          · exit 1
xs[0] = n        # [7]          · exit 1
d["k"] = n       # {'k': 7}     · exit 1
d[n] = 1         # {7: 1}       · exit 1
```

The measurement was run before anything changed: six slot kinds (int, text, float, `None`, verdict,
container) over 24 bodies — the four mutators and every position that reads the container back (`print`,
`len`, `in`, `for`, `str`, a subscript read) — 144 programs against CPython. Fifteen agreed, all of them
because both sides failed. That number is the row's whole content, and it is why the cycle measured first:
the tracker's own example list had long since stopped containing any of these shapes.

Each of the four roads already had a **tagged door**, because a heterogeneous literal needed one (ADR 0232):
`rt_append_tagged(h, v, tag)`, `rt_set_add_tagged(h, v, tag)`, `rt_dict_put_tagged(h, k, v, kt, vt)` and the
`rt_put_elem`/`rt_tag_elem` pair. What they were missing was a caller with two words to hand: every one of
them got its element from `heapElemKind`, which asks an expression for its **spelling**, and a name has no
spelling that describes what the objects hold. Reading one word out of a pair is not a partial answer, it is a
different value — a text slot's payload is its `@str_tab` index, a float slot's a box handle, a container's an
entry count — and a container that stores one prints it today and keeps printing it through every `len`, `in`,
`for` and `str` that follows.

## Decision

**The pair goes to the door that already takes the tag, and the container is told its slots speak.** Each road
asks `pairElemPair` first; a pair answers with the value the object wrote into the slot and the value it wrote
into `_name_tag`, both registers, and the road emits its tagged call with them. `promotePairMixed` does
`promoteMixed`'s bookkeeping — clear the container's declared key/value/element kinds, set the self-describing
bit 8, switch the float printer on — without `promoteMixed`'s `elemKindTag` precondition, which is exactly the
question a pair cannot answer. **`pkg/lang/runtime.go` has no diff in this commit**, and that is the check a
later cycle can run: if a pair-aware append ever appears, the invariant has been duplicated rather than kept.

**Every value that is *not* a pair is still asked what it is by the ordinary road first.** This is the half
that nearly wasn't. The first version deleted `assignIndex`'s eager `g.value(b, val)` because it refused
before the pair road got to answer; the sweep then found `xs[0] = xs[0] / 2` printing `[2]` where CPython
prints `[3.5]` — a true division's payload dropped into an `i32` slot, at the exit code of success, while
`d["k"] = xs[0] / 2` printed `3.0` only because the dict's printer happened to ask. Both had been honest
refusals (Gap R.88's sentence) and one edit had quietly converted refusals into wrong answers. The eager
question is back, guarded to the non-pair case, and `TestWhatTheMutationRoadsStillRefuseIsStillRefusedInWords`
pins it: **a row that stops refusing without answering has not been paid, it has been lost.** Where the road
already carried a pair — `ys.append(xs[0] / 2)`, through the mixed-list door — the answer is `3.5` and that
row is pinned as an answer rather than a refusal.

**A key and a member ask the hashable question on these roads too.** `s.add(n)` and `d[n] = v` put the value
in a bucket, and a payload answers "hashable" for every value the language has, so `guardHashableTag` (ADR
0310) is emitted wherever a pair writes a member or a key: three compares against the list/dict/set tags,
three raises spelling CPython's `TypeError: unhashable type: 'list'`/`'dict'`/`'set'`, catchable by the
program's own `except TypeError:`. `Gap R.81` still owns the general rule — the guard covers the doors this
cycle opened, and `TestAMutatedContainerTakesItsWordsFromTheTaggedDoors` asserts the guard is present for a
pair-written member/key and absent where a literal's kinds are constants.

**What stays refused.** A literal a builtin folds into a static array (`sum([n])`, `min([n, 3])`) has no tag
storage and its lookup is a second teacher; a pair handed across a call (`{"k": k}` in a body whose parameter
is not a pair) is `Gap R.154`'s boundary; a value that is not a pair written into a slot keeps the ordinary
road's refusal; `xs[n] = 1` — a pair used as a *position* — is a different question (a text slot's payload is
an index into another table where the reference raises `list indices must be integers or slices`) and stays
refused with the rest. Each keeps a sentence naming the value's origin, the missing half and the roadmap row,
at exit 1, never exit 2 (ADR 0166).

## Agentic rationale

The IR is the assertion. `TestAMutatedContainerTakesItsWordsFromTheTaggedDoors` fails if a road stops calling
its tagged door, if the tag stops being read out of `%_n_tag`, if the tag arrives as a constant when the pair
had a register, if the container is never marked self-describing, or if the guard disappears from a member/key
road — every one of which is a silent one-word misread that no stdout comparison against the record can
distinguish from a correct container. The probe `programs/probe_a_pair_bound_name_mutates_a_container.gy` is
registered `match` (172 rows, 133 conformant), so an agent reads the mutated containers' bytes next to
CPython's instead of reading this ADR to learn what the backend does. `TestThePairBoundMutationProbeIsOnRecord`
is the loud version of ADR 0302's missing-record rule, and this cycle also corrected the 0310 probe's record
entry, which had been written without `hasStdout` and therefore compared nothing — a record field an agent
reads as "the answer" has to be the field the harness reads.

## Alternatives rejected

- **Lift the payload and pass a constant tag** (the obvious implementation, and the one every cycle refuses):
  it converts a text's interned index and a float's box handle into plausible container contents at exit 0.
- **Add `rt_append_pair`/`rt_dict_put_pair` helpers.** Rejected: the tagged doors already take `(payload, tag)`
  and branch per kind; a second door for one object is where ADR 0187's invariant goes to die.
- **Widen `mixedElemTag`/`taggedOperand` instead of adding pair arms.** Rejected: those helpers serve the folds
  and the arithmetic roads too, and widening them makes `sum([n])`/`min([n, 3])` answer *something* before
  their own lookup has been taught to read a tag — the exact "half-paid table" failure `Gap R.146` warns about.
- **Drop the eager `g.value` question to unblock the pair roads.** Rejected after it shipped in the working
  tree and turned two honest refusals into two wrong answers (`[2]` for `[3.5]`); the pair arms take the pair
  and nothing else, and the ordinary road keeps asking its own question.
- **Pay `xs[n] = v` (a pair as an index) alongside the values.** Rejected: the reference raises for every
  non-integer index and the payload of a text slot is an index into a *different* table; the guard would be a
  new trap family, not this door.
