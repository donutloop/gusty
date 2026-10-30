package lang

import (
	"strings"
	"testing"
)

// container_subscript_test.go — a subscript of a container handed back by a call is read by the door that
// reads a container variable's slot (roadmap Gap R.214, ADR 0334).
//
// ADR 0333 gave the answer; this row is the door one position further out, and it is the row ADR 0326
// named and left open:
//
//	def build():
//	    return [1, 2]
//	print(build()[7])       # CPython: IndexError: list index out of range
//	                        # this toolchain: exit 1, "index requires an inline list/dict/set literal"
//	print(build()[0])       # CPython: 1
//	                        # this toolchain: the same refusal
//
// while the same element read off a name — `ys = build(); print(ys[7])` — had raised the catchable
// `IndexError` since ADR 0326, and `print([1, 2][7])` had raised it in the fold since the same ADR. One
// question, three doors, and the door that was missing chose its road from the base's **spelling** (a call
// is not a literal) rather than from what the compiler holds once the answer is lowered — the same
// misattribution ADR 0251 fixed for a container the program built, one construct over.
//
// The fix is a rewrite, not a second reader: the answer is lowered to the rooted name ADR 0333 records its
// notebooks under, the base of a copied node is set to that name, and the roads that have always answered
// `ys[i]` — the kind roads, the mixed reads, the tagged read, the bounds check — answer it. The order is
// asserted, because it is load-bearing: the kind roads run first, since a dict built from a literal carries
// no per-slot tags and the tagged read handed back a text value's @str_tab index and called it a number.
//
// Every `want` below is the reference's, read off `python3`; `integration/container_subscript_test.go` runs
// the same programs through the CLI and derives its expectations from the interpreter, not from this file.

