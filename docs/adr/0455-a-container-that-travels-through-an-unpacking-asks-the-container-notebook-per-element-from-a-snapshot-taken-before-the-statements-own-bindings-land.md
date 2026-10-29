# ADR 0455 — a container that TRAVELS through an unpacking asks the container notebook per element, from a snapshot taken before the statement's own bindings land

- **Status**: Accepted
- **Date**: 2026-11-02
- **Roadmap**: **Gap R.527** done — filed 2026-11-02 by **ADR 0454**, from the row that made spelled containers
  bindable in an unpacking; the swap rows THAT row added are where this one was measured. Two defects measured
  beside it are filed rather than absorbed: **Gap R.528** (`global` is honoured by the plain binding and ignored
  by the unpacking) and **Gap R.529** (the front end's undefined-name census treats an arm the condition rejects
  as if it ran).
- **Builds measured**: parent `2406d8d` (**ADR 0454**) and this commit, every byte from the gate's
  `-tags=llvm20`.

## Context

**ADR 0454** made `x, y = [1], 2` bind the object. Its own table has a row for the container swap —
`a, b = [1], [2]` then `a, b = b, a` — and that row could only exist once the statement compiled at all. The
moment it did, its neighbour was measurable, and the neighbour was wrong:

```
$ gustyc --eval 'xs = [1, 2]
a, b = xs, 5
print(a)'
0                                    reference: [1, 2]

$ gustyc --eval 'd = {"k": 1}
a, b = d, 2
print(a)'
1                                    reference: {'k': 1}

$ gustyc --eval 'a = [1]
b = 2
a, b = b, a
print(a)
print(b)'
[]
0                                    reference: 2 / [1]
```

Every one at the **exit code of success**. And `len(a)` on the first of them refused the build with
`len of a non-string variable`, so one program was refused at one door and lied at the next — **ADR 0232**'s
one-question rule violated two constructs away from **Gap R.523**'s identical shape.

The cause was the one **ADR 0412** had already named for the plain road: *"the handle travels and the notebook
claims do not."* A container is not its handle. What makes `print(xs)` print a list is a dozen compile-time
claims keyed by the **NAME** — `listVars`, the element-kind notebooks, the "my slots describe themselves" bit,
the immutable-tuple bit — the twelve **Gap Q.1** settled into one door. `ys = xs` copies them (**Gap R.415**);
the unpacking's bind loop wrote `store i32 %loaded, i32* %_a` and asked none of them. So the receiving name was a
number wearing another object's bits, and — the half that makes the swap wrong rather than merely ugly — the name
it displaced **kept claiming a container it no longer held**, which is why the swap printed `[]` (the number `2`
rendered through the list door) and `0` (the handle rendered through the int door).

## Decision

