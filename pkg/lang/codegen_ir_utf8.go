// The UTF-8 text doors: a text is measured in code points, and every index, slice and character question is
// answered against a byte offset (roadmap L11.5, ADR 0362). One block of heapRuntimeIRTmpl.
package lang

// heapIRUtf8 is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRUtf8 = `
; ---------------------------------------------------------------------------
; Runtime string operations (ADR 0229). Everything above this line treated a string as a
; compile-time fact: an @str_tab index chosen by the compiler, with the text already in the
; module. But a string value is an index into a table the runtime can *add to* (rt_str_intern
; is content-addressed), so an operation asked about at run time has somewhere to live after
; all: these helpers take indices and return indices, and the compiled convention from
; ADR 0224 never changes — print, ==, substring tests and the container paths keep working on
; the result.
; Counting is in code points, not bytes, because that is what a position means in this
; language (ADR 0225): a byte is 0x80..0xBF if it is a UTF-8 continuation byte, and the bytes
; that are not continuations are exactly the code points.
; ---------------------------------------------------------------------------

; rt_str_from_bytes is the only way a string that did not exist at compile time becomes a
; value: copy the bytes, NUL-terminate, intern. Dedup is by content, so a runtime-built "b"
; and the literal "b" are the same index and compare equal without being told to.
define internal i32 @rt_str_from_bytes(i8* %p, i32 %n) {
entry:
  %n1 = add i32 %n, 1
  %sz = zext i32 %n1 to i64
  %buf = call ptr @malloc(i64 %sz)
  br label %copy
copy:
  %i = phi i32 [ 0, %entry ], [ %inext, %body ]
  %done = icmp sge i32 %i, %n
  br i1 %done, label %term, label %body
body:
  %src = getelementptr i8, i8* %p, i32 %i
  %ch = load i8, i8* %src
  %dst = getelementptr i8, i8* %buf, i32 %i
  store i8 %ch, i8* %dst
  %inext = add i32 %i, 1
  br label %copy
term:
  %t = getelementptr i8, i8* %buf, i32 %n
  store i8 0, i8* %t
  %idx = call i32 @rt_str_intern(i8* %buf)
  ret i32 %idx
}

; rt_str_nchars counts code points: the bytes that are not UTF-8 continuation bytes.
define internal i32 @rt_str_nchars(i32 %s) {
entry:
  %p = call i8* @rt_str_ptr(i32 %s)
  br label %scan
scan:
  %n = phi i32 [ 0, %entry ], [ %nnext, %body ]
  %o = phi i32 [ 0, %entry ], [ %onext, %body ]
  %at = getelementptr i8, i8* %p, i32 %o
  %b = load i8, i8* %at
  %end = icmp eq i8 %b, 0
  br i1 %end, label %fin, label %body
body:
  %m = and i8 %b, -64
  %cont = icmp eq i8 %m, -128
  %isz = zext i1 %cont to i32
  %step = sub i32 1, %isz
  %nnext = add i32 %n, %step
  %onext = add i32 %o, 1
  br label %scan
fin:
  ret i32 %n
}