// containerSubscriptPrograms are the answered shapes. They are the parity this row exists for: each is the
// same read the bound form has always answered, written one statement shorter.
var containerSubscriptPrograms = []struct {
	src  string
	want string
}{
	{"def b():\n    return [1, 2]\n\nprint(b()[0])", "1"},
	{"def b():\n    return [1, 2]\n\nprint(b()[1])", "2"},
	{"def b():\n    return [1, 2]\n\nprint(b()[-1])", "2"},
	{"def b():\n    return [1, 2]\n\nprint(b()[-2])", "1"},
	{"def f():\n    return {\"a\": 1, \"b\": 2}\n\nprint(f()[\"a\"])", "1"},
	{"def f():\n    return {\"a\": 1, \"b\": 2}\n\nprint(f()[\"b\"])", "2"},
	{"def b():\n    return [1, \"a\", None, [4]]\n\nprint(b()[1], b()[2], b()[3][0])", "a None 4"},
	{"def b():\n    return [1.5, 2]\n\nprint(b()[0])", "1.5"},
	{"def f():\n    return {1: \"one\", 2: \"two\"}\n\nprint(f()[2])", "two"},
	{"def b():\n    return [[1, 2], [3]]\n\nprint(b()[0][1])", "2"},
	{"def b():\n    return [{\"a\": \"x\"}]\n\nprint(b()[0][\"a\"])", "x"},
	{"def b():\n    return {\"a\": [1, 2]}\n\nprint(b()[\"a\"][1])", "2"},
	{"def b():\n    return [{\"a\": {\"b\": 1}}]\n\nprint(b()[0][\"a\"][\"b\"])", "1"},
	{"def b():\n    return [1, 2]\n\ni = 1\nprint(b()[i])", "2"},
	{"def b():\n    return [10, 20, 30]\n\nfor i in range(3):\n    print(b()[i])", "10\n20\n30"},
	{"def b():\n    xs = []\n    xs.append(\"q\")\n    return xs\n\nprint(b()[0])", "q"},
	{"def b():\n    return [x * 2 for x in [1, 2]]\n\nprint(b()[1])", "4"},
	{"def b():\n    return [1, 2]\n\ndef g():\n    return b()[1]\n\nprint(g())", "2"},
	{"def b():\n    return [1, 2]\n\nxs = b()[0]\nprint(xs)", "1"},
	{"def b():\n    return [1, 2]\n\nprint(b()[0] + 1)", "2"},
	{"def b():\n    return [1, 2]\n\nif b()[0] > 1:\n    print(\"big\")\nelse:\n    print(\"small\")", "small"},
	{"def b():\n    return [1, 2]\n\nprint(b()[0], b()[1])", "1 2"},
	{"def b():\n    return [1, \"a\"]\n\nprint(str(b()[1]))", "a"},
	{"def b():\n    return [1, 2]\n\nprint(b()[0] in [1, 9])", "True"},
	{"def b():\n    return {\"k\": [7, 8]}\n\nprint(len(b()[\"k\"]))", "2"},
	{"def b():\n    return {\"a\": [1, 2]}\n\nprint(2 in b()[\"a\"])", "True"},
	{"def b():\n    return [1, 2]\n\nys = b()[0] + b()[1]\nprint(ys)", "3"},
	{"def f():\n    return {\"a\": {\"b\": [3]}}\n\nprint(f()[\"a\"][\"b\"][0])", "3"},
	{"def b():\n    return [[\"a\", \"b\"], [\"c\"]]\n\nprint(b()[0][1], b()[1][0])", "b c"},
	// An answer whose slots are all text is the shape the notebook clearing decides: the object is tagged,
	// so the notebooks have to say nothing about a single kind, or the reader asks the untagged word road —
	// which has no tag to print — and refuses with Gap R.82's sentence while `print(ys[1])` prints the text.
	{"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[1])", "ho"},
	{"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[0], words()[1])", "hi ho"},
	// The two rows beside that one are the guard on the clearing itself: the answer is built TAGGED, so its
	// element notebooks say nothing, and the roads that still need to answer a tagged container are the whole
	// printer (which asks `mixedLists` first) and the loop (which prints each element from the slot). If the
	// clearing ever runs on an UNtagged answer, these two print interned indices instead of texts.
	{"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words())", "['hi', 'ho']"},
	{"def words():\n    return [\"hi\", \"ho\"]\n\nfor x in words():\n    print(x)", "hi\nho"},
	{"def m():\n    return {\"a\": \"x\", \"b\": \"y\"}\n\nprint(m()[\"b\"])", "y"},
	{"def b():\n    return [1.5, \"a\", [7]]\n\nprint(b()[2][0], b()[1])", "7 a"},
	// A key that IS there is read, whatever the kind of it: this is the row that would start raising
	// KeyError if the hashing door confused a key with a position (Gap R.231's neighbour).
	{"def z():\n    return {0: \"zero\"}\n\nprint(z()[0])", "zero"},
}

// TestASubscriptOnACallResultAnswersTheElement is the row's own claim: the element a call hands back is
// read, in every container kind and every kind of slot, by the door that reads a variable's slot.
func TestASubscriptOnACallResultAnswersTheElement(t *testing.T) {
	t.Parallel()
	for _, tt := range containerSubscriptPrograms {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tt.src), "\n"); got != tt.want {
				t.Fatalf("%q printed %q, want %q (the reference's answer, read off python3)", tt.src, got, tt.want)
			}
		})
	}
}

