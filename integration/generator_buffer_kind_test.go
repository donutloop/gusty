package integration

// ADR 0433 · roadmap Gap R.468 — the reference leg of the generator's BUFFER KINDS.
//
// The unit file (`pkg/lang/generator_buffer_kind_test.go`) proves what this compiler emits; this one proves
// what the CLI ANSWERS, against a live CPython asked in the same case rather than from a comment:
//
//	class C():
//	    def m(self):
//	        yield "a"      # the reference prints a;  this backend, before, printed 0
//	        yield 2        # 2 — the only word that was ever right
//	        yield None     # None  vs  0
//	        yield 1.5      # 1.5   vs  1
//
//	c = C()
//	for v in c.m():
//	    print(v)
//
// A generator's buffer is a list, and every other list in this backend writes its element's kind beside its
// payload because **ADR 0187** made `rt_append` and `rt_tag_elem` one instruction; the generator road never
// asked that door, so the four roads that receive the answer — the walk, the print door, a binding, the
// membership test — read the payload and guessed at the kind. **Gap R.468** proved the hole belonged to the
// append door by walking the SAME body through a module `def` and getting the SAME wrong bytes back.
//
// Every answered row runs on the shipped CLI engines, prints the reference's bytes, and is on the record
// (`testdata/interpreter-golden.json`), so an answer cannot be certified by the backend that earned it or
// deleted by deleting a test (**ADR 0302**). The rows the eager model and the reference disagree about
// (`print(g())` prints the buffer — **ADR 0081**) are pinned as their own table, and the owed rows pin this
// backend's bytes beside the reference's so the day a door opens, the pin fails rather than quietly passing.

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// generatorBufferKindFamily is the answered half. `want` is both the bytes a live CPython prints (checked in
// every case) and the bytes this compiler must print.
func generatorBufferKindFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{"the row's program", "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\n        yield 1.5\nc = C()\nfor v in c.m():\n    print(v)\n", "a\n2\nNone\n1.5\n"},
		{"the same body at the module door", "def g():\n    yield \"a\"\n    yield 2\n    yield None\n    yield 1.5\nfor v in g():\n    print(v)\n", "a\n2\nNone\n1.5\n"},
		{"bound, then walked", "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\n        yield 1.5\nc = C()\nys = c.m()\nfor v in ys:\n    print(v)\n", "a\n2\nNone\n1.5\n"},
		{"a verdict keeps its word", "class C():\n    def m(self):\n        yield True\n        yield False\nc = C()\nfor v in c.m():\n    print(v)\n", "True\nFalse\n"},
		{"a yielded container prints its brackets", "class C():\n    def m(self):\n        yield [1, 2]\n        yield 3\nc = C()\nfor v in c.m():\n    print(v)\n", "[1, 2]\n3\n"},
		{"a float beside an integer", "class C():\n    def m(self):\n        yield 1.5\n        yield 2\n        yield 2\n        yield 1.5\nc = C()\nfor v in c.m():\n    print(v)\n", "1.5\n2\n2\n1.5\n"},
		{"a buffer of nothing but texts", "class C():\n    def m(self):\n        yield \"hi\"\n        yield \"there\"\nc = C()\nfor v in c.m():\n    print(v)\n", "hi\nthere\n"},
		{"a buffer of nothing but the void", "class C():\n    def m(self):\n        yield None\n        yield None\nc = C()\nfor v in c.m():\n    print(v)\n", "None\nNone\n"},
		{"an interpolated field yields the text it renders", "def g(n):\n    yield f\"n={n}\"\n    yield 7\nfor v in g(3):\n    print(v)\n", "n=3\n7\n"},
		{"yield from a literal that mixes kinds", "def g():\n    yield from [1, \"a\", None]\nfor v in g():\n    print(v)\n", "1\na\nNone\n"},
		{"yield from a literal that mixes kinds, through the class door", "class C():\n    def m(self):\n        yield from [True, \"z\", 2.5]\nc = C()\nfor v in c.m():\n    print(v)\n", "True\nz\n2.5\n"},
		// A `yield from` that forwards another generator: the claim crosses the copy by asking the forwarded
		// body, and the integer twin is what keeps a forwarded SUM out of Gap R.146's refusal.
		{"a buffer forwarded from another generator keeps the kinds", "def inner():\n    yield \"a\"\n    yield 2\ndef outer():\n    yield from inner()\n    yield None\nfor v in outer():\n    print(v)\n", "a\n2\nNone\n"},
		{"a buffer forwarded through the class door keeps the kinds", "class C():\n    def gen(self):\n        yield \"a\"\n        yield 2\n    def outer(self):\n        yield from self.gen()\n        yield None\nc = C()\nfor v in c.outer():\n    print(v)\n", "a\n2\nNone\n"},
		{"a forwarded integer buffer still sums", "def gen(n):\n    for i in range(n):\n        yield i * i\ndef outer():\n    yield from gen(3)\n    yield 99\ns = 0\nfor x in outer():\n    s = s + x\nprint(s)\n", "104\n"},
		{"membership finds the yielded text", "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\nc = C()\nprint(\"a\" in c.m())\nprint(\"b\" in c.m())\n", "True\nFalse\n"},
		{"an inherited generator keeps the kinds", "class B():\n    def m(self):\n        yield None\n        yield \"z\"\nclass D(B):\n    pass\nd = D()\nfor v in d.m():\n    print(v)\n", "None\nz\n"},
		{"super() keeps the kinds", "class Base():\n    def gen(self):\n        yield \"b\"\n        yield 1\nclass Child(Base):\n    def drain(self):\n        for v in super().gen():\n            print(v)\n        return None\nc = Child()\nprint(c.drain())\n", "b\n1\nNone\n"},
		{"a call through self keeps the kinds", "class C():\n    def m(self):\n        yield \"s\"\n        yield 1\n    def n(self):\n        for v in self.m():\n            print(v)\n        return None\nc = C()\nprint(c.n())\n", "s\n1\nNone\n"},
		{"dynamically dispatched, every arm mixed", "class B():\n    def m(self):\n        yield \"x\"\n        yield 1\nclass D(B):\n    def m(self):\n        yield \"y\"\n        yield 2\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(D())\n", "y\n2\n"},
		// The controls: an all-integer buffer is NOT read through the pair, so a sum stays a sum and the
		// pair road's standing refusal (Gap R.146) never reaches a program that was adding numbers.
		{"an integer buffer still sums", "class C():\n    def m(self):\n        yield 10\n        yield 20\nc = C()\ns = 0\nfor v in c.m():\n    s = s + v\nprint(s)\n", "30\n"},
		{"an integer buffer at the module door still sums", "def g():\n    yield 10\n    yield 20\ns = 0\nfor v in g():\n    s = s + v\nprint(s)\n", "30\n"},
		{"a buffer whose elements come from a parameter keeps its answer", "class C():\n    def m(self, k):\n        yield k\n        yield k + 1\nc = C()\nt = 0\nfor k in range(3):\n    for v in c.m(k):\n        t = t + v\nprint(t)\n", "9\n"},
		{"a buffer of expressions keeps its answer", "class C():\n    def m(self, n):\n        for i in range(n):\n            yield i * 2\nc = C()\nfor v in c.m(3):\n    print(v)\n", "0\n2\n4\n"},
	}
}

