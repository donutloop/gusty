package integration

// integration/target_surface_test.go — the three `%T` doors Gap R.379 files, asked of the shipped CLI on
// both of its roads (roadmap L11.7 · **ADR 0400**).
//
// `pkg/lang/target_surface_test.go` decides the shape — which node the door names, which row it cites, which
// controls keep answering. This file asks the two questions only the built binary can answer:
//
//   - does each of the three refusals reach the reader at exit 1 with the program's own node in it, on
//     `--jit` and on `--aot`, with no Go type name anywhere in what the human is shown; and
//   - does the same sentence reach the machine leg intact — a codegen-or-parse-phase exit 1 whose `error`
//     carries it — since ADR 0004's schema shows the consumer the identical string (ADR 0346).
//
// Exit 2 is forbidden in every loop: ADR 0166 reserves it for a module the compiler wrote and LLVM refused,
// and a reworded refusal has no business reaching the assembler. The controls are re-derived from `python3`
// inside the case, so a literal that drifts from the reference fails here rather than certifying itself.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// targetSurfaceRefused is the same family as the package table, in the same order: the three doors over the
// node kinds a program can actually put in the position. Every one of them is a refusal the REFERENCE agrees
// with (a TypeError, an AttributeError, a SyntaxError) except the two this row filed instead of fixed —
// `xs[0] += 5` (**Gap R.380**) and the item stores whose container is not a variable (**Gap R.381**) — whose
// reference answer is recorded beside the shape in `pkg/lang/target_surface_test.go`.
var targetSurfaceRefused = []struct {
	name string
	src  string
	said string
	door string
}{
	{name: "an item store whose container is a list literal", src: "[1, 2][0] = 3\nprint(1)\n", said: "the list literal `[1, 2]`", door: "item store"},
	{name: "an item store whose container is a dict literal", src: "{\"a\": 1}[\"a\"] = 2\nprint(1)\n", said: "the dict literal", door: "item store"},
	{name: "an item store whose container is a call's answer", src: "def f():\n    return [1, 2]\n\nf()[0] = 3\nprint(1)\n", said: "the call `f(…)`", door: "item store"},
	{name: "an item store whose container is an instance attribute", src: "class C:\n    def __init__(self):\n        self.a = [1]\n\nc = C()\nc.a[0] = 3\nprint(c.a)\n", said: "the attribute `a` of the name `c`", door: "item store"},
	{name: "an attribute store on a number", src: "(1).x = 2\nprint(1)\n", said: "the attribute `x` of a number", door: "attribute store"},
	{name: "an attribute store on a text", src: "\"ab\".x = 2\nprint(1)\n", said: "the attribute `x` of a text", door: "attribute store"},
	{name: "an attribute store on a list literal", src: "[1].x = 2\nprint(1)\n", said: "the attribute `x` of a list literal", door: "attribute store"},
	{name: "an augmented assignment to a number", src: "x = 1\n3 += x\nprint(x)\n", said: "the number `3`", door: "parser"},
	{name: "an augmented assignment to a call", src: "def f():\n    return 1\n\nf() += 1\nprint(1)\n", said: "the call `f(…)`", door: "parser"},
	{name: "an augmented assignment to a list literal", src: "x = 1\n[1] += x\nprint(x)\n", said: "the list literal `[1]`", door: "parser"},
	{name: "an item store on a set literal", src: "{1, 2}[0] = 9\nprint(1)\n", said: "the set literal `{1, 2}`", door: "item store"},
	{name: "an item store on a text", src: "\"abc\"[0] = 9\nprint(1)\n", said: "the text `\"abc\"`", door: "item store"},
	{name: "an item store through a subscript, one container deep", src: "d = {}\nd[\"x\"] = {}\nd[\"x\"][0] = 1\nprint(1)\n", said: "the subscript `d[\"x\"]`", door: "item store"},
	{name: "an attribute store whose base is what a call returns", src: "def f():\n    return [1]\n\nf().pop = 9\nprint(1)\n", said: "the attribute `pop` of a call's answer", door: "attribute store"},
	{name: "an augmented assignment to a subscript", src: "xs = [1]\nxs[0] += 5\nprint(xs[0])\n", said: "the subscript `xs[0]`", door: "parser"},
	{name: "an augmented assignment to a call, spelled on a builtin", src: "print(1) += 1\nprint(1)\n", said: "the call `print(…)`", door: "parser"},
}

// TestWhatTheTargetDoorsSayAtTheCLI is the human leg, on both compiled roads.
func TestWhatTheTargetDoorsSayAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range targetSurfaceRefused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "target_surface.gy", tc.src)
			for _, road := range []string{"--jit", "--aot"} {
				out := cliRun(t, road, path)
				_, code := cliRunCode(t, road, path)
				if code == 2 {
					t.Fatalf("%s reported a compiler bug (exit 2) for a refusal the reference agrees must fail (ADR 0166):\n%s", road, out)
				}
				if code != 1 {
					t.Fatalf("%s exited %d, want the refusal class 1:\n%s", road, code, out)
				}
				if !strings.Contains(out, tc.said) {
					t.Fatalf("%s's %s refusal does not name what the program wrote (%q):\n%s", road, tc.door, tc.said, out)
				}
				for _, leak := range []string{"*lang.", "lang.", "0x", "%!s(", "%!d(", "%v", "roadmapmap["} {
					if strings.Contains(out, leak) {
						t.Fatalf("%s's %s refusal leaks the compiler's own machinery (%q):\n%s", road, tc.door, leak, out)
					}
				}
			}
		})
	}
}

