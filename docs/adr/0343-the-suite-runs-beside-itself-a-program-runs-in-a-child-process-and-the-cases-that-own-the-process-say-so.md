# The suite runs beside itself: a program runs in a child process, and the cases that own the process say so

Status: accepted
Date: 2026-10-14
Supersedes: the "the suite is deliberately serial" rationale in `tools/testshards/main.go` and
`docs/operations.md`, which this row replaces rather than contradicts — the reason those files gave for
running one case per process (the suite shares process state) is still true; what changed is that the state
has been moved out, one case at a time, and the residue is now named instead of assumed.

## Context

`make test` was 11 m 43 s of wall clock on a 20-core box (`go test -tags=llvm20 -count=1 ./...`, 2,246
top-level cases and 646 subtests). Nothing in it was allowed to overlap, and `tools/testshards` documented
why in its own header: the suite wrote process-wide state.

Measured, that state came in four kinds.

1. **A compiled program ran inside the test process.** `JIT`/`JITWithOptions` called `CreateMCJITCompilerOptions`
   with `UseOrcMCJITReplacement = false` — the legacy MCJIT engine — and captured a running program's stdout
   and stderr by re-pointing **fd 1 and fd 2** at a pipe, and the same engine loaded its object with
   `dlopen`/`dlmopen` into the process (a `RTLD_LOCAL`/`dlmopen` branch and a `benchSO` path). Two programs
   alive in one process are two programs both writing to the same terminal, both closing the same fd, and
   both handing their own `@heap`, `@str_pool` and intern tables to whatever the other one allocated. The
   reason the suite was serial was this one sentence.
2. **Per-run facts were process facts.** `lang.SetStdlibDir` and `lang.SetGCReport` are process-wide setters;
   the import resolver read a global stdlib root and `os.Getwd()`; `JITOptions` had no per-run directory,
   environment or stdlib root at all. A case that imported `math` from a fixture tree and a case that
   compiled `import os` from a temp directory could not both be right.
3. **Package-level caches without a lock.** `bodyBindingCache` in `strargs.go` was a bare map read by every
   codegen run; `pairClosedOut` in `paircall.go` was a package variable written by `pairCallSpecs` and read
   later by codegen; `tools/recmerge` retargeted its `recordPath` package variable per test;
   `integration/build_test.go`'s `writeSrc` wrote `filepath.Join(dir, name)` where `name` was the test case's
   label — and two labels in one table share a name.
4. **Cases that genuinely own the process.** `t.Setenv` (Go 1.22 panics if called after `t.Parallel()`), the
   toolchain overrides that point `llc`/`opt`/`objdump` at a stub, and the in-process entry itself.

The serial suite hid a fifth thing, which is what the last one bought us.

## Decision

**One compiled program runs in a child process, and that is the whole execution model.** `pkg/lang/run_child.go`
links the module to an executable (`linkSOOrExecutable(..., exePath, ...)`, `chmod 0755`) and runs it as a
subprocess with its own `Stdout`/`Stderr` pipes, a context deadline from the tool budget, and an inherited-or
overridden environment. The in-process engine is no longer the default: it stays reachable through
`JITOptions{InProcess: true}` for the one case that exists to test it, and `linkSOOrExecutable` keeps the
`-shared` shape it needs (`-Wl,-u,pyre_main`, no `-e main`, no `--gc-sections`) because a shared object has no
entry point of its own and `--gc-sections` removes the global the child would otherwise call. A death by
signal is reported as the compiler's own bug and not the program's, in the words ADR 0302's retired-engine
contract already required: *"the compiled program was killed by segmentation fault — it never returned, so
this is the artifact, not the program."*

**A run's facts travel with the run.** `CompileRun` gained `Dir`, `StdlibDir`, `GCReport`, and `irOptions()`
returns `nil` — meaning "the shared defaults, read under the stdlib lock" — when none of them are set, so the
ordinary path is untouched. `JITOptions` gained `Dir`, `Env`, `Stdin`, `Budget`, `NoBudget`, `StdlibDir`,
`GCReport` and the private `wantsIR()`. `moduleSearch` is now a value threaded through `resolveImports(prog,
paths)`, `moduleSearchFor(opts)`, `stdlibSearchPath(run)` and `ResolveImportPathWith`; the process-wide
standard-library root survives only behind `stdlibMu` (an `RWMutex`) so a set-once default cannot race. The GC
report is `gcReportWanted(opts)` — a per-run request, not a global switch — and codegen reads `g.gcReport`,
never the global. The test-side doors are `RunSnippetWith`, `RunSourceWith`, `CompileWith`, and in
`golden.go` the `…With` family (`runRecordedWith`, `goldenRunWith`, `evalGoldenWith`, `goldenStdoutWith`,
`goldenRunErrorWith`), so a fixture is a parameter and not an appointment with the process.

