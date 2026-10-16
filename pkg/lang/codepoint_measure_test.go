package lang

// The measurement a text answers with (roadmap L11.5, closing Gap N.2).
//
// `TestStringLengthIsBytesForNow` used to pin the wrong answer on purpose: it recorded that `len("café")`
// was 5 and `"héllo"[1]` was the byte 195, because every reader of a text walked bytes. The rows below are
// the oracle test that file was always meant to become — the same source asked of this compiler and of
// CPython, on every road the row names: `len`, `s[i]`, `s[i:j]`, `for c in s`, the interned table and the
// printers. Each is asked through a text the compiler can read AND through one it cannot, because the
// representation decision had to be taken once for both roads or the divergence would have just moved.

import (
	"os/exec"
	"strings"
	"testing"
)

// codePointMeasureCases is the row's own evidence, one line per claim it has to keep answering.
var codePointMeasureCases = []struct {
	name string
	src  string
}{
	// Gap N.2's two headline answers.
	{"len counts code points", "print(len(\"café\"))\n"},
	{"indexing gives a character", "print(\"héllo\"[1])\n"},
	// ...and the neighbours of those two claims.
	{"len of an astral text", "print(len(\"🎉é\"))\n"},
	{"index of an astral character", "print(\"🎉é\"[0])\n"},
	{"index from the end", "print(\"héllo\"[-1])\n"},
	{"a slice is a text of characters", "print(\"héllo\"[1:3])\n"},
	{"a slice subscripted again", "print(\"héllo\"[1:4][0])\n"},
	{"an empty slice", "print(\"héllo\"[2:2])\n"},
	{"iteration by literal", "for c in \"aé\":\n    print(c)\n"},
	{"iteration counts the astral pair", "for c in \"🎉é\":\n    print(c)\n"},
	{"the last character is one character", "s = \"héllo\"\nprint(s[-1])\n"},
	{"a character is a text", "s = \"héllo\"\nprint(s[1] == \"é\")\n"},
	{"a character measures one", "s = \"héllo\"\nprint(len(s[1]))\n"},
	{"ord asks the character", "s = \"héllo\"\nprint(ord(s[1]))\n"},
	// The interned table: two texts equal by content on both roads, and a character read out of a
	// container is a text the printers can print.
	{"comparison is by content", "print(\"café\" == \"café\")\n"},
	{"membership counts characters", "print(\"é\" in \"café\")\n"},
	{"a text in a list prints as text", "xs = [\"é\", \"ü\"]\nprint(xs)\n"},
	{"an element read out is a text", "xs = [\"é\", \"ü\"]\nprint(xs[1])\n"},
	{"a text as a dict key", "d = {\"café\": 1}\nprint(d[\"café\"])\n"},
	{"a character key", "d = {}\nfor c in \"aé\":\n    d[c] = 1\nprint(len(d))\n"},
	// The printers: a text through the one mixed-value printer, in every container shape.
	{"a tuple of texts", "t = (\"é\", \"ü\")\nprint(t)\n"},
	{"a dict of texts", "d = {\"é\": \"ü\"}\nprint(d)\n"},
	{"repr of a text", "print(repr(\"café\"))\n"},
	// The doors the row adds, on the two roads, measured in characters.
	{"case answers count characters", "print(len(\"straße\".upper()))\n"},
	{"trim counts characters", "print(len(\"  ßﬁ  \".strip()))\n"},
	{"trim keeps non-ascii", "print(\" ß \".strip())\n"},
}

// TestTheStringMeasuresCodePointsOnEveryRoad is the oracle test L11.5's evidence names. Each source is
// written once and asked of both sides in the same case, so an expectation cannot be quietly rewritten to
// match whatever the compiler happened to do.
func TestTheStringMeasuresCodePointsOnEveryRoad(t *testing.T) {
	t.Parallel()
	for _, tc := range codePointMeasureCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command("python3", "-c", tc.src)
			var errb strings.Builder
			cmd.Stderr = &errb
			want, err := cmd.Output()
			if err != nil {
				t.Fatalf("the reference refused a program the row claims to answer (%v): %s\n%s", err, tc.src, errb.String())
			}
			got := compiledOut(t, tc.src)
			if got != string(want) {
				t.Fatalf("the compiled leg printed %q, CPython prints %q\n%s", got, string(want), tc.src)
			}
		})
	}
}

// TestTheStringMeasuresCodePointsThroughAValue is the same claims over the road where the compiler cannot
// read the text — the half that used to be a compile-time refusal, and the half a representation decision
// can get wrong on its own.
func TestTheStringMeasuresCodePointsThroughAValue(t *testing.T) {
	t.Parallel()
	// Every claim above, re-asked with the text behind a parameter, a call, or a container slot.
	wrappers := []struct {
		name  string
		build func(src, body string) string
	}{
		{"through a parameter", func(src, body string) string {
			return "def f(s):\n    return " + body + "\nprint(f(" + src + "))\n"
		}},
		{"through a call", func(src, body string) string {
			return "def g():\n    return " + src + "\ndef f(s):\n    return " + body + "\nprint(f(g()))\n"
		}},
		{"through a container slot", func(src, body string) string {
			return "xs = [" + src + "]\ndef f(s):\n    return " + body + "\nprint(f(xs[0]))\n"
		}},
	}
	sources := []string{
		"\"café\"", "\"héllo\"", "\"🎉é\"", "\"straße\"", "\"  ßﬁ  \"", "\"aé\"", "\"ΣΣΣ\"",
	}
	// The questions every text answers on both roads. A name missing here is the shape that hid before:
	// print asked one predicate and the operation asked another.
	bodies := []string{
		"len(s)", "s[0]", "s[-1]", "s[1:3]", "s.upper()", "s.lower()", "s.strip()", "\"é\" in s", "s + \"!\"",
	}
	for _, w := range wrappers {
		for _, src := range sources {
			for _, body := range bodies {
				w, src, body := w, src, body
				t.Run(w.name+"/"+body, func(t *testing.T) {
					t.Parallel()
					program := w.build(src, body)
					cmd := exec.Command("python3", "-c", program)
					var errb strings.Builder
					cmd.Stderr = &errb
					want, err := cmd.Output()
					if err != nil {
						t.Fatalf("the reference refused the program (%v):\n%s\n%s", err, program, errb.String())
					}
					got := compiledOut(t, program)
					if got != string(want) {
						t.Fatalf("the compiled leg printed %q, CPython prints %q\n%s", got, string(want), program)
					}
				})
			}
		}
	}
}
