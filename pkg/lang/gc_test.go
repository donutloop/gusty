package lang

import (
	"strings"
	"testing"
)

// runGC evaluates src with the interpreter, returning the final value and the
// evaluator so a test can read the heap and the collector's self-report.
// threshold is the allocation pressure that triggers a collection (0 = default).
func runGC(t *testing.T, src string, threshold int64) (int64, *Evaluator) {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ev := NewEvaluator()
	if threshold > 0 {
		ev.SetGCAllocThreshold(threshold)
	}
	v, err := ev.EvalProgram(prog)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	return v, ev
}

// TestGCFramesKeepRecursionLive is the frame-root test. Each recursive frame
// binds a list in its *locals*, then calls a helper whose loop takes collection
// safe points — including a full GC, which is the only pass that sweeps old
// objects. At that moment the evaluator's current environment is the helper's,
// the lists were allocated long ago, and nothing but the suspended frames above
// references them. A root set without call frames sweeps them out from under the
// recursion that is about to read them back.
func TestGCFramesKeepRecursionLive(t *testing.T) {
	src := `
def pressure():
    i = 0
    acc = 0
    while i < 600:
        junk = [i, i]
        acc = acc + junk[0]
        i = i + 1
    return acc

def rec(n):
    xs = [n, n + 1]
    z = 0
    if n > 0:
        rec(n - 1)
    pressure()
    return xs[0] + xs[1]

rec(6)
`
	v, ev := runGC(t, src, 8)
	if want := int64(2*6 + 1); v != want { // rec(6) binds [6, 7]
		t.Fatalf("frame locals were collected while their frames were live: got %d, want %d", v, want)
	}
	st := ev.GCStats()
	if st.Collections == 0 {
		t.Fatal("expected the collector to run during the program")
	}
	if st.Frames == 0 {
		t.Fatalf("expected call frames in the root set, got %+v", st)
	}
}

// TestGCLoopBodyReclaimsGarbage: a top-level loop that binds a fresh list every
// iteration must not grow the heap with the dead ones. Before precise roots the
// collector only ran between top-level statements, so one statement accumulated
// every allocation it ever made.
func TestGCLoopBodyReclaimsGarbage(t *testing.T) {
	src := `
total = 0
i = 0
while i < 400:
    xs = [i, i, i, i]
    total = total + xs[0]
    i = i + 1
total
`
	_, ev := runGC(t, src, 32)
	live := len(ev.heap)
	if live > 64 {
		t.Fatalf("loop kept %d heap objects alive; the collector should reclaim dead iterations", live)
	}
	st := ev.GCStats()
	if st.Collections == 0 || st.TotalFreed == 0 {
		t.Fatalf("expected collections and reclaimed objects, got %+v", st)
	}
}

// TestGCFunctionCallsReclaimAcrossIterations: garbage made inside a callee is
// unreachable once the call returns; the loop's next safe point must reclaim it.
func TestGCFunctionCallsReclaimAcrossIterations(t *testing.T) {
	src := `
def make_row(i):
    a = [i, i]
    b = [i, i, i]
    c = {"k": i}
    return a[0] + b[0] + c["k"]

total = 0
i = 0
while i < 3000:
    total = total + make_row(i)
    i = i + 1
total
`
	v, ev := runGC(t, src, 32)
	if v != 13495500 { // 3 * (0 + ... + 2999)
		t.Fatalf("got %d, want 13495500", v)
	}
	// Nine thousand allocations, and the only bound is the collector: without the
	// safe points all 9000 containers would still be live here.
	if live := len(ev.heap); live > 600 {
		t.Fatalf("callee garbage accumulated: %d heap objects still live", live)
	}
	if ev.GCStats().TotalFreed < 5000 {
		t.Fatalf("callee garbage was never reclaimed: %+v", ev.GCStats())
	}
}

// TestGCPreciseRootsSkipImmediates: root slots that hold raw ints are proved
// non-handles and never scanned. A conservative collector would guess at all of
// them, so `skipped` is the number precision saves.
func TestGCPreciseRootsSkipImmediates(t *testing.T) {
	src := `
a = 1
b = 2
c = 3
d = 4
e = 5
f = "text"
g = [1, 2]
h = a + b + c + d + e
h
`
	_, ev := runGC(t, src, 0)
	ev.Collect()
	st := ev.GCStats()
	if st.Skipped < 5 {
		t.Fatalf("expected immediate roots to be skipped, got %+v", st)
	}
	if st.Roots == 0 {
		t.Fatalf("expected the container roots to be traced, got %+v", st)
	}
}

