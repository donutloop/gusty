# ADR 0475 — an answer whose kind lives in a table crosses a call boundary as a box, and a ternary arm whose kind is a folded global crosses as a heap object

**Status:** Accepted
**Date:** 2026-11-02
**Decides:** the exit-2 half of roadmap **Gap R.581** (a call whose answer carries a kind loses the tag in the CALL-ARGUMENT position, and one shape of it reaches **exit 2**) and **Gap R.584** (a `return` whose arm is a container literal puts the compile-time global in the ternary's `phi`, and `llc` rejects the module)
**Files:** **Gap R.585** (a tuple arm of a `return` answers the int road where the list, dict and set arms now answer — `w = f(1)` / `print(w)` writes `0` for CPython's `(1, 2)` at **exit 0**)
**Keeps open:** **Gap R.581**'s tag half (a text, a verdict, a `None` and a container answer still cross a parameter as a payload alone), **Gap R.146** (the one-word position — the boxed float at a condition refuses here where it used to reach `llc`), **Gap R.171** (`str()` of a forwarded answer), **Gap R.324** (the six one-word positions; this is their call-argument twin), **Gap R.344** / **Gap R.102** (the arms that do not agree on a word), **Gap R.67** (a container answer is a value), **L11.1** (the tagged value word all of these are waiting for)
**Related:** ADR 0273 (the `(payload, tag)` pair across a call, and the rule that a gate may not cost a program that answers), ADR 0274 (the float-return promotion this row's argument arrives from), ADR 0357 (the two-double arm, the second road that hands back a `double`), ADR 0368 (the answer direction, and the opt-in doors that already read both words), ADR 0389 (a scan opens a position, a door decides it), ADR 0226 (no global in a value position), ADR 0454 / Gap R.524 (the same invalid shape at the unpacking road), Gap J.5 (the argument's `@.lstN` copy, reused here rather than rewritten), ADR 0275 (arms lowered in their own block, merge dominance), ADR 0232 (one question asked once), ADR 0166 (exit 2 is this compiler's bug; a wrong answer at exit 0 is worse), ADR 0192 (a fix reaches the position that asked, not the optimisation beside it), ADR 0249 (a refusal names the program), ADR 0302 (the two witness legs)

## The rule

**A value whose kind the compiler knows only from a TABLE it owns crosses a boundary in a word that can say what
it is.** Two boundaries were answering with a word that cannot:

* a **call argument** whose value is a `double` — because ADR 0274's promotion, or ADR 0357's agreement of two
  double arms, emitted the callee `define double` — was handed to an `i32` parameter slot. The fix boxes it with
  the one box a float that changes its variable's mind already takes, `@rt_float_new`, and the parameter arrives as
  the pair the pair road already knows how to bind;
* a **ternary arm** whose value is a container the program spelled out was the fold's global `@.lstN` — an array —
  dropped into a typed `phi`. The fix copies it into the runtime heap, which is the same copy `argVal` has made for
  an argument of that shape since **Gap J.5**.

Both defects were **exit 2** — `llc` refusing a module for a program `python3` prints — which **ADR 0166** assigns
to the compiler, not the author.

## Context

```
$ gustyc --eval 'def f(x):
      return 1.5

  def g(v):
    return v

  print(g(f(1)))'
gustyc: jit: llc: exit status 1
llc-20: error: '%t9' defined with type 'double' but expected 'i32'
  %t12 = call i32 @gy_g(i32 %t9)
                            ^
$ echo $?
2
```

```
$ gustyc --eval 'def f(x):
      return [1, 2] if x else 0

  w = f(1)
  print(w)'
gustyc: jit: llc: exit status 1
llc-20: error: global variable reference must have pointer type
  %t2 = phi i32 [ @.lst1, %tern.thenfwd3 ], [ 0, %tern.elsefwd4 ]
                  ^
$ echo $?
2
```

Nine shapes of the first program were measured against the pinned reference for **Gap R.581**; three of the nine
were not wrong answers but this crash, and the container arm's exit 2 had never been filed at all. Measuring a row
rather than describing it is what found **Gap R.584**.

## Decision

**1 — the argument is boxed by the door that already boxes a rebound float.** In `pairArgWords`, an argument that
is a call to a body the float table owns becomes `@rt_float_new`'s handle with the tag `"1"` beside it. Not a new
box: `floatbind.go`'s, so a slot, a call argument and a container element cannot answer "what is a boxed double?"
differently (**ADR 0232**). The scan (`pairCallScan`) is what opens the parameter — a new `floatAnswerCall`
question, asked of the callee the argument names, on top of the same AST predicate (`pairExprIsFloatBySpelling`)
the pair road already uses to *decline* a body, extended for **ADR 0357**'s road: both arms doubles, never one,
because the mixed shape refuses outright (`ternaryWordErr`) and opening a position for an answer that never
reaches the call would trade a refusal for a crash.

**2 — the pair road and the answer direction own disjoint bodies.** `pairAnswerIsCarried` is the question the doors
that read an ANSWER ask of the pair road: `spec.returnsPair && pairRetDone[name] && !answer.carries(name)`. The
negation is load-bearing: a body served by the answer direction stores its tag through that road's register, and a
door that asked the pair road as well would read a global nobody wrote (**ADR 0273**'s convention, **ADR 0368**'s
direction).

**3 — the arm is copied into the heap where it is lowered.** `materialiseTernaryArm` asks the fold's three tables
(`staticLists`, `staticDicts`, `staticSets`) and `emitSet`'s own `setNames` notebook — the spelled set arrives from
there, not from the comprehension fold, and the row's own program is the set arm — and calls `heapListFrom` /
`heapDictFrom` / `heapSetFrom` **in the arm's block, before the forwarding branch**, so the handle dominates the
merge that names it. An arm that cannot be built answers the empty string: the door's own decline, which falls back
to the road that refuses in words rather than emitting a `phi` over instructions that do not exist.

**4 — the fold is left alone.** Two control rows assert that a comprehension outside a ternary still emits
`@.lstN` and that `len([1, 2, 3])` still answers without building anything. Deleting the fold would also make the
crash go away, and would be a regression wearing a fix (**ADR 0192**).

## Alternatives rejected

* **Refuse the call-argument position** (the obvious move: an argument whose answer carries a kind is
  under-described, so say so). Rejected because six of the nine shapes compile today and answer — a refusal is not
  a ladder when the program already runs, and **ADR 0273** forbids a gate that costs a program that answers. This
  is also why the boxed float at a *condition* now refuses with **Gap R.146**'s sentence rather than reaching
  `llc`: that position never answered, so a named refusal is a strict improvement, and it names the row that owns
  the rest.
* **Widen `pairExprIsFloatBySpelling` to a single float arm of a ternary.** Rejected: that shape is refused by
  `ternaryWordErr` before any call happens, so the scan would open a parameter for an answer that never exists, and
  the module would fail where a refusal stands.
* **Bind the pair into the callee's parameter slot without boxing** (pass the `double` and let the body unbox).
  Rejected: the callee's parameter is an `i32` in the module's calling convention; the type error is the bug, not
  the reading of it.
* **Let `ternaryBranch` emit a `phi` of the global's first element** (`load` out of `@.lstN`). Rejected: the
  container roads would then read a length as a handle — an exit-0 wrong answer replacing an exit-2 honest failure,
  the one trade **ADR 0166** calls the compiler's bug rather than the program's.
* **Close `Gap R.581` with this commit.** Rejected and recorded: a text, a verdict, a `None` and a container answer
  still cross as a payload, so the row stays 🟨 `PARTIAL` with its remainder pinned leg-by-leg; and the tuple arm,
  newly measured, is filed as **Gap R.585** rather than folded into a row whose definition of done it is not.

## Consequences

* `pkg/lang/paircall.go` gained the scan's fourth argument question and the float box in `pairArgWords`;
  `pkg/lang/ternary_arms.go` gained `materialiseTernaryArm`. Both are one door each, asked by the roads that
  already existed rather than written beside them.
* The exit-2 census of this family is now empty: no program in the answered tables, the corpus program, or the
  `--verify-llvm` module table reaches `llc` with a type error.
* The record leg grew **18** entries (11083 → **11101**), every one read from the reference by `tools/recmerge`
  with the compiled leg cross-checked; the corpus grew `integration/programs/ternary_pair_arg.gy` (294 → **295**
  programs, 280 → **281** matrix rows, 244 → **245** parity-asserted, oracle `match` 230 → **231**, `fail` 0,
  `drift` 0).
* What is still owed is written where it can be seen: `TestWhatTheCallArgumentDoorStillCannotAnswerIsFiledNotFixed`
  pins each remaining shape with this backend's bytes beside the reference's and the roadmap row that owns the fix.
