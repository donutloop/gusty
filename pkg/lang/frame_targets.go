// frame_targets.go is the record of the names the frame NOW being emitted binds for itself — the
// hidden locals a comprehension installs — and the one question the capture roads ask of it.
//
// A comprehension has its own scope, and its target is a binding of THAT scope: the reference does not
// leak a module-level comprehension's target into the module (`print([x for x in [1, 2]])` then
// `print(x)` is a NameError there, and this backend's front end refuses the same program), and the
// binding nearer a read is the one that read sees. The comprehension road already lowers that binding —
// it allocates the target's slot and stores each item into it — but it recorded the name nowhere, so
// every road that asks a frame what it owns came back saying nothing:
//
//	print([(lambda z: z + x)(1) for x in [10, 20]])   # the reference's [11, 21]
//
// The body of the applied `lambda` reads `x` from inside the `define` the closure road minted for it,
// and a `define` reaches neither another `define`'s registers nor the module's own storage for a name
// the module never bound — so the read was refused as `undefined name "x"`, and where the module DID
// bind the same spelling it answered the module's value instead of the target's, at the exit code of
// success. Both are the same hole: the name the loop binds was never installed as a name the frame
// owns (roadmap L11.7 · **Gap R.348**).
//
// The `for` road has the mirror-image shape and keeps the mirror-image rule: a module-level `for`
// target IS module state (the reference leaks it), so that road stores through `storeModuleScalar` and
// the module-read refusal (roadmap Gap R.35) is what a body reading one meets. A comprehension target
// is the opposite case, and this file is where the difference is written down.
package lang

// noteFrameTarget records that the frame now being emitted binds `name` for itself, the way a
// comprehension binds its target. The count is the nesting: `[[f(x, y) for y in ys] for x in xs]`
// installs `x` and then `x` again, and releasing one comprehension's target must not unbind the other's.
func (g *irGen) noteFrameTarget(name string) {
	if name == "" {
		return
	}
	if g.frameTargets == nil {
		g.frameTargets = map[string]int{}
	}
	g.frameTargets[name]++
}

// forgetFrameTarget is the release, called when the comprehension that installed the name has finished
// lowering its element and its filter. A binding that outlives the construct that wrote it is a lie the
// next expression would believe, which is the difference between scoping and a global cache (ADR 0273's
// rule that a fact is decided where it is true, not where it is convenient).
func (g *irGen) forgetFrameTarget(name string) {
	if name == "" || g.frameTargets == nil {
		return
	}
	if g.frameTargets[name] <= 1 {
		delete(g.frameTargets, name)
		return
	}
	g.frameTargets[name]--
}

// frameOwnsTarget answers the question the capture roads ask: does the frame now being emitted own this
// name ITSELF, as a binding of its own rather than as module state or a read of a frame further out?
//
// The slot has to exist, and the two facts are therefore asked together. A comprehension that folds to
// compile-time constants binds its target through the folder rather than a slot, and a name answered
// from a register that was never allocated is the module `llc` rejects — exit 2, the code ADR 0166
// reserves for a compiler bug — which is why this predicate is the only reader of the table.
func (g *irGen) frameOwnsTarget(name string) bool {
	return g.frameTargets[name] > 0 && g.allocd[name]
}

// beginFrameTargets starts the table for a frame of its own and returns the undo. A body owns only the
// targets of the comprehensions written INSIDE it; what the host frame bound is a capture by the time
// the body reads it, and it arrives as a parameter (ADR 0372's union of entries and captures). Leaving
// the host's table standing would let a body answer for a name it received, which is how a clone ends up
// forwarding a register no `define` owns (roadmap L11.6 · Gap R.342, ADR 0378 — the same frame rule the
// slot table and the float table keep).
func (g *irGen) beginFrameTargets() func() {
	saved := g.frameTargets
	g.frameTargets = map[string]int{}
	return func() { g.frameTargets = saved }
}
