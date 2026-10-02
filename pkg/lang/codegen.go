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
  ; @estr[h] is the container-wide "my elements are interned text" flag, and the printers
  ; dispatch on it. Like a slot's tag it is an attribute of the object, so its lifetime has to
  ; match the object's: a recycled slot that kept its predecessor's flag made print(["a"])
  ; followed by print({1, 2}) render the numbers through the string table as {(null), (null)}.
  ; Clearing the tag array would be a 256-entry memset and is covered by writing tags in pairs
  ; with payloads (ADR 0187); this flag is one store, so clear it (ADR 0188).
  %eslot = getelementptr [1024 x i32], [1024 x i32]* @estr, i32 0, i32 %c
  store i32 0, i32* %eslot
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
  %eslot2 = getelementptr [1024 x i32], [1024 x i32]* @estr, i32 0, i32 %fh
  store i32 0, i32* %eslot2
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
@str_tab = internal global [4096 x i8*] zeroinitializer
; Parallel table holding each interned string's Python repr form (quoted, with the quote
; character chosen the way Python chooses it). Containers store the index; printing inside a
; container uses the repr slot, printing a single value uses the raw text.
@str_repr_tab = internal global [4096 x i8*] zeroinitializer
; Per-object element-kind flags, indexed by heap handle: bit 0 = elements are interned
; strings, bit 1 = dict keys are, bit 2 = dict values are. Whether a container holds strings
; is a property of the *object*, not of the variable — a helper can fill a list its caller
; created — so the printers read this instead of trusting a static guess (Gap I.2/J.5).
@estr = internal global [1024 x i32] zeroinitializer
; A float box is a heap object of kind HeapKindFloat whose bits live here, indexed by handle.
; It is parallel to @heap rather than inside it for one reason: an element slot is one i32 word
; and a double does not fit in one, so a float that goes into a slot goes in as the handle of a
; box (roadmap L11.1, ADR 0233). The collector marks and sweeps by index rather than by kind, so a
; box is live exactly as long as the container that holds it and is recycled with it.
@float_box = internal global [1024 x double] zeroinitializer
; @heap_tags is the per-element half of the value model (roadmap L11.1). @estr[h] says one
; thing about a whole container -- "its elements are interned strings" -- which is why a
; heterogeneous xs = [1, "a"] had to be refused rather than printed (ADR 0175). This array
; numbers each slot with a canonical ValueTag (int=0, None=3, str=4, from the table in
; value.go, so no new vocabulary), letting one list hold numbers and interned strings
; together. Only scalars and interned strings are taggable: a nested container would have to
; be marked by the collector, and that case stays refused.
@heap_tags = internal global [1024 x [256 x i32]] zeroinitializer
; @inst_set is the instance half of the same idea (roadmap Gap B, ADR 0235): @inst_set[h][slot] is 1
; once something has been WRITTEN to that attribute slot of instance h. A class pattern binds capture
; names to attributes and a missing attribute is documented to FAIL the pattern, but @heap's data
; words cannot answer that -- an unwritten slot and a written zero look identical, so
; "case Point(a, b):" matched an instance that had no "a" and bound 0. rt_inst_clear zeroes the row
; when a heap slot becomes a new instance, because heap slots are reused and a stale 1 would be the
; previous tenant's answer.
@inst_set = internal global [1024 x [256 x i32]] zeroinitializer
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
  %slot = getelementptr [4096 x i8*], [4096 x i8*]* @str_tab, i32 0, i32 %i
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
  %slot2 = getelementptr [4096 x i8*], [4096 x i8*]* @str_tab, i32 0, i32 %n0
  store i8* %p, i8** %slot2
  %n1 = add i32 %n0, 1
  store i32 %n1, i32* @str_count
  ret i32 %n0
full:
  ; Out of table space. Reusing the last entry — the old behaviour — made a runtime-built
  ; string *print as a different string*, which is a wrong answer with no diagnostic at all.
  ; Distinct runtime strings are now bounded by @str_tab's capacity (4096, ADR 0229) and
  ; exceeding it is a trap with a message, the same honest shape as any other limit.
  ; -2 is the sentinel for "the table is full". Nothing is printed here: a built-in trap is a
  ; *typed raise*, and the raise belongs to the code that knows the source (ADR 0212, ADR 0214),
  ; which checks for this value and raises RuntimeError the program can catch. Printing from the
  ; runtime would be a sentence the program cannot intercept, and the old behaviour — silently
  ; reusing the last entry — was a wrong answer with no diagnostic at all (ADR 0229).
  ret i32 -2
}

; rt_elem_gt answers whether one container element has to move past another. mode 0 compares
; payloads as signed numbers; mode 1 compares them as indices into @str_tab and orders by the
; text — the interned index records the order strings first appeared in the program, so sorting
; strings by payload would sort them by arrival and look almost right until it did not.
define internal i32 @rt_elem_gt(i32 %a, i32 %b, i32 %mode) {
entry:
  %isstr = icmp eq i32 %mode, 1
  br i1 %isstr, label %strs, label %nums
nums:
  %c = icmp sgt i32 %a, %b
  %r = zext i1 %c to i32
  ret i32 %r
strs:
  %sa = getelementptr [4096 x i8*], [4096 x i8*]* @str_tab, i32 0, i32 %a
  %pa = load i8*, i8** %sa
  %sb = getelementptr [4096 x i8*], [4096 x i8*]* @str_tab, i32 0, i32 %b
  %pb = load i8*, i8** %sb
  %c2 = call i32 @strcmp(i8* %pa, i8* %pb)
  %g = icmp sgt i32 %c2, 0
  %r2ok = zext i1 %g to i32
  ret i32 %r2ok
}

; rt_str_order answers which of two interned texts comes first **in the text**, not in the program.
;
; An ordering has never been answerable from the payload: an index is the order the text arrived in
; @str_tab, so print(1 if "b" > "a" else 0) — the first two texts the program mentioned intern to 0
; and 1 — compared 0 with 1 and printed 0 where CPython prints 1 (roadmap Gap R.84). The same trap
; is why rt_sort above takes a mode and compares strings with strcmp; the comparison operators were
; never given the same question, so a > b, xs[0] < "c" and d["k"] >= "a" silently sorted texts by
; whichever spelling the program happened to mention first.
;
; -1, 0, 1, the sign of strcmp. Equality of two interned texts stays an index comparison, because
; interning is content-addressed (ADR 0173); ordering is the one thing that has to read the text.
define internal i32 @rt_str_order(i32 %a, i32 %b) {
entry:
  %sa = getelementptr [4096 x i8*], [4096 x i8*]* @str_tab, i32 0, i32 %a
  %pa = load i8*, i8** %sa
  %sb = getelementptr [4096 x i8*], [4096 x i8*]* @str_tab, i32 0, i32 %b
  %pb = load i8*, i8** %sb
  %c = call i32 @strcmp(i8* %pa, i8* %pb)
  %islt = icmp slt i32 %c, 0
  %br1 = zext i1 %islt to i32
  %isgt = icmp sgt i32 %c, 0
  %br2 = zext i1 %isgt to i32
  %neg = sub i32 0, %br1
  %r = add i32 %neg, %br2
  ret i32 %r
}

; rt_sort is an in-place insertion sort over a heap list. Stable, in the order-preserving sense
; that equal elements keep their relative positions — the compiled twin of the interpreter's
; sortElems, and the reason sorted(key=) will be able to sit on top of it later. The heap element
; array is 256 deep, so the quadratic cost is bounded by the representation, not by luck.
define internal void @rt_sort(i32 %h, i32 %mode) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %n = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  br label %outer
outer:
  %i = phi i32 [ 1, %entry ], [ %inext, %ostep ]
  %ic = icmp slt i32 %i, %n
  br i1 %ic, label %inner, label %done
inner:
  %j = phi i32 [ %i, %outer ], [ %jnext, %iswap ]
  %jgt = icmp sgt i32 %j, 0
  br i1 %jgt, label %jbody, label %ostep
jbody:
  %jm = sub i32 %j, 1
  %pa = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %jm
  %va = load i32, i32* %pa
  %pb = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %j
  %vb = load i32, i32* %pb
  %gt = call i32 @rt_elem_gt(i32 %va, i32 %vb, i32 %mode)
  %swap = icmp ne i32 %gt, 0
  br i1 %swap, label %iswap, label %ostep
iswap:
  store i32 %vb, i32* %pa
  store i32 %va, i32* %pb
  %ta = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %jm
  %tb = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %j
  %gta = load i32, i32* %ta
  %gtb = load i32, i32* %tb
  store i32 %gtb, i32* %ta
  store i32 %gta, i32* %tb
  %jnext = sub i32 %j, 1
  br label %inner
ostep:
  %inext = add i32 %i, 1
  br label %outer
done:
  ret void
}

; rt_reverse flips a heap list in place — the compiled xs.reverse().
define internal void @rt_reverse(i32 %h) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %n = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  br label %lo
lo:
  %i = phi i32 [ 0, %entry ], [ %inext, %body ]
  %hic = sub i32 %n, 1
  %hi2 = sub i32 %hic, %i
  %go = icmp slt i32 %i, %hi2
  br i1 %go, label %body, label %end
body:
  %pa = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %i
  %pb = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %hi2
  %va = load i32, i32* %pa
  %vb = load i32, i32* %pb
  store i32 %vb, i32* %pa
  store i32 %va, i32* %pb
  %ta = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %tb = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %hi2
  %gta = load i32, i32* %ta
  %gtb = load i32, i32* %tb
  store i32 %gtb, i32* %ta
  store i32 %gta, i32* %tb
  %inext = add i32 %i, 1
  br label %lo
end:
  ret void
}

; rt_list_copy builds a heap list with the same elements (and the same tags) as another one:
; sorted(xs) is a sort of a copy, so the argument keeps its order, which is the difference
; between the builtin and the method that a program can see.
define internal i32 @rt_list_copy(i32 %h) {
entry:
  %n = call i32 @rt_list_len(i32 %h)
  %nh = call i32 @rt_alloc(i32 1)
  br label %lo
lo:
  %i = phi i32 [ 0, %entry ], [ %inext, %body ]
  %c = icmp slt i32 %i, %n
  br i1 %c, label %body, label %done
body:
  %v = call i32 @rt_get_elem(i32 %h, i32 %i)
  %t = call i32 @rt_tag_of(i32 %h, i32 %i)
  call void @rt_set_elem(i32 %nh, i32 %i, i32 %v)
  call void @rt_tag_elem(i32 %nh, i32 %i, i32 %t)
  %inext = add i32 %i, 1
  br label %lo
done:
  ret i32 %nh
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
  %slot = getelementptr [4096 x i8*], [4096 x i8*]* @str_tab, i32 0, i32 %i
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

@.strempty = private unnamed_addr constant [1 x i8] c"\00"
@.strfull = private unnamed_addr constant [46 x i8] c"\52\75\6E\74\69\6D\65\45\72\72\6F\72\3A\20\74\6F\6F\20\6D\61\6E\79\20\64\69\73\74\69\6E\63\74\20\73\74\72\69\6E\67\20\76\61\6C\75\65\73\00"
declare ptr @malloc(i64)

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

; rt_str_case folds ASCII letters: mode 0 upper-cases, mode 1 lower-cases. Bytes outside the
; ASCII range are copied unchanged, which is the documented limit of the first cut — the
; interpreter folds Unicode case tables, the compiled backend folds ASCII (roadmap Gap R.47).
define internal i32 @rt_str_case(i32 %s, i32 %mode) {
entry:
  %p = call i8* @rt_str_ptr(i32 %s)
  %nb = call i32 @rt_str_len(i32 %s)
  %n1 = add i32 %nb, 1
  %sz = zext i32 %n1 to i64
  %buf = call ptr @malloc(i64 %sz)
  br label %scan
scan:
  %i = phi i32 [ 0, %entry ], [ %inext, %body ]
  %done = icmp sge i32 %i, %nb
  br i1 %done, label %term, label %body
body:
  %src = getelementptr i8, i8* %p, i32 %i
  %b = load i8, i8* %src
  %ub = icmp sgt i8 %b, 96
  %ub2 = icmp slt i8 %b, 123
  %upperable = and i1 %ub, %ub2
  %lb = icmp sgt i8 %b, 64
  %lb2 = icmp slt i8 %b, 91
  %lowerable = and i1 %lb, %lb2
  %upperMode = icmp eq i32 %mode, 0
  %lowerMode = icmp eq i32 %mode, 1
  %doUpper = and i1 %upperable, %upperMode
  %doLower = and i1 %lowerable, %lowerMode
  %flipU = select i1 %doUpper, i8 -32, i8 0
  %flipL = select i1 %doLower, i8 32, i8 0
  %flip = or i8 %flipU, %flipL
  %out = add i8 %b, %flip
  %dst = getelementptr i8, i8* %buf, i32 %i
  store i8 %out, i8* %dst
  %inext = add i32 %i, 1
  br label %scan
term:
  %t = getelementptr i8, i8* %buf, i32 %nb
  store i8 0, i8* %t
  %idx = call i32 @rt_str_intern(i8* %buf)
  ret i32 %idx
}

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
  %o = phi i32 [ 0, %entry ], [ %onext, %step ]
  %c = phi i32 [ 0, %entry ], [ %cnext, %step ]
  %done = icmp sge i32 %c, %kcl
  br i1 %done, label %fin, label %body
body:
  %at = getelementptr i8, i8* %p, i32 %o
  %b = load i8, i8* %at
  %end = icmp eq i8 %b, 0
  br i1 %end, label %fin, label %step
step:
  %m = and i8 %b, -64
  %iscont = icmp eq i8 %m, -128
  ; A code point is a byte that is NOT a continuation byte, so the count advances on the
  ; complement — counting the continuations instead walks past every ASCII character and the
  ; offset lands on the terminator, which is how s[1:3] came back empty.
  %isz = zext i1 %iscont to i32
  %one = sub i32 1, %isz
  %cnext = add i32 %c, %one
  %onext = add i32 %o, 1
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

; rt_str_strip trims ASCII whitespace at both ends. The interpreter trims the full Unicode set
; of spaces; the compiled backend trims bytes at or below space, which is the documented limit
; of this cut (roadmap Gap R.47) rather than a silent approximation.
define internal i32 @rt_str_strip(i32 %s) {
entry:
  %p = call i8* @rt_str_ptr(i32 %s)
  %nb = call i32 @rt_str_len(i32 %s)
  br label %lead
lead:
  %lo = phi i32 [ 0, %entry ], [ %lonext, %leadbody ]
  %past = icmp slt i32 %lo, %nb
  br i1 %past, label %leadcheck, label %done2
leadcheck:
  %la = getelementptr i8, i8* %p, i32 %lo
  %lb = load i8, i8* %la
  %lsp = icmp sgt i8 %lb, 32
  br i1 %lsp, label %done2, label %leadbody
leadbody:
  %lonext = add i32 %lo, 1
  br label %lead
done2:
  %lof = phi i32 [%lo, %lead], [%lo, %leadcheck]
  br label %tail
tail:
  %hib = phi i32 [ %nb, %done2 ], [ %hinext, %tailbody ]
  %above = icmp sgt i32 %hib, %lof
  br i1 %above, label %tailcheck, label %empty
tailcheck:
  %ta = getelementptr i8, i8* %p, i32 %hib
  %tam = getelementptr i8, i8* %ta, i32 -1
  %tb = load i8, i8* %tam
  %tsp = icmp sgt i8 %tb, 32
  br i1 %tsp, label %fin, label %tailbody
tailbody:
  %hinext = sub i32 %hib, 1
  br label %tail
fin:
  %len = sub i32 %hib, %lof
  %base = getelementptr i8, i8* %p, i32 %lof
  %idx = call i32 @rt_str_from_bytes(i8* %base, i32 %len)
  ret i32 %idx
empty:
  %zero = call i32 @rt_str_intern(i8* getelementptr ([1 x i8], [1 x i8]* @.strempty, i32 0, i32 0))
  ret i32 %zero
}

; rt_str_of_int is str(n) for a number the compiler cannot read: digits written backwards into
; a block that is then interned from the first digit onward. The interpreter has always answered
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

define internal i8* @rt_str_repr_ptr(i32 %i) {
entry:
  %slot = getelementptr [4096 x i8*], [4096 x i8*]* @str_repr_tab, i32 0, i32 %i
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

define internal void @rt_tag_elem(i32 %h, i32 %i, i32 %t) {
entry:
  %p = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  store i32 %t, i32* %p
  ret void
}

; rt_append_tagged is rt_append plus the tag the new slot carries. An append that forgets the
; tag leaves the slot reading back as whatever the *previous* tenant of that slot was — the
; tag array is not cleared on free — so a mixed list would print an interned string's index as
; a number. Appending and tagging are one operation precisely because they must not be two
; (roadmap L11.1, ADR 0187).
define internal void @rt_append_tagged(i32 %h, i32 %v, i32 %t) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %len
  store i32 %v, i32* %ep
  %tp = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %len
  store i32 %t, i32* %tp
  %len1 = add i32 %len, 1
  store i32 %len1, i32* %lp
  ret void
}

define internal i32 @rt_tag_of(i32 %h, i32 %i) {
entry:
  %p = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %v = load i32, i32* %p
  ret i32 %v
}

; rt_float_new allocates the float box that a container slot can hold: rt_alloc's own kind for
; this purpose (HeapKindFloat), the double stored in the parallel @float_box table, and the handle
; returned for the slot's payload. A failed allocation is -1, the same answer rt_alloc gives when
; the heap is full, and the callers that check it behave as they do for any exhausted heap.
define internal i32 @rt_float_new(double %d) {
entry:
  %h = call i32 @rt_alloc(i32 5)
  %bad = icmp slt i32 %h, 0
  br i1 %bad, label %fail, label %put
put:
  %p = getelementptr [1024 x double], [1024 x double]* @float_box, i32 0, i32 %h
  store double %d, double* %p
  ret i32 %h
fail:
  ret i32 -1
}

; rt_float_of reads a box back. It is the only way to a stored float, and it is what makes the
; comparison below able to say anything about numbers.
define internal double @rt_float_of(i32 %h) {
entry:
  %p = getelementptr [1024 x double], [1024 x double]* @float_box, i32 0, i32 %h
  %d = load double, double* %p
  ret double %d
}

; rt_payload_eq is the one answer to "do these two slot payloads denote the same value?", asked
; with the tags that make the payloads mean something. Within a tag it is payload equality, which
; is what makes a stored string and the number 1 different values even when both are the i32 1.
; Across the two numeric tags it is numeric equality, because Python's containers answer 1 == 1.0
; and [1] == [1.0] and 1.0 in [1] with True, and a tag comparison alone would
; answer False (roadmap L11.1, ADR 0233). fcmp oeq is the right predicate for the float cases:
; it gives -0.0 == 0.0 like Python, and NaN unequal to itself, which is what Python's own float
; comparison does. Any other pair of tags is unequal, bool included: Python does treat True as
; 1, but rendering True as a number is a separate known gap (ADR 0233's record), and answering
; this one wrongly in either direction is worse than answering it the way the tags say.
;
; It is also the answer a source-level equality between two tagged reads gets (roadmap L11.1, Gap
; R.79): an element of a container whose slots describe themselves is a (payload, tag) pair, and
; the only sound equality for a pair is this one. The naive helper that used to sit here — same
; tag, then compare the words — compared two float slots by box handle, so y = xs[0] over
; xs = [1.5, "a"] answered y == 1.5 false while printing 1.5 one line later.
define internal i32 @rt_payload_eq(i32 %a, i32 %ta, i32 %b, i32 %tb) {
entry:
  %sameTag = icmp eq i32 %ta, %tb
  br i1 %sameTag, label %same, label %mixed
same:
  %isFloat = icmp eq i32 %ta, 1
  br i1 %isFloat, label %floats, label %chkContainer
floats:
  %fa = call double @rt_float_of(i32 %a)
  %fb = call double @rt_float_of(i32 %b)
  %feq = fcmp oeq double %fa, %fb
  %fr = zext i1 %feq to i32
  ret i32 %fr
chkContainer:
  ; Two slots that both hold a container are equal when their *contents* are, which is
  ; rt_container_eq's question — the payload alone would answer it with two addresses, and
  ; [[1, 2]] == [[1, 2]] would be False for two literals that build two objects
  ; (roadmap L11.1, ADR 0189). rt_container_eq compares slots with rt_slot_eq, which calls
  ; back into this function, so the comparison recurses to any depth.
  %isList = icmp eq i32 %ta, 5
  %isDict = icmp eq i32 %ta, 6
  %isSet = icmp eq i32 %ta, 7
  %c1 = or i1 %isList, %isDict
  %c2 = or i1 %c1, %isSet
  br i1 %c2, label %containers, label %payloads
containers:
  %ce = call i32 @rt_container_eq(i32 %a, i32 %b)
  ret i32 %ce
payloads:
  %peq = icmp eq i32 %a, %b
  %pr = zext i1 %peq to i32
  ret i32 %pr
mixed:
  %aIsInt = icmp eq i32 %ta, 0
  %bIsFloat = icmp eq i32 %tb, 1
  %forward = and i1 %aIsInt, %bIsFloat
  br i1 %forward, label %intFloat, label %backward
backward:
  %aIsFloat = icmp eq i32 %ta, 1
  %bIsInt = icmp eq i32 %tb, 0
  %reverse = and i1 %aIsFloat, %bIsInt
  br i1 %reverse, label %floatInt, label %unequal
intFloat:
  %da = sitofp i32 %a to double
  %db = call double @rt_float_of(i32 %b)
  %eq1 = fcmp oeq double %da, %db
  %r1 = zext i1 %eq1 to i32
  ret i32 %r1
floatInt:
  %dc = call double @rt_float_of(i32 %a)
  %dd = sitofp i32 %b to double
  %eq2 = fcmp oeq double %dc, %dd
  %r2 = zext i1 %eq2 to i32
  ret i32 %r2
unequal:
  ret i32 0
}

; rt_slot_eq compares two container slots by (payload, tag), which is the only sound element
; comparison: a stored string is an index into @str_tab, and the number 1 is a payload that can
; equal it. The pair is asked of rt_payload_eq rather than compared directly so that a float slot
; and an int slot holding the same number answer equal, which is what [1] == [1.0] needs
; (roadmap L11.1, ADR 0189 and ADR 0233).
define internal i32 @rt_slot_eq(i32 %h1, i32 %i1, i32 %h2, i32 %i2) {
entry:
  %o1 = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h1
  %e1 = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %o1, i32 0, i32 2, i32 %i1
  %v1 = load i32, i32* %e1
  %o2 = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h2
  %e2 = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %o2, i32 0, i32 2, i32 %i2
  %v2 = load i32, i32* %e2
  %t1p = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h1, i32 %i1
  %t1 = load i32, i32* %t1p
  %t2p = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h2, i32 %i2
  %t2 = load i32, i32* %t2p
  %r32 = call i32 @rt_payload_eq(i32 %v1, i32 %t1, i32 %v2, i32 %t2)
  ret i32 %r32
}

; rt_container_eq compares two containers the way Python's == does: same kind, same size, and
; every element equal under rt_slot_eq. Lists compare position by position; sets and dicts compare
; by containment, because {1, 2} == {2, 1} is True and so is {"a": 1, "b": 2} == {"b": 2, "a": 1}
; — a positional walk would make both False. Non-containers and mismatched kinds are unequal,
; which is also the right answer for [1] == 1 (ADR 0189).
define internal i32 @rt_container_eq(i32 %a, i32 %b) {
entry:
  %rangeA = icmp slt i32 %a, 1024
  %rangeB = icmp slt i32 %b, 1024
  %bothrange = and i1 %rangeA, %rangeB
  br i1 %bothrange, label %kinds, label %no
kinds:
  %oa = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %a
  %ob = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %b
  %kap = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %oa, i32 0, i32 0
  %kbp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ob, i32 0, i32 0
  %ka = load i32, i32* %kap
  %kb = load i32, i32* %kbp
  %ksameness = icmp eq i32 %ka, %kb
  br i1 %ksameness, label %sizes, label %no
sizes:
  %lap = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %oa, i32 0, i32 1
  %lbp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %ob, i32 0, i32 1
  %la = load i32, i32* %lap
  %lb = load i32, i32* %lbp
  %lsame = icmp eq i32 %la, %lb
  br i1 %lsame, label %disp, label %no
disp:
  ; HeapKindList = 1, HeapKindDict = 2, HeapKindSet = 3
  %islist = icmp eq i32 %ka, 1
  br i1 %islist, label %li_entry, label %chkset
chkset:
  %isset = icmp eq i32 %ka, 3
  br i1 %isset, label %si_entry, label %chkdict
chkdict:
  %isdict = icmp eq i32 %ka, 2
  br i1 %isdict, label %di_entry, label %no

li_entry:
  br label %li
li:
  %li_i = phi i32 [ 0, %li_entry ], [ %li_next, %li_step ]
  %li_c = icmp slt i32 %li_i, %la
  br i1 %li_c, label %li_body, label %yes
li_body:
  %li_s = call i32 @rt_slot_eq(i32 %a, i32 %li_i, i32 %b, i32 %li_i)
  %li_ne = icmp ne i32 %li_s, 0
  br i1 %li_ne, label %li_step, label %no
li_step:
  %li_next = add i32 %li_i, 1
  br label %li

si_entry:
  br label %si
si:
  %si_i = phi i32 [ 0, %si_entry ], [ %si_next, %si_step ]
  %si_c = icmp slt i32 %si_i, %la
  br i1 %si_c, label %si_body, label %yes
si_body:
  br label %sj
sj:
  %sj_j = phi i32 [ 0, %si_body ], [ %sj_next, %sj_step ]
  %sj_c = icmp slt i32 %sj_j, %lb
  br i1 %sj_c, label %sj_body, label %no
sj_body:
  %sj_s = call i32 @rt_slot_eq(i32 %a, i32 %si_i, i32 %b, i32 %sj_j)
  %sj_ne = icmp ne i32 %sj_s, 0
  br i1 %sj_ne, label %si_step, label %sj_step
sj_step:
  %sj_next = add i32 %sj_j, 1
  br label %sj
si_step:
  %si_next = add i32 %si_i, 1
  br label %si

di_entry:
  br label %di
di:
  %di_i = phi i32 [ 0, %di_entry ], [ %di_next, %di_step ]
  %di_c = icmp slt i32 %di_i, %la
  br i1 %di_c, label %di_body, label %yes
di_body:
  %di_k = mul i32 %di_i, 2
  %di_v = add i32 %di_k, 1
  br label %dj
dj:
  %dj_j = phi i32 [ 0, %di_body ], [ %dj_next, %dj_step ]
  %dj_c = icmp slt i32 %dj_j, %lb
  br i1 %dj_c, label %dj_body, label %no
dj_body:
  %dj_k = mul i32 %dj_j, 2
  %dj_v = add i32 %dj_k, 1
  %dj_key = call i32 @rt_slot_eq(i32 %a, i32 %di_k, i32 %b, i32 %dj_k)
  %dj_kne = icmp ne i32 %dj_key, 0
  br i1 %dj_kne, label %dj_val, label %dj_step
dj_val:
  %dj_val_eq = call i32 @rt_slot_eq(i32 %a, i32 %di_v, i32 %b, i32 %dj_v)
  %dj_vne = icmp ne i32 %dj_val_eq, 0
  br i1 %dj_vne, label %di_step, label %no
dj_step:
  %dj_next = add i32 %dj_j, 1
  br label %dj
di_step:
  %di_next = add i32 %di_i, 1
  br label %di

yes:
  ret i32 1
no:
  ret i32 0
}

; rt_print_container_value renders a container that some other container's slot holds. The slot's
; tag says list, dict or set; only the object's own record says how its elements are stored — plain
; numbers, interned text, or per-element tags — so that choice is made at run time from @heap's kind
; and @estr's element bits instead of from what the builder happened to know. Guessing it statically
; is how a nested list of text came to print its interned indexes (roadmap L11.1).
define internal void @rt_print_container_value(i32 %h, i32 %quote) {
entry:
  %o = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %kp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %o, i32 0, i32 0
  %kind = load i32, i32* %kp
  %isList = icmp eq i32 %kind, 1
  br i1 %isList, label %list, label %chkDict
list:
  ; Each container printer asks its own object how its elements are stored — plain numbers,
  ; interned text, or per-slot tags — so the delegate is chosen by kind here and by the object
  ; there, never by a guess in this scope (roadmap Gap J.5, L11.1).
  call void @rt_print_list(i32 %h, i32 0)
  ret void
chkDict:
  %isDict = icmp eq i32 %kind, 2
  br i1 %isDict, label %dict, label %chkSet
dict:
  call void @rt_dict_print(i32 %h, i32 0)
  ret void
chkSet:
  %isSet = icmp eq i32 %kind, 3
  br i1 %isSet, label %set, label %nothing
set:
  call void @rt_set_print(i32 %h, i32 0)
  ret void
nothing:
  ret void
}

; rt_print_mixed_value renders one element by its tag, using the same three texts the
; interpreter's Repr produces: numbers with %d, interned strings through the repr slot
; (Python quotes elements inside a container), and the None singleton as "None".
define internal void @rt_print_mixed_value(i32 %v, i32 %t, i32 %quote) {
entry:
  %isStr = icmp eq i32 %t, 4
  br i1 %isStr, label %str, label %checkNone
str:
  ; Inside a container Python shows repr() -- quoted, with its chosen quotes -- and at top
  ; level str() -- the text itself. The caller knows which context it is in; only the tag
  ; cannot say, so the flag comes from the call site (ADR 0185).
  %q = icmp ne i32 %quote, 0
  br i1 %q, label %strrepr, label %strraw
strrepr:
  %rp = call i8* @rt_str_repr_ptr(i32 %v)
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmts, i32 0, i32 0), i8* %rp)
  ret void
strraw:
  %sp = call i8* @rt_str_ptr(i32 %v)
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmts, i32 0, i32 0), i8* %sp)
  ret void
checkNone:
  %isNone = icmp eq i32 %t, 3
  br i1 %isNone, label %none, label %checkFloat
none:
  call void @rt_print_none(i32 0)
  ret void
checkFloat:
  ; A float slot holds the handle of a @float_box entry, so the bits come from the box and the
  ; rendering comes from rt_fmt_double, the same helper that prints a bare float with Python's
  ; Python's 1.0 rather than printf's 1. Inside a container Python shows repr(), which for a
  ; float is its str() — the same call, not a second formatter (roadmap L11.1, ADR 0233).
  %isFloat = icmp eq i32 %t, 1
  br i1 %isFloat, label %flt, label %checkContainer
flt:
  %d = call double @rt_float_of(i32 %v)
  %fp = call i8* @rt_fmt_double(double %d)
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmts, i32 0, i32 0), i8* %fp)
  ret void
checkContainer:
  ; A slot that names another container is rendered by that container's own printer, chosen at
  ; run time from its record rather than from what the builder knew — the tag says list, only
  ; the object says whether its elements are numbers, text, or tagged (roadmap L11.1).
  %isList = icmp eq i32 %t, 5
  br i1 %isList, label %cont, label %checkDict
checkDict:
  %isDict = icmp eq i32 %t, 6
  br i1 %isDict, label %cont, label %checkSet
checkSet:
  %isSet = icmp eq i32 %t, 7
  br i1 %isSet, label %cont, label %num
cont:
  call void @rt_print_container_value(i32 %v, i32 %quote)
  ret void
num:
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmti, i32 0, i32 0), i32 %v)
  ret void
}

; rt_print_list_mixed walks a list whose elements carry per-element tags. It is rt_print_list
; with the container-wide @estr[h] flag replaced by a per-element tag lookup -- the difference
; between "this list is the string list" and "this slot holds a string".
define internal void @rt_print_list_mixed(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
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
  %tp = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %t = load i32, i32* %tp
  br i1 %is0, label %first, label %sep
first:
  call void @rt_print_mixed_value(i32 %e, i32 %t, i32 1)
  br label %cont
sep:
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  call void @rt_print_mixed_value(i32 %e, i32 %t, i32 1)
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
  ; bit 8 says the slots describe themselves: a list that mixes kinds, or whose elements are
  ; handles into other containers, has no single kind to report, so it prints element by element
  ; (roadmap L11.1 (1b), ADR 0232). Without this arm a nested list is a handle printed as a number
  ; when the printer was chosen by kind from outside — which is how an inner list reached through
  ; rt_print_container_value used to answer [5].
  %mixedBit = and i32 %flags, 8
  %isMixed = icmp ne i32 %mixedBit, 0
  br i1 %isMixed, label %mixed, label %notMixed
mixed:
  call void @rt_print_list_mixed(i32 %h, i32 %nl)
  ret void
notMixed:
  %eb = and i32 %flags, 1
  %isStr = icmp ne i32 %eb, 0
  %istr = zext i1 %isStr to i32
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtlopen, i32 0, i32 0))
  br label %loop
loop:
  ; The predecessor is notMixed, not entry: a phi names the blocks that actually branch to it,
  ; and the tag dispatch above sits in between (llc calls a wrong one a malformed PHI).
  %i = phi i32 [ 0, %notMixed ], [ %i1, %cont ]
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

; rt_dict_put_tagged is rt_dict_put with the tags the two new slots carry. A dict interleaves key
; and value in the element array, so an entry owns tag slots 2i and 2i+1; updating an existing
; key rewrites the value's tag, because a key that now maps to a different kind of thing must not
; keep printing and comparing as the old one (ADR 0187's rule, applied to dicts by ADR 0189).
define internal void @rt_dict_put_tagged(i32 %h, i32 %k, i32 %v, i32 %kt, i32 %vt) {
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
  ; The key is compared as a (payload, tag) pair: {0: "n"} and {"0": "s"} are one entry each, and
  ; they are different entries only because the tag says so (ADR 0232).
  %km = call i32 @rt_slot_matches(i32 %h, i32 %idx, i32 %k, i32 %kt)
  %eq = icmp ne i32 %km, 0
  br i1 %eq, label %upd, label %cont
upd:
  %idx2 = add i32 %idx, 1
  %vp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx2
  store i32 %v, i32* %vp
  %utg = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %idx2
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
  %ktg = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %idx3
  store i32 %kt, i32* %ktg
  %vtg = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %idx4
  store i32 %vt, i32* %vtg
  %len2 = add i32 %len, 1
  store i32 %len2, i32* %lp
  ret void
}

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

; rt_contains_tagged is rt_contains with the tag compared alongside the payload. It answers the
; same question for lists (elements), dicts (keys) and sets (members), and answers it correctly:
; an @str_tab index and an integer of the same number are different values, and only the tag says
; which one a slot holds (ADR 0189 made the tags always-available, ADR 0232 makes them always
; consulted). rt_contains stays for a needle whose kind the compiler cannot prove.
define internal i32 @rt_contains_tagged(i32 %h, i32 %v, i32 %t) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
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

; --- tagged dict and set slots (roadmap L11.1 (1b), ADR 0232) -------------------
;
; rt_slot_matches answers "does this slot hold exactly this value?" — payload *and* tag. The
; payload alone cannot answer it: a stored string is an index into @str_tab, so the key "0" and
; the number 0 are the same i32, and a dict or set that compares payloads alone hands back the
; wrong entry for one of them. This is the soundness hole ADR 0189 wrote down and left for here:
; equality carried the tag, lookup and dedup did not.
define internal i32 @rt_slot_matches(i32 %h, i32 %i, i32 %v, i32 %t) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  %e = load i32, i32* %ep
  %tp = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %et = load i32, i32* %tp
  %m = call i32 @rt_payload_eq(i32 %e, i32 %et, i32 %v, i32 %t)
  ret i32 %m
}

