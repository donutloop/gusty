// The dict doors: put (plain and tagged), the print roads, contains and get. One block of heapRuntimeIRTmpl.
package lang

// heapIRDict is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRDict = `
@.fmtdopen = private unnamed_addr constant [2 x i8] c"{\00"
@.fmtditem = private unnamed_addr constant [7 x i8] c"%d: %d\00"
@.fmtdsep = private unnamed_addr constant [3 x i8] c", \00"
@.fmtdclose = private unnamed_addr constant [2 x i8] c"}\00"

; rt_dict_put_tagged is rt_dict_put with the tags the two new slots carry. A dict interleaves key
; and value in the element array, so an entry owns tag slots 2i and 2i+1; updating an existing
; key rewrites the value's tag, because a key that now maps to a different kind of thing must not
; keep printing and comparing as the old one (ADR 0187's rule, applied to dicts by ADR 0189).
define internal void @rt_dict_put_tagged(i32 %h, i32 %k, i32 %v, i32 %kt, i32 %vt) {
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
  %idx = mul i32 %i, 2
  ; The key is compared as a (payload, tag) pair: {0: "n"} and {"0": "s"} are one entry each, and
  ; they are different entries only because the tag says so (ADR 0232).
  %km = call i32 @rt_slot_matches(i32 %h, i32 %idx, i32 %k, i32 %kt)
  %eq = icmp ne i32 %km, 0
  br i1 %eq, label %upd, label %cont
upd:
  %idx2 = add i32 %idx, 1
  %vp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx2
  store i32 %v, i32* %vp
  %utg = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %idx2
  store i32 %vt, i32* %utg
  ret void
cont:
  %next = add i32 %i, 1
  br label %check
add:
  %idx3 = mul i32 %len, 2
  %kp3 = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx3
  store i32 %k, i32* %kp3
  %idx4 = add i32 %idx3, 1
  %vp4 = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx4
  store i32 %v, i32* %vp4
  %ktg = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %idx3
  store i32 %kt, i32* %ktg
  %vtg = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %idx4
  store i32 %vt, i32* %vtg
  %len2 = add i32 %len, 1
  store i32 %len2, i32* %lp
  ret void
}

define internal void @rt_dict_put(i32 %h, i32 %k, i32 %v) {
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
  %idx = mul i32 %i, 2
  %kp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx
  %ek = load i32, i32* %kp
  %eq = icmp eq i32 %ek, %k
  br i1 %eq, label %upd, label %cont
upd:
  %idx2 = add i32 %idx, 1
  %vp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx2
  store i32 %v, i32* %vp
  ret void
cont:
  %next = add i32 %i, 1
  br label %check
add:
  %idx3 = mul i32 %len, 2
  %kp2 = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx3
  store i32 %k, i32* %kp2
  %idx4 = add i32 %idx3, 1
  %vp2 = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx4
  store i32 %v, i32* %vp2
  %len2 = add i32 %len, 1
  store i32 %len2, i32* %lp
  ret void
}

; rt_contains_tagged is rt_contains with the tag compared alongside the payload. It answers the
; same question for lists (elements), dicts (keys) and sets (members), and answers it correctly:
; an @str_tab index and an integer of the same number are different values, and only the tag says
; which one a slot holds (ADR 0189 made the tags always-available, ADR 0232 makes them always
; consulted). rt_contains stays for a needle whose kind the compiler cannot prove.
define internal i32 @rt_contains_tagged(i32 %h, i32 %v, i32 %t) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %kp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 0
  %kind = load i32, i32* %kp
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %islist = icmp eq i32 %kind, 1
  br i1 %islist, label %loop, label %chkdict
chkdict:
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ 0, %chkdict ], [ %inext, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %miss
body:
  %isdict = icmp eq i32 %kind, 2
  %idx = mul i32 %i, 2
  %sel = select i1 %isdict, i32 %idx, i32 %i
  %ok = call i32 @rt_slot_matches(i32 %h, i32 %sel, i32 %v, i32 %t)
  %yes = icmp ne i32 %ok, 0
  br i1 %yes, label %hit, label %cont
cont:
  %inext = add i32 %i, 1
  br label %loop
hit:
  ret i32 1
miss:
  ret i32 0
}

