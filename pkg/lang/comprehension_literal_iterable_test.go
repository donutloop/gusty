package lang

import (
	"sort"
	"strings"
	"testing"
)

// comprehension_literal_iterable_test.go — a comprehension's ITERABLE is a container the program wrote,
// and it does not have to be a list of integers. The `for` road has long walked a literal of texts, floats,
// voids, verdicts and containers by binding the loop variable once per item and recording what that item is.
// The comprehension road used to hand the same literal to the integer folder (roadmap Gap R.358, ADR 0386).
//
// The fold still owns the all-integer literal: `min`/`max`/`sum`, folded subscripting, and the rule that a
// comprehension which folds IS the literal it folds to all depend on that. What moves to the builder is
// every iterable whose items have a kind the compile-time integer array cannot name.

func TestALiteralIterableComprehensionAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"list of texts",
			"print([x for x in [\"a\", \"b\"]])\n",
			"['a', 'b']\n",
		},
		{
			"filtered list of texts",
			"print([x for x in [\"a\", \"b\"] if x != \"b\"])\n",
			"['a']\n",
		},
		{
			"dict keyed by computed texts",
			"print({k: 1 for k in [\"a\", \"b\"]})\n",
			"{'a': 1, 'b': 1}\n",
		},
		{
			"list of mixed scalars",
			"print([x for x in [1, \"a\", 2.5, None, True]])\n",
			"[1, 'a', 2.5, None, True]\n",
		},
		{
			"void and verdicts in their own order",
			"print([x for x in [None, True, False]])\n",
			"[None, True, False]\n",
		},
		{
			"negative integer, float and void",
			"print([x for x in [-1, 2.5, None]])\n",
			"[-1, 2.5, None]\n",
		},
		{
			"text method over a text iterable",
			"print([x.upper() for x in [\"a\", \"b\"]])\n",
			"['A', 'B']\n",
		},
		{
			"list literal items",
			"print([x for x in [[1], [2]]])\n",
			"[[1], [2]]\n",
		},
		{
			"dict literal items",
			"print([x for x in [{}, {\"a\": 1}]])\n",
			"[{}, {'a': 1}]\n",
		},
		{
			"tuple literal items",
			"print([x for x in [(), (1,)]])\n",
			"[(), (1,)]\n",
		},
		{
			"list of the loop variable carries each item's kind",
			"print([[x] for x in [None, 1.5, True]])\n",
			"[[None], [1.5], [True]]\n",
		},
		{
			"dict entry copies a mixed item",
			"print({x: x for x in [\"a\", 1, 2.5]})\n",
			"{'a': 'a', 1: 1, 2.5: 2.5}\n",
		},
		{
			"dict value is a list of the mixed loop variable",
			"print({x: [x] for x in [\"a\", 1, None]})\n",
			"{'a': ['a'], 1: [1], None: [None]}\n",
		},
		{
			"call in the literal iterable",
			"def f():\n    return 1\n\nprint([x for x in [1, f()]])\n",
			"[1, 1]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Compile(tc.src); err != nil {
				t.Fatalf("refused: %v", err)
			}
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Fatalf("compiled = %q, want %q", got, tc.want)
			}
			if got, _, err := PythonRun(tc.src); err != nil || got != tc.want {
				t.Fatalf("reference = %q (%v), want %q", got, err, tc.want)
			}
		})
	}
}

func TestALiteralIterableSetAnswersTheSameMembersAsTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, wantMembers string
	}{
		{
			"mixed scalar members",
			"print({x for x in [1, \"a\", 2.5, None, True]})\n",
			"'a', 1, 2.5, None",
		},
		{
			"void and verdicts",
			"print({x for x in [None, True, False]})\n",
			"False, None, True",
		},
		{
			"one survivor of a text filter",
			"print({x for x in [\"a\", \"b\"] if x != \"b\"})\n",
			"'a'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := compiledOut(t, tc.src)
			want, _, err := PythonRun(tc.src)
			if err != nil {
				t.Fatalf("reference: %v", err)
			}
			gotSet := sortedComprehensionSetMembers(got)
			if gotSet != tc.wantMembers {
				t.Fatalf("compiled = %q (%q), want members %q", got, gotSet, tc.wantMembers)
			}
			wantSet := sortedComprehensionSetMembers(want)
			if wantSet != gotSet {
				t.Fatalf("compiled members %q, reference members %q", gotSet, wantSet)
			}
		})
	}
}

