package integration

import (
	"strings"
	"testing"
)

// The CLI leg of roadmap **Gap R.590**, **Gap R.591**, **Gap R.592** and **Gap R.593** — the four callable
// shapes the compiled backend refuses while `python3` prints a plain number (L11.7's remaining refusals).
//
// The unit leg (`pkg/lang/callable_value_shapes_test.go`) pins the sentence; this leg pins the REFERENCE's
// answer beside it, so a row cannot be filed against a program nobody runs and cannot be closed by a build
// that answers something else. Every `python` cell below was read off a live `python3` run of the same
// source, and the test re-derives it: a row whose reference cell has gone stale fails before the compiled
// side is even asked.
//
// All fourteen are exit 1 — the code this contract gives the COMPILER for "I have no answer" (ADR 0006).
// Exit 2 is the compiler's own bug and is forbidden on every row (ADR 0166); exit 3 would mean the program
// raised, and none of these programs raises in the reference.

type callableValueRow struct {
	name   string
	src    string
	python string
	marker string
	row    string
	why    string
}

func callableValueRows() []callableValueRow {
	return []callableValueRow{
		// ---- Gap R.590 — a callable returned from a function ------------------------------------------
		{
			name:   "a returned lambda, bound and called",
			src:    "def make():\n    return lambda x: x * 2\n\nf = make()\nprint(f(3))\n",
			python: "6\n",
			marker: "a lambda used as a value has no compiled representation",
			row:    "Gap R.590",
			why:    "the callable has no value to be: it is declared, and no call site ever bound it",
		},
		{
			name:   "a returned lambda that reads its frame",
			src:    "def make(k):\n    return lambda x: x * k\n\nprint(make(3)(2))\n",
			python: "6\n",
			marker: "a lambda used as a value has no compiled representation",
			row:    "Gap R.590",
			why:    "the capture has to leave the frame with the callable, and the return has no word for it",
		},
		{
			name:   "a declared function returned by a function",
			src:    "def add1(x):\n    return x + 1\n\ndef idf(f):\n    return f\n\nprint(idf(add1)(2))\n",
			python: "3\n",
			marker: "is called but is not a function this backend can build a call to",
			row:    "Gap R.590",
			why:    "the callee is a call's answer, and the call door builds a call only to a define it can name",
		},
		{
			name:   "a conditional over two declared functions",
			src:    "def add1(x):\n    return x + 1\n\ndef mul2(x):\n    return x * 2\n\ndef pick(n):\n    return add1 if n else mul2\n\nprint(pick(1)(3))\n",
			python: "4\n",
			marker: "names function, which the compiled backend has no value for",
			row:    "Gap R.590",
			why:    "the arm is a callable, so the ternary needs the pair ADR 0475 gave a container answer",
		},
		// ---- Gap R.591 — a callable held in a container -------------------------------------------------
		{
			name:   "a lambda in a list, called by index",
			src:    "ops = [lambda x: x + 1]\nprint(ops[0](1))\n",
			python: "2\n",
			marker: "a lambda used as a value has no compiled representation",
			row:    "Gap R.591",
			why:    "a container slot holds int64, float or the tagged value; a callable has nowhere to be stored",
		},
		{
			name:   "a declared function in a list",
			src:    "def add1(x):\n    return x + 1\n\nops = [add1]\nprint(ops[0](1))\n",
			python: "2\n",
			marker: "names function, which the compiled backend has no value for",
			row:    "Gap R.591",
			why:    "the pass sees a function reaching a container and withdraws the whole program",
		},
		{
			name:   "a declared function as a dict value",
			src:    "def add1(x):\n    return x + 1\n\ntbl = {\"inc\": add1}\nprint(tbl[\"inc\"](1))\n",
			python: "2\n",
			marker: "names function, which the compiled backend has no value for",
			row:    "Gap R.591",
			why:    "the dict side of the same slot, where the tag that would name the kind lives",
		},
		{
			name:   "the same list read twice",
			src:    "def add1(x):\n    return x + 1\n\nxs = [add1, add1]\nprint(xs[0](1) + xs[1](2))\n",
			python: "5\n",
			marker: "names function, which the compiled backend has no value for",
			row:    "Gap R.591",
			why:    "which define a read names is a fact about the element, not about the call site",
		},
		// ---- Gap R.592 — a callable held in a field ------------------------------------------------------
		{
			name:   "a lambda stored in a field and called",
			src:    "class C:\n    def __init__(self):\n        self.f = lambda x: x + 1\n\nc = C()\nprint(c.f(1))\n",
			python: "2\n",
			marker: "on a receiver that is not a text the compiler can read",
			row:    "Gap R.592",
			why:    "the ATTRIBUTE road answers first (Gap I.2) and its sentence is about the receiver, not the callable",
		},
		{
			name:   "a declared function stored in a field and called",
			src:    "def add1(x):\n    return x + 1\n\nclass C:\n    def __init__(self):\n        self.f = add1\n\nc = C()\nprint(c.f(1))\n",
			python: "2\n",
			marker: "on a receiver that is not a text the compiler can read",
			row:    "Gap R.592",
			why:    "same misattribution: the store and the call are two doors, and the reader is told about neither",
		},
		// ---- Gap R.593 — a callable parameter forwarded ---------------------------------------------------
		{
			name:   "a parameter forwarded one level",
			src:    "def double(x):\n    return x * 2\n\ndef use(f, x):\n    return f(x)\n\ndef outer(g, y):\n    return use(g, y)\n\nprint(outer(double, 3))\n",
			python: "6\n",
			marker: "is not a declared function, so it has no name to substitute",
			row:    "Gap R.593",
			why:    "inside the clone minted for `double`, `g` IS `double`; the entry resolver asks for a declared name",
		},
		{
			name:   "a parameter forwarded two levels",
			src:    "def add1(x):\n    return x + 1\n\ndef forward(f, v):\n    return f(v)\n\ndef call_it(g, w):\n    return forward(g, w)\n\nprint(call_it(add1, 4))\n",
			python: "5\n",
			marker: "is not a declared function, so it has no name to substitute",
			row:    "Gap R.593",
			why:    "the inner function's entry tuple is derived from another clone, and sites are read from the source",
		},
		{
			name:   "one parameter forwarded to two calls",
			src:    "def add1(x):\n    return x + 1\n\ndef use(f, x):\n    return f(x)\n\ndef twice(g):\n    return use(g, 1) + use(g, 2)\n\nprint(twice(add1))\n",
			python: "5\n",
			marker: "is not a declared function, so it has no name to substitute",
			row:    "Gap R.593",
			why:    "the rewrite has to be keyed per call rank, which is what the recursion road already does",
		},
	}
}

