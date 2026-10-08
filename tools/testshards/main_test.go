package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// The harness has one job: every case runs, exactly once, and several of them run at the same time.
// Each of those is a claim that can fail quietly — a `-run` pattern without anchors covers a case twice
// and another not at all, and a "parallel" runner that runs its shards one after another still prints
// PASS — so all three are measured here rather than assumed.

func TestPartitionCoversEveryNameOnce(t *testing.T) {
	names := []string{}
	for i := 0; i < 23; i++ {
		names = append(names, fmt.Sprintf("TestCase%02d", i))
	}
	for _, n := range []int{1, 2, 3, 4, 8, 40} {
		shards := partition(names, n)
		seen := map[string]int{}
		total := 0
		for _, sh := range shards {
			if sh.Tests != len(sh.Names) {
				t.Errorf("n=%d shard %d: Tests=%d, %d names in it — the count an agent reads is not the list", n, sh.Index, sh.Tests, len(sh.Names))
			}
			for _, nm := range sh.Names {
				seen[nm]++
				total++
			}
		}
		if total != len(names) {
			t.Errorf("n=%d: %d names placed, %d exist", n, total, len(names))
		}
		for nm, c := range seen {
			if c != 1 {
				t.Errorf("n=%d: %s landed in %d shards, want exactly 1", n, nm, c)
			}
		}
		if len(shards) > min(n, len(names)) {
			t.Errorf("n=%d: %d shards for %d tests — an empty shard runs nothing and looks like a pass", n, len(shards), len(names))
		}
	}
}

func TestPartitionIsReproducible(t *testing.T) {
	// The same names must give the same shards every time, in any starting order: `-list` reports in
	// file order, and a boundary that moves when a file is renamed makes every bisect a new experiment.
	a := []string{"TestZulu", "TestAlpha", "TestMike", "TestBravo", "TestYankee", "TestKilo"}
	b := []string{"TestMike", "TestBravo", "TestKilo", "TestYankee", "TestAlpha", "TestZulu"}
	sa, sb := partition(a, 3), partition(b, 3)
	for i := range sa {
		if strings.Join(sa[i].Names, ",") != strings.Join(sb[i].Names, ",") {
			t.Fatalf("shard %d differs by input order: %v vs %v", i, sa[i].Names, sb[i].Names)
		}
	}
}

func TestRunRegexIsAnchored(t *testing.T) {
	// The bug this guards: `-run TestFoo` also matches TestFooBar, so one case runs in two shards and
	// the second shard's case never runs at all — a hole that no failure ever reports.
	got := runRegex([]string{"TestFoo", "TestFooBar"})
	if got != "^(TestFoo|TestFooBar)$" {
		t.Fatalf("runRegex = %s", got)
	}
	if !strings.HasPrefix(got, "^(") || !strings.HasSuffix(got, ")$") {
		t.Fatal("the alternation must be anchored at both ends")
	}
}

func TestIsTestNameRejectsProse(t *testing.T) {
	for name, want := range map[string]bool{
		"TestSomething":                   true,
		"TestA_1":                         true,
		"BenchmarkThing":                  false,
		"ExampleThing":                    false,
		"ok  \tgithub.com/x/y":            false,
		"testing: warning":                false,
		"":                                false,
		"Test With Space":                 false,
		"# github.com/x/y [build failed]": false,
	} {
		if got := isTestName(name); got != want {
			t.Errorf("isTestName(%q) = %v, want %v", name, got, want)
		}
	}
}

