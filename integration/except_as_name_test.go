package integration

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// This file is the REFERENCE leg of **ADR 0406** (roadmap Gap R.113): what the LINKED PROGRAM answers where
// an `except` clause names what it caught. The pkg/lang side of the row (except_as_name_test.go) asks the
// codegen its IR claim; this side asks the program its bytes, against CPython, on the execution backend the
// CLI offers. Every answered row was recorded from the reference by tools/recmerge first
// (TestTheCaughtNameFamilyIsOnTheRecord): the compiled leg is the thing under test and never writes its own
// answers (ADR 0309), and the shapes it could not agree on are the pinned table below, not the record.
//
// The bug this row closes is a hole in the grammar, and it is the largest one the exit-code contract has to
// offer: `except ZeroDivisionError as err:` — the idiom the reference's own documentation leads with — died
// at `parse error at 3:26: expected ":"` with exit 1 on every path, because the parser read the type name and
// then asked for a colon (roadmap Gap R.113, measured 2026-10-02). A program the reference runs is refused at
// the door by a grammar that never had the clause.
//
// What the arm's name holds is the decision ADR 0406 records: the reference hands the arm an exception
// OBJECT, and this backend's value is one word wide, so the name is bound to what that object WRITES — the
// message the raise carried, interned through the one door every other text uses (`@exn_arg`, written beside
// the `"<Type>: <message>"` report the uncaught path prints, so a message containing ": " loses nothing).
// `print(err)`, `str(err)`, `f"{err}"`, `return err` and the print of that return are then one fact, told by
// one record.
//
// What the row still owes is pinned below rather than hidden: the object the reference keeps answering about
// — its `repr`, its `args`, its type, the name's life after the arm, and the operand doors that `len`, a text
// method and `==` reach — is roadmap Gap R.395.

// exceptAsCLICase is one answered program of the family: what the source writes, and what the reference prints.
type exceptAsCLICase struct {
	name string
	src  string
	want string
}

// exceptAsCLIRows is the answered family at this altitude: each raise family that names itself, the message a
// `raise` carried, the positions that read the name, and the control rows that name nothing. Every `want` is
// what the pinned CPython printed for the same source (Gap N's standing rule), re-asked at run time below so a
// stale expectation fails rather than passing quietly.
func exceptAsCLIRows() []exceptAsCLICase {
	return []exceptAsCLICase{
		{"the row's own program", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err)", "division by zero\n"},
		{"a builtin's ValueError", "try:\n    int(\"abc\")\nexcept ValueError as err:\n    print(err)", "invalid literal for int() with base 10: 'abc'\n"},
		{"an out-of-range index names itself", "xs = [1]\ntry:\n    print(xs[9])\nexcept IndexError as err:\n    print(err)", "list index out of range\n"},
		{"a missing key says which key", "d = {}\ntry:\n    print(d[\"k\"])\nexcept KeyError as err:\n    print(err)", "'k'\n"},
		{"the message a raise carried", "try:\n    raise ValueError(\"boom\")\nexcept ValueError as err:\n    print(err)", "boom\n"},
		{"a message whose own colon stays", "try:\n    raise ValueError(\"a: b\")\nexcept ValueError as err:\n    print(err)", "a: b\n"},
		{"a raise that carried nothing", "try:\n    raise ValueError\nexcept ValueError as err:\n    print(\"[\" + str(err) + \"]\")", "[]\n"},
		{"the name inside an interpolation", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(f\"got: {err}\")", "got: division by zero\n"},
		{"the name at the print door beside another argument", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(\"caught\", err, \"end\")", "caught division by zero end\n"},
		{"str of the name", "try:\n    raise TypeError(\"bad type\")\nexcept TypeError as err:\n    print(str(err))", "bad type\n"},
		{"the arm that matches is the arm that binds", "try:\n    1/0\nexcept TypeError as te:\n    print(\"te\", te)\nexcept ZeroDivisionError as ze:\n    print(\"ze\", ze)", "ze division by zero\n"},
		{"the catch-all arm names it too", "try:\n    1/0\nexcept Exception as err:\n    print(err)", "division by zero\n"},
		{"an arm that calls, then reads the name", "def note():\n    print(\"arm ran\")\n\ntry:\n    1/0\nexcept ZeroDivisionError as err:\n    note()\n    print(\"after\", err)", "arm ran\nafter division by zero\n"},
		{"an inner arm re-raises for the outer arm's name", "try:\n    try:\n        1/0\n    except ZeroDivisionError as inner:\n        print(\"inner\", inner)\n        raise ValueError(\"second\")\nexcept ValueError as outer:\n    print(\"outer\", outer)", "inner division by zero\nouter second\n"},
		{"a function returns the name", "def grab():\n    try:\n        return 1/0\n    except ZeroDivisionError as err:\n        return err\n\nprint(grab())", "division by zero\n"},
		{"a method returns the name, with a finally", "class Box:\n    def check(self):\n        try:\n            return [1][4]\n        except IndexError as err:\n            return err\n        finally:\n            print(\"checked\")\n\nprint(Box().check())", "checked\nlist index out of range\n"},
		{"the name inside a loop body", "i = 0\nwhile i < 2:\n    try:\n        print(7/0)\n    except ZeroDivisionError as err:\n        print(\"loop\", err)\n    i = i + 1", "loop division by zero\nloop division by zero\n"},
		{"the program continues after the arm", "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err)\nprint(\"tail\")", "division by zero\ntail\n"},
		{"a raise from inside a call, caught outside it", "def g():\n    return 1/0\n\ndef f():\n    try:\n        g()\n    except ZeroDivisionError as err:\n        return \"caught \" + str(err)\n\nprint(f())", "caught division by zero\n"},
		{"the control: an arm that names nothing", "try:\n    1/0\nexcept ZeroDivisionError:\n    print(\"caught\")", "caught\n"},
		{"the control: an arm that never runs", "try:\n    print(1)\nexcept ValueError as err:\n    print(\"never\", err)", "1\n"},
	}
}

// TestTheCaughtNameFamilyAnswersLikeTheReference is the reference leg: every answered program prints, linked,
// exactly what CPython prints for the same source, and none of them reaches exit 2.
func TestTheCaughtNameFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range exceptAsCLIRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// One directory per subtest: these cases are parallel, and a shared name written by each of them
			// would let one program's bytes be run under another's name.
			path := writeSrc(t, t.TempDir(), "except_as_name.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q for this program (prints %q) — this table's expectation went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 reserves exit 2 for a compiler bug):\n%s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q — the arm's name was refused:\n%s", engine, code, want, out)
				}
				if out != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", engine, out, want, tc.src)
				}
			}
		})
	}
}

