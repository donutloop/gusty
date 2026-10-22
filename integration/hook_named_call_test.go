package integration

// integration/hook_named_call_test.go — a `key=lambda` hook that CALLS a function the module declares, asked
// of the shipped CLI on both of its roads (roadmap L11.7 · **Gap R.378**, closed by **ADR 0399**).
//
// `pkg/lang/hook_named_call_test.go` decides the SHAPE: which programs the hook road hoists, which words the
// minted declaration gains, what the emitted module contains, which refusals stay refusals. This file asks the
// three questions only the built binary can answer:
//
//   - does each program of the family print, on `--jit` AND on `--aot`, exactly what `python3` prints for the
//     same source — read off the reference at run time, never from a literal copied out of a build;
//   - does each program the backend still refuse reach the reader as exit 1 with the door's own sentence, on
//     both roads and on `--json`, and without the compiler's own machinery in it; and
//   - is every answered program on the record leg, where the answer came from the reference and not from this
//     backend (`tools/recmerge`, which files an entry only where the reference and the compiled leg agree)?
//
// Exit 2 is forbidden in every loop below: ADR 0166 reserves it for a module the compiler wrote and LLVM
// refused, and a hook hoisted to a `define` the assembler dislikes is exactly the failure this row could
// produce. ADR 0345 §1's claim — no function VALUE anywhere in the module — is checked on the emitted IR too,
// because a `ptrtoint` here would be that half arriving through the back door.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// hookNamedCallPrograms is the answered family — the same sources as the package table, in the same order, so
// the two files read as one experiment rather than two that can drift apart.
var hookNamedCallPrograms = []struct{ name, src string }{
	{name: "a hook that calls a function the module declares", src: "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n"},
	{name: "the same hook on the other side of the order, as `min`", src: "def helper(v):\n    return 0 - v\n\nprint(min([3, 1, 2], key=lambda x: helper(x)))\n"},
	{name: "and as `max`", src: "def helper(v):\n    return 0 - v\n\nprint(max([3, 1, 2], key=lambda x: helper(x)))\n"},
	{name: "a hook that calls a function and reads a local of its own frame", src: "def helper(v):\n    return 0 - v\n\ndef use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: helper(x) + base))\n\nuse()\n"},
	{name: "a callee that reads a module name of its own", src: "OFF = 1\n\ndef helper(v):\n    return 0 - v + OFF\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n"},
	{name: "a callee whose second argument comes from the body", src: "def helper(v, k):\n    return 0 - v + k\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x, 1)))\n"},
	{name: "a callee with a default the hook does not supply", src: "def helper(v, k=1):\n    return 0 - v + k\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n"},
	{name: "a callee called with a NAMED argument", src: "def helper(v, k=1):\n    return 0 - v + k\n\nprint(sorted([3, 1, 2], key=lambda x: helper(v=x)))\n"},
	{name: "the same call over texts", src: "def helper(s):\n    return 0 - len(s)\n\nprint(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: helper(s)))\n"},
	{name: "a callee that is a declared CLASS", src: "class Box:\n    def __init__(self, v):\n        self.v = v\n\nprint(sorted([3, 1, 2], key=lambda x: Box(x).v))\n"},
	{name: "a callee whose answer comes from a branch", src: "def sign(v):\n    if v > 0:\n        return 1\n    return 0 - 1\n\nprint(sorted([3, -1, 2], key=lambda x: sign(x)))\n"},
	{name: "a hook written in a method's frame", src: "def helper(v):\n    return 0 - v\n\nclass C:\n    def m(self):\n        print(sorted([3, 1, 2], key=lambda x: helper(x)))\n\nC().m()\n"},
	{name: "two hooks of one frame, both calling", src: "def helper(v):\n    return 0 - v\n\ndef use():\n    print(sorted([3, 1, 2], key=lambda x: helper(x)))\n    print(sorted([5, 4], key=lambda y: helper(y)))\n\nuse()\n"},
	{name: "one hook called twice over two containers", src: "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\nprint(sorted([7, 8], key=lambda x: helper(x)))\n"},
	{name: "two calls of one function from one hook", src: "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x) + helper(0)))\n"},
	{name: "a hook in a while body", src: "def helper(v):\n    return 0 - v\n\nn = 0\nwhile n < 2:\n    print(sorted([3, 1, 2], key=lambda x: helper(x) + n))\n    n = n + 1\n"},
	{name: "the answer bound, measured and walked", src: "def helper(v):\n    return 0 - v\n\nxs = sorted([3, 1, 2], key=lambda x: helper(x))\nprint(xs, len(xs), xs[0], xs[2])\n"},
	{name: "a hook that calls a function which calls a function", src: "def twice(v):\n    return helper(v) + helper(v)\n\ndef helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: twice(x)))\n"},
	{name: "a builtin called beside the declared one", src: "def helper(v):\n    return 0 - len(v)\n\nprint(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: helper(s) + len(s)))\n"},
	{name: "a comprehension inside the hook beside the call", src: "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x) + len([q for q in [1, 2]])))\n"},
	{name: "the comparison of two keyed calls", src: "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)) == sorted([9, 8], key=lambda y: helper(y)))\n"},
	{name: "the self-contained hook", src: "print(sorted([3, 1, 2], key=lambda x: 0 - x))\n"},
	{name: "the hook that only reads its frame", src: "def use():\n    base = 1\n    print(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n\nuse()\n"},
	{name: "the hook that only reads a module name", src: "base = 1\nprint(sorted([3, 1, 2], key=lambda x: 0 - x + base))\n"},
	{name: "the named key — a `def` handed to the door as the key itself", src: "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=helper))\n"},
	{name: "the builtin key", src: "print(sorted([\"bbb\", \"a\"], key=len))\n"},
	{name: "the applied body calling a declared function", src: "def helper(v):\n    return 0 - v\n\nprint((lambda x: helper(x) + 5)(3))\n"},
	{name: "the higher-order entry calling a declared function", src: "def helper(v):\n    return 0 - v\n\ndef use(fn):\n    return fn(1)\n\nprint(use(lambda v: helper(v)))\n"},
}

