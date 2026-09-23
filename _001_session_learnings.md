# Session Learnings
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
Get the `gusty` compiler building and passing tests against LLVM 20 with TinyGo's
`tinygo.org/x/go-llvm` bindings, and fix the printf codegen semantics. Continue the
greater vision (Python-like language) — but this session focused on the build/correctness
fix, not new language features.

## Key facts discovered

### go-llvm dependency & the local reference dir
- The local `go-llvm/` directory is a **full separate module** (own `go.mod`, `go 1.14`)
  with the *free-function* API (`llvm.NewModule`, `llvm.Int32Type`, `llvm.VoidType`,
  `llvm.NewBuilder`).
- The module-cache version `tinygo.org/x/go-llvm v0.0.0-20260721072906-185673ef46a5`
  has the **Context-method** API (`func (c Context) NewModule`, `Int32Type`, `VoidType`,
  `NewBuilder`) and free `FunctionType`, `PointerType`, `ConstInt`, `InitializeAll*`.
  It does NOT have the free `NewModule`/`Int32Type`/`VoidType`/`NewBuilder` functions.
- User preference: **update the module dependency**, do NOT vendor/commit the local
  `go-llvm/` (it stays gitignored reference-only). A `replace => ./go-llvm` directive would
  break builds for other clones because go-llvm is not committed.

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
- `go env GOMODCACHE` + grep the cached `tinygo.org/x/go-llvm@v0.0.0-...` to check API.
- Regenerate expected files by temporarily adding `os.WriteFile(...)` to the assert in the
  test, running it, then reverting (kept the diff minimal — reverted import-block cosmetic
  change too).

## Final state
- go.mod: require `tinygo.org/x/go-llvm v0.0.0-20260721072906-185673ef46a5`, NO replace.
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

# Session learnings: tiny Go (the gusty interpreter)

This session I implemented `raise`/`try`/`except`/`finally`, generators (`yield`),
list literals, `for`-over-lists, and `len(list)`. Here is what I learned about
this small Go compiler's interpreter (`pkg/lang/jit.go`) and analyzer
(`pkg/lang/semantic.go`).

## Architecture (it's genuinely tiny)
- The "compiler" is mostly an AST interpreter: `pkg/lang/parser.go` builds a
  `Program` of `Stmt`/`Expr` AST nodes; `pkg/lang/semantic.go` is a type
  checker; `pkg/lang/jit.go` is a tree-walking evaluator; `pkg/lang/codegen.go`
  is the LLVM IR emitter (via TinyGo's `go-llvm` bindings).
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
- Always `go test -tags llvm ./pkg/...` before committing; the llvm build tag
  is required (TinyGo's go-llvm bindings).

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
- Lesson: recent AOT features get an ADR (e.g. 0129-classes-aot), NOT a
  versioned CHANGELOG entry — CHANGELOG only tracks milestone releases. Follow
  that convention (ADR 0130-generators-aot, no CHANGELOG edit).
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
  (fmt flags), CHANGELOG, README (fmt flags + docstring note).
- Round 8 hygiene: the formatter had shipped without doc updates; this round
  back-filled roadmap/CHANGELOG/README/operations for `gusty fmt`.

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
