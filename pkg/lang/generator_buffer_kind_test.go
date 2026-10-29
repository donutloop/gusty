// pkg/lang/generator_buffer_kind_test.go — ADR 0433 · roadmap Gap R.468: a generator's buffer carries the
// KIND of every element the body yielded, and the doors that receive the buffer ask each slot what it
// holds instead of printing the word underneath the value.
//
// A generator's buffer is a list. It is `rt_alloc`'s HeapKindList, it is walked by `rt_get_elem`, it is
// printed by the list printers — and it is the one list this backend fills one `yield` at a time:
//
//	class C():
//	    def m(self):
//	        yield "a"      # the walk printed 0
//	        yield 2        # the walk printed 2 — the only one that was right
//	        yield None     # the walk printed 0
//	        yield 1.5      # the walk printed 1
//
//	c = C()
//	for v in c.m():
//	    print(v)           # the reference: a / 2 / None / 1.5   this backend, before, at exit 0
//
// `xs.append(1.5)` has never had that hole, because `rt_append` and `rt_tag_elem` are one operation there
// (**ADR 0187**, **ADR 0311**): an append that writes the payload without the tag leaves the slot reading
// back as whatever the previous tenant of that slot was tagged as, so the two must be one instruction. The
// generator road asked neither. It appended the word and let four doors — the walk, the print door, a
// binding, the membership test — guess at what the word meant, which is how a text became the integer 0 and
// a float became the float box's heap slot. **Gap R.468** measured it and named the door the fix belongs to
// by running the SAME four bytes through the module `def`, which printed the same wrong four: the hole was
// the append road's, not the class round's.
//
// One door now writes both lists — `genBufferAppend`, which asks the yielded expression the question
// `xs.append(...)` asks its argument (`elemPayloadAndTag`, the pair the container slots are built from) — and
// three doors read the result: the `for` loop binds payload AND tag per iteration, the print door picks the
// printer that reads a slot's tag, and a `yield from` carries the pair across the copy.
//
// The claim is deliberately EARNED and not universal. A buffer whose every yielded element is written in the
// source as a plain integer keeps the roads it always had: `for v in c.m(): s = s + v` is a sum, and the
// pair road's standing refusal (**Gap R.146**) has no business firing on a program that was adding numbers
// up. The verdict is `generatorBodyWritesKinds`, asked at the body's own door beside the reading
// **ADR 0432** takes — the same walk, the second half of what it knows — and asked of EVERY candidate body
// where the receiver's class is a run-time fact, because the arm that runs is not written in the line.
//
// Rows are judged on BOTH witness legs (**ADR 0302**): this compiler's module through the LLVM JIT, and the
// answer `testdata/interpreter-golden.json` holds for those exact bytes, where a MISSING record fails the
// case. The reference leg — a live CPython beside the linked compiler — is
// `integration/generator_buffer_kind_test.go`. Where the eager model and the reference part company (the
// reference prints `<generator object …>` where **ADR 0081** prints the buffer) the row says so in its
// dossier and the module twin is the second witness instead; where the row refuses, both doors refuse with
// one sentence and the ladder is what proves the refusal is the honest one.

package lang

import (
	"strings"
	"testing"
)