// TestShardsRunRealTestsForReal is the integration half: a synthetic package of sleeping cases, run by
// the built harness, checked for coverage and for genuine overlap.
func TestShardsRunRealTestsForReal(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("no `go` on PATH to build the harness with")
	}
	dir := writeSyntheticPkg(t, 12, "400*time.Millisecond")
	bin := buildHarness(t)

	mark := filepath.Join(dir, "marks")
	if err := os.MkdirAll(mark, 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(bin, "-shards", "4", "-timeout", "5m", "-json", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GUSTY_SHARD_MARKS="+mark)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harness failed: %v\n%s", err, out)
	}
	var sum summary
	if err := json.Unmarshal(out, &sum); err != nil {
		t.Fatalf("harness -json did not parse: %v\n%s", err, out)
	}
	if !sum.OK {
		t.Fatalf("summary says FAIL: %s", out)
	}
	if len(sum.Results) != 4 {
		t.Fatalf("%d shards reported, want 4 (results: %s)", len(sum.Results), jsonOf(sum.Results))
	}

	// Overlap, measured rather than asserted by faith. Twelve 400ms cases in four shards are ~4.8s of
	// work in the suite and ~1.2s of work in each shard: if the shards were run one after another — the
	// quiet way a "parallel" runner fails — the wall clock would be at least the sum of the shard
	// durations. Comparing the two is also robust to a slow machine, because both sides of the ratio
	// slow down together; an absolute deadline would just be a flake with a number in it.
	elapsed := mustDur(t, sum.Elapsed)
	work := time.Duration(0)
	for _, r := range sum.Results {
		work += mustDur(t, r.Duration)
	}
	if elapsed*2 >= work {
		t.Errorf("no overlap: wall %s against %s of shard work (sum %s) — the shards ran in sequence", elapsed, work, jsonOf(sum.Results))
	}

	// Coverage: every case ran, and ran exactly once — counted from what the cases themselves wrote,
	// not from the harness's own claim about its plan.
	lines := ranCases(t, mark)
	want := 12
	if len(lines) != want {
		t.Fatalf("%d case executions recorded, want %d: %v", len(lines), want, lines)
	}
	counts := map[string]int{}
	for _, l := range lines {
		counts[l]++
	}
	for nm, c := range counts {
		if c != 1 {
			t.Errorf("%s ran %d times, want once", nm, c)
		}
	}
	if len(counts) != want {
		t.Errorf("%d distinct cases ran, want %d — a case fell through the partition", len(counts), want)
	}
}

func TestAFailingShardFailsTheRun(t *testing.T) {
	// The harness may not turn a red case green by virtue of being a wrapper: a failing shard exits the
	// tool non-zero, says which shard, and the other shards still get to run.
	dir := writeSyntheticPkg(t, 6, "10*time.Millisecond")
	bin := buildHarness(t)
	cmd := exec.Command(bin, "-shards", "3", "-timeout", "5m", "-json", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GUSTY_SYNTH_FAIL=TestSynthetic003")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("a failing case did not fail the harness:\n%s", out)
	}
	var sum summary
	if err := json.Unmarshal(out, &sum); err != nil {
		t.Fatalf("harness -json did not parse: %v\n%s", err, out)
	}
	if sum.OK {
		t.Error("summary.ok is true although a case failed")
	}
	failed := 0
	for _, r := range sum.Results {
		if !r.OK {
			failed++
			if r.Err == "" {
				t.Errorf("shard %d failed and named nothing", r.Index)
			}
		}
	}
	if failed != 1 {
		t.Errorf("%d shards failed, want exactly the one holding the bad case", failed)
	}
	if len(sum.Results) != 3 {
		t.Errorf("%d shards reported; every shard has to run even when one is red", len(sum.Results))
	}
}

// --- fixtures -------------------------------------------------------------------------------------

// writeSyntheticPkg makes a module of n sleeping cases. Each case records its own name by appending to
// a file the harness points at with GUSTY_SHARD_MARKS, so the coverage claim is checked from what the
// cases did rather than from anything the harness says about its own plan.
func writeSyntheticPkg(t *testing.T, n int, sleep string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module gusty.shard.probe\n\ngo 1.22\n")

	var b strings.Builder
	b.WriteString("package synth\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n\t\"time\"\n)\n\n")
	b.WriteString(`
func record(t *testing.T, name string) {
	dir := os.Getenv("GUSTY_SHARD_MARKS")
	if dir == "" {
		return
	}
	f, err := os.OpenFile(filepath.Join(dir, "ran.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintf(f, "%s\n", name)
	f.Close()
}
`)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("TestSynthetic%03d", i)
		// GUSTY_SYNTH_FAIL names one case: the failure test needs a case that is red on purpose, and
		// needs the other shards to keep running anyway.
		b.WriteString(fmt.Sprintf("func %s(t *testing.T) {\n\trecord(t, %q)\n\tif os.Getenv(\"GUSTY_SYNTH_FAIL\") == %q {\n\t\tt.Fatalf(\"this case is red on purpose\")\n\t}\n\ttime.Sleep(%s)\n}\n\n", name, name, name, sleep))
	}
	write("synth_test.go", b.String())
	return dir
}

