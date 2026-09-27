# Session Learnings
## Round 19 — Walrus operator (`:=`)

**Feature (Phase 5, L5.4)**: assignment expressions `name := expr` — assigns `name` in the enclosing function/module scope and yields `expr`'s value. Implemented across lexer, parser, AST, semantic analyzer, interpreter, and LLVM codegen, with parity integration tests.

- **Lexer**: added `:=` to the ops list (longest-match, before `:`).
- **AST**: new `AssignExpr{Name, Value}` node.
- **Parser**: `precWalrus` precedence (lowest, below ternary); `:=` handled as a TokOp binary in `binaryOp`'s op-switch (NOT the keyword switch — `:=` is a TokOp); the Pratt loop special-cases `:=` into `AssignExpr` and rejects a non-Name LHS.
- **Semantic**: `inferExpr`/`inferExprTy` cases compute the value type and `scope.define` the walrus name (enclosing scope, so it works after the expression).
- **Interpreter**: `eval` case evaluates `n.Value`, stores `e.Vars[name] = v`, returns `v`.
- **Codegen**: `value` case evaluates the RHS, allocates `%_name` (tracked via `g.allocd`) if needed, stores i32/double, loads back, returns the loaded register.
- **Gotchas**:
  - `binaryOp` dispatches on `t.Kind`: keywords vs TokOp are separate `switch t.Text` blocks — the walrus case must go in the TokOp block or `:=` is never matched (parse error "expected )").
  - Go precedence const blocks use `iota`; a new lowest const needs its own `= iota + 1` and the following const becomes implicit.
  - Codegen format strings: `%s` for the temp register (a `%t%d` string), `%%_%s` for the name.
- **Tests**: `TestParityWalrus` (if-condition walrus, value reused after), `TestParityWalrusFnScope` (walrus in a function returning the value). Both pass interpreter + AOT.


## Round 12 — Runtime dispatch: `%obj`-tagged value representation

- **Next planned item selected**: Phase 2 "Runtime dispatch — `%obj`-tagged value representation so AOT and interpreter agree on the dynamic type model".
- **Canonical dynamic type model**: added `pkg/lang/value.go` — a single Go source of truth (`ValueTag` constants, `objKindTag`, `kindForTag`) that the AOT runtime IR (emitting tag constants into LLVM) and the interpreter heap (`obj.tag()`, `tagOfVal`) both read, so AOT and interpreter agree by construction.
- **AOT runtime `%obj`**: `%obj = type {i32, i32}` plus helpers `rt_mkobj`, `rt_obj_tag`, `rt_obj_payload`, `rt_obj_is` emitted into the prelude.
- **Dispatch wiring**: dynamic method dispatch now wraps the receiver as `{tag=instance, payload=handle}`, tag-checks it (`rt_obj_is`), and extracts the payload (`rt_obj_payload`) before the class-id switch. Caveat: `%obj` in `fmt.Sprintf` format literals must be escaped as `%%obj`.
- **Interpreter mirror**: `obj.tag()` maps `obj.kind` strings to the canonical tags; `tagOfVal` reports heap reference tags and TagInt for plain values (plain int/bool/None are raw i64s today).
- **Tests**: `value_test.go` (canonical tag table, obj.tag mirror, tagOfVal), ircheck `TestObjTaggedDispatch` (llc-valid IR contains `%obj` helpers), parity `TestParityObjTaggedDispatch` (polymorphic dispatch identical stdout on both backends). Note: attributes/`__init__`/string-returns are not yet AOT-supported, so the parity program uses int returns.
- **Docs**: extended `docs/agentic/ast-ir-schema.md` with the `%obj` model + tag table; ADR 0142; roadmap Phase 2 marked DONE.
- **ADR**: `docs/adr/0142-obj-tagged-value-representation.md`.


## Goal
Get the `gusty` compiler building and passing tests against LLVM 20, and fix the
printf codegen semantics. Continue the greater vision (Python-like language) — but this
session focused on the build/correctness fix, not new language features.

## Key facts discovered

### go-llvm dependency & the local reference dir (since removed)
> Note: the `go-llvm` binding dependency described here has since been **removed** from the
> project. Codegen is now a textual LLVM IR emitter verified by external `llc`/`cc`; go.mod
> no longer requires any LLVM Go bindings. The notes below are retained as history.

- The local `go-llvm/` directory was a **full separate module** (own `go.mod`, `go 1.14`)
  with the *free-function* API (`llvm.NewModule`, `llvm.Int32Type`, `llvm.VoidType`,
  `llvm.NewBuilder`).
- The module-cache version of `go-llvm` had the **Context-method** API (`func (c Context)
  NewModule`, `Int32Type`, `VoidType`, `NewBuilder`) and free `FunctionType`,
  `PointerType`, `ConstInt`, `InitializeAll*`. It did NOT have the free
  `NewModule`/`Int32Type`/`VoidType`/`NewBuilder` functions.
- User preference then: **update the module dependency**, do NOT vendor/commit the local
  `go-llvm/` (it stays gitignored reference-only). A `replace => ./go-llvm` directive would
  break builds for other clones because go-llvm was not committed.

### The two codegen bugs (both were silently masked before)
1. **printf signature**: first param was `PointerType(ctx.Int32Type(), 0)` (ptr-to-i32),
   but the format-string global is `[3 x i8]` and calls pass `ptr @format_string`.
   LLVM's verifier rejected it. Fix: `PointerType(formatString.Type(), 0)` — pointer to the
   format string's array type. This is why calls must pass `ptr @format_string`.
2. **printf printed addresses, not values**: `printf(donut)` generated
   `load ptr, ptr %donut` and passed the variable's *address* to printf. It now loads the
   i32 **value** (`load i32, ptr %donut`) and passes `i32 %donutValue`.

## Approach that worked
- To test WITHOUT the replace directive safely: copy `go.mod` to `/tmp`, strip the
  `replace` line via regex, `go build`/`go test`, then restore. This confirmed the module
  version alone builds and passes.
- Removing the replace left a trailing blank line in go.mod; `s.rstrip()+'\n'` fixed it so
  `git diff go.mod` was clean vs HEAD.
- `integration` package has no non-test Go files, so `go build ./integration/` fails with
  "no non-test Go files" — that's expected; build `./pkg/lang` and run tests instead.
- Integration tests only `bytes.Compare` generated IR text vs `expected/*.ll`; they do NOT
  execute the code. So expected files must exactly match the generated IR.

## Commands
- `go test -tags=llvm20 ./pkg/... ./integration/ -count=1` — the full check (exit 0).
- `go env GOMODCACHE` + grep the cached `go-llvm@v0.0.0-...` module to check API.
- Regenerate expected files by temporarily adding `os.WriteFile(...)` to the assert in the
  test, running it, then reverting (kept the diff minimal — reverted import-block cosmetic
  change too).

## Final state
- go.mod: require `go-llvm v0.0.0-...`, NO replace (since removed).
- go.sum updated; .gitignore reduced to just `go-llvm` (reference-only).
- `pkg/lang/IR.go`: Context-method port (`ctx := llvm.NewContext()`, `ctx.NewModule`,
  `ctx.NewBuilder`, `ctx.Int32Type`, `ctx.VoidType`) threaded through generateCaller/
  generateLet/generateAdd/generateFor; printf type + value-load fixes.
- `integration/expected/*.ll` regenerated for corrected IR.
- Committed `eaaa624`. Working tree clean; `go test -tags=llvm20 ./pkg/... ./integration/`
  passes.

## Next milestone (the loop)
Language surface is still the C-like curly-brace subset (`function`, `let`, `for`, `while`,
`i32`, `printf`). The Python-like vision remains: indentation-based INDENT/DEDENT lexer,
parser/AST, gradual typing, comprehensions, generators, classes/inheritance, decorators,
exceptions, pattern matching, modules, stdlib.

---

## Round 20 — Union-type syntax `int | str` (L5.5)

- **types.go**: added `KindUnion`, `Members []*Type` field, `TUnion(...)` constructor, `unionName()` rendering (`int | str`), and order-independent `Same()` for unions.
- **parser.go**: `parseTypeAnnot` now parses `T | U | ...` as a union of member types (each member via a new `parseTypeTerm`); unions nest inside generics (`list[int | str]`).
- **semantic.go**: `assignable` handles unions — `got` is assignable to a union iff assignable to any member, and a union-typed `got` is assignable iff every member is.
- **schema.go**: documented union rendering in annotation text (`"int | str"`).
- Verified: `x: int | str = 3` and `= "hi"` accepted; `= True` reports `expected int | str, got bool`; `list[int | str] = [1]` accepted; union `Same()` is order-independent.
- Integration tests use only union cases that hit working codegen paths (string-through-function and mixed lists are pre-existing codegen limits, unrelated to unions).


# Session learnings: tiny Go (the gusty interpreter)

This session I implemented `raise`/`try`/`except`/`finally`, generators (`yield`),
list literals, `for`-over-lists, and `len(list)`. Here is what I learned about
this small Go compiler's interpreter (`pkg/lang/jit.go`) and analyzer
(`pkg/lang/semantic.go`).

## Architecture (it's genuinely tiny)
- The "compiler" is mostly an AST interpreter: `pkg/lang/parser.go` builds a
  `Program` of `Stmt`/`Expr` AST nodes; `pkg/lang/semantic.go` is a type
  checker; `pkg/lang/jit.go` is a tree-walking evaluator; `pkg/lang/codegen.go`
  is the LLVM IR emitter.
- There is no IR/bytecode in between — the interpreter evaluates ASTs directly.

## The value model is unusual
- The evaluator represents values as `int64` "handles" into a `heap map[int64]*obj`.
  `obj` has `kind` ("class"/"instance"/"method") plus `attrs map[string]int64`.
- Functions live in `e.funcs map[string]*FuncDef` — they are NOT first-class
  values. This is the single biggest constraint: decorators and closures are hard
  because there's no way to pass a `FuncDef` as a handle.
- I added `kind:"list"` with `elems []int64` for the list/generator feature.

## Generator implementation (eager, not lazy)
- I implemented generators by detecting `containsYield(fd.Body)` at call time,
  setting a `yieldList` accumulator handle on the Evaluator, and letting each
  `YieldStmt` append its value. Calling the function returns the list handle.
- Key bug: my first `case *YieldStmt` did `return v, nil` — but the dispatcher
  loop treats a `return` as stopping the whole body, so only the FIRST yield was
  collected. Fix: `last = v; continue` instead of returning.
- This is eager (collect all yields), a faithful subset for pure generators.

## The semantic analyzer's scope chain
- `Scope.lookup` chains to parents; `an.scope.define(name, type)` always adds.
- The `for` loop analyzer binds the loop variable into a NEW child scope, then
  analyzes the body per-statement.
- Critical gotcha: if `inferExpr(iter)` returns `nil` (unknown return type,
  e.g. a generator call), `define(x, nil)` adds x with a nil type — and the
  Name resolver treats "found but nil type" as **undefined name x**. I fixed it
  by falling back to `TDyn()` when the iterable element type is nil.

## Builtin dispatch duplication (a real smell)
- Builtins like `range`/`print`/`len` are handled in TWO places: the evaluator's
  `evalCall` switch (`case "print"`, `case "len"`) AND the semantic analyzer's
  `inferCall` switch (`"print" -> TVoid()`, `"len" -> TInt()`). Adding a builtin
  means touching both, and they can drift out of sync.
- `print` returns `0, nil`; `range` is special-cased in `rangeBounds`. My `len`
  returns `int64(len(o.elems))`.

## `for` loop dispatch gotcha
- The original `ForStmt` called `e.rangeBounds(s.Iter)` which internally evaluates
  the iterable as a range call. When I rewrote it to `e.eval(s.Iter)` first to
  detect lists, `range(a, b)` broke — the generic `eval` hits the builtin
  `range` handler which errors on 2 args. Fix: special-case `range` calls before
  generic evaluation.

## Test hygiene
- The package has no exported helpers (e.g. `heapOf`), so generator tests had to
  go through `EvalExpr`/`parseProgram` + `NewEvaluator().EvalProgram` directly.
- I wrote several throwaway `dbg*_test.go` files to trace bugs, then deleted them
  before committing — the final feature tests live in `pkg/lang/gen_test.go`.

## Process lessons
- Each feature = parser/semantic/evaluator edit + docs/language.md + an ADR in
  `docs/adr/` + tests, committed and pushed per feature.
- The repo convention is one commit per feature with a `feat(lang):` message,
  and an ADR explaining the decision, rationale, and rejected alternatives.
- Always `go test ./pkg/...` before committing; verify emitted IR with the
  external `llc`/`llvm-as` tools.

## Round 2 — Generators (yield + generator expressions) in AOT codegen
- Landed generator functions and generator expressions in the LLVM AOT path:
  `funcDef` detects generators via `containsYield` (recursive body scan),
  `rt_alloc(1)` a heap-list handle, each `yield` lowers to `rt_append`; the
  function rets the handle on normal and raise-exit paths.
- Call sites track generator funcs in `genFuncs`; result operands register in
  `listOperands` so `print`/indexing treat them as lists (`rt_print_list`).
- `genExpr` unrolls constant `range(...)`/`ListLit` iterables at codegen time
  (mirroring `comp()` for comprehensions), folds `Cond`, appends to `%gxN`.
- Parity rule: keep AOT lowering identical to interpreter eager-list semantics.
- Lesson: recent AOT features get an ADR (e.g. 0129-classes-aot,
  0130-generators-aot).
- Lesson: unit tests for AOT landings live in `pkg/lang/ircheck_test.go` via
  `llcCompiles` (IR validity), plus a whole-program `integration/` test that
  actually runs. Don't cite ADR numbers you haven't verified (ADR 0088 is
  opt-passes, not runtime dispatch).