// generatorBufferKindFamily is the answered half. `want` is what a live CPython prints for the source AND
// what this compiler must print; the reference leg re-reads every one of them in
// `integration/generator_buffer_kind_test.go` rather than trusting this file.
func generatorBufferKindFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The row's own program, at the class's door and — byte for byte — at the module's.
		{
			name: "the row's program: a text, an integer, the void and a float",
			src:  "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\n        yield 1.5\nc = C()\nfor v in c.m():\n    print(v)\n",
			want: "a\n2\nNone\n1.5\n",
		},
		{
			name: "the same body at the module door",
			src:  "def g():\n    yield \"a\"\n    yield 2\n    yield None\n    yield 1.5\nfor v in g():\n    print(v)\n",
			want: "a\n2\nNone\n1.5\n",
		},
		// The door that RECEIVES the buffer: a binding, walked later, is the shape a program really writes.
		{
			name: "bound, then walked",
			src:  "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\n        yield 1.5\nc = C()\nys = c.m()\nfor v in ys:\n    print(v)\n",
			want: "a\n2\nNone\n1.5\n",
		},
		// Every kind the untagged word could not carry.
		{
			// The mixed literal, asked a membership question: the payload-only scan used to take the integer 0
			// for the interned index of `"a"` (**Gap R.478**'s literal half). The spelled-literal haystack now
			// asks its slots' (payload, kind) pairs (**ADR 0456**, paying **Gap R.523**), so the answer is the
			// reference's — and the generator row below, which asks the SAME question of a buffer and still
			// prints `True`, is the measurement that says the remaining defect belongs to the buffer's door.
			name: "a mixed literal asked a membership question asks its slots' kinds",
			src:  "print(0 in [\"a\", 2])\n",
			want: "False\n",
		},
		{
			name: "a verdict keeps its word",
			src:  "class C():\n    def m(self):\n        yield True\n        yield False\nc = C()\nfor v in c.m():\n    print(v)\n",
			want: "True\nFalse\n",
		},
		{
			name: "a yielded container prints its brackets",
			src:  "class C():\n    def m(self):\n        yield [1, 2]\n        yield 3\nc = C()\nfor v in c.m():\n    print(v)\n",
			want: "[1, 2]\n3\n",
		},
		{
			name: "a float beside an integer, in either order",
			src:  "class C():\n    def m(self):\n        yield 1.5\n        yield 2\n        yield 2\n        yield 1.5\nc = C()\nfor v in c.m():\n    print(v)\n",
			want: "1.5\n2\n2\n1.5\n",
		},
		{
			name: "a buffer of nothing but texts",
			src:  "class C():\n    def m(self):\n        yield \"hi\"\n        yield \"there\"\nc = C()\nfor v in c.m():\n    print(v)\n",
			want: "hi\nthere\n",
		},
		{
			name: "a buffer of nothing but the void",
			src:  "class C():\n    def m(self):\n        yield None\n        yield None\nc = C()\nfor v in c.m():\n    print(v)\n",
			want: "None\nNone\n",
		},
		{
			name: "an interpolated field yields the text it renders",
			src:  "def g(n):\n    yield f\"n={n}\"\n    yield 7\nfor v in g(3):\n    print(v)\n",
			want: "n=3\n7\n",
		},
		// `yield from` copies a whole object into the buffer; the kind has to cross the copy too.
		{
			name: "yield from a literal that mixes kinds",
			src:  "def g():\n    yield from [1, \"a\", None]\nfor v in g():\n    print(v)\n",
			want: "1\na\nNone\n",
		},
		{
			name: "yield from a literal that mixes kinds, through the class door",
			src:  "class C():\n    def m(self):\n        yield from [True, \"z\", 2.5]\nc = C()\nfor v in c.m():\n    print(v)\n",
			want: "True\nz\n2.5\n",
		},
		// A `yield from` that forwards ANOTHER generator: the claim has to cross the copy, which it does by
		// asking the forwarded body — and the integer twin below is what keeps that question from turning a
		// forwarded sum into **Gap R.146**'s refusal.
		{
			name: "a buffer forwarded from another generator keeps the kinds",
			src:  "def inner():\n    yield \"a\"\n    yield 2\ndef outer():\n    yield from inner()\n    yield None\nfor v in outer():\n    print(v)\n",
			want: "a\n2\nNone\n",
		},
		{
			name: "a buffer forwarded through the class door keeps the kinds",
			src:  "class C():\n    def gen(self):\n        yield \"a\"\n        yield 2\n    def outer(self):\n        yield from self.gen()\n        yield None\nc = C()\nfor v in c.outer():\n    print(v)\n",
			want: "a\n2\nNone\n",
		},
		{
			name: "a forwarded integer buffer still sums",
			src:  "def gen(n):\n    for i in range(n):\n        yield i * i\ndef outer():\n    yield from gen(3)\n    yield 99\ns = 0\nfor x in outer():\n    s = s + x\nprint(s)\n",
			want: "104\n",
		},
		// The membership test reads the same slots; the integer answer it used to guess at is the row's.
		{
			name: "membership finds the yielded text",
			src:  "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\nc = C()\nprint(\"a\" in c.m())\nprint(\"b\" in c.m())\n",
			want: "True\nFalse\n",
		},
		// The four doors **ADR 0432** opened, re-read with kinds in the buffer.
		{
			name: "an inherited generator keeps the kinds",
			src:  "class B():\n    def m(self):\n        yield None\n        yield \"z\"\nclass D(B):\n    pass\nd = D()\nfor v in d.m():\n    print(v)\n",
			want: "None\nz\n",
		},
		{
			name: "super() keeps the kinds",
			src:  "class Base():\n    def gen(self):\n        yield \"b\"\n        yield 1\nclass Child(Base):\n    def drain(self):\n        for v in super().gen():\n            print(v)\n        return None\nc = Child()\nprint(c.drain())\n",
			want: "b\n1\nNone\n",
		},
		{
			name: "a call through self keeps the kinds",
			src:  "class C():\n    def m(self):\n        yield \"s\"\n        yield 1\n    def n(self):\n        for v in self.m():\n            print(v)\n        return None\nc = C()\nprint(c.n())\n",
			want: "s\n1\nNone\n",
		},
		{
			name: "dynamically dispatched, every arm mixed",
			src:  "class B():\n    def m(self):\n        yield \"x\"\n        yield 1\nclass D(B):\n    def m(self):\n        yield \"y\"\n        yield 2\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(D())\n",
			want: "y\n2\n",
		},
		// The controls: what a buffer of nothing but integers does today stays exactly what it did, because
		// the pair road's refusal (Gap R.146) must not reach a program that was adding numbers.
		{
			name: "an integer buffer still SUMS rather than carrying a pair",
			src:  "class C():\n    def m(self):\n        yield 10\n        yield 20\nc = C()\ns = 0\nfor v in c.m():\n    s = s + v\nprint(s)\n",
			want: "30\n",
		},
		{
			name: "an integer buffer walked by a module def still sums",
			src:  "def g():\n    yield 10\n    yield 20\ns = 0\nfor v in g():\n    s = s + v\nprint(s)\n",
			want: "30\n",
		},
		{
			name: "a buffer whose elements come from a parameter keeps its answer",
			src:  "class C():\n    def m(self, k):\n        yield k\n        yield k + 1\nc = C()\nt = 0\nfor k in range(3):\n    for v in c.m(k):\n        t = t + v\nprint(t)\n",
			want: "9\n",
		},
		{
			name: "a buffer of expressions keeps its answer",
			src:  "class C():\n    def m(self, n):\n        for i in range(n):\n            yield i * 2\nc = C()\nfor v in c.m(3):\n    print(v)\n",
			want: "0\n2\n4\n",
		},
	}
}

