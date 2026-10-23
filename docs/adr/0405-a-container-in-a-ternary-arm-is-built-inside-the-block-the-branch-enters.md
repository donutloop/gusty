# A container in a ternary arm is built inside the block the branch enters, not named by its global

**Roadmap:** **Gap R.385** (closed here — a container in a ternary arm is emitted where a word belongs) ·
**Gap R.128** (closed here — the same shape measured 2026-10-03 while landing **ADR 0262**, whose corpus program
this ADR pays) · **Gap R.381** / **ADR 0401** (the applied-`lambda` cycle whose CONTROL found this row) ·
**Gap R.393** / **Gap R.394** (the two rows this cycle measures and does not own) · **L11.1** (the container roads) ·
**Gap R.340** / **ADR 0374** (an arm is a program the test runs) · **ADR 0188** / **ADR 0192** (a print call passes
a word, not the module-global container) · **ADR 0166** (exit 2 is a compiler bug; a refusal at exit 1 beats a wrong
answer at exit 0 and beats a module the assembler refuses) · **ADR 0232** (one question, one asking place — the
decide-and-build pair) · **ADR 0333** / **Gap R.67** (the door that materialises a container and the positions that
read one word) · **ADR 0189** (every container builder hands back a built handle) · **ADR 0273** (a verdict is a
function of the program, so the family needs control rows) · **ADR 0302** / **ADR 0309** (the newly answering
programs are recorded from the reference, never self-certified)

## Status

Accepted. Landed 2026-10-23. Closes **Gap R.385** and, from the same root, **Gap R.128** — whose corpus program
`integration/programs/probe_ternary_container_arms.gy` left the debt ledger for the asserted parity corpus with this
commit.

## Context

`print([1, 2] if 1 else [9])` is a program the reference answers in one line. The compiled backend did not answer
it; `llc-20` refused the module, at **exit 2**, the code ADR 0166 reserves for a compiler bug:

```
%t1 = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([3 x i8], [3 x i8]* @.fmt1, …), i32 @.lst1)
llc-20: error: global variable reference must have pointer type
```

The backend has two roads for a container. The module-level road (`containerValue`) builds a literal once, names it
with a module global (`@.lst1`, `@.set1`, `@.dict1`) and prints it at the door; the call-argument road
(`containerOperand`) lowers a container to a heap word and hands that to a function. Neither road had the ternary
**arm** as a shape it could name, so the arm fell through to the number road, whose `value()` renders a container
literal as the *address of its compile-time global* — **ADR 0188**'s mistake and **ADR 0192**'s mistake, one
construct later. Two members of the family were quieter than the crash, and quieter is worse:

| program | compiled (parent `5985031`) | CPython |
|---|---|---|
| `print([1, 2] if 1 else [9])` | **exit 2**, `llc` refuses the module | `[1, 2]` |
| `print({"a": 1} if 1 else {"b": 2})` | `0` at **exit 0** — the heap handle, printed as a number | `{'a': 1}` |
| `print((1, 2) if 1 else (3, 4, 5))` | `0` at **exit 0** | `(1, 2)` |
| `print([v for v in [1, "a", True]] if 1 else [9])` | `0` at **exit 0** | `[1, 'a', True]` |
| `print(sorted([2, 1] if 1 else [9]))` | exit 1, refusal in words | `[1, 2]` |
| `print(hot() if go else cold())` (arms call container-answering `def`s) | exit 1, refusal in words | `hot`, `[1, 2]` |

And the shape had an owner already, eight hundred roadmap lines away: **Gap R.128**, filed 2026-10-03 while landing
**ADR 0262** for `c = 1` / `print([1, 2] if c > 0 else [3])`, which emitted `select i1 %c, i32 @.lst1, i32 @.lst2` —
the two globals themselves in the choose. Its corpus program carried a `Missing` pin in the ledger: a compiled leg
that never reached a run time, recorded so the omission would not read as coverage. **ADR 0401** found the same
fault again, as the control of its own ternary row, and filed it as **Gap R.385**. Three cycles had measured one
root: the arm's lowering asks `value()` for a name where every container builder has handed back a **built handle**
since **ADR 0189**.

## Decision