// TestAYieldedKindAnswersLikeTheReference is the reference leg: the live interpreter is asked in the same
// case, and the CLI must print its bytes on the engine that ships (**ADR 0302**).
func TestAYieldedKindAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range generatorBufferKindFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "generator_buffer_kind.gy", tc.src)
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

// TestAYieldedKindFamilyIsOnTheRecord keeps the coverage auditable: an answer this row earned may not be
// certified by the backend that earned it, and may not be deleted by deleting a test (**ADR 0302**).
func TestAYieldedKindFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	var missing []string
	for _, tc := range generatorBufferKindFamily() {
		if !lang.HasGoldenAnswer(tc.src) {
			missing = append(missing, tc.src)
		}
	}
	if len(missing) != 0 {
		t.Fatalf("%d program(s) of this family are not on the record leg — record them from the reference with "+
			"tools/recmerge (the compiled leg is cross-checked, never the author):\n%s", len(missing), strings.Join(missing, "\n---\n"))
	}
}

// TestTheEagerBufferPrintsWhatItsSlotsHold is the model's own boundary, kept out of the reference family
// because the reference cannot witness it: printing the generator prints its eager buffer (**ADR 0081**,
// `docs/language.md`), where CPython prints `<generator object …>`. Both doors must print the SAME list, and
// the integers-only control must print the list it always printed.
func TestTheEagerBufferPrintsWhatItsSlotsHold(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"a mixed buffer, at the class door", "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\n        yield 1.5\nc = C()\nprint(c.m())\n", "['a', 2, None, 1.5]\n"},
		{"a mixed buffer, at the module door", "def g():\n    yield \"a\"\n    yield 2\n    yield None\n    yield 1.5\nprint(g())\n", "['a', 2, None, 1.5]\n"},
		{"a verdict prints True inside the brackets", "def g():\n    yield True\n    yield None\nprint(g())\n", "[True, None]\n"},
		{"an integer buffer prints the list it always printed", "class C():\n    def m(self):\n        yield 1\n        yield 2\nc = C()\nprint(c.m())\n", "[1, 2]\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "generator_buffer_eager.gy", tc.src)
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code != 0 {
					t.Fatalf("%s: exited %d with %q, want the eager buffer at exit 0\n%s", engine, code, stderr, tc.src)
				}
				if stdout != tc.want {
					t.Fatalf("%s (**ADR 0081**'s eager model): printed %q, want %q\n%s", engine, stdout, tc.want, tc.src)
				}
			}
		})
	}
}

