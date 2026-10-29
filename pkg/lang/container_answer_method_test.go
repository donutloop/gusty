package lang

// container_answer_method_test.go — a method run on the container a call hands back is answered by the
// roads that answer the same method on the name that container was bound to (roadmap **Gap R.229**,
// **ADR 0458**).
//
// Four doors already read the object ADR 0333's scan proves a call hands back — `print(b())`,
// `str(b())`, `if b():`, `for v in b():`, `ys = b()` — and a fifth position, asking that object to DO
// something, fell through every one of them to the text road:
//
//	def b():
//	    return [1, 2]
//	print(b().append(3))   # CPython: None      this toolchain: exit 1, Gap I.2's "string-method append
//	                       #                    on a receiver that is not a text the compiler can read"
//
// The bound spelling — `ys = b()` / `ys.append(3)` — has answered the reference's bytes since ADR 0333.
// So one program had an answer when the name was written and lost it when the name was left out, which
// is ADR 0334's judgement about a subscript (`build()[0]` vs `ys[0]`) applied one construct further out:
// the road was chosen from the base's SPELLING rather than from what the compiler holds once the answer
// is lowered.
//
// The rules these tests hold, in the order the row states them:
//
//   - The REWRITE, not a second reader: the receiver becomes the rooted name the answer was recorded
//     under and the position lowers the copied node, so the mutator road, the bounds raise, the set
//     hash and the pop door are the ones that answer the bound name. `TestTheMethodOnACallResultIsThe
//     BoundFormsMethod` is the parity that makes this a defect rather than a missing feature.
//   - The void and the value are one question (ADR 0300): `print(b().append(3))` may not call the call
//     a value while the call road calls it a mutation — the symptom of getting that wrong was
//     `printf(…, i32 )`, which `llc` rejects at ADR 0166's exit 2. `TestNoAnsweredShapeDiesInTheAssembler`
//     holds the line.
//   - A refusal stays the road's own, and names the receiver the PROGRAM wrote. The door invents a name
//     (`__cans7`); a refusal quoting it would blame the compiler for the program's missing door
//     (ADR 0166), and `TestARefusalNamesTheReceiverTheProgramWrote` is the guard on that.
//   - The door serves only what the roads serve for a container VARIABLE. `dict.get`, `list.index`,
//     `list.count` refuse for `ys` too, so they refuse for `b()` with the same sentence — Gap R.174's
//     standing, not this row's (roadmap Gap R.538).
//
// Every `want` below is the reference's, read off `python3`;
// `integration/container_answer_method_test.go` runs the same programs through the CLI and derives its
// expectations from a live CPython, not from this file.

import (
	"strings"
	"testing"
)

