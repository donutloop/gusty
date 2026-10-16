package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// The run-time half of printf-style formatting (roadmap Gap R.31, L11.1, ADR 0360).
//
// pkg/lang/percent_format.go decides, at compile time, that a % on a text is a formatting and not
// a remainder, and lays the arguments out in a table. This file is what runs: one rt_format walker
// over the format text, three renderers (a text, an integer, a real number), and the error register
// the compiler raises from.
//
// It is textual LLVM IR for the same reason the rest of the runtime is (ADR 0173), and for the reason
// ADR 0359 measured: a walker written in Go would answer from what the compiler believed about an
// argument, while the door has to answer from the (payload, tag) pair the slot holds. So the renderers
// ask the doors this module already has — rt_str_of_value for anything a %s/%r/%a renders
// (ADR 0258's one printer), rt_lift_num for the double a %f needs, rt_kind_name for the word a
// wrong-kind TypeError prints, rt_dict_get_tagged/rt_dict_value_tag for a mapping key — and a
// %s, a print, a str() and "%s" % x are one answer rather than four that can drift.
//
// The IR spells no tag number and no exception class code. percentRuntimeBlock substitutes both from
// value.go's ValueTag table and exceptions.go's registry, which is what keeps this block's
// vocabulary the module's (ADR 0182's one-table rule; the tags' own comment calls the numbers "the
// wire format").
//
// The answers below were read off CPython 3.12 rather than from a memory of the printf manual, and the
// places where they differ from C are the ones that matter: %#o writes 0o10, not 010; %x of a
// negative is -0x12, not two's complement; %d of a float truncates toward zero and refuses NaN and
// infinity with two different classes; %c accepts an integer up to 0x110000 exclusive and encodes it
// as UTF-8; and a format with a literal % in it is still one conversion deep.

// percentOutLimit is the rendered answer's byte limit; percentSpecLimit the width of one assembled
// conversion spec; percentKeyLimit the length of a %(name)s key. All three are this backend's, not
// the reference's: a program that would overflow one raises rather than being silently truncated,
// because a truncated answer is a wrong answer that prints (ADR 0166's rule against the near miss).
const (
	percentOutLimit   = 32768
	percentSpecLimit  = 64
	percentKeyLimit   = 256
	percentDigitLimit = 40
)

// The parse-state slots of one conversion, indexed into @rt.pct.st. They are globals rather than
// registers because the flag scan below is four loops deep and the conversion that follows it is
// reached from all four; a register would need a phi in every loop header for a value that is, by
// construction, written once per conversion.
const (
	pctStateConv = iota
	pctStateLeft
	pctStateZero
	pctStateAlt
	pctStateSign
	pctStateWidth
	pctStatePrec
	pctStateDot
	pctStateSpan
	pctStateLen
)

// percentRuntimeGlobals is the door's whole state. The argument table is the compiler's side of the
// contract (percent_format.go fills it); the rest is the door's own.
func percentRuntimeGlobals() string {
	return `; ---- printf-style formatting: the argument table, the answer, the error register ----
; @rt.pct.argp/@rt.pct.argt are the (payload, tag) pairs the compiler lowered the arguments to —
; the same two words every printer, comparison and container in this runtime reads (ADR 0232's
; slots-are-the-truth). The door takes them one at a time, in the order the conversions take them;
; * and .* take theirs too, which is where the reference takes them from.
@rt.pct.argp = private global [64 x i32] zeroinitializer
@rt.pct.argt = private global [64 x i32] zeroinitializer
@rt.pct.narg = private global i32 0
@rt.pct.ridx = private global i32 0
@rt.pct.spn = private global i32 0
@rt.pct.map = private global i32 0
@rt.pct.out = private global [32768 x i8] zeroinitializer
@rt.pct.outlen = private global i32 0
@rt.pct.err = private global i32 0
@rt.pct.msg = private global [256 x i8] zeroinitializer
@rt.pct.spec = private global [64 x i8] zeroinitializer
@rt.pct.st = private global [14 x i32] zeroinitializer
@rt.pct.num = private global [40 x i8] zeroinitializer
@rt.pct.dg = private global [40 x i8] zeroinitializer
@rt.pct.buf = private global [80 x i8] zeroinitializer
@rt.pct.key = private global [256 x i8] zeroinitializer
@rt.pct.chr = private global [5 x i8] zeroinitializer
` + percentConstIR("rt.pct.digs", "0123456789abcdef") + "\n" +
		percentConstIR("rt.pct.digU", "0123456789ABCDEF") + "\n" +
		percentConstIR("rt.pct.fmt.d", "%d") + "\n" + percentConstIR("rt.pct.fmt.ld", "%lld") + "\n"
}

// percentRuntimeMessages holds the reference's own sentences, one global per piece, so the door raises
// what CPython raises and a reader can tell the two apart by the text rather than by a position in a
// switch (ADR 0214's "catchable, with the reference's message"). The pieces are pieces because two of
// the sentences carry the kind's name, and that name is rt_kind_name's answer — no per-kind constant
// belongs here (ADR 0171's one table).
func percentRuntimeMessages() string {
	// Every message global is the same size, and the text is what CPython's own message is; the
	// literal is what pads it out with NULs. One size is what lets the thirty-odd call sites below
	// reference a sentence without any of them carrying a count that could disagree with it — the
	// class of bug a hand-counted [N x i8] invites (ADR 0171's generated constants).
	msgs := [][2]string{
		{"noargs", "not enough arguments for format string"},
		{"more", "not all arguments converted during string formatting"},
		{"incomplete", "incomplete format"},
		{"nokey", "incomplete format key"},
		{"nomap", "format requires a mapping"},
		{"listidx", "list indices must be integers or slices, not str"},
		{"nochr", "%c requires int or char"},
		{"stars", "* wants int"},
		{"real", "cannot convert float NaN to integer"},
		{"inf", "cannot convert float infinity to integer"},
		{"rngchr", "%c arg not in range(0x110000)"},
		{"toolong", "formatted text is longer than this backend can hold"},
		{"1.a", " format: a real number is required, not "},
		{"2.a", " format: an integer is required, not "},
		{"3.a", "must be real number, not "},
		{"4.a", "unsupported format character '"},
		{"4.b", "' (0x"},
		{"4.c", ") at index "},
		{"colon", ": "},
		{"key", "KeyError"},
		{"type", "TypeError"},
		{"value", "ValueError"},
		{"over", "OverflowError"},
		{"rterr", "RuntimeError"},
		{"pfx0o", "0o"},
		{"pfx0O", "0O"},
		{"pfx0x", "0x"},
		{"pfx0X", "0X"},
		{"zero", "0"},
		{"plus", "+"},
		{"minus", "-"},
		{"space", " "},
		{"quote", "'"},
		{"pct", "%"},
	}
	lines := []string{`; ---- the reference's sentences, verbatim ----`}
	for _, m := range msgs {
		lines = append(lines, fmt.Sprintf("@rt.pct.m.%s = private global [64 x i8] c%s", m[0], percentHexLiteral(m[1], 64)))
	}
	return strings.Join(lines, "\n") + "\n"
}

// percentHexLiteral renders a message as an LLVM c-string of exactly width bytes, every byte written
// as its own \XX escape. Nothing is spelled as a character in it, so nothing in a message — a percent
// sign, a quote, a space the reader would otherwise have to count — can be misread by either parser,
// and the declared length is the number of escapes, which is checkable by eye (ADR 0171's rule that
// the module's constants are generated, not hand-counted).
func percentHexLiteral(text string, width int) string {
	s := &strings.Builder{}
	s.WriteByte('"')
	for i := 0; i < width; i++ {
		b := byte(0)
		if i < len(text) {
			b = text[i]
		}
		s.WriteString(fmt.Sprintf("\\%02x", b))
	}
	s.WriteByte('"')
	return s.String()
}

