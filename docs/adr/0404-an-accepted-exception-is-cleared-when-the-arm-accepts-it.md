# An accepted exception is cleared when the arm accepts it, and the handler's callable stops needing a veto

**Roadmap:** **Gap R.241** (closed here — an `except` arm that CALLS takes the run down with the exception it
already handled) · **Gap R.389** (the same fault read from the callable side, closed with it) · **Gap R.383** /
**ADR 0403** (the carrier reader whose handler veto this removes) · **L11.7** (functions are values that compile) ·
**ADR 0213** (the arms are dispatched in source order) · **ADR 0218** (the edge-out clear this keeps as a belt) ·
**ADR 0259** (the frame's trap table, whose entries the arm's calls are keyed to) · **ADR 0166** (exit 2 is a
compiler bug; a wrong answer at the success exit is the class this files; a refusal is only worth keeping while the
fault behind it stands) · **ADR 0273** (a verdict is a function of the program — a gate keyed on a position rather
than on a proof is the shape to avoid) · **ADR 0232** (one question, one asking place) · **ADR 0004** (the account
field is schema-declared) · **ADR 0302** / **ADR 0309** (the newly answering programs are recorded from the
reference, never self-certified)

## Status

Accepted. Landed 2026-10-23. Supersedes the `except_handler` restraint of **ADR 0403** and renames that cycle's
account field `carriers_declined` to `carriers_read`.

## Context

```python
def answer():
    return 7

try:
    z = 1 / 0
except ZeroDivisionError:
    print(answer())
print("after")
```

The reference prints `7` and `after` and exits 0. This backend printed **nothing** and died at **exit 3** with the
`ZeroDivisionError` the handler existed for — the same death on the parent commit, and on the `lambda`-free program
above, which is the evidence that made it nobody's callable bug:

