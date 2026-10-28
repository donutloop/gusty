package lang

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// Roadmap row **L12.2 / Gap R.54** at the compiler: the ITERATOR PROTOCOL. A `for` asks the object for its
// iterator with `__iter__`, asks the iterator for one element per turn with `__next__`, and the
// `StopIteration` the iterator raises when it has none left IS the loop's end signal (**ADR 0449**).
//
// Before this row the loop had exactly one question for an iterable — can I count its slots? — and an object
// that answered a different question fell through the loop entirely: the counter class below compiled, ran
// ZERO iterations, printed nothing, and exited **0**, the code that means the program ran. The reference
// prints `1`, `2`, `after`. Every row here was measured against the reference first.
//
// The answers are judged on the compiled leg AND the record leg (**ADR 0302**); the refusals are judged on
// the sentence and the row that owns the missing half, because a gate that compiles a program the reference
// rejects — or that answers a program the reference runs with silence — is the failure this file keeps out.

// iteratorClass is the protocol class every shape below is written with: an `__iter__` that hands back
// `self`, a `__next__` that counts and raises the end signal.
const iteratorClass = `class Count:
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

// iteratorAnswers is the promise: an object that says `__iter__`/`__next__` is iterated. Each `want` is what
// `python3` prints for the same source, read off the reference rather than off this backend.
var iteratorAnswers = []struct{ name, src, want string }{
	{
		"the row's own program: the counter class counts out, and the trailer runs",
		iteratorClass + "\nfor v in Count():\n    print(v)\nprint(\"after\")\n", "1\n2\n3\nafter\n",
	},
	{
		"the loop sums what the iterator yields",
		iteratorClass + "\ntotal = 0\nfor v in Count():\n    total = total + v\nprint(total)\n", "6\n",
	},
	{
		"a name bound to the construction is the iterable",
		iteratorClass + "\nr = Count()\nfor v in r:\n    print(v)\n", "1\n2\n3\n",
	},
	{
		"the element keeps the kind __next__ answered with: a text stays a text",
		"class Words:\n    def __init__(self):\n        self.i = 0\n\n    def __iter__(self):\n        return self\n\n    def __next__(self):\n        self.i = self.i + 1\n        if self.i > 2:\n            raise StopIteration\n        if self.i == 1:\n            return \"a\"\n        return \"b\"\n\nfor w in Words():\n    print(w)\nprint(\"done\")\n", "a\nb\ndone\n",
	},
	{
		"break leaves the loop early, and continue skips one body",
		iteratorClass + "\nfor v in Count():\n    if v == 2:\n        continue\n    if v == 3:\n        break\n    print(v)\nprint(\"after\")\n", "1\nafter\n",
	},
	{
		"the end signal is a NORMAL completion, so the loop's else clause runs",
		iteratorClass + "\nfor v in Count():\n    print(v)\nelse:\n    print(\"counted out\")\n", "1\n2\n3\ncounted out\n",
	},
	{
		"a break skips the else clause, exactly as a counted loop's does",
		iteratorClass + "\nfor v in Count():\n    if v == 2:\n        break\nelse:\n    print(\"never\")\nprint(\"tail\")\n", "tail\n",
	},
	{
		"__iter__ may hand back a DIFFERENT class, named by the source",
		"class Inner:\n    def __init__(self):\n        self.n = 0\n\n    def __iter__(self):\n        return self\n\n    def __next__(self):\n        self.n = self.n + 1\n        if self.n > 2:\n            raise StopIteration\n        return self.n * 10\n\nclass Outer:\n    def __iter__(self):\n        return Inner()\n\nfor v in Outer():\n    print(v)\n", "10\n20\n",
	},
	{
		"two protocol loops, nested, each with its own iterator",
		iteratorClass + "\nclass Inner:\n    def __init__(self):\n        self.n = 0\n\n    def __iter__(self):\n        return self\n\n    def __next__(self):\n        self.n = self.n + 1\n        if self.n > 2:\n            raise StopIteration\n        return self.n\n\nfor a in Count():\n    for b in Inner():\n        print(a * 100 + b)\n", "101\n102\n201\n202\n301\n302\n",
	},
	{
		"a frame whose PARAMETER is the iterable: the call site names the class",
		iteratorClass + "\ndef total_of(it):\n    s = 0\n    for v in it:\n        s = s + v\n    return s\n\nprint(total_of(Count()))\n", "6\n",
	},
	{
		"a frame builds a container in a protocol loop it can see the class of",
		iteratorClass + "\ndef build():\n    out = []\n    for v in Count():\n        out.append(v)\n    return out\n\nprint(build())\n", "[1, 2, 3]\n",
	},
	{
		"an exception that is NOT the end signal walks out to the program's own handler",
		iteratorClass + "\nclass Boom:\n    def __iter__(self):\n        return self\n\n    def __next__(self):\n        raise ValueError(\"boom\")\n\ntry:\n    for v in Boom():\n        print(v)\nexcept ValueError:\n    print(\"caught\")\n", "caught\n",
	},
	{
		"the loop catches StopIteration ITSELF: an enclosing try never sees it",
		iteratorClass + "\ntry:\n    for v in Count():\n        print(v)\nexcept StopIteration:\n    print(\"the loop should have eaten it\")\nprint(\"tail\")\n", "1\n2\n3\ntail\n",
	},
	{
		"a subclass inherits the protocol the way it inherits every other method (ADR 0446)",
		iteratorClass + "\nclass Sub(Count):\n    pass\n\nfor v in Sub():\n    print(v)\n", "1\n2\n3\n",
	},
	{
		"a method's loop asks the protocol too",
		iteratorClass + "\nclass Report:\n    def sum_all(self):\n        s = 0\n        for v in Count():\n            s = s + v\n        return s\n\nprint(Report().sum_all())\n", "6\n",
	},
	{
		"a protocol object reached through a name bound in a function is iterated",
		iteratorClass + "\nout = []\nfor v in Count():\n    out.append(v * 2)\nprint(out, len(out))\n", "[2, 4, 6] 3\n",
	},
	{
		// The catchable face of the raise below: what `except TypeError as e:` binds is the message with no
		// caption on it, which is what the reference's `str(e)` returns (Gap R.113's contract).
		"the handler binds the reference's own sentence",
		"class Plain:\n    pass\n\ntry:\n    for v in Plain():\n        print(v)\nexcept TypeError as e:\n    print(str(e))\n", "'Plain' object is not iterable\n",
	},
}

// iteratorTraps is the reference's failure half: an object with no `__iter__` and no `__next__` is not
// iterable, and the compiled leg RAISES that sentence at the line that wrote `for` — catchable, and fatal
// with exit 3 to a program that has no handler. Zero iterations at exit 0 was the answer before ADR 0449.
var iteratorTraps = []struct{ name, src, class, message string }{
	{
		"an object with neither dunder is not iterable",
		"class Plain:\n    def value(self):\n        return 1\n\nfor v in Plain():\n    print(v)\nprint(\"after\")\n",
		"TypeError", "'Plain' object is not iterable",
	},
	{
		"__next__ alone does not make an iterable",
		"class Tail:\n    def __next__(self):\n        return 1\n\nfor v in Tail():\n    print(v)\n",
		"TypeError", "'Tail' object is not iterable",
	},
	{
		"a subclass that declares nothing inherits the non-iterability",
		"class Plain:\n    def value(self):\n        return 1\n\nclass Derived(Plain):\n    pass\n\nfor v in Derived():\n    print(v)\n",
		"TypeError", "'Derived' object is not iterable",
	},
}

// iteratorRefusals is the half this backend will not guess at. Each names the construct the reference
// answers, the missing rule, and the roadmap row that owns it. None of them may reach the printer, and
// none of them may reach `llc` (exit 2 is a compiler bug, ADR 0166).
var iteratorRefusals = []struct {
	name  string
	src   string
	says  string
	wants string
}{
	{
		"__getitem__ is the reference's other way to be iterable",
		"class Squares:\n    def __getitem__(self, i):\n        if i > 2:\n            raise IndexError\n        return i * i\n\nfor v in Squares():\n    print(v)\n",
		"leans on __getitem__", "Gap R.514",
	},
	{
		"__iter__ handing back a field makes the element dispatch a run-time fact",
		"class Hidden:\n    def __init__(self):\n        self.src = 0\n\n    def __iter__(self):\n        return self.src\n\nfor v in Hidden():\n    print(v)\n",
		"for a value whose class the source does not name", "Gap R.515",
	},
	{
		"__iter__ handing back a literal is the reference's `iter() returned non-iterator`",
		"class OnlyIter:\n    def __iter__(self):\n        return 1\n\nfor v in OnlyIter():\n    print(v)\n",
		"iter() returned non-iterator of type 'int'", "Gap R.516",
	},
	{
		"__iter__ handing back an object whose class has no __next__ is `not an iterator`",
		"class NoNext:\n    pass\n\nclass Hands:\n    def __iter__(self):\n        return NoNext()\n\nfor v in Hands():\n    print(v)\n",
		"'NoNext' object is not an iterator", "Gap R.516",
	},
	{
		"two call sites disagreeing about one parameter is one loop asked to be two",
		iteratorClass + "\ndef walk(it):\n    n = 0\n    for v in it:\n        n = n + 1\n    return n\n\nprint(walk(Count()))\nprint(walk(3))\n",
		"which protocol the loop runs is a fact about the call", "Gap R.517",
	},
	{
		"a container the loop fills and the frame hands back has no kind for the answer table",
		iteratorClass + "\ndef build(it):\n    out = []\n    for v in it:\n        out.append(v)\n    return out\n\nprint(build(Count()))\n",
		"the answer table has no kind to record for those slots", "Gap R.518",
	},
	{
		"a __iter__ with no return at all has no iterator to hand back",
		"class Silent:\n    def __iter__(self):\n        pass\n\nfor v in Silent():\n    print(v)\n",
		"no run-time iterator test to raise it from", "Gap R.516",
	},
	{
		// `print(H().m())` for a float-answering method is the same missing word, and the row beside this
		// one owns it: the class road emits a method as an i32 function whatever its body answers.
		"a __next__ that answers a fractional number has no float word to arrive in",
		"class Halves:\n    def __init__(self):\n        self.i = 0\n\n    def __iter__(self):\n        return self\n\n    def __next__(self):\n        self.i = self.i + 1\n        if self.i > 2:\n            raise StopIteration\n        return self.i / 4\n\nfor v in Halves():\n    print(v)\n",
		"without the word that says it is a float", "Gap R.519",
	},
}

func TestTheIteratorProtocolAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range iteratorAnswers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, the reference prints %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

func TestAnUniterableObjectRaisesWhereTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range iteratorTraps {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ee := trapRun(t, tc.src)
			if ee.ExnType != tc.class {
				t.Fatalf("the trap carried class %q, the reference raises %q (message %q)\n%s", ee.ExnType, tc.class, ee.ExnMsg, tc.src)
			}
			if ee.ExnMsg != tc.message {
				t.Fatalf("the trap carried message %q,\n                the reference raises %q\n%s", ee.ExnMsg, tc.message, tc.src)
			}
		})
	}
}

func TestTheIteratorShapesThatCannotBeLoweredRefuseByName(t *testing.T) {
	t.Parallel()
	for _, tc := range iteratorRefusals {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := compileAndRunAny(t, tc.src)
			if err == nil {
				t.Fatalf("the compiled leg ANSWERED a program this row pins as a refusal — the shape the reference answers by "+
					"asking a question this backend cannot ask must stop the build, not print nothing\n%s", tc.src)
			}
			msg := errText(out, err)
			if !strings.Contains(msg, tc.says) {
				t.Fatalf("the refusal read\n  %s\nand must contain %q\n%s", msg, tc.says, tc.src)
			}
			if !strings.Contains(msg, tc.wants) {
				t.Fatalf("the refusal did not name the row that owns the fix (%s):\n  %s\n%s", tc.wants, msg, tc.src)
			}
			if strings.Contains(msg, "llc") || strings.Contains(msg, "***") {
				t.Fatalf("a refusal that blames the toolchain is a compiler bug (ADR 0166): %s", msg)
			}
		})
	}
}

// TestTheIteratorLoopEmitsTheProtocol asks the emitted module, not the answer: the element arrives from a
// call to the class's own `__next__`, the loop tests the exception CODE rather than any flag it invented,
// and the iterator is rooted for the collector. The negative control is the row's own promise that none of
// this leaks into a loop over a container, which keeps the counter road it has always had.
func TestTheIteratorLoopEmitsTheProtocol(t *testing.T) {
	t.Parallel()
	protocolSrc := iteratorClass + "\nfor v in Count():\n    print(v)\n"
	ir, err := compileIR(t, protocolSrc)
	if err != nil {
		t.Fatalf("the protocol loop did not compile: %v", err)
	}
	for _, want := range []string{
		"call i32 @gy_Count___iter__(", // the loop asks the object for its iterator, once
		"call i32 @gy_Count___next__(", // one element per turn, from the class the source named
		"load i32, i32* @exn_code",     // the end signal is the reference's CODE, not a flag of this file's
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("the protocol module does not carry %q — the loop is not asking the object the way the protocol says\n%s", want, ir)
		}
	}
	if !strings.Contains(ir, "rt_root_put(i32* %__gy_iter") {
		t.Fatalf("the iterator handle is not rooted: a body that allocates would collect the object mid-loop (ADR 0181)\n%s", ir)
	}

	containerIR, err := compileIR(t, "xs = [1, 2, 3]\nfor v in xs:\n    print(v)\n")
	if err != nil {
		t.Fatalf("the container loop did not compile: %v", err)
	}
	for _, absent := range []string{"__next__", "__iter__", "%__gy_iter", "iter.cond"} {
		if strings.Contains(containerIR, absent) {
			t.Fatalf("a loop over a container grew the protocol's machinery (%q appears) — the container road is untouched by this row\n%s", absent, containerIR)
		}
	}
}

// TestAHungIteratorLoopIsImpossibleByConstruction is the row's watchdog: the only way out of the loop is
// the end signal the iterator raises, so a bounded iterator CANNOT spin. The DoD asks for this assertion
// because a `for` that never ends is the failure mode a protocol loop can invent, and the retired
// interpreter's version of this row printed 7.3 M lines in 15 s before the harness killed it.
func TestAHungIteratorLoopIsImpossibleByConstruction(t *testing.T) {
	t.Parallel()
	src := iteratorClass + "\nn = 0\nfor v in Count():\n    n = n + v\nprint(n)\n"
	done := make(chan string, 1)
	go func() {
		res, err := percentRun(src + "\n")
		if err != nil {
			done <- "error: " + err.Error()
			return
		}
		if res.Code != 0 {
			done <- fmt.Sprintf("trapped %d: %s", res.Code, res.Stderr)
			return
		}
		done <- res.Output
	}()
	select {
	case got := <-done:
		if got != "6\n" {
			t.Fatalf("the bounded loop answered %q, the reference answers 6\n%s", got, src)
		}
	case <-time.After(90 * time.Second):
		t.Fatalf("the protocol loop did not finish inside the watchdog: a loop whose only exit is the iterator's " +
			"StopIteration must terminate for an iterator that raises it (roadmap L12.2, Gap R.54)")
	}
}

// The watchdog above runs the program through percentRun — the same JIT entry the ordinary rows go
// through — from a goroutine that must NOT call t.Fatalf: a compile that hung is reported through the
// channel and failed by the deadline, which is the only way a hang becomes a FAILURE rather than a
// stalled suite.
