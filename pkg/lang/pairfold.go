package lang

// pkg/lang/pairfold.go — the fold builtins answer the pair (roadmap L11.1, Gap R.146's
// "`min(n, 3)`" and "a literal `sum`/`min`/`max` folds into a static array"; ADR 0316).
//
// `xs = []` / `xs.append(7)` / `n = xs[0]` / `print(min(n, 3))` is CPython's `3`, and the compiled
// backend spent exit 1 on it. So is `print(sum([n, 1]))`, `print(max([n, 2.5]))` and the bound form
// `m = min(n, 3)` / `print(m)`. The name already answers `print(n)` (ADR 0303's one printer),
// `print(n - 1)` (ADR 0304's per-operator door), `print(n / 4)` (ADR 0305), `print([n])` (ADR 0306),
// `f"{n}"` (ADR 0307), `abs(n)` (ADR 0309) and the four mutation statements (ADR 0311). What made the
// folds the next row is that they are the last positions in Gap R.146 that decide a *kind* rather than
// consume one:
//
//   - `min` and `max` return one of the values they were given, so the answer's kind is the WINNER's
//     kind — `min(2.5, 3)` is the float and `max(2.5, 3)` is the integer. A fold therefore cannot be
//     lowered by lifting both sides, comparing, and returning one word: the word is the loser's shape
//     as often as the winner's, which is why `max([1, 2.5])` used to answer `2` (Gap R.104) and why
//     the varargs road still refuses a mixed int/double pair it cannot settle at compile time.
//   - `sum` is a left fold over `+` seeded with the integer `0`, which is where its cross-kind sentence
//     comes from (`sum([ "a"])` is `unsupported operand type(s) for +: 'int' and 'str'` — the `int` is
//     the seed, not an operand the program wrote).
//
// Both are the same door the arithmetic already has: hand the pairs to the run time, let it decide the
// arm by the tags, and bring the answer's kind back beside the answer. `@rt_pair_fold` picks the winner
// and writes its payload AND its tag; the sum reuses `@rt_num_arith`'s `+`, so the fold's raise is the
// operator's own sentence per kind rather than a second one written here (ADR 0265's rule, ADR 0271's
// wording rule).
//
// What the door is NOT: a widening of the arithmetic door. `arithWouldRefuse` does not learn about
// these calls, so `min(n, 3) + 1` keeps the refusal ADR 0309 pinned for `abs(n) + 1` — a fold answer
// used as an operand of the numeric door is the one-word reading this row exists to refuse, and letting
// the float road lift a fold-shaped sibling is the recursion that ADR 0309 had to kill.

import (
	"fmt"
	"strings"
)

// The two shapes the fold door answers. `sum` is not an op of `@rt_pair_fold`: it is `+`, and `+` already
// has a door whose raise names each kind. The three codes are the door's own vocabulary — `foldDoorSum`
// is deliberately NOT rt_num_arith's `+`, which is 0 and which `foldDoorMin` already occupies here.
const (
	foldDoorMin = 0
	foldDoorMax = 1
	foldDoorSum = 2
	// rtNumArithAdd is @rt_num_arith's op code for `+`, the operator a sum folds with.
	rtNumArithAdd = 0
)

// foldCallShape recognises the call a fold door answers and hands back the values it folds. It is a
// precise syntactic question, not a guess: the name is a builtin this program does not shadow, the
// argument list holds no `name = value` argument (`sum(xs, start=1)`, `min(a, b, key=f)` belong to
// L11.7's call surface, not to this door), and what arrives is either values side by side or ONE
// literal container of them. A name bound to a list is not that container — reading a built container's
// elements in order is Gaps R.95/R.83's row, and a fold over it would answer from the wrong element set.
func (g *irGen) foldCallShape(c *Call) (door int, args []Expr, ok bool) {
	if c == nil || c.Fn == nil {
		return 0, nil, false
	}
	nm, isName := c.Fn.(*Name)
	if !isName || g.builtinShadowed(nm.Value) {
		return 0, nil, false
	}
	switch nm.Value {
	case "min", "max", "sum":
	default:
		return 0, nil, false
	}
	if len(c.Args) == 0 {
		return 0, nil, false
	}
	for _, a := range c.Args {
		if _, isKw := a.(*KeywordArg); isKw {
			return 0, nil, false
		}
	}
	if len(c.Args) == 1 {
		// The one-argument spelling folds a literal container. Two containers are not in this door:
		//
		//   - a dict, because the ordinary road already refuses a non-empty one ("sum expects a list or
		//     set") and a door that quietly accepted `sum({1: 2})` would answer a program the language
		//     refuses; and
		//   - a SET, because a fold over one is not the fold the source describes: CPython iterates a set
		//     in hash order and dedups it by value first, so which element is the incumbent and how many
		//     steps the fold takes are neither of them written in the line. `sum({n, 3})` with a slot
		//     holding 3 is 3 there and 6 here, at exit 0 — a wrong answer, not a refusal, which is the one
		//     thing this row is not allowed to ship (roadmap L11.1, Gap R.146; ADR 0316). The elements of a
		//     container the program built are Gaps R.95/R.83's row.
		switch n := c.Args[0].(type) {
		case *ListLit:
			args = n.Elems
		default:
			return 0, nil, false
		}
	} else {
		args = c.Args
	}
	if len(args) == 0 {
		return 0, nil, false
	}
	switch nm.Value {
	case "min":
		return foldDoorMin, args, true
	case "max":
		return foldDoorMax, args, true
	}
	return foldDoorSum, args, true
}

