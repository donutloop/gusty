package lang

import (
	"fmt"
	"strings"
)

// ─── Gap R.55: what an instance prints as, and who is asked before the fallback ────────────────
//
// The compiled backend had exactly one answer for an instance: the identity form ADR 0325 built,
// `<__main__.C object at 0x…>`. That is CPython's answer for a class that says NOTHING, and the
// program is allowed to say something — `def __str__(self):` and `def __repr__(self):` are the two
// methods whose whole job is to answer this question. A class that defined them was still printed with
// the identity form, and `str()`/`repr()` of such an instance REFUSED at the compile door, so the one
// place a program can describe how its own object should read was the one place the compiler would not
// listen (roadmap **Gap R.55**, row **L12.3**).
//
// The fix is one table per class and TWO questions, asked by one helper. `rt_print_mixed_value` is the
// module's only value renderer (ADR 0258 made `print`, `str()`, `repr()`, an f-string field and every
// container element go through it), and its instance arm is where the identity form is written. That arm
// now hands the value to `rt_print_instance_value`, which asks, in this order:
//
//	does the object's class — the one the object ITSELF names in slot 0, not the one the expression
//	happens to spell — bind a dunder this backend can run? If so, call it and render what came back
//	through this same printer; if not, the identity form, exactly as before.
//
// Asking the OBJECT is what makes the door survive the shapes an expression cannot name: a name bound to
// one class on one line and another on the next, a slot of a container that mixes an instance with
// numbers and texts, one `print` carrying two different classes. Each of those arrives at the helper as a
// handle, and the handle carries its class id in its own slot 0 (ADR 0325's `rt_inst_get(h, 0)`), so the
// table is indexed by that id and the run time picks the row. What it does NOT yet reach is a value whose
// arrival loses the tag entirely — the loop variable of `[C(), C()]` and a parameter that receives an
// instance are printed as the number the handle happens to be, because the BINDING position stored the
// word without a kind beside it and `print` never routed it to this door. That is **Gap R.509**, not a
// hole in this table: the door reads the class off the object precisely so it will not care which
// position the handle came from, and all it waits for is the tag.
//
// The two doors are kept apart, because the reference keeps them apart. The str question (`print`,
// `str()`, an f-string field — the printer's `quote` flag at 0) asks `__str__`, then `__repr__` the way
// `object.__str__` hands the question down, then the identity form; the repr question (a container
// element, `repr()` — flag 1) asks `__repr__`, then the identity form, and NEVER `__str__`. A class that
// defines only `__str__` therefore prints its text with `print` and its identity INSIDE a container, which
// is what CPython does, and which one shared "the class said something" flag would get wrong.
//
// What the table will not hold is a dunder whose answer the compiler cannot see is a text. The reference
// requires one (`TypeError: __str__ returned non-string (type int)`), and this backend represents a text
// as an interned index, so an answer the body does not provably make a text has no tag to carry: the row
// stays null and the class keeps the identity form. Filing that shape is a decision — an invented `0`
// for a text is the Gap R.22 signature this file exists to keep dead.

// renderDoor is one class's answer to one of the two render questions.
type renderDoor struct {
	fn   string // the symbol of the dunder the resolution order names
	tag  int32  // the tag its answer carries (TagStr — the only one this door accepts)
	line int    // the line of its `def`, which is the line the reference's traceback names
}

// The two dunders, named by the door that asks them.
const (
	renderStrDunder  = "__str__"
	renderReprDunder = "__repr__"
)

