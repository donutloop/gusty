# Pyre (gusty repo) — Roadmap

This file is the living, concrete plan for building and evolving **Pyre** (the
`gusty` repo), a Python-like language whose core is compiled ahead-of-time
through LLVM. It lives next to `AGENTS.md` and is the single source of truth
for *what exists*, *what is next*, and *what is gap-shaped*.

> Status snapshot (verified against the code, 2026): version `0.10.0`
> (`pkg/lang/compile.go`). ADRs run `0001`..`0154`.

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
- `float()`/`round()` paths that still fall back to interpreter-only
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

### Gap H — real LLVM `opt` pipeline (scalar replacement follow-on)
- **Status**: 🟠 PARTIAL — `opt.go` is a pure-Go textual dead-global eliminator,
  not an LLVM pass.
- Drive a real LLVM `opt` pipeline via the external `llc`/`opt` tools so AOT
  emits verified, optimized IR.
- **New (follow-on)**: scalar replacement / SROA — promote a heap object whose
  handle never escapes the function to registers, eliminating the `rt_alloc`
  (this is the natural partner of Gap A's instance-layout work).
- DoD: every emitted module passes `verifyModule`; a hot loop shows SROA
  eliminates the allocation.

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
  diagnostics per run (feed the LSP).
- **L4.2 Rich token spans** — each token carries `start` AND `end` (byte +
  rune offsets), plus an optional multi-line flag, so f-strings, slices, and
  `match` patterns have exact ranges for hover/diagnostics/formatting.
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

- **L5.1 Pratt / precedence-climbing parser** — replace the recursive
  expression parser with a Pratt loop keyed off a precedence table (unary,
  `**` right-assoc, multiplicative, additive, comparison, `and`/`or`, ternary).
  Keeps current semantics; makes new operators one-line additions.
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
- **L5.6 `async`/`await` + effectful syntax** — parse `async def`,
  `await expr`, `async for`, `async with` as first-class syntax (see
  Phase 7 runtime); AOT lowers them to state machines.
- **L5.7 Type aliases `type X = ...`** — parse alias declarations; the type
  checker resolves them structurally (not nominal) by default.
- **L5.8 Incremental parse** — a stable parse tree keyed by spans so the LSP
  and REPL can re-parse only edited ranges (feeds incremental JIT in Phase 9).

### Phase 6 — semantics & type-system growth (2026)

**Goal: move `semantic.go` from static checks toward a real gradual checker.**

- **L6.1 Exhaustiveness checking for `match`** — prove a `match` covers all
  subject shapes (int ranges, unions, wildcard); warn on non-exhaustive;
  this is the semantic half of Gap B.
- **L6.2 Definite-assignment analysis** — track which names are definitely
  assigned on every path through `if`/`match`/`try`; warn on possibly-unbound
  reads (mypy-style) before codegen.
- **L6.3 Union types** (`int | str`, `None | int` sugar for `Optional`) —
  infer/check unions through assignment + call boundaries; AOT widens to a
  tagged union layout.
- **L6.4 Literal types** — `Literal[1, 2]` so `match` on constants enables
  exhaustiveness + narrowing; feed L6.1.
- **L6.5 Type narrowing / refinement** — after `if isinstance(x, int):`, the
  checker narrows `x` from `any` to `int`; after `match case 1:`, narrows to
  literal `1`. Drives better AOT layout (Gap A).
- **L6.6 Variance + generics** — `list[T]` invariance, protocol structural
  subtyping; `gusty check` reports contravariant misuse. (Monomorphization is
  Phase 8.)
- **L6.7 Call-graph + reachability** — compute a module call graph so
  dead-global elimination (`opt.go`) is precise and `__doc__` folding is
  reachable-driven.

### Phase 7 — runtime: concurrency, effects, precise GC (2026)

**Goal: a deterministic async core + memory-safety hardening.**

- **L7.1 Async runtime (`async`/`await`)** — a small cooperative scheduler
  (event loop) in the interpreter and AOT; `async for`/`async with` lower to
  generator state machines; deterministic, no GIL-style races.
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
- **L8.2 `verifyModule`-driven pipeline** — every emitted module runs the real
  LLVM verifier + `opt` passes through the external `llc`/`opt` tools
  (extends Gap H).
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
- **L10.2 ABI stability** — a versioned, documented C ABI for `extern fn`
  exports (stable struct layout for unions/tagged values across releases).
- **L10.3 Shared-library export** — `gusty --build=shared` emits a
  position-independent `.so`/`.dylib` with the stable ABI (L10.2).
- **L10.4 Benchmark harness** — `integration/` parity programs become
  benchmark cases (interp vs AOT vs JIT) so regressions surface as numbers.

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
Gaps A–H are the *current* next work (they unblock modern features). The 2026
phases (4–10) are layered on top: lexer/parser modernization (4–5) is
front-end work that can start in parallel with Gap D–H; semantics (6) and
runtime (7) build on Gap A–B; codegen (8) builds on Gap H.
