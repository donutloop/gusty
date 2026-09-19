package lang

import (
	"testing"
)

// TestMemoryModelCollectsDeadList: rebinding x from [1,2,3] to [4] leaves the
// old list unreachable from the top-level environment, so the mark-and-sweep
// GC (per top-level statement) must free it.
func TestMemoryModelCollectsDeadList(t *testing.T) {
	ev := NewEvaluator()
	prog, err := Parse("x = [1, 2, 3]\nx = [4]\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := ev.EvalProgram(prog); err != nil {
		t.Fatalf("eval: %v", err)
	}
	ev.Collect() // memory model: explicit GC frees unreachable pure-data objects
	for id, o := range ev.heap {
		if o.kind == "list" && len(o.elems) == 3 {
			t.Fatalf("dead [1,2,3] list still live (id=%d)", id)
		}
	}
	// the live [4] list must still be present
	found := false
	for _, o := range ev.heap {
		if o.kind == "list" && len(o.elems) == 1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("live [4] list was collected")
	}
}

// TestMemoryModelKeepsLiveList: two live variables keep both lists reachable,
// so the GC must not free either.
func TestMemoryModelKeepsLiveList(t *testing.T) {
	ev := NewEvaluator()
	prog, err := Parse("x = [1, 2, 3]\ny = [4]\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := ev.EvalProgram(prog); err != nil {
		t.Fatalf("eval: %v", err)
	}
	ev.Collect()
	found3 := false
	for _, o := range ev.heap {
		if o.kind == "list" && len(o.elems) == 3 {
			found3 = true
		}
	}
	if !found3 {
		t.Fatalf("live [1,2,3] list was collected")
	}
}

// TestMemoryModelKeepsClosureEnv: a closure stored in a variable captures an
// environment; the GC must keep the captured list reachable.
func TestMemoryModelKeepsClosureEnv(t *testing.T) {
	ev := NewEvaluator()
	prog, err := Parse("x = [1, 2, 3]\nf = lambda: x\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := ev.EvalProgram(prog); err != nil {
		t.Fatalf("eval: %v", err)
	}
	ev.Collect()
	found := false
	for _, o := range ev.heap {
		if o.kind == "list" && len(o.elems) == 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("list captured by closure env was collected")
	}
}

// TestMemoryModelRebindLoopReclaims: rebinding a list var thousands of times
// makes each old list unreachable; the interpreter GC must reclaim them all,
// leaving only the final live list reachable.
func TestMemoryModelRebindLoopReclaims(t *testing.T) {
	ev := NewEvaluator()
	var prog string
	for i := 0; i < 3000; i++ {
		prog += "x = [3, 4]\n"
	}
	p, err := Parse("x = [1, 2]\n" + prog)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := ev.EvalProgram(p); err != nil {
		t.Fatalf("eval: %v", err)
	}
	ev.Collect()
	// after GC only the final live [3,4] should remain reachable.
	live2 := 0
	for _, o := range ev.heap {
		if o != nil && o.kind == "list" && len(o.elems) == 2 {
			live2++
		}
	}
	if live2 > 3 {
		t.Fatalf("rebind loop left %d live 2-elem lists reachable (expected ~1 after GC)", live2)
	}
}

func TestGenerationalGC(t *testing.T) {
	ev := NewEvaluator()
	prog, err := Parse("g = [1, 2, 3]\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := ev.EvalProgram(prog); err != nil {
		t.Fatalf("eval: %v", err)
	}
	gid := ev.Vars["g"]
	if gid <= 0 || ev.heap[gid] == nil {
		t.Fatalf("global list not allocated")
	}
	// a nursery object not reachable from roots must be reclaimed by a young GC,
	// while the reachable global is promoted to the old generation.
	drop := ev.allocObj("list")
	ev.Collect() // young GC
	if ev.heap[drop] != nil {
		t.Fatal("unreachable nursery object should be reclaimed by young GC")
	}
	if ev.heap[gid] == nil {
		t.Fatal("reachable global should be promoted and survive the young GC")
	}
	// a second young GC only traces the new nursery: the promoted (old) global
	// must survive.
	ev.Collect()
	if ev.heap[gid] == nil {
		t.Fatal("promoted (old) global should survive subsequent young GCs")
	}
}