// foldMayNeedPair is the cheap, side-effect-free question the positions ask before trying the door:
// would the ordinary fold road have refused one of these elements? It is `operandMayNeedPair`, the same
// test the arithmetic door gates itself with, applied to every element — so a program whose fold is all
// literals, all settled variables or all float variables keeps the road and the instruction count it
// has always had, and landing this door cannot reroute one working program.
func (g *irGen) foldMayNeedPair(args []Expr) bool {
	for _, a := range args {
		if g.operandMayNeedPair(a) {
			return true
		}
	}
	return false
}

// taggedFoldPair is the door: lower every folded value to a (payload, tag) pair and chain the run-time
// fold across them. The answer is the pair the winner travelled in, which is what lets the print door,
// the binder, `str`/`repr` and the f-string field render a number, a float or a text without the
// compiler having to know which one it got.
//
// ok=false means "not mine" — the caller keeps the refusal it has always printed, which is what keeps
// every one-word position (`min(n, 3) + 1`, an `abs` operand, a dict key) honest rather than reading a
// payload alone.
func (g *irGen) taggedFoldPair(b *strings.Builder, e Expr) (payload, tag string, ok bool, err error) {
	c, isCall := e.(*Call)
	if !isCall {
		return "", "", false, nil
	}
	door, args, shapeOK := g.foldCallShape(c)
	if !shapeOK || !g.foldMayNeedPair(args) {
		return "", "", false, nil
	}
	pairs := make([][2]string, 0, len(args))
	for _, a := range args {
		p, t, okPair, perr := g.arithOperandPair(b, a)
		if perr != nil {
			return "", "", false, perr
		}
		if !okPair {
			return "", "", false, nil
		}
		pairs = append(pairs, [2]string{p, t})
	}
	g.heapUsed = true
	g.arithUsed = true
	g.floatFmtUsed = true
	acc := pairs[0]
	start := 1
	if door == foldDoorSum {
		// CPython's sum starts at the integer 0, and that is why the sentence a text element earns
		// names 'int' first: `sum(["a"])` is `0 + "a"`.
		acc = [2]string{"0", "0"}
		start = 0
	}
	for _, cand := range pairs[start:] {
		var stepErr error
		acc, stepErr = g.emitFoldStep(b, door, acc, cand, c.Span())
		if stepErr != nil {
			return "", "", false, stepErr
		}
	}
	return acc[0], acc[1], true, nil
}

