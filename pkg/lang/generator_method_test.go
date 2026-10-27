// pkg/lang/generator_method_test.go — ADR 0432 · roadmap Gap R.455: a generator METHOD gets the reading its
// own body asks for, and the doors that receive its answer ask the same question the class's door asked.
//
// A generator is a body, not a place. The backend has always known how to lower one — allocate a buffer in
// the callee's frame, root it, append each `yield` into it, hand the buffer back as the answer (ADR 0140, and
// the eager-to-a-list model ADR 0081 leaves on the record for a module `def`) — and the whole-program scan
// that decides whether a body is such a thing walked `prog.Stmts`:
//
//	class C():
//	    def m(self):
//	        yield 1
//
//	c = C()
//	print(c.m() is None)   # the reference: False   this backend, before: exit 1 — `yield outside a generator function`
//
// A `def` written inside a `ClassDef` body is a method by POSITION, and the statement walk never stepped into
// the class to read what its body does: `containsYield` was consulted by `funcDef`, `registerClass` never
// consulted it at all, and `stmtYield` — the one door that turns a `yield` into an `rt_append` — found no
// buffer because the emitter of a method body never opened one. The refusal then blamed the program for the
// position its body was written in, which is the shape of sentence **ADR 0166** refuses: the compiler's own
// walk, described as the program's mistake.
//
// The reading is now taken at the class's door, where every other per-method fact is taken (`registerClass`'s
// walk, beside `strFuncs` and the answer round), under the MANGLED symbol `resolveMethod` hands a call site
// (**ADR 0224**'s rule for a method-keyed table). One reading, then, asked at four doors:
//
//   - `emitClassMethod` opens the buffer and answers it at both exits — the ordinary one and the unwind;
//   - the call roads — static dispatch, `super()`, and the class-id `switch` — label the register they leave
//     the answer in, which is what the print door and the membership test ask;
//   - the binding road claims the name as a list, which is what a later `for`, `print` or `in` asks;
//   - the `for` road walks the buffer instead of falling through to the numeric-range path that counts to the
//     handle it was handed and prints the counter (an answer the program never wrote, at the exit code of
//     success — the artifact this contract ranks worst).
//
// One shape the dynamic door cannot answer is REFUSED in words rather than answered: when the receiver's class
// is a run-time fact and the classes that declare the method do not agree about yielding, the joined register
// is a buffer on one arm and a plain value on another. That is **Gap R.467**'s row, and the refusal is
// registered as `lower.unsupported.split-generator-method-dispatch` (**L11.8**, **ADR 0346**) so an agent
// branches on a code instead of on prose.
//
// Rows are judged on BOTH witness legs (**ADR 0302**): this compiler's module, and the answer
// `testdata/interpreter-golden.json` holds for that exact source, where a MISSING record fails the case. The
// reference leg — a live CPython beside the linked compiler — is `integration/generator_method_test.go`. The
// eager model is not this row's invention: `print(g())` for a module generator answers `[10, 15]` on the
// record, and `docs/language.md` states the model; the rows below are therefore the shapes where the eager
// buffer and the reference's iterator answer agree, and the ones where they do not are named in the
// never-worse table rather than quietly compared.

package lang

import (
	"strings"
	"testing"
)

// generatorMethodBases are the modules the family is written against.
const (
	gmOne   = "class C():\n    def m(self):\n        yield 1\nc = C()\n"
	gmTwo   = "class C():\n    def m(self):\n        yield 1\n        yield 2\nc = C()\n"
	gmThree = "class C():\n    def m(self):\n        yield 1\n        yield 2\n        yield 3\nc = C()\n"
	// gmArgs yields from a loop, so the buffer is filled by a body that computes.
	gmArgs = "class C():\n    def m(self, n):\n        for i in range(n):\n            yield i * 2\nc = C()\n"
	// gmEmptyBody yields on one arm only: the other arm's buffer is empty, and an empty buffer is the one
	// answer the eager model and the reference disagree about for TRUTH (a reference generator object is
	// always truthy), so it is walked, never asked for its truth.
	gmEmptyBody = "class C():\n    def m(self, k):\n        if k:\n            yield 1\nc = C()\n"
	// gmTwin is the module `def` the method family is measured against: the same buffer, filled by the same
	// body, one scope further out.
	gmTwin = "def g():\n    yield 1\n    yield 2\n"
)

