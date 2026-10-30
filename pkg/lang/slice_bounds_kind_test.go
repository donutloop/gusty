package lang

import (
	"regexp"
	"strings"
	"testing"
)

// slice_bounds_kind_test.go — a slice bound is asked what it is: a position, the absence of one, or the
// reference's TypeError (roadmap Gap R.233, ADR 0336).
//
// `rt_slice` takes seven i32s — handle, three bounds, three presence flags — and the road handed every bound to
// the generic expression door without asking what it held:
//
//	xs = [1, 2]
//	print(xs["a":"b"])   # TypeError: slice indices must be integers or None or have an __index__ method
//	                     # this toolchain before the row: [1]   at exit 0
//	print(xs[1.5:2])     # the reference raises;            this toolchain: [2]   at exit 0
//	print(xs[[0]:2])     # the reference raises;            this toolchain: exit 2 (`llc` rejected the module)
//	s  = "abcd"
//	print(s["a":"b"])    # the reference raises;            this toolchain: b     at exit 0
//
// …and the bound the reference excuses, `None`, was read as a position too, because `None` lowers to the word 0
// and the presence flag was set beside it as though the program had written a number:
//
//	print(xs[None:None]) # [1, 2] in the reference;  this toolchain: []  at exit 0
//	print(s[None:None])  # abcd   in the reference;  this toolchain: (empty) at exit 0
//	print(xs[::None])    # [1, 2] in the reference;  this toolchain: exit 2 (the runtime's own step loop)
//
// One question, three handles: an integer (or a bool, which is an int) is a position, `None` is the absence of
// one, and anything else the compiler can name the kind of is the reference's `TypeError`. `Gap R.222`'s gate
// (ADR 0329) already asks it of an `Index`'s position; this is the same door with the bounds in it.
//
// Every `want` and every trap below is the reference's, read off `python3`; the integration file runs the same
// programs through the CLI and derives its expectations from the reference, not from this file.

