package integration

// ADR 0434 · roadmap Gap R.472 — the reference leg of the METHOD CALL'S ARGUMENT VECTOR.
//
// One question, four doors: a call to a method must hand the callee one word per parameter the METHOD
// declared, in declaration order, filling each argument the call left empty from that parameter's own
// default expression and binding each `name=` by name. The roads that emit a method call — the static
// receiver dispatch, `super()`, the run-time class-id switch, and `C(...)`'s constructor call in
// codegen_instance.go — each built their operand list with `for i, arg := range c.Args`, so:
//
//	class C():
//	    def m(self, a, b=2):
//	        return b
//	print(C().m(5))          # the reference prints 2; this backend printed 0
//
//	class D():
//	    def m(self, a=1, b=2):
//	        return a * 10 + b
//	print(D().m(b=7, a=3))   # the reference prints 37; this backend printed 73
//
// — wrong numbers, at the exit code of success, in modules `llc` and `opt -passes=verify` both accept,
// because an under-filled operand list is not malformed IR. The module-`def` road had done the walk over
// the declared parameters since **ADR 0166**; the method roads are separate roads and had never been asked.
//
// Every answered row runs on the shipped CLI engines, prints a live CPython's bytes, and is on the record
// (`testdata/interpreter-golden.json`), so an answer is certified by neither the backend that earned it nor
// a test the author could delete (**ADR 0302**). The refusals are pinned at the exit code the reference's
// own `TypeError` exits with; the owed halves pin this backend's bytes beside the reference's so that the
// day a door opens, the pin fails rather than quietly passing.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// methodArgFamily is the answered half. `want` is both the bytes a live CPython prints (checked in every
// case) and the bytes this compiler must print.
func methodArgFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The row's own program: the body reads the parameter the call left out.
		{"an omitted default answers its own value", "class C():\n    def m(self, a, b=2):\n        return b\nc = C()\nprint(c.m(5))\n", "2\n"},
		{"defaults on either side of a written argument", "class C():\n    def m(self, a, b=10, c=100):\n        return a + b + c\nprint(C().m(1))\n", "111\n"},
		{"a default filled in the middle of the list", "class C():\n    def m(self, a, b=10, c=100):\n        return a + b + c\nprint(C().m(1, 20))\n", "121\n"},
		// The keyword half, and the shape that made the row loud: the two values travelled in the order the
		// source wrote them, so the digits arrived swapped.
		{"keyword arguments bind by name, in any order", "class C():\n    def m(self, a=1, b=2):\n        return a * 10 + b\nprint(C().m(b=7, a=3))\n", "37\n"},
		{"one keyword argument and one default", "class C():\n    def m(self, a=1, b=2):\n        return a * 10 + b\nprint(C().m(a=3))\n", "32\n"},
		{"every argument spelled by name", "class C():\n    def m(self, a=1, b=2):\n        return a * 10 + b\nprint(C().m(b=7, a=3))\n", "37\n"},
		// The constructor road, a fifth `for i, arg := range c.Args`.
		{"a constructor's omitted default", "class C():\n    def __init__(self, a, b=2):\n        self.b = b\n        self.a = a\nc = C(5)\nprint(c.a)\nprint(c.b)\n", "5\n2\n"},
		// The defaults come from the class that DECLARES the method, read by the same base walk that
		// resolved the symbol.
		{"an inherited method's default", "class B():\n    def m(self, a, b=3):\n        return a + b\nclass C(B):\n    pass\nprint(C().m(1))\n", "4\n"},
		{"an override brings its own default", "class B():\n    def m(self, a, b=3):\n        return a + b\nclass C(B):\n    def m(self, a, b=4):\n        return a + b\nprint(B().m(1))\nprint(C().m(1))\n", "4\n5\n"},
		// The run-time class-id switch, and the proof the vector is built per arm: the two arms' defaults
		// differ, and each prints its own.
		{"a dynamically dispatched method's defaults, per arm", "class B():\n    def m(self, a, b=3):\n        return a + b\nclass C(B):\n    def m(self, a, b=5):\n        return a + b\ndef pick(n):\n    if n == 0:\n        return B()\n    return C()\nx = pick(0)\nprint(x.m(1))\nx = pick(1)\nprint(x.m(1))\n", "4\n6\n"},
		// The `super()` road reads the BASE's parameters; the enclosing class's own default is the caller's
		// and is not passed on.
		{"super() reaches the base method's default", "class B():\n    def m(self, a, b=3):\n        return a + b\nclass C(B):\n    def m(self, a, b=4):\n        return super().m(a) * 10\nprint(C().m(1))\n", "40\n"},
		// The verdict half: an omitted default is still a verdict, and `return flag` travels the bool road.
		{"an omitted verdict default", "class C():\n    def m(self, flag=True):\n        return flag\nprint(C().m())\nprint(C().m(False))\n", "True\nFalse\n"},
		// The generator-method twin the previous cycle filed: the buffer's slots are filled from the same
		// vector any other method's parameters are.
		{"a generator method's omitted default yields the default", "class C():\n    def gen(self, a, b=3):\n        yield a\n        yield b\nc = C()\nfor v in c.gen(7):\n    print(v)\n", "7\n3\n"},
		{"a generator method's written arguments still travel", "class C():\n    def gen(self, a, b=3):\n        yield a\n        yield b\nc = C()\nfor v in c.gen(7, 8):\n    print(v)\n", "7\n8\n"},
		{"a defaulted buffer summed", "class C():\n    def gen(self, a, b=3):\n        yield a\n        yield b\nc = C()\nt = 0\nfor v in c.gen(7):\n    t = t + v\nprint(t)\n", "10\n"},
	}
}

