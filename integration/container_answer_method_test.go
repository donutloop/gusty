package integration

import (
	"encoding/json"
	"strings"
	"testing"
)

// A method run on the container a call hands back, through the CLI (roadmap **Gap R.229**, **ADR 0458**).
//
// At this altitude the symptom was a refusal where the reference prints: `gustyc --eval
// 'def b(): return [1, 2]` / `print(b().append(3))'` came back at exit 1 with Gap I.2's sentence,
// `string-method append on a receiver that is not a text the compiler can read: b(...)`, while the
// bound spelling — `ys = b()` / `ys.append(3)` — had printed the reference's bytes since ADR 0333 gave
// the answer a heap handle and a record. One program, two answers, and the road chosen by whether the
// program happened to name the object.
//
// The door rewrites the written call into the same call written against the rooted name the answer was
// recorded under — `containerAnswerSubscript`'s judgement (ADR 0334) one construct further out — and
// then the position lowers that node through the roads that answer `ys.append(3)`: the mutator road,
// the bounds raise, the set's hash, the pop door, the container printer. It serves what those roads
// serve for a container VARIABLE and nothing more (Gap R.174's readers stay refused, for both
// spellings), and a refusal it does not take keeps the sentence naming the receiver the PROGRAM wrote.
//
// Every expectation here is what a live `python3` printed for the same source, never what this file
// remembers (Gap N's standing rule).

// TestAMethodOnACallResultAnswersThroughTheCLI is the answering half: the same source through the
// reference and through the CLI, agreeing line for line.
func TestAMethodOnACallResultAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def b():\n    return [1, 2]\n\nprint(b().append(3))",
		"def b():\n    return [1, 2]\n\nb().append(3)\nprint(b())",
		"def b():\n    return [1, 2]\n\nprint(b().append(3), b().append(4))",
		"def b():\n    return [1, 2]\n\nprint(b().pop())",
		"def b():\n    return [1, 2]\n\nprint(b().pop(0), b().pop())",
		"def b():\n    return [\"a\", \"b\"]\n\nprint(b().pop())",
		"def b():\n    return [1, \"a\", None]\n\nprint(b().pop(), b().pop())",
		"def b():\n    return [2, 1]\n\nprint(b().sort())",
		"def b():\n    return [1, 2]\n\nprint(b().reverse())",
		"def s():\n    return {1}\n\nprint(s().add(3))",
		"def s():\n    return {1, 2}\n\ns().discard(1)\nprint(s())",
		"def s():\n    return {1, 2}\n\ns().remove(2)\nprint(s())",
		"def s():\n    return {1, 2}\n\ns().clear()\nprint(s())",
		"def b():\n    return [1, 2]\n\nprint(str(b().append(3)), f\"{b().reverse()}\")",
		"def b():\n    return [1, 2]\n\nprint(\"v\", b().append(3), \"w\")",
		// The answer the slot table could not describe still takes the mutator: the object is the
		// runtime's, and the mutator asks the object (Gap R.261, ADR 0344).
		"def g():\n    return 5\n\ndef b():\n    xs = []\n    xs.append(g())\n    return xs\n\nb().append(3)\nprint(b())",
		// The aliasing rule the row had to measure before it opened the door: the answer is a FRESH
		// object per call, and the binding copies it, so a mutation reaches nobody else's view.
		"def b():\n    return [1, 2]\n\nys = b()\nys.append(3)\nprint(ys, b())",
		"def b():\n    return [1, 2]\n\nfor i in range(2):\n    b().append(i)\nprint(b())",
		"def b():\n    return [1, 2]\n\ndef use():\n    b().append(7)\n    return b()\n\nprint(use())",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s\n%s", stderr, stdout)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestAMethodOnACallResultRaisesThroughTheCLI: the mutator's raise is the program's exception — exit 3,
// the reference's class and words on stderr, nothing on stdout — and never exit 2, the code the
// contract reserves for a compiler bug (ADR 0006, ADR 0214).
func TestAMethodOnACallResultRaisesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src      string
		contains string
	}{
		{"def b():\n    return [1, 2]\n\nb().pop(9)", "IndexError: pop index out of range"},
		{"def b():\n    return []\n\nb().pop()", "IndexError: pop from empty list"},
		{"def s():\n    return {1, 2}\n\ns().remove(9)", "KeyError: 9"},
		{"def b():\n    return [1, 2]\n\nprint(b().pop(9))", "IndexError: pop index out of range"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 3 {
				t.Fatalf("exit %d, want 3 (an uncaught program exception): stdout %q stderr %q", code, stdout, stderr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("wrote %q before the raise; the reference writes nothing", stdout)
			}
			if !strings.Contains(stderr, tt.contains) {
				t.Fatalf("stderr %q does not carry the reference's %q", stderr, tt.contains)
			}
		})
	}
}

