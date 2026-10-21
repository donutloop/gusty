package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// call_key_param_test.go — the reference leg for the hook lambda whose body asks a door the call site
// never wrote (roadmap L11.7 · Gap R.264; ADR 0363).
//
// `pkg/lang/call_key_param_test.go` decides the SHAPE — which parameter the `key=` door claims, what the
// hoisted hook's body lowers to, which refusals stay refusals. This file asks the question no table can
// answer by itself: is the answer the SAME TEXT the reference prints? Every expectation below is read off
// CPython by `cpythonOut` at run time, and a compiled answer that differs is a failure — a stale literal in
// a test file is not evidence about a language.
//
// The programs are the same sources as the compiled-leg table, kept here in the same order, so the two
// files read as one experiment: same programs, two witnesses (AGENTS.md's two legs).

var keyedParamPrograms = []struct{ name, src string }{
	{"the row's own program", "print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)))\n"},
	{"the same key with a name", "def k(s):\n    return len(s)\nprint(sorted([\"bbb\", \"a\", \"cc\"], key=k))\n"},
	{"a method door in the key", "print(sorted([\"Z\", \"a\", \"Mb\"], key=lambda s: s.lower()))\n"},
	{"a subscript door in the key", "print(sorted([\"ab\", \"ba\"], key=lambda s: s[0]))\n"},
	{"an upper key", "print(sorted([\"b\", \"a\"], key=lambda s: s.upper()))\n"},
	{"a concatenation door in the key", "print(sorted([\"b\", \"a\"], key=lambda s: s + \"!\"))\n"},
	{"a named key that trims", "def k(s):\n    return s.strip()\nprint(sorted([\" b\", \"a\"], key=k))\n"},
	{"a named key that subscripts", "def k(s):\n    return s[0]\nprint(sorted([\"ba\", \"ab\"], key=k))\n"},
	{"a descending key", "print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: 0-len(s)))\n"},
	{"a key with reverse", "print(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s), reverse=True))\n"},
	{"ties keep the written order", "print(sorted([\"aa\", \"bb\", \"c\", \"d\"], key=lambda s: len(s)))\n"},
	{"the text decides, not the intern table", "print(sorted([\"z\", \"a\", \"Mb\"], key=lambda s: s.lower()))\n"},
	{"a code point, not a byte", "print(sorted([\"café\", \"a\", \"ab\"], key=lambda s: len(s)))\n"},
	{"min takes the key's parameter", "print(min([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)))\n"},
	{"max takes the key's parameter", "print(max([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)))\n"},
	{"min over written-out candidates", "print(min(\"bbb\", \"a\", key=lambda s: len(s)))\n"},
	{"max over written-out candidates", "print(max(\"bbb\", \"a\", \"cc\", key=lambda s: len(s)))\n"},
	{"a named descending key picks the shortest", "def k(s):\n    return 0-len(s)\nprint(max([\"bbb\", \"a\", \"cc\"], key=k))\n"},
	{"a named descending key on min picks the longest", "def k(s):\n    return 0-len(s)\nprint(min([\"bbb\", \"a\", \"cc\"], key=k))\n"},
	{"a bound choice is a text", "best = max([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s))\nprint(best, best.upper(), len(best))\n"},
	{"the answer is a list like any other", "xs = sorted([\"bbb\", \"a\"], key=lambda s: len(s))\nprint(xs, len(xs), xs[0])\n"},
	{"the answer is walkable", "for v in sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s)):\n    print(v)\n"},
	{"the answer's length", "print(len(sorted([\"bbb\", \"a\", \"cc\"], key=lambda s: len(s))))\n"},
	{"a helper answers a number", "def L(s):\n    return len(s)\nprint(sorted([\"bbbb\", \"a\", \"cc\"], key=L))\n"},
	{"a helper answers a text", "def first(s):\n    return s[0]\nprint(sorted([\"zz\", \"am\"], key=first))\n"},
	// the negative controls: what answered before this row answers still
	{"a numeric key still answers", "print(sorted([3, 1, 2], key=lambda x: 0-x))\n"},
	{"the builtin key still answers", "print(sorted([\"bbb\", \"a\", \"cc\"], key=len))\n"},
	{"no key still answers", "print(sorted([\"b\", \"a\"]))\n"},
	{"a plain call of the same body still answers", "def k(s):\n    return len(s)\nprint(k(\"abc\"), k(\"a\"))\n"},
	{"the program's own name is not the builtin", "def sorted(xs):\n    return [1]\nprint(sorted([3, 2]))\n"},
}

