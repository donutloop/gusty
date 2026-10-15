# ADR 0355 — A `print` statement writes one line, and writes it after every argument has been evaluated

Status: accepted
Date: 2026-10-15
Amends: [ADR 0186](0186-cpython-is-the-conformance-oracle.md) (the row this closes was found by that leg and mis-described by the docs for its whole life)
Depends on: [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md), [ADR 0164](0164-verification-is-a-pipeline-stage.md), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md), [ADR 0257](0257-a-verdict-is-printed-from-the-expression-that-made-it-not-from-the-slot-that-holds-it.md), [ADR 0269](0269-and-and-or-hand-back-the-operand-the-test-chose.md), [ADR 0273](0273-a-slot-read-handed-to-a-function-crosses-the-call-as-a-pair.md), [ADR 0317](0317-a-guard-that-does-not-read-its-own-file-is-a-guard-that-never-ran.md), [ADR 0258](0258-the-rendering-pair-writes-to-a-switchable-sink-so-print-str-and-repr-are-one-table.md), [ADR 0204](0204-a-programs-stdout-is-only-what-it-prints.md)
Roadmap row: Gap L.5

## The gap, as measured

```gusty
def twice(v):
    print("<<", v, ">>")
    return v + v

print("got", twice(21))
```

The reference answers:

```
<< 21 >>
got 42
```

this toolchain answered `got << 21 >>` then `42`. The caller's separator and first argument were
already on the stream when the callee ran and printed its own line, so the callee's bytes landed
in the middle of the caller's. One statement, five `printf` calls, and the callee's call between
two of them: the print lowering emitted the separator, then each argument's render, then the
terminator, in the order it happened to emit them — and an argument that prints of its own accord
printed *while* the line was being written.

`docs/language.md` § print described this as the language's behaviour ("an argument that prints
keeps its place in the line … both backends interleave it identically"), and two tests asserted
it: `integration/print_semantics_test.go` carried the row `got << 21 >>\n42\n` under that name, and
`integration/lang_test.go::TestExecStringInClassMethod` asserted `r inside method\n42\n` for a
method that prints. Both were written when there were two engines and both legs agreed, which is
the exact blind spot ADR 0186 was written for. The row's own ID names the leg that found it: Gap
L.5 was discovered by the CPython oracle leg on that leg's first run, and stayed open for nine
rounds because the fix had no shape anyone trusted.

Measured against the record first, as AGENTS requires. `pkg/lang/testdata/interpreter-golden.json`
**cannot see this bug**: `goldenRepr` flattens a rendered line — it drops empty components and trims
trailing spaces — so a record compares a line's *contents* and not the instant its bytes were
written. Sorting the print statements before flattening would have destroyed the very thing the
row is about, and a *missing* record fails a case, so the record could not be edited into agreement
either. What the record leg was allowed to do is stay exactly where it was: the drift ledger
(`pkg/lang/testdata/interpreter-golden-drift.json`) regenerated **byte-identical** after the change,
which is the bar this house sets — *nothing else moved*. The witness here is the reference leg, and
the standard the rewrite is held to is stricter than byte equality: **where a line's bytes come from
inside the statement is allowed to change; *whether they appear at all*, and in what order, is not**
— a line must never lose a write, and never gain one.

## The decision

**Do not move the language. Move the statement's writes, and only where the move is provably the
same execution.** `pkg/lang/print_atomic.go` is two pure functions over the IR text of one print
statement, called from the print lowering at the instant the statement is complete:

```go
if rewritten, moved, _ := deferPrintLineWrites(b.String(), argMarks); moved { … }
```