// generatorMethodFamily is the answered half. `want` is what a live CPython prints for the source AND what
// this compiler must print; the reference leg re-reads every one of them in
// `integration/generator_method_test.go` rather than trusting this file.
func generatorMethodFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The row's own program, and the identity question it was filed with.
		{"the row's program", gmOne + "print(c.m() is None)\n", "False\n"},
		{"the negated identity", gmOne + "print(c.m() is not None)\n", "True\n"},
		// The two doors the row's Definition of Done names: the walk, and the same answer through a binding.
		{"the walk", gmTwo + "for v in c.m():\n    print(v)\n", "1\n2\n"},
		{"bound, then walked", gmTwo + "ys = c.m()\nfor v in ys:\n    print(v)\n", "1\n2\n"},
		{"bound, and asked whether it is the void", gmTwo + "ys = c.m()\nprint(ys is None)\nfor v in ys:\n    print(v)\n", "False\n1\n2\n"},
		{"walked and summed", gmThree + "s = 0\nfor v in c.m():\n    s = s + v\nprint(s)\n", "6\n"},
		// The doors that read a container by asking it a question.
		{"membership, hit and miss", gmTwo + "print(1 in c.m())\nprint(9 in c.m())\n", "True\nFalse\n"},
		{"the truth of a buffer that holds something", gmTwo + "print(bool(c.m()))\n", "True\n"},
		{"the truth asked as a condition", gmTwo + "if c.m():\n    print(\"yes\")\nelse:\n    print(\"no\")\n", "yes\n"},
		{"two calls are two buffers", gmTwo + "print(c.m() == c.m())\n", "False\n"},
		{"a comprehension over the bound answer", gmTwo + "ys = c.m()\nprint([v for v in ys])\n", "[1, 2]\n"},
		// The body is a body: everything a module generator's body may contain, a method's body may too.
		{"a loop in the body", gmArgs + "for v in c.m(3):\n    print(v)\n", "0\n2\n4\n"},
		{"both arms of a condition", "class C():\n    def m(self, flag):\n        if flag:\n            yield 1\n        else:\n            yield 2\nc = C()\nfor v in c.m(True):\n    print(v)\nfor v in c.m(False):\n    print(v)\n", "1\n2\n"},
		{"a while loop in the body", "class C():\n    def m(self):\n        i = 0\n        while i < 3:\n            yield i\n            i = i + 1\nc = C()\nt = 0\nfor v in c.m():\n    t = t + v\nprint(t)\n", "3\n"},
		{"the body's own effect on a field", "class C():\n    def __init__(self):\n        self.n = 0\n    def m(self):\n        self.n = self.n + 1\n        yield self.n\n        self.n = self.n + 1\n        yield self.n\nc = C()\nfor v in c.m():\n    print(v)\nprint(c.n)\n", "1\n2\n2\n"},
		{"a yield inside match", "class C():\n    def m(self, n):\n        match n:\n            case 1:\n                yield 10\n            case _:\n                yield 20\nc = C()\nfor v in c.m(1):\n    print(v)\nfor v in c.m(5):\n    print(v)\n", "10\n20\n"},
		// The reading and the lowering are two questions: a body whose `yield` lives in a handler is READ as a
		// generator when any other statement yields, and the handler's `yield` appends like any other (the body
		// whose ONLY yield is in a handler is **Gap R.470**'s owed row, refused by `containsYield`).
		{"a yield inside an except handler", "class C():\n    def m(self):\n        yield 1\n        try:\n            x = 1 // 0\n        except ZeroDivisionError:\n            yield 2\nc = C()\nfor v in c.m():\n    print(v)\n", "1\n2\n"},
		{"an empty buffer walks nothing", gmEmptyBody + "for v in c.m(0):\n    print(v)\nprint(\"done\")\n", "done\n"},
		// The four kinds the untagged word could not carry — **Gap R.468**'s program, which stands in the
		// answered family now that the yield writes payload AND tag together (**ADR 0433**).
		{"a yielded text, integer, void and float keep their kinds", "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\n        yield 1.5\nc = C()\nfor v in c.m():\n    print(v)\n", "a\n2\nNone\n1.5\n"},
		// Call-site shapes: defaults, keywords, and two calls of one body.
		{"defaulted and keyword arguments", "class C():\n    def gen(self, a, b=3):\n        yield a\n        yield b\nc = C()\nfor v in c.gen(7, 8):\n    print(v)\n", "7\n8\n"},
		{"two bindings are two buffers", gmTwo + "a = c.m()\nb = c.m()\nfor v in a:\n    print(v)\nfor v in b:\n    print(v)\n", "1\n2\n1\n2\n"},
		{"a parameterised body called from a loop", "class C():\n    def m(self, k):\n        yield k\n        yield k + 1\nc = C()\nt = 0\nfor k in range(3):\n    for v in c.m(k):\n        t = t + v\nprint(t)\n", "9\n"},
		// The class doors: `self.`, `super()`, and a base the subclass never re-declares.
		{"a method calling its own class's generator", "class C():\n    def m(self):\n        yield 1\n        yield 2\n    def n(self):\n        for v in self.m():\n            print(v)\n        return None\nc = C()\nprint(c.n())\n", "1\n2\nNone\n"},
		{"super() reaching a base's generator", "class Base():\n    def gen(self):\n        yield 1\n        yield 2\nclass Child(Base):\n    def drain(self):\n        for v in super().gen():\n            print(v)\n        return None\nc = Child()\nprint(c.drain())\n", "1\n2\nNone\n"},
		{"an inherited generator", "class B():\n    def m(self):\n        yield 7\n        yield 8\nclass D(B):\n    pass\nd = D()\nfor v in d.m():\n    print(v)\n", "7\n8\n"},
		{"yield from a method's own answer", "class C():\n    def src(self):\n        return [1, 2, 3]\n    def m(self):\n        yield from self.src()\nc = C()\nfor v in c.m():\n    print(v)\n", "1\n2\n3\n"},
		// The dynamic door, answered where every class that declares the name yields.
		{"dynamically dispatched, every arm a generator", "class B():\n    def m(self):\n        yield 1\n        yield 2\nclass D(B):\n    def m(self):\n        yield 3\n        yield 4\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(D())\n", "3\n4\n"},
	}
}

