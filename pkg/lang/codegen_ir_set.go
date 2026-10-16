// The set doors: add, discard, clear, len and the two print roads. One block of heapRuntimeIRTmpl.
package lang

// heapIRSet is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRSet = `
@.fmtemptyset = private unnamed_addr constant [6 x i8] c"set()\00"
@.fmtsopen = private unnamed_addr constant [2 x i8] c"{\00"
@.fmtsitem = private unnamed_addr constant [3 x i8] c"%d\00"
@.fmtssep = private unnamed_addr constant [3 x i8] c", \00"
@.fmtsclose = private unnamed_addr constant [2 x i8] c"}\00"

; rt_set_add_tagged is rt_set_add with the tag for the slot it writes. Adding a member that is
; already there writes nothing, and the tag store below then lands one past the end — harmless,
; because no loop reads past len, and the member that is there already keeps the tag it was added
; with (ADR 0189).
define internal void @rt_set_add_tagged(i32 %h, i32 %v, i32 %t) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  br label %check
check:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %add
body:
  ; A member is already present only when payload and tag both agree; otherwise adding "0" to a
  ; set holding 0 loses one of them (ADR 0232).
  %m = call i32 @rt_slot_matches(i32 %h, i32 %i, i32 %v, i32 %t)
  %eq = icmp ne i32 %m, 0
  br i1 %eq, label %ret, label %cont
ret:
  ret void
cont:
  %next = add i32 %i, 1
  br label %check
add:
  %kp2 = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %len
  store i32 %v, i32* %kp2
  %tg = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %len
  store i32 %t, i32* %tg
  %len2 = add i32 %len, 1
  store i32 %len2, i32* %lp
  ret void
}

define internal void @rt_set_add(i32 %h, i32 %v) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  br label %check
check:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %add
body:
  %kp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %i
  %ev = load i32, i32* %kp
  %eq = icmp eq i32 %ev, %v
  br i1 %eq, label %ret, label %cont
ret:
  ret void
cont:
  %next = add i32 %i, 1
  br label %check
add:
  %kp2 = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %len
  store i32 %v, i32* %kp2
  %len2 = add i32 %len, 1
  store i32 %len2, i32* %lp
  ret void
}

; rt_set_discard removes a value if present and shifts the tail left; removing an absent
; value is a no-op (that is what distinguishes discard from remove).
define internal void @rt_set_clear(i32 %h) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  store i32 0, i32* %lp
  ret void
}

define internal void @rt_set_discard(i32 %h, i32 %v) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  ; Computed here so it dominates the done block, which is reachable both when the
  ; value is absent (scan -> done) and after the shift loop.
  %last = sub i32 %len, 1
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  br label %scan
scan:
  %i = phi i32 [ 0, %entry ], [ %inext, %step ]
  %more = icmp slt i32 %i, %len
  br i1 %more, label %body, label %done
body:
  %p = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %i
  %x = load i32, i32* %p
  %hit = icmp eq i32 %x, %v
  br i1 %hit, label %remove, label %step
step:
  %inext = add i32 %i, 1
  br label %scan
remove:
  br label %shift
shift:
  %j = phi i32 [ %i, %remove ], [ %jnext, %shiftdo ]
  %go = icmp slt i32 %j, %last
  br i1 %go, label %shiftdo, label %done
shiftdo:
  %src = add i32 %j, 1
  %sp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %src
  %sv = load i32, i32* %sp
  %dpp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %j
  store i32 %sv, i32* %dpp
  %jnext = add i32 %j, 1
  br label %shift
done:
  store i32 %last, i32* %lp
  ret void
}

define internal i32 @rt_set_len(i32 %h) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  ret i32 %len
}

define internal void @rt_set_print(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  ; bit 8: the members describe themselves, so ask each slot instead of the object (ADR 0232)
  %fsp0 = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %h
  %flags0 = load i32, i32* %fsp0
  %mixedBit = and i32 %flags0, 8
  %isMixed = icmp ne i32 %mixedBit, 0
  br i1 %isMixed, label %mixed, label %notMixed
mixed:
  call void @rt_set_print_mixed(i32 %h, i32 %nl)
  ret void
notMixed:
  ; members are interned strings when the object says so (see rt_mark_estr), not when a
  ; static guess in this scope said so (roadmap Gap J.5)
  %fsp = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %h
  %flags = load i32, i32* %fsp
  %mb = and i32 %flags, 1
  %msb = icmp ne i32 %mb, 0
  %mstr = zext i1 %msb to i32
  ; Python renders the empty set as set(), not {} (which is a dict); the record's
  ; Repr already does this, so the compiled renderer must agree or printing an empty set
  ; differs between the backends.
  %isEmpty = icmp eq i32 %len, 0
  br i1 %isEmpty, label %empty, label %open
empty:
  call void @rt_out_txt(i8* getelementptr ([6 x i8], [6 x i8]* @.fmtemptyset, i32 0, i32 0))
  %wantnl0 = icmp ne i32 %nl, 0
  br i1 %wantnl0, label %eol0, label %fin0
eol0:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin0
fin0:
  ret void
open:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtsopen, i32 0, i32 0))
  br label %check
check:
  ; the phi's incoming block follows the new predecessor: entry no longer reaches check
  ; directly now that the empty case branches away first
  %i = phi i32 [ 0, %open ], [ %next, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %done
body:
  %kp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %i
  %ev = load i32, i32* %kp
  call void @rt_print_value(i32 %ev, i32 %mstr, i32 1)
  %i1 = add i32 %i, 1
  %c1 = icmp slt i32 %i1, %len
  br i1 %c1, label %sep, label %cont
sep:
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtssep, i32 0, i32 0))
  br label %cont
cont:
  %next = phi i32 [ %i1, %body ], [ %i1, %sep ]
  br label %check
done:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtsclose, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}
`
