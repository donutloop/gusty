package lang

// text_slice_len_test.go — the witnesses for the length of a slice of a text (roadmap **Gap R.240**,
// **ADR 0470**).
//
// Every `want` below is what `python3` prints for that exact program, measured beside the compiled leg
// before it was written down; every raise row carries the reference's class AND its whole sentence, and a
// row that paraphrases the message is not a witness. The two legs the contract allows are both asked: the
// record (`goldenLookup`, `HasGoldenAnswer` — a missing record fails the case, **ADR 0302**) and the
// reference (the CLI, in `integration/len_text_slice_test.go`).
//
// What the rows are load-bearing against:
//   - a `len` door with no arm for a text slice — every answered row comes back
//     `len requires an inline list/dict/set literal`, a sentence about a container the program never wrote;
//   - a door that counted BYTES — `"café"[2:]` would print 3 rather than 2, and every UTF-8 row below fails;
//   - a door that counted the whole base instead of the slice — `len(s[1:3])` would print 4;
//   - a door that counted BEFORE the gates — the raise rows come back as numbers at exit 0, which is the
//     one class of bug this loop is not allowed to trade a refusal for (**ADR 0166**, **ADR 0211**);
//   - a door that kept a SECOND copy of the slice road instead of asking it — the parity rows below (the
//     same program with constant bounds and with value bounds, and the slice bound to a name) are exactly
//     where two copies drift apart, and the IR rows pin that the answer is asked of the object rather than
//     re-derived from a spelling.

import (
	"strings"
	"testing"
)

// textSliceLenAnswered is the family the reference answers and this toolchain must answer identically. The
// three rows of the roadmap's definition of done are the first three.
var textSliceLenAnswered = []struct {
	src  string
	want string
}{
	// ---- the row's own definition of done ----
	{"s = \"abcd\"\nprint(len(s[1:3]))", "2"},
	{"print(len(\"abcd\"[1:3]))", "2"},
	{"s = \"abcd\"\nprint(len(s[::-1]))", "4"},

	// ---- every bound shape the fold and the helper share ----
	{"s = \"abcd\"\nprint(len(s[1:]))", "3"},
	{"s = \"abcd\"\nprint(len(s[:]))", "4"},
	{"s = \"abcd\"\nprint(len(s[::2]))", "2"},
	{"s = \"abcd\"\nprint(len(s[::-2]))", "2"},
	{"print(len(\"abcd\"[1:3:2]))", "1"},
	{"s = \"abcdefgh\"\nprint(len(s[1:6:2]))", "3"},
	{"s = \"abcd\"\nprint(len(s[-2:]))", "2"},
	{"s = \"abcd\"\nprint(len(s[-3:-1]))", "2"},
	{"s = \"abcd\"\nprint(len(s[10:20]))", "0"}, // out of range selects nothing, and nothing has no characters
	{"s = \"abcd\"\nprint(len(s[3:1]))", "0"},   // an empty range is 0, not a negative count
	{"s = \"\"\nprint(len(s[1:3]))", "0"},

	// ---- bounds that are VALUES: the `rt_str_slice` road, the same question ----
	{"s = \"abcd\"\ni = 1\nj = 3\nprint(len(s[i:j]))", "2"},
	{"s = \"abcd\"\nn = 2\nprint(len(s[:n]))", "2"},
	{"s = \"hello world\"\nprint(len(s[6:]))", "5"},

	// ---- CODE POINTS, never bytes (ADR 0225): a byte count would say 3 and 5 ----
	{"s = \"café\"\nprint(len(s[2:]))", "2"},
	{"s = \"héllo\"\nprint(len(s[1:3]))", "2"},

	// ---- the answer is an int, so every position an int sits in must take it ----
	{"s = \"abcd\"\nprint(len(s[1:3]) + len(s))", "6"},
	{"s = \"abcd\"\nprint(len(s[1:3]) + len(s[1:3]))", "4"},
	{"s = \"abcd\"\nprint(len(s[1:3]) == 2)", "True"},
	{"s = \"abcd\"\nprint(bool(len(s[1:3])))", "True"},
	{"s = \"abcd\"\nif len(s[1:3]) == 2:\n    print(\"two\")", "two"},
	{"s = \"abcd\"\nprint([len(s[1:3]), len(s)])", "[2, 4]"},
	{"s = \"abcd\"\nprint({\"n\": len(s[1:3])})", "{'n': 2}"},

	// ---- the base is whatever the text road can name: a parameter, a call's answer, a method's answer ----
	{"def f(t):\n    return len(t[1:3])\nprint(f(\"abcdef\"))", "2"},
	{"def g(t, i, j):\n    return len(t[i:j])\nprint(g(\"abcdef\", 1, 4))", "3"},
	{"def h():\n    return \"abcdef\"\nprint(len(h()[1:3]))", "2"},
	{"print(len(\"abcdef\".upper()[0:3]))", "3"},
	{"s = \"abcd\"\nprint(len(s.upper()[1:3]))", "2"},
	{"s = \"abcd\"\nprint(len(s[1:3].upper()))", "2"},

	// ---- a slice of a slice: the second door reads the text the first one selected ----
	{"s = \"abcd\"\nprint(len(s[1:3][0:1]))", "1"},
	{"s = \"abcd\"\nprint(len(s[1:3][::-1]))", "2"},
}