// containerAnswerMethodPrograms are the answered shapes: every one is the same program as its bound
// twin, written one statement shorter.
var containerAnswerMethodPrograms = []struct {
	src  string
	want string
}{
	// The list mutators. `append` answers the void, and the void has no digits: the first row is the
	// one that began the row's life as an `llc` rejection.
	{"def b():\n    return [1, 2]\n\nprint(b().append(3))", "None"},
	{"def b():\n    return [1, 2]\n\nb().append(3)\nprint(b())", "[1, 2]"},
	{"def b():\n    return [1, 2]\n\nprint(b().append(3), b().pop())", "None 2"},
	{"def b():\n    return [1, 2]\n\nprint(b().append(1.5))", "None"},
	{"def b():\n    return [1, 2]\n\nb().append(1.5)\nprint(b())", "[1, 2]"},
	{"def b():\n    return [1, 2]\n\nprint(b().append(3), b().append(4))", "None None"},
	{"def b():\n    return [2, 1]\n\nprint(b().sort())", "None"},
	{"def b():\n    return [2, 1]\n\nb().sort()\nprint(b())", "[2, 1]"},
	{"def b():\n    return [1, 2]\n\nprint(b().reverse())", "None"},
	{"def b():\n    return [1, 2]\n\nb().reverse()\nprint(b())", "[1, 2]"},
	// The slot the popped element hands back is rendered by the door that knows what the slots hold —
	// the pair door beside the mutator road, asked of the materialised name.
	{"def b():\n    return [1, 2]\n\nprint(b().pop())", "2"},
	{"def b():\n    return [1, 2]\n\nprint(b().pop(0))", "1"},
	{"def b():\n    return [1, 2]\n\nprint(b().pop(-1))", "2"},
	{"def b():\n    return [\"a\", \"b\"]\n\nprint(b().pop())", "b"},
	{"def b():\n    return [\"a\", \"b\"]\n\nprint(b().pop(0))", "a"},
	{"def b():\n    return [1, \"a\", None]\n\nprint(b().pop())", "None"},
	{"def b():\n    return [1, \"a\", None]\n\nprint(b().pop(0))", "1"},
	{"def b():\n    return [1.5, 2]\n\nprint(b().pop(0))", "1.5"},
	{"def b():\n    return [[1, 2], [3]]\n\nprint(b().pop())", "[3]"},
	// The set mutators.
	{"def s():\n    return {1}\n\nprint(s().add(3))", "None"},
	{"def s():\n    return {1}\n\ns().add(3)\nprint(s())", "{1}"},
	{"def s():\n    return {1, 2}\n\nprint(s().discard(1))", "None"},
	{"def s():\n    return {1, 2}\n\ns().discard(1)\nprint(s())", "{1, 2}"},
	{"def s():\n    return {1, 2}\n\ns().remove(1)\nprint(s())", "{1, 2}"},
	{"def s():\n    return {1, 2}\n\nprint(s().clear())", "None"},
	{"def s():\n    return {1, 2}\n\ns().clear()\nprint(s())", "{1, 2}"},
	// The answer the slot table could not describe — `xs.append(g())` leaves the record silent about the
	// slots — is still an object the mutator roads write, and the doors that ask the OBJECT take it
	// (Gap R.261, ADR 0344).
	{"def g():\n    return 5\n\ndef b():\n    xs = []\n    xs.append(g())\n    return xs\n\nprint(b().append(3))", "None"},
	{"def g():\n    return 5\n\ndef b():\n    xs = []\n    xs.append(g())\n    return xs\n\nb().append(3)\nprint(b())", "[5]"},
	// The positions that read the void: print, str, an f-string field, the REPL's echo. Each of them
	// lowered the void's MISSING word into an operand before this row (ADR 0300's class, ADR 0166's exit).
	{"def b():\n    return [1, 2]\n\nprint(str(b().append(3)))", "None"},
	{"def b():\n    return [1, 2]\n\nprint(f\"{b().append(3)}\")", "None"},
	{"def b():\n    return [1, 2]\n\nprint(f\"{b().reverse()}\")", "None"},
	{"def b():\n    return [1, 2]\n\nprint(\"v\", b().append(3), \"w\")", "v None w"},
	// A method call inside a function body, and inside a loop: the answer is a fresh object each call,
	// so the mutation reaches nobody else's view of it (the aliasing rule the row had to measure).
	{"def b():\n    return [1, 2]\n\ndef use():\n    b().append(3)\n    return 1\n\nprint(use())", "1"},
	{"def b():\n    return [1, 2]\n\nfor i in range(2):\n    b().append(i)\nprint(b())", "[1, 2]"},
	{"def b():\n    return [1, 2]\n\nb().append(3)\nb().append(4)\nprint(b())", "[1, 2]"},
	{"def b():\n    return [1, 2]\n\nb().append(3)\nprint(b()[0])", "1"},
	// What the answer was already answering, unchanged by the door beside it.
	{"def b():\n    return [1, 2]\n\nprint(len(b()))", "2"},
	{"def b():\n    return [1, 2]\n\nprint(1 in b())", "True"},
	{"def b():\n    return [1, 2]\n\nfor v in b():\n    print(v)", "1\n2"},
}

