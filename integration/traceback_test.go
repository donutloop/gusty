package integration

// The reference leg of Gap K.8 (ADR 0354): the frames a compiled program prints when nothing
// handles its exception, checked against the frames CPython prints for the same source.
//
// The record is silent about frames — it stores a trap's class and message, never the stack the
// retired engine walked — so this is the only witness that says whether a traceback names the
// right calls. Everything here is therefore a comparison and nothing is a hardcoded expectation:
// the reference's answer is taken from the same run, one line of source at a time.
//
// Three things are compared, and each is a different bug a reader would see:
//
//   - the frames, in order, by function name and line — the traceback's content;
//   - the reference's collapse of a recursion (`[Previous line repeated N more times]`), which is
//     the difference between a report a reader can parse and 200 copies of one line;
//   - the exception class on the last line, which is what a tool branches on.
//
// The message beside the class is not compared: the wording of `KeyError: 'b'` against
// `KeyError: key not found` is a separate ledger row (the record's wording, ADR 0211), and a
// traceback test that failed on prose would hide the frames it is here to check.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/donutloop/gusty/pkg/lang"
)

// frameRepeatMarkerRe is the reference's own sentence for a run of identical frames; the whole
// line, count included, is what two reports have to agree about.
var frameRepeatMarkerRe = regexp.MustCompile(`\[Previous line repeated \d+ more times?\]`)

func frameRepeatMarker(report string) string {
	return frameRepeatMarkerRe.FindString(report)
}

// tracebackPrograms are programs whose failure a reader has to diagnose from the report alone.
var tracebackPrograms = []struct {
	name string
	src  string
}{
	{
		"two calls deep",
		"def bar(x):\n  return x // 0\ndef foo(x):\n  return bar(x)\nfoo(10)\n",
	},
	{
		"a method",
		"class C:\n  def m(self):\n    return 1 // 0\nc = C()\nc.m()\n",
	},
	{
		"an index past the end, reached through a helper",
		"def pick(xs, i):\n    return xs[i]\n\nxs = [1, 2, 3]\nprint(pick(xs, 5))\n",
	},
	{
		"a recursion shallow enough to print in full",
		"def down(n):\n    if n == 0:\n        return 1 // 0\n    return down(n - 1)\n\nprint(down(2))\n",
	},
	{
		"a recursion deep enough that the reference collapses it",
		"def down(n):\n    if n == 0:\n        return 1 // 0\n    return down(n - 1)\n\nprint(down(6))\n",
	},
	{
		"the exception that escapes while an earlier one was handled",
		"def safe():\n    try:\n        return 1 // 0\n    except ZeroDivisionError:\n        pass\n    return 1\n\ndef boom():\n    return 7 // 0\n\na = safe()\nb = boom()\nprint(a, b)\n",
	},
	{
		"a failure inside a comprehension's helper",
		"def scale(v):\n    return v[0] // v[1]\n\nprint(scale([10, 0]))\n",
	},
	{
		"the module's own statement",
		"x = 3\ny = 0\nprint(x // y)\n",
	},
}

