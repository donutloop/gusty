# 0375 — The float-variable table a body reads is that body's own writes, not the last `define`'s

> Numbering note: **0374 is left unclaimed on purpose**. The applied-`lambda` text/verdict rows already
> in flight cite that number in `pkg/lang/boolvalue.go`, `pkg/lang/codegen_class.go` and
> `pkg/lang/codegen_facts.go`; taking it here would give one ADR number two decisions and hand the next
> reader a citation that points at the wrong file. 0374 is that cycle's to claim.

## Context

ADR 0373 settled *which* write a read answers: the **last** write a body makes to a name decides the
word its `return` of that name crosses in. It did not settle — and its own conformance program tripped
over the omission — ***whose*** writes the table holds at all.

`g.floatVars` answers 「whose last assignment in this body produced a double」. It is read by
`isFloat`'s `*Name` arm (`pkg/lang/codegen_float.go`), by the print door, by the comparison road, by
`logic_value`'s verdict census, by `heapargs` and by `list_index_kind` — every one of them asking about
a name in the body being lowered right now. The map was allocated once per module (`pkg/lang/codegen.go`)
and nothing cleared it when the next `define` began. Two bodies, one spelling, and the second one's
answer came from the first:

```python
def bound():
    u = 9.5
    return u

def lastWrite():
    u = 2.5
    u = 3          # LAST write: the integer 3
    return u

print(bound())       # 9.5      — right, for the wrong reason
print(lastWrite())   # CPython 3 · this toolchain 3.0, at the exit code of success
```

`def lastWrite()` in a file of its own answers `3`. That asymmetry is the diagnosis: the answer was read
off the emission order, not off the program. The defect was found by the **conformance matrix**, which is
the only witness in the suite that puts several bodies in one file — every other case asks one function
at a time, and a test that asks one function at a time cannot see a bug whose whole nature is what the
other function did. `float_answer_word.gy` carried this pair until the pair was cut out of it (the matrix
refuses to assert bytes it knows are wrong), and the row was filed as **Gap R.339** with the reference's
answer beside today's.

The loudest form, found while writing the witnesses below, is one function answering two ways in one
line of one program:

```python
def a():
    v = 1.5
    return v

def b():
    v = 7
    return v

print(b(), a(), b())   # was: 7.0 1.5 7.0 — the SAME call, three times, in one print
```

and the IR was wrong too, not just the rendering: `funcDef` chooses the **return word** by asking
`isFloat` of each `return` expression *through this table*, so the int-returning body was emitted
`define double @gy_b()` — a body returning a double it never computed.

The module already has the right mechanism, in the same file. `beginScope()` is where a body begins:
it installs the container, dict/set, None, **bool** and interned-text tables fresh, and restores the
enclosing frame's on the way out, with the comment 「which names are containers … are facts about a
single scope」. `floatVars` — and its companion `doubleSlot`, which says which of this body's slots were
allocated as a `double` — were the per-frame kind tables that helper never installed. ADR 0163 closed
this exact disease ("leaked per-body state") for the tables that were on the list; this is the sibling
that was left off it.

## Decision

1. **`pkg/lang/float_frame.go` is the rule, in one place.** `enterBodyFloatTable()` saves the frame's
   `floatVars`/`doubleSlot`, installs two fresh maps, and returns the undo. It is a copy-on-entry rather
   than the same map, because the enclosing frame keeps its own: a nested `define` emitted in the middle
   of a statement goes back to reading the frame that contains it, with the names *that* frame wrote
   still in place — Gap R.265's rule about a nested emission that used to take its frame's state away
   with it, now applied to the float table.
2. **`beginScope()` installs it.** The float table joins the bool, None, text and container tables it
   already installs, so the `def` road (`funcDef`) and the module's own frame (`main`) both begin with a
   table of their own. `funcDef` asks `funcReturnsFloat` / `returnedFloatRebindings` /
   `doubleReturnCarriable` *after* that point, so the word the `define` returns is decided from this
   body's writes and nobody else's.
