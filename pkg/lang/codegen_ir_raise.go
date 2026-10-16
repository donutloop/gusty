// codegen_ir_raise.go holds the raise side's runtime block: the one table that names an exception class
// for a raise the compiler emits, and the block that reports an uncaught exception the way the record does
// — a traceback header and the exception line — on stderr (fd 2, via write so it links on every platform
// without depending on a libc `stderr` symbol) before main returns 1. The block is text with frame-caption
// references filled by frameCaptionRefs (ADR 0355).
package lang

import (
	"strings"
)

func exnCode(name string) int {
	// The canonical list (and its codes) lives in exceptions.go, shared with the
	// record's isExnClass and the checker's name table.
	return exnClassCode(name)
}

// raiseRuntimeIR is the raise block as shipped: the template below with the frame-report captions
// filled in from the strings the frame stack declares. The substitution exists because an LLVM
// getelementptr has to state the array type it indexes, and a length typed twice is a length that
// can disagree with itself (ADR 0171, and frameCaptionRefs).
var raiseRuntimeIR = strings.NewReplacer(frameCaptionRefs()...).Replace(raiseRuntimeIRTemplate)

const raiseRuntimeIRTemplate = `
declare i64 @strlen(i8*)

; @exn_msg travels with the flag: a raise statement and a compiler-generated raise
; (an out-of-bounds item assignment) both store a "<Type>: <message>" string here, so
; the uncaught path can report what it was. A raise that never ran leaves it null and
; rt_die prints a generic line. The block is emitted only for programs that can raise.
@exn_msg = internal global i8* null
@exn_frame = internal global i8* null

@.rt_die.hdr = private unnamed_addr constant [36 x i8] c"Traceback (most recent call last):\0A\00"
@.rt_die.none = private unnamed_addr constant [26 x i8] c"gusty: uncaught exception\00"
@.rt_die.nl = private unnamed_addr constant [2 x i8] c"\0A\00"

define internal void @rt_die(i8* %msg) {
entry:
  %nlpre = getelementptr inbounds [2 x i8], [2 x i8]* @.rt_die.nl, i32 0, i32 0
  %hdr = getelementptr inbounds [36 x i8], [36 x i8]* @.rt_die.hdr, i32 0, i32 0
  %hlen = call i64 @strlen(i8* %hdr)
  call i64 @write(i32 2, i8* %hdr, i64 %hlen)
  ; Every frame the raise travelled through, outermost first, the way the reference prints
  ; them (roadmap Gap K.8, ADR 0354). The frames come from the snapshot the raise took, not
  ; from the live stack: by here every one of them has returned.
  %n = load i32, i32* @rt.frames.tb.n
  %any = icmp sgt i32 %n, 0
  br i1 %any, label %walk, label %oneframe
walk:
  %i = phi i32 [ 0, %entry ], [ %nx, %step ], [ %sx, %skip ]
  %lp = getelementptr inbounds [64 x i32], [64 x i32]* @rt.frames.tb.line, i32 0, i32 %i
  %line = load i32, i32* %lp
  %known = icmp sgt i32 %line, 0
  br i1 %known, label %frame, label %skip
frame:
  %pre = %%PRE%%
  %plen = call i64 @strlen(i8* %pre)
  call i64 @write(i32 2, i8* %pre, i64 %plen)
  %ds = call i8* @rt_frame_digits(i32 %line)
  %dlen = call i64 @strlen(i8* %ds)
  call i64 @write(i32 2, i8* %ds, i64 %dlen)
  %mid = %%MID%%
  %mlen = call i64 @strlen(i8* %mid)
  call i64 @write(i32 2, i8* %mid, i64 %mlen)
  %np = getelementptr inbounds [64 x i8*], [64 x i8*]* @rt.frames.tb.name, i32 0, i32 %i
  %nm = load i8*, i8** %np
  %noname = icmp eq i8* %nm, null
  %unk = %%UNK%%
  %use = select i1 %noname, i8* %unk, i8* %nm
  %nlen = call i64 @strlen(i8* %use)
  call i64 @write(i32 2, i8* %use, i64 %nlen)
  call i64 @write(i32 2, i8* %nlpre, i64 1)
  ; Three copies of a frame and a count of the rest, which is what the reference prints for a
  ; recursion; the count was made when the snapshot was taken, not here.
  %cp = getelementptr inbounds [64 x i32], [64 x i32]* @rt.frames.tb.coll, i32 0, i32 %i
  %spared = load i32, i32* %cp
  %hidden = icmp sgt i32 %spared, 0
  br i1 %hidden, label %repeated, label %step
repeated:
  %rp = %%REP%%
  %rlen = call i64 @strlen(i8* %rp)
  call i64 @write(i32 2, i8* %rp, i64 %rlen)
  %rds = call i8* @rt_frame_digits(i32 %spared)
  %rdlen = call i64 @strlen(i8* %rds)
  call i64 @write(i32 2, i8* %rds, i64 %rdlen)
  %isonce = icmp eq i32 %spared, 1
  %r1 = %%REP1%%
  %rN = %%REPN%%
  %tail = select i1 %isonce, i8* %r1, i8* %rN
  %tlen = call i64 @strlen(i8* %tail)
  call i64 @write(i32 2, i8* %tail, i64 %tlen)
  br label %step
step:
  %nx = add i32 %i, 1
  %nxmore = icmp slt i32 %nx, %n
  br i1 %nxmore, label %walk, label %message
skip:
  %sx = add i32 %i, 1
  %sxmore = icmp slt i32 %sx, %n
  br i1 %sxmore, label %walk, label %message
; Nothing was named — a raise whose statement carried no position, in a program that never
; called. The single frame the raise site spelled out is better than no frame at all.
oneframe:
  %fp = load i8*, i8** @exn_frame
  %hasframe = icmp ne i8* %fp, null
  br i1 %hasframe, label %withframe, label %message
withframe:
  %flen = call i64 @strlen(i8* %fp)
  call i64 @write(i32 2, i8* %fp, i64 %flen)
  call i64 @write(i32 2, i8* %nlpre, i64 1)
  br label %message
message:
  %isnull = icmp eq i8* %msg, null
  %none = getelementptr inbounds [26 x i8], [26 x i8]* @.rt_die.none, i32 0, i32 0
  %m = select i1 %isnull, i8* %none, i8* %msg
  %len = call i64 @strlen(i8* %m)
  call i64 @write(i32 2, i8* %m, i64 %len)
  call i64 @write(i32 2, i8* %nlpre, i64 1)
  ret void
}
`
