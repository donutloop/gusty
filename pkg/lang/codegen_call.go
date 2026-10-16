// codegen_call.go is where a Call is decided. g.call names the callee and hands it to the
// road that knows it: a method in codegen_method.go, an instantiation in
// codegen_instance.go, a user function in codegen_func.go, an extern and an imported
// sibling here, and every predeclared name in codegen_builtin.go and its family files.
package lang

import (
	"fmt"
	"strings"
)

// call resolves the callee's name — a Name, an inlined lambda, a name bound to a lambda — and then
// asks, in order, the six roads that can answer a call. Each road is a method in the file that owns
// its shape; the order below is the order the roads have always been tried in.
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
		return g.callMethod(b, c, attr, fnName)
	}

	// Class instantiation: `ClassName(args)`.
	if _, isClass := g.classInfos[fnName]; isClass {
		return g.callInstantiate(b, c, fnName)
	}

	if ed, ok := g.externs[fnName]; ok {
		return g.callExtern(b, c, fnName, ed)
	}
	if g.curModName != "" && g.imports != nil {
		if v, called, err := g.callImportedSibling(b, c, fnName); called {
			return v, err
		}
	}
	if g.funcs[fnName] {
		return g.callUserFunc(b, c, fnName)
	}
	// Everything the predeclared names answer is one dispatch away (codegen_builtin.go).
	return g.callBuiltins(b, c, fnName)
}

// callImportedSibling calls a function that lives in the module this one is being emitted
// from, under the name the linker gave it. It answers `called` rather than falling through:
// the caller keeps going to the function table when the name is not a sibling's.
func (g *irGen) callImportedSibling(b *strings.Builder, c *Call, fnName string) (string, bool, error) {
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
					return "", true, err
				}
				b.WriteString("i32 " + av)
			}
			b.WriteString(")\n")
			return ret, true, nil
		}
	}
	return "", false, nil
}

// callExtern lowers a call to an `extern` declaration: the argument words the declaration asks for
// and the call to the program's own symbol, unprefixed (roadmap Gap R.4).
func (g *irGen) callExtern(b *strings.Builder, c *Call, fnName string, ed *ExternDecl) (string, error) {
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