// TestASubscriptPastTheEndOfACallResultRaisesWhatTheReferenceRaises covers the positions the fold cannot
// count. The reference does not refuse these and does not answer them: it raises, and the program's own
// `except` is allowed to take the raise. Exit 2 — the code the contract reserves for a compiler bug — is
// forbidden on every row (ADR 0166, ADR 0211).
func TestASubscriptPastTheEndOfACallResultRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src     string
		class   string
		message string
	}{
		{
			"def b():\n    return [1, 2]\n\nprint(b()[7])",
			"IndexError", "list index out of range",
		},
		{
			"def b():\n    return [1, 2]\n\nprint(b()[-9])",
			"IndexError", "list index out of range",
		},
		{
			"def f():\n    return {\"a\": 1}\n\nprint(f()[\"zz\"])",
			"KeyError", "'zz'",
		},
		{
			"def f():\n    return {1: 2}\n\nprint(f()[0])",
			"KeyError", "0",
		},
		{
			// Gap R.231, PAID by ADR 0419. A number subscripting a TEXT-keyed dict used to read the
			// POSITION and answer `1 2` at exit 0; the read road now asks the key's kind, which is the
			// question the hashing door has always asked, and the answer is the reference's miss.
			"d = {\"a\": 1, \"b\": 2}\nprint(d[0], d[1])",
			"KeyError", "0",
		},
		{
			"def f():\n    return {\"a\": 1, \"b\": 2}\n\nprint(f()[0])",
			"KeyError", "0",
		},
		{
			// The twin that must keep answering, so the two halves of the key question cannot drift: a
			// number key read by that number is a hit.
			"d = {0: \"zero\"}\nprint(d[0])",
			"HIT:zero", "",
		},
		{
			"def b():\n    return [1, 2]\n\nprint(b()[\"a\"])",
			"TypeError", "list indices must be integers or slices, not str",
		},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if strings.HasPrefix(tt.class, "HIT:") {
				// A hit row standing in a raise table: the same road's other answer, kept beside the miss.
				want := strings.TrimPrefix(tt.class, "HIT:")
				res, err := JIT(tt.src, 0)
				if err != nil {
					t.Fatalf("%q was refused: %v", tt.src, err)
				}
				if res.Output != want+"\n" {
					t.Fatalf("%q printed %q, want %q", tt.src, res.Output, want+"\n")
				}
				return
			}
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("%q raised %s: %s; the reference raises %s: %s", tt.src, class, message, tt.class, tt.message)
			}
		})
	}
}

// TestATrapInsideACallResultSubscriptIsCatchable is the same raise asked of the program: a trap that ends
// the run is a crash, and a trap the program can take is a value the language has.
func TestATrapInsideACallResultSubscriptIsCatchable(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src  string
		want string
	}{
		{"def b():\n    return [1, 2]\n\ntry:\n    print(b()[9])\nexcept IndexError:\n    print(\"caught\")", "caught"},
		{"def f():\n    return {\"a\": 1}\n\ntry:\n    print(f()[\"b\"])\nexcept KeyError:\n    print(\"missing\")", "missing"},
		{
			// Gap R.231 / ADR 0419: the miss is a trap the program can TAKE, not a value it was handed.
			"d = {\"a\": 1}\ntry:\n    print(d[0])\nexcept KeyError as e:\n    print(\"caught\", e)", "caught 0"},
	} {
		t.Run(strings.ReplaceAll(tc.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimRight(compiledOut(t, tc.src), "\n"); got != tc.want {
				t.Fatalf("%q printed %q, want the handler to have printed %q", tc.src, got, tc.want)
			}
		})
	}
}

// runForParity is the one runner the parity rows can use: an answer and a raise are both program results,
// and a comparison that can only speak about answers would call `build()[7]` and `ys[7]` different because
// both raise. `ParseTrapReport` is the host's half of reading the runtime's own report back, so what comes
// out here is the program's exception rather than the compiler's opinion of it (ADR 0214).
func runForParity(t *testing.T, src string) (stdout, trap string) {
	t.Helper()
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("%q: jit: %v", src, err)
	}
	if res.Code == 2 {
		t.Fatalf("%q failed as a toolchain rejection (exit 2):\n%s", src, res.Stderr)
	}
	if _, class, message := ParseTrapReport(res.Stderr); class != "" {
		return strings.TrimSpace(res.Output), class + ": " + message
	}
	return strings.TrimSpace(res.Output), ""
}