## Round 3 — Arbitrary decorators in AOT codegen (identity + clear rejection)
- Landed decorator application machinery in emitDecoratedFunc: resolveDecorators
  walks decorators in source order (matching interpreter: @dec1 @dec2 def f ==
  f = dec2(dec1(f))).
- Identity decorators (`def dec(g): return g`) resolve to @f_impl and keep the
  existing direct-call path; multiple identity decorators work.
- Wrapping/closure/transform decorators are rejected with a clear codegen error
  ("not an identity decorator") instead of being silently ignored (the previous
  behavior was a correctness bug).
- Lesson: the codegen function model returns i32 and has no fnptr operands /
  indirect calls; full arbitrary decorators (closures calling g()) are a
  follow-on. ADR 0131 documents the phased landing.
- Lesson: LLVM IR edits via line-index Python surgery are fragile — use
  str.replace on exact extracted substrings (starting at the full line, not
  mid-line). Always restore from HEAD before re-applying.

## Round 4 — Generational tracing GC in the interpreter heap
- Made Collect() two-generation: nursery = ids >= nurseryBase; young GC sweeps
  unreachable nursery objects and promotes survivors (advance nurseryBase to
  maxHeapID); full GC sweeps whole heap when old-gen count > 512 or nurseryBase==0.
- allocObj increments allocCount (unused trigger for now — GC stays explicit).
- Lesson: the interpreter has no stack, so auto-GC mid-execution is unsafe
  (Vars-only roots); keep Collect explicit. ADR 0132 documents the phased landing.
- Lesson: LLVM/heap edits via line-index Python surgery are fragile; use exact
  substring replaces and restore from HEAD before re-applying.

## Round 5 — GC stress/leak tests
- Added TestGCStressBoundedHeap (2000 short-lived nursery allocs with young GC
  after each; live global survives; promoted survivors survive stress).
- Added TestGCFullGCBoundsOldGen (promote two globals, drop one, push old-gen
  past full-GC threshold 512; full GC reclaims unreachable old, keeps live old).
