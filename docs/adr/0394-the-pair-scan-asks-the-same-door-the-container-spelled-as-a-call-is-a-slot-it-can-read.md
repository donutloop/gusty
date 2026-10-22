# The pair scan asks the same door: the container spelled as a call is a slot it can read

**Roadmap:** Gap R.369 (closed here — the read of a container literal's slot, where the slot was filled by
`list()` / `set()` / `dict()`, crossed a call argument as its heap handle) ·
**ADR 0391** (the BUILDER: `elemKindTag` asks `(*irGen).emptyContainerCall`, so the slot is written with a tag) ·
**ADR 0392** (the DECIDING roads: `staticContainerKind`, `literalNeedsHeap`, `literalNeedsTags` ask the same
door, so the object is built self-describing at all) ·
**ADR 0389** (the argument that crosses as a pair — this ADR opens a parameter that ADR 0389 closed) ·
**ADR 0393** (the condition that spends the tag the pair carries; the door that made this row's wrong answer
visible, because it prints `f` for a value the print door had been printing as `3`) ·
**ADR 0232** (a question has one asking place — the disease this is the third patient of) ·
Gap R.360 / **ADR 0391** (the shadowing door this one copies, `(*irGen).builtinShadowed`) ·
Gap R.81 (unhashable members — the reason a KEY keeps the strict question) ·
Gap R.82 (the mixed-list refusal, which is what `xs = [set()]` printed instead of an answer) ·
Gap R.220 (the pair that crosses) · L11.1 (the tagged value word) ·
ADR 0166 (a wrong answer at exit 0 is the defect this repo files rather than ships) ·
Gap R.370 – Gap R.374 (the residue, filed with today's bytes)

## Status

Accepted. Landed 2026-10-22.

## Context

Three rows in a row have been the same program with one word changed.

```python
def f(v):
    print(v)
    if v:
        print("t")
    else:
        print("f")

d = {"k": []}      # ADR 0391 + ADR 0392 + ADR 0393: prints [] and takes the else arm — right
d = {"k": list()}  # this ADR:               prints 3 and takes the then  arm — wrong, exit 0
```

`print(list())` standing alone had been right since ADR 0392. `f(d["k"])` with the literal spelling had been
right since ADR 0389 gave the read of a self-describing slot its pair. Put together it looked like the store
was the hole: ADR 0391's filing said as much, and the row's `Item` cell still says it. It was not the store.
The store asks `elemKindTag`, which asks `(*irGen).emptyContainerCall`, which had been taught the call
spelling in ADR 0391 — the entry *was* being written with the tag of the container the call spells.

The hole was in the scan. `pkg/lang/paircall.go` answers, once per program, "which (function, parameter)
positions take the pair?" One of the predicates on that road is

```go
// can the builder write this slot from its spelling alone?
func pairSlotIsSpelling(e Expr) bool { … *IntLit, *FloatLit, *StrLit, *BoolLit, *NoneLit, *ListLit, *SetLit, *DictLit … }
```

a hand-written list of AST node types. A `*Call` is not on it. So `s.readOfLiteralSlots(d["k"])` — the
question "is this argument a read of a literal container's slot, which is a read whose value needs a kind to
mean anything?" — answered **no** for an entry the program spelled `list()`, the position was recorded as
*closed*, and the callee received the ordinary road's one word: the handle of the empty list, printed as `3`
and tested against zero as `t`.

That list is the third hand-written copy of one question. ADR 0391 put the door in the builder's copy, ADR
0392 put it in the deciding roads' copy, and both noted — in their own text — that the emitter's
`emptyContainerCall` is "one of several spelling tests in this backend". This is the third, and the last one
that decided an answer.

## Decision

**The scan's value/member half of the slot question asks the emitter's door.**

`pairSlotIsSpelling` keeps its name and keeps its strictness, and is demoted to what it actually was always
asking: the **KEY** question. Two new methods on `pairScan` sit beside it:

- **`valueSlotIsSpelling(e Expr) bool`** — the door for a dict *value* and a list/set *member*, and for the
  same shapes nested. It recurses through `*ListLit` / `*SetLit` / `*DictLit` (a nested dict's **keys** stay
  the strict `pairSlotIsSpelling` question, see below), falls through to `pairSlotIsSpelling` for everything
  that already qualified, and adds the one arm the list was missing: a `*Call` whose callee is `list`, `set`
  or `dict`, with no arguments, **and which the program has not taken the name of**.
- **`programDefinesName(name string) bool`** — the one fact the scan cannot borrow. `(*irGen).emptyContainerCall`
  asks `(*irGen).builtinShadowed`, a method on a generator the scan has no handle on: `pairCallScan` is a
  function over the AST, built once per `Compile`, with no `irGen` to reach. So the scan carries its own
  census, `stmtsAreNamed`, asking exactly one question and no more: does this program introduce the name with
  a `def` or a `class`, in any frame, at any depth? `pairScan.stmts` is `prog.Stmts`, captured at construction.

`dictSlotsCarryKinds` and `listSlotsCarryKinds` — the two places that asked `pairSlotIsSpelling` on both
sides of a container — now ask the two questions they mean: keys the strict one, values and members the door.

**Why the key keeps the strict question.** A value in a slot is a thing the callee might be told the kind of;
a key in a slot is a thing that has to be *hashed*. `s = {list()}` is not a missing tag, it is
`TypeError: unhashable type: 'list'`, and when that raises is Gap R.81's decision, not this pass's. Opening a
parameter for a program the reference refuses before the read would put this backend in the business of
answering questions the language does not ask — the same reason ADR 0353 made a key's kind the door's own
business. The strict key arm is therefore a gate, and `TestTheEmptyContainerArgumentRoadDeclinesWhatItCannotPromise`
asserts the gate by asserting the *absence* of the pair road, not just the absence of a wrong answer.

**Why the census asks exactly what `builtinShadowed` asks, and not more.** The two failure directions are not
symmetric. Declining the door costs this family an answer it did not yet have. Opening it on a name the
program took over labels an arbitrary payload as a container — a tag that names nothing, which is the defect
class Gap R.360 exists to keep shut. So `programDefinesName` errs toward *not* declining, and copies its
source door rather than inventing a stricter one. That was measured, not assumed: the first version of the
census counted *every* binding form (a `for` variable, a function's own parameter) as a write of the name, and
this program printed `3` while the door above it kept labelling the entry a container —

```python
def g(list):        # `list` here is g's own binding, not the module's
    return list

y = g(4)

def f(v):
    print(v)

d = {"k": list()}
f(d["k"])           # census too wide: prints 3   —  now prints []
```

A gate stricter than the door it copies is not "safe", it is a second door that disagrees with the first.

## What moved, measured

46 probes — the whole family (three spellings × dict entry / list member / nested / mixed with ordinary
slots, plus the shadowing controls and the ADR 0393 ladders that mention a call spelling), each asked of
CPython first, then of a build of the commit before this row and a build of this one:

| | agreeing with the reference |
|---|---|
| before (HEAD, `b7af74d`) | **7** |
| after | **34** |
| newly agreeing | **27** |
| regressed | **0** |

The 12 that still disagree are the residue table, each row naming its owner: the two module-level truth rows
(Gap R.370), the five value-position rows (Gap R.371), the loop-variable key (Gap R.372), the method named
`set` (Gap R.373), the unhashable member (Gap R.81), and the two callee-layout rows (Gap R.374). Gap R.374 is
worth reading twice: `f([1], 2)` / `f(3, 4)` prints `[] 4` on the build **before** this row as well, so the
layout drift is the pair road's own — what this ADR changes is how easy it is to stand on, which is why it is
filed here and not smoothed over.

## The IR, asserted

`TestTheEmptyContainerCallArgumentIsWrittenWithItsKind` reads the module rather than the answer, because the
first heap slot of a program is often address `3` and a table of right answers can be passed by a compiler
that got them by luck:

- the store carries the tag of the container the CALL spells — `@rt_dict_put_tagged(i32 %…, i32 4, i32 5)` for
  `list()`, `i32 6` for `dict()`, `i32 7` for `set()`, the words of ADR 0182's one table;
- the callee's `define` carries the second word — `define i32 @gy_f(i32 %p0, i32 %q0)` — and the body spends
  it: `%_v_tag = alloca i32` plus `@rt_print_mixed_value` for the print door, `@rt_pair_truth` for the truth
  door;
- the *caller's* `@main` is where the read is lowered, so `@rt_dict_get_tagged` / `@rt_dict_value_tag` are
  asserted there and not in the callee — a claim about a missing ask read out of the wrong region is a claim
  about nothing;
- and the negative control: `d = {"a": 7}` has a payload that **is** its number, so the callee must NOT grow
  `%_v_tag` and must keep the `icmp ne i32 %p0, 0` it has always had.

## Agentic path

No new flag, no new schema field: the change is one more family of programs moving from "refused or wrong" to
"answered", on the roads `--eval` / `--file` / `--repl` / `--emit-llvm` already expose. What an agent gets is
the machine-readable half of this ADR's honesty: `--emit-llvm` on a declined program shows a callee with one
parameter word and no tag alloca, which is how the residue pins above are asserted mechanically rather than by
eyeball. `gustyc --file integration/programs/empty_container_argument.gy` prints CPython's 55 lines byte for
byte on both `--jit` and `--aot`, and the program is on the record.

## Alternatives rejected

1. **Teach the dict-literal store road to call `(*irGen).emptyContainerCall` directly.** The store already
   asks it, through `elemKindTag` — that was ADR 0391's change, and it is why the entry's tag is correct in
   the IR above. The row's own filing guessed this and would have produced a second fix for a hole that was
   not there. Rejected: the store was not the hole.
2. **Widen `pairSlotIsSpelling` itself to accept any zero-argument `*Call`.** The function answers two
   questions at once (a key's and a value's) and would open parameters for `f({"k": foo()})` where `foo` is
   the program's own function — a payload with no spelling and no tag, which is the shape ADR 0387's
   "a spelling is not a kind" exists to keep off the road. Rejected: split the door instead of widening the
   merge (`valueSlotIsSpelling` for values, `pairSlotIsSpelling` for keys).
3. **Give `pairCallScan` an `irGen` to ask.** The scan is built once per `Compile` and runs over ASTs from
   several modules; handing it a generator means handing it *a* module's state and reading another module's
   facts out of it — the exact bug class ADR 0343 removed when it took the package-level `closed` set out of
   the scan. Rejected: carry the one question (`programDefinesName`) rather than the whole object.
4. **A conservative census: count every binding of a name as taking it.** Rejected by measurement, above:
   it regressed a program whose `list` was only another function's parameter, and a decline that the emitter
   contradicts is worse than no decline.
5. **Say `list()` is not a literal, so the scan is right and the program should be refused.** The reference
   answers it, the print door answers it, and Gap R.82's mixed-list refusal is what `xs = [set()]` printed
   for — a refusal in one road does not license a wrong answer in another.

## Consequences

- **Three copies of one question, one door.** `elemKindTag` (ADR 0391), the deciding roads (ADR 0392), the
  scan (this ADR). The remaining copies are the two that *should* stay separate — `keyIsSpelling` (a key is
  hashed, Gap R.81) and the emitter's `emptyContainerLiteral` (which is the shape, not the permission).
- **The scan now knows about the program's definitions**, which it did not before. `stmtsAreNamed` is a
  statement-form walk: a new statement form that can hold a `def` has to be added to it, the same standing
  obligation `stmtsRebindName` carries. That is a maintenance debt this ADR takes on deliberately, in the
  direction that fails loud — an unrecognised form counts as "the program might define anything", which
  declines the door rather than lying about a tag.
- **Gap R.374 is now easy to hit.** A callee whose second word is a tag has always been mis-emit-able when
  one call site needs it and another does not. More programs reach that road now. The row is in the queue at
  number 209 with the reproducer and the scan data structure (`calls[fn][i]`) that already holds what a fix
  needs.
- The retired engine's record grows by 28 sources (27 answer rows + the conformance program), all recorded
  from the reference with the compiled leg cross-checked; the two ADR 0393 residue pins that this row closes
  are deleted from both legs, not rewritten.
