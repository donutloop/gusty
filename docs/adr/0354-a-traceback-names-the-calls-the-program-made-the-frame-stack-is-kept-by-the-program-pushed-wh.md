# ADR 0354 — A traceback names the calls the program made: the frame stack is kept by the program, pushed where the call is written

Status: accepted
Date: 2026-10-17
Amends: [ADR 0171](0171-traceback-frames-need-the-raise-s-span-and-the-aot-report-needs-a-frame-to-print.md), [ADR 0214](0214-a-trap-arrives-named-the-class-as-data-and-a-gate-that-invents-nothing.md), [ADR 0231](0231-debug-line-tables-in-the-module-dwarf-from-the-compiler-not-from-the-link.md)
Depends on: [ADR 0173](0173-a-referenced-internal-function-that-was-never-emitted-is-an-invalid-module-not-a-warning.md), [ADR 0231](0231-debug-line-tables-in-the-module-dwarf-from-the-compiler-not-from-the-link.md), [ADR 0302](0302-one-execution-backend-the-record-leg-and-the-reference-leg-are-the-two-witnesses.md), [ADR 0164](0164-a-claim-about-an-artifact-is-taken-from-the-artifact.md), [ADR 0166](0166-the-fold-proves-an-answer-the-ir-must-say-it-too.md), [ADR 0320](0320-a-rewritten-definition-is-still-the-function-the-program-wrote-the-name-a-tool-reads-is-the-source-name.md)
Roadmap row: Gap K.8 (the AOT frame stack L8.5 unblocked)

## The gap, as measured

```gusty
def inner(x):
    return x // 0

def outer(x):
    return inner(x)

outer(7)
```

The reference answers:

```
Traceback (most recent call last):
  File "prog", line 7, in <module>
  File "prog", line 5, in outer
  File "prog", line 2, in inner
ZeroDivisionError: integer division or modulo by zero
```

this toolchain answered two lines — the header and `File "prog", line 2, in inner` plus the class —
and left the reader to guess who called `inner`. A traceback that names one frame blames the wrong
line for a bug that lives in the interaction of three statements, which is the whole reason
tracebacks exist. ADR 0171 put the raise's own span on the report and the row stayed open for the
rest of the stack; ADR 0231 put a real line table in the module and wrote "Unblocks Gap K.8" in its
row. Both halves were in place. What was missing was the runtime half — a stack someone pushes to.

Measured against the record first, as AGENTS requires: `pkg/lang/testdata/interpreter-golden.json`
holds **no** frame for any of its sources (a scan for `File "prog"` over the artifact returns zero
entries). The record stores a trap's class, its message, its stdout and its front-end refusal — never
its history. So this feature has no record leg to satisfy, and under ADR 0302 the reference leg is
the definition: the frames, their order, their names and the recursion collapse all answer to
CPython.

## The decision

**The program keeps its own frame stack.** `pkg/lang/frame_stack.go` is that file, and it holds both
halves: the runtime block (`frameRuntimeIR`) — two parallel tables of 64 `(name, line)` slots, a
depth, a snapshot copy, and four doors (`rt_frame_push`, `rt_frame_pop`, `rt_frame_here`,
`rt_frames_snapshot`) — and the post-pass (`attachFrameStack`) that lays them over an assembled
module.

- Every call site to a program function pushes the callee's frame and, in the same store, records
  the call's own line as the *caller's* currently-executing line — which is exactly what the
  reference shows for a frame that is not where the raise happened.
- The call site pops when the call returns. Unconditionally.
- The raise site records its own line in the frame it stands in (`rt_frame_here`, fed from the
  span ADR 0171 added) and copies the live stack out; `rt_die` prints the copy, because by the time
  it runs every frame the exception travelled through has `ret`urned.

**Pop at the call site, not at the callee's exits.** This is why the instrumentation is a pass and
not a hunt through every `ret`-emitting road in the emitter. A call site that forgets its pop costs
one stale frame and never a corrupt answer; a pop missing from one of the emitter's many return
roads costs a frame per call for the rest of the run, and the roads are many.

**One door for the eleven call roads.** A `def`, a method, a `super()`, a constructor, the
dynamic-dispatch switch, a module function, a hoisted hook lambda, a closure `_impl`, a defunctionalised
clone's trampoline: eleven roads in `codegen.go` call a program function, and a feature whose
correctness depends on remembering all eleven ships a missing frame on the road nobody thought of.
The pass instead asks the module which defines are program code — the same `dbgDefine` records
ADR 0231 keeps for the line table — and instruments every call to one, whoever emitted it. The
`<module>` frame is opened the same way, by the pass, after `main`'s entry label (an instruction
between the `define` and its label is the parse error `llc` reports as exit 2).

**The gate is `raiseUsed`, and nothing else.** That flag is set by `setExn`, the one door every
raise goes through — the program's `raise` and the machine-detected index, key and conversion traps
alike. A program that can never report a trap ships the module it always shipped, byte for byte, IR
line numbers included; that is what keeps ADR 0166's fold-purity pins and ADR 0231's line-table rows
untouched, and `TestTheFrameStackIsNotPaidByAProgramThatCannotTrap` is the guard that says so.