- Lesson: roadmap item name is "GC stress / leak harness" (not "GC stress/leak
  tests") — always grep the exact row text before replacing.
- ADR 0133 documents the stress/leak contract; roadmap marks the item LANDED.

## Round 3 — escape-analysis heap elision (ADR 0134)

- AOT codegen emitted an `rt_alloc` heap allocation for every top-level list
  literal, even for variables never read afterwards — a provably dead object.
- Delivered an always-on source-level escape analysis (`pkg/lang/escape.go`,
  `deadListAssignments`): a top-level variable is a dead-list candidate iff
  every top-level assignment to it is a list literal AND it is never read at
  top level. Function bodies are NOT descended into — verified empirically that
  this codegen cannot read top-level globals from a function (`index of a
  non-literal variable`), so top-level deadness is decided purely from top-level
  reads, and the `inFunc` guard double-protects against firing inside a body.
- Key soundness checks via `-emit-llvm`: a dead list emits 0 `rt_alloc`; a read
  list still allocates; a function-local list with the same name as a dead
  global still allocates (distinct `%_g` local vs `@_g` global). Conservative:
  any non-list assignment or any top-level read keeps the allocation, so no path
  can observe a freed slot.
- Coverage: `TestAOTEscapeDeadList` checks the emitted IR; `TestEscapeDeadListRun`
  (integration) guards whole-program output parity.

## Round 8 — dynamic method dispatch (AOT) + interpreter return-in-block fix

- The previous round left uncommitted AOT dynamic-dispatch work (codegen.go
  dispatch in `call` for statement-level method calls on runtime receivers,
  ircheck test TestAOTDynamicDispatch, ADR 0136). This round completed and
  landed it.
- Interpreter bug found while verifying dispatch: `return` inside a nested
  block (if/while/for) was NOT propagated out of a function body — the
  block handler treated evalBody's returned value as just "last expression"
  and continued the outer loop, so `def make(k): if k==1: return A; return B`
  always returned B. Fixed by introducing a `returnSignal{val}` error that
  flows through the block handlers (which already propagate errors via
  `return 0, err`) and is unwrapped at callFunc, callClosure, callMethod, the
  function-call site, decorators, and generators. Lambdas and decorators
  needed the unwrap at callFunc's regular path too.
- Added interpreter test TestEvalDynamicDispatch (make returns Animal/Dog by
  kind; a.speak()+b.speak()=43), AOT ircheck test TestAOTDynamicDispatch, and
  native integration test TestExecDynamicDispatch (statement-level dispatch).
- AOT limitation documented: expression-level dispatch on function parameters
  (e.g. `v.speak()` inside a function) still resolves statically; field reads
  like `a.x` on runtime-unknown receivers are unsupported (error
  "unsupported attr expression"). Only statement-level dispatch on instances
  is correct end-to-end.

## Round: Membership + identity ops (`in`/`not in`, `is`/`is not`)

Landed Phase 6 language-surface feature. Adds:
- Parser: `in`, `not in` (two-token), `is`, `is not` (two-token) as comparison-level operators in `parseComparison`, with a `peekNext` helper for two-token ops; added `is` to keywords.
- Semantic: these ops infer `TBool`.
- Interpreter (jit.go): `in`/`not in` via `contains` (list/set element equality, dict key equality, str substring); `is`/`is not` via handle identity; extracted shared equality into `eqVal` used by `==` and membership.
- Codegen (codegen.go): `is`/`is not` lowered to `icmp eq/ne` (i1); `in`/`not in` lowered to a runtime `@rt_contains` call (reads container `kind`/`len`/`data`, dict keys at i*2) + `icmp ne` to i1 (and `xor` for `not in`), so results are usable in `if` conditions like other comparisons.
- Tests: `TestEvalMembership` (interpreter, incl. literals/strings/dicts/sets) and `TestIRMembership` (llc-valid IR for variable containers).
- Known codegen parity gap (consistent with i32-only codegen): `in` on an inline literal container is not lowered (only runtime container variables); the interpreter supports literals.

Key gotchas: `g.write` doesn't exist — emission uses `b.WriteString(fmt.Sprintf(...))`; comparison results are i1 in codegen (usable in `br i1`), not i32, so `in` must return i1; literal containers are globals (`@.lstN`), not `@heap` indices, so `rt_contains` works only for runtime heap handles.

---

## Round 8 — Formatter (`gusty fmt`) [Phase 8]
- Added a full canonical pretty-printer in `pkg/lang/fmt.go`: `Format`, `FormatSrc`, `IsFormatted`.
- Deterministic 2-space-indent canonical style; precedence-based parenthesization for `BinOp`; canonical literal rendering
  (int via `FormatInt`, float via `FormatFloat` with `.0` suffix so it stays a float, double-quoted strings with escaping).
- Covers every statement/expr node: if/elif/else, while/for/else, def (params/annot/return-anno/decorators),
  class (bases), match/case/guard, try/except/finally, with/as, yield/yield-from, import, lambda,
  tuple/list/set/dict, call/index/attr/slice, cond-expr, comprehensions, f-strings.
- CLI: `--fmt <src>` prints canonical source (machine: deterministic stdout); `--fmt-check <src>` verifies
  canonical and returns exit 0 if canonical / 1 if not (trailing-newline tolerant); `--fmt-file <path>`
  reads a file; `--json` emits a machine report. Added `exitNotCanonical = 1`.
- Tests: `pkg/lang/fmt_test.go` — round-trip (re-format stable, re-parse succeeds) + canonical examples.
- Idempotence: `Format(Parse(Format(src))) == Format(src)`; canonical output re-parses successfully.

## Round 9 — Docstrings / `__doc__` (ADR 0141)

- Parser: `parseFuncDef`/`parseClassDef` pull a leading bare `StrLit` statement
  out of the body into `FuncDef.Doc`/`ClassDef.Doc` via `extractDoc`; non-first
  string literals stay expressions.
- AST: `Doc string` fields on `FuncDef` and `ClassDef`.
- Interpreter: `obj.doc` on closures (set in `allocClosure` from `fn.Doc`) and
  classes (set at `ClassDef` eval); top-level `f.__doc__` resolves straight to
  `e.funcs[name].Doc` (top-level funcs are AST nodes, no runtime object); the
  `case *Attr` handler checks `__doc__` before eval (Name case) and after eval
  (closure/class heap objects).
- Formatter: `writeDoc` re-emits the docstring as the first body statement so
  `gusty fmt` round-trips preserve it (added `TestFormatDocstringRoundTrip`).
- AOT limit documented in ADR 0141: functions/classes are compile-time
  artifacts (class ids, method functions), so `__doc__` reads are
  interpreter-only — mirrors ADR 0131 (decorators/closures).
- Tests: `TestEvalDocstringFunc`, `...FuncNone`, `...Class`, `...Closure`,
  `...NotFirst`, plus the fmt round-trip. Full `go test ./...` green.
- Docs: roadmap (formatter DONE + docstrings DONE), language.md, operations.md
  (fmt flags), README (fmt flags + docstring note).
- Round 8 hygiene: the formatter had shipped without doc updates; this round
  back-filled roadmap/README/operations for `gusty fmt`.

## Round 15 — IR-level dead-heap-object elimination
- Added `fn.deadHeapElim()` in `pkg/lang/opt.go`: an IR-level liveness +
  escape-analysis pass that removes **whole dead heap objects** (an `rt_alloc`
  plus every mutating op on it) when the handle never escapes the function and
  is never read/printed/sliced/`%obj`-converted. Wired into `OptimizeIR`'s
  fixpoint loop (`c5`).
- Learned: `in.callee` is stored WITH the leading `@` (`@rt_alloc`, not
  `rt_alloc`), so comparisons must trim the prefix (`calleeTrim`).
- Learned: call instructions do not parse their args into `irInstr`; I parse
  `in.raw` with `callArgRegs` (taking the LAST `%` token per arg so `%obj %o`
  yields `%o`, and `""` for constants like `i32 0`).
- Added `TestDeadHeapElim` covering: dead single object, two dead objects,
  read-kept, print-kept, escape-via-append-kept.
- The `integration` parity large-program segfault is pre-existing/environmental
  (reproduces with the feature fully stashed); unit tests are the validator.

## Round 14 — Generics / structural protocols

- Implemented the roadmap Phase 9 generics/protocols item: annotations are now
  recursive generic expressions (`list[int]`, `dict[str, int]`,
  `Callable[[int], bool]`, nested `list[list[int]]`).
- Added two structural protocol kinds to `Type`: `KindSequence`
  (`Sequence[T]`) and `KindCallable` (`Callable[[...], R]`); `Name()` and
  `Same()` handle their shape structurally; JSON round-trips carry them via
  existing struct tags (machine path unchanged).
- Replaced exact-kind equality at assignment/call-argument/return checks with a
  single `assignable(got, want)` relation; concrete kinds keep exact equality,
  `any` tolerates anything, protocols recurse on element/param/return shape.
- Key learning: function-name arguments resolve to a bare `fn` (no params/ret),
  so Callable checking happens structurally at the bound rather than at call
  sites; a bare `fn` reference is assignable to any Callable bound.
- Tested parser generics, nested generics, Callable multi-param, Sequence
  protocol assign/reject, Callable assign/reject (unit-testing `assignable`),
  Name rendering, and JSON round-trip.

## Round 16 — Fuzz / property-based testing of both backends

- Added a deterministic, seeded whole-program generator for the
  interpreter+LLVM-AOT shared surface (`pkg/lang/proptest.go`):
  `PropGrammar` / `DefaultPropGrammar`, `PropSource(seed,n,g)` (source
  strings), `PropPrograms(seed,n,g)` (ASTs). Same seed ⇒ same corpus, so
  drift/failures are reproducible.
- Scope discipline keeps generated programs well-formed: top-level
  expressions read only top-level vars (bound-before-use), suite bodies
  (if/for/function) read only locals + literals. No undefined refs, no
  forward bindings.
- `pkg/lang/proptest_test.go` — unit properties: corpus reproducibility,
  parse-cleanliness, interpreter validity (no undefined names / runtime
  errors), interpreter determinism (run-twice byte-identical stdout).
- `integration/proptest_test.go` — cross-backend parity harness: each
  generated source runs through interpreter AND the `Compile`→`llc`→`cc`→run
  pipeline; stdout diffed. Interpreter failures fail the build; AOT drift is
  logged (seed+index) so the suite stays green while drift is tracked.
- `FuzzPropInterpreter` — Go-native fuzz target seeded from the corpus;
  asserts the interpreter never panics on arbitrary input.
- ADR 0148 records the decision; roadmap Phase 9 item marked DONE.
- Pushing the unpushed backlog (Rounds 10-16 commits) blocked this session:
  the SSH deploy key is passphrase-protected and no token/credential is
  available, so `git push` cannot authenticate. Local commits are intact.

## Round: class patterns in `match`

- Delivered `case Point(x, y):` class patterns in the interpreter:
  - Parser: `parsePatternAtom` now detects `Name(...)` after an ident and
    builds a `Call` class-pattern AST node (attribute-name args).
  - Interpreter: `matchPattern` adds a `*Call` case using a new
    `resolveClassID` helper (definition name or class-valued variable); it
    requires an `instance` whose `classIDs` base chain contains the pattern
    class, then binds same-named attributes to capture variables.
  - Missing attr / non-instance / non-subclass => pattern fails; non-class
    `Call` patterns keep expression-equality.
- Tests: `match_test.go` (basic, subclass, non-match, missing-attr, alias).
- Docs: `docs/language.md` match section, `docs/adr/0149-class-patterns.md`,
  `roadmap.md` (Phase 6 row).
- AOT/codegen `match` remains expression-equality only — documented as an
  interpreter-side feature.
- Push still blocked: the deploy key is passphrase-protected and no token or
  keyring is available (see top note). Commits are local-only; `git log`
  ahead-of-origin shows rounds 9-16 unpushed.

## Round 2 — L4.5: raw strings + triple-quoted strings (lexer modernization)

Implemented raw string literals (`r"..."`/`R'...'`), triple-quoted strings
(`"""..."""`/`'''...'''`), and raw triple-quoted strings (`r"""..."""`), per
roadmap Phase 4 item L4.5.

- Lexer: `scanString` handles triple/raw forms; `countNewlines` tracks lines for
  multi-line triple strings; raw-prefix detection in the identifier case.
- Tokens: `TokRawString`, `TokTripleString`, `TokRawTripleString`.
- AST: `StrLit` gains `Raw`/`Triple` fields.
- Parser: `parseAtom` + `parsePatternAtom` build `StrLit` with the decoded
  value (`t.Str`) and set the flags.
- Docstrings: any string-literal form (incl. triple-quoted) as a leading
  statement is extracted into `FuncDef.Doc`; `funcDoc` in the LSP index now
  prefers the parser-extracted `Doc`.
- Formatter: `fmtStrLit` re-emits raw/triple/raw-triple faithfully.
- Fixed an accidental codegen.go corruption from an earlier splice (restored to
  HEAD), and a `funcDoc` path that re-read the body instead of `fd.Doc`.

Tests: `strings_test.go` covers raw, triple, raw-triple, and triple docstrings;
full `pkg/lang` suite passes; `go build ./...` passes.

## Round 4 — Match exhaustiveness + definite-assignment checking (Gap B semantic half, L6.1/L6.2)

- Completed the in-progress semantic work left in the working tree: `MatchStmt`
  in `pkg/lang/semantic.go` now (a) warns when a match has NO irrefutable case
  (`case _:` or a bare-name `case y:`) and (b) intersects each case's bound
  names (`boundAll`) and defines only those in the enclosing scope after the
  match — definite assignment.
- Key discovery while writing integration tests: the `print(...)` builtin does
  NOT analyze its arguments (returns `TVoid()` without recursing into args), so
  `print(y)` never triggers undefined-name detection. Definite-assignment tests
  must use a construct that walks `inferExpr` on a Name, e.g. `return y`.
- The runtime parity test must use `assertOutput` (compiles to native and runs,
  capturing stdout): `EvalProgram` returns the last *value* (None -> 0 for a
  trailing `print`), not stdout, so `ev.Repr(out)` gave "0" not "9".
- AOT codegen already lowers guards, or-patterns, and `_`; only dict-pattern /
  class-pattern lowering remains interpreter-only. The exhaustiveness warning
  is advisory: it does not change `gusty check` exit codes (only LevelError
  does), matching mypy semantics.
- New ADR 0154, integration suite `integration/match_exhaustiveness_check_test.go`
  (6 tests), docs updates in language.md / operations.md / README.

## Round 7 — L5.3: Trailing commas (parser modernization)

- **Next roadmap item taken**: Phase 5 L5.3 Trailing commas — allow
  `f(a, b,)`, `[1, 2,]`, `{1: 2,}`, `{1, 2,}`, `(a, b,)`, and `match`
  class-pattern arg lists (`case Point(x, y,)`).
- **Parser fixes** (`pkg/lang/parser.go`):
  - call args (`parsePostfixOp`): after consuming `,`, break if peek is `)`;
  - dict literal (`parseDictOrSet`): break if peek is `}` after the comma;
  - set literal: same `}` trailing-comma break;
  - tuple (`parseAtom` `(` case): track a `trailing` flag so `(a,)` becomes a
    1-tuple (Python semantics) and `(a, b,)` a 2-tuple;
  - match class-pattern args (`case Point(x, y,)`): break if peek is `)`.
  - List literals and class patterns already tolerated trailing commas
    (loop-condition checks), so only the above needed changes.
- **Formatter**: `FormatSrc` already normalizes commas away, so trailing-comma
  source round-trips to clean canonical output that re-parses fine.
- **Tests**: new `pkg/lang/trailing_comma_test.go` — 7 tests via `EvalExpr`
  (call/list/dict/set/tuple/1-tuple/match-arg end-to-end values) plus a
  format round-trip re-parse check.
- **Full suite green**; roadmap updated; commit pushed to origin/main.

## Round: L6.4 Literal types
- Added `KindLiteral` + `LitVal` to types.go; `TLit(v)` renders `Literal[v]`; `Same()` compares literal values.
- Parser: `Literal[1]` → TLit, `Literal[1, 2]` → union of TLits.
- Semantic: `assignable` handles Literal (int→Literal allowed at semantic, Literal→int allowed; Literal→Literal requires equal value).
- Match exhaustiveness: subject typed Literal/union-of-literals is exhaustive iff every literal value is covered by constant patterns.
- Match narrowing: in `case 1:`, the subject Name is shadowed with `Literal[1]` in that case's scope.
- Runtime checkAnnot enforces Literal value on func params (Eval does NOT run the interpreter, so runtime enforcement is only reachable via the real Exec path; tests use semantic Analyze).
- Tests: literal_test.go (parse, union, exhaustiveness covered/uncovered, union exhaustiveness, return assignability).
- Conformance: integration/programs/match_literal.gy runs identically on both backends (TestConformance passes).

## Round 9 — L6.5 Type narrowing / refinement (static)
- **Deliverable**: static isinstance-if narrowing in the semantic checker.
  `if isinstance(x, T):` narrows `x` to `T` in the then branch and away from
  `T` in the else branch; `if not isinstance(x, T):` flips those.
- **Implementation**: `narrowFromCond(cond)` walks the condition conjunctively
  (only `and`; `or`/comparisons skip — no safe narrowing). Returns `pos`
  (definitely-has-T) and `neg` (definitely-not-T) maps. `analyzeNarrowed`
  temporarily shadows narrowed names in the current scope and restores them
  afterward so assignments still flow outward (no child scope — matches the
  existing if/else flow). `dropType` computes the else-branch complement by
  removing the narrowed member from a union; if the current type is dynamic or
  the complement is empty, it stays dynamic (can't represent "not T").
- **Narrowing only applies to static type names** (`typeNameToType`: int,
  float, bool, str/string, list, dict, set, tuple). User classes return nil
  (skipped).
- **Tests**: `TestNarrowIsInstanceThen` (no "type mismatch"), `TestNarrowIsInstanceElse`
  (else branch errors on narrowed-away type), `TestNarrowNotIsInstance` (flip).
- **Scoping decision**: I implemented the runtime `isinstance` builtin in the
  interpreter (jit.go) too, but it caused a dispatch issue ("undefined name")
  and required AOT codegen support for integration parity. Given time, I
  REVERTED the runtime isinstance and scoped L6.5 to the static narrowing only.
  The narrowing itself is invisible at runtime (static-only), so it's verified
  purely by semantic unit tests — no integration parity program needed.
- **Gotcha**: `assignable(Literal[1], int)` returns false in this codebase —
  literal types aren't assignable to their base type. I initially wrote a
  match-case narrowing test asserting `y: int = x` after `case 1:` and it
  failed on this pre-existing limitation (out of scope), so I removed it.
- **Revert lesson**: I used `git checkout pkg/lang/jit.go` mid-debug which
  wiped my in-progress jit.go edits. Be careful with git checkout on files with
  uncommitted work; prefer saving via git stash or committing incrementally.

## Round 2 — L10.2 ABI stability (versioned, documented C ABI for extern fn exports)

- Added `pkg/lang/abi.go`: `ABI_VERSION = 1`, stable tag words (0..14), `ABIValue`
  / `ABIUnion` Go structs mirroring the C layout, and `ABISchema()` returning a
  machine-readable JSON contract (version, struct layouts, tags, marshalling,
  IR markers).
- Emitted a versioned ABI prelude into every generated module via `EmitABI`:
  `%gusty_value = type {i32, i32}`, `%gusty_union = type {i32, i32, double, i8*}`,
  and `@gusty_abi_version = internal constant i32 1` so consumers can check
  compatibility before linking extern exports.
- Added `gustyc --abi` to dump the ABI schema as JSON.
- Documented the contract in `docs/abi.md` (stable layouts, fixed tag words,
  extern marshalling rules, stability contract).
- Tests: schema validity/version, tag-word stability, emitted-IR markers, and
  `EmitABI` idempotence.
- Golden-file gotcha: `integration/expected/ir.ll` does an EXACT string compare
  against the emitted IR, so emitting a new prelude requires regenerating the
  golden — otherwise the whole integration suite fails. Keep golden files in
  sync whenever codegen changes the module prelude.
- The roadmap text is stale (it lists L6.1/L6.2/L4.x/L5.x as not done though the
  commits exist). Reconciling against `git log` is required each round to pick
  the genuinely-next item.

## Round: L6.6 Variance + generics (ADR 0160)

- **Deliverable**: one structural subtyping relation implementing a declared
  variance table — `list[T]`/`set[T]`/`dict[K,V]` invariant, `Sequence[T]` /
  `iter[T]` / `tuple[...]` covariant, `Callable[[P...], R]` contravariant in the
  parameters + covariant return, user classes nominal over the base chain — with
  `gusty check` reporting contravariant misuse, plus the machine path
  (`Diagnostic.Code`, `gustyc --variance`, `--schema` definitions).
- **Before this round the checker was only kind-deep**: `assignable` fell through
  to `got.Kind == want.Kind`, so `list[str]` satisfied `list[int]`, a
  `fn(Dog) -> int` satisfied `Callable[[Animal], int]`, and `a: Animal` was a
  *parse error* (`buildType` only knew builtin + alias names). Class annotations
  now resolve via a token pre-scan (`scanClassNames`), so a class can be named in
  an annotation anywhere in the module, before or after its declaration.
- **Function symbols had no signature**: `analyzeFunc` defined the function name
  as `TFunc(nil, ret)`, so passing a named function to a `Callable` bound always
  took the "no parameter information -> accept anything" path and contravariance
  was unobservable. Carrying the declared parameter annotations is the change
  that makes the rule reportable. Lesson: when a rule is "never observed", check
  whether the *inputs* to the check were ever recorded.
- **Fresh literals need a covariant escape hatch**, otherwise the sound rules
  reject idiomatic code: `x: list[int | str] = [1]` must pass (nothing aliases the
  new object, mypy does the same via contextual inference) while
  `x: list[int] = ["a"]` still fails. Implemented as `freshContainer` +
  `freshContainerViolation` keyed on the *expression*, not the type — so
  `bindParams` now also returns the argument expressions (`map[int]Expr`), which
  is what let argument checking keep element fidelity without re-inferring (a
  second `inferExpr` would have duplicated "undefined name" diagnostics).
- **Message-compat discipline paid off**: rebuilding diagnostics around
  `Violation` kept the old prefixes exactly (`type mismatch: expected X, got Y`,
  `argument "f": ...`, `return type mismatch: ...`) and only *appended* the rule
  clause, so the pre-existing semantic/jit/check assertions stayed green. New
  machine fields are additive (`code`, existing `suggestion`).
- **Go gotcha**: a `switch n := e.(type)` where no case uses `n` is a compile
  error ("declared and not used") — use `switch e.(type)`.
- **Go gotcha 2**: the JSON-Schema blob in `schema.go` is a raw string literal —
  a backtick inside an added `description` silently terminates it and yields
  "syntax error: unexpected code after top level declaration". Validate with
  `json.loads` on the extracted literal (cheap python check) after editing.
- **Two real AOT bugs found while building the parity program** (verified
  pre-existing by stashing my diff and re-running: `git stash -u` then build):
  a list literal passed to a function parameter emits `call i32 @f(i32 @.lst1)`,
  and a string element in a module-scope list literal emits
  `rt_set_elem(i32 %h4, i32 1, i32 @.str1)` — both rejected by `llc-20`
  ("global variable reference must have pointer type"). Recorded as **Gap I** in
  `roadmap.md`; the parity program stays inside the supported surface
  (nominal class annotations, covariant `Sequence`, invariant list/dict).
  Calling through a `Callable`-annotated parameter is also AOT-unsupported
  (`unsupported call "f"`), so the Callable half of L6.6 is checker-only.
- **Roadmap reconciliation again required**: L5.1 (Pratt) and L5.8 were already
  implemented but still listed as planned, so the genuinely-next item was L6.6.
  Verified by reading `parseExprPrec`/`prec` rather than trusting the checklist.
- **Tests**: `pkg/lang/variance_test.go` (rules, codes+suggestions, fresh-literal
  covariance, pre-declared classes, runtime nominal check, gradual-typing
  guardrail, schema/code cross-check) + `integration/variance_check_test.go`
  (parity of `programs/variance.gy`, per-rule CLI `--check --json` codes,
  `--variance` self-description). `go test ./...` green; conformance matrix
  regenerated with the new `variance` case.
- **`Type.Same` had no `KindClass` case**, so it fell through to the `default:
  return true` branch — any two class types were "identical". That silently
  defeated the new invariance check (`list[Dog].Same(list[Animal])` was true, so
  `list[Dog]` flowed to `list[Animal]` even with the rule in place). Added
  `case KindClass: return t.ClassName == o.ClassName` + the
  `TestVarianceClassContainers` guard. Lesson: when adding a rule that delegates
  to an existing structural predicate, test the predicate itself for the new type
  kind — the rule can be correct and still never fire.

## Round: Gap I.1 — heap containers across function boundaries (ADR 0161)

- **Deliverable**: `list`/`dict`/`set` now cross AOT call boundaries as runtime
  heap handles — literal arguments are materialised with
  `rt_alloc`/`rt_set_elem`/`rt_set_add`/`rt_dict_put`, and a whole-module
  inference (`pkg/lang/heapargs.go`) types each parameter as a container from its
  annotation, its default, or any call site, so `for x in xs`, `len(xs)`, `xs[i]`,
  `xs.append(v)` and `print(xs)` behave identically on both backends. Closes the
  first half of roadmap Gap I; strings-in-containers stays as Gap I.2.
- **One bug was loud, the other was silent.** The call-site bug failed loudly
  (`llc-20: global variable reference must have pointer type`), but the callee bug
  was a *silent miscompile*: an untracked parameter turned `for x in xs` into a
  `0..handle` range loop and printed plausible garbage. Lesson: treat a verifier
  error at a boundary as evidence of a *second* bug on the far side of that
  boundary — the representation change has to be understood by both producer and
  consumer, or fixing the crash converts a loud failure into a quiet one.
- **Capability inference beats capability annotation.** Requiring
  `def total(xs: list[int])` for AOT would have made the compiled backend
  strictly weaker than the interpreter for code that already runs. The
  witnesses that made annotation unnecessary cost ~10 lines each: annotations,
  defaults (`def total(xs=[1,2])` with every call omitting `xs`), keyword
  arguments, comprehensions, generator calls, and — the one that only appeared
  once I wrote a *forwarding* test — parameter-to-parameter propagation
  (`def doubled(xs): return total(xs)`).
- **Fixed point, but bounded and guarded.** Single-pass inference said
  `total`'s parameter was an integer in the forwarding case; feeding inferred
  parameter kinds back into the variable-kind map and re-walking until stable
  fixed it. The guard matters as much as the loop: a name ever assigned a plain
  value is excluded, so a scalar `xs` elsewhere in the module cannot be dragged
  into container treatment by a same-named parameter. Bounding the loop (8
  rounds) keeps a pathological call graph from spinning.
- **Folded literals hide in more than one place.** Fixing `total([1,2,3])` was
  not enough: `total([x*2 for x in [1,2,3]])` still emitted `i32 @.lst1` because
  the *constant-folded comprehension* path builds its list global directly,
  bypassing `emitList`. Recording each list global in `staticLists` (name →
  literal) and heap-copying at the call site made both paths correct. Lesson:
  when normalising a value representation, enumerate every producer of the old
  form (`grep` for the name-minting counter, here `lstIdx`) — not just the
  canonical emitter.
- **Root keys must be per site, not per name.** `gcReg` deduplicated by alloca
  name, so two functions each with a parameter `xs` registered only one root.
  Split out `gcRegKey(b, key, allocaName)` and keyed container parameters by
  `fn + "." + name`. Same class of bug as the earlier `%_param%d` reuse: any
  module-level dedup map keyed by a *local* name is suspect.
- **Turn "emits broken IR" into "emits a message".** Strings cannot live in an
  `i32` heap slot, so the container-element path now reports
  `strings inside runtime containers are not supported by the AOT backend yet
  (the interpreter supports them)` and, under `--json`,
  `{"ok": false, "phase": "compile", "error": …}` on stdout. Documented as a
  message table in `docs/operations.md` so agents match strings instead of
  scraping `llc` output — the capability gap is part of the interface.
- **Tests**: `pkg/lang/heapargs_test.go` (17-row inference table, literal-only
  classifier, determinism, call-site/callee IR shapes, string-element diagnostic,
  per-function rooting, scalar-parameter non-regression) +
  `integration/heap_args_test.go` (13 programs asserted on **both** backends,
  verifier-green suite, CLI-visible diagnostic) + `programs/heap_containers.gy`
  in the conformance matrix (32 cases, 0 failures).
- **Process**: `go test -tags=llvm20 ./...` green, `go vet ./...` clean,
  gofmt clean; roadmap Gap I split into I.1 (done) / I.2 (strings).

## Round: L10.4 — benchmark suite + regression gate (ADR 0162)

- **Deliverable**: `gustyc --bench-suite` (+ `--bench-dir`, `--bench-baseline`,
  `--bench-baseline-update`, `--bench-gate`, `--bench-tolerance`,
  `--bench-min-ms`) measures a corpus on the interpreter and the AOT backend,
  emits a versioned JSON artifact, and gates a run against a saved baseline with
  a dedicated exit code (5). New in `pkg/lang/bench_suite.go`; schema definitions
  `benchSuite`/`benchCaseResult`/`benchReport`/`benchRegression`/`benchBaseline`.
- **A benchmark that measures nothing looks exactly like a fast compiler.** The
  first corpus case (`for i in range(100000): s = s + i`) reported `0.000 ms` for
  AOT: LLVM turns a constant-bounded additive loop into a closed form, so the
  case was measuring the optimiser's algebra. Every corpus case now does
  *opaque* work (heap list append/iteration, class dispatch, recursion, modulo),
  and `TestBenchCorpusLowers` fails if a shipped case stops running on either
  backend. Lesson: assert the *work happens*, not just that the number is stable.
- **Best-of-N is the only statistic that survives a shared machine.** Gating on
  the mean, or on the interpreter leg, produced "regressions" of 1.4–1.7x on
  unmodified code (GC and allocation churn). Gating the AOT leg with
  best-of-N + tolerance 1.5 + a 0.25 ms noise floor was stable across repeated
  runs, and still caught a doctored 65x regression.
- **Refuse to conclude rather than lie.** The noise floor is the part of the
  design that decides whether the gate is trusted: a sub-millisecond baseline is
  measuring the scheduler, so it is excluded (documented, `--bench-min-ms`).
  Likewise a case missing from the baseline is reported as `new_cases`, never as
  a failure — otherwise adding a benchmark becomes a two-file edit that people
  do off-list.
- **Exit codes are an interface, and `go run` breaks them.** Adding
  `exitBenchRegression = 5` was only observable in tests after switching to a
  built binary: `go run` reports exit status 1 for any failing program, so the
  first gate test "failed" with a misleading exit 1. CLI tests that assert exit
  codes must exec a real binary (`suiteBinPath`, built once per test binary into
  an `os.MkdirTemp` dir — `t.TempDir()` inside a `sync.Once` hands later tests a
  deleted path).
- **Timings are configuration, not source.** Deliberately did not commit a
  baseline: a machine-specific baseline fails on every other machine and gets
  muted. Baselines are generated with `--bench-baseline-update`.
- **Program stdout must be silenced while measuring** — corpus programs print, and
  a terminal write is not the quantity under test (`silenceStdout` around the
  measurement only; restoring it before reporting was a visible-output bug in the
  first draft).
- **Tests**: `pkg/lang/bench_suite_test.go` (corpus lowers on both backends,
  `BenchDir` filtering/sorting, row-preserving failures, sorted+deterministic
  artifact, gate maths: clean/within-tolerance/over-tolerance/noise-floor/
  missing-row/failed-case, baseline round-trip, self-consistent corpus gate run)
  and `cmd/gustyc/main_test.go` (`--bench-suite --json` shape, doctored baseline
  → exit 5 with exactly one named regression and a suggestion, generous baseline
  → clean, `--bench-baseline-update` artifact, missing baseline → exit 1,
  `--bench-dir`).

## Cycle: assigned containers are heap handles (Gap I.3, ADR 0163)

**What happened.** The LLVM module verifier (added as L8.2) was switched on inside
`Build`, and within minutes it had found three codegen bugs that no test in the repo
could see, all in "assign a container" territory:

1. `ys = [x * 2 for x in [1, 2]]` emitted `store i32 @.lst1, i32* %_ys` — the
   constant-folded comprehension global stored as an integer. `llc` rejects it
   (`global variable reference must have pointer type`). Same program with a literal was
   fine, so the rule "a container value is never an i32 scalar" had only been applied at
   call sites (ADR 0161), not at bindings.
2. `xs = []` at module scope allocated no slot and rooted nothing, so a later
   `xs.append(i)` stored through an undefined `%_xs`.
3. `funcDef` resets per-body codegen state but module-level code did not, so a parameter
   named `xs` could make module code reuse the *function's* alloca — a genuinely
   order-dependent miscompile.

**Lessons.**
- *Verify with the real tool, at the stage that owns the artifact.* Every one of these
  was "IR that only `llc` notices, reported at link time". Running `opt -passes=verify`
  as a pipeline stage turned them into compiler-bug reports with the verifier's own words
  (`LLVM rejected the module; this is a compiler bug, not a source error`).
- *A fix at one boundary is not a fix at the concept boundary.* ADR 0161 fixed containers
  crossing call boundaries; the same invariant had to be applied to bindings, module scope,
  and GC rooting. Ask "where else does this representation leak?" before declaring a class
  of bug closed.
- *Constant folding needs a de-folding path.* A folded value is only ever legal in
  contexts that read it structurally. Anything that stores it, passes it or prints it must
  be able to materialise it again — the fix keeps the global (indexing still folds) and
  copies it back to the heap at the store.
- *Whole-program inference must be order-independent.* The parameter-kind fixed point
  merged same-named parameters in Go map order; now it merges in sorted (function, index)
  order. Determinism is part of the CLI contract for agents, not a nicety.
- *Flaky codegen tests are a signal, not noise.* The same program alternating between
  "verifies" and "undefined value %_xs" pointed straight at the leaked per-body state.

**Coverage added.** `programs/folded_lists.gy` in the conformance corpus; unit cases in
`pkg/lang/folded_lists_test.go` (folded store, literal-shape parity, fold preserved,
rebind frees the old slot) and `heapargs_test.go` (module container rooted; parameter
slots do not leak into `main`); integration cases in
`integration/folded_lists_test.go` for both backends.

**Found and deferred.** Gap J.1 — multi-argument `print` emits one line per argument in
*both* backends, so parity hides it (the corpus only mixes strings and values inside
f-strings). Gap J.2 — set/dict comprehension assignment does not lower in AOT and the
interpreter prints sets as `<set>`.

## Cycle: L8.2 — verification is a pipeline stage (ADR 0164)

**Decision.** `VerifyModuleIR` (new `pkg/lang/verify_llvm.go`) runs
`opt -passes=verify` (+ the requested `-O` pipeline) and falls back to
`llc -filetype=null`; `Build` verifies the module it is about to link and carries the
verdict in `BuildResult.verification`; `gustyc --verify-llvm <src> [--json]` exposes the
stage alone, with `--no-verify` to opt out. A Go-side validator and a cgo `LLVMVerifyModule`
binding were both rejected: the first would drift from LLVM's real rules, the second breaks
the "textual IR + pinned external tools" toolchain rule.

**Why the structured verdict matters more than the check.** The check existed all along —
inside `llc`. What was missing was a *stage*: attributing a bad module to codegen instead of
to the link step, and a record an agent can branch on (`ok`/`tool`/`skipped`/`pipeline`/
`errors`/`note`/`toolchain`) instead of stderr containing a `/tmp/gusty-build-…` path that
differs every run. `skipped` is deliberately separate from `ok`: no toolchain must never
look like a pass.

**Payoff, same day.** Turning verification on inside `Build` found three real codegen bugs
in container binding (folded comprehension stored as an `i32`; module container without a
slot/GC root; `funcDef` state leaking into `main`) — see the Gap I.3 entry above. Two of
them were miscompilations that only ever showed up as `llc` errors in the link step, which
is exactly the failure mode this stage exists to remove.

**Process notes.**
- Normalise tool output before it reaches JSON: strip the binary name and temp path to
  `prog.ll:line:col: error: …`, drop the echoed source line and caret, cap the list.
  Otherwise every consumer learns to regex against machine-specific paths.
- Value-taking CLI flags swallow the next argv: `--verify-llvm --json "src"` compiled the
  program `--json` (yes: `-(-json)` parses). The CLI now rejects a source that starts with
  `-` as a usage error instead.
- Flags that look like one thing and do another erode trust: `--verify` runs the front end,
  so the IR check got an honest name (`--verify-llvm`) and the docs table was corrected.
- Keep `Compile` free of process spawns. The REPL/`--eval`/benchmark paths call it
  constantly; verification belongs to `Build` (which already spawns `llc`/`cc`) and to the
  explicit command.
- Documented-but-unimplemented contracts are debt: while documenting exit codes for this
  feature I confirmed the published table (`3` runtime, `4` usage) is aspirational — the
  CLI never emits either. Recorded as Gap J.3 (with J.4: `OptimizeIR`'s silent `opt`
  fallback, and J.1/J.2 found while testing containers).

## Cycle: Gap J.1 — `print(*args, sep=" ", end="\n")` (ADR 0165)

**What happened.** Both backends printed one argument per line, so
`print("a =", 1)` produced two lines. Interpreter and AOT agreed exactly, so parity,
conformance, benchmarks and every golden stayed green: the harness compares the
backends against *each other*, never against Python, and the corpus only mixes a
string with a value inside f-strings. Fixed in both backends, with `sep`/`end`
honoured for every argument kind.

**The design lever that made it cheap.** The newline had been baked into every
argument's `printf` format (`"%d\n"`, `"%s\n"`, and `}\0a` inside the dict/set
printers). Moving the terminator to the call — bare formats, plus an `i32 %nl` flag on
`rt_print_list`/`rt_dict_print`/`rt_set_print` — is what made separator/terminator
support a small change instead of a runtime rewrite. "Don't bake what a caller should
own into a shared renderer."

**Traps hit on the way.**
- *Keyword order.* My first interpreter version wrote each argument as it went and
  read `sep` when it met the keyword, so `print(a, b, sep="-")` still used the default:
  keyword arguments must be resolved before anything is written.
- *Evaluation interleaving is observable.* Printing all parts after evaluating them
  changed the output of `print("got", f())` when `f` prints. Writing each argument as
  it is evaluated, separator first, restores byte-identical behaviour with the AOT
  lowering order. Parity is about *order*, not just content.
- *Two printfs of test expectation churn were actually signal*: the set rendering
  (`<set>` in the interpreter vs `{1}` from `rt_set_print`) and the union-print
  double newline (`emitUnionPrint` had `%d\n` formats I had not parameterised).
- *A wrong test expectation is still worth writing down.* I asserted
  `print("a","b",sep="|",end="?")` should end with a newline; Python says `end`
  replaces it entirely. The test caught my own expectation, not the compiler.

**Process upgrade.** Goldens now regenerate deliberately:
`go test -tags=llvm20 ./integration -run TestCLIBuild -args -update`, and
`checkBackendParityWant` refuses to record a golden unless the interpreter and AOT
already agree — so a golden can never freeze a one-sided behaviour again.

**Found while writing the corpus.** Gap J.5: `shout("hi")` (string argument to a user
function) emits `call i32 @shout(i32 @.str1)` — LLVM rejects it, and the L8.2 verifier
now says so during `--build`; the real fix is Gap I.2's string heap kind. Gap J.2
re-scoped: interpreter set rendering is fixed, set/dict comprehension *assignment* in
AOT is still open, and `{x for x in [...] if ...}` turns out to be a parser gap too.

## Cycle: Gap J.5 — unsupported lowering must be a diagnostic (ADR 0166)

**What happened.** Writing the print corpus needed `def greet(name): print("hello",
name)`; the interpreter printed `hello ada`, and AOT emitted
`call i32 @greet(i32 @.str1)` — invalid IR that only the verifier (L8.2) or `llc` would
notice. Rather than leaving the verifier as the messenger, codegen now refuses the case
where it happens, naming the parameter: *strings are not supported as function arguments
in the AOT backend yet (parameter "name" of greet); the interpreter supports them*.

**Decision worth stating on its own (ADR 0166):** an unsupported lowering path is a
compile diagnostic raised by the stage that would emit the IR — never a module that LLVM
would reject. Three reasons: `--emit-llvm` output becomes trustworthy (if it returned IR,
it is IR), the failure names a *program construct* instead of an IR line in a temp file,
and an agent can branch on a fixed substring. The verifier stays as the backstop, worded
as a compiler-bug report so a missed path is never blamed on the program.

**Small design details that mattered.**
- Put the check inside the shared argument closure (`argVal(a, idx)`), so positional,
  keyword and default arguments are all covered — checking only the positional loop would
  have left two paths emitting bad IR.
- Report *before* `g.value` runs: lowering the string operand emits globals, so a
  late check leaves partially emitted IR behind. `res.IR == ""` on these failures is
  asserted, so a caller cannot pick up a half-built module.
- Say which backend works. "Unsupported" without an alternative makes an agent give up;
  "(the interpreter supports them)" turns it into a route.
- Pin the wording in a test (`TestStringArgumentDiagnosticIsStable`) — a message that
  agents match on is an API, so prose polish must not silently break it.

**Process note.** Discovering these while *writing corpus programs* is a cheap bug-finding
strategy: the parity harness only compares the backends with each other, so programs that
stress an unlowerable shape are the ones that find gaps — and each gap now needs a listed
message row in `docs/operations.md` plus a test, which is what keeps the catalogue honest.

## Cycle: Gap K.1 — truthiness (ADR 0167)

**What happened.** Building a both-backend corpus (for L9.6) hit the most ordinary
code in the language and fell over: `if count:`, `while total:`, `if a and b:`,
`if a < b or b > 9:`, `print(not x)`, `1 if x else 2` all failed to *compile* in AOT,
and `if 0.0:` / `if [1]:` gave opposite answers per backend. Fixed in both paths, with
`integration/truthiness_test.go` (38 cases × both backends, asserted against Python's
answer) and `programs/truthiness.gy` in the conformance corpus (35/35 parity).

**The core lesson: an assumption about representation is a bug, wherever it sits.**
Values are `i32`, predicates are `i1`. The condition paths assumed `i32`, so any
condition whose operand was a comparison (`and`/`or`, `not`, `elif`, membership) emitted
IR LLVM rejects. Fixing one call site would have been pointless — the same assumption
lived in six places. Now there is one way across the boundary (`asI1` / `asBoolI32` /
`truthOperand`), with `i1Vals` tracking which registers are predicates so the tight
path (`if a < b and b < 9:` → two `icmp` + `and i1`) stays tight instead of paying a
`zext` + re-test that `-O0` will not fold away.

**Testing the representation instead of the value** was the other half, and it was wrong
in *both* directions: the interpreter's `cond != 0` made `0.0`, `""`, `[]` truthy (heap
values are handles), while AOT's handle test made a non-empty list false. Truthiness is
about the value, so the interpreter asks the heap object (`Evaluator.truthy`) and AOT
asks the length (`rt_list_len`/`rt_dict_len`/`rt_set_len`, or the compile-time length of
a literal).