// TestAGeneratorMethodAnswersTheBufferItsBodyFilled is the answered family on BOTH legs: the compiled answer
// through the LLVM JIT, and the answer the record holds for the same bytes of source.
func TestAGeneratorMethodAnswersTheBufferItsBodyFilled(t *testing.T) {
	t.Parallel()
	for _, tc := range generatorMethodFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("%s: %v\n%s", tc.name, err, tc.src)
			}
			if res.Code != 0 {
				t.Fatalf("%s: exited %d with stderr %q, want the reference's bytes at exit 0\n%s", tc.name, res.Code, res.Stderr, tc.src)
			}
			if res.Output != tc.want {
				t.Fatalf("%s: printed %q, want %q\n%s", tc.name, res.Output, tc.want, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("%s: the record leg refuses this source: %v", tc.name, err)
			}
			if recorded != tc.want {
				t.Fatalf("%s: the record holds %q, the compiled leg printed %q — one answer, two witnesses\n%s",
					tc.name, recorded, res.Output, tc.src)
			}
		})
	}
}

// TestTheGeneratorReadingIsTheBodiesOwnQuestion asks the gate as a function, over bodies chosen so a wrong
// reading shows up as a wrong verdict rather than as a passing program: a body that yields only inside an
// `if`, a `while`, a `for`, a `match` arm or after an unconditional `return` is a generator all the same, and
// a body that merely CONTAINS a nested `def` whose body yields is the reading the module road has always
// taken (parity with `funcDef`, pinned rather than improved here — see **Gap R.457** for the nested body).
func TestTheGeneratorReadingIsTheBodiesOwnQuestion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want bool
	}{
		{"a plain body is not a generator", "class C():\n    def m(self):\n        return 1\n", false},
		{"a body that falls through is not", "class C():\n    def m(self):\n        print(1)\n", false},
		{"a bare yield is", "class C():\n    def m(self):\n        yield 1\n", true},
		{"a yield inside if is", "class C():\n    def m(self, k):\n        if k:\n            yield 1\n", true},
		{"a yield inside while is", "class C():\n    def m(self):\n        while k:\n            yield 1\n", true},
		{"a yield inside for is", "class C():\n    def m(self):\n        for i in range(3):\n            yield i\n", true},
		{"a yield inside a match arm is", "class C():\n    def m(self, n):\n        match n:\n            case 1:\n                yield 1\n", true},
		{"a yield after a return is still a generator", "class C():\n    def m(self):\n        return 1\n        yield 2\n", true},
		{"a yield from is", "class C():\n    def m(self):\n        yield from [1, 2]\n", true},
		{"an async def is the checker's, not this reading's", "class C():\n    async def m(self):\n        yield 1\n", false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("%s: %v\n%s", tc.name, err, tc.src)
			}
			var fd *FuncDef
			for _, st := range prog.Stmts {
				if cd, ok := st.(*ClassDef); ok {
					for _, bs := range cd.Body {
						if f, ok2 := bs.(*FuncDef); ok2 {
							fd = f
						}
					}
				}
			}
			if fd == nil {
				t.Fatalf("%s: no method body to read\n%s", tc.name, tc.src)
			}
			if got := methodIsGenerator(fd); got != tc.want {
				t.Fatalf("%s: methodIsGenerator answered %v, want %v\n%s", tc.name, got, tc.want, tc.src)
			}
			// The same question the module door asks, asked of the same body: one reading, two doors
			// (**ADR 0232**'s rule about a fact with two answers).
			if got := containsYield(fd.Body); got != methodIsGenerator(fd) && !fd.Async {
				t.Fatalf("%s: the class door and the `def` door disagree: %v vs %v\n%s", tc.name, got, methodIsGenerator(fd), tc.src)
			}
		})
	}
}