**Shared package state becomes a locked door, a returned value, or nothing.** `bodyBindingCache` is guarded by
`bodyBindingMu`; `pairClosedOut` is gone and `pairCallScan` returns `(specs, closed)`; `tools/recmerge` takes
its target as a `runAt(target, …)` argument instead of retargeting `recordPath`; `writeSrc` puts each table
row in its own leaf directory (`caseLeaf`, name plus a hash of the full test name) because rows in one table
can share a label. The ledgers that stay package-level (`gapLedger`, `debtSeen`, `ledger`) are all written
under their own mutex, which is what the analyser is taught to recognise.

**Parallelism is a checked property, not a style.** `tools/parify` is the tool: it parses every package
(production files included — a case that writes a variable declared in `main.go` is the case that must not
overlap), decides each `Test*` and each `t.Run` subtest, inserts `t.Parallel()` with `-apply`, and with
`-check` fails the build on a case that claims parallelism while its body reaches process state through any
chain of callees, or that sits unmarked and serial. A `//parify:serial <reason>` comment is a promise to run
alone and must carry a reason; a case that carries the marker *and* calls `t.Parallel()` is itself a failure.
`integration/parallelism_guard_test.go` runs the documented command — `go run ./tools/parify -check -quiet
./pkg/... ./cmd/... ./integration/... ./tools/...` — inside the suite, so the thing a person types is the
thing that breaks the build, and the failure message quotes the census line rather than swallowing it.

Twelve cases are marked serial, and each marker says what it owns: three `t.Setenv` cases (the `GUSTY_PYTHON`
override, the tool budgets, the `JITOptions{InProcess: true}` agreement case) and nine toolchain-override
cases that write the package's `llc`/`opt`/`objdump` command or the CLI's standard-library root, which is the
same act by another name.

## Consequences

The suite runs in **about 1 m 40 s** on the same 20-core box, against 11 m 43 s serial — a 7× cut, with
`-parallel 20`. Per-package wall time is dominated by the cases that compile and link for real, so the floor
is `cc`/`llc`, not Go. Sharding is no longer the faster way to run it on a big box (2 m 09 s against 1 m 40 s on 20 cores) — a shard
was the isolation, and the isolation moved into the child process — but it stays the command that gives each
process its own ledger evidence and its own `-timeout`, which is why CI still runs it.

Three defects surfaced that the serial suite could not see, which is the argument for the change more than the
speed is:

- **A shared source file.** Two rows of `TestBuiltArtifactRunsForReal` are both labelled `simple`, and
  `writeSrc` wrote both to `dir/simple.gy` — one row's program ran under the other's name, and the assertion
  passed because both answers happened to be right.
- **A ledger row pinned to unstable bytes.** `match` with an unbound value-pattern prints an object address,
  and the compiled address differs from the reference's; the `unstable` flag now says "pin the shape of the
  answer, not the answer", so a row that prints heap garbage per run stops being a coin flip.
- **Gap R.258.** `x = sorted([3, 1, 2], reverse=True)` followed by `x` **dies by SIGSEGV in `@rt_alloc`**, while
  `print(sorted([3, 1, 2]))` answers. Under the in-process engine the crash was invisible: the record leg's answer
  came back empty and the run went on. It is a real codegen bug — the artifact crashes standalone (`gustyc
  --file`, `--build` + execute) — and it became a drift row and a roadmap row rather than a flake.
  *What was written here first, and corrected the same cycle:* "the slot holding a list bound from a call is
  written with the tag word of a pair". Nothing emits such a store. The slot was never written at all —
  `hoistAllocas` (ADR 0181) moves each variable's `alloca` to the top of its function so the root stack sees one
  address per call, and the release-the-old-binding prologue reads that slot before the binding has run, taking
  the previous call's frame word as a handle, feeding it to `rt_free`, and letting the next `rt_alloc` walk off
  the arena. Alone the frame word was 0 and a release of 0 is skipped, which is the entire reason a serial suite
  could not reproduce it. The pass now gives the slots it moves a defined value
  (`pkg/lang/hoisted_slot_init_test.go`), the drift row was deleted rather than updated, and the correction is in
  the roadmap record too — a wrong diagnosis in a record is worse than a missing one.

