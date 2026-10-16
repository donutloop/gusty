// The rendering sink: rt_out_txt and the print doors that write an interned text, a value or a container to
// fd 1 (roadmap L11.2, ADR 0258). One block of heapRuntimeIRTmpl.
package lang

// heapIRSink is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRSink = `
; ---------------------------------------------------------------------------
; The rendering sink (roadmap L11.2, ADR 0258).
;
; Every value printer in this module writes through rt_out_txt / rt_out_int, which have exactly
; two destinations: the process's stdout, and — while str() or repr() is rendering — a capture
; buffer that comes back as an ordinary string value. One renderer with two sinks is what makes
; print(x), str(x) and the element of a container the same table instead of three separate
; guesses about how a value is written. Before this, print had a renderer and str() had a
; number-formatter, and every form the second one had not been told about came back as the
; number underneath: str([1, 2]) compiled answered 0 and str(None) answered 0, both with
; exit 0. A missing rendering is not allowed to become a number.
;
; The capture buffer is bounded, and a value that overflows it is not silently truncated: the
; length stops growing, and the str comes back short. A container bigger than 64 KB of text is
; beyond what this backend stores anyway (256 slots per object), and the bound is stated here
; rather than discovered in a test that happened to be small.
; ---------------------------------------------------------------------------
@rt_capturing = internal global i32 0
@rt_cap_len = internal global i32 0
@rt_cap = internal global [65536 x i8] zeroinitializer

; rt_out_txt writes a run-time text to the sink the renderer is pointed at.
define internal void @rt_out_txt(i8* %p) {
entry:
  %cap = load i32, i32* @rt_capturing
  %to_stdout = icmp eq i32 %cap, 0
  br i1 %to_stdout, label %emit, label %scan
emit:
  %r = call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmts, i32 0, i32 0), i8* %p)
  ret void
scan:
  %i = phi i32 [ 0, %entry ], [ %inext, %store ]
  %src = getelementptr i8, i8* %p, i32 %i
  %ch = load i8, i8* %src
  %done = icmp eq i8 %ch, 0
  br i1 %done, label %fin, label %copy
copy:
  %len = load i32, i32* @rt_cap_len
  %fits = icmp slt i32 %len, 65535
  br i1 %fits, label %store, label %fin
store:
  %dst = getelementptr [65536 x i8], [65536 x i8]* @rt_cap, i32 0, i32 %len
  store i8 %ch, i8* %dst
  %nlen = add i32 %len, 1
  store i32 %nlen, i32* @rt_cap_len
  %inext = add i32 %i, 1
  br label %scan
fin:
  ret void
}

; rt_out_int writes a number to the same sink: printf's %d, or the digits into the buffer.
define internal void @rt_out_int(i32 %v) {
entry:
  %cap = load i32, i32* @rt_capturing
  %to_stdout = icmp eq i32 %cap, 0
  br i1 %to_stdout, label %emit, label %capnum
emit:
  %r = call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmti, i32 0, i32 0), i32 %v)
  ret void
capnum:
  %buf = call ptr @malloc(i64 16)
  %n = call i32 (i8*, i32, i8*, ...) @snprintf(i8* %buf, i32 16, i8* getelementptr ([3 x i8], [3 x i8]* @.fmti, i32 0, i32 0), i32 %v)
  call void @rt_out_txt(i8* %buf)
  ret void
}

