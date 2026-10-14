package lang

import (
	"fmt"
	"strconv"
	"strings"
)

// The compiled heap is one fixed arena: `@heap` is `[HeapCapacity x {i32, i32, [256 x i32]}]`, and
// `rt_alloc` hands back an index into it — or, when the arena is full, `-1`. That -1 was the defect
// (roadmap Gap Q.2, ADR 0347): no caller asked, so a program with more live containers than the arena
// holds stored -1 as a handle and the object arithmetic that followed read and wrote the words *before*
// the arena. Measured, at exit 0, with nothing on stderr:
//
//	v0 = [0, 0] … v1029 = [1029, 1029] / r = r + len(v_i) … / print(r)
//	  compiled: 2120        CPython: 2060
//
// The wrong number is the whole argument for this file. A handle is not an integer: -1 is not "no
// object", it is the object below the objects.
//
// ## What ships here
//
// Two facts, kept apart on purpose:
//
//   - **rt_alloc counts exhaustion** (`@heap_exhausted`, reported by `--gc-stats` as `exhausted=`),
//     because the count is a fact about the run and the run is what the tool report describes.
//   - **the caller raises** — `emitHeapCall` emits, beside every call that hands back a handle, the
//     same shape `normalizeIndexFor` has used for bounds traps since ADR 0210 and `zeroStepGuardAtSlice`
//     for a zero step since ADR 0337: compare, branch to a raise block that goes to the innermost
//     handler (so the program's own `except MemoryError:` takes it) or out of the function, then the
//     continuation. A trap the program cannot name is not handleable (ADR 0214), and the only reason a
//     helper could raise instead is if it knew which handler was nearest — which is exactly what a
//     function with no idea who called it does not know.
//
// ## The road that is not a handle
//
// `rt_num_arith` allocates too — a double answer needs a box, and it takes it through `rt_float_new`
// inside the door — but its return value is a *status* and the box comes back through an out-param, so
// there is no handle here to compare against zero. That door therefore answers a fourth status (4, after
// its TypeError, OverflowError and ZeroDivisionError) and both of its callers raise the same
// `MemoryError` from `raiseHeapExhausted`, beside the status dispatch they already had. This is why the
// inventory test walks the module for **every** call that reaches the arena — handle roads and status
// roads — instead of trusting a list written by hand.
//
// ## Why every site, and not the ones that were hit in tests
//
// `rt_alloc` is called from inside `rt_list_copy`, `rt_float_new`, `rt_slice` and `rt_none` too, so a
// guard on the direct calls alone would leave four roads that smuggle -1 back through a helper's return
// value. Every one of those helpers is also a handle-producing call from codegen's point of view, so the
// guard is emitted beside the *codegen call*, whichever helper it is — see `handleReturningCalls`. A
// call whose result is a handle is a call that can fail, and there is no version of this where "we
// guarded the calls a test happened to reach" is a claim about the language.
//
// ## The negative control
//
// A program under the ceiling emits no raise block, no interned message and no reference to
// `MemoryError` at all (TestTheGuardIsNotEmittedWhereTheHeapNeverRunsOut): the guard is a branch on a
// value that a program which fits answers with a non-negative handle, so a program that fits is not
// paying for the program that does not — beyond two instructions and a block pair per allocation, which
// is what a check that is not statically decidable costs.
//
// ## What the run reports
//
// `--gc-stats` prints `capacity=`, `headroom=` and `exhausted=` beside the collector's own numbers
// (docs/operations.md § The tool report). Capacity is the backend's fact and headroom is capacity minus
// the slots the arena holds, so a caller can tell "this program is too big for this backend" from "this
// program is wrong" without knowing 1024 by heart — and `exhausted=` is non-zero exactly when a guard
// had something to raise for.

// HeapCapacity is the arena's size. It is spelled in the array types above and in the allocator's
// bound; a third spelling — the raise's sentence and the self-report's `capacity=` — is derived from
// this constant so the three cannot disagree with the allocator.
// HeapCapacity is exported so a test, the CLI and docs/operations.md can name the arena
// without repeating the constant; TestTheArenaSpellsItsCapacityOnce is what keeps the
// IR, the sentence and this number one fact.
const HeapCapacity = 1024

// heapExhaustedMessage is the trap's message. The reference has no stable wording for MemoryError (the
// platform's allocator decides it), so the sentence here is written for the reader who has to act on
// it: what ran out, how big the thing that ran out was, and the row that owns growing it.
var heapExhaustedMessage = fmt.Sprintf("out of memory: the compiled heap holds %d live containers (roadmap Gap Q.2)", HeapCapacity)

