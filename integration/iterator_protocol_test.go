package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// Roadmap row **L12.2 / Gap R.54** at the interface: the ITERATOR PROTOCOL, judged against a live CPython.
//
// The unit file (`pkg/lang/iterator_protocol_test.go`) asks the shapes on the compiled leg and the record leg
// (**ADR 0302**); this file asks the CLI — the in-process JIT for the family, and a `cc` link-and-run for the
// program that carries the whole family — against the reference, which is asked INSIDE each row rather than
// remembered in a table of expected bytes. A row here passes only while both legs say the same thing.
//
// The rows were written after measuring the parent, where every one of them printed NOTHING and exited **0**:
// a `for` whose iterable was an object asked `__iter__` a question the loop never had, so it counted zero slots
// and reported success. An answer row that drifts back to that fails here on the byte-comparison, and the
// refusal rows fail on the exit code, because a protocol loop that compiles to an empty loop is exactly the
// exit-0 silence these rows exist to forbid.

// iteratorFamily is the promise at the interface. Every `want` was read off a live CPython 3.12 first.
func iteratorFamily() []struct{ name, src, want string } {
	const counter = `class Count:
    def __init__(self):
        self.i = 0

    def __iter__(self):
        return self

    def __next__(self):
        self.i = self.i + 1
        if self.i > 3:
            raise StopIteration
        return self.i
`
	return []struct{ name, src, want string }{
		{
			"the counter class counts out and the trailer runs",
			counter + "\nfor v in Count():\n    print(v)\nprint(\"after\")\n", "1\n2\n3\nafter\n",
		},
		{
			"a frame sums what the iterator hands back",
			counter + "\ndef total_of(it):\n    s = 0\n    for v in it:\n        s = s + v\n    return s\n\nprint(total_of(Count()))\n", "6\n",
		},
		{
			"the element keeps the kind __next__ answered with",
			"class Words:\n    def __init__(self):\n        self.i = 0\n\n    def __iter__(self):\n        return self\n\n    def __next__(self):\n        self.i = self.i + 1\n        if self.i > 2:\n            raise StopIteration\n        if self.i == 1:\n            return \"a\"\n        return \"b\"\n\nfor w in Words():\n    print(w)\n", "a\nb\n",
		},
		{
			"the loop eats the end signal, so an enclosing handler never sees it",
			counter + "\ntry:\n    for v in Count():\n        print(v)\nexcept StopIteration:\n    print(\"leaked\")\nprint(\"tail\")\n", "1\n2\n3\ntail\n",
		},
		{
			"an exception that is NOT the end signal reaches the program's own handler",
			"class Boom:\n    def __iter__(self):\n        return self\n\n    def __next__(self):\n        raise ValueError(\"boom\")\n\ntry:\n    for v in Boom():\n        print(v)\nexcept ValueError:\n    print(\"caught\")\n", "caught\n",
		},
		{
			"a subclass inherits the protocol",
			counter + "\nclass Sub(Count):\n    pass\n\nfor v in Sub():\n    print(v)\n", "1\n2\n3\n",
		},
	}
}

func TestTheIteratorProtocolAnswersWhatCPythonAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range iteratorFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, code := cliRunCode(t, "--eval", tc.src)
			if code != 0 {
				t.Fatalf("the compiled leg exited %d on a program the reference prints %q:\n%s", code, tc.want, got)
			}
			if got != tc.want {
				t.Fatalf("the compiled leg printed %q, the family says %q\n%s", got, tc.want, tc.src)
			}
			wantPy, _, refErr := lang.PythonRun(tc.src)
			if refErr != nil {
				t.Fatalf("the reference did not answer a program this row prints %q: %v\n%s", tc.want, refErr, tc.src)
			}
			if wantPy != tc.want {
				t.Fatalf("the family drifted from the reference: CPython prints %q, the row says %q\n%s", wantPy, tc.want, tc.src)
			}
		})
	}
}

// TestTheIteratorProtocolLinkAndRunAnswersToo is the second execution leg: the same protocol built to a binary
// by `cc` and run as a process, byte-for-byte what CPython prints. The link leg matters more here than beside a
// scalar feature, because the loop's iterator is a heap object that has to survive a collection inside the body.
func TestTheIteratorProtocolLinkAndRunAnswersToo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "iterator_protocol.gy")
	const program = `class Count:
    def __init__(self):
        self.i = 0

    def __iter__(self):
        return self

    def __next__(self):
        self.i = self.i + 1
        if self.i > 2:
            raise StopIteration
        return self.i

out = []
for v in Count():
    out.append(v * 10)
print(out, len(out))
`
	if err := os.WriteFile(src, []byte(program), 0o644); err != nil {
		t.Fatalf("write program: %v", err)
	}
	bin := cliBin(t)
	exe := filepath.Join(dir, "iterator_protocol")
	build := exec.Command(bin, "--build", exe, src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("link-and-run build failed: %v\n%s", err, out)
	}
	run := exec.Command(exe)
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the built program failed: %v\n%s", err, out)
	}
	if got := string(out); got != "[10, 20] 2\n" {
		t.Fatalf("the built program printed %q, the reference prints \"[10, 20] 2\\n\"\n%s", got, program)
	}
	wantPy, _, refErr := lang.PythonRun(program)
	if refErr != nil {
		t.Fatalf("the reference did not answer the program: %v", refErr)
	}
	if wantPy != "[10, 20] 2\n" {
		t.Fatalf("CPython disagrees with the row: %q", wantPy)
	}
}