// TestTheYieldedKindExitCodesAreTheContractsTheyClaimToBe is the exit-code table (**ADR 0211**, **ADR
// 0006**): an answered question exits 0, a question the backend declines is refused in words at exit 1, and
// nothing this row touches reaches the assembler broken.
func TestTheYieldedKindExitCodesAreTheContractsTheyClaimToBe(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantStdout string
		wantStderr string
	}{
		{
			name:     "the row's program exits 0",
			src:      "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\n        yield 1.5\nc = C()\nfor v in c.m():\n    print(v)\n",
			wantExit: 0, wantStdout: "a\n2\nNone\n1.5\n", wantStderr: "",
		},
		{
			// A mixed buffer used as a NUMBER is the pair road's standing refusal (**Gap R.146**) — the same
			// sentence a mixed list literal walks into a sum, which is the ladder that says the refusal is
			// honest and the `3` this backend printed before the row was the artifact.
			name:     "a mixed buffer used as a number is the pair road's refusal",
			src:      "class C():\n    def m(self):\n        yield 1.5\n        yield 2.5\nc = C()\ns = 0\nfor v in c.m():\n    s = s + v\nprint(s)\n",
			wantExit: 1, wantStdout: "",
			wantStderr: "travels as a (payload, tag) pair",
		},
		{
			name:     "an integer buffer used as a number is not refused",
			src:      "class C():\n    def m(self):\n        yield 10\n        yield 20\nc = C()\ns = 0\nfor v in c.m():\n    s = s + v\nprint(s)\n",
			wantExit: 0, wantStdout: "30\n", wantStderr: "",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "generator_buffer_kind_exit.gy", tc.src)
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
				for _, leak := range []string{"*lang.", "%!s(", "__cans", "genBufferAppend"} {
					if strings.Contains(stdout+stderr, leak) {
						t.Fatalf("%s: the diagnostic leaks %q to the human and the agent:\n%s\n%s", engine, leak, stdout+stderr, tc.src)
					}
				}
			}
		})
	}
}

