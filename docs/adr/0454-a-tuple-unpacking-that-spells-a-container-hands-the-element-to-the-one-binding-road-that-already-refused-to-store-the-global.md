# ADR 0454 — a tuple unpacking that spells a container hands the element to the ONE binding road that already refused to store the global

- **Status**: Accepted
- **Date**: 2026-11-02
- **Roadmap**: **Gap R.524** done — filed 2026-11-01 by **ADR 0453**, from the sweep that closed **Gap R.522**;
  it was found while checking a *control* row of that row against the reference. One defect this sweep measured is
  filed beside it: **Gap R.527** (an unpacking element that is a NAME holding a container binds the handle as a
  number), found while checking this row's own swap rows against the reference.
- **Builds measured**: parent `7b204b2` (**ADR 0453**) and this commit, every byte from the gate's
  `-tags=llvm20`.

## Context

`x = [1]` compiles, prints `[1]`, and has for hundreds of rows. One binding position over, the same literal is
written the same way and the module does not exist:

```
$ gustyc --eval 'x, y = [1], 2
print(x)'
llc-20: jit.ll:4202:13: error: global variable reference must have pointer type
  store i32 @.lst1, i32* %_x                                             reference: [1]
```

That is not a wrong answer, a missing feature or a refusal. It is **invalid IR**: a compile-time container global —
a length beside an array, a whole struct that cannot live in an `i32` — whose NAME was handed to a value slot. The
exit-code contract (**ADR 0166**) charges a module `llc` rejects to the compiler, so an ordinary program, written
the way the reference's own tutorial writes it, ended at the compiler-bug exit class. And the shape was not exotic:
it surfaced in **ADR 0453**'s *control* table, in the rows that were supposed to be a formality.

The unpacking road had every reason to be wrong and one reason to be nearly right. It lowers each element through
`g.value`, and `g.value`'s answer for a spelled container literal is the folded global. Meanwhile the ordinary
assignment road had met that exact global eight ADRs ago and refused to store it: **Gap J.2** / **ADR 0234** moved
`sa = {1, 2}` off `store i32 @.set1, i32* %_sa` and onto building a heap object — the same `rt_alloc`, the same
per-slot tags, the same twelve container claims (**Gap Q.1**'s one door), the same rooting store (**ADR 0163**). The
unpacking is the binding road that never went to that meeting.

The tempting diff was the same size as the bug: load the global's *value* instead of its name, or copy the
heap-building block out of `stmtAssign` into the unpacking. Both are wrong for the same reason — they make the
literal have two lowerings, and the second one starts drifting the moment the first grows a thirteenth container
claim.

## Decision

**1. One literal, one lowering: the unpacking asks the plain road.** A `*ListLit`, `*SetLit` or `*DictLit` element
is not marked as unanswerable and is not rebuilt. It is marked `container` and, in the bind phase, handed to
`g.stmtAssign` as a synthesised `AssignStmt{Target: Name, Value: literal}` — the same function `x = [1]` walks, so
the two bindings emit one shape by construction rather than by imitation. A guard asserts that construction rather
than trusting the comment: the same literal through `x = [1, "a"]` and through `x, y = [1, "a"], 2` must ask
`@rt_alloc(`, `@rt_set_elem(` and `@rt_tag_elem(` the same number of times
(`TestTheUnpackedContainerIsStoredAsAnObjectNotAName`), because two roads that "should" agree are the drift this
suite exists to catch (**ADR 0232**).

**2. The bind runs between the reads and the stores, which is what keeps an unpacking simultaneous.** An unpacking
reads every value before naming any of them (**ADR 0367**); the container bind performs a *store*, so where it runs
is a correctness question, not a layout question. It runs **after** every element has been read out of its slot
and **before** any element is stored: at that point the names a container's own elements mention still hold what the
program had before this statement, and the one store the plain road performs cannot steal a swap that has not
happened yet. The swap rows beside the answer rows are the witness, not this sentence.

**3. What the road cannot do is refused in words that name the missing half.** If a container element mentions a
name the SAME statement re-binds — `b, a = [b], [a]` — then the literal must be built from the value the name *HAD*,
and the plain road can only build it from the slot as the binding left it. Before this row that shape produced a
self-referential literal and printed `[[[[[[…` until the process died with a segmentation fault: a wrong answer
again, at a worse exit code. The gate is therefore explicit — collect the names each container element mentions, and
if one of them is also a target of this statement, refuse with a message that names the name, the binding, the
reference's behaviour and the row that owns the snapshot (**Gap R.526**). A refusal that names its owner is the
artifact **ADR 0166** asks for; a segfault is not.

**4. The claim is asserted in the module's bytes, not only in its stdout.** Reading printed bytes cannot distinguish
"fixed" from "the failure moved", because before this row nothing was produced at all. So the IR is asked directly:
no `store i32 @` and no `ret i32 @` anywhere in the module, and `@main` actually calls the heap door for each of the
three kinds (`@rt_set_elem(`, `@rt_set_add(`, `@rt_dict_put(`). At the CLI the same claim is made with
`--verify-llvm-file`, which is the only way to tell a verifying module from one that never reached the verifier.

## Agentic rationale

An agent that writes `total, items = 0, [1, 2, 3]` gets an answer now instead of a toolchain error, and the two
witness legs say the same thing about it: fourteen programs (list, set, dict, both empty and then written to, nested
two and three deep, a text element, membership, a `for`, an entry write, a tuple element, containers swapped through
names, a container and a scalar rebound together) are asserted against the reference's bytes on the compiled leg,
re-asserted from the record leg (`onRecordIs`), and run again at the CLI against a live `python3` inside the case.
The refusal is machine-consumable too: the declined shape exits non-zero without reaching `llc`, and its message
carries the roadmap row, so a caller can branch on the missing capability instead of scraping a toolchain dump.
No flag, no schema change, no new diagnostic code — the machine path is the one **ADR 0166** already fixes.

The record leg grew **15 entries (10352 → 10367)**, written from the reference by `tools/recmerge` with the compiled
leg cross-checked, and the row's own recorder (`TestTheUnpackingRecordSourcesTheTableAsks`, env
`GUSTY_RECORD_UNPACK=<path>`) dumps the sources its table asks, so a case cannot ship without a record under it
(**ADR 0302**). Like the other artifact-writing modes it is scrubbed from every subprocess the suite starts
(**Gap R.410** / **ADR 0409**) — a recorder N shards each overwrite is not evidence.

