package integration

// gc_heap_trap_test.go — roadmap Gap Q.2, ADR 0347: the compiled arena is finite, and
// running out of it is a language event with four witnesses.
//
// `@heap` holds lang.HeapCapacity containers. Until this cycle an exhausted arena was
// reported by handing the program the handle -1, which the reader then painted as
// whatever bits were in slot -1: a churn loop that outlived the arena printed another
// iteration's list and exited 0. One case per question an author or an agent actually asks:
//
//   * did my program die, and did it say why?      → TrapsMemoryError
//   * can my program catch it?                     → IsHandleable
//   * can I see the arena from the machine path?   → GCStatsCarriesTheArena
//   * did the guard cost the working path anything?→ GuardDoesNotDisturbAHeapThatFits
//
// The last is the reference leg: the same allocation roads, run at a size the arena holds
// comfortably, checked against CPython — so a guard that turns a working program into a
// wrong one cannot pass this file, and the trap cases cannot be green while ordinary
// allocation is broken.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// heapTrapProgram is the path of a corpus program that asks the arena for more than it holds.
func heapTrapProgram(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("programs", name))
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Skipf("%s is missing: %v", name, err)
	}
	return p
}

// aotGCStatsFrom pulls the collector's own line out of merged output and parses it.
func aotGCStatsFrom(t *testing.T, merged string) (lang.GCStats, string) {
	t.Helper()
	for _, ln := range strings.Split(merged, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "gc: backend=aot") {
			st, ok := lang.ParseGCStatsLine(ln)
			if !ok {
				t.Fatalf("the collector's line does not parse: %q", ln)
			}
			return st, ln
		}
	}
	t.Fatalf("no collector report in the run:\n%s", merged)
	return lang.GCStats{}, ""
}

// TestAOTHeapExhaustionTrapsMemoryError is the witness for the condition nobody catches:
// the program dies, it dies naming the class, and the collector's report says the arena
// was full on the way out.
func TestAOTHeapExhaustionTrapsMemoryError(t *testing.T) {
	t.Parallel()
	prog := heapTrapProgram(t, "gc_heap_uncaught.gy")
	out, code := cliRunMerged(t, "--aot", "--file", prog, "--gc-stats")
	if code != 3 {
		t.Errorf("an exhausted heap exited %d, want 3 (a trap, ADR 0211): %s", code, out)
	}
	if !strings.Contains(out, "MemoryError: "+lang.HeapExhaustedMessage()) {
		t.Errorf("the trap did not name the arena:\n%s", out)
	}
	if !strings.Contains(out, "Traceback") {
		t.Errorf("the trap arrived without a traceback:\n%s", out)
	}
	st, line := aotGCStatsFrom(t, out)
	if st.Capacity != lang.HeapCapacity {
		t.Errorf("capacity=%d, want the one spelling %d the allocator bounds with", st.Capacity, lang.HeapCapacity)
	}
	if st.Exhausted == 0 {
		t.Errorf("the run exhausted the arena and the report does not say so: %s", line)
	}
	if st.Headroom != 0 {
		t.Errorf("headroom=%d on a run that trapped with the arena full: %s", st.Headroom, line)
	}
}

