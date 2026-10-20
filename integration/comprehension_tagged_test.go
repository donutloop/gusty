package integration

import (
	"sort"
	"strings"
	"testing"
)

// comprehension_tagged_test.go is the CLI half of roadmap Gap R.353 / ADR 0384. The list road has had a
// runtime loop since ADR 0192; the set and dict roads previously had only a compile-time fold, which made
// them answer calls, texts, floats, None, verdicts and containers badly or refuse them while the list twin
// worked. Through `gustyc` the same shapes must either print CPython's answer, raise CPython's unhashable
// TypeError, or refuse by naming a roadmap row — never exit 2, and never exit 0 with a slot index.

// TestTheSetAndDictComprehensionCLIAnswersTheShapesTheFoldCannotName is the headline: these all answer at
// exit 0, and every row is checked against CPython from the same source.
func TestTheSetAndDictComprehensionCLIAnswersTheShapesTheFoldCannotName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"set lambda member",
			"print({(lambda z: z + x)(1) for x in [10, 20]})\n",
			"{11, 21}\n",
		},
		{
			"dict lambda value",
			"print({x: (lambda z: z + x)(1) for x in [10, 20]})\n",
			"{10: 11, 20: 21}\n",
		},
		{
			"set def member",
			"def f(v):\n    return v * 2\nprint({f(x) for x in [1, 2]})\n",
			"{2, 4}\n",
		},
		{
			"dict def value",
			"def f(v):\n    return v * 2\nprint({x: f(x) for x in [1, 2]})\n",
			"{1: 2, 2: 4}\n",
		},
		{
			"runtime list source",
			"def f(v):\n    return v + 1\nxs = []\nxs.append(3)\nxs.append(4)\nprint({f(x) for x in xs})\n",
			"{4, 5}\n",
		},
		{
			"None and verdict members",
			"print({None for x in [1]})\nprint({True for x in [1]})\n",
			"{None}\n{True}\n",
		},
		{
			"None and verdict values",
			"print({1: None for x in [1]})\nprint({1: True for x in [1]})\n",
			"{1: None}\n{1: True}\n",
		},
		{
			"text key dict keeps text and count",
			"d = {\"k\": v for v in [1, 2, 3]}\nprint(d)\nprint(d[\"k\"], len(d))\n",
			"{'k': 3}\n3 1\n",
		},
		{
			"float dict value",
			"xs = []\nxs.append(6)\nprint({v: v / 2 for v in xs})\n",
			"{6: 3.0}\n",
		},
		{
			"text dict value from a method",
			"xs = [\"a\", \"b\"]\nprint({x: x.upper() for x in xs})\n",
			"{'a': 'A', 'b': 'B'}\n",
		},
		{
			"container dict value",
			"print({x: [x] for x in [1, 2]})\n",
			"{1: [1], 2: [2]}\n",
		},
		{
			"set dict value",
			"print({x: {x} for x in [1, 2]})\n",
			"{1: {1}, 2: {2}}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "comp_tagged.gy", tc.src)
			got, code := cliRunMerged(t, "--file", gy)
			if code != 0 {
				t.Fatalf("the CLI refused or trapped a shape the fold now builds (exit %d):\n%s", code, got)
			}
			if got != tc.want {
				t.Fatalf("compiled = %q, want %q", got, tc.want)
			}
			want := pythonOutput(t, tc.src)
			if want != tc.want {
				t.Fatalf("CPython = %q, want %q", want, tc.want)
			}
		})
	}
}

// TestTheSetAndDictComprehensionCLIRaisesUnhashableMembers makes the one hashable position answer with a
// trap, not a toolchain rejection. An invalid LLVM module would be exit 2; the program raises and exits 3.
func TestTheSetAndDictComprehensionCLIRaisesUnhashableMembers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"list member in a set comprehension",
			"print({[1] for x in [1]})\n",
			"TypeError: unhashable type: 'list'",
		},
		{
			"set member in a set comprehension",
			"print({{1} for x in [1]})\n",
			"TypeError: unhashable type: 'set'",
		},
		{
			"dict member in a set comprehension",
			"print({{1: 2} for x in [1]})\n",
			"TypeError: unhashable type: 'dict'",
		},
		{
			"list key in a dict comprehension",
			"print({[1]: 1 for x in [1]})\n",
			"TypeError: unhashable type: 'list'",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "comp_unhashable.gy", tc.src)
			out, code := cliRunMerged(t, "--file", gy)
			if code == 2 {
				t.Fatalf("exit 2 means the emitted module was rejected (ADR 0166), not that the program raised:\n%s", out)
			}
			if code == 0 {
				t.Fatalf("an unhashable member answered at exit 0 (the worst answer): %q", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Fatalf("compiled trap = %q, want it to contain %q", out, tc.want)
			}
		})
	}
}

// TestTheSetOfTextsAtTheCLIAnswersTheReferenceMembers is the CLI half of the hash-randomisation problem:
// CPython's text-set repr has arbitrary order, so the row compares member sets, not source spelling.
func TestTheSetOfTextsAtTheCLIAnswersTheReferenceMembers(t *testing.T) {
	t.Parallel()
	const src = "xs = [\"a\", \"b\"]\nprint({x.upper() for x in xs})\n"
	dir := t.TempDir()
	gy := writeSrc(t, dir, "comp_text_set.gy", src)
	got, code := cliRunMerged(t, "--file", gy)
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, got)
	}
	want := pythonOutput(t, src)
	if sortedSetMembers(got) != "'A', 'B'" || sortedSetMembers(want) != sortedSetMembers(got) {
		t.Fatalf("compiled = %q (%q), reference = %q (%q)", got, sortedSetMembers(got), want, sortedSetMembers(want))
	}
}

// TestTheFoldStillAnswersTheConstantRowsAtTheCLI guards the other road: what the folder can name still
// compiles to the static container and prints the same answer; the runtime builder must not swallow it.
func TestTheFoldStillAnswersTheConstantRowsAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"folded set",
			"print({x * 2 for x in [1, 2, 2]})\n",
			"{2, 4}\n",
		},
		{
			"folded dict",
			"print({x: x * 2 for x in [1, 2]})\n",
			"{1: 2, 2: 4}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "comp_fold.gy", tc.src)
			got, code := cliRunMerged(t, "--file", gy)
			if code != 0 || got != tc.want {
				t.Fatalf("compiled = %q exit %d, want %q", got, code, tc.want)
			}
			ir := cliRun(t, "--emit-llvm", tc.src)
			if !strings.Contains(ir, "@.set") && !strings.Contains(ir, "@.dict") {
				t.Fatalf("constant comprehension no longer emits its folded container:\n%s", ir)
			}
			if strings.Contains(ir, "call void @rt_set_add_tagged") || strings.Contains(ir, "call void @rt_dict_put_tagged") {
				t.Fatalf("a foldable comprehension routed itself through the tagged builder:\n%s", ir)
			}
		})
	}
}

func sortedSetMembers(s string) string {
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
