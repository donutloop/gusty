package lang

// refusal_surface.go — the two questions a refusal about a NODE has to answer in the program's own
// words (roadmap L11.7 · **Gap R.377**, ADR 0398).
//
// A refusal has three jobs, all of them ADR 0166's and ADR 0249's: name what is missing, name the
// roadmap row that owns the missing half, and never name the compiler's own machinery. The first two
// are taught; the third has a standing exception, and it is this one:
//
//	gustyc: jit: codegen: a *lang.ExprStmt statement cannot be compiled: `key=lambda ...` is refused:
//	a lambda is a function VALUE, and this backend has none — … (roadmap L11.7)
//
// The payload — everything after the colon — is the door's own sentence: it names the construct, the
// missing half and the row. The wrapper around it says nothing a reader can act on, because a reader
// does not have the emitter's type table. It is also the reason two tests of the sibling row had to
// STRIP a prefix before they could assert the sentence was clean (`pkg/lang/key_hook_frame_test.go`,
// `integration/key_hook_frame_test.go`), which is what a carve-out around a leak always means: the
// leak became the test's problem instead of the compiler's.
//
// The sibling arm of the same helper was fixed under **Gap R.38**: a `*FuncDef` is named
// `function "m"`, the program's own word, and the drift ledger quotes that sentence to this line. The
// `Stmt` arm kept the `%T`. It is the last arm, and this file is why it is now the boring one.

import (
	"fmt"
	"reflect"
	"strings"
)

// refusalIsSentence asks whether an error is already the whole sentence a reader needs.
//
// The house rule makes this answerable rather than a guess: ADR 0166 and ADR 0249 require every refusal
// to name the row that owns the missing half, so a sentence the compiler wrote deliberately carries a
// `roadmap` citation (or a bare `ADR `/`Gap ` reference), and a raw Go error from a helper does not. The
// capability registry is the second witness — a payload it classifies (`--capabilities`, ADR 0346) is by
// construction a refusal the backend wrote on purpose, and `--json` prints it as one.
//
// The question is asked ONCE, here (ADR 0232). A caller that wants to wrap a payload in a sentence about
// where it came from asks this first, and a payload that answers yes is handed to the reader untouched.
func refusalIsSentence(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if _, ok := ClassifyCapabilityRefusal(msg); ok {
		return true
	}
	return strings.Contains(msg, "roadmap") || strings.Contains(msg, "ADR ") || strings.Contains(msg, "Gap ")
}

// stmtSurface names a statement the way the program wrote it, with the line it was written on.
//
// It is the statement-side sibling of `exprSurface` and `exprTyName`, and it exists for one reason: the
// only honest way to say "this statement did not lower" is to say WHICH statement, in a language the
// reader has. A Go type name is not that language — `*lang.WithStmt` is what the parser built, not what
// anyone typed.
//
// Every arm returns the KEYWORD the program used plus the line, because the keyword is what the reader
// greps for in their own file. The fallback never falls back to a Go type name either: an unknown
// statement kind is still a statement at a line, and a line is actionable where a type name is not.
func stmtSurface(st Stmt) string {
	if st == nil || reflectIsNil(st) {
		return "the statement"
	}
	line := st.Span().Line
	at := func(kind string) string {
		if line > 0 {
			return fmt.Sprintf("the %s at line %d", kind, line)
		}
		return "the " + kind
	}
	switch s := st.(type) {
	case *ExprStmt:
		// An expression statement is named by its expression, which is the thing the reader can point at
		// and the thing the door that refused actually saw.
		if s.Expr != nil {
			if c, ok := s.Expr.(*Call); ok {
				return at(fmt.Sprintf("`%s(…)` call", callSurfaceName(c)))
			}
			return at(fmt.Sprintf("`%s` expression", exprSurface(s.Expr)))
		}
		return at("expression statement")
	case *ReturnStmt:
		return at("`return`")
	case *RaiseStmt:
		return at("`raise`")
	case *AssignStmt:
		if n, ok := s.Target.(*Name); ok {
			return at(fmt.Sprintf("assignment to `%s`", n.Value))
		}
		return at("assignment")
	case *AugAssignStmt:
		if n, ok := s.Target.(*Name); ok {
			return at(fmt.Sprintf("`%s %s=` assignment", n.Value, s.Op))
		}
		return at("augmented assignment")
	case *IfStmt:
		return at("`if`")
	case *WhileStmt:
		return at("`while`")
	case *ForStmt:
		return at("`for`")
	case *TryStmt:
		return at("`try`")
	case *WithStmt:
		return at("`with`")
	case *MatchStmt:
		return at("`match`")
	case *ImportStmt:
		return at("`import`")
	case *FuncDef:
		return at(fmt.Sprintf("`def %s`", s.Name))
	case *ClassDef:
		return at(fmt.Sprintf("`class %s`", s.Name))
	case *BreakStmt:
		return at("`break`")
	case *ContinueStmt:
		return at("`continue`")
	case *PassStmt:
		return at("`pass`")
	case *YieldStmt:
		return at("`yield`")
	case *YieldFromStmt:
		return at("`yield from`")
	case *TypeAliasStmt:
		return at("type alias")
	case *ExternDecl:
		return at("`extern` declaration")
	}
	return at("statement")
}

