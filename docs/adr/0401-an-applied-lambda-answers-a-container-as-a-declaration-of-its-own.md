# An applied `lambda` gets a declaration of its own, so its answer travels the doors a `def`'s answer uses

**Roadmap:** **Gap R.382** (closed here — `print((lambda: [1, 2])())` died inside `llc-20` at exit 2) ·
**Gap R.331** (the same missing registration one rung closer to a refusal: the two programs that printed the
allocator's slot `0` at exit 0 now print `[9, 9]` and `[111, 111]`, and its two pins are deleted; the row's own
arms — a module-level TEXT and BOOL read — stay open) · **L11.7** (functions are values that compile) ·
**ADR 0333** / **ADR 0344** (the container-answer door: the scan that registers what a `def` hands back, the
`ret` that materialises it, the eleven positions that read it) · **ADR 0368** (`answerKindPlan`, which registers
what a `def` hands back and registered nothing for a `lambda`) · **ADR 0372** / **ADR 0374** (the applied and
bound-callable roads that hoist a body today) · **ADR 0345** (`<lambda>` as the display name, `callableWalk`,
and §3's ban on a reader's sentence naming the compiler) · **ADR 0232** (one question, one asking place — the
frame question is asked by `lambdaFreeNames`, not by this pass) · **ADR 0273** (a verdict is a function of the
program, not of the order things were compiled in — which is why the names are minted once and the rounds only
ever remove a hoist) · **ADR 0320** (the front end reports the tree the human wrote — the rewrite is on a copy) ·
**ADR 0166** / **ADR 0212** (a module `llc` rejects is exit **2**, a compiler bug; a program's problem is exit 1
in words) · **ADR 0346** / **ADR 0004** (`--json` carries the identical sentence, so both channels move) ·
**Gap R.67** (the one-word positions that still refuse, which this row made MORE visible rather than fixing) ·
**Gap R.261** (a declined container answer reaching the screen as the slot — the reason the second gate accepts
an *undescribed* answer too) · **Gap R.383**, **Gap R.384**, **Gap R.385**, **Gap R.386** (the four rows this
cycle measured and filed instead of fixing)

## Status

Accepted. Landed 2026-10-23.

## Context

One program, asked of the reference and of this toolchain:

```console
$ python3 -c 'print((lambda: [1, 2])())'
[1, 2]
$ gustyc --file row.gy                    # print((lambda: [1, 2])())
gustyc: jit: llc: exit status 1
llc-20: error: …/jit.ll:11:11: error: global variable reference must have pointer type
  ret i32 @.lst1
          ^
exit status 2
```

`ret i32 @.lst1` is not a word LLVM has: a module constant is a pointer, and the instruction the emitter wanted
is `ret i32 <handle>`. Exit 2 is the code **ADR 0166** reserves for "the compiler is broken" — not a program's
problem, not a refusal, a defect that reaches a user as `llc`'s stderr. The same answer one rung further down —
with a comprehension in the body instead of a literal — *did* build, and printed the allocator's slot:
`print((lambda x: [x for q in [1, 2]])(9))` answered `0` at **exit 0** where the reference answers `[9, 9]`
(**Gap R.331**). One defect, two witnesses, both already on the tracker.

The reason is a shape difference, not a missing arm. A `def` that hands back a container is answered by a
whole-program read: `containerAnswerScan` (**ADR 0333**) reads its `return`, registers the kind, the `ret`
materialises the object through the builders a binding already uses, and each of the eleven positions that can
read the answer — `print`, `str`, `repr`, an f-string field, a binding, a condition, `len`, `in`, a loop's
iterable, a subscript, a slice — asks the door that knows the kind (`containerAnswerOperandRead`, **ADR 0344**).
A `lambda` applied where it is written has no declaration for a scan to read. `emitLambda` mints one *in the
middle of lowering the statement that contains it*, by which time every table above has long been filled, so the
answer fell through to the road that keeps one `i32` for a value: the module literal in a register slot (an
illegal module), or the handle printed as a number (a silent wrong answer).

Writing a container door per call site would have meant eleven new doors, and **Gap R.261** is the record of what
happens when the written ones and the written-over ones disagree.

## Decision