// TestTheGeneratorMethodModuleCarriesTheBufferDoors reads the machine path: the emitted module has to show the
// buffer being allocated, rooted, appended to and answered — and a method that does NOT yield must show none
// of it, which is the negative control that keeps this from being a door every method body walks through.
func TestTheGeneratorMethodModuleCarriesTheBufferDoors(t *testing.T) {
	t.Parallel()
	res, err := Compile(gmTwo + "for v in c.m():\n    print(v)\n")
	if err != nil {
		t.Fatalf("the row's program refused: %v\n%s", err, gmTwo)
	}
	body := generatorMethodDefine(t, res.IR, "gy_C_m")
	for _, want := range []string{
		"%gh1 = call i32 @rt_alloc(i32 1)",              // the buffer, in the callee's frame
		"call void @rt_append_tagged(i32 %gh1, i32 1, ", // each yield appends its value AND its kind
		"call void @rt_append_tagged(i32 %gh1, i32 2, ", // — one operation, never two (ADR 0187)
		"ret i32 %gh1", // and the answer IS the buffer
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the emitted method does not carry %q:\n%s", want, body)
		}
	}
	if strings.Count(body, "ret i32 %gh1") != 2 {
		t.Fatalf("the buffer must be answered at BOTH exits — the ordinary one and the unwind — and the define holds %d such returns:\n%s",
			strings.Count(body, "ret i32 %gh1"), body)
	}
	// The rooting that ADR 0140 learned the hard way: an unrooted handle is one collection away from a
	// stale slot, and the first body statement emits a safe point.
	if !strings.Contains(body, "store i32 %gh1, i32* %_") || !strings.Contains(body, "call void @rt_root_put(i32* %_") {
		t.Fatalf("the buffer is not rooted like the module road roots its generator's — the handle must be stored in a\n"+
			"frame slot and that slot rooted, or the first safe point collects it (ADR 0140, ADR 0181):\n%s", body)
	}

	// The negative control: a plain method body asks for none of this.
	plain, err := Compile("class C():\n    def m(self):\n        return 1\nc = C()\nprint(c.m())\n")
	if err != nil {
		t.Fatalf("a plain method refused: %v", err)
	}
	plainBody := generatorMethodDefine(t, plain.IR, "gy_C_m")
	for _, forbidden := range []string{"rt_alloc", "rt_append", "%gh"} {
		if strings.Contains(plainBody, forbidden) {
			t.Fatalf("a method that never yields carries %q in its define — the reading is the body's:\n%s", forbidden, plainBody)
		}
	}
}

// generatorMethodDefine cuts one `define` out of an emitted module, so an IR row asserts about the method and
// not about the runtime the module also carries.
func generatorMethodDefine(t *testing.T, ir, symbol string) string {
	t.Helper()
	marker := "define i32 @" + symbol + "("
	at := strings.Index(ir, marker)
	if at < 0 {
		t.Fatalf("the module carries no @%s:\n%s", symbol, ir)
	}
	rest := ir[at:]
	end := strings.Index(rest, "\n}\n")
	if end < 0 {
		t.Fatalf("the define for @%s never closes", symbol)
	}
	return rest[:end]
}

