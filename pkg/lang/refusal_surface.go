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