// TestAMethodOnACallResultIsCatchableThroughTheCLI asks the same raise of the program.
func TestAMethodOnACallResultIsCatchableThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def b():\n    return [1, 2]\n\ntry:\n    b().pop(9)\nexcept IndexError:\n    print(\"caught\")",
		"def b():\n    return []\n\ntry:\n    b().pop()\nexcept IndexError as e:\n    print(\"e\", e)",
		"def s():\n    return {1, 2}\n\ntry:\n    s().remove(9)\nexcept KeyError:\n    print(\"miss\")",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the handler takes this raise\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestAPositionThatCannotMutateACallResultRefusesThroughTheCLI is the owed half, judged on the sentence
// as well as the exit code: a refusal has to name the missing thing, and the missing thing is not the
// name the door invented for the materialised answer (`__cansN`) — an agent reading the JSON would go
// looking for a name the source never contains (ADR 0166, ADR 0006).
func TestAPositionThatCannotMutateACallResultRefusesThroughTheCLI(t *testing.T) {
	t.Parallel()
	// The door declines a shape the container roads do not serve, and the POSITION keeps the sentence
	// its own road prints — which names the receiver the source spelled (`d(...)`, `b(...)`), never the
	// name the door invented for the materialised answer (`__cans7`). The bound spelling refuses with
	// the container-name sentence (Gap R.174's); the two sentences are the same refusal about the same
	// missing door, and both are asserted to name the program rather than the compiler.
	for _, tt := range []struct {
		name     string
		src      string
		mustSay  string
		mustName string
		gap      string
	}{
		{"a dict asked for a value", "def d():\n    return {1: 2}\n\nprint(d().get(3))", "receiver that is not a text the compiler can read", "d(...)", "Gap R.538"},
		{"a dict asked for its keys", "def d():\n    return {1: 2}\n\nprint(d().keys())", "receiver that is not a text the compiler can read", "d(...)", "Gap R.538"},
		{"a dict asked to forget an entry", "def d():\n    return {1: 2}\n\nprint(d().popitem())", "receiver that is not a text the compiler can read", "d(...)", "Gap R.538"},
		{"a dict cleared", "def d():\n    return {1: 2}\n\nd().clear()\nprint(d())", "receiver that is not a text the compiler can read", "d(...)", "Gap R.538"},
		{"a list asked where a member sits", "def b():\n    return [1, 2]\n\nprint(b().index(2))", "receiver that is not a text the compiler can read", "b(...)", "Gap R.538"},
		{"a list counted", "def b():\n    return [1, 2]\n\nprint(b().count(1))", "receiver that is not a text the compiler can read", "b(...)", "Gap R.538"},
		{"a list copied", "def b():\n    return [1, 2]\n\nprint(b().copy())", "receiver that is not a text the compiler can read", "b(...)", "Gap R.538"},
		// The bound spelling of the very same missing door, kept beside it: the container-name sentence,
		// naming the VARIABLE the program wrote (Gap R.174's standing).
		{"a dict reader on a bound name", "def d():\n    return {1: 2}\n\nys = d()\nprint(ys.get(3))", "which the program bound to a container rather than a text", "ys", "Gap R.174 / Gap R.538"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("%s: the toolchain failed on its own program:\n%s", tt.name, stderr)
			}
			if code != 1 {
				t.Fatalf("%s: exit %d, want 1 (the road refuses, %s): stdout %q stderr %q", tt.name, code, tt.gap, stdout, stderr)
			}
			all := stdout + stderr
			if strings.Contains(all, "__cans") || strings.Contains(all, "__mans") {
				t.Fatalf("%s: the refusal quotes the door's invented name:\n%s", tt.name, all)
			}
			if !strings.Contains(all, tt.mustSay) {
				t.Fatalf("%s: the refusal does not say %q:\n%s", tt.name, tt.mustSay, all)
			}
			if !strings.Contains(all, tt.mustName) {
				t.Fatalf("%s: the refusal does not name the receiver the program wrote (%s):\n%s", tt.name, tt.mustName, all)
			}
		})
	}
}

