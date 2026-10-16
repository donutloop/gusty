// The compiled root stack's bookkeeping on the codegen side: which stack slots hold handles, which loops
// are safe points, and whether a piece of code can allocate at all (ADR 0181, ADR 0187).
package lang

import (
	"fmt"
	"strconv"
	"strings"
)

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

// loopSafePoint emits the collection at the top of a loop body — the safe point a loop otherwise never
// has. A function body collects before each of its statements (gcCall), but a loop is *one* statement,
// and its body can mint an object per iteration: the arena therefore had to hold every container a
// `while` or a `for ... in range(...)` ever produced, because nothing between the loop's first iteration
// and its last was allowed to reclaim one. Roadmap Gap Q.2 measured the consequence — a churn loop of
// 2000 short-lived lists filled the 1024-slot arena, rt_alloc answered -1, and the run went on walking
// the words below the objects (integration/programs/gc_precise.gy, which says in its own header that
// "thousands of short-lived containers must be reclaimed while the program is still running", and could
// only ever answer that by luck).
//
// A loop body is a safe point on the same argument a function-body statement is (ADR 0181): the call
// stands before the body's first instruction, so nothing the body computed is in flight in a register,
// and everything the loop carries across the back edge lives in a rooted slot — the counter, the loop
// variable — or is parked by gcRootIterable for the roads that walk a container by handle. A road that
// cannot make that argument keeps no safe point and says so where it stands.
func (g *irGen) loopSafePoint(b *strings.Builder, body []Stmt) {
	if !loopMayAllocate(body) {
		// A loop that cannot ask for an object — `while i < n: total = total + i` — is not made to sweep
		// 1024 slots per iteration for nothing.
		return
	}
	// Asking for a collection means the module references the collector and the arena, so both blocks
	// have to be emitted even if no other site in this module turns out to allocate (ADR 0173).
	g.heapUsed = true
	g.rooted = true
	g.gcCall(b)
}

// nonAllocatingBuiltins are the calls whose answer is never a fresh object: a count, a verdict, a
// rendering into the text table, or one of the operands the program already holds. Anything else a loop
// body may call — a user function, `list(xs)`, `sorted(xs)`, `sum(…)` folding to a double — can mint a
// container or a box, and the loop that calls it needs its safe point.
var nonAllocatingBuiltins = map[string]bool{
	"len": true, "print": true, "type": true, "id": true, "abs": true, "ord": true, "chr": true,
	"int": true, "float": true, "bool": true, "str": true, "repr": true, "range": true,
	"min": true, "max": true, "all": true, "any": true, "hash": true, "divmod": true,
}