3. **`enterBody()` installs it too.** The two roads that emit a body without calling `beginScope` —
   `emitClosureDef` (the closure lift's nested `def`) and `emitClassMethod` (a method) — go through
   `enterBody`, which is now the second caller of the same helper. Without this leg a method's
   `u = 9.5` still taught the next `def` that `u` was a double; the witness is
   `TestWhatFloatTableABodyReadsIsThatBodyOwnWrites`' method rows, measured on the pre-ADR build as
   `1 / 3.0` where CPython answers `1 / 3`.
4. **The rule does not seed the table from the module.** A body reading a *module-level* float is
   Gap R.35's question — 「may a body read module state that changes at all」 — and that row's refusal is
   what those programs get, unchanged by this ADR. Seeding would have been Gap R.35's work in this
   commit, and the two rows are kept apart on purpose: this one is about whose writes a body reads,
   that one about what a body may read. `TestTheFrameRuleIsNotAnAnswerToTheModuleStateQuestion` pins a
   Gap R.35 refusal row *inside this file* so the separation survives the next reader.

## Agentic rationale

An agent driving this toolchain compiles programs, not functions. It will put several bodies in one
file — that is what a program *is* — and it will read a wrong value as a fact about the language rather
than as an artifact of file layout. Two properties are being promised here:

* **Order-independence.** A program's answer does not depend on which `define` the compiler lowered
  first. The answer table asserts the same pair written in both orders, which is the machine-checkable
  form of that promise; an off-diagonal answer is now a test failure rather than a coincidence.
* **Self-describing refusal.** A refusal still names the row that owes the answer (`Gap R.35` in the
  module-state sentence, and the filed rows name `Gap R.341`/`Gap R.342`), so a consumer can route the
  failure instead of scraping prose.

The witnesses are deliberately three-legged: the compiled leg (`runIR`, `--jit`, `--aot`), the **record**
leg (`HasGoldenAnswer` + `goldenStdout` + `compiledRunIsTheBackend`, with a guard that fails if *none* of
the rows is on record), and the **reference** leg (CPython, asked in the same subtest as the compiled
run — not compared against the other compiled leg, which shares the very table that was the bug).

## Codegen / IR implications

* `TestTheFloatDefineAndTheIntDefineCoexistInOneModule` pins, in one module,
  `define double @gy_a()` beside `define i32 @gy_b()` beside `define double @gy_c()`, and asserts that an
  i32 body contains no `ret double`. That is the assertion that distinguishes this fix from a printer
  redirect: before, `@gy_b` opened `define double`.
* No new globals, no new declarations, no new `define`: the change is which entries an existing map holds
  at a given moment. The `pairTag` global, the float prologue's `alloca double` and the `%pN` parameter
  registers are untouched.
* `withFloatNames` (ADR 0373's copy-add-restore) now copies the *body's* table. The scan and the emission
  still read one table — which is ADR 0273's rule, and the reason the new helper is the only place that
  installs these two maps: a second writer would be a second opinion.
* Integration surface: `integration/programs/float_frame_table.gy` (new matrix row, `match`) and the
  `lastWrite` pair returning to `integration/programs/float_answer_word.gy`, the program it had been cut
  out of. Matrix: 214 rows, 182 parity-asserted, 168 oracle `match`, 0 failures, 0 drift. Record:
  7892 → **7907** sources.
* `TestWhatTheFloatAnswerWordStillOwesIsFiledNotFixed` is **deleted**, as its own rule required: both of
  its rows were Gap R.339 and both now answer the reference's bytes.

## Alternatives rejected

* **Clear `floatVars` at the top of `funcDef` only.** Leaves the method road and the closure lift leaking
  (both emit a body without `funcDef`), so it fixes the reported program and leaves the rule false. The
  method row in the answer table is exactly what that half-open version fails.
* **Seed the body's table from the module's float bindings.** That is Gap R.35's answer, arrived at from
  the wrong direction: it would silently widen what a body may read, turning today's honest refusals into
  answers nobody has cross-checked, and it would need the fold (`moduleFloatFold`) and the print door to
  agree on the same seeding rule before it could be trusted. Rejected as bundling a second row into this
  one.
* **A per-`*FuncDef` map computed by the pre-IR scan.** More machinery than the defect needs: the table is
  *incrementally* written by the assignment road as the body lowers (codegen_assign.go), which is what makes
  「last write wins」 work at all. A frozen scan answer would either disagree with the emission (ADR 0273's
  banned shape) or have to be recomputed per statement, which is the same map with extra steps.
* **Promote everything with a `double` in it, or refuse the ambiguous program.** Both were tested against the
  answer table and rejected by it: the promotion prints `3.0` for `3`, and the refusal would stop programs
  the reference runs to completion at exit 1 for a bug that is ours, not theirs.
* **Also fold `allocd` (「which slots exist」) into the helper.** Measured: the nested-`def` shape that looks
  like the same bug (`def outer(): def inner(): w = 3 / return w / w = 1.5 / return inner()`) dies in `llc`
  with `use of undefined value '%_w'` **before and after** this ADR — `beginScope` already resets `allocd`,
  and the closure-lift road is the one that does not call it. Bundling it would have made this commit two
  rules and hidden which one a passing test was proving. It is filed as **Gap R.342** with the exit-2
  pinned, and Gap R.341 (a `for`-driven float accumulator inside a body answering `0`) is filed beside it.

## Consequences

* Wrong values at exit 0 removed: `print(bound()) / print(lastWrite())` → `9.5 / 3`;
  `print(a(), b(), c())` → `1.5 7 2.0`; `print(b(), a(), b())` → `7 1.5 7`; a method's float local no
  longer decides the next `def`'s word; a promoted parameter (`def addf(x): x = x + 1.5`) no longer makes
  a later body's `x` a double.
* The rule is now stated once (`float_frame.go`) and has one writer per road, so the next leaked per-frame
  table has a home — and a name — to be added to.
* Two newly measured defects are **filed, not fixed**: **Gap R.341** (a `for`-driven float accumulator
  inside a body prints `0` where the reference prints `6.0`; the module-level twin answers correctly, and
  is the control row in the same program) and **Gap R.342** (a nested `def` sharing a name with its
  enclosing body's float local stores through the *enclosing* frame's alloca; `llc` rejects the module and
  the program leaves **exit 2**, ADR 0166's code for a compiler bug). Both are pinned with the reference's
  answer beside today's, in `pkg/lang/float_frame_test.go` and `integration/float_frame_test.go`, each
  guarded so it fails the day it stops being true.
* Gap R.35 keeps its refusals, and this file says so out loud: `RATE = 2.5` above
  `def scale(v): return v * RATE` still refuses with the module-state sentence, and `HALF = 10 / 4` above
  `def h(): return HALF * 2` still prints `0`. Those are not this row, and the test that proves the
  difference is in the same file as the fix.
