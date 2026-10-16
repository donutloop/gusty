// codegen_binop.go is the road an arithmetic or comparison BinOp takes: the one place a `+`, `//`, `<`
// or `%` between two values decides what the program does. irGen.value hands it the node; the
// predicates it asks — operand kinds, container shapes, the trap a pair of kinds gets — are in
// codegen_arith.go, and the numeric fold that answers them before the module is written is in
// codegen_float.go.
package lang

import (
	"fmt"
	"math"
	"math/big"
	"strings"
)

// valueBinOp is the road a BinOp takes: the operand kinds the module has recorded decide the road,
// rather than a switch restated on each one (ADR 0259's tagged-element family, ADR 0279's shared
// power rule).
func (g *irGen) valueBinOp(b *strings.Builder, e Expr, n *BinOp) (string, error) {
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
}
