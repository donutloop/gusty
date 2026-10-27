package integration

// ADR 0432 · roadmap Gap R.455 — the reference leg of the generator METHOD.
//
// The unit file (`pkg/lang/generator_method_test.go`) proves what this compiler emits; this one proves what
// the CLI ANSWERS, against a live CPython asked in the same case rather than from a comment:
//
//	class C():
//	    def m(self):
//	        yield 1
//
//	c = C()
//	print(c.m() is None)   # the reference: False   this backend, before: exit 1 — `yield outside a generator function`
//
// The generator reading was a whole-program walk over the module's statements, so a `def` written inside a
// `ClassDef` body never got it and its body's own `yield` was refused as standing outside a generator — a
// sentence about the position the compiler chose to read the body in. **ADR 0432** takes the reading at the
// class's door, under the mangled symbol `resolveMethod` hands a call site, and gives the four doors that
// receive the answer the same question: the call roads label the register, the binding road claims the name as
// a list, the `for` road walks the buffer instead of counting to the handle it was handed, and the print door
// and the membership test ask the label the call road left.
//
// Every answered row runs on the shipped CLI engine, prints the reference's bytes, and is on the record
// (`testdata/interpreter-golden.json`), so an answer cannot be certified by the backend that earned it or
// deleted by deleting a test (**ADR 0302**). The owed rows pin this backend's bytes beside the reference's, so
// the day a door opens the pin fails rather than quietly passing.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// generatorMethodCLIBases are the modules the family is written against, spelled the same as the unit file's.
const (
	gmCLITwo = "class C():\n    def m(self):\n        yield 1\n        yield 2\nc = C()\n"
)