// TestGCDeadFrameReleasesItsLocals: once a call returns, the objects only its
// frame referenced become collectable.
func TestGCDeadFrameReleasesItsLocals(t *testing.T) {
	src := `
def temp():
    xs = [1, 2, 3, 4, 5]
    return 0

i = 0
while i < 40:
    temp()
    i = i + 1
i
`
	_, ev := runGC(t, src, 0)
	before := ev.GCStats()
	ev.Collect()
	after := ev.GCStats()
	if after.Freed < 30 {
		t.Fatalf("dead frames retained their locals: freed=%d before=%+v", after.Freed, before)
	}
	if after.Frames != 0 {
		t.Fatalf("collection at a statement boundary should have no active frames, got %+v", after)
	}
}

// TestGCLoopIterableSurvivesBodyPressure: the iterable of a for loop lives in a
// Go local of the loop, not in any frame, so the loop must declare it as a root
// group before running its body.
func TestGCLoopIterableSurvivesBodyPressure(t *testing.T) {
	src := `
def pressure():
    i = 0
    while i < 30:
        t = [i]
        i = i + 1

rows = [[1, 2], [3, 4], [5, 6]]
total = 0
for r in rows:
    pressure()
    total = total + r[0] + r[1]
total
`
	v, _ := runGC(t, src, 8)
	if v != 21 {
		t.Fatalf("loop iterable was collected: got %d, want 21", v)
	}
}

// TestGCLoopOverFreshLiteral: the iterable is created by the loop itself, so it
// is young at first — and then becomes the loop's own root as iterations run.
func TestGCLoopOverFreshLiteral(t *testing.T) {
	src := `
def pressure():
    i = 0
    while i < 30:
        t = [i]
        i = i + 1

total = 0
for x in [10, 20, 30]:
    pressure()
    total = total + x
total
`
	v, _ := runGC(t, src, 8)
	if v != 60 {
		t.Fatalf("fresh loop literal was collected: got %d, want 60", v)
	}
}

// TestGCStressModePreservesSemantics runs the same programs with a collection at
// every statement boundary: a root the interpreter forgot is a wrong answer here.
func TestGCStressModePreservesSemantics(t *testing.T) {
	programs := []struct {
		src  string
		want string
	}{
		{"x = [1, 2, 3]\nlen(x)", "3"},
		{"def f(n):\n    xs = [n]\n    return xs[0] + 1\nf(41)", "42"},
		{"total = 0\nfor i in range(50):\n    ys = [i, i]\n    total = total + ys[1]\ntotal", "1225"},
		{"class A:\n    def __init__(self, v):\n        self.v = v\n    def get(self):\n        return self.v\nprint(A(7).get())\n0", "0"},
		{"d = {}\nfor i in range(20):\n    d[str(i)] = [i, i]\nlen(d)", "20"},
		{"def outer():\n    xs = [1, 2, 3]\n    def inner():\n        return len(xs)\n    return inner()\nouter()", "3"},
		{"try:\n    x = [1]\n    raise ValueError(\"boom\")\nexcept ValueError:\n    x = [2, 3]\nlen(x)", "2"},
		{"def gen():\n    for i in range(5):\n        yield [i]\nlist(gen())[4][0]", "4"},
	}
	for _, p := range programs {
		for _, stress := range []bool{false, true} {
			prog, err := Parse(p.src)
			if err != nil {
				t.Fatalf("parse %q: %v", p.src, err)
			}
			ev := NewEvaluator()
			ev.SetGCAllocThreshold(1)
			ev.SetGCStress(stress)
			v, err := ev.EvalProgram(prog)
			if err != nil {
				t.Fatalf("eval %q (stress=%v): %v", p.src, stress, err)
			}
			if got := ev.Repr(v); got != p.want {
				t.Fatalf("program %q (stress=%v) = %s, want %s", p.src, stress, got, p.want)
			}
		}
	}
}

