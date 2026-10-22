package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// pair_param_truth_test.go is the CLI half of roadmap Gap R.361 / ADR 0393.
//
// ADR 0389 gave a call argument its (payload, tag) pair; the callee's OWN condition never asked the tag,
// so a body that tests its parameter — `if v:`, `while v:`, `1 if v else 0`, `not v`, `bool(v)` — tested
// the payload against zero. For the kinds the pair road exists for that is never the reference's answer: a
// text's payload is its `@str_tab` index, a container's a heap handle, `None`'s `0`. At the terminal the
// whole family printed with the exit code of success.
//
// Each row below is asked of CPython first — the table's own expectation is checked against the reference
// inside the case, so a row cannot drift into pinning this backend — and then of both compiled legs, with
// exit 2 forbidden.

var pairParamTruthCLIPrograms = []struct {
	name, src, want string
}{
	{
		name: "the row program: a callee that tests its own parameter",
		src:  "def f(v):\n    if v:\n        return 1\n    return 0\n\nd = {\"e\": [], \"t\": \"\", \"l\": [1], \"n\": None}\nprint(f(d[\"e\"]), f(d[\"t\"]), f(d[\"l\"]), f(d[\"n\"]))\n",
		want: "0 0 1 0\n",
	},
	{
		name: "the else arm of an empty container parameter",
		src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": []}\nf(d[\"k\"])\n",
		want: "f\n",
	},
	{
		name: "the then arm of a filled container parameter",
		src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": [1]}\nf(d[\"k\"])\n",
		want: "t\n",
	},
	{
		name: "an empty text parameter is false",
		src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": \"\"}\nf(d[\"k\"])\n",
		want: "f\n",
	},
	{
		name: "a text parameter is true",
		src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": \"hi\"}\nf(d[\"k\"])\n",
		want: "t\n",
	},
	{
		name: "an empty dict parameter is false",
		src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": {}}\nf(d[\"k\"])\n",
		want: "f\n",
	},
	{
		name: "a void parameter is false and a zero is false",
		src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"n\": None, \"z\": 0}\nf(d[\"n\"])\nf(d[\"z\"])\n",
		want: "f\nf\n",
	},
	{
		name: "a negative-zero parameter is false",
		src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": -0.0}\nf(d[\"k\"])\n",
		want: "f\n",
	},
	{
		name: "not over a caller-supplied parameter",
		src:  "def f(v):\n    print(not v)\n\nd = {\"k\": []}\nf(d[\"k\"])\n",
		want: "True\n",
	},
	{
		name: "not over a truthy caller-supplied parameter",
		src:  "def f(v):\n    print(not v)\n\nd = {\"k\": \"x\"}\nf(d[\"k\"])\n",
		want: "False\n",
	},
	{
		name: "bool of a caller-supplied empty container",
		src:  "def f(v):\n    print(bool(v))\n\nd = {\"k\": []}\nf(d[\"k\"])\n",
		want: "False\n",
	},
	{
		name: "bool of a caller-supplied filled container",
		src:  "def f(v):\n    print(bool(v))\n\nd = {\"k\": [1, 2]}\nf(d[\"k\"])\n",
		want: "True\n",
	},
	{
		name: "a ternary head over a caller-supplied parameter",
		src:  "def f(v):\n    print(\"y\" if v else \"n\")\n\nd = {\"k\": []}\nf(d[\"k\"])\n",
		want: "n\n",
	},
	{
		name: "a ternary head over a truthy parameter",
		src:  "def f(v):\n    print(\"y\" if v else \"n\")\n\nd = {\"k\": 2.5}\nf(d[\"k\"])\n",
		want: "y\n",
	},
	{
		name: "the loop head asks the tag the caller wrote",
		src:  "def f(v):\n    while v:\n        print(\"once\")\n        break\n    print(\"done\")\n\nd = {\"k\": []}\nf(d[\"k\"])\n",
		want: "done\n",
	},
	{
		name: "the loop head runs for a truthy parameter",
		src:  "def f(v):\n    while v:\n        print(\"once\")\n        break\n    print(\"done\")\n\nd = {\"k\": [1]}\nf(d[\"k\"])\n",
		want: "once\ndone\n",
	},
	{
		name: "an elif chain reads the same tag",
		src:  "def f(v):\n    if v:\n        print(\"a\")\n    elif v:\n        print(\"b\")\n    else:\n        print(\"c\")\n\nd = {\"k\": \"\"}\nf(d[\"k\"])\n",
		want: "c\n",
	},
	{
		name: "a verdict parameter keeps its own truth",
		src:  "def f(v):\n    print(bool(v), not v)\n\nd = {\"t\": True, \"f\": False}\nf(d[\"t\"])\nf(d[\"f\"])\n",
		want: "True False\nFalse True\n",
	},
	// The entry spelled as a CALL rather than as a literal. `list()` / `set()` / `dict()` is the container the
	// program meant; the read of the slot used to cross as the heap handle the literal store had left behind,
	// because the pair scan kept a third copy of the spelling test and a `Call` was not on its list (roadmap
	// Gap R.369, ADR 0394). Each row asks the print door and the truth door of the same entry, so a fix that
	// only opens one of them cannot pass.
	{
		name: "an empty container spelled as a list() call prints itself and is false",
		src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": list()}\nf(d[\"k\"])\n",
		want: "[]\nf\n",
	},
	{
		name: "an empty container spelled as a set() call prints itself and is false",
		src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": set()}\nf(d[\"k\"])\n",
		want: "set()\nf\n",
	},
	{
		name: "an empty container spelled as a dict() call prints itself and is false",
		src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {\"k\": dict()}\nf(d[\"k\"])\n",
		want: "{}\nf\n",
	},
	{
		name: "the list element spelled as a call, and its filled neighbour",
		src:  "def f(v):\n    print(v)\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nxs = [list(), [1]]\nf(xs[0])\nf(xs[1])\n",
		want: "[]\nf\n[1]\nt\n",
	},
	// Controls: the kinds the compiler can prove never needed this door and keep the road they had.
	{
		name: "a provable integer parameter still compares",
		src:  "def f(v):\n    if v:\n        print(\"t\")\n    else:\n        print(\"f\")\n\nd = {1: 2}\nf(d[1])\n",
		want: "t\n",
	},
	{
		name: "a parameter that arrives as an ordinary int keeps its road",
		src:  "def f(v):\n    print(bool(v), not v)\n\nf(0)\nf(3)\n",
		want: "False True\nTrue False\n",
	},
}

func TestTheTruthOfACallerSuppliedParameterAtTheCLI(t *testing.T) {
	for _, tc := range pairParamTruthCLIPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference failed to answer this program: src: %s", tc.src)
			}
			if py != tc.want {
				t.Fatalf("this table's expectation is not the reference's: table %q, CPython %q\nsrc: %s", tc.want, py, tc.src)
			}
			path := writeSrc(t, t.TempDir(), "pair_param_truth.gy", tc.src)
			for _, engine := range []string{"--jit", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: the compiled leg rejected the compiler's own module (ADR 0166):\n%s",
						engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s: exit %d, want 0: %s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Errorf("%s: printed %q, want CPython's %q\nsrc: %s", engine, out, tc.want, tc.src)
				}
			}
		})
	}
}

