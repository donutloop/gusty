package integration

// gc_heap_growth_test.go — roadmap Gap Q.2's second half, ADR 0350: the compiled arena grows, the
// machine path says so, and the reference agrees with the programs the old ceiling killed.
//
// ADR 0347 put the wall in words (`MemoryError`, exit 3, `capacity=`/`headroom=`/`exhausted=`); this
// file is about the wall moving. `@heap_cap` is the window `rt_alloc` has committed for the run: it
// starts at `lang.HeapInitialCapacity` and doubles until it reaches `lang.HeapCapacity`, and `grown=`
// counts the widenings — the field that lets a caller read `capacity=2048` as "this arena grew" rather
// than "this backend's arena is 2048".
//
// The reference leg matters more here than anywhere else in the family: a program that dies with
// MemoryError and a program that prints the wrong number both look like failures from the exit code
// alone, and the whole claim of growth is that a program which was told it is too big for the backend
// is, in fact, a program the backend can run — exactly like CPython does.

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// gcArenaProgram builds the same program the unit suite asks: `buckets` groups of `per` containers, all
// reachable, plus the group lists and the outer list — and a print of two counts that are only right if
// every one of those objects is still there to be read.
func gcArenaProgram(buckets, per int) string {
	return "def make_group(size, base):\n" +
		"    g = []\n" +
		"    i = 0\n" +
		"    while i < size:\n" +
		"        g.append([base + i, (base + i) * 2])\n" +
		"        i = i + 1\n" +
		"    return g\n\n" +
		"def fill(groups, count, size):\n" +
		"    k = 0\n" +
		"    while k < count:\n" +
		"        groups.append(make_group(size, k * size))\n" +
		"        k = k + 1\n" +
		"    return count\n\n" +
		"groups = []\n" +
		fmt.Sprintf("fill(groups, %d, %d)\n", buckets, per) +
		"shown = 0\nfor gr in groups:\n    shown = shown + 1\nprint(shown, len(groups[0]))\n"
}

