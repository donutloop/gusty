# ADR 0339: a `for` loop asks its iterable what it is — a container it cannot name is refused, never read as a count

Status: accepted (2026-10-14)
Amends: [ADR 0196](0196-parameter-rebinding-and-loop-counters.md) — the loop's counter road, which ADR 0196 gave to the `range` loop and to nobody else; this row is the question that decides whether a `for` reaches it at all
Extends: [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md) (exit 2 is the compiler's code, and five ordinary programs were paying it), [ADR 0233](0233-every-construct-is-implemented-refused-or-absent-never-answer-wrong.md) (the refusals this row leaves standing name the iterable, the missing half and the answer not taken), [ADR 0318](0318-a-fold-that-orders-containers-walks-their-elements.md) (one ordering: the loop's fold calls the builtin's own sort), [ADR 0323](0323-a-loop-that-picks-its-helper-from-the-compilers-notebook-walks-an-object-the-program-never-made.md) (the loop's helper comes from what its iterable is, and the helper is the runtime's), [ADR 0333](0333-a-container-returned-from-a-call-is-a-value.md) (naming a built container in a rooted slot is how a loop walks one), [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md) (every newly answered program is recorded from the reference, and the CLI leg asks CPython)
Supersedes: nothing
Related: roadmap L11.1 (the tagged value word, which owns the mixed-element and callable-argument halves this row leaves refused), Gap R.246 (closes), **Gap R.247, Gap R.248, Gap R.249, Gap R.250, Gap R.251, Gap R.253 (newly measured here, filed here)**, Gap R.252 (measured here; its untagged half is paid by this ADR, its tagged half stays open), Gap R.146 (the tagged-element refusal several of the refusals quote), Gap I.2 (the interned-text element record the container road reads when it claims a loop variable is text), Gap R.67 (the container-carrying call that Gap R.253 needs)
Owner: `pkg/lang/for_iterable.go`, `pkg/lang/codegen.go` (`case *ForStmt:` and `builtinSorted`'s literal branch), `pkg/lang/for_iterable_test.go`, `integration/for_iterable_test.go`, `integration/programs/for_iterables.gy`

## Context

Five shapes of one program, and two answers, neither of them the reference's:

```python
for v in sorted([3,1,2]):                print(v)   # 1 2 3      this backend: EXIT 2, llc rejects the module
for v in reversed([1,2,3]):              print(v)   # 3 2 1      this backend: EXIT 2
for v in sorted([]):                     print(v)   # (nothing)  this backend: EXIT 2
for v in sorted([3,1,2], reverse=True):  print(v)   # 3 2 1      this backend: EXIT 2
for v in sorted([3,1,2][0:2]):           print(v)   # 1 3        this backend: 0        at exit 0
for v in sorted(["b","a"]):              print(v)   # a b        this backend: nothing  at exit 0
for v in reversed("abc"):                print(v)   # c b a      this backend: nothing  at exit 0
xs = [3,1,2]
for v in sorted(xs):                     print(v)   # 1 2 3      this backend: 0        at exit 0
def b(): return [3,1,2]
for v in sorted(b()):                    print(v)   # 1 2 3      this backend: nothing  at exit 0
```

Four of them never got as far as running. `llc-20` wrote out the module it had been handed and refused to
assemble it, on one instruction that says the whole story:

```
global variable reference must have pointer type: %t1 = icmp slt i32 %_ctr1.ld1, @.lst1
```

The loop's counter had been told to count up to the **address of a list's global**. Exit 2 is the code the
contract reserves for a module the compiler wrote and LLVM refused (ADR 0166) — so an ordinary loop over a
sorted list was, on the record, a compiler bug, five programs deep in the most ordinary construct in the
language.

The rest were worse, because they *succeeded*. `case *ForStmt:` has three roads and no question in front of
them: the unrolled literal road (a `*ListLit` or a `*StrLit` standing where the iterable goes), the runtime
container road (a name, a set or dict literal, or whatever the container pair vouches for), and the numeric
counter road for `range(...)` and this language's `for x in <integer>` repeat count. A container that none of
them claimed fell to the counter road, which asks the iterable for a **bound**; given a container it receives a
word, the comparison runs, the loop variable is bound to the **counter**, and a loop whose body prints its
variable prints whatever that word was. `for v in sorted([3, 1, 2][0:2])` printed `0` where CPython prints `1`
and `3`; `for v in sorted(["b", "a"])` and `for v in reversed("abc")` printed nothing at all, at the exit code
that means the program ran.

The tell is that the *expression* road had these calls all along: `print(sorted([3, 1, 2]))` prints `[1, 2, 3]`
and `print(sorted(xs))` sorts a real heap list with `@rt_sort`. The builtin was fine. The loop never asked what
it was about to step over.

## Decision

`case *ForStmt:` asks its iterable, at the top of the case, before any road is chosen — three questions, in this
order, because each is narrower and cheaper than the next, and the last one is the door every `for` has always
gone through.

**1. Can the compiler write the container down?** `foldedForIterable` rewrites `for v in sorted([3, 1, 2])` into
the literal list the expression denotes — `[1, 2, 3]` — and the loop then walks it down the unrolled road
`for v in [1, 2, 3]` already uses. The fold applies the operations **in order, one layer at a time**, so
`reversed(sorted([1, 3, 2]))` is `3, 2, 1`; unwrapping to the innermost literal (which is what `listCallElems`
does for the `len`/`sum` folds, where order does not matter) would answer `2, 3, 1` — measured against CPython
before the difference was accepted. Empty is an answer, not a refusal: `for v in sorted([])` steps over nothing,
which is what `for v in []` already printed.

**2. Can the compiler name the container the builtin denotes?** `materializeSortedReversedForLoop` builds the
copy — `@rt_sort` for the order, `@rt_list_copy` + `@rt_reverse` for the flip — roots it in a slot, records its
element kind in the same notebooks a `xs = [...]` assignment fills, and hands the loop a *name*. The loop then
walks it down the road it has always used for a list variable: `rt_list_len`, `rt_get_elem`, and the loop
variable claimed from that notebook rather than from a guess.

**3. Otherwise the container door beside it** (ADR 0333, for the container a user function built), and below
that, the counter road — which no `sorted`/`reversed` reaches any more, because the guard beside it refuses
first.

Three rules held the row in place.

*One ordering, one implementation.* The fold calls `sortedLiteralElems` — the same function `builtinSorted`'s
literal branch calls — so the order a program sees when it prints a sorted list and the order it sees when it
steps over one are written in one place (ADR 0318's rule, that a fold which orders containers asks their
elements and does not read an order out of a handle).
That test deliberately does **not** pair `reversed` the way it pairs `sorted`: the expression form of `reversed`
answers its argument unreversed (Gap R.251), and sharing an ordering with a road that does not order would
certify the bug rather than catch it.

*Neither door claims an element kind it cannot see.* The gate is `forIterSortedElemKind`: integers and interned
text, all of one or all of the other, visible from a literal's elements, a list variable's notebook, a numeric
slice or a single-armed container answer. A float list, a mixed list, a dict, a set, a text, a range and a
callable argument fail the gate and are refused by name — Gap R.247, R.248, R.249, R.250. Before this ADR those
same shapes were exit-2 modules or silent exit-0 answers, so a refusal is not a regression against them, and the
`@rt_sort` comparator is chosen from an argument's *spelling*, which is why text through a slice or a call is
refused rather than sorted by arrival order and printed as if it were ordered.

*A refusal is chosen, not defaulted to.* A `for` whose body prints the loop variable answers at exit 0 whether
it walked anything or not. That is why the guard sits immediately above the counter road rather than beside the
fold: the fall-through is the failure mode, and the sentence says so (`Reading its word as a repeat count would
answer at exit 0 with a number the program never wrote, which is the answer this contract ranks worst`).

## The measurement that narrowed this ADR

The first version opened the second door through the container **pair**: arms for `sorted`/`reversed` in
`isContainerExpr`, `staticContainerKind` and `containerHandleOf`, on ADR 0333's reasoning that the roads asking
"may I treat this as a container" and "give me its handle" must get one answer. It was tidier, and it broke
`print(1 in sorted(xs))`, which answers `True` today — the membership test asks the pair the first question and
then reaches for `containerOperand`, whose arm these builtins did not get. A working answer became a refusal,
which ADR 0233 forbids, and the cause was counting a two-part pair where the road set has three members.

The arms came out. The row is now entirely local to the loop's own case plus `for_iterable.go`, the pair is
untouched, and the widening is filed as **Gap R.253** with this measurement as its evidence. The general lesson
is the ADR's own: *before widening a shared predicate, list every road that asks it — not the ones you
remember.* The tagged-loop-variable leak was caught the same way (`for v in sorted(words)` then
`for v in [7, 8]` printed `(null)` twice, and the same variable used as a number met Gap R.146's sentence), and
it is why door 2 names the copy instead of handing over a bare handle.

## What the loop also learned to do (Gap R.252's untagged half)

Asking the iterable per loop means *un-asking* what the previous loop answered. The container road set
`g.internedVars[loopVar]` when its iterable held text and nothing took the claim back, so

```python
words = ["b", "a"]
xs = [1, 2]
for v in words: print(v)     # b, a
for v in xs:    print(v)     # this backend: a, (null)     CPython: 1, 2
```

printed the first loop's answer through the second loop's variable, and `for v in range(2)` after a text loop
printed the string table. Both roads now claim from their own iterable and delete otherwise; the counter road
takes the claim back too, because a counter is a number under no reading. The tagged half (a mixed loop element
leaving the name a (payload, tag) pair for the rest of the function) stays open as Gap R.252.

## Consequences

* Five programs move from exit 2 to the reference's answer, and five from a silent exit-0 answer to the
  reference's answer. Nothing that answered before changes its answer: `for v in [1, 2, 3]`, `for c in "abc"`,
  `for i in range(3)`, this language's `for i in <integer>` repeat-count deviation, a set literal, a dict's keys,
  a generator's answer, a user function's container answer and a program that defines `sorted` itself are
  asserted unchanged, in the same table as the new rows.
* The `sorted` builtin and the loop now share one comparator and one ordering. A change to one is a change to
  both, which is the point, and the test that says so reads the emitted module (`call void @rt_sort`,
  `call void @rt_reverse`).
* `builtinSorted`'s literal branch lost its inline sort and calls the shared fold. Its own answers are unchanged
  (`print(sorted(...))`, `len(sorted(...))`, `sum(sorted(...))` are asserted), which is what makes the sharing
  safe rather than hopeful.
* The refusals are load-bearing and pinned: `sorted` of a text/dict/set/range, `sorted` with `key=` or a
  positional key, `sorted` of a call the scan cannot see, and a float list. Each pins today's sentence and the
  reference's answer beside it, so closing a filed row means moving a pinned expectation, and a row that quietly
  starts answering is caught as surely as one that regresses.
* Exit 2 is unreachable for this family: the guard is asserted never to let one of these programs reach the
  counter road, and the counter-vs-container-global instruction is asserted absent from every module the suite
  compiles.

## Agentic rationale, and the shape of the IR

- **The refusal is the machine path.** `gustyc --json --eval 'xs = [1.5, 0.5]\nfor v in sorted(xs):\n    print(v)'`
  answers `{"ok": false, "phase": "codegen", "error": "jit: codegen: `for` over sorted(xs) is refused: … the ELEMENT
  kind of that list is not something this backend's roads print from one word … (roadmap Gap R.248) … (roadmap
  L11.1, ADR 0339)", "exit": 1}` — one sentence naming the builtin, the argument, the half that is missing and the
  row that owns it, on the field `docs/operations.md` § JSON documents. An agent does not have to guess whether the
  loop sorted, crashed or stepped over nothing — and the two doors' refusals stay distinguishable, the builtin's own
  `key=`/`reverse=` sentence (`roadmap L11.7`) versus the loop's `repeat count` sentence, because an agent that
  cannot tell which door refused cannot plan around it.
- **The crash class is banned by shape, not by value.** `TestTheLoopNeverComparesItsCounterWithAContainer` (and its
  CLI twin) rejects any `icmp <pred> i32 %ctr…, @.lst/@.set/@.dict` instruction in any program the suite compiles.
  A future road cannot re-introduce the exit-2 module by answering a value correctly, because the gate reads the
  module, not the answer. The positive half of the same test asserts `call void @rt_sort` /
  `call void @rt_reverse` are present for the six programs whose order comes from the runtime, so "the loop stopped
  calling the helper" fails too.
- **The witnesses are 202 programs and 6591 records.** The conformance matrix gained
  `integration/programs/for_iterables.gy` (its expectations generated from `/usr/bin/python3`, compared line for
  line), and the record gained the 50 sources this family never had — zero matrix coverage is precisely how a road
  that ignored `sorted` printed the record's own answer at exit 0.

## Alternatives rejected

*Answer the rest and let the fall-through stand.* The counter road is reachable today by any iterable no road
claimed; leaving it reachable means every future container-shaped expression is one missing arm away from
printing the counter at exit 0. Rejected: this is the class the contract ranks worst, and it was the actual
behaviour, not a hypothetical.

*Widen the container pair (the version this ADR first shipped).* Rejected for the measured reason above — it
turned `1 in sorted(xs)` from `True` into a refusal — and filed as Gap R.253 to be taken again with
`containerOperand` in hand.

*Reuse `listCallElems` for the fold.* It is the existing unwrapper for `sorted`/`reversed` and it is right for
`len`/`sum`/`min`/`max`, where the order of the elements does not matter. For a loop it is wrong: it flattens the
operations to the innermost literal, and `reversed(sorted([1, 3, 2]))` came out `2, 3, 1` against CPython's
`3, 2, 1`. Rejected with the measurement in the row's test table.

*Let the loop tag its variable the way the generic handle arm does.* Correct for a slot whose element kind is
unknown, wrong for a copy whose kind was looked up before building it: the tag stuck to the name for the rest of
the function. Rejected; naming the copy and using the list-variable road is what ADR 0333 does for the same
shape of problem.

*Refuse everything the fold does not cover.* Cheaper, and it would have made `for v in sorted(xs)` — the shape a
beginner writes — a refusal beside a `print(sorted(xs))` that answers. Rejected: ADR 0233's rule is that a
refusal is the answer only where the door is genuinely missing, and here the runtime road already existed.

## Witnesses

`pkg/lang/for_iterable_test.go`: 23 answered rows run on the compiled leg, the JIT and the record; 5 refused
rows assert their sentence and never exit 2; 21 unchanged rows are the negative control; the ordering-sharing
test asks `print` and the loop for the same sequence; the IR test asserts the counter-vs-container-global
instruction is absent from every module the suite compiles; and the runtime-road test asserts
`@rt_sort`/`@rt_reverse` are present. `integration/for_iterable_test.go` runs 28 answered and 9 refused programs
through the CLI against the pinned interpreter, re-reads every `oracle` column from CPython rather than trusting
it, asserts the counter ban and the runtime helpers through `--emit-llvm`, and runs
`integration/programs/for_iterables.gy` line-for-line against the recorded reference answer. The 50 newly
answered sources are on the record (ADR 0302); the refused shapes are not recordable and live in the two test
tables instead.