// TestTheRefusalOnAMethodAnAnswerCannotServeReachesAnAgentAsJSON is the machine path (ADR 0006,
// ADR 0346): the same refusal, consumable without scraping prose.
func TestTheRefusalOnAMethodAnAnswerCannotServeReachesAnAgentAsJSON(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := cliStreams(t, "--json", "--eval", "def d():\n    return {1: 2}\n\nprint(d().get(3))")
	if code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr)
	}
	var doc struct {
		OK         bool   `json:"ok"`
		Exit       int    `json:"exit"`
		Phase      string `json:"phase"`
		Backend    string `json:"backend"`
		Error      string `json:"error"`
		Capability *struct {
			Code    string   `json:"code"`
			Shape   string   `json:"shape"`
			Suggest string   `json:"suggestion"`
			Rows    []string `json:"rows"`
		} `json:"capability"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(stdout)), &doc); err != nil {
		t.Fatalf("the refusal is not JSON: %v\n%s", err, stdout)
	}
	if doc.OK || doc.Exit != 1 || doc.Phase != "codegen" || doc.Backend != "aot" {
		t.Fatalf("the refusal document is wrong: %s", stdout)
	}
	if doc.Capability == nil || !strings.HasPrefix(doc.Capability.Code, "lower.unsupported.") {
		t.Fatalf("no capability code reached the agent: %s", stdout)
	}
	if strings.Contains(doc.Error, "__cans") {
		t.Fatalf("the machine-readable refusal quotes the door's invented name: %s", doc.Error)
	}
}

// TestTheTupleAnswerRefusesTheMutatorThroughTheCLI keeps L11.3's immutability gate in front of the
// door at the CLI: the reference raises, and a road that appended happily would print a value no
// program of this language can print.
func TestTheTupleAnswerRefusesTheMutatorThroughTheCLI(t *testing.T) {
	t.Parallel()
	stdout, stderr, code := cliStreams(t, "--eval", "def t():\n    return (1, 2)\n\nt().append(4)")
	if code == 2 {
		t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
	}
	if code == 0 {
		t.Fatalf("appending to a tuple answered %q; the reference raises AttributeError", stdout)
	}
	if strings.Contains(stdout+stderr, "__cans") {
		t.Fatalf("the refusal quotes the door's invented name:\n%s", stdout+stderr)
	}
}

// TestEveryMethodOnACallResultModuleVerifies is the exit-2 guard from the outside: `--verify-llvm`
// reports the module verifier's own verdict for each answered shape.
func TestEveryMethodOnACallResultModuleVerifies(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def b():\n    return [1, 2]\n\nprint(b().append(3))",
		"def b():\n    return [1, 2]\n\nprint(b().pop())",
		"def b():\n    return [1, \"a\", None]\n\nprint(b().pop(), b().pop())",
		"def s():\n    return {1, 2}\n\ns().add(3)\nprint(s())",
		"def s():\n    return {1, 2}\n\nprint(s().clear())",
		"def b():\n    return [1, 2]\n\nprint(f\"{b().append(3)}\")",
		"def b():\n    return [1, 2]\n\nb().pop(9)",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--verify-llvm", src)
			if code != 0 {
				t.Fatalf("the module verifier refused the program (exit %d):\n%s\n%s", code, stdout, stderr)
			}
		})
	}
}

// TestWhatTheMethodDoorStillOwesAtTheCLI keeps the neighbours visible with the reference's bytes beside
// them: the void at the condition and binding doors (Gap R.537) and the reader methods the container
// roads do not serve for a VARIABLE either (Gap R.538).
func TestWhatTheMethodDoorStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		// wantExit is what THIS backend does today; the reference's answer is derived live.
		wantExit int
		gap      string
	}{
		{
			name:     "a mutator used as a condition",
			src:      "def b():\n    return [1, 2]\n\nif b().append(3):\n    print(\"y\")\nelse:\n    print(\"n\")",
			wantExit: 2,
			gap:      "Gap R.537 (the condition door lowers the void's missing word into an `icmp`)",
		},
		{
			name:     "a mutator bound to a name",
			src:      "def b():\n    return [1, 2]\n\nx = b().append(3)\nprint(x)",
			wantExit: 2,
			gap:      "Gap R.537 (the binding door lowers it into a `store`)",
		},
		{
			name:     "a dict reader on an answer",
			src:      "def d():\n    return {1: 2}\n\nprint(d().get(3))",
			wantExit: 1,
			gap:      "Gap R.538 (the reader roads refuse for a container VARIABLE too)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, code := cliStreams(t, "--eval", tc.src)
			if code != tc.wantExit {
				t.Fatalf("%q: exit %d, the row records exit %d for %s", tc.src, code, tc.wantExit, tc.gap)
			}
		})
	}
}