1. **`printWriteSinks` / `printWriteLine`** names, as a whitelist of eighteen entries and nowhere
   else, the calls that write to the program's one text stream: `@printf`, the two `rt_out_*` writers,
   the six scalar-and-tag printers (`none`, `bool`, `str`, `value`, `mixed_value`, `container_value`)
   and the three container families — list, dict and set, three spellings each (plain, `_str`,
   `_mixed`). The list is a whitelist on purpose, and a line's callee is read from after its last
   `call` keyword rather than at its first `@`, because a print line's operands name globals of their
   own (`@.fmt3`, `@.fmtdictkeys`) and the first `@` is not the callee — the decoys in the classifier
   table (`@rt_print_list_lengths`, `load … @rt_print_list_count`) are there because a naive name test
   reads those as writes. Two families are deliberately **not** writes: `@rt_die`, whose report belongs
   to the raise happening right now and goes to stderr, and `@rt_echo_*`, which is the REPL's channel
   and not the program's stdout (ADR 0204). The doors the renderers are built from —
   `rt_str_of_value`, `rt_str_ptr`, `rt_fmt_double`, `rt_bool_text`, `rt_str_intern` — are
   *evaluations*: they compute text and never put it anywhere. Anything not on the list stays where the
   emitter put it, so a printer added later without a line here costs itself the interleaving it already
   had — the state of the world before this row — rather than losing a line or moving a call past the
   code that raised.
2. **`printWriteDeferralReason`** is the gate. It refuses the rewrite — five named reasons — unless
   every write of an argument sits in the straight-line run *after that argument's last block
   boundary*, where a boundary is a label or a terminator, and no write's returned register is
   mentioned anywhere else in the statement.
3. **`deferPrintLineWrites`** does the move: each argument's chunk is kept whole, its writes are
   lifted out of it, and the writes are re-emitted at the end of the argument region in their
   original relative order, with the chunk's non-write lines staying exactly where they were. The
   terminator and the function epilogue are outside the region the marks describe, so they cannot be
   reordered at all, and an unwind path reached from inside an argument's evaluation is outside every
   mark and is left where it was generated.

**Why the straight-line tail is the sound domain.** A write in a chunk's trailing straight-line run
is a write in the block the emitter stands in when the argument is finished. The block the statement
finishes in — where the writes are re-emitted — is a descendant of that block on a single
straight-line path, so the write executes **exactly when the line is printed at all**, and the
dominance relation the value uses is untouched. A write anywhere *earlier* means the argument's own
lowering branched: the write is then conditional on a path the statement's tail is not, and moving
it would make it execute on paths that never take the branch that printed it — or, the other way,
delete a write from a path that did. Those are the shapes the gate exists to refuse, and refusing
them is not a failure of the fix: an honest interleaving is a wrong *ordering*; a line that
sometimes goes missing is a wrong *program*.

**Why a textual pass and not a value phase.** The roadmap's original wording — "evaluate all
arguments (+ `sep`/`end`) to values, then emit the line as one unit" — is right about the semantics
and wrong about the system. `emitText(sep)` is one line of code; **argument** emission is a
~1400-line `switch` with ~20 `return` roads and ~1500 appends into the same builder. "Evaluate
first" means a second pass over that switch that returns *values* instead of writing, run before the
printing pass — either two paths through 1400 lines that must agree, or a line buffer inside the
runtime that every printer would have to know about. That code does not exist, and the road that
needed it would have drifted from the print road it duplicated within two cycles. The rewrite is
confined to the print statement and to one pure file; the print lowering changed by four statements,
and the rest of the emitter does not know this row happened.

**Why the rewrite is allowed to reorder at all, when reordering is usually forbidden here.** ADR 0166
and ADR 0273 forbid *inventing* an ordering the source never stated. This rewrite claims nothing new:
the semantics it implements were always the language's, and were stated by the reference, which was
asked before the emitter was touched — measured on the row's own probe and on nine shapes covering
`sep`, `end`, a container argument, two printing arguments, an argument that is itself a `print`,
a print inside an `if` branch, and nesting (`/tmp/probe5/shapes.gy` in the cycle's notes, committed
as part of the answer table in `pkg/lang/print_atomic_test.go`). The pass does not decide *whether*
the line is a unit; it emits what the language already promised, once the statement is complete.
That is also why the pass is textual and idempotent rather than an AST feature: it is applied to one
statement's emitted text once, at the point of that statement's emission, and nothing downstream can
see it happened.

