package integration

// len_text_slice_test.go — the length of a slice of a text, through the CLI and against the reference
// (roadmap **Gap R.240**, **ADR 0470**).
//
// `s = "abcd"` / `print(len(s[1:3]))` is CPython's `2`. Until this row it stopped the build with
// `codegen: len requires an inline list/dict/set literal` at **exit 1** — a sentence about a container the
// program never mentions, in a program whose only literal is a text — while the two roads beside it both
// answered: `print(len(s))` printed `4` and `print(s[1:3])` printed `bc`. The characters were therefore
// already in the compiler twice over; the length door had no arm that went and asked.
//
// Every expectation below was measured against the pinned CPython in the same case that asserts it — the
// answered rows re-run `python3` so the reference column cannot rot, and the raise rows read CPython's own
// last traceback line rather than quoting one here. Exit 2 is forbidden on every row: it is the code the
// contract reserves for a compiler bug (**ADR 0166**), and an earlier draft of this arm reached it by
// emitting a measurement after the raise's terminator.

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// lenTextSliceCLIRows are the shapes the reference answers.
var lenTextSliceCLIRows = []struct {
	src  string
	want string
}{
	// the row's own definition of done
	{"s = \"abcd\"\nprint(len(s[1:3]))", "2"},
	// A slot of the list an ORDERING BUILTIN answered now measures the text inside it: the container door
	// builds the list and the slot carries its own kind (**ADR 0472**; **Gap R.567**'s builtin half, paid;
	// the METHOD half below is still owed).
	{"print(len(sorted([\"b\", \"a\"])[0]))", "1"},
	{"print(len(\"abcd\"[1:3]))", "2"},
	{"s = \"abcd\"\nprint(len(s[::-1]))", "4"},

	// every bound shape the fold and the helper share
	{"s = \"abcd\"\nprint(len(s[1:]))", "3"},
	{"s = \"abcd\"\nprint(len(s[:]))", "4"},
	{"s = \"abcd\"\nprint(len(s[::2]))", "2"},
	{"s = \"abcd\"\nprint(len(s[::-2]))", "2"},
	{"print(len(\"abcd\"[1:3:2]))", "1"},
	{"s = \"abcdefgh\"\nprint(len(s[1:6:2]))", "3"},
	{"s = \"abcd\"\nprint(len(s[-2:]))", "2"},
	{"s = \"abcd\"\nprint(len(s[-3:-1]))", "2"},
	{"s = \"abcd\"\nprint(len(s[10:20]))", "0"},
	{"s = \"abcd\"\nprint(len(s[3:1]))", "0"},
	{"s = \"\"\nprint(len(s[1:3]))", "0"},
	{"s = \"abcd\"\nprint(len(s[1:3][0:1]))", "1"},
	{"s = \"abcd\"\nprint(len(s[1:3][::-1]))", "2"},
	{"s = \"abcd\"\nprint(len(s[::-1][::-1]))", "4"},

	// bounds that are VALUES take the `rt_str_slice` road and must answer the same number
	{"s = \"abcd\"\ni = 1\nj = 3\nprint(len(s[i:j]))", "2"},
	{"s = \"abcd\"\nn = 2\nprint(len(s[:n]))", "2"},
	{"s = \"abcd\"\nprint(len(s[1:3]) if False else len(s[1:]))", "3"},
	{"s = \"hello world\"\nprint(len(s[6:]))", "5"},
	{"s = \"hello world\"\nprint(len(s[:5]))", "5"},

	// code points, never bytes — a byte count prints 3 and 5 and is wrong about its own string
	{"s = \"café\"\nprint(len(s))", "4"},
	{"s = \"café\"\nprint(len(s[2:]))", "2"},
	{"s = \"héllo\"\nprint(len(s[1:3]))", "2"},
	{"s = \"日本語です\"\nprint(len(s[1:3]))", "2"},

	// the base is whatever the text road can name
	{"def f(t):\n    return len(t[1:3])\nprint(f(\"abcdef\"))", "2"},
	{"def g(t, i, j):\n    return len(t[i:j])\nprint(g(\"abcdef\", 1, 4))", "3"},
	{"def h():\n    return \"abcdef\"\nprint(len(h()[1:3]))", "2"},
	{"print(len(\"abcdef\".upper()[0:3]))", "3"},
	{"s = \"abcd\"\nprint(len(s.upper()[1:3]))", "2"},
	{"s = \"abcd\"\nprint(len(s[1:3].upper()))", "2"},
	{"s = \"abcd\"\nt = s[1:3]\nprint(len(t))", "2"},

	// the answer is an int, so every position an int sits in has to take it
	{"s = \"abcd\"\nprint(len(s[1:3]) + len(s))", "6"},
	{"s = \"abcd\"\nprint(len(s[1:3]) == 2)", "True"},
	{"s = \"abcd\"\nprint(bool(len(s[0:0])))", "False"},
	{"s = \"abcd\"\nif len(s[1:3]) == 2:\n    print(\"two\")", "two"},
	{"s = \"abcd\"\nprint([len(s[1:3]), len(s)])", "[2, 4]"},
	{"s = \"abcd\"\nprint({\"n\": len(s[1:3])})", "{'n': 2}"},
	{"s = \"abcd\"\nfor n in [len(s[1:3])]:\n    print(n)", "2"},
}