// TestTheCompiledTracebackNamesTheFramesTheReferenceNames is the reference leg: same source, same
// failure, and the two reports must name the same calls in the same order.
func TestTheCompiledTracebackNamesTheFramesTheReferenceNames(t *testing.T) {
	t.Parallel()
	for _, tc := range tracebackPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pyOut, pyErr, perr := lang.PythonRun(tc.src)
			if perr == nil {
				t.Fatalf("CPython accepted a program meant to trap; the case tests nothing (it printed %q)", pyOut)
			}
			wantFrames := lang.ParseTrapFrames(pyErr)
			if len(wantFrames) == 0 {
				t.Fatalf("no frames in CPython's report:\n%s", pyErr)
			}
			wantClass, _, _ := lang.ParseTrapReport(pyErr)
			if wantClass == "" {
				t.Fatalf("CPython's report names no exception class:\n%s", pyErr)
			}

			// The link-and-run path, not the in-process JIT: this is the report a user of the
			// shipped compiler sees, and it is produced by a module `llc` accepted.
			out, err := runAOTWithTimeout(t, tc.src, 60*time.Second)
			if err == nil {
				t.Fatalf("the compiled program exited 0 on an unhandled exception: %q", out)
			}
			gotFrames := lang.ParseTrapFrames(out)
			gotClass, _, _ := lang.ParseTrapReport(out)

			if len(gotFrames) != len(wantFrames) {
				t.Fatalf("the compiled report named %d frames %v, the reference named %d %v\n--- compiled ---\n%s\n--- reference ---\n%s",
					len(gotFrames), gotFrames, len(wantFrames), wantFrames, out, pyErr)
			}
			for i, want := range wantFrames {
				got := gotFrames[i]
				if got.Name != want.Name || got.Line != want.Line {
					t.Errorf("frame %d is %s at line %d, the reference says %s at line %d\n--- compiled ---\n%s",
						i, got.Name, got.Line, want.Name, want.Line, out)
				}
			}
			if gotClass != wantClass {
				t.Errorf("the report ends in %q, the reference ends in %q", gotClass, wantClass)
			}
			// The collapse sentence is part of the shape: a recursion the reference shortens must
			// be shortened here too, with the same count, or the two reports disagree about how
			// deep the program was.
			wantMarker := frameRepeatMarker(pyErr)
			gotMarker := frameRepeatMarker(out)
			if gotMarker != wantMarker {
				t.Errorf("the compiled report collapses the recursion as %q, the reference as %q\n--- compiled ---\n%s",
					gotMarker, wantMarker, out)
			}
			// The header and the caption are read by everyone who has ever read a Python failure.
			for _, want := range []string{"Traceback (most recent call last):", `File "prog", line `} {
				if !strings.Contains(out, want) {
					t.Errorf("the compiled report is missing %q:\n%s", want, out)
				}
			}
		})
	}
}

// TestTheJSONFramesMemberAgreesWithTheTerminalReport is the machine path (AGENTS' rule that an
// agent must not scrape prose): the same frames, as data, beside the text the terminal saw.
func TestTheJSONFramesMemberAgreesWithTheTerminalReport(t *testing.T) {
	t.Parallel()
	src := "def bar(x):\n  return x // 0\ndef foo(x):\n  return bar(x)\nfoo(10)\n"
	path := filepath.Join(t.TempDir(), "frames.gy")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	out, code := cliRunCode(t, "--json", "--file", path)
	if code == 0 {
		t.Fatalf("the CLI exited 0 on an unhandled exception: %s", out)
	}
	var rep struct {
		Stderr    string `json:"stderr"`
		Frames    []lang.Frame
		Exit      int    `json:"exit"`
		Exception string `json:"exception"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("the JSON result does not parse: %v\n%s", err, out)
	}
	if rep.Exit != code {
		t.Errorf("the JSON reports exit %d, the process exited %d", rep.Exit, code)
	}
	if len(rep.Frames) == 0 {
		t.Fatalf("no frames member: %s", out)
	}
	// The two members are the same frames read two ways; if they ever drift, an agent branching
	// on `frames` and a human reading `stderr` are looking at different runs.
	terminal := lang.ParseTrapFrames(rep.Stderr)
	if len(terminal) != len(rep.Frames) {
		t.Fatalf("frames member has %d entries, the report printed %d: %v vs %v", len(rep.Frames), len(terminal), rep.Frames, terminal)
	}
	for i := range rep.Frames {
		if rep.Frames[i] != terminal[i] {
			t.Errorf("frame %d is %v as data and %v on the terminal", i, rep.Frames[i], terminal[i])
		}
	}
	want := []lang.Frame{{Name: "<module>", Line: 5}, {Name: "foo", Line: 4}, {Name: "bar", Line: 2}}
	if len(rep.Frames) != len(want) {
		t.Fatalf("frames = %v, want %v", rep.Frames, want)
	}
	for i := range want {
		if rep.Frames[i].Name != want[i].Name || rep.Frames[i].Line != want[i].Line {
			t.Errorf("frame %d = %v, want %v", i, rep.Frames[i], want[i])
		}
	}
	if rep.Exception != "ZeroDivisionError" {
		t.Errorf("exception = %q, want ZeroDivisionError", rep.Exception)
	}
}

// TestAFramesReportSurvivesTheVerifierAndTheLink is the ordinary-module half: the frame
// instrumentation is real IR in the shipped module, so `llc` and `cc` have to accept it for every
// program in the table above, including the ones that never trap their way out of a call.
func TestAFramesReportSurvivesTheVerifierAndTheLink(t *testing.T) {
	t.Parallel()
	for _, tc := range tracebackPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := runAOTWithTimeout(t, tc.src, 60*time.Second); err == nil {
				t.Fatalf("%q was meant to trap and did not", tc.name)
			}
		})
	}
}