## What the gate still refuses, in words

The gate answers in words, and the answer is the diagnostic — a refusal that names no reason is an
ADR 0166 violation even when the answer is right. Five refusals, each asserted as a function over
hand-built IR in `TestTheGateIsAskedAsAFunction`, and the two no-ops the rewrite returns itself:

| the answer | shape | why |
|---|---|---|
| `the statement has no arguments` | `print()` | not this pass's business — the terminator alone is already one unit |
| `an argument's emitted region is not inside the statement` | a mark outside the region | the marks came from somewhere that is not this statement; refuse the whole idea |
| `the arguments did not emit in order` | marks that go backwards | the same |
| `an argument's write returns a register the statement reads` | `%t3 = call i32 @printf(…)`, whose `%t3` is used later | that write is an **evaluation wearing a write**; moving it moves a value's producer |
| `an argument writes inside a block its own lowering opened` | a label, `br`, `switch` or `invoke` sits before the write inside the argument's region | the write is **conditional** and the statement's tail is not — the case below. An `invoke` is a boundary for the same reason: its unwind edge is a second successor the straight-line reasoning does not cover |
| `the statement writes nothing` | an argument with no sink in it | nothing to move |
| `the line is already written after every argument` | a straight-line statement whose emitter never interleaved | returned byte for byte, which is what lets every other IR-shape guard in the package keep certifying the module it certified |

The fifth row is a real language shape and it stays interleaved, filed-not-fixed in
`TestWhatTheRewriteStillCannotReachIsFiledNotFixed`:

```gusty
x: int | str = 1
def p():
    print("S")
    return 2

print("a", x, p())
```

CPython writes `S` then `a 1 2`. This toolchain still writes `a 1 S` then `2`, and the reason is
visible in the module: printing `x` is the union box's *tagged arm* printer, so the write for `"a "`
and the text sits in `sbr4`/`ibr5`/`fbr1` and only the `join3` merge block is straight-line. Deferring
that arm's writes to the tail would print them on the arm that did **not** run. What the row owes is
the union box's printer writing its line from the merge block instead of from each arm — L11.1's
tagged value word, not this row. The gate's own wording (`"an argument writes inside a block its own
lowering opened"`) and the compiled answer are both asserted, and the test fails if the divergence
ever *disappears* unannounced, which is the same promotion rule the conformance ledger uses.

## The machine path

No flag, no exit code, no schema changed: `print` was already one statement and the IR stays textual.
What an agent can read is unchanged in shape and better in truth —

- `--emit-llvm` shows the statement's writes after the whole argument region, and shows the refused
  shape showing them inside an arm block, so the gate's reach is inspectable rather than documented;
- `integration/conformance-matrix.json` moved two rows from `debt` to `match` — `programs/print_args`
  (the row that opened the debt list) and `programs/probe_print_atomic` (the probe ADR 0186 found).
  Both are **deleted from `oracleLedger` and promoted into `conformanceStandalone()`**, which is that
  ledger's definition of done: a paid debt left on the list fails the build with *"oracle debt is
  paid … update the registry"*, so the closure is enforced by the harness and not by prose. The
  artifact's own counters moved 148 → 150 `match` and 28 → 26 `debt`, 163 → 164 parity-asserted, and
  `pkg/lang/roadmap_snapshot_test.go` reads those numbers back out of the file (ADR 0164, Gap R.204).

## Alternatives rejected

- **"Evaluate every argument into a value first, then print" as written in the roadmap cell.**
  Requires a value-returning twin of the 1400-line argument switch that no other road uses; two
  implementations of one lowering that must agree forever, for a reordering. Rejected on the size of
  the duplicated surface, not on the semantics — the cell's semantics are this ADR's.