; rt_str_char answers s[i] as a one-character string: walk to the i-th code point, measure it,
; intern it. A negative index counts from the end, like every other position in this language
; (ADR 0210). -1 says there is no such position — the caller raises IndexError, because a trap
; the program can name has to be raisable by the code that knows the source (ADR 0212).
define internal i32 @rt_str_char(i32 %s, i32 %i) {
entry:
  %n = call i32 @rt_str_nchars(i32 %s)
  %neg = icmp slt i32 %i, 0
  %addn = add i32 %i, %n
  %iad = select i1 %neg, i32 %addn, i32 %i
  %lo = icmp slt i32 %iad, 0
  %hi = icmp sge i32 %iad, %n
  %bad = or i1 %lo, %hi
  br i1 %bad, label %oor, label %walk
oor:
  ret i32 -1
walk:
  %p = call i8* @rt_str_ptr(i32 %s)
  br label %wloop
wloop:
  %o = phi i32 [ 0, %walk ], [ %onext, %advance ]
  %c = phi i32 [ 0, %walk ], [ %cnext, %advance ]
  %at = getelementptr i8, i8* %p, i32 %o
  %b = load i8, i8* %at
  %end = icmp eq i8 %b, 0
  br i1 %end, label %oor, label %wbody
wbody:
  %m = and i8 %b, -64
  %iscont = icmp eq i8 %m, -128
  %isstart = icmp ne i8 %m, -128
  %wanted = icmp eq i32 %c, %iad
  %found = and i1 %isstart, %wanted
  br i1 %found, label %measure, label %advance
advance:
  %csz = zext i1 %isstart to i32
  %cnext = add i32 %c, %csz
  %onext = add i32 %o, 1
  br label %wloop
measure:
  %o1 = add i32 %o, 1
  br label %mloop
mloop:
  %q = phi i32 [ %o1, %measure ], [ %qnext, %mstep ]
  %at2 = getelementptr i8, i8* %p, i32 %q
  %b2 = load i8, i8* %at2
  %end2 = icmp eq i8 %b2, 0
  br i1 %end2, label %mk, label %mbody
mbody:
  %m2 = and i8 %b2, -64
  %cont2 = icmp eq i8 %m2, -128
  br i1 %cont2, label %mstep, label %mk
mstep:
  %qnext = add i32 %q, 1
  br label %mloop
mk:
  %len = sub i32 %q, %o
  %base = getelementptr i8, i8* %p, i32 %o
  %idx = call i32 @rt_str_from_bytes(i8* %base, i32 %len)
  ret i32 %idx
}

; rt_str_codepoint is ord(): the value of the string's single code point, or -1 when the
; string is empty or holds more than one. Decoding is the standard UTF-8 shape — lead byte
; gives the length and the high bits, each continuation contributes six more.
define internal i32 @rt_str_codepoint(i32 %s) {
entry:
  %p = call i8* @rt_str_ptr(i32 %s)
  %at0 = getelementptr i8, i8* %p, i32 0
  %b0 = load i8, i8* %at0
  %empty = icmp eq i8 %b0, 0
  br i1 %empty, label %bad, label %lead
lead:
  %m7 = and i8 %b0, -128
  %asc = icmp eq i8 %m7, 0
  %m32 = and i8 %b0, -32
  %two = icmp eq i8 %m32, -64
  %m16 = and i8 %b0, -16
  %three = icmp eq i8 %m16, -32
  %lenasc = select i1 %asc, i32 1, i32 4
  %lentwo = select i1 %two, i32 2, i32 %lenasc
  %lenthr = select i1 %three, i32 3, i32 %lentwo
  %maskasc = and i8 %b0, 127
  %masktwo = and i8 %b0, 31
  %maskthr = and i8 %b0, 15
  %maskfour = and i8 %b0, 7
  %masctwo = select i1 %two, i8 %masktwo, i8 %maskfour
  %mascth = select i1 %three, i8 %maskthr, i8 %masctwo
  %mask = select i1 %asc, i8 %maskasc, i8 %mascth
  %v0 = sext i8 %mask to i32
  br label %walk
walk:
  %k = phi i32 [ 1, %lead ], [ %knext, %step ]
  %v = phi i32 [ %v0, %lead ], [ %vnext, %step ]
  %done = icmp sge i32 %k, %lenthr
  br i1 %done, label %check, label %cont
cont:
  %at = getelementptr i8, i8* %p, i32 %k
  %b = load i8, i8* %at
  %m = and i8 %b, -64
  %ok = icmp eq i8 %m, -128
  br i1 %ok, label %step, label %bad
step:
  %low = and i8 %b, 63
  %lz = zext i8 %low to i32
  %shl = shl i32 %v, 6
  %vnext = or i32 %shl, %lz
  %knext = add i32 %k, 1
  br label %walk
check:
  %atn = getelementptr i8, i8* %p, i32 %lenthr
  %bn = load i8, i8* %atn
  %just = icmp eq i8 %bn, 0
  br i1 %just, label %fin, label %bad
fin:
  ret i32 %v
bad:
  ret i32 -1
}

