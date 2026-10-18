# 0374 — A conditional expression runs the arm its test chose, not the pair it was written with

## Context

`a if c else b` is one expression in this language and an `if`/`else` statement pair in the reference's
own grammar. The compiled backend lowered it as the expression it looks like: ADR 0026 gave a run-time test
a single LLVM `select`, and the two arms were lowered **beside the test**, into the block the test itself
sits in, so that `select` could take them as operands.

A `select` has no unreachable operand. Both of its operands are arguments, which means the run time has
already executed both arms' instructions at the moment it picks one of their results. Every effect written
in an arm — a call, a division, a subscript, an allocation — therefore happened on the way to the answer,
whatever the test said. Four programs, all measured on the shipped binary before this ADR, each answered by
the arm that was never supposed to run:

| source | CPython | this backend, before this ADR |
|---|---|---|
| `x = 5` / `print(1 if x > 0 else boom())`, `boom` prints | `1` | printed `boom`, then `1` |
| `x = 5` / `print(boom() if x < 0 else 2)` | `2` | printed `boom`, then `2` |
| `x = 5` / `print(1 if x > 0 else 1 // 0)` | `1` | **ZeroDivisionError**, exit 3 — from the arm the test had already rejected |
| `def fact(n):` / `return 1 if n <= 1 else n * fact(n - 1)` / `print(fact(5))` | `120` | "the compiled root stack is full (recursion too deep)", **exit 2** |

The first two are ADR 0166's "wrong answer at the exit code of success" class. The third is a program that
dies of a raise it never performed. The fourth is the worst of the family and the reason this is a defect
rather than an inelegance: the recursive call had been emitted unconditionally, in the block with the test,
so the base case was a value and not a stop — the artifact was *killed* rather than returning, which is the
class ADR 0166 and Gap R.203 define as this compiler's bug, not the program's, and the harness reports it as
exit 2 with "this is the artifact, not the program".

`--emit-llvm` said exactly what was wrong. The whole conditional was one instruction whose operand list
contained the call:

```llvm
;t4 = icmp sgt i32 %_x.ld1, 0
%t8 = select (i1 %t7, i32 1, i32 %t5)      ; %t5 was produced by the call above this line
```

ADR 0275 had already solved this, for the two operators that have the same shape. `a and b` / `a or b` were
also a `select`, and the fix was `logicSkeleton`: evaluate the first operand, branch on its truth, evaluate
the second **only in the block the branch enters**, and meet the two results at a block-parameter `phi`.
That ADR names the ternary as the second half of the same bug and leaves it on the eager road. This is that
half.

## Decision

**An arm the test rejected is not evaluated. An arm whose evaluation cannot do anything but answer a value
may still be folded into a `select`, because for that pair the two roads disagree about nothing except how
many instructions the module carries.**

