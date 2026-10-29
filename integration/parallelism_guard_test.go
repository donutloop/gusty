package integration

// The parallelism guard. The suite runs 2,880+ cases beside each other, and that is not a property a file can
// be audited for by eye — it is a property of every test function in the tree, including the one someone adds
// next week while reading a doc that says "just write a test".
//
// So the property is checked by the same command a human and an agent run: `parify -check`, which walks every
// *_test.go in the tree, follows helper calls to what they touch, and fails on a Test function that neither
// calls t.Parallel nor carries a `//parify:serial <reason>` saying why it must own the process. The guard
// invokes the CLI rather than importing an analysis library on purpose: the documented command is the thing
// that must fail the build, so the build fails through it — if the CLI breaks, the suite goes red with it.
// The tool's own behaviour (what it refuses, what it honours, where it puts the line) is covered case by case
// in tools/parify, where the sources are synthetic and the assertions are about verdicts. (ADR 0343, ADR 0343.)

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestTheSuiteIsMarkedToRunBesideItself is the build's guarantee about the suite itself: no case may quietly
// opt out of the parallel run by forgetting to ask for it.
func TestTheSuiteIsMarkedToRunBesideItself(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("the parallelism guard builds a second tool; -short is the way to skip it")
	}
	root := repoRootForParify(t)
	out, err := runParifyCheck(t, root, suitePatterns...)
	if err != nil {
		// The tool's own message names the file, the line, the reason, and the marker to add, so the whole
		// diagnosis travels with the failure instead of being paraphrased here.
		t.Fatalf("the suite is not marked to run beside itself: %v\n%s", err, out)
	}
	// The summary line is the evidence that it looked at the suite and not at nothing: a `go run` that found
	// no packages would exit 0 with an empty report, which is how a guard becomes decoration.
	if !strings.Contains(out, "test(s)") {
		t.Fatalf("the guard reported no census, so it cannot have read the suite:\n%s", out)
	}
	t.Logf("guard: %s", strings.TrimSpace(out))
}

// repoRootForParify is the module root, found from the test binary's own source location rather than from the
// working directory — which is the package directory under `go test`, and which some cases have opinions about.
func repoRootForParify(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tools", "parify", "main.go")); err != nil {
		t.Fatalf("the guard cannot find the tool it guards the suite with: %v", err)
	}
	return dir
}

// suitePatterns is the tree the guard covers: everything with a _test.go file that the suite runs. A tool
// invoked with no patterns prints a usage line and exits 2, so the guard says what it is looking at rather
// than hoping the tool has a favourite directory.
var suitePatterns = []string{"./pkg/...", "./cmd/...", "./integration/...", "./tools/..."}

