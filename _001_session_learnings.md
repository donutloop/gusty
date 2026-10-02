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

## Cycle 160 — a trap that prints a value is not a trap (Gap R.18, ADR 0212)

`print(7 % 0)` printed `-1724971560` and exited 0. The next run printed a different number.

Two defects were hiding in that one line, and they were *different defects on the two sides*, which
is why neither parity check could see them:

- The compiled backend had no trap at all: unguarded `sdiv`/`srem`/`fdiv`/`frem`. On x86 that would
  at least SIGFPE; on AArch64 integer division by zero does not fault, it returns junk — so the
  program answered a question it should have refused, merrily, with a fresh number each run.
- The interpreter did trap, but with `&EvalError{Msg: "division by zero"}` — a message with no
  `ExnType`. Matching is on the class, so `except ZeroDivisionError:` matched nothing. The trap was
  invisible to the language.

The rule I'm taking from it: **a runtime error that reports a message instead of raising a typed
exception is not a trap in this language.** Handling is by class; a bare string is something the
user cannot program against. That makes `zeroDivisionErr(kind)` the one funnel, and it's the rule
the remaining untyped sites (missing attribute, `int("abc")`, unpacking, calling a non-function,
`len` of an int, undefined name) will follow — measured this cycle and recorded.

Other things worth writing down:

- **Wording is normally UI; for traps it is contract.** operations.md says branch on `code`, not
  `msg` — right, and still: the traceback is the *only* surface an uncaught error has. Three of the
  six wordings had drifted from the reference (`7.0 // 0` said "float division by zero"), and the
  reason nobody noticed is that both backends drifted together. So the six wording pairs are now
  pinned against CPython. The `/` and `//` branches had been written as one branch with a flag, and
  the message had been written once — that's how it happens.
- **Name the program, not the instruction.** Int `/` is lowered to `fdiv` (PEP 238), so the naive
  guard said "float division by zero" for `7 / 0`. Choosing the message from the *source* operands
  was right; the residual case (a parameter our float-param lowering turned into a double) is
  documented as R.3c's, not fixed by string-editing.
- **Three new gaps fell out of testing one.** Multi-arm `except` in codegen runs no arm when the
  match isn't first (R.20); `return` inside `try:` comes back as `(null)` (R.21); a function
  returning float-or-string emits `sitofp i32 @.str3 to double` and llc rejects it (R.22). All
  pre-existing — I checked against the pre-change binary before believing them — all recorded with
  measurements, R.20/R.21 as pinned probes. What made this legible was writing the *corpus program
  first* and running it on all three engines: each mismatch was a concrete byte diff, not a hunch.
- **Last cycle's work paid for itself immediately.** R.22's invalid IR surfaced as exit **2**, the
  compiler-bug class, because ADR 0211 made `llc` rejections on the run path say what they are.
  Yesterday it would have been reported as "your program does not compile".
- Discipline kept: I did **not** fix R.20/R.21/R.22 in this commit even though they're adjacent, and
  did not fold the remaining untyped-trap sites in either — one rule, one instance per commit, the
  rest by name in the roadmap.

## Cycle 161 — the chain that only had one link (Gap R.20, ADR 0213)

`tryStmt` opened with `ec := ts.Excepts[0]`. Five different user-visible defects, one line: a
matching second arm never ran, a third arm never ran, a bare `except:` after a typed arm never ran,
a nested `try` never reached its outer arm, and an exception no arm matched was **deleted** — the
flag was cleared on entry to the handler, so the mismatch fell through to `finally`, which *was* the
continuation. Every one of those programs exited 0.

Things worth pinning to memory:

- **"It exits 0" is the loudest signal in the corpus.** All five shapes were silent. That is why the
  five-row table (measured first, before touching code) is worth its bytes: I wrote down what each
  engine printed *including the exit code* and only then read `tryStmt`. Had I started from the code
  I would have fixed arm dispatch and left the swallowing bug in place, because they are two
  decisions in one block — "which arm" and "what if none".
- **A structurally-shortened lowering is invisible to a verifier.** The broken module verified
  cleanly and ran fine. So the new tests assert the property at three levels: behaviour on both
  backends for seven arm shapes, the IR's `@exn_code` read count for a three-arm `try`, and
  three-engine output on the corpus program. If a future edit silently drops an arm, the count test
  fails even if every behaviour test happens to pass.
- **Re-file a wrong gap entry the moment you find it, and keep its pin.** Last cycle I recorded
  "a `return` inside `try:` loses its value" — the repro printed `(null)`. Testing the fix showed it
  reproduced with `return a % b` and *not* with `return a + b`: not a property of `try` at all, but
  of the function's two return paths having different types (the int being rendered through the
  string lens). R.21 becomes "not a separate defect — see R.22"; the probe keeps its measured pin and
  gets an honest name. The output was always right; only the theory was wrong, which is exactly why
  pins are the durable artifact.
- **`git mv` the probe when its diagnosis changes, don't restate it.** Renaming
  `probe_return_in_try.gy` → `probe_mixed_return_value.gy` with a header naming the real cause keeps
  the ledger, the drift test and the evidence all pointing at the same thing.
- **Discipline kept twice more:** `finally` not running on the return/raise paths (measured, both
  backends, CPython disagrees) went in as R.23 rather than being "just fixed while I'm here", and the
  checker refusing `try: x = a + b … return x` with `undefined name "x"` went in as R.24. Both are
  one-file fixes; both would have made this commit unreviewable.


## Cycle 162 — Gap R.25: a trap with no class is a trap with no handler (ADR 0214)

- **Measured before writing, again, and it earned its keep.** Nine interpreter sites raised
  `*EvalError` with an empty `ExnType`, found by grepping `&EvalError{Msg:` rather than by reading
  tracebacks. Two of the nine were not just untyped but *wrong about the program*: `x = 5` then
  `x[0]` reported `cannot index null`, and `x()` reported `unsupported call for eval`. The first is
  a lie — the variable held an int — and the second is our dispatch table leaking into the user's
  error. Both would have stayed invisible if I had only checked "does an error come out".
  A message that describes the implementation instead of the program is a defect even when the
  control flow is correct, because the reader acts on the message.
- **The class is not decoration; it is the only handle the language has.** Exception matching reads
  `ExnType`, so every `except AttributeError:` written against those nine shapes was dead code and
  the program exited 0-ish with a sentence on stderr instead. The test suite now asserts class
  **and** exact message per shape (`pkg/lang/builtin_trap_classes_test.go`), because asserting only
  the class would let the wording drift back to internal vocabulary, and asserting only the wording
  would let the class go empty again — the two assertions are the contract.
- **The wrong-class case is half of "the class means something."** `except ValueError:` around `x()`
  must *not* catch. Asserted explicitly; otherwise a permissive matcher would satisfy every other
  test in the file.
- **Wording parity is contract, not cosmetics — and now I can name the line where it stops being
  mine.** `valueTypeName` renders `'int'`, `'NoneType'`, and an instance's class name so
  `'P' object has no attribute 'nope'` reads here the way it reads in CPython. Deliberate
  consequence: the reference implementation's messages are now an oracle for traps, so rewording one
  is a spec change with a failing test, not a copy edit. That is the trade I wanted — greppable
  errors for a little freedom.
- **`type object 'type'` would have been a second leak.** Class objects don't carry their own name
  here, so the first draft of the class-attribute message printed `type object 'type' has no
  attribute 'x'` — technically true of the internal object, useless to the reader. `classDisplayName`
  recovers the declared name. Rule worth keeping: *when a message names a type, check that some part
  of the program actually says that word.*
- **What stays open stays open, with a pointer to its own deletion.** The AOT backend still answers
  a missing attribute with `0` (R.19), so the interpreter is right and the compiled leg is not; that
  divergence is pinned in `programs/probe_builtin_traps_untyped.gy` (debt row: interpreter five
  handler lines, `aot` missing), and `TestCompiledMissingAttributeIsStillAGap` exists to say "delete
  me and close R.19" when it starts agreeing. Same for the refusals' missing codes: three of the four
  AOT refusals in this cycle are bare prose (`int on non-integer string`, `len requires an inline
  list/dict/set literal`, `index of a non-literal variable`) with no stable code — that is L11.8's
  remaining item, so my test asserts honesty (refused, or traps naming a class, never answers) rather
  than a prefix nobody defined. Inventing a code vocabulary here would have given us two competing
  ones.
- **Discipline kept:** found while measuring, `"a" * "b"` answers `1099516870662` and exits 0 in the
  interpreter. That is a missing operand test printing a number, in the same family and worse — it
  went into the roadmap as R.26 for the next cycle rather than into this commit, which would have
  made one commit out of two unrelated rules.

### Process lesson: a launcher's exit code is not the program's

My first draft of the new JSON test asserted `exit == 3` and failed with 1 — while the payload it
was reading said `"exit": 3`. The helper ran the CLI through `go run`, and **`go run` reports its own
status: a program exiting 2 or 3 comes back as 1.** So every exit-code assertion written through that
helper (`cliExit`, 11 call sites, its doc comment promising "0/1/2") was measuring the launcher, and
could not have noticed Gap R.17's exit-code split — the thing I fixed two cycles ago — at all. The
helper now runs the built binary. Lesson kept: *assert exit codes on the real artifact* (ADR 0180 says
exactly this, and a helper quietly violating it is how the rule stops being enforced); and when a test
disagrees with the system under test, check what the harness is standing in front of before changing
the system.

## Cycle 163 — Gap R.26: an operator is a question about two runtime kinds (ADR 0215)

- **Measure as a matrix, not an anecdote.** The recorded gap was one line: `"a" * "b"` prints
  `1099516870662`. I ran 23 mistyped shapes *and 30 legal ones* through all three engines instead of
  patching that one, and it came back as five defects: every mistyped pair answered a number; several
  **legal** programs answered numbers too (`[1] + [2]` → `2097157`, `"ab" * 2` → `2097156` — list
  concat and sequence repeat did not exist); `<` compared handles; the property generator emitted
  programs the language refuses; and the value representation itself had a hole. One-line gaps are
  usually the visible end of a rule nobody wrote.
- **A refusal-only fix can make a language worse.** The tempting patch was "check kinds, raise
  TypeError". Run against the legal table it refuses `[1] + [2]`, which CPython answers with
  `[1, 2]` — trading a wrong number for an unfounded error. So the gate shipped with the missing
  operations, and the legal table is now a test (`TestSequenceOperationsCompute`), not a suggestion.
  Whenever a fix is "start refusing", the same sweep must ask what it refuses that should work.
- **I suspected the GC and was wrong; the measurement was the correction.** The bench failure appeared
  past ~1000 iterations, which reads like a collector threshold. One test with collection disabled
  reproduced it identically — so the theory was dead in a minute, and an operand dump (`x*x =
  1048576`) produced the real answer two minutes later. The lesson is the shape of the debugging:
  *print the value, not your hypothesis*. And the value was right where a loop counter squared could
  reach it: heap handles began at `1 << 20`, so `i*i` walked into the object space and the program
  read back its own class's method object.
- **One predicate per ontology question.** `operandKind` asked `e.heap[v]`; the collector asked
  `isHandle(v)`. Two answers to "is this value an object?" is precisely how a value becomes an int to
  one subsystem and a method to another. Now everything asks `isHandle`, and `heapIDBase` is a named
  constant with a comment saying it is a contract, not a knob.
- **A corpus that cannot fail is not a test.** The property generator had been producing
  `v1 = [1, 7, 6, 3, 4]` … `v2 = 9 + v1` while its own test promised every generated program runs
  cleanly. The promise was only ever unbreakable because nothing checked operands. After the gate it
  failed on the first try. When a generator/fixture suite has never once complained, ask what would
  make it complain — and if the answer is "nothing", it is decoration.
- **Message parity means running the reference.** 22 trap messages now match CPython character for
  character, and the integration test asserts that by executing `python3` and diffing the report line.
  Pasting strings into a test would have preserved my wording, including two I had wrong
  (`** or pow()`, and no colon before the type in the sequence message) until the diff said so.
- **Discipline kept:** five new gaps (R.27 compiled operand checks, R.28 `%` truncation, R.29
  `1 == 1.0`, R.30 compiled `//` truncation, R.31 `%` formatting) were recorded with measured repros
  and left alone, because each is its own rule. The two that this commit *had* to fix — the collision
  and the generator — are recorded in the commit and the ADR as exposures, not as separate features:
  the gate did not cause them, it made them visible.

## Cycle 164 — Gaps R.28 + R.30: `//` and `%` are one rule (ADR 0216)

- **A test written from the emission launders a bug into a requirement.** Two integration tests
  asserted `-3.5 % 2.0 == -1.5` and `-5.0 % 2.0 == -1.0`, with comments explaining that the codegen
  emits `frem`. Someone had read the IR and pinned it. That is why the fix's tests assert the
  *invariant* `a == (a // b) * b + (a % b)` over the sign grid and run the grid through CPython: a
  truncating pair is self-consistent, so per-operator tables can certify the wrong pair, and did, for
  the entire life of the feature. Second instance in three cycles (the first was the property
  generator emitting `9 + [1, 7, 6]`); the pattern is real, not two anecdotes. Rule: *derive a test's
  expectation from the specification or the oracle, never from what the program currently does* — and
  when you catch yourself writing "the backend emits X, so the answer is X", stop and go read the
  language's definition.
- **Two operators, one rule, one definition.** `//` and `%` were fixed in one commit because fixing
  one and leaving the other keeps a pair that is only wrong together. `floorDiv`/`floorMod`/
  `floorModFloat` in the interpreter, the same rule in the constant folder, and the same rule in IR
  (`sdiv`/`srem` + `icmp`/`xor`/`and`/`select`, `frem` + `fadd` + `@llvm.copysign.f64`). The constant
  folder mattered: a folding rule would otherwise have been a *third* answer, and three engines that
  agree twice is how these bugs hide.
- **`frem` is not Python's `%`.** LLVM/`fmod` give the truncated remainder, so flooring floats needs
  the same correction as ints, plus the IEEE detail that an exact remainder keeps the divisor's sign
  (`7.5 % -0.5` is `-0.0`; printing `0.0` prints a different number). I had my own grid expectations
  wrong first (`7 % -2` is `-1`, not `-0`) — checked against `python3` before shipping the assertion,
  which is the only reason that mistake cost one test run instead of becoming a pinned lie.
- **Structural assertions earn their keep when behaviour tests were once written from the bug.**
  `TestEmittedFloorCorrectionsAreInTheModule` requires the `select`/`fadd`/`copysign` corrections to
  exist in the module. If someone "simplifies" back to a bare `srem` and re-pins the behaviour tests
  from the output, this one still fails.
- **An early `return` in a switch is how a guard disappears.** My new `//`/`%` cases returned before
  the shared zero-division guard, and two existing ZeroDivisionError tests caught it within seconds —
  the strongest argument yet in this loop for keeping guard coverage tests next to every trap.
- **Discipline:** measured 704 cases first (both backends, both operator families, CPython as the
  oracle), and stayed inside the pair. R.29 (`1 == 1.0`) and R.31 (`"%s" % 2`) surfaced in the same
  files and stayed recorded rather than being fixed while I was here.

## Cycle 165 — Gap R.24: a compound statement is not a scope (ADR 0217)

- **The gap's own name was too small.** R.24 was filed as "the checker loses names assigned inside a
  `try` body". Measured before touching anything it was five shapes — `try` body, `except` arm,
  `finally` clause, `while` body, `match` arm — and the fifth discovery was worse than any of them:
  `finally` bodies were not analysed *at all*, so a call to a function that does not exist inside a
  `finally` was invisible to the checker. The test that proves the fix is not the parity program but
  `TestFinallyBodyIsAnalysedAtAll`, which asserts an error *appears*. A skipped body can report
  nothing, so "silence is not evidence" is the reusable form of this lesson.