**A shared hook in the compiler, found by `-race` and fixed by making it a value.** The first
`go test -race -parallel 20 ./...` came back with four reports on one address: `(*irGen).installPowerReaders`
writing the package-level `kindOfIntValue` function variable while another goroutine's
`readIntLiteral` read it. The power predicates read a NAME the program bound to a literal int (`z = 0` /
`print(z ** -1)` is the same program to the reference as `print(0 ** -1)`), and the reader was installed —
a closure over the asking generator's module constants, guarded by a "wired once per generator" bit. With two
generators alive that is a data race on a function pointer; with one it is still last-writer-wins, so a compile
could be answered from another module's constants while its own comment promised the reader was scoped to it.
It is now `powerFacts{kindOf, readInt}`, a value `(*irGen).powerReaders()` builds per ask and the predicates
take as a parameter. `TestThePowerPredicatesReadTheProgramThroughAValueTheyAreGiven` fails if the hook or the
installer returns, and `TestCompilesAnswerTheirOwnPowerQuestionsUnderOverlap` compiles a batch of power programs
alone and then concurrently and requires every answer to be the answer it gave alone. The tree is
**race-detector-clean** across `./pkg/... ./cmd/... ./integration/... ./tools/...` (`make testrace`, 4m35s).

**The toolchain was process state too, and it was lying in the same way.** `llcCmd`, `ccCmd`, `optCmd` and
`dwarfdumpCmd` were package variables, and the only way to run a build against a different binary was to write
them — which is how, in the first overlapping runs of this suite, `import math` came to report
`invalid module: my favourite complaint`: the sentence belonging to a stub `llc` that a toolchain-error case had
installed while an unrelated case was compiling. The same mechanism made a golden-record program "could not be
run". The override is a value now: `ToolOverride{LLC, CC, Opt, DWARFDump}` on `JITOptions` and `BuildOptions`,
resolved once per run by `toolNamesFor` and carried by every step that execs a binary — the optimizer
(`optimizeIRReportWith`), the verifier (`verifyModuleIRWith`), `llc`, `cc`, and the `llvm-dwarfdump` behind
`--debug`. An empty field means "the default", and absence (the optimizer that was never discovered) is a
separate, explicit question: `toolNamesWithoutOpt`. Nine cases that had to own the process stopped needing to,
which the census says: **2878 parallel / 12 marked serial became 2892 parallel / 4**, and then — once the
environment knobs (`GUSTY_PYTHON`, `GUSTY_TOOL_TIMEOUT`) became functions of an environment they are handed
rather than `t.Setenv` cases, with `tools/envprobe` to ask the process-wide entry points end to end — **2902
parallel / 1 marked serial**. The one survivor is the parity case that asks for `JITOptions{InProcess: true}`,
which takes fd 1 and fd 2 by definition. The target was never zero markers; it was a marker for every case that
genuinely owns the process and no marker for any other.

The change also caught a regression of its own making, which is worth recording: lifting `pairClosedOut` into
`pairCallScan`'s return value initially dropped the closed-position set on the exit where no function ends up
paired. `TestAPairAnswerHeldByAPositionThatKeepsOneWordStillRefuses` failed immediately — `z = twice(y)` printed
the truncated word `8` instead of refusing — and `TestTheClosedPositionSetSurvivesAnEmptySpecMap` now pins both
halves of the scan's answer, and fails when the fix is reverted.

## Witness

- `tools/parify/main_test.go` — the analyser's own contract, in the house's naming: a case that touches nothing
  runs beside the others; a file already marked is left alone; the process-wide acts refuse and say why; a case
  that edits a package variable runs alone; a subtest that writes its parent's fixture keeps its siblings serial;
  a marked case is left alone with its reason; the tool ignores what is not a test; the tool is idempotent;
  insertion lands inside the brace; the shapes that broke the first version (a one-line `func() { os.Exit(1) }`,
  an `IndexExpr` root, a `range :=`, a labelled statement, a `defer` inside the body, an `if` without braces) all
  parse and decide; a variable declared outside the test file is still a variable; a local that shadows a package
  variable is not that variable; and a case that says parallel and takes the process anyway is a failure. The last
  one asserts both directions, so "already has `t.Parallel()`" buys nothing.
- `integration/parallelism_guard_test.go` — `TestTheSuiteIsMarkedToRunBesideItself` fails if the documented
  command fails, fails if the tree stops mentioning parallel execution (the failure quotes the census line), and
  fails without the tag that the tests need to exist at all.