// TestGCLongLivedSessionStaysBounded is the REPL property: evaluating many
// independent inputs through one evaluator must not accumulate their garbage.
func TestGCLongLivedSessionStaysBounded(t *testing.T) {
	ev := NewEvaluator()
	ev.SetGCAllocThreshold(8)
	for i := 0; i < 2000; i++ {
		prog, err := Parse("xs = [1, 2, 3, 4]\nys = {\"a\": xs}\nlen(ys)")
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if _, err := ev.EvalProgram(prog); err != nil {
			t.Fatalf("eval: %v", err)
		}
	}
	// Without a collector that runs during evaluation this is one object set per
	// input (6000 live). With it, the session is bounded by the full-GC threshold
	// instead of by how long the session has been open.
	if live := len(ev.heap); live > 700 {
		t.Fatalf("session accumulated %d heap objects over 2000 inputs", live)
	}
}

// TestGCStatsReport: the collector must describe itself, in a shape a machine can
// read as well as a human.
func TestGCStatsReport(t *testing.T) {
	_, ev := runGC(t, "i = 0\nwhile i < 60:\n    xs = [i]\n    i = i + 1\ni", 16)
	st := ev.GCStats()
	if st.Backend != "interpreter" {
		t.Fatalf("stats must name the backend, got %+v", st)
	}
	line := st.String()
	for _, want := range []string{"gc: backend=interpreter", "collections=", "roots=", "skipped=", "marked=", "freed=", "live=", "frames=", "protected="} {
		if !strings.Contains(line, want) {
			t.Fatalf("stats line %q missing %q", line, want)
		}
	}
}

// TestGCCollectionNeverRunsUnbounded: the number of collections is driven by a
// constant allocation threshold, so the same program collects the same number of
// times on every run (determinism is what makes parity assertions possible).
func TestGCCollectionCountIsDeterministic(t *testing.T) {
	src := "total = 0\nfor i in range(200):\n    xs = [i, i]\n    total = total + xs[0]\ntotal"
	var first int
	for run := 0; run < 3; run++ {
		_, ev := runGC(t, src, 16)
		st := ev.GCStats()
		if run == 0 {
			first = st.Collections
		} else if st.Collections != first {
			t.Fatalf("collection count drifted: run %d made %d collections, first made %d", run, st.Collections, first)
		}
	}
	if first == 0 {
		t.Fatal("expected collections under allocation pressure")
	}
}

// TestGCStatementCallKeepsFrameLocals is the case frame rooting exists for: the
// callee binds a list before its loop, the loop's safe points make that list
// collectable, and nothing but the callee's frame references it. The call is a
// statement, so the callee's body takes its own safe points.
func TestGCStatementCallKeepsFrameLocals(t *testing.T) {
	src := `
def work():
    xs = [7, 8, 9]
    total = 0
    for i in range(300):
        junk = [i, i, i]
        total = total + xs[0] + xs[1] + xs[2]
    return total

work()
`
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	ev := NewEvaluator()
	ev.SetGCAllocThreshold(16)
	v, err := ev.EvalProgram(prog)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if v != 7200 { // 300 * (7+8+9)
		t.Fatalf("frame local was collected while the frame was live: got %d, want 7200", v)
	}
	st := ev.GCStats()
	if st.Collections == 0 || len(ev.heap) > 64 {
		t.Fatalf("the callee's loop never reclaimed anything: %d live, %+v", len(ev.heap), st)
	}
	if st.Frames == 0 {
		t.Fatalf("expected call frames to appear in the root set, got %+v", st)
	}
}

// TestGCNestedCallStaysConservative: a call made *inside* an expression is not a
// safe point — the enclosing expression holds temporaries no root set can see —
// so a value only that expression references must survive the nested call.
func TestGCNestedCallStaysConservative(t *testing.T) {
	src := `
def pressure():
    i = 0
    while i < 40:
        junk = [i]
        i = i + 1
    return 0

def pick(xs, k):
    pressure()
    return xs[k]

pick([11, 22, 33], 1) + 0
`
	v, _ := runGC(t, src, 4)
	if v != 22 {
		t.Fatalf("temporary argument of a nested call was collected: got %d, want 22", v)
	}
}
