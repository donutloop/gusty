// Tagged dict and set slots (roadmap L11.1 (1b), ADR 0232): rt_slot_matches answers "does this slot hold
// exactly this value?" — payload *and* tag. One block of heapRuntimeIRTmpl.
package lang

// heapIRTaggedSlots is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRTaggedSlots = `
; --- tagged dict and set slots (roadmap L11.1 (1b), ADR 0232) -------------------
;
; rt_slot_matches answers "does this slot hold exactly this value?" — payload *and* tag. The
; payload alone cannot answer it: a stored string is an index into @str_tab, so the key "0" and
; the number 0 are the same i32, and a dict or set that compares payloads alone hands back the
; wrong entry for one of them. This is the soundness hole ADR 0189 wrote down and left for here:
; equality carried the tag, lookup and dedup did not.
define internal i32 @rt_slot_matches(i32 %h, i32 %i, i32 %v, i32 %t) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  %e = load i32, i32* %ep
  %tp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %et = load i32, i32* %tp
  %m = call i32 @rt_payload_eq(i32 %e, i32 %et, i32 %v, i32 %t)
  ret i32 %m
}

; rt_dict_find returns the entry whose key is the (payload, tag) pair, or -1 when the dict has no
; such key. One scan, several readers: membership, a value read and the update path of a put all
; ask this same question, and they must ask the same way or they disagree about the dict.
define internal i32 @rt_dict_find(i32 %h, i32 %k, i32 %kt) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  br label %check
check:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %miss
body:
  %slot = mul i32 %i, 2
  %m = call i32 @rt_slot_matches(i32 %h, i32 %slot, i32 %k, i32 %kt)
  %hit = icmp ne i32 %m, 0
  br i1 %hit, label %found, label %cont
found:
  ret i32 %i
cont:
  %next = add i32 %i, 1
  br label %check
miss:
  ret i32 -1
}

; rt_dict_get_tagged reads the value of the entry rt_dict_find names. Absent answers 0, the way
; rt_dict_get always did; d[k] for a missing key is checkKeyRead's job, which raises KeyError.
define internal i32 @rt_dict_get_tagged(i32 %h, i32 %k, i32 %kt) {
entry:
  %i = call i32 @rt_dict_find(i32 %h, i32 %k, i32 %kt)
  %bad = icmp slt i32 %i, 0
  br i1 %bad, label %miss, label %found
miss:
  ret i32 0
found:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %slot = mul i32 %i, 2
  %vslot = add i32 %slot, 1
  %vp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %vslot
  %v = load i32, i32* %vp
  ret i32 %v
}

define internal i32 @rt_dict_has_tagged(i32 %h, i32 %k, i32 %kt) {
entry:
  %i = call i32 @rt_dict_find(i32 %h, i32 %k, i32 %kt)
  %hit = icmp sge i32 %i, 0
  %r = zext i1 %hit to i32
  ret i32 %r
}

; rt_set_find is rt_dict_find for a set, whose members occupy slots 0..len-1 rather than pairs.
define internal i32 @rt_set_find(i32 %h, i32 %v, i32 %t) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  br label %check
check:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %miss
body:
  %m = call i32 @rt_slot_matches(i32 %h, i32 %i, i32 %v, i32 %t)
  %hit = icmp ne i32 %m, 0
  br i1 %hit, label %found, label %cont
found:
  ret i32 %i
cont:
  %next = add i32 %i, 1
  br label %check
miss:
  ret i32 -1
}

; rt_dict_value_tag answers the other half of a read from a dict whose values mix kinds: the
; entry's value is one call away, and its tag is this one. Two scans of a short array is the
; honest price of one word per slot; the layout change that would halve it is L11.1 (5).
define internal i32 @rt_dict_value_tag(i32 %h, i32 %k, i32 %kt) {
entry:
  %i = call i32 @rt_dict_find(i32 %h, i32 %k, i32 %kt)
  %bad = icmp slt i32 %i, 0
  br i1 %bad, label %miss, label %found
miss:
  ret i32 0
found:
  %slot = mul i32 %i, 2
  %vslot = add i32 %slot, 1
  %tp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %vslot
  %t = load i32, i32* %tp
  ret i32 %t
}