- **A runtime line buffer.** ADR 0258's rendering pair already writes to a switchable sink — `@rt_cap`
  with `@rt_cap_len` and the `@rt_capturing` flag, "one global and one branch in two functions", which
  is what makes `print`, `str()` and `repr()` one table. Pointing `print` at that buffer and flushing it
  at the terminator looked free, and is not: the buffer has **one** length, and a print statement is not
  the only thing that can be mid-line when something else writes. An argument's call runs a *nested*
  print, whose flush would write the outer statement's half-written line too, and `str()` resets the
  same length to 0 the moment it starts. Making that protocol sound needs a stack of marks, an ownership
  rule for a mark when an exception unwinds past one, and a reason to trust a depth counter after a trap
  raised inside an argument — pieces of runtime state a program gets wrong by losing or duplicating a
  line at exit 0. Rejected also because it puts the ordering where no leg can witness it: the IR would no
  longer say when a write reaches the stream, and `--emit-llvm` would stop being evidence.
- **Moving writes unconditionally.** Unsound exactly where the gate fires: a write moved out of a
  conditional arm prints on paths that never took the arm, and a write left behind on a moved block
  disappears from a path that did. A line that is sometimes missing is a worse bug than a line whose
  bytes interleave, and it is invisible to a record that flattens lines.
- **Re-deriving the golden record with the print statements sorted before `goldenRepr` flattens them.**
  It would have made the record agree with the new code by making the record unable to see ordering at
  all — self-certification, which ADR 0302 forbids (the record is evidence, not the definition), and
  ADR 0164's "a claim about an artifact is taken from the artifact". Instead both ledgers were
  **regenerated and diffed against what was committed**: `pkg/lang`'s came back identical (339 rows, none
  new, none vanished), and `integration`'s gained two rows — the two programs whose *record* was the
  interleaved line, filed as divergences from the record and owned by this row, because the reference
  agrees with the new answer and the record does not.
- **Keeping the two contract tests as witnesses of the bug.** `TestPrintSeparationParity`'s row and
  `TestExecStringInClassMethod` asserted the interleaving because both witness legs had it — one backend,
  and the record the retired engine left behind, which is exactly why a leg-vs-leg comparison could not
  find it. They are CPython's expectations now. Leaving them would have made the suite the thing that had
  to be lied to.
- **Sorting by sink *rank*** (all the plain `printf`s, then all the container printers). Rejected
  because the order between an argument's writes and another's is the order of the writes in the
  source: an argument's own rendering may itself be several writes (`@rt_print_list` opens with
  `[` and closes with `]` in separate calls), and ranking sinks across arguments would reorder the
  pieces of one value's rendering.

## What was witnessed