// renderAnswerKind asks what a dunder's body hands back, when the body says so outright: every
// `return` in it is a literal of ONE kind. That is the whole of the honest evidence this backend has —
// a body that returns `self.n`, or one thing on one path and another on the next, has no kind the
// compiler may name, and the row stays dead rather than inventing one.
//
// The answer is used for two decisions: the tag the render arm reads, and the word inside the
// reference's own sentence for a dunder that handed back something it will not print
// (`TypeError: __str__ returned non-string (type int)`), which the trap needs before the run time does.
func (g *irGen) renderAnswerKind(fd *FuncDef) (int32, bool) {
	if fd == nil {
		return 0, false
	}
	tag := int32(-1)
	seen := false
	var bad bool
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			if bad {
				return
			}
			switch s := st.(type) {
			case *ReturnStmt:
				t, ok := renderLiteralKind(s.Expr)
				if !ok {
					bad = true
					return
				}
				if seen && tag != t {
					bad = true // two arms, two kinds: no single word for it
					return
				}
				tag, seen = t, true
			case *FuncDef, *ClassDef:
				// A nested body is its own scope: its returns are not this one's.
			default:
				walk(classChainStmtChildren(st))
			}
		}
	}
	walk(fd.Body)
	if bad {
		return 0, false
	}
	if !seen {
		// A body with no `return` in it at all ANSWERS NONE — that is what the reference does when a
		// function falls off its end, and the reference then refuses to print it: `TypeError: __str__
		// returned non-string (type NoneType)`. Naming that kind is what makes a body whose every path
		// raises behave too: the raise sets the flag, the door bails without printing, and the raise
		// the program wrote is the one the traceback reports (roadmap Gap R.55).
		return int32(TagNone), true
	}
	return tag, true
}

// renderLiteralKind is the one place that names a literal's kind for the render door. It reads the
// spellings the printer already renders — the number, the float, the verdict, None, the four containers
// and the text — and says nothing about anything else.
func renderLiteralKind(e Expr) (int32, bool) {
	switch v := e.(type) {
	case *IntLit:
		return int32(TagInt), true
	case *FloatLit:
		return int32(TagFloat), true
	case *BoolLit:
		return int32(TagBool), true
	case *NoneLit:
		return int32(TagNone), true
	case *StrLit:
		return int32(TagStr), true
	case *ListLit:
		return int32(TagList), true
	case *DictLit:
		return int32(TagDict), true
	case *SetLit:
		return int32(TagSet), true
	case *Tuple:
		return int32(TagTuple), true
	case *FString:
		_ = v
		return int32(TagStr), true
	}
	return 0, false
}

// classRenderDoor answers what the class — and every base of it, in the reference's resolution order
// (ADR 0446's `classLinearization`, which is why an inherited `__str__` is found on the base that owns
// it and a shadowing one on the subclass) — binds for one render dunder, IF this backend may run it.
//
// "May run it" is three questions, all read off the declaration the class road already holds:
//   - the method exists (the `methods` and `methodDefs` tables of ADR 0434 agree);
//   - it takes exactly its receiver — `def __str__(self, extra)` is CPython's
//     `__str__() missing 1 required positional argument: 'extra'`, a raise this backend has no door
//     for, so the row stays null and the identity form answers (filed with the arity family);
//   - its answer is provably a text (`methodReturnsStr`, the same predicate that decides
//     `print(c.__str__())`), because the row carries one tag and that tag is TagStr.
func (g *irGen) classRenderDoor(className, dunder string) (renderDoor, bool) {
	if className == "" || dunder == "" {
		return renderDoor{}, false
	}
	for _, cn := range g.classLinearization(className) {
		ci, ok := g.classInfos[cn]
		if !ok || ci == nil {
			continue
		}
		sym, has := ci.methods[dunder]
		if !has || sym == "" {
			continue
		}
		fd := ci.methodDefs[dunder]
		if fd == nil || len(fd.Params) != 1 {
			return renderDoor{}, false
		}
		// A text answer is the door's normal case; anything the body outright says otherwise about is
		// still a live row, because the reference RAISES for it rather than printing anything.
		tag, provable := g.renderAnswerKind(fd)
		if !g.methodReturnsStr(fd) {
			if !provable {
				return renderDoor{}, false
			}
		} else {
			tag = int32(TagStr)
		}
		line := int(fd.Span().Line)
		return renderDoor{fn: sym, tag: tag, line: line}, true
	}
	return renderDoor{}, false
}

// classRenderStr / classRenderRepr are the two questions, spelled out so no caller has to know which
// dunder backs which door.
func (g *irGen) classRenderStr(className string) (renderDoor, bool) {
	return g.classRenderDoor(className, renderStrDunder)
}