// TestAKeyThatAsksItsElementAnswersAtTheCLIParallelWithCPython is the reference leg: the compiled answer
// is asked beside `python3`'s, and the compiled one has to match the reference's rather than its own
// last build.
func TestAKeyThatAsksItsElementAnswersAtTheCLIParallelWithCPython(t *testing.T) {
	t.Parallel()
	for _, tc := range keyedParamPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "keyed_param.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 2 {
					t.Fatalf("%s rejected the compiler's own module (ADR 0166 / exit-code contract):\n%s", engine, cliRun(t, engine, path))
				}
				if code != 0 {
					t.Fatalf("%s exited %d: %s", engine, code, cliRun(t, engine, path))
				}
				if out != want {
					t.Fatalf("%s printed %q, CPython printed %q — for %s", engine, out, want, tc.src)
				}
			}
		})
	}
}

// TestTheKeyedParamProgramsAreOnTheRecord is ADR 0302's rule at this altitude: every program this row
// answers has an answer on record, and a record that disappeared fails here rather than passing by having
// nothing to compare against. The record was written from the reference by tools/recmerge, never from this
// backend.
func TestTheKeyedParamProgramsAreOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range keyedParamPrograms {
		// No t.Parallel: the parent's "did any row hit the record" check runs when this function
		// returns, and a parallel subtest has not run by then.
		t.Run(tc.name, func(t *testing.T) {
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend", tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one of this row's programs is on the record: the record leg is not being walked")
	}
}

// TestTheKeyedParamRefusalsStayRefusalsAtTheCLI holds a refusal to ADR 0166 and ADR 0249: exit 1, never
// exit 2, and the sentence names the shape rather than leaving the answer to a run-time question.
func TestTheKeyedParamRefusalsStayRefusalsAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		src    string
		phrase string
	}{
		{
			"elements that exist only at run time",
			"ys = [\"bbb\", \"a\"]\nprint(sorted(ys, key=lambda s: len(s)))\n",
			"exist only at run time",
		},
		{
			"a key the ordinary call road has no answer for",
			"print(sorted([\"bbb\", \"a\"], key=round))\n",
			// `round` is a key the door can now ask (roadmap Gap R.255): the one-argument call answers an
			// integer, so the verdict is the ordinary call road's — and the ordinary road's answer for a
			// TEXT operand is its own sentence, not one written here. The reference RAISES
			// `TypeError: type str doesn't define __round__ method` at that call, which is the raise the
			// plain `round("a")` already owes (Gap R.363).
			"cannot round a string",
		},
		{
			"one parameter, two representations",
			"print(max(\"bbb\", 1, key=lambda s: 1))\n",
			"strings are not supported as function arguments",
		},
		{
			"the same parameter, two contradictory claims",
			"def k(s):\n    return len(s)\nprint(k(3))\nprint(sorted([\"bbb\", \"a\"], key=k))\n",
			"len of a non-string variable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "keyed_param_refusal.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, path)
				if code == 0 {
					t.Fatalf("%s answered %q at exit 0; this shape is still owed by roadmap L11.1/L11.7", engine, out)
				}
				if code == 2 {
					t.Fatalf("%s exited 2 on a refusal (ADR 0166): %s", engine, out)
				}
				combined := cliRun(t, engine, path)
				if !strings.Contains(combined, tc.phrase) {
					t.Fatalf("%s refused without naming %q:\n%s", engine, tc.phrase, combined)
				}
			}
		})
	}
}
