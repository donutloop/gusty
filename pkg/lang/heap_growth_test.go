package lang

// heap_growth_test.go — roadmap Gap Q.2's second half, ADR 0350: the compiled arena GROWS.
//
// ADR 0347 made exhaustion a language event — `MemoryError`, catchable, reported — and left the ceiling
// where it was: `@heap` was `[1024 x {i32, i32, [256 x i32]}]` and a program that held more containers
// than that alive at once died naming the class. Measured on the commit before this row, the program in
// `arenaGrowthSource(6, 200)` — twelve hundred containers, all reachable — printed
//
//	gustyc: jit: … MemoryError: out of memory: the compiled heap holds 1024 live containers
//
// at exit 3 where CPython prints `6 200`. The class was honest; the ceiling was the defect.
//
// A handle in this runtime is an INDEX, not a pointer, which is what makes growth cheap: widening the
// window `rt_alloc` may hand out needs no reference fixed up anywhere, only a store to `@heap_cap`. The
// reservation (the arrays themselves) is one zero-filled block each, so a program that never needs the
// room never touches the pages, and the seven tables that share the handle space cannot drift because
// they are bounded by the same global.
//
// The rows below ask three questions of the emitter and the JIT: does a program the old ceiling killed
// now ANSWER (with the record's answer beside it), does the collector's per-collection work stay linear
// in the arena (the outer pass loop ADR 0350 removed made a 2400-container program take 15s), and does
// the trap road ADR 0347 built still stand at the ceiling the arena grows up to.

import (
	"fmt"
	"strings"
	"testing"
)

// arenaGrowthSource writes a program that keeps buckets*per containers alive at once — every one of them
// reachable from a name the program still holds, so no collection can hand a slot back — and prints two
// numbers that are only right if every container is still there to be counted.
//
// Two constraints keep the program an honest test of the arena rather than a walk over its edge: no
// container is filled past the 256 element words one object owns, and nothing is aliased and then
// rebound, which is the retention bug roadmap Gap R.257 files.
func arenaGrowthSource(buckets, per int) string {
	return arenaGrowthPrelude() + arenaGrowthWork(buckets, per, false)
}

// arenaGrowthCatchSource is the same program with the allocation inside `try: / except MemoryError:`, so
// the ceiling can be reached and handled in one run — the shape docs/language.md documents and the CLI
// suite checks against the reference's exit code.
func arenaGrowthCatchSource(buckets, per int) string {
	return arenaGrowthPrelude() + arenaGrowthWork(buckets, per, true)
}

func arenaGrowthPrelude() string {
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
		"groups = []\n"
}

func arenaGrowthWork(buckets, per int, caught bool) string {
	fill := fmt.Sprintf("fill(groups, %d, %d)\n", buckets, per)
	if caught {
		fill = "try:\n    " + fill + "except MemoryError:\n    print(\"caught at the ceiling\")\n"
	}
	return fill + "shown = 0\nfor gr in groups:\n    shown = shown + 1\nprint(shown, len(groups[0]))\n"
}

// arenaGrowthRows are the sizes the arena has to meet. `want` is what the reference prints for the same
// program — never what this file remembers — and `grown` is how many doublings the window needs to get
// there from HeapInitialCapacity, which the CLI suite asserts against the report.
var arenaGrowthRows = []struct {
	name    string
	buckets int
	per     int
	want    string
}{
	// the size the fixed arena was built for: a program that fits must not pay for the growth road
	{"fits the window it starts in", 4, 200, "4 200"},
	// the row's own program: ONE container past the ceiling the arena used to stop at
	{"one container past the old ceiling", 5, 205, "5 205"},
	// twelve hundred live containers: the window has to have widened once
	{"twelve hundred live containers", 6, 200, "6 200"},
	// past the first doubling: the window has to have widened twice
	{"twenty-four hundred live containers", 12, 200, "12 200"},
	// three doublings, and the collector walking a 8192-slot window per collection
	{"eight thousand live containers", 40, 200, "40 200"},
}

// TestTheArenaGrowsForAProgramThatNeedsTheRoom is the row's answer: every one of these programs printed
// the reference's numbers at exit 3 with a MemoryError traceback on the commit before it. A refusal is a
// failure here, and so is a trap — the containers are reachable, so nothing about the program asked to
// be told it is out of memory.
func TestTheArenaGrowsForAProgramThatNeedsTheRoom(t *testing.T) {
	t.Parallel()
	for _, r := range arenaGrowthRows {
		src := arenaGrowthSource(r.buckets, r.per)
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("jit: %v", err)
			}
			if res.Code == 2 {
				t.Fatalf("the module was rejected by the toolchain (exit 2, ADR 0166):\n%s", res.Stderr)
			}
			if _, class, message := ParseTrapReport(res.Stderr); class != "" {
				t.Fatalf("the program trapped with %s: %s where the reference prints %q\n%s",
					class, message, r.want, res.Output)
			}
			if got := strings.TrimSpace(res.Output); got != r.want {
				t.Fatalf("printed %q, want the reference's %q", got, r.want)
			}
			// The record leg: what the retired engine answered for this source, and what the
			// reference answers today.
			if out, err := runGoldenStdout(t, src); err == nil {
				if strings.TrimSpace(out) != r.want {
					t.Fatalf("the record answers %q for this source, want %q", strings.TrimSpace(out), r.want)
				}
			}
		})
	}
}

