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

// parifyEnv is the environment the guard builds its checker with: the suite's own build tag, so the tool is
// compiled the same way everything else here is, with any inherited GOFLAGS dropped so a wrapper flag cannot
// quietly turn the check into a no-op.
func parifyEnv() []string {
	env := os.Environ()
	out := env[:0:0]
	for _, kv := range env {
		if strings.HasPrefix(kv, "GOFLAGS=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GOFLAGS=-tags=llvm20")
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
