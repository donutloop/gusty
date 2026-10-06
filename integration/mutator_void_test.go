package integration

// CLI coverage for the rule that a container method which mutates in place answers the void
// (roadmap Gap R.187, ADR 0300). The unit tables in pkg/lang/mutator_void_test.go pin the engines;
// these run real programs through the binary and check the two things only a whole run can show:
// the interpreted leg answers what CPython answers, and no member of the family reaches exit 2.
//
// Before this row `print(xs.append(2))` printed `[1, 2]` on the interpreter at exit 0 and made the
// compiled leg emit `printf(i8* @.fmt1, i32 )` — a call with a missing operand — which llc rejects,
// so the same one-line program spent exit 2, ADR 0166's forbidden class.

import (
	"strings"
	"testing"
)

var mutationFamily = []struct {
	src  string
	want string
}{
	{`xs = [1]
print(xs.append(2))`, "None"},
	{`xs = [1]
print(xs.append(2))
print(xs)`, "None\n[1, 2]"},
	{`xs = [3, 1]
print(xs.sort())`, "None"},
	{`xs = [1, 2]
print(xs.reverse())`, "None"},
	{`s = {1}
print(s.add(2))`, "None"},
	{`s = {1}
print(s.discard(2))`, "None"},
	{`d = {"a": 1}
print(d.get("zzz"))`, "None"},
	// `pop` is the mutator that IS an expression: it answers with what it removed.
	{`xs = [1, 2]
print(xs.pop())`, "2"},
	{`xs = [1, 2]
print(xs.pop(0))`, "1"},
}

// The interpreted leg is pinned to CPython line for line, not "starts with".
func TestCLIInterpreterAnswersTheVoidLikeTheReference(t *testing.T) {
	dir := t.TempDir()
	for _, c := range mutationFamily {
		src := writeSrc(t, dir, "mut.gy", c.src)
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
			t.Errorf("%s: this table's expectation drifted from the reference: %q vs %q", c.src, ref, c.want)
		}
	}
}

// The compiled leg may answer or refuse, and may not do anything else. Exit 2 is called out
// separately because that is the class this row actually eliminated.
func TestCLICompiledMutationNeverExitsTwo(t *testing.T) {
	dir := t.TempDir()
	for _, c := range mutationFamily {
		src := writeSrc(t, dir, "mut.gy", c.src)
		for _, backend := range []string{"--interp", "--aot"} {
			got, code := cliRunMerged(t, backend, "--file", src)
			if code == 2 {
				t.Errorf("%s %s: exit 2 — a compiler bug — on %s: %s", backend, c.src, c.src, got)
			}
		}
	}
}

// Where the compiled leg answers, it answers the reference. Where it refuses, the sentence must say
// which method and which receiver, so a reader can act on it (Gap R.38's rule for refusals).
func TestCLICompiledMutationAgreesOrNamesItsRefusal(t *testing.T) {
	dir := t.TempDir()
	for _, c := range mutationFamily {
		src := writeSrc(t, dir, "mut.gy", c.src)
		a, ac := cliRunMerged(t, "--aot", "--file", src)
		switch ac {
		case 0:
			if strings.TrimSuffix(a, "\n") != c.want {
				t.Errorf("%s: compiled leg printed %q, reference answers %q", c.src, a, c.want)
			}
		case 1:
			if !strings.Contains(a, "method") && !strings.Contains(a, "container") {
				t.Errorf("%s: compiled leg refused without naming the method or the container: %s", c.src, a)
			}
		default:
			t.Errorf("%s: compiled leg exited %d (%s); only 0 and 1 are legal here", c.src, ac, a)
		}
	}
}

// A mutator's answer is the void, so the call is not a value: `sum([1,2,3].append(4))` is the
// reference's TypeError, and an answer of 10 here means the container came back.
func TestCLIMutatorCannotBeUsedAsAValue(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []string{
		`print(sum([1, 2, 3].append(4)))`,
		`print(len([1].append(2)))`,
	} {
		f := writeSrc(t, dir, "notavalue.gy", src)
		ref, ok := cpythonPlainOut(t, dir, src)
		if ok {
			t.Fatalf("this table's premise is wrong: the reference answered %q for %s", ref, src)
		}
		got, code := cliRunMerged(t, "--interp", "--file", f)
		if code == 0 {
			t.Errorf("%s: interpreted leg answered %q at exit 0; the reference raises TypeError", src, got)
		}
	}
}