// TestAMethodOnACallResultAnswersWhatTheReferenceAnswers is the row's own claim on the compiled leg.
func TestAMethodOnACallResultAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range containerAnswerMethodPrograms {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tt.src), "\n"); got != tt.want {
				t.Fatalf("%q printed %q, want %q (the reference's answer, read off python3)", tt.src, got, tt.want)
			}
		})
	}
}

// TestTheMethodOnACallResultIsTheBoundFormsMethod is the parity that makes this a defect rather than a
// missing feature. For each program, `ys = b()` followed by the same method on `ys` must give the very
// same stdout and the very same trap.
func TestTheMethodOnACallResultIsTheBoundFormsMethod(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		direct string
		bound  string
	}{
		{
			"def b():\n    return [1, 2]\n\nprint(b().append(3))",
			"def b():\n    return [1, 2]\n\nys = b()\nprint(ys.append(3))",
		},
		{
			"def b():\n    return [1, 2]\n\nb().append(3)\nprint(b())",
			// The bound twin keeps the PRINT on the callee, not on the name: `b().append(3)` mutates the
			// fresh object the call handed back and throws it away, so the reference prints `[1, 2]` for
			// both spellings — the aliasing rule this row had to measure before opening the door.
			"def b():\n    return [1, 2]\n\nys = b()\nys.append(3)\nprint(b())",
		},
		{
			"def b():\n    return [1, 2]\n\nprint(b().pop())",
			"def b():\n    return [1, 2]\n\nys = b()\nprint(ys.pop())",
		},
		{
			"def b():\n    return [\"a\", \"b\"]\n\nprint(b().pop())",
			"def b():\n    return [\"a\", \"b\"]\n\nys = b()\nprint(ys.pop())",
		},
		{
			"def b():\n    return [1, \"a\", None]\n\nprint(b().pop())",
			"def b():\n    return [1, \"a\", None]\n\nys = b()\nprint(ys.pop())",
		},
		{
			"def b():\n    return [2, 1]\n\nb().sort()\nprint(b())",
			// The callee is asked again on the right of both pairs: the reference builds a NEW list per
			// call, so the mutation the statement discards must not reach the print — the aliasing rule
			// this row measured before it opened the door.
			"def b():\n    return [2, 1]\n\nys = b()\nys.sort()\nprint(b())",
		},
		{
			"def b():\n    return [1, 2]\n\nprint(b().reverse())",
			"def b():\n    return [1, 2]\n\nys = b()\nprint(ys.reverse())",
		},
		{
			"def s():\n    return {1}\n\ns().add(3)\nprint(s())",
			"def s():\n    return {1}\n\nys = s()\nys.add(3)\nprint(s())",
		},
		{
			"def s():\n    return {1, 2}\n\ns().discard(1)\nprint(s())",
			"def s():\n    return {1, 2}\n\nys = s()\nys.discard(1)\nprint(s())",
		},
		{
			"def s():\n    return {1, 2}\n\ns().remove(1)\nprint(s())",
			"def s():\n    return {1, 2}\n\nys = s()\nys.remove(1)\nprint(s())",
		},
		{
			"def s():\n    return {1, 2}\n\ns().clear()\nprint(s())",
			"def s():\n    return {1, 2}\n\nys = s()\nys.clear()\nprint(s())",
		},
		{
			"def b():\n    return []\n\nprint(b().pop())",
			"def b():\n    return []\n\nys = b()\nprint(ys.pop())",
		},
		{
			"def b():\n    return [1, 2]\n\nb().pop(9)",
			"def b():\n    return [1, 2]\n\nys = b()\nys.pop(9)",
		},
		{
			"def b():\n    return [1, 2]\n\ntry:\n    b().pop(9)\nexcept IndexError:\n    print(\"caught\")",
			"def b():\n    return [1, 2]\n\nys = b()\ntry:\n    ys.pop(9)\nexcept IndexError:\n    print(\"caught\")",
		},
	} {
		t.Run(strings.ReplaceAll(tc.direct, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			directOut, directTrap := runForParity(t, tc.direct)
			boundOut, boundTrap := runForParity(t, tc.bound)
			if directOut != boundOut || directTrap != boundTrap {
				t.Fatalf("one program, two answers: %q gave (%q, %q), %q gave (%q, %q)",
					tc.direct, directOut, directTrap, tc.bound, boundOut, boundTrap)
			}
			if directTrap != "" && directOut != "" {
				t.Fatalf("%q wrote %q before the raise it shares with %q; the reference writes nothing",
					tc.direct, directOut, tc.bound)
			}
		})
	}
}

