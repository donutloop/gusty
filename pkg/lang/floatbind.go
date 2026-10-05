package lang

import (
	"fmt"
	"strings"
)

// floatbind.go — the assignment that changes a variable's state from int to float
// (roadmap L11.6, Gap P.1's `/=`, Gap R.155's rebinding; ADR 0274).
//
// Two statements have always ended with a double landing in a slot the variable's
// first binding made four bytes wide:
//
//   x = 7
//   x /= 2          # `/` is true division: the answer is a float whatever arrives
//   x = 2.5         # the rebinding the reference answers with a float too
//
// The reference keeps the *value* and its *kind* together, so both leave `x` holding
// a float. This backend keeps a variable's kind in the width of its stack slot, and
// nothing was said about the change of mind: the emitted `store double` wrote eight
// bytes into the four-byte `alloca` the first binding chose. LLVM's module verifier
// cannot see through an opaque pointer, so the module verified, llc accepted it, and
// the variable's neighbour paid — `y = 12345` beside an `x = 2.5` printed `1074003968`,
// with the exit code of success (Gap R.155, measured in Gap P.1's probe).
//
// The pair is what the change of mind needs, and it is the same pair the rest of the
// family already carries: the double goes into a float box, the variable is bound to
// the `(payload, tag)` pair with the float's tag, and every position that reads the
// name afterwards — print, truthiness, equality, the arithmetic door, a function's
// pair parameter — asks the tag as it always has. `numericPairVar` vouches for a
// float-rebound variable exactly as it does for one whose pair came from arithmetic
// (ADR 0267), so the read doors need no new case.
//
// What it costs is one `@rt_float_new` and one tagged store on the statement that
// changes the kind, and nothing anywhere else: a variable that never changes state
// keeps the plain slot and the plain load it had, which is why the `fibonacci` and
// `function_calls` benchmarks are unchanged by this file.

// bindFloatRebinding re-binds a variable that was born holding an `i32` with the double
// it has just been given, as the (payload, tag) pair. It reports whether it took the
// statement; a `false` leaves the caller's old road exactly where it was.
func (g *irGen) bindFloatRebinding(b *strings.Builder, name, v string) bool {
	if v == "" || v == "undef" {
		return false
	}
	// Worth taking only when the slot under the name is the `i32` the variable's first
	// binding chose. A name whose slot was allocated as a `double` (`x = 1.5` from the
	// start, or a parameter the signature already typed as one) fits the store the
	// caller is about to emit, and taking the pair there would pay for a box to buy
	// nothing.
	if !g.allocd[name] || g.doubleSlot[name] {
		return false
	}
	// A name that is already a float was allocated for the double, or already carries the pair:
	// both took this road before the row landed, and re-boxing a name whose slot the previous
	// binding wrote a box handle into would show the runtime one box too many.
	if g.floatVars[name] {
		return false
	}
	// Anything else that already owns the name keeps its own road: a tagged name is
	// already a pair, a container or an instance name is not a number the reference
	// would divide, and a text name's slot holds a string table index.
	if g.taggedVars[name] || g.listVars[name] || g.runtimeDicts[name] || g.runtimeSets[name] || g.mixedLists[name] {
		return false
	}
	if g.containerNamed(name) || g.unionVars[name] || g.varClasses[name] != "" {
		return false
	}
	if _, hasText := g.strVals[name]; hasText {
		return false
	}
	if g.noneVars[name] || g.boolVars[name] {
		return false
	}
	// A parameter's slot is the signature's allocation: what arrives is what the caller
	// promised, and re-deciding it here would disagree with the `define`.
	if _, isParam := g.params[name]; isParam {
		return false
	}
	if strings.HasPrefix(v, "@") {
		// A named global (`@none_h` and friends) is a handle, not the double this road
		// is about to box.
		return false
	}
	// The double becomes a float box and the variable becomes the pair that points at
	// it: the tag is what every later read asks, and the box is what `rt_num_arith`
	// lifts back to a double on the way into an operation.
	g.heapUsed = true
	h := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_float_new(double %s)\n", h, v))
	g.bindTaggedVar(b, name, h, "1")
	// The slot now holds a heap handle — the float box — where it used to hold an `i32`.
	// Unrooted, the collector cannot see the box, recycles its handle, and the next
	// `@rt_float_new` writes a different double over the value the variable still names:
	// `h = 1` / `h /= 3` then two prints of `h` answered 1.333…, 2.666… (the second seeing
	// the first's answer), with the exit code of success (roadmap L11.6, ADR 0181's rule
	// that every slot holding a handle says so).
	g.gcReg(b, name)
	if g.taggedOrigin == nil {
		g.taggedOrigin = map[string]string{}
	}
	g.taggedOrigin[name] = taggedOriginFloat
	if g.floatVars != nil {
		delete(g.floatVars, name)
	}
	return true
}
