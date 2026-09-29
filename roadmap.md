# Pyre (gusty repo) — Roadmap

This file is the living, concrete plan for building and evolving **Pyre** (the
`gusty` repo), a Python-like language whose core is compiled ahead-of-time
through LLVM. It lives next to `AGENTS.md` and is the single source of truth
for *what exists*, *what is next*, and *what is gap-shaped*.

> Status snapshot (verified against the code, 2026): version `0.10.0`
> (`pkg/lang/compile.go`). ADRs run `0001`..`0194`. `go test -tags=llvm20 ./...`
> is green. Conformance corpus: 74 programs under `integration/programs/` (18 of
> them pinned probes), 66 matrix rows over **three legs** (interpreter, compiled binary, CPython): 48
> parity cases plus 18 pinned probes; oracle 32 `match` / 24 `debt` / 10 `not_applicable`, 0 drift.
> **The current plan is Phase 11 — the value model** (below);
> its harness, L11.9, is ✅ DONE (ADR 0186), so no remaining Phase 11 item may be
> marked done on parity alone — each one has a pinned program that has to change.

## Component map (state verified against the code)

| Component | File(s) | State |
|---|---|---|
| Lexer (INDENT/DEDENT) | `pkg/lang/lexer.go` | done |
| Parser → AST | `pkg/lang/parser.go`, `ast.go`, `token.go`, `types.go` | done |
| Semantic analysis / gradual typing | `pkg/lang/semantic.go` | static checks only |
| Interpreter backend + heap GC | `pkg/lang/jit.go` | full dynamic surface |
| AOT codegen (textual IR) | `pkg/lang/codegen.go`, `closure.go` | i32-first, see gaps |
| Optimizer | `pkg/lang/opt.go` | pure-Go textual dead-global elim. (not an LLVM `opt` pass) |
| Multi-file build | `pkg/lang/build.go` | done |
| Source maps / debug info | `pkg/lang/sourcemap.go` | AOT line/col + DWARF via `cc -g` |
| Standalone type-check (`gusty check`) | `pkg/lang/check.go` | mypy-style, ADR 0152 |
| Canonical formatter (`gusty fmt`) | `pkg/lang/fmt.go` | round-trips docstrings |
| Language server / LSP | `pkg/lang/lsp.go` | stdio; hover + completion + diagnostics |
| JSON schema / machine output | `pkg/lang/schema.go` | `--json` AST/IR dumps |
| Property/fuzz testing | `pkg/lang/proptest.go`, `proptest_test.go` | seeded cross-backend parity |
| CLI | `cmd/gustyc/main.go` | parse → semantic → (eval \| codegen → `llc` → `cc`) |
| Version constant | `pkg/lang/compile.go` | reconciled at `0.10.0` |

## Roadmap — phased plan

### Phase 0 — hygiene
- Version reconciled (v0.10.0 in `compile.go`, printed by CLI). ✅ DONE
- **New**: renumber the duplicated ADR `0143` (standalone-type-check) to `0152`;
  the file header and roadmap snapshot/component-map references now all say
  `0152`. ✅ DONE

### Phase 1 — AOT/interpreter parity
- Floats in AOT ✅ DONE — codegen emits real `double` IR (`fadd double 0.0, <const>`),
  float arithmetic with `sitofp` promotion, `%.17g` float print matching interpreter
  `%v`, `fcmp` float comparisons, and `double` allocas/stores tracked via `floatVars`;
  `float()` folds int/string literals to double constants.
- Runtime heap + GC in AOT (milestone 1..11) ✅ DONE — boxed mutable lists with
  `list.append()` on variables + `print(list)`, `len(x)` on runtime list vars,
  `x[i]` index read, free-list slot reuse on rebind, free on rebind-to-non-list,
  variable-index reads `x[a]`, runtime heap dicts, runtime heap sets,
  cross-collection rebind free, heap-stress/leak harness, heap bounds safety
  (`rt_alloc` -1 sentinel), conservative mark-and-sweep GC, closure env slots
  rooted across top-level GC boundaries. ✅ DONE

### Phase 2 — language surface
- Gradual typing (`--verify` checks assignment annotations) ✅ DONE
- Comprehensions (list/dict/set) ✅ DONE
- Slicing (`s[a:b]`, `s[::step]`, negative indices) ✅ DONE — interpreter + codegen
  (string *variables* limited to inline literals in AOT; see gap below)
- Augmented assignment (`+= -= *= /= //= %=`) ✅ DONE
- Tuple unpacking / multi-assign (`a, b = b, a`, `for a, b in ...`) ✅ DONE
- Membership + identity ops (`in`/`not in`, `is`/`is not` via `rt_contains`) ✅ DONE
- Power `**` (right-assoc binop; `math.Pow`/binary-exponentiation; `@llvm.pow.f64` in AOT) ✅ DONE
- Pattern-match depth: guards (`case x if cond:`), or-patterns (`case 1 | 2:`),
  dict patterns (`case {"k": v}:`) ✅ DONE (interpreter; AOT is expression-equality only — see gap)
- Class patterns (`case Point(x, y):` with subclass walk + aliases) ✅ DONE (interpreter-only; see gap)
- Operator overloading / dunder dispatch ✅ DONE (interpreter; AOT static-dispatch only — see gap)
- `with` / context managers + `yield from` ✅ DONE (interpreter; AOT emits protocol/loop but runtime blocked — see gap)
- f-strings (`f"..."` / `f'...'` with `{}` interpolation) ✅ DONE
- Docstrings + `__doc__` ✅ DONE (interpreter; AOT `__doc__` interpreter-only — see gap)
- FFI / `extern fn` → C calls ✅ DONE
- On-disk stdlib modules (`math`, `string`, `collections`, `json` — folded as AOT constants) ✅ DONE

### Phase 3 — tooling
- CLI pipeline (eval | codegen → `llc` → `cc`) ✅ DONE
- Multi-file build ✅ DONE
- Source maps + DWARF debug info ✅ DONE
- Standalone type-check mode (`gusty check`) ✅ DONE
- Canonical formatter (`gusty fmt`) ✅ DONE
- LSP server (stdio; hover, completion, diagnostics) ✅ DONE
- `--json` schema for AST/IR dumps ✅ DONE
- Seeded property/fuzz testing across both backends ✅ DONE

---

## Current gap-shaped work (fill before new features)

These are the *known* parity/robustness gaps — the next planned items, in
priority order. Each ships with a unit test + an `integration/` compile-and-run
case (and an ADR where the decision is non-obvious).

### Gap A — AOT dynamic-dispatch correctness (ADR 0151)
- **Status**: ✅ DONE — variable slots holding instance handles are registered
  as GC roots on every assignment form (single, augmented, tuple, loop,
  params), and the GC transitively marks reachable heap data (lists/dicts/
  instances), so a polymorphic receiver that survives many real GC
  collections + slot reuse dispatches on the *live* instance. All call sites,
  including expression-level `v.speak()` in function bodies and
  `make(1).speak()`, emit the runtime class-id dispatch switch.
- Verified: `dispatch_gc_stress.gy` conformance case allocates two receivers
  (Animal + Dog), then forces the 1024-slot heap to fill/free/reuse repeatedly
  (2000 throwaway lists), then dispatches on both — parity 43 (1+42),
  interpreter == AOT. Also `dispatch_gc.gy` (receiver survives later
  allocation) and `dispatch_nested.gy` (expression-level receiver in a
  function body).
- DoD: parity program dispatches identically on interpreter + AOT + JIT. ✅

### Gap B — AOT match / pattern exhaustiveness
- **Status**: 🟢 MOSTLY DONE — AOT `match` now lowers list-destructuring patterns (`[a, b]`), dict patterns (`{k: v}`), and class patterns (`Point(x, y)` with subclass-walk + attribute binding) to runtime IR via `rt_list_len`/`rt_get_elem`, `rt_dict_has`/`rt_dict_get`, and `rt_heap_kind`/`rt_inst_get`. Guards, `_`, and or-patterns already lower. Remaining: class-pattern aliases (`Alias = Point`) and bare-name binding edge cases.
- Port interpreter pattern semantics to codegen: guards, or-patterns, dict
  patterns, class patterns (subclass walk + attribute binding).
- ✅ DONE (Round 13): bare-name capture binding (`case x:`) now works in both
  backends — the interpreter binds the subject to the name and always matches,
  and codegen emits a fresh variable-slot store instead of comparing against a
  non-existent variable. Locked in by `match_test.go` unit tests
  (`TestMatchBareNameCapture*`) and the `match_baren` conformance program
  (23/23 conformance cases parity).
- **New**: exhaustiveness / irrefutability checking in `semantic.go` — a
  `match` over an `int`/enum-like subject with a `case _:` is exhaustive; a
  non-exhaustive `match` is a warning (mypy-style) and a definite-assignment
  analysis proves which bindings are definitely assigned after the `match`.
- DoD: every interpreter `match` case also lowers to AOT; a
  `match_exhaustive` integration program runs identically on both backends.

### Gap C — arbitrary (fnptr-valued) decorators
- **Status**: ✅ DONE — the canonical wrapping decorator
  (`def dec(g): def wrap(x): return g(x)+1; return wrap`) compiles and runs in
  AOT via compile-time specialization (`@f_orig` + `@f_impl` + funcBind).
- Add fnptr operands and indirect `call` lowering to codegen (`closure.go`),
  then support `@dec` where `dec(f)` returns a transformed function value.
- DoD: `@dec @dec2 def f` with wrapping decorators runs identically on both
  backends.

### Gap D — AOT `with` / `yield from` runtime (ADR 0140)
- **Status**: ✅ DONE (Round 17) — `with`/`yield from` pass AOT/interpreter parity.
- Round 17 root cause: generator accumulator lists (`genHandle`) were not GC-rooted, so the GC at body-statement boundaries collected/reused them; `yield from` then read a stale handle (double-appends, wrong sums). Fix: root each generator's `genHandle` and the `yield from` sub-list in rooted alloca slots; list-literal `yield from` now unrolls instead of treating a global as a heap handle. Regression tests: `TestParityYieldFromAcrossGC`, `TestParityYieldFromLiteral`.
  a runtime yield-from loop, but execution is blocked by a pre-existing
  duplicate-function defect (ADR 0140).
- Fix the duplicate-function lowering so the emitted protocol/loop actually
  runs; add parity programs for `with expr as name` + `yield from`.
- DoD: `with` and `yield from` integration programs run on AOT, byte-identical
  to the interpreter.

### Gap E — AOT float/`__doc__`/string-slicing leftovers
- **Status**: ✅ mostly DONE, small leftovers.
- `float()`/`round()`/`int()` paths now emit real `double` IR: the semantic
  analyzer recognizes the conversion builtins (`float`, `round`, `int`, `str`,
  `chr`, `ord`) so they pass `verify`/`--jit`, and the AOT codegen emits a
  real `double` for a general `float(x)` argument (via `floatValue`, which
  `sitofp`-converts ints and passes through floats). Parity test:
  `TestParityConversionBuiltins`. ✅ DONE.
  (`codegen.go:4449`) should emit real `double` IR.
- `__doc__` reads are interpreter-only: emit the folded docstring constant in AOT. ✅ DONE — `def.__doc__`/`Cls.__doc__` now folds to a string constant in the AOT backend (see `case *Attr:` in `value()` + `stringVal()`), with a JIT unit test (`TestJITDocstrings`) and an integration parity test (`TestParityDocstrings`).
- String slicing in AOT is limited to inline literals; support string *variable*
  slicing by lowering to the runtime `rt_slice` helper.
- DoD: no `interpreter-only` branch remains in codegen for a tested feature.

### Gap F — AOT operator overloading (dunder dispatch)
- **Status**: ✅ DONE (Round 16)
- `emitDunderBinOp` in codegen.go lowers a BinOp with a statically-known class
  operand to a direct call to the class's dunder (`__add__`/`__mul__`/`__lt__`…)
  or reflected (`__radd__`/`__rmul__`/swapped comparisons) method, mirroring
  jit.go's `evalBinOp` dispatch order (left dunder first, then reflected right).
- Falls back to builtin arithmetic/comparison when no overloading applies.
- Conformance program `integration/programs/dunder.gy` covers left/reflected
  dispatch, `__lt__` comparisons, and non-instance fallback; interp==AOT.
- Limitation: AOT dispatch is static (varClasses/receiverClass), so dunder only
  fires on operands known to be instances at compile time (direct `Vec()`
  assignment or `self` in a class body), matching the existing static-method AOT.

### Gap G — AOT `in`/`not in` on inline literal containers
- **Status**: ✅ DONE (ADR 0156) — literal list/set/dict membership is unrolled
  to `l == elem` comparisons against the constant integer elements/keys
  (dict tests keys), empty literals fold, `not in` inverts.
- DoD: `TestParityLiteralMembership` — `x in [1,2,3]`, `x not in [1,2,3]`, `x in {1,2,3}`, dict-key `in`, and empty-container `in`/`not in` all match the interpreter on AOT.

- **Gap H — LLVM `opt` pipeline** — ✅ DONE — real `opt` is invoked when installed
  (`runLLVMopt`); the deterministic textual fallback (`optimizeTextual`) runs
  const-fold, promote (mem2reg), deadHeapElim, scalarRepl (SROA), dead-block,
  and dead-global passes.
- **SROA follow-on** — ✅ DONE — `scalarRepl` promotes a non-escaping heap list
  object whose every element access uses a constant index to registers: each
  `rt_get_elem(h, N)` is rewritten to the value the most recent
  `rt_set_elem(h, N, v)` stored, and the `rt_alloc` + all `rt_set_elem` are
  deleted. It fires only within a single basic block (program order makes the
  stored value unambiguous) and skips escaping objects, appends, and reads of
  unwritten indices.

- **Gap I.1 — AOT heap containers across function boundaries** — ✅ DONE
  (ADR 0161). A list/dict/set is an i32 handle into the runtime heap; getting
  one through a call boundary needed both halves fixed, and each had a
  different failure mode:
  - *call site*: a container literal was lowered to its compile-time global and
    passed as `i32` (`call i32 @total(i32 @.lst1)`), which `llc-20` rejects with
    "global variable reference must have pointer type". Literal arguments are
    now materialised with `rt_alloc`/`rt_set_elem`/`rt_set_add`/`rt_dict_put`
    and passed by handle — including globals produced by constant-folded
    comprehensions.
  - *callee*: a parameter was an unknown-shape integer, so `for x in xs`
    silently compiled into a `0..handle` range loop (wrong answers, no error).
    A whole-module inference (`pkg/lang/heapargs.go`) now types each parameter
    as list/dict/set from its annotation, its default, or any call site —
    propagated to a fixed point so forwarding calls
    (`def doubled(xs): return total(xs)`) work — and the body uses
    `rt_list_len`/`rt_get_elem`/`rt_dict_get` accordingly.
  Covered by `pkg/lang/heapargs_test.go`, `integration/heap_args_test.go`, and
  the `programs/heap_containers.gy` conformance case (interpreter/AOT stdout
diff
  asserted in the matrix).

- **Gap I.3 — assigned containers are heap handles (AOT)** — ✅ DONE (ADR 0163).
  The same class of bug at the *binding* site, all three surfaced by the L8.2 module
  verifier: assigning a constant-folded comprehension emitted `store i32 @.lst1, i32*
  %_ys` (rejected by LLVM) and printed an address; module-level `xs = []` left the
  variable without a slot, so `xs.append(i)` referenced an undefined `%_xs` and `rt_gc`
  could not see the handle; and `funcDef`'s per-body state leaked into `main`, letting a
  parameter's alloca be reused by module code. Assignments now materialise folded lists
  (`rt_alloc`/`rt_set_elem`), module definitions allocate + root their slot, module code
  starts a fresh binding scope, and the parameter-kind fixed point merges in sorted order
  (determinism). Covered by `programs/folded_lists.gy` plus unit/integration cases.
- **Gap I.2 — strings inside runtime containers (AOT)** — ✅ DONE (ADR 0173).
  The heap stores i32 slots while a string is a compile-time global, so
  `xs = ["a", "b"]` emitted `rt_set_elem(i32 %h, i32 1, i32 @.str1)` and LLVM rejected the
  module — reporting a two-line program as a compiler bug (exit 2). The runtime now keeps an
  **interned string table**: `rt_str_intern2(text, repr) -> i32` appends distinct texts to
  `@str_tab` by `strcmp` (content-addressed, so two separate `"k"` literals are the same dict
  key) with the Python repr form in the parallel `@str_repr_tab`; container slots hold the
  index. `rt_print_value(v, isStr, quote)` picks the slot by context, so `print(x)` prints raw
  text and `print(xs)` prints `['a', 'b']` — and `rt_print_list_str` / `rt_set_print_str` /
  `rt_dict_print_s` all share that one helper, which is why list, set and dict rendering
  cannot drift apart.
  - Element kinds are tracked per container and per dict side — `listElemStr`, `setElemStr`,
    `dictKeyStr`, `dictValStr` — saved and restored in `beginScope`, so `{'ada': 3}` and
    `{1: 'one'}` both render correctly and one function's `xs` never describes another's.
  - `heapElemKind(b, e)` is the single place a container word is produced (folded int passes
    through, string interns, anything else is the ADR 0166 diagnostic); every store site goes
    through it — append, add, setitem, list/dict literal, dict read by key, and the `in`
    needle, which was the last shape still emitting `rt_contains(i32 %h, i32 @.strN)`.
  - Working and byte-identical in both backends, checked against CPython: string list literal
    with `len`/index/iteration/`in`, `append`, `xs[0] = "s"`, `s.add`, `d["k"] = v`,
    `d[1] = "v"`, dict reads by string key, string lists passed to functions, and Python's
    repr rules (`set()`, `["it's", 'plain']` — single quotes unless the text has `'` and no `"`).
  - The interpreter's `Repr` was fixed alongside: container elements route through
    `reprNested`, and dict keys print via `Repr` — `%v` used to print the heap handle
    (`{1048581: 1}` for `{'k': 1}`).
  - Covered by `integration/string_containers_test.go` (20 shapes × both backends × CPython's
    output), `pkg/lang/string_containers_test.go` (IR invariants: interned store, no
    `i32 @.str` anywhere, repr table emitted, quote rule) and the new conformance program
    `string_containers.gy`. `TestIntContainersStillBuild` keeps the guard from becoming a
    blanket refusal.
  Remaining, deliberately: an element whose string comes from a `str`-typed **parameter** has
  no compile-time text to intern and still reports the ADR 0166 diagnostic.
  Note: `Callable` parameters called through the parameter (`def apply(f, x):
  return f(x)`) are likewise unsupported in AOT (`codegen: unsupported call "f"`)
  — the L6.6 Callable surface is therefore checker-only for now.

---

## New work — modern 2026 language-design roadmap

These are *new* phases layered on top of the existing DONE work. Each item is
a first-class roadmap entry with an ADR, a unit test, and an `integration/`
program where relevant. Order reflects dependency: front-end (lexer/parser)
first, then semantics/type system, then runtime, then codegen, then tooling.

### Phase 4 — lexer modernization (2026)

**Goal: a resilient, position-rich lexer that supports a modern source surface.**

- **L4.1 Error-recovering lexer** — on an unexpected character, emit a
  `TokError` token carrying the span + message and *resume*, instead of
  aborting the whole file. The parser/semantic can then report multiple
  diagnostics per run (feed the LSP). ✅ DONE (ADR 0177) — the lexer recovers
  (`emitErr` appends a `TokError` and keeps scanning) and the parser reports a
  `ParseErrors` forest, but the last hop was discarding both: `CheckSource` /
  `CheckFile` returned a Go error, which threw away `prog.Diags` (the precise
  `unexpected character "$"` spans), collapsed the forest into one `Error()`
  string, skipped `Analyze` — so type errors in statements that *had* parsed
  went unreported — and left `--json check` printing prose. A failed parse is now
  data: one `Diagnostic` per recovered error with the stable `parse.error` code,
  sorted into document order, and the statements that parsed are still checked.
  One run on a file with two bad lines yields all three diagnostics in both
  shapes.
- **L4.2 Rich token spans** — each token carries `start` AND `end` (byte +
  rune offsets), plus an optional multi-line flag, so f-strings, slices, and
  `match` patterns have exact ranges for hover/diagnostics/formatting. ✅ DONE —
  verified against the code (probed, not assumed): `Token` carries `Start`/`End`
  byte offsets and `StartRune`/`EndRune` rune offsets plus `Multiline`, and a
  triple-quoted string token reports `multiline=true` with exact rune ranges.
- **L4.3 Unicode identifiers** — accept the full `XID_Start`/`XID_Continue`
  classes (not just ASCII), with NFC normalization + a clear diagnostic for
  confusables (e.g. `l` vs `1`, `Ο` vs `O`). ✅ DONE — lexer now decodes UTF-8
  runes and scans identifiers by Unicode `ID_Start`/`ID_Continue` categories
  (letters + Nl + Other_ID_Start additions for start; plus marks, digits,
  connector punctuation, and Other_ID_Continue for continuation), NFC-normalizes
  identifier text via `golang.org/x/text/unicode/norm` (so decomposed and
  precomposed spellings are one symbol), and emits a `TokWarning` →
  `LevelWarning` diagnostic for Greek/Cyrillic homoglyph lookalikes (e.g.
  `Ο` U+039F vs Latin `O`). ASCII-start identifiers may continue through
  multi-byte runes (e.g. `café`). Added `TokWarning` token kind; parser
  collects warnings into `prog.Diags` for the CLI/LSP warning path.
