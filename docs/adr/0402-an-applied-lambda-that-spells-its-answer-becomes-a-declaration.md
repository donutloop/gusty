# An applied `lambda` that spells its answer becomes a declaration, so the three printers a `def` uses answer it

**Roadmap:** **Gap R.384** (closed here — `print((lambda: True)())` printed `1`, `print((lambda: None)())` and
`print((lambda: "hi")())` printed `0`, all at **exit 0**, while the `def` twin of each printed the reference’s
bytes) · **L11.7** (functions are values that compile) · **Gap R.387**, **Gap R.388** (the two rows this cycle
measured and filed instead of fixing) · **ADR 0401** (the pass this widens, and the rule it established: give the
`lambda` the one thing the doors can read rather than writing a door per position) · **ADR 0333** / **ADR 0344**
(the container-answer scan whose verdict remains the first question the gate asks) · **ADR 0368** (`answerKindPlan`,
which registers what a `def` hands back and registered nothing for what a `lambda` hands back) · **ADR 0366**
(`textReturningCallables`, the text arm of the same registration) · **ADR 0257** (the bool arm — the print door
that asks the AST whether an expression is a verdict) · **ADR 0302** (the newly answered sources are recorded from
the reference, not self-certified) · **ADR 0309**/`tools/recmerge` (47 entries written from CPython, every one
cross-checked against the compiled leg, none written by it) · **ADR 0333**’s sibling **Gap R.333** (the text arm’s
missing runtime block, which the hoist removes rather than patches) · **Gap R.383** (the reader that cannot see an
interpolation’s field, and the reason this gate does NOT take an f-string body) · **Gap R.171**, **Gap R.331**
(the two void/text shapes this row pins rather than owns) · **ADR 0166** / **ADR 0212** (a wrong value at exit 0 is
the class this files; exit 2 is the class it removes) · **ADR 0004** (the pass’s account gains three answer words
and the schema requires them) · **ADR 0273** (a verdict is a function of the program — the new question is a pure
AST read, asked in the same round as the old one) · **ADR 0232** (one question, one asking place: the frame
question stays `lambdaFreeNames’`)

## Status

Accepted. Landed 2026-10-23.

## Context

One family, asked of the reference and of this toolchain:

```console
$ python3 -c 'print((lambda: True)()); print((lambda: None)()); print((lambda: "hi")())'
True
None
hi
$ gustyc --file kinds.gy
1
0
0
```

Three bodies the reference answers with one word each, and this toolchain answered with the number the object
arrived in: a verdict’s `1`, a void’s `0`, and a text’s `@str_tab` position. No trap, no refusal, no diagnostics —
the exit code of success. The same three bodies written as `def`s have printed `True`, `None` and `hi` for weeks:

```console
$ gustyc --file 'def f(): return None' …    # prints None
```

because the print door does not decide “what kind is this call’s answer?” from the payload. It asks three doors
that read the **declaration**: `textReturningCallables` (**ADR 0366**) for a text, `IsBoolExpr`’s call arm
(**ADR 0257**) for a verdict, and `fdReturnsValue` for the void, with `answerKindPlan` (**ADR 0368**) storing the
tag beside a `return` that needs one. An applied `lambda` has no declaration for any of them to read — the emitter
mints one (`emitLambda`) in the middle of lowering the statement that contains it, by which time every whole-program
table has been filled. Its answer therefore fell through to the road that keeps ONE `i32` for a value, which is the
road that cannot carry a kind.

**ADR 0401** had closed the container arm of exactly this hole, eighteen hours earlier, and had left these three
outside its gate on purpose: that gate admits only what `containerAnswerScan` proves, and a widening by analogy
would have been a claim that the text road behaves like the container road — the kind of claim this repo files
rather than assumes. **Gap R.384** was that row, with the three programs pinned beside the reference’s answers.

## Decision

**One question added to the pass’s answer half; no door, printer, tag or runtime helper written.**

`appliedLambdaProved` asked one question — does **ADR 0333**’s scan read this declaration as answering a container,
described or undescribed? It now asks a second, and a body that satisfies either is a keep:

```go
// appliedLambdaBodyRegisters is the answer half's second question, and it is asked of the ONE spelling both
// roads share — `lambdaFuncDef`'s single `return` of the body …
func appliedLambdaBodyRegisters(lam *Lambda) string {
	switch lam.Body.(type) {
	case *StrLit:  return lambdaAnswerText   // "text"
	case *BoolLit: return lambdaAnswerBool   // "bool"
	case *NoneLit: return lambdaAnswerNone   // "none"
	}
	return ""
}
```

A keep means what it meant before: the application’s callee becomes a name, the module grows a `FuncDef` with
`CloneOf = <lambda>`, and the answer is registered, materialised and read by the doors that already answer for a
`def`. There is still no “lambda answer” road to fall out of step with, because there is still only the answer road —
which is **ADR 0401**’s rule, applied one rung up rather than widened by analogy.