// callSurfaceName answers the callee's name as the program wrote it: `print` for `print(…)`, `.m` for a
// method call. It is `exprSummary`'s Call arm asked without a generator, because a refusal about a
// statement is written where there may not be one.
func callSurfaceName(c *Call) string {
	if c == nil {
		return "call"
	}
	if n, ok := c.Fn.(*Name); ok {
		return n.Value
	}
	if at, ok := c.Fn.(*Attr); ok && at.Name != nil {
		return "." + at.Name.Value
	}
	return "call"
}

// reflectIsNil is the guard every surface helper uses before reflect.ValueOf: a typed nil in an
// interface is not nil, and a diagnostic that panics is worse than one that says nothing.
func reflectIsNil(node any) bool {
	if node == nil {
		return true
	}
	v := reflect.ValueOf(node)
	return v.Kind() == reflect.Ptr && v.IsNil()
}

// nodeSurface names an EXPRESSION the way the program wrote it, and says what KIND of thing it is in the
// language's own words — the sentence a door needs when the program put the wrong kind of thing in a
// position (a list literal where a `d[k] = v` store wants a variable, a number on the left of `+=`, an
// attribute of a text).
//
// It is the expression-side sibling of `stmtSurface` (ADR 0398), and it exists for the same reason:
// `*lang.ListLit` is what the parser built, not what anyone typed, and a reader — human or `--json`
// consumer — has no table to look it up in (ADR 0166, ADR 0249). `exprSurface` renders an expression's
// TEXT and falls back to `a %T` for a node the formatter cannot write; a door that quotes that fallback
// would put the Go type name back in the sentence, which is the defect **Gap R.379** files. So this helper
// returns the source text where the formatter knows the node, and an honest "an expression the compiler
// cannot name" where it does not — never a type.
func nodeSurface(e Expr) string {
	if e == nil || reflectIsNil(e) {
		return "nothing"
	}
	text := func(s string, ok bool) (string, bool) {
		if !ok || s == "" {
			return "", false
		}
		return "`" + s + "`", true
	}
	switch x := e.(type) {
	case *Name:
		if q, ok := text(exprSourceText(x)); ok {
			return "the name " + q
		}
		return "the name `…`"
	case *IntLit, *FloatLit:
		if q, ok := text(exprSourceText(x)); ok {
			return "the number " + q
		}
		return "a number"
	case *StrLit:
		if q, ok := text(exprSourceText(x)); ok {
			return "the text " + q
		}
		return "a text"
	case *BoolLit:
		return "a bool"
	case *NoneLit:
		return "None"
	case *ListLit:
		// one container representation, two spellings (ADR 0361): the flag on the node decides whether the
		// reader is told "list" or "tuple" — and the tuple's own parentheses are rendered here, because the
		// canonical formatter writes the shared `[…]` shape and a sentence that quotes `[1, 2]` for what the
		// author typed as `(1, 2)` describes a program they did not write.
		if x.Tuple {
			parts := make([]string, 0, len(x.Elems))
			for _, el := range x.Elems {
				parts = append(parts, exprSurface(el))
			}
			if len(parts) == 1 {
				return "the tuple literal `(" + parts[0] + ",)`"
			}
			return "the tuple literal `(" + strings.Join(parts, ", ") + ")`"
		}
		if q, ok := text(exprSourceText(x)); ok {
			return "the list literal " + q
		}
		return "a list literal"
	case *DictLit:
		if q, ok := text(exprSourceText(x)); ok {
			return "the dict literal " + q
		}
		return "a dict literal"
	case *SetLit:
		if q, ok := text(exprSourceText(x)); ok {
			return "the set literal " + q
		}
		return "a set literal"
	case *Tuple:
		// the same wording as the list, dict and set arms: `(1, 2)` on the left of an assignment is a literal
		// the program wrote, and "literal" is what tells the reader no variable is involved.
		if q, ok := text(exprSourceText(x)); ok {
			return "the tuple literal " + q
		}
		return "a tuple literal"
	case *Call:
		return "the call `" + callSurfaceName(x) + "(…)`"
	case *Attr:
		// "the attribute `x` of a number" — the attribute NAME and the KIND of the thing it is read off, which
		// is the pair the reader needs; `1.x` would be a rendering the program never contained.
		if x.Name != nil {
			return "the attribute `" + x.Name.Value + "` of " + nodeKind(x.Obj)
		}
		return "an attribute of " + nodeKind(x.Obj)
	case *Index:
		if q, ok := text(exprSourceText(x)); ok {
			return "the subscript " + q
		}
		return "a subscript of " + nodeKind(x.Obj)
	}
	if s, ok := exprSourceText(e); ok {
		return "the expression `" + s + "`"
	}
	// The honest fallback. A door that cannot name the node still says so in the reader's language, and
	// the sentence around it names the position the node was in — which is the actionable half.
	return "an expression the compiler cannot name"
}

// nodeKind is `nodeSurface`'s short form for the operand a compound node hangs off: "a number", "a text",
// "the list literal `[1, 2]`", "`objs`". It never carries a Go type name either, for the same reason.
func nodeKind(e Expr) string {
	if e == nil || reflectIsNil(e) {
		return "nothing"
	}
	switch e.(type) {
	case *IntLit, *FloatLit:
		return "a number"
	case *StrLit:
		return "a text"
	case *BoolLit:
		return "a bool"
	case *NoneLit:
		return "None"
	case *ListLit:
		return "a list literal"
	case *DictLit:
		return "a dict literal"
	case *SetLit:
		return "a set literal"
	case *Tuple:
		return "a tuple"
	case *Call:
		return "a call's answer"
	}
	return nodeSurface(e)
}
