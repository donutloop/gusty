package integration

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// TestGCStressCorpus runs every shared conformance program twice through the
// interpreter: once with the collector's normal allocation threshold, and once
// with a collection forced at *every* statement boundary. A root the interpreter
// forgot is a wrong answer or a crash in the second run, so this is the harness
// that makes the precise root set (L7.2, ADR 0181) a tested property instead of
// an argument. It also asserts the stressed run really collected — a probe that
// never runs proves nothing.
func TestGCStressCorpus(t *testing.T) {
	for _, c := range conformanceCases() {
		if !c.Shared {
			continue
		}
		normal, err := lang.InterpreterRun(c.Source)
		stressed, stats, serr := lang.InterpreterRunOpts(c.Source, lang.InterpreterRunOptions{
			GCStress:         true,
			GCAllocThreshold: 1,
		})
		if (err == nil) != (serr == nil) {
			t.Fatalf("%s: normal err=%v, gc-stressed err=%v", c.ID, err, serr)
		}
		if normal != stressed {
			t.Fatalf("%s: gc stress changed the program's output\n normal:   %q\n stressed: %q", c.ID, normal, stressed)
		}
		if err != nil {
			// A program that traps must trap the same way under collection.
			if !strings.Contains(serr.Error(), firstLine(err.Error())) {
				t.Fatalf("%s: gc stress changed the failure: %v vs %v", c.ID, err, serr)
			}
			continue
		}
		if stats.Collections == 0 {
			t.Fatalf("%s: gc stress collected nothing at all: %+v", c.ID, stats)
		}
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestGCStressKeepsTheHeapBounded is the property that makes collection worth
// having: a long-running program's heap is bounded by live data, not by everything
// it ever allocated. The same program under no collection grows linearly, so the
// comparison is the assertion.
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
	_, stressed, err := lang.InterpreterRunOpts(src, lang.InterpreterRunOptions{GCAllocThreshold: 16})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if stressed.Collections == 0 {
		t.Fatalf("the safe points never fired: %+v", stressed)
	}
	if stressed.TotalFreed < 3000 {
		t.Fatalf("the loop's garbage survived: %+v", stressed)
	}
}

// TestGCAgentMachinePath: --gc-stats is the machine consumption path for the
// collector, so its numbers must arrive as data, not as prose to scrape.
func TestGCAgentMachinePath(t *testing.T) {
	src := "acc = 0\nfor k in range(300):\n    row = [k, k]\n    acc = acc + row[1]\nprint(acc)\n"
	out, stats, err := lang.InterpreterRunOpts(src, lang.InterpreterRunOptions{GCAllocThreshold: 8})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "44850\n" {
		t.Fatalf("program output = %q", out)
	}
	line := stats.String()
	for _, want := range []string{"backend=interpreter", "collections=", "roots=", "skipped=", "freed=", "total_freed=", "live="} {
		if !strings.Contains(line, want) {
			t.Fatalf("collector report %q missing %q", line, want)
		}
	}
}

// TestAOTCollectorReportsAndReclaims is the compiled backend's half of L7.2. The
// numbers live in the target program's own globals, so they arrive as the documented
// `gc: backend=aot …` line on its stderr (ADR 0179 keeps stdout the program's).
//
// Three claims are checked, because each one has been false at some point in this
// round and none of them were visible any other way:
//
//   - the compiled program and the interpreter agree on the answer (the static root
//     table used to clobber a recursive frame's entry and print 346 where the
//     interpreter printed 130);
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
	want, err := lang.InterpreterRun(src)
	if err != nil {
		t.Fatalf("interpreter run: %v", err)
	}
	res, err := lang.JIT(src, 0)
	if err != nil {
		t.Fatalf("jit: %v", err)
	}
	if res.Output != want {
		t.Fatalf("compiled answer differs from the interpreter:\n aot:  %q\n interp: %q", res.Output, want)
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
	wantDeep, err := lang.InterpreterRun(deep)
	if err != nil {
		t.Fatalf("interpreter deep run: %v", err)
	}
	resDeep, err := lang.JIT(deep, 0)
	if err != nil {
		t.Fatalf("jit deep: %v", err)
	}
	if resDeep.Output != wantDeep {
		t.Fatalf("deep recursion differs from the interpreter:\n aot:  %q\n interp: %q", resDeep.Output, wantDeep)
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