// percentRuntimeSink is the answer buffer, the message buffer and their writers. Everything any
// conversion renders goes through rt_pct_puts, which is what makes the byte limit one question
// rather than twelve; everything any raise says goes through rt_pct_mput, which is what lets a
// message be assembled from the reference's pieces and the kind's own name.
func percentRuntimeSink() string {
	return `
@rt.pct.msglen = private global i32 0
; The seal on the message being written: rt_pct_msg_begin sets it when this raise is the first of the walk,
; and clears it when one already happened. Every piece of a message is written through rt_pct_mput, which
; writes nothing while the seal is down — so a walk that raises twice reports the first raise, the way the
; reference's unwinding does (roadmap Gap R.31).
@rt.pct.sealed = private global i32 0

define internal i32 @rt_pct_slen(i8* %p) {
entry:
  br label %loop
loop:
  %n = phi i32 [ 0, %entry ], [ %nx, %step ]
  %at = getelementptr i8, i8* %p, i32 %n
  %b = load i8, i8* %at
  %end = icmp eq i8 %b, 0
  br i1 %end, label %fin, label %step
step:
  %nx = add i32 %n, 1
  br label %loop
fin:
  ret i32 %n
}

; rt_pct_append is the one writer: %n bytes of %p onto the end of the answer, answering how many it
; wrote and -1 when they would not fit. A negative %n means "up to the NUL".
define internal i32 @rt_pct_append(i8* %p, i32 %n) {
entry:
  %all = icmp slt i32 %n, 0
  br i1 %all, label %cnt, label %have
cnt:
  %k = call i32 @rt_pct_slen(i8* %p)
  br label %chk
have:
  %k2 = add i32 %n, 0
  br label %chk
chk:
  %kv = phi i32 [ %k, %cnt ], [ %k2, %have ]
  %o = load i32, i32* @rt.pct.outlen
  %need = add i32 %o, %kv
  %fits = icmp slt i32 %need, @OUTLIMIT@
  br i1 %fits, label %copy, label %over
copy:
  %dst = getelementptr [32768 x i8], [32768 x i8]* @rt.pct.out, i32 0, i32 %o
  br label %cl
cl:
  %ci = phi i32 [ 0, %copy ], [ %cn, %cbody ]
  %cc = icmp slt i32 %ci, %kv
  br i1 %cc, label %cbody, label %cdone
cbody:
  %sa = getelementptr i8, i8* %p, i32 %ci
  %sb = load i8, i8* %sa
  %da = getelementptr i8, i8* %dst, i32 %ci
  store i8 %sb, i8* %da
  %cn = add i32 %ci, 1
  br label %cl
cdone:
  store i32 %need, i32* @rt.pct.outlen
  ret i32 %kv
over:
  ret i32 -1
}

define internal void @rt_pct_put(i8* %p) {
entry:
  %n = call i32 @rt_pct_append(i8* %p, i32 -1)
  %bad = icmp slt i32 %n, 0
  br i1 %bad, label %of, label %ret
of:
  call void @rt_pct_toolong()
  br label %ret
ret:
  ret void
}

define internal void @rt_pct_putn(i8* %p, i32 %n) {
entry:
  %neg = icmp slt i32 %n, 0
  br i1 %neg, label %ret, label %go
go:
  %w = call i32 @rt_pct_append(i8* %p, i32 %n)
  %full = icmp slt i32 %w, 0
  br i1 %full, label %of, label %ret
of:
  call void @rt_pct_toolong()
  br label %ret
ret:
  ret void
}

; rt_pct_pad appends %n spaces. The two renderers that let libc do its own padding (the real-number
; conversions, whose width travels inside the spec) never call it.
; rt_pct_padz appends %n zeros. Precision on an integer is written in zeros ("%5.3d" % 42 is '  042')
; and so is the zero flag, while field fill is spaces — two fillers, one loop body each, because a
; program that asked for three digits and got three spaces is the kind of wrong answer that ships.
define internal void @rt_pct_padz(i32 %n) {
entry:
  %go = icmp sgt i32 %n, 0
  br i1 %go, label %loop, label %ret
loop:
  %i = phi i32 [ 0, %entry ], [ %in, %write ]
  %c = icmp slt i32 %i, %n
  br i1 %c, label %body, label %fin
body:
  %o = load i32, i32* @rt.pct.outlen
  %room = sub i32 @OUTLIMIT@, %o
  %fits = icmp sgt i32 %room, 1
  br i1 %fits, label %write, label %over
write:
  %at = getelementptr [32768 x i8], [32768 x i8]* @rt.pct.out, i32 0, i32 %o
  store i8 48, i8* %at
  %nl = add i32 %o, 1
  store i32 %nl, i32* @rt.pct.outlen
  %in = add i32 %i, 1
  br label %loop
over:
  call void @rt_pct_toolong()
  br label %fin
fin:
  ret void
ret:
  ret void
}

define internal void @rt_pct_pad(i32 %n) {
entry:
  %go = icmp sgt i32 %n, 0
  br i1 %go, label %loop, label %ret
loop:
  %i = phi i32 [ 0, %entry ], [ %in, %write ]
  %c = icmp slt i32 %i, %n
  br i1 %c, label %body, label %fin
body:
  %o = load i32, i32* @rt.pct.outlen
  %room = sub i32 @OUTLIMIT@, %o
  %fits = icmp sgt i32 %room, 1
  br i1 %fits, label %write, label %over
write:
  %at = getelementptr [32768 x i8], [32768 x i8]* @rt.pct.out, i32 0, i32 %o
  store i8 32, i8* %at
  %nl = add i32 %o, 1
  store i32 %nl, i32* @rt.pct.outlen
  %in = add i32 %i, 1
  br label %loop
over:
  call void @rt_pct_toolong()
  br label %fin
fin:
  ret void
ret:
  ret void
}

define internal void @rt_pct_toolong() {
entry:
  call void @rt_pct_msg_begin(i32 @CODE_RUNTIMEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.rterr, i32 0, i32 0), i32 1)
  call void @rt_pct_mput(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.toolong, i32 0, i32 0))
  ret void
}

; rt_pct_nchars is the display width of the format segment [%b,%e): its byte count less the continuation
; bytes of whatever UTF-8 the program typed. Padding counts characters, the way the printer does.
define internal i32 @rt_pct_nchars(i8* %p, i32 %b, i32 %e) {
entry:
  br label %loop
loop:
  %n = phi i32 [ 0, %entry ], [ %nn, %body ]
  %i = phi i32 [ %b, %entry ], [ %in, %body ]
  %more = icmp slt i32 %i, %e
  br i1 %more, label %body, label %fin
body:
  %at = getelementptr i8, i8* %p, i32 %i
  %c = load i8, i8* %at
  %m = and i8 %c, -64
  %cont = icmp eq i8 %m, -128
  %isz = zext i1 %cont to i32
  %st = sub i32 1, %isz
  %nn = add i32 %n, %st
  %in = add i32 %i, 1
  br label %loop
fin:
  ret i32 %n
}

; ---- the message buffer: a class, then pieces, then nothing else ----

define internal void @rt_pct_msg_begin(i32 %code, i8* %cls, i32 %withcls) {
entry:
  ; The first raise of a walk is the one the program sees: the reference raises, unwinds, and never
  ; reaches the complaint that would have come after it. The message the door writes is assembled by
  ; many calls to rt_pct_mput, so the decision is not made here alone — it is recorded in @rt.pct.sealed
  ; and every writer obeys it. A missing key followed by an incomplete conversion is the case this
  ; guards: the KeyError is the answer, and the sentence that came second must not follow it (Gap R.31).
  %e = load i32, i32* @rt.pct.err
  %already = icmp ne i32 %e, 0
  br i1 %already, label %sealed, label %write
write:
  store i32 %code, i32* @rt.pct.err
  store i32 0, i32* @rt.pct.msglen
  store i32 1, i32* @rt.pct.sealed
  %wc = icmp ne i32 %withcls, 0
  br i1 %wc, label %putcls, label %ret
putcls:
  call void @rt_pct_mput(i8* %cls)
  call void @rt_pct_mput(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.colon, i32 0, i32 0))
  br label %ret
sealed:
  store i32 0, i32* @rt.pct.sealed
  ret void
ret:
  ret void
}

; rt_pct_needmapping is the answer to a keyed conversion whose argument is not something you can look a
; name up in. The reference asks that question two different ways depending on the argument: a list does
; answer the subscript operator, so the complaint comes from inside the subscript and names the key; a
; number, a text or nothing at all does not answer it at all, and the complaint is the mapping's own.
; The door asks which one the argument is, through the one kind name the runtime keeps.
define internal void @rt_pct_needmapping(i32 %h, i32 %t) {
entry:
  %islist = icmp eq i32 %t, @TAG_LIST@
  br i1 %islist, label %subscript, label %mapping
subscript:
  call void @rt_pct_msg(i32 @CODE_TYPEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.type, i32 0, i32 0), i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.listidx, i32 0, i32 0))
  ret void
mapping:
  call void @rt_pct_msg(i32 @CODE_TYPEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.type, i32 0, i32 0), i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.nomap, i32 0, i32 0))
  ret void
}

define internal void @rt_pct_mput(i8* %p) {
entry:
  %w = load i32, i32* @rt.pct.sealed
  %mine = icmp ne i32 %w, 0
  br i1 %mine, label %go, label %ret
go:
  %n = call i32 @rt_pct_slen(i8* %p)
  br label %loop
loop:
  %i = phi i32 [ 0, %go ], [ %in, %write ]
  %c = icmp slt i32 %i, %n
  br i1 %c, label %body, label %fin
body:
  %o = load i32, i32* @rt.pct.msglen
  %room = sub i32 250, %o
  %fits = icmp sgt i32 %room, 1
  br i1 %fits, label %write, label %fin
write:
  %sa = getelementptr i8, i8* %p, i32 %i
  %sb = load i8, i8* %sa
  %da = getelementptr [256 x i8], [256 x i8]* @rt.pct.msg, i32 0, i32 %o
  store i8 %sb, i8* %da
  %nl = add i32 %o, 1
  store i32 %nl, i32* @rt.pct.msglen
  %in = add i32 %i, 1
  br label %loop
fin:
  br label %ret
ret:
  ret void
}

define internal void @rt_pct_mputc(i32 %c) {
entry:
  %at = getelementptr [5 x i8], [5 x i8]* @rt.pct.chr, i32 0, i32 0
  %b = trunc i32 %c to i8
  store i8 %b, i8* %at
  %e = getelementptr [5 x i8], [5 x i8]* @rt.pct.chr, i32 0, i32 1
  store i8 0, i8* %e
  %p = getelementptr [5 x i8], [5 x i8]* @rt.pct.chr, i32 0, i32 0
  call void @rt_pct_mput(i8* %p)
  ret void
}

; rt_pct_mputint and rt_pct_mputhex print the two numbers a message carries — the index of an
; unsupported conversion character and its code point — through the one snprintf this module declares
; (ADR 0171's one formatted-printing door).
define internal void @rt_pct_mputint(i32 %v) {
entry:
  %buf = getelementptr [80 x i8], [80 x i8]* @rt.pct.buf, i32 0, i32 0
  %fmt = getelementptr [5 x i8], [5 x i8]* @rt.pct.fmt.ld, i32 0, i32 0
  %w = sext i32 %v to i64
  %r = call i32 @snprintf(i8* %buf, i32 80, i8* %fmt, i64 %w)
  %bad = icmp slt i32 %r, 0
  br i1 %bad, label %ret, label %go
go:
  call void @rt_pct_mput(i8* %buf)
  br label %ret
ret:
  ret void
}

define internal void @rt_pct_mputhex(i32 %v) {
entry:
  %dg = getelementptr [40 x i8], [40 x i8]* @rt.pct.dg, i32 0, i32 0
  call i32 @rt_pct_digits(i32 %v, i32 16, i32 0, i8* %dg)
  call void @rt_pct_mput(i8* %dg)
  ret void
}

; rt_pct_msg_ptr hands the compiler the address of the sentence the door wrote, which is the half of a
; raise only the run time knows. The register is module-wide and the raise below it copies it, so a
; program that raises twice in a row gets each sentence, not the last one twice.
define internal i8* @rt_pct_msg_ptr() {
entry:
  %p = getelementptr [256 x i8], [256 x i8]* @rt.pct.msg, i32 0, i32 0
  ret i8* %p
}

define internal void @rt_pct_mputkind(i32 %t) {
entry:
  %nm = call i8* @rt_kind_name(i32 %t)
  call void @rt_pct_mput(i8* %nm)
  ret void
}

; rt_pct_digits writes the base-%r digits of the unsigned %v into %dst, most significant first, and
; answers how many it wrote. Two tables, spelled once: radix 16 asks for the lowercase one, which is
; the only thing that distinguishes %x from %X (the reference distinguishes %#x's 0x from %#X's 0X too,
; and the caller picks that prefix).
define internal i32 @rt_pct_digits(i32 %v, i32 %r, i32 %up, i8* %dst) {
entry:
  %low = getelementptr [17 x i8], [17 x i8]* @rt.pct.digs, i32 0, i32 0
  %upp = getelementptr [17 x i8], [17 x i8]* @rt.pct.digU, i32 0, i32 0
  %is16 = icmp eq i32 %r, 16
  %upb = icmp ne i32 %up, 0
  %isu = and i1 %is16, %upb
  %tab = select i1 %isu, i8* %upp, i8* %low
  %tmp = getelementptr [40 x i8], [40 x i8]* @rt.pct.num, i32 0, i32 0
  br label %gen
gen:
  %i = phi i32 [ 0, %entry ], [ %in, %gen ]
  %v2 = phi i32 [ %v, %entry ], [ %vn, %gen ]
  %md = urem i32 %v2, %r
  %pa = getelementptr i8, i8* %tab, i32 %md
  %ch = load i8, i8* %pa
  %sa = getelementptr i8, i8* %tmp, i32 %i
  store i8 %ch, i8* %sa
  %vn = udiv i32 %v2, %r
  %in = add i32 %i, 1
  %more = icmp ne i32 %vn, 0
  %ok = icmp slt i32 %in, @DIGITLIMIT@
  %go = and i1 %more, %ok
  br i1 %go, label %gen, label %rev
rev:
  ; %in is the count the loop stopped on — %i is the index it wrote at, one behind. Reading the wrong
  ; one of the two is the classic off-by-one here, and it shows as the most significant digit missing:
  ; 1234 came out 234, 8 came out 0, and 0 came out nothing at all.
  %j = phi i32 [ 0, %gen ], [ %jn, %rbody ]
  %cm = icmp slt i32 %j, %in
  br i1 %cm, label %rbody, label %fin
rbody:
  %last = sub i32 %in, 1
  %si = sub i32 %last, %j
  %pa2 = getelementptr i8, i8* %tmp, i32 %si
  %c2 = load i8, i8* %pa2
  %da = getelementptr i8, i8* %dst, i32 %j
  store i8 %c2, i8* %da
  %jn = add i32 %j, 1
  br label %rev
fin:
  %end = getelementptr i8, i8* %dst, i32 %in
  store i8 0, i8* %end
  ret i32 %in
}
`
}