// TestTheGeneratorMethodRoadIsNeverWorseThanTheModuleTwin is the ladder this row is measured on: every shape,
// asked of a module `def` and of a method of a class, must answer the same bytes. A door that answers one of
// the two is a door with two answers (**ADR 0232**), and the pair that disagrees is what a reader would
// otherwise discover by shipping one of them.
func TestTheGeneratorMethodRoadIsNeverWorseThanTheModuleTwin(t *testing.T) {
	t.Parallel()
	pairs := []struct {
		name   string
		module string
		method string
	}{
		{"the walk", gmTwin + "for v in g():\n    print(v)\n", gmTwo + "for v in c.m():\n    print(v)\n"},
		{"bound then printed", gmTwin + "xs = g()\nprint(xs)\n", gmTwo + "xs = c.m()\nprint(xs)\n"},
		{"bound then walked", gmTwin + "xs = g()\nfor v in xs:\n    print(v)\n", gmTwo + "xs = c.m()\nfor v in xs:\n    print(v)\n"},
		{"membership", gmTwin + "print(1 in g())\n", gmTwo + "print(1 in c.m())\n"},
		{"identity against the void", gmTwin + "print(g() is None)\n", gmTwo + "print(c.m() is None)\n"},
		{"a comprehension over the binding", gmTwin + "xs = g()\nprint([v for v in xs])\n", gmTwo + "ys = c.m()\nprint([v for v in ys])\n"},
		{"printed whole (the eager model, both doors)", gmTwin + "print(g())\n", gmTwo + "print(c.m())\n"},
	}
	for _, p := range pairs {
		p := p
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()
			m, merr := runCompiledBytes(p.module)
			d, derr := runCompiledBytes(p.method)
			if (merr == nil) != (derr == nil) {
				t.Fatalf("%s: one door answered and the other refused — module %v / method %v", p.name, merr, derr)
			}
			if merr != nil {
				if !sameRefusalSentence(merr, derr) {
					t.Fatalf("%s: the two doors refuse with different sentences:\n  module: %v\n  method: %v", p.name, merr, derr)
				}
				return
			}
			if m != d {
				t.Fatalf("%s: the module door prints %q and the method door prints %q — one model, two answers\nmodule:\n%s\nmethod:\n%s",
					p.name, m, d, p.module, p.method)
			}
		})
	}
}

// runCompiledBytes runs one program through the JIT and returns its stdout, or the refusal.
func runCompiledBytes(src string) (string, error) {
	res, err := JIT(src, 0)
	if err != nil {
		return "", err
	}
	if res.Code != 0 {
		return "", &generatorMethodTrap{code: res.Code, stderr: res.Stderr, stdout: res.Output}
	}
	return res.Output, nil
}

// generatorMethodTrap lets a program that RAN but trapped be compared with a program that was refused, by the
// same never-worse ladder — both are "this door declined", and the ladder compares the sentences.
type generatorMethodTrap struct {
	code   int
	stderr string
	stdout string
}

func (t *generatorMethodTrap) Error() string { return t.stderr }

// sameRefusalSentence compares two refusals on the substance of what they refuse: the `codegen:` prefix and
// any roadmap citation are shared, so a pair that refuses the same shape passes even when one sentence names
// the container differently.
func sameRefusalSentence(a, b error) bool {
	if a == nil || b == nil {
		return a == b
	}
	fold := func(s string) string {
		s = strings.TrimPrefix(s, "jit: ")
		s = strings.TrimPrefix(s, "codegen: ")
		if at := strings.Index(s, "(roadmap"); at >= 0 {
			s = s[:at]
		}
		return strings.TrimSpace(s)
	}
	return fold(a.Error()) == fold(b.Error())
}