// lenTextSliceCLIRaiseRows are the programs the reference stops on. `sentence` is the last line of its
// traceback, read from CPython inside the case as well as asserted here.
var lenTextSliceCLIRaiseRows = []struct {
	src      string
	sentence string
}{
	{"s = \"abcd\"\nprint(len(s[\"a\":\"b\"]))", "TypeError: slice indices must be integers or None or have an __index__ method"},
	{"s = \"abcd\"\nprint(len(s[1.5:2]))", "TypeError: slice indices must be integers or None or have an __index__ method"},
	{"s = \"abcd\"\nprint(len(s[[1]:2]))", "TypeError: slice indices must be integers or None or have an __index__ method"},
	{"s = \"abcd\"\nprint(len(s[1:3:0]))", "ValueError: slice step cannot be zero"},
	{"s = \"abcd\"\nprint(len(s[::0]))", "ValueError: slice step cannot be zero"},
	{"st = {1, 2}\nprint(len(st[0:1]))", "TypeError: 'set' object is not subscriptable"},
	{"d = {\"a\": 1}\nprint(len(d[0:1]))", "KeyError: slice(0, 1, None)"},
	{"print(len(1[0:2]))", "TypeError: 'int' object is not subscriptable"},
	{"print(len(1.5[0:2]))", "TypeError: 'float' object is not subscriptable"},
	{"print(len(None[0:1]))", "TypeError: 'NoneType' object is not subscriptable"},
	{"print(len(True[0:1]))", "TypeError: 'bool' object is not subscriptable"},
	{"xs = [1, 2, 3]\nprint(len(xs[\"a\":\"b\"]))", "TypeError: slice indices must be integers or None or have an __index__ method"},
}

// lenTextSliceCLIHandlerRows are the same traps through a handler: the raise went out through the ordinary
// raise road, so the arm runs and the program leaves at 0 (**ADR 0211**).
var lenTextSliceCLIHandlerRows = []struct {
	src  string
	want string
}{
	{"s = \"abcd\"\ntry:\n    print(len(s[\"a\":\"b\"]))\nexcept TypeError as e:\n    print(e)", "slice indices must be integers or None or have an __index__ method"},
	{"s = \"abcd\"\ntry:\n    print(len(s[::0]))\nexcept ValueError as e:\n    print(e)", "slice step cannot be zero"},
	{"st = {1, 2}\ntry:\n    print(len(st[0:1]))\nexcept TypeError:\n    print(\"caught\")", "caught"},
	{"d = {\"a\": 1}\ntry:\n    print(len(d[0:1]))\nexcept KeyError as e:\n    print(\"miss\", e)", "miss slice(0, 1, None)"},
	{"s = \"abcd\"\ntry:\n    print(len(s[::0]))\nexcept ValueError:\n    print(\"right arm\")\nexcept TypeError:\n    print(\"wrong arm\")", "right arm"},
}