define internal i32 @rt_set_contains_tagged(i32 %h, i32 %v, i32 %t) {
entry:
  %i = call i32 @rt_set_find(i32 %h, i32 %v, i32 %t)
  %hit = icmp sge i32 %i, 0
  %r = zext i1 %hit to i32
  ret i32 %r
}

; rt_iter_len, rt_iter_elem and rt_iter_tag answer the loop's three questions of the object the tag
; names: how many steps there are is asked once, before the loop, by the same door that raises for a
; value with no iteration at all, so these three only ever see a list, a dict, a set or a text. A list
; yields elements, a set members, a dict its KEYS at the stride 2 the dict's own reader walks, and a
; text its characters — interned, because s[i] is a one-character string and not a byte (ADR 0225),
; and tagged str so the loop variable prints as text rather than as the interned index (ADR 0185).
;
; They exist because the loop, unlike the read below the same slot (ADR 0251), chose its helper by the
; kind the compiler had written down rather than by the kind the slot carries: a dict with one entry
; whose value was a list walked the dict's own words and printed one 0 where the reference prints 1
; and 2 (roadmap L11.1, Gap R.95).
define internal i32 @rt_iter_len(i32 %h, i32 %t) {
entry:
  %islist = icmp eq i32 %t, 5
  %isdict = icmp eq i32 %t, 6
  %isset = icmp eq i32 %t, 7
  br i1 %islist, label %lst, label %chk1
lst:
  %nl = call i32 @rt_list_len(i32 %h)
  ret i32 %nl
chk1:
  br i1 %isdict, label %dct, label %chk2
dct:
  %nd = call i32 @rt_dict_len(i32 %h)
  ret i32 %nd
chk2:
  br i1 %isset, label %st, label %chr
st:
  %ns = call i32 @rt_set_len(i32 %h)
  ret i32 %ns
chr:
  %nc = call i32 @rt_str_nchars(i32 %h)
  ret i32 %nc
}

define internal i32 @rt_iter_elem(i32 %h, i32 %t, i32 %i) {
entry:
  %isstr = icmp eq i32 %t, 4
  %isdict = icmp eq i32 %t, 6
  %dpos = mul i32 %i, 2
  %pos = select i1 %isdict, i32 %dpos, i32 %i
  br i1 %isstr, label %ch, label %sl
ch:
  %c = call i32 @rt_str_char(i32 %h, i32 %i)
  ret i32 %c
sl:
  %e = call i32 @rt_get_elem(i32 %h, i32 %pos)
  ret i32 %e
}

define internal i32 @rt_iter_tag(i32 %h, i32 %t, i32 %i) {
entry:
  %isstr = icmp eq i32 %t, 4
  %isdict = icmp eq i32 %t, 6
  %dpos = mul i32 %i, 2
  %pos = select i1 %isdict, i32 %dpos, i32 %i
  br i1 %isstr, label %ch, label %sl
ch:
  ret i32 4
sl:
  %tg = call i32 @rt_tag_of(i32 %h, i32 %pos)
  ret i32 %tg
}

; rt_set_discard_tagged removes a member by (payload, tag) and shifts the tail of BOTH arrays.
; The scan is rt_set_find's question and the shift is rt_set_remove_at's, so the two removal
; roads -- s.discard(x) and s.remove(x) -- cannot ever disagree about which member a payload-and-tag
; pair names, which is the pair ADR 0187 and ADR 0232 insist a slot is never read without
; (roadmap Gap R.422).
define internal void @rt_set_discard_tagged(i32 %h, i32 %v, i32 %t) {
entry:
  %at = call i32 @rt_set_find(i32 %h, i32 %v, i32 %t)
  %absent = icmp slt i32 %at, 0
  br i1 %absent, label %done, label %work
work:
  call void @rt_set_remove_at(i32 %h, i32 %at)
  ret void
done:
  ret void
}

; rt_set_remove_at drops the member at a position the caller already found, and shifts the tail of
; BOTH arrays. Shifting the payloads while leaving the tags where they were would print every member
; after the removed one through the kind of its neighbour -- the bug ADR 0187 names on every
; positional door, kept here on the set's own array (roadmap Gap R.422). The caller has already
; answered "is it there?"; this door does not ask again, which is why remove can raise its
; KeyError from the finding instead of from a second scan that might find something else.
define internal void @rt_set_remove_at(i32 %h, i32 %pos) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %last = sub i32 %len, 1
  br label %shift