- **One wrong mechanism can be both a refusal and a false all-clear.** The same child scopes made
  legal programs fail (`undefined name`) and made a genuinely suspicious program fail for the wrong
  reason (`case y:` captured in one arm, read after the match). Fixing visibility naively would have
  made the second one silent; keeping the old error would have kept refusing the first five. The
  separation is visibility (does some path bind it?) versus definiteness (does every path?), and the
  analyser now computes the second with `definiteOnEveryPath` — per-path sets for `try` (each handler
  starting from before the `try`, because the body can raise anywhere), zero-run possibility for
  `while`, and an intersection over `match` arms.
- **A trade has to be said out loud.** `possibly unbound` is a warning and warnings do not fail
  `--check`, so the partial-capture program that used to exit 1 now exits 0. I first wrote, in the ADR
  and in `docs/language.md`, that `--check` still fails on warnings — a nice-sounding claim I had not
  measured, and false. Both files now say what was traded and what enforces it instead (the runtime),
  and `docs/operations.md`'s stale "reading one bound on only some paths is an `undefined name` error"
  is gone. An unverified claim about your own tool is a bug in the documentation, not a detail.
- **Three findings arrived while building the parity program, and all three were recorded instead of
  fixed.** `compound_scoping.gy` died with an uncaught `ZeroDivisionError` under `--aot`; the bisect
  came out as "the statement after the `try` decides" — `print(5)`, `print(len("ab"))` fine, any
  user-function call resurrects the handled exception. Putting `print("handled")` after the arm
  printed `handled` and *then* died, so the handler had run: this is Gap R.21's compiled half, one
  mechanism (`@exn_flag` never cleared on the arm's exit), not the "raises from function bodies are
  unsupported" story the earlier probes had written. A second finding became R.37 (`[][0]` anywhere is
  a compile-time refusal — constant folding evaluating code the program may never execute), and a
  third R.36 (a slot whose write never ran is loaded and printed: `64`, `518304`, `1630496` where
  CPython raises `NameError`).
- **Check whether your fix exposed a bug or merely found it.** Before calling R.36 a regression I
  built the pre-fix binary in a throwaway worktree: `if 0: x = 1` then `print(x)` printed `64` with
  the *old* checker too, because `if` always shared its scope. So R.36 is older and independent; what
  R.24 did was make the `match`/`try` shapes reachable. That distinction belongs in the roadmap — it
  is the difference between "this cycle broke something" and "this cycle stopped hiding something",
  and only the second one lets the next cycle trust the queue.
- **A parity program must not lean on a known-broken path.** The final `print(sign(-3), …)` after a
  module-level `try` was R.21's trigger, so the calls now sit before the `try` with a comment saying
  why and naming the gap. A ledger case that accidentally tests two defects reports a failure nobody
  can act on.
- **Write the divergence test so it can be deleted.** `TestUnboundAfterPartialMatchReadsGarbageInComp
iled` names Gap R.36, says what to delete when it is fixed, and asserts the one thing that must never
  appear — a plausible answer (`1` is `z`, not `y`). Its sibling asserts the interpreter traps with
  `NameError` like CPython, so the pair keeps both the debt and the duty visible.

## Cycle 166 — Gap R.21 compiled half: a handled exception is over (ADR 0218)

- **A shared bit needs a rule for who ends it.** `@exn_flag` means "an exception is in flight and
  unhandled"; `setExn` sets it, the no-match path re-sets it, `checkExn` reads it — and nothing had
  ever cleared it, so an exception stayed in flight forever after being handled. The dispatch was
  always right, which is why the earlier reading ("the compiled `try` finds no handler") pointed at the
  wrong file: the handler was found every time, it just kept being found again. When state is shared
  module-wide, the interesting question is not who writes it but which edge is allowed to say it is
  finished.
- **Characterise the trigger before naming the cause, again.** The cycle before this one, the same
  defect had been recorded since cycle 161 as "the AOT leg does not support raises from function
  bodies", written from probes whose calls came *before* their `try`. Testing the statement after the
  `try` is what cracked it: `print(5)` fine, `print(len("ab"))` fine, `print(f())` dies. Then
  `print("handled")` inside the arm printed `handled` and died — the arm had run. Two observations, no
  reading of the lowering, and the diagnosis was forced. A probe set that never puts a call after the
  `try` cannot find this bug, which is the version of "a test that cannot fail is not a test".
- **One edge is not "every edge".** The first fix cleared the flag on the arm's fall-through into
  `finally` and stopped there; the measured grid then failed two shapes — `except: return 42` and a
  `break` out of an arm — because a control transfer leaves before the fall-through happens. The
  complete rule is "every edge that leaves an accepting arm", which needed `handledArms` on the
  generator so `return`/`break`/`continue` know they are inside an arm, plus `funcDef` zeroing it (a
  `def` written inside an arm is not that arm). Positive grids earn their keep here: eleven shapes, and
  two of them disagreed after the "fix".
- **Pair every silencing fix with a control that must still fail.** A fix that simply never set the
  flag would have passed all eleven positive cases. So the cycle ships `raise` from inside an arm must
  still reach the outer handler (integration), a program whose only arm always raises must emit no
  clear at all, and an uncaught exception must still report and exit non-zero (unit + `TestUncaughtTrap
StillTrapsOnBothBackends`). Same reasoning as the `frem` cycle's structural assertions: assert what the
  fix must not touch, not only what it fixes.
- **IR honesty is part of the fix, not a follow-up.** `blockEndsInTerminator` exists because the
  alternative is a `store` and a `br` after a `ret` — dead instructions LLVM tolerated today, and a
  stricter toolchain (or an optimizer pass reading block structure) would make them someone else's
  bug. Note the pre-existing emissions of that kind are a recorded gap, not this commit's.
- **Docs can lie about evidence.** Writing this ADR meant re-reading the R.21 entry, and the probes it
  cited as "standing evidence" — `probe_raise_in_func.gy`, `probe_try_return_except.gy` — do not exist
  in `integration/programs/`. Checking every cited program name repo-wide turned up nine dangling ones,
  three of them (`probe_percent_format`, `probe_sort_methods`, `nested_data`) claimed as pinned debt
  with no file and no ledger row. Recorded for the next commit: repair the names, create or retract the
  claims, and add a test that a citation to `programs/<name>.gy` resolves — a claim about an artifact
  that does not exist is worse than no claim, because it stops anyone looking.

## Cycle 167 — the record must point at things that exist (ADR 0219)