// TestTheCaughtNameFamilyIsOnTheRecord is ADR 0302's rule at this altitude: every program this row answers has
// an answer on record, written from the reference by tools/recmerge and cross-checked against the compiled leg
// — never written by this backend. A row whose record disappeared fails here.
func TestTheCaughtNameFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range exceptAsCLIRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one caught-name program is on the record: the record leg is not being walked")
	}
}

// TestTheCaughtNameProgramIsTheReferenceLineForLine is the conformance corpus's own claim, asked of the file
// rather than of a table: integration/programs/except_as_name.gy prints the reference's bytes for every line,
// with no line dropped between the two.
func TestTheCaughtNameProgramIsTheReferenceLineForLine(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("except_as_name")
	want, ok := cpythonPlainOut(t, t.TempDir(), src)
	if !ok {
		t.Skip("no live python3 to ask")
	}
	for _, engine := range cliEngines {
		path := writeSrc(t, t.TempDir(), "except_as_name.gy", src)
		out, code := cliRunCode(t, engine, path)
		if code != 0 {
			t.Fatalf("%s exited %d: %s", engine, code, out)
		}
		if out != want {
			wl, gl := strings.Split(want, "\n"), strings.Split(out, "\n")
			for i := 0; i < len(wl) || i < len(gl); i++ {
				var w, g string
				if i < len(wl) {
					w = wl[i]
				}
				if i < len(gl) {
					g = gl[i]
				}
				if w != g {
					t.Fatalf("%s differs on line %d: printed %q, CPython prints %q", engine, i+1, g, w)
				}
			}
		}
	}
}

// TestTheCaughtNameProgramIsOnTheRecord keeps the corpus program itself on the record leg.
func TestTheCaughtNameProgramIsOnTheRecord(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("except_as_name")
	if !lang.HasGoldenAnswer(src) {
		t.Fatalf("integration/programs/except_as_name.gy is not on the record — record it from the reference with tools/recmerge")
	}
}

// exceptAsPinned is one shape of the family this row measured and does NOT own: the exit code the linked
// program gives today, the bytes or sentence it gives with it, the reference's answer beside it, and the
// roadmap row that owns the fix. A row that stops being true FAILS, and the case is DELETED rather than
// edited — the only way a pinned wrong answer cannot quietly become a rumour.
type exceptAsPinned struct {
	name     string
	gap      string
	src      string
	code     int
	mustMach string
	want     string
}

