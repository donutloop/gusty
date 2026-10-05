# ADR 0284 — how many arguments there were is one question, asked on the road every caller shares

Date: 2026-10-06
Status: Accepted
Roadmap: closes **Gap R.168**.
Depends on: ADR 0283 (which reordered the call roads so a declared `def` keeps the checked road, and
filed this row), ADR 0215 (the wording of a trap is a contract), ADR 0211/0228 (a trap of the wrong class
catches nowhere), ADR 0166 (exit 2 is the compiler's bug; a wrong number at exit 0 is worse).

## The measure

`python3` 3.12.3 is the oracle. All three left-hand programs are ordinary; all three printed a number at
**exit 0** on `--interp`:

| program | CPython | `--interp` before | `--interp` after | `--aot` (both) |
| --- | --- | --- | --- | --- |
| `g = lambda x: x * 2` / `print(g(1, 2))` | `TypeError: <lambda>() takes 1 positional argument but 2 were given` | **`2` at exit 0** | exit 3, names it | exit 1 refusal |
| `g = lambda x: x * 2` / `print(g())` | `TypeError: … missing 1 required positional argument: 'x'` | **`0` at exit 0** | exit 3 | exit 1 refusal |
| `g = lambda x, y: x - y` / `print(g(3))` | `TypeError: … 'y'` | **`3` at exit 0** | exit 3 | exit 1 refusal |

The controls, all still exact on both engines: defaults `g(1)`/`g(1,5)`/`g(1,5,6)` → `6`/`9`/`12`,
keywords out of order `g(c=9, a=1)` → `12`, a computed default, a lambda called correctly → `42`, a
lambda through a parameter → `12`, a nested `def` → `11`, a method → `5`, recursion `fact(5)` → `120`,
five parameters → `15`.

The compiled backend already refused every one of these (`too many arguments for lambda_0`,
`missing argument "x"`). So this row is not a refusal bought with an answer — it is two engines brought
into agreement by fixing the one that was **inventing numbers**.

## The diagnosis

The interpreter has two roads into a call. The checked road — a literal call to a `def`'d name — counts:
it walks `n.Args`, tracks `argSet`, rejects `pos >= len(fd.Params)` and reports an unset parameter. The
other road, `callClosure`, is how a callable read out of a *variable* is called (`g = lambda x: x * 2`),
and it asked nothing at all:

```go
func (e *Evaluator) callClosure(o *obj, n *Call) (int64, error) {
	argVals := make([]int64, len(n.Args))     // however many there were
	for i, a := range n.Args { argVals[i], err = e.eval(a) }
	return e.callFunc(o.fn, argVals, o.env, stmtRootCall)   // no count, no question
}
```

and `callFunc`'s bind loop is written for the *lenient* case:

```go
for i, p := range fd.Params {
	if i < len(argVals) { av = argVals[i] }        // extras are simply never indexed
	else if p.Default != nil { … }                  // …and a missing arg with no default
	scope[p.Name] = av                              // keeps av == 0, the zero value
}
```

Extras are ignored because the loop walks *parameters*, not arguments. A missing argument without a
default falls through to `av`'s zero value. That is why the answers were believable: `g(1, 2)` computed
`1 * 2` and `g()` computed `0 * 2` — arithmetic on values that were never passed.

This is the same code path ADR 0283 stepped around by reordering the roads, and the row it filed. The
reorder made `def` calls safe; a lambda in a variable was already broken and stayed broken.

## The decision

**Ask the question once, in `callFunc`** — the road every caller shares — rather than adding a count to
`callClosure`:

```go
if len(argVals) > len(fd.Params) {
	return 0, &EvalError{Msg: fmt.Sprintf("too many arguments for %s: it accepts %d argument%s, got %d", …)}
}
for i := len(argVals); i < len(fd.Params); i++ {
	if fd.Params[i].Default == nil {
		return 0, &EvalError{Msg: fmt.Sprintf("missing argument %q for %s", fd.Params[i].Name, …)}
	}
}
```

Placed before the bind loop, and the bind loop unchanged. Placement is the whole design: a fifth road
added later (an `await`-style trampoline, a decorator wrapper, a `functools`-shaped helper) inherits the
question instead of having to remember it. A guard beside one caller is how this hole opened.

**Defaults are not an error, and only the arity is.** The loop starts at `len(argVals)`, so a trailing
parameter with a default is filled exactly as before; keyword arguments are still resolved by the checked
road that already handles them. Nothing here tightens what a *correct* call may look like — which is what
the eleven-line control probe exists to prove.

**One sentence for both roads.** The checked road's `too many arguments` / `missing argument x` became the
shared sentence too, because ADR 0215 makes the wording a contract and one program must not read two ways
depending on whether its callee was written with `def` or with `=`. A callee is named the way the reference
names it: a `def` keeps the name the program gave it (`too many arguments for measure`), and a lambda is
`<lambda>` — never the compiler's generated `lambda_0`, which is a name no reader wrote. The pin fails if
either spelling leaks.

## Alternatives rejected

* **Count inside `callClosure`.** Rejected: it fixes the one road measured today and leaves `callFunc` —
  the shared sink — still able to invent a zero. The next caller gets the bug back.
* **Answer the missing argument with the zero value and say so** (a "lenient call" mode). Rejected
  outright: that is the current defect renamed. CPython raises, and a number at exit 0 is what an agent
  reads as success — ADR 0166's ordering puts this below even a wrong refusal.
* **Reuse the reference's exact sentence** (`<lambda>() takes 1 positional argument but 2 were given`).
  Rejected for now, and named: the two roads use two vocabularies today — the *checker* intercepts a
  literal call with `verify: function "f" expects 1 argument, got 0`, which no run-time string can shadow
  for the cases it sees. Making the checker quote `takes 1 positional argument but 2 were given` is an
  ADR 0215 table change across every call site's wording, and doing it inside this row would bury a
  wording decision in an arity fix. The interpreter's own trap reads
  `too many arguments for <lambda>: it accepts 1 argument, got 2` — the count, the callee and the class
  are all present and searchable.
* **Refuse at compile time for lambdas too.** Rejected: `g = lambda x: x*2` then `g(1, 2)` is a checker
  hole, not a codegen one, and a front-end refusal would escape `except TypeError:` (ADR 0211). The
  compiled leg's existing refusal is left alone — it is pre-existing behaviour on a row this ADR does not
  own, and the tests record it as a refusal rather than laundering it into an answer.

## Agentic rationale

Before, `--interp` was the *worse* engine for an agent here: it returned exit 0 and a plausible number for
a call the reference refuses, so the fast-feedback path was the one that lied. After, exit 0 means the same
thing on both engines. The message carries the three facts a repair needs — the callee (`<lambda>` or the
`def`'s name), the accepted count (`accepts 1 argument`), and the received count (`got 2`) — so a harness
can compute the fix rather than re-probe. The `--json` path is unchanged: same `"error"` object, same
string, so "not implemented" and "wrong" stay distinguishable without reading IR.

## Codegen / IR notes

No IR change: this is the interpreter's call road. The compiled module for these shapes already emitted
the checker's refusal, and the new tests assert it stays a refusal naming the arity question, so the
agreement cannot be bought later by quietly deleting the compiled check.

## Tests

* `pkg/lang/call_arity_test.go` — `TestACallableFromAVariableIsAskedItsArgumentCount` (9 rows: extras,
  nothing, a missing second parameter, four extras, two lambdas, a `def` through a name, an unknown
  keyword, a value given twice), `TestDefaultsAndKeywordsStillAnswer` (11 controls × CPython-pinned),
  `TestTheAritySentenceIsOneSentenceForBothRoads` (fails on `lambda_0`, `None`, `nil`, or on a `def`
  being called `<lambda>`), `TestTheCompiledRoadAlreadyAskedIt` (the agreement half).
* `integration/call_arity_test.go` — the CLI table: `--interp` may not exit 0 on a program the reference
  stops; `--aot` may not exit 0 with a wrong number and may not exit 2 at all; the controls run on both
  engines, with the two compiled refusals recorded as refusals rather than promoted to answers.
* `integration/programs/probe_asked_how_many_arguments.gy` — eleven lines, CPython-exact, **promoted** to
  `conformanceStandalone()` (the ledger test caught my first attempt to file a fully-matching program as
  a debt row: a probe that matches is a paid debt, and the artifact is where the truth lives).
  Matrix 152 → 153 rows, 115 → 116 parity, 100 → 101 oracle `match`, 0 fail, 0 drift.
* Sweep against the ADR 0283 binary (`/tmp/at10/pyre`, built from `b905ec3`): **161 files, no difference
  at all** except the pre-existing `int()`/`float()` panic row (`Gap R.131`) whose stack-trace addresses
  differ. No program's number or wording moved anywhere in the corpus.