// percentRuntimeRenderers holds the three renderers: a text (%s, %r, %a), an integer (%d,
// %i, %o, %x, %X, %c) and a real number (%f, %F, %e, %E, %g, %G).
//
// They are three because the reference's answer is three answers: a text is padded to a character
// width and truncated by characters ("%10s|" % "é" pads to ten CHARACTERS, which is why the count
// comes from rt_str_nchars and the slice from rt_str_byteoff, not from strlen); an integer is
// built from digits the module owns (%x of a negative is -0x12, which no C library will print from
// an unsigned argument, and %#o writes 0o10, which no C library writes at all); and a real number
// is left to the C library's %f/%e/%g — with its width, precision and flags still inside the spec,
// because that is the only way "%010.2f" puts the zeros between the sign and the digits
// (-000003.50) and the same rule keeps "%11.2e" right-aligned.
//
// None of the three asks what its argument IS: the walker read the tag, checked the kind against the
// conversion, and hands a renderer a value the conversion can render. That is the division the door
// keeps — the tag is read once, in one place, by the same code that raises the wrong-kind TypeError.
func percentRuntimeRenderers() string {
	return `
define internal void @rt_pct_render_text(i32 %p, i32 %t, i32 %q, i32 %w, i32 %pr, i32 %dot, i32 %left) {
entry:
  %vi = call i32 @rt_str_of_value(i32 %p, i32 %t, i32 %q)
  %bp = call i8* @rt_str_ptr(i32 %vi)
  %tc = call i32 @rt_str_nchars(i32 %vi)
  %isp = icmp ne i32 %dot, 0
  %short = icmp slt i32 %pr, %tc
  %cut = and i1 %isp, %short
  %cnt = select i1 %cut, i32 %pr, i32 %tc
  %bl = call i32 @rt_str_byteoff(i32 %vi, i32 %cnt)
  %gap = sub i32 %w, %cnt
  %negf = icmp slt i32 %gap, 0
  %fill = select i1 %negf, i32 0, i32 %gap
  %lt = icmp ne i32 %left, 0
  br i1 %lt, label %lft, label %rgt
lft:
  call void @rt_pct_putn(i8* %bp, i32 %bl)
  call void @rt_pct_pad(i32 %fill)
  br label %fin
rgt:
  call void @rt_pct_pad(i32 %fill)
  call void @rt_pct_putn(i8* %bp, i32 %bl)
  br label %fin
fin:
  ret void
}

; rt_pct_render_int prints an integer in base %r. The digits are the module's own (rt_pct_digits), the
; prefix is the reference's — 0o/0O for %#o, 0x/0X for %#x except when the value is zero, which
; the reference prints as plain 0 — and the fill is placed by the three rules the reference follows:
; left-aligned puts the fill last, zero-filled keeps the sign and the prefix adjacent and pushes the
; zeros in behind them, and anything else right-aligns with spaces around the whole body.
define internal void @rt_pct_render_int(i32 %v, i32 %r, i32 %up, i32 %alt, i32 %w, i32 %pr, i32 %dot, i32 %left, i32 %zero, i32 %sgn) {
entry:
  %dg = getelementptr [40 x i8], [40 x i8]* @rt.pct.dg, i32 0, i32 0
  %neg = icmp slt i32 %v, 0
  %negw = sub i32 0, %v
  %abs = select i1 %neg, i32 %negw, i32 %v
  %dn = call i32 @rt_pct_digits(i32 %abs, i32 %r, i32 %up, i8* %dg)
  %pg = sub i32 %pr, %dn
  %pgn = icmp slt i32 %pg, 0
  %padn = select i1 %pgn, i32 0, i32 %pg
  %alta = icmp ne i32 %alt, 0
  %one = icmp eq i32 %dn, 1
  %z0 = getelementptr [40 x i8], [40 x i8]* @rt.pct.dg, i32 0, i32 0
  %zb = load i8, i8* %z0
  %is0 = icmp eq i8 %zb, 48
  %iszero = and i1 %one, %is0
  %is8 = icmp eq i32 %r, 8
  %a8 = and i1 %alta, %is8
  %is16 = icmp eq i32 %r, 16
  %a16 = and i1 %alta, %is16
  %hx = and i1 %a16, %iszero
  %pre = select i1 %a8, i32 2, i32 0
  %pre2 = select i1 %hx, i32 0, i32 2
  %pren = select i1 %a16, i32 %pre2, i32 %pre
  %sg = select i1 %neg, i32 1, i32 0
  %pos = xor i1 %neg, true
  %p1 = icmp eq i32 %sgn, 1
  %p2 = icmp eq i32 %sgn, 2
  %sgp = and i1 %pos, %p1
  %sgs = and i1 %pos, %p2
  %sgb = or i1 %sgp, %sgs
  %sgn1 = zext i1 %sgb to i32
  %sglen = add i32 %sg, %sgn1
  %body0 = add i32 %dn, %padn
  %body1 = add i32 %body0, %pren
  %body = add i32 %body1, %sglen
  %gap = sub i32 %w, %body
  %gn = icmp slt i32 %gap, 0
  %fill = select i1 %gn, i32 0, i32 %gap
  %lt = icmp ne i32 %left, 0
  %zt = icmp ne i32 %zero, 0
  %nof = icmp sgt i32 %fill, 0
  %zfill = and i1 %zt, %nof
  br i1 %lt, label %lft, label %rgt
lft:
  call void @rt_pct_sign(i32 %sgn, i1 %neg)
  call void @rt_pct_prefix(i32 %r, i32 %alt, i32 %up)
  call void @rt_pct_padz(i32 %padn)
  call void @rt_pct_digs(i8* %dg, i32 %dn)
  call void @rt_pct_pad(i32 %fill)
  br label %fin
rgt:
  br i1 %zfill, label %zf, label %sf
zf:
  call void @rt_pct_sign(i32 %sgn, i1 %neg)
  call void @rt_pct_prefix(i32 %r, i32 %alt, i32 %up)
  %all = add i32 %fill, %padn
  call void @rt_pct_padz(i32 %all)
  call void @rt_pct_digs(i8* %dg, i32 %dn)
  br label %fin
sf:
  call void @rt_pct_pad(i32 %fill)
  call void @rt_pct_sign(i32 %sgn, i1 %neg)
  call void @rt_pct_prefix(i32 %r, i32 %alt, i32 %up)
  call void @rt_pct_padz(i32 %padn)
  call void @rt_pct_digs(i8* %dg, i32 %dn)
  br label %fin
fin:
  ret void
}

define internal void @rt_pct_digs(i8* %p, i32 %n) {
entry:
  %c = icmp sgt i32 %n, 0
  br i1 %c, label %go, label %ret
go:
  call void @rt_pct_putn(i8* %p, i32 %n)
  br label %ret
ret:
  ret void
}

define internal void @rt_pct_sign(i32 %sgn, i1 %neg) {
entry:
  br i1 %neg, label %minus, label %pos
minus:
  call void @rt_pct_put(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.minus, i32 0, i32 0))
  ret void
pos:
  %isp = icmp eq i32 %sgn, 1
  br i1 %isp, label %plus, label %sp
plus:
  call void @rt_pct_put(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.plus, i32 0, i32 0))
  ret void
sp:
  %iss = icmp eq i32 %sgn, 2
  br i1 %iss, label %put, label %ret
put:
  call void @rt_pct_put(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.space, i32 0, i32 0))
  br label %ret
ret:
  ret void
}

define internal void @rt_pct_prefix(i32 %r, i32 %alt, i32 %up) {
entry:
  %a = icmp ne i32 %alt, 0
  %is8 = icmp eq i32 %r, 8
  %o = and i1 %a, %is8
  br i1 %o, label %oct, label %hex
oct:
  %u8 = icmp ne i32 %up, 0
  br i1 %u8, label %oO, label %oo
oO:
  call void @rt_pct_put(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.pfx0O, i32 0, i32 0))
  ret void
oo:
  call void @rt_pct_put(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.pfx0o, i32 0, i32 0))
  ret void
hex:
  %is16 = icmp eq i32 %r, 16
  %h = and i1 %a, %is16
  br i1 %h, label %hx, label %ret
hx:
  %u16 = icmp ne i32 %up, 0
  br i1 %u16, label %oX, label %ox
oX:
  call void @rt_pct_put(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.pfx0X, i32 0, i32 0))
  ret void
ox:
  call void @rt_pct_put(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.pfx0x, i32 0, i32 0))
  ret void
ret:
  ret void
}

; rt_pct_render_float leaves the whole conversion to the C library, spec and all: the width, the
; precision and the flags are already in @rt.pct.spec, and this only appends what the program did not
; write itself (a * width or precision, which the walker read out of the argument list) and the
; conversion character. That is what keeps "%010.2f" % -3.5 at -000003.50 — the zeros belong
; between the sign and the digits, and only the library puts them there.
define internal void @rt_pct_render_float(i32 %p, i32 %t, i32 %w, i32 %pr, i32 %dot, i32 %conv) {
entry:
  %d = call double @rt_lift_num(i32 %p, i32 %t)
  %sp = getelementptr [64 x i8], [64 x i8]* @rt.pct.spec, i32 0, i32 0
  %putw = icmp sgt i32 %w, 0
  br i1 %putw, label %wv, label %now
wv:
  call void @rt_pct_specnum(i8* %sp, i32* @rt.pct.spn, i32 %w)
  br label %now
now:
  %hasp = icmp ne i32 %dot, 0
  br i1 %hasp, label %putdot, label %noc
putdot:
  call void @rt_pct_specputc(i8* %sp, i32* @rt.pct.spn, i32 46)
  call void @rt_pct_specnum(i8* %sp, i32* @rt.pct.spn, i32 %pr)
  br label %noc
noc:
  call void @rt_pct_specputc(i8* %sp, i32* @rt.pct.spn, i32 %conv)
  %buf = getelementptr [80 x i8], [80 x i8]* @rt.pct.buf, i32 0, i32 0
  %r = call i32 @snprintf(i8* %buf, i32 80, i8* %sp, double %d)
  %bad = icmp slt i32 %r, 0
  br i1 %bad, label %ret, label %emit
emit:
  %cap = icmp sgt i32 %r, 79
  %cnt = select i1 %cap, i32 79, i32 %r
  call void @rt_pct_putn(i8* %buf, i32 %cnt)
  br label %ret
ret:
  ret void
}

; The spec is assembled in place at the running end recorded in the caller's slot, so a conversion that
; reuses it never has to clear it: every write is at the slot's offset and every read stops at the NUL
; the last write left.
define internal void @rt_pct_specputc(i8* %sp, i32* %sn, i32 %c) {
entry:
  %n = load i32, i32* %sn
  %at = getelementptr i8, i8* %sp, i32 %n
  %b = trunc i32 %c to i8
  store i8 %b, i8* %at
  %n2 = add i32 %n, 1
  %e = getelementptr i8, i8* %sp, i32 %n2
  store i8 0, i8* %e
  store i32 %n2, i32* %sn
  ret void
}

define internal void @rt_pct_specnum(i8* %sp, i32* %sn, i32 %v) {
entry:
  %nb = getelementptr [80 x i8], [80 x i8]* @rt.pct.buf, i32 0, i32 0
  %fmt = getelementptr [5 x i8], [5 x i8]* @rt.pct.fmt.ld, i32 0, i32 0
  %x = sext i32 %v to i64
  %r = call i32 @snprintf(i8* %nb, i32 80, i8* %fmt, i64 %x)
  %bad = icmp slt i32 %r, 0
  br i1 %bad, label %ret, label %cp
cp:
  br label %loop
loop:
  %j = phi i32 [ 0, %cp ], [ %jn, %body ]
  %c = icmp slt i32 %j, %r
  br i1 %c, label %body, label %fin
body:
  %sa = getelementptr i8, i8* %nb, i32 %j
  %b = load i8, i8* %sa
  %bi = sext i8 %b to i32
  call void @rt_pct_specputc(i8* %sp, i32* %sn, i32 %bi)
  %jn = add i32 %j, 1
  br label %loop
fin:
  ret void
ret:
  ret void
}
`
}

