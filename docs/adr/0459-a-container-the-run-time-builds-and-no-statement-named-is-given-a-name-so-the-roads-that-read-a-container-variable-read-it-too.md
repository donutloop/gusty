# A container the run time builds and no statement named is given a name, so the roads that read a container variable read it too

Status: accepted.
Date: 2026-10-30.
Roadmap rows: **Gap R.235** (paid), **Gap R.539**, **Gap R.540** (the declines this door makes by name),
**Gap R.541** (the copy's register liveness, paid beside it).
Builds on: **ADR 0333**/ADR 0334 (a call's container answer, read as an object), **ADR 0335** (a slice copy
is a list with its base's tags, and the *binding* records it), **ADR 0458** (the shape of this fix: rewrite
the node, then let the position lower it), **ADR 0232** (one question, one door, at the altitude that
decides), **ADR 0181**/ADR 0347/ADR 0443 (a live object must be reachable from a root).

## Context

Three constructs build a container object and never name it:

- `rt_slice` builds the copy — `xs[0:2]`;
- a comprehension's loop builds its brace — `[x for x in xs]`;
- a slot holds the object another container wrote — `d[0]`, `m()["k"]`.

Every one of them lowers to a heap handle. The roads that **read** a container asked their question of the
base's **spelling**: the fold's gate wanted "an inline list/dict/set literal", the copy's gate wanted "a
literal, a variable a binding recorded as a list, a call that hands back a list, or a slice of one". A
handle has a spelling and none of those words describe it. The consequence, measured on the row ADR 0335
left open (`Gap R.235`), was that one program was an answer and a refusal depending on whether it had
bothered to name an object it built:

| program | the reference | this toolchain before this ADR |
|---|---|---|
| `xs = [1.5, "a", [7]]` / `ys = xs[0:2]` / `print(ys[0])` | `1.5` | **`1.5`, since ADR 0335** |
| `xs = [1.5, "a", [7]]` / `print(xs[0:2][0])` | `1.5` | exit 1 — `index requires an inline list/dict/set literal` |
| `xs = [1, 2]` / `print(xs[0:2][5])` | `IndexError: list index out of range` | exit 1, the same fold sentence |
| `def m(): return {"k": [1.5, "a"]}` / `print(m()["k"][0:1])` | `[1.5]` | exit 1 — `cannot slice … the base is not a list the records describe` |
| `d = [[1, 2]]` / `print(d[0][0:1])` | `[1]` | exit 1, the same gate sentence |
| `print([x for x in [1, 2, 3]][0:2])` | `[1, 2]` | exit 1, the same gate sentence |
| `xs = [1.5, "a", [7]]` / `v = xs[0:2][1]` / `print(v)` | `a` | exit 1 — the fold's sentence, naming **Gap R.82** |

The last row is the one that shows why the fix could not be one gate. The print and binding roads ask for a
slot's **(payload, tag)** **pair**; the ordinary subscript road loads **one word and calls it the value**. Opening
the copy's gate alone was built first and measured second, and it produced ADR 0166's worst class:
`xs = ["x", "y"]` / `print(xs[0:2][0] + xs[0:2][1])` answered **`1`** — the two texts' indices into
`@str_tab`, summed — at **exit 0**, where CPython answers `xy`. A text slot's one word is an interned index,
a float's is a box handle, a container's is a heap index; none of them is the value.

## Decision

**Give the object a name, and let the roads that read a container variable read it.**
`pkg/lang/container_copy_read.go` holds one door and the four positions that ask it:

- `containerCopyOperand(b, e)` — asks the object's own builder for the handle (`containerHandleOf`, the road
  the print, the length and the membership doors already use), roots it in a fresh frame local, writes the
  **same notebooks** the statement `ys = <that>` writes, and hands back a `Name`;
- `containerCopySubscript` — the subscript door's half (`obj[i]` whose `obj` is a copy), which is the row
  `Gap R.232` and `Gap R.82` were pointing at;
- `containerCopySliceBase` — the copy door's half, for a base the run time built (`[x for x in xs][0:2]`,
  `d[0][0:1]`, `m()["k"][0:1]`);