// TestTheCallResultReadIsTheBoundFormsRead is the parity that makes this a defect rather than a missing
// feature. `ys = build()` has answered `ys[i]` for as long as ADR 0333 has existed; the row is that the
// same program written one statement shorter answers the same thing.
func TestTheCallResultReadIsTheBoundFormsRead(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		direct string
		bound  string
	}{
		{
			"def b():\n    return [1, \"a\", None, [4]]\n\nprint(b()[1], b()[2], b()[3][0])",
			"def b():\n    return [1, \"a\", None, [4]]\n\nys = b()\nprint(ys[1], ys[2], ys[3][0])",
		},
		{
			"def f():\n    return {1: \"one\", 2: \"two\"}\n\nprint(f()[2])",
			"def f():\n    return {1: \"one\", 2: \"two\"}\n\nd = f()\nprint(d[2])",
		},
		{
			"def b():\n    return [1.5, 2]\n\nprint(b()[0])",
			"def b():\n    return [1.5, 2]\n\nys = b()\nprint(ys[0])",
		},
		{
			"def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[1])",
			"def words():\n    return [\"hi\", \"ho\"]\n\nys = words()\nprint(ys[1])",
		},
		{
			"def b():\n    return [1, 2]\n\nprint(b()[7])",
			"def b():\n    return [1, 2]\n\nys = b()\nprint(ys[7])",
		},
		{
			"def f():\n    return {\"a\": 1}\n\nprint(f()[\"b\"])",
			"def f():\n    return {\"a\": 1}\n\nd = f()\nprint(d[\"b\"])",
		},
	} {
		t.Run(strings.ReplaceAll(tc.direct, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			directOut, directTrap := runForParity(t, tc.direct)
			boundOut, boundTrap := runForParity(t, tc.bound)
			if directTrap != boundTrap || directOut != boundOut {
				t.Fatalf("one read, two answers: %q gave (%q, %q), %q gave (%q, %q)",
					tc.direct, directOut, directTrap, tc.bound, boundOut, boundTrap)
			}
			if directTrap != "" && directOut != "" {
				t.Fatalf("%q wrote %q before the raise it shares with %q; the reference writes nothing",
					tc.direct, directOut, tc.bound)
			}
		})
	}
}

// TestTheFoldStillOwnsTheLiteralRead is the negative control: the door this row grew runs where the
// compiler cannot count the answer's slots, and the compile-time lookup keeps every shape it can read.
// A row that starts allocating is a row that got the gate wrong.
func TestTheFoldStillOwnsTheLiteralRead(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"print([1, 2][0])",
		"print({1: 2}[1])",
		"print([1, 2][0] + 1)",
	} {
		t.Run(src, func(t *testing.T) {
			t.Parallel()
			main := mainBodyOf(emittedModuleIR(t, src))
			for _, banned := range []string{"@rt_alloc", "@rt_get_elem", "@rt_dict_get_tagged", "@rt_list_from"} {
				if strings.Contains(main, banned) {
					t.Fatalf("%q now asks %s: the fold answered this read before this row and still must", src, banned)
				}
			}
		})
	}
}

// TestEveryCallResultSubscriptModuleVerifies is the exit-2 guard: the row began as a refusal, and a row that
// ends as an `llc` rejection has only changed class, not been fixed.
func TestEveryCallResultSubscriptModuleVerifies(t *testing.T) {
	t.Parallel()
	for _, tt := range containerSubscriptPrograms {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			mustVerifyWithLLC(t, emittedModuleIR(t, tt.src))
		})
	}
}

