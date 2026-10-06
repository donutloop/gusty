package lang

// A container method that mutates its receiver IN PLACE answers the void, not the container
// (roadmap Gap R.187, ADR 0300). The interpreter used to hand back the receiver so "the REPL could
// show the resulting list" — which made `print(xs.append(2))` print `[1, 2]` at exit 0 where the
// reference prints `None` — and the compiled leg lowered the mutation to no value at all, so the
// print door emitted `printf(i8* @.fmt1, i32 )`: a call with a MISSING operand, rejected by llc as
// an invalid module, which is exit 2, ADR 0166's forbidden class.
//
// The mutation was never wrong. Only the answer the statement throws away was — which is exactly
// why it survived: a program writes `xs.append(2)`, not `print(xs.append(2))`.

import (
	"strings"
	"testing"
)

// The void-answer table, read line by line off the pinned reference.
func TestInPlaceMutationAnswersTheVoid(t *testing.T) {
	cases := []struct{ src, want string }{
		{`xs = [1]
print(xs.append(2))`, "None"},
		{`xs = [1]
print(xs.append(2))
print(xs)`, "None\n[1, 2]"},
		{`xs = [1, 2]
print(xs.sort())`, "None"},
		{`xs = [1, 2]
print(xs.reverse())`, "None"},
		{`s = {1}
print(s.add(2))`, "None"},
		{`s = {1}
print(s.add(2))
print(s)`, "None\n{1, 2}"},
		{`s = {1}
print(s.discard(2))`, "None"},
		{`s = {1}
print(s.discard(1))
print(s)`, "None\nset()"},
		{`s = {1}
print(s.remove(1))`, "None"},
		{`s = {1}
print(s.clear())`, "None"},
		{`d = {"a": 1}
print(d.get("zzz"))`, "None"},
	}
	for _, c := range cases {
		got := captureStdout(t, c.src)
		if strings.TrimSuffix(got, "\n") != c.want {
			t.Errorf("%s: printed %q, reference answers %q", c.src, got, c.want)
		}
	}
}

// A mutator's answer being the void has a consequence the container-answer hid: the call is not a
// value, and feeding it to something that wants one is the reference's TypeError, not a number.
// `sum([1,2,3].append(4))` used to answer 10 here; the reference cannot get that far.
func TestAMutatorIsNotAValueInThePlacesAValueIsWanted(t *testing.T) {
	for _, src := range []string{
		`sum([1, 2, 3].append(4))`,
		`len([1].append(2))`,
		`abs({1}.add(2))`,
	} {
		if err := goldenRunError(t, src); err == nil {
			t.Errorf("%s answered; the reference raises TypeError because the mutator hands back None", src)
		}
	}
}

// `pop` and `popitem` REMOVE and ANSWER with what they removed — they are the mutators that are
// genuinely expressions, and `while xs: x = xs.pop()` is why. If the void table swept them in, this
// is the test that says so.
func TestPopStillAnswersWithWhatItTook(t *testing.T) {
	cases := []struct{ src, want string }{
		{`xs = [1, 2]
print(xs.pop())`, "2"},
		{`xs = [1, 2]
print(xs.pop(0))`, "1"},
		{`xs = [1, 2]
y = xs.pop()
print(y)
print(xs)`, "2\n[1]"},
	}
	for _, c := range cases {
		got := captureStdout(t, c.src)
		if strings.TrimSuffix(got, "\n") != c.want {
			t.Errorf("%s: printed %q, reference answers %q", c.src, got, c.want)
		}
	}
}

// The compiled leg must never emit the operand-less printf again, and must never reach exit 2 for
// any member of the family. A refusal is legal; an invalid module is not.
func TestCompiledMutationPrintsNoneOrRefusesButNeverTraps(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`xs = [1]
print(xs.append(2))`, "None"},
		{`s = {1}
print(s.add(2))`, "None"},
		{`xs = [1]
print(xs.sort())`, "None"},
		{`xs = [1]
print(xs.reverse())`, "None"},
		{`xs = [1, 2]
print(xs.pop())`, "2"},
	} {
		out, refused := compiledOutOrRefusal(t, c.src)
		if refused {
			if !strings.Contains(out, "container") && !strings.Contains(out, "method") {
				t.Errorf("%s: refused without saying why: %s", c.src, out)
			}
			continue
		}
		if strings.TrimSuffix(out, "\n") != c.want {
			t.Errorf("%s: compiled leg printed %q, reference answers %q", c.src, out, c.want)
		}
	}
}

// A program that defines its own method called `append` answers whatever ITS body returns; the void
// table belongs to the language's container methods, not to every name spelled `append`.
func TestAUserMethodNamedAppendKeepsItsOwnAnswer(t *testing.T) {
	got := captureStdout(t, `class Bag:
    def append(self, x):
        return 7

b = Bag()
print(b.append(1))`)
	if strings.TrimSuffix(got, "\n") != "7" {
		t.Errorf("a user method named append printed %q, want 7 — the void table is the language's mutators, not every spelling of append", got)
	}
}