| program (handler catches, arm calls) | compiled (parent) | CPython |
|---|---|---|
| `print(answer())` where `answer` is a `def` | **exit 3**, traceback on stderr, stdout empty | `7`, `after` |
| `h()` where `h` prints (roadmap Gap R.241's own row) | `caught`, then **exit 3**, `after` never runs | `caught`, `after` |
| `print("caught")` — no call | `caught`, exit 0 ✅ | same |
| `print(len([1, 2]))` — a builtin, not the program's `define` | `2`, exit 0 ✅ | same |

So the handler is reached, and the difference is a call that lowers to a `define` of the program's own. Those are
the calls that end with `checkExn` (`pkg/lang/codegen_error.go`): a load of `@exn_flag` and a branch to the
innermost handler — or, the handler stack being popped before the arms are emitted, to the function's raise-exit.

And the flag was still set. `tryStmt` cleared it in exactly one place — on the edge OUT of an arm that had taken the
exception:

```go
if !armed {
    // The arm took the exception, so the exception is over.
    g.clearExn(b)
    b.WriteString("  br label %" + finally + "\n")
}
```

Which is correct as far as it goes (**ADR 0218**: an arm that leaves by `return`/`break`/`raise` must not leave the
flag set) and too late by one whole arm: everything between the arm's first statement and its last ran under a flag
saying "an exception is in flight". The arm's own call therefore looked like the raise, and the run took the
exception on a second round — in the shape where the arm's statement is `print(answer())`, before anything printed.

**ADR 0403** had met this from the other side. Its reflective reader made an `except` handler's calls visible to the
defunctionalisation pass for the first time, the clone roads then offered to point one at a minted declaration, and
the same program died the same death one rung earlier. That cycle wrote a veto — `callableRewriteDeclinedCarriers`
holding `except_handler`, `carriers_declined` in the account, a refusal sentence naming the position and the line —
and filed the fault as **Gap R.389**. That was the right call at the time, because the fault was unexplained and a
refusal at exit 1 beats an uncaught trap at exit 3 (**ADR 0166**). It was also, as it turned out, a gate in front of
the wrong door.

## Decision

**Clear the exception on the accepting edge, before the arm's first statement.** One store, in `pkg/lang/
codegen_error.go`'s `tryStmt`, immediately after the arm's label and before its body is emitted:

```go
b.WriteString(armBody + ":\n")
// The arm is reached BECAUSE @exn_flag is set, and reaching it means the exception has been taken. …
g.clearExn(b)
armed := false
```

The edge-out clear stays. It is not redundant: it is the **ADR 0218** case, an arm that leaves by a path the entry
clear cannot speak for (a `return` in the middle of a body whose earlier statements were emitted under a flag this
arm never set), and deleting it would trade one silent class of stale flag for another.

**With that, the handler's callable needs no veto, and the veto goes.** `callableRewriteDeclinedCarriers`, the
`noteDeclinedCarrier` skip in the four clone roads, the decline sentence and the `carrierCrossesAClone` belt are
DELETED, not left empty — an empty gate is a claim with no witness, and the witness it had (`try:` /
`    print([1, 2][5])` / `except IndexError:` / `    print(apply(lambda x: x * 2, 3))` dying at exit 3) now prints
`6`. What survives is the part that was never a gate: the carrier stamp on each call the reader met, reported as
`carriers_read`, which is an account of how far the reader reached rather than a record of what it refused.

Renaming the field rather than adding a second one is deliberate. A `carriers_declined` that can only ever be `{}`
invites a reader to conclude something was refused; the useful question at this altitude is "did this pass read the
call inside the arm at all", and that is what the field now answers.

## Consequences

`pkg/lang/codegen_error.go` gains one `clearExn` call and the comment that says why; `pkg/lang/callable_value.go`
loses the veto and renames the account; `pkg/lang/schema.go` renames the required key.

Measured, against a parent-commit build (the tree at **ADR 0403**) and `python3`, both binaries from the same tree:

* roadmap Gap R.241's own rows and the two this cycle added print the reference's two lines at exit 0 — the `h()`
  arm prints `caught` **and** the run continues to `after`;
* **twelve** of the fourteen newly recorded programs came over from the parent commit's answer to CPython's bytes:
  **five** from an exit-1 refusal (`sorted(key=lambda …)` in a reached arm, two `apply(f, v)` shapes, two
  `use(lambda v: …)` shapes) and **seven** from an exit-3 uncaught trap (three `print(answer())` shapes, two `h()`
  shapes, two applied `lambda`s). Five of the twelve contain no `lambda` anywhere — Gap R.241's own shape, a plain
  `def` called from an arm — which is the evidence that the fault was the flag and not anybody's callable pass. The
  other two recorded programs are the controls (a builtin in the arm, a nameless arm) that answered before this
  cycle and still do;
* fifteen exception shapes were run against the reference and none moved: an unmatched exception still propagates
  to the outer arm and still exits with a traceback, `finally` still runs after a caught error and after a call in
  the arm (that one was itself broken: `h()` in an arm under a `finally` printed `arm` and died before `finally`), a
  `raise` inside an arm still travels outward (its flag is set after the entry clear, and nothing clears it on that
  path), a nested `try` still hands off, and `print(1/0)` with nothing to catch it still dies — twelve shapes
  byte-identical to the parent build, three newly answering;
* 14 newly answering programs went onto the record leg (**8695 → 8709**), each written from CPython by
  `tools/recmerge` with the compiled leg cross-checked, which is the only way these rows get recorded at all.

## Machine path

`gustyc --callables <src>` reports the reader's reach:

```json
{
  "tool": "callable-values",
  "pass": "defunctionalize-callables",
  "rewritten": true,
  "carriers_read": { "except_handler": 2 },
  "symbols": ["use$callable$1a2b3c4d_0"],
  "refused": []
}
```

`carriers_read` counts the calls the reader met inside each carrier — `match_arm`, `interpolation_field`,
`except_handler` are the whole key set — and `{}` means the program crossed no carrier. The count is over DISTINCT
call nodes (`print(...)` and `use(...)` above, one each): the walk meets a call once per route to it and the roads
ask once per candidate, so a raw visit count would be a number nothing in the source can be compared with. Nothing
in the document says a position was refused any more, because nothing is; if a future measurement condemns a
position, the veto is a table away, together with the sentence and the belt, and the tests demand the failing
program before the table is allowed an entry. No Go type, minted symbol or pointer value appears in the document
(**ADR 0345** §3); the key is required in `definitions.callablePassReport` and
`TestTheCallablesDocumentIsTheOneTheSchemaDeclares` fails if the payload and the document disagree (**ADR 0004**).

## IR implications

One `store i32 0, i32* @exn_flag` per accepting arm, ahead of the arm's body. The shape of the module is otherwise
unchanged: no new runtime symbol, no new instruction, no change to the raise path, and no `fnptr`/indirect `call`
anywhere (**ADR 0345**'s ban holds). `TestRaiseInsideAnArmDoesNotGetCleared` was rewritten to assert the ordering
that carries the weight — the flag may be cleared when the arm is entered, and must not be cleared after the arm's
own raise — because the old claim ("a module that clears the flag anywhere can swallow the raise") was an artefact of
there being exactly one clear and in the wrong place.

## Alternatives rejected

* **Clear the flag at the arm's end only, and teach `checkExn` to look the other way inside an arm.** Tempting and
  local, and it leaves the semantics wrong: an arm that RAISES must be able to set the flag and be believed by the
  code that follows, so a blanket "no checks while inside an arm" would silently swallow the arm's own errors. The
  flag is the state; the fix is to not be holding it when you did not raise it.
* **Keep the veto and pin the four handler programs as refusals.** That is where **ADR 0403** left them, and it was
  defensible while the fault was unnamed. Keeping a refusal after the fault is fixed buys a red suite and nothing
  else: the four programs answer, `TestTheReaderSeesInAHandlerNowAnswers` asserts it, and a gate whose only remaining
  purpose is to keep a passing program refused is the ADR 0273 shape — a verdict keyed on where a call was written
  rather than on what the program proves.
* **Leave `carriers_declined` in place, always empty, and add `carriers_read` beside it.** Two fields where one
  lives, and the empty one is a claim that something was declined. The schema requires its keys; a required key that
  can never be non-empty is documentation that lies.
* **Fold the entry clear into `clearExn`'s edge-out call site as a single helper.** The two clears are on different
  paths for different reasons — one says "this arm accepted it", the other "this arm is leaving by its own route" —
  and collapsing them is how the **ADR 0218** case gets quietly dropped the next time someone tidies.
* **Fix the trap table (**ADR 0259**) instead.** Not needed and not this: the table entries are keyed to the calls
  the program wrote and were never the broken half — with the flag cleared, a cloned call inside an arm lands its
  raise on the frame the program asked for, measured by the nested-`try`, second-arm and `finally` rows above.

## Tests

* `pkg/lang/exception_clear_test.go` — the module-shape half: a clear is emitted, the propagate path still sets the
  flag, the clear on a `return`ing arm precedes the `ret`, and no clear follows an arm's own raise.
* `pkg/lang/callable_carrier_test.go` — `TestWhatTheReaderSeesInAHandlerNowAnswers` holds five handler programs
  compiled, on the record, and in the account (`carriers_read.except_handler`), each beside its un-carried twin;
  `TestEveryCallTheAccountNamesIsADefinitionTheModuleCarries` replaced the veto's belt with the invariant the veto
  was protecting (no `call` to a `define` the module does not emit) and asks it of a program with an arm, a handler
  and an interpolation's field all calling the same hook; `TestTheASTCarriesExpressionsInExactlyTheseThreeNonNodeTypes`
  now asserts the reader's closed set and that each carrier name is REACHED by a probe, and that no carrier is
  declined — which it checks by asking the handler's account to be `rewritten` with a minted symbol.
* `integration/slice_bounds_test.go` — `TestACallInsideAnExceptArmRunsAndTheRunContinues` (the two rows
  **Gap R.241** was filed with, plus the builtin and nameless-arm controls and a call whose answer the arm's `print`
  consumes), each row's expected bytes taken from `python3` in the case.
* `integration/lambda_carrier_test.go` — the handler programs moved out of the declined table (deleted) into the
  answered family against CPython; the pinned rows that recorded the escape are deleted, not edited. The
  conformance corpus's `programs/lambda_carrier.gy` gained the handler section its own header used to rule out,
  reached by the program's own traps, and still prints the reference's bytes line for line.
