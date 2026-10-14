package lang

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The compiled heap is one fixed arena, and `rt_alloc` answers -1 when it cannot hand out a slot. That
// -1 was roadmap Gap Q.2: no caller asked, so a program with more live containers than the arena holds
// stored -1 as a handle and the object arithmetic after it read and wrote the words *before* the arena —
// measured at exit 0, printing a number the program never wrote:
//
//	v0 = [0, 0] … v1029 = [1029, 1029] / r = r + len(v_i) … / print(r)
//	  compiled: 2120        CPython: 2060
//
// What ships is a trap the program can name (`MemoryError`, exit 3, catchable) at every road that hands
// a handle to something that can read it, and the arena's own facts (`capacity=`, `headroom=`,
// `exhausted=`) in the self-report. These tests ask the emitter, as tables, the questions the programs
// can only sample: which calls reached the arena, and which of them carry the guard.
//
// ADR 0347. The behavioural half — real exit codes, the report, CPython — is integration/
// heap_exhaustion_test.go.

// heapRoadProbe is one shape that reaches the arena by a distinct road. `src` must compile; the road's
// own call is what the guard has to stand beside.
var heapRoadProbe = []struct {
	name string
	src  string
}{
	{"list literal bound to a name", "w = []\nw.append(1)\na = [w[0], 2]\nprint(len(a))\n"},
	{"list literal at top level", "xs = [1, 2]\nprint(len(xs))\n"},
	{"dict literal", "d = {1: 2}\nprint(len(d))\n"},
	{"set literal", "s = {1, 2}\nprint(len(s))\n"},
	{"dict with a text key built at run time", "dd = {}\nk = 3\ndd[k] = k\nprint(len(dd))\n"},
	{"slice of a runtime list", "a = []\na.append(1)\na.append(2)\nb = a[0:2]\nprint(len(b))\n"},
	{"whole-list copy", "a = []\na.append(1)\nb = a[:]\nprint(len(b))\n"},
	{"list comprehension", "c = [i for i in [1, 2, 3]]\nprint(len(c))\n"},
	{"dict comprehension", "c = {i: i for i in [1, 2]}\nprint(len(c))\n"},
	{"set comprehension", "c = {i for i in [1, 2]}\nprint(len(c))\n"},
	{"class instance", "class C:\n    x = 1\no = C()\nprint(1)\n"},
	{"float box out of an operator", "def half(k):\n    return k / 2\nw = []\nw.append(3)\nprint(half(w[0]))\n"},
	{"float box out of a mixed addition", "w = []\nw.append(3)\nprint(w[0] + 0.25)\n"},
	{"sorted copy", "a = [3, 1, 2]\na.sort()\nprint(len(a))\n"},
	{"append into a grown list", "t = []\nt.append(1)\nt.append(2)\nprint(len(t))\n"},
	{"loop body allocation (the churn shape)", "total = 0\ni = 0\nwhile i < 4:\n    junk = [i, i]\n    total = total + junk[1]\n    i = i + 1\nprint(total)\n"},
	{"container loop body allocation", "xs = [1, 2, 3]\nn = 0\nfor x in xs:\n    tmp = [x, x]\n    n = n + 1\nprint(n)\n"},
}

var (
	roadCallRe   = regexp.MustCompile(`call\s+(?:i32|void)\s+@([a-z_0-9]+)\(`)
	dstOfCallRe  = regexp.MustCompile(`^+\s*(%[a-zA-Z_0-9.]+) = call`)
	statusOfRe   = regexp.MustCompile(`^+\s*(%[a-zA-Z_0-9.]+) = call i32 @([a-z_0-9]+)\(`)
	arenaArrayRe = regexp.MustCompile(`@(heap|gc_mark|free_next|estr|heap_tags|inst_set) = internal global \[(\d+) x `)
	allocBoundRe = regexp.MustCompile(`icmp sge i32 %c, (\d+)`)
)

// programBodies returns just the module's own code — @main and the emitted @gy_* functions — because the
// runtime blocks legitimately allocate inside themselves. A guard measured over the whole module would
// count the runtime's own rt_alloc calls as unguarded roads and say nothing about the program.
func programBodies(ir string) []string {
	var bodies []string
	var cur []string
	inBody := false
	for _, ln := range strings.Split(ir, "\n") {
		if strings.HasPrefix(ln, "define ") {
			if inBody {
				bodies = append(bodies, strings.Join(cur, "\n"))
			}
			inBody = strings.Contains(ln, "@main(") || strings.Contains(ln, "@gy_")
			cur = nil
			continue
		}
		if inBody && ln == "}" {
			bodies = append(bodies, strings.Join(cur, "\n"))
			inBody = false
			continue
		}
		if inBody {
			cur = append(cur, ln)
		}
	}
	if inBody {
		bodies = append(bodies, strings.Join(cur, "\n"))
	}
	return bodies
}