; rt_dict_find returns the entry whose key is the (payload, tag) pair, or -1 when the dict has no
; such key. One scan, several readers: membership, a value read and the update path of a put all
; ask this same question, and they must ask the same way or they disagree about the dict.
define internal i32 @rt_dict_find(i32 %h, i32 %k, i32 %kt) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
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
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
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
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
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
  %tp = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %vslot
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

; rt_set_discard_tagged removes a member by (payload, tag) and shifts the tail of BOTH arrays.
; Shifting the payloads while leaving the tags where they were would print every member after the
; removed one through the kind of its neighbour.
define internal void @rt_set_discard_tagged(i32 %h, i32 %v, i32 %t) {
entry:
  %at = call i32 @rt_set_find(i32 %h, i32 %v, i32 %t)
  %absent = icmp slt i32 %at, 0
  br i1 %absent, label %done, label %work
work:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %last = sub i32 %len, 1
  br label %shift
shift:
  %j = phi i32 [ %at, %work ], [ %jnext, %shiftdo ]
  %go = icmp slt i32 %j, %last
  br i1 %go, label %shiftdo, label %shrink
shiftdo:
  %src = add i32 %j, 1
  %sp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %src
  %sv = load i32, i32* %sp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %j
  store i32 %sv, i32* %dp
  %st = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %src
  %stv = load i32, i32* %st
  %dt = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %j
  store i32 %stv, i32* %dt
  %jnext = add i32 %j, 1
  br label %shift
shrink:
  store i32 %last, i32* %lp
  ret void
done:
  ret void
}

; rt_dict_print_mixed walks a dict whose key and value slots carry their own tags: each position
; asks the tag what it is instead of asking the object what kind of dict it is. That is the
; difference between {"a": 1} and {"a": 1, "b": "x"} — the second has no kind.
define internal void @rt_dict_print_mixed(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtdopen, i32 0, i32 0))
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
  %kt = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %idx
  %keyTag = load i32, i32* %kt
  %vt = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %idx2
  %valTag = load i32, i32* %vt
  %first = icmp eq i32 %i, 0
  br i1 %first, label %emit, label %sepd
sepd:
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  br label %emit
emit:
  call void @rt_print_mixed_value(i32 %k, i32 %keyTag, i32 1)
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtcolon, i32 0, i32 0))
  call void @rt_print_mixed_value(i32 %v, i32 %valTag, i32 1)
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

; rt_set_print_mixed is the same walk for a set, empty included: Python renders the empty set as
; set(), and {} is a dict, so the empty case cannot fall through to the general loop.
define internal void @rt_set_print_mixed(i32 %h, i32 %nl) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
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
  %ep = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2, i32 %i
  %e = load i32, i32* %ep
  %tp = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %t = load i32, i32* %tp
  %first = icmp eq i32 %i, 0
  br i1 %first, label %emit, label %sepd
sepd:
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  br label %emit
emit:
  call void @rt_print_mixed_value(i32 %e, i32 %t, i32 1)
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


@.fmtemptyset = private unnamed_addr constant [6 x i8] c"set()\00"
@.fmtsopen = private unnamed_addr constant [2 x i8] c"{\00"
@.fmtsitem = private unnamed_addr constant [3 x i8] c"%d\00"
@.fmtssep = private unnamed_addr constant [3 x i8] c", \00"
@.fmtsclose = private unnamed_addr constant [2 x i8] c"}\00"

; rt_set_add_tagged is rt_set_add with the tag for the slot it writes. Adding a member that is
; already there writes nothing, and the tag store below then lands one past the end — harmless,
; because no loop reads past len, and the member that is there already keeps the tag it was added
; with (ADR 0189).
define internal void @rt_set_add_tagged(i32 %h, i32 %v, i32 %t) {
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
  ; A member is already present only when payload and tag both agree; otherwise adding "0" to a
  ; set holding 0 loses one of them (ADR 0232).
  %m = call i32 @rt_slot_matches(i32 %h, i32 %i, i32 %v, i32 %t)
  %eq = icmp ne i32 %m, 0
  br i1 %eq, label %ret, label %cont
ret:
  ret void
cont:
  %next = add i32 %i, 1
  br label %check
add:
  %kp2 = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %len
  store i32 %v, i32* %kp2
  %tg = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %len
  store i32 %t, i32* %tg
  %len2 = add i32 %len, 1
  store i32 %len2, i32* %lp
  ret void
}

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
  ; bit 8: the members describe themselves, so ask each slot instead of the object (ADR 0232)
  %fsp0 = getelementptr [1024 x i32], [1024 x i32]* @estr, i32 0, i32 %h
  %flags0 = load i32, i32* %fsp0
  %mixedBit = and i32 %flags0, 8
  %isMixed = icmp ne i32 %mixedBit, 0
  br i1 %isMixed, label %mixed, label %notMixed
mixed:
  call void @rt_set_print_mixed(i32 %h, i32 %nl)
  ret void
notMixed:
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
  ; writing the value writes its presence, the way a container write writes its tag (ADR 0175)
  %sp = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @inst_set, i32 0, i32 %h, i32 %slot
  store i32 1, i32* %sp
  ret void
}

; rt_inst_clear zeroes an instance's attribute-presence row, and is emitted at each instantiation.
; Without it a heap slot inherited from a collected object would still claim its old attributes.
define internal void @rt_inst_clear(i32 %h, i32 %n) {
entry:
  br label %loop
loop:
  %i = phi i32 [ 0, %entry ], [ %in, %body ]
  %more = icmp slt i32 %i, %n
  br i1 %more, label %body, label %done
body:
  %sp = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @inst_set, i32 0, i32 %h, i32 %i
  store i32 0, i32* %sp
  %in = add i32 %i, 1
  br label %loop
done:
  ret void
}

; rt_inst_has answers "has this attribute been written?" — the question a class pattern asks of each
; capture name, and the question Gap R.19 will ask of a plain attribute read.
define internal i32 @rt_inst_has(i32 %h, i32 %slot) {
entry:
  %sp = getelementptr [1024 x [256 x i32]], [1024 x [256 x i32]]* @inst_set, i32 0, i32 %h, i32 %slot
  %v = load i32, i32* %sp
  ret i32 %v
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
  ; How many element words to walk. A dict's len counts *entries* and each entry occupies two
  ; slots, key then value, so walking len words covers only the first entry: every later entry's
  ; key and value went unmarked, and a float box or nested container stored there was swept while
  ; the dict still referenced it — the recycled slot then handed back somebody else's bits, which
  ; is how a two-entry dict came to print its second key as the first one's after an in-test
  ; allocated a temporary. List and set keep one word per element (roadmap L11.1, ADR 0233).
  %lp0 = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len0 = load i32, i32* %lp0
  %kd = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 0
  %kind = load i32, i32* %kd
  %isDict = icmp eq i32 %kind, 2
  %stride = select i1 %isDict, i32 2, i32 1
  %span = mul i32 %len0, %stride
  br label %e.loop
e.loop:
  %e = phi i32 [ 0, %e.init ], [ %e.nxt, %e.inc ]
  %e.end = icmp sge i32 %e, %span
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

// GenerateIR runs codegen and returns the module. No debug records are emitted:
// every caller that wants them asks for them (GenerateIRWithOptions /
// GenerateIRReport), so the IR an agent reads with --emit-llvm is byte-identical
// to the IR it was before L8.5 (docs/operations.md § Debug info, ADR 0231).
func GenerateIR(prog *Program) (string, error) {
	ir, _, err := GenerateIRReport(prog, nil)
	return ir, err
}

// GenerateIRWithOptions is GenerateIR with the emitter's choices spelled out.
func GenerateIRWithOptions(prog *Program, opts *IRGenOptions) (string, error) {
	ir, _, err := GenerateIRReport(prog, opts)
	return ir, err
}

// GenerateIRReport compiles a program to textual IR and, when debug records were
// requested, accounts for them (L8.5). The account is read back from the module
// text that was produced, so what `--debug-info` prints is what the module says
// and not what the emitter hoped for.
func GenerateIRReport(prog *Program, opts *IRGenOptions) (string, *DebugInfo, error) {
	imports, err := resolveImports(prog)
	if err != nil {
		return "", nil, err
	}
	g := &irGen{
		classIDs: map[string]int{}, nextSlot: 1,
		listVars:     map[string]bool{},
		runtimeDicts: map[string]bool{}, runtimeSets: map[string]bool{}, noneVars: map[string]bool{},
		listElemStr: map[string]bool{}, setElemStr: map[string]bool{}, dictKeyStr: map[string]bool{}, dictValStr: map[string]bool{}, listElemInt: map[string]bool{}, setElemInt: map[string]bool{}, dictKeyInt: map[string]bool{}, dictValInt: map[string]bool{}, internedVars: map[string]bool{}, strAttrs: map[string]bool{}, mixedLists: map[string]bool{}, mixedDicts: map[string]bool{}, mixedSets: map[string]bool{}, taggedVars: map[string]bool{}, strParamOf: strArgKinds(prog), strFuncs: strReturningFuncs(prog), strFillOf: stringFillingParams(prog), imports: imports, sym: map[string]string{}, allocd: map[string]bool{}, funcs: map[string]bool{}, funcBind: map[string]string{}, externs: map[string]*ExternDecl{}, genFuncs: map[string]bool{}, listOperands: map[string]bool{}, floatFuncs: map[string]bool{}, floatTemps: map[string]bool{}, fds: map[string]*FuncDef{}, params: map[string]string{}, fmtIdx: 0, strIdx: 0, tmp: 0, ldN: 0}
	// What a compiled function body may know about its module: literal bindings the module never
	// rebinds are values; the rest stay refused with a message that says so (ADR 0227).
	g.moduleConsts, g.moduleNames = moduleEnvFor(prog)
	g.moduleSlots = moduleSlotNames(prog, g.moduleConsts, g.moduleNames)
	// Which container variables are still the literal they were bound to, which is what decides
	// whether reading one of their slots can be checked against the tag the builder wrote (ADR 0241).
	g.containerLits = containerLiteralsOf(prog)
	// Which names a body may read before assigning them is the checker's question, asked once here
	// so codegen does not grow a second, subtly different dataflow rule (ADR 0228).
	g.unwritten = UnwrittenReads(prog)
	// What a class pattern denotes, and every attribute name the program can write, come from the AST
	// and are settled *before* emission: the presence row cleared at each instantiation is only sound
	// if its length already covers every attribute slot the program will ever name (ADR 0235).
	g.classPat = classPatternsOf(prog)
	for _, a := range g.classPat.attrs {
		g.attrSlot(a)
	}
	g.fdAlias = map[*FuncDef]*FuncDef{}
	for nm, sym := range g.moduleSlots {
		g.moduleSlotDecls += fmt.Sprintf("@%s = global i32 0 ; module binding %q, read by a function body (ADR 0227)\n", sym, nm)
	}
	// Debug records are opt-in; the marks that make them are one append per statement
	// and per function definition, collected whether or not they will be used, because
	// `--emit-source-map` wants the line table without asking for DWARF. Turning marks
	// into metadata is the gated part (ADR 0231).
	g.dbgOn = opts != nil && opts.Debug != nil
	g.decoratorNames = map[string]bool{}
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			for _, d := range fd.Decorators {
				if nm, ok := d.(*Name); ok {
					g.decoratorNames[nm.Value] = true
				}
			}
		}
	}
	// pre-scan top-level for user function names
	// escape analysis: dead list-literal assignments skip rt_alloc
	g.deadLists = deadListAssignments(prog.Stmts)
	for _, st := range prog.Stmts {
		if fd, ok := st.(*FuncDef); ok {
			g.funcs[fd.Name] = true
			g.fds[fd.Name] = fd
			// A module-level function whose return is a string returns a @str_tab index, and
			// saying so here — at the pre-scan, before any call site is lowered — is what lets
			// `print(get()[1])` know that `get()` is a string at all. Methods were registered
			// this way since ADR 0224; the module's own functions were not, which is why
			// `s[1]` worked and `get()[1]` refused (ADR 0229).
			if methodReturnsStr(fd) {
				g.strFuncs[fd.Name] = true
			}
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
				return "", nil, err
			}
		}
	}
	if err := g.emitModuleFuncs(&b); err != nil {
		return "", nil, err
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

	// The module's statements are `main`'s body, and `main` is a function a debugger can be
	// asked about, so it gets a subprogram too. Its position is the first statement's, which is
	// what the prologue (root-stack reset, GC calls) really belongs to (ADR 0231).
	// `main`'s position is the first statement that is really its own code: a `def` or a
	// `class` at the top of a file is emitted as its own function, and the module's
	// prologue belongs to the first line that runs, not to the first line that exists.
	mainSpan := Span{Line: 1, Col: 1}
	for _, st := range prog.Stmts {
		switch st.(type) {
		case *FuncDef, *ClassDef, *ImportStmt, *ExternDecl:
			continue
		}
		if !st.Span().IsZero() {
			mainSpan = st.Span()
			break
		}
	}
	g.dbgDefine(&b, "main", "main", mainSpan)
	b.WriteString("define i32 @main() {\nentry:\n")
	// Module-level code is its own variable-binding scope. funcDef resets these
	// per body; without a reset here, an alloca emitted for a function parameter
	// named `xs` would make `xs = [1, 2]` in main skip its own alloca and store
	// through the (out-of-scope) `%_xs` register inside the function.
	// Module-level code is its own scope too (see beginScope).
	restoreScope := g.beginScope()
	defer restoreScope()
	// Which names hold @str_tab indices, and which functions hand one back — asked of the whole
	// program before any of it is lowered, so a use after a binding knows the kind the binding
	// gave it (ADR 0229). Asked *after* beginScope: that reset clears the per-scope kind maps, so
	// a scan answered before it would be silently thrown away — the same lesson the module-level
	// written-flags learned (ADR 0228), one cycle apart.
	g.scanStringBindings(prog.Stmts)
	// The module's own statements get written-flags too: an unwritten read at top level is a
	// NameError in CPython and must not read the alloca's previous contents (ADR 0228). The nil key
	// is the module's entry in the checker's table, and `inFunc` being false is what makes the trap
	// raise NameError rather than UnboundLocalError. It goes *after* beginScope: that reset clears the
	// slot bookkeeping, and flagging before it emitted a second alloca for the same name -- a module
	// `llc` rejects as "multiple definition of local value", which the exit-code contract calls a
	// compiler bug (ADR 0166).
	doneModuleFlags := g.enterBoundFlags(nil, prog.Stmts)
	defer doneModuleFlags()
	g.emitBoundAllocas(&b)
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
		g.dbgMark(&b, st.Span())
		g.gcCall(&b)
		if err := g.stmt(&b, st); err != nil {
			return "", nil, err
		}
	}
	g.inMain = false
	// A refusal recorded while a method body was being emitted is a compile error and is
	// reported as one; it must never reach `llc` as a half-built function (ADR 0166,
	// roadmap Gap R.41).
	if g.emitErr != nil {
		return "", nil, g.emitErr
	}
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
	// script that ran it. Report it on stderr and exit non-zero, like the interpreter --
	// and with the *same code as every other trap*, because ADR 0211 says a failure class
	// has one code whichever path produced it. This is the compiled binary's own exit code,
	// not the CLI's: `gustyc --aot prog.gy` and `./prog` must not disagree about whether the
	// program trapped, and 1 belongs to a compile error, so leaving 1 here made a trap look
	// like a compiler bug to any script running the binary directly.
	if g.raiseUsed {
		b.WriteString("  %exn.m = load i8*, i8** @exn_msg\n")
		b.WriteString("  call void @rt_die(i8* %exn.m)\n")
		b.WriteString("  ret i32 3\n")
	} else {
		b.WriteString("  ret i32 0\n")
	}
	b.WriteString("}\n")
	// assemble output
	var out strings.Builder
	g.emitEnvGlobals()
	// Which runtime blocks a module needs is derived from what the module *references*,
	// not only from a flag each emitting path remembers. The flags (`heapUsed`,
	// `raiseUsed`, `floatFmtUsed`) are how a path announces what it lowers, and the ones
	// that forgot are exactly the invalid modules LLVM catches and the exit-code contract
	// calls a compiler bug: `def txt(): return "hi"` returns the interned index — correct,
	// ADR 0174 — but never marked the heap runtime used, so the module called
	// `@rt_str_intern2` with no definition of it, the same signature the roadmap blamed on
	// the await path (roadmap Gap R.2, ADR 0209). The flags stay: a container read raises
	// without an explicit `raise`, a printed float needs the formatter. But no block is
	// omitted while the emitted code mentions one of its names.
	bodyText := b.String()
	if g.raiseUsed || g.heapUsed || runtimeBlockReferenced(raiseRuntimeIR, bodyText) {
		g.globals.WriteString(raiseRuntimeIR)
	}
	// rt_fmt_double is only referenced by Python-style float rendering, so it travels in its own
	// block: a program that never prints a float does not pay for the snprintf/strtod
	// declarations. But the mixed container printer references it from the heap block, so the
	// heap block's own text counts as a reference too — a gate that reads only the body would
	// omit the formatter for `[1.5, "a"]` and produce a module with an undefined internal call,
	// which is the exact failure mode the preamble above warns about.
	needHeap := g.heapUsed || runtimeBlockReferenced(heapRuntimeIR, bodyText)
	if g.floatFmtUsed || runtimeBlockReferenced(floatRuntimeIR, bodyText) || needHeap {
		g.globals.WriteString(floatRuntimeIR)
	}
	if needHeap {
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
	out.WriteString(g.moduleSlotDecls)
	// A closure body that could not be lowered and that a decorated call does not run is said here,
	// in the module, where --emit-llvm and the verifier can both see it. It is not a hidden failure:
	// the program runs because the trampoline, not this closure, is what executes (ADR 0227).
	for _, note := range g.closureBodyNotes {
		out.WriteString("; note: " + note + "\n")
	}
	// The debug pass needs to know where in the finished module each builder's writes landed:
	// marks were recorded as offsets into `globals` and into the body builder, and those two
	// address spaces become one here (L8.5, ADR 0231).
	out.WriteString(g.strGlobals.String())
	globalsPrefix := out.Len()
	out.WriteString(g.globals.String())
	EmitABI(&g.decls)
	bodyPrefix := out.Len() + len(g.decls)
	out.WriteString(g.decls)
	out.WriteString(b.String())
	// PIC Level = 2 module flag: forces llc to emit position-independent code
	// so string constants in .rodata are referenced PIC-safely. Without it llc
	// defaults to the static relocation model, which emits 32-bit absolute
	// relocations (e.g. R_X86_64_32) that the default PIE link (cc) rejects.
	out.WriteString("!llvm.module.flags = !{!0}\n")
	out.WriteString("!0 = !{i32 2, !\"PIC Level\", i32 2}\n")
	// The line table is added to the module as assembled text, before the slot hoisting that
	// moves allocation instructions to the top of their function: a hoisted `alloca` keeps the
	// `!dbg` it was tagged with, so the slot stays attributed to the assignment that created it
	// rather than to the `def` (ADR 0231, and ADR 0181 for the hoisting itself).
	if opts != nil && opts.Debug != nil {
		text, art := attachDebugInfo(out.String(), g.dbgMarks, g.dbgFuncs, globalsPrefix, bodyPrefix, opts.Debug)
		// Slot hoisting first, *then* the account of the table: hoisting moves allocation
		// lines to the top of their function, and a line table whose rows were counted
		// before that happened would publish IR positions the shipped module does not have.
		shipped := hoistAllocas(text)
		return shipped, readBackDebugInfo(shipped, art), nil
	}
	// Every variable slot has to be allocated once per call for its *address* to
	// identify it — see hoistAllocas (ADR 0181).
	return hoistAllocas(out.String()), nil, nil
}

// iterableIsRuntimeString reports whether a `for … in` right-hand side is text that codegen
// cannot walk: an interpolated or concatenated string, a folded string expression, a variable
// holding a @str_tab index, or a call to a function known to return one. A *literal* is not
// included: the loop unrolls it above (ADR 0208).
func (g *irGen) iterableIsRuntimeString(e Expr) bool {
	if e == nil {
		return false
	}
	if _, ok := e.(*StrLit); ok {
		return false
	}
	if _, ok := e.(*ListLit); ok {
		return false
	}
	if isStringExpr(e) {
		return true
	}
	if _, ok := g.stringVal(e); ok {
		return true
	}
	if nm, ok := e.(*Name); ok {
		if g.internedVars[nm.Value] {
			return true
		}
		if g.strVals != nil {
			if _, isStr := g.strVals[nm.Value]; isStr {
				return true
			}
		}
		return false
	}
	if c, ok := e.(*Call); ok {
		return g.callReturnsStr(c)
	}
	return false
}

// runtimeBlockReferenced reports whether emitted module code mentions any name that the
// given runtime block defines. The decision is derived from the artifact rather than from a
// flag, so a helper cannot be called without being defined no matter which codegen path
// emits the call — and a data global (@str_tab, @gc.roots) cannot be referenced while its
// definition is missing.
//
// The block is scanned for `define … @name(` and `@name = internal global/constant` lines,
// which is what those files already look like; nothing is maintained by hand, so adding a
// helper to a block cannot reintroduce the bug.
func runtimeBlockReferenced(block, module string) bool {
	for _, ln := range strings.Split(block, "\n") {
		t := strings.TrimSpace(ln)
		var name string
		callForm := false
		switch {
		case strings.HasPrefix(t, "define "):
			i := strings.Index(t, "@")
			if i < 0 {
				continue
			}
			name = t[i+1:]
			if j := strings.IndexAny(name, "( "); j >= 0 {
				name = name[:j]
			}
			callForm = true
		case strings.HasPrefix(t, "@"):
			rest := t[1:]
			sp := strings.IndexAny(rest, " =")
			if sp < 0 {
				continue
			}
			tail := strings.TrimLeft(strings.TrimSpace(rest[sp:]), "= ")
			if !strings.HasPrefix(tail, "internal global") && !strings.HasPrefix(tail, "internal constant") && !strings.HasPrefix(tail, "private") && !strings.HasPrefix(tail, "common") {
				continue
			}
			name = rest[:sp]
		}
		if name == "" {
			continue
		}
		if callForm {
			if strings.Contains(module, "@"+name+"(") {
				return true
			}
			continue
		}
		if strings.Contains(module, "@"+name) {
			return true
		}
	}
	return false
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
	// rtComps records the comprehensions whose elements are computed at runtime, so a
	// consumer (sum/min/max/len folding) cannot read an empty compile-time element set and
	// answer 0 for a list that has elements (roadmap L11.7, ADR 0192).
	rtComps map[*Comp]bool
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
	// mixedLists records list variables whose elements carry per-element tags (more than
	// one kind, all taggable: numbers, interned strings, None). Printing them works; every
	// other element-wise use refuses rather than reading a tag through one static kind
	// (roadmap L11.1, ADR 0184).
	mixedLists map[string]bool
	// mixedDicts and mixedSets are the same rule for the other two containers: the slots
	// describe themselves, so the container has no single element kind to record. A
	// container in these maps prints, dedups and looks up by per-slot tag, and the static
	// dictKey*/dictVal*/setElem* maps say nothing about it (roadmap L11.1 (1b), ADR 0232).
	mixedDicts map[string]bool
	mixedSets  map[string]bool
	// taggedVars records variables bound by a loop over a mixed list: their value slot is an
	// i32 whose meaning depends on the companion tag slot, so printing dispatches on the tag
	// and every other use refuses (roadmap L11.1, ADR 0185).
	taggedVars map[string]bool

	// loopElemTag is the unrolled-loop companion of taggedVars: `for v in [1.5, "a", None]` emits
	// one body copy per element, and the body needs to know what *this* element's slot holds to
	// print it. The value is the canonical ValueTag; the entry exists only while a body copy is
	// being emitted (roadmap L11.1, ADR 0233).
	loopElemTag map[string]int32
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
	// paramSlot names the parameters whose body rebinds them: their slot (`%_name`),
	// not the incoming argument register, is what reads load (Gap R.3, ADR 0196).
	paramSlot map[string]bool
	// forCtrSeq numbers the private induction counters of `for ... in range(...)`
	// loops, which are no longer allowed to share the loop variable's slot (Gap R.3b).
	forCtrSeq int
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
	floatFmtUsed bool
	// numCtx is the BinOp currently being lowered into the float arms, so the slot read inside it can
	// name the operator and span in the TypeError it raises. Set and restored by floatBinOp.
	numCtx *BinOp
	// floatUnlowerable records that a float arm reached an operand it could not lift, which the caller
	// turns into a front-end refusal instead of an instruction with an empty operand (Gap R.88).
	floatUnlowerable string
	heapSeq          int
	handlerStack     []string
	// handledArms counts the `except` arms whose body is currently being lowered. Inside one,
	// the exception in flight has been *accepted by the program*, so a control transfer out of
	// the arm (`return`, `break`, `continue`) has to leave the pending-exception flag cleared
	// like the arm's normal exit does — otherwise the transfer escapes the clear and the next
	// user-function call reports the exception all over again (roadmap Gap R.21, compiled half).
	handledArms int
	// strAttrs records the instance attributes a class assigns a string to (`self.w = "hi"`),
	// keyed "Class.attr", so a read of one is known to be an @str_tab index and prints as text
	// instead of as the number (roadmap Gap R.42, ADR 0224).
	strAttrs map[string]bool
	// emitErr carries the first codegen refusal raised inside a body emitted outside GenerateIR's
	// statement walk -- a class method, whose emitter writes into the globals buffer and has no
	// error return. Dropping it built an incomplete function and let `llc` report the problem as
	// a toolchain rejection, blaming the compiler for a source error (ADR 0166, roadmap Gap R.41).
	emitErr error
	// deferred is the stack of `finally` bodies belonging to the `try` statements currently
	// being lowered, outermost first. A control transfer that leaves a `try` for good -- a
	// `return`, `break`, `continue`, a raise that its arms do not catch -- has to run them,
	// innermost first, before it goes (roadmap Gap R.23, ADR 0222). The straight-line path
	// does not consult this stack: `tryStmt` emits its own deferred body once, directly.
	deferred [][]Stmt
	// emittingDeferred is set while a deferred body is being lowered, so that a raise from
	// inside one still walks the remaining (outer) deferred bodies instead of jumping out
	// and skipping them.
	emittingDeferred bool
	funcRaiseExit    string

	classInfos map[string]*classInfo // class name -> info
	classIDs   map[string]int        // class name -> runtime dispatch id
	classOrder []string              // classes in id order (dispatch switch)
	varClasses map[string]string     // local var -> class name
	selfClass  string                // enclosing class of current self
	attrSlots  map[string]int        // attr name -> instance data slot
	// classPat is the front end's answer to what a class pattern needs to know — which class a
	// pattern name denotes (through `Alias = Point`), and which attribute names the program can
	// ever write. It is computed before emission so the presence-row clear length is final (ADR 0235).
	classPat *classPatternInfo
	nextSlot int

	// genFuncs records generator function names; calling one yields a runtime
	// heap list handle (mirroring the interpreter's eager yield semantics).
	genFuncs map[string]bool
	// staticLists maps a compile-time list global name (@.lstN) to its literal,
	// so a call site can copy it into the runtime heap when the callee expects a
	// container handle.
	staticLists map[string]*ListLit
	// staticSets and staticDicts are the same record for the two container kinds a
	// comprehension can fold to: `sa = {x for x in [3, 1, 2]}` emits @.setN, whose layout is
	// a length plus an array and therefore not a value, so the binding site materialises the
	// literal these maps hold instead of storing the global (roadmap Gap J.2, ADR 0234).
	staticSets  map[string]*SetLit
	staticDicts map[string]*DictLit
	// containerLits records the container literal a name is bound to exactly once at module
	// level and never mutated afterwards. It is what licenses reading a slot as a container: the
	// builder wrote a tag for that slot from that literal, and as long as nothing has changed the
	// object, the tag the compiler remembers is the tag the object holds. A second binding, an item
	// assignment or a mutating method takes the name out of this map, and the read is refused
	// rather than trusted (roadmap L11.1, ADR 0241).
	containerLits map[string]Expr
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
	// moduleConsts holds the *program's own* module-level names whose value is a literal and which
	// the module never rebinds. A compiled function body may read those as values, because a value
	// that cannot change needs no slot to read it from (ADR 0227, roadmap Gap R.35's compiled half).
	moduleConsts map[string]Expr
	// unwritten is the checker's answer to "which names may this body read before assigning them",
	// keyed by the FuncDef whose body the read sits in (nil key: the module's own statements). It is
	// the same walk the checker runs and already warns about, asked a second question (ADR 0228) --
	// codegen does not get its own copy of the rule.
	unwritten map[*FuncDef]map[string]bool
	// fdAlias points an emitted clone (a decorator's `_impl`) at the FuncDef the checker saw, so the
	// clone inherits its unwritten-read set.
	fdAlias map[*FuncDef]*FuncDef
	// boundFlags is the entry from `unwritten` for the body being emitted, after eligibility filtering;
	// boundSlots names the i8 flag alloca for each of those names, and boundOrder lists them in a
	// deterministic order so the entry block does not depend on map iteration. A name outside this set pays nothing:
	// the flag exists only where the checker could not prove the write happens.
	boundFlags map[string]bool
	boundSlots map[string]string
	boundOrder []string
	// decoratorNames holds the names used as decorators anywhere in the program (`@add1` -> add1).
	// While emitting such a function, a nested closure body that cannot be lowered is reported in the
	// module comment instead of refusing: the decorated call goes through the trampoline, whose own
	// body *is* compiled with full error propagation, so the closure object here is unreachable and a
	// refusal would break a program that works (ADR 0227). The failure is never hidden -- it is said
	// in the module, where --emit-llvm and the IR verifier both can see it.
	decoratorNames map[string]bool
	// emittingDecorator is set while funcDef emits one of those functions.
	emittingDecorator bool
	// closureBodyNotes collects the deferred body failures named in the module comment.
	closureBodyNotes []string
	// funcLocals is the set of names the function body currently being emitted binds anywhere in
	// itself. It is the language's rule, not a slot-timing question: a binding inside a body makes
	// the name local to that body even when the module binds it too (ADR 0220), so `K = 1; return K`
	// in a function answers 1 while the module's K stays 5.
	funcLocals map[string]bool
	// moduleSlots holds the module names a compiled body reads that the module also *rebinds*: they
	// cannot be folded, so they live in module globals (`@gy_mod_LATE`) that main writes and any body
	// loads. A frame's allocas die with the frame; module state does not, which is exactly what a call
	// that looks a name up "when it runs" requires (ADR 0220's rule, implemented for the compiled
	// backend by ADR 0227). Containers stay refused -- the element ops are the missing machinery.
	moduleSlots map[string]string
	// moduleSlotDecls is the IR declaring those globals.
	moduleSlotDecls string
	// moduleNames is every name the module binds, constant or not. A read of one that is not a
	// constant is refused with the reason that names the missing machinery, instead of the typo
	// message that claims the interpreter reports the same error -- it does not (Gap R.38).
	moduleNames map[string]bool
	// curModGlobals holds folded module-global constants for the module
	// function currently being emitted; bare Name refs resolve against it.
	curModGlobals map[string]Expr
	// curModParams holds the param set of the module function being emitted,
	// so a param that shadows a module global is not substituted.
	curModParams map[string]bool
	// dbgOn asks for DWARF line records; dbgMarks and dbgFuncs are where each statement's
	// and each function definition's IR was written, which the attach pass turns into !dbg
	// metadata (L8.5, ADR 0231). Offsets are builder-relative: a mark says whether the write
	// went to `globals` or to the body builder.
	dbgOn    bool
	dbgMarks []dbgMark
	dbgFuncs []dbgFunc
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
		// A method symbol is minted once, stored in the class index, and reused by every
		// call through resolveMethod — so the prefix applied here reaches definition and
		// call sites together.
		funcName := irSymbol(fmt.Sprintf("%s_%s", cd.Name, mname))
		ci.methods[mname] = funcName
		g.markStringAttrs(cd.Name, fd)
		if methodReturnsStr(fd) {
			// Registered under the mangled symbol, which is what a call site resolves to
			// (Gap R.42, ADR 0224).
			g.strFuncs[funcName] = true
		}
		g.collectAttrs(fd.Body)
		g.emitClassMethod(cd.Name, funcName, fd)
	}
}

// copyInReboundParams gives every parameter the body rebinds a stack slot of its own
// at the top of the entry block and makes that slot authoritative, so a read after the
// assignment sees the store. Without it the assignment allocates a slot nothing reads
// and the read keeps returning the incoming register: `def bump(n): n = n + 1;
// return n` answers the argument, and an accumulator loop never terminates (Gap R.3,
// ADR 0196).
//
// floatRet is the float-returning case, whose prologue already copies every parameter
// into a double slot: those parameters behave like locals today, and giving them a
// second (i32) slot would be both wrong and a duplicate name. A parameter rebound to a
// float outside that case is excluded by reboundParams, which asks the same
// (*irGen).isFloat question the store path will ask, so no i32 slot is ever asked to
// hold a double.
func (g *irGen) copyInReboundParams(b *strings.Builder, fd *FuncDef, floatRet bool) {
	if fd == nil || floatRet || len(fd.Params) == 0 {
		return
	}
	rb := reboundParams(fd, g.isFloat)
	if len(rb) == 0 {
		return
	}
	if g.paramSlot == nil {
		g.paramSlot = map[string]bool{}
	}
	for _, p := range fd.Params {
		if !rb[p.Name] {
			continue
		}
		reg, ok := g.params[p.Name]
		if !ok {
			continue
		}
		if g.allocd[p.Name] || g.paramSlot[p.Name] {
			// A slot already exists for this name (the float prologue, or a container
			// path that allocated it first): it is the authoritative one.
			g.allocd[p.Name] = true
			g.paramSlot[p.Name] = true
			continue
		}
		fmt.Fprintf(b, "  %%%s = alloca i32\n", "_"+p.Name)
		fmt.Fprintf(b, "  store i32 %s, i32* %%%s\n", reg, "_"+p.Name)
		// The slot is rooted like any other local: after `n = make()` it holds a heap
		// handle, and an unrooted handle is one collection away from a segfault
		// (ADR 0181). Rooting a raw int costs the collector one skip.
		g.gcReg(b, p.Name)
		g.allocd[p.Name] = true
		g.paramSlot[p.Name] = true
	}
}