// generatorMethodCLIFamily is the answered family: `want` is both the bytes the reference prints (checked
// against a live CPython in every case) and the bytes this compiler must print.
func generatorMethodCLIFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"the row's program", "class C():\n    def m(self):\n        yield 1\nc = C()\nprint(c.m() is None)\n", "False\n"},
		{"the negated identity", "class C():\n    def m(self):\n        yield 1\nc = C()\nprint(c.m() is not None)\n", "True\n"},
		{"the walk", gmCLITwo + "for v in c.m():\n    print(v)\n", "1\n2\n"},
		{"bound, then walked", gmCLITwo + "ys = c.m()\nfor v in ys:\n    print(v)\n", "1\n2\n"},
		{"bound, and asked whether it is the void", gmCLITwo + "ys = c.m()\nprint(ys is None)\nfor v in ys:\n    print(v)\n", "False\n1\n2\n"},
		{"walked and summed", "class C():\n    def m(self):\n        yield 1\n        yield 2\n        yield 3\nc = C()\ns = 0\nfor v in c.m():\n    s = s + v\nprint(s)\n", "6\n"},
		{"membership, hit and miss", gmCLITwo + "print(1 in c.m())\nprint(9 in c.m())\n", "True\nFalse\n"},
		{"the truth of a buffer that holds something", gmCLITwo + "print(bool(c.m()))\n", "True\n"},
		{"two calls are two buffers", gmCLITwo + "print(c.m() == c.m())\n", "False\n"},
		{"a comprehension over the bound answer", gmCLITwo + "ys = c.m()\nprint([v for v in ys])\n", "[1, 2]\n"},
		{"a loop in the body", "class C():\n    def m(self, n):\n        for i in range(n):\n            yield i * 2\nc = C()\nfor v in c.m(3):\n    print(v)\n", "0\n2\n4\n"},
		{"both arms of a condition", "class C():\n    def m(self, flag):\n        if flag:\n            yield 1\n        else:\n            yield 2\nc = C()\nfor v in c.m(True):\n    print(v)\nfor v in c.m(False):\n    print(v)\n", "1\n2\n"},
		{"a while loop in the body", "class C():\n    def m(self):\n        i = 0\n        while i < 3:\n            yield i\n            i = i + 1\nc = C()\nt = 0\nfor v in c.m():\n    t = t + v\nprint(t)\n", "3\n"},
		{"the body's own effect on a field", "class C():\n    def __init__(self):\n        self.n = 0\n    def m(self):\n        self.n = self.n + 1\n        yield self.n\n        self.n = self.n + 1\n        yield self.n\nc = C()\nfor v in c.m():\n    print(v)\nprint(c.n)\n", "1\n2\n2\n"},
		{"a yield inside match", "class C():\n    def m(self, n):\n        match n:\n            case 1:\n                yield 10\n            case _:\n                yield 20\nc = C()\nfor v in c.m(1):\n    print(v)\nfor v in c.m(5):\n    print(v)\n", "10\n20\n"},
		// The body is read by its own `yield`; a `yield` that LIVES in a handler is lowered once the body is
		// read as a generator at all — what the reading misses is a body whose ONLY yield is in a handler
		// (that one is **Gap R.470**'s owed row below, refused on both doors).
		{"a yield inside an except handler", "class C():\n    def m(self):\n        yield 1\n        try:\n            x = 1 // 0\n        except ZeroDivisionError:\n            yield 2\nc = C()\nfor v in c.m():\n    print(v)\n", "1\n2\n"},
		{"an empty buffer walks nothing", "class C():\n    def m(self, k):\n        if k:\n            yield 1\nc = C()\nfor v in c.m(0):\n    print(v)\nprint(\"done\")\n", "done\n"},
		{"defaulted and keyword arguments", "class C():\n    def gen(self, a, b=3):\n        yield a\n        yield b\nc = C()\nfor v in c.gen(7, 8):\n    print(v)\n", "7\n8\n"},
		{"two bindings are two buffers", gmCLITwo + "a = c.m()\nb = c.m()\nfor v in a:\n    print(v)\nfor v in b:\n    print(v)\n", "1\n2\n1\n2\n"},
		{"a method calling its own class's generator", "class C():\n    def m(self):\n        yield 1\n        yield 2\n    def n(self):\n        for v in self.m():\n            print(v)\n        return None\nc = C()\nprint(c.n())\n", "1\n2\nNone\n"},
		{"super() reaching a base's generator", "class Base():\n    def gen(self):\n        yield 1\n        yield 2\nclass Child(Base):\n    def drain(self):\n        for v in super().gen():\n            print(v)\n        return None\nc = Child()\nprint(c.drain())\n", "1\n2\nNone\n"},
		{"an inherited generator", "class B():\n    def m(self):\n        yield 7\n        yield 8\nclass D(B):\n    pass\nd = D()\nfor v in d.m():\n    print(v)\n", "7\n8\n"},
		{"yield from a method's own answer", "class C():\n    def src(self):\n        return [1, 2, 3]\n    def m(self):\n        yield from self.src()\nc = C()\nfor v in c.m():\n    print(v)\n", "1\n2\n3\n"},
		{"dynamically dispatched, every arm a generator", "class B():\n    def m(self):\n        yield 1\n        yield 2\nclass D(B):\n    def m(self):\n        yield 3\n        yield 4\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(D())\n", "3\n4\n"},
	}
}

// TestTheGeneratorMethodFamilyAnswersLikeTheReference is the reference leg: the live interpreter is asked in
// the same case, and the CLI must print its bytes on the engine that ships (**ADR 0302**).
func TestTheGeneratorMethodFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range generatorMethodCLIFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "generator_method.gy", tc.src)
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

// TestTheGeneratorMethodFamilyIsOnTheRecord keeps the coverage auditable: the answer this row earned may not
// be certified by the backend that earned it, and may not be deleted by deleting a test (**ADR 0302**).
func TestTheGeneratorMethodFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	var missing []string
	for _, tc := range generatorMethodCLIFamily() {
		if !lang.HasGoldenAnswer(tc.src) {
			missing = append(missing, tc.src)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("%d program(s) of this family are not on the record leg — record them from the reference with "+
			"tools/recmerge (the compiled leg is cross-checked, never the author):\n%s", len(missing), strings.Join(missing, "\n---\n"))
	}
}