// TestWhatTheCallResultSubscriptStillCannotReachIsFiledNotFixed keeps the neighbours of this door visible,
// each with the reference's answer beside what this backend says today — the discipline ADR 0331, ADR 0332
// and ADR 0333 each shipped with. A row that stops refusing moves; it does not disappear.
func TestWhatTheCallResultSubscriptStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		src      string
		compiled string // what this backend answers today ("" = it refuses)
		oracle   string // what the reference answers
		gap      string
	}{
		{
			"an answer that is a set, subscripted",
			"def s():\n    return {1, 2}\n\nprint(s()[0])",
			"", "TypeError: 'set' object is not subscriptable",
			"roadmap Gap R.217 (a set has no slots, and the raise this shape owes is that door's, not this one's)",
		},
		{
			"an answer that is None, subscripted",
			"def n():\n    return None\n\nprint(n()[0])",
			"", "TypeError: 'NoneType' object is not subscriptable",
			"roadmap Gap R.228 (a function that hands back None answers the digit 0; the return kind the subscript needs is the same missing record)",
		},
		{
			"an answer that is a number, subscripted",
			"def n():\n    return 5\n\nprint(n()[0])",
			"", "TypeError: 'int' object is not subscriptable",
			"roadmap Gap R.227 (a subscript of something the compiler holds as a number, at a call rather than at a literal)",
		},
		{
			"an answer's slot written to",
			"def b():\n    return [1, 2]\n\nb()[0] = 9\nprint(1)",
			"", "1",
			"roadmap Gap R.229 (the item-assignment road takes a variable on the left; the answer is an object with no variable named for it)",
		},
		{
			"a slice of an answer measured by the row that opened it",
			"def b():\n    return [1, 2, 3]\n\nprint(len(b()[0:2]))",
			"2", "2",
			"roadmap Gap R.33 (sequence operations: the length road took the copy this row's door builds; the row stays open for the shapes it still declines)",
		},
		{
			"two text slots joined",
			"def b():\n    return [\"x\", \"y\"]\n\nprint(b()[0] + b()[1])",
			"", "xy",
			"roadmap Gap R.82 (a text slot used arithmetically: no road builds a joined text out of two slots)",
		},
		{
			"a number read out of a slot of an answer",
			"def mk():\n    return [{\"k\": 1}]\n\nprint(mk()[0][\"k\"] + 1)",
			"", "2",
			"roadmap Gap R.82 (the numeric use of a slot: the pair travels with the value, but this context wants one static kind)",
		},
		// The slice neighbours of this door — `print(b()[0:2])`, `ys = b()[0:2]`, `len(b()[0:2])` — were filed
		// here as refusals and are answered by ADR 0335; they are witnessed in `slice_answer_test.go`, where a
		// row that stops answering fails rather than quietly disappearing.
		{
			"two float slots added",
			"def mk():\n    return [1.5, 2.5]\n\nprint(mk()[0] + mk()[1])",
			"", "4.0",
			"roadmap Gap R.82 (the pair road's own sentence, kept: this row did not widen arithmetic over slots)",
		},
		// The row this table carried for **Gap R.232** — `def words(): return ["hi", "ho"]` /
		// `print(words()[0] in words())`, recorded as refusing while the reference printed `True` — is
		// DELETED, not edited: the needle now arrives as the (payload, tag) pair the equality door already
		// computes, and the rows live in `pkg/lang/membership_needle_pair_test.go` and
		// `integration/membership_needle_pair_test.go`, where a shape that stops answering fails rather than
		// quietly disappearing. The needle's base being a call's container answer is the same door as the
		// subscript beside it (**ADR 0334**), which is what makes the two spellings one program (**ADR 0474**).
		//
		// A neighbour measured while writing this row, wrong for EVERY base — a literal, a variable
		// or a call's answer alike — so the subscript road inherits the wrongness rather than causing it.
		// The `a text-keyed dict subscripted by a number` row that used to sit here is DELETED, not edited:
		// it answers the reference now. Its rows live in
		// `TestASubscriptPastTheEndOfACallResultRaisesWhatTheReferenceRaises` (the miss, the call-result
		// twin and the number-key hit) and `TestATrapInsideACallResultSubscriptIsCatchable` (the same miss
		// taken by the program) — the row that owned it, roadmap Gap R.231, is closed.
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

// The record leg: every answered row has an answer on record, so the door cannot be deleted without
// deleting the evidence the suite reads (ADR 0302's rule that a missing record fails the case).
func TestTheCallResultSubscriptRowsAreRecorded(t *testing.T) {
	t.Parallel()
	for _, tt := range containerSubscriptPrograms {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.Repr == "" && rec.Stdout == "" && rec.Err == "" {
				t.Fatalf("%q has no recorded answer: re-record it before shipping the door that reads it", tt.src)
			}
		})
	}
}