// sliceBoundsAnswered are the shapes whose bounds are positions or absences. They are the half that must keep
// answering — and the `None` rows are the ones this row newly answers.
var sliceBoundsAnswered = []struct {
	src  string
	want string
}{
	// A bound written as None is a bound the program did not write.
	{"xs = [1, 2]\nprint(xs[None:None])", "[1, 2]"},
	{"xs = [1, 2]\nprint(xs[None:2])", "[1, 2]"},
	{"xs = [1, 2]\nprint(xs[1:None])", "[2]"},
	{"xs = [1, 2]\nprint(xs[::None])", "[1, 2]"},
	{"xs = [1, 2]\ni = None\nprint(xs[i:None])", "[1, 2]"},
	{"s = \"abcd\"\nprint(s[None:None])", "abcd"},
	{"s = \"abcd\"\nprint(s[None:2])", "ab"},
	{"s = \"abcd\"\nprint(s[1:None])", "bcd"},
	{"s = \"abcd\"\nprint(s[::None])", "abcd"},
	// A bool is an int, so a bool bound is a position — and the reference agrees by taking True as 1.
	{"xs = [1, 2, 3]\nprint(xs[True:True])", "[]"},
	{"xs = [1, 2, 3]\nprint(xs[True:3])", "[2, 3]"},
	{"xs = [1, 2, 3]\nprint(xs[False:True])", "[1]"},
	// Positions the compiler reads, in every base the slice door answers.
	{"xs = [1, 2, 3]\nprint(xs[0:2])", "[1, 2]"},
	{"xs = [1, 2, 3]\nprint(xs[1:], xs[:2], xs[::-1], xs[-2:])", "[2, 3] [1, 2] [3, 2, 1] [2, 3]"},
	{"xs = [1, 2, 3]\nprint(xs[-9:9])", "[1, 2, 3]"},
	{"xs = [1, 2, 3]\nprint(xs[5:9])", "[]"},
	{"xs = [1, 2, 3]\nk = 2\nprint(xs[1:k])", "[2]"},
	{"xs = [1, 2, 3]\nk = 1\nprint(xs[k:k + 2])", "[2, 3]"},
	{"print([1, 2, 3][1:])", "[2, 3]"},
	{"print([1.5, \"a\", [7]][0:2])", "[1.5, 'a']"},
	{"xs = [1.5, \"a\", [7]]\nprint(xs[None:2])", "[1.5, 'a']"},
	{"def b():\n    return [1, 2, 3]\nprint(b()[None:2])", "[1, 2]"},
	{"def b():\n    return [1, 2, 3]\nprint(b()[1:])", "[2, 3]"},
	{"def b():\n    return [1, 2, 3]\nys = b()[None:2]\nprint(ys)", "[1, 2]"},
	{"s = \"abcdef\"\nprint(s[1:3])", "bc"},
	{"print(\"abcdef\"[::-1])", "fedcba"},
	// The copy of a copy keeps the same bound rule.
	{"xs = [1, 2, 3]\nprint(xs[0:2][None:1])", "[1]"},
	// A step the compiler cannot read is a step the row guards rather than refuses: the guard is beside the
	// copy, and an ordinary stride answers exactly what it answered before the row (roadmap Gap R.236, ADR 0337).
	{"xs = [1, 2, 3, 4]\nk = 2\nprint(xs[::k])", "[1, 3]"},
	{"def z(n):\n    return n\nxs = [1, 2, 3, 4]\nprint(xs[::z(2)])", "[1, 3]"},
	{"xs = [1, 2, 3, 4]\nprint(xs[0:4:2])", "[1, 3]"},
	{"xs = [1, 2, 3, 4]\nk = 3\nprint(xs[::k])", "[1, 4]"},
	// A `return` whose value is a slice — **Gap R.238**, **ADR 0465**. The copy `rt_slice` builds is a list like
	// any other (its slots are the base's payloads copied WITH their tags, **ADR 0294**), and every position below
	// already answered for a `return` that spelled a LITERAL (**ADR 0333**); what was missing was the scan's arm, so
	// the answer travelled as one word and the caller printed the handle. The five shapes the row measured first
	// (`print`, a loop, `len`, `==`, a mixed base) are the first five rows; the rest are the positions a copy has to
	// reach once it is a value at all — an open bound, a stride, an index into the answer, a text's copy (which
	// stays a TEXT), a copy of another call's answer, a nested copy, a concatenation, the empty copy, a condition,
	// `str`, and two arms where one returns a literal and the other a copy.
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nprint(n())", "[2, 3]"},
	{"def n():\n    xs = [1.5, \"a\", [7]]\n    return xs[0:2]\nprint(n())", "[1.5, 'a']"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nfor v in n():\n    print(v)", "2\n3"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nprint(len(n()))", "2"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nprint(n() == [2, 3])", "True"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nys = n()\nprint(ys)", "[2, 3]"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[0:3:2]\nprint(n())", "[1, 3]"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:]\nprint(n())", "[2, 3]"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[:2]\nprint(n())", "[1, 2]"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nprint(n()[0])", "2"},
	{"def n():\n    s = \"abcd\"\n    return s[0:2]\nprint(n())", "ab"},
	{"def build():\n    return [7, 8, 9]\n\ndef n():\n    return build()[0:2]\nprint(n())", "[7, 8]"},
	{"def n():\n    xs = [[1, 2], [3, 4]]\n    return xs[0:1]\nprint(n())", "[[1, 2]]"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nprint(n() + [4])", "[2, 3, 4]"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[5:9]\nprint(n())", "[]"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nif n():\n    print(\"truthy\")", "truthy"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nprint(str(n()))", "[2, 3]"},
	{"def n(f):\n    if f:\n        return [9, 9]\n    xs = [1, 2, 3]\n    return xs[0:2]\nprint(n(False))\nprint(n(True))", "[1, 2]\n[9, 9]"},
	{"def n():\n    xs = [1.5, \"a\", 3]\n    return xs[0:2]\nprint(n() == [1.5, \"a\"])", "True"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nprint(n(), len(n()), n()[1], n() == [2, 3])", "[2, 3] 2 3 True"},
	// `bool(build())` is the condition door asked as a call, and the copy a returned slice is a container the
	// object — not the handle — answers. The empty copy is the row's real risk: `rt_slice`'s handle is never zero,
	// so the word under test made CPython's `False` into `True` at the exit code of a success, and the verdict now
	// comes from the same `rt_heap_len` the `if build():` road asks (**roadmap Gap R.238**, **ADR 0465**).
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nprint(bool(n()))", "True"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[5:9]\nprint(bool(n()))", "False"},
	{"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nprint(bool(n()), str(n()), n() + [4])", "True [2, 3] [2, 3, 4]"},
	// The bounds whose kind only the run time can name, and the tag is what answers them — **Gap R.237**'s half
	// of the door. A slot's tag says `None`, which is the side the program did not write (not a position of 0),
	// a call's answer carries its kind beside its payload, a bool is an int, and an arithmetic bound is the pair
	// the arithmetic road already lifts. Each row is the same shape as the raise rows below with the tag pointed
	// at a legal kind: the copy is what the reference copies, and the tag is what decided that.
	{"xs = [1, 2]\nys = [None]\nprint(xs[ys[0]:2])", "[1, 2]"},
	{"xs = [1, 2, 3]\nys = [None]\nprint(xs[0:ys[0]])", "[1, 2, 3]"},
	{"xs = [1, 2, 3]\nys = [None]\nprint(xs[::ys[0]])", "[1, 2, 3]"},
	{"xs = [1, 2, 3]\nys = [True]\nprint(xs[ys[0]:2])", "[2]"},
	{"xs = [1, 2, 3]\nys = [\"a\"]\nprint(xs[ys[0] == \"a\":2])", "[2]"},
	{"def g():\n    return None\n\nxs = [1, 2, 3]\nprint(xs[g():2])", "[1, 2]"},
	{"def g():\n    return 1\n\nxs = [1, 2, 3]\nprint(xs[0:g()])", "[1]"},
	{"xs = [1, 2, 3, 4]\nys = [1, 1, 1]\nprint(xs[ys[0] + ys[2]:4])", "[3, 4]"},
	{"def g():\n    return 1\n\nxs = [1, 2, 3, 4]\nprint(xs[g() + 1:4])", "[3, 4]"},
	{"xs = [1, 2, 3]\nprint(xs[0:3:len(xs)])", "[1]"},
	{"d = {\"k\": 2}\nxs = [1, 2, 3]\nprint(xs[0:d[\"k\"]])", "[1, 2]"},
	{"class Box:\n    def __init__(self):\n        self.k = 1\n\nb = Box()\nxs = [1, 2, 3]\nprint(xs[0:b.k])", "[1]"},
	{"def at(k):\n    xs = [1, 2, 3]\n    return xs[0:k]\n\nprint(at(2))", "[1, 2]"},
	{"def at(k):\n    return \"abcdef\"[k:4]\n\nprint(at(1))", "bcd"},
	{"def pick(who):\n    if who:\n        return 1\n    return \"x\"\n\nxs = [1, 2, 3]\nprint(xs[0:pick(True)])", "[1]"},
	{"s = \"abcdef\"\nys = [2]\nprint(s[ys[0]:4])", "cd"},
	// A bound that is a call runs BEFORE the bound is refused: the reference builds the slice from the values the
	// expressions answer, so the callee's own output is on stdout ahead of the complaint. This row is the answer
	// half of that order (the raise half is `TestABadBoundRunsTheCallItRefusesFirst`).
	{"def g():\n    print(\"side\")\n    return 1\n\nxs = [1, 2, 3]\nprint(xs[g():3])", "side\n[2, 3]"},
}

// TestABoundThatIsNotAPositionRaisesWhatTheReferenceRaises covers the bounds the compiler can name the kind of.
// The reference does not answer these and does not refuse them: it raises, the program's own `except` may take
// the raise, and exit 2 — the code the contract reserves for a compiler bug — is forbidden on every row
// (ADR 0166, ADR 0211). The list-bound row is the one that used to reach `rt_slice`'s i32 argument as a
// `@.lstN` global reference and have `llc` reject the module, exactly the shape Gap R.222 measured for an index.
func TestABoundThatIsNotAPositionRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	const message = "slice indices must be integers or None or have an __index__ method"
	for _, tt := range []struct {
		src string
	}{
		// A text bound: its word is an @str_tab index, and the copy ran from slot 0 to slot 1.
		{"xs = [1, 2]\nprint(xs[\"a\":\"b\"])"},
		{"print([1, 2][\"a\":\"b\"])"},
		{"xs = [1, 2]\nprint(xs[\"a\":None])"},
		{"xs = [1, 2]\nprint(xs[None:\"b\"])"},
		{"xs = [1.5, \"a\", [7]]\nprint(xs[\"a\":2])"},
		{"def b():\n    return [1, 2, 3]\nprint(b()[\"a\":2])"},
		{"xs = [1, 2, 3]\nprint(xs[0:2][\"a\":1])"},
		// A float bound.
		{"xs = [1, 2]\nprint(xs[1.5:2])"},
		{"xs = [1, 2]\nprint(xs[0:2.5])"},
		{"s = \"abcd\"\nprint(s[1.5:3])"},
		// A container bound: the word this road has no use for, and the one that took the module down.
		{"xs = [1, 2]\nprint(xs[[0]:2])"},
		{"xs = [1, 2]\nprint(xs[{\"a\": 1}:2])"},
		{"xs = [1, 2]\nprint(xs[{1, 2}:2])"},
		{"xs = [1, 2]\nprint(xs[(1,):2])"},
		// The step position asks the same question.
		{"xs = [1, 2]\nprint(xs[::\"a\"])"},
		{"xs = [1, 2]\nprint(xs[::1.5])"},
		// A name the records last filled with one of those kinds is that kind: the gate asks the same
		// notebooks the print door asks, and stays silent where none of them knows the name.
		{"xs = [1, 2]\ni = \"a\"\nprint(xs[i:2])"},
		{"xs = [1, 2]\nf = 1.5\nprint(xs[f:2])"},
		{"xs = [1, 2]\nzs = [\"a\"]\nprint(xs[zs:2])"},
		// A text base is sliced by counting characters, so the same bounds are positions there too.
		{"s = \"abcd\"\nprint(s[\"a\":\"b\"])"},
		{"s = \"abcd\"\nprint(s[\"a\":])"},
		// Every position that reads a copy asks it too — the print door, `len`, a loop, a condition,
		// equality, membership and a binding.
		{"xs = [1, 2]\nprint(len(xs[\"a\":2]))"},
		{"xs = [1, 2]\nfor v in xs[\"a\":2]:\n    print(v)"},
		{"xs = [1, 2]\nif xs[\"a\":2]:\n    print(\"yes\")"},
		{"xs = [1, 2]\nprint(xs[\"a\":2] == [1])"},
		{"xs = [1, 2]\nprint(1 in xs[\"a\":2])"},
		{"xs = [1, 2]\nys = xs[\"a\":2]\nprint(ys)"},
		// A parameter is its argument's kind, and a body that slices by it raises when handed a text.
		{"def f(a):\n    xs = [1, 2]\n    return xs[a:2]\nprint(f(\"a\"))"},
		// --- the kinds only the run time names (roadmap **Gap R.237**): the same sentence, reached through a
		// slot's tag, a call's answer word, a parameter the caller filled, and a class's field. None of these
		// shapes has a kind in the SOURCE for the static gate to read, so each was answered by the word the
		// pair road lifts — a text's `@str_tab` index or a float's box handle counted as a position, at the
		// exit code of a success, and the float spelling reached `rt_slice`'s i32 argument as a `double`.
		{"xs = [1, 2]\nys = [\"a\"]\nprint(xs[ys[0]:2])"},
		{"xs = [1, 2, 3]\nys = [\"a\"]\nprint(xs[1:ys[0]])"},
		{"xs = [1, 2, 3]\nys = [\"a\"]\nprint(xs[0:3:ys[0]])"},
		{"xs = [1, 2, 3]\nys = [\"a\"]\nprint(xs[-1:ys[0]:1])"},
		{"xs = [1, 2, 3]\nys = [1.5]\nprint(xs[ys[0]:2])"},
		{"xs = [1, 2]\nys = [[\"a\"]]\nprint(xs[ys[0][0]:2])"},
		{"d = {\"k\": \"v\"}\nxs = [1, 2]\nprint(xs[d[\"k\"]:2])"},
		{"t = (1, \"x\")\nxs = [1, 2]\nprint(xs[t[1]:2])"},
		{"def g():\n    return \"a\"\n\nxs = [1, 2]\nprint(xs[g():2])"},
		{"def g():\n    return \"a\"\n\nxs = [1, 2]\nprint(xs[0:g()])"},
		{"def g():\n    return 1.5\n\nxs = [1, 2]\nprint(xs[g():2])"},
		{"def g():\n    return 1.5\n\nxs = [1, 2]\nprint(xs[0::g()])"},
		{"def g():\n    return 1.5\n\ns = \"abcdef\"\nprint(s[0:g()])"},
		{"def g():\n    return {\"a\": 1}\n\nxs = [1, 2]\nprint(xs[g():2])"},
		{"def at(k):\n    xs = [1, 2, 3]\n    return xs[0:k]\nprint(at(1.5))"},
		{"def at(k):\n    xs = [1, 2, 3]\n    return xs[k:2]\nprint(at(\"1\"))"},
		{"def at(k):\n    return \"abcdef\"[k:4]\nprint(at(1.5))"},
		{"def pick(who):\n    if who:\n        return 1\n    return \"x\"\n\nprint(\"abcdef\"[0:pick(False)])"},
		{"s = \"abcdef\"\nys = [\"a\"]\nprint(s[ys[0]:3])"},
		{"s = \"abcdef\"\nys = [\"a\"]\nprint(s[1:ys[0]])"},
		{"class Box:\n    def __init__(self):\n        self.k = \"x\"\n\nb = Box()\nxs = [1, 2, 3]\nprint(xs[0:b.k])"},
		// The step position asks the run-time question too, and a `None` in a slot is NOT a step of zero: it is
		// the stride the program left out. The raise row is a slot holding a text where the stride belongs.
		{"xs = [1, 2, 3]\nys = [\"a\"]\nprint(xs[::ys[0]])"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			class, msg := jitTrap(t, tt.src)
			if class != "TypeError" || msg != message {
				t.Fatalf("%q raised %s: %s; the reference raises TypeError: %s", tt.src, class, msg, message)
			}
		})
	}
}