- `pkg/lang/run_child.go`, `pkg/lang/jit_llvm.go` — the child path is what every run test in the tree exercises;
  `TestTheIsolatedRunAndTheInProcessEntryAgree` is the case that pins the two paths answering the same, and it is
  marked serial because it is the case that takes the process.
- `pkg/lang/pair_call_test.go` — `TestTheClosedPositionSetSurvivesAnEmptySpecMap`, whose teeth were checked by
  reverting the fix and watching it fail on the closed-set row.
- `pkg/lang/tool_override_test.go` — the toolchain as a value, from both sides: `TestAnOverrideReplacesOnlyTheToolItNames`
  (resolution: one field named changes one binary, `nil` is the whole pinned set, an empty field is the default
  and not an absence), `TestARunUnderAFailingStubDoesNotTakeTheNextRunDownWithIt` (a run under a failing stub
  beside a run on the real toolchain, six rounds, asserted concurrently so `-race` is the witness),
  `TestTheToolchainNamedByOneRunIsNotTheOneAnotherRunReads` (a stub `cc` that logs its own invocation to a shared
  file: **one** line means only the run that named it used it — a claim about which binary executed, not about
  which one a report mentions), and `TestTheBuildCarriesItsOwnToolchain` (the `--build` path, where the failure of
  a build under a bad `cc` must not be the build beside it).
- `pkg/lang/testdata/interpreter-golden-drift.json` — 341 rows, the last being Gap R.258 with the record's answer
  (`result "[3, 2, 1]"`) beside the compiled one (`trapped: exit 139`).
- `integration/parallelism_guard_test.go::TestTheParallelCasesActuallyOverlap` — the claim that belongs to this ADR
  and not to a census line: four heavy `pkg/lang` families (each subtest compiling, linking and running a program
  as a child) are run through `go test -parallel 20 -json`, their subtest durations are summed from the JSON, and
  the sum must exceed twice the wall clock the run occupied. On the machine this was written, **207 cases, 45.6 s of
  case time in 3.0 s of wall — 15×**. A suite that is marked parallel and still queues behind a lock prints the same
  census and takes the same twelve minutes; only the measurement tells them apart.
- The same test then runs that **identical subset again with `-parallel 1`** and compares the two wall clocks:
  **the same 207 cases took 36.3 s one at a time and 3.0 s beside each other — 11.9× on 20 cores**. Overlap is
  the mechanism; speedup is the point, and a suite can overlap its cases and still buy nothing if the overlap is
  spent queueing on something. The comparison is skipped below four cores, where there is nothing to gain and a
  failure would be noise. Which is the rule
  `tools/testshards` has always applied to its shards and now applies to the cases too. The four case names are a
  fixed list, and the test fails if any of them reports no subtest, so a rename cannot turn the measurement into a
  run of nothing that passes by vacuity.

## Alternatives rejected

- **`t.Parallel()` everywhere with no analysis, and `//nolint` for the failures.** The suite would have gone
  green by deleting assertions. The analyser exists so that "parallel" is a claim with a witness.
- **Keep the in-process engine and wrap each run in a global mutex.** That preserves 11 m 43 s of serial time in
  the hottest part of the suite, and it keeps two programs sharing one fd table and one heap in the case where
  the mutex is forgotten. Isolating the process is the fix; the lock was the thing we'd have written instead.
- **Run the whole suite in N shards instead (`tools/testshards`).** Sharding parallelises *packages*; it cannot
  help `pkg/lang`, which is one package and 1,427 cases, and it multiplies the per-process setup the ADR removes
  rather than the per-case serialisation that causes it. Shards stay useful for the artifact-writing modes, which
  must still run one process (`GUSTY_GOLDEN_UPDATE` writes one ledger).
- **Refactor `JIT` to always dlopen the shared object and `dlsym` `pyre_main`.** This keeps everything in-process
  (one runtime, no `cc` link step) but cannot separate the fd capture, the `@heap` globals, or a `SIGSEGV` from a
  test failure — the three things this ADR is actually about. The `InProcess` path keeps the shape for the one
  case that needs it.
- **A `TestMain` that runs each case in a subprocess via a re-exec protocol.** Heavier than a per-run link-and-run
  (which the compiler already does), and it would have made every run test pay a Go-side handshake on top of the
  `cc` call instead of replacing one with the other.
- **Teaching `parify` to be conservative and mark everything serial.** With `-check` wired into the suite that
  would have been green on day one and worthless by day ten: twelve markers with sentences attached are the
  reviewable form of the same information.