// HeapExhaustedMessage is the trap's sentence, exported so the CLI's documentation test and the
// integration witness can assert the words rather than copy them.
func HeapExhaustedMessage() string { return heapExhaustedMessage }

// handleReturningCalls are the runtime calls whose result is a heap handle — the set of calls that a
// full arena can poison. `rt_alloc` is the source of the -1; the four helpers allocate through it and
// hand the value straight back, so a guard on `rt_alloc` alone would leave them returning -1 into
// whoever called them. `rt_num_arith` reaches the arena the other way (a status, an out-param) and is
// guarded by its callers' status dispatch — see the note above and `statusRoads`.
var handleReturningCalls = []string{
	"rt_alloc",
	"rt_list_copy",
	"rt_float_new",
	"rt_slice",
	"rt_none",
}

// statusRoads are the calls that reach the arena but do not answer with a handle: their return value is
// a status and the object comes back through an out-param, so the guard is a status branch in the caller
// rather than a comparison beside the call. The inventory test asks both lists, so adding a runtime
// allocator without putting it in one of them is a failing test, not a silent road.
var statusRoads = []string{"rt_num_arith"}

// emitHeapCall emits `%dst = <callText>` and the guard beside it: a full arena answers a negative
// handle, and the raise goes to the innermost handler this point is inside, or out of the function to
// the uncaught path — which is what makes `except MemoryError:` take it and a bare run die at exit 3
// with the class named (ADR 0211's classes, ADR 0214's handleability).
//
// The guard compares rather than assumes because the call is not statically decidable: unlike a literal
// index, whether the arena is full is a fact about the run.
func (g *irGen) emitHeapCall(b *strings.Builder, dst, callText string, sp Span) {
	b.WriteString(fmt.Sprintf("  %s = %s\n", dst, callText))
	bad := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", bad, dst))
	g.markI1(bad)
	badL, okL := g.newLabel("heapfull"), g.newLabel("heapok")
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", bad, badL, okL))
	b.WriteString(badL + ":\n")
	g.raiseHeapExhausted(b, sp)
	b.WriteString(okL + ":\n")
	g.heapUsed = true
}

// raiseHeapExhausted terminates the current block with the arena's trap: the class CPython raises for
// the same condition, the sentence above, and the same two doors every other built-in trap has — the
// innermost handler if this point is inside one, the function's raise exit otherwise. Shared by the
// handle guard (`emitHeapCall`) and the two status roads (`rt_num_arith`'s callers), so a program that
// writes `except MemoryError:` cannot tell which road ran out of memory, and neither can the report.
func (g *irGen) raiseHeapExhausted(b *strings.Builder, sp Span) {
	g.raiseTo(b, exnCode("MemoryError"), "MemoryError", heapExhaustedMessage, sp)
}

// emitRtAllocKind is the common case: allocate one object of a kind the compiler knows as a constant
// (HeapList, HeapDict, HeapSet, HeapKindInstance, …).
func (g *irGen) emitRtAllocKind(b *strings.Builder, dst string, kind int, sp Span) {
	g.emitRtAlloc(b, dst, strconv.Itoa(kind), sp)
}

// emitRtFloatNew is the float box — a container slot that holds a double needs one of these, and it is
// allocated inside the helper, so the guard belongs beside this call.
func (g *irGen) emitRtFloatNew(b *strings.Builder, dst, constText string, sp Span) {
	g.emitHeapHelperCall(b, dst, "rt_float_new", "double "+constText, sp)
}

// emitRtAlloc is the common case: allocate one object of a known kind.
func (g *irGen) emitRtAlloc(b *strings.Builder, dst, kind string, sp Span) {
	g.emitHeapCall(b, dst, fmt.Sprintf("call i32 @rt_alloc(i32 %s)", kind), sp)
}

// emitHeapHelperCall is the same guard for a helper that allocates internally — `rt_list_copy(src)`,
// `rt_float_new(d)`, `rt_slice(…)`, `rt_none()`. The helper is where the -1 comes from as surely as
// rt_alloc is, and it is the call site that knows which handler the raise belongs to.
func (g *irGen) emitHeapHelperCall(b *strings.Builder, dst, fn, args string, sp Span) {
	call := fmt.Sprintf("call i32 @%s(%s)", fn, args)
	if args == "" {
		call = fmt.Sprintf("call i32 @%s()", fn)
	}
	g.emitHeapCall(b, dst, call, sp)
}
