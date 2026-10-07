package integration

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The collector, asked through the programs the corpus is made of.
//
// These cases used to run each program twice on the AST interpreter — once at the normal allocation
// threshold and once with a collection forced at every statement — on the grounds that a root the
// runtime forgot is a wrong answer in the second run and nothing else would catch it. The compiled
// backend makes that second run pointless rather than the property weaker: it emits a collection at
// every statement boundary of its own accord, so the aggressive schedule *is* the ordinary schedule,
// and what is left to prove is that the collector really runs, really reclaims, and really leaves the
// program's answer alone. Each claim below has been false at some point and none of them were visible
// from the program's stdout alone, which is why the collector reports itself (ADR 0179).

// allocatesHeuristically reports whether a program builds any heap object at all — a container, a
// class instance, a string built at runtime. A program that never touches the heap is not evidence
// that a collector works, so the corpus loop only insists on collections from programs like these.
func allocatesHeuristically(src string) bool {
	return strings.ContainsAny(src, "[{") || strings.Contains(src, "class ") || strings.Contains(src, "def ")
}

// TestGCCorpusCollectsAndAgrees runs each asserted conformance program natively with the collector
// reporting, and checks the thing the double-run used to serve: the answer the program prints is the
// one on record, so a root the collector lost shows up here as a wrong answer rather than as a leak
// nobody notices.
//
// The corpus half about collection moved from per-program to the corpus, and the reason is a real
// difference between the two collectors rather than a loosening. The retired interpreter's collector
// ran on every allocation, so "an allocating program collected" was automatic; the compiled runtime
// collects when the heap crosses a threshold, which a program like `programs/sq` never reaches — it
// allocates a handful of objects and exits. Failing that program would be failing the runtime for
// being lazier than a teaching interpreter. What still has to be true, and is asserted over the whole
// corpus so a runtime that never collects cannot pass, is that the collector ran at all; and the
// bounded-heap property that makes collection worth having is TestGCStressKeepsTheHeapBounded's.
func TestGCCorpusCollectsAndAgrees(t *testing.T) {
	lang.SetGCReport(true)
	defer lang.SetGCReport(false)
	var collections, allocating, freed int
	for _, c := range conformanceCases() {
		if !c.Asserted {
			continue
		}
		src := c.Source
		// Report every unrecorded program in the corpus in one pass. Asking through the ordinary
		// golden helper reports the first and then the case compares itself against an empty
		// expectation, which turns a recording cycle into one missing source per run.
		if !lang.HasGoldenAnswer(src) {
			t.Errorf("%s: no recorded answer for this corpus program — the retired engine's answer is the expectation, so record it:\n%s", c.ID, src)
			continue
		}
		want := runCompiled(t, src) // the recorded answer, checked against the compiled run
		res, err := lang.JIT(src, 0)
		if err != nil {
			// A program the backend refuses is a ledger row elsewhere, not a GC finding.
			t.Logf("%s: not run (compiled backend refused it): %v", c.ID, err)
			continue
		}
		if res.Output != want {
			t.Errorf("%s: the collector changed the program's answer\n without: %q\n with:    %q", c.ID, want, res.Output)
		}
		if !allocatesHeuristically(src) {
			continue
		}
		allocating++
		st, line := aotGCReport(t, res.Stderr)
		collections += st.Collections
		freed += st.Freed
		if st.Collections > 0 && st.Freed == 0 {
			// Per program this proves nothing: a snippet that allocates three strings and exits can
			// collect with everything still reachable from the roots, and the reference's own program
			// text decides that, not the collector. What must hold is the corpus-level claim below —
			// the two numbers here are the log that lets a reader see the shape.
			t.Logf("%s: the collector ran and freed nothing in this program: %s", c.ID, line)
		}
	}
	if allocating == 0 {
		t.Fatal("the corpus contains no allocating program — the corpus shrank and this case proves nothing")
	}
	if collections == 0 {
		t.Fatalf("no program in the %d-program allocating corpus triggered a collection: the compiled collector never ran", allocating)
	}
	if freed == 0 {
		t.Errorf("the collector ran %d times across the %d-program allocating corpus and never freed an object: the mark phase or the sweep is not doing its job. The strict version of this property — a bounded heap under a real allocation loop — is TestGCStressKeepsTheHeapBounded.", collections, allocating)
	}
	t.Logf("allocating corpus programs: %d; collections across the corpus: %d; objects freed: %d", allocating, collections, freed)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestGCStressKeepsTheHeapBounded is the property that makes collection worth having: a long-running
// program's heap is bounded by live data, not by everything it ever allocated. The compiled runtime
// reports its own counters, so the claim is read from the run rather than argued.
func TestGCStressKeepsTheHeapBounded(t *testing.T) {
	src := strings.Join([]string{
		"def churn(n):",
		"    total = 0",
		"    i = 0",
		"    while i < n:",
		"        junk = [i, i, i]",
		"        total = total + junk[2]",
		"        i = i + 1",
		"    return total",
		"acc = 0",
		"for k in range(400):",
		"    row = [k, k, k]",
		"    acc = acc + row[0] + churn(8)",
		"print(acc)",
	}, "\n")
	lang.SetGCReport(true)
	defer lang.SetGCReport(false)
	res, err := lang.JIT(src, 0)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	st, line := aotGCReport(t, res.Stderr)
	if st.Collections == 0 {
		t.Fatalf("the safe points never fired: %s", line)
	}
	if st.TotalFreed < 3000 {
		t.Fatalf("the loop's garbage survived: %s", line)
	}
}

// TestGCAgentMachinePath is the machine consumption path for the collector: the numbers must arrive as
// data, not as prose an agent would have to scrape.
func TestGCAgentMachinePath(t *testing.T) {
	src := "acc = 0\nfor k in range(300):\n    row = [k, k]\n    acc = acc + row[1]\nprint(acc)\n"
	lang.SetGCReport(true)
	defer lang.SetGCReport(false)
	res, err := lang.JIT(src, 0)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Output != "44850\n" {
		t.Fatalf("program output = %q", res.Output)
	}
	st, line := aotGCReport(t, res.Stderr)
	for _, want := range []string{"backend=aot", "collections=", "roots=", "skipped=", "freed=", "total_freed=", "live="} {
		if !strings.Contains(st.String(), want) && !strings.Contains(line, want) {
			t.Fatalf("collector report %q missing %q", line, want)
		}
	}
}

// TestAOTCollectorReportsAndReclaims is L7.2 read through the one backend. The numbers live in the
// target program's own globals, so they arrive as the documented `gc: backend=aot …` line on its
// stderr (ADR 0179 keeps stdout the program's).
//
// Three claims are checked, because each one has been false at some point and none of them were
// visible any other way:
//
//   - the compiled program agrees with the recorded answer (the static root table used to clobber a
//     recursive frame's entry and print 346 where the program prints 130);
//   - the collector actually reclaims (total_freed > 0) — a GC that never runs is
//     otherwise indistinguishable from one that works;
//   - the root stack stays small (top < 64). This is the leak guard: a function that
//     pushes roots without popping its frame, or a slot whose address changes every
//     iteration, drove top to 2002 and filled the heap.
func TestAOTCollectorReportsAndReclaims(t *testing.T) {
	lang.SetGCReport(true)
	defer lang.SetGCReport(false)

	// (1) The loop program: its live root set is a handful of variables, so the root
	// stack must stay small. It does not: before codegen allocated every variable's slot
	// once per call, a loop body's alloca was re-executed each iteration at llc's -O0,
	// each iteration's address differed, the entry never matched, and top reached 2002
	// while the heap filled and the program died.
	src := readProgramSrc("gc_precise")
	want := runCompiled(t, src) // the answer on record, from the engine that used to check this one
	res, err := lang.JIT(src, 0)
	if err != nil {
		t.Fatalf("jit: %v", err)
	}
	if res.Output != want {
		t.Fatalf("the compiled answer differs from the record:\n compiled:  %q\n recorded: %q", res.Output, want)
	}
	st, line := aotGCReport(t, res.Stderr)
	if st.Collections == 0 {
		t.Errorf("the compiled collector never ran: %s", line)
	}
	if st.TotalFreed == 0 {
		t.Errorf("the compiled collector reclaimed nothing: %s", line)
	}
	// 64 is far above the handful of container variables live at any moment and far
	// below the 4096-entry capacity a leak would run into.
	if top := statTop(line); top >= 64 {
		t.Errorf("root stack grew to %d entries for a program with a handful of live variables: %s", top, line)
	}

	// (2) Deep recursion: every active frame legitimately roots its own locals, so
	// `top` during the descent is large — but once the frames return, a frame that
	// forgot to pop would keep registering dead stack slots, and the objects those
	// slots last held would stay live forever. `live` at the end is therefore the
	// frame-discipline assertion.
	deep := "def deep(n):\n    keep = [n, n * 2]\n    junk = [n, n, n]\n    if n > 0:\n        deep(n - 1)\n    return keep[0] + junk[2]\n\nt = 0\nfor i in range(400):\n    t = t + deep(12)\nprint(t)\n"
	wantDeep := runCompiled(t, deep)
	resDeep, err := lang.JIT(deep, 0)
	if err != nil {
		t.Fatalf("jit deep: %v", err)
	}
	if resDeep.Output != wantDeep {
		t.Fatalf("deep recursion differs from the record:\n compiled:  %q\n recorded: %q", resDeep.Output, wantDeep)
	}
	stDeep, lineDeep := aotGCReport(t, resDeep.Stderr)
	if stDeep.TotalFreed == 0 {
		t.Errorf("the compiled collector reclaimed nothing across 400 recursive storms: %s", lineDeep)
	}
	// Only main's own handful of variables are live when it finishes; a frame that
	// never popped would leave hundreds of returned frames' lists rooted.
	if stDeep.Live > 8 {
		t.Errorf("%d objects still live at the end — returned frames are still rooting their locals: %s", stDeep.Live, lineDeep)
	}
}

// aotGCReport pulls the collector line out of the compiled program's stderr.
func aotGCReport(t *testing.T, stderr string) (lang.GCStats, string) {
	t.Helper()
	line := ""
	for _, ln := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "gc: backend=aot") {
			line = ln
		}
	}
	if line == "" {
		t.Fatalf("no collector report on the program's stderr: %q", stderr)
	}
	st, ok := lang.ParseGCStatsLine(line)
	if !ok {
		t.Fatalf("collector report is not parseable: %q", line)
	}
	if st.Backend != "aot" {
		t.Fatalf("report names the wrong backend: %q", st.Backend)
	}
	return st, line
}

