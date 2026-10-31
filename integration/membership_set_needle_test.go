package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// A set asked of a set, through the CLI (roadmap Gap R.586, ADR 0478).
//
//	 print({1} in {1, 2})      # CPython False · this toolchain: TypeError: unhashable type: 'set', exit 3
//	 print({1} in {1: 2})      # CPython TypeError: unhashable type: 'set' · the same raise, the right door
//	 print([1] in {1, 2})      # CPython TypeError: unhashable type: 'list' · and this one never moved
//	 print((1, {2}) in {1, 2}) # CPython TypeError: unhashable type: 'set' · a CARRYING set still raises
//
// A SET converts a set needle to the frozenset it hashes as (`set_contains`, `set_discard`, `set_remove`) and
// answers; a dict asks the key's own hash and refuses. The unhashable rule was never missing here — the
// run-time guard had been carrying the set arm off since **ADR 0425** measured the same asymmetry for
// `discard`/`remove` — it was placed at the wrong door, in front of every answer, by the compile-time gate.
//
// Every expectation below is what `python3` printed for the same source, read inside the case (Gap N's
// standing rule); the raise rows are judged the way the exit-code contract judges a raise — exit 3, silence on
// stdout, the reference's sentence on stderr, and never the compiler's own `codegen:` prefix (**ADR 0006**,
// **ADR 0166**, **ADR 0214**).

// setNeedleCLIAnswered are the shapes the door must leave answering, including the negative controls: a set
// asked of a LIST is the content question and is True, and a plain member is a plain member.
var setNeedleCLIAnswered = []struct {
	name string
	src  string
}{
	{"the row's own program", "print({1} in {1, 2})"},
	{"a named haystack", "s = {1, 2}\nprint({1} in s)"},
	{"a named needle", "n = {1}\nprint(n in {1, 2})"},
	{"both named", "n = {1}\ns = {1, 2}\nprint(n in s)"},
	{"a haystack that is a call's answer", "def g():\n    return {1, 2}\nprint({1} in g())"},
	{"a needle that is a call's answer", "def f():\n    return {1}\nprint(f() in {1, 2})"},
	{"negated", "s = {1, 2}\nprint({1} not in s)"},
	{"in a condition", "s = {1, 2}\nif {1} in s:\n    print(\"yes\")\nelse:\n    print(\"no\")"},
	{"in and/or", "print({1} in {1, 2} and 1 or 2)"},
	{"a handle beside the number it could be mistaken for", "s = {0}\nprint({0} in s)"},
	{"the empty spellings", "print(set() in {1, 2})\nprint({1} in set())"},
	{"a set asked of the list that holds it", "print({1} in [{1}])"},
	{"a set asked of a built list", "xs = [{1}, {2}]\nprint({2} in xs)"},
	{"a plain member", "print(1 in {1, 2})"},
	{"a text member", "print(\"a\" in {\"a\"})"},
	{"a tuple needle", "print((1,) in {1, 2})"},
}

func TestASetAskedOfASetAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range setNeedleCLIAnswered {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, tt.src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program, so a refusal is the wrong answer\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestANeedleTheReferenceRefusesStillRaisesThroughTheCLI is the half that must not be relaxed with the
// exemption. The four sentences are the reference's, and the exit class is the program's: 3, not 2 — a
// compiler bug is what ADR 0166 reserves that code for.
func TestANeedleTheReferenceRefusesStillRaisesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		message string
	}{
		{"a list asked of a set", "print([1] in {1, 2})", "TypeError: unhashable type: 'list'"},
		{"a named list asked of a set", "n = [1]\ns = {1, 2}\nprint(n in s)", "TypeError: unhashable type: 'list'"},
		{"a dict asked of a set", "print({1: 2} in {1, 2})", "TypeError: unhashable type: 'dict'"},
		{"a set asked of a dict", "print({1} in {1: 2})", "TypeError: unhashable type: 'set'"},
		{"a set asked of a named dict", "d = {1: 2}\nprint({1} in d)", "TypeError: unhashable type: 'set'"},
		{"a tuple carrying a set", "print((1, {2}) in {1, 2})", "TypeError: unhashable type: 'set'"},
		{"a tuple carrying a list", "print(([1],) in {1, 2})", "TypeError: unhashable type: 'list'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("the toolchain spent its own bug code on a program the reference stops:\n%s", stderr)
			}
			if code != 3 {
				t.Fatalf("exit %d, want 3: an exception the program owns leaves through the trap road\nstdout %q stderr %q", code, stdout, stderr)
			}
			if strings.TrimSpace(stdout) != "" {
				t.Fatalf("stdout %q, want silence before the trap", stdout)
			}
			if !strings.Contains(stderr, tc.message) {
				t.Fatalf("stderr %q does not carry %q", stderr, tc.message)
			}
			if strings.Contains(stderr, "codegen:") || strings.Contains(stderr, "*lang.") {
				t.Fatalf("the raise is told in the compiler's words, not the program's:\n%s", stderr)
			}
			_, pyErr, err := lang.PythonRun(tc.src + "\n")
			if err == nil || !strings.Contains(pyErr, tc.message) {
				t.Fatalf("the reference does not raise %q for %q:\n%s", tc.message, tc.src, pyErr)
			}
		})
	}
}