shift:
  %j = phi i32 [ %pos, %entry ], [ %jnext, %shiftdo ]
  %go = icmp slt i32 %j, %last
  br i1 %go, label %shiftdo, label %shrink
shiftdo:
  %src = add i32 %j, 1
  %sp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %src
  %sv = load i32, i32* %sp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %j
  store i32 %sv, i32* %dp
  %st = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %src
  %stv = load i32, i32* %st
  %dt = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %j
  store i32 %stv, i32* %dt
  %jnext = add i32 %j, 1
  br label %shift
shrink:
  store i32 %last, i32* %lp
  ret void
}

; rt_dict_print_mixed walks a dict whose key and value slots carry their own tags: each position
; asks the tag what it is instead of asking the object what kind of dict it is. That is the
; difference between {"a": 1} and {"a": 1, "b": "x"} — the second has no kind.
define internal void @rt_dict_print_mixed(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtdopen, i32 0, i32 0))
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %more = icmp slt i32 %i, %len
  br i1 %more, label %body, label %done
body:
  %idx = mul i32 %i, 2
  %idx2 = add i32 %idx, 1
  %kp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %idx
  %k = load i32, i32* %kp
  %vp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %idx2
  %v = load i32, i32* %vp
  %kt = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %idx
  %keyTag = load i32, i32* %kt
  %vt = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %idx2
  %valTag = load i32, i32* %vt
  %first = icmp eq i32 %i, 0
  br i1 %first, label %emit, label %sepd
sepd:
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  br label %emit
emit:
  call void @rt_print_mixed_value(i32 %k, i32 %keyTag, i32 1)
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtcolon, i32 0, i32 0))
  call void @rt_print_mixed_value(i32 %v, i32 %valTag, i32 1)
  br label %cont
cont:
  %next = add i32 %i, 1
  br label %loop
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

; rt_set_print_mixed is the same walk for a set, empty included: Python renders the empty set as
; set(), and {} is a dict, so the empty case cannot fall through to the general loop.
define internal void @rt_set_print_mixed(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %isEmpty = icmp eq i32 %len, 0
  br i1 %isEmpty, label %empty, label %open
empty:
  call void @rt_out_txt(i8* getelementptr ([6 x i8], [6 x i8]* @.fmtemptyset, i32 0, i32 0))
  br label %fin
open:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtsopen, i32 0, i32 0))
  br label %loop
loop:
  %i = phi i32 [ 0, %open ], [ %next, %cont ]
  %more = icmp slt i32 %i, %len
  br i1 %more, label %body, label %done
body:
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  %e = load i32, i32* %ep
  %tp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %t = load i32, i32* %tp
  %first = icmp eq i32 %i, 0
  br i1 %first, label %emit, label %sepd
sepd:
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  br label %emit
emit:
  call void @rt_print_mixed_value(i32 %e, i32 %t, i32 1)
  br label %cont
cont:
  %next = add i32 %i, 1
  br label %loop
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

define internal i32 @rt_dict_len(i32 %h) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  ret i32 %len
}

; rt_dict_print asks the object which of its positions hold interned strings and delegates:
; bit 1 = keys, bit 2 = values. A static map cannot answer that question when the dict was
; filled inside a helper that received it as a parameter (roadmap Gap J.5).
define internal void @rt_dict_print(i32 %h, i32 %nl) {
entry:
  %fsp = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %h
  %flags = load i32, i32* %fsp
  ; bit 8 says the slots describe themselves: a dict that mixes kinds has no single kind to
  ; report, so it prints position by position (roadmap L11.1 (1b), ADR 0232).
  %mixedBit = and i32 %flags, 8
  %isMixed = icmp ne i32 %mixedBit, 0
  br i1 %isMixed, label %mixed, label %static
mixed:
  call void @rt_dict_print_mixed(i32 %h, i32 %nl)
  ret void
static:
  %kb = and i32 %flags, 2
  %ksb = icmp ne i32 %kb, 0
  %ksi = zext i1 %ksb to i32
  %vb = and i32 %flags, 4
  %vsb = icmp ne i32 %vb, 0
  %vsi = zext i1 %vsb to i32
  call void @rt_dict_print_s(i32 %h, i32 %nl, i32 %ksi, i32 %vsi)
  ret void
}

`