// percentRuntimeParse is the walk's front end: the raise helpers, the argument cursor, the scan for the
// next percent sign, the mapping-key read, and the parse of one conversion.
//
// The parse state is one global per fact rather than one register per fact because the flag, width and
// precision scan is a single loop with a phase, entered from five places; a register would need a phi in
// every header for a value written exactly once per conversion. The argument cursor is the walker's, not
// the compiler's, because the reference takes the width of %*d and the precision of %.*f out of the same
// positions the values come from — and a format that never reaches a conversion consumes nothing, which
// is how "%" + "s" % 1 reaches the reference's "not all arguments converted" sentence.
//
// Every arm reads its argument's TAG and refuses what the conversion cannot render, in the reference's
// class and words. That is the whole reason the door is IR: an argument's kind is the program's fact and
// not the compiler's, and it is read once, here, by the same code that raises the wrong-kind TypeError
// (ADR 0166's rule against the confident wrong answer, ADR 0214's catchable raise).
func percentRuntimeParse() string {
	return `
@rt.pct.conv = private global i32 0
@rt.pct.left = private global i32 0
@rt.pct.zero = private global i32 0
@rt.pct.alt = private global i32 0
@rt.pct.sgn = private global i32 0
@rt.pct.wid = private global i32 0
@rt.pct.prec = private global i32 0
@rt.pct.dot = private global i32 0
@rt.pct.cidx = private global i32 0
@rt.pct.phase = private global i32 0
@rt.pct.starw = private global i32 0
@rt.pct.starp = private global i32 0
@rt.pct.keyused = private global i32 0
@rt.pct.cur = private global i32 0
@rt.pct.curt = private global i32 0
; How many bytes the one character %c renders is made of — the code-point arm fills @rt.pct.utf and
; writes this, the one-character str arm copies its bytes in and writes this, and the padding path below
; reads it so both arms share the one way a field is filled.
@rt.pct.chrlen = private global i32 0
@rt.pct.hi = private global i32 0

; ---- the raise helpers: one message's worth of words, spelled once ----

define internal void @rt_pct_msg(i32 %code, i8* %cls, i8* %txt) {
entry:
  call void @rt_pct_msg_begin(i32 %code, i8* %cls, i32 1)
  call void @rt_pct_mput(i8* %txt)
  ret void
}

define internal void @rt_pct_keyerr(i8* %kb) {
entry:
  call void @rt_pct_msg_begin(i32 @CODE_KEYERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.key, i32 0, i32 0), i32 1)
  call void @rt_pct_mput(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.quote, i32 0, i32 0))
  call void @rt_pct_mput(i8* %kb)
  call void @rt_pct_mput(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.quote, i32 0, i32 0))
  ret void
}

; The two wrong-kind sentences the numeric conversions raise carry the conversion character and the
; argument's kind name. The name is rt_kind_name's answer — the same word the arithmetic door, the
; printer and the comparison use for that tag — which is what makes the sentence agree with what
; print(x) calls x (ADR 0182's one table, ADR 0171's one formatted-printing door).
define internal void @rt_pct_kind_real(i32 %t) {
entry:
  call void @rt_pct_msg_begin(i32 @CODE_TYPEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.type, i32 0, i32 0), i32 1)
  call void @rt_pct_conv_name()
  call void @rt_pct_mput(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.1.a, i32 0, i32 0))
  call void @rt_pct_mputkind(i32 %t)
  ret void
}

define internal void @rt_pct_kind_int(i32 %t) {
entry:
  call void @rt_pct_msg_begin(i32 @CODE_TYPEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.type, i32 0, i32 0), i32 1)
  call void @rt_pct_conv_name()
  call void @rt_pct_mput(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.2.a, i32 0, i32 0))
  call void @rt_pct_mputkind(i32 %t)
  ret void
}

define internal void @rt_pct_kind_float(i32 %t) {
entry:
  call void @rt_pct_msg_begin(i32 @CODE_TYPEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.type, i32 0, i32 0), i32 1)
  call void @rt_pct_mput(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.3.a, i32 0, i32 0))
  call void @rt_pct_mputkind(i32 %t)
  ret void
}

; rt_pct_conv_name writes the conversion the way the reference names it in these two sentences: with
; the percent sign in front of it (%d format: a real number is required, not str), which is NOT how the
; unsupported-character sentence writes it (that one quotes the bare character, and builds it itself).
define internal void @rt_pct_conv_name() {
entry:
  call void @rt_pct_mput(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.pct, i32 0, i32 0))
  %cv = load i32, i32* @rt.pct.conv
  call void @rt_pct_mputc(i32 %cv)
  ret void
}

; rt_pct_unsupported is the reference's own words for a conversion character it does not implement,
; including the character, its code point in hex, and the index it sat at.
define internal void @rt_pct_unsupported() {
entry:
  call void @rt_pct_msg_begin(i32 @CODE_VALUEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.value, i32 0, i32 0), i32 1)
  call void @rt_pct_mput(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.4.a, i32 0, i32 0))
  %cv = load i32, i32* @rt.pct.conv
  call void @rt_pct_mputc(i32 %cv)
  call void @rt_pct_mput(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.4.b, i32 0, i32 0))
  call void @rt_pct_mputhex(i32 %cv)
  call void @rt_pct_mput(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.4.c, i32 0, i32 0))
  %ix = load i32, i32* @rt.pct.cidx
  call void @rt_pct_mputint(i32 %ix)
  ret void
}

; ---- the argument cursor ----

; rt_pct_keyed turns the argument the cursor just spent into the value the conversion renders, for the
; mapping form only: %(a)d asks the same dictionary %(b)s asks, and reads its own key out of it. The
; reference checks the argument's shape before the key exists, so a non-mapping is the mapping TypeError
; and a missing key is the KeyError — both raised here, by the one arm every conversion goes through.
define internal void @rt_pct_keyed() {
entry:
  %ku = load i32, i32* @rt.pct.keyused
  %hk = icmp ne i32 %ku, 0
  br i1 %hk, label %lookup, label %ret
lookup:
  %mp = load i32, i32* @rt.pct.map
  %no = icmp eq i32 %mp, 0
  br i1 %no, label %nomap, label %find
nomap:
  %mh = load i32, i32* @rt.pct.cur
  %mt = load i32, i32* @rt.pct.curt
  call void @rt_pct_needmapping(i32 %mh, i32 %mt)
  br label %ret
find:
  %h = load i32, i32* @rt.pct.cur
  %kb = getelementptr [256 x i8], [256 x i8]* @rt.pct.key, i32 0, i32 0
  %ki = call i32 @rt_str_intern(i8* %kb)
  %at = call i32 @rt_dict_find(i32 %h, i32 %ki, i32 @TAG_STR@)
  %miss = icmp slt i32 %at, 0
  br i1 %miss, label %keyerr, label %got
keyerr:
  call void @rt_pct_keyerr(i8* %kb)
  br label %ret
got:
  %v = call i32 @rt_dict_get_tagged(i32 %h, i32 %ki, i32 @TAG_STR@)
  %t = call i32 @rt_dict_value_tag(i32 %h, i32 %ki, i32 @TAG_STR@)
  store i32 %v, i32* @rt.pct.cur
  store i32 %t, i32* @rt.pct.curt
  br label %ret
ret:
  ret void
}

define internal void @rt_pct_take() {
entry:
  ; A keyed conversion does not draw from the argument list at all: the mapping IS argument zero, and
  ; the reference hands it to every keyed conversion in the format. Only the positional conversions walk
  ; the list, and only they can run out of arguments (roadmap Gap R.31).
  %ku = load i32, i32* @rt.pct.keyused
  %hk = icmp ne i32 %ku, 0
  br i1 %hk, label %mapping, label %positional
mapping:
  %mp = load i32, i32* @rt.pct.map
  %no = icmp eq i32 %mp, 0
  br i1 %no, label %needmap, label %take0
take0:
  %p0 = load i32, i32* getelementptr ([64 x i32], [64 x i32]* @rt.pct.argp, i32 0, i32 0)
  %t0 = load i32, i32* getelementptr ([64 x i32], [64 x i32]* @rt.pct.argt, i32 0, i32 0)
  store i32 %p0, i32* @rt.pct.cur
  store i32 %t0, i32* @rt.pct.curt
  ret void
needmap:
  %nh = load i32, i32* getelementptr ([64 x i32], [64 x i32]* @rt.pct.argp, i32 0, i32 0)
  %nt = load i32, i32* getelementptr ([64 x i32], [64 x i32]* @rt.pct.argt, i32 0, i32 0)
  call void @rt_pct_needmapping(i32 %nh, i32 %nt)
  ret void
positional:
  %r = load i32, i32* @rt.pct.ridx
  %n = load i32, i32* @rt.pct.narg
  %out = icmp sge i32 %r, %n
  br i1 %out, label %none, label %get
none:
  call void @rt_pct_msg(i32 @CODE_TYPEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.type, i32 0, i32 0), i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.noargs, i32 0, i32 0))
  ret void
get:
  %pp = getelementptr [64 x i32], [64 x i32]* @rt.pct.argp, i32 0, i32 %r
  %tt = getelementptr [64 x i32], [64 x i32]* @rt.pct.argt, i32 0, i32 %r
  %pv = load i32, i32* %pp
  %tv = load i32, i32* %tt
  store i32 %pv, i32* @rt.pct.cur
  store i32 %tv, i32* @rt.pct.curt
  %nx = add i32 %r, 1
  store i32 %nx, i32* @rt.pct.ridx
  ret void
}

; rt_pct_keycheck asks the mapping for the key the format named, and answers the reference's KeyError if
; it is not there. It runs while the conversion is still being READ, because that is when the reference
; asks: a format whose key is missing is a KeyError even when the conversion character never arrived,
; and a program that wrote a key with no conversion character hears about the key first.
define internal void @rt_pct_keycheck() {
entry:
  %mp = load i32, i32* @rt.pct.map
  %no = icmp eq i32 %mp, 0
  br i1 %no, label %needmap, label %have
have:
  %h = load i32, i32* getelementptr ([64 x i32], [64 x i32]* @rt.pct.argp, i32 0, i32 0)
  %kb = getelementptr [256 x i8], [256 x i8]* @rt.pct.key, i32 0, i32 0
  %ki = call i32 @rt_str_intern(i8* %kb)
  %at = call i32 @rt_dict_find(i32 %h, i32 %ki, i32 @TAG_STR@)
  %miss = icmp slt i32 %at, 0
  br i1 %miss, label %keyerr, label %done
keyerr:
  call void @rt_pct_keyerr(i8* %kb)
  br label %done
done:
  ret void
needmap:
  %nh = load i32, i32* getelementptr ([64 x i32], [64 x i32]* @rt.pct.argp, i32 0, i32 0)
  %nt = load i32, i32* getelementptr ([64 x i32], [64 x i32]* @rt.pct.argt, i32 0, i32 0)
  call void @rt_pct_needmapping(i32 %nh, i32 %nt)
  ret void
}

; ---- the scan, the key read, the parse ----

define internal i32 @rt_pct_scan(i8* %p, i32 %n, i32 %from, i32 %ch) {
entry:
  br label %loop
loop:
  %i = phi i32 [ %from, %entry ], [ %in, %step ]
  %e = icmp sge i32 %i, %n
  br i1 %e, label %nf, label %body
body:
  %at = getelementptr i8, i8* %p, i32 %i
  %c = load i8, i8* %at
  %b = sext i8 %c to i32
  %hit = icmp eq i32 %b, %ch
  br i1 %hit, label %found, label %step
step:
  %in = add i32 %i, 1
  br label %loop
nf:
  ret i32 %n
found:
  ret i32 %i
}

define internal i32 @rt_pct_keyread(i8* %p, i32 %n, i32 %from, i8* %dst) {
entry:
  br label %loop
loop:
  %i = phi i32 [ %from, %entry ], [ %in, %write ]
  %j = phi i32 [ 0, %entry ], [ %jn, %write ]
  %e = icmp sge i32 %i, %n
  br i1 %e, label %bad, label %body
body:
  %at = getelementptr i8, i8* %p, i32 %i
  %c = load i8, i8* %at
  %close = icmp eq i8 %c, 41
  br i1 %close, label %fin, label %fits
fits:
  %ok = icmp slt i32 %j, @KEYLIMIT@
  br i1 %ok, label %write, label %bad
write:
  %da = getelementptr i8, i8* %dst, i32 %j
  store i8 %c, i8* %da
  %jn = add i32 %j, 1
  %in = add i32 %i, 1
  br label %loop
fin:
  %z = getelementptr i8, i8* %dst, i32 %j
  store i8 0, i8* %z
  %past = add i32 %i, 1
  ret i32 %past
bad:
  ret i32 -1
}

define internal i32 @rt_pct_parse(i8* %p, i32 %n, i32 %at) {
entry:
  store i32 0, i32* @rt.pct.spn
  call void @rt_pct_specputc(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.spec, i32 0, i32 0), i32* @rt.pct.spn, i32 37)
  store i32 0, i32* @rt.pct.conv
  store i32 0, i32* @rt.pct.left
  store i32 0, i32* @rt.pct.zero
  store i32 0, i32* @rt.pct.alt
  store i32 0, i32* @rt.pct.sgn
  store i32 0, i32* @rt.pct.wid
  store i32 0, i32* @rt.pct.prec
  store i32 0, i32* @rt.pct.dot
  store i32 0, i32* @rt.pct.phase
  store i32 0, i32* @rt.pct.starw
  store i32 0, i32* @rt.pct.starp
  store i32 0, i32* @rt.pct.keyused
  %f0 = add i32 %at, 1
  store i32 %f0, i32* @rt.pct.hi
  br label %loop
loop:
  %f = phi i32 [ %f0, %entry ], [ %fn, %advance ]
  %over = icmp sge i32 %f, %n
  br i1 %over, label %incomplete, label %body
body:
  %pa = getelementptr i8, i8* %p, i32 %f
  %c8 = load i8, i8* %pa
  %c = sext i8 %c8 to i32
  %ph = load i32, i32* @rt.pct.phase
  %islp = icmp eq i32 %c, 40
  %first = icmp eq i32 %ph, 0
  %dokey = and i1 %islp, %first
  br i1 %dokey, label %key, label %star
key:
  ; The reference checks the argument's shape before it reads the key, so %(a)s against a number is
  ; 'format requires a mapping' and not a complaint about the key.
  %mp = load i32, i32* @rt.pct.map
  %no = icmp eq i32 %mp, 0
  br i1 %no, label %needmap, label %readkey
readkey:
  %kb = getelementptr [256 x i8], [256 x i8]* @rt.pct.key, i32 0, i32 0
  %kf = add i32 %f, 1
  %kr = call i32 @rt_pct_keyread(i8* %p, i32 %n, i32 %kf, i8* %kb)
  %nob = icmp slt i32 %kr, 0
  br i1 %nob, label %inckey, label %setkey
setkey:
  store i32 1, i32* @rt.pct.keyused
  store i32 %kr, i32* @rt.pct.hi
  call void @rt_pct_keycheck()
  br label %advance
star:
  %isstar = icmp eq i32 %c, 42
  br i1 %isstar, label %starrun, label %dotchk
starrun:
  ; * is a width only before the width has been read, and a precision only after the point. Anywhere
  ; else it is the conversion character, and the reference answers that one with its
  ; unsupported-format-character ValueError (%5* is that case), not with a star.
  %ph2 = load i32, i32* @rt.pct.phase
  %in2 = icmp eq i32 %ph2, 2
  %prew = icmp eq i32 %ph2, 0
  %legal = or i1 %in2, %prew
  br i1 %legal, label %stardisp, label %conv
stardisp:
  %at2 = icmp eq i32 %ph2, 2

  br i1 %at2, label %starprec, label %starwidth
starwidth:
  store i32 1, i32* @rt.pct.starw
  store i32 1, i32* @rt.pct.phase
  %n1 = add i32 %f, 1
  store i32 %n1, i32* @rt.pct.hi
  br label %advance
starprec:
  store i32 1, i32* @rt.pct.starp
  %n2 = add i32 %f, 1
  store i32 %n2, i32* @rt.pct.hi
  br label %advance
dotchk:
  %isdot = icmp eq i32 %c, 46
  %ph3 = load i32, i32* @rt.pct.phase
  %before = icmp slt i32 %ph3, 2
  %setdot = and i1 %isdot, %before
  br i1 %setdot, label %dot, label %digitchk
dot:
  store i32 1, i32* @rt.pct.dot
  store i32 2, i32* @rt.pct.phase
  %n3 = add i32 %f, 1
  store i32 %n3, i32* @rt.pct.hi
  br label %advance
digitchk:
  ; A leading 0 is the zero flag, not the width 0: that is the reference's reading (%05d is width five
  ; filled with zeros), and reading it as a digit instead loses the flag and leaves the field space
  ; filled — the difference between -0003 and the three-space wrong answer.
  %z = icmp sge i32 %c, 48
  %ni = icmp sle i32 %c, 57
  %dig0 = and i1 %z, %ni
  %ph6 = load i32, i32* @rt.pct.phase
  %at0d = icmp eq i32 %ph6, 0
  %iszero0 = icmp eq i32 %c, 48
  %flag0 = and i1 %at0d, %iszero0
  %notflag = xor i1 %flag0, true
  %dig = and i1 %dig0, %notflag
  br i1 %dig, label %accum, label %flagchk
accum:
  %d = sub i32 %c, 48
  %ph4 = load i32, i32* @rt.pct.phase
  %in3 = icmp eq i32 %ph4, 2
  br i1 %in3, label %precnum, label %widnum
widnum:
  %cw = load i32, i32* @rt.pct.wid
  %nw = mul i32 %cw, 10
  %nw2 = add i32 %nw, %d
  store i32 %nw2, i32* @rt.pct.wid
  store i32 1, i32* @rt.pct.phase
  %n4 = add i32 %f, 1
  store i32 %n4, i32* @rt.pct.hi
  br label %advance
precnum:
  %cp = load i32, i32* @rt.pct.prec
  %np = mul i32 %cp, 10
  %np2 = add i32 %np, %d
  store i32 %np2, i32* @rt.pct.prec
  %n5 = add i32 %f, 1
  store i32 %n5, i32* @rt.pct.hi
  br label %advance
flagchk:
  %ph5 = load i32, i32* @rt.pct.phase
  %at0 = icmp eq i32 %ph5, 0
  %isp = icmp eq i32 %c, 32
  %ipl = icmp eq i32 %c, 43
  %imi = icmp eq i32 %c, 45
  %ish = icmp eq i32 %c, 35
  %iz = icmp eq i32 %c, 48
  %o1 = or i1 %isp, %ipl
  %o2 = or i1 %o1, %imi
  %o3 = or i1 %o2, %ish
  %o4 = or i1 %o3, %iz
  %isflag = and i1 %at0, %o4
  br i1 %isflag, label %flag, label %conv
flag:
  %cb = trunc i32 %c to i8
  %cb2 = sext i8 %cb to i32
  call void @rt_pct_specputc(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.spec, i32 0, i32 0), i32* @rt.pct.spn, i32 %cb2)
  %wflag = icmp eq i32 %c, 45
  br i1 %wflag, label %setleft, label %zflag
setleft:
  store i32 1, i32* @rt.pct.left
  br label %zflag
zflag:
  %zb = icmp eq i32 %c, 48
  br i1 %zb, label %setzero, label %aflag
setzero:
  store i32 1, i32* @rt.pct.zero
  br label %aflag
aflag:
  %hb = icmp eq i32 %c, 35
  br i1 %hb, label %setalt, label %sflag
setalt:
  store i32 1, i32* @rt.pct.alt
  br label %sflag
sflag:
  %pb = icmp eq i32 %c, 43
  br i1 %pb, label %setplus, label %spflag
setplus:
  store i32 1, i32* @rt.pct.sgn
  br label %spflag
spflag:
  %spb = icmp eq i32 %c, 32
  br i1 %spb, label %setsp, label %advflag
setsp:
  %has = load i32, i32* @rt.pct.sgn
  %free = icmp eq i32 %has, 0
  br i1 %free, label %setw, label %advflag
setw:
  store i32 2, i32* @rt.pct.sgn
  br label %advflag
advflag:
  %n6 = add i32 %f, 1
  store i32 %n6, i32* @rt.pct.hi
  br label %advance
conv:
  store i32 %c, i32* @rt.pct.conv
  store i32 %f, i32* @rt.pct.cidx
  %past = add i32 %f, 1
  store i32 %past, i32* @rt.pct.hi
  ret i32 %past
advance:
  %fn = load i32, i32* @rt.pct.hi
  br label %loop
incomplete:
  call void @rt_pct_msg(i32 @CODE_VALUEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.value, i32 0, i32 0), i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.incomplete, i32 0, i32 0))
  ret i32 -1
inckey:
  call void @rt_pct_msg(i32 @CODE_VALUEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.value, i32 0, i32 0), i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.nokey, i32 0, i32 0))
  ret i32 -1
needmap:
  %nh2 = load i32, i32* getelementptr ([64 x i32], [64 x i32]* @rt.pct.argp, i32 0, i32 0)
  %nt2 = load i32, i32* getelementptr ([64 x i32], [64 x i32]* @rt.pct.argt, i32 0, i32 0)
  call void @rt_pct_needmapping(i32 %nh2, i32 %nt2)
  ret i32 -1
}
`
}