// TestTheCallableValueRowsAnswerInTheReferenceButNotHere is the reference leg: the row's program prints a
// plain number under `python3`, and this backend refuses it at exit 1 with the clause the row names.
func TestTheCallableValueRowsAnswerInTheReferenceButNotHere(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, r := range callableValueRows() {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if want != r.python {
				t.Fatalf("%s: the row is stale — python3 prints %q, the row pins %q\n%s", r.row, want, r.python, r.src)
			}
			p := writeSrc(t, dir, "callable_value_debt", r.src)
			out, code := cliRunMerged(t, "--aot", "--file", p)
			if code == 2 {
				t.Fatalf("%s: exit 2 — the compiler's own bug (ADR 0166), not a refusal:\n%s", r.row, out)
			}
			if code == 3 {
				t.Fatalf("%s: the program raised (%s); the reference runs it to the end and prints %q", r.row, firstLine(out), want)
			}
			if code == 0 {
				t.Fatalf("%s: the door opened — this shape now compiles and prints %q, so delete the row and close %s's half that names it",
					r.row, out, r.row)
			}
			if code != 1 {
				t.Fatalf("%s: exit %d is not in the contract for a refusal (ADR 0006): %s", r.row, code, out)
			}
			if !strings.Contains(out, r.marker) {
				t.Fatalf("%s: the refusal does not say %q — %s\nfull output:\n%s", r.row, r.marker, r.why, out)
			}
		})
	}
}