**Give the `lambda` the one thing the scan needs: a declaration.** `pkg/lang/lambda_answer.go`'s
`hoistAppliedLambdaAnswers(prog) (*Program, int)` runs in `GenerateIRReport` immediately after
`defunctionalizeCallablesTelling` and before imports, the pair scan, the answer-kind plan and the container scan —
the same position, and for the same reason, as defunctionalisation's: those are whole-program reads of this tree,
and a declaration that arrived after them would be a second opinion about a program they had already answered.

For each application it accepts, the pass mints a module-level `FuncDef` and points the call's callee at it:

```llvm
define i32 @gy_lambda_0() {            ; was (lambda: [1, 2])()
  …
  %t1 = call i32 @rt_alloc(i32 1)      ; the object is BUILT at the return,
  … rt_set_elem / rt_tag_elem …        ; per call, not once per module
  ret i32 %t1                          ; a register holding a handle — not @.lst1
}
define i32 @main() {
  …
  %t6 = call i32 @gy_lambda_0()
  %___cans9.ld1 = load i32, i32* %___cans9      ; ADR 0333's container root
  call void @rt_print_list(i32 %___cans9.ld1, i32 0)
  …
}
```

There is no second answer road. The answer is registered by the same scan a `def`'s is registered by, stored by
the same `ret` road, rooted by the same `___cans` root, read by the same eleven doors — and refused by the same
doors that refuse, in the same sentence, with the callable named `<lambda>`. Four rules make it a pass rather than
a rewrite:

1. **The gate is the scans' own question, not a new one.** A hoist is kept only if `containerAnswerScan` reads
   the minted declaration as answering a container — *described* or *proved-and-undescribed* (the second is what
   turns a printed handle into a refusal, **Gap R.261**). A body that answers a number, a text, a float, `None`,
   a module name, an unfollowable call, or a container of two different kinds is left exactly where the source
   wrote it: the pass buys nothing there and would add an exit-2 class to the risk.
2. **The frame question is asked by the door that already answers it.** A `def` reads the module; a `lambda`
   reads the frame its application is written in. `appliedLambdaBodyCarriable` therefore admits a body only when
   every free name — asked with `lambdaFreeNames`, the same function the capture road uses (**ADR 0232**) — is a
   predeclared builtin. A body that reads `x` from the enclosing frame stays on the closure road, which carries
   it as a parameter the call fills. A hoist that silently read a different `x` would be ADR 0166's bug wearing
   this pass's clothes; what that road still owes is **Gap R.386**.
3. **A verdict is the program's, not the compile's** (**ADR 0273**). All names are minted once, before any round,
   above every spelling the program itself declared (`def lambda_0(): …` is legal source, and a second `define`
   of it is `llc`'s duplicate-symbol complaint at exit 2 — so the emitter's `lambdaCounter` is started at the
   count this pass consumed). Because the second gate's answer depends on call sites that only *exist* once the
   callee is a name — `(lambda v: [v, 1])(2.5)` hands its parameter a (payload, tag) pair — the pass runs in at
   most four rounds, and a round only ever *removes* a hoist. Rounds that do not settle hand back the untouched
   program with the emitter's numbering intact: a rewrite that flickers is worse than one that never happens.
4. **The tree the front end reports is not this pass's to edit** (**ADR 0320**). The rewrite happens on
   `copyProgramAST`'s copy, so `--emit-ast`, `gusty fmt` and the checker still see the `lambda` the human typed;
   the declaration carries the application's own span, so a traceback names the program's line, and `CloneOf`
   carries **ADR 0345**'s `<lambda>`, which `callableSurfaceName` already refuses to blame a programmer with.

The walk is the pass's own — `appliedLambdaWalk`, a reflective descent through struct fields, slice elements and
interface values — because `callableWalk` follows a field only when the field's element type implements `Node`,
and three carriers of expressions in this AST are not nodes: `FStringPart`, `MatchCase`, `ExceptClause`. With the
plain `callableWalk` shape, `print(f"{(lambda: [1, 2])()}")` kept its `ret i32 @.lst1`: the one position the
answer door most needed to answer was invisible to the reader. The same hole in the *defunctionalisation* pass's
reading is a real refusal, measured this cycle and filed as **Gap R.383** — to be closed by making the two walks
one, not by writing a third.