// percentRuntimeConversions holds the dispatch and the seven arms: text (%s, %r, %a), decimal (%d, %i),
// octal, hex lower, hex upper, character (%c) and real (%f, %F, %e, %E, %g, %G).
//
// Each arm takes its own argument through the cursor — which is what makes %*d consume its width and
// then its value in the order the reference does — reads the pair's TAG once, and refuses the kinds the
// conversion cannot render with the reference's class and sentence. The renderer it then calls is given
// a value that conversion can render: no renderer has to guess, and neither does the message.
func percentRuntimeConversions() string {
	return `
define internal void @rt_pct_emit() {
entry:
  call void @rt_pct_stars()
  %e = load i32, i32* @rt.pct.err
  %bad = icmp ne i32 %e, 0
  br i1 %bad, label %ret, label %d0
d0:
  %cv = load i32, i32* @rt.pct.conv
  %is = icmp eq i32 %cv, 115
  %ir = icmp eq i32 %cv, 114
  %ia = icmp eq i32 %cv, 97
  %t1 = or i1 %is, %ir
  %t2 = or i1 %t1, %ia
  br i1 %t2, label %textarm, label %d1
textarm:
  %q1 = select i1 %ir, i32 1, i32 0
  %q = select i1 %ia, i32 1, i32 %q1
  call void @rt_pct_conv_text(i32 %q)
  br label %ret
d1:
  %id = icmp eq i32 %cv, 100
  %ii = icmp eq i32 %cv, 105
  %de = or i1 %id, %ii
  br i1 %de, label %decarm, label %d2
decarm:
  call void @rt_pct_conv_dec()
  br label %ret
d2:
  %io = icmp eq i32 %cv, 111
  br i1 %io, label %octarm, label %d3
octarm:
  call void @rt_pct_conv_rad(i32 8, i32 0)
  br label %ret
d3:
  %ix = icmp eq i32 %cv, 120
  br i1 %ix, label %lox, label %d4
lox:
  call void @rt_pct_conv_rad(i32 16, i32 0)
  br label %ret
d4:
  %iX = icmp eq i32 %cv, 88
  br i1 %iX, label %upx, label %d5
upx:
  call void @rt_pct_conv_rad(i32 16, i32 1)
  br label %ret
d5:
  %ic = icmp eq i32 %cv, 99
  br i1 %ic, label %chararm, label %d6
chararm:
  call void @rt_pct_conv_char()
  br label %ret
d6:
  %if1 = icmp eq i32 %cv, 102
  %iF = icmp eq i32 %cv, 70
  %ie = icmp eq i32 %cv, 101
  %iE = icmp eq i32 %cv, 69
  %ig = icmp eq i32 %cv, 103
  %iG = icmp eq i32 %cv, 71
  %f1 = or i1 %if1, %iF
  %f2 = or i1 %f1, %ie
  %f3 = or i1 %f2, %iE
  %f4 = or i1 %f3, %ig
  %f5 = or i1 %f4, %iG
  br i1 %f5, label %fltarm, label %nope
fltarm:
  call void @rt_pct_conv_float()
  br label %ret
nope:
  call void @rt_pct_unsupported()
  br label %ret
ret:
  ret void
}

; rt_pct_stars spends the arguments a %* or .* asked for, in the order they appear in the tuple, and
; takes the reference's answer for a negative width: left-aligned, not a negative field.
define internal void @rt_pct_stars() {
entry:
  %sw = load i32, i32* @rt.pct.starw
  %hasw = icmp ne i32 %sw, 0
  br i1 %hasw, label %take_w, label %prec_star
take_w:
  call void @rt_pct_take()
  %e1 = load i32, i32* @rt.pct.err
  %b1 = icmp ne i32 %e1, 0
  br i1 %b1, label %ret, label %chk_w
chk_w:
  %t1 = load i32, i32* @rt.pct.curt
  %isint1 = icmp eq i32 %t1, @TAG_INT@
  %isb1 = icmp eq i32 %t1, @TAG_BOOL@
  %ok1 = or i1 %isint1, %isb1
  br i1 %ok1, label %got_w, label %star_bad
got_w:
  %v1 = load i32, i32* @rt.pct.cur
  %neg1 = icmp slt i32 %v1, 0
  %abs1 = sub i32 0, %v1
  %wv = select i1 %neg1, i32 %abs1, i32 %v1
  store i32 %wv, i32* @rt.pct.wid
  br i1 %neg1, label %setl, label %prec_star
setl:
  store i32 1, i32* @rt.pct.left
  br label %prec_star
prec_star:
  %sp = load i32, i32* @rt.pct.starp
  %hasp = icmp ne i32 %sp, 0
  br i1 %hasp, label %take_p, label %ret
take_p:
  call void @rt_pct_take()
  %e2 = load i32, i32* @rt.pct.err
  %b2 = icmp ne i32 %e2, 0
  br i1 %b2, label %ret, label %chk_p
chk_p:
  %t2 = load i32, i32* @rt.pct.curt
  %isint2 = icmp eq i32 %t2, @TAG_INT@
  %isb2 = icmp eq i32 %t2, @TAG_BOOL@
  %ok2 = or i1 %isint2, %isb2
  br i1 %ok2, label %got_p, label %star_bad
got_p:
  %v2 = load i32, i32* @rt.pct.cur
  store i32 %v2, i32* @rt.pct.prec
  store i32 1, i32* @rt.pct.dot
  br label %ret
star_bad:
  call void @rt_pct_msg(i32 @CODE_TYPEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.type, i32 0, i32 0), i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.stars, i32 0, i32 0))
  br label %ret
ret:
  ret void
}

define internal void @rt_pct_conv_text(i32 %q) {
entry:
  call void @rt_pct_take()
  %e = load i32, i32* @rt.pct.err
  %bad = icmp ne i32 %e, 0
  br i1 %bad, label %ret, label %keyed
keyed:
  call void @rt_pct_keyed()
  %e2 = load i32, i32* @rt.pct.err
  %bad2 = icmp ne i32 %e2, 0
  br i1 %bad2, label %ret, label %render
render:
  %p = load i32, i32* @rt.pct.cur
  %t2 = load i32, i32* @rt.pct.curt
  %w = load i32, i32* @rt.pct.wid
  %pr = load i32, i32* @rt.pct.prec
  %d = load i32, i32* @rt.pct.dot
  %l = load i32, i32* @rt.pct.left
  call void @rt_pct_render_text(i32 %p, i32 %t2, i32 %q, i32 %w, i32 %pr, i32 %d, i32 %l)
  br label %ret
ret:
  ret void
}

; rt_pct_conv_dec is %d and %i. A float is allowed and truncates toward zero, which is the reference's
; answer and not printf's (printf would have refused the conversion outright); NaN and infinity are the
; two cases it refuses, with two different classes.
define internal void @rt_pct_conv_dec() {
entry:
  call void @rt_pct_take()
  %e = load i32, i32* @rt.pct.err
  %bad = icmp ne i32 %e, 0
  br i1 %bad, label %ret, label %keyed
keyed:
  call void @rt_pct_keyed()
  %ek = load i32, i32* @rt.pct.err
  %bk = icmp ne i32 %ek, 0
  br i1 %bk, label %ret, label %kindchk
kindchk:
  %t = load i32, i32* @rt.pct.curt
  %p = load i32, i32* @rt.pct.cur
  %isf = icmp eq i32 %t, @TAG_FLOAT@
  %isi = icmp eq i32 %t, @TAG_INT@
  %isb = icmp eq i32 %t, @TAG_BOOL@
  %n1 = or i1 %isi, %isb
  %ok = or i1 %n1, %isf
  br i1 %ok, label %work, label %wrong
wrong:
  call void @rt_pct_kind_real(i32 %t)
  br label %ret
work:
  br i1 %isf, label %fromfloat, label %direct
direct:
  store i32 %p, i32* @rt.pct.cur
  br label %out
fromfloat:
  %d = call double @rt_lift_num(i32 %p, i32 %t)
  %nan = fcmp uno double %d, 0.0
  br i1 %nan, label %is_nan, label %infchk
is_nan:
  call void @rt_pct_msg(i32 @CODE_VALUEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.value, i32 0, i32 0), i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.real, i32 0, i32 0))
  br label %ret
infchk:
  %hi = fcmp ogt double %d, 1.7976931348623157e+308
  %lo = fcmp olt double %d, -1.7976931348623157e+308
  %inf = or i1 %hi, %lo
  br i1 %inf, label %is_inf, label %trunc
is_inf:
  call void @rt_pct_msg(i32 @CODE_OVERFLOWERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.over, i32 0, i32 0), i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.inf, i32 0, i32 0))
  br label %ret
trunc:
  %iv = fptosi double %d to i32
  store i32 %iv, i32* @rt.pct.cur
  br label %out
out:
  %v = load i32, i32* @rt.pct.cur
  %w = load i32, i32* @rt.pct.wid
  %pr = load i32, i32* @rt.pct.prec
  %dot = load i32, i32* @rt.pct.dot
  %lf = load i32, i32* @rt.pct.left
  %z = load i32, i32* @rt.pct.zero
  %sg = load i32, i32* @rt.pct.sgn
  call void @rt_pct_render_int(i32 %v, i32 10, i32 0, i32 0, i32 %w, i32 %pr, i32 %dot, i32 %lf, i32 %z, i32 %sg)
  br label %ret
ret:
  ret void
}

; rt_pct_conv_rad is %o, %x and %X: an INTEGER is required, which is the reference's refusal of a float
; (%d takes one and truncates, these two do not) as well as of everything else.
define internal void @rt_pct_conv_rad(i32 %r, i32 %up) {
entry:
  call void @rt_pct_take()
  %e = load i32, i32* @rt.pct.err
  %bad = icmp ne i32 %e, 0
  br i1 %bad, label %ret, label %keyed
keyed:
  call void @rt_pct_keyed()
  %ek = load i32, i32* @rt.pct.err
  %bk = icmp ne i32 %ek, 0
  br i1 %bk, label %ret, label %kindchk
kindchk:
  %t = load i32, i32* @rt.pct.curt
  %isi = icmp eq i32 %t, @TAG_INT@
  %isb = icmp eq i32 %t, @TAG_BOOL@
  %ok = or i1 %isi, %isb
  %isf = icmp eq i32 %t, @TAG_FLOAT@
  br i1 %ok, label %out, label %wrong
wrong:
  br i1 %isf, label %intreq, label %intreq2
intreq:
  call void @rt_pct_kind_int(i32 %t)
  br label %ret
intreq2:
  call void @rt_pct_kind_int(i32 %t)
  br label %ret
out:
  %v = load i32, i32* @rt.pct.cur
  %w = load i32, i32* @rt.pct.wid
  %pr = load i32, i32* @rt.pct.prec
  %dot = load i32, i32* @rt.pct.dot
  %lf = load i32, i32* @rt.pct.left
  %z = load i32, i32* @rt.pct.zero
  %sg = load i32, i32* @rt.pct.sgn
  %alt = load i32, i32* @rt.pct.alt
  call void @rt_pct_render_int(i32 %v, i32 %r, i32 %up, i32 %alt, i32 %w, i32 %pr, i32 %dot, i32 %lf, i32 %z, i32 %sg)
  br label %ret
ret:
  ret void
}

define internal void @rt_pct_conv_float() {
entry:
  call void @rt_pct_take()
  %e = load i32, i32* @rt.pct.err
  %bad = icmp ne i32 %e, 0
  br i1 %bad, label %ret, label %keyed
keyed:
  call void @rt_pct_keyed()
  %ek = load i32, i32* @rt.pct.err
  %bk = icmp ne i32 %ek, 0
  br i1 %bk, label %ret, label %kindchk
kindchk:
  %t = load i32, i32* @rt.pct.curt
  %p = load i32, i32* @rt.pct.cur
  %isi = icmp eq i32 %t, @TAG_INT@
  %isf = icmp eq i32 %t, @TAG_FLOAT@
  %isb = icmp eq i32 %t, @TAG_BOOL@
  %n1 = or i1 %isi, %isf
  %ok = or i1 %n1, %isb
  br i1 %ok, label %out, label %wrong
wrong:
  call void @rt_pct_kind_float(i32 %t)
  br label %ret
out:
  %w = load i32, i32* @rt.pct.wid
  %pr = load i32, i32* @rt.pct.prec
  %dot = load i32, i32* @rt.pct.dot
  %cv = load i32, i32* @rt.pct.conv
  call void @rt_pct_render_float(i32 %p, i32 %t, i32 %w, i32 %pr, i32 %dot, i32 %cv)
  br label %ret
ret:
  ret void
}

; rt_pct_conv_char is %c: an integer code point below 0x110000, encoded as UTF-8 and padded as the one
; character it is. Anything the reference would answer "%c requires int or char" for, this answers the
; same way — including the float that every other numeric conversion would happily truncate.
define internal void @rt_pct_conv_char() {
entry:
  call void @rt_pct_take()
  %e = load i32, i32* @rt.pct.err
  %bad = icmp ne i32 %e, 0
  br i1 %bad, label %ret, label %keyed
keyed:
  call void @rt_pct_keyed()
  %ek = load i32, i32* @rt.pct.err
  %bk = icmp ne i32 %ek, 0
  br i1 %bk, label %ret, label %kindchk
kindchk:
  %t = load i32, i32* @rt.pct.curt
  %isi = icmp eq i32 %t, @TAG_INT@
  %isb = icmp eq i32 %t, @TAG_BOOL@
  %isnum = or i1 %isi, %isb
  %iss = icmp eq i32 %t, @TAG_STR@
  %okkind = or i1 %isnum, %iss
  br i1 %okkind, label %which, label %nochr
which:
  %notstr = icmp ne i32 %t, @TAG_STR@
  br i1 %notstr, label %range, label %onec
onec:
  ; A str argument is the character %c renders only when it IS one character:
  ; the reference measures characters, not bytes, so a two-byte accent is one
  ; character and is accepted, and two characters are the TypeError every other
  ; wrong-shape argument gets.
  %sh = load i32, i32* @rt.pct.cur
  %nc = call i32 @rt_str_nchars(i32 %sh)
  %onechr = icmp eq i32 %nc, 1
  br i1 %onechr, label %strbytes, label %nochr
strbytes:
  ; One character: stage its bytes where the code-point arm puts them, and let the
  ; one padding path below lay them in the field — right-aligned padding has to
  ; come before the bytes, so both arms share it.
  %sbuf = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 0
  %sptr = call i8* @rt_str_ptr(i32 %sh)
  %snum = call i32 @rt_str_len(i32 %sh)
  %fitsb = icmp ule i32 %snum, 8
  br i1 %fitsb, label %scopy, label %nochr
scopy:
  %sb2 = call i32 @rt_pct_cpbytes(i8* %sptr, i32 %snum, i8* %sbuf)
  store i32 %sb2, i32* @rt.pct.chrlen
  br label %emit
nochr:
  call void @rt_pct_msg(i32 @CODE_TYPEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.type, i32 0, i32 0), i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.nochr, i32 0, i32 0))
  br label %ret
range:
  %cp = load i32, i32* @rt.pct.cur
  %big = icmp sge i32 %cp, 1114112
  %neg = icmp slt i32 %cp, 0
  %bad2 = or i1 %big, %neg
  br i1 %bad2, label %over, label %put
over:
  call void @rt_pct_msg(i32 @CODE_OVERFLOWERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.over, i32 0, i32 0), i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.rngchr, i32 0, i32 0))
  br label %ret
put:
  %cp2 = load i32, i32* @rt.pct.cur
  %nb2 = call i32 @rt_pct_utf8(i32 %cp2)
  store i32 %nb2, i32* @rt.pct.chrlen
  br label %emit
emit:
  %cb = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 0
  %nb = load i32, i32* @rt.pct.chrlen
  %w = load i32, i32* @rt.pct.wid
  %l = load i32, i32* @rt.pct.left
  %gap = sub i32 %w, 1
  %gn = icmp slt i32 %gap, 0
  %fill = select i1 %gn, i32 0, i32 %gap
  %lt = icmp ne i32 %l, 0
  br i1 %lt, label %lft, label %rgt
lft:
  call void @rt_pct_putn(i8* %cb, i32 %nb)
  call void @rt_pct_pad(i32 %fill)
  br label %ret
rgt:
  call void @rt_pct_pad(i32 %fill)
  call void @rt_pct_putn(i8* %cb, i32 %nb)
  br label %ret
ret:
  ret void
}

; rt_pct_cpbytes copies n bytes of a string into a character buffer and answers how many it wrote.
; The one-character %c argument lands here: whatever bytes that character is made of are the bytes the
; conversion writes, and no re-encoding of the code point happens between the string and the output.
define internal i32 @rt_pct_cpbytes(i8* %src, i32 %n, i8* %dst) {
entry:
  %clamped = icmp sgt i32 %n, 8
  %len = select i1 %clamped, i32 8, i32 %n
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ %in, %step ]
  %done = icmp sge i32 %i, %len
  br i1 %done, label %fin, label %step
step:
  %sa = getelementptr i8, i8* %src, i32 %i
  %c = load i8, i8* %sa
  %da = getelementptr i8, i8* %dst, i32 %i
  store i8 %c, i8* %da
  %in = add i32 %i, 1
  br label %loop
fin:
  ret i32 %len
}

; rt_pct_percent writes the literal percent the %% form asked for - or, for the mapping form, the key
; the program named, which the reference checks against the mapping before it writes anything.
define internal void @rt_pct_percent() {
entry:
  %ku = load i32, i32* @rt.pct.keyused
  %hk = icmp ne i32 %ku, 0
  br i1 %hk, label %keyed, label %plain
plain:
  call void @rt_pct_put(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.pct, i32 0, i32 0))
  ret void
keyed:
  %mp = load i32, i32* @rt.pct.map
  %no = icmp eq i32 %mp, 0
  br i1 %no, label %nomap, label %take
take:
  call void @rt_pct_take()
  %e = load i32, i32* @rt.pct.err
  %bad = icmp ne i32 %e, 0
  br i1 %bad, label %ret, label %find
find:
  %h = load i32, i32* @rt.pct.cur
  %kb = getelementptr [256 x i8], [256 x i8]* @rt.pct.key, i32 0, i32 0
  %ki = call i32 @rt_str_intern(i8* %kb)
  %at = call i32 @rt_dict_find(i32 %h, i32 %ki, i32 @TAG_STR@)
  %miss = icmp slt i32 %at, 0
  br i1 %miss, label %keyerr, label %ok
keyerr:
  call void @rt_pct_keyerr(i8* %kb)
  br label %ret
nomap:
  %nh3 = load i32, i32* getelementptr ([64 x i32], [64 x i32]* @rt.pct.argp, i32 0, i32 0)
  %nt3 = load i32, i32* getelementptr ([64 x i32], [64 x i32]* @rt.pct.argt, i32 0, i32 0)
  call void @rt_pct_needmapping(i32 %nh3, i32 %nt3)
  br label %ret
ok:
  call void @rt_pct_put(i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.pct, i32 0, i32 0))
  br label %ret
ret:
  ret void
}
`
}

