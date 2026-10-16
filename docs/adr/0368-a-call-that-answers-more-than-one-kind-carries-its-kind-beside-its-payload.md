# 0368 — A call that answers more than one kind carries its kind beside its payload

## Context

For most of this backend's life the answer of a call was **one word**, and a table keyed by the
callee's name said what that word meant: `strFuncs` for a text, `floatFuncs` for a double, nothing at
all for a number. That is sound exactly as long as a body answers one kind. It is a wrong answer — at
the exit code of success — the moment it does not:

```python
def f(x):
    if x:
        return "x"
    return 0

print(f(1))
print(f(0))        # x and x here;  the reference prints x and 0
```

`returnsStringExpr`/`methodReturnsStr` set `strFuncs` when **any** reachable return yields text, and
the caller then rendered *every* answer with `rt_print_str`, whose job is to look an integer up in
`@str_tab`. The arm that answered `0` printed `x`. The same missing word answered `7`/`0` for
CPython's `7`/`None` (Gap R.228), `1`/`2` for CPython's `True`/`2`, `3`/`0` for CPython's
`3`/`{'k': 1}`, and `['x', 'x']` for CPython's `['x', 0]`. A **container** arm was worse than wrong: it
reached `value`, lowered a global into an `i32` return word — `ret i32 @.lst1` — and the module `llc`
rejects it, so `def f(x): if x: return "a"` / `    return [1, 2]` died at **exit 2**, the exit ADR 0166
forbids outright.

Measured as a 25-shape probe of mixed-return bodies against a build of the parent commit: **19 of the 25
answers are wrong, and only two of the 19 come with a diagnostic**. The other seventeen printed a number,
a table index or a box handle and exited 0 — the shapes are the ordinary ones —
a text on one arm and a number on the other, a number and the void, a bool and a number, a container
and a number — in `print`, in `str`/`repr`, in an f-string field, in a condition, in a container
literal, bound to a name, and nested in a call.

Two neighbours had already solved halves of this and neither solved the question. The **pair road**
(ADR 0273) carries `(payload, tag)` across a call boundary, but only for a body whose returns are
arithmetic over a pair the caller handed in, and its gate `pairKind` can name two kinds and no more.
The **container table** (`containerReturningFuncs`, ADR 0333) refuses a body whose arms are not all
one container kind, which is correct, and therefore leaves a mixed body on the one-word road with no
record at all.

## Decision

**A call's answer carries the kind of the arm that ran, and the kind is decided once, program-wide,
before any IR exists.**

`pkg/lang/answer_kind.go` is the whole thing: one scan (`answerKindPlanOf`) over the program's AST
answers two questions and nothing else.

1. `carries(fn)` — does this callable hand back more than one kind? If not, the tables keyed by its
   name are correct and this pass does not touch it.
2. `armOf(returnStmt)` — what kind word does *this* `return` road store?

The words are the tag table's own (`TagInt` 0, `TagBool` 2, `TagNone` 3, `TagStr` 4, `TagList` 5,
`TagDict` 6, `TagSet` 7), and the store goes into the module global the pair road already declares
(`@gy_f.anst`, `pairTagSlot`). A function is served by exactly one of the two roads: the scan is
handed the pair road's own answer set and leaves those functions alone, because a *run-time* tag beats
the *constant* one this scan can write.

The three places that behaviour lives:

- **the gates stop guessing** — `textReturningCallables` and `scanStringBindings` ask `answerOwns`
  before writing `strFuncs`, and a carried body gets no entry (Gap R.219's defect is a table entry
  that was a guess about a payload);
- **the `return` road stores the word** — `stmtReturn` asks `armOf` and hands the payload to the road
  that has always produced it, then stores the constant; a bare `return` and the fall-off-the-end
  store `pairTagNone`, which is what turns `7`/`0` into `7`/`None` (Gap R.228);
- **the reading positions ask the pair** — print, `str`/`repr` (`renderPair`), a truth test
  (`truthOperandErr` → `rt_pair_truth`), an f-string field (`filedPairOf`), a container element
  (`isPairElem` → `carriedCallIsPairElem`), and an expression statement, which keeps no word and so
  grants the call permission to run without its tag being read (`grantDiscardedCallAnswer`).

