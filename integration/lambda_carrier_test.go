package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// This file is the REFERENCE leg of ADR 0403 (roadmap Gap R.383): what the whole-program reader sees inside
// the three AST positions that are not nodes — a `match` arm, an f-string interpolation, an `except` handler
// — and what the LINKED PROGRAM therefore answers there. The pkg/lang side of the row
// (callable_carrier_test.go) asks the PASS its account; this side asks the PROGRAM its bytes, against
// CPython, on the compiled leg the CLI offers. Every answered row was recorded from the reference by
// tools/recmerge first (TestTheLambdaCarrierFamilyIsOnTheRecord): the compiled leg never writes its own
// answers (ADR 0309), and the ten programs it could not agree on are the pinned table below, not the record.
//
// Every answer row below is one half of a pair measured against the parent commit: the same statement
// written outside the carrier already answered, and the carrier's copy was refused at exit 1 with the
// sentence for a callable that has no word to travel in. Nine programs came over to CPython's bytes that
// way; the pairs are kept here so a regression that reopens only the carrier cannot pass.

type lambdaCarrierCase struct {
	name string
	src  string
	want string
}

// lambdaCarrierPrograms is the answered family: the carriers the reader reads AND the rewrite takes.
func lambdaCarrierPrograms() []lambdaCarrierCase {
	return []lambdaCarrierCase{
		// A `match` arm: pattern, guard and body are not Nodes, so every call in an arm used to be invisible.
		{"use_in_match", "def use(f):\n    return f(2)\n\nmatch 1:\n    case 1:\n        print(use(lambda v: v + 1))\n", "3\n"},
		{"use_in_match_default", "def use(f):\n    return f(2)\n\nmatch 2:\n    case 1:\n        print(\"never\")\n    case _:\n        print(use(lambda v: v - 1))\n", "1\n"},
		{"use_in_match_guard", "def use(f):\n    return f(2)\n\nmatch 4:\n    case v if v > 0:\n        print(use(lambda w: w * v))\n", "8\n"},
		{"ho_in_match", "def apply(f, v):\n    return f(v)\n\nmatch 1:\n    case 1:\n        print(apply(lambda x: x * 2, 3))\n", "6\n"},
		{"ho_in_match_square", "def apply(f, v):\n    return f(v)\n\nmatch 1:\n    case 1:\n        print(apply(lambda x: x * x, 5))\n", "25\n"},
		{"key_in_match", "base = 1\nmatch 1:\n    case 1:\n        print(sorted([3, 1, 2], key=lambda v: v + base))\n", "[1, 2, 3]\n"},
		// An interpolation's field is not a Node either.
		{"use_in_interp", "def use(f):\n    return f(2)\n\nprint(f\"{use(lambda v: v + 1)}\")\n", "3\n"},
		{"ho_in_interp", "def apply(f, v):\n    return f(v)\n\nprint(f\"{apply(lambda x: x * 2, 3)}\")\n", "6\n"},
		{"applied_in_interp", "print(f\"v={(lambda w: w * 2)(4)}\")\n", "v=8\n"},
		{"ho_in_guard_field", "match 4:\n    case v if (lambda w: w > 0)(v):\n        print(f\"got {(lambda w: w * 2)(v)}\")\n", "got 8\n"},
		// The un-carried twins — the parity half of every pair above, and the control rows that answered
		// before this cycle and must keep answering (ADR 0273: a fix is not a verdict).
		{"twin_use_plain", "def use(f):\n    return f(2)\n\nprint(use(lambda v: v + 1))\n", "3\n"},
		{"twin_ho_plain", "def apply(f, v):\n    return f(v)\n\nprint(apply(lambda x: x * 2, 3))\n", "6\n"},
		{"twin_key_plain", "base = 1\nprint(sorted([3, 1, 2], key=lambda v: v + base))\n", "[1, 2, 3]\n"},
		{"twin_applied_plain", "print((lambda w: w * 2)(4))\n", "8\n"},
		// Carriers with no callable in them at all: the reader's descent must not disturb what already worked.
		{"applied_in_match", "match 7:\n    case v:\n        print((lambda w: w * 2)(v))\n", "14\n"},
		{"applied_in_guard", "match 4:\n    case v if (lambda w: w > 2)(v):\n        print((lambda w: w * 3)(v))\n", "12\n"},
		{"helper_in_match", "def helper(v):\n    return v * 2\n\nmatch 1:\n    case 1:\n        print(helper(5))\n", "10\n"},
		{"helper_in_interp", "def helper(v):\n    return v * 2\n\nprint(f\"{helper(5)}\")\n", "10\n"},
		{"rec_in_match", "def fact(n):\n    if n <= 1:\n        return 1\n    return n * fact(n - 1)\n\nmatch 4:\n    case 4:\n        print(fact(4))\n", "24\n"},
	}
}