// textSliceLenRaiseRows are the programs the reference STOPS on. Each is a shape where counting first
// would have printed a number: the bounds gate, the step gate and the base gate all live in the slice road,
// and the length door inherits them because it asks that road rather than imitating it (ADR 0336, ADR 0336's
// step, ADR 0466/ADR 0468's base gate).
var textSliceLenRaiseRows = []struct {
	src     string
	class   string
	message string
}{
	{"s = \"abcd\"\nprint(len(s[\"a\":\"b\"]))", "TypeError", "slice indices must be integers or None or have an __index__ method"},
	{"s = \"abcd\"\nprint(len(s[1.5:2]))", "TypeError", "slice indices must be integers or None or have an __index__ method"},
	{"s = \"abcd\"\nprint(len(s[[1]:2]))", "TypeError", "slice indices must be integers or None or have an __index__ method"},
	{"s = \"abcd\"\nprint(len(s[1:3:0]))", "ValueError", "slice step cannot be zero"},
	{"s = \"abcd\"\nprint(len(s[::0]))", "ValueError", "slice step cannot be zero"},
	{"st = {1, 2}\nprint(len(st[0:1]))", "TypeError", "'set' object is not subscriptable"},
	{"d = {\"a\": 1}\nprint(len(d[0:1]))", "KeyError", "slice(0, 1, None)"},
	{"print(len(1[0:2]))", "TypeError", "'int' object is not subscriptable"},
	{"print(len(None[0:1]))", "TypeError", "'NoneType' object is not subscriptable"},
	{"print(len(True[0:1]))", "TypeError", "'bool' object is not subscriptable"},
	{"xs = [1, 2, 3]\nprint(len(xs[\"a\":\"b\"]))", "TypeError", "slice indices must be integers or None or have an __index__ method"},
}

// textSliceLenCatchRows are the same raises through a handler: the raise is the ordinary emitted one, so
// the arm runs and the program leaves at 0 (**ADR 0211**).
var textSliceLenCatchRows = []struct {
	src  string
	want string
}{
	{"s = \"abcd\"\ntry:\n    print(len(s[\"a\":\"b\"]))\nexcept TypeError as e:\n    print(e)", "slice indices must be integers or None or have an __index__ method"},
	{"s = \"abcd\"\ntry:\n    print(len(s[::0]))\nexcept ValueError as e:\n    print(e)", "slice step cannot be zero"},
	{"st = {1, 2}\ntry:\n    print(len(st[0:1]))\nexcept TypeError:\n    print(\"caught\")", "caught"},
	{"d = {\"a\": 1}\ntry:\n    print(len(d[0:1]))\nexcept KeyError as e:\n    print(\"miss\", e)", "miss slice(0, 1, None)"},
	{"s = \"abcd\"\ntry:\n    print(len(s[::0]))\nexcept ValueError:\n    print(\"right arm\")\nexcept TypeError:\n    print(\"wrong arm\")", "right arm"},
}

