package lang

import (
	"os/exec"
	"strings"
	"testing"
)

// A method call fills the CALLEE's parameter vector.
//
// Four call roads in codegen_method.go and one in codegen_instance.go each built their operand list
// with `for i, arg := range c.Args` — one word per argument the SOURCE wrote. The callee reads its
// parameters by position (`g.params[fd.Params[i].Name] = "%p<i>"`, codegen_class.go), so a parameter the
// call omitted was a slot nobody filled and a `name=` keyword was a positional wearing a name:
// `class C():` / `    def m(self, a, b=2):` / `        return b` / `c = C()` / `print(c.m(5))` printed
// `0` where the reference prints `2`, and `def m(self, a=1, b=2):` / `        return a * 10 + b` called
// as `c.m(b=7, a=3)` printed `73` for the reference's `37`. The module-`def` road had been laying the
// vector out over the declared parameters since ADR 0166 (`callUserFunc` in codegen_func.go); the method
// roads are separate roads and had never been asked. Roadmap Gap R.472, ADR 0434.
//
// # The legs
//
// A row is answered when THREE readings agree: the compiled leg, CPython, and — where the retired
// engine's record covers the source — the record's own answer. Every row here is compiled through the
// LLVM JIT and executed; every row but the one the reference's own parser refuses is also run through
// CPython and must print the same bytes.
//
// The owed halves are in TestWhatAMethodCallStillOwesIsFiledNotFixed and the CLI halves in
// integration/method_args_test.go.

// pyOut is the reference leg: run the same source through CPython, the one engine besides this
// backend that has ever executed these programs (the retired interpreter left with ADR 0302, and what it
// left behind is the record, not a runner). A reference that raises is reported as a refusal so a row
// cannot claim an answer the reference does not give.
func pyOut(t *testing.T, src string) (string, bool) {
	t.Helper()
	cmd := exec.Command("python3", "-c", src)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), false
	}
	return string(out), true
}

// methodArgRow is one answered shape: the source, and the bytes both legs print.
type methodArgRow struct{ name, src, want string }