// emitFoldStep runs one comparison of the fold: the incumbent pair and the candidate pair go in, the
// winning pair comes out. The candidate is passed first because that is the order CPython's TypeError
// names the two kinds in — the fold asks `candidate < incumbent` (min) or `candidate > incumbent`
// (max) — and the incumbent is kept on a tie, which is why `max(True, 1)` is `True`.
//
// The raise is emitted here and not in the helper, because only this code knows whether a handler is
// open: the helper fills a static buffer, the emitted code stores it through the same raise door every
// trap in the language uses, so `except TypeError:` reaches it (ADR 0228).
func (g *irGen) emitFoldStep(b *strings.Builder, door int, incumbent, candidate [2]string, sp Span) ([2]string, error) {
	isFold := door == foldDoorMin || door == foldDoorMax
	if isFold {
		// The door's own block: only a module that ORDERS a pair carries the ordering's sentence, and a sum
		// — which compares nothing — pays for no format it never reaches (ADR 0309's gate, asked of this
		// door; roadmap L11.1, Gap R.146).
		g.foldUsed = true
	}
	outp, outt, outm, st := g.newTmp(), g.newTmp(), g.newTmp(), g.newTmp()
	fmt.Fprintf(b, "  %s = alloca i32\n", outp)
	fmt.Fprintf(b, "  %s = alloca i32\n", outt)
	fmt.Fprintf(b, "  %s = alloca i8*\n", outm)
	if isFold {
		fmt.Fprintf(b, "  %s = call i32 @rt_pair_fold(i32 %d, i32 %s, i32 %s, i32 %s, i32 %s, i32* %s, i32* %s, i8** %s)\n",
			st, door, candidate[0], candidate[1], incumbent[0], incumbent[1], outp, outt, outm)
	} else {
		fmt.Fprintf(b, "  %s = call i32 @rt_num_arith(i32 %d, i32 %s, i32 %s, i32 %s, i32 %s, i32* %s, i32* %s, i8** %s)\n",
			st, rtNumArithAdd, incumbent[0], incumbent[1], candidate[0], candidate[1], outp, outt, outm)
	}
	// Every block written here begins with exactly one label, because a second label while a block is
	// still open is the module llc rejects ("expected instruction opcode") — so the merge label the one-
	// status door would never test is not emitted at all rather than emitted empty.
	badType, done := g.newLabel("foldtype"), g.newLabel("foldok")
	isType := g.newTmp()
	fmt.Fprintf(b, "  %s = icmp eq i32 %s, 1\n", isType, st)
	g.markI1(isType)
	if isFold {
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isType, badType, done)
		fmt.Fprintf(b, "%s:\n", badType)
		// One class from one door: the fold's own sentence comes from @rt.fold.fmt, written by the helper
		// that knows the two operand kinds.
		g.raiseRuntimeMsg(b, "TypeError", outm, sp)
	} else {
		next := g.newLabel("foldnext")
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isType, badType, next)
		fmt.Fprintf(b, "%s:\n", badType)
		// The sum's sentence is the operator's, per kind, from the door `+` already has (ADR 0265).
		g.raiseRuntimeMsg(b, "TypeError", outm, sp)
		fmt.Fprintf(b, "%s:\n", next)
		// `+` is the one fold step that can leave a second status: the whole number the answer would
		// be does not fit this backend's int word, and the answer raises rather than letting `fptosi`
		// answer poison (Gap R.133's rule; L12.12 owns the word). `rt_pair_fold` compares and cannot
		// overflow, so the fold arms carry one status and no dead branch.
		isOvf := g.newTmp()
		badOvf := g.newLabel("foldovf")
		fmt.Fprintf(b, "  %s = icmp eq i32 %s, 2\n", isOvf, st)
		g.markI1(isOvf)
		fmt.Fprintf(b, "  br i1 %s, label %%%s, label %%%s\n", isOvf, badOvf, done)
		fmt.Fprintf(b, "%s:\n", badOvf)
		g.raiseRuntimeMsg(b, "OverflowError", outm, sp)
	}
	fmt.Fprintf(b, "%s:\n", done)
	v, t := g.newTmp(), g.newTmp()
	fmt.Fprintf(b, "  %s = load i32, i32* %s\n", v, outp)
	fmt.Fprintf(b, "  %s = load i32, i32* %s\n", t, outt)
	// The winner's payload is one the operands already carried — a float box or a container handle —
	// and a register is not a root. The registration is the frame's own (ADR 0181), so it costs one
	// entry the frame close drops; an int payload pushed to die with the frame is a word wasted, not
	// a bug, and ADR 0265's recycled-box answer is what not doing it costs.
	fmt.Fprintf(b, "  call void @rt_root_put(i32* %s)\n", outp)
	g.rooted = true
	return [2]string{v, t}, nil
}

// pairFoldPrint is the print position's fold road, asked where the ordinary numeric road would have
// refused: the one printer that reads a tag renders the winner, so `print(min(n, 3))` and `print(m)`
// cannot disagree about `3` versus `3.0` the way the two number formatters once disagreed about a
// container (ADR 0303's one printer, ADR 0306's container element, ADR 0309's signless call).
func (g *irGen) pairFoldPrint(b *strings.Builder, e Expr) (bool, error) {
	p, t, ok, err := g.taggedFoldPair(b, e)
	if err != nil || !ok {
		return false, err
	}
	g.heapUsed = true
	g.floatFmtUsed = true
	fmt.Fprintf(b, "  call void @rt_print_mixed_value(i32 %s, i32 %s, i32 0)\n", p, t)
	return true, nil
}

// bindFoldPair is the binding position's fold road: `m = min(n, 3)` binds the name with the pair the
// answer arrived in, so every tag-reading position — print, str/repr, an f-string field, a container
// element, a further fold — reads the same two words the run time wrote. The origin is recorded rather
// than inferred, because the refusal this name meets later has to say where its tag came from and this
// program contains no loop, no call site and no slot the name itself was read out of (Gap R.38).
func (g *irGen) bindFoldPair(b *strings.Builder, name string, e Expr) (bool, error) {
	p, t, ok, err := g.taggedFoldPair(b, e)
	if err != nil || !ok {
		return false, err
	}
	g.bindTaggedVar(b, name, p, t)
	if g.taggedOrigin == nil {
		g.taggedOrigin = map[string]string{}
	}
	g.taggedOrigin[name] = taggedOriginFold
	return true, nil
}

// foldPairOf is the rendering positions' question — the f-string field's and str()/repr()'s — and it is
// the same door print and the binder ask, so one fold cannot print `3` in a line and `3.0` in a field
// (ADR 0303's rule that a value has ONE renderer, extended to the answer of a call).
func (g *irGen) foldPairOf(b *strings.Builder, e Expr) (payload, tag string, ok bool, err error) {
	return g.taggedFoldPair(b, e)
}
