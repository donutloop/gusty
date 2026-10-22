package integration

// integration/refusal_surface_test.go — what a statement refusal is allowed to say, asked of the built CLI
// on both of its roads (roadmap L11.7 · **Gap R.377**, closed by **ADR 0398**).
//
// `pkg/lang/refusal_surface_test.go` decides the shape — which arm of the emitter's wrapper spoke, which
// sentence the door wrote, which name the frame prints under. This file asks the two questions only the
// shipped binary can answer:
//
//   - does a program the backend refuses reach the reader as the DOOR's sentence — exit 1, on `--jit` and on
//     `--aot`, with the frame and the line the program wrote the statement on, and without a Go type name in
//     it anywhere; and
//   - does a program the backend answers print CPython's bytes, with the same answer on record?
//
// The first question is the row's own defect: the shape that filed it reached the CLI as
//
//	gustyc: jit: codegen: a *lang.ExprStmt statement cannot be compiled: `key=lambda ...` is refused: …
//
// — a sentence whose first half was the emitter's Go type table and whose second half was the only half a
// reader could act on. The two tests of the sibling row that had to cut that prefix off before they could
// assert the rest was clean now assert its ABSENCE instead, which is what a leak looks like once it is gone.
//
// Expectations are read off the reference at run time (AGENTS.md's two legs — the record and the reference,
// never two engines). Exit 2 is forbidden everywhere: ADR 0166 reserves it for a module the compiler wrote
// and LLVM refused.

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// refusalSurfaceFamily mirrors the package table — same sources, same order, same expectations — so one
// experiment is read at two altitudes rather than two experiments drifting apart.
var refusalSurfaceFamily = []struct {
	name  string
	src   string
	said  string
	frame string
	line  int
}{
	{
		"a key hook inside a nested def reads the frame two steps out",
		"def outer():\n    base = 2\n    def use():\n        print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n    return use()\n\nouter()\n",
		"`key=lambda ...` is refused", "use", 4,
	},
	{
		"a closure body reads a declared function as a value",
		"def helper(v):\n    return v\n\ndef outer():\n    def use():\n        print(helper)\n    return use()\n\nouter()\n",
		`"helper" names function, which the compiled backend has no value for`, "use", 6,
	},
	{
		"a closure body calls a number",
		"def outer():\n    x = 3\n    def use():\n        return x(1)\n    return use()\n\nprint(outer())\n",
		`"x" is called but is not a function this backend can build a call to`, "use", 4,
	},
	{
		"a closure body iterates a text in a comprehension",
		"def outer():\n    def use():\n        return [c for c in 'abc']\n    return use()\n\nprint(outer())\n",
		`a comprehension over "abc" is refused`, "use", 3,
	},
	{
		"a try block in a closure body copies a container",
		"def outer():\n    def use():\n        try:\n            print(list([1]))\n        except:\n            print(0)\n    return use()\n\nouter()\n",
		"list(<container>) copies are not supported", "use", 3,
	},
	{
		"a with block on a number in a closure body",
		"def outer():\n    def use():\n        with 3 as x:\n            print(x)\n    return use()\n\nouter()\n",
		"string-method __enter__ on a receiver that is not a text the compiler can read", "use", 3,
	},
	{
		"a match arm in a closure body calls a number",
		"def outer():\n    def use(v):\n        match v:\n            case 1:\n                print(tuple(2))\n    return use(1)\n\nouter()\n",
		`"tuple" is called but is not a function this backend can build a call to`, "use", 3,
	},
	{
		"an if block in a closure body copies a container",
		"def outer():\n    def use():\n        if 1 < 2:\n            print(list([1, 2]))\n    return use()\n\nouter()\n",
		"list(<container>) copies are not supported", "use", 3,
	},
	{
		"a while block in a closure body calls a number",
		"def outer():\n    n = 0\n    def use():\n        while n < 1:\n            print(tuple(3))\n            n = 1\n    return use()\n\nouter()\n",
		`"tuple" is called but is not a function this backend can build a call to`, "use", 4,
	},
}

// TestAStatementRefusalReachesTheCLIInTheProgramsWords is the row's own claim at the CLI, on both roads.
func TestAStatementRefusalReachesTheCLIInTheProgramsWords(t *testing.T) {
	t.Parallel()
	for _, tc := range refusalSurfaceFamily {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "refusal_surface.gy", tc.src)
			for _, road := range []string{"--jit", "--aot"} {
				out := cliRun(t, road, path)
				_, code := cliRunCode(t, road, path)
				if code == 2 {
					t.Fatalf("%s reported a compiler bug (exit 2) for a shape the backend refuses in words (ADR 0166):\n%s", road, out)
				}
				if code != 1 {
					t.Fatalf("%s exited %d, want the refusal class 1:\n%s", road, code, out)
				}
				if !strings.Contains(out, tc.said) {
					t.Fatalf("%s's refusal does not carry the door's own sentence %q:\n%s", road, tc.said, out)
				}
				if strings.Contains(out, "statement cannot be compiled") {
					t.Fatalf("%s's refusal arrives inside the statement wrapper (Gap R.377):\n%s", road, out)
				}
				for _, leak := range []string{"*lang.", "lang.", "0x", "%!s(", "%!d(", "%v", "roadmapmap["} {
					if strings.Contains(out, leak) {
						t.Fatalf("%s's refusal leaks the compiler's own machinery (%q):\n%s", road, leak, out)
					}
				}
				want := "(in `" + tc.frame + "`, line " + strconv.Itoa(tc.line) + ")"
				if !strings.Contains(out, want) {
					t.Fatalf("%s's refusal does not say where the statement is (%s):\n%s", road, want, out)
				}
			}
		})
	}
}