// methodArgAnsweredRowsForTest is the answered family, written once: the case below and the case that audits
// that every row is on the record both read this list, so a row cannot be added to one and forgotten in the
// other (ADR 0232's rule that a question asked in two places is two questions that will drift apart).
func methodArgAnsweredRowsForTest() []methodArgRow {
	return []methodArgRow{
		{
			// The row this one exists for, and the only shape whose compiled answer was wrong on BOTH
			// sides of the door: the body read `b` from a slot no operand reached.
			"an omitted default answers its own value",
			"class C():\n    def m(self, a, b=2):\n        return b\nc = C()\nprint(c.m(5))\n",
			"2\n",
		},
		{
			// A default marks a parameter that may be omitted and says nothing about the parameters
			// around it: the walk fills ONE slot and the positional list keeps counting.
			"defaults on either side of a written argument",
			"class C():\n    def m(self, a, b=10, c=100):\n        return a + b + c\nprint(C().m(1))\n",
			"111\n",
		},
		{
			// The keyword half, and the shape that made the row loud: the values were emitted in the
			// order the source wrote them, so the digits arrived swapped.
			"keyword arguments bind by name, in any order",
			"class C():\n    def m(self, a=1, b=2):\n        return a * 10 + b\nprint(C().m(b=7, a=3))\n",
			"37\n",
		},
		{
			"one keyword argument, the positional list untouched",
			"class C():\n    def m(self, a, b=10, c=100):\n        return a + b + c\nprint(C().m(1, c=5))\n",
			"16\n",
		},
		{
			// `C(5)` reaches the constructor through codegen_instance.go, a fifth road that had read
			// c.Args as the vector too.
			"a constructor's omitted default",
			"class C():\n    def __init__(self, a, b=2):\n        self.b = b\n        self.a = a\nc = C(5)\nprint(c.b)\n",
			"2\n",
		},
		{
			// The defaults come from the class that DECLARES the method, read by the same base walk
			// that resolved the symbol — a call site that filled the vector from the wrong signature
			// would fill it from the subclass, which has none.
			"an inherited method's default",
			"class B():\n    def m(self, a, b=3):\n        return a + b\nclass C(B):\n    pass\nprint(C().m(1))\n",
			"4\n",
		},
		{
			"an override brings its own default",
			"class B():\n    def m(self, a, b=3):\n        return a + b\nclass C(B):\n    def m(self, a, b=4):\n        return a + b\nprint(B().m(1))\nprint(C().m(1))\n",
			"4\n5\n",
		},
		{
			// The run-time class-id switch: the receiver's class is a fact the program decides, so the
			// arms' vectors are built before the switch and each arm carries its OWN method's defaults.
			// Two arms reading two different defaults is the proof the vector is per-arm.
			"a dynamically dispatched method's defaults, per arm",
			"class B():\n    def m(self, a, b=3):\n        return a + b\nclass C(B):\n    def m(self, a, b=5):\n        return a + b\ndef pick(n):\n    if n == 0:\n        return B()\n    return C()\nx = pick(0)\nprint(x.m(1))\nx = pick(1)\nprint(x.m(1))\n",
			"4\n6\n",
		},
		{
			// The `super()` road reads the base class's parameters, and the base's defaults — the
			// enclosing class's `b=4` is the caller's own default, which this line does not pass on.
			"super() reaches the base method's default",
			"class B():\n    def m(self, a, b=3):\n        return a + b\nclass C(B):\n    def m(self, a, b=4):\n        return super().m(a) * 10\nprint(C().m(1))\n",
			"40\n",
		},
		{
			// The verdict half: a defaulted parameter the call omits is still a verdict, and the body's
			// `return flag` travels the bool road rather than a word nothing wrote.
			"an omitted verdict default",
			"class C():\n    def m(self, flag=True):\n        return flag\nprint(C().m())\n",
			"True\n",
		},
		{
			// The generator method the previous cycle's owed table handed over: the buffer's slots are
			// filled from the same vector any other method's parameters are.
			"a generator method's omitted default yields the default",
			"class C():\n    def gen(self, a, b=3):\n        yield a\n        yield b\nc = C()\nfor v in c.gen(7):\n    print(v)\n",
			"7\n3\n",
		}}
}

func TestAMethodCallFillsTheCalleesParameterVector(t *testing.T) {
	t.Parallel()
	for _, tc := range methodArgAnsweredRowsForTest() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			t.Run("compiled leg", func(t *testing.T) {
				t.Parallel()
				if got := compiledOut(t, tc.src); got != tc.want {
					t.Fatalf("compiled printed %q, want %q", got, tc.want)
				}
			})
			t.Run("reference leg", func(t *testing.T) {
				t.Parallel()
				got, ok := pyOut(t, tc.src)
				if !ok {
					t.Fatalf("the reference refused this program (exit non-zero) — the row may not claim an answer it does not give: %s", got)
				}
				if got != tc.want {
					t.Fatalf("reference printed %q, want %q", got, tc.want)
				}
			})
		})
	}
}

// TestTheRecordIsWhatAnOmittedDefaultOwesTheReference documents the one shape in this family the
// reference REFUSES and this language answers: a parameter written AFTER a default. CPython rejects the
// `def` itself (`non-default argument follows default argument`), so the answer can only be witnessed by
// the record — which states it, and states why: a default marks one parameter as omittable and says
// nothing about the parameters around it, and filling the defaulted slot with the next positional would
// have answered `3` where the record says `6`.
func TestTheRecordIsWhatAnOmittedDefaultOwesTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"a default does not steal the next positional",
			"class C:\n    def m(self, a, b=1, c):\n        return a + b + c\n\nz = C().m(1, 2, 3)\n",
			"6",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The RECORD half only. The record's own source ends in `z = C().m(1, 2, 3)`, an assignment, so
			// the compiled run echoes nothing and there is no compiled answer to compare this against
			// without rewriting the record's key — and the printing twin of the shape is in the answered
			// table above, where the program does print.
			if rec := goldenLookup(t, tc.src); rec.Repr != tc.want {
				t.Fatalf("the record holds %q for this source, want %q", rec.Repr, tc.want)
			}
			t.Run("reference leg", func(t *testing.T) {
				t.Parallel()
				if _, ok := pyOut(t, tc.src); ok {
					t.Fatal("the reference accepted a parameter written after a default — if that changed, " +
						"this row's whole claim needs re-measuring")
				}
			})
		})
	}
}

