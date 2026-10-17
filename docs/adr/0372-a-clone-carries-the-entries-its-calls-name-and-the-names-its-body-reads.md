# 0372 — A clone carries the entries its calls name AND the names its body reads

## Context

ADR 0370 taught the defunctionalisation pass two things in one cycle: how to clone an applied `lambda` once per
**tuple of functions** its called parameters are asked to be, and how to give a `lambda` that reads a name its own
parameters do not bind one **capture parameter** per such name. It never taught the pass to do both at once. Five
sources, each answered by the reference, all refused by this backend, each one a body that calls a parameter *and*
reads the frame around its application:

| source | reference | this backend, before this ADR |
|---|---|---|
| `def f(x): return (lambda h: h(1) + x)(lambda q: q * 10)` / `print(f(2))` | `12` | exit 1 — `"h" is called but is not a function this backend can build a call to` |
| `def g(v): return (lambda f, k=3: f(k) + v)(lambda a: a * 2)` / `print(g(5))` | `11` | exit 1 — the same sentence |
| `def h(a, b): return (lambda f: f(a) + b)(lambda z: z * 2)` / `print(h(3, 4))` | `10` | exit 1 — the same sentence |
| `def k(): t = 7; return (lambda f: f(1) + t)(lambda z: z * 2)` / `print(k())` | `9` | exit 1 — the same sentence |
| `def m(v): return (lambda f, g: f(g(v)))(lambda a: a * 2, lambda b: b + 1)` / `print(m(3))` | `8` | exit 1 — the same sentence |
| `print([(lambda h: h(x) * 2)(lambda y: y + 1) for x in [1, 2, 3]])` | `[4, 6, 8]` | exit 1 — the same sentence |

And each **half** answered on the very same build, which is the localisation: `(lambda y: y * 2)(x)` inside `def f(x)`
is the reference's `8` and this backend's `8` (ADR 0370, Gap R.276), and `(lambda k: k(1))(lambda q: q * 10)` inside
`def f(x)` is the reference's `10` and this backend's `10` (Gap R.265). The row is roadmap Gap R.334.

The two lists were built by two functions that never looked at each other. `cloneCallable` minted the clone's
parameters from `calledPos` — the positions the body *calls* — and the capture list was consulted only when a `lambda`
was being **hoisted as a value**, never when one was being **cloned as an application**. So a body needing both was
declined by the entry audit before the capture road was reached, and the ordinary road answered with ADR 0283's
generic sentence. The missing thing was not a word; it was a union.

## Decision

**A clone's parameter list is the union the BODY asks for: the entries its calls name, in `calledPos` order, and one
capture parameter per name its body is free in. The application fills each capture from the CALLER's own reading of
that name.**

1. **The capture list is asked of the applied `lambda`, at the only moment it can be asked**
   (`callableLambdaCaptures(r.encs[rank], prog, lam)`). It asks the closure road's own question —
   `lambdaFrameCaptures`, the same reader `emitFunction` uses when it lowers a `lambda` to a `define` — so the two
   roads cannot disagree about which names a body is free in. Three exclusions keep it a capture list and not a
   broom: a name the **module** binds stays on the module-global road (a generated `define` reaches module storage
   like any other, so taking it as a parameter would change what a program that compiles today passes across the
   boundary — Gap R.35 owns module state), a **predeclared** name is a door and not a value, and a name the enclosing
   frame merely *reads* without binding is not that frame's to hand over. The list is sorted, because a map iteration
   would mint a different `define` per run (ADR 0273's determinism rule, which the pass's own hash test holds).
2. **A name the enclosing frame does not own is declined, not guessed at.** `def outer(a): def inner(b): return
   (lambda f: f(1) + a + b)(…)` reads `a` from two frames up, and the only machinery that reaches that far is ADR
   0245's nested-`def` lift, which AGENTS.md's L11.7 dependency says does not compose with this pass — a clone's call
   is a plain call and the lift's env chain is not in it. Declining leaves the program to the ordinary road's sentence;
   answering with a word that reads nothing would be ADR 0166's bug. That is Gap R.330's depth, named in the row.