; The case door used to live here: a loop over ASCII that copied every other byte unchanged, which is the
; documented shape of the first cut (Gap R.47) and a wrong answer now that the compiled backend is the only
; one (ADR 0302). It lives in pkg/lang/case_runtime.go, walks the table generated from the reference, and is
; gated — a module that never asks a case question does not carry it (roadmap L11.5, ADR 0362).

; rt_str_byteoff is the byte offset where the k-th code point begins, with the clamping a
; slice needs: negative counts from the end, out of range collapses to an end, and k=0 is 0.
; Code points are the unit because a position in this language is a code point (ADR 0225).
define internal i32 @rt_str_byteoff(i32 %s, i32 %k) {
entry:
  %p = call i8* @rt_str_ptr(i32 %s)
  %n = call i32 @rt_str_nchars(i32 %s)
  %neg = icmp slt i32 %k, 0
  %kadd = add i32 %k, %n
  %kad = select i1 %neg, i32 %kadd, i32 %k
  %below = icmp slt i32 %kad, 0
  %k0 = select i1 %below, i32 0, i32 %kad
  %above = icmp sgt i32 %k0, %n
  %kcl = select i1 %above, i32 %n, i32 %k0
  br label %scan
scan:
  %o = phi i32 [ 0, %entry ], [ %oe, %ech ]
  %c = phi i32 [ 0, %entry ], [ %cn, %ech ]
  %done = icmp sge i32 %c, %kcl
  br i1 %done, label %fin, label %at
at:
  %a = getelementptr i8, i8* %p, i32 %o
  %b = load i8, i8* %a
  %end = icmp eq i8 %b, 0
  %o1 = add i32 %o, 1
  br i1 %end, label %fin, label %walk
walk:
  ; A code point is its lead byte AND its continuation bytes, so the walk past one character does not
  ; stop at the second byte because that byte is not a lead byte: it stops at the first byte that is not
  ; a continuation. Stopping on the lead byte instead is what made s[1:3] of "héllo" answer 'é' where the
  ; language's own position-is-a-code-point rule (ADR 0225) says 'él' — a two-byte character lost its
  ; tail, and every offset measured from it was one byte short (roadmap Gap R.47, found by the
  ; printf-style door's character-width padding, roadmap Gap R.31).
  %w = phi i32 [ %o1, %at ], [ %wn, %wstep ]
  %wa = getelementptr i8, i8* %p, i32 %w
  %wv = load i8, i8* %wa
  %wnn = icmp ne i8 %wv, 0
  %wm = and i8 %wv, -64
  %wcont = icmp eq i8 %wm, -128
  %wgo = and i1 %wcont, %wnn
  br i1 %wgo, label %wstep, label %ech
wstep:
  %wn = add i32 %w, 1
  br label %walk
ech:
  %cn = add i32 %c, 1
  %oe = add i32 %w, 0
  br label %scan
fin:
  ret i32 %o
}

; rt_str_slice answers s[a:b] when the bounds are values rather than constants. Two sentinels
; stand in for a bound the source left out — INT_MIN means "from the start", INT_MAX means "to
; the end" — because there is no absent i32; everything else is rt_str_byteoff plus a copy.
define internal i32 @rt_str_slice(i32 %s, i32 %lo, i32 %hi) {
entry:
  %n = call i32 @rt_str_nchars(i32 %s)
  %noLo = icmp eq i32 %lo, -2147483648
  %loSel = select i1 %noLo, i32 0, i32 %lo
  %noHi = icmp eq i32 %hi, 2147483647
  %hiSel = select i1 %noHi, i32 %n, i32 %hi
  %bo = call i32 @rt_str_byteoff(i32 %s, i32 %loSel)
  %eo = call i32 @rt_str_byteoff(i32 %s, i32 %hiSel)
  %rev = icmp slt i32 %eo, %bo
  %d = sub i32 %eo, %bo
  %len = select i1 %rev, i32 0, i32 %d
  %p = call i8* @rt_str_ptr(i32 %s)
  %base = getelementptr i8, i8* %p, i32 %bo
  %idx = call i32 @rt_str_from_bytes(i8* %base, i32 %len)
  ret i32 %idx
}

; rt_str_cat joins two interned strings: allocate both lengths plus a terminator, copy, intern.
; Interning dedups by content, so a" + word() of "b" is the same index as the literal "ab"
; wherever it appears, and equality between a built string and a literal needs no special case.
define internal i32 @rt_str_cat(i32 %a, i32 %b) {
entry:
  %al = call i32 @rt_str_len(i32 %a)
  %bl = call i32 @rt_str_len(i32 %b)
  %tl = add i32 %al, %bl
  %t1 = add i32 %tl, 1
  %sz = zext i32 %t1 to i64
  %buf = call ptr @malloc(i64 %sz)
  %ap = call i8* @rt_str_ptr(i32 %a)
  %bp = call i8* @rt_str_ptr(i32 %b)
  br label %copyA
copyA:
  %i = phi i32 [ 0, %entry ], [ %ianext, %bodyA ]
  %adone = icmp slt i32 %i, %al
  br i1 %adone, label %bodyA, label %copyB
bodyA:
  %asrc = getelementptr i8, i8* %ap, i32 %i
  %ach = load i8, i8* %asrc
  %adst = getelementptr i8, i8* %buf, i32 %i
  store i8 %ach, i8* %adst
  %ianext = add i32 %i, 1
  br label %copyA
copyB:
  %j = phi i32 [ 0, %copyA ], [ %jbnext, %bodyB ]
  %bdone = icmp slt i32 %j, %bl
  br i1 %bdone, label %bodyB, label %term
bodyB:
  %bsrc = getelementptr i8, i8* %bp, i32 %j
  %bch = load i8, i8* %bsrc
  %bdst = getelementptr i8, i8* %buf, i32 %j
  %bdstof = getelementptr i8, i8* %bdst, i32 %al
  store i8 %bch, i8* %bdstof
  %jbnext = add i32 %j, 1
  br label %copyB
term:
  %t = getelementptr i8, i8* %buf, i32 %tl
  store i8 0, i8* %t
  %idx = call i32 @rt_str_intern(i8* %buf)
  ret i32 %idx
}


; rt_str_of_int is str(n) for a number the compiler cannot read: digits written backwards into
; a block that is then interned from the first digit onward. the record has always answered
; this; the compiled backend refused, so str(get()) was unreachable while str(42) worked.
define internal i32 @rt_str_of_int(i32 %v) {
entry:
  %buf = call ptr @malloc(i64 16)
  %tail = getelementptr i8, i8* %buf, i32 15
  store i8 0, i8* %tail
  %neg = icmp slt i32 %v, 0
  %negv = sub i32 0, %v
  %u0 = select i1 %neg, i32 %negv, i32 %v
  br label %digits
digits:
  %u = phi i32 [ %u0, %entry ], [ %unext, %digits ]
  %p = phi i32 [ 15, %entry ], [ %pnext, %digits ]
  %d = urem i32 %u, 10
  %ch = add i32 %d, 48
  %c8 = trunc i32 %ch to i8
  %at = getelementptr i8, i8* %buf, i32 %p
  %atm = getelementptr i8, i8* %at, i32 -1
  store i8 %c8, i8* %atm
  %unext = udiv i32 %u, 10
  %pnext = sub i32 %p, 1
  %more = icmp ne i32 %unext, 0
  br i1 %more, label %digits, label %after
after:
  %p2 = phi i32 [ %pnext, %digits ]
  br i1 %neg, label %putminus, label %fin
putminus:
  %ma = getelementptr i8, i8* %buf, i32 %p2
  %mam = getelementptr i8, i8* %ma, i32 -1
  store i8 45, i8* %mam
  %p3 = sub i32 %p2, 1
  br label %fin
fin:
  %start = phi i32 [ %p2, %after ], [ %p3, %putminus ]
  %sp = getelementptr i8, i8* %buf, i32 %start
  %idx = call i32 @rt_str_intern(i8* %sp)
  ret i32 %idx
}

; rt_repr_of_text is the run-time half of Python's quoting (roadmap L11.2, ADR 0258).
;
; @str_tab/@str_repr_tab have carried a text's repr since ADR 0224, but only for text the
; compiler could see: rt_str_intern2 was handed both forms, and the single-argument
; rt_str_intern — the one every runtime-built text goes through — filled the raw slot and left
; the repr slot null. A text built at run time inside a container therefore printed as
; printf("%s", null), which glibc spells (null): n = 7 / xs.append("v" + str(n)) /
; print(xs) printed [(null)] where CPython prints ['v7']. Making repr() a builtin makes
; the quoting rule unavoidable at run time, so it lives here beside the Go table in
; render.go, and the pair table test holds the two to CPython's answers rather than to each
; other's.
define internal i8* @rt_repr_of_text(i8* %raw) {
entry:
  br label %length
; The length is counted here rather than asked of @strlen: this block travels with the heap
; runtime, and strlen's declare lives in the string block, so a module that renders a repr
; without ever building a string had the call and not the declaration — llc called that a use of
; an undefined value on a program that never mentioned a string function (ADR 0209's rule: a
; runtime block carries what it calls; roadmap L11.2, ADR 0258).
length:
  %n = phi i64 [ 0, %entry ], [ %nnext, %stepl ]
  %cand = getelementptr i8, i8* %raw, i64 %n
  %cb = load i8, i8* %cand
  %end = icmp eq i8 %cb, 0
  br i1 %end, label %sized, label %stepl
stepl:
  %nnext = add i64 %n, 1
  br label %length
sized:
  %w = mul i64 %n, 2
  %sz = add i64 %w, 3
  %buf = call ptr @malloc(i64 %sz)
  br label %scan
scan:
  ; The predecessor is sized, not entry: the byte count above sits in between, and a phi names
  ; the blocks that actually branch to it (llc calls a wrong one a malformed PHI).
  %i = phi i64 [ 0, %sized ], [ %inext, %scanbody ]
  %hasq = phi i32 [ 0, %sized ], [ %qnext, %scanbody ]
  %hasd = phi i32 [ 0, %sized ], [ %dnext, %scanbody ]
  %done = icmp eq i64 %i, %n
  br i1 %done, label %choose, label %scanbody
scanbody:
  %sp = getelementptr i8, i8* %raw, i64 %i
  %c = load i8, i8* %sp
  %issq = icmp eq i8 %c, 39
  %isdq = icmp eq i8 %c, 34
  %tq = select i1 %issq, i32 1, i32 0
  %qnext = add i32 %hasq, %tq
  %td = select i1 %isdq, i32 1, i32 0
  %dnext = add i32 %hasd, %td
  %inext = add i64 %i, 1
  br label %scan
choose:
  %qseen = icmp ne i32 %hasq, 0
  %dseen = icmp eq i32 %hasd, 0
  %double = and i1 %qseen, %dseen
  %q = select i1 %double, i8 34, i8 39
  %op = getelementptr i8, i8* %buf, i64 0
  store i8 %q, i8* %op
  br label %write
write:
  %j = phi i64 [ 1, %choose ], [ %jnext3, %next ]
  %k = phi i64 [ 0, %choose ], [ %knext, %next ]
  %done2 = icmp eq i64 %k, %n
  br i1 %done2, label %term, label %body
body:
  %src = getelementptr i8, i8* %raw, i64 %k
  %ch = load i8, i8* %src
  %isq = icmp eq i8 %ch, %q
  %isbs = icmp eq i8 %ch, 92
  %isnl = icmp eq i8 %ch, 10
  %istab = icmp eq i8 %ch, 9
  %any1 = or i1 %isq, %isbs
  %any2 = or i1 %any1, %isnl
  %esc = or i1 %any2, %istab
  br i1 %esc, label %emitesc, label %emitplain
emitesc:
  %d1 = getelementptr i8, i8* %buf, i64 %j
  store i8 92, i8* %d1
  %j1 = add i64 %j, 1
  %s1 = select i1 %isq, i8 %q, i8 0
  %s2 = select i1 %isbs, i8 92, i8 %s1
  %s3 = select i1 %isnl, i8 110, i8 %s2
  %s4 = select i1 %istab, i8 116, i8 %s3
  %d2 = getelementptr i8, i8* %buf, i64 %j1
  store i8 %s4, i8* %d2
  %je = add i64 %j1, 1
  br label %next
emitplain:
  %d3 = getelementptr i8, i8* %buf, i64 %j
  store i8 %ch, i8* %d3
  %jp = add i64 %j, 1
  br label %next
next:
  %jnext3 = phi i64 [ %je, %emitesc ], [ %jp, %emitplain ]
  %knext = add i64 %k, 1
  br label %write
term:
  ; The closing quote is the character this block existed to write and did not: a text built at
  ; run time came back with an opening quote and no closing one, so xs.append("v" + str(7))
  ; printed ['v7] where CPython prints ['v7']. The buffer was sized for it (2n + 3), so the only
  ; thing missing was the store (roadmap L11.2, ADR 0258).
  %closep = getelementptr i8, i8* %buf, i64 %j
  store i8 %q, i8* %closep
  %close1 = add i64 %j, 1
  %t = getelementptr i8, i8* %buf, i64 %close1
  store i8 0, i8* %t
  ret i8* %buf
}

; rt_str_repr_ptr hands back a text's quoted form, building it the first time it is asked for.
; Interning a run-time text fills the raw slot only, so the repr is derived on demand and cached
; in the same table — which is also what makes a runtime-built element inside a container print
; as 'v7' instead of the (null) an unfilled slot printed.
define internal i8* @rt_str_repr_ptr(i32 %i) {
entry:
  %slot = getelementptr [4096 x i8*], [4096 x i8*]* @str_repr_tab, i32 0, i32 %i
  %p = load i8*, i8** %slot
  %missing = icmp eq i8* %p, null
  br i1 %missing, label %build, label %have
build:
  %raw = call i8* @rt_str_ptr(i32 %i)
  %r = call i8* @rt_repr_of_text(i8* %raw)
  store i8* %r, i8** %slot
  ret i8* %r
have:
  ret i8* %p
}

; rt_str_intern2 interns the raw text and its repr together: dedup is by raw text (so two
; spellings of "k" are the same dict key), and index i always has both forms available.
define internal i32 @rt_str_intern2(i8* %raw, i8* %repr) {
entry:
  %n0 = load i32, i32* @str_count
  br label %scan
scan:
  %i = phi i32 [ 0, %entry ], [ %inext, %next ]
  %more = icmp slt i32 %i, %n0
  br i1 %more, label %cmp, label %add
cmp:
  %slot = getelementptr [4096 x i8*], [4096 x i8*]* @str_tab, i32 0, i32 %i
  %q = load i8*, i8** %slot
  %r = call i32 @strcmp(i8* %raw, i8* %q)
  %same = icmp eq i32 %r, 0
  br i1 %same, label %found, label %next
next:
  %inext = add i32 %i, 1
  br label %scan
found:
  %rs = getelementptr [4096 x i8*], [4096 x i8*]* @str_repr_tab, i32 0, i32 %i
  store i8* %repr, i8** %rs
  ret i32 %i
add:
  %oob = icmp sge i32 %n0, 256
  br i1 %oob, label %full, label %put
put:
  %slot2 = getelementptr [4096 x i8*], [4096 x i8*]* @str_tab, i32 0, i32 %n0
  store i8* %raw, i8** %slot2
  %rs2 = getelementptr [4096 x i8*], [4096 x i8*]* @str_repr_tab, i32 0, i32 %n0
  store i8* %repr, i8** %rs2
  %n1 = add i32 %n0, 1
  store i32 %n1, i32* @str_count
  ret i32 %n0
full:
  %last = sub i32 %n0, 1
  ret i32 %last
}
`