// TestWhatTheYieldedKindStillOwesAtTheCLI is the reference leg of the honest half: the reference's bytes
// beside this backend's, with the row that owns the fix named. When a program starts printing the reference's
// bytes the row is DELETED and moves into generatorBufferKindFamily().
func TestWhatTheYieldedKindStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, r := range []struct{ name, src, gap, python, compiled string }{
		{
			// The comprehension classifies its iterable out of `mixedLists` and the rendering road out of
			// `listOperands`; neither asks the buffer claim this row minted (`genBuffers`), so both still read
			// an untagged list.
			name: "a comprehension over a bound buffer reads the word, not the value", gap: "Gap R.477",
			src:    "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\nc = C()\nys = c.m()\nprint([v for v in ys])\n",
			python: "['a', 2, None]\n", compiled: "[0, 2, 0]\n",
		},
		{
			// The membership scan compares the payload word: `"a"` interns to slot 0 and the integer 0 walks
			// in as a match. The tagged compare the container roads use is the door — and the literal road
			// walks the SAME untagged door, which is why the row below is in this table too: this is not a
			// bug the generator brought, it is a bug the generator inherited.
			name: "membership takes an integer for a text", gap: "Gap R.478",
			src:    "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\nc = C()\nprint(0 in c.m())\n",
			python: "False\n", compiled: "True\n",
		},
		{
			// The control that makes the door, and not this row's buffer, the owner of the shape above: no
			// generator, no class, one literal list — and the same wrong verdict at the exit code of success,
			// byte-identical on this commit's parent.
			name: "the mixed-list twin of that question, asked of a literal", gap: "Gap R.478",
			src:    "print(0 in [\"a\", 2])\n",
			python: "False\n", compiled: "True\n",
		},
		{
			// **Gap R.467**/**Gap R.471**'s missing word beside the joined register, seen from the buffer's
			// side: the arms agree that they yield and disagree about what they yield, and the arm that runs
			// is a run-time fact — so the claim is not earned and the walk keeps reading words.
			name: "a dynamic join whose arms disagree about kinds still reads words", gap: "Gap R.467",
			src:    "class B():\n    def m(self):\n        yield \"x\"\n        yield 1\nclass D(B):\n    def m(self):\n        yield 3\n        yield 4\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(B())\nshow(D())\n",
			python: "x\n1\n3\n4\n", compiled: "0\n1\n3\n4\n",
		},
		{
			// **Gap R.475**'s missing parameter kind, reached from the copy side: `yield from` a list the
			// caller handed in cannot ask the source's slots, because the loop has no classification to read.
			name: "a yield from a parameter's list copies words, not values", gap: "Gap R.475",
			src:    "xs = [1, \"a\", None]\ndef g(src):\n    yield from src\nfor v in g(xs):\n    print(v)\n",
			python: "1\na\nNone\n", compiled: "1\n0\n0\n",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "generator_buffer_kind_owed.gy", r.src)
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
					"bytes, delete this row and move the program into generatorBufferKindFamily()\n%s",
					r.gap, stdout, r.python, r.src)
			}
		})
	}
}

// TestTheYieldedKindCorpusProgramIsTheRowItWasFiledWith checks the corpus program is the program this row
// was filed with — the four kinds the untagged word could not carry, walked at both doors, plus the buffer
// forwarded from another generator and the integer buffer that must stay a sum — and that the compiled leg
// and CPython still agree on it line for line. The parent build reaches `llc` with a container literal in an
// operand list for this program (exit 2), so the file is the row's own measurement.
func TestTheYieldedKindCorpusProgramIsTheRowItWasFiledWith(t *testing.T) {
	t.Parallel()
	src := readProgram(t, "generator_buffer_kind.gy")
	want, ok := cpythonOut(t, writeSrc(t, t.TempDir(), "generator_buffer_kind_matrix.py", src))
	if !ok {
		t.Skip("no live python3 to ask")
	}
	for _, line := range strings.Split(strings.TrimRight(want, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			t.Fatalf("the matrix program's reference output carries a blank line — every line must be an answer:\n%s", want)
		}
	}
	for _, need := range []string{"a\n2\nNone\n1.5\n", "True\nFalse\n", "[1, 2]\n3\n", "f\n2.5\nNone\n"} {
		if !strings.Contains(want, need) {
			t.Fatalf("the matrix program no longer asks the questions the row was filed with (missing %q):\n%s", need, want)
		}
	}
	for _, engine := range cliEngines {
		path := writeSrc(t, t.TempDir(), "generator_buffer_kind.gy", src)
		stdout, stderr, code := cliStreams(t, engine, path)
		if code != 0 || stdout != want {
			t.Fatalf("%s: exit %d, printed %q (reference %q, stderr %q)", engine, code, stdout, want, stderr)
		}
	}
}