3. **The list travels on the node the pass minted** (`FuncDef.Captures`), beside `SkipEmit` and `CloneOf`, and is
   `json:"-"`: `--emit-ast` and `gusty fmt` speak the SOURCE, so a rewrite must not mutate what the front end reports
   about the text (ADR 0320). `GenerateIRReport` prepopulates `g.lambdaCaptures` from those declarations **before** the
   first call is lowered — the same "asked of the program, not of the emission order" rule the pair arity and the
   container-answer scans read by — and the closure road fills the same table from the other end when it emits a
   `lambda`.
4. **A generated parameter is not a position an application may name** (`callableFn.srcParams`). The audits ADR 0371
   installed — `callableSiteOmission`, `callableSiteOperand`, `callableSiteFill` — read the parameter count the SOURCE
   wrote, not `len(def.Params)`. This is load-bearing, not tidiness: the first draft appended capture parameters to the
   enclosing `def`'s own list instead, and `def h(a, b): return (lambda f: f(a) + b)(…)` then died with
   `missing argument "b" for h` — the emitter checking a call against a prototype the human never wrote.
5. **The call fills a capture with the caller's own word.** For `def h(a, b)` the emitted call reads
   `call i32 @gy_h$lambda$…$callable$…(i32 0, i32 %p0, i32 %p1)`: the `ptrtoint`-form placeholder ADR 0371 installed
   where the clone names the entry by symbol, and the caller's own parameter words for the captures. The value is never
   copied into the clone; the clone reads what the caller reads, which is what keeps a captured local live across the
   call and what makes two applications of one body two clones rather than two constants.
6. **A capture whose kind one word cannot carry is refused before the body is emitted**, by the gate ADR 0370 built
   (`captureCarriesOneWord`), and the refusal now names the callable as the reader knows it — `<lambda>()` for a clone
   of an applied lambda, `twice()` for a clone of a `def` — instead of the symbol the pass minted. That is Gap R.325's
   rule (ADR 0345 §3) applied to a sentence it had not covered.

## Agentic rationale

