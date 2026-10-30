package lang

import (
	"fmt"
	"strings"
)

// The length of a slice of a TEXT (roadmap Gap R.240, ADR 0470).
//
// `s = "abcd"` / `print(len(s[1:3]))` is CPython's `2`. The answer already existed twice over in this
// backend before this file: `print(len(s))` asks the string table how many CODE POINTS `s` holds, and
// `print(s[1:3])` asks the slice road for the folded text and prints `bc` — so the characters are known
// and counted in two neighbouring roads, and only the length door had no arm for the shape. It fell
// through every container arm of `builtinLen` and died with `codegen: len requires an inline
// list/dict/set literal` at exit 1: a sentence about a container the program never mentions, in a
// program whose only literal is a text.
//
// The arm is therefore NOT a second slice road and not a second counting rule. Two rules decide
// anything here and both already have an owner:
//
//   * WHICH object the slice selects, in which characters — the slice road (`valueSlice`'s text branch,
//     with **ADR 0336**'s bound gate and step-zero raise in front of it). This file EXTRACTS that branch
//     (`valueTextSlice`) rather than copying it, because a copy would be a second place that decides what
//     a text slice is, and **ADR 0232**'s one-question rule is the whole reason the fold, the bounds and
//     the count have stayed in agreement this long. The extraction is also why `len` cannot disagree with
//     `print` about a program like `s = "abc\u00e9llo"` / `len(s[1:4])`, and why the shape **Gap R.243**
//     still owes (a step that is a VALUE) will be answered by `len` the day the road answers it, with no
//     change to this file.
//
//   * HOW LONG a text is — `rt_str_nchars`, the same helper `len` asks for a text variable (**ADR 0225**:
//     code points, never bytes). Asking the OBJECT rather than re-deriving a count means the folded
//     spelling (`s[1:3]`, interned at compile time) and the value-bounded spelling (`s[i:j]`, built by
//     `rt_str_slice`) are measured by one rule, and a program whose bounds are constants cannot print a
//     different number from the same program with the bounds in variables.
//
// The order is load-bearing, and it is the slice road's own: the base is asked before anything
// (**ADR 0466**/**ADR 0468**'s one gate — a set or a dict complains about the object, and `len` must
// inherit that complaint rather than reach the bounds), then the bounds (**ADR 0336**: `len(s["a":"b"])`
// is `TypeError: slice indices must be integers or None or have an __index__ method`, not the length of
// whatever the interned index happens to select), then the step, and only then the text and its count.
// A `len` that counted first would turn a raise into a number at exit 0, which is the class of bug this
// loop cannot close (**ADR 0166**/**ADR 0211**).

// sliceRoadCallsItAText is the question `valueSlice` asks its base to decide it is standing in the text
// branch, exported so the length door can ask the SAME question instead of guessing from the base's
// spelling. It emits nothing and raises nothing: it is a read of the records, and a "yes" here is a
// promise that `valueTextSlice` has an answer or a refusal, never a wrong value.
func (g *irGen) sliceRoadCallsItAText(n *Slice) bool {
	if g == nil || n == nil {
		return false
	}
	// The road's own words, unchanged: a base the fold can read, or a base the records call a text even
	// when the compiler cannot read its characters (`s = "abcdef"` inside a function body — ADR 0229).
	if _, folded := g.stringVal(n.Obj); folded {
		return true
	}
	return g.exprIsString(n.Obj)
}