// TestTheMachinePathStillClassesAStatementRefusal is the agentic half: a refusal that reads well must still
// arrive as the same JSON the sibling rows read — a codegen-phase exit 1 whose `error` carries the door's
// sentence, and whose `capability` object still resolves (ADR 0004's schema, ADR 0346's registry). The
// wrapper this row removed carried no information for a machine either: the class comes from the payload.
func TestTheMachinePathStillClassesAStatementRefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range refusalSurfaceFamily {
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
			if doc.OK || doc.Exit != 1 || doc.Phase != "codegen" {
				t.Fatalf("the refusal did not arrive as a codegen-phase exit 1: %s", payload)
			}
			if !strings.Contains(doc.Error, tc.said) {
				t.Fatalf("the JSON refusal lost the door's sentence %q:\n%s", tc.said, payload)
			}
			if strings.Contains(doc.Error, "statement cannot be compiled") || strings.Contains(doc.Error, "*lang.") {
				t.Fatalf("--json carries the statement wrapper the CLI no longer prints (Gap R.377):\n%s", payload)
			}
			if _, classified := lang.ClassifyCapabilityRefusal(doc.Error); classified && doc.Capability == nil {
				t.Fatalf("the registry knows this refusal but --json carries no capability code (ADR 0346):\n%s", payload)
			}
		})
	}
}

// refusalSurfaceControls are the answered siblings of the family: the same statements, the same closure
// roads, with nothing missing. They are what keeps the row honest about what it did NOT change.
var refusalSurfaceControls = []struct {
	name string
	src  string
	std  string
}{
	{
		"the nested def whose hook reads its OWN frame",
		"def use():\n    base = 2\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n",
		"[3, 2, 1]\n",
	},
	{
		"the nested def whose body calls a declared function",
		"def helper(v):\n    return v * 2\n\ndef outer():\n    def use():\n        print(helper(3))\n    return use()\n\nouter()\n",
		"6\n",
	},
	{
		"the closure body inside a try and an except",
		"def outer():\n    def use():\n        try:\n            print(len([1, 2, 3]))\n        except:\n            print(0)\n    return use()\n\nouter()\n",
		"3\n",
	},
	{
		"the match arm over a closure body",
		"def outer():\n    def use(v):\n        match v:\n            case 1:\n                print(len([1, 2]))\n            case _:\n                print(0)\n    return use(1)\n\nouter()\n",
		"2\n",
	},
	{
		"the closure body with a for loop and a builtin over its own name",
		"def outer():\n    def use():\n        total = 0\n        for v in [1, 2, 3]:\n            total = total + len([v, v])\n        print(total)\n    return use()\n\nouter()\n",
		"6\n",
	},
}

// TestTheAnsweredSiblingsOfTheFamilyPrintWhatCPythonPrints is the reference leg: the pass the family probes
// runs through is the same pass these programs compile with, so a refusal that changed WHEN a sentence is
// spoken — not just how it reads — fails here.
func TestTheAnsweredSiblingsOfTheFamilyPrintWhatCPythonPrints(t *testing.T) {
	t.Parallel()
	for _, tc := range refusalSurfaceControls {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "refusal_surface_control.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.std {
				t.Fatalf("this file's expectation of the reference is stale: CPython printed %q, not %q", want, tc.std)
			}
			for _, road := range []string{"--jit", "--aot"} {
				out, code := cliRunCode(t, road, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166):\n%s", road, cliRun(t, road, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d: %s", road, code, cliRun(t, road, path))
				}
				if out != want {
					t.Fatalf("%s printed %q, CPython printed %q", road, out, want)
				}
			}
		})
	}
}

// TestTheAnsweredSiblingsAreOnTheRecord is ADR 0302's rule at this altitude: each control's answer is on
// record, written from the reference by tools/recmerge and never from this backend.
func TestTheAnsweredSiblingsAreOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range refusalSurfaceControls {
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend:\n%s", tc.name, tc.src)
			}
		})
	}
}

// TestTheFuncDefArmKeepsTheSentenceTheDriftLedgerQuotes guards the arm this row left alone. Its wording is
// quoted by `pkg/lang/testdata/interpreter-golden-drift.json`, where a row whose wording silently changed is
// a divergence that silently disappeared; folding the `*FuncDef` arm into the new passthrough would have
// done exactly that.
func TestTheFuncDefArmKeepsTheSentenceTheDriftLedgerQuotes(t *testing.T) {
	t.Parallel()
	src := "class C:\n    def m(self, x):\n        x = x + 0.5\n        return x\n\nc = C()\nprint(c.m(1.0))\n"
	path := writeSrc(t, t.TempDir(), "funcdef_arm.gy", src)
	for _, road := range []string{"--jit", "--aot"} {
		out := cliRun(t, road, path)
		if !strings.Contains(out, `codegen: function "m" cannot be compiled: `) {
			t.Fatalf("%s changed the *FuncDef arm's sentence (Gap R.38's, quoted by the drift ledger):\n%s", road, out)
		}
		for _, leak := range []string{"*lang.", "statement cannot be compiled"} {
			if strings.Contains(out, leak) {
				t.Fatalf("%s's *FuncDef refusal leaks %q:\n%s", road, leak, out)
			}
		}
	}
}
