# The whole-program reader reads the carriers the AST hides expressions in, and the rewrite keeps a measured veto

**Roadmap:** **Gap R.383** (closed here — the reader that decides what a `lambda` is could not see three of the
AST's carriers) · **L11.7** (functions are values that compile) · **Gap R.389**, **Gap R.390**, **Gap R.391** (the
three rows this cycle measured and filed instead of fixing) · **ADR 0401** / **ADR 0402** (the applied-`lambda`
answer pass, whose own reader `appliedLambdaWalk` was already reflective and whose comment named this hole) ·
**ADR 0345** (the defunctionalisation pass this widens, and its rule that a half-rewrite is never emitted) ·
**ADR 0395** / **ADR 0370** (the entry-capture minting the arm's `lambda` now reaches) · **ADR 0340** (the `key=`
door whose refusal was the visible symptom) · **ADR 0273** (a verdict is a function of the program, and a scan
that declines must not speak over the road that named what it could not carry) · **ADR 0166** (exit 2 is a
compiler bug; a wrong value at the success exit is the class this files; a refusal in words is the answer a gate
keeps) · **ADR 0232** (one question, one asking place — the carrier is named once, by `callableCarrierKindOf`) ·
**ADR 0004** (the pass's account gains `carriers_declined`, and the schema requires it) · **ADR 0302** /
**ADR 0309** (the newly answering sources are recorded from the reference, not self-certified) · **Gap R.327** /
**Gap R.328** (the folded-container answer the arms land on, owned there) · **Gap R.114** (a container in an
interpolation's field) · **ADR 0259** (the frame's trap table, which the handler's row is about)

## Status

Accepted. Landed 2026-10-23.

## Context

One statement, written twice:

```python
def outer():
    base = 1
    return sorted([3, 1, 2], key=lambda v: v + base)   # compiles
```

```python
def outer():
    base = 1
    match 1:
        case 1:
            return sorted([3, 1, 2], key=lambda v: v + base)   # refused
```

```console
$ gustyc --file beside.gy     # exit 1
codegen: `key=lambda ...` is refused: a lambda is a function VALUE, and this backend has none —
`def` bindings are declared, not assigned, so the key has no word to carry to the call (roadmap L11.7)
```

The two programs differ by one indentation level and nothing else. The refusal is the sentence the backend
gives a callable that has no word to travel in — which is a *correct* sentence for a program the pass never
saw, and a wrong one for a program it did.

The pass had not seen it. `callableWalk`, the one traversal on which every question the defunctionalisation
pass asks is answered, descends a field only when the field's type can hold a node:

```go
case reflect.Slice, reflect.Array:
    if !callableIsNodeInterface(f.Type().Elem()) { continue }
```

and `callableIsNodeInterface` asks one question of the element type — `t.Implements(Node)`, where `Node` is
`Span() Span`. Three types in this AST carry expressions and answer no `Span()`:

| carrier | what it holds | what the walk therefore missed |
|---|---|---|
| `MatchCase` | `Pattern`, `Or`, `Guard`, `Body []Stmt` | every call, name and `lambda` in a `match` arm |
| `ExceptClause` | `Exn *Name`, `Body []Stmt` | every call, name and `lambda` in an `except` handler |
| `FStringPart` | `Expr`, `Spec`, `Conv` | every expression between the braces of an interpolation |

They are not nodes, so they are never *visited*; but they are the AST's own storage, so a reader that skips
them is not reading a smaller program, it is reading a program with three regions cut out. **ADR 0401** had
already met this from the other side and fixed it for its own reader (`appliedLambdaWalk`, reflective, with a
comment naming the three carriers and filing this row); **ADR 0402** declined an f-string body for the same
reason in a third reader (`scanFreeIn`). Three readers, three opinions about where the expressions are — which
is what ADR 0232 exists to forbid.

What the hole cost, measured before this cycle touched anything:

| program | parent commit | CPython |
|---|---|---|
| a key `lambda` in a `match` arm | exit 1, `key=lambda … is refused` | `[1, 2, 3]` |
| the same statement beside the arm | exit 0, `[1, 2, 3]` | `[1, 2, 3]` |
| a higher-order argument in an interpolation's field | exit 1, same sentence | `6` |
| the same call outside the interpolation | exit 0, `6` | `6` |
| a key `lambda` in an `except` handler | exit 1, same sentence | `[1, 2, 3]` |

## Decision

**One: the reader reads the carriers.** `callableWalk.fields` now descends a non-node field, slice element or
interface value whose type can reach a node, keeping the three guards the walk already had — the enclosing
`FuncDef`, the `inLambda` counter, and the per-path cycle set with its depth cap. A carrier is *descended*,
never *visited*: it has no node identity to report, and handing a visitor a value that answers none of the node
kinds it switches on would be a new kind of wrong. Whether a type may be entered is one question with one
answer place (`callableCarrierCarriesNodes`, memoised per `reflect.Type`, with a per-question `seen` set so
`*Type` reaches `*Type` and stops). `*Type`, `Span` and `Diagnostic` still cannot reach a node, so the walk
still does not wander into the annotation grammar — the same fact the old filter protected, now asked of a
struct type instead of of a field's interface type.

**Two: the rewrite keeps a veto, and the veto is measured, not inferred.** Seeing a call is not the same
decision as moving one. Each call the read collects is stamped with the carrier it was written inside, and the
roads that mint a clone or point a call at a declaration ask the stamp first. The closed set they refuse is
data — `callableRewriteDeclinedCarriers` — and today it has exactly one name in it:

* `match_arm` — **taken.** Measured answering: `print(sorted([3, 1, 2], key=lambda v: v + base))` inside an
  arm prints `[1, 2, 3]`, the pass reports `rewritten: true` with a minted symbol and one capture parameter,
  and its un-carried twin prints the same bytes.
* `interpolation_field` — **taken.** `print(f"{use(lambda v: v * 3)}")` prints `6` at the CLI and in the compiled leg.
* `except_handler` — **declined.** Cloning the call out of the handler made a program that catches its own
  `ZeroDivisionError` die with it unhandled at exit 3. A refusal at exit 1 beats that, twice over: ADR 0166's
  ordering, and the fact that the refusal is the answer the program had an hour before.

The declined byte was not invented by this cycle. `def answer(): return 7` called from a handler dies the same
way on the parent commit, with no `lambda` anywhere in the program — which is the witness that the missing road
belongs to the frame's trap table (**Gap R.389**, ADR 0259) and not to the reader, and the reason the gate is
written as a refusal rather than as a fix.

**Three: a declined site may not leave a call at a definition that was taken away.** A road that skips a site
leaves the call naming the function the program wrote, and the pass removes that definition once every *other*
site has been pointed at a clone — which is a `call` to a `define` nobody emits, the half-rewrite ADR 0345
forbids. So the invariant at the end of the rewrite asks one more question: does any declined site still name a
function this pass cloned? If it does, the whole program is handed back untouched, with its refusal intact.

**Four: the account says what it refused.** A pass that quietly leaves a call out is indistinguishable from one
that never saw it — which is the exact confusion this row closes. `--callables` gains `carriers_declined`, an
object counting the declined sites by carrier name, and `refused` gains a sentence naming the position, the
line the call sits on and the row that owns the missing road. It is printed on the *decline* path too, where
there is no plan to remember the read, by asking the read again.

## Consequences

`pkg/lang/callable_value.go` gains the carrier descent, the carrier stamp, the closed decline set and the
belt; `pkg/lang/callable_carrier_test.go` is new; the pass's document gains one schema-declared field.

Eleven programs came over from the parent commit's exit-1 refusal to the reference's bytes — eight in a `match`
arm, three in an interpolation's field — and 27 new sources are on the record leg (8668 → **8695** entries,
written by `tools/recmerge` from CPython with the compiled leg cross-checked, which refuses any entry the
compiled leg will not agree to). The exact program this row was filed with is NOT one of the eleven: its answer
is a container handed back by a `def`, which prints the handle for the reason **Gap R.328** owns — the same byte
its un-carried twin printed on the parent commit — so it is pinned, not claimed. Nothing that compiled before changed answer: the two refused shapes that did not become answers
became the *same* wrong byte their own control prints (the folded-container answer, Gap R.327/Gap R.328), which
is what "the arm now behaves like the statement beside it" means when the door below has its own row.

Three divergences surfaced on the way and are filed rather than absorbed, each pinned with today's byte beside
the reference's in **both** suites:

* **Gap R.389** — a call written in an `except` handler loses the frame's trap catch; `print(answer())` in a
  handler turns a caught `ZeroDivisionError` into an uncaught one at exit 3. Pre-existing, parent commit, no
  `lambda` required. This cycle's gate is what keeps the carrier work from converting a refusal into it.
* **Gap R.390** — an interpolation's field over a *folded* container answers the handle (`print(f"{sorted(…)}")`
  prints `0`) or hands the assembler a module global in a `printf` slot (`print(f"{sorted([3,1,2])}")`, the exit-2
  class). Its `def`-key twin answers the same way on the parent commit, which is what makes it the door's and not
  the reader's; it sits beside **Gap R.114** (a bound container in a field) and **Gap R.327**.
