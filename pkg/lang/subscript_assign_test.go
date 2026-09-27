package lang

import (
	"strings"
	"testing"
)

// Subscript assignment (`d[k] = v`, `xs[i] = v`).
//
// The statement used to parse as an expression statement with the `= value` consumed and
// thrown away: `d[1] = 2` compiled, verified, ran, and did nothing — on both backends.
// These tests pin the AST shape, the runtime semantics, and the emitted IR.

func TestSubscriptAssignmentParsesAsAssignment(t *testing.T) {
	prog, err := Parse("d = {}\nd[1] = 2\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(prog.Stmts) != 2 {
		t.Fatalf("want 2 statements, got %d", len(prog.Stmts))
	}
	as, ok := prog.Stmts[1].(*AssignStmt)
	if !ok {
		t.Fatalf("d[1] = 2 must be an AssignStmt, got %T", prog.Stmts[1])
	}
	ix, ok := as.Target.(*Index)
	if !ok {
		t.Fatalf("target must be an Index, got %T", as.Target)
	}
	obj, ok := ix.Obj.(*Name)
	if !ok || obj.Value != "d" {
		t.Errorf("index target should read from d, got %#v", ix.Obj)
	}
	if _, ok := as.Value.(*IntLit); !ok {
		t.Errorf("assigned value should be the literal 2, got %T", as.Value)
	}
}

func TestNonAssignableTargetIsAnErrorNotADrop(t *testing.T) {
	// `def f(): return 1` then `f() = 1` — this used to parse and silently do nothing.
	_, err := Parse("def f():\n    return 1\n\nf() = 1\n")
	if err == nil {
		t.Fatalf("assigning to a call result must be a parse error")
	}
	if !strings.Contains(err.Error(), "cannot assign") {
		t.Errorf("error should name the problem, got: %v", err)
	}
}

func TestEmptyBracesIsAnEmptyDict(t *testing.T) {
	prog, err := Parse("d = {}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	as := prog.Stmts[0].(*AssignStmt)
	if _, ok := as.Value.(*DictLit); !ok {
		t.Fatalf("`{}` is an empty dict in Python; got %T", as.Value)
	}
	// A non-empty brace without colons is still a set.
	prog2, err := Parse("s = {1, 2}\n")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, ok := prog2.Stmts[0].(*AssignStmt).Value.(*SetLit); !ok {
		t.Errorf("`{1, 2}` is a set, got %T", prog2.Stmts[0].(*AssignStmt).Value)
	}
}

func TestInterpreterSubscriptAssignment(t *testing.T) {
	cases := []struct {
		src  string
		want string // what print()s must show
	}{
		{"d = {}\nd[1] = 2\nprint(d[1])\n", "2\n"},
		{"d = {1: 2}\nd[1] = 9\nprint(d[1])\n", "9\n"},
		{"d = {}\nfor i in range(3):\n    d[i] = i * 10\n\nprint(len(d))\nprint(d[2])\n", "3\n20\n"},
		{"xs = [1, 2, 3]\nxs[1] = 9\nprint(xs)\n", "[1, 9, 3]\n"},
		{"xs = [1, 2, 3]\nt = 0\nfor i in range(3):\n    xs[i] = xs[i] + 1\n\nprint(xs)\n", "[2, 3, 4]\n"},
	}
	for _, tc := range cases {
		out, err := InterpreterRun(tc.src)
		if err != nil {
			t.Errorf("InterpreterRun(%q): %v", tc.src, err)
			continue
		}
		if out != tc.want {
			t.Errorf("InterpreterRun(%q) = %q, want %q", tc.src, out, tc.want)
		}
	}
}

func TestInterpreterSubscriptAssignmentErrors(t *testing.T) {
	cases := []struct{ src, want string }{
		{"xs = [1]\nxs[5] = 2\n", "index out of range"},
		{"s = {1, 2}\ns[0] = 5\n", "cannot assign to a set element"},
	}
	for _, tc := range cases {
		_, _, err := EvalExpr(tc.src)
		if err == nil {
			t.Errorf("EvalExpr(%q) should fail", tc.src)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("EvalExpr(%q) error = %v, want to contain %q", tc.src, err, tc.want)
		}
	}
}

func TestIRSubscriptAssignment(t *testing.T) {
	res, err := Compile("d = {}\nd[1] = 2\nprint(len(d))\n")
	if err != nil {
		t.Fatalf("Compile(dict): %v", err)
	}
	if !strings.Contains(res.IR, "call void @rt_dict_put(") {
		t.Errorf("dict item assignment must use rt_dict_put:\n%s", res.IR)
	}
	if v, err := VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
		t.Errorf("dict assignment module must verify: %v %v", v.Errors, err)
	}

	res, err = Compile("xs = [1, 2, 3]\nxs[1] = 9\nprint(len(xs))\n")
	if err != nil {
		t.Fatalf("Compile(list): %v", err)
	}
	for _, want := range []string{"call void @rt_put_elem(", "call i32 @rt_list_len(", "store i32 1, i32* @exn_flag"} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("list item assignment must bounds-check and use rt_put_elem (%q missing):\n%s", want, res.IR)
		}
	}
	if v, err := VerifyModuleIR(res.IR, 0); err != nil || !v.OK {
		t.Errorf("list assignment module must verify: %v %v", v.Errors, err)
	}
}

func TestIRSubscriptAssignmentDiagnostics(t *testing.T) {
	cases := []struct{ src, want string }{
		{"s = {1, 2}\ns[0] = 5\nprint(1)\n", "sets do not support item assignment"},
		{"def f(x):\n    return x\n\nf() = 1\n", "cannot assign"},
	}
	for _, tc := range cases {
		_, err := Compile(tc.src)
		if err == nil {
			t.Errorf("Compile(%q) should fail", tc.src)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Compile(%q) error = %v, want to contain %q", tc.src, err, tc.want)
		}
	}
}
