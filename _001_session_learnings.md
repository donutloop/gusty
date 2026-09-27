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
