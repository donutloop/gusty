# 0313. The suite runs sharded: one test process per core

Status: accepted. Roadmap: `Gap R.195` (the witness legs are subprocess-bound, so a serial suite is a
single-core suite), `Phase 0` (hygiene — CI must fit its budget), `Phase 3` (tooling: the harness is a
first-class, self-describing command). Continues: ADR 0302 (one backend, two witness legs — the legs are
what the suite pays for, and they are processes), ADR 0308 (the witness vocabulary: a shard is still a
run, and the drift ledger judges only what a run asked about), ADR 0312 (the same CI log, hang half).

## Context

`go test ./pkg/...` runs a package's cases **serially**: one binary, one case at a time, and
`t.Parallel` is the only lever the framework offers (measured below, it is not usable here). The suite is
`pkg/lang`'s 1244 top-level tests and 2187 subtests, and nearly every one of them ends inside a
subprocess, because that is what the two witness legs are:

| what a case waits for | process | measured |
|---|---|---|
| the record leg compiling it (`RunSource` → `llc` → `cc -shared` → `dlopen`) | `llc-20` | **58 ms** |
| | `cc` | 13 ms |
| | `dlopen` + `main` | < 1 ms |
| the compiled leg executing the module (`runIR`) | `llvm-as-20` | 10 ms |
| | `lli-20` | **57 ms** |
| the reference leg | `python3` | ~40 ms |
| the case's own Go work (lex + parse + analyse + codegen + optimise) | — | 1 ms |

Call counts from one instrumented run: 1818 record-leg programs and 1177 compiled-leg programs — about
6000 subprocesses to answer 3000 questions. The Go side of the suite is noise: 1 ms against ~140 ms of
toolchain per case. So the package's wall clock *is* toolchain time, spent one core at a time. Pinned to
four CPUs — the CI runner — it measured **10m06s of wall for 8m31s of CPU**, which reproduces the failure
exactly, because `go test`'s default is **10m00s**. Unpinned on the 20-core development box the same run
came in at 5m48s with `user 4m42s`, and that gap is the whole diagnosis in one line: under one core busy,
nineteen idle, while one process waited on one `llc`.
CI's four-core runner crossed `go test`'s ten-minute default with the suite still two-thirds through
(`TestTextPredicatesPrintAVerdict` had been running 7 s when the alarm fired; it takes 4.9 s).

Why not the obvious lever. Three things make `t.Parallel()` unavailable to *this* suite, and they are
properties of the toolchain, not carelessness:

1. **The in-process runner owns the process's descriptors.** `captureFD` dup2s fd 1 and fd 2 around the
   call into the loaded program (`pkg/lang/jit_llvm.go`, and `RunSource`'s own comment says it), so two
   programs running at once in one process would print into each other's pipes. The record leg cannot be
   concurrent *within* a process; it can only be concurrent across processes.
2. **Cases need process-wide states of their own**: 46 sites in four files use `os.Chdir` (the import-
   from-cwd cases in `ircheck_test.go`, `compiled_eval_test.go`, `host_symbols_test.go`) or `t.Setenv`
   (the oracle override). `chdir` is one per process, so it is not shareable — and Go's model does not
   help: a test that calls `t.Parallel()` runs *beside* the serial ones rather than in a phase after
   them, so leaving those cases non-parallel does not protect the cases that run next to them.
3. **The compiler keeps process-wide scan tables** — `pairClosedOut` (written by `pairCallSpecs`, read
   by codegen), `bodyBindingCache`, the `kindOfIntValue` hook codegen installs. Making them safe is a
   real and separate piece of work (the LSP server will want it); making the *process* the unit of
   parallelism gets the suite its speed without pretending it is done.

The last point decides it: the honest unit of parallelism here is a process, because that is already the
unit of isolation the compiler and the runner have.

## Decision

**`tools/testshards` runs one package's cases in N processes**, one per core, and is what CI runs.

* **The partition is `sort` + round-robin over what `go test -list` reports.** `-list` is the authority
  on which cases exist; sorting makes the split reproducible (its file order moves when a file is
  renamed, and a boundary that drifts turns every bisect into a new experiment); dealing rather than
  slicing keeps two expensive neighbours in one table (`TestCrossTypeComparisonGrid`, 24 s, and its
  friends) out of the same shard. Every name lands in exactly one shard — asserted, not assumed.
* **Each shard is `go test -count=1 -timeout D -run '^(A|B|…)$' pkg`.** The anchors are load-bearing:
  `-run TestFoo` also matches `TestFooBar`, which would cover one case twice and another not at all —
  a hole no failure ever reports. Only names beginning `Test` are admitted: a `Benchmark`/`Example` name
  in a shard's `-run` runs nothing and looks like a pass.
* **The build is warmed once** (`go test -run TestNothingMatchesThisNameXXX`) before the shards start,
  so N shards do not compile the same package N times over, and a compile failure surfaces once, in the
  compiler's own words, instead of N times as N indistinguishable shard failures.
* **Output is attributed, and the summary is data.** Every shard line is prefixed `[lang shard 3/4]`;
  `-json` prints the plan and one row per shard — tests, wall time, ok, error — so an agent can ask
  which shard was the long pole instead of reading prose. `-list` prints the partition without running
  anything, which is how "did every case run, exactly once?" gets checked before a green run is trusted.
* **A shard's timeout is one shard's budget** (`-timeout`, default 10 m), which is now an explicit number
  rather than `go test`'s implicit one; with ADR 0312's per-call budgets a hang ends at the call, and the
  shard timeout is the belt.
* **Ledger-writing modes stay single-process.** `GUSTY_GOLDEN_UPDATE` (rewriting the drift ledger) and
  `GUSTY_GOLDEN_MISSING` (collecting sources the record does not cover) are per-*run* artifacts; N shards
  would each write their own subset over the file. Plain `go test` — or `-shards 1` — is that path, and
  the tool's own docs say so.

Nothing in `pkg/lang` changed for this. Every case still asserts exactly what it asserted, because what
changed is how many processes the assertions are spread over.

## Agentic rationale

The suite is the toolchain's own evidence surface, and an agent has to be able to ask it questions
without reading a wall of prose: `-list` answers "how will you split this", `-json` answers "what ran,
how long, and what failed", and the exit code answers "is the tree green". The split is data
(`plan[].shards[].names`), so coverage is inspectable *before* trusting the verdict — which matters more
for a sharded run than a serial one, because a shard that silently ran nothing is a shard that passes.

## Consequences

* Measured on this tree, pinned to four CPUs (`taskset -c 0-3`) to stand in for a CI runner: **5 m 48 s
  serial → 3 m 03 s with four shards**, each shard 160–181 s. Eight shards bought 2 m 57 s — i.e.
  nothing, which is the number that says the suite is CPU-bound and the floor is total toolchain CPU
  divided by cores, not a scheduling accident. Per-shard durations are in the summary so a future
  imbalance is visible instead of being a mystery slowdown.
* **`t.Parallel` stays unavailable**, and that is now a documented property rather than an implicit one.
  If a later cycle makes the in-process runner descriptor-safe (handing the program its own descriptors
  instead of dup2-ing over the process's) and the scan tables concurrent, in-process parallelism becomes
  available and the shard count becomes a preference rather than a necessity. Both ADR 0312 and this one
  stay correct either way.
* The drift ratchet survives sharding because it was already built for subsets: `checkDriftAgainst`
  skips a ledger row whose case never asked (`askedAbout`), which is what makes a `-run` subset honest
  and also what makes a shard honest. A row is still noticed in whichever shard owns it, and a shard
  that dies fails the run.
* `make testshards` and the workflow's Unit-tests step both go through the tool, so local and CI run the
  same thing; `make test` remains the plain serial command for the artifact-writing modes.
* Two new test files: `tools/testshards/main_test.go` pins coverage (every case ran, exactly once, judged
  from a file the cases themselves write), overlap (the sum of shard work must exceed twice the wall
  clock — robust to a slow machine, because both sides of the ratio slow together), the anchored regex,
  the name filter, and that a red shard turns the harness red while the other shards still run.

## Alternatives rejected

* **`t.Parallel()` on the cases.** Blocked by the three properties in the Context — the dup2 capture, the
  process-wide `chdir`/`Setenv` sites (and Go's model, which runs parallel tests beside serial ones), and
  the compiler's scan tables. Each would have to be re-engineered to make 1244 cases concurrency-safe,
  and a flake in a suite this size is worse than a slow CI step.
* **Splitting `pkg/lang` into several test packages** so `go test`'s own `-p` parallelism kicks in:
  internal tests need the package's unexported surface (`goldenStdoutIs`, `runIR`, the codegen internals),
  and the drift ledger is per-package — moving cases would fork the ratchet that keeps the record honest.
* **Memoising compiled artifacts by IR hash.** Measured before rejecting it: of 1177 compiled-leg runs
  only 972 modules were distinct and of 1818 record-leg programs only 1760 sources were — 3–17 %
  duplicate, about 8 % of the wall, for cache keys, invalidation and a memory footprint that a
  correctness-sensitive suite does not want.
* **Trimming the grid cases** (`TestCrossTypeComparisonGrid` and the rest of the long tail). They are the
  suite's most valuable rows: they are what notices a wrong answer in a rarely-used position. Speed that
  is bought by deleting coverage is not speed, it is blindness with a better clock.
* **`-shards` above the core count by default.** Eight shards on four cores measured 2 m 57 s against 3 m
  03 s — the toolchain calls are CPU-bound, not latency-bound, so oversubscription buys nothing and
  multiplies the memory the runner needs.
* **A CI shell loop over `-run` patterns.** Same mechanism, no `-list` coverage guarantee, no anchored
  regex, no JSON summary, and the partition would live in a YAML string where nobody can test it.

## References

`tools/testshards/main.go`, `tools/testshards/main_test.go`, `makefile` (`testshards`),
`.github/workflows/go.yml`, `docs/operations.md` (§ Running the suite), `pkg/lang/golden.go`
(`askedAbout`, `checkDriftAgainst`), `pkg/lang/jit_llvm.go` (`captureFD`), ADR 0302, ADR 0308, ADR 0312.
