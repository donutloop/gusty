package integration

// integration/key_hook_frame_test.go — the reference leg for the ordering door's hook lambda that reads the
// frame its call is written in (roadmap L11.7 · Gap R.376; ADR 0396).
//
// `pkg/lang/key_hook_frame_test.go` decides the SHAPE — which programs the hook road hoists, which words it
// mints, what the emitted module contains, which refusals stay refusals. This file asks the question only a
// run can answer: does the compiled program print what `python3` prints, on both of its roads — the in-process
// JIT and the linked binary — and does every module it produces stay LLVM the toolchain accepts?
//
// Every expectation below is read off the reference at run time by `cpythonOut`. A compiled answer that
// differs from the reference's is a failure; a stale literal copied out of a previous build is not evidence
// about a language (AGENTS.md's two legs — the record and the reference, never two engines). Exit 2 is
// forbidden in every loop: ADR 0166 reserves it for a module the compiler wrote and LLVM refused.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// keyHookFramePrograms is the answered family — the same sources as the package table, in the same order, so
// the two files read as one experiment.
var keyHookFramePrograms = []struct{ name, src string }{
	{"a hook reads a local of the frame the call is written in", "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n"},
	{"the same hook spelled at module level over a module name", "base = 1\nprint(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n"},
	{"a hook reads a constant the module declares", "LIMIT = 10\nprint(sorted([3, 1, 2], key=lambda x: 0 - x + LIMIT))\n"},
	{"a hook reads a constant from inside a function frame", "LIMIT = 10\ndef use():\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + LIMIT))\n\nuse()\n"},
	{"the frame's own parameter, and the frame runs twice", "def use(seed):\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + seed))\n\nuse(1)\nuse(4)\n"},
	{"min and max ask the one hook the same question", "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base), max([1, 2, 3], key=lambda v: 0 - v + base))\n\nuse()\n"},
	{"min and max over written-out candidates", "def use():\n    base = 1\n    print(min([3, 1, 2], key=lambda x: 0 - x + base), max([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n"},
	{"reverse=True counts the other side of the same key", "def use():\n    base = 1\n    print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: 0 - len(s) + base, reverse=True))\n\nuse()\n"},
	{"the answer is a list like any other", "def use():\n    base = 1\n    xs = sorted([3, 1, 2], key=lambda x: 0 - x + base)\n    print(xs, len(xs), xs[0], xs[2])\n\nuse()\n"},
	{"the keyed answer is walkable", "def use(step):\n    for v in sorted([3, 1, 2], key=lambda x: 0 - x + step):\n        print(v)\n\nuse(1)\n"},
	{"two hooks of one frame", "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n    print(sorted([5, 4], key=lambda y: 0 - y + base))\n\nuse()\n"},
	{"a hook in a while body", "def use():\n    base = 1\n    n = 0\n    while n < 2:\n        print(sorted([3, 1, 2], key=lambda x: 0 - x + base + n))\n        n = n + 1\n\nuse()\n"},
	{"a rebinding between the two hooks moves both answers", "def use():\n    base = 1\n    print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: 0 - len(s) + base))\n    base = 5\n    print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: 0 - len(s) + base))\n\nuse()\n"},
	{"a hook that reads two names of its frame", "def use():\n    base = 1\n    other = 2\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base + other))\n\nuse()\n"},
	{"a hook inside a method's frame", "class C:\n    def method(self):\n        base = 1\n        print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nC().method()\n"},
	{"the lambda's own parameter is the frame's name", "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda base: 0 - base + base))\n\nuse()\n"},
	{"a hook in a for body inside a def", "def use():\n    for step in [1, 2]:\n        print(sorted([3, 1, 2], key=lambda x: 0 - x + step))\n\nuse()\n"},
	{"the self-contained hook keeps the one word it always had", "print(sorted([3, 1, 2], key=lambda x: 0 - x))\n"},
	{"a builtin key", "print(sorted([\"bbb\", \"a\"], key=len))\n"},
	{"a named key", "def neg(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=neg))\n"},
	{"the program's own sorted is not the builtin", "def sorted(xs):\n    return [1]\nprint(sorted([3, 2]))\n"},
	{"the key's body answers a plain call too", "def k(s, base):\n    return 0 - len(s) + base\n\nprint(k(\"ab\", 1), k(\"abcd\", 1))\n"},
	{"an int key that reads the frame orders texts", "def use():\n    base = 1\n    print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: 0 - len(s) + base))\n\nuse()\n"},
}