- `containerCopyPairBase` — the same rewrite standing at the **pair** doors (`subscriptPairOf`,
  `runtimeSlotPairDeep`), which the print road, `v = xs[i]` and the binding road ask **before** they choose what
  to ask the subscript road for. That is what makes `print(xs[0:2][1])` print `a` and `v = xs[0:2][1]` bind `a`
  — the slot read with its tag, exactly as the bound spelling has always answered it. The **comparison** and
  operator positions, which need one word rather than a pair, keep their own refusal
  (`print(xs[0:2][1] == "a")` on a list whose slots mix kinds stays **Gap R.82**'s sentence with **Gap
  R.146**'s missing pair); `pkg/lang/slice_copy_root_test.go::TestWhatTheCopyDoorStillCannotReachIsFiledNotFixed`
  pins that the door did not swallow them.

The caller lowers the rewritten node, so the bounds raise, the mutator, the pop pair, the set hash door, the
container printer and the length door all answer the direct shape **without being told anything exists** —
no new behaviour, only a new access path to behaviour that already existed behind a name.

**Claims are imported, never invented.** Every kind and tag claim the door writes comes from the road that
built the object: `recordContainerSlice` for a copy (ADR 0335's own door), `recordCompBindingClaims` for a
comprehension (extracted from the binding road so the two roads cannot disagree — Gap Q.1's "one door, twelve
claims"), `listObjectClaims`/`containerSlotsCarryTags` for a slot's object (the builder's
`literalNeedsHeap || literalNeedsTags || literalHasPairElement`). An object whose element kind no builder ever
claimed is **declined by name**, and the decline keeps the position's own refusal: that is `Gap R.539` (a
comprehension whose loop copies a container that mixes kinds) and `Gap R.540` (a name whose last binding was
such a slot read), each refused at exit 1 rather than labelled with a claim the compiler made up.

**A door that opens a one-word position needs proof that a road exists to render it.** `containerCopySubscript`
takes an untagged copy only where `containerSlotsAnswerOneWord` proves every slot holds a value whose one word
*is* the value (an integer, or a verdict wearing 0/1) — asked of the literal the program spelled, the answer the
scan proved, or the claim the builder left on the name. Anything else is the pair door's or nobody's, which is
how the `1` above stays a wrong answer the toolchain does not print (ADR 0232's rule, one position further in).

**The copy is rooted where it is built** (`gcRootSliceCopy`, `codegen_gc.go`, called by the one road that emits
`rt_slice`). The measured shape is `print(m()["k"][0:1], m()["k"][1:2])`: the first copy is built, the second
argument's call runs, that call's body is a collection point (ADR 0347, ADR 0317), the collector cannot see the
copy's register, the arena hands its slot to the next allocation, and the print answered
**`['k'] ['a']`** for CPython's **`[1.5] ['a']`** — the copy of a list printing the dict's own KEY as its single
slot, at the exit code of success. This is **Gap R.541**, the third member of a family with two prior members
(ADR 0443's `gcRootDuringBuild` for a comprehension's half-built object, `gcRootIterable` for a loop's
iterable) and the last one the roads mint. The root is deliberately **not** released at the end of the
statement that built it, because the consumer lowers and reads later; the retention is bounded instead — one
`%_sroot` alloca per slice site, one handle live in it, the frame's close drops it.

## Gate, and what stays refused

The copy's gate (`sliceBaseIsDescribed`) is unchanged in shape and widened in its arms: a literal, a recorded
variable, a scan-proved call, a slice of one, a **brace** the program wrote (`staticContainerKind`'s new `*Comp`
arm, which is spelled by the syntax rather than inferred) and a **slot** whose object this door can pin to the
expression that wrote it. A base the records do not describe is still refused with a sentence naming the missing
record (ADR 0249), now citing `Gap R.540` — the row that owes it — and the capability table's
`lower.unsupported.slice-base-not-described` sample moved to that still-refused shape so the code stays a claim
about something the toolchain actually declines.

## Machine path

No new flag, no new code, no new schema field: the refusal codes, the exit codes (ADR 0006) and the
`--explain` catalogue are unchanged except that one sample. `gustyc --emit-llvm` is the witness for the rooting
claim (`store i32 %slN, i32* %_srootM` beside `rt_root_put`), which is what the suite asserts, because a GC
window cannot be read off a run.

## Tests

- `pkg/lang/slice_answer_test.go` — the direct shapes move from `TestWhatTheSliceDoorStillCannotReachIsFiledNotFixed`
  into the answered tables; the bound and direct spellings are compared as one program; every module is verified.
- `pkg/lang/slice_copy_root_test.go` — eight survival rows (compiled leg + record leg), the IR claim that the copy
  is parked and pushed, the claim that the root is not cleared by its own statement, the one-root-slot-per-site
  bound, and the filed-not-fixed refusals (`Gap R.539`, `Gap R.540`, `Gap R.146`-shaped pair).
- `integration/slice_answer_test.go` — the same rows through the CLI against a live `python3`, plus the raise rows
  (`IndexError: list index out of range` / `string index out of range`, and their catchable twins).
- `integration/programs/slice_answer.gy` (matrix row `programs/slice_answer`, oracle `match`) extended with the
  direct shapes, line-for-line CPython, and re-recorded from the reference.
- 24 sources recorded through `tools/recmerge` (14 answered slice rows, the whole matrix program, 9 copy-survival
  rows), which refuses any entry the compiled leg will not agree to.

## Alternatives rejected

- **Widen the fold's literal gate** so `Index{Obj:*Slice}` lowers off the copy's words. Rejected: that is what
  produced the summed `@str_tab` indices above. The fold reads one word; a copy whose slots describe themselves
  has no one-word answer.
- **Answer slot-of-copy reads in the runtime** with a new `rt_slice_at` helper. Rejected: the tagged read, its
  bounds check and its catchable `IndexError` already exist behind a name; a second implementation would be a
  second place for the raise to be wrong (ADR 0232).
- **Clear the copy's root at the end of its statement.** Rejected on measurement: the consumer (print, pair door,
  loop walk) lowers after the copy is built, so the release unroots a live object again — the same bug with a
  tidy-looking dealloc. `TestTheCopyRootOutlivesTheStatementThatBuiltIt` pins the choice.
- **Let the door lower first and inspect the error** to decide whether it worked. Rejected per ADR 0458: a door
  that lowers first leaves a diagnostic about a name the program never wrote, so the claims are asked *before*
  anything is emitted.
- **Keep `print([x for x in [1,2,3]][0:2])` refused** because ADR 0432 declined generator answers. Rejected: the
  bracket comp is an eager container built by its loop, `compIsContainer` is the predicate that says so, and the
  generator's buffer answer stays declined by that arm.