// textSliceLenParityRows are the row's real claim about the extraction: one question — how many characters
// does the text this slice selects hold — asked of ONE owner. Each entry is the same program written so the
// text arrives by a different road; two counting rules would disagree here, and the reference cannot tell
// them apart either.
var textSliceLenParityRows = []struct {
	name string
	a    string
	b    string
	want string
}{
	{
		"constant bounds, value bounds, and the slice bound to a name",
		"s = \"abcd\"\nprint(len(s[1:3]))",
		"s = \"abcd\"\ni = 1\nj = 3\nprint(len(s[i:j]))",
		"2",
	},
	{
		"the slice measured where it stands, and after a binding",
		"s = \"abcd\"\nprint(len(s[1:3]))",
		"s = \"abcd\"\nt = s[1:3]\nprint(len(t))",
		"2",
	},
	{
		"a UTF-8 slice measured through the fold and through the table",
		"s = \"café\"\nprint(len(s[2:]))",
		"s = \"café\"\nk = 2\nprint(len(s[k:]))",
		"2",
	},
	{
		"a reversed slice measured folded, and measured through its own reversal",
		"s = \"abcd\"\nprint(len(s[::-1]))",
		"s = \"abcd\"\nprint(len(s[::-1][::-1]))",
		"4",
	},
}

// textSliceLenUntouchedRows are the neighbours the new arm must not move: a list slice's length, a text
// variable's length, a method answer's length, the slice the PRINT road answers, and the containers whose
// length roads read a count field. Every one answered before the arm existed; an arm written in the wrong
// place takes one of them over.
var textSliceLenUntouchedRows = []struct {
	src  string
	want string
}{
	{"xs = [1, 2, 3]\nprint(len(xs[0:2]))", "2"},
	{"xs = [1, 2, 3]\nprint(len(xs[::2]))", "2"},
	{"xs = [1, 2, 3, 4]\nprint(len(xs[1:6:2]))", "2"},
	{"xs = [1.5, \"a\", [7]]\nprint(len(xs[0:2]))", "2"},
	{"xs = [\"ab\", \"cd\"]\nprint(len(xs[0:1]))", "1"},
	{"xs = [\"ab\", \"cd\"]\nprint(len(xs[0:1][0]))", "2"},
	{"d = {\"a\": 1, \"b\": 2}\nprint(len(d))", "2"},
	{"print(len([1, 2, 3]))", "3"},
	{"print(len({1, 2}))", "2"},
	{"print(len(\"abcd\"))", "4"},
	{"print(len(\"café\"))", "4"},
	{"s = \"abcd\"\nprint(len(s))", "4"},
	{"s = \"café\"\nprint(len(s))", "4"},
	{"def f(t):\n    print(len(t))\nf(\"café\")", "4"},
	{"w = \"hello world\"\nprint(len(w.split(\" \")))", "2"},
	{"print(len(\"abcd\".upper()))", "4"},
	{"s = \"abcd\"\nprint(s[1:3])", "bc"},
	{"print(\"abcdef\"[::-1])", "fedcba"},
	{"xs = [1, 2, 3]\nprint(xs[0:2])", "[1, 2]"},
	{"print(len(\"abcd\"[1:3]) == 2)", "True"}, // the folded text's length and the slice's own print agree
}