## What this row deliberately did not touch

Four divergences came out from under the row. Each is pinned in `pkg/lang/lambda_answer_test.go` with the
reference's answer beside this toolchain's, and each pin FAILS the day the program starts answering — a fix may
not retire its own evidence.

* **Gap R.384 — an answer that is a text, a verdict or `None`.** `print((lambda: True)())` still prints `1`,
  `print((lambda: None)())` still prints `0`, `print((lambda: "hi")())` prints `0` on the CLI leg and its module
  is rejected on the `pkg/lang` leg (`use of undefined value '@rt_str_intern2'` — **Gap R.333**'s missing
  definition, arriving through a body the program wrote). The gate admits only what the *container* scan proves,
  which is the row's own discipline: the other three scans (`strAnswerScan`, the bool arm,
  `answerKindPlan`'s `None` arm) each need their own measurement, their own pins and their own row.
* **Gap R.385 — a container in a ternary arm, with no `lambda` in the program.** `print([1, 2] if 1 else [9])`
  emits `i32 @.lst1` into the `printf` argument list and `llc` refuses it at exit 2, pre-existing and independent;
  it surfaced as the control of this row's ternary case.
* **Gap R.386 — an applied `lambda` that reads a name and answers a container.** `def f(x):` /
  `    return (lambda: [x])()` is refused by ``list literal elements must be integers, not `` — the literal
  builder's complaint, an empty thing after `not`, naming no callable, no row, no ADR (**ADR 0345 §3**). The
  closure road it is left on has captures and no container answer; the `def` twin prints `[2]`.
* **Gap R.383 — the walk.** Shared with **ADR 0395**'s clone road, which reads the program with `callableWalk`
  and so never sees a capture written in a `match` arm or an `except` body: the arm's copy is refused with
  `` `key=lambda …` is refused: a lambda is a function VALUE … `` while the identical statement beside it
  compiles.

And one thing the row refused to "fix" by narrowing: **Gap R.67**'s positions. `print([1, (lambda: [1, 2])()])`,
`print(g((lambda: [1, 2])()))`, `print({"k": (lambda: [1, 2])()})`, `bool(…)`, `(lambda: [1, 2])() + 1` and a
ternary arm refuse — as they refused before, in words, at exit 1, with the same sentence the `def` twin gets.
`TestWhatAnAppliedLambdaAnswerRefusesInWordsWhereTheDoorHasNoRoad` pins that the sentence names `<lambda>` and
the row that owns the missing road, and leaks nothing (`lambda_0`, `__cans`, `%!s(`, `0x`, a Go type). A
container answer that arrives where one word is kept must stay a refusal; printing a handle would be
**Gap R.261** again, one door over.

## Measured

Thirty-eight programs, asked of `python3` first and then of a build of the parent commit and a build of this
commit side by side. The reference column is CPython's own bytes, not this file's memory. "`llc` rejects" is the
one message `global variable reference must have pointer type` over a `ret i32 @.lst1` / `@.dict1` / `@.set1`, at
exit 2; a bare number in the parent column is an exit-0 answer that is not the reference's.