Three things the gate will not take, all of them load-bearing:

* **a number or a float.** Their one-word road answers correctly — `print((lambda: 5)())` is `5`,
  `print((lambda: 2.5)())` is `2.5`, both pinned as controls in three suites. A hoist there buys nothing and risks
  the pair road, the double-domain `define` and **ADR 0370**’s float-frame gate.
* **an answer the source does not spell.** A body that returns a `*Name`, a `*Call`, a comprehension or an
  interpolation is the scans’ business or no business at all; guessing a kind from a callee the gate has not read
  is the difference between a door and a broom (**Gap R.387**, filed with the four programs this leaves wrong).
* **an interpolation body, until the frame reader can see one.** This is the discovery of the cycle, and it is why
  the arm is shut rather than merely unclaimed: `lambdaFreeNames` → `scanFreeIn` filters slice fields by
  `nodeField`, the same filter **Gap R.383** filed against `callableWalk`, and `FStringPart` is not a `Node`. A body
  of `f"{x}"` therefore reports *no* free names, the frame gate opens, and the hoisted `def` reads the module’s `x`
  where the program wrote the frame’s:

  ```python
  x = 99
  def f():
      x = 7
      return (lambda: f"{x}")()   # CPython: 7
  ```

  Taking that arm today would be **ADR 0166**’s wrong answer at **exit 0**, produced by the pass whose job is to
  stop wrong answers. The f-string arm moves when the WALK moves.

## What this row deliberately did not touch

`print((lambda: None)() is None)` still answers `False`, and its `def` twin (`def f(): return None` /
`print(f() is None)`) answers `False` too. The row that owns it is **Gap R.388**, filed here: a body that falls off
its end stores the run-time `TagNone` word and `is None` is `True`; a body that WRITES `return None` stores nothing
and is not in `answerKindPlan`’s `void` set either. What makes it a finding rather than a footnote is that
**Gap R.228**’s own Definition of done reads “`print(f())` writes `None` on both legs **and `f() is None` is
`True`**” — the first half holds for both spellings, the second holds only for one, and no test anywhere asked the
question. A DoD cell is a claim; a claim nobody runs is a claim that quietly stops being true.

The void handed to a forwarding parameter (`def f(v): return v` / `print(f((lambda: None)()))` → `0`) is
**Gap R.171**’s and the `def` twin diverges identically; it is pinned in `lambdaAnswerFiledNotFixed` as the PARITY
it is, not as this pass’s answer.

## Measured

A 44-program family run against `python3` before and after, on both the linked CLI leg and the in-process compiled
leg. Before: **22 of 44 disagreed**. After: **43 of 44 agree**, and the 44th is **Gap R.388**’s.

| shape | before | after | CPython |
|---|---|---|---|
| `print((lambda: True)())` | `1` | `True` | `True` |
| `print((lambda: False)())` | `0` | `False` | `False` |
| `print((lambda: None)())` | `0` | `None` | `None` |
| `print((lambda: "hi")())` | `0` (CLI) / `llc` rejects the module (compiled leg) | `hi` on both | `hi` |
| `x = (lambda: "hi")()` / `print(x)` | `0` | `hi` | `hi` |
| `print(str((lambda: "hi")()))`, `repr(…)` | refused / `0` | `hi`, `'hi'` | same |
| `print((lambda: "hi")().upper())`, `len(…)` | refused (`Gap I.2`, `len requires an inline literal`) | `HI`, `2` | same |
| `print((lambda: "a")() + "!")` | refused | `a!` | `a!` |
| `print([(lambda: True)(), (lambda: None)()])` | `[1, 0]` | `[True, None]` | `[True, None]` |
| `print({"k": (lambda: "hi")()})` | `{'k': 1}` | `{'k': 'hi'}` | `{'k': 'hi'}` |
| `print({(lambda: "k")(): 1})`, `{(lambda: "k")()}` | `{0: 1}`, `{0}` | `{'k': 1}`, `{'k'}` | same |
| `def f(v): return v` / `print(f((lambda: "hi")()))` | `0` | `hi` | `hi` |
| `for c in (lambda: "ab")(): print(c)` | `llc` rejects the module | `a` / `b` | same |
| `print(f"{(lambda: True)()}")` | `1` | `True` | `True` |
| `print((lambda: 5)())`, `print((lambda: 2.5)())` | `5`, `2.5` | `5`, `2.5` | **controls — untouched** |
| `print((lambda: None)() is None)` | `False` | `False` | `True` — **Gap R.388** |