// textSliceLenFiledNotFixed is what this row does NOT repair, measured the same day, each with the
// reference's answer beside the refusal this toolchain gives.
var textSliceLenFiledNotFixed = []struct {
	src     string
	refWant string // what `python3` prints — written here so a reader can see the gap
	want    string // the word the refusal must contain, so a refusal that names nothing fails (ADR 0249)
}{
	{
		// A step that is a VALUE: `rt_str_slice` takes a low and a high and no stride, so the fold answers
		// a constant step and refuses any other. What this row changed is WHICH sentence the reader gets:
		// `len` now repeats the slice road's own words instead of blaming a container, and the day
		// **Gap R.243** lands the stride road, `len` answers it with no change to this file.
		"s = \"abcd\"\nk = 2\nprint(len(s[::k]))", "2", "cannot fold string slice",
	},
	{
		// A name the BODY does not own: the module bound the text, the frame's notebooks never saw it, and
		// the slice road declines the base — which is **Gap R.402**'s own family (`print(s[1:3])` in a body
		// refuses today, and `len` inherits exactly that refusal rather than inventing its own).
		"s = \"abcd\"\ndef body():\n    print(len(s[1:3]))\nbody()", "2", "len requires",
	},
	{
		// The same door, list side: the copy road needs the base the records describe.
		"xs = [\"ab\", \"cd\"]\ndef body():\n    print(len(xs[0:1]))\nbody()", "1", "len requires",
	},
	{
		// A slot of the list a METHOD answered: the object is on the heap and walkable, and what is missing
		// is the (payload, tag) pair coming back out of the road that built it — the tagged word, L11.1.
		"s = \"hello world\"\nprint(len(s.split(\" \")[1]))", "5", "len requires",
	},
	{
		// The same shape spelled with a builtin call instead of a method call.
		"print(len(sorted([\"b\", \"a\"])[0]))", "1", "len requires",
	},
}

// TestTheLengthOfATextSliceAnswersWhatTheReferenceAnswers runs every answered row through the compiler and
// through the JIT, and refuses to accept a refusal: these programs were front-end refusals before this
// commit, which is not a capability boundary the suite should learn to tolerate (**ADR 0249**).
func TestTheLengthOfATextSliceAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range textSliceLenAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference answers %q and the answer was already in this backend twice over — the fold holds the characters and the table counts them",
					tt.src, got, tt.want+"\n")
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q through the compiled leg, want %q", tt.src, got, tt.want+"\n")
			}
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the reference prints %q", tt.src, trap, tt.want)
			}
			if out != tt.want {
				t.Fatalf("%q printed %q through the JIT, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestTheLengthOfATextSliceIsMeasuredByTheTableIsTheRowsIR is the IR witness: the count is asked of the
// object through `rt_str_nchars`, which is the helper `len` already asks for a text VARIABLE — so the
// folded spelling and the value-bounded spelling cannot be given two different rules. The counter-rows are
// what make the assertions mean something: a module that called neither helper would satisfy a bare
// absence, and a door that asked the BYTE helper would satisfy the presence check.
func TestTheLengthOfATextSliceIsMeasuredByTheTableIsTheRowsIR(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"s = \"abcd\"\nprint(len(s[1:3]))",
		"s = \"abcd\"\nprint(len(s[::-1]))",
		"s = \"abcd\"\ni = 1\nj = 3\nprint(len(s[i:j]))",
		"s = \"café\"\nprint(len(s[2:]))",
		"s = \"hello world\"\nprint(len(s[6:]))",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			body := mainBodyOf(mustCompileIR(t, src))
			if !strings.Contains(body, "call i32 @rt_str_nchars(") {
				t.Fatalf("%q never asked the string table how many characters the slice holds — the answer was counted somewhere else, which is a second rule:\n%s", src, body)
			}
			if strings.Contains(body, "call i32 @rt_str_len(") {
				t.Fatalf("%q measured BYTES (`rt_str_len`) where the reference counts code points:\n%s", src, body)
			}
		})
	}
	// Counter-rows: the helpers exist and are used by their own doors, so the checks above are facts about
	// this arm and not facts about a runtime that was never emitted.
	for _, tc := range []struct {
		src  string
		want string
	}{
		// the truth test's own helper — the byte helper this arm must not reach for
		{"def f():\n    return \"abc\"\nif f():\n    print(\"t\")", "call i32 @rt_str_len("},
		// the print road answers a text slice with NO length call at all
		{"s = \"abcd\"\nprint(s[1:3])", "@rt_str_nchars("},
		// a list copy's length asks the copy road, not the string table
		{"xs = [1, 2, 3]\nprint(len(xs[0:2]))", "call i32 @rt_slice("},
	} {
		t.Run("counter-row "+strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			body := mainBodyOf(mustCompileIR(t, tc.src))
			switch tc.want {
			case "@rt_str_nchars(":
				if strings.Contains(body, tc.want) {
					t.Fatalf("%q (the print road, not the length door) now measures the slice too — the question has to have one owner:\n%s", tc.src, body)
				}
			default:
				if !strings.Contains(body, tc.want) {
					t.Fatalf("%q no longer emits %s; the counter-row has stopped being a counter-row and the absence above proves nothing:\n%s", tc.src, tc.want, body)
				}
			}
		})
	}
}