// TestASliceAnswersWhatTheReferenceAnswers covers the half that must not move: a position stays a position, an
// absence stays an absence. These are the rows a gate fails by refusing, so they are asked of the compiler and
// of the reference in the same breath.
func TestASliceAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBoundsAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference answers %q", tt.src, got, tt.want+"\n")
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, want %q", tt.src, got, tt.want+"\n")
			}
			out, trap := runForParity(t, tt.src)
			if trap != "" || out+"\n" != tt.want+"\n" {
				t.Fatalf("%q printed %q with trap %q, the reference prints %q", tt.src, out, trap, tt.want)
			}
		})
	}
}

// TestABadBoundIsTheProgramsExceptionNotTheCompilers: the raise is catchable, which is the difference between a
// program result and a build failure.
func TestABadBoundIsTheProgramsExceptionNotTheCompilers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want string
	}{
		{
			"xs = [1, 2]\ntry:\n    print(xs[\"a\":\"b\"])\nexcept TypeError:\n    print(\"caught\")",
			"caught",
		},
		{
			"s = \"abcd\"\ntry:\n    print(s[\"a\":])\nexcept TypeError:\n    print(\"caught\")",
			"caught",
		},
		{
			"xs = [1, 2]\ntry:\n    print(len(xs[[0]:2]))\nexcept TypeError:\n    print(\"caught\")",
			"caught",
		},
		{
			"def b():\n    return [1, 2, 3]\n\ntry:\n    print(b()[1.5:])\nexcept TypeError:\n    print(\"caught\")",
			"caught",
		},
		// The raise the run-time tag emits is on the same road as the one the static gate emits, so the
		// program's own handler takes it: the kind was only a run-time fact, but the exception is the program's.
		{
			"xs = [1, 2]\nys = [\"a\"]\ntry:\n    print(xs[ys[0]:2])\nexcept TypeError:\n    print(\"caught\")",
			"caught",
		},
		{
			"def g():\n    return \"a\"\n\nxs = [1, 2, 3]\ntry:\n    print(xs[g():2])\nexcept TypeError:\n    print(\"caught\")",
			"caught",
		},
		{
			"def at(k):\n    xs = [1, 2, 3]\n    return xs[0:k]\n\ntry:\n    print(at(1.5))\nexcept TypeError:\n    print(\"caught\")",
			"caught",
		},
		{
			"xs = [1, 2, 3]\nys = [\"a\"]\ntry:\n    print(xs[::ys[0]])\nexcept ValueError:\n    print(\"wrong class\")\nexcept TypeError:\n    print(\"caught\")",
			"caught",
		},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tc.src), "\n"); got != tc.want {
				t.Fatalf("%q printed %q, want the handler to have printed %q", tc.src, got, tc.want)
			}
		})
	}
}

