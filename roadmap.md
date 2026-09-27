# Pyre (gusty repo) — Roadmap

This file is the living, concrete plan for building and evolving **Pyre** (the
`gusty` repo), a Python-like language whose core is compiled ahead-of-time
through LLVM. It lives next to `AGENTS.md` and is the single source of truth
for *what exists*, *what is next*, and *what is gap-shaped*.

> Status snapshot (verified against the code, 2026): version `0.10.0`
> (`pkg/lang/compile.go`). ADRs run `0001`..`0180`. `go test -tags=llvm20 ./...`
> is green (857 test functions over `pkg/lang` + `integration`). Conformance
> corpus: 50 programs under `integration/programs/`, 41 matrix cases, all
> `parity: true`. **The current plan is Phase 11 — the value model** (below);
> it is motivated by a CPython-oracle probe whose findings are recorded there.

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
- **L7.3 Tagged pointers / NaN-boxing** — box small ints and floats in the
  payload so `int`/`float`/`bool` avoid heap allocation; pairs with L7.2 for
  a compact, allocation-free fast path.
- **L7.4 Refcount + cycle-collect hybrid** — refcount for acyclic data (fast
  reclaim), tracing collector for cycles; deterministic pause for the AOT
  path.
- **L7.5 Algebraic effects** — a `raise`/`yield`/`await` effect system as a
  first-class control-flow model in codegen, unifying exceptions, generators,
  and async (one lowering, one runtime).
- **L7.6 Effect/async exhaustiveness** — the semantic check proves an
  `async def`'s control flow always terminates (no missing `await`/`return`).

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
done).** All ⏳ PLANNED. Order is dependency order — L11.1 is the keystone; do not
start L11.3/L11.4/L11.5 before it, or they re-decide the representation locally.

- **L11.1 — Tagged value word (both backends)** ⏳ PLANNED — one value shape on
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
  family at the source. DoD: `programs/nested_data.gy` + `programs/heterogeneous.gy`
  byte-identical on both backends *and* equal to CPython; no
  `i32 @\.(str|lst|dict|set)` ever appears in an argument or store position
  (extend `runtime_ir_test.go` to assert it module-wide). Pairs with L7.2/L7.3 —
  a tag is what makes precise rooting and NaN-boxing well-defined.
- **L11.2 — `str()` vs `repr()` are one function per backend (closes Gap L.2)**
  ⏳ PLANNED — `print(True)` is `1` today; bools, `True`/`False`, `None`, quoting
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
- **L11.4 — Python-shaped indexing: negatives, bounds, one rule** ⏳ PLANNED —
  `xs[-1]` raises `IndexError` on **both** backends where Python answers `3`,
  `"abc"[-1]` fails compilation, and a literal `[-1]` **panics the compiler**
  (`pkg/lang/codegen.go:4314`). One normalisation (`i < 0 ⇒ i + len`) shared by
  read, write, `pop`, `index`, slicing and `for`, with bounds checks emitted at
  the same place the ADR 0168 checks already live. Fix the `docs/language.md`
  heading claiming "negative indices ✅ DONE" — it is true of slicing only.
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
- **L11.8 — Refusal is part of the model, and so is its exit code** ⏳ PLANNED —
  no tested shape may leave the compiler as an `llc` rejection, a Go panic, or a
  SIGSEGV: the ADR 0166 diagnostic is the *only* exit for what does not lower.
  Add the missing capability diagnostics (`nested container literal`,
  `tuple literal`, `sorted(...)`, `enumerate(...)`), and close the contract
  asymmetry found today: an `llc` rejection is exit **2** through `--build` but
  exit **1** through `--aot`/the JIT, so the same compiler bug is reported two
  ways. Extend `TestCLIExitCodeContract` to drive the JIT leg as well as `--build`.
- **L11.9 — The corpus is the spec: CPython is the oracle everywhere** ⏳ PLANNED —
  the conformance harness (41 cases) still asserts backend-vs-backend only, which
  is exactly why rows like `print(True)`, `xs[-1]`, `len("café")`,
  `print(math.PI)` sit in a green build. Promote the `pythonOutput` helper out of
  `escapes_test.go` into the harness, require every `integration/programs/*.gy` to
  match CPython on **both** legs, record the oracle output in
  `conformance-matrix.json` (`python_stdout`, `oracle_match`), and add the Phase 11
  rows above as new programs. Until this lands, no Phase 11 item may be marked
  DONE on parity alone.

**Machine path (AGENTS.md, non-negotiable).** The tag enum is exposed as
`gustyc --schema` → `valueTag` and named in `--lang` (`values: tagged int/float/bool/str/None/list/dict/set/tuple/instance/function`) so an
agent can ask what a value *is* instead of inferring it from output; each new
refusal gets a stable `Diagnostic.Code` (`lower.unsupported.<shape>`) and appears
in `--json`; the exit-code table in `docs/operations.md` stays the implementation
for both legs.

**Sequencing.** Phase 11 sits *before* the remaining L7/L8 items that assume a
representation: L7.2 (precise roots), L7.3 (tagged pointers/NaN-boxing), L8.1
(monomorphization) and L8.4 (SROA on heap objects) all read the tag — do them
after L11.1. L11.9 should land first (it is the harness that proves the rest).

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
**current** next work is **Phase 11 (the value model)** — its L11.9 harness first,
then L11.1 (the tagged value word), because L7.2/L7.3/L8.1/L8.4 all assume it.
The still-open gap-shaped items (Gap J.2, Gap K.8 part 2 — full AOT tracebacks,
Gap M.2 — flipping `--file` to the compiled backend, Gaps N.2, P.1, P.2) are
absorbed by Phase 11 where they are representation decisions, and stay their own
work where they are not (K.8 needs L8.5's line tables; M.2 flips only once the
Phase 11 oracle gate is green through the compiled leg).

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