// TestTheArenaStillTrapsAtTheCeilingItGrewTo is ADR 0347's promise kept one number later: growth moves
// the wall, it does not remove it. A program that outlives even the reservation still dies with the
// class it can name, at exit 3, and the guard road — not a garbage handle — is what decides it.
func TestTheArenaStillTrapsAtTheCeilingItGrewTo(t *testing.T) {
	t.Parallel()
	// HeapCapacity + a few: the reservation, and then one container too many.
	src := arenaGrowthSource(42, 200)
	t.Run("uncaught", func(t *testing.T) {
		t.Parallel()
		_, trap := runForParity(t, src)
		if !strings.HasPrefix(trap, "MemoryError:") {
			t.Fatalf("a program past the reservation did not trap with MemoryError (got trap %q) — the arena answered a handle for a slot that does not exist", trap)
		}
		if !strings.Contains(trap, fmt.Sprintf("holds %d live containers", HeapCapacity)) {
			t.Fatalf("the trap does not name the ceiling it reached (%d): %q", HeapCapacity, trap)
		}
		if !strings.Contains(trap, fmt.Sprintf("having grown from %d", HeapInitialCapacity)) {
			t.Fatalf("the trap does not say the arena grew before it filled: %q", trap)
		}
	})
	t.Run("catchable", func(t *testing.T) {
		t.Parallel()
		out, trap := runForParity(t, arenaGrowthCatchSource(42, 200))
		if trap != "" {
			t.Fatalf("the program caught the trap and still raised %s\n%s", trap, out)
		}
		if !strings.Contains(out, "caught at the ceiling") {
			t.Fatalf("the arm did not run: %q", out)
		}
	})
}

// TestTheCollectorScansTheArenaOncePerCollection is the shape behind the timing. rt_gc used to walk an
// outer counter to @heap_count with the whole object scan inside it — O(slots squared) per collection —
// which a fixed 1024-slot ceiling and a sweep that pulled the high-water mark back down both hid. Once
// the arena could grow, a program holding 2400 containers took 15 seconds to print two numbers.
func TestTheCollectorScansTheArenaOncePerCollection(t *testing.T) {
	t.Parallel()
	src := "xs = [1, 2]\nprint(len(xs))\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	ir := res.IR
	if !strings.Contains(ir, "define internal void @rt_gc(") {
		t.Fatal("the module carries no collector to inspect")
	}
	if strings.Contains(ir, "%p.end = icmp sge i32 %p, %hc1") {
		t.Error("rt_gc scans the arena inside an outer loop bounded by @heap_count — that is the quadratic collector ADR 0350 removed")
	}
	if strings.Count(ir, "%k.end = icmp sge i32 %k, %hc1") != 1 {
		t.Error("rt_gc does not have exactly one scan of the arena bounded by @heap_count")
	}
	// The mark-clear walk is bounded by the COMMITTED window, not by the reservation: a program that
	// never grew is not paying to clear slots it never asked for.
	if !strings.Contains(ir, "%i.end = icmp sge i32 %i, %capcl") {
		t.Error("rt_gc does not clear marks across the committed window (@heap_cap)")
	}
}

// TestTheReservationIsAReservationAndNotAnAllocation is the cost claim behind the ceiling: the arena's
// tables are zero-filled globals, so a module that reserves 8192 slots writes none of them until the run
// reaches them. An arena built by writing a fixed number of slots at startup would show up here as an
// initializer.
func TestTheReservationIsAReservationAndNotAnAllocation(t *testing.T) {
	t.Parallel()
	res, err := Compile("xs = [1, 2]\nprint(len(xs))\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	for _, name := range []string{"@heap", "@heap_tags", "@estr", "@gc_mark", "@free_next"} {
		decl := name + " = internal global ["
		i := strings.Index(res.IR, decl)
		if i < 0 {
			t.Errorf("no %s declaration in the module", name)
			continue
		}
		line := res.IR[i:]
		if eol := strings.IndexByte(line, '\n'); eol >= 0 {
			line = line[:eol]
		}
		if !strings.HasSuffix(line, "] zeroinitializer") {
			t.Errorf("%s is not zero-filled at link time, so the reservation costs the program bytes it never used: %s", name, line)
		}
	}
}

// TestGrowthLeavesTheGuardInPlace keeps the two halves of the row together: an arena that widens is not
// an arena that stopped checking. Every handle road still stands beside its guard, and the raise block
// is still in the module that grew.
func TestGrowthLeavesTheGuardInPlace(t *testing.T) {
	t.Parallel()
	src := arenaGrowthSource(6, 200)
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(res.IR, "heapfull") || !strings.Contains(res.IR, "MemoryError") {
		t.Error("a program that grows the arena emitted no exhaustion guard: growth replaced the trap instead of moving its ceiling")
	}
	if !strings.Contains(res.IR, "ret i32 -1") {
		t.Error("rt_alloc no longer answers -1 when the reservation is full, so the guard has nothing to branch on")
	}
}