// TestABadBoundRunsTheCallItRefusesFirst is the ORDER half of the door, and the half a kind check gets wrong by
// itself: the reference builds the slice object from the values the bound expressions ANSWER, and only then
// refuses the slice it built. A bound that is a call therefore runs — the callee's own `print` reaches stdout —
// before the complaint. Refusing the shape statically, which is what the gate does for a bound the program
// spelled (a literal, a slot, a container), skips those effects and prints less than the reference does; the
// rows below are the same programs as the raise rows above with an effect in the callee, and each asserts the
// stdout the reference writes AS WELL AS the class and the sentence (roadmap **Gap R.237**, **ADR 0336**'s order).
func TestABadBoundRunsTheCallItRefusesFirst(t *testing.T) {
	t.Parallel()
	const message = "slice indices must be integers or None or have an __index__ method"
	for _, tc := range []struct {
		src  string
		want string
	}{
		{
			"def g():\n    print(\"side\")\n    return \"x\"\n\nxs = [1, 2, 3]\nprint(xs[g():3])",
			"side",
		},
		{
			"def g():\n    print(\"side\")\n    return 1.5\n\nxs = [1, 2, 3]\nprint(xs[0:g()])",
			"side",
		},
		{
			"def shout(k):\n    print(\"bound\")\n    return k\n\nxs = [1, 2, 3]\nprint(xs[0:shout(\"a\")])",
			"bound",
		},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			out, trap := runForParity(t, tc.src)
			if trap != "TypeError: "+message {
				t.Fatalf("%q trapped %q; the reference traps TypeError: %s", tc.src, trap, message)
			}
			if out != tc.want {
				t.Fatalf("%q printed %q before the raise; the reference prints %q — the bound's own call was skipped", tc.src, out, tc.want)
			}
		})
	}
}