`pairRetDone` is *not* seeded from the plan. It is the pair road's permission for the positions that
serve a pair, and `value`'s gate reads it to refuse any other position that asks a call for one word.
Seeding it made positions that never asked about kinds **stop compiling** programs the reference runs, and
the first attempt at this ADR did exactly that. The exemption (`!g.answer.carries(fnName)` in `value`'s
gate) is what keeps the pass an opt-in for the doors that read a tag. The gate now
carries `!g.answer.carries(fnName)`, and those positions keep the answer they had before this cycle,
wrong on one arm though that is. They are filed as **Gap R.324** with the reference's answer beside
the compiled one, not closed by a refusal nobody measured.

## What this pass will not claim

- **a kind it cannot spell out of the source.** An arm that returns a name, an attribute, a float or
  anything else whose kind is not in the spelling leaves the function on the roads it always had. A
  gate that guesses is worse than a gate that declines: `1` printed for `True` is the bug, `True`
  printed for `True` is the claim.
- **a body that is not an ordinary body** — a generator answers its accumulator handle, an `async def`
  answers a coroutine, a decorated def answers through the decorator's convention, and a method
  answers through `emitClassMethod`, which shares none of the return roads `stmtReturn` walks.
- **anything the pair road already owns**, for the run-time-tag reason above.
- **a position the reading doors do not reach**, which is Gap R.324's five shapes: an answer passed into an
  ordinary parameter, a method's mixed arms, a float arm beside an int arm, a float arm beside a text arm,
  and a conditional `return` whose two arms answer different kinds. Arithmetic and comparison against a
  *literal* were measured and answer correctly — `f(1) + 1` over a text arm raises CPython's own
  `unsupported operand type(s) for +: 'str' and 'int'`, `1 if f(0) == 0 else 0` prints `1`, and
  `pick(1) == "z"` is `True` — so they are not filed, and a row that files what it did not measure is the
  defect this file exists to remove.
- **the equality door keeps its own refusal**, and had to be given it back. The first build of this pass let
  `xs[0] == pick(1)` over `xs = [1, "a"]` compile and print `1` where CPython prints `0`: the door's
  provability question was answered by `strFuncs`, and removing the entry that named `pick` a text had also
  removed the reason it refused. `comparePair` now declines a carried call by name, so that comparison is
  refused in words as it was before this cycle — ADR 0232's collision (an untagged compare answers with
  whichever number happens to sit in the slot) is a worse outcome than a refusal, and
  `TestSlotEqualityRefusesAnUnprovableKind` exists to say so. It is the one place this change made a
  program *less* able to compile, and it is a refusal that was already there.

## Consequences

| shape | reference | before | after |
|---|---|---|---|
| `x`/`0` from a text-and-number body | `x` `0` | `x` `x` | `x` `0` |
| `7`/`None` from a number-and-fall-off body | `7` `None` | `7` `0` | `7` `None` |
| `True`/`2` | `True` `2` | `1` `2` | `True` `2` |
| `3`/`{'k': 1}` | `3` `{'k': 1}` | `3` `0` | `3` `{'k': 1}` |
| `"a"`/`[1, 2]` | `a` `[1, 2]` | **exit 2** — `ret i32 @.lst1`, `llc` rejects the module | `a` `[1, 2]` |
| `str(f(0))`, `repr(f(0))` | `0` `'0'` | `x` `'x'` — the text arm's rendering, twice | `0` `'0'` |
| `print([f(1), f(0)])` | `['x', 0]` | `['x', 'x']` | `['x', 0]` |
| `if f(1):`/`if f(0):` over a text-and-number body | `t1` then `f0` | neither arm — `f(1)` was false too, so only `f0` printed | `t1` `f0` |
| `ys = []` / `ys.append(f(1))` / `print(ys)` | `['a']` | **exit 1** — *cannot put `f(...)` in a compiled list: the compiler cannot prove one kind for it* | `['a']` |
| `s.add(f(1))`, `d["k"] = f(1)`, `ys[0] = f(1)` | `{'a'}`, `{'k': 'a'}`, `['a']` | refused, same sentence | all four answer |
| one-kind body, every shape above | unchanged | unchanged | unchanged |