// handleRoadUnguarded reports the first handle road in body whose call is not followed by the guard: the
// comparison against zero on the call's own destination register, and the `heapfull` block it branches to.
func handleRoadUnguarded(body string) []string {
	var bad []string
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		m := roadCallRe.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil || !hasStr(handleReturningCalls, m[1]) {
			continue
		}
		dst := dstOfCallRe.FindStringSubmatch(ln)
		if dst == nil {
			continue // a void call: nothing hands a handle back
		}
		window := ""
		if i+7 < len(lines) {
			window = strings.Join(lines[i:i+7], "\n")
		}
		cmp := fmt.Sprintf("icmp slt i32 %s, 0", dst[1])
		if !strings.Contains(window, cmp) || !strings.Contains(window, "heapfull") {
			bad = append(bad, fmt.Sprintf("%s → %s", m[1], strings.TrimSpace(ln)))
		}
	}
	return bad
}

// statusRoadUnguarded reports the first status road (rt_num_arith: a status back, the handle through an
// out-param) whose status register is never tested against the arena's fourth status.
func statusRoadUnguarded(body string) []string {
	var bad []string
	lines := strings.Split(body, "\n")
	for i, ln := range lines {
		m := statusOfRe.FindStringSubmatch(ln)
		if m == nil || !hasStr(statusRoads, m[2]) {
			continue
		}
		if !strings.Contains(body[i:], fmt.Sprintf("icmp eq i32 %s, 4", m[1])) {
			bad = append(bad, fmt.Sprintf("%s → %s", m[2], strings.TrimSpace(ln)))
		}
	}
	return bad
}

func hasStr(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

func TestEveryHandleRoadCarriesTheGuard(t *testing.T) {
	t.Parallel()
	for _, p := range heapRoadProbe {
		ir := mustIR(t, p.src)
		var bad []string
		for _, b := range programBodies(ir) {
			bad = append(bad, handleRoadUnguarded(b)...)
		}
		if len(bad) > 0 {
			t.Errorf("%s: a call handed back a heap handle with no guard beside it: %s", p.name, bad[0])
		}
	}
}

// TestEveryStatusRoadRaisesOnExhaustion is the other half of the coverage claim. `rt_num_arith` allocates
// the box for a double answer inside the door and returns a status, so there is no handle for the guard
// to compare: the door answers a fourth status and both callers raise it. A call whose status is never
// tested against 4 is the -1 smuggled through memory.
func TestEveryStatusRoadRaisesOnExhaustion(t *testing.T) {
	t.Parallel()
	seen := 0
	for _, p := range heapRoadProbe {
		ir := mustIR(t, p.src)
		for _, b := range programBodies(ir) {
			if bad := statusRoadUnguarded(b); len(bad) > 0 {
				t.Errorf("%s: %s reached the arena and no status branch raises for it: %s", p.name, statusRoads, bad[0])
			}
			seen += strings.Count(b, "rt_num_arith(")
		}
	}
	if seen == 0 {
		t.Fatal("the probe set reached no status road at all: the test would pass on an empty table")
	}
}

// TestEveryHandleRoadCarriesTheGuardAcrossTheCorpus asks the same question of every program the
// conformance corpus holds, which is what turns "the roads a test happened to reach" into a claim about
// the language (the argument in heap_guard.go § Why every road).
func TestEveryHandleRoadCarriesTheGuardAcrossTheCorpus(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob("../../integration/programs/*.gy")
	if err != nil || len(files) < 100 {
		t.Fatalf("the corpus walk needs the conformance programs; found %d", len(files))
	}
	compiled, roads, statusCalls := 0, 0, 0
	for _, f := range files {
		src, rerr := os.ReadFile(f)
		if rerr != nil {
			continue
		}
		prog, perr := Parse(string(src))
		if perr != nil {
			continue
		}
		ir, gerr := GenerateIR(prog)
		if gerr != nil {
			continue // a refused program has no roads to guard
		}
		compiled++
		for _, b := range programBodies(ir) {
			if bad := handleRoadUnguarded(b); len(bad) > 0 {
				t.Errorf("%s: unguarded handle road: %s", filepath.Base(f), bad[0])
			}
			if bad := statusRoadUnguarded(b); len(bad) > 0 {
				t.Errorf("%s: unguarded status road: %s", filepath.Base(f), bad[0])
			}
			roads += strings.Count(b, "call i32 @rt_alloc(") + strings.Count(b, "call i32 @rt_slice(") +
				strings.Count(b, "call i32 @rt_list_copy(") + strings.Count(b, "call i32 @rt_float_new(")
			statusCalls += strings.Count(b, "call i32 @rt_num_arith(")
		}
	}
	t.Logf("%d corpus programs; %d guarded handle calls, %d guarded status calls", compiled, roads, statusCalls)
	if compiled < 100 || roads < 200 || statusCalls < 20 {
		t.Fatalf("the corpus walk covered too little to mean anything: %d programs, %d handle calls, %d status calls",
			compiled, roads, statusCalls)
	}
}

// TestTheGuardIsNotEmittedWhereTheHeapNeverRunsOut is the negative control: a program that asks the arena
// for nothing pays neither the branch nor the interned sentence. Without this the guard could be shipped
// as a blanket cost and the tests above would still pass.
func TestTheGuardIsNotEmittedWhereTheHeapNeverRunsOut(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print(1 + 1)\n",
		"i = 0\nwhile i < 4:\n    i = i + 1\nprint(i)\n",
		"def f(a, b):\n    return a * b + 1\nprint(f(3, 4))\n",
	} {
		ir := mustIR(t, src)
		if strings.Contains(ir, "heapfull") {
			t.Errorf("a program that never reaches the arena still emits the guard:\n%s", src)
		}
		if strings.Contains(ir, "MemoryError") || strings.Contains(ir, "out of memory") {
			t.Errorf("a program that never reaches the arena carries the trap's sentence:\n%s", src)
		}
	}
	// And the guard is real: the same emitter, one allocation, and both appear.
	ir := mustIR(t, "xs = [1, 2]\nprint(len(xs))\n")
	if !strings.Contains(ir, "heapfull") || !strings.Contains(ir, heapExhaustedMessage) {
		t.Fatal("a program that allocates emits no guard: the negative control above proves nothing")
	}
}

