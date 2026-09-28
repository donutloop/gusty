package lang

import (
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// GenerateIR produces LLVM IR text for prog (deterministic, no native LLVM).
const heapRuntimeIR = `@heap_count = internal global i32 0
; Cached handle for the None singleton (heap kind 4). None cannot be an immediate the way
; ints are: any i32 value is a legal integer, so no bit pattern is free to mean "no value".
; The handle is allocated once and never freed, so "x = None" and "x == None" behave the
; same in the compiled backend and the interpreter (ADR 0172).
@none_h = internal global i32 -1
@gc_mark = internal global [1024 x i8] zeroinitializer
@gc_urgent = internal global i32 0
@free_head = internal global i32 -1
@free_next = internal global [1024 x i32] zeroinitializer
@heap = internal global [1024 x {i32, i32, [256 x i32]}] zeroinitializer

define internal i32 @rt_alloc(i32 %kind) {
entry:
  %fh = load i32, i32* @free_head
  %isneg = icmp slt i32 %fh, 0
  br i1 %isneg, label %alloc_new, label %alloc_reuse
alloc_new:
  %c = load i32, i32* @heap_count
  %oob = icmp sge i32 %c, 1024
  br i1 %oob, label %full, label %newok
full:
  ret i32 -1
newok:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %c
  %kp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 0
  store i32 %kind, i32* %kp
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  store i32 0, i32* %lp
  %c1 = add i32 %c, 1
  store i32 %c1, i32* @heap_count
  ret i32 %c
alloc_reuse:
  %rn = getelementptr [1024 x i32], [1024 x i32]* @free_next, i32 0, i32 %fh
  %next = load i32, i32* %rn
  store i32 %next, i32* @free_head
  %obj2 = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %fh
  %kp2 = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj2, i32 0, i32 0
  store i32 %kind, i32* %kp2
  %lp2 = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj2, i32 0, i32 1
  store i32 0, i32* %lp2
  ret i32 %fh
}

define internal void @rt_free(i32 %h) {
entry:
  %fh = load i32, i32* @free_head
  %fn = getelementptr [1024 x i32], [1024 x i32]* @free_next, i32 0, i32 %h
  store i32 %fh, i32* %fn
  store i32 %h, i32* @free_head
  ret void
}
define internal void @rt_set_elem(i32 %h, i32 %i, i32 %v) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  store i32 %v, i32* %ep
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %len1 = add i32 %len, 1
  store i32 %len1, i32* %lp
  ret void
}

; list element replacement: xs[i] = v writes elems[i] in place. Unlike
; rt_set_elem this must NOT bump the length, which is what appends do.
define internal void @rt_put_elem(i32 %h, i32 %i, i32 %v) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  store i32 %v, i32* %ep
  ret void
}

define internal i32 @rt_list_len(i32 %h) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  ret i32 %len
}

define internal i32 @rt_get_elem(i32 %h, i32 %i) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  %v = load i32, i32* %ep
  ret i32 %v
}

; rt_pop removes element %i and returns it, shifting the tail left and shrinking the
; length. The caller bounds-checks (an out-of-range index raises IndexError), and the
; index is already normalised for negatives.
define internal i32 @rt_pop(i32 %h, i32 %i) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  %ip = getelementptr [256 x i32], [256 x i32]* %ep, i32 0, i32 %i
  %v = load i32, i32* %ip
  %last = sub i32 %len, 1
  br label %shift
shift:
  %j = phi i32 [ %i, %entry ], [ %jnext, %shiftdo ]
  %more = icmp slt i32 %j, %last
  br i1 %more, label %shiftdo, label %done
shiftdo:
  %src = add i32 %j, 1
  %sp = getelementptr [256 x i32], [256 x i32]* %ep, i32 0, i32 %src
  %sv = load i32, i32* %sp
  %dp = getelementptr [256 x i32], [256 x i32]* %ep, i32 0, i32 %j
  store i32 %sv, i32* %dp
  %jnext = add i32 %j, 1
  br label %shift
done:
  store i32 %last, i32* %lp
  ret i32 %v
}

define internal void @rt_append(i32 %h, i32 %v) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %len
  store i32 %v, i32* %ep
  %len1 = add i32 %len, 1
  store i32 %len1, i32* %lp
  ret void
}

@.fmtlopen = private unnamed_addr constant [2 x i8] c"[\00"
@.fmtsep = private unnamed_addr constant [3 x i8] c", \00"
@.fmtlclose = private unnamed_addr constant [2 x i8] c"]\00"
@.fmti = private unnamed_addr constant [3 x i8] c"%d\00"
@.fmtnl = private unnamed_addr constant [2 x i8] c"\0A\00"
@.fmtnone = private unnamed_addr constant [5 x i8] c"None\00"
@.fmtq = private unnamed_addr constant [5 x i8] c"'%s'\00"
@.fmts = private unnamed_addr constant [3 x i8] c"%s\00"
@.fmtcolon = private unnamed_addr constant [3 x i8] c": \00"
@str_count = internal global i32 0
@str_tab = internal global [256 x i8*] zeroinitializer
; Parallel table holding each interned string's Python repr form (quoted, with the quote
; character chosen the way Python chooses it). Containers store the index; printing inside a
; container uses the repr slot, printing a single value uses the raw text.
@str_repr_tab = internal global [256 x i8*] zeroinitializer
; Per-object element-kind flags, indexed by heap handle: bit 0 = elements are interned
; strings, bit 1 = dict keys are, bit 2 = dict values are. Whether a container holds strings
; is a property of the *object*, not of the variable — a helper can fill a list its caller
; created — so the printers read this instead of trusting a static guess (Gap I.2/J.5).
@estr = internal global [1024 x i32] zeroinitializer
declare i32 @strcmp(i8*, i8*)

; Strings are compile-time globals, so a container slot cannot hold one directly (it is an
; i32). The runtime keeps a table of the string texts it has been shown and hands back the
; index; containers store that index, and rt_str_ptr reads the text back when printing or
; comparing. Indices are content-addressed (strcmp), so two spellings of the same text are
; the same key (roadmap Gap I.2).
define internal i32 @rt_str_intern(i8* %p) {
entry:
  %n0 = load i32, i32* @str_count
  br label %scan
scan:
  %i = phi i32 [ 0, %entry ], [ %inext, %next ]
  %more = icmp slt i32 %i, %n0
  br i1 %more, label %cmp, label %add
cmp:
  %slot = getelementptr [256 x i8*], [256 x i8*]* @str_tab, i32 0, i32 %i
  %q = load i8*, i8** %slot
  %r = call i32 @strcmp(i8* %p, i8* %q)
  %same = icmp eq i32 %r, 0
  br i1 %same, label %found, label %next
next:
  %inext = add i32 %i, 1
  br label %scan
found:
  ret i32 %i
add:
  %oob = icmp sge i32 %n0, 256
  br i1 %oob, label %full, label %put
put:
  %slot2 = getelementptr [256 x i8*], [256 x i8*]* @str_tab, i32 0, i32 %n0
  store i8* %p, i8** %slot2
  %n1 = add i32 %n0, 1
  store i32 %n1, i32* @str_count
  ret i32 %n0
full:
  ; Out of table space: reuse the last entry rather than returning a wild index.
  %last = sub i32 %n0, 1
  ret i32 %last
}

; rt_mark_estr records which positions of a container hold interned strings. It ORs, because a
; dict can gain string keys and later string values.
define internal void @rt_mark_estr(i32 %h, i32 %bits) {
entry:
  %slot = getelementptr [1024 x i32], [1024 x i32]* @estr, i32 0, i32 %h
  %old = load i32, i32* %slot
  %both = or i32 %old, %bits
  store i32 %both, i32* %slot
  ret void
}

define internal i8* @rt_str_ptr(i32 %i) {
entry:
  %slot = getelementptr [256 x i8*], [256 x i8*]* @str_tab, i32 0, i32 %i
  %p = load i8*, i8** %slot
  ret i8* %p
}
; rt_str_contains tests substring membership of two interned strings: needle and
; haystack are both @str_tab indices, never raw pointers.
; reaches here when the haystack is a string value rather than a container, and a string value
; is a @str_tab index (ADR 0173). The earlier path handed the raw @.strN global to
; rt_contains(i32, i32), which is not valid IR — a global in an i32 slot — so the module died
; in llc and the failure looked like a compiler bug (ADR 0166). The scan is written in IR
; rather than calling libc strstr, which would need a second declaration of a libc symbol the
; runtime may already declare with a different signature (the strlen lesson, ADR 0173).
define internal i32 @rt_str_contains(i32 %hay, i32 %needle) {
entry:
  %hp = call i8* @rt_str_ptr(i32 %hay)
  %np = call i8* @rt_str_ptr(i32 %needle)
  %n0 = load i8, i8* %np
  %empty = icmp eq i8 %n0, 0
  br i1 %empty, label %hit, label %outer
outer:
  %i = phi i32 [ 0, %entry ], [ %inext, %nomatch ]
  %ih = getelementptr i8, i8* %hp, i32 %i
  %hc = load i8, i8* %ih
  %done = icmp eq i8 %hc, 0
  br i1 %done, label %miss, label %inner
inner:
  %j = phi i32 [ 0, %outer ], [ %jnext, %innercont ]
  %ij = add i32 %i, %j
  %hjp = getelementptr i8, i8* %hp, i32 %ij
  %hjc = load i8, i8* %hjp
  %njp = getelementptr i8, i8* %np, i32 %j
  %njc = load i8, i8* %njp
  %nst = icmp eq i8 %njc, 0
  br i1 %nst, label %hit, label %cmp
cmp:
  %same = icmp eq i8 %hjc, %njc
  br i1 %same, label %innercont, label %nomatch
innercont:
  %jnext = add i32 %j, 1
  br label %inner
nomatch:
  %inext = add i32 %i, 1
  br label %outer
hit:
  ret i32 1
miss:
  ret i32 0
}

; rt_str_len measures an interned string: len(s) where s is a parameter or an element read
; out of a container has no compile-time text to fold, unlike a literal (Gap J.5). It counts
; bytes directly rather than calling strlen, because another runtime helper already declares
; strlen with an i64 return and LLVM keys declarations by name.
define internal i32 @rt_str_len(i32 %i) {
entry:
  %p = call i8* @rt_str_ptr(i32 %i)
  br label %scan
scan:
  %n = phi i32 [ 0, %entry ], [ %nnext, %step ]
  %at = getelementptr i8, i8* %p, i32 %n
  %ch = load i8, i8* %at
  %done = icmp eq i8 %ch, 0
  br i1 %done, label %fin, label %step
step:
  %nnext = add i32 %n, 1
  br label %scan
fin:
  ret i32 %n
}

define internal i8* @rt_str_repr_ptr(i32 %i) {
entry:
  %slot = getelementptr [256 x i8*], [256 x i8*]* @str_repr_tab, i32 0, i32 %i
  %p = load i8*, i8** %slot
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
  %slot = getelementptr [256 x i8*], [256 x i8*]* @str_tab, i32 0, i32 %i
  %q = load i8*, i8** %slot
  %r = call i32 @strcmp(i8* %raw, i8* %q)
  %same = icmp eq i32 %r, 0
  br i1 %same, label %found, label %next
next:
  %inext = add i32 %i, 1
  br label %scan
found:
  %rs = getelementptr [256 x i8*], [256 x i8*]* @str_repr_tab, i32 0, i32 %i
  store i8* %repr, i8** %rs
  ret i32 %i
add:
  %oob = icmp sge i32 %n0, 256
  br i1 %oob, label %full, label %put
put:
  %slot2 = getelementptr [256 x i8*], [256 x i8*]* @str_tab, i32 0, i32 %n0
  store i8* %raw, i8** %slot2
  %rs2 = getelementptr [256 x i8*], [256 x i8*]* @str_repr_tab, i32 0, i32 %n0
  store i8* %repr, i8** %rs2
  %n1 = add i32 %n0, 1
  store i32 %n1, i32* @str_count
  ret i32 %n0
full:
  %last = sub i32 %n0, 1
  ret i32 %last
}

; rt_print_str writes an interned string (an index into @str_tab) as its text, honouring the
; caller's newline flag: printing an element shows the characters, not the index.
define internal void @rt_print_str(i32 %idx, i32 %nl) {
entry:
  %sp = call i8* @rt_str_ptr(i32 %idx)
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmts, i32 0, i32 0), i8* %sp)
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
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
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmts, i32 0, i32 0), i8* %rp)
  ret void
strraw:
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmts, i32 0, i32 0), i8* %sp)
  ret void
num:
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmti, i32 0, i32 0), i32 %v)
  ret void
}

; rt_set_print_str renders a set whose members are interned strings: set() when empty,
; {'a', 'b'} otherwise, matching the interpreter's Repr.
define internal void @rt_set_print_str(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  %isEmpty = icmp eq i32 %len, 0
  br i1 %isEmpty, label %empty, label %open
empty:
  call i32 (i8*, ...) @printf(i8* getelementptr ([6 x i8], [6 x i8]* @.fmtemptyset, i32 0, i32 0))
  br label %fin
open:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtsopen, i32 0, i32 0))
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
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  br label %emit
emit:
  call void @rt_print_value(i32 %idx, i32 1, i32 1)
  br label %cont
cont:
  %next = add i32 %i, 1
  br label %loop
done:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtsclose, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}

; rt_dict_print_s renders a dict whose keys and/or values are interned strings; the two
; flags say which, so {'a': 1} and {1: 'a'} and {'a': 'b'} all print like the interpreter.
define internal void @rt_dict_print_s(i32 %h, i32 %nl, i32 %ks, i32 %vs) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtdopen, i32 0, i32 0))
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
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  br label %emit
emit:
  call void @rt_print_value(i32 %k, i32 %ks, i32 1)
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtcolon, i32 0, i32 0))
  call void @rt_print_value(i32 %v, i32 %vs, i32 1)
  br label %cont
cont:
  %next = add i32 %i, 1
  br label %loop
done:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtsclose, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}

; rt_print_list_str renders a list whose elements are interned strings, with Python's
; quoting: ['a', 'b'].
define internal void @rt_print_list_str(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtlopen, i32 0, i32 0))
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %more = icmp slt i32 %i, %len
  br i1 %more, label %body, label %done
body:
  %ip = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %i
  %idx = load i32, i32* %ip
  %first = icmp eq i32 %i, 0
  br i1 %first, label %emit, label %sepd
sepd:
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  br label %emit
emit:
  call void @rt_print_value(i32 %idx, i32 1, i32 1)
  br label %cont
cont:
  %next = add i32 %i, 1
  br label %loop
done:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtlclose, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}

; rt_none returns the None singleton handle, allocating it on first use.
define internal i32 @rt_none() {
entry:
  %cached = load i32, i32* @none_h
  %have = icmp ne i32 %cached, -1
  br i1 %have, label %hit, label %make
make:
  %h = call i32 @rt_alloc(i32 4)
  store i32 %h, i32* @none_h
  ret i32 %h
hit:
  ret i32 %cached
}

; rt_none_len is the container length of the None object (always 0), so a generic
; length query on a None handle reports 0 instead of reading an uninitialised word.
define internal i32 @rt_is_none(i32 %h) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %kp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 0
  %k = load i32, i32* %kp
  %isnone = icmp eq i32 %k, 4
  %r = zext i1 %isnone to i32
  ret i32 %r
}

; rt_print_none writes "None", honouring the caller's newline flag like every other
; runtime printer (ADR 0165: the printer does not own the terminator).
define internal void @rt_print_none(i32 %nl) {
entry:
  call i32 (i8*, ...) @printf(i8* getelementptr ([5 x i8], [5 x i8]* @.fmtnone, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}

define internal void @rt_print_list(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  ; Whether the elements are interned strings is read from the object, not from a static
  ; guess: a helper like "def fill(out, v): out.append(v)" can fill a list its caller created,
  ; and the caller's scope has no idea what the callee stored (Gap J.5).
  %fsp = getelementptr [1024 x i32], [1024 x i32]* @estr, i32 0, i32 %h
  %flags = load i32, i32* %fsp
  %eb = and i32 %flags, 1
  %isStr = icmp ne i32 %eb, 0
  %istr = zext i1 %isStr to i32
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtlopen, i32 0, i32 0))
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ %i1, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %done
body:
  %is0 = icmp eq i32 %i, 0
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  %e = load i32, i32* %ep
  br i1 %is0, label %first, label %sep
first:
  call void @rt_print_value(i32 %e, i32 %istr, i32 1)
  br label %cont
sep:
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  call void @rt_print_value(i32 %e, i32 %istr, i32 1)
  br label %cont
cont:
  %i1 = add i32 %i, 1
  br label %loop
done:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtlclose, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}

@.fmtdopen = private unnamed_addr constant [2 x i8] c"{\00"
@.fmtditem = private unnamed_addr constant [7 x i8] c"%d: %d\00"
@.fmtdsep = private unnamed_addr constant [3 x i8] c", \00"
@.fmtdclose = private unnamed_addr constant [2 x i8] c"}\00"

define internal void @rt_dict_put(i32 %h, i32 %k, i32 %v) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
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

define internal i32 @rt_contains(i32 %h, i32 %v) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
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
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
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

define internal i32 @rt_dict_len(i32 %h) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  ret i32 %len
}

; rt_dict_print asks the object which of its positions hold interned strings and delegates:
; bit 1 = keys, bit 2 = values. A static map cannot answer that question when the dict was
; filled inside a helper that received it as a parameter (roadmap Gap J.5).
define internal void @rt_dict_print(i32 %h, i32 %nl) {
entry:
  %fsp = getelementptr [1024 x i32], [1024 x i32]* @estr, i32 0, i32 %h
  %flags = load i32, i32* %fsp
  %kb = and i32 %flags, 2
  %ksb = icmp ne i32 %kb, 0
  %ksi = zext i1 %ksb to i32
  %vb = and i32 %flags, 4
  %vsb = icmp ne i32 %vb, 0
  %vsi = zext i1 %vsb to i32
  call void @rt_dict_print_s(i32 %h, i32 %nl, i32 %ksi, i32 %vsi)
  ret void
}


@.fmtemptyset = private unnamed_addr constant [6 x i8] c"set()\00"
@.fmtsopen = private unnamed_addr constant [2 x i8] c"{\00"
@.fmtsitem = private unnamed_addr constant [3 x i8] c"%d\00"
@.fmtssep = private unnamed_addr constant [3 x i8] c", \00"
@.fmtsclose = private unnamed_addr constant [2 x i8] c"}\00"

define internal void @rt_set_add(i32 %h, i32 %v) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
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

; rt_set_discard removes a value if present and shifts the tail left; removing an absent
; value is a no-op (that is what distinguishes discard from remove).
define internal void @rt_set_clear(i32 %h) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  store i32 0, i32* %lp
  ret void
}

define internal void @rt_set_discard(i32 %h, i32 %v) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  ; Computed here so it dominates the done block, which is reachable both when the
  ; value is absent (scan -> done) and after the shift loop.
  %last = sub i32 %len, 1
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  br label %scan
scan:
  %i = phi i32 [ 0, %entry ], [ %inext, %step ]
  %more = icmp slt i32 %i, %len
  br i1 %more, label %body, label %done
body:
  %p = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %i
  %x = load i32, i32* %p
  %hit = icmp eq i32 %x, %v
  br i1 %hit, label %remove, label %step
step:
  %inext = add i32 %i, 1
  br label %scan
remove:
  br label %shift
shift:
  %j = phi i32 [ %i, %remove ], [ %jnext, %shiftdo ]
  %go = icmp slt i32 %j, %last
  br i1 %go, label %shiftdo, label %done
shiftdo:
  %src = add i32 %j, 1
  %sp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %src
  %sv = load i32, i32* %sp
  %dpp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %j
  store i32 %sv, i32* %dpp
  %jnext = add i32 %j, 1
  br label %shift
done:
  store i32 %last, i32* %lp
  ret void
}

define internal i32 @rt_set_len(i32 %h) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  ret i32 %len
}

define internal void @rt_set_print(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  ; members are interned strings when the object says so (see rt_mark_estr), not when a
  ; static guess in this scope said so (roadmap Gap J.5)
  %fsp = getelementptr [1024 x i32], [1024 x i32]* @estr, i32 0, i32 %h
  %flags = load i32, i32* %fsp
  %mb = and i32 %flags, 1
  %msb = icmp ne i32 %mb, 0
  %mstr = zext i1 %msb to i32
  ; Python renders the empty set as set(), not {} (which is a dict); the interpreter's
  ; Repr already does this, so the compiled renderer must agree or printing an empty set
  ; differs between the backends.
  %isEmpty = icmp eq i32 %len, 0
  br i1 %isEmpty, label %empty, label %open
empty:
  call i32 (i8*, ...) @printf(i8* getelementptr ([6 x i8], [6 x i8]* @.fmtemptyset, i32 0, i32 0))
  %wantnl0 = icmp ne i32 %nl, 0
  br i1 %wantnl0, label %eol0, label %fin0
eol0:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin0
fin0:
  ret void
open:
  %r1 = call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtsopen, i32 0, i32 0))
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
  %r3 = call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtssep, i32 0, i32 0))
  br label %cont
cont:
  %next = phi i32 [ %i1, %body ], [ %i1, %sep ]
  br label %check
done:
  %r4 = call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtsclose, i32 0, i32 0))
  %wantnl = icmp ne i32 %nl, 0
  br i1 %wantnl, label %eol, label %fin
eol:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  br label %fin
fin:
  ret void
}

define internal i32 @rt_inst_get(i32 %h, i32 %slot) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %p = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %slot
  %v = load i32, i32* %p
  ret i32 %v
}

define internal void @rt_inst_put(i32 %h, i32 %slot, i32 %v) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %p = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %slot
  store i32 %v, i32* %p
  ret void
}

define internal i32 @rt_gc_mark(i32 %h) {
entry:
  %neg = icmp slt i32 %h, 0
  %hc = load i32, i32* @heap_count
  %big = icmp sge i32 %h, %hc
  %bad = or i1 %neg, %big
  br i1 %bad, label %ret0, label %chk
chk:
  %m = getelementptr [1024 x i8], [1024 x i8]* @gc_mark, i32 0, i32 %h
  %mv = load i8, i8* %m
  %is = icmp eq i8 %mv, 0
  br i1 %is, label %mark, label %ret0
mark:
  store i8 1, i8* %m
  br label %ret1
ret1:
  ret i32 1
ret0:
  ret i32 0
}

define internal void @rt_gc([4096 x i32*]* %roots, i32 %nroots) {
entry:
  %c0 = load i32, i32* @gc.stat_collections
  %c1 = add i32 %c0, 1
  store i32 %c1, i32* @gc.stat_collections
  store i32 0, i32* @gc.stat_freed
  store i32 0, i32* @gc.stat_roots
  store i32 0, i32* @gc.stat_skipped
  store i32 0, i32* @gc.stat_live
  br label %cl.loop
cl.loop:
  %i = phi i32 [ 0, %entry ], [ %i.nxt, %cl.inc ]
  %i.end = icmp sge i32 %i, 1024
  br i1 %i.end, label %fl.init, label %cl.body
cl.body:
  %cm = getelementptr [1024 x i8], [1024 x i8]* @gc_mark, i32 0, i32 %i
  store i8 0, i8* %cm
  br label %cl.inc
cl.inc:
  %i.nxt = add i32 %i, 1
  br label %cl.loop
fl.init:
  %fh0 = load i32, i32* @free_head
  br label %fl.loop
fl.loop:
  %fh = phi i32 [ %fh0, %fl.init ], [ %fn, %fl.body ]
  %fh.end = icmp slt i32 %fh, 0
  br i1 %fh.end, label %roots.init, label %fl.body
fl.body:
  %fm = getelementptr [1024 x i8], [1024 x i8]* @gc_mark, i32 0, i32 %fh
  store i8 2, i8* %fm
  %fnp = getelementptr [1024 x i32], [1024 x i32]* @free_next, i32 0, i32 %fh
  %fn = load i32, i32* %fnp
  br label %fl.loop
roots.init:
  br label %roots.loop
roots.loop:
  %j = phi i32 [ 0, %roots.init ], [ %j.nxt, %roots.inc ]
  %j.end = icmp sge i32 %j, %nroots
  br i1 %j.end, label %pass.init, label %roots.body
roots.body:
  ; A slot tagged 0 holds a raw value (or nothing): it is *not scanned*, which is
  ; the whole point of precise roots. Counted separately so --gc-stats can show
  ; how much precision buys (ADR 0181).
  %kp = getelementptr [4096 x i8], [4096 x i8]* @gc.kinds, i32 0, i32 %j
  %kv = load i8, i8* %kp
  %isHandle = icmp ne i8 %kv, 0
  br i1 %isHandle, label %roots.check, label %roots.notHandle
roots.notHandle:
  %sk0 = load i32, i32* @gc.stat_skipped
  %sk1 = add i32 %sk0, 1
  store i32 %sk1, i32* @gc.stat_skipped
  br label %roots.skip
roots.check:
  %rp = getelementptr [4096 x i32*], [4096 x i32*]* %roots, i32 0, i32 %j
  %rpp = load i32*, i32** %rp
  %rnull = icmp eq i32* %rpp, null
  br i1 %rnull, label %roots.skip, label %roots.mark
roots.mark:
  %rs0 = load i32, i32* @gc.stat_roots
  %rs1 = add i32 %rs0, 1
  store i32 %rs1, i32* @gc.stat_roots
  %rh = load i32, i32* %rpp
  call void @rt_gc_mark(i32 %rh)
  br label %roots.skip
roots.skip:
  br label %roots.inc
roots.inc:
  %j.nxt = add i32 %j, 1
  br label %roots.loop
pass.init:
  br label %pass.loop
pass.loop:
  %p = phi i32 [ 0, %pass.init ], [ %p.nxt, %pass.inc ]
  %hc1 = load i32, i32* @heap_count
  %p.end = icmp sge i32 %p, %hc1
  br i1 %p.end, label %sweep.init, label %k.init
k.init:
  br label %k.loop
k.loop:
  %k = phi i32 [ 0, %k.init ], [ %k.nxt, %k.inc ]
  %k.end = icmp sge i32 %k, %hc1
  br i1 %k.end, label %pass.inc, label %k.body
k.body:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %k
  %km = getelementptr [1024 x i8], [1024 x i8]* @gc_mark, i32 0, i32 %k
  %kmv = load i8, i8* %km
  %kmk = icmp eq i8 %kmv, 1
  br i1 %kmk, label %e.init, label %k.inc
e.init:
  br label %e.loop
e.loop:
  %e = phi i32 [ 0, %e.init ], [ %e.nxt, %e.inc ]
  %lenp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lenp
  %e.end = icmp sge i32 %e, %len
  br i1 %e.end, label %k.inc, label %e.body
e.body:
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %e
  %ev = load i32, i32* %ep
  call void @rt_gc_mark(i32 %ev)
  br label %e.inc
e.inc:
  %e.nxt = add i32 %e, 1
  br label %e.loop
k.inc:
  %k.nxt = add i32 %k, 1
  br label %k.loop
pass.inc:
  %p.nxt = add i32 %p, 1
  br label %pass.loop
sweep.init:
  br label %sw.loop
sw.loop:
  %w = phi i32 [ 0, %sweep.init ], [ %w.nxt, %sw.inc ]
  %hc2 = load i32, i32* @heap_count
  %w.end = icmp sge i32 %w, %hc2
  br i1 %w.end, label %done, label %sw.body
sw.body:
  %wm = getelementptr [1024 x i8], [1024 x i8]* @gc_mark, i32 0, i32 %w
  %wmv = load i8, i8* %wm
  %walive = icmp eq i8 %wmv, 1
  br i1 %walive, label %sw.live, label %sw.check
sw.live:
  %lv0 = load i32, i32* @gc.stat_live
  %lv1 = add i32 %lv0, 1
  store i32 %lv1, i32* @gc.stat_live
  br label %sw.check
sw.check:
  %wfree = icmp eq i8 %wmv, 0
  br i1 %wfree, label %sw.free, label %sw.inc
sw.free:
  %fr0 = load i32, i32* @gc.stat_freed
  %fr1 = add i32 %fr0, 1
  store i32 %fr1, i32* @gc.stat_freed
  %ft0 = load i32, i32* @gc.stat_total_freed
  %ft1 = add i32 %ft0, 1
  store i32 %ft1, i32* @gc.stat_total_freed
  %fhp = getelementptr [1024 x i32], [1024 x i32]* @free_next, i32 0, i32 %w
  %fhc = load i32, i32* @free_head
  store i32 %fhc, i32* %fhp
  store i32 %w, i32* @free_head
  br label %sw.inc
sw.inc:
  %w.nxt = add i32 %w, 1
  br label %sw.loop
done:
  %mk = load i32, i32* @gc.stat_live
  store i32 %mk, i32* @gc.stat_marked
  ret void
}


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

define internal i32 @rt_slice(i32 %h, i32 %low, i32 %high, i32 %step, i32 %hasLow, i32 %hasHigh, i32 %hasStep) {
entry:
  %p = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
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
  %np = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %nh
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
  %dstp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %np, i32 0, i32 2
  %c = load i32, i32* %cd
  %dst = getelementptr [256 x i32], [256 x i32]* %dstp, i32 0, i32 %c
  store i32 %v, i32* %dst
  %c1 = add i32 %c, 1
  store i32 %c1, i32* %cd
  %i1 = add i32 %i, %stp
  store i32 %i1, i32* %ci
  br label %loop

done:
  %c2 = load i32, i32* %cd
  store i32 %c2, i32* %nlenp
  ret i32 %nh
}

%obj = type {i32, i32} ; tagged dynamic value: {tag, payload}

; Construct a tagged %obj value from a kind tag and a payload. The payload is
; a heap handle for reference kinds (str/list/dict/set/tuple/class/instance/
; method/closure/exn/module) or the raw immediate for int/bool/None.
define internal %obj @rt_mkobj(i32 %tag, i32 %payload) {
entry:
  %o = insertvalue %obj undef, i32 %tag, 0
  %o2 = insertvalue %obj %o, i32 %payload, 1
  ret %obj %o2
}

; Read the kind tag word of an %obj value.
define internal i32 @rt_obj_tag(%obj %o) {
entry:
  %t = extractvalue %obj %o, 0
  ret i32 %t
}

; Read the payload word of an %obj value.
define internal i32 @rt_obj_payload(%obj %o) {
entry:
  %p = extractvalue %obj %o, 1
  ret i32 %p
}

; Test whether an %obj value carries the given kind tag.
define internal i1 @rt_obj_is(%obj %o, i32 %tag) {
entry:
  %t = extractvalue %obj %o, 0
  %eq = icmp eq i32 %t, %tag
  ret i1 %eq
}


define internal i32 @rt_heap_kind(i32 %h) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %kf = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 0
  %k = load i32, i32* %kf
  ret i32 %k
}

define internal i32 @rt_heap_len(i32 %h) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lf = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %l = load i32, i32* %lf
  ret i32 %l
}

define internal i32 @rt_heap_get(i32 %h, i32 %i) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %arr = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  %p = getelementptr [256 x i32], [256 x i32]* %arr, i32 0, i32 %i
  %v = load i32, i32* %p
  ret i32 %v
}

define internal i32 @rt_dict_has(i32 %h, i32 %k) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lf = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lf
  ; A dict stores [key, value] pairs, so scanning keys means walking the flat element
  ; array two words at a time up to 2*count. The bound used to be the entry count, which
  ; scanned only the first ceil(count/2) slots: "3 in {1: 2, 3: 4}" was false in the
  ; compiled binary, and a dict read for any later key raised KeyError.
  %limit = mul i32 %len, 2
  %arr = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  br label %header
header:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %ok = icmp slt i32 %i, %limit
  br i1 %ok, label %loop, label %miss
loop:
  %p = getelementptr [256 x i32], [256 x i32]* %arr, i32 0, i32 %i
  %kv = load i32, i32* %p
  %eq = icmp eq i32 %kv, %k
  br i1 %eq, label %hit, label %cont
cont:
  %next = add i32 %i, 2
  br label %header
hit:
  ret i32 1
miss:
  ret i32 0
}
`

// gcRootCap is the capacity of the compiled backend's root stack (ADR 0181). One
// entry is one *live handle variable* in one *active frame*, so the working set is
// (container variables in scope) x (call depth) — 4096 is generous for real
// programs, and exhausting it is a loud, deterministic failure rather than a
// silently unrooted handle.
const gcRootCap = 4096

// rootRuntimeIR is the compiled backend's precise-root stack: globals, the push /
// clear / frame primitives, and the collector's self-report (ADR 0181).
//
// It replaces the old static table. Before, `xs = [...]` recorded the *address* of
// `%_xs` at slot number f(scope,name), allocated once per function body — so a
// recursive call re-registered the same entry and the inner frame's list replaced
// the outer frame's root; a collection inside the callee then swept a list the outer
// frame was still about to read. The table also scanned every slot ever registered
// for the whole run, and relied on the address of an int variable being out of range
// of @heap to avoid marking a random object — guessing, which is exactly what
// precision is supposed to end.
//
// Now the array is a stack with discipline: a function prologue opens a frame,
// every handle-assigning store pushes the slot it wrote (deduped within the frame,
// because a loop body re-executes its assignments), a store of a non-handle tags the
// entry dead, and every return pops the frame. rt_gc traces only entries tagged as
// handles and counts the ones it did not have to look at.
const rootGlobalsIR = `
@gc.kinds = internal global [@CAP@ x i8] zeroinitializer
@gc.stat_collections = internal global i32 0
@gc.stat_roots = internal global i32 0
@gc.stat_skipped = internal global i32 0
@gc.stat_marked = internal global i32 0
@gc.stat_freed = internal global i32 0
@gc.stat_total_freed = internal global i32 0
@gc.stat_live = internal global i32 0
@gc.stat_topmax = internal global i32 0
@gc.root_overflow = internal global i32 0
@.gcrootmsg = private unnamed_addr constant [@MSGLEN@ x i8] c@MSG@
@.gcreport = private unnamed_addr constant [@FMTLEN@ x i8] c@FMT@
`

// rootRuntimeIR is the code half: the push / clear / frame primitives and the
// collector's self-report. The data half above is emitted whether or not a module
// touches the root stack, because rt_gc (part of the heap runtime) reads @gc.kinds
// and the stat globals.
const rootRuntimeIR = `
declare void @llvm.trap()

define internal void @rt_root_put(i32* %slot) {
entry:
  %top0 = load i32, i32* @gc_roots_used
  %start = sub i32 %top0, 1
  %empty = icmp slt i32 %start, 0
  br i1 %empty, label %rp.append, label %rp.loop
rp.loop:
  %i = phi i32 [ %start, %entry ], [ %i.next, %rp.step ]
  %sp = getelementptr [@CAP@ x i32*], [@CAP@ x i32*]* @gc.roots, i32 0, i32 %i
  %have = load i32*, i32** %sp
  %dup = icmp eq i32* %have, %slot
  br i1 %dup, label %rp.hit, label %rp.step
rp.step:
  %i.next = sub i32 %i, 1
  %more = icmp sge i32 %i.next, 0
  br i1 %more, label %rp.loop, label %rp.append
rp.hit:
  %kh = getelementptr [@CAP@ x i8], [@CAP@ x i8]* @gc.kinds, i32 0, i32 %i
  store i8 1, i8* %kh
  ret void
rp.append:
  %fits = icmp slt i32 %top0, @CAP@
  br i1 %fits, label %rp.push, label %rp.full
rp.push:
  %wp = getelementptr [@CAP@ x i32*], [@CAP@ x i32*]* @gc.roots, i32 0, i32 %top0
  store i32* %slot, i32** %wp
  %wk = getelementptr [@CAP@ x i8], [@CAP@ x i8]* @gc.kinds, i32 0, i32 %top0
  store i8 1, i8* %wk
  %used = add i32 %top0, 1
  store i32 %used, i32* @gc_roots_used
  %cur = load i32, i32* @gc.stat_topmax
  %grow = icmp sgt i32 %used, %cur
  br i1 %grow, label %rptm, label %rptm.done
rptm:
  store i32 %used, i32* @gc.stat_topmax
  br label %rptm.done
rptm.done:
  ret void
rp.full:
  store i32 1, i32* @gc.root_overflow
  ; The message length is known at compile time, so no strlen (which travels with the
  ; raise runtime and may not be declared in this module).
  call i64 @write(i32 2, i8* getelementptr ([@MSGLEN@ x i8], [@MSGLEN@ x i8]* @.gcrootmsg, i32 0, i32 0), i64 @MSG_BYTES@)
  call void @llvm.trap()
  unreachable
}

define internal void @rt_root_clear(i32* %slot) {
entry:
  %top0 = load i32, i32* @gc_roots_used
  %start = sub i32 %top0, 1
  %empty = icmp slt i32 %start, 0
  br i1 %empty, label %rc.done, label %rc.loop
rc.loop:
  %i = phi i32 [ %start, %entry ], [ %i.next, %rc.step ]
  %sp = getelementptr [@CAP@ x i32*], [@CAP@ x i32*]* @gc.roots, i32 0, i32 %i
  %have = load i32*, i32** %sp
  %same = icmp eq i32* %have, %slot
  br i1 %same, label %rc.hit, label %rc.step
rc.step:
  %i.next = sub i32 %i, 1
  %more = icmp sge i32 %i.next, 0
  br i1 %more, label %rc.loop, label %rc.done
rc.hit:
  %k = getelementptr [@CAP@ x i8], [@CAP@ x i8]* @gc.kinds, i32 0, i32 %i
  store i8 0, i8* %k
  ret void
rc.done:
  ret void
}

define internal void @rt_frame_open(i32* %save) {
entry:
  %top = load i32, i32* @gc_roots_used
  store i32 %top, i32* %save
  ret void
}

define internal void @rt_frame_close(i32 %base) {
entry:
  ; Entries above the base are unreachable to rt_gc once the top is pulled back,
  ; so a returning frame retains nothing; the next rt_root_put re-tags any index it
  ; reclaims.
  store i32 %base, i32* @gc_roots_used
  ret void
}

define internal void @rt_gc_report() {
entry:
  %topmax = load i32, i32* @gc.stat_topmax
  ; The report describes the run, not the program, so it goes to fd 2 like every
  ; other tool-level line this compiler emits (ADR 0179).
  %buf = alloca [192 x i8]
  %c = load i32, i32* @gc.stat_collections
  %r = load i32, i32* @gc.stat_roots
  %s = load i32, i32* @gc.stat_skipped
  %m = load i32, i32* @gc.stat_marked
  %f = load i32, i32* @gc.stat_freed
  %t = load i32, i32* @gc.stat_total_freed
  %l = load i32, i32* @gc.stat_live
  %fmt = getelementptr [@FMTLEN@ x i8], [@FMTLEN@ x i8]* @.gcreport, i32 0, i32 0
  %n = call i32 (i8*, i32, i8*, ...) @snprintf(i8* %buf, i32 192, i8* %fmt, i32 %c, i32 %r, i32 %s, i32 %m, i32 %f, i32 %t, i32 %l, i32 %topmax)
  %bad = icmp slt i32 %n, 0
  br i1 %bad, label %rdone, label %rwrite
rwrite:
  %len = sext i32 %n to i64
  call i64 @write(i32 2, i8* %buf, i64 %len)
  br label %rdone
rdone:
  ret void
}
`

// llvmCString renders a Go string as an LLVM byte-string constant, NUL terminated.
// Escapes count as one byte each, so the declared array length stays honest.
func llvmCString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\0A`)
		default:
			if c < 0x20 || c > 0x7e {
				b.WriteString(fmt.Sprintf(`\%02X`, c))
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteString(`\00"`)
	return b.String()
}

