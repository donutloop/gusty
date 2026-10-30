// The expression roads. irGen.value is the dispatcher: a literal, a name, a subscript, a slice or a
// BinOp is handed to the road that knows its answer, and the road lives with the family it belongs
// to — valueBinOp in codegen_binop.go, valueIndex and valueSlice in codegen_index.go — so that a
// reader who wants to know "who decides this" opens one file instead of scrolling 1,700 lines.
package lang

import (
	"fmt"
	"os"
	"strings"
)

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
	case *ExnMessage:
		// What the exception in flight writes. The arm that named it (`except T as name:`)
		// binds through here, and so does any later read of that name -- the road below is
		// the one text an interned name carries (roadmap Gap R.113, ADR 0406).
		return g.valueExnMessage(b)
	case *AssignExpr:
		v, err := g.value(b, n.Value)
		if err != nil {
			return "", err
		}
		name := n.Name.Value
		if g.isFloat(n.Value) {
			if !g.allocd[name] {
				b.WriteString(fmt.Sprintf("  %%_%s = alloca double\n", name))
				g.allocd[name] = true
				if g.doubleSlot == nil {
					g.doubleSlot = map[string]bool{}
				}
				g.doubleSlot[name] = true
			}
			b.WriteString(fmt.Sprintf("  store double %s, double* %%_%s\n", v, name))
		} else {
			if !g.allocd[name] {
				b.WriteString(fmt.Sprintf("  %%_%s = alloca i32\n", name))
				g.allocd[name] = true
			}
			b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%_%s\n", v, name))
			// A walrus binds, so the slot is written: ADR 0228's flag has to know.
			g.markBound(b, name)
		}
		t := g.newTmp()
		if g.isFloat(n.Value) {
			b.WriteString(fmt.Sprintf("  %s = load double, double* %%_%s\n", t, name))
		} else {
			b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", t, name))
		}
		return t, nil
	case *Name:
		return g.valueName(b, e, n)
	case *Attr:
		// `__doc__` on a top-level def/class name folds the docstring to a
		// string constant (mirrors the interpreter's jit doc introspection).
		if n.Name.Value == "__doc__" {
			if nm, ok := n.Obj.(*Name); ok {
				if fd, ok := g.fds[nm.Value]; ok {
					return g.strConst(fd.Doc), nil
				}
				if ci, ok := g.classInfos[nm.Value]; ok {
					return g.strConst(ci.doc), nil
				}
			}
		}
		// Instance attribute read: `self.x` / `inst.x`.
		if className := g.receiverClass(n.Obj); className != "" {
			// An instance does not hold the class's members: the reference looks in the object's own row first
			// and then walks the class and EVERY base of it. This backend stopped at the object's row, so
			// `class C:` / `X = 1` / `print(C().X)` read a slot nothing had written and printed `0` at the exit
			// code of success (roadmap **Gap R.56**, row **L12.4**). The class object is asked here, and the
			// question that decides HOW it is asked is whether the program ever writes this name onto an
			// instance of this class: an instance's own word wins, as the reference's `__dict__` wins.
			//   · no such write exists — the answer is a compile-time fact, so the read lowers the class's own
			//     value and emits nothing for the object at all;
			//   · the write exists AND the class binds the name — which word belongs to a given object is a
			//     run-time fact, and `@rt_inst_has` is asked (**Gap R.19**'s row keeps the case where NEITHER
			//     binds it and the reference raises AttributeError while this backend still answers 0);
			//   · the write exists and no class binds the name — the instance road it has always used.
			owner, chainVal, chainOwed, chainFound := g.classChainMember(className, n.Name.Value)
			bindsOnInstance := g.instanceBindsAttr(className, n.Name.Value)
			if chainFound && !bindsOnInstance {
				if chainOwed {
					return "", g.classChainRefusal(fmt.Sprintf("%s.%s", owner, n.Name.Value), className, n.Name.Value)
				}
				return g.value(b, chainVal)
			}
			objHandle, err := g.value(b, n.Obj)
			if err != nil {
				return "", err
			}
			g.heapUsed = true
			slot := g.attrSlot(n.Name.Value)
			if chainFound && !chainOwed {
				if out, ok := g.presenceCheckedAttrRead(b, objHandle, slot, chainVal); ok {
					return out, nil
				}
			}
			ret := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 %d)\n", ret, objHandle, slot))
			return ret, nil
		}

		// A MEMBER OF A CLASS, read through the class name: `C.first`. The reference runs a class body and
		// keeps what it bound as attributes of the class, so `class C:` / `first = xs[0]` / `print(C.first)`
		// prints 1. This backend dropped every non-method statement of the body and reached this sentence
		// instead — `unsupported attr expression`, which names an AST node rather than the program, and stops
		// a build the reference simply runs (roadmap Gap R.297, ADR 0418; the sentence rule is Gap R.116).
		//
		// The read asks the class's own table, which is a different road from the instance attributes above:
		// those live in the object's slot array and are addressed by `@rt_inst_get`, and an instance is a
		// value with a class word, while a class here is a name the front end resolved. The recorded
		// expression is lowered where it is READ, which is what keeps a text printing as a text and a number
		// as a number without a second value representation for the class road.
		if nm, isName := n.Obj.(*Name); isName {
			if _, isClass := g.classInfos[nm.Value]; isClass {
				member := n.Name.Value
				// The read asks the class AND every base of it, in the reference's resolution order: it used to
				// look only at the class's own body, so `class Base:` / `NAME = "base"` / `class Sub(Base)` /
				// `print(Sub.NAME)` was refused where the reference answers (roadmap **Gap R.66**, row **L12.4**).
				if _, val, owed, found := g.classChainMember(nm.Value, member); found {
					if owed {
						return "", g.classChainRefusal(fmt.Sprintf("%s.%s", nm.Value, member), nm.Value, member)
					}
					return g.value(b, val)
				}
				return "", g.classChainRefusal(fmt.Sprintf("%s.%s", nm.Value, member), nm.Value, member)
			}
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
		return g.valueBinOp(b, e, n)
	case *UnOp:
		if unaryPosIsTheRoad(n) {
			// `+x` is `__pos__`, not `x + 0`: the operand's own word, or the reference's TypeError.
			// The road is pkg/lang/unary_plus.go (roadmap Gap R.244, ADR 0469).
			return g.valueUnaryPos(b, n)
		}
		// `not build()` asks the same question `if build():` asks, of the container the callee built: the
		// operand is materialised to a name and the operator asked again, so the truth road below — the one
		// a container variable already goes through — answers the answer (roadmap Gap R.67, ADR 0333).
		if n.Op == "not" {
			if ansName, _, okAns := g.containerAnswerOperandRead(b, n.X, containerReadShape); okAns {
				redo := *n
				redo.X = ansName
				return g.value(b, &redo)
			}
		}
		// `-xs[i]` before the operand is read as a plain number: the read below would refuse, and the
		// float arms are the only place that can ask the slot's tag what it holds. Negation carries its
		// own sentence, so a text slot says `bad operand type for unary -: 'str'` the way CPython does
		// rather than the binary minus's line (roadmap L11.1, Gap R.88).
		if n.Op == "-" && g.taggedNegationApplies(n) {
			res, ok, err := g.taggedFloatNegate(b, n)
			if err != nil {
				return "", err
			}
			if ok {
				neg := g.newTmp()
				// The same spelling the static float negation uses: `fsub double 0.0, %v`. A hand-written
				// `fneg` is the right idea and the wrong token for this LLVM, which read it as a malformed
				// instruction and had llc reject the compiler's own module.
				fmt.Fprintf(b, "  %s = fsub double 0.0, %s\n", neg, res)
				return neg, nil
			}
		}
		// The operand's own kind, asked before any instruction is written for it. A negated text, None,
		// a container literal, an instance, or a slot the literal says holds no number, each reach the
		// reference as a TypeError, and reached this road as `sub i32 0, <the value's storage>` — the
		// interned index printed as a negative number, or llc rejecting the module. The raise is the
		// ordinary emitted one, so an open except TypeError: still reaches it (roadmap Gap R.137, ADR 0266).
		if n.Op == "-" {
			if kind, bad := g.negationOperandKind(n.X); bad {
				if kind != "" {
					return g.emitBadNegation(b, kind, n.Span()), nil
				}
				// The slot can report more than one kind, so the raise is a table the object's tag
				// walks — one branch per kind the literal says the slot holds, the last as the else.
				if ix, isIdx := n.X.(*Index); isIdx {
					if v, ok := g.emitBadNegationOfSlot(b, ix, n.Span()); ok {
						return v, nil
					}
				}
			}
		}
		// `not` yields a boolean *value* (printable, storable), so it returns the
		// interpreter's i32 0/1 rather than a bare i1.
		//
		// Ask the SAME road an `if` test asks. `asI1` compares the lowered word against zero,
		// which is right for a number and wrong for anything whose value is a HANDLE or an
		// INTERNED INDEX: `not "x"` tested the intern slot, said "that word is zero", and
		// answered True where the reference answers False — at exit 0, while `if "x":` beside it
		// answered correctly (roadmap Gap R.183).
		//
		// It is asked BEFORE the operand is lowered, because `not` never needs the operand's VALUE.
		// Lowering one first ran the operand twice — print(not boom()) printed "boom" twice at exit 0
		// (roadmap Gap R.365, ADR 0393) — and for a name the pair road bound it produced the payload word
		// this road then had to ignore: the same one-word read that leaves a ternary arm printing a heap
		// handle (roadmap Gap R.367).
		if n.Op == "not" {
			tv, terr := g.truthyValue(b, n.X)
			if terr != nil {
				return "", terr
			}
			p := g.asI1(b, tv)
			if p == "true" {
				return "0", nil
			}
			if p == "false" {
				return "1", nil
			}
			notP := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = xor i1 %s, true\n", notP, p))
			g.markI1(notP)
			return g.asBoolI32(b, notP), nil
		}
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
		default:
			return "", fmt.Errorf("codegen: unsupported unary %q", n.Op)
		}
		return t, nil
	case *CondExpr:
		// The answer's word is the arms' word. A ternary whose arms are doubles is chosen by a
		// number-typed `select`; one whose arms disagree on the word has no word, and is refused
		// rather than truncated (roadmap L11.6, Gap R.102, ADR 0262 — `ternary_number.go`).
		return g.ternaryI32(b, n)

	case *StrLit:
		// A string *value* in this language is an index into the runtime @str_tab, not the
		// address of a private global: `x == "hi"`, `return "hi"` from a string-returning
		// function, and `self.w = "hi"` all put it in an i32 slot, and emitting `@.str7`
		// there made llc reject the module -- an exit-2 toolchain rejection for an ordinary
		// program (ADR 0166's rule, roadmap Gap R.42 and L11.8). Contexts that genuinely want
		// the bytes -- printf, the compile-time folds in rt_str_* helpers, the traceback
		// strings -- go through g.strConst directly, never through value().
		return g.internStr(b, n.Value), nil
	case *FString:
		// An interpolation is a TEXT value, not only a printing instruction: it builds its parts
		// through the runtime string table and answers an @str_tab index, the same shape a StrLit
		// answers, so a binding, a concatenation, a string method, len, ==, a subscript, a slice, a
		// for-iterable, a return and a string argument all take it (roadmap L11.1, Gap R.146's value
		// half, ADR 0359; the door is pkg/lang/fstring_value.go).
		return g.fstringValue(b, n)
	case *ListLit:
		// inline list literal: emit a dedicated global struct and return its name.
		// Two questions open the heap path, not one: literalNeedsHeap asks whether a payload fits
		// an i32 slot, literalNeedsTags whether a slot can say what it holds. A bool answers the
		// first yes and the second no — the 0/1 fits, and nothing beside it names it — so asking
		// only the first kept [True] on the static path, printing [1] (roadmap Gap R.112, ADR 0259).
		// A tuple is not a second layout, so the same two questions decide its shape as decide a list's;
		// what the tuple adds is that the OBJECT so much as exists carries the bracket flag (heapListFrom /
		// heapListFromTagged) and the print road asks the object rather than the spelling. A tuple of
		// integer constants still folds, because that is how `(1, 2)[0]` and `n = (1, 2)[0]` keep answering
		// from the literal the way the list spelling always did (roadmap L11.3, ADR 0361).
		// A tuple NEVER takes the static shape: the fold `@.lstN = global {i32, [N x i32]}` has no attribute word
		// beside it, and the object's own bit is what answers the bracket question. Handing that GLOBAL to a door
		// that takes a handle (`call i32 @rt_heap_kind(i32 @.lst1)`) is the exit-2 shape ADR 0188 removed for
		// lists, and ADR 0188's rule — the object has to be a heap object because its OWN bits answer — applies to
		// a bit the list never had (roadmap L11.3, ADR 0361).
		if g.literalNeedsHeap(n) || g.literalNeedsTags(n) || g.literalHasPairElement(n) || n.IsTuple() {
			// A literal that mixes kinds is not a refusal when every slot can be tagged: the tags
			// carry the meaning the container-wide kind used to. The list branch asked this later
			// than the dict and set branches did, so `f([1, "a"])` was refused where `f({1: "a"})`
			// was built (roadmap L11.1, ADR 0184's rule applied in one place rather than two).
			if literalMixedKinds(n) && !g.taggableMixedList(n) {
				return "", mixedKindErr("list")
			}
			return g.heapListFrom(b, n, "")
		}
		name, err := g.emitList(n)
		if err != nil {
			return "", err
		}
		return name, nil
	case *DictLit:
		// A literal with string keys or values cannot be the static {count, keys, vals}
		// global — that layout is i32-only — so build a heap dict and intern (Gap J.6).
		// A dict that mixes kinds is not a refusal when every slot can be tagged: it is a
		// dict with no kind, and the tags carry the meaning (ADR 0232).
		if g.literalNeedsHeap(n) || g.literalNeedsTags(n) || g.literalHasPairElement(n) {
			if literalMixedKinds(n) && !g.taggableMixedDict(n) {
				return "", mixedKindErr("dict")
			}
			return g.heapDictFrom(b, n, "")
		}
		name, err := g.emitDict(n)
		if err != nil {
			return "", err
		}
		return name, nil
	case *SetLit:
		// The set gate is the dict gate: a bool's 0/1 fits an i32 slot perfectly and has no way
		// to say what it is, so the static global path has to hand it to the tagged builder.
		// Asking literalNeedsHeap alone refused {True, 1} as "either strings or numbers".
		if g.literalNeedsHeap(n) || g.literalNeedsTags(n) || g.literalHasPairElement(n) {
			if literalMixedKinds(n) && !g.taggableMixedSet(n) {
				return "", mixedKindErr("set")
			}
			return g.heapSetFrom(b, n, "")
		}
		name, err := g.emitSet(n)
		if err != nil {
			return "", err
		}
		return name, nil
	case *Slice:
		return g.valueSlice(b, e, n)
	case *Index:
		return g.valueIndex(b, e, n)
	case *Comp:
		return g.comp(b, n)
	case *Generator:
		return g.genExpr(b, n)
	case *AwaitExpr:
		// await e: codegen runs eagerly; await reduces to evaluating e.
		return g.value(b, n.Expr)
	case *Call:
		return g.call(b, n)
	case *KeywordArg:
		return g.value(b, n.Value)
	case *Lambda:
		// A `lambda` in the same position as a function's name: `print(lambda x: x)` emitted
		// `printf(…, i32 lambda_0)` — the closure's function *global* used as a number, the instruction
		// family ADR 0271 deleted for containers and Gap R.151 measured for `-(lambda x: x)`. A lambda
		// has no value to read until a call site binds it, and only a call site can supply one, so the
		// read refuses rather than handing out the address-like name. A lambda that is *called* —
		// `g = lambda x: x * 3` then `g(4)`, and a lambda passed as an argument — never reaches this
		// line, and both engines answer those (roadmap Gap R.150, Gap R.151, ADR 0283).
		// (The callable positions — a lambda called directly, bound as a decorator, or handed to the
		// closure road — register their FuncDef at their own sites and never come through here.)
		return "", fmt.Errorf("codegen: a lambda used as a value has no compiled representation: it is declared, not assigned, so there is no slot to read and no number, text or container to be — the reference answers `print(lambda x: x)` with a function object and the reference prints a function object, while a lambda that is called (`g = lambda x: x * 3` / `g(4)`, or passed to a function) is answered by the compiled backend too (roadmap Gap R.150, Gap R.151, ADR 0283)")
	case *ChainCompare:
		// The comparison roads read g.numCtx to know which operator an operand is being lowered for;
		// a chain must set it, or a container subscript inside a chain loses the answer its
		// single-comparison form had (roadmap L12.1 / Gap R.53, ADR 0288).
		prev := g.numCtx
		if len(n.Ops) > 0 {
			g.numCtx = &BinOp{Op: n.Ops[0], Src: n.Src}
		}
		out, err := g.chainValue(b, n)
		g.numCtx = prev
		return out, err
	default:
		return "", fmt.Errorf("codegen: unsupported expression %T", e)
	}
}