func (g *irGen) classRenderRepr(className string) (renderDoor, bool) {
	return g.classRenderDoor(className, renderReprDunder)
}

// programAsksToBeRendered is the gate: the table, the globals beside it and the frame-name strings
// exist only when SOME class in the module binds a dunder this backend can run. A program whose classes
// say nothing pays nothing — the printer's instance arm stays the identity form alone, which is the
// negative control the IR row of `class_render_test.go` asserts.
func (g *irGen) programAsksToBeRendered() bool {
	for _, cn := range g.classOrder {
		if _, ok := g.classRenderStr(cn); ok {
			return true
		}
		if _, ok := g.classRenderRepr(cn); ok {
			return true
		}
	}
	return false
}

// renderNonStringSentence is the reference's own words for a dunder that handed back something the
// reference refuses to print. The parenthesised word is the kind the answer actually was, which is why
// the sentence belongs to the row rather than to the module: the compiler is the only thing that knows
// what the body returns.
func renderNonStringSentence(dunder, kindWord string) string {
	return "TypeError: " + dunder + " returned non-string (type " + kindWord + ")"
}

func renderNonStringArg(dunder, kindWord string) string {
	return dunder + " returned non-string (type " + kindWord + ")"
}

// renderKindWord names a kind for that sentence, from the one tag table every other door reads.
func renderKindWord(tag int32) string {
	switch ValueTag(tag) {
	case TagInt:
		return "int"
	case TagFloat:
		return "float"
	case TagBool:
		return "bool"
	case TagNone:
		return "NoneType"
	case TagStr:
		return "str"
	case TagList:
		return "list"
	case TagDict:
		return "dict"
	case TagSet:
		return "set"
	case TagTuple:
		return "tuple"
	case TagInstance:
		return "object"
	}
	return "object"
}

// renderTableIR is the fourteen arrays the printer indexes by the class id the OBJECT carries. It is
// emitted with the instance block (ADR 0325), because the arm that reads it lives in the mixed-print
// block that always follows.
//
// The arrays are parallel rather than one struct row for a reason visible in the IR: the printer reads
// the function pointer FIRST and refuses the whole detour when it is null, and a struct load of the
// remaining fields would happen in the same block whether or not the class said anything.
func (g *irGen) renderTableIR(out *strings.Builder) string {
	n := len(g.classOrder)
	if n == 0 || !g.programAsksToBeRendered() {
		return ""
	}
	var b strings.Builder
	w := func(f string, a ...interface{}) { fmt.Fprintf(&b, f, a...) }
	w("@gy.render.line = internal global i32 0\n")
	w("; The two render doors, indexed by the class id the object carries in slot 0\n")
	w("; (roadmap Gap R.55, row L12.3). A null function pointer means the class binds no\n")
	w("; runnable dunder for that question, and the identity form answers.\n")

	strRow := make([][7]string, n)
	repRow := make([][7]string, n)
	for i, cn := range g.classOrder {
		strRow[i] = g.renderRow(out, cn, renderStrDunder, i)
		repRow[i] = g.renderRow(out, cn, renderReprDunder, i)
	}

	arr := func(name, ty string, pick func([7]string) string) {
		w("%s = private global [%d x %s] [", name, n, ty)
		for i := range g.classOrder {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(pick(strRow[i]))
		}
		b.WriteString("]\n")
		w("%s = private global [%d x %s] [", strings.Replace(name, ".str.", ".repr.", 1), n, ty)
		for i := range g.classOrder {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(pick(repRow[i]))
		}
		b.WriteString("]\n")
	}
	arr("@gy.render.str.fn", "ptr", func(r [7]string) string { return r[0] })
	arr("@gy.render.str.tag", "i32", func(r [7]string) string { return r[1] })
	arr("@gy.render.str.msg", "i8*", func(r [7]string) string { return r[2] })
	arr("@gy.render.str.arg", "i8*", func(r [7]string) string { return r[3] })
	arr("@gy.render.str.fname", "i8*", func(r [7]string) string { return r[5] })
	return b.String()
}