// The conformance program: the whole family in one file, line for line what CPython prints.
const pairParamTruthProgram = "pair_param_truth"

func TestThePairParamTruthProgramMatchesTheReference(t *testing.T) {
	t.Parallel()
	src := readProgramSrc(pairParamTruthProgram)
	want := pythonOutput(t, src)
	if strings.TrimSpace(want) == "" {
		t.Fatal("the reference printed nothing for the conformance program")
	}
	path := writeSrc(t, t.TempDir(), pairParamTruthProgram+".gy", src)
	for _, engine := range []string{"--jit", "--aot"} {
		out, code := cliRunCode(t, engine, path)
		if code != 0 {
			t.Fatalf("%s: exit %d, want 0: %s", engine, code, cliRun(t, engine, path))
		}
		if out != want {
			t.Errorf("%s printed %q, the reference prints %q", engine, out, want)
		}
	}
}

func TestThePairParamTruthProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src := readProgramSrc(pairParamTruthProgram)
	if !lang.HasGoldenAnswer(src) {
		t.Fatal("the conformance program is not on the record — record it from the reference with tools/recmerge")
	}
}

func TestThePairParamTruthModulesVerify(t *testing.T) {
	for _, tc := range pairParamTruthCLIPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			v, verr := lang.VerifyModuleIR(res.IR, 0)
			if verr != nil || !v.OK {
				t.Fatalf("module does not verify: %v %v", v.Errors, verr)
			}
		})
	}
}

