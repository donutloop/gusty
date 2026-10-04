# Pyre roadmap — the detailed record

This file is the **narrative half** of the roadmap. `../roadmap.md` is the tracker: every
item, gap, status, ADR and piece of evidence lives in its tables, and the tables are
authoritative for *state*. This file holds the reasoning behind them — how each gap was
found, what the wrong answer looked like, which measurement settled it, what was rejected
— copied verbatim out of the roadmap's free text so the "why" survives the tabulation.

Rule for future cycles:

- a **status** changes only in `../roadmap.md`, never here;
- the **story** of a cycle (measurement, root cause, alternatives rejected) is appended to
  the matching section here, in the same commit that changes the status there;
- item IDs (`L4.1`, `L11.6`, `Gap R.33`, …) are stable forever — never renumber, never
  reuse. Comments in `pkg/lang` cite them.

Every item and gap carries an anchor; the tracker's `Record` column links straight to it.
The tracker also keeps each item's **own wording** in its `Free text` column — the sentence
that used to *be* the item, lifted verbatim from the pre-tabulation roadmap — so the tracker
is readable on its own; this file is the long form: programs, measurement tables, root
causes and the alternatives that were rejected.
Text below is the record's original wording, in its original order.

<a id="gap-a"></a>
## Gap-shaped work: Gap A … Gap I.3

_Rows of the tracker's “Gaps A–I” table. Verbatim record._

<a id="gap-a-2"></a>
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

<a id="gap-b"></a>
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

**Re-measured 2026-10-02** (three engines, found while closing Gap J.2, which is the same shape — a
compiled match arm reading a name it never bound). The row said "the interpreter answers `no`, the
compiled backend answers `0`"; the answer is worse, and one shape is worse than that:

```gy
class Point:
    def __init__(self, x, y):
        self.x = x
        self.y = y
p = Point(1, 2)
match p:
    case Point(a, b):        # also with `Alias = Point` in pattern position
        print("pt", a, b)
    case _:
        print("no")
```

`--interp` prints `no` — the class pattern does not match at all. `--aot` prints `pt 0 0`: the arm
*matched* and bound both names to a word that was never written. So the two backends disagree about
whether the arm applied, and the compiled answer is not merely wrong but an unbound slot read. CPython
cannot adjudicate this shape (it raises `TypeError: Point() accepts 0 positional sub-patterns` — gusty's
positional class patterns are an extension, `docs/language.md` § Pattern matching), so the ledger row
for the promoting program must be a `debt` with per-leg pins, not a `match`.

The alias form additionally rejects the module. With `Alias = Point` written *above* the `class Point`
line and the `match` inside a function body, codegen emits:

```llvm
  %t7 = and i1 %t4, %t6        ; llc: expected instruction opcode — exit 2
```

Verified identical at `d03c87d` (before ADR 0234), so it is not a regression from the comprehension
work — it is Gap B's own invalid-IR site, and it is the one to fix first: an exit 2 on a program both
other engines run is a compiler bug, per ADR 0211.

**Closed 2026-10-02 (ADR 0235).** Three fixes, and the measurement corrected in one place: the
`%t7 = and i1 %t4, %t6` rejection also happened with `Alias = Point` written *after* the class — the
documented spelling — so the earlier note's "written above the class" was an accident of the probe,
not the condition. What the three failures had in common was that a name in pattern position was
interpreted locally by each backend, and the compiled one kept a branch for "some other kind of name"
that swallowed its own error.

- **One table.** `pkg/lang/classpat.go` answers, once per program, which declared class a
  pattern-position name denotes (`Alias = Point`, chains, cycles bounded) and which attribute names the
  program can write. Both matchers read it. `case Alias(x, y):` in a function body is now the same
  lowering as `case Point(x, y):`, subclass walk and all.
- **The module is the body's scope for a bare name.** `resolveClassID` consults `e.curModule` after the
  frame — the same reach any other bare name in a body has (ADR 0227). That is the interpreter's half;
  before it, a class pattern in a function could only fail *upwards* into calling the class.
- **The instance is asked.** `@inst_set[h][slot]` is written by `rt_inst_put` with the value, read by
  `rt_inst_has`, and cleared by `rt_inst_clear` when a heap slot becomes an instance — because
  `rt_alloc` recycles slots, and a presence bit left over from the previous tenant would say an
  instance has an attribute it never had. Verified with 300 instantiations under
  `GUSTY_ENV_GC_STRESS=1`: the sum comes out right and a `ghost` attribute nobody wrote stays absent.
- **A pattern answers `i1`.** `"1"`/`"0"` became `true`/`false` and the combinators fold constants;
  `and i1 %t, 0` — which the old code could emit — is not IR, and `case x:` in an or-pattern would
  have hit it.

What this deliberately left alone is named in **Gap R.68** below: a capture that only a *skipped* arm
would have bind is still readable in compiled code. The conformance program avoids the shape rather
than pinning it, because pinning a wrong answer as parity is how `pt 0 0` survived this long.

<a id="gap-r-68"></a>

### Gap R.68 — a capture that only a skipped arm would have bind is readable in the compiled backend (found 2026-10-02 while closing Gap B)

```
class Point:
    def __init__(self, x, y):
        self.x = x
        self.y = y

def probe(v):
    match v:
        case Point(x):
            print("bound", x)
        case 5:
            print("five", x)      # this path never binds x
    return 0

probe(Point(1, 2))               # --interp: bound 1 | five … NameError (exit 3)
probe(5)                         # --aot:    bound 1 | five 0
```

The interpreter raises `NameError: name 'x' is not defined` — a name the taken path never bound has no
value, and gusty follows Python here (the same rule ADR 0228 states for a function body's locals: a
name the body binds anywhere is local to that body, so reading it before any path assigned it is
`UnboundLocalError`, not a lookup that quietly finds something else). The compiled backend prints `0`:
`bindPat` allocated `%_x` at the top of the function and the arm that would have written it was not
taken, so the read is of an alloca nothing on this path wrote.

`UnwrittenReads(prog)` is already consulted at the top of codegen (ADR 0228) and already refuses a
body that reads a local no path assigned. The gap is that a `match` arm's captures are not edges in
that graph: the analysis sees the name bound *somewhere* in the body and stops asking. The fix is
therefore not a new analysis but the existing one extended to arm-scoped bindings, and the message
must name the arm — "x is bound only by `case Point(x)`" — because a refusal that does not say which
arm is the kind of message Gap R.38 is about.

Deliberately not fixed with this commit: it is a different question from "does this instance have the
attribute" (which the class pattern now asks), and one commit per feature is the rule. The conformance
program `programs/match_classpat.gy` avoids the shape; `programs/match_binding_edges.gy (planned)`
reports the trap through a `try`/`except` so the row tests the trap and not the print.

<a id="gap-c"></a>
### Gap C — arbitrary (fnptr-valued) decorators
- **Status**: ✅ DONE — the canonical wrapping decorator
  (`def dec(g): def wrap(x): return g(x)+1; return wrap`) compiles and runs in
  AOT via compile-time specialization (`@f_orig` + `@f_impl` + funcBind).
- Add fnptr operands and indirect `call` lowering to codegen (`closure.go`),
  then support `@dec` where `dec(f)` returns a transformed function value.
- DoD: `@dec @dec2 def f` with wrapping decorators runs identically on both
  backends.

<a id="gap-d"></a>
### Gap D — AOT `with` / `yield from` runtime (ADR 0140)
- **Status**: ✅ DONE (Round 17) — `with`/`yield from` pass AOT/interpreter parity.
- Round 17 root cause: generator accumulator lists (`genHandle`) were not GC-rooted, so the GC at body-statement boundaries collected/reused them; `yield from` then read a stale handle (double-appends, wrong sums). Fix: root each generator's `genHandle` and the `yield from` sub-list in rooted alloca slots; list-literal `yield from` now unrolls instead of treating a global as a heap handle. Regression tests: `TestParityYieldFromAcrossGC`, `TestParityYieldFromLiteral`.
  a runtime yield-from loop, but execution is blocked by a pre-existing
  duplicate-function defect (ADR 0140).
- Fix the duplicate-function lowering so the emitted protocol/loop actually
  runs; add parity programs for `with expr as name` + `yield from`.
- DoD: `with` and `yield from` integration programs run on AOT, byte-identical
  to the interpreter.

<a id="gap-e"></a>
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

<a id="gap-f"></a>
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

<a id="gap-g"></a>
### Gap G — AOT `in`/`not in` on inline literal containers
- **Status**: ✅ DONE (ADR 0156) — literal list/set/dict membership is unrolled
  to `l == elem` comparisons against the constant integer elements/keys
  (dict tests keys), empty literals fold, `not in` inverts.
- DoD: `TestParityLiteralMembership` — `x in [1,2,3]`, `x not in [1,2,3]`, `x in {1,2,3}`, dict-key `in`, and empty-container `in`/`not in` all match the interpreter on AOT.

<a id="gap-h"></a>
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

<a id="gap-i-1"></a>
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

<a id="gap-i-3"></a>
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
<a id="gap-i-2"></a>
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

<a id="phase-4"></a>
## Phases 4–11 — the 2026 language-design plan

_Rows of the tracker's phase tables. Verbatim record._

<a id="phase-4-2"></a>
### Phase 4 — lexer modernization (2026)

**Goal: a resilient, position-rich lexer that supports a modern source surface.**

<a id="l4-1"></a>
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
<a id="l4-2"></a>
- **L4.2 Rich token spans** — each token carries `start` AND `end` (byte +
  rune offsets), plus an optional multi-line flag, so f-strings, slices, and
  `match` patterns have exact ranges for hover/diagnostics/formatting. ✅ DONE —
  verified against the code (probed, not assumed): `Token` carries `Start`/`End`
  byte offsets and `StartRune`/`EndRune` rune offsets plus `Multiline`, and a
  triple-quoted string token reports `multiline=true` with exact rune ranges.
<a id="l4-3"></a>
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
<a id="l4-4"></a>
- **L4.4 Numeric-literal modernization** — `0xFF` hex, `0b101` binary,
  `0o17` octal, and `1_000`/`0x_FF` digit-group separators; keep exact
  integer semantics, reject `_` misuse. ✅ DONE (ADR 0153)
