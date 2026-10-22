package integration

// integration/forwarded_word_test.go — the reference leg for the word a clone forwards to its entry, in the
// shape that used to be refused (roadmap L11.7 · Gap R.375; ADR 0397).
//
// `pkg/lang/forwarded_word_test.go` decides the SHAPE: which words the pass mints, that they cannot be
// spelled in the source, that the declaration's three lists stay parallel, and what the emitted module
// contains. This file asks the question only a run can answer: does the compiled program print what `python3`
// prints, on both of its roads — the in-process JIT and the linked binary — and does every module the row
// produces stay LLVM the toolchain accepts?
//
// Every expectation is read off the reference at run time by `cpythonOut`; a stale literal copied out of a
// previous build is not evidence about a language (AGENTS.md's two legs — the record and the reference, never
// two engines). Exit 2 is forbidden throughout: ADR 0166 reserves it for a module the compiler wrote and LLVM
// refused, and a row whose programs are refused in words must reach the reader at exit 1.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// forwardedWordCLIPrograms is the answered family: the nine sources of the package table's
// forwardedWordCollisionPrograms, in that order, followed by the five answered shapes this leg measures that the
// package keeps in lambda_applied_test.go's answering table. The two files read as one experiment — the same
// programs, one table read by the compiler API, one read through the CLI.
var forwardedWordCLIPrograms = []struct{ name, src string }{
	{"the body binds the name the entry reads", "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n"},
	{"the name is one of the callee's own parameters", "def apply1(fn, v, base):\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed, 9)\n\nprint(use(3))\n"},
	{"the body owns the name as a loop target", "def apply1(fn, v):\n    for base in [7, 8]:\n        pass\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n"},
	{"the body owns one of the two names the entry reads", "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    off = 10\n    return apply1(lambda x: x * base + off, seed)\n\nprint(use(3))\n"},
	{"two entries read the one word the body owns", "def both(f, g, v):\n    base = 100\n    return f(v) + g(v)\n\ndef use(seed):\n    base = 2\n    return both(lambda x: x + base, lambda x: x * base, seed)\n\nprint(use(3))\n"},
	{"three entries over two words the body owns", "def third(f, g, h, v):\n    base = 5\n    off = 6\n    return f(v) + g(v) + h(v)\n\ndef use(seed):\n    base = 2\n    off = 3\n    return third(lambda x: x + base, lambda x: x * base, lambda x: x + off, seed)\n\nprint(use(3))\n"},
	{"the callee calls its parameter twice under the collision", "def twice(fn, seed):\n    base = 0\n    return fn(fn(seed))\n\ndef use(seed):\n    base = 1\n    return twice(lambda x: x * 2 + base, seed)\n\nprint(use(2))\n"},
	{"a higher-order recursion forwards the word down", "def run(n, f):\n    base = 100\n    return 0 if n == 0 else f(n) + run(n - 1, f)\n\ndef use():\n    base = 1\n    return run(3, lambda x: x + base)\n\nprint(use())\n"},
	{"two hosts of one recursive callee", "def run(n, f):\n    base = 100\n    return 0 if n == 0 else f(n) + run(n - 1, f)\n\ndef use_a():\n    base = 1\n    return run(3, lambda x: x + base)\n\ndef use_b():\n    base = 10\n    return run(2, lambda x: x + base)\n\nprint(run(0, lambda x: x) + use_a() * 100 + use_b())\n"},
	{"the body owns the name through a comprehension target", "def apply1(fn, v):\n    base = [base for base in [9]]\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n"},
	{"the body reads its own word and the entry reads the host's", "def apply1(fn, v):\n    base = 9\n    return fn(v) + base\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n"},
	{"an omitted argument beside a forwarded word", "def apply1(fn, v=3):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base)\n\nprint(use(4))\n"},
	{"a capturing clone and a closed clone of one callee", "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed) + apply1(lambda x: x + 1, seed)\n\nprint(use(3))\n"},
	{"a body capture and an entry word spelled the same", "def stepper():\n    total = 0\n    for step in [1, 2, 3]:\n        shift = step * 10\n        total = total + (lambda fn: fn(2) + shift + step)(lambda n: n * step)\n    return total\n\nprint(stepper())\n"},
}