// TestAMutatorTrapOnACallResultIsWhatTheReferenceRaises covers the raises the mutator roads own: the
// bounds words, the set's miss, and the immutability of the tuple answer. A trap that ends the run
// without a handler and a trap the program can take are both program results; exit 2 is neither.
func TestAMutatorTrapOnACallResultIsWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src     string
		class   string
		message string
	}{
		{"def b():\n    return [1, 2]\n\nb().pop(9)", "IndexError", "pop index out of range"},
		{"def b():\n    return []\n\nb().pop()", "IndexError", "pop from empty list"},
		{"def b():\n    return [1, 2]\n\nprint(b().pop(9))", "IndexError", "pop index out of range"},
		{"def s():\n    return {1, 2}\n\ns().remove(9)", "KeyError", "9"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("%q raised %s: %s; the reference raises %s: %s", tt.src, class, message, tt.class, tt.message)
			}
		})
	}
}

// TestAMutatorTrapOnACallResultIsCatchable is the same raise asked of the program.
func TestAMutatorTrapOnACallResultIsCatchable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"def b():\n    return [1, 2]\n\ntry:\n    b().pop(9)\nexcept IndexError:\n    print(\"caught\")", "caught"},
		{"def b():\n    return []\n\ntry:\n    b().pop()\nexcept IndexError as e:\n    print(\"e\", e)", "e pop from empty list"},
		{"def s():\n    return {1, 2}\n\ntry:\n    s().remove(9)\nexcept KeyError:\n    print(\"miss\")", "miss"},
		{"def b():\n    return [1, 2]\n\ntry:\n    print(b().pop(0), b().pop(9))\nexcept IndexError:\n    print(\"caught\")", "caught"},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tc.src), "\n"); got != tc.want {
				t.Fatalf("%q printed %q, want the handler to have printed %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestTheTuplesImmutableAnswerRefusesTheMutator keeps L11.3's gate in front of the door: the tuple is
// the immutable container, and the reference raises rather than answering `(1, 2, 3)`.
func TestTheTuplesImmutableAnswerRefusesTheMutator(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ src, want string }{
		{"def t():\n    return (1, 2)\n\nt().append(4)", "'tuple' object has no attribute 'append'"},
		{"def t():\n    return (1, 2)\n\nt().sort()", "'tuple' object has no attribute 'sort'"},
		{"def t():\n    return (1, 2)\n\nt().reverse()", "'tuple' object has no attribute 'reverse'"},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			_, refused := compiledOutOrRefusal(t, tc.src)
			if refused {
				// The refusal in words is the row's own answer today; the class it owes is the reference's
				// AttributeError, which is L11.3's raise row rather than this one's.
				return
			}
			class, message := jitTrap(t, tc.src)
			if class != "AttributeError" || message != tc.want {
				t.Fatalf("%q raised %s: %s, want AttributeError: %s", tc.src, class, message, tc.want)
			}
		})
	}
}

