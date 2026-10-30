// The slice door and the min/max helpers the compiler asks at run time. One block of heapRuntimeIRTmpl.
package lang

// heapIRSlice is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRSlice = `
define internal i32 @rt_max(i32 %a, i32 %b) {
entry:
  %cmp = icmp sgt i32 %a, %b
  br i1 %cmp, label %aret, label %bret
aret:
  ret i32 %a
bret:
  ret i32 %b
}

define internal i32 @rt_min(i32 %a, i32 %b) {
entry:
  %cmp = icmp slt i32 %a, %b
  br i1 %cmp, label %aret, label %bret
aret:
  ret i32 %a
bret:
  ret i32 %b
}

; rt_bound_is_position is the slice door's bound question asked of a TAG. A bound is either a position or
; the default for that side, and the reference words both answers in one sentence — TypeError: slice indices
; must be integers or None or have an __index__ method — so the door needs three verdicts, not a boolean:
; 1 for a word that counts (an int, or a bool, which is an int), 0 for the absence of a bound (None), and -1
; for anything else — the float whose word is a box handle, the text whose word is an @str_tab index, and
; every container, whose word would reach rt_slice's i32 argument as a heap handle or a global address.
; One helper holds the legal-tag set for all three bounds of both slice roads, because the compiler that
; guesses it per road is the compiler that gets xs[ys[0]:2] over ys = ["a"] wrong at the exit code of
; success (roadmap Gap R.237). Tag words are the tagged-value table in value.go: int 0, float 1, bool 2,
; None 3, and every container and text above that.
define internal i32 @rt_bound_is_position(i32 %tag) {
entry:
  %isInt = icmp eq i32 %tag, 0
  %isBool = icmp eq i32 %tag, 2
  %isPos = or i1 %isInt, %isBool
  %isNone = icmp eq i32 %tag, 3
  %asNone = select i1 %isNone, i32 0, i32 -1
  %v = select i1 %isPos, i32 1, i32 %asNone
  ret i32 %v
}