// TestTheVectorIsTheCalleesDeclarationNotTheCallersCount is the reading table. One question — which
// register does parameter `i` arrive in? — is asked of one function, `callArgBinder.callArgs`, and the
// rows say what it answers for each shape of call site. The compiled-leg rows above would notice a wrong
// answer; this table notices a WRONG READING, which is what turns a fixed shape into a rephrased bug when
// the two live in different places (ADR 0232's one-question-one-reading rule).
func TestTheVectorIsTheCalleesDeclarationNotTheCallersCount(t *testing.T) {
	t.Parallel()
	// A row states which EXPRESSION each slot's word came from — the argument the call wrote or the
	// parameter's own default — because that is the reading. Comparing registers by name would pin the
	// temporary allocator's numbering rather than the answer (ADR 0232's one-question-one-reading rule).
	// One shared set of expression pointers per literal, so a row's `args` and its `want` name the SAME
	// expression and the assertion is about identity, not about two look-alike literals.
	lit := map[int64]Expr{}
	i := func(v int64) Expr {
		if lit[v] == nil {
			lit[v] = &IntLit{Value: v}
		}
		return lit[v]
	}
	for _, tc := range []struct {
		name    string
		params  []*Param
		args    []Expr
		want    []Expr // nil == the slot is filled from that parameter's default
		refuses string
	}{
		{
			name:    "an omitted default is filled from its own expression",
			params:  []*Param{{Name: "self"}, {Name: "a"}, {Name: "b", Default: i(2)}},
			args:    []Expr{i(5)},
			want:    []Expr{i(5), nil},
			refuses: "",
		},
		{
			name:    "a keyword binds by name, not by the order it was written",
			params:  []*Param{{Name: "self"}, {Name: "a", Default: i(1)}, {Name: "b", Default: i(2)}},
			args:    []Expr{&KeywordArg{Name: "b", Value: i(7)}, &KeywordArg{Name: "a", Value: i(3)}},
			want:    []Expr{i(3), i(7)},
			refuses: "",
		},
		{
			name:    "a positional list keeps counting past a defaulted slot",
			params:  []*Param{{Name: "self"}, {Name: "a"}, {Name: "b", Default: i(1)}, {Name: "c"}},
			args:    []Expr{i(1), i(2), i(3)},
			want:    []Expr{i(1), i(2), i(3)},
			refuses: "",
		},
		{
			name:    "every omitted slot is filled, in declaration order",
			params:  []*Param{{Name: "self"}, {Name: "a", Default: i(1)}, {Name: "b", Default: i(2)}, {Name: "c", Default: i(3)}},
			args:    nil,
			want:    []Expr{i(1), i(2), i(3)},
			refuses: "",
		},
		{
			name:    "self is not a slot the call can fill",
			params:  []*Param{{Name: "self"}, {Name: "a", Default: i(1)}},
			args:    []Expr{&KeywordArg{Name: "self", Value: i(9)}},
			want:    nil,
			refuses: `unknown keyword argument "self"`,
		},
		{
			name:    "a parameter with no default and no argument is a refusal, not a zero",
			params:  []*Param{{Name: "self"}, {Name: "a"}, {Name: "b"}},
			args:    []Expr{i(1)},
			want:    nil,
			refuses: `missing argument "b"`,
		},
		{
			name:    "one argument past the declared list is a refusal, not a dropped word",
			params:  []*Param{{Name: "self"}, {Name: "a"}},
			args:    []Expr{i(1), i(2)},
			want:    nil,
			refuses: "too many arguments",
		},
		{
			name:    "the same parameter written twice is a refusal",
			params:  []*Param{{Name: "self"}, {Name: "a"}, {Name: "b", Default: i(2)}},
			args:    []Expr{i(1), i(2), &KeywordArg{Name: "b", Value: i(3)}},
			want:    nil,
			refuses: `multiple values for argument "b"`,
		},
		{
			name:    "a positional after a keyword is a refusal",
			params:  []*Param{{Name: "self"}, {Name: "a"}, {Name: "b"}},
			args:    []Expr{&KeywordArg{Name: "a", Value: i(1)}, i(2)},
			want:    nil,
			refuses: "positional argument after keyword argument",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			g := &irGen{}
			fd := &FuncDef{Name: "m", Params: tc.params}
			b := &strings.Builder{}
			binder := g.newCallArgBinder(b)
			got, err := binder.methodCallArgs(tc.args, fd, "C.m")
			if tc.refuses != "" {
				if err == nil {
					t.Fatalf("the vector read %v where the shape is a refusal (%s) — a call this road cannot lay out is a refusal, not a zero", got, tc.refuses)
				}
				if !strings.Contains(err.Error(), tc.refuses) {
					t.Fatalf("refused with %q, want a sentence containing %q", err.Error(), tc.refuses)
				}
				return
			}
			if err != nil {
				t.Fatalf("the road refused a call it can lay out: %v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("the vector is %v, want %d words — one per declared parameter, in declaration order", got, len(tc.want))
			}
			for i := range got {
				if tc.want[i] == nil {
					// The slot's word must be the register the parameter's OWN default emitted.
					def := tc.params[i+1].Default
					if def == nil {
						t.Fatalf("parameter %d is pinned as defaulted and has no default expression", i+1)
					}
					if binder.memo[def] == "" || got[i] != binder.memo[def] {
						t.Fatalf("parameter %d arrived in %q, want the register %s's default emitted (%q)",
							i+1, got[i], tc.params[i+1].Name, binder.memo[def])
					}
					continue
				}
				if binder.memo[tc.want[i]] == "" || got[i] != binder.memo[tc.want[i]] {
					t.Fatalf("parameter %d arrived in %q, want the register written for %v (%q)",
						i+1, got[i], tc.want[i], binder.memo[tc.want[i]])
				}
			}
		})
	}
}