The text row’s two-face failure is the one to read carefully, because it is why the pin was written from the wrong
leg the first time it was filed: through the CLI the program LINKED and printed `0`; through the in-process leg the
module never reached the run time, `llc-20` refusing it with `use of undefined value '@rt_str_intern2'`. The hoist
removes the second face rather than patching it, because the hoisted `return` interns on the road that sets the flag
the heap block is gated on (and the block’s presence is additionally derived from what the module references —
**ADR 0209**’s rule, doing its job once the emission moved).

47 sources went to the record through `tools/recmerge`: 36 for the `pkg/lang` family, 11 more for the CLI family.
The recorder asks the reference and cross-checks the compiled leg, so a source whose two legs disagree cannot be
written — which is why **Gap R.387**’s four programs are in this ADR as pinned divergences and not in that file.

## Agentic path

`gustyc --lambda-answers '<src>'` (and the `lambdaAnswerReport` definition in `--schema`) now says WHICH answer
each declaration proved, in the vocabulary the `def` roads use:

```console
$ gustyc --lambda-answers 'print((lambda: "hi")())' --json | jq '.declarations[0]'
{ "symbol": "lambda_0", "answer": "text", "params": [], "defaults": [], "line": 1 }
```

`answer` is `list`/`dict`/`set`/`container` for the container arm (unchanged) and `text`/`bool`/`none` for the
three this cycle added. A number answer is still `declined` under `answers_nothing_provable`, and the sentence was
rewritten so the decline TEACHES something: a number and a float are declined because the one-word road renders
them correctly, and a name, a call or an operation is declined because no scan can name a kind for a `lambda` yet —
with the rows (`Gap R.331`, `Gap R.386`, `Gap R.387`) named for the agent to read. `TestTheSchemaDeclaresTheLambdaAnswerAccount`
keeps payload and schema one claim, and `TestWhatThePassCallsAVerdictATextAndTheVoid` requires each of the three
words and requires the module to carry the `define` the account names.

## Alternatives rejected

* **A print-door arm per kind** — three `if`-arms in the print road for “the callee is a hoisted lambda”. Rejected
  for the reason **ADR 0401** rejected eleven: the print door is one of eleven positions that read a call’s answer,
  and a fix that lives in one of them is a fix that makes the other ten disagree. The `--eval` run that prints
  `True` and the `str()` run that prints `hi` are the same object asked the same question.
* **`g.heapUsed = true` at the closure road’s interning call** — the one-character fix for the text arm’s `llc`
  rejection. Rejected because it treats the symptom of a road that emits a `define` into a side buffer the
  reference-derived gate reads late, and because it would have left the text answer printed by the number door —
  the wrong value with a valid module.
* **Widening the gate to a body that is a `*Call`.** Measured first (four programs, above), then refused: the gate
  would have to read the callee’s registration, and the door that owns that reading is `answerKindPlan`, which
  already forwards a callee’s word for `return other(...)`. Writing a second reader of a body’s returns is the
  **Gap R.383** mistake. The row’s definition of done names the door to reuse.
* **Widening the gate to an `*FString` body.** The `def` twin answers, the change is one `case`, and it is refused:
  the frame gate’s own reader cannot see into an interpolation, so the hoist would silently rebind a name
  (`x = 99` / `x = 7` above). Filed inside **Gap R.387** with that counter-example.
* **Routing every answer through the tag.** The honest destination — **L11.1**’s tagged value word — and not this
  rung: it changes every call in the language, and the row above it is 43 programs.

## Consequences

* `print`, `str`, `repr`, an f-string field, a binding, a condition, `len`, a membership test, a loop iterable, a
  call argument, a container element, a dict key and a dict value all answer the family, because they were never
  missing anything — they were being asked about a callee they could not see.
* The pass’s answer half is now two questions, and both are asked of the program AFTER the rewrite in the same
  round, so the rounds still only ever remove a hoist and the module’s text still cannot depend on how many of them
  ran (**ADR 0273**).
* The emitter’s `lambdaCounter` still starts above every spelling the pass took; three collision rows (`def
  lambda_0`, a binding, a `for` target) still pin it, and the duplicate-`define` scan runs over every row of the IR
  table.
* 47 sources are on the record (the artifact to **8668**), written from CPython by `tools/recmerge`; the record count
  in `AGENTS.md`, `docs/operations.md`’s artifact table and the roadmap Snapshot row moved to the file’s own number,
  which is what the three guards named one by one (**Gap R.204**).
* `integration/programs/lambda_answer_kind.gy` joined the conformance corpus as a line-for-line `match` row, its own
  header saying what it does NOT contain.
* Two rows came out from under the row and are pinned with today’s byte beside the reference’s: **Gap R.387**
  (a body whose kind lives in a callee or an interpolation — four programs, two suites) and **Gap R.388**
  (the void spelled `return None` compared with `None`, with **Gap R.228**’s unfetched DoD claim quoted at it).
