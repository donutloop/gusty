// numArithRuntimeIRTmpl is the arithmetic door the compiler cannot answer for a slot before the module is
// written — what kind of number lives here — and the TypeError/ZeroDivisionError sentences that come with it.
package lang

// numArithRuntimeIR is the arithmetic door for the one question the compiler cannot ask a slot
// before the module is written: **what kind of number lives here**. A slot read out of a container
// the pass cannot see (`xs = []` / `xs.append([7, 8])` / `xs[0][0] + 1`) carries its tag with it, and
// the tag decides everything about the answer — whether `+ 1` prints `8` or `8.5`, and whether a text
// operand is a number or CPython's `TypeError: unsupported operand type(s) for +: 'list' and 'int'`.
// Until now the compiled backend declined the whole question (roadmap L11.1's remaining numeric
// clause, ADR 0265); the interpreter had been answering it correctly all along, which is what made it
// a divergence rather than a missing feature.
//
// The shape is ADR 0253's (`/` is a float whatever arrives, so one lift was enough) generalised to the
// operators whose *answer* changes kind: the helper takes both (payload, tag) pairs, does the
// arithmetic in double — the one word that holds both families exactly for the magnitudes at stake
// here — and hands back the payload **and** the tag the caller must print with. The raise stays with
// the caller: the helper fills a static buffer and returns a status, and the emitted code does the
// store-and-branch every raise in this language does, so `except TypeError:` reaches it (ADR 0228).
//
// The int arm is guarded before the `fptosi`, for the reason Gap R.133 learned the day it was filed:
// out of the compiled `int` word the truncation is poison, not a wrong number, so an answer that would
// not fit raises rather than wrapping (L12.12 owns the word; until then it owns the message).
const numArithRuntimeIRTmpl = `@rt.num.fmt = private constant [61 x i8] c"TypeError: unsupported operand type(s) for %s: '%s' and '%s'\00"
@rt.num.negfmt = private constant [46 x i8] c"TypeError: bad operand type for unary -: '%s'\00"
@rt.num.absfmt = private constant [44 x i8] c"TypeError: bad operand type for abs(): '%s'\00"
; The third sentence the reference writes for the plus operator, for the three kinds that know how to
; concatenate: str + int, list + int and tuple + int are not "unsupported operand" at all, they are the
; concatenation method reporting what it was handed — can only concatenate str (not "int") to str. dict,
; set, None and an instance have no concat method for a number and DO write the generic sentence, which is
; why the arm below tests three tags and not "anything that is not a number" (roadmap Gap R.442, ADR 0265's
; per-kind rule).
@rt.num.catfmt = private constant [52 x i8] c"TypeError: can only concatenate %s (not \22%s\22) to %s\00"
@rt.num.ofmt = private constant [121 x i8] c"OverflowError: the whole number the arithmetic would answer is beyond the word this backend's int holds (roadmap L12.12)\00"
@rt.num.msg = private global [192 x i8] zeroinitializer
@rt.k.int = private constant [4 x i8] c"int\00"
@rt.k.float = private constant [6 x i8] c"float\00"
@rt.k.bool = private constant [5 x i8] c"bool\00"
@rt.k.none = private constant [9 x i8] c"NoneType\00"
@rt.k.str = private constant [4 x i8] c"str\00"
@rt.k.list = private constant [5 x i8] c"list\00"
@rt.k.dict = private constant [5 x i8] c"dict\00"
@rt.k.set = private constant [4 x i8] c"set\00"
@rt.k.tuple = private constant [6 x i8] c"tuple\00"
@rt.k.obj = private constant [7 x i8] c"object\00"
@rt.o.add = private constant [2 x i8] c"+\00"
@rt.o.sub = private constant [2 x i8] c"-\00"
@rt.o.mul = private constant [2 x i8] c"*\00"
@rt.o.fdiv = private constant [3 x i8] c"//\00"
@rt.o.mod = private constant [2 x i8] c"%\00"
; The four ZeroDivisionError sentences the reference chooses between, by the operator and by the operands'
; kinds. It is not two sentences and not three: '//' and '%' share one wording across the two integer cases
; only for '//', because CPython answers '7 % 0' with "integer modulo by zero" and '7 // 0' with "integer
; division or modulo by zero". The tags decide which is stored, so a parameter that carries a double says
; "float modulo" where the same line with an int says "integer modulo by zero" (roadmap Gap R.162, ADR 0278).
@rt.num.dzi = private constant [54 x i8] c"ZeroDivisionError: integer division or modulo by zero\00"
@rt.num.dzm2 = private constant [42 x i8] c"ZeroDivisionError: integer modulo by zero\00"
@rt.num.dzf = private constant [48 x i8] c"ZeroDivisionError: float floor division by zero\00"
@rt.num.dzm = private constant [32 x i8] c"ZeroDivisionError: float modulo\00"

; rt_kind_name answers the word CPython puts inside the quotes of its operand-type message, from the
; canonical tag in value.go — the same table the printer, the comparison and the record read, so
; one tag vocabulary names a value everywhere and nowhere twice. The default is 'object', which is
; what the reference calls an instance of a user class.
define internal i8* @rt_kind_name(i32 %t) {
entry:
  %e0 = icmp eq i32 %t, 0
  %e1 = icmp eq i32 %t, 1
  %e2 = icmp eq i32 %t, 2
  %e3 = icmp eq i32 %t, 3
  %e4 = icmp eq i32 %t, 4
  %e5 = icmp eq i32 %t, 5
  %e6 = icmp eq i32 %t, 6
  %e7 = icmp eq i32 %t, 7
  %e8 = icmp eq i32 %t, 8
  %n8 = select i1 %e8, i8* getelementptr inbounds ([6 x i8], [6 x i8]* @rt.k.tuple, i32 0, i32 0), i8* getelementptr inbounds ([7 x i8], [7 x i8]* @rt.k.obj, i32 0, i32 0)
  %n7 = select i1 %e7, i8* getelementptr inbounds ([4 x i8], [4 x i8]* @rt.k.set, i32 0, i32 0), i8* %n8
  %n6 = select i1 %e6, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @rt.k.dict, i32 0, i32 0), i8* %n7
  %n5 = select i1 %e5, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @rt.k.list, i32 0, i32 0), i8* %n6
  %n4 = select i1 %e4, i8* getelementptr inbounds ([4 x i8], [4 x i8]* @rt.k.str, i32 0, i32 0), i8* %n5
  %n3 = select i1 %e3, i8* getelementptr inbounds ([9 x i8], [9 x i8]* @rt.k.none, i32 0, i32 0), i8* %n4
  %n2 = select i1 %e2, i8* getelementptr inbounds ([5 x i8], [5 x i8]* @rt.k.bool, i32 0, i32 0), i8* %n3
  %n1 = select i1 %e1, i8* getelementptr inbounds ([6 x i8], [6 x i8]* @rt.k.float, i32 0, i32 0), i8* %n2
  %res = select i1 %e0, i8* getelementptr inbounds ([4 x i8], [4 x i8]* @rt.k.int, i32 0, i32 0), i8* %n1
  ret i8* %res
}

; rt_num_bad writes the sentence CPython writes — 'unsupported operand type(s) for +: 'int' and 'str''
; for a binary operator, 'bad operand type for unary -: 'str'' for the negation, which is a different
; sentence in the reference and not a stylistic variation of the first. snprintf is the one this module
; already declares; the extra arguments the unary format does not use are ignored by it, which is the
; only reason one call serves both.
define internal void @rt_num_bad(i32 %op, i32 %lt, i32 %rt) {
entry:
  ; Two calls, not one, because the two sentences ask for different things: the binary one wants the
  ; operator symbol and both kind names, the negation wants the kind name and has no operator to
  ; interpolate. One variadic call serving both printed a bare minus where CPython prints the operand's
  ; kind — measured the day this shipped, and the reason the two formats are not shared.
  %isneg = icmp eq i32 %op, 3
  %isabs = icmp eq i32 %op, 6
  %signless = or i1 %isneg, %isabs
  %buf = getelementptr [192 x i8], [192 x i8]* @rt.num.msg, i32 0, i32 0
  br i1 %signless, label %neg, label %bin
neg:
  ; Two signless sentences, chosen by which call the operand came from: abs writes
  ; "bad operand type for abs(): 'str'" and the unary minus writes "bad operand type for unary -: 'str'".
  ; Sharing one format is how ADR 0271's rule got broken the first time -- abs of a text naming the
  ; minus in a program that never wrote one.
  %isneg2 = icmp eq i32 %op, 3
  %fmt = select i1 %isneg2, i8* getelementptr inbounds ([46 x i8], [46 x i8]* @rt.num.negfmt, i32 0, i32 0), i8* getelementptr inbounds ([44 x i8], [44 x i8]* @rt.num.absfmt, i32 0, i32 0)
  %ln0 = call i8* @rt_kind_name(i32 %lt)
  %w0 = call i32 (i8*, ...) @snprintf(i8* %buf, i32 192, i8* %fmt, i8* %ln0)
  ret void
bin:
  %o0 = icmp eq i32 %op, 0
  ; The plus operator over a text, a list or a tuple is answered by the reference with a DIFFERENT sentence
  ; from every other operator and every other kind: the concatenation method's own complaint, which names
  ; the left operand's kind twice and the right operand's once, in double quotes rather than single. Handing
  ; it to the generic format answered unsupported operand type(s) for +: 'str' and 'int' at the exit code of
  ; the trap for a program whose own except-TYPE-ERROR prints the sentence (ADR 0271's rule — one trap, one
  ; wording — and ADR 0166's that the sentence is the program's, not the compiler's).
  %cs = icmp eq i32 %lt, 4
  %cl = icmp eq i32 %lt, 5
  %ct = icmp eq i32 %lt, 8
  %co1 = or i1 %cs, %cl
  %co = or i1 %co1, %ct
  %isconcat = and i1 %o0, %co
  br i1 %isconcat, label %concat, label %generic
concat:
  %ln2 = call i8* @rt_kind_name(i32 %lt)
  %rn2 = call i8* @rt_kind_name(i32 %rt)
  %w2 = call i32 (i8*, ...) @snprintf(i8* %buf, i32 192, i8* getelementptr inbounds ([52 x i8], [52 x i8]* @rt.num.catfmt, i32 0, i32 0), i8* %ln2, i8* %rn2, i8* %ln2)
  ret void
generic:
  %o1 = icmp eq i32 %op, 1
  %o2 = icmp eq i32 %op, 2
  %o4 = icmp eq i32 %op, 4
  %o5 = icmp eq i32 %op, 5
  ; One select chain, ordered innermost-first: the last select is the first test. Nested this way, each
  ; operator symbol is named by its own comparison rather than by which pair the previous two selects
  ; happened to share, which is how the flooring operators get named at all (ADR 0278).
  %s1 = select i1 %o5, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @rt.o.mod, i32 0, i32 0), i8* getelementptr inbounds ([3 x i8], [3 x i8]* @rt.o.fdiv, i32 0, i32 0)
  %s2 = select i1 %o4, i8* getelementptr inbounds ([3 x i8], [3 x i8]* @rt.o.fdiv, i32 0, i32 0), i8* %s1
  %s3 = select i1 %o2, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @rt.o.mul, i32 0, i32 0), i8* %s2
  %s4 = select i1 %o1, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @rt.o.sub, i32 0, i32 0), i8* %s3
  %sym = select i1 %o0, i8* getelementptr inbounds ([2 x i8], [2 x i8]* @rt.o.add, i32 0, i32 0), i8* %s4
  %ln1 = call i8* @rt_kind_name(i32 %lt)
  %rn1 = call i8* @rt_kind_name(i32 %rt)
  %w1 = call i32 (i8*, ...) @snprintf(i8* %buf, i32 192, i8* getelementptr inbounds ([61 x i8], [61 x i8]* @rt.num.fmt, i32 0, i32 0), i8* %sym, i8* %ln1, i8* %rn1)
  ret void
}

; rt_lift_num is the one place a tagged slot becomes a double: a float slot's payload is a handle on an
; @float_box and is unboxed, an int's *is* the number and is widened (ADR 0243's payload+widen step),
; and a bool is a number to Python, so it widens too — the same rule rt_payload_eq follows for '=='.
define internal double @rt_lift_num(i32 %p, i32 %t) {
entry:
  %isf = icmp eq i32 %t, 1
  br i1 %isf, label %box, label %wide
box:
  %d = call double @rt_float_of(i32 %p)
  br label %lifted
wide:
  %w = sitofp i32 %p to double
  br label %lifted
lifted:
  %v = phi double [ %d, %box ], [ %w, %wide ]
  ret double %v
}

; rt_pair_truth answers the question and/or have to ask of the operand they test when that operand's
; kind is the run time's fact and not the compiler's: is the value the pair describes true or false. The tag
; is the module's own vocabulary (value.go's ValueTag) and the answer is the record's truthy for the
; same object — a number in either family is true when it is not zero (so a float slot is unboxed by
; rt_lift_num rather than read as a handle, which is what made a float box always true), an empty text or an
; empty container is false, None is false, and any other heap object is true. It is the door that lets the
; operand the test did not choose stay out of the program entirely (roadmap Gap R.149, ADR 0275).
define internal i32 @rt_pair_truth(i32 %p, i32 %t) {
entry:
  %isnum = icmp ult i32 %t, 3
  br i1 %isnum, label %num, label %rest
num:
  %d = call double @rt_lift_num(i32 %p, i32 %t)
  %nz = fcmp one double %d, 0.0
  %rn = zext i1 %nz to i32
  ret i32 %rn
rest:
  %isnone = icmp eq i32 %t, 3
  br i1 %isnone, label %zero, label %checktext
checktext:
  %istext = icmp eq i32 %t, 4
  br i1 %istext, label %textlen, label %checkcont
checkcont:
  ; 5, 6, 7 are list, dict and set, and the heap record's length word is the entry count for all three,
  ; so one read answers emptiness for each. Anything above is an object, and an object is true.
  %iscont = icmp ult i32 %t, 8
  br i1 %iscont, label %contlen, label %one
textlen:
  %n = call i32 @rt_str_len(i32 %p)
  %rt = icmp ne i32 %n, 0
  %rti = zext i1 %rt to i32
  ret i32 %rti
contlen:
  %l = call i32 @rt_heap_len(i32 %p)
  %rc = icmp ne i32 %l, 0
  %rci = zext i1 %rc to i32
  ret i32 %rci
one:
  ret i32 1
zero:
  ret i32 0
}

; rt_num_arith answers the pair question. 0 means "the payload and the tag are written"; 1 means "the
; operands do not add" and leaves CPython's sentence in *outmsg; 2 means the int answer would not fit
; this backend's int word. The caller — not this helper — turns a status into a raise, because only the
; emitted code knows whether a handler is open.
define internal i32 @rt_num_arith(i32 %op, i32 %ap, i32 %at, i32 %bp, i32 %bt, i32* %outp, i32* %outt, i8** %outmsg) {
entry:
  %a0 = icmp eq i32 %at, 0
  %a1 = icmp eq i32 %at, 1
  %a2 = icmp eq i32 %at, 2
  %a01 = or i1 %a0, %a1
  %aisnum = or i1 %a01, %a2
  %b0 = icmp eq i32 %bt, 0
  %b1 = icmp eq i32 %bt, 1
  %b2 = icmp eq i32 %bt, 2
  %b01 = or i1 %b0, %b1
  %bisnum = or i1 %b01, %b2
  %both = and i1 %aisnum, %bisnum
  br i1 %both, label %calc, label %bad
bad:
  call void @rt_num_bad(i32 %op, i32 %at, i32 %bt)
  %m = getelementptr [192 x i8], [192 x i8]* @rt.num.msg, i32 0, i32 0
  store i8* %m, i8** %outmsg
  ret i32 1
calc:
  %af = call double @rt_lift_num(i32 %ap, i32 %at)
  %bf = call double @rt_lift_num(i32 %bp, i32 %bt)
  %sum = fadd double %af, %bf
  %dif = fsub double %af, %bf
  %prd = fmul double %af, %bf
  %neg = fneg double %af
  %aiint = or i1 %a0, %a2
  %biint = or i1 %b0, %b2
  %bothint = and i1 %aiint, %biint
  %o0 = icmp eq i32 %op, 0
  %o1 = icmp eq i32 %op, 1
  %o2 = icmp eq i32 %op, 2
  %o3 = icmp eq i32 %op, 3
  %o4 = icmp eq i32 %op, 4
  %o5 = icmp eq i32 %op, 5
  %o6 = icmp eq i32 %op, 6
  ; the two operators whose divisor may be zero, and the reference raises rather than
  ; answering. The lifted divisor is the honest test — an int 0 and a float 0.0 both lift to zero, and
  ; negative zero compares equal to it, which is what CPython raises for too.
  %isdv = or i1 %o4, %o5
  %iszero = fcmp oeq double %bf, 0.000000e+00
  %dz = and i1 %isdv, %iszero
  br i1 %dz, label %divzero, label %have
divzero:
  ; The sentence is the operands' kind's, not the source text's: floorit(0.0) and floorit(0) reach
  ; this same block with different tags, and the reference names them differently — and ' % ' names the
  ; integer case differently from '//', which is why the select is two deep rather than one.
  %mf = select i1 %o4, i8* getelementptr inbounds ([48 x i8], [48 x i8]* @rt.num.dzf, i32 0, i32 0), i8* getelementptr inbounds ([32 x i8], [32 x i8]* @rt.num.dzm, i32 0, i32 0)
  %mi = select i1 %o4, i8* getelementptr inbounds ([54 x i8], [54 x i8]* @rt.num.dzi, i32 0, i32 0), i8* getelementptr inbounds ([42 x i8], [42 x i8]* @rt.num.dzm2, i32 0, i32 0)
  %ms = select i1 %bothint, i8* %mi, i8* %mf
  store i8* %ms, i8** %outmsg
  ret i32 3
have:
  ; The flooring pair, the same arithmetic the statement-level float road already runs (ADR 0216): a
  ; floor of the quotient, and the remainder corrected from libm's truncated fmod to Python's
  ; floor-mod — an exact remainder carries the divisor's sign, and a remainder whose sign differs from
  ; the divisor's has the divisor added back. Keeping the two roads' arithmetic identical is what lets
  ; a == (a // b) * b + (a % b) hold for a pair-bound operand as it does for a literal one.
  %q = fdiv double %af, %bf
  %fl = call double @llvm.floor.f64(double %q)
  %rm0 = frem double %af, %bf
  %zs = call double @llvm.copysign.f64(double 0.000000e+00, double %bf)
  %iz = fcmp oeq double %rm0, 0.000000e+00
  %rm1 = select i1 %iz, double %zs, double %rm0
  %nz = fcmp one double %rm1, 0.000000e+00
  %rne = fcmp olt double %rm1, 0.000000e+00
  %rne_b = fcmp olt double %bf, 0.000000e+00
  %diff = xor i1 %rne, %rne_b
  %adj = and i1 %nz, %diff
  %sum2 = fadd double %rm1, %bf
  %md = select i1 %adj, double %sum2, double %rm1
  %c0 = select i1 %o0, double %sum, double %dif
  %c1 = select i1 %o1, double %dif, double %prd
  %c2 = select i1 %o2, double %prd, double %c0
  %c3 = select i1 %o3, double %neg, double %c2
  %c4 = select i1 %o4, double %fl, double %c3
  %c5 = select i1 %o5, double %md, double %c4
  ; abs is the signless call: a number out, or the reference's raise. The magnitude is the operand's
  ; bits with the sign cleared — NOT an fcmp-olt-against-zero with a select, which is false for -0.0
  ; and therefore handed -0.0 back where CPython answers 0.0. Nothing noticed while the constant's sign
  ; was being eaten upstream (Gap R.132's fadd-double-zero idiom turned the box's -0.0 into +0.0 before
  ; it was stored); with ADR 0349 stating the sign honestly, the door's own rule had to become the
  ; honest one. Clearing the bit is what llvm.fabs means, and it leaves NaN's payload alone.
  %abits = bitcast double %af to i64
  %amagbits = and i64 %abits, 9223372036854775807
  %absv = bitcast i64 %amagbits to double
  %r = select i1 %o6, double %absv, double %c5
  %hi = fcmp oge double %r, 2147483648.000000e+00
  %lo = fcmp olt double %r, -2147483648.000000e+00
  %oor = or i1 %hi, %lo
  %oob = and i1 %bothint, %oor
  br i1 %oob, label %ovf, label %fin
ovf:
  %om = getelementptr [121 x i8], [121 x i8]* @rt.num.ofmt, i32 0, i32 0
  store i8* %om, i8** %outmsg
  ret i32 2
fin:
  %ip = fptosi double %r to i32
  %fp = call i32 @rt_float_new(double %r)
  ; An int answer needs no slot, so a negative box is only a failure when the answer is the double.
  ; This is the road the guard beside a call cannot stand on: rt_num_arith hands the handle back
  ; through an out-param and its return value is a status, so there is no handle for emitHeapCall to
  ; compare. The door therefore reports exhaustion as a fourth status and the caller — which is the one
  ; place that knows the nearest handler — raises it, exactly as it already raises this door's TypeError,
  ; OverflowError and ZeroDivisionError. Branch-free, because a check on the answer that fits costs three
  ; instructions and no new block (roadmap Gap Q.2, ADR 0347).
  %isflt = icmp eq i1 %bothint, false
  %ispoison = icmp slt i32 %fp, 0
  %oom = and i1 %isflt, %ispoison
  %stat = select i1 %oom, i32 4, i32 0
  %pl = select i1 %bothint, i32 %ip, i32 %fp
  store i32 %pl, i32* %outp
  %tg = select i1 %bothint, i32 0, i32 1
  store i32 %tg, i32* %outt
  ret i32 %stat
}

`