<a id="l4-5"></a>
- **L4.5 Raw strings + triple-quoted strings** — `r"..."`/`R'...'` raw strings (no escape processing; a `\` before a quote keeps the string open) and `"""..."""`/`'''...'''` triple-quoted strings (may span lines); raw-triple `r"""..."""`; docstring extraction reuses both forms; formatter preserves raw/triple forms. — ✅ DONE
<a id="l4-6"></a>
- **L4.6 Line continuation** — trailing `\` at end of line joins the next
  physical line into one logical line (Python-compatible), so long call
  argument lists don't force parens. — ✅ DONE (lexer skips the continued
  line's leading indentation and blank/comment-only continuation lines;
  unit + integration tests)
<a id="l4-7"></a>
- **L4.7 Token-stream cursor API** — a small cursor/`peek(n)`/`mark()`
  abstraction shared by parser, formatter, and LSP so all three walk the same
  stream (single source of truth for spans). ✅ DONE — `Cursor` in
  `pkg/lang/token.go` with bounds-safe `peek(n)`, `next()`, `mark()`/`reset()`,
  `position()`, and `atEOF`/`atNewline`/`atDedent`/`atIndent`/`skipNewlines`;
  the parser now walks the shared cursor (its `toks`/`pos` are gone), so the
  token stream and spans are a single source of truth; unit tests in
  `pkg/lang/cursor_test.go` (lookahead, mark/reset backtracking, EOF safety,
  predicates).

<a id="phase-5"></a>
### Phase 5 — parser modernization (2026)

**Goal: a fast, error-tolerant Pratt parser with a modern syntax surface.**

<a id="l5-1"></a>
- **L5.1 Pratt / precedence-climbing parser** ✅ DONE — `parseExprPrec` in
  `pkg/lang/parser.go` is a precedence-climbing loop over a `prec` table (unary,
  `**` right-assoc, multiplicative, additive, comparison, `and`/`or`, ternary);
  new operators are one table entry + one `binaryOp` case. Covered by
  `pkg/lang/parser_pratt_test.go` (associativity + precedence rendering).
<a id="l5-2"></a>
- **L5.2 Panic-mode error recovery** — on a parse error, skip to the next
  statement/block boundary and keep parsing, producing a forest of
  `ParseError`s (not just the first). Feeds the LSP + `gusty check`. ✅ DONE
  — `parseProgram` collects a forest of `*ParseError`s via `recoverStmt()`
  (nest-aware INDENT/DEDENT skipping), returns a partial AST plus an
  aggregate `*ParseErrors`; the LSP reports each error as a separate
  diagnostic and still indexes the partial program; `gusty check`/`verify`
  print the whole forest.
<a id="l5-3"></a>
- **L5.3 Trailing commas** — allow `f(a, b,)`, `[1, 2,]`, `{1: 2,}` and
  `match` case arg lists, for clean diffs and formatter round-trips.
  ✅ DONE — call args, list/dict/set literals, tuples (incl. `(a,)` 1-tuple),
  and match class-pattern arg lists all tolerate a trailing comma; the
  canonical formatter normalizes them away and its output re-parses cleanly.
  Covered by `pkg/lang/trailing_comma_test.go`.
<a id="l5-4"></a>
- **L5.4 ✅ DONE — Walrus operator (`:=`)** — assignment expressions usable inside `if`
  conditions and comprehensions (`if (n := len(x)) > 0:`); scope rules per
  Python 3.8+.
<a id="l5-5"></a>
- **L5.5 ✅ DONE — Union-type syntax `int | str`** — parse `|` in annotation position
  (and in `match` patterns) as a union type, not a bitwise-or; feed the
  gradual type checker.
<a id="l5-6"></a>
- **L5.6 `async`/`await` + effectful syntax** ✅ DONE (this round) — parse `async def`,
  `await expr`, `async for`, `async with` as first-class syntax. `async`/`await` are lexed keywords; `async` sets the `Async` flag on
  `FuncDef`/`ForStmt`/`WithStmt`; `await e` reduces to `e` under the minimal synchronous-coroutine model (no suspension
  primitives yet), so both the interpreter and the AOT/JIT backend run async programs identically to their sync
  counterparts (conformance parity `async_basic.gy`). The cooperative event-loop runtime (coroutines, async protocols,
  a first-class `AwaitExpr`) is Phase 7 (L7.1).
<a id="l5-7"></a>
- **L5.7 Type aliases `type X = ...`** ✅ DONE (this round) — parse alias declarations into a `TypeAliasStmt`; the type checker resolves references structurally (not nominal) by default via parse-time substitution of a structural copy. Aliases are compile-time no-ops in the interpreter/codegen/formatter; conformance case `typealias.gy`, ADR 0159, and unit tests.
<a id="l5-8"></a>
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

<a id="phase-6"></a>
### Phase 6 — semantics & type-system growth (2026)

**Goal: move `semantic.go` from static checks toward a real gradual checker.**

<a id="l6-1"></a>
- **L6.1 Exhaustiveness checking for `match`** ✅ DONE — prove a `match` covers all
  subject shapes (int ranges, unions, wildcard); warn on non-exhaustive;
  this is the semantic half of Gap B.
<a id="l6-2"></a>
- **L6.2 Definite-assignment analysis** ✅ DONE — warn which names are definitely
  assigned on every path through `if`/`match`/`try`; warn on possibly-unbound
  reads (mypy-style) before codegen.
<a id="l6-3"></a>
- **L6.3 Union types** (`int | str`, `None | int` sugar for `Optional`) —
  infer/check unions through assignment + call boundaries; AOT widens to a
  tagged union layout. ✅ DONE (semantic inference: ternary widening +
  union-aware arithmetic, ADR 0158). ✅ DONE (runtime: union-annotated variables — `int | str`, `int | float` — are accepted and evaluated by the interpreter; `checkAnnot` accepts any union member; conformance + IR tests added). ✅ DONE (AOT tagged-union lowering: union-annotated scalar variables (`int | float`, `int | str`) get a tagged `%unionbox` slot; assignment stores the runtime member tag (0=int, 1=float, 2=str); print dispatches on the tag to emit `%d`/`%f`/`%s`, so an int member prints as `%d` and a float member as `%f` even after cross-member reassignment under branches/loops).
- L6.4 Literal types — `Literal[1, 2]` so `match` on constants enables exhaustiveness + narrowing; feed L6.1. ✅ DONE (this round)
<a id="l6-5"></a>
- **L6.5 Type narrowing / refinement** — after `if isinstance(x, int):`, the
  checker narrows `x` from `any` to `int`; after `match case 1:`, narrows to
  literal `1`. Drives better AOT layout (Gap A). ✅ DONE — static isinstance-if
  narrowing (then/else branches, `not` flip, union complement via `dropType`)
  is implemented in the semantic checker (`narrowFromCond`/`analyzeNarrowed`)
  and unit-tested (`TestNarrowIsInstanceThen/Else/Not`). Match-literal narrowing
  to `Literal[1]` was already present (L6.4).
<a id="l6-6"></a>
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
<a id="l6-7"></a>
- **L6.7 Call-graph + reachability** ✅ DONE — compute a module call graph so
  dead-global elimination (`opt.go`) is precise and `__doc__` folding is
  reachable-driven.

<a id="phase-7"></a>
### Phase 7 — runtime: concurrency, effects, precise GC (2026)

**Goal: a deterministic async core + memory-safety hardening.**

<a id="l7-1"></a>
- **L7.1 Async runtime (`async`/`await`)** ✅ DONE — cooperative async in the
  interpreter: `async def` returns a *coroutine object* (deferred thunk; the
  body does not run at call time), `await` runs it to completion (deterministic,
  race-free), and `async for` awaits each coroutine element. AOT keeps eager
  semantics; parity holds because awaited async calls run once to completion in
  both backends (codegen `await` evaluates its operand). `async with` remains
  eager (`__enter__`/`__exit__`); mid-body suspension and `__aenter__`/`__aexit__`
  protocols are future work.
<a id="l7-2"></a>
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
<a id="l7-3"></a>
- **L7.3 Tagged pointers / NaN-boxing** — box small ints and floats in the
  payload so `int`/`float`/`bool` avoid heap allocation; pairs with L7.2 for
  a compact, allocation-free fast path.
<a id="l7-4"></a>
- **L7.4 Refcount + cycle-collect hybrid** — refcount for acyclic data (fast
  reclaim), tracing collector for cycles; deterministic pause for the AOT
  path.
<a id="l7-5"></a>
- **L7.5 Algebraic effects** — a `raise`/`yield`/`await` effect system as a
  first-class control-flow model in codegen, unifying exceptions, generators,
  and async (one lowering, one runtime).
<a id="l7-6"></a>
- **L7.6 Effect/async exhaustiveness** ✅ DONE — the await/return discipline is a
  semantic check, not a runtime promise: `async.coro.never_awaited`,
  `async.coro.awaited_twice` and `async.generator.unsupported` refuse programs whose
  async meaning is broken, `async.await.outside_coroutine`,
  `async.async_stmt.outside_coroutine`, `async.await.not_coroutine` and
  `async.missing_return` warn where the program still means something. The proof
  (effect signatures + a flow-sensitive coroutine-liveness walk) is shared by both
  backends and machine-readable: `gustyc --effects` (ADR 0195). See Gap R.
<a id="l7-6a"></a>
- **L7.6a Deferred coroutines in codegen** ⏳ PLANNED — the compiled backend still
  lowers a coroutine construction as a call, so a coroutine created early performs its
  effects early. Pinned as `programs/probe_async_eager`: the interpreter prints
  `between / effect 1 / 2`, the compiled binary `effect 1 / between / 2` (ADR 0195).

<a id="phase-8"></a>
### Phase 8 — codegen: monomorphization, verification, autovectorization (2026)

**Goal: make AOT a first-class optimized backend.**

<a id="l8-1"></a>
- **L8.1 Generic monomorphization** — instantiate `list[T]`/`dict[K,V]` per
  concrete type at compile time (no runtime generics), enabling scalar
  replacement (Gap H) and boxing elimination (L7.3).
<a id="l8-2"></a>
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
<a id="l8-3"></a>
- **L8.3 Autovectorization** — annotate loop/array IR so LLVM vectorizes hot
  numeric loops; add a `--report=vector` output showing which loops vectorize.
<a id="l8-4"></a>
- **L8.4 SROA/scalar-replacement** — promote non-escaping heap objects to
  registers (the Gap H follow-on), now driven by monomorphization (L8.1).
<a id="l8-5"></a>
- **L8.5 Debug line tables in IR** — ✅ DONE (ADR 0231). Codegen records which statement
  each stretch of emitted code was written for; a post-pass (`pkg/lang/debug.go`) lays LLVM
  debug metadata over the finished module — `DICompileUnit` (`DW_AT_language` =
  `DW_LANG_Python`), one `DISubprogram` per *program* function and none for the compiler's own
  GC/exception/printer blocks, and a `DILocation` per instruction — so `llc` writes a real
  `.debug_line` table and `cc -g` is not the lie it used to be (DWARF comes from the module,
  not the link). The report is read back from the artifact: `gustyc --debug-info <src>`
  (`definitions.debugInfo`, per-function coverage, IR-line-to-source-line rows, a `defect` field
  when module and emitter disagree) and `--build --debug`'s `dwarf` member
  (`definitions.dwarfReport`), produced by `llvm-dwarfdump --debug_line` over the linked object
  — `skipped` for a missing toolchain, never `ok`. Honoured by `--build`, `--emit-llvm` and
  `--jit/--aot`; source map v2 carries the same table. Found by reading the artifact: a
  `DISubprogram` whose `type:` named a bare type list made `llc` print `invalid subroutine
  type`, exit 0, and emit an empty table while every internal count looked perfect. Required
  closing a Gap-K.6-class hole first: attribute and tuple assignment carried no position, so
  their instructions inherited the previous statement's line. Unblocks Gap K.8.

<a id="phase-9"></a>
### Phase 9 — tooling: incremental JIT, package manager, richer LSP (2026)

**Goal: a modern developer loop.**

<a id="l9-1"></a>
- **L9.1 Incremental JIT REPL** — recompile only edited ranges (L5.8) through
  the LLVM JIT; hot loop retains registers across edits; `--repl` mode.
<a id="l9-2"></a>
- **L9.2 Package manager (`gusty install`/`publish`)** — a module registry for
  on-disk stdlib + user packages; `extern fn` + FFI bindings per package;
  reproducible lockfile.
<a id="l9-3"></a>
- **L9.3 Incremental build cache** — key module IR/objects by source hash +
  dependency graph (L6.7); only rebuild dirty subgraphs (`gusty build --cache`).
<a id="l9-4"></a>
- **L9.4 Richer LSP** — go-to-definition, find-references, rename, hover type
  display (uses L6.3/L6.5 narrowing), inline error squiggles from the
  error-recovering lexer/parser (L4.1/L5.2).
<a id="l9-5"></a>
- **L9.5 Formatting on save** — `gusty fmt` as an LSP `textDocument/format`
  provider, round-tripping f-strings, trailing commas (L5.3), and docstrings.
<a id="l9-6"></a>
- **L9.6 Fuzz/parity CI** — extend `proptest.go` to differential-test the
  lexer/parser (parse → format → reparse) and both backends on the new
  surface (unions, async, walrus), seeded + deterministic.

<a id="phase-10"></a>
### Phase 10 — cross-cutting: targets, ABI, portability (2026)

**Goal: Pyre runs anywhere.**

<a id="l10-1"></a>
- **L10.1 WASM target** — lower AOT to WebAssembly via LLVM; `gusty --target=wasm`
  for browser/edge runtimes; the scheduler (L7.1) maps to `wasm` event loop.
<a id="l10-2"></a>
- **L10.2 ABI stability** — a versioned, documented C ABI for `extern fn` exports (stable struct layout for unions/tagged values across releases). ✅ DONE (Round 2)
  exports (stable struct layout for unions/tagged values across releases).
<a id="l10-3"></a>
- **L10.3 Shared-library export** — `gustyc --build <out> --shared` emits a
  position-independent `.so`/`.dylib` with the stable ABI (`BuildShared`,
  `cc -shared -fPIC`; object already PIC via `llc -relocation-model=pic`;
  versioned ABI marker carried, so extern exports stay stable across
  `dlopen`/loads). ✅ DONE (Round 3)
<a id="l10-4"></a>
- **L10.4 Benchmark harness** ✅ DONE — `gustyc --bench-suite` measures a corpus
  (built-in, or every `*.gy` in `--bench-dir`, so the `integration/` parity
  programs become benchmark cases) on both execution backends and emits a
  versioned JSON artifact; `--bench-baseline` gates a run against a saved
  baseline (best-of-N on the AOT leg, tolerance + noise floor, per-regression
  `suggestion`) so a slowdown surfaces as a number with its own exit code (5).
  See ADR 0162, `docs/benchmark.md`.

<a id="phase-11"></a>
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

<a id="l11-1"></a>
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
    **Latent, and now closed (ADR 0232)**: dict key lookup and set dedup compared payloads only,
    so they became unsound the moment a string and a number could collide — L11.1 (1b) carried the
    tag into `rt_dict_get`/`rt_dict_has`/`rt_set_add`/`rt_contains`, not only into equality.
  - ✅ **Done (ADR 0232): mixed dicts and sets, and the lookup that reads the tag.** The compiled
    path refused `{"a": 1, "b": "x"}` and `{1, "a"}` outright ("either strings or numbers, not
    both"), and — the part that mattered more — answered questions wrongly for the containers it
    *did* accept. Strings are `@str_tab` indices, so an interned string and an integer of the same
    number are the same bits: `d = {1: "one"}; print(d["a"])` answered `one` compiled while the
    interpreter and CPython raise `KeyError`, and `1 in {"a"}` was True. That is the latent note at
    the end of the ADR 0189 entry above, collected.
    A slot is now the pair everywhere: `d[k] = v` writes key tag *and* value tag with the payloads
    (`rt_dict_put_tagged`), membership and lookup compare both (`rt_dict_find`, `rt_dict_get_tagged`
    + `rt_dict_value_tag`, `rt_dict_has_tagged`, `rt_set_contains_tagged`, `rt_contains_tagged`, and
    `rt_mixed_eq` for `if x == "a":` on a tagged loop variable), `rt_set_discard_tagged` shifts tags
    with payloads, and a mixed container announces itself with a new `@estr` bit 8 ("the slots
    describe themselves") so `rt_dict_print`/`rt_set_print` dispatch to the mixed printers while
    uniform containers keep their static paths untouched. `for k in d` / `for x in s` bind
    `%_k`/`%_k_tag` exactly as ADR 0185's list loop does; `print(d[k])` and `v = d[k]` fetch the
    value's tag at the read site. **Lookups compare tags whenever the needle's kind is provable,
    uniform container or not** — bit 8 is not the gate, because the wrong answer was happening to
    containers that never mix.
    Growth stopped being a refusal too: `xs.append("a")` on a list of numbers, `s.add("a")` on a set
    of numbers, and `d["b"] = "x"` on an int-valued dict now *promote* the container (available only
    because ADR 0189 made every builder write tags). The dict case had been answering
    `{'a': 'x', 'b': 'x'}` for `{"a": 1}` + `d["b"] = "x"`, because item assignment overwrote the
    container's recorded kind and the print followed it; likewise `xs = [1, 2]; xs[0] = "s"` printed
    `['s', 'b']` — the untouched 2 rendered as whatever string its index names. Both are pinned by
    parity rows now, AOT ≡ interpreter ≡ CPython, and the two `string_containers_test.go` "must be
    refused" rows for growth were inverted (ADR 0230's lesson, applied again).
    Two refusals were added where an answer would have been a guess, both new to this cycle: an
    element whose kind cannot be proven — `def pick(c): return "z" / return 7`, which printed
    `(null)` for the 7 because `print` asks at print time and a *slot* is labelled once — and a
    needle whose kind cannot be proven against a container that mixes. Bools inside containers are
    accepted and tagged `TagInt`, matching what both backends store today; when L11.2 gives bool its
    own kind, one line of `elemKindTag` changes and containers follow. New unit file
    `pkg/lang/mixed_dict_set_test.go`, new integration file
    `integration/mixed_container_test.go` (parity + oracle + refusal + verifier rows).
  - 🟢 **Remaining**, in order: (1a) ~~the other element-wise *reads*~~ — done (ADR 0187);
    the next element-wise uses need tagged values at the *use* site, which is L11.2; (1b) ~~mixed
    *dicts* and *sets*~~ — done (ADR 0232), including the tagged lookup ADR 0189 left as a latent
    note; (1c) floats in containers, which
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
<a id="l11-2"></a>
- **L11.2 — `str()` vs `repr()` are one pair per backend (closes Gap L.2)** ✅ DONE (ADR 0258) —
  the pair is one renderer on each side, asked which of its two forms the caller is in.

  *What was measured first (2026-10-02, three engines, before touching anything).* `repr` existed in
  neither backend: `repr(1)` was `NameError` on the interpreter and `unsupported call "repr"`
  compiled, both exit 1. `str` was a **second** renderer, and a form it had not been told about came
  back as the number underneath the value:

  | program | CPython | interpreter | compiled, before |
  |---|---|---|---|
  | `xs = [1, 2]` · `print(str(xs))` | `[1, 2]` | `[1, 2]` | **`0`**, exit 0 — the heap handle's digits |
  | `print(str(None))` | `None` | `None` | **`0`**, exit 0 |
  | `x = 1.5` · `print(str(x))` | `1.5` | `1.5` | refusal `str on non-integer`, exit 1 |
  | `print(str({1}))`, `print(str(set()))` | `{1}`, `set()` | same | **exit 2** — `llc` rejected the module |
  | `xs = [[1, 2], [3]]` · `print(str(xs))` | `[[1, 2], [3]]` | same | **`[1, 2]`** — the two inner handles |
  | `xs = ["a", 1]` · `print(str(xs))` | `['a', 1]` | same | **`[0, 1]`** — the two intern indexes |
  | `xs = []` · `xs.append("v" + str(7))` · `print(xs)` | `['v7']` | `['v7']` | **`(null)`**, then `['v7]` once a runtime repr existed |

  Every one of those had exit 0. A missing rendering was answering as a number, which is what made
  the defect survivable: nothing distinguished it from an answer, and the two backends agreed with
  each other on `0` often enough that the matrix had nothing to say.

  *The design that closed it.* The compiled printers no longer call `printf`. All 51 of their writes
  go through `rt_out_txt` (text) and `rt_out_int` (digits), which are pointed either at stdout or —
  while `str()`/`repr()` render — at a capture buffer whose bytes come back interned. `str` and
  `repr` are then: point the sink, run the printer `print` would have run, intern what was written.
  The `quote` flag ADR 0185 introduced for container elements now carries the pair, and it is read
  only by the text arms at the top of a render, because a text is the one value whose two halves
  differ; inside a container both halves quote, which is why `print(xs)` and `str(xs)` cannot drift.
  On the interpreter side `repr` delegates to the same `Repr` that `print` and `str` already shared
  and differs only in quoting a text. `--json --eval 'repr("hi")'` reports
  `{"result": "'hi'", "type": "str"}`; `str()`/`repr()` of a form the expression does not name is a
  refusal in words, exit 1, naming which half is missing — never the number underneath, never exit 2.

  *What the fix surfaced, and had nothing to do with `str`.* A container assigned from a literal
  wrote per-slot tags but never said so on the object: the element kinds were recorded only in the
  compiler's scope, so `print` — which chooses its printer from that record — was right while anything
  that *asks the object* was wrong. The set and dict assignment paths had marked `@estr` bit 8 ("the
  slots describe themselves") all along; the list one had not, and no assignment path marked bit 1 for
  interned text either. That is the same asymmetry Gap L.2 was, one level down, and it is why
  `print(["a", 1])` printed `['a', 1]` while `str(["a", 1])` answered `[0, 1]` from the same object.
  Objects now describe their own slots on every assignment path (`heapListFrom`, the list-assignment
  builder, the dict and set twins).

  *Where the pair still stops, and why.* It renders what the expression can name: a container literal,
  a registered container variable, a constructor whose inferred type names its kind, a text, a float,
  a verdict, `None`, a number. A value handed over as an untagged word whose kind no expression names
  still takes the digits arm — both halves agree, which is what this row promised, and both are wrong
  together, which is filed as Gap R.115. A container returned from a function is Gap R.67's
  (`ret i32 @.lst1`, exit 2, unchanged by this row); a bool inside a container prints the number the
  verdict is stored as (Gap R.112, unchanged); a tuple is L11.3's and refuses; and an f-string cannot
  interpolate a container variable at all (Gap R.114).

  *Coverage.* `pkg/lang/render_pair_test.go` is the table: 55 value forms through both backends, each
  expected to write CPython's answer — the expectations taken from `python3`, because pinning the pair
  to itself is how this defect stayed alive — plus a negative table that fails if a form answers
  `0`/`1`/`2`, a `print`-vs-`str` agreement table, and an IR row that fails the day a second value
  renderer reappears in a module. `integration/render_pair_test.go` drives 21 of them through the CLI
  legs, the `--json` machine leg, and the `--oracle` leg (a matching pair program is exit 0; a bool
  inside a rendered container is still exit 6, which is Gap R.112 being reported rather than hidden).
  `programs/probe_render_pair.gy` joined the conformance corpus and is `match` on all three legs. Two
  refusal rows left `slot_division_test.go` (unit and integration): `str()` is a context a double now
  can travel into, because the renderer that prints a float writes into the capture buffer — that
  program is pinned at CPython's `3.0` instead, and the sinks that store an `i32` word still refuse.

<a id="l11-3"></a>
- **L11.3 — Tuples are values, not syntax sugar** ⏳ PLANNED — `TupleLit` has no
  AOT lowering at all (`unsupported expression *lang.Tuple`, so
  `def pair(): return (1, 2)` cannot compile), and the interpreter prints a tuple
  as `[1, 2, 3]`. Ship the tuple type in both backends: immutable, indexable,
  unpackable, hashable as a dict key, printed `(1, 2)`/`(1,)`/`()`. Feeds the
  covariant `tuple[...]` rule of L6.6, which today only has a checker to talk to.
<a id="l11-4"></a>
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
<a id="l11-5"></a>
- **L11.5 — Code-point strings (closes Gap N.2)** ⏳ PLANNED — `len("café")` is 5,
  `"héllo"[1]` is the byte `195`, and `for c in s` at module scope emits an
  invalid `store i32 @.str1`. Decide the representation **once for both backends**
  (the gap's own rule: a half-migration is worse than the divergence): UTF-8 bytes
  + a decode/measure helper shared by `len`, `s[i]`, `s[i:j]`, `for c in s`, the
  interned table and the printers; `len` counts code points, `s[i]` returns a
  one-code-point string. Extend `TestStringLengthIsBytesForNow` into the oracle
  test it will become.
<a id="l11-6"></a>
- **L11.6 — Numeric truth in the compiled backend (closes Gaps P.1 + P.2)**
  ⏳ PLANNED — floored `//` on negative ints (`-7 // 2` → `-4`), `x /= 2` yields
  `4.0`, float-through-untyped-parameter keeps its float (`f(0.1)` → `0.2`) via a
  float-parameter inference in exactly ADR 0174's shape, Python-floored `%` on
  floats (`-3.5 % 2.0` → `0.5`, wrong on *both* backends today), `floor`/`ceil`
  return `int`, and **stdlib constants keep their type** — `print(math.PI)` prints
  `3` compiled today, because on-disk data-only modules fold to `int`. Settle the
  `--bench`/golden expectations in the same commit.
<a id="l11-7"></a>
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
<a id="l11-8"></a>
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
  **Closed since this row was written**: the refusal that covered the interned-string comparison —
  a filter comparing elements with a string literal, and the same comparison in a `for` loop, which
  made `llc` reject the module — is gone, because both bugs under it are fixed: a string value is an
  `@str_tab` index (Gap R.42) and the filtered loop's `phi` names a predecessor that really branches
  to it (ADR 0224). `probe_str_loop_eq` and `probe_comp_str_filter` were promoted to parity programs.
<a id="l11-9"></a>
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
<a id="gap-l-5"></a>
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
<a id="gap-l-6"></a>
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

<a id="gap-j"></a>
## Gap families J, K, L, M, N, P, Q, R

_Rows of the tracker's gap-family tables. Verbatim record._

<a id="gap-j-2"></a>
## Gap J — found while closing earlier gaps (2026-09-27)

Surfaced by the L8.2 module verifier and by writing both-backend print coverage.
Each is a concrete, reproducible defect with the shape to fix it.

<a id="gap-j-1"></a>
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
<a id="gap-j-2-2"></a>
- **Gap J.2 — set/dict comprehension assignment (AOT)** — ✅ DONE (ADR 0234, closing what
  ADR 0165 left open).
  Fixed by ADR 0165 first: the interpreter now renders a set as `{1, 2}` (and `set()` when
  empty) instead of `<set>`, matching `rt_set_print`, so a set prints the same on both
  backends. What remained was the binding, and it turned out to be four defects, not one.

  The fold emitted `@.setN` / `@.dictN` — a `{i32, [n x i32]}` private global, a length plus an
  array — and then every consumer asked that global for a *value*:

  ```gy
  sa = {x for x in [3, 1, 2]}        # store i32 @.set1, i32* %_sa          -> llc exit 2
  print({x for x in [3, 1, 2]})      # rt_print_list_mixed(i32 @.set1, 0)   -> llc exit 2
  print(2 in {x for x in [1, 2]})    # rt_contains(i32 @.set1, i32 2)       -> llc exit 2
  sa = {x for x in xs if x > 1}      # parse error: expected keyword "else"  -> never reached
  ```

  Three invalid modules and one parse failure, all in the two container kinds that had never been
  carried over from the list path: ADR 0188 (print builds the object, the printer renders it) and
  ADR 0163 (a binding allocates, tags and registers) had both been written for `staticLists` alone,
  and print's `*Comp` branch asked one printer for all three kinds — so even a valid module would
  have rendered `{1, 2}` as `[1, 2]`.

  **Root cause of the parse half** was a precedence choice, not a grammar hole: `parseDictOrSet`
  parsed the iterable with `parseExpr()`, a full expression, and a full expression is a ternary —
  which read the comprehension's `if` as *its* `if`. `parseListOrComp` had learned this lesson
  earlier (`parseExprPrec(precOr)`); the braces never got the same line, so `[x for x in xs if c]`
  parsed and `{x for x in xs if c}` did not. Both brace branches — the one inside the element list
  and the one after `}` — are fixed, and an `or` iterable still parses (`{x for x in a or b if …}`).

  **The fix is a deletion, not a mechanism**: `foldSetComp` / `foldDictComp` return the
  `*SetLit` / `*DictLit` a folding comprehension denotes, `comp()` emits its global from that
  literal and records it in `staticSets` / `staticDicts` beside `staticLists`, and the binding rule
  binds the literal through the path `d = {1: 2}` already used. `runtimeCompLoop` — ADR 0192's real
  loop, previously list-only — fills a set with `rt_set_add_tagged` and a dict with
  `rt_dict_put_tagged`, and the binding records `runtimeSets` / `runtimeDicts` from the kind written
  in the syntax, which is what makes `print(sa)` ask the set printer instead of printing the handle
  (`1`) as a number. Measured before that record was added: interpreter `{2, 3}`, CPython `{2, 3}`,
  compiled `1`.

  **Re-measured after** (three engines, `gustyc --interp` / `--aot` / CPython 3.12.3): every shape
  above agrees, and the conformance program `programs/comp_containers.gy` prints CPython's answer
  line for line — set members written ascending, so CPython's hash-ordered rendering and the
  documented insertion-order convention say the same line and the matrix row is a `match`, not a
  `debt`. Guard: `TestFoldedContainerComprehensionNeverSitsInAValuePosition` scans whole modules for
  a folded container global in an operand position over seven programs, and
  `TestContainerComprehensionRefusalsStayHonest` requires the set twin to refuse in exactly the words
  its list twin uses.

  **Still refused, honestly** (both refusal texts are the list spelling's, asserted): an iterable the
  compiler folded away (`xs = [1, 2, 3]` then `{x for x in xs}` — `xs.append(...)` materialises it),
  owned by L11.1's tagged element; and a non-integer iterable, key or element, owned by L11.5/L11.6.

  **Found on the way, owed elsewhere** (`Gap R.67`): the same operand question one statement further
  out — a container *returned* from a function. `return [1, 2]` is `ret i32 @.lst1` (exit 2), and
  `la = [1, 2]; return la` compiles and prints `0`. It affects literals, so the comprehension did not
  create it; the binding simply stopped refusing at the assignment and now reaches it.
<a id="gap-j-3"></a>
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
<a id="gap-l-1"></a>
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
<a id="gap-k-10"></a>
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
<a id="gap-j-4"></a>
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
<a id="gap-j-5"></a>
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

<a id="gap-j-6"></a>
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

<a id="gap-m"></a>
## Gap M — CLI shapes the tests never typed (found 2026-07-29)

<a id="gap-m-1"></a>
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

<a id="gap-k"></a>
## Gap K — truthiness (found writing the L9.6 corpus, 2026-09-27)

<a id="gap-k-1"></a>
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
<a id="gap-k-2"></a>
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
<a id="gap-k-4"></a>
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
<a id="gap-k-5"></a>
- **Gap K.5 — `{}` was a set in the interpreter and a dict in AOT** — ✅ DONE.
  Python's `{}` is an empty dict; the parser's brace classifier fell through to `SetLit`
  when there were no elements, so `d = {}` then `d[k] = v` failed with `not in set` on
  the interpreter while the AOT emitted dict code for the same source. `{}` now parses to
  an empty `DictLit` (`{1, 2}` is still a set), pinned by `TestEmptyBracesIsAnEmptyDict`.
<a id="gap-k-6"></a>
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
<a id="gap-k-8"></a>
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
  prints one frame per stack level. Its prerequisite has since landed — L8.5 put a real line
  table in the module (ADR 0231), so the frames an unwinder could read are there; what is
  missing is the runtime half, an explicit frame stack pushed at each call site to walk.
<a id="gap-k-7"></a>
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
<a id="gap-k-3"></a>
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

<a id="gap-n"></a>
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

<a id="gap-n-2"></a>
### Gap N.2 — strings measure bytes, not code points (OPEN)

`len("café")` is 5 (bytes) where CPython says 4, `"héllo"[1]` returns the byte
`195` rather than `"é"`, and `for c in s` walks bytes. This is documented in
`docs/language.md` and pinned by `TestStringLengthIsBytesForNow`, which records
both answers. Code-point semantics are a **representation decision for both
backends at once** — `len`, `s[i]`, `s[i:j]`, `for c in s`, the interned table,
and the container printers all agree on bytes today — so a half-migration (one
backend only) is worse than the divergence. Do it as one change, or as a tagged
string representation alongside Gap L.2.

<a id="gap-m-2"></a>
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

<a id="gap-p"></a>
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

<a id="gap-p-1"></a>
### Gap P.1 — compiled division shapes still wrong (OPEN)
- `//` on negative **ints** truncates in AOT (`-7 // 2` → `-3`, Python `-4`);
- `x /= 2` keeps the integer representation (prints `4`, Python `4.0`);
- a **float through an untyped parameter** truncates: `def f(x): return x * 2`
  with `f(0.1)` prints `0`. This is the static float-typing model, not the
  operator — it needs float-parameter inference in the same shape as ADR 0174's
  string-parameter inference.

**Re-measured 2026-10-04**, three shapes, one program (`programs/probe_float_numeric`, which now carries
all three), each leg pinned line by line by the oracle harness:

| line | CPython | `--interp` | `--aot` |
|---|---|---|---|
| `print(-7 // 2)` | `-4` | `-4` | `-4` |
| `print(-3.5 % 2.0)` | `0.5` | `0.5` | `0.5` |
| `x = 8` / `x /= 2` / `print(x)` | `4.0` | `4.0` | **`4`** |
| `def dbl(v): return v * 2` / `print(dbl(0.1))` | `0.2` | `0.2` | **`0`** |
| `def bump(v): return v + 1` / `print(bump(1.5))` | `2.5` | `2.5` | **`2`** |

The two flooring rows are the pair ADR 0216 paid, and they are the control: the same emitter, the same
operator road, the right answer. What the three diverging rows have in common is that **the value is a
double and the storage is not** — `/=` decides a variable's word at its first write and the first write
was `x = 8`, and a parameter's word is decided by its declaration, which says nothing. Every one of them
leaves at **exit 0** with a number-shaped answer, which is the outcome ADR 0166 ranks worst.

The third row is new to the record and is why the probe grew it: `dbl(0.1)` → `0` could be explained away
as a multiplication whose operand never got its double, but `bump(1.5)` → `2` is an *addition*, so the
explanation cannot be about the operator; it is about the parameter. That is the difference between this
row and Gap R.130 (a `for` binding over a literal container of doubles), and the reason the row stays open
until both words — the variable's and the parameter's — come from what flows into them rather than from
the first line that happened to mention them.

<a id="gap-p-2"></a>
### Gap P.2 — numeric builtins that Python types differently (CLOSED by ADR 0264, 2026-10-03)
- ~~`floor`/`ceil` return a float where Python's `math.floor` returns an `int`~~ — closed by ADR 0264,
  which also explains why the row's own wording ("docs say 'the largest double <=', so this is a
  deliberate-but-questionable choice") was the problem rather than the answer: the phrase describes a
  *value* and was being read as a *type*, by the compiler, by the language doc's reader and by the test
  that pinned `2.0`. What the reference returns is an `int`, and `print(floor(2.7))` now prints `2`.
- ~~float `%` uses truncated (`math.Mod`) rather than Python's floored modulo~~ — closed by ADR 0216.
- ~~the `round` tie rule~~ (Gap R.50) — closed by ADR 0236.

The row is closed because all three named defects were re-measured against CPython on both engines in the
commit that closed it, not because the ledger said so: `-3.5 % 2.0` → `0.5`, `round(2.5)` → `2`,
`round(3.5)` → `4`, `round(-0.5)` → `0`, `floor(2.7)` → `2`, `ceil(2.2)` → `3`, each identical on
`--interp`, `-aot` and `python3`. What L11.6 still owes is elsewhere in its own row (`/=` → float, a float
through an untyped parameter, stdlib constants keeping their type), not here.

<a id="gap-q"></a>
## Gap Q — container-kind rebinding and a fixed 1024-slot heap (found 2026-09-28, L7.2)

Both surfaced while making the compiled collector precise (ADR 0181): one as a
wrong answer that reproduced identically at HEAD, one as the ceiling the new root stack
revealed underneath. Neither is a root-set bug — they are what the collector was hiding.

<a id="gap-q-1"></a>
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

<a id="gap-q-2"></a>
### Gap Q.2 — the runtime heap is a fixed 1024 objects, and exhaustion is silent (OPEN)
`@heap` is `[1024 x {i32, i32, [256 x i32]}]`; when it fills, `rt_alloc` returns **-1**
and the program carries on with an invalid handle. Before ADR 0181 the collector kept
everything alive, so a heap-stress program reached the ceiling and died; the root stack
fixed the retention but not the ceiling. Two things are missing, and neither is subtle:
an out-of-memory **diagnostic** (raise `MemoryError`-style and exit non-zero like any
other runtime failure, instead of returning -1), and a heap that grows — either larger or
segmented — with the capacity reported in `--gc-stats` so a workload's headroom is
visible (the interpreter's `--gc-stats` already has `live`; the AOT has `live` and `top`).

<a id="gap-r"></a>
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

<a id="gap-r-3"></a>
- **R.3 / R.3b, closed by ADR 0196** — a parameter was a constant in the compiled backend
  (assigning to one was ignored, an accumulator loop never terminated), and a `for` loop over
  `range` wrote its counter through the user's variable. Both were refusals to run ordinary
  programs, and the `--aot` leg of a pinned program could hang forever, taking the whole
  package timeout with it.
<a id="gap-r-5"></a>
- **R.5, closed by ADR 0197** — a `def` below the code that uses it was refused as
  `undefined name`, so mutual recursion could not be compiled at all while the interpreter
  ran it. The checker now resolves a name to any `def` of the enclosing *function* scope,
  and still refuses what runs where it is written: a module-level call, or a decorator.
<a id="gap-r-4"></a>
- **R.4, closed by ADR 0198** — a program that defined `sync`, `exit`, `write`, `time` or `main`
  was emitted under those exact link names, so libc answered its own calls (and `def main`
  collided with the generated entry point, which made the program unbuildable). Program-owned
  symbols now carry a `gy_` prefix and nothing else keeps its name unless the program did not
  define it — which is why `extern fn` still binds the C name it declares.
<a id="gap-r-6"></a>
- **R.6, closed by ADR 0199** — a program that defined `str`, `float`, `sqrt`, `chr` ...
  had its call read through the *built-in's meaning* by codegen: `float(1)` folded to a conversion
  (printing `1.0` for a function returning `x + 7`), and `str(1)` was emitted as the program's call
  and then printed as the built-in's string, which `llc` rejected. One predicate now decides —
  does the program own this name? — in front of every shape reading keyed on a built-in name.
<a id="gap-r-8"></a>
- **R.8, closed by ADR 0200** — a module function and a method that shared a name shared the
  checker's function key too, so the module call was measured against the method's
  `self`-inclusive arity and the program was refused with an undefined-name error on the callee's
  own parameter — while the evaluator, the compiled module and CPython all ran it.
<a id="gap-r-10"></a>
- **R.10, closed by ADR 0201** — calling a function with too few arguments was reported by nobody:
  the parameter arrived unbound and the program was blamed afterwards, at the callee's line, for an
  undefined name — while the interpreter already refused it at run time with the message the
  checker never gave.
<a id="gap-r-7"></a>
- **R.7, closed by ADR 0202** — the checker repeated itself structurally (per-call-site return
  inference re-walks a callee's body), so one warning appeared two or three times and the length of
  the JSON diagnostic array was not a count of findings.
<a id="gap-r-9"></a>
- **R.9, closed by ADR 0203** — `print` and `range` were in the lexer's keyword table, so a program
  could not define a function, a parameter, a keyword argument or a method with those names:
  `def print(x)` failed with `expected identifier` before anything else saw it.
<a id="gap-r-12"></a>
- **R.12, closed by ADR 0205** — a program-defined built-in name was visible to codegen *above* its
  definition, so `for i in range(2)` iterated the program's value while the interpreter used the
  built-in: two programs from one file, with no diagnostic anywhere on the way.
<a id="gap-r-14"></a>
- **R.14, closed by ADR 0207 as a declared feature** — `for i in 4:` printed `0 1 2 3` on both backends
  while CPython refused it, and nothing said so: now documented, ledger-pinned and tested, boundaries
  included.
<a id="gap-r-15"></a>
- **R.15 (OPEN, compiler bug)**, found while measuring those boundaries — `for c in "ab":` makes the
  compiled backend emit `store i32 @.str1, i32* %_c`, which `llc` rejects: an invalid module is a
  compiler bug under the exit-code contract, not a user error.
<a id="gap-r-13"></a>
- **R.13 (OPEN)** — found while building the program that proves R.9: a file's final bare expression statement is
  echoed by the interpreter and by nobody else (closed below, ADR 0204); and `for x in 5` iterates on
  both backends while
  being undocumented and refused by CPython.
<a id="gap-r-11"></a>
- **R.11, closed by ADR 0206 as "not a defect"** — `def f(a, b=1, c)` looked like a missing CPython
  refusal, but measurement showed the shape binds correctly in both engines; it is now a documented
  divergence with a ledger row that excludes the oracle.

Each of these was discovered by writing a corpus program rather than by reading code, which is why
they are all reproducible as programs.

<a id="gap-r-1"></a>
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

<a id="gap-r-2"></a>
### R.2 — see the re-scoped entry below (closed by ADR 0209)

This entry once read "`await` in a loop-exit position emits an undefined intern function", and its
analysis — "this is the await path's intern accounting, not the loop's" — was wrong on both counts. The
re-scoped entry further down has the await-free repro, the real cause, and the fix.

<a id="gap-r-3-2"></a>
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

<a id="gap-r-3b"></a>
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

<a id="gap-r-3c"></a>
### R.3c — a parameter rebound to a float, returned as a bare name (CLOSED, ADR 0254)

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
unnoticed. **It was paid in ADR 0254, and the diagnosis above was half wrong**: the type was indeed decided
twice, but the second decision did not need the tagged value word — it needed to be asked of the predicate the
emitted body already asks (`isFloat`), before the header is written. See *Gap R.3c — paid beside ADR 0254* at the
end of this file. Related, and also fixed by (e) of ADR 0196: a float-returning function whose
body assigns a parameter used to emit a second `alloca` of the same name — an `llc`
"multiple definition of local value" rejection — and compiles now.

<a id="gap-r-4-2"></a>
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

<a id="gap-r-5-2"></a>
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

<a id="gap-r-6-2"></a>
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

<a id="gap-r-9-2"></a>
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

<a id="gap-r-12-2"></a>
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

<a id="gap-r-13-2"></a>
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

<a id="gap-r-14-2"></a>
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

<a id="gap-r-15-2"></a>
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

<a id="gap-r-2-2"></a>
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

<a id="gap-r-16"></a>
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

<a id="gap-r-17"></a>
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

<a id="gap-r-18"></a>
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

<a id="gap-r-20"></a>
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

<a id="gap-r-21"></a>
### R.21 — "a `return` inside `try:` loses its value" (NOT A SEPARATE DEFECT — refiled as R.22)

Recorded while fixing R.18, and wrong: it reproduced with `return a % b` and not with
`return a + b`, which is not a property of `try`. The function's two `return` paths had different
*types* (int and string), it was specialised as one, and the caller rendered the integer as an
interned-string index — `(null)`. That is the mixed-return-type defect below. The probe was renamed
`probe_mixed_return_value.gy` and its ledger row reworded; the measured pin never changed, which is
the value of pinning the output rather than the theory. Keep the standing lesson: characterise the
trigger before naming the cause — a wrong cause sends the next cycle to the wrong file.

<a id="gap-r-21-2"></a>
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

<a id="gap-r-23"></a>
### R.23 — `finally` did not run when the `try` body returned, broke, continued, or raised (CLOSED, ADR 0222)

```gusty
def f() -> int:
    try:
        return 1
    finally:
        print("fin")      # was: both backends print 1 and nothing else · CPython: fin, then 1
```

Measured on eleven shapes against CPython before touching anything: **nine were wrong, and both
backends were wrong identically** — the deferred body ran on fall-through and after a handled
exception, and was skipped for `return`, `break`, `continue`, and for an exception no arm matched.
Parity could not see it; only the third leg could.

The same statement had a second wrong half, found in the interpreter's arm matching. Transfers
(`returnSignal`, `loopSignal`) travel as Go errors — the same channel raised exceptions use — and the
arms asked "did something come out?" instead of "did an *exception* come out?", so a bare `except:`
**caught a `return`**: it printed its body and dropped the function's value, where CPython returns.

Fixed in both paths. The interpreter runs the arms (only for `catchesException`) and then the deferred
body, exactly once, before releasing the pending transfer; a `return`/`raise` inside the `finally`
returns its own signal, which is Python's last-transfer-wins for free. The compiler keeps a
`deferred [][]Stmt` stack in `irGen`: `tryStmt` pushes its body for its body and arms, and each
transfer site consults it — `return` emits the value, then the pending bodies, then the frame close and
`ret`; `break`/`continue` do the same before branching; the "no arm matched" edge and a `raise` inside an
arm run **only the innermost** body, because the outer ones are run by their own statements when the
hand-off reaches their handlers (running the whole stack there printed `outer fin` twice). Judging
whether a body already left the block is done by *where control went* — `ret`, `unreachable`, a branch
to the raise-exit — not by opcode, since an `if` or a loop also ends its block with a `br`.

Value-before-deferred ordering is Python's and is observable: `return n` with a `finally` of
`n = 99; print("fin", n)` prints `fin 99` then `1` on both backends.

Tests: `pkg/lang/deferred_bodies_test.go` (10 units incl. the arms-cannot-catch-transfers half and a
control that a "just run it everywhere" fix would have passed) and `integration/deferred_bodies_test.go`
(20 shapes × 2 engines, expectations written from CPython, and the trap shapes asserted at exit class 3
from the documented contract rather than from observation). `programs/deferred_bodies.gy` is a
standalone parity program. Matrix: 66/92 parity, oracle 47 match / 31 debt / 14 not-applicable.

<a id="gap-r-41"></a>
### R.41 — a method is now a call like any other (CLOSED, ADR 0223)

`emitClassMethod` was a code path that had never been brought back to parity with `funcDef`, and
three separate wrong interfaces came out of it. Measured before the change, re-checked after:

| shape | compiled before | CPython |
|-------|-----------------|---------|
| `try`/`finally`, `try`/`except`, `break` through a `finally` in a method | exit 2 — `br label %`, an empty target | `m fin`/`3`, `handled`/`4`, `fin`/`9` |
| `raise` out of a method, uncaught | **prints `0`, exit 0** | traceback, exit 1 |
| `raise` out of a method, caught by the caller | **prints `0`, then the arm** | the arm alone |
| nested `self.bad()` that raises | **prints `0`, the arm never runs** | the arm |
| a construct codegen refuses, inside a method | **exit 2** (half a function emitted) | — |

The empty label was `g.funcRaiseExit`, set by `funcDef` and never by the method path. The silent `0`
was that plus no call-site exception check: the flag was set, and the caller had already taken its
value. The dropped `g.stmt` error was the sneakiest — any refused construct inside a method produced a
truncated function and an `llc` rejection, exit 2 blaming the compiler for a source error (ADR 0166).

Now: the method gets its own `Class_method.raiseexit` block (which closes the root frame it opened, so
an unwind does not leak a frame — ADR 0181), its `handlerStack`/`handledArms`/`deferred` are cleared
for its body, its traceback frame is named `Class.method`, its first refusal is recorded in
`g.emitErr` and returned by `GenerateIR`, and **every** call site into program code checks the flag —
static dispatch, `super().m()`, the class-id `switch`, and a constructor's `__init__`. Inside a
`switch` arm the check has to finish the arm, so `checkExnLabel` hands back the continuation block and
the join's `phi` names *that* as its predecessor: a call site that can raise cannot also be a value
producer for the join.

Tests: `pkg/lang/method_unwind_test.go` asserts the artifacts — the raise-exit block present, no
branch to an empty label, an `@exn_flag` load after a method call, `rt_frame_close` on the unwind
path, no branch from inside a method to `main.raiseexit`, `VerifyModuleIR` clean, and a refusal inside
a method reported as a compile error — and `integration/method_exceptions_test.go` runs seven shapes on
both engines plus the exit-code contract. `programs/method_try.gy` is a standalone parity program, and
the debt row for this shape was deleted by the drift test that demands exactly that when a debt is paid.

<a id="gap-r-42"></a>
### R.42 — a string value is an `@str_tab` index, not the address of a literal (CLOSED, ADR 0224)

```gusty
class Dog:
    def sound(self) -> str:
        return "woof"

print(Dog().sound())        # interpreter and CPython: woof · compiled: exit 2
```

The defect was not the method emitter: it was the **value** path. `value()` returned the string
global for a literal, so every shape that reads a string as a value reached `llc` as an `i32` holding
an address — `ret i32 @.str1`, `icmp eq i32 @.str1, %t1`, `icmp eq i32 %getresult2, @.str2` — and the
compiler took the exit-2 blame for an ordinary program. A measured matrix of 22 string shapes (each
expectation taken from CPython, not from the emission) found five such refusals and one silent wrong
answer, and none of them was specific to methods.

The rule now: a string value is an index into `@str_tab`; the address of a literal appears only where
bytes are the question (a `printf` format, an argument to `rt_str_*`, a compile-time fold). Interning
a constant emits `call i32 @rt_str_intern2(...)`; text the runtime creates itself is interned there;
printing an index reads it back with `rt_str_ptr`; folds that *produce* strings intern their result.
Element-kind facts follow the value: `exprIsString` answers the static question for a comprehension
element so `listElemStr` can be recorded, and an `-> str` method registers under its mangled symbol so
its callers know the `i32` they hold means text.

Two things underneath had to give way at the same time. The static-dispatch emitter began the `call`
line before evaluating arguments, so an argument needing an instruction of its own was written into
the middle of the operand list; and the filtered comprehension's loop header declared the *body* as its
back edge when the increments came from the *skip* block, which is the second bug the L11.8 refusal had
been covering (`PHI node entries do not match predecessors!`). Both fixed, with the `phi` asserted to
name the block that actually branches to it.

Closed by: `pkg/lang/string_value_test.go`, `TestStringElementFilterCompiles`,
`integration/string_value_test.go`, parity programs `string_values.gy`, `str_loop_eq.gy` and
`comp_str_filter.gy` (the last two are promoted probes; the matrix no longer carries their rows).
What remains is recorded as R.46 and R.45 rather than left implicit.

<a id="gap-r-40"></a>
### R.40 — a container slot is a word: ask what fits before writing it (CLOSED, ADR 0226)

```gusty
print(1 if [1] == [1.0] else 0)   # before: exit 2 -- [1 x i32] [@env_store = internal global ...
print([1.5, 2])                    # before: exit 2 -- %t1 = sitofp i32  to double
print(1 if 1.0 == [1] else 0)      # before: exit 2 -- %t2 = sitofp i32 @.lst1 to double
xs = [1.5]
print(xs[0])                       # before: printed 1   (a truncated float, silently)
print(1 if {1.5} == {1.6} else 0)  # before: printed 1   (CPython: 0 -- true by truncation)
```

Recorded from two repros, measured at six, and the measurement also found three answers that had been
green: `{1.0} == {1.0}` compiled to True through the same truncation that made `{1.5} == {1.6}` True.
The rule is one question asked in one place — `heapElemKind`, which every container path already goes
through, now asks whether the compiled word can hold the element, and refuses with the element kind,
the missing representation and the roadmap item that owns it (L11.6).

Two mechanism fixes came out of reading the emitted text rather than the error strings:

- `emitList` wrote the global's opening text *before* validating elements, so a refusal mid-loop left
  an unterminated `@.lstN = private global ...` in the module, and every downstream path that caught
  and ignored that error shipped it. It builds the whole definition, then writes it once.
- `valueText` was `v, _ := g.value(b, e); return v` — the third error-swallow this loop has found (after
  `truthyValue` in ADR 0225 and the method emitter in ADR 0223), and the one that produced
  `sitofp i32  to double`. Failures now report into the generator and `GenerateIR` refuses: nothing that
  is about to be refused may also be executed.

`1.0 == [1]` is answered by kind (ADR 0215 with ADR 0221's numeric exception), not by coercing a
container global through a float conversion.

Closed by: `pkg/lang/container_element_test.go` (including the artifact blacklist —
`sitofp i32  to double`, `sitofp i32 @.lst`, `[1 x i32] [@`, `ret i32 @.`, `icmp eq i32 @.` — run over
the whole family), `integration/container_element_test.go`, parity program
`integration/programs/kind_mismatch_equality.gy`. What remains is L11.6 (a float that can live in a
slot, which turns each refusal into an answer) and R.37 for the ordering comparisons.

<a id="gap-r-46"></a>
### R.46 — printing an element of a freshly built comprehension list prints its index (OPEN, compiled)

```gusty
names = ["a", "b"]
names.append("c")
out = [n for n in names if n == "a"]
print(out[0])           # CPython: a · compiled: 0
```

`0` is the interned index of `"a"` printed with `%d`. The list knows its elements are strings once
something has walked it — the same program with a preceding `for n in names:` loop prints `a`, because
that loop is what registers the element kind under the shared variable name. A parity program that
happened to have such a loop is exactly the kind of test that goes green for the wrong reason, so it
now asserts only what the oracle agrees with, and this shape is pinned at what the compiler does
(`TestPrintingAnElementOfAFreshComprehensionListIsPinned`). Fix: have the comprehension's list-binding
ask `exprIsString` of the element *and* of the iterated name, and have the element printer fall back to
the same facts the container printer uses.

<a id="gap-r-45"></a>
### R.45 — a subscript of a string is a one-character string (CLOSED, ADR 0225)

```gusty
s = "abc"
print(s[1])             # CPython: b · both backends before: 98
print(s[0] + s[2])      # CPython: ac · both backends before: 196
print(1 if s[1] == "b" else 0)   # CPython: 1 · both backends before: 0
```

Recorded from the interpreter side, but the compiled fold had the same missing type and produced the
same wrong values, and nothing in the suite noticed because the two backends agreed with each other —
only the CPython leg can see that `98` is not `b`. Sixteen measured shapes, zero matching before.
The character is a string now, allocated in the interpreter and interned in codegen, and the unit it
agrees on is the code point: `len("café")` is 4 (was 5), `"café"[3]` is `é`, `s[2:]` cannot cut a
character in half, `ord` takes the first rune.

What the measurement uncovered on the way was worse than what we came for: `truthyValue` returned
`asI1(b, "0")` when a condition failed to lower, believing "the enclosing statement path still has
the error" — the ternary path had no error channel, so `print(1 if s[1] == "b" else 0)` emitted
`icmp ne i32 0, 0` and printed `0`. A part of a program that cannot be lowered is a compile error,
never a default value (asserted in `TestUnlowerableConditionIsAnErrorNotAFalseBranch`).

Closed by: `pkg/lang/string_subscript_test.go`, `integration/string_subscript_test.go`, parity program
`integration/programs/string_subscript.gy`.

<a id="gap-r-47"></a>
### R.47 — a compiled string is a compile-time value only (CLOSED, ADR 0229 + ADR 0230)

```gusty
s = "abc"
i = 0
print(s[i])          # interpreter and CPython: a · compiled: refuses
print(len(s[1]))     # interpreter and CPython: 1 · compiled: refuses
print(s[1].upper())  # interpreter and CPython: B · compiled: refuses
print(ord(s[1]))     # interpreter and CPython: 98 · compiled: refuses
for c in s:          # interpreter and CPython: a, b, c · compiled: refuses
    print(c)
```

**Closed — no new representation was needed** (ADR 0229 reads, ADR 0230 writes). A compiled string
value is already an `@str_tab` index (ADR 0224) and `rt_str_intern` already appends by content, so
the table grows while the program runs: the operations take indices and return indices, and print,
equality, substring tests and container slots keep working on a string the compiler never saw. Ten
runtime helpers in all — `rt_str_from_bytes`, `rt_str_nchars`, `rt_str_char`, `rt_str_codepoint`,
`rt_str_case`, `rt_str_cat`, `rt_str_byteoff`, `rt_str_slice`, `rt_str_strip`, `rt_str_of_int` — plus
a `for`-over-string that drives the ordinary counter loop with `rt_str_nchars`/`rt_str_char`.

What was actually missing was a *question*: each operation asked "can the compiler read this string's
text?" where the question is "is this a string?", which is why `"abc"[1]` answered and `get()[1]`
refused, why `s[1].lower()` printed text while `s[1].upper()` printed `2`, and why a correct slice
printed `1`. The whole-program kind scan (`scanStringBindings`) and the two classification predicates
now answer once — and adding an operation means adding its case to operations, print and the scan in
the same commit.

Both halves are covered against CPython: `programs/runtime_string_ops.gy` (reads) and
`programs/runtime_string_writes.gy` (writes) are matrix rows declared `match`, and
`TestCompiledStringSubscriptAnswersAtRuntime` / `TestCompiledStringWritesAnswerAtRuntime` refuse to
run if CPython disagrees with the table. Iterating a runtime string is the shape that had been
*silently* wrong rather than refused — the table index read as a repeat count, printing nothing with
exit 0 (Gap R.16) — so those tests assert output, not acceptance.

Not claimed by the closure, and written down rather than half-implemented: compiled case folding is
ASCII only and `strip` trims the ASCII whitespace set (the interpreter has both full tables);
`str(<float>)` refuses rather than truncate a float it cannot yet hold (L11.6); `%s` formatting is
Gap R.31; `str * int` and list concatenation are Gap R.33; the table's 4096-entry bound raises a
catchable `RuntimeError` (ADR 0229).

Process finding worth keeping: `for c in txt()` had quietly become the canonical "the compiled
backend refuses" fixture in four tests — the exit-code contract, the trap-class contract, the
oracle's refused-leg check and a diagnostics table. When the shape became answerable they went green
while exercising nothing. Four fixtures moved to `print("ab" * 2)` (still refused) and the pinned
refusal tables were emptied into CPython-checked answer tables. A pinned refusal is a claim about
the future: when the gap closes, the pin has to move.

<a id="gap-r-48"></a>
### R.48 — there is no `global` statement (OPEN, language surface)

```gusty
def touch():
    global gz
    if 0:
        gz = 1
    return gz

print(touch())
```

CPython reads `global gz` as a declaration and the read raises `NameError: name 'gz' is not defined`.
gusty has no such statement, so the line parses as the *expression* `global gz`: the interpreter reports
`name 'global' is not defined`, and the compiled backend refuses the program. Three engines, three
answers, on a construct every Python reader will reach for — and the worst property of the three is that
two of them are wrong in ways no one reading the file would predict.

Pinned as `programs/probe_global_statement.gy` (an oracle row with both legs' behaviour recorded), found
during ADR 0228's probe pass and left out of that cycle rather than folded in, because the fix is the
statement itself: a declaration that routes reads and writes of the named entries to module state — which
is exactly the `@gy_mod_<name>` storage ADR 0227 already built for rebound module scalars, so the
machinery is there and only the surface is missing. The checker side needs the mirror rule of ADR 0228's
`seedLocalsFromBody`: a `global` name is *not* a local of the frame, whatever the body assigns.

<a id="gap-r-30"></a>
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

<a id="gap-r-31"></a>
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

<a id="gap-r-33"></a>
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

<a id="gap-r-35"></a>
### R.35 — a function cannot read a module-level name (CLOSED for scalars, container/float half OPEN, ADR 0220 + ADR 0227)

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

**Compiled: still open.** Four shapes refuse with `undefined name "MAX" (no binding for it; assign it
before use)` — a sentence whose claim that the interpreter reports the same error is now measurably
false (Gap R.38 gained a second instance) — and one shape is worse than the others: a nested `def`
reading a module name compiles and prints `0`, because the slot is never written (the Gap R.36
signature). The method-shaped silent zero that used to be in that list is gone: since ADR 0223 stopped
`emitClassMethod` discarding its body's refusal, a method reading a module name reports the same
`undefined name` error as everyone else — still the wrong answer for the front end to give, but an
honest one, and it is asserted in the refusal table.

**Closed for scalars (ADR 0227).** Three rules, all measured first:

- a name the module binds once to a literal, and never rebinds, is a *value*: a body reads it and no
  slot is needed. `MAX = 40; def twice(): return MAX * 2` answers 80 on both backends;
- a name the module rebinds is module *state*, and state outlives a frame: it lives in a
  `@gy_mod_<name>` global that main writes and any callee loads, so `LATE = 0; def read(): return LATE;
  LATE = 3; print(read())` prints 3 — ADR 0220's call-time lookup, finally compiled. A main-frame
  `alloca` could not do this: it is dead by the time the callee runs;
- a binding inside a body is local, decided by what the body binds anywhere inside itself and not by
  the order slots happen to be allocated in, so `K = 5` at module level with `def f(): K = 1; return K`
  prints `1 5`, not `5 5`.

Containers are deliberately left out: giving a body the handle of a module list without the container
operations behind it would trade an honest refusal for a half-working answer, so `def n(): return
len(xs)` over a module list still refuses — and now refuses with a message naming module state, instead
of the old `len of a non-string variable` / `string method append on non-constant string`, which
described a list as a string (Gap R.38's third instance).

The closure-body swallow that hid the silent zero is gone too: `emitClosureDef` reported nothing when a
statement failed and the function fell through to `ret i32 0`, the third emitter found doing this
(after `emitClassMethod` in ADR 0223 and `truthyValue` in ADR 0225). Its one documented exemption is a
closure nested in a function used as a decorator — a decorated call runs the trampoline, not that
closure, and the trampoline reports its own failures — and the deferred failure is printed in the
module (`; note: closure wrap: body not lowered …`) where `--emit-llvm` can see it.

`programs/module_scope_in_functions.gy` (both legs `80 7 5 40 1`) and `programs/module_calltime_lookup.gy`
(both legs `3 1`) are the oracle rows; `TestModuleScalarsReachCompiledFunctionBodies` replaced the test
that pinned the refusals and the silent zero, and names the container refusal that is still owed.

<a id="gap-r-39"></a>
### R.39 — reading a name the body also assigns below should be UnboundLocalError (CLOSED, ADR 0228)

```gusty
v = 10
def f() -> int:
    print(v      # CPython: UnboundLocalError — gusty: prints 10
    v = 1
    return v
```

A consequence of Gap R.35's lenient fallback, recorded rather than glossed: the frame does not know
which names are locals before it runs, so the read falls through to the module instead of refusing.
**Closed (ADR 0228).** The plumbing was exactly that: `seedLocalsFromBody` pre-records the names a body
binds anywhere inside itself, and `Evaluator.bodyBinds` consults it at the read — a name the frame owns
is looked for in the frame alone, never in the module, and an unbound one raises `UnboundLocalError`
with CPython's own message. The compiled backend raises the same class from the same rule (a written-flag
on the slot), so the two backends and CPython now agree on stdout, on class, and on exit 3. Measured
before the change: CPython `UnboundLocalError`, both gusty engines `NameError`, and the compiled leg for
the sibling shape (`if c: x = 1`) printed `0` and exited 0. `UnboundLocalError` got its own code in the
canonical exception table, so `except UnboundLocalError:` matches on both engines (ADR 0212's rule that a
built-in trap is a typed raise).

<a id="gap-r-36"></a>
### R.36 — an unwritten variable slot reads as raw memory instead of raising (CLOSED, ADR 0228)

Any name whose only assignment sits on a path that did not run, read at module level:

| shape | compiled | interpreter | CPython |
|-------|----------|-------------|---------|
| `if 0: x = 1` then `print(x)` | prints `64` | NameError | NameError |
| `while 0: w = 1` then `print(w)` | prints `64` | NameError | NameError |
| `try: a = 1 // 0` / `b = 2` / `except: pass`, then `print(b)` | prints `1630496` | NameError | NameError |
| untaken `match` arm's capture, then `print(y)` | prints `518304` | NameError | NameError |
| `for i in []: f = 1` then `print(f)` | compile refusal (`codegen:`) | NameError | NameError |
| `def f(c): if c: x = 1` then `return x`, called with `False` | prints `0`, **exits 0** | traps (NameError — the class is R.39's) | UnboundLocalError |

**Closed (ADR 0228).** Fourteen shapes measured across three engines; nine were compiled-only silent
wrong answers (`0`, `64`, `8555776`, `518208` — frame leftovers and stale handles), four were compile-time
refusals for programs CPython runs, and every one of them now prints what CPython prints and traps with
the class CPython raises. The mechanism is one byte per slot *the checker cannot prove was written*: the
checker answers which (`UnwrittenReads`, the same walk that already warns `possibly unbound`), codegen
adds a flag only where it can hook every write to that name, entry clears it, writes set it, reads test
it, and the failure is a typed raise through the ordinary unwind path.

Three checker rules were the underlying cause and were fixed as part of it: a `for` body may run zero
times (names assigned in it were being carried out as certain, which `while` already got right); a
`match` may match nothing (names every arm binds are certain only under an irrefutable pattern); and a
loop variable *is* certain inside its own body — over-correcting the first rule warned about `for i in
range(n): total = total + i`, which is nonsense, and that test is now in the suite.

Also fixed on the way, because the probes exposed it: `main.raiseexit` returned 1 — the compile-error
code — so the *linked binary* reported a compiler bug for a program that merely raised, while the CLI
said 3. ADR 0211 says one class, one code, whichever path produced it; the binary now returns 3, and the
two tests that pinned the `1` were updated with the cause named.

`programs/unwritten_slot_trap.gy` is the pinned artifact (its oracle row records what all three legs do);
`pkg/lang/bound_flag_test.go` asserts the flag exists where it is needed and *nowhere else*, and
`integration/unwritten_slot_test.go` runs twelve shapes against CPython on both engines. What remains
open in this family is the deliberately narrow part: names bound by a `for` header or `with ... as` carry
no flag (a check whose flag some writer forgot would be a spurious trap, which is worse than the bug), and
float- and container-valued slots store elsewhere, so they are still uncovered.

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

<a id="gap-r-38"></a>
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
| `len of a non-string variable` — `def n(): return len(xs)` over a module **list** | (implies the value is a string) | **prints 2**; the value is a list ✗ — fixed by ADR 0227's `moduleStateErr`, which names module state instead |
| `string method append on non-constant string` — `xs.append(2)` on a module **list** | (implies the receiver is a string) | appends to a list ✗ — same fix, same site |

Both new rows are fixed: a body that reaches module container state is now refused by a message that
says what is missing (module-level state, Gap R.35) rather than describing a list as a string. The
lesson generalises — a refusal template that asserts something about *another backend* or about an
*operand kind* must build its sentence from the gate that knows, and `moduleStateErr` is that gate here.
The `undefined name` sentence keeps its claim because it is still checked against a real NameError.

The message is one template, `operator %q on a string is not supported in the AOT backend; the
interpreter evaluates it`, parameterised by operator — so it asserts the same thing about `%`, which
no backend implements, and `*`, which one does. The fix is to stop pasting the claim and derive it:
`checkBinOp(op, l, r)` in the interpreter is the single predicate that answers "would this operand pair
raise?", so codegen can ask it and say either "the interpreter evaluates it" or "neither backend
supports this yet (Gap …)". A refusal is the last thing a stuck program prints, and a sentence about
the other path that is true for the operator two lines above and false for this one is how someone
ends up trusting an answer that was never available.

<a id="gap-r-37"></a>
### R.37 — a constant operation that should trap is refused at compile time (OPEN, compiled only; one instance closed by ADR 0228)

**One instance closed (ADR 0228).** Four of the R.36 shapes were this bug in the same costume: `for i in
[]: z = 1` then `print(z)`, and `print(v)` above `v = 2`, were refused at compile time with `undefined
name` while CPython runs them and traps at the read. They now compile and raise. The remainder of this
entry is about constant-folded arithmetic (`1 // 0`, `"a" * 3` in a position the folder can see), where
the same argument — a refusal is a different event from a trap — still applies.

`[][0]` and `int("x")` anywhere in a program, even inside a `try` the handler of which would catch
it, exit 1 with `gustyc: jit: codegen: list index out of range` / `int on non-integer string`. CPython
traps at runtime and the handler runs; the interpreter agrees (`41`). Constant folding has become an
evaluation of code the program may never execute, and its failure is reported as a compilation error
rather than as the runtime trap the source asks for. Two consequences: a program that *deliberately*
provokes and catches such an error cannot be compiled at all, and a refusal appears for a line that
would never have run (`if debug: x = [][0]`). The fold must produce an IR-level trap — the same
`gy_trap` call the dynamic path emits, with the same class — whenever the operation would raise; the
only constants it may resolve are ones the program cannot avoid executing.

<a id="gap-r-8-2"></a>
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

<a id="gap-r-10-2"></a>
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

<a id="gap-r-11-2"></a>
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

<a id="gap-r-7-2"></a>
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

---

<a id="recovered-rows"></a>

## Recovered and newly measured rows (tabulation, 2026-10-01)

The roadmap's free text had drifted from the record it described: some IDs were cited by
comments in `pkg/lang`, by ADRs and by the conformance ledger but had no row at all, and a
few rows still asserted a state an earlier cycle had already changed. The tabulation into
[`../roadmap.md`](../roadmap.md) recovered the missing rows here (each one sourced from its
ADR, its test and its ledger pin — nothing invented) and re-measured the stale ones with
`gustyc` at HEAD. Items are `⏳ PLANNED`/`⏳ OPEN`/`✅ DONE` only in the tracker; this is
their provenance.

<a id="gap-r-19"></a>

### Gap R.19 — a missing attribute answers `0` in the compiled backend

`getattr` on a name an instance does not have should raise `AttributeError`. The
interpreter and CPython agree; the compiled backend answers with a value, so the compiled
leg of `programs/probe_builtin_traps_untyped` never completes (the ledger pins the
interpreter's five handler lines against a missing aot leg). `integration/builtin_trap_classes_test.go`
carries the divergence and says so out loud: "compiled backend now traps on a missing
attribute — Gap R.19 looks fixed; delete this test and close the gap". ADR 0214 is the
interpreter half; the compiled half is the open row.

<a id="gap-r-22"></a>

### Gap R.22 — returns of differing types share one lowering

The original reading (Gap R.21) blamed `try`/`except` for losing a `return`'s value. ADR
0213 refiled it: the defect is that a function whose return paths have different types is
lowered as returning *one* of them, so a compiled caller reads the integer as an interned
string index and prints `(null)` where the interpreter and CPython print `3` — silently,
exit 0. `programs/probe_mixed_return_value` is the pin (`interp "3\n"`, `aot "(null)\n"`).
The tagged value word (L11.1) is the fix; the ADR that refiled the reading is 0213, and
ADR 0221 keeps the numeric case honest meanwhile.

<a id="gap-r-24"></a>

### Gap R.24 — a compound statement binds in the enclosing scope (✅ DONE, ADR 0217)

Names bound inside `if`/`for`/`while`/`match`/`try` bodies belong to the enclosing block,
which is what the interpreter already did and what the flat `vars` map means. The checker
both refused legal programs and called a partial binding undefined. Fixed with ADR 0217;
`pkg/lang/scoping_test.go`, `integration/scoping_test.go` and `programs/compound_scoping.gy`
hold it.

<a id="gap-r-25"></a>

### Gap R.25 — a built-in trap carries the class its handler matches on (🟨 PARTIAL, ADR 0212 + 0214)

A trap raised with a message but no exception class is invisible to `except`: the
interpreter half is closed (ADR 0212 made built-in traps typed raises; ADR 0214 gave the
remaining ones a class), and `pkg/lang/builtin_trap_classes_test.go` +
`integration/builtin_trap_classes_test.go` keep it closed. The compiled half stays pinned:
`programs/probe_builtin_traps_untyped` has the interpreter's five handler lines and an
absent aot leg, and its case is Gap R.19.

<a id="gap-r-26"></a>

### Gap R.26 — an operator applied to operands it cannot apply answered a number (✅ DONE, ADR 0215)

"An operator is a question about two runtime kinds": `pkg/lang/operator_operand_test.go`
pins the interpreter's verdicts (and its messages) for the operand shapes where a wrong
answer used to be printed. ADR 0215 is the gate; the compiled mirror is Gap R.27.

<a id="gap-r-27"></a>

### Gap R.27 — the compiled backend has no operand-kind check

The AOT half of Gap R.26, recorded separately because it is a separate fix: the compiled
backend answers `1 + None` with a value instead of raising and refuses the rest of the
probe at compile time, so `programs/probe_operand_types` never completes natively.
`integration/operator_operand_test.go` fails with "Gap R.27 may be fixed; re-check and
delete this test" the moment it does.

<a id="gap-r-28"></a>

### Gap R.28 — `//` and `%` followed Go's truncation in the interpreter (✅ DONE, ADR 0216)

Python floors; Go truncates. `-7 // 2` is `-4`, and `%` keeps the sign of the divisor —
one rule for both operators rather than two operators with two opinions. Closed together
with Gap R.30 (the compiled `//`) in ADR 0216; `pkg/lang/floor_division_test.go` and
`integration/floor_division_test.go` are the grid.

<a id="gap-r-29"></a>

### Gap R.29 — int/float equality is one question in either order (✅ DONE, ADR 0221)

`1 == 1.0` was answered differently depending on which side the literal sat on. ADR 0221
made it one question about two numbers; `pkg/lang/number_equality_test.go`,
`integration/number_equality_test.go` and the three-engine row `programs/numeric_equality.gy`
keep both orders pinned.

<a id="gap-r-49"></a>

### Gap R.49 — a module variable whose name was also a parameter gets no slot (found by the 2026-10-01 sweep)

```
def f(x): return x * 2
x = 8
x /= 2
print(x)
```

The interpreter prints `4.0`, CPython prints `4.0`, and the compiled backend does not
build: codegen emits `store i32 %t5, i32* %_x`, and `llc-20` rejects the module with
"use of undefined value '%_x'" — an ordinary program leaving the compiler as exit **2**, a
compiler bug under the exit-code contract (ADR 0211). Renaming either binding (`def g(q)`,
or the module name) compiles, so the fault is name-keyed: the module-level `x` is treated as
already bound because a *parameter* of that name exists in an earlier body. It is the same
disease as Gap I.3's leaked per-body state (ADR 0163) and R.36's slot bookkeeping (ADR 0228):
a variable's slot is allocated once per call, and a name that some other scope used must not
count as an allocation the module can reuse. Fix with the binding-scope rules that L11.1's
tagged slots land on, and pin it with a parity program plus an IR-shape test that the module
allocas are hoisted and per-scope.

<a id="gap-r-50"></a>

### Gap R.50 — `round` ties away from zero; CPython ties to even (found by the 2026-10-01 sweep)

```
print(round(2.5))   # CPython 2   gusty --interp 3   gusty --aot 3
print(round(3.5))   # CPython 4   gusty --interp 4   gusty --aot 4
print(round(-0.5))  # CPython 0   gusty --interp -1  gusty --aot -1
print(round(0.5))   # CPython 0   gusty --interp 1   gusty --aot 1
```

Both backends agree with each other and disagree with Python, which is exactly the class
of defect the two-backend parity matrix can never see: `round` rounds halves away from
zero where CPython rounds to the nearest *even* value. The oracle leg of the conformance
matrix is the only instrument that can find it, and the fix belongs with L11.6 — the same
cycle that settles numeric typing — because `round` also has to keep its result type
(`round(2.5)` is an `int` in Python). The pin should be an oracle row, not a parity row, so
that the fix is what closes it.

**Closed 2026-10-02 (ADR 0236).** Both backends now ask for the named IEEE operation — `math.RoundToEven`
in the evaluator, `call double @llvm.roundeven.f64` in the compiled runtime, `math.RoundToEven` again in
the compiled constant fold — and `round(2.5)`/`round(0.5)`/`round(-2.5)` print 2/0/-2 on all three
engines.

The interesting part is where the wrong rule lived. Not in `round` — in **four** places: the evaluator,
the compiled runtime, the compiled fold, and four tests. Two of those tests stated the rule in prose
(`must round half-away in the AOT binary`, `rounds half-away-from-zero in both interpreter and AOT`) and
one pinned `i32 3` in the emitted IR. A test that restates a rule is not a check on it; it is a second
authority for it, and four authorities agreeing is what makes a wrong answer survive. Parity could not
see it by construction — it compares the two implementations to each other — and the CPython leg, the
one instrument that could, was not in those tests. So the fix is recorded as the four inverted pins as
much as the two code changes: each keeps its old expectation in a comment, because the record of what
we asserted is how the next one gets caught.

The corpus program `integration/programs/round_ties.gy` deliberately has **no ledger row**: under this
repo's convention that means "must print what CPython prints", checked on both gusty legs, so the old
answer is now a CI failure rather than a passing parity. Literal ties and variable ties are separate
cases because the fold and the intrinsic are separate paths in codegen: fixing one and leaving the
other would have kept half of every program wrong, and — as the fold's `math.Round` shows — that is
exactly what happened originally.

One stale claim in `docs/language.md` died with it: the builtin paragraph said `round(x)` truncates,
and folds-and-is-a-no-op, and that "the AOT backend has no float representation" — three states of a
builtin, in one paragraph, none of them current.

**Found while writing its test (Gap R.69, below):** `round(x, ndigits)` — ignored by the interpreter,
refused by the compiler with the *wrong exit class*.

<a id="gap-r-51"></a>

### Gap R.51 — three names the checker knows, the interpreter traps on, and the compiler answers (found by the 2026-10-01 sweep)

`floor`, `ceil` and `sqrt` are in `pkg/lang/predeclared.go`, so the checker calls a program
that uses them well-typed (`gustyc check` says `ok`); the interpreter has no such builtin
and traps (`NameError: name 'floor' is not defined`, exit 3 through `--interp`); the
compiled backend lowers them and answers — with the wrong type, since `print(floor(3.7))`
prints `3.0` where CPython prints `3` (Python's `math.floor` returns an `int`). One name,
three engines, three behaviours: the predeclared table's "deliberately generous" list has
outgrown the implementations, and AGENTS.md's rule is that a feature lives in both paths or
is documented as living in one.

The sweep also says which of those names are *honestly* missing: `bool`, `bytes`, `tuple`,
`map`, `filter`, `isinstance`, `repr`, `hash`, `id`, `getattr`, `setattr`, `hasattr`,
`callable`, `pow`, `divmod`, `fabs` trap in the interpreter **and** are refused by codegen
with `unsupported call "<name>"` — a capability gap on both paths, tracked by L11.8 (the
refusal needs a stable code) and by L11.2/L11.3/L11.7 where a value model would make the
name real. What is not acceptable is the three-way split above: either lower a name on both
paths, or refuse it on both, and say which in the diagnostic.

**Closed 2026-10-03 by ADR 0264.** The three names are implemented on both paths, and the row's own
definition of done — `print(floor(3.7))` prints an `int`-shaped `3` — was measured rather than asserted:

```gusty
print(floor(2.7))   # CPython 2 · --interp 2 · -aot 2      (was: NameError 3 / 2.0)
print(ceil(-2.2))   # CPython -2 · --interp -2 · -aot -2   (was: NameError 3 / -2.0)
print(floor(7))     # CPython 7 · --interp 7 · -aot 7      (was: NameError 3 / 7.0)
print(sqrt(9))      # CPython 3.0 · --interp 3.0 · -aot 3.0 (the float answer, unchanged)
print(floor(2.7) + 1.5)          # CPython 3.5 · 3.5 · 3.5
print([floor(2.7), ceil(2.2)])   # CPython [2, 3] · [2, 3] · [2, 3]   (was [2.0, 3.0] compiled)
print(str(floor(2.7)))           # CPython 2 · 2 · 2                  (was 2.0 compiled)
print(sqrt(-1))     # ValueError: math domain error on all three, exit 3 where catchable
print(floor("a"))   # TypeError: must be real number, not str on all three
```

The value table is 30 shapes wide (halves, both signs, whole floats, `True`/`False`, a container, an
f-string, `str()`, an `==`, a comparison in an `if`, a variable, a function parameter, a product of two
`sqrt`s) and both engines print CPython's bytes on 29 of them; the 30th is the already-filed loop-variable
wall (Gap R.130), pinned not asserted. The trap half is twelve shapes, each with the reference's sentence
and each catchable by class.

Two things this row's closure discovered, filed as their own rows because they are not this row's
question: a folded non-finite constant could not be emitted at all (Gap R.134, fixed in the same commit
because `sqrt` needed it), and the predeclared table's other dead names, including one the reference runs
(Gap R.136). And one thing it could not close: the answer past the compiled `int` word (Gap R.133), which
is L12.12's decision to make.

<a id="gap-r-52"></a>

### Gap R.52 — stdlib discovery is relative to the working directory (found by the 2026-10-01 sweep)

`pkg/lang/resolve.go` walks up from the **current working directory** looking for a
`stdlib` directory, so this works from inside a checkout and fails anywhere else:

```
$ cd /home/donutloop/Workspace/gusty && gustyc --interp /tmp/pp/m1.gy   # 3.141592653589793
$ cd /tmp/pp                    && gustyc --interp m1.gy                # cannot import module math, exit 3
```

The escape hatches exist and are documented (`--stdlib <dir>`, `$GUSTY_STDLIB_DIR`, and
`docs/language.md` § On-disk stdlib modules), which is why nobody has tripped over them
yet: the development workflow always runs from the repo. An *installed* binary has no repo
above it, so the first thing a user does — `pyre hello.gy` with `import math` in it —
traps. The fix is one more entry in the search list: beside the executable, and beside
`argv[0]`'s `../share/pyre/stdlib`, with the root actually searched reported in `--json`
(a machine should be able to ask which stdlib answered). It belongs with L9.2, because a
package manager is the moment the binary leaves the repo.


---

<a id="phase-12"></a>

## Phase 12 — the Python-visible surface contract (surveyed 2026-10-01)

Phase 11 asks what a value *is*. Phase 12 asks what the language *says*: whether a program
written from memory of Python compiles, runs, and means what its author meant. The trigger was
a request to check the language against modern language-design expectations for 2026, and the
honest way to answer that question is not to enumerate features in the abstract but to write the
programs and read the three answers.

**Method.** 76 programs, each covering one construct or one closely-related pair, written to be
idiomatic Python rather than gusty-shaped Gusty. Each ran through `gustyc --interp --file`,
`gustyc --aot --file` and `python3` on identical source, and was classified by what the three
legs *did*, not by what the file intended:

| Class | Programs | Examples |
|---|---|---|
| `MATCH` — CPython-equal on both backends | 9 | `f"hello {name} {1 + 2}"`, `for … else`, `yield from`, `print(…, sep=, end=)`, `sorted(…, reverse=True)`, `str.index` / `str.count`, annotated `def f(xs: list[int]) -> int`, `with` + `__enter__` / `__exit__`, `print(1 in [1, 2])` (modulo the bool rendering of Gap L.2) |
| `ABSENT` — both engines refuse, CPython runs | 37 | `@dataclass`, `class Color(Enum)`, `Protocol`, `TypedDict`, `from … import … as …`, `del`, `assert`, `nonlocal`, `global`, `...` as a body, `raise … from`, `case [*rest]`, `case T() as x`, `case T(kw=…)`, `f(*xs)`, `f(**d)`, `*args` / `**kwargs` / keyword-only, `getattr` / `setattr` / `hasattr` / `repr` / `map` / `filter`, `bytes` / `frozenset` / `tuple()`, set operators, `__name__` |
| `AOT-REFUSES` — the interpreter agrees with Python, codegen declines | 16 | `list(...)`/`keys()`/`items()` copies, `str.split`, a method on a container held in an instance field, `super()` through a string concatenation, `@staticmethod` / `@classmethod`, `__setitem__`-shaped attribute reads |
| **`SILENTLY-WRONG` — every leg runs, gusty disagrees** | **8** | comparison chains, `2 ** 63`, f-string specs, `print(obj)` vs `__str__`, the dict-pattern binding, bool rendering (Gap L.2), integer overflow (Gap R.64's family), `list(<container>)` shapes that answer instead of refusing |
| **`HANG`** | **2** | `for` over `__iter__` / `__next__`: 7.3 M lines in 15 s interpreted, zero iterations compiled |
| rejected everywhere / interp-only / other | 4 | probes whose CPython reference itself needs a stdlib gusty does not have |

Five of the `AOT-REFUSES` are not refusals at all: they emit a module `llc` **rejects**
(`%t1 = add i32 @.lst1, @.lst2` for `[1] + [2]`, `%t1 = mul i32 @.lst1, 2` for `[1, 2] * 2`, and
`x += [2]` for the same reason), which the exit-code contract (ADR 0211) reports as exit **2** —
a compiler bug — rather than as a refusal. That is Gap R.33's signature and the phase's clearest
illustration of the rule below: the same `str * int` shape *does* refuse cleanly, so the
difference between a refusal and an invalid module is whether anyone wrote the check.

**The three-state rule, which is the phase's actual deliverable — [ADR 0233](adr/0233-every-construct-is-implemented-refused-or-absent-never-answer-wrong.md).** Every construct is
**implemented** (both backends, CPython-equal), **refused** (a stable `Diagnostic.Code`, a
documented exit class, a line in `docs/language.md`), or **absent** (not in the surface
manifest). "Parses, runs, prints something" is not a state and must not be one. The rule is the
same instinct that made built-in traps typed raises (ADR 0212) and made one event mean one exit
code (ADR 0211); what is new here is applying it to the *grammar* rather than to the runtime.

**How to re-run the census.** Per program, `gustyc --oracle <src> --json` already reports the
three legs and the verdict, with exit 0 on a match, 6 on a gusty disagreement and 7 when the
oracle cannot judge (ADR 0186); `--interp` / `--aot` supply the legs directly. The 2026-10-01
sweep was driven by an external harness over 76 files and its raw table is the four columns
above; **the permanent form of this census is the corpus**, and that is L12.13's definition of
done — each of these probes becomes a `programs/surface_*.gy` row, the manifest publishes the
verdicts, and a construct that ships without appearing in the manifest fails CI. Until then the
numbers above are a measurement with a date, not a standing check, and the record says so rather
than implying a test that does not exist.

**Two observations that are not defects but belong on the record.**

• `with` runs on both backends and is documented, but the protocol's *arity* is unenforced: a
one-argument `__exit__(self, e)` runs happily where CPython raises `TypeError`. `docs/language.md`
documents the three-argument form. Making protocol conformance a front-end fact rather than an
accident belongs with the protocol rows (L12.2, L12.3), not with `with` itself, which works.

• The bundled standard library is four modules and 24 lines in total: `math` is seven constants
(no `sqrt`), `string` is six character tables, `collections` is four constants, `json` is three
(`NULL`, `TRUE`, `FALSE`) — there is no `json.dumps`. `import math` therefore "works" while
`math.sqrt(16)` answers `no name sqrt in module`, which is a worse experience than a missing
module because it looks like a typo in the program. Discovery is Gap R.52; *breadth* is the
stdlib row L12.11 sits beside, and the ADR for this phase should decide what the stdlib
guarantees before anyone writes another constant table.

<a id="l12-1"></a>

### L12.1 — comparison chains

`(a < b) < c` is what the current grammar means, and `True < 3` is a comparison gusty will happily
answer. The single-line form that makes Python's rule work is a parse- and AST-level change: a
comparison node carrying `n` operands and `n-1` operators, with each middle operand evaluated once
and fed to both neighbours. Doing it at the AST (rather than desugaring in the parser to
`a < b and b < c`) is what keeps the semantics honest — `and` short-circuits, and Python does not
evaluate the tail of a chain lazily in the way that matters here: with a call in the middle,
`f() < g() < h()` must call `g` exactly once, and a desugaring written carelessly calls it twice.
The checker gets `bool` for the whole chain, which also gives Gap L.2's bool rendering something
correct to render once L11.2 lands. Alternatives rejected: keeping left-associativity and
special-casing `bool` operands (fixes the observed cases, leaves the operator wrong); refusing
chains (a refusal for the most ordinary comparison in the language is the kind of refusal this
loop has repeatedly decided against).

<a id="l12-2"></a>

### L12.2 — the iterator protocol

The interpreter already calls `__next__`; what it does not do is read `StopIteration` as the end
signal, so a correct iterator loops until the harness kills it — 7.3 million printed integers in
fifteen seconds. The compiled backend answers "no elements" for the same object, exiting 0. Both
wrong halves come from the same omission: there is no *iterator protocol*, only a special-cased
`range`/container loop. The design is one dispatch — ask the value for an iterator, ask the
iterator for a value, stop on `StopIteration`, and raise a typed error on anything else — shared
by `for`, `unpacking`, `list(...)`, `zip`, `enumerate` and the comprehension machinery, so the
next protocol-shaped feature is a table entry rather than a new loop. A protocol codegen cannot
lower is refused by name (with a `Diagnostic.Code`), never answered with zero iterations, because
"empty" and "cannot tell" are the two answers a program cannot distinguish and only one of them
is honest. `__aenter__` / `__aexit__` and mid-body `await` stay with L7.6a, where the coroutine
object is the blocker.

<a id="l12-3"></a>

### L12.3 — `print` renders by protocol

`print(C())` printing `0` in the compiled backend is not a rendering bug; it is an untagged handle
being fed to `%d`, i.e. Gap R.22's disease showing up at the printer. The interpreter's
`<instance>` is at least honest-looking, which is a lesson in itself: a wrong answer that looks
like a placeholder survives longer than one that looks like a number. The design is the
value-form table L11.2 is already building (`str` for `print`, `repr` inside containers, one pair
per backend) with one extra lookup in front of it: does the class define `__str__` / `__repr__`?
`repr` also has to become a real built-in — it is absent from `predeclared.go`, so the idiom that
debuggers, tracebacks and every container's rendering depends on is currently unwritable. Order
matters: L11.2 defines the pair, this item puts the class in front of it.

<a id="l12-4"></a>

### L12.4 — the class object: attributes and MRO

Two measurements, one missing structure. A class body that binds only methods means `C.X` is an
`AttributeError` and `class Color(Enum): RED = 1` is unwritable — so "there is no enum" is really
"there is no class namespace", which is cheaper to fix and unblocks a family of idioms
(constants, counters, registries, `@dataclass` field tables, and the enum type itself as stdlib
written in gusty). Second, the interpreter resolves an inherited attribute through the first base
only and fails on the second, while the compiled backend resolves both — the rare inversion where
the AOT leg is right, and the direct evidence that the linearisation must be computed once and
consulted by both legs, not reimplemented. C3 is the documented algorithm and the small one to
implement; the row's definition of done includes the case CPython itself calls out (a diamond
resolving the way C3 says, not the order of `bases`), because an MRO that works for one base and
two is an MRO nobody has tested.

<a id="l12-5"></a>

### L12.5 — the descriptor built-ins

Decorator *application* works in the interpreter — that half is a known AOT limit. What does not
exist is `property`, `staticmethod` and `classmethod` as built-ins with a binding rule, which is
why `@property` silently leaves a method object in the class dictionary: the decorator runs,
returns something, and nothing asks what that something means when an attribute is read. The
design is a descriptor table — name → (`__get__` semantics, `__set__`, whether `self` is passed,
how it lowers) — read by the checker (so `T(3).d` types as the property's return), the interpreter
(so the read applies it), and codegen (so `S.s()` lowers instead of refusing). Rejecting an
unknown decorator at check time is part of the row: a decorator nobody can interpret is a compile
fact, not a runtime surprise.

<a id="l12-6"></a>

### L12.6 — the call surface

`*args`, `**kwargs`, keyword-only parameters and `f(*xs)` / `f(**d)` all die in the parser, which
means every consumer behind it has never seen the shape — a good position to be in, because the
rule this loop already applies to parameters (ADR 0196: a parameter is a variable, decided by the
body) and to arity (ADR 0201's diagnostics) can be applied once, to a calling-convention table,
before four consumers have opinions. Packing targets are tuples, so L11.3 is the natural
dependency for the star half; the parameter-list half — `def f(a, *rest, k=1, **kw)` binding into
frame slots — has no such dependency and is where the item starts. Two decisions the ADR has to
make explicitly: what a packed parameter is when the callee mutates it (`args.append(x)` in
CPython raises `TypeError` for a tuple, and gusty's tuples are immutable by L11.3's definition),
and whether `f(**d)` requires `d` to be a `dict` or accepts any mapping.

<a id="l12-7"></a>

### L12.7 — statements are keywords

The measured shape is the interesting part: `assert x > 0, "…"` is accepted by the parser because
`assert` is an identifier, is refused by the checker as `undefined name "assert"`, and is a
runtime `NameError` under `--file`. One source line, two different events with two different exit
codes, and the weaker of the two is the one people hit. `del`, `nonlocal` and Gap R.48's `global`
share the shape; `...` as a body is the one that blocks a whole idiom (`Protocol`, abstract
methods, stubs), and `Ellipsis`/`…` needs the same ADR because it is both an expression and a
statement form. The fix is structural rather than four one-off statements: one keyword table
drives the lexer's reserved words, the parser's statement dispatch, the checker's name table and
the interpreter's statement evaluation, so a word that means a statement cannot simultaneously be
an identifier — and a statement no engine implements is reported where it is written, with a span,
the way ADR 0166 made "the built-in exists but this backend cannot lower it" a front-end event.

<a id="l12-8"></a>

### L12.8 — f-strings format

Interpolation works and prints CPython's answer, so the missing half is precise: the `!s`/`!r`/`!a`
conversions and the `:spec` field are discarded at parse time. That is the good news — nothing is
misimplemented, something is unimplemented — and it dictates the shape: parse them into the AST
(everything after that is a formatter), implement `str.format` against the same formatter, and let
`f"{x!r}"` reach the protocol renderer of L12.3 rather than a second copy of `str()`. Width and
precision formatting for floats has to wait for L11.6's float representation, because `f"{3.5:.2f}"`
is untestable while `3.5` and `3.50` are the same value; the alignment cases (`f"{x:>6}"`) are
width on the *rendered* string and can land earlier. The ADR decides the supported spec subset —
Python's mini-language is large and refusing the rest with a code is better than a silent
partial, which is exactly what the row is about.

<a id="l12-9"></a>

### L12.9 — match completes

Three of the four shapes are grammar: `case [first, *rest]:`, `case str() as s:`,
`case Point(x=0, y=0):`. The matcher behind them already handles list, dict and class patterns,
guards, or-patterns and (per Gap B) subclass attribute binding, so these are new nodes rather than
new machinery. The fourth is a Phase 11 signature: `case {"k": v}:` prints `7` interpreted and
`(null)` compiled, the value-read-as-interned-index defect arriving through a pattern binding, so
this item splits along the same seam the rest of the tracker uses — the parse half is free, the
print half waits for L11.1's tag. `as` in pattern position needs a checker decision the ADR should
record: the bound name's type is the narrowed type, not the subject's, which is the same rule as
L6.5's `isinstance` narrowing and should reuse it.

<a id="l12-10"></a>

### L12.10 — the module surface

`from math import sqrt`, `from math import sqrt as r` and `__name__` are missing, so the two lines
that begin most Python programs — the import and `if __name__ == "__main__":` — cannot be written.
Discovery is Gap R.52's half (the search path is cwd-relative); this row is the grammar and the
module's self-knowledge. `__name__` is small and load-bearing: a module needs an identity string
before it can answer `__name__`, and the entry-point idiom needs that before a file can be both a
library and a script. `from … import … as …` is a surface form of an existing resolution path, and
its ADR should settle the two questions the current `import` leaves open: whether imported names
are read-only bindings (a rebinding of `sqrt` should not leak into the module it came from) and
what `import a.b.c` means when the on-disk layout has no packages yet — refusing packages by name
is acceptable, inventing a silent fallback is not.

<a id="l12-11"></a>

### L12.11 — receiver tables

The measurement that decides the design is a message: `xs.insert(0, 0)` on a **list** produces
`codegen: string method insert on non-constant string`. The compiled backend resolves a method
name against the string table whenever it cannot see the receiver's kind, so the diagnostic
describes a value the program never had — Gap R.38's rule ("never assert something about another
backend or an operand kind from a template") broken structurally rather than by a pasted sentence,
and fixable only by making dispatch take the receiver kind as a key. Beside it sits the built-in
list the survey produced: `map`, `filter`, `getattr`, `setattr`, `hasattr`, `repr`, `hash`, `id`,
`vars`, `dir`, `list.sort/index/count/remove`, `dict.setdefault/pop`, `str.format`, `frozenset`,
`tuple(...)`, bytes literals, and the set operators `&`, `-`, `|` (which do not parse at all). The
mechanism is `predeclared.go`'s single-source rule, generalised: one table per receiver kind,
consulted by checker, interpreter and codegen, so a name from the wrong table is an impossibility
rather than a message, and the Gap R.51 class of defect — checker predeclares what the interpreter
lacks — cannot recur for a method either.

<a id="l12-12"></a>

### L12.12 — integers are integers, or the language says so

`2 ** 63` is `-9223372036854775808` interpreted, `0` compiled, `9223372036854775808` in CPython;
`10 ** 19` disagrees between the two backends by 33 orders of magnitude of significance. Neither
leg is a plausible user-visible `int`, and neither says anything. This row is an ADR before it is
an implementation, and the ADR has to choose one of three positions:

• **arbitrary precision** — Python's answer, and the honest one for a dynamic language, but a real
  cost: a value kind that does not fit L11.1's word, an allocator in the hot path, and a
  `--gc-stats` story;

• **bounded `int` with an overflow trap** — cheaper, defensible for an AOT-first language, and
  already half the truth about the compiled backend, *provided* the boundary is documented and the
  overflow raises (a `OverflowError`-class trap, exit 3, the class CPython uses) instead of
  wrapping. Width becomes part of the ABI story (L10.2) rather than an accident of a fold;

• **the status quo** — rejected: it is not a design, it is two bugs with a documentation gap
  between them, and the parity matrix cannot see it because the legs disagree.

The interim requirement is only that the compiler stop answering: an expression whose constant
result does not fit the width it is folding at should refuse with a code (Gap R.37's rule, that a
constant fold may only resolve what the program cannot avoid executing).

<a id="l12-13"></a>

### L12.13 — the surface manifest is the spec

`gustyc --lang` never over-claims — no phantom construct in its list — but it under-claims badly
enough to be useless to its second audience: `with` ships on both backends and is documented and
is not in the list; `async def`, `yield`, `break`, `continue`, `in`, `is`, `**`, the ternary, the
walrus and f-strings are all absent from it while running today. AGENTS.md makes a machine path
part of every feature, and the manifest is where that contract lives for a language surface: an
agent planning a program should read one document, in JSON, that says per construct and per backend
whether it is `supported`, `refused(code)`, or `absent`. Implementation is generation, not editing:
the lexer's keyword table, the parser's statement dispatch, `predeclared.go`, the descriptor table
of L12.5, the receiver tables of L12.11 and codegen's refusal catalogue are the sources, and the
three-engine census is the check that the generated document matches behaviour. The test that makes
it stick is the diff direction that currently has no witness: a construct that ships without being
listed fails, as does a listed construct that the oracle says is wrong.

<a id="gap-r-53"></a>

### Gap R.53 — comparison chains answer the wrong value on both backends (found by the 2026-10-01 surface survey)

```
x = 5
if 1 < x < 3:      # CPython: out   ·   --interp: in   ·   --aot: in
    print("in")
else:
    print("out")
print(1 < 2 < 3)   # True — right for the wrong reason
print(10 < x < 20) # CPython: False ·   both backends: 1
```

The chain is left-associative, so `1 < x < 3` is `(1 < x) < 3`: an `int` compared against a
boolean, which gusty answers rather than refusing. Note which test would not have caught it:
`print(1 < 2 < 3)` prints `True` — the accidental pass. Owner L12.1; the AST-level fix and why a
`and` desugaring was rejected are in [L12.1](#l12-1).

<a id="gap-r-54"></a>

### Gap R.54 — `for` over `__iter__`/`__next__` spins forever interpreted and iterates nothing compiled (found by the 2026-10-01 surface survey)

```
class R:
    def __init__(self): self.i = 0
    def __iter__(self): return self
    def __next__(self):
        self.i = self.i + 1
        if self.i > 2: raise StopIteration
        return self.i
for x in R():
    print(x)
print("after")        # CPython: 1 2 after
```

The interpreter produced **7.3 million lines in 15 s** before the harness killed it: `__next__` is
called, the `StopIteration` arrives, and nothing reads it as the end of iteration. The compiled
binary printed `after` and exited **0** — an iterable with elements answered "empty". This is the
phase's signature pair: two engines, two different wrong answers, and the only shape in the survey
that can hang a CI job rather than fail it. Owner L12.2.

<a id="gap-r-55"></a>

### Gap R.55 — `print` ignores `__str__` / `__repr__` (found by the 2026-10-01 surface survey)

```
class C:
    def __str__(self):  return "S"
    def __repr__(self): return "R"
print(C())             # CPython: S · --interp: <instance> · --aot: 0
print(repr(C()))       # NameError: name 'repr' is not defined
```

The compiled `0` is an interned-string index going to `%d` — Gap R.22's mechanism, new location.
The interpreter's `<instance>` is a placeholder, which makes it *safer* than the compiled answer
and no more correct. `repr` being absent from the built-in table is the deeper half: containers,
tracebacks and debuggers all render through a name the language does not have. Owner L12.3, on
top of L11.2's `str`/`repr` pair.

<a id="gap-r-56"></a>

### Gap R.56 — a class body binds no attributes (found by the 2026-10-01 surface survey)

```
class C:
    X = 1
print(C.X)             # CPython: 1
                       # --interp: AttributeError: type object 'C' has no attribute 'X'
                       # --aot:    codegen: unsupported attr expression
```

Only methods reach the class object. The consequence is not one lost feature but a lost *idiom*:
class constants, counters, registries, `@dataclass`'s field table and `class Color(Enum): RED = 1`
are all "bindings in a class body", and none of them can be written. Cheapest structural fix in
the tracker — one missing write — and the unlock is disproportionate. Owner L12.4.

<a id="gap-r-57"></a>

### Gap R.57 — `@property` yields the method object; `@staticmethod` / `@classmethod` do not compile (found by the 2026-10-01 surface survey)

```
class T:
    def __init__(self, v): self._v = v
    @property
    def d(self): return self._v * 2
print(T(3).d)                        # CPython: 6 · --interp: <method> · --aot: refusal

class S:
    @staticmethod
    def s(): return 7
    @classmethod
    def c(cls): return 8
print(S.s(), S.c())                  # CPython: 7 8 · --interp: 7 8 · --aot: 2 errors
```

The decorator call itself runs, so `@property` succeeds *and does nothing*: the attribute stays a
method object and printing it is a silent wrong answer. `staticmethod` / `classmethod` are
interpreter-only, giving the compiled backend two refusals for the ordinary way to write a utility
and a factory. Owner L12.5.

<a id="gap-r-58"></a>

### Gap R.58 — `*args`, `**kwargs`, keyword-only parameters and call unpacking do not parse (found by the 2026-10-01 surface survey)

```
def f(a, *rest, k=1, **kw): ...      # parse error at 1:10: expected identifier
def g(a, b=2, *, c=3): ...           # parse error at 1:15: expected identifier
g(*[1, 2])                           # parse error at 4:9: unexpected token
g(**{"a": 1, "b": 2})                # parse error: unexpected token
```

CPython runs all four. Keyword arguments at a *call* work (Gap J.1 pinned `sep`/`end` long ago), so
what is missing is the star side of the parameter list and the call. Because the failure is in the
parser, no consumer behind it has an opinion yet — the moment to write the calling-convention table
once. Owner L12.6; L11.3 supplies the tuple the star forms pack into.

<a id="gap-r-59"></a>

### Gap R.59 — `assert`, `del`, `nonlocal` are identifiers; `...` is not a body (found by the 2026-10-01 surface survey)

```
assert x > 0, "must be positive"   # --check: error at 1:1: undefined name "assert"
                                   # --file:  NameError: name 'assert' is not defined, exit 3
del d["a"]                         # NameError: name 'del' is not defined
nonlocal n                         # same shape, inside a nested def
def f():
    ...                            # parse error at 2:5: unexpected token
```

The checker catches the first three and the runner does not, so the same source has a compile-time
meaning and a runtime meaning with different exit codes. `global` is Gap R.48 and the same defect.
The `...` case is the one that costs users most, because it is how a `Protocol`, an abstract method
and a stub are written. Owner L12.7, with the keyword-table rule.

<a id="gap-r-60"></a>

### Gap R.60 — f-string conversions and format specs are dropped; `str.format` is absent (found by the 2026-10-01 surface survey)

```
x = 3.5
print(f"{x:.2f}")                   # CPython: 3.50 · both backends: 3.5
print(f"{x!r}")                     # CPython: repr(x) · both backends: str(x)
print(f"{x:>6}")                    # CPython: '    3.5' · both backends: 3.5
print("{},{}".format("x", "y"))     # no such string method
```

`f"hello {name} {1 + 2}"` is correct on all three engines, which localises the defect precisely:
the `!conversion` and `:spec` fields are parsed away on the way to an AST that has nowhere to put
them. Printing a narrower rendering than the program asked for is a wrong answer that looks
reasonable, which is the category only the oracle leg finds. Owner L12.8; the float half waits for
L11.6, the alignment half need not.

<a id="gap-r-61"></a>

### Gap R.61 — three pattern forms do not parse, and the dict pattern that does prints `(null)` (found by the 2026-10-01 surface survey)

```
case [first, *rest]:          # parse error at 3:22: unexpected token
case str() as s:              # parse error at 3:20: expected ':'
case Point(x=0, y=0):         # parse error at 7:21: expected ')' in class pattern

def f(d):
    match d:
        case {"k": v}: return v
print(f({"k": 7}))            # CPython: 7 · --interp: 7 · --aot: (null)
```

Three grammar gaps in front of a matcher that already lowers list, dict and class patterns, plus
one more instance of the untagged-value signature (Gap R.22) arriving through a pattern binding.
Owner L12.9, which splits along that seam; the alias half of class patterns is Gap B.

<a id="gap-r-62"></a>

### Gap R.62 — no `from … import … [as …]`, and no `__name__` (found by the 2026-10-01 surface survey)

```
from math import sqrt          # parse error at 1:1: unexpected token
from math import sqrt as r     # parse error at 1:1: unexpected token
print(__name__)                # NameError: name '__name__' is not defined
if __name__ == "__main__":     # therefore unwritable
    main()
```

`import math` resolves and `math.PI` folds, but `math.sqrt(16)` answers `no name sqrt in module`
because the bundled stdlib is data-only — that half is the stdlib story, and Gap R.52 is the search
path. This row is the grammar plus the module's identity, and it is what makes a file a program
rather than a snippet. Owner L12.10.

<a id="gap-r-63"></a>

### Gap R.63 — a list or dict method is answered from the string-method table; the built-in list is short (found by the 2026-10-01 surface survey)

```
xs = [1, 2]
xs.insert(0, 0)     # --interp: no such list method   · --aot: codegen: string method insert on non-constant string
xs.index(3)         # --aot: string method index on non-constant string
d.setdefault("b", 2)  # --aot: string method setdefault on non-constant string
map(f, xs) · filter(f, xs) · getattr(o, "v") · repr(x) · hash(x) · id(x) · vars(o) · dir(o)
frozenset([1, 2]) · tuple([1, 2]) · b"abc"
{1, 2} & {2, 3}     # parse error
```

The messages are the finding: the compiled backend asks the *string* table for a method name when it
cannot see the receiver's kind, so a list is described as a string. Gap R.38 established that a
refusal may not assert something false about another backend or an operand kind; this is the same
rule broken one level down, where the assertion comes from a lookup table rather than a template.
The missing-name list is the second half, and both halves want the same instrument: one table per
receiver kind, consulted by checker, interpreter and codegen. Owner L12.11.

<a id="gap-r-64"></a>

### Gap R.64 — `int` overflows silently, and the two backends overflow differently (found by the 2026-10-01 surface survey)

```
print(2 ** 62)    # 4611686018427387904 — exact on both
print(2 ** 63)    # CPython 9223372036854775808 · --interp -9223372036854775808 · --aot 0
print(10 ** 19)   # CPython 10000000000000000000 · --interp -8446744073709551616 · --aot -1981284352
print(10 ** 30)   # CPython 1000000000000000000000000000 · --interp 5076944270305263616 · --aot 1073741824
```

Three answers, none CPython's, none reported. The compiled path folds through `i32` where the
interpreter keeps `i64`, so the disagreement is width *and* representation, and parity is blind to
it by construction. The fix starts with an ADR that picks a position — bignums, or a documented
bounded `int` that traps on overflow — and the interim rule is Gap R.37's: a constant fold may only
resolve a value the program can actually hold, otherwise refuse by code. Owner L12.12.

<a id="gap-r-65"></a>

### Gap R.65 — the surface manifest under-reports what ships (found by the 2026-10-01 surface survey)

`gustyc --lang` reports `statements: assign, print, if/elif/else, while, for-in-range, def/return,
pass, match, try/except/finally, raise, class, import`. It omits `with` — shipped on both backends,
documented in `docs/language.md`, and verified in this survey — plus `async def`, `yield`, `yield
from`, `break`, `continue`; and its expression line omits `in`, `is`, `**`, the ternary, the walrus
and f-strings, all of which run. The failure is in the harmless direction (it never advertises a
construct that does not exist) which is exactly why it persists. AGENTS.md makes self-description
part of a feature, and for a language surface the manifest *is* that part; the fix is generation
from the implementation's own tables plus the census as the witness. Owner L12.13.

<a id="gap-r-66"></a>

### Gap R.66 — the interpreter resolves an inherited attribute through the first base only (found by the 2026-10-01 surface survey)

```
class A:
    def a(self): return "a"
class B:
    def b(self): return "b"
class C(A, B):
    pass
print(C().a())   # a — every engine
print(C().b())   # CPython: b · --aot: b · --interp: AttributeError: 'C' has no attribute 'b'
```

The inverted one: the compiled backend walks every base and is right, the interpreter walks the
first and reports an inherited method as missing. Two MRO implementations, differing in
correctness, is the argument for computing the linearisation once and having both legs consult it —
the same table that resolves the class attributes of Gap R.56. Owner L12.4.

<a id="gap-r-67"></a>

### Gap R.67 — a container returned from a function is not a value in the compiled backend (found 2026-10-02 while closing Gap J.2)

```
def f():
    return [1, 2]
print(f())          # --interp: [1, 2] · CPython: [1, 2] · --aot: exit 2, ret i32 @.lst1

def g():
    la = [1, 2]
    return la
print(g())          # --interp: [1, 2] · CPython: [1, 2] · --aot: 0
```

Two answers, both wrong in a different way, and only one of them is even labelled a compiler bug.
`return <container literal>` writes the fold's global straight into the return slot —
`ret i32 @.lst1`, `ret i32 @.set1`, `ret i32 @.dict1` — which `llc` refuses, and the exit-code
contract (ADR 0211) correctly calls that exit 2, a bug of ours, for a program CPython runs. Binding
first is worse precisely because nothing complains: the handle is a small `i32`, the caller prints
it as the number it is, and `0` comes out where `[1, 2]` belongs.

It is Gap J.2's operand question one statement further out. A returned container needs the two
things ADR 0163 gives a *binding*: a heap object built at the `return` (so the value is a handle),
and a recorded return kind (so `print(f())` reaches `rt_print_list` / `rt_set_print` /
`rt_dict_print` instead of `printf("%d")`). The classification layer that already answers "what does
this call return?" for strings has no container case — the same shape of miss ADR 0230 recorded for
`return s[i:j]`, where a correct slice printed `1` for want of a return-kind case.

**Why it is owed and not fixed here:** it affects plain literals, so the comprehension binding did
not create it — closing Gap J.2 removed the refusal that used to stand in front of it, which is how
it got measured. One commit per feature (AGENTS.md), and the fix belongs with L11.1: a tagged value
word makes the handle *and* its kind travel together, which is what removes both halves at once.
Until then the shapes above stay untested and unfixed, and this row is where they are recorded.
<a id="original-preamble"></a>

## Original preamble, snapshot, component map and sequencing note (verbatim)

Superseded in the tracker by the Snapshot, Component map and Sequencing sections — kept here
so the pre-tabulation wording survives in one place with the rest of the record. The status
numbers below are as-written at the time (see the tracker's Snapshot for the measured ones).

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
harness) is ✅ DONE (ADR 0186), and L11.1 has now taken its four steps for lists, dicts and
sets (ADR 0182 tags, ADR 0184/0185/0187/0189 slots and reads, ADR 0232 mixed dicts/sets plus
the tagged lookup that made them safe), so the queue is the 22 pinned `debt` rows: L11.1's
remaining steps ((1c) floats in containers, (1d) retiring `@estr[h]`, and the tagged word
itself), L11.2 (bools as values), L11.3 (tuples), L11.4 (indexing), L11.5
(code-point strings), L11.6 (numerics), L11.7 (functions as values), then L11.8 (refusals and
exit codes). L7.2/L7.3/L8.1/L8.4 all assume L11.1. The still-open gap-shaped items (Gap J.2,
Gap K.8 part 2 — full AOT tracebacks, Gap M.2 — flipping `--file` to the compiled backend,
Gap L.5 — print atomicity, Gaps N.2, P.1, P.2) are absorbed by Phase 11 where they are
representation decisions, and stay their own work where they are not (K.8's prerequisite
landed with L8.5's line tables (ADR 0231) — what is left there is the frame stack, not the
metadata; M.2 flips only once the corpus is green through the compiled leg — which, since
L11.9, is a measured claim rather than an assumption).

<a id="gap-r-69"></a>

### Gap R.69 — `round(x, ndigits)` is three behaviours, one of them the wrong exit code (✅ CLOSED by ADR 0263, 2026-10-03; found 2026-10-02 while closing Gap R.50)

```
print(round(2.345, 2))   # CPython 2.35 | --interp was 2 | --aot was exit 1: round expects one argument
print(round(3.5, 0))     # CPython 4.0  | --interp was 4 | --aot was exit 1: round expects one argument
```

Three engines, three behaviours, all of them wrong in a different register:

- **The interpreter ignored `ndigits` and returned an integer.** `round(2.345, 2)` gave `2` — not
  `2.35`, and not a float. A caller that did `x * 100` afterwards got an answer off by the whole
  fractional part, with nothing said.
- **The compiled backend refused the call**, which is defensible as a refusal and indefensible as
  emitted: `gustyc: jit: codegen: round expects one argument` came back with **exit 1**, the contract's
  "your program has a compile error" class (ADR 0211), for a program CPython runs. That is L11.8's and
  Gap R.38's subject exactly — a refusal claiming something false about the program.
- **CPython returns a float**, which is the part the row existed to pin: `round(3.5, 0)` is `4.0`, not
  `4`. In a language whose float/int distinction reaches `print` (`4.0` vs `4`), getting the digits
  right and the type wrong is still a wrong answer.

**The row's own suggested fix was measured and rejected.** It proposed ADR 0236's shape one step out —
`s = 10**n; roundeven(x*s)/s` — and that algorithm is wrong, not approximate. The failure is not that
ties are subtle; it is that **scaling manufactures ties the value never had**:

| program | the double's exact value | ×100 | CPython | scale · `roundeven` · unscale |
|---|---|---|---|---|
| `round(0.005, 2)` | 0.00500000000000000010408… | `0.5` | `0.01` | `0.0` |
| `round(0.025, 2)` | 0.0250000000000000013877… | `2.5` | `0.03` | `0.02` |
| `round(0.075, 2)` | 0.0749999999999999972244… | `7.5` | `0.07` | `0.08` |
| `round(2.675, 2)` | 2.67499999999999982236… | `267.5` | `2.67` | `2.68` |
| `round(-0.005, 2)` | −0.00500000000000000010408… | `-0.5` | `-0.01` | `-0.0` |
| `round(2.345, 2)` | 2.34500000000000019539… | 234.50000000000003 | `2.35` | `2.35` — right by luck |

The last row is the one that should worry anyone: the roadmap's own definition-of-done program prints
CPython's `2.35` under **both** algorithms, because that particular double happens to sit above its tie,
so the scaled product lands past it and even-rounding reaches the same answer for reasons the question
does not contain. A DoD written as one example is not a test of a rule. Across the sweeps the row's rule
differs from CPython on **1,077 of 375,224** fractional pairs and **74,838 of 156,048** negative-digit
ones (the last figure including every pair at a digit count where the scale overflows to +Inf and the
rule answers NaN).

What the reference rounds is the **exact decimal value of the double**, to `ndigits` places, ties to even,
and the answer is the nearest double to that decimal (David Gay's dtoa, tie broken against the true value).
That is a correctly-rounded double→decimal conversion — a different operation from `roundeven`, and one
neither backend owns. So neither implements it. `pkg/lang/round_digits.go`'s `roundToDigits` is the one rule,
and its decisive step is borrowed per platform:

```
interpreter   strconv.FormatFloat(v, 'f', n, 64) → strconv.ParseFloat     (Go's dtoa)
compiled      snprintf("%.*f", n, v)             → strtod                 (the C library's, in rt_round_digits)
```

Neither library is trusted on sight. **531,272 `(value, ndigits)` pairs** were swept against CPython and
compared bit for bit: every `k/1000` and `k/100` in the tested range, 22,500 uniform randoms, 500 raw bit
patterns, `ndigits` −2..10 — 375,224 pairs, **zero** differences for either spelling — then the same shapes
with `ndigits` −400..400 for the negative branch, 156,048 more. The 143 that remain are all between
|x| = 4.117e18 and 1e300, all exactly one ULP, are the scale's own inexactness rather than the conversion's,
and are recorded below rather than hidden behind a passing test.

The four pieces under the round trip, and why each is shaped as it is:

* **negative digit counts** divide to bring the round point to the units place, ask the same conversion with
  no fractional digits, and scale back. `math.RoundToEven(v/scale)*scale` — the row's idea, again — was built
  first: 74,838 of those 156,048 pairs away from the reference, where the `%.0f`+`strtod` shape that shipped
  is 143.
* **`pow10` is ten multiplied by itself k times**, in Go and as a `phi` loop in the emitted runtime, so the two
  backends round the same products instead of one calling libm `pow` and the other squaring. Every power up to
  10²² is exact; below that the agreement comes from walking the same sequence, not from agreeing on a formula.
* **the clamps are arithmetic facts.** Every binary double is exactly a decimal with at most 324 digits after
  the point (the smallest subnormal is 4.94e-324), so `n ≥ 324` is the identity — and the rendered text stays
  inside the runtime's buffer. `10^309` is past the largest finite double, so `n ≤ −309` asks the nearest
  multiple of a scale no double reaches: zero, **with the sign taken from the value's own bits**, because an
  `fcmp` cannot tell −0.0 from 0.0. CPython answers the same at both clamps.
* **the answer is the kind the value arrived as**: `round(5, 2)` is the int `5`, `round(True, 2)` is `1`
  (ADR 0257), `round(5.0, 2)` is `5.0`, `round(3.5, 0)` is `4.0`. The digit count is evaluated even when the
  value is an int, because `round(5, 1.5)` is a `TypeError` in the reference and not a free pass.

Two exit-code decisions, both ADR 0166's and neither obvious:

- a digit count that is not an integer (`round(2.345, 1.5)`, `round(2.345, "2")`) is a program CPython *runs
  and stops on*, so both engines **raise** its `TypeError: '<kind>' object cannot be interpreted as an
  integer` through the same catchable door an out-of-range index uses — `except TypeError` works on all three
  engines, exit 3 — rather than refusing to build the program at exit 1. Truncating the digit count instead
  would be this row's original mistake wearing a different hat, so the compiled fold checks the count *before*
  it folds anything.
- `round()` and `round(x, 1, 2)` are typos in the program: one shared sentence (`roundArityMessage`), exit 1
  compiled and exit 3 interpreted, **exit 2 nowhere**. Until this cycle `round()` was `n.Args[0]` in the
  evaluator — a Go panic, `index out of range [0] with length 0`, exit 2 spent on a user's mistake.

And one small trap in the fold: the first version emitted `%t = fadd double 0.0, <const>` to get a name for
the constant, which is where `round(-0.5, 0)` lost the sign of its zero. A constant is a value; it travels as
itself. The *literal* spelling of that sign question is still broken and is Gap R.132 below.

What is left is not the rounding. `round(x, ndigits)` follows the kind of `x`, and the compiled backend can
only follow a kind the module can see — which is why Gaps R.129 and R.130 are filed with each engine's number
pinned instead of refused: retiring `def f(x): return round(x, 2)` would trade a working int-call-site program
for an error message. `int()`, `float()`, `ord()` and `chr()` have the arity panic this cycle fixed for
`round`, and are Gap R.131's.

`integration/programs/round_ndigits.gy` is the row's own definition of done and is in the corpus as a
standalone row — `oracle: match`, parity yes, `2.35` and `4.0` on all three engines. The rule is tested four
ways: `pkg/lang/round_digits_test.go` (a parity table of every shape the rule can name × both engines against
CPython, an IR row that the conversion is asked for exactly once and is not carried by programs that never
round, the trap half including its catchability, the arity half, and `roundToDigits` itself with the swept
values and its two clamps), `integration/round_digits_test.go` (the same through the shipped CLI against
`python3`, with the exit classes pinned and exit 2 failing the file), and the filed-not-fixed tables that keep
the boundary of the feature in the suite rather than in a comment.


### Gap R.70 — `for` over a set or dict literal reached the range path, and its bound was the container's global (found 2026-10-02 while closing the nested half of L11.1)

```
for v in {1, 2}:
    print(v)          # --aot: exit 2 — %t1 = icmp slt i32 %_ctr1.ld1, @.set1
for k in {"a": 1, "b": 2}:
    print(k)          # --aot: exit 2; had it run, the loop variable was the counter
```

Both legs refuse today, and the message is `llc: exit status 1` — the exit-code contract's *compiler's
fault* class (ADR 0166), issued for two lines that CPython runs in one go.

The `for` lowering asks whether the iterable is a container it knows, and that question was written for
variables: `g.mixedLists`, `g.listVars`, `g.runtimeSets`, `g.mixedDicts`, a generator call. A literal
answered none of them, fell past the runtime container loop, and landed in `rangeBounds`, whose
catch-all is `0 .. g.value(iterable)`. For a set literal `g.value` answers the folded static global
`@.set1`, whose layout is `{i32, [N x i32]}` — a *global in an i32 slot*, the shape this loop has
already been forced to make impossible four times over (ADR 0188 for print, ADR 0192 for
comprehensions, ADR 0226 for element writes, ADR 0238 for the float box). Even had the operand
compiled, the loop variable was bound to the counter, so the loop would have printed `0, 1` where
CPython prints `1, 2`.

The fix is to ask the same question of a literal that the variable leg already asks: build the object,
take its handle, walk `0 .. rt_set_len(h)` (or `2*i` for a dict's keys), and bind the loop variable to
the element. Two details were worth as much as the fix itself:

- **The loop variable of a dict literal must be recorded as interned text** when every key is text.
  The variable leg already did that from `g.dictKeyStr`; without it `for k in {"a": 1}` printed the
  interned index `0`, which is ADR 0229's rendering rule arriving by a different road.
- **A generator call is not a container**, at least not to this rule: its own lowering already
  produced the handle, and sending every iterable through the container builder turned
  `for y in g(3)` into `*lang.Call is not a container` — the compiled leg losing a shape it used to
  answer, caught by `TestEveryGeneratedModuleVerifies` rather than by review.

`integration/for_container_literal_test.go` pins both engines against CPython for set, dict and mixed
literals, and compares a mixed set's members as a multiset, because CPython's set order is hash order
and a test must not pin an implementation detail. The generator regression is pinned beside it.

### Gap R.71 — the numeric folds stopped asking their elements what they are (measured 2026-10-02 while closing the nested half of L11.1)

```
x = 3
print(max([x, 2.5]))   # CPython 3 | --interp 2.5 | --aot 3.0
```

Three engines, three answers, and the one that looks closest (`3.0`) is the compiled leg — a float-typed
answer to a question whose answer CPython keeps as an integer, because `max` returns *the element it
chose*, not a number of whichever type appeared in the list.

The static half of this is settled by ADR 0239: `max([1, 2.5])` is `2.5`, `min([2.5, 1])` is `1`,
`sum([1, 2.5])` is `3.5`, on both backends and the oracle. Those all fold, and the fold now picks a
winner and returns that winner's own type.

What is left is an element the compiler cannot see. There is no static winner, so the compiled leg
falls back to lifting an i32 through `sitofp` and prints `3.0`; the interpreter adds or compares raw
handles, which is how `sum([1.5, 2.5])` once printed `562949953421319` — the bits of a float box read
as an integer (that particular case is fixed, and pinned in `pkg/lang/numeric_fold_test.go`). A
reduction over runtime values is not a fold: it needs a loop, a comparison that asks each element's
kind, and a result that remembers the winner's type. That belongs with the float work (L11.6) that
owns the rest of the numeric surface, not with the container work that surfaced it.

### Gap R.72 — one line of source, three verdicts: `;` is a diagnostic some entry points enforce and one ignores (measured 2026-10-02 while measuring the nested half of L11.1)

```gy
x = 5; print(x+1)
```

| entry point | verdict |
|---|---|
| `--interp --file` / `--eval` / `--repl` | `6` — CPython's answer |
| `--jit` / `--aot` | exit 1, `jit: 1 error(s) in source` + `error at 1:6: unexpected character ";"` |
| `--emit-llvm` | exit 0, no diagnostic, and `llc-20 -filetype=null` accepts the module |

The lexer has no `;`: it records `unexpected character ";"` as a diagnostic and carries on, so both
statements parse and `GenerateIR` emits a module that works. What differs is who asks.
`JITWithOptions` refuses on any error-level diagnostic; `Compile` collects diagnostics into its result
and returns the IR anyway; the interpreter never looks. Same program, three stories — which is exactly
what an agent comparing backends will read as a backend bug, and burn a round on.

ADR 0240 fixed half of the pain: the refusal now says `unexpected character ";"` at `1:6` instead of
reporting a bare count, so the disagreement is legible. What it did not do is decide the question,
because the decision has two legitimate answers and they are not the same work:

- **`;` is a statement separator** (CPython agrees: it terminates a simple statement, and `gusty`
  source files already accept `x = 5; print(x+1)` on the interpreter). Then the lexer should emit it
  as such, the diagnostic disappears, and every path says `6`.
- **`;` is not in the language.** Then the lexer error must be fatal on *every* path, including
  `--interp` and `--emit-llvm`, and the interpreter's tolerance is the bug.

Either way the rule is ADR 0166's, applied to the CLI instead of to codegen: a program gets one
verdict, and the paths that report it report the same one. Definition of done: a table test that runs
one `;`-separated program and one `;`-free control through all five entry points and asserts the same
exit code and the same stdout, plus a line in `docs/language.md` saying which answer was chosen.

### L11.1 step 3 — the tagged element read: a slot is read back by the tag the builder wrote (measured 2026-10-02, closed the same cycle, ADR 0241)

ADR 0239 left L11.1's first row with one clause open, and it was the half that a program actually
asks for. Build a nested container and print it: both backends agree. Reach into it and use what comes
back: the interpreter answered, the compiled backend refused.

```gy
xs = [[1, 2], [3, 4]]
print(len(xs[0]))      # 2          — AOT refused: "len requires an inline list/dict/set literal"
print(xs[0][1])        # 2          — AOT refused: "index requires an inline …"
d = {"a": [1, 2]}
print(d["a"][1])       # 2          — AOT refused
m = {0: [1, 2], 1: 3}
print(m[0][1])         # 2          — AOT refused
t = [[[1]]]
print(t[0][0][0])      # 1          — AOT refused
print(1 if xs[0] == [1, 2] else 0)   # 1 — AOT refused
print(1 if 2 in xs[0] else 0)        # 1 — AOT refused
for v in xs[0]: print(v)             # 1, 2 — AOT refused
```

Nine rows, all measured against CPython first, all answered by the interpreter, and every refusal the
same sentence pointing at a shape in the source that was fine. The shape was fine; what was missing was
a rule for *who may believe* that a slot holds a container.

The rule ADR 0233 already gave for writes — a slot is only writeable when the compiler can name the kind
— has a mirror for reads, and it is a compile-time promise rather than a runtime guess.
`containerLiteralsOf` (`pkg/lang/container_env.go`) records the names bound exactly once to a container
literal, and removes them again the moment the object could have changed: a rebind, an `xs[0] = …`, an
`append`/`sort`/`add`/`update`/`pop`, or the container passed to a callee this pass cannot see. A name
still in the map licenses reading its slots, because the tag the builder wrote is the tag the object
holds. Everything else declines.

`rt_container_slot` — a runtime helper that would have taken any payload and asked `@heap` whether the
object at that index is a container — was written, tested, and deleted. That question is answerable and
worthless: the payload of an `int` slot is a number, `@heap` has 1024 entries, so *some* object lives at
that index and would be printed. `[[5]]` where the program wrote `[[1, 2]]`, or the interned text at
`@str_tab[5]`, is the class of wrong answer ADR 0233, ADR 0238 and Gap R.65/R.66 all exist to keep out.
The static promise buys refusals; the runtime guess would have cost trust.

Three messages, chosen by which question actually failed, so the reader is not sent after the wrong
thing:

| situation | refusal |
|---|---|
| the container was mutated or handed off | `len cannot reach into xs's slots: the name was rebound, mutated, or handed to code this pass cannot see …` |
| the read is licensed, the context wants a bare `i32` | `index cannot use an element of xs as a plain number: the payload only means something with its tag …` |
| the program is subscripting a number | `len reaches past a int in xs: the slot holds a int, not a container …` (CPython: `TypeError: 'int' object is not subscriptable`) |

Two things stay open on the row, both named by the message that produces them: the **numeric use** of an
element (`xs[0] + 1`, `max(xs[0])`) — the tagged value word in its arithmetic form — and a container
**built at run time** (`xs = []; xs.append([7, 8]); xs[0][0]`), which no literal ever described. The
second is why `probe_nested_list` stays in the oracle ledger after `probe_heterogeneous` was promoted out
of it.

**Resolved 2026-10-02 (ADR 0242): `;` is a statement separator.** The row above listed two legitimate
endings and this cycle chose the second one's opposite — accept the separator, because the parser already
understood the statements on either side and forbidding it would have bought truth with less language.

What the change came down to, beyond a lexer case:

- **The token has to stay in the stream.** The old error token was dropped by `filterLex`, so the parser
  saw `for i in [1,2]: print(i) print("step")` and put the second statement *after* the loop: engines
  agreed on `1 2 step` where CPython prints `1 step 2 step`. An inline suite is a list of simple
  statements, so `TokSemi` is kept and `parseBlock`'s single-line branch loops over the separated
  statements, stopping at the NEWLINE that ends the physical line. That wrong answer is the reason the
  first attempt at this change is in the test table (`inline_for_body_runs_per_iteration`).
- **The rule belongs to the parser, not to the diagnostic channel.** `x = 1;;y = 2` (the empty statement,
  which CPython also rejects) is a `ParseError`, asserted in the same table to be rejected by the
  interpreter path too. Had it stayed a lexer diagnostic, the interpreter would have run the program the
  JIT refused — the very disagreement this row is about, recreated one level down.
- **Separators and blank lines are skipped together.** `x = 5;` ends with a separator followed by the
  line's NEWLINE; a loop that skipped only one kind handed `parseStmt` a NEWLINE to parse as a statement.
  `skipSeparators` loops over both, and `skipNewlinesAt` (the incremental parse cache's boundary helper)
  skips both, so statement boundaries stay aligned across an edit between two separated statements.
- **Indentation is unchanged** — the separator is emitted from the line loop, so INDENT/DEDENT still come
  one pair per physical line; a dedicated regression test covers the body-with-separator shape.

`x = 5; print(x+1)` now prints `6` under `--interp`, `--eval`, `--repl`, `--jit`, `--aot` and
`--emit-llvm` alike. ADR 0240's message stays: a program with a real syntax error is still refused, with
its message, on every path.

### L11.1 step 4 — the numeric use of an element: a slot the compiler can see holding a number is compiled as that number (measured 2026-10-02, closed the same cycle, ADR 0243)

ADR 0241 gave a slot read its tag, and with it every context that *carries* one: print, `==`, `in`,
`len`, a further subscript, `for`, a binding. What remained was the context that wants a **single word** —
arithmetic and comparison — and there both backends still refused programs CPython answers without
hesitation:

| program | CPython | `--interp` | `--aot` (before) |
|---|---|---|---|
| `xs = [1, "a"]; print(xs[0] + 1)` | `2` | `2` | refused |
| `xs = [1.5, "a"]; print(xs[0] + 1)` | `2.5` | `2.5` | refused |
| `xs = [1, "a"]; print(1 if xs[0] > 2 else 0)` | `0` | `0` | refused |
| `xs = [1.5, 2]; print(xs[0] + xs[1])` | `3.5` | `3.5` | refused |
| `t = [[1, 2]]; print(t[0][0] + 1)` | `2` | `2` | refused |
| `xs = [1, "a"]; y = xs[0] + 1; print(y)` | `2` | `2` | refused |

The refusal said *"this context needs a single static kind"*. That is a true sentence about the slot and
the wrong diagnosis for the program: the context needs the **value**, and when the container is one the
program spelled out and never changed, the value is already known at compile time.

**The decision: compile the element, not the slot.** Where ADR 0241's promise holds (the name was bound
once to a container literal and nothing mutated it or took it past an unseen callee) *and* the element is
a numeric literal, the numeric use compiles the element expression. `xs[0] + 1` becomes an addition on
`1`; `xs = [1.5, "a"]` takes the double path because `isFloat`, `floatValue` and `floatEval` ask the same
question of the slot and answer from the element. Three refusal sites and three float hooks consult one
helper (`staticNumericElem` via `numericElemUse`); no new value representation, no dispatch opcode, no
runtime helper, and so nothing new that can verify badly.

Why not ship `rt_num_add(payload, tag, payload, tag, i32* tagOut)` now: it is the general answer, but it
buys exactly the programs above while adding a second arithmetic engine beside the int and double paths
(rounding, `%`/`//` sign rules, division traps become wrong answers instead of refusals), the module's
first two-result arithmetic helper, and a tag to carry for an *unnamed temporary* — a case the tag
machinery handles for bindings and loop variables but not for results. The general engine stays on the
roadmap as what a *dynamic* tag needs.

**Literals only, and that restriction is the soundness.** `staticElemExpr` can resolve a name element, and
the container may be untouched, but the promise is about the *object*, not about what its elements' names
were bound to:

```gy
a = 1
xs = [a, "b"]
a = 5
print(xs[0] + 1)     # CPython: 2 — the slot holds what a was when the list was built
```

Reading the variable at the point of use answers `10`. A literal cannot drift, so the fold is limited to
int/float/bool literals and their negations; a name-filled or runtime-built element keeps the refusal.

**What still refuses, with the reason in the message**: a slot holding text, `None` or a container used
arithmetically (CPython raises `TypeError` there, so declining is closer than computing on an interned
index); an element of a mutated or handed-off container (`cannot reach into xs's slots`); a loop variable
over a mixed list, and a slot read through a runtime index `xs[i]` (both need the run-time tag); a fold
`max(xs[0], 5)` — which turned out not to be a tagged-value gap at all, and is filed as Gap R.73.

Measured after the change: 19 of the 20 numeric probe programs print CPython's answer on **both**
backends — `+ - * / // %`, unary `-`, comparisons, two elements in one expression, an element through a
function call, an element in an `if` condition and in a loop body, `d["a"][0] * 2`, `t[0][0] + 1`. The
refusal family gained the three shapes above, the integration trap table gained a text and a container
element used as a number, and every row still asserts the exit-2 guard.

### Gap R.73 — `min`/`max` took one argument on both engines, where CPython takes two (measured 2026-10-02 while writing the ADR 0243 probe table; 🟨 PARTIAL, ADR 0256)

`max(xs[0], 5)` was the natural "use an element numerically inside a fold" probe for the table above, and
both engines refused it for a reason that has nothing to do with tags:

| engine | `print(min(1, 5), max(1, 5))`, before |
|---|---|
| CPython | `1 5` |
| `--interp` | `min/max expects 1 argument` |
| `--aot` | `min expects one argument` |

The two backends **agree**, so this was missing surface rather than a divergence — which is exactly why it
did not belong on L11.1's row, and why a parity-only test suite never noticed it: nothing compared the
pair against CPython's varargs form.

ADR 0256 paid the source-visible half, and measuring it found that the compiled leg had never been right
either: its varargs path promoted every candidate to `double` and selected a `double`, so `min(2.5, 1)`
answered `1.0`, all-int candidates were refused, and a text or `None` candidate was compared by whatever
untagged `i32` the payload happened to hold (`min([1, "a"])` exited 0 with a number). The rule that closes
it is in the ADR — **a fold returns the candidate it chose** — and the same three programs now print CPython's
answer on three legs. What stays open is the half where a candidate's kind is not a fact the module has:
Gaps [R.107](#gap-r-107), [R.108](#gap-r-108), [R.109](#gap-r-109) and [R.110](#gap-r-110) below, each of
which needs the reduction to hand back a `(payload, tag)` pair. That is the runtime reduction Gap R.71
already owes, so the four rows and R.71 name one helper.

### Gap R.74 — `[{1, 2} for x in xs]` parsed as a list holding one set comprehension (found 2026-10-02 by comparing comprehensions against CPython, closed the same cycle with ADR 0244)

Every row below was measured through `--interp`, `--aot` and `python3` on the same file. A parity test
would have shown none of it, because both backends were wrong *together*:

| program | CPython | both backends, before |
|---|---|---|
| `d = [{1, 2} for x in [1]]` → `print(len(d[0]))` | `2` | refused / `{1}` |
| `d = [{"k": x} for x in [1, 2]]` → `print(len(d))` | `2` | `1`, and `d[0]` printed `{'k': 1, 'k': 2}` |
| `d = [{1, 2} for x in [1, 2]]` → `print(len(d))` | `2` | `1` |

**The AST was the bug, not the evaluator.** Dumping the tree said it: `[{1, 2} for x in [1]]` came back as
`ListLit[ Comp(set, elems=[1,2]) ]` — the list comprehension had vanished, and a set comprehension had
taken its `for`. `parseDictOrSet` carried two `for` branches: one *before* the closing brace, which is
`{x for x in xs}` and correct, and one *after* it. The later branch exists for the call-argument form
`len({x * x} for x in xs)`, where the display really is the whole expression the `for` completes. Inside
a `[` it is a theft — there the `for` belongs to the enclosing list display — and that single branch
explained every symptom, including the dict row: with the comprehension stolen, one dict literal was
built once and filled once per item, which is what `{'k': 1, 'k': 2}` is.

**The rule, and the case that must not break.** CPython rejects `d = {1, 2} for x in y` outright; it
accepts the display-then-`for` form only as a call argument, where it builds a *generator*. So the
branch cannot simply be deleted — `len({x*x} for x in xs)` is in the test suite and on the interpreter
path today. The parser now counts the `[` displays whose element list an expression is being parsed
inside (`inListLit`), and the brace-stealing branch runs only at depth 0. `[{1, 2} for x in xs]` builds a
list comprehension over a set literal; `len({x*x} for x in xs)` keeps the reading it has, and ADR 0244
states out loud that CPython's answer there is a `TypeError` on a generator, so the divergence is
recorded rather than asserted as truth.

**Why the test asserts the tree.** An output table can only show that the engines agree. The AST table
(`TestBraceDisplayDoesNotStealTheEnclosingFor`, ten shapes, with the call-argument form among them and a
separate `TestMisParsedShapeIsNotAcceptedAnymore`) is what makes it expensive to put the branch back.

### Gap R.75 — a comprehension wrote its slots without their tags (found 2026-10-02 with Gap R.74, closed the same cycle with ADR 0244)

With the AST fixed, the compiled backend met the programs the parser had been hiding:

| program | CPython | compiled, before |
|---|---|---|
| `xs = [[1, 2] for x in [1]]` → `print(xs[0])` | `[1, 2]` | **exit 2**: `call void @rt_append_tagged(i32 %h1, i32 @.set1, i32 7)` — `global variable reference must have pointer type` |
| `xs = [1.5 for x in [1]]` → `print(xs)` | `[1.5]` | `[1]` |
| `xs = [None for x in [1]]` → `print(xs)` | `[None]` | `[0]` |
| `xs = ["a" for x in [1]]` → `print(xs)` | `['a']` | `a` |

Four symptoms, one cause: `runtimeCompList` and `runtimeCompLoop` compiled each element with `g.value`
and appended it, falling back to the **untagged** `rt_append`/`rt_set_add` whenever `elemKindTag` could
not name the kind. Every other writer in this backend goes through ADR 0187's rule — payload and tag in
one call — because a slot that gets only a payload reads back tagged as whatever the previous occupant of
that recycled heap slot was. `xs.append(v)` had that door; the comprehension had a path around it.

- **The container element** reached the slot as the compiler's *global* (`@.set1`, `@.lst1`) rather than a
  heap handle. `llc` rejected the module and the CLI exited 2 — the compiler reporting its own output as
  broken, which the exit-code contract classifies as a bug and never as a refusal. Fixed by taking the
  element through `heapElemKind`, which materialises it into the heap the way the append path does.
- **The float element** had a correct tag and a wrong *object*: nothing had told the container its slots
  describe themselves, so the printer rendered every slot through the number printer and printed the box
  handle. Fixed by `rt_mark_estr(h, 8)` when the element's tag is one a compiled word cannot render.
- **The `None` element** never reached the runtime builder at all: `foldConstInt` answers `0` for `None`,
  which is the right answer to `if None:` and a wrong answer to "what is in this slot". `foldsToAnInteger`
  keeps non-integer elements out of the constant path, which is where the tag can still be written.
- **The text element** made the *variable* an interned-string variable — `scanStringBindings` asked
  `exprIsString` of the comprehension, which asked its element. A comprehension binds a container, so it
  is now an explicit exception, and `print(xs)` reaches the container printer instead of `rt_print_str`.

**The half-pair is the part worth keeping.** Marking the object self-describing fixed `print(xs)` and left
`print(xs[0])` printing the handle, because the *binding* still recorded the variable as an ordinary
int list (`g.mixedLists[xs] = false`, set by the comprehension branch of the assignment path). A tag is
only worth having if you follow it to the read: the fix sets the object's bit **and** marks the bound
variable tagged, and the integration table asserts both prints, which is the assertion that would have
caught the half-fix.

**What was still open is closed.** Gap R.46's remaining shape —
`names = ["a","b"]; names.append("c"); out = [n for n in names if n == "a"]; print(out[0])`, which answered
`0`, the interned index of `"a"` — needed the element's kind asked of the **iterated object** rather than
of the loop variable, whose `internedVars` facts are scoped to the loop and gone by the time the result
is bound. `compElemPrintsAsText` asks the container that is still in scope (`listElemStr`/`setElemStr`/
`dictKeyStr` of the iterable's name), so the bound list registers with the kind its slots hold and the
two reads — `print(out)` from the object, `print(out[0])` from the compiler — agree. The integration
test that used to pin the wrong output (`1\n0\n`) is now the parity case it asked to become.

### Gap R.76 — a comprehension over a mixed **set or dict** printed the machine word (found 2026-10-02 while measuring ADR 0244's element rule, closed the same cycle)

| program | CPython | compiled, before | compiled, now |
|---|---|---|---|
| `sa = {1, "a", None}` → `out = [x for x in sa]` → `print(out)` | `[1, 'a', None]` | `[1, 0, 0]` | refusal: *iterating a **set** whose elements are of more than one kind needs a tagged loop variable…* |
| `sa = set(); sa.add(1); sa.add("a")` → `print([x for x in sa])` | `[1, 'a']` | `[1, 0]` | refusal, naming the set |
| `d = {}; d["a"] = 1; d[2] = "b"` → `out = [k for k in d]` | `['a', 2]` | `[0, 1]` | refusal, naming the dict |
| `d = {1: "x", "k": 2}` → `out = [v for v in d]` | `[1, 'k']` | `['k', 'x']` | refusal, naming the dict |

The last row is the one that shows how much the wrong answer was worth: iterating a dict bound its
**keys** into the list while the program asked for its values, and dressed them in whatever the key
row's interned index happened to be. A list of small integers is a plausible-looking output for a
comprehension; nothing in the run tells you two of the elements were supposed to be text.

The cause was one registry. ADR 0241 tags the loop variable of a `for` over a mixed container, and the
guard that kept the *comprehension* loop — which never caught up — from answering anyway consulted
`mixedLists` alone. A set's slots are registered in `mixedSets`, a dict's in `mixedDicts`, and neither
was asked, so those two walked straight into the plain load and printed payloads with no tag to make
them mean anything. The zeros are exactly what ADR 0238…0241 keep saying they are: an interned index and
a fold's integer, wearing numbers. All three container kinds now go through the one door, which says
which tag is missing and which questions about the container it can already answer (`print`, `len`,
`==`). The answer itself needs L11.2's tagged comprehension loop variable, which is the same binding
`for` performs.

**The probe that nearly deleted the guard is the process lesson.** The guard looked dead: a build with a
`println` at the top of it stayed silent across `./pkg/lang` and `./integration`, and hand-run probes of
the four programs above printed `[1, 'a', None]`, `['a', 2]` and friends — so the guard was deleted on
the theory that a door no corpus row opens is a lie about what the compiler fears. It was not a lie, it
was the only thing keeping those four honest, and the corpus never opened the door because *the corpus
has no mixed-kind comprehension in it* — which is what the row is for. The hand-run probes were worse
than useless: `gustyc --file prog.gy` **defaults to the interpreter**, so every one of them measured the
backend that had the right answer all along. Re-measured with `--aot` the numbers are the table above.
Two rules come out of it, and both are now habits: a reachability probe has to run the backend the
claim is about (say `--aot` out loud, never bare `--file`), and "no test covers this refusal" is evidence
that the shape is untested, never evidence that the refusal is unnecessary.


**Paid the cycle after it was opened (ADR 0245).** The answer this row said it needed — the comprehension's
loop variable carrying its element's tag, the way `for` already does — is what shipped, so the refusal is
gone and the rows above are parity rows. Refusing was the right call when it was written (an answer that
agrees with no oracle is not an answer), and it was also the wrong call for coverage: the door caught
`[k for k in d]` over a plain text-keyed dict, a program that had always been answerable. A refusal is for
the thing the compiler cannot do, not for the thing it has not been asked to do yet.

### Gap R.77 — a comprehension walked a dict at stride 1 (found 2026-10-02 while answering Gap R.76, closed the same cycle with ADR 0245)

`d = {}; d[1] = "x"; d[2] = "y"; print([k for k in d])` answered `[1, 0]` where CPython answers `[1, 2]`,
and `{1: "x", "k": 2}` answered `['k', 'x']`. A dict entry is two words — a key word and a value word —
and iterating a dict yields its **keys**, so `for v in d:` scales its counter by 2 and measures the object
in entries (ADR 0188). The comprehension loop did neither: slot 0 and slot 1 are one key and one value, and
it called them the two keys.

Two things make this its own row rather than a clause of Gap R.76. Nothing in the program is *mixed*: the
keys are all ints and the values all text, so a guard keyed on mixed containers could never have caught
it, and the answer looks exactly as plausible as a correct one — a list of small integers. And it survived
the mixed-iterable door that was added between the two binaries measured in ADR 0245: `[1, 0]` came out of
both, which is the evidence that the guard was covering a different defect. `for` had the rule; the
comprehension had never been told, and the two loops kept their own copies of "how do you walk a dict".

### Gap R.78 — a dict comprehension wrote an interned key as an integer (found 2026-10-02 with Gap R.77, closed the same cycle with ADR 0245)

    d = {}; d["a"] = 1
    out = {k: 1 for k in d}
    print(out)         # CPython {'a': 1} · compiled, measured: {0: 1}
    print(out["a"])    # CPython 1        · compiled, measured: KeyError: key not found

Iterating a text container binds its loop variable to an index into `@str_tab`. The entry write took that
payload and asked `elemKindTag` what it was, and the answer was *int* — so the key went in as the integer
`0`, the printer wrote `{0: 1}`, and the lookup, which asks index **and** tag, could not find an entry
whose key it had been told to read as a number. One wrong tag, two symptoms, in different subsystems: the
kind facts this backend keeps in `listElemStr` / `setElemStr` / `dictKeyStr` had simply never been asked
for the dict key case, and `elemKindTag` of a loop variable does not know what the container holds.

The trap here is the one half the pair: `print(out)` and `out["a"]` are two different readers of one tag,
and fixing only the printer would have produced `{0: 1}` that happened to look up correctly. Both readers
now see the same `TagStr`, and the test asserts both, having learned it from ADR 0244.

### Gap R.79 — comparing a slot of a run-time-built mixed list with text refuses (measured 2026-10-02 while paying Gap R.76; paid the same day by ADR 0247)

    xs = []; xs.append(1); xs.append("a")
    out = [x for x in xs]
    print(out[1])                         # compiled: a            — answered
    print(1 if out[1] == "a" else 0)      # CPython 1 · compiled: refusal

The row is worth writing down because it shows where the tag now reaches: the *printer* can already ask a
slot what it holds and render text for an i32 that is an interned index, but the *comparison* lowering
still needs the needle's kind to be provable before it will run the tagged scan (ADR 0232). Same slot,
same tag, one reader short. It is not a regression — the shape never answered — and the refusal names the
missing kind rather than answering `0`, which is what an untagged compare of an interned index against a
string global would have cost.

### Gap R.80 — printing a container parameter writes the first slot instead of the container (measured 2026-10-02, left open)

    def f(y):
        print(y)

    xs = [[1, 2]]
    f(xs)              # CPython [[1, 2]] · compiled: [1]

Found while moving a row out of the refusal table: the program that exercised "the name was handed to
code this pass cannot see" also printed the container, and the two engines agreed on `[1]` where CPython
writes `[[1, 2]]`. Before writing that into the ledger I built `fa7c7b9` in a worktree and ran the same
program: same output, so this is not fallout from the tagged-read work — it is the argument-passing half
of a gap that already has a row for the return half (Gap R.67: a container *returned* from a function
compiles to `ret i32 @.lst1`). A container's payload is a handle, and a parameter that loses that fact is
printed by the number printer, which reads the object's first word and calls it the value.

The parity tables deliberately do not contain this program. A row that asserts a wrong answer because it
is convenient to have the program in the suite is how a bug becomes a requirement; the answering rows use
a function that returns rather than prints, which is what let the `len(xs[0])` half be pinned at all.

### Gap R.81 — a set accepts a member CPython refuses (measured 2026-10-02, left open)

    sa = set()
    sa.add([1])
    print(len(sa))     # CPython: TypeError: unhashable type: 'list' · both engines: 1

The dict side of this is already written down — a dict keyed by a container has no hashing rule for a
handle — but nobody wrote the set side, and the set is the one a program reaches by accident, because
`sa.add(...)` has no reason to fail. Set membership here is a payload-and-tag scan (ADR 0232), so nothing
ever asks whether the value *can* be a member: a container slot's payload is an integer handle, two
distinct lists get distinct handles and therefore distinct members, and `in` plus dedup quietly mean
something other than what the language says. The fix is small and it is the same rule twice: `rt_set_add`
raises for a list, dict or set member, with CPython's message — the trap ADR 0211's exception classes
already know how to deliver.

The row is worth keeping because it shows exactly where the tag reached and where it stopped: the
*printer* could already ask a slot what it holds and render text for an i32 that is an interned index,
while the *comparison* lowering still demanded a provable needle before it would run the tagged scan
(ADR 0232). Same slot, same tag, one reader short — and the one reader short was the ordinary question
a program asks about a slot.

ADR 0247 closed it by lowering both sides of `==`/`!=` to the `(payload, tag)` pair and letting
`rt_payload_eq` — the equality `rt_slot_eq` already uses to walk two containers — answer, which is the
third time this project has found that a tag is only worth having if you follow it to *every* reader
(ADR 0187 for writes, ADR 0244 for bindings, this for comparisons). The probe that found the pair also
found a wrong answer that had been shipping: two float slots holding `1.5` compared false, because the
helper that compared "tags then words" was comparing two box handles. That helper is gone; one equality
answers the slot question now, and a test fails if the old one comes back.

### Gap R.82 — the ordering comparison of a tagged slot read refuses (measured 2026-10-02, paid by ADR 0250)

    xs = [1, "a"]
    print(1 if xs[1] > "a" else 0)     # CPython 0 · --interp 0 · --aot: "needs a single static kind"

Equality landed with ADR 0247 because the equality it needed already existed; ordering has no such
helper to reuse, and the reason is worth stating so nobody re-does the measurement. `rt_payload_eq`
answers "same kind, then same value". An ordering has to answer, per pair of tags: within `int` an
`icmp`, within `float` an `fcmp` after unboxing each side out of its `@float_box` slot, within text a
walk of the interned bytes (the intern table is indexed by first-appearance order, so an interned index
is *not* a code-point ordering — reading the index is how `["b"] < ["a"]` would answer true), and across
kinds CPython's own `TypeError: '>=' not supported between instances of 'str' and 'int'`. Six arms and a
raise, where equality needed one call. Refusing is the honest interim: an untagged compare here does not
merely miss, it orders by whichever word the slot happens to hold.

### Gap R.83 — comparing a tagged slot with an unprovable expression had no answer at all (measured 2026-10-02, refusal in place, verdict owed)

    def pick(c):
        return "z"
        return 7

    xs = [1, "a"]
    print(1 if xs[0] == pick(1) else 0)   # CPython 0 · --interp 0 · --aot: 1 before ADR 0247

This one is not a refusal that went stale; it is a wrong answer that was in the shipped build, found by
the same probe sweep that found the float-box comparison. The left side is a slot read, the right side is
a call with two paths of different kinds, and `taggedOperand` declines the call because a call is only
taggable when *every* path returns the same kind — so the comparison fell through to the ordinary path,
compared the slot's `1` against the interned index of `"z"`, and the two happened to be the same integer.
An answer by coincidence is the exact class of defect the diagnostic-quality contract names, and no
parity test would have caught it, because the two backends were asked nothing (the interpreter answered
`0`, the compiler answered `1`, and the row was in no table).

ADR 0247 stops the wrong answer: the door refuses, in exit class 1, naming the operand whose kind cannot
be proven. What is owed is the answer, which needs a call to carry the pair a slot carries — a recorded
return kind per path, which is L11.7's "functions are values that compile" territory rather than a
comparison-lowering patch. Until then the refusal is asserted in both suites so the wrong answer cannot
sneak back as a regression.

### Gap R.84 — an ordering of two texts compared their interned indices (measured and paid 2026-10-02, ADR 0248)

    print(1 if "b" > "a" else 0)     # CPython 1 · --interp 1 · --aot 0 before ADR 0248

    a = "b"
    b = "a"
    print(1 if a > b else 0)         # CPython 1 · --interp 1 · --aot 0 before ADR 0248

A text value in the compiled backend is an index into `@str_tab`, and the index is handed out in the
order the program mentions each spelling. Equality is safe under that representation — interning is
content-addressed, so two equal texts are the same index, and that is what ADR 0173 bought — but an
ordering is not: an index records arrival, not characters. Every `<`, `<=`, `>`, `>=` between two
texts had the arrival answer, in both the value position and the condition position.

The interesting part is that the fix already existed. `rt_sort` has taken a mode argument since ADR
0173 and, for mode 1, compares two elements with `strcmp` rather than by payload, with the comment
that sorting strings by payload "would sort them by arrival and look almost right until it did not".
The sorter knew. The comparison operators were never given the same question, so a program could
`sorted(xs)` correctly and then compare two of its elements wrongly — the two-doors shape this ledger
keeps hitting, and the reason the ADR names one helper rather than one call site.

Three lowering sites had to agree, not one: `value`'s comparison arm (which emitted
`icmp sgt i32 %t1, %t2` on the two indices), the condition arm that reuses `cmpI1Op` to keep the `i1`
for a branch, and — the one that hid the defect's shape — the string-operand guard that *refused*
`later = a > b` with "operator ">" on a string is not supported in the AOT backend" while
`print(1 if a > b else 0)` cheerfully answered the wrong thing. So the same comparison was a
refusal in a `let` and a wrong answer in a `print`, and the fix had to hoist the door above the
guard rather than patch the arm under it.

Equality deliberately did **not** move to `strcmp`. It would be defensible for symmetry, but it pays a
call to learn something the representation guarantees, and the pin in `text_order_test.go`
(`TestTextEqualityIsStillAnIndexComparison`) is what keeps that asymmetry from being tidied away by a
later cycle that "unifies" the two.

### Gap R.85 — an ordering between kinds that do not order against each other gets a verdict (measured 2026-10-02, left open)

    print(1 if "a" < 1 else 0)        # CPython TypeError · --interp TypeError · --aot 1
    print(1 if 1 < "a" else 0)        # CPython TypeError · --interp TypeError · --aot 0
    print(1 if None < "a" else 0)     # CPython TypeError · --interp TypeError · --aot 0
    s = "a"
    n = 1
    print(1 if s < n else 0)          # CPython TypeError · --interp TypeError · --aot 1

The interpreter is right here: it raises `TypeError: '<' not supported between instances of 'str' and
'int'`, class and sentence, and `pkg/lang/slot_ordering_test.go` pins that so the two backends cannot
drift apart about what traps. The compiled leg compares the two words it has — an interned index and an
integer — and reports whichever way they happen to fall. That is Gap R.37's class exactly: a decision
the compiler makes statically with no way to become a language event, which is why the row points at
R.37 rather than pretending to be new.

The shape of the fix is already sketched, because the ordering door written for ADR 0248 needed the
classification anyway: an `orderKindOf` that sorts each operand into numeric / text / unorderable /
unknown (a slot read placed by the tag its literal wrote, via `staticSlotTag`). Same family answers;
different families raise through the door `lenOfTaggedSlot` already uses (`raiseTo(exnCode("TypeError"),
...)` per kind, with the sentence built from `tagName`); an unknown family refuses by naming the tag. The
first half of that is written and parked with the R.82 work, not in this commit, because it is a
comparison-typing feature and ADR 0248 is a rendering-of-text fix.

### Gap R.86 — a container orders against a container by its heap handle (measured 2026-10-02, left open)

    xs = [2]
    ys = [1]
    print(1 if xs < ys else 0)        # CPython 0 · --interp 0 · --aot 1

    xs = ["b"]
    ys = ["a"]
    print(1 if xs > ys else 0)        # CPython 1 · --interp 1 · --aot 0

Python orders containers lexicographically and no Python answer ever depends on which object the
allocator handed out first; here both rows depend on exactly that. Equality got the right helper long
ago — `rt_container_eq` walks the slots and asks `rt_slot_eq` for each pair (ADR 0189) — and an ordering
needs the walk with a sign instead of a boolean: an `rt_container_order` that pairs up slots, stops at
the first pair that does not order, and returns -1/0/1. Until then the two directions are both pinned in
the details rather than in a test, because one row alone would look like a passing program half the time
(the first literal allocated is the smaller handle, so one of the two rows always agrees with CPython by
accident).

### Gap R.87 — two list literals compared for order make the compiler emit a module `llc` rejects (measured 2026-10-02, left open)

    print(1 if [1, 2] < [1, 3] else 0)   # CPython 1 · --interp 1 · --aot: exit 2

    llc-20: /tmp/gusty-jit-*/jit.ll:21:22: error: global variable reference must have pointer type
      %t1 = icmp slt i32 @.lst1, @.lst2

Exit 2 is not a refusal. Under ADR 0166's exit-code contract it means the compiler produced a module the
toolchain rejects, and the program asked nothing exotic — a comparison of two list literals, the same
question `==` answers through `rt_container_eq` and the same reason `containerOperand` exists (to keep
`@.lstN` out of an `i32` slot, ADR 0166's global-in-a-parameter shape). The ordering door is the last
comparison path that never learned either rule: equality was taught, ordering was not.

This is filed separately from Gap R.86 on purpose. R.86 is a wrong answer, which is bad and quiet; this
one is loud, and the fix is different — materialise both literals before the comparison, or refuse the
shape honestly — but in neither case may the emitted module hand `@.lstN` to an `icmp`.

### Gap R.88 — a numeric use of a slot read through a computed index (measured 2026-10-02, closed by ADR 0249)

    xs = [10, 4]
    i = 0
    print(xs[i] / 4)          # CPython 2.5 · --interp 2.5 · --aot: exit 2

    %t2 = fdiv double , %t1   # what the module said, which llc-20 rejects

Three measurements, three different failures, one missing question. The literal promise of ADR 0243 stops
at the index the program computes; past that line the read had no way to say what it held, so the float
arm asked for an operand and got an empty string, and the arithmetic it emitted had a hole in it. The
same read reached the *print* path the same way — `fsub double 0.0, ` and `rt_fmt_double(double )` were
both reachable, in code written long before this, which is the second exit-2 hole here and the reason the
fix is a rule about empty operands rather than a patch to one operator.

What the sweep measured before anything was written (all four columns run through `python3`, the
interpreter and `--aot` on the same file):

| program | CPython | `--interp` | `--aot`, before |
|---|---|---|---|
| `xs=[10,4]; i=0; print(xs[i]/4)` | `2.5` | `2.5` | **exit 2** |
| `xs=[1.5,"a"]; i=0; print(xs[i]+1)` | `2.5` | `2.5` | refused *needs a single static kind* |
| `xs=[1.5,"a"]; i=0; print(-xs[i])` | `-1.5` | `-1.5` | refused |
| `xs=[1.5,"a"]; i=1; print(-xs[i])` | TypeError *bad operand type for unary -: 'str'* | TypeError | refused |
| `xs=[1,"a"]; k=1; print(1 > xs[k])` | TypeError *'&gt;' … 'int' and 'str'* | TypeError | TypeError *'str' and 'int'* (reversed) |
| `xs=[1,"a"]; i=0; print(xs[i]+1)` | `2` | `2` | refused |
| `xs=[1,2.5]; i=0; j=1; print(xs[i]+xs[j])` | `3.5` | `3.5` | refused |

The last row is the one that stayed refused, and it is the interesting one. The float arms are the only
place that can ask a tag, and the float arms always answer a `double` — so answering `3.5` there is fine,
but answering `xs[i] + 1` on a container whose slots are ints *here* and floats *there* prints `2.0` where
CPython prints `2`. That is not a near miss, it is a different value, and the next line divides it. So the
gate is the **result's** kind rather than the operand's: a float-only container is admitted for the
arithmetic operators, true division is admitted whatever arrives because Python always answers a float, an
ordering is admitted because it answers `0`/`1`, and everything whose result kind is only knowable while
the program runs is declined with the debt named — which is Gap R.82's tagged value word, arriving from
the arithmetic side instead of the comparison side.

Two details worth keeping, because both are the kind of thing that reads as pedantry until it is a bug:

- **The two type names in a TypeError come in source order.** The first draft built the message from
  (the slot's kind, the other operand's kind), which is right for `xs[i] > 1` and wrong for `1 > xs[i]`.
  CPython writes the left type first either way, so the door carries which side the read was on.
- **Negation is not a binary minus wearing a hat.** `-xs[i]` over a text slot is
  *bad operand type for unary -: 'str'*, a sentence the binary path never produces; lowering it as
  `0 - xs[i]` would have raised *unsupported operand type(s) for -: 'int' and 'str'* for a program that
  never contained a binary minus. Hence its own door, sharing the tag dispatch.

`*` and `%` over a container that can hold text are also declined, and the reason is that CPython does not
raise there at all: `"ab" * 2` repeats and `"a" % 2` formats (*not all arguments converted during string
formatting*). A door whose only vocabulary is `raise` would answer both wrongly, so they wait for Gap R.33
and Gap R.31.

A refusal pin broke, as one is allowed to do when the work lands: `mixed_list_test.go` insisted
`print(1 if xs[1] > 2 else 0)` over `[1, "a"]` must be refused at compile time. CPython raises for that
program; both backends now raise the same sentence; the row moved to the trap table and the table checks
both the interpreter and the compiled binary.

### Gap R.89 — unary minus never asks a tag (measured 2026-10-02, CLOSED by ADR 0266 on 2026-10-04)

    print(-"a")     # CPython TypeError: bad operand type for unary -: 'str'
                    # --interp -281474976710658   --aot 0
    print(-[1])     # CPython TypeError: bad operand type for unary -: 'list'
                    # --interp -281474976710658   --aot: exit 2 (%t1 = sub i32 0, @.lst1)

The interpreter negates the interned index; the compiled backend negates 0. Neither is a wrong answer by a
little — `-281474976710658` is a heap-table position being presented as arithmetic — and the list-literal
form reaches `llc` with a global in an `i32` slot, which under ADR 0166 is exit 2, the compiler's own bug.

ADR 0249 fixes exactly one shape here: a float-family container read through a computed index raises
CPython's unary sentence on the compiled path, pinned in `tagged_numeric_test.go` with the interpreter half
marked `aotOnly` rather than quietly dropped. The rest is unary minus's own work: it has to ask what kind
its operand is, in both backends, and the literal cases must trap at run time rather than be folded into a
compilation error (Gap R.37's rule, which this is another instance of).

**Closed 2026-10-04 by ADR 0266.** `pkg/lang/negation.go` is the operator's own road: the interpreter's
`negate` asks the value through `operandKind`, the compiled `negationOperandKind` asks the expression
through the same records the print dispatch reads (`printsAsInternedStr` for the text question, so the two
doors cannot disagree about what a name holds), and both raise the reference's sentence through the ordinary
emitted store-and-branch. The `aotOnly` pins in `pkg/lang/tagged_numeric_test.go` and
`integration/tagged_numeric_test.go` were flipped rather than deleted, so the interpreted half is now
asserted instead of remembered. `-"a"`, `-[1]`, `-None`, `-{"a": 1}`, `-{1, 2}`, `-Token()`, `-xs[0]` and
`-"hi" + 1.5` all stop at exit 3 on both engines, with 14 negations that still have to answer doing so;
`programs/negation_names_the_kind.gy` is `match` on three engines. The list literal's `sub i32 0, @.lst1` —
the exit-2 half — is gone, and an IR row fails if a global reaches an `i32` arithmetic instruction again.
Two siblings the same measurement found are filed rather than absorbed: `abs` of a text (Gap R.140) and a
tuple named `'list'` by the interpreter (Gap R.141).

### Gap R.90 — an out-of-range subscript names the wrong container (measured 2026-10-02, left open)

    xs = [1.5, 2.5]
    print(xs[7])    # CPython IndexError: list index out of range
                    # both engines: IndexError: index out of range

Found while checking that opening the numeric door had not lost the bounds check. It had not — the read
still goes through `rt_get_elem`, and `TestTaggedNumericKeepsTheBoundsCheck` pins the trap on both engines
for a literal index and a computed one. What is wrong is the sentence, and a sentence is contract: a
program that catches `IndexError` and looks at the text behaves differently here than in Python. One word,
taken from whichever table both backends already read their messages from, with `KeyError`'s wording left
alone because a key is a key.


### Gap R.91 — a loop variable over a container that mixes kinds is given a verdict (measured 2026-10-02, left open)

    xs = ["b", "a"]
    for k in xs:
        print(1 if k > 0 else 0)       # CPython TypeError · --interp TypeError · --aot: 1

The slot read `xs[i]` now asks the tag which pair the comparison was (ADR 0250); the loop binding `k` is the
same `(payload, tag)` pair one step earlier, and the tag is dropped on the way into the comparison, so the
compiled leg compares a word and prints a verdict for the `TypeError` both the oracle and the interpreter
raise. `print(k + 1)` beside it refuses with the concatenation sentence, which is at least honest — a refusal
and a verdict are not the same distance from right. ADR 0226 already promotes a mixed container for the loop
itself, so the pair is there to be bound.

### Gap R.92 — an ordering of two dict value slots answers by the interned index (measured 2026-10-02, left open)

    d = {}
    d["k"] = "b"
    d["j"] = 1
    print(1 if d["k"] > "a" else 0)    # CPython 1 · --interp 1 · --aot: 0

ADR 0248 taught a dict whose values are all text to order by the text behind the index. As soon as a second
kind appears in the value slots the read falls back on the index itself, which is exactly the answer Gap R.84
measured for lists: `b` arriving before `a` makes `b > a` false. The tracked `dictValStr`/`dictValInt` maps
already know both kinds are possible, so the fix is the routing, not new information.

### Gap R.93 — an ordering of a slot in a container a loop built prints a verdict (measured 2026-10-02, closed by ADR 0252)

    xs = []
    xs.append(1)
    print(1 if xs[0] > "a" else 0)     # CPython TypeError · --interp TypeError · --aot: 0

ADR 0250's door declines a slot whose kinds it cannot list, which is the honest half. The dishonest half is
what happens next: the lowering underneath, which never had to answer this shape before, compares the payload
it has and prints `0`. The `append` path records kinds in the same maps the print and length doors read, so
the arm list can be built for a run-time container too; until it is, the shape must refuse by naming itself.

### L11.1 (1a) — the nested read of a container the program built (measured 2026-10-02, closed by ADR 0251)

    xs = []
    xs.append([7, 8])
    print(xs[0][0])          # CPython 7 · --interp 7 · --aot: exit 1 "index cannot reach into xs's slots"
    d = {}
    d["a"] = [1, 2]
    print(d["a"][1])         # CPython 2 · --interp 2 · --aot: exit 1, same sentence about d
    xs.append({"k": 5})
    print(xs[2]["k"])        # CPython 5 · --interp 5 · --aot: exit 1 — and no static kind exists to ask for

Three legs, two answers, on the exact program the roadmap row names as its own remainder. The read ADR 0241
grants is a *compile-time promise* — the literal behind the name is still the object in front of it — and an
`append` takes that promise away, which is why the first refusal was honest. What made it a gap rather than a
limit is that the promise was never the only source of the same fact: ADR 0187 makes every writer put a tag
beside every payload, so the object can say what its slot holds, and ADR 0246 had already started asking it for
`len`.

What the row did not say, and the measurement did, is that the answer is not one door but four. The tag beside
the outer slot says what kind of object the payload names, and the arm it selects is the read that kind
supports: a position (with `normalizeIndex`, so `xs[0][-1]` counts from the end and `xs[0][9]` raises
`IndexError` like any other subscript), a key (`checkKeyReadTagged`, so a missing one raises `KeyError`), a
character of a text (`rt_str_char`, one-character strings interned, `IndexError: string index out of range`
past the end), or — for a set — the membership question the language documents for a set subscript. The tags
that name nothing raise, per kind: `'int' object is not subscriptable`, `'float' …`, `'bool' …`, `'NoneType' …`,
and a generic sentence for a kind this language has not grown `__getitem__` for. Every one of them is a *trap*,
exit 3, catchable — not a compile-time refusal, which would have been exit 1 for a program whose only crime is
a wrong index (ADR 0166's classes again).

The interesting case is the one no table could have held:

    xs = []
    xs.append([1, 2])
    xs.append({"k": 5})
    print(xs[0][1], xs[1]["k"])      # 2 5   — one container, two slot kinds, two different questions

A single static element kind would answer one of those wrongly, and a compiler-side table of "what `xs` holds"
would have had to be wrong the moment a second `append` landed. This is the same lesson ADR 0250 learned about
arms, from the other side: the arms that *can* run must all be emitted, and a `phi` may name only the blocks
that actually reach the merge — so the four value arms end in their own tail blocks, the raise arms end at the
handler, and the merge stays legal by construction (ADR 0205).

The refusal sentence aged with the feature, and one claim came out of it: it had been advertising that "`in`
still works", which the same sweep measured as false (`7 in xs[0]` refuses). Gap R.38 counts a refusal that
overstates its backend as a defect of its own, so the sentence now lists what is actually answered — `print`,
`==`, `len`, a further subscript — and the two shapes that need the object's *kind* rather than its tag got
rows: Gap R.95 (`in`, `for`) and Gap R.94 (a set variable read directly, answered by one backend and refused
by the other).

### Gap R.94 — a set variable's subscript is answered by one backend and refused by the other (measured 2026-10-02, left open)

    sa = {5, 6, 7}
    print(sa[6])             # --interp 6 · --aot: exit 1 "index of a non-literal variable"

A set subscript is a *documented* gusty extension, not a Python behaviour: `docs/language.md § Dicts & sets`
grants it, `programs/data_b.gy` and `programs/features_b.gy` use it, and their ledger rows are
`oracle: not_applicable` because CPython rejects every set subscript with `TypeError: 'set' object is not
subscriptable`. Deliberate divergence is fine; divergence **between the two backends** is not, and that is
what this is. The set arm ADR 0251 wrote for a set in a *slot* is the missing lowering for a set in a
*variable* — same helper (`rt_set_find`), same `KeyError: not in set` when the member is not there.

Two more things came out of measuring it. The first is that `docs/language.md` described this extension as
picking "the element at index 2" while both engines had always read the subscript as a *membership* question —
`{1, 2, 3}[2]` answers `2`, never `3`. The paragraph now says what the code does, because a source of truth
that describes a behaviour nobody implements is worse than no paragraph. The second is that the compiled tag
arm was briefly the only engine raising CPython's sentence for a set subscript, and the tempting fix — dragging
the interpreter to CPython's answer — quietly deleted a documented surface with an `oracle: not_applicable`
ledger row behind it. That change lasted ten minutes and one red conformance matrix; the decision, and the
reason it was the wrong one, are in ADR 0251's rejected alternatives.

### Gap R.95 — a membership test or a loop whose haystack is a run-time-built slot read refuses (measured 2026-10-02, left open)

    xs = []
    xs.append([7, 8])
    print(7 in xs[0])        # CPython True · --interp 1 · --aot: exit 1 "this context needs a single static kind"
    for v in xs[0]:          # CPython 7, 8 · --interp 7, 8 · --aot: same refusal
        print(v)

The read ADR 0251 opened is the sibling of these two, not the same door: a subscript needs the slot's *tag*,
which one `icmp` per kind resolves, while `in` picks among `rt_contains_tagged` / `rt_dict_has_tagged` /
`rt_set_contains_tagged` and `for` picks a length-and-elements walk, both by the object's *kind* — a
compile-time `string` in the code that picks the helper today. The dispatch is written; it has to be reached by
these two callers as well, and a haystack whose tag names a scalar must then raise what CPython raises rather
than iterate nothing (Gap R.29's rule for an iterator that cannot be lowered).

### L11.1 (1b) — an ordering of a slot no literal describes (measured 2026-10-02, closed by ADR 0252)

    xs = []
    for i in [1, 2]:
        xs.append(i)
    print(1 if xs[0] > "a" else 0)   # CPython TypeError · --interp TypeError · --aot printed 1

    d = {}
    d["k"] = 1
    print(1 if d["k"] > "a" else 0)  # the dict has no literal either: same printed verdict

    xs = []
    xs.append([3, "a"])
    print(1 if xs[0][0] > 1 else 0)  # 1 everywhere; ADR 0251 answered the read, not the comparison

Three shapes, one cause: ADR 0250 built its three arms — numbers, texts, CPython's `TypeError` — out of the
list of kinds the **literal** could show, and a container built by `append` in a loop has no such list. Its gate
declined, and the lowering underneath, which compares `i32` words, answered for a program the oracle crashes.
That is the same defect family as Gap R.82, Gap R.85, Gap R.91 and Gap R.92 — a verdict printed where CPython
raises — measured a fourth time in a week, which is the argument for fixing the representation rather than the
symptom each time.

The obvious fix was a second table: track, per variable, the kinds its slots have ever held. It was rejected
without being written, because the measurement that produced the bug also disposes of it — `xs.append(3)` then
`xs.append("a")` then `xs.append(1.5)` is three slots and three kinds, and any compile-time list for that `xs` is
either the tag array restated or a guess about data the pass has not seen. ADR 0187 already makes every writer
put a tag beside every payload; ADR 0246 and ADR 0251 had already taught `len` and the subscript to ask. The
ordering door was the last caller still reading a notebook.

What the tag answers, per arm: `orderAskTags` asks "are you one of the three number tags?" and "are you the text
tag?", the numeric arm lifts with `rt_float_of` or `sitofp` and compares doubles, the text arm goes to
`rt_str_order` — ADR 0248's helper, reached through the tag for the first time — and everything else is the raise
arm. The raise arm is where the design earned its keep: CPython's sentence names **both** operand types
(`'>' not supported between instances of 'list' and 'int'`), so with one side reported by the object the arm
cannot be one `raiseTo`. It is a chain over the closed tag set — `int`, `bool`, `float`, `NoneType`, `list`,
`dict`, `set`, and the text tag as the unconditional last arm — each link raising the sentence for its own kind.
It may end in an `else` only because the set of tags ADR 0187's writers leave beside a payload is closed; had it
been open, the last link would have been a guess about a kind the compiler had never seen, which is exactly the
mistake the chain exists to stop.

An ordering can be lowered this way at all because its **verdict is a bool whatever the operands turn out to be**.
That sentence is also the boundary of this cycle: `xs[0][0] + 1` must know whether it answers `4` or `4.5` before
the module exists, so it still refuses, honestly, by naming itself.

Two shapes were measured beside the change and filed instead of absorbed. **Gap R.96**: `xs = []` /
`xs.append(3)` / `print(xs[0] / 4)` prints `0.0` with exit 0 where CPython and the interpreter print `0.75` —
`/` is the one arithmetic operator whose result kind *is* settled (true division is always a float), so the float
arm is reachable for a slot it cannot describe, and ADR 0249's empty-operand `fdiv` has come back as a silent
zero rather than a rejection; the neighbours (`+ 1`, `* 2`, `// 1`, `% 1`, `** 2`) either answer correctly for an
int slot or refuse for a float one, so this is one operator's arm, not the door's. **Gap R.97**: `xs[0] > ys[0]`
over two built containers answers by the two payloads, because the gate admits one side whose kind the object
reports and not two — a sentence naming two types needs one branch per *pair* of kinds, and the pair it would meet
most often (container against container) is a pair CPython *does* order, which is Gap R.86's helper, not a trap.
Both are pinned by `TestSlotOrderOfTwoUnliteralisedSlotsStaysFiledNotFixed` on each side of the boundary, with
what every engine answers today written into the row: the rows fail when the compiler catches up, so a filed
divergence cannot quietly become a passing test.

Two shapes on rows that were already open came out of the same sweep, and are recorded there rather than as new
IDs because the emission is the same one: an ordering whose settled side is a **container literal**
(`print(1 if 3 > [0] else 0)` → `%t1 = icmp sgt i32 3, @.lst1`, `print(1 if [0] > 3 else 0)` → `%t1 = icmp sgt i32
@.lst1, 3`, both **exit 2**) is Gap R.87's global-in-a-value-position in a new costume; and an ordering against a
**call of two kinds** (`xs[0] > pick(1)` printing `1` where the oracle traps) is Gap R.83's missing return tag
seen from the relational side. Neither is made worse by this cycle — both reach the same lowering they reached
before — and both are now named by a table that fails when someone fixes them.

### L11.1 (1c) — the true division of a slot no literal describes (measured 2026-10-02, closed by ADR 0253)

Gap R.96 was found by writing ADR 0252's tables and asking the obvious question — an *ordering* can ask the
object because its verdict is a bool either way, so which arithmetic operator can? The answer is exactly one,
and the row's own program is the measurement:

| program | CPython 3.12 | `--interp` | `--aot` (before ADR 0253) |
|---|---|---|---|
| `xs = []` / `xs.append(3)` / `print(xs[0] / 4)` | `0.75` | `0.75` | **`0.0`, exit 0** |
| `xs = []` / `xs.append(1.5)` / `print(xs[0] / 2)` | `0.75` | `0.75` | **`0.0`, exit 0** |
| `xs = []` / `xs.append(3)` / `print(xs[0] + 1)` | `4` | `4` | `4` |
| `xs = []` / `xs.append(1.5)` / `print(xs[0] + 1)` | `2.5` | `2.5` | refusal (honest) |

`0.0` is the interesting part. It is not arithmetic gone wrong: it is ADR 0249's **empty operand** — the float
arm calling `floatValue` for a register the tag never described, getting `""` back, and the caller writing the
literal `0.0` so the instruction would at least parse. The version that reaches `llc` is exit 2, which ADR 0166
already assigns to the compiler; the version that substitutes is worse, because the program prints a
number-shaped answer, exits 0, and the person reading it has no reason to look twice. `+ 1`, `* 2`, `// 1`,
`% 1`, `** 2` on the same slot either answered correctly for an `int` slot or refused honestly for a `float`
one, which is what told us the defect was one operator's arm and not the door's.

Why `/` may be answered at all: true division is a float **whatever arrives**, so the one thing the module has
to commit to before the slot is asked — the result's kind — is settled by the operator, not by the data. Every
other operator's result kind is a fact about the slot, and a compiler that guesses it prints `4.5` where the
program's data says `4`. The gate in `taggedNumberOperands`/`taggedNumberUseApplies` therefore admits a side
the literal never described (`slotReadFromObject`, the same gate the print, the equality, the length, the
ordering and the subscript use) **for `/` only**, and the arms are the closed tag chain: `rt_float_of` for a
float slot, `sitofp` for `int`/`bool`, one `raiseTo` per other kind with CPython's sentence for this operator
and this kind.

The zero trap took the longest to get right, and the reason is entirely about wording. LLVM's `fdiv` does not
trap — it answers ±inf — so the `fcmp oeq … 0.0` guard is ours, and CPython's two sentences:

| pair | CPython's sentence |
|---|---|
| `3 / 0`, `xs[0] / 0` with an `int` slot | `division by zero` |
| `1.5 / 0`, `xs[0] / 0.0`, `xs[0] / 0` with a `float` slot | `float division by zero` |

With one operand's kind only in the object, a guard placed after the merge could not tell which table to read,
so the guard is emitted *inside* each arm, where the arm knows what it lifted — `guardNonZeroFloat` and
`branchRaise` now return the block the continuation runs in, so the arm's `phi` names the block the guard ends
in (ADR 0138's lesson, now carried in the signature). A guessed sentence is not a cosmetic failure: the
program's own `except ZeroDivisionError:` may read the message, and a person always does.

Three defects in the same emission family fell to the sweep, all pre-existing, all made visible by the new arm
either reaching them or asking about them:

| program | `--aot` before ADR 0253 | after |
|---|---|---|
| `t = 0.0` / `t += 1.5` / `print(t)` | `llc` rejects: *multiple definition of local value named `_t`* → **exit 2** | `1.5` |
| `u = 0` / `u += 1.5` / `print(u)` | **`panic: assignment to entry in nil map`** — the compiler crashed | refusal, exit 1, names the tagged value word |
| `n = 0` / `n += xs[0] / 2` | would emit `add i32 %_n.ld, %t` with `%t` a `double` → exit 2 | refusal, exit 1 |

The augmented-assignment pair had been lowering its float result **twice** — once into a dead `add i32` nobody
read, once into the `fadd` that stored it — and the dead half was harmless only while every operand it folded
was an `i32` the folder could finish. `g.isFloat` is the question both statements now ask once, in the domain
that stores the value, and `allocd` keeps the second `%_t` alloca from ever being written.

**The domain gate.** `floatValue` increments `doubleDomain` around everything it lowers, and `value()` refuses
a tagged numeric `BinOp` when the counter is zero. The rule is the one ADR 0166 forces: an answer the caller
cannot hold is not an answer. Printing, a comparison, an `if`/`while` head, a float binding and a float-boxed
container element ask for the double and take it; a call argument, `str()`'s argument, a dict slot written by
key and `+=` onto an `int` variable do not, and receiving it there is the module `llc` rejects. Those four
stay refusals and are recorded as Gap R.98 — answering them is the `(payload, tag)` pair, i.e. the tagged value
word itself.

Four shapes were measured beside the change and **filed rather than absorbed**, each by a table that fails when
the compiler catches up: Gap R.98 (a double handed to an `i32` context), Gap R.99 (a float element of a
comprehension over a container the program built, `[xs[0] / 2]` → `[2]`), Gap R.100 (a raising element moving
the comprehension's loop back edge, `[v / 2 for v in xs]` → **exit 2** from *PHI node entries do not match
predecessors*), Gap R.101 (`xs[0] / ys[0]`, the pair-of-kinds table the ordering also owes as Gap R.97). The
last three are pinned by `TestTrueDivisionInsideAComprehensionIsFiledNotFixed` and
`TestTrueDivisionOfTwoUnliteralisedSlotsIsFiledNotFixed`, in the unit file with each engine's own answer written
into the row.

### Gap R.96 — the true division of a slot the program built answered `0.0` with exit 0 (measured 2026-10-02, closed by ADR 0253)

The row as filed by ADR 0252's sweep, and the measurement that settled it, are in
[L11.1 (1c)](#l111-1c--the-true-division-of-a-slot-no-literal-describes-measured-2026-10-02-closed-by-adr-0253):
`xs = []` / `xs.append(3)` / `print(xs[0] / 4)` printed `0.0` on the compiled leg for `0.75` everywhere else, the
`0.0` being ADR 0249's empty `fdiv` operand substituted by the caller rather than refused. Closed by ADR 0253 —
the arm asks the tag (`rt_float_of`, `sitofp`, one `raiseTo` per other kind), the substitution is gone from the
file, and an operand that still cannot be lifted is refused at the front end. `integration/programs/slot_division.gy`
is the corpus row: 19 lines, three engines, byte-identical.

### Gap R.98 — the numeric door's double reaches a context that stores an i32 word (measured 2026-10-02, half paid by ADR 0253)

A `double` is only an answer where the caller can hold one. Four sinks cannot, and each of them used to be a
module `llc` rejects or a crash:

| program | before ADR 0253 | today |
|---|---|---|
| `def f(a, b): return b` / `print(f(1, xs[0] / 2))` | `call i32 @gy_f(i32 1, double %t)` → **exit 2** | refusal, exit 1 |
| `print(str(xs[0] / 2))` | `str()` of a double as an i32 word | refusal, exit 1 |
| `d = {}` / `d["k"] = xs[0] / 2` | the double truncated into the slot, `print(d["k"])` answered `2` | refusal, exit 1 |
| `n = 0` / `n += xs[0] / 2` | `add i32 %_n.ld, %t` with `%t` a `double` → **exit 2** | refusal, exit 1 |
| `t = 0.0` / `t += 1.5` | *multiple definition of local value named `_t`* → **exit 2** | `1.5` — paid |
| `u = 0` / `u += 1.5` | **`panic: assignment to entry in nil map`** | refusal, exit 1 — the crash is paid |

The refusals stand because the value that leaves the expression it was computed in is the `(payload, tag)` pair
the tagged value word carries: a call's argument word, `str()`'s argument word and a container slot are one
static word each, and choosing that word from the run-time kind is precisely what L11.1 owes. The gate is a
counter (`doubleDomain`) around `floatValue`, so the refusal is raised by the context, in one sentence, rather
than by whichever instruction happened to be emitted last.

<a id="gap-r-99"></a>
### Gap R.99 — a float element of a comprehension over a container the program built is appended as an i32 (✅ CLOSED by ADR 0262, measured 2026-10-02)

```
xs = []
xs.append(6)
print([xs[0] / 2])      # CPython [3.0] · --interp [3.0] · --aot [2]
print([v / 2 for v in xs])
```

Not a regression, and not the division's: the comprehension appends its element through the **static** path,
which never consults the numeric door, so the `double` is truncated to the one `i32` word a slot is. The same
program with an `int` element (`[v + 1 for v in xs]` → `[7]`) is correct today, which locates the defect at the
element's tag rather than at the loop. The fix is the one ADR 0238/ADR 0239 taught the `append` path — box the
float, write the tag, let the printer and the read ask it — applied to `appendElem`, and the two rows in
`TestTrueDivisionInsideAComprehensionIsFiledNotFixed` fail when it lands.

**Closed by ADR 0262, from one door earlier than this row predicted.** The element *was* already boxed and
tagged — `heapElemKind` asks `g.isFloat(e)` of the expression, not just a literal, and it has for a while. What
was missing was the container's own announcement: `literalNeedsTags` tested `isFloatLitExpr`, so an element that
is a float-valued *expression* rather than a float literal left the object unmarked, the printer followed the
container-wide kind, and the `@float_box` handle printed as the number it is — `[2]`, the handle, not the value.
`floatSlotExpr` asks the element as an expression, which is exactly the shape ADR 0259 chose for a verdict slot
(`boolSlotExpr`) for the same reason. The row out of `TestTrueDivisionInsideAComprehensionIsFiledNotFixed` is now
`TestAFloatElementOfAComprehensionOverABuiltContainerIsAPaidRow`, in the same file, asserting CPython's answer on
both engines; Gap R.100 (the element that raises, whose guard blocks land between a loop header and its `phi`'s
back edge) is the sibling that is still open.

<a id="gap-r-100"></a>
### Gap R.114 — an f-string cannot interpolate a container variable (OPEN, measured closing Gap L.2)

```gusty
xs = [1, 2]
print(f"{xs}")          # CPython and --interp: [1, 2]
```

The compiled leg answers `gustyc: jit: codegen: codegen: undefined name "xs" (no binding for it;
assign it before use)`, exit 1, on both paths — while the same shape written `print(str(xs))` prints
`[1, 2]` on all three engines (that row is green in `TestRenderPairWritesWhatCPythonWrites`). The name
*is* bound: `print(xs)` from the same source compiles. The interpolation lowers its expression in a
scope that never saw the binding, so it never reaches a renderer at all.

An f-string's `{expr}` **is** `str(expr)` — which is what makes ADR 0258's pair worth reaching for: one
call into `renderPair`, the same arms, the same refusal. Filed rather than fixed in this cycle because
the defect is the interpolation's scope handling, not the renderer's, and because a defect measured
while closing one row and left unwritten is the same as one that does not exist. No test yet; the row
moves from the refusal tables into the pair table when it closes.

### Gap R.115 — `str()` / `repr()` of a value whose kind no expression names answers digits (OPEN, measured closing Gap L.2)

```gusty
def g():
    return [1, 2]

print(str(g()))         # CPython and --interp: [1, 2]
```

Compiled, this is exit 2 today — `llc: global variable reference must have pointer type` on
`ret i32 @.lst1` — which is Gap R.67's emission, owned there, and measured unchanged by ADR 0258. What
*this* row owns is what the pair does with a value whose kind the compiler cannot see once the handle
does survive: it takes the digits arm and writes the decimal form of the heap index, with exit 0. Both
halves agree, which is exactly what L11.2 promised them, and both are wrong in the same place — the
value arrived as an untagged word, and a table cannot be consulted about a kind nobody states.

The container arms fire for a literal, for a variable the container records registered, and for a
constructor whose inferred type names its kind (`str(set())`, `str(list())` — the checker's type being
the second witness the container arm consults). The digits arm is what remains, and it should answer
only a value the compiler can *prove* is a number; anything else should be refused the way a tuple
already is (`TestStrOfAValueWhoseFormIsNotVisibleRefusesBothHalvesTogether` pins that refusal, and
`TestPairRefusalIsWrittenForTheAgent` forbids exit 2 on the CLI leg). L11.1's tagged value word is the
real owner: a `(payload, tag)` pair reaching the builtin is a form the renderer can be asked about.

### Gap R.100 — a comprehension element that traps moves the loop's back edge and `llc` rejects the module (measured 2026-10-02, closed by ADR 0255)

```
xs = []
xs.append(6)
print([v / 2 for v in xs])
# llc: PHI node entries do not match predecessors!
#   %t3 = phi i32 [ 0, %comp.pre1 ], [ %cc2, %comp.body3 ]
#   label %comp.body3 / label %fdiv.ok6
```

The runtime comprehension writes its induction `phi` with a back edge named for the block the body *starts* in
(`comp.body`), and an element that emits a guard — the zero trap here, a bounds check tomorrow — ends the body
in a block the `phi` has never heard of. It is ADR 0224's lesson (a `phi` whose entry list must match its
predecessors, discovered with a string filter) met again one door over, and the same root as the `for`
statement's latch, which is why `for v in xs: print(v / 2)` works and the comprehension does not. The fix is to
thread the generator's *current block* through the body rather than assume it, the way `branchRaise` now reports
the block it ends in. Exit 2 on an ordinary program is ADR 0166's own class, which is why this row is in the
queue's priority-1 neighbourhood rather than filed under the comprehension.

### Gap R.101 — the true division of two slots the object would have to describe refuses (measured 2026-10-02, left open)

```
xs = []
xs.append(6)
ys = []
ys.append(4)
print(xs[0] / ys[0])    # CPython 1.5 · --interp 1.5 · --aot refusal (exit 1)
```

Honest, but a refusal of a program the oracle answers, so it is recorded rather than praised. The gate admits
one side whose kind the object reports, because the raise sentence names *two* types and a second such side is
one branch per pair of kinds — 16 blocks of tag tests for one `fdiv`, for the common case where both slots are
numbers. It is the same table the ordering owes as Gap R.97 and the equality would have wanted before ADR 0247's
`rt_payload_eq`; the shape worth building is one pair table shared by `/`, `<`/`>` and `==`, not three. Pinned by
`TestTrueDivisionOfTwoUnliteralisedSlotsIsFiledNotFixed` (unit) and the `two_built_slots` row of
`TestTrueDivisionOfAnUnliteralisedSlotRefusesHonestly` (CLI), which fail when the door grows the pair.

### Gap R.3c — re-measured beside ADR 0253: the leg stopped being rejected and started being wrong

`programs/probe_float_param_rebind` has been a debt row since ADR 0196, pinned as "the compiled leg does not
compile" (`%p0` defined with type `double` but expected `i32`, **exit 2**). ADR 0253's store-once rule — ask a
float value for in the domain that stores it, and consult `allocd` before writing a second `%_t` — made that
module verify, and the pin broke in the direction nobody wants: the leg runs, prints `1` for `print(addf(1.0))`
where the oracle prints `2.5`, and exits 0. The row now pins both legs' stdout, because a debt that silently
changes shape is a debt that stops being measurable; the fix is the function's **return word**, chosen from what
the body does with the parameter rather than from the shape of the last line, which is L11.6's to make.


### Gap R.3c — paid beside ADR 0254: the return word is read from what the body does

```gusty
def addf(x):
    x = x + 1.5
    return x

print(addf(1.0))         # CPython 2.5 · --interp 2.5 · --aot 2.5  (was 1, exit 0)
```

Two measurements decided this one, and the second is the reason the ADR is not just a promotion rule.

The first is the obvious one: `funcReturnsFloat` asks `g.isFloat` of each `return` **expression**, and a bare
name is not evidence of a kind. So the return word was asked of the last line's syntax while the body — generated
afterwards, with the float-variable table filled by the assignments it walked past — already knew the parameter
held a double. Reading the body answered it, and the whole family came with it: the bare name, `return x * 2`,
`x % 3`, `x // 1`, `abs(x)`, `float(x)`, the local spelling `y = x + 0.5; return y`, each inside `if`, `while`,
`try`, recursion, defaults and a nested `def`.

The second measurement is why "just promote anything whose body stores a float into a parameter" was rejected.
The same question asked that way also promotes `return int(x)`, `return round(x)` and `return x > 2`, which
answer with an `i32` — and an `i32` written to a `double` `ret` is the module `llc` rejects. So the at-stake set
is a **statement shape** (`returnedFloatRebindings`: a name the body binds to a float, read back by a `return` as
a bare name, an arithmetic expression, a negation, a kind-preserving numeric builtin, or a ternary arm), and the
promote-or-refuse decision inside that set is the predicate's own answer, asked over a copy of the float-variable
table. One question, asked once, of the thing that will obey it.

Asking it that way also closed a defect that had been pinned as unfixable. `return -x` of such a parameter had
been an `llc` rejection since before this ADR's numbering — `isFloat` does answer a unary minus, but only once
the parameter is known to be a float, which happened too late to influence the header — and the module was
emitted `define double` ending in `ret i32` of a `fsub double 0.0`. The row that pinned it said it should fail
the day the emission learned; the emission learned, and the row is a parity row now.

What still cannot be carried is refused before the header is written, and the refusal is measured rather than
theoretical: forcing the promotion through a mixed signature yields `sitofp i32 @.lst1 to double` (a container
parameter the body reads) and `call void @rt_print_str(i32 %p1)` with `%p1` a `double` (an interned-text
parameter the body reads); a method has one convention for receiver and answer together and is emitted `i32`
whatever its body computes. Each names the variable whose double has nowhere to go, and each is exit 1 —
`assertNoForbiddenIR` and the CLI tables forbid exit 2 in every one of those rows.

<a id="gap-r-102"></a>
### Gap R.102 — the ternary arm of a rebound parameter (✅ CLOSED by ADR 0262)

```gusty
def f(x):
    x = x + 1.5
    return x if x > 2 else 0.0

print(f(1.0))            # CPython 2.5 · --interp 2.5 · --aot 2.5 (was: refused in words; before that 1, exit 0)
```

The row named one missing instruction — `select i1 %c, double %a, double %b` — and the instruction was legal LLVM
the whole time. What was missing was a predicate willing to say the arms were doubles. `isFloat` had no `CondExpr`
arm, so a ternary answered "not a float" whatever it held, and the two arms were handed to `value()`, whose float
case renders a double by its truncated integer. Every one of these came out of that single no, on the compiled leg,
with exit 0:

| program | CPython | compiled, before ADR 0262 |
|---|---|---|
| `print(1 if 0 else 2.5)` | `2.5` | `2` |
| `def f(x): return 1.5 if x > 2 else 2.5` → `f(1)` | `2.5` | `2` |
| `def f(x): return 1 if x > 2 else 0.0` → `f(1)` | `0.0` | `0` |
| `def f(x): x = x + 1.5` / `return x if x > 2 else 0.0` | `2.5` | refused in words (the row itself) |
| `print([1.5 if c > 0 else 2.5])` | `[2.5]` | `[2]` |

The fix is one question, asked in one place (`ternaryKind` in `pkg/lang/ternary_number.go`) and read by the four
doors that had been answering it separately: the renderer (`isFloat`, so `print`/`str()`/f-strings pick the float
formatter), the i32 lowering (`ternaryI32`), the double lowering (`ternaryDouble`, the select), and the gate that
picks a function's return word. Three consequences had to be chased, each one a door that asked the question in
its own way:

* **the return-word gate stopped at the wrapper** — `scanBareReturns` looked through a `*Call`, a `-x`, and a
  ternary's arms, but read only a bare `*Name` on the side of a product, so `return (x if x > 2 else 0.0) * 2` was
  never promoted and answered an i32 under a program that wanted a double. It now walks
  `namedNumericLeaves`, the same walker ADR 0254 introduced for `return abs(-x)`.
* **the container asked a literal where it should have asked an expression** — `literalNeedsTags` tested
  `isFloatLitExpr`, so a float-valued *ternary* in a list left the container unmarked; the builder stored the
  `@float_box` handle correctly and the printer then read that handle as the number it is (`[1]`). The sibling of
  ADR 0259's `boolSlotExpr`, `floatSlotExpr`, asks `isFloat` of the element.
* **the constant test is a different question from the run-time test** — `print(1 if 1 else 2.5)` and
  `print(1 if 0 else 2.5)` have different answers in CPython (`1` and `2.5`), so a ternary whose test is a value the
  source wrote takes the kind of the arm that *runs* (ADR 0261's `constantTestArm`), and the dead arm is not
  emitted at all — which is what the reference does with it too.

What remains beyond this row is the shape whose arms do **not** agree on a word: `1 if c else 0.0`, where which
arm runs is a run-time fact and neither an `i32` nor a `double` prints both answers correctly. That is refused in
words, with the tagged value word (L11.1) named — and its two non-numeric siblings are Gaps R.127 and R.128 below,
filed while measuring this one.

<a id="gap-r-103"></a>
### Gap R.103 — filed as the dict literal's, measured again as the container *return*'s (OPEN, owner Gap R.67)

```gusty
def f(x):
    x = x + 1.5
    return {"k": x}

print(f(1.0))            # CPython {'k': 2.5} · --interp {'k': 2.5} · --aot 0, exit 0
```

The row above is what this ID was filed as, and the diagnosis was wrong. Re-running the shape without the
function clears the dict literal completely:

```gusty
x = 2.5
print({"k": x})          # {'k': 2.5} on the interpreter, the compiled leg and CPython
k = 6.0
print({"k": k / 2})      # {'k': 3.0} on all three — the value is a computed double, and it prints as one
```

What the `0` needs is the **function around it**:

```gusty
def f():
    return {"k": 1}
print(f())               # CPython {'k': 1} · --interp {'k': 1} · --aot 0        ← exit 0, wrong

def g():
    return [1, 2]
print(g())               # CPython [1, 2] · --interp [1, 2] · --aot exit 2      ← `ret i32 @.lst1`
```

So the defect is a container crossing a function boundary as a *return* — the callee has one `i32` word, the
literal has no heap object to name, and the printer is handed a handle with no kind: **Gap R.67**, which has
carried this measurement since ADR 0204's round and owns the queue row. This ID stays, wording corrected, for
two reasons the roadmap's own rules give it: the misattribution is part of the record (a row written from one
program's symptom, filed before the control was run), and the control that clears the dict literal is
worth pinning where the next reader will look. The lesson is the general one: file the shape you *measured*,
and run the smaller program before naming the cause — the row here claimed a missing tag on a dict slot that
is written correctly.

<a id="gap-r-104"></a>
### Gap R.104 — `min` / `max` with two arguments (CLOSED, ADR 0256)

```gusty
print(min(1.0, 2), max(1, 2.5))   # CPython 1.0 2.5 · --interp was: min/max expects 1 argument · --aot 1.0 2.5
print(min(2.5, 1))                # CPython 1       · --interp was the same refusal            · --aot 1.0
print(min(1, 5))                  # CPython 1 5     · both engines refused
print(min([1, "a"]))              # CPython TypeError · --aot answered 0 with exit 0
```

The row was filed from one asymmetry — the interactive path refusing a spelling the compiled path answered —
and the sweep done on the way to the fix found that the asymmetry was the smallest of four answers to the same
question. The compiled leg had **one path for a single container and one float-domain path for several
values**: it promoted every candidate to `double` and selected a `double`, which is right for `max(1.0, 2.5)`
and wrong for `min(2.5, 1)`, because Python returns the winning *element*, so the answer's type is the
winner's. All-int varargs were refused outright, and a text, `None` or container candidate fell through to an
`icmp` over whatever the payload happened to be — an intern index or a heap slot — which is how an
uncomparable pair exited 0 with a number.

The rule that closes it is in ADR 0256: **a fold returns the candidate it chose, not the comparison that found
it**. Both engines now share it: the interpreter's `compareOrder(a, b, op)` is the comparator `sortElems`
already used with the operator threaded through it (the reference implementation puts the *failing operator*
in the message, so `min` reaches `'<'` and `max` reaches `'>'`, and the two kinds come out in the order the
fold met them — `min(None, 1)` names `'int' and 'NoneType'`, not the sorted pair), and the codegen classifies
the candidates before lowering them: compile-time fold, `icmp`/`select` over `i32`, `fcmp`/`select` over
`double`, or `rt_str_order` for text, with the winner's own kind deciding the printing. A comparison the
reference implementation cannot make is emitted as its `TypeError` — catchable, exit 3 — rather than refused
or answered, and the module carries one sentence per kind pair, with no operand the compiler invented.

What the cycle deliberately did **not** do is answer the shapes whose winner's kind is only known at run time.
That is the tagged value word (L11.1), and forcing the double domain there is precisely the wrong answer the
row started with; those four shapes are Gaps R.107–R.110 below, each with its refusal (or, in R.110's case,
its wrong answer) written into the row.

<a id="gap-r-107"></a>
### Gap R.107 — a runtime numeric container cannot be folded compiled (OPEN, measured landing ADR 0256)

```gusty
xs = [3, 1, 2]
print(min(xs), max(xs))   # CPython 1 3 · --interp 1 3 · --aot refuses: min requires an inline list/set/dict literal
```

The interpreter walks the elements; the compiled path only ever had a fold over an *inline* literal, whose
elements are globals it can read. A variable holding a list is a heap address: the elements are `(payload,
tag)` pairs the object can report (ADR 0246 gave the read side that walk), but no reduction over them exists.
The refusal names the missing thing rather than guessing, which is why this is a missing half and not a wrong
answer.

<a id="gap-r-108"></a>
### Gap R.108 — a runtime text container cannot be folded compiled (OPEN, measured landing ADR 0256)

```gusty
xs = ["b", "a"]
print(min(xs))   # CPython a · --interp a · --aot refuses the same message
```

Same walk, different arm: text is ordered by `rt_str_order` (ADR 0248 took the intern-index comparison out of
the module and put `strcmp` in its place), and the source-visible text fold already uses it. The runtime case
needs only the walk this gap shares with R.107, so the two are one helper with two comparison arms.

<a id="gap-r-109"></a>
### Gap R.109 — runtime int/double candidates: comparable, but the winner's kind cannot travel (OPEN, measured landing ADR 0256)

```gusty
a = 2.5
b = 1
print(min(a, b), max(a, b))   # CPython 1 2.5 · --interp 1 2.5 · --aot refuses: … the winner's own kind needs the tagged value word
```

The most interesting of the four, because the hard part is not the question. Two candidates, one `i32` and one
`double`, whose static kinds the module cannot settle: `fcmp` after converting the int decides *which* wins,
and a `select` can carry the value — but a `select` carries one **type**, and the answer has to be an `int`
sometimes and a `double` other times, depending on a comparison the compiler cannot evaluate. Choosing the
double domain unconditionally is what produced `min(2.5, 1)` → `1.0`, so the fold refuses and names the word
it is missing: the `(payload, tag)` pair that a subscripted slot already returns (ADR 0241) and that every
sink — print, arithmetic, a binding — knows how to ask.

<a id="gap-r-110"></a>
### Gap R.110 — a call site can settle the winner the body never saw (OPEN, measured landing ADR 0256)

```gusty
def choose(a, b):
    return max(a, b)

print(choose(2.0, 1))   # CPython 2.0 · --interp 2.0 · --aot 2
```

The one silently-wrong answer of the family, and the reason it is a wrong answer rather than a refusal is
informative: the body asks its parameters what they are, and ADR 0174's convention answers from the call sites
that settled them — so `max(a, b)` is emitted with the int word, prints `2`, and the program looks like it
worked. ADR 0254 fixed exactly this shape for a *number a function returns* by asking the emitted body;
the fold's version needs the same treatment plus the tagged pair from R.109, because for a fold the answer's
kind is chosen by data rather than written in the `return`. Pinned at the wrong answer, with the oracle's
answer beside it, so the day the pair arrives the row flips to parity instead of being rewritten.


### Gap R.111 — a verdict handed to a function loses its name on the way in (OPEN, measured landing ADR 0257)

```gusty
def show(f):
    print(f)

show(1 == 1)   # CPython True · --interp 1 · --aot 1
show(True)     # CPython True · --interp 1 · --aot 1
```

ADR 0257's predicate answers a *print site*: it looks at the expression in the argument position and says
whether that expression answers a question. Inside `show` there is no such expression any more — there is a
parameter, bound to whatever arrived, and the value that arrived is an immediate `0`/`1` with nothing
attached. Every engine in the repo that has ever tried to carry a kind across a call has hit the same wall:
Gap R.80 (a container argument prints `[1]`), ADR 0256's Gap R.110 (a fold's winner settled by a call site),
Gap R.67 (a container returned from a function). The closure is not a better question at the print site — it
is the `(payload, tag)` pair arriving with the argument, which is L11.1's tagged value word and nothing less.

Pinned twice so it cannot drift quietly: the probe program's row in the oracle ledger carries each leg's exact
`1\n1\n`, and the unit table pins the same answer with the gap named in the failure text.

### Gap R.112 — closed by ADR 0259: a verdict stored in a container says `True`

```gusty
print([True, 1])        # CPython [True, 1] · both engines [1, 1]
print({"k": True})      # CPython {'k': True} · both engines {'k': 1}
```

This is L11.1's written plan for bools — *give bool its own tag; one line of `elemKindTag` and every container
follows* — and it is the half of the plan that a print-side rule cannot reach. A container's slots are typed by
the element tag vocabulary ADR 0184 built (`int`, `str`, `list`, `dict`, `set`); there is no `bool` in it, so
the container printer reads the immediate the same way it reads an untagged int, and `TestSlotOrderOfABoolSlot
NamesIntUntilL11_2`'s `TypeError` says `'int'` for the identical reason. One entry in that vocabulary closes
both rows. What would *not* close them is a heap-object `ValueTag`: a bool is not an object, it allocates
nothing, and the readers of a heap tag — `gc.kinds`, the collector, `rt_print_mixed_value`'s object arms — are
not the readers that need the answer. The distinction is why the row's plan said `elemKindTag` and why the
print rule went a different way in ADR 0257 rather than waiting for a tag nothing would ask about.

### Gap R.113 — `except <Type> as e:` is not in the grammar (OPEN, measured probing bools through a `try`)

```gusty
try:
    x = 1 / 0
except ZeroDivisionError as err:
    print(err)          # CPython: division by zero
```

`gustyc: parse error at 3:26: expected ":"`, then `parse error at 4:5: unexpected token`, exit 1, on both paths.
The arms themselves are fine — `except ZeroDivisionError:` catches, `except Exception:` and a bare `except:`
catch anything, and the arms are tried in source order (ADR 0213) — but nothing can *name* the exception that
was caught, so its message is only reachable by printing a traceback. `docs/language.md` never claimed the
binding, so this is unimplemented surface rather than a regression; it is filed here because a bool cycle
reached for it while asking what `print(0 == None)` should look like inside a `try`, found nothing, and a
defect that is measured and not written down is the same as one that does not exist. No test yet: the closure
owes the program above running on three engines, and the test that pins today's parse failure comes with it.

### Gap R.116 — a set or dict comprehension over verdicts refuses (OPEN, measured closing Gap R.112)

```gusty
print([x for x in [True, 1, 1]])   # [True, 1, 1] on three engines — paid by ADR 0259
print({True for x in [1]})         # CPython {True} · --interp {True} · --aot refuses, exit 1
print({1: True for x in [1]})      # CPython {1: True} · --interp {1: True} · --aot refuses, exit 1
print({True: 1 for x in [1]})      # CPython {True: 1} · --interp {True: 1} · --aot refuses, exit 1
```

A list comprehension over verdicts was the last shape printing `[1, 1, 1]` after the literal rule was
fixed, and the reason is that the element *expression* is the loop variable: `x` says nothing about a
kind, and `comprehensionFolds` re-synthesised every folded element as an `IntLit`. ADR 0244 had already
drawn this line for `None`, float and text — `foldsToAnInteger` declines them so the runtime builder can
tag the slot — and `*BoolLit` was still on the declining arm's "folds to a number" side. Two changes
follow: the fold asks the **item** (`compElemCopiesABool`), and `runtimeCompList` takes the tag from the
item while the payload still comes from the slot. `[x for x in [True, 1] if x]` is the filtered twin.

The set and the dict cannot take that route yet, because their fold (`foldSetComp`, `foldDictComp`) emits
`@.set%d`/`@.dict%d` — a compile-time global of payloads with no `@heap_tags` row to write into. Before
ADR 0259 they answered `{1}`, `{1: 1}` and `{1: 1}`: Gap R.112's defect one container kind away, produced
by the code that was supposed to fix it. They now decline, and the refusal names the half that is missing
— `a comprehension of verdicts needs the tagged set builder, which a set comprehension does not have yet`
— while the interpreter prints all four lines. A refusal that says what it lacks is a row the next cycle
can start from; a number in a container that should say `True` is the row we just closed.

### Gap R.117 — closed by ADR 0261: the operator that chooses an operand renders what the chosen candidate is

```gusty
print(max([True, 0]))     # CPython True · --interp True · --aot was 1, now True
print(min([False, 1]))    # CPython False · --interp False · --aot was 0, now False
print(max(True, 0))       # CPython True · both engines used to print 1
print(max([True, 1.5]))   # CPython 1.5 · --aot used to print 1 — the double, truncated
ys = [True, 1]
print(ys[0] and ys[1])    # 1 on all three, before and after — CPython hands back the operand
```

ADR 0256 fixed the *choice*: a fold returns the candidate it chose, not the comparison that found it. What
it left open was the candidate's **kind**, and the fix turned out not to need the tagged value word after
all — for any candidate the source writes, the winner is decidable, and the answer was already in the
renderer’s own file. Three doors had to agree on one rule rather than each keep a copy:

* `minMaxCandidateValue` gives the number a candidate compares to — with `BoolLit` finally counting as the
  1/0 it has always behaved as (ADR 0259’s numeric family). That single omission is why `max([True, 1.5])`
  printed `1`: the fold declined the whole candidate list and the double fell through it.
* `numericWinner` is the one strict comparison. Four hand-written copies of it — the float fold, the int
  fold, `minMaxReturnsFloat`, `minMaxFoldedWinner` — collapsed into it, and a tripwire
  (`TestTheFoldAndTheVerdictQuestionChooseFromOneDoor`) fails the build if `vals[i] < vals[best]` reappears.
* `IsBoolExpr`’s `*Call` arm asks the *winner*. `print`, `str()`, `repr()`, an f-string, the container
  element tag and `--json` needed no change at all: they already ask that one predicate, which is why the
  whole rendering story moved with one arm.

The interpreter had its own half, invisible to the row as filed: `min(a, b, …)` evaluated its argument
expressions, and an argument written `True` arrived as the immediate `1`. Candidates now enter through
`slotVal`, the literal builder’s own door, so `print(max(True, 0))` prints `True` on both engines.

The tie rows are what no element-level rule can produce, and they are pinned both ways: `max([True, 1])`
is `True`, `max([1, True])` is `1`, `min([0, False])` is `0`, `max([False, 0])` is `False`. And the `and`
row has not moved — CPython hands that one back an operand *and* its number, which is the constraint that
kept the fix from being “print every chosen operand like a verdict”.

### Gap R.124 — a min/max candidate the compiler cannot read prints the number (OPEN, measured landing ADR 0261)

```gusty
i = 0
print(max([True, i]))     # CPython True · --interp True · --aot 1
xs = [0]
print(max([True, xs[0]])) # True on all three — the candidate the source wrote
```

The pair is the boundary, stated exactly. A candidate the source writes — a literal, or the element a
literal wrote into a slot — settles the comparison, so the renderer is told which candidate won and prints
its kind. A name the compiler has not folded is evaluated by the run-time `select`, which keeps a payload
and nothing else: the value is right, the kind is not there to be asked, and `1` walks out. This is the
compiled leg diverging from an interpreter and a CPython that agree, which is the class of defect this
project keeps coming back for; owner roadmap L11.1 with Gaps R.107–R.110, because the answer needs the tag
on the travelling value rather than a static reading of it. Refusing was considered and rejected: the
program prints the right *number* today, and a refusal would retire a working line to fix a rendering — the
row and its per-leg pin (`programs/probe_minmax_candidate_unreadable.gy`) keep it visible instead.

### Gap R.125 — a ternary whose test the compiler cannot read prints the number (OPEN, measured landing ADR 0261)

```gusty
c = True
print(max([True, 0]) if c else 2)   # CPython True · --interp True · --aot 1
xs = [1]
print(True if xs else 2)            # CPython True · --interp 1 · --aot 1
```

ADR 0261 gave the ternary the same rule as min/max where it is decidable: if the test is a value the source
wrote, the arm it selects is the arm that runs, and that arm decides what prints (`print(False if 1 else
2)` is `False`, `print(1 if 0 else False)` is `False`, `print(True if "" else 2)` is `2`). These two lines
are the rest of it. With a test that must be evaluated, neither arm is known to run, so the conservative
rule stands — a ternary is a verdict only when both arms are (ADR 0257) — and the number underneath prints.
Note the second line: the *interpreter* agrees with the compiled backend and neither agrees with CPython,
since the verdict is held in a variable whose kind the print site cannot see either. Owner roadmap L11.1:
the tag on the value is what closes both halves at once. Pinned per leg in
`programs/probe_ternary_the_test_chose.gy`, where the compiled leg is wrong on all four lines and the
interpreter on two.

### Gap R.118 — closed by ADR 0260: a dict puts its entries, however it is built

```gusty
d = {1: 2 for x in [1, 2]}
print(d)        # CPython {1: 2} · --aot {1: 2} · --interp {1: 2, 1: 2}
print(len(d))   # CPython 1     · --aot 1     · --interp 2
```

Nothing in the program is a verdict; this is not a bool defect wearing bool clothes, it is the
interpreter's `CompDict` path, found while asking why `{1: True for x in [1, 2]}` printed two entries.
A dict is a key → value mapping: writing a key that is already present replaces the value and leaves the
size alone — which is what `d[k] = v` does through `dictKeyEq`, and what the compiled fold does by
deduplicating keys as it unrolls. The comprehension loop appends an entry without asking, so the object
carries two slots under one key, both printed and both counted. It is the rare ledger row where the
**interpreter** is the diverging leg and the compiled backend is the reference, and it reproduces on the
commit before ADR 0259, so it is filed as a gap rather than treated as a regression.

What it turned out to be, once the neighbours were measured (ADR 0260): not a comprehension bug but the
interpreter's dict **builders**. The literal, the comprehension and the `dict(d)` copy each grew the entry
arrays with `append`; only item assignment asked the dict whether the key was already there. Four builders,
three rules, and one door now — `Evaluator.dictPut(o, key, val)`, which is the only place `dvals` grows:

```go
for i, k := range o.elems {
    if e.dictKeyEq(k, key) { o.dvals[i] = val; return }   // the entry keeps its place, takes the value
}
o.elems = append(o.elems, key); o.dvals = append(o.dvals, val)  // a new key extends the walk
```

The compiled backend needed no change: its fold deduplicates keys as it unrolls and `rt_dict_put_tagged`
updates in place. That is why this is the rare row where the interpreter is the diverging engine and the
compiled answer is the reference — and why the ledger pins are per-leg, so a "fix" that had moved the
*compiled* leg would have been caught. `TestEveryDictBuilderWalksTheOneDoor` is the structural half: the
behaviour rows would pass again the day someone writes a fifth builder that appends, which is how three
became four here.

### Gap R.120 — closed by ADR 0260: a dict literal with a repeated key is one entry

```gusty
print({"a": 1, "a": 2})      # CPython {'a': 2}   · --interp {'a': 1, 'a': 2}   · --aot {'a': 2}
print({1: "a", 1: "b"})      # CPython {1: 'b'}   · --interp {1: 'a', 1: 'b'}   · --aot {1: 'b'}
print({1: "a", True: "b"})   # CPython {1: 'b'}   · --interp {1: 'a', True: 'b'}
print({1.0: "a", 1: "b"})    # CPython {1.0: 'b'} · --interp {1.0: 'a', 1: 'b'}
```

Written the day Gap R.118 closed, because writing that probe meant writing the neighbours, and the literal
had the same defect. Two rules the door had to get right, both load-bearing because dict order is language
surface here too (`for k in d` walks entries in insertion order, ADR 0188):

* the entry keeps the **position of its first write** — `{"a": 1, "b": 2, "a": 3}` is `{'a': 3, 'b': 2}`,
  not `{'b': 2, 'a': 3}`;
* the **key that survives is the first one written** — `{1: 'a', True: 'b'}` prints `{1: 'b'}` while
  `{True: 1, 1: 2}` prints `{True: 2}`: value from the last write, key from the first.

Key equality is ADR 0259's `dictKeyEq` — a verdict unboxes, a float box is asked for its number — so `1`,
`True` and `1.0` are one key and all four collision spellings collapse to one entry. That dependency is why
this could not have landed a cycle earlier: with a verdict compared by handle, a `dictPut` on the old
equality would have merged some pairs and silently not others.

### Gap R.121 — dict display unpacking does not parse (OPEN, measured landing ADR 0260)

```gusty
d = {"a": 1}
print({**d, "a": 2})              # CPython {'a': 2} · both engines: parse error at 2:8: unexpected token
print({**{"a": 1}, **{"a": 2}})   # CPython {'a': 2} · parse error at 1:8: unexpected token
```

`**expr` is not in the dict display, so the question the feature actually asks — does an unpacked key
override a literal written before it, and where does the entry sit — has never been put to either backend.
It belongs with Gap R.58 / L12.6 (`f(**d)`, `*args`, `**kwargs`): one parser boundary, and nothing behind a
parser can implement a form the parser rejects. It is filed beside ADR 0260 rather than only there because
the answer it owes is this ADR's: unpacking an entry means putting it through `dictPut`, so `{**d, "a": 2}`
and `{"a": 1, **d}` differ exactly the way CPython says they do and in no other way.

### Gap R.122 — a dict comprehension cannot unpack a pair (OPEN, measured landing ADR 0260)

```gusty
print({k: v for k, v in [(1, 2)]})   # CPython {1: 2}
# --interp  NameError: name 'v' is not defined   (exit 3)
# --aot     error at 1:20: undefined name "v"     (exit 1)
```

The `for` *statement* unpacks a pair today — `for k, v in [(1, 2)]` prints `1 2` interpreted — through
`loopVarNames` handling a tuple target. A comprehension never reaches that door: its `ForVar` is a single
name, so `k` binds the whole pair and `v` is never bound at all, which is why the program dies with a
`NameError` about a variable it did declare. The compiled refusal (`undefined name "v"`) is the other half
of the same missing feature, and behind it `TupleLit` has no lowering at all (L11.3), so the iterable cannot
be built either. Owner L11.3 for that; the comprehension-target binding is the small fix that follows.

### Gap R.123 — a dict comprehension with a text key refuses in the compiled backend (OPEN, measured landing ADR 0260)

```gusty
d = {"k": v for v in [1, 2, 3]}
print(d)                # CPython {'k': 3} · --interp {'k': 3} · --aot comprehension key must be constant (exit 1)
print(d["k"], len(d))   # CPython 3 1      · --interp 3 1
```

Three neighbours, measured side by side, say where the edge is:

| program | interpreter / CPython | compiled |
|---|---|---|
| `{1: v for v in [1, 2, 3]}` | `{1: 3}` | `{1: 3}` |
| `{"k": v for v in [1, 2, 3]}` | `{'k': 3}` | **refused** |
| `{k: d[k] for k in d}` over `{"a": 1, "b": 2}` | `{'a': 1, 'b': 2}` | `{'a': 1, 'b': 2}` |

So the shape that fails is a key the *program writes as text* in a comprehension the compiler cannot fold
away: ADR 0234 lets a folded comprehension become the literal it denotes, and spelling an interned string
into that literal's key array is the one thing its builder cannot do — while a key **read** out of a
text-keyed dict comes through the tags ADR 0232 installed and compiles. This is not a wrong answer (exit 1,
in words naming the key it wants, nothing reaching `llc`), which is why it is a row rather than a fire, and
why the interpreter leg is asserted against CPython in the same test: the day someone opens the builder, the
answer is already pinned. Owner L11.7 — ADR 0232's `rt_dict_put_tagged` already takes a tagged string key for
item assignment, so the runtime comprehension builder owes that one argument.

### Gap R.119 — an ordering CPython refuses answers in the compiled backend inside a ternary (OPEN, pre-existing, found by the bool spelling)

```gusty
xs = [1]
print(1 if xs[0] > "a" else 0)
# CPython  TypeError: '>' not supported between instances of 'int' and 'str'  (exit 3)
# --interp  the same sentence, raised, exit 3
# --aot     1     (exit 0)
```

ADR 0250 and ADR 0252 built the arms that raise this: a comparison of a tagged slot against a text walks
one test per tag and raises CPython's own sentence naming the kind the slot really holds. Those arms are
reached from a `>` in an expression, an `if` head, a `while` head and an `and` compound — but not from a
`CondExpr`'s **condition**, where the compiled path still folds the comparison against a literal-backed
container and lets the ternary choose a branch. The trap never runs, and the program exits 0 with an
answer. The int spelling above is what the probe pins, because it reproduces on the commit before this
cycle: the bool spelling (`xs.append(True)` and the same comparison) is what made someone look, and ADR
0259 made the bool slot name itself in the sentence — see `TestSlotOrderOfABoolSlotNamesBool`, where both
engines now raise `'bool'` for the shape that is not folded. The oracle leg of the probe is
`not_applicable`, because CPython raises too and has no opinion to compare against; the honest exit is 7.

### Gap R.100 — closed by ADR 0255: the loop's increment needs a latch block, not a guess

```gusty
xs = []
xs.append(6)
print([v / 2 for v in xs])   # CPython [3.0] · --interp [3.0] · --aot [3.0]  (was exit 2)
```

The runtime comprehension wrote its induction as

```
comp.pre1:   br label %comp.cond2
comp.cond2:  %t3 = phi i32 [ 0, %comp.pre1 ], [ %cc2, %comp.body3 ]     ; ← the body, always
             …
comp.body3:  …the element…          ; `v / 2` emits `fcmp oeq … 0.0` + `br i1` here
fdiv.ok6:    …append…; %cc2 = add i32 %t3, 1;  br label %comp.cond2
```

and that entry list is a lie the moment the element branches: the block that actually jumps back to the
header is `fdiv.ok6`, not `comp.body3`, and `llc` says so — *PHI node entries do not match predecessors*,
exit 2, on the most ordinary program in the file. The filter path had never shown it because it already
owned a merge block: both arms of `br i1 …, keep, skip` end by branching to `skip`, so `skip` was already
the one predecessor the `phi` could name honestly. The unfiltered path had no such block and was naming the
block the body *starts* in.

The fix is therefore the filter's own shape, used always: the increment moves into a fresh `comp.merge`
block, every path the element can end on branches to it, and the `phi` names **that**. One predecessor, one
terminator, and the entry list says what the CFG does. This is ADR 0224's lesson (a `phi` must name the
predecessor the edge actually comes from) met one door over from where it was learned, and it was askable at
all only because ADR 0253 made a guard report the block it ends in: an emitter that hand-builds a loop has
to ask where its body finished, not assume.

What the row now pins instead of the rejection: nine element shapes on both engines (one element, two, the
slot as divisor, the filter keeping and dropping, a set comprehension, a `for` body that divides, a `for`
that appends the quotient, a non-trapping operator on the same loop), four raises that must be **raised**
with the pair's own `ZeroDivisionError` wording behind and in front of a filter, a catchable raise in a `for`
body, and an IR row that reads the module's own terminators to check the `phi`'s entry list — the check
`llc` makes, spelled so the failure names the loop. The row that had pinned the rejection printed its
instructions ("this row is the pin") and was deleted on the day it stopped failing.

<a id="gap-r-105"></a>
### Gap R.105 — the dict comprehension's value loses the double's tag (OPEN, measured closing Gap R.100)

```gusty
xs = []
xs.append(6)
print({v: v / 2 for v in xs})   # CPython {6: 3.0} · --interp {6: 3.0} · --aot {6: 3}
```

The list comprehension on the same loop got it right — `rt_append_tagged` is handed payload *and* tag, and
the printer renders `3.0` — while `rt_dict_put_tagged` is handed the key's tag honestly and the value's
absent, so the slot prints as the int 3. The pair is the whole story here (ADR 0238 gave a float element its
box, ADR 0243 taught a numeric read to ask for it, ADR 0232 made the object let each slot speak); this is the
dict's write side, one call away from Gap R.99's list-side row, and it is listed as a silently-wrong answer
rather than a refusal because exit 0 is the failure mode that fools people.

<a id="gap-r-106"></a>
### Gap R.106 — a comprehension loop variable whose slot holds text answers instead of raising (OPEN, measured closing Gap R.100)

```gusty
xs = []
xs.append("a")
print([v / 2 for v in xs])   # CPython TypeError: … 'str' and 'int' · --interp raises · --aot [0.0]
```

The same program's subscript spelling (`xs[0] / 2`) raises CPython's sentence with the slot's real kind in
it, because ADR 0253's door asks the tag. The loop variable does not go through that door: the container's
element kind is settled statically, so a text slot becomes an interned index and the index is divided — a
number, printed, exit 0. The `for`-statement spelling is wrong the same way (`for v in xs: print(v / 2)`
prints `0.0`), which is the sign that this is the loop-variable binding and not the comprehension: it wants
the object's **kind** where the read today asks only its tag, which is L11.1's own remaining sentence
rather than a patch at the comprehension.

<a id="gap-r-127"></a>
### Gap R.127 — a ternary whose arms are text prints the interned index (OPEN, measured landing ADR 0262)

```gusty
c = 1
print("a" if c > 0 else "b")      # CPython a · --interp a · --aot 0
print("x" if 1 else "y")          # CPython x · --interp x · --aot 2
```

ADR 0262 gave the ternary its number-word rule, and this is the same question asked of the other kinds an arm can
have. Text is an index into `@str_tab`; the compiled `select` chooses an index, and the printer is never told the
answer is text, so it renders the index as a number — `0` and `2`, the two interned slots, printed happily with
exit 0. The second line is the interesting one: its test is a constant, so this is not a question about which arm
runs (the compiler knows), and no branch analysis can be the missing piece. It is the tag: the value has to say it
is text, which is the wall L11.1 owns with Gaps R.107–R.110, and the reason ADR 0262's rule stops at numbers
rather than pretending to finish here.

Pinned per leg in `programs/probe_ternary_text_arms.gy`, because both backends agreeing with each other on the
interpreter leg is exactly what makes a two-engine matrix green while CPython prints `a`.

<a id="gap-r-128"></a>
### Gap R.128 — a ternary whose arms are containers is rejected by the assembler (OPEN, measured landing ADR 0262)

```gusty
c = 1
print([1, 2] if c > 0 else [3])   # CPython [1, 2] · --interp [1, 2] · --aot exit 2
```

The lowering asks `value()` for each arm, and a container literal's `value()` is the name of its global —
`select i1 %c, i32 @.lst1, i32 @.lst2` — and `llc` refuses a global in a value position. So an ordinary program
whose answer is one line ends as a toolchain rejection (exit 2), which ADR 0166 counts as the compiler's bug.

This is not a new family: it is `i32 @.N`-in-an-operand-position (Gap R.67's `ret i32 @.str1`, Gap J.6's stores and
calls) arriving through a construct that has never compiled. Every container builder has handed back a *built
handle* since ADR 0189, and the ternary's lowering is the one place still asking for the global's name. The DoD is
that — choose the object, not the symbol that names it — and the tagged value word makes the print side correct
once the handle is chosen at run time.

Recorded in `programs/probe_ternary_container_arms.gy`, with the compiled leg pinned as `Missing` and the
reference's own line in its header, so the ledger shows a rejection rather than quietly omitting the row.


<a id="gap-r-129"></a>
### Gap R.129 — a digit-count round of a value whose kind the module cannot see takes the i32 road (OPEN, measured landing ADR 0263)

```gusty
def scale(v):
    return round(v, 2)

print(scale(2.345))          # CPython 2.35 · --interp 2.35 · --aot 2
```

`round(x, ndigits)` answers with the kind `x` arrived as — that is half of what Gap R.69 was about — and
the compiled backend can only follow a kind the module can see. `v` is a parameter the call filled with a
double; nothing in the function's own text says so, so the call's i32 road is emitted and the truncation
happens where the argument was lowered rather than where the rounding was asked. The interpreter boxes the
value and asks it, and prints CPython's answer.

The row is deliberately **not** a refusal. The same call with a body that binds `v = v * 1.0` — the shape
ADR 0254 taught the return-word gate to read — answers `2.35` on both engines, so a refusal would retire
programs that work today (int call sites included) to replace a wrong number with an error message. What is
owed is the `(payload, tag)` pair travelling out of the call, which is L11.1's tagged value word and the
same wall as Gaps R.107–R.110.

Pinned per leg in `programs/probe_round_digit_count_kind_unseen.gy`, and in the two filed-not-fixed tables —
`pkg/lang/round_digits_test.go::TestADigitCountOfAValueTheModuleCannotSeeIsFiledNotFixed` and
`integration/round_digits_test.go::TestTheDigitCountProbeStillOwesWhatTheRoadmapSays` — which fail in the
other direction the day the answer arrives, at which point the row and the pin both get deleted.

<a id="gap-r-130"></a>
### Gap R.130 — a loop variable over a literal list of doubles has no kind at all to follow (OPEN, measured landing ADR 0263)

```gusty
for v in [1.5]:
    print(v * 2)     # CPython 3.0 · --interp 3.0 · --aot 0
    print(v + 1)     # CPython 2.5 · --interp 2.5 · --aot 1
    print(v / 2)     # CPython 0.75 · --interp 0.75 · --aot 0.0
```

Filed separately from Gap R.129, because it is a different wall and older than `round`. The compiled `for`
hands the loop variable the element's **handle** and nothing beside it says the slot holds a double, so `v * 2`
multiplies the handle, `v + 1` adds to it, and `v / 2` — true division, whose result kind is a float whatever
arrives — divides it. Exit 0, three number-shaped answers. ADR 0243 made the *element of a literal container*
readable when it is subscripted (`xs[0] * 2` answers `3.0` on both engines); the loop binding never got the
same pair, and ADR 0245's tagged loop variable exists for comprehensions over *tagged* containers only.

`round(v, 2)` inherits this, which is the second line of `programs/probe_round_digit_count_kind_unseen.gy`
printing `0` rather than `2.35`: the answer is the handle, and the digit count never got a chance to matter.

Pinned in `programs/probe_float_loop_variable_as_number.gy` with the reference's three answers in its header.
Owner: L11.1, and the shape to copy is ADR 0245's — bind the element from the object's tag, not from the
literal's spelling.

<a id="gap-r-131"></a>
### Gap R.131 — a built-in called with no argument: four still reach for `Args[0]` first (OPEN, measured landing ADR 0263)

```gusty
print(int())      # CPython 0     · --interp Go panic (index out of range), exit 2 · --aot exit 1
print(float())    # CPython 0.0   · --interp Go panic, exit 2                       · --aot exit 1
```

`int()` and `float()` are the conversions of zero, which is what the reference answers. The evaluator indexes
`n.Args[0]` before anything asks whether there is an argument, so the program dies with a Go runtime panic and
**exit 2** — the code the exit-code contract reserves for *the compiler is broken* (ADR 0166), spent on a typo.
The compiled backend refuses with `int expects one argument` and **exit 1**, the code for "your program has a
compile error", on a program CPython runs — L11.8's complaint in its purest form. `ord()` and `chr()` panic on
the same shape (programs CPython itself rejects, so exit 1/3 is owed there rather than an answer), and `chr()`
panics the compiler as well as the evaluator.

`round` is the closed half of this row and the template for the rest: one sentence
(`roundArityMessage`, `pkg/lang/round_digits.go`) written once and read by both backends — raised by the
evaluator, refused by codegen — exit 3 and exit 1, exit 2 nowhere. The two conversions of zero need CPython's
answers as well as the sentence, because zero arguments *is* a call they answer.

Recorded in `programs/probe_builtin_without_arguments.gy`, with the interpreter leg pinned as a panic and the
compiled leg as a refusal, plus an exit-code-class row in `integration/bool_element_test.go`: a panic that
turned into a clean exit 2, or an exit 2 that quietly became an answer, both move the pin.

<a id="gap-r-132"></a>
### Gap R.132 — a negative zero the compiler wrote has no sign (OPEN, measured landing ADR 0263)

```gusty
print(-0.0)             # CPython -0.0 · --interp -0.0 · --aot 0.0
y = -0.0
print(y)                # CPython -0.0 · --interp -0.0 · --aot 0.0
print(0.0 * -1)         # CPython -0.0 · --interp -0.0 · --aot -0.0
print(round(-0.5, 0))   # CPython -0.0 · --interp -0.0 · --aot -0.0
```

The boundary is the interesting part, and it is one instruction wide. Where the *target* computes the value
— a runtime `fmul`, the compiled `rt_round_digits` — the sign survives, because the runtime's float formatter
handles negative zero correctly. Where the *compiler* wrote it, the sign is gone, because the emitter
materialises a folded float constant with

```llvm
  %t1 = fadd double 0.0, -0.0e+00
```

— thirteen such sites in `pkg/lang/codegen.go` — and IEEE answers −0.0 + +0.0 with **+0.0**. The minus dies
before the module exists, and `rt_fmt_double`, which would have rendered it, is never handed anything.

This is ADR 0236's shape one more time: one rule with two implementers, and the two disagree. It arrived in
this cycle's own work — the digit-count fold was going to ship as `fadd double 0.0, <const>` too, which is
how `round(-0.5, 0)` would have printed `0.0` alongside a green DoD program — and the fold now hands back the
constant itself, because a constant is a value and does not need an instruction to become one. What is left
here is the other twelve sites, and the fix is the same one-line idea applied to all of them, with print,
`str()`, a binding and a container slot asserted.

Recorded in `programs/probe_negative_zero_constant.gy`, whose four lines are chosen as the boundary rather
than as a list of complaints: two lose the sign, two keep it, and the day all four keep it the row leaves the
ledger.

<a id="gap-r-133"></a>
### Gap R.133 — the whole number past the compiled `int` word: the guard raises, and the two engines disagree (OPEN, owner L12.12, measured landing ADR 0264)

```gusty
print(floor(2147483647.0))   # CPython 2147483647 · --interp 2147483647 · -aot 2147483647
print(ceil(-2147483648.0))   # CPython -2147483648 · --interp -2147483648 · -aot -2147483648
print(floor(2147483648.0))   # CPython 2147483648 · --interp 2147483648 · -aot OverflowError, exit 3
print(floor(-2147483649.0))  # CPython -2147483649 · --interp -2147483649 · -aot OverflowError, exit 3
print(ceil(2147483647.1))    # CPython 2147483648 · --interp 2147483648 · -aot OverflowError, exit 3
```

The compiled backend's `int` is an `i32` (L12.12 has owned that sentence since it was written), and
`fptosi double … to i32` outside the word is not a wrong number — it is *poison*: LLVM may answer anything,
and on x86-64 `cvttsd2si` hands back `0x80000000`, i.e. `-2147483648`, for any double too large to fit. So
`floorCeilValue` asks before it truncates — `fcmp uno` for NaN, `fcmp oge 2147483648.0`, `fcmp olt
-2147483648.0` — and raises a catchable `OverflowError` naming the row that owns the decision:

```
OverflowError: floor: the whole number is beyond the word this backend's int holds (roadmap L12.12)
```

The evaluator's ints are `int64`, so it answers CPython's number in every row above. **The two legs
disagree, and this row is where that is recorded** rather than being averaged: the compiled expectation was
not rewritten to `3000000000` (the compiler cannot deliver it), and the guard was not softened to print
something (a plausible wrong answer is the one no test catches, and Gap R.64 is already the ledger's record
of what silent overflow costs). The day the compiled leg answers the number, the probe
(`programs/probe_whole_number_beyond_the_int_word`) leaves the ledger and this row closes with L12.12's.

The boundary is the row's real content, and it nearly shipped broken. The lower bound was first written as
`-2147483649.0` with `fcmp olt`, on the reasoning "one past the minimum is surely the first value out". It
is not: the i32 holds −2147483648 … 2147483647, so `-2147483649.0` *is* out of range, `fcmp olt` does not
catch it, and it went into the poison `fptosi` and came back as **`2147483647`** — a positive number, from
a negative input, with exit 0. Walking the boundary found it; the middle of the range had nothing to say
either way. The upper bound keeps the asymmetric shape deliberately (`oge 2^31`, because 2^31−1 fits), and
both directions are asserted from both sides in `TestTheWholeNumberBuiltinsAnswerWholeNumbers` and the
filed-not-fixed table beside it.

<a id="gap-r-134"></a>
### Gap R.134 — a folded non-finite constant could not be emitted at all (CLOSED alongside Gap R.51 by ADR 0264, 2026-10-03)

```gusty
x = float("inf")        # CPython prints inf · used to die in llc: exit 2
print(x)
print(sqrt(float("inf")))   # CPython inf · the fold route to the same broken spelling
```

Two routes, one spelling, and the worst exit code in the contract. `floatConst` rendered any folded float by
taking Go's own text and gluing this emitter's exponent suffix on it; for the two values that have no
decimal spelling that produced `inf.0e+00` and `nan.0e+00`, which LLVM 20's parser has no token for:

```
llc-20: error: … :163:40: error: expected value token
  %t1 = call i8* @rt_fmt_double(double inf.0e+00)
                                       ^
```

**Exit 2** — ADR 0166's "the compiler is broken" class — on `x = float("inf")`, a two-line program the
reference prints, measured on the pre-change binary. It had simply never been tried: nothing in the corpus
bound an `inf` to a name, and `print(float("inf"))` goes through the printer's constant path and never
needs a double literal in the module, which is why it had always looked fine. The `sqrt` fold (`sqrt(inf)`
→ `inf`) was the second route, found by running the sweep this feature's own values asked for.

The fix is in the emitter, not at the call sites: `floatConst` writes the three values as their IEEE bit
patterns — `0x7FF0000000000000`, `0xFFF0000000000000`, `0x7FF8000000000000` — which is the spelling LLVM's
parser does accept, the same 64 bits the assembler would write, and free of the sign-loss that the
`fadd double 0.0, …` fold idiom carries (Gap R.132). `inf.0e+00` and `nan.0e+00` were added to the shared
`forbiddenIR` list in `pkg/lang/container_element_test.go`, so a module containing either is now rejected by
the package's own tests rather than by a linker hours later. `programs/non_finite_float_constant.gy` is the
regression net — nine lines, each of them CPython's bytes on both engines, and `sqrt(inf)` = `inf`,
`sqrt(nan)` = `nan`, `sqrt(-inf)` raising `ValueError: math domain error`.

<a id="gap-r-135"></a>
### Gap R.135 — a float with an exponent does not lex (OPEN, measured landing ADR 0264)

```gusty
print(1e18)        # CPython 1e+18   · --interp and -aot: parse error at 1:8: expected ")"  · exit 1
print(1.5e-3)      # CPython 0.0015  · same class of parse error                            · exit 1
print(2E8)         # CPython 2e+08   · same                                                 · exit 1
print(float("1e18"))   # 1e+18 — the string form parses and parses correctly, on both engines
```

The exponent marker is not in the number lexer, so `1e18` lexes as the number `1` followed by the *name*
`e18`, and the parser — still inside the `print(` — reports `expected ")"`. Both engines agree, which is
the only kind of agreement this row has: the reference evaluates all three lines, and the toolchain
refuses to build any of them. Exit 1 is the right class only for a program the reference rejects, and
CPython runs these, so until the lexer grows the form this is a divergence, pinned as
`programs/probe_float_literal_with_exponent` (`oracle: debt`, one pin per leg with the parse error, `ref`
this row).

It surfaced while choosing values for `sqrt`: the interesting square roots to test are `1e154`, `1e-3`,
`2E8`, and every one of them was a program that would not compile. A test-value choice that cannot be
written down is a signal about the lexer, not an instruction to write smaller tests. Until it lands, the
workaround is `float("1e18")`, whose argument is parsed by the conversion and is exact.

<a id="gap-r-136"></a>
### Gap R.136 — ten names the checker's own table advertises and neither engine can call (OPEN, owner L11.8, measured landing ADR 0264)

```gusty
print(pow(2, 3))            # CPython 8     · --interp NameError, exit 3 · -aot exit 1 `unsupported call "pow"`
print(divmod(7, 2))         # CPython (3, 1) · same two answers
print(hash(2))              # CPython 2      · same
print(callable(print))      # CPython True   · same
print(getattr([1], "x"))    # CPython <built-in method …> · same
print(fabs(-3.5))           # CPython has no fabs either (it is math.fabs) — the table promises it anyway
```

Walking `pkg/lang/predeclared.go` and calling every name in it — the sweep that found `floor`, `ceil` and
`sqrt` dead, in the cycle that implemented them — found ten more that are dead in the same way: **`map`,
`filter`, `isinstance`, `hash`, `id`, `getattr`, `callable`, `pow`, `divmod`, `fabs`**. The checker calls a
program that uses them well-typed, the evaluator traps `NameError` (exit 3), and codegen refuses with
`unsupported call "<name>"` (**exit 1**).

Two separate complaints, and the second is the one that matters for the contract. A capability gap is
L11.8's and is honest work: the name is missing, both paths say so. But `print(pow(2, 3))` is a program
CPython *evaluates*, and the compiled leg spends exit 1 — "your program has a compile error" — on it, which
is exactly the class ADR 0211 and L11.8 exist to close (a refusal is a divergence, never a diagnosis).
`fabs` is the mirror case: the reference has no such builtin, so the honest fix there is not an
implementation but a removal from a table that promises what the language does not have.

Pinned as `programs/probe_predeclared_name_not_callable` (`oracle: debt`, one line, `pow`, the sharpest of
the ten, with the interpreted `name 'pow' is not defined` and the compiled `unsupported call "pow"` pinned
leg by leg). The fix for each is the one ADR 0264 made for the other three names: lower it on both paths, or
take it out of the table so the checker stops promising it — and the sweep that found them is worth
re-running after any change to `predeclared.go`, because the table and the engines drift apart silently.

### Gap R.137 — the negation of a text answers a number on both engines (CLOSED by ADR 0266 on 2026-10-04; the same defect as Gap R.89, measured twice)

`print(-"hi")` and `x = "hi"` / `print(-x)` are stopped by CPython with
`TypeError: bad operand type for unary -: 'str'`. Both of this compiler's engines answer a **number** and
exit 0: `-281474976710658` interpreted, `0` compiled. Measured with `/tmp/r51/gustyc` (the pre-door
binary) so the numbers are not this commit's own output quoted back at itself.

The interpreted digits are the interned *index* of the text with its sign taken — the evaluator's unary
`-` is reached with whatever word the value carries and does not ask what the value is. The compiled
`0` is a folded negation of a value whose payload is a table index the folder could not read. Either way
the shape is the numeric road accepting an operand it has no meaning for, which is ADR 0166's own class:
a silent wrong answer at exit 0 is the single outcome the exit-code contract does not permit, worse than
the refusal this commit replaced.

Filed rather than fixed in the ADR 0265 commit: the door shipped there is a different feature, and a
silent-exit-0 row earns its own measurement, its own probes and its own commit. The way out is already
written — `@rt_kind_name` and `@rt_num_bad` name the operand's kind and raise the reference's sentence for
a slot that turns out to hold text — and the same table has to be reachable from the static numeric road,
where the compiler *does* know the operand is a text and can emit the raise directly. `probe_negated_text_slot`
is the ledger row; it becomes a `match` when both engines stop.

**Closed 2026-10-04 by ADR 0266**, which is the same commit roadmap booked as Gap R.89's: the two rows
described one defect from two directions — R.89 from the operator, R.137 from the door that had just landed
beside it — and closing them together is what the tracker's "one defect, one measurement" habit is for.
`probe_negated_text_slot` left the ledger with its pins (the interpreted leg's `-281474976710659` and the
compiled leg's `exit status 3`), and the shape is parity surface in `programs/negation_names_the_kind.gy`,
where the raise is *caught* and the three engines print the same bytes.

### Gap R.138 — the arithmetic the print position answers was refused one statement earlier (closed by ADR 0267, owner L11.1, measured landing ADR 0265)

```
xs = []
xs.append([7, 8])
n = xs[0][0] * 2
print(n)
```

CPython prints `14`; the interpreted leg prints `14`; the compiled leg spends exit 1 on
`index cannot reach into xs's slots`. One statement earlier — `print(xs[0][0] * 2)` — the same expression
prints `14` from the compiled leg since ADR 0265.

The difference is not the arithmetic, it is who asks. The pair road is opened in the print dispatch, where
a value and its tag are exactly what the mixed printer wants. An assignment asks the ordinary numeric road
instead, and that road still insists on a static kind before it will emit an `add`. So the door works where
the print dispatch reaches and stops one statement short of it, which is the definition of a half-lift and
the reason it is recorded as a row rather than quietly left as a surprise.

**Closed 2026-10-05 by ADR 0267.** The way out was the one already built: an assignment whose value is
arithmetic over a run-time-described slot now stores the pair the printer reads — `_n` and `_n_tag` — and
records in the symbol table that the name holds the pair, which is the shape the print door and
`arithOperandPair` already handle. The gate is the same program-wide proof; nothing about *which* programs
may take the road changed, only about who may ask. `programs/probe_arith_result_bound_to_a_name` left the
debt ledger with that and became a parity program — slot arithmetic bound to a name, a slot that holds a
float, a dict slot, a negative answer, and four rebindings of the same name — `match` on all three legs.

The close was not only the call. Three things the measurement did not predict came with it:

* the refusal the shape had been getting **blamed the wrong thing** — it cited a mixed-list loop and pointed
  at the container append two lines below the binding, because `lowerValue`'s old-index path saw `xs` as a
  plain name with no entry in the static-tables map. The refusal now asks `taggedVarErr` first, which names
  the binding itself. A refusal is a user-facing diagnosis too: pointing at the statement that would have
  fixed it is worth as much as naming the missing half.
* a name the pair road bound **kept its tag when the program rebound it** through an ordinary road, and the
  result was a wrong answer rather than a refusal — `n = xs[0][0] * 2` then `n = [1, 2]` printed `2`, the
  heap handle, and `n = 2.5` printed `0`, both at exit 0. That is Gap R.142, and it is why the binding is
  paired with `forgetTaggedBinding` at every road that stores a payload alone.
* the binding is a new **kind of binding**, and the roads that read names back as one static number know
  about none of the kinds: `print(n + 1)` refuses where `print(xs[0][0] * 2 + 1)` answers. That is Gap
  R.143, and it is the same sentence as L11.1 — a value carries its tag, and every position that reads the
  value has to read the tag with it. The reason it is a row and not a fix is that the plain-number road is
  asked from binary operators, unary operators, call arguments, conditions, format fields and augmented
  assignments, and each wants the same three-arm unbox-or-convert-or-raise that ADR 0249 built for an
  operand; that is a shared helper, not a patch at six call sites.

### Gap R.139 — the same slot read as an argument is refused (OPEN, owner L11.1, measured landing ADR 0265)

```
def twice(v):
    return v * 2

xs = []
xs.append([7, 8])
print(twice(xs[0][0]))
```

`14` from CPython, `14` from the interpreted leg, exit 1 from the compiled leg. One word short of Gap
R.138's fix: the caller has the pair in hand, and the parameter does not take it. A parameter whose
argument arrived as a pair has to be bound as a tagged parameter, so the body's `v * 2` sees the tag the
caller saw — which is L11.1's own sentence, quoted again at the fourth door in a row: the kind belongs to
the value, and every place a value crosses a boundary has to carry the tag across with it.

### Gap R.140 — `abs` never asks a tag either (OPEN, owner L11.1, measured landing ADR 0266)

```
print(abs("hi"))    # CPython TypeError: bad operand type for abs(): 'str'
                    # --interp hi   ·   --aot 0        (both exit 0)
```

Found by writing the negation sweep and asking whether the same road had other entrances: `abs` is spelled
differently and reaches the same numeric lowering, and it raises a sentence of its own —
`bad operand type for abs(): 'str'`, not the unary-minus one. Both engines answer, at exit 0, which is the
class ADR 0166 does not allow; the compiled leg prints the interned index's neighbour and the interpreted
leg prints the text itself, because its `abs` returns the argument unchanged for anything it cannot read as
a number.

The fix is the one ADR 0266 just made for `-`: name the operand's kind and raise it from the numeric road,
on both engines, catchably — `pkg/lang/negation.go`'s `negate` and `negationOperandKind` are the two doors,
and `abs` needs a third that asks the same question with the reference's own wording. Filed rather than
fixed with the negation because the sentence is different, the door is different, and one commit per feature
is the rule.

### Gap R.141 — a tuple's operand-type sentence names the representation, not the value (OPEN, owner L11.3, measured landing ADR 0266)

```
print(-("a", 1))    # CPython TypeError: bad operand type for unary -: 'tuple'
                    # --interp 'list'   ·   --aot 'tuple'      (both exit 3)
```

Both engines stop, which is the verdict the reference gives; they only disagree about the word inside the
quotes. A tuple literal is built as a *list* object by the interpreter, so `operandKind` reports the
representation it used. The compiled leg has a `Tuple` node in the AST and names `'tuple'`.

This is not the negation's debt — it is L11.3's ("Tuples are values, not syntax sugar") arriving in a
message. It is booked here because the row has to exist before the test can stop pinning it:
`TestTheNegationOfATupleNamesWhatTheReferenceNames` asserts the compiled leg against the reference and pins
the interpreted leg's `'list'` verbatim, so the day L11.3 lands, that pin fails with "paid" instead of the
row quietly agreeing.

### Gap R.142 — a tagged binding outlived the binding that gave it the tag (closed by ADR 0267, owner L11.1, measured landing ADR 0267)

```
xs = []
xs.append([7, 8])
n = xs[0][0] * 2
n = [1, 2]
print(n)          # CPython [1, 2]; the first build of the fix printed 2 — the heap handle
n = 2.5
print(n)          # CPython 2.5; the same build printed 0
```

Both at exit 0, with CPython and the interpreted leg agreeing beside them. The road that stored the pair
recorded the name as pair-bound; the roads that store a payload alone — a list literal, a float, a set, a
dict, a comprehension, a lambda, a call's parameter, a container the compiler folded into globals — stored
the value and left the record standing, so the next read took the pair road against a payload that was no
longer a number.

This was measured on the cycle's own first build, which is the class of bug the tracker's second question is
for: *was it already broken?* — yes, on the pre-cycle binary, and the fix is in the same commit as the row.
Every one of those roads now goes through `forgetTaggedBinding`, which retires the name's tag alongside the
other per-binding statuses the symbol table holds. `TestARebindingRetiresTheTagAtTheCLI`
asserts each rebind against CPython on both engines rather than pinning a number, and the rebinding rows are
in `programs/probe_arith_result_bound_to_a_name` so the conformance corpus walks them too.

The same omission, unseen, had been answering a rebound **loop variable** `(null)` since ADR 0185 bound it
with a tag — `for v in [1, 2]` then `v = [1, 2]` then `print(v)`. That is the second reason the retirement
belongs in one function rather than at each site: the sites are already nine, and the tenth will be written
by the next cycle.

### Gap R.143 — a pair-bound name is refused by every position that asks for one static number (OPEN, owner L11.1, measured landing ADR 0267)

```
xs = []
xs.append([7, 8])
n = xs[0][0] * 2
print(n + 1)        # CPython 15 — compiled: exit 1
print(-n)           # 15
print(abs(n))       # 15
print(bool(n))      # True
print(f"{n}")       # 14
print(str(n))       # 14
while n > 0:        # the head refuses too
    break
n += 1              # so does the augmented assignment
```

Every one is CPython's answer and the interpreted leg's, and the compiled leg spends exit 1 on the line.
The binding gave the name a payload and a tag; `print(n)` reads both, because the print dispatch is a mixed
door and has always taken the pair. The positions above ask the numeric road for **one** `i64`, and that
road knows only the bindings it made itself — what ADR 0249 called the three-arm decision, unbox-or-convert
-or-raise, which it builds for an *operand expression* and not for a name that already holds one.

The tag the name needs is provably `0` or `1` — the only road that ever writes a pair-bound tag is
`rt_num_arith`, which stores `0` or `1` and has already decided the operand's kind — so the arm can be
narrowed to unbox-or-convert, and the raise is the float path's. What makes it a row rather than a patch is
the number of doors: binary operator, unary operator, call argument, condition, `while` head, format field,
`str`, and the target of an augmented assignment each reach the numeric road separately, and all eight want
the same helper. ADR 0249's `rt_lift_num` is the helper; the fix is to ask it with the name's stored payload
and tag instead of rebuilding the operand from the container.

`TestAPairBoundNameRefusesThePositionsThePairDoesNotReach` pins the family with the reference's answer beside each
refusal, and `programs/probe_pair_bound_name_as_a_number` is the ledger's copy of the same shape.

### Gap R.144 — a tuple-unpacking target does not take the pair road (OPEN, owner L11.1, measured landing ADR 0267)

```
xs = []
xs.append([7, 8])
a, b = xs[0][0] + 1, xs[0][1] + 2
print(a)            # CPython 8 — compiled: exit 1, index cannot reach into xs's slots
```

The plain assignment has taken the pair since ADR 0267; the unpacking still binds each name through the
ordinary numeric road, which refuses what it cannot see into. The awkward part is that the unpacking is the
*easier* case in one respect and the harder one in another: it already builds each element as a (payload,
tag) pair in order to hand it to the tuple constructor, so the pair the binding needs is in hand — and it
binds the names in a different function from the one that owns the plain assignment, next to the for-loop
targets, which will want the same door for the same reason (`for v in xs` where `xs` was built at run time).
The fix is to walk the road once, from a helper both bindings call.

### Gap R.145 — an interned-text binding survives the rebinding that replaced it (OPEN, owner L11.3 with Gap Q.1's table, measured landing ADR 0267)

```
n = "text"
n = [1, 2]
print(n)            # CPython [1, 2] · --interp [1, 2] · --aot text, at exit 0
```

Not brought in by ADR 0267 — reproduced on the pre-cycle binary, which is what makes it a row and not a
regression report. The print dispatch reads the name's interned text before it looks at the heap object the
assignment stored, and the container binding does not clear the text record. It is the same omission as Gap
R.142 seen from the other status: a binding owns the *whole* description of the name it binds — payload,tag, interned text, `None`, bool, container kind — and any status a later binding does not retake is a lie
the next read will believe.

It belongs to Gap Q.1 ("One place decides what a name's status is") rather than to this cycle's door,
because the fix is a table, not a call: the set of statuses each road retires has to be written down once
and asserted, or every new status added to the symbol table will re-measure this. Until then
`TestAContainerRebindingRetiresTheTextBindingToo` pins the wrong answer rather than let it pass as parity,
and `TestAPairBoundNameRefusesThePositionsThePairDoesNotReachAtTheCLI` keeps the one row of the family that was already
wrong-answer territory out of the parity class.