// TestARefusalNamesTheReceiverTheProgramWrote is ADR 0166's rule about refusals, read against a door
// that invents a name. The materialised slot is called `__cansN`; a sentence quoting it would report
// the compiler's own bookkeeping as the program's missing door, and an agent consuming the refusal
// would go looking for a name the source never contains.
func TestARefusalNamesTheReceiverTheProgramWrote(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"def d():\n    return {1: 2}\n\nprint(d().get(3))",
		"def d():\n    return {1: 2}\n\nprint(d().keys())",
		"def d():\n    return {1: 2}\n\nprint(d().popitem())",
		"def d():\n    return {1: 2}\n\nd().clear()\nprint(d())",
		"def b():\n    return [1, 2]\n\nprint(b().index(2))",
		"def b():\n    return [1, 2]\n\nprint(b().count(1))",
		"def b():\n    return [1, 2]\n\nprint(b().copy())",
		"def b():\n    return [1, 2]\n\nb().extend([3])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, src)
			if !refused {
				t.Fatalf("%q answered %q where the row records a refusal (Gap R.174's standing)", src, out)
			}
			if strings.Contains(out, "__cans") || strings.Contains(out, "__mans") {
				t.Fatalf("%q refused with the door's own invented name:\n%s", src, out)
			}
			if strings.Contains(out, "llc") || strings.Contains(out, "verify") {
				t.Fatalf("%q failed as a toolchain rejection: %s", src, out)
			}
		})
	}
}

// TestTheMethodDoorReadsTheRoadsOwnDecline pins the two sentences the door recognises as *"this is not
// a method I serve on a container"*. A rewording the door stopped recognising would turn an answered
// shape back into a refusal about the wrong thing, and would do it silently.
func TestTheMethodDoorReadsTheRoadsOwnDecline(t *testing.T) {
	t.Parallel()
	// The container-name sentence (Gap R.174's) and the text-method sentence (Gap I.2's) must both
	// still be printable by the roads, and the door must still read both as a decline.
	for _, probe := range []struct {
		name string
		msg  string
	}{
		{"the container-name sentence", `codegen: get() on "ys", which the program bound to a container rather than a text: ...`},
		{"the text-method sentence", `codegen: string-method get on a receiver that is not a text the compiler can read: d(...)`},
	} {
		t.Run(probe.name, func(t *testing.T) {
			t.Parallel()
			if !containerMethodRoadDeclinesThisMethod(probe.msg) {
				t.Fatalf("%s is no longer read as a decline; the door would report it as the program's own refusal", probe.name)
			}
		})
	}
	for _, probe := range []struct {
		name string
		msg  string
	}{
		{"a pair refusal", "codegen: the pair the argument carries cannot travel into this container"},
		{"a bounds refusal", "codegen: index out of range"},
	} {
		t.Run(probe.name+" stays a real refusal", func(t *testing.T) {
			t.Parallel()
			if containerMethodRoadDeclinesThisMethod(probe.msg) {
				t.Fatalf("%q would be swallowed by the door and replaced by a refusal about the wrong thing", probe.msg)
			}
		})
	}
	// And the roads still print both, over a program that reaches them.
	for _, tc := range []struct{ src, want string }{
		{"def d():\n    return {1: 2}\n\nys = d()\nprint(ys.get(3))", "which the program bound to a container rather than a text"},
		{"def b():\n    return [1, 2]\n\nprint(b().upper())", "on a receiver that is not a text the compiler can read"},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if !refused || !strings.Contains(out, tc.want) {
				t.Fatalf("%q refused %q; the sentence %q is the one the door recognises", tc.src, out, tc.want)
			}
		})
	}
}