| the program | CPython | parent | this commit |
|---|---|---|---|
| `print((lambda: [1, 2])())` | `[1, 2]` | `llc` rejects, exit 2 | `[1, 2]` |
| `print((lambda: {1: 2})())` | `{1: 2}` | `llc` rejects | `{1: 2}` |
| `print((lambda: {1, 2})())` | `{1, 2}` | `llc` rejects | `{1, 2}` |
| `print((lambda: (1, 2))())` | `(1, 2)` | `0` | `(1, 2)` |
| `print((lambda: [])())` | `[]` | `llc` rejects | `[]` |
| `print((lambda: list())())` | `[]` | `0` | `[]` |
| `print((lambda: [q for q in [3, 1]])())` | `[3, 1]` | `llc` rejects | `[3, 1]` |
| `print((lambda v: [v, 1])(9))` | `[9, 1]` | refused: "list literal elements must be integers, not " | `[9, 1]` |
| `print((lambda x=3: [x])())` | `[3]` | refused, same sentence | `[3]` |
| `x = (lambda: [1, 2])()` / `print(x)` | `[1, 2]` | `llc` rejects | `[1, 2]` |
| `print(len(x))` over that binding | `2` | refused: "len of a non-string variable" | `2` |
| `print(1 in (lambda: [1, 2])())` | `True` | `llc` rejects | `True` |
| `for v in (lambda: [7, 8])(): print(v)` | `7`, `8` | `llc` rejects | `7`, `8` |
| `print(str((lambda: [1, 2])()))` | `[1, 2]` | refused: no word for the argument's kind | `[1, 2]` |
| `print(repr((lambda: [1, 2])()))` | `[1, 2]` | refused: "repr of a call is not implemented" | `[1, 2]` |
| `if (lambda: [1, 2])(): print("yes")` | `yes` | `llc` rejects | `yes` |
| `print((lambda: [1, 2])()[0:2])` | `[1, 2]` | refused: "cannot slice \*lang.Call" | `[1, 2]` |
| `print((lambda: [1, 2])()[0])` | `1` | refused: "index requires an inline literal" | `1` |
| `print((lambda: [[1, 2], [3]])()[1][0])` | `3` | refused, same sentence | `3` |
| `print((lambda: {"a": [1, 2]})())` | `{'a': [1, 2]}` | `0` | `{'a': [1, 2]}` |
| `print((lambda: [1, 2])() == [1, 2])` | `True` | refused: "comparing a container with \*lang.Call" | `True` |
| `print(sorted((lambda: [3, 1, 2])()))` | `[1, 2, 3]` | refused: "sorted(…) is refused" | `[1, 2, 3]` |
| `print([v for v in (lambda: [1, 2])()])` | `[1, 2]` | refused: "\*lang.Call does not name a container" | `[1, 2]` |
| `print(f"{(lambda: [1, 2])()}")` | `[1, 2]` | `llc` rejects | `[1, 2]` |
| the same application inside a `def`, a `match` arm, a `while` body, a `finally` body, a method body, a forwarding `def` | `[1, 2]` (the `finally` one also prints `1`) | `llc` rejects, all six | the reference's bytes, all six |
| `print((lambda: [1, 2])()[9])` | `IndexError: list index out of range` | refused: "index requires an inline literal" | `IndexError: list index out of range` |
| `print((lambda: {"a": 1})()["b"])` | `KeyError: 'b'` | refused: "index must be a constant" | `KeyError: 'b'` |
| the same `IndexError` under the program's own `except IndexError:` | `caught` | refused | `caught` |
| `print((lambda x: [x for q in [1, 2]])(9))` | `[9, 9]` | `0` | `[9, 9]` |
| the outermost of three forwarding lambdas answers a container | `[111, 111]` | `0` | `[111, 111]` |
| `print((lambda: [1, 2])() if 1 else [9])` | `[1, 2]` | `llc` rejects | refused in words: `"<lambda>" hands back a container … (roadmap Gap R.67, ADR 0333)` |
| `print(g((lambda: [1, 2])()))` | `[1, 2]` | `llc` rejects | refused in words, same sentence |
| `print((lambda: 5)())` — the negative control | `5` | `5` | `5` (no container built for nothing) |

Three claims that are not stdout, asserted where they live:

* **the IR** — `TestTheHoistedAnswerIsBuiltAtTheReturnAndNeverTravelsAsAGlobal` requires
  `define i32 @gy_lambda_0()`, `call i32 @rt_alloc(` and `ret i32 %` for a list and a dict answer, forbids
  `ret i32 @.` in any module, and for the number answer forbids `rt_alloc` and the container root at all. It also
  counts the `define`s: two of one spelling is the exit-2 shape this row is built to avoid.
* **the pass, before any IR exists** — `TestTheHoistIsDecidedByTheProgramAlone` asks `hoistAppliedLambdaAnswers`
  directly for ten programs (which declarations were minted, how many names were consumed), and
  `TestTheHoistLeavesTheTreeTheFrontEndReportsAlone` proves the caller's node still holds a `*Lambda`, and that
  two runs of one source mint the same names.