// TestWhatAGeneratorMethodStillCannotReachIsFiledNotFixed pins what this row leaves owed, with today's bytes
// beside the reference's. Every one is byte-identical on this row's parent, measured with a `git worktree`
// build of it — the sentence that makes them other rows' lesions rather than this commit's damage. An answer
// that arrives here FAILS the case, because a pin that starts passing is a row that was closed without
// deleting its pin.
func TestWhatAGeneratorMethodStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantOut    string
		wantStderr string
		dossier    string
	}{
		{
			// A `return` ends the body through the ordinary answer road, which hands back the void's word
			// rather than the buffer the yields already filled.
			name:     "a return after the yields answers the void, not the buffer",
			src:      "class C():\n    def m(self):\n        yield 1\n        return\nc = C()\nxs = c.m()\nfor v in xs:\n    print(v)\n",
			wantExit: 0,
			wantOut:  "",
			dossier:  "**Gap R.469** — the reference prints `1`; the module twin prints nothing either, and the road that owns it is `stmtReturn`'s answer beside a generator frame",
		},
		{
			// `containsYield` walks if/elif/else, while, for, match and a nested def; a handler is not in
			// the list, so a yield that lives only inside one is still refused — on BOTH doors.
			name:       "a yield inside a try body is still refused",
			src:        "class C():\n    def m(self):\n        try:\n            yield 1\n        except ValueError:\n            yield 2\nc = C()\nfor v in c.m():\n    print(v)\n",
			wantExit:   1,
			wantStderr: "yield outside a generator function",
			dossier:    "**Gap R.470** — the reference prints `1`; the module twin refuses with this same sentence, and the walk behind it is `fold.go`'s `containsYield`, which never enters a TryStmt or a WithStmt",
		},
		{
			// The dynamic door joins one register out of arms of different kinds: refused in words, and the
			// refusal is this row's own, registered as a capability code.
			name:       "a split generator census is refused in words, not answered",
			src:        "class B():\n    def m(self):\n        return 0\nclass D(B):\n    def m(self):\n        yield 3\n        yield 4\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(D())\n",
			wantExit:   1,
			wantStderr: "is a generator in some classes this program declares and not in others",
			dossier:    "**Gap R.467** — the reference prints `3 4`; the door refuses rather than walk a counter, and the same road answers `0` for a method that returns a LIST in every arm (**Gap R.471**, no yield anywhere in that program)",
		},
		{
			// The control beside the refusal above: the dynamic door already answered `0` for a container
			// before this row existed, which is what makes the refusal a decision and not an oversight.
			name:     "a dynamically dispatched container answer still walks a counter",
			src:      "class B():\n    def m(self):\n        return [9, 9]\nclass D(B):\n    def m(self):\n        return [3, 4]\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(D())\n",
			wantExit: 0,
			wantOut:  "0\n",
			dossier:  "**Gap R.471** — the reference prints `3 4`, byte-identical on this row's parent: no generator anywhere in the program, and the row that owns it is the dynamic door's kind word",
		},
		{
			// The truth of a generator's answer is taken from the WORD the buffer arrived in, not from the
			// buffer: handle 0 is a legal slot (Gap K.1's standing rule), and the module door's first
			// allocation IS that slot, so the same question asked of the two doors is decided by which
			// number the allocator handed out. The method door prints the reference's `yes` by the same
			// accident in the other direction.
			name:     "the truth of a generator answer is the handle word, not the buffer",
			src:      gmTwin + "if g():\n    print(\"yes\")\nelse:\n    print(\"no\")\n",
			wantExit: 0,
			wantOut:  "no\n",
			dossier:  "**Gap R.473** — the reference prints `yes` (and this backend prints `yes` for the method twin, whose handle happens to be non-zero); the door is `truthOperandErr`, which never asks a generator's register for its length",
		},
		{
			// **ADR 0431**'s forwarding arm asks the plan's two method tables (`methodVoid`, `methodCarry`) and
			// neither holds a generator verdict, so the forwarded call answers the word a void answers.
			name:     "a method that forwards a generator call answers a word that is not the buffer",
			src:      "class C():\n    def m(self):\n        yield 1\n        yield 2\n    def n(self):\n        return self.m()\nc = C()\nprint(c.n())\n",
			wantExit: 0,
			wantOut:  "1\n",
			dossier:  "**Gap R.476** — the eager model's answer is `[1, 2]` (what the module twin prints and what the record holds for `print(g())`); the walking form of the same body prints `0` for the reference's `1 2`, and the table the forwarding arm reads has no generator row",
		},
		{
			// The one shape this row REFUSES rather than answer: the loop road classifies a Name iterable out
			// of the tables the module walk fills, a method parameter is in none of them, and the walk would run
			// zero iterations and hand back the EMPTY buffer at the exit code of success.
			name:       "a generator method walking its own parameter is refused, not answered empty",
			src:        "class C():\n    def m(self, xs):\n        for v in xs:\n            yield v\n\nxs = [1, 2]\nprint(C().m(xs))\n",
			wantExit:   1,
			wantStderr: "is a generator whose body walks its own parameter",
			dossier:    "**Gap R.475** — the reference walks the list; the SAME four lines in a module `def` answer `[1, 2]` on this build, so the hole is the method door's missing kind word, and the row's parent refused the program with `yield outside a generator function` (the refusal is this row's decision, the hole is not)",
		},
		{
			// The shape this row surfaced but did not dig: a container LITERAL written into a method — a call
			// argument, or a field the constructor fills — has its global emitted inside the `define`, which `llc`
			// rejects. The module call road copies a literal into the heap through `staticLists`/`heapargs`; the
			// method road never asks that door, and the plain (yieldless) spelling is rejected byte for byte on
			// this row's parent, which is the measurement that files it as another row's lesion.
			name:       "a container literal handed to a method is the rejection its parent already was",
			src:        "class C():\n    def m(self, xs):\n        for i in range(2):\n            yield xs\n\nprint(C().m([1, 2]))\n",
			wantExit:   1,
			wantStderr: "global variable reference must have pointer type",
			dossier:    "**Gap R.474** — `i32 @.lst1` in an operand list, and the yieldless twin (`def m(self, xs):` / `        return 1`) is rejected identically on this row's PARENT; through the CLI this is the `llc` rejection's exit **2**, pinned byte for byte in `integration/generator_method_test.go`",
		},
		{
			// The eager model, not this row's decision: `print(g())` has answered a buffer since ADR 0081 and
			// the record says so for a module generator (`print(g(2))` → `[20]`). Pinned so the day the model
			// changes, the change is a decision with a row and not a drift.
			name:     "printing the generator itself prints the eager buffer",
			src:      gmTwo + "print(c.m())\n",
			wantExit: 0,
			wantOut:  "[1, 2]\n",
			dossier:  "**ADR 0081**'s eager model, stated in `docs/language.md` — the reference prints `<generator object C.m at 0x…>`; the module twin prints `[1, 2]` too",
		},
		{
			// The refusal the row must NOT widen: `len` of a generator answer is the fold's own standing
			// refusal, identical to the module door's.
			name:       "len of the generator answer is the fold's standing refusal",
			src:        gmTwo + "print(len(c.m()))\n",
			wantExit:   1,
			wantStderr: "len requires an inline list/dict/set literal",
			dossier:    "**Gap R.71**'s standing sentence — the module twin refuses `len(g())` with the same words, and the reference traps `TypeError` there",
		},
		{
			// `list(<container>)` is a registered capability refusal for EVERY container, generator or not
			// (`lower.unsupported.list-copy`, L11.1 / Gap I.2), so the generator twin refuses the same way.
			name:       "list() over the answer is the registry's copy refusal",
			src:        gmTwo + "print(list(c.m()))\n",
			wantExit:   1,
			wantStderr: "list(<container>) copies are not supported",
			dossier:    "**L11.1** / **Gap I.2** via `lower.unsupported.list-copy` — `list([1, 2])` refuses with these bytes on a program with no generator in it at all",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				if tc.wantExit != 1 {
					t.Fatalf("%s (%s): failed in a way this pin does not describe: %v", tc.name, tc.dossier, err)
				}
				if !strings.Contains(err.Error(), tc.wantStderr) {
					t.Fatalf("%s (%s): failed with %q, which is not the sentence this pin holds (%q)",
						tc.name, tc.dossier, err.Error(), tc.wantStderr)
				}
				return
			}
			if tc.wantExit == 1 {
				t.Fatalf("%s (%s): the program compiled — this pin holds a REFUSAL, and a refusal that stopped "+
					"refusing is a row that was closed without deleting its pin\n%s", tc.name, tc.dossier, tc.src)
			}
			if res.Code != tc.wantExit {
				t.Fatalf("%s (%s): exited %d with stdout %q stderr %q — an answer that arrived here means the "+
					"row was closed and this table did not follow\n%s",
					tc.name, tc.dossier, res.Code, res.Output, res.Stderr, tc.src)
			}
			if tc.wantOut != "" && res.Output != tc.wantOut {
				t.Fatalf("%s (%s): printed %q, want %q\n%s", tc.name, tc.dossier, res.Output, tc.wantOut, tc.src)
			}
		})
	}
}