// TestAYieldedElementAnswersItsOwnKind is the answered family on BOTH legs: the compiled answer through the
// LLVM JIT, and the answer the record holds for the same bytes of source.
func TestAYieldedElementAnswersItsOwnKind(t *testing.T) {
	t.Parallel()
	for _, tc := range generatorBufferKindFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("%s: %v\n%s", tc.name, err, tc.src)
			}
			if res.Code != 0 {
				t.Fatalf("%s: exited %d with stderr %q, want the reference's bytes at exit 0\n%s",
					tc.name, res.Code, res.Stderr, tc.src)
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

// TestTheYieldedKindIsTheSameAnswerAtBothDoors is the ladder **ADR 0232** insists on: the module `def` and
// the method of a class are one body read twice, and the kinds in the buffer cannot be allowed to be right
// at one door and wrong at the other. `generatorMethodFamily` measures the same pair for the buffer's
// EXISTENCE; this measures it for the buffer's SLOTS, over sources chosen so a missing tag shows up as a
// different rendering rather than as a program that prints nothing at all.
func TestTheYieldedKindIsTheSameAnswerAtBothDoors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, module, method string }{
		{
			name:   "a text, an integer, the void and a float",
			module: "def g():\n    yield \"a\"\n    yield 2\n    yield None\n    yield 1.5\nfor v in g():\n    print(v)\n",
			method: "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\n        yield 1.5\nc = C()\nfor v in c.m():\n    print(v)\n",
		},
		{
			name:   "the same, through a binding",
			module: "def g():\n    yield \"a\"\n    yield 2\n    yield None\nxs = g()\nfor v in xs:\n    print(v)\n",
			method: "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\nc = C()\nxs = c.m()\nfor v in xs:\n    print(v)\n",
		},
		{
			name:   "printing the buffer (the eager model, both doors)",
			module: "def g():\n    yield \"a\"\n    yield 2\n    yield None\n    yield 1.5\nprint(g())\n",
			method: "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\n        yield 1.5\nc = C()\nprint(c.m())\n",
		},
		{
			name:   "a verdict",
			module: "def g():\n    yield True\n    yield False\nfor v in g():\n    print(v)\n",
			method: "class C():\n    def m(self):\n        yield True\n        yield False\nc = C()\nfor v in c.m():\n    print(v)\n",
		},
		{
			name:   "a yielded container",
			module: "def g():\n    yield [1, 2]\n    yield 3\nfor v in g():\n    print(v)\n",
			method: "class C():\n    def m(self):\n        yield [1, 2]\n        yield 3\nc = C()\nfor v in c.m():\n    print(v)\n",
		},
		{
			name:   "yield from a literal that mixes kinds",
			module: "def g():\n    yield from [1, \"a\", None]\nfor v in g():\n    print(v)\n",
			method: "class C():\n    def m(self):\n        yield from [1, \"a\", None]\nc = C()\nfor v in c.m():\n    print(v)\n",
		},
		{
			name:   "membership of a yielded text",
			module: "def g():\n    yield \"a\"\n    yield 2\nprint(\"a\" in g())\n",
			method: "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\nc = C()\nprint(\"a\" in c.m())\n",
		},
		{
			name:   "an integer buffer, which stays a sum",
			module: "def g():\n    yield 10\n    yield 20\ns = 0\nfor v in g():\n    s = s + v\nprint(s)\n",
			method: "class C():\n    def m(self):\n        yield 10\n        yield 20\nc = C()\ns = 0\nfor v in c.m():\n    s = s + v\nprint(s)\n",
		},
		{
			// The pair road's standing refusal, asked of BOTH doors: a mixed buffer walked into an arithmetic
			// position is the same shape a mixed LIST literal is, and the two refuse with one sentence.
			name:   "a mixed buffer used as a number refuses like a mixed list",
			module: "def g():\n    yield 1.5\n    yield 2.5\ns = 0\nfor v in g():\n    s = s + v\nprint(s)\n",
			method: "class C():\n    def m(self):\n        yield 1.5\n        yield 2.5\nc = C()\ns = 0\nfor v in c.m():\n    s = s + v\nprint(s)\n",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m, merr := runCompiledBytes(tc.module)
			d, derr := runCompiledBytes(tc.method)
			if (merr == nil) != (derr == nil) {
				t.Fatalf("%s: one door answered and the other refused — module %v / method %v", tc.name, merr, derr)
			}
			if merr != nil {
				if !sameRefusalSentence(merr, derr) {
					t.Fatalf("%s: the two doors refuse with different sentences:\n  module: %v\n  method: %v", tc.name, merr, derr)
				}
				return
			}
			if m != d {
				t.Fatalf("%s: the module door prints %q and the method door prints %q — one buffer, two answers\nmodule:\n%s\nmethod:\n%s",
					tc.name, m, d, tc.module, tc.method)
			}
		})
	}
}