// TestTheTargetDoorsMachinePathCarriesTheSameSentence is the agentic half: `--json` shows a consumer the
// identical string, so the wording fix had to land on both channels at once — and the phase and class stay
// what an agent branches on (ADR 0004's schema, ADR 0346's registry).
func TestTheTargetDoorsMachinePathCarriesTheSameSentence(t *testing.T) {
	t.Parallel()
	for _, tc := range targetSurfaceRefused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			payload := cliRun(t, "--json", "--eval", tc.src)
			var doc struct {
				OK         bool   `json:"ok"`
				Exit       int    `json:"exit"`
				Phase      string `json:"phase"`
				Error      string `json:"error"`
				Capability *struct {
					Code string `json:"code"`
				} `json:"capability"`
			}
			if err := json.Unmarshal([]byte(payload), &doc); err != nil {
				t.Fatalf("the --json payload is not JSON: %v\n%s", err, payload)
			}
			if doc.OK || doc.Exit != 1 {
				t.Fatalf("the refusal did not arrive as an exit 1: %s", payload)
			}
			if doc.Phase != "codegen" && doc.Phase != "parse" {
				t.Fatalf("the refusal arrived in phase %q, want codegen or parse: %s", doc.Phase, payload)
			}
			if !strings.Contains(doc.Error, tc.said) {
				t.Fatalf("the JSON refusal lost the program's node %q:\n%s", tc.said, payload)
			}
			if strings.Contains(doc.Error, "*lang.") {
				t.Fatalf("--json carries the Go type name the CLI no longer prints (Gap R.379):\n%s", payload)
			}
			if _, classified := lang.ClassifyCapabilityRefusal(doc.Error); classified && doc.Capability == nil {
				t.Fatalf("the registry knows this refusal but --json carries no capability code (ADR 0346):\n%s", payload)
			}
		})
	}
}

// TestTheTargetFormsTheReferenceAnswersPrintWhatCPythonPrints is the control leg on both roads: the target
// forms that WORK must keep working, byte for byte, on the JIT and on the linked binary.
func TestTheTargetFormsTheReferenceAnswersPrintWhatCPythonPrints(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"the name target", "x = 1\nx = 2\nprint(x)\n"},
		{"the subscript target", "xs = [1, 2]\nxs[0] = 9\nprint(xs)\n"},
		{"the attribute target of an instance", "class C:\n    def __init__(self):\n        self.v = 0\n\nc = C()\nc.v = 7\nprint(c.v)\n"},
		{"augmented assignment to a name", "x = 1\nx += 2\nprint(x)\n"},
		{"the tuple target", "a, b = [1, 2]\nprint(a, b)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "target_surface_control.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			for _, road := range []string{"--jit", "--aot"} {
				out, code := cliRunCode(t, road, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166):\n%s", road, cliRun(t, road, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference answers (%q): %s", road, code, want, cliRun(t, road, path))
				}
				if out != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", road, out, want, tc.src)
				}
			}
		})
	}
}

// TestWhatTheTargetDoorsLeaveStandingIsFiledNotFixed is the live half of the same pin: for the five shapes
// this row MEASURED and did not take, it asks CPython in the case itself and fails if the reference ever
// starts agreeing with the refusal (the door would then be a plain unsupported form, and the row's wording
// would be wrong) or if the compiled leg ever starts answering them (the row is closed — delete the pin).
// Exit 2 is forbidden here too: a divergence is a refusal at exit 1, never a compiler bug (ADR 0166).
func TestWhatTheTargetDoorsLeaveStandingIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, row string }{
		{"an augmented assignment to a subscript", "xs = [1]\nxs[0] += 5\nprint(xs[0])\n", "Gap R.380"},
		{"an item store whose container is what a call returns", "def f():\n    return [1, 2]\n\nf()[0] = 3\nprint(1)\n", "Gap R.381"},
		{"an item store into a list literal", "[1, 2][0] = 3\nprint(1)\n", "Gap R.381"},
		{"an item store through a subscript, one container deep", "d = {}\nd[\"x\"] = {}\nd[\"x\"][0] = 1\nprint(1)\n", "Gap R.381"},
		{"an item store through an instance attribute", "class C:\n    def __init__(self):\n        self.a = [1]\n\nc = C()\nc.a[0] = 3\nprint(c.a)\n", "Gap R.381"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "target_surface_debt.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if strings.HasPrefix(want, "error:") || strings.HasPrefix(want, "traceback:") {
				t.Fatalf("the reference now REFUSES this program (%s) — it is no longer a divergence; move the pin out of the debt table:\n%s", tc.src, want)
			}
			_, code := cliRunCode(t, "--jit", path)
			if code == 2 {
				t.Fatalf("a divergence is a refusal at exit 1, not a compiler bug (ADR 0166):\n%s", cliRun(t, "--jit", path))
			}
			if code == 0 {
				t.Fatalf("the compiled leg now answers %q — %s is closed: move its row to DONE and delete this pin\n%s", want, tc.row, tc.src)
			}
			// the refusal goes to stderr, so the sentence is read with the combined helper (the exit code
			// above came from the stdout-only one, which is the leg that must stay empty)
			if said := cliRun(t, "--jit", path); !strings.Contains(said, tc.row) {
				t.Fatalf("the CLI refusal does not name the row that owns it (%s):\n%s", tc.row, said)
			}
		})
	}
}

// TestTheTargetFormsTheReferenceAnswersAreOnTheRecord is ADR 0302's rule at this altitude: each control's
// answer is on record, written from the reference by `tools/recmerge` and never from this backend.
func TestTheTargetFormsTheReferenceAnswersAreOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range targetSurfaceRefused {
		t.Run(tc.name, func(t *testing.T) {
			if lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("a refused shape is on the record, which the recorder refuses to file:\n%s", tc.src)
			}
		})
	}
}