// TestTheArenaSpellsItsCapacityOnce is the drift tripwire. The raise's sentence and the report's
// `capacity=` are generated from HeapCapacity; the arena's arrays and the allocator's bound are written
// into the runtime by hand. One of those two groups changing alone is how a backend ends up reporting a
// headroom against an arena that is not the one it allocates from, so they are pinned together here —
// and the row that grows the arena (Gap Q.4) starts by having to change this constant.
func TestTheArenaSpellsItsCapacityOnce(t *testing.T) {
	t.Parallel()
	ir := mustIR(t, "xs = [1, 2]\nys = {'k': 1}\nss = {3}\nprint(len(xs) + len(ys) + len(ss))\n")
	seen := map[string]int{}
	for _, m := range arenaArrayRe.FindAllStringSubmatch(ir, -1) {
		seen[m[1]]++
		if m[2] != fmt.Sprint(HeapCapacity) {
			t.Errorf("@%s is declared with capacity %s, but HeapCapacity is %d", m[1], m[2], HeapCapacity)
		}
	}
	for _, name := range []string{"heap", "gc_mark", "free_next", "heap_tags"} {
		if seen[name] == 0 {
			t.Errorf("the tripwire saw no @%s declaration, so it cannot check it", name)
		}
	}
	bounds := allocBoundRe.FindAllStringSubmatch(ir, -1)
	if len(bounds) == 0 {
		t.Fatal("the allocator's bound is not where the test can reach it")
	}
	for _, b := range bounds {
		if b[1] != fmt.Sprint(HeapCapacity) {
			t.Errorf("rt_alloc bounds the arena at %s while HeapCapacity says %d", b[1], HeapCapacity)
		}
	}
	if !strings.Contains(ir, "capacity=%d") || !strings.Contains(ir, "headroom=%d") || !strings.Contains(ir, "exhausted=%d") {
		t.Error("--gc-stats' self-report line does not carry the arena's three facts")
	}
	if want := fmt.Sprintf("out of memory: the compiled heap holds %d live containers", HeapCapacity); !strings.Contains(ir, want) {
		t.Errorf("the trap's sentence does not name the capacity the allocator bounds at: %q", heapExhaustedMessage)
	}
}