**Corpus-writing is the highest-yield bug finder here.** The parity harness compares the
backends with each other, so it cannot see a bug both share, and cannot see a shape no
program exercises. Writing programs against *Python's* answer (not "whatever both
backends do") is what surfaced:
- `ys = []` freeing heap slot **0** unconditionally: `0` doubles as "not a handle" and as
  slot 0's index, so declaring one container recycled an unrelated list. Every `rt_free`
  is now guarded (`if (h != 0) rt_free(h)`), with `freshSlots` skipping the "release the
  previous binding" step for the slot a statement has just created.
- `for k in d:` iterates nothing in AOT (Gap K.2), and `xs.pop()` / `set()` do not exist
  (Gap K.3) — the reason the truthiness tests must rebind `xs = []` to end a `while xs:`.

**Process notes.**
- A test that fails under parallel load is a defect: `TestCLIBenchSuiteBaselineAndGate`
  demanded *exactly one* regression from a doctored baseline, so an unrelated noisy case
  turned it red. It now asserts the doctored case is reported and widens the tolerance on
  the clean-baseline leg via `--bench-tolerance`, keeping the test about the gate.
- When a golden/test expectation and reality disagree, check which one is wrong before
  typing: two of my new cases failed because the *expected output* was wrong (a missing
  `print(n)`), not the compiler.