* **the regression gate on the declines** — `TestWhatTheHoistLeavesAloneStillAnswersWhatItDid` pins the exact
  bytes and the exact sentences of the programs the pass does not touch, including
  `print((lambda: None)())` → `0` and the pair-road refusal of `(lambda v: [v, 1])(2.5)`. A "gate" that moves a
  program it claims not to touch is not a gate.

Record leg: every program in the answered table is read back out of
`pkg/lang/testdata/interpreter-golden.json` by `runGoldenStdout`, and a *missing* record fails the case, so the
coverage this row bought cannot be deleted by deleting a record (**Gap R.204**, **ADR 0302**). The record went
from 8568 to 8621 sources, all of them cross-checked against the compiled leg by `tools/recmerge`, which refuses
to write an entry its own leg will not agree to.

## Agentic path

The pass now speaks for itself. `gustyc --lambda-answers <src>` prints its own account as JSON —
`tool`, `pass`, `schema`, `rewritten`, `settled`, `declarations`, `symbols`, `declined`, `names_consumed`, `refused` —
described by three new definitions in `gustyc --schema` (`lambdaAnswerReport`, `lambdaAnswerDeclaration`,
`lambdaAnswerDecline`), on the model **ADR 0379** established for `--callables`:

```console
$ gustyc --lambda-answers 'print((lambda: [1, 2])())
def f(x):
    return (lambda: [x])()
print((lambda: 5)())'
{
  "tool": "callable-values",
  "pass": "hoist-applied-lambda-answers",
  "schema": "gusty.lambda-answers.v1",
  "rewritten": true,
  "settled": true,
  "declarations": [{"symbol": "lambda_0", "answer": "list", "params": [], "defaults": [], "line": 1}],
  "symbols": ["lambda_0"],
  "declined": [
    {"shape": "captures_frame", "reason": "the body reads \"x\", which the frame it is applied in owns: … (roadmap Gap R.386, ADR 0370)", "line": 3},
    {"shape": "answers_nothing_provable", "reason": "the body does not answer one container kind along every road through it … (roadmap Gap R.331, Gap R.384, ADR 0333)", "line": 5}
  ],
  "names_consumed": 3,
  "refused": []
}
```

Four things make that a machine path rather than a debug print. **Every application is on one side or the other** —
the frame gate lives in the hoist loop, not in the collection walk, precisely so the document can say which of its two
gates closed instead of being silent about the bodies it never considered. **`shape` is a closed pair**
(`captures_frame`, `answers_nothing_provable`), typed in the schema as an enum, so a script branches on a name rather
than on prose; each `reason` carries the roadmap row that owns the missing road, and no Go type, minted symbol or
pointer value (a test asserts the leak list, as it does for the refusal sentences). **The account and the module are
one claim**: `TestWhatThePassSaysIsWhatTheModuleShows` asks the pass what it minted and then requires
`define i32 @gy_<symbol>()` in the emitted module, so an account describing a hoist the compiler did not perform — the
failure mode that makes an account worthless — is a test failure and not a lie an agent acts on. **A decline is free**:
`rewritten: false` with `names_consumed: 0`, because the emitter's `lambda_%d` counter starts at that number and a
module's text must not move for a rewrite nobody performed (the IR rows that quote a symbol would move with it).

What stayed put is the rest of the interface, and stayed honest: `--json` carries the identical refusal sentence for the
positions that still refuse, `--emit-ast` still shows the `lambda` the program wrote (**ADR 0320**), `--verify` accepts
every module the pass rewrites, and the refusal a script branches on is the same class it was — `unsupported`, exit 1,
naming `<lambda>`, naming **Gap R.67** and **ADR 0333** — now consistent between the two spellings of one program
(`def f(): return [1,2]` in a call argument vs `(lambda: [1,2])()` there).

## Alternatives rejected

* **No account at all ("read the IR if you want to know what the pass did").** Rejected on the repo's own rule that a
  feature ships with a machine path: the question an agent asks is not "what does the module look like" but "did this
  application get a declaration, and if not, which gate closed" — the second of which is not in the IR at all, because a
  declined application looks exactly like a program with no pass. `--callables` is the precedent for answering that
  question as data (**ADR 0379**), and the account is written by the hoist loop rather than by a second walk, so it
  cannot describe a rewrite the compiler did not perform.