// TestTheMethodDoorServesWhatTheVariableRoadsServe asks the predicate the row claims, in both
// directions: what it claims served must answer on the compiled leg, and what it does not must still
// be refused by the road rather than answered by the door.
func TestTheMethodDoorServesWhatTheVariableRoadsServe(t *testing.T) {
	t.Parallel()
	served := map[containerAnswerKind][]string{
		listAnswer: {"append", "pop", "sort", "reverse"},
		setAnswer:  {"add", "discard", "remove", "clear"},
	}
	for kind, names := range served {
		for _, n := range names {
			if !containerAnswerMethodIsServed(kind, n) {
				t.Errorf("%s.%s() is answered on the compiled leg and the predicate denies it", kind, n)
			}
		}
	}
	// The readers the container roads do not serve for a VARIABLE (Gap R.174) are not served for an
	// answer either — and the predicate says so, so a widening of the predicate without a widening of
	// the road fails here rather than in front of a user.
	for _, pair := range [][2]string{
		{"list", "index"}, {"list", "count"}, {"list", "copy"}, {"list", "extend"}, {"list", "insert"},
		{"dict", "get"}, {"dict", "keys"}, {"dict", "values"}, {"dict", "items"}, {"dict", "popitem"},
		{"dict", "setdefault"}, {"dict", "update"},
	} {
		kind := notAContainerAnswer
		switch pair[0] {
		case "list":
			kind = listAnswer
		case "dict":
			kind = dictAnswer
		}
		if containerAnswerMethodIsServed(kind, pair[1]) {
			t.Errorf("the predicate claims %s.%s() is served; the container roads refuse it for a variable too (Gap R.174)", pair[0], pair[1])
		}
	}
}

// TestNoAnsweredShapeDiesInTheAssembler is the exit-2 guard (ADR 0166): the row began as a refusal and
// as an `llc` rejection, and a row that ends as a rejection has only changed class.
func TestNoAnsweredShapeDiesInTheAssembler(t *testing.T) {
	t.Parallel()
	for _, src := range containerAnswerMethodPrograms {
		t.Run(strings.ReplaceAll(src.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			mustVerifyWithLLC(t, emittedModuleIR(t, src.src))
		})
	}
}

// TestTheTextRoadStillOwnsTheTextReceiver is the negative control: the door this row grew runs only
// where the receiver is a call the container scan proves, and the text roads keep every literal they
// folded before. A row that starts allocating for `"ab".upper()` got the gate wrong.
func TestTheTextRoadStillOwnsTheTextReceiver(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`print("ab".upper())`,
		`print(", ".join(["a", "b"]))`,
		`print("ab".replace("a", "b"))`,
		"def s():\n    return {1}\n\ns().add(3)",
	} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			main := mainBodyOf(emittedModuleIR(t, src))
			if strings.Contains(src, "upper") || strings.Contains(src, "join") || strings.Contains(src, "split") {
				for _, banned := range []string{"@rt_alloc", "@rt_list_from", "@rt_set_add"} {
					if strings.Contains(main, banned) {
						t.Fatalf("%q now asks %s: the text road answered this before this row and still must", src, banned)
					}
				}
			}
		})
	}
}