// gcRootReportLine is what the compiled backend prints for --gc-stats, mirroring the
// interpreter's report so one assertion can read either backend.
const gcRootReportLine = "gc: backend=aot collections=%d roots=%d skipped=%d marked=%d freed=%d total_freed=%d live=%d top=%d\n"

// gcRootOverflowMessage is the loud failure for a root stack that ran out. A handle
// that cannot be pushed may be swept while still live, so the alternative to
// stopping is corruption.
const gcRootOverflowMessage = "gustyc: the compiled root stack is full (recursion too deep); see ADR 0181\n"

// renderedRootRuntime fills in the root runtime's capacities and string constants.
func renderedRootRuntime() string {
	return fillRootTemplate(rootRuntimeIR)
}

// renderedRootGlobals renders the root stack's data half, which is emitted for every
// module (rt_gc reads @gc.kinds and the counters even in a program that never pushes).
func renderedRootGlobals() string {
	return fillRootTemplate(rootGlobalsIR)
}

// fillRootTemplate substitutes the capacities and string constants into a root-runtime
// block. Placeholders are used instead of fmt because the IR is full of `%` register
// names, which Sprintf would read as verbs.
func fillRootTemplate(t string) string {
	cap := strconv.Itoa(gcRootCap)
	t = strings.ReplaceAll(t, "@CAP@", cap)
	t = strings.ReplaceAll(t, "@MSGLEN@", strconv.Itoa(len(gcRootOverflowMessage)+1))
	t = strings.ReplaceAll(t, "@MSG_BYTES@", strconv.Itoa(len(gcRootOverflowMessage)))
	t = strings.ReplaceAll(t, "@MSG@", llvmCString(gcRootOverflowMessage))
	t = strings.ReplaceAll(t, "@FMTLEN@", strconv.Itoa(len(gcRootReportLine)+1))
	t = strings.ReplaceAll(t, "@FMT@", llvmCString(gcRootReportLine))
	return t
}

func GenerateIR(prog *Program) (string, error) {
	imports, err := resolveImports(prog)
	if err != nil {
		return "", err
	}
	g := &irGen{
		classIDs: map[string]int{}, nextSlot: 1,
		listVars:     map[string]bool{},
		runtimeDicts: map[string]bool{}, runtimeSets: map[string]bool{}, noneVars: map[string]bool{},
		listElemStr: map[string]bool{}, setElemStr: map[string]bool{}, dictKeyStr: map[string]bool{}, dictValStr: map[string]bool{}, listElemInt: map[string]bool{}, setElemInt: map[string]bool{}, dictKeyInt: map[string]bool{}, dictValInt: map[string]bool{}, internedVars: map[string]bool{}, strParamOf: strArgKinds(prog), strFuncs: strReturningFuncs(prog), strFillOf: stringFillingParams(prog), imports: imports, sym: map[string]string{}, allocd: map[string]bool{}, funcs: map[string]bool{}, funcBind: map[string]string{}, externs: map[string]*ExternDecl{}, genFuncs: map[string]bool{}, listOperands: map[string]bool{}, floatFuncs: map[string]bool{}, floatTemps: map[string]bool{}, fds: map[string]*FuncDef{}, params: map[string]string{}, fmtIdx: 0, strIdx: 0, tmp: 0, ldN: 0}
	// pre-scan top-level for user function names
	// escape analysis: dead list-literal assignments skip rt_alloc
	g.deadLists = deadListAssignments(prog.Stmts)
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			g.funcs[fd.Name] = true
			g.fds[fd.Name] = fd
		}
	}
	g.decls = "declare i32 @printf(i8*, ...)\n"
	// AOT heap containers crossing function boundaries: which parameters receive
	// a list/dict/set handle (see heapargs.go).
	g.heapArgs = heapArgKinds(prog)
	g.globals.WriteString("@exn_flag = internal global i32 0\n")
	g.globals.WriteString("@gc_roots_used = internal global i32 0\n")
	cap := strconv.Itoa(gcRootCap)
	g.globals.WriteString(fmt.Sprintf("@gc.roots = internal global [%s x i32*] zeroinitializer\n", cap))
	// The root runtime's two libc dependencies are declared once, here, for the whole
	// module: LLVM rejects the same function declared twice, and they are needed by
	// code (`rt_root_put`'s overflow diagnostic, `rt_gc_report`) that is present even
	// in a module that neither raises nor prints a float (the strlen lesson, ADR 0173).
	// The root stack's data and code travel together with the features that need them.
	// The libc declarations are always emitted (LLVM rejects a function declared in two
	// blocks, so they may not live in the raise/float runtimes alongside their users);
	// the globals go with any module that has a heap or pushes roots, and the
	// primitives additionally with --gc-stats, whose report call lives in main.
	g.globals.WriteString("declare i64 @write(i32, i8*, i64)\n")
	g.globals.WriteString("declare i32 @snprintf(i8*, i32, i8*, ...)\n")
	g.globals.WriteString("@exn_code = internal global i32 0\n")
	var b strings.Builder
	// pre-scan top-level for user function names
	// user function definitions become separate defines before main
	// Register classes before compiling function bodies so that class
	// constructor calls and polymorphic dispatch are recognized inside
	// functions (e.g. `def make(): return Animal()`).
	for _, st := range prog.Stmts {
		if cd, ok := st.(*ClassDef); ok {
			g.registerClass(cd)
		}
	}

	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			if err := g.funcDef(&b, fd); err != nil {
				return "", err
			}
		}
	}
	if err := g.emitModuleFuncs(&b); err != nil {
		return "", err
	}
	// FFI: collect extern declarations and emit their C prototypes.
	for _, st := range prog.Stmts {
		if ed, ok := st.(*ExternDecl); ok {
			g.externs[ed.Name] = ed
			argTypes := []string{}
			for _, p := range ed.Params {
				if p.Annot != nil && p.Annot.Kind == KindString {
					argTypes = append(argTypes, "i8*")
				} else {
					argTypes = append(argTypes, "i32")
				}
			}
			ret := "i32"
			if ed.ReturnAnno != nil && ed.ReturnAnno.Kind == KindString {
				ret = "i8*"
			}
			g.decls += "declare " + ret + " @" + ed.Name + "(" + strings.Join(argTypes, ", ") + ")\n"
		}
	}

	b.WriteString("define i32 @main() {\nentry:\n")
	// Module-level code is its own variable-binding scope. funcDef resets these
	// per body; without a reset here, an alloca emitted for a function parameter
	// named `xs` would make `xs = [1, 2]` in main skip its own alloca and store
	// through the (out-of-scope) `%_xs` register inside the function.
	// Module-level code is its own scope too (see beginScope).
	restoreScope := g.beginScope()
	defer restoreScope()
	for _, ap := range g.applyCalls {
		b.WriteString(fmt.Sprintf("  call void %s()\n", ap))
	}
	b.WriteString("  store i32 0, i32* @gc_roots_used\n")
	// Root every module-global closure env slot so GC keeps captured envs
	// (and any heap handles they hold) alive across top-level boundaries.
	for _, envName := range g.envSlots {
		g.gcRegGlobal(&b, envName)
	}
	g.funcRaiseExit = "main.raiseexit"
	g.inMain = true
	for _, st := range prog.Stmts {
		if _, ok := st.(*FuncDef); ok {
			continue
		}
		g.gcCall(&b)
		if err := g.stmt(&b, st); err != nil {
			return "", err
		}
	}
	g.inMain = false
	g.gcCall(&b)
	if GCReportEnabled() {
		// The compiled backend's own collector self-report (--gc-stats). The numbers
		// live in the program's globals, so only its runtime can read them out; the
		// line goes to fd 2 like every other tool-level line (ADR 0179, ADR 0181).
		b.WriteString("  call void @rt_gc_report()\n")
	}
	b.WriteString("  ret i32 0\n")
	b.WriteString("main.raiseexit:\n")
	// An uncaught exception used to fall off the end of main and exit 0 printing
	// nothing, so a program that raised looked like a program that succeeded to any
	// script that ran it. Report it on stderr and exit non-zero, like the interpreter.
	if g.raiseUsed {
		b.WriteString("  %exn.m = load i8*, i8** @exn_msg\n")
		b.WriteString("  call void @rt_die(i8* %exn.m)\n")
		b.WriteString("  ret i32 1\n")
	} else {
		b.WriteString("  ret i32 0\n")
	}
	b.WriteString("}\n")
	// assemble output
	var out strings.Builder
	g.emitEnvGlobals()
	// Container reads raise as well (IndexError / KeyError), so the raise runtime
	// travels with the heap runtime, not only with an explicit `raise`.
	if g.raiseUsed || g.heapUsed {
		g.globals.WriteString(raiseRuntimeIR)
	}
	if g.floatFmtUsed {
		// rt_fmt_double is only referenced by Python-style float rendering, so it
		// travels in its own block: a program that never prints a float does not
		// pay for the snprintf/strtod declarations.
		g.globals.WriteString(floatRuntimeIR)
	}
	if g.heapUsed {
		g.globals.WriteString(heapRuntimeIR)
	}
	// The precise-root stack runtime (ADR 0181) is emitted whether or not the program
	// allocates: every function prologue calls rt_frame_open, and a referenced but
	// undefined internal function fails verification. It is emitted after the other
	// runtime blocks so it can see which libc names they already declared.
	// Both halves of the root runtime are emitted *after* the body has been generated:
	// only then does codegen know whether this module touches the root stack at all.
	// (Emitting the data half in the preamble, where `rooted` was still false while the
	// function half emitted later referenced it, produced modules whose @gc.kinds was
	// missing — the verifier caught it, the tests did not until this round.)
	if g.rooted || g.heapUsed || GCReportEnabled() {
		g.globals.WriteString(renderedRootGlobals())
	}
	if g.rooted || GCReportEnabled() {
		g.globals.WriteString(renderedRootRuntime())
	}
	out.WriteString(g.strGlobals.String())
	out.WriteString(g.globals.String())
	EmitABI(&g.decls)
	out.WriteString(g.decls)
	out.WriteString(b.String())
	// PIC Level = 2 module flag: forces llc to emit position-independent code
	// so string constants in .rodata are referenced PIC-safely. Without it llc
	// defaults to the static relocation model, which emits 32-bit absolute
	// relocations (e.g. R_X86_64_32) that the default PIE link (cc) rejects.
	out.WriteString("!llvm.module.flags = !{!0}\n")
	out.WriteString("!0 = !{i32 2, !\"PIC Level\", i32 2}\n")
	// Every variable slot has to be allocated once per call for its *address* to
	// identify it — see hoistAllocas (ADR 0181).
	return hoistAllocas(out.String()), nil
}

// isAllocaLine reports whether a module line defines a stack slot, e.g.
// `  %_p = alloca i32`.
func isAllocaLine(ln string) bool {
	t := strings.TrimSpace(ln)
	return strings.HasPrefix(t, "%") && strings.Contains(t, " = alloca ")
}

// hoistAllocas moves every stack-slot allocation to the top of its own function.
//
// A precise root stack identifies a variable by the *address* of its slot, so the
// address must be one per variable per call. Codegen emits `%_p = alloca i32` where
// the variable is first assigned, which for a loop body means an alloca instruction
// inside the loop — and at llc's default -O0 nothing hoists it, so each iteration
// bumped a fresh frame slot. Consequences measured, not guessed: the machine stack
// grew by a slot per iteration, and the root entry for that variable never matched the
// one recorded before, so the root stack grew by one entry per iteration (a loop that
// built 2000 instances reached top=2002), every stale slot kept its object live, the
// 1024-slot heap filled up, and the program died. Hoisting the allocation gives each
// variable exactly one slot per call, which is also what a local variable means in C.
func hoistAllocas(module string) string {
	lines := strings.Split(module, "\n")
	out := make([]string, 0, len(lines))
	for i := 0; i < len(lines); {
		ln := lines[i]
		if !strings.HasPrefix(ln, "define ") || !strings.HasSuffix(strings.TrimSpace(ln), "{") {
			out = append(out, ln)
			i++
			continue
		}
		end := i + 1
		for end < len(lines) && lines[end] != "}" {
			end++
		}
		if end >= len(lines) {
			// No closing brace: emit the rest unchanged rather than guess.
			out = append(out, lines[i:]...)
			return strings.Join(out, "\n")
		}
		out = append(out, ln)
		body := lines[i+1 : end]
		var lead []string
		if len(body) > 0 && strings.HasSuffix(strings.TrimSpace(body[0]), ":") {
			lead = append(lead, body[0])
			body = body[1:]
		}
		var allocas, rest []string
		for _, bl := range body {
			if isAllocaLine(bl) {
				allocas = append(allocas, bl)
			} else {
				rest = append(rest, bl)
			}
		}
		out = append(out, lead...)
		out = append(out, allocas...)
		out = append(out, rest...)
		out = append(out, "}")
		i = end + 1
	}
	return strings.Join(out, "\n")
}

type irGen struct {
	globals    strings.Builder
	strGlobals strings.Builder // string constants, emitted at top of IR
	decls      string
	sym        map[string]string // variable -> load temp
	allocd     map[string]bool   // alloca emitted?
	// noneVars records variables whose latest assignment is the None singleton, so
	// print/truthiness/equality can be decided statically (ADR 0172).
	noneVars map[string]bool
	// listElemStr / setElemStr / dictKeyStr / dictValStr record that a container's elements
	// are interned strings (indices into @str_tab), which decides how they print and how
	// element reads behave (roadmap Gap I.2).
	listElemStr map[string]bool
	setElemStr  map[string]bool
	dictKeyStr  map[string]bool
	dictValStr  map[string]bool
	// The same four facts for numbers. A compiled container records one element kind, so a
	// container that has held both must be reported rather than printed through the wrong
	// table (roadmap Gap J.6).
	listElemInt map[string]bool
	setElemInt  map[string]bool
	dictKeyInt  map[string]bool
	dictValInt  map[string]bool
	// internedVars records names bound to an interned-string index (element reads and loop
	// variables over string containers), so print renders the text rather than the index.
	internedVars map[string]bool
	// strParamOf maps a function name to the parameter indices that receive strings; the
	// callee marks them in internedVars and the caller interns the argument (Gap J.5).
	strParamOf map[string]map[int]bool
	// strFuncs names the functions that return a string, so `print(echo("yo"))` prints text
	// and `xs.append(make_key())` stores an interned element rather than a raw index.
	strFuncs map[string]bool
	// strFillOf reports, per function and container parameter, which positions the body fills
	// with strings; a call site transfers that onto the caller's own variable so printing an
	// element later does not show a raw @str_tab index (Gap J.5).
	strFillOf map[string]map[int]int
	funcs     map[string]bool
	funcBind  map[string]string
	externs   map[string]*ExternDecl // user-defined function names
	fds       map[string]*FuncDef    // function definitions by name (for call arg binding)
	imports   *ImportInfo            // folded module globals for `import mod`
	params    map[string]string      // current function params: name -> register
	fmtIdx    int
	strIdx    int
	tmp       int
	label     int
	ldN       int
	loopStack []loopInfo

	closures    map[string]*closureInfo
	envMode     bool
	envCaptures map[string]int
	envParam    string
	decorated   map[string]bool
	deadLists   map[string]bool
	inFunc      bool
	applyCalls  []string
	listNames   map[*ListLit]string
	lstIdx      int
	dictNames   map[*DictLit]string
	dictIdx     int
	setNames    map[*SetLit]string
	setIdx      int
	compNames   map[*Comp]string
	// compLen records the folded element count of each lowered comprehension,
	// so indexing can bounds-check and emit the correct GEP shape.
	compLen map[*Comp]int
	// compEls records the folded element constant of each lowered comprehension,
	// so aggregate builtins (sum/min/max) can fold over the comprehension.
	compEls map[*Comp][]int64
	// compKeys records the folded key constants of each lowered dict
	// comprehension, so `d[key]` on a dict comprehension can be resolved at
	// codegen time (set/list comprehensions have no separate keys).
	compKeys map[*Comp][]int64
	// constBindings maps a comprehension variable name to its compile-time
	// constant so comprehension bodies can be unrolled at codegen time.
	constBindings map[string]int64
	// strVals maps a variable name to its concrete string constant value,
	// so `len(s)` and string concat can be resolved at codegen time.
	strVals map[string]string
	// dictVals maps a variable name to its concrete dict literal value,
	// so `d[key]` on a dict variable can be resolved at codegen time.
	dictVals map[string]*DictLit
	// lambdaCounter numbers generated anonymous functions (lambda_0, ...).
	lambdaCounter int
	// lambdas maps a variable name bound to a lambda to its generated
	// FuncDef name, so `f = lambda x: ...; f(3)` resolves in Call.
	lambdas map[string]string
	// floatVars tracks variables whose last assignment produced a double.
	floatVars map[string]bool
	// unionVars holds names annotated with a union type (e.g. `int | float`);
	// their slots are tagged so the runtime member is tracked for print dispatch.
	unionVars map[string]bool
	unionDecl bool
	// floatTemps tracks temps that hold a double result (e.g. a float call).
	floatTemps map[string]bool
	// floatFuncs tracks user functions that return a double.
	floatFuncs map[string]bool
	// curFunc tracks the user function currently being emitted.
	curFunc      string
	listVars     map[string]bool
	runtimeDicts map[string]bool
	runtimeSets  map[string]bool
	heapUsed     bool
	// curFnSrc is the source-level name of the function being lowered, for the
	// traceback frame; empty means module level, which renders as <module>.
	curFnSrc string
	// raiseUsed is set by any raise site (a `raise` statement, or a
	// compiler-generated one such as an out-of-bounds item assignment). It gates
	// rt_die, which reports an uncaught exception on stderr before main exits 1.
	raiseUsed bool
	// floatFmtUsed records that a float is rendered at run time, which pulls in
	// floatRuntimeIR (rt_fmt_double).
	floatFmtUsed  bool
	heapSeq       int
	handlerStack  []string
	funcRaiseExit string

	classInfos map[string]*classInfo // class name -> info
	classIDs   map[string]int        // class name -> runtime dispatch id
	classOrder []string              // classes in id order (dispatch switch)
	varClasses map[string]string     // local var -> class name
	selfClass  string                // enclosing class of current self
	attrSlots  map[string]int        // attr name -> instance data slot
	nextSlot   int

	// genFuncs records generator function names; calling one yields a runtime
	// heap list handle (mirroring the interpreter's eager yield semantics).
	genFuncs map[string]bool
	// staticLists maps a compile-time list global name (@.lstN) to its literal,
	// so a call site can copy it into the runtime heap when the callee expects a
	// container handle.
	staticLists map[string]*ListLit
	// heapArgs records, per function name, which parameter positions receive a
	// runtime heap container handle (see heapargs.go). Inferred once, before any
	// IR is emitted.
	heapArgs map[string]map[int]int
	// freshSlots records variables whose heap slot was created by the statement
	// currently being lowered, so the "release the previous binding" step is
	// skipped for them (see emitFreeOld).
	freshSlots map[string]bool
	// i1Vals records SSA registers whose LLVM type is i1 — comparison results and
	// boolean-logic results. Truthiness tests need it: feeding an i1 to
	// `icmp ne i32 …, 0` (or an i32 to `br i1`) is the classic way a module stops
	// verifying, and the old code assumed every condition operand was an i32.
	i1Vals map[string]bool
	// listOperands records IR operands known to be runtime heap list handles
	// (generator function results and generator expression results), so
	// print/indexing can treat them as lists.
	listOperands map[string]bool
	// genHandle is the runtime list handle for the generator function whose
	// body is currently being emitted; stmt() appends each `yield` to it.
	genHandle  string
	genIdx     int
	genExprIdx int
	inMain     bool
	gcRootIdx  int
	gcCallIdx  int
	gcRootSeen map[string]bool
	// frameOpen is set while a function body's root frame is open (ADR 0181): the
	// prologue recorded the root-stack base, so every return and unwind path must pop
	// back to it. gcRootSeen is now only per-scope bookkeeping — dedup of root entries
	// moved into the runtime, where it can be per *frame* rather than per body.
	frameOpen bool
	// rooted says the module touches the root stack at all, which gates shipping the
	// root runtime: a program of pure scalars ships no collector scaffolding.
	rooted bool
	// envSlots holds the module-global closure env slot names; each holds an
	// env heap handle and must be rooted so GC keeps captured envs alive.
	envSlots []string

	// curFnOverride, when non-empty, overrides the emitted name of the
	// current FuncDef (used for AOT module-function emission).
	curFnOverride string
	// curModName is the module whose function is currently being emitted; bare
	// Name calls inside it dispatch to sibling module functions.
	curModName string
	// curModGlobals holds folded module-global constants for the module
	// function currently being emitted; bare Name refs resolve against it.
	curModGlobals map[string]Expr
	// curModParams holds the param set of the module function being emitted,
	// so a param that shadows a module global is not substituted.
	curModParams map[string]bool
}

// markUnion records that the %unionbox type must be declared in the IR
// preamble (lazily, so non-union modules don't emit an unused declaration).
func (g *irGen) markUnion() {
	if !g.unionDecl {
		g.decls += "%unionbox = type {i32, i32, double, i8*}\n"
		g.unionDecl = true
	}
}

// emitUnionStore stores a value into a union-annotated scalar slot, tagging
// the runtime member (0=int, 1=float, 2=string) for print dispatch.
func (g *irGen) emitUnionStore(b *strings.Builder, nm string, e Expr) {
	g.markUnion()
	if !g.allocd[nm] {
		fmt.Fprintf(b, "  %%_%s = alloca %%unionbox\n", nm)
		g.allocd[nm] = true
	}
	if g.isFloat(e) {
		uf := g.newTmp()
		ut := g.newTmp()
		fmt.Fprintf(b, "  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 2\n", uf, nm)
		fmt.Fprintf(b, "  store double %s, double* %s\n", g.floatValue(b, e), uf)
		fmt.Fprintf(b, "  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 0\n", ut, nm)
		fmt.Fprintf(b, "  store i32 1, i32* %s\n", ut)
		return
	}
	if str, ok := e.(*StrLit); ok {
		us := g.newTmp()
		ut := g.newTmp()
		fmt.Fprintf(b, "  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 3\n", us, nm)
		fmt.Fprintf(b, "  store i8* %s, i8** %s\n", g.strConst(str.Value), us)
		fmt.Fprintf(b, "  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 0\n", ut, nm)
		fmt.Fprintf(b, "  store i32 2, i32* %s\n", ut)
		return
	}
	iv, _ := g.value(b, e)
	ui := g.newTmp()
	ut := g.newTmp()
	fmt.Fprintf(b, "  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 1\n", ui, nm)
	fmt.Fprintf(b, "  store i32 %s, i32* %s\n", iv, ui)
	fmt.Fprintf(b, "  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 0\n", ut, nm)
	fmt.Fprintf(b, "  store i32 0, i32* %s\n", ut)
}

// emitUnionPrint prints a union-annotated scalar variable, dispatching on its
// runtime tag to emit %d, %f, or %s for the currently-stored member. `nl` is the
// terminator written after the value ("\n" standalone, "" inside a print argument
// whose terminator comes from the call's `end`).
func (g *irGen) emitUnionPrint(b *strings.Builder, nm, nl string) {
	g.markUnion()
	tag := g.newTmp()
	lt := g.newTmp()
	fmt.Fprintf(b, "  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 0\n", tag, nm)
	fmt.Fprintf(b, "  %s = load i32, i32* %s\n", lt, tag)
	isf := g.newTmp()
	lf := g.newLabel("fbr")
	ln := g.newLabel("notf")
	lj := g.newLabel("join")
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, 1\n", isf, lt)
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isf, lf, ln)
	fmt.Fprintf(b, "%s:\n", lf)
	fv := g.newTmp()
	uf := g.newTmp()
	fmt.Fprintf(b, "  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 2\n", uf, nm)
	fmt.Fprintf(b, "  %s = load double, double* %s\n", fv, uf)
	// Python's rendering, not printf's %g: see rt_fmt_double.
	g.floatFmtUsed = true
	fs := g.newTmp()
	fmt.Fprintf(b, "  %s = call i8* @rt_fmt_double(double %s)\n", fs, fv)
	fmt.Fprintf(b, "  %s\n", g.printfCall("i8*", fs, "%s"+nl))
	fmt.Fprintf(b, "  br label %%%s\n", lj)
	fmt.Fprintf(b, "%s:\n", ln)
	iss := g.newTmp()
	ls := g.newLabel("sbr")
	li := g.newLabel("ibr")
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, 2\n", iss, lt)
	fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", iss, ls, li)
	fmt.Fprintf(b, "%s:\n", ls)
	sv := g.newTmp()
	us := g.newTmp()
	fmt.Fprintf(b, "  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 3\n", us, nm)
	fmt.Fprintf(b, "  %s = load i8*, i8** %s\n", sv, us)
	fmt.Fprintf(b, "  %s\n", g.printfCall("i8*", sv, "%s"+nl))
	fmt.Fprintf(b, "  br label %%%s\n", lj)
	fmt.Fprintf(b, "%s:\n", li)
	iv := g.newTmp()
	ui := g.newTmp()
	fmt.Fprintf(b, "  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 1\n", ui, nm)
	fmt.Fprintf(b, "  %s = load i32, i32* %s\n", iv, ui)
	fmt.Fprintf(b, "  %s\n", g.printfCall("i32", iv, "%d"+nl))
	fmt.Fprintf(b, "  br label %%%s\n", lj)
	fmt.Fprintf(b, "%s:\n", lj)
}

// printfCall emits a call to @printf with the given operand type and format.
func (g *irGen) printfCall(ty, v, format string) string {
	name, size := g.fmtStr(format)
	res := g.newTmp()
	return fmt.Sprintf("%s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), %s %s)", res, size, size, name, ty, v)
}

// registerClass records a class definition (ClassDef) and emits its methods.
type classInfo struct {
	name    string
	bases   []string
	methods map[string]string // method name -> LLVM function name
	doc     string
}

func (g *irGen) registerClass(cd *ClassDef) {
	if g.classInfos == nil {
		g.classInfos = map[string]*classInfo{}
	}
	// Idempotent: the class-registration pre-pass runs before main-stmt
	// processing, so a later duplicate call must not re-emit method bodies
	// (that would redefine the same LLVM functions).
	if _, ok := g.classInfos[cd.Name]; ok {
		return
	}
	if _, ok := g.classIDs[cd.Name]; !ok {
		g.classIDs[cd.Name] = len(g.classOrder)
		g.classOrder = append(g.classOrder, cd.Name)
	}
	ci := &classInfo{bases: []string{}, methods: map[string]string{}, doc: cd.Doc}
	for _, b := range cd.Bases {
		if b != nil {
			ci.bases = append(ci.bases, b.Value)
		}
	}
	g.classInfos[cd.Name] = ci
	for _, st := range cd.Body {
		fd, ok := st.(*FuncDef)
		if !ok {
			continue
		}
		mname := fd.Name
		funcName := fmt.Sprintf("%s_%s", cd.Name, mname)
		ci.methods[mname] = funcName
		g.collectAttrs(fd.Body)
		g.emitClassMethod(cd.Name, funcName, fd)
	}
}

// emitClassMethod emits a class method as an LLVM function with self as param 0.
func (g *irGen) emitClassMethod(className, funcName string, fd *FuncDef) {
	prevParams := g.params
	prevSelf := g.selfClass
	paramRegs := []string{"i32 %self"}
	for i := range fd.Params {
		paramRegs = append(paramRegs, fmt.Sprintf("i32 %%p%d", i+1))
	}
	g.params = map[string]string{"self": "%self"}
	for i := 1; i < len(fd.Params); i++ {
		g.params[fd.Params[i].Name] = fmt.Sprintf("%%p%d", i)
	}
	g.selfClass = className
	g.globals.WriteString(fmt.Sprintf("define i32 @%s(%s) {\n", funcName, strings.Join(paramRegs, ", ")))
	// A method is a call like any other: it opens its own root frame and pops it on
	// the way out. Without this the roots its body pushes (self, container args,
	// locals) piled up on the root stack one frame per call, so a loop that made
	// thousands of instances ran the stack out (ADR 0181).
	savedFrame := g.frameOpen
	g.gcOpenFrame(&g.globals)
	g.inFunc = true
	for _, st := range fd.Body {
		g.stmt(&g.globals, st)
	}
	g.gcCloseFrame(&g.globals)
	g.globals.WriteString("  ret i32 0\n}\n")
	g.inFunc = false
	g.frameOpen = savedFrame
	g.selfClass = prevSelf
	g.params = prevParams
}

// collectAttrs walks a method body and assigns a slot to each instance attr.
func (g *irGen) collectAttrs(stmts []Stmt) {
	for _, st := range stmts {
		g.scanAttrs(st)
	}
}

func (g *irGen) scanAttrs(st Stmt) {
	switch n := st.(type) {
	case *AssignStmt:
		g.slotForAttrExpr(n.Target)
		g.slotForAttrExpr(n.Value)
	case *ExprStmt:
		g.slotForAttrExpr(n.Expr)
	case *IfStmt:
		for _, s := range n.Then {
			g.scanAttrs(s)
		}
		for _, s := range n.Else {
			g.scanAttrs(s)
		}
	case *WhileStmt:
		for _, s := range n.Body {
			g.scanAttrs(s)
		}
	case *ForStmt:
		for _, s := range n.Body {
			g.scanAttrs(s)
		}
	}
}

// slotForAttrExpr assigns a global slot for each `self.x` / `self.x = v` attr.
func (g *irGen) slotForAttrExpr(e Expr) {
	if attr, ok := e.(*Attr); ok {
		g.attrSlot(attr.Name.Value)
		return
	}
	if call, ok := e.(*Call); ok {
		for _, a := range call.Args {
			g.slotForAttrExpr(a)
		}
	}
}

// attrSlot returns the global instance-data slot for an attribute name.
func (g *irGen) attrSlot(name string) int {
	if g.attrSlots == nil {
		g.attrSlots = map[string]int{}
	}
	if s, ok := g.attrSlots[name]; ok {
		return s
	}
	s := g.nextSlot
	g.attrSlots[name] = s
	g.nextSlot++
	return s
}

// resolveMethod resolves a method name across a class chain.
func (g *irGen) resolveMethod(className, mname string) (string, bool) {
	ci, ok := g.classInfos[className]
	if !ok {
		return "", false
	}
	if fn, ok := ci.methods[mname]; ok {
		return fn, true
	}
	for _, b := range ci.bases {
		if fn, ok := g.resolveMethod(b, mname); ok {
			return fn, true
		}
	}
	return "", false
}

// hasMethod reports whether any registered class defines a method named mname.
func (g *irGen) hasMethod(mname string) bool {
	for _, cls := range g.classOrder {
		if _, ok := g.resolveMethod(cls, mname); ok {
			return true
		}
	}
	return false
}

// receiverClass returns the class of a receiver expression, if statically known.
func (g *irGen) receiverClass(recv Expr) string {
	if n, ok := recv.(*Name); ok {
		if n.Value == "self" {
			return g.selfClass
		}
		if c, ok := g.varClasses[n.Value]; ok {
			return c
		}
	}
	return ""
}

type loopInfo struct {
	breakLabel    string
	continueLabel string
}

func (g *irGen) newTmp() string           { g.tmp++; return fmt.Sprintf("%%t%d", g.tmp) }
func (g *irGen) newLabel(s string) string { g.label++; return fmt.Sprintf("%s%d", s, g.label) }

// fmtStr emits a global string constant for a printf format; returns name and size.
func (g *irGen) fmtStr(format string) (string, int) {
	g.fmtIdx++
	name := fmt.Sprintf("@.fmt%d", g.fmtIdx)
	// escape backslashes for the IR c"..." literal; % is literal in IR and
	// must stay single so printf sees a real format directive (e.g. %d -> 42).
	f := strings.ReplaceAll(format, "\\", "\\\\")
	f = strings.ReplaceAll(f, "\n", "\\0A")
	// The array size must be the decoded byte count of the emitted constant:
	// each IR \\0A escape decodes to a single newline byte, so the raw
	// (unescaped) format length plus one trailing null is correct.
	g.strGlobals.WriteString(fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n", name, len(format)+1, f))
	return name, len(format) + 1
}

func isScalarConst(e Expr) bool {
	switch n := e.(type) {
	case *IntLit, *FloatLit, *BoolLit, *StrLit:
		return true
	case *BinOp:
		// `x = 1 + 2` is a scalar constant too: codegen folds it, so the variable
		// cannot hold a heap handle and does not need a root entry (which also keeps
		// the root runtime out of purely scalar modules).
		return isScalarConst(n.L) && isScalarConst(n.R)
	case *UnOp:
		return isScalarConst(n.X)
	}
	return false
}

func (g *irGen) gcReg(b *strings.Builder, name string) {
	g.gcRegKey(b, name, name)
}

// gcOpenFrame emits the prologue that remembers where this call's root entries
// start, so the matching rt_frame_close can drop everything the call pushed
// (ADR 0181).
func (g *irGen) gcOpenFrame(b *strings.Builder) {
	b.WriteString("  %gc.frame.base = alloca i32\n")
	b.WriteString("  call void @rt_frame_open(i32* %gc.frame.base)\n")
	g.frameOpen = true
	g.rooted = true
}

// gcCloseFrame emits the pop of the current call's root frame (ADR 0181). Outside a
// function body — main, module level, the synthetic apply functions — no frame was
// opened, so there is nothing to pop.
func (g *irGen) gcCloseFrame(b *strings.Builder) {
	if !g.frameOpen {
		return
	}
	g.rooted = true
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%gc.frame.base\n", t))
	b.WriteString(fmt.Sprintf("  call void @rt_frame_close(i32 %s)\n", t))
}

// gcRegKey pushes the alloca %_<allocaName> as a root of the currently open frame.
// The key names the *site*, so two functions whose parameter slots share an alloca
// name stay distinct even though the runtime dedups by slot address.
func (g *irGen) gcRegKey(b *strings.Builder, key, allocaName string) {
	// Push, never overwrite: the entry belongs to whichever frame is currently open,
	// so a recursive call gets its own instead of clobbering the outer one. The runtime
	// dedups within the frame, which is what lets codegen emit this at every assignment
	// site rather than only the first one it generates (a registration that lives in a
	// branch which did not run rooted nothing at all).
	b.WriteString("  call void @rt_root_put(i32* %_" + allocaName + ")\n")
	g.rooted = true
	g.gcRootIdx++
}

// gcClearRoot tags a variable's root entry dead when a non-handle is stored into it
// (ADR 0181). Without it, an int left in a variable slot would be scanned as a
// candidate heap index — the guessing precise rooting exists to end.
func (g *irGen) gcClearRoot(b *strings.Builder, allocaName string) {
	b.WriteString("  call void @rt_root_clear(i32* %_" + allocaName + ")\n")
	g.rooted = true
}

// gcStoreHandle stores a heap handle into a variable's slot and re-registers that
// slot as a root of the currently open frame (ADR 0181).
//
// The re-registration is not redundant. Rebinding a variable to a scalar tags its
// root entry dead (rt_root_clear), so the *next* container binding — say `s = {1, 2}`
// after `s = 5` — would otherwise leave a live container invisible to the collector,
// which recycles it while the program still uses it (observed as a set whose insert
// loop never terminated: the object under the variable had been reused). Because the
// runtime dedups within a frame, pushing on every handle store costs a scan of a
// handful of entries and cannot grow the stack.
func (g *irGen) gcStoreHandle(b *strings.Builder, h, name string) {
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", h, name))
	b.WriteString("  call void @rt_root_put(i32* %_" + name + ")\n")
	g.rooted = true
	g.gcRootIdx++
}

// gcRegGlobal pushes a module-global closure env slot as a permanent root.
func (g *irGen) gcRegGlobal(b *strings.Builder, name string) {
	// Root a module-global closure env slot: the env slot holds an i32 heap handle
	// (the closure env), so the collector must see it even when no main-level local
	// references it. Pushed, not written into a fixed index, so it is tagged as a
	// handle rather than left for the scanner to guess about (ADR 0181).
	b.WriteString("  call void @rt_root_put(i32* @" + name + "_slot)\n")
	g.rooted = true
	g.gcRootIdx++
}

func (g *irGen) gcCall(b *strings.Builder) {
	if !g.heapUsed {
		return
	}
	ci := g.gcCallIdx
	g.gcCallIdx++
	fmt.Fprintf(b, "  %%gc.n%d = load i32, i32* @gc_roots_used\n", ci)
	fmt.Fprintf(b, "  call void @rt_gc([%s x i32*]* @gc.roots, i32 %%gc.n%d)\n", strconv.Itoa(gcRootCap), ci)
}

// strConst emits a global for a string literal operand.
func (g *irGen) strConst(s string) string {
	g.strIdx++
	name := fmt.Sprintf("@.str%d", g.strIdx)
	esc := strings.ReplaceAll(s, "\\", "\\\\")
	// A raw '"' inside the payload would terminate the LLVM string literal early and the
	// module fails to verify with a nonsense array length ("got type '[7 x i8]' but
	// expected '[31 x i8]'"). This bit me via a traceback frame (`  File "prog", …`), but
	// any program string containing a double quote hit it too.
	esc = strings.ReplaceAll(esc, "\"", "\\22")
	esc = strings.ReplaceAll(esc, "\n", "\\0A")
	esc = strings.ReplaceAll(esc, "\t", "\\09")
	esc = strings.ReplaceAll(esc, "\r", "\\0D")
	// Array size is the decoded byte count: the raw string length (newlines
	// and backslashes are single bytes) plus one trailing null.
	g.strGlobals.WriteString(fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\\00\"\n", name, len(s)+1, esc))
	return name
}