// TestAHookThatReadsItsFramePrintsWhatCPythonPrints is the reference leg on both compiled roads.
func TestAHookThatReadsItsFramePrintsWhatCPythonPrints(t *testing.T) {
	t.Parallel()
	for _, tc := range keyHookFramePrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "key_hook_frame.gy", tc.src)
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

// TestTheHookFrameProgramsAreOnTheRecord is ADR 0302's rule at this altitude: every program this row answers
// has an answer on record, and the record was written from the reference by tools/recmerge, never from this
// backend. A row whose record disappeared fails here rather than passing by having nothing to compare to.
func TestTheHookFrameProgramsAreOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range keyHookFramePrograms {
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// TestWhatTheHookRoadDeclinesStaysRefusedAtTheCLI holds each shape this row leaves alone to ADR 0166 and
// ADR 0249: exit 1, never exit 2, and a sentence that names the missing half and the row that owns it.
func TestWhatTheHookRoadDeclinesStaysRefusedAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		src    string
		phrase string
		owns   string
	}{
		{
			"a name of a frame two steps out",
			"def outer():\n    base = 2\n    def use():\n        print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\n    use()\n\nouter()\n",
			"a lambda is a function VALUE", "L11.7",
		},
		{
			"a capture the one word cannot carry: a text",
			"def use():\n    label = \"x\"\n    print(sorted([\"ab\", \"b\"], key=lambda s: s + label))\n\nuse()\n",
			"holds a text", "L11.1",
		},
		{
			"a capture the one word cannot carry: a container",
			"def use():\n    bag = [1, 2]\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + bag))\n\nuse()\n",
			"holds a container", "L11.1",
		},
		{
			"a module frame whose name the module rebinds",
			"for step in [1, 2]:\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + step))\n",
			"bound at module level", "Gap R.35",
		},
		{
			"a hook that calls a function the module declares",
			"def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n",
			"a lambda is a function VALUE", "L11.7",
		},
		{
			"a hook written inside a lambda body",
			"def use():\n    base = 1\n    print((lambda xs: sorted(xs, key=lambda x: 0 - x + base))([3, 1, 2]))\n\nuse()\n",
			"a lambda is a function VALUE", "L11.7",
		},
		{
			"a lambda in the option slot the door never calls",
			"def use():\n    base = 1\n    print(sorted([3, 1, 2], reverse=lambda x: x + base))\n\nuse()\n",
			"needs a constant flag", "L11.7",
		},
		{
			"float elements under a hook that reads its frame",
			"def use():\n    base = 1\n    print(sorted([3.5, 1.5, 2.0], key=lambda x: 0 - x + base))\n\nuse()\n",
			"Gap R.248", "L11.1",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "key_hook_frame_refusal.gy", tc.src)
			for _, road := range []string{"--jit", "--aot"} {
				// Two calls, two streams: the exit code is the contract and the MESSAGE is on stderr,
				// which `cliRunCode` does not capture (a refusal that only prints to stderr would read
				// here as an empty sentence, which is what it did the first time this row was run).
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
				// The statement wrapper these refusals used to arrive inside leaked a Go type name, and this test
				// and its package twin had to CUT it off the front before they could call the sentence clean — a
				// leak becoming the test's problem instead of the compiler's. Closed by **Gap R.377** /
				// **ADR 0398**, so the cut is gone and the absence is the assertion: the door's sentence arrives as
				// the message, with the frame and line the program wrote the statement on.
				for _, leak := range []string{"*lang.", "0x", "%!s(", "__cans", "%c4"} {
					if strings.Contains(out, leak) {
						t.Fatalf("%s's refusal leaks %s:\n%s", road, leak, out)
					}
				}
				if strings.Contains(out, "statement cannot be compiled") {
					t.Fatalf("%s's refusal still arrives inside the statement wrapper (Gap R.377):\n%s", road, out)
				}
			}
		})
	}
}

