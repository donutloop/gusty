// pkg/lang/constant_trap.go — the constant fold that has no answer raises what the reference raises
// (roadmap Gap R.37, ADR 0326).
//
// A fold is an evaluation the compiler performs on the program's behalf. When the operand is a
// constant the compiler can read — `[1, 2][5]`, `"abc"[7]`, `int("x")`, `ord("")`, `chr(0x110000)` —
// the compiler is the first thing in the program's order of business to discover that the operation
// fails. That discovery is not a diagnosis of the source: the *reference* runs the same line, discovers
// the same thing, and reports it as a `TypeError`/`ValueError`/`IndexError` the program can catch. This
// file exists because the two answers are different language events:
//
//   - a trap is a fact about the program's execution; it reaches the program's own `except IndexError:`,
//     it lands in the runtime-error exit class (ADR 0211), and the `if debug:` branch that never runs
//     costs the program nothing;
//   - a refusal is a fact about this compiler; it stops the whole program at exit 1 before a statement
//     runs, so a handler that would have caught the error never gets a chance, and a line inside a
//     branch the program never takes can condemn a program that would have run fine.
//
// The rule this file applies is the one ADR 0211 and ADR 0228 already apply to the dynamic roads:
// a failing operation becomes an IR-level trap through the same `raiseTo` the `raise` statement and the
// dynamic bounds check use — the class from `exceptions.go`, the sentence from the one table below, the
// landing pad the enclosing `try:` pushed. The only constants a fold may still resolve are ones the
// program cannot avoid executing and that actually have a value.
//
// What stays a refusal is the shape the *language surface* cannot express, not the operation that fails:
// a constant integer too wide for this backend's `int` word (roadmap L12.12 — answering it would be a
// wrong number, and CPython's `int` has no bound), and an index the compiler cannot read at all (roadmap
// Gap R.213), which is a missing lowering rather than a missing trap.

package lang

import (
	"errors"
	"strconv"
	"strings"
)

// constantTrap emits, at the point where a fold found no value, the trap the reference raises for the
// operation the program wrote — and then opens the block the caller's remaining instructions belong to.
//
// The second half is not decoration. `raiseTo` ends the current block with a branch to the landing pad,
// so an emitted trap that is not followed by a label leaves whatever the caller appends next sitting
// after a terminator, which is a module the verifier rejects and the exit-code contract charges the
// compiler with (ADR 0166's exit 2). The caller therefore returns the word the trap makes unreachable
// ("0" — never read, never printed) and continues writing into the fresh block, exactly like the
// dynamic bounds check, which emits `ix.bad`/`ix.ok` around its own raise.
func (g *irGen) constantTrap(b *strings.Builder, class, msg string, sp Span) {
	g.raiseTo(b, exnCode(class), class, msg, sp)
	lbl := g.newLabel("fold.trap")
	b.WriteString(lbl + ":\n")
}

// The positional-trap sentences, written once. The dynamic bounds check, the text road and the constant
// fold all quote the reference through these, because a fold that invents its own wording is how one
// program gets two different messages for the same accident depending on whether the compiler happened
// to see the position (roadmap Gap R.90 owns the remaining distance between the first of these and
// CPython's `list index out of range`).
// listIndexOutOfRange is the READ sentence, and it names its container: CPython says
// `list index out of range`, not the bare `index out of range` this backend had been saying while the
// string road beside it was already specific (roadmap Gap R.223, ADR 0330). A traceback is the
// interface — Gap R.35 established that a trap reported with the wrong words is a trap reported
// wrongly — and the container is the word a reader needs when two containers are on the line.
func listIndexOutOfRange() string { return "list index out of range" }

// tupleIndexOutOfRange is the same accident one spelling over. The two containers share the compiled
// layout, so a bounds check that quotes one sentence for both tells the reader about an object the
// program never wrote — and a traceback is the interface (Gap R.35's rule, roadmap L11.3, ADR 0361).
func tupleIndexOutOfRange() string { return "tuple index out of range" }

// indexOutOfRangeSentenceFor is the one question the bounds doors ask: which spelling does the object
// they are about to subscript have? The answer comes from the same census the printer asks, so one
// program cannot get `list` from the print and `tuple` from the trap (roadmap L11.3, ADR 0361).
func indexOutOfRangeSentenceFor(isTuple bool) string {
	if isTuple {
		return tupleIndexOutOfRange()
	}
	return listIndexOutOfRange()
}

