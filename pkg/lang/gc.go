package lang

import (
	"os"
	"strconv"
	"strings"
)

// Precise stack roots (roadmap L7.2, ADR 0181).
//
// The interpreter heap holds every container, instance, closure, boxed string,
// boxed float and coroutine. Before this file the collector had exactly one
// root — the module environment — and nothing called it while a program ran,
// which meant two things at once: a REPL session grew without bound, and no
// collection could happen *during* a program because the values that are live
// at that moment live in Go locals the collector cannot see.
//
// The root set is now explicit, and collection happens at real safe points:
//
//   - frames:      the local scope of every active call (pushed by callFunc).
//   - rootGroups:  handles a statement keeps in Go locals while it runs a
//                  nested body — a for-loop's iterable, a with-statement's
//                  manager, a match subject, the value of the previous
//                  statement. Each construct declares them before the body.
//   - permanent:   the None singleton, the generator accumulator, the super()
//                  receiver, and every class object.
//   - gcWatermark: the soundness floor. Everything allocated after the watermark
//                  is unconditionally live, which is what makes it safe to
//                  collect while expressions are half-evaluated: their
//                  temporaries are younger than the watermark by construction.
//
// The watermark only advances at a *statement boundary with an empty expression
// stack* (exprDepth == 0) reached from a construct that declared its root
// groups. That single invariant is the whole soundness argument: when it holds,
// no interpreter frame is mid-expression, so nothing lives only in a register.
// The AOT backend mirrors the same idea with a per-slot handle tag so its
// collector knows exactly which stack slots hold handles (see codegen.go,
// @gc.kinds and rt_gc).

// gcAllocThresholdDefault is how many heap allocations may accumulate before a
// statement boundary triggers a collection. It is a constant, not a clock or a
// heuristic: two runs of one program collect at exactly the same statements,
// which is what keeps interpreter output deterministic (and parity testable).
const gcAllocThresholdDefault = 256

// GCStats is the collector's self-report. The compiled backend produces the
// same fields from its own runtime, so --gc-stats means the same thing for both
// backends and an agent can read either without scraping prose.
type GCStats struct {
	// Collections is how many mark-and-sweep passes have run.
	Collections int `json:"collections"`
	// Roots is how many root handles the last collection traced.
	Roots int `json:"roots"`
	// Skipped is how many root candidates the last collection proved were raw
	// immediates rather than handles, and never scanned. This is the number
	// precision buys: a conservative collector would have guessed at all of them.
	Skipped int `json:"skipped"`
	// Marked is how many heap objects the last collection found reachable.
	Marked int `json:"marked"`
	// Freed is how many heap objects the last collection reclaimed.
	Freed int `json:"freed"`
	// TotalFreed is cumulative across every collection this run: the size of the
	// garbage the program produced. Freed alone describes only the last pass.
	TotalFreed int `json:"total_freed"`
	// Live is how many heap objects remain after the last collection.
	Live int `json:"live"`
	// Frames is how many call frames were rooted by the last collection.
	Frames int `json:"frames"`
	// Protected is how many objects the watermark kept alive without tracing.
	Protected int `json:"protected"`
	// Generational is true when the last collection was a young (nursery) GC.
	Generational bool `json:"generational"`
	// Backend names the collector that produced these numbers.
	Backend string `json:"backend"`
}

// String renders the stats as the stable key=value line the CLI prints. One
// format for the human path and for --json, so a script can grep the human line
// while a machine reads the object.
func (s GCStats) String() string {
	var b strings.Builder
	b.WriteString("gc: backend=")
	b.WriteString(s.Backend)
	b.WriteString(" collections=")
	b.WriteString(strconv.Itoa(s.Collections))
	b.WriteString(" roots=")
	b.WriteString(strconv.Itoa(s.Roots))
	b.WriteString(" skipped=")
	b.WriteString(strconv.Itoa(s.Skipped))
	b.WriteString(" marked=")
	b.WriteString(strconv.Itoa(s.Marked))
	b.WriteString(" freed=")
	b.WriteString(strconv.Itoa(s.Freed))
	b.WriteString(" total_freed=")
	b.WriteString(strconv.Itoa(s.TotalFreed))
	b.WriteString(" live=")
	b.WriteString(strconv.Itoa(s.Live))
	b.WriteString(" frames=")
	b.WriteString(strconv.Itoa(s.Frames))
	b.WriteString(" protected=")
	b.WriteString(strconv.Itoa(s.Protected))
	if s.Generational {
		b.WriteString(" kind=young")
	} else {
		b.WriteString(" kind=full")
	}
	return b.String()
}