func ranCases(t *testing.T, mark string) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(mark, "ran.log"))
	if err != nil {
		t.Fatalf("the cases never reported in: %v", err)
	}
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}

func buildHarness(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "testshards")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build the harness: %v\n%s", err, out)
	}
	return bin
}

// mustDur reads a duration the harness reported. A malformed one is a harness bug, not a skip.
func mustDur(t *testing.T, s string) time.Duration {
	t.Helper()
	d, err := time.ParseDuration(s)
	if err != nil {
		t.Fatalf("harness reported %q, which is not a duration", s)
	}
	return d
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// writeModule and writeFile build the two-package tree the skip test needs: a module with one package
// that has cases and one that does not.
func writeModule(t *testing.T, dir string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, "go.mod"), "module gusty.shard.mixed\n\ngo 1.22\n")
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAPackageWithNoTestFilesIsSkippedNotFatal runs the harness over a tree holding one package with
// cases and one with none — which is what `./...` actually matches, since the tree has tools and a
// command in it. The empty package is reported and skipped: refusing to run because some package holds
// nothing would make the CI command depend on which directories happen to exist, and saying nothing
// would let a renamed package contribute nothing to a green run unnoticed.
func TestAPackageWithNoTestFilesIsSkippedNotFatal(t *testing.T) {
	dir := t.TempDir()
	writeModule(t, dir)
	if err := os.MkdirAll(filepath.Join(dir, "withtests"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "withtests", "cases.go"), "package withtests\n\nfunc Answer() int { return 42 }\n")
	writeFile(t, filepath.Join(dir, "withtests", "answer_test.go"), `package withtests

import "testing"

func TestFirst(t *testing.T)  {}
func TestSecond(t *testing.T) {}
`)
	if err := os.MkdirAll(filepath.Join(dir, "notests"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "notests", "tool.go"), "package notests\n\nfunc Unused() {}\n")

	bin := buildHarness(t)
	cmd := exec.Command(bin, "-shards", "2", "-json", "./...")
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running the harness: %v", err)
		}
		code = ee.ExitCode()
	}
	if code != 0 {
		t.Fatalf("a package with no tests must not fail the run (exit %d)\n%s\n%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "notests has no tests to shard") {
		t.Errorf("the skipped package should be named, so a green run says what it did not run:\n%s", stderr.String())
	}
	var sum struct {
		OK      bool `json:"ok"`
		Results []struct {
			Pkg   string `json:"pkg"`
			Tests int    `json:"tests"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &sum); err != nil {
		t.Fatalf("-json is not parseable: %v\n%s", err, stdout.String())
	}
	if !sum.OK {
		t.Errorf("the run reported itself red: %s", stdout.String())
	}
	// Two cases across two shards: one entry per shard that ran, two cases between them. The claim
	// being checked here is that the package with no files contributed nothing and nothing else did.
	total := 0
	for _, r := range sum.Results {
		if !strings.HasSuffix(r.Pkg, "/withtests") {
			t.Errorf("a shard ran for %s, which has no tests", r.Pkg)
		}
		total += r.Tests
	}
	if total != 2 {
		t.Errorf("the package with cases should account for both of them across the shards, got %+v", sum.Results)
	}
}

// --- the merged drift adjudication (roadmap Gap R.199, ADR 0315) --------------------------------
//
// A ledger row is a claim about a RUN, and a sharded run is N processes. These four rows are the four
// ways that goes wrong, each pinned against a real ledger file in a temp dir so the adjudication under
// test is the one TestMain would have run, not a mock of it.

func writeEvidenceFixture(t *testing.T, dir, name string, e lang.GoldenEvidence) string {
	t.Helper()
	path := filepath.Join(dir, name)
	body, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		t.Fatalf("marshal evidence: %v", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	return path
}

// ledgerFixture writes a drift ledger and returns its path: the file's shape is the committed one
// (testdata/interpreter-golden-drift.json), because the point is to read it the way the suite does.
func ledgerFixture(t *testing.T, dir string, rows []lang.Divergence) string {
	t.Helper()
	path := filepath.Join(dir, "interpreter-golden-drift.json")
	body, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		t.Fatalf("marshal ledger: %v", err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
	return path
}

const shardSource = "print(min(n, 3))"

func TestMergedEvidenceJudgesTheRunAndNotTheShard(t *testing.T) {
	dir := t.TempDir()
	ledger := ledgerFixture(t, dir, []lang.Divergence{{
		Source: shardSource, Reason: "the compiled backend cannot build a program the engine ran",
		Expected: `stdout "3\n"`, Actual: "roadmap L11.1",
	}})
	// The two witnesses of one source land in different shards: one saw it diverge, the other only ran it.
	// Judged per process — which is what this runner used to do — the second shard reports the row PAID and
	// the run fails over a partition that merely moved.
	shard1 := writeEvidenceFixture(t, dir, "s1.json", lang.GoldenEvidence{
		Ledger: ledger, Asked: []string{shardSource},
		Divergences: []lang.Divergence{{
			Source: shardSource, Reason: "the compiled backend cannot build a program the engine ran",
			Expected: `stdout "3\n"`, Actual: "roadmap L11.1",
		}},
	})
	shard2 := writeEvidenceFixture(t, dir, "s2.json", lang.GoldenEvidence{
		Ledger: ledger, Asked: []string{shardSource},
	})
	problems := adjudicate(summary{OK: true, Results: []resultFor{
		{Pkg: "pkg/lang", Index: 0, Of: 2, OK: true, Evidence: shard1},
		{Pkg: "pkg/lang", Index: 1, Of: 2, OK: true, Evidence: shard2},
	}})
	if len(problems) != 0 {
		t.Fatalf("the merged run is owed nothing, and the shard that only ran the source would have filed it paid:\n%s", strings.Join(problems, "\n"))
	}
	// The same evidence, judged by the second shard alone, is the false failure this closes.
	if alone := adjudicate(summary{OK: true, Results: []resultFor{
		{Pkg: "pkg/lang", Index: 1, Of: 2, OK: true, Evidence: shard2},
	}}); len(alone) == 0 {
		t.Errorf("one shard's evidence on its own should read PAID — that is the defect Gap R.199 files")
	} else if !strings.Contains(alone[0], "PAID debt") {
		t.Errorf("the per-shard reading should say PAID, it said: %s", alone[0])
	}
}

func TestPaidDebtIsStillCaughtAcrossShards(t *testing.T) {
	dir := t.TempDir()
	ledger := ledgerFixture(t, dir, []lang.Divergence{{
		Source: shardSource, Reason: "the compiled backend cannot build a program the engine ran",
		Expected: `stdout "3\n"`,
	}})
	// Every shard asked the source and NO shard saw it diverge: the debt is paid, and the ratchet that
	// says "shrink the list" has to fire for the run as a whole. Merging must not buy a free pass.
	e1 := writeEvidenceFixture(t, dir, "s1.json", lang.GoldenEvidence{Ledger: ledger, Asked: []string{shardSource}})
	e2 := writeEvidenceFixture(t, dir, "s2.json", lang.GoldenEvidence{Ledger: ledger, Asked: []string{shardSource}})
	problems := adjudicate(summary{OK: true, Results: []resultFor{
		{Pkg: "pkg/lang", Index: 0, Of: 2, OK: true, Evidence: e1},
		{Pkg: "pkg/lang", Index: 1, Of: 2, OK: true, Evidence: e2},
	}})
	if len(problems) != 1 || !strings.Contains(problems[0], "PAID debt still on the ledger") {
		t.Fatalf("a row no shard saw diverge is paid off and must fail the run, got %q", problems)
	}
}

func TestANewDivergenceSeenByOneShardFailsTheRun(t *testing.T) {
	dir := t.TempDir()
	ledger := ledgerFixture(t, dir, nil)
	// Debt arriving unannounced in ONE shard is still debt for the run; a per-shard check finds it too, so
	// this row is about not LOSING it while the other half moved to the runner.
	e1 := writeEvidenceFixture(t, dir, "s1.json", lang.GoldenEvidence{
		Ledger: ledger, Asked: []string{shardSource},
		Divergences: []lang.Divergence{{Source: shardSource, Reason: "stdout differs from the record", Expected: "3\n", Actual: "2\n"}},
	})
	e2 := writeEvidenceFixture(t, dir, "s2.json", lang.GoldenEvidence{Ledger: ledger, Asked: []string{"print(1)"}})
	problems := adjudicate(summary{OK: true, Results: []resultFor{
		{Pkg: "pkg/lang", Index: 0, Of: 2, OK: true, Evidence: e1},
		{Pkg: "pkg/lang", Index: 1, Of: 2, OK: true, Evidence: e2},
	}})
	if len(problems) != 1 || !strings.Contains(problems[0], "NEW divergence") {
		t.Fatalf("new debt reported by one shard has to redden the run, got %q", problems)
	}
}

func TestAFailedShardAdjudicatesNothing(t *testing.T) {
	dir := t.TempDir()
	ledger := ledgerFixture(t, dir, []lang.Divergence{{Source: shardSource, Reason: "r", Expected: "x"}})
	// A shard that died mid-run left partial evidence at best; the shard's own failure is the message, and
	// a second one invented from a truncated file sends someone chasing the wrong thing.
	e1 := writeEvidenceFixture(t, dir, "s1.json", lang.GoldenEvidence{Ledger: ledger, Asked: []string{shardSource}})
	problems := adjudicate(summary{Results: []resultFor{
		{Pkg: "pkg/lang", Index: 0, Of: 2, OK: true, Evidence: e1},
		{Pkg: "pkg/lang", Index: 1, Of: 2, OK: false, Err: "test timed out"},
	}})
	if len(problems) != 0 {
		t.Fatalf("a run that already failed must not also be told its ledger is paid off: %q", problems)
	}
}

// TestEvidenceNamesDoNotCollideAcrossPackages pins the naming the merge depends on: four packages are sharded
// at once, each with its own shard indices, so a name keyed on the index alone would have shard 1 of one
// package overwrite the evidence of shard 1 of another — and the merger would silently judge a ledger on half
// of one run's evidence.
func TestEvidenceNamesDoNotCollideAcrossPackages(t *testing.T) {
	if got := evidenceName("github.com/x/y/pkg/lang", 3); got != "github-com-x-y-pkg-lang-003.golden-evidence.json" {
		t.Errorf("evidenceName = %q", got)
	}
	if evidenceName("pkg/a", 1) == evidenceName("pkg/b", 1) {
		t.Error("two packages' shard 1 collided on one evidence file")
	}
	if evidenceName("pkg/a", 1) == evidenceName("pkg/a", 2) {
		t.Error("two shards of one package collided on one evidence file")
	}
}

// --- the serial command the loop is told to run (docs/operations.md § Running the suite) ---------
//
// `make test` is the gate AGENTS.md names, and half the suite reads files that live outside the package
// holding the test — docs/language.md, roadmap.md, integration/programs/*.gy. `go test`'s cache key is the
// package's build inputs, not the data a test opens, so on a warm cache a doc guard can be reported green by
// a run that never opened the file a doc edit changed. The only honest serial command is the one that skips
// the cache; this asserts the shipped makefile says so, rather than the docs claiming it and the makefile
// disagreeing.
func TestTheMakefileRunsTheSerialSuiteWithoutTheCache(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("..", "..", "makefile"))
	if err != nil {
		t.Fatalf("read the makefile: %v", err)
	}
	var line string
	inTest := false
	for _, l := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(l, "test:") {
			inTest = true
			continue
		}
		if !inTest {
			continue
		}
		if !strings.HasPrefix(l, "\t") {
			break
		}
		if strings.Contains(l, "go test") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatal("the makefile's `test` target has no `go test` line to check")
	}
	if !strings.Contains(line, "-count=1") {
		t.Errorf("`make test` may answer from the test cache, and a cached doc guard never read the doc: %q", line)
	}
	// The pinned toolchain is part of the command too: without the tag the suite skips its LLVM legs and
	// reports a green that means nothing (the same trap ADR 0313 records for a shard built without it).
	if !strings.Contains(line, "-tags=llvm20") {
		t.Errorf("`make test` must build against the pinned LLVM: %q", line)
	}
}
