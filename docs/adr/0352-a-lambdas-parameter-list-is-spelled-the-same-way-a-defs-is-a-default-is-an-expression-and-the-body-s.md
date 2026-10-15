# ADR 0352 — A `lambda`'s parameter list is spelled the same way a `def`'s is: a default is an expression, and the body separator is the only ambiguity

Status: accepted
Date: 2026-10-15
Amends: [ADR 0322](0322-a-callable-takes-its-options-from-inside-itself-the-default-lives-in-the-prototype-the-keyword-matches-by-name.md), [ADR 0345](0345-a-function-used-as-a-value-is-a-name-this-pass-mints-before-the-doors-that-decide-a-call-are-asked.md)
Depends on: [ADR 0249](0249-a-refusal-naming-the-missing-half-is-the-answer-an-agent-can-branch-on.md), [ADR 0166](0166-exit-2-is-a-compiler-bug-and-not-an-answer-so-a-program-the-reference-runs-may-not-leave-the-toolchain-with-it.md)
Roadmap row: Gap R.263 (L11.7's next-action list, first item)

## The gap, as measured

`lambda` had its own hand-written parameter loop, and it read names only:

```gusty
f = lambda x, y=1: x + y
print(f(1))          # parse error at 1:16: expected ":"    ← this toolchain
                     # 2                                      ← the reference
```

Exit 1, with a parse error — the right exit code for a source the language genuinely cannot express, which is
exactly why the row had sat unnoticed: nothing looked broken, the program simply did not exist. `def` got its
defaults from `parseParam()`; `lambda` did not, and the code said why in a comment — `parseParam()` would
greedily treat the `:` body separator as a type annotation.

That comment is correct, and it is also the reason the fix is not "call `parseParam()`". A lambda's body
separator and an annotation's introducer are the same token, so the annotation arm needs the
"is the token after `:` a type keyword" test that only the lambda's context can supply. What the comment
proved was that the loop had to stay; nothing in it said the loop had to stop at `=`.

## The decision

**A default is an expression, and `=` is not ambiguous.** The lambda loop keeps its own annotation
disambiguation and grows one arm: on `=`, consume it and `parseExpr()` a default into the same
`Param.Default` a `def` fills. Nothing else was needed, because:

- `:` is not an operator, so a default's expression stops at the body separator by itself — including a
  default that *contains* one (`{"a": 1}`, `"a:b"`), which consumes the colon inside its own brackets;
- `Param` already carries `Default`, and the front end's arity and binding rules read it for `def`, so a
  lambda's default arrives in a shape every downstream door already understands;
- ADR 0322 put a callable's options *inside the callable* — the default is evaluated against the definition's
  scope and read at the call — so the call site needed no change, and the `sorted(key=…)` hook specialisation
  and the arity check (already `to N`) worked the moment the AST was honest.

That is the whole feature: one parser block, no codegen change. The temptation on a row like this is to
"also" touch the pass or the arity check; both were already correct, and touching them would have made the
witnesses certify work nobody did.

## What was rejected

- **Sharing `parseParam()` with `def`.** It re-imports the annotation greediness the original comment was
  written to avoid: `lambda x: x + 1` becomes a parse of `x` annotated with the type `x`. The two lists are
  spelled the same but disambiguate differently, and the parser should keep that difference visible rather
  than paper over it with a flag.
- **Refusing `lambda` defaults with a stable code instead** (ADR 0249's honest refusal). Rejected because the
  refusal was not honest: the backend already *answers* every consequence of a defaulted lambda — ADR 0322's
  call-site rule, the hook specialisation and the arity check were built for `def` and work unchanged. A
  refusal that hides an implemented feature is a bug with better manners, and `--lang`/`--capabilities` would
  have had to document a shape the machine already handles.
- **Desugaring `lambda` to a hoisted `def` at parse time.** It answers the row and deletes the construct, and
  with it the `--callables` account's `hoisted_lambdas`, the minted `f$lambda$…` symbols and every diagnostic
  that names `<lambda>`. It also re-opens the question of what a lambda *is* — L11.1's question, which this
  row does not own.
- **Widening the loop to `*args`/`**kwargs` and a trailing comma while it was open.** Rejected, and guarded:
  both stay parse errors (roadmap Gap R.278, Gap R.279) and
  `TestTheLambdaGrammarStillDeclinesWhatNoRowHasPaid` fails if the grammar accepts a spelling that has no
  answer behind it. A grammar change and the semantics it presumes must not be split apart by accident, which
  is what a guard on the *refusal* is for.

## What this row measured and filed instead of fixing

The probe session that built the tables found six shapes the reference answers and this build does not. Each
is a different road's business, and each is pinned with today's answer beside the reference's in
`TestWhatTheLambdaDefaultRowLeavesStandingIsFiledNotFixed`:

| shape | reference | today | row |
|---|---|---|---|
| `def outer(): b = 42; f = lambda: b` | `42` | refuses `undefined name "b"` | **Gap R.276** |
| the same shape with a nested `def` | `42` | `42` (the control) | — |
| `def d(y=None): return y` | `None` | **`0` at exit 0** | **Gap R.277** |
| `def d(y=None): return y is None` | `True` | **`False` at exit 0** | Gap R.277 |
| `def d(xs=[]): return xs` / `w={}` | `[]` / `{}` | **`0` / `0`** | Gap R.277 |
| `def d(xs=[]): return len(xs)`, called with `[1,2,3]` | `3` | `3` (the control) | — |
| `neg = lambda v: 0 - v` used as `sorted(key=neg)` | `[3, 2, 1]` | raises `'int' object is not callable` | **Gap R.281** |
| the same hook written inline, or with a `def` | `[3, 2, 1]` | `[3, 2, 1]` (control) | — |
| `def d(*a)` / `lambda *a`, `**kw` | answers | parse error | **Gap R.278** |
| `def d(x, y=1,)` / `lambda x, y=1, :` | answers | parse error | **Gap R.279** |
| arity of a defaulted `lambda` at the CLI | `TypeError` | codegen's `missing argument "x"`, where `def` reaches the front end's `expects 2 arguments` | **Gap R.280** |
| a container default in a program with a higher-order call | answers | refuses `"fn" is called but is not a function` | **Gap R.282** |

**Gap R.277 is the one to read twice.** It is the priority-1 class — a wrong number on stdout at the exit code
that means the program ran — on the most ordinary Python surface there is, and it is not a lambda's problem:
the `def` road does it too, which is why this row's Definition of done is deliberately `def`-parity for the
grammar and *not* CPython-parity for those defaults. Every scalar default answers; every non-scalar default
reads the payload word `0`; a container default *supplied by the caller* answers. The road that binds an
omitted argument is the missing one, and it is its own row, not this one smuggled in under "defaults".

Gap R.282 got the same treatment from the other side: my first draft of the corpus program died with the
`"fn" is called…` refusal when the container-default lambda was in it. The naive shrink "found" a 6-line
repro that had no call in it at all — it was failing for a different reason, and a shrink that does not pin
the message is a shrink that files fiction. Re-run with the refusal's text as the invariant, the repro is
five statements (in the filed table), and the corpus row was written around the shape with the reason in its
header rather than the shape quietly deleted.

## Codegen / IR implications

- None for this row, which is the point worth recording: the IR for `f = lambda x, y=1: x + y` is the IR for
  the `def` spelling, minted `f$lambda$<hash>_N` by ADR 0345's pass, and the default is materialised at the
  call site by ADR 0322's rule. `TestTheCallablesAccountReportsTheDefaultedLambda` asserts that from the
  machine document (`rewritten`, `hoisted_lambdas`, the minted symbol, and `refused: []`) so "the grammar took
  it *and* the pass took it" is one read, not two.
- Exit 2 is forbidden on every answered row (`TestNoLambdaDefaultProgramExitsTwo`). Before the row, exit 1 was
  the honest code; after it, anything short of an answer at exit 0 — and anything worse than a diagnostic — is
  a compiler bug.

## Verification

- Unit (`pkg/lang/lambda_default_param_test.go`): 21 answered shapes on the compiled leg and the JIT against
  CPython's answer, the same 21 asserted **on the record** (recorded from the reference by `tools/recmerge`;
  the compiled leg's agreement is checked by the conformance matrix, and `recmerge` refuses an entry whose
  legs disagree); 10 AST cases asking the grammar directly — how many parameters, how many carry a default —
  through the same JSON document `--emit-ast` prints, so the claim is about what an agent reads; 3 refusals
  that must stay refusals; exit 2 forbidden; and the 18-row filed table above.
- Integration (`integration/lambda_default_param_test.go`): the same sources through the CLI against
  `python3`; the `--callables` account; the door contrast between a `def`'s arity and a `lambda`'s at the CLI
  (where a caller actually sees it); and `integration/programs/lambda_default_param.gy` line-for-line against
  `testdata/lambda_default_param.expected`, which CPython wrote.
- Corpus: `lambda_default_param.gy` joins the conformance matrix as a `match` row — 15 lines, written to keep
  the container default and the name-bound hook lambda out, with both absences named in the header and each
  owned by a row.
