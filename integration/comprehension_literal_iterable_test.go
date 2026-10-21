package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// comprehension_literal_iterable_test.go is the CLI half of roadmap Gap R.358 / ADR 0386. A comprehension
// over `[1, 2]` used to ask its ITERABLE for constant integers, while a `for` over the same literal had long
// walked texts, floats, voids, verdicts and containers. At the terminal these programs either print CPython
// byte for byte, raise CPython's unhashable TypeError, or refuse honestly — never exit 2, and never print a
// container's handle or a void's zero at exit 0.

var literalIterableComprehensionPrograms = []struct {
	name, src, want string
}{
	{
		"texts answer the text list",
		"print([x for x in [\"a\", \"b\"]])\n",
		"['a', 'b']\n",
	},
	{
		"a filter over texts keeps the reference's survivors",
		"print([x for x in [\"a\", \"b\"] if x != \"b\"])\n",
		"['a']\n",
	},
	{
		"a dict comprehension keys itself by text",
		"print({k: 1 for k in [\"a\", \"b\"]})\n",
		"{'a': 1, 'b': 1}\n",
	},
	{
		"a mixed scalar list prints every kind",
		"print([x for x in [1, \"a\", 2.5, None, True]])\n",
		"[1, 'a', 2.5, None, True]\n",
	},
	{
		"void and verdicts are not zero and one",
		"print([x for x in [None, True, False]])\n",
		"[None, True, False]\n",
	},
	{
		"container items remain containers",
		"print([x for x in [[1], [2]]])\n",
		"[[1], [2]]\n",
	},
	{
		"dict literal items remain dicts",
		"print([x for x in [{}, {\"a\": 1}]])\n",
		"[{}, {'a': 1}]\n",
	},
	{
		"a list of the loop variable carries each item's tag",
		"print([[x] for x in [None, 1.5, True]])\n",
		"[[None], [1.5], [True]]\n",
	},
	{
		"a dict entry copies a mixed item",
		"print({x: x for x in [\"a\", 1, 2.5]})\n",
		"{'a': 'a', 1: 1, 2.5: 2.5}\n",
	},
	{
		"a call belongs in an iterable as much as in a for",
		"def f():\n    return 1\n\nprint([x for x in [1, f()]])\n",
		"[1, 1]\n",
	},
}

func TestALiteralIterableComprehensionAnswersTheReferenceAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range literalIterableComprehensionPrograms {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "comp_literal_iterable.gy", tc.src)
			got, code := cliRunMerged(t, "--file", gy)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, got)
			}
			if got != tc.want {
				t.Fatalf("compiled = %q, want %q", got, tc.want)
			}
			if want := pythonOutput(t, tc.src); want != tc.want {
				t.Fatalf("CPython = %q, want %q", want, tc.want)
			}
		})
	}
}

func TestALiteralIterableSetAtTheCLIAnswersTheReferenceMembers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, wantMembers string
	}{
		{
			"mixed scalars",
			"print({x for x in [1, \"a\", 2.5, None, True]})\n",
			"'a', 1, 2.5, None",
		},
		{
			"void and verdicts",
			"print({x for x in [None, True, False]})\n",
			"False, None, True",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "comp_literal_set.gy", tc.src)
			got, code := cliRunMerged(t, "--file", gy)
			if code != 0 {
				t.Fatalf("exit %d: %s", code, got)
			}
			want := pythonOutput(t, tc.src)
			if sortedSetMembers(got) != tc.wantMembers || sortedSetMembers(want) != sortedSetMembers(got) {
				t.Fatalf("compiled = %q (%q), reference = %q (%q), want %q",
					got, sortedSetMembers(got), want, sortedSetMembers(want), tc.wantMembers)
			}
		})
	}
}

func TestALiteralIterableComprehensionAtTheCLIRaisesTheReferenceUnhashableSentence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"list member of a set comprehension",
			"print({x for x in [[1], [2]]})\n",
			"TypeError: unhashable type: 'list'",
		},
		{
			"list key of a dict comprehension",
			"print({x: 1 for x in [[1], [2]]})\n",
			"TypeError: unhashable type: 'list'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "comp_literal_unhashable.gy", tc.src)
			out, code := cliRunMerged(t, "--file", gy)
			if code == 2 {
				t.Fatalf("exit 2 is a compiler bug, not the program's raise:\n%s", out)
			}
			if code == 0 {
				t.Fatalf("an unhashable member answered at exit 0: %q", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("compiled = %q, want it to contain %q", out, tc.want)
			}
		})
	}
}

func TestTheLiteralIterableComprehensionProgramsAreOnTheRecord(t *testing.T) {
	t.Parallel()
	onRecord := 0
	for _, tc := range literalIterableComprehensionPrograms {
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

func TestTheIntegerLiteralComprehensionStillUsesTheFoldAtTheCLI(t *testing.T) {
	t.Parallel()
	const src = "print([x for x in [1, 2]])\n"
	dir := t.TempDir()
	gy := writeSrc(t, dir, "comp_int_fold.gy", src)
	got, code := cliRunMerged(t, "--file", gy)
	if code != 0 || got != "[1, 2]\n" {
		t.Fatalf("compiled = %q exit %d, want [1, 2]", got, code)
	}
	ir := cliRun(t, "--emit-llvm", src)
	if !strings.Contains(ir, "@.lst") {
		t.Fatalf("an all-integer comprehension no longer folds to a static list:\n%s", ir)
	}
	if strings.Contains(ir, "call void @rt_append_tagged(") {
		t.Fatalf("the all-integer fold was pushed onto the tagged runtime builder:\n%s", ir)
	}
}
