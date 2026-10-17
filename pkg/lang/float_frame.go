package lang

// The float-variable table is a FRAME fact, and this file is the one place that says so.
//
// `g.floatVars` answers 「whose last assignment in THIS body produced a double」. It is read by
// `isFloat`'s `*Name` arm (codegen_float.go), by the print door, by the comparison road, by
// `logic_value`'s verdict census, by `heapargs` and by `list_index_kind` — every one of them asking
// about a name in the body that is being lowered right now. For most of the backend's life the map
// was allocated ONCE per module and never cleared between `define`s, so a body read the table the
// previous body had left behind, and the answer to a question about this body's `u` was decided by
// whatever some other body had last written to a name of the same spelling.
//
// The program that exposes it is two functions that each bind `u`:
//
//	def bound():
//	    u = 9.5
//	    return u
//
//	def lastWrite():
//	    u = 2.5
//	    u = 3
//	    return u
//
//	print(lastWrite())          # 3 in the reference, 3.0 in this backend
//
// `lastWrite`'s own last write to `u` is the integer 3, and ADR 0373 is right that the read answers
// the LAST write — but with one table for the whole module the read of `u` was answered by `bound`'s
// `u = 9.5`, so the body reached the print door flagged as float and `%s`/`rt_fmt_double` rendered
// the integer 3 as `3.0`. Wrong, at the exit code of success. `def lastWrite()` alone answers `3`,
// which is the signature of an answer that depends on emission order rather than on the program: the
// conformance matrix found it by putting the two bodies in one file, and the same suite could not
// find it any other way, because every other test asked one function at a time.
//
// What the table may still hold, and what it must not:
//
//   - this body's own writes, added by the assignment road as the body lowers (codegen_assign.go);
//   - this body's own parameters, added by the float prologue when the return word is `double`
//     (codegen_func.go) and by the method road when `self`/a parameter arrives as a double;
//   - the names a scan is asking about, added on a COPY by `withFloatNames` for the length of one
//     question (ADR 0373's rule that a scan and an emission must read one table).
//
// What it must never hold again is another body's state. A module-level float read from a body is
// NOT seeded here: a compiled body cannot read module state that changes, and the gate that says so
// (「is bound at module level … roadmap Gap R.35」) is the answer that shape gets, seeded or not —
// seeding it would be Gap R.35's work (making the fold a module float gets be the fold the body's
// arithmetic wants), and that row is open. The two rows are kept apart on purpose: this one is about
// whose writes a body reads, that one is about what a body may read at all.

// enterBodyFloatTable installs the table one body reads, and returns the undo. Every road that
// emits a body — the `def` road, the method road, and anything new that lowers a `FuncDef` into a
// `define` — takes it at the body's entry, the same place `g.params` and `g.paramSlot` are installed,
// and puts the enclosing frame's table back on the way out.
//
// It returns a func rather than taking one (contrast `withFloatNames`) because a body's questions are
// not one boolean: the table has to stay installed across the whole emission, and the restore is a
// defer. It is a COPY-on-entry rather than the same map because the enclosing frame keeps its own
// table: a `lambda` or a nested `def` emitted in the middle of a statement goes back to reading the
// frame that contains it, with the names that frame wrote still in place (Gap R.265's rule about a
// nested emission that used to take its frame's state with it).
func (g *irGen) enterBodyFloatTable() func() {
	savedVars, savedSlots := g.floatVars, g.doubleSlot
	g.floatVars = map[string]bool{}
	g.doubleSlot = map[string]bool{}
	return func() { g.floatVars, g.doubleSlot = savedVars, savedSlots }
}

// bodyFloatNames is the table a body is currently reading, for the witnesses that report it. It is a
// copy: a caller must not be able to edit the frame's table through it.
func (g *irGen) bodyFloatNames() map[string]bool {
	out := make(map[string]bool, len(g.floatVars))
	for k, v := range g.floatVars {
		if v {
			out[k] = true
		}
	}
	return out
}