// TestTheHookFrameModulesVerify asks the artifact rather than the run: the hook is a named `define` with the
// element and the words it reads, the caller's call carries the host's own reading, and every module the row
// produces is LLVM the toolchain accepts (AGENTS.md's verification rule).
func TestTheHookFrameModulesVerify(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct {
		name string
		src  string
		want []string
		// hostRead is how the host's own reading of the captured name reaches the hook's call: the LOCAL'S
		// load (`%_base.ld1`) or the PARAMETER'S own register (`%p0`) — the two shapes the frame holds a
		// name in. Neither is a copy of the value and neither is an address, which is the claim the second
		// operand of every keyed call below has to satisfy.
		hostRead string
		// defineHead is the minted hook's own parameter list: the element's word, plus one per name the
		// frame owns — which is the difference between this row and the door's own ADR 0345 hoist, read off
		// the `define` rather than off the pass's account.
		defineHead string
	}{
		{
			name:       "a def's local crosses to the hook beside the element",
			src:        "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n",
			want:       []string{"define i32 @gy_sorted$lambda$", "call i32 @rt_elem_gt(i32 %", "store i32 %p1, i32* %_param1"},
			hostRead:   `call i32 @gy_sorted\$lambda\$[^(]*\(i32 [^,]+, i32 %_base\.ld[0-9]+\)`,
			defineHead: `define i32 @gy_sorted\$lambda\$[^(]*\(i32 %p0, i32 %p1\) \{`,
		},
		{
			name:       "the frame's parameter crosses the same way",
			src:        "def use(seed):\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + seed))\n\nuse(1)\n",
			want:       []string{"define i32 @gy_sorted$lambda$", "call i32 @rt_elem_gt(i32 %", "store i32 %p1, i32* %_param1"},
			hostRead:   `call i32 @gy_sorted\$lambda\$[^(]*\(i32 [^,]+, i32 %p\d+\)`,
			defineHead: `define i32 @gy_sorted\$lambda\$[^(]*\(i32 %p0, i32 %p1\) \{`,
		},
		{
			name:       "a method's frame is a frame like any other",
			src:        "class C:\n    def method(self):\n        base = 1\n        print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nC().method()\n",
			want:       []string{"define i32 @gy_sorted$lambda$", "%_base.ld"},
			hostRead:   `call i32 @gy_sorted\$lambda\$[^(]*\(i32 [^,]+, i32 %_base\.ld[0-9]+\)`,
			defineHead: `define i32 @gy_sorted\$lambda\$[^(]*\(i32 %p0, i32 %p1\) \{`,
		},
		{
			// A name the module binds is read by the hoisted `def` directly (ADR 0395's rule, asked of this
			// road now): the hook is minted with the element's parameter and NO word beside it.
			name:       "a module name mints no word: the hoisted def reads the global",
			src:        "LIMIT = 10\nprint(sorted([3, 1, 2], key=lambda x: 0 - x + LIMIT))\n",
			want:       []string{"define i32 @gy_sorted$lambda$", "call i32 @rt_elem_gt(i32 %"},
			defineHead: `define i32 @gy_sorted\$lambda\$[^(]*\(i32 %p0\) \{`,
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
					t.Fatalf("%s: the module does not ask %q:\n%s", tc.name, want, res.IR)
				}
			}
			for _, forbidden := range []string{"ptrtoint", "inttoptr", "fnptr"} {
				if strings.Contains(res.IR, forbidden) {
					t.Fatalf("%s: the module reached for %q — ADR 0345's rule is that a function is declared, not a value", tc.name, forbidden)
				}
			}
			if tc.defineHead != "" {
				if !regexp.MustCompile(tc.defineHead).MatchString(res.IR) {
					t.Fatalf("%s: the minted hook's parameter list is not what the words it reads require:\n%s", tc.name, res.IR)
				}
			}
			if tc.hostRead != "" {
				if !regexp.MustCompile(tc.hostRead).MatchString(res.IR) {
					t.Fatalf("%s: the keyed call does not carry the host's own reading of the name:\n%s", tc.name, res.IR)
				}
				// and it is a reading, not a constant: a call of the hook with TWO literals in it is the
				// shape that would answer one program's value into every other frame's call
				if regexp.MustCompile(`call i32 @gy_sorted\$lambda\$[^(]*\(i32 [^,]+, i32 \d+\)`).MatchString(res.IR) {
					t.Fatalf("%s: the forwarded word arrived as a constant:\n%s", tc.name, res.IR)
				}
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

// TestTheHookFrameConformanceProgramIsTheReferenceByteForByte reads the corpus row this row owns: the program
// in integration/programs/ prints CPython's text on both compiled roads, which is what keeps a row that
// answers 23 probe shapes from drifting once the probes have stopped being run.
func TestTheHookFrameConformanceProgramIsTheReferenceByteForByte(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("key_hook_frame")
	path := writeSrc(t, t.TempDir(), "key_hook_frame.gy", src)
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