// renderRow builds one class's row for one question: the function pointer, the tag the answer carries,
// the two spellings of the reference's sentence, the traceback's own line, the frame's display name and
// the line to push for it. A class that binds nothing is a null row.
func (g *irGen) renderRow(out *strings.Builder, className, dunder string, id int) (row [7]string) {
	door, ok := g.classRenderDoor(className, dunder)
	if !ok {
		// A null row: no door. The tag word is the one value that says so (0 is TagInt, which can never
		// be the text tag the render arm accepts), and the pointer slot carries a null pointer of the
		// right type so the array remains well-typed.
		return [7]string{"ptr null", "i32 -1", "i8* null", "i8* null", "i8* null", "i8* null", "i32 0"}
	}
	kind := renderKindWord(door.tag)
	row[0] = "ptr @" + door.fn
	row[1] = fmt.Sprintf("i32 %d", door.tag)
	row[2] = "i8* " + emitLLVMStringGlobal(out, fmt.Sprintf("@gy.render.msg%d%s", id, dunder), renderNonStringSentence(dunder, kind))
	row[3] = "i8* " + emitLLVMStringGlobal(out, fmt.Sprintf("@gy.render.arg%d%s", id, dunder), renderNonStringArg(dunder, kind))
	// row[4] is unused since the door took over the traceback line: the raise names the
	// PRINT site, not the def, which is the line the reference shows here. The slot stays so the
	// seven arrays keep one shape.
	row[4] = "i8* null"
	row[5] = "i8* " + emitLLVMStringGlobal(out, fmt.Sprintf("@gy.render.name%d%s", id, dunder), dunder)
	row[6] = fmt.Sprintf("i32 %d", door.line)
	return row
}

