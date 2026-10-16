// The collector: rt_gc_mark and rt_gc, the worklist over the root stack that frees what nothing holds. One
// block of heapRuntimeIRTmpl.
package lang

// heapIRCollector is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRCollector = `
define internal i32 @rt_gc_mark(i32 %h) {
entry:
  %neg = icmp slt i32 %h, 0
  %hc = load i32, i32* @heap_count
  %big = icmp sge i32 %h, %hc
  %bad = or i1 %neg, %big
  br i1 %bad, label %ret0, label %chk
chk:
  %m = getelementptr [@HEAPSLOTS@ x i8], [@HEAPSLOTS@ x i8]* @gc_mark, i32 0, i32 %h
  %mv = load i8, i8* %m
  %is = icmp eq i8 %mv, 0
  br i1 %is, label %mark, label %ret0
mark:
  store i8 1, i8* %m
  br label %ret1
ret1:
  ret i32 1
ret0:
  ret i32 0
}

define internal i32 @rt_gc_tag_is_obj(i32 %t) {
entry:
  ; Does a container slot whose tag is %t hold a slot number the arena has to keep alive? The four
  ; immediate kinds answer no: an int, a bool and None hold a number, and an interned text holds an index
  ; into @str_tab, not a heap handle (ADR 0174). Treating those as handles retains whatever object happens
  ; to live at the index the number names — the reason lst = [i] in a loop pinned a dead object every
  ; iteration and walked the arena to its ceiling (roadmap Gap Q.2, ADR 0347). Every other tag the table
  ; can carry — a float box, a nested container, an instance — is an object the sweep must not take.
  ; An unrecognised tag answers "object": keeping a dead object is a leak, sweeping a live one is a crash.
  %isint = icmp eq i32 %t, 0
  %isbool = icmp eq i32 %t, 2
  %isnone = icmp eq i32 %t, 3
  %isstr = icmp eq i32 %t, 4
  %ab = or i1 %isint, %isbool
  %cd = or i1 %isnone, %isstr
  %imm = or i1 %ab, %cd
  %obj = xor i1 %imm, true
  %r = zext i1 %obj to i32
  ret i32 %r
}