// valueTextSlice is `valueSlice`'s text branch, extracted verbatim so that the slice road and the length
// door walk the same gates in the same order. It returns the register holding the selected text, or
// `raised` when the branch put the program's exception where the read was — in which case the caller MUST
// NOT emit anything else in this block, and must not answer anything either: the raise is the answer.
func (g *irGen) valueTextSlice(b *strings.Builder, n *Slice) (string, bool, error) {
	// A text slice counts characters, so its bounds are positions and the reference's sentence for a
	// bound that is neither an integer nor `None` is the same one a list's slice gets. Asked before the
	// fold and before the helper, because both read a bound as a number without asking what it holds:
	// `print(s["a":"b"])` over `s = "abcd"` answered `b` at exit 0 (roadmap Gap R.233, ADR 0336).
	if g.badSliceBoundGate(b, n) {
		return "0", true, nil
	}
	// A step of zero is the reference's `ValueError: slice step cannot be zero`, and the fold reads a
	// step as a stride without asking whether it strides at all — asked here, before the fold and
	// before the helper, so the shape that used to be refused with `cannot fold string slice` is the
	// program's exception instead (roadmap Gap R.236, ADR 0336).
	if g.sliceStepIsKnownZero(n) {
		g.raiseSliceStepIsZero(b, n)
		return "0", true, nil
	}
	if sv, ok := g.stringVal(n); ok {
		// Folded: the text is the program's own characters, interned; the length door measures it with
		// the same helper a text variable's length asks, so neither spelling is a special case (ADR 0224).
		return g.internStr(b, sv), false, nil
	}
	// Bounds that are values rather than constants are still a slice of the same
	// string: an absent bound is spelled with the helper's own sentinel, because an
	// i32 argument cannot be "missing" (ADR 0229).
	if g.sliceBoundIsAbsent(n.Step) {
		if sreg, sok, serr := g.strReg(b, n.Obj); serr != nil {
			return "", false, serr
		} else if sok {
			lo := "-2147483648"
			hi := "2147483647"
			if !g.sliceBoundIsAbsent(n.Low) {
				v, lerr := g.strPosReg(b, n.Low)
				if lerr != nil {
					return "", false, lerr
				}
				lo = v
			}
			if !g.sliceBoundIsAbsent(n.High) {
				v, herr := g.strPosReg(b, n.High)
				if herr != nil {
					return "", false, herr
				}
				hi = v
			}
			r := g.rtStrCall(b, "rt_str_slice", "i32 "+sreg, "i32 "+lo, "i32 "+hi)
			g.checkStrSentinels(b, r, "IndexError", "string slice out of range", n.Span(), "stslice")
			return r, false, nil
		}
	}
	// A step the fold cannot read (a step that is a VALUE: `s[::k]`) is Gap R.243's, and the refusal is
	// the road's own sentence — the length door adds nothing to it, and in particular does not fall
	// through to `len`'s container refusal, which would blame a container for a text.
	return "", false, fmt.Errorf("cannot fold string slice")
}

// lenOfTextSlice is `len`'s arm for a slice of a text: the text the slice road selects, measured in code
// points. `handled` is false for every base the slice road does not call a text — a list slice, a dict
// slice, a slice of nothing — and those keep the length road's own answers and refusals untouched.
func (g *irGen) lenOfTextSlice(b *strings.Builder, arg Expr) (string, bool, error) {
	n, isSlice := arg.(*Slice)
	if !isSlice {
		return "", false, nil
	}
	// The base is asked exactly twice on this path, both of them the slice road's own questions: the
	// object first (a set has no position to slice from, a dict hashes the slice as a key — neither
	// reaches the length, and `len` may not either), and only then "is this a text?".
	if g.sliceRaisesWhatTheBaseIs(b, n) {
		return "0", true, nil
	}
	if !g.sliceRoadCallsItAText(n) {
		return "", false, nil
	}
	reg, raised, err := g.valueTextSlice(b, n)
	if err != nil {
		return "", false, err
	}
	if raised {
		// The bounds or the step already put a raise where the read was. Nothing is measured, nothing is
		// emitted after a terminator, and no number is printed for a program the reference aborts.
		return "0", true, nil
	}
	// Code points, not bytes — the same helper `len(s)` asks for a text variable, so the answer cannot
	// depend on which road produced the text (ADR 0225, ADR 0229).
	return g.rtStrCall(b, "rt_str_nchars", "i32 "+reg), true, nil
}