// TestAMethodCallAnswersLikeTheReference is the reference leg: the live interpreter is asked in the same
// case, and the CLI must print its bytes on the engine that ships (**ADR 0302**).
func TestAMethodCallAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range methodArgFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "method_args.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q (prints %q) — this row's premise went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: the module reached the assembler broken (**ADR 0166**): %s\n%s", engine, stderr, tc.src)
				}
				if code != 0 {
					t.Fatalf("%s: exit %d where the reference prints %q: %s\n%s", engine, code, want, stderr, tc.src)
				}
				if stdout != want {
					t.Fatalf("%s: printed %q, the reference printed %q\n%s", engine, stdout, want, tc.src)
				}
			}
		})
	}
}

// TestAMethodCallFamilyIsOnTheRecord keeps the coverage auditable: an answer this row earned may not be
// certified by the backend that earned it, and may not be deleted by deleting a test (**ADR 0302**).
func TestAMethodCallFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	var missing []string
	for _, tc := range methodArgFamily() {
		if !lang.HasGoldenAnswer(tc.src) {
			missing = append(missing, tc.src)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("%d program(s) of this family are not on the record leg — record them from the reference with "+
			"tools/recmerge (the compiled leg is cross-checked, never the author):\n%s", len(missing), strings.Join(missing, "\n---\n"))
	}
}

// TestAMethodCallRefusesAtTheExitCodeTheReferenceDoes is the refusal half: every one of these is a program
// the reference refuses too (a `TypeError` at the call), so the two legs disagree only about who says the
// words. The contract being pinned is that this backend says them at exit 1 — a front-end refusal in words —
// and never as a wrong answer at exit 0 and never as an assembler rejection at exit 2, which would be this
// compiler's bug rather than the program's (**ADR 0166**, **ADR 0029**).
func TestAMethodCallRefusesAtTheExitCodeTheReferenceDoes(t *testing.T) {
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
		{
			"a positional written after a keyword",
			"class C():\n    def m(self, a, b):\n        return a + b\nprint(C().m(a=1, 2))\n",
			"positional argument after keyword argument for C.m",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "method_args_refusal.gy", tc.src)
			_, _, refCode := cliStreams(t, cliEngines[0], path)
			if refCode == 0 {
				t.Fatalf("the reference ANSWERS a program this backend refuses — the row is a regression, not a refusal\n%s", tc.src)
			}
			for _, engine := range cliEngines {
				_, stderr, code := cliStreams(t, engine, path)
				if code != 1 {
					t.Fatalf("%s: exited %d, want the front-end refusal's 1 (the reference exits %d with its own TypeError)\n%s\n%s",
						engine, code, refCode, stderr, tc.src)
				}
				if !strings.Contains(stderr, tc.want) {
					t.Fatalf("%s: the refusal does not say %q — it must name the shape, the method and the parameter: %s\n%s",
						engine, tc.want, stderr, tc.src)
				}
				if strings.Contains(stderr, "llc") || strings.Contains(stderr, "verify") {
					t.Fatalf("%s: a refusal that reached the VERIFIER is exit 2, our bug, not the program's: %s", engine, stderr)
				}
			}
		})
	}
}

// TestTheDefaultedContainerArgumentIsStillTheAssemblerRejectionItsParentWas guards the door that is not
// this row's. A container DEFAULT the fold turned into a module global travels to the callee as the global,
// where an i32 operand belongs, and the assembler rejects the module — the same rejection **Gap R.474**
// records for a container literal in a method's operand list, reached a second way now that a default's
// expression goes through the same operand road a written argument does. Pinned at exit 2 so the next cycle
// finds the row rather than the surprise.
func TestTheDefaultedContainerArgumentIsStillTheAssemblerRejectionItsParentWas(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src string }{
		{"a defaulted list the method returns", "class C():\n    def m(self, xs=[1, 2, 3]):\n        return xs\nprint(C().m())\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "method_args_defaulted_container.gy", r.src)
			_, stderr, code := cliStreams(t, cliEngines[0], path)
			if code != 2 {
				t.Fatalf("%s (**Gap R.474**): exited %d, want the assembler rejection's 2 — the day this changes, "+
					"the row that owns a container call argument was landed and this pin must follow it\n%s", r.name, code, r.src)
			}
			if !strings.Contains(stderr, "global variable reference must have pointer type") &&
				!strings.Contains(stderr, "expected instruction opcode") {
				t.Fatalf("%s (**Gap R.474**): rejected with %q, which is not the sentence this pin holds\n%s", r.name, stderr, r.src)
			}
		})
	}
}