// TestLoopBodiesAreSafePoints pins the other half of why the trap means what it says: a loop mints an
// object per iteration, and unless the collector gets a turn inside the body, the arena fills with
// garbage and an honest program dies of exhaustion. A loop that cannot allocate is left alone.
func TestLoopBodiesAreSafePoints(t *testing.T) {
	t.Parallel()
	churn := []struct {
		name string
		src  string
		want bool
	}{
		{"while allocating a list", "t = 0\ni = 0\nwhile i < 3:\n    junk = [i, i]\n    t = t + junk[1]\n    i = i + 1\nprint(t)\n", true},
		{"for-range allocating a list", "t = 0\nfor i in range(3):\n    junk = [i]\n    t = t + 1\nprint(t)\n", true},
		{"for over a container, allocating", "xs = [1, 2, 3]\nn = 0\nfor x in xs:\n    tmp = [x]\n    n = n + 1\nprint(n)\n", true},
		{"nested function body allocating", "def churn(n):\n    i = 0\n    while i < n:\n        junk = [i]\n        i = i + 1\n    return n\nprint(churn(3))\n", true},
		{"while that cannot allocate", "s = 0\ni = 0\nwhile i < 4:\n    s = s + i\n    i = i + 1\nprint(s)\n", false},
		{"for-range that cannot allocate", "s = 0\nfor i in range(4):\n    s = s + i\nprint(s)\n", false},
	}
	for _, c := range churn {
		ir := mustIR(t, c.src)
		got := false
		for _, b := range programBodies(ir) {
			for _, ln := range strings.Split(b, "\n") {
				if strings.Contains(ln, "br label %") { // the loop's blocks; the safe point must be among them
					got = got || strings.Contains(b, "call void @rt_gc(")
				}
			}
		}
		// The loop body collects iff it can allocate: count the collections between the body label and the
		// back edge rather than over the module, where a function's statement boundaries also collect.
		got = loopBodyHasCollection(ir)
		if got != c.want {
			t.Errorf("%s: loop body collects = %v, want %v", c.name, got, c.want)
		}
	}
}

// loopBodyHasCollection reports whether the module's loop bodies — between a body label and the branch
// that returns to the condition — contain a collection.
func loopBodyHasCollection(ir string) bool {
	lines := strings.Split(ir, "\n")
	for i, ln := range lines {
		if !strings.HasSuffix(ln, ":") || !strings.Contains(ln, "body") {
			continue
		}
		for _, inner := range lines[i:] {
			if strings.Contains(inner, "br label %") {
				break
			}
			if strings.Contains(inner, "call void @rt_gc(") {
				return true
			}
		}
	}
	return false
}

// TestCollectorMarksOnlyWhatTheTagCallsAnObject is the table behind the fix that made the trap survivable:
// the collector walks a container's slots, and marking every payload as a handle treats the 7 in [7] as an
// index and retains whatever object lives at slot 7 — so a churn loop pinned a dead object per iteration
// and walked the arena to its ceiling. The four immediate kinds must not be marked.
func TestCollectorMarksOnlyWhatTheTagCallsAnObject(t *testing.T) {
	t.Parallel()
	ir := mustIR(t, "xs = [1, 2]\nys = {'k': 1}\nprint(len(xs) + len(ys))\n")
	if !strings.Contains(ir, "define internal i32 @rt_gc_tag_is_obj(i32 %t)") {
		t.Fatal("the collector has no tag question to ask a slot")
	}
	body := ir[min(len(ir), strings.Index(ir, "@rt_gc_tag_is_obj")):]
	if i := strings.Index(body, "define internal i32 @rt_gc_tag_is_obj"); i >= 0 {
		body = body[i:]
		body = body[:min(len(body), strings.Index(body, "\n}\n")+2)]
	}
	for _, tag := range []string{"int", "bool", "None", "str"} {
		_ = tag // named in the comment; the numbers are what the IR compares
	}
	for _, n := range []int{0, 2, 3, 4} { // int, bool, None, interned text
		if !strings.Contains(body, fmt.Sprintf("icmp eq i32 %s, %d", "%t", n)) {
			t.Errorf("rt_gc_tag_is_obj does not exclude tag %d, so a slot holding that immediate pins a random object", n)
		}
	}
	if !strings.Contains(ir, "call i32 @rt_gc_tag_is_obj(i32 %etg)") {
		t.Error("the collector's element walk does not ask the tag before marking")
	}
}

// TestMemoryErrorIsAClassTheProgramCanName: the trap is only handleable if the class exists in the one
// exception table, is numbered, and is what the guard raises.
func TestMemoryErrorIsAClassTheProgramCanName(t *testing.T) {
	t.Parallel()
	if !isExnClass("MemoryError") {
		t.Error("MemoryError is not an exception class: `except MemoryError:` cannot name it")
	}
	if code := exnClassCode("MemoryError"); code != 10 {
		t.Errorf("MemoryError's runtime code is %d; the class table in docs/language.md and the compiled raise sites agree on 10", code)
	}
	if !builtinExceptions()["MemoryError"] {
		t.Error("the checker's name table does not carry MemoryError, so `except MemoryError:` is an undefined name")
	}
	for _, e := range exnClasses {
		if e.code < 0 {
			t.Errorf("exception class %s has a negative code", e.name)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
