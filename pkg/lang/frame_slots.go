package lang

// The slot table is a FRAME fact, and this file is the one place that says so.
//
// `g.allocd` answers 「does this name already have a stack slot in the frame being emitted」. Three
// roads read it — the assignment road (allocate the slot before storing through it), `emitFreeOld`
// (only release an old binding if the frame ever owned a slot for the name) and `copyInReboundParams`
// (a rebound parameter owns its slot, whichever road allocated it first). `g.freshSlots` says which of
// those slots the current statement created, and `g.gcRootSeen` is that frame's root bookkeeping.
//
// For most of the backend's life the maps were allocated once per *module* and shared by every frame,
// because the only road that reset them was `beginScope`, which the `def` road and the module's top-level
// code take. The two roads that emit a body without passing through `funcDef` — the closure lift
// (`emitClosureDef`, a nested `def` lifted to a top-level `define`) and the class road's
// `emitClassMethod` — never did. What that costs is measured, not guessed:
//
//	def outer():
//	    def inner():
//	        w = 3
//	        return w
//	    w = 1.5
//	    return inner()
//
//	print(outer())
//
// `inner` was emitted inside `outer`'s scope, so when `inner` bound `w` the table answered 「already
// allocated」 — for `outer`'s `w`, which had not been emitted yet — and the closure's `define` got no
// `alloca` either. Then `outer`'s own `w = 1.5` asked the same table, got the same answer, emitted
// `store i32 %t7, i32* %_w` in a `define` that had never allocated `%_w`, and `llc-20` rejected the
// module: **exit 2**, the code ADR 0166 reserves for a compiler bug, for three lines the reference runs.
// Renaming either binding compiled, which is the signature of a name-keyed table that one road resets and
// another does not (roadmap L11.6 · **Gap R.342**, ADR 0378 — the sibling of ADR 0375's float table and
// ADR 0351's one-name-one-slot rule, the same disease in the frame's other direction).
//
// What the table may hold is only ever THIS frame's own writes. In particular it must not be seeded from
// an enclosing frame: a nested body reads the names the enclosing frame owns through the *environment*
// (the closure lift's `%env` slots), not through the enclosing frame's stack slots, which are dead by the
// time the closure runs — the answer a captured name arrives in is roadmap L11.1's tagged word, not this
// table (see Gap R.332, which stays open for exactly that reason).

// enterFrameSlots installs the tables that say which stack slots the frame being emitted owns, and
// returns the undo. Every road that lowers a body into a `define` takes it at the body's entry — the
// `def` road through `beginScope`, the method and closure-lift roads through `enterBody`, which the
// two share precisely so that a road cannot be added that forgets one of them.
//
// Like `enterBodyFloatTable` it installs NEW maps rather than clearing the frame's own, so the enclosing
// frame keeps its bookkeeping and gets it back on the way out: a nested body is emitted in the middle of
// its frame, and the frame still owns its own locals afterwards.
func (g *irGen) enterFrameSlots() func() {
	savedAlloc, savedRoots, savedFresh := g.allocd, g.gcRootSeen, g.freshSlots
	g.allocd = map[string]bool{}
	g.gcRootSeen = map[string]bool{}
	g.freshSlots = map[string]bool{}
	return func() { g.allocd, g.gcRootSeen, g.freshSlots = savedAlloc, savedRoots, savedFresh }
}

// frameSlotsAreTheFramesOwn is the witness that the entry above is an entry and not a rename: a body that
// installs the tables starts with no slots at all, and the frame that contains it still has its own.
func (g *irGen) frameSlotsAreTheFramesOwn() bool {
	return len(g.allocd) == 0 && len(g.freshSlots) == 0
}
