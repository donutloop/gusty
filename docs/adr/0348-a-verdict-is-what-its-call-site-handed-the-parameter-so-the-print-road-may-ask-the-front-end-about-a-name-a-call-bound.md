# ADR 0348 — A verdict is what its call site handed the parameter: the print road may ask the front end about a name a call bound

Status: accepted
Date: 2026-10-15
Decomposes: [ADR 0257](0257-a-verdict-is-printed-from-the-expression-that-made-it-not-from-the-slot-that-holds-it.md),
[ADR 0259](0259-a-container-slot-may-say-bool-the-element-tag-vocabulary-grows-a-bool.md),
[ADR 0261](0261-the-operator-that-chooses-an-operand-renders-what-the-chosen-candidate-is.md)
Roadmap row: Gap R.111 (L11.1's remaining bool item)
Follow-up: Gap R.270 (the shapes this reading declines — a contradicted position, a method name two classes share), owned by L11.1's tagged value word
Sibling: `strArgKinds` (Gap J.5), `stringFillingParams`

## The gap, as measured

A bool has always been a value to this language and never one to its value word: a verdict is
stored as the number the comparison produced, and what makes it print `True` is the front end's
answer to "is this expression a verdict?" — asked once, in `pkg/lang/boolvalue.go`, read by the
print, `str`, `repr`, f-string, container and `--json` roads (ADR 0257), by a container slot (ADR
0259), by the candidate `min`/`max` chose (ADR 0261).

The question is asked of an AST. It works wherever the expression is visible. It could not reach
one place:

```gusty
def show(f):
    print(f)      # the print road asks IsBoolExpr(Name{"f"})

show(True)        # gusty: 1     CPython: True
```

Inside `show`, `f` is a `*Name`, and `BoolEnv.Vars` records the names whose **assignment** produced
a verdict. A parameter is never assigned. So the answer was "not a verdict", the print road took
`printf("%d")`, and the program printed the number the caller's `True` was made from. Measured
surface, each row checked against `python3`:

| shape | before | CPython |
|---|---|---|
| `show(True)` | `1` | `True` |
| `b = True; show(b)` | `1` | `True` |
| `show(1 == 1)` | `1` | `True` |
| `def show(a, f): …; show(1, True)` | `1` for `f` | `True` |
| `def show(f=True): …; show()` | `1` | `True` |
| `show(f=True)` | `1` | `True` |
| `def m(self, f): …; z.m(True)` | `1` | `True` |
| `def outer(f): inner(f)` | `1` in `inner` | `True` |
| `def isit(): b = True; return b` → `print(isit())` | `1` | `True` |
| `def show(f): return [f, 1]` → `print(show(True))` | `[1, 1]` | `[True, 1]` |

Nine shapes, one cause. The information was not lost — the call site had read the verdict — it was
simply never carried to where the body asked the question.

## The decision

**A parameter's verdict-ness is a fact about the program's call sites, read by the front end.**

`pkg/lang/boolargs.go` walks the whole program once, before any IR exists, and answers two
questions from one walk:

- `boolArgKinds(prog)` — for each callable, which parameter positions **every** call site fills
  with a verdict;
- `boolLocalsOf(prog)` — for each callable, which names its **own body** binds to a verdict.

The first is registered on the compiled backend's per-body `boolVars` table when a body's
parameters are bound (`funcDef`, and the method path in `emitClassMethod`, which binds its own
parameters and so needed its own registration); the second is wired into `BoolEnv.CalleeVars`, so
when the print road asks whether a *call* is a verdict, the callee's body is asked with the
callee's names instead of the caller's. That second half is why `def isit(): b = True; return b`
works: `bodyReturnsOnlyBools` had been asking with the caller's table, where `b` had never existed.

Nothing about the value word changed. There is no boxed bool, no tag word beside an argument, no
hidden tag parameter on a `define`: the compiled answer still comes from the front end reading the
program, which is ADR 0172 (`None`), ADR 0224 (interned text) and ADR 0257 (verdicts) all saying
the same thing. `TestBoolThroughCallKeepsTheVerdictOutOfTheValueWord` fails if a runtime bool
representation appears.

### The three rules that make it safe

1. **A position is marked only when no call site contradicts it.** One `show(1)` anywhere unmarks
   it, and the body prints the number it always printed. An answer the compiler is unsure of
   degrades to yesterday's behaviour, never to a new wrong one. This is why
   `show(True); show(1)` still prints `1` for both calls: it is not correct for the first, and it
   is what the program printed before. Getting *that* right needs a tag that travels with the
   argument — L11.1's tagged value word, not a bigger inference.
2. **A variable forwards a verdict only while nothing has stored a non-verdict into it, per
   scope.** `def a(flag)…; def b(flag)…` with `a(True)` and `b(1)` marks `a`'s and not `b`'s.
   Scope-keyed tables are the whole reason the sibling string analysis survived; the failure mode
   it recorded was one function's `s` leaking onto another's.
3. **A name the analysis cannot judge is neither marked nor refused.** `definitelyNotABool` is a
   *closed list* of shapes — literals, containers, an index, arithmetic — not the negation of
   `IsBoolExpr`. A name it has not learned, a call it cannot resolve, an attribute it cannot see
   through: silence, which means the program keeps printing what it always printed. A test
   (`TestDefinitelyNotABoolIsClosed`) fails if an unresolved shape is ever *refused*, because a
   decline invented from ignorance is as much a bug as a guess.

Two details are worth recording because they are the difference between this working and silently
marking the wrong parameter:

- **A method's receiver is not an argument.** The parser puts `self` in the method's `Params`, and
  a call site does not pass it. Marking `z.m(True)` at position 0 would mark `self` and leave `f`
  a number, so `oneCall` offsets positional arguments by one for a call that arrived through an
  attribute. This needed a new hook on the shared AST walker (`heapASTWalker.attrCall`) because
  the walker only reported `f(args)` and an analysis cannot mark a call it is never told about.
- **A parameter's registration is authoritative, not additive.** Bodies are emitted sequentially
  against one `irGen`, and `enterBody` saves and restores `boolVars`; an unmarked parameter
  explicitly **deletes** its name rather than leaving whatever an enclosing body had recorded, so a
  nested definition cannot inherit a verdict from a differently-scoped name.

## Behaviour

Before: ten shapes printed the number; after: they print CPython's answer, verified row by row in
`integration/bool_call_boundary_test.go` against `python3` on the compiled leg.

```
$ gustyc --file programs/probe_bool_through_a_call.gy
True
True
```

and the conservative half stays conservative:

```
$ gustyc --eval 'def show(f):
    print(f)

show(True)
show(1)'
1
1
```

## Witness legs

The **reference leg** is the promise: 17 rows of `integration/bool_call_boundary_test.go` compile,
run and compare against `python3`, plus 4 rows pinned to the decline, plus an IR-shape row asserting
no runtime bool representation crept in.

The **record leg** diverges, and correctly so. The retired engine had the same bug, so
`testdata/interpreter-golden-drift.json` records `def show(f): print(f)` / `show(1 == 1)` /
`show(True)` as owed `1\n1\n`. The compiled answer is now `True\nTrue\n`, which is what CPython
prints. Per AGENTS.md the record is evidence about the language and CPython is its definition: the
record is the debt, the divergence is registered in the drift ledger, and the row that owns the
record's side is this one.

Four contract rows in the suite had been *pointing at* R.111 as their example of an open divergence
(`--oracle` must exit 6 on it). A contract row pointed at a paid-off debt asserts a contract nothing
violates, so all four moved — to `print_args`' interleaving (`Gap L.5`) — following the note each of
them already carried, which recorded the two previous migrations of the same rows (off
`print(True)` when ADR 0257 landed, off `print([True, 1])` when ADR 0259 landed).
`integration/bool_value_test.go`'s `TestOracleStillCallsTheBoolThroughACallADivergence` was inverted
into `TestOracleAgreesOnTheBoolThroughACall`: the same program, the opposite requirement, so a
future narrowing of the inference goes red here instead of in a user's program.

## IR impact

None. No new global, no new runtime function, no change to any `define`'s signature. The only
emitted difference is which print helper a call site takes: `rt_print_bool` where the front end can
say the name holds a verdict, `printf("%d")` where it cannot.

## Alternatives rejected

- **A tag word beside every object argument** (a hidden `i32 %tagN` per parameter, the shape
  ADR 0273's pair road uses for a fold's winner). Correct in the limit — it is the only way to get
  `show(True); show(1)` right — and it is L11.1's destination, not this rung. It changes every
  `define`, every call road (there are several: keyed, closure, method, generator), the pair-spec
  arity question and the verifier contract, to fix a print. The tagged value word is the row that
  should pay for it, once, with a tag that means something everywhere rather than a tag that means
  something to `print`.
- **Boxing bools at the call boundary** (pass a heap object for a verdict). Costs a slot per verdict
  argument — and slots are what Gap Q.2 just learned to run out of — for an answer the compiler
  already knows. It also breaks the rule that `True + 1` is `2`: a boxed verdict now needs
  unwrapping at every numeric door, which is where ADR 0259's whole difficulty came from.
- **Marking a parameter when *most* call sites pass a verdict.** A majority is a guess about which
  path ran. The rule is all-or-nothing, and the `mixed call sites` row is its witness.
- **Asking the print road at run time** (a runtime "is this word 0 or 1?" test). `1` is a legal
  integer; this is exactly the confusion ADR 0257 exists to end, arriving from the other side.
- **Special-casing `print`.** The decline table is not about print: `str(f)`, an f-string,
  `[f, 1]` and `--json`'s type field must all follow the one predicate, which is why the fix is a
  fact about the parameter and not a branch in the printer.

## Residuals this cycle does not pay

- A **contradicted position** (`show(True)` and `show(1)`) prints the number for both calls. Needs
  the travelling tag; owned by L11.1.
- **There is no `type(x).__name__` to read a verdict's name with.** `type` is a statement keyword in
  this grammar (it introduces a type alias, `parser.go`'s `type` arm), so `type(1).__name__` — and even
  `t = type` — is a parse error at exit 1, exactly as `docs/language.md` § Booleans and `None`
  documents ("`type` is a reserved word here (annotations), so `type(x)` is not a call in this
  grammar"). Recorded here because a runtime answer to "what kind is this?" is what a print-side rule
  eventually has to give way to, and the row that opens that door (L11.1's tagged value word, and
  Gap R.270 above) owes that question an answer too — not because today's parse error is undocumented.
  Measured while looking for a witness for this row's shapes.
- **`f"{f}"` on a parameter** refuses with "f-string requires a constant expression" — the f-string
  row's, not this one.

## Tests

`pkg/lang/boolargs_test.go` — 16 positive rows (literal, comparison, `not`, membership, annotation,
default, keyword, second position, forwarding one and two deep, method, builtin verdict, `and`,
ternary, module-bound name), 15 decline rows (contradiction, every literal kind, an unknown name,
arithmetic, a body that rebinds, two classes sharing a method name, two functions sharing a name,
an index, an unresolved call), the scope-leak row, the callee-locals row, and the closed-negation
row.

`integration/bool_call_boundary_test.go` — the reference leg (every row compared with `python3`),
the decline rows, and the no-new-representation row. Plus the migrated contract rows in
`integration/oracle_test.go`, `integration/render_pair_test.go`, `integration/build_test.go`,
`cmd/gustyc/oracle_test.go`, and the inverted `integration/bool_value_test.go`.