// TestAHookThatCallsADeclaredFunctionPrintsWhatCPythonPrints is the reference leg on both compiled roads. The
// expectations are read off `python3` at run time; a stale literal is not evidence about a language.
func TestAHookThatCallsADeclaredFunctionPrintsWhatCPythonPrints(t *testing.T) {
	t.Parallel()
	for _, tc := range hookNamedCallPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "hook_named_call.gy", tc.src)
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
					t.Fatalf("%s exited %d on a program the reference answers (%q): %s", road, code, want, cliRun(t, road, path))
				}
				if out != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", road, out, want, tc.src)
				}
			}
		})
	}
}

// TestAHookThatCallsADeclaredFunctionIsOnTheRecord is ADR 0302's rule at this altitude: every answered program
// of the family has an entry in the record, and that entry was written from the reference — never from this
// backend. A missing entry fails, so coverage cannot be deleted by deleting a record.
func TestAHookThatCallsADeclaredFunctionIsOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range hookNamedCallPrograms {
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend:\n%s", tc.name, tc.src)
			}
		})
	}
}

// hookNamedCallRefused are the shapes the row leaves as refusals, with the sentence the door must keep saying.
// `ref` is what CPython does with the same source, so a reader can see that a refusal here is a decline and
// not a wrong answer.
var hookNamedCallRefused = []struct{ name, src, ref, said string }{
	{name: "a hook that calls a function declared FURTHER DOWN the module than the hook", src: "print(sorted([3, 1, 2], key=lambda x: helper(x)))\n\ndef helper(v):\n    return 0 - v\n", ref: "raises NameError: name 'helper' is not defined", said: "a lambda is a function VALUE"},
	{name: "a frame that shadows the declared function with a local of the same name", src: "def helper(v):\n    return 0 - v\n\ndef use():\n    helper = 3\n    print(sorted([3, 1, 2], key=lambda x: helper(x)))\n\nuse()\n", ref: "raises TypeError: 'int' object is not callable", said: "a lambda is a function VALUE"},
	{name: "a module that rebinds the declared function before the call", src: "def helper(v):\n    return 0 - v\n\nhelper = 3\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n", ref: "raises TypeError: 'int' object is not callable", said: "a lambda is a function VALUE"},
	{name: "a module that rebinds the declared function AFTER the call", src: "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\nhelper = 3\n", ref: "[3, 2, 1]", said: "a lambda is a function VALUE"},
	{name: "a rebind of the declared function inside a module-level `if`", src: "def helper(v):\n    return 0 - v\n\nflag = 0\nif flag == 0:\n    helper = 3\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n", ref: "raises TypeError: 'int' object is not callable", said: "a lambda is a function VALUE"},
	{name: "a rebind of the declared function inside a module-level `for`", src: "def helper(v):\n    return 0 - v\n\nfor t in [3]:\n    helper = t\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n", ref: "raises TypeError: 'int' object is not callable", said: "a lambda is a function VALUE"},
	{name: "a hook that calls through a name the module bound to the function", src: "def helper(v):\n    return 0 - v\n\nalias = helper\nprint(sorted([3, 1, 2], key=lambda x: alias(x)))\n", ref: "[3, 2, 1]", said: "is called but is not a function this backend can build a call to"},
	{name: "a hook that reads the function as a VALUE beside its call", src: "def helper(v):\n    return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x) + len(str(helper))))\n", ref: "[3, 2, 1]", said: "a lambda is a function VALUE"},
	{name: "a hook that hands the function to another function", src: "def helper(v):\n    return 0 - v\n\ndef apply1(fn, v):\n    return fn(v)\n\nprint(sorted([3, 1, 2], key=lambda x: apply1(helper, x)))\n", ref: "[3, 2, 1]", said: "a lambda is a function VALUE"},
	{name: "an applied body that reads the function as a value", src: "def helper(v):\n    return v + 1\n\nprint((lambda x: helper)(3))\n", ref: "<function helper at 0x…>", said: "names function, which the compiled backend has no value for"},
	{name: "a hook that calls a name the module never declares", src: "print(sorted([3, 1, 2], key=lambda x: not_here(x)))\n", ref: "raises NameError: name 'not_here' is not defined", said: "is called but is not a function this backend can build a call to"},
	{name: "a hook that calls a function declared inside a module-level `if`", src: "flag = 1\nif flag == 1:\n    def helper(v):\n        return 0 - v\n\nprint(sorted([3, 1, 2], key=lambda x: helper(x)))\n", ref: "[3, 2, 1]", said: "is called but is not a function this backend can build a call to"},
}