// TestWhatTheMethodCallStillOwesAtTheCLI pins the CLI halves of what this cycle measured and did not fix.
// They share one cause that is not the vector: once the vector fills a slot from a DEFAULT, the doors that
// read a parameter's KIND — the text census (**ADR 0366**), the heap-container registration — are still
// keyed on the arguments the call WROTE, so a parameter nobody wrote arrives with the right word and the
// wrong kind. Roadmap **Gap R.479**.
func TestWhatTheMethodCallStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, gap, want, today string }{
		{
			name:  "a defaulted text parameter prints the intern position",
			src:   "class C():\n    def greet(self, who=\"world\"):\n        print(f\"hi {who}\")\nC().greet()\n",
			gap:   "Gap R.479",
			want:  "hi world\n",
			today: "hi 0\n",
		},
		{
			name:  "a defaulted text parameter returned by the method prints the payload word",
			src:   "class C():\n    def m(self, s=\"hi\"):\n        return s\nprint(C().m())\n",
			gap:   "Gap R.479",
			want:  "hi\n",
			today: "0\n",
		},
		{
			name:  "a defaulted text parameter is not a text to `+`",
			src:   "class C():\n    def greet(self, name=\"world\"):\n        return \"hi \" + name\nprint(C().greet())\n",
			gap:   "Gap R.479",
			want:  "hi world\n",
			today: "",
		},
		{
			name:  "a defaulted container parameter is not a container to `len`",
			src:   "class C():\n    def m(self, xs=[1, 2, 3]):\n        return len(xs)\nprint(C().m())\n",
			gap:   "Gap R.479",
			want:  "3\n",
			today: "",
		},
		{
			name:  "a defaulted float parameter is one word, not a double",
			src:   "class C():\n    def m(self, x=2.5):\n        return x * 2\nprint(C().m())\n",
			gap:   "Gap R.35",
			want:  "5.0\n",
			today: "4\n",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "method_args_owed.gy", r.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != r.want {
				t.Fatalf("%s (%s): the reference no longer prints %q (prints %q) — re-measure the row\n%s",
					r.name, r.gap, r.want, want, r.src)
			}
			stdout, stderr, code := cliStreams(t, cliEngines[0], path)
			if r.today == "" {
				if code != 1 {
					t.Fatalf("%s (%s): exited %d with %q, want the refusal's 1 — the day this answers, the row is paid: "+
						"move it to the answered family\n%s", r.name, r.gap, code, stderr, r.src)
				}
				return
			}
			if code != 0 {
				t.Fatalf("%s (%s): exited %d with %q, want today's %q at exit 0", r.name, r.gap, code, stderr, r.today)
			}
			if stdout == r.want {
				t.Fatalf("%s has been PAID (%s) — the CLI prints the reference's %q. Move the row to the answered family "+
					"and update roadmap %s", r.name, r.gap, r.want, r.gap)
			}
			if stdout != r.today {
				t.Fatalf("%s: the CLI changed shape (prints %q, pinned %q) — re-measure it and say which row owns it now "+
					"(roadmap %s)\n%s", r.name, stdout, r.today, r.gap, r.src)
			}
		})
	}
}

// TestTheMethodArgCorpusProgramIsTheRowItWasFiledWith checks the matrix program is the program this row was
// filed with, that the compiled leg and CPython still agree on it line for line, and that the matrix carries
// it as an asserted row that matches — the machine path for a claim about a program has to be readable by a
// machine (**ADR 0302**'s two legs, **ADR 0030**'s artifact contract).
func TestTheMethodArgCorpusProgramIsTheRowItWasFiledWith(t *testing.T) {
	t.Parallel()
	src := readProgram(t, "method_arg_defaults.gy")
	want, ok := cpythonOut(t, writeSrc(t, t.TempDir(), "method_arg_defaults.py", src))
	if !ok {
		t.Skip("no live python3 to ask")
	}
	for _, engine := range cliEngines {
		path := writeSrc(t, t.TempDir(), "method_arg_defaults.gy", src)
		stdout, stderr, code := cliStreams(t, engine, path)
		if code != 0 {
			t.Fatalf("%s: exited %d: %s", engine, code, stderr)
		}
		if stdout != want {
			t.Fatalf("%s: printed:\n%s\nthe reference printed:\n%s", engine, stdout, want)
		}
	}
	m := conformanceMatrix(t)
	for _, r := range m.Results {
		if r.Case.ID == "programs/method_arg_defaults" {
			if !r.Case.Asserted {
				t.Fatal("the matrix carries programs/method_arg_defaults as an unasserted probe: this row's program is surface")
			}
			if !r.Conformant {
				t.Fatalf("the matrix says this program's compiled leg does not match the reference: %s", r.AOTErr)
			}
			if r.Oracle != "match" {
				t.Fatalf("the matrix's oracle verdict for this program is %q, want `match`", r.Oracle)
			}
			return
		}
	}
	t.Fatal("the conformance matrix does not carry programs/method_arg_defaults")
}
