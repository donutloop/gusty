package lang

// module_const.go is the one question a *data import* has to answer for every other question: `mod.NAME`
// is not a variable and has no word of its own — the value is a literal the program never wrote, folded in
// by `resolveImports` — so every kind question the compiler asks about an expression has to be asked of that
// literal (roadmap L11.6, the "typed stdlib constants" clause; the ledger's `probe_math_const`).
//
// What it fixes is a wrong answer at exit 0, on the compiled leg only, for the most ordinary program the
// standard library offers:
//
//	import math
//	print(math.PI)        # CPython 3.141592653589793 · --interp 3.141592653589793 · --aot 3
//	print(math.PI * 2)    # CPython 6.283185307179586 · --interp 6.283185307179586 · --aot 6
//	print(-math.PI)       # CPython -3.141592653589793 · --interp -3.141592653589793 · --aot -3
//	x = math.PI
//	print(x > 3.14)       # CPython True · --interp True · --aot False
//
// The interpreter evaluated the module and read the value back, so it was right; the compiled backend
// resolved the name to the folded `*FloatLit` only where it *writes* the value (`value()`), and every
// predicate that decides *how* to write it — is this a double, does it have a sign, is it a verdict — saw an
// `*Attr` it could not read and took the integer road. `math.PI` is 3.141592653589793; the module printed
// `3`, which is what an `i32` looks like when a double is poured into it. The same program with a hand-written
// literal (`pi = 3.141592653589793`) has always been right, which is the shape of the bug: the kind was in
// the source, and only half the questions were allowed to read it.
//
// So this is not a new kind rule and not a new table. It is one transparent read: the predicates that own a
// kind question ask it of the folded expression, through the same `g.imports.Globals` map `value()` already
// consults — so the answer that is written and the answer that is asked about are the same answer by
// construction (ADR 0229's rule about who owns a kind question, one construct further out).
//
// A name the program can bind shadows the fold, exactly as it shadows a builtin: `resolveImports` already
// declines to record a module global under a name the importing program assigns itself, and this file asks
// that bookkeeping first rather than keeping a second list.

// foldedModuleAttr resolves `mod.NAME` to the literal `resolveImports` folded in for it, if there is one.
// It returns the expression the module actually declares — a `*FloatLit`, a `*StrLit`, a list literal — and
// callers ask *that* the question they were about to ask the `*Attr`.
//
// The nil guards are the whole contract: no import table, no such module, no such attribute, or an attribute
// whose object is not a plain module name (a method call on an instance, a field of an instance) all answer
// "nothing folded", and the caller keeps the answer it had.
func (g *irGen) foldedModuleAttr(e Expr) (Expr, bool) {
	a, ok := e.(*Attr)
	if !ok || a == nil || g == nil || g.imports == nil {
		return nil, false
	}
	mod, ok := a.Obj.(*Name)
	if !ok || mod == nil || a.Name == nil {
		return nil, false
	}
	// A program that binds the module's name itself owns the name — `math = 3` is not read through the
	// import table, and neither is anything `resolveImports` refused to fold because the program assigned it.
	if g.moduleNames[mod.Value] || g.moduleConsts[mod.Value] != nil {
		return nil, false
	}
	globals, ok := g.imports.Globals[mod.Value]
	if !ok {
		return nil, false
	}
	lit, ok := globals[a.Name.Value]
	if !ok || lit == nil {
		return nil, false
	}
	return lit, true
}

// foldedModuleFloat is the question the print formatter, the arithmetic and the binding ask: is this
// expression's value a double the module declared? One read, one answer, and the same `isFloat` that owns
// every other expression's answer is what reads it, so a module constant is never asked about by a rule the
// program's own literal is not also asked about.
func (g *irGen) foldedModuleFloat(e Expr) bool {
	lit, ok := g.foldedModuleAttr(e)
	if !ok {
		return false
	}
	return g.isFloat(lit)
}