**A frame is named the way the program wrote it, which is not how the module names it.** The name
comes from the same display-name record the line table uses, then loses what the reader never typed:
the closure pass's `_impl`/`_apply`, the callable pass's `$lambda$…`/`$callable$…` infix (ADR 0320's
rule, applied to the frame), and — the part the reference leg decided — the class qualifier. A
method's frame reads `in bump`, not `in Counter.bump`, because CPython prints `in bump`, and under
ADR 0302 a report that cannot be compared to the reference is a report that is wrong. The qualified
name is not lost: it is what the `DISubprogram` says, where a debugger is the audience (ADR 0231).

**The table is 64 frames, and it is a window.** A push into a full table slides the oldest frame
out; the newest frames are the ones a traceback is read for. And a run of identical frames is
printed three times and then counted, in the reference's own words:

```
  File "prog", line 4, in down
  File "prog", line 4, in down
  File "prog", line 4, in down
  [Previous line repeated 61 more times]
  File "prog", line 3, in down
```

The compaction happens in `rt_frames_snapshot`, not in the printer, because it is a fact about the
stack and not about its layout — and because a 200-deep recursion that prints 200 lines answers one
question 200 times.

**The line table moves with the insertion.** The pass inserts instructions into finished text, and
ADR 0231 resolves every `!dbg` record against byte offsets in that text. `attachFrameStack` returns
the marks *shifted* by what it inserted, in the order it inserted it; a pass that forgot would leave
the table intact and wrong — every statement after the first call attributed to the line before it
— which no verifier can see. `TestTheFrameStackKeepsTheLineTableOnTheRightLine` is the guard, and it
checks coverage of the *last* line of a program whose calls sit above it.

**The captions are generated.** A `getelementptr` states the array type it indexes, so a report
string's length appears in its declaration and at every use. That pair is how ADR 0171's broken
module happened — a hand-typed `[31 x i8]` about 32 bytes — so `frameStrGlobal` writes the
declarations and `frameCaptionRefs` substitutes the uses, both from one Go string, and
`raiseRuntimeIR` is now `raiseRuntimeIRTemplate` with those references filled in.

## The machine path

`--json` grows a `frames` member beside `traceback`: the same report the terminal saw, as data,
outermost first — `{"name": "bar", "line": 2, "col": 0}`. It is produced by `ParseTrapFrames`
reading the target's own bytes back, not by the host reconstructing a stack it cannot see, so the
`traceback` string and the `frames` array cannot tell two stories about one run (ADR 0214's rule for
the class and message, extended to the stack). `TrapError.Traceback` is filled from the same source
in the golden harness. The shape is `definitions.trapFrames`/`definitions.trapFrame` in
`gustyc --schema`, and `TestSchemaDeclaresTrapFrames` checks that every name `Frame` marshals under
is declared there. `col` is always 0: the runtime keeps lines, and a column the compiler does not
track would be a number the report invented.

## What was witnessed

- **Reference leg** — `integration/traceback_test.go`, eight programs (two calls deep, a method, an
  index through a helper, a recursion too shallow to collapse and one deep enough to, an escape
  after an earlier exception was handled, a failure inside a helper of a comprehension, and the
  module's own statement), each compiled through `llc` and `cc` and run, compared to CPython's
  report for the same source on: the frame sequence by name *and* line, the collapse sentence with
  its count, and the exception class. Nothing in that file is a hardcoded expectation.
- **Unit leg** — `pkg/lang/frame_stack_test.go`: push/pop balance read out of the shipped module
  (`frameStackBalance`, which counts the `<module>` frame as the one frame a program never closes),
  the zero-cost gate, the naming rule on the shapes the passes create, the pass's own safety rule
  about a call instruction whose operand list is still open, the constant-length rule, the parser of
  the report including what it must refuse to read as a frame, and the truncation-and-collapse
  behaviour at 120 frames.
- The record leg is asked and answered honestly: the record has no frames, so no case claims one
  from it, and no case asserts a frame the target did not print (`TestParseTrapFrames…` asserts the
  *absence* of invented frames on transcripts that contain headers and messages only).

## What was rejected

- **Reading the frames back out of the artifact** — ADR 0231's own hint, "walk what an unwinder can
  read". A native backtrace names `gy_outer` and the closure trampolines, not `outer`; the lifted
  closure and defunctionalisation passes have already rewritten which define a statement becomes, so
  the native stack belongs to a different program. A DWARF walk needs `--debug`, and a trap is
  precisely when a reader wants the frames whatever the build asked for.
- **Popping at the callee's returns.** Correct, and wrong the first time one of the emitter's return
  roads is missed; the call site is where the frame was opened, so the call site is where it closes.
- **Printing the live stack in `rt_die`.** It is empty by then — that is the entire nature of a
  compiled raise. The snapshot exists so the pop at the call site can be unconditional, which in
  turn is what keeps a *caught* exception from stealing frames from the report of the raise being
  handled.
- **Keeping `Counter.bump` in the frame.** More informative in isolation, unverifiable against the
  reference, and the reference is the definition (ADR 0302). The qualified name lives in the line
  table instead.
- **Dropping a frame whose line is unknown** (the first cut, which silently trimmed the innermost
  frames of a deep recursion). A missing frame is the bug this row exists to fix; a frame printed
  with a count instead of a line is not.
- **Filling `col` with the call's column.** The compiled runtime tracks lines; a column would be
  decoration that reads as evidence.