// TestTheLengthOfATextSliceAsksTheGatesFirst witnesses the order: a program the reference stops on may not
// print a number. The raise table is asked of the compiled trap AND of the record.
func TestTheLengthOfATextSliceAsksTheGatesFirst(t *testing.T) {
	t.Parallel()
	for _, tt := range textSliceLenRaiseRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class {
				t.Fatalf("%q raised %s: %s; the reference raises %s", tt.src, class, message, tt.class)
			}
			if message != tt.message {
				t.Fatalf("%q wrote %q; the reference writes %q", tt.src, message, tt.message)
			}
		})
	}
}

// TestTheLengthOfATextSliceRaiseIsCatchable witnesses the same raises through a handler — the raise went out
// through the ordinary raise road, so an `except` arm takes it and the program leaves at 0.
func TestTheLengthOfATextSliceRaiseIsCatchable(t *testing.T) {
	t.Parallel()
	for _, tt := range textSliceLenCatchRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, trap := runForParity(t, tt.src)
			if trap != "" {
				t.Fatalf("%q raised %s; the handler was meant to run", tt.src, trap)
			}
			if out != tt.want {
				t.Fatalf("%q printed %q, want %q", tt.src, out, tt.want)
			}
		})
	}
}

// TestTheLengthOfATextSliceRaisesWithoutMeasuringAnything is the negative of the IR witness on the raise
// paths: a program the reference stops on must not emit the measurement at all — not as a fall-through,
// and not as an instruction past the raise's terminator. The answered row beside each one is the
// counter-row, so the absence is earned by the arm and not by a deleted road.
func TestTheLengthOfATextSliceRaisesWithoutMeasuringAnything(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"s = \"abcd\"\nprint(len(s[\"a\":\"b\"]))",
		"s = \"abcd\"\nprint(len(s[::0]))",
		"st = {1, 2}\nprint(len(st[0:1]))",
		"d = {\"a\": 1}\nprint(len(d[0:1]))",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			mod := mustCompileIR(t, src)
			body := mainBodyOf(mod)
			if strings.Contains(body, "call i32 @rt_str_nchars(") {
				t.Fatalf("%q measured a text the reference never reached:\n%s", src, body)
			}
			if !strings.Contains(mod, "rt_die") && !strings.Contains(mod, "@rt_raise") {
				t.Fatalf("%q emitted no raise at all; the shape is the program's exception, not a compiler refusal:\n%s", src, body)
			}
		})
	}
	// The counter-row: the answered shape DOES call the helper, so the loop above is not satisfied by a
	// `len` that never measures anything.
	body := mainBodyOf(mustCompileIR(t, "s = \"abcd\"\nprint(len(s[1:3]))"))
	if !strings.Contains(body, "call i32 @rt_str_nchars(") {
		t.Fatalf("the counter-row no longer measures the slice, so the raise rows above prove nothing:\n%s", body)
	}
}

