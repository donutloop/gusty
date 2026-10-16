// The compiled root stack's IR: the globals rt_gc/rt_gc_report read, and the runtime block that pushes
// and pops handles. gcRootCap is the ceiling the root stack is declared with (ADR 0181).
package lang

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
; The arena's two reportable facts, kept beside the collector's counters rather than in the heap block:
; rt_gc_report prints capacity, headroom and the exhaustion count for every module it reports on,
; including one that never allocated, and the heap block is emitted on a narrower condition than the
; report is (roadmap Gap Q.2, ADR 0347).
@heap_live = internal global i32 0
@heap_exhausted = internal global i32 0
; The arena's third fact: how big the window this run has COMMITTED is. It starts at HeapInitialCapacity
; and doubles at the door that allocates until it reaches HeapCapacity, so capacity= is a fact about the
; run rather than a constant a program is measured against (roadmap Gap Q.2, ADR 0350). @heap_grown counts
; the widenings, which is the difference between "this arena grew" and "this arena was always that big" —
; a distinction a caller cannot recover from capacity alone.
@heap_cap = internal global i32 @HEAPINIT@
@heap_grown = internal global i32 0
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
  %buf = alloca [256 x i8]
  %c = load i32, i32* @gc.stat_collections
  %r = load i32, i32* @gc.stat_roots
  %s = load i32, i32* @gc.stat_skipped
  %m = load i32, i32* @gc.stat_marked
  %f = load i32, i32* @gc.stat_freed
  %t = load i32, i32* @gc.stat_total_freed
  %l = load i32, i32* @gc.stat_live
  ; The arena half: how big the window this run has committed is, how much of it is left, how many times
  ; it widened, and how many times it hit the wall. Headroom is the committed capacity minus the slots
  ; held, clamped at zero — a negative headroom would mean the occupancy counter drifted, and the honest
  ; reading of that is "none left", not a negative number of objects (roadmap Gap Q.2, ADR 0347, ADR 0350).
  ; grown= is the witness that capacity= is a run fact and not a constant: a run that never needed
  ; more than the window it started with reports the initial one and grown=0.
  %hlive = load i32, i32* @heap_live
  %hcap = load i32, i32* @heap_cap
  %hr0 = sub i32 %hcap, %hlive
  %hrneg = icmp slt i32 %hr0, 0
  %hr = select i1 %hrneg, i32 0, i32 %hr0
  %hex = load i32, i32* @heap_exhausted
  %hgrown = load i32, i32* @heap_grown
  %fmt = getelementptr [@FMTLEN@ x i8], [@FMTLEN@ x i8]* @.gcreport, i32 0, i32 0
  %n = call i32 (i8*, i32, i8*, ...) @snprintf(i8* %buf, i32 256, i8* %fmt, i32 %c, i32 %r, i32 %s, i32 %m, i32 %f, i32 %t, i32 %l, i32 %topmax, i32 %hcap, i32 %hr, i32 %hgrown, i32 %hex)
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
