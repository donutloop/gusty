package lang

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// GenerateIR produces LLVM IR text for prog (deterministic, no native LLVM).
const heapRuntimeIR = `@heap_count = internal global i32 0
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

define internal void @rt_print_list(i32 %h) {
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
  br i1 %is0, label %first, label %sep
first:
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmti, i32 0, i32 0), i32 %e)
  br label %cont
sep:
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsep, i32 0, i32 0))
  call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmti, i32 0, i32 0), i32 %e)
  br label %cont
cont:
  %i1 = add i32 %i, 1
  br label %loop
done:
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtlclose, i32 0, i32 0))
  call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtnl, i32 0, i32 0))
  ret void
}

@.fmtdopen = private unnamed_addr constant [2 x i8] c"{\00"
@.fmtditem = private unnamed_addr constant [7 x i8] c"%d: %d\00"
@.fmtdsep = private unnamed_addr constant [3 x i8] c", \00"
@.fmtdclose = private unnamed_addr constant [3 x i8] c"}\0a\00"

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

define internal void @rt_dict_print(i32 %h) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  %r1 = call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtdopen, i32 0, i32 0))
  br label %check
check:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %done
body:
  %idx = mul i32 %i, 2
  %kp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx
  %k = load i32, i32* %kp
  %idx2 = add i32 %idx, 1
  %vp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %idx2
  %v = load i32, i32* %vp
  %r2 = call i32 (i8*, ...) @printf(i8* getelementptr ([7 x i8], [7 x i8]* @.fmtditem, i32 0, i32 0), i32 %k, i32 %v)
  %i1 = add i32 %i, 1
  %c1 = icmp slt i32 %i1, %len
  br i1 %c1, label %sep, label %cont
sep:
  %r3 = call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtdsep, i32 0, i32 0))
  br label %cont
cont:
  %next = phi i32 [ %i1, %body ], [ %i1, %sep ]
  br label %check
done:
  %r4 = call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtdclose, i32 0, i32 0))
  ret void
}


@.fmtsopen = private unnamed_addr constant [2 x i8] c"{\00"
@.fmtsitem = private unnamed_addr constant [3 x i8] c"%d\00"
@.fmtssep = private unnamed_addr constant [3 x i8] c", \00"
@.fmtsclose = private unnamed_addr constant [3 x i8] c"}\0a\00"

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

define internal i32 @rt_set_len(i32 %h) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  ret i32 %len
}

define internal void @rt_set_print(i32 %h) {
entry:
  %obj = getelementptr [1024 x {i32, i32, [256 x i32]}], [1024 x {i32, i32, [256 x i32]}]* @heap, i32 0, i32 %h
  %lp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 1
  %len = load i32, i32* %lp
  %dp = getelementptr {i32, i32, [256 x i32]}, {i32, i32, [256 x i32]}* %obj, i32 0, i32 2
  %r1 = call i32 (i8*, ...) @printf(i8* getelementptr ([2 x i8], [2 x i8]* @.fmtsopen, i32 0, i32 0))
  br label %check
check:
  %i = phi i32 [ 0, %entry ], [ %next, %cont ]
  %c = icmp slt i32 %i, %len
  br i1 %c, label %body, label %done
body:
  %kp = getelementptr [256 x i32], [256 x i32]* %dp, i32 0, i32 %i
  %ev = load i32, i32* %kp
  %r2 = call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsitem, i32 0, i32 0), i32 %ev)
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
  %r4 = call i32 (i8*, ...) @printf(i8* getelementptr ([3 x i8], [3 x i8]* @.fmtsclose, i32 0, i32 0))
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

define internal void @rt_gc([1024 x i32*]* %roots, i32 %nroots) {
entry:
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
  %rp = getelementptr [1024 x i32*], [1024 x i32*]* %roots, i32 0, i32 %j
  %rpp = load i32*, i32** %rp
  %rh = load i32, i32* %rpp
  call void @rt_gc_mark(i32 %rh)
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
  %wfree = icmp eq i8 %wmv, 0
  br i1 %wfree, label %sw.free, label %sw.inc
sw.free:
  %fhp = getelementptr [1024 x i32], [1024 x i32]* @free_next, i32 0, i32 %w
  %fhc = load i32, i32* @free_head
  store i32 %fhc, i32* %fhp
  store i32 %w, i32* @free_head
  br label %sw.inc
sw.inc:
  %w.nxt = add i32 %w, 1
  br label %sw.loop
done:
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

`