// TestTheSliceRoadAndTheLengthDoorAgree runs the parity table: the same program with its text arriving by
// another road, which is where two counting rules would part company.
func TestTheSliceRoadAndTheLengthDoorAgree(t *testing.T) {
	t.Parallel()
	for _, tc := range textSliceLenParityRows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, refused := compiledOutOrRefusal(t, tc.a)
			if refused {
				t.Fatalf("%q refused (%s)", tc.a, a)
			}
			b, refused := compiledOutOrRefusal(t, tc.b)
			if refused {
				t.Fatalf("%q refused (%s)", tc.b, b)
			}
			if a != tc.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tc.a, a, tc.want+"\n")
			}
			if b != tc.want+"\n" {
				t.Fatalf("%q printed %q — the same program, measured a different way: %q vs %q", tc.b, b, a, tc.want+"\n")
			}
		})
	}
}

// TestTheLengthOfATextSliceRowIsRecorded is the record leg's own guard: every answered, raise and handler
// row has an entry in `testdata/interpreter-golden.json`, so neither half of the row can be witnessed by
// the compiled leg alone and neither can be retired by deleting its evidence (**ADR 0302**).
func TestTheLengthOfATextSliceRowIsRecorded(t *testing.T) {
	t.Parallel()
	for _, tt := range textSliceLenAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if !HasGoldenAnswer(tt.src) {
				t.Fatalf("%q is not on the record: a newly answered program is recorded from the reference, not self-certified (**ADR 0302**)", tt.src)
			}
			rec := goldenLookup(t, tt.src)
			if !rec.HasStd || rec.Stdout != tt.want+"\n" {
				t.Fatalf("%q: the record holds %q, the reference prints %q", tt.src, rec.Stdout, tt.want+"\n")
			}
		})
	}
	for _, tt := range textSliceLenRaiseRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.ExnType == "" && rec.ExnMsg == "" {
				t.Fatalf("%q is not on the record as a raise; record it with `go run -tags=llvm20 ./tools/recmerge -sources …` (**ADR 0302**)", tt.src)
			}
			if rec.ExnType != tt.class {
				t.Errorf("%q: the record raised %q, the reference raises %q", tt.src, rec.ExnType, tt.class)
			}
			if rec.ExnMsg != tt.message {
				t.Errorf("%q: the record wrote %q, the reference writes %q", tt.src, rec.ExnMsg, tt.message)
			}
		})
	}
	for _, tt := range textSliceLenCatchRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.Stdout != tt.want+"\n" {
				t.Fatalf("%q: the record printed %q, the handler prints %q", tt.src, rec.Stdout, tt.want+"\n")
			}
		})
	}
	for _, tc := range textSliceLenParityRows {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, src := range []string{tc.a, tc.b} {
				if !HasGoldenAnswer(src) {
					t.Fatalf("%q is not on the record", src)
				}
			}
		})
	}
}