// TestTheFoldStillOwnsTheSliceThatFolds is the negative control: the gate stands in front of the object road,
// and the read the compiler can do at compile time still emits no object, no slice helper and no allocation —
// a row that starts allocating for `print("abcdef"[1:3])` is a row that asked the wrong question (ADR 0249's
// gate-and-door rule, asserted where a reader can see the IR).
func TestTheFoldStillOwnsTheSliceThatFolds(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print(\"abcdef\"[1:3])",
		"print(\"abcdef\"[::-1])",
		"print([1, 2, 3][1:])",
		"xs = [1, 2, 3]\nprint(xs[None:2])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			mustVerifyWithLLC(t, emittedModuleIR(t, src))
		})
	}
}

// TestTheBoundGateLeavesTheUntaggedCopyAlone: a program with a good bound keeps its plain word copy and its
// plain printer, and a program with a bad bound emits a raise rather than a call into the slice helper — the
// gate runs before any bound is lowered, which is also what keeps a container-valued bound out of an i32
// argument slot (the exit-2 shape this row retires).
func TestTheBoundGateLeavesTheUntaggedCopyAlone(t *testing.T) {
	t.Parallel()
	plain := mainBodyOf(emittedModuleIR(t, "xs = [1, 2, 3]\nprint(xs[1:])"))
	if !strings.Contains(plain, "@rt_slice(") || strings.Contains(plain, "rt_print_list_mixed") {
		t.Fatalf("a slice with a good bound changed road or printer:\n%s", plain)
	}
	bad := mainBodyOf(emittedModuleIR(t, "xs = [1, 2]\nprint(xs[\"a\":\"b\"])"))
	if strings.Contains(bad, "@rt_slice(") {
		t.Fatalf("a bad-bound slice still reaches the copy helper:\n%s", bad)
	}
	if !strings.Contains(bad, "@rt_die(") && !strings.Contains(bad, "exn_msg") {
		t.Fatalf("a bad-bound slice does not emit the program's raise:\n%s", bad)
	}
}

// TestTheTagQuestionIsInTheModuleWhereTheBoundIs asserts the run-time half as IR, so a door that starts trusting the
// payload again — or starts asking the tag of a bound whose kind the source already wrote — fails here and not only
// in a printed list. A guarded bound carries the helper call, the verdict test and a presence flag DERIVED from the
// verdict; a bound the source kinds carries none of it. That is the same rule **ADR 0337**'s zero-step guard
// follows: the guard lives where the value is, and a program that wrote a constant pays for nothing.
func TestTheTagQuestionIsInTheModuleWhereTheBoundIs(t *testing.T) {
	t.Parallel()
	// The presence flag is the fifth i32 of `rt_slice`, and for a bound whose tag decides it that argument is a
	// register and never the constant 1 — a compiler that writes `1` there is a compiler that read the payload
	// and never asked the tag, which is the whole of this row's defect.
	hasLowIsARegister := regexp.MustCompile(`@rt_slice\(i32 [^,]+, i32 [^,]+, i32 3, i32 1, i32 %`)
	slot := mainBodyOf(emittedModuleIR(t, "xs = [1, 2, 3]\nys = [1]\nprint(xs[ys[0]:3])"))
	if !strings.Contains(slot, "@rt_bound_is_position(") {
		t.Fatalf("a slot-read bound did not ask the tag:\n%s", slot)
	}
	if !strings.Contains(slot, "@rt_slice(") {
		t.Fatalf("a legal slot-read bound stopped building the copy:\n%s", slot)
	}
	if !hasLowIsARegister.MatchString(slot) || !strings.Contains(slot, "zext i1") {
		t.Fatalf("the copy's low bound is not the run-time verdict it was read from:\n%s", slot)
	}
	constant := mainBodyOf(emittedModuleIR(t, "xs = [1, 2, 3]\nk = 1\nprint(xs[0:k])"))
	if !strings.Contains(constant, "@rt_slice(") {
		t.Fatalf("the ordinary copy stopped being built by the ordinary road:\n%s", constant)
	}
	if strings.Contains(constant, "@rt_bound_is_position(") {
		t.Fatalf("a bound the source kinds was asked the run-time question anyway:\n%s", constant)
	}
	// A step the program left out is not a step the run time has to ask about either: `xs[ys[0]:]` has no stride
	// to kind, and a guard beside it would have **ADR 0337**'s zero-step question accusing a stride nobody wrote.
	open := mainBodyOf(emittedModuleIR(t, "xs = [1, 2, 3]\nys = [1]\nprint(xs[ys[0]:])"))
	if strings.Count(open, "@rt_bound_is_position(") > strings.Count(slot, "@rt_bound_is_position(") {
		t.Fatalf("an open bound asked more tag questions than a written one:\nopen:\n%s\nwritten:\n%s", open, slot)
	}
}