The four mutation roads came free with the gate: `literalNeedsTags`, `elemsTaggable` and
`taggableMixedDict` ask *"does any element arrive as a register beside its payload?"*, and an answer that
carries its kind is exactly such an element — a gate that did not know the third shape refused the program it
was being asked about (ADR 0249's rule about a gate and a door that disagree).

**12 of the 25 probe shapes changed answer and none regressed** — 13 were already right and stayed right,
which is the direction ADR 0166 asks for and the reason the probe was run against a build of the parent
commit rather than from memory. Of the 7 still wrong, 5 are Gap R.324's (an answer into an ordinary
parameter, a method's mixed arms, a float arm beside an int arm, a float arm beside a text arm, and a
conditional `return`); the other 2 predate this ADR — Gap R.83's unprovable comparison and the pair road's
own one-word refusal — and stay on their own rows.

Two process lessons come with it. The scan answers *to its caller* and is never left in a package
variable — ADR 0343's rule, which the pair scan learned and this one re-learns, and which
`TestAnswerKindPlanIsNotAPackageVariable` keeps. And the fixed point over `return other(...)` is
computed in **rounds**, not in one pass over the source, so a body written above its callee and the
same body written below it get one answer rather than two.

## Machine path

`--emit-llvm` is the discoverable face of the decision: a carried body declares its kind word and every
`return` road stores into it, so an agent can tell a carried answer from a one-word answer without
running the program.

```
@gy_f.anst = internal global i32 0
  store i32 4, i32* @gy_f.anst      ; return "x"   — TagStr
  store i32 0, i32* @gy_f.anst      ; return 0     — TagInt
  store i32 3, i32* @gy_f.anst      ; fall off     — TagNone
  %t11 = load i32, i32* @gy_f.anst  ; the caller asks the tag rather than guessing
```

A one-kind body declares no such global, and the row that asserts that is the negative control that
keeps the pass from widening into a tax on every function in the module. Exit codes are unchanged
(ADR 0006): the shapes above leave the compile door at 0, and Gap R.324's doors stay where they were.

## Alternatives rejected

- **Refuse the mixed shape**, which is Gap R.219's own plan (`register a function as text-returning
  only where every reachable return is text-valued, and refuse the mixed shape`). Rejected once the
  answer could carry its kind: the refusal is unnecessary, and it costs `print(f(0))` — a program the
  reference runs and this backend had been printing, wrongly — its only green leg.
- **A void-specific table and a void-specific printer**, which is Gap R.228's plan. Rejected as
  narrower than the defect: the void is one arm of the same missing record, and building the wider
  one cost nothing extra while closing both rows.
- **Extend the pair road's `pairKind` to name text-versus-number.** Rejected: its gates
  (`pairRetTypeMatches`, `pairCallSiteTypeMatches`) are written for a two-kind world and compare
  `strFuncs` membership, so widening the plan means widening every guard beside it, and the pair road
  would then answer bodies whose tags are constants as though they were run-time facts.
- **Seed `pairRetDone` from the plan** so every position inherits the pair. Rejected by measurement:
  it converts four silent wrong answers into four compile-time refusals, which ADR 0273 forbids.
- **Guess the kind from the callee's annotation or from its call sites.** Rejected outright — the
  table keyed by a name is what caused this, and a second table keyed on looser evidence is the same
  defect with more inputs.

## Where the witnesses live

`pkg/lang/answer_kind_test.go` — the tag table seam, twelve bodies asked of the gate directly, the
fall-off's void, the pair road left alone, the forwarding fixed point in both file orders, and the
run-scoped-plan rule. `integration/mixed_answer_kind_test.go` — fifteen programs pinned to CPython's
own bytes through the CLI with exit 2 forbidden, and the emitted-module reading of the kind word.
`docs/roadmap-details.md` — Gap R.219 and Gap R.228 closed, Gap R.324 filed.