// TestTheTextSliceDoorIsTheOneThatAnswersItself asks the predicate that decides the arm directly, of the
// node the parser built — so a change in the door is a failing row and not a wrong number somewhere else.
// The nil generator is the soundness row: a door that answered "this is a text" with nothing to read from
// would hand `len` a count of a heap handle.
func TestTheTextSliceDoorIsTheOneThatAnswersItself(t *testing.T) {
	t.Parallel()
	// Each row carries the notebooks the binding road would have written for it, so the door is asked of a
	// generator in the state the program puts it in — a `t` the caller handed a text to is a text, and a `t`
	// the caller handed a list to is not.
	type doorCase struct {
		src string
		gen *irGen
	}
	yes := []doorCase{
		{"print(len(\"abcd\"[1:3]))", &irGen{}},        // the base is the literal the source wrote
		{"print(len(\"ab\"[1:]))", &irGen{}},           // and an open bound changes nothing
		{"print(len(\"abc\".upper()[0:2]))", &irGen{}}, // the base is a text method's answer
		{"s = \"abcd\"\nprint(len(s[1:3]))", &irGen{strVals: map[string]string{"s": "abcd"}}},
		{"s = \"abcd\"\nprint(len(s[::2]))", &irGen{strVals: map[string]string{"s": "abcd"}}},
		{"s = \"abcd\"\nprint(len(s[::-1]))", &irGen{strVals: map[string]string{"s": "abcd"}}},
		{"s = \"abcd\"\nprint(len(s[1:3][0:1]))", &irGen{strVals: map[string]string{"s": "abcd"}}},
		{"def f(t):\n    print(len(t[1:3]))", &irGen{internedVars: map[string]bool{"t": true}}},
	}
	no := []doorCase{
		{"xs = [1, 2, 3]\nprint(len(xs[0:2]))", &irGen{listVars: map[string]bool{"xs": true}}}, // the copy road's
		{"xs = [1, 2, 3]\nprint(len(xs[::2]))", &irGen{listVars: map[string]bool{"xs": true}}},
		{"d = {\"a\": 1}\nprint(len(d[0:1]))", &irGen{runtimeDicts: map[string]bool{"d": true}}},
		{"st = {1, 2}\nprint(len(st[0:1]))", &irGen{runtimeSets: map[string]bool{"st": true}}},
		{"print(len(1[0:2]))", &irGen{}},
		{"print(len([1, 2][0:1]))", &irGen{}},
		{"def f(t):\n    print(len(t[0:2]))", &irGen{listVars: map[string]bool{"t": true}}},
	}
	for _, tc := range yes {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			sl := firstTextSliceIn(t, tc.src)
			if !tc.gen.sliceRoadCallsItAText(sl) {
				t.Fatalf("%q: the door declined a slice of a text the records name", tc.src)
			}
		})
	}
	for _, tc := range no {
		t.Run("declines "+strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			sl := firstTextSliceIn(t, tc.src)
			if tc.gen.sliceRoadCallsItAText(sl) {
				t.Fatalf("%q: the door claimed a slice that is not a text, and `len` would measure it as one", tc.src)
			}
		})
	}
	// The soundness rows: NO generator at all, and a generator with no records at all. Both must agree with
	// each other — never crash, and never grant permission to measure something unnamed. The shapes asked
	// here are the ones whose base needs no notebook (a literal, a text method's answer), because a name is
	// legitimately declined by a generator that has never seen the binding.
	var noGen *irGen
	for _, src := range []string{
		"print(len(\"abcd\"[1:3]))",
		"print(len(\"ab\"[1:]))",
		"print(len(\"abc\".upper()[0:2]))",
		"print(len(1[0:2]))",
		"print(len([1, 2][0:1]))",
	} {
		sl := firstTextSliceIn(t, src)
		if noGen.sliceRoadCallsItAText(sl) {
			t.Errorf("%q: a road with NO generator at all claimed a text (the guard is a crash guard, and it must not grant)", src)
		}
	}
	// The empty generator answers the shapes the NODE settles — a literal's slice, a text method's answer —
	// because those need no notebook; it declines the ones a notebook settles. Both halves are the point:
	// the door reads records, and where a record is what is missing it says so by declining.
	for _, src := range []string{"print(len(\"abcd\"[1:3]))", "print(len(\"ab\"[1:]))", "print(len(\"abc\".upper()[0:2]))"} {
		sl := firstTextSliceIn(t, src)
		if !(&irGen{}).sliceRoadCallsItAText(sl) {
			t.Errorf("%q: a generator with no records declined a text the SOURCE itself names", src)
		}
	}
	// And the door asked of a name it has never seen is closed, whatever the source claims.
	for _, src := range []string{"s = \"abcd\"\nprint(len(s[1:3]))", "def f(t):\n    print(len(t[1:3]))"} {
		sl := firstTextSliceIn(t, src)
		if noGen.sliceRoadCallsItAText(sl) || (&irGen{}).sliceRoadCallsItAText(sl) {
			t.Errorf("%q: a door with no records called the base a text", src)
		}
	}
}