## Codegen / IR implications

For `x, y = [1], 2`, `@main` now contains the same sequence `x = [1]` has always contained — `call i32 @rt_alloc(…)`,
`call void @rt_tag_elem(…)`, `call void @rt_set_elem(…)`, `store i32 %hN, i32* %_x` — plus the ordinary word store
for the scalar element. The folded globals `@.lst1`, `@.set1`, `@.dict1` disappear from the module for this shape:
they are compile-time layout, and the only legal way to bind one is to build the object it describes. The rooting
store is unchanged and load-bearing: the handle is written into a slot registered as a root (**ADR 0163**,
**Gap J.2**), so a container bound by an unpacking survives a collection the way one bound by an ordinary
assignment does. No new runtime symbol, no new global, no new section, no new GC root kind — the diff is in
`pkg/lang/codegen_assign.go` and touches no runtime file.

The refusal runs before any of that is emitted, so a declined shape produces no module at all — which is asserted,
because a refusal that reaches `llc` is a different exit class and would read as a compiler bug.

## Alternatives rejected

- **Loading the global's value instead of its name.** `@.lst1` is `{ i32 <len>, [N x i32] }`; there is no word to
  load. This is the fix the roadmap row's own `How to fix` cell floated first, and it is not expressible.
- **Copying the heap-building block from `stmtAssign` into the unpacking.** Two lowerings for one literal, and the
  two silently diverge as soon as the container notebook grows a claim (the twelve-claim door **Gap Q.1** exists
  precisely because three of them used to be set in three places). Rejected in favour of calling the road.
- **Answering the element through the pair road (`assignPairWords`).** The pair doors answer reads — slots, calls,
  arithmetic, folds — and a spelled literal is not a read: it has no tag to carry because the compiler wrote every
  slot. Asking a pair door first is harmless but answers nothing; the literal's own road already tags each slot.
- **Building the container eagerly at read time (before the loads).** That would make `b, a = [b], [a]` answer
  instead of refusing — by reading `b` after the statement's own earlier store, which is the self-referential
  literal that segfaulted. The snapshot that makes it correct needs a value the road has already thrown away; that
  is **Gap R.526**, not this row.
- **Letting the shape run and printing what comes out.** Measured: `[[[[[…` to a segfault. **ADR 0166**'s whole
  argument is that a refusal naming the missing thing beats an invented answer, and here the alternative to the
  refusal was not even an answer.
- **Shipping the fix with only stdout assertions.** The defect was invisible to stdout, since no stdout existed.
  `--verify-llvm-file` and the no-`store i32 @` assertion are the pair that make "fixed" a measured claim.

## What this row does not pay

**Gap R.526** — a container element that mentions a name the same statement re-binds — is refused in words, pinned
with its own message in `TestWhatTheUnpackingStillRefusesToPretend`, which fails when the shape starts compiling so
paying it moves the row instead of deleting a guard.

**Gap R.527**, measured while checking this row's swap rows: an element that is a NAME holding a container still
binds the handle as a plain word, because the container's claims never travel with the object. `xs = [1, 2]` /
`a, b = xs, 5` / `print(a)` prints `0`, `s = {1, 2}` / `a, b = s, 2` prints `0`,
`d = {"k": 1}` / `a, b = d, 2` prints `1`, `len(a)` refuses with `len of a non-string variable`, and the mixed swap
`a = [1]` / `b = 2` / `a, b = b, a` prints `[]` and `0` where the reference prints `2` and `[1]` — all at the exit
code of success. The plain road solved the same problem with `recordContainerAlias` (**ADR 0412**); the unpacking's
bind loop has not asked it. It is the same one-word-position family as **L11.1** / **Gap R.321**, and it is why a
row about containers has to be measured against the swap rows and not only against the literal rows.

Also untouched: a target that is not a name (`a, d[0] = 1, 2`, **Gap R.322**), unpacking from ONE collection
(`a, b = xs[0]`, **Gap R.321**), star targets (`a, *b = …`, not in the grammar), nested tuple targets
(`a, (b, c) = 1, [2]`, which parses and then leaves `b` undefined), and `global` on an unpacking target — a body
that declares `global x` and writes `x, y = …` binds locally where **ADR 0448** made the plain road write the
module (**Gap R.528**).
