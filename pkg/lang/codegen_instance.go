// Instances: which class an expression's value belongs to, the instance runtime block, the
// operand question an arithmetic or comparison road asks about an instance, and the road that
// builds one (callInstantiate, the other half of `Point(1, 2)`).
package lang

import (
	"fmt"
	"strings"
)

// instanceExpressionClass names the class of an expression the program constructed or bound to an
// instance, when the module's records can see it. It is deliberately narrow: a name whose binding the
// assignment walk tracked, or a bare construction. Anything else is a value the ordinary road may still
// answer, and stealing it here would turn a working int into a print dispatch no one asked for.
func (g *irGen) instanceExpressionClass(e Expr) string {
	if g == nil || e == nil {
		return ""
	}
	switch n := e.(type) {
	case *Name:
		return g.varClasses[n.Value]
	case *Call:
		if nm, ok := n.Fn.(*Name); ok {
			if _, isClass := g.classInfos[nm.Value]; isClass {
				return nm.Value
			}
		}
	}
	return ""
}

// instanceRuntimeIR is the module's own half of the instance tag: the class-id-to-qualified-name
// table and the helpers that ask a heap handle for the name its constructor stored in slot 0. The
// tag says *instance*; only the object says `C`. That distinction is what made a slot appended with
// `C()` answer every tag-keyed question as `int` (roadmap Gap R.209), and a tag no renderer can name
// is only half a kind — so this block is emitted whenever the heap block is, because the heap block's
// mixed printer references its `@rt_instance_repr_ptr` by name.
func (g *irGen) instanceRuntimeIR() string {
	var out strings.Builder
	out.WriteString("; The class-name table and the two instance renderers (roadmap Gap R.209, ADR 0325).\n")
	out.WriteString("; An instance slot's payload is a heap handle, and the object's class id lives in the\n")
	out.WriteString("; object's own slot 0 — written by the constructor, read here. A user class therefore names\n")
	out.WriteString("; itself in a TypeError or a print, instead of the tag's fallback word `object`.\n")
	out.WriteString("@rt.inst.buf = internal global [192 x i8] zeroinitializer\n")

	defaultName := emitLLVMStringGlobal(&out, "@rt.inst.default", "object")
	fmt0 := emitLLVMStringGlobal(&out, "@rt.inst.fmt0", "TypeError: argument of type '%s' is not iterable")
	fmt1 := emitLLVMStringGlobal(&out, "@rt.inst.fmt1", "TypeError: '%s' object is not iterable")
	fmt2 := emitLLVMStringGlobal(&out, "@rt.inst.fmt2", "TypeError: object of type '%s' has no len()")
	fmt3 := emitLLVMStringGlobal(&out, "@rt.inst.fmt3", "TypeError: 'in <string>' requires string as left operand, not %s")
	fmt4 := emitLLVMStringGlobal(&out, "@rt.inst.fmt4", "TypeError: unhashable type: '%s'")
	fmt5 := emitLLVMStringGlobal(&out, "@rt.inst.fmt5", "<%s object at 0x%x>")

	modName := g.curModName
	if modName == "" {
		modName = "__main__"
	}
	n := len(g.classOrder)
	if n > 0 {
		qualified := make([]string, n)
		short := make([]string, n)
		for i, cls := range g.classOrder {
			if cls == "" {
				cls = "object"
			}
			qualified[i] = emitLLVMStringGlobal(&out, fmt.Sprintf("@rt.inst.qname%d", i), modName+"."+cls)
			short[i] = emitLLVMStringGlobal(&out, fmt.Sprintf("@rt.inst.sname%d", i), cls)
		}
		fmt.Fprintf(&out, "@rt.inst.names = private global [%d x i8*] [", n)
		for i, np := range qualified {
			if i > 0 {
				out.WriteString(", ")
			}
			fmt.Fprintf(&out, "i8* %s", np)
		}
		out.WriteString("]\n")
		fmt.Fprintf(&out, "@rt.inst.short_names = private global [%d x i8*] [", n)
		for i, np := range short {
			if i > 0 {
				out.WriteString(", ")
			}
			fmt.Fprintf(&out, "i8* %s", np)
		}
		out.WriteString("]\n")
	}

	// The reference puts two different words in two different places. A `TypeError` names the class
	// (`argument of type 'C' is not iterable`); the identity form names the class as the interpreter
	// saw it (`<__main__.C object at 0x...>`). One helper per spelling, because the two are not free to
	// drift back into each other.
	out.WriteString("define internal i8* @rt_class_qual(i32 %id) {\nentry:\n")
	if n == 0 {
		// No class in this module can have stored an id, so nothing can reach a table. Keep the
		// helper total anyway: the printer arm that calls it lives in the static heap block, and a
		// program that allocates no instance must still verify when a tag of 10 somehow reaches it.
		fmt.Fprintf(&out, "  ret i8* %s\n}\n\n", defaultName)
	} else {
		fmt.Fprintf(&out, "  %%ok = icmp ult i32 %%id, %d\n", n)
		out.WriteString("  br i1 %ok, label %load, label %fallback\n")
		out.WriteString("load:\n")
		fmt.Fprintf(&out, "  %%p = getelementptr [%d x i8*], [%d x i8*]* @rt.inst.names, i32 0, i32 %%id\n", n, n)
		out.WriteString("  %name = load i8*, i8** %p\n")
		out.WriteString("  ret i8* %name\n")
		out.WriteString("fallback:\n")
		fmt.Fprintf(&out, "  ret i8* %s\n}\n\n", defaultName)
	}
	out.WriteString("define internal i8* @rt_class_short(i32 %id) {\nentry:\n")
	if n == 0 {
		fmt.Fprintf(&out, "  ret i8* %s\n}\n\n", defaultName)
	} else {
		fmt.Fprintf(&out, "  %%ok = icmp ult i32 %%id, %d\n", n)
		out.WriteString("  br i1 %ok, label %load, label %fallback\n")
		out.WriteString("load:\n")
		fmt.Fprintf(&out, "  %%p = getelementptr [%d x i8*], [%d x i8*]* @rt.inst.short_names, i32 0, i32 %%id\n", n, n)
		out.WriteString("  %name = load i8*, i8** %p\n")
		out.WriteString("  ret i8* %name\n")
		out.WriteString("fallback:\n")
		fmt.Fprintf(&out, "  ret i8* %s\n}\n\n", defaultName)
	}

	// rt_instance_repr_ptr renders the stable identity form. The reference prints a real address;
	// this backend prints the object's heap slot in the same position, because an ASLR'd pointer in a
	// compiled program would make a byte-for-byte corpus run impossible. Only the *class* is the claim.
	out.WriteString("define internal i8* @rt_instance_repr_ptr(i32 %h) {\nentry:\n")
	out.WriteString("  %id = call i32 @rt_inst_get(i32 %h, i32 0)\n")
	out.WriteString("  %name = call i8* @rt_class_qual(i32 %id)\n")
	out.WriteString("  %buf = getelementptr [192 x i8], [192 x i8]* @rt.inst.buf, i32 0, i32 0\n")
	fmt.Fprintf(&out, "  call i32 (i8*, i32, i8*, ...) @snprintf(i8* %%buf, i32 192, i8* %s, i8* %%name, i32 %%h)\n", fmt5)
	out.WriteString("  ret i8* %buf\n}\n\n")

	// rt_instance_msg_ptr writes one of five sentences into the module's one instance message buffer.
	// It is not a raise: the arm that calls it owns the exception code, the span and the landing pad, and
	// then stores the buffer over the static text setExn wrote. A helper that raised would re-derive none
	// of that (ADR 0265, ADR 0271).
	out.WriteString("define internal i8* @rt_instance_msg_ptr(i32 %h, i32 %which) {\nentry:\n")
	out.WriteString("  %id = call i32 @rt_inst_get(i32 %h, i32 0)\n")
	out.WriteString("  %name = call i8* @rt_class_short(i32 %id)\n")
	out.WriteString("  %buf = getelementptr [192 x i8], [192 x i8]* @rt.inst.buf, i32 0, i32 0\n")
	for _, which := range []int{4, 3, 2, 1, 0} {
		fmt.Fprintf(&out, "  %%c%d = icmp eq i32 %%which, %d\n", which, which)
	}
	fmt.Fprintf(&out, "  %%f4 = select i1 %%c4, i8* %s, i8* %s\n", fmt4, fmt3)
	fmt.Fprintf(&out, "  %%f3 = select i1 %%c3, i8* %s, i8* %%f4\n", fmt3)
	fmt.Fprintf(&out, "  %%f2 = select i1 %%c2, i8* %s, i8* %%f3\n", fmt2)
	fmt.Fprintf(&out, "  %%f1 = select i1 %%c1, i8* %s, i8* %%f2\n", fmt1)
	fmt.Fprintf(&out, "  %%f0 = select i1 %%c0, i8* %s, i8* %%f1\n", fmt0)
	out.WriteString("  call i32 (i8*, i32, i8*, ...) @snprintf(i8* %buf, i32 192, i8* %f0, i8* %name)\n")
	out.WriteString("  ret i8* %buf\n}\n\n")
	return out.String()
}