// TestEverySliceBoundsModuleVerifies is the exit-2 guard: the row began as three wrong answers and one `llc`
// rejection, and a fix that ends with the verifier unhappy has only changed class.
func TestEverySliceBoundsModuleVerifies(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBoundsAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			mustVerifyWithLLC(t, emittedModuleIR(t, tt.src))
		})
	}
}

// TestASliceStepOfZeroRaisesWhatTheReferenceRaises covers the one value a bound may not take. The kind gate above
// cannot ask it — `0` is a perfectly good integer — and `rt_slice` walks its own loop forever on it, so the run
// had to be killed and the contract filed the toolchain's own bug (exit 2, ADR 0166) for a program whose only
// fault was writing `::0`. The reference answers `ValueError: slice step cannot be zero` wherever the bounds say
// what the answer should be, and the row asserts the class, the message, the silence on stdout and the absence of
// the compiler's `codegen:` prefix on every position that reads a copy (roadmap Gap R.236, ADR 0337).
func TestASliceStepOfZeroRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	const message = "slice step cannot be zero"
	for _, tt := range []struct {
		src string
	}{
		// The shape the row was measured on, and the ones beside it: no bounds at all, both bounds, a
		// negative zero, a constant expression, a variable, a call's answer.
		{"xs = [1, 2]\nprint(xs[::0])"},
		{"print([1, 2, 3][::0])"},
		{"xs = [1, 2]\nprint(xs[0:2:0])"},
		{"xs = [1, 2]\nprint(xs[::-0])"},
		{"xs = [1, 2]\nprint(xs[::1 - 1])"},
		{"xs = [1, 2]\nk = 0\nprint(xs[::k])"},
		{"def z():\n    return 0\nxs = [1, 2]\nprint(xs[::z()])"},
		{"def b():\n    return [1, 2, 3]\nprint(b()[::0])"},
		{"xs = [1.5, \"a\", [7]]\nprint(xs[::0])"},
		// A text counts characters and strides the same way; the fold used to refuse it.
		{"s = \"abcd\"\nprint(s[::0])"},
		{"print(\"abcd\"[::0])"},
		{"print(\"abcd\"[0:4:0])"},
		// Every position that reads a copy asks it too.
		{"xs = [1, 2]\nprint(len(xs[::0]))"},
		{"xs = [1, 2]\nfor v in xs[::0]:\n    print(v)"},
		{"xs = [1, 2]\nif xs[::0]:\n    print(\"yes\")"},
		{"xs = [1, 2]\nprint(xs[::0] == [1])"},
		{"xs = [1, 2]\nprint(1 in xs[::0])"},
		{"xs = [1, 2]\nys = xs[::0]\nprint(ys)"},
		{"xs = [1, 2]\nprint(xs[::0][0:1])"},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			class, msg := jitTrap(t, tt.src)
			if class != "ValueError" || msg != message {
				t.Fatalf("%q raised %s: %s; the reference raises ValueError: %s", tt.src, class, msg, message)
			}
		})
	}
}

// TestASliceStepOfZeroIsTheProgramsExceptionNotTheCompilers: the raise is catchable, and the statement after the
// handler still runs — which is the difference between the program's ValueError and the killed run this row
// replaced (a killed run prints nothing, catches nothing, and exits 2).
func TestASliceStepOfZeroIsTheProgramsExceptionNotTheCompilers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want string
	}{
		{
			"xs = [1, 2]\ntry:\n    print(xs[::0])\nexcept ValueError:\n    print(\"caught\")",
			"caught",
		},
		{
			"xs = [1, 2]\nk = 0\ntry:\n    print(xs[::k])\nexcept ValueError:\n    print(\"caught\")\nprint(\"after\")",
			"caught\nafter",
		},
		{
			"s = \"abcd\"\ntry:\n    print(s[::0])\nexcept ValueError:\n    print(\"caught\")",
			"caught",
		},
		{
			"xs = [1, 2]\nprint(\"first\")\ntry:\n    print(xs[0:2:0])\nexcept ValueError:\n    print(\"caught\")\nprint(\"last\")",
			"first\ncaught\nlast",
		},
		{
			// A `TypeError` arm does not take a `ValueError`: the classes are the reference's, not ours to blur.
			"xs = [1, 2]\ntry:\n    print(xs[::0])\nexcept TypeError:\n    print(\"wrong arm\")\nexcept ValueError:\n    print(\"right arm\")",
			"right arm",
		},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tc.src), "\n"); got != tc.want {
				t.Fatalf("%q printed %q, want the handler to have printed %q", tc.src, got, tc.want)
			}
			if out, trap := runForParity(t, tc.src); trap != "" || out+"\n" != tc.want+"\n" {
				t.Fatalf("%q printed %q with trap %q, the reference prints %q", tc.src, out, trap, tc.want)
			}
		})
	}
}