// reversedExprs returns a copy of elems with the order reversed.
func reversedExprs(elems []Expr) []Expr {
	out := append([]Expr(nil), elems...)
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// listElemLoad loads list element i from an inline list literal's global
// struct with a constant GEP index (this llc build accepts only constant
// GEP indices).
func (g *irGen) listElemLoad(b *strings.Builder, ln *ListLit, name string, i int) string {
	v := g.newTmp()
	n := len(ln.Elems)
	b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32]}, {i32, [%d x i32]}* %s, i32 0, i32 1, i32 %d)\n", v, n, n, name, i))
	return v
}

// dictLiteralKeys returns the integer keys of a dict literal, erroring if any
// key is not a constant integer (the AOT path lowers only int-keyed dicts).
func dictLiteralKeys(dl *DictLit) ([]int64, error) {
	keys := make([]int64, len(dl.Keys))
	for i, k := range dl.Keys {
		il, ok := k.(*IntLit)
		if !ok {
			return nil, fmt.Errorf("codegen: a compiled dict literal holds constant integer keys only; the interpreter supports string and other keys (a compiled string needs the runtime string table, roadmap Gap I.2)")
		}
		keys[i] = il.Value
	}
	return keys, nil
}

// dictLiteralVals returns the integer values of a dict literal, erroring if
// any value is not a constant integer.
func dictLiteralVals(dl *DictLit) ([]int64, error) {
	vals := make([]int64, len(dl.Vals))
	for i, v := range dl.Vals {
		il, ok := v.(*IntLit)
		if !ok {
			return nil, fmt.Errorf("codegen: a compiled dict literal holds constant integer values only; the interpreter supports string and other values (a compiled string needs the runtime string table, roadmap Gap I.2)")
		}
		vals[i] = il.Value
	}
	return vals, nil
}

// setLiteralElems returns the integer elements of a set literal, erroring if
// any element is not a constant integer.
func setLiteralElems(sl *SetLit) ([]int64, error) {
	elems := make([]int64, len(sl.Elems))
	for i, el := range sl.Elems {
		il, ok := el.(*IntLit)
		if !ok {
			return nil, fmt.Errorf("set literal elements must be constant integers")
		}
		elems[i] = il.Value
	}
	return elems, nil
}

// intMembershipValues returns the integer membership-test values of a literal
// container expression (list/set elements, or dict keys) and whether the
// expression is a literal container whose test values are all integer
// constants. For `x in {k: v, ...}`, membership tests keys -- matching the
// interpreter (dict `in` checks keys).
func intMembershipValues(e Expr) ([]int64, bool) {
	var elems []Expr
	switch c := e.(type) {
	case *ListLit:
		elems = c.Elems
	case *SetLit:
		elems = c.Elems
	case *DictLit:
		elems = c.Keys // membership tests keys
	default:
		return nil, false
	}
	vals := make([]int64, 0, len(elems))
	for _, el := range elems {
		v, ok := constIntMemberVal(el)
		if !ok {
			return nil, false
		}
		vals = append(vals, v)
	}
	return vals, true
}

// constIntMemberVal returns the integer constant value of an expr, or ok=false.
func constIntMemberVal(e Expr) (int64, bool) {
	switch c := e.(type) {
	case *IntLit:
		return c.Value, true
	case *NoneLit:
		return 0, true
	case *BoolLit:
		if c.Value {
			return 1, true
		}
		return 0, true
	case *UnOp:
		if c.Op == "-" {
			if v, ok := constIntMemberVal(c.X); ok {
				return -v, true
			}
		}
	}
	return 0, false
}

// isStringExpr reports whether e is a string-producing expression. Literal
// membership against a literal container must not be unrolled when the tested
// value is a string (the comparison would be pointer-vs-int, not valid i32 IR).
// runtimeStringErr is the ADR 0166 rule applied to strings in runtime containers. Strings
// are compile-time constants in this backend (@.strN globals) while container slots hold i32
// handles, so a string operand lowers to a global fed to an i32 parameter — LLVM rejects it
// ("global variable reference must have pointer type") and the exit-code contract then calls
// valid user code a compiler bug (exit 2). Refusing with a clear message is the honest
// alternative; the interpreter supports strings in containers, and the plan for AOT is an
// interned string table (roadmap Gap I.2).
func runtimeStringErr(slot, op string) error {
	return fmt.Errorf("codegen: cannot %s a string as a runtime %s in the AOT backend yet; the interpreter supports it — a compiled container slot holds an int/bool value, and strings need the runtime string table (roadmap Gap I.2)", op, slot)
}

// rejectRuntimeString reports whether e would put (or look up) a string in a heap container
// slot. It covers every way codegen knows a value is a string: a literal/interpolated
// literal, a folded string expression (str(x), s.upper(), concatenation), and a variable
// bound to a string.
func (g *irGen) rejectRuntimeString(e Expr, slot, op string) error {
	if e == nil {
		return nil
	}
	if isStringExpr(e) {
		return runtimeStringErr(slot, op)
	}
	if _, ok := g.stringVal(e); ok {
		return runtimeStringErr(slot, op)
	}
	if nm, ok := e.(*Name); ok && g.strVals != nil {
		if _, isStr := g.strVals[nm.Value]; isStr {
			return runtimeStringErr(slot, op)
		}
	}
	return nil
}

func isStringExpr(e Expr) bool {
	switch e.(type) {
	case *StrLit, *FString:
		return true
	}
	return false
}

// stringConst resolves a string literal or a chain of `+`-concatenated string
// literals to its concrete value. Returns (s, true) when the expression is a
// compile-time-known string constant.
func stringConst(e Expr) (string, bool) {
	if sl, ok := e.(*StrLit); ok {
		return sl.Value, true
	}
	if b, ok := e.(*BinOp); ok && b.Op == "+" {
		ls, lok := stringConst(b.L)
		rs, rok := stringConst(b.R)
		if lok && rok {
			return ls + rs, true
		}
	}
	if c, ok := e.(*Call); ok {
		attr, ok := c.Fn.(*Attr)
		if ok {
			v, ok := stringConst(attr.Obj)
			if ok {
				switch attr.Name.Value {
				case "upper":
					return strings.ToUpper(v), true
				case "capitalize":
					return capitalize(v), true
				case "title":
					return title(v), true
				case "swapcase":
					return swapcase(v), true
				case "lower":
					return strings.ToLower(v), true
				case "strip":
					return strings.TrimSpace(v), true
				case "lstrip":
					return strings.TrimLeftFunc(v, unicode.IsSpace), true
				case "rstrip":
					return strings.TrimRightFunc(v, unicode.IsSpace), true
				case "replace":
					if len(c.Args) != 2 {
						return "", false
					}
					oldv, ok := stringConst(c.Args[0])
					if !ok {
						return "", false
					}
					newv, ok := stringConst(c.Args[1])
					if !ok {
						return "", false
					}
					return strings.ReplaceAll(v, oldv, newv), true
				case "join":
					if len(c.Args) != 1 {
						return "", false
					}
					ll, ok := c.Args[0].(*ListLit)
					if !ok {
						return "", false
					}
					parts := []string{}
					for _, el := range ll.Elems {
						sv, ok := stringConst(el)
						if !ok {
							return "", false
						}
						parts = append(parts, sv)
					}
					return strings.Join(parts, v), true
				}
			}
		}
		// str(int-literal) folds to its decimal string, so len(str(42)) -> 2.
		if n, ok := c.Fn.(*Name); ok && n.Value == "str" && len(c.Args) == 1 {
			if il, ok := c.Args[0].(*IntLit); ok {
				return strconv.FormatInt(il.Value, 10), true
			}
		}
		return "", false
	}
	return "", false
}

// stringConstLen is len() over a compile-time-known string constant.
func stringConstLen(e Expr) (int, bool) {
	if s, ok := stringConst(e); ok {
		return len(s), true
	}
	return 0, false
}

// listCallElems returns the underlying element list of a list-returning
// builtin call over an inline list literal. sorted/reversed preserve the
// element set (only reordering), so consumers like len/sum/min/max/any/all
// fold identically over the original elements.
func listCallElems(a Expr) ([]Expr, bool) {
	c, ok := a.(*Call)
	if !ok {
		return nil, false
	}
	fn, ok := c.Fn.(*Name)
	if !ok {
		return nil, false
	}
	switch fn.Value {
	case "sorted", "reversed":
		if len(c.Args) == 0 {
			return nil, false
		}
		if lit, ok := c.Args[0].(*ListLit); ok {
			if lit.Elems == nil {
				// Empty inline list: return a non-nil empty slice so
				// consumers fold len/sum/any/all over it (e.g. sum(sorted([]))).
				return []Expr{}, true
			}
			return lit.Elems, true
		}
		// Nested sorted/reversed calls preserve the element set; recurse to
		// the underlying inline list literal (e.g. sorted(reversed([...]))).
		if elems, ok := listCallElems(c.Args[0]); ok {
			return elems, true
		}
	}
	return nil, false
}

// listArgLen returns the length of a list-producing expression, which may
// itself be a nested list-producing builtin call (sorted/reversed/enumerate/
// zip/partition/split/rsplit). It recursively unwraps calls to match the
// interpreter's length semantics.
func (g *irGen) listArgLen(a Expr) (int, bool) {
	switch v := a.(type) {
	case *ListLit:
		return len(v.Elems), true
	case *Call:
		fn, ok := v.Fn.(*Name)
		if !ok || len(v.Args) == 0 {
			return 0, false
		}
		switch fn.Value {
		case "sorted", "reversed":
			return g.listArgLen(v.Args[0])
		case "enumerate":
			return g.listArgLen(v.Args[0])
		case "zip":
			if len(v.Args) != 2 {
				return 0, false
			}
			a, ok1 := g.listArgLen(v.Args[0])
			b, ok2 := g.listArgLen(v.Args[1])
			if !ok1 || !ok2 {
				return 0, false
			}
			if b < a {
				return b, true
			}
			return a, true
		case "partition":
			if _, ok := g.stringVal(v.Args[0]); ok {
				return 3, true
			}
			return 0, false
		case "split", "rsplit":
			if len(v.Args) != 2 {
				return 0, false
			}
			s, ok1 := g.stringVal(v.Args[0])
			sep, ok2 := g.stringVal(v.Args[1])
			if !ok1 || !ok2 {
				return 0, false
			}
			return strings.Count(s, sep) + 1, true
		}
	}
	return 0, false
}

// listLen returns the length of a list-producing builtin call over literal
// arguments, matching the interpreter semantics:
//
//	enumerate(x)   -> len(x)         (x must be an inline list literal)
//	zip(a, b)      -> min(len(a), len(b))
//	partition(s)   -> 3              (always three parts)
//	split(s, sep)  -> occurrences(sep in s) + 1
//	rsplit(s, sep) -> occurrences(sep in s) + 1
func (g *irGen) listLen(a Expr) (int, bool) {
	c, ok := a.(*Call)
	if !ok || len(c.Args) == 0 {
		return 0, false
	}
	fn, ok := c.Fn.(*Name)
	if !ok {
		return 0, false
	}
	switch fn.Value {
	case "reversed":
		if len(c.Args) != 1 {
			return 0, false
		}
		// reversed(s) preserves the length of a constant string s.
		if s, ok := g.stringVal(c.Args[0]); ok {
			return len(s), true
		}
		return 0, false
	case "enumerate":
		if len(c.Args) != 1 {
			return 0, false
		}
		return g.listArgLen(c.Args[0])
	case "zip":
		if len(c.Args) != 2 {
			return 0, false
		}
		a, ok1 := g.listArgLen(c.Args[0])
		b, ok2 := g.listArgLen(c.Args[1])
		if !ok1 || !ok2 {
			return 0, false
		}
		if b < a {
			return b, true
		}
		return a, true
	case "partition":
		if len(c.Args) != 1 {
			return 0, false
		}
		if _, ok := g.stringVal(c.Args[0]); ok {
			return 3, true
		}
		return 0, false
	case "split", "rsplit":
		if len(c.Args) != 2 {
			return 0, false
		}
		s, ok1 := g.stringVal(c.Args[0])
		sep, ok2 := g.stringVal(c.Args[1])
		if !ok1 || !ok2 {
			return 0, false
		}
		return strings.Count(s, sep) + 1, true
	}
	return 0, false
}

// emitList emits a dedicated global struct for an inline list literal
// (dedup by AST node) and returns its global name. Elements must be ints.
func (g *irGen) emitList(ln *ListLit) (string, error) {
	if name, ok := g.listNames[ln]; ok {
		return name, nil
	}
	if g.listNames == nil {
		g.listNames = map[*ListLit]string{}
	}
	n := len(ln.Elems)
	g.lstIdx++
	name := fmt.Sprintf("@.lst%d", g.lstIdx)
	if g.staticLists == nil {
		g.staticLists = map[string]*ListLit{}
	}
	g.staticLists[name] = ln
	g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [", name, n, n, n))
	for i, el := range ln.Elems {
		il, ok := el.(*IntLit)
		if !ok {
			return "", fmt.Errorf("list literal elements must be integers")
		}
		if i > 0 {
			g.globals.WriteString(", ")
		}
		g.globals.WriteString(fmt.Sprintf("i32 %d", il.Value))
	}
	g.globals.WriteString("] }\n")
	g.listNames[ln] = name
	return name, nil
}

// emitDict emits a dedicated global struct for an inline dict literal
// (dedup by AST node) and returns its global name. Layout: {i32 count,
// [n x i32] keys, [n x i32] vals}. Keys and values must be constant ints.
func (g *irGen) emitDict(dl *DictLit) (string, error) {
	if name, ok := g.dictNames[dl]; ok {
		return name, nil
	}
	if g.dictNames == nil {
		g.dictNames = map[*DictLit]string{}
	}
	keys, err := dictLiteralKeys(dl)
	if err != nil {
		return "", err
	}
	vals, err := dictLiteralVals(dl)
	if err != nil {
		return "", err
	}
	n := len(dl.Keys)
	g.dictIdx++
	name := fmt.Sprintf("@.dict%d", g.dictIdx)
	var keysArr, valsArr strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			keysArr.WriteString(", ")
			valsArr.WriteString(", ")
		}
		keysArr.WriteString(fmt.Sprintf("i32 %d", keys[i]))
		valsArr.WriteString(fmt.Sprintf("i32 %d", vals[i]))
	}
	g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32], [%d x i32]} { i32 %d, [%d x i32] [%s], [%d x i32] [%s] }\n", name, n, n, n, n, keysArr.String(), n, valsArr.String()))
	g.dictNames[dl] = name
	return name, nil
}

// emitSet emits a dedicated global struct for an inline set literal
// (dedup by AST node) and returns its global name. Layout matches a list:
// {i32 count, [n x i32] elems}. Elements must be constant ints.
func (g *irGen) emitSet(sl *SetLit) (string, error) {
	if name, ok := g.setNames[sl]; ok {
		return name, nil
	}
	if g.setNames == nil {
		g.setNames = map[*SetLit]string{}
	}
	elems, err := setLiteralElems(sl)
	if err != nil {
		return "", err
	}
	n := len(sl.Elems)
	g.setIdx++
	name := fmt.Sprintf("@.set%d", g.setIdx)
	var elemsArr strings.Builder
	for i := 0; i < n; i++ {
		if i > 0 {
			elemsArr.WriteString(", ")
		}
		elemsArr.WriteString(fmt.Sprintf("i32 %d", elems[i]))
	}
	g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [%s] }\n", name, n, n, n, elemsArr.String()))
	g.setNames[sl] = name
	return name, nil
}

// value emits an IR expression returning an i32 value; returns the operand string.

// stringVal resolves an Expr to a concrete string constant, if any.
func (g *irGen) dictMethodElems(e Expr) ([]Expr, bool) {
	c, ok := e.(*Call)
	if !ok {
		return nil, false
	}
	attr, ok := c.Fn.(*Attr)
	if !ok {
		return nil, false
	}
	if attr.Name.Value == "split" {
		str, ok := g.stringVal(attr.Obj)
		if !ok {
			return nil, false
		}
		sep := " "
		if len(c.Args) > 0 {
			if s2, ok := g.stringVal(c.Args[0]); ok {
				sep = s2
			} else {
				return nil, false
			}
		}
		parts := strings.Split(str, sep)
		elems := make([]Expr, len(parts))
		for i, p := range parts {
			elems[i] = &StrLit{Value: p}
		}
		return elems, true
	}
	if attr.Name.Value == "rsplit" {
		s, ok := g.stringVal(attr.Obj)
		if !ok {
			return nil, false
		}
		sep := " "
		if len(c.Args) == 1 {
			sep, ok = g.stringVal(c.Args[0])
			if !ok {
				return nil, false
			}
		}
		parts := strings.Split(s, sep)
		var elems []Expr
		for _, part := range parts {
			elems = append(elems, &StrLit{Value: part})
		}
		return elems, true
	}
	if attr.Name.Value == "partition" {
		// partition(s) always returns [head, sep, tail] (3 parts), so
		// len(...) folds to 3. Return three dummy int elements to count.
		if _, ok := g.stringVal(attr.Obj); !ok {
			return nil, false
		}
		return []Expr{
			&IntLit{Value: 0},
			&IntLit{Value: 0},
			&IntLit{Value: 0},
		}, true
	}
	if ll, ok := attr.Obj.(*ListLit); ok && attr.Name.Value == "append" {
		if len(c.Args) != 1 {
			return nil, false
		}
		elems := append(append([]Expr{}, ll.Elems...), c.Args[0])
		return elems, true
	}
	dl, ok := attr.Obj.(*DictLit)
	if !ok {
		return nil, false
	}
	switch attr.Name.Value {
	case "keys":
		return dl.Keys, true
	case "values":
		return dl.Vals, true
	case "items":
		// len({...}.items()) folds to the number of key/value pairs.
		return dl.Keys, true
	case "partition":
		// partition(s) always returns [head, sep, tail] (3 parts), so
		// len(...) folds to 3. Return three dummy int elements to count.
		return []Expr{
			&IntLit{Value: 0},
			&IntLit{Value: 0},
			&IntLit{Value: 0},
		}, true
	}
	return nil, false
}

func (g *irGen) isPartitionCall(c *Call) bool {
	if attr, ok := c.Fn.(*Attr); ok && attr.Name.Value == "partition" {
		return true
	}
	return false
}

func reverseVals(v []int64) {
	for i, j := 0, len(v)-1; i < j; i, j = i+1, j-1 {
		v[i], v[j] = v[j], v[i]
	}
}

func (g *irGen) indexListElems(c *Call) ([]Expr, bool) {
	// Method calls returning list elements: keys(), values(), split().
	// partition() returns only dummy length elems (see dictMethodElems),
	// so it is excluded here to avoid silently wrong results.
	if elems, ok := g.dictMethodElems(c); ok {
		if g.isPartitionCall(c) {
			return nil, false
		}
		return elems, true
	}
	// Builtin calls returning list elements: sorted(list), reversed(list).
	if fn, ok := c.Fn.(*Name); ok && (fn.Value == "sorted" || fn.Value == "reversed") {
		if lit, ok := c.Args[0].(*ListLit); ok {
			vals := make([]int64, len(lit.Elems))
			for i, el := range lit.Elems {
				il, ok := el.(*IntLit)
				if !ok {
					return nil, false
				}
				vals[i] = il.Value
			}
			if fn.Value == "sorted" {
				sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
				// honor sorted(list, reverse=True)
				if len(c.Args) > 1 {
					if kw, ok := c.Args[1].(*KeywordArg); ok && kw.Name == "reverse" {
						if b, ok := kw.Value.(*BoolLit); ok && b.Value {
							reverseVals(vals)
						}
					}
				}
			} else {
				rev := make([]int64, len(vals))
				for i, v := range vals {
					rev[len(vals)-1-i] = v
				}
				vals = rev
			}
			elems := make([]Expr, len(vals))
			for i, v := range vals {
				elems[i] = &IntLit{Value: v}
			}
			return elems, true
		}
	}
	return nil, false
}

func (g *irGen) stringVal(e Expr) (string, bool) {
	switch n := e.(type) {
	case *Attr:
		// `def.__doc__` / `Cls.__doc__` folds the docstring to a string
		// (mirrors the interpreter's doc introspection in AOT).
		if e.(*Attr).Name.Value == "__doc__" {
			if nm, ok := e.(*Attr).Obj.(*Name); ok {
				if fd, ok := g.fds[nm.Value]; ok {
					return fd.Doc, true
				}
				if ci, ok := g.classInfos[nm.Value]; ok {
					return ci.doc, true
				}
			}
		}
		// imported module global folded to a string (data imports)
		if nm, ok := e.(*Attr).Obj.(*Name); ok {
			if globals, ok := g.imports.Globals[nm.Value]; ok {
				if lit, ok := globals[e.(*Attr).Name.Value]; ok {
					if str, ok := lit.(*StrLit); ok {
						return str.Value, true
					}
				}
			}
		}
		return "", false
	case *StrLit:
		return n.Value, true
	case *Slice:
		// compile-time string slice fold: s[a:b:c] where the source and all
		// bounds are compile-time constants. Mirrors the interpreter's Python
		// slice semantics via pySliceIndices.
		src, ok := g.stringVal(n.Obj)
		if !ok {
			return "", false
		}
		low := int64(0)
		high := int64(0)
		step := int64(1)
		hasLow := false
		hasHigh := false
		if n.Low != nil {
			v, ok := g.foldConstInt(n.Low)
			if !ok {
				return "", false
			}
			low = v
			hasLow = true
		}
		if n.High != nil {
			v, ok := g.foldConstInt(n.High)
			if !ok {
				return "", false
			}
			high = v
			hasHigh = true
		}
		if n.Step != nil {
			v, ok := g.foldConstInt(n.Step)
			if !ok {
				return "", false
			}
			step = v
			if step == 0 {
				return "", false
			}
		}
		length := int64(len(src))
		start, stop, stp := pySliceIndices(low, high, step, hasLow, hasHigh, length)
		var sb strings.Builder
		if stp > 0 {
			for i := start; i < stop; i += stp {
				sb.WriteByte(src[i])
			}
		} else {
			for i := start; i > stop; i += stp {
				sb.WriteByte(src[i])
			}
		}
		return sb.String(), true

	case *Name:
		if g.strVals != nil {
			if s, ok := g.strVals[n.Value]; ok {
				return s, true
			}
		}
		return "", false
	case *BinOp:
		if n.Op == "+" {
			ls, lok := g.stringVal(n.L)
			rs, rok := g.stringVal(n.R)
			if lok && rok {
				return ls + rs, true
			}
		}
		return "", false
	case *Call:
		// str(int-literal) folds to its decimal string, e.g. print(str(42));
		// str(float-constant) folds to its %g decimal string (matches the
		// interpreter's Repr), so print(str(3.5)) emits a valid %s printf
		// with the string-global pointer rather than a %d printf fed an i8*.
		if name, ok := n.Fn.(*Name); ok && name.Value == "str" && len(n.Args) == 1 {
			if il, ok := n.Args[0].(*IntLit); ok {
				return strconv.FormatInt(il.Value, 10), true
			}
			if fv, ok := g.floatEval(n.Args[0]); ok {
				return pyFloatRepr(fv), true
			}
		}
		if name, ok := n.Fn.(*Name); ok && name.Value == "chr" {
			if il, ok := n.Args[0].(*IntLit); ok {
				return string(rune(il.Value)), true
			}
		}
		// constant-fold string methods: `"AbC".upper()`, `.lower()`, `.strip()`.
		attr, ok := n.Fn.(*Attr)
		if !ok {
			return "", false
		}
		v, ok := g.stringVal(attr.Obj)
		if !ok {
			return "", false
		}
		switch attr.Name.Value {
		case "swapcase":
			return swapcase(v), true
		case "title":
			return title(v), true
		case "capitalize":
			return capitalize(v), true
		case "upper":
			return strings.ToUpper(v), true
		case "lower":
			return strings.ToLower(v), true
		case "strip":
			return strings.TrimSpace(v), true
		case "lstrip":
			return strings.TrimLeftFunc(v, unicode.IsSpace), true
		case "rstrip":
			return strings.TrimRightFunc(v, unicode.IsSpace), true
		case "replace":
			if len(n.Args) != 2 {
				return "", false
			}
			oldv, ok := g.stringVal(n.Args[0])
			if !ok {
				return "", false
			}
			newv, ok := g.stringVal(n.Args[1])
			if !ok {
				return "", false
			}
			return strings.ReplaceAll(v, oldv, newv), true
		case "join":
			if len(n.Args) != 1 {
				return "", false
			}
			ll, ok := n.Args[0].(*ListLit)
			if !ok {
				return "", false
			}
			parts := []string{}
			for _, el := range ll.Elems {
				sv, ok := g.stringVal(el)
				if !ok {
					return "", false
				}
				parts = append(parts, sv)
			}
			return strings.Join(parts, v), true
		}
		return "", false
	}
	return "", false
}

// dictIndex resolves a constant key against a DictLit at codegen time.
func (g *irGen) dictIndex(dl *DictLit, key int64) (string, error) {
	keys, err := dictLiteralKeys(dl)
	if err != nil {
		return "", err
	}
	vals, err := dictLiteralVals(dl)
	if err != nil {
		return "", err
	}
	for i, k := range keys {
		if k == key {
			return fmt.Sprintf("%d", vals[i]), nil
		}
	}
	return "", fmt.Errorf("missing dict key %d", key)
}

// floatConst formats a float constant as an LLVM double literal.
func floatConst(v float64) string {
	f := pyFloatRepr(v)
	// LLVM double literals need a decimal point in the mantissa.
	if i := strings.IndexAny(f, "eE"); i >= 0 {
		if !strings.Contains(f[:i], ".") {
			f = f[:i] + ".0" + f[i:]
		}
	} else {
		if !strings.Contains(f, ".") {
			f += ".0"
		}
		f += "e+00"
	}
	return f
}

// isFloat reports whether expression e produces a float (double) value.
func (g *irGen) isFloat(e Expr) bool {
	switch n := e.(type) {
	case *FloatLit:
		return true
	case *UnOp:
		if n.Op == "-" {
			return g.isFloat(n.X)
		}
		return false
	case *BinOp:
		// `/` is *true* division (PEP 238): 7 / 2 is 3.5 even when both operands are
		// integers, so the result is a float whatever the operands are. Truncating it
		// made the language's most common operator behave like C's, and the parity
		// harness could not see it because both backends agreed on the wrong answer.
		if n.Op == "/" {
			return true
		}
		switch n.Op {
		case "+", "-", "*", "%", "//":
			return g.isFloat(n.L) || g.isFloat(n.R)
		}
		return false
	case *Name:
		if g.floatVars != nil {
			return g.floatVars[n.Value]
		}
		return false
	case *ListLit:
		for _, e := range n.Elems {
			if g.isFloat(e) {
				return true
			}
		}
		return false
	case *Call:
		if n.Fn != nil {
			if id, ok := n.Fn.(*Name); ok {
				if id.Value == "float" {
					return true
				}
				if g.floatFuncs[id.Value] {
					return true
				}
				if id.Value == "abs" || id.Value == "min" || id.Value == "max" {
					for _, a := range n.Args {
						if g.isFloat(a) {
							return true
						}
					}
				}
				if id.Value == "sqrt" || id.Value == "floor" || id.Value == "ceil" {
					return true
				}
				if id.Value == "sum" {
					for _, a := range n.Args {
						if g.isFloat(a) {
							return true
						}
					}
				}
			}
		}
		return false
	}
	return false
}

// valueText returns the i32 operand text for e, discarding any codegen error.
func (g *irGen) valueText(b *strings.Builder, e Expr) string {
	v, _ := g.value(b, e)
	return v
}

// floatValue emits a double IR operand for a float-typed expression e.
func (g *irGen) floatValue(b *strings.Builder, e Expr) string {
	switch n := e.(type) {
	case *FloatLit:
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(n.Value))
		return t
	case *Name:
		if g.floatTemps != nil && g.floatTemps[n.Value] {
			return n.Value
		}
		if g.floatVars != nil && g.floatVars[n.Value] {
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = load double, double* %%_%s\n", t, n.Value)
			return t
		}
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", t, g.valueText(b, n))
		return t
	case *UnOp:
		if n.Op == "-" {
			fx := g.floatValue(b, n.X)
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = fsub double 0.0, %s\n", t, fx)
			return t
		}
	case *BinOp:
		return g.floatBinOp(b, n)
	case *Call:
		if n.Fn != nil {
			if id, ok := n.Fn.(*Name); ok {
				if g.floatFuncs[id.Value] {
					t, err := g.call(b, n)
					if err != nil {
						return ""
					}
					return t
				}
			}
			if id, ok := n.Fn.(*Name); ok && id.Value == "sum" && len(n.Args) >= 1 {
				if lst, ok := n.Args[0].(*ListLit); ok {
					total := 0.0
					okAll := true
					for _, elem := range lst.Elems {
						fv, ok2 := g.floatEval(elem)
						if !ok2 {
							okAll = false
							break
						}
						total += fv
					}
					if okAll {
						t := g.newTmp()
						fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(total))
						return t
					}
				}
			}
			if id, ok := n.Fn.(*Name); ok && (id.Value == "min" || id.Value == "max") {
				if lst, ok := n.Args[0].(*ListLit); ok {
					elems := lst.Elems
					fe := make([]float64, 0, len(elems))
					okAll := true
					for _, elem := range elems {
						fv, ok2 := g.floatEval(elem)
						if !ok2 {
							okAll = false
							break
						}
						fe = append(fe, fv)
					}
					if okAll && len(fe) > 0 {
						bestf := fe[0]
						for _, fv := range fe[1:] {
							if id.Value == "min" && fv < bestf {
								bestf = fv
							}
							if id.Value == "max" && fv > bestf {
								bestf = fv
							}
						}
						t := g.newTmp()
						fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(bestf))
						return t
					}
				}
				fvals := make([]float64, 0, len(n.Args))
				allFold := true
				for _, a := range n.Args {
					fv, ok := g.floatEval(a)
					if !ok {
						allFold = false
						break
					}
					fvals = append(fvals, fv)
				}
				if allFold {
					if len(fvals) == 0 {
						return g.floatValue(b, n)
					}
					best := fvals[0]
					for _, fv := range fvals[1:] {
						if id.Value == "min" && fv < best {
							best = fv
						}
						if id.Value == "max" && fv > best {
							best = fv
						}
					}
					t := g.newTmp()
					fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(best))
					return t
				}
				if len(n.Args) == 2 {
					aop := g.floatValue(b, n.Args[0])
					bop := g.floatValue(b, n.Args[1])
					t := g.newTmp()
					c := g.newTmp()
					if id.Value == "min" {
						fmt.Fprintf(b, "  %s = fcmp olt double %s, %s\n", c, aop, bop)
					} else {
						fmt.Fprintf(b, "  %s = fcmp ogt double %s, %s\n", c, aop, bop)
					}
					fmt.Fprintf(b, "  %s = select i1 %s, double %s, double %s\n", t, c, aop, bop)
					return t
				}
			}
			if id, ok := n.Fn.(*Name); ok && id.Value == "abs" && len(n.Args) == 1 {
				t := g.newTmp()
				fmt.Fprintf(b, "  %s = call double @llvm.fabs.f64(double %s)\n", t, g.floatValue(b, n.Args[0]))
				return t
			}
			if id, ok := n.Fn.(*Name); ok && id.Value == "sqrt" {
				fx := g.floatValue(b, n.Args[0])
				rt := g.newTmp()
				fmt.Fprintf(b, "  %s = call double @llvm.sqrt.f64(double %s)\n", rt, fx)
				return rt
			}
			if id, ok := n.Fn.(*Name); ok && (id.Value == "floor" || id.Value == "ceil") {
				fx := g.floatValue(b, n.Args[0])
				rt := g.newTmp()
				if id.Value == "floor" {
					fmt.Fprintf(b, "  %s = call double @llvm.floor.f64(double %s)\n", rt, fx)
				} else {
					fmt.Fprintf(b, "  %s = call double @llvm.ceil.f64(double %s)\n", rt, fx)
				}
				return rt
			}
			if id, ok := n.Fn.(*Name); ok && id.Value == "float" && len(n.Args) == 1 {
				arg := n.Args[0]
				if g.isFloat(arg) {
					return g.floatValue(b, arg)
				}
				switch a := arg.(type) {
				case *IntLit:
					t := g.newTmp()
					fmt.Fprintf(b, "  %s = sitofp i32 %d to double\n", t, a.Value)
					return t
				case *StrLit:
					if fv, err := strconv.ParseFloat(a.Value, 64); err == nil {
						t := g.newTmp()
						fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(fv))
						return t
					}
				}
				t := g.newTmp()
				fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", t, g.valueText(b, arg))
				return t
			}
		}
	}
	t := g.newTmp()
	fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", t, g.valueText(b, e))
	return t
}

// floatBinOp emits float arithmetic/comparison for a float-typed BinOp.
func (g *irGen) floatBinOp(b *strings.Builder, n *BinOp) string {
	l := g.floatValue(b, n.L)
	r := g.floatValue(b, n.R)
	t := g.newTmp()
	switch n.Op {
	case "+":
		fmt.Fprintf(b, "  %s = fadd double %s, %s\n", t, l, r)
	case "-":
		fmt.Fprintf(b, "  %s = fsub double %s, %s\n", t, l, r)
	case "*":
		fmt.Fprintf(b, "  %s = fmul double %s, %s\n", t, l, r)
	case "//":
		fmt.Fprintf(b, "  %s = fdiv double %s, %s\n", t, l, r)
		q := g.newTmp()
		fmt.Fprintf(b, "  %s = call double @llvm.floor.f64(double %s)\n", q, t)
		return q
	case "/":
		fmt.Fprintf(b, "  %s = fdiv double %s, %s\n", t, l, r)
	case "%":
		fmt.Fprintf(b, "  %s = frem double %s, %s\n", t, l, r)
	case "**":
		fmt.Fprintf(b, "  %s = call double @llvm.pow.f64(double %s, double %s)\n", t, l, r)
	case "==":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp oeq double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	case "!=":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp one double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	case "<":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp olt double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	case "<=":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp ole double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	case ">":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp ogt double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	case ">=":
		bt := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp oge double %s, %s\n", bt, l, r)
		fmt.Fprintf(b, "  %s = zext i1 %s to i32\n", t, bt)
		return t
	default:
		fmt.Fprintf(b, "  %s = fadd double %s, %s\n", t, l, r)
	}
	return t
}