* **Gap R.391** — a `match` whose subject is a text takes no arm at all and prints nothing at the success exit
  where the reference prints, measured while writing the carrier witnesses and owned by the `match` road.

## Machine path

`gustyc --callables <src>` prints the pass's account, and the account now says which positions the reader saw
and the rewrite refused:

```json
{
  "tool": "callable-values",
  "pass": "defunctionalize-callables",
  "rewritten": false,
  "carriers_declined": { "except_handler": 2 },
  "refused": [
    "defunctionalization declines: … ; a call of a callable written inside an `except` handler at line 7 keeps
     the ordinary road's refusal: pointing that call at a declaration this pass minted moves it out of the frame
     whose trap catch the handler is registered on, and the program that catches its own error today would die
     unhandled at exit 3 (roadmap Gap R.389)"
  ]
}
```

`carriers_declined` is `{}` for every program that crosses no carrier, which is the ordinary case; the key set
is closed and small, so a script branches on the key and reads the sentence for the line and the row. No Go
type, minted symbol or pointer value appears in either (ADR 0166, ADR 0345 §3). The field is required in the
schema's `definitions.callablePassReport`, and `TestTheCallablesDocumentIsTheOneTheSchemaDeclares` fails if the
payload and the document disagree.

## IR implications

No new instruction is emitted for a carrier — which is the point: a `lambda` in an arm reaches the roads that
already existed, and the emitted module for the arm's copy is the module the un-carried twin always emitted,
`define i32 @gy_sorted$lambda$…()` beside a call that names it. The gate prevents the module that a handler's
clone would have produced, which was a `call` whose frame had lost its entry in the trap table (ADR 0259) and,
in the mixed shape, a `call` to a `define` that was marked unemittable. `print(f"{sorted([3,1,2])}")` still
emits `i32 @.lst1` into a `printf` slot and `llvm-as` still rejects it: that is **Gap R.390**'s pin, held
deliberately, in the module text rather than in prose.