// generatorBufferEager is the pair that the reference cannot witness: printing the generator prints its
// eager buffer (**ADR 0081**, `docs/language.md`), where CPython prints `<generator object g at 0x…>`. The
// rows are pinned on the compiled leg with the two doors as their witnesses, and the reference leg carries
// them as its own never-worse table rather than pretending the two agree.
func TestThePrintedBufferIsTheEagerListItsSlotsHold(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			name: "printing a mixed buffer prints what its slots hold",
			src:  "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\n        yield 1.5\nc = C()\nprint(c.m())\n",
			want: "['a', 2, None, 1.5]\n",
		},
		{
			name: "the module door prints the same list",
			src:  "def g():\n    yield \"a\"\n    yield 2\n    yield None\n    yield 1.5\nprint(g())\n",
			want: "['a', 2, None, 1.5]\n",
		},
		{
			name: "a verdict prints True inside the brackets",
			src:  "def g():\n    yield True\n    yield None\nprint(g())\n",
			want: "[True, None]\n",
		},
		{
			name: "an integer buffer prints the list it always printed",
			src:  "class C():\n    def m(self):\n        yield 1\n        yield 2\nc = C()\nprint(c.m())\n",
			want: "[1, 2]\n",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := runCompiledBytes(tc.src)
			if err != nil {
				t.Fatalf("%s (%s): %v\n%s", tc.name, "ADR 0081's eager model", err, tc.src)
			}
			if out != tc.want {
				t.Fatalf("%s (%s): printed %q, want %q\n%s", tc.name, "ADR 0081's eager model", out, tc.want, tc.src)
			}
		})
	}
}

