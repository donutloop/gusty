// The gradual-typing union: how a slot that can hold more than one kind is stored, and how it prints.
package lang

import (
	"fmt"
	"strings"
)

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