// TestWhatTheNamedCallRowRefusesReachesTheCLIAsARefusal is the refusal leg of the shipped binary: exit 1 on
// both roads, the door's own sentence intact, and no compiler machinery in it. Exit 2 would mean the pass's
// decline was ignored and a half-rewrite reached the assembler.
func TestWhatTheNamedCallRowRefusesReachesTheCLIAsARefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range hookNamedCallRefused {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "hook_named_call_refused.gy", tc.src)
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
					t.Fatalf("%s's refusal does not say %q:\n%s", road, tc.said, out)
				}
				for _, leak := range []string{"*lang.", "lang.", "0x", "%!s(", "%!d(", "%v", "roadmapmap["} {
					if strings.Contains(out, leak) {
						t.Fatalf("%s's refusal leaks the compiler's own machinery (%q):\n%s", road, leak, out)
					}
				}
			}
		})
	}
}

// TestTheNamedCallMachinePathStillAnswersForAF refusal is the agentic half: the same declines arrive on
// `--json` as a codegen-phase exit 1 whose `error` carries the door's sentence, and whose `capability` object
// resolves whenever the registry knows the shape (ADR 0004's schema, ADR 0346's registry).
func TestTheNamedCallMachinePathStillAnswersForARefusal(t *testing.T) {
	t.Parallel()
	for _, tc := range hookNamedCallRefused {
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
			if _, classified := lang.ClassifyCapabilityRefusal(doc.Error); classified && doc.Capability == nil {
				t.Fatalf("the registry knows this refusal but --json carries no capability code (ADR 0346):\n%s", payload)
			}
		})
	}
}

// TestTheNamedCallHookEmitsAModuleTheToolchainAccepts asks the IR itself the question ADR 0345 §1 settles: the
// hoisted hook is a `define`, the callee is reached by SYMBOL, and the module carries no function value — no
// `fnptr`, no `ptrtoint`, no `inttoptr`, no indirect call. A program that prints the right answer through a
// boxed function pointer would pass every test above and unship the decision.
func TestTheNamedCallHookEmitsAModuleTheToolchainAccepts(t *testing.T) {
	t.Parallel()
	for _, tc := range hookNamedCallPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiler refused a program the reference answers (%v): %s", err, tc.src)
			}
			for _, bad := range []string{"fnptr", "ptrtoint", "inttoptr", "indirect call", "call <null>"} {
				if strings.Contains(res.IR, bad) {
					t.Fatalf("the emitted module contains %q — a function value came back (ADR 0345 §1):\n%s", bad, tc.src)
				}
			}
		})
	}
}

// TestTheNamedCallConformanceProgramPrintsWhatCPythonPrints is the conformance program's own leg: the file in
// `integration/programs/` prints the bytes in `integration/testdata/`, and those bytes are the reference's —
// checked against `python3` here, so an `.expected` file that drifts from the reference fails even though the
// matrix would still compare two files against each other.
func TestTheNamedCallConformanceProgramPrintsWhatCPythonPrints(t *testing.T) {
	t.Parallel()
	file := filepath.Join("programs", "hook_named_call.gy")
	src, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("the conformance program is missing: %v", err)
	}
	wantBytes, err := os.ReadFile(filepath.Join("testdata", "hook_named_call.expected"))
	if err != nil {
		t.Fatalf("the conformance expectations are missing: %v", err)
	}
	want := string(wantBytes)

	ref, ok := cpythonOut(t, file)
	if !ok {
		t.Skip("no live python3 to ask")
	}
	if ref != want {
		t.Fatalf("the expectations are not the reference's answer any more:\nCPython:\n%s\n.expected:\n%s", ref, want)
	}
	for _, road := range []string{"--jit", "--aot"} {
		out, code := cliRunCode(t, road, file)
		if code != 0 {
			t.Fatalf("%s exited %d on the conformance program: %s", road, code, cliRun(t, road, file))
		}
		if out != want {
			t.Fatalf("%s printed\n%s\nthe expectations say\n%s\nfor\n%s", road, out, want, src)
		}
	}
}
