// The expression roads: irGen.value dispatches an Expr to the road that knows its answer.
package lang

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
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
			objHandle, err := g.value(b, n.Obj)
			if err != nil {
				return "", err
			}
			g.heapUsed = true
			slot := g.attrSlot(n.Name.Value)
			ret := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 %d)\n", ret, objHandle, slot))
			return ret, nil
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
		// `and`/`or` are not operators here: they are the two operators that choose an operand and hand it
		// back unconverted, so the question is *which value*, never *what verdict* (roadmap Gap R.147,
		// ADR 0269). The door folds a test the source wrote, branches on the operand the program wrote, and
		// merges the two arms in a phi (Gap R.149, ADR 0275) — and it has to be asked **before the operands
		// are lowered**, exactly like the membership and tagged-equality doors below it. Sitting down here
		// meant the generic lowering had already emitted both operands — which is why `boom() and 2` called
		// `boom` twice on the compiled leg and `x and boom()` called it once with `x` false: an operand the
		// test had already excluded was in the module, and the module ran it.
		if n.Op == "and" || n.Op == "or" {
			return g.logicValue(b, n)
		}
		// `%` on a text is not the remainder: it is the formatting, and it has to be asked **before the
		// operands are lowered** for the same reason the membership doors below it are — the numeric road
		// lowers a text as its interned index and then does arithmetic on it, which is exactly the `0.0`
		// that `"%.2f" % 3.5` used to print (roadmap Gap R.165, ADR 0282). The door is asked for every `%`
		// and answers nothing at all when the left side is not a text, so an ordinary remainder keeps the
		// road it has always had (roadmap Gap R.31, ADR 0360).
		if n.Op == "%" {
			// The plan is asked, not the kind predicate: a conversion this door declines still OWNS the
			// expression, and the refusal it gives names the character and the argument. Falling through to
			// the generic text-`%` refusal would tell the reader the operator is missing, which it is not
			// (roadmap Gap R.31, ADR 0360's one-question rule).
			if plan := g.percentFormatOf(n); plan.applies {
				return g.percentFormat(b, n)
			}
		}
		// Membership in a container whose slots describe themselves is decided by (payload,
		// tag): comparing payloads alone would answer `0 in {"0": 1}` from the interned index
		// the key's word happens to hold (ADR 0232's soundness rule). This is checked before
		// the operands are lowered, because a tagged loop variable has no untagged lowering at
		// all — `for x in s: x in t` is the shape this exists for.
		if n.Op == "in" || n.Op == "not in" {
			// `[1] in {"a": 1}` is not a question the reference allows: an unhashable needle is
			// a TypeError naming its type, and the payload-only scan answered it False at exit 0
			// (roadmap Gap R.217, ADR 0328). Asked first, before any fold and before the tagged
			// membership door, so no arm of the test gets to answer it.
			if g.haystackHashesKeys(n.R) {
				if res, bad := g.unhashableKeyGate(b, n.L, n.Span()); bad {
					return res, nil
				}
			}
			if res, ok, err := g.mixedMembership(b, n); err != nil {
				return "", err
			} else if ok {
				return res, nil
			}
		}
		// `if x == "a":` where x came from a loop over a container that mixes kinds: the
		// comparison is between a (payload, tag) pair and a value whose kind the compiler
		// knows, and the tag has to take part or `x == 1` answers true for the string whose
		// interned index happens to be 1.
		if n.Op == "==" || n.Op == "!=" {
			if res, ok, err := g.mixedTaggedCompare(b, n); err != nil {
				return "", err
			} else if ok {
				return res, nil
			}
		}
		// Operator overloading: dispatch dunder methods on statically-known
		// class instances before falling back to builtin arithmetic.
		if res, ok := g.emitDunderBinOp(b, n); ok {
			return res, nil
		}
		if g.isFloat(n.L) || g.isFloat(n.R) || g.taggedNumberUseApplies(n) {
			// Which question is this? Two operands of different runtime kinds are not equal
			// (ADR 0215's rule; ADR 0221 makes the numeric cross-kind pair the exception), and
			// CPython answers `1.0 == [1]` with False without ever converting the list. Handing
			// it to the float path instead emitted `sitofp i32 @.lst1 to double` -- a container
			// global fed to a float conversion -- and llc rejected the module, so the program
			// got a toolchain rejection for asking an ordinary question (Gap R.40, ADR 0166).
			other := n.R
			if !g.isFloat(n.L) {
				other = n.L
			}
			if g.isContainerExpr(other) && !(g.isContainerExpr(n.L) && g.isContainerExpr(n.R)) {
				switch n.Op {
				case "==", "!=", "is", "is not":
					eq := n.Op == "!=" || n.Op == "is not"
					v := "0"
					if eq {
						v = "1"
					}
					return v, nil
				default:
					return "", fmt.Errorf("codegen: ordering a number against a %s is a TypeError this backend cannot raise at runtime (roadmap Gap R.37)", exprTyName(other))
				}
			}
			switch n.Op {
			case "==", "!=", "<", "<=", ">", ">=":
				res := g.floatBinOp(b, n)
				if g.floatUnlowerable != "" {
					return "", g.floatLoweringRefusal(n)
				}
				return res, nil
			}
		}
		// A numeric use of a slot whose kind the object carries, in either direction. The float arms are
		// the only ones that can ask the tag what the payload means, and the read brings its
		// (payload, tag) pair to them: a float slot unboxes, an int or bool slot converts, and a slot
		// holding text, None or a container raises the TypeError CPython raises for this operator and
		// that kind. The gate sits above the ordinary arithmetic lowering on purpose — that path reads
		// the payload as a number, which is what this door exists to stop (roadmap L11.1, Gap R.88).
		// A negative base to a fractional exponent is a COMPLEX number in the reference
		// (`(-8) ** (1/3)` is `(1.0000000000000002+1.7320508075688772j`). This language has no complex
		// value, so the shape is refused in words rather than answered with `nan`, which is what
		// `llvm.pow.f64` returns and what both backends printed at exit 0 (Gap R.176, ADR 0293).
		if n.Op == "**" && powerComplexIsAsked(n.L, n.R, g.powerReaders()) {
			return "", fmt.Errorf("`%s` is a negative base raised to a fractional power, which the reference answers with a complex number; this language has no complex value, and `llvm.pow.f64` would answer NaN (roadmap Gap R.176, ADR 0166)", exprSurface(n))
		}
		if g.taggedNumberUseApplies(n) {
			switch n.Op {
			case "+", "-", "*", "/", "//", "%", "**":
				if g.doubleDomain == 0 {
					// The arms answer this with a double and nobody upstream asked for one: whoever
					// receives it would type an i32 instruction, a call or a container slot with it,
					// which is the module llc rejects. Refuse the use instead (roadmap Gap R.88).
					return "", g.doubleDomainRefusal(n)
				}
				res := g.floatBinOp(b, n)
				if g.floatUnlowerable != "" {
					return "", g.floatLoweringRefusal(n)
				}
				return res, nil
			}
		}
		// None equality. Values are untagged i32s here, so `x == None` is decided the way
		// the other dynamic-looking comparisons are: statically, from what the source says
		// each side is. Two Nones are equal, None and anything else are not — comparing a
		// None handle against the integer 0 would have made `0 == None` true (ADR 0172).
		if n.Op == "==" || n.Op == "!=" || n.Op == "is" || n.Op == "is not" {
			ln, rn := g.isNoneExpr(n.L), g.isNoneExpr(n.R)
			if ln || rn {
				// Both sides still have to be evaluated: `print(f() == None)` runs f(),
				// and f may print. Deciding the result statically must not delete the
				// operand's effects.
				if _, err := g.value(b, n.L); err != nil {
					return "", err
				}
				if _, err := g.value(b, n.R); err != nil {
					return "", err
				}
				eq := ln && rn
				if n.Op == "!=" || n.Op == "is not" {
					eq = !eq
				}
				v := "0"
				if eq {
					v = "1"
				}
				return v, nil
			}
		}

		// Constant string concatenation: fold "a" + "b" (and foldable string
		// calls like str(7)) into a single string global.
		if n.Op == "+" {
			ls, lok := g.stringVal(n.L)
			rs, rok := g.stringVal(n.R)
			if lok && rok {
				// A folded concatenation is still a string *value*, so it is an @str_tab
				// index; the pointer belongs only to the printf paths that ask for bytes
				// (Gap R.42, ADR 0224).
				return g.internStr(b, ls+rs), nil
			}
		}
		// A container is equal to another container by *value*. Both backends compared the
		// two i32s instead, which compared heap slots: xs == ys was False for two equal
		// lists, [1] == [1] was False, and a literal operand made the module invalid (the
		// static @.lstN global in an icmp). rt_container_eq walks the (payload, tag) pairs
		// the way Python's __eq__ walks elements (roadmap L11.1, ADR 0189). `is` stays
		// identity, which is what Python's `is` is for containers.
		if (n.Op == "==" || n.Op == "!=") && (g.isContainerExpr(n.L) || g.isContainerExpr(n.R)) {
			return g.containerEquality(b, n)
		}
		// `2 in xs[0]`: the haystack is a read that names a container, so its handle comes out of
		// the slot rather than out of the value path, which has no single kind to name it with. The
		// needle arrives as a (payload, tag) pair for the same reason the container's own elements
		// are stored that way — membership is the payload-and-tag question, one level out
		// (roadmap L11.1, ADR 0241).
		if n.Op == "in" || n.Op == "not in" {
			if _, isRead := n.R.(*Index); isRead {
				if res, okM, mErr := g.membershipOfReadHaystack(b, n); mErr != nil {
					return "", mErr
				} else if okM {
					return res, nil
				}
				// The haystack is a slot the program built rather than wrote: the tag beside its payload is
				// the only thing that says whether to walk slots, keys, members or bytes at all, so the
				// question goes to the object. Until this line the test either refused in words or — worse,
				// at the exit code of success — read a number's payload as a heap handle and answered False
				// where the reference raises (roadmap L11.1, Gap R.95, ADR 0251's rule for this caller).
				if res, okM, mErr := g.membershipOfBuiltSlotHaystack(b, n); mErr != nil {
					return "", mErr
				} else if okM {
					return res, nil
				}
			}
		}
		// An ordering with a slot on one side is answered **before the operands are lowered**, because the
		// ordinary lowering of that read is the thing that refuses: `xs[1] > "a"` over a list that can hold
		// both a number and a text has to ask the tag which pair this is — two numbers, two texts, or the
		// TypeError CPython raises — and an icmp on the two payloads would answer all three with whichever
		// number happens to sit in the slot (roadmap L11.1, Gap R.82; the same hoisting ADR 0248 had to
		// learn for text, for the same reason).
		if g.taggedOrderApplies(n) {
			res, _, ook, oerr := g.emitTaggedOrder(b, n)
			if oerr != nil {
				return "", oerr
			}
			if ook {
				return res, nil
			}
		}
		if i1, okPair, perr := g.pairOrder(b, n); perr != nil {
			return "", perr
		} else if okPair {
			// A comparison is a *value* here (printable, storable), so the predicate is widened to the
			// interpreter's i32 0/1 the same way the doors beside it do; the i1 stays internal.
			res := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", res, i1))
			return res, nil
		}
		// `7 in build()` — the haystack is the container the callee built, whose word the ordinary road
		// cannot keep. The membership test is asked again of the name the container door materialised for
		// it: every road below already reads a container variable, and a container answer is precisely one
		// of those (roadmap Gap R.67, ADR 0333). The needle keeps its own lowering, so a container needle
		// beside an answer still takes the tagged slot rule.
		//
		// The door is asked before `isContainerExpr` is, because the question the two answer differs: the
		// notebooks know a *described* answer, while the door also takes an answer the scan proved a
		// container and the slot table declined — and `5 in f()` asked the object and answered correctly
		// before this row existed. Losing that answer to a refusal would be the fix costing more than the
		// bug (roadmap Gap R.261).
		if n.Op == "in" || n.Op == "not in" {
			if ansName, _, okAns := g.containerAnswerOperandRead(b, n.R, containerReadMembers); okAns {
				redo := *n
				redo.R = ansName
				return g.value(b, &redo)
			}
			if fnName, refused := g.containerAnswerReadIsRefused(n.R, containerReadMembers); refused {
				// The haystack is a container this backend proved and could not describe, and its keys or
				// elements are the undescribed half — so the test below would walk slots it cannot name and
				// answer with whatever the allocator wrote. Refuse rather than answer (roadmap Gap R.261).
				return "", containerAnswerUndescribedRefusal(fnName)
			}
		}
		l, err := g.value(b, n.L)
		if err != nil {
			return "", err
		}
		r, err := g.value(b, n.R)
		if err != nil {
			return "", err
		}
		// `x in xs` compares against the words stored in the container, so a string needle
		// must become its @str_tab index: passing @.strN to rt_contains(i32, i32) is the
		// shape LLVM rejects (roadmap Gap I.2).
		// An ordering of two texts is answered here, before the refusal below: the operands are
		// already lowered, and the question is strcmp's, not the interned index's (roadmap Gap R.84).
		if g.textOrderOperands(n) {
			res, _, oerr := g.emitTextOrderOperands(b, n.Op, l, r)
			if oerr != nil {
				return "", oerr
			}
			return res, nil
		}
		// Arithmetic on a value that is really an index into the string table would compute a
		// number from an address-like word (`s + 1` on a string parameter returned 1, where the
		// interpreter raises TypeError). Refuse it as a compile diagnostic instead (Gap J.5).
		switch n.Op {
		case "+", "-", "*", "/", "//", "%", "**", "<", ">", "<=", ">=":
			// A function's name, an imported module, or an inline `lambda` in an arithmetic operand is
			// the same class of mistake as a text there, and was worse: `f + 1` emitted
			// `load i32, i32* %_f` — a slot nothing allocated, because a function is declared rather than
			// assigned — and `llc-20` answered "use of undefined value", spending the contract's exit 2 on
			// a program the reference stops with one sentence. The raise names the operand's kind in
			// CPython's wording, source-ordered, so the program can catch what it actually did
			// (`except TypeError:` runs on both engines). (roadmap Gap R.150, Gap R.151, ADR 0283)
			// A CONTAINER in a numeric operand is the same class of mistake, and was worse than the
			// function case beside it: `value()` renders a list literal as the ADDRESS of a compile-time
			// global, so `print([0] * 3)` emitted `mul i32 @.lst1, 3`, `print([1, 2] + [3])` emitted
			// `add i32 @.lst1, @.lst2`, and `print([1] / 2)` reached the double road with
			// `sitofp i32 @.lst1 to double`. llc rejected the compiler's own module each time, and the
			// exit-code contract spends exit 2 on OUR bug, not the program's (ADR 0166).
			//
			// Two answers, split by what the REFERENCE does rather than by what this road lacks:
			//   - where CPython raises (`[1] - [2]`, `{1: 2} * 2`, `[1] + {}`, `[] < {}`), raise its own
			//     sentence, so `except TypeError:` runs on both engines (ADR 0215);
			//   - where CPython ANSWERS (`[0] * 3`, `[1, 2] + [3]`, `[1] < [2]`), decline in words — the
			//     backend builds no runtime list-repeat or concatenate helper, and a container global in
			//     an arithmetic operand is the invalid IR this row exists to remove. Answering a number
			//     there would be the wrong-number class ADR 0166 counts as our bug.
			//
			// A PARAMETER and a subscript are deliberately NOT claimed here: their kind belongs to the
			// caller and to the slot roads, which already answer (`def half(xs): return xs[0] / 2` with
			// `half([1.5])` = 0.75) or refuse with a message naming the slot they could not read. An
			// answer may not become a refusal, and a precise diagnostic may not become a vague one.
			for _, side := range []Expr{n.L, n.R} {
				if !g.arithOperandIsContainer(side) {
					continue
				}
				other := "int"
				if k := g.numericUseKind(otherOperand(n, side)); k != "" {
					other = k
				}
				// `list + dict` is `can only concatenate list (not "dict") to list`, and naming the other
				// side "int" there would describe a program the author did not write (Gap R.38).
				if ok := g.containerKindName(otherOperand(n, side)); ok != "" {
					other = ok
				}
				if n.Op == "*" {
					// `*`'s sentence quotes the MULTIPLIER's type (`can't multiply sequence by non-int
					// of type 'str'`), and the two lookups above answer "" or "int" for a text, a float
					// and None — which would print a type the author never wrote (Gap R.38).
					other = g.multiplierKindName(otherOperand(n, side))
				}
				if g.containerRaisesForPair(n.Op, n.L, n.R) {
					class, msg := containerNumberOp(n.Op, other, g.containerKindName(n.L), g.containerKindName(n.R))
					g.raiseTo(b, exnCode(class), class, msg, n.Span())
					return "0", nil
				}
				// Two same-kind containers in a COMPARISON are the tagged order door's question, and it
				// answered `a = [1]` / `b = [2]` / `print(a < b)` before this row existed: declining here
				// turned that True into a refusal, which the ladder forbids. Claim only what this road
				// would otherwise put a container global into — an arithmetic operator, or a comparison
				// over LITERALS that no other door lowered.
				if isCompareOpForContainer(n.Op) && !exprIsContainerShape(n.L) && !exprIsContainerShape(n.R) {
					continue
				}
				return "", fmt.Errorf("`%s` has no compiled lowering: the backend builds no runtime list-concatenate, sequence-repeat or container-ordering helper, and a container lowers to the address of a compile-time global rather than a number this road can carry — CPython answers this program, and the compiled leg waits for the tagged value word (roadmap L11.1, Gap R.175, ADR 0166)", exprSurface(n))
			}
			for _, side := range []Expr{n.L, n.R} {
				kindName, isValue := g.valueWithNoSign(side)
				if !isValue {
					continue
				}
				other := "int"
				if k := g.numericUseKind(otherOperand(n, side)); k != "" {
					other = k
				}
				var class, msg string
				if n.L == side {
					class, msg = unsupportedNumberOp(n.Op, kindName, other)
				} else {
					class, msg = unsupportedNumberOp(n.Op, other, kindName)
				}
				g.raiseTo(b, exnCode(class), class, msg, n.Span())
				return "0", nil
			}
			isStrOperand := func(e Expr) bool {
				if _, ok := g.stringVal(e); ok {
					return true
				}
				return g.printsAsInternedStr(e)
			}
			if (isStrOperand(n.L) || isStrOperand(n.R)) && n.Op != "+" {
				// The message has to say what is missing rather than claim orderings on text have no
				// meaning: an ordering of two texts the compiler can see is answered by strcmp since
				// ADR 0248, and what reaches this line is arithmetic, or an ordering with an operand
				// whose textness the compiler cannot prove — a parameter, typically, whose kind the call
				// sites do not agree on in a way this pass can carry into the body (Gap R.38: a refusal
				// that claims something false about the language is its own defect).
				return "", fmt.Errorf("codegen: operator %q on a string (%s) is not supported in the AOT backend; CPython evaluates it — a compiled string is an interned table index, so arithmetic on it has no meaning, and an ordering of two texts is answered only where the compiler can see both sides are text (roadmap Gap R.82)", n.Op, exprSnippet(n.L))
			}
			if n.Op == "+" && (isStrOperand(n.L) || isStrOperand(n.R)) {
				if _, ok := g.stringVal(n.L); ok {
					if _, ok2 := g.stringVal(n.R); ok2 {
						break // both constant: folded below
					}
				}
				// Building a string while the program runs is a buffer, an intern, and the
				// same index convention (ADR 0229): the operands' values are indices, the
				// result is an index, and print/==/in/container slots keep working on it.
				ls, lok, lerr := g.strReg(b, n.L)
				if lerr != nil {
					return "", lerr
				}
				rs, rok, rerr := g.strReg(b, n.R)
				if rerr != nil {
					return "", rerr
				}
				if lok && rok {
					return g.rtStrCall(b, "rt_str_cat", "i32 "+ls, "i32 "+rs), nil
				}
				if lok != rok {
					return "", fmt.Errorf("codegen: concatenating a string with a value that is not a string is not supported in the AOT backend; CPython raises TypeError for it")
				}
				return "", fmt.Errorf("codegen: concatenating a runtime string is not supported in the AOT backend yet; CPython answers it — building a new string needs a buffer allocation (roadmap Gap J.5)")
			}
		}
		// `s == "yes"` where s is a string parameter, a container element, or the result of a
		// string-returning call compares @str_tab indices: interning makes equal content the
		// same index, so this is content equality without a character loop (roadmap Gap J.5).
		// The literal side becomes its index; the interned side is already one.
		if n.Op == "==" || n.Op == "!=" {
			internSide := func(e Expr) (string, bool) {
				txt, ok := g.stringVal(e)
				if !ok {
					return "", false
				}
				g.heapUsed = true
				t := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", t, g.strConst(txt), g.strConst(pyReprString(txt))))
				return t, true
			}
			litSide, otherSide := Expr(nil), Expr(nil)
			if _, ok := g.stringVal(n.L); ok && g.printsAsInternedStr(n.R) {
				litSide, otherSide = n.L, n.R
			} else if _, ok := g.stringVal(n.R); ok && g.printsAsInternedStr(n.L) {
				litSide, otherSide = n.R, n.L
			}
			if litSide != nil {
				if iv, ok := internSide(litSide); ok {
					if litSide == n.L {
						l = iv
					} else {
						r = iv
					}
				}
				_ = otherSide
			}
		}
		if n.Op == "in" || n.Op == "not in" {
			if needle, ok := g.stringVal(n.L); ok {
				g.heapUsed = true
				it := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", it, g.strConst(needle), g.strConst(pyReprString(needle))))
				l = it
			}
		}
		// Constant folding: fold integer literals at compile time.
		// Constant-fold string equality/inequality: compare contents, not refs.
		if n.Op == "==" || n.Op == "!=" {
			ls, lok := g.stringVal(n.L)
			if lok {
				rs, rok := g.stringVal(n.R)
				if rok {
					res := 0
					if ls == rs {
						res = 1
					}
					if n.Op == "!=" {
						res = 1 - res
					}
					return fmt.Sprintf("%d", res), nil
				}
			}
		}
		if li, lok := n.L.(*IntLit); lok {
			if ri, rok := n.R.(*IntLit); rok && !g.isFloat(n) {
				lv, rv := int64(li.Value), int64(ri.Value)
				var res int64
				folded := false
				switch n.Op {
				case "+":
					res, folded = lv+rv, true
				case "-":
					res, folded = lv-rv, true
				case "*":
					res, folded = lv*rv, true
				case "/", "//":
					// `//` folds to the *floor* quotient (`/` never reaches here: true
					// division is always a float). A folder that truncates would be a
					// third opinion disagreeing with both backends (Gap R.28, R.30).
					if rv != 0 {
						if n.Op == "//" {
							res, folded = floorDiv(lv, rv), true
						} else {
							res, folded = lv/rv, true
						}
					}
				case "%":
					if rv != 0 {
						res, folded = floorMod(lv, rv), true
					}
				case "**":
					if rv >= 0 {
						// The fold has to see the overflow the binary-exponentiation loop hid: `2 ** 62`
						// folded to `0` because the multiply wrapped past 1<<62 in silence, and `0` is a
						// number the reference never produces. Refusing the fold keeps the value honest
						// and lets the runtime guard below answer in words (Gap R.176, ADR 0293).
						acc := new(big.Int).Exp(big.NewInt(lv), big.NewInt(rv), nil)
						// The WORD the backend actually has — an i32 (Gap R.133) — not int64: `2 ** 31`
						// folded to -2147483648 through this line, and a negative answer to a positive
						// exponent is a number the reference never produces.
						if !acc.IsInt64() || acc.Int64() < math.MinInt32 || acc.Int64() > math.MaxInt32 {
							folded = false
							break
						}
						res = acc.Int64()
						folded = true
					}
				case "and", "or":
					// The folder used to answer the verdict — `2 and 3` became the 1 the operator never
					// returns — so every index, repeat count and constant argument built from `and`/`or`
					// was computed from a number the language does not have. One door for both backends and
					// both folders: logicFoldConst (roadmap Gap R.147, ADR 0269).
					folded = true
					res = logicFoldConst(lv, n.Op, rv)
				case "==":
					folded = true
					if lv == rv {
						res = 1
					}
				case "<":
					folded = true
					if lv < rv {
						res = 1
					}
				case "<=":
					folded = true
					if lv <= rv {
						res = 1
					}
				case ">":
					folded = true
					if lv > rv {
						res = 1
					}
				case ">=":
					folded = true
					if lv >= rv {
						res = 1
					}
				}
				if folded {
					return fmt.Sprintf("%d", res), nil
				}
			}
		}
		t := g.newTmp()
		// Membership tests need runtime container access, so handle them
		// separately from the i32 arithmetic/comparison ops.
		if n.Op == "in" || n.Op == "not in" {
			// An instance is not iterable, and it is not a number either. The compiler may know the class
			// from the name or constructor that wrote the value, in which case the raise can name it directly;
			// when the value only carries a tag, the tag asks the object. Either way, asking membership of
			// `C()` is a TypeError that says `C`, not one that says `int` or `object` (Gap R.209).
			if cls := g.instanceExpressionClass(n.R); cls != "" {
				g.raiseTo(b, exnCode("TypeError"), "TypeError", notIterableSentence(cls), n.Span())
				return "0", nil
			}
			if cls := g.instanceExpressionClass(n.L); cls != "" {
				if _, ok := g.stringVal(n.R); ok {
					g.raiseTo(b, exnCode("TypeError"), "TypeError", inStrNeedleSentence(cls), n.Span())
					return "0", nil
				}
				if g.printsAsInternedStr(n.R) {
					g.raiseTo(b, exnCode("TypeError"), "TypeError", inStrNeedleSentence(cls), n.Span())
					return "0", nil
				}
			}
			// Literal container (list/set/dict literal): unroll `l == elem`
			// checks against the constant integer elements/keys. This fixes
			// `x in [1,2,3]` where the container is a compile-time global
			// struct, not a heap handle (the rt_contains path below indexes
			// @heap by the struct address, which is UB).
			if vals, ok := intMembershipValues(n.R); ok && !isStringExpr(n.L) {
				if len(vals) == 0 {
					// Empty container: nothing is contained.
					if n.Op == "not in" {
						return "1", nil
					}
					return "0", nil
				}
				acc := g.newTmp()
				cmp := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = icmp eq i32 %s, %d\n", cmp, l, vals[0]))
				b.WriteString(fmt.Sprintf("\t%s = or i1 false, %s\n", acc, cmp))
				for _, v := range vals[1:] {
					c2 := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = icmp eq i32 %s, %d\n", c2, l, v))
					nacc := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = or i1 %s, %s\n", nacc, acc, c2))
					acc = nacc
				}
				if n.Op == "not in" {
					inv := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = xor i1 %s, true\n", inv, acc))
					acc = inv
				}
				res := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = zext i1 %s to i32\n", res, acc))
				return res, nil
			}
			// A string haystack: `"cat" in greeting` is substring membership, not
			// container membership. Both sides must be @str_tab indices — handing the
			// raw @.strN global to rt_contains(i32, i32) emitted a global in an i32
			// slot, which is invalid IR, so the module died in llc and a plain Python
			// program looked like a compiler bug (ADR 0166).
			if hay, ok := g.stringVal(n.R); ok {
				hn := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = call i32 @rt_str_intern2(i8* %s, i8* %s)\n", hn, g.strConst(hay), g.strConst(pyReprString(hay))))
				sc := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = call i32 @rt_str_contains(i32 %s, i32 %s)\n", sc, hn, l))
				bt := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = icmp ne i32 %s, 0\n", bt, sc))
				g.markI1(bt)
				if n.Op == "not in" {
					inv := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = xor i1 %s, true\n", inv, bt))
					g.markI1(inv)
					return g.asBoolI32(b, inv), nil
				}
				return g.asBoolI32(b, bt), nil
			}
			if g.printsAsInternedStr(n.R) {
				sc := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = call i32 @rt_str_contains(i32 %s, i32 %s)\n", sc, r, l))
				bt := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = icmp ne i32 %s, 0\n", bt, sc))
				g.markI1(bt)
				if n.Op == "not in" {
					inv := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = xor i1 %s, true\n", inv, bt))
					g.markI1(inv)
					return g.asBoolI32(b, inv), nil
				}
				return g.asBoolI32(b, bt), nil
			}
			// l is the value to test, r is the container handle. A comprehension on the right is
			// the container the program means, but the constant path hands back a folded global
			// whose layout is a length plus an array: `rt_contains(i32 @.set1, i32 2)` is a global
			// in an i32 slot, the module llc refuses, so it is materialised into the heap first
			// (ADR 0166's rule one construct on; roadmap Gap J.2, ADR 0234).
			if _, isComp := n.R.(*Comp); isComp || g.isContainerExpr(n.R) {
				ch, chErr := g.containerOperand(b, n.R)
				if chErr != nil {
					return "", chErr
				}
				r = ch
			}
			// A needle that is itself a container is built as an object and matched by the tagged
			// slot rule. Two reasons, both measured: the constant path hands back a folded global
			// whose layout is a length plus an array, which llc refuses in an i32 parameter; and an
			// untagged i32 compare of two slots against a handle would never notice that two
			// literals holding the same elements denote the same value, so [1, 2] in [[1, 2], 3]
			// would answer False forever (roadmap L11.1, ADR 0189).
			if g.isContainerExpr(n.L) {
				nh, nErr := g.containerOperand(b, n.L)
				if nErr != nil {
					return "", nErr
				}
				l = nh
				nt, _ := g.elemKindTag(n.L)
				t := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = call i32 @rt_contains_tagged(i32 %s, i32 %s, i32 %d)\n", t, r, l, nt))
				bt := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = icmp ne i32 %s, 0\n", bt, t))
				g.markI1(bt)
				if n.Op == "not in" {
					inv := g.newTmp()
					b.WriteString(fmt.Sprintf("\t%s = xor i1 %s, true\n", inv, bt))
					g.markI1(inv)
					return g.asBoolI32(b, inv), nil
				}
				return g.asBoolI32(b, bt), nil
			}
			t := g.newTmp()
			b.WriteString(fmt.Sprintf("\t%s = call i32 @rt_contains(i32 %s, i32 %s)\n", t, r, l))
			bt := g.newTmp()
			b.WriteString(fmt.Sprintf("\t%s = icmp ne i32 %s, 0\n", bt, t))
			g.markI1(bt)
			if n.Op == "not in" {
				nt := g.newTmp()
				b.WriteString(fmt.Sprintf("\t%s = xor i1 %s, true\n", nt, bt))
				g.markI1(nt)
				return g.asBoolI32(b, nt), nil
			}
			return g.asBoolI32(b, bt), nil
		}
		var op string
		switch n.Op {
		case "+":
			op = "add"
		case "-":
			op = "sub"
		case "*":
			op = "mul"
		case "/":
			// True division reaches the i32 domain only where no float is expected (a
			// float-valued assignment is emitted in the double domain instead, and prints
			// 3.5 for 7 / 2); the zero guard below is what makes `1 / 0` a trap here.
			op = "sdiv"
		case "//":
			// Floor division, not truncation. `sdiv` truncates toward zero, so the
			// quotient steps down one whenever there is a remainder and the operands
			// have opposite signs — the same rule as floorDiv in the interpreter, said in
			// IR (roadmap Gap R.30: `-7 // 2` compiled to -3 and printed it happily).
			// The zero guard is emitted here as well as for the shared path below, because
			// this case returns before reaching it.
			g.guardNonZeroInt(b, r, n.Span(), "integer division or modulo by zero")
			q := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = sdiv i32 %s, %s\n", q, l, r))
			rm := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = srem i32 %s, %s\n", rm, l, r))
			rne := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", rne, rm))
			ls := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", ls, l))
			rs := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", rs, r))
			diff := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = xor i1 %s, %s\n", diff, ls, rs))
			adj := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", adj, rne, diff))
			qm := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = sub i32 %s, 1\n", qm, q))
			b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, adj, qm, q))
			return t, nil
		case "%":
			// Floor modulo: the remainder carries the divisor's sign, which is what keeps
			// a == (a // b) * b + (a % b) true for every sign combination. `srem` alone
			// truncates (`-7 % 2` answered -1), so add the divisor back when the signs
			// disagree and there is a remainder (roadmap Gap R.28). Guarded here, too:
			// this case returns before the shared `sdiv`/`srem` guard below.
			g.guardNonZeroInt(b, r, n.Span(), "integer modulo by zero")
			rm := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = srem i32 %s, %s\n", rm, l, r))
			rne := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", rne, rm))
			rneg := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", rneg, rm))
			bneg := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp slt i32 %s, 0\n", bneg, r))
			diff := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = xor i1 %s, %s\n", diff, rneg, bneg))
			adj := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = and i1 %s, %s\n", adj, rne, diff))
			sum := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = add i32 %s, %s\n", sum, rm, r))
			b.WriteString(fmt.Sprintf("  %s = select i1 %s, i32 %s, i32 %s\n", t, adj, sum, rm))
			return t, nil
		case "**":
			// Reaching THIS case means the power rule said the answer is an int — a non-negative exponent
			// on int operands. The two cases the reference refuses are guarded rather than run: `0 ** -n`
			// (which pow() answers with `inf` and the old code printed as `2147483647` after `fptosi`
			// saturated) and a base whose magnitude would overflow the word, which used to wrap to 0 in
			// silence (Gap R.176, ADR 0293).
			// The zero-base raise for the i32 road is likewise the runtime guard's job (see the double
			// road above): `pow(0, -1)` answers inf, and the guard below tests both signs at run time so
			// a base the compiler cannot see is trapped too.
			if ex, ok := g.intPowerOverflow(n.L, n.R); ok {
				return "", fmt.Errorf("`%s` overflows the language's bounded integer: %s is %s, which does not fit a 64-bit word. The reference has arbitrary-precision integers and answers %s; this backend declines rather than print a wrapped number (roadmap Gap R.176, ADR 0166)",
					exprSurface(n), exprSurface(n.L), exprSurface(n.R), ex)
			}
			ld := g.newTmp()
			fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", ld, l)
			rd := g.newTmp()
			fmt.Fprintf(b, "  %s = sitofp i32 %s to double\n", rd, r)
			pw := g.newTmp()
			fmt.Fprintf(b, "  %s = call double @llvm.pow.f64(double %s, double %s)\n", pw, ld, rd)
			t2 := g.newTmp()
			fmt.Fprintf(b, "  %s = fptosi double %s to i32\n", t2, pw)
			return t2, nil
		case "is":
			op = "icmp eq"
		case "is not":
			op = "icmp ne"
		case "==":
			op = "icmp eq"
		case "!=":
			op = "icmp ne"
		case "<":
			op = "icmp slt"
		case "<=":
			op = "icmp sle"
		case ">":
			op = "icmp sgt"
		case ">=":
			op = "icmp sge"
		default:
			return "", fmt.Errorf("codegen: unsupported operator %q", n.Op)
		}
		if strings.HasPrefix(op, "icmp") {
			// Two texts order by their characters, never by their interned index: the index records
			// which spelling the program mentioned first, so every `<`/`>` on text had an answer that
			// depended on the order of the source lines (roadmap Gap R.84). The operands are already
			// lowered above, so the door takes their registers rather than lowering them a second time
			// — an operand that prints must not print twice.
			if g.textOrderOperands(n) {
				res, _, terr := g.emitTextOrderOperands(b, n.Op, l, r)
				if terr != nil {
					return "", terr
				}
				return res, nil
			}
			// The same door, with the tag asked which pair this is: a slot on one side means the answer
			// is a run-time question, and only the tags can settle whether it is numbers, texts, or a
			// TypeError (roadmap L11.1, Gap R.82).
			if g.taggedOrderApplies(n) {
				res, _, ook, oerr := g.emitTaggedOrder(b, n)
				if oerr != nil {
					return "", oerr
				}
				if ook {
					return res, nil
				}
			}
			// A comparison is a *value* (printable, storable, passable), so it
			// returns the interpreter's i32 0/1; the i1 predicate stays internal
			// (tracked in i1Vals) so a condition can use it without re-testing.
			b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", t, op, l, r))
			g.markI1(t)
			res := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = zext i1 %s to i32\n", res, t))
			return res, nil
		}
		if op == "sdiv" || op == "srem" {
			// Division by zero is a language event, not an instruction: on x86 an unguarded
			// `sdiv` is a SIGFPE, and on AArch64 it does not trap at all — `print(7 % 0)`
			// answered a different garbage number each run and exited 0 (roadmap Gap R.18,
			// ADR 0212).
			kind := "division by zero"
			if n.Op == "//" {
				kind = "integer division or modulo by zero"
			}
			if op == "srem" {
				kind = "integer modulo by zero"
			}
			g.guardNonZeroInt(b, r, n.Span(), kind)
		}
		b.WriteString(fmt.Sprintf("  %s = %s i32 %s, %s\n", t, op, l, r))
		return t, nil
	case *UnOp:
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
		case "not":
			// `not` yields a boolean *value* (printable, storable), so it returns the
			// interpreter's i32 0/1 rather than a bare i1.
			//
			// Ask the SAME road an `if` test asks. `asI1` compares the lowered word against zero,
			// which is right for a number and wrong for anything whose value is a HANDLE or an
			// INTERNED INDEX: `not "x"` tested the intern slot, said "that word is zero", and
			// answered True where the reference answers False — at exit 0, while `if "x":` beside it
			// answered correctly (roadmap Gap R.183).
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
		if literalNeedsHeap(n) || g.literalNeedsTags(n) || g.literalHasPairElement(n) || n.IsTuple() {
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
		if literalNeedsHeap(n) || g.literalNeedsTags(n) || g.literalHasPairElement(n) {
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
		if literalNeedsHeap(n) || g.literalNeedsTags(n) || g.literalHasPairElement(n) {
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
		// A slice of a call's container answer asks the road that slices a container variable, by naming the
		// slot the answer was recorded under. `rt_slice` copies the slots with their tags and the binding road
		// records the copy, so `print(build()[0:2])` and `ys = build(); print(ys[0:2])` are one program written
		// two ways — the base's spelling stopped being the relevant fact the day the answer became an object
		// (roadmap Gap R.230, ADR 0335; the answer itself is ADR 0333, the index door ADR 0334).
		if redo, okAns := g.containerAnswerSubscript(b, n); okAns {
			return g.value(b, redo)
		}
		// String slices are folded at compile time via stringVal; the fold is a string
		// value, so it is emitted as its @str_tab index -- the pointer is for printf
		// contexts, which fold the text themselves (Gap R.42, ADR 0224).
		// The gate is "is this a string?", not "can the compiler read its text?": a slice of a
		// string the emitter only knows the *kind* of (`s = "abcdef"` inside a function body)
		// still has an answer, and the table gives it (ADR 0229).
		_, objIsFoldedStr := g.stringVal(n.Obj)
		if objIsFoldedStr || g.exprIsString(n.Obj) {
			// A text slice counts characters, so its bounds are positions and the reference's sentence for a
			// bound that is neither an integer nor `None` is the same one a list's slice gets. Asked before the
			// fold and before the helper, because both read a bound as a number without asking what it holds:
			// `print(s["a":"b"])` over `s = "abcd"` answered `b` at exit 0 (roadmap Gap R.233, ADR 0336).
			if g.badSliceBoundGate(b, n) {
				return "0", nil
			}
			// A step of zero is the reference's `ValueError: slice step cannot be zero`, and the fold reads a
			// step as a stride without asking whether it strides at all — asked here, before the fold and
			// before the helper, so the shape that used to be refused with `cannot fold string slice` is the
			// program's exception instead (roadmap Gap R.236, ADR 0336).
			if g.sliceStepIsKnownZero(n) {
				g.raiseSliceStepIsZero(b, n)
				return "0", nil
			}
			if sv, ok := g.stringVal(n); ok {
				return g.internStr(b, sv), nil
			}
			// Bounds that are values rather than constants are still a slice of the same
			// string: an absent bound is spelled with the helper's own sentinel, because an
			// i32 argument cannot be "missing" (ADR 0229).
			if g.sliceBoundIsAbsent(n.Step) {
				if sreg, sok, serr := g.strReg(b, n.Obj); serr != nil {
					return "", serr
				} else if sok {
					lo := "-2147483648"
					hi := "2147483647"
					if !g.sliceBoundIsAbsent(n.Low) {
						v, lerr := g.strPosReg(b, n.Low)
						if lerr != nil {
							return "", lerr
						}
						lo = v
					}
					if !g.sliceBoundIsAbsent(n.High) {
						v, herr := g.strPosReg(b, n.High)
						if herr != nil {
							return "", herr
						}
						hi = v
					}
					r := g.rtStrCall(b, "rt_str_slice", "i32 "+sreg, "i32 "+lo, "i32 "+hi)
					g.checkStrSentinels(b, r, "IndexError", "string slice out of range", n.Span(), "stslice")
					return r, nil
				}
			}
			return "", fmt.Errorf("cannot fold string slice")
		}
		// The copy is only as good as the description of what it copies: `rt_slice` needs a heap list, and the
		// roads that read the copy need to be told whether its slots describe themselves. Asked of a base
		// neither record describes, the road used to emit the copy over whatever word the base lowered to and
		// the caller printed that word — a heap handle wearing the program's answer (`print(m()["k"][0:1])`
		// printed `3`, exit 0). The refusal names the missing record (ADR 0249's rule; roadmap Gap R.235,
		// ADR 0335).
		if !g.sliceBaseIsDescribed(n.Obj) {
			return "", fmt.Errorf("cannot slice %s: the copy is a list whose slots are the base's payloads with their tags, and the base is not a list the records describe — a literal, a variable a binding recorded as a list, a call that hands back a list, or a slice of one (roadmap Gap R.235, ADR 0335)", exprSnippet(n.Obj))
		}
		// The base is a container the records describe; now the three bounds are asked the same question the
		// subscript door asks its position (Gap R.222's gate, one construct over): an integer or a bool is a
		// position, `None` is the absence of one, and anything else the compiler can name the kind of is the
		// reference's `TypeError: slice indices must be integers or None or have an __index__ method`. Asked
		// before any bound is lowered, so a text bound never becomes its `@str_tab` index and a position
		// (`print(xs["a":"b"])` answered `[1]` at exit 0) and a list bound never reaches `rt_slice`'s i32
		// arguments as a global address (exit 2, roadmap Gap R.233, ADR 0336).
		if g.badSliceBoundGate(b, n) {
			return "0", nil
		}
		objReg, err := g.value(b, n.Obj)
		if err != nil {
			return "", err
		}
		// A container LITERAL lowers to the ADDRESS of a compile-time global (`@.lst1`), which is not a
		// heap handle, and `rt_slice` walks the heap — so `print([1, 2, 3][1:])` emitted
		// `call i32 @rt_slice(i32 @.lst1, …)` and llc rejected the compiler's own module (exit 2, which
		// ADR 0166 counts as OUR bug). The comprehension road beside this one already solved it (ADR
		// 0234): ask the literal records, and if the operand is one, BUILD it as a real heap object
		// first and slice that (roadmap Gap R.179).
		// The base may be a literal NODE (`[1, 2, 3][1:]`) or a NAME the records hold a literal for
		// (`xs = [1, 2, 3]` / `xs[1:]`); both lower to a global address and both need a heap object.
		if lit := g.listLiteralOf(n.Obj); lit != nil {
			// The same question the build site asks wherever a list literal becomes an object: do the slots
			// need tags to be understood? A text's word is an interned index, a float's a box handle, a
			// nested container's a handle — all of which the printer can only read from a tag. Asking
			// `literalNeedsHeap` alone built the sliced source untagged, and `rt_slice` then faithfully copied
			// tags nobody had written (roadmap Gap R.230, ADR 0187's pairing rule).
			var berr error
			if literalNeedsHeap(lit) || g.literalNeedsTags(lit) || g.literalHasPairElement(lit) {
				built, terr := g.heapListFromTagged(b, lit)
				if terr != nil {
					return "", terr
				}
				objReg, berr = built, nil
			} else {
				objReg, berr = g.heapListFrom(b, lit, "")
			}
			if berr != nil {
				return "", berr
			}
		}
		lowReg := "0"
		highReg := "0"
		stepReg := "1"
		hasLow := "0"
		hasHigh := "0"
		hasStep := "0"
		if !g.sliceBoundIsAbsent(n.Low) {
			lowReg, err = g.value(b, n.Low)
			if err != nil {
				return "", err
			}
			hasLow = "1"
		}
		if !g.sliceBoundIsAbsent(n.High) {
			highReg, err = g.value(b, n.High)
			if err != nil {
				return "", err
			}
			hasHigh = "1"
		}
		if !g.sliceBoundIsAbsent(n.Step) {
			stepReg, err = g.value(b, n.Step)
			if err != nil {
				return "", err
			}
			hasStep = "1"
		}
		if hasStep == "1" {
			// The kind gate above could not ask this: `0` is a perfectly good integer. It is the one
			// value a step may not take, and `rt_slice` walks its own loop forever on it — a run that has
			// to be killed comes out as exit 2, which the contract blames on the compiler (ADR 0166), and
			// that is what `print(xs[::0])` did. The question is asked where the value is, after every
			// bound has been evaluated and before any copy is built, which is the order the reference
			// raises in: a step the compiler can read is settled there and then — a known zero raises with
			// no copy and no counter touched (a gate stays pure, ADR 0273), a known non-zero needs no test
			// at all — and only a step it cannot read carries the run-time guard (roadmap Gap R.236,
			// ADR 0336's neighbour, paid by ADR 0337).
			if v, known := g.sliceStepIsKnown(n); known {
				if v == 0 {
					g.raiseSliceStepIsZero(b, n)
					return "0", nil
				}
			} else {
				g.zeroStepGuardAtSlice(b, stepReg, n.Span())
			}
		}
		r := g.heapSeq
		g.heapSeq++
		// The copy is built inside rt_slice, which allocates through rt_alloc — so a full arena reaches
		// this caller as a negative handle too, and the store that follows the call would be the write
		// below the arena (roadmap Gap Q.2, ADR 0347). The guard stands after every bound has been
		// evaluated and after ADR 0337's zero-step question, which is the order the reference raises in.
		// A slice of the immutable spelling comes back a tuple, because `rt_slice` copies the source
		// object's attribute word along with its slots and tags (ADR 0361) — the door that decides what a
		// value prints is the object, not the road that copied it.
		g.emitHeapCall(b, fmt.Sprintf("%%sl%d", r), fmt.Sprintf("call i32 @rt_slice(i32 %s, i32 %s, i32 %s, i32 %s, i32 %s, i32 %s, i32 %s)", objReg, lowReg, highReg, stepReg, hasLow, hasHigh, hasStep), n.Span())
		return fmt.Sprintf("%%sl%d", r), nil

	case *Index:
		// An index that is not a position is asked about before the subscript is lowered at all.
		// The bounds check downstream takes an i32 and never asked what it meant, so `xs["a"]`
		// counted to slot 1 with the text's @str_tab index and printed an element nobody asked
		// for, and `xs[[0]]` lowered a list literal to a @.lstN global reference inside an
		// `icmp slt` — a module llc rejected, exit 2, the code the contract reserves for a
		// compiler bug. The reference raises TypeError naming the type, and so does this (roadmap
		// Gap R.222, ADR 0329). A dict or a set is not subscripted by position and is left to the
		// hashing door beside it (Gap R.217, ADR 0328).
		if g.badIndexGateAtSubscript(b, n) {
			return "0", nil
		}
		// A subscript whose base is a call that hands back a container asks the door the bound form
		// already answers. The callee materialised the object at its `ret` and the answer arrives under a
		// rooted name carrying the notebooks this road consults for a container variable, so `build()[7]`
		// is `ys = build(); ys[7]` written shorter — same read, same bounds check, same catchable raise.
		// Until this arm the fold's own gate condemned the shape because a call is not a literal, which
		// is the base's *spelling* rather than what the compiler holds: the same misattribution ADR 0251
		// fixed for a container the program built, one construct over (roadmap Gap R.214, ADR 0334).
		if redo, okAns := g.containerAnswerSubscript(b, n); okAns {
			return g.value(b, redo)
		}
		// A subscript of a string the compiler cannot read asks the table (ADR 0229): the
		// base only has to *be* a string, not be a constant. The fold below still answers
		// everything the compiler can name, so this path is reached exactly when the fold
		// cannot — which used to be a refusal, so `print(s[1])` worked and `print(get()[1])`
		// did not, one rule with two behaviours.
		_, foldBase := g.stringVal(n.Obj)
		_, foldIdx := n.Idx.(*IntLit)
		if !foldBase || !foldIdx {
			if ch, isStr, err := g.emitStrChar(b, n.Obj, n.Idx, n.Span()); err != nil {
				return "", err
			} else if isStr {
				return ch, nil
			}
		}
		// list/dict/set indexing against an inline literal with a constant
		// index/key (this llc build accepts only constant GEP indices).
		// Constant keys/elements are resolved at compile time.
		key := int64(0)
		if il, ok := n.Idx.(*IntLit); ok {
			key = il.Value
		} else {
			// non-literal index: only runtime containers support it (x[a]) — and a dict literal, which is
			// a runtime container the compiler happens to be looking at. Refusing the text key here is what
			// made `print({"a": 1}["a"])` stop the program at exit 1 while `d = {"a": 1}; print(d["a"])`
			// answered `1`: one construct, two doors (roadmap Gap R.215, ADR 0327).
			if nm, ok := n.Obj.(*Name); ok {
				if !g.listVars[nm.Value] && !g.runtimeDicts[nm.Value] && !g.runtimeSets[nm.Value] {
					return "", fmt.Errorf("index must be a constant")
				}
			} else if _, isDictLit := n.Obj.(*DictLit); !isDictLit {
				return "", fmt.Errorf("index must be a constant")
			}
		}
		// A subscript of a string is a one-character string (ADR 0225), and the base the
		// compiler can name is folded: a literal, a variable holding text, a folded
		// concatenation. Negative positions are positions, as ADR 0210 ruled for containers,
		// and the count is code points, which is how `len` and slicing already count. Without
		// this the base only worked as a literal, so `s = "abc"; print(s[1])` refused while
		// `"abc"[1]` answered -- one rule, two behaviours.
		if _, isInt := n.Idx.(*IntLit); isInt {
			if txt, isStr := g.stringVal(n.Obj); isStr {
				runes := []rune(txt)
				i := normPosIndex(key, int64(len(runes)))
				if i < 0 || i >= int64(len(runes)) {
					g.constantTrap(b, "IndexError", stringIndexOutOfRange(), n.Span())
					return "0", nil
				}
				return g.internStr(b, string(runes[i])), nil
			}
		}
		switch obj := n.Obj.(type) {
		case *Name:
			// runtime heap list variable: x[i] reads heap[x].data[i].
			if g.runtimeDicts[obj.Value] {
				g.heapSeq++
				hs := g.heapSeq
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, obj.Value))
				// The key may be a runtime value (`for k in d: d[k]`), so it is
				// lowered like the list index path does; only a literal key folds. A
				// string key becomes its @str_tab index, because rt_dict_get/rt_dict_has
				// take i32 keys and a global pointer there is what LLVM rejected.
				keyOp := strconv.FormatInt(key, 10)
				// The key is compared as (payload, tag). A string needle's payload is an
				// @str_tab index, and an int key holding the same number is a different key:
				// {1: "one"} asked for "a" must raise KeyError, not answer "one" — which is
				// what an untagged comparison did until the tag was consulted here too
				// (ADR 0232, and ADR 0189's promise that the tag is always there to read).
				keyTag := ""
				if _, isLit := n.Idx.(*IntLit); !isLit {
					kv, isStr, e := g.heapElemKind(b, n.Idx)
					if e != nil {
						return "", e
					}
					if isStr {
						g.dictKeyStr[obj.Value] = true
					}
					keyOp = kv
					if t, ok := g.elemKindTag(n.Idx); ok {
						keyTag = strconv.FormatInt(int64(t), 10)
					} else if isStr {
						keyTag = strconv.FormatInt(int64(TagStr), 10)
					} else {
						keyTag = strconv.FormatInt(int64(TagInt), 10)
					}
				} else if nm, ok := n.Idx.(*Name); ok && g.taggedVars[nm.Value] {
					kt := g.newTmp()
					b.WriteString(fmt.Sprintf("  %s = load i32, i32* %%_%s_tag\n", kt, nm.Value))
					keyTag = kt
				}
				if keyTag != "" {
					g.checkKeyReadTagged(b, fmt.Sprintf("%%h%d", hs), keyOp, keyTag, n.Idx, n.Span())
					b.WriteString(fmt.Sprintf("  %%g%d = call i32 @rt_dict_get_tagged(i32 %%h%d, i32 %s, i32 %s)\n", hs, hs, keyOp, keyTag))
				} else {
					g.checkKeyRead(b, fmt.Sprintf("%%h%d", hs), keyOp, n.Idx, n.Span())
					b.WriteString(fmt.Sprintf("  %%g%d = call i32 @rt_dict_get(i32 %%h%d, i32 %s)\n", hs, hs, keyOp))
				}
				return fmt.Sprintf("%%g%d", hs), nil
			}
			if g.listVars[obj.Value] {
				idxOp := strconv.FormatInt(key, 10)
				if _, ok := n.Idx.(*IntLit); !ok {
					v, e := g.value(b, n.Idx)
					if e != nil {
						return "", e
					}
					idxOp = v
				}
				g.heapSeq++
				hs := g.heapSeq
				if g.mixedLists[obj.Value] {
					// A numeric use of an element is not a use that needs the tag. When the container is one the
					// program spelled out and never changed, and the element is a number literal, the slot holds
					// that number: `xs = [1, "a"]; print(xs[0] + 1)` is an addition on 1, and the float form takes
					// the float path with it (roadmap L11.1, ADR 0243). What the promise does not cover — a name
					// element, a runtime index, text or a container in the slot — keeps the refusal below, because
					// for those the tag is exactly what the answer depends on.
					if v, ok := g.numericElemUse(b, n); ok {
						return v, nil
					}
					if v, ok := g.numericSlotUse(b, obj.Value, n); ok {
						return v, nil
					}
					return "", mixedReadErr("list")
				}
				b.WriteString(fmt.Sprintf("  %%h%d = load i32, i32* %%_%s\n", hs, obj.Value))
				idxOp = g.normalizeIndexFor(b, fmt.Sprintf("%%h%d", hs), idxOp, n.Span(), indexOutOfRangeSentenceFor(g.isTupleExpr(obj)))
				b.WriteString(fmt.Sprintf("  %%g%d = call i32 @rt_get_elem(i32 %%h%d, i32 %s)\n", hs, hs, idxOp))
				return fmt.Sprintf("%%g%d", hs), nil
			}
			if g.dictVals != nil {
				if dl, ok := g.dictVals[obj.Value]; ok {
					return g.dictIndex(dl, key)
				}
			}
			// A name whose container the program spelled out and never changed answers from the
			// literal, whatever the kind maps happen to record: the mixed-list arm above consults this
			// fold and the arms below do not, so a chain — which lowers each operand through an
			// ordinary `value` read rather than through the comparison's own context — reached the
			// refusal for `0 < xs[1] < 3` while the same program written as one comparison did not
			// (roadmap L12.1 / Gap R.53, ADR 0288). Asking the fold here is the choke-point form of
			// ADR 0243's promise: the slot holds that number, so the answer is the number.
			if v, ok := g.numericElemUse(b, n); ok {
				return v, nil
			}
			if v, ok := g.numericSlotUse(b, obj.Value, n); ok {
				return v, nil
			}
			return "", fmt.Errorf("index of a non-literal variable")

		case *Attr:
			// imported module list global (data imports): mod.list[i]
			if nm, ok := obj.Obj.(*Name); ok {
				if globals, ok2 := g.imports.Globals[nm.Value]; ok2 {
					if lit, ok3 := globals[obj.Name.Value]; ok3 {
						if lst, ok4 := lit.(*ListLit); ok4 {
							if key >= 0 && int(key) < len(lst.Elems) {
								return g.value(b, lst.Elems[key])
							}
						}
						if dct, ok4 := lit.(*DictLit); ok4 {
							ks, err := dictLiteralKeys(dct)
							if err != nil {
								return "", err
							}
							vs, err := dictLiteralVals(dct)
							if err != nil {
								return "", err
							}
							for i := range ks {
								if ks[i] == key {
									return fmt.Sprintf("%d", vs[i]), nil
								}
							}
						}
					}
				}
			}
			return "", fmt.Errorf("codegen: index of non-list module attr")
		case *ListLit:
			// index into a list literal: evaluate the element directly. A negative key
			// counts from the end like every other positional subscript (L11.4), and it has
			// to be checked here: this line used to hand `-1` straight to the Go slice and
			// crash the compiler (`panic: runtime error: index out of range [-1]`), which the
			// exit-code contract classifies as a compiler bug (ADR 0168's rule).
			li := int(normPosIndex(key, int64(len(obj.Elems))))
			if li < 0 || li >= len(obj.Elems) {
				// The position is a constant, so this backend is the first to notice it is not there — and
				// notice it the reference does: an IndexError the program's `except IndexError:` can take,
				// not a compile stop that no handler can reach (roadmap Gap R.37, ADR 0326). The sentence
				// names the spelling the source wrote, because `(1, 2)[5]` is not a list running off its
				// end (roadmap L11.3, ADR 0361).
				g.constantTrap(b, "IndexError", indexOutOfRangeSentenceFor(obj.IsTuple()), n.Span())
				return "0", nil
			}
			if !g.listLiteralFoldAnswers(obj, n.Idx) {
				// Two doors, one question — the dict arm above, taken for the other container (ADR 0327).
				// Where the compiler can read the whole literal (every element an integer constant) the read
				// is a compile-time lookup and the program gets the number. Where it cannot, the element's
				// kind lives in the tag the builder wrote beside its payload, and a position that keeps one
				// word for a whole value has nowhere to put it: this road refuses by name rather than hand
				// back an @str_tab index or a @float_box handle wearing a number. print, str()/repr(), an
				// f-string field and a binding ask the pair door instead, which is where the answer is
				// the reference's (roadmap Gap R.218, ADR 0331; the residue is Gap R.146).
				return g.listLiteralRead(b, obj, n.Idx, n.Span())
			}
			return g.value(b, obj.Elems[li])
		case *DictLit:
			// Two doors, one question. Where the compiler can read the whole literal and the key — every key
			// and every value an integer constant, the key an integer literal — the read is a compile-time
			// lookup and the program gets the number. Where it cannot, the read is a message to the object:
			// the same three calls a dict variable's read makes, on a dict the builder just tagged slot by
			// slot, so a text key, a number key, a key of a kind the dict's other entries never held, and a
			// key that is simply absent all get the answer the object gives rather than the answer the
			// spelling suggests (roadmap Gap R.215, ADR 0327).
			if g.dictLiteralFoldAnswers(obj, n.Idx) {
				keys, err := dictLiteralKeys(obj)
				if err != nil {
					return "", err
				}
				vals, err := dictLiteralVals(obj)
				if err != nil {
					return "", err
				}
				// A repeated key in a literal is the SAME key written twice, so the later entry wins the value
				// and the earlier one only contributes its spelling of the key: `{1: 2, 1: 3}[1]` is `3` in the
				// reference, which is what a reverse scan says and a forward one gets backwards. The dict is
				// built by insertion, so the object already answered this correctly — the fold was the half that
				// had never been asked (roadmap Gap R.216, ADR 0327).
				for i := len(keys) - 1; i >= 0; i-- {
					if keys[i] == key {
						return fmt.Sprintf("%d", vals[i]), nil
					}
				}
				// A constant key the literal does not hold is the KeyError the reference raises, with the key
				// itself as its message — the same sentence the dynamic read of a heap dict raises through
				// `checkKeyRead`, so one program gets one message whichever way the compiler reached it
				// (roadmap Gap R.37, ADR 0326).
				g.constantTrap(b, "KeyError", g.keyNotFoundMessage(n.Idx), n.Span())
				return "0", nil
			}
			return g.literalDictRead(b, obj, n.Idx, n.Span())
		case *SetLit:
			// constant membership lookup: return the element if present.
			elems, err := setLiteralElems(obj)
			if err != nil {
				return "", err
			}
			for _, el := range elems {
				if el == key {
					return fmt.Sprintf("%d", key), nil
				}
			}
			return "", fmt.Errorf("not in set")
		case *Comp:
			// indexing into a lowered comprehension result. The struct shape
			// depends on the comprehension kind:
			//   - list: positional GEP+load into {i32, [n x i32]}.
			//   - set:  membership test against the folded elements.
			//   - dict: constant key lookup against the folded key/value pairs.
			switch obj.Kind {
			case CompList:
				name, err := g.comp(b, obj)
				if err != nil {
					return "", err
				}
				n := g.compLen[obj]
				key = normPosIndex(key, int64(n))
				if key < 0 || key >= int64(n) {
					g.constantTrap(b, "IndexError", listIndexOutOfRange(), obj.Span())
					return "0", nil
				}
				v := g.newTmp()
				b.WriteString(fmt.Sprintf("  %s = load i32, i32* getelementptr({i32, [%d x i32]}, {i32, [%d x i32]}* %s, i32 0, i32 1, i32 %d)\n", v, n, n, name, key))
				return v, nil
			case CompSet:
				// set membership: `s[key]` returns the element if present,
				// otherwise errors (interpreter semantics). comp() populates
				// compEls and emits the set global.
				if _, err := g.comp(b, obj); err != nil {
					return "", err
				}
				for _, el := range g.compEls[obj] {
					if el == key {
						return fmt.Sprintf("%d", key), nil
					}
				}
				return "", fmt.Errorf("not in set")
			case CompDict:
				// dict lookup: `d[key]` returns the mapped value, else errors.
				// comp() populates compKeys/compEls and emits the dict global.
				if _, err := g.comp(b, obj); err != nil {
					return "", err
				}
				for i, k := range g.compKeys[obj] {
					if k == key {
						return fmt.Sprintf("%d", g.compEls[obj][i]), nil
					}
				}
				return "", fmt.Errorf("key not found")
			default:
				return "", fmt.Errorf("unsupported comprehension kind %d", obj.Kind)
			}
		case *StrLit:
			str, ok := g.stringVal(obj)
			if !ok {
				return "", fmt.Errorf("string index on non-constant string")
			}
			// The same rule as the interpreter (ADR 0225): a subscript of a string is a
			// one-character string, so it comes back as an @str_tab index, and it is counted in
			// code points. `%d`-ing the byte made `s[1] == "b"` compile and answer false -- a wrong
			// value, not a refusal, which is the worst thing this backend can do.
			runes := []rune(str)
			key = normPosIndex(key, int64(len(runes)))
			if key < 0 || key >= int64(len(runes)) {
				g.constantTrap(b, "IndexError", stringIndexOutOfRange(), n.Span())
				return "0", nil
			}
			return g.internStr(b, string(runes[key])), nil
		case *Call:
			// Element access into list-producing call expressions: keys(),
			// values(), sorted(...), reversed(...), split(...). partition()
			// returns only dummy length elems (see dictMethodElems), so it
			// is excluded here to avoid silently wrong results.
			if elems, ok2 := g.indexListElems(obj); ok2 {
				key = normPosIndex(key, int64(len(elems)))
				if key < 0 || int(key) >= len(elems) {
					g.constantTrap(b, "IndexError", listIndexOutOfRange(), n.Span())
					return "0", nil
				}
				return g.value(b, elems[key])
			}
			// The base is an expression rather than a name the compiler holds (`t[0][0]`, `d["a"][1]`,
			// `xs[0][0]`). Same rule as the mixed-list arm above: a literal number in a slot of a container
			// the program spelled out and never changed *is* that number, and the arithmetic the language
			// already has runs on it (roadmap L11.1, ADR 0243).
			if v, ok := g.numericElemUse(b, n); ok {
				return v, nil
			}
			return "", g.slotReadRefusal(n, "index")
		default:
			if v, ok := g.numericElemUse(b, n); ok {
				return v, nil
			}
			return "", g.slotReadRefusal(n, "index")
		}
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