define internal i32 @rt_contains(i32 %h, i32 %v) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %kp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 0
  %kind = load i32, i32* %kp
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  %islist = icmp eq i32 %kind, 1
  br i1 %islist, label %loop, label %chkdict
chkdict:
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ 0, %chkdict ], [ %inext, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %miss
body:
  %isdict = icmp eq i32 %kind, 2
  %idx = mul i32 %i, 2
  %sel = select i1 %isdict, i32 %idx, i32 %i
  %ep = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %sel
  %e = load i32, i32* %ep
  %eq = icmp eq i32 %e, %v
  br i1 %eq, label %hit, label %cont
cont:
  %inext = add i32 %i, 1
  br label %loop
hit:
  ret i32 1
miss:
  ret i32 0
}

define internal i32 @rt_dict_get(i32 %h, i32 %k) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  br label %check
check:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %miss
body:
  %idx = mul i32 %i, 2
  %kp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx
  %ek = load i32, i32* %kp
  %eq = icmp eq i32 %ek, %k
  br i1 %eq, label %hit, label %cont
hit:
  %idx2 = add i32 %idx, 1
  %vp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx2
  %v = load i32, i32* %vp
  ret i32 %v
cont:
  %next = add i32 %i, 1
  br label %check
miss:
  ret i32 0
}

; rt_dict_keys copies a dict's KEYS, in entry order, into a fresh list. It is the door 'sorted(d)'
; reads its input from, and the reason it exists is a stride: a dict's payload array holds TWO words
; per entry (the key at 2i, the value at 2i+1) while the length word counts ENTRIES, so a walk that
; reads 'len' words positionally hands back keys and values interleaved. 'd = {"a": 1, "b": 2}' /
; 'print(sorted(d))' sorted the first two words of that array and printed ['a', 1] where the reference
; prints ['a', 'b'] -- one key and one value presented as the two items of a two-entry sort, at exit 0
; (roadmap Gap R.426, ADR 0417). The walk here steps by two and stops at the entry count, which is the
; same stride question the collector asks in rt_gc and the print doors ask in rt_dict_print_mixed.
;
; The caller names the kind its record holds for the keys, and that answer is used for a TEXT key and
; for nothing else. A dict whose keys are all of one kind is stored through the untagged put and keeps
; their kind in the compiler's own record, so the arena's tag word for a text key says only who slept
; in that slot last -- and a copy that read it printed the intern table's INDEX (xs = sorted(d) over
; {"b": 2, "a": 1} came out [1, 0], two numbers, where the reference prints ['a', 'b']). Every other
; key kind arrives through the tagged put, which wrote the slot's own tag, so the copy takes that and
; a bool key still prints True rather than 1. One question to the record, one question to the slot, and
; the same codegen that chose the sort's comparator answers both -- so the order and the print of a key
; cannot disagree. A dict whose key kinds neither answer can name is refused before this door is built.
;
; The order is entry order -- the insertion order rt_dict_del's compaction
; preserves -- which is the order 'list(d)' and 'for k in d' answer, so the three cannot disagree about
; which keys a dict has and in what sequence.
define internal i32 @rt_dict_keys(i32 %h, i32 %kt) {
entry:
  %n = call i32 @rt_dict_len(i32 %h)
  %nh = call i32 @rt_alloc(i32 1)
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  br label %lo
lo:
  %i = phi i32 [ 0, %entry ], [ %inext, %body ]
  %c = icmp slt i32 %i, %n
  br i1 %c, label %body, label %done
body:
  %w = mul i32 %i, 2
  %kp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %w
  %k = load i32, i32* %kp
  %tp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %w
  %st = load i32, i32* %tp
  %istext = icmp eq i32 %kt, 4
  %tg = select i1 %istext, i32 4, i32 %st
  call void @rt_set_elem(i32 %nh, i32 %i, i32 %k)
  call void @rt_tag_elem(i32 %nh, i32 %i, i32 %tg)
  %inext = add i32 %i, 1
  br label %lo
done:
  ret i32 %nh
}
`