// lambdaCarrierDeclined holds the one carrier the rewrite leaves alone. The program is REFUSED at exit 1 —
// which is what the pass's gate buys: with the gate removed the same program dies at exit 3 with its own
// IndexError unhandled, because the clone moves the call out of the frame whose trap catch the handler is
// registered on (roadmap Gap R.389). A refusal above an uncaught trap is ADR 0166's ordering, asked here at
// the CLI rather than in the pass's account.
func lambdaCarrierDeclined() []lambdaCarrierCase {
	return []lambdaCarrierCase{
		{"ho_in_handler", "def apply(f, v):\n    return f(v)\n\ntry:\n    print(1)\nexcept ZeroDivisionError:\n    print(apply(lambda x: x * 2, 3))\n", ""},
		{"use_in_handler", "def use(f):\n    return f(2)\n\ntry:\n    print(1)\nexcept IndexError:\n    print(use(lambda v: v + 1))\n", ""},
		{"key_in_handler", "base = 1\ntry:\n    print(1)\nexcept IndexError:\n    print(sorted([3, 1, 2], key=lambda v: v + base))\n", ""},
		{"ho_in_reached_handler", "def apply(f, v):\n    return f(v)\n\ntry:\n    print([1, 2][5])\nexcept IndexError:\n    print(apply(lambda x: x * 2, 3))\n", ""},
	}
}

// lambdaCarrierPinned is what this row measured and does NOT own: the byte the linked program gives today,
// the exit code it gives it with, the reference's answer beside it, and the roadmap row that owns the fix.
// A row that stops being true FAILS — the day a door opens, the pin is DELETED and the program moves to the
// answered table above, which is the only way a pinned wrong answer cannot quietly become a rumour.
type lambdaCarrierPinned struct {
	name     string
	gap      string
	src      string
	code     int    // the exit code the linked program gives today
	today    string // what this backend prints today (pinned bytes)
	want     string // what CPython prints for the same source
	mustMach string // when set, the substring the pinned output has to carry instead of matching it exactly
}

func lambdaCarrierPinnedRows() []lambdaCarrierPinned {
	return []lambdaCarrierPinned{
		// An interpolation's field over a folded container: the handle answers the field (three spellings,
		// one door). These reproduce on the parent commit with and without a `lambda`, which is the evidence
		// that they are the field's road and not this cycle's reader.
		{
			name:  "an interpolation over a keyed sorted() answers the handle",
			gap:   "Gap R.390",
			src:   "base = 1\nprint(f\"{sorted([3, 1, 2], key=lambda v: v + base)}\")\n",
			code:  0,
			today: "0\n",
			want:  "[1, 2, 3]\n",
		},
		{
			name:  "an interpolation over a def-keyed sorted() answers the handle",
			gap:   "Gap R.390",
			src:   "def neg(v):\n    return 0 - v\n\nprint(f\"{sorted([3, 1, 2], key=neg)}\")\n",
			code:  0,
			today: "0\n",
			want:  "[3, 2, 1]\n",
		},
		{
			name:  "an interpolation over an applied lambda that hands back a container answers the handle",
			gap:   "Gap R.390",
			src:   "print(f\"{(lambda xs: xs)([1, 2])}\")\n",
			code:  0,
			today: "0\n",
			want:  "[1, 2]\n",
		},
		// A `def` whose arm's answer IS a folded container: the print door reads the handle, which is the
		// return road's missing registration (Gap R.328's shape, Gap R.67's family) rather than the arm's.
		{
			name:  "a `def` returning a folded container from an arm answers the handle",
			gap:   "Gap R.328",
			src:   "def outer():\n    base = 1\n    match 1:\n        case 1:\n            return sorted([3, 1, 2], key=lambda v: v + base)\n\nprint(outer())\n",
			code:  0,
			today: "0\n",
			want:  "[1, 2, 3]\n",
		},
		// A `match` whose subject is a text takes no arm: silence at the success exit where the reference
		// matches. Measured on the parent commit with no `lambda` in the program too.
		{
			name:  "a `match` on a text subject takes no arm at all",
			gap:   "Gap R.391",
			src:   "def use(f):\n    return f(2)\n\nmatch \"hi\":\n    case \"hi\":\n        print(use(lambda v: v + 1))\n",
			code:  0,
			today: "",
			want:  "3\n",
		},
		// A call written in a handler loses the frame's trap catch: the error the handler exists for walks
		// out at exit 3. No `lambda` is in this program and the parent commit dies the same way, which is
		// what makes it the trap table's row rather than any callable pass's.
		{
			// The traceback goes to stderr, which the CLI leg does not capture, so the pin is what the
			// program leaves on stdout: nothing, where the reference prints the handler's answer.
			name:  "a call in an `except` handler lets the frame's own trap escape",
			gap:   "Gap R.389",
			src:   "def answer():\n    return 7\n\ntry:\n    z = 1 / 0\nexcept ZeroDivisionError:\n    print(answer())\n",
			code:  3,
			today: "",
			want:  "7\n",
		},
	}
}