// gcEnvStress forces a collection at every statement boundary instead of only
// when allocation pressure asks for one. It exists so the soundness of the root
// set can be *tested* rather than assumed: the conformance corpus runs twice,
// once normally and once with this on, so a root the interpreter forgot shows up
// as a failing test instead of a heisenbug. Enabled with GUSTY_GC_STRESS=1.
func gcEnvStress() bool {
	v := os.Getenv("GUSTY_GC_STRESS")
	if v == "" {
		return false
	}
	on, err := strconv.ParseBool(v)
	return err == nil && on
}

// pushFrame roots a call frame's local scope for the duration of the call.
func (e *Evaluator) pushFrame(scope map[string]int64) {
	e.frames = append(e.frames, scope)
}

// popFrame unroots the innermost call frame. Handles that only the returned
// frame referenced become collectable from this point on — the collector never
// traces a dead frame's slots, which is what frame discipline means.
func (e *Evaluator) popFrame() {
	if n := len(e.frames); n > 0 {
		e.frames[n-1] = nil
		e.frames = e.frames[:n-1]
	}
}

// pushRoots anchors handles that a statement keeps in Go locals while it runs a
// nested body. It returns a cookie for popRoots.
func (e *Evaluator) pushRoots(vs ...int64) int {
	e.rootGroups = append(e.rootGroups, vs)
	return len(e.rootGroups) - 1
}

// runBodyRooted runs a nested statement list (a loop body, a branch, a handler)
// at a statement boundary that is safe for the collector: the construct has
// handed over every handle it keeps live across the body in `live`, so the
// watermark may advance between the body's statements and memory is reclaimed
// while a long-running loop never grows the heap.
//
// It is a function rather than inline calls so the root group is popped on every
// exit path — including break, continue and a raised exception — instead of
// leaking one root per iteration.
func (e *Evaluator) runBodyRooted(stmts []Stmt, live ...int64) (int64, error) {
	cookie := e.pushRoots(live...)
	defer e.popRoots(cookie)
	return e.evalBodySafe(stmts, true)
}

// pushRootBox anchors one mutable root slot: the returned slice aliases storage
// the caller keeps updating, so the collector sees the current value. Used for
// the "value of the last statement" that an executor holds across boundaries.
func (e *Evaluator) pushRootBox() []int64 {
	box := make([]int64, 1)
	e.rootGroups = append(e.rootGroups, box)
	return box
}

func (e *Evaluator) popRoots(cookie int) {
	if cookie >= 0 && cookie < len(e.rootGroups) {
		e.rootGroups = e.rootGroups[:cookie]
	}
}

// isHandle reports whether v names a live heap object, as opposed to a raw
// immediate (ints, bools and offsets are stored unboxed). Precise rooting
// starts here: the collector asks instead of guessing.
func (e *Evaluator) isHandle(v int64) bool {
	if v <= 0 {
		return false
	}
	o, ok := e.heap[v]
	return ok && o != nil
}

// beginStatementBody runs the collector safe point that precedes one statement.
// safe reports whether the construct owning this statement boundary has
// declared every handle it keeps live across the body in a root group.
//
// Two things happen here, under different guards:
//
//   - the watermark may advance only when this is a true safe point — no
//     expression is being evaluated anywhere (exprDepth == 0) and the owning
//     construct declared its roots. Advancing is what makes older objects
//     collectable, so it is the step that must be provably safe.
//   - collection itself always runs when allocation pressure warrants it. It
//     cannot touch anything younger than the watermark, so collecting with
//     half-evaluated expressions on the stack is sound.
func (e *Evaluator) beginStatementBody(safe bool) {
	if e.exprDepth <= e.exprBase && safe {
		e.gcWatermark = e.nextID
	}
	e.safepoint()
}