// valueName is the road a name takes: a slot the module owns, a parameter, a constant the checker
// already read, or a builtin that survived shadowing.
func (g *irGen) valueName(b *strings.Builder, e Expr, n *Name) (string, error) {
	if g.taggedVars[n.Value] {
		// The value slot only means something together with its tag, and this
		// context wants a number, not a (value, tag) pair (ADR 0185).
		return "", g.taggedVarErr(n.Value)
	}
	// A function's name and an imported module are not variables: a `def` declares a global
	// `@gy_f` and allocates no slot, so reading the name as a value emitted
	// `load i32, i32* %_f` for a slot nothing ever wrote and `llc-20` rejected the module with
	// "use of undefined value" — exit 2, the exit-code contract's "the compiler is broken" code,
	// spent on `f + 1`, on `xs = [f]`, on `str(f)` (roadmap Gap R.150, Gap R.151, ADR 0283).
	//
	// Refusing here, at the read, is the one place that covers every road: the arithmetic roads ask
	// the operand later, and by then the load is already in the module. A callable the program put
	// in a *variable* is untouched — `params`, `boundFlags` and the container records all answer
	// before this line, so a name the program assigned keeps its meaning, and only a name that is
	// purely a declaration is refused.
	if kindName, isValue := g.nameIsAValueWithNoSign(n.Value); isValue {
		return "", fmt.Errorf("codegen: %q names %s, which the compiled backend has no value for: a %s is declared rather than assigned, so it has no slot to read and no number, text or container to be — the reference answers `%s` with a %s object and the reference prints it, while passing one to a function is answered by the compiled backend too (roadmap Gap R.150, Gap R.151, ADR 0283)", n.Value, kindName, kindName, n.Value, kindName)
	}
	// String variables are compile-time constants (strVals); emit their
	// global pointer so printf/assign via value() sees the real string.
	if sv, ok := g.strVals[n.Value]; ok {
		// Same rule as a literal: the folded constant is still a string *value*, and a
		// string value is an @str_tab index (Gap R.42, ADR 0224).
		return g.internStr(b, sv), nil
	}
	// A module-level class name used as a value (e.g. `Alias = Point`)
	// resolves to its class id so aliases can be stored and matched.
	if g.classIDs[n.Value] != 0 || g.classInfos[n.Value] != nil {
		return fmt.Sprintf("%d", g.classIDs[n.Value]), nil
	}
	// A module function body may reference a folded module-global
	// constant by its bare name (captured like a closure env). Resolve
	// it to the folded constant unless a parameter shadows it.
	if g.curModGlobals != nil {
		if folded, ok := g.curModGlobals[n.Value]; ok && !g.curModParams[n.Value] {
			return g.value(b, folded)
		}
	}
	// Comprehension variable bound to a compile-time constant.
	if v, ok := g.constBindings[n.Value]; ok {
		return fmt.Sprintf("%d", v), nil
	}
	if reg, ok := g.params[n.Value]; ok && !g.paramSlot[n.Value] {
		// The incoming register is the value only until the body rebinds the name;
		// a rebound parameter reads its slot, which the entry copied over (Gap R.3).
		return reg, nil
	}
	// A slot the checker could not prove was written carries a flag, and the read tests it: an
	// unwritten local raises UnboundLocalError here, catchably, instead of loading whatever the
	// frame happened to hold (roadmap Gap R.36, ADR 0228).
	g.checkBound(b, n.Value, n.Span())
	// Always load fresh from the alloca so the value dominates its use.
	g.ldN++
	if off, ok := g.envCaptures[n.Value]; ok && g.envMode {
		return g.emitEnvLoad(b, g.envParam, off), nil
	}
	// An unbound name would emit a load from a slot that was never allocated, and
	// LLVM's module verifier would reject it — which the exit-code contract then
	// reports as a *compiler bug* for what is an ordinary typo (roadmap Gap K.10).
	// The checker catches this today; this is the safety net that keeps the next
	// checker hole from surfacing as "input module is broken" (ADR 0166).
	if sym, ok := g.moduleSlots[n.Value]; ok && (!g.inFunc || !(g.allocd[n.Value] || (g.funcLocals != nil && g.funcLocals[n.Value]))) {
		// Read the module's own global: whatever the module assigned most recently, not what
		// this frame happened to see when it was compiled (ADR 0227).
		// No rooting: a module slot holds an i32 value (int, interned string index, or the
		// None handle, which the runtime allocates once and never frees), never a collectable
		// heap pointer -- floats and containers are excluded from moduleSlots for that reason.
		g.ldN++
		b.WriteString(fmt.Sprintf("  %%l%d = load i32, i32* @%s\n", g.ldN, sym))
		return fmt.Sprintf("%%l%d", g.ldN), nil
	}
	if g.inFunc {
		// The module is a scope too (ADR 0220), and a compiled body reaches it through these
		// two tables. A name bound to a literal the module never rebinds is a value, so it
		// needs no slot -- `MAX = 40` then `def f(): return MAX` answers 40 rather than refusing
		// or, as it did before, answering 0 through an empty closure body (ADR 0227).
		// A binding inside the body wins: `K = 1` in a function is a local even when the module
		// also binds K (ADR 0220), and reading it from the module would answer 5 for 1.
		if folded, ok := g.moduleConsts[n.Value]; ok && !(g.allocd[n.Value] || (g.funcLocals != nil && g.funcLocals[n.Value])) {
			return g.value(b, folded)
		}
		if g.moduleFloatOwns(n.Value) {
			// The module holds this name's double, and this position keeps ONE i32 word for a value:
			// the roads that read a number ask `isFloat` and take the float road, which loads the
			// module's global. A road that reaches here has not asked, and answering it with the double
			// register would hand `llc` a `mul i32 … <double>` — exit 2, the code the exit-code contract
			// reserves for a compiler bug (ADR 0166). So the name is refused here, by name, with the
			// road that owns it (roadmap Gap R.35, `module_float.go`).
			return "", g.refuseModuleFloat(n.Value)
		}
		if os.Getenv("GY_DEBUG_MF") != "" && g.moduleNames[n.Value] {
			fmt.Fprintf(os.Stderr, "[read] %q inFunc=%v slots=%q allocd=%v funcLocals=%v params=%v\n", n.Value, g.inFunc, g.moduleFloatSlots[n.Value], g.allocd[n.Value], g.funcLocals != nil && g.funcLocals[n.Value], func() string {
				s := ""
				for k := range g.params {
					s += k + ","
				}
				return s
			}())
		}
		if g.moduleNames[n.Value] && !(g.allocd[n.Value] || (g.funcLocals != nil && g.funcLocals[n.Value])) {
			return "", fmt.Errorf("codegen: %q is bound at module level, and a compiled function body cannot read module-level state that changes: only a literal the module never rebinds is visible here (CPython answers this program; compiled module globals are roadmap Gap R.35)", n.Value)
		}
	}
	if !g.nameIsBound(n.Value) {
		return "", fmt.Errorf("codegen: undefined name %q (no binding for it; assign it before use) — the reference reports the same error", n.Value)
	}
	ld := fmt.Sprintf("%%_%s.ld%d", n.Value, g.ldN)
	if g.unionVars[n.Value] {
		ui := fmt.Sprintf("%%_%s.ui%d", n.Value, g.ldN)
		b.WriteString(fmt.Sprintf("  %s = getelementptr %%unionbox, %%unionbox* %%_%s, i32 0, i32 1\n", ui, n.Value))
		b.WriteString(fmt.Sprintf("  %s = load i32, i32* %s\n", ld, ui))
		return ld, nil
	}
	b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s\n", ld, n.Value))
	return ld, nil
}
