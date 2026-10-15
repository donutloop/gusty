package lang

// boolargs_test.go — roadmap Gap R.111 (L11.1's remaining bool item).
//
// The promise is narrow and has to be held exactly: a parameter is treated as a verdict *only*
// when every call site the program contains fills that position with one, and a parameter about
// which the compiler is unsure keeps printing the number it always printed. A test that only
// checked the marking would pass while the analysis guessed, so half of these cases are
// refusals, and the refusal cases are the ones that would break a user program if they regressed.

import (
	"fmt"
	"sort"
	"testing"
)

func markedParams(t *testing.T, src string) map[string][]int {
	t.Helper()
	p := mustParse(t, src)
	kinds := boolArgKinds(p)
	out := map[string][]int{}
	for fn, m := range kinds {
		idxs := make([]int, 0, len(m))
		for i := range m {
			idxs = append(idxs, i)
		}
		sort.Ints(idxs)
		out[fn] = idxs
	}
	return out
}

func TestBoolArgKindsMarksAVerdictPosition(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		src  string
		fn   string
		want []int
	}{
		{"literal argument", "def show(f):\n    print(f)\n\nshow(True)\n", "show", []int{0}},
		{"both literals", "def show(f):\n    print(f)\n\nshow(True)\nshow(False)\n", "show", []int{0}},
		{"comparison argument", "def show(f):\n    print(f)\n\nshow(1 == 1)\n", "show", []int{0}},
		{"not argument", "def show(f):\n    print(f)\n\nshow(not 0)\n", "show", []int{0}},
		{"membership argument", "def show(f):\n    print(f)\n\nshow(1 in [1, 2])\n", "show", []int{0}},
		{"bool annotation", "def show(f: bool):\n    print(f)\n\nshow(True)\n", "show", []int{0}},
		{"bool default", "def show(f=True):\n    print(f)\n\nshow()\n", "show", []int{0}},
		{"keyword argument", "def show(a, f):\n    print(f)\n\nshow(1, f=True)\n", "show", []int{1}},
		{"second position only", "def show(a, f):\n    print(a)\n    print(f)\n\nshow(1, True)\n", "show", []int{1}},
		{"forwarded through a helper", "def inner(f):\n    print(f)\n\ndef outer(f):\n    inner(f)\n\nouter(True)\n", "inner", []int{0}},
		{"forwarded twice", "def inner(f):\n    print(f)\n\ndef mid(f):\n    inner(f)\n\ndef outer(f):\n    mid(f)\n\nouter(True)\n", "inner", []int{0}},
		{"method, self not counted", "class C:\n    def m(self, f):\n        print(f)\n\nz = C()\nz.m(True)\n", "m", []int{1}},
		{"verdict from a builtin", "def show(f):\n    print(f)\n\nshow(all([1, 1]))\n", "show", []int{0}},
		{"and of two verdicts", "def show(f):\n    print(f)\n\nshow(True and False)\n", "show", []int{0}},
		{"ternary of two verdicts", "def show(f):\n    print(f)\n\nshow(True if 1 else False)\n", "show", []int{0}},
		{"a name the module bound to a verdict", "flag = True\ndef show(f):\n    print(f)\n\nshow(flag)\n", "show", []int{0}},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got := markedParams(t, r.src)
			if fmt.Sprint(got[r.fn]) != fmt.Sprint(r.want) {
				t.Errorf("%s: marked %v, want %v (whole answer %v)", r.name, got[r.fn], r.want, got)
			}
		})
	}
}

// TestBoolArgKindsDeclinesWhereTheEvidenceIsThin is the half that keeps the analysis honest.
func TestBoolArgKindsDeclinesWhereTheEvidenceIsThin(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		src  string
		fn   string
	}{
		{"one numeric call site contradicts every verdict", "def show(f):\n    print(f)\n\nshow(True)\nshow(1)\n", "show"},
		{"a number alone", "def show(f):\n    print(f)\n\nshow(1)\n", "show"},
		{"a float alone", "def show(f):\n    print(f)\n\nshow(1.5)\n", "show"},
		{"a text alone", "def show(f):\n    print(f)\n\nshow(\"x\")\n", "show"},
		{"a container alone", "def show(f):\n    print(f)\n\nshow([1])\n", "show"},
		{"a dict alone", "def show(f):\n    print(f)\n\nshow({1: 2})\n", "show"},
		{"None alone", "def show(f):\n    print(f)\n\nshow(None)\n", "show"},
		{"an unknown name", "def show(f):\n    print(f)\n\nshow(mystery)\n", "show"},
		{"an arithmetic argument", "def show(f):\n    print(f)\n\nshow(2 + 3)\n", "show"},
		{"the body rebinds the name to a number", "def show(f):\n    print(f)\n    f = 3\n\nshow(True)\n", "show"},
		{"two classes share the method name", "class A:\n    def run(self, f):\n        print(f)\n\nclass B:\n    def run(self, f):\n        print(f)\n\na = A()\nb = B()\na.run(True)\nb.run(1)\n", "run"},
		{"two functions share the name", "def show(f):\n    print(f)\n\ndef show(f):\n    print(f)\n\nshow(True)\n", "show"},
		{"an annotation the call sites contradict", "def show(f: bool):\n    print(f)\n\nshow(0)\n", "show"},
		{"an index reads a value", "xs = [1]\ndef show(f):\n    print(f)\n\nshow(xs[0])\n", "show"},
		{"a call to an unresolved helper", "def show(f):\n    print(f)\n\nshow(mystery(1))\n", "show"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if got := markedParams(t, r.src); len(got[r.fn]) != 0 {
				t.Errorf("%s: marked %v, want nothing marked (whole answer %v)", r.name, got[r.fn], got)
			}
		})
	}
}