// emitClassMethod emits a class method as an LLVM function with self as param 0.
func (g *irGen) emitClassMethod(className, funcName string, fd *FuncDef) {
	prevParams := g.params
	prevSlot := g.paramSlot
	prevSelf := g.selfClass
	paramRegs := []string{"i32 %self"}
	for i := range fd.Params {
		paramRegs = append(paramRegs, fmt.Sprintf("i32 %%p%d", i+1))
	}
	g.params = map[string]string{"self": "%self"}
	// A method body is a body like any other: what it binds stays local to it (ADR 0227).
	doneMethodBody := g.enterBody(fd.Body)
	defer doneMethodBody()
	for i := 1; i < len(fd.Params); i++ {
		g.params[fd.Params[i].Name] = fmt.Sprintf("%%p%d", i)
	}
	g.selfClass = className
	// A method is a call like any other, so it needs what `funcDef` gives a `def`: its own
	// raise-exit block, no handler inherited from whatever was being emitted when the class
	// happened to be registered, and no deferred bodies belonging to an enclosing `try`. The
	// missing raise-exit was the visible half of roadmap Gap R.41 (ADR 0223): a `try` or a
	// `raise` in a method body branched to `br label %` with an empty target and `llc`
	// rejected the module, so a program whose answer was `m fin\n3` exited 2 with a temp path.
	prevRaise := g.funcRaiseExit
	g.funcRaiseExit = funcName + ".raiseexit"
	prevHandlers := g.handlerStack
	g.handlerStack = nil
	prevHandled := g.handledArms
	g.handledArms = 0
	prevDeferred := g.deferred
	g.deferred = nil
	prevFnSrc := g.curFnSrc
	g.curFnSrc = className + "." + fd.Name
	// A method is program source, so it gets a DISubprogram of its own; the pass only ever puts
	// locations inside a function that registered one, which is what keeps the compiler's own
	// runtime blocks out of the line table (ADR 0231).
	g.dbgDefine(&g.globals, funcName, className+"."+fd.Name, fd.Src)
	g.globals.WriteString(fmt.Sprintf("define i32 @%s(%s) {\n", funcName, strings.Join(paramRegs, ", ")))
	// The written-flags for this body's possibly-unwritten locals, immediately inside the brace
	// (ADR 0228).
	g.emitBoundAllocas(&g.globals)
	// A method is a call like any other: it opens its own root frame and pops it on
	// the way out. Without this the roots its body pushes (self, container args,
	// locals) piled up on the root stack one frame per call, so a loop that made
	// thousands of instances ran the stack out (ADR 0181).
	savedFrame := g.frameOpen
	g.gcOpenFrame(&g.globals)
	// A method that assigns to one of its parameters gets a slot for it, like any
	// other rebinding body (Gap R.3).
	g.copyInReboundParams(&g.globals, fd, false)
	g.inFunc = true
	// A method's locals are frame slots like any other body's, so they carry the same flags (ADR 0228).
	doneMethodFlags := g.enterBoundFlags(fd, fd.Body)
	defer doneMethodFlags()
	for _, st := range fd.Body {
		// A refusal inside a method body used to be dropped on the floor, which turned a source
		// error the front end should name into an incomplete function definition and a toolchain
		// rejection -- the ADR 0166 class, and the reason a method could not contain a construct
		// a plain function could (roadmap Gap R.41, ADR 0223).
		if err := g.stmt(&g.globals, st); err != nil {
			if g.emitErr == nil {
				g.emitErr = err
			}
			break
		}
	}
	g.gcCloseFrame(&g.globals)
	g.globals.WriteString("  ret i32 0\n")
	// The method's own unwind target: it closes the frame it opened and returns; the caller's
	// call-site check (ADR 0218) is what turns the flag into a propagation.
	fmt.Fprintf(&g.globals, "%s:\n", g.funcRaiseExit)
	g.gcCloseFrame(&g.globals)
	g.globals.WriteString("  ret i32 0\n}\n")
	g.inFunc = false
	g.frameOpen = savedFrame
	g.curFnSrc = prevFnSrc
	g.deferred = prevDeferred
	g.handledArms = prevHandled
	g.handlerStack = prevHandlers
	g.funcRaiseExit = prevRaise
	g.selfClass = prevSelf
	g.params = prevParams
	g.paramSlot = prevSlot
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

// exprIsString answers whether an expression's value is text -- an index into @str_tab. The
// element-kind tests ask a runtime question (`heapElemKind` reads the emitted shape), and an
// element written with the comprehension's own variable -- `out = [n for n in names if n == "a"]`
// -- has no shape to read, which left `print(out[0])` printing the index as a number. This is the
// static half of the same question (roadmap Gap R.42, ADR 0224).
func (g *irGen) exprIsString(e Expr) bool {
	switch v := e.(type) {
	case *StrLit, *FString:
		return true
	case *Name:
		return g.internedVars[v.Value]
	case *Call:
		if g.callReturnsStr(v) {
			return true
		}
		// A string method on a string receiver returns a string: `get()[1].upper()` is text, and
		// the print path and the operation path must ask that question the same way or one of them
		// renders the interned index as a number (ADR 0229).
		if at, ok := v.Fn.(*Attr); ok && len(v.Args) == 0 && g.exprIsString(at.Obj) {
			switch at.Name.Value {
			case "upper", "lower", "strip":
				return true
			}
		}
		// str(n) is text whatever n is (ADR 0229): the print path and the operation path ask
		// one predicate, or a number's digits come out as an index.
		if g.builtinCallAs(v, "str") {
			return true
		}
		return false
	case *Attr:
		if cls := g.receiverClass(v.Obj); cls != "" {
			return g.strAttrs[cls+"."+v.Name.Value]
		}
		return false
	case *BinOp:
		if v.Op != "+" {
			return false
		}
		_, lf := g.stringVal(v.L)
		_, rf := g.stringVal(v.R)
		return lf || rf || (g.exprIsString(v.L) && g.exprIsString(v.R))
	case *Slice:
		// s[a:b] of a string is a string whatever the bounds are, constant or not (ADR 0229).
		return g.exprIsString(v.Obj)
	case *Index:
		// A subscript of a string the compiler can name is text (ADR 0225), and so is an element
		// of a string container: that is what lets `xs = [s[1]]` remember that its elements are
		// strings rather than print the index that means them.
		// A subscript of a string the compiler *cannot* name is still a one-character string
		// (ADR 0229) — being a string does not require being a constant.
		if g.exprIsString(v.Obj) {
			return true
		}
		if _, isStr := g.stringVal(v.Obj); isStr {
			return true
		}
		if nm, ok := v.Obj.(*Name); ok {
			return g.listElemStr[nm.Value] || g.setElemStr[nm.Value] || g.dictValStr[nm.Value]
		}
		return false
	case *Comp:
		if len(v.Elems) != 1 {
			return false
		}
		// The element names the iteration variable, so its kind is the iterated collection's.
		if it, ok := v.Iter.(*Name); ok && (g.listElemStr[it.Value] || g.setElemStr[it.Value]) {
			if iv, ok2 := v.Elems[0].(*Name); ok2 && iv.Value == v.ForVar.Value {
				return true
			}
		}
		return g.exprIsString(v.Elems[0])
	}
	return false
}

// callReturnsStr answers whether the value a call produces is a string, in the language's
// representation -- an index into @str_tab. Module functions were answered by `strReturningFuncs`;
// methods had no such registration, so `print(Dog().sound())` printed the index as a number while
// the interpreter and CPython printed the text (roadmap Gap R.42, ADR 0224).
func (g *irGen) callReturnsStr(c *Call) bool {
	if nm, ok := c.Fn.(*Name); ok {
		return g.strFuncs[nm.Value]
	}
	if at, ok := c.Fn.(*Attr); ok {
		if cls := g.receiverClass(at.Obj); cls != "" {
			if sym, ok2 := g.resolveMethod(cls, at.Name.Value); ok2 {
				return g.strFuncs[sym]
			}
		}
	}
	return false
}

// methodReturnsStr is `strReturningFuncs` for a method body: the annotation says it, or some
// `return` in the body hands back a string literal or a string parameter. The emitter cannot ask
// the value path, because at that point the question is how to name the return type at every call
// site.
func (g *irGen) markStringAttrs(className string, fd *FuncDef) {
	for _, st := range fd.Body {
		g.scanStringAttrs(className, st)
	}
}

// scanStringAttrs finds `self.<attr> = <string>` anywhere a method body can reach, including
// inside an if/while/try, because an attribute initialised on one path is still a string-valued
// attribute of the class.
func (g *irGen) scanStringAttrs(className string, st Stmt) {
	switch n := st.(type) {
	case *AssignStmt:
		if at, ok := n.Target.(*Attr); ok {
			recv := g.receiverClass(at.Obj)
			if recv != "" && recv != className {
				recv = className // `self` inside a method of this class
			}
			if recv == "" {
				recv = className
			}
			if _, foldable := g.stringVal(n.Value); foldable {
				g.strAttrs[recv+"."+at.Name.Value] = true
			} else if c, ok := n.Value.(*Call); ok && g.callReturnsStr(c) {
				g.strAttrs[recv+"."+at.Name.Value] = true
			}
		}
	case *IfStmt:
		for _, s := range n.Then {
			g.scanStringAttrs(className, s)
		}
		for _, e := range n.Elifs {
			for _, s := range e.Then {
				g.scanStringAttrs(className, s)
			}
		}
		for _, s := range n.Else {
			g.scanStringAttrs(className, s)
		}
	case *WhileStmt:
		for _, s := range n.Body {
			g.scanStringAttrs(className, s)
		}
	case *ForStmt:
		for _, s := range n.Body {
			g.scanStringAttrs(className, s)
		}
	case *WithStmt:
		for _, s := range n.Body {
			g.scanStringAttrs(className, s)
		}
	case *TryStmt:
		for _, s := range n.Body {
			g.scanStringAttrs(className, s)
		}
		for _, ec := range n.Excepts {
			for _, s := range ec.Body {
				g.scanStringAttrs(className, s)
			}
		}
		for _, s := range n.Finally {
			g.scanStringAttrs(className, s)
		}
	case *MatchStmt:
		for _, c := range n.Cases {
			for _, s := range c.Body {
				g.scanStringAttrs(className, s)
			}
		}
	}
}

func methodReturnsStr(fd *FuncDef) bool {
	if fd.ReturnAnno != nil && fd.ReturnAnno.Kind == KindString {
		return true
	}
	strParams := map[string]bool{}
	for _, p := range fd.Params {
		if p.Annot != nil && p.Annot.Kind == KindString {
			strParams[p.Name] = true
		}
	}
	found := false
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			switch n := st.(type) {
			case *ReturnStmt:
				if n.Expr == nil {
					continue
				}
				switch e := n.Expr.(type) {
				case *StrLit, *FString:
					found = true
				case *Name:
					if strParams[e.Value] {
						found = true
					}
				}
			case *IfStmt:
				walk(n.Then)
				for _, el := range n.Elifs {
					walk(el.Then)
				}
				walk(n.Else)
			case *WhileStmt:
				walk(n.Body)
			case *ForStmt:
				walk(n.Body)
			case *WithStmt:
				walk(n.Body)
			case *MatchStmt:
				for _, c := range n.Cases {
					walk(c.Body)
				}
			case *TryStmt:
				walk(n.Body)
				for _, ec := range n.Excepts {
					walk(ec.Body)
				}
				walk(n.Finally)
			}
		}
	}
	walk(fd.Body)
	return found
}

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
	// `Dog().sound()` — the receiver is a fresh instance of a class the source names. Without
	// this the receiver's class is unknown, so the call fell to the dynamic switch and the
	// question "does this method return a string?" had nobody to ask, printing the @str_tab
	// index where the interpreter and CPython print the text (Gap R.42, ADR 0224).
	if c, ok := recv.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 {
			if _, isClass := g.classInfos[n.Value]; isClass {
				return n.Value
			}
		}
	}
	return ""
}

type loopInfo struct {
	breakLabel    string
	continueLabel string
}

// sortedRuntime lowers sorted(<container>[, reverse=...]) to a runtime copy-and-sort. The fold in
// the call lowering only covers constant int literals; this is the path for a variable and for
// anything that is not plain integers, and print(sorted(xs)) reaches it directly so the handle it
// yields is rendered by the runtime printer rather than printf'd (roadmap L11.7, ADR 0191).
func (g *irGen) sortedRuntime(b *strings.Builder, c *Call) (string, error) {
	// A literal argument is freshly allocated by containerOperand, so sorting it in place is
	// correct; a variable must be copied first, because sorted(xs) leaves xs in its own order.
	argIsFresh := true
	if _, isName := c.Args[0].(*Name); isName {
		argIsFresh = false
	}
	sv, serr := g.containerOperand(b, c.Args[0])
	if serr != nil {
		return "", serr
	}
	// Which comparator: numbers by payload, interned strings by their text. A list holding both
	// is Python's TypeError, and ordering strings by payload would sort them by arrival order.
	mode := 0
	switch a := c.Args[0].(type) {
	case *ListLit:
		sawInt, sawStr := false, false
		for _, el := range a.Elems {
			if _, isStr := el.(*StrLit); isStr {
				mode, sawStr = 1, true
			}
			if _, isInt := el.(*IntLit); isInt {
				sawInt = true
			}
		}
		if sawInt && sawStr {
			return "", fmt.Errorf("cannot sort a list whose elements are of more than one kind; Python raises TypeError here too, and the compiled backend reports it (roadmap L11.1, ADR 0191)")
		}
	case *Name:
		if g.listElemStr[a.Value] {
			mode = 1
		}
		if g.mixedLists[a.Value] {
			return "", fmt.Errorf("cannot sort a list whose elements are of more than one kind; Python raises TypeError here too, and the compiled backend reports it (roadmap L11.1, ADR 0191)")
		}
	}
	// Handle registers in this backend are named %h<N> off heapSeq; a %t<N> name reads as an
	// ordinary temp to the print and call lowerings, which then printf a handle as a value.
	g.heapSeq++
	slot := fmt.Sprintf("%%h%d", g.heapSeq)
	if argIsFresh {
		b.WriteString(fmt.Sprintf("  call void @rt_sort(i32 %s, i32 %d)\n", sv, mode))
		slot = sv
	} else {
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_copy(i32 %s)\n", slot, sv))
		b.WriteString(fmt.Sprintf("  call void @rt_sort(i32 %s, i32 %d)\n", slot, mode))
	}
	if len(c.Args) == 2 {
		revExpr := c.Args[1]
		if kw, ok := c.Args[1].(*KeywordArg); ok {
			revExpr = kw.Value
		}
		rv, rerr := g.constIntVal(revExpr)
		if rerr != nil {
			return "", fmt.Errorf("sorted(reverse=...) needs a constant flag (roadmap L11.7)")
		}
		if rv != 0 {
			b.WriteString(fmt.Sprintf("  call void @rt_reverse(i32 %s)\n", slot))
		}
	}
	return slot, nil
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
	g.markBound(b, name) // a handle store binds the name too (ADR 0228)
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
// stringConst folds an expression to a string constant. `shadowed` reports whether the
// program defines a function of a given name; when it does, the call is the program's and
// no fold through the built-in's meaning is allowed (`str`/`chr` here) — that fold is how
// `def str(x): return x + 7` printed a string the program never returned (Gap R.6, ADR 0199).
// Callers without program context pass nil, which folds only what cannot be a call.
func stringConst(e Expr, shadowed func(string) bool) (string, bool) {
	if sl, ok := e.(*StrLit); ok {
		return sl.Value, true
	}
	if b, ok := e.(*BinOp); ok && b.Op == "+" {
		ls, lok := stringConst(b.L, shadowed)
		rs, rok := stringConst(b.R, shadowed)
		if lok && rok {
			return ls + rs, true
		}
	}
	if c, ok := e.(*Call); ok {
		attr, ok := c.Fn.(*Attr)
		if ok {
			v, ok := stringConst(attr.Obj, shadowed)
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
					oldv, ok := stringConst(c.Args[0], shadowed)
					if !ok {
						return "", false
					}
					newv, ok := stringConst(c.Args[1], shadowed)
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
						sv, ok := stringConst(el, shadowed)
						if !ok {
							return "", false
						}
						parts = append(parts, sv)
					}
					return strings.Join(parts, v), true
				}
			}
		}
		// str() over a literal folds to the text the program would print, so
		// len(str(42)) -> 2 and `s = str(None)` needs no store. None folds to "None"
		// because that is what str(None) is: the int 0 is a different value, and a
		// fold that says "0" both prints the wrong thing and hands back a string
		// constant the assignment path cannot elide (ADR 0183).
		if n, ok := c.Fn.(*Name); ok && n.Value == "str" && len(c.Args) == 1 && !nameShadowed(shadowed, n.Value) {
			switch arg := c.Args[0].(type) {
			case *IntLit:
				return strconv.FormatInt(arg.Value, 10), true
			case *NoneLit:
				return "None", true
			case *StrLit:
				return arg.Value, true
			}
			if fl, ok := c.Args[0].(*FloatLit); ok {
				return pyFloatRepr(fl.Value), true
			}
		}
		return "", false
	}
	return "", false
}