func GenerateIR(prog *Program) (string, error) {
	imports, err := resolveImports(prog)
	if err != nil {
		return "", err
	}
	g := &irGen{
		classIDs: map[string]int{}, nextSlot: 1,
		listVars:     map[string]bool{},
		runtimeDicts: map[string]bool{}, runtimeSets: map[string]bool{}, imports: imports, sym: map[string]string{}, allocd: map[string]bool{}, funcs: map[string]bool{}, genFuncs: map[string]bool{}, listOperands: map[string]bool{}, fds: map[string]*FuncDef{}, params: map[string]string{}, fmtIdx: 0, strIdx: 0, tmp: 0, ldN: 0}
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
	g.globals.WriteString("@exn_flag = internal global i32 0\n")
	g.globals.WriteString("@gc_roots_used = internal global i32 0\n")
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
	b.WriteString("define i32 @main() {\nentry:\n")
	for _, ap := range g.applyCalls {
		b.WriteString(fmt.Sprintf("  call void %s()\n", ap))
	}
	b.WriteString("  %gc.roots = alloca [1024 x i32*]\n")
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
	b.WriteString("  ret i32 0\n")
	b.WriteString("main.raiseexit:\n")
	b.WriteString("  ret i32 0\n}\n")
	// assemble output
	var out strings.Builder
	g.emitEnvGlobals()
	if g.heapUsed {
		g.globals.WriteString(heapRuntimeIR)
	}
	out.WriteString(g.strGlobals.String())
	out.WriteString(g.globals.String())
	out.WriteString(g.decls)
	out.WriteString(b.String())
	// PIC Level = 2 module flag: forces llc to emit position-independent code
	// so string constants in .rodata are referenced PIC-safely. Without it llc
	// defaults to the static relocation model, which emits 32-bit absolute
	// relocations (e.g. R_X86_64_32) that the default PIE link (cc) rejects.
	out.WriteString("!llvm.module.flags = !{!0}\n")
	out.WriteString("!0 = !{i32 2, !\"PIC Level\", i32 2}\n")
	return out.String(), nil
}

type irGen struct {
	globals   strings.Builder
	strGlobals strings.Builder // string constants, emitted at top of IR
	decls     string
	sym       map[string]string   // variable -> load temp
	allocd    map[string]bool     // alloca emitted?
	funcs     map[string]bool     // user-defined function names
	fds       map[string]*FuncDef // function definitions by name (for call arg binding)
	imports   *ImportInfo         // folded module globals for `import mod`
	params    map[string]string   // current function params: name -> register
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
	floatVars     map[string]bool
	listVars      map[string]bool
	runtimeDicts  map[string]bool
	runtimeSets   map[string]bool
	heapUsed      bool
	heapSeq       int
	handlerStack  []string
	funcRaiseExit string

	classInfos map[string]*classInfo // class name -> info
	classIDs   map[string]int   // class name -> runtime dispatch id
	classOrder  []string            // classes in id order (dispatch switch)
	varClasses map[string]string     // local var -> class name
	selfClass  string                // enclosing class of current self
	attrSlots  map[string]int        // attr name -> instance data slot
	nextSlot   int

	// genFuncs records generator function names; calling one yields a runtime
	// heap list handle (mirroring the interpreter's eager yield semantics).
	genFuncs map[string]bool
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
	// envSlots holds the module-global closure env slot names; each holds an
	// env heap handle and must be rooted so GC keeps captured envs alive.
	envSlots []string
}