// gcStatsFromJSON runs the built binary with --gc-stats --json and hands back the machine object: the
// path an agent reads, where the arena's numbers arrive as data rather than as a line to scrape.
func gcStatsFromJSON(t *testing.T, bin, file string) (*lang.GCStats, string) {
	t.Helper()
	cmd := exec.Command(bin, "--aot", "--gc-stats", "--json", "--file", file)
	out, err := cmd.Output()
	if err != nil {
		// A trap is a program result, not a harness failure: the payload still carries the gc member.
		if ee, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("gustyc --aot --gc-stats --json --file %s: %v", file, err)
		} else if len(ee.Stderr) > 0 && len(out) == 0 {
			out = ee.Stderr
		}
	}
	var payload struct {
		GC *lang.GCStats `json:"gc"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("payload is not JSON: %v\n%s", err, out)
	}
	return payload.GC, string(out)
}

// arenaDoublings counts the doublings from the window a run starts in to `to` — the arithmetic the
// runtime's own counter is supposed to agree with.
func arenaDoublings(to int) int {
	if to < lang.HeapInitialCapacity {
		return -1
	}
	n, w := 0, lang.HeapInitialCapacity
	for w < to {
		w *= 2
		n++
	}
	return n
}

// TestAOTArenaGrowthMatchesTheReference is the reference leg: every one of these programs printed a
// MemoryError traceback at exit 3 on the commit before ADR 0350, and CPython has printed two numbers all
// along.
func TestAOTArenaGrowthMatchesTheReference(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []struct {
		name         string
		buckets, per int
		want         string
		wantCapacity int
	}{
		{"fits the window it starts in", 4, 200, "4 200", lang.HeapInitialCapacity},
		{"one container past the old ceiling", 5, 205, "5 205", lang.HeapInitialCapacity},
		{"past the first doubling", 6, 200, "6 200", lang.HeapInitialCapacity * 2},
		{"past the second doubling", 12, 200, "12 200", lang.HeapInitialCapacity * 4},
		{"the reservation", 40, 200, "40 200", lang.HeapCapacity},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			src := gcArenaProgram(r.buckets, r.per)
			want, ok := cpythonPlainOut(t, dir, src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if strings.TrimSpace(want) != r.want {
				t.Fatalf("the reference itself answers %q and this row's want column said %q — the row is wrong, not the compiler", strings.TrimSpace(want), r.want)
			}
			bin := filepath.Join(t.TempDir(), "gustyc")
			buildCLI(t, bin)
			file := writeSrc(t, dir, "arena_growth", src)
			got, code := cliRunMerged(t, "--aot", "--file", file)
			if code != 0 {
				t.Fatalf("a program the arena has room for exited %d, want 0:\n%s", code, got)
			}
			if strings.Contains(got, "MemoryError") {
				t.Fatalf("the arena refused a program it has room for:\n%s", got)
			}
			if line := arenaPrintedLine(got); line != r.want {
				t.Fatalf("compiled printed %q, the reference printed %q", line, r.want)
			}
			st, raw := gcStatsFromJSON(t, bin, file)
			if st == nil {
				t.Fatalf("--json carried no gc member:\n%s", raw)
			}
			if st.Capacity < lang.HeapInitialCapacity || st.Capacity > lang.HeapCapacity ||
				st.Capacity&(st.Capacity-1) != 0 {
				t.Errorf("capacity=%d is not a window this arena can commit (initial %d, reservation %d, powers of two only)",
					st.Capacity, lang.HeapInitialCapacity, lang.HeapCapacity)
			}
			if st.Capacity < r.wantCapacity {
				t.Errorf("capacity=%d, want at least %d for a program holding %d containers",
					st.Capacity, r.wantCapacity, r.buckets*r.per)
			}
			if st.Grown != arenaDoublings(st.Capacity) {
				t.Errorf("grown=%d does not agree with capacity=%d, which is %d doublings from the initial window",
					st.Grown, st.Capacity, arenaDoublings(st.Capacity))
			}
			if want := arenaDoublings(r.wantCapacity); st.Grown < want {
				t.Errorf("grown=%d, want at least %d: this program could not have been answered without widening", st.Grown, want)
			}
			if st.Exhausted != 0 {
				t.Errorf("exhausted=%d on a run that answered the program", st.Exhausted)
			}
			if st.Headroom < 0 || st.Headroom > st.Capacity {
				t.Errorf("headroom=%d is outside the window the run committed (%d)", st.Headroom, st.Capacity)
			}
		})
	}
}

// TestAOTProgramThatFitsNeverGrows is the negative control for a claim a backend can too easily make:
// growing eagerly would report the biggest window every time and make the collector walk slots the
// program never asked for. A program the initial window holds reports that window, and grown=0.
func TestAOTProgramThatFitsNeverGrows(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	bin := filepath.Join(t.TempDir(), "gustyc")
	buildCLI(t, bin)
	for _, src := range []string{
		"xs = [1, 2, 3]\nprint(len(xs))\n",
		gcArenaProgram(2, 50),
	} {
		file := writeSrc(t, dir, "arena_fits", src)
		st, raw := gcStatsFromJSON(t, bin, file)
		if st == nil {
			t.Fatalf("--json carried no gc member:\n%s", raw)
		}
		if st.Capacity != lang.HeapInitialCapacity {
			t.Errorf("a program the initial window holds reported capacity=%d, want %d", st.Capacity, lang.HeapInitialCapacity)
		}
		if st.Grown != 0 {
			t.Errorf("a program the initial window holds reported grown=%d, want 0", st.Grown)
		}
	}
}

// TestAOTArenaGrowthIsBoundedAndStillTraps is ADR 0347's promise at the new ceiling: the window stops at
// the reservation, the trap is still a class the program can name, and the report still says which number
// the run hit. It also carries the timing claim — before ADR 0350 removed the collector's outer pass loop
// a program of this size did not finish inside the toolchain budget at all.
func TestAOTArenaGrowthIsBoundedAndStillTraps(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// HeapCapacity/200 groups of 200 and then some: more containers than the reservation holds.
	src := gcArenaProgram(42, 200)
	file := writeSrc(t, dir, "arena_full", src)
	started := time.Now()
	got, code := cliRunMerged(t, "--aot", "--gc-stats", "--file", file)
	took := time.Since(started)
	if code != 3 {
		t.Fatalf("a program past the reservation exited %d, want 3 (a trap, ADR 0211):\n%s", code, got)
	}
	if !strings.Contains(got, "MemoryError: "+lang.HeapExhaustedMessage()) {
		t.Errorf("the trap did not name the arena the way heap_guard.go spells it:\n%s", got)
	}
	if !strings.Contains(got, "Traceback") {
		t.Errorf("the trap arrived without a traceback:\n%s", got)
	}
	st, line := aotGCStatsFrom(t, got)
	if st.Capacity != lang.HeapCapacity {
		t.Errorf("capacity=%d, want the reservation %d", st.Capacity, lang.HeapCapacity)
	}
	if st.Grown < 1 {
		t.Errorf("grown=%d on a run that reached the reservation: the arena never widened, so the ceiling the run hit is not the one the code declares", st.Grown)
	}
	if st.Exhausted == 0 {
		t.Errorf("exhausted=0 on a run that trapped with the arena full: %s", line)
	}
	// The quadratic collector is gone: this program asks for more than eight thousand containers, and
	// with the outer pass loop back in rt_gc it took over the two-minute toolchain budget rather than a
	// fraction of a second. The bound is deliberately loose — a loaded CI box, a cold cache — and is here
	// to catch a factor-of-the-arena regression, not a 30% wobble.
	if took > 60*time.Second {
		t.Errorf("a program holding %d containers took %s: the collector is scanning the arena more than once per collection", 42*200, took)
	}
}

// TestAOTArenaGrowthProgramFileMatchesCPython runs the corpus file that IS the row's Definition of done —
// one container past the ceiling the arena used to stop at — on both legs.
func TestAOTArenaGrowthProgramFileMatchesCPython(t *testing.T) {
	t.Parallel()
	prog := heapTrapProgram(t, "gc_heap_growth.gy")
	body, err := os.ReadFile(prog)
	if err != nil {
		t.Fatalf("read %s: %v", prog, err)
	}
	dir := t.TempDir()
	// The file's own text: its `#` lines are comments to CPython as much as to gusty, which is what
	// makes a corpus program a reference leg at all (L11.9's rule).
	want, ok := cpythonPlainOut(t, dir, string(body))
	if !ok {
		t.Skip("no python3 available to cross-check")
	}
	got, code := cliRunMerged(t, "--aot", "--file", prog)
	if code != 0 {
		t.Fatalf("the row's own program exited %d, want 0:\n%s", code, got)
	}
	if strings.Contains(got, "MemoryError") {
		t.Fatalf("the row's own program was told it is too big for the backend:\n%s", got)
	}
	if arenaPrintedLine(got) != strings.TrimSpace(want) {
		t.Errorf("compiled printed %q, the reference printed %q", arenaPrintedLine(got), strings.TrimSpace(want))
	}
}

// arenaPrintedLine is the program's own first line of output, with the tool channel's collector report
// filtered out of merged output.
func arenaPrintedLine(merged string) string {
	for _, ln := range strings.Split(merged, "\n") {
		t := strings.TrimSpace(ln)
		if t == "" || strings.HasPrefix(t, "gc: backend=") {
			continue
		}
		return t
	}
	return ""
}