; rt_str_of_value is the str()/repr() half of the pair for one value: point the renderer at the
; buffer, ask the one printer this module has, and hand the text back as a string value. The tag
; says which arm of the printer runs; %quote is the pair's own question — a text writes its
; characters under str and its quoted repr under repr, and the caller is the only thing that
; knows which context it is in (ADR 0185's flag, now carrying the pair).
define internal i32 @rt_str_of_value(i32 %v, i32 %t, i32 %quote) {
entry:
  store i32 0, i32* @rt_cap_len
  store i32 1, i32* @rt_capturing
  call void @rt_print_mixed_value(i32 %v, i32 %t, i32 %quote)
  br label %taken
taken:
  %n = load i32, i32* @rt_cap_len
  store i32 0, i32* @rt_capturing
  %p = bitcast [65536 x i8]* @rt_cap to i8*
  %idx = call i32 @rt_str_from_bytes(i8* %p, i32 %n)
  ret i32 %idx
}

; rt_str_of_container asks a container object to render itself as a string. The object answers
; how its own slots are stored — plain numbers, interned text, or per-slot tags — which is why
; the question has to go to the printer rather than to whatever the builder happened to record.
define internal i32 @rt_str_of_container(i32 %h, i32 %quote) {
entry:
  store i32 0, i32* @rt_cap_len
  store i32 1, i32* @rt_capturing
  call void @rt_print_container_value(i32 %h, i32 %quote)
  br label %taken
taken:
  %n = load i32, i32* @rt_cap_len
  store i32 0, i32* @rt_capturing
  %p = bitcast [65536 x i8]* @rt_cap to i8*
  %idx = call i32 @rt_str_from_bytes(i8* %p, i32 %n)
  ret i32 %idx
}

; rt_print_str writes an interned string (an index into @str_tab) as its text, honouring the
; caller's newline flag: printing an element shows the characters, not the index.
define internal void @rt_print_str(i32 %idx, i32 %nl) {
entry:
  %sp = call i8* @rt_str_ptr(i32 %idx)
  call void @rt_out_txt(i8* %sp)
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}


; rt_print_value writes one container element. isStr says the value is an index into
; @str_tab, quote asks for Python's repr quoting (inside a container) rather than the raw
; text (print(x) of a single string). One helper keeps list, set and dict rendering
; identical, including the mixed cases where a dict has string keys and integer values.
define internal void @rt_print_value(i32 %v, i32 %isStr, i32 %quote) {
entry:
  %wantstr = icmp ne i32 %isStr, 0
  br i1 %wantstr, label %str, label %num
str:
  %sp = call i8* @rt_str_ptr(i32 %v)
  %q = icmp ne i32 %quote, 0
  br i1 %q, label %strq, label %strraw
strq:
  ; the repr slot already carries Python's chosen quotes, so it prints with a plain %s
  %rp = call i8* @rt_str_repr_ptr(i32 %v)
  call void @rt_out_txt(i8* %rp)
  ret void
strraw:
  call void @rt_out_txt(i8* %sp)
  ret void
num:
  call void @rt_out_int(i32 %v)
  ret void
}

; rt_set_print_str renders a set whose members are interned strings: set() when empty,
; {'a', 'b'} otherwise, matching the record's Repr.
define internal void @rt_set_print_str(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
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
  %ip = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %i
  %idx = load i32, i32* %ip
  %first = icmp eq i32 %i, 0
  br i1 %first, label %emit, label %sepd
sepd:
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  br label %emit
emit:
  call void @rt_print_value(i32 %idx, i32 1, i32 1)
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

; rt_dict_print_s renders a dict whose keys and/or values are interned strings; the two
; flags say which, so {'a': 1} and {1: 'a'} and {'a': 'b'} all print like the record.
define internal void @rt_dict_print_s(i32 %h, i32 %nl, i32 %ks, i32 %vs) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  call void @rt_out_txt(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtdopen, i32 0, i32 0))
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %more = icmp slt i32 %i, %len
  br i1 %more, label %body, label %done
body:
  %idx = mul i32 %i, 2
  %kp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx
  %k = load i32, i32* %kp
  %idx2 = add i32 %idx, 1
  %vp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx2
  %v = load i32, i32* %vp
  %first = icmp eq i32 %i, 0
  br i1 %first, label %emit, label %sepd
sepd:
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  br label %emit
emit:
  call void @rt_print_value(i32 %k, i32 %ks, i32 1)
  call void @rt_out_txt(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtcolon, i32 0, i32 0))
  call void @rt_print_value(i32 %v, i32 %vs, i32 1)
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
`