// instanceRenderHelperIR is the arm the printer calls. It is generated rather than a static block
// because its bounds and its table names are the program's class list.
//
// The order inside it is the order the reference performs the work: push the frame the traceback will
// name, call the dunder, pop, and only then decide what the answer was. Pushing before the call is what
// makes a raise INSIDE the body report `in __str__`; the standing `exn_flag` check after it is ADR 0166's
// arrangement at every other call site in this module.
func (g *irGen) instanceRenderHelperIR() string {
	n := len(g.classOrder)
	if n == 0 || !g.programAsksToBeRendered() {
		var out strings.Builder
		out.WriteString("define internal void @rt_print_instance_value(i32 %v, i32 %quote) {\nentry:\n")
		out.WriteString("  %ip = call i8* @rt_instance_repr_ptr(i32 %v)\n")
		out.WriteString("  call void @rt_out_txt(i8* %ip)\n")
		out.WriteString("  ret void\n}\n\n")
		return out.String()
	}
	var b strings.Builder
	w := func(f string, a ...interface{}) { fmt.Fprintf(&b, f, a...) }

	w("define internal void @rt_print_instance_value(i32 %%v, i32 %%quote) {\nentry:\n")
	w("  %%cid = call i32 @rt_inst_get(i32 %%v, i32 0)\n")
	w("  %%inr = icmp ult i32 %%cid, %d\n", n)
	w("  br i1 %%inr, label %%pick, label %%identity\n")
	w("pick:\n")
	w("  %%qisr = icmp ne i32 %%quote, 0\n")
	w("  br i1 %%qisr, label %%askRepr, label %%askStr\n")

	w("askStr:\n")
	w("  %%pt = getelementptr [%[1]d x i32], [%[1]d x i32]* @gy.render.str.tag, i32 0, i32 %%cid\n", n)
	w("  %%ts = load i32, i32* %%pt\n")
	w("  %%vs = icmp ne i32 %%ts, -1\n")
	// The str question falls through to the repr question, not to the identity form: CPython's
	// object.__str__ asks __repr__, so a class that binds only __repr__ still prints that answer
	// to print(x) and to str(x) (roadmap Gap R.55).
	w("  br i1 %%vs, label %%runStr, label %%askRepr\n")

	w("runStr:\n")
	g.renderDoorBlock(&b, n, "str", "s")

	w("askRepr:\n")
	w("  %%pg = getelementptr [%[1]d x i32], [%[1]d x i32]* @gy.render.repr.tag, i32 0, i32 %%cid\n", n)
	w("  %%tr = load i32, i32* %%pg\n")
	w("  %%vr = icmp ne i32 %%tr, -1\n")
	w("  br i1 %%vr, label %%runRepr, label %%identity\n")
	w("runRepr:\n")
	g.renderDoorBlock(&b, n, "repr", "r")

	// The answer, whichever door produced it. A text goes back through the module's ONE printer with
	// the quote flag at 0 — what a dunder handed back IS the form, and quoting it again inside a
	// container would put the reference's `[R]` back to `['R']` (roadmap Gap R.55). Anything else is
	// the reference's TypeError, raised here rather than printed: the answer is not printable, and the
	// alternative — rendering the interned index as a number — is Gap R.22 arriving through the printer.
	// The standing `@exn_flag` check runs in the door block that made the call (renderDoorBlock), both
	// because a raise out of the body leaves NOTHING to print and because a `phi` may only name a block
	// that really branches here (ADR 0224).
	w("examine:\n")
	w("  %%ans = phi i32 [ %%vy, %%runStr ], [ %%ry, %%runRepr ]\n")
	w("  %%tgs = phi i32 [ %%ts, %%runStr ], [ %%tr, %%runRepr ]\n")
	w("  %%msg = phi i8* [ %%ms, %%runStr ], [ %%mr, %%runRepr ]\n")
	w("  %%arg = phi i8* [ %%gs, %%runStr ], [ %%gr, %%runRepr ]\n")
	w("  %%istg = icmp eq i32 %%tgs, %d\n", int(TagStr))
	w("  br i1 %%istg, label %%render, label %%refused\n")
	w("render:\n")
	w("  call void @rt_print_mixed_value(i32 %%ans, i32 %d, i32 0)\n", int(TagStr))
	w("  ret void\n")
	w("refused:\n")
	w("  store i32 1, i32* @exn_flag\n")
	w("  store i32 %d, i32* @exn_code\n", exnCode("TypeError"))
	w("  store i8* %%msg, i8** @exn_msg\n")
	w("  store i8* %%arg, i8** @exn_arg\n")
	// @exn_frame is left to the door that asked for the render: the reference names the PRINT site, not
	// the def, and only the door knows which line that was (roadmap Gap R.55).
	w("  ret void\n")
	w("bailed:\n")
	w("  ret void\n")
	w("identity:\n")
	w("  %%ip = call i8* @rt_instance_repr_ptr(i32 %%v)\n")
	w("  call void @rt_out_txt(i8* %%ip)\n")
	w("  ret void\n")
	w("}\n\n")
	return b.String()
}