// TestBoolLocalsCarryTheBodyOwnVerdicts is the calling-side half: what a body's own bindings
// hold, so a call is a verdict when the callee says it is — not when the caller's names happen to
// have the same spelling.
func TestBoolLocalsCarryTheBodyOwnVerdicts(t *testing.T) {
	t.Parallel()
	src := "def isit():\n    b = True\n    return b\n\ndef other():\n    return 1\n\nprint(isit())\n"
	p := mustParse(t, src)
	facts := verdictFacts(p)
	if !facts.locals["isit"]["b"] {
		t.Errorf("isit's own verdict local is missing: %v", facts.locals["isit"])
	}
	env := BoolEnv{
		Lookup:     func(n string) *FuncDef { return findNamedFunc(p, n) },
		CalleeVars: facts.of,
	}
	if !IsBoolExpr(&Call{Fn: &Name{Value: "isit"}}, env) {
		t.Error("isit() is not read as a verdict, so print(isit()) answers 1 where CPython answers True")
	}
	if IsBoolExpr(&Call{Fn: &Name{Value: "other"}}, env) {
		t.Error("other() returns 1 and was read as a verdict")
	}
}

// TestTheVerdictQuestionDoesNotGuessAcrossScopes: one function's `flag` must not make another
// function's `flag` print True. The tables are scope-keyed for exactly this.
func TestTheVerdictQuestionDoesNotGuessAcrossScopes(t *testing.T) {
	t.Parallel()
	src := "def a(flag):\n    print(flag)\n\ndef b(flag):\n    print(flag)\n\na(True)\nb(1)\n"
	got := markedParams(t, src)
	if len(got["a"]) != 1 || got["a"][0] != 0 {
		t.Errorf("a's verdict parameter is missing: %v", got)
	}
	if len(got["b"]) != 0 {
		t.Errorf("b is marked from a's call site — the tables leaked across scopes: %v", got)
	}
}

// TestDefinitelyNotABoolIsClosed: the negation is a closed list of shapes, so a construct this
// file cannot judge is neither marked nor refused, and an unresolved program keeps printing
// exactly what it printed before this file existed.
func TestDefinitelyNotABoolIsClosed(t *testing.T) {
	t.Parallel()
	notBool := map[string]Expr{
		"int":      &IntLit{Value: 1},
		"float":    &FloatLit{Value: 1.5},
		"text":     &StrLit{Value: "x"},
		"none":     &NoneLit{},
		"list":     &ListLit{},
		"dict":     &DictLit{},
		"set":      &SetLit{},
		"tuple":    &Tuple{},
		"index":    &Index{Obj: &Name{Value: "xs"}, Idx: &IntLit{Value: 0}},
		"minus":    &UnOp{Op: "-", X: &Name{Value: "x"}},
		"multiply": &BinOp{Op: "*", L: &Name{Value: "x"}, R: &IntLit{Value: 2}},
	}
	for name, e := range notBool {
		if !definitelyNotABool(e) {
			t.Errorf("%s is not read as a non-verdict, so a call site could mark it", name)
		}
	}
	unknown := map[string]Expr{
		"name":     &Name{Value: "mystery"},
		"call":     &Call{Fn: &Name{Value: "mystery"}},
		"attr":     &Attr{Obj: &Name{Value: "z"}, Name: &Name{Value: "x"}},
		"cond":     &CondExpr{If: &BoolLit{Value: true}, Cond: &Name{Value: "c"}, Else: &Name{Value: "d"}},
		"and":      &BinOp{Op: "and", L: &Name{Value: "a"}, R: &Name{Value: "b"}},
		"not":      &UnOp{Op: "not", X: &Name{Value: "x"}},
		"keyword":  &KeywordArg{Name: "f", Value: &Name{Value: "mystery"}},
		"callBool": &Call{Fn: &Name{Value: "mystery"}, Args: []Expr{&IntLit{Value: 1}}},
	}
	for name, e := range unknown {
		if definitelyNotABool(e) {
			t.Errorf("%s is judged a non-verdict without evidence; that turns an unknown into a decline", name)
		}
	}
	if definitelyNotABool(&BoolLit{Value: true}) {
		t.Error("a bool literal read as a non-verdict — the whole analysis would decline")
	}
}

func findNamedFunc(p *Program, name string) *FuncDef {
	for _, st := range p.Stmts {
		if fd, ok := st.(*FuncDef); ok && fd.Name == name {
			return fd
		}
	}
	return nil
}