// TestWhatAForwardedWordPrintsWhatCPythonPrints is the reference leg on both compiled roads.
func TestWhatAForwardedWordPrintsWhatCPythonPrints(t *testing.T) {
	t.Parallel()
	for _, tc := range forwardedWordCLIPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "forwarded_word.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			for _, road := range []string{"--jit", "--aot"} {
				out, code := cliRunCode(t, road, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / the exit-code contract):\n%s", road, cliRun(t, road, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d: %s", road, code, cliRun(t, road, path))
				}
				if out != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", road, out, want, tc.src)
				}
			}
		})
	}
}

// TestTheForwardedWordProgramsAreOnTheRecord is ADR 0302's rule at this altitude: every program this row
// answers has an answer on record, written from the reference by tools/recmerge and cross-checked against the
// compiled leg — never written by this backend. A row whose record disappeared fails here.
func TestTheForwardedWordProgramsAreOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range forwardedWordCLIPrograms {
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one forwarded-word program is on the record: the record leg is not being walked")
	}
}

// TestWhatTheForwardingGateStillRefusesAtTheCLI holds the shapes the minting does NOT reach to ADR 0166 and
// ADR 0249: exit 1, never exit 2, and a sentence that names the kind the value really has. Each row owns the
// name twice — as a number in the higher-order body, where a table read of the CLONE would have said the
// value travels, and as the kind that cannot travel in the HOST, which owns it. A refusal that read the
// clone's tables would compile these programs clean and print a handle.
func TestWhatTheForwardingGateStillRefusesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		src    string
		phrase string
		owns   string
	}{
		{
			"a text the host owns while the body holds a number",
			"def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = \"hey\"\n    return apply1(lambda x: x + len(base), seed)\n\nprint(use(3))\n",
			"holds a text", "L11.1",
		},
		{
			"a float the host owns while the body holds a number",
			"def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1.5\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n",
			"holds a float", "L11.1",
		},
		{
			"a container the host owns while the body holds a number",
			"def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = [1, 2]\n    return apply1(lambda x: x + len(base), seed)\n\nprint(use(3))\n",
			"holds a container", "L11.1",
		},
		{
			"the void the host owns while the body holds a number",
			"def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = None\n    return apply1(lambda x: x + len([base]), seed)\n\nprint(use(3))\n",
			"holds the void value", "L11.1",
		},
		{
			// The read the entry road has never owned, and still declines: a name past the frame the
			// application is written in belongs to ADR 0245's nested-`def` lift, not to this pass. Minting a
			// word for the frame the clone CAN see did not widen this.
			"an entry that reads a frame two steps out",
			"def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef outer():\n    base = 2\n    def use(seed):\n        return apply1(lambda x: x + base, seed)\n    return use(3)\n\nprint(outer())\n",
			"an entry is a bare symbol", "Gap R.336",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "forwarded_word_refusal.gy", tc.src)
			for _, road := range []string{"--jit", "--aot"} {
				_, code := cliRunCode(t, road, path)
				out := cliRun(t, road, path)
				if code == 2 {
					t.Fatalf("%s reported a compiler bug (exit 2) for a shape the row refuses in words:\n%s", road, out)
				}
				if code != 1 {
					t.Fatalf("%s exited %d, want the refusal class 1:\n%s", road, code, out)
				}
				if !strings.Contains(out, tc.phrase) {
					t.Fatalf("%s's refusal = %q, want it to mention %q", road, out, tc.phrase)
				}
				if !strings.Contains(out, tc.owns) && !strings.Contains(out, "roadmap") {
					t.Fatalf("%s's refusal %q does not point at a row (%s)", road, out, tc.owns)
				}
				// The minted spelling is this compiler's private name: it belongs in the pass's account and
				// nowhere a reader is shown (ADR 0345 §3, ADR 0166).
				for _, leak := range []string{callableForwardInfixForTest, "0x", "%!s(", "__cans", "*lang."} {
					if strings.Contains(out, leak) {
						t.Fatalf("%s's refusal leaks %q:\n%s", road, leak, out)
					}
				}
			}
		})
	}
}

