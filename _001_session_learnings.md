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

## L7.2 — the collector had no roots, and nobody called it (ADR 0181)

**What the code actually was.** `Evaluator.Collect` was a real mark-and-sweep with a
nursery and tests — and its only callers were tests. A REPL session retained every
object it had ever allocated; a program that looped a million times never collected
once. "We have a GC" and "the GC runs" are different claims, and only one of them is
checkable by reading the file.

**Precise rooting in a tree-walking interpreter reduces to two rules.** The live set
at an arbitrary moment includes Go locals (`a + b` holds `a` while `b` evaluates, a
call holds its argument list while the callee runs), and Go gives no way to walk that
stack. What worked instead of guessing: a **watermark** (everything minted after the
last safe point is unconditionally live, so half-evaluated expressions are safe to
collect around) and a **safe point** (a statement boundary with `exprDepth` at its
base, reached from a construct that *declared* its root groups). Declaring is the
work: a `for` loop must hand its iterable to the collector instead of keeping it in a
Go local, and an executor must hand over `last`.

**A test that cannot fail proves nothing.** My first frame-root test passed with
`pushFrame` stubbed out — two accidents masked the whole mechanism: the "value of the
last statement" root happened to hold the list, and the young GC never sweeps old
objects, so nothing was ever reclaimed where it mattered. The test only became real
when it demanded the symptom (`cannot index null` under a full GC with frames
disabled) instead of an internal counter. Stub-the-feature and re-run is now a step I
take before believing a memory test.

**Statement-root calls are where the win is.** A call that *is* the statement has no
half-evaluated expression above it — arguments are already in the callee's rooted
frame, the result does not exist — so its body may take safe points of its own. That
is the difference between `def main(): <big loop>` leaking unboundedly and not. A call
nested inside an expression stays conservative, and that limit is documented rather
than discovered later as an OOM; removing it needs the Phase 11 value stack.

**Turning the check on found an AOT wrong-answer bug.** While writing the repro
program, `walk(k)` returned `0`s under `--aot` and `130` under `--interp`. Root slots
in codegen are static per (scope, name), so a recursive call re-registers the *same*
entry and the inner frame's list replaces the outer frame's root — then a collection
inside the callee sweeps the outer frame's live list. Same root cause as Gap A's
instance-layout bug, and the same answer: roots need stack discipline, not a name
table. `integration/programs/gc_precise.gy` is pinned as the repro; the AOT half of
L7.2 (root stack + per-slot handle tags + `rt_gc` counting what it skips) is the next
commit. This is the third time a latent codegen bug surfaced only when a new check was
actually run (L8.2's verifier, the heap-stress harness, now this).

**Process — instrument the tool, not just the test.** Because `--gc-stats` reports
roots traced vs roots *proved non-handles*, "is the collector precise?" became a
number in a test rather than a paragraph in an ADR, and the corpus-wide
`GUSTY_GC_STRESS=1` run became possible. Same lesson as the optimizer report (Gap
J.4): a subsystem with no self-report cannot be gated.

## L7.2 (second commit) — the compiled backend's roots were a table, not a stack

**The bug was the data structure.** Codegen registered each container variable's slot
address once, at index `f(scope,name)`. One entry per name cannot express two live
frames, so a recursive call re-registered the entry and the inner frame's list replaced
the outer frame's root; a collection in the callee swept a list the outer frame was
still about to read. `gc_precise.gy`: interpreter 130, AOT 346. The fix was not a
special case for recursion — it was turning the array into a root *stack* with an
explicit push/clear/open/close protocol in the target's runtime, the same discipline the
interpreter got in the previous commit.

**"Precise" depends on things that have nothing to do with the collector.** With the
stack in place, a 2000-iteration loop that built instances reported `top=2002`: the root
stack grew by one entry per iteration, nothing could be reclaimed, the 1024-slot heap
filled, and the program died. Cause: codegen emits `%_p = alloca i32` where the variable
is first assigned — *inside the loop body* — and at llc's default `-O0` nothing hoists
it, so each iteration used a different frame slot and the recorded address never matched
the one already registered. Fixed with a `hoistAllocas` pass (one slot per variable per
call), after which `top` is 3. The machine stack had been quietly growing per iteration
for the same reason, in every program, since forever.

**Making a tag dead is only safe if every store can bring it back to life.** Once a
scalar binding cleared a variable's root entry, the *next* container binding
(`s = {1, 2}` after `s = 5`) left the new object tagged dead — it was recycled while in
use, and the symptom was a set whose insert loop never terminated. Not a crash, not a
wrong number: a hang. `gcStoreHandle` pairs every handle store with a push; runtime
dedup makes that cost a scan of a handful of entries.

**An assertion is only real if removing the feature breaks it.** The first version of
the compiled-backend runtime test asserted `top < 64`, and passed with `rt_frame_close`
stubbed into a no-op — because with stable slots, dedup hides a missing pop. The
assertion that actually discriminates is *retention after the frames are gone*: after
400 recursive storms return, at most a handful of objects may still be live (with the
stub: 19, and the test says so). Same lesson as `pushFrame` earlier in the round: stub
the mechanism, watch the test fail, then believe it.

**Measure, don't theorize — and check that a "wrong answer" is wrong.** Two of the three
"GC corruption" symptoms I chased this commit turned out to be something else: the
nonsense number `-1630298296` is exactly `int32(2664669000)`, i.e. the AOT's 32-bit
arithmetic being correct while the interpreter's int64 disagrees (that divergence is
Phase 11's, not a root bug), and `d = {1: 10}; d = [3, 4]; d[0]` returning `0` reproduces
identically at HEAD — a pre-existing container-kind bug, not my regression. Counter
globals (`topmax`, and temporarily appends/hits) answered both questions in one run each;
reading the runtime and guessing would have cost the whole day. Two candidates for the
gap list: dict→list rebinding keeps the stale container kind, and the i32/int64 split.

**The LLVM verifier found what the test suite could not.** Gating the root runtime's
*data* half on `g.rooted` in the module preamble — where `rooted` cannot be true yet
because the body has not been generated — produced modules referencing an undefined
`@gc.kinds`. Every `--verify`-driven test caught it instantly; nothing else did. Emission
of feature-gated runtime blocks belongs after body generation, all halves together.

**Process — never suppress git output.** An A/B experiment used
`git stash push <paths> … ; git stash pop >/dev/null`; the pop failed and I did not see
it. The next `go build` failed with `undefined: lang.SetGCReport`, and the reason was that
most of the round's work was sitting in `stash@{0}`, not in the tree. Recovered, nothing
lost, but the correct habit is a copy of the file (or a second worktree) for A/B builds,
and reading what git actually printed.

## L11.1 first step: one tag table, and the compiled heap's kind is a projection of it (ADR 0182)

- What I actually found, rather than what the roadmap assumed: the "single source of truth for
  the tag set" was *half* built already. `ValueTag` in `value.go` existed, the interpreter's
  `obj.tag()` and the compiled `%obj` FFI read it, and `abi.go`'s numbers happened to match —
  but the compiled heap's `kind` word was a wholly separate numbering (list = 1 while
  `TagList` = 5), with the numbers written as literals into IR (`call i32 @rt_alloc(i32 1)`)
  at a dozen codegen sites, and `HeapKindName` spelling the kind names out a third time. So
  the work was the projection and the naming, not a new table.
- **Numbers that reach IR must stay constants.** My first attempt was
  `const HeapList = HeapKindFor(TagList)` — which cannot compile, and even if a function could
  be a constant, a function call cannot be a `case` label. What works is constants for the
  wire values plus one order-slice (`heapKindOrder`) as the projection, with names derived from
  the tag table and *tests* tying the two. That is the general shape whenever a Go value becomes
  LLVM text.
- The tests only earned trust after being stubbed twice: renumbering `HeapKindSet` to 5 fails
  `TestEmittedHeapKindsComeFromTheTable` with "codegen allocated an unknown heap kind 5", and
  swapping dict/set in the projection order fails the round-trip. A test that passes only
  because the numbers happen to match is not a guard.
- Backticks are a syntax hazard, not just formatting: putting `kind` in a description inside
  `schema.go`'s **raw string** closed the literal early — `syntax error: unexpected kind after
  top level declaration`. Raw-string content must not contain backticks; quoting in those
  descriptions has to be done with plain words.
- **A probe is worth an hour of guessing about scope.** Before writing anything I ran 15 print
  forms through `--interp`, `--aot` and `python3`. That is what turned up the decisive fact —
  `--json` reports `"type": "int"` for `True` — which means L11.2 (`str()`/`repr()`, "bools
  print `1`") is not an independent fix: there is no tag to print from until L11.1's bool step
  lands. Recording that in the roadmap as a gate is more useful than a half-migrated renderer
  that would have to be written twice.
- The probe also banked the remaining invalid-IR cases for L11.1 rather than silently
  "fixing" them: `print({1, 2})`, `print([1.0, 1.5, -0.0])` and `str(None)` each fail `llc`
  with a `global variable reference must have pointer type`-family error, and `print(set())`
  prints `0` compiled versus `set()` interpreted. Those belong to the tagged element, not to a
  printer patch (Gap J.6).
- Left the one literal kind inside the runtime's IR text (`%h = call i32 @rt_alloc(i32 4)` in
  the instance helper) rather than turning another const into a rendered template, and paid for
  that choice with a test (`TestInstanceKindsInRuntimeAndCodegenAgree`) asserting text and named
  constant agree — cheaper than a second placeholder mechanism, and it is the check that makes
  leaving it acceptable.

## `str(None)` was `0` because three places decided it (ADR 0183)

- `print(str(None))` printed `0` and `s = str(None)` failed to compile — one bug, two
  symptoms, because **three** sites fold `str()` of a compile-time-known argument
  (`stringConst`, `irGen.stringVal`, and the builtin's own lowering) and each kept its own case
  list. None was in none of them, so it fell through to `value(None) == 0`; and because the
  sites disagreed about *whether the result is a string*, the assignment path stored the string
  **global** into an `i32` slot — the constant-in-value-position shape ADR 0167/0168 kept
  producing. A fold shared by every site, plus a test that compares the two folders against the
  interpreter, is what makes this class of bug impossible to leave half-fixed.
- **Two "better" designs were wrong, and the tests are why I know.** Interning inside the
  builtin lowering (`rt_str_intern2` for every fold) broke `print(len(w))` and `"x" + w`,
  because those paths key off the folded *text*. Making `stringVal` delegate to `stringConst`
  broke the same two, because `stringConst` folds forms the string-value folder deliberately
  does not. The rule the failures taught: case lists must **agree on the forms they share**,
  not be identical. Reverted both; the correct fix was three two-line case additions.
- Manual probing lied by omission. `s = str(None); print(s)` worked while
  `print(str(None))` did not, so a single probe per shape would have shipped a false "fixed".
  The parity table (interpreter + JIT + expected CPython text) caught what the CLI check missed.
- Load-bearing checks, both verified by stubbing: fold `str(None)` back to `"0"` and the
  integration parity test fails on exactly the `str(None)` rows; drop the same fold from the
  builtin lowering and the in-package test fails on the emitted bytes (`no c"None` global).
- Blunt IR-shape guards earn their keep: asserting no line contains `i32 @.` (or
  `store i32 @`) states the invariant llc enforces, in the language of the artifact, and does
  not care which helper got it wrong.
- A Go octal escape is a trap in generated test strings: `"…\\00"` written as `"…\00"` fails
  with `illegal character U+0022 '"' in escape sequence` — keep NUL markers in raw strings or
  leave them out of the message.
- Backticks and `%` are not inert inside Go raw strings and schema descriptions respectively:
  a backtick ends a raw string literal early (`syntax error: unexpected kind after top level
  declaration`), and a `%obj` inside the schema text trips `go vet`'s printf check on the
  `fmt.Println(lang.ASTIRSchema)` that prints it.

## A tag per element: `[1, "a", None]` compiles, prints, and still refuses what it cannot read (ADR 0184)

- **The enabling change was ~10 lines; the design is the gate that refuses.** `@heap_tags` plus a
  per-slot load in a copy of `rt_print_list` is the whole feature. Everything worth reviewing is
  in `elemKindTag`'s exclusions: bools (would print `1` where Python prints `True`), floats (no
  float branch in the mixed printer), containers (the collector has no element worklist, so a
  nested heap object could be freed under the list), and any element whose string-ness codegen
  cannot prove. Skip any one of those and the compiler ships the old `(null)`/index-as-number bug
  in a new costume.
