# 0317. A guard that never reads its own file is a guard that never ran

Status: accepted. Roadmap: `Gap R.200` (the serial gate could answer from `go test`'s cache), continues ADR 0313
(the shard runner, which already runs each shard with `-count=1`), ADR 0302 (the record leg, whose guards read
`testdata`), Gap R.193 (the witness guard, which reads the agent-read documents by path).

## Context

`make test` — the command `AGENTS.md` names as the gate — was `go test -tags=llvm20 ./...`, with no `-count=1`.

Most of this suite cannot be served from a cache wrongly: a case's answer comes from `llc`, `cc`, `lli` or
`python3` run during the test, and the cache key is the package's build inputs. But a third of the suite is
**guards over files that live outside the package holding the test** — `docs/language.md`, `docs/operations.md`,
`README.md`, `roadmap.md`, `docs/adr/*.md`, `integration/programs/*.gy`, `testdata/*.json`. `go test` puts build
inputs in the cache key; it does not put a file the test opens at run time. So on a warm cache:

```
$ edit docs/language.md          # a sentence the doc pin asserts
$ go test -tags=llvm20 ./pkg/lang
ok      github.com/donutloop/gusty/pkg/lang       (cached)
```

The guard did not run. The agent editing the doc — the primary consumer of this interface, per the project
contract — reads `ok` and moves on, and the doc drifts from the code it claims to describe. The failure is
asymmetric and therefore invisible: the *first* run after an edit is honest, and every run after that can be a
lie until something touches a `.go` file.

ADR 0313's shard runner already passes `-count=1` for exactly this reason; the serial command — the one the
loop is told to run, and the one the artifact-writing modes (`GUSTY_GOLDEN_UPDATE`, `GUSTY_GOLDEN_MISSING`)
require — did not.

## Decision

**`make test` runs `go test -tags=llvm20 -count=1 ./...`, and a test asserts the makefile says so.**

* The flag goes on the shipped command, not in a doc sentence. A rule that lives only in prose is a rule that
  half the contributors have read.
* `tools/testshards` — the tool that owns "how the suite is run" — grows
  `TestTheMakefileRunsTheSerialSuiteWithoutTheCache`: it parses the makefile's `test` recipe and requires both
  `-count=1` (no cached verdict) and `-tags=llvm20` (an untagged run skips the whole LLVM leg and prints a
  green that means nothing — ADR 0313's own trap). The guard was checked by deleting the flag: the test goes
  red, so it is a guard and not a comment.
* CI is unaffected: a fresh checkout has no test cache, so `-count=1` costs nothing there. What it costs is
  the warm-cache rerun an agent does between two edits — measured here at the same wall clock as a cached run
  for the subprocess-bound packages, because those dominate.

## Agentic rationale

The interface this project promises an agent is not only the CLI; it is also the *verdicts* an agent can act
on. Two rules follow and both are implemented here:

* A verdict a machine consumes must be produced by a run that actually read its inputs. The same instinct that
  makes `--json` carry `phase` and `code` rather than prose makes the test gate carry `-count=1` rather than
  hope.
* A documented command and a shipped command are one artifact. The makefile is now under test, in the package
  that already tests how the suite is sharded, so the docs cannot drift from it (ADR 0127's rule for code
  examples, applied to a build file).

## Consequences

* `make test` is the honest serial gate; `make testshards` already was.
* `tools/testshards/main_test.go` gains a fifth runner-behaviour row, and the makefile gains a comment saying
  why the flag is load-bearing — so the next person who "simplifies" the line finds a failing test, not a
  silent hole.
* The `docs/operations.md` section on running the suite states the rule where an agent reading the CLI surface
  will find it.
* What this does **not** fix, said plainly: a test that reads an external file and is run by some *other*
  harness (a bespoke script, an editor integration) can still be cached by that harness. The rule for any new
  runner is the same as the rule here — no cache, or a cache key that includes what the tests open.

## Alternatives rejected

* **`go test -a`** — rebuilds everything every time: correct and wasteful; the packages that genuinely
  recompile dominate the cost, and it does not make the *cache key* right, it just avoids it.
* **Marking the data files as `testmain` dependencies** — there is no such mechanism; making external files
  build inputs means `go:embed`, which would copy the record, the docs and the roadmaps into the test binaries,
  bloat every build, and still not track a file a helper opens by a computed path.
* **Deleting the test cache in CI and in the loop** — an invisible side effect on shared state, and it makes the
  command that is *supposed* to be reproducible depend on which directory someone cleared last.
* **Documenting `-count=1` and leaving the makefile alone** — the failure this row closes is precisely a
  documented rule that the shipped command did not implement.

## References

`makefile` (`test`), `tools/testshards/main_test.go`
(`TestTheMakefileRunsTheSerialSuiteWithoutTheCache`), `docs/operations.md` (§ Running the suite), ADR 0313,
ADR 0302, Gap R.193.