// TestTheZeroStepGuardIsInTheModuleWhereTheValueIs is the row asked of the emitted IR: the three cases the fix
// distinguishes are three different modules, and each is checked for the shape rather than for the exit code.
//
//   - a step the compiler reads as zero raises and builds nothing — no `@rt_slice(` anywhere, the copy never exists;
//   - a step the compiler cannot read carries the guard beside the call, because the loop that hangs is the
//     helper's and the value only exists at run time;
//   - a step the compiler reads as anything else asks nothing: the ordinary `xs[::2]` module is no fatter than it
//     was, which is the negative control a guard emitted unconditionally would fail.
func TestTheZeroStepGuardIsInTheModuleWhereTheValueIs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name        string
		src         string
		want        []string
		forbidsCall bool
	}{
		{
			name:        "a known zero raises with no copy",
			src:         "xs = [1, 2]\nprint(xs[::0])",
			want:        []string{"store i32 1, i32* @exn_flag", "slice step cannot be zero"},
			forbidsCall: true,
		},
		{
			name:        "a step the compiler cannot read is guarded beside the copy",
			src:         "def z(n):\n    return n\nxs = [1, 2]\nprint(xs[::z(2)])",
			want:        []string{"= icmp eq i32", "store i32 1, i32* @exn_flag", "call i32 @rt_slice(", "slice step cannot be zero"},
			forbidsCall: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(res.IR, want) {
					t.Fatalf("the module does not carry %q:\n%s", want, res.IR)
				}
			}
			if tc.forbidsCall && strings.Contains(mainBodyOf(res.IR), "call i32 @rt_slice(") {
				t.Fatalf("the module builds a copy the program can never read:\n%s", res.IR)
			}
			mustVerifyWithLLC(t, res.IR)
		})
	}
	for _, src := range []string{
		"xs = [1, 2, 3, 4]\nprint(xs[::2])",
		"xs = [1, 2, 3, 4]\nprint(xs[::-1])",
		"xs = [1, 2, 3, 4]\nprint(xs[0:4:2])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			if !strings.Contains(res.IR, "call i32 @rt_slice(") {
				t.Fatalf("an answered slice no longer copies:\n%s", res.IR)
			}
			if strings.Contains(res.IR, "slice step cannot be zero") {
				t.Fatalf("a step the compiler reads still carries a raise site it can never reach:\n%s", res.IR)
			}
			mustVerifyWithLLC(t, res.IR)
		})
	}
}