## Alternatives rejected

* **Give `callableWalk` a hand-written switch over statement kinds.** That is the shape that produced the hole:
  a list someone maintains, which stops being complete the day a construct is added. The closed set in this ADR
  is over *positions the rewrite may not cross*, which is a decision, not a description of the tree; the
  description of the tree is reflection, and a test reads `ast.go` to keep the two apart.
* **Fix the trap table here too.** Tempting, and it would have let the handler through the gate. Rejected: the
  escaped `ZeroDivisionError` reproduces on the parent commit with no `lambda` in the program, so the missing
  road is ADR 0259's table crossing (Gap R.389), and a reader fix that also rewrote the trap machinery would
  have left the suite unable to say which of the two earned a green row.
* **Take the handler position and pin the exit-3 byte.** A silent wrong answer at the success exit — or worse, an
  uncaught trap — is the class ADR 0166 says this toolchain files; a refusal the program already had is not a
  regression, and ADR 0273's rule that a gate must not cost a program that compiles today cuts the other way
  when the program in question does not compile today.
* **Leave the handler invisible to the reader, so no gate is needed.** That keeps the bug this row is about, in
  the one carrier where it is cheapest to keep: the reader would still be reading a program with an `except`
  body cut out, and every other question the pass asks — which names are bound, which calls exist, which
  functions are used as values — would go on being answered from half the tree.