// TestTheGeneratorMethodExitCodesAreTheContractsTheyClaimToBe is the exit-code table (**ADR 0211**, **ADR
// 0006**): an answered question exits 0, a question the backend declines is refused in words at exit 1, and
// nothing this row touches reaches the assembler broken.
func TestTheGeneratorMethodExitCodesAreTheContractsTheyClaimToBe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantStdout string
		wantStderr string
	}{
		{"the row's program exits 0", "class C():\n    def m(self):\n        yield 1\nc = C()\nprint(c.m() is None)\n", 0, "False\n", ""},
		{"the walk exits 0", gmCLITwo + "for v in c.m():\n    print(v)\n", 0, "1\n2\n", ""},
		{"the bound answer exits 0", gmCLITwo + "ys = c.m()\nprint(ys)\n", 0, "[1, 2]\n", ""},
		{"a dynamic answer with one kind exits 0", "class B():\n    def m(self):\n        yield 1\nclass D(B):\n    def m(self):\n        yield 3\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(D())\n", 0, "3\n", ""},
		{
			// The split census: the door cannot see which arm runs, so it refuses in words instead of
			// walking a counter (**Gap R.467**, registered as `lower.unsupported.split-generator-method-dispatch`).
			name:     "a split generator census is refused in words",
			src:      "class B():\n    def m(self):\n        return 0\nclass D(B):\n    def m(self):\n        yield 3\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(D())\n",
			wantExit: 1, wantStdout: "",
			wantStderr: "is a generator in some classes this program declares and not in others",
		},
		{
			// A refusal this row must NOT widen: the fold's own standing sentence, identical to the one the
			// module door spends on `len(g())`.
			name:     "len over the answer keeps the fold's refusal",
			src:      gmCLITwo + "print(len(c.m()))\n",
			wantExit: 1, wantStdout: "",
			wantStderr: "len requires an inline list/dict/set literal",
		},
		{
			name:     "list() over the answer keeps the registry's copy refusal",
			src:      gmCLITwo + "print(list(c.m()))\n",
			wantExit: 1, wantStdout: "",
			wantStderr: "list(<container>) copies are not supported",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "generator_method_exit.gy", tc.src)
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: exit 2 — the module was the compiler's mistake, not the program's (ADR 0166)\n%s", engine, stderr)
				}
				if code != tc.wantExit {
					t.Fatalf("%s: exited %d, want %d (stdout %q, stderr %q)\n%s", engine, code, tc.wantExit, stdout, stderr, tc.src)
				}
				if stdout != tc.wantStdout {
					t.Fatalf("%s: printed %q, want %q\n%s", engine, stdout, tc.wantStdout, tc.src)
				}
				if tc.wantStderr != "" && !strings.Contains(stderr, tc.wantStderr) {
					t.Fatalf("%s: stderr %q does not carry %q", engine, stderr, tc.wantStderr)
				}
				for _, leak := range []string{"*lang.", "%!s(", "__cans", "methodIsGenerator"} {
					if strings.Contains(stdout+stderr, leak) {
						t.Fatalf("%s: the diagnostic leaks %q to the human and the agent:\n%s\n%s", engine, leak, stdout+stderr, tc.src)
					}
				}
			}
		})
	}
}