// classInfo records a statically-known class: its base classes and its methods.
type classInfo struct {
	bases   []string
	methods map[string]string // method name -> IR function name
}

// registerClass records a class definition (ClassDef) and emits its methods.
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
	ci := &classInfo{bases: []string{}, methods: map[string]string{}}
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
	g.inFunc = true
	for _, st := range fd.Body {
		g.stmt(&g.globals, st)
	}
	g.inFunc = false
	g.globals.WriteString("  ret i32 0\n}\n")
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

func (g *irGen) gcReg(b *strings.Builder, name string) {
	if !g.inMain {
		return
	}
	if g.gcRootSeen == nil {
		g.gcRootSeen = map[string]bool{}
	}
	if g.gcRootSeen[name] {
		return
	}
	g.gcRootSeen[name] = true
	idx := g.gcRootIdx
	g.gcRootIdx++
	fmt.Fprintf(b, "  %%gc.slot%d = getelementptr [1024 x i32*], [1024 x i32*]* %%gc.roots, i32 0, i32 %d\n", idx, idx)
	fmt.Fprintf(b, "  store i32* %%_%s, i32** %%gc.slot%d\n", name, idx)
	fmt.Fprintf(b, "  store i32 %d, i32* @gc_roots_used\n", g.gcRootIdx)
}

func (g *irGen) gcRegGlobal(b *strings.Builder, name string) {
	// Root a module-global closure env slot: the env slot holds an i32 heap
	// handle (the closure env), so GC must mark it even when no main local
	// references it. Conservative: the slot may hold a scalar, which is just
	// treated as a potential heap address.
	slot := g.gcRootIdx
	g.gcRootIdx++
	fmt.Fprintf(b, "  %%gc.envSlot%d = getelementptr [1024 x i32*], [1024 x i32*]* %%gc.roots, i32 0, i32 %d\n", slot, slot)
	fmt.Fprintf(b, "  store i32* @%s_slot, i32** %%gc.envSlot%d\n", name, slot)
	fmt.Fprintf(b, "  store i32 %d, i32* @gc_roots_used\n", g.gcRootIdx)
}

func (g *irGen) gcCall(b *strings.Builder) {
	if !g.inMain || !g.heapUsed {
		return
	}
	ci := g.gcCallIdx
	g.gcCallIdx++
	fmt.Fprintf(b, "  %%gc.n%d = load i32, i32* @gc_roots_used\n", ci)
	fmt.Fprintf(b, "  call void @rt_gc([1024 x i32*]* %%gc.roots, i32 %%gc.n%d)\n", ci)
}

