package lang

// heapRuntimeIRTmpl is the arena's runtime block: the allocator, the element and text doors, the
// printing sink, the collector and the shape helpers. It is ONE string to the emitter and to every
// IR-shape test that reads it, and thirteen fragments to whoever has to review it; the order below
// is the order the module emits them in, and the concatenation is byte-identical to the single
// template that preceded this split.
//
// Like the arithmetic and pair-fold blocks (codegen_ir_numeric.go, codegen_ir_pair.go), it is a
// TEMPLATE, not final text: every declaration of an arena table spells its ceiling as @HEAPSLOTS@ and
// every question about the size the RUN has committed reads @heap_cap. The ceiling is a link-time fact
// — the tables are one zero-filled block each — and the committed size is a run-time fact that
// `rt_alloc` doubles when a program asks for a slot the window has not committed yet (roadmap Gap Q.2,
// ADR 0350). `fillRootTemplate` (codegen_ir_render.go) renders the three blocks once per process from
// the constants in heap_guard.go, so the reservation, the initial window and the growth factor have one
// spelling each.
const heapRuntimeIRTmpl = heapIRArenaGlobals +
	heapIRStringGlobals +
	heapIRUtf8 +
	heapIRSink +
	heapIRElemTags +
	heapIRMixedPrint +
	heapIRDict +
	heapIRTaggedSlots +
	heapIRSet +
	heapIRInstance +
	heapIRCollector +
	heapIRSlice +
	heapIRObject