// TestWhatTheBoundDoorStillCannotReachIsFiledNotFixed keeps the neighbours of this door visible, each with the
// reference's answer beside what this backend says today. A row that stops answering fails the test and moves;
// it does not disappear.
//
// The two wrongest rows — a bound that is a text hidden in a container slot or in a call's answer — are the
// same missing question with the tag, and they stay wrong until a road can read a bound's kind out of a pair
// (the run-time half of this row, roadmap Gap R.237), which is also why a float flowing into a parameter that
// slices by it is silent: a float parameter belongs to the pair road, so the body holds no static kind to ask,
// while the same body handed a **text** raises (that row is in the raise table above, and the difference is the
// record, not the syntax). Two further neighbours the row measured and filed rather than folded in: a
// comprehension whose iterable is a copy (Gap R.239 — name the copy and it answers) and the length of a slice of
// a text (Gap R.240). The step of zero used to be in this table as an unwitnessable row — it answered nothing, refused nothing,
// raised nothing: `rt_slice` looped and the run was killed (exit 2, the old **Gap R.236**) — and it left for the
// raise tables above when ADR 0337 put the value question beside the call. What the text road still cannot answer
// is its own row: a step that is a *value* (`s[::k]`) refuses with the fold's sentence where the reference strides
// the text, because `rt_str_slice` takes a low and a high and no stride at all (**Gap R.243**). The `except`
// arm that calls a function used to be pinned one altitude out, in `integration/slice_bounds_test.go`, where
// the run's exit code was the thing (**Gap R.241**); an accepting edge now clears the exception flag, so that
// row answers and moved into that file's answering table.
func TestWhatTheBoundDoorStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this backend answers today ("" = it refuses)
		oracle   string // what the reference answers
		gap      string
	}{
		// The two rows this table filed for **Gap R.237** — a text hidden in a slot, and the same text handed
		// back by a call — are DELETED by their own text. The bound door now asks the pair roads the question the
		// print, comparison, binding and membership doors already ask, and reads the tag beside the payload:
		// `rt_bound_is_position` holds the legal-tag set for every bound of both slice roads, and a tag that says
		// `None` is the side the program left out rather than a position of 0. They are witnessed as raise rows
		// and as answered rows in this file (**roadmap Gap R.237**, **ADR 0336**'s order, one construct on).
		// The two shapes this table filed for **Gap R.234** — a slice of a set literal and a slice of a dict kept
		// in a name — are DELETED by their own text. The base question is now asked BEFORE the bounds question,
		// which is the reference's order: a set complains about the object (`TypeError: 'set' object is not
		// subscriptable`) and a dict hashes the slice and names the key it missed
		// (`KeyError: slice(0, 1, None)`), so neither shape reaches the bounds sentence at all. They are witnessed
		// in `pkg/lang/slice_base_kind_test.go`, including the ORDER itself: a set sliced by a float or by a list
		// still says `'set'`, never the slice-indices words this file owns (**roadmap Gap R.234**, **ADR 0466**).
		// The two shapes this table used to file — a slot of a copy read directly, and a copy of a slot that
		// is itself a list — left with **ADR 0459**: the door that names what the run time built answers them,
		// and they are witnessed as answered rows in `pkg/lang/slice_answer_test.go` rather than here.
		//
		// The five rows that stood here — a `return` of a slice printed the copy's HANDLE as a number (`1`, and `3`
		// over a mixed base), and the same call's answer looped, measured or compared refused or walked one member —
		// are DELETED by their own text: `containerKindOfIn` gained the `*Slice` arm and
		// `containerAnswerSlotReader.slot` asks the copy's base, so all five answer the reference now (**roadmap
		// Gap R.238**, **ADR 0465**). They live in `sliceBoundsAnswered` beside the rest of the door, where every
		// answered row is asked of the compiler, the reference, the module verifier AND the record.
		// The row this table filed for a float handed to a parameter that slices by it is DELETED by the same
		// text: a parameter is the binding that is never in the body, and the caller's kind now travels with the
		// pair the callee was handed (`def at(k): return xs[0:k]` / `print(at(1.5))` raises, the reference raises).
		{
			"a comprehension over a copy",
			"xs = [1, 2, 3]\nprint([x * 2 for x in xs[0:2]])",
			// Was a refusal (`the iterable door builds a comprehension over a literal, range() or a variable
			// it knows the kind of`) and is an answer: the refusal was emitted because the escape analysis
			// never counted a comprehension's ITERABLE as a read, so `xs`'s binding was deleted and the
			// copy had no object to walk (ADR 0356, roadmap Gap R.239 for the sibling that still refuses).
			"[2, 4]", "[2, 4]",
			"roadmap Gap R.239",
		},
		// The length of a slice of a text — `s = "abcd"` / `print(len(s[1:3]))`, the row's own definition of
		// done — is DELETED by its own text: the length door now walks this file's own slice road (the extracted
		// `valueTextSlice`) and measures the text it selects through `rt_str_nchars`, so the answer is `2` on the
		// compiled leg, the JIT and the record, and the four raises this door now INHERITS from the bounds and
		// step gates and the base gate are witnessed beside it. It lives in `pkg/lang/text_slice_len_test.go`
		// (**roadmap Gap R.240**, **ADR 0470**), and the shape still refused here — a step that is a VALUE — is
		// filed as **Gap R.243** with the road's own sentence, not this file's container one.
		{
			"a stepped slice of a text whose step is a value",
			"s = \"abcd\"\nk = 2\nprint(s[::k])",
			"", "ac",
			"roadmap Gap R.243 (`rt_str_slice` takes a low and a high and no stride, so the fold answers a constant step and refuses any other; `s[i:i+2]` — the same road without a step — has answered since ADR 0229)",
		},
		// The two neighbours **Gap R.238** measured on the way and left standing, each with the answer this
		// backend gives today beside the reference's, so the day a door opens the row fails rather than rots.
		{
			"a copy handed back, concatenated in a statement that reads the call again",
			"def n():\n    xs = [1, 2, 3]\n    return xs[1:3]\nprint(n() + [4], bool(n()), str(n()))",
			"[2, 3] True [2, 3]", "[2, 3, 4] True [2, 3]",
			"roadmap Gap R.551 (one call read at two positions in one statement: the `+` door is handed the operand the OTHER read materialised, so the concatenation is lost — and the same bytes come out of this row's parent for a body that FORWARDS a literal answer, `def b():` / `    return b2()`, which is what makes this a neighbour and not this row's defect)",
		},
		{
			"a slice of a PARAMETER handed back",
			"def n(xs):\n    return xs[0:2]\nprint(n([1, 2, 3]))",
			"1", "[1, 2]",
			"roadmap Gap R.552 (the scan asks what the BODY bound a name to, and a parameter's object is the CALLER'S — the record the copy's kind needs is not in this frame: Gap R.154's pair-parameter question one construct out, and the byte-identical answer on this row's parent)",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, trap, refused := sliceFiledRow(t, tc.src)
			if tc.compiled == "" {
				if !refused {
					t.Fatalf("%q answered %q (trap %q) where the row records a refusal — the door grew, so move the row into the tables above (reference: %q)", tc.src, out, trap, tc.oracle)
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

// TestTheBoundsRowsAreRecorded is the record leg: every answered row has an answer on record, so the door
// cannot be deleted without deleting the evidence the suite reads (ADR 0302's rule that a missing record fails
// the case).
func TestTheBoundsRowsAreRecorded(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceBoundsAnswered {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.Repr == "" && rec.Stdout == "" && rec.Err == "" {
				t.Fatalf("%q has no recorded answer: re-record it before shipping the door that reads it", tt.src)
			}
		})
	}
}