// TestWhatAMethodCallStillOwesIsFiledNotFixed pins the shapes this cycle measured and did NOT fix, each
// with today's compiled answer beside the reference's. They share one cause, and it is not the vector:
// once the vector fills a slot from a DEFAULT, the doors that read a parameter's KIND — the text census
// (`strParamOf`, ADR 0366), the verdict census (`boolParamOf`), the heap-container registration
// (`declareHeapParams`) — are still keyed on the arguments the call WROTE, so a parameter nobody wrote
// arrives with the right word and the wrong kind. That is roadmap Gap R.479.
func TestWhatAMethodCallStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, r := range []struct {
		name, src, today, reference, row string
	}{
		{
			// The body never assigned `who`, the call never wrote it, and the census only ever saw a
			// written argument: the interpolation field reads the @str_tab POSITION.
			"a defaulted text parameter prints the intern position",
			"class C():\n    def greet(self, who=\"world\"):\n        print(f\"hi {who}\")\nC().greet()\n",
			"hi 0\n", "hi world\n", "Gap R.479",
		},
		{
			// The crispest form of the reading: the SAME source at the module door prints the text (the
			// `def` road's census saw the argument the call wrote), and at the method door it prints the
			// payload word.
			"a defaulted text parameter returned by the method prints the payload word",
			"class C():\n    def m(self, s=\"hi\"):\n        return s\nprint(C().m())\n",
			"0\n", "hi\n", "Gap R.479",
		},
		{
			// The same reading, at the door that refuses rather than prints: `+` asks whether either
			// side is a text, and the parameter is not registered as one.
			"a defaulted text parameter is not a text to `+`",
			"class C():\n    def greet(self, name=\"world\"):\n        return \"hi \" + name\nprint(C().greet())\n",
			"", "hi world\n", "Gap R.479",
		},
		{
			// A defaulted container reaches the body as a heap handle the loop roads were never told
			// about, so `len` asks its question of a word it cannot classify.
			"a defaulted container parameter is not a container to `len`",
			"class C():\n    def m(self, xs=[1, 2, 3]):\n        return len(xs)\nprint(C().m())\n",
			"", "3\n", "Gap R.479",
		},
		{
			// A float default arrives as one word, which is the method's missing double return word
			// (Gap R.35 read from the class side, L11.6, ADR 0196) rather than a vector hole: the
			// written-argument spelling `C().m(2.5)` answers `4` for the same reason.
			"a defaulted float parameter is one word, not a double",
			"class C():\n    def m(self, x=2.5):\n        return x * 2\nprint(C().m())\n",
			"4\n", "5.0\n", "Gap R.35",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, r.src)
			if r.today == "" {
				if !refused {
					t.Fatalf("%s: the compiled leg printed %q where it used to refuse in words — if it now answers, the row is paid: move it to the answered table and update roadmap %s", r.name, out, r.row)
				}
				if strings.Contains(out, "llc") || strings.Contains(out, "verify") {
					t.Fatalf("%s: a refusal that reached the VERIFIER is exit 2, our bug, not the program's: %s", r.name, out)
				}
				return
			}
			if refused {
				t.Fatalf("%s: the compiled leg refused a program the reference answers (%s)", r.name, out)
			}
			if out == r.reference {
				t.Fatalf("%s has been PAID — the compiled leg prints %q, the reference's answer. Move the row from this table to the answered ones and update roadmap %s", r.name, out, r.row)
			}
			if out != r.today {
				t.Fatalf("%s: the compiled leg changed shape (printed %q, pinned %q) — re-measure it and say which row owns it now (roadmap %s)", r.name, out, r.today, r.row)
			}
		})
	}
}