- `VerifyModuleIR` in a unit test catches the i1/i32 class immediately — five of these
  bugs predate today and were invisible to every existing test.

## Cycle: Gap K.2/K.4/K.5 — container iteration, item assignment, `{}`

**What happened.** Continuing the corpus work, `d = {}` + `d[1] = 2` + `print(d[1])`
printed nothing on **both** backends. The parity harness was useless here: both sides
agreed — on doing nothing. Root causes stacked three deep:

1. The parser accepted `d[1] = 2`, consumed `= 2`, and returned an *expression
   statement* — the assignment was thrown away. `1 = 2` and `f() = 2` produced **zero
   statements** because `parseTopLevel`'s recovery loop recorded only `*ParseError` and
   dropped every other error kind.
2. Neither backend implemented item assignment (interpreter matched Name/Tuple/Attr
   targets only; codegen rejected `Index`).
3. `{}` parsed as an empty **set** — so the interpreter called `d[k] = v` a set mutation
   (`not in set`) while codegen emitted dict code for the same token.

Plus two latent ones found on the way: the checker bound a `for` variable to the
*iterable's* type (so `for k in d: s = s + k` was rejected as `int + dict[any, any]`),
and the loop variable's `alloca` sat inside the body block — which does not dominate the
code after the loop, so `for k in d:` followed by `for k in m:` failed the verifier
("Instruction does not dominate all uses").

**Decisions worth stating (ADR 0168).**
- **A compiler must not lose statements.** The parser rule is general, not "support Index
  targets": any target that cannot be assigned is a `*ParseError`, and `parseTopLevel`
  converts *any* non-`ParseError` from `parseStmt` into one rather than recovering past it.
- `rt_set_elem` **appends** (it bumps the length), so item assignment needed
  `rt_put_elem`; the bounds test is emitted around the store and takes the existing raise
  path with `IndexError`'s code. An unchecked write past the end of a container is how you
  get today's `rt_free(0)` class of bug tomorrow.
- `{}` is an empty **dict**; `set()` doesn't exist yet, so "write `{}` for an empty set"
  is not an option — that gap is K.3, recorded not papered over.

**Debugging notes (what actually found the bugs).**
- `opt-20 -passes=verify` on `--emit-llvm` output named the dominance bug in one line. It
  is the fastest oracle in this repo — five bugs so far.
- The `--build` failure that *only* printed warnings and exited 1 was maddening: `--json`
  had the reason (`verification.ok=false`) while the human path lost it, because the error
  branch printed diagnostics **or** the error. Recorded as Gap K.7; fixing it as its own
  commit (one commit per feature is a promise to my future self, not bureaucracy).
- Three of my own test expectations were wrong before the compiler was: iterating
  `range(3)` then a dict sums to 93, not 14, and a `while` test had no `print` in it at
  all. Check whose expectation is wrong before typing a fix.
- Scope resets must cover every per-scope map. `beginScope()` now owns
  allocd/gcRootSeen/listVars/runtimeDicts/runtimeSets/freshSlots; the previous fix reset
  only the first two, so a dict named `d` inside a function made module code free `%_d`
  before main allocated it ("input module is broken").
- Registering a variable's container kind from the checker's inferred type
  (`d = make(3)`) interacts with the "rebind to a non-container ⇒ free the old slot"
  heuristic: registering then immediately letting that heuristic *un*register it made
  iteration silently revert to comparing the index against the handle. Both now share one
  computed `boundKind`.

**State.** Suite green; conformance corpus 36/36 at parity; gaps K.2/K.4/K.5 closed,
K.3/K.6/K.7 recorded for the next cycles.

## Cycle: Gap K.7 — a failed build must say why

**What happened.** I lost ~20 minutes to a `--build` that printed two warnings and exited
1. The real reason (`verification.ok=false`, "input module is broken") was only in `--json`,
because the CLI's error branch printed diagnostics **or** the error, and `Build`'s
codegen/`llc`/source-map failure paths returned `nil` results, so scripts lost the
diagnostics that humans got.

**Lessons.**
- **Symmetry of failure output is a contract, not polish.** The rule now: every failure
  prints *all* diagnostics *and* the reason, exits non-zero, and the same information
  arrives in the partial `BuildResult` under `--json`. Documented as "Failure output
  contract" in `docs/operations.md`. This is exactly the agent-facing promise — *don't
  scrape stderr* — being kept, and I had been violating it while using the tool myself.