// lenTextSliceCLIRefusals are what this row does NOT repair, with the reference's answer beside the
// refusal, so the day a road grows one of these the row fails and moves rather than rots.
var lenTextSliceCLIRefusals = []struct {
	name    string
	src     string
	must    string // a word the refusal has to contain (ADR 0249: a refusal names what it could not do)
	refWant string // what the reference answers
	gap     string
}{
	{
		"a step that is a VALUE",
		"s = \"abcd\"\nk = 2\nprint(len(s[::k]))",
		"cannot fold string slice", "2",
		"roadmap Gap R.243 (`rt_str_slice` takes a low and a high and no stride; the length door now repeats the slice road's own sentence, so the reader is sent to the road that declined rather than to a container the program never wrote — and `len` answers the day the stride road lands, with no change here)",
	},
	{
		"a module text a BODY reads",
		"s = \"abcd\"\ndef body():\n    print(len(s[1:3]))\nbody()",
		"len requires", "2",
		"roadmap Gap R.402 (the doors read the MODULE's container records, not the frame's — `print(s[1:3])` in a body refuses for the same reason, and the length door inherits that refusal rather than inventing one)",
	},
	{
		"a module list a BODY reads",
		"xs = [\"ab\", \"cd\"]\ndef body():\n    print(len(xs[0:1]))\nbody()",
		"len requires", "1",
		"roadmap Gap R.402 (the same door, list side: the copy road wants a base the records describe)",
	},
	{
		"a slot of the list a METHOD answered",
		"s = \"hello world\"\nprint(len(s.split(\" \")[1]))",
		"len requires", "5",
		"roadmap L11.1 (the object is on the heap and walkable; what is missing is the (payload, tag) pair coming back out of the road that built it)",
	},
}

// TestTheLengthOfATextSliceAnswersThroughTheCLI runs every answered row through the CLI and through
// CPython in the same case, so the reference column cannot drift from the program it cites.
func TestTheLengthOfATextSliceAnswersThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range lenTextSliceCLIRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference runs: %s\n%s", tt.want, stderr)
			}
			if code != 0 {
				t.Fatalf("%q left %d: %s\nstdout %q", tt.src, code, strings.TrimSpace(stderr), stdout)
			}
			if got := strings.TrimRight(stdout, "\n"); got != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want)
			}
			if py := strings.TrimRight(pythonOutput(t, tt.src+"\n"), "\n"); py != tt.want {
				t.Fatalf("the CPython column of this row is stale: it answers %q, the row says %q", py, tt.want)
			}
		})
	}
}

// TestTheLengthOfATextSliceRaisesThroughTheCLIWhatTheReferenceRaises pins the class and the whole sentence
// on the uncaught path, and the exit code the contract reserves for a trap. These are the shapes where a
// `len` that measured first would have printed a number at exit 0.
func TestTheLengthOfATextSliceRaisesThroughTheCLIWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tt := range lenTextSliceCLIRaiseRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference stops on: the contract's compiler-bug code\n%s", stderr)
			}
			if code == 0 {
				t.Fatalf("%q answered %q at exit 0 where the reference raises %s", tt.src, stdout, tt.sentence)
			}
			if code != 3 {
				t.Fatalf("%q left %d; a catchable trap is exit 3 (ADR 0166's contract)\n%s", tt.src, code, stderr)
			}
			if got := lastTracebackLine(stderr); got != tt.sentence {
				t.Fatalf("%q wrote %q; the reference writes %q", tt.src, got, tt.sentence)
			}
			_, refErr, _ := lang.PythonRun(tt.src + "\n")
			if got := lastTracebackLine(refErr); got != tt.sentence {
				t.Fatalf("the row's sentence is not what CPython writes: %q vs %q (traceback %q)", got, tt.sentence, refErr)
			}
		})
	}
}

// TestTheLengthOfATextSliceTrapIsCaughtByItsHandlerThroughTheCLI witnesses the catchable half.
func TestTheLengthOfATextSliceTrapIsCaughtByItsHandlerThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range lenTextSliceCLIHandlerRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code != 0 {
				t.Fatalf("%q left %d (the handler was meant to run): %s\nstdout %q", tt.src, code, strings.TrimSpace(stderr), stdout)
			}
			if got := strings.TrimRight(stdout, "\n"); got != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestNoTextSliceLengthIsMeasuredInBytes is the IR witness at the CLI altitude: the count is asked of the
