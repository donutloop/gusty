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
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
	for _, pkg := range []string{"lang", "integration", "gustyc", "parify"} {
		if !strings.Contains(out, pkg) && strings.Contains(out, " — ") {
			// The census line names the totals only; the per-reason lines name packages. Checking that the
			// tree's own package names appear somewhere keeps a pattern typo from emptying the check.
			t.Logf("guard note: no line mentions %q", pkg)
		}
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