- **L4.4 Numeric-literal modernization** — `0xFF` hex, `0b101` binary,
  `0o17` octal, and `1_000`/`0x_FF` digit-group separators; keep exact
  integer semantics, reject `_` misuse. ✅ DONE (ADR 0153)
- **L4.5 Raw strings + triple-quoted strings** — `r"..."`/`R'...'` raw strings (no escape processing; a `\` before a quote keeps the string open) and `"""..."""`/`'''...'''` triple-quoted strings (may span lines); raw-triple `r"""..."""`; docstring extraction reuses both forms; formatter preserves raw/triple forms. — ✅ DONE
- **L4.6 Line continuation** — trailing `\` at end of line joins the next
  physical line into one logical line (Python-compatible), so long call
  argument lists don't force parens. — ✅ DONE (lexer skips the continued
  line's leading indentation and blank/comment-only continuation lines;
  unit + integration tests)
- **L4.7 Token-stream cursor API** — a small cursor/`peek(n)`/`mark()`
  abstraction shared by parser, formatter, and LSP so all three walk the same
  stream (single source of truth for spans). ✅ DONE — `Cursor` in
  `pkg/lang/token.go` with bounds-safe `peek(n)`, `next()`, `mark()`/`reset()`,
  `position()`, and `atEOF`/`atNewline`/`atDedent`/`atIndent`/`skipNewlines`;
  the parser now walks the shared cursor (its `toks`/`pos` are gone), so the
  token stream and spans are a single source of truth; unit tests in
  `pkg/lang/cursor_test.go` (lookahead, mark/reset backtracking, EOF safety,
  predicates).

### Phase 5 — parser modernization (2026)

**Goal: a fast, error-tolerant Pratt parser with a modern syntax surface.**

- **L5.1 Pratt / precedence-climbing parser** ✅ DONE — `parseExprPrec` in
  `pkg/lang/parser.go` is a precedence-climbing loop over a `prec` table (unary,
  `**` right-assoc, multiplicative, additive, comparison, `and`/`or`, ternary);
  new operators are one table entry + one `binaryOp` case. Covered by
  `pkg/lang/parser_pratt_test.go` (associativity + precedence rendering).
- **L5.2 Panic-mode error recovery** — on a parse error, skip to the next
  statement/block boundary and keep parsing, producing a forest of
  `ParseError`s (not just the first). Feeds the LSP + `gusty check`. ✅ DONE
  — `parseProgram` collects a forest of `*ParseError`s via `recoverStmt()`
  (nest-aware INDENT/DEDENT skipping), returns a partial AST plus an
  aggregate `*ParseErrors`; the LSP reports each error as a separate
  diagnostic and still indexes the partial program; `gusty check`/`verify`
  print the whole forest.
- **L5.3 Trailing commas** — allow `f(a, b,)`, `[1, 2,]`, `{1: 2,}` and
  `match` case arg lists, for clean diffs and formatter round-trips.
  ✅ DONE — call args, list/dict/set literals, tuples (incl. `(a,)` 1-tuple),
  and match class-pattern arg lists all tolerate a trailing comma; the
  canonical formatter normalizes them away and its output re-parses cleanly.
  Covered by `pkg/lang/trailing_comma_test.go`.
- **L5.4 ✅ DONE — Walrus operator (`:=`)** — assignment expressions usable inside `if`
  conditions and comprehensions (`if (n := len(x)) > 0:`); scope rules per
  Python 3.8+.
- **L5.5 ✅ DONE — Union-type syntax `int | str`** — parse `|` in annotation position
  (and in `match` patterns) as a union type, not a bitwise-or; feed the
  gradual type checker.
- **L5.6 `async`/`await` + effectful syntax** ✅ DONE (this round) — parse `async def`,
  `await expr`, `async for`, `async with` as first-class syntax. `async`/`await` are lexed keywords; `async` sets the `Async` flag on
  `FuncDef`/`ForStmt`/`WithStmt`; `await e` reduces to `e` under the minimal synchronous-coroutine model (no suspension
  primitives yet), so both the interpreter and the AOT/JIT backend run async programs identically to their sync
  counterparts (conformance parity `async_basic.gy`). The cooperative event-loop runtime (coroutines, async protocols,
  a first-class `AwaitExpr`) is Phase 7 (L7.1).
- **L5.7 Type aliases `type X = ...`** ✅ DONE (this round) — parse alias declarations into a `TypeAliasStmt`; the type checker resolves references structurally (not nominal) by default via parse-time substitution of a structural copy. Aliases are compile-time no-ops in the interpreter/codegen/formatter; conformance case `typealias.gy`, ADR 0159, and unit tests.
- **L5.8 Incremental parse** — a stable parse tree keyed by spans so the LSP
  and REPL can re-parse only edited ranges (feeds incremental JIT in Phase 9).
  ✅ DONE (ADR 0176) — `ParseCache` now splices **three** regions on every
  `didChange`: preserved prefix statements, a re-parsed middle, and preserved
  **tail** statements. Previously reuse was prefix-only, so editing line 1 of a
  21-statement file reused `0` statements and re-parsed all 21 (measured). Tail
  reuse is allowed only when it cannot make a span lie — one edit, confined to
  one line, adding no line, and the text after it byte-identical apart from a
  constant shift — and `parseTopLevelRange` parses a token window instead of the
  rest of the file. `publishDiagnostics` gained a `parseCache` self-report
  (`statements`, `reusedStatements`, `incremental`) so an editor or agent can see
  the work instead of timing the server, and a change that fails to apply now
  keeps the previous buffer and warns instead of replacing it with the change's
  fragment (which is what the old `SetText(last.Text)` fallback did).

### Phase 6 — semantics & type-system growth (2026)

**Goal: move `semantic.go` from static checks toward a real gradual checker.**

- **L6.1 Exhaustiveness checking for `match`** ✅ DONE — prove a `match` covers all
  subject shapes (int ranges, unions, wildcard); warn on non-exhaustive;
  this is the semantic half of Gap B.
- **L6.2 Definite-assignment analysis** ✅ DONE — warn which names are definitely
  assigned on every path through `if`/`match`/`try`; warn on possibly-unbound
  reads (mypy-style) before codegen.
- **L6.3 Union types** (`int | str`, `None | int` sugar for `Optional`) —
  infer/check unions through assignment + call boundaries; AOT widens to a
  tagged union layout. ✅ DONE (semantic inference: ternary widening +
  union-aware arithmetic, ADR 0158). ✅ DONE (runtime: union-annotated variables — `int | str`, `int | float` — are accepted and evaluated by the interpreter; `checkAnnot` accepts any union member; conformance + IR tests added). ✅ DONE (AOT tagged-union lowering: union-annotated scalar variables (`int | float`, `int | str`) get a tagged `%unionbox` slot; assignment stores the runtime member tag (0=int, 1=float, 2=str); print dispatches on the tag to emit `%d`/`%f`/`%s`, so an int member prints as `%d` and a float member as `%f` even after cross-member reassignment under branches/loops).
- L6.4 Literal types — `Literal[1, 2]` so `match` on constants enables exhaustiveness + narrowing; feed L6.1. ✅ DONE (this round)
- **L6.5 Type narrowing / refinement** — after `if isinstance(x, int):`, the
  checker narrows `x` from `any` to `int`; after `match case 1:`, narrows to
  literal `1`. Drives better AOT layout (Gap A). ✅ DONE — static isinstance-if
  narrowing (then/else branches, `not` flip, union complement via `dropType`)
  is implemented in the semantic checker (`narrowFromCond`/`analyzeNarrowed`)
  and unit-tested (`TestNarrowIsInstanceThen/Else/Not`). Match-literal narrowing
  to `Literal[1]` was already present (L6.4).
- **L6.6 Variance + generics** ✅ DONE (ADR 0160) — one structural subtyping
  relation (`subType` in `pkg/lang/variance.go`) implements a declared variance
  table: `list[T]`/`set[T]`/`dict[K, V]` **invariant** (writable containers),
  `Sequence[T]`/`iter[T]`/`tuple[...]` **covariant** (read-only, element type may
  widen, arity fixed), `Callable[[P...], R]` **contravariant** in parameters +
  covariant return, user classes **nominal** over the declared base chain
  (`ClassIndex`, filled by a statement-tree pre-pass so annotations may name a
  class before its declaration). Protocol structural subtyping covers
  `Sequence`/`Callable`; a *freshly built* container literal is checked
  covariantly (`x: list[int | str] = [1]` is accepted, `x: list[int] = ["a"]` is
  not). `gusty check` **reports contravariant misuse** (and every other rule)
  with a stable `Diagnostic.Code` (`type.variance.invariant` /
  `.covariant` / `.contravariant` / `.nominal`, `type.callable.arity`,
  `type.union.members`) plus an actionable `Suggestion`; `gustyc --variance`
  prints the machine-readable table and `--schema` declares the `diagnostic` /
  `varianceRule` shapes. Function symbols now carry their declared parameter
  annotations (`TFunc(annots, ret)`), which is what makes contravariant
  substitution observable. Unit tests: `pkg/lang/variance_test.go`; parity +
  machine path: `integration/variance_check_test.go`, `programs/variance.gy`.
- **L6.7 Call-graph + reachability** ✅ DONE — compute a module call graph so
  dead-global elimination (`opt.go`) is precise and `__doc__` folding is
  reachable-driven.

### Phase 7 — runtime: concurrency, effects, precise GC (2026)

**Goal: a deterministic async core + memory-safety hardening.**

- **L7.1 Async runtime (`async`/`await`)** ✅ DONE — cooperative async in the
  interpreter: `async def` returns a *coroutine object* (deferred thunk; the
  body does not run at call time), `await` runs it to completion (deterministic,
  race-free), and `async for` awaits each coroutine element. AOT keeps eager
  semantics; parity holds because awaited async calls run once to completion in
  both backends (codegen `await` evaluates its operand). `async with` remains
  eager (`__enter__`/`__exit__`); mid-body suspension and `__aenter__`/`__aexit__`
  protocols are future work.
- **L7.2 Precise stack roots** — replace conservative mark-and-sweep with
  precise rooting: the GC knows exactly which stack slots/registers hold
  handles (fixes Gap A's instance-layout bug at the root cause).
  ✅ **DONE (this round, ADR 0181)** — both backends.
  *Interpreter*: the root set is `Vars` ∪ active call frames ∪ declared root groups ∪
  permanent roots; a watermark makes everything allocated after the last safe point
  unconditionally live; safe points are statement boundaries with an empty expression
  stack, plus the body of a *statement-root* call. Collection now actually runs while
  programs execute (it used to be reachable only from tests), a loop's garbage is
  reclaimed, a 2000-input REPL session stays bounded, and `GUSTY_GC_STRESS=1` +
  `integration/gc_stress_test.go` stress the whole corpus under a collection at every
  statement. Stubbing `pushFrame` makes `TestGCFramesKeepRecursionLive` fail with
  `cannot index null`.
  *Compiled backend*: the static per-(scope,name) root table became a **root stack** —
  `@gc.roots` + `@gc.kinds`, `rt_root_put`/`rt_root_clear`/`rt_frame_open`/
  `rt_frame_close`; prologues open a frame, every handle store pushes (deduped per
  frame), scalar stores tag the entry dead, every return and unwind edge pops, and
  `rt_gc` traces only tagged entries while counting what it skipped. Fixing it exposed
  two latent codegen faults it now depends on: a variable's slot must be allocated once
  per call (`hoistAllocas` — an `alloca` left in a loop body changed address every
  iteration: `top` hit 2002, the 1024-slot heap filled, the program died), and every
  function-emitting path including `emitClassMethod` must pop what it pushed. The
  instance-layout bug that Gap A worked around is rooted out at the cause.
  *Machine path*: `GCStats`, `--gc-stats` (either backend, stderr), the `gc` member of
  `--json`, `definitions.gcStats` (incl. `top`) in `--schema`, and
  `lang.ParseGCStatsLine` ↔ `GCStats.String` round-trip. `gc_precise.gy` is in the
  conformance matrix; the AOT half is pinned by structural invariants
  (`pkg/lang/gc_roots_ir_test.go`) and by runtime assertions calibrated against stubs
  (no frame pop → "19 objects still live"; no hoisting → "root stack grew to 1210").
- **L7.3 Tagged pointers / NaN-boxing** — box small ints and floats in the
  payload so `int`/`float`/`bool` avoid heap allocation; pairs with L7.2 for
  a compact, allocation-free fast path.
- **L7.4 Refcount + cycle-collect hybrid** — refcount for acyclic data (fast
  reclaim), tracing collector for cycles; deterministic pause for the AOT
  path.
- **L7.5 Algebraic effects** — a `raise`/`yield`/`await` effect system as a
  first-class control-flow model in codegen, unifying exceptions, generators,
  and async (one lowering, one runtime).
- **L7.6 Effect/async exhaustiveness** ✅ DONE — the await/return discipline is a
  semantic check, not a runtime promise: `async.coro.never_awaited`,
  `async.coro.awaited_twice` and `async.generator.unsupported` refuse programs whose
  async meaning is broken, `async.await.outside_coroutine`,
  `async.async_stmt.outside_coroutine`, `async.await.not_coroutine` and
  `async.missing_return` warn where the program still means something. The proof
  (effect signatures + a flow-sensitive coroutine-liveness walk) is shared by both
  backends and machine-readable: `gustyc --effects` (ADR 0195). See Gap R.
- **L7.6a Deferred coroutines in codegen** ⏳ PLANNED — the compiled backend still
  lowers a coroutine construction as a call, so a coroutine created early performs its
  effects early. Pinned as `programs/probe_async_eager`: the interpreter prints
  `between / effect 1 / 2`, the compiled binary `effect 1 / between / 2` (ADR 0195).

### Phase 8 — codegen: monomorphization, verification, autovectorization (2026)

**Goal: make AOT a first-class optimized backend.**

- **L8.1 Generic monomorphization** — instantiate `list[T]`/`dict[K,V]` per
  concrete type at compile time (no runtime generics), enabling scalar
  replacement (Gap H) and boxing elimination (L7.3).
- **L8.2 `verifyModule`-driven pipeline** — ✅ DONE (ADR 0164). Verification is a
  pipeline stage, not a side effect of linking: `VerifyModuleIR` runs
  `opt -passes=verify` (plus the requested `-O` pipeline, following `--opt-level`)
  and falls back to `llc -filetype=null` when `opt` is absent. `Build` verifies the
  module it is about to link and carries the verdict in `BuildResult.verification`;
  `gustyc --verify-llvm <src>` exposes it (human line, or `--json` →
  `irVerification`: `ok`/`tool`/`skipped`/`pipeline`/`errors`/`note`/`toolchain`),
  with `--no-verify` to opt out. A missing toolchain is `skipped`, never `ok`.
  Finding: switching this on inside `Build` immediately exposed three container
  codegen bugs (Gap I.3, ADR 0163) that no test could see.
- **L8.3 Autovectorization** — annotate loop/array IR so LLVM vectorizes hot
  numeric loops; add a `--report=vector` output showing which loops vectorize.
- **L8.4 SROA/scalar-replacement** — promote non-escaping heap objects to
  registers (the Gap H follow-on), now driven by monomorphization (L8.1).
- **L8.5 Debug line tables in IR** — emit `!dbg` records from `sourcemap.go`
  spans so DWARF (already wired via `cc -g`) shows exact source lines.

### Phase 9 — tooling: incremental JIT, package manager, richer LSP (2026)

**Goal: a modern developer loop.**

- **L9.1 Incremental JIT REPL** — recompile only edited ranges (L5.8) through
  the LLVM JIT; hot loop retains registers across edits; `--repl` mode.
- **L9.2 Package manager (`gusty install`/`publish`)** — a module registry for
  on-disk stdlib + user packages; `extern fn` + FFI bindings per package;
  reproducible lockfile.
- **L9.3 Incremental build cache** — key module IR/objects by source hash +
  dependency graph (L6.7); only rebuild dirty subgraphs (`gusty build --cache`).
- **L9.4 Richer LSP** — go-to-definition, find-references, rename, hover type
  display (uses L6.3/L6.5 narrowing), inline error squiggles from the
  error-recovering lexer/parser (L4.1/L5.2).
- **L9.5 Formatting on save** — `gusty fmt` as an LSP `textDocument/format`
  provider, round-tripping f-strings, trailing commas (L5.3), and docstrings.
- **L9.6 Fuzz/parity CI** — extend `proptest.go` to differential-test the
  lexer/parser (parse → format → reparse) and both backends on the new
  surface (unions, async, walrus), seeded + deterministic.

### Phase 10 — cross-cutting: targets, ABI, portability (2026)

**Goal: Pyre runs anywhere.**

- **L10.1 WASM target** — lower AOT to WebAssembly via LLVM; `gusty --target=wasm`
  for browser/edge runtimes; the scheduler (L7.1) maps to `wasm` event loop.
- **L10.2 ABI stability** — a versioned, documented C ABI for `extern fn` exports (stable struct layout for unions/tagged values across releases). ✅ DONE (Round 2)
  exports (stable struct layout for unions/tagged values across releases).
- **L10.3 Shared-library export** — `gustyc --build <out> --shared` emits a
  position-independent `.so`/`.dylib` with the stable ABI (`BuildShared`,
  `cc -shared -fPIC`; object already PIC via `llc -relocation-model=pic`;
  versioned ABI marker carried, so extern exports stay stable across
  `dlopen`/loads). ✅ DONE (Round 3)
- **L10.4 Benchmark harness** ✅ DONE — `gustyc --bench-suite` measures a corpus
  (built-in, or every `*.gy` in `--bench-dir`, so the `integration/` parity
  programs become benchmark cases) on both execution backends and emits a
  versioned JSON artifact; `--bench-baseline` gates a run against a saved
  baseline (best-of-N on the AOT leg, tolerance + noise floor, per-regression
  `suggestion`) so a slowdown surfaces as a number with its own exit code (5).
  See ADR 0162, `docs/benchmark.md`.

### Phase 11 — one tagged value model: nested data, real tuples, code-point strings (2026)

**Goal: a *value* is one thing everywhere.** Every divergence still found by
probing against CPython, and every remaining invalid-module / crash /
"unsupported" defect in the compiled backend, has the same root: an AOT value
is an **untagged `i32`** — a handle, an interned string index, or a compile-time
global name — so it cannot say *what it is*. It cannot nest, it cannot mix kinds,
it cannot carry `True` vs `1`, and the moment the compiler must decide, it either
guesses (wrong answer) or refuses (an `llc` rejection the exit-code contract then
blames on the compiler). Phase 11 fixes the representation **once**, and absorbs
the representation-shaped half of Gaps J.2, J.6, L.2, N.2, P.1 and P.2 instead of
patching each symptom.

**Why now (measured, `gustyc` at `b704a14`, corpus green).** Parity compares the
backends to *each other*; the CPython oracle exists in only two files
(`integration/escapes_test.go`, `integration/division_test.go`), so a construct
*both* backends get wrong is invisible to CI. Each row below was run through
`--interp`, `--aot` and `python3` on the same source, today:

| program | CPython | `--interp` | `--aot` |
|---|---|---|---|
| `print(True)` | `True` | `1` | `1` |
| `print([1, "a"])` | `[1, 'a']` | `[1, 'a']` ✅ | refused (Gap J.6 diagnostic) |
| `m = [[1,2],[3,4]]` / `print(m[0][1])` | `2` | `2` | `index requires an inline list/dict/set literal` |
| `d = {"a": [1,2]}` / `print(d["a"][1])` | `2` | `2` | same refusal |
| `xs = []` / `xs.append([1,2])` | ok | ok | **invalid IR**: `call void @rt_append(i32 %h2, i32 @.lst1)` |
| `sa = {x for x in [3,1,2]}` | `{1, 2, 3}` | `{3, 1, 2}` | **invalid IR**: `store i32 @.set1, i32* %_sa` (Gap J.2) |
| `print(sorted([3,1,2]))` | `[1, 2, 3]` | ✅ | **invalid IR**: `printf(... , i32 @.lst1)` |
| `t = (1,2,3)` / `print(t[1]); print(t)` | `2` / `(1, 2, 3)` | `2` / `[1, 2, 3]` | `unsupported expression *lang.Tuple` |
| `xs = [1,2,3]` / `print(xs[-1])` | `3` | `IndexError` | `IndexError` |
| `print([1,2,3][-1])` | `3` | `IndexError` | **Go panic** in `irGen.value` (`codegen.go:4314`) |
| `print("abc"[-1])` | `c` | `IndexError` | `string index out of range` |
| `len("café")` / `"héllo"[1]` | `4` / `é` | `5` / `195` | `5` / `195` (Gap N.2) |
| `for c in "aé": print(c)` | `a`,`é` | ✅ | **invalid IR**: `store i32 @.str1, i32* %_c` |
| `def f(x): return x*2` / `print(f(0.1))` | `0.2` | `0.2` | `0` (Gap P.1) |
| `print(-7 // 2)` / `x=8; x/=2; print(x)` | `-4` / `4.0` | ✅ | `-3` / `4` (Gap P.1) |
| `print(-3.5 % 2.0)` | `0.5` | `-1.5` | `-1.5` (Gap P.2 — **both** backends) |
| `import math; print(math.PI)` | `3.141592653589793` | ✅ | `3` (stdlib constants fold to int) |
| `def apply(f, xs): … f(x)` | works | works | `unsupported call "f"` (ADR 0161 note) |
| `class B(A)` / `A.__init__(self, x)` | `3` | `3` | **SIGSEGV** in the JIT (cgo) |
| `def m(self): return "hi"` (called) | `hi` | `hi` | **invalid IR**: `ret i32 @.str1` |

**Items (each: ADR + unit test + `integration/` program, per the Definition of
done).** L11.9 is ✅ DONE (ADR 0186) — its 16 probe programs are the measured form of every
row below, so an item is not done until its probe's pins change. Order is dependency order —
L11.1 is the keystone; do not
start L11.3/L11.4/L11.5 before it, or they re-decide the representation locally.

- **L11.1 — Tagged value word (both backends)** 🟢 IN PROGRESS — one value shape on
  both sides: the interpreter's `value.go` tag set and a compiled `rt_value`
  (`{i64 payload, i64 tag}`, or a boxed slot with a parallel tag word) become the
  *same* enum, generated from one table so the two cannot drift (mirror the
  single-source-of-truth rule of `predeclared.go` / `exceptions.go`). Containers
  store tagged words, not bare handles ⇒ **per-element tagging** falls out:
  heterogeneous `[1, "a"]`, nested `[[1,2],[3,4]]`, `d["a"][1]`,
  `xs.append([1,2])`, `{'k': True}` all lower, print, index and iterate. The
  compile-time element-kind maps (`listElemStr`/`listElemInt`/`setElem*`/
  `dictKey*`/`dictVal*`, ADR 0175) become a *property of the object* read from the
  tag, so the ADR 0175 refusal retires and the `@estr[h]` per-object flags
  (ADR 0174) collapse into the tag. Kills the whole `store i32 @.strN` /
  `ret i32 @.str1` / `rt_append(i32, i32 @.lstN)` / `store i32 @.setN` invalid-IR
  family at the source. DoD: `programs/nested_data.gy (planned)` + `programs/probe_heterogeneous.gy`
  byte-identical on both backends *and* equal to CPython; no
  `i32 @\.(str|lst|dict|set)` ever appears in an argument or store position
  (extend `runtime_ir_test.go` to assert it module-wide). Pairs with L7.2/L7.3 —
  a tag is what makes precise rooting and NaN-boxing well-defined.
  - ✅ **Done (ADR 0182): one tag table, and the compiled heap's kind is a projection of
    it.** Three vocabularies answered "what kind is this": `ValueTag` (`value.go`, read by
    the interpreter's heap, the compiled `%obj` values and `rt_obj_is`), the ABI tags
    (`abi.go`, the exported ABI), and the heap's `kind` word (`heapargs.go`, `rt_alloc`'s
    parameter and the collector's dispatch) — where a list was `5` in one and `1` in the
    other, written into IR as a literal `rt_alloc(i32 1)` at a dozen sites. Now
    `heapKindOrder` is the projection (`none=0, list=1, dict=2, set=3, instance=4`),
    `HeapKindFor`/`HeapTagFor` translate both ways, names come from `kindForTag` (so
    `HeapKindName` no longer spells them out a second time), codegen allocates through named
    constants, and `gustyc --lang` prints both tables with `definitions.valueTag` in
    `--schema`. Verified against stubs: renumbering `HeapKindSet` fails with "codegen
    allocated an unknown heap kind 5", and swapping the projection order fails the
    round-trip test.
  - ✅ **Done (ADR 0184): a tag word per element.** `@heap_tags : [1024 x [256 x i32]]` sits
    parallel to the element slots (a fourth struct field would have rewritten every existing GEP
    in the same commit that changes behaviour) and carries the canonical `ValueTag` numbers, so
    `int=0` is both the zero value and "integer" with no init pass. `rt_print_list_mixed` is
    `rt_print_list` with the container-wide `@estr[h]` load replaced by a per-slot tag load.
    `xs = [1, "a", None, 2]` now compiles and prints `[1, 'a', None, 2]` — identical to CPython
    and to the interpreter — where ADR 0175 refused it. `elemKindTag` is the gate: numbers,
    interned strings and `None` only; **bools** (not values yet), **floats** (the mixed printer
    has no float rendering) and **containers** (the collector cannot mark elements) stay refused,
    as does anything whose string-ness codegen cannot prove, because printing a string table
    *index* as a number is the very bug the original refusal prevented. Loop bodies
    (`for i in range(N): xs = [i, "a", None]`), string variables, last-assignment-wins rebinding
    and string-returning calls all work; a 150-iteration loop is tested to actually collect
    (`freed=148`) while printing correctly. `integration/string_containers_test.go`'s
    "must be refused" rows for literals moved to a print-correctly test in this commit.
  - ✅ **Done (ADR 0185): `for x in xs` over a mixed list.** The loop already computes the
    index, so the tag is available where the value is bound: the runtime list loop fetches
    `rt_get_elem(h, i)` *and* `rt_tag_of(h, i)` into `%_x` / `%_x_tag`, and `print(x)` dispatches
    on the tag at run time. `rt_print_mixed_value` gained a `quote` flag — container printing
    passes 1 (`repr()`, quoted, from the interned repr slot of ADR 0174), top-level printing
    passes 0 (`str()`) — which is Gap L.2's str/repr split landing where it can be implemented.
    Anything else with the variable refuses via one guard in `value()`: "print(x) works, but
    using it as a number needs a tagged value". Stub check: pinning the tag to 0 prints
    `1\n0\n0` and fails the parity rows.
  - ✅ **Done (ADR 0187): element reads and writes carry the tag.** The CPython oracle found
    the first of these as a *wrong answer in a passing build*: `xs[0] = "z"` on a mixed list
    stored the payload through `rt_put_elem` and never wrote `@heap_tags`, so `print(xs)`
    answered `[1, 'a', None]` — the interned index of `"z"`, rendered through the slot's stale
    `int` tag. Both backends agreed, so the two-backend matrix had nothing to say.
    The rule now: **the operation that writes a slot's payload writes its tag**.
    `rt_append_tagged(h, v, t)` appends both in one call (the tag array is not cleared on free,
    so a forgotten tag reads back the previous tenant's tag); item assignment emits
    `rt_put_elem` + `rt_tag_elem` inside the same bounds-checked block and skips the
    container-wide kind bookkeeping, because the tags carry the truth. Reading emits
    `rt_get_elem` + `rt_tag_of`, and two uses of the pair are open: `print(xs[i])` dispatches on
    the tag, and `v = xs[i]` binds a tagged variable (the same `%_v`/`%_v_tag` pair ADR 0185
    gives a loop variable, so `print(v)` and rebinding-to-retire-the-tag came free). Payload and
    tag are cross-checked — one saying interned-string while the other says number refuses
    rather than emitting IR. Still refused, with a message naming what works: arithmetic,
    comparison, call arguments and format specs on a tagged element, dict/set key reads, and
    elements the tag cannot describe (bool, float, nested container). New parity programs
    `mixed_element_reads.gy`, `mixed_element_writes.gy`; two older tests that asserted
    `print(xs[i])` *must* refuse were inverted, and every refusal test now asserts the refusal's
    text so a future opening is noticed.
  - ✅ **Done (ADR 0189): containers compare by value.** `xs == ys` for two equal lists answered
    False on *both* backends — the comparison compared heap handles — and the bug went the other
    way too: a stored string is an index into `@str_tab`, so `[0] == ["zero"]` was **True**, the
    interned index matching the number. `if [1] == 1:` emitted `icmp eq i32 @.lst1, 1` and llc
    refused the module; in the interpreter `!=` was not even the negation of `==`, so
    `[1] == [1]` and `[1] != [1]` both answered False.
    Now `rt_container_eq` walks the containers the way Python's `__eq__` does — lists positional,
    sets and dicts by containment (a positional walk would make `{1, 2} == {2, 1}` False) — and
    every element is the `(payload, tag)` pair, compared by `rt_slot_eq`. The tag is what makes
    that sound: without it the payload collision returns. **Stub check**: delete the builder tag
    stores and `TestTaggedElementsDistinguishPayloadCollisions` answers True where CPython answers
    False.
    Following ADR 0187's pairing rule to its end found four builders that had never written tags —
    the plain list/dict/set literal builders, the container-variable assignment builders, and
    `heapArg`, which hand-rolled its own `rt_alloc` + `rt_dict_put` loop for call arguments. An
    untagged slot is uninitialised memory: a container could compare unequal to an *identical*
    container depending on which heap slot it landed on. `heapArg` now delegates to
    `heapSetFrom`/`heapDictFrom`, so a container is built in exactly one place per kind; mutation
    uses `rt_append_tagged` / `rt_set_add_tagged` / `rt_dict_put_tagged` (whose update path also
    rewrites the value's tag). `Tripwire`: `TestEveryContainerBuilderWritesTags` covers ten build
    paths. Container-vs-proven-scalar is decided statically (with both operands still evaluated so
    side effects survive); container-vs-*unknown* refuses — "comparing a container with X needs a
    tagged value" — which is L11.2 named precisely. `is` stays identity, and `!=` is `==`'s
    negation. New parity program `programs/container_equality.gy`, which reports verdicts through
    `if` so the row tests equality and not the bool-rendering debt `probe_bool_value` pins.
    **Latent, and now written down**: dict key lookup and set dedup still compare payloads only,
    so they become unsound the moment heterogeneous keys are allowed — L11.1 (1b) must carry the
    tag into `rt_dict_get`/`rt_set_add`, not only into equality.
  - 🟢 **Remaining**, in order: (1a) ~~the other element-wise *reads*~~ — done (ADR 0187);
    the next element-wise uses need tagged values at the *use* site, which is L11.2; (1b) mixed
    *dicts* and *sets* (same storage trick, `rt_dict_print`/`rt_set_print` dispatch on one flag
    today, and a key read answers through one static kind); (1c) floats in containers, which
    needs a float branch in `rt_print_mixed_value` — the tag exists, the renderer does not;
    (1d) retiring `@estr[h]` entirely once every read path is tagged; (2) **bools as values** — measured today `--json` reports
    `"type": "int"` for `True` on both backends, so `print(True)` prints `1`, and L11.2
    cannot be fixed independently: there is no tag to print from; (3) the
    `i32 @.strN` / `ret i32 @.str1` / `rt_append(i32, i32 @.lstN)` invalid-IR family
    (Gap J.6) disappears once elements are tagged, at which point `runtime_ir_test.go`
    should assert module-wide that no handle constant appears in a value position;
    (4) `programs/nested_data.gy (planned)` + `programs/probe_heterogeneous.gy` byte-identical across
    backends and equal to CPython; (5) `@heap` elements carrying tag-and-payload together
    rather than two parallel arrays — cleaner, but a layout change touching every container
    operation, and the pairing rule above already makes the failure mode unreachable.
  - Probes recorded while planning this item (all reproducible, all still open):
    heterogeneous lists/dicts/nested literals fail to compile AOT while the interpreter and
    Python agree; `print({1, 2})` emits invalid IR (`global variable reference must have
    pointer type`); `print(set())` is `set()` in the interpreter and `0` compiled;
    `print([1.0, 1.5, -0.0])` emits invalid IR.
    - ✅ Fixed while starting L11.2 (ADR 0183): `str(None)` is `None` and `str("x")` is `x`,
      on both backends, and `s = str(None)` compiles — the three compile-time `str()` folds
      (the AST folder, the codegen's string-value folder, and the builtin lowering) now agree,
      and a folded string is never stored as a global (the IR-shape guard rejects any
      `i32 @.` / `store i32 @`). Pinned by `pkg/lang/str_fold_test.go` and
      `integration/str_fold_test.go` against CPython, and checked against a stub that folds
      `str(None)` back to `"0"`. `print(set())` (compiled prints `0`) and container quoting
      remain open here; `str(True)` remains `1` because bools are not values yet.
- **L11.2 — `str()` vs `repr()` are one function per backend (closes Gap L.2)**
  ⏳ PLANNED — **gated on L11.1's bool step**: `print(True)`/`print(1 == 1)` print `1` on
  *both* backends and `--json` reports `"type": "int"` for `True`, so the rendering table
  cannot be fixed before bools are values (measured 2026-08-04). The parts that do not need
  a tag — the empty-set rule (`print(set())` is `set()` interpreted, `0` compiled), quoting
  inside containers, and `str(None)`'s invalid-IR emission — can land with it. — `print(True)` is `1` today; bools, `True`/`False`, `None`, quoting
  and the empty-set `set()` rule are decided in two places (Go `Repr`, IR
  `@rt_print_value`). Make it one shared, context-correct pair (`str` for
  `print`/f-strings, `repr` inside containers), pinned by a table test that runs
  every value form through both backends + CPython.
- **L11.3 — Tuples are values, not syntax sugar** ⏳ PLANNED — `TupleLit` has no
  AOT lowering at all (`unsupported expression *lang.Tuple`, so
  `def pair(): return (1, 2)` cannot compile), and the interpreter prints a tuple
  as `[1, 2, 3]`. Ship the tuple type in both backends: immutable, indexable,
  unpackable, hashable as a dict key, printed `(1, 2)`/`(1,)`/`()`. Feeds the
  covariant `tuple[...]` rule of L6.6, which today only has a checker to talk to.
- **L11.4 — Python-shaped indexing: negatives, bounds, one rule** ✅ DONE (ADR 0210) —
  one normalisation (`i < 0 ⇒ i + len`) now serves read, write, `pop`, `index` and slice
  bounds: `xs[-1]`, `xs[-1] = v`, `"abc"[-1]` and the folded literal `[1, 2, 3][-1]` answer
  what CPython answers, and the literal shape no longer takes the compiler down with
  `panic: runtime error: index out of range [-1]` (it had no bounds test at all). In the
  compiled backend the normalisation is *emitted* against `rt_list_len` and the bounds check
  runs on the normalised index, so it holds for a list the compiler never saw. **Dicts and sets
  are exempt** — their subscript is a key, and `-1` is a key you can store — pinned on three
  engines so no future "simplify it into one path" can quietly merge the two operations. The
  `docs/language.md` heading that claimed "negative indices ✅ DONE" for slicing only now says
  what is true of both. `probe_negative_index` and `probe_negative_literal` graduated to
  `programs/negative_index.gy` / `programs/negative_literal_index.gy` with their ledger rows
  deleted; what remains is L11.5's string value model (a string in a *variable* is still
  AOT-refused) and the exit-code split recorded as R.17.
- **L11.5 — Code-point strings (closes Gap N.2)** ⏳ PLANNED — `len("café")` is 5,
  `"héllo"[1]` is the byte `195`, and `for c in s` at module scope emits an
  invalid `store i32 @.str1`. Decide the representation **once for both backends**
  (the gap's own rule: a half-migration is worse than the divergence): UTF-8 bytes
  + a decode/measure helper shared by `len`, `s[i]`, `s[i:j]`, `for c in s`, the
  interned table and the printers; `len` counts code points, `s[i]` returns a
  one-code-point string. Extend `TestStringLengthIsBytesForNow` into the oracle
  test it will become.
- **L11.6 — Numeric truth in the compiled backend (closes Gaps P.1 + P.2)**
  ⏳ PLANNED — floored `//` on negative ints (`-7 // 2` → `-4`), `x /= 2` yields
  `4.0`, float-through-untyped-parameter keeps its float (`f(0.1)` → `0.2`) via a
  float-parameter inference in exactly ADR 0174's shape, Python-floored `%` on
  floats (`-3.5 % 2.0` → `0.5`, wrong on *both* backends today), `floor`/`ceil`
  return `int`, and **stdlib constants keep their type** — `print(math.PI)` prints
  `3` compiled today, because on-disk data-only modules fold to `int`. Settle the
  `--bench`/golden expectations in the same commit.
- **L11.7 — Functions are values that compile** ⏳ PLANNED — `def apply(f, xs):
  … f(x)` and `lambda` through a parameter are `unsupported call "f"` in AOT, so
  `Callable[[P…],R]` (L6.6) is checker-only and `map`/`filter`/`sorted(key=)` are
  unreachable. Ship the fnptr/indirect-call lowering, then make `sorted`,
  `enumerate`, `zip`, `reversed`, `min/max(key=)` real language surface instead of
  `unsupported call "enumerate"`, and list them in `--lang`.
  - **Sorting is surface now (ADR 0191).** `xs.sort()`, `xs.reverse()` and `sorted(xs)` compile
    and match CPython, and the two probes the sweep pinned for them (`probe_sort_methods`,
    `probe_sorted`) were promoted to the parity programs `sorting.gy` / `sorting_literals.gy` —
    deleting the pin is how a paid debt gets recorded. One comparator (`rt_elem_gt`) orders slot
    pairs, mode 0 numerically and mode 1 by the **text** behind an interned index; the sort is a
    stable insertion sort on both paths, because `sorted(key=)` will be decorate–sort–undecorate
    over a stable sort.
  - **Comprehensions build their lists now (ADR 0192).** `[f(x) for x in range(5)]`,
    `[abs(x) for x in [...]]`, and a filter that calls a function all compile — the AOT path had
    made its constant folder the *meaning* of a comprehension and refused everything else with
    `comprehension element must be constant`. `probe_comprehension_call` was promoted to the parity
    row `comprehension_calls.gy`. Three shapes refuse rather than answer wrongly, and each is a
    pinned debt with an owner: `sum`/`min`/`max` over a runtime comprehension (folding the empty
    element set answers **0** — `probe_comp_runtime_reduce`, this item), a filter comparing elements
    with a string (see below), and iterating a list the escape analysis folded away
    (`probe_comp_folded_iter`, L11.2).
  - **A crash found on the way, and it is not the comprehension's:** `for n in names: if n == "a":`
    emits `icmp eq i32 %_n, @.str3` — an index into `@str_tab` compared with the *address* of a
    string global — and **`llc` rejects the module**, so the program exits 2 as a compiler bug rather
    than 1 as a refusal. Pinned as `probe_str_loop_eq`; the comprehension equivalent refuses
    instead of inheriting it (`probe_comp_str_filter`). Fix this first: it predates L11.7, it is
    L11.8's contract violation made concrete, and it is reachable from any program that iterates
    strings.
  - **What the next container method owes:** `xs.insert` / `xs.index` / `xs.remove` / `xs.extend` /
    `xs.clear` share this dispatch table and each needs ADR 0187's pairing rule (write the tag with
    the payload); `sorted(key=)`, `min/max(key=)` need the fnptr lowering; and a runtime helper
    emitted in the prelude changes any module-wide call-site count, so count in user code.
- **L11.8 — Refusal is part of the model, and so is its exit code** 🔷 PARTIAL —
  **the exit-code half is ✅ DONE (ADR 0211)**: an `llc` rejection is exit **2** on every path
  (`*lang.ToolchainRejectionError`, matched with `errors.As`), a trap is exit **3** on every run
  path including `--aot` (`JITResult.Code`), a refusal stays 1, and a toolchain that is not
  installed is not an LLVM rejection; `TestCLIExitCodeContract` drives the `--aot` leg and
  `integration/trap_exit_test.go` drives the classes end to end with a manufactured `llc`
  failure and a real-toolchain control. **Still PLANNED**: the capability diagnostics with
  stable codes for what is refused today only in prose — `list(<container>)`, `tuple(...)`, a
  compiled container of containers — measured this cycle to be clean refusals rather than `llc`
  rejections or panics (so the ADR 0166 "no tested shape may leave the compiler as a panic"
  contract holds for them), but their messages are `codegen:` text rather than schema'd codes.
- **L11.9 — The corpus is the spec: CPython is the oracle everywhere** ✅ DONE (ADR 0186) —
  the harness ran 41 cases and asserted backend-vs-backend only, which is exactly why rows like
  `print(True)`, `xs[-1]`, `len("café")`, `print(math.PI)` sat in a green build. The matrix now
  runs **three legs** (interpreter, compiled binary, CPython) and asserts a second contract on
  top of parity.
  - **One classifier** — `pkg/lang/oracle.go`'s `BuildOracleReport` returns `match` / `debt` /
    `not_applicable` plus per-leg `matches_python` flags and notes. A leg that did not run
    (refusal, trap, **Go panic**) does not match: a refusal is a debt, never a skip (ADR 0166's
    rule, expressed in the matrix). The harness and `gustyc --oracle` share the function, so a
    program cannot pass in one place and fail in the other.
  - **The ledger is the spec** — `integration/conformance_cases.go` declares every case's state.
    **Absence of a row means `match`**, so a divergence cannot enter the corpus silently; a
    `debt` row needs a reason, a roadmap ref (an owner), and a **pin per leg** — the exact stdout
    that leg produces today, or `Missing: true` with an optional error substring. `OracleCheck`
    returns drift, and drift fails the build **in both directions**: a row that got worse, and a
    row that got better (`oracle debt is paid … update the registry`). A ledger nobody can be
    forced to maintain is fiction.
  - **Comparison rules are named and echoed** — one default rule, `set-order`: a bare `{…}`
    rendering with no `k: v` entry compares as a sorted multiset, because CPython's set iteration
    order depends on hash seed and insertion history; a dict rendering keeps its order, because
    that one is observable in both languages. `PYTHONHASHSEED=0` makes an oracle run reproducible,
    and the rule list travels in every row so nothing is normalised away invisibly.
  - **16 probe programs** (`integration/programs/probe_*.gy`) reproduce the Phase 11 rows and are
    recorded + pinned rather than parity-asserted: bools as values, nested lists, mixed elements,
    tuples, negative indexing (the variable form *and* the literal form that **panics the Go
    compiler**), code-point strings, string indexing, stdlib constant types, `//` / `/=` / float
    `%`, `sorted`, `enumerate`/`zip`, calling a lambda through a parameter, passing a `def`'d
    name as a value, `print(set())`, and print atomicity. **Promotion rule:** when a probe starts
    matching CPython its pins fail with the instruction to delete the ledger row and move the
    program into `conformanceStandalone()` — that promotion is the definition of done.
  - **Nothing may crash the harness** — all three legs run behind `recover()`, so a compiler
    panic is one row whose aot error reads `compiler panic: …` instead of a dead test binary.
  - **Machine path (1.2, ADR 0193)** — `toolchain.min_python` records the pinned oracle the
    declared verdicts presuppose (CPython >= 3.12; `lang.OracleMinPython`), `TestConformanceMatrix`
    fails with the remedy when a stale oracle would otherwise present as compiler drift, and CI pins
    the runner that provides it. Found because `programs/typealias.gy` opens with `type Count = int`
    — PEP 695, unparseable by Ubuntu 22.04's Python 3.10 — so CI reported oracle drift on a compiler
    row while the compiler was right. An expectation copied from an external tool is a **versioned
    dependency of the test suite**: recording the version is worth nothing until a check reads it.
  - **Machine path** — matrix `schema_version` 1.1 adds `python_stdout`/`python_ok`/
    `python_error`, `interp_matches_python`/`aot_matches_python`, `oracle` with its
    `oracle_declared` counterpart, `oracle_reason`/`oracle_ref`/`oracle_rules`/`oracle_notes`/
    `oracle_drift`, the `rows`/`skipped` and oracle counters, and a `toolchain` block naming the
    interpreter and LLVM that produced the artifact. `--schema` gains `definitions.oracleReport`
    and `definitions.conformanceRow`. `gustyc --oracle <src>` / `--oracle-file <path>` is the
    ad-hoc form (`--json` for the leg-by-leg report), with **two new exit codes: 6** (gusty
    disagreed with Python) and **7** (the oracle could not judge the source) — neither may
    collapse into 1 or 3. `tools/oracleprobe` prints the three legs, so a ledger row is written
    from measured data rather than memory.
  - **Measured result** — 59 rows: 43 parity cases (still 43/43) + 16 probes; oracle 27 `match` /
    22 `debt` / 10 `not_applicable`. **Seven of the rows that used to pass print something other
    than what CPython prints** (`features_a`, `print_args`, `container_methods`, `none_values`,
    `string_containers`, `string_escapes`, `string_params`) and eight more are gusty-only surface
    the oracle cannot run — fifteen cases whose state was previously unrecorded, all now declared,
    owned and pinned.
  - **Found on its first run**, and in no roadmap row before this: **Gap L.5, `print` is not
    atomic** (below). `docs/language.md` § print had described the wrong behaviour as intentional
    ("keeps its place in the line, and both backends interleave it identically") and now states
    what is actually true.
  - Kept honest by tests, not prose: `pkg/lang/oracle_test.go` (classification, the set rule, and
    every drift shape including "a leg that used to fail now runs"), `integration/oracle_test.go`
    (the ledger describes only registered cases, every exception carries reason + owner + pins for
    both legs, the third leg is not a stub, and **a stubbed pin must produce drift** — the
    harness is tested against its own ability to fail), and `cmd/gustyc/oracle_test.go` plus the
    extended `TestCLIExitCodeContract` for the payload and the two new codes.
- **Gap L.5 — `print` is not atomic** ⏳ PLANNED (found by the L11.9 oracle leg, ADR 0186) —
  `print` writes each argument **as it evaluates it**, so an argument whose own evaluation prints
  interleaves into the caller's line: with `def twice(v): print("<<", v, ">>")
  return v + v`, `print("got", twice(21))` emits `got << 21 >>` / `42` where CPython emits
  `<< 21 >>` / `got 42`. Both backends do it identically — precisely why parity could never see
  it. Python evaluates every argument and only then writes the line. Fix: evaluate all arguments
  (and `sep`/`end`) into values first, then emit the line as a unit, in the interpreter's print
  builtin and in codegen's print lowering (which currently emits its `printf` calls interleaved
  with the callee calls). Pinned by `programs/probe_print_atomic.gy` and by the
  `programs/print_args` ledger row — both pins have to be rewritten when it is fixed. DoD: the
  probe is promoted into `conformanceStandalone()` and the print bullet in `docs/language.md`
  that describes the interleaving is deleted.
- **Gap L.6 — a container printed as its handle, its global, or its predecessor's strings** ✅ DONE
  (found by the L11.9 oracle leg, ADR 0188) — the most ordinary program in the corpus did not
  compile: `print([1, 2])` emitted `printf("%d\n", i32 @.lst1)` and `llc-20` refused the module
  (`global variable reference must have pointer type`); `print([])` and `print({})` the same; and
  `print(set())`, `print(list())`, `print(dict())` answered `0` — the freshly allocated handle.
  Running CPython on them found two more that *did* run: `print(["a"])` then `print({1, 2})`
  printed `{(null), (null)}`, and `print([["a"], ["b"]])` printed `[1, 2]` — the interned indices
  of the inner strings, as numbers, exit 0.
  One cause: print position asked a **storage** question (`literalNeedsHeap` — "does this literal
  hold a string?") instead of a **rendering** one, so an all-int or empty literal fell through to
  the static global struct, and constructors never reached the print path at all because the gate
  asked for a literal (and the empty set has no literal spelling, Gap K.3).
  Fixed in four moves, each with a stub check: **print asks about use** (`isContainerLiteral`, and
  `emptyContainerLiteral` turns a zero-arg `set()`/`list()`/`dict()` into the empty literal for the
  print path only — value lowering stays on `rt_alloc`, per ADR 0163); **an attribute of a
  container is cleared where the container is born** (`rt_alloc` clears `@estr[h]` on both the
  fresh and the recycled path — one store, unlike the 256-entry tag array, which ADR 0187 protects
  by writing tags in pairs with payloads); **nested containers refuse with the collector's reason**
  (`heapElemKind` rejects an element that is itself a container: an element handle has no variable
  slot to be marked from, ADR 0181); and the **module-wide invariant is now a test** —
  `TestNoContainerGlobalInAValuePosition` fails if any compiled program puts `@.lstN`/`@.dictN`/
  `@.setN`/`@.strN` in an i32 value position, which is Gap J.6's closing condition (item (3))
  landed. `probe_empty_set` is a **paid debt**: promoted out of the ledger into
  `programs/empty_set.gy`, alongside the new `programs/empty_containers.gy` — which is the
  stub-proven guard (delete the two `@estr` stores and it prints `{(null), (null)}` again).
  Still open, and now named rather than wrong: containers inside containers (needs element tags
  *and* collector marking — L11.1 with ADR 0181), and `@estr[h]` retirement (L11.1 (1d)).

**Machine path (AGENTS.md, non-negotiable).** The tag enum is exposed as
`gustyc --schema` → `valueTag` and named in `--lang` (`values: tagged int/float/bool/str/None/list/dict/set/tuple/instance/function`) so an
agent can ask what a value *is* instead of inferring it from output; each new
refusal gets a stable `Diagnostic.Code` (`lower.unsupported.<shape>`) and appears
in `--json`; the exit-code table in `docs/operations.md` stays the implementation
for both legs.

**Sequencing.** Phase 11 sits *before* the remaining L7/L8 items that assume a
representation: L7.2 (precise roots), L7.3 (tagged pointers/NaN-boxing), L8.1
(monomorphization) and L8.4 (SROA on heap objects) all read the tag — do them
after L11.1. L11.9 landed first (ADR 0186) — it is the harness that proves the rest, and from
here each item below arrives with a pinned program that has to start printing Python's answer.

---

## Definition of done per item

An item is done when it ships:

- A unit test in `pkg/lang/` exercising the behavior.
- An `integration/` whole-program compile-and-run case where applicable,
  asserting interpreter/AOT/JIT parity (byte-identical stdout).
- A `docs/adr/` entry for any non-obvious decision (or a renumbering of an
  existing ADR, e.g. `0152` renumbered from the duplicated `0143`).
- An update to `docs/language.md` and `docs/operations.md` when it changes
  user-visible syntax or CLI flags.

## Definition of done for gap-shaped work
A gap is closed when the previously interpreter-only path also lowers on AOT
(or a new feature's both-backend parity program passes), and no
`interpreter-only`/`not lowered` comment remains in `codegen.go` for it.

## Sequencing note
Gaps A–H are closed; the 2026 phases (4–10) are largely closed too. The
**current** next work is **Phase 11 (the value model)** — L11.9 (the CPython oracle
harness) is ✅ DONE (ADR 0186), so the queue is now the 22 pinned `debt` rows: L11.1's
remaining reads, L11.2 (bools as values), L11.3 (tuples), L11.4 (indexing), L11.5
(code-point strings), L11.6 (numerics), L11.7 (functions as values), then L11.8 (refusals and
exit codes). L7.2/L7.3/L8.1/L8.4 all assume L11.1. The still-open gap-shaped items (Gap J.2,
Gap K.8 part 2 — full AOT tracebacks, Gap M.2 — flipping `--file` to the compiled backend,
Gap L.5 — print atomicity, Gaps N.2, P.1, P.2) are absorbed by Phase 11 where they are
representation decisions, and stay their own work where they are not (K.8 needs L8.5's line
tables; M.2 flips only once the corpus is green through the compiled leg — which, since L11.9,
is a measured claim rather than an assumption).

## Gap J — found while closing earlier gaps (2026-09-27)

Surfaced by the L8.2 module verifier and by writing both-backend print coverage.
Each is a concrete, reproducible defect with the shape to fix it.

- **Gap J.1 — multi-argument `print` separator** — ✅ DONE (ADR 0165).
  `print` is now Python's `print(*args, sep=" ", end="\n")` in both backends:
  `print("a =", 1)` writes `a = 1` (it used to write `a =` and `1` on separate lines,
  which parity could never see because both backends did it). `sep`/`end` are honoured
  for every argument kind — the runtime container printers (`rt_print_list`,
  `rt_dict_print`, `rt_set_print`) take the newline as a flag instead of baking it into
  the format — a `%` in `sep` is literal text, `print()` writes a blank line, and an
  argument that prints during its own evaluation interleaves identically per backend.
  Non-constant `sep`/`end` in AOT is an actionable diagnostic, not bad IR. Goldens were
  regenerated with the new `go test ./integration -run TestCLIBuild -args -update` path,
  and `programs/print_args.gy` joins the conformance corpus.
- **Gap J.2 — set/dict comprehension assignment (AOT)** — 🟨 PARTIAL.
  Fixed by ADR 0165: the interpreter now renders a set as `{1, 2}` (and `set()` when
  empty) instead of `<set>`, matching `rt_set_print`, so a set prints the same on both
  backends. Still open: `sa = {x for x in [3, 1, 2]}` / `da = {k: k * 2 for k in [1, 2]}`
  at module scope do not lower (`len of a non-string variable` for the dict case) — needs
  the ADR 0163 binding rule extended to set and dict comprehensions. `{x for x in [...]
  if ...}` is also a parser gap today (it parses the `if` as a conditional expression and
  demands `else`).
- **Gap J.3 — the exit-code table was aspirational** — ✅ DONE.
  The docs promised `3 = runtime error` and `4 = usage error`; the CLI emitted neither.
  Parse, usage and front-end failures all returned `2` — the same code as an LLVM module
  rejection — and a trapped program shared `1` with a compile error, so a script could not
  tell "my program crashed" from "the compiler broke" from "I forgot a flag". Now the table
  is the implementation: **1** compile error (parse/analysis/codegen/llc/cc), **2** LLVM
  rejected the module *we* emitted (compiler bug, per ADR 0164/0166), **3** the program ran
  and trapped, **4** CLI usage error (bad flags, no source, unreadable file, empty
  `--bench-dir`, missing baseline), **5** benchmark regression.
  - the flag parser used `flag.ExitOnError`, whose own status is `2`; it is
    `ContinueOnError` now so a bad command line says `4`.
  - `reportCompileErr` labelled compile failures as usage errors (`exit: 4` in the JSON
    payload); they are `1`, and `--json` parse failures now emit
    `{"ok":false,"phase":"parse","error":"1:1: …","errors":[{line,col,msg}],"exit":1}`
    instead of printing nothing and leaving the agent to scrape stderr prose.
  - pinned by `TestCLIExitCodeContract` (drives the real binary through every row),
    `TestBuildExitCodeClassifiesVerifierRejection` and `TestExitCodesAreDistinctAndDocumented`.
- **Gap L.1 — `None` was the integer 0, and every program printed a stray `0`** — ✅ DONE (ADR 0172).
  `NoneLit` existed in the AST and `KindNone`/`TNone()` in the type table, but no value ever
  represented it: `print(None)` printed `0`, `0 == None` was true, a procedure "returned" the
  value of its last statement so `print(f())` printed that value and `f() == None` was false.
  Worse, `--eval`/`--file` echoed the last value unconditionally, so **every** program's stdout
  ended with a line the program never printed (`print(1)` → `1`, then `0`) — anything piping a
  program's output had to know to discard it.
  - None is now a singleton: interpreter heap object with `tag() == TagNone`, AOT heap object of
    kind 4 cached in `@none_h` and handed out by `rt_none()`, a permanent GC root. Equality is
    handle identity. A reserved integer was rejected: every `i32` is a legal integer, so
    `x = -2147483648` would have printed `None`.
  - AOT values are untagged, so `irGen.isNoneExpr` decides statically (literal / variable whose
    latest assignment was None / call to a function that cannot produce a value), and always
    evaluates its operands first so `print(emit())` and `f() == None` keep the callee's effects.
  - `fdReturnsValue` walks the body by reflection: a hand-enumerated walk missed a `return`
    nested in `match` and printed four `None`s in the conformance corpus.
  - CLI echo rule: echo iff the last statement is a bare expression **and** the value is not
    None; `--json` reports `{"result": null, "type": "None"}` for a void-ending program.
    `--eval "x = 1 + 2\nx"` still prints `3`.
  - bare `return` now yields None in both backends (codegen previously failed with
    `codegen: unsupported expression <nil>`).
  - Tests: `none_values.gy` in the conformance corpus (38 cases), plus
    `TestNoneSingletonSemantics`, `TestNoneSurvivesGC`, `TestVoidFunctionYieldsNone`,
    `TestCompiledNoneUsesTheRuntimeSingleton`, `TestNoneEqualityIsStaticButNotLazy`,
    `TestNoneVarIsClearedByReassignment`, `TestNoneValuesInterpreter/AOT`, `TestNoneIsNotZero`,
    `TestVoidCallSideEffectsStay`, `TestCLIEvalDoesNotEchoVoid`.
  - Still open and deliberately out of scope: bools print `1`/`0` rather than `True`/`False`,
    and strings inside containers print unquoted (`[1, None]` → `[1, None]`, `[a, b]`). Tracked
    as Gap L.2 (value rendering is a language decision, not a debug-print detail).
  - **Gap K.10 — `print(<undefined name>)` reached LLVM** — ✅ DONE.
  `print(undefined_thing)` at module level passed the checker (its arguments were not
  analysed), codegen emitted a load from the non-existent slot `%_undefined_thing`, and
  LLVM's verifier rejected the module — so the exit-code contract reported an ordinary typo
  as a *compiler bug* (exit 2). Fixed at three levels:
  - the checker analyses the arguments of `print` / `len` / `range` (including `sep=`/`end=`),
    so `error at 1:7: undefined name "undefined_thing"` is a front-end error and exit **1**;
  - **one** built-in name table (`pkg/lang/predeclared.go`) is now predeclared by the
    checker, consulted by the codegen guard and used for LSP completion — `sum`, `enumerate`,
    `zip`, `round` and friends had worked in both backends while being unknown to the
    checker, which is why `print(sum(xs))` failed to compile;
  - codegen refuses a name it has no binding for with an actionable diagnostic instead of a
    dangling load (ADR 0166). The guard immediately found three bindings that allocated a
    slot without registering it (`with … as m:` among them).
  `type` is a reserved word, so `type(x)` is not a call here; it is gone from the built-in
  table and from LSP completions, which used to suggest source that would not parse.
  Tests: `TestUndefinedNameInBuiltinCallIsAFrontEndError`,
  `TestUndefinedNameBuildIsADiagnosticNotAnInvalidModule`,
  `TestBuiltinsArePredeclaredInTheChecker`, `TestPredeclaredTableCoversTheLSPList`,
  `TestWithAsTargetIsBound`, `TestCLIBuildTypoIsACompileErrorNotACompilerBug`.
  `print(undefined_thing)` at module level passes the checker (its arguments are not
  analysed), codegen emits a reference to the non-existent slot `%_undefined_thing`, and
  LLVM's verifier rejects the module — so the user is told (correctly, by the contract)
  that this is a *compiler bug* (exit 2) when they wrote an ordinary typo. `x =
  undefined_thing` is caught properly, which localises the hole to the print argument path.
  Fix by analysing print's arguments in the checker, and (per ADR 0166) making codegen
  refuse an unbound name with an actionable diagnostic instead of emitting a dangling slot.
- **Gap J.4 — `opt` fallback was silent** — ✅ DONE.
  `OptimizeIR` ran the real `opt` pipeline and, on any failure (tool missing, IR rejected),
  returned the unoptimized module with no signal, so a machine could not tell "optimized at
  -O2" from "the optimizer was unavailable" — `--opt-level=2` quietly meant `-O0`. The stage
  now reports itself:
  - `OptimizeIRReport` returns an `Optimization` record (`tool`, `pipeline`, `level`,
    `applied`, `fallback`, `note`, `error`); `applied` is true **only** when the real LLVM
    optimizer ran, so the textual pass is never counted as a success;
  - it travels on `BuildResult.Optimization` (`"optimization"` in `--build --json`) and every
    failure result after that stage, and human output prints `optimized by opt-20 (-O2)` or
    `NOT LLVM-optimized: <note>`;
  - schema: `gustyc --schema` → `optimization` (no level requested ⇒ no report, so a
    level-0 build never claims an optimization stage).
  Found while testing it: **flags after positional args were broken** — Go's `flag` package
  stops at the first positional, so `gustyc --build out src.gy --opt-level=2` tried to open a
  file called `--opt-level=2`. `reorderFlags` now hoists flag tokens to the front while keeping
  each flag's value attached (`--eval --help` still evaluates the text `--help`).
  Tests: `TestOptimizationReportWhenOptToolIsMissing`, `…WhenOptToolIsAbsent`,
  `…AbsentWhenNotRequested`, `…WhenOptRuns`, `TestBuildCarriesOptimizationReport`,
  `TestCLIBuildReportsOptimization`.
- **Gap J.5 — string arguments to user functions (AOT)** — ✅ DONE (ADR 0174).
  `shout("hi")` emitted `call i32 @shout(i32 @.str1)` — a global pointer in an i32 parameter,
  rejected by LLVM, reported as a compiler bug for a two-line program. Strings already live in
  the interned table (Gap I.2), so the argument is interned at the call site and the callee
  receives the index; `strArgKinds` (`pkg/lang/strargs.go`) infers which parameters receive
  strings from a `str` annotation, a string default, or a call site that passes one, to a fixed
  point in sorted order like the container-parameter inference. A marked parameter joins
  `internedVars`, so print/len/`==`/membership/container-store all reuse the I.2 paths, and
  comparison is an index test rather than a character loop because interning makes equal text
  the same index.
  - Three facts travel with it: `strReturningFuncs` (so `print(echo("yo"))` prints text and
    `xs.append(make_key())` records an interned element), `stringFillingParams` (so a helper that
    fills a container its caller created — `def fill(out, v): out.append(v)` — tells the caller
    what `names[1]` is), and per-object runtime flags `@estr[h]` set by `rt_mark_estr` and read by
    the printers, because whether a container holds strings is a property of the *object*.
  - `len(s)` counts bytes in IR rather than calling `strlen`: the runtime already declares
    `strlen` returning `i64`, and LLVM keys declarations by name.
  - **Found on the way:** `heapArgKinds`' variable-kind table was keyed by bare name, so
    `def ins(s, v): s.add(v)` made every variable named `s` a set program-wide and silently
    vetoed `echo`'s string parameter. `heapASTWalker` now carries the enclosing function as a
    scope (same disease as the parameter-kind leak fixed in `5232142`).
  - Still refused, as diagnostics naming the interpreter: concatenating a runtime string, string
    methods on a parameter, arithmetic/ordering on a string (which used to *compile* and return
    `index + 1` where the interpreter raises `TypeError`), and a parameter used as both string and
    number — printing `7` through the string table gave `(null)`, worse than a diagnostic.
  - Covered by `integration/string_args_test.go` (14 programs × both backends × CPython),
    `pkg/lang/string_args_test.go` (IR invariants, mixed call sites not guessed at, deterministic
    inference), and the new conformance program `string_params.gy`.

- **Gap J.6 — dict/set literals with strings, and mixed-kind containers** — ✅ DONE (ADR 0175).
  Three shapes probing found, none of them tested: `d = {"a": 1}` was refused by the
  `dictLiteralKeys`/`dictLiteralVals` constant-int checks even though the heap lowering that can
  intern sits below them (`print({"a"})` hit the same wall in `setLiteralElems`);
  `print({"a": 1})` printed `0`, the handle, via `%d`; and `print([1, "a"])` printed
  `[(null), 'a']` — a plausible-looking wrong answer, because the printer applied the string table
  to an element that was an integer.
  - `literalNeedsHeap` chooses the lowering: all-constant-integer literals keep the compile-time
    global struct, anything with a string is built as a heap object by `heapListFrom` /
    `heapDictFrom` / `heapSetFrom`, which intern and stamp the object's `@estr` flags
    (`heapListFrom stamps too, so every builder is consistent`).
  - `len({"a": 1})` measures the heap object (`rt_dict_len`/`rt_set_len`); bare literals print
    through the runtime printers, never `%d`.
  - **One element kind per position**: the per-scope maps gained number twins
    (`listElemInt`/`setElemInt`/`dictKeyInt`/`dictValInt`, saved/restored in `beginScope`) and
    `recordElemKind` refuses a container that has held the other kind there —
    `a compiled list holds either strings or numbers, not both; the interpreter allows mixing`.
    Growth (`append`, `add`) collides; **replacement does not**: `replaceElemKind` lets
    `xs = [1]` then `xs[0] = "s"` leave a list printing `['s']`, the interpreter's answer, which is
    what the first draft of the guard wrongly refused.
  - Covered by `TestContainerLiteralsMatchPython` (10 shapes × both backends × CPython),
    `TestMixedContainersAreADiagnosticNotAMisprint` (6 shapes),
    `TestItemAssignmentReplacesElementKind`, `TestStringLiteralsBuildHeapContainers`, and
    `TestLiteralNeedsHeapAndMixedKinds` (pins the two deciding predicates).
  Remaining for true Python semantics: heterogeneous containers need per-element tagging — a
  representation change, not a printer fix — so they stay a diagnostic rather than a guess.

## Gap M — CLI shapes the tests never typed (found 2026-07-29)

- **Gap M.1 — `gustyc prog.gy` was a silent no-op** — ✅ DONE.
  The usage line advertises `gustyc [flags] [<src>]` and `docs/operations.md` called `--file`
  "alias for a positional source", but no branch in `run()` read a positional argument: the
  command printed the usage banner and exited **0** without compiling anything. Every CLI test
  passed a flag first (`--file`, `--eval`, `--version`); none passed a bare path, so the single
  most natural invocation was untested. Now a positional naming an existing file is `--file`
  (asserted byte-identical to its documented alias, `--json` envelope and exit codes included),
  anything else is evaluated as source text like `--eval`, and a `.gy` name that does not exist
  is a usage error (exit 4) saying `no such file` — reading it as a program had turned a
  mistyped path into a runtime `undefined name prog`, blaming the user's code for a shell
  mistake. Covered by `TestCLIPositionalSourcePathRuns`,
  `TestCLIPositionalSourceWithTrailingFlags`, `TestCLIPositionalSourceTextEvaluates`,
  `TestCLIMissingSourceFileIsAUsageError`.
  - **Standing rule for future cycles:** the CLI is tested by *shapes of command lines*, not
    only by flags — bare path, path + trailing flags, missing path, and stdin.

## Gap K — truthiness (found writing the L9.6 corpus, 2026-09-27)

- **Gap K.1 — truthiness was wrong or uncompilable on both backends** — ✅ DONE (ADR 0167).
  `if count:` (an integer condition) failed to compile at all — the backend fed an `i32`
  to `br i1` — and `if a and b:`, `if a < b or b > 9:`, `while total:`, `print(not x)`,
  `1 if x else 2` and `elif <int>:` all failed the same way, because a condition operand
  was assumed to be an `i32` while comparisons produce an `i1`. On the other side,
  `if 0.0:` was *true* in the interpreter (a float is a handle, and a handle is nonzero),
  and in AOT a non-empty list or dict was *false* (`if xs:` tested the handle word). Now:
  conditions normalise `i1`/`i32` through one helper (`truthOperand`, also used by `elif`,
  `and`/`or`, `not`, the ternary and membership results), containers and strings test
  their length (`rt_list_len`/`rt_dict_len`/`rt_set_len`, or the compile-time length of a
  literal), boolean operators yield the interpreter's `i32` 0/1, and the interpreter asks
  the heap object behind a handle (`Evaluator.truthy`) at every condition site — `if`,
  `elif`, `while`, `and`/`or`, `not`, ternary, comprehension filters and `match` guards.
  Covered by `integration/truthiness_test.go` (38 cases checked against Python's answer on
  both backends + module verification) and `programs/truthiness.gy` in the conformance
  corpus.
  **Found on the way and fixed in the same change:** rebinding a container variable freed
  its previous handle unconditionally, and `0` — the value of a raw int/bool/None and of a
  freshly declared container — is also a legal heap slot index, so `ys = []` recycled
  slot 0 out from under an unrelated list (observed as `xs = [i for i in range(3)]` later
  reading as empty). Every free is now guarded (`if (h != 0) rt_free(h)`), pinned by
  `TestEmptyBindingNeverFreesHandleZero`.
- **Gap K.2 — `for k in d:` over a dict yields nothing in AOT** — ✅ DONE.
  `d = {1: 2, 3: 4}` / `for k in d: print(k)` printed the keys on the interpreter and
  nothing in the native binary, with no error: the loop compared its index against the
  dict's *handle* instead of its length. Container iteration now asks the runtime
  (`rt_list_len`/`rt_dict_len`/`rt_set_len`), a dict yields its keys from the `[key,
  value]` pair layout (entry i's key at `2*i`), and a container returned by a call is
  registered from the checker's inferred type so `d = make(3)` iterates too. Fixed
  alongside it: the checker bound a `for` variable to the *iterable's* type (so
  `for k in d: s = s + k` was rejected as `int + dict[any, any]`), and the loop
  variable's slot was allocated inside the body block, which does not dominate the code
  after the loop — two loops reusing one name (`for k in d:` … `for k in m:`) failed the
  verifier with "Instruction does not dominate all uses". Covered by
  `integration/dict_iteration_test.go` (17 cases × both backends vs Python) and
  `programs/subscript_assign.gy`.
- **Gap K.4 — item assignment was silently dropped by the parser** — ✅ DONE.
  `d[1] = 2` parsed "successfully": `parseExprOrAssign` consumed `= 2` and returned an
  expression statement, so the assignment vanished — on both backends, with no
  diagnostic (`1 = 2` and `f() = 1` likewise produced *zero* statements, because the
  statement-recovery loop in `parseTopLevel` only recorded `*ParseError`s and threw away
  every other parser failure). Now: `d[k] = v` inserts/updates a dict, `xs[i] = v`
  replaces an element and raises `IndexError` out of bounds (a bounds test around the
  store in AOT, via the new `rt_put_elem` which writes without bumping the length),
  sets and strings reject it, and an unassignable target is a real parse error —
  including any non-`ParseError` the parser raises, which can no longer disappear.
  Covered by `pkg/lang/subscript_assign_test.go` (AST shape, parse errors, interpreter
  semantics, IR shape + verification, actionable diagnostics) and
  `integration/programs/subscript_assign.gy`.
- **Gap K.5 — `{}` was a set in the interpreter and a dict in AOT** — ✅ DONE.
  Python's `{}` is an empty dict; the parser's brace classifier fell through to `SetLit`
  when there were no elements, so `d = {}` then `d[k] = v` failed with `not in set` on
  the interpreter while the AOT emitted dict code for the same source. `{}` now parses to
  an empty `DictLit` (`{1, 2}` is still a set), pinned by `TestEmptyBracesIsAnEmptyDict`.
- **Gap K.6 — unhandled exceptions reported nothing in AOT** — ✅ DONE.
  `xs = [1]` / `xs[5] = 2` raised on the interpreter and exited 1, while the AOT binary
  took the raise-exit path and exited **0 printing nothing**: to a script, a trapped
  program had succeeded. Four defects were underneath it:
  - the raise-exit block was `ret i32 0`, and there was no message global beside
    `@exn_code`, so the raise path *could not* say what it was reporting. Now `@exn_msg`
    travels with the flag (`rt_die` writes a traceback header + `IndexError: index out of
    range` to fd 2 via `write`, and main returns 1). The block is emitted only for
    programs that can raise, so clean programs' IR is byte-identical.
  - the checker's `exceptions` map was **declared but never populated**, so
    `raise ValueError("boom")` was a valid interpreter program that failed AOT
    compilation with `undefined name "ValueError"`. The class list, its codes, and the
    checker's name table now come from one place (`pkg/lang/exceptions.go`), and a class
    derived from an exception is itself raisable.
  - **runtime errors were not exceptions** in the interpreter: an out-of-range read or
    write aborted instead of unwinding, so `except IndexError:` never ran (AOT caught it,
    the interpreter did not — an inversion worth remembering). Reads are now checked too:
    the AOT used to load whatever sat at the slot and print `0` for `xs[5]` / `d[missing]`;
    they raise `IndexError` / `KeyError` via `checkIndexRead` / `checkKeyRead`.
  - the interpreter printed its traceback to **stdout**, mixing diagnostics with program
    output; both backends now use stderr, and the report names the class
    (`ValueError: boom`), matching Python's last line.
  Found while fixing this: **`rt_dict_has` scanned `i < count` while walking the flat
  [key, value] array two words at a time**, so only the first ⌈count/2⌉ keys were ever
  found — `3 in {1: 2, 3: 4}` was false in compiled binaries and `d[k]` for a later key
  raised KeyError. It was invisible because nothing ever asked a compiled program about a
  dict's later keys. Covered by `integration/uncaught_exception_test.go` (reports, exit
  codes, clean stdout, catchability on both backends, statically-impossible assignments)
  plus new membership cases in `integration/dict_iteration_test.go`.
- **Gap K.8 — tracebacks had no usable source location** — 🟨 PARTIAL (ADR 0171).
  Two bugs, one of them predating the AOT report entirely:
  - **the parser never gave `raise` a span**, so every traceback — interpreter included —
    said `File "prog", line 0`. `RaiseStmt` now carries the `raise` keyword's position.
  - the compiled report printed no frame at all. Raise sites now emit a pre-rendered
    frame into a new `@exn_frame` global (`  File "prog", line 3, in boom`) and `rt_die`
    prints it under the header, so the AOT's innermost frame matches the interpreter's.
  Found on the way: **`strConst` did not escape `"`**, so a double quote in any program
  string ended the LLVM literal early and the module failed to verify with a nonsense
  array length ("got type '[7 x i8]' but expected '[31 x i8]'") — a traceback frame, which
  contains quotes, was the first thing to trip it. Quotes are now `\22` (tabs/CR too).
  Still open: the AOT report shows only the raise site's own frame, where the interpreter
  prints one frame per stack level. Doing it properly needs the call-stack line tables of
  L8.5 (or an explicit frame stack pushed at each call site).
- **Gap K.7 — `--build` could fail with no stated reason** — ✅ DONE. The error branch
  printed the diagnostics *or* the failure line, never both, so a build that died in
  codegen while the program also carried warnings exited 1 showing only warnings; and the
  failure paths in `Build` returned no result at all for codegen/`llc`/source-map errors,
  so `--json` lost the diagnostics too. Now every failure path returns the partial
  `BuildResult` (diagnostics, IR, verification) and the CLI always prints the reason:
  `warning at 1:7: …` then `gustyc: build: codegen: unsupported call "enumerate"`, exit 1
  (the stage prefix is no longer doubled either). Contract documented in
  `docs/operations.md` § Failure output contract; pinned by
  `TestCLIBuildFailureStatesItsReason` and `TestCLIBuildFailureJSONCarriesDiagnosticsOnFailure`.
  The gap was found by a build that failed this way and said nothing.
- **Gap K.3 — `list.pop`, `set()`, and the set/list/dict methods were missing** — ✅ DONE
  (ADR 0170). `xs.pop()` was `no such list method pop` in the interpreter and fell through
  to the *string* method path in AOT (`string method pop on non-constant string`), so the
  natural way to drain a container in a `while xs:` loop did not exist — the truthiness
  tests had to rebind to `[]` instead. `set()` failed as `unsupported call for eval`, and
  `s.add(1)` as `attribute access on method`, so even the empty-set constructor produced a
  value nothing could grow. Now, identically on both backends:
  - `xs.pop()` removes and returns the last element, `xs.pop(i)` removes index `i`
    (negative counts from the end); empty → `IndexError: pop from empty list`, bad index →
    `IndexError: pop index out of range` (checked in the emitted code, via a new `rt_pop`
    that shifts the tail left and shrinks the length — `rt_set_elem`-style appends could
    not express removal);
  - `set()` / `list()` / `dict()` construct empty containers (`rt_alloc` per kind), and
    `s.add` / `s.discard` / `s.clear` grow and shrink a heap set (`rt_set_add`,
    `rt_set_discard`, `rt_set_clear`); `remove` raises `KeyError` where `discard` is silent,
    like Python. `set`/`list`/`dict` were also unbound names in the *checker*;
  - printing an empty set is `set()` in AOT too (the interpreter already matched Python;
    `{}` would have been a dict).
  `list(xs)`/`set(xs)`/`dict(d)` copies are a documented AOT diagnostic naming the
  interpreter as the working backend (ADR 0166), not a miscompile. Covered by
  `pkg/lang/containers_test.go`, the new `programs/container_methods.gy` conformance case
  (37 cases), and — after the second time this cycle — `pkg/lang/runtime_ir_test.go`, which
  scans the embedded runtime IR for `//` comments, stray backticks, unbalanced `define`
  blocks, and duplicate helper definitions.

## Gap N — string literals were bytes and escapes were letters (found 2026-08-03)

- **Status**: ✅ DONE (ADR 0178), one divergence left open below.
- **Found by**: probing the surface the suite never typed — a program with a
  non-ASCII string. The parity harness compares the backends to *each other*, and
  both share the front end, so a front-end bug is invisible unless a program
  asserts **Python's** answer.
- **What was wrong**:
  - escapes were "decoded" by dropping the backslash and keeping the next byte:
    `print("a\nb")` printed `anb`, `"tab\there"` printed `tabhere`, `"\x41"`
    printed `x41` — in every program, both backends, silently;
  - the ordinary-string scanner built its value with `val += string(src[j])`, and
    Go converts a `byte` to a **rune**, so each non-ASCII byte was re-encoded:
    `"héllo"` became `hÃ©llo`, with `len` reporting 8 — neither Python's 5 nor the
    6 bytes it actually occupies.
- **Fixed**: one `appendEscape` decoder implements Python's rules (`\n \t \r \a \b
  \f \v \0 \\ \' \"`, `\xHH`, `\uHHHH`, `\UHHHHHHHH`; unknown and malformed
  escapes kept verbatim), shared by `scanString` and the parser's `unescapeStr`;
  the duplicated ordinary-string scanner is deleted so the `string(byte)` class of
  bug cannot come back; f-string literal parts accumulate bytes instead of runes.
- **Three AOT-only defects surfaced behind it** (each was invalid IR or a wrong
  answer, found by the conformance harness — not by `gustyc --file`, which runs
  the interpreter):
  - `"é" in greeting` lowered to `rt_contains(i32 @.str14, …)`, a global in an
    i32 slot, rejected by `llc` → fixed with `@rt_str_contains`, an IR substring
    scan over interned indices (no libc `strstr`: the `strlen` redeclaration trap
    of ADR 0173);
  - `def g(): return "hello"` emitted `ret i32 @.str1` → a string-returning
    function now returns its `@str_tab` index;
  - `print(d["k"])` printed the interned **index** (`6`) once the dict had not
    been printed first → `printsAsInternedStr` now knows a dict's values are
    interned.
- **Tests**: `pkg/lang/escapes_test.go` (decoder unit tests, every string form,
  UTF-8 byte preservation), `integration/escapes_test.go` (28 escape cases +
  membership + AOT print cases, each expectation cross-checked by running
  CPython on the same source), and `programs/string_escapes.gy` as the 41st
  conformance program.
- **Standing rule from this gap**: integration expectations must be *derived*
  from CPython (`pythonOutput(t, src)`), not remembered. A claim about what
  Python prints that turns out to be wrong now breaks the build.

### Gap N.2 — strings measure bytes, not code points (OPEN)

`len("café")` is 5 (bytes) where CPython says 4, `"héllo"[1]` returns the byte
`195` rather than `"é"`, and `for c in s` walks bytes. This is documented in
`docs/language.md` and pinned by `TestStringLengthIsBytesForNow`, which records
both answers. Code-point semantics are a **representation decision for both
backends at once** — `len`, `s[i]`, `s[i:j]`, `for c in s`, the interned table,
and the container printers all agree on bytes today — so a half-migration (one
backend only) is worse than the divergence. Do it as one change, or as a tagged
string representation alongside Gap L.2.

## Gap M.2 — `gustyc --file` runs the interpreter (found 2026-08-03)

`docs/language.md` and `AGENTS.md` describe `--file` as the AOT path, but
`evalSrcOrFile` runs the **evaluator** unless `--jit` is passed. Consequence:
every manual "compiled this program" probe through the CLI has been an
interpreter run, and AOT-only bugs hide behind the default path — three of them
(invalid IR in `in` on a string, `ret i32 @.str`, `print(d["k"])` printing an
index) survived behind exactly that. Needed:

- ✅ DONE (ADR 0179): the `--json` payload carries `"backend": "interpreter" |
  "aot"` on every execution result (result line, runtime-error line, captured
  output), so an agent never infers the engine from the flag list; `--aot` (alias
  of `--jit`) and `--interp` select it explicitly, and asking for both is a usage
  error (exit 4) that names the contradiction rather than silently choosing;
  `--show-backend` prints the human answer on stderr, leaving stdout the program's
  (ADR 0169).
- ⏳ OPEN: flip the `--file` default to the compiled backend — but only once the
  conformance matrix is green through it, so "it ran" never again means "the
  interpreter ran it". Until then, AOT-only bugs need `--aot` or the harness to
  show themselves, which is how three of them (ADR 0178) survived manual use.

## Gap P — `/` truncated and floats printed as ints (found 2026-08-03)

- **Status**: ✅ DONE for the interpreter and the compiled paths that were
  reachable (ADR 0180); the sub-gaps below stay open.
- **Found by**: running the conformance corpus through CPython (the Python oracle
  added while working on L9.6). Parity compares backends to *each other*; only an
  external oracle sees an operator that both backends get wrong.
- **What was wrong**:
  - `7 / 2` was `3` and `1 / 4` was `0` — `/` and `//` were the same truncating
    operator, and `docs/language.md` said so. PEP 238 says `/` is true division;
    this was C's operator wearing Python's syntax;
  - `//` truncated toward zero instead of flooring (`-7 // 2` was `-3`);
  - `print(2.0)` printed `2`, `print(1.0 + 2.0)` printed `3`, and the two
    formatting tools disagreed with each other as well: `%g` lost digits
    (`0.123456789` → `0.123457`), `%.17g` invented them (`0.1` →
    `0.10000000000000001`).
- **Fixed**: `/` yields a float in both backends; `//` floors; `x /= e` and
  `x //= e` are distinct operators again; one `pyFloatRepr` (interpreter, folding)
  and `@rt_fmt_double` (compiled runtime, a `snprintf` precision ladder validated
  by `strtod`, then the trailing `.0`) render floats the way Python does.
  `TestKnownAOTDivisionGaps` pins the three compiled gaps with both the right
  answer and today's wrong one.

### Gap P.1 — compiled division shapes still wrong (OPEN)
- `//` on negative **ints** truncates in AOT (`-7 // 2` → `-3`, Python `-4`);
- `x /= 2` keeps the integer representation (prints `4`, Python `4.0`);
- a **float through an untyped parameter** truncates: `def f(x): return x * 2`
  with `f(0.1)` prints `0`. This is the static float-typing model, not the
  operator — it needs float-parameter inference in the same shape as ADR 0174's
  string-parameter inference.

### Gap P.2 — numeric builtins that Python types differently (OPEN)
- `floor`/`ceil` return a float where Python's `math.floor` returns an `int`
  (docs say "the largest double <=", so this is a deliberate-but-questionable
  choice — settle it and update the golden expectations);
- float `%` uses truncated (`math.Mod`) rather than Python's floored modulo
  (`-3.5 % 2.0` is `-1.5`, Python `0.5`).

## Gap Q — container-kind rebinding and a fixed 1024-slot heap (found 2026-09-28, L7.2)

Both surfaced while making the compiled collector precise (ADR 0181): one as a
wrong answer that reproduced identically at HEAD, one as the ceiling the new root stack
revealed underneath. Neither is a root-set bug — they are what the collector was hiding.

### Gap Q.1 — a container variable rebound to another kind keeps the old kind (OPEN)
```
d = {1: 10}
d = [3, 4]
print(d[0])        # interpreter 3, python3 3, --aot 0   (identical before ADR 0181)
```
The assignment frees the old object and stores the new handle, but the variable's
container-kind tracking (`listVars` / `runtimeDicts` / `runtimeSets`) does not settle on
the new kind for every read path, so `d[0]` is still measured as a dict lookup and yields
the wrong slot. Reproduced at HEAD, so this is not a regression from the root work; it is
a candidate for the same "one tagged word" fix as Gap A/J.6 rather than another patch to
the kind maps.

### Gap Q.2 — the runtime heap is a fixed 1024 objects, and exhaustion is silent (OPEN)
`@heap` is `[1024 x {i32, i32, [256 x i32]}]`; when it fills, `rt_alloc` returns **-1**
and the program carries on with an invalid handle. Before ADR 0181 the collector kept
everything alive, so a heap-stress program reached the ceiling and died; the root stack
fixed the retention but not the ceiling. Two things are missing, and neither is subtle:
an out-of-memory **diagnostic** (raise `MemoryError`-style and exit non-zero like any
other runtime failure, instead of returning -1), and a heap that grows — either larger or
segmented — with the capacity reported in `--gc-stats` so a workload's headroom is
visible (the interpreter's `--gc-stats` already has `live`; the AOT has `live` and `top`).

## Gap R — async code that could not mean what it said (found while starting L7.6)

L7.6 asks the semantic check to prove an `async def`'s control flow always terminates.
Before writing the proof, the async surface was measured on all three engines, and the
result was worse than "missing check": for one program there were three different answers,
two of them plausible.

```gusty
async def f(x):
    return x * 2
v = f(2)      # a coroutine: the body has not run
print(v)
```

| path | measured |
|------|----------|
| `--interp` / `--file` | `<coro>` — the repr of a value whose body never ran |
| `--aot` / `--jit` | `4` — codegen lowers the construction as a call, so `v` holds the *result* |
| CPython | runs, prints `2`, and warns `RuntimeWarning: coroutine 'f' was never awaited` |

A double await was worse: the interpreter re-ran the body and printed `4 4`; CPython raises
`RuntimeError: cannot reuse coroutine object`. Both backends "worked". `docs/language.md`
had no async section at all.

**Closed by ADR 0195.** `pkg/lang/effects.go` computes an effect signature per function
(effects performed, return shape, whether control flow can run off the end) and runs a
flow-sensitive coroutine-liveness walk; `Analyze` refuses when a coroutine is created and
never awaited (`async.coro.never_awaited`), when one object is awaited twice on a path
(`async.coro.awaited_twice`), and when an `async def` yields
(`async.generator.unsupported` — neither backend lowers an async generator, so it is refused
rather than implemented half-way). Four rules warn instead of refusing, on the principle
that a program which still means something is not a refusal: `await` / `async for` /
`async with` inside a plain `def` (CPython calls them `SyntaxError`; our model evaluates
them), `await` on a value that provably is not a coroutine, and an `async def` path that
runs off the end while other paths promise a value (`async.missing_return`). The signatures
are published as data — `gustyc --effects`, `--json`, `definitions.effectSummary` in
`--schema` — so the facts behind every verdict are readable without re-deriving them, and
`docs/language.md` now has the async section the language never had.

Three more came out of the same measuring, and are recorded here rather than left in commit
messages where nobody would look:

- **R.3 / R.3b, closed by ADR 0196** — a parameter was a constant in the compiled backend
  (assigning to one was ignored, an accumulator loop never terminated), and a `for` loop over
  `range` wrote its counter through the user's variable. Both were refusals to run ordinary
  programs, and the `--aot` leg of a pinned program could hang forever, taking the whole
  package timeout with it.
- **R.5, closed by ADR 0197** — a `def` below the code that uses it was refused as
  `undefined name`, so mutual recursion could not be compiled at all while the interpreter
  ran it. The checker now resolves a name to any `def` of the enclosing *function* scope,
  and still refuses what runs where it is written: a module-level call, or a decorator.
- **R.4, closed by ADR 0198** — a program that defined `sync`, `exit`, `write`, `time` or `main`
  was emitted under those exact link names, so libc answered its own calls (and `def main`
  collided with the generated entry point, which made the program unbuildable). Program-owned
  symbols now carry a `gy_` prefix and nothing else keeps its name unless the program did not
  define it — which is why `extern fn` still binds the C name it declares.
- **R.6, closed by ADR 0199** — a program that defined `str`, `float`, `sqrt`, `chr` ...
  had its call read through the *built-in's meaning* by codegen: `float(1)` folded to a conversion
  (printing `1.0` for a function returning `x + 7`), and `str(1)` was emitted as the program's call
  and then printed as the built-in's string, which `llc` rejected. One predicate now decides —
  does the program own this name? — in front of every shape reading keyed on a built-in name.
- **R.8, closed by ADR 0200** — a module function and a method that shared a name shared the
  checker's function key too, so the module call was measured against the method's
  `self`-inclusive arity and the program was refused with an undefined-name error on the callee's
  own parameter — while the evaluator, the compiled module and CPython all ran it.
- **R.10, closed by ADR 0201** — calling a function with too few arguments was reported by nobody:
  the parameter arrived unbound and the program was blamed afterwards, at the callee's line, for an
  undefined name — while the interpreter already refused it at run time with the message the
  checker never gave.
- **R.7, closed by ADR 0202** — the checker repeated itself structurally (per-call-site return
  inference re-walks a callee's body), so one warning appeared two or three times and the length of
  the JSON diagnostic array was not a count of findings.
- **R.9, closed by ADR 0203** — `print` and `range` were in the lexer's keyword table, so a program
  could not define a function, a parameter, a keyword argument or a method with those names:
  `def print(x)` failed with `expected identifier` before anything else saw it.
- **R.12, closed by ADR 0205** — a program-defined built-in name was visible to codegen *above* its
  definition, so `for i in range(2)` iterated the program's value while the interpreter used the
  built-in: two programs from one file, with no diagnostic anywhere on the way.
- **R.14, closed by ADR 0207 as a declared feature** — `for i in 4:` printed `0 1 2 3` on both backends
  while CPython refused it, and nothing said so: now documented, ledger-pinned and tested, boundaries
  included.
- **R.15 (OPEN, compiler bug)**, found while measuring those boundaries — `for c in "ab":` makes the
  compiled backend emit `store i32 @.str1, i32* %_c`, which `llc` rejects: an invalid module is a
  compiler bug under the exit-code contract, not a user error.
- **R.13 (OPEN)** — found while building the program that proves R.9: a file's final bare expression statement is
  echoed by the interpreter and by nobody else (closed below, ADR 0204); and `for x in 5` iterates on
  both backends while
  being undocumented and refused by CPython.
- **R.11, closed by ADR 0206 as "not a defect"** — `def f(a, b=1, c)` looked like a missing CPython
  refusal, but measurement showed the shape binds correctly in both engines; it is now a documented
  divergence with a ledger row that excludes the oracle.

Each of these was discovered by writing a corpus program rather than by reading code, which is why
they are all reproducible as programs.

### R.1 — the compiled backend runs a coroutine at the call, not at the await (OPEN)

`programs/probe_async_eager` is accepted by the checker and disagrees with itself:

```gusty
async def work(x):
    print("effect", x)
    return x * 2
async def run():
    pending = work(1)
    print("between")
    return await pending
print(await run())
```

interpreter `between / effect 1 / 2`; compiled `effect 1 / between / 2`. The awaiting rule
catches the dishonest program, not this ordering, because fixing it needs coroutine objects
in machine code: `rt_coro_new` + a per-`async def` trampoline, so a call constructs instead
of executing. Named as **L7.6a** in the plan; the probe pins both orders so paying the debt
flips the row rather than going unnoticed.

### R.2 — see the re-scoped entry below (closed by ADR 0209)

This entry once read "`await` in a loop-exit position emits an undefined intern function", and its
analysis — "this is the await path's intern accounting, not the loop's" — was wrong on both counts. The
re-scoped entry further down has the await-free repro, the real cause, and the fix.

### R.3 — a parameter was read-only in the compiled backend (CLOSED, ADR 0196)

It began as "a compiled loop whose condition variable the body reassigns never ends" and
turned out to be the whole variable model. `codegen` registered each parameter as its
incoming argument register and resolved every reference from there, so the slot an
assignment allocated was never read:

| shape | interpreter / CPython | compiled, before |
|-------|----------------------|------------------|
| `def bump(n): n = n + 1; return n` | `1` | `0` |
| `def twice(n): n = n*2; n = n+1; return n` | `7` | `3` |
| `def acc(n): while n > 0: total += n; n = n - 1` | `10` | **never returns** |
| `def count(n): for n in range(3): print(n); return n` | `0 1 2` then `2` | `9 9 9 9` then `9` |
| method `def bumped(self, n): n = n + 1; return n` | `2` | `1` |
| nested `def inner(m): m = m * 3; return m` | `6` | `2` |
| `def show(xs): xs = [9, 9]; print(xs)` | `[9, 9]` | `[9, 9]` ✓ already |
| `def greet(s): s = "world"; return s` | `world` | `world` ✓ already |

A hang is what made it visible, but the hang was the quiet case: the ordinary ones
printed a plausible number. Only parameters rebound to a float, a string or a container
worked, because those reads consult the kind maps instead of the register.

Fixed by giving every parameter the body rebinds an entry slot — the copy an SSA-form
compiler makes for a variable assigned after its definition, at the entry so a read on a
path before any assignment still reads initialized storage — and by making the slot
authoritative (`pkg/lang/params.go` + `copyInReboundParams`). Functions, methods and
nested defs share the mechanism. `programs/param_rebind.gy` is now a parity **and**
oracle-match corpus row; `integration/params_test.go` runs the class shape by shape with
a **timeout on the compiled leg**, because a program that never returns has no output to
diff and no error to read.

### R.3b — `for … in range()` drove iteration through the user's loop variable (CLOSED, ADR 0196)

Found in the same lowering while proving R.3, and independent of it: sharing one slot
between the counter and the variable meant

```gusty
for i in range(3):
    print(i)
print(i)                 # CPython and the interpreter: 2 — compiled: 3
```

and that a body assigning to its own loop variable moved the iteration:
`for i in range(3): i = i * 100` printed `0 100 101` where Python prints `0 100 200`. The
range loop now has a private `%_ctrN` counter and binds the variable from it at the top
of the body — the point Python binds it. (The container-loop lowering already had this
shape; the range path did not.)

### R.3c — a parameter rebound to a float, returned as a bare name (OPEN)

```gusty
def addf(x):
    x = x + 1.5
    return x

print(addf(1.0))         # interpreter and CPython 2.5; compiled answers 1
```

A function's argument type is read from the *shape of its return expression*; `return x`
says nothing, so the parameters are declared `i32` and `1.0` is truncated on the way in.
The obvious fix — treat a parameter the body rebinds to a float as a float variable —
made `llc` reject the module one layer later, because the type is then decided twice from
two different pieces of evidence. That is the tagged value word (**L11.6**), not a patch
here. Pinned as `programs/probe_float_param_rebind`: the interpreter leg prints
`2.5 / 3.0`, the compiled leg fails, and paying L11.6 flips the row rather than passing
unnoticed. Related, and also fixed by (e) of ADR 0196: a float-returning function whose
body assigns a parameter used to emit a second `alloca` of the same name — an `llc`
"multiple definition of local value" rejection — and compiles now.

### R.4 — an emitted function name can collide with a C symbol (CLOSED, ADR 0198)

```gusty
def sync():
    return 7
print(sync())
```

The interpreter answered `7`; the compiled binary answered **`0`** — the emitted function was
named exactly `sync`, the linker resolved the call against libc's `sync()`, and nothing along
the way noticed. Measured as a battery (`def NAME(x): return x + 7`, called through a wrapper,
expected `8`): `sync`, `printf`, `exit`, `strlen`, `free`, `malloc`, `write`, `read`, `open`,
`time`, `rand`, `system`, `abort` each answered `0`, `1`, garbage, or killed the process, and
`main` failed to build at all because the generated entry point is `@main`.

Closed by `irSymbol`: every symbol the program's own source defines is emitted as `gy_<name>` —
module functions, methods (`@gy_Point_x`), generated lambdas, decorated bodies, imported module
functions (`@gy_lib$f`) — and every reference to them (the `call`, the decorator's function-
pointer global, the source map's `symbol`) is minted through the same helper, which is what keeps
a define and its calls from drifting apart. What is *not* prefixed is what the program does not
define: the runtime helpers (`rt_*`), the C library, the generated `@main`, and every `extern fn`,
whose link name is the C name the declaration binds.

`programs/host_symbol_names.gy` is in the ledger with `oracle: "match"` — seven host-ABI names,
a class over them, and a wrapper calling all seven print identically on the interpreter, in the
binary, and in CPython. `TestBuiltBinaryCarriesThePrefixedSymbols` asserts on the artifact rather
than its stdout: `nm -defined-only` must list `gy_sync` and must not list `sync`, because a
binary that prints the right numbers was never the property — the broken one printed plausible
numbers too.

Two adjacent defects surfaced while writing that program, and are *not* closed by prefixing:


### R.5 — a `def` below the code that uses it was refused as an undefined name (CLOSED, ADR 0197)

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

`--interp` printed `1`; `--check` and `--aot` reported `error at 4:12: undefined name
"is_odd"`. Because the compiled path refuses to emit IR for a program the front end rejected
(ADR 0177), the canonical Python shape could not be compiled at all — a false positive in the
checker is a false refusal by the compiler. Codegen had no such problem: it resolves calls by
name against the module's function table.

Fixed in the checker by `collectFuncs`: the `def`s of a statement list are collected into a
table that the name lookup consults **only while analyzing a function body**, so a deferred
call sees the whole scope while a call at module or class top level — and a decorator
expression — still require the name above them. The table holds the `*FuncDef`, so a forward
call still gets its arity and argument-type checks against the declared annotations.
`programs/forward_defs.gy` is in the ledger with `oracle: "match"`: interpreter, compiled
binary and CPython byte-identical.

### R.6 — a built-in name shadowed by a `def` was read through the built-in (CLOSED, ADR 0199)

```gusty
def float(x):
    return x + 7

print(float(1))
```

`--interp` printed `8`; the compiled binary printed **`1.0`**. And with `str`/`chr` the program did
not compile at all: the call was emitted as the program's own function and then *used* as the
built-in's string result — `printf("%s", i32 %t5)` — which `llc` rejected.

The measured split across `def NAME(x): return x + 7` was: `float`, `sqrt`, `floor`, `ceil` folded
to the float conversion; `str`, `chr` produced an invalid module; `abs`, `int`, `ord`, `round`,
`sum`, `len`, `min`, `max`, `sorted`, `any`, `all` were already fine. That pattern is the whole
story: the *dispatch* was ordered correctly (user functions are consulted before the built-in
`switch`), but an LLVM backend also needs a call's **shape** — is it a float, does it fold, is it a
string, how does `print` render it — and this codegen answered many of those from the callee's
*name*.

Closed by asking one question in one place — `builtinShadowed(name)`, "does the program define
this name?" — in front of every shape reading keyed on a built-in name: `isFloat`'s call case, the
`float`/`abs`/`min`/`max`/`sum`/`sqrt`/`floor`/`ceil` readings in `floatValue`/`floatEval`, and the
`str`/`chr` folds in `stringConst`/`stringVal` (which now take the predicate as a parameter, since
they are pure helpers). Two rules came out of getting it wrong first:

- **a fact about the program outranks a fact about the built-in** — `floatFuncs` must be consulted
  before the guard, or `def half(x): return x / 2.0` loses its float arithmetic (the first patch
  did exactly that, and `programs/floatfn.gy` failed parity within the minute);
- **with the fold gone, lift** — the program's call returns `i32`, so float arithmetic needs
  `sitofp i32 … to double`, the same lift any int expression gets; skipping it produced the
  mirror-image failure (an `i32` inside an `fadd`).

`programs/shadowed_builtins.gy` is in the ledger with `oracle: "match"` — six shadowed names,
shadowed calls in print position, in float arithmetic, and over a real list — and
`TestShadowedBuiltinBatteryCompiles` compiles and runs one program per name, because each fold lives
in a different helper: `float` passing said nothing about `chr`. Unshadowed programs keep their
folds: `print(str(42))` is still constant-folded.

### R.9 — `def print` and `def range` did not parse (CLOSED, ADR 0203)

```gusty
def print(x):
    return x + 1
```

`--check` answered `error at 1:5: expected identifier` — the lexer refused the program before the
checker, the codegen or the interpreter ever saw it. So did a parameter called `range`, a keyword
argument called `print`, and a method named `range`: words a program needs, unspellable because the
lexer's `keywords` map listed `print` and `range` as keywords.

Nothing needed them to be keywords. The parser accepted them in expression position with a special
case that produced a plain `Name`, and every downstream component — the checker's built-in tables,
`codegen.go`'s `case "print"` / `case "range"`, the interpreter's `n.Value == "range"` loop fast path,
the closure built-in set, LSP completion — dispatches on the *text*. Keyword status bought nothing
and cost the ability to name those words, which also made ADR 0199 ("a built-in name is a name")
unexpressible for its two most-used members.

Closed by narrowing the keyword table to the words that actually change grammar, with tests on both
sides: built-in names usable as function, method, parameter and keyword-argument names, the built-ins
themselves still working (`print(1, sep=",", end="!")`, `range(1, 9, 2)`, comprehensions over
`range`), the whole real-keyword set still reserved with the specific message, and `Lex` asserted to
produce `TokIdent` for every built-in name so this cannot regress silently.

### R.12 — a program-defined built-in name was visible to codegen above its definition (CLOSED, ADR 0205)

```gusty
for i in range(2):          # interpreter: the built-in → 0 and 100
    print(i * 100)          # codegen:     the program's range(2) = 6, as a count
                            #              → 0 100 200 300 400 500


def range(x):
    return x * 3

print(range(4))             # 12 in both
```

No diagnostic, no crash — two different programs from one file, found only because the ledger program
written for R.9 had to be written to avoid it. The interpreter executes in order, so at the loop the
`def` has not run and `range` is still the built-in; codegen emits every function before the module
body, so the call resolves to the program's definition wherever it is written.

Closed in the front end: `SemanticAnalyzer` keeps the positions of module-level `def`s whose names are
predeclared (from the same `predeclaredNames` table the codegen guard and LSP share), and a module-level
call to one of those names above its definition is refused at the call — naming the built-in, the
definition's line, both readings, and the two ways out. Bounded to where order is real: inside a
function body every module `def` has already run, so both engines agree and nothing is refused; a method
named `range` is a method, not a module binding. Analysis continues down the built-in path after the
error, so one mistake stays one diagnostic (ADR 0201's rule, applied again).

The refusal carries its own death clause: `TestBackendsGenuinelyDifferOnTheRefusedProgram` runs the
refused source through both engines around the gate and fails if they ever agree — a refusal outgrown
by its cause is a restriction, and this is the only way to notice. The faithful alternative (dynamic
built-in lookup in the compiled backend, so a shadow can take effect at run time the way Python's
globals do) stays open deliberately: it is a runtime-design decision for the L11.x value-model work,
not a checker patch, and until then the language does not silently pick a side.

### R.13 — a file's final bare expression statement was echoed by the interpreter only (CLOSED, ADR 0204)

```gusty
def f(x):
    return x * 2

f(5)
```

`--file` and `--interp` printed `10`; `--aot` and CPython printed nothing. Echoing the last value is
a courtesy for *snippets* — the code comment said so, in more precise words than the code implemented
— and the surviving guard asked "is the final statement a bare expression with a non-None value?"
when the question is "did this source arrive as a snippet or as a file?". A program ending in a
value-returning call is ordinary (`main()` last, a `render()` call, a benchmark), so piped stdout —
the path scripts and agents use — was being appended to.

Closed by keying the echo on provenance: `evalSrcOrFile` already knows whether it received source text
or a path, so a file echoes nothing in any interpreted mode (identical to `--aot` and to
`python prog.py`), `--eval` keeps echoing a final bare expression, and `--json` still reports `result`
for a file because that field describes the evaluation rather than the program's output. The tests
compare the two backends against each other rather than against a wish, and pin the asymmetry so
neither half gets "simplified" away later.

A process note worth keeping: the gap was written up while its neighbouring gap (R.9) was being closed,
because the ledger program built to prove R.9 ended in a call. Corollary — **a comment that describes
its intent more precisely than its code does is a defect report waiting to be filed.**

### R.14 — `for x in 5` iterated on both backends, undocumented (CLOSED, ADR 0207 — declared feature)

`for i in 4:` printed `0 1 2 3` in the interpreter and in the compiled binary, identically, while
CPython raises `TypeError: 'int' object is not iterable`. Nothing said so: `docs/language.md` listed only
the `range(...)` and list-literal forms, no test touched it, and the boundaries (a count of `0`, a
negative count, a count from an expression, the same form inside a comprehension) were unstated. An
undocumented, exercised, Python-divergent construct is the worst surface to have for the two audiences
this toolchain is built for: a programmer can't tell whether to rely on it, an agent has nothing to
generate from, and a refactor could change it with nothing failing.

Closed by declaring it, measured first:

| shape | interpreter | compiled | CPython |
|---|---|---|---|
| `for i in 4:` | `0 1 2 3` | `0 1 2 3` | TypeError |
| `for j in n` (n=3), `for k in 2 + 1:` | `0 10 20`, `0 1 2` | same | — |
| `for i in 0:` / `for i in -2:` | zero iterations | zero iterations | — |
| `[x for x in 3]` | `[0, 1, 2]` | refused: "comprehension iterable must be an inline list literal, range(), or a container variable" | — |

So: an integer on the right of `for … in` is a **repeat count**, from any integer expression, `n <= 0`
running zero times, and it lowers to the same counter loop as `range(n)` (ADR 0196). It earns its keep —
it needs no built-in, which makes it the counted loop available to a module that has claimed the name
`range` for itself (ADR 0205). `programs/for_int_count.gy` joins the ledger's gusty-only-surface section
with `oracle: not-applicable`, and the test asserts CPython still refuses it, because an excluded oracle
is a claim with a test behind it rather than an excuse. The comprehension asymmetry is documented as the
compiled-side limitation it is, with the message that already names the alternatives.

### R.15 — iterating a string literal emitted a module `llc` rejects (CLOSED, ADR 0208)

```gusty
for c in "ab":
    print(c)
```

The interpreter printed `a` and `b`; the compiled backend emitted

```
store i32 @.str1, i32* %_c
```

and `llc-20` refused it (*global variable reference must have pointer type*) — a compiler bug under the
exit-code contract, not a source error. The unrolled loop took each element through the **scalar** value
path, which returns the raw `@.strN` global, while loop variables live in `i32` slots.

Fixed by applying the rule Gap I.2 already owns: anything written into a slot goes through
`heapElemKind`, which stores the `@str_tab` index and marks the name interned so `print` renders text.
Internedness is set per **element copy** (the unroller emits a body per element) and restored after the
loop, which is what makes `for m in [1, "a", 2]` print `1 a 2` rather than half indices, half numbers.
Numeric loops are asserted to stay numeric — the new path is shared, so "fixed the strings, made every
element a heap value" had to be ruled out explicitly. Tests read the emitted module (no `store i32 @.str`,
an `@rt_str_intern2(` call, the definition present) and run LLVM's verifier, because the interpreter
printed the right thing *while* the compiler was dying: an output test alone would have passed.

`programs/for_string_chars.gy` is the three-engine parity program
(`a b c x y 1 a 2 < h > < i > ab cd`).

### R.2 — a module that interned without saying so emitted a call to an undefined helper (CLOSED, ADR 0209)

The original entry blamed the await path; both of its ingredients were red herrings. What the failing
programs shared was a codegen site that emits `call i32 @rt_str_intern2(...)` without setting
`g.heapUsed` — the flag that decides whether `heapRuntimeIR`, which *defines* that helper, travels with
the module. The deterministic repro needs no async and no loop:

```gusty
def txt():
    return "hi"

for c in txt():      # llc: use of undefined value '@rt_str_intern2'
    print(c)
```

The string-returning path returns the interned index, which is correct (ADR 0174) — it just never
announced the helper. Two such sites were found: the `strFuncs` return path and one comparison fold.

Fixed by deriving the decision from the artifact rather than from a flag any of N call sites could
forget. At assembly the emitted body is scanned, and every runtime block whose defined names
(`define … @name(` or `@name = internal global`) the module mentions gets emitted — applied to
`heapRuntimeIR`, `raiseRuntimeIR` and `floatRuntimeIR`, with the name list parsed from the blocks
themselves so a newly added helper cannot be referenced without being defined. The flags stay, because a
program that prints no float should not carry the snprintf/strtod machinery; they can now only add,
never omit.

The unit test states the invariant `llc` enforces at build time, over seven shapes that reach helpers by
different paths: every `@rt_*`/`@gc.*` call in a module has a matching `define`/`declare`. The predicate
is tested in both directions — a libc-only module must stay lean, and a name mentioned in a comment is
not a reference. The async repro now builds and runs; its compiled output is still wrong (`0` where the
interpreter and CPython print `ok`), which is R.1, and the R.2 test skips loudly if the compiled leg ever
starts agreeing, so the two gaps cannot be mistaken for each other.

### R.16 — iterating a string computed at run time is AOT-unsupported, and now refused (OPEN)

```gusty
def txt():
    return "hi"

for c in txt():
    print(c)          # interpreter and CPython: h i
```

Once R.2 was fixed this compiled cleanly and printed **nothing**: the call hands back its `@str_tab`
index and the counted-loop fall-through reads that index as a repeat count — zero iterations for `"hi"`.
A silent wrong answer is worse than a refusal, so the shape is named instead: *iterating a string
computed at run time is not supported in the AOT backend yet; the interpreter prints its characters —
iterate a string literal, or subscript a string literal with a constant (`"abc"[0]`); a string held in a
variable needs the runtime string value model*. String **literals** iterate (ADR 0208). The real
implementation belongs with L11.5 (code-point strings / runtime string values); until then the compiled
backend declines rather than pretends. (The sentence in the original entry advised `s[0]`, which turned
out to be false — a string in a variable is refused too. Found while testing L11.4, and recorded in
ADR 0210: a refusal whose workaround does not work is worse than no workaround.)

### R.17 — an uncaught exception exited 0 through `--aot` (CLOSED, ADR 0211)

```gusty
xs = [1, 2, 3]
print(xs[-4])      # linked binary: 1 · --interp: 3 · --aot used to say: 0
```

Recorded the day it was measured, and the cause was not where the symptom was: the in-process
JIT dlopen'd the program, called the generated `main`, and threw away its return value — the C
helper `jit_call` already returned it. So `--aot` printed the traceback, answered `"exit": 0` in
`--json`, and the oracle's compiled leg filed a trap as an *answer* rather than a leg that did not
complete. Fixed by making the status data (`JITResult.Code`), the classification a type
(`*lang.ToolchainRejectionError`, matched with `errors.As`, so an `llc` rejection through the run
path is exit 2 like it has always been through `--build`, while a toolchain that is merely not
installed says `could not be run` and stays 1), and the JSON `exit` field derived from the process
status instead of hard-coded.

The lesson to keep: the linked binary had been exiting 1 correctly all along. Every test that
linked and ran a binary saw the truth; only the in-process path dropped it. **A contract verified
on one execution path is unverified.**

Pinned by `pkg/lang/jit_exit_code_test.go` (status 0 clean, non-zero for an uncaught `raise` and
for a bounds trap, and still 0 when the exception is *caught* — the direction a naive fix breaks),
`pkg/lang/toolchain_error_test.go` (an llc refusal is the typed rejection with its words; a missing
tool is not), and `integration/trap_exit_test.go` (the same trap is class 3 on `--aot`/`--file`/
`--interp`; the traceback stays off stdout; the JSON `exit` cannot contradict the process; a
manufactured `llc` failure through the run path is class 2 with a real-toolchain control).

### R.18 — division by zero answered `inf` instead of raising (CLOSED, ADR 0212)

Fixed as the first instance of a rule: **a built-in trap is a typed raise, in every backend**.
Interpreter: six hand-built `&EvalError{Msg: "division by zero"}` literals went through one funnel,
`zeroDivisionErr(kind)`, with CPython's wording per operation — the class is what makes
`except ZeroDivisionError:` fire, and three of the six wordings had also drifted (`7.0 // 0` said
"float division by zero"). Codegen: `guardNonZeroInt` / `guardNonZeroFloat` test the divisor in
front of every `sdiv`/`srem`/`fdiv`/`frem` and raise through `raiseTo`, the same entry an explicit
`raise` uses, so a handler unwinds and a handled error leaves the program exiting 0.

The compiled half was the dangerous one and looked like nothing: an unguarded `srem` does not
fault on AArch64, so `print(7 % 0)` printed a *different garbage integer on every run* and exited
0; the float path printed `inf`. Now no trap puts anything on stdout — asserted by printing
nothing at all, over six shapes including a divisor computed at run time (`x % z()`), because a
guard that only works on constants guards nothing.

### R.20 — the compiled backend dispatched only the first `except` arm (CLOSED, ADR 0213)

`tryStmt` emitted `ts.Excepts[0]` and nothing else, and cleared `@exn_flag` on entry to the
handler, so a mismatch fell through to `finally` — the ordinary continuation. Five measured shapes,
all silent, all exit 0: a matching second or third arm never ran; a bare `except:` after a typed arm
never ran; a nested `try` never reached its outer arm; and an exception no arm matched was
**deleted** — no report, no failure. Fixed by emitting the arms as the chain the syntax describes —
one block pair per arm, `@exn_code` compared in order, bare/`Exception` arms as branch-through
catches — and ending the chain with a real re-raise (restore the flag, branch to the enclosing
handler or the function's raise-exit, the same target an explicit `raise` uses). A `raise` inside a
handler now escapes to the enclosing scope, because the arm bodies are emitted with the handler
stack popped.

Pinned three ways, because a shortened chain is invisible to a verifier: behaviourally on both
backends for seven arm shapes (`pkg/lang/except_dispatch_test.go`), at the IR level by counting the
`@exn_code` reads a three-arm `try` must produce, and end-to-end on three engines via
`programs/except_arm_order.gy` (promoted from the probe, ledger row deleted).

### R.21 — "a `return` inside `try:` loses its value" (NOT A SEPARATE DEFECT — refiled as R.22)

Recorded while fixing R.18, and wrong: it reproduced with `return a % b` and not with
`return a + b`, which is not a property of `try`. The function's two `return` paths had different
*types* (int and string), it was specialised as one, and the caller rendered the integer as an
interned-string index — `(null)`. That is the mixed-return-type defect below. The probe was renamed
`probe_mixed_return_value.gy` and its ledger row reworded; the measured pin never changed, which is
the value of pinning the output rather than the theory. Keep the standing lesson: characterise the
trigger before naming the cause — a wrong cause sends the next cycle to the wrong file.

#### R.21 again — the compiled half, measured and fixed (cycles 165–166, ADR 0218)

Cycle 161 closed the interpreter half and deferred the AOT one as "raises from function bodies are
unsupported", quoting probes built on `raise` statements. Measured now with plain built-in traps, the
compiled failure is one mechanism and it is simpler to state: **a handled exception is never cleared,
so the next call re-raises it.**

```gusty
try:
    crash = 1 // 0
except:
    recovered = 1

def f() -> int:
    return 5

print(f())
```

The handler runs — the interpreter and CPython both print `5`; the compiled program dies with an
uncaught `ZeroDivisionError`, exit 3. What decides whether it appears is the statement *after* the
try: `print(5)`, `print(len("ab"))` and `print(recovered)` pass, while anything that calls a
user-defined function (`print(f())`, `v = f()`, `print(1, f())`) resurrects the exception. A call
before the try is harmless. In a function body, putting `print("handled")` after the handler prints
`handled` and then dies — so the handler fired, the arm completed, and the exception came back at the
next call site. Reproduces with `1 // 0`, `[][0]`, `int("x")` and a user `raise`.

**Fixed the same cycle it was characterised** (cycle 166, ADR 0218): `@exn_flag` is now cleared on
every edge that leaves an accepting arm — the fall-through into `finally`, and the `return`, `break`
or `continue` that would otherwise escape it, with `irGen.handledArms` telling the codegen when it is
in an arm and `funcDef` zeroing that for a nested function. What is deliberately not cleared is an
arm's own `raise` and the no-arm-matched re-raise, so the exception that is genuinely travelling still
travels; `TestUncaughtTrapStillTrapsOnBothBackends` is the control that a mute-shaped "fix" could not
pass. Eleven shapes now print the same thing on the interpreter, the compiled binary and CPython
(`integration/exception_clear_test.go`), and the module shape is pinned too
(`pkg/lang/exception_clear_test.go`).

Still open in this family, and not the same statement: **Gap R.23** (`finally` does not run when the
`try` body leaves via `return`/`raise`) and **Gap R.37** (a constant `[][0]`/`int("x")` is a
compile-time refusal instead of a runtime trap the arm could catch — which is why the grids above use
`1 // 0`, the one trap the compiled path folds correctly).

### R.23 — `finally` does not run when the `try` body returns or raises (OPEN, both backends)

```gusty
def f():
    try:
        return 1
    finally:
        print("fin")      # gusty (both backends): prints nothing, then 1 · CPython: fin, then 1
```

Measured on three engines: `finally` runs on the fall-through path and after a handled exception
(both correct), but not when the body leaves by `return` or by `raise` — the transfer paths skip the
deferred body entirely, silently, in both backends. Fix: deferred actions must be attached to the
exit paths, not just the straight-line one — for the interpreter, the unwind/return has to run the
enclosing `finally` bodies before propagating (and a `return` in `finally` overriding the pending
value follows Python); for codegen, the function's return/raise-exit blocks have to walk the pending
`finally` list. The ADR 0213 chain is testable evidence that the arm wiring is right, so this is the
remaining `try` semantics gap.

### R.24 — the checker lost names bound inside compound statements (CLOSED, ADR 0217)

Recorded in cycle 161 as "does not register names assigned inside a `try` body". Measured properly in
cycle 165 it was five shapes, not one, and the analyser's child scopes were the whole cause:

| shape | `--check` before | interpreter | CPython |
|-------|------------------|-------------|---------|
| name assigned in a `try` body, read after | `undefined name` (exit 1) | `2` | `2` |
| name assigned in an `except` arm, read after | `undefined name` | NameError (matches) | NameError |
| name assigned in a `finally` clause | `undefined name` — and the clause was **never analysed at all** | `4` | `4` |
| name first assigned in a `while` body | `undefined name` | `1` | `1` |
| name assigned in both `match` arms | `undefined name` | `1` | `1` |

`if` and `for` already analysed in the enclosing scope, with comments saying why ("assignments there
flow outward (like Python)", "runtime uses shared vars"); `try`, its handlers, `finally`, `while` and
`match` had simply never been brought to that rule. All five now share the enclosing scope, `finally`
is walked (so a typo inside it is a check-time error — the sharpest proof, in
`TestFinallyBodyIsAnalysedAtAll`), and `--aot` compiled all of them as soon as the front end stopped
refusing, because codegen had always collected assignments across nested blocks.

Definiteness was kept as a separate, correct question via `definiteOnEveryPath`: a `match` capture in
one arm, or an assignment in one handler, or anything first assigned in a `while` body is *visible*
but not *certain*, and reads get the `possibly unbound` warning rather than the old error. See ADR
0217 for the trade that makes — `--check` no longer fails that program — and for why the runtime is
what enforces it now. `programs/compound_scoping.gy` is a standalone conformance case with no ledger row — the registry's
way of saying it must print what Python prints — and it prints `-1 1 41 / 123 high 3 1 1` on all
three engines.

Two gaps fell out of the work and are recorded rather than bundled: **Gap R.21 compiled half** (a
handled exception is re-raised by the next call — the reason this program's calls sit before its
module-level `try`) and **Gap R.36** (an untouched slot is loaded and printed instead of trapping).

### R.25 — built-in traps carried a message but no exception class (CLOSED, ADR 0214)

Nine interpreter sites raised `*EvalError` with an empty `ExnType`, which made them invisible to the
language: matching is on the class, so `except AttributeError:` / `except ValueError:` /
`except TypeError:` were dead code, and the traceback printed a bare sentence where an exception
should have been named. Each now raises what CPython raises, in CPython's words — measured first with
`errors.As` rather than by eyeballing tracebacks, which is how the ADR's table got its right-hand
column.

One row justifies the whole exercise: `x = 5` then `x[0]` reported **"cannot index null"**. Not
untyped — *false about the program*. The variable held an int; the message described our internal
representation because that was the easiest thing to say, and a reader who believed it would go
looking for a null that was never there. `valueTypeName` renders `'int'`, `'NoneType'`, and an
instance's class name, so the messages are about the user's program again.

Pinned by `pkg/lang/builtin_trap_classes_test.go` (class **and** exact message per shape; the handler
runs; the wrong class does not catch it) and `integration/builtin_trap_classes_test.go` (interpreter
equals CPython on all five handler lines; the shapes AOT will not lower stay honest refusals — class
1, non-zero, never a substitute answer). `programs/probe_builtin_traps_untyped.gy` keeps the family
in the matrix while R.19 and R.26 stay open.


### R.26 — operators answered a number for operands they cannot apply (CLOSED, ADR 0215)

```gusty
print("a" * "b")   # was 1099516870662, exit 0
print(1 + None)   # was 1048578, exit 0
print([1] + 1)    # was 2097157, exit 0
```

Measured as a matrix before anything was written — 23 mistyped shapes and 30 legal ones, each run
on the interpreter, the compiled backend and CPython — and it came back with three defects rather
than one, all from the same root: `evalBin` never consulted an operand's kind, so a heap handle
that reached an arithmetic path was added or multiplied as an integer.

1. Every mistyped pair answered a number (this row).
2. **Legal programs answered numbers too**: `[1] + [2]` was `2097157`, `[1] * 3` was `3145734`,
   `"ab" * 2` was `2097156`. List concatenation and sequence repeat simply did
   not exist. A refusal-only gate would have made these worse — refusing a program the reference
   runs is still a divergence — so the rule shipped with the missing operations.
3. Ordered comparison compared handles, so `"a" < "b"` was a question about allocation order.

Now: `checkBinOp` after dunder dispatch and before every arithmetic path; the reference's four
refusal message shapes; 22 shapes byte-identical to CPython, asserted by running `python3` rather
than by pasting strings; sequence repeat and concatenation computing real values (negative and zero
counts empty, either operand order); ordering by value for strings and lists including the nested
`[1] < ["a"]` raise; `==`/`in`/`is` deliberately outside the gate, with a test that they stay total.
`"a" + ""` is in the test table on purpose — the old path used "the string is empty" as the test for
"the operand is not a string".

Two things this exposed, both fixed here:

- **The interpreter's untagged values.** Heap handles started at `1 << 20`, so a loop computing
  `i * i` reached the object space: at `i = 1024`, `self.x * self.x` is `1048576`, the bench
  accumulator reached `1048580`, which *was* the class's own method object, and `s + p.norm()` read
  as int-plus-method. Correct to ~1000 iterations, wrong past that. `heapIDBase` is now `1 << 48`,
  and `isHandle` is the single predicate answering "is this value an object?" for the collector, the
  gate and every kind test — two answers to that question is how a value becomes an int to one
  subsystem and a method to another. Tags are still the real answer (L11.1); the regression test
  computes its expectation in Go, so a reintroduced collision cannot hide behind the language's own
  arithmetic.
- **The property generator could not fail.** It bound a list to a name and read that name as an
  operand of `+` (`v1 = [1, 7, 6, 3, 4]` … `v2 = 9 + v1`), while its own test promises every
  generated program runs cleanly. The promise had been untrue all along because nothing could make
  it false. The generator now records the kind of each binding and reads a name only where that kind
  is accepted.

Pinned by `pkg/lang/operator_operand_test.go` (refusals, totals, sequence values, the collision
regressions) and `integration/operator_operand_test.go` (report lines against a real `python3`, the
parity program, compiled-leg honesty), with `programs/sequence_ops.gy` and
`programs/probe_operand_types.gy` keeping both halves in the matrix. The compiled half is R.27 and
R.33.


### R.22 — a function returning a float on one path and a string on another emits invalid IR (OPEN, L11.8 violation)

```gusty
def div_or(a, b, fallback):
    if b == 0:
        return fallback     # a string
    return a / b            # a float  →  sitofp i32 @.str3 to double, llc rejects the module

print(div_or(10, 5, "none"))
```

Confirmed pre-existing. The function is specialised as float-returning and the string path is then
`sitofp`'d — and a `sitofp` of a *global string reference* rather than a pointer is not even a
value, so `llc` rejects the module: "global variable reference must have pointer type". That is
exactly what roadmap L11.8 forbids for a tested shape (an `llc` rejection where a refusal is
required) — and note the *tool* now reports it honestly as the compiler-bug class, exit **2**, per
ADR 0211, which is how it came to light. Fix: a capability diagnostic naming the mixed return
types, or real tagged values (L11.1) which subsume the problem.

### R.19 — a missing attribute answers `0` instead of raising (OPEN, compiled half only)

```gusty
class P:
    pass
p = P()
try:
    print(p.nope)         # compiled: prints `0`, exit 0, no report
except AttributeError:
    print("caught attr")  # runs in the interpreter and CPython, not in AOT
```

The interpreter half is **done** (ADR 0214): the trap now carries
`AttributeError: 'P' object has no attribute 'nope'`, the handler runs, and the output is
byte-for-byte CPython. What is left is the compiled backend, which still substitutes a default value
for the trap — the expression evaluates, the handler never fires, the program exits 0. Fix it the way
ADR 0210 emits bounds tests and ADR 0212 emits division guards: a member test plus
`raiseTo(exnCode("AttributeError"), …)` at the attribute-read sites (instance and class). Then
promote `programs/probe_builtin_traps_untyped.gy` and delete
`TestCompiledMissingAttributeIsStillAGap` in `integration/builtin_trap_classes_test.go`, which exists
only to say out loud what to remove.


### R.27 — operators never consulted operand kinds (OPEN, compiled half)

Measured as a matrix, not as one anecdote — see R.26 for the interpreter half, closed by ADR 0215.
The compiled backend answers a number for two shapes where a heap handle reaches arithmetic:

| program | `--aot` prints | CPython |
|---------|----------------|---------|
| `print(1 + None)` | `1` | `TypeError: unsupported operand type(s) for +: 'int' and 'NoneType'` |
| `print(None * 2)` | `0` | `TypeError: unsupported operand type(s) for *: 'NoneType' and 'int'` |

Every other mistyped pair is a compile-time refusal there (exit 1, `codegen: …`), which is honest.
What is missing is the runtime guard ADR 0212 and ADR 0215 ask for: test the operand kinds the
static type cannot pin down and `raiseTo(exnCode("TypeError"), …)` rather than fold. Fix with the
same `branchRaise`/`raiseTo` machinery the division guards use, and promote
`programs/probe_operand_types.gy`.

### R.28 — `%` truncated toward zero instead of flooring (CLOSED, ADR 0216)

```gusty
print(-7 % 2)   # was -1 on both backends   CPython: 1
print(7 % -2)   # was 1  on both backends   CPython: -1
```

Measured over the whole sign grid: **88 integer cases wrong in the compiled backend (44 of them
`%`) and 44 in the interpreter** — plus 14 float cases on each, because `frem`/`fmod` is the
truncated remainder too. Fixed with `floorDiv`/`floorMod`/`floorModFloat` as the single definition,
used by the interpreter, the constant folder and (in IR form, `srem` + `select`) the emitted module.
The float case needed the IEEE detail as well: an exact remainder carries the *divisor's* sign, so
`7.5 % -0.5` prints `-0.0`, not `0.0`.

Worth reading with ADR 0216: two integration tests had **pinned the truncated values as the expected
output** — with comments naming `frem` — so the bug had a certificate of correctness. The assertions
were written from the emitted IR rather than from the language. They stay, corrected, with the
history in the comment; the new tests assert the identity
`a == (a // b) * b + (a % b)` over the grid, which a consistently truncating pair satisfies per
operator but cannot satisfy as a pair.

### R.29 — `1 == 1.0` was false in the interpreter (CLOSED, ADR 0221)

```gusty
print(1 == 1.0)     # was: interpreter 0, compiled 1, CPython True
print(1.0 == 1)     # was: interpreter 1, compiled 1, CPython True
```

The interpreter compared an integer's raw word against a float object's handle, so an int was never
equal to the float with the same value, and `!=` was wrong the matching way. The compiled backend was
right. Measured as a grid before touching anything — five integers × five floats × six operators ×
both orders, 300 comparisons — the compiled leg answered all 300 correctly and the interpreter was
wrong on exactly 8, every one of them `==`/`!=` with the **integer on the left**. All the ordering
operators were fine everywhere, and so was the float-on-left direction: the test that had been written
from the working direction is why this survived.

Fixed in `Evaluator.eqVal`: a float on the right now coerces a plain-integer left instead of returning
`false`, gated by `isHandle` so nothing that is an object gets coerced (`1 == [1]`, `1.0 == "a"` stay
False, not errors — ADR 0215's single predicate). Container equality inherited the fix rather than
needing one of its own: `[1] == [1.0]` and `{"a": 1} == {"a": 1.0}` are True because element equality
*is* this predicate. `is` deliberately did not change, and a test says so. The compiled path needed no
edit, which is recorded so nobody "fixes" it to match the old interpreter.

`programs/numeric_equality.gy` is a standalone parity program — 30 comparisons printed as `1`/`0`,
byte-identical to CPython on both legs — and `TestNumericEqualityMatchesCPythonOnBothEngines` runs the
300-case grid through the interpreter, the compiler and `python3`, checking its own expectations
against CPython before letting them judge the backends. Matrix: 65/90 parity, oracle 46 match / 30
debt / 14 not-applicable over 90 cases.

### R.40 — a literal container holding a float, or `float == str`, emits a module `llc` rejects (OPEN, compiled)

Two shapes answer with a toolchain rejection (exit 2) where the answer is known and the interpreter
prints it:

```gusty
print(1 if [1] == [1.0] else 0)     # llc: expected type
print(1 if 1.0 == "a" else 0)       # llc: global variable reference must have pointer type
```

The emissions, verbatim from `--emit-llvm`:

```
@.lst2 = private global {i32, [1 x i32]} { i32 1, [1 x i32] [@env_store = internal global [4096 x i32] zeroinitializer
%t222 = sitofp i32 @.str13 to double
```

The literal-list emitter writes a `@` with no name into an int-typed element slot; the equality path
feeds a string global to `sitofp`. Both are the Gap K.10 / ADR 0166 class — the front end owes a
refusal and instead hands `llc` a module it rejects, so the user sees exit 2 and a temp-file path
instead of a diagnostic. Neither is a semantics hole: the same comparisons **through variables**
(`xs = [1]; ys = [1.0]; print(1 if xs == ys else 0)`) compile and print the right answer, so this is
the literal emitter plus a missing operand-kind gate (Gap R.38's `checkBinOp` covers arithmetic, not
this path). `TestMixedNumericListEqualityIsAPinnedCompiledDebt` pins the first — including an assertion
on the emitted IR text, and a demand that the second leg's exit code stop being 2 — and
`programs/probe_float_list_equal.gy` is the debt row. Fix: fold literal float elements to the same
representation the bound path uses, and refuse a comparison whose operand kinds cannot be compared
instead of emitting a conversion the type system forbids.

### R.30 — the compiled backend truncated `//` toward zero (CLOSED, ADR 0216)

```gusty
print(-7 // 2)   # was: interpreter -4, compiled -3   CPython: -4
```

The interpreter already floored (via `math.Floor` on a float quotient — itself a precision hazard
for large operands); the emitted IR was a bare `sdiv`. Fixed in the same commit as Gap R.28, because
the two operators are one rule: `sdiv`/`srem` followed by `icmp ne`/`icmp slt`/`xor`/`and` and a
`select` that steps the quotient down when there is a remainder and the signs disagree. The
interpreter now uses the same `floorDiv` on integers instead of going through a double, and the
constant folder uses it, so `print(-7 // 2)` cannot be folded to a third answer.

Pinned by `pkg/lang/floor_division_test.go` (behaviour *and* the presence of the corrections in the
module) and `integration/floor_division_test.go` (312 integer and 392 float cases against CPython on
both backends, plus the identity on the compiled path).

### R.31 — `%` on a string is not formatting (OPEN, feature)

```gusty
print("%s" % 2)          # CPython: 2          gusty: TypeError, unsupported operand type(s)
print("%d-%d" % (1, 2))  # CPython: 1-2        gusty: TypeError
print("hi %s" % "you")   # CPython: hi you     gusty: TypeError
```

String interpolation with the format operator is a feature, not a bug. It used to be a *lie*: the
interpreter returned the left operand (`%d-%d`) or `0`, and `print("%s" % 2)` exited 0 having printed
a number no one wrote. The operand gate (ADR 0215, Gap R.26) turned that into a catchable `TypeError`,
so the shape is now an honest absence rather than a wrong answer — better, and still not what the line
means. `programs/probe_percent_format.gy` is the ledger row: all three shapes reported from inside a
`try`, plus `7 % 2` and `-7 % 2` so the probe distinguishes "no formatting" from "no percent at all".

The compiled leg refuses to lower `str % x` at all, with a message worth fixing on the way: *"the
interpreter evaluates it"* is false for this operator — the interpreter raises too. That sentence is
written for string ordering, where the interpreter really does evaluate; a refusal that describes the
other backend inaccurately is exactly the kind of claim this loop keeps having to retract, and a
refusal message should be generated from what the other path does, not pasted from a neighbour's case.



### R.33 — the compiled backend cannot lower sequence operations (OPEN, compiled backend)

```gusty
print([1] + [2])   # llc rejects the module: "global variable reference must have pointer type"
print([1] * 3)     # same
print("ab" * 2)    # honest refusal: `operator "*" on a string is not supported in the AOT backend`
print("a" < "b")   # honest refusal
```

The interpreter now computes all four (ADR 0215); the codegen path emits `%t1 = add i32 @.lst1,
@.lst2` for list concatenation — a `TypeError` waiting in the IR, which `llc` correctly refuses, so
the CLI reports the compiler-bug class (exit 2) rather than answering. Same signature as Gap R.16's
`store i32 @.str1`. Fix: the list/str runtime helpers (length, element copy, repeat) behind the same
operand-kind dispatch the interpreter uses, so one rule decides both backends. Until then
`programs/sequence_ops.gy` stays a debt row, and `TestCompiledSequenceOpsNeverAnswerWrong` allows a
refusal or a verification failure but never a wrong value.



### R.35 — a function cannot read a module-level name (HALF FIXED, compiled half OPEN, ADR 0220)

This program did not exist in the language:

```gusty
v = 1
def g() -> int:
    return v
print(g())          # CPython: 1 — gusty: NameError (interp) / refusal (compiled)
```

Measured as a matrix before anything was touched: the interpreter raised `NameError` and the compiled
backend refused, for a module int, for a name assigned **below** the `def`, for `len(xs)` on a module
list, for a method reading a module name, and for a nested def reaching past two frames. A closure
reading an enclosing *function* worked — the scope chain existed and the module was simply not on the
end of it. So this was the most ordinary shape in a script, missing.

**Interpreter and checker: fixed (cycle 168).** A name a function reads is resolved in its frame, then
the captured closure env, then the module the function was *defined* in — recorded per `*FuncDef` by
`rememberModuleScope`, so a nested def gets its enclosing function's module and a function defined in
an imported module gets that module, not the importer's. Because a module scope can now be read
arbitrarily late, each one is anchored as a permanent GC root (`globalScopes`, scanned by
`rootHandles`) — an unrooted map here is how "the list reads back empty" happens in this collector.
The checker pre-collects the names top-level statements can bind and consults that set **only inside a
function body**, beside the deferred-def rule of ADR 0197; module top level stays order-sensitive, which
two existing tests (`TestHoistingIsNotAFreeForAll`, `TestForwardReferenceIsNotARefusal`) caught red-handed
when a first attempt pre-registered the names in the module scope itself. Twelve shapes now agree with
CPython on the interpreter (`pkg/lang/module_scope_test.go`, `integration/module_scope_test.go`).

**Compiled: still open.** Three shapes refuse with `undefined name "MAX" (no binding for it; assign it
before use)` — a sentence whose claim that the interpreter reports the same error is now measurably
false (Gap R.38 gained a second instance) — and two shapes are worse: a method reading a module name,
and a nested def reading one, compile and print `0`, because the slot is never written (the Gap R.36
signature). `programs/probe_module_scope.gy` is the debt row (interpreter pinned at `80 7 5 40 1`,
compiled leg missing), and `TestModuleScopeIsStillOutOfReachForCompiledCode` pins both the refusals and
the silent zeros, naming its own deletion. The fix is module bindings in real global slots the collector
scans, which is a cycle of its own.

### R.39 — reading a name the body also assigns below should be UnboundLocalError (OPEN, interpreter)

```gusty
v = 10
def f() -> int:
    print(v      # CPython: UnboundLocalError — gusty: prints 10
    v = 1
    return v
```

A consequence of Gap R.35's lenient fallback, recorded rather than glossed: the frame does not know
which names are locals before it runs, so the read falls through to the module instead of refusing.
The divergence is toward the forgiving answer, and the checker covers the shape with
`possibly unbound`, but the runtime should raise `UnboundLocalError` (a `NameError` subclass, so a
program catching `NameError` behaves like CPython). The plumbing is the checker's per-function local
set reaching `callFunc`, where it can seed the frame.

### R.36 — an unwritten variable slot reads as raw memory instead of raising NameError (OPEN, compiled only)

Any name whose only assignment sits on a path that did not run, read at module level:

| shape | compiled | interpreter | CPython |
|-------|----------|-------------|---------|
| `if 0: x = 1` then `print(x)` | prints `64` | NameError | NameError |
| `while 0: w = 1` then `print(w)` | prints `64` | NameError | NameError |
| `try: a = 1 // 0` / `b = 2` / `except: pass`, then `print(b)` | prints `1630496` | NameError | NameError |
| untaken `match` arm's capture, then `print(y)` | prints `518304` | NameError | NameError |
| `for i in []: f = 1` then `print(f)` | compile refusal (`codegen:`) | NameError | NameError |

The numbers are the slot's previous contents — a tagged word from whatever the allocator handed out —
so the compiled program is not merely wrong, it reads memory it was never given and prints it as a
value. The interpreter's answer (NameError, matching CPython's exit-1-with-NameError class) is the
correct one, and it is what the interpreter does in all five rows.

Checked against the pre-fix binary before calling this a regression: the `if` and `while` shapes
already printed `64` with the old checker too, because `if` always shared its enclosing scope — so
this defect is older than Gap R.24 and independent of it. What Gap R.24 did was extend the *reachable*
set to the `match`-capture and `try`-body shapes, which the old front end refused to compile. That
makes it the first thing to fix after R.24, not a reason to have kept the refusals: the alternative was
a checker that rejects working programs to hide a codegen hole, which is the trade this loop has
repeatedly refused to make.

The fix is a per-slot written-ness check in codegen for names the analyser reports as not definite on
every path (the machinery Gap R.24 added — `definiteOnEveryPath` — is exactly the input needed): emit
the same `NameError` trap the interpreter raises, with the same wording, instead of loading the slot.
Names definite on every path keep their direct load, so no cost is paid by ordinary code. Until then
`TestUnboundAfterPartialMatchReadsGarbageInCompiled` pins the divergence and says what to delete.

### R.38 — a refusal message claims something false about the other backend (OPEN, diagnostics)

The compiled backend refuses some programs by telling you what the interpreter would have done. Most
of those sentences are true; one is not. Measured pair by pair:

| refusal emitted by codegen | its claim | what the interpreter actually does |
|------------------------------|-----------|-------------------------------------|
| `operator "%s-format" on a string` — `print("%s" % 2)` | "the interpreter evaluates it" | **raises `TypeError`** (exit 3) |
| `operator "*" on a string` — `print("ab" * 2)` | "the interpreter evaluates it" | prints `abab` ✓ true |
| concatenating a runtime string | "the interpreter supports it" | prints `hello world` ✓ true |
| compiled dict literal with string keys/values | "the interpreter supports string and other keys" | true, and the compiled leg supports it too now |
| sets do not support item assignment | "the interpreter raises TypeError" | raises ✓ true |
| `undefined name` | "the interpreter reports the same error" | NameError ✓ true |

The message is one template, `operator %q on a string is not supported in the AOT backend; the
interpreter evaluates it`, parameterised by operator — so it asserts the same thing about `%`, which
no backend implements, and `*`, which one does. The fix is to stop pasting the claim and derive it:
`checkBinOp(op, l, r)` in the interpreter is the single predicate that answers "would this operand pair
raise?", so codegen can ask it and say either "the interpreter evaluates it" or "neither backend
supports this yet (Gap …)". A refusal is the last thing a stuck program prints, and a sentence about
the other path that is true for the operator two lines above and false for this one is how someone
ends up trusting an answer that was never available.

### R.37 — a constant operation that should trap is refused at compile time (OPEN, compiled only)

`[][0]` and `int("x")` anywhere in a program, even inside a `try` the handler of which would catch
it, exit 1 with `gustyc: jit: codegen: list index out of range` / `int on non-integer string`. CPython
traps at runtime and the handler runs; the interpreter agrees (`41`). Constant folding has become an
evaluation of code the program may never execute, and its failure is reported as a compilation error
rather than as the runtime trap the source asks for. Two consequences: a program that *deliberately*
provokes and catches such an error cannot be compiled at all, and a refusal appears for a line that
would never have run (`if debug: x = [][0]`). The fold must produce an IR-level trap — the same
`gy_trap` call the dynamic path emits, with the same class — whenever the operation would raise; the
only constants it may resolve are ones the program cannot avoid executing.



### R.8 — a module function and a method of one name share the checker's key (CLOSED, ADR 0200)

```gusty
def time(x):
    return x + 5


class Timer:
    def time(self, x):
        return x * 3

    def run(self, x):
        return self.time(x) + 1


print(time(1))
print(Timer().run(2))
```

`error at 7:16: undefined name "x"` — pointing at the *method's own parameter*. The interpreter
printed `6 7`, CPython printed `6 7`, and a binary built from the module codegen emitted for this
source printed `6 7` too: only the front end thought the program was broken, and the build gate
trusts the front end (ADR 0177), so it could not be compiled.

The bisect was the interesting part, because each plausible story was killed by a smaller program:
`self.time(x)` is a dynamic attribute call and is not argument-checked at all (so it was not a
method-call bug); `tick`, `zork` and `f` behaved identically (so the name was not special); and
removing either the module def or the class made it check clean. What survived: the checker had
**one** function table keyed by bare name, methods registered into it, the method analyzed later
replaced the module function, and the call `time(1)` resolved to a definition with parameters
`(self, x)` — one argument for two parameters, so the second stayed unbound and the method body's
`x` was reported as undefined. An error at the callee, never at the call, which is what made it look
like a scoping bug.

Closed by registering methods under `Class.method` in their own table (`SemanticAnalyzer.methods`),
with the enclosing class name saved and restored around the class body so a def in an outer scope
cannot inherit it. A bare-name call can now only resolve to a module function or a nested def, which
is the only definition whose arity the call site could possibly match.

`programs/method_function_name_clash.gy` was promoted to `programs/method_function_name_clash.gy`
in the parity corpus (`oracle: "match"`), and its ledger row deleted — `TestOracleProbeRowsAreRecordedAsDebt`
fails a probe whose debt has been paid, which is how a known divergence gets turned back into
regression coverage rather than left as a permanent carve-out. Because methods were never
argument-checked through this table, "the collision is gone" could also be satisfied by checking
nothing: the tests therefore assert *which parameter the diagnostic names* (`argument "x"`, never
`"label"`) to prove the call is measured against the right definition.

### R.10 — too few arguments is not reported at all (CLOSED, ADR 0201)

```gusty
def build(a, b):
    return a


print(build(1))        # used to be: ok
```

The checker enforced half the calling contract: too **many** arguments was refused, too **few** was
not. The unfilled parameter was left unbound, and the program was blamed afterwards for a mistake
nobody had made — one dropped argument arrived as three diagnostics, two of them pointing at the
callee's correct source (`undefined name "b"`), and inconsistently at that, because the walk that
produced them only ran for annotated callees. Meanwhile the interpreter already refused, at run
time, with the message the checker never offered: `missing argument b`.

Closed in `bindParams`, which already knew the answer — it fills parameters by index, so an index
with no entry and no default is a parameter that received nothing. Two messages for two mistakes:
`function "build" expects 2 arguments, got 1` for a positional shortfall, and
`function "build" is missing argument "b"` when the call named its arguments and skipped one. Both
name the callee, because "too many arguments" without a callee is only half a diagnostic in a file
with four calls. And a call that does not fit its definition is no longer walked into the callee's
body: one mistake now produces one error, at the call.

Measured against the whole corpus: nothing was newly refused — every legitimate short call in the
corpus passes fewer arguments because a default supplies the rest, which is the evidence that the
rule targets mistakes rather than style. `programs/arity_defaults.gy` was added as a three-engine
parity program for exactly that reason (trailing defaults, all defaults, keyword-only,
keyword-plus-default, zero-parameter).

### R.11 — a non-default parameter after a defaulted one (CLOSED, ADR 0206 — not a defect)

```gusty
def offset(base, step=10, bonus):
    return base + step + bonus


print(offset(1, 2, 3))          # 6   both engines
print(offset(1, bonus=5))       # 16  both engines
```

The entry read "CPython refuses this at the `def` … the definition site is the honest place to say so",
and the cycle began by implementing that — until the measurement said `ok`, `6`, `16`, on both engines.
CPython forbids the shape because its positional binding stops at the first default, which would leave
`bonus` unreachable; this language fills positionally left to right and lets a keyword call name what it
fills, so nothing is unreachable and the imported refusal would have rejected programs whose calls all
bind correctly.

Closed as a **deliberate divergence, documented and pinned**: language.md states the rule (a default
marks a parameter as omissible, not as last), operations.md tells a code generator not to sort
parameters to satisfy a Python rule this language does not have, `programs/param_default_order.gy` joins
the ledger's gusty-only-surface section with `oracle: not-applicable`, and the tests assert both engines
agree *and* that CPython still cannot run it. What remains enforced is the thing that is broken in either
language: a call that leaves a no-default parameter unfilled, refused at the call by name (ADR 0201).

Process note, and the reason this entry stays in the roadmap rather than being deleted: a gap copied
from a language comparison is a **hypothesis**, and the cheap step is the one that measures the current
behaviour before implementing the imported fix. The `def`-level refusal would have been committed with
tests, ADR and docs if the repro had been run after the patch instead of before it.

### R.7 — one source line can report the same diagnostic two or three times (CLOSED, ADR 0202)

Measured scaling, before the fix — the same `match is not exhaustive` warning, once per call site of
the function that contains it:

| call sites for `f` | times the warning was printed |
|---|---|
| 0 | 1 |
| 1 | 2 |
| 2 | 3 |

and five corpus programs (`dispatch_gc`, `dispatch_gc_stress`, `dispatch_nested`, `gc_precise`,
`match_literal`) each printed a byte-identical line twice.

The cause is deliberate and stays: per-call-site return inference (ADR 0190) walks a callee's body
with each call's argument types, and source-level facts inside that body get re-derived each time.
What was wrong is that re-derivation was allowed to *re-report*. Every diagnostic now passes through
one funnel keyed by `(level, line, column, code, message)` — a fact about a position is either true
or not, and cannot become more true by being said again — so `len(diagnostics)` means what it looks
like, `--json` output is safe to group and diff, and the CLI never prints the same line twice.

The narrowness of the key is the tested part: different messages at one position, the same message at
two positions, the same sentence at two levels, and the same message under two codes are all still
separate findings — collapsing any of those would turn deduplication into information loss. And the
dedupe lives at the analyzer rather than at the printers, so LSP, schema dumps and tests all see the
same truth. `TestWholeCorpusReportsNoDiagnosticTwice` walks every corpus program and fails on any
repeat: the measurement that found the gap is now the test that keeps it closed.

