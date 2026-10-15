# ADR 0357 — A comprehension walks what a call answers, and refuses what nothing can prove

Status: accepted
Date: 2026-10-16
Depends on: [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md), [ADR 0333](0333-a-container-returned-from-a-call-is-a-value.md), [ADR 0192](0192-a-comprehension-builds-its-list.md), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md), [ADR 0249](0249-a-slot-read-through-an-index-the-program-computes-is-asked-of-the-tag.md), [ADR 0356](0356-an-f-string-field-is-a-print-in-disguise-one-subscript-door-one-printer-and-a-re.md), [ADR 0188](0188-a-printed-container-is-a-container.md)
Roadmap rows: Gap R.291 (paid), Gap R.293, Gap R.296, Gap R.298, Gap R.299

## The gap, as measured

A comprehension has two routes: an iterable the compiler can enumerate is **unrolled** at compile time, and one
it cannot is **walked** by a runtime loop. The dispatcher decided between them by asking whether the iterable was
a call, and if it was, it handed it to the unroller. The unroller understands an inline list literal and
`range(start, stop[, step])` with constant bounds — and to read a `range` it opens `Args[0]`:

```gusty
def g():
    return [1, 2, 3]

print([x for x in g()])
# panic: runtime error: index out of range [0] with length 0
#   lang.(*irGen).compItems  pkg/lang/codegen.go:13050
#   lang.(*irGen).comp       pkg/lang/codegen.go:13242
# exit 2
```

Three measurements from the binary at HEAD, before any of this was written:

| program | HEAD | CPython |
|---|---|---|
| `def g(): return [1,2,3]` / `print([x for x in g()])` | **exit 2**, `panic: index out of range [0] with length 0` | `[1, 2, 3]` |
| `def z(): return {"a": 1}` / `print([k for k in z()])` | **exit 2**, same panic | `['a']` |
| `def h(n): return [n*n]` / `print([x for x in h(3)])` | `[0, 1, 2]` at exit 0 | `[9]` |
| `def h(n): return n + 1` / `print([x for x in h(3)])` | `[0, 1, 2]` at exit 0 | `TypeError: 'int' object is not iterable` |
| `print([x for x in sorted([3,1,2])])` | refused: `range bound must be a constant` | `[1, 2, 3]` |

The panic is the loud half. The quiet half is the third and fourth rows, and they are the reason this is a
decision rather than a nil check.

## The rule that produced a right answer for the wrong reason

Because any call was handed to the unroller, and the unroller only knows `range`, it read **the call's own
argument as the loop bound**: `[x for x in h(3)]` became `range(3)` whichever `h` returned. `[0, 1, 2]` came out
for a callee returning `range(n)`, for one returning `[n*n]` (where the reference gives `[9]`), and for one
returning `n + 1` (where the reference raises `TypeError`).

That matters because of the ladder this project ranks its failures by: an answer may not be traded for a refusal.
A rule that reads the wrong thing and prints the right digits for one input is not an answer, and the two
controls above are how that was established rather than argued — the same call, the same position, differing only
in what the callee answers, is where a coincidence shows itself.

## The decision

**1. Only `range(...)` with at least one argument has a compile-time length** — `compIterIsStaticRange` in
`pkg/lang/codegen.go`. A call to any other name, and a `range` with no arguments at all, is not enumerable here.
`compItems` asks the same predicate on its own entry, so a caller that routes badly gets a refusal instead of an
index panic: the derivation's contract is now enforced at the door it guards (the `nil`-check alternative is in
the rejected list precisely because it leaves the routing wrong and the crash merely rarer).

**2. Every other call goes to the runtime loop** — `isCallExpr` joins the conditions that select
`runtimeCompLoop`, beside the container-variable and typed-container conditions that were already there. The
shape that motivated the row:

```gusty
xs = [1, 2, 3]
print([x for x in xs])      # HEAD: refused — "xs is a list the compiler kept as a compile-time constant"
                            # now:  [1, 2, 3]      (Gap R.289's walk is what lets the iterable count as a read)
```

**3. The loop may not walk an object nothing proved exists** — before lowering, a call iterable goes through
`normaliseCompIterable`, which asks the `for` road's three questions in the `for` road's order: fold it if the
compiler already holds every element (`foldedForIterable`), materialise a `sorted`/`reversed` over a walkable
list (`materializeSortedReversedForLoop`), or read a proven container answer into a name (`containerAnswerOperandRead`).
If none applies, the build stops with the sentence the container-handle door already uses (Gap R.67, ADR 0333):

> `*lang.Call` does not name a container the compiled backend can prove, so the comprehension walking it has
> nothing to measure; a call answers a container only where the scan can see every arm of the callee answer one

The refusal is not decoration. Without the gate, routing a call to the runtime loop **created a new wrong
answer**: `def h(n): return range(n)` / `[x for x in h(3)]` lowered the call to the one-word road, took `0` back,
asked `rt_list_len` of it, and walked nothing — printing `[]` at exit 0 where the reference prints `[0, 1, 2]`.
Walking an object that was never proven to be a container is the same mistake the unroller made, one stage later.