// TestWhatTheMethodDoorStillCannotReachIsFiledNotFixed keeps the neighbours of this door visible, each
// with the reference's answer beside what this backend says today. A row that stops refusing moves; it
// does not disappear.
func TestWhatTheMethodDoorStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this backend answers today ("" = it refuses)
		oracle   string // what the reference answers
		gap      string
	}{
		{
			"a dict asked for a value by the reader roads",
			"def d():\n    return {1: 2}\n\nprint(d().get(3))",
			"", "None",
			"roadmap Gap R.538 (the reader methods refuse for a container VARIABLE too — Gap R.174's standing, not this door's)",
		},
		{
			"a dict asked to remove an entry",
			"def d():\n    return {1: 2}\n\nprint(d().popitem())",
			"", "(1, 2)",
			"roadmap Gap R.538",
		},
		{
			"a dict cleared",
			"def d():\n    return {1: 2}\n\nd().clear()\nprint(d())",
			"", "{1: 2}",
			"roadmap Gap R.538 (the dict mutator road folds a literal written at the call and asks nothing of a run-built object)",
		},
		{
			"a list asked where an element sits",
			"def b():\n    return [1, 2]\n\nprint(b().index(2))",
			"", "1",
			"roadmap Gap R.538",
		},
		{
			"a list copied",
			"def b():\n    return [1, 2]\n\nprint(b().copy())",
			"", "[1, 2]",
			"roadmap Gap R.538",
		},
		{
			"a list grown by several members",
			"def b():\n    return [1, 2]\n\nb().extend([3])\nprint(b())",
			"", "[1, 2]",
			"roadmap Gap R.538 (extend refuses for `ys` as well; the fresh object each call answers keeps the program's own answer either way)",
		},
		{
			"a list member removed by value",
			"def b():\n    return [1, 2]\n\nb().remove(5)",
			"", "ValueError: list.remove(x): x not in list",
			"roadmap Gap R.538",
		},
		// The void the mutator answers at a CONDITION or a BINDING is refused by neither door and
		// dies in the assembler instead; those two shapes are pinned by their own exit-2 row,
		// `TestTheVoidAtTheConditionAndBindingDoorsDiesInTheAssembler` (roadmap Gap R.537), because
		// a table that can speak only of answers and refusals would call an exit 2 a refusal.
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tc.src)
			if tc.compiled == "" {
				if !refused {
					t.Fatalf("%q answered %q where the row records a refusal — the door grew, so move the row into the tables above (reference: %q)", tc.src, out, tc.oracle)
				}
				if strings.Contains(out, "llc") || strings.Contains(out, "verify") {
					t.Fatalf("%q failed as a toolchain rejection: %s", tc.src, out)
				}
				return
			}
			if refused {
				t.Fatalf("%q refuses (%s); the row records the answer %q for %s", tc.src, out, tc.compiled, tc.gap)
			}
			if got := strings.TrimRight(out, "\n"); got != tc.compiled {
				t.Fatalf("%q answered %q, the answer recorded for %s was %q (reference: %q)", tc.src, got, tc.gap, tc.compiled, tc.oracle)
			}
		})
	}
}

// TestTheVoidAtTheConditionAndBindingDoorsDiesInTheAssembler pins Gap R.537 with today's bytes: the
// shape reaches `llc`, which is ADR 0166's compiler-bug class, and the pin exists so the row cannot be
// quietly forgotten — a case that starts ANSWERING fails here too, and the row moves out.
func TestTheVoidAtTheConditionAndBindingDoorsDiesInTheAssembler(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"the answer as a condition", "def b():\n    return [1, 2]\n\nif b().append(3):\n    print(\"y\")\nelse:\n    print(\"n\")"},
		{"the variable as a condition", "xs = [1, 2]\nif xs.append(3):\n    print(\"y\")\nelse:\n    print(\"n\")"},
		{"the answer bound to a name", "def b():\n    return [1, 2]\n\nx = b().append(3)\nprint(x)"},
		{"the variable bound to a name", "xs = [1, 2]\nx = xs.append(3)\nprint(x)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				if strings.Contains(err.Error(), "llc") || strings.Contains(err.Error(), "invalid") {
					return // the pinned failure
				}
				t.Fatalf("%q: %v", tc.src, err)
			}
			if res.Code == 2 {
				return // the pinned failure, reported as the toolchain's own
			}
			t.Fatalf("%q now runs (exit %d, %q): Gap R.537 is paid — delete this pin and move the program into the answered table",
				tc.src, res.Code, res.Output)
		})
	}
}

// The record leg: every answered row has an answer on record, so the door cannot be deleted without
// deleting the evidence the suite reads (ADR 0302's rule that a missing record fails the case).
func TestTheMethodOnACallResultRowsAreRecorded(t *testing.T) {
	t.Parallel()
	for _, tt := range containerAnswerMethodPrograms {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.Repr == "" && rec.Stdout == "" && rec.Err == "" {
				t.Fatalf("%q has no recorded answer: re-record it before shipping the door that answers it", tt.src)
			}
		})
	}
}
