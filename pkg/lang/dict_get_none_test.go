package lang

// `d.get(key)` with the key absent hands back NONE (Gap R.174, ADR 0291).
//
// Both engines had a hole in the one place a dict lookup answers "not there":
//
//	print(d.get("z"))            CPython None · interpreter 0 · compiled refused
//	print({"a": 1}.get("z"))     CPython None · interpreter 0 · compiled refused ("key not found")
//	print(d.keys())              compiled refusal said "string method keys on non-constant STRING"
//
// The first two are the bare-word zero: a void leaving a builtin was written `return 0, nil`, which is the
// same representation Gap R.171 caught leaving a function body, and it printed as a number. The third is
// Gap R.38's rule — the refusal described a program the author never wrote. A `d` bound to a dict reached
// the string-method road, because that road is entered whenever the receiver is not a text the compiler
// can fold, and a dict name is not a text the compiler can fold either.
//
// The zero is fixed at the road that produces it; the wording is fixed at the road that emits it. Neither
// is fixed by giving the void a tag, which is L11.1's work and stays owed.

import (
	"strings"
	"testing"
)

func TestDictGetWithNoDefaultAnswersNone(t *testing.T) {
	rows := []struct {
		name string
		src  string
		want string
	}{
		// The literal written at the call, which is the shape the compiled fold handles.
		{"literal, key present", "print({\"a\": 1}.get(\"a\"))\n", "1\n"},
		{"literal, key absent", "print({\"a\": 1}.get(\"z\"))\n", "None\n"},
		{"literal, absent with a default", "print({\"a\": 1}.get(\"z\", 42))\n", "42\n"},
		{"literal, int key absent", "print({1: \"x\"}.get(9))\n", "None\n"},
		// (an int key whose VALUE is a text, and a text DEFAULT, are the same fold answering `0` —
		// they are pinned in TestADictAnswerThatIsATextStillOwesItsWord, not claimed here)
		{"literal, empty dict", "print({}.get(\"a\"))\n", "None\n"},
		// None is a value, so it compares, prints inside a container and survives a binding.
		{"compares equal to None", "v = {\"a\": 1}.get(\"z\")\nprint(v == None)\n", "True\n"},
		{"not equal to zero", "v = {\"a\": 1}.get(\"z\")\nprint(v == 0)\n", "False\n"},
		{"in an if test", "if {\"a\": 1}.get(\"z\") == None:\n    print(\"absent\")\nelse:\n    print(\"there\")\n", "absent\n"},
		// A truth test: None is falsey, so an absent key takes the else branch.
		{"falsey as a test", "if {\"a\": 1}.get(\"z\"):\n    print(\"truthy\")\nelse:\n    print(\"falsey\")\n", "falsey\n"},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			if got := captureStdout(t, r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q — a void printed as its word is Gap R.174", got, r.want)
			}
			if got := compiledOut(t, r.src); got != r.want {
				t.Errorf("--aot printed %q, want %q", got, r.want)
			}
		})
	}
}

// TestDictGetOnANameIsNotAnsweredByTheFold pins the honest half. The compiled backend folds a container
// method only over a literal written at the call; a name's slots belong to the runtime, and answering
// through a word would be the wrong-number class ADR 0166 counts as our bug. The interpreter answers
// every row, which is what makes the compiled leg's answer a refusal rather than a crash.
func TestDictGetOnANameIsNotAnsweredByTheFold(t *testing.T) {
	for _, src := range []string{
		"d = {\"a\": 1}\nprint(d.get(\"a\"))\n",
		"d = {\"a\": 1}\nprint(d.get(\"z\"))\n",
		"d = {\"a\": 1}\nprint(d.get(\"z\", 42))\n",
		"d = {\"a\": 1}\nprint(d.keys())\n",
	} {
		src := src
		t.Run(strings.TrimSpace(src), func(t *testing.T) {
			want := captureStdout(t, src) // the interpreter answers the reference on every row
			res, err := Compile(src)
			if err == nil {
				if out := runIR(t, res.IR); out != want {
					t.Errorf("--aot answered %q where the reference answers %q", out, want)
				}
				return
			}
			// The refusal must describe the program. "string method get on non-constant string"
			// about a dict was the second defect this row exists to remove (Gap R.38).
			for _, banned := range []string{"non-constant string", "not a text"} {
				if strings.Contains(err.Error(), banned) {
					t.Errorf("refusal calls a dict a string (`%s`): %v", banned, err)
				}
			}
			if !strings.Contains(err.Error(), "container") || !strings.Contains(err.Error(), `"d"`) {
				t.Errorf("refusal %q does not name the receiver and what it holds (Gap R.38)", err)
			}
			if strings.Contains(err.Error(), "global variable reference") || strings.Contains(err.Error(), "store i32 @") {
				t.Errorf("refusal leaked a malformed module: %v", err)
			}
		})
	}
}