// listAssignmentIndexOutOfRange is the store's own sentence. The reference distinguishes the two
// because the actions differ — one read past the end, one wrote past it — and the shared bounds check
// that normalises a negative index cannot tell them apart unless it is told (ADR 0330).
func listAssignmentIndexOutOfRange() string { return "list assignment index out of range" }
func stringIndexOutOfRange() string         { return "string index out of range" }

// notSubscriptableSentence is the read the reference refuses when the object under the subscript is not
// a container at all: `print([[1, 2][0][0], 1])` folds the INNER read to the integer 1, and the outer
// subscript therefore stands on a number, which CPython answers
// `TypeError: 'int' object is not subscriptable` — a trap the program can catch, not a build the
// compiler may decline (roadmap Gap R.227, ADR 0440). The tag-reading run-time door quotes the same
// helper, so one program gets one message whichever road read the slot (ADR 0326's one-question rule).
func notSubscriptableSentence(kind string) string {
	return "'" + kind + "' object is not subscriptable"
}

// constantSubscriptKind names the constant a subscript is really standing on. The inner read of
// `[[1, 2][0][0], 1]`, of `[1, 2, 3][0][0]` and of `(1, 2)[0][0]` is answered at compile time, and the
// answer is a number, a verdict or nothing — so the program has written `1[0]`, which the reference
// answers with a catchable TypeError rather than a build that stops. Walking one fold level is the whole
// of it: the fold's answer IS that constant, and the AST node the outer subscript sees is still the inner
// `Index` (roadmap Gap R.227, ADR 0440). A TEXT is deliberately absent from both halves: `"ab"[0][0]` is
// `'a'[0]` to CPython and answers `'a'`, so the character road keeps its own arm, and a pair door that can
// answer the read is asked before this one is.
func (g *irGen) constantSubscriptKind(e Expr) (string, bool) {
	switch x := e.(type) {
	case *IntLit:
		return "int", true
	case *FloatLit:
		return "float", true
	case *BoolLit:
		return "bool", true
	case *NoneLit:
		return "NoneType", true
	case *Index:
		if g == nil {
			return "", false
		}
		if el, ok := g.foldedLiteralElemOf(x); ok {
			return g.constantSubscriptKind(el)
		}
	}
	return "", false
}

// foldedLiteralElemOf is the element the compile-time read of a list or tuple literal answers, asked of
// the read rather than of the container: the position the program named, resolved only where
// listLiteralFoldAnswers says the whole literal is readable at compile time (the same question
// valueIndex's list arm asks before it folds, so the fold and this walk cannot disagree about which
// literal is a constant — ADR 0326's one-question rule, roadmap Gap R.227). A position outside the
// literal, and a literal the fold cannot read, answer "no": the pair door and the bounds check own those,
// and this road exists only for the accident the fold has already decided.
func (g *irGen) foldedLiteralElemOf(ix *Index) (Expr, bool) {
	ll, isList := ix.Obj.(*ListLit)
	if !isList || !g.listLiteralFoldAnswers(ll, ix.Idx) {
		return nil, false
	}
	il, isInt := ix.Idx.(*IntLit)
	if !isInt {
		return nil, false
	}
	pos := int(normPosIndex(il.Value, int64(len(ll.Elems))))
	if pos < 0 || pos >= len(ll.Elems) {
		return nil, false
	}
	return ll.Elems[pos], true
}

// The `int()`/`float()` conversion sentences, in CPython's own words. Both quote the *source text* it
// was handed, repr-quoted, which is the part a reader acts on — `invalid literal for int() with base 10`
// without the offending text is a message that tells the author nothing about which argument to fix.
func intLiteralSentence(src string) string {
	return "invalid literal for int() with base 10: " + pyReprString(src)
}

func floatLiteralSentence(src string) string {
	return "could not convert string to float: " + pyReprString(src)
}

// ordSentence is `ord()`'s own complaint about a text that is not exactly one character. The length it
// quotes is the code-point count, not the byte count — `"é"` is one character to CPython and two bytes
// to a Go string, and quoting the byte count would blame the author for a string they did not write.
func ordSentence(n int) string {
	return "ord() expected a character, but string of length " + strconv.Itoa(n) + " found"
}

// chrSentence is the range complaint `chr()` raises outside the scalar range.
func chrSentence() string { return "chr() arg not in range(0x110000)" }

