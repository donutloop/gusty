// The per-element tag doors: rt_tag_elem/rt_append_tagged/rt_tag_of and the payload/slot/container equality
// they make possible. One block of heapRuntimeIRTmpl.
package lang

// heapIRElemTags is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRElemTags = `
define internal void @rt_tag_elem(i32 %h, i32 %i, i32 %t) {
entry:
  %p = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  store i32 %t, i32* %p
  ret void
}

; rt_append_tagged is rt_append plus the tag the new slot carries. An append that forgets the
; tag leaves the slot reading back as whatever the *previous* tenant of that slot was — the
; tag array is not cleared on free — so a mixed list would print an interned string's index as
; a number. Appending and tagging are one operation precisely because they must not be two
; (roadmap L11.1, ADR 0187).
define internal void @rt_append_tagged(i32 %h, i32 %v, i32 %t) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %len
  store i32 %v, i32* %ep
  %tp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %len
  store i32 %t, i32* %tp
  %len1 = add i32 %len, 1
  store i32 %len1, i32* %lp
  ret void
}

define internal i32 @rt_tag_of(i32 %h, i32 %i) {entry:
  %p = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %v = load i32, i32* %p
  ret i32 %v
}

; rt_float_new allocates the float box that a container slot can hold: rt_alloc's own kind for
; this purpose (HeapKindFloat), the double stored in the parallel @float_box table, and the handle
; returned for the slot's payload. A failed allocation is -1, the same answer rt_alloc gives when
; the heap is full, and the callers that check it behave as they do for any exhausted heap.
define internal i32 @rt_float_new(double %d) {
entry:
  %h = call i32 @rt_alloc(i32 5)
  %bad = icmp slt i32 %h, 0
  br i1 %bad, label %fail, label %put
put:
  %p = getelementptr [@HEAPSLOTS@ x double], [@HEAPSLOTS@ x double]* @float_box, i32 0, i32 %h
  store double %d, double* %p
  ret i32 %h
fail:
  ret i32 -1
}

; rt_float_of reads a box back. It is the only way to a stored float, and it is what makes the
; comparison below able to say anything about numbers.
define internal double @rt_float_of(i32 %h) {
entry:
  %p = getelementptr [@HEAPSLOTS@ x double], [@HEAPSLOTS@ x double]* @float_box, i32 0, i32 %h
  %d = load double, double* %p
  ret double %d
}