// TestAnUniterableObjectRaisesAtTheCLIWithTheReferenceOwnExitCode asks the raise through the CLI rather than the
// library: the class, the sentence, the caption line, and the exit code that tells a script the program died.
// Zero iterations at exit 0 is what the parent did here, and an agent cannot tell that from an empty iterable.
func TestAnUniterableObjectRaisesAtTheCLIWithTheReferenceOwnExitCode(t *testing.T) {
	t.Parallel()
	const src = "class Plain:\n    def value(self):\n        return 1\n\nfor v in Plain():\n    print(v)\nprint(\"after\")\n"
	got, code := iteratorCLIMerged(t, "--eval", src)
	if code != 3 {
		t.Fatalf("the compiled leg exited %d on a program the reference dies on; 0 would be the silent answer "+
			"this row was filed against\n"+src, code)
	}
	for _, want := range []string{"TypeError", "'Plain' object is not iterable", "Traceback (most recent call last)", "line 5"} {
		if !strings.Contains(got, want) {
			t.Fatalf("the raise did not carry %q:\n%s", want, got)
		}
	}
	// The reference agrees about the class and the sentence — its own exit code is 1, which is CPython's
	// "uncaught exception" code and not this CLI's contract (ADR 0006), so only the message is compared.
	_, refErrText, refErr := lang.PythonRun(src)
	if refErr == nil {
		t.Fatalf("the reference answered a program this row raises: %q", refErrText)
	}
	if !strings.Contains(refErrText, "TypeError: 'Plain' object is not iterable") {
		t.Fatalf("the reference does not raise the sentence this row raises:\n%s", refErrText)
	}
}

// iteratorCLIMerged runs the CLI with stdout and stderr in ONE stream: a raise prints its traceback to stderr,
// and a row that asks what the program SAID has to read both halves — the stdout the program got out before it
// died and the caption that says why.
func iteratorCLIMerged(t *testing.T, args ...string) (string, int) {
	t.Helper()
	cmd := exec.Command(cliBin(t), args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("gustyc %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return string(out), code
}

// TestTheIteratorShapesThatCannotBeLoweredRefuseAtTheCLI is the machine path: exit 1 (never 2, which is our own
// crash — ADR 0166), the sentence the unit file pins, the roadmap row that owns the missing half, and the
// capability CODE the `--json` payload carries so an agent can branch on the refusal without scraping prose.
func TestTheIteratorShapesThatCannotBeLoweredRefuseAtTheCLI(t *testing.T) {
	t.Parallel()
	const counter = `class Count:
    def __init__(self):
        self.i = 0

    def __iter__(self):
        return self

    def __next__(self):
        self.i = self.i + 1
        if self.i > 2:
            raise StopIteration
        return self.i
`
	for _, tc := range []struct {
		name   string
		src    string
		says   string
		wants  string
		codeID string
	}{
		{
			"__getitem__ is the reference's other way to be iterable",
			"class Squares:\n    def __getitem__(self, i):\n        if i > 2:\n            raise IndexError\n        return i * i\n\nfor v in Squares():\n    print(v)\n",
			"leans on __getitem__", "Gap R.514", "lower.unsupported.iterator-getitem-sequence",
		},
		{
			"__iter__ handing back a field has no static class",
			"class Hidden:\n    def __init__(self):\n        self.src = 0\n\n    def __iter__(self):\n        return self.src\n\nfor v in Hidden():\n    print(v)\n",
			"whose class the source does not name", "Gap R.515", "lower.unsupported.iterator-opaque-handoff",
		},
		{
			"__iter__ handing back a literal is the reference's non-iterator TypeError",
			"class OnlyIter:\n    def __iter__(self):\n        return 1\n\nfor v in OnlyIter():\n    print(v)\n",
			"iter() returned non-iterator of type 'int'", "Gap R.516", "lower.unsupported.iterator-not-an-iterator",
		},
		{
			"two call sites, one parameter, two iterables",
			counter + "\ndef walk(it):\n    n = 0\n    for v in it:\n        n = n + 1\n    return n\n\nprint(walk(Count()))\nprint(walk(3))\n",
			"fact about the call", "Gap R.517", "lower.unsupported.iterator-parameter-kinds-disagree",
		},
		{
			"a container the loop fills and the frame hands back has no kind for the answer table",
			counter + "\ndef build(it):\n    out = []\n    for v in it:\n        out.append(v)\n    return out\n\nprint(build(Count()))\n",
			"the answer table has no kind to record", "Gap R.518", "lower.unsupported.iterator-frame-answers-built-container",
		},
		{
			"a __next__ answering a fractional number has no float word to travel in",
			"class Halves:\n    def __init__(self):\n        self.i = 0\n\n    def __iter__(self):\n        return self\n\n    def __next__(self):\n        self.i = self.i + 1\n        if self.i > 2:\n            raise StopIteration\n        return self.i / 4\n\nfor v in Halves():\n    print(v)\n",
			"without the word that says it is a float", "Gap R.519", "lower.unsupported.iterator-float-answer",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, code := cliRefusalRun(t, tc.src)
			if code == 2 {
				t.Fatalf("exit 2 is our own crash, not a refusal:\n%s", out)
			}
			if code != 1 {
				t.Fatalf("expected exit 1 for a refusal, got %d:\n%s", code, out)
			}
			for _, want := range []string{tc.says, tc.wants, tc.codeID} {
				if !strings.Contains(out, want) {
					t.Fatalf("the diagnostic did not contain %q:\n%s", want, out)
				}
			}
			// The refusal must not be a wrapper around silence: the same source through the library is an
			// error too, so no path runs a program this row says it cannot lower.
			if _, err := lang.Compile(tc.src); err == nil {
				t.Fatalf("the library COMPILED a program the CLI refuses at exit 1\n%s", tc.src)
			}
		})
	}
}
