package lang

// A container has the methods its reference's containers have (roadmap Gap R.188 / Gap R.63, ADR
// 0301). `xs.extend([2,3])`, `xs.insert(0,9)`, `xs.index(2)`, `xs.clear()`, `d.update(...)`,
// `d.pop(k)`, `d.setdefault(k, v)` and `d.clear()` answered `no such list method` / `no such dict
// method` on BOTH engines -- a missing answer rather than a wrong one, which is the ladder's second
// class and the reason no pin could have caught it: there was no output to compare.
//
// Every sentence below was read off the pinned reference, including the ones a caught exception
// shows (ADR 0215's rule: a catchable raise keeps CPython's wording, because programs match on it).

import (
	"strings"
	"testing"
)

func TestListHasItsReferenceMethods(t *testing.T) {
	cases := []struct{ src, want string }{
		{`xs = [1]
xs.extend([2, 3])
print(xs)`, "[1, 2, 3]"},
		{`xs = [1]
xs.extend([2, 3])
print(xs.append(1))`, "None"}, // extend is a mutator, so its answer is the void too
		{`xs = [1]
xs.insert(0, 9)
print(xs)`, "[9, 1]"},
		// An index past either end CLAMPS rather than failing -- the rule that makes
		// building a list by repeated insert(n, ...) work.
		{`xs = [1]
xs.insert(9, 9)
print(xs)`, "[1, 9]"},
		{`xs = [1]
xs.insert(-9, 0)
print(xs)`, "[0, 1]"},
		{`xs = [1, 2]
xs.insert(-1, 9)
print(xs)`, "[1, 9, 2]"},
		{`print([1, 2].index(2))`, "1"},
		// index finds by VALUE equality, so a verdict is a number's key here as everywhere else.
		{`print([1].index(True))`, "0"},
		{`xs = [1, 2, 3]
xs.remove(2)
print(xs)`, "[1, 3]"},
		{`xs = [1, 2]
xs.clear()
print(xs)`, "[]"},
		{`xs = []
xs.extend([1, 2])
xs.extend([3])
print(xs)`, "[1, 2, 3]"},
		{`xs = []
xs.extend({1, 2})
print(xs)`, "[1, 2]"},
	}
	for _, c := range cases {
		got := captureStdout(t, c.src)
		if strings.TrimSuffix(got, "\n") != c.want {
			t.Errorf("%s: printed %q, reference answers %q", c.src, got, c.want)
		}
	}
}

func TestDictHasItsReferenceMethods(t *testing.T) {
	cases := []struct{ src, want string }{
		{`d = {"a": 1}
d.update({"b": 2})
print(d)`, "{'a': 1, 'b': 2}"},
		// update over an existing key REPLACES the value and keeps the key's position.
		{`d = {"a": 1, "b": 2}
d.update({"a": 9})
print(d)`, "{'a': 9, 'b': 2}"},
		{`d = {"a": 1, "b": 2}
print(d.pop("a"))
print(d)`, "1\n{'b': 2}"},
		{`print({"a": 1}.pop("zz", 9))`, "9"},
		// setdefault WRITES, which is what makes the grouping idiom terminate at all.
		{`d = {}
d.setdefault("a", []).append(1)
print(d)`, "{'a': [1]}"},
		{`d = {"a": 1}
print(d.setdefault("a", 9))
print(d)`, "1\n{'a': 1}"},
		{`d = {}
d.setdefault("a")
print(d)`, "{'a': None}"},
		{`d = {"a": 1}
d.clear()
print(d)`, "{}"},
		// pop is an EXPRESSION -- it answers with what it removed -- while update/clear answer void.
		{`d = {"a": 1}
print(d.update({"b": 2}))`, "None"},
		{`d = {1: "a", 2: "b"}
print(d.pop(1))
print(d.pop(2))
print(d)`, "a\nb\n{}"},
	}
	for _, c := range cases {
		got := captureStdout(t, c.src)
		if strings.TrimSuffix(got, "\n") != c.want {
			t.Errorf("%s: printed %q, reference answers %q", c.src, got, c.want)
		}
	}
}

// The raises keep the reference's sentences, character for character: a program that catches one and
// prints it, and every conformance ledger that matches on text, sees the difference (ADR 0215).
func TestTheNewMethodsRaiseWhatTheReferenceRaises(t *testing.T) {
	cases := []struct{ src, want string }{
		{`print([1, 2].index(5))`, "ValueError: 5 is not in list"},
		{`print([1, 2].index(1, 5))`, "ValueError: 1 is not in list"},
		{`xs = [1, 2]
xs.remove(9)`, "ValueError: list.remove(x): x not in list"},
		{`print({"a": 1}.pop("z"))`, "KeyError: 'z'"},
		{`print({1: 2}.pop(5))`, "KeyError: 5"},
		{`print({}.popitem())`, "KeyError: popitem(): dictionary is empty"},
		{`xs = [1]
xs.extend(5)`, "TypeError: extend() argument must be a list or a set"},
	}
	for _, c := range cases {
		// The class and the sentence together, the way every other raise table here reads them
		// (ADR 0215): the traceback's last line is `Class: message`, and that line is what a program
		// matching on text would see.
		ee := trapRun(t, c.src)
		if got := ee.ExnType + ": " + ee.ExnMsg; got != c.want {
			t.Errorf("%s: raised %q, reference raises %q", c.src, got, c.want)
		}
	}
}

// popitem answers a PAIR. This language has no tuple value yet (L11.3), so the method exists and
// refuses with a sentence naming the missing representation -- rather than answering a list-shaped
// lie that print would render `[k, v]` where the reference renders `(k, v)`.
func TestPopitemRefusesUntilThereIsAPairValue(t *testing.T) {
	ee := trapRun(t, `print({1: 2}.popitem())`)
	got := ee.ExnType + ": " + ee.ExnMsg
	if !strings.Contains(got, "popitem") || !strings.Contains(got, "pair") {
		t.Errorf("popitem answered %q; it must refuse naming the pair, which L11.3 owes", got)
	}
}

// A KeyError names the KEY, not the failure: the reference's `d["a"]` over an empty dict is
// `KeyError: 'a'`, quoted because the key is a text and a KeyError carries its repr. The compiled
// leg still prints the module's constant and is Gap R.189's remaining half.
func TestKeyErrorNamesTheKey(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`d = {}
print(d["a"])`, "KeyError: 'a'"},
		{`d = {}
print(d[9])`, "KeyError: 9"},
		{`d = {1: 2}
print(d[9])`, "KeyError: 9"},
	} {
		ee := trapRun(t, c.src)
		if got := ee.ExnType + ": " + ee.ExnMsg; got != c.want {
			t.Errorf("%s: raised %q, reference answers %q", c.src, got, c.want)
		}
	}
}

// A dict is two parallel slices; a removal that shrinks one and not the other misaligns every later
// key against the wrong value, and the failure looks like a mystery three lines later.
func TestDictRemovalKeepsTheParallelSlicesAligned(t *testing.T) {
	got := captureStdout(t, `d = {1: "a", 2: "b", 3: "c"}
print(d.pop(2))
print(d)
print(d[1])
print(d[3])`)
	if want := "b\n{1: 'a', 3: 'c'}\na\nc"; strings.TrimSuffix(got, "\n") != want {
		t.Errorf("parallel slices misaligned after pop: printed %q, want %q", got, want)
	}
}