define internal i32 @rt_slice(i32 %h, i32 %low, i32 %high, i32 %step, i32 %hasLow, i32 %hasHigh, i32 %hasStep) {
entry:
  %p = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lenp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %p, i32 0, i32 1
  %len = load i32, i32* %lenp
  %kindp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %p, i32 0, i32 0
  %kind = load i32, i32* %kindp
  %posCmp = icmp sgt i32 %step, 0
  br i1 %posCmp, label %pos, label %neg

pos:
  %ps = alloca i32
  %pt = alloca i32
  store i32 0, i32* %ps
  store i32 %len, i32* %pt
  %hl = icmp ne i32 %hasLow, 0
  br i1 %hl, label %p_low, label %p_hi
p_low:
  %lneg = icmp slt i32 %low, 0
  br i1 %lneg, label %p_low_neg, label %p_low_nn
p_low_neg:
  %l1 = add i32 %len, %low
  %lm = call i32 @rt_max(i32 %l1, i32 0)
  store i32 %lm, i32* %ps
  br label %p_hi
p_low_nn:
  %lmin = call i32 @rt_min(i32 %low, i32 %len)
  store i32 %lmin, i32* %ps
  br label %p_hi
p_hi:
  %hh = icmp ne i32 %hasHigh, 0
  br i1 %hh, label %p_high, label %p_done
p_high:
  %hneg = icmp slt i32 %high, 0
  br i1 %hneg, label %p_high_neg, label %p_high_nn
p_high_neg:
  %h1 = add i32 %len, %high
  %hm = call i32 @rt_max(i32 %h1, i32 0)
  store i32 %hm, i32* %pt
  br label %p_done
p_high_nn:
  %hmin = call i32 @rt_min(i32 %high, i32 %len)
  store i32 %hmin, i32* %pt
  br label %p_done
p_done:
  %s = load i32, i32* %ps
  %t = load i32, i32* %pt
  br label %build

neg:
  %ns = alloca i32
  %nt = alloca i32
  %lmb = sub i32 %len, 1
  store i32 %lmb, i32* %ns
  store i32 -1, i32* %nt
  %nl = icmp ne i32 %hasLow, 0
  br i1 %nl, label %n_low, label %n_hi
n_low:
  %nlneg = icmp slt i32 %low, 0
  br i1 %nlneg, label %n_low_neg, label %n_low_nn
n_low_neg:
  %nl1 = add i32 %len, %low
  %nlm = call i32 @rt_max(i32 %nl1, i32 -1)
  store i32 %nlm, i32* %ns
  br label %n_hi
n_low_nn:
  %nlmin = call i32 @rt_min(i32 %low, i32 %lmb)
  store i32 %nlmin, i32* %ns
  br label %n_hi
n_hi:
  %nh2 = icmp ne i32 %hasHigh, 0
  br i1 %nh2, label %n_high, label %n_done
n_high:
  %nhneg = icmp slt i32 %high, 0
  br i1 %nhneg, label %n_high_neg, label %n_high_nn
n_high_neg:
  %nh1 = add i32 %len, %high
  %nhm = call i32 @rt_max(i32 %nh1, i32 -1)
  store i32 %nhm, i32* %nt
  br label %n_done
n_high_nn:
  %nhmin = call i32 @rt_min(i32 %high, i32 %lmb)
  store i32 %nhmin, i32* %nt
  br label %n_done
n_done:
  %ns2 = load i32, i32* %ns
  %nt2 = load i32, i32* %nt
  br label %build

build:
  %ss = phi i32 [ %s, %p_done ], [ %ns2, %n_done ]
  %tt = phi i32 [ %t, %p_done ], [ %nt2, %n_done ]
  %stp = phi i32 [ %step, %p_done ], [ %step, %n_done ]
  %nh = call i32 @rt_alloc(i32 %kind)
  %np = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %nh
  %nlenp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %np, i32 0, i32 1
  store i32 0, i32* %nlenp
  %ci = alloca i32
  store i32 %ss, i32* %ci
  %cd = alloca i32
  store i32 0, i32* %cd
  br label %loop

loop:
  %i = load i32, i32* %ci
  %spos = icmp sgt i32 %stp, 0
  br i1 %spos, label %cond_pos, label %cond_neg
cond_pos:
  %cp = icmp slt i32 %i, %tt
  br i1 %cp, label %body, label %done
cond_neg:
  %cn = icmp sgt i32 %i, %tt
  br i1 %cn, label %body, label %done
body:
  %srcdp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %p, i32 0, i32 2
  %src = getelementptr [256 x i32], [256 x i32]* %srcdp, i32 0, i32 %i
  %v = load i32, i32* %src
  ; ADR 0187 pairing rule, which this builder had never followed: the operation that writes a
  ; slot payload writes its tag. rt_slice copied payloads into the new handle and left heap_tags
  ; at whatever the reused heap slot held last, so a slice of texts rendered the interned INDEX as
  ; a number where the reference prints the quoted text (roadmap Gap R.179).
  %srctgtp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %sv = load i32, i32* %srctgtp
  %dstp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %np, i32 0, i32 2
  %c = load i32, i32* %cd
  %dst = getelementptr [256 x i32], [256 x i32]* %dstp, i32 0, i32 %c
  store i32 %v, i32* %dst
  %dsttgtp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %nh, i32 %c
  store i32 %sv, i32* %dsttgtp
  %c1 = add i32 %c, 1
  store i32 %c1, i32* %cd
  %i1 = add i32 %i, %stp
  store i32 %i1, i32* %ci
  br label %loop

done:
  %c2 = load i32, i32* %cd
  store i32 %c2, i32* %nlenp
  ; A copy of an object carries the object's own attributes, which is ADR 0251's rule applied to the
  ; one door that had never carried them: the slots and their tags travel, and the object's @estr
  ; word - the interned-text claims and the immutable-container flag - travels with them. Without
  ; this a slice of a tuple arrived claiming to be a list and printed (1, 2)[0:1] as [1], and a slice
  ; of a container that had said "these slots describe themselves" had to be told again by whoever
  ; looked next (roadmap L11.3, ADR 0361; rt_alloc clears the word, so the store is a copy, not an or).
  %esrc = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %h
  %attr = load i32, i32* %esrc
  %edst = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %nh
  store i32 %attr, i32* %edst
  ret i32 %nh
}

%obj = type {i32, i32} ; tagged dynamic value: {tag, payload}
`