// TestTheBufferReadingIsTheBodiesOwnQuestion asks `generatorBodyWritesKinds` as a function, beside the
// reading `containsYield`/**ADR 0432** take of the same body: a row is chosen so a wrong verdict shows up as
// a wrong answer rather than as a program that happens to print the right thing either way. `wantKinds` is
// the claim the walk and the print door will act on, so a `true` where the body yields only integers is the
// over-refusal (**Gap R.146** reaching a sum) and a `false` where it yields a text is the wrong answer at
// the exit code of success.
func TestTheBufferReadingIsTheBodiesOwnQuestion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name         string
		src          string
		fromSrc      string
		attributable bool
		wantGen      bool
		wantKinds    bool
	}{
		{name: "an integer body is a buffer with nothing to ask", src: "class C():\n    def m(self):\n        yield 1\n        yield 2\n", wantGen: true, wantKinds: false},
		{name: "a text is a kind the untagged road cannot render", src: "class C():\n    def m(self):\n        yield \"a\"\n", wantGen: true, wantKinds: true},
		{name: "a float is", src: "class C():\n    def m(self):\n        yield 1.5\n", wantGen: true, wantKinds: true},
		{name: "a verdict is", src: "class C():\n    def m(self):\n        yield True\n", wantGen: true, wantKinds: true},
		{name: "the void is", src: "class C():\n    def m(self):\n        yield None\n", wantGen: true, wantKinds: true},
		{name: "a container is", src: "class C():\n    def m(self):\n        yield [1]\n", wantGen: true, wantKinds: true},
		{name: "an interpolation is", src: "class C():\n    def m(self, n):\n        yield f\"{n}\"\n", wantGen: true, wantKinds: true},
		{name: "a name says nothing about its kind", src: "class C():\n    def m(self, k):\n        yield k\n", wantGen: true, wantKinds: false},
		{name: "a sum says nothing about its kind", src: "class C():\n    def m(self, k):\n        yield k * 2\n", wantGen: true, wantKinds: false},
		{name: "a call says nothing about its kind", src: "class C():\n    def m(self):\n        yield len2()\n", wantGen: true, wantKinds: false},
		{
			name:      "one mixed yield among integers is enough",
			src:       "class C():\n    def m(self, k):\n        yield 1\n        yield 2\n        yield \"a\"\n        yield k\n",
			wantGen:   true,
			wantKinds: true,
		},
		{
			name:      "the reading walks an if, a while, a for and a match arm",
			src:       "class C():\n    def m(self, k):\n        if k:\n            yield \"a\"\n        while k:\n            yield 1\n        for i in range(2):\n            yield 2\n        match k:\n            case 1:\n                yield 3\n",
			wantGen:   true,
			wantKinds: true,
		},
		{
			// A nested `def` fills ITS OWN buffer: the reading `containsYield` descends into (Gap R.457's
			// standing parity item) must not make this body answer for a kind it never yields.
			name:      "a nested def's yields are not this buffer's",
			src:       "class C():\n    def m(self):\n        yield 1\n        def inner():\n            yield \"a\"\n        return None\n",
			wantGen:   true,
			wantKinds: false,
		},
		{
			// The source of a `yield from` is asked, not assumed: a parameter has no classification this pass can
			// read (**Gap R.475**'s missing word), and claiming kinds for it would hand the loop a pair for a
			// buffer whose slots the copy could not have tagged anyway.
			name:      "a yield from of a source the pass cannot attribute earns nothing",
			src:       "class C():\n    def m(self, src):\n        yield from src\n",
			wantGen:   true,
			wantKinds: false,
		},
		{
			// The claim crosses into a body it can read: `yield from gen()` earns what `gen`'s own body earns,
			// which is the difference between this row answering a forwarded buffer and refusing the sum the
			// record holds an answer for. `fromSrc` is handed to the resolver the walk asks.
			name:         "a yield from a generator that yields a text earns its kinds",
			src:          "class C():\n    def m(self):\n        yield from gen()\n",
			fromSrc:      "def gen():\n    yield 1\n    yield \"a\"\n",
			attributable: true,
			wantGen:      true,
			wantKinds:    true,
		},
		{
			// The record's own program, measured: `def gen(n): yield i * i` under `def outer(): yield from gen(3)`
			// summed to `104` on the build this row opens, and a blanket claim here spent **Gap R.146**'s refusal
			// on that sum. The forwarded buffer is a sum, so the walk must not be handed a pair.
			name:         "a yield from a generator that yields only integers earns nothing",
			src:          "class C():\n    def m(self):\n        yield from gen()\n",
			fromSrc:      "def gen():\n    yield 1\n    yield 2\n",
			attributable: true,
			wantGen:      true,
			wantKinds:    false,
		},
		{
			// Every arm of an attributed source has to earn it, for the reason `markGenBuffer` gives for a joined
			// register: the door holds one value and the arm that produced it is a run-time fact.
			name:         "one arm that yields only integers holds the whole source down",
			src:          "class C():\n    def m(self):\n        yield from pick()\n",
			fromSrc:      "def pick():\n    return [1, 2]\n",
			attributable: true,
			wantGen:      true,
			wantKinds:    false,
		},
		{
			// The self-copy has no body to inherit from, and a walk that followed it would not stop.
			name:      "a generator that copies itself earns nothing and does not spin",
			src:       "def m(n):\n    if n:\n        yield from m(n)\n        yield 1\n",
			wantGen:   true,
			wantKinds: false,
		},
		{
			name:      "a yield from a literal of nothing but integers does not",
			src:       "class C():\n    def m(self):\n        yield from [1, 2]\n",
			wantGen:   true,
			wantKinds: false,
		},
		{name: "a body that never yields is neither", src: "class C():\n    def m(self):\n        return 1\n", wantGen: false, wantKinds: false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("%s: %v\n%s", tc.name, err, tc.src)
			}
			var fd *FuncDef
			for _, st := range prog.Stmts {
				if f, ok := st.(*FuncDef); ok && f.Name == "m" {
					fd = f
				}
				if cd, ok := st.(*ClassDef); ok {
					for _, bs := range cd.Body {
						if f, ok2 := bs.(*FuncDef); ok2 && f.Name == "m" {
							fd = f
						}
					}
				}
			}
			if fd == nil {
				t.Fatalf("%s: no body to read\n%s", tc.name, tc.src)
			}
			if got := methodIsGenerator(fd); got != tc.wantGen {
				t.Fatalf("%s: methodIsGenerator answered %v, want %v\n%s", tc.name, got, tc.wantGen, tc.src)
			}
			// The `yield from` question is asked of a resolver, so the row can hand the walk the body the source
			// names — which is what the compiler asks of `genBodies` at the call site — or leave it unattributed.
			var resolve func(Expr) ([][]Stmt, bool)
			if tc.attributable {
				fromProg, err := Parse(tc.fromSrc)
				if err != nil {
					t.Fatalf("%s: the source this row hands the resolver does not parse: %v\n%s", tc.name, err, tc.fromSrc)
				}
				var callee []Stmt
				for _, st := range fromProg.Stmts {
					if f, ok := st.(*FuncDef); ok {
						callee = f.Body
					}
				}
				if callee == nil {
					t.Fatalf("%s: the source this row hands the resolver declares no function\n%s", tc.name, tc.fromSrc)
				}
				resolve = func(Expr) ([][]Stmt, bool) { return [][]Stmt{callee}, true }
			}
			if got := generatorBodyWritesKinds(fd.Body, resolve); got != tc.wantKinds {
				t.Fatalf("%s: generatorBodyWritesKinds answered %v, want %v\n%s", tc.name, got, tc.wantKinds, tc.src)
			}
			// The claim can only ever be narrower than the reading: a body the reading does not call a
			// generator has no buffer for anyone to ask about.
			if tc.wantKinds && !tc.wantGen {
				t.Fatalf("%s: the two readings contradict each other\n%s", tc.name, tc.src)
			}
		})
	}
}