**4. The permission is one function** — `(*irGen).callAnswersContainer` in `pkg/lang/heapargs.go` is the
predicate `containerHandleOf`'s `*Call` arm had inlined; the loop and the arm now ask the same function, in the
ADR 0356 manner. Two copies of a permission test is how the two roads came to disagree about the same object.

## What the row leaves on the tracker, and how it is pinned

* **Gap R.298** — `normaliseCompIterable` sits *beside* the `for` road's own sequence rather than replacing it.
  This is the exact shape ADR 0356 just closed for a subscript, and the merge was left out to keep this commit to
  one feature. The rows that would have been the merge's regression set already exist.
* **Gap R.299** — the `for` road still reaches `rangeBounds` with a call's own argument and never asks the callee
  what it answers, so `for v in h(2)` prints `0`/`1` for an int-returning `h`. That is the comprehension's
  removed bug, still live one road over, and it is pinned as a measurement in the record so the fix inherits the
  pair of controls rather than re-deriving them.
* **Gap R.293** — a `range` is not a value in this backend at all (`print(h(3))` prints `3`, not `range(0, 3)`),
  which is *why* the accident worked and why the honest answer for that one input is a refusal today.
* **Gap R.296** — a generator argument is refused by the grammar in its bare spelling and by the door in its
  parenthesised one; the two refusals are pinned side by side in
  `pkg/lang/dead_binding_test.go::TestAReadCountsEvenWhereTheBackendHasNoAnswerYet`, a table whose rows assert the
  *refusal sentence* as well as the read, so a fix cannot close a row by rewriting its message.

## The machine path

No flag, no schema field, no exit code moved. What changed for a machine consumer is the *content* of two
diagnostics: the exit-2 panic is gone (a crash has no `--json` payload at all, so an agent had nothing to read),
and the refusal carries the same sentence the container-handle door has been giving, which means one string can
be matched against two roads. `gustyc --verify` and `opt -passes=verify` pass on every module the new rows emit,
and the answered rows are checked by the same link-and-run path as any other program.

## Alternatives rejected

* **Guard the `Args[0]` index and leave the dispatcher alone.** One line, and it removes the crash. It leaves
  every one-argument call being read as `range(its own argument)` — i.e. it keeps the third and fourth rows of the
  table wrong, and the crash merely rarer. Rejected because the row is about the rule, not the panic.
* **Answer `[x for x in h(3)]` by scanning the callee for a `range` arm and substituting the call's arguments for
  its parameters.** It is the shape that would restore that one row's answer, and it is the same shape as the bug:
  a loop bound inferred from a call's arguments instead of from the object the callee answers. It belongs to
  Gap R.293 (a `range` as a real object), where `print`, `len`, `in`, `for` and the comprehension would all ask
  that object the same question. Doing it here would have bought one row and pre-committed the language to the
  accident.
* **Let the loop walk whatever `g.value` returned and trust `rt_list_len`.** This is what produced `[]` during
  the work, at exit 0, for a program whose reference answer is `[0, 1, 2]`. An empty container and an unexamined
  object are indistinguishable at the call site, so the check has to happen where the type is known — in the
  compiler.
* **Refuse all call iterables.** `print([x for x in sorted(xs)])` and `print([x for x in g()])` answer today; a
  blanket refusal trades four measured answers for one tidy rule, and the ladder puts that direction below a
  refusal of its own. `sorted` over a literal and over a variable are pinned as the regression guard against
  exactly this.
* **Merge the `for` road's normalisation in the same commit.** Right destination, wrong commit: it would have put
  a behaviour change to the `for` road — which has its own rows and its own wrong answers (Gap R.299) — inside a
  comprehension fix. Filed as Gap R.298 instead, with the reason in the cell.

## What was witnessed

Reference leg and compiler, in `pkg/lang/comp_iterable_route_test.go`: ten answered rows (the two zero-argument
calls that panicked, a call reading its own argument, a square-returning call, `sorted` over a literal and over a
variable, a container variable with and without a condition, and the two shapes that must still *unroll* rather
than loop — `range(3)` and an inline list), each checked against CPython and against a pinned expectation; two
refusal rows that require the sentence to name the missing proof; ten predicate rows for
`compIterIsStaticRange` — including the zero-argument `range`, a `range` whose stop is a name, a method-shaped
call, and a non-call — because a gate tested only through programs that already reach it proves nothing about the
calls it turns away; and `TestCompItemsRefusesAZeroArgumentCallRatherThanReadingItsFirstArgument`, which recovers
in a `defer` so the exit-2 class has a case that fails if it returns. The crash regression is also witnessed
through `Compile` rather than the CLI, so a panic is caught as a test failure and not as a lost subprocess.