1. **`(*irGen).ternaryBranch` (`pkg/lang/ternary_arms.go`) is `logicSkeleton` for a ternary.** The test is
   lowered once, in the block the caller stands in, via the same `truthyValue` the `and`/`or` door uses; a
   `br i1` sends the run time to `tern.then` or `tern.else`; each arm is lowered **only inside the block the
   branch enters**; both jump to a forwarding block (`tern.thenfwd`, `tern.elsefwd`) and the merge block
   (`tern.merge`) carries one `phi` per answer.
   The forwarding blocks are not decoration. An arm whose own lowering contains a call ends, like every call
   in this backend, in an exception-check block that may branch to the function's raise-exit; a `phi`'s
   incoming block has to be a block this door controls, so the arm jumps to a block the door named instead
   of landing in the merge directly. It also means an arm that traps never reaches the merge at all — the
   raise unwinds to `main.raiseexit` with the frames the arm's own call pushed, which is what the trap rows
   in the two test files assert (exit 3, the reference's class, the frame list including `boom`).
2. **Both arms are built into a scratch builder and nothing is written to the caller until both answered** —
   ADR 0275's lesson (Gap R.149), that a half-emitted door leaves a module with a block that has no
   terminator, which `llc` reports as a compiler bug at exit 2.
3. **The gate is `ternaryArmsNeedBranch`, and "silent" is a short list, not a long one.** An arm needs the
   branch unless it is provably nothing but a value: a literal; a name read the checker proves was written;
   `+ - * & | ^ << >>` and the six orderings over literals of one domain; `not`/unary-minus of a literal.
   Everything else is "may act" and goes behind the branch — because in this backend an expression raises
   for reasons that are not in the node: a subscript raises `IndexError`/`KeyError`, `//` and `%` raise
   `ZeroDivisionError`, an ordering of two kinds raises `TypeError`, a call raises and prints whatever its
   body does, a container literal allocates, a comprehension loops. The default is the direction that costs
   an instruction and never the direction that runs a program's effects.
4. **The written-slot question is asked from the same tables the emitted check reads.** A name read is
   silent only when ADR 0228's written-flag machinery says the slot cannot be unwritten. That matters beyond
   performance: an `UnboundLocalError` is an effect, and the guard cannot be hoisted out of a branch because
   both operands of a `select` dominate the test. `def f(flag):` / `if flag: y = 7` / `return (1 if flag else
   y) + 0` used to raise for a name the taken arm never mentions; it answers `1` now.
5. **The `select` stays where it was.** Two literal arms, or arithmetic over literals, keep the instruction
   ADR 0026 put there and ADR 0262 pinned, byte for byte (`TestATernaryWithPureArmsIsStillASelect` asserts
   the `select` is present *and* that no `tern.merge` block exists). That is ADR 0273's rule — a gate must
   not cost a program that compiles today — applied to the instruction scheduler's benefit rather than to a
   refusal.
6. **An arm that will not lower inside its own block is a refusal, not a fallback.** The int road says so in
   words that name the construct, the gap and ADR 0166, rather than quietly re-emitting the pair beside the
   test — the road that runs the arm the test rejected is not available as a "plan B".

## Agentic rationale

- Every row of the defect is now an **answer row with the reference's bytes written into it**: 59 rows across
  seven tables in `pkg/lang/ternary_arms_test.go` (23 untaken-arm rows, 6 taken-arm trap rows plus the control
  that a trap-bearing function is simply never called, 4 pure-arm IR rows, 3 branch/`phi` IR rows, 5 recursion
  rows, 17 record-leg rows) and 27 rows across three tables in `integration/ternary_arms_test.go` (the same
  shapes through the linked binary and `--aot`, each row first re-measuring CPython so a row cannot pass by
  both the expectation and the compiler drifting).
- The IR shape is pinned, not just the answer — `TestATernaryWithAnEffectfulArmIsABranchAndPhi` asserts the
  `select` is gone, the three `tern.*` blocks exist, a block-parameter `phi` meets the arms, the arm's call
  appears exactly once, and — the assertion that distinguishes the two roads — **the call is emitted after
  the `br i1` that chooses it**. Without the fix that table fails on all five assertions; with it, it passes,
  and each of the three programs is put through `opt-20 -passes=verify` and `llc-20 -O3` in the same case.
- **Thirty-four sources went to `tools/recmerge`** (the record grew to 7892 sources). The recorder asks
  CPython first and refuses to write an entry whose compiled leg disagrees, so all thirty-four are answers
  the reference gave and the compiled leg matched — including the four that trap, whose entries carry the
  reference's class and sentence. Every one of them is asked by a case, so a record cannot be deleted without
  a case going red, and `pkg/lang/golden_artifact_counts_test.go` recomputes the count that `AGENTS.md`,
  `docs/operations.md` and `roadmap.md` quote.
- Exit-code discipline is asserted, not assumed: a trap row must not be exit 2 (the compiler-bug class), must
  not be exit 1 (a refusal issued before the program ran), and must be exit 3 with a traceback; the recursion
  rows must not be exit 2 and must not print "root stack is full".
- No new machine-readable surface. This cycle changes which block an arm is lowered into; `--emit-llvm`
  already reports the module faithfully, and `--callables` is untouched because no callable is added, removed
  or re-shaped — the defunctionalization pass and this door are orthogonal, which is why `def fact(n)` in a
  callable position also stops.

## Codegen / IR implications

- A branch-lowered ternary adds five labels per conditional (`tern.then`, `tern.else`, `tern.thenfwd`,
  `tern.elsefwd`, `tern.merge`), two unconditional jumps, one conditional branch and one `phi` per answer. The
  forwarding blocks are the two-instruction cost of the `phi`-dominance rule; the simplifier folds them and
  `llc-20 -O3` accepts the module either way.
- The arms keep their own exception roads. An arm that calls still emits the `@exn_flag` test and its branch
  to the enclosing function's raise-exit block, and that branch is now inside `tern.else` rather than beside
  the test — which is what makes a trap in a rejected arm impossible rather than merely unobserved.
- Frame bookkeeping is unaffected: the call inside an arm still pushes its own frame, so `rt_die`'s report
  names the function the trap travelled through (`File "prog", line 2, in boom`).
- Nested ternaries each own their own five labels (`g.newLabel` interns per generator), and the two ternaries
  in one expression are two independent branches; the register a `phi` reads is the one the arm returned,
  which dominates the terminator of the path that reaches it.
- The pure-arm `select` path is untouched, so the two modules a program like `print(1.5 if c > 0 else 2.5)`
  produces before and after this ADR are identical — the pin on that is a guard, not a hope.

## Alternatives rejected

- **Hoist only calls, and leave subscripts, divisions and allocations beside the test.** The first measurement
  suggested exactly this ("the arm is a call"), and it is the shape that would have shipped the third row of
  the table: `1 // 0` in a dead arm raises without any call in sight, and `xs[9]` and `d["k"]` do the same.
  The gate is about effects, and an effect in this backend is not a node type.
- **Make every ternary a branch and delete the `select`.** Simpler code, and it costs the folding that ADR
  0262 exists to have: a pair of literals has no effects to order, `select` is what the scheduler wants there,
  and ADR 0273's rule is that a correctness gate must not take an instruction away from a program that
  compiles and answers today. The pure-arm case is asserted in both directions to keep this from drifting.
- **Rewrite `a if c else b` into an `if`/`else` statement with a synthetic temporary at the AST level.** The
  reference's own desugaring, and wrong here: the ternary sits in expression positions a statement cannot
  stand in — an f-string field, a call argument, a `return`, a default value, a comprehension element, a
  nested arm. A temporary is also a slot, which means a binding, which means the ADR 0228 written-flag
  machinery would start seeing names the program never wrote.
- **Refuse a ternary whose arms can act, the way the word-disagreement gate refuses mixed arms.** That turns
  hundreds of ordinary programs into exit 1 with a compiler sentence, on a construct the reference runs; ADR
  0166 counts a refusal where the reference answers as a defect of the same family as a wrong answer.
- **Evaluate both arms and undo the effects.** There is nothing to undo: the arm printed, allocated, and raised.
- **Rely on the optimiser to make the dead arm's instructions dead.** `select` operands are not dead — they
  are used, and the passes are not permitted to delete a call with observable behaviour, which is the whole
  point of the instruction the codegen chose.

## Consequences

- **Closed:** roadmap **Gap R.340** — the conditional expression evaluated the arm its test rejected, in four
  faces (a printed line, a raise, a dead artifact, and an `UnboundLocalError` from a slot the taken arm never
  reads). The two test files above are the evidence cells.
- **Measured and recorded:** 34 sources through `tools/recmerge`; the record holds 7892 sources, and
  `AGENTS.md`, `docs/operations.md` and `roadmap.md` carry that number, each recomputed by
  `pkg/lang/golden_artifact_counts_test.go` and `integration/docs_artifact_counts_test.go`. The Snapshot's
  ADR, program, matrix, oracle and queue tallies moved with it (365 ADRs to `0374`, 226 programs, 213 matrix
  rows of which 181 are parity-asserted, 167 `match` oracle verdicts, 246 queue rows), and
  `integration/programs/ternary_arms.gy` is the 226th program and the matrix row `programs/ternary_arms`.
- **Filed, not fixed:**
  - a **container arm** is typed before the branch is reached: `[1] if c else [boom()]` is refused
    `list literal elements must be integers, not int` by the list-literal element door
    (`pkg/lang/codegen_container.go`), before and after this change. The reference prints `[1]`. The refusal is
    the container-literal element typing, not this door — the row says so in the tracker rather than acquiring
    a second ID for the same program. The container PAIR is the same story one door further on:
    `[1, 2] if c > 0 else [3]` lowers each literal beside its own branch and the two meet at
    `phi i32 [ @.lst1, %tern.thenfwd ], [ @.lst2, %tern.elsefwd ]`, which is the operand `llc` has rejected
    since **Gap R.128** filed it — the literal's global in a value position, now through a `phi` instead of a
    `select`, still exit 2, still `programs/probe_ternary_container_arms.gy`'s pinned row;
  - **mixed-word arms** still refuse, as ADR 0166 requires: `print(2 if x else 3.5)` says the arms do not
    agree on a word. That refusal is `L11.1`'s tagged value word to earn, and this cycle did not widen it —
    a gate that promotes an arm into a branch cannot promote an arm into a word it does not have;
  - the **loop** shape is correct but pays the forwarding blocks per pass; together with the other
    two-instruction block chains it belongs to the block-placement family (Gap R.290's neighbourhood), not to
    this defect.
- **Adjacent and filed elsewhere:** the conformance program spells each of its functions' parameters with its
  own name, because two bodies that share a parameter name share the module's one float-variable table and a
  name one body bound to a double is read as a double by every later body — `def scaled(n): return 0.0 if n
  == 0 else 1.5 + scaled(n - 1)` above `def fact(n): return 1 if n <= 1 else n * fact(n - 1)` made `fact`'s
  arms disagree on a word and the program refused at exit 1. That is **Gap R.339**'s measurement wearing a new
  hat (its own row owns the fix, the per-body table); this cycle changed the program's spelling and did not
  edit a ledger to hide the answer.
- **Adjacent and unchanged:** the defunctionalization pass (`pkg/lang/callable_value.go`, Gap R.335) still
  declines a higher-order recursion that omits its own defaulted parameter. The two defects share a program
  shape and nothing else — that one is about which clone gets built, this one is about which arm runs — and
  the fix here did not touch the clone table.