// TestTheYieldWritesPayloadAndTagTogether reads the machine path: the emitted method has to show the buffer
// being filled by the door that writes the kind with the value, and — the half that keeps this from being a
// blanket change — the walk of an INTEGER buffer must not grow the tag read that a mixed one needs.
func TestTheYieldWritesPayloadAndTagTogether(t *testing.T) {
	t.Parallel()
	mixed, err := Compile("class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\nc = C()\nfor v in c.m():\n    print(v)\n")
	if err != nil {
		t.Fatalf("the mixed program refused: %v", err)
	}
	body := generatorMethodDefine(t, mixed.IR, "gy_C_m")
	for _, want := range []string{
		// Each yield writes its payload AND its kind: the tag is the element's, not the previous tenant's.
		"call void @rt_append_tagged(i32 %gh1, i32 %",
		"ret i32 %gh1",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("the emitted method does not carry %q:\n%s", want, body)
		}
	}
	if n := strings.Count(body, "call void @rt_append_tagged(i32 %gh1,"); n != 3 {
		t.Fatalf("the body yields three times, so the tagged door is asked three times — the define holds %d:\n%s", n, body)
	}
	if strings.Contains(body, "call void @rt_append(i32 %gh1,") {
		t.Fatalf("a yield still goes through the untagged door, which leaves the slot carrying whoever held it last:\n%s", body)
	}

	// The walk reads the tag it was handed — per iteration, beside the payload. The question is asked of
	// the program's own `define`s: the module carries the runtime's helpers whatever the program asks, and
	// a helper that reads a slot's tag is not this program reading one.
	if !strings.Contains(generatorModuleBody(mixed.IR), "call i32 @rt_tag_of(") {
		t.Fatalf("a walk over a mixed buffer never asks a slot what it holds; the loop variable would be one word:\n%s",
			generatorModuleBody(mixed.IR))
	}

	plain, err := Compile("class C():\n    def m(self):\n        yield 1\n        yield 2\nc = C()\nfor v in c.m():\n    print(v)\n")
	if err != nil {
		t.Fatalf("the integer program refused: %v", err)
	}
	if strings.Contains(generatorModuleBody(plain.IR), "call i32 @rt_tag_of(") {
		t.Fatalf("a walk over a buffer of nothing but integers was handed the pair door — the sum becomes a refusal "+
			"the program never asked for (Gap R.146):\n%s", generatorModuleBody(plain.IR))
	}
	// The negative control the reading owes: a method that never yields allocates no buffer at all.
	bare, err := Compile("class C():\n    def m(self):\n        return 1\nc = C()\nprint(c.m())\n")
	if err != nil {
		t.Fatalf("a plain method refused: %v", err)
	}
	bareBody := generatorMethodDefine(t, bare.IR, "gy_C_m")
	for _, forbidden := range []string{"rt_append", "%gh"} {
		if strings.Contains(bareBody, forbidden) {
			t.Fatalf("a method that never yields carries %q in its define — the reading is the body's:\n%s", forbidden, bareBody)
		}
	}
}