// floatEval returns the float64 value of a float-literal expression, if foldable.
func (g *irGen) floatEval(e Expr) (float64, bool) {
	switch n := e.(type) {
	case *FloatLit:
		return n.Value, true
	case *BinOp:
		l, lok := g.floatEval(n.L)
		r, rok := g.floatEval(n.R)
		if !lok || !rok {
			return 0, false
		}
		switch n.Op {
		case "+":
			return l + r, true
		case "-":
			return l - r, true
		case "*":
			return l * r, true
		case "/":
			if r == 0 {
				return 0, false
			}
			return l / r, true
		case "**":
			return math.Pow(l, r), true
		}
		return 0, false
	case *Call:
		if n.Fn != nil {
			if id, ok := n.Fn.(*Name); ok && id.Value == "float" && len(n.Args) == 1 {
				switch a := n.Args[0].(type) {
				case *IntLit:
					return float64(a.Value), true
				case *StrLit:
					if f, err := strconv.ParseFloat(a.Value, 64); err == nil {
						return f, true
					}
				}
			}
		}
	}
	return 0, false
}

// truthyValue emits an i32 0/1 for a condition, using float != 0.0
// for float expressions (fcmp one + zext) instead of truncated ints.
func (g *irGen) truthyValue(b *strings.Builder, e Expr) string {
	v, err := g.truthOperandErr(b, e)
	if err != nil {
		// Mirrors valueText: an un-lowerable condition is reported by the enclosing
		// statement path, which still has the error.
		return g.asI1(b, "0")
	}
	return v
}

// --- i1 / i32 truthiness normalisation -------------------------------------
//
// Conditions in this language accept any value (`if count:`, `if flag and ready:`,
// `while total:`), and a value in IR is either an i32 (integers, booleans stored
// as 0/1) or an i1 (the result of a comparison or boolean-logic instruction).
// Mixing the two — `icmp ne i32 %cmp, 0` on an i1, or `br i1 %count` on an i32 —
// is a verifier failure, so every consumer of a condition goes through these
// helpers instead of assuming a representation.

// markI1 records that reg holds an i1 and returns reg, so emission sites can wrap
// the register inline.
func (g *irGen) markI1(reg string) string {
	if g.i1Vals == nil {
		g.i1Vals = map[string]bool{}
	}
	g.i1Vals[reg] = true
	return reg
}

// i1Reg reports whether reg was produced by a comparison / boolean-logic
// instruction and is therefore already an i1.
func (g *irGen) i1Reg(reg string) bool { return g.i1Vals[reg] }

// asI1 normalises any scalar value to an i1 predicate usable by br/select/and/or:
// comparison results pass straight through, everything else is tested against zero.
func (g *irGen) asI1(b *strings.Builder, v string) string {
	switch v {
	case "true":
		return "true"
	case "false":
		return "false"
	}
	if g.i1Reg(v) {
		return v
	}
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", t, v))
	return g.markI1(t)
}

// asBoolI32 renders a boolean-producing expression the way the interpreter stores
// booleans — an i32 0/1 — so `not x`, `a and b`, `x in xs` can be printed, stored
// in a variable and tested again instead of leaking a bare i1 into an i32 slot.
func (g *irGen) asBoolI32(b *strings.Builder, v string) string {
	p := g.asI1(b, v)
	switch p {
	case "true":
		return "1"
	case "false":
		return "0"
	}
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", t, p))
	return t
}

// truthOperand is the condition-side helper: float falsiness goes through fcmp, a
// comparison emits its predicate directly, everything else goes through asI1.
func (g *irGen) truthOperand(b *strings.Builder, e Expr) string {
	v, err := g.truthOperandErr(b, e)
	if err != nil {
		// Mirrors valueText: a condition that cannot be lowered is reported by the
		// enclosing statement path, not here.
		return g.asI1(b, "0")
	}
	return v
}

func (g *irGen) truthOperandErr(b *strings.Builder, e Expr) (string, error) {
	// Strings and containers test their *content*, never their representation: a
	// string is an i8* and a container is a compile-time struct or a heap handle,
	// so `if xs:` on a handle would be true even for [] and testing the string
	// pointer is not even valid IR. Empty is false, non-empty is true.
	if sv, ok := g.stringVal(e); ok {
		return constI1(sv != ""), nil
	}
	if n, ok := g.listArgLen(e); ok {
		return constI1(n != 0), nil
	}
	if fn, ok := g.heapContainerLenFn(e); ok {
		v, err := g.value(b, e)
		if err != nil {
			return "", err
		}
		n := g.newTmp()
		fmt.Fprintf(b, "  %s = call i32 @%s(i32 %s)\n", n, fn, v)
		p := g.newTmp()
		fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", p, n)
		return g.markI1(p), nil
	}
	if g.isFloat(e) {
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp one double %s, 0.0\n", t, g.floatValue(b, e))
		return g.markI1(t), nil
	}
	// `if a < b and b < 9:` would otherwise pay for a zext and a re-test per
	// comparison; emit the predicate itself and use it directly.
	if n, ok := e.(*BinOp); ok && cmpI1Op(n.Op) != "" && !g.isFloat(n.L) && !g.isFloat(n.R) {
		l, err := g.value(b, n.L)
		if err != nil {
			return "", err
		}
		r, err := g.value(b, n.R)
		if err != nil {
			return "", err
		}
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = %s i32 %s, %s\n", t, cmpI1Op(n.Op), l, r)
		return g.markI1(t), nil
	}
	v, err := g.value(b, e)
	if err != nil {
		return "", err
	}
	return g.asI1(b, v), nil
}

// assignIndex lowers `ix.Obj[ix.Idx] = val` against a runtime container variable.
func (g *irGen) assignIndex(b *strings.Builder, ix *Index, val Expr) error {
	nm, ok := ix.Obj.(*Name)
	if !ok {
		return fmt.Errorf("codegen: item assignment needs a container variable on the left (d[k] = v), got %T; the interpreter supports more forms", ix.Obj)
	}
	v, err := g.value(b, val)
	if err != nil {
		return err
	}
	h, err := g.value(b, ix.Obj)
	if err != nil {
		return err
	}
	key, kIsStr, err := g.heapElemKind(b, ix.Idx)
	if err != nil {
		return err
	}
	switch {
	case g.runtimeDicts[nm.Value]:
		v, vIsStr, err := g.heapElemKind(b, val)
		if err != nil {
			return err
		}
		// Item assignment overwrites, so the new element's kind is the truth at that slot:
		// d[k] = "s" after d[k] = 1 leaves a string-valued dict (Gap J.6).
		g.replaceElemKind(nm.Value, "dict value", vIsStr)
		g.replaceElemKind(nm.Value, "dict key", kIsStr)
		b.WriteString(fmt.Sprintf("  call void @rt_dict_put(i32 %s, i32 %s, i32 %s)\n", h, key, v))
		if kIsStr || vIsStr {
			bits := 0
			if kIsStr {
				bits |= 2 // dict keys are interned strings
			}
			if vIsStr {
				bits |= 4 // dict values are interned strings
			}
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 %d)\n", h, bits))
		}
		return nil
	case g.listVars[nm.Value]:
		// A string element is stored as its @str_tab index (Gap I.2), like every other
		// container slot write; the printed form follows from listElemStr.
		sv, sIsStr, serr := g.heapElemKind(b, val)
		if serr != nil {
			return serr
		}
		g.replaceElemKind(nm.Value, "list", sIsStr)
		v = sv
		// Bounds are checked so an out-of-range index raises IndexError through the
		// same exception path `raise` uses, instead of writing past the elements.
		ln := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_len(i32 %s)\n", ln, h))
		hi := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, %s\n", hi, key, ln))
		g.markI1(hi)
		lo := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", lo, key))
		g.markI1(lo)
		bad := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", bad, lo, hi))
		g.markI1(bad)
		badL, okL, endL := g.newLabel("item.bad"), g.newLabel("item.ok"), g.newLabel("item.end")
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
		b.WriteString(fmt.Sprintf("%s:\n", badL))
		g.raiseTo(b, exnCode("IndexError"), "IndexError", "index out of range", ix.Span())
		b.WriteString(fmt.Sprintf("%s:\n", okL))
		b.WriteString(fmt.Sprintf("  call void @rt_put_elem(i32 %s, i32 %s, i32 %s)\n", h, key, v))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		b.WriteString(fmt.Sprintf("%s:\n", endL))
		return nil
	case g.runtimeSets[nm.Value]:
		return fmt.Errorf("codegen: sets do not support item assignment (s[k] = v); the interpreter raises TypeError")
	case isStringExpr(ix.Obj) || (g.strVals != nil && g.strVals[nm.Value] != ""):
		return fmt.Errorf("codegen: strings are immutable, so s[k] = v is not allowed")
	}
	return fmt.Errorf("codegen: item assignment is only supported for runtime dict and list variables")
}

// exprTyName returns the checker's inferred type name for an expression, for the
// node kinds whose binding a container registration depends on.
func exprTyName(e Expr) string {
	switch n := e.(type) {
	case *Call:
		return n.Ty
	case *Name:
		return n.Ty
	case *Index:
		return n.Ty
	case *Attr:
		return n.Ty
	case *CondExpr:
		return n.Ty
	case *ListLit:
		return n.Ty
	case *DictLit:
		return n.Ty
	case *SetLit:
		return n.Ty
	}
	return ""
}

// containerKindFromTy maps an inferred type name ("dict[any, any]", "list[int]",
// "set[int]") to the runtime container kind, or "" when it is not a container.
func containerKindFromTy(name string) string {
	switch {
	case strings.HasPrefix(name, "dict["):
		return "dict"
	case strings.HasPrefix(name, "list["):
		return "list"
	case strings.HasPrefix(name, "set["):
		return "set"
	}
	return ""
}

// beginScope starts a fresh variable-binding scope — a function body, or the
// module's top-level code — and returns the closure that restores the previous one.
//
// Which names are containers, which slots have been allocated, and which are GC
// roots are facts about a single scope. Sharing them across scopes makes code in one
// scope act on registers belonging to another: a dict `d` inside a function made
// module-level code free `%_d` before main had allocated it, producing a module LLVM
// rejects.
func (g *irGen) beginScope() func() {
	savedAlloc, savedRoots := g.allocd, g.gcRootSeen
	savedList, savedDict, savedSet, savedFresh := g.listVars, g.runtimeDicts, g.runtimeSets, g.freshSlots
	savedNone := g.noneVars
	savedLStr, savedSStr, savedKStr, savedVStr, savedInt := g.listElemStr, g.setElemStr, g.dictKeyStr, g.dictValStr, g.internedVars
	savedLNum, savedSNum, savedKNum, savedVNum := g.listElemInt, g.setElemInt, g.dictKeyInt, g.dictValInt
	g.allocd = map[string]bool{}
	g.gcRootSeen = map[string]bool{}
	g.listVars = map[string]bool{}
	g.runtimeDicts = map[string]bool{}
	g.runtimeSets = map[string]bool{}
	g.freshSlots = map[string]bool{}
	g.noneVars = map[string]bool{}
	g.listElemStr = map[string]bool{}
	g.setElemStr = map[string]bool{}
	g.dictKeyStr = map[string]bool{}
	g.dictValStr = map[string]bool{}
	g.listElemInt = map[string]bool{}
	g.setElemInt = map[string]bool{}
	g.dictKeyInt = map[string]bool{}
	g.dictValInt = map[string]bool{}
	g.internedVars = map[string]bool{}
	return func() {
		g.allocd, g.gcRootSeen = savedAlloc, savedRoots
		g.listVars, g.runtimeDicts, g.runtimeSets = savedList, savedDict, savedSet
		g.freshSlots = savedFresh
		g.noneVars = savedNone
		g.listElemStr, g.setElemStr = savedLStr, savedSStr
		g.dictKeyStr, g.dictValStr, g.internedVars = savedKStr, savedVStr, savedInt
		g.listElemInt, g.setElemInt = savedLNum, savedSNum
		g.dictKeyInt, g.dictValInt = savedKNum, savedVNum
	}
}

// emitFreeOld releases the container handle a variable currently holds, if it
// holds one at all.
//
// Heap handles share a word with raw values (ints, bools, None), and 0 means "not a
// handle" — but 0 is also a valid heap slot index. Freeing it unconditionally
// releases whatever object owns slot 0, so an unrelated list could be recycled out
// from under its own variable (observed as a non-empty list reading back as empty).
func (g *irGen) emitFreeOld(b *strings.Builder, name string) {
	// Only a variable that already has a slot can have an old binding, and a slot
	// created by this same statement holds a value that is not a live handle.
	// Loading and freeing it anyway emits `load i32, i32* %_x` before `%_x` is
	// allocated — a module LLVM rejects outright.
	if !g.allocd[name] || g.freshSlots[name] {
		delete(g.freshSlots, name)
		return
	}
	g.heapSeq++
	fs := g.heapSeq
	h := fmt.Sprintf("%%f%d", fs)
	b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", h, name))
	g.emitFree(b, h)
}

// emitFree emits `if (h != 0) rt_free(h)` for a possibly-null handle.
func (g *irGen) emitFree(b *strings.Builder, h string) {
	doIt := g.newLabel("free.do")
	skip := g.newLabel("free.skip")
	ok := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", ok, h))
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", ok, doIt, skip))
	b.WriteString(fmt.Sprintf("%s:\n", doIt))
	b.WriteString(fmt.Sprintf("  call void @rt_free(i32 %s)\n", h))
	b.WriteString(fmt.Sprintf("  br label %%%s\n", skip))
	b.WriteString(fmt.Sprintf("%s:\n", skip))
}

// constI1 renders a compile-time truth value as an LLVM i1 constant.
func constI1(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

// heapContainerLenFn returns the runtime helper that reports a container
// variable's length, when the expression names a known runtime container.
func (g *irGen) heapContainerLenFn(e Expr) (string, bool) {
	nm, ok := e.(*Name)
	if !ok {
		return "", false
	}
	// The container maps are keyed by the written name (see listVars/dicts/sets).
	switch {
	case g.listVars[nm.Value]:
		return "rt_list_len", true
	case g.runtimeDicts[nm.Value]:
		return "rt_dict_len", true
	case g.runtimeSets[nm.Value]:
		return "rt_set_len", true
	}
	return "", false
}

// cmpI1Op maps a comparison operator to its LLVM predicate instruction, or "" when
// the operator is not a comparison.
func cmpI1Op(op string) string {
	switch op {
	case "==", "is":
		return "icmp eq"
	case "!=", "is not":
		return "icmp ne"
	case "<":
		return "icmp slt"
	case "<=":
		return "icmp sle"
	case ">":
		return "icmp sgt"
	case ">=":
		return "icmp sge"
	}
	return ""
}

// emitDunderBinOp emits an AOT lowering of operator overloading for a binary
// operator. When a statically-known operand is a class instance that defines a
// dunder (or reflected) method for the operator, it emits a direct call to that
// method and returns the result register. It returns ("", false) when no
// overloading applies, so the caller falls back to builtin arithmetic.
func (g *irGen) emitDunderBinOp(b *strings.Builder, n *BinOp) (string, bool) {
	dunder := dunderForBinOp(n.Op)
	reflected := reflectedDunder(dunder)
	if dunder == "" {
		return "", false
	}
	// The interpreter dispatches the left operand's dunder first, then a
	// reflected method on the right operand (jit.go evalBinOp).
	clsL := g.receiverClass(n.L)
	if clsL != "" {
		if fn, ok := g.resolveMethod(clsL, dunder); ok {
			ret := g.newTmp()
			lv, err := g.value(b, n.L)
			if err != nil {
				return "", false
			}
			rv, err := g.value(b, n.R)
			if err != nil {
				return "", false
			}
			fmt.Fprintf(b, "  %s = call i32 @%s(i32 %s, i32 %s)\n", ret, fn, lv, rv)
			return ret, true
		}
	}
	clsR := g.receiverClass(n.R)
	if clsR != "" {
		if fn, ok := g.resolveMethod(clsR, reflected); ok {
			ret := g.newTmp()
			rv, err := g.value(b, n.R)
			if err != nil {
				return "", false
			}
			lv, err := g.value(b, n.L)
			if err != nil {
				return "", false
			}
			fmt.Fprintf(b, "  %s = call i32 @%s(i32 %s, i32 %s)\n", ret, fn, rv, lv)
			return ret, true
		}
	}
	return "", false
}

func (g *irGen) value(b *strings.Builder, e Expr) (string, error) {
	switch n := e.(type) {
	case *IntLit:
		return fmt.Sprintf("%d", n.Value), nil
	case *FloatLit:
		return fmt.Sprintf("%d", int64(n.Value)), nil
	case *BoolLit:
		if n.Value {
			return "1", nil
		}
		return "0", nil
	case *NoneLit:
		return "0", nil
	case *AssignExpr:
		v, err := g.value(b, n.Value)
		if err != nil {
			return "", err
		}
		name := n.Name.Value
		if g.isFloat(n.Value) {
			if !g.allocd[name] {
				b.WriteString(fmt.Sprintf("  %%_%s = alloca double\n", name))
				g.allocd[name] = true
			}
			b.WriteString(fmt.Sprintf("  store double %s, double* %%_%s\n", v, name))
		} else {
			if !g.allocd[name] {
				b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", name))
				g.allocd[name] = true
			}
			b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, name))
		}
		t := g.newTmp()
		if g.isFloat(n.Value) {
			b.WriteString(fmt.Sprintf("  %s = load double, double* %%_%s\n", t, name))
		} else {
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", t, name))
		}
		return t, nil
	case *Name:
		// String variables are compile-time constants (strVals); emit their
		// global pointer so printf/assign via value() sees the real string.
		if sv, ok := g.strVals[n.Value]; ok {
			return g.strConst(sv), nil
		}
		// A module-level class name used as a value (e.g. `Alias = Point`)
		// resolves to its class id so aliases can be stored and matched.
		if g.classIDs[n.Value] != 0 || g.classInfos[n.Value] != nil {
			return fmt.Sprintf("%d", g.classIDs[n.Value]), nil
		}
		// A module function body may reference a folded module-global
		// constant by its bare name (captured like a closure env). Resolve
		// it to the folded constant unless a parameter shadows it.
		if g.curModGlobals != nil {
			if folded, ok := g.curModGlobals[n.Value]; ok && !g.curModParams[n.Value] {
				return g.value(b, folded)
			}
		}
		// Comprehension variable bound to a compile-time constant.
		if v, ok := g.constBindings[n.Value]; ok {
			return fmt.Sprintf("%d", v), nil
		}
		if reg, ok := g.params[n.Value]; ok {
			return reg, nil
		}
		// Always load fresh from the alloca so the value dominates its use.
		g.ldN++
		if off, ok := g.envCaptures[n.Value]; ok && g.envMode {
			return g.emitEnvLoad(b, g.envParam, off), nil
		}
		// An unbound name would emit a load from a slot that was never allocated, and
		// LLVM's module verifier would reject it — which the exit-code contract then
		// reports as a *compiler bug* for what is an ordinary typo (roadmap Gap K.10).
		// The checker catches this today; this is the safety net that keeps the next
		// checker hole from surfacing as "input module is broken" (ADR 0166).
		if !g.nameIsBound(n.Value) {
			return "", fmt.Errorf("codegen: undefined name %q (no binding for it; assign it before use) — the interpreter reports the same error", n.Value)
		}
		ld := fmt.Sprintf("%%_%s.ld%d", n.Value, g.ldN)
		if g.unionVars[n.Value] {
			ui := fmt.Sprintf("%%_%s.ui%d", n.Value, g.ldN)
			b.WriteString(fmt.Sprintf("  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 1\n", ui, n.Value))
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", ld, ui))
			return ld, nil
		}
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", ld, n.Value))
		return ld, nil
	case *Attr:
		// `__doc__` on a top-level def/class name folds the docstring to a
		// string constant (mirrors the interpreter's jit doc introspection).
		if n.Name.Value == "__doc__" {
			if nm, ok := n.Obj.(*Name); ok {
				if fd, ok := g.fds[nm.Value]; ok {
					return g.strConst(fd.Doc), nil
				}
				if ci, ok := g.classInfos[nm.Value]; ok {
					return g.strConst(ci.doc), nil
				}
			}
		}
		// Instance attribute read: `self.x` / `inst.x`.
		if className := g.receiverClass(n.Obj); className != "" {
			objHandle, err := g.value(b, n.Obj)
			if err != nil {
				return "", err
			}
			g.heapUsed = true
			slot := g.attrSlot(n.Name.Value)
			ret := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 %d)\n", ret, objHandle, slot))
			return ret, nil
		}

		// `mod.var` — imported module global folded to a constant by resolveImports.
		if nm, ok := e.(*Attr).Obj.(*Name); ok {
			if globals, ok := g.imports.Globals[nm.Value]; ok {
				if lit, ok := globals[e.(*Attr).Name.Value]; ok {
					return g.value(b, lit)
				}
				return "", fmt.Errorf("codegen: unknown module attribute %s.%s", nm.Value, e.(*Attr).Name.Value)
			}
		}
		return "", fmt.Errorf("codegen: unsupported attr expression")

	case *BinOp:
		// Operator overloading: dispatch dunder methods on statically-known
		// class instances before falling back to builtin arithmetic.
		if res, ok := g.emitDunderBinOp(b, n); ok {
			return res, nil
		}
		if g.isFloat(n.L) || g.isFloat(n.R) {
			switch n.Op {
			case "==", "!=", "<", "<=", ">", ">=":
				return g.floatBinOp(b, n), nil
			}
		}
		// None equality. Values are untagged i32s here, so `x == None` is decided the way
		// the other dynamic-looking comparisons are: statically, from what the source says
		// each side is. Two Nones are equal, None and anything else are not — comparing a
		// None handle against the integer 0 would have made `0 == None` true (ADR 0172).
		if n.Op == "==" || n.Op == "!=" || n.Op == "is" || n.Op == "is not" {
			ln, rn := g.isNoneExpr(n.L), g.isNoneExpr(n.R)
			if ln || rn {
				// Both sides still have to be evaluated: `print(f() == None)` runs f(),
				// and f may print. Deciding the result statically must not delete the
				// operand's effects.
				if _, err := g.value(b, n.L); err != nil {
					return "", err
				}
				if _, err := g.value(b, n.R); err != nil {
					return "", err
				}
				eq := ln && rn
				if n.Op == "!=" || n.Op == "is not" {
					eq = !eq
				}
				v := "0"
				if eq {
					v = "1"
				}
				return v, nil
			}
		}

		// Constant string concatenation: fold "a" + "b" (and foldable string
		// calls like str(7)) into a single string global.
		if n.Op == "+" {
			ls, lok := g.stringVal(n.L)
			rs, rok := g.stringVal(n.R)
			if lok && rok {
				return g.strConst(ls + rs), nil
			}
		}
		l, err := g.value(b, n.L)
		if err != nil {
			return "", err
		}
		r, err := g.value(b, n.R)
		if err != nil {
			return "", err
		}
		// `x in xs` compares against the words stored in the container, so a string needle
		// must become its @str_tab index: passing @.strN to rt_contains(i32, i32) is the
		// shape LLVM rejects (roadmap Gap I.2).
		// Arithmetic on a value that is really an index into the string table would compute a
		// number from an address-like word (`s + 1` on a string parameter returned 1, where the
		// interpreter raises TypeError). Refuse it as a compile diagnostic instead (Gap J.5).
		switch n.Op {
		case "+", "-", "*", "/", "//", "%", "**", "<", ">", "<=", ">=":
			isStrOperand := func(e Expr) bool {
				if _, ok := g.stringVal(e); ok {
					return true
				}
				return g.printsAsInternedStr(e)
			}
			if (isStrOperand(n.L) || isStrOperand(n.R)) && n.Op != "+" {
				return "", fmt.Errorf("codegen: operator %q on a string is not supported in the AOT backend; the interpreter evaluates it — a compiled string is an interned table index, so arithmetic and ordering on it have no meaning", n.Op)
			}
			if n.Op == "+" && (isStrOperand(n.L) || isStrOperand(n.R)) {
				if _, ok := g.stringVal(n.L); ok {
					if _, ok2 := g.stringVal(n.R); ok2 {
						break // both constant: folded below
					}
				}
				return "", fmt.Errorf("codegen: concatenating a runtime string is not supported in the AOT backend yet; the interpreter supports it — building a new string needs a buffer allocation (roadmap Gap J.5)")
			}
		}
		// `s == "yes"` where s is a string parameter, a container element, or the result of a
		// string-returning call compares @str_tab indices: interning makes equal content the
		// same index, so this is content equality without a character loop (roadmap Gap J.5).
		// The literal side becomes its index; the interned side is already one.
		if n.Op == "==" || n.Op == "!=" {
			internSide := func(e Expr) (string, bool) {
				txt, ok := g.stringVal(e)
				if !ok {
					return "", false
				}
				g.heapUsed = true
				t := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", t, g.strConst(txt), g.strConst(pyReprString(txt))))
				return t, true
			}
			litSide, otherSide := Expr(nil), Expr(nil)
			if _, ok := g.stringVal(n.L); ok && g.printsAsInternedStr(n.R) {
				litSide, otherSide = n.L, n.R
			} else if _, ok := g.stringVal(n.R); ok && g.printsAsInternedStr(n.L) {
				litSide, otherSide = n.R, n.L
			}
			if litSide != nil {
				if iv, ok := internSide(litSide); ok {
					if litSide == n.L {
						l = iv
					} else {
						r = iv
					}
				}
				_ = otherSide
			}
		}
		if n.Op == "in" || n.Op == "not in" {
			if needle, ok := g.stringVal(n.L); ok {
				g.heapUsed = true
				it := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", it, g.strConst(needle), g.strConst(pyReprString(needle))))
				l = it
			}
		}
		// Constant folding: fold integer literals at compile time.
		// Constant-fold string equality/inequality: compare contents, not refs.
		if n.Op == "==" || n.Op == "!=" {
			ls, lok := g.stringVal(n.L)
			if lok {
				rs, rok := g.stringVal(n.R)
				if rok {
					res := 0
					if ls == rs {
						res = 1
					}
					if n.Op == "!=" {
						res = 1 - res
					}
					return fmt.Sprintf("%d", res), nil
				}
			}
		}
		if li, lok := n.L.(*IntLit); lok {
			if ri, rok := n.R.(*IntLit); rok && !g.isFloat(n) {
				lv, rv := int64(li.Value), int64(ri.Value)
				var res int64
				folded := false
				switch n.Op {
				case "+":
					res, folded = lv+rv, true
				case "-":
					res, folded = lv-rv, true
				case "*":
					res, folded = lv*rv, true
				case "/", "//":
					if rv != 0 {
						res, folded = lv/rv, true
					}
				case "%":
					if rv != 0 {
						res, folded = lv%rv, true
					}
				case "**":
					if rv >= 0 {
						base, exp := lv, rv
						res = 1
						for exp > 0 {
							if exp&1 != 0 {
								res *= base
							}
							exp >>= 1
							if exp > 0 {
								base *= base
							}
						}
						folded = true
					}
				case "and":
					folded = true
					if lv != 0 && rv != 0 {
						res = 1
					}
				case "or":
					folded = true
					if lv != 0 || rv != 0 {
						res = 1
					}
				case "==":
					folded = true
					if lv == rv {
						res = 1
					}
				case "<":
					folded = true
					if lv < rv {
						res = 1
					}
				case "<=":
					folded = true
					if lv <= rv {
						res = 1
					}
				case ">":
					folded = true
					if lv > rv {
						res = 1
					}
				case ">=":
					folded = true
					if lv >= rv {
						res = 1
					}
				}
				if folded {
					return fmt.Sprintf("%d", res), nil
				}
			}
		}
		t := g.newTmp()
		// `and`/`or` lower to boolean comparisons combined with i1 logic, then
		// zero-extended back to an i32 0/1 — mirroring the interpreter (which
		// evaluates both operands and returns a boolean).
		if n.Op == "and" || n.Op == "or" {
			// `and`/`or` test the *truth* of each operand — which may be an integer,
			// a float, or the i1 result of a comparison — then combine the two
			// predicates and zero-extend back to the interpreter's i32 0/1 boolean.
			lt := g.truthOperand(b, n.L)
			rt := g.truthOperand(b, n.R)
			if n.Op == "and" {
				b.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", t, lt, rt))
			} else {
				b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", t, lt, rt))
			}
			g.markI1(t)
			res := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", res, t))
			return res, nil
		}
		// Membership tests need runtime container access, so handle them
		// separately from the i32 arithmetic/comparison ops.
		if n.Op == "in" || n.Op == "not in" {
			// Literal container (list/set/dict literal): unroll `l == elem`
			// checks against the constant integer elements/keys. This fixes
			// `x in [1,2,3]` where the container is a compile-time global
			// struct, not a heap handle (the rt_contains path below indexes
			// @heap by the struct address, which is UB).
			if vals, ok := intMembershipValues(n.R); ok && !isStringExpr(n.L) {
				if len(vals) == 0 {
					// Empty container: nothing is contained.
					if n.Op == "not in" {
						return "1", nil
					}
					return "0", nil
				}
				acc := g.newTmp()
				cmp := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = icmp eq i32 %s, %d\n", cmp, l, vals[0]))
				b.WriteString(fmt.Sprintf("\t%s = or i1 false, %s\n", acc, cmp))
				for _, v := range vals[1:] {
					c2 := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = icmp eq i32 %s, %d\n", c2, l, v))
					nacc := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = or i1 %s, %s\n", nacc, acc, c2))
					acc = nacc
				}
				if n.Op == "not in" {
					inv := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = xor i1 %s, true\n", inv, acc))
					acc = inv
				}
				res := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = zext i1 %s to i32\n", res, acc))
				return res, nil
			}
			// A string haystack: `"cat" in greeting` is substring membership, not
			// container membership. Both sides must be @str_tab indices — handing the
			// raw @.strN global to rt_contains(i32, i32) emitted a global in an i32
			// slot, which is invalid IR, so the module died in llc and a plain Python
			// program looked like a compiler bug (ADR 0166).
			if hay, ok := g.stringVal(n.R); ok {
				hn := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", hn, g.strConst(hay), g.strConst(pyReprString(hay))))
				sc := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = call i32 @rt_str_contains(i32 %s, i32 %s)\n", sc, hn, l))
				bt := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = icmp ne i32 %s, 0\n", bt, sc))
				g.markI1(bt)
				if n.Op == "not in" {
					inv := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = xor i1 %s, true\n", inv, bt))
					g.markI1(inv)
					return g.asBoolI32(b, inv), nil
				}
				return g.asBoolI32(b, bt), nil
			}
			if g.printsAsInternedStr(n.R) {
				sc := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = call i32 @rt_str_contains(i32 %s, i32 %s)\n", sc, r, l))
				bt := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = icmp ne i32 %s, 0\n", bt, sc))
				g.markI1(bt)
				if n.Op == "not in" {
					inv := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = xor i1 %s, true\n", inv, bt))
					g.markI1(inv)
					return g.asBoolI32(b, inv), nil
				}
				return g.asBoolI32(b, bt), nil
			}
			// l is the value to test, r is the container handle.
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("\t%s = call i32 @rt_contains(i32 %s, i32 %s)\n", t, r, l))
			bt := g.newTmp()
			b.WriteString(fmt.Sprintf("\t%s = icmp ne i32 %s, 0\n", bt, t))
			g.markI1(bt)
			if n.Op == "not in" {
				nt := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = xor i1 %s, true\n", nt, bt))
				g.markI1(nt)
				return g.asBoolI32(b, nt), nil
			}
			return g.asBoolI32(b, bt), nil
		}
		var op string
		switch n.Op {
		case "+":
			op = "add"
		case "-":
			op = "sub"
		case "*":
			op = "mul"
		case "/", "//":
			op = "sdiv"
		case "%":
			op = "srem"
		case "**":
			ld := g.newTmp()
			fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", ld, l)
			rd := g.newTmp()
			fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", rd, r)
			pw := g.newTmp()
			fmt.Fprintf(b, "  %s = call double @llvm.pow.f64(double %s, double %s)\n", pw, ld, rd)
			t2 := g.newTmp()
			fmt.Fprintf(b, "  %s = fptosi double %s to i32\n", t2, pw)
			return t2, nil
		case "is":
			op = "icmp eq"
		case "is not":
			op = "icmp ne"
		case "==":
			op = "icmp eq"
		case "!=":
			op = "icmp ne"
		case "<":
			op = "icmp slt"
		case "<=":
			op = "icmp sle"
		case ">":
			op = "icmp sgt"
		case ">=":
			op = "icmp sge"
		default:
			return "", fmt.Errorf("codegen: unsupported operator %q", n.Op)
		}
		if strings.HasPrefix(op, "icmp") {
			// A comparison is a *value* (printable, storable, passable), so it
			// returns the interpreter's i32 0/1; the i1 predicate stays internal
			// (tracked in i1Vals) so a condition can use it without re-testing.
			b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", t, op, l, r))
			g.markI1(t)
			res := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", res, t))
			return res, nil
		}
		b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", t, op, l, r))
		return t, nil
	case *UnOp:
		x, err := g.value(b, n.X)
		if err != nil {
			return "", err
		}
		t := g.newTmp()
		switch n.Op {
		case "-":
			if g.isFloat(n.X) {
				fx := g.floatValue(b, n.X)
				b.WriteString(fmt.Sprintf("  %s = fsub double 0.0, %s\n", t, fx))
				break
			}
			b.WriteString(fmt.Sprintf("  %s = sub i32 0, %s\n", t, x))
		case "not":
			// `not` yields a boolean *value* (printable, storable), so it returns the
			// interpreter's i32 0/1 rather than a bare i1.
			p := g.asI1(b, x)
			if p == "true" {
				return "0", nil
			}
			if p == "false" {
				return "1", nil
			}
			notP := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = xor i1 %s, true\n", notP, p))
			g.markI1(notP)
			return g.asBoolI32(b, notP), nil
		default:
			return "", fmt.Errorf("codegen: unsupported unary %q", n.Op)
		}
		return t, nil
	case *CondExpr:
		// ternary `then if cond else otherwise`: pick a branch by condition.
		cond := g.truthyValue(b, n.Cond)
		then, err := g.value(b, n.If)
		if err != nil {
			return "", err
		}
		els, err := g.value(b, n.Else)
		if err != nil {
			return "", err
		}
		// the condition is an i1 (comparison/and/or) or a bare constant that
		// LLVM infers as i1 in the select context.
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, cond, then, els))
		return t, nil

	case *StrLit:
		name := g.strConst(n.Value)
		return name, nil
	case *FString:
		return "", fmt.Errorf("codegen: f-string requires a constant expression (AOT backend)")
	case *ListLit:
		// inline list literal: emit a dedicated global struct and return its name.
		if literalNeedsHeap(n) {
			if literalMixedKinds(n) {
				return "", mixedKindErr("list")
			}
			return g.heapListFrom(b, n, "")
		}
		name, err := g.emitList(n)
		if err != nil {
			return "", err
		}
		return name, nil
	case *DictLit:
		// A literal with string keys or values cannot be the static {count, keys, vals}
		// global — that layout is i32-only — so build a heap dict and intern (Gap J.6).
		if literalNeedsHeap(n) {
			if literalMixedKinds(n) {
				return "", mixedKindErr("dict")
			}
			return g.heapDictFrom(b, n, "")
		}
		name, err := g.emitDict(n)
		if err != nil {
			return "", err
		}
		return name, nil
	case *SetLit:
		if literalNeedsHeap(n) {
			if literalMixedKinds(n) {
				return "", mixedKindErr("set")
			}
			return g.heapSetFrom(b, n, "")
		}
		name, err := g.emitSet(n)
		if err != nil {
			return "", err
		}
		return name, nil
	case *Slice:
		// String slices are folded at compile time via stringVal; emit the
		// folded result's global pointer (used by print/assign via value()).
		if _, isStr := g.stringVal(n.Obj); isStr {
			if sv, ok := g.stringVal(n); ok {
				return g.strConst(sv), nil
			}
			return "", fmt.Errorf("cannot fold string slice")
		}
		objReg, err := g.value(b, n.Obj)
		if err != nil {
			return "", err
		}
		lowReg := "0"
		highReg := "0"
		stepReg := "1"
		hasLow := "0"
		hasHigh := "0"
		hasStep := "0"
		if n.Low != nil {
			lowReg, err = g.value(b, n.Low)
			if err != nil {
				return "", err
			}
			hasLow = "1"
		}
		if n.High != nil {
			highReg, err = g.value(b, n.High)
			if err != nil {
				return "", err
			}
			hasHigh = "1"
		}
		if n.Step != nil {
			stepReg, err = g.value(b, n.Step)
			if err != nil {
				return "", err
			}
			hasStep = "1"
		}
		r := g.heapSeq
		g.heapSeq++
		fmt.Fprintf(b, "  %%sl%d = call i32 @rt_slice(i32 %s, i32 %s, i32 %s, i32 %s, i32 %s, i32 %s, i32 %s)\n", r, objReg, lowReg, highReg, stepReg, hasLow, hasHigh, hasStep)
		return fmt.Sprintf("%%sl%d", r), nil

	case *Index:
		// list/dict/set indexing against an inline literal with a constant
		// index/key (this llc build accepts only constant GEP indices).
		// Constant keys/elements are resolved at compile time.
		key := int64(0)
		if il, ok := n.Idx.(*IntLit); ok {
			key = il.Value
		} else {
			// non-literal index: only runtime containers support it (x[a]).
			if nm, ok := n.Obj.(*Name); ok {
				if !g.listVars[nm.Value] && !g.runtimeDicts[nm.Value] && !g.runtimeSets[nm.Value] {
					return "", fmt.Errorf("index must be a constant")
				}
			} else {
				return "", fmt.Errorf("index must be a constant")
			}
		}
		switch obj := n.Obj.(type) {
		case *Name:
			// runtime heap list variable: x[i] reads heap[x].data[i].
			if g.runtimeDicts[obj.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, obj.Value))
				// The key may be a runtime value (`for k in d: d[k]`), so it is
				// lowered like the list index path does; only a literal key folds. A
				// string key becomes its @str_tab index, because rt_dict_get/rt_dict_has
				// take i32 keys and a global pointer there is what LLVM rejected.
				keyOp := strconv.FormatInt(key, 10)
				if _, isLit := n.Idx.(*IntLit); !isLit {
					kv, isStr, e := g.heapElemKind(b, n.Idx)
					if e != nil {
						return "", e
					}
					if isStr {
						g.dictKeyStr[obj.Value] = true
					}
					keyOp = kv
				}
				g.checkKeyRead(b, fmt.Sprintf("%%h%d", hs), keyOp, n.Span())
				b.WriteString(fmt.Sprintf("  %%g%d = call i32 @rt_dict_get(i32 %%h%d, i32 %s)\n", hs, hs, keyOp))
				return fmt.Sprintf("%%g%d", hs), nil
			}
			if g.listVars[obj.Value] {
				idxOp := strconv.FormatInt(key, 10)
				if _, ok := n.Idx.(*IntLit); !ok {
					v, e := g.value(b, n.Idx)
					if e != nil {
						return "", e
					}
					idxOp = v
				}
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, obj.Value))
				g.checkIndexRead(b, fmt.Sprintf("%%h%d", hs), idxOp, n.Span())
				b.WriteString(fmt.Sprintf("  %%g%d = call i32 @rt_get_elem(i32 %%h%d, i32 %s)\n", hs, hs, idxOp))
				return fmt.Sprintf("%%g%d", hs), nil
			}
			if g.dictVals != nil {
				if dl, ok := g.dictVals[obj.Value]; ok {
					return g.dictIndex(dl, key)
				}
			}
			return "", fmt.Errorf("index of a non-literal variable")

		case *Attr:
			// imported module list global (data imports): mod.list[i]
			if nm, ok := obj.Obj.(*Name); ok {
				if globals, ok2 := g.imports.Globals[nm.Value]; ok2 {
					if lit, ok3 := globals[obj.Name.Value]; ok3 {
						if lst, ok4 := lit.(*ListLit); ok4 {
							if key >= 0 && int(key) < len(lst.Elems) {
								return g.value(b, lst.Elems[key])
							}
						}
						if dct, ok4 := lit.(*DictLit); ok4 {
							ks, err := dictLiteralKeys(dct)
							if err != nil {
								return "", err
							}
							vs, err := dictLiteralVals(dct)
							if err != nil {
								return "", err
							}
							for i := range ks {
								if ks[i] == key {
									return fmt.Sprintf("%d", vs[i]), nil
								}
							}
						}
					}
				}
			}
			return "", fmt.Errorf("codegen: index of non-list module attr")
		case *ListLit:
			// index into a list literal: evaluate the element directly.
			return g.value(b, obj.Elems[key])
		case *DictLit:
			// constant-key lookup: find the key in the literal and return its
			// constant value at compile time.
			keys, err := dictLiteralKeys(obj)
			if err != nil {
				return "", err
			}
			vals, err := dictLiteralVals(obj)
			if err != nil {
				return "", err
			}
			for i, k := range keys {
				if k == key {
					return fmt.Sprintf("%d", vals[i]), nil
				}
			}
			return "", fmt.Errorf("dict key not found")
		case *SetLit:
			// constant membership lookup: return the element if present.
			elems, err := setLiteralElems(obj)
			if err != nil {
				return "", err
			}
			for _, el := range elems {
				if el == key {
					return fmt.Sprintf("%d", key), nil
				}
			}
			return "", fmt.Errorf("not in set")
		case *Comp:
			// indexing into a lowered comprehension result. The struct shape
			// depends on the comprehension kind:
			//   - list: positional GEP+load into {i32, [n x i32]}.
			//   - set:  membership test against the folded elements.
			//   - dict: constant key lookup against the folded key/value pairs.
			switch obj.Kind {
			case CompList:
				name, err := g.comp(b, obj)
				if err != nil {
					return "", err
				}
				n := g.compLen[obj]
				if key < 0 || key >= int64(n) {
					return "", fmt.Errorf("list index out of range")
				}
				v := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32]}, {i32, [%d x i32]}* %s, i32 0, i32 1, i32 %d)\n", v, n, n, name, key))
				return v, nil
			case CompSet:
				// set membership: `s[key]` returns the element if present,
				// otherwise errors (interpreter semantics). comp() populates
				// compEls and emits the set global.
				if _, err := g.comp(b, obj); err != nil {
					return "", err
				}
				for _, el := range g.compEls[obj] {
					if el == key {
						return fmt.Sprintf("%d", key), nil
					}
				}
				return "", fmt.Errorf("not in set")
			case CompDict:
				// dict lookup: `d[key]` returns the mapped value, else errors.
				// comp() populates compKeys/compEls and emits the dict global.
				if _, err := g.comp(b, obj); err != nil {
					return "", err
				}
				for i, k := range g.compKeys[obj] {
					if k == key {
						return fmt.Sprintf("%d", g.compEls[obj][i]), nil
					}
				}
				return "", fmt.Errorf("key not found")
			default:
				return "", fmt.Errorf("unsupported comprehension kind %d", obj.Kind)
			}
		case *StrLit:
			str, ok := g.stringVal(obj)
			if !ok {
				return "", fmt.Errorf("string index on non-constant string")
			}
			if key < 0 || key >= int64(len(str)) {
				return "", fmt.Errorf("string index out of range")
			}
			return fmt.Sprintf("%d", str[key]), nil
		case *Call:
			// Element access into list-producing call expressions: keys(),
			// values(), sorted(...), reversed(...), split(...). partition()
			// returns only dummy length elems (see dictMethodElems), so it
			// is excluded here to avoid silently wrong results.
			if elems, ok2 := g.indexListElems(obj); ok2 {
				if key < 0 || int(key) >= len(elems) {
					return "", fmt.Errorf("list index out of range")
				}
				return g.value(b, elems[key])
			}
			return "", fmt.Errorf("index requires an inline list/dict/set literal")
		default:
			return "", fmt.Errorf("index requires an inline list/dict/set literal")
		}
	case *Comp:
		return g.comp(b, n)
	case *Generator:
		return g.genExpr(b, n)
	case *AwaitExpr:
		// await e: codegen runs eagerly; await reduces to evaluating e.
		return g.value(b, n.Expr)
	case *Call:
		return g.call(b, n)
	case *KeywordArg:
		return g.value(b, n.Value)
	case *Lambda:
		// a lambda used as a value: register its anonymous FuncDef and
		// return the generated name as a closure reference.
		name, err := g.emitLambda(b, n)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("%s", name), nil
	default:
		return "", fmt.Errorf("codegen: unsupported expression %T", e)
	}
}

