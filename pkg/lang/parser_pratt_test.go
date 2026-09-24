package lang

import (
	"testing"
)

// renderPratt canonicalizes an expression AST into a parenthesized string so
// precedence and associativity decisions are visible and testable.
func renderPratt(e Expr) string {
	switch n := e.(type) {
	case *Name:
		return n.Value
	case *IntLit:
		return n.Text
	case *FloatLit:
		return n.Text
	case *BoolLit:
		return "true"
	case *UnOp:
		return "(" + n.Op + " " + renderPratt(n.X) + ")"
	case *BinOp:
		return "(" + renderPratt(n.L) + " " + n.Op + " " + renderPratt(n.R) + ")"
	case *CondExpr:
		return "(" + renderPratt(n.If) + " if " + renderPratt(n.Cond) + " else " + renderPratt(n.Else) + ")"
	case *Call:
		return n.Fn.(*Name).Value + "(args)"
	case *Index:
		return n.Obj.(*Name).Value + "[idx]"
	case *Attr:
		return n.Obj.(*Name).Value + ".attr"
	default:
		return "?"
	}
}

func prattExpr(t *testing.T, src string) string {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	if len(prog.Stmts) != 1 {
		t.Fatalf("expected 1 stmt, got %d", len(prog.Stmts))
	}
	es, ok := prog.Stmts[0].(*ExprStmt)
	if !ok {
		t.Fatalf("expected ExprStmt, got %T", prog.Stmts[0])
	}
	return renderPratt(es.Expr)
}

func TestPrattPrecedence(t *testing.T) {
	cases := []struct{ src, want string }{
		// multiplicative binds tighter than additive
		{"a + b * c", "(a + (b * c))"},
		{"a * b + c", "((a * b) + c)"},
		{"a + b - c + d", "(((a + b) - c) + d)"},
		{"a * b / c % d", "(((a * b) / c) % d)"},
		{"a + b // c", "(a + (b // c))"},
		// comparison binds looser than additive
		{"a < b + c", "(a < (b + c))"},
		{"a + b == c", "((a + b) == c)"},
		{"a < b < c", "((a < b) < c)"},
		{"a == b != c", "((a == b) != c)"},
		{"a in b", "(a in b)"},
		{"a not in b", "(a not in b)"},
		{"a is b", "(a is b)"},
		{"a is not b", "(a is not b)"},
		{"a is b == c", "((a is b) == c)"},
		// logical operators
		{"a and b", "(a and b)"},
		{"a and b or c", "((a and b) or c)"},
		{"a or b and c", "(a or (b and c))"},
		{"not a == b", "(not (a == b))"},
		{"not a and b", "((not a) and b)"},
		{"a and not b", "(a and (not b))"},
		{"not not x", "(not (not x))"},
		// power is right-associative and binds tighter than unary minus
		{"2 ** 3 ** 2", "(2 ** (3 ** 2))"},
		{"- 2 ** 2", "(- (2 ** 2))"},
		{"2 ** - 2", "(2 ** (- 2))"},
		{"2 ** 3 + 4", "((2 ** 3) + 4)"},
		// ternary is right-associative and lowest-precedence
		{"a if b else c", "(a if b else c)"},
		{"a if b else c if d else e", "(a if b else (c if d else e))"},
		{"a + b if c else d", "((a + b) if c else d)"},
		{"a if b in c else d", "(a if (b in c) else d)"},
		{"a if b else c and d", "(a if b else (c and d))"},
	}
	for _, c := range cases {
		got := prattExpr(t, c.src)
		if got != c.want {
			t.Errorf("%q: got %q want %q", c.src, got, c.want)
		}
	}
}

func TestPrattPostfix(t *testing.T) {
	for _, src := range []string{
		"f(a, b)",
		"g(x)[i].m",
		"f(a, b) + g(x)[i].m",
		"a[1:2]",
		"a[:2]",
		"a[::2]",
	} {
		if _, err := Parse(src); err != nil {
			t.Errorf("parse %q: %v", src, err)
		}
	}
}

// TestPrattKeywords verifies keyword operators parse and don't leak tokens.
func TestPrattKeywords(t *testing.T) {
	for _, src := range []string{
		"x in y",
		"x not in y",
		"x is y",
		"x is not y",
		"x and y or z",
		"x if y else z",
	} {
		if _, err := Parse(src); err != nil {
			t.Errorf("parse %q: %v", src, err)
		}
	}
}