// callableForwardInfixForTest is the pass's mark on a minted parameter, spelled here rather than imported so a
// change to the private spelling fails the leak guard rather than following it.
const callableForwardInfixForTest = "$cap$"

// TestTheForwardedWordModulesVerify asks the artifact rather than the run: the clone is a named `define` with
// one word more than the source `def` wrote, the host's call carries its own reading of the name the program
// spelled, the clone's call of the entry reads only parameters, and every module the row produces is LLVM the
// toolchain accepts (AGENTS.md's verification rule).
func TestTheForwardedWordModulesVerify(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct {
		name  string
		src   string
		want  []string
		clone string // the clone's own parameter list, as the minted word changes it
		entry string // the clone's call of the entry: value + word, both parameters
		host  string // the host's call of the clone: the placeholder, the value, the host's own reading
	}{
		{
			name:  "a def whose body binds the name the entry reads",
			src:   "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n",
			want:  []string{"define i32 @gy_apply1", "%_base"},
			clone: `define i32 @gy_apply1\$callable\$[^(]*\(i32 %p0, i32 %p1, i32 %p2\) \{`,
			entry: `call i32 @gy_use\$lambda\$[^(]*\(i32 %p\d+, i32 %p\d+\)`,
			host:  `call i32 @gy_apply1\$callable\$[0-9a-f]*_\d+\(i32 0, i32 %p\d+, i32 %_base[^,)]*\)`,
		},
		{
			// The host reads its PARAMETER, not a local: the forwarded word is a load of whatever the frame
			// holds, and the shape the module reaches for is the frame's own.
			name:  "the host's word is its parameter",
			src:   "def apply1(fn, v, base):\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed, 9)\n\nprint(use(3))\n",
			want:  []string{"define i32 @gy_apply1"},
			clone: `define i32 @gy_apply1\$callable\$[^(]*\(i32 %p0, i32 %p1, i32 %p2, i32 %p3\) \{`,
			entry: `call i32 @gy_use\$lambda\$[^(]*\(i32 %p\d+, i32 %p\d+\)`,
			host:  `call i32 @gy_apply1\$callable\$[0-9a-f]*_\d+\(i32 0, i32 %p\d+, i32 9, i32 %_base[^,)]*\)`,
		},
		{
			name:  "the recursion hands the word down",
			src:   "def run(n, f):\n    base = 100\n    return 0 if n == 0 else f(n) + run(n - 1, f)\n\ndef use():\n    base = 1\n    return run(3, lambda x: x + base)\n\nprint(use())\n",
			want:  []string{"define i32 @gy_run"},
			clone: `define i32 @gy_run\$callable\$[^(]*\(i32 %p0, i32 %p1, i32 %p2\) \{`,
			entry: `call i32 @gy_use\$lambda\$[^(]*\(i32 %p\d+, i32 %p\d+\)`,
			host:  `call i32 @gy_run\$callable\$[0-9a-f]*_\d+\(i32 3, i32 0, i32 %_base[^,)]*\)`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference answers: %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(res.IR, want) {
					t.Fatalf("%s: the module does not ask %q:\n%s", tc.name, want, headOf(res.IR))
				}
			}
			for _, forbidden := range []string{"ptrtoint", "inttoptr", "fnptr", "call <2 x i32>", "call {}"} {
				if strings.Contains(res.IR, forbidden) {
					t.Fatalf("%s: the module reached for %q — ADR 0345's rule is that a function is declared, not a value", tc.name, forbidden)
				}
			}
			for _, claim := range []struct{ what, re string }{
				{"the clone's parameter list", tc.clone},
				{"the clone's call of the entry", tc.entry},
				{"the host's call of the clone", tc.host},
			} {
				if claim.re == "" {
					continue
				}
				if !regexp.MustCompile(claim.re).MatchString(res.IR) {
					t.Fatalf("%s: %s is not what the minted word requires:\n%s", tc.name, claim.what, headOf(res.IR))
				}
			}
			// The forwarded word is read, never baked: a call of the clone whose last operand is a literal
			// is one program's value answering every other frame.
			if regexp.MustCompile(`call i32 @gy_apply1\$callable\$[0-9a-f]*_\d+\(i32 0, i32 %p\d+, i32 \d+\)`).MatchString(res.IR) {
				t.Fatalf("%s: the forwarded word arrived as a constant:\n%s", tc.name, headOf(res.IR))
			}
			// The minted spelling never reaches the module's own text as a name a reader could mistake for
			// the program's: parameters are positional registers, and the private spelling has no business
			// in a define the human reads.
			if strings.Contains(res.IR, callableForwardInfixForTest) {
				t.Fatalf("%s: the module leaks the pass's private parameter spelling into the IR:\n%s", tc.name, headOf(res.IR))
			}
			irPath := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".ll")
			if err := os.WriteFile(irPath, []byte(res.IR), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			llc := "llc-20"
			if _, err := exec.LookPath(llc); err != nil {
				llc = "llc"
			}
			out, err := exec.Command(llc, "-filetype=null", irPath).CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %s rejected the module: %v\n%s\nIR:\n%s", tc.name, llc, err, out, res.IR)
			}
		})
	}
}