// stringConstLen is len() over a compile-time-known string constant.
func stringConstLen(e Expr, shadowed func(string) bool) (int, bool) {
	if s, ok := stringConst(e, shadowed); ok {
		// Code points, the unit every other string position question uses (ADR 0225). This fold
		// measured bytes, so `len("héllo")` answered 6 at compile time while the same program with
		// the text in a variable answered 5 -- one language, two units, chosen by where the text
		// happened to be written.
		return len([]rune(s)), true
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
	// Build the whole definition before touching the globals buffer. This emitter used to write the
	// opening `@.lstN = private global {i32, [N x i32]} { i32 N, [N x i32] [` and only then look at the
	// elements; when one of them was not an int it returned an error and left an unterminated global
	// definition in the module. Whatever caught that error -- the container-equality path, the print
	// path -- shipped the broken line, and the user got exit 2, `expected type`, for a program like
	// `print([1.5, 2])` whose answer CPython prints in one line (roadmap Gap R.40, ADR 0166).
	var def strings.Builder
	parts := make([]string, 0, n)
	for i, el := range ln.Elems {
		il, ok := el.(*IntLit)
		if !ok {
			if _, isFloat := el.(*FloatLit); isFloat {
				return "", fmt.Errorf("a compiled container cannot hold a float yet: the element slot is an i32 word and %s has no representation in one (the interpreter and CPython both answer this program; compiled floats in containers are roadmap L11.6)", exprTyName(el))
			}
			return "", fmt.Errorf("list literal elements must be integers, not %s", exprTyName(el))
		}
		_ = i
		parts = append(parts, fmt.Sprintf("i32 %d", il.Value))
	}
	def.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [%s] }\n", name, n, n, n, strings.Join(parts, ", ")))
	if g.staticLists == nil {
		g.staticLists = map[string]*ListLit{}
	}
	g.staticLists[name] = ln
	g.globals.WriteString(def.String())
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
		if name, ok := n.Fn.(*Name); ok && name.Value == "str" && len(n.Args) == 1 && !g.builtinShadowed(name.Value) {
			if il, ok := n.Args[0].(*IntLit); ok {
				return strconv.FormatInt(il.Value, 10), true
			}
			// str(None) is "None", not "0": the fold has to agree with the interpreter's
			// str()/print(), or the same program prints two different things depending on
			// which backend ran it (ADR 0183).
			if _, ok := n.Args[0].(*NoneLit); ok {
				return "None", true
			}
			if sl, ok := n.Args[0].(*StrLit); ok {
				return sl.Value, true
			}
			if fv, ok := g.floatEval(n.Args[0]); ok {
				return pyFloatRepr(fv), true
			}
		}
		if name, ok := n.Fn.(*Name); ok && name.Value == "chr" && !g.builtinShadowed(name.Value) {
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
			// A negated slot read whose kind the object carries is answered by the float arms, so its
			// result *is* a float and print has to choose the float formatter for it (Gap R.88).
			return g.isFloat(n.X) || g.taggedNegationApplies(n)
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
		// The numeric door answers an arithmetic use of a slot whose kind the object carries through
		// the float arms — unboxing a float slot rather than reading its handle as a count — so the
		// result of such an expression *is* a float, and print has to pick the float formatter for it
		// (roadmap L11.1, Gap R.88). Comparisons are not in this: they answer 0/1 whatever lifts.
		switch n.Op {
		case "+", "-", "*", "%", "//", "**":
			if g.taggedNumberUseApplies(n) {
				return true
			}
		}
		switch n.Op {
		case "+", "-", "*", "%", "//":
			return g.isFloat(n.L) || g.isFloat(n.R)
		}
		return false
	case *Index:
		// A slot whose literal is a float is a float *use*, even though the container holding it is
		// not a float (the container arm below): `xs = [1.5, "a"]; print(xs[0] * 2)` must multiply a
		// double and print `3.0`, and the only reason the compiler knows the slot holds 1.5 is the
		// same promise that lets the numeric read fold (roadmap L11.1, ADR 0243). Restricted to the
		// tagged shapes — a mixed list, or a slot reached through another slot — so every container
		// that already answered this question keeps the answer it has.
		if el, ok := g.staticNumericElem(n); ok {
			switch base := n.Obj.(type) {
			case *Name:
				if g.mixedLists[base.Value] {
					return g.isFloat(el)
				}
			case *Index:
				return g.isFloat(el)
			}
		}
		return false
	case *Name:
		if g.floatVars != nil {
			return g.floatVars[n.Value]
		}
		return false
	case *ListLit, *DictLit, *SetLit:
		// A container is not a float, whatever its elements are — `[1.5, "a"] == [1.5, "a"]` is a
		// structural comparison of two containers, not an fcmp of two doubles. This arm used to
		// answer "yes, it is a float" when any element was, which was unreachable while a float
		// element was refused outright; once the element could be stored the answer came out as
		// `sitofp i32 <handle> to double`, comparing boxes rather than contents (roadmap L11.1,
		// ADR 0233). A program that wants a number out of a container asks for the element.
		return false
	case *Call:
		if n.Fn != nil {
			if id, ok := n.Fn.(*Name); ok {
				// A function the program defined and that returns a float is a fact about
				// the program, and it outranks everything below.
				if g.floatFuncs[id.Value] {
					return true
				}
				// A program that defines `float`, `abs`, `sum` ... owns that name: nothing
				// below may read the call through the built-in's shape (Gap R.6).
				if g.builtinShadowed(id.Value) {
					return false
				}
				if id.Value == "float" {
					return true
				}
				if id.Value == "abs" || id.Value == "min" || id.Value == "max" {
					// min/max return the element they choose, so the answer is a float when the
					// *winner* is; the others ask whether any operand is (ADR 0221).
					if (id.Value == "min" || id.Value == "max") && len(n.Args) == 1 {
						if lst, isLit := n.Args[0].(*ListLit); isLit {
							if isF, known := g.minMaxReturnsFloat(lst.Elems, id.Value == "min"); known {
								return isF
							}
						}
					}
					for _, a := range n.Args {
						if g.isFloatNumericOperand(a) {
							return true
						}
					}
				}
				if id.Value == "sqrt" || id.Value == "floor" || id.Value == "ceil" {
					return true
				}
				if id.Value == "sum" {
					for _, a := range n.Args {
						if g.isFloatNumericOperand(a) {
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

// numericFoldElems answers a min/max/sum argument's elements as numbers the compiler can hold,
// and says which of them are floats. It is the question min/max must ask twice over: Python returns
// the *element*, so the answer's type is the winner's own type — max([1, 2.5]) is the float 2.5 and
// min([2.5, 1]) is the integer 1, printed 2.5 and 1. Comparing them in the i32 domain truncated the
// float first, which answered max([1, 2.5]) as 2 (roadmap L11.1). ok is false when some element is
// computed at runtime, because then no static winner exists.
func (g *irGen) numericFoldElems(elems []Expr) (vals []float64, isFloatElem []bool, ok bool) {
	vals = make([]float64, 0, len(elems))
	isFloatElem = make([]bool, 0, len(elems))
	for _, el := range elems {
		if fv, isF := g.floatEval(el); isF {
			vals = append(vals, fv)
			isFloatElem = append(isFloatElem, true)
			continue
		}
		if il, isI := el.(*IntLit); isI {
			vals = append(vals, float64(il.Value))
			isFloatElem = append(isFloatElem, false)
			continue
		}
		return nil, nil, false
	}
	return vals, isFloatElem, true
}

// minMaxReturnsFloat answers whether min/max of these elements denotes a float — which is a question
// about the *winning* element, not about whether any element is a float. When the winner is an
// integer, printing the call must print an integer: CPython's min([2.5, 1]) is 1, not 1.0.
func (g *irGen) minMaxReturnsFloat(elems []Expr, wantMin bool) (bool, bool) {
	vals, floats, ok := g.numericFoldElems(elems)
	if !ok || len(vals) == 0 {
		return false, false
	}
	best := 0
	for i := 1; i < len(vals); i++ {
		if wantMin && vals[i] < vals[best] {
			best = i
		}
		if !wantMin && vals[i] > vals[best] {
			best = i
		}
	}
	return floats[best], true
}

// isFloatNumericOperand is the question the numeric folds ask, which isFloat no longer answers
// for a container: what type should the accumulator have. sum([1.5, 2.5]) and max([1.5, 2.5])
// fold a literal container into one number, and that number is a double when any element is.
// isFloat itself says a container is not a float — `[1.5] == [1.5]` is a structural comparison of
// two containers, and reading it as an fcmp of two truncated handles was how print([1.5]) came to
// answer [1] — so the folds ask the element question here, out loud, rather than having a list
// claim to be a number (roadmap L11.1, ADR 0233).
func (g *irGen) isFloatNumericOperand(e Expr) bool {
	if g.isFloat(e) {
		return true
	}
	switch n := e.(type) {
	case *ListLit:
		for _, el := range n.Elems {
			if g.isFloat(el) {
				return true
			}
		}
	case *SetLit:
		for _, el := range n.Elems {
			if g.isFloat(el) {
				return true
			}
		}
	}
	return false
}

// valueText returns the i32 operand text for e, discarding any codegen error.
func (g *irGen) valueText(b *strings.Builder, e Expr) string {
	v, err := g.value(b, e)
	if err != nil {
		// This line used to discard the error and hand back "". The float path then emitted
		//     %t1 = sitofp i32  to double
		// into the module -- an instruction with no operand -- which llc rejected, so an ordinary
		// program like `print([1.5, 2])` came back as exit 2, a toolchain rejection blaming the
		// compiler for a program whose answer CPython prints in one line (roadmap Gap R.40,
		// ADR 0166). The failure is recorded instead; the module is refused at assembly. "0" keeps
		// the half-written instruction parseable purely so that the refusal, not the garbage, is
		// what a user reads -- nothing that is about to be refused may also be executed.
		g.noteUnlowered(e, err)
		return "0"
	}
	return v
}

// noteUnlowered records the first expression a helper could not lower. It shares the field the
// method emitter uses (ADR 0223) because the rule is the same one: an emission path with no error
// channel of its own reports into the generator, and GenerateIR refuses rather than shipping IR it
// knows to be wrong.
// moduleEnvFor decides, once per program, what a compiled function body may know about the module
// it sits in. A name bound exactly once at module level to a literal, and never rebound by any
// module-level statement, is a value: inlining it is sound and needs no runtime slot. Everything
// else the module binds stays refused -- with a message that says so -- because main's slots are
// stack allocas a callee cannot lawfully read. Assignments inside a function body are not module
// writes (a binding inside a body is local, ADR 0220), so they neither disqualify a constant nor
// make one mutable.
func moduleEnvFor(prog *Program) (map[string]Expr, map[string]bool) {
	consts := map[string]Expr{}
	all := map[string]bool{}
	if prog == nil {
		return consts, all
	}
	literal := func(e Expr) bool {
		switch e.(type) {
		case *IntLit, *StrLit, *BoolLit, *NoneLit:
			return true
		}
		return false
	}
	disqualify := func(names map[string]bool) {
		for n := range names {
			delete(consts, n)
			all[n] = true
		}
	}
	for _, st := range prog.Stmts {
		switch n := st.(type) {
		case *FuncDef, *ClassDef:
			continue // a body's bindings are local to it
		case *AssignStmt:
			targets := map[string]bool{}
			moduleBindingNames(n, targets)
			if nm, ok := n.Target.(*Name); ok && len(targets) == 1 && literal(n.Value) && !all[nm.Value] {
				consts[nm.Value] = n.Value
				all[nm.Value] = true
				continue
			}
			disqualify(targets)
		default:
			targets := map[string]bool{}
			moduleBindingNames(st, targets)
			disqualify(targets)
		}
	}
	return consts, all
}

// moduleSlotNames chooses which module-level names need real storage: those that some function,
// method or closure body reads, that the module rebinds (a name bound once to a literal is a value and
// needs no slot at all), and whose module-level assignments are all scalar. A container is left out on
// purpose: reading a module list as a handle without the container operations behind it would trade an
// honest refusal for a half-working answer (Gap R.35's remaining half, ADR 0227).
func moduleSlotNames(prog *Program, consts map[string]Expr, all map[string]bool) map[string]string {
	slots := map[string]string{}
	if prog == nil {
		return slots
	}
	// What scalar shape does each module name hold?
	scalar := map[string]bool{}
	seenBefore := map[string]bool{}
	for _, st := range prog.Stmts {
		as, ok := st.(*AssignStmt)
		if !ok {
			continue
		}
		nm, ok := as.Target.(*Name)
		if !ok {
			continue
		}
		// Every assignment to the name must hold a scalar: an absent map entry reads false, so the
		// first assignment has to initialise it rather than AND against nothing.
		if !seenBefore[nm.Value] {
			scalar[nm.Value] = scalarValue(as.Value)
			seenBefore[nm.Value] = true
		} else {
			scalar[nm.Value] = scalar[nm.Value] && scalarValue(as.Value)
		}
	}
	// Which names do bodies read?
	reads := map[string]bool{}
	var walk func(node interface{})
	var mark func(e Expr)
	mark = func(e Expr) {
		switch n := e.(type) {
		case *Name:
			if all[n.Value] {
				reads[n.Value] = true
			}
		case *BinOp:
			mark(n.L)
			mark(n.R)
		case *UnOp:
			mark(n.X)
		case *Index:
			mark(n.Obj)
			mark(n.Idx)
		case *Call:
			mark(n.Fn)
			for _, a := range n.Args {
				mark(a)
			}
		case *Attr:
			mark(n.Obj)
		case *Tuple:
			for _, el := range n.Elems {
				mark(el)
			}
		}
	}
	walk = func(node interface{}) {
		switch n := node.(type) {
		case *FuncDef:
			for _, st := range n.Body {
				walk(st)
			}
			for _, pa := range n.Params {
				if pa.Default != nil {
					mark(pa.Default)
				}
			}
		case *ClassDef:
			for _, st := range n.Body {
				walk(st)
			}
		case []Stmt:
			for _, st := range n {
				walk(st)
			}
		case *AssignStmt:
			mark(n.Value)
		case *AugAssignStmt:
			mark(n.Value)
			mark(n.Target)
		case *ExprStmt:
			mark(n.Expr)
		case *ReturnStmt:
			mark(n.Expr)
		case *IfStmt:
			mark(n.Cond)
			walk(n.Then)
			walk(n.Else)
		case *WhileStmt:
			mark(n.Cond)
			walk(n.Body)
			walk(n.Else)
		case *ForStmt:
			mark(n.Iter)
			walk(n.Body)
			walk(n.Else)
		case *TryStmt:
			walk(n.Body)
			for _, arm := range n.Excepts {
				walk(arm.Body)
			}
			walk(n.Finally)
		case *WithStmt:
			mark(n.Expr)
			walk(n.Body)
		case *MatchStmt:
			mark(n.Subject)
			for _, c := range n.Cases {
				walk(c.Body)
			}
		}
	}
	for _, st := range prog.Stmts {
		switch st.(type) {
		case *FuncDef, *ClassDef:
			walk(st)
		}
	}
	for nm := range reads {
		if _, isConst := consts[nm]; isConst {
			continue // a value needs no slot
		}
		if !scalar[nm] {
			continue
		}
		slots[nm] = irSymbol("gy_mod_" + nm)
	}
	return slots
}

func scalarValue(e Expr) bool {
	switch n := e.(type) {
	case *IntLit, *StrLit, *BoolLit, *NoneLit:
		return true
	case *BinOp:
		return scalarValue(n.L) && scalarValue(n.R)
	case *UnOp:
		return scalarValue(n.X)
	}
	return false
}

// moduleBindingNames records the names a module-level statement *binds* -- the left-hand sides, the
// loop variable, the `with ... as` name, and whatever a nested if/while/try body binds. It is a
// binding collector, not a use collector: an expression that merely reads a name says nothing about
// whether that name can change.
func moduleBindingNames(st Stmt, out map[string]bool) {
	if st == nil {
		return
	}
	var bind func(e Expr)
	bind = func(e Expr) {
		switch t := e.(type) {
		case *Name:
			out[t.Value] = true
		case *Tuple:
			for _, el := range t.Elems {
				bind(el)
			}
		case *ListLit:
			for _, el := range t.Elems {
				bind(el)
			}
		}
	}
	switch n := st.(type) {
	case *AssignStmt:
		bind(n.Target)
	case *AugAssignStmt:
		bind(n.Target)
	case *ForStmt:
		bind(n.Var)
		for _, s := range n.Body {
			moduleBindingNames(s, out)
		}
		for _, s := range n.Else {
			moduleBindingNames(s, out)
		}
	case *WhileStmt:
		for _, s := range n.Body {
			moduleBindingNames(s, out)
		}
		for _, s := range n.Else {
			moduleBindingNames(s, out)
		}
	case *IfStmt:
		for _, s := range n.Then {
			moduleBindingNames(s, out)
		}
		for _, s := range n.Else {
			moduleBindingNames(s, out)
		}
	case *TryStmt:
		for _, s := range n.Body {
			moduleBindingNames(s, out)
		}
		for _, arm := range n.Excepts {
			for _, s := range arm.Body {
				moduleBindingNames(s, out)
			}
		}
		for _, s := range n.Finally {
			moduleBindingNames(s, out)
		}
	case *WithStmt:
		if n.As != nil {
			out[n.As.Value] = true
		}
		for _, s := range n.Body {
			moduleBindingNames(s, out)
		}
	case *MatchStmt:
		for _, c := range n.Cases {
			for _, s := range c.Body {
				moduleBindingNames(s, out)
			}
		}
	}
}

// moduleStateErr is the honest answer when a body refers to module-level *state* -- a container, or
// anything the module rebinds -- which a compiled body cannot reach. The two sites that used to answer
// "non-constant string" (for a plain list's .append) and "non-string variable" (for len of a list) are
// the ADR-0166 face of that gap: a claim that misdescribes the value sends the reader to a line they
// never wrote, which the record treats as worse than no claim at all (roadmap Gap R.38).
func (g *irGen) moduleStateErr(nm string) error {
	if !g.inFunc || nm == "" {
		return nil
	}
	if g.allocd[nm] || (g.funcLocals != nil && g.funcLocals[nm]) {
		return nil // the body binds it: this is a local, and its own rules apply
	}
	if g.moduleNames[nm] {
		return fmt.Errorf("codegen: %q is bound at module level, and a compiled function body cannot reach module-level containers or state that changes (the interpreter answers this program; compiled module globals are roadmap Gap R.35)", nm)
	}
	return nil
}

// enterBody records what a function, method or closure body binds anywhere inside itself, so the
// module-read path can honour the scoping rule rather than the order slots happen to be allocated in.
func (g *irGen) enterBody(body []Stmt) func() {
	saved := g.funcLocals
	g.funcLocals = map[string]bool{}
	collectLocals(body, g.funcLocals)
	return func() { g.funcLocals = saved }
}

func (g *irGen) noteUnlowered(where any, err error) {
	what := "an expression"
	switch n := where.(type) {
	case Expr:
		if t := exprTyName(n); t != "" {
			what = t
		}
	case Stmt:
		what = fmt.Sprintf("a %T statement", n)
	}
	if g.emitErr == nil {
		g.emitErr = fmt.Errorf("codegen: %s cannot be compiled: %v", what, err)
	}
}

// floatValue emits a double IR operand for a float-typed expression e.
func (g *irGen) floatValue(b *strings.Builder, e Expr) string {
	switch n := e.(type) {
	case *FloatLit:
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(n.Value))
		return t
	case *Index:
		// The numeric half of the slot read (roadmap L11.1, ADR 0243): a slot whose literal is a float
		// *is* that float for arithmetic. The word the slot holds is a float box handle, and lifting one
		// of those straight into an fadd is the wrong answer ADR 0233 exists to keep out — so the double
		// comes from the element the compiler can see instead.
		if el, ok := g.staticNumericElem(n); ok {
			return g.floatValue(b, el)
		}
		// The other half: the literal no longer describes the element, but the object remembers what it
		// wrote beside it. The pair comes out of the slot and the tag decides — unbox, convert, or raise
		// what CPython raises for this operator and that kind (roadmap L11.1, Gap R.88).
		if g.numCtx != nil {
			if fam, ok := g.taggedNumberOperands(g.numCtx); ok {
				if v, okUse, err := g.taggedFloatOperand(b, n, g.numCtx, fam); okUse && err == nil && v != "" {
					return v
				}
			}
		}
		return ""
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
			// A negated slot read whose kind only the object carries goes to the tag dispatch, which is
			// the only place that can unbox a float slot rather than read its handle as a number.
			if g.taggedNegationApplies(n) {
				if v, ok, err := g.taggedFloatNegate(b, n); err != nil {
					g.floatUnlowerable = err.Error()
					return ""
				} else if ok && v != "" {
					t := g.newTmp()
					fmt.Fprintf(b, "  %s = fsub double 0.0, %s\n", t, v)
					return t
				}
			}
			fx := g.floatValue(b, n.X)
			if fx == "" {
				// `fsub double 0.0, ` with an empty operand is the module llc rejects, which ADR 0166
				// counts as the compiler's bug — so the shape is recorded and the caller refuses it.
				g.floatUnlowerable = fmt.Sprintf("the operand %s of the negation", g.exprSummary(n.X))
				return ""
			}
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = fsub double 0.0, %s\n", t, fx)
			return t
		}
	case *BinOp:
		return g.floatBinOp(b, n)
	case *Call:
		if n.Fn != nil {
			if id, ok := n.Fn.(*Name); ok && g.builtinShadowed(id.Value) && !g.floatFuncs[id.Value] {
				// The name is the program's, so this is an ordinary call — and a call in this
				// backend returns i32 unless the program's own definition says otherwise. Lift
				// its result the way a plain int expression is lifted into float arithmetic
				// (what the switch's own default does), rather than folding it as the built-in
				// conversion, which is what produced an i32 operand in an fadd (Gap R.6).
				t := g.newTmp()
				fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", t, g.valueText(b, n))
				return t
			}
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
							// An integer element of a sum that also holds a float is still a
							// number. Refusing to lift it sent the whole call through the i32
							// path, where 2.5 became 2: print(sum([1, 2.5])) answered 3.0 against
							// CPython's 3.5 (roadmap L11.1).
							if il, isInt := elem.(*IntLit); isInt {
								fv, ok2 = float64(il.Value), true
							}
						}
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
							// The same lift as sum's: max([1, 2.5]) is 2.5, not 2.
							if il, isInt := elem.(*IntLit); isInt {
								fv, ok2 = float64(il.Value), true
							}
						}
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
					// The elements fold to numbers even when not every one of them is a float
					// literal; the winner decides, and an integer winner is the i32 path's to
					// answer (roadmap L11.1).
					if vals, floats, foldable := g.numericFoldElems(lst.Elems); foldable && len(vals) > 0 {
						best := 0
						for i := 1; i < len(vals); i++ {
							if (id.Value == "min" && vals[i] < vals[best]) || (id.Value == "max" && vals[i] > vals[best]) {
								best = i
							}
						}
						if !floats[best] {
							return ""
						}
						t := g.newTmp()
						fmt.Fprintf(b, "  %s = fadd double 0.0, %s\n", t, floatConst(vals[best]))
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
	// The numeric door (taggedFloatOperand) needs to know which operator and which source span the
	// raise it emits belongs to; floatValue is reached from here, so the context is set around the
	// call and restored after it, the way a nested expression's own operator belongs to itself.
	prev := g.numCtx
	g.numCtx = n
	defer func() { g.numCtx = prev }()
	l := g.floatValue(b, n.L)
	r := g.floatValue(b, n.R)
	if l == "" || r == "" {
		// An operand the lift cannot reach must never reach the instruction. `fdiv double , %t1` is
		// llc rejecting the compiler's own module, which ADR 0166 counts as our bug rather than the
		// program's, so the shape is recorded and the caller refuses it as a front-end diagnostic
		// (roadmap Gap R.88).
		missing, which := n.L, "left"
		if l != "" {
			missing, which = n.R, "right"
		}
		g.floatUnlowerable = fmt.Sprintf("the %s operand %s of %q", which, g.exprSummary(missing), n.Op)
		if l == "" {
			l = "0.0"
		}
		if r == "" {
			r = "0.0"
		}
	}
	t := g.newTmp()
	switch n.Op {
	case "+":
		fmt.Fprintf(b, "  %s = fadd double %s, %s\n", t, l, r)
	case "-":
		fmt.Fprintf(b, "  %s = fsub double %s, %s\n", t, l, r)
	case "*":
		fmt.Fprintf(b, "  %s = fmul double %s, %s\n", t, l, r)
	case "//":
		g.guardNonZeroFloat(b, r, n.Span(), g.floorDivMessage(n.L, n.R))
		fmt.Fprintf(b, "  %s = fdiv double %s, %s\n", t, l, r)
		q := g.newTmp()
		fmt.Fprintf(b, "  %s = call double @llvm.floor.f64(double %s)\n", q, t)
		return q
	case "/":
		g.guardNonZeroFloat(b, r, n.Span(), g.trueDivMessage(n.L, n.R))
		fmt.Fprintf(b, "  %s = fdiv double %s, %s\n", t, l, r)
	case "%":
		g.guardNonZeroFloat(b, r, n.Span(), "float modulo")
		// `frem` is libm's fmod — the truncated remainder — so `-7.0 % 2.0` would answer
		// -1. Same correction as the integer path: add the divisor back when the
		// remainder's sign differs from the divisor's (floorModFloat says it in Go).
		rm := g.newTmp()
		fmt.Fprintf(b, "  %s = frem double %s, %s\n", rm, l, r)
		// An exact remainder carries the divisor's sign (7.5 % -0.5 is -0.0); fmod gives it
		// the dividend's, which prints as a different number.
		zsig := g.newTmp()
		fmt.Fprintf(b, "  %s = call double @llvm.copysign.f64(double 0.0, double %s)\n", zsig, r)
		iszero := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp oeq double %s, 0.0\n", iszero, rm)
		base := g.newTmp()
		fmt.Fprintf(b, "  %s = select i1 %s, double %s, double %s\n", base, iszero, zsig, rm)
		rm = base
		nz := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp one double %s, 0.0\n", nz, rm)
		rneg := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp olt double %s, 0.0\n", rneg, rm)
		bneg := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp olt double %s, 0.0\n", bneg, r)
		diff := g.newTmp()
		fmt.Fprintf(b, "  %s = xor i1 %s, %s\n", diff, rneg, bneg)
		adj := g.newTmp()
		fmt.Fprintf(b, "  %s = and i1 %s, %s\n", adj, nz, diff)
		sum := g.newTmp()
		fmt.Fprintf(b, "  %s = fadd double %s, %s\n", sum, rm, r)
		fmt.Fprintf(b, "  %s = select i1 %s, double %s, double %s\n", t, adj, sum, rm)
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
	case *Index:
		// Same fold as floatValue: the element the literal wrote, not the word in the slot
		// (roadmap L11.1, ADR 0243).
		if el, ok := g.staticNumericElem(n); ok {
			return g.floatEval(el)
		}
		return 0, false
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
			if id, ok := n.Fn.(*Name); ok && g.builtinShadowed(id.Value) && !g.floatFuncs[id.Value] {
				return 0, false
			}
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
func (g *irGen) truthyValue(b *strings.Builder, e Expr) (string, error) {
	v, err := g.truthOperandErr(b, e)
	if err != nil {
		// The condition used to be replaced by a false branch and the error dropped on the
		// floor. That is the worst failure this backend can make: `print(1 if s[1] == "b" else 0)`
		// could not lower its condition at all, and printed `0` as if the program had asked a
		// question and been answered. A part that cannot be lowered is a compile error, reported
		// by the caller that has the error channel (roadmap Gap R.45, ADR 0225; ADR 0166).
		return "", err
	}
	return v, nil
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
	// `if x == "a":` where x carries a runtime tag — a loop variable over a container that
	// mixes kinds. An `if` condition is lowered here, not through the expression path, so the
	// tagged comparison gets its hook in both places (ADR 0232).
	if n, ok := e.(*BinOp); ok && (n.Op == "==" || n.Op == "!=") {
		if res, handled, err := g.mixedTaggedCompare(b, n); err != nil {
			return "", err
		} else if handled {
			p := g.newTmp()
			fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", p, res)
			return g.markI1(p), nil
		}
	}
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
		// A container comparison in a condition goes through the same value-equality helper the
		// expression path uses; `if [1] == 1:` emitted `icmp eq i32 @.lst1, 1` before, which is
		// both invalid IR and the wrong question (ADR 0189).
		if (n.Op == "==" || n.Op == "!=") && (g.isContainerExpr(n.L) || g.isContainerExpr(n.R)) {
			eq, err := g.containerEquality(b, n)
			if err != nil {
				return "", err
			}
			t := g.newTmp()
			fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", t, eq)
			return g.markI1(t), nil
		}
		// The same door as the value path: an ordering of two texts asks strcmp, because the
		// operands at this point are two @str_tab indices and their order is the order the texts
		// were interned (roadmap Gap R.84).
		if g.textOrderOperands(n) {
			_, i1, terr := g.emitTextOrder(b, n)
			if terr != nil {
				return "", terr
			}
			return i1, nil
		}
		// The same door as the value path, on the side a condition takes: an ordering of a slot whose
		// kind the object carries has to reach the arms that can ask the tag, because the integer path
		// here reads the payload of a float slot as a count (roadmap L11.1, Gap R.88).
		if g.taggedNumberUseApplies(n) && cmpI1Op(n.Op) != "" {
			res := g.floatBinOp(b, n)
			if g.floatUnlowerable != "" {
				return "", g.floatLoweringRefusal(n)
			}
			p := g.newTmp()
			fmt.Fprintf(b, "  %s = icmp ne i32 %s, 0\n", p, res)
			return g.markI1(p), nil
		}
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
		if g.printsAsInternedStr(val) {
			vIsStr = true
		}
		// The pair and its two tags go in together: a dict entry that keeps the tag of the
		// value it used to hold prints and compares as the old kind (ADR 0187, ADR 0189). And
		// every entry is stored with both tags, because the lookup compares them (ADR 0232):
		// an entry whose tag was never written would answer to the wrong needle. Where
		// elemKindTag cannot describe an operand, this site knows which word it interned — an
		// @str_tab index or a number — and says so.
		//
		// Storing a value of the kind this dict has not held before leaves it with no single
		// kind to claim. It used to *replace* the recorded one, which is a statement about the
		// slot being written and was read as a statement about every slot: `d = {"a": 1}` then
		// `d["b"] = "x"` printed {'a': 'x', 'b': 'x'} — the number 1 printed as the string whose
		// interned index happens to be 1. The slots carry their tags, so the container is
		// promoted to describing itself, and the refusal stays for a value no tag can name.
		contradicts := (vIsStr && g.dictValInt[nm.Value]) || (!vIsStr && g.dictValStr[nm.Value]) ||
			(kIsStr && g.dictKeyInt[nm.Value]) || (!kIsStr && g.dictKeyStr[nm.Value])
		if !g.mixedDicts[nm.Value] {
			if contradicts {
				if !g.promoteMixed(b, h, nm.Value, "dict value", val, ix.Idx) {
					return mixedKindErr("dict")
				}
			} else {
				g.replaceElemKind(nm.Value, "dict value", vIsStr)
				g.replaceElemKind(nm.Value, "dict key", kIsStr)
			}
		}
		kt, ktOK := g.elemKindTag(ix.Idx)
		if !ktOK {
			if kIsStr {
				kt = int32(TagStr)
			} else {
				kt = int32(TagInt)
			}
		}
		vt, vtOK := g.elemKindTag(val)
		if !vtOK {
			if vIsStr {
				vt = int32(TagStr)
			} else {
				vt = int32(TagInt)
			}
		}
		b.WriteString(fmt.Sprintf("  call void @rt_dict_put_tagged(i32 %s, i32 %s, i32 %s, i32 %d, i32 %d)\n", h, key, v, kt, vt))
		// An entry whose key or value is a float or None has a payload the dict's compiled kinds
		// cannot render — a box handle and nothing — so the dict stops claiming them, the same move
		// ADR 0232 made for an entry that mixes numbers with text (roadmap L11.1, ADR 0233).
		if !g.mixedDicts[nm.Value] && (slotTagSelfDescribing(kt) || slotTagSelfDescribing(vt)) {
			if g.promoteMixed(b, h, nm.Value, "dict value", val, ix.Idx) {
				g.floatFmtUsed = g.floatFmtUsed || kt == int32(TagFloat) || vt == int32(TagFloat)
			}
		}
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
		if g.mixedLists[nm.Value] {
			// An element of a tagged list is (payload, tag); writing the payload alone would
			// leave the slot tagged as whatever lived there before, so the tag is written with
			// it. This is the shape that used to answer `[1, 'a', None]` for `xs[0] = "z"` —
			// the interned index printed through the stale int tag (roadmap L11.1, ADR 0187).
			sv, tag, serr := g.mixedElemTag(b, val)
			if serr != nil {
				return serr
			}
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
			b.WriteString(fmt.Sprintf("  call void @rt_put_elem(i32 %s, i32 %s, i32 %s)\n", h, key, sv))
			b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %s, i32 %s, i32 %s)\n", h, key, tag))
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
			b.WriteString(fmt.Sprintf("%s:\n", endL))
			return nil
		}
		// A string element is stored as its @str_tab index (Gap I.2), like every other
		// container slot write; the printed form follows from listElemStr. What counts as a
		// string word is asked of the value twice, because heapElemKind answers for the word it
		// interned and printsAsInternedStr answers for the value the source says it is: `xs[0] =
		// d["a"]` reads a string out of a dict, and deciding "not a string" there promoted
		// nothing and left the list printing its new string element through the number printer —
		// the store happened, the print lied (ADR 0232).
		sv, sIsStr, serr := g.heapElemKind(b, val)
		if serr != nil {
			return serr
		}
		if g.printsAsInternedStr(val) {
			sIsStr = true
		}
		// Writing one slot is not the same statement as what the whole list holds. This site used
		// to overwrite the recorded kind, so `xs = [1, 2]; xs[0] = "s"` left the list claiming
		// strings and printed the untouched 2 as whatever string its index happens to name
		// (`['s', 'b']`). Contradiction promotes the list to describing its slots (ADR 0232);
		// agreement keeps the static path.
		if !g.mixedLists[nm.Value] {
			if contradicts := (sIsStr && g.listElemInt[nm.Value]) || (!sIsStr && g.listElemStr[nm.Value]); contradicts {
				if !g.promoteMixed(b, h, nm.Value, "list", val) {
					return mixedKindErr("list")
				}
			} else {
				g.replaceElemKind(nm.Value, "list", sIsStr)
			}
		}
		v = sv
		// Bounds are checked so an out-of-range index raises IndexError through the
		// The index is normalised and bounds-checked by the same helper the read path
		// uses, so `xs[-1] = v` writes the last element and an out-of-range write
		// raises IndexError through the same path an explicit `raise` uses
		// (roadmap L11.4, ADR 0210).
		endL := g.newLabel("item.end")
		key = g.normalizeIndex(b, h, key, ix.Span())
		b.WriteString(fmt.Sprintf("  call void @rt_put_elem(i32 %s, i32 %s, i32 %s)\n", h, key, v))
		b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %s, i32 %s, i32 %s)\n", h, key, g.elemTagOperand(b, val, sIsStr)))
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
	case *FloatLit:
		// Naming matters in a refusal: this helper filled the blank in "an element slot is an i32
		// word and <blank> has no representation in one", which told the reader nothing about
		// what to change (ADR 0226).
		return "a float value"
	case *IntLit:
		return "an integer"
	case *StrLit:
		return "a string"
	case *BoolLit:
		return "a bool"
	case *NoneLit:
		return "None"
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
	savedMixDict, savedMixSet := g.mixedDicts, g.mixedSets
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
	g.mixedDicts = map[string]bool{}
	g.mixedSets = map[string]bool{}
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
		g.mixedDicts, g.mixedSets = savedMixDict, savedMixSet
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

// nameShadowed applies a (possibly nil) shadow predicate.
func nameShadowed(shadowed func(string) bool, name string) bool {
	return shadowed != nil && shadowed(name)
}

// builtinShadowed reports whether the program defines a function of this name itself —
// in which case the name belongs to the program, and codegen must not read a call to it
// as the built-in of that name.
//
// Both the interpreter and CPython let a `def` shadow a built-in, and the interpreter
// already resolves it that way; the compiled path read the call by name instead, so
//
//	def float(x):
//	    return x + 7
//	print(float(1))
//
// printed 8 interpreted and 1.0 compiled — the float-shape helpers saw the *name* `float`
// and folded the call as the conversion, never asking whether the program had defined it.
// `str` and `chr` were worse still: the call was emitted as the user's function and then
// *used* as the builtin's string result, which llc rejected (roadmap Gap R.6, ADR 0199).
//
// The rule is not "refuse the name" — `float`, `str`, `len`, `sum` are names people choose
// deliberately — it is that the program's definition wins, the way every other scope rule
// in the language says the innermost declaration wins.
func (g *irGen) builtinShadowed(name string) bool {
	if name == "" {
		return false
	}
	if g.funcs[name] {
		return true
	}
	if fd, ok := g.fds[name]; ok && fd != nil {
		return true
	}
	return false
}

// builtinCallAs reports whether a call may still be read as the built-in `name`: the callee
// has to be that bare name, and the program must not have defined it. Every shape decision
// keyed on a built-in name goes through this, so there is one place that decides.
func (g *irGen) builtinCallAs(c *Call, name string) bool {
	n, ok := c.Fn.(*Name)
	return ok && n.Value == name && !g.builtinShadowed(name)
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
			// A walrus binds, so the slot is written: ADR 0228's flag has to know.
			g.markBound(b, name)
		}
		t := g.newTmp()
		if g.isFloat(n.Value) {
			b.WriteString(fmt.Sprintf("  %s = load double, double* %%_%s\n", t, name))
		} else {
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", t, name))
		}
		return t, nil
	case *Name:
		if g.taggedVars[n.Value] {
			// The value slot only means something together with its tag, and this
			// context wants a number, not a (value, tag) pair (ADR 0185).
			return "", mixedTaggedVarErr(n.Value)
		}
		// String variables are compile-time constants (strVals); emit their
		// global pointer so printf/assign via value() sees the real string.
		if sv, ok := g.strVals[n.Value]; ok {
			// Same rule as a literal: the folded constant is still a string *value*, and a
			// string value is an @str_tab index (Gap R.42, ADR 0224).
			return g.internStr(b, sv), nil
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
		if reg, ok := g.params[n.Value]; ok && !g.paramSlot[n.Value] {
			// The incoming register is the value only until the body rebinds the name;
			// a rebound parameter reads its slot, which the entry copied over (Gap R.3).
			return reg, nil
		}
		// A slot the checker could not prove was written carries a flag, and the read tests it: an
		// unwritten local raises UnboundLocalError here, catchably, instead of loading whatever the
		// frame happened to hold (roadmap Gap R.36, ADR 0228).
		g.checkBound(b, n.Value, n.Span())
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
		if sym, ok := g.moduleSlots[n.Value]; ok && (!g.inFunc || !(g.allocd[n.Value] || (g.funcLocals != nil && g.funcLocals[n.Value]))) {
			// Read the module's own global: whatever the module assigned most recently, not what
			// this frame happened to see when it was compiled (ADR 0227).
			// No rooting: a module slot holds an i32 value (int, interned string index, or the
			// None handle, which the runtime allocates once and never frees), never a collectable
			// heap pointer -- floats and containers are excluded from moduleSlots for that reason.
			g.ldN++
			b.WriteString(fmt.Sprintf("  %%l%d = load i32, i32* @%s\n", g.ldN, sym))
			return fmt.Sprintf("%%l%d", g.ldN), nil
		}
		if g.inFunc {
			// The module is a scope too (ADR 0220), and a compiled body reaches it through these
			// two tables. A name bound to a literal the module never rebinds is a value, so it
			// needs no slot -- `MAX = 40` then `def f(): return MAX` answers 40 rather than refusing
			// or, as it did before, answering 0 through an empty closure body (ADR 0227).
			// A binding inside the body wins: `K = 1` in a function is a local even when the module
			// also binds K (ADR 0220), and reading it from the module would answer 5 for 1.
			if folded, ok := g.moduleConsts[n.Value]; ok && !(g.allocd[n.Value] || (g.funcLocals != nil && g.funcLocals[n.Value])) {
				return g.value(b, folded)
			}
			if g.moduleNames[n.Value] && !(g.allocd[n.Value] || (g.funcLocals != nil && g.funcLocals[n.Value])) {
				return "", fmt.Errorf("codegen: %q is bound at module level, and a compiled function body cannot read module-level state that changes: only a literal the module never rebinds is visible here (the interpreter answers this program; compiled module globals are roadmap Gap R.35)", n.Value)
			}
		}
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
		// Membership in a container whose slots describe themselves is decided by (payload,
		// tag): comparing payloads alone would answer `0 in {"0": 1}` from the interned index
		// the key's word happens to hold (ADR 0232's soundness rule). This is checked before
		// the operands are lowered, because a tagged loop variable has no untagged lowering at
		// all — `for x in s: x in t` is the shape this exists for.
		if n.Op == "in" || n.Op == "not in" {
			if res, ok, err := g.mixedMembership(b, n); err != nil {
				return "", err
			} else if ok {
				return res, nil
			}
		}
		// `if x == "a":` where x came from a loop over a container that mixes kinds: the
		// comparison is between a (payload, tag) pair and a value whose kind the compiler
		// knows, and the tag has to take part or `x == 1` answers true for the string whose
		// interned index happens to be 1.
		if n.Op == "==" || n.Op == "!=" {
			if res, ok, err := g.mixedTaggedCompare(b, n); err != nil {
				return "", err
			} else if ok {
				return res, nil
			}
		}
		// Operator overloading: dispatch dunder methods on statically-known
		// class instances before falling back to builtin arithmetic.
		if res, ok := g.emitDunderBinOp(b, n); ok {
			return res, nil
		}
		if g.isFloat(n.L) || g.isFloat(n.R) || g.taggedNumberUseApplies(n) {
			// Which question is this? Two operands of different runtime kinds are not equal
			// (ADR 0215's rule; ADR 0221 makes the numeric cross-kind pair the exception), and
			// CPython answers `1.0 == [1]` with False without ever converting the list. Handing
			// it to the float path instead emitted `sitofp i32 @.lst1 to double` -- a container
			// global fed to a float conversion -- and llc rejected the module, so the program
			// got a toolchain rejection for asking an ordinary question (Gap R.40, ADR 0166).
			other := n.R
			if !g.isFloat(n.L) {
				other = n.L
			}
			if g.isContainerExpr(other) && !(g.isContainerExpr(n.L) && g.isContainerExpr(n.R)) {
				switch n.Op {
				case "==", "!=", "is", "is not":
					eq := n.Op == "!=" || n.Op == "is not"
					v := "0"
					if eq {
						v = "1"
					}
					return v, nil
				default:
					return "", fmt.Errorf("codegen: ordering a number against a %s is a TypeError this backend cannot raise at runtime (roadmap Gap R.37)", exprTyName(other))
				}
			}
			switch n.Op {
			case "==", "!=", "<", "<=", ">", ">=":
				res := g.floatBinOp(b, n)
				if g.floatUnlowerable != "" {
					return "", g.floatLoweringRefusal(n)
				}
				return res, nil
			}
		}
		// A numeric use of a slot whose kind the object carries, in either direction. The float arms are
		// the only ones that can ask the tag what the payload means, and the read brings its
		// (payload, tag) pair to them: a float slot unboxes, an int or bool slot converts, and a slot
		// holding text, None or a container raises the TypeError CPython raises for this operator and
		// that kind. The gate sits above the ordinary arithmetic lowering on purpose — that path reads
		// the payload as a number, which is what this door exists to stop (roadmap L11.1, Gap R.88).
		if g.taggedNumberUseApplies(n) {
			switch n.Op {
			case "+", "-", "*", "/", "//", "%", "**":
				res := g.floatBinOp(b, n)
				if g.floatUnlowerable != "" {
					return "", g.floatLoweringRefusal(n)
				}
				return res, nil
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
				// A folded concatenation is still a string *value*, so it is an @str_tab
				// index; the pointer belongs only to the printf paths that ask for bytes
				// (Gap R.42, ADR 0224).
				return g.internStr(b, ls+rs), nil
			}
		}
		// A container is equal to another container by *value*. Both backends compared the
		// two i32s instead, which compared heap slots: xs == ys was False for two equal
		// lists, [1] == [1] was False, and a literal operand made the module invalid (the
		// static @.lstN global in an icmp). rt_container_eq walks the (payload, tag) pairs
		// the way Python's __eq__ walks elements (roadmap L11.1, ADR 0189). `is` stays
		// identity, which is what Python's `is` is for containers.
		if (n.Op == "==" || n.Op == "!=") && (g.isContainerExpr(n.L) || g.isContainerExpr(n.R)) {
			return g.containerEquality(b, n)
		}
		// `2 in xs[0]`: the haystack is a read that names a container, so its handle comes out of
		// the slot rather than out of the value path, which has no single kind to name it with. The
		// needle arrives as a (payload, tag) pair for the same reason the container's own elements
		// are stored that way — membership is the payload-and-tag question, one level out
		// (roadmap L11.1, ADR 0241).
		if n.Op == "in" || n.Op == "not in" {
			if _, isRead := n.R.(*Index); isRead {
				if res, okM, mErr := g.membershipOfReadHaystack(b, n); mErr != nil {
					return "", mErr
				} else if okM {
					return res, nil
				}
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
		// An ordering of two texts is answered here, before the refusal below: the operands are
		// already lowered, and the question is strcmp's, not the interned index's (roadmap Gap R.84).
		if g.textOrderOperands(n) {
			res, _, oerr := g.emitTextOrderOperands(b, n.Op, l, r)
			if oerr != nil {
				return "", oerr
			}
			return res, nil
		}
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
				// The message has to say what is missing rather than claim orderings on text have no
				// meaning: an ordering of two texts the compiler can see is answered by strcmp since
				// ADR 0248, and what reaches this line is arithmetic, or an ordering with an operand
				// whose textness the compiler cannot prove — a parameter, typically, whose kind the call
				// sites do not agree on in a way this pass can carry into the body (Gap R.38: a refusal
				// that claims something false about the language is its own defect).
				return "", fmt.Errorf("codegen: operator %q on a string (%s) is not supported in the AOT backend; the interpreter evaluates it — a compiled string is an interned table index, so arithmetic on it has no meaning, and an ordering of two texts is answered only where the compiler can see both sides are text (roadmap Gap R.82)", n.Op, exprSnippet(n.L))
			}
			if n.Op == "+" && (isStrOperand(n.L) || isStrOperand(n.R)) {
				if _, ok := g.stringVal(n.L); ok {
					if _, ok2 := g.stringVal(n.R); ok2 {
						break // both constant: folded below
					}
				}
				// Building a string while the program runs is a buffer, an intern, and the
				// same index convention (ADR 0229): the operands' values are indices, the
				// result is an index, and print/==/in/container slots keep working on it.
				ls, lok, lerr := g.strReg(b, n.L)
				if lerr != nil {
					return "", lerr
				}
				rs, rok, rerr := g.strReg(b, n.R)
				if rerr != nil {
					return "", rerr
				}
				if lok && rok {
					return g.rtStrCall(b, "rt_str_cat", "i32 "+ls, "i32 "+rs), nil
				}
				if lok != rok {
					return "", fmt.Errorf("codegen: concatenating a string with a value that is not a string is not supported in the AOT backend; CPython and the interpreter raise TypeError for it")
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
					// `//` folds to the *floor* quotient (`/` never reaches here: true
					// division is always a float). A folder that truncates would be a
					// third opinion disagreeing with both backends (Gap R.28, R.30).
					if rv != 0 {
						if n.Op == "//" {
							res, folded = floorDiv(lv, rv), true
						} else {
							res, folded = lv/rv, true
						}
					}
				case "%":
					if rv != 0 {
						res, folded = floorMod(lv, rv), true
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
			// l is the value to test, r is the container handle. A comprehension on the right is
			// the container the program means, but the constant path hands back a folded global
			// whose layout is a length plus an array: `rt_contains(i32 @.set1, i32 2)` is a global
			// in an i32 slot, the module llc refuses, so it is materialised into the heap first
			// (ADR 0166's rule one construct on; roadmap Gap J.2, ADR 0234).
			if _, isComp := n.R.(*Comp); isComp || g.isContainerExpr(n.R) {
				ch, chErr := g.containerOperand(b, n.R)
				if chErr != nil {
					return "", chErr
				}
				r = ch
			}
			// A needle that is itself a container is built as an object and matched by the tagged
			// slot rule. Two reasons, both measured: the constant path hands back a folded global
			// whose layout is a length plus an array, which llc refuses in an i32 parameter; and an
			// untagged i32 compare of two slots against a handle would never notice that two
			// literals holding the same elements denote the same value, so [1, 2] in [[1, 2], 3]
			// would answer False forever (roadmap L11.1, ADR 0189).
			if g.isContainerExpr(n.L) {
				nh, nErr := g.containerOperand(b, n.L)
				if nErr != nil {
					return "", nErr
				}
				l = nh
				nt, _ := g.elemKindTag(n.L)
				t := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = call i32 @rt_contains_tagged(i32 %s, i32 %s, i32 %d)\n", t, r, l, nt))
				bt := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = icmp ne i32 %s, 0\n", bt, t))
				g.markI1(bt)
				if n.Op == "not in" {
					inv := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = xor i1 %s, true\n", inv, bt))
					g.markI1(inv)
					return g.asBoolI32(b, inv), nil
				}
				return g.asBoolI32(b, bt), nil
			}
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
		case "/":
			// True division reaches the i32 domain only where no float is expected (a
			// float-valued assignment is emitted in the double domain instead, and prints
			// 3.5 for 7 / 2); the zero guard below is what makes `1 / 0` a trap here.
			op = "sdiv"
		case "//":
			// Floor division, not truncation. `sdiv` truncates toward zero, so the
			// quotient steps down one whenever there is a remainder and the operands
			// have opposite signs — the same rule as floorDiv in the interpreter, said in
			// IR (roadmap Gap R.30: `-7 // 2` compiled to -3 and printed it happily).
			// The zero guard is emitted here as well as for the shared path below, because
			// this case returns before reaching it.
			g.guardNonZeroInt(b, r, n.Span(), "integer division or modulo by zero")
			q := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = sdiv i32 %s, %s\n", q, l, r))
			rm := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = srem i32 %s, %s\n", rm, l, r))
			rne := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", rne, rm))
			ls := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", ls, l))
			rs := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", rs, r))
			diff := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = xor i1 %s, %s\n", diff, ls, rs))
			adj := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", adj, rne, diff))
			qm := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = sub i32 %s, 1\n", qm, q))
			b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, adj, qm, q))
			return t, nil
		case "%":
			// Floor modulo: the remainder carries the divisor's sign, which is what keeps
			// a == (a // b) * b + (a % b) true for every sign combination. `srem` alone
			// truncates (`-7 % 2` answered -1), so add the divisor back when the signs
			// disagree and there is a remainder (roadmap Gap R.28). Guarded here, too:
			// this case returns before the shared `sdiv`/`srem` guard below.
			g.guardNonZeroInt(b, r, n.Span(), "integer modulo by zero")
			rm := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = srem i32 %s, %s\n", rm, l, r))
			rne := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", rne, rm))
			rneg := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", rneg, rm))
			bneg := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", bneg, r))
			diff := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = xor i1 %s, %s\n", diff, rneg, bneg))
			adj := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", adj, rne, diff))
			sum := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", sum, rm, r))
			b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, adj, sum, rm))
			return t, nil
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
			// Two texts order by their characters, never by their interned index: the index records
			// which spelling the program mentioned first, so every `<`/`>` on text had an answer that
			// depended on the order of the source lines (roadmap Gap R.84). The operands are already
			// lowered above, so the door takes their registers rather than lowering them a second time
			// — an operand that prints must not print twice.
			if g.textOrderOperands(n) {
				res, _, terr := g.emitTextOrderOperands(b, n.Op, l, r)
				if terr != nil {
					return "", terr
				}
				return res, nil
			}
			// A comparison is a *value* (printable, storable, passable), so it
			// returns the interpreter's i32 0/1; the i1 predicate stays internal
			// (tracked in i1Vals) so a condition can use it without re-testing.
			b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", t, op, l, r))
			g.markI1(t)
			res := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", res, t))
			return res, nil
		}
		if op == "sdiv" || op == "srem" {
			// Division by zero is a language event, not an instruction: on x86 an unguarded
			// `sdiv` is a SIGFPE, and on AArch64 it does not trap at all — `print(7 % 0)`
			// answered a different garbage number each run and exited 0 (roadmap Gap R.18,
			// ADR 0212).
			kind := "division by zero"
			if n.Op == "//" {
				kind = "integer division or modulo by zero"
			}
			if op == "srem" {
				kind = "integer modulo by zero"
			}
			g.guardNonZeroInt(b, r, n.Span(), kind)
		}
		b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", t, op, l, r))
		return t, nil
	case *UnOp:
		// `-xs[i]` before the operand is read as a plain number: the read below would refuse, and the
		// float arms are the only place that can ask the slot's tag what it holds. Negation carries its
		// own sentence, so a text slot says `bad operand type for unary -: 'str'` the way CPython does
		// rather than the binary minus's line (roadmap L11.1, Gap R.88).
		if n.Op == "-" && g.taggedNegationApplies(n) {
			res, ok, err := g.taggedFloatNegate(b, n)
			if err != nil {
				return "", err
			}
			if ok {
				neg := g.newTmp()
				// The same spelling the static float negation uses: `fsub double 0.0, %v`. A hand-written
				// `fneg` is the right idea and the wrong token for this LLVM, which read it as a malformed
				// instruction and had llc reject the compiler's own module.
				fmt.Fprintf(b, "  %s = fsub double 0.0, %s\n", neg, res)
				return neg, nil
			}
		}
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
		cond, cerr := g.truthyValue(b, n.Cond)
		if cerr != nil {
			return "", cerr
		}
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
		// A string *value* in this language is an index into the runtime @str_tab, not the
		// address of a private global: `x == "hi"`, `return "hi"` from a string-returning
		// function, and `self.w = "hi"` all put it in an i32 slot, and emitting `@.str7`
		// there made llc reject the module -- an exit-2 toolchain rejection for an ordinary
		// program (ADR 0166's rule, roadmap Gap R.42 and L11.8). Contexts that genuinely want
		// the bytes -- printf, the compile-time folds in rt_str_* helpers, the traceback
		// strings -- go through g.strConst directly, never through value().
		return g.internStr(b, n.Value), nil
	case *FString:
		return "", fmt.Errorf("codegen: f-string requires a constant expression (AOT backend)")
	case *ListLit:
		// inline list literal: emit a dedicated global struct and return its name.
		if literalNeedsHeap(n) {
			// A literal that mixes kinds is not a refusal when every slot can be tagged: the tags
			// carry the meaning the container-wide kind used to. The list branch asked this later
			// than the dict and set branches did, so `f([1, "a"])` was refused where `f({1: "a"})`
			// was built (roadmap L11.1, ADR 0184's rule applied in one place rather than two).
			if literalMixedKinds(n) && !g.taggableMixedList(n) {
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
		// A dict that mixes kinds is not a refusal when every slot can be tagged: it is a
		// dict with no kind, and the tags carry the meaning (ADR 0232).
		if literalNeedsHeap(n) {
			if literalMixedKinds(n) && !g.taggableMixedDict(n) {
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
			if literalMixedKinds(n) && !g.taggableMixedSet(n) {
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
		// String slices are folded at compile time via stringVal; the fold is a string
		// value, so it is emitted as its @str_tab index -- the pointer is for printf
		// contexts, which fold the text themselves (Gap R.42, ADR 0224).
		// The gate is "is this a string?", not "can the compiler read its text?": a slice of a
		// string the emitter only knows the *kind* of (`s = "abcdef"` inside a function body)
		// still has an answer, and the table gives it (ADR 0229).
		_, objIsFoldedStr := g.stringVal(n.Obj)
		if objIsFoldedStr || g.exprIsString(n.Obj) {
			if sv, ok := g.stringVal(n); ok {
				return g.internStr(b, sv), nil
			}
			// Bounds that are values rather than constants are still a slice of the same
			// string: an absent bound is spelled with the helper's own sentinel, because an
			// i32 argument cannot be "missing" (ADR 0229).
			if n.Step == nil {
				if sreg, sok, serr := g.strReg(b, n.Obj); serr != nil {
					return "", serr
				} else if sok {
					lo := "-2147483648"
					hi := "2147483647"
					if n.Low != nil {
						v, lerr := g.strPosReg(b, n.Low)
						if lerr != nil {
							return "", lerr
						}
						lo = v
					}
					if n.High != nil {
						v, herr := g.strPosReg(b, n.High)
						if herr != nil {
							return "", herr
						}
						hi = v
					}
					r := g.rtStrCall(b, "rt_str_slice", "i32 "+sreg, "i32 "+lo, "i32 "+hi)
					g.checkStrSentinels(b, r, "IndexError", "string slice out of range", n.Span(), "stslice")
					return r, nil
				}
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
		// A subscript of a string the compiler cannot read asks the table (ADR 0229): the
		// base only has to *be* a string, not be a constant. The fold below still answers
		// everything the compiler can name, so this path is reached exactly when the fold
		// cannot — which used to be a refusal, so `print(s[1])` worked and `print(get()[1])`
		// did not, one rule with two behaviours.
		_, foldBase := g.stringVal(n.Obj)
		_, foldIdx := n.Idx.(*IntLit)
		if !foldBase || !foldIdx {
			if ch, isStr, err := g.emitStrChar(b, n.Obj, n.Idx, n.Span()); err != nil {
				return "", err
			} else if isStr {
				return ch, nil
			}
		}
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
		// A subscript of a string is a one-character string (ADR 0225), and the base the
		// compiler can name is folded: a literal, a variable holding text, a folded
		// concatenation. Negative positions are positions, as ADR 0210 ruled for containers,
		// and the count is code points, which is how `len` and slicing already count. Without
		// this the base only worked as a literal, so `s = "abc"; print(s[1])` refused while
		// `"abc"[1]` answered -- one rule, two behaviours.
		if _, isInt := n.Idx.(*IntLit); isInt {
			if txt, isStr := g.stringVal(n.Obj); isStr {
				runes := []rune(txt)
				i := normPosIndex(key, int64(len(runes)))
				if i < 0 || i >= int64(len(runes)) {
					return "", fmt.Errorf("string index out of range")
				}
				return g.internStr(b, string(runes[i])), nil
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
				// The key is compared as (payload, tag). A string needle's payload is an
				// @str_tab index, and an int key holding the same number is a different key:
				// {1: "one"} asked for "a" must raise KeyError, not answer "one" — which is
				// what an untagged comparison did until the tag was consulted here too
				// (ADR 0232, and ADR 0189's promise that the tag is always there to read).
				keyTag := ""
				if _, isLit := n.Idx.(*IntLit); !isLit {
					kv, isStr, e := g.heapElemKind(b, n.Idx)
					if e != nil {
						return "", e
					}
					if isStr {
						g.dictKeyStr[obj.Value] = true
					}
					keyOp = kv
					if t, ok := g.elemKindTag(n.Idx); ok {
						keyTag = strconv.FormatInt(int64(t), 10)
					} else if isStr {
						keyTag = strconv.FormatInt(int64(TagStr), 10)
					} else {
						keyTag = strconv.FormatInt(int64(TagInt), 10)
					}
				} else if nm, ok := n.Idx.(*Name); ok && g.taggedVars[nm.Value] {
					kt := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s_tag\n", kt, nm.Value))
					keyTag = kt
				}
				if keyTag != "" {
					g.checkKeyReadTagged(b, fmt.Sprintf("%%h%d", hs), keyOp, keyTag, n.Span())
					b.WriteString(fmt.Sprintf("  %%g%d = call i32 @rt_dict_get_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, hs, keyOp, keyTag))
				} else {
					g.checkKeyRead(b, fmt.Sprintf("%%h%d", hs), keyOp, n.Span())
					b.WriteString(fmt.Sprintf("  %%g%d = call i32 @rt_dict_get(i32 %%h%d, i32 %s)\n", hs, hs, keyOp))
				}
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
				if g.mixedLists[obj.Value] {
					// A numeric use of an element is not a use that needs the tag. When the container is one the
					// program spelled out and never changed, and the element is a number literal, the slot holds
					// that number: `xs = [1, "a"]; print(xs[0] + 1)` is an addition on 1, and the float form takes
					// the float path with it (roadmap L11.1, ADR 0243). What the promise does not cover — a name
					// element, a runtime index, text or a container in the slot — keeps the refusal below, because
					// for those the tag is exactly what the answer depends on.
					if v, ok := g.numericElemUse(b, n); ok {
						return v, nil
					}
					return "", mixedReadErr("list")
				}
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, obj.Value))
				idxOp = g.normalizeIndex(b, fmt.Sprintf("%%h%d", hs), idxOp, n.Span())
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
			// index into a list literal: evaluate the element directly. A negative key
			// counts from the end like every other positional subscript (L11.4), and it has
			// to be checked here: this line used to hand `-1` straight to the Go slice and
			// crash the compiler (`panic: runtime error: index out of range [-1]`), which the
			// exit-code contract classifies as a compiler bug (ADR 0168's rule).
			li := int(normPosIndex(key, int64(len(obj.Elems))))
			if li < 0 || li >= len(obj.Elems) {
				return "", fmt.Errorf("list index out of range")
			}
			return g.value(b, obj.Elems[li])
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
				key = normPosIndex(key, int64(n))
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
			// The same rule as the interpreter (ADR 0225): a subscript of a string is a
			// one-character string, so it comes back as an @str_tab index, and it is counted in
			// code points. `%d`-ing the byte made `s[1] == "b"` compile and answer false -- a wrong
			// value, not a refusal, which is the worst thing this backend can do.
			runes := []rune(str)
			key = normPosIndex(key, int64(len(runes)))
			if key < 0 || key >= int64(len(runes)) {
				return "", fmt.Errorf("string index out of range")
			}
			return g.internStr(b, string(runes[key])), nil
		case *Call:
			// Element access into list-producing call expressions: keys(),
			// values(), sorted(...), reversed(...), split(...). partition()
			// returns only dummy length elems (see dictMethodElems), so it
			// is excluded here to avoid silently wrong results.
			if elems, ok2 := g.indexListElems(obj); ok2 {
				key = normPosIndex(key, int64(len(elems)))
				if key < 0 || int(key) >= len(elems) {
					return "", fmt.Errorf("list index out of range")
				}
				return g.value(b, elems[key])
			}
			// The base is an expression rather than a name the compiler holds (`t[0][0]`, `d["a"][1]`,
			// `xs[0][0]`). Same rule as the mixed-list arm above: a literal number in a slot of a container
			// the program spelled out and never changed *is* that number, and the arithmetic the language
			// already has runs on it (roadmap L11.1, ADR 0243).
			if v, ok := g.numericElemUse(b, n); ok {
				return v, nil
			}
			return "", g.slotReadRefusal(n, "index")
		default:
			if v, ok := g.numericElemUse(b, n); ok {
				return v, nil
			}
			return "", g.slotReadRefusal(n, "index")
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

// runtimeCompLoop is the comprehension over a container whose length is only known at runtime —
// a variable, or a builtin that builds one. Unlike the unrolled paths it is a real loop: an index
// counter, rt_get_elem per step, the loop variable bound to the element the way a `for` statement
// binds it (roadmap L11.7, ADR 0192).
func (g *irGen) runtimeCompLoop(b *strings.Builder, c *Comp) (string, error) {
	// The operands a loop has to lower: one element for a list or a set, a key and a value for a
	// dict. A shape the loop cannot spell is a refusal, never a container built without them.
	var elemExprs []Expr
	switch c.Kind {
	case CompDict:
		if len(c.Keys) != 1 || len(c.Vals) != 1 {
			return "", fmt.Errorf("codegen: a runtime comprehension builds one entry at a time (roadmap L11.1)")
		}
		elemExprs = []Expr{c.Keys[0], c.Vals[0]}
	default:
		if len(c.Elems) != 1 {
			return "", fmt.Errorf("codegen: a runtime comprehension builds one element at a time (roadmap L11.1)")
		}
		elemExprs = []Expr{c.Elems[0]}
	}
	for _, el := range elemExprs {
		if g.nestedContainerElem(el) && !g.taggableNestedElem(el) {
			return "", nestedContainerErr(el)
		}
	}
	// Iterating a container whose slots mix kinds binds its loop variable by tag — the pair ADR 0185
	// put in `%_x` and `%_x_tag` for `for`, and which the comprehension loop now binds the same way.
	// Reading the payload without the tag is how `out = [x for x in sa]` over {1, "a", None} printed
	// [1, 0, 0] on the compiled backend: three numbers, two of them zeros, where CPython prints
	// [1, 'a', None] (roadmap Gap R.76, ADR 0244).
	mixedIter := false
	if nm, ok := c.Iter.(*Name); ok {
		mixedIter = g.mixedLists[nm.Value] || g.mixedSets[nm.Value] || g.mixedDicts[nm.Value]
	}
	// Iterating a dict walks its **keys**, and a dict entry occupies two words, so the position is the
	// counter doubled and the length asked of the object is its entry count — the same pair of facts
	// `for v in d:` applies (ADR 0188). Reading slot 0 and slot 1 instead of slot 0 and slot 2 answers
	// `['a', 1]` for `{"a": 1, 2: "b"}`, which is a key and a value wearing the two keys' place.
	iterIsDict := false
	if nm, ok := c.Iter.(*Name); ok {
		iterIsDict = g.runtimeDicts[nm.Value] || g.mixedDicts[nm.Value]
	}
	// The iterable has to become a heap handle. A tracked container variable already has one;
	// a variable the compiler kept as a compile-time list does not — its value is a folded
	// global whose layout is a length plus an array, so it is materialised into the heap here.
	// That the same list can be either depends on what else the program does with it is exactly
	// the kind of compiler-state decision this project keeps having to chase, so both routes are
	// explicit rather than one of them being an accident.
	// A variable the compiler kept as a compile-time list has no slot to load — asking for one
	// emits `load i32, i32* %_xs` for a register that does not exist, which is the module llc
	// rejects — so the static route is tried first and only a real container variable goes
	// through the heap path.
	// A name the escape analysis kept as a compile-time list has no runtime object to walk and
	// no slot to load: emitting the load is the module llc rejects. It is a refusal today, and
	// the honest fix is the tagged value word making every container a runtime object.
	if nm, ok := c.Iter.(*Name); ok && !g.allocd[nm.Value] && !g.listVars[nm.Value] && !g.mixedLists[nm.Value] && !g.runtimeSets[nm.Value] && !g.runtimeDicts[nm.Value] {
		return "", fmt.Errorf("codegen: %s is a list the compiler kept as a compile-time constant, so a comprehension cannot walk it at runtime; mutating it (append) or iterating it with `for` materialises it, and the tagged value word will make every container a runtime object (roadmap L11.2, ADR 0192)", nm.Value)
	}
	var src string
	var err error
	lowered, lerr := g.value(b, c.Iter)
	if lerr == nil {
		if lit, ok := g.staticLists[lowered]; ok {
			src, err = g.heapListFrom(b, lit, "")
			if err != nil {
				return "", err
			}
		} else {
			src = lowered
		}
	} else {
		src, err = g.containerOperand(b, c.Iter)
		if err != nil {
			return "", err
		}
	}
	lv := loopVarName(c.ForVar)
	if !g.allocd[lv] {
		b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", lv))
		g.gcReg(b, lv)
		g.allocd[lv] = true
	}
	g.heapUsed = true // the comprehension is often the only heap allocation in the program
	// The loop variable of a string container holds an index into @str_tab, and saying so is
	// what makes `n == "a"` in the filter compare texts instead of comparing an index with a
	// string global — which is the IR llc rejects (the `for` statement records the same fact).
	iterIsStrElems := false
	if nm, ok := c.Iter.(*Name); ok {
		// A dict iteration yields keys, so it is the key kind that decides what the loop variable
		// holds — the same fact `for v in d:` uses to print a text key as text instead of as its
		// index in @str_tab (Gap I.2, ADR 0188).
		iterIsStrElems = g.listElemStr[nm.Value] || g.setElemStr[nm.Value] || g.dictKeyStr[nm.Value]
	}
	if ty := exprTyName(c.Iter); !iterIsStrElems && containerKindFromTy(ty) != "" && strings.Contains(ty, "str") {
		// The compiler's own tracking is not the only evidence: the analyzer inferred
		// list[str], and a folded (never-materialised) list never populates the kind maps.
		iterIsStrElems = true
	}
	if iterIsStrElems {
		g.internedVars[lv] = true
	}

	g.heapSeq++
	h := fmt.Sprintf("%%h%d", g.heapSeq)
	// The object the loop fills is the container the comprehension means: a list appends, a set
	// adds (deduping on the (payload, tag) pair) and a dict puts (replacing the entry whose key
	// pair matches) — all three through the tagged helpers, which find their own slot, so a filter
	// that skips an item cannot leave a hole behind (roadmap Gap J.2, ADR 0234).
	allocKind := HeapList
	if c.Kind == CompDict {
		allocKind = HeapKindDict
	} else if c.Kind == CompSet {
		allocKind = HeapKindSet
	}
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_alloc(i32 %d)\n", h, allocKind))
	nlen := g.newTmp()
	dictLenFn := "rt_list_len"
	if iterIsDict {
		dictLenFn = "rt_dict_len"
	}
	b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s)\n", nlen, dictLenFn, src))
	// A named preheader block: the induction phi needs a predecessor to take its 0 from, and
	// the block the loop is written in has no name to reference.
	pre := g.newLabel("comp.pre")
	cond := g.newLabel("comp.cond")
	body := g.newLabel("comp.body")
	done := g.newLabel("comp.done")
	b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n  br label %%%s\n%s:\n", pre, pre, cond, cond))
	// The induction variable's phi forward-references the value the body defines; the parser
	// takes that (the runtime helpers do the same), and naming it off the temp counter keeps
	// two comprehensions in one function from colliding. A %s<N> name would read as a string
	// value to the print and call lowerings, which is a bug this backend has already had.
	g.tmp++
	nextReg := fmt.Sprintf("%%cc%d", g.tmp)
	idx := g.newTmp()
	// A filter puts the increment in the block the skips fall through to, so that block -- not
	// the body -- is the loop header's real predecessor. Naming the body anyway built a `phi`
	// whose entry list did not match its predecessors, which is why a runtime-list comprehension
	// with a string filter was refused rather than compiled: the refusal was covering an invalid
	// module, and the comparison bug underneath it (roadmap L11.8) has its own fix now
	// (Gap R.42, ADR 0224).
	backEdge := body
	var keep, skip string
	if c.Cond != nil {
		keep = g.newLabel("comp.keep")
		skip = g.newLabel("comp.skip")
		backEdge = skip
	}
	b.WriteString(fmt.Sprintf("  %s = phi i32 [ 0, %%%s ], [ %s, %%%s ]\n", idx, pre, nextReg, backEdge))
	cmp := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, %s\n  br i1 %s, label %%%s, label %%%s\n%s:\n", cmp, idx, nlen, cmp, body, done, body))
	iv := g.newTmp()
	pos := idx
	if iterIsDict {
		scaled := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = mul i32 %s, 2\n", scaled, idx))
		pos = scaled
	}
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_get_elem(i32 %s, i32 %s)\n", iv, src, pos))
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", iv, lv))
	if mixedIter {
		// The companion tag slot, in the loop body and not in the preheader, because it is the element
		// of *this* iteration that is being named. `for` over the same container binds it identically
		// (ADR 0185), and the two allocas are read as one pair by elemPayloadAndTag.
		if !g.allocd[lv+"_tag"] {
			b.WriteString(fmt.Sprintf("  %%_%s_tag = alloca i32\n", lv))
			g.allocd[lv+"_tag"] = true
		}
		lvt := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_tag_of(i32 %s, i32 %s)\n", lvt, src, pos))
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s_tag\n", lvt, lv))
		g.taggedVars[lv] = true
	}
	appendElem := func() error {
		if c.Kind == CompDict {
			// A dict entry is two (payload, tag) pairs, and a pair is only ever read or written as a
			// pair. When the key or value is the loop variable of a container whose slots mix kinds,
			// its kind lives in the object and nowhere else, so the pair comes from the two allocas —
			// the same door the list element goes through (ADR 0185, ADR 0244). Anything else keeps the
			// static answer it always had, including the message for a kind the compiler cannot name.
			dictPair := func(e Expr) (payload, tag string, interned, dynamic bool, err error) {
				if nm, isName := e.(*Name); isName {
					if p, t, ok := g.taggedLoopVarRead(b, nm.Value); ok {
						return p, t, false, true, nil
					}
					// The loop variable of a text container holds an index into @str_tab, and the tag has
					// to say so: writing it as the integer 0 makes the entry an int the lookup cannot
					// find, so `{k: 1 for k in d}` printed {0: 1} and `out["a"]` died with KeyError
					// while the interpreter and CPython both answered {'a': 1} (roadmap Gap R.78, ADR 0245).
					if c.ForVar != nil && nm.Value == c.ForVar.Value && g.internedVars[nm.Value] {
						p, perr := g.value(b, e)
						if perr != nil {
							return "", "", false, false, perr
						}
						return p, strconv.FormatInt(int64(TagStr), 10), true, false, nil
					}
				}
				p, verr := g.value(b, e)
				if verr != nil {
					return "", "", false, false, verr
				}
				t, ok := g.elemKindTag(e)
				if !ok {
					return "", "", false, false, fmt.Errorf("codegen: a dict entry whose key or value is not a kind the compiler can name needs a tagged value word (roadmap L11.1)")
				}
				_, intern, _ := g.heapElemKind(b, e)
				return p, strconv.FormatInt(int64(t), 10), intern, false, nil
			}
			kv, kt, kIntern, kDyn, kerr := dictPair(c.Keys[0])
			if kerr != nil {
				return kerr
			}
			vv, vt, vIntern, vDyn, verr := dictPair(c.Vals[0])
			if verr != nil {
				return verr
			}
			b.WriteString(fmt.Sprintf("  call void @rt_dict_put_tagged(i32 %s, i32 %s, i32 %s, i32 %s, i32 %s)\n", h, kv, vv, kt, vt))
			if kIntern {
				b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 2)\n", h))
			}
			if vIntern {
				b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 4)\n", h))
			}
			if kDyn || vDyn {
				// A slot written from a tag the compiler only carries, never reads, obliges the object
				// to let each slot speak for itself when it is printed (ADR 0232).
				b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 8)\n", h))
			}
			return nil
		}
		av, kt, intern, aerr := g.elemPayloadAndTag(b, c.Elems[0])
		if aerr != nil {
			return aerr
		}
		addTagged := "rt_append_tagged"
		if c.Kind == CompSet {
			addTagged = "rt_set_add_tagged"
		}
		// Payload and tag are one call, through the door `xs.append(v)` uses: no untagged add is left to
		// fall back to, so a slot can never carry the tag of whoever held it last (ADR 0187, ADR 0244).
		b.WriteString(fmt.Sprintf("  call void @%s(i32 %s, i32 %s, i32 %s)\n", addTagged, h, av, kt))
		if intern {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 1)\n", h))
		}
		// As above: a slot that needs its tag obliges the object to let each slot speak (ADR 0232). A
		// tagged loop variable always needs it — its kind is in the object and nowhere the compiler can
		// read it at this point in the program.
		if t, okTag := g.elemKindTag(c.Elems[0]); (okTag && slotTagSelfDescribing(t)) || g.compElemIsTaggedLoopVar(c) {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 8)\n", h))
		}
		return nil
	}
	if c.Cond != nil {
		tv := g.truthOperand(b, c.Cond)
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n%s:\n", tv, keep, skip, keep))
		if err := appendElem(); err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n", skip, skip))
	} else {
		if err := appendElem(); err != nil {
			return "", err
		}
	}
	b.WriteString(fmt.Sprintf("  %s = add i32 %s, 1\n  br label %%%s\n%s:\n", nextReg, idx, cond, done))
	if g.rtComps == nil {
		g.rtComps = map[*Comp]bool{}
	}
	g.rtComps[c] = true
	return h, nil
}

// comprehensionFolds reports whether the constant comprehension path can evaluate the element
// expression for every item. It runs the same fold the constant path will run, so the two can
// never disagree about what is foldable — a probe that lies would build a list at compile time
// and a list at runtime and call whichever failed an error.
func (g *irGen) comprehensionFolds(c *Comp, items []int64) bool {
	if g.constBindings == nil {
		g.constBindings = map[string]int64{}
	}
	if len(c.Elems) != 1 {
		return false
	}
	// `None` folds to the integer 0 — which is the right answer to `if None:` and the wrong answer to
	// \"what is in this slot\". An element that is not an integer belongs to the runtime builder, which
	// tags every slot it writes (roadmap Gap R.75, ADR 0244).
	if !foldsToAnInteger(c.Elems[0]) {
		return false
	}
	for _, item := range items {
		g.constBindings[c.ForVar.Value] = item
		_, ok := g.foldConstInt(c.Elems[0])
		if ok && c.Cond != nil {
			// The filter has to fold too: a condition that calls a function is exactly as
			// un-foldable as an element that does, and the constant path would refuse with
			// "comprehension condition must be constant".
			_, ok = g.foldConstInt(c.Cond)
		}
		delete(g.constBindings, c.ForVar.Value)
		if !ok {
			return false
		}
	}
	return true
}

// foldsToAnInteger is the comprehension fold's own question, asked before the shared folder answers
// the truthiness one: an element that is literally a None, a piece of text or a float is a *value* of
// that kind, not the integer its truthiness happens to fold to. Those elements go to the runtime
// builder, where the slot gets the tag that says what it holds (roadmap Gap R.75, ADR 0244).
func foldsToAnInteger(e Expr) bool {
	switch e.(type) {
	case *IntLit, *BoolLit:
		return true
	case *NoneLit, *StrLit, *FloatLit:
		return false
	}
	return true
}

// runtimeCompList builds a list comprehension whose element is not a constant: the element
// expression is evaluated once per item and appended to a heap list. The loop is unrolled, as
// everything else in this backend is — `for` over a literal gets one block per element too — so
// the iterable still has to be a shape the compiler can enumerate (a literal or range with
// constant bounds) even though the elements no longer have to be known.
//
// Binding the loop variable matters: it is a real slot the element expression reads, which is why
// [sq(x) for x in range(5)] can call sq at all (roadmap L11.7, ADR 0192).
func (g *irGen) runtimeCompList(b *strings.Builder, c *Comp, itemExprs []Expr) (string, error) {
	if len(c.Elems) != 1 {
		return "", fmt.Errorf("codegen: a runtime comprehension builds one element at a time (roadmap L11.1)")
	}
	if g.nestedContainerElem(c.Elems[0]) && !g.taggableNestedElem(c.Elems[0]) {
		return "", fmt.Errorf("codegen: %s", nestedContainerErr(c.Elems[0]).Error())
	}
	lv := loopVarName(c.ForVar)
	if !g.allocd[lv] {
		b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", lv))
		g.gcReg(b, lv)
		g.allocd[lv] = true
	}
	g.heapUsed = true // the comprehension is often the only heap allocation in the program
	// Same rule for the unrolled path: iterate ["a", "b"] and the loop variable holds indices.
	if ll, ok := c.Iter.(*ListLit); ok && len(ll.Elems) > 0 {
		if _, isStr := ll.Elems[0].(*StrLit); isStr {
			g.internedVars[loopVarName(c.ForVar)] = true
		}
	}
	g.heapSeq++
	h := fmt.Sprintf("%%h%d", g.heapSeq)
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_alloc(i32 1)\n", h))
	// Whether the elements are interned text is asked of the element itself, once per write, by the
	// door below — a pre-scan here emitted an intern call the loop never used.
	// appendElem writes the payload and the tag together, through the same door `xs.append(v)` uses
	// (ADR 0187, ADR 0244). An append that wrote only the payload leaves the new slot tagged as
	// whatever held it before, and an element compiled with `g.value` alone reaches a container slot as
	// the compiler's *global* (`@.lst1`) — a value position `llc` rejects, which is how a comprehension
	// over container literals ended the module instead of building the list (roadmap Gap R.75).
	appendElem := func() error {
		av, kt, intern, err := g.elemPayloadAndTag(b, c.Elems[0])
		if err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %s, i32 %s, i32 %s)\n", h, av, kt))
		if intern {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 1)\n", h))
		}
		// A slot whose payload only means something through its tag — a float box, None, another
		// container — obliges the whole object to stop claiming one element kind, or the printer renders
		// every slot through the number printer and `[[1, 2] for x in [1]]` prints the box handles
		// ([2, 3]) instead of the lists (ADR 0232, roadmap Gap R.75).
		if t, okTag := g.elemKindTag(c.Elems[0]); okTag && slotTagSelfDescribing(t) {
			b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %s, i32 8)\n", h))
		}
		return nil
	}
	for _, item := range itemExprs {
		iv, err := g.value(b, item)
		if err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", iv, lv))
		if c.Cond == nil {
			if err := appendElem(); err != nil {
				return "", err
			}
			continue
		}
		// The filter runs per item, in source order, with the loop variable bound — so
		// [x for x in xs if f(x)] filters with the same f the interpreter calls.
		condL := g.newLabel("comp.cond")
		itemL := g.newLabel("comp.item")
		skipL := g.newLabel("comp.skip")
		b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n", condL, condL))
		tv := g.truthOperand(b, c.Cond)
		b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n%s:\n", tv, itemL, skipL, itemL))
		if err := appendElem(); err != nil {
			return "", err
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n%s:\n", skipL, skipL))
	}
	if g.rtComps == nil {
		g.rtComps = map[*Comp]bool{}
	}
	g.rtComps[c] = true
	return h, nil
}

// compItems derives the items a comprehension walks at compile time: an inline list literal of
// constant integers, or range(start, stop[, step]) with constant bounds. Anything else — a
// container variable, a builtin that builds a list — has no compile-time length, and the caller
// answers it with the runtime loop (roadmap L11.7, ADR 0192).
func (g *irGen) compItems(c *Comp) ([]int64, []Expr, error) {
	var items []int64
	var itemExprs []Expr
	if ll, ok := c.Iter.(*ListLit); ok {
		for _, el := range ll.Elems {
			v, ok := g.foldConstInt(el)
			if !ok {
				return nil, nil, fmt.Errorf("codegen: comprehension iterable must be constant integers")
			}
			items = append(items, v)
			itemExprs = append(itemExprs, el)
		}
		return items, itemExprs, nil
	}
	if r, ok := c.Iter.(*Call); ok {
		// range(stop), range(start, stop) or range(start, stop, step)
		start := int64(0)
		stop, ok := g.foldConstInt(r.Args[0])
		if !ok {
			return nil, nil, fmt.Errorf("codegen: range bound must be a constant")
		}
		step := int64(1)
		if len(r.Args) > 1 {
			start = stop
			stop, ok = g.foldConstInt(r.Args[1])
			if !ok {
				return nil, nil, fmt.Errorf("codegen: range stop must be a constant")
			}
		}
		if len(r.Args) > 2 {
			step, ok = g.foldConstInt(r.Args[2])
			if !ok {
				return nil, nil, fmt.Errorf("codegen: range step must be a constant")
			}
		}
		if step > 0 {
			for v := start; v < stop; v += step {
				items = append(items, v)
				itemExprs = append(itemExprs, &IntLit{Value: v})
			}
		} else {
			for v := start; v > stop; v += step {
				items = append(items, v)
				itemExprs = append(itemExprs, &IntLit{Value: v})
			}
		}
		return items, itemExprs, nil
	}
	return nil, nil, fmt.Errorf("codegen: comprehension iterable must be an inline list literal, range(), or a container variable")
}

// foldConstUnder folds one operand of a comprehension with the loop variable bound to one item,
// clearing the binding again on the way out. Every fold a comprehension does goes through here, so
// a refused fold cannot leave the loop variable's value behind for the next statement to read, and
// the refusal names the operand that gave up (`element`, `condition`, `key`, `value`) instead of
// blaming the element for a filter that will not fold.
func (g *irGen) foldConstUnder(noun, forVar string, item int64, e Expr) (int64, error) {
	if g.constBindings == nil {
		g.constBindings = map[string]int64{}
	}
	g.constBindings[forVar] = item
	v, ok := g.foldConstInt(e)
	delete(g.constBindings, forVar)
	if !ok {
		return 0, fmt.Errorf("codegen: comprehension %s must be constant", noun)
	}
	return v, nil
}

// foldSetComp unrolls a set comprehension into the literal it means: the element and, when there
// is one, the filter are folded with the loop variable bound to each item, and a member already
// seen is dropped. The literal is what the binding rule stores into the heap, so a folded set
// comprehension and the equivalent `{1, 2}` literal build the very same object (roadmap Gap J.2,
// ADR 0234).
func (g *irGen) foldSetComp(c *Comp, items []int64) (*SetLit, []int64, error) {
	seen := map[int64]bool{}
	var vals []int64
	var elems []Expr
	for _, item := range items {
		v, err := g.foldConstUnder("element", c.ForVar.Value, item, c.Elems[0])
		if err != nil {
			return nil, nil, err
		}
		if c.Cond != nil {
			cv, cerr := g.foldConstUnder("condition", c.ForVar.Value, item, c.Cond)
			if cerr != nil {
				return nil, nil, cerr
			}
			if cv == 0 {
				continue
			}
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		vals = append(vals, v)
		elems = append(elems, &IntLit{Value: v})
	}
	return &SetLit{Elems: elems, Src: c.Src}, vals, nil
}

// foldDictComp is the same unrolling for a dict comprehension, keeping the folded keys beside the
// literal so `d[k]` on a folded comprehension can still be answered at codegen time.
func (g *irGen) foldDictComp(c *Comp, items []int64) (*DictLit, []int64, []int64, error) {
	var keys, vals []int64
	var kexprs, vexprs []Expr
	for _, item := range items {
		k, kerr := g.foldConstUnder("key", c.ForVar.Value, item, c.Keys[0])
		if kerr != nil {
			return nil, nil, nil, kerr
		}
		v, verr := g.foldConstUnder("value", c.ForVar.Value, item, c.Vals[0])
		if verr != nil {
			return nil, nil, nil, verr
		}
		if c.Cond != nil {
			cv, cerr := g.foldConstUnder("condition", c.ForVar.Value, item, c.Cond)
			if cerr != nil {
				return nil, nil, nil, cerr
			}
			if cv == 0 {
				continue
			}
		}
		keys = append(keys, k)
		vals = append(vals, v)
		kexprs = append(kexprs, &IntLit{Value: k})
		vexprs = append(vexprs, &IntLit{Value: v})
	}
	return &DictLit{Keys: kexprs, Vals: vexprs, Src: c.Src}, keys, vals, nil
}

// foldedContainerCompLiteral is the question the binding rule asks: is this right-hand side a
// set/dict comprehension that means a literal outright? When it is, the binding goes through the
// literal's own lowering, so a comprehension and the literal it folds to cannot grow apart — and
// when it is not (an element the folder cannot see, a container variable to walk), the answer is
// simply `false` and the runtime loop answers the program instead (roadmap Gap J.2, ADR 0234).
func (g *irGen) foldedContainerCompLiteral(e Expr) (Expr, bool) {
	c, ok := e.(*Comp)
	if !ok || (c.Kind != CompSet && c.Kind != CompDict) {
		return nil, false
	}
	if c.ForVar == nil || len(c.Elems) > 1 || len(c.Keys) > 1 || len(c.Vals) > 1 {
		return nil, false
	}
	items, _, err := g.compItems(c)
	if err != nil {
		return nil, false
	}
	switch c.Kind {
	case CompSet:
		if len(c.Elems) != 1 {
			return nil, false
		}
		if lit, _, ferr := g.foldSetComp(c, items); ferr == nil {
			return lit, true
		}
	case CompDict:
		if len(c.Keys) != 1 || len(c.Vals) != 1 {
			return nil, false
		}
		if lit, _, _, ferr := g.foldDictComp(c, items); ferr == nil {
			return lit, true
		}
	}
	return nil, false
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
	// A container variable as the iterable has no compile-time length, so this comprehension runs
	// as a real loop over the heap list. The check comes before the item derivation because that
	// derivation only understands literals and range(), and a variable is neither (roadmap L11.7,
	// ADR 0192).
	// A literal or range() iterable is enumerable at compile time, so the fold below keeps
	// priority — min/max/sum over a comprehension read its folded elements, and taking the
	// runtime path first would make those refuse. Only an iterable the compiler cannot enumerate
	// (a container variable) needs the real loop (roadmap L11.7, ADR 0192).
	iterStatic := false
	switch c.Iter.(type) {
	case *ListLit, *Call:
		iterStatic = true
	}
	iterKind := containerKindFromTy(exprTyName(c.Iter))
	if !iterStatic && (g.isContainerExpr(c.Iter) || iterKind == "list" ||
		(c.Kind != CompList && (iterKind == "set" || iterKind == "dict"))) {
		return g.runtimeCompLoop(b, c)
	}
	// Determine the iteration items: an inline integer list literal or range(n).
	items, itemExprs, ierr := g.compItems(c)
	if ierr != nil {
		return "", ierr
	}
	// The path below needs every element to fold to an integer. When an element is a call, a
	// string, or anything else the folder cannot see — the ordinary `[f(x) for x in range(5)]`
	// — the comprehension is built at runtime instead, with the same per-item unrolling this
	// backend already does for `for` loops (roadmap L11.7, ADR 0192).
	if c.Kind == CompList && !g.comprehensionFolds(c, items) {
		return g.runtimeCompList(b, c, itemExprs)
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
		sl, vals, serr := g.foldSetComp(c, items)
		if serr != nil {
			return "", serr
		}
		results = vals
		g.setIdx++
		name = fmt.Sprintf("@.set%d", g.setIdx)
		if g.staticSets == nil {
			g.staticSets = map[string]*SetLit{}
		}
		g.staticSets[name] = sl
		var parts []string
		for _, v := range results {
			parts = append(parts, fmt.Sprintf("i32 %d", v))
		}
		n := len(results)
		g.globals.WriteString(fmt.Sprintf("%s = private global {i32, [%d x i32]} { i32 %d, [%d x i32] [%s] }\n", name, n, n, n, strings.Join(parts, ", ")))
	case CompDict:
		dl, keys, vals, derr := g.foldDictComp(c, items)
		if derr != nil {
			return "", derr
		}
		results = vals
		g.dictIdx++
		name = fmt.Sprintf("@.dict%d", g.dictIdx)
		if g.staticDicts == nil {
			g.staticDicts = map[string]*DictLit{}
		}
		g.staticDicts[name] = dl
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
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_alloc(i32 %d)\n", h, HeapKindList))
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
					b.WriteString(fmt.Sprintf("  %s = call i32 @%s(", ret, irSymbol(mangle)))
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
					// Every argument is emitted BEFORE the call line is started: a value that
					// needs an instruction of its own (interning a string literal does) would
					// otherwise be written into the middle of the operand list (Gap R.42).
					argRegs := make([]string, len(c.Args))
					for i, arg := range c.Args {
						av, err := g.value(b, arg)
						if err != nil {
							return "", err
						}
						argRegs[i] = ", i32 " + av
					}
					ret := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %%self%s)\n", ret, fn, strings.Join(argRegs, "")))
					// A method is program code, and program code raises: without the
					// call-site check the exception stayed in flight, the method returned
					// its unwind value, and the caller carried on printing (Gap R.41, ADR 0223).
					g.checkExn(b)
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
				argRegs := make([]string, len(c.Args))
				for i, arg := range c.Args {
					av, err := g.value(b, arg)
					if err != nil {
						return "", err
					}
					argRegs[i] = ", i32 " + av
				}
				ret := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s%s)\n", ret, fn, recvHandle, strings.Join(argRegs, "")))
				g.checkExn(b) // a method call propagates like any other call (Gap R.41, ADR 0223)
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
				id int
				// cont is the block the exception check branches into; the join below has to be
				// entered from there, not from the call's own arm, once the call can raise (ADR 0223).
				fn, lab, ret, cont string
			}
			var cases []dynCase
			for _, cls := range g.classOrder {
				if fn, ok := g.resolveMethod(cls, mname); ok {
					cases = append(cases, dynCase{id: g.classIDs[cls], fn: fn, lab: g.newLabel("dyn.c"), ret: g.newTmp()})
				}
			}
			done := g.newLabel("dyn.done")
			miss := g.newLabel("dyn.miss")
			b.WriteString(fmt.Sprintf("  switch i32 %s, label %%%s [\n", cid, miss))
			for _, dc := range cases {
				b.WriteString(fmt.Sprintf("    i32 %d, label %%%s\n", dc.id, dc.lab))
			}
			b.WriteString("  ]\n")
			for i := range cases {
				dc := &cases[i]
				b.WriteString(fmt.Sprintf("%s:\n", dc.lab))
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s", dc.ret, dc.fn, h))
				for _, av := range argvals {
					b.WriteString(fmt.Sprintf(", i32 %s", av))
				}
				b.WriteString(")\n")
				// The check has to happen inside the arm, and the arm's `phi` incoming label
				// becomes the check's continuation -- a call site that raises cannot also be a
				// value producer for the join (Gap R.41, ADR 0223).
				dc.cont = g.checkExnLabel(b)
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
				b.WriteString(fmt.Sprintf(", [ %s, %%%s ]", dc.ret, dc.cont))
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
				if g.mixedSets[nm.Value] {
					// Growing a set whose members describe themselves has to keep that promise:
					// the member travels with its tag, and adding without one would make the new
					// member print as whatever kind its slot held last (ADR 0232).
					mv, mt, ok := g.taggedOperand(b, c.Args[0])
					if !ok {
						return "", fmt.Errorf("codegen: %s.%s(%s) adds a member whose kind the compiler cannot prove to a set whose members are of more than one kind; a float or container member needs the tagged value word (roadmap L11.1, ADR 0232)", nm.Value, attr.Name.Value, g.exprSummary(c.Args[0]))
					}
					g.heapSeq++
					hs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
					fn := "rt_set_add_tagged"
					if attr.Name.Value == "discard" {
						fn = "rt_set_discard_tagged"
					}
					b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 %s, i32 %s)\n", fn, hs, mv, mt))
					return "", nil
				}
				av, interned, err := g.heapElemKind(b, c.Args[0])
				if err != nil {
					return "", err
				}
				if g.printsAsInternedStr(c.Args[0]) {
					interned = true
				}
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%%s\n", hs, "_"+nm.Value))
				if err := g.recordElemKind(nm.Value, "set", interned, c.Args[0]); err != nil {
					// A set grown with a member of the other kind is promoted, not refused, for
					// the same reason an appended element is (ADR 0232).
					if !g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "set", c.Args[0]) {
						return "", err
					}
				}
				fn := "rt_set_add"
				if attr.Name.Value == "discard" {
					fn = "rt_set_discard"
				}
				// A member that is a float or None has no payload the set's compiled kind can render,
				// so the set stops claiming one — the same promotion an appended element gets
				// (roadmap L11.1, ADR 0233).
				if !g.mixedSets[nm.Value] {
					if mt, mok := g.elemKindTag(c.Args[0]); mok && slotTagSelfDescribing(mt) {
						if g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "set", c.Args[0]) {
							g.floatFmtUsed = g.floatFmtUsed || mt == int32(TagFloat)
						}
					}
				}
				if kt, ok := g.elemKindTag(c.Args[0]); ok && attr.Name.Value == "add" {
					b.WriteString(fmt.Sprintf("  call void @rt_set_add_tagged(i32 %%h%d, i32 %s, i32 %d)\n", hs, av, kt))
				} else {
					b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 %s)\n", fn, hs, av))
				}
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
		// xs.sort() and xs.reverse() mutate in place and return None. Neither existed in
		// either backend, and the compiled path diagnosed xs.sort() as a *string* method:
		// the call fell through to string-method dispatch and told the user their list was
		// a string (roadmap L11.7, ADR 0191).
		if nm, ok := attr.Obj.(*Name); ok && g.listVars[nm.Value] &&
			(attr.Name.Value == "sort" || attr.Name.Value == "reverse") {
			if len(c.Args) != 0 {
				return "", fmt.Errorf("%s() takes no arguments in this build — key= and reverse= need first-class functions (roadmap L11.7)", attr.Name.Value)
			}
			if attr.Name.Value == "sort" && g.mixedLists[nm.Value] {
				// Python raises TypeError for a str/int mix rather than inventing an order,
				// and ordering interned strings by payload would sort them by arrival.
				return "", fmt.Errorf("cannot sort a list whose elements are of more than one kind; Python raises TypeError here too, and the compiled backend reports it (roadmap L11.1, ADR 0191)")
			}
			g.heapSeq++
			hslot := g.heapSeq
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hslot, nm.Value))
			if attr.Name.Value == "sort" {
				mode := 0
				if g.listElemStr[nm.Value] {
					mode = 1
				}
				b.WriteString(fmt.Sprintf("  call void @rt_sort(i32 %%h%d, i32 %d)\n", hslot, mode))
			} else {
				b.WriteString(fmt.Sprintf("  call void @rt_reverse(i32 %%h%d)\n", hslot))
			}
			return "", nil
		}
		if nm, ok := attr.Obj.(*Name); ok && g.listVars[nm.Value] && attr.Name.Value == "append" {
			if len(c.Args) != 1 {
				return "", fmt.Errorf("append expects one argument")
			}
			if g.mixedLists[nm.Value] {
				// Appending to a tagged list writes the payload and the tag together:
				// rt_append alone would leave the new slot reading back with whatever tag the
				// freed slot last carried, which is how an appended string would print as its
				// interned index (roadmap L11.1, ADR 0187).
				if av, tag, terr := g.mixedElemTag(b, c.Args[0]); terr == nil {
					g.heapSeq++
					hs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
					b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, av, tag))
					return "", nil
				} else {
					return "", terr
				}
			}
			av, interned, err := g.heapElemKind(b, c.Args[0])
			if err != nil {
				return "", err
			}
			// The value is asked twice what it is: heapElemKind answers for the word it interned,
			// printsAsInternedStr for the value the source says it is, and `xs.append(d["a"])` is
			// the shape where only the second one knows (ADR 0232).
			if g.printsAsInternedStr(c.Args[0]) {
				interned = true
			}
			g.heapSeq++
			hs := g.heapSeq
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
			if err := g.recordElemKind(nm.Value, "list", interned, c.Args[0]); err != nil {
				// Growing a list with a value of the other kind used to be the refusal, because
				// the container records one kind and would print the new element through the old
				// one. The slots carry tags, so the honest move is to stop claiming a kind
				// (ADR 0232); the refusal stays for a value no tag can describe.
				if !g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "list", c.Args[0]) {
					return "", err
				}
			}
			// A float or None element is the case the container's one compiled kind cannot render:
			// its payload is a box handle or nothing at all, so printing the list through the number
			// printer shows 1 where Python shows 1.5. The slots already carry tags, so the honest
			// move is the one ADR 0232 made for strings — stop claiming a kind (roadmap L11.1, ADR 0233).
			if !g.mixedLists[nm.Value] {
				if t, ok := g.elemKindTag(c.Args[0]); ok && slotTagSelfDescribing(t) {
					if g.promoteMixed(b, fmt.Sprintf("%%h%d", hs), nm.Value, "list", c.Args[0]) {
						g.floatFmtUsed = g.floatFmtUsed || t == int32(TagFloat)
					}
				}
			}
			// Payload and tag are one operation, so an append cannot leave the new slot carrying
			// the tag of whoever held it last (ADR 0187, ADR 0189): there is no untagged append
			// left to fall back to.
			b.WriteString(fmt.Sprintf("  call void @rt_append_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, av, g.elemTagOperand(b, c.Args[0], interned)))
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
				} else if sv, ok := stringConst(c.Args[0], g.builtinShadowed); ok {
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
			// The receiver's name, when the receiver is one: `xs.append(2)` over a module list
			// is not a string method with a non-constant string, and saying so is a lie about
			// the program (Gap R.38).
			if nm, ok2 := attr.Obj.(*Name); ok2 {
				if err := g.moduleStateErr(nm.Value); err != nil {
					return "", err
				}
			}
			// upper() and lower() of a string the compiler cannot read ask the table
			// (ADR 0229). The compiled fold covers ASCII and copies other bytes unchanged;
			// the interpreter has the full case tables — the difference is recorded as a
			// limit of this cycle, not hidden (roadmap Gap R.47).
			if sreg, isStr, err2 := g.strReg(b, attr.Obj); err2 != nil {
				return "", err2
			} else if isStr {
				switch attr.Name.Value {
				case "upper":
					return g.rtStrCall(b, "rt_str_case", "i32 "+sreg, "i32 0"), nil
				case "lower":
					return g.rtStrCall(b, "rt_str_case", "i32 "+sreg, "i32 1"), nil
				case "strip":
					r := g.rtStrCall(b, "rt_str_strip", "i32 "+sreg)
					g.checkStrSentinels(b, r, "RuntimeError", strFullMessage, c.Span(), "ststrip")
					return r, nil
				}
			}
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
				// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
				return g.internStr(b, v), nil
			}
			spaces := strings.Repeat(" ", pad)
			if attr.Name.Value == "ljust" {
				// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
				return g.internStr(b, v+spaces), nil
			}
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, spaces+v), nil
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
				// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
				return g.internStr(b, v), nil
			}
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, strings.Repeat("0", pad)+v), nil
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
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, res), nil
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
			var sb strings.Builder
			col := 0
			for _, r := range v {
				if r == '\t' {
					n := w - (col % w)
					sb.WriteString(strings.Repeat(" ", n))
					col += n
				} else {
					sb.WriteRune(r)
					col++
				}
			}
			v = sb.String()
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, v), nil
		default:
			return "", fmt.Errorf("unsupported string method %s", attr.Name.Value)
		}
		// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
		return g.internStr(b, v), nil
	}

	// Class instantiation: `ClassName(args)`.
	if _, isClass := g.classInfos[fnName]; isClass {
		g.heapUsed = true
		h := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_alloc(i32 %d)\n", h, HeapKindInstance))
		// A heap slot is reused, and an instance inherits nothing from whoever held it last — not
		// even which attributes exist. The clear zeroes the presence row; the slot count is final
		// because every name the program can write was interned before emission (ADR 0235).
		b.WriteString(fmt.Sprintf("  call void @rt_inst_clear(i32 %s, i32 %d)\n", h, g.nextSlot))
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
			// A constructor that raises has failed to construct: the exception propagates
			// before the instance is ever used (Gap R.41, ADR 0223).
			g.checkExn(b)
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
		// NOT irSymbol: an `extern fn` is an FFI surface, and its link name is the C name
		// the program asked to bind — `declare i32 @strlen(i8*)` and the call to it must
		// both stay exactly `strlen` (roadmap Gap R.4). Only names the program *defines*
		// are prefixed.
		b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", t, ret, fnName, strings.Join(callArgs, ", ")))
		return t, nil
	}
	if g.curModName != "" && g.imports != nil {
		if sibling := g.imports.Funcs[g.curModName]; sibling != nil {
			if fd, ok := sibling[fnName]; ok && fd != nil {
				mangle := g.curModName + "$" + fnName
				ret := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(", ret, irSymbol(mangle)))
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
			// The decorated body is emitted through funcDef, which defines program-owned
			// names under irSymbolPrefix, so the reference must carry it too (Gap R.4).
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s_impl(%s)\n", t, irSymbol(fnName), strings.Join(vals, ", ")))
			g.checkExn(b)
		} else {
			if isFloat {
				b.WriteString(fmt.Sprintf("  %s = call double @%s(%s)\n", t, irSymbol(fnName), strings.Join(vals, ", ")))
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
					b.WriteString(fmt.Sprintf("  %s = call %s @%s(%s)\n", t, ret, irSymbol(fnName), strings.Join(callArgs, ", ")))
					return t, nil
				}
				b.WriteString(fmt.Sprintf("  %s = call i32 @%s(%s)\n", t, irSymbol(fnName), strings.Join(vals, ", ")))
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
			// print a constant string: literals and folded string-method results. This is a
			// context that wants the BYTES, so it takes the global pointer from strConst
			// directly -- value() now hands back an @str_tab index, which is the language's
			// string value and not something printf's %s may read (Gap R.42, ADR 0224).
			if txt, ok := g.stringVal(a); ok {
				fmtName, size := g.fmtStr("%s")
				v := g.strConst(txt)
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
							if fv == "" {
								return "", g.floatOperandRefusal(part.Expr)
							}
							g.floatFmtUsed = true
							fs := g.newTmp()
							b.WriteString(fmt.Sprintf("  %s = call i8* @rt_fmt_double(double %s)\n", fs, fv))
							operands = append(operands, "i8* "+fs)
						} else if _, knownStr := g.stringVal(part.Expr); knownStr || g.printsAsInternedStr(part.Expr) {
							// An interpolated string is an @str_tab index now, so printf must be
							// handed the bytes through rt_str_ptr -- `%d` printed the index and
							// `f"hi {n}"` came out as `hi 0` (Gap R.42, ADR 0224).
							fmtLit += "%s"
							vv, err := g.value(b, part.Expr)
							if err != nil {
								return "", err
							}
							sp := g.newTmp()
							b.WriteString(fmt.Sprintf("  %s = call i8* @rt_str_ptr(i32 %s)\n", sp, vv))
							operands = append(operands, "i8* "+sp)
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
			if nm, ok := a.(*Name); ok && g.taggedVars[nm.Value] {
				vt := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", vt, nm.Value))
				tg := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s_tag\n", tg, nm.Value))
				b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", vt, tg))
				continue
			}
			if ix, ok := a.(*Index); ok {
				// print(xs[i]) reads the element *and* its tag, so the printer dispatches on
				// what the slot holds instead of on what the compiler guessed: the same
				// (value, tag) pair a tagged variable carries, produced at the read site
				// (roadmap L11.1, ADR 0187).
				if listName, mixed := g.mixedIndexRead(ix); mixed {
					val, tag, err := g.mixedElemPair(b, listName, ix.Idx, ix.Span())
					if err != nil {
						return "", err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", val, tag))
					continue
				}
				if dictName, mixed := g.mixedDictIndexRead(ix); mixed {
					// The value's slot carries its own tag, and that is the only thing that can
					// tell "x" from the number 2 (ADR 0232).
					val, tag, err := g.mixedDictPair(b, dictName, ix.Idx, ix.Span())
					if err != nil {
						return "", err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", val, tag))
					continue
				}
				// print(xs[0][1]), print(d["a"][0]), print(m[0][1]): the container being indexed is
				// itself a read, so its kind is in the object and not in the spelling. The slot's
				// payload and tag are read together and handed to the one printer that can read a
				// tag, which renders a number, unboxes a float, looks up interned text and — for the
				// case that started this — prints a nested container as its own contents
				// (roadmap L11.1, ADR 0241).
				if val, tag, okRead, rerr := g.taggedContainerRead(b, ix); rerr != nil {
					return "", rerr
				} else if okRead {
					g.heapUsed = true
					g.floatFmtUsed = true
					b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", val, tag))
					continue
				}
			}
			// print([f(x) for x in xs]) — a comprehension is a container, so the runtime
			// printer renders it. The constant path used to printf the folded global
			// (@.lst1), which llc rejects outright: "global variable reference must have
			// pointer type" (roadmap L11.7, ADR 0192 — ADR 0188's rule one construct later).
			// The printer is the one for the container the comprehension means: a set comp is
			// rendered `{1, 2}` and a dict comp `{1: 2}`, not `[1, 2]`, which is what made
			// `print({x for x in [3, 1, 2]})` a question about the comprehension's kind and not
			// only about its elements (roadmap Gap J.2, ADR 0234).
			if comp, ok := a.(*Comp); ok {
				h, cerr := g.containerOperand(b, a)
				if cerr != nil {
					return "", cerr
				}
				printer := "rt_print_list_mixed"
				switch comp.Kind {
				case CompSet:
					printer = "rt_set_print"
				case CompDict:
					printer = "rt_dict_print"
				}
				b.WriteString(fmt.Sprintf("  call void @%s(i32 %s, i32 0)\n", printer, h))
				continue
			}
			// print(sorted(xs)) — that lowering hands back a runtime list handle, and only
			// the runtime printer can render it: the static path would printf the handle,
			// which is the invalid-IR shape ADR 0188 removed for literals (ADR 0191).
			if call, ok := a.(*Call); ok {
				// reversed("abc") is a string reversal and takes its own path; only a
				// container argument means this lowering produced a heap handle.
				if cname, ok := call.Fn.(*Name); ok && (cname.Value == "sorted" || cname.Value == "reversed") && g.isContainerExpr(call.Args[0]) {
					h, cerr := g.sortedRuntime(b, call)
					if cerr != nil {
						return "", cerr
					}
					b.WriteString(fmt.Sprintf("  call void @rt_print_list_mixed(i32 %s, i32 0)\n", h))
					continue
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
				if g.mixedLists[nm.Value] {
					printer = "rt_print_list_mixed"
				}
				b.WriteString(fmt.Sprintf("  call void @%s(i32 %%h%d, i32 0)\n", printer, hs))
				continue
			}
			// A loop variable unrolled from an inline list literal carries the tag its element was
			// built with. Printing the i32 alone showed a float box's handle and None's zero where
			// Python shows 1.5 and None; the tag travels with the element, and rt_print_mixed_value
			// is the one place that knows how to read it. Only the tags nothing else can render
			// take this branch — an integer and an interned string already have their answers from the
			// paths above, and routing them here too dragged the string runtime into a module for a
			// loop over plain numbers. A container element is a handle, and printing it without its
			// tag is how `for row in [[1, 2], [3, 4]]` printed 0 and 1 (roadmap L11.1, ADR 0233).
			if nm, ok := a.(*Name); ok && g.loopElemTag != nil && !g.listVars[nm.Value] && !g.taggedVars[nm.Value] {
				if tg, has := g.loopElemTag[nm.Value]; has && (tg == int32(TagFloat) || tg == int32(TagNone) ||
					tg == int32(TagList) || tg == int32(TagDict) || tg == int32(TagSet)) {
					vv := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", vv, nm.Value))
					b.WriteString(fmt.Sprintf("  call void @rt_print_mixed_value(i32 %s, i32 %d, i32 0)\n", vv, tg))
					if tg == int32(TagFloat) {
						g.floatFmtUsed = true
					}
					continue
				}
			}
			var t string
			if g.isFloat(a) {
				// Python renders a float as its shortest round-tripping text with a
				// ".0" when integral (rt_fmt_double); printf's %.17g invented digits
				// and %g truncated them, and neither marked 2.0 as a float.
				fmtName, size := g.fmtStr("%s")
				fv := g.floatValue(b, a)
				if fv == "" {
					// An empty operand here would reach printf as `rt_fmt_double(double )` and
					// llc would reject the module: refuse it at the front end instead (ADR 0166).
					return "", g.floatOperandRefusal(a)
				}
				g.floatFmtUsed = true
				fs := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i8* @rt_fmt_double(double %s)\n", fs, fv))
				t = g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i8* %s)\n", t, size, size, fmtName, fs))
			} else {
				// A container literal in print position is a *rendering* question, not a storage
				// question: build the runtime object and let the runtime printer render it. The old
				// gate was literalNeedsHeap — "does it contain a string?" — so an all-int literal
				// fell through to the static path and printed the elements-array global as a number:
				// `print([1, 2])` emitted `printf("%d\n", i32 @.lst1)`, which llc rejects outright
				// ("global variable reference must have pointer type"), and `print(set())` answered
				// `0`, the handle (roadmap Gap J.6, item (3)).
				if lit, ok := emptyContainerLiteral(a); ok {
					// `set()` / `list()` / `dict()` are containers the same way a literal is, and the
					// empty set has no literal spelling at all — so the constructor has to reach the
					// printer too, not just the literal (Gap K.3).
					a = lit
				}
				// print(xs[i]) with both parts literal folds to the element itself, and this branch
				// has to see that element: a folded container element that reaches the number path
				// emits printf("%d", i32 @.lst1), which llc refuses outright — the compiler rejecting
				// its own module for an ordinary program (roadmap L11.1, ADR 0166).
				if ix, isIx := a.(*Index); isIx {
					if base, isLit := ix.Obj.(*ListLit); isLit {
						if k, isConst := g.foldConstInt(ix.Idx); isConst && k >= 0 && k < int64(len(base.Elems)) {
							a = base.Elems[k]
						}
					}
				}
				if isContainerLiteral(a) {
					if _, isLL := a.(*ListLit); isLL && (literalMixedKinds(a) || literalNeedsTags(a)) && g.taggableMixedList(a.(*ListLit)) {
						// Heterogeneous list of taggable elements: build it with per-element
						// tags and print through the tag-aware printer, which is what the
						// interpreter's Repr does element by element (ADR 0184).
						g.heapUsed = true
						h, err := g.heapListFromTagged(b, a.(*ListLit))
						if err != nil {
							return "", err
						}
						b.WriteString(fmt.Sprintf("  call void @rt_print_list_mixed(i32 %s, i32 0)\n", h))
						continue
					}
					// A literal that mixes kinds is printed through the same dispatching
					// printers its variable-bound twin uses, whenever every slot can say what
					// it holds (ADR 0232); only a slot that cannot is the refusal.
					if (literalMixedKinds(a) || literalNeedsTags(a)) && !g.literalMixedIsTaggable(a) {
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
		if n, ok := stringConstLen(c.Args[0], g.builtinShadowed); ok {
			return fmt.Sprintf("%d", n), nil
		}
		// len of a string the compiler cannot read asks the table, and counts code points —
		// the same unit every other position question in this language is asked in
		// (ADR 0225, ADR 0229). Bytes would say 5 for "café" and be wrong about its own string.
		if _, foldable := g.stringVal(c.Args[0]); !foldable {
			if sreg, isStr, err := g.strReg(b, c.Args[0]); err != nil {
				return "", err
			} else if isStr {
				// Code points, not bytes: the same unit every other position question in this
				// language is asked in (ADR 0225). Bytes would call "café" 5 characters long.
				return g.rtStrCall(b, "rt_str_nchars", "i32 "+sreg), nil
			}
		}
		// imported string module global (data imports): len(mod.str)
		if attr, ok := c.Args[0].(*Attr); ok {
			if nm, ok2 := attr.Obj.(*Name); ok2 {
				if globals, ok3 := g.imports.Globals[nm.Value]; ok3 {
					if lit, ok4 := globals[attr.Name.Value]; ok4 {
						if str, ok5 := lit.(*StrLit); ok5 {
							return fmt.Sprintf("%d", len([]rune(str.Value))), nil // code points, ADR 0225
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
					return fmt.Sprintf("%d", len([]rune(sv))), nil
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
			if err := g.moduleStateErr(lit.Value); err != nil {
				return "", err
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
			if h, _, okH := g.containerHandleOf(b, c.Args[0]); okH {
				return g.heapLenOf(b, h), nil
			}
			if ix, isIx := c.Args[0].(*Index); isIx {
				// The slot belongs to a container the program built rather than spelled: its own tag
				// array says what it holds, so the length is asked of the object (ADR 0187, L11.1).
				if v, t, okPair := g.runtimeSlotPair(b, ix); okPair {
					return g.lenOfTaggedSlot(b, v, t, ix.Span()), nil
				}
			}
			return "", g.slotReadRefusal(c.Args[0], "len")
		default:
			// len(xs[0]), len(d["a"]), len(s[0]): the argument is a read rather than a spelling, so
			// its length is a word inside the object the slot names, and the question goes to the
			// runtime the same way a container variable's does. The tag is what licenses asking it:
			// a slot the compiler cannot see is a container is refused, because the length of whatever
			// object happens to share a number's index is not an answer (roadmap L11.1, ADR 0241).
			if h, _, okH := g.containerHandleOf(b, c.Args[0]); okH {
				return g.heapLenOf(b, h), nil
			}
			if ix, isIx := c.Args[0].(*Index); isIx {
				if v, t, okPair := g.runtimeSlotPair(b, ix); okPair {
					// The tag travels with the payload to the check, which is what lets a slot the
					// compiler never saw be measured as the text or container it turned out to hold —
					// and refuse to be measured as a number (roadmap L11.1, ADR 0241).
					return g.lenOfTaggedSlot(b, v, t, ix.Span()), nil
				}
			}
			return "", g.slotReadRefusal(c.Args[0], "len")
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
		for _, elem := range elems {
			if isContainerLiteral(elem) {
				// The fold would test the element's compile-time global for truth, which is a
				// global in an i32 slot: llc refuses it, and the exit-code contract calls that a
				// compiler bug for an ordinary program (ADR 0166). The interpreter, whose elements
				// are boxed values, answers `any([[1], [2]])` (roadmap L11.1).
				return "", fmt.Errorf("%s asks each element whether it is truthy, and %s is a container the compiled fold has no word for; the interpreter answers this program (roadmap L11.1)", fnName, exprSnippet(elem))
			}
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
			if g.rtComps[comp] {
				// The elements are computed at runtime, so there is no compile-time element
				// set to add up. Folding the empty one would answer 0 for a list that has
				// elements — a wrong answer wearing the costume of a refusal (ADR 0192).
				return "", fmt.Errorf("sum of a comprehension whose elements are computed at runtime needs a runtime reduction (roadmap L11.7, ADR 0192)")
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
		for _, elem := range elems {
			if isContainerLiteral(elem) {
				// The fold would add the element's globals; Python answers this program with
				// `unsupported operand type(s) for +: 'int' and 'list'`, and this backend has no
				// operator dispatch to raise either, so the honest answer is the refusal
				// (roadmap L11.1, ADR 0166).
				return "", fmt.Errorf("sum adds numbers, and %s is a container: there is no numeric answer to give (Python raises TypeError for this program)", exprSnippet(elem))
			}
		}
		anyFloat := false
		fvals := make([]float64, 0, len(elems))
		for _, elem := range elems {
			// An element is asked what it adds. Text adds nothing here: Python raises
			// `unsupported operand type(s) for +: 'int' and 'str'`, and this fold answering 0
			// for sum(["a"]) is a number nobody asked for (roadmap L11.1, ADR 0166).
			if isStringExpr(elem) || g.printsAsInternedStr(elem) {
				return "", fmt.Errorf("sum adds numbers, and %s is text: Python raises TypeError for this program, and the compiled fold has no number to answer with (roadmap L11.1)", exprSnippet(elem))
			}
			if g.isFloat(elem) {
				anyFloat = true
			}
			if fv, ok := g.floatEval(elem); ok {
				fvals = append(fvals, fv)
				continue
			}
			// An integer element of a sum that also holds a float contributes its value. The
			// fold used to collect only the float elements and then, finding the list "not all
			// float", added everything in the i32 domain — so sum([1, 2.5]) printed 3.0, the
			// 2.5 truncated on the way through the integer path (roadmap L11.1).
			if il, ok := elem.(*IntLit); ok {
				fvals = append(fvals, float64(il.Value))
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
			// Some element is computed at runtime and the answer is a float: there is no i32
			// answer to fall back to, and truncating each element on the way through one is
			// the wrong answer this fold used to give (roadmap L11.7, ADR 0192).
			return "", fmt.Errorf("sum of a list whose elements are computed at runtime, with a float among them, needs a runtime reduction (roadmap L11.7)")
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
			if g.rtComps[comp] {
				return "", fmt.Errorf("%s of a comprehension whose elements are computed at runtime needs a runtime reduction (roadmap L11.7, ADR 0192)", fnName)
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
		for _, elem := range elems {
			if isContainerLiteral(elem) {
				// The fold compares the elements with icmp on i32, and an element that is a
				// container is a compile-time global there — a global in an i32 slot, which llc
				// refuses, and the exit-code contract counts as a compiler bug for an ordinary
				// program (ADR 0166). Python compares lists element by element and answers this;
				// the interpreter, whose elements are boxed, agrees with it (roadmap L11.1).
				return "", fmt.Errorf("%s compares its elements, and %s is a container: the compiled fold has no word for comparing two containers, so it would compare globals (the interpreter answers this program; roadmap L11.1)", fnName, exprSnippet(elem))
			}
		}
		// When every element is a number the compiler can hold, the winner is decided here and
		// the answer is that element's own text. Comparing the i32 payloads instead truncated a
		// float on the way past g.value, so max([1, 2.5]) answered 2 (roadmap L11.1).
		if vals, floats, foldable := g.numericFoldElems(elems); foldable && len(vals) > 0 {
			best := 0
			for i := 1; i < len(vals); i++ {
				if (fnName == "min" && vals[i] < vals[best]) || (fnName == "max" && vals[i] > vals[best]) {
					best = i
				}
			}
			if floats[best] {
				// The winner is a float, so this is the wrong domain to be asked in: the caller
				// that prints or binds the value asks g.isFloat first, and that question answers
				// yes for exactly this shape. Reaching here means the two rules disagreed, and a
				// truncated integer is what the disagreement used to produce.
				return "", fmt.Errorf("%s of these elements is the float %s, which the integer fold cannot return; the float path answers this program (roadmap L11.1)", fnName, floatConst(vals[best]))
			}
			return fmt.Sprintf("%d", int64(vals[best])), nil
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
				// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
				return g.internStr(b, pyFloatRepr(fv)), nil
			}
		}
		// str(None) is "None" and str("x") is "x" — not the int 0 and not an error.
		// The two compile-time folders agree on these texts (stringConst, and
		// irGen.stringVal for print/len/concat); this is the third site that has to,
		// because print(str(None)) lowers here rather than through an assignment
		// (ADR 0183). A string constant prints with %s, so handing back the folded
		// global is correct in that position; storing one is not, which is why the
		// assignment path interns (see the AssignStmt string branch).
		if _, ok := c.Args[0].(*NoneLit); ok {
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, "None"), nil
		}
		if sl, ok := c.Args[0].(*StrLit); ok {
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, sl.Value), nil
		}
		v, err := g.value(b, c.Args[0])
		if err != nil {
			return "", err
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			// str() of a number the compiler cannot read is digits written at run time and
			// interned, so the answer is an ordinary string value (ADR 0229). It used to be
			// reachable only for a literal argument, which made str(42) work and str(get())
			// refuse — one rule, two behaviours.
			// A float must keep refusing: the compiled backend has no float in a word-sized
			// slot yet (L11.6, ADR 0226), and truncating one to render it would be the
			// silent-truncation bug that gap exists to keep dead.
			if !g.builtinShadowed("str") && len(c.Args) == 1 && !g.exprIsString(c.Args[0]) &&
				!strings.Contains(exprTyName(c.Args[0]), "float") {
				v, verr := g.value(b, c.Args[0])
				if verr != nil {
					return "", verr
				}
				r := g.rtStrCall(b, "rt_str_of_int", "i32 "+v)
				g.checkStrSentinels(b, r, "RuntimeError", strFullMessage, c.Span(), "stoint")
				return r, nil
			}
			return "", fmt.Errorf("str on non-integer")
		}
		// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
		return g.internStr(b, fmt.Sprintf("%d", n)), nil
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
			// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
			return g.internStr(b, reverseStr(lit.Value)), nil
		}
		// imported string module global (data imports): reversed(mod.str)
		if attr, ok := c.Args[0].(*Attr); ok {
			if nm, ok2 := attr.Obj.(*Name); ok2 {
				if globals, ok3 := g.imports.Globals[nm.Value]; ok3 {
					if lit2, ok4 := globals[attr.Name.Value]; ok4 {
						if str, ok5 := lit2.(*StrLit); ok5 {
							// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
							return g.internStr(b, reverseStr(str.Value)), nil
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
		// The ordinary cases first: sorted(xs) on a variable, and sorted([...]) with
		// anything but plain integers inside. The fold below is a fast path for constant
		// int literals; the runtime path copies and sorts, so sorted(xs) leaves xs in its
		// original order — the difference between the builtin and the method that a
		// program can see (roadmap L11.7, ADR 0191).
		if g.isContainerExpr(c.Args[0]) {
			foldable := false
			if lit, ok := c.Args[0].(*ListLit); ok {
				foldable = true
				for _, el := range lit.Elems {
					if _, isInt := el.(*IntLit); !isInt {
						foldable = false
					}
				}
			}
			if !foldable {
				return g.sortedRuntime(b, c)
			}
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
		// A string produced by a call is an @str_tab index, not the address of a global (Gap R.42, ADR 0224).
		return g.internStr(b, string(rune(cn))), nil
	case "ord":
		// ord(s) folds a constant string to the codepoint of its first byte,
		// mirroring the interpreter (int64(o.sval[0])).
		if len(c.Args) != 1 {
			return "", fmt.Errorf("ord expects one argument")
		}
		sv, ok := stringConst(c.Args[0], g.builtinShadowed)
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
								return fmt.Sprintf("%d", int([]rune(str.Value)[0])), nil
							}
						}
					}
				}
			}
		}
		if !ok {
			// ord of a string the compiler cannot read asks the table (ADR 0229). -1 means the
			// string is not exactly one code point, which is CPython's TypeError and, being a
			// raise, is catchable (ADR 0212).
			if sreg, isStr, err2 := g.strReg(b, c.Args[0]); err2 != nil {
				return "", err2
			} else if isStr {
				cp := g.rtStrCall(b, "rt_str_codepoint", "i32 "+sreg)
				g.checkStrSentinels(b, cp, "TypeError", "ord() expected a character", c.Span(), "ordcp")
				return cp, nil
			}
			return "", fmt.Errorf("ord: codegen folds only a constant string arg")
		}
		if len(sv) == 0 {
			return "", fmt.Errorf("ord of empty string")
		}
		// the first code point, not the first byte (ADR 0225)
		return fmt.Sprintf("%d", int64([]rune(sv)[0])), nil
	case "round":
		// round(x) folds a constant integer literal to itself (mirroring the
		// interpreter's int case; the AOT backend has no float representation).
		// A float goes to the nearest value with ties to EVEN: `llvm.roundeven.f64` is IEEE
		// roundTiesToEven and `math.RoundToEven` is the same operation on the host, so the constant
		// fold, the runtime call and the interpreter cannot drift the way `llvm.round.f64` and
		// `math.Round` did — both tied away from zero, both wrong where CPython ties to even.
		if len(c.Args) != 1 {
			return "", fmt.Errorf("round expects one argument")
		}
		if g.isFloat(c.Args[0]) {
			if fv, ok := g.floatEval(c.Args[0]); ok {
				return fmt.Sprintf("%d", int64(math.RoundToEven(fv))), nil
			}
			fx := g.floatValue(b, c.Args[0])
			rt := g.newTmp()
			fmt.Fprintf(b, "  %s = call double @llvm.roundeven.f64(double %s)\n", rt, fx)
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
		if sv, ok := stringConst(c.Args[0], g.builtinShadowed); ok {
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
	case *Attr:
		// `self.w = "hi"` in the class, `print(C().w)` outside it: the slot holds an
		// @str_tab index, so printing it must show the text (Gap R.42, ADR 0224).
		if cls := g.receiverClass(v.Obj); cls != "" {
			return g.strAttrs[cls+"."+v.Name.Value]
		}
		return false
	case *Call:
		// echo("yo") returns an index into @str_tab; printing it must show the text
		// (roadmap Gap J.5).
		if g.callReturnsStr(v) {
			return true
		}
		// A string method on a string receiver — `get()[1].upper()` — also prints as text. The
		// print path and the operation path ask this question of the same predicate, or one of
		// them renders the interned index as a number (ADR 0229).
		if at, ok := v.Fn.(*Attr); ok && len(v.Args) == 0 && g.exprIsString(at.Obj) {
			switch at.Name.Value {
			case "upper", "lower", "strip":
				return true
			}
		}
		// str(n) is text whatever n is, so printing it shows digits and not a count.
		if g.builtinCallAs(v, "str") {
			return true
		}
		return false
	case *BinOp:
		// `s[0] + s[2]` folds to text; every operand question is the same question.
		return g.exprIsString(v)
	case *Slice:
		// s[a:b] of a string is a string, constant bounds or not (ADR 0229) — the print path
		// asks the same question as the operation path, or a slice prints its index.
		if g.exprIsString(v.Obj) {
			return true
		}
		return false
	case *Index:
		// A subscript of a string is a one-character string (ADR 0225), so printing it is a
		// text question. Answering with %d printed the interned index: `print(s[1])` said `0`.
		// The base being a runtime string changes nothing about the answer's kind (ADR 0229).
		if g.exprIsString(v.Obj) {
			return true
		}
		if _, isStr := g.stringVal(v.Obj); isStr {
			return true
		}
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
	if g.boundFlags[nm] {
		// A name carrying a written-flag has a slot: emitBoundAllocas gave it one, precisely because
		// no store had to have run for it to be readable. Refusing here would be codegen blaming the
		// program for a name the body itself binds -- `def f(): print(v); v = 2` is a program, and
		// CPython runs it (raising UnboundLocalError at the read, which is what the flag does too).
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
// normalizeIndex turns a possibly-negative positional index into the offset the runtime
// understands and bounds-checks the *normalised* value, raising IndexError through the
// same path an explicit `raise` uses. One rule for read and write, shared with `pop` and
// slicing: a negative index counts from the end (roadmap L11.4, ADR 0210). Dict and set
// subscripts never come here — their index is a key, and `-1` is a key you can store.
func (g *irGen) normalizeIndex(b *strings.Builder, h, idx string, sp Span) string {
	ln := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_len(i32 %s)\n", ln, h))
	neg := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", neg, idx))
	g.markI1(neg)
	wrapped := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", wrapped, idx, ln))
	ix := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", ix, neg, wrapped, idx))
	hi := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp sge i32 %s, %s\n", hi, ix, ln))
	g.markI1(hi)
	lo := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", lo, ix))
	g.markI1(lo)
	bad := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", bad, lo, hi))
	g.markI1(bad)
	badL, okL := g.newLabel("ix.bad"), g.newLabel("ix.ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	g.raiseTo(b, exnCode("IndexError"), "IndexError", "index out of range", sp)
	b.WriteString(fmt.Sprintf("%s:\n", okL))
	g.heapUsed = true
	return ix
}

// trueDivMessage and floorDivMessage pick the wording CPython uses for the same operation, so
// the two tracebacks can be compared line for line. The distinction is not cosmetic: `7 / 0`
// divides two integers and the answer is "division by zero", even though this backend lowers
// int `/` to a double division (PEP 238) and the emitted instruction is a fdiv. Naming the
// instruction would describe our codegen at the expense of describing the program.
func (g *irGen) trueDivMessage(l, r Expr) string {
	if g.isFloat(l) || g.isFloat(r) {
		return "float division by zero"
	}
	return "division by zero"
}

func (g *irGen) floorDivMessage(l, r Expr) string {
	if g.isFloat(l) || g.isFloat(r) {
		return "float floor division by zero"
	}
	return "integer division or modulo by zero"
}

// guardNonZeroInt emits the ZeroDivisionError trap in front of an integer sdiv/srem. It goes
// through raiseTo rather than a runtime abort because the trap must be catchable:
// `except ZeroDivisionError:` matches on the exception code, and a program that handles a
// division has run to completion — the same distinction ADR 0211 made for exit statuses.
func (g *irGen) guardNonZeroInt(b *strings.Builder, divisor string, sp Span, kind string) {
	isZero := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", isZero, divisor))
	g.markI1(isZero)
	g.branchRaise(b, isZero, "ZeroDivisionError", kind, sp, "div")
}

// guardNonZeroFloat is the same trap for the double paths. `fdiv x, 0.0` is not an error to
// LLVM — it returns ±inf, which is how the compiled backend came to print `inf` for
// `print(1 / 0)` and exit 0 — so the test is ours to emit. A NaN divisor is not zero, exactly
// as in Python, and -0.0 compares equal to 0.0, which is what Python does too.
func (g *irGen) guardNonZeroFloat(b *strings.Builder, divisor string, sp Span, kind string) {
	isZero := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = fcmp oeq double %s, 0.000000e+00\n", isZero, divisor))
	g.markI1(isZero)
	g.branchRaise(b, isZero, "ZeroDivisionError", kind, sp, "fdiv")
}

// branchRaise emits `cond ? raise(class, kind) : continue` as its own pair of blocks, the
// shape every emitted bounds/member check already uses.
func (g *irGen) branchRaise(b *strings.Builder, cond, class, kind string, sp Span, tag string) {
	badL, okL := g.newLabel(tag+".bad"), g.newLabel(tag+".ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cond, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	g.raiseTo(b, exnCode(class), class, kind, sp)
	b.WriteString(fmt.Sprintf("%s:\n", okL))
}

// heapLenOf reads a container's length through the runtime, which is the only party that knows the
// object: the count is the same word for a list, a dict and a set, so one call answers all three.
func (g *irGen) heapLenOf(b *strings.Builder, h string) string {
	g.heapUsed = true
	v := g.newTmp()
	fmt.Fprintf(b, "  %s = call i32 @rt_heap_len(i32 %s)\n", v, h)
	return v
}

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

// mixedTaggedCompare answers `a == b` (and `!=`) when either side is a value whose kind lives in
// the object rather than in the compiler's notebook. Two shapes reach it:
//
//   - a variable that carries a runtime tag — a loop variable over a mixed container, or one
//     bound from a mixed read (ADR 0185, ADR 0187); and
//   - a *slot read*: an element of a container whose slots describe themselves, or a slot reached
//     through an index the program computes (roadmap L11.1, Gap R.79). Until now `print(out[1])`
//     asked the slot and rendered `a`, while `out[1] == "a"` refused — the tag was carried to the
//     printer and dropped at the comparison.
//
// Both sides become (payload, tag) pairs and `rt_payload_eq` answers, which is the one equality
// that knows what a payload means: within a tag by payload, across the numeric tags numerically,
// container slots by content. The other side must still be an expression whose kind the compiler
// can prove; when it cannot, this declines and the ordinary path reports whatever it reports
// (ADR 0232).
func (g *irGen) mixedTaggedCompare(b *strings.Builder, n *BinOp) (string, bool, error) {
	if n.Op != "==" && n.Op != "!=" {
		return "", false, nil
	}
	if !g.isTaggedCompareSide(n.L) && !g.isTaggedCompareSide(n.R) {
		return "", false, nil
	}
	// The read emits its own bounds check, whose IndexError branch terminates a block. A door that
	// changes its mind halfway through would leave the caller's block with instructions after a
	// terminator, and llc would report the compiler's mistake as the program's (ADR 0166). So the
	// whole door is built in a scratch buffer and committed only once both sides answer.
	var scratch strings.Builder
	lv, lt, lok := g.comparePair(&scratch, n.L)
	rv, rt, rok := g.comparePair(&scratch, n.R)
	if !lok || !rok {
		// One side's kind lives in the object, and the other side's cannot be proven at all. The
		// untagged compare that used to answer this compared two bare words, so `xs[0] == f()` came
		// out true whenever f handed back the text whose interned index happened to be the number in
		// the slot — an answer by coincidence (ADR 0232's collision, at a new site). Refuse by naming
		// the missing half instead. When *both* sides are reads, decline and let the ordinary path
		// report whatever it reports.
		if g.isTaggedCompareSide(n.L) && !rok && !g.isTaggedCompareSide(n.R) {
			return "", true, g.taggedCompareOperandErr(n.R, n.L)
		}
		if g.isTaggedCompareSide(n.R) && !lok && !g.isTaggedCompareSide(n.L) {
			return "", true, g.taggedCompareOperandErr(n.L, n.R)
		}
		return "", false, nil
	}
	cmp := g.newTmp()
	fmt.Fprintf(&scratch, "  %s = call i32 @rt_payload_eq(i32 %s, i32 %s, i32 %s, i32 %s)\n", cmp, lv, lt, rv, rt)
	res := g.newTmp()
	fmt.Fprintf(&scratch, "  %s = icmp ne i32 %s, 0\n", res, cmp)
	g.markI1(res)
	if n.Op == "!=" {
		inv := g.newTmp()
		fmt.Fprintf(&scratch, "  %s = xor i1 %s, true\n", inv, res)
		g.markI1(inv)
		res = inv
	}
	g.heapUsed = true
	// rt_payload_eq unboxes a float slot, so the float block has to be in the module even for a
	// program whose own literals are all integers.
	g.floatFmtUsed = true
	b.WriteString(scratch.String())
	return g.asBoolI32(b, res), true, nil
}

// mixedMembership lowers `x in s` / `k in d` where the container's slots carry tags. It answers
// (handled, error): handled is false when the container is uniform (or not a named container at
// all), and the untagged rt_contains path stays in charge — that path is sound for a container
// whose one kind the compiler has proved, and only for one (ADR 0232).
func (g *irGen) mixedMembership(b *strings.Builder, n *BinOp) (string, bool, error) {
	nm, ok := n.R.(*Name)
	if !ok || !g.allocd[nm.Value] {
		return "", false, nil // not a container variable the codegen built a slot for
	}
	// Which container, and which lookup agrees with its slot layout. A uniform container takes
	// the tagged call too whenever the needle's kind is provable: `1 in ["1"]` is false, and a
	// payload-only scan answers it true because "1" interned to the index 1 (ADR 0232).
	fn := ""
	switch {
	case g.mixedSets[nm.Value] || g.runtimeSets[nm.Value]:
		fn = "rt_set_contains_tagged"
	case g.mixedDicts[nm.Value] || g.runtimeDicts[nm.Value]:
		fn = "rt_dict_has_tagged"
	case g.mixedLists[nm.Value] || g.listVars[nm.Value]:
		fn = "rt_contains_tagged"
	default:
		return "", false, nil
	}
	mixed := g.mixedSets[nm.Value] || g.mixedDicts[nm.Value] || g.mixedLists[nm.Value]
	needle, tag, tok := g.taggedOperand(b, n.L)
	if !tok {
		if !mixed {
			return "", false, nil // the untagged scan keeps the answer it always gave
		}
		return "", false, fmt.Errorf("codegen: testing %s against the container %q whose slots describe themselves needs a needle whose kind the compiler can prove; a bool, float or container needle needs the tagged value word (roadmap L11.1, ADR 0232)", n.L, nm.Value)
	}
	h := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%%s\n", h, "_"+nm.Value))
	hit := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @%s(i32 %s, i32 %s, i32 %s)\n", hit, fn, h, needle, tag))
	cmp := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", cmp, hit))
	g.markI1(cmp)
	if n.Op == "not in" {
		inv := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = xor i1 %s, true\n", inv, cmp))
		g.markI1(inv)
		cmp = inv
	}
	return g.asBoolI32(b, cmp), true, nil
}

// checkKeyReadTagged is checkKeyRead for a dict whose keys are tagged: the same KeyError, raised
// by the same path, asked of the (payload, tag) key. Without the tag the check would answer "the
// key is there" for {0: 1} when asked for "0", and the raise below would never come.
func (g *irGen) checkKeyReadTagged(b *strings.Builder, h, key, keyTag string, sp Span) {
	ok := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_dict_has_tagged(i32 %s, i32 %s, i32 %s)\n", ok, h, key, keyTag))
	isZero := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", isZero, ok))
	g.markI1(isZero)
	badL, okL := g.newLabel("rd.bad"), g.newLabel("rd.ok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isZero, badL, okL))
	b.WriteString(fmt.Sprintf("%s:\n", badL))
	g.raiseTo(b, exnCode("KeyError"), "KeyError", "key not found", sp)
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
	// A raise that leaves an arm it already accepted, or that comes out of a `finally`, has
	// deferred bodies still owed to it: they run now, before the jump hands the exception to
	// the next handler, or they would never run at all (Gap R.23, ADR 0222). A raise from an
	// ordinary `try` body does not: this `try`'s own arms may still catch it, and they run
	// before its deferred body.
	if g.handledArms > 0 && !g.emittingDeferred {
		if err := g.runDeferredInnermost(b); err != nil {
			return err
		}
	}
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}
	return nil
}

// escapedTerminator reports whether the text a deferred statement emitted left the block for
// good: a `ret`, an `unreachable`, or a branch toward this function's raise-exit or a try
// handler. An `if` or a loop ends its block with an ordinary forward `br` too, and calling that
// an escape would silently stop the walk before the outer `finally` bodies -- so the test is
// about where control went, not about the opcode.
func (g *irGen) escapedTerminator(text string) bool {
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "unreachable" || strings.HasPrefix(ln, "ret ") {
			return true
		}
		if strings.HasPrefix(ln, "br ") && (strings.Contains(ln, "%"+g.funcRaiseExit) || strings.Contains(ln, "raiseexit")) {
			return true
		}
	}
	return false
}

// runDeferredLevel lowers one pending `finally` body -- the one at index i -- and reports whether
// it transferred out (its own `return` or raise), in which case nothing after it may be emitted.
// Levels below i are taken off the stack for the duration, so a `return` inside this body sees
// only the *outer* pending bodies and a raise in it cannot re-enter this level.
func (g *irGen) runDeferredLevel(b *strings.Builder, i int) (escaped bool, err error) {
	if i < 0 || i >= len(g.deferred) {
		return false, nil
	}
	level := g.deferred[i]
	saved := g.deferred
	g.deferred = saved[:i]
	g.emittingDeferred = true
	for _, st := range level {
		before := b.Len()
		if err := g.stmt(b, st); err != nil {
			g.emittingDeferred = false
			g.deferred = saved
			return false, err
		}
		if g.escapedTerminator(b.String()[before:]) {
			escaped = true
			break
		}
	}
	g.emittingDeferred = false
	g.deferred = saved
	return escaped, nil
}

// runDeferred lowers every pending `finally` body, innermost first: the walk a `return`,
// `break` or `continue` owes, because that transfer leaves all of the enclosing `try`
// statements at once and nothing else is going to run them (Gap R.23, ADR 0222).
func (g *irGen) runDeferred(b *strings.Builder) error {
	for i := len(g.deferred) - 1; i >= 0; i-- {
		escaped, err := g.runDeferredLevel(b, i)
		if err != nil {
			return err
		}
		if escaped {
			return nil
		}
	}
	return nil
}

// runDeferredInnermost lowers only the innermost pending `finally` body. This is what an
// exception leaving a `try` owes: the *outer* bodies are run by those statements themselves when
// the hand-off reaches their handler blocks, so running them here would run them twice -- once
// on the way out and once again on their own straight-line path.
func (g *irGen) runDeferredInnermost(b *strings.Builder) error {
	_, err := g.runDeferredLevel(b, len(g.deferred)-1)
	return err
}

// clearExn marks the pending exception as finished. A handled exception has to leave the
// flag behind cleared, because @exn_flag is one module-wide bit: anything still reading it
// takes the exception as still in flight. That is what happened — an arm ran, the program
// continued, and the next user-function call's check branched to the handler again (or to the
// raise-exit when no handler was in scope any more), reporting an exception the program had
// already handled. The interpreter cleared it in Gap R.21 (ADR 0213: "falling out of a `try`
// clears it"); this is the same rule on the compiled side (roadmap Gap R.21, compiled half).
func (g *irGen) clearExn(b *strings.Builder) {
	g.raiseUsed = true
	b.WriteString("  store i32 0, i32* @exn_flag\n")
}

// internStr emits the interning call that turns a compile-time-known string into its runtime
// representation, an index into @str_tab. Interning is content-addressed (rt_str_intern2 dedups by
// strcmp on the raw text), so index equality IS content equality: `x == "hi"` is an `icmp` once the
// operands are interned, which is what makes string comparison compilable at all (roadmap L11.8,
// Gap R.42, ADR 0224).
func (g *irGen) internStr(b *strings.Builder, txt string) string {
	g.heapUsed = true
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", t, g.strConst(txt), g.strConst(pyReprString(txt))))
	return t
}

// blockEndsInTerminator reports whether the block being built already ended in a terminator,
// in which case another instruction would be dead weight: an arm whose last statement is
// `return` or `raise` has already left, and it did so with the exception's own state.
func blockEndsInTerminator(b *strings.Builder) bool {
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		if strings.HasSuffix(ln, ":") {
			return false // a label: a fresh block, nothing emitted in it yet
		}
		return strings.HasPrefix(ln, "ret ") || strings.HasPrefix(ln, "br ") || ln == "unreachable"
	}
	return false
}

// checkExn emits a check of @exn_flag after a user-function call.
func (g *irGen) checkExn(b *strings.Builder) {
	g.checkExnLabel(b)
}

// checkExnLabel is checkExn with the continuation block's name, for a caller inside a `switch`
// arm that has to name that block in a `phi` incoming list afterwards.
func (g *irGen) checkExnLabel(b *strings.Builder) string {
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
	return cont
}

// tryStmt compiles a try/except/finally statement.
func (g *irGen) tryStmt(b *strings.Builder, ts *TryStmt) error {
	handler := g.newLabel("try.handler")
	finally := g.newLabel("try.finally")
	after := g.newLabel("try.after")
	g.handlerStack = append(g.handlerStack, handler)
	// This `try`'s deferred body is pending for the body and for every arm: a transfer out of
	// either one owes it a run (Gap R.23, ADR 0222). It is popped before the straight-line
	// `finally:` block below, which emits the body itself exactly once.
	if len(ts.Finally) > 0 {
		g.deferred = append(g.deferred, ts.Finally)
		defer func(saved [][]Stmt) { g.deferred = saved }(g.deferred[:len(g.deferred)-1])
	}
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
	// The arms are dispatched in source order, each comparing @exn_code, a bare `except:` and
	// `except Exception:` catching everything. Only the first arm used to be emitted at all, and
	// a non-matching exception had its flag cleared and fell through to the continuation — so a
	// handler that exists ran nothing, an unhandled exception vanished with no traceback and
	// exit 0, and a nested try never reached its outer arm (roadmap Gap R.20, ADR 0213).
	for _, ec := range ts.Excepts {
		armBody := g.newLabel("try.arm")
		notThis := g.newLabel("try.next")
		catchAll := ec.Exn == nil || ec.Exn.Value == "Exception"
		if !catchAll {
			code := g.newTmp()
			m := g.newTmp()
			b.WriteString("  " + code + " = load i32, i32* @exn_code\n")
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", m, code, exnCode(ec.Exn.Value)))
			g.markI1(m)
			b.WriteString("  br i1 " + m + ", label %" + armBody + ", label %" + notThis + "\n")
		} else {
			b.WriteString("  br label %" + armBody + "\n")
		}
		b.WriteString(armBody + ":\n")
		armed := false
		g.handledArms++
		for _, st := range ec.Body {
			if err := g.stmt(b, st); err != nil {
				g.handledArms--
				return err
			}
			if blockEndsInTerminator(b) {
				armed = true // this arm returned or raised: it left by its own path
				break
			}
		}
		g.handledArms--
		if !armed {
			// The arm took the exception, so the exception is over.
			g.clearExn(b)
			b.WriteString("  br label %" + finally + "\n")
		}
		b.WriteString(notThis + ":\n")
	}
	// Nothing matched. The exception belongs to an enclosing scope now: restore the flag and
	// hand it to the next handler out, or to the function's raise-exit — the same destination an
	// explicit `raise` with no handler in sight uses.
	b.WriteString("  store i32 1, i32* @exn_flag\n")
	// Nobody in this statement caught it, so this `try` is being left by an exception: its
	// deferred body (and any outer one still pending) runs before the hand-off.
	if err := g.runDeferredInnermost(b); err != nil {
		return err
	}
	if len(g.handlerStack) > 0 {
		b.WriteString("  br label %" + g.handlerStack[len(g.handlerStack)-1] + "\n")
	} else {
		b.WriteString("  br label %" + g.funcRaiseExit + "\n")
	}

	b.WriteString(finally + ":\n")
	// Straight-line exit: the deferred body is emitted here, not from the stack, so the stack
	// is dropped first -- otherwise a `return` inside it would emit this body a second time.
	// A `try` with arms but no `finally` clause pushed nothing (the block above still runs, as
	// the single continuation label), so the pop belongs to the same guard the push does.
	if len(ts.Finally) > 0 {
		g.deferred = g.deferred[:len(g.deferred)-1]
	}
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
// irSymbolPrefix is the prefix on the link name of every function the program's own
// source defines; irSymbol applies it.
//
// An emitted function name is not a label, it is a symbol the linker resolves. A program
// that wrote
//
//	def sync():
//	    return 7
//	print(sync())
//
// was emitted as `define i32 @sync()`, and the call bound to libc's `sync()` instead: the
// interpreter printed `7`, the compiled binary printed libc's `0`, and nothing along the
// way complained (roadmap Gap R.4). `main`, `exit`, `printf`, `free`, `strlen`, `write`,
// `time` are the same accident — all names a program may choose deliberately — and the
// generated entry point is `@main` too, so a user `def main()` was a duplicate definition.
//
// Prefixing every program-defined function closes the whole class without a blocklist to
// keep current, and without refusing a name the program is free to choose. Names the
// program does not define keep theirs: the runtime helpers (`rt_*`), the C library
// (`printf`, `write`, `snprintf`), and the `extern fn` declarations an FFI surface must
// export under the C name (see emitExterns / the `declare` path).
const irSymbolPrefix = "gy_"

// irSymbol is the link name a program-defined function is emitted and called under. It is
// idempotent because a symbol is minted once and then travels through registries (a
// method's symbol is stored in its class index), and both ends must agree.
func irSymbol(name string) string {
	if name == "" || strings.HasPrefix(name, irSymbolPrefix) {
		return name
	}
	return irSymbolPrefix + name
}

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
	// A function body is not inside its caller's `except` arm, however it was reached — a `def`
	// written inside an arm must not have its `return` clear the enclosing arm's exception.
	prevHandledArms := g.handledArms
	g.handledArms = 0
	defer func() { g.handledArms = prevHandledArms }()
	// Nor is it inside the caller's `try`: a `return` in a nested function body must not run the
	// enclosing statement's deferred bodies (Gap R.23, ADR 0222).
	prevDeferred := g.deferred
	g.deferred = nil
	defer func() { g.deferred = prevDeferred }()
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
	// What this body binds anywhere inside itself (so a name the module also binds stays local --
	// ADR 0220's rule applied where it is decidable), and whether this function is used as a
	// decorator (ADR 0227).
	doneBody := g.enterBody(fd.Body)
	defer doneBody()
	doneFlags := g.enterBoundFlags(fd, fd.Body)
	defer doneFlags()
	wasDecorator := g.emittingDecorator
	g.emittingDecorator = g.decoratorNames != nil && g.decoratorNames[fd.Name]
	defer func() { g.emittingDecorator = wasDecorator }()
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
	g.paramSlot = nil
	g.curFunc = g.fnName(fd)
	// A parameter the body rebinds to a float is a float variable, and the calling
	// convention does not know that yet: `def addf(x): x = x + 1.5; return x` returns a
	// bare name, so nothing reads the assignment, the function is emitted as
	// int-returning, and 1.0 arrives truncated to 1. Teaching it needs the tagged value
	// word, because the argument's type and the return's type are decided separately
	// today (roadmap L11.6, pinned as programs/probe_float_param_rebind).
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
	// The program's own functions carry the irSymbolPrefix; see irSymbol.
	g.dbgDefine(b, irSymbol(g.fnName(fd)), g.fnName(fd), fd.Src)
	fmt.Fprintf(b, "define %s @%s(", retTy, irSymbol(g.fnName(fd)))
	for i := range fd.Params {
		if i > 0 {
			fmt.Fprintf(b, ", ")
		}
		fmt.Fprintf(b, "%s %%p%d", paramTy, i)
	}
	fmt.Fprintf(b, ") {\n")
	// The written-flags for this body's possibly-unwritten locals, immediately inside the brace:
	// they must be instructions, not module-level text (ADR 0228).
	g.emitBoundAllocas(b)
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
			// The slot exists now, so the body's assignment to this name must reuse
			// it: without the registration the assignment emitted a second alloca of
			// the same name and llc called it "multiple definition of local value"
			// (Gap R.3c, ADR 0196).
			g.allocd[p.Name] = true
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
	// The call sites decide a parameter's kind, so this is the first moment the body's own
	// returns can be judged: `def f(s): return s[1]` called with a string hands back a string
	// index, and the caller has to read it as one (ADR 0229). Without this the value was
	// correct and the print was not — the index leaked out as a number.
	if !fd.Async && returnsStringExpr(g, fd) {
		g.strFuncs[g.fnName(fd)] = true
	}
	isGen := containsYield(fd.Body)
	if isGen {
		g.genIdx++
		g.genHandle = fmt.Sprintf("%%gh%d", g.genIdx)
		g.genFuncs[g.fnName(fd)] = true
		g.heapUsed = true
		fmt.Fprintf(b, "  %s = call i32 @rt_alloc(i32 %d)\n", g.genHandle, HeapKindList)
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
	// A parameter the body rebinds is a local that starts out bound to an argument:
	// copy it into a named slot now and read from there, or every read keeps answering
	// the argument however many times the body assigned (Gap R.3, ADR 0196).
	g.copyInReboundParams(b, fd, floatRet)
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
	g.paramSlot = nil
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
	if a == "true" || a == "1" {
		return c
	}
	if c == "true" || c == "1" {
		return a
	}
	// A pattern that cannot match is a constant, and `and i1 0, x` is not IR — the fold belongs in
	// the combinator, not in every caller (ADR 0235).
	if a == "false" || a == "0" || c == "false" || c == "0" {
		return "false"
	}
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", t, a, c))
	return t
}

func (g *irGen) orCond(b *strings.Builder, a, c string) string {
	if a == "false" || a == "0" {
		return c
	}
	if c == "false" || c == "0" {
		return a
	}
	if a == "true" || a == "1" || c == "true" || c == "1" {
		return "true"
	}
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
	// A capture binds, and bindPat is the only writer of that slot: the flag follows (ADR 0228).
	g.markBound(b, name)
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

// matchPattern answers "does this pattern match the subject?" with an i1 value, and binds the
// capture names it matched. The answer is a truth value in LLVM's own vocabulary: `true`/`false`
// fold in andCond/orCond instead of reaching `br i1` as an integer (ADR 0235).
func (g *irGen) matchPattern(b *strings.Builder, sub string, pat Expr) (string, error) {
	switch p := pat.(type) {
	case *Name:
		if p.Value == "_" {
			cmp := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cmp, sub, sub))
			return cmp, nil
		}
		// Bare-name capture pattern: bind the subject to a fresh variable slot and always match
		// (Python `case x:` semantics).
		g.bindPat(b, p.Value, sub)
		return "true", nil
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
		return cond, nil
	case *DictLit:
		cond := "true"
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
		return cond, nil
	case *Call:
		// Class pattern — `case Point(x, y):` or `case Alias(x, y):` — or an ordinary call compared
		// to the subject. Which class the callee names is the front end's question, asked once for
		// both backends (ADR 0235). The old code asked it twice and got it wrong twice: a name that
		// was not a declared class went down a "runtime alias" branch that read it with g.value and
		// DISCARDED the error, so `case f():` emitted `load i32, i32* %_f` for a variable that does
		// not exist and `case Alias(...)` inside a function emitted `icmp eq i32 %t5, ` with nothing
		// after the comma. Both were exit 2 (ADR 0211).
		fn, isName := p.Fn.(*Name)
		if !isName {
			return g.matchEquality(b, sub, pat)
		}
		class := g.classPat.classOfName(fn.Value)
		if class == "" && (g.classIDs[fn.Value] != 0 || g.classInfos[fn.Value] != nil) {
			class = fn.Value
		}
		if class != "" {
			return g.matchInstance(b, sub, class, "", p.Args)
		}
		// Not a class name. It may still name a value that holds one (`Alias = Point` written where
		// the generator cannot see it), which is the one form that has to be asked at run time — but
		// only if the name is readable and is not a function, and a refusal to read it falls through
		// to expression-equality rather than emitting a half-built compare.
		if !g.funcs[fn.Value] && !isPredeclaredName(fn.Value) {
			if cv, err := g.value(b, fn); err == nil {
				return g.matchInstance(b, sub, "", cv, p.Args)
			}
		}
		return g.matchEquality(b, sub, pat)
	default:
		return g.matchEquality(b, sub, pat)
	}
}

// matchEquality is where every non-structural pattern ends: evaluate the pattern expression and
// compare it to the subject. A pattern that cannot be evaluated matches nothing — the next case is
// tried, which is what the interpreter's `return false, nil` says for the same shape.
func (g *irGen) matchEquality(b *strings.Builder, sub string, pat Expr) (string, error) {
	pv, err := g.value(b, pat)
	if err != nil {
		return "false", nil
	}
	cmp := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cmp, sub, pv))
	return cmp, nil
}

// matchInstance lowers a class pattern. The subject must be an instance whose class is `class` (or a
// subclass of it) — or, when `class` is empty, whose class id equals the run-time value `classVal` —
// and every capture name must name an attribute the instance actually has. That last conjunct is the
// half that was missing: @heap's data words cannot tell an attribute that was never written from a
// stored 0, so `case Point(a, b):` matched an instance carrying neither and printed `pt 0 0` while
// the interpreter and the documentation both say the pattern fails (roadmap Gap B, ADR 0235).
func (g *irGen) matchInstance(b *strings.Builder, sub, class, classVal string, args []Expr) (string, error) {
	kind := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_heap_kind(i32 %s)\n", kind, sub))
	kc := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", kc, kind, HeapKindInstance))
	cond := kc
	cid := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 0)\n", cid, sub))
	if class != "" {
		cond = g.andCond(b, cond, g.classChainCond(b, cid, class))
	} else {
		cc := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cc, cid, classVal))
		cond = g.andCond(b, cond, cc)
	}
	for _, arg := range args {
		nm, ok := arg.(*Name)
		if !ok {
			// Only a name is a capture position; anything else is a sub-pattern this form cannot
			// match, and the interpreter fails the case the same way.
			return "false", nil
		}
		slot := g.attrSlot(nm.Value)
		has := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_has(i32 %s, i32 %d)\n", has, sub, slot))
		hc := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", hc, has))
		cond = g.andCond(b, cond, hc)
		ar := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 %d)\n", ar, sub, slot))
		g.bindPat(b, nm.Value, ar)
	}
	return cond, nil
}

func (g *irGen) stmt(b *strings.Builder, st Stmt) error {
	// Every statement is a position a debugger, a profiler or (Gap K.8) a traceback can be
	// asked about, so record where its IR starts before anything writes a line (L8.5, ADR 0231).
	g.dbgMark(b, st.Span())
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
		// A bare string expression as a statement has no effect, and evaluating it would now
		// intern it -- which keeps the literal's globals alive and unprunable for a program that
		// only mentions the text (the `--opt-level=1` dead-global assertion). Skipping it is the
		// same no-op it always was.
		if _, isLit := n.Expr.(*StrLit); !isLit {
			if _, err := g.value(b, n.Expr); err != nil {
				return err
			}
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
					g.markBound(b, nm.Value) // unpacking binds every element (ADR 0228)
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
			// A set/dict comprehension whose every operand folds means the literal it folds to, and a
			// container literal already has exactly one lowering here: allocate the heap object, write
			// every slot with its tag, register the variable's kind (ADR 0163's binding rule). Asking
			// the fold before the value is lowered is what makes `sa = {1, 2}` and
			// `sa = {x for x in [1, 2]}` build one and the same object, instead of the second spelling
			// storing the compile-time global's address — `store i32 @.set1, i32* %_sa`, the module llc
			// rejected and the exit-code contract called a compiler bug for an ordinary program
			// (roadmap Gap J.2, ADR 0234).
			rhs := n.Value
			if lit, folded := g.foldedContainerCompLiteral(n.Value); folded {
				rhs = lit
			}
			_, literal := rhs.(*ListLit)
			_, literalDict := rhs.(*DictLit)
			_, literalSet := rhs.(*SetLit)
			boundKind := containerKindFromTy(exprTyName(n.Value))
			// A comprehension says what it binds better than an inferred type can: the kind is written
			// in the syntax — `[..]`, `{..}` and `{k: v ..}` build a list, a set and a dict — and the
			// runtime loop has just put a handle of that kind in the slot. Without the record,
			// `print(sa)` printf'd the handle and answered `1` where both other engines answer `{2, 3}`
			// (roadmap Gap J.2, ADR 0234).
			if c, isComp := rhs.(*Comp); isComp && boundKind == "" {
				switch c.Kind {
				case CompList:
					boundKind = "list"
				case CompSet:
					boundKind = "set"
				case CompDict:
					boundKind = "dict"
				}
			}
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
			// `ys = [f(x) for x in ...]` / `sa = {x for x in xs}` / `da = {k: v for k in xs}` bind a container
			// the program builds at runtime. The variable needs its slot and its GC root *before* the
			// handle is stored: an unrooted handle is one collection away from a segfault, and this branch
			// is what keeps a comprehension result alive past the next statement (ADR 0192, ADR 0181; the
			// set and dict kinds join it with ADR 0234).
			// A comprehension whose operands all folded is not bound here: `rhs` carries the literal the
			// fold produced, and the container-literal paths below bind it, so `{x for x in [1, 2]}` and
			// `{1, 2}` are one code path (ADR 0234). This branch is the runtime one — the loop has just
			// built a handle, and it needs the slot, the root and the kind recorded for it.
			if comp, isComp := n.Value.(*Comp); isComp && rhs == n.Value {
				if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
					g.emitFreeOld(b, nm.Value)
				}
				g.listVars[nm.Value] = false
				g.runtimeDicts[nm.Value] = false
				g.runtimeSets[nm.Value] = false
				g.mixedLists[nm.Value] = false
				switch comp.Kind {
				case CompDict:
					g.runtimeDicts[nm.Value] = true
				case CompSet:
					g.runtimeSets[nm.Value] = true
				default:
					g.listVars[nm.Value] = true
					if len(comp.Elems) == 1 {
						if g.compElemIsTaggedLoopVar(comp) {
							// The element's kind is only in the object: the loop variable came out of a
							// container whose slots mix kinds, so nothing static says what landed in this
							// list. It has to let its slots speak (ADR 0232) and be able to print a float it
							// cannot see coming (roadmap Gap R.76, ADR 0244).
							g.mixedLists[nm.Value] = true
							g.listElemStr[nm.Value] = false
							g.floatFmtUsed = true
						} else if t, okTag := g.elemKindTag(comp.Elems[0]); okTag && slotTagSelfDescribing(t) {
							// The builder wrote a tag with every slot and told the object to let its slots
							// speak; the read has to ask them too, or `print(xs[0])` shows the float box's
							// handle where CPython shows 1.5, and a container element prints its handle
							// instead of the list inside (roadmap Gap R.46, Gap R.75, ADR 0244).
							g.mixedLists[nm.Value] = true
							g.floatFmtUsed = g.floatFmtUsed || t == int32(TagFloat)
						} else if g.compElemPrintsAsText(comp) {
							g.listElemStr[nm.Value] = true
						} else {
							g.listElemStr[nm.Value] = g.exprIsString(comp.Elems[0])
						}
					}
				}
				if !g.allocd[nm.Value] {
					b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
					g.gcReg(b, nm.Value)
					g.allocd[nm.Value] = true
				}
				h, herr := g.value(b, n.Value)
				if herr != nil {
					return herr
				}
				// The constant path hands back a folded global (@.lstN / @.setN / @.dictN), whose
				// layout is a length plus an array — storing that into an i32 slot is the module llc
				// refuses, so materialise it into the heap (ADR 0163's rule; Gap J.2, ADR 0234).
				if lit, folded := g.staticLists[h]; folded {
					hh, merr := g.heapListFrom(b, lit, "")
					if merr != nil {
						return merr
					}
					h = hh
				} else if lit, folded := g.staticSets[h]; folded {
					hh, merr := g.heapSetFrom(b, lit, "")
					if merr != nil {
						return merr
					}
					h = hh
				} else if lit, folded := g.staticDicts[h]; folded {
					hh, merr := g.heapDictFrom(b, lit, "")
					if merr != nil {
						return merr
					}
					h = hh
				}
				g.gcStoreHandle(b, h, nm.Value)
				delete(g.noneVars, nm.Value)
				return nil
			}
			// `v = xs[i]` out of a tagged list binds a (value, tag) pair, not a bare i32:
			// the slot means nothing without its companion, and print(v) dispatches on the
			// tag exactly as a loop variable over a mixed list does (ADR 0185, ADR 0187).
			if ix, isIndex := n.Value.(*Index); isIndex {
				if v, t, okRead, rerr := g.taggedContainerRead(b, ix); okRead && rerr == nil {
					// `y = xs[0][1]`, `y = d["a"][1]`, `y = t[0][0][0]` — binding a slot of a container that
					// is itself reached through a slot. Same rule as the two bindings below: the payload
					// means nothing without its tag, so both travel and print(y) dispatches on the tag
					// instead of guessing a kind (roadmap L11.1, ADR 0241).
					g.bindTaggedVar(b, nm.Value, v, t)
					return nil
				}
				if dictName, mixed := g.mixedDictIndexRead(ix); mixed {
					// `v = d[k]` out of a dict whose values mix kinds binds the pair too; the
					// block below this one is the list version of the same binding.
					val, tag, err := g.mixedDictPair(b, dictName, ix.Idx, ix.Span())
					if err != nil {
						return err
					}
					if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
						g.emitFreeOld(b, nm.Value)
						if g.allocd[nm.Value] {
							g.gcClearRoot(b, nm.Value)
						}
					}
					g.listVars[nm.Value] = false
					g.runtimeDicts[nm.Value] = false
					g.runtimeSets[nm.Value] = false
					g.mixedLists[nm.Value] = false
					delete(g.strVals, nm.Value)
					delete(g.internedVars, nm.Value)
					delete(g.noneVars, nm.Value)
					if g.floatVars != nil {
						delete(g.floatVars, nm.Value)
					}
					if !g.allocd[nm.Value] {
						b.WriteString(fmt.Sprintf("  %%%s = alloca i32\n", "_"+nm.Value))
						g.allocd[nm.Value] = true
					}
					if !g.allocd[nm.Value+"_tag"] {
						b.WriteString(fmt.Sprintf("  %%%s_tag = alloca i32\n", "_"+nm.Value))
						g.allocd[nm.Value+"_tag"] = true
					}
					b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", val, nm.Value))
					b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s_tag\n", tag, nm.Value))
					g.markBound(b, nm.Value)
					g.taggedVars[nm.Value] = true
					return nil
				}
				if listName, mixed := g.mixedIndexRead(ix); mixed {
					val, tag, err := g.mixedElemPair(b, listName, ix.Idx, ix.Span())
					if err != nil {
						return err
					}
					// Rebinding over a container or a tagged value: free the old heap slot and
					// mark the root dead, the way every other immediate binding does.
					if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
						g.emitFreeOld(b, nm.Value)
						if g.allocd[nm.Value] {
							g.gcClearRoot(b, nm.Value)
						}
					}
					g.listVars[nm.Value] = false
					g.runtimeDicts[nm.Value] = false
					g.runtimeSets[nm.Value] = false
					g.mixedLists[nm.Value] = false
					delete(g.strVals, nm.Value)
					delete(g.internedVars, nm.Value)
					delete(g.noneVars, nm.Value)
					if g.floatVars != nil {
						delete(g.floatVars, nm.Value)
					}
					if !g.allocd[nm.Value] {
						b.WriteString(fmt.Sprintf("  %%%s = alloca i32\n", "_"+nm.Value))
						g.allocd[nm.Value] = true
					}
					if !g.allocd[nm.Value+"_tag"] {
						b.WriteString(fmt.Sprintf("  %%%s_tag = alloca i32\n", "_"+nm.Value))
						g.allocd[nm.Value+"_tag"] = true
					}
					b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", val, nm.Value))
					b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s_tag\n", tag, nm.Value))
					g.markBound(b, nm.Value) // the tagged slot is written too (ADR 0228)
					g.taggedVars[nm.Value] = true
					return nil
				}
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
				b.WriteString(fmt.Sprintf("  %%h%d = call i32 @rt_alloc(i32 %d)\n", hs, HeapKindList))
				// A literal that mixes numbers with interned strings or None is no longer a
				// refusal: each slot carries a canonical ValueTag (heap_tags), and the
				// container prints through rt_print_list_mixed. Everything else that would
				// read an element out of it keeps refusing (ADR 0184).
				mixed := g.taggableMixedList(lit)
				for i, el := range lit.Elems {
					// heapElemKind, not value(): an assigned container literal is still a
					// runtime container, so a string element becomes an index into @str_tab
					// instead of the global pointer that LLVM rejects in an i32 parameter
					// (ADR 0166, roadmap Gap I.2).
					ev, interned, err := g.heapElemKind(b, el)
					if err != nil {
						return err
					}
					// `heapElemKind` reads the emitted shape, and an element that is a folded string
					// subscript (`xs = [s[1]]`) has no shape yet -- its interning is a call, not a
					// literal. The static question is the same question, and the tag below is what the
					// printer reads, so answering it only one way prints the index (ADR 0225).
					if !interned {
						interned = g.exprIsString(el)
					}
					// Every slot is tagged, not just the ones in a mixed list: rt_container_eq
					// compares (payload, tag) pairs, and a slot whose tag was never written holds
					// whatever the previous tenant of that heap slot left (ADR 0187, ADR 0189).
					tag := g.elemTagFor(el, interned)
					if tag == int32(TagNone) {
						// None has no i32 payload of its own; the tag is what renders it.
						ev = "0"
					}
					b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %%h%d, i32 %d, i32 %d)\n", hs, i, tag))
					if !mixed {
						if err := g.recordElemKind(nm.Value, "list", interned, el); err != nil {
							return err
						}
					}
					b.WriteString(fmt.Sprintf("  call void @rt_set_elem(i32 %%h%d, i32 %d, i32 %s)\n", hs, i, ev))
				}
				if mixed {
					g.mixedLists[nm.Value] = true
					delete(g.listElemStr, nm.Value)
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
			if sl, ok := rhs.(*SetLit); ok {
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
				// A set whose members describe themselves has no element kind to record: {1, "a"}
				// is neither the number set nor the string set (roadmap L11.1 (1b), ADR 0232).
				mixedSet := g.taggableMixedSet(sl)
				if mixedSet {
					g.mixedSets[nm.Value] = true
					g.setElemStr[nm.Value] = false
					g.setElemInt[nm.Value] = false
				}
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = call i32 @rt_alloc(i32 %d)\n", hs, HeapKindSet))
				si := 0
				for _, el := range sl.Elems {
					ev, interned, err := g.heapElemKind(b, el)
					if err != nil {
						return err
					}
					if mixedSet {
						// Adding and tagging are one call: a member added without its tag dedups
						// against the payload alone and prints through whatever the slot last held.
						t, _ := g.elemKindTag(el)
						b.WriteString(fmt.Sprintf("  call void @rt_set_add_tagged(i32 %%h%d, i32 %s, i32 %d)\n", hs, ev, t))
						si++
						continue
					}
					if err := g.recordElemKind(nm.Value, "set", interned, el); err != nil {
						return err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_set_add(i32 %%h%d, i32 %s)\n", hs, ev))
					b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %%h%d, i32 %d, i32 %d)\n", hs, si, g.elemTagFor(el, interned)))
					si++
				}
				if mixedSet {
					b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 8)\n", hs))
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
			if dl, ok := rhs.(*DictLit); ok {
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
				// The set above and heapDictFrom share this rule: a dict whose keys or values mix
				// kinds has no kind to record, so every slot carries its own tag (ADR 0232).
				mixedDict := g.taggableMixedDict(dl)
				if mixedDict {
					g.mixedDicts[nm.Value] = true
					g.dictKeyStr[nm.Value] = false
					g.dictKeyInt[nm.Value] = false
					g.dictValStr[nm.Value] = false
					g.dictValInt[nm.Value] = false
				}
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = call i32 @rt_alloc(i32 %d)\n", hs, HeapKindDict))
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
					if mixedDict {
						// Put and tag in one call: an entry stored without its key tag would match
						// another key with the same payload, and an entry without its value tag
						// would print the kind the slot held last time.
						kt, _ := g.elemKindTag(dl.Keys[i])
						vt, _ := g.elemKindTag(dl.Vals[i])
						b.WriteString(fmt.Sprintf("  call void @rt_dict_put_tagged(i32 %%h%d, i32 %s, i32 %s, i32 %d, i32 %d)\n", hs, kk, vv, kt, vt))
						continue
					}
					if err := g.recordElemKind(nm.Value, "dict key", kIsStr, dl.Keys[i]); err != nil {
						return err
					}
					if err := g.recordElemKind(nm.Value, "dict value", vIsStr, dl.Vals[i]); err != nil {
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
					b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %%h%d, i32 %d, i32 %d)\n", hs, i*2, g.elemTagFor(dl.Keys[i], kIsStr)))
					b.WriteString(fmt.Sprintf("  call void @rt_tag_elem(i32 %%h%d, i32 %d, i32 %d)\n", hs, i*2+1, g.elemTagFor(dl.Vals[i], vIsStr)))
					if bits != 0 {
						b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 %d)\n", hs, bits))
					}
				}
				if mixedDict {
					b.WriteString(fmt.Sprintf("  call void @rt_mark_estr(i32 %%h%d, i32 8)\n", hs))
				}
				g.gcStoreHandle(b, fmt.Sprintf("%%h%d", hs), nm.Value)
				return nil
			}
			if g.unionVars[nm.Value] {
				g.emitUnionStore(b, nm.Value, n.Value)
				return nil
			}
			if !g.inFunc {
				if sym, ok := g.moduleSlots[nm.Value]; ok {
					// The module's state lives in a module global, so a body called later reads what
					// the module assigned by then -- which is what "look the name up when the call
					// runs" means once a frame's allocas are gone (ADR 0220, implemented compiled-side
					// by ADR 0227). A main-frame alloca could not do that.
					b.WriteString(fmt.Sprintf("  store i32 %s, i32* @%s\n", v, sym))
					return nil
				}
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
				// sorted(...)/reversed(...) return a handle the same way — without this,
				// `ys = sorted(xs)` assigned a handle to a plain int variable and
				// print(ys) printed the slot number (roadmap L11.7, ADR 0191).
				if call, ok := n.Value.(*Call); ok {
					if fn, ok2 := call.Fn.(*Name); ok2 {
						if g.genFuncs[fn.Value] {
							g.listVars[nm.Value] = true
						} else if fn.Value == "sorted" || fn.Value == "reversed" || fn.Value == "list" {
							g.listVars[nm.Value] = true
							// Inherit what the elements are, so the printer and the sort
							// comparator agree with the source container.
							if len(call.Args) > 0 {
								if src, isName := call.Args[0].(*Name); isName {
									if g.listElemStr[src.Value] {
										g.listElemStr[nm.Value] = true
									}
									if g.mixedLists[src.Value] {
										g.mixedLists[nm.Value] = true
									}
								}
							}
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
				// Every write to a possibly-unwritten slot sets its flag; a read that follows any of
				// them is then legitimate (ADR 0228).
				g.markBound(b, nm.Value)
				if g.floatVars != nil {
					delete(g.floatVars, nm.Value)
				}
				delete(g.taggedVars, nm.Value)
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
		cond, cerr := g.truthyValue(b, n.Cond)
		if cerr != nil {
			return cerr
		}
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
				pc, perr := g.matchPattern(b, sub, p)
				if perr != nil {
					return perr
				}
				if orTmp == "" {
					orTmp = pc
				} else {
					orTmp = g.orCond(b, orTmp, pc)
				}
			}
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", orTmp, bodyL, fallL))
			b.WriteString(fmt.Sprintf("%s:\n", bodyL))
			if c.Guard != nil {
				gok, gerr := g.truthyValue(b, c.Guard)
				if gerr != nil {
					return gerr
				}
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
		cond, cerr := g.truthyValue(b, n.Cond)
		if cerr != nil {
			return cerr
		}
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
			loopSlot := loopVarName(n.Var)
			prevInterned, wasInterned := g.internedVars[loopSlot]
			for _, el := range ll.Elems {
				// Each element is lowered the way a heap container slot is, not the way a
				// scalar is: a string element becomes its @str_tab index. Handing `g.value`
				// a StrLit returned the raw `@.strN` global, so an unrolled loop over text
				// emitted `store i32 @.str1, i32* %_c` — a global in an i32 slot, which llc
				// rejects and the exit-code contract calls a compiler bug (roadmap Gap R.15,
				// ADR 0208; the same rule Gap I.2 installed for container writes).
				v, isStr, err := g.heapElemKind(b, el)
				if err != nil {
					return err
				}
				// The body copy emitted for *this* element then reads the loop variable as
				// text or as a number, which is what lets a mixed literal print correctly.
				if isStr {
					g.internedVars[loopSlot] = true
				} else {
					delete(g.internedVars, loopSlot)
				}
				// And the tag says what the element is, which is the question neither of those two
				// answers covers for a float (whose payload is a box handle) or None (whose payload
				// is nothing): printing the loop variable showed 0 where Python shows 1.5 and None
				// (roadmap L11.1, ADR 0233).
				if g.loopElemTag == nil {
					g.loopElemTag = map[string]int32{}
				}
				g.loopElemTag[loopSlot] = g.elemTagFor(el, isStr)
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
			// The loop variable stops existing as a loop variable here: restore whatever
			// the name meant before this loop, rather than leaving it interned for the
			// rest of the function.
			if wasInterned {
				g.internedVars[loopSlot] = prevInterned
			} else {
				delete(g.internedVars, loopSlot)
			}
			delete(g.loopElemTag, loopSlot) // the loop variable is a plain name again (ADR 0233)
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
		mixedIter := false
		var hVal string
		if name, ok := n.Iter.(*Name); ok {
			switch {
			case g.mixedLists[name.Value]:
				// Iterating a mixed list binds the loop variable to an element *and* its
				// tag, so print(x) can dispatch per iteration (ADR 0185).
				iterKind = "list"
				mixedIter = true
			case g.listVars[name.Value]:
				iterKind = "list"
			case g.mixedSets[name.Value]:
				// A mixed set binds payload and tag like a mixed list: the member's i32 is
				// ambiguous on its own, and the tag is what prints it (ADR 0232).
				iterKind = "set"
				mixedIter = true
			case g.mixedDicts[name.Value]:
				// A dict yields keys, so it is the key slots (stride 2) whose tags travel.
				iterKind = "dict"
				mixedIter = true
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
		} else if sl, ok := n.Iter.(*SetLit); ok {
			// `for v in {1, 2}` is a container the program wrote where a variable would stand: it is
			// built as an object and walked by the runtime, exactly like the variable form. Falling
			// through to the range path did two wrong things — it compared the counter against @.set1,
			// a global in an i32 slot, which llc refuses and the exit-code contract charges the
			// compiler with (ADR 0166), and it bound the loop variable to the counter, which would
			// have printed 0, 1 where CPython prints 1, 2 (roadmap L11.1).
			iterKind = "set"
			mixedIter = g.taggableMixedSet(sl)
		} else if dl, ok := n.Iter.(*DictLit); ok {
			// A dict literal yields its keys, in insertion order, like the dict it is (ADR 0188).
			iterKind = "dict"
			mixedIter = g.taggableMixedDict(dl)
		} else if h, kind, ok := g.containerHandleOf(b, n.Iter); ok {
			// `for v in xs[0]` — the iterable is a container the program can name, reached through a
			// slot rather than standing where a literal would. The slot's payload is the handle of the
			// inner object, and only its tag says so: containerHandleOf asks that question and refuses
			// when the slot holds a number pretending to be one. The loop then walks that object the way
			// it walks any list, and the tag travels with each element so print(v) dispatches per
			// iteration — text, float, None, a nested container (roadmap L11.1, ADR 0241).
			iterKind = kind
			mixedIter = true
			hVal = h
		}
		if iterKind != "" {
			// A loop over a container of interned strings binds the loop variable to an
			// index into @str_tab; recording that keeps print(x) rendering the text instead
			// of the index (roadmap Gap I.2). Dict iteration yields keys, so it is the key
			// kind that matters.
			if mixedIter {
				// The tag decides per iteration; a static claim here would print every
				// member through one kind, which is the bug the tag exists to prevent.
				delete(g.internedVars, loopVar)
				delete(g.noneVars, loopVar)
			}
			if name, ok := n.Iter.(*Name); ok {
				switch iterKind {
				case "list", "set":
					if !mixedIter && (g.listElemStr[name.Value] || g.setElemStr[name.Value]) {
						g.internedVars[loopVar] = true
					}
				case "dict":
					if g.dictKeyStr[name.Value] {
						g.internedVars[loopVar] = true
					}
				}
			} else if !mixedIter {
				// The same claim for a literal iterable, asked of its elements: a loop over
				// {"a", "b"} binds interned text, and printing the loop variable without that
				// records the interned index instead of the text (Gap I.2).
				var keys []Expr
				switch it := n.Iter.(type) {
				case *SetLit:
					keys = it.Elems
				case *DictLit:
					keys = it.Keys
				}
				if keys != nil && len(keys) > 0 {
					allText := true
					for _, k := range keys {
						// isStringExpr because a literal key is text the program wrote, and
						// printsAsInternedStr because a key expression can hand back an index too.
						if !isStringExpr(k) && !g.printsAsInternedStr(k) {
							allText = false
							break
						}
					}
					if allText {
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
				// The iterable is asked for its *handle*. g.value answers a literal with its folded
				// global, whose layout is a length plus an array, so `for v in {1, 2}` compared the
				// counter against @.set1 — a global in an i32 slot, which llc refuses and the
				// exit-code contract then charges the compiler with for an ordinary program (ADR 0166).
				// A container variable already holds a handle, which is why only the literal leg
				// broke. Anything the container rule does not cover — a generator call's result is
				// the live one — already carries a handle from its own lowering (ADR 0192).
				var hv string
				var err error
				if g.isContainerExpr(n.Iter) {
					hv, err = g.containerOperand(b, n.Iter)
				} else {
					hv, err = g.value(b, n.Iter)
				}
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
			if mixedIter {
				// The companion tag slot is what makes the loop variable printable: its
				// i32 alone is ambiguous (a number, or an index into the string table),
				// and only the tag says which (ADR 0185).
				if !g.allocd[loopVar+"_tag"] {
					b.WriteString(fmt.Sprintf("  %%_%s_tag = alloca i32\n", loopVar))
					g.allocd[loopVar+"_tag"] = true
				}
				tagT := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_tag_of(i32 %s, i32 %s)\n", tagT, hVal, pos))
				b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s_tag\n", tagT, loopVar))
				g.taggedVars[loopVar] = true
			}
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
		// Iterating text that only exists at run time is not implementable in this backend,
		// and the fall-through below would not refuse it: `def txt(): return "hi"` hands back
		// its @str_tab index, which the count path reads as a repeat count — so
		// `for c in txt(): print(c)` compiled cleanly and printed nothing, where the
		// interpreter and CPython print `h i`. A silent wrong answer is worse than a refusal,
		// so the shape is named instead (roadmap Gap R.16, ADR 0209). A string *literal*
		// iterates fine — it became a list of one-rune literals above (ADR 0208).
		if g.iterableIsRuntimeString(n.Iter) {
			// The refusal used to stand here because the fall-through read a string's table
			// index as a repeat count and printed nothing — a silent wrong answer. The table
			// now supplies the count and each character, so the loop is the counter loop with
			// a code-point index for an element (ADR 0229, ADR 0196).
			return g.emitForOverRuntimeString(b, n)
		}
		start, stop, step, err := g.rangeBounds(b, n.Iter)
		if err != nil {
			return err
		}
		if !g.allocd[loopVarName(n.Var)] {
			b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", loopVarName(n.Var)))
			g.gcReg(b, loopVarName(n.Var))
			g.allocd[loopVarName(n.Var)] = true
		}
		// The induction counter is the loop's own, never the loop variable's slot.
		// Sharing them made the variable answer the *bound* after the loop —
		// `for i in range(3): print(i)` and then `print(i)` gave 3 where Python and
		// the interpreter give 2 — and worse, an assignment to the loop variable in
		// the body moved the iteration, so `for i in range(3): i = i * 100` ran twice
		// and answered 101. The variable is bound from the counter at the top of each
		// body, which is when Python binds it (Gap R.3b, ADR 0196).
		g.forCtrSeq++
		ctr := fmt.Sprintf("_ctr%d", g.forCtrSeq)
		b.WriteString(fmt.Sprintf("  %%%s = alloca i32\n", ctr))
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%%s\n", start, ctr))
		b.WriteString(fmt.Sprintf("  br label %%%s\n", condL))
		b.WriteString(fmt.Sprintf("%s:\n", condL))
		g.ldN++
		cld := fmt.Sprintf("%%%s.ld%d", ctr, g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%%s\n", cld, ctr))
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
		// Python binds the loop variable to each element as the loop produces it, so
		// the store to the user's name happens here and nowhere else: what the body
		// writes to it is overwritten by the next element, and survives the loop only
		// as the last value actually bound.
		g.ldN++
		cbd := fmt.Sprintf("%%%s.ld%d", ctr, g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%%s\n", cbd, ctr))
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", cbd, loopVarName(n.Var)))
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
		ild := fmt.Sprintf("%%%s.ld%d", ctr, g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%%s\n", ild, ctr))
		itmp := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", itmp, ild, step))
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%%s\n", itmp, ctr))
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
			if pn, ok := g.params[c]; ok && !g.paramSlot[c] {
				val = pn
			} else {
				val = g.newTmp()
				fmt.Fprintf(b, "  %s = load i32, i32* %%_%s\n", val, c)
			}
			g.emitEnvStore(b, env, i, val)
		}
		fmt.Fprintf(b, "  store i32 %s, i32* @%s_slot\n", env, n.Name)
	case *ReturnStmt:
		if g.handledArms > 0 {
			// Returning out of an `except` arm ends the handler's work: the exception this
			// arm accepted must not survive the return, or the caller's next call-site check
			// hands it back as though nothing had handled it (Gap R.21, compiled half).
			g.clearExn(b)
		}
		if n.Expr == nil {
			// bare `return` yields None (ADR 0172). A float function reaching this is
			// a type error the checker reports; 0.0 keeps the module valid.
			if g.floatFuncs[g.curFunc] {
				if err := g.runDeferred(b); err != nil {
					return err
				}
				g.gcCloseFrame(b)
				b.WriteString("  ret double 0.000000\n")
				return nil
			}
			g.heapUsed = true
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_none()\n", t))
			if err := g.runDeferred(b); err != nil {
				return err
			}
			g.gcCloseFrame(b)
			b.WriteString(fmt.Sprintf("  ret i32 %s\n", t))
			return nil
		}
		if g.floatFuncs[g.curFunc] {
			fv := g.floatValue(b, n.Expr)
			if err := g.runDeferred(b); err != nil {
				return err
			}
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
				if err := g.runDeferred(b); err != nil {
					return err
				}
				g.gcCloseFrame(b)
				b.WriteString(fmt.Sprintf("  ret i32 %s\n", it))
				return nil
			}
		}
		v, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
		// The return value is computed by the `return` statement, and only then do the
		// deferred bodies run -- which is why `return n` in a `try` hands back the `n` from
		// before the `finally` reassigned it, as it does in Python (Gap R.23, ADR 0222).
		if err := g.runDeferred(b); err != nil {
			return err
		}
		g.gcCloseFrame(b)
		b.WriteString(fmt.Sprintf("  ret i32 %s\n", v))
	case *BreakStmt:
		if len(g.loopStack) == 0 {
			return fmt.Errorf("codegen: break outside loop")
		}
		info := g.loopStack[len(g.loopStack)-1]
		if g.handledArms > 0 {
			g.clearExn(b) // leaving an arm we already accepted the exception in
		}
		if err := g.runDeferred(b); err != nil {
			return err
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", info.breakLabel))
	case *ContinueStmt:
		if len(g.loopStack) == 0 {
			return fmt.Errorf("codegen: continue outside loop")
		}
		info := g.loopStack[len(g.loopStack)-1]
		if g.handledArms > 0 {
			g.clearExn(b)
		}
		if err := g.runDeferred(b); err != nil {
			return err
		}
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
