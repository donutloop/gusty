// The arena itself: the parallel tables, the free list, rt_alloc (with the window-doubling door of ADR 0350)
// and rt_free, plus the element read/write doors. One block of heapRuntimeIRTmpl.
package lang

// heapIRArenaGlobals is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRArenaGlobals = `@heap_count = internal global i32 0
; Cached handle for the None singleton (heap kind 4). None cannot be an immediate the way
; ints are: any i32 value is a legal integer, so no bit pattern is free to mean "no value".
; The handle is allocated once and never freed, so "x = None" and "x == None" behave the
; same in the compiled backend and the record (ADR 0172).
@none_h = internal global i32 -1
@gc_mark = internal global [@HEAPSLOTS@ x i8] zeroinitializer
@gc_urgent = internal global i32 0
@free_head = internal global i32 -1
@free_next = internal global [@HEAPSLOTS@ x i32] zeroinitializer
@heap = internal global [@HEAPSLOTS@ x {i32, i32, [256 x i32]}] zeroinitializer
; The two facts about the arena that a run reports — how many slots it is holding, and how many times
; it ran out — are deliberately NOT declared here. They live with the collector's other self-report
; numbers in rootGlobalsIR, because rt_gc_report reports capacity and headroom for a module that never
; touched the arena either (they are the backend's facts, not this module's), and an internal global the
; report reads while this block owns it is the undefined-symbol class ADR 0173 made the whole rule about
; (roadmap Gap Q.2, ADR 0347).

define internal i32 @rt_alloc(i32 %kind) {
entry:
  %fh = load i32, i32* @free_head
  %isneg = icmp slt i32 %fh, 0
  br i1 %isneg, label %alloc_new, label %alloc_reuse
alloc_new:
  %c = load i32, i32* @heap_count
  ; The window this RUN has committed, not the ceiling the module reserves. A program that never asks
  ; for more than HeapInitialCapacity slots never leaves the window it started in; a program that does
  ; asks the door below for another generation of slots and keeps going (roadmap Gap Q.2, ADR 0350).
  %cap0 = load i32, i32* @heap_cap
  %oob = icmp sge i32 %c, %cap0
  br i1 %oob, label %maybe_grow, label %newok
maybe_grow:
  ; Growing the arena is a commit, not a relocation: a handle is an INDEX into the tables, so nothing
  ; the program holds has to move when the window widens — which is why the growth fits in this door
  ; and why the seven parallel tables cannot drift apart (they share one index space and one bound).
  ; The window doubles until it reaches the reservation; past that there is nothing left to commit and
  ; the run is out of memory in the sense ADR 0347 made a MemoryError.
  %room = icmp slt i32 %cap0, @HEAPSLOTS@
  br i1 %room, label %grow, label %full
grow:
  %doubled = mul i32 %cap0, @HEAPGROW@
  %wide = icmp sgt i32 %doubled, @HEAPSLOTS@
  %cap1 = select i1 %wide, i32 @HEAPSLOTS@, i32 %doubled
  store i32 %cap1, i32* @heap_cap
  ; The newly committed slots are the zero-filled tail of the reservation, so nothing has to be cleared
  ; for them: @heap[k]'s kind and length are written by this door, @estr[k] is cleared here, an element
  ; tag is written beside every payload that needs one (ADR 0187) and an instance row is cleared by
  ; rt_inst_clear (ADR 0235). A slot the window has never held has therefore never been read.
  %grew = load i32, i32* @heap_grown
  %grew1 = add i32 %grew, 1
  store i32 %grew1, i32* @heap_grown
  ; One doubling is enough by construction — the count only ever reaches the window it was committed
  ; in, so after a widening it sits below it — but the door re-asks rather than assumes, because the
  ; alternative is handing out an index outside the window on the first day the arithmetic is wrong.
  %again = icmp sge i32 %c, %cap1
  br i1 %again, label %full, label %newok
full:
  ; Exhaustion is counted here and raised by the caller's guard, not here: rt_alloc is called from
  ; inside the other helpers too (rt_list_copy, rt_float_new, rt_slice, rt_none), and a helper that
  ; raised would leave each of them returning -1 into a store that runs anyway. The caller is the one
  ; place that knows which handler the raise belongs to (ADR 0337's argument, applied one door earlier;
  ; roadmap Gap Q.2, ADR 0347). A helper that cannot report a handle at all — rt_num_arith, which
  ; returns a status and hands the box back through an out-param — answers a status of its own instead;
  ; see the fourth status at the end of the door.
  %e = load i32, i32* @heap_exhausted
  %e1 = add i32 %e, 1
  store i32 %e1, i32* @heap_exhausted
  ret i32 -1
newok:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %c
  %kp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 0
  store i32 %kind, i32* %kp
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  store i32 0, i32* %lp
  ; @estr[h] is the container-wide "my elements are interned text" flag, and the printers
  ; dispatch on it. Like a slot's tag it is an attribute of the object, so its lifetime has to
  ; match the object's: a recycled slot that kept its predecessor's flag made print(["a"])
  ; followed by print({1, 2}) render the numbers through the string table as {(null), (null)}.
  ; Clearing the tag array would be a 256-entry memset and is covered by writing tags in pairs
  ; with payloads (ADR 0187); this flag is one store, so clear it (ADR 0188).
  %eslot = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %c
  store i32 0, i32* %eslot
  %c1 = add i32 %c, 1
  store i32 %c1, i32* @heap_count
  ; The occupancy the self-report prints is kept beside the high-water mark, not computed from the free
  ; list at report time: the list is the truth about *reuse*, and a report that walked it would need the
  ; arena's own capacity bound in the reporting block (roadmap Gap Q.2, ADR 0347).
  %lv = load i32, i32* @heap_live
  %lv1 = add i32 %lv, 1
  store i32 %lv1, i32* @heap_live
  ret i32 %c
alloc_reuse:
  %rn = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @free_next, i32 0, i32 %fh
  %next = load i32, i32* %rn
  store i32 %next, i32* @free_head
  %obj2 = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %fh
  %kp2 = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj2, i32 0, i32 0
  store i32 %kind, i32* %kp2
  %lp2 = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj2, i32 0, i32 1
  store i32 0, i32* %lp2
  %eslot2 = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %fh
  store i32 0, i32* %eslot2
  %lv2 = load i32, i32* @heap_live
  %lv21 = add i32 %lv2, 1
  store i32 %lv21, i32* @heap_live
  ret i32 %fh
}