// TestTheCallableValueControlsStillAnswerThroughTheCLI is the wall beside the door: the higher-order shapes
// the defunctionalisation pass already rewrites must keep answering, through the same CLI, at the same
// numbers. A change to the pass that costs one of these is a regression, not a widening (ADR 0273).
func TestTheCallableValueControlsStillAnswerThroughTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, r := range []struct {
		name string
		src  string
		want string
	}{
		{
			name: "a callable argument, called",
			src:  "def add1(x):\n    return x + 1\n\ndef apply_(f, v):\n    return f(v)\n\nprint(apply_(add1, 1))\n",
			want: "2\n",
		},
		{
			name: "a lambda argument, called",
			src:  "def use(f, x):\n    return f(x)\n\nprint(use(lambda v: v * 7, 3))\n",
			want: "21\n",
		},
		{
			name: "a callable parameter called twice",
			src:  "def add1(x):\n    return x + 1\n\ndef twice(f, v):\n    return f(f(v))\n\nprint(twice(add1, 5))\n",
			want: "7\n",
		},
		{
			name: "a builtin that takes a callable and calls it",
			src:  "def add1(x):\n    return x + 1\n\nprint(sorted([3, 1, 2], key=lambda v: 0 - v))\n",
			want: "[3, 2, 1]\n",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if want != r.want {
				t.Fatalf("the row's reference answer is stale: python3 prints %q, the row pins %q", want, r.want)
			}
			p := writeSrc(t, dir, "callable_control", r.src)
			out, code := cliRunMerged(t, "--aot", "--file", p)
			if code != 0 || out != r.want {
				t.Fatalf("compiled exit %d, prints %q, want %q (this shape answers today — L11.7's paid half)\n%s",
					code, out, r.want, out)
			}
		})
	}
}

// TestWhatTheCallableValueDoorsStillCannotAnswerThroughTheCLI is the residue table's reference leg: the
// shapes measured, decided unreachable this round, and filed beside the four rows rather than absorbed into
// them. Two of them print an ADDRESS under the reference — `<function add1 at 0x7f…>` — which is why no
// conformance program on this list prints a callable: a program whose expected output contains an address can
// be asserted only by prefix, and a parity row that cannot be byte-exact is a row that will rot. The rows
// above are closeable by answering a number; a print-of-a-callable is a separate debt and is filed as one.
func TestWhatTheCallableValueDoorsStillCannotAnswerThroughTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, r := range []struct {
		name    string
		src     string
		want    string // the reference's whole answer, when it is one
		prefix  string // the reference's answer up to the address, when it is not
		marker  string
		rowName string
	}{
		{
			name:    "a callable in a ternary arm beside an int",
			src:     "def add1(x):\n    return x + 1\n\ndef pick(n):\n    return add1 if n else 0\n\nprint(pick(1))\n",
			prefix:  "<function add1 at 0x",
			marker:  "names function, which the compiled backend has no value for",
			rowName: "Gap R.590",
		},
		{
			name:    "print of a callable",
			src:     "def add1(x):\n    return x + 1\n\nprint(add1)\n",
			prefix:  "<function add1 at 0x",
			marker:  "names function, which the compiled backend has no value for",
			rowName: "the print-of-a-callable debt named beside Gap R.590",
		},
		{
			name:    "a callable compared to itself",
			src:     "def add1(x):\n    return x + 1\n\nprint(add1 == add1)\n",
			want:    "True\n",
			marker:  "names function, which the compiled backend has no value for",
			rowName: "Gap R.591's identity half",
		},
		{
			name:    "a callable read out of a container a call answers",
			src:     "def add1(x):\n    return x + 1\n\ndef ops():\n    return [add1]\n\nprint(ops()[0](1))\n",
			want:    "2\n",
			marker:  "names function, which the compiled backend has no value for",
			rowName: "Gap R.591 beside Gap R.67",
		},
	} {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			out, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if r.want != "" && out != r.want {
				t.Fatalf("the row is stale: python3 prints %q, the row pins %q\n%s", out, r.want, r.src)
			}
			if r.prefix != "" && !strings.HasPrefix(out, r.prefix) {
				t.Fatalf("the row is stale: python3 prints %q, which does not start with the address-shaped %q", out, r.prefix)
			}
			p := writeSrc(t, dir, "callable_residue", r.src)
			got, code := cliRunMerged(t, "--aot", "--file", p)
			if code == 2 {
				t.Fatalf("%s: exit 2 is the compiler's own bug (ADR 0166):\n%s", r.rowName, got)
			}
			if code == 0 {
				t.Fatalf("%s: this residue shape now answers %q — it is no longer residue; give it a row of its own and delete the pin", r.rowName, got)
			}
			if code != 1 {
				t.Fatalf("%s: exit %d is not a refusal (ADR 0006): %s", r.rowName, code, got)
			}
			if !strings.Contains(got, r.marker) {
				t.Fatalf("%s: the filed refusal moved, expected %q\n%s", r.rowName, r.marker, got)
			}
		})
	}
}
