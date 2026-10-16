// The interned-text tables and the format constants the print doors share. One block of heapRuntimeIRTmpl.
package lang

// heapIRStringGlobals is one block of the arena's runtime block; heapRuntimeIRTmpl in codegen_ir_arena.go is
// the concatenation the emitter reads, in the order the module emits.
const heapIRStringGlobals = `
@.fmtlopen = private unnamed_addr constant [2 x i8] c"[\00"
@.fmtsep = private unnamed_addr constant [3 x i8] c", \00"
@.fmtlclose = private unnamed_addr constant [2 x i8] c"]\00"
; The words a dict view renders with -- dict_keys(['a']), not ['a'] -- so the distinction the
; reference draws between a view and a list has somewhere to live in the compiled module too
; (roadmap Gap R.182).
@.fmtdictkeys = private unnamed_addr constant [11 x i8] c"dict_keys(\00"
@.fmtdictvalues = private unnamed_addr constant [13 x i8] c"dict_values(\00"
@.fmtdictitems = private unnamed_addr constant [12 x i8] c"dict_items(\00"
@.fmtdictclose = private unnamed_addr constant [2 x i8] c")\00"
@.fmti = private unnamed_addr constant [3 x i8] c"%d\00"
@.fmtnl = private unnamed_addr constant [2 x i8] c"\0A\00"
@.fmtnone = private unnamed_addr constant [5 x i8] c"None\00"
@.fmttrue = private unnamed_addr constant [5 x i8] c"True\00"
@.fmtfalse = private unnamed_addr constant [6 x i8] c"False\00"
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
@estr = internal global [@HEAPSLOTS@ x i32] zeroinitializer
; A float box is a heap object of kind HeapKindFloat whose bits live here, indexed by handle.
; It is parallel to @heap rather than inside it for one reason: an element slot is one i32 word
; and a double does not fit in one, so a float that goes into a slot goes in as the handle of a
; box (roadmap L11.1, ADR 0233). The collector marks and sweeps by index rather than by kind, so a
; box is live exactly as long as the container that holds it and is recycled with it.
@float_box = internal global [@HEAPSLOTS@ x double] zeroinitializer
; @heap_tags is the per-element half of the value model (roadmap L11.1). @estr[h] says one
; thing about a whole container -- "its elements are interned strings" -- which is why a
; heterogeneous xs = [1, "a"] had to be refused rather than printed (ADR 0175). This array
; numbers each slot with a canonical ValueTag (int=0, None=3, str=4, from the table in
; value.go, so no new vocabulary), letting one list hold numbers and interned strings
; together. Only scalars and interned strings are taggable: a nested container would have to
; be marked by the collector, and that case stays refused.
@heap_tags = internal global [@HEAPSLOTS@ x [256 x i32]] zeroinitializer
; @inst_set is the instance half of the same idea (roadmap Gap B, ADR 0235): @inst_set[h][slot] is 1
; once something has been WRITTEN to that attribute slot of instance h. A class pattern binds capture
; names to attributes and a missing attribute is documented to FAIL the pattern, but @heap's data
; words cannot answer that -- an unwritten slot and a written zero look identical, so
; "case Point(a, b):" matched an instance that had no "a" and bound 0. rt_inst_clear zeroes the row
; when a heap slot becomes a new instance, because heap slots are reused and a stale 1 would be the
; previous tenant's answer.
@inst_set = internal global [@HEAPSLOTS@ x [256 x i32]] zeroinitializer
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
; that equal elements keep their relative positions — the compiled twin of the record's
; sortElems, and the reason sorted(key=) will be able to sit on top of it later. The heap element
; array is 256 deep, so the quadratic cost is bounded by the representation, not by luck.
define internal void @rt_sort(i32 %h, i32 %mode) {
entry:
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
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
  %ta = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %jm
  %tb = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %j
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
  %obj = getelementptr [@HEAPSLOTS@ x {i32, i32, [256 x i32]}], [@HEAPSLOTS@ x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
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
  %ta = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %i
  %tb = getelementptr [@HEAPSLOTS@ x [256 x i32]], [@HEAPSLOTS@ x [256 x i32]]* @heap_tags, i32 0, i32 %h, i32 %hi2
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
  %slot = getelementptr [@HEAPSLOTS@ x i32], [@HEAPSLOTS@ x i32]* @estr, i32 0, i32 %h
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
`