// strConst emits a global for a string literal operand.
func (g *irGen) strConst(s string) string {
	g.strIdx++
	name := fmt.Sprintf("@.str%d", g.strIdx)
	esc := strings.ReplaceAll(s, "\\", "\\\\")
	esc = strings.ReplaceAll(esc, "\n", "\\0A")
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
			return nil, fmt.Errorf("dict literal keys must be constant integers")
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
			return nil, fmt.Errorf("dict literal values must be constant integers")
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
	switch n := e.(type) {	case *Attr:
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
				return fmt.Sprintf("%g", fv), true
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
	f := fmt.Sprintf("%g", v)
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
		switch n.Op {
		case "+", "-", "*", "/", "%", "//":
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
	if g.isFloat(e) {
		t := g.newTmp()
		fmt.Fprintf(b, "  %s = fcmp one double %s, 0.0\n", t, g.floatValue(b, e))
		return t
	}
	return g.valueText(b, e)
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
	case *Name:
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
		ld := fmt.Sprintf("%%_%s.ld%d", n.Value, g.ldN)
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", ld, n.Value))
		return ld, nil
	case *Attr:
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
		if g.isFloat(n.L) || g.isFloat(n.R) {
			switch n.Op {
			case "==", "!=", "<", "<=", ">", ">=":
				return g.floatBinOp(b, n), nil
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
			if ri, rok := n.R.(*IntLit); rok {
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
			lt := g.newTmp()
			rt := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", lt, l))
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", rt, r))
			if n.Op == "and" {
				b.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", t, lt, rt))
			} else {
				b.WriteString(fmt.Sprintf("  %s = or i1 %s, %s\n", t, lt, rt))
			}
			res := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", res, t))
			return res, nil
		}
		// Membership tests need runtime container access, so handle them
		// separately from the i32 arithmetic/comparison ops.
		if n.Op == "in" || n.Op == "not in" {
			// l is the value to test, r is the container handle.
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("	%s = call i32 @rt_contains(i32 %s, i32 %s)\n", t, r, l))
			bt := g.newTmp()
			b.WriteString(fmt.Sprintf("	%s = icmp ne i32 %s, 0\n", bt, t))
			if n.Op == "not in" {
				nt := g.newTmp()
				b.WriteString(fmt.Sprintf("	%s = xor i1 %s, true\n", nt, bt))
				return nt, nil
			}
			return bt, nil
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
			b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", t, op, l, r))
		} else {
			b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", t, op, l, r))
		}
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
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, 0\n", t, x))
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
		name, err := g.emitList(n)
		if err != nil {
			return "", err
		}
		return name, nil
	case *DictLit:
		name, err := g.emitDict(n)
		if err != nil {
			return "", err
		}
		return name, nil
	case *SetLit:
		name, err := g.emitSet(n)
		if err != nil {
			return "", err
		}
		return name, nil
	case *Slice:
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
			// non-literal index: only runtime list vars support it (x[a]).
			if nm, ok := n.Obj.(*Name); ok {
				if !g.listVars[nm.Value] {
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
				b.WriteString(fmt.Sprintf("  %%g%d = call i32 @rt_dict_get(i32 %%h%d, i32 %d)\n", hs, hs, key))
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
		if lamName, ok := g.lambdas[fnName]; ok {
			fnName = lamName
		}
	}
	// constant-fold attr methods on constant receivers.
	if attr, ok := c.Fn.(*Attr); ok {
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
			cid := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 0)\n", cid, h))
			argvals := make([]string, len(c.Args))
			for i, a := range c.Args {
				v, _ := g.value(b, a)
				argvals[i] = v
			}
			type dynCase struct{ id int; fn, lab, ret string }
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
		if nm, ok := attr.Obj.(*Name); ok && g.listVars[nm.Value] && attr.Name.Value == "append" {
			if len(c.Args) != 1 {
				return "", fmt.Errorf("append expects one argument")
			}
			av, err := g.value(b, c.Args[0])
			if err != nil {
				return "", err
			}
			g.heapSeq++
			hs := g.heapSeq
			b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
			b.WriteString(fmt.Sprintf("  call void @rt_append(i32 %%h%d, i32 %s)\n", hs, av))
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

	if g.funcs[fnName] {
		fd := g.fds[fnName]
		if fd == nil {
			return "", fmt.Errorf("codegen: unknown function %q", fnName)
		}
		n := len(fd.Params)
		vals := make([]string, n)
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
				av, err := g.value(b, kw.Value)
				if err != nil {
					return "", err
				}
				vals[idx] = "i32 " + av
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
			av, err := g.value(b, a)
			if err != nil {
				return "", err
			}
			vals[pos] = "i32 " + av
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
			dv, err := g.value(b, fd.Params[i].Default)
			if err != nil {
				return "", err
			}
			vals[i] = "i32 " + dv
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
			b.WriteString(fmt.Sprintf("  %s = call i32 @%s(%s)\n", t, fnName, strings.Join(vals, ", ")))
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
		for _, a := range c.Args {
			if _, ok := a.(*KeywordArg); ok {
				return "", fmt.Errorf("codegen: %s does not accept keyword arguments", fnName)
			}
		}
		if len(c.Args) < 1 {
			// zero-argument print() matches the interpreter: writes nothing.
			return g.newTmp(), nil
		}
		// multi-argument print mirrors the interpreter: each argument is
		// written to stdout on its own line, one printf per argument.
		// String-literal arguments use a %s\n format (the interpreter prints
		// strings via Repr); integer arguments use %d\n.
		var last string
		for i, a := range c.Args {
			// print a constant string: literals and folded string-method results.
			if _, ok := g.stringVal(a); ok {
				fmtName, size := g.fmtStr("%s\n")
				v, err := g.value(b, a)
				if err != nil {
					return "", err
				}
				t := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i8* %s)\n", t, size, size, fmtName, v))
				last = t
				continue
			}
			if nm, ok := a.(*Name); ok {
				if g.runtimeDicts[nm.Value] {
					g.heapSeq++
					hs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
					b.WriteString(fmt.Sprintf("  call void @rt_dict_print(i32 %%h%d)\n", hs))
					continue
				}
				if g.runtimeSets[nm.Value] {
					g.heapSeq++
					hs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
					b.WriteString(fmt.Sprintf("  call void @rt_set_print(i32 %%h%d)\n", hs))
					continue
				}
			}
			if sl, ok := a.(*Slice); ok {
				if nm2, ok2 := sl.Obj.(*Name); ok2 {
					if _, isList := g.listVars[nm2.Value]; isList {
						h, err := g.value(b, sl)
						if err != nil {
							return "", err
						}
						fmt.Fprintf(b, "  call void @rt_print_list(i32 %s)\n", h)
						continue
					}
				}
			}
			if nm, ok := a.(*Name); ok && g.listVars[nm.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, nm.Value))
				b.WriteString(fmt.Sprintf("  call void @rt_print_list(i32 %%h%d)\n", hs))
				continue
			}
			var t string
			if g.isFloat(a) {
				fmtName, size := g.fmtStr("%.17g\n")
				fv := g.floatValue(b, a)
				t = g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), double %s)\n", t, size, size, fmtName, fv))
			} else {
				v, err := g.value(b, a)
				if err != nil {
					return "", err
				}
				// a generator result is a runtime heap list handle: print it
				// as a list rather than an int.
				if g.listOperands[v] {
					b.WriteString(fmt.Sprintf("  call void @rt_print_list(i32 %s)\n", v))
					continue
				}
				fmtName, size := g.fmtStr("%d\n")
				t := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([%d x i8], [%d x i8]* %s, i32 0, i32 0), i32 %s)\n", t, size, size, fmtName, v))
			}
			if i == len(c.Args)-1 {
				last = t
			}
		}
		return last, nil
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
			name, err := g.emitDict(lit)
			if err != nil {
				return "", err
			}
			n := len(lit.Keys)
			v := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32], [%d x i32]}, {i32, [%d x i32], [%d x i32]}* %s, i32 0, i32 0)\n", v, n, n, n, n, name))
			return v, nil
		case *SetLit:
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
				return g.strConst(fmt.Sprintf("%g", fv)), nil
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
		return "", fmt.Errorf("float: codegen folds only a constant int/string arg")
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
	switch name {
	case "ValueError":
		return 1
	case "TypeError":
		return 2
	case "KeyError":
		return 3
	case "IndexError":
		return 4
	case "RuntimeError":
		return 5
	case "StopIteration":
		return 6
	case "ZeroDivisionError":
		return 7
	default:
		return 0
	}
}