// renderDoorBlock reads one class's row, pushes the frame the reference's traceback names, runs the
// class's own method on the object, pops, and falls through to the shared answer block. The call carries
// one argument — the receiver — which is the shape every method call in this module already takes.
func (g *irGen) renderDoorBlock(b *strings.Builder, n int, which, s string) {
	w := func(f string, a ...interface{}) { fmt.Fprintf(b, f, a...) }
	// Every table read is two instructions — the slot address, then the load — because LLVM's parser
	// refuses a function-local name inside an inline getelementptr, and %cid is the class id the object
	// answered (ADR 0325's rt_inst_get). The house shape is rt_class_qual's own.
	w("  %%pf%[3]s = getelementptr [%[2]d x ptr], [%[2]d x ptr]* @gy.render.%[1]s.fn, i32 0, i32 %%cid\n", which, n, s)
	w("  %%f%[3]s = load ptr, ptr %%pf%[3]s\n", which, n, s)
	w("  %%pm%[3]s = getelementptr [%[2]d x i8*], [%[2]d x i8*]* @gy.render.%[1]s.msg, i32 0, i32 %%cid\n", which, n, s)
	w("  %%m%[3]s = load i8*, i8** %%pm%[3]s\n", which, n, s)
	w("  %%pg%[3]s = getelementptr [%[2]d x i8*], [%[2]d x i8*]* @gy.render.%[1]s.arg, i32 0, i32 %%cid\n", which, n, s)
	w("  %%g%[3]s = load i8*, i8** %%pg%[3]s\n", which, n, s)
	w("  %%pn%[3]s = getelementptr [%[2]d x i8*], [%[2]d x i8*]* @gy.render.%[1]s.fname, i32 0, i32 %%cid\n", which, n, s)
	w("  %%n%[3]s = load i8*, i8** %%pn%[3]s\n", which, n, s)
	// The frame the traceback names is the DUNDER's name at the CALL SITE's line — the shape every
	// other call in this module pushes: rt_frame_push records the line on the parent, so the <module>
	// frame ends up holding the print's line, which is what the reference's first frame says. The line
	// travels in a cell the door wrote, because this helper is shared by every print in the program and
	// only the door knows where it was called from (roadmap Gap R.55).
	w("  %%ln%[3]s = load i32, i32* @gy.render.line\n", which, n, s)
	w("  call void @rt_frame_push(i8* %%n%[3]s, i32 %%ln%[3]s)\n", which, n, s)
	if which == "str" {
		w("  %%vy = call i32 %%fs(i32 %%v)\n")
	} else {
		w("  %%ry = call i32 %%fr(i32 %%v)\n")
	}
	w("  call void @rt_frame_pop()\n")
	// The standing check every call in this module runs after a call, in the block that made it: a
	// raise out of the render body leaves nothing to print, and the answer it handed back on its way
	// out is the raise-exit's zero rather than anything the program said (ADR 0166; roadmap Gap R.55).
	w("  %%ev%[3]s = load i32, i32* @exn_flag\n", which, n, s)
	w("  %%rg%[3]s = icmp eq i32 %%ev%[3]s, 1\n", which, n, s)
	w("  br i1 %%rg%[3]s, label %%bailed, label %%examine\n", which, n, s)
}

// instanceRenderIR is the whole generated block: the table the printer reads and the arm that reads it.
// It is appended to the instance runtime block, which is where the class-name table already lives.
func (g *irGen) instanceRenderIR() string {
	var globals strings.Builder
	table := g.renderTableIR(&globals)
	return globals.String() + table + g.instanceRenderHelperIR()
}

// instanceAnswersThroughItsClass is the compile-time half of the same question: may `str(x)` and
// `repr(x)` be handed to the renderer for this expression at all. It is true when the expression names
// an instance of a class this program declared — the same reading the print door already does — because
// those two builtins refuse anything whose kind they cannot name, and an instance whose class the front
// end cannot see is exactly the case they have always refused (Gap R.171's rule: a missing rendering
// must not become a number).
func (g *irGen) instanceAnswersThroughItsClass(e Expr) bool {
	return g.receiverClassOf(e) != ""
}

// renderDoorPositions is the frame bookkeeping a render call owes. The reference's traceback for a
// dunder that handed back something unprintable names the PRINT (or the str(), or the f-string), not the
// def — the error is raised by the machinery, after the body returned — so the door writes its own frame
// line and copies the shadow stack before the renderer runs user code. A raise from INSIDE the body
// overwrites both, which is why the two shapes report different traces, as they should (roadmap
// Gap R.55; the arrangement every other raise site follows is ADR 0354's).
func (g *irGen) renderDoorPositions(b *strings.Builder, sp Span) {
	if !g.programAsksToBeRendered() {
		return
	}
	g.raiseUsed = true
	if frame := g.raiseFrame(sp); frame != "" {
		fmt.Fprintf(b, "  store i8* %s, i8** @exn_frame\n", g.strConst(frame))
	}
	line := 0
	if !sp.IsZero() && sp.Line > 0 {
		line = int(sp.Line)
	}
	// The cell the shared renderer reads to name the call site in the frame it pushes: one store, and
	// the helper cannot know the line itself because every print in the program calls it (roadmap Gap R.55).
	fmt.Fprintf(b, "  store i32 %d, i32* @gy.render.line\n", line)
	fmt.Fprintf(b, "  call void @rt_frame_here(i32 %d)\n", line)
	b.WriteString("  call void @rt_frames_snapshot()\n")
}