**An arm is a program the test runs, so build it where the branch enters.** `containerOperand` grows a `*CondExpr`
case that lowers each arm through itself (`containerOperand(bb, arm)`) inside the block the branch selects, using
`ternaryBranch`, and phis the two handles at the merge. A test the source wrote is not a run-time question: the arm
that runs is the answer and the arm it rejected is not in the program — not executed, and not emitted (the same rule
**ADR 0374** states for a `lambda`'s arm, and the reason `print(7 if 1 else [1, 2])` builds no list at all).

**The two questions an arm answers are read through one pair of functions.** `isContainerExpr` and
`staticContainerKind` each grow a `*CondExpr` case, and both ask the arms through `isContainerArm` /
`staticContainerArmKind` (`pkg/lang/heapargs.go`). Those two are the *pair-wide* version of the module-level
predicates, widened by the one spelling an arm commonly takes that the module-level roads settle at another door: a
**comprehension**. A bare `[v for v in xs]` is built and printed by the comprehension road, so the shared predicate
had never been asked to call it a container — and an arm that is not a container to the door that picks the printer
is a program whose arm reaches the number road and handprints a global. `CompGenerator` is deliberately excluded:
this backend materialises a generator eagerly as a list, which is a divergence the generator row owns, and an arm of
that spelling keeps the road it has rather than being re-decided here.

**The print door asks before the number road gets a vote, and refuses what it cannot name.** The door takes the
`*CondExpr` case ahead of the `sorted`/`reversed` case, chooses `rt_print_list` / `rt_set_print` / `rt_dict_print`
from the kind the arms agree on (with the tuple pair going to `rt_print_container_value`, because the immutable
spelling's parentheses belong to the object and not to the road that built it — **ADR 0361**'s rule read at the
ternary), and when the arms are containers that agree on no kind it refuses **in words at exit 1** rather than
emitting: a handle printed as a number is indistinguishable from a right answer, and **ADR 0166** prefers the
refusal. Four such programs moved from *exit 2* to *exit 1* by this decision alone.

## Consequences

`pkg/lang/heapargs.go` grows the `*CondExpr` arms of `containerOperand`, `isContainerExpr` and
`staticContainerKind`, plus `isContainerArm`, `staticContainerArmKind`, `compIsContainer`,
`ternaryHasContainerArm` and `ternaryContainerArmErr`; `pkg/lang/codegen_builtin_print.go` grows the door's
`*CondExpr` case. No runtime symbol is added, no instruction is invented, and the module verifier sees nothing it
had not seen before.

Measured with a parent-commit build (the tree at `5985031`) and the working tree built from the same checkout, over
the **same files**, 39 programs drawn from the two new test tables plus the conformance program:

* **25 programs moved**, and every one of them now prints CPython's bytes; **none prints a wrong answer at exit 0**.
  Of the 20 answered-family programs that moved: **12** came off **exit 2** (`llc` refusing the module), **5** off a
  **silent wrong answer at exit 0** (the `0` of a heap handle — the dict pair, the dict-with-number pair, the tuple
  pair, the mixed-tag comprehension arm, the nested-container comprehension arm) and **3** off a **refusal in
  words** (the `lambda`-building arm, the `sorted` arm, the container-answering-`def` arm). Five more answered before
  this cycle and are in the tables as controls (ADR 0273).
* **4 kind-disagreeing pairs moved from exit 2 to exit 1**, which is **ADR 0166**'s preferred direction: the module
  is no longer handed to the assembler, and the sentence names the arms and the road.
* **Gap R.128's corpus program paid.** `programs/probe_ternary_container_arms.gy` prints its header's two lines,
  its `Missing` pin left the ledger with the row, and the program moved from the probe corpus to the asserted parity
  corpus (`conformanceProbes` → `conformanceStandalone`). The matrix is 231 rows, 202 parity-asserted, 29 recorded
  divergences, 188 `match` / 21 `debt` / 22 `not_applicable`, drift 0.
* **44 programs went onto the record leg (8709 → 8753)** — the answered families of `pkg/lang` and `integration`
  plus the corpus program — each written from CPython by `tools/recmerge` with the compiled leg cross-checked, which
  is the only way an entry gets written at all (**ADR 0309**).
* **Two rows were filed from the same sweep rather than quietly fixed**: **Gap R.393**, the doors that *ask* a
  container ternary a question (`str(...)`, a loop's iterable, a `def`'s `return`, a `%` format, an interpolation,
  `len`, a position) — five of which still exit 2 and three of which refuse with a sentence about literals; and
  **Gap R.394**, the arm pair this row refuses, whose answer is the tag printer **ADR 0333** built for the answer of
  a call. Both are pinned with today's bytes beside the reference's, on both legs, so neither can be filed and
  forgotten.

## Machine path

The program's answer and the refusal are both consumable without scraping text:

```
$ gustyc --json --eval 'print([1, 2] if 1 else [9])'
{"output": "[1, 2]\n", "backend": "aot", "exit": 0, "result": null, "type": "None"}

$ gustyc --json --eval 't = 1 < 2
print([1, 2] if t else {"a": 1})'
{"backend":"aot","error":"jit: codegen: the arms of `[1, 2] if t else {\"a\": 1}` are containers whose kinds this
 backend cannot choose one printer from … (roadmap Gap R.385, beside Gap R.67 and ADR 0333)","exit":1,"ok":false,
 "phase":"codegen"}
```

The refusal is exit 1 with `phase: codegen` (**ADR 0004**'s schema, **ADR 0006**'s exit-code contract), names the
source text of the ternary and the Gap that owns the shape, and leaks no Go type or pointer. The family is a row of
the conformance artifact (`programs/ternary_container`, `oracle: match`, both legs' bytes in
`integration/conformance-matrix.json`), so an agent reads the claim out of the artifact rather than out of prose.

## IR implications

For a constant test, one arm's builder: `print([1, 2] if 1 else [9])` emits the list's `rt_alloc` +
`rt_set_elem`/`rt_tag_elem` pair and `call void @rt_print_list(i32 %h, i32 0)` — the rejected arm contributes no
instruction at all, which `TestTheUntakenContainerArmBuildsNothing` asserts by looking for the literal `9` and not
finding it. For a run-time test, `br i1 %t1, label %tern.then1, label %tern.else2`, one builder per block, and the
merge phis the two handles into the printer's argument. What the module never carries again is a folded container
global in an integer position: `TestTheContainerArmsTravelInAWordAndNotAsAGlobal` walks every `@printf` line of every
answered program's module and fails on `@.lst`, `@.set` or `@.dict` appearing there, because that instruction is the
exit-2 shape wearing a passing test.

## Alternatives rejected

* **Emit the folded global and `inttoptr`/`bitcast` it into the slot.** This is what the roads did for one more
  construct than they should have: the global's *address* is not the container's *handle*, and the two are equal only
  by accident of the heap layout. **ADR 0188** and **ADR 0192** settled the direction — a print call passes a word
  the runtime produced, never a module constant wearing one.
* **Add a `*Comp` case to `isContainerExpr` and `staticContainerKind` themselves.** True and tempting, and it is a
  different row: those two predicates are asked by 25 call sites across the membership test, the comparison, the
  loop, the truthiness door, the `%` road and the argument roads, and widening them all at once is a bundle of
  unrelated behaviour changes riding on a print fix (the one-commit rule). The arm question is therefore widened
  where the arm needs it, in one place, with the comment that names what a future row should do when the module-level
  predicates grow the comprehension themselves.
* **Keep the `select` and materialise both arms before the choose.** That builds an object the program never takes —
  an allocation, a heap-full trap and all the arm's element effects, run for a value that is discarded — and
  **ADR 0374** already settled the shape for the arms of a `lambda`: the arm that cannot run must not be run. The
  branch road is what makes the difference observable, and `TestTheUntakenContainerArmBuildsNothing` is what keeps
  it.
* **Print the kind-disagreeing pair with the number printer, or with a printer chosen from the taken arm.** The first
  is the silent `0` this row exists to remove; the second needs the taken arm to be known at compile time, which for
  `t = 1 < 2` it is not — and guessing from the first arm would print a dict with list brackets half the time.
  Refusal by name is the answer that can be told apart from a wrong one.
* **Fold `Gap R.393`'s five exit-2 shapes into this commit.** They are the same family and a different door — the
  doors that *ask* a question rather than the one that prints — and each needs its own operand road. Shipping them
  half-done (a container built, then read as a word) would convert four exit-2s into exit-0 wrong answers, which is
  the one trade this repository does not make. They are pinned as owed rows instead, on both legs, with the
  reference's bytes beside today's.

## Tests

* `pkg/lang/ternary_container_test.go` — the answered family on the record leg (`goldenStdoutIs`, so the record's own
  bytes are asserted and coverage cannot be deleted by deleting a record) and the compiled leg (`runIR`), the
  arm-does-not-run claim, the IR claim above, the refusal-shape claim (names the arms, names the Gap, leaks nothing),
  and the owed table: `rejects` rows pinned by `llc`'s message and `refuses` rows pinned by the sentence, each with
  CPython's answer beside it and a `t.Fatalf` that fires when a program starts agreeing with the reference.
* `pkg/lang/lambda_answer_test.go` — the row **ADR 0403** left in `lambdaAnswerOwed` for this shape is DELETED, not
  edited, with the comment that says where the program went.
* `integration/ternary_container_test.go` — the reference leg: 35 programs run through the CLI against `python3`
  (including the shapes no `pkg/lang` row asks: an arm in a method body, in a `while` body, in a `for` body, in an
  `except` handler and in a `match` arm, an arm compared to a container, an arm whose test is a call that hands back
  a container, the three empty-container spellings), the whole family checked to be on the record, the corpus program
  checked line-for-line against CPython, and the owed shapes pinned at the CLI with exit code, empty stdout and the
  message they fail with.
* `integration/conformance_cases.go` / `integration/bool_element_test.go` /
  `integration/float_rebound_return_test.go` — the corpus program's debt pin, its exit-6 contract row and the stale
  "not a row here" comment are all removed with the debt; the program joins the asserted parity corpus beside
  `programs/ternary_container.gy`, the file whose every line is what CPython prints for it.