// runParifyCheck runs the tool the way docs/operations.md tells a person to run it, from the repo root.
func runParifyCheck(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the guard shells out to `go run`, which is spelled differently there")
	}
	argv := append([]string{"run", "./tools/parify", "-check", "-quiet"}, args...)
	cmd := exec.Command("go", argv...)
	cmd.Dir = dir
	cmd.Env = parifyEnv()
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// parifyEnv is the environment the guard builds its checker and its measurement runs with: the suite's own
// build tag, so the tool is compiled the same way everything else here is, with any inherited GOFLAGS dropped
// so a wrapper flag cannot quietly turn the check into a no-op — and the artifact-writing modes dropped too.
//
// The second half is not cosmetic. `TestTheParallelCasesActuallyOverlap` shells out to `go test ./pkg/lang/`
// with a `-run` subset, and `pkg/lang`'s TestMain adjudicates its divergence LEDGER: with
// `GUSTY_GOLDEN_UPDATE=1` in the environment — which is how a person runs the artifact-writing mode, serially,
// for one package — that subset rewrote `pkg/lang/testdata/interpreter-golden-drift.json` with the ONE row its
// four named cases happened to diverge on, deleting 317 rows of the other package's ratchet. Measured twice in
// one cycle, which is the definition of a trap the harness has to defuse itself (roadmap Gap R.410, ADR 0409).
var artifactWritingVars = []string{
	"GUSTY_GOLDEN_UPDATE=",
	"GUSTY_GOLDEN_MISSING=",
	"GUSTY_GOLDEN_REPORT=",
	"GUSTY_DEBT_UPDATE=",
	// The membership row's own recorder (`pkg/lang/membership_name_test.go::TestTheRecordSourcesThisRowAsksFor`)
	// writes the source list its tables ask, in the shape `tools/recmerge` reads. It is the same class: a
	// subprocess of the suite inheriting it would have N processes each overwrite the file with their own
	// subset, and the recorder is worth nothing if the list it writes is whichever shard finished last.
	"GUSTY_RECORD_MEMBERSHIP=",
	// … and its twin one row further down the tracker (`pkg/lang/membership_literal_test.go::TestTheRecordSourcesTheLiteralHaystackRowAsksFor`),
	// the recorder for the membership question asked of a spelled container literal (**Gap R.523** / **ADR 0456**).
	"GUSTY_RECORD_MEMBERSHIP_LITERAL=",
	// The corpus recorder (`integration/corpus_record_test.go::TestTheRecordSourcesTheCorpusRecordRowAsksFor`),
	// which dumps every ASSERTED conformance program the record leg owes an answer for (**Gap R.532** / **ADR 0456**).
	"GUSTY_RECORD_CORPUS=",
	// The count-door row's recorder (`pkg/lang/count_answer_effect_test.go::TestTheRecordSourcesTheCountAnswerEffectRowAsksFor`),
	// the same shape again: the sources the answered tables ask, plus the corpus program this row registered,
	// dumped for `tools/recmerge` (**Gap R.534** / **ADR 0460**).
	"GUSTY_RECORD_COUNT_ANSWER_EFFECT=",
	// The applied-lambda module-read row's recorder (`pkg/lang/lambda_module_read_test.go::TestTheFoldedModuleReadRecordSourcesWhatItOwed`),
	// which dumps the same corpus this row registered (**Gap R.331** / **ADR 0461**).
	"GUSTY_RECORD_LAMBDA_MODULE_READ=",
	// The unpacking row's recorder (`pkg/lang/unpack_container_test.go::TestTheUnpackingRecordSourcesTheTableAsks`),
	// the same shape one row further down the tracker — and the reason the list below is now checked against the
	// documents that describe it (`TestEveryRecordingKnobTheDocumentsDescribeIsScrubbed`).
	"GUSTY_RECORD_UNPACK=",
	// … and its twin one row further down the tracker (`pkg/lang/unpack_claims_test.go::TestTheUnpackingClaimsRecordSourcesTheTableAsks`),
	// the recorder for the container that TRAVELS through an unpacking (**Gap R.527** / **ADR 0455**).
	"GUSTY_RECORD_UNPACK_CLAIMS=",
	// The gap ledger (`integration/parity_test.go`) dumps every refusal a run took, for filing as roadmap rows. A
	// refusal dump is an artifact like any other: `-check`'s own subset would otherwise overwrite the run's ledger
	// with the handful of refusals its four cases happen to take.
	"GUSTY_GAP_LEDGER=",
}

func parifyEnv() []string {
	return append(withoutArtifactWritingVars(stripGoFlags(os.Environ())), "GOFLAGS=-tags=llvm20")
}