define internal void @rt_gc([4096 x i32*]* %roots, i32 %nroots) {
entry:
  %c0 = load i32, i32* @gc.stat_collections
  %c1 = add i32 %c0, 1
  store i32 %c1, i32* @gc.stat_collections
  store i32 0, i32* @gc.stat_freed
  store i32 0, i32* @gc.stat_roots
  store i32 0, i32* @gc.stat_skipped
  store i32 0, i32* @gc.stat_live
  br label %cl.loop
cl.loop:
  %i = phi i32 [ 0, %entry ], [ %i.nxt, %cl.inc ]
  ; Clear the marks the window holds, not the marks the reservation could hold: a program that never
  ; grew is not paying per-slot for an arena it never asked for (roadmap Gap Q.2, ADR 0350).
  %capcl = load i32, i32* @heap_cap
  %i.end = icmp sge i32 %i, %capcl
  br i1 %i.end, label %fl.init, label %cl.body
cl.body:
  %cm = getelementptr [@HEAPSLOTS@ x i8], [@HEAPSLOTS@ x i8]* @gc_mark, i32 0, i32 %i
  store i8 0, i8* %cm
  br label %cl.inc
cl.inc:
  %i.nxt = add i32 %i, 1
  br label %cl.loop
fl.init:
  %fh0 = load i32, i32* @free_head
  br label %fl.loop
fl.loop:
  %fh = phi i32 [ %fh0, %fl.init ], [ %fn, %fl.body ]
  %fh.end = icmp slt i32 %fh, 0
  br i1 %fh.end, label %roots.init, label %fl.body
fl.body:
  %fm = getelementptr [@HEAPSLOTS@ x i8], [@HEAPSLOTS@ x i8]* @gc_mark, i32 0, i32 %fh
  store i8 2, i8* %fm
  %fnp = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @free_next, i32 0, i32 %fh
  %fn = load i32, i32* %fnp
  br label %fl.loop
roots.init:
  br label %roots.loop
roots.loop:
  %j = phi i32 [ 0, %roots.init ], [ %j.nxt, %roots.inc ]
  %j.end = icmp sge i32 %j, %nroots
  br i1 %j.end, label %pass.init, label %roots.body
roots.body:
  ; A slot tagged 0 holds a raw value (or nothing): it is *not scanned*, which is
  ; the whole point of precise roots. Counted separately so --gc-stats can show
  ; how much precision buys (ADR 0181).
  %kp = getelementptr [4096 x i8], [4096 x i8]* @gc.kinds, i32 0, i32 %j
  %kv = load i8, i8* %kp
  %isHandle = icmp ne i8 %kv, 0
  br i1 %isHandle, label %roots.check, label %roots.notHandle
roots.notHandle:
  %sk0 = load i32, i32* @gc.stat_skipped
  %sk1 = add i32 %sk0, 1
  store i32 %sk1, i32* @gc.stat_skipped
  br label %roots.skip
roots.check:
  %rp = getelementptr [4096 x i32*], [4096 x i32*]* %roots, i32 0, i32 %j
  %rpp = load i32*, i32** %rp
  %rnull = icmp eq i32* %rpp, null
  br i1 %rnull, label %roots.skip, label %roots.mark
roots.mark:
  %rs0 = load i32, i32* @gc.stat_roots
  %rs1 = add i32 %rs0, 1
  store i32 %rs1, i32* @gc.stat_roots
  %rh = load i32, i32* %rpp
  call void @rt_gc_mark(i32 %rh)
  br label %roots.skip
roots.skip:
  br label %roots.inc
roots.inc:
  %j.nxt = add i32 %j, 1
  br label %roots.loop
pass.init:
  ; ONE scan of the arena per collection. This used to be an outer pass.loop walking a counter to
  ; @heap_count with the object scan inside it — an accidental O(slots squared) per collection, invisible
  ; while the arena's own ceiling was 1024 and while a churn loop's sweep pulled the high-water mark
  ; back down, and the whole cost of a program that keeps thousands of containers alive once the arena
  ; could grow to meet them (measured: 2400 live containers, 15s; roadmap Gap Q.2, ADR 0350).
  %hc1 = load i32, i32* @heap_count
  br label %k.init
k.init:
  br label %k.loop
k.loop:
  %k = phi i32 [ 0, %k.init ], [ %k.nxt, %k.inc ]
  %k.end = icmp sge i32 %k, %hc1
  br i1 %k.end, label %sweep.init, label %k.body
k.body:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %k
  %km = getelementptr [@HEAPSLOTS@ x i8], [@HEAPSLOTS@ x i8]* @gc_mark, i32 0, i32 %k
  %kmv = load i8, i8* %km
  %kmk = icmp eq i8 %kmv, 1
  br i1 %kmk, label %e.init, label %k.inc
e.init:
  ; How many element words to walk. A dict's len counts *entries* and each entry occupies two
  ; slots, key then value, so walking len words covers only the first entry: every later entry's
  ; key and value went unmarked, and a float box or nested container stored there was swept while
  ; the dict still referenced it — the recycled slot then handed back somebody else's bits, which
  ; is how a two-entry dict came to print its second key as the first one's after an in-test
  ; allocated a temporary. List and set keep one word per element (roadmap L11.1, ADR 0233).
  %lp0 = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len0 = load i32, i32* %lp0
  %kd = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 0
  %kind = load i32, i32* %kd
  %isDict = icmp eq i32 %kind, 2
  %stride = select i1 %isDict, i32 2, i32 1
  %span = mul i32 %len0, %stride
  br label %e.loop
e.loop:
  %e = phi i32 [ 0, %e.init ], [ %e.nxt, %e.inc ]
  %e.end = icmp sge i32 %e, %span
  br i1 %e.end, label %k.inc, label %e.body
e.body:
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %e
  %ev = load i32, i32* %ep
  ; The payload is only a handle when the tag says it is. Marking every word the slot holds treats the
  ; 7 in [7] as an index and retains whatever object happens to live at slot 7, which is why a churn loop
  ; over lst = [i] pinned one dead object per iteration and walked the arena to its 1024th slot
  ; (roadmap Gap Q.2, ADR 0347). The tag is the same word the printers and the comparisons read (ADR
  ; 0184, ADR 0187), so the collector and the program cannot disagree about what a slot holds.
  %etp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %k, i32 %e
  %etg = load i32, i32* %etp
  %eobj = call i32 @rt_gc_tag_is_obj(i32 %etg)
  %emust = icmp ne i32 %eobj, 0
  br i1 %emust, label %e.mark, label %e.inc
e.mark:
  call void @rt_gc_mark(i32 %ev)
  br label %e.inc
e.inc:
  %e.nxt = add i32 %e, 1
  br label %e.loop
k.inc:
  %k.nxt = add i32 %k, 1
  br label %k.loop
sweep.init:
  br label %sw.loop
sw.loop:
  %w = phi i32 [ 0, %sweep.init ], [ %w.nxt, %sw.inc ]
  %hc2 = load i32, i32* @heap_count
  %w.end = icmp sge i32 %w, %hc2
  br i1 %w.end, label %done, label %sw.body
sw.body:
  %wm = getelementptr [@HEAPSLOTS@ x i8], [@HEAPSLOTS@ x i8]* @gc_mark, i32 0, i32 %w
  %wmv = load i8, i8* %wm
  %walive = icmp eq i8 %wmv, 1
  br i1 %walive, label %sw.live, label %sw.check
sw.live:
  %lv0 = load i32, i32* @gc.stat_live
  %lv1 = add i32 %lv0, 1
  store i32 %lv1, i32* @gc.stat_live
  br label %sw.check
sw.check:
  %wfree = icmp eq i8 %wmv, 0
  br i1 %wfree, label %sw.free, label %sw.inc
sw.free:
  %fr0 = load i32, i32* @gc.stat_freed
  %fr1 = add i32 %fr0, 1
  store i32 %fr1, i32* @gc.stat_freed
  %ft0 = load i32, i32* @gc.stat_total_freed
  %ft1 = add i32 %ft0, 1
  store i32 %ft1, i32* @gc.stat_total_freed
  %fhp = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @free_next, i32 0, i32 %w
  %fhc = load i32, i32* @free_head
  store i32 %fhc, i32* %fhp
  store i32 %w, i32* @free_head
  ; The slot is back on the free list, so the arena is holding one object fewer — and the report's
  ; headroom= has to follow, because the sweep is the road that usually frees (rt_free is the other).
  ; An occupant released twice is already a corrupted free list, hence the clamp (roadmap Gap Q.2, ADR 0347).
  %hl0 = load i32, i32* @heap_live
  %hl1 = sub i32 %hl0, 1
  %hlneg = icmp slt i32 %hl1, 0
  %hlc = select i1 %hlneg, i32 0, i32 %hl1
  store i32 %hlc, i32* @heap_live
  br label %sw.inc
sw.inc:
  %w.nxt = add i32 %w, 1
  br label %sw.loop
done:
  %mk = load i32, i32* @gc.stat_live
  store i32 %mk, i32* @gc.stat_marked
  ret void
}

`
