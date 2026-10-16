// The sequence doors: rt_seq_concat, rt_seq_repeat and rt_str_repeat — the three helpers the `+`
// between two sequences and the `*` between a sequence and a whole number ask for. One gated block,
// in the company of the arithmetic, formatting and case doors (codegen_ir_numeric.go,
// percent_runtime.go, codegen_ir_utf8.go).
package lang

// seqRuntimeIRTmpl is the runtime half of sequence operations (roadmap Gap R.33, ADR 0365).
//
// The compiled backend had always refused all three of these. The refusal was written when a container
// lowered to the address of a compile-time global — `mul i32 @.lst1, 3`, `add i32 @.lst1, @.lst2`, which
// is the module `llc` rejects and ADR 0166 reserves exit 2 for — and it stayed written after the heap
// moved: a container is a heap object now, its slots are `{kind, length, [256 x i32]}` beside the parallel
// `@heap_tags`, and the two questions `a + b` and `xs * n` ask are exactly the questions `rt_slice`
// already answers for `xs[i:j]`. What the slice door had and these did not is a builder.
//
// Three rules the block keeps, all of them already the house's:
//
//   - **ADR 0187's pairing rule.** The operation that copies a slot's payload copies its tag in the same
//     breath. A concatenated list whose tags came from whatever the reused heap slot held last prints an
//     interned text's index as a number — the exact failure `rt_slice` was found doing (Gap R.179).
//   - **ADR 0251's rule that the object owns its attributes.** The new object is born self-describing (bit
//     8), because its slots are the pair whatever the two operands were built as, and it inherits the
//     immutable-container bit from the operands the door asked about, which is the only place the
//     parentheses of `(1, 2) + (3,)` are knowable (L11.3, ADR 0361). The attribute word arrives as an
//     argument rather than being spelled here: the heap family does not own the tuple bit (ADR 0361 said
//     so of `rt_tuple_flag`), and the door that read the AST is the one that knows the spelling.
//   - **ADR 0229's rule about a built text.** `rt_str_repeat` does not invent a second string shape: it
//     fills a buffer, interns it, and answers the same `@str_tab` index a text literal answers, so every
//     text road — the printer, `len`, a subscript, a method, a loop — takes the answer without knowing
//     what built it. This is `rt_str_cat`'s shape with one operand repeated.
//
// The element count is not capped here. One object's element row is 256 words and a repeat can ask for
// more than that; the row that owns what happens when a container outgrows the row is Gap R.271, which
// `xs.append` in a loop already reaches, and the door in `sequence_ops.go` declines a repeat it can
// *prove* would pass the row rather than build it.
const seqRuntimeIRTmpl = `
; rt_seq_concat is the list — or the tuple — that a + b denotes: a fresh object of the left
; operand's physical kind whose slots are the left operand's payloads with their tags, then the right
; operand's payloads with their tags. The two operands are both containers of the list layout, which is
; the door's promise, not this helper's: a dict or a set never reaches here (the reference raises for
; those, and the raise is emitted in words by the door that owns the sentence).
define internal i32 @rt_seq_concat(i32 %a, i32 %b, i32 %attrs) {
entry:
  %ao = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %a
  %akp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ao, i32 0, i32 0
  %kind = load i32, i32* %akp
  %alp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ao, i32 0, i32 1
  %alen = load i32, i32* %alp
  %bo = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %b
  %blp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %bo, i32 0, i32 1
  %blen = load i32, i32* %blp
  %nh = call i32 @rt_alloc(i32 %kind)
  %no = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %nh
  %nlp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %no, i32 0, i32 1
  store i32 0, i32* %nlp
  br label %walkA
walkA:
  %ia = phi i32 [ 0, %entry ], [ %ian, %copyA ]
  %haveA = icmp slt i32 %ia, %alen
  br i1 %haveA, label %copyA, label %walkB
copyA:
  %aep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ao, i32 0, i32 2, i32 %ia
  %av = load i32, i32* %aep
  %atp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %a, i32 %ia
  %at = load i32, i32* %atp
  %ca = load i32, i32* %nlp
  %adp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %no, i32 0, i32 2, i32 %ca
  store i32 %av, i32* %adp
  %adtp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %nh, i32 %ca
  store i32 %at, i32* %adtp
  %can = add i32 %ca, 1
  store i32 %can, i32* %nlp
  %ian = add i32 %ia, 1
  br label %walkA
walkB:
  %ib = phi i32 [ 0, %walkA ], [ %ibn, %copyB ]
  %haveB = icmp slt i32 %ib, %blen
  br i1 %haveB, label %copyB, label %seal
copyB:
  %bep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %bo, i32 0, i32 2, i32 %ib
  %bv = load i32, i32* %bep
  %btp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %b, i32 %ib
  %bt = load i32, i32* %btp
  %cb = load i32, i32* %nlp
  %bdp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %no, i32 0, i32 2, i32 %cb
  store i32 %bv, i32* %bdp
  %bdtp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %nh, i32 %cb
  store i32 %bt, i32* %bdtp
  %cbn = add i32 %cb, 1
  store i32 %cbn, i32* %nlp
  %ibn = add i32 %ib, 1
  br label %walkB
seal:
  ; The new object describes its own slots, whatever the two operands were built as, and inherits the
  ; one attribute the door knows and the slots do not: the immutable spelling (ADR 0361).
  %eslot = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %nh
  store i32 %attrs, i32* %eslot
  ret i32 %nh
}

; rt_seq_repeat is the list — or the tuple — that xs * n denotes, and n * xs is the same object:
; the source's slots, written %n times over. A negative count is the empty sequence, which is what the
; reference answers for [1] * -1 and why the clamp is here rather than in the door: the reference
; clamps, it does not raise.
define internal i32 @rt_seq_repeat(i32 %h, i32 %n, i32 %attrs) {
entry:
  %isneg = icmp slt i32 %n, 0
  %count = select i1 %isneg, i32 0, i32 %n
  %ho = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %hp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ho, i32 0, i32 0
  %kind = load i32, i32* %hp
  %lenp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ho, i32 0, i32 1
  %len = load i32, i32* %lenp
  %nh = call i32 @rt_alloc(i32 %kind)
  %no = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %nh
  %nlp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %no, i32 0, i32 1
  store i32 0, i32* %nlp
  br label %rounds
rounds:
  %r = phi i32 [ 0, %entry ], [ %rn, %nextRound ]
  %done = icmp slt i32 %r, %count
  br i1 %done, label %roundBody, label %seal
roundBody:
  %slot = phi i32 [ 0, %rounds ], [ %sn, %copy ]
  %has = icmp slt i32 %slot, %len
  br i1 %has, label %copy, label %nextRound
copy:
  %sep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ho, i32 0, i32 2, i32 %slot
  %v = load i32, i32* %sep
  %stp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %slot
  %t = load i32, i32* %stp
  %w = load i32, i32* %nlp
  %dep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %no, i32 0, i32 2, i32 %w
  store i32 %v, i32* %dep
  %dtp = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %nh, i32 %w
  store i32 %t, i32* %dtp
  %w1 = add i32 %w, 1
  store i32 %w1, i32* %nlp
  %sn = add i32 %slot, 1
  br label %roundBody
nextRound:
  %rn = add i32 %r, 1
  br label %rounds
seal:
  %eslot = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %nh
  store i32 %attrs, i32* %eslot
  ret i32 %nh
}

; rt_str_repeat is the text that "ab" * n denotes, built the way rt_str_cat builds a concatenation:
; malloc the bytes, copy, terminate, intern, and answer the @str_tab index. The answer is one word and
; it is the same word a text literal answers, which is the whole reason the repeat is a text and not a
; new value kind (ADR 0229). A negative count is the empty text, which the reference answers and this
; clamps to a zero-length buffer — the empty text's own index.
define internal i32 @rt_str_repeat(i32 %s, i32 %n) {
entry:
  %isneg = icmp slt i32 %n, 0
  %count = select i1 %isneg, i32 0, i32 %n
  %slen = call i32 @rt_str_len(i32 %s)
  %total = mul i32 %slen, %count
  %sz = add i32 %total, 1
  %sz64 = zext i32 %sz to i64
  %buf = call ptr @malloc(i64 %sz64)
  %sp = call i8* @rt_str_ptr(i32 %s)
  br label %rounds
rounds:
  %r = phi i32 [ 0, %entry ], [ %rn, %nextRound ]
  %done = icmp slt i32 %r, %count
  br i1 %done, label %roundBody, label %term
roundBody:
  %slot = phi i32 [ 0, %rounds ], [ %sn, %copy ]
  %has = icmp slt i32 %slot, %slen
  br i1 %has, label %copy, label %nextRound
copy:
  %src = getelementptr i8, i8* %sp, i32 %slot
  %ch = load i8, i8* %src
  %base = mul i32 %r, %slen
  %off = add i32 %base, %slot
  %dst = getelementptr i8, i8* %buf, i32 %off
  store i8 %ch, i8* %dst
  %sn = add i32 %slot, 1
  br label %roundBody
nextRound:
  %rn = add i32 %r, 1
  br label %rounds
term:
  %t = getelementptr i8, i8* %buf, i32 %total
  store i8 0, i8* %t
  %idx = call i32 @rt_str_intern(i8* %buf)
  ret i32 %idx
}
`