// runFuncBody runs a called function's body.
//
// A call that *is* the statement — `work()`, or `total = helper(x)` — is a real
// safe point boundary: the caller's frames above the statement boundary hold
// nothing that is not rooted, because its arguments have already been copied into
// the callee's (rooted) frame and its result does not exist yet. Such a call may
// take collection safe points of its own, which is what stops a long-running
// `def main(): ...` loop from growing the heap. A call nested inside a larger
// expression is not a safe point: the enclosing expression keeps results in Go
// locals that no root set can name, so that callee stays conservative.
func (e *Evaluator) runFuncBody(fd *FuncDef, stmtRootCall bool) (int64, error) {
	if !stmtRootCall || !e.stmtSafe {
		return e.runStatements(fd.Body, false)
	}
	prevBase := e.exprBase
	e.exprBase = e.exprDepth // measure expression depth from the call, not from its caller
	rv, err := e.runStatements(fd.Body, true)
	e.exprBase = prevBase
	return rv, err
}

// safepoint runs a collection once enough allocations have piled up since the
// last one. With GUSTY_GC_STRESS=1 it runs at every safe point.
func (e *Evaluator) safepoint() {
	if !e.gcStress && e.allocCount < e.gcThreshold {
		return
	}
	e.collect()
}

// rootHandles walks the root set, calling mark for every handle it finds. It
// reports how many roots were traced and how many were proved immediates.
func (e *Evaluator) rootHandles(mark func(int64)) (traced, skipped int) {
	root := func(v int64) {
		if e.isHandle(v) {
			traced++
			mark(v)
			return
		}
		skipped++
	}
	if e.noneVal != 0 {
		// The None singleton is a permanent root: sweeping it would hand its heap
		// slot to another object, and later values would print as "None".
		mark(e.noneVal)
		traced++
	}
	for _, id := range e.Vars {
		root(id)
	}
	for _, fr := range e.frames {
		if fr == nil {
			continue
		}
		for _, id := range fr {
			root(id)
		}
	}
	for _, grp := range e.rootGroups {
		for _, id := range grp {
			root(id)
		}
	}
	if e.yieldList != 0 {
		root(e.yieldList)
	}
	if e.curSelf != 0 {
		root(e.curSelf)
	}
	// Class objects are reachable from the class table even when no variable
	// references them: methods, fields and dynamic dispatch all read through them.
	for _, id := range e.classIDs {
		root(id)
	}
	return traced, skipped
}

// traceFrom marks an object and everything it references. Container payloads
// and the object tables (attrs, closure environments, coroutine arguments) are
// all traversed, so an object only reachable through a class, a closure env or a
// coroutine's bound arguments still survives.
func (e *Evaluator) traceFrom(id int64, marked map[int64]bool) {
	if id <= 0 || marked[id] {
		return
	}
	o, ok := e.heap[id]
	if !ok || o == nil {
		return
	}
	marked[id] = true
	switch o.kind {
	case "list", "set":
		for _, v := range o.elems {
			e.traceFrom(v, marked)
		}
	case "dict":
		for _, k := range o.elems {
			e.traceFrom(k, marked)
		}
		for _, v := range o.dvals {
			e.traceFrom(v, marked)
		}
	case "closure":
		for _, v := range o.env {
			e.traceFrom(v, marked)
		}
	}
	// class / instance / method / superproxy / coroutine kinds store ids in
	// attrs, env and (for coroutines) the bound argument list.
	for _, v := range o.attrs {
		e.traceFrom(v, marked)
	}
	for _, v := range o.env {
		e.traceFrom(v, marked)
	}
	for _, v := range o.args {
		e.traceFrom(v, marked)
	}
	if o.recv != 0 {
		e.traceFrom(o.recv, marked)
	}
	if o.result != 0 {
		e.traceFrom(o.result, marked)
	}
	if o.base != 0 {
		e.traceFrom(o.base, marked)
	}
}

