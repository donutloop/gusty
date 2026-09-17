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