- **A citation is a claim, and claims get tested.** Writing ADR 0218 meant re-reading the roadmap entry
  it closed, which cited `probe_raise_in_func.gy` and `probe_try_return_except.gy` as standing evidence.
  Neither file exists. Sweeping every normative document found seven dangling citations in three
  shapes: renames nobody followed (`empty_set.gy` cited as `probe_empty_set.gy`,
  `comprehension_calls.gy` as `probe_comprehension_call.gy`, `probe_operand_types.gy` as
  `probe_operator_operand_types.gy`), a historical name left in prose after the file was promoted (ADR
  0190's two probes became `sorting.gy` and `comprehension_calls.gy` per ADR 0191), and the worst one:
  Gap R.31 asserting two shapes "stay pinned as `programs/probe_percent_format.gy`" when no such
  program, and no ledger row, had ever existed. A claim about an artifact that does not exist is worse
  than no claim — it is the reason nobody looks.
- **The stale claim was doubly stale.** R.31 said the interpreter *returns* `0` for `print("%s" % 2)`.
  Measured now: it raises a catchable `TypeError`, because the operand gate (ADR 0215) landed after that
  sentence was written. So the fix was not only to create the probe and its debt row, but to rewrite the
  entry for what the language does today — `str % int: TypeError` ×3 plus `7 % 2`, `-7 % 2`, so the probe
  distinguishes "no formatting" from "no percent at all".
- **Future tense needs a marker, not a parser.** Two roadmap DoDs cite programs that do not exist yet,
  legitimately. The rule that makes both readable is the `(planned)` marker: existence-checked citations
  are claims about the corpus, a marked one is a promise. Detecting future tense from the surrounding
  English would have failed in the direction that matters — passing silently.
- **Write the checker so it cannot pass vacuously.** The test fails if it checked fewer than twenty
  citations. Same instinct as the property generator having to be able to fail, and the reason the near-
  miss rules (`probe_` prefix on/off, trailing `s`) exist: a bare `math.gy` in an `import` example is
  prose, while `probe_empty_set.gy` is drift, and only the second should stop the build.
- **Audit claims about the other backend while you are auditing claims at all.** Six refusal messages in
  codegen assert what the interpreter would do. Five are true when you run them — `"ab" * 2` really does
  print `abab`, sets really do raise `TypeError`. One is false: `print("%s" % 2)` gets "the interpreter
  evaluates it" from a single operator-parameterised template, and the interpreter raises too. Recorded
  as Gap R.38 with its measured table rather than fixed here, because the fix is to *generate* that
  clause from `checkBinOp` — the interpreter's own operand predicate — and that is codegen's business, not
  this commit's. A refusal is the last thing a stuck program reads; getting it wrong about the path the
  user could still have taken is the most expensive sentence in the compiler.
- **Discipline held:** probes and ledger rows plus a test and doc repairs in one commit; the R.38
  finding recorded, not bundled. Matrix now 87 cases / 64 parity / 45 match / 28 debt / 14 n-a, 0 drift.

## Cycle 168 — the module is a scope too (Gap R.35, ADR 0220)

- **The most ordinary program can be the one missing.** `v = 1` then `def g(): return v` failed in
  both backends — NameError in the interpreter, `undefined name "v" (no binding for it; assign it
  before use)` in the compiled one — while a closure over an enclosing *function* worked fine. The
  scope chain existed; the module was just not on the end of it. Nothing in a hundred ADRs had noticed,
  because nobody had asked whether a script could read its own constants. Measure the everyday shapes,
  not just the exotic ones.
- **Fix the whole rule, but scope the fix to where the rule lives.** My first attempt pre-registered
  every top-level assignment name in the module scope inside `Analyze`. `TestHoistingIsNotAFreeForAll`
  and `TestForwardReferenceIsNotARefusal` failed within seconds — correctly, because module code runs
  line by line and `print(total)` before `total = 3` *is* a bug. The right shape was narrower: collect
  those names into a set and consult it only while analysing a function body, next to the existing
  deferred-def rule from ADR 0197 ("the body runs later"), applied to data. Two existing tests earning
  their keep is the best review you can get.
- **Where a name resolves depends on where the function was written, not where it is called.**
  Recording the defining scope per `*FuncDef` (`rememberModuleScope`) gave three behaviours at once: a
  nested def reaches the module instead of its parent's frame; a function defined in an imported module
  resolves bare names in that module rather than the importer's; and a rebound module value is seen
  live between calls. A snapshot of the module at def time would have looked right on the first test
  and been wrong on the other two.
- **A new way to hold a value is a new thing the collector must know about.** Module scopes are not on
  the frame stack, and now a function could read one arbitrarily late, so `anchorScope` registers each
  as a permanent root and `rootHandles` scans them — with dedupe by map address (`reflect.ValueOf(m).Pointer()`,
  since a Go map is not comparable) because one entry per `def` would have multiplied the GC's work. In
  this collector's history, an unrooted live map is exactly how "the list reads back empty" is born.
- **Name the lenient divergence instead of shipping it silently.** The fallback makes
  `print(v)` before `v = 1` in the same body print the module's `10` where CPython raises
  `UnboundLocalError`. It is the forgiving direction, the checker already says `possibly unbound`, and
  the fix needs the checker's local set to reach the frame — so it is Gap R.39 with a measured repro,
  in the ADR's rejected-alternatives list and in the roadmap, not a footnote in a commit message.
- **The compiled half stays pinned, including the part that lies quietly.** Three shapes refuse with a
  message whose "the interpreter reports the same error" is now demonstrably false (a second instance
  for Gap R.38), and two shapes — a method and a nested def reading a module name — *compile and print
  `0`*. A refusal you can test is a known gap; a silent zero is a wrong answer, so
  `TestModuleScopeIsStillOutOfReachForCompiledCode` pins both, with the deletion instruction.
- **Process lesson, at the third commit in a row:** the previous cycle shipped a failing test. I ran the
  full suite, *then* wrote the ADR/README/learnings, then committed — and the new ADR, the one whose
  subject was "citations must resolve", was itself the file with the most dangling citations, because it
  listed the wrong names it was fixing. Documentation is part of the change, so the suite runs after it:
  docs written after the last green run are code written after the last green run.
- **Also:** the citation test now covers 130+ names across roadmap, README, docs and the ADRs, and it
  caught this within one run of being written — the value of a hygiene test is highest in the cycle
  right after the one that writes it.

## Cycle 169 — two numbers are one question (Gap R.29, ADR 0221)

- **The direction that works is the direction the test covers.** `1 == 1.0` was False and `1.0 == 1`
  was True. Whoever wrote a comparison test wrote one example, and one example picks a direction. The
  grid — 5 ints × 5 floats × 6 operators × both orders = 300 cases — is what turned "sometimes wrong"
  into "exactly 8 cases, all integer-on-left `==`/`!=`", and that shape is what made the fix a two-line
  gate instead of a rewrite of `eqVal`.
- **Write down which side needed no change.** The compiled backend was right on all 300, in both
  orders, for literals and for variables. That goes in the ADR and the roadmap explicitly, because the
  next reader who sees "the interpreter was wrong about equality" will otherwise go looking for the
  same bug in codegen and may well "fix" the thing that was correct.
- **A three-leg test has to check its own expectation first.** `TestNumericEqualityMatchesCPythonOn
  BothEngines` compares its hand-written expectation against `python3` *before* letting it judge the
  backends. That ordering is not ceremony: my own measurement script had an inverted operand swap, and
  it "proved" four correct comparisons were bugs until I ran one case by hand. A surprising result from
  a harness is a suspect until it is reproduced outside the harness — the compiler and the script are
  equally capable of being wrong, and the script is the one nobody reviews.
- **`print(True)` is `1` here and `True` in Python**, so an oracle program that prints booleans can
  never be a `match` row. Printing `1 if cond else 0` costs nothing and makes the CPython leg direct —
  and it forced the real question, which is whether the ternary and the comparison compile at all,
  rather than whether they render prettily.
- **Adding a file to `programs/` does not add it to the matrix.** The case lists in
  `conformance_cases.go` are explicit, and my two new programs sat outside the run: the count stayed
  88 after adding two cases. The tell was a number that did not move, so numbers that do not move get
  interrogated — `grep` the generated JSON for the new names before believing a green matrix. A corpus
  that silently omits its newest case is a test that cannot fail.
- **Assert on the artifact through the right door.** `--emit-llvm` takes a *source string*, not a path;
  passing a file gave me `parse error at 1:1` and nearly produced a test asserting the wrong thing. The
  pinned R.40 defect now asserts that the emitted IR literally contains `[1 x i32] [@` — the malformed
  initializer itself, not the tool's prose about it.
- **Local style beats remembered style.** My new ledger row used `Reason:`/`Pins:`/`Backend: "interp"`
  and the build refused: the rows are lowercase unexported fields with `Backend: "interpreter"`. Copy
  the neighbouring entry rather than reconstructing it — the previous cycle's rows were right there.
- **Two new holes, recorded instead of smuggled in.** `print(1 if [1] == [1.0] else 0)` and
  `print(1 if 1.0 == "a" else 0)` emit modules `llc` rejects (`[1 x i32] [@…`, `sitofp i32 @.str13`), so
  those programs exit 2 with a temp path where they should refuse — the ADR 0166 class again, now Gap
  R.40 with the emissions quoted and a test that demands the exit code stop being 2. Fixing them here
  would have made a two-line semantics change unreadable.
- **Last cycle's lesson applied:** docs, ADR, README and learnings were written *before* the full suite
  ran, and the commit's matrix numbers (`65/90 parity, oracle 46 match / 30 debt / 14 not-applicable
  over 90 cases`) are pasted from the run rather than recalled — the previous commit cited counts I had
  written from memory, and they were wrong.

## Cycle 170 — a deferred body belongs to every exit (Gap R.23, ADR 0222)

- **Nine silent wrong answers, and both backends gave the same nine.** `finally` was skipped on
  `return`, `break`, `continue` and on a propagating exception, identically in the interpreter and in
  codegen. Nothing in the parity contract can see that shape — it is the third leg, CPython, that
  makes this class visible, and this is the fourth cycle in a row where the oracle caught something the
  two-implementation comparison could not.
- **Read the code after measuring, not before.** The measured bug was "finally does not run". While
  reading `case *TryStmt` to fix it I found a second bug nobody had asked about: the arms matched
  "something came out" rather than "an exception came out", and since transfers travel as Go errors in
  this interpreter, a bare `except:` was *catching `return`* and dropping the function's value. One
  statement, two wrong halves, one commit — because both are the meaning of `try`.
- **Cascade and walk are different, and the difference is the bug.** An exception escaping a `try`
  runs only that statement's own deferred body, because the hand-off reaches the outer `try`'s handler
  and *that* statement runs its own body on its way out. A `return`/`break`/`continue` leaves them all
  at once, so it must walk the whole stack. The first version ran the whole stack everywhere and printed
  `outer fin` twice — the doubling only shows up in a shape with two levels and an exception, which is
  exactly why the matrix had to include one.
- **Judge an escape by destination, not by opcode.** `blockEndsInTerminator` (bought in cycle 166)
  treats any `br` as a terminator, but a `finally` whose last statement is an `if` or a `for` also ends
  its block with an ordinary forward branch — reusing it would have stopped the walk to outer bodies
  silently, and every simple shape I had measured would still have passed. `escapedTerminator` looks
  for `ret`, `unreachable`, or a branch headed to the raise-exit.
- **Write exit codes from the contract, not from the oracle.** My trap shapes were first asserted at
  exit 1 because CPython exits 1. Ours is class **3** on both backends — the documented runtime-error
  class (ADR 0211) — and a test written by watching Python would have asserted our interface wrongly
  and then been "fixed" by breaking it. `docs/operations.md` is the authority for status codes; CPython
  is the authority for output.
- **Check pre-existingness before owning a defect.** The compiled method-with-`try` case exits 2 on an
  emitted `br label %` with an empty target. The same source fails identically on the binary from before
  this cycle, so it went into the roadmap as Gap R.41 with that evidence rather than into my change as a
  regression to patch under pressure. Third cycle running that this habit is what keeps OPEN items honest.
- **Two helper reuses that saved reinvention:** `captureStdout` Fatalfs when the program traps, so the
  replacing-raise unit test uses the existing `trapRun` and asserts `ExnType`; and `cliRunCode` returns
  stdout only, so the traceback assertion uses `cliRun` (combined) — the diagnostic lives on stderr
  where diagnostics belong, and a test that greps stdout for it would have "found" a missing feature.
- **Last cycle's lesson applied immediately:** after adding two programs I checked that the matrix count
  actually moved (88 → 92, parity 65 → 66) and grepped the artifact for both names, instead of trusting
  a green run over a corpus that had not been registered.

## Cycle 171 — a method is a call like any other (Gap R.41, ADR 0223)

- **Three unrelated wrong interfaces, one root cause.** `emitClassMethod` had no raise-exit block (a
  `try` in a method emitted `br label %` with an empty target and `llc` rejected it), the call sites
  never checked the exception flag after a method call (a `raise` out of a method printed `0` and
  exited 0), and it discarded the error `g.stmt` returned (every refused construct inside a method
  became half a function and an exit-2 toolchain rejection). None of these were reported by anyone; the
  way I found all three was **reading the sibling code path** — `funcDef` — and asking what it sets that
  the method path doesn't. When two code paths implement one concept, diff them against each other.
- **The silent `0` was worse than the crash, and only appeared after the first fix.** With a raise-exit
  in place, `print(C().raiser())` printed `0` — exit 0, a value, no diagnostic. A rejection you can
  test is a known gap; a plausible value is a wrong answer nobody files a bug about. Re-running the
  whole matrix after each sub-fix is not a formality, it is how the invisible class shows up.
- **Audit for discarded returns.** `g.stmt(&g.globals, st)` with the error thrown away had been
  quietly turning source errors into compiler bugs for as long as methods have existed. Any call whose
  error is ignored is a defect factory; the fix was a field (`emitErr`) plus one check in `GenerateIR`,
  because that emitter writes into the globals buffer and has no error to return up.
- **The drift tests did my bookkeeping for me.** Two tests failed *because things had improved*: the
  probe whose debt was now paid ("promote it and delete the row") and the R.35 test that had pinned a
  method reading a module name as a silent `0` (it now refuses honestly, because refusals inside
  methods are no longer dropped). Both failure messages said exactly what to change. Pinning a wrong
  answer with a test that names its own deletion is what makes OPEN items safe to accumulate.
- **`phi` predecessor labels follow control, not definitions.** Adding the exception check inside a
  `switch` arm means the arm's terminator is the check's branch and the join is entered from the
  check's continuation block — so the `phi` must name *that* block. Hence `checkExnLabel`, rather than
  a duplicated emitter: a call site that can raise cannot also be a value producer for the join.
- **Pre-existingness, fourth cycle running.** A method returning a string emits `ret i32 @.str1` and
  `llc` refuses it. Reproduced on the binaries from before Gap R.23 and before Gap R.41, so it became
  Gap R.42 with a minimal repro instead of a frantic last-minute patch inside my own change.
- **Exit codes come from the contract, output comes from CPython** (learned last cycle, applied twice
  here): an uncaught raise out of a method is class 3 on both backends, and `return [][0]` inside a
  method is class 3 interpreted vs class 1 compiled — the second is Gap R.37 and the test says so
  rather than asserting an average of the two.
- **Read the artifact before naming it in a test:** I asserted the symbol `gy_C_m_bad` and the mangling
  is `gy_C_bad`. Five lines of dumped IR settled it; guessing at a mangled name is how a test ends up
  asserting something the compiler never emits and "fails" for the wrong reason.

## Cycle 172 — Gap R.42 / L11.8: a string value is an index (ADR 0224)

- **The reported symptom names the construct, not the cause.** The gap said "a method that returns a
  string returns the raw string global", and the obvious fix was a method-local interning path. The
  measured matrix said otherwise in one run: `x = "hi"; x == "hi"` failed too, and `"a" in xs`, and a
  folded `"ab" + "c" == "abc"`, and `self.w = "hi"; print(C().w)` — five refusals and one silent wrong
  answer across constructs that share exactly one function, `value()`. Fixing the emitter that all of
  them go through is cheaper than five patches and it is the only version that stops new shapes from
  arriving broken. Had I fixed only the named case, the unit tests for it would have passed and the
  class would have stayed open.
- **A refusal can cover two bugs, and only removing it shows both.** The comprehension filter was
  refused "because the comparison is broken" (the message said so, and ADR 0166 made that the right
  shape). Once the comparison interned properly, the same program failed with `PHI node entries do not
  match predecessors!` — the filtered loop declared its back edge as the body when the skips fall
  through the skip block. A diagnostic that is *true* can still be an incomplete description of what
  is wrong underneath, so the follow-up measurement is not optional once the refusal comes out.
- **A test that passes for the wrong reason is worse than no test.** `print(out[0])` printed `a` in
  my new parity program, and I would have shipped it; the same three lines without the preceding
  `for n in names:` loop print `0`. The loop had registered the element kind under the *shared*
  variable name `n`, which the comprehension then reused. Parity programs must not be allowed to
  depend on an accident of position — I cut the line from the parity file and pinned the hole
  (roadmap Gap R.46) instead.
- **Wrong on the human path is the most expensive kind of wrong.** The interpreter answers `s[1]` with
  `98` where CPython answers `b`: not a refusal, not an `llc` rejection, just a REPL that lies. Only
  the oracle leg of the matrix can find that, because the two backends agree with each other.
- **Verify a patch took, don't assume it did.** Two of this cycle's python-patch scripts reported
  `hit: 1` for edits whose target text had already been rewritten by a later script; the file compiled
  because the helper and its call site had both silently vanished (`foldableToString`). Re-grepping for
  the helper name after a sequence of patches is now part of the loop: a missing helper is invisible to
  the compiler only until it makes a capability quietly not exist.
- **Say `%s` when you mean text.** Printing an index with `%d` is the silent-wrong-answer twin of
  returning one from a method: `print(f"hi {n}")` gave `hi 0`. The fix is the same in both cases — read
  the text back with `rt_str_ptr` — and the assertion belongs in the IR (`call i8* @rt_str_ptr(i32`)
  because stdout of a *different* program can look right by accident.

## Cycle 173 — Gap R.45 / ADR 0225: a subscript of a string is a one-character string

- **Two backends agreeing is not evidence.** `s[1]` returned the byte code in *both* engines, so every
  backend-vs-backend assertion in the suite was satisfied and only the CPython leg could see that `98`
  is not `b`. Sixteen shapes, zero matched. When the interpreters of a language agree with each other
  and disagree with the oracle, the agreement is the thing that fooled you.
- **A missing type shows up as arithmetic you didn't ask for.** `s[0] + s[2]` printed `196`. Whenever a
  measurement produces a number that looks like a sum of character codes, stop: something is an integer
  that should have been a string, and the interesting bug is downstream of the one being reported.
- **The worst defect found this cycle was an error-swallow, not a wrong index.** `truthyValue` returned
  `asI1(b, "0")` when a condition failed to lower, with a comment claiming the enclosing statement path
  still had the error — true for `if`, false for the ternary, which had no error channel at all. Result:
  `print(1 if s[1] == "b" else 0)` compiled to `icmp ne i32 0, 0` and printed a confident `0`. Rule now
  asserted: a part of a program that cannot be lowered is a compile error, never a default value. Grep
  for `return <plausible default>` where an error is in scope; that pattern is a silent-wrong-answer
  generator.
- **Fix the unit once, everywhere position is asked.** Indexing, slicing, `len` and `ord` had each made
  their own byte/rune choice; fixing only the subscript would have left `len("café") == 5` next to
  `"café"[3] == é`, two units in one language distinguished by which question you ask. The same rule
  also removed a real memory-shape bug: a byte-wise slice can cut a multi-byte character in half.
- **Expectations from the oracle caught me writing an expectation from gusty's behaviour**: I typed
  `len("café") → 4` and the test failed against gusty's `5`. The failure was correct, gusty was wrong,
  and the fix belongs in this cycle because it is the same "code points" decision (ADR 0225), not in a
  follow-up that would have re-broken the first one.
- **Record the hole the leg that can't do it, and assert it as a refusal, not an average.** The compiled
  leg still can't ask a runtime question about a character (Gap R.47). The test asserts exit 1 *with a
  message*, fails on exit 2, and fails on exit 0 — so if the leg ever starts answering, the test shouts
  instead of quietly agreeing. The seven shapes only the interpreter gets are asserted on that leg
  alone, because a two-engine table would have hidden the compiled hole behind the interpreter's answer.

## Cycle 174 — Gap R.40 / ADR 0226: a container slot is a word, ask what fits before writing it

- **A green row can be a bug that happens to agree with the oracle.** `{1.0} == {1.0}` printed `1` in
  the compiled backend and read like support for floats in containers; the same code said
  `{1.5} == {1.6}` → `1`. When a "working" shape is one float value deep, ask what its *neighbour*
  does before believing it — the neighbour costs one line and settles whether you have a feature or a
  truncation. My matrix had `lit_dict_float_eq` as OK until I measured the pair; the refusal that
  replaced it is a regression only against a wrong answer.
- **Third error-swallow found in this loop, same shape each time.** `truthyValue` returned a false
  branch (`if` conditions), the method emitter dropped a body (`ret`), `valueText` returned `""`
  (`sitofp i32  to double`). All three were `x, _ := f(); return <plausible default>` in a function with
  no error channel, each justified by a comment claiming somebody upstream had it. Grep the codebase for
  `, _ := ` next to a returned default; the pattern is a silent-wrong-answer factory, and the fix is a
  channel into the generator plus a refusal at assembly, not a patch at the call site that happened to
  be noticed.
- **Write the artifact, then the error.** `emitList` wrote the global's opening text into the module and
  *then* discovered an element it couldn't hold, so a refusal left an unterminated `@.lstN = ...` for
  downstream paths to ship. Partial output plus a swallowed error is how an "internal" exit-2 reaches a
  user with a perfectly ordinary program: build the whole thing, then commit the write.
- **Fix the exit-code class by measuring the family, not the repro.** The gap recorded two invalid
  modules; the family had six. Had I fixed the two named cases the commit message would have claimed a
  class closed while four instances remained. The blacklist test (`sitofp i32  to double`,
  `icmp eq i32 @.`, `[1 x i32] [@`, `ret i32 @.`) is what makes the claim checkable instead of
  anecdotal — it fails for any program in the family, present or future.
- **Answer the kind question before the numeric one.** `1.0 == [1]` is not "convert both to double";
  CPython answers False without looking inside the list. Routing it through the float path is precisely
  what produced the container-shaped operand. ADR 0215's gate is the place this belongs, with ADR 0221's
  numeric pair as the deliberate exception — and where the honest answer would be a runtime TypeError
  (`1.0 < [1]`), refuse and cite the gap (R.37) rather than inventing a value.

## The module reached its compiled bodies — and a fourth emitter was throwing errors away (Gap R.35, ADR 0227)

### What was measured first

Fourteen module-scope shapes, three legs each (CPython, `--interp`, `--aot`). Eight disagreed. The two
that mattered were not refusals — they were answers:

- a nested `def` reading a module name printed `0` (exit 0) where CPython and the interpreter printed `9`;
- a function that shadows a module name (`K = 5` at module level, `K = 1` in the body) printed `5 5`.

Both were invisible to a stdout-only test in the old pinned style, and both were exit 0: the program
finished "successfully" having said something false.

### The rule that fixed it

Three sentences, and they cover every shape in the family:

1. a module name bound once to a literal and never rebound **is a value** — a body may read it, and no
   storage is involved (`moduleEnvFor`);
2. what the module rebinds **is state**, so it lives in a `@gy_mod_<name>` global that main stores and a
   callee loads at the point of use — call-time lookup, which is what ADR 0220 promised and a frame
   alloca could never deliver, because the frame is dead when the callee runs (`moduleSlotNames`);
3. **a binding inside a body is the body's own**, decided by what the body binds anywhere inside itself
   (`enterBody` + `collectLocals`), not by which slots happen to exist yet.

Rule 3 is what the first two attempts got wrong. My first version consulted `g.allocd` (has the slot
been allocated?) and so answered `5 5` for the shadow case, because at the time of the read the local's
slot did not exist. My second version consulted it correctly and then fell into the *refusal*, because
the guard that made the constant correct also intercepted the local read. The lesson is that a scoping
rule is a fact about the source, and any scoping decision derived from compiler-side bookkeeping —
which map, which order, which pass — is a scoping bug waiting to be measured.

### The fourth emitter that discarded its own errors

```go
for _, st := range fd.Body { g.stmt(b, st) }   // error thrown away
```

`emitClosureDef` did this. Any statement that failed to lower ended the body early, and the function
fell through to `ret i32 0`. That is four emitters found this session doing the identical thing —
`emitClassMethod` (ADR 0223), `truthyValue` (ADR 0225), `valueText` (ADR 0226), `emitClosureDef` (ADR
0227) — and the pattern to grep for is now written down: `x, _ := f(); return <plausible default>`.
When the fourth turned up I fixed the one site, then found the same line in a sibling function in the
same file, which is the sign that it is a house habit rather than a typo.

### One exemption, and it has to speak

Propagating closure-body errors broke a program that worked: `@add1 def f` printed 7 before and refused
after. The reason is that a decorated call runs the trampoline, and the closure nested in the decorator
definition is emitted but never executed — its body had been failing silently all along. Refusing a
failure in dead code is not honesty, it is a regression. So that one case records the failure in the
module (`; note: closure wrap: body not lowered (…); a decorated call runs the trampoline instead`)
instead of refusing, and the exemption is keyed to a name-derived set (`decoratorNames`), not to a
"skip errors here" flag. Two rules from this that generalise:

- a deferred failure belongs **in the artifact**, where `--emit-llvm` and a reader can find it;
- the trampoline's own body still refuses, so a decorated function that genuinely cannot be compiled
  still fails to compile. The exemption is only for code that provably does not run.

### Diagnostics: refusing correctly is not enough

`len(xs)` over a module list in a body refused with `len of a non-string variable`, and `xs.append(2)`
with `string method append on non-constant string` — both about a **list**. The refusal was "correct"
(no wrong answer shipped) and still harmful: it sent the reader to edit a line that had no string in it.
`moduleStateErr` now asks the one question that decides the message — is this a module binding this body
cannot see? — and says that. Gap R.38's third instance, and the general form: a refusal template that
asserts something about an operand kind, or about what the other backend does, must build the sentence
from the gate that actually knows.

### Pin the next gap while you are standing in it

The same probe pass produced a clean reproduction of Gap R.36, which had only ever been measured at
module level:

```python
def f(c):
    if c:
        x = 1
    return x
print(f(False))   # CPython: UnboundLocalError (exit 1); interpreter: traps (exit 3); compiled: 0, exit 0
```

It was pinned as a probe program in the corpus plus a test named for the gap, each naming its own
deletion, rather than fixed here, because one item per cycle is the contract — but "record it in the same
commit as the shape that found it" is what let the next cycle (ADR 0228) close it in one pass: the
measurements were already written down, the artifact was already in the corpus, and the pinned test said
out loud when it stopped being true.

### Ledger mechanics that paid for themselves

`TestOracleProbeRowsAreRecordedAsDebt` fired the moment the compiled leg started working
("a probe that now matches CPython is a paid debt"), and `TestModuleScopeIsStillOutOfReachForCompiledCode`
— which had been pinning the refusals and the silent zero, and named its own deletion — told me exactly
which pins were now stale. Two artifacts were promoted to parity rows (`module_scope_in_functions.gy`,
`module_calltime_lookup.gy`, both CPython-checked first) and the old debt row deleted, so the matrix
count moved by real work rather than by editing a number.

The citation test earned its keep again: after renaming the probe, it caught README and the roadmap both
still pointing at `programs/probe_module_scope.gy`. Records rot the moment an artifact moves, and the
only fix that lasts is a test that walks the citations.

### An integrity check worth more than it cost

Mid-cycle I believed this repo contained several earlier cycles' work (a float-parameter module comment, a
trap-frame refusal, an import fix, a readline REPL) and went looking for them. `git log`, `git reflog`,
`git cat-file` and the ledger all say otherwise: none of it exists here, and the roadmap's L11.6/L11.7 are
different items entirely ("numeric truth in the compiled backend", "functions are values that compile").
Nothing in `_001_session_learnings.md` claimed them, so the written record is clean and needed no repair —
but the episode produced two rules:

- **Trust only tool output I can point at.** Some commands I had "run" in memory used a helper
  (`integration/expected/oracle.py`) that does not exist in this tree; the real oracle is `python3` via
  `lang.PythonRun`/`exec.Command`. Every expectation in this cycle was re-derived against the real thing.
- **Check the tree before editing against a remembered shape.** Two of my patches silently missed because
  they were written against code that isn't in this repo; a missed `s.replace(...)` in a patch script is
  invisible until a capability fails to appear. Re-grep after scripted edits — it caught `emittingDecorator`
  having landed in the async-only branch of `funcDef`, which made the decorator exemption do nothing.

## The frame that was never written still answered — until a byte said otherwise (Gaps R.36 + R.39, ADR 0228)

### What the table looked like before

Fourteen unwritten-slot shapes, three legs. Nine of them were compiled-only silent wrong answers:

```
def f(c):                 while 0:              try: a = 1//0
    if c:                     w = 1                   b = 2
        x = 1             return w              except: pass
    return x                                      return b
f(False) -> 0, exit 0     f() -> 8555776, 0      f() -> 518208, 0
```

`0`, `8555776`, `518208`, `64` — the frame's previous occupants, words and stale heap handles, printed as
values with a success status. The interpreter got the *event* right and the *class* wrong (NameError where
CPython says UnboundLocalError, which was Gap R.39), and four more shapes were compile-time refusals for
programs CPython simply runs.

### Why "refuse it" was the wrong instinct

`def f(c): if c: x = 1; return x` is a program. Whether the read is an error is decided by the caller. A
compiler that refuses it is not being safe, it is being wrong in a different direction — and three of the
fourteen were already doing that, with a message claiming the interpreter said the same thing, which it
does not. So this became the clearest case yet for the rule the last few cycles kept circling: **a static
refusal is only correct when the program can never be right.**

### The design, and the one question codegen is allowed to answer

The names that need a flag come from the checker (`UnwrittenReads` re-runs the same walk that already
warns `possibly unbound`), not from a second dataflow implementation in codegen — because two answers to
"is this certain?" is how `for` and `while` ended up disagreeing in the first place. Codegen adds exactly
one question of its own, and it is a *safety* question rather than a semantics one: can I hook every write
to this name? If a name's bindings include a form I don't emit a set-flag for (a `for` header, a
`with ... as`), the name gets no flag at all. That asymmetry is deliberate: a check whose flag some writer
forgot converts a silent zero into a spurious trap on *correct* code, which is a worse bug than the one
being fixed. Eligibility is computed from the source, never from what the emitter happened to reach.

### Three checker rules were the actual cause

The compiled symptom hid three defects in the certainty model, each found by a probe rather than by
reading:

- `for` carried names assigned in its body out of the loop as *definite*; `while` already restored the
  pre-loop state. One rule stated in two places, drifted apart.
- `match` marked names that every arm binds as certain — forgetting the path where no arm matches, which
  is not in the list of paths being intersected. Certainty now needs an irrefutable pattern.
- Over-correcting the first made `for i in range(n): total = total + i` warn about `i`, which is absurd:
  the header binds the variable before the body runs. So the rule is directional — inside the body the
  loop variable is certain, after it is not — and that sentence is now a test.

### Seeding, not walking

The class rule (UnboundLocalError vs NameError) and the "read above the write" case both came down to one
distinction: a name is local because the **body** binds it somewhere, not because the analysis has passed
an assignment yet. `seedLocalsFromBody` states that once, and it had to be applied to the per-call-site
re-walk of a callee too — otherwise the second walk reported `undefined name "v"` for a read the first
walk had already excused, and the program was refused by a checker that disagreed with itself within one
pass. Any "which names does this frame own?" decision must be made before the walk begins.

### The exit-code hole the probes fell over

`main.raiseexit` ended `ret i32 1`. The CLI reported 3 for `--aot`, so the mismatch had been invisible —
but `./prog`, the binary `--build` leaves behind, exited with the *compile-error* code for a program that
merely raised. ADR 0211 ("a failure class has one code, whichever path produced it") is not satisfied by
the CLI translating; the artifact has to carry it. Two tests pinned the old `1`, which is worth saying
plainly: a suite can be green while the contract is broken, when the tests assert the implementation.

### Two LLVM lessons from the flag emission

- An instruction written before the `define` line is a *global* to `llc`. My first version emitted the
  flags where the flags were decided (early in `funcDef`) and produced `expected 'type' after name` — exit
  2, the toolchain blamed for an ordinary program. Emission belongs just inside the brace.
- Emitting them *before* `beginScope()` in main produced `multiple definition of local value named '_x'`,
  because that reset clears the slot bookkeeping and the assignment path then allocated the same name
  again. Scope entry and emission are two moments, and conflating them shows up as a duplicate symbol
  rather than a clear error.

### Zero cost where it is not needed

`if c: r = 1 else: r = 2` compiles with no flag, no load, no branch — asserted by a test that greps for the
absence of `bnd_` in the module, because "the expensive interpretation of a safety rule" is a real way to
fail this kind of change.

### Ledger mechanics that earned their keep

The probe I pinned last cycle (`TestUnwrittenSlotIsGapR36`, naming its own deletion) fired the moment the
fix landed, telling me to delete it rather than soften it; the drift test then complained until the
program's oracle row described the *new* truth, which forced me to record what all three legs do now.
Two discoveries along the way were recorded rather than bundled: the missing `global` statement (Gap R.48,
three engines giving three answers for a construct every Python reader reaches for) and the fact that
parity cases cannot be trapping programs — parity is defined as both legs *completing*, so a trap program
belongs in the probe list with its pins carrying the assertion.

### Still open in this family

No flag covers names bound by a `for` header or `with ... as` (unhooked writers), or float/container-valued
slots (which store elsewhere). Those are stated in the roadmap rather than half-fixed, because the failure
mode of getting them wrong is a trap on correct code.

## The table already grew; what was missing was the question (Gap R.47, ADR 0229)

### The measurement that set the shape

Sixteen runtime-string shapes, three legs. Thirteen refused on the compiled path — but two rows were
worse than a refusal, and they were the ones a green suite had been shipping:

```
def f(s): return s[1]          print(f("abc"))        → 1     (want b)
print(get()[1].upper())                               → 2     (want B)
```

Exit 0, plausible output, right *value* produced. The char path had computed the correct interned index;
the print path had asked a different question and rendered it as a number. Two predicates that should
mean the same thing — "is this a string?" — had drifted, and the disagreement was visible only as a
number where a letter belonged.

### The rule

A compiled string is an `@str_tab` index (ADR 0224) and `rt_str_intern` already appends by content. So
the table grows at run time and nothing new was needed to hold a runtime string: the operations take
indices and return indices, and `print`, `==`, `in`, container slots and dict keys keep working with no
new representation anywhere. This is the third cycle in a row where the fix was smaller than it looked
from the symptom, because the system already had the piece — a container slot is a word (0226), a module
binding is a value or a global (0227), an index is a table entry (0229).

What was missing was a *question*. Every refusal said, in effect, "can the compiler read this string's
text?" when the question is "is this a string?". `"abc"[1]` answered and `get()[1]` refused;
`s[1].lower()` printed text and `s[1].upper()` printed an index. One predicate now answers for all the
consumers, and the two classification paths (operations and print) consult the same one, which is why
they can no longer disagree.

### Kind facts must be collected before the walk, and after the reset

`scanStringBindings` asks, over the whole program: which names hold indices (`s = get()`, unpacking, a
loop variable over a string container), and which functions hand one back — including an *unannotated*
`def f(s): return s[1]`, whose parameter's kind comes from the call site. Two placement lessons learned
once and re-learned here:

- It must run **after `beginScope()`**, whose reset clears the per-scope kind maps: a scan answered
  before it is silently thrown away, and the symptom was the exact shape it was meant to fix. ADR 0228
  learned the identical fact about written-flags one cycle earlier, in the same file, and the fix had to
  be rediscovered because the constraint was never written down as "anything consulting the kind maps
  is scheduled after the scope reset".
- It must not mark **async** functions: an `async def g(): return "ok"` yields a coroutine handle, so a
  caller told "this returns a string index" read a handle as an index and `llc` rejected the module. The
  suite caught it (`TestR2ReproCompilesAndRuns`), which is the argument for keeping a repro corpus.

### The runtime may only reach for its own block

`rt_str_intern` capped the table at 256 and, on overflow, **reused the last entry** — a string printed as
a different string, forever, silently. Raising runtime-created strings made the cap reachable (iterating
a text interns a one-char string per distinct character). Raising it to 4096 plus a `-2` sentinel that
the *caller* turns into a catchable `RuntimeError` is the honest shape.

Getting there cost two build breaks and taught a rule worth pinning: my first overflow path called
`rt_die`, defined in a different runtime block, so a module that didn't include that block failed as
`use of undefined value '@rt_die'`; declaring `write` locally instead failed as `invalid redefinition of
function 'write'`. Runtime helpers must use what is in their own block, and where that is impossible,
return a sentinel and let the code that knows the source raise — a printed sentence is not catchable, so
a print from the runtime would have broken ADR 0212 while "fixing" it.

### Two Go/IR traps, both cheap to avoid, both invisible until they bite

- A backtick inside the runtime IR blob terminates the Go raw string: `; … `in` …` in a comment broke the
  build with an error pointing into the middle of the blob.
- An IR comment must start every line with `;`, not just the first — a wrapped line without one parses as
  an instruction. Both were build-time, not test-time, failures; the diff is smaller than the debugging
  time they cost, so they are recorded here rather than as code.

### What stayed honest

Concatenation, run-time slicing, `for c in <runtime string>`, `str(<runtime int>)` and `strip()` remain
refusals with their measured messages — they need to *build* buffers, the write half of the same runtime.
Compiled case folding is ASCII only, stated in the roadmap rather than quietly asserted. And the pinned
"these shapes are unreachable" test fired exactly as designed when six of them started answering: it
demanded promotion to a CPython-checked table instead of a softened expectation.

## The write half of the string table — and the fixtures that stopped refusing (Gap R.47, ADR 0230)

### What closed

Concatenation with a runtime operand, slicing at run-time/open/negative bounds, `str()` of a
computed number, `strip()`, and iteration of a string held in a variable. Five more runtime
operations (`rt_str_cat`, `rt_str_byteoff`, `rt_str_slice`, `rt_str_strip`, `rt_str_of_int`) plus a
`for`-over-string driving the ordinary counter loop. Every shape is checked against CPython on both
engines; the matrix moved to 101 rows / 78 pass / 60 oracle-match, 0 fail, 0 drift.

### The bug behind the empty slice

`rt_str_byteoff` advanced its code-point counter on *continuation* bytes instead of their
complement. Pure ASCII therefore never counted a character, the walk ran to the NUL, both slice
bounds came back as the end, and `s[1:3]` returned `""`. Two things worth noting: the earlier
`rt_str_char` got this right because it had been written with the complement, and the discrepancy
survived every test until one asserted the *answer*. A counting rule stated in two places drifts —
the same finding ADR 0228 made about `for` versus `while`.

### Silent-zero, take four

`for c in txt()` printed nothing and exited 0, because the string's table index was read as a
repeat count (Gap R.16). Then it was an honest refusal. Now it is an answer, and the test that
guards it asserts output rather than acceptance — the distinction that keeps this family from
regressing into a green suite over a dead loop.

### The process finding: a pinned refusal is a claim about the future

`for c in txt(): print(c)` had become the canonical "the compiled backend refuses" fixture in four
places — the exit-code contract (`--aot` must exit 1), the trap-class contract (a refusal must not
blur into exit 3), the oracle's refused-leg check, and a diagnostics table. When the shape became
answerable they all went green while exercising nothing. Same for the pinned refusal *tables* in
`string_subscript_test.go` / `string_value_test.go` and the two `string_args_test.go` lists: each
entry that starts answering has to be promoted into a CPython-checked answer table, and each
fixture has to move to a shape that genuinely still refuses (`print("ab" * 2)`, Gap R.33 here).

The mechanism that made this visible at all is the one that keeps firing: the pinned-refusal tests
are written to fail when the leg *answers*, with the message "if it is right, promote this case".
Sixteen cycles of that have converted every gap closure into a forced edit of the record instead of
a quiet softening.

### Assertions over the emission, again

The classification layer is where the wrong answers live, not the lowering: the slice value was
correct while `print(f(1))` printed `1`, because neither the whole-program return-kind scan nor the
print predicate had a case for `return s[i:j]`. Restating ADR 0229's rule so it can be applied
without re-deriving it: **one predicate answers "is this a string?" for every consumer — operations,
print, and the kind scan — and adding an operation means adding its case to all three in the same
commit.**

### Where the honesty lives now

Compiled case folding is ASCII-only, `strip` trims ASCII whitespace, `str(<float>)` refuses instead
of truncating a float it cannot hold (L11.6), `%` formatting doesn't exist (R.31), `str * int` and
list concatenation still refuse (R.33), and the 4096-entry string table raises a catchable
`RuntimeError`. Each is written down where the next cycle will find it, rather than being quietly
half-implemented behind a passing test.

## L8.5 — the line table lives in the module (ADR 0231)

### The claim that had no witness

`--debug` added `-g` to the `cc` link. DWARF is not made there: `llc` writes `.debug_line` from
`!dbg` metadata, and the module had none. So for the whole life of the flag, `--debug` produced an
object with an empty line table and a satisfied message. Nothing in the suite could have noticed,
because every assertion was about what the compiler *intended* — the string `-g` in a command line.
The fix was not only the metadata; it was that **every number the tool now reports is read out of
the artifact**: the module's own metadata nodes for `--debug-info`, `llvm-dwarfdump --debug_line`
for the object, `llvm-addr2line` in the integration suite. A report that can only answer "yes" is
not a report.

### `llc` lied with a straight face

The first version of the metadata was wrong in one field, and the failure was spectacularly quiet:

```
invalid subroutine type
```

…exit code 0, module accepted, object written, `.debug_line` **empty**. `DISubprogram`'s `type:`
has to name a `DISubroutineType`; pointing it at the bare `!{…}` type list is not a malformed
module — LLVM simply declines to describe the function. The internal counts (subprograms: 3,
instructions tagged: 57) were all *correct about the metadata I had written* and completely wrong
about the artifact. Only the dwarfdump read-back, which I had added an hour earlier on a hunch,
turned this from a shipped no-op into a caught bug.

Small enumeration of what LLVM 20 accepts, since it is not guessable: `DW_LANG_Python` yes;
`DW_LANG_PYTHON`, `DW_LANG_python`, `DW_LANG_BASIC`, `DW_LANG_Carbon` no. `DISubprogram` and
DICompileUnit are `distinct`; `DILocation` is not. Without `!llvm.module.flags` carrying
Dwarf Version + Debug Info Version, the whole block is ignored.

### Positions are the feature, twice over

The line table can only name a line the parser wrote down. `self.n = self.n + k` and
`a, b = xs` were built as `AssignStmt` with **no `Src` at all** — so every instruction they emitted
inherited the *previous* statement's line, and the first version of the test failed with "no
emitted IR line is attributed to source line 9" while the emitter was behaving perfectly. Gap K.6
taught exactly this about `raise` ("every traceback said line 0"); the same class had been open in
two other statement shapes for the entire history of the language, invisible because a diagnostic
pointing at the wrong line looks like a diagnostic.

The general rule to keep: **a statement node without a position is a bug**, and the cheapest way to
hold it is an assertion that a program with every statement shape in it produces one line-table row
per statement.

### Where the pass belongs

Emitting metadata inline (the clang way) would have meant threading a builder, an insert block and
a scope through twelve emit sites in four files. Instead codegen records *(byte offset, statement,
function)* and one post-pass lays the records over the finished text — one place where debug info is
decided, one place to test, and the offsets make the two halves agree without either mirroring the
other's line counting. It also gave the "which functions are program code" question a better
answer than a book list: a define is program code iff some `DISubprogram` names it, which is the
same rule LLVM's own debug-info verifies, and it excludes the GC/exception/printer blocks by
construction rather than by an exempt-list that has to be maintained.

Ordering mattered more than expected: allocas are hoisted *after* the records go on, which moves
lines, so the read-back runs on the shipped text — reported IR line numbers are the ones in the file
someone will open. The textual fallback optimizer (used when no `opt` is on PATH) had to learn to
carry the metadata block as the module's tail and to keep `, !dbg !N` out of the instruction text it
parses, or the line table vanished exactly when the real toolchain was missing.

### `--debug` means every path that builds an artifact

It is honoured by `--build`, `--emit-llvm` (which prints the very module a debug build links), and
`--jit/--aot` — where `GUSTY_KEEP_LLVM=1` now leaves a `.ll` and a loaded `.so` that a debugger can
actually read. A flag that silently does nothing on two of three backends is the same bug as `-g` on
the link step, wearing a different hat.

### Docs are user-visible behaviour

`docs/operations.md` promised, in prose, that `--build --debug` "passes `-g` to the final `cc` link
so the binary carries DWARF debug info (line tables)". That sentence was the bug, written down. When
a flag's docs describe a mechanism nobody implemented, the docs are part of the test surface.

## L11.1 (1b) — I came to add mixed dicts and sets, and found the wrong answers instead (ADR 0232)

### The feature was the easy half

The cycle was scheduled as "same storage trick as lists, two containers". It was that, and it was
also the cycle in which the compiler stopped answering `{1: "one"}["a"]` with `one`.

Nobody had noticed, because nobody had asked. Both backends had been refusing or agreeing, and the
matrix compares backends. I asked only because the pinned-refusal test in `string_containers_test.go`
had to be inverted to write the new positive rows, and inverting a refusal forces you to state what
the program answers — so I ran the shape family through `python3` first, as the oracle rule says to.
Two of the shapes answered `one` and `1` where CPython raised `KeyError` and printed `False`. Strings
are `@str_tab` indices; a payload-only comparison in a container of one *declared* kind is a coin
flip, and the coin had been landing heads for as long as the feature existed.

**The lesson, filed under "run the oracle over what is already claimed, not only over what you are
adding"**: a subsystem you arrive at to extend is also a subsystem you are now responsible for
measuring.

### ADR 0189's rule paid for itself three times in one commit

"Whatever writes a slot's payload writes its tag, in the same operation" had been written down two
cycles ago as a hygiene rule. This cycle it was the *precondition* for three separate things:

* **Tagged lookups** became legal — you cannot compare a tag on read that nobody promised to write.
* **Promotion** became sound — when `xs.append("a")` contradicts the container's recorded kind, the
  compiler can stop claiming a kind precisely because the slots already describe themselves.
* **`discard` on a mixed set** — deleting a member shifts payloads down; if tags are not shifted with
  them, every survivor prints as the kind of the member before it.

The converse also showed up, same shape, three times: `d = {"a": 1}; d["b"] = "x"` printed
`{'a': 'x', 'b': 'x'}`, and `xs = [1, 2]; xs[0] = "s"` printed `['s', 'b']`. Both were a *claim*
("this container holds strings") standing in for an *observation* ("this slot holds this word, with
this tag"), and the claim was made about every slot by the operation that touched one. Verified at
HEAD in a throwaway worktree — pre-existing, not introduced here — which is exactly why it went
into the ADR as a wrong answer rather than a regression note.

### I added two refusals while adding support

`[pick(1), "a", pick(0)]`, where `pick` returns `"z"` on one path and `7` on another, printed
`['z', 'a', (null)]`: the 7 looked up in the string table. `print` asks what a value is *when it
prints*; a container slot is labelled *once*, when it is built, and `int | str` has no single label.
The honest move was to refuse, not to pick. Same for a membership needle whose kind cannot be proven
against a container that mixes. To a reviewer that reads as a regression ("less compiles now!"); the
test that proves otherwise is the one that asserts the *text* of each new refusal, and the row in the
refusal table asserting exit 1 — never exit 2, and never exit 0 with an answer nobody can justify.

### Bools: agree with the interpreter today, let the pinned test keep the difference

Tagging a bool `TagBool` would have made compiled `[1, True]` print `[1, True]`, disagreeing with the
interpreter (which stores bool as `Int(1)` and prints `1`) and inventing a second, contradicting
pinned difference. The tag chosen was the one both backends already store — `TagInt` — with the note
that when L11.2 gives bool its own kind, one line of `elemKindTag` changes and every container
follows. One pinned difference per commit, always.

### The 5-minute loop, and `git worktree` as a memory

Everything above was found with the same three commands in a loop, on throwaway programs in `/tmp`:

```
gustyc --file p.gy --aot    # the compiled leg
gustyc --file p.gy          # the interpreter
python3 p.gy                # the answer
```

…before any Go test existed. The test suite's job is to make the finding permanent, not to make the
finding. And when the question is "did I break this, or was it already broken?", the answer is a
`git worktree add /tmp/gh-head HEAD` and a second binary — cheaper than reasoning about it, and twice
this session the reasoning would have been wrong. (One caveat learned the hard way: a stale worktree
directory gets silently pruned, and `git worktree add` then succeeds without creating the checkout;
`git worktree list` is the check.)

### The suite's tripwires fired, on purpose

Third time now that a pinned "this must be refused" test had to be inverted after the hole it pinned
was closed (ADR 0230's lesson, twice applied). That is the design working: a refusal test that never
fails is not testing a refusal, it is protecting one. Two rows moved from `mixedContainerCases` to a
print-correctly table, the mixed-list element tests' bool rows flipped from "refused" to "prints what
both backends store", and the whole suite told me which ones in seconds.

### Small things worth writing down

* `%s` on an AST node prints `&{%!s(*lang.Call=...)}` into a user-facing diagnostic. There was no
  expression-summary helper in the codebase, so `exprSummary` is now one: name the call, name the
  variable, otherwise name the kind. Diagnostics are an interface (docs/operations.md), including for
  the compiler's own error paths.
* A value can be asked what it is twice, and get two true answers: `heapElemKind` answers "which word
  did I intern", `printsAsInternedStr` answers "what does the source say this is". `xs[0] = d["a"]`
  satisfied the first and not the second, and the store happened while the print denied it.
* The oracle cannot judge every row. `print({1, "a", None})` is `{1, 'a', None}` here and
  `{'a', 1, None}` in CPython, because gusty documents insertion order for sets. The oracle table now
  carries a comment saying which rows it is allowed to decide, instead of the test quietly dropping
  the inconvenient one.
* Push still blocked on the SSH agent refusing to sign (`ssh-add -l` lists the key, signing fails);
  the commit is local and the push is retried each cycle.

## The roadmap became a tracker, and the tracker found bugs (roadmap tabulation, 2026-10-01)

No compiler change this cycle: `roadmap.md` was 2,750 lines of prose with statuses embedded
in sentences, and it was asked to be a table. It is now a tracker — status vocabulary,
measured snapshot, component map, one **open queue**, one row per item and per gap — and the
narrative moved, verbatim, to `docs/roadmap-details.md` (2,789 lines, one anchor per row).
`AGENTS.md` says what the format obliges: a status is a cell, the queue is the only list of
owed work, and item IDs are permanent because `pkg/lang` comments cite them.

What the exercise taught, in the order it hurt:

* **A status written twice is a status that is wrong.** The old file stated the same fact in
  up to three places (the item bullet, a "R." summary list, and the sequencing note), and
  they had drifted apart: the summary still said `R.13`/`R.15` were OPEN while their own
  sections reported them closed by ADR 0204 and ADR 0208; `Gap E` still promised string
  *variable* slicing that `rt_slice` had long since lowered; `Gap P.2` still claimed float `%`
  was wrong on both backends after ADR 0216. Re-measuring each of those took seconds with the
  built binary and changed four rows. Single-cell status is not tidiness, it is the mechanism
  that keeps the file from lying.
* **A snapshot paragraph rots fastest.** It claimed 74 programs / 18 probes / ADRs to `0194`;
  the truth was 110 / 21 / `0232`. The snapshot now has a "measured by" column and every row
  names an artifact (`integration/conformance-matrix.json`, `docs/adr/`, `pkg/lang/compile.go`).
  A number nobody can re-measure is decoration.
* **The tracker has to be the union of every citation, not of one file.** Eight IDs —
  `R.19`, `R.22`, `R.24`–`R.29` — were cited by comments in `pkg/lang`, by ADRs and by the
  conformance ledger's `ref:` fields, and had no row anywhere. They are recorded now, in a
  clearly-marked *recovered* section of the record, each sourced from its ADR and its test.
  The ledger's `ref:` strings and the code comments are now the two things a new row has to
  agree with, which is the same "citations must resolve" rule ADR 0219 enforces for programs.
* **Tabulating is measuring.** Checking an `Evidence` cell means running the program, and the
  sweep turned up four defects that no test and no row knew about: **Gap R.49** (`def f(x)`
  above `x = 8; x /= 2` leaves the module's `x` with no slot, `llc` rejects
  `store i32 %t5, i32* %_x`, exit 2 — the compiler-bug code, for an ordinary program);
  **Gap R.50** (`round(2.5)` is `3` here and `2` in CPython — identical on both backends, so
  the parity matrix structurally cannot see it; only the oracle leg can); **Gap R.51**
  (`floor`, `ceil`, `sqrt` are predeclared for the checker, `NameError` for the interpreter,
  answered-by-fold for the compiler — three engines, three behaviours, and the answer has the
  wrong type); **Gap R.52** (stdlib discovery walks up from the cwd, so an installed binary
  cannot `import math` without `--stdlib`). Each got a row, an owner, and its story in the
  record — no fixes smuggled into a docs commit.
* **The citations test is what made the split safe.** Moving 2,700 lines into `docs/` moved
  them *into* the scanner's scope (`docs/*.md` is checked too), so `programs/NAME.gy`
  citations that were safe at the repo root had to resolve where they landed. 156 of them do.
  The `(planned)` marker earns its keep in the queue's DoD cells: `programs/nested_data.gy
  (planned)` is a promise, and the test knows the difference between a promise and a lie.
* **Tables discipline the writing.** A cell cannot hold a paragraph, so the long version goes
  to the record; `` `int | str` `` has to be escaped or it silently splits a row; and an
  anchor per row turned "see the discussion above" into a link that either resolves or does
  not. The format is doing the work the prose never did: it makes an unmaintained status
  visible instead of merely long.
* **Committed generated artifacts churn.** `integration/conformance-matrix.json` embeds
  `/tmp/gusty-oracle4187099451/prog.py` in two rows' `python_error`, so any test run produces
  a 22-line diff of pure noise; this commit reverts it. Follow-up owed: strip the temp path
  from the oracle error text (or normalize it) so the artifact is byte-stable — a "measured"
  claim that changes when nobody measures anything stops being evidence.

## The tracker grew a `Free text` column (follow-up to the tabulation, same day)

The tabulation's first draft made each row `ID · item · status · path · ADR · evidence ·
record`, with the old wording reachable only through a link. The objection was right and
worth writing down: **a link is not a copy.** Reading a row meant leaving the row, and a
record nobody scrolls through is a record nobody reads.

So every item and gap row now carries an eighth column, `Free text`, holding the item's own
sentence from before the tabulation — sub-bullets kept as `•`/`↳`, code listings kept as
`<code>` blocks, hard-wrapped lines joined — with the rule stated in the column contract:
*history, not state; where it disagrees with the `Status` cell, the cell is right.*

How it was done, so it can be redone:

* The pre-tabulation file was kept as `/tmp/roadmap.orig.md`, and the extractor walked it
  splitting on item markers (`### Gap N.2`, `### R.16`, `- **L4.1 …**`, `- **Gap J.1 — …**`),
  collecting each block until the next marker, then rendering a block as a cell: paragraphs
  joined, sub-bullets prefixed, fenced programs HTML-escaped, `|` → `\|`, pipes inside code
  → `&#124;`. 155 blocks came out, and 170 rows are filled (the extra 15 are the recovered
  `R.19`/`R.22`/`R.24–R.29` and the four new `R.49–R.52` rows, whose text was written this
  cycle and lifted back out of the record by anchor).
* **The verification is the part that matters:** every extracted block is asserted to appear
  verbatim in the tracker (155/155), and a token-multiset diff over the Phase 0–3 lists shows
  nothing but heading words left out. A "nothing was lost" claim that isn't a diff is a
  feeling.
* Section intros that no item owns — the Phase 4–11 goals, the "New work" ordering note, the
  Gap J/Q/R family intros — went in as collapsed `<details>` blocks; the original header,
  status snapshot, component map and sequencing note went to the record under
  *Original preamble*, because they are superseded but should not be gone.
* Roadmap is now 233 KB in one file, and rows for `L11.1`/`Gap R.35` are long. That is the
  cost of "readable without leaving the table", and it is the right order: the Status column
  still answers "what's owed" in one screen, the free text answers "what did it say" in place,
  and the record answers "why" at whatever length the finding needs.

## Phase 12: the surface survey, and the three states a construct is allowed to be in (2026-10-01)

Asked to make the roadmap "compliant with modern language design in 2026", the temptation was to
write a wish-list from what modern languages have. That would have been the fourth status-bearing
prose section this tracker deliberately removed, and it would have been unfalsifiable. Instead:
gather the repo's facts, then measure the language, then let the measurement write the phase.

**Facts first.** 65 010 lines of Go, `codegen.go` alone at 13 421; 224 ADRs, highest `0232`;
110 corpus programs, 21 of them probes; the conformance matrix at 101 rows / 78 parity / 0 drift;
the bundled stdlib at **four modules and 24 lines** (`math` is seven constants and has no `sqrt`,
`json` is three constants and has no `dumps`) — a fact that reframes "does `import math` work?"
into "it works, which is why `math.sqrt` looking like a typo is worse than a missing module".

**Then the survey.** 76 programs written the way a person writes Python — f-strings, `@dataclass`,
`Enum`, `Protocol`, `with`, `*args`, `del`, `assert`, `match` patterns, dunder protocols, set
operators, `from … import … as …`, `__name__` — each through `--interp`, `--aot` and
CPython 3.12.3. Classified by what the legs did, never by what the file intended. The classes came
out: 9 CPython-equal on both backends, 37 honest absences, 16 compiled-only refusals, **8 that run
everywhere and answer wrong**, 2 that hang.

**What measuring found that reasoning would not have.**

• **The accidental pass.** `print(1 < 2 < 3)` prints `True` — and the operator is wrong. Chains
  parse as `(1 < 2) < 3`, so the middle operand meets a boolean; `1 < x < 3` takes the branch with
  `x = 5` on both backends. A test written from the one case that looks like a chain would have
  certified it forever.

• **Two legs, two different wrong answers, and one of them hangs.** A class with
  `__iter__`/`__next__` printing 7.3 million integers in 15 s interpreted, and answering *"no
  elements"* with exit 0 compiled. `for` does not read `StopIteration`; there is no iterator
  protocol, only special-cased loops. Parity could never see this: there is no agreement to check.

• **`int` overflows, and the backends overflow differently.** `2 ** 63` is `-9.2e18` interpreted,
  `0` compiled, `9.2e18` in Python; `10 ** 19` differs by the compiled path folding through `i32`.
  Documented bounded integers are a defensible 2026 decision; silent wrapping is neither design nor
  accident.

• **The wrong-table message is a structural finding, not a typo.** `xs.insert(0, 0)` on a **list**
  compiles to `codegen: string method insert on non-constant string`. Gap R.38 said a refusal may
  not assert something false about an operand kind; here the false assertion comes from a lookup
  table rather than a template, so the fix is a receiver-keyed dispatch, not a reworded sentence.

• **The rare inversion.** `class C(A, B)` resolves `A`'s methods in the interpreter and fails on
  `B`'s — while the compiled backend resolves both. Two MRO implementations differing in
  correctness is the argument for computing the linearisation once.

• **The manifest rots in the safe direction.** `gustyc --lang` never claims a construct that does
  not exist, which is why nobody notices it omitting `with` (both backends, documented), `yield`,
  `async def`, `in`, `is`, `**`, the ternary, the walrus and f-strings. For an agent-facing
  toolchain that is not cosmetic: the manifest is the language an agent compiles against.

**The decision the phase records** is not a feature list but a state machine: every construct is
**implemented** (both backends, CPython-equal), **refused** (stable `Diagnostic.Code`, documented
exit class, a line in `docs/language.md`), or **absent** from the manifest. There is no fourth
state, and "parses, runs, prints something" is the one that has to be deleted. It is ADR 0212's
"a trap is a typed raise" and ADR 0211's "one event, one code" applied to the grammar.

**Process notes.** This is a docs-only cycle: the queue gained 13 rows (`L12.1`–`L12.13`) and the
Gap R family 14 (`R.53`–`R.66`), no code changed — a phase whose rows are measurements, so the
loop can pick them one at a time. Three rows are Phase 11's by root cause (the `(null)` dict
binding, the `str`/`repr` pair, integer width) and are marked as such rather than started early,
which is the same discipline that keeps L11.3 behind L11.1. The census harness itself lived in
`/tmp` and is *not* a test — the record says so plainly, and `L12.13`'s definition of done is to
promote the probes into the corpus so the manifest is checked by CI instead of remembered. The
citation guard did its job again: every new `programs/*.gy` citation had to be marked `(planned)`
or it would not resolve, which is precisely the failure mode ADR 0219 was written for.

## A folded comprehension is the literal it folds to (Gap J.2, ADR 0234)

**Picked from the queue, row 15 — the Phase 2 surface row the tracker had been carrying as
🟨 PARTIAL since ADR 0165.** `sa = {x for x in [3, 1, 2]}` did not compile; `da = {k: k * 2 for k in
[1, 2]}` did not compile; `{x for x in xs if x > 1}` did not *parse*. The interpreter ran all three
and CPython ran all three.

**What was actually broken was four things, and the gap named one.** `comp()` folds a comprehension
into `@.setN` / `@.dictN` — a `{i32, [n x i32]}`, a length plus an array — and four consumers then
asked that global for a *value*: the binding (`store i32 @.set1, i32* %_sa`), the printer
(`rt_print_list_mixed(i32 @.set1, 0)`), membership (`rt_contains(i32 @.set1, i32 2)`), and — found
only when I wrote the corpus program rather than the minimal repro — `x in {comp}`. Every one is a
module `llc` refuses, which ADR 0211 correctly files as exit 2, *a compiler bug*, for a program a
person writes without thinking. Fixing only the store — the literal reading of the gap row — would
have shipped something that compiles and dies on the next line.

**The list spelling had already been fixed; twice.** ADR 0188 (a literal in print position is a
rendering question → build the object, ask the printer) and ADR 0163 (a binding allocates, tags,
registers, keyed on `staticLists`). Neither had been carried to sets and dicts. `staticLists` sat
alone in `irGen` with no `staticSets` / `staticDicts` beside it — a two-field omission with an
exit-2 tail, which is what "the fix is a table, not an if" keeps meaning in this codebase.

**The print branch hid a wrong answer behind the invalid module.** `*Comp` in print position asked
one printer for all three kinds. Had the module been valid, `print({x for x in [1, 2]})` would have
printed `[1, 2]`. The conformance program is written so CPython's rendering pins that: `{4, 5}` and
`{4: 12, 5: 15}` are not what the list printer emits. An invalid module can be *protecting* a wrong
answer; deleting it means re-asking every question about the same shape.

**The parse gap was one precedence choice.** `parseListOrComp` parses an iterable at `or`-precedence
precisely so a ternary cannot eat the comprehension's `if`; `parseDictOrSet` used `parseExpr()`, a
full expression — so `{x for x in xs if x > 1}` became `{x for x in (xs if x > 1 …)}` and died on
`expected keyword "else"`. The list branch had learned this lesson; nobody had told the braces. When
two constructs share a grammar rule, the fix has to be checked in both spellings, not just where the
bug was found — which is why the new parser tests cover `{…}` and the `{k: v …}` form and re-assert
that an `or` iterable still parses.

**The fix is a deletion, not a mechanism.** `foldSetComp` / `foldDictComp` return the `*SetLit` /
`*DictLit` a folding comprehension denotes; `comp()` emits its global *from* that literal; the
binding substitutes the literal and falls into the path `d = {1: 2}` has always used. No comprehension
survives to the binding, so nothing special-cases one. The runtime loop (ADR 0192's
`runtimeCompLoop`, previously list-only) gained `rt_set_add_tagged` and `rt_dict_put_tagged` — both
self-indexing, which is what lets an `if` filter skip items without leaving a hole.

**The classification layer struck again, exactly as ADR 0230 recorded.** A runtime-loop set bound to
a variable printed `1`. Not the set: the *handle*, through `printf("%d")`, because nothing had
recorded `runtimeSets[sa]`, so `print` had no reason to ask `rt_set_print`. The kind is written in
the syntax — `[..]`, `{..}`, `{k: v ..}` — and I had derived it only from the inferred type. Two
vocabularies answering one question, one of them silent: fixed by letting the comprehension's own
kind answer when the inferred type does not.

**Measured, not remembered.** Every claim above was run through `--interp`, `--aot` and CPython 3.12.3
before it was written down; the corpus program `programs/comp_containers.gy` prints CPython's answer
line for line and enters the matrix as row 102 with a `match`. Set members are written ascending
*because* of that run: `{3, 1, 2}` is `{1, 2, 3}` in CPython and `{3, 1, 2}` in both gusty backends
(documented insertion order), and a corpus row that needs a `debt` entry to pass proves nothing about
the thing it claims to test.

**Refusals kept honest by comparison, not by prose.** `TestContainerComprehensionRefusalsStayHonest`
compiles the set spelling and the list spelling of the same unsupported shape and fails if the two
messages differ — a refusal that is only true of sets is a refusal nobody designed.

**Newly measured, deliberately not fixed (Gap R.67).** `return [1, 2]` emits `ret i32 @.lst1`
(exit 2) and `la = [1, 2]; return la` prints `0` — the same operand question one statement further
out, affecting literals, so not created here: closing the binding just removed the refusal that used
to stand in front of it. It is L11.1's (a handle and its kind travelling together), and it goes on the
queue with a `(planned)` program rather than into this commit — one commit per feature.

**Process notes.** Full suite green before and after (`go test -tags=llvm20 ./...`), matrix at 102
rows / 79 parity / 0 drift. The queue lost row 15 (Gap J.2 ✅) and gained one (Gap R.67), so it still
says 53 owed — a reminder that closing a row honestly often costs a row. The guard test I wrote first
is the one I trust most: a regex over seven whole modules for a folded container global in an operand
position, because three `strings.Contains` on the three lines I knew about would have certified only
the three lines I knew about.

## A class pattern is one question about a class — and the instance has to be asked (Gap B, ADR 0235)

**The row understated itself.** Gap B's cell said the two backends "disagree by one digit" on
`case Alias(a, b):`. Run it: the interpreter declines the arm and prints `no`; the compiled backend
takes the arm and prints `pt 0 0`. That is not a digit, it is a different program — and the compiled
answer is an unbound slot read dressed as a match. A tracker cell that says "disagrees" when the
reality is "one side invented an answer" is how a defect survives being looked at.

**Three failures, one shape.** `case Point(a, b):` matched an instance with no `a`. `case Alias(x, y):`
inside a function was exit 2 in codegen and exit 3 in the interpreter. `case f():` — a plain call
pattern, nothing to do with classes — loaded `%_f`, a variable that does not exist. All three came out
of the same branch in `matchPattern`, the one labelled "not a declared class, so it must be a variable
holding one", and that branch began `alias, _ := g.value(b, fn)`. The discarded error was the whole
bug: with `alias == ""` the emitted line was `%t6 = icmp eq i32 %t5, `, and every callee that was not
a class landed there. A hole in a fallback branch is not a missing feature, it is the compiler's
answer for everything that branch covers.

**The instance could not answer the question the rule asks.** "Does this instance have attribute `a`?"
has an obvious answer in the interpreter (the attribute map has the key or it doesn't) and no answer at
all in a compiled instance, which is a row of `i32`s where "never written" and "written `0`" are the
same word. So the pattern ANDed a class-chain test and *bound anyway*. The fix is `@inst_set`, the
presence array `rt_inst_put` writes with the value — ADR 0175's pairing rule ("the operation that
writes the payload writes its tag") arriving at the third data structure after lists and dicts. And
the part the tag history had already taught: `rt_alloc` recycles slots, so presence must be cleared at
instantiation, or a fresh object inherits which attributes the previous tenant of that heap slot had.
`GUSTY_ENV_GC_STRESS=1` over 300 instantiations, then a probe for a `ghost` attribute nobody ever
wrote, is the test for that — not a comment saying "cleared on alloc".

**One question, one table — the same rule, the third time it has had to be applied.** Which class does
a pattern-position name denote? The evaluator answered from the scope it happened to be executing (so
`Alias = Point` existed at module scope and vanished inside a function, and the pattern fell through to
*calling* the class: `TypeError: 'type' object is not callable`). Codegen answered separately, and
badly. Both now read `classpat.go`, built once from the AST — which also had to answer "what
attributes can this program write?" *before* emitting anything, because the clear length is an
allocation-time constant and slots intern lazily.

**A pattern's answer is an `i1`, not an integer.** The old code handed back `"1"`/`"0"` for
always/never-matching patterns; `andCond` special-cased `"1"` and the `or` combinator did not, so
`case x:` inside an or-pattern would have emitted `or i1 %t, 1`. Patterns now answer `true`/`false` and
both combinators fold constants. Same lesson as the `switch`-shape bugs earlier: an intermediate string
representation of a truth value is where the invalid IR lives.

**Refusal-shaped honesty.** `case n(x):` where `n` is an integer now fails the case in both backends;
CPython raises `TypeError` there, and gusty's documented rule is that the case fails — so the ledger
row pins "both backends agree, CPython not applicable" rather than pretending the oracle covers it.
A positional class sub-pattern needs `__match_args__` this language does not have, so the whole program
is `not_applicable` with per-leg pins: `match: true` would have been a lie the matrix believed.

**Newly measured, deliberately not fixed (Gap R.68).** A capture that only a *skipped* arm would have
bound is readable compiled — `five 0` where the interpreter raises `NameError: name 'x' is not
defined`. That is ADR 0228's definite-assignment graph missing arm-scoped bindings, a different
question from the one this commit answers, so it goes on the queue with its own ID rather than into
this commit. The conformance program avoids the shape on purpose: pinning `five 0` as parity is how
`pt 0 0` survived being looked at for two releases.

**Process notes.** Full suite green before and after (`go test -tags=llvm20 ./...`); matrix 103 rows,
80 shared and all at parity, oracle drift 0. The queue lost Gap B and gained Gap R.68, so it still
says 53 owed. Two corrections worth naming: the `%t7 = and i1 %t4, %t6` repro I recorded yesterday
came from a probe with `Alias = Point` written *above* the class — the documented spelling (after it)
fails identically, and the record now says so; and `docs/language.md` carried "Class patterns are an
interpreter-side feature; the AOT backend lowers `match` to expression-equality only" while codegen
had a 60-line class-pattern lowering in it, which is the documentation equivalent of the fallback
branch — a stale claim that made the wrong answer look intended. The `patterns:` line in `--lang` is
the machine-path fix for the same failure: nobody could discover the construct, so nobody tested it.

## A numeric rule lives in one IEEE operation, not in two implementations that agree (Gap R.50, ADR 0236)

**`round(2.5)` was 3, and the build was green.** CPython says 2. The gap row had said it plainly —
"both backends agree with each other and disagree with Python" — and it had sat there since the
2026-10-01 sweep, because that sentence describes a defect the suite is structurally unable to see:
parity compares our two implementations to each other, and both had independently implemented the same
wrong rule.

**Where the wrongness lived is the finding.** Four places, not one: `math.Round` in the evaluator,
`llvm.round.f64` in the compiled runtime, `math.Round` again in the compiled constant fold, and four
tests — two of them asserting the rule *in prose* ("must round half-away in the AOT binary"), one
pinning the string `i32 3` in emitted IR. A test that restates a rule is a second authority for it, not
a check on it. When someone later reads why the tie rule is what it is, the four inverted pins are the
useful part, which is why each keeps its old expectation in a comment instead of being quietly edited.

**The fix removes the rule rather than correcting it.** Neither backend implements rounding any more;
both ask for IEEE `roundTiesToEven` by name — `math.RoundToEven` on the host, `call double
@llvm.roundeven.f64` in the module. Two standard libraries of different provenance agreeing is a much
stronger claim than two of my own implementations agreeing, and "half away from zero" no longer appears
in the tree in code or in comment.

**Literals and variables are separate cases on purpose.** The fold path (`floatEval`) and the runtime
intrinsic were wrong in different ways — the fold was wrong for literals, the call for variables — so a
test that exercised only variables would have gone green on a program that still printed 3 for
`round(2.5)`. `round_ties.gy` pins both, and it went into the corpus with *no ledger row*, which under
this repo's convention is the strongest claim available: print what CPython prints, both legs.

**The fold's assertion had to be written as an absence.** A check that only looks for the right call
passes on a module whose fold is still wrong, because a folded constant emits no call at all. So the
fold test asserts `!strings.Contains(mod, "llvm.round")` and `contains "i32 2"` — negatives are the only
way to test a path that leaves no trace.

**Found while writing the test, left owed (Gap R.69).** `round(2.345, 2)`: CPython `2.35`, interpreter
`2` (the digit count is silently ignored — no error, no fractional part, a caller's `*100` off by the
whole fraction), compiler an **exit 1** refusal for a program CPython runs. The exit code is the
outrage: exit 1 is the contract's "your program has a compile error" class (ADR 0211), spent on valid
Python. That is L11.8's and Gap R.38's subject with a fresh repro, and the value half is now cheap
because the named operation exists in the tree — the expensive half is ADR 0230's lesson, that a
compiled function must *record* it returns a float, not merely produce one.

**The documentation was a fifth authority.** `docs/language.md`'s builtin paragraph said, about `round`
in one breath, that it truncates, that it folds-and-is-a-no-op, and that "the AOT backend has no float
representation" — three states, none current, all of them reading as settled. Rewritten, and the open
`ndigits` case is now stated as an open gap with a workaround instead of being silently absent from the
language description.

**Process notes.** Suite green before and after; matrix 104 rows, 81 shared all at parity, oracle 62
`match` (up one — the new program is a real oracle match, not a parity claim). Queue unchanged at 53:
Gap R.50 closed, Gap R.69 opened while closing it. This was also the first piece of L11.6 (and of the
one remaining Phase 2 item, P2.15), landed as a piece rather than as "the L11.6 cycle": the umbrella row
now strikes the tie rule from its next-action list and keeps the rest.

## A model is configuration, not a constant (tools/pi-loop, ADR 0237)

The cycle was "make pi-loop use the new `setup/` model config" — `setup/pi_qwen3.8-flash-next.json`,
`local-vllm/qwen3.8-flash-next` at `http://localhost:8888/v1`. The two-line version (change the port,
change the id) was refused, because the pair being changed was itself the bug. What the sweep found:

**The published `models.json` had been dead on arrival the whole time.** pi validates it with a *closed*
TypeBox object — legal roots are `providers` and `modelOverrides` — and `ModelConfig.load` throws away
the **entire file** on one unknown key. pi-loop had been writing a top-level `models` map beside
`providers` for its whole life, so the SDK parsed it, rejected it, and every run proceeded on the
hand-built inline model while the tool congratulated itself on `wrote provider config to models.json`.
`ModelRuntime.getError()` existed for exactly this and had never been called. A write that the consumer
silently refuses is not an integration; it is a rumour. Now the merge emits only legal roots, drops the
legacy key with a line of its own, and prints `getError()` when pi complains.

**Duplicating pi's data model is how you ship a 400.** The inline model hard-coded `reasoning: true`
plus `compat.supportsReasoningEffort: true` and a 1:1 `thinkingLevelMap`, which makes the
openai-completions path send `reasoning_effort` — probed against the live endpoint:
`{"error":{"message":"Unexpected reasoning effort max. Supported types are xhigh (default), medium, and low."}}`
with HTTP 400. The new config's `supportsReasoningEffort: false` is the honest statement, and thinking
still happens because the server's *default* effort is xhigh. So: pi resolves the model
(`ModelRuntime.getPhysicalModel`) and pi-loop passes that object plus its own runtime into
`createAgentSession`; the config file — editable by a human, readable by `pi /model` — is the only place
an endpoint is described. The inline object stays only as a fallback for an unresolvable model.

**Limits: stated or nothing.** pi's composed defaults for a `models.json` model are
`contextWindow: 128000, maxTokens: 16384`. The old constant claimed 524288/131072 — numbers no server in
this repo has ever had. `modelLimits` now returns the value *and its origin*
(`setting|config|endpoint|default`) and `applyLimits` overwrites pi only for a stated origin, never for
the derived guess. Reason is a specific vLLM failure mode: a request whose `prompt_tokens + max_tokens`
exceeds `max_model_len` is refused, so an inflated output budget converts a long round into a mid-run
HTTP 400 — the worst possible time to discover a limit. The endpoint's own `max_model_len` (from the
pre-flight) is trusted when the config omits `contextWindow`.

**A parent pi session can hijack a child loop.** pi exports `PI_MODEL`/`PI_PROVIDER` into every shell
command it runs, so `pi-loop` started from inside a session read its *parent's* model out of the
environment — caught by accident, when a deliberate `--models-config=` test failed with
`model "qwen3.8-flash-next" is not in /tmp/piloop-dead.json` and the id in the message was the one this
session is running on. Fix is a namespace rule, not a rename: `PI_LOOP_<NAME>` always wins, plain `PI_*`
still works outside a pi session (existing operators' shells keep working), and inside one the two
pi-owned variables are **ignored and reported** — `pi-loop: ignoring PI_MODEL=… inherited from the parent
pi session`. Inherited state is not requested state.

**Fail before the round.** A `GET <baseUrl>/models` pre-flight now gates round 1, with the exit message
carrying `ECONNREFUSED` out of `fetch`'s `cause` — bare `fetch failed` is not actionable. `--describe`
prints the whole resolution as JSON, and `--dry-run` (`noTools: "all"`, no state write) proves config →
`models.json` → SDK session → endpoint in one command: the model answered `PI_LOOP_OK`, `↗ 1.6k in · 19 out ·
13 think · 1.7k cached`.

**Two self-inflicted wounds worth remembering.** (1) The first `--describe` implementation redirected
`console.log` to stderr *including the JSON print itself*, so `--describe | jq` parsed an empty string —
capture `printOut` before shadowing `console.log`. Machine paths need their stdout as a pure document,
not as a shared channel. (2) `node --test tools/pi-loop/` does **not** discover
`tools/pi-loop/*.test.mjs` on Node 22 — it tries to execute the directory as a module and fails with
`MODULE_NOT_FOUND`; the glob (`node --test tools/pi-loop/*.test.mjs`, or `npm test --prefix tools/pi-loop`)
is what works. A test command that silently doesn't run tests is worse than none, because it reads green
to whoever trusts it.

**Scope discipline.** No language surface, roadmap row, ledger row, or `docs/language.md` claim moved
here: this is driver tooling, so it carries its own unit tests, its README, and an ADR — and leaves the
compiler alone. The loop contract's "machine consumption path" clause applies to the tool as much as to
the language, which is why `--describe`/`--help`/`--dry-run` and the exit codes are part of the change
rather than a follow-up.

## A container element is a handle, and the tag — not the builder — runs the print (L11.1 step 2, ADR 0239)

**What the gate claimed versus what was behind it.** `elemKindTag` refused a container element
because "the collector cannot mark an element that is a handle". `rt_gc` marks *every element word of
every marked object* and re-walks to a fixed point, so the reason had gone stale — and the refusal was
hiding a wrong answer, not preventing one: `[[1, "a"]]` compiled, verified, exited 0 and printed
`[['b', 'a']]`, the inner string indices read as text-slot indexes by the wrong printer. The first
move in any stale-gate cleanup is to run the refused program and read what it actually does.

**Who chooses the printer is the whole bug.** A container has no element kind — `[[1,"a"],[2,"b"]]`
has an int row and a text row — so any *static* choice is a guess, and a guess in this runtime selects
a printer that reads a different table. `rt_print_list` was the one printer without the `@estr` bit-8
self-dispatch `rt_dict_print` and `rt_set_print` already had; adding it, and routing a tagged
container slot through one `rt_print_container_value(h, quote)` that reads the inner object's `@heap`
kind, moved the decision to the only party that knows the answer.

**Two builders, one truth.** `xs.append(ys)` printed `[1, 1]` for `[1, [2]]`: `elemTagFor` asked
`literalNeedsTags` (a copy of the predicate) while `elemKindTag` (the authoritative table) answered
`-1` and the append fell back to `TagInt`. A tag question must be answered by the table, never by a
second implementation of it — `elemTagFor` now delegates to `elemKindTag`, exactly the rule ADR 0232
wrote for payloads.

**A phi names the blocks that branch to it.** Giving `rt_print_list` the bit-8 branch changed its
`loop` predecessor from `%entry` to `%notMixed`, and llc said `PHI node entries do not match
predecessors!` — the textual emitter has no verifier until `llc` runs, so a restructured runtime
block is a two-place edit (branch *and* phi), and the failure arrives as exit 2, the compiler blamed
for an ordinary program (ADR 0166).

**Admitting a shape moves the exit-2 boundary to the folds.** With nested elements allowed, `sum`,
`any`/`all`, `min`/`max` began folding `@.lst1` straight into `add`/`icmp` operands — modules llc
refused, for programs CPython answers with a `TypeError`. A fold is only allowed to reach for
operands it can name: check the elements, refuse with the element named, and never let a handle
become a number because no one asked.

**Python returns the *element*, so the winner's type is the answer.** `min`/`max` widened when *any*
element was a float, which printed `max([1, 2.5])` as `2` (comparing in the i32 domain truncated the
float first) and would have printed `min([2.5, 1])` as `1.0`, which CPython never says. Ask which
element wins, then take its type — `minMaxReturnsFloat`.

**The interpreter adds handles.** `sum([[1],[2]])` printed `562949953421319` — `KindList`'s tag word
summed as an integer, the same class of bug as ADR 0238's `-1` for `len`. `sum` now asks each
element's kind and raises the `unsupported operand type(s) for +:` sentence CPython raises.

**`for v in {1, 2}` was a `for` over a range with a global as the bound.** The set/dict literal arms
only fed `iterStr`/`iterInts`; anything else fell through to the counter path and emitted
`icmp slt i32 %_ctr1, @.set1` — and bound the loop variable to the counter, so the body ran with
`v = 0`. A container literal is an object: build it, take its handle, iterate it (`iterKind`), and
let the unrolled print hook take the container tags too.

**Process lessons.** (1) The oracle before the verdict, again: `print(True)` printing `1` is pinned
debt (L11.1 step 1c), so membership rows are written `print(1 if x in s else 0)` — asserting raw
`x in s` "fails" against a bug that is already on the ledger and hides the real regression. (2) A
`--file` probe with `;`-separated statements can differ from the same program with newlines: the
lexer reports `;` as a diagnostic that some entry points enforce and `--emit-llvm` ignores; the
authoritative form is a real file, one statement per line. (3) The tree is shared with another agent
mid-session (it refactored `taggableMixedElem` under me and rewrote a test file while I was editing
it): re-compile and re-run the specific probe after any pause, and stage by path — never `git add -A`
— or a half-written foreign file lands in the feature commit.

## Cycle: L11.1 step 3 — reading a container back out of a slot (ADR 0241)

**What the row asked for.** ADR 0239 made a container element a handle plus a tag, but stopped at
storing and printing. The uses a program actually asks for — `len(xs[0])`, `xs[0][1]`, `d["a"][1]`,
`m[0][1]`, `t[0][0][0]`, `xs[0] == [1,2]`, `2 in xs[0]`, `for v in xs[0]`, `y = xs[0][1]` — were answered
by the interpreter and refused by the compiled backend, nine rows deep, with one message
("requires an inline list/dict/set literal") that blamed a shape in the source that was fine.

**The decision, and why it is not a runtime helper.** A read is allowed when the compiler can still
prove what the slot holds: `containerLiteralsOf` records names bound exactly once to a container
literal and forgets them on a rebind, an item assignment, a mutating method, or a hand-off to an
unknown callee. With that, the tag the builder wrote is the tag the object holds, and
`containerHandleOf` can hand the same handle to `len`, to a second subscript, to `==`/`in`, to `for`,
and to print — one question, one answer, so no use can be half-supported.

The alternative was written first and then deleted: `rt_container_slot`, which took a payload and asked
`@heap` whether the object at that index is a container. It is answerable and worthless — an `int`
payload is also an index, `@heap` has 1024 entries, so *something* is always there and would be printed.
`[[5]]` for `[[1, 2]]` is the same class as `[1, 'b', 'a']` for `[1, 'a', None]` (ADR 0226) and for the
`{'b': 'a'}` phantom (ADR 0239). Refusals are affordable; a plausible wrong value is not.

**Three refusals, not one.** The message is chosen by which question failed: mutated/rebound
(`cannot reach into xs's slots: …`), licensed-but-wants-a-bare-i32 (`cannot use an element of xs as a
plain number …`, which is the arithmetic half still open), and subscripting a number (`reaches past a
int in xs …`, where CPython raises `TypeError: 'int' object is not subscriptable`). One sentence for all
three would send a reader after the wrong thing, which is the same argument ADR 0240 made about `jit:`.

**Loop fall-throughs are the dangerous kind of "not supported yet".** `for v in <container expr>`
without a recognised iterable went to the counter/range path, which bound the loop variable to the
counter — printing `0, 1` where CPython prints `1, 2` — and, for a literal, emitted
`icmp slt i32 %_ctr1, @.set1`: a global in an `i32` slot, i.e. exit 2 charged to the compiler
(ADR 0166). Anything that "falls through to range" is a wrong answer with extra steps; the fix was to
recognise the iterable, not to catch the bad module later.

**Ledger mechanics.** Paying a probe is a promotion, not an edit: `probe_heterogeneous` moved from
`conformanceProbes()` to `conformanceStandalone()` and its oracle-registry row was deleted, because both
backends now print CPython's answer on every line. `probe_nested_list` stayed, narrowed to the shape that
genuinely still refuses (a container built by `append`, which no literal described). `TestNestedShapes
ThatStillRefuse` lost its two element-read rows and gained the mutated-container and plain-number ones —
the record of what is owed has to move with the work or it becomes fiction.

**Process lessons.** (1) The stale-binary trap: building `-o gusty` while probing with `./gustyc` makes
every measurement a lie about the previous edit. Build both names (or one canonical name) before
measuring; half a cycle went into "the fix does not work". (2) Expectations are assertions too: a table
that asserts "still refuses" is a standing claim about today's compiler, so paying a feature must rewrite
the row in the same commit — otherwise the suite reports a regression exactly when the work succeeds.
(3) Both-legs tables have to be *run* on both legs: the interpreter rows are what told me the compiled
refusal was the anomaly rather than the program being illegal.

## Cycle: Gap R.72 — `;` is a statement separator, so it is a token and never a diagnostic (ADR 0242)

**What the row measured.** `x = 5; print(x+1)` printed `6` under `--interp`/`--eval`/`--repl`, was
refused by `--jit`/`--aot` with `unexpected character ";"`, and `--emit-llvm` answered exit 0 with a
module `llc` accepted. One line of source, three verdicts — the shape of bug an agent reads as "the
backends disagree", verifies by trying a third flag, and loses a round to.

**Root cause, in three layers.** The lexer had no `;` case, so it fell through the operator scan to
`unexpected character ";"`. `filterLex` turned that token into an error diagnostic *and dropped it from
the stream*. The parser — which never looks at TokError — carried on parsing both statements. Then each
entry point kept its own opinion: `JITWithOptions` fails on any error-level diagnostic, `Compile`
collects diagnostics and returns the IR anyway, the interpreter never asks. Each layer was defensible;
the composition was the bug.

**The decision, and the trap in its first attempt.** Accept the separator (the parser already understood
the statements either side of it — forbidding it would have traded language surface for truth). The first
version dropped `TokSemi` the way the error token had been dropped, and `for i in [1,2]: print(i);
print("step")` printed `1 2 step` where CPython prints `1 step 2 step`: all three engines agreeing on a
wrong answer, which is the worst outcome this project has ever measured. An inline suite is a *list of
simple statements*, so the separator must stay in the token stream and `parseBlock`'s single-line branch
must loop over it, stopping at the NEWLINE that ends the physical line.

**Rules go in the parser when all paths must agree.** `a = 1;;b = 2` is the empty statement, and CPython
rejects it. Making it a lexer diagnostic would have rebuilt the same disagreement one level down —
interpreter runs it, JIT refuses it. As a `ParseError` it is one verdict, and the test asserts the
interpreter rejects it too.

**Two small things that were not small.** (1) `x = 5;` ends with a separator *followed by* the line's
NEWLINE: a statement loop that skipped newlines and separators in two separate calls handed `parseStmt` a
NEWLINE to parse as a statement. `skipSeparators` loops over both kinds. (2) The incremental parse cache's
`skipNewlinesAt` had to learn about separators too, or an edit between two `;`-separated statements shifts
the reused-prefix boundary by a token.

**Process lessons.** (1) *Build the binary you are going to measure.* Half of this cycle went into a
"the fix does nothing" chase because `go build -o gusty` and `./gustyc` were different files. (2) A probe
whose verdict differs between `;` and a newline is telling you something — my first sweep used `;`-joined
statements and drew conclusions about the wrong engine. (3) When a change removes a diagnostic, the test
that matters is "no error-level diagnostic is recorded" (`TestSemicolonIsNotADiagnostic`), not "the
message reads better" — that was ADR 0240's job, and it stays useful for programs with real syntax errors.

## L11.1 step 4 — a slot the compiler can see holding a number is compiled as that number (ADR 0243)

**The measurement.** ADR 0241 left one clause of the row open, and the two backends disagreed about it
out loud: `xs = [1, "a"]; print(xs[0] + 1)` was `2` interpreted and refused compiled, while CPython said
`2`; the same for `xs[0] > 2`, `-xs[0]`, `xs[0] / 4`, `xs[0] // 3`, `xs[0] % 3`, `f(xs[0])`, `d["a"][0] * 2`
and `t[0][0] + 1`. The refusal — *"this context needs a single static kind"* — was a true sentence about
the slot and the wrong diagnosis of the program: an addition does not need a *kind*, it needs a **value**,
and for a container the program spelled out and never mutated, the compiler already has that value.

**The decision.** Compile the element, not the slot. Where ADR 0241's compile-time promise holds and the
element is a numeric literal, the numeric use compiles the element expression itself and the existing int
and double paths run on it — `isFloat`, `floatValue` and `floatEval` ask the same question of the slot so
`xs = [1.5, "a"]; print(xs[0] * 2)` takes the float path and prints `3.0`. No new representation, no
runtime helper, no `tagOut` parameter: the runtime tagged-arithmetic engine is the general answer, and
today it would add a second arithmetic engine (rounding, `%`/`//` sign rules, division traps becoming wrong
answers instead of refusals) to buy exactly the programs the fold already buys.

**The soundness came from an unexpected place.** The first version resolved *any* element `staticElemExpr`
could name, and it was quietly wrong:

```gy
a = 1
xs = [a, "b"]
a = 5
print(xs[0] + 1)     # CPython: 2 — the slot holds the value a had when the list was built
```

The ADR 0241 promise is about the **object** ("nothing mutated this container"), not about what its
elements' names were bound to. Reading the variable at the point of use answers `10`. Restricting the fold
to literals and their negations makes the element and the slot unable to disagree — a restriction that
reads like a limitation and is actually the correctness argument.

**Refusals are still answers, and they must be checked as answers.** `xs[1] + 1` (a text element) and
`xs[0] + 1` where `xs[0]` is a container are CPython `TypeError`s; the trap table pins that CPython really
raises, that neither engine exits 0, and that neither prints a `Traceback` on stdout. A refusal that
*silently* matched a wrong number would have passed a stdout-only test.

**Process lessons.** (1) *Finish the wiring before believing a probe.* One of the two refusal sites had no
fallback wired, and I spent time "explaining" why `t[0][0] + 1` still refused; the answer was that the code
path I was reading was not the one that produced the message. Read every refusal's producer in the source
before theorising. (2) *Rebuild both binaries.* `./gusty` and `./gustyc` again differed mid-cycle. (3) A
probe that turns up an unrelated hole is a gift, not a detour: `max(xs[0], 5)` refusing on **both** engines
is missing surface (`min`/`max` take one argument here, two in CPython), so it became Gap R.73 instead of
being quietly folded into L11.1's row — and that discovery only happened because the probe compared against
CPython rather than between the backends. (4) The roadmap's ledger table is edited by *cell*, and a whole-row
paste can silently splice two rows into one; the row count is worth checking after a table edit
(`Gap R.71` briefly lost its identity to `Gap R.73` this way).

## Gap R.74 + Gap R.75 — a brace display ends at its brace, and a comprehension slot gets its tag (ADR 0244)

**The measurement came from the oracle, not from parity.** I swept comprehension programs through
`--interp`, `--aot` and `python3` side by side. Parity had nothing to say about any of it, because the two
backends were wrong *together*: `[{"k": x} for x in [1, 2]]` printed `1` for `len(d)` on both, and
`[1.5 for x in [1]]` printed `[1]` on both. Two engines agreeing on a wrong answer is the failure mode
this project keeps hitting, and the only cure found so far is a third engine that neither of them wrote.

**The wrongest answer was in the parser.** `[{1, 2} for x in [1]]` evaluated to a one-member set because
it *parsed* to a list containing a set comprehension — `ListLit[Comp(set, elems=[1,2])]`. The evaluator
was innocent; a `for` branch in `parseDictOrSet`, placed after the display's closing brace, had stolen
the enclosing list comprehension's clause. Reading the AST was five minutes; reasoning about the
evaluator would have been an afternoon. Dump the tree before you form a theory about the back end.

**Deleting a branch that serves two readings is the whole difficulty.** The post-`brace branch was not
gratuitous: `len({x*x} for x in xs)` depends on it, and it is in the suite. So the fix is contextual — the
parser counts the `[` displays whose element list it is parsing, and the branch runs only at depth 0 — and
pinned on *both* sides: the mis-parsed shape must not come back, and the call-argument reading must not
leave. Where two readings share a token, an AST table is cheaper than an output table, because an output
table can be satisfied by the wrong program.

**Four symptoms, one missing door.** Exit 2 (`@.set1` in a value position), `[1]` for `[1.5]`, `[0]` for
`[None]`, and a bare `a` for `['a']` all came from the comprehension builder writing elements with
`g.value` alone, while `xs.append(v)` had ADR 0187's payload-and-tag door. One detail deserves pinning:
after fixing the *object* (marking it self-describing), `print(xs)` was right and `print(xs[0])` was still
wrong, because the *binding* recorded the variable as an ordinary int list. A tag is only worth having if
you follow it to the read — half the pair is worse than useless, because the half that works is the half
you will test.

**Honesty about what is left.** Gap R.46 stays open, narrowed rather than closed: with a run-time-grown
text iterable the element still prints `0`. I built HEAD in a `git worktree` and confirmed the pre-change
binary answers `0` too before writing that into the row — "not a regression" has to be measured, not
assumed, and the worktree build is the cheapest way to get the answer. The call-argument genexp divergence
(`len({...} for x in xs)` is a `TypeError` in CPython, `2` here) is recorded in the ADR instead of being
asserted as if it were CPython's answer — a test that pins non-CPython behaviour without saying so is how
a wrong answer becomes a requirement.

## Gap R.46 + Gap R.76 — what a comprehension's loop variable knows (ADR 0244, continued)

**The most expensive mistake of the cycle was a probe that ran the wrong backend.** A guard in
`runtimeCompLoop` refused "a list whose elements are of more than one kind needs a tagged loop variable".
It looked dead: a `println` probe stayed silent through `./pkg/lang` and `./integration`, and six hand-run
programs printed the right answers, so I deleted it and wrote a comment calling the guard a lie about what
the compiler fears. Re-measured with `--aot` spelled out, the same six programs print `[1, 0, 0]` for
`[x for x in {1, "a", None}]` and `[0, 1]` for a dict's keys. The guard was the only thing standing between
those outputs and the user. Two rules, both now habits: **`--file` defaults to the interpreter**, so a
probe that does not name `--aot` is measuring the backend that has always been right; and "no corpus row
reaches this refusal" means *untested*, never *unneeded* — the shape the row exists for is exactly the
shape the corpus lacks, which is why the row exists.

**The guard's real bug was one registry.** It asked `mixedLists` and nothing else. A set's slots are
registered in `mixedSets`, a dict's in `mixedDicts`; neither was consulted, so those two walked the plain
load and printed payloads with no tag to make them mean anything — the same lesson as ADR 0238…0241 in a
new costume: an untagged read gives you the interned index and the fold's integer, and both look like
plausible small integers in a printed list. Where an answer agrees with no oracle, refuse and name the
missing thing; that is the exit-code contract's own logic, applied to values instead of modules.

**Half the pair again, and this time it was the *variable*.** Gap R.46's `out = [n for n in names if n ==
"a"]` printed `['a']` for `print(out)` and `0` for `print(out[0])`: the container knew its elements were
text, the binding did not. The reason is scope, not forgetting — the loop variable's `internedVars` facts
are alive inside the loop and gone when the result is assigned, so asking them at the assignment is asking
a question nobody can any longer answer. Asking the **iterated container**, which is still in scope and
still knows what its slots hold, is the fix. Generalisable: when a fact has to survive a scope, ask
something that outlives it, and write the test so it reads the container *and* a slot of it — a table that
only prints the container passes on the half-fix.

**A pinned-divergence test told me the bug was fixed before I looked for it.**
`TestPrintingAnElementOfAFreshComprehensionListIsPinned` asserted the *wrong* output (`1\n0\n`) and its
failure message said "if this is now `1\na\n` make it a parity case". It went red the moment the binding
was fixed, and promoting it was one edit. Pinning a known-wrong answer with instructions for its own
promotion is the cheapest form of regression insurance this project has; the ones that only assert the
right answer silently retire themselves when the bug is elsewhere.

**Process notes.** Expectations were taken from `python3` before any table was written (a set-of-strings
row had to go: CPython's own iteration order moves with the hash seed, so `{"a","b"}` is not a determinism
oracle), and one trap row had to be rewritten when the oracle revealed it dies on `None + 0` *first*, with
a different message than the one the table claimed — a trap table whose oracle does not fail as asserted is
worse than no row, because it lazes a guess as a fact. Full suites green: `./pkg/lang`, `./integration`,
`./cmd/gustyc` (42s / 155s / 48s).

## Gap R.76 + Gap R.77 + Gap R.78 — a comprehension's loop variable carries its element's tag (ADR 0245)

**A refusal costs coverage, and nobody audits the bill until the next cycle.** The previous cycle ended
with a door that refused any comprehension iterating a container whose slots mix kinds. Honest — and it
also refused `[k for k in d]` over `{"a": 1, "b": 2}`, a program this compiler has always been able to
answer. The row was written as "closed with a refusal", which is a phrase that should worry whoever reads
it next: a refusal is the right shape when the alternative is `[1, 0, 0]`, and it is a bug when the
alternative is available. Both sides belong in the record — what the refusal prevented *and* the ordinary
programs it took away — or the next cycle inherits a coverage hole with a green tick next to it.

**Two loops, one rule, and only one of them learned it.** `for v in d:` knows a dict entry is two words:
it scales its counter and asks `rt_dict_len`. The comprehension loop is a second implementation of "walk
this container" that had never been told, so it read slots 0 and 1 of a two-entry dict — one key and one
value — and returned them as the two keys. `[1, 0]` for `[1, 2]`, no mixed kind in sight, no guard in
sight either. The generalisation is uncomfortable but useful: where two constructs share a rule, the rule
should be one question both ask ("what kind of container is this name, and how do its elements lie?"),
because the divergence is invisible until somebody prints a wrong answer with a straight face.

**One wrong tag, two symptoms, in two subsystems.** `{k: 1 for k in d}` over a text-keyed dict printed
`{0: 1}` *and* died with `KeyError: key not found`: the loop variable held an interned index, the entry
write asked `elemKindTag` for a tag and got *int*, so the printer labelled it a number and the lookup —
which asks index **and** tag — could not find an entry its own printer had already mislabelled. The
tempting fix was the printer (it is the symptom you see), and it would have left the KeyError in place.
The rule this project keeps relearning: name the tag once, at the write, and test every reader of it.
Same lesson as ADR 0244's half-pair, one layer down.

**"Before" is a binary, not a claim.** The first draft of ADR 0245's table said the mixed-list-with-a-float
row used to print `[1, 2, 0, 3]`-shaped words. It did not: it *refused*, because the list-only door was
already in place. Building the two parent commits in a `git worktree` and running the same seven programs
through each (`/tmp/gusty_fa`, `/tmp/gusty_before`) turned a guessed column into a measured one, and the
table now says which binary produced which output. That also caught the fact that the mixed-iterable door
added nothing for `[1, 0]` — the stride defect walked straight out from both binaries — which is exactly
why Gap R.77 is its own row and not a clause of Gap R.76.

**Kept the honest refusals where they were.** `[x for x in xs]` over a compile-time-constant `xs`, the
`cannot reach into …'s slots` family, and `out[1] == "a"` (Gap R.79) still refuse by naming the missing
promise, and the table asserts exit 1 rather than exit 2 for each. Answering three shapes and refusing
three others is fine; answering three and answering three *wrongly* is what this cycle was for.

**Process.** Expectations from `python3` before writing any table (the set rows ask length/membership
because CPython's own string-set order moves with the hash seed); suites green at the end —
`./pkg/lang`, `./integration`, `./cmd/gustyc`.