// loopMayAllocate answers whether a loop body can ask the arena for an object, which is what decides
// whether the body is worth a collection. Unknown shapes answer true: a false positive costs a sweep
// that finds nothing, and a false negative is the silent heap exhaustion this file exists to end
// (roadmap Gap Q.2, ADR 0347).
func loopMayAllocate(sts []Stmt) bool {
	for _, s := range sts {
		if stmtMayAllocate(s) {
			return true
		}
	}
	return false
}
func stmtMayAllocate(s Stmt) bool {
	switch n := s.(type) {
	case *AssignStmt:
		return exprMayAllocate(n.Target) || exprMayAllocate(n.Value)
	case *AugAssignStmt:
		return exprMayAllocate(n.Target) || exprMayAllocate(n.Value)
	case *ExprStmt:
		return exprMayAllocate(n.Expr)
	case *ReturnStmt:
		return exprMayAllocate(n.Expr)
	case *IfStmt:
		if exprMayAllocate(n.Cond) || loopMayAllocate(n.Then) || loopMayAllocate(n.Else) {
			return true
		}
		for _, el := range n.Elifs {
			if stmtMayAllocate(el) {
				return true
			}
		}
		return false
	case *WhileStmt:
		return exprMayAllocate(n.Cond) || loopMayAllocate(n.Body) || loopMayAllocate(n.Else)
	case *ForStmt:
		return exprMayAllocate(n.Iter) || loopMayAllocate(n.Body) || loopMayAllocate(n.Else)
	case *TryStmt:
		if loopMayAllocate(n.Body) || loopMayAllocate(n.Finally) {
			return true
		}
		for _, h := range n.Excepts {
			if h != nil && loopMayAllocate(h.Body) {
				return true
			}
		}
		return false
	case *WithStmt:
		return exprMayAllocate(n.Expr) || loopMayAllocate(n.Body)
	case *MatchStmt:
		if exprMayAllocate(n.Subject) {
			return true
		}
		for _, c := range n.Cases {
			if c != nil && (exprMayAllocate(c.Pattern) || exprMayAllocate(c.Guard) || loopMayAllocate(c.Body)) {
				return true
			}
		}
		return false
	case *BreakStmt, *ContinueStmt, *PassStmt, *ImportStmt, *ExternDecl, *TypeAliasStmt:
		return false
	default:
		// A statement a future cycle adds to the set is treated as able to allocate, which is the
		// direction that cannot lose a program (roadmap Gap Q.2, ADR 0347).
		return true
	}
}
func exprMayAllocate(e Expr) bool {
	switch n := e.(type) {
	case nil:
		return false
	case *Name, *IntLit, *StrLit, *BoolLit, *NoneLit:
		return false
	case *FloatLit:
		// A double the compiler cannot keep as an immediate is a box, and a box is a slot.
		return true
	case *ListLit, *DictLit, *SetLit, *Tuple, *Comp, *Generator, *Lambda, *Slice, *AwaitExpr, *FString:
		return true
	case *BinOp:
		return exprMayAllocate(n.L) || exprMayAllocate(n.R)
	case *UnOp:
		return exprMayAllocate(n.X)
	case *Index:
		// A subscript reads a word out of an object the program already holds.
		return exprMayAllocate(n.Obj) || exprMayAllocate(n.Idx)
	case *Attr:
		return exprMayAllocate(n.Obj)
	case *Call:
		if fn, ok := n.Fn.(*Name); ok && nonAllocatingBuiltins[fn.Value] && !isExnClass(fn.Value) {
			for _, a := range n.Args {
				if exprMayAllocate(a) {
					return true
				}
			}
			return false
		}
		// A call — a user function most of all — can allocate behind the call site's back.
		for _, a := range n.Args {
			if exprMayAllocate(a) {
				return true
			}
		}
		return true
	case *CondExpr:
		return exprMayAllocate(n.Cond) || exprMayAllocate(n.If) || exprMayAllocate(n.Else)
	case *ChainCompare:
		for _, part := range n.Operands {
			if exprMayAllocate(part) {
				return true
			}
		}
		return false
	case *KeywordArg:
		return exprMayAllocate(n.Value)
	default:
		return true
	}
}

// gcRootIterable parks the container handle a loop road walks into a frame slot and registers that slot
// as a root. The road reads the handle from a register on every iteration, which is the one shape
// ADR 0181 forbids at a collection: live, and reachable from nowhere the collector can see. Without it,
// the loop safe point would recycle the container the loop is still walking.
func (g *irGen) gcRootIterable(b *strings.Builder, h string) {
	// The slot is spelled `%_<tmp>` — the same shape a variable's alloca has — because that is the name
	// `rt_root_put` is pushed with (gcReg), and a root entry for a slot that was never allocated is the
	// module `llc` rejects, not a warning.
	name := fmt.Sprintf("it%d", g.gcRootIdx) // gcReg bumps the counter, so each loop's slot is distinct
	b.WriteString(fmt.Sprintf("  %%%s = alloca i32\n", "_"+name))
	b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%%s\n", h, "_"+name))
	g.gcReg(b, name) // pushes `%_<name>`, the spelling every variable slot uses
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