// TestWhatTheLengthOfATextSliceLeavesStanding runs the neighbours: the list copy's length, the containers'
// count fields, the text variable's length, the print road's own answer.
func TestWhatTheLengthOfATextSliceLeavesStanding(t *testing.T) {
	t.Parallel()
	for _, tt := range textSliceLenUntouchedRows {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if want, isTrap := strings.CutPrefix(tt.want, "trap:"); isTrap {
				class, message := jitTrap(t, tt.src)
				if got := class + ": " + message; got != want {
					t.Fatalf("%q raised %q; the neighbour's own sentence is %q", tt.src, got, want)
				}
				return
			}
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); this neighbour answered before the arm landed, and the arm must not take it over", tt.src, got)
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
		})
	}
}

// TestTheLengthOfATextSliceRefusesInWordsIsFiledNotFixed keeps the row honest about its edges: a refusal is
// only acceptable when it says which road declined and which question, and each of these is filed with the
// reference's answer beside it (**ADR 0249**).
func TestTheLengthOfATextSliceRefusesInWordsIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range textSliceLenFiledNotFixed {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if !refused {
				t.Fatalf("%q answered %q; the row expects the filed refusal to stand (the reference does %s)", tc.src, out, tc.refWant)
			}
			if strings.Contains(out, "unsupported") {
				t.Errorf("%q refused with %q, which names nothing (**ADR 0249**)", tc.src, out)
			}
			if tc.want != "" && !strings.Contains(out, tc.want) {
				t.Fatalf("%q refused with %q, which does not say which road declined (ADR 0249)", tc.src, out)
			}
		})
	}
}

// firstTextSliceIn returns the program's first slice node, so the door can be asked of the AST the parser
// actually built rather than of a node assembled by hand.
func firstTextSliceIn(t *testing.T, src string) *Slice {
	t.Helper()
	prog, err := parseProgram(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	var sl *Slice
	var walkE func(Expr)
	var walkS func(Stmt)
	walkE = func(e Expr) {
		if e == nil || sl != nil {
			return
		}
		switch v := e.(type) {
		case *Slice:
			sl = v
			return
		case *Index:
			walkE(v.Obj)
			walkE(v.Idx)
		case *Call:
			walkE(v.Fn)
			for _, a := range v.Args {
				walkE(a)
			}
		case *BinOp:
			walkE(v.L)
			walkE(v.R)
		case *UnOp:
			walkE(v.X)
		case *ListLit:
			for _, el := range v.Elems {
				walkE(el)
			}
		case *Tuple:
			for _, el := range v.Elems {
				walkE(el)
			}
		}
	}
	walkS = func(s Stmt) {
		if sl != nil {
			return
		}
		switch v := s.(type) {
		case *ExprStmt:
			walkE(v.Expr)
		case *AssignStmt:
			walkE(v.Target)
			walkE(v.Value)
		case *IfStmt:
			walkE(v.Cond)
			for _, st := range v.Then {
				walkS(st)
			}
			for _, st := range v.Else {
				walkS(st)
			}
		case *WhileStmt:
			walkE(v.Cond)
			for _, st := range v.Body {
				walkS(st)
			}
		case *ForStmt:
			walkE(v.Iter)
			for _, st := range v.Body {
				walkS(st)
			}
		case *ReturnStmt:
			walkE(v.Expr)
		case *FuncDef:
			for _, st := range v.Body {
				walkS(st)
			}
		case *TryStmt:
			for _, st := range v.Body {
				walkS(st)
			}
			for _, ex := range v.Excepts {
				for _, st := range ex.Body {
					walkS(st)
				}
			}
			for _, st := range v.Finally {
				walkS(st)
			}
		}
	}
	for _, s := range prog.Stmts {
		walkS(s)
	}
	if sl == nil {
		t.Fatalf("%q contains no slice for the door to be asked of", src)
	}
	return sl
}
