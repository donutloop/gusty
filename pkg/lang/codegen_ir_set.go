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

; rt_member_bucket answers the one question the set's order is built from: where does this pair
; sit in the reference's order? The reference's set is an open-addressed table, and the array every
; reader walks is that table read in bucket order — bucket = hash(payload) mod (table size). For the
; kinds the payload can answer for, that is the payload's own floor-mod: an int hashes to itself
; (with CPython's documented remap of -1 to -2, so that -1 never wears the error value), a verdict
; hashes to 0/1, and the void hashes to a constant that lands it in bucket 0 — ahead of every small
; number, which is why the reference spells the three lines s = {1} / s.add(None) / print(s) as
; {None, 1} where this runtime used to print them in the order the program mentioned them
; (roadmap Gap R.421, ADR 0415). A pair the payload cannot answer for — a text, a container — gets
; 8, the "not participating" answer: the reference's own hash for those is not a function of the
; payload at all (a text's is salted per process), so such a member keeps the position the insertion
; gave it, after the numbers.
define internal i32 @rt_member_bucket(i32 %v, i32 %t) {
entry:
  %isNone = icmp eq i32 %t, 3
  br i1 %isNone, label %void0, label %chknum
void0:
  ret i32 0
chknum:
  %isInt = icmp eq i32 %t, 0
  %isBool = icmp eq i32 %t, 2
  %num = or i1 %isInt, %isBool
  br i1 %num, label %mod, label %aside
aside:
  ret i32 8
mod:
  %isMinusOne = icmp eq i32 %v, -1
  %hv = select i1 %isMinusOne, i32 -2, i32 %v
  %r = srem i32 %hv, 8
  %neg = icmp slt i32 %r, 0
  %radj = add i32 %r, 8
  %bk = select i1 %neg, i32 %radj, i32 %r
  ret i32 %bk
}

; rt_set_add_tagged is rt_set_add with the tag for the slot it writes. Adding a member that is
; already there writes nothing at all, and a member that is NOT already there does not simply go on
; the tail: it goes where its bucket belongs, and the tail shifts up one to make room — payload AND
; tag together, the pair rule of ADR 0410, because a shift that moved the payload alone would leave
; the shifted member wearing the kind of the slot it landed on. The array is the order both the
; printer and the loop read, so ordering it here is the one place the reference's order is decided;
; ordering the printer instead would have print and a for-loop over the same set disagree.
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
  %bk = call i32 @rt_member_bucket(i32 %v, i32 %t)
  br label %find
find:
  ; the position is the first member whose bucket is strictly greater; equal buckets keep the
  ; order the insertions gave them, which is the probe order the reference's table keeps
  %p = phi i32 [ 0, %add ], [ %pn, %after ]
  %pc = icmp slt i32 %p, %len
  br i1 %pc, label %fcmp, label %place
fcmp:
  %fep = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %p
  %fv = load i32, i32* %fep
  %ftp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %p
  %ft = load i32, i32* %ftp
  %fb = call i32 @rt_member_bucket(i32 %fv, i32 %ft)
  %gt = icmp sgt i32 %fb, %bk
  br i1 %gt, label %place, label %after
after:
  %pn = add i32 %p, 1
  br label %find
place:
  %j = phi i32 [ %len, %find ], [ %len, %fcmp ], [ %jnext, %shift ]
  %has = icmp sgt i32 %j, %p
  br i1 %has, label %shift, label %grow
shift:
  %src = sub i32 %j, 1
  %sp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %src
  %sv = load i32, i32* %sp
  %dst = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %j
  store i32 %sv, i32* %dst
  %stp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %src
  %stv = load i32, i32* %stp
  %dtp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %j
  store i32 %stv, i32* %dtp
  %jnext = sub i32 %j, 1
  br label %place
grow:
  %kp2 = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %p
  store i32 %v, i32* %kp2
  %tg = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %p
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

; rt_set_clear empties the object: the length goes to zero and the slots are left alone, because
; nothing reads a member past the length.
define internal void @rt_set_clear(i32 %h) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  store i32 0, i32* %lp
  ret void
}

; The removal doors are NOT here. rt_set_discard scanned the payload array with icmp eq i32 and
; deleted the first member whose payload happened to equal the argument's -- which is how a set of
; {1, 2} lost its member 2 to s.discard("a"), the text's intern index having collided with a number's
; payload, and how s.discard(9) removed a member that was not 9. A payload is never the discriminator
; (ADR 0187, ADR 0189), and a removal decided by one is not a door this module mounts: both verbs go
; through rt_set_find and rt_set_remove_at in the tagged-slots block below, which compare the
; (payload, tag) pair with rt_slot_matches (roadmap Gap R.422, ADR 0425).

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