// foldConstInt evaluates e to a compile-time integer constant using the
// current constBindings, or returns (0, false) when not compile-time-known.
// It mirrors the interpreter's constant arithmetic so comprehension bodies can
// be unrolled at codegen time without emitting IR.
func (g *irGen) foldConstInt(e Expr) (int64, bool) {
	switch n := e.(type) {
	case *IntLit:
		return n.Value, true
	case *BoolLit:
		if n.Value {
			return 1, true
		}
		return 0, true
	case *NoneLit:
		return 0, true
	case *Name:
		if v, ok := g.constBindings[n.Value]; ok {
			return v, true
		}
		return 0, false
	case *BinOp:
		lv, lok := g.foldConstInt(n.L)
		rv, rok := g.foldConstInt(n.R)
		if !lok || !rok {
			return 0, false
		}
		switch n.Op {
		case "+":
			return lv + rv, true
		case "-":
			return lv - rv, true
		case "*":
			return lv * rv, true
		case "/", "//":
			if rv == 0 {
				return 0, false
			}
			return lv / rv, true
		case "%":
			if rv == 0 {
				return 0, false
			}
			return lv % rv, true
		case "==":
			if lv == rv {
				return 1, true
			}
			return 0, true
		case "!=":
			if lv != rv {
				return 1, true
			}
			return 0, true
		case "<":
			if lv < rv {
				return 1, true
			}
			return 0, true
		case "<=":
			if lv <= rv {
				return 1, true
			}
			return 0, true
		case ">":
			if lv > rv {
				return 1, true
			}
			return 0, true
		case ">=":
			if lv >= rv {
				return 1, true
			}
			return 0, true
		case "and":
			if lv != 0 && rv != 0 {
				return 1, true
			}
			return 0, true
		case "or":
			if lv != 0 || rv != 0 {
				return 1, true
			}
			return 0, true
		}
		return 0, false
	case *UnOp:
		xv, xok := g.foldConstInt(n.X)
		if !xok {
			return 0, false
		}
		switch n.Op {
		case "-":
			return -xv, true
		case "not":
			if xv == 0 {
				return 1, true
			}
			return 0, true
		}
		return 0, false
	default:
		return 0, false
	}
}

// comp lowers a list comprehension over a constant iterable (inline list
// literal or range(n)) into a dedicated global struct, unrolled at compile
// time. Returns the global's name.
func (g *irGen) comp(b *strings.Builder, c *Comp) (string, error) {
	if c.ForVar == nil {
		return "", fmt.Errorf("codegen: comprehension must bind a loop variable")
	}
	if c.Kind != CompList && c.Kind != CompSet && c.Kind != CompDict {
		return "", fmt.Errorf("codegen: unsupported comprehension kind %d", c.Kind)
	}
	// Determine the iteration items: an inline integer list literal or range(n).
	var items []int64
	if ll, ok := c.Iter.(*ListLit); ok {
		for _, el := range ll.Elems {
			v, ok := g.foldConstInt(el)
			if !ok {
				return "", fmt.Errorf("codegen: comprehension iterable must be constant integers")
			}
			items = append(items, v)
		}
	} else if r, ok := c.Iter.(*Call); ok {
		// range(stop), range(start, stop) or range(start, stop, step)
		start := int64(0)
		stop, ok := g.foldConstInt(r.Args[0])
		if !ok {
			return "", fmt.Errorf("codegen: range bound must be a constant")
		}
		step := int64(1)
		if len(r.Args) > 1 {
			start = stop
			stop, ok = g.foldConstInt(r.Args[1])
			if !ok {
				return "", fmt.Errorf("codegen: range stop must be a constant")
			}
		}
		if len(r.Args) > 2 {
			step, ok = g.foldConstInt(r.Args[2])
			if !ok {
				return "", fmt.Errorf("codegen: range step must be a constant")
			}
		}
		if step > 0 {
			for v := start; v < stop; v += step {
				items = append(items, v)
			}
		} else {
			for v := start; v > stop; v += step {
				items = append(items, v)
			}
		}
	} else {
		return "", fmt.Errorf("codegen: comprehension iterable must be an inline list literal or range()")
	}
	// Unroll the comprehension, binding the loop variable to each item.
	if g.constBindings == nil {
		g.constBindings = map[string]int64{}
	}
	if g.compNames == nil {
		g.compNames = map[*Comp]string{}
	}
	if g.compLen == nil {
		g.compLen = map[*Comp]int{}
	}
	if g.compEls == nil {
		g.compEls = map[*Comp][]int64{}
	}
	if g.compKeys == nil {
		g.compKeys = map[*Comp][]int64{}
	}
	var name string
	var results []int64
	switch c.Kind {
	case CompList:
		for _, item := range items {
			g.constBindings[c.ForVar.Value] = item
			v, ok := g.foldConstInt(c.Elems[0])
			if !ok {
				delete(g.constBindings, c.ForVar.Value)
				return "", fmt.Errorf("codegen: comprehension element must be constant")
			}
			if c.Cond != nil {
				cv, ok := g.foldConstInt(c.Cond)
				if !ok {
					delete(g.constBindings, c.ForVar.Value)
					return "", fmt.Errorf("codegen: comprehension condition must be constant")
				}
				if cv == 0 {
					delete(g.constBindings, c.ForVar.Value)
					continue
				}
			}
			results = append(results, v)
			delete(g.constBindings, c.ForVar.Value)
		}
		g.lstIdx++
		name = fmt.Sprintf("@.lst%d", g.lstIdx)
		if g.staticLists == nil {
			g.staticLists = map[string]*ListLit{}
		}
		lit := &ListLit{Elems: make([]Expr, 0, len(results))}
		for _, v := range results {
			lit.Elems = append(lit.Elems, &IntLit{Value: v})
		}
		g.staticLists[name] = lit
		var parts []string
		for _, v := range results {
			parts = append(parts, fmt.Sprintf("i32 %d", v))
		}
		n := len(results)
		g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [%s] }\n", name, n, n, n, strings.Join(parts, ", ")))
	case CompSet:
		seen := map[int64]bool{}
		for _, item := range items {
			g.constBindings[c.ForVar.Value] = item
			v, ok := g.foldConstInt(c.Elems[0])
			if !ok {
				delete(g.constBindings, c.ForVar.Value)
				return "", fmt.Errorf("codegen: comprehension element must be constant")
			}
			if c.Cond != nil {
				cv, ok := g.foldConstInt(c.Cond)
				if !ok {
					delete(g.constBindings, c.ForVar.Value)
					return "", fmt.Errorf("codegen: comprehension condition must be constant")
				}
				if cv == 0 {
					delete(g.constBindings, c.ForVar.Value)
					continue
				}
			}
			if !seen[v] {
				seen[v] = true
				results = append(results, v)
			}
			delete(g.constBindings, c.ForVar.Value)
		}
		g.setIdx++
		name = fmt.Sprintf("@.set%d", g.setIdx)
		var parts []string
		for _, v := range results {
			parts = append(parts, fmt.Sprintf("i32 %d", v))
		}
		n := len(results)
		g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [%s] }\n", name, n, n, n, strings.Join(parts, ", ")))
	case CompDict:
		var keys []int64
		for _, item := range items {
			g.constBindings[c.ForVar.Value] = item
			k, ok := g.foldConstInt(c.Keys[0])
			if !ok {
				delete(g.constBindings, c.ForVar.Value)
				return "", fmt.Errorf("codegen: comprehension key must be constant")
			}
			v, ok := g.foldConstInt(c.Vals[0])
			if !ok {
				delete(g.constBindings, c.ForVar.Value)
				return "", fmt.Errorf("codegen: comprehension value must be constant")
			}
			if c.Cond != nil {
				cv, ok := g.foldConstInt(c.Cond)
				if !ok {
					delete(g.constBindings, c.ForVar.Value)
					return "", fmt.Errorf("codegen: comprehension condition must be constant")
				}
				if cv == 0 {
					delete(g.constBindings, c.ForVar.Value)
					continue
				}
			}
			keys = append(keys, k)
			results = append(results, v)
			delete(g.constBindings, c.ForVar.Value)
		}
		g.dictIdx++
		name = fmt.Sprintf("@.dict%d", g.dictIdx)
		var kparts, vparts []string
		for _, k := range keys {
			kparts = append(kparts, fmt.Sprintf("i32 %d", k))
		}
		for _, v := range results {
			vparts = append(vparts, fmt.Sprintf("i32 %d", v))
		}
		n := len(keys)
		g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32], [%d x i32]} { i32 %d, [%d x i32] [%s], [%d x i32] [%s] }\n", name, n, n, n, n, strings.Join(kparts, ", "), n, strings.Join(vparts, ", ")))
		// record the folded keys so `d[key]` on a dict comprehension can be
		// resolved at codegen time.
		g.compKeys[c] = keys
	default:
		return "", fmt.Errorf("codegen: unsupported comprehension kind %d", c.Kind)
	}
	g.compNames[c] = name
	g.compLen[c] = len(results)
	g.compEls[c] = results
	return name, nil
}

// genExpr lowers a generator expression `(elem for var in iter [if cond])`
// to a runtime heap list handle, matching the interpreter's eager semantics.
// The iterable is unrolled at codegen time when it is a constant range() call
// or list literal (mirroring comp()); each element is appended to the list.
func (g *irGen) genExpr(b *strings.Builder, gen *Generator) (string, error) {
	var items []int64
	if r, ok := gen.Iter.(*Call); ok {
		// the parser represents a range(...) iterable as a Call (mirroring
		// comp()); unroll its (constant) bounds into a slice of items.
		start := int64(0)
		stop, ok := g.foldConstInt(r.Args[0])
		if !ok {
			return "", fmt.Errorf("codegen: generator range() stop must be constant")
		}
		step := int64(1)
		if len(r.Args) > 1 {
			start = stop
			stop, ok = g.foldConstInt(r.Args[1])
			if !ok {
				return "", fmt.Errorf("codegen: generator range() stop must be constant")
			}
		}
		if len(r.Args) > 2 {
			step, ok = g.foldConstInt(r.Args[2])
			if !ok {
				return "", fmt.Errorf("codegen: generator range() step must be constant")
			}
		}
		if step > 0 {
			for v := start; v < stop; v += step {
				items = append(items, v)
			}
		} else {
			for v := start; v > stop; v += step {
				items = append(items, v)
			}
		}
	} else if lst, ok := gen.Iter.(*ListLit); ok {
		for _, el := range lst.Elems {
			v, ok := g.foldConstInt(el)
			if !ok {
				return "", fmt.Errorf("codegen: generator list elements must be constant")
			}
			items = append(items, v)
		}
	}
	g.genExprIdx++
	h := fmt.Sprintf("%%gx%d", g.genExprIdx)
	g.heapUsed = true
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_alloc(i32 1)\n", h))
	if g.constBindings == nil {
		g.constBindings = map[string]int64{}
	}
	for _, item := range items {
		g.constBindings[gen.ForVar.Value] = item
		if gen.Cond != nil {
			if cv, ok := g.foldConstInt(gen.Cond); ok && cv == 0 {
				continue
			}
		}
		ev, ok := g.foldConstInt(gen.Elems[0])
		if !ok {
			evOp, err := g.value(b, gen.Elems[0])
			if err != nil {
				return "", err
			}
			b.WriteString(fmt.Sprintf("  call void @rt_append(i32 %s, i32 %s)\n", h, evOp))
			continue
		}
		b.WriteString(fmt.Sprintf("  call void @rt_append(i32 %s, i32 %d)\n", h, ev))
	}
	g.listOperands[h] = true
	return h, nil
}

// rangeBounds computes the loop start and stop operands for a for statement.
// A `range(a, b)` iterable yields start=a and stop=b; anything else starts at 0
// with stop being the single evaluated bound.
func (g *irGen) rangeBounds(b *strings.Builder, iter Expr) (string, string, string, error) {
	if c, ok := iter.(*Call); ok && c.Fn != nil {
		if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" && (len(c.Args) == 2 || len(c.Args) == 3) {
			lo, err := g.value(b, c.Args[0])
			if err != nil {
				return "", "", "", err
			}
			hi, err := g.value(b, c.Args[1])
			if err != nil {
				return "", "", "", err
			}
			step := "1"
			if len(c.Args) == 3 {
				step, err = g.value(b, c.Args[2])
				if err != nil {
					return "", "", "", err
				}
				if step == "0" {
					return "", "", "", fmt.Errorf("range step cannot be zero")
				}
			}
			return lo, hi, step, nil
		}
	}
	hi, err := g.value(b, iter)
	if err != nil {
		return "", "", "", err
	}
	return "0", hi, "1", nil
}