func TestTheIntegerFoldStillOwnsTheAllIntegerLiteralIterable(t *testing.T) {
	t.Parallel()
	ir := compileSrcIR(t, "print([x for x in [1, 2]])\n")
	if !strings.Contains(ir, "@.lst") {
		t.Fatalf("an all-integer literal comprehension stopped folding to its static list:\n%s",
			strings.Join(irLinesContaining(ir, "@.lst"), "\n"))
	}
	if strings.Contains(ir, "call void @rt_append_tagged(") {
		t.Fatalf("the all-integer fold was pushed onto the runtime tagged builder:\n%s",
			strings.Join(irLinesContaining(ir, "rt_append_tagged"), "\n"))
	}

	ir = compileSrcIR(t, "print([x for x in [None, 2.5]])\n")
	for _, needle := range []string{"call void @rt_append_tagged(", "@rt_print_list_mixed"} {
		if !strings.Contains(ir, needle) {
			t.Fatalf("a non-integer literal comprehension did not build or print through %s:\n%s",
				needle, strings.Join(irLinesContaining(ir, "rt_"), "\n"))
		}
	}
}

func TestWhatALiteralIterableComprehensionStillCannotAnswerRefusesHonestly(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"a text loop variable used as a number",
			"print([x + 1 for x in [\"a\", \"b\"]])\n",
			"not supported",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			msg := ""
			if err != nil {
				msg = err.Error()
			} else if res != nil {
				for _, d := range res.Diagnostics {
					msg += d.Msg
				}
			}
			if msg == "" {
				t.Fatalf("compiled clean; want a refusal mentioning %q", tc.want)
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("refusal = %q, want %q", msg, tc.want)
			}
		})
	}
}

func TestEveryLiteralIterableComprehensionAnswerIsOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"texts", "print([x for x in [\"a\", \"b\"]])\n", "['a', 'b']\n"},
		{"filtered texts", "print([x for x in [\"a\", \"b\"] if x != \"b\"])\n", "['a']\n"},
		{"text keys", "print({k: 1 for k in [\"a\", \"b\"]})\n", "{'a': 1, 'b': 1}\n"},
		{"mixed scalars", "print([x for x in [1, \"a\", 2.5, None, True]])\n", "[1, 'a', 2.5, None, True]\n"},
		{"void and verdicts", "print([x for x in [None, True, False]])\n", "[None, True, False]\n"},
		{"negative float void", "print([x for x in [-1, 2.5, None]])\n", "[-1, 2.5, None]\n"},
		{"text methods", "print([x.upper() for x in [\"a\", \"b\"]])\n", "['A', 'B']\n"},
		{"list items", "print([x for x in [[1], [2]]])\n", "[[1], [2]]\n"},
		{"dict entry copies mixed item", "print({x: x for x in [\"a\", 1, 2.5]})\n", "{'a': 'a', 1: 1, 2.5: 2.5}\n"},
		{"dict list value", "print({x: [x] for x in [\"a\", 1, None]})\n", "{'a': ['a'], 1: [1], None: [None]}\n"},
		{"one text set survivor", "print({x for x in [\"a\", \"b\"] if x != \"b\"})\n", "{'a'}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := runGoldenStdout(t, tc.src)
			if err != nil {
				if err == errGoldenSkipped {
					t.Fatalf("this program is not on the record leg — record it with tools/recmerge\nsrc: %s", tc.src)
				}
				t.Fatalf("the record leg trapped a program this row answers: %v\nsrc: %s", err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("record = %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

func TestEveryLiteralIterableComprehensionRaiseIsOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"unhashable list member from a literal iterable", "print({x for x in [[1], [2]]})\n", "TypeError: unhashable type: 'list'"},
		{"unhashable dict key from a literal iterable", "print({x: 1 for x in [[1], [2]]})\n", "TypeError: unhashable type: 'list'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := RecordedRunError(t, tc.src)
			if err == nil {
				t.Fatalf("the record answered an unhashable program the reference raises\nsrc: %s", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("record trap = %q, want %q\nsrc: %s", err, tc.want, tc.src)
			}
			if _, err := Compile(tc.src); err != nil {
				t.Fatalf("the compiled leg refused a program the reference raises instead of raising it: %v\nsrc: %s", err, tc.src)
			}
			class, message := jitTrap(t, tc.src)
			if class != "TypeError" || message != strings.TrimPrefix(tc.want, "TypeError: ") {
				t.Fatalf("compiled trap = %s: %s, want %s\nsrc: %s", class, message, tc.want, tc.src)
			}
		})
	}
}

func sortedComprehensionSetMembers(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return ""
	}
	parts := strings.Split(s, ", ")
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}