// TestAMethodCallRefusesTheShapeItCannotLayOut is the refusal half, judged on the exit code AND the
// sentence, with the reference's exit code beside it: every one of these is a program CPython refuses too
// (a TypeError at the call, or at the `def`), so the two legs disagree only about who says the words.
// A compiled answer of 0 here would be the worst outcome the ladder ranks — a wrong number, at rest.
func TestAMethodCallRefusesTheShapeItCannotLayOut(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"one argument past the declared list",
			"class C():\n    def m(self, a):\n        return a\nprint(C().m(1, 2, 3))\n",
			"too many arguments for C.m",
		},
		{
			"a required parameter the call left empty",
			"class C():\n    def m(self, a, b):\n        return a + b\nprint(C().m(1))\n",
			`missing argument "b" for C.m`,
		},
		{
			"a keyword no parameter answers to",
			"class C():\n    def m(self, a):\n        return a\nprint(C().m(k=1))\n",
			`unknown keyword argument "k" for C.m`,
		},
		{
			"a constructor's required parameter, left empty",
			"class C():\n    def __init__(self, v):\n        self.v = \"hi\"\n\n    def get(self):\n        return self.v\n\n\nprint(C().get())\n",
			`missing argument "v" for C.__init__`,
		},
		{
			"the same parameter written by position and by name",
			"class C():\n    def m(self, a, b=2):\n        return a + b\nprint(C().m(1, 2, b=3))\n",
			`multiple values for argument "b" of C.m`,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if !refused {
				t.Fatalf("the compiled leg ANSWERS a program the reference refuses, printing %q — a wrong number at exit 0 is the outcome the ladder ranks worst, and this row is a refusal", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("the refusal does not say %q — every refusal here is an exit 1 that names the shape and the method: %s", tc.want, out)
			}
			if strings.Contains(out, "llc") || strings.Contains(out, "verify") {
				t.Fatalf("a refusal that reached the VERIFIER is exit 2, our bug, not the program's: %s", out)
			}
			if _, ok := pyOut(t, tc.src); ok {
				t.Fatalf("the reference ANSWERS a program this backend refuses — the refusal would then be a regression, not a refusal: %s", tc.src)
			}
		})
	}
}

// methodArgAnsweredSourcesForTest is the harness's spigot: the family table is written once, and the
// record-recording tool and the case that audits that every row is on the record both read the same list
// rather than a copy an editor could forget.
func methodArgAnsweredSourcesForTest() []string {
	var out []string
	for _, tc := range methodArgAnsweredRowsForTest() {
		out = append(out, tc.src)
	}
	return out
}