// TestWhatTheTruthOfAPairSuppliedParameterStillOwesAtTheCLI is the same filed residue as
// `pkg/lang/pair_param_truth_test.go`, read at the terminal: a wrong answer with the exit code of success
// is only a claim until the reference is asked beside it, and a run that never ends is only a claim when
// somebody has sat through it.
func TestWhatTheTruthOfAPairSuppliedParameterStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, today, wantSub, row string
	}{
		{
			// Gap R.366: the rebinding road writes the payload and leaves the tag the entry store wrote.
			name:  "a parameter the body rebinds is asked a stale tag",
			src:   "def f(v):\n    n = 0\n    while v:\n        n = n + 1\n        v = 0\n    return n\n\nd = {\"e\": [], \"t\": \"\", \"l\": [1], \"n\": None}\nprint(f(d[\"e\"]), f(d[\"t\"]), f(d[\"l\"]), f(d[\"n\"]))\n",
			today: "1 1 1 0\n",
			row:   "Gap R.366",
		},
		{
			// Gap R.367: the `and` operand keeps the whole body on the ordinary road.
			name:  "an and operand that is a caller-supplied parameter",
			src:   "def f(v):\n    if v and 1:\n        return \"T\"\n    return \"F\"\n\nd = {\"e\": [], \"l\": [1]}\nprint(f(d[\"e\"]), f(d[\"l\"]))\n",
			today: "T T\n",
			row:   "Gap R.367",
		},
		{
			name:    "the chosen operand of an and over a parameter refuses",
			src:     "def f(v):\n    return v and 7\n\nd = {\"e\": []}\nprint(f(d[\"e\"]))\n",
			wantSub: "chooses between two values whose kinds",
			row:     "Gap R.367",
		},
		{
			// The ternary's HEAD is answered by ADR 0393 (see the ladder above); its ARM is the same
			// chosen-value hole, and it answers at exit 0 rather than refusing.
			name:  "a ternary arm that is a caller-supplied parameter",
			src:   "def f(v):\n    return v if v else \"z\"\n\nd = {\"e\": []}\nprint(f(d[\"e\"]))\n",
			today: "3\n",
			row:   "Gap R.367",
		},
		{
			// Gap R.368: a text-answering body is emitted under the convention that owns its parameters.
			name:  "a text answering body keeps its parameters one word",
			src:   "def f(v):\n    while v:\n        print(\"once\")\n        break\n    return \"done\"\n\nd = {\"e\": [], \"l\": [1]}\nprint(f(d[\"e\"]), f(d[\"l\"]))\n",
			today: "once\nonce\ndone done\n",
			row:   "Gap R.368",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Fatalf("the reference failed to answer this program: src: %s", tc.src)
			}
			if tc.wantSub != "" {
				path := writeSrc(t, t.TempDir(), "pair_param_truth_owed.gy", tc.src)
				stdout, stderr, code := cliStreams(t, "--aot", "--file", path)
				if code != 1 {
					t.Fatalf("%s: exit %d, want the refusal's 1 (stdout %q, stderr %q)", tc.row, code, stdout, stderr)
				}
				if !strings.Contains(stderr, tc.wantSub) {
					t.Errorf("%s refused without naming %q:\n%s", tc.row, tc.wantSub, stderr)
				}
				if stdout != "" {
					t.Errorf("%s printed %q before refusing; a refusal is not a partial answer\nsrc: %s", tc.row, stdout, tc.src)
				}
				if strings.TrimSpace(py) == "" {
					t.Fatalf("the reference answers this program, so the refusal is the debt (%s)", tc.row)
				}
				return
			}
			if py == tc.today {
				t.Fatalf("%s: the compiled leg agrees with the reference — delete or rewrite this row\nsrc: %s", tc.row, tc.src)
			}
			path := writeSrc(t, t.TempDir(), "pair_param_truth_owed.gy", tc.src)
			out, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("%s: the compiled leg rejected the compiler's own module (ADR 0166):\n%s", tc.row, cliRun(t, "--aot", path))
			}
			if code != 0 {
				t.Fatalf("%s: exit %d, want 0: %s", tc.row, code, cliRun(t, "--aot", path))
			}
			if out != tc.today {
				t.Errorf("%s: printed %q, pinned %q — the shape changed, DELETE or rewrite the pin (%s)\nsrc: %s", tc.row, out, tc.today, tc.row, tc.src)
			}
		})
	}
}

// TestARebindingParameterLoopStillRunsForever witnesses the hang the same family leaves behind: the module
// is fine, the run is not. `rt_heap_len` of a payload the stale list tag points at never reaches zero, so
// the program the reference finishes in one iteration is still counting when the run is killed.
func TestARebindingParameterLoopStillRunsForever(t *testing.T) {
	t.Parallel()
	const src = "def f(v):\n    n = 0\n    while v:\n        n = n + 1\n        v = []\n    return n\n\nd = {\"e\": [], \"t\": \"\", \"l\": [1], \"n\": None, \"z\": 0, \"o\": 1, \"s\": \"hi\", \"f\": 0.0}\nprint(f(d[\"e\"]), f(d[\"t\"]), f(d[\"l\"]), f(d[\"n\"]), f(d[\"z\"]), f(d[\"o\"]), f(d[\"s\"]), f(d[\"f\"]))\n"
	if got := pythonOutput(t, src); got != "0 0 1 0 0 1 1 0\n" {
		t.Fatalf("the reference reads %q, want %q", got, "0 0 1 0 0 1 1 0\n")
	}
	res, err := lang.Compile(src)
	if err != nil {
		t.Fatalf("the compiled leg refused a program the reference runs: %v", err)
	}
	dir := t.TempDir()
	llPath := filepath.Join(dir, "prog.ll")
	if err := os.WriteFile(llPath, []byte(res.IR), 0o644); err != nil {
		t.Fatal(err)
	}
	obj := filepath.Join(dir, "prog.o")
	if out, err := exec.Command("llc-20", "-relocation-model=pic", "-filetype=obj", llPath, "-o", obj).CombinedOutput(); err != nil {
		t.Fatalf("llc: %v\n%s", err, out)
	}
	bin := filepath.Join(dir, "prog")
	if out, err := exec.Command("cc", obj, "-lm", "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("cc: %v\n%s", err, out)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return // witnessed: the run this row files is the one that never ends
	}
	if err != nil {
		t.Fatalf("the linked binary failed for a reason that is not the hang this row pins: %v\n%s", err, out)
	}
	t.Fatalf("the linked binary finished with %q; the pin is the run that never ends — DELETE or rewrite it (Gap R.366)\nsrc: %s", string(out), src)
}