// percentRuntimeEntry is rt_format itself, plus the one encoder the character conversion needs.
//
// The walk is deliberately dumb: find the next percent sign, copy the run before it, parse one
// conversion, emit it, continue. Everything that could go wrong is a write to the error register and a
// jump to the end, which is the same shape the module's other run-time doors use — and it is what makes
// a raise from inside a format land on the handler the program wrote rather than on a half-written
// answer (ADR 0212, ADR 0214).
func percentRuntimeEntry() string {
	return `
@rt.pct.utf = private global [8 x i8] zeroinitializer

; rt_pct_utf8 encodes one code point, and answers how many bytes it wrote. %c needs it because the
; reference turns an integer into a CHARACTER there, and a character is counted as one for padding
; however many bytes it costs.
define internal i32 @rt_pct_utf8(i32 %cp) {
entry:
  %lt128 = icmp ult i32 %cp, 128
  br i1 %lt128, label %one, label %two_chk
one:
  %b0 = trunc i32 %cp to i8
  %a0 = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 0
  store i8 %b0, i8* %a0
  ret i32 1
two_chk:
  %lt2 = icmp ult i32 %cp, 2048
  br i1 %lt2, label %two, label %three_chk
two:
  %s6 = lshr i32 %cp, 6
  %m6 = and i32 %s6, 31
  %t0 = or i32 %m6, 192
  %x6 = and i32 %cp, 63
  %t1 = or i32 %x6, 128
  %b10 = trunc i32 %t0 to i8
  %b11 = trunc i32 %t1 to i8
  %p10 = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 0
  %p11 = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 1
  store i8 %b10, i8* %p10
  store i8 %b11, i8* %p11
  ret i32 2
three_chk:
  %lt3 = icmp ult i32 %cp, 65536
  br i1 %lt3, label %three, label %four
three:
  %s12 = lshr i32 %cp, 12
  %m12 = and i32 %s12, 15
  %u0 = or i32 %m12, 224
  %s6b = lshr i32 %cp, 6
  %m6b = and i32 %s6b, 63
  %u1 = or i32 %m6b, 128
  %m6c = and i32 %cp, 63
  %u2 = or i32 %m6c, 128
  %b20 = trunc i32 %u0 to i8
  %b21 = trunc i32 %u1 to i8
  %b22 = trunc i32 %u2 to i8
  %q0 = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 0
  %q1 = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 1
  %q2 = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 2
  store i8 %b20, i8* %q0
  store i8 %b21, i8* %q1
  store i8 %b22, i8* %q2
  ret i32 3
four:
  %s18 = lshr i32 %cp, 18
  %m18 = and i32 %s18, 7
  %v0 = or i32 %m18, 240
  %s12c = lshr i32 %cp, 12
  %m12c = and i32 %s12c, 63
  %v1 = or i32 %m12c, 128
  %s6c = lshr i32 %cp, 6
  %m6c2 = and i32 %s6c, 63
  %v2 = or i32 %m6c2, 128
  %m6d = and i32 %cp, 63
  %v3 = or i32 %m6d, 128
  %c0 = trunc i32 %v0 to i8
  %c1 = trunc i32 %v1 to i8
  %c2 = trunc i32 %v2 to i8
  %c3 = trunc i32 %v3 to i8
  %r0 = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 0
  %r1 = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 1
  %r2 = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 2
  %r3 = getelementptr [8 x i8], [8 x i8]* @rt.pct.utf, i32 0, i32 3
  store i8 %c0, i8* %r0
  store i8 %c1, i8* %r1
  store i8 %c2, i8* %r2
  store i8 %c3, i8* %r3
  ret i32 4
}

; rt_pct_firstkey answers whether the first conversion of a format reads a key — the question the
; reference answers before it hands anything to anything, and the reason a keyed format followed
; by a positional conversion against a mapping is 'not enough arguments for format string' while a
; positional conversion first is the conversion's own complaint about a dict: a format that opens on a
; key leaves the positional conversions nothing to draw from, and a format that opens on a conversion
; takes the whole argument as its first.
define internal i32 @rt_pct_firstkey(i8* %p, i32 %n) {
entry:
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ %in, %ch ]
  %at = call i32 @rt_pct_scan(i8* %p, i32 %n, i32 %i, i32 37)
  %none = icmp sge i32 %at, %n
  br i1 %none, label %no, label %two
two:
  %j = add i32 %at, 1
  %past = icmp sge i32 %j, %n
  br i1 %past, label %no, label %ch
ch:
  %ja = getelementptr i8, i8* %p, i32 %j
  %c8 = load i8, i8* %ja
  %c = zext i8 %c8 to i32
  %isd = icmp eq i32 %c, 37
  %in = add i32 %j, 1
  br i1 %isd, label %loop, label %islp
islp:
  %lp = icmp eq i32 %c, 40
  %yes = zext i1 %lp to i32
  ret i32 %yes
no:
  ret i32 0
}

define internal i32 @rt_format(i32 %s, i32 %n, i32 %m) {
entry:
  store i32 %n, i32* @rt.pct.narg
  store i32 %m, i32* @rt.pct.map
  store i32 0, i32* @rt.pct.outlen
  store i32 0, i32* @rt.pct.err
  store i32 0, i32* @rt.pct.msglen
  store i32 0, i32* @rt.pct.ridx
  %bp = call i8* @rt_str_ptr(i32 %s)
  %ln = call i32 @rt_str_len(i32 %s)
  ; A mapping spends its one argument on the keyed conversions, and a format that opens on a key
  ; therefore has nothing left for the positional ones (roadmap Gap R.31).
  %fk = call i32 @rt_pct_firstkey(i8* %bp, i32 %ln)
  %isfk = icmp ne i32 %fk, 0
  %ism0 = icmp ne i32 %m, 0
  %empty = and i1 %ism0, %isfk
  %argcount = select i1 %empty, i32 0, i32 %n
  store i32 %argcount, i32* @rt.pct.narg
  br label %walk
walk:
  %i = phi i32 [ 0, %entry ], [ %nx, %again ]
  %at = call i32 @rt_pct_scan(i8* %bp, i32 %ln, i32 %i, i32 37)
  %gap = sub i32 %at, %i
  %has = icmp sgt i32 %gap, 0
  br i1 %has, label %lit, label %chk
lit:
  %pb = getelementptr i8, i8* %bp, i32 %i
  call void @rt_pct_putn(i8* %pb, i32 %gap)
  %oe = load i32, i32* @rt.pct.err
  %ob = icmp ne i32 %oe, 0
  br i1 %ob, label %fin, label %chk
chk:
  %noMore = icmp sge i32 %at, %ln
  br i1 %noMore, label %tail, label %parse
parse:
  %after = call i32 @rt_pct_parse(i8* %bp, i32 %ln, i32 %at)
  %pe = load i32, i32* @rt.pct.err
  %pbad = icmp ne i32 %pe, 0
  br i1 %pbad, label %tail, label %which
which:
  %cv = load i32, i32* @rt.pct.conv
  %isp = icmp eq i32 %cv, 37
  br i1 %isp, label %litpct, label %emit
litpct:
  store i32 0, i32* @rt.pct.keyused
  call void @rt_pct_percent()
  %e2 = load i32, i32* @rt.pct.err
  %b2 = icmp ne i32 %e2, 0
  br i1 %b2, label %tail, label %again
emit:
  call void @rt_pct_emit()
  %e3 = load i32, i32* @rt.pct.err
  %b3 = icmp ne i32 %e3, 0
  br i1 %b3, label %tail, label %again
again:
  %nx = load i32, i32* @rt.pct.hi
  br label %walk
tail:
  ; The reference checks the arguments it was handed and never spent, and only then; a mapping is spent
  ; by its first conversion, which is why the mapping form never trips this.
  %mp = load i32, i32* @rt.pct.map
  %ism = icmp ne i32 %mp, 0
  %e4 = load i32, i32* @rt.pct.err
  %already = icmp ne i32 %e4, 0
  %skip = or i1 %ism, %already
  br i1 %skip, label %fin, label %spare
spare:
  %r = load i32, i32* @rt.pct.ridx
  %na = load i32, i32* @rt.pct.narg
  %left = icmp slt i32 %r, %na
  br i1 %left, label %more, label %fin
more:
  call void @rt_pct_msg(i32 @CODE_TYPEERROR@, i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.type, i32 0, i32 0), i8* getelementptr inbounds ([64 x i8], [64 x i8]* @rt.pct.m.more, i32 0, i32 0))
  br label %fin
fin:
  %er = load i32, i32* @rt.pct.err
  %bad = icmp ne i32 %er, 0
  br i1 %bad, label %badout, label %good
good:
  %ol = load i32, i32* @rt.pct.outlen
  %z = getelementptr [32768 x i8], [32768 x i8]* @rt.pct.out, i32 0, i32 %ol
  store i8 0, i8* %z
  %nb = getelementptr [32768 x i8], [32768 x i8]* @rt.pct.out, i32 0, i32 0
  %idx = call i32 @rt_str_from_bytes(i8* %nb, i32 %ol)
  ret i32 %idx
badout:
  ret i32 -1
}
`
}