// call emits a call; supports print/printf and range(n).
func (g *irGen) call(b *strings.Builder, c *Call) (string, error) {
	fnName := ""
	if n, ok := c.Fn.(*Name); ok {
		fnName = n.Value
	} else if lam, ok := c.Fn.(*Lambda); ok {
		// inline lambda callee: `(lambda x: expr)(args)`.
		name, err := g.emitLambda(b, lam)
		if err != nil {
			return "", err
		}
		fnName = name
	}
	// `f(3)` where f was bound to a lambda: resolve to its FuncDef name.
	if fnName != "" {
		if b, ok := g.funcBind[fnName]; ok {
			fnName = b
		}
	}
	if fnName != "" {
		if lamName, ok := g.lambdas[fnName]; ok {
			fnName = lamName
		}
	}
	// constant-fold attr methods on constant receivers.
	if attr, ok := c.Fn.(*Attr); ok {
		// mod.fn(args): dispatch to an AOT module function.
		if modName, isName := attr.Obj.(*Name); isName {
			if modFuncs := g.imports.Funcs[modName.Value]; modFuncs != nil {
				if fd, ok := modFuncs[attr.Name.Value]; ok && fd != nil {
					mangle := modName.Value + "$" + attr.Name.Value
					ret := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = call i32 @%s(", ret, mangle))
					for i, arg := range c.Args {
						if i > 0 {
							b.WriteString(", ")
						}
						av, err := g.value(b, arg)
						if err != nil {
							return "", err
						}
						b.WriteString("i32 " + av)
					}
					b.WriteString(")\n")
					return ret, nil
				}
			}
		}
		mname := attr.Name.Value

		// super().m(args): dispatch m on the base class of the enclosing class.
		if call, isSuper := attr.Obj.(*Call); isSuper && len(call.Args) == 0 {
			if n, isN := call.Fn.(*Name); isN && n.Value == "super" {
				base := ""
				if ci := g.classInfos[g.selfClass]; ci != nil && len(ci.bases) > 0 {
					base = ci.bases[0]
				}
				if fn, ok := g.resolveMethod(base, mname); ok {
					ret := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %%self", ret, fn))
					for _, arg := range c.Args {
						av, err := g.value(b, arg)
						if err != nil {
							return "", err
						}
						b.WriteString(", i32 " + av)
					}
					b.WriteString(")\n")
					return ret, nil
				}
			}
		}

		// Class method dispatch: `recv.m(args)` where recv's class is known.
		if className := g.receiverClass(attr.Obj); className != "" {
			if fn, ok := g.resolveMethod(className, mname); ok {
				recvHandle, err := g.value(b, attr.Obj)
				if err != nil {
					return "", err
				}
				ret := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s", ret, fn, recvHandle))
				for _, arg := range c.Args {
					av, err := g.value(b, arg)
					if err != nil {
						return "", err
					}
					b.WriteString(", i32 " + av)
				}
				b.WriteString(")\n")
				return ret, nil
			}
		}

		// Dynamic dispatch: `recv.m(args)` where recv is a class instance whose
		// class is unknown at compile time. Dispatch on the runtime class-id
		// stored in instance slot 0 (set at instantiation).
		if mname := attr.Name.Value; g.hasMethod(mname) {
			h, _ := g.value(b, attr.Obj)
			// Runtime dispatch operates on the canonical %obj-tagged value
			// representation: the receiver is wrapped as {tag=instance,
			// payload=heap-handle}, its kind tag is verified, and the payload
			// (the instance heap handle) is extracted before the class-id is
			// read from instance slot 0. Both AOT and interpreter derive these
			// tags from the same canonical table (value.go).
			recv := g.newTmp() // %obj
			b.WriteString(fmt.Sprintf("  %s = call %%obj @rt_mkobj(i32 %d, i32 %s)\n", recv, int(TagInstance), h))
			isInst := g.newTmp() // i1
			b.WriteString(fmt.Sprintf("  %s = call i1 @rt_obj_is(%%obj %s, i32 %d)\n", isInst, recv, int(TagInstance)))
			h2 := g.newTmp() // i32 payload = the instance heap handle
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_obj_payload(%%obj %s)\n", h2, recv))
			cid := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 0)\n", cid, h2))
			argvals := make([]string, len(c.Args))
			for i, a := range c.Args {
				v, _ := g.value(b, a)
				argvals[i] = v
			}
			type dynCase struct {
				id           int
				fn, lab, ret string
			}
			var cases []dynCase
			for _, cls := range g.classOrder {
				if fn, ok := g.resolveMethod(cls, mname); ok {
					cases = append(cases, dynCase{g.classIDs[cls], fn, g.newLabel("dyn.c"), g.newTmp()})
				}
			}
			done := g.newLabel("dyn.done")
			miss := g.newLabel("dyn.miss")
			b.WriteString(fmt.Sprintf("  switch i32 %s, label %%%s [\n", cid, miss))
			for _, dc := range cases {
				b.WriteString(fmt.Sprintf("    i32 %d, label %%%s\n", dc.id, dc.lab))
			}
			b.WriteString("  ]\n")
			for _, dc := range cases {
				b.WriteString(fmt.Sprintf("%s:\n", dc.lab))
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s", dc.ret, dc.fn, h))
				for _, av := range argvals {
					b.WriteString(fmt.Sprintf(", i32 %s", av))
				}
				b.WriteString(")\n")
				b.WriteString(fmt.Sprintf("  br label %%%s\n", done))
			}
			b.WriteString(fmt.Sprintf("%s:\n", miss))
			// Runtime miss: the receiver is not an instance of a class that
			// defines this method. Return 0 (the interpreter raises; AOT returns
			// a sentinel since no runtime error infrastructure exists).
			b.WriteString(fmt.Sprintf("  br label %%%s\n", done))
			b.WriteString(fmt.Sprintf("%s:\n", done))
			phi := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = phi i32 [ 0, %%%s ]", phi, miss))
			for _, dc := range cases {
				b.WriteString(fmt.Sprintf(", [ %s, %%%s ]", dc.ret, dc.lab))
			}
			b.WriteString("\n")
			return phi, nil
		}
		if nm, ok := attr.Obj.(*Name); ok && g.runtimeSets[nm.Value] {
			// Set methods on a heap set: without `add` the `set()` constructor produced
			// a value nothing could grow (roadmap Gap K.3).
			switch attr.Name.Value {
			case "add", "discard":
				if len(c.Args) != 1 {
					return "", fmt.Errorf("codegen: %s() takes exactly 1 argument", attr.Name.Value)
				}
				av, interned, err := g.heapElemKind(b, c.Args[0])
				if err != nil {
					return "", err
				}
				if err := g.recordElemKind(nm.Value, "set", interned); err != nil {
					return "", err
				}
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
				fn := "rt_set_add"
				if attr.Name.Value == "discard" {
					fn = "rt_set_discard"
				}
				b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 %s)\n", fn, hs, av))
				if interned {
					// The object, not the variable, records that its members are strings: a helper
					// that fills a container it was handed would otherwise leave the caller's print
					// rendering raw indices (roadmap Gap J.5).
					b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 1)\n", hs))
				}
				return "", nil
			case "clear":
				if len(c.Args) != 0 {
					return "", fmt.Errorf("codegen: clear() takes no arguments")
				}
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
				b.WriteString(fmt.Sprintf("  call void @rt_set_clear(i32 %%h%d)\n", hs))
				return "", nil
			}
		}
		if nm, ok := attr.Obj.(*Name); ok && g.listVars[nm.Value] && attr.Name.Value == "pop" {
			// xs.pop() removes and returns the last element; xs.pop(i) removes index i
			// (negative counts from the end). The bounds test is emitted around the
			// removal so an out-of-range index raises IndexError through the same
			// exception path a `raise` uses (roadmap Gap K.3).
			if len(c.Args) > 1 {
				return "", fmt.Errorf("codegen: pop() takes at most 1 argument (xs.pop(), xs.pop(i))")
			}
			g.heapSeq++
			hs := g.heapSeq
			h := fmt.Sprintf("%%h%d", hs)
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%%s\n", h, "_"+nm.Value))
			n := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_len(i32 %s)\n", n, h))
			idx := g.newTmp()
			badL, okL := g.newLabel("pop.bad"), g.newLabel("pop.ok")
			var bad string
			if len(c.Args) == 1 {
				iv, err := g.value(b, c.Args[0])
				if err != nil {
					return "", err
				}
				neg := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", neg, iv))
				g.markI1(neg)
				adj := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", adj, iv, n))
				b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", idx, neg, adj, iv))
				hi := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, %s\n", hi, idx, n))
				g.markI1(hi)
				lo := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", lo, idx))
				g.markI1(lo)
				bad = g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", bad, lo, hi))
				g.markI1(bad)
			} else {
				// No index: the last element, and an empty list is the error case.
				b.WriteString(fmt.Sprintf("  %s = sub i32 %s, 1\n", idx, n))
				bad = g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", bad, n))
				g.markI1(bad)
			}
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
			b.WriteString(fmt.Sprintf("%s:\n", badL))
			if len(c.Args) == 1 {
				g.raiseTo(b, exnCode("IndexError"), "IndexError", "pop index out of range", c.Span())
			} else {
				g.raiseTo(b, exnCode("IndexError"), "IndexError", "pop from empty list", c.Span())
			}
			b.WriteString(fmt.Sprintf("%s:\n", okL))
			ret := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_pop(i32 %s, i32 %s)\n", ret, h, idx))
			return ret, nil
		}
		if nm, ok := attr.Obj.(*Name); ok && g.listVars[nm.Value] && attr.Name.Value == "append" {
			if len(c.Args) != 1 {
				return "", fmt.Errorf("append expects one argument")
			}
			av, interned, err := g.heapElemKind(b, c.Args[0])
			if err != nil {
				return "", err
			}
			if err := g.recordElemKind(nm.Value, "list", interned); err != nil {
				return "", err
			}
			g.heapSeq++
			hs := g.heapSeq
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
			b.WriteString(fmt.Sprintf("  call void @rt_append(i32 %%h%d, i32 %s)\n", hs, av))
			if interned {
				b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 1)\n", hs))
			}
			return "", nil
		}

		// list method: `[1, 2, 3].append(4)` -> [1, 2, 3, 4].
		if ll, ok := attr.Obj.(*ListLit); ok {
			if attr.Name.Value == "append" {
				if len(c.Args) != 1 {
					return "", fmt.Errorf("append expects one argument")
				}
				elems := append(append([]Expr{}, ll.Elems...), c.Args[0])
				return g.value(b, &ListLit{Elems: elems})
			}
			return "", fmt.Errorf("unsupported list method %s", attr.Name.Value)
		}
		// dict methods: `{1: 2, 3: 4}.keys()` -> [1, 3], `.values()` -> [2, 4].
		if dl, ok := attr.Obj.(*DictLit); ok {
			switch attr.Name.Value {
			case "keys":
				return g.value(b, &ListLit{Elems: dl.Keys})
			case "values":
				return g.value(b, &ListLit{Elems: dl.Vals})
			case "get":
				// dict.get(key, default) returns the value for key or the default.
				if len(c.Args) < 1 || len(c.Args) > 2 {
					return "", fmt.Errorf("get expects 1 or 2 arguments")
				}
				// int key lookup
				if kv, kerr := g.constIntVal(c.Args[0]); kerr == nil {
					for i, k := range dl.Keys {
						if il, ok := k.(*IntLit); ok && il.Value == kv {
							return g.value(b, dl.Vals[i])
						}
					}
				} else if sv, ok := stringConst(c.Args[0]); ok {
					// string key lookup
					for i, k := range dl.Keys {
						if sl, ok := k.(*StrLit); ok && sl.Value == sv {
							return g.value(b, dl.Vals[i])
						}
					}
				}
				// not found: return the default value, if given
				if len(c.Args) == 2 {
					return g.value(b, c.Args[1])
				}
				return "", fmt.Errorf("get: key not found and no default")
			default:
				return "", fmt.Errorf("unsupported dict method %s", attr.Name.Value)
			}
		}
		// string methods: `"AbC".upper()`, `.lower()`, `.strip()`.
		v, ok := g.stringVal(attr.Obj)
		if !ok {
			return "", fmt.Errorf("string method %s on non-constant string", attr.Name.Value)
		}
		switch attr.Name.Value {
		case "upper":
			v = strings.ToUpper(v)
		case "capitalize":
			v = capitalize(v)
		case "title":
			v = title(v)
		case "swapcase":
			v = swapcase(v)
		case "lower":
			v = strings.ToLower(v)
		case "strip":
			v = strings.TrimSpace(v)
		case "lstrip":
			v = strings.TrimLeftFunc(v, unicode.IsSpace)
		case "rstrip":
			v = strings.TrimRightFunc(v, unicode.IsSpace)
		case "replace":
			if len(c.Args) != 2 {
				return "", fmt.Errorf("replace() takes exactly 2 arguments")
			}
			oldv, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("replace() old must be a constant string")
			}
			newv, ok := g.stringVal(c.Args[1])
			if !ok {
				return "", fmt.Errorf("replace() new must be a constant string")
			}
			v = strings.ReplaceAll(v, oldv, newv)
		case "join":
			if len(c.Args) != 1 {
				return "", fmt.Errorf("join() takes exactly 1 argument")
			}
			ll, ok := c.Args[0].(*ListLit)
			if !ok {
				return "", fmt.Errorf("join() argument must be a constant list")
			}
			parts := []string{}
			for _, el := range ll.Elems {
				sv, ok := g.stringVal(el)
				if !ok {
					return "", fmt.Errorf("join() list elements must be constant strings")
				}
				parts = append(parts, sv)
			}
			v = strings.Join(parts, v)
		case "find":
			// s.find(sub) -> index of first occurrence of sub, or -1 if absent.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("find() takes exactly 1 argument")
			}
			subv, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("find() argument must be a constant string")
			}
			return fmt.Sprintf("%d", strings.Index(v, subv)), nil
		case "rfind":
			// s.rfind(sub) -> index of last occurrence of sub, or -1 if absent.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("rfind() takes exactly 1 argument")
			}
			subv, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("rfind() argument must be a constant string")
			}
			return fmt.Sprintf("%d", strings.LastIndex(v, subv)), nil
		case "count":
			// s.count(sub) -> number of non-overlapping occurrences of sub.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("count() takes exactly 1 argument")
			}
			subv, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("count() argument must be a constant string")
			}
			return fmt.Sprintf("%d", strings.Count(v, subv)), nil
		case "isdigit":
			// s.isdigit() -> 1 if all runes are digits, else 0.
			res := 0
			if v != "" {
				all := true
				for _, r := range v {
					if !unicode.IsDigit(r) {
						all = false
						break
					}
				}
				if all {
					res = 1
				}
			}
			return fmt.Sprintf("%d", res), nil
		case "isalpha":
			// s.isalpha() -> 1 if all runes are alphabetic, else 0.
			res := 0
			if v != "" {
				all := true
				for _, r := range v {
					if !unicode.IsLetter(r) {
						all = false
						break
					}
				}
				if all {
					res = 1
				}
			}
			return fmt.Sprintf("%d", res), nil
		case "isalnum":
			res := 0
			if v != "" {
				all := true
				for _, r := range v {
					if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
						all = false
						break
					}
				}
				if all {
					res = 1
				}
			}
			return fmt.Sprintf("%d", res), nil
		case "isspace":
			res := 0
			if v != "" {
				all := true
				for _, r := range v {
					if !unicode.IsSpace(r) {
						all = false
						break
					}
				}
				if all {
					res = 1
				}
			}
			return fmt.Sprintf("%d", res), nil
		case "islower":
			res := 0
			hasCased := false
			allLower := true
			for _, r := range v {
				if unicode.IsLower(r) {
					hasCased = true
				} else if unicode.IsUpper(r) {
					hasCased = true
					allLower = false
				}
			}
			if hasCased && allLower {
				res = 1
			}
			return fmt.Sprintf("%d", res), nil
		case "isupper":
			res := 0
			hasCased := false
			allUpper := true
			for _, r := range v {
				if unicode.IsUpper(r) {
					hasCased = true
				} else if unicode.IsLower(r) {
					hasCased = true
					allUpper = false
				}
			}
			if hasCased && allUpper {
				res = 1
			}
			return fmt.Sprintf("%d", res), nil
		case "startswith", "endswith":
			// s.startswith(sub) / s.endswith(sub) -> 1 or 0.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("%s() takes exactly 1 argument", attr.Name.Value)
			}
			subv, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("%s() argument must be a constant string", attr.Name.Value)
			}
			res := 0
			if attr.Name.Value == "startswith" {
				if strings.HasPrefix(v, subv) {
					res = 1
				}
			} else if strings.HasSuffix(v, subv) {
				res = 1
			}
			return fmt.Sprintf("%d", res), nil
		case "ljust", "rjust":
			// ljust pads the receiver on the right with spaces to width w;
			// rjust pads on the left (no-op when len(v) >= w).
			if len(c.Args) != 1 {
				return "", fmt.Errorf("%s expects one argument", attr.Name.Value)
			}
			wv, werr := g.constIntVal(c.Args[0])
			if werr != nil {
				return "", fmt.Errorf("%s: codegen folds only a constant width arg", attr.Name.Value)
			}
			pad := int(wv) - len(v)
			if pad <= 0 {
				return g.strConst(v), nil
			}
			spaces := strings.Repeat(" ", pad)
			if attr.Name.Value == "ljust" {
				return g.strConst(v + spaces), nil
			}
			return g.strConst(spaces + v), nil
		case "zfill":
			// zfill pads the receiver on the left with '0' to width w
			// (no-op when len(v) >= w), mirroring the interpreter.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("zfill expects one argument")
			}
			wv, werr := g.constIntVal(c.Args[0])
			if werr != nil {
				return "", fmt.Errorf("zfill: codegen folds only a constant width arg")
			}
			pad := int(wv) - len(v)
			if pad <= 0 {
				return g.strConst(v), nil
			}
			return g.strConst(strings.Repeat("0", pad) + v), nil
		case "removeprefix", "removesuffix":
			// removeprefix strips the given prefix from the receiver;
			// removesuffix strips the suffix, mirroring strings.TrimPrefix/TrimSuffix.
			if len(c.Args) != 1 {
				return "", fmt.Errorf("%s expects one argument", attr.Name.Value)
			}
			sub, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("%s: codegen folds only a constant string arg", attr.Name.Value)
			}
			var res string
			if attr.Name.Value == "removeprefix" {
				res = strings.TrimPrefix(v, sub)
			} else {
				res = strings.TrimSuffix(v, sub)
			}
			return g.strConst(res), nil
		case "index":
			// "s".index(sub) returns the byte index of sub (strings.Index).
			// The interpreter raises on not-found; the AOT codegen has no error
			// channel, so it folds to -1 on not-found (like find).
			if len(c.Args) != 1 {
				return "", fmt.Errorf("index expects one argument")
			}
			sub, ok := g.stringVal(c.Args[0])
			if !ok {
				return "", fmt.Errorf("index: codegen folds only a constant string arg")
			}
			return fmt.Sprintf("%d", int64(strings.Index(v, sub))), nil
		case "expandtabs":
			// expandtabs replaces each tab with the spaces up to the next tab
			// stop at width w, tracking the running column, mirroring the
			// interpreter's tab-stop algorithm. Source string literals have no
			// escape sequences, so literal receivers contain no tabs (no-op).
			if len(c.Args) != 1 {
				return "", fmt.Errorf("expandtabs expects one argument")
			}
			wv, werr := g.constIntVal(c.Args[0])
			if werr != nil {
				return "", fmt.Errorf("expandtabs: codegen folds only a constant width arg")
			}
			w := int(wv)
			if w <= 0 {
				return "", fmt.Errorf("expandtabs width must be positive")
			}
			var b strings.Builder
			col := 0
			for _, r := range v {
				if r == '\t' {
					n := w - (col % w)
					b.WriteString(strings.Repeat(" ", n))
					col += n
				} else {
					b.WriteRune(r)
					col++
				}
			}
			return g.strConst(b.String()), nil
		default:
			return "", fmt.Errorf("unsupported string method %s", attr.Name.Value)
		}
		return g.strConst(v), nil
	}

	// Class instantiation: `ClassName(args)`.
	if _, isClass := g.classInfos[fnName]; isClass {
		g.heapUsed = true
		h := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_alloc(i32 4)\n", h))
		b.WriteString(fmt.Sprintf("  call void @rt_inst_put(i32 %s, i32 0, i32 %d)\n", h, g.classIDs[fnName]))
		if fn, ok := g.resolveMethod(fnName, "__init__"); ok {
			// Compute each argument value first (each emits its own load
			// statement on a fresh line), then emit the call using the
			// computed temporaries so the IR stays well-formed.
			argTmp := make([]string, len(c.Args))
			for i, arg := range c.Args {
				av, err := g.value(b, arg)
				if err != nil {
					return "", err
				}
				argTmp[i] = av
			}
			b.WriteString(fmt.Sprintf("  call i32 @%s(i32 %s", fn, h))
			for _, av := range argTmp {
				b.WriteString(", i32 " + av)
			}
			b.WriteString(")\n")
		}
		return h, nil
	}

	if ed, ok := g.externs[fnName]; ok {
		if len(ed.Params) != len(c.Args) {
			panic(fmt.Sprintf("extern function %q: expected %d args, got %d", fnName, len(ed.Params), len(c.Args)))
		}
		argTypes := []string{}
		argRegs := []string{}
		for i, p := range ed.Params {
			if p.Annot != nil && p.Annot.Kind == KindString {
				lit, ok2 := c.Args[i].(*StrLit)
				if !ok2 {
					panic("extern string args must be string literals: " + fnName)
				}
				gn := g.strConst(lit.Value)
				n := len(lit.Value)
				argTypes = append(argTypes, "i8*")
				argRegs = append(argRegs, fmt.Sprintf("getelementptr([%d x i8], [%d x i8]* %s, i32 0, i32 0)", n, n, gn))
			} else {
				v, err := g.value(b, c.Args[i])
				if err != nil {
					return "", err
				}
				argTypes = append(argTypes, "i32")
				argRegs = append(argRegs, v)
			}
		}
		ret := "i32"
		if ed.ReturnAnno != nil && ed.ReturnAnno.Kind == KindString {
			ret = "i8*"
		}
		callArgs := []string{}
		for i := range argTypes {
			callArgs = append(callArgs, argTypes[i]+" "+argRegs[i])
		}
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", t, ret, fnName, strings.Join(callArgs, ", ")))
		return t, nil
	}
	if g.curModName != "" && g.imports != nil {
		if sibling := g.imports.Funcs[g.curModName]; sibling != nil {
			if fd, ok := sibling[fnName]; ok && fd != nil {
				mangle := g.curModName + "$" + fnName
				ret := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(", ret, mangle))
				for i, arg := range c.Args {
					if i > 0 {
						b.WriteString(", ")
					}
					av, err := g.value(b, arg)
					if err != nil {
						return "", err
					}
					b.WriteString("i32 " + av)
				}
				b.WriteString(")\n")
				return ret, nil
			}
		}
	}
	if g.funcs[fnName] {
		fd := g.fds[fnName]
		if fd == nil {
			return "", fmt.Errorf("codegen: unknown function %q", fnName)
		}
		n := len(fd.Params)
		isFloat := g.floatFuncs[fnName]
		vals := make([]string, n)
		// A container literal argument is materialised into the runtime heap and
		// passed by handle; the callee declares the matching parameter as a
		// container (see declareHeapParams / heapargs.go).
		// strArgCheck reports the AOT limitation before emitting IR that LLVM
		// would reject: a string is an `i8*` global in this backend, so passing
		// one where the callee expects an `i32` produces
		// `call i32 @f(i32 @.str1)` — "global variable reference must have
		// pointer type". Naming the parameter keeps the message actionable.
		strArgCheck := func(a Expr, idx int) error {
			if _, isStr := g.stringVal(a); !isStr {
				if _, isFs := a.(*FString); !isFs {
					return nil
				}
			}
			what := "an argument"
			if idx >= 0 && idx < len(fd.Params) {
				what = fmt.Sprintf("parameter %q of %s", fd.Params[idx].Name, fnName)
			}
			return fmt.Errorf("codegen: strings are not supported as function arguments in the AOT backend yet (%s); the interpreter supports them", what)
		}
		argVal := func(a Expr, idx int) (string, error) {
			if h, ok, err := g.heapArg(b, a); ok || err != nil {
				if err != nil {
					return "", err
				}
				return h, nil
			}
			// A string argument to a string parameter becomes its @str_tab index: the
			// parameter is an i32 slot, and `call i32 @f(i32 @.strN)` is what LLVM rejects
			// (Gap J.5). A forwarded string (already an index) passes straight through.
			if idx >= 0 && g.strParamOf[fnName][idx] {
				if nm, ok := a.(*Name); ok && g.internedVars[nm.Value] {
					return g.value(b, a)
				}
				if txt, ok := g.stringVal(a); ok {
					g.heapUsed = true
					t := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", t, g.strConst(txt), g.strConst(pyReprString(txt))))
					return t, nil
				}
			}
			if err := strArgCheck(a, idx); err != nil {
				return "", err
			}
			v, err := g.value(b, a)
			if err != nil {
				return "", err
			}
			// A compile-time list global (@.lstN, e.g. a folded comprehension)
			// is not an i32 handle: copy it into the runtime heap so the callee
			// receives a handle it can iterate, measure and index.
			if ln, ok := g.staticLists[v]; ok {
				return g.heapListFrom(b, ln, "")
			}
			return v, nil
		}
		// A helper that fills a container it was handed tells the caller what its elements are:
		// `def fill(out, v): out.append(v)` called as `fill(names, "one")` is what makes
		// `print(names[1])` render text instead of the raw string-table index (Gap J.5).
		if fills := g.strFillOf[fnName]; len(fills) > 0 {
			fillPos := 0
			for _, a := range c.Args {
				if kw, ok := a.(*KeywordArg); ok {
					for i, p := range fd.Params {
						if p.Name == kw.Name {
							g.applyStrFill(fnName, i, kw.Value)
						}
					}
					continue
				}
				g.applyStrFill(fnName, fillPos, a)
				fillPos++
			}
		}
		provided := make([]bool, n)
		pos := 0
		seenKw := false
		for _, a := range c.Args {
			if kw, ok := a.(*KeywordArg); ok {
				seenKw = true
				idx := -1
				for i, p := range fd.Params {
					if p.Name == kw.Name {
						idx = i
						break
					}
				}
				if idx < 0 {
					return "", fmt.Errorf("codegen: unknown keyword argument %q for %s", kw.Name, fnName)
				}
				if provided[idx] {
					return "", fmt.Errorf("codegen: multiple values for argument %q of %s", kw.Name, fnName)
				}
				if isFloat {
					fv := g.floatValue(b, kw.Value)
					vals[idx] = "double " + fv
				} else {
					av, err := argVal(kw.Value, idx)
					if err != nil {
						return "", err
					}
					vals[idx] = "i32 " + av
				}
				provided[idx] = true
				continue
			}
			if seenKw {
				return "", fmt.Errorf("codegen: positional argument after keyword argument for %s", fnName)
			}
			if pos >= n {
				return "", fmt.Errorf("codegen: too many arguments for %s", fnName)
			}
			if provided[pos] {
				return "", fmt.Errorf("codegen: multiple values for argument %q of %s", fd.Params[pos].Name, fnName)
			}
			if isFloat {
				fv := g.floatValue(b, a)
				vals[pos] = "double " + fv
			} else {
				av, err := argVal(a, pos)
				if err != nil {
					return "", err
				}
				vals[pos] = "i32 " + av
			}
			provided[pos] = true
			pos++
		}
		// fill defaults for params not supplied
		for i := range fd.Params {
			if provided[i] {
				continue
			}
			if fd.Params[i].Default == nil {
				return "", fmt.Errorf("codegen: missing argument %q for %s", fd.Params[i].Name, fnName)
			}
			if isFloat {
				fv := g.floatValue(b, fd.Params[i].Default)
				vals[i] = "double " + fv
			} else {
				dv, err := argVal(fd.Params[i].Default, i)
				if err != nil {
					return "", err
				}
				vals[i] = "i32 " + dv
			}
		}
		t := g.newTmp()
		if _, ok := g.closures[fnName]; ok {
			env := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* @%s_slot\n", env, fnName))
			callArgs := append([]string{"i32 " + env}, vals...)
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s_env(%s)\n", t, fnName, strings.Join(callArgs, ", ")))
			g.checkExn(b)
		} else if g.decorated[fnName] {
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s_impl(%s)\n", t, fnName, strings.Join(vals, ", ")))
			g.checkExn(b)
		} else {
			if isFloat {
				b.WriteString(fmt.Sprintf("  %s = call double @%s(%s)\n", t, fnName, strings.Join(vals, ", ")))
				if g.floatTemps == nil {
					g.floatTemps = map[string]bool{}
				}
				g.floatTemps[t] = true
			} else {
				// FFI: call to an extern (C) function — marshal values to native types.
				if ed, ok := g.externs[fnName]; ok {
					if len(ed.Params) != len(c.Args) {
						panic(fmt.Sprintf("extern function %q: expected %d args, got %d", fnName, len(ed.Params), len(c.Args)))
					}
					argTypes := []string{}
					argRegs := []string{}
					for i, p := range ed.Params {
						if p.Annot != nil && p.Annot.Kind == KindString {
							lit, ok2 := c.Args[i].(*StrLit)
							if !ok2 {
								panic("extern string args must be string literals: " + fnName)
							}
							gn := g.strConst(lit.Value) // ensure global @.strN exists
							n := len(lit.Value)
							argTypes = append(argTypes, "i8*")
							argRegs = append(argRegs, fmt.Sprintf("i8* getelementptr([%d x i8], [%d x i8]* %s, i32 0, i32 0)", n, n, gn))
						} else {
							v, err := g.value(b, c.Args[i])
							if err != nil {
								return "", err
							}
							argTypes = append(argTypes, "i32")
							argRegs = append(argRegs, "i32 "+v)
						}
					}
					ret := "i32"
					if ed.ReturnAnno != nil && ed.ReturnAnno.Kind == KindString {
						ret = "i8*"
					}
					callArgs := []string{}
					for i := range argTypes {
						callArgs = append(callArgs, argTypes[i]+" "+argRegs[i])
					}
					b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", t, ret, fnName, strings.Join(callArgs, ", ")))
					return t, nil
				}
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(%s)\n", t, fnName, strings.Join(vals, ", ")))
			}
			g.checkExn(b)
			// a generator function returns a runtime heap list handle
			if g.genFuncs[fnName] {
				g.listOperands[t] = true
			}
		}
		return t, nil
	}
	switch fnName {
	case "print", "printf":
		// print(*args, sep=" ", end="\n") — Python's separator/terminator
		// semantics, identical on both backends (ADR 0165). An argument is
		// written WITHOUT its own terminator; `sep` is emitted between arguments
		// and `end` after the last one. So print("a =", n) is one line, print()
		// is a blank line, and print(x, end="") keeps the line open.
		sep, end := " ", "\n"
		var last string
		// emitText writes a literal string (the separator / terminator). `%` is
		// doubled so printf treats it as text, not as a directive.
		emitText := func(text string) {
			name, size := g.fmtStr(strings.ReplaceAll(text, "%", "%%"))
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0))\n", t, size, size, name))
			last = t
		}
		args := make([]Expr, 0, len(c.Args))
		for _, a := range c.Args {
			kw, isKw := a.(*KeywordArg)
			if !isKw {
				args = append(args, a)
				continue
			}
			sl, isStr := kw.Value.(*StrLit)
			if !isStr {
				return "", fmt.Errorf("codegen: print's %s must be a compile-time string constant (the interpreter accepts any expression)", kw.Name)
			}
			switch kw.Name {
			case "sep":
				sep = sl.Value
			case "end":
				end = sl.Value
			default:
				return "", fmt.Errorf("codegen: print got an unexpected keyword argument %q", kw.Name)
			}
		}
		if len(args) < 1 {
			// print() writes just the terminator: a blank line, like Python.
			emitText(end)
			return last, nil
		}
		for i, a := range args {
			if i > 0 {
				emitText(sep)
			}
			if nm, ok := a.(*Name); ok && g.unionVars[nm.Value] {
				g.emitUnionPrint(b, nm.Value, "")
				continue
			}
			// print a constant string: literals and folded string-method results.
			if _, ok := g.stringVal(a); ok {
				fmtName, size := g.fmtStr("%s")
				v, err := g.value(b, a)
				if err != nil {
					return "", err
				}
				t := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i8* %s)\n", t, size, size, fmtName, v))
				last = t
				continue
			}
			// an f-string: print each constant part as a string and each
			// interpolated expression as its value (no runtime string type).
			// All parts are combined into a single printf call so the output
			// matches the interpreter's one-string Repr.
			if fs, ok := a.(*FString); ok {
				var fmtLit string
				var operands []string
				for _, part := range fs.Parts {
					if part.Lit != "" {
						// escape % as %% so printf shows a literal %
						fmtLit += strings.ReplaceAll(part.Lit, "%", "%%")
						continue
					}
					if part.Expr != nil {
						if g.isFloat(part.Expr) {
							// %s of rt_fmt_double's text, not %.17g: Python's str(0.1) is
							// "0.1" and str(2.0) is "2.0".
							fmtLit += "%s"
							fv := g.floatValue(b, part.Expr)
							g.floatFmtUsed = true
							fs := g.newTmp()
							b.WriteString(fmt.Sprintf("  %s = call i8* @rt_fmt_double(double %s)\n", fs, fv))
							operands = append(operands, "i8* "+fs)
						} else {
							fmtLit += "%d"
							vv, err := g.value(b, part.Expr)
							if err != nil {
								return "", err
							}
							operands = append(operands, "i32 "+vv)
						}
					}
				}
				fmtName, size := g.fmtStr(fmtLit)
				t := g.newTmp()
				callArgs := fmt.Sprintf("i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0)", size, size, fmtName)
				if len(operands) > 0 {
					callArgs += ", " + strings.Join(operands, ", ")
				}
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(%s)\n", t, callArgs))
				last = t
				continue
			}
			if nm, ok := a.(*Name); ok {
				if g.runtimeDicts[nm.Value] {
					g.heapSeq++
					hs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
					if g.dictKeyStr[nm.Value] || g.dictValStr[nm.Value] {
						// keys and/or values are @str_tab indices: the flagged printer renders
						// {'a': 1} / {1: 'a'} exactly as the interpreter's Repr does (Gap I.2).
						ks, vs := "0", "0"
						if g.dictKeyStr[nm.Value] {
							ks = "1"
						}
						if g.dictValStr[nm.Value] {
							vs = "1"
						}
						b.WriteString(fmt.Sprintf("  call void @rt_dict_print_s(i32 %%h%d, i32 0, i32 %s, i32 %s)\n", hs, ks, vs))
					} else {
						b.WriteString(fmt.Sprintf("  call void @rt_dict_print(i32 %%h%d, i32 0)\n", hs))
					}
					continue
				}
				if g.runtimeSets[nm.Value] {
					g.heapSeq++
					hs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
					printer := "rt_set_print"
					if g.setElemStr[nm.Value] {
						printer = "rt_set_print_str"
					}
					b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 0)\n", printer, hs))
					continue
				}
			}
			// None prints as "None" in both backends. The decision is static, like every
			// other print format here: values are untagged i32s, so codegen — not the
			// runtime — knows which expressions are the None singleton (ADR 0172).
			if g.isNoneExpr(a) {
				g.heapUsed = true // the None printer lives in the heap runtime module
				// Evaluate the argument first: `print(emit())` must run emit() (and any
				// output it produces) before writing None, exactly as the interpreter
				// interleaves them. Skipping the call is the classic way to lose output.
				if _, isLit := a.(*NoneLit); !isLit {
					if _, err := g.value(b, a); err != nil {
						return "", err
					}
				}
				b.WriteString("  call void @rt_print_none(i32 0)\n")
				continue
			}
			if g.printsAsInternedStr(a) {
				g.heapUsed = true
				v, err := g.value(b, a)
				if err != nil {
					return "", err
				}
				fmt.Fprintf(b, "  call void @rt_print_str(i32 %s, i32 0)\n", v)
				continue
			}
			if sl, ok := a.(*Slice); ok {
				if nm2, ok2 := sl.Obj.(*Name); ok2 {
					if _, isList := g.listVars[nm2.Value]; isList {
						h, err := g.value(b, sl)
						if err != nil {
							return "", err
						}
						printer := "rt_print_list"
						if g.listElemStr[nm2.Value] {
							printer = "rt_print_list_str"
						}
						fmt.Fprintf(b, "  call void @%s(i32 %s, i32 0)\n", printer, h)
						continue
					}
				}
			}
			if nm, ok := a.(*Name); ok && g.listVars[nm.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
				printer := "rt_print_list"
				if g.listElemStr[nm.Value] {
					printer = "rt_print_list_str"
				}
				b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 0)\n", printer, hs))
				continue
			}
			var t string
			if g.isFloat(a) {
				// Python renders a float as its shortest round-tripping text with a
				// ".0" when integral (rt_fmt_double); printf's %.17g invented digits
				// and %g truncated them, and neither marked 2.0 as a float.
				fmtName, size := g.fmtStr("%s")
				fv := g.floatValue(b, a)
				g.floatFmtUsed = true
				fs := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i8* @rt_fmt_double(double %s)\n", fs, fv))
				t = g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i8* %s)\n", t, size, size, fmtName, fs))
			} else {
				// A container literal whose elements are strings builds a heap object (the static
				// global layout is i32-only), so print it with the runtime printers rather than
				// as the integer handle it happens to be (roadmap Gap J.6).
				if literalNeedsHeap(a) {
					if literalMixedKinds(a) {
						noun := "list"
						switch a.(type) {
						case *SetLit:
							noun = "set"
						case *DictLit:
							noun = "dict"
						}
						return "", mixedKindErr(noun)
					}
					g.heapUsed = true
					switch lit := a.(type) {
					case *ListLit:
						h, err := g.heapListFrom(b, lit, "")
						if err != nil {
							return "", err
						}
						b.WriteString(fmt.Sprintf("  call void @rt_print_list(i32 %s, i32 0)\n", h))
					case *SetLit:
						h, err := g.heapSetFrom(b, lit, "")
						if err != nil {
							return "", err
						}
						b.WriteString(fmt.Sprintf("  call void @rt_set_print(i32 %s, i32 0)\n", h))
					case *DictLit:
						h, err := g.heapDictFrom(b, lit, "")
						if err != nil {
							return "", err
						}
						b.WriteString(fmt.Sprintf("  call void @rt_dict_print(i32 %s, i32 0)\n", h))
					}
					continue
				}
				v, err := g.value(b, a)
				if err != nil {
					return "", err
				}
				// a generator result is a runtime heap list handle: print it
				// as a list rather than an int.
				if g.listOperands[v] {
					b.WriteString(fmt.Sprintf("  call void @rt_print_list(i32 %s, i32 0)\n", v))
					continue
				}
				fmtName, size := g.fmtStr("%d")
				t := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i32 %s)\n", t, size, size, fmtName, v))
			}
			if i == len(args)-1 {
				last = t
			}
		}
		emitText(end)
		return last, nil
	case "set", "list", "dict":
		// Container constructors. `{}` is already the empty dict and `{1, 2}` a set,
		// but the empty *set* has no literal at all, so `set()` is the only way to
		// write one; `list()`/`dict()` are the matching spellings (roadmap Gap K.3).
		// Lowering to the corresponding empty literal keeps one allocation path.
		if len(c.Args) > 1 {
			return "", fmt.Errorf("codegen: %s() takes at most 1 argument", calleeName(c))
		}
		if len(c.Args) == 1 {
			// Copying another container is a loop over its elements; the interpreter
			// supports it, so say which backend does rather than miscompile (ADR 0166).
			return "", fmt.Errorf("codegen: %s(<container>) copies are not supported in the AOT backend yet; the interpreter supports them — build the container with %s() and add elements", calleeName(c), calleeName(c))
		}
		// Allocate a fresh heap container. Lowering to an empty literal instead would
		// hand back a compile-time global (@.set1), and `s = set()` would then store that
		// global into the variable slot — the folded-global bug this repo has been bitten
		// by before (ADR 0163). rt_alloc zeroes the length in both the fresh and the
		// recycled path, so the handle it returns is an empty container.
		g.heapSeq++
		hs := g.heapSeq
		kind := 3 // set
		switch c.Fn.(*Name).Value {
		case "list":
			kind = 1
		case "dict":
			kind = 2
		}
		g.heapUsed = true
		b.WriteString(fmt.Sprintf("  %%h%d = call i32 @rt_alloc(i32 %d)\n", hs, kind))
		return fmt.Sprintf("%%h%d", hs), nil
	case "len":
		// len(string-constant) -> compile-time character count; otherwise
		// len(list/dict/set) loads the count field (i32 0) of the inline
		// literal's global struct. Layouts share the count as the first field.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("len expects one argument")
		}
		if n, ok := stringConstLen(c.Args[0]); ok {
			return fmt.Sprintf("%d", n), nil
		}
		// imported string module global (data imports): len(mod.str)
		if attr, ok := c.Args[0].(*Attr); ok {
			if nm, ok2 := attr.Obj.(*Name); ok2 {
				if globals, ok3 := g.imports.Globals[nm.Value]; ok3 {
					if lit, ok4 := globals[attr.Name.Value]; ok4 {
						if str, ok5 := lit.(*StrLit); ok5 {
							return fmt.Sprintf("%d", len(str.Value)), nil
						}
						if lst, ok5 := lit.(*ListLit); ok5 {
							return fmt.Sprintf("%d", len(lst.Elems)), nil
						}
						if dct, ok5 := lit.(*DictLit); ok5 {
							return fmt.Sprintf("%d", len(dct.Keys)), nil
						}
					}
				}
			}
		}
		switch lit := c.Args[0].(type) {
		case *Name:
			if g.strVals != nil {
				if sv, ok := g.strVals[lit.Value]; ok {
					return fmt.Sprintf("%d", len(sv)), nil
				}
			}
			// An interned string (parameter, container element, loop variable) has no
			// compile-time text: measure it through the runtime table (Gap J.5).
			if g.internedVars[lit.Value] {
				g.heapUsed = true
				v, err := g.value(b, lit)
				if err != nil {
					return "", err
				}
				t := g.newTmp()
				fmt.Fprintf(b, "  %s = call i32 @rt_str_len(i32 %s)\n", t, v)
				return t, nil
			}
			if g.listVars[lit.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, lit.Value))
				b.WriteString(fmt.Sprintf("  %%l%d = call i32 @rt_list_len(i32 %%h%d)\n", hs, hs))
				return fmt.Sprintf("%%l%d", hs), nil
			}
			if g.runtimeDicts[lit.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, lit.Value))
				b.WriteString(fmt.Sprintf("  %%l%d = call i32 @rt_dict_len(i32 %%h%d)\n", hs, hs))
				return fmt.Sprintf("%%l%d", hs), nil
			}
			if g.runtimeSets[lit.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, lit.Value))
				b.WriteString(fmt.Sprintf("  %%l%d = call i32 @rt_set_len(i32 %%h%d)\n", hs, hs))
				return fmt.Sprintf("%%l%d", hs), nil
			}
			return "", fmt.Errorf("len of a non-string variable")

		case *ListLit:
			// len of a list literal is the element count; no element lowering needed.
			return fmt.Sprintf("%d", len(lit.Elems)), nil
		case *DictLit:
			// A dict literal with strings is a heap object (its keys/values are interned),
			// so measure it through the runtime like any other dict (Gap J.6).
			if literalNeedsHeap(lit) {
				h, err := g.heapDictFrom(b, lit, "")
				if err != nil {
					return "", err
				}
				v := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_dict_len(i32 %s)\n", v, h))
				return v, nil
			}
			name, err := g.emitDict(lit)
			if err != nil {
				return "", err
			}
			n := len(lit.Keys)
			v := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32], [%d x i32]}, {i32, [%d x i32], [%d x i32]}* %s, i32 0, i32 0)\n", v, n, n, n, n, name))
			return v, nil
		case *SetLit:
			if literalNeedsHeap(lit) {
				h, err := g.heapSetFrom(b, lit, "")
				if err != nil {
					return "", err
				}
				v := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_set_len(i32 %s)\n", v, h))
				return v, nil
			}
			name, err := g.emitSet(lit)
			if err != nil {
				return "", err
			}
			n := len(lit.Elems)
			v := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32]}, {i32, [%d x i32]}* %s, i32 0, i32 0)\n", v, n, n, name))
			return v, nil
		case *Comp:
			// lowered comprehension result: same global shape as a list literal,
			// so len loads the stored count field.
			name, err := g.comp(b, lit)
			if err != nil {
				return "", err
			}
			n := g.compLen[lit]
			v := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32]}, {i32, [%d x i32]}* %s, i32 0, i32 0)\n", v, n, n, name))
			return v, nil
		case *Call:
			if elems, ok := g.dictMethodElems(c.Args[0]); ok {
				return fmt.Sprintf("%d", len(elems)), nil
			}
			if elems, ok := listCallElems(c.Args[0]); ok {
				return fmt.Sprintf("%d", len(elems)), nil
			}
			if n, ok := g.listLen(c.Args[0]); ok {
				return fmt.Sprintf("%d", n), nil
			}
			return "", fmt.Errorf("len requires an inline list/dict/set literal")
		default:
			return "", fmt.Errorf("len requires an inline list/dict/set literal")
		}
	case "any", "all":
		// any(iter) is 1 if any element is nonzero; all(iter) is 1 if all are.
		anyMode := fnName == "any"
		var elems []Expr
		switch a := c.Args[0].(type) {
		case *ListLit:
			elems = a.Elems
		case *SetLit:
			elems = a.Elems
		case *Call:
			if le, ok := listCallElems(a); ok {
				elems = le
			} else {
				return "", fmt.Errorf("any/all need a list literal")
			}
		default:
			return "", fmt.Errorf("any/all need a list literal")
		}
		if len(elems) == 0 {
			if anyMode {
				return "0", nil
			}
			return "1", nil
		}
		b0, err := g.value(b, elems[0])
		if err != nil {
			return "", err
		}
		acc := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", acc, b0))
		for i := 1; i < len(elems); i++ {
			el, err := g.value(b, elems[i])
			if err != nil {
				return "", err
			}
			bi := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", bi, el))
			op := "or"
			if !anyMode {
				op = "and"
			}
			a2 := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = %s i1 %s, %s\n", a2, op, acc, bi))
			acc = a2
		}
		// any/all yield an i1 accumulator; widen to i32 so callers (e.g. print)
		// can consume it as an integer 0/1.
		res := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", res, acc))
		return res, nil

	case "sum":
		// sum(list) -> sum the elements of an inline list literal (unrolled).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("sum expects one argument")
		}
		// sum over a lowered comprehension: the elements are folded constants,
		// so fold to a single constant at codegen time.
		if comp, ok := c.Args[0].(*Comp); ok {
			if _, err := g.comp(b, comp); err != nil {
				return "", err
			}
			total := int64(0)
			for _, e := range g.compEls[comp] {
				total += e
			}
			return fmt.Sprintf("%d", total), nil
		}
		var elems []Expr
		if de, ok := g.dictMethodElems(c.Args[0]); ok {
			elems = de
		}
		if le, ok := listCallElems(c.Args[0]); ok {
			elems = le
		}
		if ln, ok := c.Args[0].(*ListLit); ok {
			elems = ln.Elems
		} else if sl, ok := c.Args[0].(*SetLit); ok {
			elems = sl.Elems
		} else if dl, ok := c.Args[0].(*DictLit); ok {
			if len(dl.Keys) > 0 {
				// The interpreter rejects non-empty dict literals for sum.
				return "", fmt.Errorf("sum expects a list or set")
			}
			elems = dl.Keys
		}
		if elems == nil {
			// Empty inline list/set/dict literals are valid (sum([]) -> 0),
			// so promote a nil slice to an empty one for those receivers.
			switch c.Args[0].(type) {
			case *ListLit, *SetLit, *DictLit:
				elems = []Expr{}
			default:
				return "", fmt.Errorf("sum requires an inline list/set/dict literal")
			}
		}
		if len(elems) == 0 {
			// sum([]) folds to 0, matching the interpreter.
			return "0", nil
		}
		anyFloat := false
		fvals := make([]float64, 0, len(elems))
		for _, elem := range elems {
			if g.isFloat(elem) {
				anyFloat = true
			}
			if fv, ok := g.floatEval(elem); ok {
				fvals = append(fvals, fv)
			}
		}
		if anyFloat {
			if len(fvals) == len(elems) {
				total := 0.0
				for _, fv := range fvals {
					total += fv
				}
				t := g.newTmp()
				fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(total))
				return t, nil
			}
		}

		acc, err := g.value(b, elems[0])
		if err != nil {
			return "", err
		}
		for i := 1; i < len(elems); i++ {
			el, err := g.value(b, elems[i])
			if err != nil {
				return "", err
			}
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", t, acc, el))
			acc = t
		}
		return acc, nil
	case "min", "max":
		// min/max(list) -> fold the elements of an inline list literal (unrolled).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("%s expects one argument", fnName)
		}
		// min/max accept a single scalar value (treated as a one-element
		// collection): min(5) -> 5, max(7) -> 7, matching the interpreter.
		if il, ok := c.Args[0].(*IntLit); ok {
			return fmt.Sprintf("%d", il.Value), nil
		}
		// min/max over a lowered comprehension: fold the constant elements.
		if comp, ok := c.Args[0].(*Comp); ok {
			if _, err := g.comp(b, comp); err != nil {
				return "", err
			}
			els := g.compEls[comp]
			if len(els) == 0 {
				return "", fmt.Errorf("%s of an empty comprehension", fnName)
			}
			best := els[0]
			for _, e := range els[1:] {
				if fnName == "min" && e < best {
					best = e
				}
				if fnName == "max" && e > best {
					best = e
				}
			}
			return fmt.Sprintf("%d", best), nil
		}
		var elems []Expr
		if de, ok := g.dictMethodElems(c.Args[0]); ok {
			elems = de
		}
		if le, ok := listCallElems(c.Args[0]); ok {
			elems = le
		}
		if ln, ok := c.Args[0].(*ListLit); ok {
			elems = ln.Elems
		} else if sl, ok := c.Args[0].(*SetLit); ok {
			elems = sl.Elems
		} else if dl, ok := c.Args[0].(*DictLit); ok {
			if len(dl.Keys) > 0 {
				// The interpreter rejects non-empty dict literals for min/max.
				return "", fmt.Errorf("%s expects a list or set", fnName)
			}
			elems = dl.Keys
		}
		if elems == nil {
			return "", fmt.Errorf("%s requires an inline list/set/dict literal", fnName)
		}
		if len(elems) == 0 {
			return "", fmt.Errorf("%s of an empty list", fnName)
		}
		best, err := g.value(b, elems[0])
		if err != nil {
			return "", err
		}
		for i := 1; i < len(elems); i++ {
			el, err := g.value(b, elems[i])
			if err != nil {
				return "", err
			}
			cmp := g.newTmp()
			op := "icmp sgt"
			if fnName == "min" {
				op = "icmp slt"
			}
			b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", cmp, op, el, best))
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, cmp, el, best))
			best = t
		}
		return best, nil
	case "abs":
		// abs(x) -> x < 0 ? -x : x (constant-folded when x is a literal).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("abs expects one argument")
		}
		if g.isFloat(c.Args[0]) {
			if fv, ok := g.floatEval(c.Args[0]); ok {
				if fv < 0 {
					fv = -fv
				}
				t := g.newTmp()
				fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(fv))
				return t, nil
			}
		}

		if il, ok := c.Args[0].(*IntLit); ok {
			if il.Value < 0 {
				return fmt.Sprintf("%d", -il.Value), nil
			}
			return fmt.Sprintf("%d", il.Value), nil
		}
		v, err := g.value(b, c.Args[0])
		if err != nil {
			return "", err
		}
		neg := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = sub i32 0, %s\n", neg, v))
		cmp := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", cmp, v))
		t := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, cmp, neg, v))
		return t, nil
	case "sqrt":
		// sqrt promotes its argument to float and returns the square root.
		if fv, ok := g.floatEval(c.Args[0]); ok {
			if fv < 0 {
				return "", fmt.Errorf("sqrt: cannot take square root of a negative number")
			}
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(math.Sqrt(fv)))
			return t, nil
		}
		fx := g.floatValue(b, c.Args[0])
		rt := g.newTmp()
		fmt.Fprintf(b, "  %s = call double @llvm.sqrt.f64(double %s)\n", rt, fx)
		return rt, nil
	case "floor":
		// floor returns the largest double <= the float value of its argument.
		if fv, ok := g.floatEval(c.Args[0]); ok {
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(math.Floor(fv)))
			return t, nil
		}
		fx := g.floatValue(b, c.Args[0])
		rt := g.newTmp()
		fmt.Fprintf(b, "  %s = call double @llvm.floor.f64(double %s)\n", rt, fx)
		return rt, nil
	case "ceil":
		// ceil returns the smallest double >= the float value of its argument.
		if fv, ok := g.floatEval(c.Args[0]); ok {
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(math.Ceil(fv)))
			return t, nil
		}
		fx := g.floatValue(b, c.Args[0])
		rt := g.newTmp()
		fmt.Fprintf(b, "  %s = call double @llvm.ceil.f64(double %s)\n", rt, fx)
		return rt, nil
	case "range":
		for _, a := range c.Args {
			if _, ok := a.(*KeywordArg); ok {
				return "", fmt.Errorf("codegen: range does not accept keyword arguments")
			}
		}
		if len(c.Args) != 1 {
			return "", fmt.Errorf("codegen: range needs one argument")
		}
		return g.value(b, c.Args[0])
	case "str":
		// str(n) folds to the decimal string of an int literal.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("str expects one argument")
		}
		if g.isFloat(c.Args[0]) {
			if fv, ok := g.floatEval(c.Args[0]); ok {
				return g.strConst(pyFloatRepr(fv)), nil
			}
		}
		v, err := g.value(b, c.Args[0])
		if err != nil {
			return "", err
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return "", fmt.Errorf("str on non-integer")
		}
		return g.strConst(fmt.Sprintf("%d", n)), nil
	case "int":
		// int(x) folds to a constant on literal args: int(str) parses the
		// decimal string, int(int) is the identity. float() stays
		// interpreter-only: the AOT codegen has no float representation.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("int expects one argument")
		}
		if g.isFloat(c.Args[0]) {
			if fv, ok := g.floatEval(c.Args[0]); ok {
				return strconv.FormatInt(int64(fv), 10), nil
			}
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = fptosi double %s to i32\n", t, g.floatValue(b, c.Args[0]))
			return t, nil
		}

		if il, ok := c.Args[0].(*IntLit); ok {
			return fmt.Sprintf("%d", il.Value), nil
		}
		if sl, ok := c.Args[0].(*StrLit); ok {
			n, err := strconv.ParseInt(sl.Value, 10, 64)
			if err != nil {
				return "", fmt.Errorf("int on non-integer string")
			}
			return fmt.Sprintf("%d", n), nil
		}
		return "", fmt.Errorf("int: codegen folds only literal int/string args")
	case "reversed":
		if len(c.Args) != 1 {
			return "", fmt.Errorf("reversed expects one argument")
		}
		if lit, ok := c.Args[0].(*ListLit); ok {
			rev := &ListLit{Elems: reversedExprs(lit.Elems)}
			return g.emitList(rev)
		}
		if lit, ok := c.Args[0].(*StrLit); ok {
			return g.strConst(reverseStr(lit.Value)), nil
		}
		// imported string module global (data imports): reversed(mod.str)
		if attr, ok := c.Args[0].(*Attr); ok {
			if nm, ok2 := attr.Obj.(*Name); ok2 {
				if globals, ok3 := g.imports.Globals[nm.Value]; ok3 {
					if lit2, ok4 := globals[attr.Name.Value]; ok4 {
						if str, ok5 := lit2.(*StrLit); ok5 {
							return g.strConst(reverseStr(str.Value)), nil
						}
						if lst, ok5 := lit2.(*ListLit); ok5 {
							return g.emitList(&ListLit{Elems: reversedExprs(lst.Elems)})
						}
					}
				}
			}
		}
		return "", fmt.Errorf("reversed: codegen folds only literal list/string args")
	case "sorted":
		// sorted(iter[, reverse=True]) folds to a sorted inline list literal.
		// The AOT codegen represents lists as constant integer-element globals,
		// so sorted folds only over an inline list literal of integer literals,
		// mirroring the interpreter's ascending-by-value default and the
		// descending reverse=True keyword / truthy positional second arg.
		if len(c.Args) < 1 || len(c.Args) > 2 {
			return "", fmt.Errorf("sorted expects 1 or 2 arguments")
		}
		ln, ok := c.Args[0].(*ListLit)
		if !ok {
			// imported module list global (data imports): sorted(mod.list)
			if attr, ok2 := c.Args[0].(*Attr); ok2 {
				if nm, ok3 := attr.Obj.(*Name); ok3 {
					if globals, ok4 := g.imports.Globals[nm.Value]; ok4 {
						if lit, ok5 := globals[attr.Name.Value]; ok5 {
							if lst, ok6 := lit.(*ListLit); ok6 {
								ln, ok = lst, true
							}
						}
					}
				}
			}
		}
		if !ok {
			return "", fmt.Errorf("sorted: codegen folds only an inline list literal")
		}
		vals := make([]int64, len(ln.Elems))
		for i, el := range ln.Elems {
			il, ok := el.(*IntLit)
			if !ok {
				return "", fmt.Errorf("sorted: list elements must be integer literals")
			}
			vals[i] = il.Value
		}
		sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
		if len(c.Args) == 2 {
			revExpr := c.Args[1]
			if kw, ok := c.Args[1].(*KeywordArg); ok {
				revExpr = kw.Value
			}
			rv, rerr := g.constIntVal(revExpr)
			if rerr != nil {
				return "", rerr
			}
			if rv != 0 {
				for i, j := 0, len(vals)-1; i < j; i, j = i+1, j-1 {
					vals[i], vals[j] = vals[j], vals[i]
				}
			}
		}
		elems := make([]Expr, len(vals))
		for i, v := range vals {
			elems[i] = &IntLit{Value: v}
		}
		return g.emitList(&ListLit{Elems: elems})
	case "chr":
		// chr(n) folds a constant codepoint to a single-character string
		// global, mirroring the interpreter's string(rune(n)).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("chr expects one argument")
		}
		cn, cerr := g.constIntVal(c.Args[0])
		if cerr != nil {
			return "", fmt.Errorf("chr: codegen folds only a constant integer arg")
		}
		return g.strConst(string(rune(cn))), nil
	case "ord":
		// ord(s) folds a constant string to the codepoint of its first byte,
		// mirroring the interpreter (int64(o.sval[0])).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("ord expects one argument")
		}
		sv, ok := stringConst(c.Args[0])
		if !ok {
			// imported string module global (data imports): ord(mod.str)
			if attr, ok2 := c.Args[0].(*Attr); ok2 {
				if nm, ok3 := attr.Obj.(*Name); ok3 {
					if globals, ok4 := g.imports.Globals[nm.Value]; ok4 {
						if lit, ok5 := globals[attr.Name.Value]; ok5 {
							if str, ok6 := lit.(*StrLit); ok6 {
								if str.Value == "" {
									return "", fmt.Errorf("codegen: ord of empty string")
								}
								return fmt.Sprintf("%d", int(str.Value[0])), nil
							}
						}
					}
				}
			}
		}
		if !ok {
			return "", fmt.Errorf("ord: codegen folds only a constant string arg")
		}
		if len(sv) == 0 {
			return "", fmt.Errorf("ord of empty string")
		}
		return fmt.Sprintf("%d", int64(sv[0])), nil
	case "round":
		// round(x) folds a constant integer literal to itself (mirroring the
		// interpreter's int case; the AOT backend has no float representation).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("round expects one argument")
		}
		if g.isFloat(c.Args[0]) {
			if fv, ok := g.floatEval(c.Args[0]); ok {
				return fmt.Sprintf("%d", int64(math.Round(fv))), nil
			}
			fx := g.floatValue(b, c.Args[0])
			rt := g.newTmp()
			fmt.Fprintf(b, "  %s = call double @llvm.round.f64(double %s)\n", rt, fx)
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = fptosi double %s to i32\n", t, rt)
			return t, nil
		}

		rv, rerr := g.constIntVal(c.Args[0])
		if rerr != nil {
			if _, ok := c.Args[0].(*StrLit); ok {
				return "", fmt.Errorf("round: cannot round a string")
			}
			v, verr := g.value(b, c.Args[0])
			if verr != nil {
				return "", verr
			}
			return v, nil
		}
		return fmt.Sprintf("%d", rv), nil
	case "float":
		// float(x) converts x to a float. The AOT backend represents floats
		// as truncated ints (value() truncates FloatLit to int64), so
		// float(int) folds to itself and float(str) parses the string to a
		// float then truncates, mirroring the interpreter's allocFloat.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("float expects one argument")
		}
		if il, ok := c.Args[0].(*IntLit); ok {
			return fmt.Sprintf("%d", il.Value), nil
		}
		if sv, ok := stringConst(c.Args[0]); ok {
			f, err := strconv.ParseFloat(sv, 64)
			if err != nil {
				return "", fmt.Errorf("float: cannot parse %q", sv)
			}
			return fmt.Sprintf("%d", int64(f)), nil
		}
		// General arg: emit a real double value. floatValue already converts
		// ints to doubles (sitofp) and passes through float values, so
		// float(x) == floatValue(x) for numeric args.
		v := g.floatValue(b, c.Args[0])
		if v == "" {
			return "", fmt.Errorf("float: unsupported argument")
		}
		return v, nil
	default:
		return "", fmt.Errorf("codegen: unsupported call %q", fnName)
	}
}