define internal void @rt_free(i32 %h) {
entry:
  %fh = load i32, i32* @free_head
  %fn = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @free_next, i32 0, i32 %h
  store i32 %fh, i32* %fn
  store i32 %h, i32* @free_head
  ; One slot less in use. Clamped, because rt_free has never owned a double-free contract: freeing the
  ; same handle twice already corrupts the free list, and an occupancy that went negative would turn the
  ; report's headroom= into a number larger than the capacity — a second lie where the first one is
  ; already being debugged (roadmap Gap Q.2, ADR 0347).
  %lv = load i32, i32* @heap_live
  %lvm = sub i32 %lv, 1
  %lown = icmp slt i32 %lvm, 0
  %lvc = select i1 %lown, i32 0, i32 %lvm
  store i32 %lvc, i32* @heap_live
  ret void
}
define internal void @rt_set_elem(i32 %h, i32 %i, i32 %v) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  store i32 %v, i32* %ep
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %len1 = add i32 %len, 1
  store i32 %len1, i32* %lp
  ret void
}

; list element replacement: xs[i] = v writes elems[i] in place. Unlike
; rt_set_elem this must NOT bump the length, which is what appends do.
define internal void @rt_put_elem(i32 %h, i32 %i, i32 %v) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  store i32 %v, i32* %ep
  ret void
}

define internal i32 @rt_list_len(i32 %h) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  ret i32 %len
}

define internal i32 @rt_get_elem(i32 %h, i32 %i) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  %v = load i32, i32* %ep
  ret i32 %v
}

; rt_pop removes element %i and returns it, shifting the tail left and shrinking the
; length. The caller bounds-checks (an out-of-range index raises IndexError), and the
; index is already normalised for negatives.
;
; A slot is TWO words — the payload and the tag that says what the payload means — so the shift
; moves both, and the removed slot's tag leaves through %outt. A shift that moved the payload
; alone left every tail element wearing the kind of the slot it landed on: the three lines
; xs = [1, 2, "a"] / xs.pop(0) / print(xs) wrote [2, 0] where the reference writes [2, 'a'], because
; the text's intern index slid into a slot whose tag still said int, and the three lines
; xs = ["a", 1] / xs.pop(0) / if xs[0]: read @str_tab[1] — a pointer the intern table never wrote —
; and the process died in the printer with nothing on stdout (exit 139, ADR 0166's artifact-at-fault
; class). The tag array
; is not cleared on free, so the stale tag is whatever the slot's last tenant carried: the answer
; depended on the program's history, not on its source.
; rt_reverse's swap (ADR 0187) and rt_set_discard_tagged's shift keep the same rule; this is the
; third positional door, and the one that had not been told (roadmap L11.1, ADR 0410).
define internal i32 @rt_pop(i32 %h, i32 %i, i32* %outt) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  %ip = getelementptr [256 x i32], [256 x i32]* %ep, i32 0, i32 %i
  %v = load i32, i32* %ip
  %tp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %t = load i32, i32* %tp
  store i32 %t, i32* %outt
  %last = sub i32 %len, 1
  br label %shift
shift:
  %j = phi i32 [ %i, %entry ], [ %jnext, %shiftdo ]
  %more = icmp slt i32 %j, %last
  br i1 %more, label %shiftdo, label %done
shiftdo:
  %src = add i32 %j, 1
  %sp = getelementptr [256 x i32], [256 x i32]* %ep, i32 0, i32 %src
  %sv = load i32, i32* %sp
  %dp = getelementptr [256 x i32], [256 x i32]* %ep, i32 0, i32 %j
  store i32 %sv, i32* %dp
  %st = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %src
  %stv = load i32, i32* %st
  %dt = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %j
  store i32 %stv, i32* %dt
  %jnext = add i32 %j, 1
  br label %shift
done:
  store i32 %last, i32* %lp
  ret i32 %v
}

define internal void @rt_append(i32 %h, i32 %v) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %len
  store i32 %v, i32* %ep
  %len1 = add i32 %len, 1
  store i32 %len1, i32* %lp
  ret void
}
`