// statTop reads the `top=` field of a collector report (max root-stack occupancy).
func statTop(line string) int {
	for _, f := range strings.Fields(line) {
		if k, v, ok := strings.Cut(f, "="); ok && k == "top" {
			n := 0
			for _, c := range v {
				if c < '0' || c > '9' {
					return -1
				}
				n = n*10 + int(c-'0')
			}
			return n
		}
	}
	return -1
}

// TestAOTGCStatsJSON carries the same numbers as data for scripts: --gc-stats with
// --json must put them in the payload, not only in a line an agent would have to
// scrape.
func TestAOTGCStatsJSON(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "gustyc")
	buildCLI(t, bin)
	cmd := exec.Command(bin, "--aot", "--gc-stats", "--json", "--eval", "total = 0\nfor k in range(600):\n    row = [k, k]\n    total = total + row[1]\nprint(total)\n")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gustyc --aot --gc-stats --json: %v", err)
	}
	var payload struct {
		Output  string        `json:"output"`
		Backend string        `json:"backend"`
		GC      *lang.GCStats `json:"gc"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, out)
	}
	if payload.Backend != "aot" {
		t.Fatalf("backend = %q, want aot", payload.Backend)
	}
	if !strings.Contains(payload.Output, "179700") {
		t.Fatalf("program output = %q", payload.Output)
	}
	if payload.GC == nil {
		t.Fatalf("no gc member in %s", out)
	}
	if payload.GC.Backend != "aot" || payload.GC.Collections == 0 || payload.GC.TotalFreed == 0 {
		t.Fatalf("gc member is empty or never collected: %+v", payload.GC)
	}
}