func exceptAsPinnedRows() []exceptAsPinned {
	return []exceptAsPinned{
		{
			name: "the repr of what was caught",
			gap:  "Gap R.395",
			src:  "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(repr(err))",
			code: 0,
			want: "ZeroDivisionError('division by zero')\n",
		},
		{
			name: "what was caught, inside a list",
			gap:  "Gap R.395",
			src:  "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print([err])",
			code: 0,
			want: "[ZeroDivisionError('division by zero')]\n",
		},
		{
			// The reference deletes the arm's name when the arm ends; this backend's binding is the scope's,
			// so the name outlives the arm and still says what the raise carried.
			name: "the name read after the arm",
			gap:  "Gap R.395",
			src:  "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(\"x\")\nprint(err)",
			code: 0,
			want: "x\n",
		},
		{
			name: "the caught name copied to a second name, then measured",
			gap:  "Gap R.395",
			src:  "try:\n    1/0\nexcept ZeroDivisionError as err:\n    s = err\nprint(len(s))",
			code: 0,
			want: "",
		},
		{
			name: "membership of what was caught",
			gap:  "Gap R.395",
			src:  "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(\"a\" in err)",
			code: 0,
			want: "",
		},
		{
			// The operand doors refuse rather than answer the message as if it were the object: ADR 0166's
			// preference, and the reference raises through all three.
			name:     "len of what was caught",
			gap:      "Gap R.395",
			src:      "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(len(err))",
			code:     1,
			mustMach: "not a text to measure",
			want:     "",
		},
		{
			name:     "a text method of what was caught",
			gap:      "Gap R.395",
			src:      "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err.upper())",
			code:     1,
			mustMach: "not a text to call a method on",
			want:     "",
		},
		{
			name:     "comparing what was caught with a text",
			gap:      "Gap R.395",
			src:      "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err == \"division by zero\")",
			code:     1,
			mustMach: "not a text to compare against",
			want:     "False\n",
		},
		{
			name:     "the args of what was caught",
			gap:      "Gap R.395",
			src:      "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(err.args)",
			code:     1,
			mustMach: "unsupported attr expression",
			want:     "('division by zero',)\n",
		},
		{
			// Not the arm's fault, and found by asking the arm's name the reference's question: `type` is a
			// reserved word in this lexer, so no program can call it. Roadmap Gap R.396.
			name:     "the type of what was caught",
			gap:      "Gap R.396",
			src:      "try:\n    1/0\nexcept ZeroDivisionError as err:\n    print(type(err))",
			code:     1,
			mustMach: "unexpected token",
			want:     "<class 'ZeroDivisionError'>\n",
		},
		{
			// The reference reprs a KeyError's message — `str(KeyError('a: b'))` is `"'a: b'"`, and so is the
			// line its traceback prints. This backend stores what the raise was handed; the machine raises
			// already say `'k'` and match. Roadmap Gap R.395.
			name: "an explicit KeyError reprs its message in the reference",
			gap:  "Gap R.395",
			src:  "try:\n    raise KeyError(\"a: b\")\nexcept KeyError as err:\n    print(err)",
			code: 0,
			want: "'a: b'\n",
		},
	}
}

// TestWhatTheCaughtNameRoadStillOwesAtTheCLI holds the shapes this row measured and left on the tracker. Three
// assertions per row: the reference answers what the row records, the program produces what the row records on
// stdout, and where the row is a refusal stderr carries the pinned sentence. Where the pinned code is 0 and the
// bytes are the program's own, the row is a WRONG ANSWER held open by a roadmap row, and says so here.
func TestWhatTheCaughtNameRoadStillOwesAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range exceptAsPinnedRows() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "except_as_name_owed.gy", tc.src)
			want, ok := owedReferenceOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want != tc.want {
				t.Fatalf("the reference no longer prints %q for this program (prints %q) — this table's expectation went stale\n%s", tc.want, want, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: %s ended in the code ADR 0166 reserves for a compiler bug:\n%s", engine, tc.gap, stderr)
				}
				if code != tc.code {
					t.Fatalf("%s: %s pinned exit %d, the program exited %d (the reference prints %q)\nstdout %q\nstderr %q",
						engine, tc.gap, tc.code, code, want, stdout, stderr)
				}
				if tc.mustMach != "" {
					if stdout != "" {
						t.Fatalf("%s: %s printed %q on stdout while refusing: a refusal prints nothing\n%s", engine, tc.gap, stdout, tc.src)
					}
					if !strings.Contains(stderr, tc.mustMach) {
						t.Fatalf("%s: %s refused with %q, which does not carry %q", engine, tc.gap, stderr, tc.mustMach)
					}
					continue
				}
				if stdout == want {
					t.Fatalf("%s: %s now prints the reference's %q — the row closed and this pin has to be DELETED, "+
						"with the program moved into the answered table\n%s", engine, tc.gap, want, tc.src)
				}
			}
		})
	}
}

// owedReferenceOut asks the reference what a PINNED program prints. It is cpythonOut with the one difference
// this table needs: most of the shapes below RAISE in the reference — TypeError at `len(err)`, AttributeError at
// `err.upper()`, NameError at the name read after the arm — and a pinned row records the bytes the reference
// wrote before it raised, not the exit code it died on. A machine with no oracle skips, exactly as cpythonOut
// does; nothing here passes by being unasked.
func owedReferenceOut(t *testing.T, path string) (string, bool) {
	t.Helper()
	cmd := oracleCommand(t, path)
	out, err := cmd.Output()
	if err != nil {
		if _, isExit := err.(*exec.ExitError); !isExit {
			t.Fatalf("%s %s could not be run: %v", oracleBinaryName(), path, err)
		}
	}
	return string(out), true
}