// instanceOperand is the compiled side of the dunder question the bool predicate asks:
// `g.receiverClass` is the same predicate `emitDunderBinOp` uses to decide whether `a < 4`
// is an icmp or a call to `__lt__`, and the two must agree — an overloaded comparison prints
// what its method returned (dunder.gy prints 1, 0 for exactly that reason), while a builtin
// one prints a verdict (ADR 0257, against the L6.6 rule).
func (g *irGen) instanceOperand(x Expr) bool {
	return g.receiverClass(x) != ""
}

// callInstantiate builds an instance of a class: the slot, the class id, and the attribute
// row that starts out holding nothing (ADR 0235).
func (g *irGen) callInstantiate(b *strings.Builder, c *Call, fnName string) (string, error) {
	g.heapUsed = true
	h := g.newTmp()
	g.emitRtAllocKind(b, h, HeapKindInstance, c.Span())
	// A heap slot is reused, and an instance inherits nothing from whoever held it last — not
	// even which attributes exist. The clear zeroes the presence row; the slot count is final
	// because every name the program can write was interned before emission (ADR 0235).
	b.WriteString(fmt.Sprintf("  call void @rt_inst_clear(i32 %s, i32 %d)\n", h, g.nextSlot))
	b.WriteString(fmt.Sprintf("  call void @rt_inst_put(i32 %s, i32 0, i32 %d)\n", h, g.classIDs[fnName]))
	if fn, ok := g.resolveMethod(fnName, "__init__"); ok {
		// Each parameter of the constructor gets one word, in the order the constructor declared them, and a
		// parameter the call omitted is filled from its OWN default expression (each value emits its own load
		// statement on a fresh line, which is why the vector is built before the call line is started).
		// `class C():` / `    def __init__(self, a, b=2):` / `        self.b = b` / `c = C(5)` / `print(c.b)`
		// read `0` for the reference's `2` while this walk ran `for i, arg := range c.Args` and left the body's
		// `b` slot unfilled (roadmap Gap R.472, ADR 0434).
		argWords, aerr := g.newCallArgBinder(b).methodCallArgs(c.Args, g.resolveMethodDef(fnName, "__init__"), fnName+".__init__")
		if aerr != nil {
			return "", aerr
		}
		b.WriteString(fmt.Sprintf("  call i32 @%s(i32 %s%s)\n", fn, h, operandWords(argWords)))
		// A constructor that raises has failed to construct: the exception propagates
		// before the instance is ever used (Gap R.41, ADR 0223).
		g.checkExn(b)
	}
	return h, nil
}