// constIntVal resolves a literal expression to a compile-time integer value
// (IntLit, BoolLit, FloatLit truncation, NoneLit -> 0). Used by builtins that
// fold keyword/positional truthiness (e.g. sorted's reverse=True) at codegen.
func (g *irGen) constIntVal(e Expr) (int64, error) {
	switch n := e.(type) {
	case *IntLit:
		return n.Value, nil
	case *BoolLit:
		if n.Value {
			return 1, nil
		}
		return 0, nil
	case *FloatLit:
		return int64(n.Value), nil
	case *NoneLit:
		return 0, nil
	default:
		return 0, fmt.Errorf("codegen: reverse flag must be a literal")
	}
}

// emitLambda registers a lambda as a generated anonymous FuncDef and emits
// its closure IR, returning the generated function name so Call can invoke it.
func (g *irGen) emitLambda(b *strings.Builder, lam *Lambda) (string, error) {
	name := fmt.Sprintf("lambda_%d", g.lambdaCounter)
	g.lambdaCounter++
	fd := &FuncDef{
		Name:   name,
		Params: lam.Params,
		Body:   []Stmt{&ReturnStmt{Expr: lam.Body}},
	}
	// emit the lambda FuncDef at module level (globals), not the current
	// statement builder, so the `define` is not nested inside main.
	if err := g.funcDef(&g.globals, fd); err != nil {
		return "", err
	}
	if g.funcs == nil {
		g.funcs = map[string]bool{}
	}
	g.funcs[name] = true
	if g.fds == nil {
		g.fds = map[string]*FuncDef{}
	}
	g.fds[name] = fd
	return name, nil
}

func exnCode(name string) int {
	// The canonical list (and its codes) lives in exceptions.go, shared with the
	// interpreter's isExnClass and the checker's name table.
	return exnClassCode(name)
}

// raiseRuntimeIR reports an uncaught exception the way the interpreter does — a
// traceback header and the exception line — on stderr (fd 2, via write so it links on
// every platform without depending on a libc `stderr` symbol) before main returns 1.
const floatRuntimeIR = `; rt_fmt_double renders a runtime double the way Python's str() does, because the
; printf formats alone cannot: %g loses precision (0.123456789 -> 0.123457) and
; %.17g invents digits (0.1 -> 0.10000000000000001), and neither prints the
; trailing ".0" that marks an integral float (print(x * 2.0) showed 2). It tries
; the shortest precision that round-trips through strtod, then appends ".0" when
; the text has no decimal point or exponent (and is not inf/nan). The result
; lives in a small rotating buffer set so one print statement can format several
; values before handing them to printf.
@rt.fd.bufs = private global [8 x [64 x i8]] zeroinitializer
@rt.fd.next = private global i32 0
@rt.fd.fmt = private constant [5 x i8] c"%.*g\00"

declare double @strtod(i8*, i8**)

define internal i8* @rt_fmt_double(double %v) {
entry:
  %slot = load i32, i32* @rt.fd.next
  %ns = add i32 %slot, 1
  %nw = urem i32 %ns, 8
  store i32 %nw, i32* @rt.fd.next
  %buf = getelementptr [8 x [64 x i8]], [8 x [64 x i8]]* @rt.fd.bufs, i32 0, i32 %slot, i32 0
  br label %try
try:
  %prec = phi i32 [ 15, %entry ], [ %pn, %bad ]
  %fmtp = getelementptr inbounds [5 x i8], [5 x i8]* @rt.fd.fmt, i32 0, i32 0
  %w = call i32 (i8*, i32, i8*, ...) @snprintf(i8* %buf, i32 64, i8* %fmtp, i32 %prec, double %v)
  %back = call double @strtod(i8* %buf, i8* null)
  %same = fcmp oeq double %back, %v
  br i1 %same, label %fix, label %bad
bad:
  %pn = add i32 %prec, 1
  %more = icmp slt i32 %prec, 17
  br i1 %more, label %try, label %fix
fix:
  ; snprintf told us how many characters it wrote; that is where the ".0" goes.
  %len = phi i32 [ %w, %try ], [ %w, %bad ]
  %clamped = icmp ult i32 %len, 62
  %safe = select i1 %clamped, i32 %len, i32 62
  ; Decide the trailing ".0" from the value, not from the text: appending it is
  ; correct exactly when the number is a finite integral value small enough that
  ; %g rendered it positionally (Python: 2.0 -> "2.0", but 1e+16 -> "1e+16" and
  ; 3.5 -> "3.5" take no suffix).
  %fl = call double @llvm.floor.f64(double %v)
  %integral = fcmp oeq double %fl, %v
  %an = call double @llvm.fabs.f64(double %v)
  %finite = fcmp olt double %an, 1.000000e+308
  %positional = fcmp olt double %an, 1.000000e+15
  %c1 = and i1 %integral, %finite
  %needs = and i1 %c1, %positional
  br i1 %needs, label %addzero, label %done
addzero:
  %p0 = getelementptr i8, i8* %buf, i32 %safe
  store i8 46, i8* %p0
  %i1 = add i32 %safe, 1
  %p1 = getelementptr i8, i8* %buf, i32 %i1
  store i8 48, i8* %p1
  %i2 = add i32 %safe, 2
  %p2 = getelementptr i8, i8* %buf, i32 %i2
  store i8 0, i8* %p2
  br label %done
done:
  ret i8* %buf
}

`

const raiseRuntimeIR = `
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
  %fp = load i8*, i8** @exn_frame
  %hasframe = icmp ne i8* %fp, null
  br i1 %hasframe, label %withframe, label %withoutframe
withframe:
  %flen = call i64 @strlen(i8* %fp)
  call i64 @write(i32 2, i8* %fp, i64 %flen)
  call i64 @write(i32 2, i8* %nlpre, i64 1)
  br label %withoutframe
withoutframe:
  %isnull = icmp eq i8* %msg, null
  %none = getelementptr inbounds [26 x i8], [26 x i8]* @.rt_die.none, i32 0, i32 0
  %m = select i1 %isnull, i8* %none, i8* %msg
  %len = call i64 @strlen(i8* %m)
  call i64 @write(i32 2, i8* %m, i64 %len)
  call i64 @write(i32 2, i8* %nlpre, i64 1)
  ret void
}
`

// setExn records a raise: the flag, the exception code, and a "<Type>: <message>"
// string for the uncaught path. Every raise site goes through here so the report can
// never disagree with the code the handler matches on.
func (g *irGen) setExn(b *strings.Builder, code int, typeName, msg string, sp Span) {
	g.raiseUsed = true
	b.WriteString("  store i32 1, i32* @exn_flag\n")
	b.WriteString(fmt.Sprintf("  store i32 %d, i32* @exn_code\n", code))
	text := typeName
	if msg != "" {
		text = typeName + ": " + msg
	}
	if text == "" {
		b.WriteString("  store i8* null, i8** @exn_msg\n")
	} else {
		b.WriteString(fmt.Sprintf("  store i8* %s, i8** @exn_msg\n", g.strConst(text)))
	}
	// The interpreter prints one frame per stack level; the compiled report prints the
	// raise site's own frame, which is the line that answers "where". Full call stacks
	// need the debug line tables of L8.5 (roadmap Gap K.8).
	if frame := g.raiseFrame(sp); frame != "" {
		b.WriteString(fmt.Sprintf("  store i8* %s, i8** @exn_frame\n", g.strConst(frame)))
	} else {
		b.WriteString("  store i8* null, i8** @exn_frame\n")
	}
}

// printsAsInternedStr reports whether an expression's value is an index into the runtime
// string table: an element read from a string container, or a loop variable walking one.
// Those print as their own text, while the container itself prints as a list of
// single-quoted strings the way Python does (roadmap Gap I.2).
func (g *irGen) printsAsInternedStr(e Expr) bool {
	switch v := e.(type) {
	case *Name:
		return g.internedVars[v.Value]
	case *Call:
		// echo("yo") returns an index into @str_tab; printing it must show the text
		// (roadmap Gap J.5).
		if nm, ok := v.Fn.(*Name); ok {
			return g.strFuncs[nm.Value]
		}
	case *Index:
		if nm, ok := v.Obj.(*Name); ok {
			// Elements of a string list/set and the values of a string dict are both
			// @str_tab indices; printing one must show its text, not the index. The
			// dict case was missing, so `d["k"] = "v"; print(d["k"])` printed the
			// integer index whenever the whole dict had not been printed first (the
			// runtime @estr flag only covers printing the container itself).
			return g.listElemStr[nm.Value] || g.setElemStr[nm.Value] || g.dictValStr[nm.Value]
		}
	}
	return false
}

// isNoneExpr reports whether an expression evaluates to the None singleton. The AOT
// representation of a value is an untagged i32, so unlike the interpreter this cannot be
// decided at run time: None is recognised where the source says so — the literal, a variable
// whose latest assignment was None, and a call to a function whose body never returns a value
// (ADR 0172).
func (g *irGen) isNoneExpr(e Expr) bool {
	switch v := e.(type) {
	case *NoneLit:
		return true
	case *Name:
		return g.noneVars[v.Value]
	case *Call:
		if nm, ok := v.Fn.(*Name); ok {
			if fd, ok2 := g.fds[nm.Value]; ok2 && !fdReturnsValue(fd) {
				return true
			}
		}
	}
	return false
}

// fdReturnsValue reports whether a function can produce a value: any `return <expr>`
// anywhere in the body, or any `yield` (a generator evaluates to the list of yielded
// values). It is used to decide statically whether a call yields None, so the search is
// deliberately exhaustive — a hand-enumerated statement walk once mistook a `return`
// nested inside `match` for a procedure and printed None instead of the value (ADR 0172).
func fdReturnsValue(fd *FuncDef) bool {
	if fd == nil {
		return false
	}
	if containsYield(fd.Body) {
		return true
	}
	return hasValueReturn(reflect.ValueOf(fd.Body))
}

func hasValueReturn(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Invalid:
		return false
	case reflect.Interface, reflect.Ptr:
		if v.IsNil() {
			return false
		}
		if rs, ok := v.Interface().(*ReturnStmt); ok {
			return rs.Expr != nil
		}
		return hasValueReturn(v.Elem())
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			if hasValueReturn(v.Index(i)) {
				return true
			}
		}
		return false
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if hasValueReturn(v.Field(i)) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// calleeName renders a call's callee as the source text the user wrote, so diagnostics
// name `list(...)` rather than dumping the AST node (`&{list {2 7} fn() -> list[any]}`).
func calleeName(c *Call) string {
	if n, ok := c.Fn.(*Name); ok {
		return n.Value
	}
	return "<expression>"
}

// nameIsBound reports whether nm names something codegen has actually allocated or
// predeclared, so a reference to it can be lowered. Everything here is a name that can
// legally appear as a value without a visible alloca: parameters, captured env slots,
// container handles, folded constants, module globals, classes, and predeclared names.
func (g *irGen) nameIsBound(nm string) bool {
	if g.allocd[nm] || g.funcs[nm] {
		return true
	}
	if _, ok := g.params[nm]; ok {
		return true
	}
	if _, ok := g.funcBind[nm]; ok {
		return true
	}
	if _, ok := g.externs[nm]; ok {
		return true
	}
	if g.listVars[nm] || g.runtimeDicts[nm] || g.runtimeSets[nm] {
		return true
	}
	if g.strVals != nil {
		if _, ok := g.strVals[nm]; ok {
			return true
		}
	}
	if g.floatVars != nil && g.floatVars[nm] {
		return true
	}
	if g.unionVars[nm] || g.classIDs[nm] != 0 || g.classInfos[nm] != nil {
		return true
	}
	if _, ok := g.constBindings[nm]; ok {
		return true
	}
	if g.envCaptures != nil {
		if _, ok := g.envCaptures[nm]; ok {
			return true
		}
	}
	if g.curModGlobals != nil {
		if _, ok := g.curModGlobals[nm]; ok {
			return true
		}
	}
	if g.curModParams != nil {
		if _, ok := g.curModParams[nm]; ok {
			return true
		}
	}
	if g.imports != nil {
		// `import mod` binds `mod` as a value-ish name (mod.var / mod.fn()).
		for mod := range g.imports.Globals {
			if mod == nm {
				return true
			}
		}
		for mod := range g.imports.Funcs {
			if mod == nm {
				return true
			}
		}
	}
	if isExnClass(nm) || isPredeclaredName(nm) {
		return true
	}
	return false
}

// raiseFrame renders one traceback frame the way the interpreter does, e.g.
//
//	File "prog", line 12, in area
//
// for a raise at line 12 inside `area` (or <module> at top level). An empty span yields
// no frame rather than a fabricated line 0.
func (g *irGen) raiseFrame(sp Span) string {
	if sp.IsZero() {
		return ""
	}
	fn := "<module>"
	if g.curFnSrc != "" {
		fn = g.curFnSrc
	}
	return fmt.Sprintf("  File \"prog\", line %d, in %s", sp.Line, fn)
}

// raiseTo records a raise of an exception the *machine* detected (an out-of-range
// index, a missing key) and transfers to the innermost handler, or out of the function
// to the uncaught path. The interpreter raises the same typed errors, so `except
// IndexError:` works on both backends.
func (g *irGen) raiseTo(b *strings.Builder, code int, typeName, msg string, sp Span) {
	g.setExn(b, code, typeName, msg, sp)
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}
}

// checkIndexRead emits the bounds test for a heap-list read: `xs[i]` used to load
// whatever sat at that slot and print 0, where Python raises IndexError.
func (g *irGen) checkIndexRead(b *strings.Builder, h, idx string, sp Span) {
	ln := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_len(i32 %s)\n", ln, h))
	hi := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, %s\n", hi, idx, ln))
	g.markI1(hi)
	lo := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", lo, idx))
	g.markI1(lo)
	bad := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", bad, lo, hi))
	g.markI1(bad)
	badL, okL := g.newLabel("rd.bad"), g.newLabel("rd.ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	g.raiseTo(b, exnCode("IndexError"), "IndexError", "index out of range", sp)
	b.WriteString(fmt.Sprintf("%s:\n", okL))
}

// checkKeyRead emits the membership test for a heap-dict read: `d[k]` for a missing
// key used to return 0, where Python raises KeyError.
func (g *irGen) checkKeyRead(b *strings.Builder, h, key string, sp Span) {
	ok := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_dict_has(i32 %s, i32 %s)\n", ok, h, key))
	isZero := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", isZero, ok))
	g.markI1(isZero)
	badL, okL := g.newLabel("rd.bad"), g.newLabel("rd.ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isZero, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	g.raiseTo(b, exnCode("KeyError"), "KeyError", "key not found", sp)
	b.WriteString(fmt.Sprintf("%s:\n", okL))
}

// raiseStmt compiles `raise Exception("msg")` / `raise ValueError("msg")`.
func (g *irGen) raiseStmt(b *strings.Builder, rs *RaiseStmt) error {
	code, typeName, msg := 0, "", ""
	if c, ok := rs.Expr.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 {
			code = exnCode(n.Value)
			typeName = n.Value
			if len(c.Args) > 0 {
				if sl, ok3 := c.Args[0].(*StrLit); ok3 {
					msg = sl.Value // `raise ValueError("boom")` -> "ValueError: boom"
				}
			}
		}
	} else if n, ok := rs.Expr.(*Name); ok {
		code = exnCode(n.Value)
		typeName = n.Value
	}
	g.setExn(b, code, typeName, msg, rs.Span())
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}
	return nil
}

// checkExn emits a check of @exn_flag after a user-function call.
func (g *irGen) checkExn(b *strings.Builder) {
	f := g.newTmp()
	c := g.newTmp()
	cont := g.newLabel("exn.cont")
	b.WriteString("  " + f + " = load i32, i32* @exn_flag\n")
	b.WriteString("  " + c + " = icmp eq i32 " + f + ", 1\n")
	target := g.funcRaiseExit
	if len(g.handlerStack) > 0 {
		target = g.handlerStack[len(g.handlerStack)-1]
	}
	b.WriteString("  br i1 " + c + ", label %" + target + ", label %" + cont + "\n")
	b.WriteString(cont + ":\n")
}

// tryStmt compiles a try/except/finally statement.
func (g *irGen) tryStmt(b *strings.Builder, ts *TryStmt) error {
	handler := g.newLabel("try.handler")
	finally := g.newLabel("try.finally")
	after := g.newLabel("try.after")
	g.handlerStack = append(g.handlerStack, handler)
	for _, st := range ts.Body {
		if err := g.stmt(b, st); err != nil {
			return err
		}
	}
	g.handlerStack = g.handlerStack[:len(g.handlerStack)-1]
	f := g.newTmp()
	c := g.newTmp()
	b.WriteString("  " + f + " = load i32, i32* @exn_flag\n")
	b.WriteString("  " + c + " = icmp eq i32 " + f + ", 1\n")
	b.WriteString("  br i1 " + c + ", label %" + handler + ", label %" + finally + "\n")
	b.WriteString(handler + ":\n")
	b.WriteString("  store i32 0, i32* @exn_flag\n")
	if len(ts.Excepts) > 0 {
		ec := ts.Excepts[0]
		specific := false
		specCode := 0
		if ec.Exn != nil && ec.Exn.Value != "Exception" {
			specific = true
			specCode = exnCode(ec.Exn.Value)
		}
		cbody := g.newLabel("try.body")
		if specific {
			code := g.newTmp()
			m := g.newTmp()
			b.WriteString("  " + code + " = load i32, i32* @exn_code\n")
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", m, code, specCode))
			b.WriteString("  br i1 " + m + ", label %" + cbody + ", label %" + finally + "\n")
			b.WriteString(cbody + ":\n")
		}
		for _, st := range ec.Body {
			if err := g.stmt(b, st); err != nil {
				return err
			}
		}
		b.WriteString("  br label %" + finally + "\n")
	} else {
		b.WriteString("  br label %" + finally + "\n")
	}
	b.WriteString(finally + ":\n")
	for _, st := range ts.Finally {
		if err := g.stmt(b, st); err != nil {
			return err
		}
	}
	b.WriteString("  br label %" + after + "\n")
	b.WriteString(after + ":\n")
	return nil
}

// funcReturnsFloat reports whether fd has a return expression that produces a
// double. It scans the body for ReturnStmt expressions and asks isFloat, also
// tracking local variables that are assigned float values so that a function
// returning such a variable (e.g. `y = x * 1.5; return y`) is detected.
func funcReturnsFloat(g *irGen, fd *FuncDef) bool {
	var scan func([]Stmt) bool
	scan = func(sts []Stmt) bool {
		for _, st := range sts {
			switch n := st.(type) {
			case *ReturnStmt:
				if g.isFloat(n.Expr) {
					return true
				}
			case *IfStmt:
				if scan(n.Then) {
					return true
				}
				for _, e := range n.Elifs {
					if scan(e.Then) {
						return true
					}
				}
				if scan(n.Else) {
					return true
				}
			case *WhileStmt:
				if scan(n.Body) {
					return true
				}
			case *ForStmt:
				if scan(n.Body) {
					return true
				}
			case *MatchStmt:
				for _, c := range n.Cases {
					if scan(c.Body) {
						return true
					}
				}
			}
		}
		return false
	}
	return scan(fd.Body)
}

// fnName returns the emitted IR name for fd, honoring a module-function
// name override (g.curFnOverride) so AOT module functions get a mangled,
// collision-free label like `mod$fn`.
func (g *irGen) fnName(fd *FuncDef) string {
	if g.curFnOverride != "" {
		return g.curFnOverride
	}
	return fd.Name
}

// emitModuleFuncs lowers AOT module functions (from `import mod` where mod
// defines functions) as standalone IR defines with a mangled name `mod$fn`.
// Module-global constants are captured by bare-name resolution during funcDef.
func (g *irGen) emitModuleFuncs(b *strings.Builder) error {
	if g.imports == nil {
		return nil
	}
	for mod, fns := range g.imports.Funcs {
		globals := g.imports.Globals[mod]
		for fnName, fd := range fns {
			mangle := mod + "$" + fnName
			params := map[string]bool{}
			for _, p := range fd.Params {
				params[p.Name] = true
			}
			prevOverride, prevGlobals, prevParams, prevMod := g.curFnOverride, g.curModGlobals, g.curModParams, g.curModName
			g.curFnOverride = mangle
			g.curModName = mod
			g.curModGlobals = globals
			g.curModParams = params
			err := g.funcDef(b, fd)
			g.curFnOverride = prevOverride
			g.curModName = prevMod
			g.curModGlobals = prevGlobals
			g.curModParams = prevParams
			if err != nil {
				return fmt.Errorf("import %q: lowering module function %q: %v", mod, fnName, err)
			}
		}
	}
	return nil
}

func (g *irGen) funcDef(b *strings.Builder, fd *FuncDef) error {
	prevRaise := g.funcRaiseExit
	prevHandlers := g.handlerStack
	g.handlerStack = nil
	// The traceback frame names the function the raise is written in.
	prevFnSrc := g.curFnSrc
	g.curFnSrc = fd.Name
	defer func() { g.curFnSrc = prevFnSrc }()
	g.funcRaiseExit = g.fnName(fd) + ".raiseexit"
	g.closures = map[string]*closureInfo{}
	// A function body is its own variable-binding scope: slot/allocation state and
	// the container-kind maps are per scope. Without this, a dict named `d` in one
	// function made module-level `d = make(4)` emit its "release the old binding"
	// free against `%_d` — a register main had not allocated yet — and the module
	// failed to verify ("input module is broken").
	restoreScope := g.beginScope()
	defer restoreScope()
	g.envMode = false
	g.envCaptures = nil
	g.envParam = "%env"
	g.decorated = map[string]bool{}
	g.inFunc = true
	op := map[string]bool{}
	for _, p := range fd.Params {
		op[p.Name] = true
	}
	ol := map[string]bool{}
	collectLocals(fd.Body, ol)
	for _, nd := range nestedDefs(fd.Body) {
		ci := closureInfoFor(nd, op, ol)
		g.closures[ci.name] = ci
		fmt.Fprintf(&g.globals, "@%s_slot = internal global i32 0\n", ci.name)
		g.envSlots = append(g.envSlots, ci.name)
		g.emitClosureDef(b, ci, nd)
	}
	if len(fd.Decorators) > 0 {
		if err := g.emitDecoratedFunc(b, fd); err != nil {
			return err
		}
		return nil
	}
	g.params = map[string]string{}
	g.curFunc = g.fnName(fd)
	floatRet := funcReturnsFloat(g, fd)
	retTy := "i32"
	retVal := "0"
	paramTy := "i32"
	if floatRet {
		g.floatFuncs[g.fnName(fd)] = true
		retTy = "double"
		retVal = "0.0"
		paramTy = "double"
	}
	fmt.Fprintf(b, "define %s @%s(", retTy, g.fnName(fd))
	for i := range fd.Params {
		if i > 0 {
			fmt.Fprintf(b, ", ")
		}
		fmt.Fprintf(b, "%s %%p%d", paramTy, i)
	}
	fmt.Fprintf(b, ") {\n")
	// A call opens its own root frame (ADR 0181). Everything this body pushes as a
	// root is dropped when it returns, so a dead frame cannot retain a list — and a
	// recursive call gets its own entries instead of overwriting the outer frame's.
	g.gcOpenFrame(b)
	for i, p := range fd.Params {
		g.params[p.Name] = fmt.Sprintf("%%p%d", i)
		if floatRet {
			if g.floatVars == nil {
				g.floatVars = map[string]bool{}
			}
			g.floatVars[p.Name] = true
			fmt.Fprintf(b, "  %%_%s = alloca double\n", p.Name)
			fmt.Fprintf(b, "  store double %%p%d, double* %%_%s\n", i, p.Name)
		}
	}
	// A parameter that receives a list/dict/set handle must be treated as a
	// runtime container inside the body (see heapargs.go). The registration is
	// scoped to this body: undo it when the body is done.
	defer g.declareHeapParams(b, g.fnName(fd), fd, func(i int) string { return fmt.Sprintf("%%p%d", i) })()
	// A parameter whose arguments are strings arrives as an index into the runtime string
	// table, so inside the body it behaves exactly like an element read from a string
	// container: print shows the text, len measures it, == compares content (Gap J.5).
	for i, p := range fd.Params {
		if g.strParamOf[g.fnName(fd)][i] {
			g.internedVars[p.Name] = true
		}
	}
	isGen := containsYield(fd.Body)
	if isGen {
		g.genIdx++
		g.genHandle = fmt.Sprintf("%%gh%d", g.genIdx)
		g.genFuncs[g.fnName(fd)] = true
		g.heapUsed = true
		fmt.Fprintf(b, "  %s = call i32 @rt_alloc(i32 1)\n", g.genHandle)
		// Keep the generator's accumulator list alive across the GC emitted
		// before the first body statement; otherwise it is collected and every
		// yield appends into a stale/freed slot.
		ghslot := g.newTmp()
		ghslotName := strings.TrimPrefix(ghslot, "%")
		fmt.Fprintf(b, "  %%_%s = alloca i32\n", ghslotName)
		fmt.Fprintf(b, "  store i32 %s, i32* %%_%s\n", g.genHandle, ghslotName)
		g.gcReg(b, ghslotName)
	}
	for i := range fd.Params {
		if floatRet {
			continue
		}
		fmt.Fprintf(b, "  %%_param%d = alloca i32\n", i)
		fmt.Fprintf(b, "  store i32 %%p%d, i32* %%_param%d\n", i, i)
		g.gcReg(b, fmt.Sprintf("param%d", i))
	}
	if g.isWrappingDecorator(fd) {
		g.gcCloseFrame(b)
		b.WriteString("  ret i32 0\n}\n")
		g.frameOpen = false
		return nil
	}
	for _, st := range fd.Body {
		g.gcCall(b)
		if err := g.stmt(b, st); err != nil {
			return err
		}
	}
	if isGen {
		g.gcCloseFrame(b)
		fmt.Fprintf(b, "  ret i32 %s\n", g.genHandle)
	} else {
		g.gcCloseFrame(b)
		fmt.Fprintf(b, "  ret %s %s\n", retTy, retVal)
	}
	fmt.Fprintf(b, "%s:\n", g.funcRaiseExit)
	// An unwinding raise pops the frame too, so a raise that crosses a frame boundary
	// does not leave that frame's handles rooted forever.
	if isGen {
		g.gcCloseFrame(b)
		fmt.Fprintf(b, "  ret i32 %s\n", g.genHandle)
	} else {
		g.gcCloseFrame(b)
		fmt.Fprintf(b, "  ret %s %s\n", retTy, retVal)
	}
	fmt.Fprintf(b, "}\n")
	g.frameOpen = false
	g.funcRaiseExit = prevRaise
	g.genHandle = ""
	g.handlerStack = prevHandlers
	g.params = map[string]string{}
	g.curFunc = ""
	g.inFunc = false
	return nil
}

// loopVarName returns the name of a Name loop variable, or "" for a Tuple.
func loopVarName(v Expr) string {
	if n, ok := v.(*Name); ok {
		return n.Value
	}
	return ""
}

func (g *irGen) andCond(b *strings.Builder, a, c string) string {
	if a == "1" {
		return c
	}
	if c == "1" {
		return a
	}
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", t, a, c))
	return t
}

func (g *irGen) orCond(b *strings.Builder, a, c string) string {
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", t, a, c))
	return t
}

func (g *irGen) bindPat(b *strings.Builder, name, val string) {
	if !g.allocd[name] {
		g.allocd[name] = true
		b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", name))
	}
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", val, name))
}

func (g *irGen) hasBase(bases []string, class string) bool {
	for _, base := range bases {
		if base == class {
			return true
		}
		if g.classInfos[base] != nil && g.hasBase(g.classInfos[base].bases, class) {
			return true
		}
	}
	return false
}

func (g *irGen) classChainCond(b *strings.Builder, cid, class string) string {
	pid := g.classIDs[class]
	regs := []string{}
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", t, cid, pid))
	regs = append(regs, t)
	for cname, ci := range g.classInfos {
		if cname == class {
			continue
		}
		if g.hasBase(ci.bases, class) {
			sc := g.classIDs[cname]
			st := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", st, cid, sc))
			regs = append(regs, st)
		}
	}
	cond := regs[0]
	for _, r := range regs[1:] {
		cond = g.orCond(b, cond, r)
	}
	return cond
}