// TestTheLambdaCarrierFamilyAnswersLikeTheReference is the reference leg: every answered program prints,
// compiled, exactly what CPython prints for the same source.
func TestTheLambdaCarrierFamilyAnswersLikeTheReference(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range lambdaCarrierPrograms() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, dir, "lambda_carrier.gy", tc.src)
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
					t.Fatalf("%s exited %d on a program the reference prints %q — a carrier statement was refused:\n%s", engine, code, want, out)
				}
				if out != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", engine, out, want, tc.src)
				}
			}
		})
	}
}

// TestTheLambdaCarrierDeclinedCarriersRefuseRatherThanDie is the handler position at the CLI: the reference
// answers every one of these, and this backend REFUSES them at exit 1 rather than run a clone whose call no
// longer sits inside the frame whose trap catch the handler is registered on (roadmap Gap R.389). Exit 2 is
// forbidden, and so is exit 3: a program that catches its own error must not die unhandled.
func TestTheLambdaCarrierDeclinedCarriersRefuseRatherThanDie(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range lambdaCarrierDeclined() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, dir, "lambda_carrier_declined.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want == "" {
				t.Fatalf("this decline row is supposed to describe a program the reference answers; it prints nothing")
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Fatalf("%s ran a call cloned out of an `except` handler (Gap R.389) — the gate is gone and this case is\n"+
						"the one that has to be DELETED, with the program moved to the answered table\n%s", engine, tc.src)
				}
				if code == 2 {
					t.Fatalf("%s: exit 2 on a program whose only fault is where its call sits — ADR 0166 reserves it for a\n"+
						"compiler bug, and the row files the fault as Gap R.389 rather than shipping it:\n%s", engine, out)
				}
				if code == 3 {
					t.Fatalf("%s: exit 3 — the handler's own error escaped instead of being caught (Gap R.389):\n%s\n%s", engine, out, tc.src)
				}
				if code != 1 {
					t.Fatalf("%s: exit %d, expected the refusal at 1: %s", engine, code, out)
				}
			}
		})
	}
}

// TestTheLambdaCarrierFamilyIsOnTheRecord is ADR 0302's rule at this altitude: every program this row
// answers is on the record, and a record that disappeared fails here rather than passing by having nothing
// to compare against. The entries were written from the reference by tools/recmerge — the compiled leg is
// the thing under test, so it is never allowed to write its own answers.
func TestTheLambdaCarrierFamilyIsOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range lambdaCarrierPrograms() {
		src := tc.src
		name := tc.name
		t.Run(name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// TestWhatThisRowMeasuredAndDoesNotOwnIsPinnedWithTheReferencesAnswer holds the shapes the reader reaches
// and the doors still answer wrongly. Two assertions per row: the reference answers (so the row describes a
// program, not a broken one), and the linked program prints EXACTLY the bytes pinned here with the exit code
// pinned here.
func TestWhatTheCarrierRowMeasuredAndDoesNotOwnIsPinnedWithTheReferencesAnswer(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range lambdaCarrierPinnedRows() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, dir, "lambda_carrier_owed.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if want == tc.today {
				t.Fatalf("%s: the program now prints the reference's %q — the row closed and this pin has to be DELETED, "+
					"with the program moved into the answered table\n%s", tc.gap, want, tc.src)
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s: exit 2 on a program the reference answers — ADR 0166 reserves it for a compiler bug, and "+
						"this row files the fault as %s rather than shipping it:\n%s", engine, tc.gap, out)
				}
				if code != tc.code {
					t.Fatalf("%s: %s pinned exit %d, the program exited %d (the reference prints %q)\n%s", engine, tc.gap, tc.code, code, want, tc.src)
				}
				if out != tc.today {
					t.Fatalf("%s: %s pinned today's answer as %q, the program printed %q (the reference prints %q)\n%s",
						engine, tc.gap, tc.today, out, want, tc.src)
				}
			}
		})
	}
}

// TestTheLambdaCarrierProgramIsTheReferenceLineForLine is the conformance corpus's own claim, asked of the
// file rather than of a table: integration/programs/lambda_carrier.gy prints the reference's bytes for every
// line, with no line dropped between the two.
func TestTheLambdaCarrierProgramIsTheReferenceLineForLine(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("lambda_carrier")
	want, ok := cpythonPlainOut(t, t.TempDir(), src)
	if !ok {
		t.Skip("no live python3 to ask")
	}
	for _, engine := range cliEngines {
		path := writeSrc(t, t.TempDir(), "lambda_carrier.gy", src)
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