// The raise belongs to the program: its own `except TypeError` reaches it, and the exempt pair does not
// reach the handler at all (ADR 0214).
func TestAHashSetNeedleRaiseIsCatchableThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		{"a list needle caught", "try:\n    print([1] in {1, 2})\nexcept TypeError:\n    print(\"caught\")", "caught"},
		{"a set key caught", "try:\n    print({1} in {1: 2})\nexcept TypeError:\n    print(\"caught\")", "caught"},
		{"a tuple-carried set caught", "try:\n    print((1, {2}) in {1, 2})\nexcept TypeError:\n    print(\"caught\")", "caught"},
		{"the exempt pair never reaches the handler", "try:\n    print({1} in {1, 2})\nexcept TypeError:\n    print(\"caught\")", "False"},
		{"the bound exception names the needle", "try:\n    print([1] in {1, 2})\nexcept TypeError as err:\n    print(err)", "unhashable type: 'list'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, tc.src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code != 0 {
				t.Fatalf("exit %d, want 0\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) || got != tc.want {
				t.Fatalf("printed %q, the row says %q and the reference prints %q", got, tc.want, strings.TrimSpace(want))
			}
		})
	}
}

// The order the reference keeps, asserted through the CLI because it is an ORDER: an operand's effect runs
// before the answer, and the exemption must not delete it.
func TestAHashSetNeedleRunsWhatTheOperandsDoThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"the needle's call runs", "def nz():\n    print(\"needle\")\n    return {1}\nprint(nz() in {1, 2})"},
		{"the haystack's call runs", "def hs():\n    print(\"hay\")\n    return {1, 2}\nprint({1} in hs())"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, tc.src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code != 0 {
				t.Fatalf("exit %d, want 0\nstderr %q", code, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// TestTheMembershipSetNeedleProgramMatchesTheReferenceThroughTheCLI is the row's one-program witness: the
// whole family, byte for byte, on the same path a user and an agent take.
func TestTheMembershipSetNeedleProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/membership_set_needle.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/membership_set_needle.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/membership_set_needle.gy")
	if code == 2 {
		t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
	}
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr %q", code, stderr)
	}
	got := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	wantLines := strings.Split(strings.TrimRight(string(want), "\n"), "\n")
	if len(got) != len(wantLines) {
		t.Fatalf("the program printed %d lines, the reference printed %d:\n%s", len(got), len(wantLines), strings.Join(got, "\n"))
	}
	for i := range wantLines {
		if got[i] != wantLines[i] {
			t.Fatalf("line %d: the program answered %q where CPython answers %q\n%s", i+1, got[i], wantLines[i], strings.Join(got, "\n"))
		}
	}
}

// TestTheMembershipSetNeedleProgramIsOnTheRecord is ADR 0302's rule at this altitude: a program whose record
// disappeared fails here rather than passing by having nothing to compare against.
func TestTheMembershipSetNeedleProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/membership_set_needle.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("programs/membership_set_needle.gy has no recorded answer: the record leg cannot check a program that is not on it")
	}
}

// What the door still cannot reach, judged through the CLI: each row prints what the reference prints or is
// refused with the sentence the registry owns, and the row's own gap ID is in the failure message.
func TestWhatTheHashSetNeedleDoorStillCannotAnswerThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		refuses bool   // the compiled leg refuses it (exit 1, `codegen:` sentence) today
		raises  string // the compiled leg raises this sentence today (exit 3)
		oracle  string // what CPython prints ("" when the reference raises)
		gap     string
	}{
		{
			name:   "a haystack bound in an arm is a set at run time",
			src:    "def no():\n    return False\ns = {1, 2}\nif no():\n    s = {\"a\": 1}\nprint({1} in s)",
			raises: "TypeError: unhashable type: 'set'",
			oracle: "False",
			gap:    "roadmap Gap R.588 (a name the binder walk cannot settle keeps today's raise; the tag that travels with the value is roadmap L11.1's)",
		},
		{
			name:   "a needle asked of a text is not this door's question",
			src:    "print({1} in \"abc\")",
			raises: "",
			oracle: "TypeError: 'in <string>' requires string as left operand, not set",
			gap:    "roadmap Gap R.587 (the text door hands a container GLOBAL to `rt_str_contains`, which is an exit-2 module today)",
		},
		{
			name:    "a set built by copying a container never reaches the needle",
			src:     "print({1} in set([1, 2]))",
			refuses: true,
			oracle:  "False",
			gap:     "the `set(<container>)` copy door (the capability registry's copy family, roadmap L11.8)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			switch {
			case tc.refuses:
				if code != 1 {
					t.Fatalf("exit %d, want the refusal (1): %s\nstdout %q stderr %q", code, tc.gap, stdout, stderr)
				}
			case tc.raises != "":
				if code != 3 || !strings.Contains(stderr, tc.raises) {
					t.Fatalf("exit %d with stderr %q; the row records the raise %q (%s)", code, stderr, tc.raises, tc.gap)
				}
			default:
				if code == 2 {
					// The exit-2 face is the row, not a failure of it: `llc` rejecting the module is what
					// Gap R.587 files, and the row fails the day the toolchain starts ANSWERING.
					return
				}
				if code != 0 {
					t.Fatalf("exit %d, want 0 for the shape the row still owes (%s)\nstderr %q", code, tc.gap, stderr)
				}
				// Only a shape the compiled leg ANSWERED is compared to the reference; a refusal or a raise
				// was already judged by its own branch above, and comparing its (empty) stdout would
				// certify the absence of an answer as an answer.
				want := pythonOutput(t, tc.src+"\n")
				if strings.TrimSpace(stdout) != strings.TrimSpace(want) {
					t.Fatalf("printed %q where the reference prints %q (%s)", strings.TrimSpace(stdout), strings.TrimSpace(want), tc.gap)
				}
			}
		})
	}
}