// stripGoFlags drops any inherited GOFLAGS so a wrapper flag cannot quietly turn the check into a no-op.
func stripGoFlags(env []string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "GOFLAGS=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// withoutArtifactWritingVars is the half that keeps an artifact an artifact: a command this guard starts must
// not be able to rewrite a ledger as a side effect of the environment it was born into.
func withoutArtifactWritingVars(env []string) []string {
	out := env[:0:0]
outer:
	for _, kv := range env {
		for _, denied := range artifactWritingVars {
			if strings.HasPrefix(kv, denied) {
				continue outer
			}
		}
		out = append(out, kv)
	}
	return out
}

// TestTheGuardsSubprocessCarriesNoArtifactWritingMode is the guard on the guard: a subprocess the parallelism
// measurement starts must not be able to rewrite an artifact, whatever the person running the suite asked for.
// It asks the scrubber a SYNTHETIC environment rather than setting variables of its own, because a case that
// edits the process environment cannot run beside the others — and the suite's own parallelism census (ADR 0343)
// is one of the things this file exists to protect.
func TestTheGuardsSubprocessCarriesNoArtifactWritingMode(t *testing.T) {
	t.Parallel()
	synth := append([]string{
		"GOFLAGS=-tags=slow",
		"GUSTY_GOLDEN_UPDATE=1",
		"GUSTY_RECORD_MEMBERSHIP=/tmp/ledger-from-a-subprocess.json",
		"GUSTY_GOLDEN_MISSING=/tmp/missing.json",
		"GUSTY_GOLDEN_REPORT=/tmp/report.json",
		"GUSTY_DEBT_UPDATE=1",
		"GUSTY_GOLDEN_UPDATED_LATER=keep-me",
		"PATH=/usr/bin",
	}, artifactWritingVars...) // each prefix must be gone from the result
	got := withoutArtifactWritingVars(synth)
	joined := strings.Join(got, "\n")
	for _, denied := range artifactWritingVars {
		for _, line := range got {
			if strings.HasPrefix(line, denied) {
				t.Fatalf("parifyEnv hands %q to a `go test` subprocess — a filtered subprocess run would rewrite a "+
					"package's drift ledger with only its own subset (roadmap Gap R.410)", line)
			}
		}
	}
	// The near-miss stays: a variable that merely starts with a similar name is not an artifact mode, and a
	// scrubber that clears prefixes rather than names would delete the suite's other knobs.
	if !strings.Contains(joined, "GUSTY_GOLDEN_UPDATED_LATER=keep-me") {
		t.Fatalf("the scrubber deleted something that is not an artifact-writing mode: %q", joined)
	}
	// Whatever the parent's GOFLAGS were, the child gets the suite's tag — the composition the guard actually
	// builds, asserted rather than each half pretending to do the other's job.
	for _, line := range stripGoFlags(withoutArtifactWritingVars(synth)) {
		if strings.HasPrefix(line, "GOFLAGS=") {
			t.Fatalf("an inherited GOFLAGS survived the composition: %q", line)
		}
	}
	if strings.Contains(strings.Join(withoutArtifactWritingVars([]string{"PATH=/usr/bin"}), "|"), "GUSTY_") {
		t.Fatal("a clean environment grew an artifact mode")
	}
	if env := strings.Join(parifyEnv(), "\n"); !strings.Contains(env, "GOFLAGS=-tags=llvm20") {
		t.Fatalf("parifyEnv must always carry the suite's build tag:\n%s", env)
	}
}

// TestEveryRecordingKnobTheDocumentsDescribeIsScrubbed keeps the DOCUMENT honest.
//
// docs/operations.md's "Recording knobs" table is the contract a person reads before setting one of these: every
// row there says the mode is "scrubbed from every subprocess of the suite". That sentence was true of the four
// ledgers it was written for and false of the two recorders added later — the unpacking row shipped a recorder and
// a documentation row claiming the scrubbing, and nothing cross-read them, so the claim would have gone stale in
// the quiet direction (a knob that is not scrubbed is invisible until someone runs the suite with it set and loses
// a ledger, which is Gap R.410's accident waiting to happen again).
//
// The exemption is data, not prose: the source of truth for what counts as a recording knob is the documents table
// itself, read here, so adding a knob without scrubbing it fails this case rather than shipping a lie. Reads only;
// like every guard over a file outside the package holding the test, it depends on the gate's `-count=1` (ADR 0317)
// to run at all after an edit.
func TestEveryRecordingKnobTheDocumentsDescribeIsScrubbed(t *testing.T) {
	t.Parallel()
	root := repoRootForParify(t)
	body, err := os.ReadFile(filepath.Join(root, "docs", "operations.md"))
	if err != nil {
		t.Fatalf("read docs/operations.md: %v", err)
	}
	var knobs []string
	start := -1
	lines := strings.Split(string(body), "\n")
	for i, line := range lines {
		if strings.Contains(line, "Recording knobs") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatal("docs/operations.md no longer has a \"Recording knobs\" table — this guard reads nothing")
	}
	for _, line := range lines[start:] {
		if len(knobs) > 0 && !strings.HasPrefix(strings.TrimSpace(line), "|") {
			break // the table ended
		}
		knobs = append(knobs, gustyKnobNames(line)...)
	}
	if len(knobs) < 5 {
		t.Fatalf("found %d recording knobs in docs/operations.md — the table moved or emptied, and this guard "+
			"reads nothing (roadmap Gap R.205's vacuity rule)", len(knobs))
	}
	scrubbed := map[string]bool{}
	for _, kv := range artifactWritingVars {
		scrubbed[strings.TrimSuffix(kv, "=")] = true
	}
	for _, k := range knobs {
		if !scrubbed[k] {
			t.Errorf("docs/operations.md documents %s as a recording knob, but parifyEnv hands it to a `go test `"+
				"subprocess: N shards would each overwrite the artifact with their own subset (Gap R.410). Add it to "+
				"artifactWritingVars, or delete the documentation row that promises the scrubbing.", k)
		}
	}
}

// gustyKnobNames pulls every `GUSTY_…` knob name out of one line: an env-var name in these documents is always
// spelled as an identifier, and the table writes them bare, with a trailing `=1`, or inside backticks — so the
// scan takes the identifier characters and stops at anything that is not one.
func gustyKnobNames(line string) []string {
	var out []string
	for i := 0; i+6 <= len(line); i++ {
		if !strings.HasPrefix(line[i:], "GUSTY_") {
			continue
		}
		j := i + 6
		for j < len(line) {
			c := line[j]
			if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
				j++
				continue
			}
			break
		}
		out = append(out, line[i:j])
		i = j
	}
	return out
}