// string table through the code-point helper for every answered shape, and the byte helper appears in the
// program's own functions only where a door that owns it emits it. `main` alone would let the runtime's own
// helper definitions satisfy the check, so the read is scoped to the program's functions.
func TestNoTextSliceLengthIsMeasuredInBytes(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"s = \"abcd\"\nprint(len(s[1:3]))",
		"s = \"abcd\"\nprint(len(s[::-1]))",
		"s = \"abcd\"\ni = 1\nj = 3\nprint(len(s[i:j]))",
		"s = \"café\"\nprint(len(s[2:]))",
		"s = \"日本語です\"\nprint(len(s[1:3]))",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			body := programFunctionBodies(t, res.IR)
			if !strings.Contains(body, "call i32 @rt_str_nchars(") {
				t.Fatalf("%q never asked the string table how many characters the slice holds:\n%s", src, body)
			}
			if strings.Contains(body, "call i32 @rt_str_len(") {
				t.Fatalf("%q measured BYTES (`rt_str_len`) where the reference counts code points:\n%s", src, body)
			}
		})
	}
	// Counter-rows: the helpers this test forbids and requires are both live in this backend, so the two
	// assertions above are facts about the arm and not about a runtime that was never emitted.
	counters := []struct {
		src  string
		want string
	}{
		{"def f():\n    return \"abc\"\nif f():\n    print(\"t\")", "call i32 @rt_str_len("},
		{"xs = [1, 2, 3]\nprint(len(xs[0:2]))", "call i32 @rt_slice("},
	}
	for _, tc := range counters {
		t.Run("counter-row "+strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			body := programFunctionBodies(t, res.IR)
			if !strings.Contains(body, tc.want) {
				t.Fatalf("%q no longer emits %s; the check above would then be satisfied by nothing:\n%s", tc.src, tc.want, body)
			}
		})
	}
	// And the print road, which renders a text slice without ever measuring it, must stay that way: the
	// question has one owner.
	t.Run("counter-row the print road does not measure", func(t *testing.T) {
		t.Parallel()
		res, err := lang.Compile("s = \"abcd\"\nprint(s[1:3])")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		if body := programFunctionBodies(t, res.IR); strings.Contains(body, "call i32 @rt_str_nchars(") {
			t.Fatalf("the print road now measures the slice too — one question, one owner:\n%s", body)
		}
	})
}

// TestTheLengthOfATextSliceProgramAnswersNothingTheReferenceDoesNot is the corpus program, line for line
// against what CPython printed for it.
func TestTheLengthOfATextSliceProgramAnswersNothingTheReferenceDoesNot(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/len_text_slice.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/len_text_slice.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/len_text_slice.gy")
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

// TestTheLengthOfATextSliceProgramIsOnTheRecord is **ADR 0302**'s rule at this altitude.
func TestTheLengthOfATextSliceProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/len_text_slice.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if !lang.HasGoldenAnswer(string(src)) {
		t.Fatal("integration/programs/len_text_slice.gy is not on the record — a corpus program is recorded like any other source")
	}
}

// TestWhatTheLengthOfATextSliceRowStillCannotAnswerThroughTheCLI is the filed half. A refusal is only
// acceptable when it names the road that declined (**ADR 0249**), and none of these may reach exit 2.
func TestWhatTheLengthOfATextSliceRowStillCannotAnswerThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range lenTextSliceCLIRefusals {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("exit 2 on a program the reference answers with %s: %s\n%s", tc.refWant, tc.src, stderr)
			}
			if code == 0 {
				t.Fatalf("%q answered %q; the row expects the filed refusal to stand (the reference answers %s)", tc.src, stdout, tc.refWant)
			}
			if code != 1 {
				t.Fatalf("%q left %d; a front-end refusal is exit 1\n%s", tc.src, code, stderr)
			}
			if !strings.Contains(stderr, tc.must) {
				t.Fatalf("%q refused with %q, which does not name the road that declined (ADR 0249); filed under %s", tc.src, strings.TrimSpace(stderr), tc.gap)
			}
			if strings.Contains(stderr, "unsupported") {
				t.Errorf("%q refused with %q, which names nothing (ADR 0249)", tc.src, strings.TrimSpace(stderr))
			}
		})
	}
}