- **My first test for this was written against the wrong string**: the actual line was
  `gustyc: build: codegen: codegen: unsupported call …` (doubled stage prefix, because
  `Build` added `build: codegen:` on top of `GenerateIR`'s own `codegen:`). Two fixes, one
  test asserting the *absence* of the repeat — assert the shape of the message, not just
  that some message exists.
- Quick repro for next time the CLI seems silent: `--json --build=…` and read
  `.verification.errors`; and run `opt-20 -passes=verify` on `--emit-llvm` output.

## Cycle: Gap K.6 — an uncaught exception must report and fail

**What happened.** `xs = [1,2,3]` + `xs[9] = 5` exited 1 with a traceback on the
interpreter and **exit 0 printing nothing** compiled. Four stacked defects: the raise-exit
block was `ret i32 0` and there was no message global to print; the checker's `exceptions`
map was **declared but never populated**, so `raise ValueError("boom")` was an interpreter
program that failed AOT with `undefined name "ValueError"`; runtime errors were not
exceptions in the interpreter (`except IndexError:` could not catch them — while the AOT
*did*, an inversion worth writing down); and the interpreter wrote tracebacks to stdout.

**Decisions (ADR 0169).**
- Report on **fd 2 via `write(2, …)`**, not `printf`, and not `fprintf(stderr, …)`:
  `stderr` is a glibc *symbol*, a macOS macro, a Windows text macro — the fd is the only
  stream that means the same thing everywhere. Exit 1 from main.
- `@exn_msg` travels with the flag through ONE helper (`setExn`/`raiseTo`), so the printed
  text can never disagree with the code `except IndexError:` matches on.
- Checks live at the **read site**, not inside `rt_get_elem`/`rt_dict_get`: iteration calls
  those helpers with in-range indexes, and a flag set by a helper nobody tests leaks into
  the next `checkExn` after a user call.
- One exception table (`pkg/lang/exceptions.go`) for interpreter + codegen + checker. The
  bug class "two front ends disagree about whether a builtin exists" is now structurally
  impossible for exception classes.

**Bugs the new checks exposed (all invisible to the parity harness).**
- AOT **reads** were unchecked: `xs[5]` printed `0`, `d[missing]` printed `0`.
- `rt_dict_has` walked the flat `[key, value]` array by 2 but bounded the walk by the
  *entry count*, so only the first ⌈count/2⌉ keys were ever found: `3 in {1: 2, 3: 4}` was
  false in compiled binaries, and `d[k]` for a later key raised KeyError once reads were
  checked. Nothing had ever asked a compiled program about a dict's later keys.

**Process notes.**
- Writing the *test table* first keeps paying off: `TestRuntimeErrorsAreCatchableOnBothBackends`
  asserted Python's answer and immediately failed four cases that "both backends agree"
  tests would have blessed.
- Two self-inflicted 20-minute losses, both the same lesson: **LLVM-IR comments are `;`,
  not `//`, and a backtick inside a Go raw string ends the literal.** Both times the module
  verifier said it in one line ("expected top-level entity", "invalid redefinition") — run
  `opt-20 -passes=verify` on `--emit-llvm` output before reading the code.
- Duplicate runtime helper: I added a second `rt_dict_has` before searching for the
  existing one. `grep` the IR strings first; the verifier only says "invalid redefinition".
- `--json` is my debugger: it showed `verification.errors` when the human output only
  printed warnings (that became Gap K.7, fixed in its own commit).

## Cycle: Gap K.3 — pop, set(), and the set/list/dict methods

**What happened.** `xs.pop()` was `no such list method pop` in the interpreter and
`string method pop on non-constant string` in the AOT (method dispatch is a chain of
per-receiver-kind cases, and heap sets had no branch, so `.pop` fell through to strings).
`set()` was `unsupported call for eval`, and — worse — `s.add(1)` was `attribute access on
method`, so the constructor I had just added produced a value nothing could grow. `{}` being
the empty dict (ADR 0168) had left the language with **no way to write an empty set**.

**Decisions (ADR 0170).** `pop` returns the popped element (unlike `append`, which returns
the handle for the REPL — an expression's value is what the program uses); `rt_pop` shifts
the tail left and shrinks the length because the existing `rt_set_elem` appends; the
constructors emit `rt_alloc(kind)` rather than lowering to an empty literal — the empty
literal gets constant-folded to a global and I reproduced the ADR 0163 bug
(`store i32 @.set1, i32* %_s`) before the verifier told me; `discard` is silent, `remove`
raises `KeyError`; `print(set())` is `set()` in AOT too (it printed `{}`, which is a dict);
`list(xs)`-style copies stay an honest AOT diagnostic.

**The lesson I keep needing to learn — four times now, all in embedded IR strings:**
- `//` inside an LLVM-IR raw string → "expected top-level entity"
- a backtick inside the raw string → the Go literal ends mid-comment → syntax error miles
  away, or IR leaking into Go
- a second `rt_dict_has` → "invalid redefinition of function"
- adding a branch *before* a loop header without updating the loop `phi`'s incoming block →
  "input module is broken"
So `pkg/lang/runtime_ir_test.go` now scans every embedded IR block for `//`/`#` comments,
stray backticks, unbalanced `define` blocks, and duplicate helper definitions. I verified it
fires by planting a `//` comment. **When I make the same mistake four times, the fix is a
test, not more care.**

**Other notes.**
- `set`/`list`/`dict` were not just unimplemented, they were **unbound names in the
  checker** — the failure happened before codegen, which is why `set()` looked like a
  codegen gap and was actually four layers deep (parser-side builtin table, checker, both
  runtimes).
- I again nearly misread parity: the CLI `--eval` path echoes the final value, so
  `interp="...|0"` vs `aot="..."` is not a mismatch. The library `runInterp` has no echo —
  use the harness, not the CLI, for stdout comparisons.
- `python3` is installed here: `python3 prog.py` gives the oracle for free. The container
  program matched Python on every line except our `1`/`0` booleans, which is documented
  language behaviour, not a bug.

## Cycle: Gap K.8 (part 1) — tracebacks name a line, and the parser is where it starts

**What happened.** Adding the frame line to the compiled report exposed that **`raise`
statements had no span at all**: the parser built `&RaiseStmt{Expr: ex}` and never filled
`Src`, so the *interpreter's* traceback said `File "prog", line 0, in boom`. Nobody noticed
because every try/except test caught its exception before printing one. A report whose line
number is fabricated is worse than no report — it is trusted.

**Also found: `strConst` did not escape `"`.** A frame contains quotes, the emitted LLVM
literal ended early, and the verifier complained
`constant expression type mismatch: got type '[7 x i8]' but expected '[31 x i8]'` — an error
message that points nowhere near the cause. Any program string with a double quote had the
same bug; the traceback was just the first string in this repo that needed one. Now the
emitter escapes `\`, `"`, newline, tab, CR.

**Decisions (ADR 0171).**
- Statements carry the line they were written at, or the parser has lied. Every AST node
  that can fail at runtime must answer "which line".
- Raise sites store a **pre-rendered** frame string in a new `@exn_frame` global; `rt_die`
  prints header / frame / exception line. No printf, no varargs in the raise path, and the
  verifier checks the literal's length for me.
- An unknown span prints **no** frame rather than `line 0`.
- The compiled report shows the raise site's own frame; caller frames still need L8.5, and
  Gap K.8 stays 🟨 PARTIAL rather than being closed on a technicality.

**Process notes.**
- My first version of the new integration test failed on *both* backends and told me more
  than the fix did: the interpreter's frames were wrong too. When a test fails on the
  reference path as well, the reference path is the bug — do not weaken the assertion.
- I wrote a `FinalizeTracebackOf` test helper before checking that `EvalExpr` already calls
  `ev.FinalizeTraceback(err)`. Read the entry point you are calling *before* writing
  scaffolding around it; the helper is gone and the test is shorter.
- `gofmt -l` flags `pkg/lang/parser.go` for pre-existing blank-line drift. I checked
  `gofmt -d` to confirm my edit was clean instead of "fixing" unrelated formatting — that
  check takes five seconds and keeps diffs reviewable.

## Cycle: Gap J.3 — the exit-code table had to become the implementation

**What happened.** `docs/operations.md` promised `3 = runtime`, `4 = usage`; the CLI emitted
neither. Parse/usage/front-end all returned `2` — the *same code as an LLVM module
rejection* — and a trapped program shared `1` with a compile error. So the documented
distinctions an agent was told to branch on did not exist.

**Now implemented:** 1 compile error · 2 LLVM rejected the module *we* emitted (compiler
bug) · 3 the program ran and trapped · 4 CLI usage error · 5 benchmark regression.

**Findings worth keeping.**
- **`flag.ExitOnError` exits with Go's own status `2`.** That silently collided with my
  verification code. A CLI that owns its exit codes must use `ContinueOnError` and classify
  the parse failure itself.
- `reportCompileErr` was labelling compile failures as *usage* errors in the `--json`
  payload (`"exit": 4`) — the kind of drift that happens when one constant (`exitErr`) means
  three things. Splitting the constants made each site a decision rather than a habit.
- `--json --eval '<unparsable>'` printed **no payload at all**: the machine path only
  existed on stderr. Now parse failures come back as
  `{"ok":false,"phase":"parse","error":"1:1: …","errors":[{line,col,msg}],"exit":1}` —
  spans included, because making an agent regex `gustyc: parse error at 1:7: …` is not a
  machine interface.
- The docs' JSON examples were stale in the same direction as the table (`"exit": 2` for a
  compile failure). Docs drift and code drift usually; audit both together.

**Found on the way (Gap K.10, still open).** `print(undefined_thing)` at module level passes
the checker — its arguments are not analysed — so codegen emits a reference to a slot that
does not exist, LLVM rejects the module, and the contract correctly says *compiler bug*
(exit 2) for what is an ordinary typo. `x = undefined_thing` **is** caught, which localises
the hole to the print-argument path. Fix: analyse print's args, and make codegen refuse an
unbound name with an actionable message (ADR 0166) rather than emitting a dangling slot.

**Process note.** Writing `TestCLIExitCodeContract` as a table over the *documented* rows is
what surfaced both the `reportCompileErr` mislabel and Gap K.10: the doc says one thing, the
binary says another, and the test is the only place they have to meet. When I wrote the
contract test I also got one expectation wrong (`--eval` of an undefined name is a *runtime*
failure, since the interactive path doesn't run the checker) — the fix was to test the
checker-gating modes (`--check`, `--build`) for exit 1 and keep exit 3 for `--eval`, not to
make the numbers agree artificially.

## Cycle: Gap K.10 — a typo is a source error, so it must die in the front end

**What happened.** Fixing the exit-code contract made a lying bug visible: `print(undefined_thing)`
passed the checker (print's arguments were never analysed), codegen emitted
`load i32, i32* %_undefined_thing` for a slot that did not exist, LLVM rejected the module —
and the contract correctly reported the user's typo as a *compiler bug* (exit 2).

**Fixed at three levels, not one.** The checker now analyses builtin-call arguments; codegen
refuses an unbound name with an actionable message (ADR 0166); and the built-in names moved to
**one table** (`pkg/lang/predeclared.go`) that the checker predeclares, the codegen guard
consults and the LSP offers in completions.

**The most interesting finding.** `print(sum(xs))` did not compile — while `sum` worked in
*both* backends. The checker had never predeclared it, so it was "undefined name" at analysis
time. Three lists existed independently: the checker's predeclarations, the LSP's `builtins`,
and codegen's dispatch. Drift between them was invisible to the parity harness (both backends
agreed — they both failed). A single table plus
`TestPredeclaredTableCoversTheLSPList`/`TestBuiltinsArePredeclaredInTheChecker` turns that
class of drift into a test failure.

**The guard found real bugs, not false alarms.** Adding `nameIsBound` surfaced bindings that
allocated a slot without registering it — `with … as m:` was rejected as "undefined name m".
That is the guard doing its job: it encodes the invariant *"every name you can read has a slot
you allocated"*, and code that violated it had only worked by luck.

**Two smaller lessons.**
- **Reserved words are not builtins.** `type` sat in the LSP completion list although
  `type(x)` never parses — the editor suggested source that would not compile. Drop it from
  both lists; assert parseability of every predeclared callable in the test so this stays true.
- **Diagnostics can leak the AST.** The container-copy refusal printed
  `&{list {%!s(int=2) %!s(int=7)} fn() -> list[any]}(<container>) …` because it formatted a
  `*Name` node with `%s`. `calleeName(c)` now renders what the user wrote. Nobody checks the
  shape of an error message until a user pastes one; render names, not nodes.

**Process note.** My probe loop was broken twice before I found it: `--check` is *stdin*-only
(`gustyc --check file.gy` reads stdin, ignores the path, and exits 4), so every "parse error"
in the first sweep was an artefact of feeding it nothing. When a probe reports the same failure
for all 40 cases, distrust the probe first.

## Cycle: Gap J.4 — every stage of the pipeline has to testify for itself

**What happened.** `OptimizeIR` ran the real `opt` pipeline and swallowed every error: missing
tool, rejected IR, empty output — all returned the unoptimized module and the build still said
"built". `--opt-level=2` silently meant `-O0`. Same class as K.7 (failed build must say why) and
L8.2 (verification is a stage, not a side effect).

**Now:** `OptimizeIRReport` returns `Optimization{tool, pipeline, level, applied, fallback,
note, error}`; it rides on `BuildResult.Optimization`, on every post-stage failure result, into
`--build --json`, the schema (`--schema` → `optimization`) and human output
(`optimized by opt-20 (-O2)` / `NOT LLVM-optimized: <note>`).

**Design rule worth keeping.** Define the boolean so the *fallback can never claim success*:
`applied` means "the real LLVM optimizer ran", and the textual pass sets `applied: false,
fallback: "textual"`. My first version set `applied: true` for the textual pass, which would
have made the field exactly as useless as the missing one.

**The bug that fell out of the test.** Writing `--build out src.gy --opt-level=2` in the
integration test failed with `open --opt-level=2: no such file or directory`: Go's `flag`
package stops at the first positional, so any flag typed after the source file became a
filename. `reorderFlags` hoists flag tokens to the front, consuming the next token as the
value only for non-bool flags written `--flag value` — so `--eval --help` still evaluates the
text `--help` instead of printing usage, and unknown `--flags` still reach the flag package's
own "flag provided but not defined". Agents type flags last; a CLI that only accepts them
first is a trap.

**Process notes.**
- Three of my patches to `codegen.go` aborted mid-script because an earlier `assert` failed
  *before* `open(...,'w')`, so **nothing** was written — including the parts that had matched.
  Patch scripts should apply-then-write once at the end and print what they matched; I wasted
  several rounds re-discovering which half of an edit had landed.
- My all-caps "probe" loops keep producing uniform results from a broken harness (`--check` is
  stdin-only, so 40/40 "parse errors"). Uniform output across cases = suspect the probe first.
- Distinguishing "the tool ran and changed nothing" from "the tool never ran" needed an explicit
  report; `optimized != ir` was a heuristic that could not tell them apart.

## Cycle: Gap L.1 — None was the integer 0 (and every program leaked a `0` line)

**What happened.** Writing a None test made two things obvious at once: `print(None)` printed
`0`, and `gustyc --file prog.gy` printed the program's output **plus a stray `0`** — the CLI
echoed the last statement's value unconditionally, and `print` returned the int 0. Parity
testing could never catch this: both backends were wrong identically. Only comparing against
*Python* exposed it.

**Fix:** None is a singleton (`tagOfVal` → `TagNone`, interpreter heap object, AOT heap kind 4
cached in `@none_h` via `rt_none()`, permanent GC root); void functions and bare `return` yield
it; the CLI echoes only when the last statement is a bare expression **and** the value is not
None.

**Representation reasoning worth keeping.** In AOT every value is an untagged `i32`, so a
reserved sentinel is impossible — every `i32` is a legal integer and `x = -2147483648` would
print `None`. Tagging every value is disproportionate for one null. Reusing the existing
`rt_alloc(kind)` object model (kind 4, allocated once, never freed, handle identity equality)
cost almost nothing because containers already work that way. **Look for the null that fits the
representation you already have.**

**Three bugs my own tests caught (they were wrong, and that is the point):**
1. My `fdReturnsValue` hand-walked statements and missed `return` nested inside `match` — the
   conformance corpus printed four `None`s. Now it walks the AST with reflection and also counts
   `yield` (a generator is not a procedure).
2. Static folding of `f() == None` deleted the call, losing the callee's `print`. Rule: a static
   answer must still evaluate its operands. Same reason `print(emit())` must call emit() before
   writing "None".
3. My test asserted `call void @emit(` — the real signature is `call i32 @emit(`. The compiler
   was right, the expectation was wrong: check which one is lying before "fixing" the compiler.

**Also found:** bare `return` (no expression) failed AOT codegen outright with
`codegen: unsupported expression <nil>` — an untested corner of the grammar, now returning None.
And `x = None; x = 0` must clear None-ness: variable-kind maps record the *latest* assignment, so
every registration path needs its clearing path (same lesson as containers-as-heap-handles).

**Process notes.**
- Backticks inside the embedded-LLVM raw string broke the build again (the comment said
  `` `x = None` ``). The guard test in `runtime_ir_test.go` exists for exactly this; write IR
  comments without them.
- `--opt-level=2`-style probes taught me to check `flag` semantics; `--emit-llvm` takes a source
  *string*, not a path — probes that silently read nothing produce uniform fake results (third
  time this loop; suspect the harness first).
- Interpreter void-return semantics lived in **three** duplicated call paths (`callFunc` plus two
  inline copies in `evalCall`). Fixing one and seeing the tests still fail was the signal. That
  duplication is a real defect: it is where the next semantic divergence will come from.

## Cycle: Gap I.2 (interim) — ten ways to make valid code look like a compiler bug

**What happened.** A probe table over "put a string in a container" showed **ten** shapes
emitting IR that LLVM rejected (`rt_set_elem(i32 %h, i32 0, i32 @.str1)`), so the exit-code
contract told users their two-line program was a *compiler bug* (exit 2). The existing guard
(`heapElem` rejecting `*StrLit`/`*FString`) had three holes: it missed folded strings
(`str(42)`), string variables, and — the actual path for `xs = ["a","b"]` — the
materialise-assigned-literal site that called `value()` directly instead of `heapElem`.

**Now:** one predicate (`irGen.rejectRuntimeString`: literal / folded string / string-bound
variable) applied at every container-slot write, with `heapElem` as the choke point. All ten
shapes fail as compile diagnostics (exit 1) naming the backend that works and the roadmap
entry; int containers still compile and verify.

**Lessons.**
- **A guard at one boundary is not a guard at the concept boundary.** The container rule lived
  in a type switch on two AST nodes while the operation ("write into a heap slot") had four
  call sites. The fix is a predicate over *meaning* ("is this a string?") plus one choke point
  per *operation*. When I harden a rule, I should enumerate the operations, not the syntax.
- **Table-driven probes find the whole class.** Writing the ten-shape probe took two minutes
  and immediately produced a list; my earlier single-case check would have shipped four of them.
- **Diagnostics are user-visible contracts.** Two old messages ("dict literal keys must be
  constant integers") were terse status lines; tests asserted their exact text, so improving
  wording meant editing the assertions. Fine — but the assertion should check *properties*
  (names the working backend, no verifier verdict leaked), which is what the new test does.
- **Deferred the real fix deliberately, with the design written down.** An interned string
  table (`rt_str_intern(i8*) -> i32`, `@str_tab`, per-container element kind, `HeapStr` param
  kind) unlocks strings in lists/dicts/sets, Python-quoted reprs, and string function args
  (Gap J.5) at once — but it touches the value model, so it is its own cycle. The roadmap entry
  now contains that plan so the next cycle doesn't rediscover it. Shipping "honest refusal"
  first is still a real improvement: exit 1 with instructions beats exit 2 blaming the compiler.

## Gap I.2 done — strings live in runtime containers now (ADR 0173)

`xs = ["a", "b"]` compiled to `rt_set_elem(i32 %h, i32 1, i32 @.str1)`: a global pointer in an
i32 slot, rejected by LLVM, reported as exit 2 — a compiler bug for a two-line program. It now
compiles, runs, and prints `['a', 'b']` on both backends.

- **Interning beat tagging, and beat bitcasting.** A container slot is an i32 and a string is a
  compile-time global, so the options were: tag every word (a representation change and a
  shift/extend on every container access), bitcast the pointer into the i32 (loses bits on
  64-bit, and asks the verifier to bless nonsense), or store an index into a runtime string
  table. Interning costs one `strcmp` scan at the store site, changes no representation, and
  gives a bonus: content-addressed identity, which is exactly what a dict key needs.
- **Two tables, one index — because printing has two contexts.** `@str_tab` holds the raw text
  (`print(name)` → `hello`) and `@str_repr_tab` holds Python's repr (`print(names)` →
  `['hello']`). One slot cannot serve both, and choosing at print time is what lets
  `print(x)` and `print([x])` each be right. `rt_print_value(v, isStr, quote)` is shared by the
  list, set and dict printers, so the three renderers cannot drift.
- **The repr rule is one function used by both backends.** `pyReprString` implements Python's
  choice — single quotes, unless the text contains `'` and no `"` — and is called by the
  interpreter's `reprNested` and by codegen when it emits the intern call. `["it's", 'plain']`
  comes out identically either way, and one unit test compares the function's answers to
  CPython's `repr` output.
- **A shared choke point is only as good as its coverage — again.** `heapElem` had been the
  guard for stores, and `heapElemKind` is now the one place a container word is produced. The
  last shape still emitting a raw global was `x in xs` (`rt_contains(i32 %h, i32 @.strN)`),
  which is not a store at all: a *read* needed the same normalisation. Membership, indexing,
  iteration and printing each needed their own wiring even though they all store the same
  index.
- **Track meaning per container *and per dict side*.** `listElemStr`, `setElemStr`,
  `dictKeyStr`, `dictValStr` — a dict with string keys and int values (`{'ada': 3}`) needs the
  two sides distinguished, and all four are saved/restored in `beginScope`, continuing the
  per-scope discipline that fixed the parameter-kind leak (commit `5232142`).
- **The interpreter had a matching bug, found only by asking Python.** Dict printing used
  `%v` on the stored key, so a string key printed as its **heap handle**: `{1048581: 1}` for
  `{'k': 1}`. A parity test compares the backends to each other and cannot see it; only the
  corpus case whose expectation came from CPython did. Rendering values *inside* containers now
  goes through `reprNested`, which quotes strings and prints keys with `Repr`.
- **Expectations written before the shape worked have to be inverted, not deleted.** The
  interim cycle's tests asserted *refusals* for ten shapes; nine of them now compile. Each was
  rewritten to assert the working behaviour (output compared to Python's), and
  `TestIntContainersStillBuild` stays behind it so the guard cannot quietly become a blanket
  refusal of containers.
- **`--file`/`--eval` stdout and the `.want` files are only as good as the oracle.** For this
  feature the expected output came from running the same program under CPython; the two lines
  that differ (`True`/`False` vs `1`/`0`) are the documented bool convention (Gap L.2), noted
  in `docs/language.md` rather than silently normalised to our own output.

Still refused, on purpose: an element whose string comes from a `str`-typed *parameter* has no
compile-time text to intern, and reports the ADR 0166 diagnostic naming the interpreter.

## The most obvious invocation was a silent no-op (`gustyc prog.gy`)

While wiring up string containers I typed `gustyc prog.gy` out of habit and got the usage
banner with exit 0 — no compile, no run, no error. The usage line had always read
`gustyc [flags] [<src>]` and `docs/operations.md` called `--file` "alias for a positional
source", but no branch in `run()` read a positional argument any more.

- **Fix:** a single positional naming an existing file becomes `--file` (identical behaviour,
  `--json` output and exit codes included); anything else is evaluated as source text, like
  `--eval`; and a name ending in `.gy` that does not exist is a usage error (exit 4) saying
  `no such file`.
- **Why the third rule:** without it, `gustyc prog.gy` from the wrong directory evaluated the
  *text* `prog.gy` and failed with a runtime `undefined name prog` — exit 3 blaming the user's
  program for a shell mistype. A missing file is a CLI problem, and the exit-code table says
  usage problems are 4.
- **The lesson about advertised interfaces:** everything in this repo is tested — 39
  conformance programs, exit-code contracts, JSON schemas — and yet the single most natural
  invocation was broken, because every CLI test passed a flag first (`--file`, `--eval`,
  `--version`) and none passed a bare path. Tests written as a list of flags cover the flags,
  not the shape of the command line. The new tests run `gustyc prog.gy`, `prog.gy --json`, and
  a mistyped path, i.e. the way people actually type.
- **Parity check as a test:** the fix asserts the positional and its documented alias `--file`
  produce byte-identical output and the same exit code, so the two can't drift again.

## Gap J.5 done — strings cross a function boundary (ADR 0174)

`shout("hi")` compiled to `call i32 @shout(i32 @.str1)`. With Gap I.2's interned table the
representation problem was already solved; the rest was making the compiler *know* which
parameters carry a string index.

- **The call site decides, the callee is told.** `strArgKinds` infers string parameters from
  annotations, defaults and call sites, to a fixed point in sorted order. Deciding inside the
  callee (by looking at its body) would print an integer argument through the string table: the
  argument's type is known where the argument is written.
- **Equality became free.** Interning makes equal text the same index, so `s == "yes"` is an
  integer test, not a character loop — a case where the representation choice paid for itself
  twice.
- **Three facts, not one.** Printing `echo("yo")` needed "this function returns a string";
  `print(names[1])` after `fill(names, "one")` needed "this helper fills the container parameter
  at these positions"; and printing the whole container needed the *object's own* runtime flag
  (`@estr[h]`, set by `rt_mark_estr` at every store). Each of the three fixed a different wrong
  output that the previous one left behind — element reads, whole-container prints, and
  cross-function mutation are three separate consumers of one idea.
- **A container's element kind is a property of the object.** A static map cannot express "the
  caller's list holds strings" when a helper appended to it. The runtime flag is the honest
  answer; the static maps remain only where a compile-time decision is unavoidable (element
  reads, loop variables).
- **Whole-program inference keyed by name is whole-program coupling.** `heapArgKinds` recorded
  "variable `s` is a set" globally, so `def ins(s, v): s.add(v)` silently vetoed `echo(s)`'s
  string parameter in an unrelated function — my string analysis looked broken but the bug was
  in the container one. `heapASTWalker` now carries the enclosing function as a scope. This is
  the second time (after `5232142`) that a per-name table needed to become per-function-per-name;
  the next such analysis should start scoped.
- **`len` could not call `strlen`.** The runtime already declares `strlen` returning `i64` and
  LLVM keys declarations by name, so a second declaration failed the module — the fix was a
  byte-counting loop, which also keeps the runtime self-contained.
- **Silent miscompilation is what the guards are for.** `s + 1` on a string parameter *compiled*
  and returned `index + 1`, where the interpreter raises `TypeError`. Now an operator on a string
  is a compile diagnostic. Likewise `f("a")` plus `f(7)` used to print `str` then `(null)`; the
  inference records negative evidence and refuses instead of guessing.
- **Expectations that were written for a refusal get inverted, not deleted** — same lesson as
  Gap I.2's tests, applied to `string_args_test.go` on both sides. One CLI test had to change its
  source: its warning (`1 + "a"`) is now itself a codegen failure, so the warning and the failure
  were no longer independent facts; it uses a non-exhaustive `match` warning instead.

## Gap J.6 — literals intern, containers are homogeneous, and replacement is not growth

Probing (not the test suite) found three wrong answers in the container story I had just
finished: `d = {"a": 1}` refused by an up-front constant-int check, `print({"a": 1})` printing
`0`, and `print([1, "a"])` printing `[(null), 'a']`.

- **The worst bug is the plausible one.** `[(null), 'a']` compiles, verifies, exits 0 — it is a
  *number-shaped* wrong answer. The element kind was recorded once per container, so the printer
  applied the string table to the integer `1`. Any guard at a boundary is only as good as the
  concept boundary it sits on: the rule is about the *container's contents*, so it needs a
  predicate on contents (`literalNeedsHeap`, `literalMixedKinds`) plus enforcement at each
  *operation* (build, append, add, store, print, len).
- **Two lowerings, chosen by a predicate, not by an error.** The heap dict builder that can intern
  already existed; `dictLiteralKeys` was rejecting string keys before it could be reached. The fix
  is a decision (`all-constant-int → static global`, `any string → heap object`), not another
  refusal. Same for `print` (runtime printer, never `%d`) and `len` (runtime length, not the static
  count field).
- **Growth vs replacement is the distinction that made the guard correct.** The first version
  refused any container that held both kinds and immediately broke Gap I.2's own conformance case
  (`xs = [1]` then `xs[0] = "s"`, which the interpreter prints as `['s']`). Item assignment
  overwrites a slot, so the new element's kind *is* the truth there (`replaceElemKind`);
  `append`/`add` grow, so mixing there is reported (`recordElemKind`). A guard written before you
  know which operation it guards is a bug generator.
- **Refusing beats guessing at print time.** "Render small indices as ints" would have made
  `[1, 'a']` look right and silently misrendered a real string whose index is small. Heterogeneous
  containers need per-element tagging — a representation change — so the honest state is exit 1
  with a message that names the interpreter and the reason.
- **Every per-scope fact belongs in `beginScope`.** The four new number maps are saved/restored
  with the rest, which is why nested functions and a helper's body cannot corrupt the caller's
  view of a container.
- **The suite's blind spot again:** all three bugs were in shapes no test had typed — a dict
  *literal* with string keys, a bare container literal in `print`, a mixed list. Tests written as
  a list of features cover the features, not the combinations; the probe loop (many shapes × both
  backends × CPython) is what found them, and each finding became a regression test.

## L5.8 — incremental parsing reuses both sides of an edit (ADR 0176)

**Measured before designing.** The cache "existed" per the roadmap, but a probe
answered the real question: editing line 1 of a 21-statement file reused **0**
statements and re-parsed all 21. Reuse was prefix-only — everything from the
first affected statement to EOF went back through the parser. An item marked
done in a plan is a claim; a probe on the actual shape (an edit near the top of
a file) is the evidence, and it turned "already implemented" into the commonest
worst case an editor can have.

**Reuse must not be allowed to lie about spans.** A reused node keeps its
`Span`, so reusing statements below an edit that *inserts a line* would leave
hover and squiggles pointing at lines that no longer hold that code. The tail
rule is therefore conditional and conservative: one edit, inside one line,
adding no newline, and the bytes after the edit identical up to a constant shift
— checked by comparing the text, not by trusting a length difference. Multi-line
edits and multi-edit notifications take the old full-reparse path, so the new
code can only ever save work, never substitute a stale tree for a correct one.
A test asserts a node is *not* allowed to survive a line insertion.

**Byte offsets must travel with reused nodes.** The tail's recorded boundaries
shift by the same delta as its text; without that, the *next* keystroke
classifies statements against stale offsets and re-parses the wrong region.
Successive-edit tests (two edits in a row, each shifting the tail) pin this.

**Two real bugs were sitting in the "obvious" fallback.** `didChange` ignored
`cache.Update`'s error and fell back to `SetText(last.Text)` — but for an
*incremental* change `last.Text` is only the edited region, so a rejected change
shrank the user's document to a fragment. The fix keeps the previous buffer and
publishes a warning telling the client to resend in full. Silent staleness and
silent data loss are both worse than a re-parse; the diagnostic is the product.

**Report the work you did.** `publishDiagnostics` now carries
`parseCache: {statements, reusedStatements, incremental}`. Reuse is otherwise
invisible: two servers, one doing 1% of the work, look identical from outside.
Every reuse test compares the incremental tree against a full parse of the same
text — "faster" is only a win if it is still the same tree, and the report is
only trustworthy if the tree is.

**Process lessons.** Two existing tests asserted the prefix-only reuse counts
(`1`, `1`) and failed the moment reuse improved; they were updated *together
with* identity assertions rather than relaxed, so the new behaviour is pinned.
Also worth noting: the roadmap listed L4.1/L4.2 as unstarted while the lexer had
already shipped both (verified by probe before marking). A plan that is not
re-verified against the code drifts in both directions — claiming features that
were never built, and hiding features that were.

## L4.1 — a failed parse is a list of diagnostics, not a Go error (ADR 0177)

**A feature is not done when its components exist.** The lexer recovered, the
parser collected a forest, tokens carried rich spans — and `gustyc check` still
printed one prose blob, `--json check` printed *no JSON at all*, and the precise
lexer message (`unexpected character "$"` at 2:5) was thrown away. The last hop
was `if err != nil { return nil, fmt.Errorf(...) }`. Roadmap items should be
verified by driving the user-visible surface, because every layer can be correct
while the feature is absent.

**`return err` is where diagnostics go to die.** A Go error is a string-shaped
channel; a parse failure is a *list* of positioned findings. Turning the list
into an error collapses it, and every consumer downstream then reinvents
parsing of a message it should never have lost. The fix routes parse failures
through the same `Diagnostic` channel as semantic ones — one shape, human and
JSON paths sharing it, exit code unchanged.

**Recovery is only worth it if you keep going.** Skipping `Analyze` when parsing
failed looked safest, but it hid every type error below the broken line: the
"fix one error, rediscover the next" loop in the compiler's own face. Recovery
yields only complete statements, so the rest of the file is still checkable. A
test asserts a `return "s"` in a function *after* a broken line still reports
`return type mismatch`.

**Duplicates are a bug users see.** The first version appended `prog.Diags` in
the parse-error converter, not knowing `Analyze` already merges them — the same
error printed twice. Deleting one side (and writing down why) beat adding a
dedup pass.

**Order is part of the product.** Parse errors come from one collector and
semantic errors from another; concatenating them by producer confused humans and
made JSON output non-diffable. Sorting by line/col/message costs nothing.

**Verify the plan before editing it.** The roadmap listed L4.1/L4.2 as not
started. A probe (`Parse` with two bad characters, `Lex` on a triple-quoted
string) showed both already shipped. A plan that is not re-verified against the
code drifts both ways: it claims features that were never built and hides
features that were.

**Guard the nil path you just made reachable.** Making `Analyze` run after a
failed parse exposed `Analyze(nil)` on the lexer-failure path (a segfault in the
first test run), and my first `checkParseErrors` called `err.Error()` on a nil
error. Whenever a call moves out of an error branch, the nil case becomes real.

## Gap N — string literals were bytes and escapes were letters (ADR 0178)

**The parity harness cannot see a front-end bug.** It compares the interpreter to
the AOT backend, and both share the lexer — so `print("a\nb")` printing `anb`,
and `"héllo"` printing `hÃ©llo` with `len` 8, passed every test ever written.
The only oracle that matters is CPython, and now the tests *execute* it:
`pythonOutput(t, src)` fails the build when a hard-coded expectation disagrees
with what Python actually prints. A claim about Python that lives in a comment is
a hypothesis; run it.

**`len` was neither bytes nor characters — that is the tell.** `len("héllo") == 8`
matches no sane model (6 bytes, 5 runes). Whenever a number matches no
representation, stop: something is being re-encoded. Here it was
`val += string(src[j])` on a `byte`: Go converts a byte to a *rune*, so every
non-ASCII byte became two bytes (`é` → `Ã©`). The same gotcha lived in
`buildFString` (`lit += string(c)`), found by looking for the pattern rather than
the symptom.

**Half-decoding is worse than not decoding.** The old rule — "drop the backslash,
keep the next character" — made every escape silently wrong in every program,
and the parser's `unescapeStr` documented it as if it were intended. One
`appendEscape` now implements Python's rules and is shared by the lexer, the
triple/raw scanners and the f-string path: one decoder means the string forms can
never disagree, which was an actual failure mode (`r"a\nb"` vs `"a\nb"`).

**Deleting the duplicate was the fix, not patching it.** The bug lived in a
second, hand-copied ordinary-string scanner next to `scanString`. Rewriting the
call site to use `scanString` removed the class of bug (two scanners drifting
apart) instead of keeping it latent.

**Fixing the front end exposed what the CLI was hiding.** `gustyc --file` runs
the *interpreter*, so every "compiled" probe this session was interpreted; the
conformance harness (real `llc` + `cc`) is what caught three AOT-only defects:
`in` on a string emitted a global into an i32 slot, `def g(): return "hi"` emitted
`ret i32 @.str1`, and `print(d["k"])` printed the interned **index** (`6`). Two
lessons: probe through the path users believe is compiled, and make the backend
explicit (recorded as Gap M.2 — the CLI must *say* which backend ran).

**A wrong answer that looks plausible is the worst kind.** `print(d["k"])` → `6`
was a valid integer from a valid-looking program. The runtime `@estr` flags cover
printing the whole container; per-position `dictValStr` covers printing one
element. Both were needed, and only the second was missing.

**Keep the divergence you cannot fix honest.** Byte-vs-code-point `len` stayed
(parity across backends is worth more than one Python detail), but
`TestStringLengthIsBytesForNow` records *both* answers — ours and Python's — so
the migration has to arrive as a failing test, not a shrug. Roadmap Gap N.2.

**Beware the raw-string trap (again).** An IR comment containing backticks around
`"cat" in greeting` terminated the Go raw string literal and broke the build —
the exact hazard already written down. Keep IR comments backtick-free, and use
`;` not `//`.

## Gap M.2 — the CLI must say which backend ran (ADR 0179)

**An undocumented default is a bug generator.** `--file` ran the interpreter while
two docs said it was the AOT path, so every manual "I compiled this" probe this
session was an interpreter run — and three genuine AOT defects (invalid IR for `in`
on a string, `ret i32 @.str`, `print(d["k"])` printing an interned index) stayed
hidden behind it, visible only to the harness that really runs `llc` + `cc`.
A default that contradicts the docs is not a small doc bug; it silently routes all
your evidence to the wrong path.

**Report the facts about the tool, not just the program.** Every execution payload
now carries `"backend"`, so an agent that asked for AOT can *see* it got AOT
instead of inferring it from the flag list it passed. Inference from inputs is how
the ambiguity was born.

**Contradictions must be errors, never precedence.** `--aot --interp` exits 4 and
names both flags. Silently resolving that (last flag wins, or "safest wins")
recreates the exact unanswerability being fixed.

**Keep the human answer off stdout.** `--show-backend` writes to stderr: a test
asserts stdout stays exactly `2\n` with and without the flag, because the moment
tool chatter enters program output, every piped program in the corpus breaks
(ADR 0169).

**When a contract changes, tighten the test rather than deleting it.**
`TestCLIJITJSON` asserted the exact JSON shape; the new field made it fail. The
assertion now includes `"backend": "aot"` — same strictness, updated contract, so
the shape stays pinned for the next change too.

**Defer the risky half explicitly.** Flipping `--file` to AOT by default would
turn "works (interpreted)" into "fails (AOT-unsupported)" for unknown programs.
Recorded as Gap M.2's open step, gated on the conformance matrix passing through
the compiled path — a migration needs its evidence before it flips a default.

## Gap P — `/` truncated and floats printed as ints (ADR 0180)

**Parity has a blind spot for shared bugs.** Both backends computed `7 / 2 = 3`
and printed `2.0` as `2`, so every parity test passed. The corpus only told the
truth once I ran it through **CPython** — 32 of 35 Python-valid programs match now.
An internal oracle proves agreement; only an external oracle proves correctness.
When a feature is "Python-like", CPython is the specification, and it should be a
test dependency, not a memory.

**A documented wrong answer is still wrong.** `docs/language.md` said "Integer
division treats `/` and `//` the same" and the docs had *internally* contradicted
themselves about float representation a few lines earlier. Docs written from
implementation notes preserve the bug and certify it; the fix had to update the
language contract, not just the code.

**Two formatters, two different lies.** `%g` dropped digits (0.123456789 →
0.123457), `%.17g` invented them (0.1 → 0.10000000000000001), and neither marked
an integral float as a float. Python's rule is "shortest that round-trips, with
`.0` when integral" — which at runtime needs a precision ladder (15→16→17)
verified by `strtod`, not a single format string.

**Decide from the value, not the text.** My first `rt_fmt_double` scanned the
formatted bytes for a `.` and rendered `3.5` as `3.5.0`. The condition — finite,
integral, small enough to render positionally — is a property of the number;
computing it (`floor(v) == v`, `|v| < 1e15`) was smaller, faster, and right.

**Match Python's thresholds, not your library's defaults.** Go's `FormatFloat(…,'g',…)`
switches to exponent notation at its own digit count, so `1e15` came out `1e+15`
where Python says `1000000000000000.0`. Reimplementing "Python's float repr" means
its *ranges* too, not just its shortest-digits rule.

**Two LLVM/Go traps, again earned.** An instruction `getelementptr` takes no
parentheses (only constant expressions do), and a runtime helper is only available
if its block is emitted — mine landed in `heapRuntimeIR` and a float-only program
got `use of undefined value '@rt_fmt_double'`. New runtime code gets its own block
plus a use-flag, and a test that runs a program using *only* that feature.

**Update expectations from the oracle, never to silence a failure.** Six tests and
two golden files changed (`4` → `4.0`, `1` → `1.0`); each was checked against
CPython first. A test updated to match output you did not verify is a test deleted.

**Pin what you cannot fix today.** Three AOT division shapes remain wrong; they
live in `TestKnownAOTDivisionGaps`, asserting Python's answer, the interpreter's
(correct) answer, *and* the compiled backend's current wrong one — so the day
someone fixes it, the test tells them to delete the allowance.

## Planning cycle — the value model is the bottleneck (Phase 11 added to `roadmap.md`)

**Why this cycle exists.** Before picking work, I re-measured the project rather than
trusting the docs: `go test -tags=llvm20 ./...` (green, 857 test functions), the
conformance matrix (41 cases, all `parity: true`), ADRs `0001`..`0180`, version
`0.10.0`. Then I ran ~30 short programs through `--interp`, `--aot` and `python3` on
the *same* source. The corpus says "everything works"; the oracle says twenty shapes
do not.

**Parity is not correctness.** The conformance harness compares the two backends to
*each other*, and CPython appears in only two test files (`escapes_test.go`,
`division_test.go`). Everything both backends agree on is invisible to CI:
`print(True)` → `1`, `xs[-1]` → `IndexError`, `len("café")` → `5`,
`-3.5 % 2.0` → `-1.5`. That is why L11.9 (oracle everywhere) leads the phase — a
green harness that cannot see a wrong answer is not a gate.

**One root, many symptoms.** The failures are not twenty bugs. An AOT value is an
untagged `i32`, so it cannot say what it is — cannot nest (`m[0][1]` refused), cannot
mix kinds (`[1, "a"]` refused), cannot tell `True` from `1`, and cannot carry a
string in a container slot without the compiler guessing. Each guess that fails
comes out as `store i32 @.set1, i32* %_sa` — an `llc` rejection the exit-code
contract then labels a *compiler bug* for a two-line program. Fix the
representation once (L11.1) and the Gaps J.2 / J.6 / L.2 / N.2 / P.1 tail goes with
it; patch the symptoms and it comes back through the next shape.

**Two findings I would not have believed without running them.** A literal
`print([1,2,3][-1])` **panics the compiler** (`irGen.value`, `codegen.go:4314`) while
the variable form raises — so the crash depends on whether the list is written inline.
And `A.__init__(self, x)` in a subclass **SIGSEGVs** under `--aot` where
`super().__init__(x)` works. Both are reachable from code a beginner writes.

**Exit-code asymmetry.** An `llc` rejection exits **2** through `--build` but **1**
through `--aot`/the JIT (`{"exit":1}` for `store i32 @.set1`). Same bug, two answers
for the same script — `TestCLIExitCodeContract` drives only the `--build` leg, so it
could never see this. (Also: `print(math.PI)` compiled prints `3`: data-only stdlib
modules fold float constants to `int`.)

**Process lesson — the committed binary rots.** My first probe run used the checked-in
`./gustyc` (built before ADR 0179/0180), which reported `--aot` as an unknown flag and
`1/2` → `0`. A rebuild (`go build -tags=llvm20`) changed a dozen answers. Any
measurement is against a *rebuilt* binary or it is a measurement of history.

**Docs drift is a fact-gathering hazard.** `docs/operations.md` still calls classes,
closures, decorators, generators and `try`/`except` "interpreter-only" (they are all
lowered), and `docs/language.md` advertises "negative indices ✅ DONE" where the truth
is *slicing only*. Both mislead the next cycle's planning, and both are now corrected
in the Phase 11 items that own them.