// TestTheForwardedWordConformanceProgramIsTheReferenceByteForByte reads the corpus row this row owns: the
// program in integration/programs/ prints CPython's text on both compiled roads, which is what keeps a row
// that answers a whole family from drifting once the probes have stopped being run.
func TestTheForwardedWordConformanceProgramIsTheReferenceByteForByte(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("forwarded_word")
	path := writeSrc(t, t.TempDir(), "forwarded_word.gy", src)
	want, ok := cpythonOut(t, path)
	if !ok {
		t.Skip("no live python3 to ask")
	}
	for _, road := range []string{"--jit", "--aot"} {
		out, code := cliRunCode(t, road, path)
		if code != 0 {
			t.Fatalf("%s exited %d: %s", road, code, cliRun(t, road, path))
		}
		if out != want {
			t.Fatalf("%s printed:\n%s\nCPython printed:\n%s", road, out, want)
		}
	}
}

// TestTheForwardedWordProgramPrintsTheRecordedReferenceText reads the row's own artifact:
// `testdata/forwarded_word.expected` is the bytes CPython wrote for this program (never the bytes this
// backend wrote), and both compiled roads have to produce them. It is the check that outlives the probes:
// a program in the corpus that stops printing the reference fails here without anyone re-running an
// experiment (AGENTS.md's reference leg; the same artifact rule as `minus_operator.expected`).
func TestTheForwardedWordProgramPrintsTheRecordedReferenceText(t *testing.T) {
	t.Parallel()
	want, err := os.ReadFile("testdata/forwarded_word.expected")
	if err != nil {
		t.Fatalf("the row's reference artifact is missing: %v", err)
	}
	src := readProgramSrc("forwarded_word")
	path := writeSrc(t, t.TempDir(), "forwarded_word.gy", src)
	if ref, ok := cpythonOut(t, path); ok && ref != string(want) {
		t.Fatalf("the recorded reference text has gone stale against python3:\nrecorded:\n%s\nreference:\n%s", want, ref)
	}
	for _, road := range []string{"--jit", "--aot"} {
		out, code := cliRunCode(t, road, path)
		if code != 0 {
			t.Fatalf("%s exited %d: %s", road, code, cliRun(t, road, path))
		}
		if out != string(want) {
			t.Fatalf("%s printed:\n%s\nthe reference recorded:\n%s", road, out, want)
		}
	}
}

func headOf(s string) string {
	if len(s) > 3000 {
		return s[:3000]
	}
	return s
}