// convertArgumentSentence is the two converters' complaint about an operand that is neither a text nor
// a number. CPython spells them differently — `int()` names the whole set it accepts, `float()` names
// two things — so they are two sentences, not one template, and quoting the wrong one is the kind of
// message an author cannot act on.
func convertArgumentSentence(fn, kind string) string {
	if fn == "float" {
		return "float() argument must be a string or a real number, not '" + kind + "'"
	}
	return "int() argument must be a string, a bytes-like object or a real number, not '" + kind + "'"
}

// unconvertibleArg names the kind of an `int()`/`float()` operand the reference refuses to convert, and
// "" for everything the ordinary road already handles — including a bool, which IS a number to the
// reference, and a text, which the fold above answers. A name the compiler holds no record for is ""
// too: its kind is a run-time fact, and this row is about the constants the fold can see.
func (g *irGen) unconvertibleArg(e Expr) string {
	switch e.(type) {
	case *NoneLit:
		return "NoneType"
	case *ListLit:
		return "list"
	case *DictLit:
		return "dict"
	case *SetLit:
		return "set"
	}
	if g.isNoneExpr(e) {
		return "NoneType"
	}
	if g.nameIsContainerRecorded(e) {
		if k := g.containerKindName(e); k != "" {
			return k
		}
		return "list"
	}
	return ""
}

// intFromText is CPython's `int(text)` grammar, applied to a constant the compiler is being asked to
// fold. It accepts what the reference accepts — surrounding whitespace, one sign, decimal digits with
// single underscores between them — and nothing else, because the fold's job is to agree with the
// reference, not to be a convenient `strconv.ParseInt`.
//
// The third result distinguishes "this text is not an integer" (a trap, which is what the reference
// raises) from "this text is an integer this backend cannot hold" (a refusal, which is roadmap L12.12's
// unbounded-`int` decision): folding an out-of-word value to the wrong word and answering it at exit 0
// is the class of wrong answer ADR 0166 counts as our bug, and it is not this row's to invent an answer
// for.
func intFromText(s string) (int64, bool, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, false, false
	}
	body := t
	if body[0] == '+' || body[0] == '-' {
		body = body[1:]
	}
	if body == "" || !digitsAndUnderscores(body) {
		return 0, false, false
	}
	n, err := strconv.ParseInt(strings.ReplaceAll(t, "_", ""), 10, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return 0, false, true // a real integer, wider than this backend's word
		}
		return 0, false, false
	}
	return n, true, false
}

// floatFromText is `float(text)`: the same grammar plus the exponent, the decimal point and the
// spellings `strconv` and CPython agree on. The underscore pass is what keeps `float("1_0.5")` answering
// 10.5 rather than raising, which is the reference's answer.
func floatFromText(s string) (float64, bool) {
	t := strings.TrimSpace(s)
	if t == "" {
		return 0, false
	}
	if f, err := strconv.ParseFloat(t, 64); err == nil {
		return f, true
	}
	if !underscoresBetweenDigits(t) {
		return 0, false
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(t, "_", ""), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// digitsAndUnderscores is "one or more decimal digits, in which an underscore appears only between two
// digits" — the only liberty CPython lets an integer literal written as text.
func digitsAndUnderscores(s string) bool {
	if s == "" {
		return false
	}
	prevDigit := false
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch == '_' {
			if !prevDigit || i+1 >= len(s) || s[i+1] < '0' || s[i+1] > '9' {
				return false
			}
			prevDigit = false
			continue
		}
		if ch < '0' || ch > '9' {
			return false
		}
		prevDigit = true
	}
	return prevDigit
}

// underscoresBetweenDigits is the one liberty CPython lets a numeric literal written as text: an
// underscore between two digits. `1_0.5`, `1_000_000.0` and `1e1_0` are accepted; `_1`, `1_`, `1__0`
// and `1._0` are `ValueError`s. Checking the spelling before the underscores are removed — rather than
// deleting them and hoping — is what keeps `float("1__0")` raising the reference's sentence instead of
// answering 10.
func underscoresBetweenDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '_' {
			continue
		}
		if i == 0 || i+1 >= len(s) {
			return false
		}
		if s[i-1] < '0' || s[i-1] > '9' || s[i+1] < '0' || s[i+1] > '9' {
			return false
		}
	}
	return true
}