func (g *irGen) matchPattern(b *strings.Builder, sub string, pat Expr) string {
	switch p := pat.(type) {
	case *Name:
		if p.Value == "_" {
			cmp := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cmp, sub, sub))
			return cmp
		}
		// Bare-name capture pattern: bind the subject to a fresh variable
		// slot and always match (Python `case x:` semantics).
		g.bindPat(b, p.Value, sub)
		return "1"
	case *ListLit:
		n := len(p.Elems)
		ln := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_len(i32 %s)\n", ln, sub))
		lc := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", lc, ln, n))
		cond := lc
		for i, e := range p.Elems {
			er := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_get_elem(i32 %s, i32 %d)\n", er, sub, i))
			if nm, ok := e.(*Name); ok && nm.Value != "_" {
				g.bindPat(b, nm.Value, er)
				continue
			}
			ev, err := g.value(b, e)
			if err != nil {
				continue
			}
			ec := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", ec, er, ev))
			cond = g.andCond(b, cond, ec)
		}
		return cond
	case *DictLit:
		cond := "1"
		for i, k := range p.Keys {
			kv, err := g.value(b, k)
			if err != nil {
				continue
			}
			hs := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_dict_has(i32 %s, i32 %s)\n", hs, sub, kv))
			hc := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", hc, hs))
			cond = g.andCond(b, cond, hc)
			v := p.Vals[i]
			vr := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_dict_get(i32 %s, i32 %s)\n", vr, sub, kv))
			if nm, ok := v.(*Name); ok && nm.Value != "_" {
				g.bindPat(b, nm.Value, vr)
				continue
			}
			vv, err := g.value(b, v)
			if err != nil {
				continue
			}
			vc := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", vc, vr, vv))
			cond = g.andCond(b, cond, vc)
		}
		return cond
	case *Call:
		if fn, ok := p.Fn.(*Name); ok {
			class := fn.Value
			if g.classIDs[class] != 0 || g.classInfos[class] != nil {
				kind := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_heap_kind(i32 %s)\n", kind, sub))
				kc := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 4\n", kc, kind))
				cond := kc
				cid := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 0)\n", cid, sub))
				cc := g.classChainCond(b, cid, class)
				cond = g.andCond(b, cond, cc)
				for _, arg := range p.Args {
					if nm, ok := arg.(*Name); ok && nm.Value != "_" {
						slot := g.attrSlot(nm.Value)
						ar := g.newTmp()
						b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 %d)\n", ar, sub, slot))
						g.bindPat(b, nm.Value, ar)
					}
				}
				return cond
			} else if g.classIDs[class] == 0 && g.classInfos[class] == nil {
				// Runtime class-pattern alias (`Alias = Point`): fn is a variable
				// holding a class id (stored by value() as a classid constant).
				// Match the subject instance class id against the runtime alias id.
				kind := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_heap_kind(i32 %s)\n", kind, sub))
				cond := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 4\n", cond, kind))
				cid := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 0)\n", cid, sub))
				alias, _ := g.value(b, fn)
				cc := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cc, cid, alias))
				cond = g.andCond(b, cond, cc)
				for _, arg := range p.Args {
					if nm, ok := arg.(*Name); ok && nm.Value != "_" {
						slot := g.attrSlot(nm.Value)
						ar := g.newTmp()
						b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 %d)\n", ar, sub, slot))
						g.bindPat(b, nm.Value, ar)
					}
				}
				return cond
			}
		}
		pv, err := g.value(b, pat)
		if err != nil {
			return "0"
		}
		cmp := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cmp, sub, pv))
		return cmp
	default:
		pv, err := g.value(b, pat)
		if err != nil {
			return "0"
		}
		cmp := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cmp, sub, pv))
		return cmp
	}
}

func (g *irGen) stmt(b *strings.Builder, st Stmt) error {
	switch n := st.(type) {
	case *ImportStmt:
	case *ExternDecl:
		// extern declarations are handled at the module level

		// `import mod` resolves module globals at compile time (see resolveImports);
		// the statement itself emits no IR.
		return nil
	case *TryStmt:
		return g.tryStmt(b, n)
	case *RaiseStmt:
		return g.raiseStmt(b, n)
	case *ExprStmt:
		if _, err := g.value(b, n.Expr); err != nil {
			return err
		}
	case *AssignStmt:
		// tuple unpacking: a, b = v1, v2
		// Subscript assignment: `d[k] = v` inserts/updates a dict entry, `xs[i] = v`
		// replaces a list element within bounds (Python raises IndexError otherwise).
		// Sets and strings reject it, as they do in the interpreter.
		if ix, ok := n.Target.(*Index); ok {
			return g.assignIndex(b, ix, n.Value)
		}
		if tup, ok := n.Target.(*Tuple); ok {
			var valElems []Expr
			switch vt := n.Value.(type) {
			case *Tuple:
				valElems = vt.Elems
			case *ListLit:
				valElems = vt.Elems
			default:
				return fmt.Errorf("codegen: unsupported tuple assignment value %T", n.Value)
			}
			if len(tup.Elems) != len(valElems) {
				return fmt.Errorf("codegen: tuple assignment length mismatch")
			}
			// Tuple assignment must be simultaneous: `a, b = b, a` swaps,
			// it must not alias the updated targets. Emit all target allocas
			// first, then snapshot all RHS values, then store each target.
			for _, tgt := range tup.Elems {
				if nm, ok2 := tgt.(*Name); ok2 {
					if !g.allocd[nm.Value] {
						g.allocd[nm.Value] = true
						b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
						g.gcReg(b, nm.Value)
					}
				}
			}
			vals := make([]string, len(tup.Elems))
			for i := range tup.Elems {
				val, err := g.value(b, valElems[i])
				if err != nil {
					return err
				}
				vals[i] = val
			}
			for i, tgt := range tup.Elems {
				if nm, ok2 := tgt.(*Name); ok2 {
					b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", vals[i], nm.Value))
				}
			}
			return nil
		}
		if nm, ok := n.Target.(*Name); ok {
			// union-annotated scalar variable: tag its slot for runtime dispatch
			if g.unionVars == nil {
				g.unionVars = map[string]bool{}
			}
			if n.Annot != nil && n.Annot.Kind == KindUnion {
				g.unionVars[nm.Value] = true
			}
			// escape analysis: a list literal assigned to a variable that is
			// never read (dead) skips its heap allocation entirely.
			if _, isList := n.Value.(*ListLit); isList && !g.inFunc && g.deadLists[nm.Value] {
				return nil
			}
			// A binding whose *inferred type* is a container is a container, even
			// when the right-hand side is a call (`d = make(4)`). The kind maps are
			// what make iteration, indexing, len and truthiness ask the runtime for
			// a length; unregistered, `for k in d:` fell back to the range path and
			// compared the loop index against the handle — zero iterations.
			_, literal := n.Value.(*ListLit)
			_, literalDict := n.Value.(*DictLit)
			_, literalSet := n.Value.(*SetLit)
			boundKind := containerKindFromTy(exprTyName(n.Value))
			// Container literals have their own lowering paths below, which register
			// the kind themselves; only non-literal bindings (a call, an index, ...)
			// need the inferred type to say so here.
			rebindsContainer := boundKind != "" && !literal && !literalDict && !literalSet
			if rebindsContainer {
				switch boundKind {
				case "dict":
					g.runtimeDicts[nm.Value] = true
				case "list":
					g.listVars[nm.Value] = true
				case "set":
					g.runtimeSets[nm.Value] = true
				}
			}
			// Track the None singleton the same way containers are tracked: the variable's
			// *latest* assignment decides how print/truthiness/equality lower, and any other
			// assignment must clear the status (x = None; x = 0 must print 0) (ADR 0172).
			if g.isNoneExpr(n.Value) {
				g.noneVars[nm.Value] = true
			} else if !rebindsContainer {
				delete(g.noneVars, nm.Value)
			}
			// A module-level container variable needs its slot + GC root before
			// anything mutates it; the empty-literal init (`xs = []`) is where that
			// happens, because the literal path below only stores a fresh handle.
			if ll, isList := n.Value.(*ListLit); isList && !g.inFunc && len(ll.Elems) == 0 {
				// Module-level `xs = []`: give the variable its slot and GC root up
				// front, the way a non-empty literal does, so a later `xs.append(i)`
				// (which only stores through the slot) has somewhere to write and
				// rt_gc can see the live handle.
				_ = ll
				g.emitModuleContainerList(b, nm.Value, "%_"+nm.Value)
			}
			// `f = lambda ...` binds the generated lambda FuncDef to the variable.
			if lam, ok := n.Value.(*Lambda); ok {
				name, err := g.emitLambda(b, lam)
				if err != nil {
					return err
				}
				if g.lambdas == nil {
					g.lambdas = map[string]string{}
				}
				g.lambdas[nm.Value] = name
				return nil
			}
			if lit, ok := n.Value.(*ListLit); ok {
				g.heapUsed = true
				g.heapSeq++
				hs := g.heapSeq
				// heap slot reuse: rebinding a list var frees its old heap slot so rt_alloc can recycle it.
				if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
					g.emitFreeOld(b, nm.Value)
				}
				g.listVars[nm.Value] = true
				if !g.allocd[nm.Value] {
					b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
					g.gcReg(b, nm.Value)
					g.allocd[nm.Value] = true
				}
				b.WriteString(fmt.Sprintf("  %%h%d = call i32 @rt_alloc(i32 1)\n", hs))
				for i, el := range lit.Elems {
					// heapElemKind, not value(): an assigned container literal is still a
					// runtime container, so a string element becomes an index into @str_tab
					// instead of the global pointer that LLVM rejects in an i32 parameter
					// (ADR 0166, roadmap Gap I.2).
					ev, interned, err := g.heapElemKind(b, el)
					if err != nil {
						return err
					}
					if err := g.recordElemKind(nm.Value, "list", interned); err != nil {
						return err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_set_elem(i32 %%h%d, i32 %d, i32 %s)\n", hs, i, ev))
				}
				g.gcStoreHandle(b, fmt.Sprintf("%%h%d", hs), nm.Value)
				return nil
			}
			// list var rebound to a non-list value: free its heap slot (GC-correctness).
			// A binding that IS a container (e.g. `d = make(4)`) keeps its kind.
			if !rebindsContainer && (g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value]) {
				g.emitFreeOld(b, nm.Value)
				// The slot now holds a raw value: tag its root entry dead so the
				// collector never scans the int as a candidate heap index (ADR 0181).
				if g.allocd[nm.Value] {
					g.gcClearRoot(b, nm.Value)
				}
				g.listVars[nm.Value] = false
				g.runtimeDicts[nm.Value] = false
				g.runtimeSets[nm.Value] = false
				g.runtimeDicts[nm.Value] = false
				g.runtimeSets[nm.Value] = false
			}
			// runtime set: allocate a heap set object and add each element.
			if sl, ok := n.Value.(*SetLit); ok {
				if !g.allocd[nm.Value] {
					b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
					g.gcReg(b, nm.Value)
					g.allocd[nm.Value] = true
				}
				g.heapUsed = true
				if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
					g.emitFreeOld(b, nm.Value)
					g.listVars[nm.Value] = false
					g.runtimeDicts[nm.Value] = false
					g.runtimeSets[nm.Value] = false
				}
				g.runtimeSets[nm.Value] = true
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = call i32 @rt_alloc(i32 3)\n", hs))
				for _, el := range sl.Elems {
					ev, interned, err := g.heapElemKind(b, el)
					if err != nil {
						return err
					}
					if err := g.recordElemKind(nm.Value, "set", interned); err != nil {
						return err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_set_add(i32 %%h%d, i32 %s)\n", hs, ev))
				}
				g.gcStoreHandle(b, fmt.Sprintf("%%h%d", hs), nm.Value)
				return nil
			}
			// String RHS: fold at compile time into strVals (strings are read back
			// via strVals, never via the runtime slot). Emit a dummy i32 store so
			// the generated IR is valid (the pointer would be invalid as i32).
			var v string
			var err error
			if _, isStr := g.stringVal(n.Value); isStr {
				v = "0"
			} else {
				v, err = g.value(b, n.Value)
				if err != nil {
					return err
				}
			}
			// track concrete string constant values for `len(s)` and string ops
			if sv, ok := g.stringVal(n.Value); ok {
				if g.strVals == nil {
					g.strVals = map[string]string{}
				}
				g.strVals[nm.Value] = sv
			}
			// track dict literals assigned to variables for `d[key]`
			if dl, ok := n.Value.(*DictLit); ok {
				// runtime dict: allocate a heap dict object and fill pairs.
				if !g.allocd[nm.Value] {
					b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
					g.gcReg(b, nm.Value)
					g.allocd[nm.Value] = true
				}
				g.heapUsed = true
				if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
					g.emitFreeOld(b, nm.Value)
					g.listVars[nm.Value] = false
					g.runtimeDicts[nm.Value] = false
					g.runtimeSets[nm.Value] = false
				}
				g.runtimeDicts[nm.Value] = true
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = call i32 @rt_alloc(i32 2)\n", hs))
				for i := range dl.Keys {
					// Keys and values go through the container-word rule: a string becomes its
					// @str_tab index and the dict's key/value kinds record which side did, so
					// {"a": 1} and {1: "v"} both build and print like the interpreter does.
					kk, kIsStr, err := g.heapElemKind(b, dl.Keys[i])
					if err != nil {
						return err
					}
					vv, vIsStr, err := g.heapElemKind(b, dl.Vals[i])
					if err != nil {
						return err
					}
					if err := g.recordElemKind(nm.Value, "dict key", kIsStr); err != nil {
						return err
					}
					if err := g.recordElemKind(nm.Value, "dict value", vIsStr); err != nil {
						return err
					}
					bits := 0
					if kIsStr {
						bits |= 2
					}
					if vIsStr {
						bits |= 4
					}
					b.WriteString(fmt.Sprintf("  call void @rt_dict_put(i32 %%h%d, i32 %s, i32 %s)\n", hs, kk, vv))
					if bits != 0 {
						b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 %d)\n", hs, bits))
					}
				}
				g.gcStoreHandle(b, fmt.Sprintf("%%h%d", hs), nm.Value)
				return nil
			}
			if g.unionVars[nm.Value] {
				g.emitUnionStore(b, nm.Value, n.Value)
				return nil
			}
			isFloat := g.isFloat(n.Value)
			if !g.allocd[nm.Value] {
				slotTy := "i32"
				if isFloat {
					slotTy = "double"
				}
				b.WriteString(fmt.Sprintf("  %%_%s = alloca %s\n", nm.Value, slotTy))
				g.allocd[nm.Value] = true
			}
			if isFloat {
				fv := g.floatValue(b, n.Value)
				b.WriteString(fmt.Sprintf("  store double %s, double* %%_%s\n", fv, nm.Value))
				if g.floatVars == nil {
					g.floatVars = map[string]bool{}
				}
				g.floatVars[nm.Value] = true
			} else {
				// Root the variable's slot so a heap handle (instance/list/dict/set
				// handle) stored here survives a later GC collection (Gap A / ADR
				// 0151). Only i32 slots are rooted; double slots hold floats.
				if !g.floatVars[nm.Value] && !isScalarConst(n.Value) {
					g.gcReg(b, nm.Value)
				}
				// Track the class of a variable assigned from a class instantiation.
				if call, ok := n.Value.(*Call); ok {
					if fn, ok2 := call.Fn.(*Name); ok2 {
						if _, isClass := g.classInfos[fn.Value]; isClass {
							if g.varClasses == nil {
								g.varClasses = map[string]string{}
							}
							g.varClasses[nm.Value] = fn.Value
						}
					}
				}
				// a generator call returns a runtime heap list handle: track the
				// target so print/for/indexing treat it as a list, not a scalar.
				if call, ok := n.Value.(*Call); ok {
					if fn, ok2 := call.Fn.(*Name); ok2 {
						if g.genFuncs[fn.Value] {
							g.listVars[nm.Value] = true
						}
					}
				}
				// A constant-folded comprehension lowers to a compile-time list
				// global (@.lstN). That is not an i32: storing it directly emits IR
				// LLVM's verifier rejects ("global variable reference must have
				// pointer type"), and print/len/append would read an address instead
				// of the container. Copy the folded elements into a runtime heap list
				// and track the target as a list, exactly as the literal path above.
				if ln, folded := g.staticLists[v]; folded {
					if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
						g.emitFreeOld(b, nm.Value)
					}
					g.listVars[nm.Value] = true
					if !g.allocd[nm.Value] {
						b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
						g.gcReg(b, nm.Value)
						g.allocd[nm.Value] = true
					}
					h, herr := g.heapListFrom(b, ln, "")
					if herr != nil {
						return herr
					}
					g.gcStoreHandle(b, h, nm.Value)
					return nil
				}
				b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, nm.Value))
				if g.floatVars != nil {
					delete(g.floatVars, nm.Value)
				}
			}
		} else if attr, ok := n.Target.(*Attr); ok {
			// Instance attribute write: `self.x = v` / `inst.x = v`.
			if className := g.receiverClass(attr.Obj); className != "" {
				objHandle, err := g.value(b, attr.Obj)
				if err != nil {
					return err
				}
				v, err := g.value(b, n.Value)
				if err != nil {
					return err
				}
				g.heapUsed = true
				slot := g.attrSlot(attr.Name.Value)
				b.WriteString(fmt.Sprintf("  call void @rt_inst_put(i32 %s, i32 %d, i32 %s)\n", objHandle, slot, v))
				return nil
			}
			return fmt.Errorf("codegen: unsupported assignment target %T", n.Target)
		} else {
			return fmt.Errorf("codegen: unsupported assignment target %T", n.Target)
		}
	case *AugAssignStmt:
		// augmented assignment: read target, apply op with rhs, store back.
		binop := &BinOp{Op: n.Op, L: n.Target, R: n.Value}
		if nm, ok := n.Target.(*Name); ok {
			v, err := g.value(b, binop)
			if err != nil {
				return err
			}
			if g.isFloat(n.Target) || g.isFloat(n.Value) {
				b.WriteString(fmt.Sprintf("  %%_%s = alloca double\n", nm.Value))
				b.WriteString(fmt.Sprintf("  store double %s, double* %%_%s\n", v, nm.Value))
				g.floatVars[nm.Value] = true
			} else {
				b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, nm.Value))
			}
			return nil
		}
		if attr, ok := n.Target.(*Attr); ok {
			objHandle, err := g.value(b, attr.Obj)
			if err != nil {
				return err
			}
			v, err := g.value(b, binop)
			if err != nil {
				return err
			}
			// attrs are int-only in codegen; truncate a float result to i32.
			if g.isFloat(n.Value) {
				b.WriteString(fmt.Sprintf("  %%_aug = fptosi double %s to i32\n", v))
				v = "%_aug"
			}
			b.WriteString(fmt.Sprintf("  call void @rt_inst_put(i32 %s, i32 %d, i32 %s)\n", objHandle, g.attrSlot(attr.Name.Value), v))
			return nil
		}
		return fmt.Errorf("codegen: unsupported augmented-assignment target %T", n.Target)

	case *IfStmt:
		cond := g.truthyValue(b, n.Cond)
		thenL := g.newLabel("if.then")
		endL := g.newLabel("if.end")
		var elseL string
		// build elif chain: each elif gets a cond label and a then label.
		type el struct {
			condL, thenL string
			e            *IfStmt
		}
		els := []el{}
		for _, e := range n.Elifs {
			els = append(els, el{g.newLabel("if.elif"), g.newLabel("if.elif.then"), e})
		}
		elseL = g.newLabel("if.else")
		// dispatch from top: cond -> then, else -> first elif cond (or else).
		firstTarget := elseL
		if len(els) > 0 {
			firstTarget = els[0].condL
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cond, thenL, firstTarget))
		b.WriteString(fmt.Sprintf("%s:\n", thenL))
		for _, s := range n.Then {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		// elif branches
		for i, e := range els {
			b.WriteString(fmt.Sprintf("%s:\n", e.condL))
			ec, err := g.truthOperandErr(b, e.e.Cond)
			if err != nil {
				return err
			}
			nextTarget := elseL
			if i+1 < len(els) {
				nextTarget = els[i+1].condL
			}
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", ec, e.thenL, nextTarget))
			b.WriteString(fmt.Sprintf("%s:\n", e.thenL))
			for _, s := range e.e.Then {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		}
		b.WriteString(fmt.Sprintf("%s:\n", elseL))
		for _, s := range n.Else {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *MatchStmt:
		sub, err := g.value(b, n.Subject)
		if err != nil {
			return err
		}
		endL := g.newLabel("match.end")
		for i, c := range n.Cases {
			bodyL := g.newLabel("match.case")
			fallL := endL
			if i < len(n.Cases)-1 {
				fallL = g.newLabel("match.next")
			}
			patterns := append([]Expr{c.Pattern}, c.Or...)
			var orTmp string
			for _, p := range patterns {
				pc := g.matchPattern(b, sub, p)
				if orTmp == "" {
					orTmp = pc
				} else {
					nt := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", nt, orTmp, pc))
					orTmp = nt
				}
			}
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", orTmp, bodyL, fallL))
			b.WriteString(fmt.Sprintf("%s:\n", bodyL))
			if c.Guard != nil {
				gok := g.truthyValue(b, c.Guard)
				bodyAfter := g.newLabel("match.case.body")
				b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", gok, bodyAfter, fallL))
				b.WriteString(fmt.Sprintf("%s:\n", bodyAfter))
			}
			for _, s := range c.Body {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
			if i < len(n.Cases)-1 {
				b.WriteString(fmt.Sprintf("%s:\n", fallL))
			}
		}
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *WhileStmt:
		condL := g.newLabel("while.cond")
		bodyL := g.newLabel("while.body")
		elseL := g.newLabel("while.else")
		endL := g.newLabel("while.end")
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		b.WriteString(fmt.Sprintf("%s:\n", condL))
		cond := g.truthyValue(b, n.Cond)
		// normal completion (cond false) enters else if present; break skips else
		normalL := endL
		if len(n.Else) > 0 {
			normalL = elseL
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cond, bodyL, normalL))
		b.WriteString(fmt.Sprintf("%s:\n", bodyL))
		g.loopStack = append(g.loopStack, loopInfo{breakLabel: endL, continueLabel: condL})
		for _, s := range n.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		if len(n.Else) > 0 {
			b.WriteString(fmt.Sprintf("%s:\n", elseL))
			for _, s := range n.Else {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		}
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *ForStmt:
		// `for x in [1, 2, 3]`: iterate an inline list literal's constant
		// elements by unrolling one body block per element. `break` skips the
		// `else`, `continue` advances to the next element; after the last
		// element normal completion enters `else` if present (like Python).
		// `for x in "str"`: unroll each rune as a single-char string literal.
		if sl, ok := n.Iter.(*StrLit); ok {
			var elems []Expr
			for _, r := range sl.Value {
				elems = append(elems, &StrLit{Value: string(r)})
			}
			n.Iter = &ListLit{Elems: elems}
		}
		if ll, ok := n.Iter.(*ListLit); ok {
			endL := g.newLabel("for.end")
			elseL := g.newLabel("for.else")
			normalL := endL
			if len(n.Else) > 0 {
				normalL = elseL
			}
			if !g.allocd[loopVarName(n.Var)] {
				b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", loopVarName(n.Var)))
				g.gcReg(b, loopVarName(n.Var))
				g.allocd[loopVarName(n.Var)] = true
			}
			var contL string
			for _, el := range ll.Elems {
				v, err := g.value(b, el)
				if err != nil {
					return err
				}
				bodyL := g.newLabel("for.list.body")
				contL = g.newLabel("for.list.cont")
				b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, loopVarName(n.Var)))
				b.WriteString(fmt.Sprintf("  br label %%%s\n", bodyL))
				b.WriteString(fmt.Sprintf("%s:\n", bodyL))
				g.loopStack = append(g.loopStack, loopInfo{breakLabel: endL, continueLabel: contL})
				for _, s := range n.Body {
					if err := g.stmt(b, s); err != nil {
						return err
					}
				}
				g.loopStack = g.loopStack[:len(g.loopStack)-1]
				b.WriteString(fmt.Sprintf("  br label %%%s\n", contL))
				b.WriteString(fmt.Sprintf("%s:\n", contL))
			}
			// last cont block (continue on the final element) reaches normal
			// completion, entering `else` when present.
			b.WriteString(fmt.Sprintf("  br label %%%s\n", normalL))
			if len(n.Else) > 0 {
				b.WriteString(fmt.Sprintf("%s:\n", elseL))
				for _, s := range n.Else {
					if err := g.stmt(b, s); err != nil {
						return err
					}
				}
				b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
			}
			b.WriteString(fmt.Sprintf("%s:\n", endL))
			return nil
		}
		// runtime heap list iterable: a generator-call result or a tracked
		// list variable. Iterate positions 0..len-1 and bind the loop
		// variable to each element via rt_get_elem.
		loopVar := loopVarName(n.Var)
		// Runtime heap container iterables. A list (or generator result) yields its
		// elements and a set its members; a dict yields its KEYS in insertion order,
		// which the runtime stores as [key, value] pairs — so entry i's key lives at
		// 2*i. Iterating anything else falls through to the range path.
		iterKind := "" // "list" | "set" | "dict"
		var hVal string
		if name, ok := n.Iter.(*Name); ok {
			switch {
			case g.listVars[name.Value]:
				iterKind = "list"
			case g.runtimeSets[name.Value]:
				iterKind = "set"
			case g.runtimeDicts[name.Value]:
				iterKind = "dict"
			}
		} else if call, ok := n.Iter.(*Call); ok {
			if fn, ok2 := call.Fn.(*Name); ok2 {
				if g.genFuncs[fn.Value] {
					iterKind = "list"
				}
			}
		}
		if iterKind != "" {
			// A loop over a container of interned strings binds the loop variable to an
			// index into @str_tab; recording that keeps print(x) rendering the text instead
			// of the index (roadmap Gap I.2). Dict iteration yields keys, so it is the key
			// kind that matters.
			if name, ok := n.Iter.(*Name); ok {
				switch iterKind {
				case "list", "set":
					if g.listElemStr[name.Value] || g.setElemStr[name.Value] {
						g.internedVars[loopVar] = true
					}
				case "dict":
					if g.dictKeyStr[name.Value] {
						g.internedVars[loopVar] = true
					}
				}
			}
			lenFn := "rt_list_len"
			elemStride := 1
			switch iterKind {
			case "set":
				lenFn = "rt_set_len"
			case "dict":
				lenFn = "rt_dict_len"
				elemStride = 2 // keys only
			}
			if hVal == "" {
				hv, err := g.value(b, n.Iter)
				if err != nil {
					return err
				}
				hVal = hv
			}
			lenT := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s)\n", lenT, lenFn, hVal))
			idxVar := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = alloca i32\n", idxVar))
			b.WriteString(fmt.Sprintf("  store i32 0, i32* %s\n", idxVar))
			// The loop variable's slot must be allocated here, in the block that
			// branches into the loop: an alloca emitted inside the body block does
			// not dominate the blocks after the loop, so a second loop reusing the
			// same variable name (`for k in d:` … `for k in m:`) failed the verifier
			// with "Instruction does not dominate all uses".
			if !g.allocd[loopVar] {
				b.WriteString(fmt.Sprintf("  %%%s = alloca i32\n", "_"+loopVar))
			}
			g.allocd[loopVar] = true
			condL := g.newLabel("for.list.cond")
			bodyL := g.newLabel("for.list.body")
			incL := g.newLabel("for.list.inc")
			elseL := g.newLabel("for.list.else")
			endL := g.newLabel("for.list.end")
			b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
			b.WriteString(fmt.Sprintf("%s:\n", condL))
			ild := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", ild, idxVar))
			cmp := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, %s\n", cmp, ild, lenT))
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cmp, bodyL, elseL))
			b.WriteString(fmt.Sprintf("%s:\n", bodyL))
			pos := ild
			if elemStride != 1 {
				// dict entries occupy two words: walk the key slots.
				scaled := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = mul i32 %s, %d\n", scaled, ild, elemStride))
				pos = scaled
			}
			elemT := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", elemT, hVal, pos))
			b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", elemT, loopVar))
			g.loopStack = append(g.loopStack, loopInfo{breakLabel: endL, continueLabel: incL})
			for _, s := range n.Body {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			g.loopStack = g.loopStack[:len(g.loopStack)-1]
			b.WriteString(fmt.Sprintf("  br label %%%s\n", incL))
			b.WriteString(fmt.Sprintf("%s:\n", incL))
			ild2 := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", ild2, idxVar))
			i2 := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = add i32 %s, 1\n", i2, ild2))
			b.WriteString(fmt.Sprintf("  store i32 %s, i32* %s\n", i2, idxVar))
			b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
			b.WriteString(fmt.Sprintf("%s:\n", elseL))
			if n.Else != nil {
				for _, s := range n.Else {
					if err := g.stmt(b, s); err != nil {
						return err
					}
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
			b.WriteString(fmt.Sprintf("%s:\n", endL))
			return nil
		}
		initL := g.newLabel("for.init")
		condL := g.newLabel("for.cond")
		bodyL := g.newLabel("for.body")
		incL := g.newLabel("for.inc")
		elseL := g.newLabel("for.else")
		endL := g.newLabel("for.end")
		b.WriteString(fmt.Sprintf("  br label %%%s\n", initL))
		b.WriteString(fmt.Sprintf("%s:\n", initL))
		start, stop, step, err := g.rangeBounds(b, n.Iter)
		if err != nil {
			return err
		}
		if !g.allocd[loopVarName(n.Var)] {
			b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", loopVarName(n.Var)))
			g.gcReg(b, loopVarName(n.Var))
			g.allocd[loopVarName(n.Var)] = true
		}
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", start, loopVarName(n.Var)))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		b.WriteString(fmt.Sprintf("%s:\n", condL))
		g.ldN++
		cld := fmt.Sprintf("%%_%s.ld%d", loopVarName(n.Var), g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", cld, loopVarName(n.Var)))
		t := g.newTmp()
		cmpOp := "slt"
		if strings.HasPrefix(step, "-") {
			cmpOp = "sgt"
		}
		b.WriteString(fmt.Sprintf("  %s = icmp %s i32 %s, %s\n", t, cmpOp, cld, stop))
		// normal completion (i >= stop) enters else if present; break skips else
		normalL := endL
		if len(n.Else) > 0 {
			normalL = elseL
		}
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", t, bodyL, normalL))
		b.WriteString(fmt.Sprintf("%s:\n", bodyL))
		g.loopStack = append(g.loopStack, loopInfo{breakLabel: endL, continueLabel: incL})
		for _, s := range n.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		g.loopStack = g.loopStack[:len(g.loopStack)-1]
		b.WriteString(fmt.Sprintf("  br label %%%s\n", incL))
		b.WriteString(fmt.Sprintf("%s:\n", incL))
		g.ldN++
		ild := fmt.Sprintf("%%_%s.ld%d", loopVarName(n.Var), g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", ild, loopVarName(n.Var)))
		itmp := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", itmp, ild, step))
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", itmp, loopVarName(n.Var)))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		if len(n.Else) > 0 {
			b.WriteString(fmt.Sprintf("%s:\n", elseL))
			for _, s := range n.Else {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		}
		b.WriteString(fmt.Sprintf("%s:\n", endL))
	case *ClassDef:
		g.registerClass(n)
		return nil
	case *FuncDef:
		ci := g.closures[n.Name]
		if ci == nil {
			if err := g.funcDef(b, n); err != nil {
				return err
			}
			return nil
		}
		env := g.emitNewEnv(b)
		for i, c := range ci.captured {
			val := ""
			if pn, ok := g.params[c]; ok {
				val = pn
			} else {
				val = g.newTmp()
				fmt.Fprintf(b, "  %s = load i32, i32* %%_%s\n", val, c)
			}
			g.emitEnvStore(b, env, i, val)
		}
		fmt.Fprintf(b, "  store i32 %s, i32* @%s_slot\n", env, n.Name)
	case *ReturnStmt:
		if n.Expr == nil {
			// bare `return` yields None (ADR 0172). A float function reaching this is
			// a type error the checker reports; 0.0 keeps the module valid.
			if g.floatFuncs[g.curFunc] {
				g.gcCloseFrame(b)
				b.WriteString("  ret double 0.000000\n")
				return nil
			}
			g.heapUsed = true
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_none()\n", t))
			g.gcCloseFrame(b)
			b.WriteString(fmt.Sprintf("  ret i32 %s\n", t))
			return nil
		}
		if g.floatFuncs[g.curFunc] {
			fv := g.floatValue(b, n.Expr)
			g.gcCloseFrame(b)
			b.WriteString(fmt.Sprintf("  ret double %s\n", fv))
			return nil
		}
		if g.strFuncs[g.curFunc] {
			// A function known to yield a string returns its @str_tab index: callers
			// read the result as an index (that is what makes print(f("x")) show text,
			// ADR 0174), so returning the raw @.strN global here put a global in an
			// i32 slot and llc rejected the module — a plain `def g(): return "hi"`
			// looked like a compiler bug (ADR 0166).
			if txt, ok := g.stringVal(n.Expr); ok {
				it := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", it, g.strConst(txt), g.strConst(pyReprString(txt))))
				g.gcCloseFrame(b)
				b.WriteString(fmt.Sprintf("  ret i32 %s\n", it))
				return nil
			}
		}
		v, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		g.gcCloseFrame(b)
		b.WriteString(fmt.Sprintf("  ret i32 %s\n", v))
	case *BreakStmt:
		if len(g.loopStack) == 0 {
			return fmt.Errorf("codegen: break outside loop")
		}
		info := g.loopStack[len(g.loopStack)-1]
		b.WriteString(fmt.Sprintf("  br label %%%s\n", info.breakLabel))
	case *ContinueStmt:
		if len(g.loopStack) == 0 {
			return fmt.Errorf("codegen: continue outside loop")
		}
		info := g.loopStack[len(g.loopStack)-1]
		b.WriteString(fmt.Sprintf("  br label %%%s\n", info.continueLabel))
	case *PassStmt, *TypeAliasStmt:
		// no-op statement: type aliases are compile-time only (L5.7); emit nothing
	case *YieldStmt:
		// `yield expr` inside a generator function appends to the function's
		// runtime heap list (the interpreter evaluates generators eagerly).
		if g.genHandle == "" {
			return fmt.Errorf("codegen: yield outside a generator function")
		}
		v, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		fmt.Fprintf(b, "  call void @rt_append(i32 %s, i32 %s)\n", g.genHandle, v)
	case *YieldFromStmt:
		if g.genHandle == "" {
			return fmt.Errorf("codegen: yield from outside a generator")
		}
		// A statically-known list literal has no heap backing: append each
		// element directly instead of treating the global as a heap handle.
		if ll, ok := n.Expr.(*ListLit); ok {
			for _, el := range ll.Elems {
				ev, err := g.value(b, el)
				if err != nil {
					return err
				}
				fmt.Fprintf(b, "  call void @rt_append(i32 %s, i32 %s)\n", g.genHandle, ev)
			}
			return nil
		}
		hv, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		// Keep the sub-list handle alive across the GC emitted before the
		// append loop; otherwise the generator's freshly-returned list can be
		// collected and the loop reads a stale handle. Store the handle into a
		// rooted alloca slot so GC keeps the object reachable.
		hvslot := g.newTmp()
		hvslotName := strings.TrimPrefix(hvslot, "%")
		fmt.Fprintf(b, "  %%_%s = alloca i32\n", hvslotName)
		fmt.Fprintf(b, "  store i32 %s, i32* %%_%s\n", hv, hvslotName)
		g.gcReg(b, hvslotName)
		ln := g.newTmp()
		fmt.Fprintf(b, "%s = call i32 @rt_list_len(i32 %s)\n", ln, hv)
		ild := g.newLabel("yf.cond")
		ilp := g.newLabel("yf.body")
		inc := g.newLabel("yf.inc")
		end := g.newLabel("yf.end")
		iv := g.newTmp()
		fmt.Fprintf(b, "%s = alloca i32\n", iv)
		fmt.Fprintf(b, "  store i32 0, i32* %s\n", iv)
		fmt.Fprintf(b, "  br label %%%s\n", ild)
		fmt.Fprintf(b, "%s:\n", ild)
		ivc := g.newTmp()
		fmt.Fprintf(b, "%s = load i32, i32* %s\n", ivc, iv)
		cmp := g.newTmp()
		fmt.Fprintf(b, "%s = icmp slt i32 %s, %s\n", cmp, ivc, ln)
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", cmp, ilp, end)
		fmt.Fprintf(b, "%s:\n", ilp)
		el := g.newTmp()
		fmt.Fprintf(b, "%s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", el, hv, ivc)
		fmt.Fprintf(b, "  call void @rt_append(i32 %s, i32 %s)\n", g.genHandle, el)
		fmt.Fprintf(b, "  br label %%%s\n", inc)
		fmt.Fprintf(b, "%s:\n", inc)
		nv := g.newTmp()
		fmt.Fprintf(b, "%s = add i32 %s, 1\n", nv, ivc)
		fmt.Fprintf(b, "  store i32 %s, i32* %s\n", nv, iv)
		fmt.Fprintf(b, "  br label %%%s\n", ild)
		fmt.Fprintf(b, "%s:\n", end)
	case *WithStmt:
		mh, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		_ = mh // manager handle kept for __exit__ dispatch
		enterCall := &Call{Fn: &Attr{Obj: n.Expr, Name: &Name{Value: "__enter__"}}}
		eh, err := g.value(b, enterCall)
		if err != nil {
			return err
		}
		if n.As != nil {
			fmt.Fprintf(b, "%%_%s = alloca i32\n", n.As.Value)
			fmt.Fprintf(b, "  store i32 %s, i32* %%_%s\n", eh, n.As.Value)
			// Record the slot like every other binding does: the unbound-name guard
			// (nameIsBound) consults allocd, and `with M() as m: … m.n` was rejected as
			// "undefined name m" because this path allocated without registering.
			g.allocd[n.As.Value] = true
			// Record the `as` binding's class so instance field access (m.n) works.
			// `__enter__` returns self, so the binding is an instance of the manager's class.
			cls := ""
			if call, ok := n.Expr.(*Call); ok {
				if fn, ok2 := call.Fn.(*Name); ok2 {
					if _, isClass := g.classInfos[fn.Value]; isClass {
						cls = fn.Value
					}
				}
			} else if name, ok := n.Expr.(*Name); ok {
				cls = g.varClasses[name.Value]
			}
			if cls != "" {
				if g.varClasses == nil {
					g.varClasses = map[string]string{}
				}
				g.varClasses[n.As.Value] = cls
			}
		}
		for _, s := range n.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		exitCall := &Call{Fn: &Attr{Obj: n.Expr, Name: &Name{Value: "__exit__"}},
			Args: []Expr{&IntLit{Value: 0}, &IntLit{Value: 0}, &IntLit{Value: 0}}}
		_, err = g.value(b, exitCall)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("codegen: unsupported statement %T", st)
	}
	return nil
}