// overlapParents names the families the overlap measurement runs: four cases in pkg/lang whose every subtest
// compiles a program, links it and runs it as a child process (ADR 0343's execution model), so each one is
// tens of seconds of subprocess work spread over tens of subtests. They are named rather than counted because
// the measurement is worth nothing if the cases it picked are `if x != 42` — and they are named as a fixed
// list, and checked to have run, so a rename cannot quietly turn the measurement into a run of zero cases
// that passes by vacuity.
var overlapParents = []string{
	"TestRunTimeBuiltSlotMembershipAndLoopAnswerOnBothLegs",
	"TestRenderPairWritesWhatCPythonWrites",
	"TestAReboundContainerAnswersWhatTheLatestBindingGaveIt",
	"TestAFoldAnswersWithTheWinnersOwnKind",
}

// TestTheParallelCasesActuallyOverlap measures the claim the whole change is about. `tools/testshards` has long
// insisted that its shards overlap — "the sum of shard durations must exceed twice the wall clock, or the run
// failed; that is the difference between a parallel harness and a serial one that prints nicely" — and the
// same suspicion applies to `t.Parallel()`: a suite where every case is marked parallel but each one blocks on
// a global lock, or on an fd capture, or on a mutex around the toolchain, prints the same census line and takes
// the same 12 minutes. So the property is measured rather than asserted: run these cases through the toolchain
// with `-parallel 20`, add up what each subtest says it took, and require that the sum clearly exceeds the wall
// clock the run occupied.
func TestTheParallelCasesActuallyOverlap(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("the overlap measurement runs a second `go test`; -short is the way to skip it")
	}
	if runtime.GOOS == "windows" {
		t.Skip("the measurement shells out to `go test`, which is spelled differently there")
	}
	root := repoRootForParify(t)
	anchored := "^(" + strings.Join(overlapParents, "|") + ")$"
	cmd := exec.Command("go", "test", "-tags=llvm20", "-count=1", "-timeout", "15m",
		"-parallel", "20", "-json", "-run", anchored, "./pkg/lang/")
	cmd.Dir = root
	cmd.Env = parifyEnv()
	start := time.Now()
	out, err := cmd.Output()
	wall := time.Since(start)
	if err != nil {
		t.Fatalf("the overlap measurement could not run its subset: %v", err)
	}

	// `-json` reports the parent's own elapsed as ~0 when its time is in its subtests, so the sum is taken
	// over the leaves — which are also the units that actually hold a subprocess each.
	var sum float64
	leaves := 0
	saw := map[string]int{}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var e struct {
			Action  string
			Test    string
			Package string
			Elapsed float64
		}
		if json.Unmarshal([]byte(line), &e) != nil || e.Package != "github.com/donutloop/gusty/pkg/lang" {
			continue
		}
		if e.Action != "pass" || e.Test == "" || !strings.Contains(e.Test, "/") {
			continue
		}
		sum += e.Elapsed
		leaves++
		saw[strings.SplitN(e.Test, "/", 2)[0]]++
	}
	for _, name := range overlapParents {
		if saw[name] == 0 {
			t.Fatalf("the case %s reported no subtest at all, so the measurement measured nothing — the "+
				"list has been renamed or deleted; fix the list, do not delete the case", name)
		}
	}
	if leaves < 30 {
		t.Fatalf("only %d leaf cases reported: the subset is too small to measure overlap with", leaves)
	}
	t.Logf("overlap: %d cases, %.1fs of case time against %.1fs of wall clock (x%.1f)",
		leaves, sum, wall.Seconds(), sum/wall.Seconds())
	if sum < 2*wall.Seconds() {
		t.Errorf("the cases did not overlap: %d of them account for %.1fs of test time in %.1fs of wall clock "+
			"— a suite whose cases are marked parallel and still queue is indistinguishable from a serial one "+
			"from the outside, which is why this is measured (ADR 0343)", leaves, sum, wall.Seconds())
	}

	// The ratio says the cases were *in flight* together; the question the harness actually owes is whether
	// running them beside each other was worth anything. So the same subset is run again with the cases kept
	// one at a time, and the two wall clocks are compared on the machine doing the measuring — no remembered
	// baseline, no CI-runner folklore, and no failure on a box too small to gain anything by overlapping.
	cores := runtime.NumCPU()
	if cores < 4 {
		t.Logf("overlap speedup not checked: %d cores is not a machine worth measuring it on", cores)
		return
	}
	serial := time.Now()
	scmd := exec.Command("go", "test", "-tags=llvm20", "-count=1", "-timeout", "15m",
		"-parallel", "1", "-run", anchored, "./pkg/lang/")
	scmd.Dir = root
	scmd.Env = parifyEnv()
	if _, err := scmd.Output(); err != nil {
		t.Fatalf("the serial control run of the same subset failed: %v", err)
	}
	serialWall := time.Since(serial)
	speedup := serialWall.Seconds() / wall.Seconds()
	t.Logf("speedup: the same %d cases took %.1fs one at a time and %.1fs beside each other (x%.1f on %d cores)",
		leaves, serialWall.Seconds(), wall.Seconds(), speedup, cores)
	if speedup < 2 {
		t.Errorf("running the cases beside each other bought %.1fx over running them one at a time — the "+
			"`t.Parallel()` marks are there and the toolchain calls are not: something is still queueing them "+
			"(a lock, a shared fixture, a governor) (ADR 0343)", speedup)
	}
}
