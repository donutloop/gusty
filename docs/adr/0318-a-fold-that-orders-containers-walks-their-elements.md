# 0318. A fold that orders containers walks their elements instead of reading a number out of the handle

Status: accepted. Roadmap: closes `Gap R.197` (filed measuring ADR 0316), the remaining clause of `L11.1`'s fold
row; files `Gap R.201` and `Gap R.202` from the same measurement and leaves them open. Continues ADR 0316 (the
fold's `(payload, tag)` door), ADR 0248 (texts order by content, not by the intern table's arrival order), ADR
0250/0252 (an ordering's raise is written per kind, from the tag), ADR 0259 (a verdict has a tag of its own), ADR
0306/0310 (a payload never travels without its tag), ADR 0182 (one tag table for the whole ABI), ADR 0228 (a trap
the program's own `except` can reach), ADR 0166 (a refusal is exit 1; exit 2 is the compiler's own bug), ADR 0314
(the reference leg pins the oracle's hash seed, which is what makes a SET comparison measurable at all).

## Context

ADR 0316 opened the fold door and filed what it left beside it. The filing was accurate, and this is the same
table with the third column filled in — measured with three binaries (the reference, the HEAD baseline, this
cycle's build), not remembered:

| program | reference | compiled before | compiled after |
| --- | --- | --- | --- |
| `la = []` / `la.append(True)` / `la.append(3)` / `o = 3` / `print(min(la, o))` | `TypeError: '<' not supported between instances of 'int' and 'list'` | **`0`, exit 0** | the raise |
| `sa = set()` over a bool slot / `print(min(sa, 3))` | `TypeError: '<' … 'int' and 'set'` | **`1`, exit 0** | the raise |
| `la = [1, 2]` / `print(min(la, 3))` | `TypeError` naming both kinds | **`0`, exit 0** | the raise |
| `min(a, b)` / `max(a, b)` over two built lists | `[1, 2]`, `[3]` | `TypeError: '<' … 'list' and 'list'`, exit 3 | `[1, 2]`, `[3]` |
| `min(p, q)` over two built sets, neither the other's subset | `{1}` (the incumbent; both comparisons are simply False) | `TypeError: '<' … 'set' and 'set'`, exit 3 | `{1}` |
| `min(a, b)` where `a` is `[]`, `b` is `[1, 2]` | `[]` | exit 3 raise | `[]` |
| `min(["a"], [1])` | `TypeError: '<' … 'int' and 'str'` — the **elements'** kinds | `TypeError: '<' … 'list' and 'list'` | the elements' sentence |
| `min(d, d)`, `min(d, e)` | `TypeError` naming `'dict'` twice | raised | raised |

Three shapes of the same missing thing. A container folded against a number answered a **length** where the
reference raises — the element count, at the exit code of success, which is the class of wrong this door exists to
make impossible. Two different containers raised the reference's own sentence at the reference's own exit class
where the reference answers a value — a wrong answer wearing the costume of a trap. And the raise that did exist
named the two containers the program wrote rather than the two elements that failed to order.

The reason all three are one bug is the reason ADR 0316 gave for the door: a fold decides a **kind**, so it has to
ask each value what it is. The door asked, and its vocabulary was numbers and texts; anything else went down the
arithmetic road, where a container is a handle and a handle is a number.

## Decision

**One helper owns the ordering the fold walks: `@rt_pair_order`.**

```
define internal i32 @rt_pair_order(i32 %depth, i32 %a, i32 %ta, i32 %b, i32 %tb,
                                  i32* %outcmp, i32* %outlt, i32* %outrt)
```

It answers one question — does this pair order before, after, or neither way — with **three statuses, because the
reference has three answers** and a two-valued door would have to give one of them wrongly:

* `0` **ordered**: `%outcmp` holds −1/0/+1 under the reference's own rules. Numbers compare in the one word that
  holds int, float and bool. Texts compare by **content** (ADR 0248), never by the `@str_tab` arrival order. A
  **list** compares lexicographically, element by element, and when every shared element is equal **the shorter
  list is the lesser one** — the reference's rule, not a rule this backend invented.
* `1` **no ordering between these two kinds**: `%outlt`/`%outrt` hold the two kinds to name. A **set** answers this
  for a member it does not order, a **dict** always — a dict against a copy of itself included, which is why the
  identity shortcut that saves a list and a set does not fire for a dict or a `None`. When the failure is *inside*
  two lists, the two slots carry the **elements'** kinds, so `[1]` against `["a"]` says `'int'` and `'str'`.
* `2` **neither order, and no raise**: two sets that are not each other's subset. The reference's `<` and `>` are
  both simply `False` there, so the fold keeps the incumbent. Conflating this with `0` would make `min({1},{2})`
  and `min({2},{1})` disagree with each other; conflating it with `1` would make the fold raise where the
  reference answers `{1}`.

**The walk carries a depth argument and stops.** `%depth` goes up by one every time a list's element is itself a
container, and the 65th level raises rather than recursing forever: a container that contains itself would otherwise
have no bottom. That guard is why `min(xs, xs)` over a self-containing list terminates; what it does not do is render
the answer, which is the separate defect this cycle measured and files as `Gap R.203` rather than describing.

**An element pair is asked for equality before ordering.** That is the order the reference's own list comparison
asks, and it is what makes `[None]` against `[None, 1]` a comparison of two lengths rather than a comparison of two
`None`s — `None` has no ordering and would raise, but the two elements are equal, so the walk moves on.

**The fold's operand pair now carries a container the compiler can name.** `foldOperandPair` asks
`foldArgCarriesAContainer`, which asks `staticContainerKind` — the analysis the printers already ask, and the one
that **emits nothing**, because deciding must not put instructions in a module that then refuses (ADR 0241's rule,
read onto a call that orders rather than subscripts). That covers a name the container analysis describes, an index
read the tag table describes, and the `set()`/`list()`/`dict()` spellings of an empty container — which matter
because `set()` is the only way to write an empty set at all (Gap K.3), and `min(set(), 3)` used to print `0`.

**A container the program WROTE among the folded values keeps the refusal it already had.** That shape is Gap
R.198's, and a door that swallowed it would answer a program the language declines (`sum({n, 3})` in written order
answers `6` where the reference answers `3`). The gate therefore opens for a container the compiler can *name* and
stays shut for one it can only *see*, and `TestTheFoldGateOpensForAContainerItCanName` pins both halves of that
sentence so a future widening cannot quietly merge them.

**The raise is the reference's sentence, in the order the failing comparison had the two kinds.** `min` asks the
later argument against the earlier one, so `min(["a"], [1])` names `'int'` and `'str'` — the element of the later
list first. The two kind slots are threaded through the walk rather than taken from the operands the program
wrote, which is the difference between `'list' and 'list'` and the truth.

## IR

The fold's non-numeric arm stops being a dead end:

```
walk:                                    ; not both-numbers, not both-texts
  %wst = call i32 @rt_pair_order(i32 0, i32 %ap, i32 %at, i32 %bp, i32 %bt,
                                 i32* %cmp, i32* %badlt, i32* %badrt)
  %wraise = icmp eq i32 %wst, 1
  br i1 %wraise, label %bad, label %wcheck
wcheck:
  %wneither = icmp eq i32 %wst, 2
  br i1 %wneither, label %keep, label %wcmp     ; status 2: the incumbent survives, nobody raises
wcmp:
  %wc = load i32, i32* %cmp
  …                              ; the fold branches on the WORD the ordering wrote
bad:
  %flt = load i32, i32* %badlt
  %frt = load i32, i32* %badrt
  call void @rt_fold_bad(i32 %op, i32 %flt, i32 %frt)   ; the kinds that failed, not the ones declared
```

`%badlt`/`%badrt` start as the operands' own tags and are overwritten by the door that fails — the identity
mechanism that lets a nested failure name its own kinds without the caller knowing the nesting exists. The old arm
compared `%ap` against `%bp` (two handles) and is gone; the only `icmp`s left in `rt_pair_fold` compare the order
word and the identity shortcut, and `TestTheOrderingDoorIsOneHelperTheFoldAsks` fails the module if it starts
comparing an operand it never asked the ordering about. The ordering lives at **one address**: the fold asks it, and
the day the relational roads need it (Gap R.201) they will call the same helper rather than copy a walk.

## Agentic rationale

An agent planning around `min`/`max` needs three different answers, and gets three:

* **what answers** — `programs/probe_a_fold_orders_two_built_containers.gy` is in the conformance corpus as
  `asserted`, `oracle: match`, `conformant: true`, its compiled stdout equal to CPython's on all fifteen lines; the
  machine artifact `integration/conformance-matrix.json` says so, and `tools/recmerge` refuses to write a record for
  a program whose compiled leg disagrees with the reference, so the record leg cannot be seeded from the compiler;
* **what raises** — exit 3, class `TypeError`, message compared **character by character** with the reference's in
  both `pkg/lang` (the record leg) and `integration` (the reference leg), and each raise asserted catchable by the
  program's own `except TypeError:` at exit 0, because a trap the program cannot reach is a refusal with extra steps
  (ADR 0228);
* **what refuses, and why** — the four untaggable shapes (Gap R.198) and the one-word positions (Gap R.146) stay
  exit 1 refusals whose sentences name the missing half and the row that owns it, asserted row by row, with exit 2
  forbidden (ADR 0166).

The record grew by 35 entries and the diff to the record is 191 lines: the recorder inserts new entries rather than
rewriting the file, because a record whose every entry is re-sorted by a tool is a record nobody can review.
Its size is the artifact table's (docs/operations.md) and `pkg/lang/golden_artifact_counts_test.go` recomputes
that number from the file, so this sentence does not carry a count that would rot (roadmap Gap R.204).

## Consequences

* `Gap R.197` closes. `probe_a_fold_orders_two_built_containers.gy` left the debt ledger for
  `conformanceStandalone` and grew from twelve lines to fifteen (the lexicographic arm, the subset arm over sets the
  program built, three raises the program catches). Its old debt row's pins — the partial stdout and `exit status 3`
  — are gone with it, so the compiled leg cannot "pass" by printing less.
* Two defects measured on the way are **filed, not fixed**: `Gap R.201` (`a < b` over two built containers raises
  with `'list' and 'str'` where the reference answers `True`, and `>=` does the same) and `Gap R.202` (`True < [1]`,
  `None < [1]`, `"a" < [1]` each name `'int'` for an operand that is not a number). Both are the same missing pair
  table Gap R.97 and Gap R.101 already name, in the operator road rather than the fold road. They are asserted as
  the reference answers them and as the compiled leg answers them today, so neither can drift or "pass" by moving.
* The fold's ordering is one helper; the relational operators still order containers by their payloads (Gap R.97).
  That is now an inconsistency of *coverage* rather than two divergent implementations of the same walk — the
  helper is the single place the rule lives.
* Every claim above is on two legs: 13 answers, 17 traps and 4 catchability rows in
  `pkg/lang/pair_container_order_test.go` against the record, and 12 parity rows, 16 traps, 4 caught raises and 8
  refusals in `integration/pair_container_order_test.go` against CPython, plus the probe asserted byte-for-byte.
* A third defect, found by pushing a cycle through the new walk, is filed as `Gap R.203`: `xs.append(xs)` then
  `print(min(xs, xs))` leaves the contract's exit 2 (ADR 0166's own-bug code) where the reference prints
  `[[...]]`. Before this cycle that program answered `0` at exit 0 — a silent wrong answer that hid the printer's
  hole; the walk chooses the right winner now and the program dies one statement later. It is asserted where a crash
  can be survived (at the CLI, one process per program) and asserted in the unit process only for the half that is
  the unit's own — that the program is BUILT and carries the ordering. This is the one shape the cycle made worse
  rather than better, and it is written down rather than left out.
* The suite got slower by one probe and one table; nothing that was answered before changed an answer —
  `TestTheOrderingDoorIsOneHelperTheFoldAsks` asserts a fold the compiler settles carries neither door, and the
  ADR 0316 fold tables, the fold probe and the whole `pkg/lang`/`integration` suites pass unedited except where a row
  asserted the old defect.

## Alternatives rejected

* **Compare the two handles.** It makes `min(a, a)` and `max(a, a)` right, which is how it would have shipped: three
  of the twelve probe lines already passed that way. Between two *different* containers it answers a plausible
  verdict at exit 0 — the exact class this row exists to stop.
* **Order lists by length first, elements second.** It is cheap, it is what a runtime that stores a length handy
  reaches for, and it answers `[1, 2] < [3]` as `False` where the reference answers `True`. The reference has no
  length-first rule for lists; it has one for nothing.
* **Fold a set literal in written order and let conformance go green.** The reference's answer depends on the
  objects' hashes, which is why ADR 0314 pins the seed — and why the shape stays a Gap R.198 refusal instead of
  becoming a second language.
* **Rewire the relational operators in the same cycle.** Tempting, since the helper was on the bench. Rejected
  because the operators order through a chain of chains that names kinds from an operand's node type (Gap R.202 is
  that chain's symptom), and replacing it is a second fix with its own measurement — shipping both would have made
  the fold's evidence impossible to read.
* **Copy the walk into the fold.** One `icmp` per kind in `rt_pair_fold` is fewer instructions than a call, and it
  guarantees the fold and the operators diverge the first time one of them is patched.
* **Seed the record from the compiled run.** Faster, and it makes the record a copy of the thing under test; the
  recorder asks the reference and refuses the entry when the compiled leg disagrees.

## References

`pkg/lang/codegen.go` (`pairFoldRuntimeIR`, `rt_pair_order`, `rt_pair_fold`, `containerFoldTag`),
`pkg/lang/pairfold.go` (`foldMayNeedPair`, `foldArgCarriesAContainer`, `foldOperandPair`, `containerFoldTag`),
`pkg/lang/pair_container_order_test.go`, `integration/pair_container_order_test.go`,
`integration/programs/probe_a_fold_orders_two_built_containers.gy`, `integration/conformance_cases.go`,
`tools/recmerge/main.go`, roadmap `Gap R.197` (closed), `Gap R.198`, `Gap R.201`, `Gap R.202`, `L11.1`,
docs/roadmap-details.md#gap-r-197, ADRs 0316, 0248, 0250, 0252, 0259, 0306, 0310, 0182, 0228, 0166, 0314.