* **Patch the symptom in the key door** (`call_key.go`'s refusal) so the arm's `lambda` reaches the hoist
  without widening the walk. Rejected: the door was refusing an unseen callable *correctly*; the blindness is
  upstream, and a second refusal-site patch would have made the walk's completeness untestable.
* **Trust `appliedLambdaWalk` for this pass too, instead of making `callableWalk` reflective.** Two walkers that
  agree is still two walkers, and ADR 0402's `scanFreeIn` is the third copy of the same blindfold. What this
  cycle did to `callableWalk` is what the next cycle owes those two.

## Tests

`pkg/lang/callable_carrier_test.go` — five cases, all legs:

* `TestWhatTheWholeProgramReaderSeesNowAnswers` — the answered family, each row with the un-carried twin that
  must print the same bytes, on the compiled leg and against the record (`runGoldenStdout`).
* `TestWhatTheGateRefusesNamesTheCarrierAndTheRow` — the handler positions: the document declines them
  (`carriers_declined.except_handler`), names the line and `Gap R.389` in `refused`, and the program is refused.
* `TestTheGateNeverLeavesACallAtADefinitionThatWasTakenAway` — the mixed shape (one call in an arm, the same
  hook in a handler): no `call` to a symbol the module never defines, and no `llc` rejection.
* `TestWhatTheCarrierFamilyStillAnswersWrongIsFiledNotFixed` — Gap R.328, Gap R.390 (two shapes, one of them the
  global-in-a-word-position read out of the module text) and Gap R.389's escaped trap, each pinned with today's
  byte and deleted, not edited, when it stops being true.
* `TestTheASTCarriesExpressionsInExactlyTheseThreeNonNodeTypes` — reads `ast.go` and asserts the carrier set is
  exactly `MatchCase`, `ExceptClause`, `FStringPart`: a fourth carrier fails the case and demands a decision
  about the rewrite, and a carrier that grows a `Span()` fails it from the other side.

`integration/lambda_carrier_test.go` — the reference leg at the CLI: `TestTheLambdaCarrierFamilyAnswersLikeTheReference`
runs the carrier family — the arm's and the field's programs that came over from the parent commit's refusal, the
un-carried twin of each, and the control shapes a reader change must not disturb (a carrier with no callable in it,
a recursion in an arm) — against the bytes `python3` prints; `TestTheLambdaCarrierDeclinedCarriersRefuseRatherThanDie`
demands exit 1 for the four handler programs with exit 2 and exit 3 both forbidden — an uncaught trap there is
**Gap R.389**, and a pass that stops declining turns those four rows red;
`TestTheLambdaCarrierFamilyIsOnTheRecord` fails if any answered program's record disappears;
`TestWhatTheCarrierRowMeasuredAndDoesNotOwnIsPinnedWithTheReferencesAnswer` pins the divergences with their exit
codes; and `TestTheLambdaCarrierProgramIsTheReferenceLineForLine` walks
`integration/programs/lambda_carrier.gy` (matrix row `programs/lambda_carrier`) line-for-line against the reference,
its header saying out loud which carriers it does not walk.