// TestTheStringMethodRefusalDescribesItsReceiver is the Gap R.38 half on its own, split by receiver kind:
// a TEXT receiver must still be called a receiver, and a CONTAINER receiver must be called a container.
// One sentence cannot serve both, which is why the road asks the records before it speaks.
func TestTheStringMethodRefusalDescribesItsReceiver(t *testing.T) {
	container := "d = {\"a\": 1}\nprint(d.keys())\n"
	_, err := Compile(container)
	if err == nil {
		t.Skip("the compiled leg answers this now; the refusal is no longer owed")
	}
	if !strings.Contains(err.Error(), "container") {
		t.Errorf("a dict receiver was not called a container: %v", err)
	}
	if strings.Contains(err.Error(), "non-constant string") {
		t.Errorf("a dict was called a string: %v", err)
	}

	param := "def f(s):\n    return s.title()\n\n\nprint(f(\"abc\"))\n"
	_, err = Compile(param)
	if err == nil {
		t.Skip("the compiled leg answers this now")
	}
	if strings.Contains(err.Error(), "container") {
		t.Errorf("a text receiver was called a container: %v", err)
	}
}

// TestADictAnswerThatIsATextStillOwesItsWord pins what this cycle did NOT reach, so the None half above
// cannot be read as "dict.get is done". A dict whose answer is a TEXT prints 0 on the compiled leg —
// `print({1: "x"}.get(1))` says 0 where the reference says x, and `.get("z", "none-ish")` says 0 where the
// reference says none-ish — because a text answering through an i32 word is the intern table's position,
// and the slot carries no tag saying otherwise. Measured byte-identical on the pre-cycle binary: this is
// L11.1's tagged value word (and the same wall as Gap R.171's void), not something the get road can fix
// by itself.
func TestADictAnswerThatIsATextStillOwesItsWord(t *testing.T) {
	for _, r := range []struct{ src, want string }{
		{"print({1: \"x\"}.get(1))\n", "x\n"},
		{"print({\"a\": 1}.get(\"z\", \"none-ish\"))\n", "none-ish\n"},
		{"print([{\"a\": 1}.get(\"z\")])\n", "[None]\n"},
	} {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			if got := captureStdout(t, r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q — the interpreter has no excuse for a kind it can see", got, r.want)
			}
			res, err := Compile(r.src)
			if err != nil {
				return // a refusal is honest and promotable
			}
			if out := runIR(t, res.IR); out == r.want {
				t.Log("the compiled leg now answers this; promote the row into TestDictGetWithNoDefaultAnswersNone")
			}
		})
	}
}

// TestDictGetStillAnswersItsDefaultKeepsTheOtherHalf pins what already worked, so the None half cannot
// have quietly taken the default half with it.
func TestDictGetStillAnswersItsDefaultKeepsTheOtherHalf(t *testing.T) {
	for _, r := range []struct{ src, want string }{
		{"print({\"a\": 1}.get(\"a\", 9))\n", "1\n"},
		{"print({\"a\": 1}.get(\"b\", 9))\n", "9\n"},
		{"print({1: 2, 3: 4}.get(3))\n", "4\n"},
		{"print({1: 2, 3: 4}.get(9, 0))\n", "0\n"},
	} {
		r := r
		t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
			if got := captureStdout(t, r.src); got != r.want {
				t.Errorf("interpreter printed %q, want %q", got, r.want)
			}
			if got := compiledOut(t, r.src); got != r.want {
				t.Errorf("--aot printed %q, want %q", got, r.want)
			}
		})
	}
}