**1. The unpacking asks the same door the plain copy asks.** `container_alias.go` gains one record type and two
readers of it: `containerClaimsOf(name)` lifts a name's whole container record (kind, mixed bit, single-kind
claims), resolving the shared-handle link exactly as the read doors do, and `writeContainerClaims(to, c)` writes
it onto another name. `recordContainerAlias` — the plain road's door — is now `noteContainerAlias` plus those
two, so the two roads cannot spell the fifteen fields differently (**ADR 0232**: one question, one reader). On top
of them sits `settleUnpackedClaims`, which the unpacking calls per element: settle the kind the element arrived
in (**Gap Q.1**'s door, which owns all twelve claims and the retirement), copy the record, keep the link, root the
handle.

**2. The record is read BEFORE any of this statement's bindings land, and written afterwards.** This is not
tidiness, it is the swap. `a, b = b, a` reads `a` while settling `b` and settles `a` with what `b` was; a claim
read after the first settle describes the object that already moved. So the read loop records which name each
element came out of, the statement snapshots every such name's record before the first bind (container or word),
and the bind phase writes the snapshots. This is **ADR 0367**'s rule — an unpacking reads every value before
naming any of them — carried from the payload into the notebook.

**3. The shared-handle link is kept only when the source is not also a target.** The plain road keeps a live link
so a later promotion reaches every name holding the object (`xs = [1]` / `ys = xs` / `xs.append("a")`). An
unpacking may keep it too — `xs = [1]` / `a, n = xs, 0` / `xs.append("z")` prints `[1, 'z']` because the link is
there — but a swap may not: recording `a → b` beside `b → a` describes two objects that have already exchanged
places, and a read that resolves through the circle lands on a record of the wrong thing. `containerSharers` and
`containerAliasTarget` are already cycle-guarded, which keeps that program from hanging; it does not make the
answer right. So the link is a decision the door takes per element, and the claims it copies are the snapshot's.

**4. A name stops being a container when the element says so.** The retirement falls out of the same door
(`settleContainerKind(name, containerKindScalar)` when nothing was carried), and it was needed in two places: the
word path, where the displaced name kept its list claims, and the **float** path, which the sweep found still
printing `[1]` for `a = [1]` / `a, b = 1.5, 2` / `print(a)` — a double is not a container either, and the box the
float road stores carries its own kind. The **pair** path is left alone on purpose: a pair carries its own tag and
is answered by the tagged doors (**ADR 0367**), and guessing a container claim from a tag the compiler does not
have would be **ADR 0299**'s guess. Its two shapes are pinned with today's bytes and their owner
(**L11.1** / **Gap R.146**) in `TestWhatTheUnpackingStillBindsAsAWord`.

**5. The witness is the door multiset, not the stdout.** `TestTheUnpackedContainerAsksTheSameDoorsAsThePlainCopy`
extracts the `@rt_print*` calls `@main` asks and requires the same multiset from `b = xs` and from
`a, b = xs, 0`, and requires the swapped program to ask the doors the plain bindings ask. The stdout could not
make this claim: before the fix `print(a)` printed `[]`, which is a *plausible* list, rendered through a door the
value had no business reaching. A row that compares two bindings' doors fails when one road's notebook goes stale;
a row that compares printed bytes passed.

## Agentic rationale

An agent that writes `total, items = 0, xs` or swaps a buffer against a counter gets CPython's answer, on the
same exit class, and can no longer tell a bound container from a bound handle by reading the program's output —
which is the property that makes the answer trustworthy in the first place. Both legs witness it: eighteen
sources answered on the compiled leg AND the record leg (**18 record entries, 10368 → 10386**, written from the
reference by `tools/recmerge` with the compiled leg cross-checked), the same family run again at the CLI against a
live `python3` inside the case, and `integration/programs/unpack_container_claims.gy` as a `match` row (corpus
281 → **282**, matrix 267 → **268**, parity-asserted 231 → **232**, oracle `match` 217 → **218**). The row's
recorder (`GUSTY_RECORD_UNPACK_CLAIMS=<file>`) dumps the sources its own table asks, and the
documents-driven guard added with **ADR 0454** (`TestEveryRecordingKnobTheDocumentsDescribeIsScrubbed`) means the
knob cannot be documented as scrubbed without being scrubbed (**Gap R.410** / **ADR 0409**). No flag, no schema
change, no new diagnostic code: the machine path is the one **ADR 0166** already fixes.

## Codegen / IR implications

For `a, b = xs, 0` where `xs` is a list, `@main` now carries `call void @rt_root_put(i32* %_a)` beside the store —
the payload is a heap handle and an unrooted handle is one collection away from the segfault **ADR 0181**'s rooting
exists to prevent (**ADR 0163**, **Gap J.2** / **ADR 0234**'s rule, at a new binding site). The print call itself
is unchanged in shape (`call void @rt_print_list(i32 %hN, …)`); what changed is which door the emitter stands in
front of, which is why the assertion is about doors and not about instructions. No new runtime symbol, no new
global, no new section, no new tag: `containerClaims` is a compile-time struct, and the only runtime-visible
effect is the rooting call. A program that binds no container through an unpacking emits the module it always
emitted — the settle is reached only from the tuple road, and it is a no-op when the name never held a container
and nothing was carried.

## Alternatives rejected

- **Reading the notebook at the bind instead of snapshotting.** Measured, that is the swap answered from the
  state the first bind left behind: `a, b = b, a` would settle `a` from a `b` that had already been told it holds
  a number. Rejected for the same reason **ADR 0367** evaluates every element before storing any.
- **Recording the alias link unconditionally.** Cycle, and a claim about the wrong object. The cycle guards exist
  (`containerAliasTarget`, `containerSharers`) and they prevent a hang, not a lie.
- **Having `bindTaggedVar` settle the kind from the tag.** That is **L11.1**'s tagged word doing compile-time
  notebook work from a run-time fact: the tag says what THIS value is, the notebook says what every later read of
  the NAME will ask, and a door that fills the second from the first is guessing (**ADR 0299**). The pair path
  stays as it is, pinned with the row that owns the residue.
- **Freeing the displaced container (`emitFreeOld`) as the plain literal road does.** Tempting, and unsafe: an
  unpacking that shares handles in both directions would return a live object to the allocator. The plain road
  does it only where the binding re-builds the object. A leaked slot is not a wrong answer; a recycled live one is.
- **Fixing only `print`, which is where the defect was seen.** `len`, `in`, a `for`, an entry write and equality
  all ask the same notebook — three of them are in the answer table for exactly this reason, and `len(a)` refusing
  while `print(a)` lied was the clearest single-program proof that the defect was the record and not the renderer.
- **Comparing stdout between the two bindings instead of the doors.** That comparison passes on the buggy build,
  because the buggy build printed `0` where the fixed one prints `[1, 2]` only if you compare against the
  reference; comparing the two bindings to EACH OTHER through the doors is what fails when a notebook goes stale.

## What this row does not pay

**Gap R.528** — `global` on an unpacking target: a body that declares `global x` and writes `x, y = …` binds
locally where **ADR 0448** made the plain road write the module. Measured in this sweep, same road, different door.

**Gap R.529** — found while writing a row for this table (`a, b, c = b if False else xs, 1, "t"`): the front end
refuses `undefined name "b"` at exit 1 where CPython prints `[1, 2]`, `1`, `t`, and the same census refuses
`n = 0` / `if n:` / `    print(zzz)` / `print("ok")` where CPython prints `ok`. An arm the condition rejects is
checked as if it ran; the reference only needs the name to exist if the branch is taken.

**Gap R.526** — a container element mentioning a name the same statement re-binds, still refused in words
(**ADR 0454**), and unchanged by this row: the snapshot this row needed lives in the notebook, and the snapshot
that row needs lives in the payload.

**L11.1** / **Gap R.146** — a pair-bound element's container claims, pinned with today's bytes in
`TestWhatTheUnpackingStillBindsAsAWord`; and **Gap R.321** — an unpacking target that is not a name, and
unpacking from ONE collection.