// TestTheGeneratorMethodRefusalIsMachineReadable is the agentic half of the honest boundary: the refusal this
// row puts where the answer cannot be earned is a machine-readable document — a stable code, a phase, a
// suggestion, the roadmap rows that own the missing half — so a script never has to read prose (**ADR 0006**,
// **ADR 0346**, roadmap **L11.8**).
func TestTheGeneratorMethodRefusalIsMachineReadable(t *testing.T) {
	t.Parallel()
	src := "class B():\n    def m(self):\n        return 0\nclass D(B):\n    def m(self):\n        yield 3\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(D())\n"
	path := writeSrc(t, t.TempDir(), "generator_method_json.gy", src)
	out, code := cliRunCode(t, "--json", path)
	if code != 1 {
		t.Fatalf("a refused program exited %d, want 1 (**ADR 0006**): %s", code, out)
	}
	for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`, `"error":`, `"capability":`,
		`"code":"lower.unsupported.split-generator-method-dispatch"`, `"suggestion":`, `"rows":`, `"Gap R.467"`} {
		if !strings.Contains(out, frag) {
			t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
		}
	}
}

// TestWhatTheGeneratorMethodStillOwesAtTheCLI is the reference leg of the honest half: the reference's bytes
// beside this backend's, with the row that owns the fix named. When a program starts printing the reference's
// bytes the row is DELETED and moves into generatorMethodCLIFamily().
func TestWhatTheGeneratorMethodStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, gap, python, compiled string }{
		{
			name: "a yielded text, float and void keep no kind", gap: "Gap R.468",
			src:    "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\n        yield 1.5\nc = C()\nfor v in c.m():\n    print(v)\n",
			python: "a\n2\nNone\n1.5\n", compiled: "0\n2\n0\n1\n",
		},
		{
			name: "a return after the yields answers the void, not the buffer", gap: "Gap R.469",
			src:    "class C():\n    def m(self):\n        yield 1\n        return\nc = C()\nxs = c.m()\nfor v in xs:\n    print(v)\n",
			python: "1\n", compiled: "",
		},
		{
			name: "a yield inside a try body is refused on both doors", gap: "Gap R.470",
			src:    "class C():\n    def m(self):\n        try:\n            yield 1\n        except ValueError:\n            yield 2\nc = C()\nfor v in c.m():\n    print(v)\n",
			python: "1\n", compiled: "",
		},
		{
			name: "the truth of a module generator's answer is the handle word", gap: "Gap R.473",
			src:    "def g():\n    yield 1\n    yield 2\nif g():\n    print(\"yes\")\nelse:\n    print(\"no\")\n",
			python: "yes\n", compiled: "no\n",
		},
		{
			name: "a dynamically dispatched container answer walks a counter", gap: "Gap R.471",
			src:    "class B():\n    def m(self):\n        return [9, 9]\nclass D(B):\n    def m(self):\n        return [3, 4]\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(D())\n",
			python: "3\n4\n", compiled: "0\n",
		},
		{
			name: "a method parameter the call omits answers 0", gap: "Gap R.472",
			src:    "class C():\n    def m(self, a, b=2):\n        return b\nc = C()\nprint(c.m(5))\n",
			python: "2\n", compiled: "0\n",
		},
		{
			// ADR 0431's forwarding arm reads the callee's WORD out of the plan's two method tables, and the
			// round that filled them has no generator verdict to give it — so a forwarded generator call answers
			// the void's word. The reference does not answer at all here (`None.m()` traps), which is what makes
			// the row's own walking form the one that matters.
			name: "a method that forwards a generator call answers 0, not the buffer", gap: "Gap R.476",
			src:    "class C():\n    def m(self):\n        yield 1\n        yield 2\n    def n(self):\n        return self.m()\nc = C()\nfor v in c.n():\n    print(v)\nprint(\"done\")\n",
			python: "1\n2\ndone\n", compiled: "0\ndone\n",
		},
		{
			// The one shape this row REFUSES rather than answer: the loop road has no kind word for a method's
			// own parameter, so the walk would run zero times and hand back the empty buffer. The same four
			// lines in a module `def` answer, because the keyed-call pass records a `def`'s parameter kind.
			name: "a generator method walking its own parameter refuses while the module twin answers", gap: "Gap R.475",
			src:    "class C():\n    def m(self, xs):\n        for v in xs:\n            yield v\n\nc = C()\nxs = [1, 2]\nfor v in c.m(xs):\n    print(v)\nprint(\"done\")\n",
			python: "1\n2\ndone\n", compiled: "",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "generator_method_owed.gy", r.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != r.python {
				t.Fatalf("the reference prints %q, not the %q this table pins — the row is stale\n%s", want, r.python, r.src)
			}
			stdout, stderr, code := cliStreams(t, cliEngines[0], path)
			if code == 2 {
				t.Fatalf("exit 2 (**ADR 0166**): %s\n%s", stderr, r.src)
			}
			if stdout != r.compiled {
				t.Fatalf("%s: this backend prints %q beside the reference's %q — the day it prints the reference's "+
					"bytes, delete this row and move the program into generatorMethodCLIFamily()\n%s",
					r.gap, stdout, r.python, r.src)
			}
		})
	}
}

// TestTheGeneratorMethodParameterWalkRefusalIsMachineReadable is the machine path of the refusal **ADR 0432**
// chose over an empty-buffer answer: a script reads a code, a phase, a suggestion and the rows that own the
// missing half rather than matching on prose (**ADR 0006**, **ADR 0346**, roadmap **L11.8**).
func TestTheGeneratorMethodParameterWalkRefusalIsMachineReadable(t *testing.T) {
	t.Parallel()
	src := "class C():\n    def m(self, xs):\n        for v in xs:\n            yield v\n\nxs = [1, 2]\nprint(C().m(xs))\n"
	path := writeSrc(t, t.TempDir(), "generator_method_param_walk.gy", src)
	out, code := cliRunCode(t, "--json", path)
	if code != 1 {
		t.Fatalf("a refused program exited %d, want 1 (**ADR 0006**): %s", code, out)
	}
	for _, frag := range []string{`"ok":false`, `"exit":1`, `"phase":"codegen"`, `"error":`, `"capability":`,
		`"code":"lower.unsupported.generator-method-parameter-walk"`, `"suggestion":`, `"rows":`, `"Gap R.475"`} {
		if !strings.Contains(out, frag) {
			t.Fatalf("the JSON refusal carries no %s:\n%s", frag, out)
		}
	}
	// The suggestion is advice a reader can act on: it names the module `def` road that already answers.
	if !strings.Contains(out, "module `def`") {
		t.Fatalf("the suggestion does not name the road that answers:\n%s", out)
	}
}

// TestTheContainerLiteralCallArgumentIsTheRejectionItsParentAlreadyWas pins the shape this row surfaced but
// did not dig: a CONTAINER LITERAL written into a method — as a call argument, or as a field in `__init__` —
// has its global emitted inside the `define`, which `llc` rejects (an `llc` rejection is **exit 2**, the
// contract **ADR 0211**). The plain-method spelling (no `yield` anywhere) is rejected byte-identically on this
// row's parent, which is the measurement that makes it **Gap R.474**'s lesion and not this row's damage: the
// program-module road copies a literal into the heap through `staticLists`/`heapargs` and the method road never
// asks that door.
func TestTheContainerLiteralCallArgumentIsTheRejectionItsParentAlreadyWas(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src string }{
		{"a generator method given a list literal", "class C():\n    def m(self, xs):\n        for i in range(2):\n            yield xs\n\nprint(C().m([1, 2]))\n"},
		{"the plain method the parent already rejected", "class C():\n    def m(self, xs):\n        return 1\n\nprint(C().m([1, 2]))\n"},
		{"a field the constructor fills with a literal", "class C():\n    def __init__(self):\n        self.xs = [1, 2]\n    def m(self):\n        for v in self.xs:\n            print(v)\n        return 1\n\nC().m()\n"},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "generator_method_literal.gy", r.src)
			_, stderr, code := cliStreams(t, cliEngines[0], path)
			if code != 2 {
				t.Fatalf("%s (**Gap R.474**): exited %d, want the `llc` rejection's 2 — the day this changes, the "+
					"row that owns the literal call argument was landed and this pin must follow it\n%s", r.name, code, r.src)
			}
			if !strings.Contains(stderr, "global variable reference must have pointer type") &&
				!strings.Contains(stderr, "expected instruction opcode") {
				t.Fatalf("%s (**Gap R.474**): rejected with %q, which is not the `llc` sentence this pin holds\n%s", r.name, stderr, r.src)
			}
		})
	}
}

// TestTheGeneratorMethodCorpusProgramIsTheRowItClaimsToBe checks the matrix program is the program this row
// was filed with, and that the compiled leg and CPython still agree on it line for line.
func TestTheGeneratorMethodCorpusProgramIsTheRowItClaimsToBe(t *testing.T) {
	t.Parallel()
	src := readProgram(t, "generator_method.gy")
	want, ok := cpythonOut(t, writeSrc(t, t.TempDir(), "generator_method_matrix.py", src))
	if !ok {
		t.Skip("no live python3 to ask")
	}
	for _, line := range strings.Split(strings.TrimRight(want, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			t.Fatalf("the matrix program's reference output carries a blank line — every line must be an answer:\n%s", want)
		}
	}
	if !strings.Contains(want, "False\n") || !strings.Contains(want, "None\n") {
		t.Fatalf("the matrix program no longer asks the two questions the row was filed with:\n%s", want)
	}
	for _, engine := range cliEngines {
		path := writeSrc(t, t.TempDir(), "generator_method.gy", src)
		stdout, stderr, code := cliStreams(t, engine, path)
		if code != 0 || stdout != want {
			t.Fatalf("%s: exit %d, printed %q (reference %q, stderr %q)", engine, code, stdout, want, stderr)
		}
	}
}