- `pkg/lang/print_atomic_test.go` is the row's evidence, 12 tests:
  `printAtomicRows` — **25 programs**, each run twice, once against a remembered answer
  (`TestAPrintStatementWritesItsLineAfterEveryArgument`) and once against `python3` run on the same
  source (`TestAPrintStatementAgreesWithTheReferenceOnBothLegs`, where a `python3` that cannot run the
  program is a failure and never a skip): texts and numbers, a comprehension argument, a method call,
  `sorted`, a slice, a tagged slot, a dict entry, a callee that loops, several statements in a row,
  a printing argument as the only argument, and two calls of which the second prints.
  **5 rows** are the trap half (`TestAPrintStatementThatNeverReachesItsLineWritesNothing`): a
  statement whose later argument raises must write **nothing**, so no half-line reaches the stream
  ahead of the traceback — the old emission wrote the leading arguments first and printed a line the
  reference never prints; and the converse, that a line begun after a caught trap still writes itself.
  **3 rows** are the record leg, run through `runGoldenStdout`, and they name themselves as the cases
  where the record and the reference disagree and the reference is the language (ADR 0302).
  **The structural claim is read out of the module**, not inferred from stdout
  (`TestTheLineIsWrittenAfterTheArgumentsInTheModuleToo`): every write the statement emits sits after
  the last call it makes to a program function, and the statement still emits exactly four writes and
  four `printf`s — a rewrite that dropped one, or moved one under the callee, fails it.
  **8 gate rows** (`TestTheGateIsAskedAsAFunction`) ask `printWriteDeferralReason` directly over
  hand-built IR text in both directions — a straight-line list answers `""`, and each unsound shape
  answers its own sentence; **2 more** keep the no-op honest (`TestTheRewriteDeclinesWhenThereIsNothingToMove`
  returns the region byte for byte, which is what lets every other IR-shape guard in the package keep
  certifying the module it always certified); **one preservation row**
  (`TestTheRewritePreservesTheStatement`) asserts each original line survives exactly once, that no
  evaluation follows a write, that the separator's text does not overtake the answer's, and that the
  region head did not travel. **The classifier is tested as a classifier** — 10 write lines, 8
  block/terminator lines, 14 lines that are neither, including the five renderer doors that *compute*
  text and must never move, `@rt_die` and the two echo calls, and two decoys whose names merely begin
  with a sink's (`@rt_print_list_lengths`, `@rt_print_list_count`). And a byte-for-byte substring test
  would read `%t1` inside `%t10`, so `TestARegisterMentionIsAWholeRegister` pins whole-register mention
  over 9 lines — the guard on the guard, without which the "the statement reads the write's answer"
  refusal fires on any statement with eleven `printf`s and the row silently stops reordering anything.
- Nine further shapes were measured against `python3` at the CLI before the gate was widened, covering
  `sep=","`, `end="!\n"`, a container argument, two printing arguments, an argument that *is* a whole
  `print` statement, a printing argument inside an `if` branch, and nesting of both: identical bytes.
- The probe (`integration/programs/probe_print_atomic.gy`) and `print_args` are `match` rows with
  `aot_stdout == python_stdout`; both ledger rows are deleted; the artifact regenerated clean
  (`oracle_drift: 0`, `fail: 0`).
- **The two record legs moved differently, and the ADR says so.** `pkg/lang`'s drift ledger
  (`pkg/lang/testdata/interpreter-golden-drift.json`) regenerated **byte for byte** the committed file —
  339 divergences, none new, none vanished (`git status` reports it unmodified after the regeneration).
  `integration`'s ledger gained **exactly two rows**, 22 → 24, and they are the interesting ones: both
  programs are ones where the record holds the *interleaved* line and CPython agrees with the new
  compiled answer — `print("got", twice(21))` (record `got << 21 >>\n42\n`, reference and compiled
  `<< 21 >>\ngot 42\n`) and the whole `print_args` program, whose last line is the same statement. That
  is ADR 0302's rule applied to this row: the record is evidence, and where it disagrees with the
  reference the record is the debt. The rows carry `expected` (what the retired engine wrote) beside
  `actual` (what the compiled backend writes, which is CPython's line), and `docs/operations.md`'s
  artifact table now says so. The regeneration also corrected one **stale `actual` at HEAD**: the
  `class P` / `int("abc")` / `a, b = [1]` row quoted `jit: codegen: int on non-integer string`, while a
  HEAD binary built in a clean worktree and the working binary both refuse that program with
  `tuple assignment length mismatch` — a ledger field nobody had re-derived, not a change this row made.
  The count guard in `docs/operations.md` moved with it (22 → 24).
- The filed shape is measured, not asserted from memory: compiled `a 1 S\n2\n`, reference
  `S\na 1 2\n`, and the arm-block writes visible in `--emit-llvm` (`%t16` in `fbr1`, `%t20` in `sbr4`,
  `%t23` in `ibr5`, and the separator and the callee's call alone in the `join3` merge block).