// percentRuntimeIR is the whole block: state, messages, sink, renderers, walker, conversions, and the
// walk — assembled once, the way the arithmetic door and the pair-fold door are (ADR 0171's generated
// constants, ADR 0258's one place per answer).
var percentRuntimeIR = percentRuntimeBlock()

func percentRuntimeBlock() string {
	// The kind tokens come from the one canonical table the whole runtime is spelled from
	// (canonicalValueTags, ADR 0171) — the door never hand-spells a tag number, which is how the
	// `%s`-prints-333 class of bug starts (ADR 0359). Only the exception codes and the door's own
	// limits are named here.
	rep := []string{
		"@OUTLIMIT@", strconv.Itoa(percentOutLimit),
		"@DIGITLIMIT@", strconv.Itoa(percentDigitLimit - 1),
		"@KEYLIMIT@", strconv.Itoa(percentKeyLimit - 1),
		"@CODE_TYPEERROR@", strconv.Itoa(exnClassCode("TypeError")),
		"@CODE_VALUEERROR@", strconv.Itoa(exnClassCode("ValueError")),
		"@CODE_KEYERROR@", strconv.Itoa(exnClassCode("KeyError")),
		"@CODE_OVERFLOWERROR@", strconv.Itoa(exnClassCode("OverflowError")),
		"@CODE_RUNTIMEERROR@", strconv.Itoa(exnClassCode("RuntimeError")),
	}
	rep = append(rep, percentKindTokens()...)
	body := percentRuntimeGlobals() + percentRuntimeMessages() + percentRuntimeSink() +
		percentRuntimeRenderers() + percentRuntimeParse() + percentRuntimeConversions() + percentRuntimeEntry()
	return strings.NewReplacer(rep...).Replace(body)
}

// percentKindTokens spells every kind constant the door compares against out of the canonical table:
// @TAG_LIST@ becomes whatever number value.go says a list handle's tag is, for every kind in the table.
// A door that compared `tag == 5` by hand would keep working the day the table moved and print the wrong
// kind's name forever; a door that reads the table cannot drift from it (ADR 0171's generated constants,
// ADR 0359's lesson about a kind that was read two ways).
func percentKindTokens() []string {
	all := ValueTagList()
	out := make([]string, 0, 2*len(all))
	for _, t := range all {
		out = append(out, "@TAG_"+strings.ToUpper(kindForTag(t))+"@", strconv.Itoa(int(t)))
	}
	return out
}

// percentConstIR declares one of the door's fixed byte tables at the size its own text fills, so the
// digit tables and the two snprintf formats carry the same generated guarantee the messages do: the
// declared length is derived from the literal, never counted by hand (ADR 0171).
func percentConstIR(name, text string) string {
	return fmt.Sprintf("@%s = private constant [%d x i8] c%s", name, len(text)+1, percentHexLiteral(text, len(text)+1))
}