// TestAOTHeapExhaustionIsHandleable is the other half of the point: a trap the program
// cannot name is not a trap. Both arms — the class and its base — have to reach it, and
// the program must finish at exit 0.
func TestAOTHeapExhaustionIsHandleable(t *testing.T) {
	t.Parallel()
	prog := heapTrapProgram(t, "gc_heap_exhaustion.gy")
	out, code := cliRunMerged(t, "--aot", "--file", prog)
	if code != 0 {
		t.Errorf("a handled MemoryError left the program at exit %d, want 0:\n%s", code, out)
	}
	if strings.Contains(out, "Traceback") {
		t.Errorf("a caught trap printed a traceback:\n%s", out)
	}
	for _, want := range []string{"MemoryError caught", "Exception caught"} {
		if !strings.Contains(out, want) {
			t.Errorf("the program did not report %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "no trap") {
		t.Errorf("the arena never filled, so the case proves nothing:\n%s", out)
	}
}

// TestAOTGCStatsCarriesTheArena is the machine consumption path: the capacity arrives as
// data, so an agent can ask how much headroom a workload has without scraping prose and
// without knowing the compile-time constant.
func TestAOTGCStatsCarriesTheArena(t *testing.T) {
	t.Parallel()
	prog := heapTrapProgram(t, "gc_heap_exhaustion.gy")
	bin := filepath.Join(t.TempDir(), "gustyc")
	buildCLI(t, bin)
	cmd := exec.Command(bin, "--aot", "--gc-stats", "--json", "--file", prog)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("gustyc --aot --gc-stats --json: %v", err)
	}
	var payload struct {
		GC *lang.GCStats `json:"gc"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, out)
	}
	if payload.GC == nil {
		t.Fatalf("--json carried no gc member: %s", out)
	}
	if payload.GC.Capacity != lang.HeapCapacity {
		t.Errorf("gc.capacity = %d, want the one spelling %d", payload.GC.Capacity, lang.HeapCapacity)
	}
	if payload.GC.Headroom < 0 || payload.GC.Headroom > lang.HeapCapacity {
		t.Errorf("gc.headroom = %d is not inside the arena", payload.GC.Headroom)
	}
	if payload.GC.Headroom+payload.GC.Live > lang.HeapCapacity {
		t.Errorf("gc.headroom %d + gc.live %d exceeds the arena %d",
			payload.GC.Headroom, payload.GC.Live, lang.HeapCapacity)
	}
}

// TestAOTGuardDoesNotDisturbAHeapThatFits is the reference leg for the roads this cycle
// put a guard on: the same allocation doors, run at a size the arena holds comfortably,
// checked against CPython's answer. It is the case that keeps "guarded" from meaning
// "raises when it should have worked".
func TestAOTGuardDoesNotDisturbAHeapThatFits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
	}{
		{"retained growth", "def push_n(grown, n):\n    i = 0\n    while i < n:\n        grown.append([1, 2, 3])\n        i = i + 1\n    return i\n\nbucket = []\npush_n(bucket, 40)\nprint(len(bucket))\nprint(len(bucket[7]))\nprint(bucket[7][2])\n"},
		{"float boxes in a loop", "xs = [1.5, 2.5, 3.5, 4.5]\ntotal = 0.0\ni = 0\nwhile i < len(xs):\n    total = total + xs[i] / 2.0\n    i = i + 1\nprint(total)\n"},
		{"text roads", "print(\",\".join([\"a\", \"b\", \"c\"]))\nprint(\"-\".join([\"x\", \"y\", \"z\"]))\nprint(len(\"abc\" + str(42)))\nprint(str(1.5) + \"|\" + str([1, 2]))\n"},
		{"nested containers", "rows = []\nfor k in range(6):\n    rows.append([k, k + 1])\nprint(len(rows))\nprint(rows[2][0])\nprint(rows[5][1])\n"},
		{"a caught trap still runs", "def push_n(grown, n):\n    i = 0\n    while i < n:\n        grown.append([1])\n        i = i + 1\n    return i\n\nxs = []\ntry:\n    push_n(xs, 5)\nexcept MemoryError:\n    print(\"not raised\")\nprint(len(xs))\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			got, code := cliRunMerged(t, "--aot", "--file", writeSrc(t, dir, "heap_fits", r.src))
			if code != 0 {
				t.Fatalf("a heap that fits exited %d, want 0:\n%s", code, got)
			}
			if strings.Contains(got, "MemoryError") {
				t.Fatalf("a heap that fits raised MemoryError:\n%s", got)
			}
			if got != want {
				t.Fatalf("compiled %q, CPython prints %q", got, want)
			}
		})
	}
}