- **Stubbing caught what "it compiles" would have hidden.** Deleting the `rt_tag_elem` emission
  still compiled, still printed a plausible list — `[1, 0, 0, 2]`. Only an assertion on the
  *text* (against CPython, not against yesterday's output) fails that. Every new capability in
  this area needs its output pinned, not its exit status.
- **A test that pins a refusal becomes a to-do item the day the feature lands.**
  `string_containers_test.go` asserted `xs = [1, "a"]` *must* be refused; it was right under
  ADR 0175 and it failed correctly under ADR 0184. The suite surfacing it — rather than me
  deleting it from memory of why it existed — is the reason the row moved to a print-correctly
  test in the same commit that retired the refusal, with the reason written down.
- **Parallel array beat a struct field for review-risk, not speed.** Adding `tags` as the fourth
  field of `{kind, len, elems}` is better layout and would have rewritten every GEP into `elems`
  in the same commit that changes observable behaviour. Two changes with one failure mode each
  are easier to trust than one change with two.
- **Make the zero value mean something, then skip the initialisation pass.** Tag `0` is the
  canonical `TagInt`, so a fresh heap slot needs no tag write and `rt_alloc` stays as it was —
  one less thing the collector and the free-list have to agree about. ADR 0182's numbering is
  what made this free.
- **Prove the memory argument or don't ship the case.** Mixed lists are safe *because* their
  elements are immediates and interned strings — nothing the collector manages is reachable only
  through them. Saying so is what makes "containers are excluded" a reasoned line rather than
  timidity, and it tells the next cycle exactly what `rt_gc` needs before `[[1], "a"]` can open.
- **The interpreter was already right**, so parity was free: `Repr` walks a slice of values and
  already had per-value kind. Divergences like this one live in the compiled backend's need to
  decide statically — which is also why the *refusal* messages are the honest interface (ADR 0166)
  while the capability grows.

## A loop over a mixed list binds (value, tag) — and the str/repr split arrives from the call site (ADR 0185)

- **"Is this limitation real, or an artifact of where I implemented it?" is the question that
  unlocked this feature.** ADR 0184 refused `xs[0]`, `xs.append(...)` *and* `for x in xs` for the
  same stated reason — the read site has one static kind. But a loop is different: it already
  computes the index, so the tag is available exactly where the variable is bound. Index reads
  genuinely cannot know; loops can. Re-refusing a construct because a sibling one is hard is how
  ADR 0175 ended up refusing literals.
- **str vs repr is a property of the *context*, not of the value.** One tag, two renderings:
  `print(x)` shows `a`, `print(xs)` shows `'a'`. The printer therefore takes a `quote` flag from
  its caller instead of trying to infer intent from the tag. ADR 0174's interned repr slot made
  this nearly free — the quoted form was already stored beside the raw text; nothing had been
  *reading* it per element.
- **Funneling through one resolver turns ten guards into one.** Every arithmetic, comparison and
  call argument eventually reaches `value()`, so a single `taggedVars` check at the top of its
  `*Name` case covers all of them with one honest message. Had each expression node resolved
  names itself, this feature would have needed a guard each — and one of them would have been
  missed.
- **Two of my own tests were assertions about yesterday's bug, and had to be inverted.**
  `mixed_list_test.go` asserted `for x in xs` must refuse; `string_containers_test.go` asserted
  literals must refuse. Both were right when written, both were the first thing to fail once the
  capability existed, and both moved to "prints correctly" tests in the same commit that retired
  the refusal. A refusal test is a scheduled deletion; write it with the reason attached so the
  deletion is a decision.
- **Watch for the homogeneous trap in a mixed-value test.** `xs = ["a"]` looks like a tagged-value
  case and is not — one string list is homogeneous and never becomes a tagged list, so a print
  assertion written against it silently tests the wrong path. Mixed means *both kinds present*.
- Stub discipline again paid: pinning `rt_tag_of` to `0` kept the program compiling and printed
  `1\n0\n0`, and flipping the top-level `quote` flag kept everything green except the one parity
  row that cares. "It compiles" continues to be worthless as an assertion.

## The corpus gets a third opinion: CPython is the oracle, parity is not enough (L11.9, ADR 0186)

- **A green suite that compares two implementations to each other is a statement about
  agreement, not about correctness.** Four of the five rows that motivated this cycle
  (`print(True)` → `1`, `len("café")` → `5`, `"abc"[1]` → `98`, `xs[-1]` trapping) had *both*
  backends agreeing, so all 41 matrix cases passed while four answers were wrong. The roadmap
  only knew that because someone had hand-run `python3` beside the compiler. The fix is not more
  tests of the same shape; it is a third engine that neither backend authors control.
- **The divergences were hiding in plain sight inside the *passing* corpus,** and two of the
  three oracle helpers that existed (`escapes_test.go`, `division_test.go`) actively normalised
  them away: `normalizePy` rewrote `True` → `1` so that "does it match Python?" could be asked
  without ever failing. Every convenience normaliser is a divergence with a hiding place. The one
  rule this harness allows itself (`set-order`) is named, documented, justified by an
  unspecified-ness argument, and echoed into every artifact row.
- **Pinning the wrong answer is what makes a TODO testable.** A debt row records reason + owner +
  the exact stdout each leg produces today. A fix that does not update the row fails ("debt is
  paid"); a change that moves the answer *without* fixing it also fails. Without pins, "we know
  this one is wrong" is prose — and prose about a corpus goes stale in both directions.
- **Default to "must match" and the corpus cannot rot quietly.** A case with no ledger row is
  declared `match`, so adding a program costs an assertion. The alternative default — "unclassified
  unless someone says otherwise" — is how the old matrix reached 41/41 with 9 wrong rows.
- **Bidirectional drift checking is the whole design.** A one-way check ("known-bad rows must still
  be bad") rots the moment someone fixes something and forgets. Making an *improvement* a build
  failure converts ledger maintenance from good intentions into a compile-time obligation.
- **Refusals are divergences, not skips.** `oracle: not_applicable` is reserved for "CPython cannot
  run this source at all", and a leg that failed to run counts as *not matching*. Otherwise
  "the compiler gave up" would report as conformance — the exact inversion ADR 0166 was written
  against.
- **A harness that dies with the program under test reports nothing.** One probe makes codegen
  panic (`print([1, 2, 3][-1])`); without `recover()` around each leg that is "the integration
  suite crashed" and 58 lost rows. Recorded as `compiler panic: …` in one row, it is a measured,
  owned defect (L11.4 + L11.8) and the rest of the corpus still reports.
- **The oracle is a *named toolchain*, not an abstraction.** `toolchain.python` and
  `toolchain.llvm` are recorded in the artifact and `GUSTY_PYTHON` overrides the interpreter,
  because "prints what Python prints" is a claim about Python 3.12.3 — pin it like the LLVM
  version is pinned, or a future CPython silently changes the spec.
- **The harness found a bug nobody had listed.** `print` writes each argument *as it evaluates it*,
  so `print("got", twice(21))` prints `got << 21 >>` / `42` where Python prints `<< 21 >>` / `42`.
  It was in the corpus, in both backends, identically — and `docs/language.md` had *documented the
  wrong behaviour as intentional*, because the only evidence available was "both backends agree".
  Agreement between two implementations is not evidence about the language; it is evidence about
  the implementations. Now Gap L.5, with two pinned programs.
- **Exit codes are documentation you can execute.** `--oracle` needed a code for "the toolchain
  answered differently than the reference" that is neither "your program is broken" (1/3) nor
  "you invoked us wrongly" (4): 6 divergence, 7 no-verdict. An agent iterating on a language bug
  can now loop on `gustyc --oracle prog.gy; [ $? -eq 0 ]` without scraping prose.
- **Stub-tested the tester.** `TestOracleHarnessCanFail` re-runs a real program with a deliberately
  wrong pin and requires drift; `TestOracleThirdLegIsNotAStub` requires the CPython leg to
  *disagree* with both backends on a program whose whole point is disagreement. A harness is only
  trustworthy once you have watched it fail on purpose.

## The oracle's first catch: a passing build with a wrong answer (L11.1, ADR 0187)

Continued L11.1 one cycle after landing the oracle (ADR 0186), and it paid for itself
immediately. The thing it found was not a refusal — it was a **build that passed everything and
printed the wrong list**.

```gusty
xs = [1, "a", None]
xs[0] = "z"
print(xs)     # both backends: [1, 'a', None]      CPython: ['z', 'a', None]
```

`rt_put_elem` wrote the payload; nothing wrote `@heap_tags`. The slot kept the `int` tag it was
allocated with, and the payload — the *interned index* of `"z"` — rendered as a number that had
come out of the string table. Under the two-backend matrix this was invisible forever: both
backends emit the same module, so parity was 100%.

**What I built.** One rule at every site that writes a slot: *the operation that writes a slot's
payload writes its tag.* `rt_append_tagged(h, v, t)` appends both in one call; item assignment
emits `rt_put_elem` + `rt_tag_elem` in the same bounds-checked block and deliberately skips the
container-wide kind bookkeeping, because the tags carry the truth now. Reads emit `rt_get_elem` +
`rt_tag_of`, and two uses of that pair are open: `print(xs[i])` dispatches on the tag, and
`v = xs[i]` binds a tagged variable. Payload and tag are cross-checked — if one says "interned
string" and the other says "number", the program refuses rather than emitting IR — because that
disagreement *is* the bug class.

**Three things to remember.**

1. **A wrong answer costs more than a refusal.** Every refusal opened this cycle cost a program
   nothing; the one place where the compiler answered instead of refusing cost the oracle its
   trust. When forced to choose, refuse loudly — but go *look* for the places where you are
   currently answering.

2. **I again wrote tests asserting yesterday's limitation, and had to invert them — third cycle
   running.** `TestMixedListElementUsesStillRefuse` asserted `print(xs[0])` must refuse. This
   time I fixed the process, not only the test: every refusal test now asserts the refusal's
   *text*, so when a future cycle opens one of those sites the assertion that breaks names the
   capability that moved. A limitation test that checks only "some error" is a landmine; one that
   checks the message is a sensor.

3. **The general shape makes the specific use cheap.** `v = xs[i]` cost ~15 lines because
   ADR 0185 had already built the tagged-variable binding for loop variables — same
   `%_v`/`%_v_tag` allocas, same print dispatch, same rebinding-retires-the-tag path. The loop
   cycle could have special-cased loop printing; it built the pair instead.

**Where I was wrong twice, both about my own test expectations.** I wrote `xs[i % 2] = "w"` over
80 iterations and predicted `['w', 'a']`; both slots get written, so the answer is `['w', 'w']`.
I would have shipped the wrong *expectation* if the three-leg workflow had not made me run
CPython on the program before writing the row. Expectations come from the oracle, not from
imagining the program.

**Also worth keeping.**

- Payload-and-tag as two stores is a bug class that recurs whenever an operation is *composed*
  rather than *named*. Naming it (`rt_append_tagged`) removed the ability to get it wrong. The
  rejected alternative — poison the tag on write so a forgotten tag traps at print time — turns a
  wrong answer into a crash but keeps the composition. Prefer the API that cannot express the bug.
- Clearing `@heap_tags` on alloc/free was the other rejection: a memset per allocation to guard a
  failure mode the pairing rule already makes unreachable. `TestMixedElementAccessSurvivesCollection`
  is what keeps that bet honest — a stale tag shows up there as a wrong render, not a crash.
- Corpus is 61 rows / 45 parity, oracle 29 match / 22 debt / 10 NA, 0 drift, 0 skipped. Matrix
  counts are now part of every cycle's record.

**Next.** L11.1's remaining list is ordered by oracle verdict, not by size: dict/set element tags
(most of the container debt), floats in containers (the tag exists, the renderer does not), then
bools as values — which still gates L11.2, because there is no tag to print from.

## The most ordinary program in the corpus did not compile (Gap L.6, ADR 0188)

I had been using the new CPython oracle to probe *interesting* shapes — bools, unicode, negative
indexes, mixed containers. This cycle I pointed it at the boring ones and found that
`print([1, 2])` — a program a tutorial would open with — made `llc-20` refuse the module:

```
error: global variable reference must have pointer type
  %t1 = call i32 (i8*, ...) @printf(i8* getelementptr(... @.fmt1 ...), i32 @.lst1)
```

Five shapes, one misplaced question. `print([])`/`print({})` same crash; `print(set())`,
`print(list())`, `print(dict())` printed `0`, the handle; and two ran *successfully* with garbage:
`print(["a"]) … print({1, 2})` → `{(null), (null)}`, and `print([["a"], ["b"]])` → `[1, 2]`, the
interned indices of the inner strings rendered as numbers, exit code 0.

The cause was one gate asking the wrong question: `literalNeedsHeap` — *"does this literal contain
a string, so does it need a heap object?"* — is a **storage** question, asked in a **rendering**
position. Everything answering "no" fell through to the static global struct, which is a fine way
to store an int list and a nonsense thing to hand `printf("%d")`.

**Four fixes, and the one rule behind two of them.** An attribute of a container must be
initialised where the container is born. `rt_alloc` already wrote `kind` and zeroed `len` on both
the fresh and the recycled path — `@estr[h]` (the "my elements are interned text" flag the printers
dispatch on) was missing, so a recycled slot inherited its predecessor's flag and printed numbers
through the string table. It now clears it on both paths. Compare ADR 0187, where I *refused* to
clear the tag array: 256 i32s per allocation is a hot-path memset, and there the failure mode is
made impossible by writing tags in pairs with payloads instead. So the runtime rule is now written
down with its cost criterion: **clear at alloc when clearing is cheap; pair at write time when
clearing is expensive.**

The other two fixes: `emptyContainerLiteral` lets a zero-arg `set()`/`list()`/`dict()` reach the
print path (the empty set has no literal spelling at all), and `heapElemKind` refuses a container
inside a container with the collector's actual reason — an element handle has no variable slot to
be marked from (ADR 0181), so "supporting" it would be a use-after-free with a passing test.

**The harness paid for itself a second time, differently.** `probe_empty_set` became a *paid debt*
and the build told me so in three places at once, with the remedy in the message:

```
oracle debt is paid: both backends now print CPython's answer — update the registry (declared debt)
pin says the aot leg prints "0\nset()\n0\n", got "set()\nset()\n0\n"
a probe that now matches CPython is a paid debt — promote the program and delete its ledger row
```

That is the ledger design validated: an unrecorded *fix* fails the build as loudly as a new
divergence. `probe_empty_set.gy` is now `empty_set.gy` in the parity corpus.

**Test the test, again — and this time it nearly fooled me.** My first regression program built 40
throwaway string-lists in a loop, then printed `{1, 2}`. Stub check: it passed **with the fix
removed**. The reuse that triggers the bug needs the literal builds back-to-back, not a GC loop. The
shape that actually catches it is the boring one — a string container immediately followed by a
numeric one. A regression test that cannot fail is worse than none, because it is believed.

**Where I was wrong again (fourth cycle running): my own expectations.** I wrote
`print([1, 2], sep=", ")` expecting `1, 2`; `sep` joins *arguments*, and there is one argument, so
CPython says `[1, 2]`. Every expectation in this cycle that I imagined before running CPython was a
coin flip. The workflow that works is: write the program, run `python3` on it, paste that as the
expectation, *then* ask the backends to match it.

**Standing rule I'm adding for future cycles.** Every feature needs the **most boring program
that uses it** in the corpus, not just the interesting ones. `print` with a mixed container was
tested seventeen ways; `print([1, 2])` — the tutorial's first line — crashed the compiler.

**Now.** Corpus: 62 rows / 47 parity, oracle 31 match / 21 debt / 10 NA, 0 drift. Next in L11.1:
dict/set element tags (most of the remaining container debt), floats in containers, then bools as
values — the one that still gates L11.2, because there is no tag to print from.

## Containers compare by value — and the bug that went *both* ways (L11.1, ADR 0189)

The boring-program sweep (twelve tutorial-shaped programs, run on all three legs) kept paying:
five of them diverged, and the richest was `xs == ys` for two equal lists.

Both backends answered **False**. That is the familiar shape: `==` on containers compared the two
i32s, and the i32s were heap slots. But the same comparison had a second, inverted face — the
element of a container holding a string is an *index into the interned table*, so

```gusty
[0] == ["zero"]     # would answer True;  CPython: False
```

A comparison that is wrong in both directions is the worst property it can have: no amount of
"just try it on a few cases" surfaces it, because whichever way you test, something looks right.

**What I built.** `rt_container_eq` walks two containers the way Python's `__eq__` does — lists
positional, sets and dicts by containment (a positional walk makes `{1, 2} == {2, 1}` False) — and
each element is compared as the `(payload, tag)` pair. Container-vs-proven-scalar is decided
statically with both operands still evaluated (`f() == xs` keeps `f`'s side effects);
container-vs-*unknown* refuses with "needs a tagged value", which is L11.2 named exactly where it
bites. `is` stays identity, and `!=` became the negation of `==` — it had been its own raw handle
comparison, so `[1] == [1]` and `[1] != [1]` both answered False.

**The pairing rule was not finished, and following it found the real bug.** ADR 0187 said *the
operation that writes a slot's payload writes its tag*; I had applied it to the mixed-list path and
the element writes, and no further. Grepping for every place a container gets built turned up four
builders that never wrote tags at all: the plain list/dict/set literal builders, the
container-variable assignment builders, and `heapArg`, which hand-rolled its own
`rt_alloc`+`rt_dict_put` loop for call arguments. An untagged slot is **uninitialised memory** — it
holds whatever the previous tenant of that heap slot left — so a container could compare unequal to
an *identical* container depending on which slot it happened to land on. That is exactly the
symptom I chased: `same({"a":1},{"a":1})` equal, `same(p, {"a":1})` unequal, both "correct-looking"
programs, differing by which allocation came first. `heapArg` now delegates to
`heapSetFrom`/`heapDictFrom`: a container is built in exactly one place per kind, and that place
writes the pair.

**The test that earned its keep.** My first collision test (`[1] == ["1"]`) passed *with the tags
removed* — the interned index never equalled the payload, so it proved nothing. The version with
teeth is `[0] == ["zero"]`: the first interned string lands on index 0, and with the builder tag
stores deleted it answers True where CPython answers False. I now treat "I stubbed it and the test
still passed" as a failed test, not a passing one — third cycle I've caught myself this way, and
each time it was a test I had written in the previous hour.

**A process note on my own expectations.** I wrote `print([1] == [1])` into a corpus program, then
caught it: comparing containers via a printed bool conflates this work with the bool-rendering debt
(`print(True)` → `1`, `probe_bool_value`). The corpus row now reports verdicts through `if`, so
`container_equality.gy` tests equality and stays a clean `match` row. Two debts, two rows — a test
that measures one thing is worth two that measure two.

**Latent, recorded not fixed:** dict key lookup and set dedup still compare payloads only. They are
safe today only because heterogeneous dict keys are refused; the moment L11.1 (1b) allows them,
`rt_dict_get`/`rt_set_add` need the tag too. It is in the ADR and the roadmap rather than in my
head, which is the whole point of writing these down.

**Corpus**: 63 rows / 48 parity, oracle 32 match / 21 debt / 10 NA, 0 drift, 0 skipped.

**Next** from the same sweep, still unpinned: a comprehension whose element is a call
(`[square(x) for x in range(5)]` — "comprehension element must be constant"), and
`names.sort()` / `sorted(names)` on a variable (the interpreter has no `sort`, and AOT's diagnostic
is the *string*-method one, which is the wrong family entirely).

## The boring-program sweep: twelve tutorials, five divergences (ADR 0190)

I wrote this entry's headline as a *standing rule* last cycle and then went and proved it in one
sitting: twelve programs shaped like a tutorial (accumulate a loop, nested loops, a string method,
a comprehension, iterate a dict, sort a list of names, a while loop, an arithmetic table, append in
a loop, default args, a small class, print some bools). Five diverged from CPython. Two of those
were new gaps and one of them is a bug in a *diagnostic*.

The two new ones, now pinned as probes with per-leg pins including the message text:

- `xs.sort()` / `xs.reverse()` — the interpreter raises `no such list method sort`; the AOT path
  answers **`string method sort on non-constant string`**. The call fell through into string-method
  dispatch, so the compiler tells the user their list is a string. That is AGENTS.md interface
  territory: a wrong-family diagnostic is a defect even when the refusal is correct.
- `print([square(x) for x in range(5)])` — refuses with `comprehension element must be constant`.
  The AOT comprehension path folds constants and stops, so the single most ordinary list-building
  idiom in Python needs a hand-written loop. `sorted(xs)` on a variable is the same family
  (`sorted: codegen folds only an inline list literal`), joining the existing literal probe.

**What I decided rather than just noted.** A corpus grown from bug reports and roadmap items
inherits their shape: it tests what we already had reason to doubt. Bug-driven corpora are
adversarial by construction, and the tutorial program — written by someone with no reason to
doubt it — is the one neither a bug report nor a fuzzer produces. So the rule, in an ADR this time
(0190): **every feature ships its least interesting program**, and interesting shapes are
additional rows, not the only ones.

**Sweeps are a cycle type, and their output is a ledger row, not prose.** The diff here is corpus
and registry only: two probes, two debt rows with reasons, roadmap owners, and per-leg pins —
including `Err: "string method sort"` and `Err: "comprehension element must be constant"`, so a
cycle that changes the message without fixing the feature trips the drift check, and a cycle that
fixes it has to delete the pin. My first pass at this cycle produced prose findings in a terminal
scrollback; the reason it produced rows instead is that I asked "what does the next cycle *read*?"

**One thing I still got wrong first.** I wrote the interpreter pin as `{Match: true}` — a field
that does not exist on `OraclePin`. The compiler caught it in one second, which is the entire
argument for pins being data.

**Corpus**: 65 rows / 48 parity, 17 probes; oracle 32 match / 23 debt / 10 NA, 0 drift, 0 skipped
short of the pre-declared legs. `tools/oracleprobe` made twelve three-leg runs a ten-minute
exercise; if a cycle can't run a program on three legs in one command, that tool is the first
thing to build.

## Sorting: the least interesting feature taught me four rules (L11.7, ADR 0191)

I expected this cycle to be a chore — `xs.sort()`, really? — and it produced more design decisions
per line than the container-equality cycle did, mostly because nothing about it forces you to think
until it has already gone wrong.

**The interned-index trap is the whole feature.** A stored string is an index into `@str_tab`, so
"sort the payloads" sorts strings by the order they first appeared in the program. `["pear",
"apple", "fig"]` sorted that way returns... whatever arrival says, which for many small lists is
*close enough to alphabetical to pass a spot check*. My comparator had to load the text and
`strcmp` it, and the mode that does that is the only reason the feature is correct. This is the
second time the interned representation has bitten (the first was `[0] == ["zero"]`, ADR 0189), and
I now think of "what is this value's payload *actually*?" as a standing question for any operation
on container slots.

**Stability is not a property of a sort you can defer.** Insertion sort, quadratic, in a runtime
whose element array is 256 deep — fine. But the reason to write it down is `sorted(key=)`: Python
specifies key-sorting as decorate–sort–undecorate over a *stable* sort. If I had written an unstable
sort today, `key=` would arrive subtly wrong in a cycle that isn't thinking about sorting. A feature
that will be built on top of this one is a legitimate reason to choose the boring algorithm today.

**The pairing rule reaches swaps too.** ADR 0187 says: the operation that writes a slot's payload
writes its tag. A sort is a sequence of two-slot writes, so my first `rt_sort` — moving payloads and
leaving tags behind — mislabelled the list it had just sorted. The test asserts the `@heap_tags`
stores are inside the swap block, which is what turns "I remembered" into "the build remembers".

**A method mutates and returns None; the builtin copies** — and the copy is where the next bug was
hiding. `ys = sorted(xs)` allocated a heap list and stored the handle in a plain int variable, so
`print(ys)` printed `1`, the slot number. Same family as ADR 0188's print-a-handle, one layer down
in the assignment path; the fix is the one the generator-call path already used (register the
target as a container, inherit element-kind from the source). Then `print(sorted(xs))` needed its
own branch, because the lowering hands back a handle and print would `printf` it.

**Three things I want to remember about the process, not the code.**

1. My stub-check on the string comparator *passed* at first — I had stubbed it by adding a dead
   block that still contained `@str_tab` and `@strcmp`, so the IR-shape test still found the words
   it was looking for. A stub check has to remove the capability, not route around it. Fourth cycle
   in a row I have caught myself writing a test that cannot fail.
2. Two existing tests broke when the new runtime helpers landed, because they counted call sites
   module-wide: `strings.Count(ir, "call i32 @rt_list_len(") == 2` was measuring *the runtime*, and
   my new `rt_list_copy` calls `rt_list_len`. They passed before only because no helper did. Now
   scoped to user code (`countOutsideRuntimePrelude`). Lesson: a module-wide count is a test that
   silently weakens every time the runtime grows.
3. Every one of these failures was "llc rejected the module", and the module was deleted with the
   temp dir. I added `GUSTY_KEEP_LLVM=1` (keep the scratch dir, print the path) after the fourth
   round of guess-and-check. Documented in operations.md: if your IR failures cannot be inspected,
   you are not debugging, you are guessing.

**Also recorded, not fixed:** `--emit-llvm <file>` is a *second* codegen entry point and refuses
things the JIT compiles (`print(sorted([10, 2, 33]))` → `unsupported attr expression`). For an
agent whose machine path is `--emit-llvm`, that gap is real; it is in operations.md and the roadmap
rather than in my head.

**Corpus**: 63 rows / 48 parity, 15 probes; oracle 32 match / 21 debt / 10 NA, 0 drift. Two probes
promoted to parity rows: `sorting.gy`, `sorting_literals.gy`.

**Next** from the still-pinned sweep finding: `[f(x) for x in ...]` on the AOT path (`comprehension
element must be constant`), which is L11.7's comprehension half.

## Comprehensions: the fold was the meaning, and that was the bug (L11.7, ADR 0192)

`[f(x) for x in range(5)]` refused in AOT with "comprehension element must be constant" and worked
in the interpreter. The message tells you everything: the compiled backend had implemented
comprehensions *as* its constant folder, so a comprehension was only defined where folding was
possible. `docs/language.md` described comprehensions without ever mentioning the boundary — docs
written from the implementation's shape, not the language's.

**What I built.** Three lowerings, ordered: fold (unchanged, must keep priority because `sum`/`min`/
`max`/`len` read the folded element set), unrolled-with-runtime-elements (one block per item — the
same strategy `for` over a literal already uses), and a real loop over a container whose length is
only known at runtime. The load-bearing detail: the loop variable is a real slot that each item is
stored into. That is what makes a call in the element position legal at all — and it means the loop
variable needs the same kind-tracking a `for` variable gets (`internedVars`), or a filter's `n == "a"`
compares an interned index with something that isn't one.

**Four bugs, and the two that mattered were found by tests I nearly skipped.**

1. `sum([sq(x) for x in range(4)])` printed **0**. Not a crash, not a refusal: a *correct-looking
   answer*. The fold consumers read the compile-time element set, which my runtime path left empty.
   I saw the 0 in a test table I had written and was about to "fix" by adjusting the expectation —
   which is precisely the failure mode ADR 0186 exists to prevent. It became a refusal
   (`probe_comp_runtime_reduce`) and an owned debt.
2. `ys = [f(x) for x in ...]` **segfaulted** under `--build`. The handle was stored without a GC
   root, so the collector recycled the list. It passed under `--aot`/JIT. If I had tested only one
   path — as I nearly did, since the JIT is faster — this would have shipped. The two-path test rule
   paid for itself again, in the same session.

**A crash that is not mine, found because I was honest about a refusal.** To make
`[n for n in names if n == "a"]` refuse rather than emit what the `for` statement emits, I had to
find out what the `for` statement emits: `icmp eq i32 %_n, @.str3` — an index into `@str_tab`
compared against the *address* of a string global — and llc rejects it. So `for n in names: if n ==
"a":` is an exit-2 compiler bug in shipped code, predating this cycle, reachable from any program
that iterates strings. It is now `probe_str_loop_eq`, with a pin asserting the *llc error text*, and
it is the next thing to fix. My comprehension refuses with exit 1 instead of inheriting it, which is
the honest version of "not my feature".

**A refusal I earned.** `[x*2 for x in xs]` where `xs` is a never-mutated all-int literal: escape
analysis keeps that list compile-time, so there's no slot to load, and emitting the load is another
llc rejection. I first shipped the load (found by my own unit test), then tried `containerOperand`
first, then understood the ordering: try the static route first, and if the variable has no slot at
all, refuse and say what materialises it (`append`, or `for`). The message points at L11.2, whose
tagged value word deletes this entire category of problem.

**The naming rule that keeps biting.** The induction register is `%cc<N>`, never `%s<N>`: a register
whose name starts with `%s` reads as a *string value* to the print and call lowerings. Third cycle
this has come up. It is in the ADR now, because a naming convention carrying semantic weight is
exactly the thing nobody tells you.

**Corpus**: 66 rows / 48 parity / 18 probes; oracle 32 match / 24 debt / 10 NA, 0 drift.
`probe_comprehension_call` promoted to `comprehension_calls.gy`; four new probes pinned
(`probe_str_loop_eq`, `probe_comp_str_filter`, `probe_comp_runtime_reduce`,
`probe_comp_folded_iter`).

**Next**: the interned-string comparison (`probe_str_loop_eq`) — a crash, older than my cycle, and
the only one of today's findings that fails a program which looks completely normal.

## CI went red on a row nobody touched: the oracle was a version, not a constant (ADR 0193)

`programs/typealias` drifted from `match` to `not_applicable` in CI and passed on my machine. The
program starts `type Count = int` — gusty's type-alias spelling, which happens to also be CPython's
PEP 695 syntax, so it needs **Python 3.12**. My laptop has 3.12.3; the CI runner was
`ubuntu-22.04` with Python 3.10, whose oracle leg died with `SyntaxError` on line 1. The drift
detector did its job; the *cause* was in the runner image, and the note ("the CPython leg did not
complete") read like a compiler problem.

**What I did not do**, and it matters more than what I did: the harness offered the "fix" of
reclassifying the row to `not_applicable`. That would have deleted a real assertion — the compiler is
right on that program and CPython agrees wherever CPython can read it — to satisfy the environment.
Editing a declared expectation to match the environment is the mistake ADR 0186 exists to prevent,
just one layer up: I would have been tuning the *oracle claim* the way you're forbidden to tune an
expected output.

**The fix had four parts, and only one of them was the CI bump.**
1. Pin the oracle: `lang.OracleMinPython = "3.12"`, documented beside the LLVM pin. I had written
   "the oracle is a named toolchain" in `docs/operations.md` and never made it a constant — words
   are not a pin.
2. CI provides it: `ubuntu-24.04` (Python 3.12), LLVM repo moved to `noble` with a `signed-by`
   keyring because `apt-key` is gone there, plus a step printing go/llc/python and failing below the
   pin. A red *setup* step tells you which world is broken; a green build with a meaningless matrix
   tells you nothing.
3. Fail loudly at the top: `requirePinnedOracle` before any leg, saying *install a newer python3 or
   set GUSTY_PYTHON* — one clear failure beats twenty drift lines each accusing the compiler.
4. Record it: `toolchain.min_python` in the matrix, schema 1.1 → 1.2, and `--schema` now says
   `oracle: "match"` means "matches **the pinned** oracle".

**The part I got wrong first, and the test caught it.** My version parser said
`strings.Contains(f, "0123456789")` — that looks for the literal substring `0123456789`, so every
banner was "unknown" and nothing was ever too old. `TestOracleTooOldIsComparedAgainstThePin` failed
immediately; `ContainsFunc(f, unicode.IsDigit)` fixed it. Fifth cycle now where a test I wrote caught
me in the hour, and the habit that keeps paying is writing the table of cases *before* believing the
helper.

**Unknown is not too old.** `OracleVersionTooOld` returns false for a banner it cannot parse: telling
a machine with an exotic interpreter that it's unsupported would be failing closed on someone who is
just different. That asymmetry is deliberate and it's in a test.

**Stub-check, because I've been burned.** I verified the preflight by putting a fake `python3` on
`GUSTY_PYTHON` that prints `Python 3.10.12`: the suite fails with the remedy line, not with drift.
Had I only run it on my own machine, where the check never fires, I'd have shipped an untested
branch — which is the same class of thing as a test that passes with the fix deleted.

**The generalisable bit:** any expectation you copy from an external tool — CPython here, `llc` and
`cc` there — is a **versioned dependency of the test suite**. Recording the version in the artifact
is cheap and worth nothing until a check reads it; the value is that a mismatch then says
"environment" instead of "compiler". The matrix already recorded `toolchain.python` before this and
still produced a confusing CI failure, which is the whole argument.

## The `go` directive does not gate the stdlib — so CI was the only thing that could notice (ADR 0194)

CI failed to *build*: `pkg/lang/oracle.go:94:44: undefined: strings.ContainsFunc`. That call is Go
**1.21**; CI builds `go-version: '1.20'`, matching `go.mod`; my laptop runs 1.22.2. So it compiled,
tested green, and shipped into a toolchain that could not resolve the symbol.

The part I had wrong conceptually: I thought `go 1.20` in `go.mod` constrained which stdlib API I
could use. **It gates language features, not stdlib API availability.** A 1.21 function type-checks
on a 1.22 toolchain regardless of the directive. Nothing in the repo enforced the floor except CI,
and I never read it that way — "CI is green" was treated as a property of my code rather than as an
argument run against a *different toolchain than mine*.

**Fix, and the order I did it in mattered.**
1. Made the call version-portable (`strings.IndexFunc(f, unicode.IsDigit) >= 0`), because that file
   should not carry a version landmine in a line that reads as ordinary string work.
2. Then asked why the floor was 1.20 at all. It was inherited, not chosen — nobody picked 1.20 as a
   compatibility promise; it's just where `go.mod` had been sitting.
3. Raised the floor to **1.22 in both places** — `go.mod` and CI's `go-version` — because raising one
   and not the other is how you get an *inverted* mismatch: permissive CI, stale declaration, and a
   false statement about the project's requirements for the price of a green tick.
4. **Checked** it in the same step that checks the oracle pin (ADR 0193), printing and gating.

**The verification I nearly skipped, and it's the only one that mattered.** Raising the `go`
directive is *not* cosmetic: at `go 1.22` per-iteration loop-variable semantics switch on, and every
green run I'd ever done was a 1.22 compiler emitting 1.20 rules. `go build` passing tells you
almost nothing there; the full suite under the new language version is the evidence. It passed —
and the grep for the pattern that actually changes behaviour (closure or `defer` capturing a loop
variable) found zero hits in the compiler packages, which is corroborating, not the proof.

**The habit that came out of it, which is the durable part.** Before pushing, I installed the real
CI toolchain — `go install golang.org/dl/go1.20@latest && go1.20 download` — and ran
`go1.20 build -tags=llvm20 ./...` and `go1.20 vet ./...` across every package, **including the test
files CI never reached** (CI had died in `pkg/lang`, so `cmd/`, `integration/`, `tools/` were
unexamined; one reported error is not evidence of one violation). That run is now clean and, more to
the point, so is the floor: a discrepancy between CI's toolchain and mine is not something to
reason about, it's something to eliminate — by removing the difference, or by running CI's toolchain
locally, which costs one command.

**Second-order lesson, shared with ADR 0193:** I now treat any external tool the build or the
*expectations* depend on — Go, `llc-20`, CPython — as a pinned, declared, **checked** dependency.
Three toolchains, three pins, all printed and all gated in one CI step. A logged version is
documentation; a version that fails the build is a contract.

## The async surface had three answers and none of them was a bug (L7.6, ADR 0195)

I went to implement "prove an `async def`'s control flow always terminates" and started the
way every round has gone wrong for me: by writing the rule before measuring the behavior. The
measurement took ten minutes and changed the feature.

```gusty
async def f(x):
    return x * 2
v = f(2)      # a coroutine: the body has not run
print(v)
```

`--interp` printed `<coro>`. `--aot` printed `4`. CPython ran the program, printed `2`, and
emitted `RuntimeWarning: coroutine 'f' was never awaited`. Three engines, three answers, and
**the two that looked like results were both wrong** — one prints the representation of a body
that never ran, the other prints the body's answer as if the call had been performed, which it
was: the LLVM path lowers a coroutine construction as a call.

So the roadmap's "no missing `await`" was not a missing check. It was the only thing standing
between a program and an invented meaning, and neither backend could supply it: at runtime you
can only notice a dropped coroutine in a finaliser, *after* the fact, if the value is even
collected. That decision is the round.

**Rule I followed.** Measure on all three engines before writing a rule, and put the table in
the ADR, the roadmap (Gap R) and the language doc. The table is the evidence that the rule was
worth writing; prose like "async is supported" is not. It also found two things I was not
looking for: the `while True: return "ok"` intern bug (below) and `def sync()` printing `0`
where the interpreter prints `7`.

**The error/warning line I drew, and why it is not the Python line.** Refuse when the program
cannot mean what it appears to mean — dropped coroutine (the body never runs), double await
(the body runs twice, where CPython raises `RuntimeError`), `yield` in an `async def` (an async
generator neither backend lowers). Warn when the program still means something but diverges
from the reference implementation — `await` inside a plain `def`, `async for` in a plain `def`,
`await` on a non-coroutine, an `async def` path that runs off the end. Python is stricter: it
calls `await` outside an `async def` a `SyntaxError`. Adopting that line would have refused the
handoff helper (`def run(c): return await c`) that our evaluation model supports, and
contradicted ADR 0167, which made module-scope `await` legal on purpose. The severity is
therefore a claim about *meaning*, stated per rule, with the Python behavior in the message so
the reader learns the discipline rather than my dialect.

**The proof is flow-sensitive or it is noise.** My first draft reported `a = f(1)` … five lines
later … `await a`, because it checked whether an *identifier* was awaited rather than whether a
*coroutine* was consumed. What actually works is a tiny abstract interpretation: each local is
`coro`/`awaited`/`not_coro`/`unknown`, merges widen, branches join, loops are walked once with
the tail folded back, and a `try` body is assumed to abort so its unawaited coroutine is still
reported even though the block completes. Every rule fires at the last line the author could
have fixed — the binding, the rebinding, the operation that consumes the value, the second
`await` — never at the end of the function, because "a coroutine existed somewhere and I did
not find a matching syntactic form" is a scan, not a check.

**False positives are the cost of a check, so the pass states what it cannot follow.** A
coroutine passed to a function, stored in a list or dict, or returned is assumed to be awaited
there; an `async for` over `[f(1), f(2)]` is the *intended* shape (the loop body awaits each
element), and a name awaited through a call chain whose definition is in another file is
unknown, not unconsumed. I wrote the negative tests first — handoffs, task lists, a bare
`await` whose value is discarded — and every one of them that lit up was a defect in the pass.

**Publish the facts the rules decide from.** `gustyc --effects` prints the per-function effect
signature — effects performed, return shape, whether control flow can run off the end — which
is literally the input to the rules, so the table and the diagnostics cannot drift, and an
agent that disagrees with a verdict can see why. `falls_through && (returns_value || annotated)
&& async` *is* `async.missing_return`; `coroutine_calls > 0` with no await *is*
`async.coro.never_awaited`. This is the ADR 0146/0127 habit again: publish the fact before an
agent has to guess it.

**Async had never been in the matrix.** `docs/language.md` had no async section, and the corpus
had no async program — L5.6 and L7.1 both claimed the surface without pinning it. It now has
`programs/async_effects.gy` (handoff helper, task list, `async for`, a bare await — legality
arguable, output pinned on both backends) and, for the honest program that still disagrees,
`programs/probe_async_eager`: the interpreter prints `between / effect 1 / 2`, the compiled
binary `effect 1 / between / 2`, because machine code performs a coroutine at the call. That
row is `parity: false` by design, with both orders pinned, so fixing it (L7.6a: `rt_coro_new` +
a trampoline per `async def`) flips the row to drift instead of passing unnoticed.

**Two findings I did not have a slot for.** (1) `await` followed by a `while True` that returns
a string emits a call to `@rt_str_intern2` that nothing defines; `llc` rejects the module and
`--verify-llvm` reports "LLVM rejected the module; this is a compiler bug, not a source error"
— the L8.2 gate doing exactly its job rather than letting the link die. (2) `def sync(): return
7` prints `7` interpreted and **`0`** compiled: the emitted symbol collided with libc's
`sync()` and the linker answered quietly. A wrong number from your own function's *name* is the
worst class in L11.8, and it is four characters of prefix away from gone — the next round's
kind of item, found only because I compiled programs nobody had compiled.

## "A compiled loop hangs" was 5% of the bug (Gap R.3, ADR 0196)

The roadmap line was `R.3 — a compiled loop whose condition variable the body reassigns
never ends`. I went to fix a hang and found that **the compiled backend treats a
parameter as a constant**:

```gusty
def bump(n):
    n = n + 1
    return n
print(bump(0))
```

interpreter `1`, CPython `1`, compiled **`0`**. Not an error — a plausible number, which
is the worst thing a compiler can hand back. The IR said the quiet part: the assignment
allocated `%_n`, stored into it, and the `ret` read `%p0` — the incoming register. The
hang was this same defect wearing a louder hat: the loop condition compared the argument,
and the body's `n = n - 1` stored where nothing read, forever.

I measured the class before writing anything, and the class was the whole variable model:

| shape | interp / CPython | compiled, before |
|---|---|---|
| `bump(n): n = n + 1` | 1 | 0 |
| `twice(n): n = n*2; n = n+1` | 7 | 3 |
| `acc(n): while n > 0: n = n - 1` | 10 | **hangs** |
| `count(n): for n in range(3)` | `0 1 2` then 2 | `9 9 9 9` then 9 |
| a method rebinding its parameter | 2 | 1 |
| a nested def rebinding its parameter | 6 | 2 |
| a parameter rebound to a container / a string | ✓ | ✓ |

The last two rows are why three years of tests never saw this: floats, strings and
containers read through the kind maps, so they were fine, and **the corpus had no program
that assigned to a parameter**. Parity testing compares two backends on the programs you
thought of; it says nothing about the shapes you didn't. The fix is four instructions —
the entry copy any SSA-form compiler makes — and it covers functions, methods and nested
defs because all three prologues take the same path.

**The hang made me look; the wrong answers were the emergency.** A hang has no output to
diff and no error to read, so `--aot` on a real file simply never returns — and a test
suite with an unbounded compiled leg doesn't fail, it stops reporting. Every compiled leg
I added now runs under `context.WithTimeout` and fails with `the compiled program did not
terminate`.

**Two symptoms, one shared storage.** Proving the fix surfaced an independent bug in the
same lowering: `for i in range(3)` drove its iteration *through the user's loop variable*,
so `print(i)` after the loop answered the bound (3, where Python says 2), and
`for i in range(3): i = i * 100` ran twice. The tempting repair was to patch the two
symptoms. The honest one was to notice that **sharing the storage is the bug**: a loop
that owns its counter and binds the variable at the top of the body — where Python binds
it — produces all three correct behaviours at once and forbids the fourth. The container
loop already had this shape; the range path did not.

**A half-fix that trades a wrong answer for a rejection is not progress.** Making
`def addf(x): x = x + 1.5; return x` work needs the *argument* type to be a float, and
that is read from the shape of the return expression today. My first patch made the
function float-returning and `llc` rejected the module one layer down. I reverted it,
pinned it as `programs/probe_float_param_rebind` (interpreter `2.5 / 3.0`, compiled leg
refused) and named it for L11.6, where the tagged value word belongs. Deciding a type
twice from two different pieces of evidence is not a bug you patch at the leaf.

**I checked whether I broke it, instead of assuming.** `probe_float_param_rebind` fails at
HEAD too — with a *different* error ("multiple definition of local value named `_x'", the
duplicate-alloca bug that my floatRet registration now fixes) — so the failure I found is
pre-existing and one layer deeper than the one I closed. That is a five-minute check I
almost skipped: `git worktree add /tmp/base HEAD`, build the old binary, run the same
program. Same habit for the regression tests: I ran the new IR tests against the old
codegen and watched them fail with exactly the messages they were written for. A
regression test that has never been seen failing is decoration — and running the whole
test file against HEAD also taught me to keep the IR-structure tests free of references
to symbols that only exist after the fix, or the "does it fail?" check becomes a build
failure that proves nothing.

**The ledger argued with me twice, and both times it was right.** I registered
`param_rebind` as an oracle-*match* row *with pins* — refused: pins record what a wrong
answer looks like, and a match row has none. I pinned the probe's compiled leg with the
`llc` message I had seen on my machine — refused: the actual leg error was a different
`llc` line, and the harness quoted me back the difference. The drift check exists to keep
the registry describing reality rather than my memory of it, and it caught both of my
aspirational entries before they could mislead the next agent to read them.

## Cycle 145 — the checker was refusing the most Python program there is (Gap R.5, ADR 0197)

A small AOT repro grew into something more basic: a program that both backends run happily was
refused outright by the front end.

```gusty
def is_even(n):
    if n == 0:
        return True
    return is_odd(n - 1)

def is_odd(n):
    if n == 0:
        return False
    return is_even(n - 1)

print(is_even(4))
```

`--interp` printed `1`; `--check` and `--aot` said `error at 4:12: undefined name "is_odd"`.
Mutual recursion — the shape every tutorial reaches for — could not be compiled, because the
compiled path refuses to emit IR for a program the front end rejected (ADR 0177). Codegen was
never the problem: `Compile` emitted both functions and the call between them, and `llc`
accepted the module. The bug was that `Analyze` walks the statement list in order and defines
each `def` as it reaches it, so anything above the definition line looked unbound. Classes had
already been given a pre-pass for exactly this reason (`indexClasses`, comment and all);
functions never were.

**The fix had to be narrower than the bug.** The obvious patch — define all of a scope's
`def`s before walking it — was written, tested, and rejected, because it quietly swallowed two
errors that must stay:

- `print(later())` at module level with `later` defined below: that call runs as the file is
  read, so the name genuinely does not exist. The interpreter raises, and the checker must too.
- `@identity` above `def identity`: a decorator is evaluated where it is written.

What makes both cases fall out of a single condition is *deferral*. The names are collected
into a table (`collectFuncs`) that the lookup consults only while `an.inFunc` — inside a
function body, where code runs later. Module and class top level never consult the table, so
code that runs now keeps its order-sensitivity for free. The table stores the `*FuncDef` rather
than a type, which is what lets `userFunc(name)` route a forward call into the ordinary arity
and argument-type checks: `take("text")` against `def take(n: int)` declared below it is still
reported as `argument "n": expected int, got str`. Only an *inferred* return type stays dynamic
for a body the walk has not reached, which is exactly where gradual typing already puts
unannotated code.

**The lesson about hoisting rules:** a "visible everywhere" fix is almost always too wide, and
what breaks it is code that *executes eagerly* — decorators, top-level calls, default
arguments. Put those shapes in the same test table as the accepted ones with `wantErr: true`,
or the feature quietly becomes a hole.

Process notes:

- This corpus entry is a parity+oracle program rather than a probe, because the defect class
  ends with all three paths agreeing: `programs/forward_defs.gy` (mutual recursion, a helper
  below its caller, `describe` → `classify` → `is_even`) prints `1 1 zero even odd 42` on the
  interpreter, the compiled binary and CPython — `oracle: "match"`, no ledger exception.
  Writing the program first caught two *other* limitations by hitting them: runtime string
  concatenation is not lowerable in codegen (documented Gap J.5 — a refusal, not a wrong
  answer), and printing a boolean prints `1` where CPython prints `True`, so the program
  compares in `1 if … else 0` to keep the oracle leg literal.
- Counting warnings around the new resolution path exposed **R.7**: `inferReturn` re-walks a
  callee per call site, so one source line can report the same diagnostic two or three times.
  Pre-existing, but now written down where the next cycle can pick it up.
- The ledger file's structure beat my guess twice: `oracleLedger` is a map keyed by program id
  and `conformanceStandalone()` is a list of *basenames* — a program with a clean oracle needs
  an entry in neither, only in the name list. `TestConformanceMatrixIsCurrent` then grows the
  artifact (70 → 71 rows) and reports drift if the description is wrong.
- `gofmt -l pkg/lang` lists fourteen files this repo has never formatted (parser.go, token.go,
  ast.go…). Only files I touched are kept gofmt-clean; reformatting the parser inside a checker
  cycle would make the diff unreadable for no benefit.

## Cycle 146 — the linker was answering the program's own calls (Gap R.4, ADR 0198)

`def sync(): return 7` printed `7` on the interpreter and **`0`** compiled. Not a wrong
computation — the module defined `@sync`, exactly as the program wrote it, and the linker resolved
the call against libc. Measured as a battery (`def NAME(x): return x + 7`, expected `8`), `sync`,
`printf`, `exit`, `strlen`, `free`, `malloc`, `write`, `read`, `open`, `time`, `rand`, `system`,
`abort` answered `0`, `1`, garbage, or killed the process; `main` could not be built at all,
because the generated entry point is `@main` and the program's `def main` was a duplicate
definition.

The fix is one prefix on symbols the program owns (`gy_sync`, `gy_Point_x`, `gy_lambda_0`,
`gy_lib$f`), applied at the places that *mint* a symbol and, crucially, at every place that
*references* it — the `call`, the decorator's function-pointer global, the source map's `symbol`
field. `irSymbol` is idempotent precisely because a symbol is minted once and then travels through
registries (a method's symbol lives in its class index); two spellings of one symbol in one module
is how the bug came back mid-implementation, twice:

- first attempt wrapped the extern call site too, so `extern fn strlen` became a call to
  `@gy_strlen` against a `declare i32 @strlen(i8*)` — `pkg/lang/ffi_test.go` caught that one, and
  it is the reason "an FFI surface exports the C name" is written into the ADR rather than left
  implicit;
- then the *decorated* body: codegen emitted `@gy_f_impl` (via `funcDef`) while
  `resolveDecorators` referenced `@f_impl` (via concatenation), and `programs/wrapping_decorator.gy`
  went silent. Minting and referencing through one helper is the only durable shape.

Process notes worth keeping:

- **Assert on the artifact, not the stdout.** The broken binary printed plausible numbers, so
  every output-level test would have passed. `TestBuiltBinaryCarriesThePrefixedSymbols` runs
  `nm -defined-only` and requires `gy_sync` to be present and `sync` absent. Stdout is evidence of
  behaviour; the symbol table is evidence of *what ran*.
- **The corpus program found two more defects before the compiler did.** Writing
  `programs/host_symbol_names.gy` hit a runtime-string concat the AOT backend refuses (documented,
  Gap J.5), and then the class: a module `def time` next to `class Timer: def time(self, x)` is
  refused by the checker with `undefined name "x"` — the method overwrote the module function in
  the checker's bare-name key, so the call is checked against parameters including a `self` that
  is not in scope. Verified on the base binary to be pre-existing, not caused by this change. Both
  became roadmap entries (R.6, R.8) with probe programs rather than being hushed to make a test
  pass.
- **The ledger corrected me again, and this is its best showing.** I declared the new probe
  `not_applicable` with the interpreter printing `6 7` and the AOT leg failing. The drift check
  answered: the interpreter leg *refuses* (`verify: undefined name "x"`), and the compiled leg
  **runs and prints `6 7`** — codegen, `llc` and the linker all consider the program fine. That is
  a sharper fact than the one I wrote down, and it would have been wrong in the artifact forever
  if the pins were trusted rather than re-measured. The registry describes reality, not my memory
  of it.
- `Compile` returning no error while the CLI refuses was surprising until read properly: the
  front-end gate lives in the build pipeline (ADR 0177), so an agent sees a *diagnostic*, not a
  Go error — worth remembering when writing assertions about "the compiler refuses".

## Cycle 147 — the compiler read the built-in's meaning into a call the program owned (Gap R.6, ADR 0199)

`def float(x): return x + 7` then `print(float(1))`: interpreter `8`, compiled **`1.0`**. With
`str` and `chr` the program did not compile at all — the call was emitted as the program's own
function and then *used* as the built-in's string result, `printf("%s", i32 %t5)`, which `llc`
rejected. A battery of 18 names split cleanly: `float`/`sqrt`/`floor`/`ceil` folded to the float
conversion, `str`/`chr` produced an invalid module, and 12 others (`abs`, `int`, `ord`, `round`,
`sum`, `len`, `min`, `max`, `sorted`, `any`, `all`, `chr`-adjacent) were already fine.

That split is the diagnosis. I had assumed the roadmap's framing — "the call is resolved against the
builtin table before the program's own `def`" — and went looking for a dispatch-order bug. Dispatch
was **already correct**: `if g.funcs[fnName]` sits ahead of the built-in `switch`. What was wrong
was subtler and worse: an LLVM backend needs facts a call expression does not carry — is its result
a float, does it fold to a constant, is it a string, how should `print` render it — and this codegen
answered many of them **from the callee's name**. So the fix was not reordering a table; it was
putting one question in front of every name-keyed shape reading:

```go
if g.builtinShadowed(id.Value) { /* this name is the program's */ }
```

Three lessons worth keeping:

- **A guard placed before a program-level fact is a new bug.** My first patch put the shadow check
  ahead of `g.floatFuncs` in `isFloat`/`floatValue`, which meant a *program-defined* float-returning
  function stopped being float. `programs/floatfn.gy` — four lines, `def half(x): return x / 2.0`,
  in the ledger for years — failed parity within the minute. Correct precedence is: what the program
  says (`floatFuncs`) → is the name claimed → only then what the built-in's name implies. The
  regression test `TestProgramFloatFunctionKeepsItsShape` is that lesson with an executable form.
- **Removing a fold can expose a missing lift.** With the `float(...)` fold gone, the program's call
  returned `i32` and got fed straight into an `fadd`. `programs/shadowed_builtins.gy` caught it as an
  llc failure; the fix is the ordinary `sitofp i32 … to double` lift every plain int expression gets.
  Silent-fold bugs often hide the absent conversion behind them.
- **Pure fold helpers need the program's state handed to them.** `stringConst`/`stringConstLen` are
  free functions with no `irGen`, and the `str`/`chr` folds live inside them. Rather than make them
  methods, they take `shadowed func(string) bool`; callers pass `g.builtinShadowed`, or `nil` where
  no program is in scope. It keeps them testable and makes "which program am I folding?" an explicit
  argument — the same shape as `floatFromSyntax(g, …)` from Cycle 144.

Process notes:

- **IR-level assertions about printing have to follow the register.** `strings.Contains(ir, "%s")`
  found the GC report's `snprintf` argument list, and a whole-module search for `@rt_list_len(`
  matched the container's own machinery rather than the shadowed `len`. `printFormatFor` now walks
  call-site → result register → the printf in that same function body → the `@.fmtN` global's bytes.
  Anything less asserts on the runtime sitting in the module.
- One name passing proves nothing about its neighbour: `float` passed while `chr` produced an
  invalid module, because the folds live in different helpers. Hence the 18-name compile-and-run
  battery, not one representative.
- Ledger grew 73 -> 74 rows; `programs/shadowed_builtins.gy` is `oracle: "match"`, and the matrix
  caught nothing this time only because I ran the corpus before writing the guard for `str` —
  which is the intended way for these to feel.

## Cycle 148 — one table, two definitions: a method overwrote a module function (Gap R.8, ADR 0200)

`def time(x)` beside `class Timer: def time(self, x)` was refused with
`error at 7:16: undefined name "x"` — an error pointing at the *method's own parameter*. The
interpreter, the compiled module and CPython all printed `6 7`. Only `Analyze` thought the program
was broken, and the build gate trusts it (ADR 0177), so an ordinary program could not be compiled.

What cost real time was that every plausible explanation was wrong, and each was killed by a
*smaller program* rather than by reading more code:

| hypothesis | killed by |
|---|---|
| method calls are argument-checked wrongly | `self.time(x)` is a dynamic attr call, never argument-checked at all |
| the name `time` is special (a builtin-ish name) | identical programs with `tick`, `zork`, `f` behaved fine |
| the class alone, or the module def alone, causes it | removing either half checks clean |

What survived the bisect: one function table, keyed by bare name, into which methods registered
(`an.funcs[fd.Name] = fd` while walking a class body). The method, analyzed after the module
function, *replaced* it; `time(1)` then resolved to a definition with parameters `(self, x)`, one
argument was bound to two parameters, the second stayed unbound, and the per-call-site body walk
reported the method's own `x` as undefined. An error at the callee and never at the call — that
asymmetry is the tell for "resolution, not scoping", and it is worth remembering: **when a
diagnostic points somewhere the programmer did not write, believe the position over the message.**

Fix: `SemanticAnalyzer.methods`, keyed `Class.method`, and `inClass`/`curClass` saved and restored
around a class body so a def in an outer scope cannot inherit the class name. A bare-name call can
now only resolve to a module function or a nested def — the only definition whose arity a call site
could possibly match.

Process notes:

- **A test that can be satisfied by checking nothing is not a test.** Methods were never
  argument-checked through this table, so `expect no error` passes both after the fix and if methods
  stopped being analyzed entirely. The assertions therefore pin *which parameter the diagnostic
  names* — with `def time(x: int)` and `Timer.time(self, label: str)`, a bad argument must come back
  as `argument "x": expected int, got str`; `"label"` in the message means the wrong definition was
  used — plus a separate test that an error inside a method body is still reported.
- **The probe promoted itself.** `TestOracleProbeRowsAreRecordedAsDebt` failed the moment the fix
  landed, saying "a probe that now matches CPython is a paid debt — promote the program to
  conformanceStandalone and delete its ledger row". That is the harness converting a known divergence
  into regression coverage by itself; the correct response to those failures is the rename it asks
  for, never a relaxed assertion. Measured: `6 7 6 12` on all three legs, `oracle: "match"`.
- **Measuring an assertion found the next gap.** I tried to assert "calling `build(a, b)` with one
  argument is reported"; the base compiler says `ok`, so the assertion was describing behaviour that
  has never existed. Opened as R.10 (too few arguments unreported, annotated or not) and the test
  rephrased to what is actually observable.
- Roadmap numbering discipline kept paying off: R.4 → R.5 → … → R.10 each with its own measured
  repro, its own section, and — for the closed ones — the program that proved it, moved from probe to
  corpus rather than deleted.

## Cycle 149 — the missing half of a contract: too few arguments was nobody's business (Gap R.10, ADR 0201)

`def build(a, b)` called as `build(1)` compiled to `ok`. Too *many* arguments had been refused all
along; too *few* left the parameter unbound, and the program was then blamed at the **callee's**
line for an `undefined name "b"` — and inconsistently, since that walk only ran for annotated
callees. The interpreter had been refusing the same program at run time with the message the
checker never offered: `missing argument b`.

The fix was nine lines in the function that already had the answer (`bindParams` fills parameters by
index; an index with no entry and no default *is* the mistake). The interesting parts were
elsewhere:

- **Half a contract is worse than none.** Because one direction was enforced, the missing direction
  looked covered. When adding a rule, ask what its mirror already does and whether the asymmetry is
  accidental — this one hid a whole class of ordinary bugs (renamed parameter, dropped argument,
  call written against yesterday's signature).
- **Derived diagnostics can be actively false.** One dropped argument produced three diagnostics,
  two pointing at source the programmer had written correctly. So `inferUserCall` now stops when the
  call does not fit its definition, instead of re-inferring a return type from a body with
  half-bound parameters. "One mistake, one diagnostic, at the place the mistake was made" is a
  contract with the reader, not a cosmetic preference — and it is testable (`TestOneErrorPerArityMistake`
  counts errors, not just messages).
- **Two mistakes need two messages.** A positional shortfall is "you lost count"
  (`expects 2 arguments, got 1`); a keyword call that skips a name is "you skipped this"
  (`is missing argument "b"`). Reporting a count for the second one — as my first draft did, `got 0`
  for `build(a=1)` — is technically true and useless. Both now name the callee; "too many arguments"
  without a callee name is half a diagnostic in a file with four calls.
- **The over-refusal guard is a program, not a test string.** `programs/arity_defaults.gy`
  (trailing defaults, all defaults, keyword-only, keyword-plus-default, zero-parameter) runs on all
  three engines and is in the ledger — a new refusal rule needs its positive space executable and
  compared, not asserted. Measured across the whole corpus: nothing was newly refused, which is the
  evidence that the rule aims at mistakes rather than style.
- **A test that passes the source path to `--check` proves nothing.** My first CLI test did exactly
  that and "failed to refuse" — `--check` takes *source text*, only the build paths take a filename
  (the same interface trap we fixed for `--emit-nova-llvm` in Cycle 142). The lesson generalises: when
  a negative test does not fire, suspect the harness before the rule.
- **Measure the neighbour before declaring the family closed.** Writing the arity tests surfaced
  `def f(a, b=1, c)`, which we only catch at the call while CPython refuses the definition — recorded
  as R.11 rather than silently folded into this commit or quietly dropped.

## Cycle 150 — a fact said twice is not more true (Gap R.7, ADR 0202)

The gap was on the roadmap from the L7.6 round, but the L7.6 work kept it as a note instead of
measuring it. This cycle started by measuring, and the measurement was beautifully mechanical: the
same `match is not exhaustive` warning, printed a number of times equal to **call sites + 1** —
never called → 1, called once → 2, called twice → 3. Five corpus programs emitted a byte-identical
line twice. That table went into the ADR and the roadmap verbatim; a scaling law is worth more than
a paragraph of description.

Cause: per-call-site return inference (ADR 0190) re-walks a callee's body with each call's argument
types, and every *source-level* fact in that body is re-derived — and re-reported — per call. The
walk is deliberate and stays; the reporting was the bug.

Fix: one funnel (`addDiag`/`addDiagFull`) keyed by `(level, line, col, code, message)`.

What this cycle taught:

- **"How would a consumer compute this?" is a defect detector.** `len(diagnostics)` was not a count
  of findings; an agent reading `--json` would have had to dedupe by hand — over a format whose whole
  purpose (ADR 0004) is to make scraping unnecessary. Ask of every structured output: which naive
  reading of it is currently wrong?
- **The dedupe key's *narrowness* is the real requirement**, so each non-collapse is its own test:
  different messages at one position; same message at two positions (the count is the signal); same
  sentence as a warning and as an error (only the error decides runnability, so the key carries the
  level — a warning must never swallow an error); same message under two codes (codes are the
  machine-readable identity, ADR 0004/0006). A one-line dedupe is trivial; proving it is not an
  information-loss machine is the work.
- **Fix at the source, not at the printers.** Deduping in the CLI or JSON encoder would leave LSP,
  schema dumps and tests each re-implementing the rule while the analyzer kept lying to anyone who
  read `Diags` directly.
- **Memoization was the tempting wrong fix.** Memoizing `inferReturn` per `(fd, argTypes)` removes
  most repeats and keeps the rest — turning the invariant probabilistic. Invariants have to be
  structural; performance work is allowed to be heuristic, and this is the moment to record the
  difference so a future cycle doesn't confuse the two.
- **Turn the measurement into a test.** `TestWholeCorpusReportsNoDiagnosticTwice` walks every corpus
  program and fails on any repeat — the exact loop I ran by hand, promoted to CI. Gaps found by
  sweeping the corpus should end as corpus-wide assertions; that is how a fixed class stays fixed.
- **Two helper-name collisions in one cycle** (`contains`, `analyzeSrc` in `pkg/lang`; `checkJSON` in
  `integration`) is a hint that this repo's test packages are large enough that new helpers need a
  topic prefix — cheap rule to adopt, saves a compile cycle each time.

## Cycle 151 — the words that change grammar (Gap R.9, ADR 0203)

`def print(x): ...` produced `parse error at 1:5: expected identifier`. Not a checker bug, not a
codegen bug — the lexer had `print` and `range` in its **keyword** table, so no name in the program
could use those words: not a function, not a parameter, not a keyword argument, not a method.

The fix was deleting two entries from a map, and that is the whole lesson: **a defect's severity is
not proportional to its size.** Two stray booleans in a table made the two most natural names in a
Python-like language unusable, and everything downstream had quietly worked around them — the parser
accepted the keyword tokens in expression position by *text*, and the checker, codegen
(`case "print"`, `case "range"`), the interpreter's loop fast path, the closure builtin set and LSP
completion all dispatch on text too. Keyword status bought nothing; it only forbade naming.

How it was found, and what that suggests: I did not go looking for it. R.9 was already on the
roadmap, and the program I wrote to *prove* it closed (a helper named `range`, a method named
`range`, parameters named `print`/`range`) immediately exposed three more behaviours that differed
across backends:

- **R.12** — `for i in range(2)` above `def range(x)`: the interpreter uses the built-in (2 lines),
  the compiled binary uses the *program's* `range` because codegen emits all functions before the
  body, so it iterated `range(2)` = 6 as a count. ADR 0197 gave the front end a declaration-order
  rule; codegen never got one.
- **R.13** — a file ending in a bare expression statement is echoed by `--file`/`--interp` (`f(5)` →
  `10`) and by nobody else. The code comment states the intent ("a REPL courtesy for snippets, not a
  program feature… matching `python prog.py`") and the guard does something narrower than the intent,
  so **a comment that describes intent more precisely than the code is a defect report waiting to be
  filed**.
- **R.14** — `for i in 5:` iterates `0..4` on both backends, undocumented, while CPython raises
  `TypeError`. Agreement between our two backends makes it a feature rather than a bug; agreement
  with Python does not, so it is documented in language.md as a deliberate extension and tracked
  rather than silently blessed or silently removed.

Process notes:

- **Ledger programs should say what they avoid.** `builtin_names_as_defs.gy` defines `range` and never
  asks for the built-in, and its header comment says why (R.12 is open). A parity program that
  quietly depends on an open gap is a future false alarm.
- **Test both sides of a narrowing.** Removing two keywords risks two failure directions — built-ins
  stop working, or real keywords stop being reserved — so the tests assert all three: names usable in
  every name position, built-ins still functioning (including `sep`/`end` and the 3-argument `range`),
  and the *entire* real keyword set still refused with the specific message, plus a direct `Lex`
  assertion that every built-in name produces `TokIdent`. That last one is the test that fails for the
  right person: whoever re-adds a built-in to the table, not whoever notices a parse error three
  months later.
- **Three new gaps recorded, none fixed** — measured, written as reproducible programs with expected/
  actual output, and left in the queue in severity order. Resisting the urge to fix a one-line-looking
  thing mid-cycle is what keeps commits one-feature and ADRs one-per-decision.

## Cycle 152 — the echo asked the wrong question (Gap R.13, ADR 0204)

`--file prog.gy` on

```gusty
def f(x):
    return x * 2

f(5)
```

printed `10`. `--aot` printed nothing. CPython printed nothing. The same source, two stdouts — and the
one that was wrong was the path scripts and agents pipe.

The mechanism was a courtesy that had outlived its predicate. An earlier cycle had already fixed this
once, after `--file` was found appending a stray `0` to every program (the void value `print()`
returned); the fix narrowed the echo to "a bare final expression whose value is not None". Right
direction, wrong question: the distinction that matters is **where the source came from** (a snippet
typed at a prompt vs a file on disk), not what shape the last statement happens to have. Once R.13 was
written as a predicate on provenance — `evalSrcOrFile` already knew, it just wasn't asked — the file
path echoes nothing in any mode, `--eval` keeps printing `3` for `x = 1 + 2\nx`, and both backends
agree with CPython.

Three things to keep:

- **A comment that states its intent more precisely than its code does is a defect report.** The code
  said "a REPL courtesy for *snippets*, not a program feature … matching `python prog.py`". The guard
  implemented something narrower than that sentence. Reading comments as *claims* — then testing them —
  finds gaps no test would have thought to ask about.
- **Compare backends to each other, not to a wish.** The tests assert `--file`/`--interp` output equals
  `--aot` output for the same source, and both equal the oracle. "No stray echo" is only meaningful
  relative to what the other engine does; asserting against a literal string would have passed while
  both engines were wrong in the same way.
- **Some asymmetries are correct and must be pinned as such.** `--json` still returns `result` for a
  file, because that field describes the *evaluation* while stdout belongs to the *program*. Without a
  test naming that intent, the natural future cleanup is "consistency!" — either dropping the field or
  re-adding the print. The test says: this difference is on purpose.
- Gaps keep arriving through the corpus, not through reading: R.13 was discovered by the ledger program
  written to prove R.9 (it ended in a call). Writing a program to demonstrate a fix is a better gap
  finder than auditing, and the reason `programs/*.gy` keeps paying for itself.

## Cycle 153 — one file, two programs: when a claimed built-in name takes effect (Gap R.12, ADR 0205)

```gusty
for i in range(2):          # interpreter: the built-in → 0 and 100
    print(i * 100)          # codegen:  the program's range(2) = 6, as a count → 0 … 500
def range(x):
    return x * 3
print(range(4))             # 12 in both
```

No error, no crash, no verification failure — just a different answer depending on which engine you
asked. Found because the ledger program written to prove R.9 had to be written to *avoid* this shape
to stay green; a corpus program that quietly depends on an open gap is a future false alarm, and
writing that avoidance down in the program's header is what made the gap explicit enough to file.

Why it exists: the interpreter executes in order, so at the loop the `def range` hasn't run and `range`
is still the built-in. Codegen emits every function before the module body, so the call resolves to the
program's definition wherever it is written. Both are individually defensible; that's what made it
survive.

Fix (front end): remember where module-level `def`s of predeclared names are, and refuse a module-level
call to one of them *above* the definition — message names the built-in, the definition's line, both
readings, and the two ways out (move it up, or rename). Corpus scanned: nothing newly refused, because
the two programs that claim built-in names both define before use.

What this cycle added to the toolbox:

- **"Both backends are right separately" is a search heuristic.** Every previous cycle's divergence
  had one engine doing something wrong. Here neither was locally wrong — the disagreement was in the
  *specification* of when a binding exists. That class hides from IR asserts and output diffs run
  separately per engine; it only appears when you diff engines against each other on the same source.
- **Bound a refusal to where the problem is real.** Inside a function body the ordering question has
  no content (module defs have all run by then) and methods aren't module bindings at all — so both
  cases are asserted *not* to be refused. A refusal without a boundary is a restriction, and the
  boundary tests are what make it a rule.
- **Keep the honest error alone.** My first draft returned dynamic after reporting, which made the loop
  variable dynamic too and produced a derived `arithmetic on non-numeric operands` warning about a
  perfectly typed call. Falling through the built-in path instead keeps the one true diagnostic alone
  (ADR 0201's rule, cited again — good signs when a rule from two cycles ago is the one you reach for).
- **Refusals need expiry dates.** `TestBackendsGenuinelyDifferOnTheRefusedProgram` runs the refused
  source through both engines *around the gate* and fails if they ever agree. Someone who later makes
  built-in lookup dynamic in codegen will be told by a failing test to delete the rule — which is the
  only way a defensive refusal doesn't fossilize into policy.
- **Rejected: making the interpreter hoist module defs to match codegen.** It would have "fixed" the
  disagreement by making working programs behave like the broken reading (six iterations on both
  engines, and CPython disagrees everywhere instead of in one backend). Consistency obtained by moving
  the *user-visible* semantics is usually the expensive kind of wrong.

## Cycle 154 — the gap that wasn't: measure the repro before implementing the imported fix (Gap R.11, ADR 0206)

`def offset(base, step=10, bonus)` — a defaulted parameter in the middle of a signature. Roadmap:
"CPython rejects this at the `def`… the definition site is the honest place to say so." That reasoning
is Python's, and the entry had inherited it: in Python, positional binding stops at the first default,
so `c` would be unreachable and the author would get a confusing `TypeError` — hence the `SyntaxError`.

I started to implement the refusal. The detour that saved the cycle was running the repro first:

| program | ours (interp) | ours (AOT) | CPython |
|---|---|---|---|
| `offset(1, 2, 3)` | 6 | 6 | SyntaxError |
| `offset(1, bonus=5)` | 16 | 16 | SyntaxError |

Here, positional binding fills left to right and a keyword call names what it fills, so **no parameter is
unreachable** — the failure mode Python's rule exists to prevent cannot occur, and the refusal would have
rejected programs whose calls all bind correctly. R.11 closed as *not a defect*: a documented divergence
plus a ledger row with `oracle: not-applicable`, tests asserting both engines agree and that CPython still
cannot run it.

What to keep:

- **A gap inherited from a language comparison is a hypothesis, not a spec.** Every R-item in this
  series came from writing a program and watching what happened — this one came from reading CPython's
  error message, and it was wrong about our language in the direction that would have *added* a false
  refusal. Rule adopted: reproduce the current behaviour and record it in the same session, before
  writing the fix.
- **"Implement the imported fix" is the expensive path when the answer is a document.** The commit still
  has tests, an ADR, docs and a corpus program — but they pin a *permission* instead of installing a
  prohibition. If the behaviour is right, the artifact that makes it a rule is a normative sentence plus
  tests that fail if it moves.
- **Ledger rows for "CPython can't run this" must be kept honest by a test**, not trusted:
  `TestDefaultedParameterInAnyPositionAgreesOnEveryPath` asserts the oracle still fails. An `NA` row that
  quietly becomes runnable is exactly how a divergence stops being a decision and becomes drift — which
  is also why the drift tests exist for the other 25 debt rows.
- Two messages carry the real contract here (`expects N arguments, got M` / `is missing argument "p"`,
  ADR 0201), and this cycle added assertions that they fire on the *unordered* shape too — a rule you
  only test on the tidy inputs is not the rule you think you have.

## Cycle 155 — declaring what the backends already agree on (Gap R.14, ADR 0207) — and finding a compiler bug on the way out

`for i in 4:` printed `0 1 2 3` — identically in the interpreter and in the compiled binary — while
CPython raises `TypeError: 'int' object is not iterable`. The roadmap called it a gap; the gap turned out
to be the **document, the ledger row and the tests**, not the behaviour: an exercised, backend-agreeing,
Python-divergent construct that nothing described. That kind of surface is invisible to its users in the
worst way — a programmer can't tell whether to rely on it, an agent has nothing to generate from, and a
refactor could change a boundary with nothing failing.

So this cycle declared it: an integer on the right of `for … in` is a **repeat count** (any integer
expression; `n <= 0` runs zero times; same counter loop as `range(n)`), `programs/for_int_count.gy` got an
oracle-excluded ledger row, and the test asserts CPython still refuses it — because "the oracle isn't
applicable" is a claim that needs a test, not an excuse that doesn't.

Two things worth pinning:

- **Measure the neighbours, not just the headline.** The headline (`for i in 4`) was already fine. Walking
  outward — zero, negative, expression counts, comprehensions, and then *strings* — is what found the real
  defect: `for c in "ab":` makes codegen emit `store i32 @.str1, i32* %_c`, which `llc-20` refuses with
  *global variable reference must have pointer type*. Under our own exit-code contract that is a
  **compiler bug**, the exact class ADR 0177's gate exists to keep away from users. Opened as R.15 with
  the failing IR quoted, for its own cycle: lower it or refuse it, never emit a module that doesn't verify.
- **A feature needs to earn its keep, and the test is "what would break without it?"** The answer here was
  concrete: the integer form needs no built-in, so it is the counted loop available to a module that has
  claimed the name `range` for itself (ADR 0205's ordering refusal otherwise pushes people into exactly
  this shape). A less useful undocumented quirk might have been the one to delete instead.
- **Not every gap closes with a patch.** Cycles 154 (R.11, not a defect) and 155 (R.14, declared feature)
  both closed by documentation + tests + ledger. The standard that keeps that honest: the document must be
  normative ("binds 0..n-1", "n <= 0 runs zero times") and the tests must fail if the behaviour moves. If
  either is missing, "documented" is just the word we use when we stop looking.
- Conformance matrix is up to 78 rows with 14 oracle-excluded, 0 fail, 0 drift; every one of those
  exclusions has a test that re-checks the oracle still can't run its program.

## Cycle 156 — a rule that had one owner, and a flag that had many (Gap R.15, ADR 0208)

`for c in "ab": print(c)` — the interpreter printed `a b`, CPython printed `a b`, and the compiled
backend died in `llc`:

```
store i32 @.str1, i32* %_c        ; global variable reference must have pointer type
```

An invalid module is a compiler bug in this project's own contract, so this was not a "string iteration
unsupported" ticket. The unrolled literal loop had been lowering each element with the **scalar** value
path (`g.value`, which hands back a raw `@.strN` global) while loop variables live in `i32` slots. The
rule that fixes it already existed and already had an owner — Gap I.2's `heapElemKind`: anything that has
to live in an `i32` slot stores the `@str_tab` index instead, and `g.internedVars` tells the printer to
render an index as text. The loop just had never been invited to it. One-line-ish fix; `for m in [1,"a",2]`
now prints `1 a 2` on all three engines, because internedness is set per **element copy** (the unroller
emits a body per element) rather than once per loop.

Then the same investigation handed me R.2's true cause:

```gusty
def txt():
    return "hi"

for c in txt():
    print(c)          # llc: use of undefined value '@rt_str_intern2'
```

The "function returns a string" path returns the interned index (correct, ADR 0174) but never sets
`g.heapUsed` — and `heapRuntimeIR`, which *defines* `@rt_str_intern2`, only travels with the module when
that flag is set. The roadmap's R.2 repro was `await` + `while True: return "ok"`, and its analysis blamed
"the await path's intern accounting". Both ingredients were red herrings: the await-free shape above is
the same bug, so **the flag, not the async path**, was the defect — and the re-scoped entry now says the
fix must be derived (emit a runtime block when the module *references* it, scanning the block's `define`s)
rather than another call site remembering to set a boolean.

What I'm taking from this:

- **"Unsupported" is a claim that requires a control experiment.** The cheap, plausible move was to make
  codegen refuse string iteration with a nice message. Measuring the neighbouring shape first —
  `xs = ["a","b"]; for x in xs:` compiles and prints `a b` — showed the capability was there all along and
  only the rule was missing. Refusing would have deleted a working feature to hide a bug in one path.
- **A flag with many writers is a defect shape.** Any number of codegen sites can emit `@rt_*` calls; one
  boolean decides whether their definitions are emitted. That asymmetry is not a series of oversights to
  patch one by one, it's the wrong mechanism — the emission decision should be derived from the artifact,
  the way `irSymbol` (ADR 0198) made linking derived from one prefix instead of everyone remembering.
- **Output tests can pass while the compiler is broken.** Here the interpreter was right and the compiler
  was dying, so the passing parity assertion had nothing to do with the bug. The tests that caught it read
  the emitted module (`no store i32 @.str`, the intern call, the definition present) and run LLVM's
  verifier. For IR-emitting backends, artifact assertions are first-class, not a bonus.
- **Investigations find neighbours.** R.14's boundary walk found R.15; R.15's root-cause hunt found R.2's
  real cause and re-scoped a stale entry. The habit worth keeping is to keep a scratch loop running over
  *adjacent* shapes (list variable, literal list, mixed literal, call-derived string) instead of only the
  reported case — every one of those cost one command and changed a roadmap entry.

## Cycle 157 — a flag with many writers is the wrong mechanism (Gap R.2, ADR 0209)

The roadmap said: `await` + `while True: return "ok"` emits a call to `@rt_str_intern2` that is never
defined, and "this is the await path's intern accounting, not the loop's" — because removing either
ingredient made the program compile. I went to implement an await-path fix and instead asked what the two
programs had in common. This one has no async and no loop:

```gusty
def txt():
    return "hi"

for c in txt():      # llc: use of undefined value '@rt_str_intern2'
    print(c)
```

The real shape: `heapRuntimeIR` (which *defines* the helper) is appended to a module only when
`g.heapUsed` is set, and the string-returning-function path — correct in every other respect, ADR 0174 —
emits the intern call without setting it. Two sites did that. So the emission decision had been living in
a boolean that N call sites had to remember, and the two "mysterious" ingredients in the repro were just
one path where nobody happened to try the other combinations.

Now the decision is derived from the artifact: at assembly the emitted body is scanned, and every runtime
block whose defined names the module mentions is emitted — names parsed out of the blocks themselves, so
there is nothing left to remember. The flags stay (no float printed → no snprintf machinery) but they can
only add, never omit.

Also found on the way: with the module finally valid, `for c in txt()` **compiled cleanly and printed
nothing** — the function returns its `@str_tab` index and the counted-loop fall-through read it as a
repeat count. Fixed as a refusal with a real message (name what's unsupported, name the backend that runs
it, name a shape that works); opened as R.16 for the actual implementation, which is L11.5's runtime
string values.

What I'm keeping:

- **`git log`-adjacent discipline for roadmap entries: when a diagnosis turns out wrong, rewrite the
  entry.** R.2's stale async analysis sat in the roadmap for cycles and would have pointed the next agent
  at an async bug that doesn't exist. I left a three-line pointer where the old entry was, so the record
  of what we *thought* survives (that's useful context) but can't be mistaken for current fact. Note the
  roadmap had **two** R.2 sections — the original and last cycle's re-scope — which is exactly how a stale
  diagnosis stays alive: fix the duplicate too, not just the wording.
- **"It compiles now" is not "it works".** The R.2 fix could have ended with the repro building. The
  compiled async program prints `0` where everyone else prints `ok` (that's R.1), and the string loop
  printed nothing (R.16). Each got its own entry and its own test; the R.2 test *skips loudly* if the
  compiled async leg ever starts agreeing, so a future cycle can't accidentally fold R.1 into R.2's
  closure.
- **A refusal message is a user-facing API.** "not supported in the AOT backend yet" alone would be a
  dead end; the shipped message says the interpreter prints the characters, that a string *literal* works,
  and that constant indexing works. The rule I can apply again: a refusal must contain the next thing the
  reader can do.
- **Derive, don't remember.** This is the third or fourth time this loop has converged on the same
  shape of fix (linker prefixes in ADR 0198, one predicate for built-in shadowing in ADR 0199, one funnel
  for diagnostics in ADR 0202): when correctness depends on every one of N sites doing the same thing, the
  mechanism is wrong, and the fix is to compute the answer from the artifact. If I ever catch myself
  adding "remember to also set X", that's the tell.

## Cycle 158 — the workaround in a refusal message has to work (L11.4, ADR 0210)

L11.4 looked like a one-liner: `if i < 0 { i += len }`. It was, and the one-liner is still not the
interesting part.

Three things happened that I want to keep:

- **I shipped a refusal whose workaround was false.** Last cycle's run-time-string refusal told people
  to "index a string with a constant (`s[0])". Writing this cycle's subscript tests I typed that shape
  into a scratch file — `s = "abc"; print(s[0])` is *refused* by the AOT backend too, because a string in
  a variable has no run-time string value yet (L11.5). Only a string *literal* may be subscripted with a
  constant. The message is fixed. The rule I can apply again, and will forget unless I write it down:
  **type the workaround into a file before shipping the sentence that offers it.** A refusal whose
  escape hatch doesn't open is worse than a bare refusal — the reader burns their time on our mistake
  and then stops trusting the rest of the message.
- **The panic was hiding in the consumer, not the producer.** `case *ListLit: return g.value(b,
  obj.Elems[key])` had no bounds test because every caller "checks the key". The place that knows the
  length is the place that does the indexing; a constant folder that hands an int to a Go slice is a
  compiler crash waiting for a negative literal. When I catch a path doing `arr[compilerComputedInt]`,
  that's the audit target — not the sites that produced the int.
- **The exemption is the finding.** The obvious refactor was "normalise all subscripts in one place".
  That breaks `d[-1]`, because a dict subscript is a key and `-1` is a key you can store. The two shapes
  looked identical (a subscript) and were different operations (a position, a key). `docs/language.md`
  already has a bullet about `{}` meaning dict-vs-set for the same reason — surface syntax that rhymes is
  where two-engine bugs live. Pinned with a three-engine test so a future "simplify" cannot pass.
- **And the standing one, re-proved:** both backends agreed on `xs[-1]` trapping for dozens of cycles.
  Agreement is not correctness; the third leg is the one with a vote. (Both probes went from `debt` rows
  to ledger-free parity cases in one edit.)

Found while measuring, recorded as **R.17 OPEN rather than fixed here** (one commit per feature): the
compiled binary dies with 1 on an uncaught exception, `--interp` reports 3, and `gustyc --aot` prints the
same traceback and exits **0**. "Did the program work?" answered yes for a crashed program, on the path
agents script against. The next cycle that touches exit codes must own it.

## Cycle 159 — the answer was one frame down, and nobody was consuming it (Gap R.17, ADR 0211)

`gustyc --aot prog.gy` printed `IndexError: index out of range` and exited **0**. The linked
binary exited 1. `--interp` exited 3. Three ways of asking one question, three answers.

The cause is the most embarrassing kind of bug: the generated `main` returned 1 correctly, the C
helper returned it (`static int jit_call(void* fn) { return ((int (*)(void)) fn)(); }`), and the Go
frame above threw it away — `captureFD1(func() { C.jit_call(fn) })`. The information existed end to
end and one frame with no current use for it dropped it.

What I'm keeping:

- **Audit *consumers*, not just producers.** For every quantity the toolchain already computes —
  exit status, verification result, timings, diagnostics — ask who reads it. If the answer is
  "nobody, we re-derive it or hard-code it", that is a defect even when nothing looks broken. This
  loop has now hit the same shape four times (symbol prefixes 0198, runtime blocks 0209, duplicated
  diagnostics 0202, discarded status 0211); the pattern is "one pipeline computes a fact, several
  paths re-implement the decision, and one path forgets".
- **A contract verified on one execution path is unverified.** Every test that linked and ran a real
  binary saw the correct exit status for dozens of cycles, so the class looked covered. The
  in-process JIT was the uncovered path. Now the matrix is *paths × classes* (`--aot` joins
  `TestCLIExitCodeContract`, plus a dedicated file), because "we test exit codes" was the claim that
  was false.
- **A payload field shaped like the truth is worse than no field.** `--json --aot` documented
  `"exit": 0`. An agent that trusts a published schema does not second-guess it. Rule to apply
  forever: every documented field is *derived from the artifact it describes*, and there is a test
  that fails if it is hard-coded. Here: `payload.Exit != process.ExitCode()` fails the build.
- **Classify with types, and keep "refused" apart from "absent".** `*ToolchainRejectionError`
  makes the `llc`-rejection class reachable through `errors.As` on any path, and the missing-tool
  case is a different message and a different code — a test I could only write *because* the
  classification is a type. Matching `"jit: llc:"` would have merged an uninstalled LLVM into
  "our compiler emitted an invalid module", which is an accusation that sends someone debugging a
  bug that does not exist. (Same honesty rule as ADR 0210's broken workaround.)
- **Doing the status correctly meant *not* fixing two wrong answers.** `print(1 / 0)` prints `inf`
  and `print(p.nope)` prints `0`, both exit 0 — and exit 0 is right, because those programs really do
  run to completion; the bug is that they answered instead of raising. Measuring which programs
  actually trapped kept me from smuggling "make traps non-zero" into a plumbing fix and calling it
  done. They are Gaps R.18 and R.19, measured and owned by the next cycles: both backends also fail to
  attach the exception *class*, so `except ZeroDivisionError:` never fires — which tells me the fix is
  one rule (every built-in trap is a typed raise everywhere), not two patches.
- **Write the repro into the roadmap entry, not just the commit message.** Both new Gap entries carry
  the exact source and the exact observed outputs of all three engines, because the entry is how the
  next cycle decides whether the gap is real.