// TestWhatAYieldedKindStillCannotReachIsFiledNotFixed pins what this row leaves owed, with today's bytes
// beside the reference's. Every one is byte-identical on this row's parent, measured with a `git worktree`
// build of it — the sentence that makes them other rows' lesions rather than this commit's damage. An answer
// that arrives here FAILS the case, because a pin that starts passing is a row that was closed without
// deleting its pin.
func TestWhatAYieldedKindStillCannotReachIsFiledNotFixed(t *testing.T) {
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
			// The door this row minted is `genBuffers`, and two roads still ask the OLD table: the
			// comprehension classifies the iterable out of `mixedLists`, and the rendering road out of
			// `listOperands`. Both therefore read the buffer as an untagged list.
			name:     "a comprehension over a bound buffer reads the word, not the value",
			src:      "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\nc = C()\nys = c.m()\nprint([v for v in ys])\n",
			wantExit: 0,
			wantOut:  "[0, 2, 0]\n",
			dossier:  "**Gap R.477** — the reference prints `['a', 2, None]`, which is what `print([v for v in xs])` over the same THREE VALUES written as a list prints on this build; `codegen_comp.go`'s iterable classification and `render.go`'s container arm ask `mixedLists`/`listOperands` and never the buffer claim this row minted",
		},
		{
			name:     "str() of a buffer renders the word, not the value",
			src:      "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\nc = C()\nprint(str(c.m()))\n",
			wantExit: 0,
			wantOut:  "[0, 2, 0]\n",
			dossier:  "**Gap R.477**'s rendering arm — `render.go`'s `listOperands` door, the same missing claim one door later. The reference prints `<generator object C.m at 0x…>` here, so the ladder is THIS backend's own model: `print(str([\"a\", 2, None]))` prints `['a', 2, None]` on this build, and the buffer handed to the same renderer prints the words under its slots; **ADR 0303**'s rule is that a value has ONE printer and this register is being handed two",
		},
		{
			// The same register one door further along: the interpolation engine asks the value road, which
			// asks `listOperands` and takes the one word it finds there.
			name:     "an f-string field of a buffer prints the word the register holds",
			src:      "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\n        yield None\nc = C()\nprint(f\"{c.m()}\")\n",
			wantExit: 0,
			wantOut:  "1\n",
			dossier:  "**Gap R.477**'s interpolation arm — the reference prints `<generator object C.m at 0x…>` and this backend's own model owes `['a', 2, None]`, which is what `xs = [\"a\", 2, None]` / `print(f\"{xs}\")` prints on this build and on the reference alike; the field prints **`1`**, one word read out of the register with no tag asked beside it (**ADR 0303**'s one printer, **ADR 0185**'s payload-without-its-tag)",
		},
		{
			// The membership test compares the payload word; the tag is what tells an interned index from
			// the integer that happens to have the same number. The literal twin NOW answers `False` — the door
			// that asks a spelled list literal its slots' (payload, kind) pairs was landed for **Gap R.523** by
			// **ADR 0456** — which is what makes this row's claim about the DOOR rather than about membership:
			// the same question at the same exit code, one road apart, answers right at the literal and wrong at
			// the buffer, and the buffer built that road itself.
			name:     "membership asks an untagged question and takes an integer for a text",
			src:      "class C():\n    def m(self):\n        yield \"a\"\n        yield 2\nc = C()\nprint(0 in c.m())\n",
			wantExit: 0,
			wantOut:  "True\n",
			dossier:  "**Gap R.478** — the reference prints `False`: `\"a\"` interns to slot 0 and the membership scan compares the payload against the integer 0 with nobody asking the tag beside it; `rt_slot_eq`/the tagged compare the container roads use is the door, and `\"a\" in c.m()` already answers `True` correctly on this build. The literal twin (`print(0 in [\"a\", 2])`) printed the same wrong `True` until **ADR 0456** gave the spelled-literal haystack the tagged scan; it is in the answered family now, and the pair of rows is what localises this one to the buffer's own door",
		},
		{
			// The join **ADR 0432** refused for a SPLIT yield census (**Gap R.467**) has the same hole one
			// level down: every arm yields, so the join is a buffer, but the arms disagree about the KINDS
			// and the claim is asked of all of them conservatively.
			name:     "a dynamic join whose arms disagree about kinds still reads words",
			src:      "class B():\n    def m(self):\n        yield \"x\"\n        yield 1\nclass D(B):\n    def m(self):\n        yield 3\n        yield 4\ndef show(o):\n    for v in o.m():\n        print(v)\nshow(B())\nshow(D())\n",
			wantExit: 0,
			wantOut:  "0\n1\n3\n4\n",
			dossier:  "**Gap R.467**/**Gap R.471**'s tag beside the joined register — the reference prints `x 1 3 4`; the arm that RUNS is a run-time fact, and this row claims the kinds only where every arm earned them, which is why `show(D())` alone prints `y 2` in the answered family",
		},
		{
			// **Gap R.475**'s missing kind word, seen from the buffer's side: a parameter the keyed-call pass
			// never classified is no kind, so the copy cannot ask the object for one.
			name:     "a yield from a parameter's list copies words, not values",
			src:      "xs = [1, \"a\", None]\ndef g(src):\n    yield from src\nfor v in g(xs):\n    print(v)\n",
			wantExit: 0,
			wantOut:  "1\n0\n0\n",
			dossier:  "**Gap R.475** (the parameter's kind) reached the copy — the reference prints `1 a None`, and `yield from [1, \"a\", None]` written as a LITERAL prints those bytes today; the source's slots are tagged, the loop cannot ask them because the parameter has no classification to read",
		},
		{
			// The pair road's standing refusal (**Gap R.146**), pinned so the day it opens, the opening is a
			// decision with a row and not a drift. Pinned beside the mixed-LIST twin, which refuses the same
			// sentence: this row did not create the refusal, it stopped the generator from answering the
			// shape with a truncated number instead of asking.
			name:       "a mixed buffer used as a number is the pair road's standing refusal",
			src:        "class C():\n    def m(self):\n        yield 1.5\n        yield 2.5\nc = C()\ns = 0\nfor v in c.m():\n    s = s + v\nprint(s)\n",
			wantExit:   1,
			wantStderr: "travels as a (payload, tag) pair",
			dossier:    "**Gap R.146**/**L11.1** — the reference prints `4.0`; `xs = [1.5, 2.5]` walked into the same sum refuses with these SAME bytes on a program with no generator in it, and pinned as this row's OWN change of answer: the parent printed `3` at the exit code of success — the two float boxes' heap slots added together — where the reference prints `4.0`, and the ladder beside it (`xs = [1.5, 2.5]` walked into the same sum) refuses with these SAME bytes on a program with no generator in it, which is what makes the refusal the honest answer rather than this row's own refusal",
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

// generatorModuleBody cuts the program's own `define`s out of an emitted module — `@main` and the
// generators it calls — so a machine-path row asserts what THIS program asked and not what the runtime
// block every module carries happens to contain.
func generatorModuleBody(ir string) string {
	var out []string
	for _, sym := range []string{"@main(", "@gy_C_m("} {
		at := strings.Index(ir, "define i32 "+sym)
		if at < 0 {
			continue
		}
		rest := ir[at:]
		if end := strings.Index(rest, "\n}\n"); end >= 0 {
			out = append(out, rest[:end])
		}
	}
	return strings.Join(out, "\n")
}