// raiseStmt compiles `raise Exception("msg")` / `raise ValueError("msg")`.
func (g *irGen) raiseStmt(b *strings.Builder, rs *RaiseStmt) error {
	code := 0
	if c, ok := rs.Expr.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 {
			code = exnCode(n.Value)
		}
	}
	b.WriteString("  store i32 1, i32* @exn_flag\n")
	b.WriteString(fmt.Sprintf("  store i32 %d, i32* @exn_code\n", code))
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

func (g *irGen) funcDef(b *strings.Builder, fd *FuncDef) error {
	prevRaise := g.funcRaiseExit
	prevHandlers := g.handlerStack
	g.handlerStack = nil
	g.funcRaiseExit = fd.Name + ".raiseexit"
	g.closures = map[string]*closureInfo{}
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
	fmt.Fprintf(b, "define i32 @%s(", fd.Name)
	for i := range fd.Params {
		if i > 0 {
			fmt.Fprintf(b, ", ")
		}
		fmt.Fprintf(b, "i32 %%p%d", i)
	}
	fmt.Fprintf(b, ") {\n")
	for i, p := range fd.Params {
		g.params[p.Name] = fmt.Sprintf("%%p%d", i)
	}
	isGen := containsYield(fd.Body)
	if isGen {
		g.genIdx++
		g.genHandle = fmt.Sprintf("%%gh%d", g.genIdx)
		g.genFuncs[fd.Name] = true
		g.heapUsed = true
		fmt.Fprintf(b, "  %s = call i32 @rt_alloc(i32 1)\n", g.genHandle)
	}
	for _, st := range fd.Body {
		if err := g.stmt(b, st); err != nil {
			return err
		}
	}
	if isGen {
		fmt.Fprintf(b, "  ret i32 %s\n", g.genHandle)
	} else {
		fmt.Fprintf(b, "  ret i32 0\n")
	}
	fmt.Fprintf(b, "%s:\n", g.funcRaiseExit)
	if isGen {
		fmt.Fprintf(b, "  ret i32 %s\n", g.genHandle)
	} else {
		fmt.Fprintf(b, "  ret i32 0\n")
	}
	fmt.Fprintf(b, "}\n")
	g.funcRaiseExit = prevRaise
	g.genHandle = ""
	g.handlerStack = prevHandlers
	g.params = map[string]string{}
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


func (g *irGen) stmt(b *strings.Builder, st Stmt) error {
	switch n := st.(type) {
	case *ImportStmt:
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
			for i, tgt := range tup.Elems {
				if nm, ok2 := tgt.(*Name); ok2 {
					if !g.allocd[nm.Value] {
						b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
						g.allocd[nm.Value] = true
					}
					v, err := g.value(b, valElems[i])
					if err != nil {
						return err
					}
					b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, nm.Value))
				}
			}
			return nil
		}
		if nm, ok := n.Target.(*Name); ok {
			// escape analysis: a list literal assigned to a variable that is
			// never read (dead) skips its heap allocation entirely.
			if _, isList := n.Value.(*ListLit); isList && !g.inFunc && g.deadLists[nm.Value] {
				return nil
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
					g.heapSeq++
					fs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%f%d = load i32, i32* %%_%s\n", fs, nm.Value))
					b.WriteString(fmt.Sprintf("  call void @rt_free(i32 %%f%d)\n", fs))
				}
				g.listVars[nm.Value] = true
				if !g.allocd[nm.Value] {
					b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", nm.Value))
					g.gcReg(b, nm.Value)
					g.allocd[nm.Value] = true
				}
				b.WriteString(fmt.Sprintf("  %%h%d = call i32 @rt_alloc(i32 1)\n", hs))
				for i, el := range lit.Elems {
					ev, err := g.value(b, el)
					if err != nil {
						return err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_set_elem(i32 %%h%d, i32 %d, i32 %s)\n", hs, i, ev))
				}
				b.WriteString(fmt.Sprintf("  store i32 %%h%d, i32* %%_%s\n", hs, nm.Value))
				return nil
			}
			// list var rebound to a non-list value: free its heap slot (GC-correctness).
			if g.listVars[nm.Value] || g.runtimeDicts[nm.Value] || g.runtimeSets[nm.Value] {
				g.heapSeq++
				fs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%f%d = load i32, i32* %%_%s\n", fs, nm.Value))
				b.WriteString(fmt.Sprintf("  call void @rt_free(i32 %%f%d)\n", fs))
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
					g.heapSeq++
					fs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%f%d = load i32, i32* %%_%s\n", fs, nm.Value))
					b.WriteString(fmt.Sprintf("  call void @rt_free(i32 %%f%d)\n", fs))
					g.listVars[nm.Value] = false
					g.runtimeDicts[nm.Value] = false
					g.runtimeSets[nm.Value] = false
				}
				g.runtimeSets[nm.Value] = true
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = call i32 @rt_alloc(i32 3)\n", hs))
				for _, el := range sl.Elems {
					ev, err := g.value(b, el)
					if err != nil {
						return err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_set_add(i32 %%h%d, i32 %s)\n", hs, ev))
				}
				b.WriteString(fmt.Sprintf("  store i32 %%h%d, i32* %%_%s\n", hs, nm.Value))
				return nil
			}
			v, err := g.value(b, n.Value)
			if err != nil {
				return err
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
					g.heapSeq++
					fs := g.heapSeq
					b.WriteString(fmt.Sprintf("  %%f%d = load i32, i32* %%_%s\n", fs, nm.Value))
					b.WriteString(fmt.Sprintf("  call void @rt_free(i32 %%f%d)\n", fs))
					g.listVars[nm.Value] = false
					g.runtimeDicts[nm.Value] = false
					g.runtimeSets[nm.Value] = false
				}
				g.runtimeDicts[nm.Value] = true
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = call i32 @rt_alloc(i32 2)\n", hs))
				for i := range dl.Keys {
					kk, err := g.value(b, dl.Keys[i])
					if err != nil {
						return err
					}
					vv, err := g.value(b, dl.Vals[i])
					if err != nil {
						return err
					}
					b.WriteString(fmt.Sprintf("  call void @rt_dict_put(i32 %%h%d, i32 %s, i32 %s)\n", hs, kk, vv))
				}
				b.WriteString(fmt.Sprintf("  store i32 %%h%d, i32* %%_%s\n", hs, nm.Value))
				return nil
			}
			if !g.allocd[nm.Value] {
				b.WriteString(fmt.Sprintf("  %%_%s = alloca double\n", nm.Value))
				g.allocd[nm.Value] = true
			}
			if g.isFloat(n.Value) {
				if !g.allocd[nm.Value] {
					b.WriteString(fmt.Sprintf("  %%_%s = alloca double\n", nm.Value))
					g.allocd[nm.Value] = true
				}
				fv := g.floatValue(b, n.Value)
				b.WriteString(fmt.Sprintf("  store double %s, double* %%_%s\n", fv, nm.Value))
				if g.floatVars == nil {
					g.floatVars = map[string]bool{}
				}
				g.floatVars[nm.Value] = true
			} else {
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
			ec, err := g.value(b, e.e.Cond)
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
			pat := sub
			var err error
			if name, ok := c.Pattern.(*Name); !ok || name.Value != "_" {
				// `case _:` wildcard: pat == sub makes the icmp always true.
				pat, err = g.value(b, c.Pattern)
				if err != nil {
					return err
				}
			}
			bodyL := g.newLabel("match.case")
			cmp := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cmp, sub, pat))
			var fallL string
			if i < len(n.Cases)-1 {
				fallL = g.newLabel("match.next")
			} else {
				fallL = endL
			}
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", cmp, bodyL, fallL))
			b.WriteString(fmt.Sprintf("%s:\n", bodyL))
			for _, s := range c.Body {
				if err := g.stmt(b, s); err != nil {
					return err
				}
			}
			b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
			if fallL != endL {
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
			b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", loopVarName(n.Var)))
			g.gcReg(b, loopVarName(n.Var))
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
		b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", loopVarName(n.Var)))
		g.gcReg(b, loopVarName(n.Var))
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
		v, err := g.value(b, n.Expr)
		if err != nil {
			return err
		}
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
	case *PassStmt:
		// no-op statement: emit nothing
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
	default:
		return fmt.Errorf("codegen: unsupported statement %T", st)
	}
	return nil
}