; rt_payload_eq is the one answer to "do these two slot payloads denote the same value?", asked
; with the tags that make the payloads mean something. Within a tag it is payload equality, which
; is what makes a stored string and the number 1 different values even when both are the i32 1.
; Across the two numeric tags it is numeric equality, because Python's containers answer 1 == 1.0
; and [1] == [1.0] and 1.0 in [1] with True, and a tag comparison alone would
; answer False (roadmap L11.1, ADR 0233). fcmp oeq is the right predicate for the float cases:
; it gives -0.0 == 0.0 like Python, and NaN unequal to itself, which is what Python's own float
; comparison does. Any other pair of tags is unequal, bool included: Python does treat True as
; 1, but rendering True as a number is a separate known gap (ADR 0233's record), and answering
; this one wrongly in either direction is worse than answering it the way the tags say.
;
; It is also the answer a source-level equality between two tagged reads gets (roadmap L11.1, Gap
; R.79): an element of a container whose slots describe themselves is a (payload, tag) pair, and
; the only sound equality for a pair is this one. The naive helper that used to sit here — same
; tag, then compare the words — compared two float slots by box handle, so y = xs[0] over
; xs = [1.5, "a"] answered y == 1.5 false while printing 1.5 one line later.
define internal i32 @rt_payload_eq(i32 %a, i32 %ta, i32 %b, i32 %tb) {
entry:
  %sameTag = icmp eq i32 %ta, %tb
  br i1 %sameTag, label %same, label %mixed
same:
  %isFloat = icmp eq i32 %ta, 1
  br i1 %isFloat, label %floats, label %chkContainer
floats:
  %fa = call double @rt_float_of(i32 %a)
  %fb = call double @rt_float_of(i32 %b)
  %feq = fcmp oeq double %fa, %fb
  %fr = zext i1 %feq to i32
  ret i32 %fr
chkContainer:
  ; Two slots that both hold a container are equal when their *contents* are, which is
  ; rt_container_eq's question — the payload alone would answer it with two addresses, and
  ; [[1, 2]] == [[1, 2]] would be False for two literals that build two objects
  ; (roadmap L11.1, ADR 0189). rt_container_eq compares slots with rt_slot_eq, which calls
  ; back into this function, so the comparison recurses to any depth.
  %isList = icmp eq i32 %ta, 5
  %isDict = icmp eq i32 %ta, 6
  %isSet = icmp eq i32 %ta, 7
  %c1 = or i1 %isList, %isDict
  %c2 = or i1 %c1, %isSet
  br i1 %c2, label %containers, label %payloads
containers:
  %ce = call i32 @rt_container_eq(i32 %a, i32 %b)
  ret i32 %ce
payloads:
  %peq = icmp eq i32 %a, %b
  %pr = zext i1 %peq to i32
  ret i32 %pr
mixed:
  ; A bool is a number to Python, and now that a container slot can say bool the comparison has to
  ; agree with the printer: True == 1, True == 1.0, {1: 'a'}[True] is 'a' and {True, 1} has one
  ; member. So the numeric family this block serves is int, float AND bool, and two slots of that
  ; family decide it on the payload they all carry — the same route an int and a float already took,
  ; which is why a bool needs no arithmetic of its own here. A bool against a text, a None or a
  ; container stays unequal, which is what the tags say (Gap R.112, ADR 0259).
  %aIsInt = icmp eq i32 %ta, 0
  %aIsBool = icmp eq i32 %ta, 2
  %aIsNum = or i1 %aIsInt, %aIsBool
  %bIsInt = icmp eq i32 %tb, 0
  %bIsBool = icmp eq i32 %tb, 2
  %bIsNum = or i1 %bIsInt, %bIsBool
  %bothNum = and i1 %aIsNum, %bIsNum
  br i1 %bothNum, label %payloads, label %mixedFloat
mixedFloat:
  %bIsFloat = icmp eq i32 %tb, 1
  %forward = and i1 %aIsNum, %bIsFloat
  br i1 %forward, label %intFloat, label %backward
backward:
  %aIsFloat = icmp eq i32 %ta, 1
  %reverse = and i1 %aIsFloat, %bIsNum
  br i1 %reverse, label %floatInt, label %unequal
intFloat:
  %da = sitofp i32 %a to double
  %db = call double @rt_float_of(i32 %b)
  %eq1 = fcmp oeq double %da, %db
  %r1 = zext i1 %eq1 to i32
  ret i32 %r1
floatInt:
  %dc = call double @rt_float_of(i32 %a)
  %dd = sitofp i32 %b to double
  %eq2 = fcmp oeq double %dc, %dd
  %r2 = zext i1 %eq2 to i32
  ret i32 %r2
unequal:
  ret i32 0
}

; rt_slot_eq compares two container slots by (payload, tag), which is the only sound element
; comparison: a stored string is an index into @str_tab, and the number 1 is a payload that can
; equal it. The pair is asked of rt_payload_eq rather than compared directly so that a float slot
; and an int slot holding the same number answer equal, which is what [1] == [1.0] needs
; (roadmap L11.1, ADR 0189 and ADR 0233).
define internal i32 @rt_slot_eq(i32 %h1, i32 %i1, i32 %h2, i32 %i2) {
entry:
  %o1 = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h1
  %e1 = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %o1, i32 0, i32 2, i32 %i1
  %v1 = load i32, i32* %e1
  %o2 = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h2
  %e2 = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %o2, i32 0, i32 2, i32 %i2
  %v2 = load i32, i32* %e2
  %t1p = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h1, i32 %i1
  %t1 = load i32, i32* %t1p
  %t2p = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h2, i32 %i2
  %t2 = load i32, i32* %t2p
  %r32 = call i32 @rt_payload_eq(i32 %v1, i32 %t1, i32 %v2, i32 %t2)
  ret i32 %r32
}

; rt_container_eq compares two containers the way Python's == does: same kind, same size, and
; every element equal under rt_slot_eq. Lists compare position by position; sets and dicts compare
; by containment, because {1, 2} == {2, 1} is True and so is {"a": 1, "b": 2} == {"b": 2, "a": 1}
; — a positional walk would make both False. Non-containers and mismatched kinds are unequal,
; which is also the right answer for [1] == 1 (ADR 0189).
define internal i32 @rt_container_eq(i32 %a, i32 %b) {
entry:
  ; A handle is only a container if the run committed the slot it names: the window, not the
  ; reservation, is what separates a slot the allocator handed out from one it never did
  ; (roadmap Gap Q.2, ADR 0350).
  %capq = load i32, i32* @heap_cap
  %rangeA = icmp slt i32 %a, %capq
  %rangeB = icmp slt i32 %b, %capq
  %bothrange = and i1 %rangeA, %rangeB
  br i1 %bothrange, label %kinds, label %no
kinds:
  %oa = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %a
  %ob = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %b
  %kap = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %oa, i32 0, i32 0
  %kbp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ob, i32 0, i32 0
  %ka = load i32, i32* %kap
  %kb = load i32, i32* %kbp
  %ksameness = icmp eq i32 %ka, %kb
  br i1 %ksameness, label %tupflags, label %no
tupflags:
  ; Same kind is not the whole question for the family that shares the list layout: a tuple and a
  ; list with equal slots are NOT equal, because the language distinguishes them and the object
  ; carries the distinction. The flag is read through the tuple block's own door so this module
  ; never spells the bit (roadmap L11.3, ADR 0361).
  %fa = call i32 @rt_tuple_flag(i32 %a)
  %fb = call i32 @rt_tuple_flag(i32 %b)
  %fsame = icmp eq i32 %fa, %fb
  br i1 %fsame, label %sizes, label %no
sizes:
  %lap = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %oa, i32 0, i32 1
  %lbp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ob, i32 0, i32 1
  %la = load i32, i32* %lap
  %lb = load i32, i32* %lbp
  %lsame = icmp eq i32 %la, %lb
  br i1 %lsame, label %disp, label %no
disp:
  ; HeapKindList = 1, HeapKindDict = 2, HeapKindSet = 3
  %islist = icmp eq i32 %ka, 1
  br i1 %islist, label %li_entry, label %chkset
chkset:
  %isset = icmp eq i32 %ka, 3
  br i1 %isset, label %si_entry, label %chkdict
chkdict:
  %isdict = icmp eq i32 %ka, 2
  br i1 %isdict, label %di_entry, label %no

li_entry:
  br label %li
li:
  %li_i = phi i32 [ 0, %li_entry ], [ %li_next, %li_step ]
  %li_c = icmp slt i32 %li_i, %la
  br i1 %li_c, label %li_body, label %yes
li_body:
  %li_s = call i32 @rt_slot_eq(i32 %a, i32 %li_i, i32 %b, i32 %li_i)
  %li_ne = icmp ne i32 %li_s, 0
  br i1 %li_ne, label %li_step, label %no
li_step:
  %li_next = add i32 %li_i, 1
  br label %li

si_entry:
  br label %si
si:
  %si_i = phi i32 [ 0, %si_entry ], [ %si_next, %si_step ]
  %si_c = icmp slt i32 %si_i, %la
  br i1 %si_c, label %si_body, label %yes
si_body:
  br label %sj
sj:
  %sj_j = phi i32 [ 0, %si_body ], [ %sj_next, %sj_step ]
  %sj_c = icmp slt i32 %sj_j, %lb
  br i1 %sj_c, label %sj_body, label %no
sj_body:
  %sj_s = call i32 @rt_slot_eq(i32 %a, i32 %si_i, i32 %b, i32 %sj_j)
  %sj_ne = icmp ne i32 %sj_s, 0
  br i1 %sj_ne, label %si_step, label %sj_step
sj_step:
  %sj_next = add i32 %sj_j, 1
  br label %sj
si_step:
  %si_next = add i32 %si_i, 1
  br label %si

di_entry:
  br label %di
di:
  %di_i = phi i32 [ 0, %di_entry ], [ %di_next, %di_step ]
  %di_c = icmp slt i32 %di_i, %la
  br i1 %di_c, label %di_body, label %yes
di_body:
  %di_k = mul i32 %di_i, 2
  %di_v = add i32 %di_k, 1
  br label %dj
dj:
  %dj_j = phi i32 [ 0, %di_body ], [ %dj_next, %dj_step ]
  %dj_c = icmp slt i32 %dj_j, %lb
  br i1 %dj_c, label %dj_body, label %no
dj_body:
  %dj_k = mul i32 %dj_j, 2
  %dj_v = add i32 %dj_k, 1
  %dj_key = call i32 @rt_slot_eq(i32 %a, i32 %di_k, i32 %b, i32 %dj_k)
  %dj_kne = icmp ne i32 %dj_key, 0
  br i1 %dj_kne, label %dj_val, label %dj_step
dj_val:
  %dj_val_eq = call i32 @rt_slot_eq(i32 %a, i32 %di_v, i32 %b, i32 %dj_v)
  %dj_vne = icmp ne i32 %dj_val_eq, 0
  br i1 %dj_vne, label %di_step, label %no
dj_step:
  %dj_next = add i32 %dj_j, 1
  br label %dj
di_step:
  %di_next = add i32 %di_i, 1
  br label %di

yes:
  ret i32 1
no:
  ret i32 0
}
`
