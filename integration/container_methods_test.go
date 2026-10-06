package integration

// Whole-program coverage for the container methods that did not exist on either engine (roadmap
// Gap R.188 / Gap R.63, ADR 0301). `xs.extend([2,3])`, `xs.insert(0,9)`, `xs.index(2)`, `xs.clear()`,
// `d.update(...)`, `d.pop(k)`, `d.setdefault(k,v)` and `d.clear()` answered `no such list method` /
// `no such dict method` everywhere — a MISSING answer rather than a wrong one, which is why parity
// could never have caught it: there was no output to compare against.
//
// The interpreted leg is pinned to CPython line for line. The compiled leg is checked for the two
// things that are legal there — the reference's answer, or a refusal that names what it cannot lower
// — and never for exit 2.

import (
	"strings"
	"testing"
)

var containerMethodFamily = []struct {
	src  string
	want string
}{
	{`xs = [1]
xs.extend([2, 3])
print(xs)`, "[1, 2, 3]"},
	{`xs = [1]
xs.insert(0, 9)
print(xs)`, "[9, 1]"},
	{`xs = [1]
xs.insert(9, 9)
print(xs)`, "[1, 9]"}, // an out-of-range index clamps, it does not fail
	{`xs = [1, 2]
xs.remove(1)
print(xs)`, "[2]"},
	{`xs = [1, 2]
xs.clear()
print(xs)`, "[]"},
	{`print([1, 2].index(2))`, "1"},
	{`print([1, 2, 1].count(1))`, "2"},
	{`d = {"a": 1}
d.update({"b": 2})
print(d)`, "{'a': 1, 'b': 2}"},
	{`d = {"a": 1}
print(d.pop("a"))
print(d)`, "1\n{}"},
	{`print({"a": 1}.pop("z", 9))`, "9"},
	{`d = {}
d.setdefault("a", [])
print(d)`, "{'a': []}"},
	{`d = {"a": 1}
d.clear()
print(d)`, "{}"},
}

func TestCLIInterpreterRunsTheMethodsTheReferenceRuns(t *testing.T) {
	dir := t.TempDir()
	for _, c := range containerMethodFamily {
		src := writeSrc(t, dir, "cm.gy", c.src)
		ref, ok := cpythonPlainOut(t, dir, c.src)
		if !ok {
			t.Fatalf("the reference itself failed on %s: %s", c.src, ref)
		}
		got, code := cliRunMerged(t, "--interp", "--file", src)
		if code != 0 {
			t.Errorf("%s: interpreted leg exited %d: %s", c.src, code, got)
			continue
		}
		if strings.TrimSuffix(got, "\n") != strings.TrimSuffix(ref, "\n") {
			t.Errorf("%s: interpreted leg printed %q, reference answers %q", c.src, got, ref)
		}
		if strings.TrimSuffix(ref, "\n") != c.want {
			t.Errorf("%s: this table drifted from the reference: %q vs %q", c.src, ref, c.want)
		}
	}
}

// None of the family may reach exit 2 on either engine, and the compiled leg may only answer or
// refuse-with-a-reason. Before this row the interpreted leg's "no such list method" was exit 1 on a
// program the reference runs to completion, which is the class this row clears.
func TestCLIContainerMethodsNeverExitTwo(t *testing.T) {
	dir := t.TempDir()
	for _, c := range containerMethodFamily {
		src := writeSrc(t, dir, "cm.gy", c.src)
		for _, backend := range []string{"--interp", "--aot"} {
			got, code := cliRunMerged(t, backend, "--file", src)
			if code == 2 {
				t.Errorf("%s %s: exit 2 — a compiler bug — on %s: %s", backend, c.src, c.src, got)
			}
			if backend == "--interp" && code != 0 {
				t.Errorf("%s: interpreted leg exited %d on a program the reference runs: %s", c.src, code, got)
			}
		}
	}
}

func TestCLICompiledContainerMethodAnswersOrNamesItsRefusal(t *testing.T) {
	dir := t.TempDir()
	for _, c := range containerMethodFamily {
		src := writeSrc(t, dir, "cm.gy", c.src)
		got, code := cliRunMerged(t, "--aot", "--file", src)
		switch code {
		case 0:
			if strings.TrimSuffix(got, "\n") != c.want {
				t.Errorf("%s: compiled leg printed %q, reference answers %q", c.src, got, c.want)
			}
		case 1:
			if !strings.Contains(got, "method") && !strings.Contains(got, "container") {
				t.Errorf("%s: compiled leg refused without naming the method or the container: %s", c.src, got)
			}
		default:
			t.Errorf("%s: compiled leg exited %d (%s); only 0 and 1 are legal here", c.src, code, got)
		}
	}
}

// The raises are the reference's sentences, at the reference's exit class (3 = a trap the reference
// traps on, ADR 0166), on the leg that runs the program at all.
func TestCLIContainerMethodRaisesAtTheReferenceExitClass(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		src  string
		want string
	}{
		{`print([1, 2].index(5))`, "ValueError: 5 is not in list"},
		{`xs = [1, 2]
xs.remove(9)`, "ValueError: list.remove(x): x not in list"},
		{`print({"a": 1}.pop("z"))`, "KeyError: 'z'"},
		{`print({1: 2}.pop(5))`, "KeyError: 5"},
	}
	for _, c := range cases {
		f := writeSrc(t, dir, "raise.gy", c.src)
		got, code := cliRunMerged(t, "--interp", "--file", f)
		if code != 3 {
			t.Errorf("%s: interpreted leg exited %d, the reference traps here; output %q", c.src, code, got)
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: said %q, reference raises %q", c.src, got, c.want)
		}
	}
}

// Gap R.189's asymmetry, pinned rather than hidden: the interpreted leg names the key in a KeyError
// because a KeyError carries the key's repr; the compiled leg still prints the module's constant,
// because its raise is a compile-time string and naming the key needs the key rendered at run time.
// When the tagged word lifts, this test is the one that says so.
func TestCLIKeyErrorNamesTheKeyOnTheLegThatCan(t *testing.T) {
	dir := t.TempDir()
	f := writeSrc(t, dir, "ke.gy", "d = {}\nprint(d[\"a\"])")
	got, code := cliRunMerged(t, "--interp", "--file", f)
	if code != 3 || !strings.Contains(got, "KeyError: 'a'") {
		t.Errorf("interpreted KeyError = %q exit %d; the reference answers KeyError: 'a'", got, code)
	}
}