* **A container door for lambdas, minted per call site.** Eleven positions, eleven chances to disagree with each
  other; `lambda_answer.go` would have become a second copy of `container_return.go`'s reader set. **Gap R.261**
  is what a family of partly-written doors looks like, and this row's own table shows the payoff: eight of the
  thirty-eight programs were refused by doors that had no arm for a call answering a container, and they needed no
  new arm — only a callee the existing doors could recognise.
* **Teach `emitLambda`'s hoisted `define` to return a container directly.** Rejected on the shape: the answer
  table, the pair road's arity and the text-return scan are all filled from the AST before the emitter mints a
  single name, so a `define` that appeared afterwards is invisible to every one of them — which is precisely the
  bug. Registering per-emission state would have meant the module's IR depending on the order statements were
  lowered in, against **ADR 0273**.
* **Widen `containerAnswerScan` to walk `Lambda` nodes.** A `lambda` in an argument position is not applied
  anywhere; its body may be called from a frame that has nothing to do with where it was written. Registering its
  body's kind as an answer would put a *guessed* kind on a callable with no declaration — a silent wrong answer
  one rung worse than the `0` this row is fixing. The pass registers an answer only for a `lambda` whose
  application it can see and whose body it can prove.
* **Hoist capturing bodies too.** Buys **Gap R.386**'s three programs and loses correctness: a hoisted body reads
  the module, and the module's `x` is not the frame's `x`. The failure would have been invisible — a number, not a
  crash. Kept as a row with its own definition of done (carry a container across a clone the way `pairCallScan`
  carries a pair) rather than as a wrong answer with a passing test.
* **Let `callableWalk` grow three special cases.** Cheaper by ten lines and the second lie about the same AST: a
  hand-maintained list of carriers is exactly how `FStringPart` came to be invisible in the first place.
  **Gap R.383**'s definition of done is to make the reflective descent the ONE reader, which deletes a walker
  rather than adding one.
* **Accept undescribed answers only (or described only).** Both halves of the scan's verdict are needed: the
  described arm is what prints `[1, 2]`, and the undescribed arm is what turns a printed handle into a refusal for
  a body whose arms disagree — dropping either would reintroduce **Gap R.261** from the other side.
* **Fold this into the defunctionalisation pass.** One pass that both clones captures and hoists answered bodies
  reads better in a review and worse in a tracker: the two are asked different questions (`what carries this
  capture?` vs `what does this answer?`), and merging them would have made one ADR answer for two rows and one
  test file for two gates.

## Consequences

* `print`, `str`, `repr`, `len`, `in`, `for`, a binding, a condition, a subscript, a slice, an f-string field, a
  `match` arm, a `while` body, a `finally` body, a method body and a forwarding `def` all answer an applied
  `lambda`'s container the way they answer a `def`'s — one road, and the eleven pins of the answered table are
  proof rather than prose.
* The traps arrived for free, and are pinned as traps: `IndexError: list index out of range` and `KeyError: 'b'`,
  each catchable by the program's own `except`. Before the row neither program reached the run time.
* Six of the eight positions that used to be refused-by-a-door-nowhere-near-it became answers, and two became the
  container door's own sentence — an exit-2 miscompile replaced by a refusal a reader can act on, which is the
  trade **ADR 0166** exists to make.
* `GenerateIRReport` has one more whole-program pass, and one more thing to keep deterministic: it mints its names
  before its rounds and hands the emitter a counter to start above, so no module gets two `define`s of one
  spelling. `TestThePassMintsNoNameTheProgramAlreadyTook` runs a program that declares `def lambda_0():`, one that
  binds it and one that binds it as a `for` target.
* Four new rows entered the tracker, each with a pin that dies when the program lives: **Gap R.383** (the shared
  walk), **Gap R.384** (text / verdict / `None` answers), **Gap R.385** (a ternary arm's container emitted as a
  global), **Gap R.386** (a capture that answers a container).
* `callableWalk` is now known to be short, in writing, in a file its own users cite. The next cycle that touches
  the clone road is told by `TestWhatTheWholeProgramReaderCannotSeeIsTheWalkHole` that the two readers are still
  two.