// collectable reports whether an object may be reclaimed at all: nothing above
// the watermark is ever swept, because the interpreter may be holding it in a
// register right now. That floor is what lets the collector run while
// expressions are half-evaluated; GUSTY_GC_STRESS=1 raises the *frequency* of
// collections, never this guarantee.
func (e *Evaluator) collectable(id int64) bool {
	return id <= e.gcWatermark
}

// Collect runs a collection over the whole heap. Called from outside a running
// program — between REPL inputs, from tests, from the CLI — nothing is
// mid-evaluation, so it is a true safe point: the watermark advances to the
// allocation frontier first and every existing object is subject to reachability.
func (e *Evaluator) Collect() {
	e.gcWatermark = e.nextID
	e.collect()
}

// CollectProtected runs a collection without lifting the watermark, so anything
// allocated after the last safe point stays live no matter what references it.
// This is the shape the interpreter itself uses while expressions are in flight.
func (e *Evaluator) CollectProtected() { e.collect() }

// collect is the mark-and-sweep pass proper.
func (e *Evaluator) collect() {
	if len(e.heap) == 0 {
		return
	}
	marked := map[int64]bool{}
	traced, skipped := e.rootHandles(func(id int64) { e.traceFrom(id, marked) })
	e.gc.Roots = traced
	e.gc.Skipped = skipped
	e.gc.Marked = len(marked)
	e.gc.Frames = len(e.frames)
	e.gc.Collections++
	protected := 0
	for id := range e.heap {
		if !e.collectable(id) {
			protected++
			marked[id] = true // below the floor: unconditionally live
		}
	}
	e.gc.Protected = protected
	// Generational sweep: a young GC reclaims unreachable nursery objects and
	// promotes the survivors (advancing nurseryBase makes them old); a full GC
	// sweeps the whole heap once the old generation grows past a threshold.
	oldCount := 0
	for id := range e.heap {
		if id < e.nurseryBase {
			oldCount++
		}
	}
	young := e.nurseryBase
	freed := 0
	reclaim := func(id int64, o *obj) bool {
		switch o.kind {
		case "list", "dict", "set", "str", "int", "float":
			delete(e.heap, id)
			return true
		}
		return false
	}
	if young == 0 || oldCount > 512 {
		for id, o := range e.heap {
			if !marked[id] && o != nil && reclaim(id, o) {
				freed++
			}
		}
		e.gc.Generational = false
	} else {
		for id, o := range e.heap {
			if o == nil || id < young || marked[id] {
				continue
			}
			if reclaim(id, o) {
				freed++
			}
		}
		e.gc.Generational = true
	}
	e.gc.Freed = freed
	e.gc.TotalFreed += freed
	e.gc.Live = len(e.heap)
	e.nurseryBase = maxHeapID(e.heap)
	e.allocCount = 0
}

// GCStats reports what the collector has done so far. These numbers describe
// the tool, not the program, so they never appear in the program's stdout.
func (e *Evaluator) GCStats() GCStats {
	s := e.gc
	s.Backend = "interpreter"
	if s.Collections == 0 {
		s.Live = len(e.heap)
	}
	return s
}

// SetGCStress forces a collection at every statement boundary that reaches every
// object. Used by the soundness harness (and GUSTY_GC_STRESS=1): with it on, a
// forgotten root is a failing test rather than a heisenbug.
func (e *Evaluator) SetGCStress(on bool) { e.gcStress = on }

// SetGCAllocThreshold overrides the allocation pressure that triggers a
// collection. 0 restores the default.
func (e *Evaluator) SetGCAllocThreshold(n int64) {
	if n <= 0 {
		n = gcAllocThresholdDefault
	}
	e.gcThreshold = n
}