`--callables` gained `capture_parameters`, and it is the field that makes this decidable from the outside: `0` for
`(lambda f: f(3))(lambda x: x * 2)` (every clone self-contained — ADR 0370's shape), `1` for Gap R.334's own program,
`2` for `def h(a, b): return (lambda f: f(a) + b)(lambda z: z * 2)`. Before it, an agent reading the account could not
tell a clone of Gap R.265's self-contained body from a clone of this one; both reported a clone, a rewrite and a hash.
The schema's `callablePassReport` describes the field, `docs/operations.md` and `docs/language.md` say which road
fills it, and the count is stable across runs because the capture list is sorted.

The pass's decline also got a sentence worth reading. `callableLambdaIsClosed` used to say `a lambda passed to a
parameter reads that parameter's function`, which described none of the programs above — the name belongs to the
caller, not the callee — and its replacement, `callableLambdaFrameRead`, names the exact word: `an entry lambda reads
"base" from the frame its application is written in, and an entry is a bare symbol: … (Gap R.336)`. The terminal still
prints the ordinary road's generic sentence for those programs, because `defunctionalizeCallables` swallows the decline
and hands the untouched program to the emitter; that loss is filed as **Gap R.337** rather than papered over, and this
ADR's tests pin both wordings so neither can quietly become the other.

## Codegen / IR implications

The module for `def f(x): return (lambda h: h(1) + x)(lambda q: q * 10)` is two `define`s and three `call`s:

```
define i32 @gy_f(i32 %p0) {
  %t1 = call i32 @gy_f$lambda$…$callable$…(i32 0, i32 %p0)
define i32 @gy_f$lambda$…$callable$…(i32 %p0, i32 %p1) {     ; one entry, one capture
  %t11 = call i32 @gy_f$lambda$…(i32 1)                      ; the entry, by symbol
```

No `fnptr` operand, no indirect `call`, no new instruction, no new runtime symbol, no ABI change: the union is a fact
about a parameter list, and the parameter list is the pass's to write. Every emitted module in the new tables is free
of `ptrtoint`/`inttoptr` and passes `llc`/`opt -passes=verify`, and compiling the same source twice yields the same
bytes — the property the capture list's sort order exists to protect. `integration/programs/
lambda_applied_capture_entry.gy` is the 211th matrix row (oracle `match`): entry+capture, two captures beside one
entry, a local rather than a parameter captured, an omitted default beside a capture, two entries, one body applied to
two tuples, a `for` body, and a comprehension — eight groups, each line for line CPython.

## Alternatives rejected

- **Append the capture parameters to the enclosing `def`'s parameter list.** Tried first, and it broke
  `def h(a, b)`: the arity check then read a prototype nobody wrote (`missing argument "b" for h`). A parameter list is
  the source's; only the generated declaration may grow.
- **Refuse the union and keep the two roads apart.** The refusal is what shipped before this ADR, and it was
  uninformative: the pass said nothing, the emitter guessed, and the reader was sent to L11.1 for a program with no
  value-word problem.
- **Carry the captured value instead of the caller's word** (a copy at the call site). It would print the same bytes for
  the programs in the table and be wrong for every program that rebinds the local after the application, and it would
  need a word per kind — a container copy is L11.1's unanswered question. Reading the caller's word is the same
  discipline ADR 0370's capture road already used.
- **Let the clone reach a name the frame's own frame owns.** The nested-`def` lift (ADR 0245) carries an env chain and
  a clone's call is a plain call; reaching past one frame would mean emitting a word that reads nothing and printing a
  number the program never bound — ADR 0166's bug, and Gap R.330's row.
- **Give the ENTRY (the `lambda` a higher-order `def` calls) the same treatment here.** Measured in the same sitting:
  `def use(seed): base = 1; return apply1(lambda x: x + base, seed)` is the reference's `4` and stays a refusal. An
  entry is a bare symbol; threading its captures means the callee's clone carries capture parameters too and the clone's
  identity becomes the pair (entry symbols, each entry's capture list) rather than the tuple ADR 0345 and ADR 0364 hash.
  That is a decision of its own, and it is filed as **Gap R.336**.
- **Emit an indirect `call` through a function pointer for a capturing body.** ADR 0345's first rejected alternative,
  rejected again here: the union is expressible as parameters and direct calls, which is the whole point of
  defunctionalising.

## Consequences

- The record leg covers ten sources first asked by this row, recorded from the reference by `tools/recmerge` and never
  from this backend (`pkg/lang/testdata/interpreter-golden.json`, 7833 → 7844). Five of them were already on the record;
  five are new.
- Five shapes moved from `integration/lambda_applied_test.go`'s refusal table to its answer table in this commit — the
  move the row's own plan predicted — and three new refusal rows name Gap R.336, with exit 2 forbidden on all of them
  and the reference's answer cross-checked for each.
- `capture_parameters` is in `--callables`, in the JSON schema, in `docs/operations.md` and in `docs/language.md`; the
  account test fails if the field stops separating the two clone families, so the witness cannot be deleted by editing
  a comment.
- Three things stay refused and are named shape by shape rather than by one sentence: a capture two frames up (Gap
  R.330 beside ADR 0245's lift), a capture whose value is not a number (Gap R.332 beside L11.1), and the same read in
  a METHOD frame (the road Gap R.150/R.151 owns). The ENTRY-side twin is Gap R.336 and the lossy diagnostic that sends
  a reader to the wrong ADR is Gap R.337.
