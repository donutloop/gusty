package lang

import (
	"regexp"
	"strings"
	"testing"
)

// runtime_slot_read_test.go — the nested read of a container the program **built** rather than
// spelled out (roadmap L11.1, ADR 0251).
//
// ADR 0241 granted the read `xs[0][1]` on a compile-time promise: the name was bound once to a
// container literal, nothing touched the object since, so the tag the literal's builder wrote is the
// tag the slot holds. ADR 0246 extended the same trust to `len(xs[0])` of a container the program grew
// with `append`. What neither covered was the read *one level below* such a container:
//
//	xs = []
//	xs.append([7, 8])
//	print(xs[0][0])          # 7 — refused: "index cannot reach into xs's slots"
//
// The refusal was honest and the program was still unanswered: CPython prints 7, the record
// prints 7, and the compiled leg exited 1 on the shape the roadmap names as its own remaining clause.
// The door this file pins asks the object instead of the notebook: the tag written beside the outer
// slot says which kind of object the payload names, and the tag written beside the inner slot says
// what the answer means. A list slot is read by position (bounds check and all), a dict slot by its
// (payload, tag) key, a text slot gives one character, and a slot that holds none of those raises the
// sentence CPython raises, per kind — because a payload read as a handle is a wrong answer wearing
// another object's bits.
//
// Every row below is checked on both legs: the compiled backend judged against the retired
// engine's record (`evalGolden`/`runGoldenStdout`, ADR 0302), and — in the sibling integration file —
// against CPython itself.

func TestRunTimeBuiltNestedSlotReadsAnswerOnBothLegs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		// ---- the shape that started the cycle: a list built by append, read one level down.
		{"first slot of an appended list", "xs = []\nxs.append([7, 8])\nprint(xs[0][0])\n", "7\n"},
		{"second slot of an appended list", "xs = []\nxs.append([7, 8])\nprint(xs[0][1])\n", "8\n"},
		{"second appended list", "xs = []\nxs.append([7, 8])\nxs.append([9, 10])\nprint(xs[1][0], xs[1][1])\n", "9 10\n"},
		{"negative position below the slot", "xs = []\nxs.append([7, 8])\nprint(xs[0][-1])\n", "8\n"},
		{"index the program computes", "xs = []\nxs.append([7, 8])\ni = 1\nprint(xs[0][i])\n", "8\n"},
		// ---- the same question of a dict the program filled.
		{"list under a dict key", "d = {}\nd[\"a\"] = [1, 2]\nd[\"b\"] = [3, 4]\nprint(d[\"a\"][1], d[\"b\"][0])\n", "2 3\n"},
		{"computed key under a computed key", "d = {}\nk = \"a\"\nd[k] = [1, 2]\nprint(d[k][0])\n", "1\n"},
		{"int key of a run-time dict", "d = {}\nd[7] = [\"x\", \"y\"]\nprint(d[7][1])\n", "y\n"},
		// ---- the tag, not the spelling, decides which object is asked: one container, three kinds of
		// slot. This is what no compile-time table could have answered, and what a single static kind
		// would have got wrong.
		{"a list slot and a dict slot in one container", "xs = []\nxs.append([1, 2])\nxs.append({\"k\": 5})\nprint(xs[0][1], xs[1][\"k\"])\n", "2 5\n"},
		{"dict built under a list slot", "xs = []\nxs.append({\"k\": [1, 2]})\nprint(xs[0][\"k\"][1])\n", "2\n"},
		{"three levels under an appended list", "xs = []\nxs.append([[1, 2], [3, 4]])\nprint(xs[0][1][0])\n", "3\n"},
		{"text slot read as a character", "xs = []\nxs.append(\"abc\")\nprint(xs[0][1])\n", "b\n"},
		{"text under a dict key", "d = {}\nd[\"a\"] = \"abc\"\nprint(d[\"a\"][2])\n", "c\n"},
		{"float slot", "xs = []\nxs.append([1.5, 2])\nprint(xs[0][0])\n", "1.5\n"},
		{"None slot", "xs = []\nxs.append([None, 2])\nprint(xs[0][0])\n", "None\n"},
		{
			// A set in a slot is asked the membership question, which is what a set subscript means in
			// this language at all (docs/language.md § Dicts & sets — a documented gusty extension,
			// `oracle: not_applicable` in the ledger because CPython rejects the shape). Reading it one
			// level down is the same rule, not a new one: the tag picks the arm, the arm asks the set.
			"set slot asked as a member", "xs = []\nxs.append({5, 6, 7})\nprint(xs[0][6])\n", "6\n"},
		{"set slot under a dict key", "d = {}\nd[\"a\"] = {5, 6, 7}\nprint(d[\"a\"][7])\n", "7\n"},
		// ---- the uses a pair is worth: a binding, an equality, a length, a print.
		{"bound and printed", "xs = []\nxs.append([7, 8])\ny = xs[0][1]\nprint(y)\n", "8\n"},
		{"bound from a dict slot and printed", "d = {}\nd[\"a\"] = [7, 8]\ny = d[\"a\"][0]\nprint(y)\n", "7\n"},
		{"compared with a number", "xs = []\nxs.append([7, 8])\nprint(1 if xs[0][1] == 8 else 0)\n", "1\n"},
		{"compared with the wrong number", "xs = []\nxs.append([7, 8])\nprint(1 if xs[0][1] == 7 else 0)\n", "0\n"},
		{"compared across the intern table", "xs = []\nxs.append([\"a\", 1])\nprint(1 if xs[0][0] == 1 else 0)\n", "0\n"},
		{"compared with its own text", "xs = []\nxs.append([\"a\", 1])\nprint(1 if xs[0][0] == \"a\" else 0)\n", "1\n"},
		{"float slot compared with its number", "xs = []\nxs.append([1.5])\nprint(1 if xs[0][0] == 1.5 else 0)\n", "1\n"},
		{"printed as itself", "xs = []\nxs.append([7, 8])\nprint(xs[0])\n", "[7, 8]\n"},
		{"length of the slot below", "xs = []\nxs.append([[1, 2]])\nprint(len(xs[0][0]))\n", "2\n"},
		{"length of a text slot below", "xs = []\nxs.append([\"abc\"])\nprint(len(xs[0][0]))\n", "3\n"},
		// ---- a comprehension is a container the program built too: the element is written by the same
		// payload-and-tag door an `append` uses (ADR 0244), so the read below it is the same question.
		{"slot of a comprehension-built dict", "d = [{\"k\": x} for x in [1, 2]]\nprint(d[1][\"k\"])\n", "2\n"},
		{"slot of a comprehension-built list", "xs = [[x, x * 2] for x in [1, 2]]\nprint(xs[1][1])\n", "4\n"},
		// Shapes that already worked, pinned so the new door cannot take them away: the static door
		// still owns a container the literal describes.
		{"literal-built container still reads", "xs = [[1, 2], [3, 4]]\nprint(xs[0][1])\n", "2\n"},
		{"literal dict still reads", "d = {\"a\": [1, 2]}\nprint(d[\"a\"][1])\n", "2\n"},
		{"three levels of a literal", "t = [[[1]]]\nprint(t[0][0][0])\n", "1\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("%s: interpreter printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
		})
	}
}

// TestRunTimeBuiltNestedSlotReadsTrapLikeCPython pins the failures. A slot that holds a number, None
// or a set has nothing below it, and a position can be out of range; CPython raises, the record
// raises, and the compiled arm the tag dispatches to raises the same sentence — it may not refuse
// these, because refusing is what the row below (the numeric use) still legitimately does.
func TestRunTimeBuiltNestedSlotReadsTrapLikeCPython(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, class, message string }{
		{"number slot subscripted", "xs = []\nxs.append([7, 8])\nxs.append(5)\nprint(xs[1][0])\n",
			"TypeError", "'int' object is not subscriptable"},
		{"float slot subscripted", "xs = []\nxs.append([7, 8])\nxs.append(1.5)\nprint(xs[1][0])\n",
			"TypeError", "'float' object is not subscriptable"},
		{"None slot subscripted", "xs = []\nxs.append([7, 8])\nxs.append(None)\nprint(xs[1][0])\n",
			"TypeError", "'NoneType' object is not subscriptable"},
		{
			// A set slot is asked as a member, so a subscript that names no member is the set's own
			// failure — the KeyError the record has always raised for this documented gusty
			// extension (docs/language.md § Dicts & sets), now raised by the compiled arm too so one
			// question has one answer at every depth (ADR 0251).
			"set slot names no member", "xs = []\nxs.append({5, 6, 7})\nprint(xs[0][9])\n",
			"KeyError", "not in set",
		},
		{"position out of range below the slot", "xs = []\nxs.append([7, 8])\nprint(xs[0][9])\n",
			"IndexError", "list index out of range"},
		{"negative position out of range below the slot", "xs = []\nxs.append([7, 8])\nprint(xs[0][-9])\n",
			"IndexError", "list index out of range"},
		// The INTERPRETED leg answers `KeyError: 'z'` -- a KeyError carries the key's repr (Gap
		// R.189, ADR 0301). The compiled leg still prints the module's constant "key not found",
		// because its raise is a compile-time string and naming the key needs the key rendered at run
		// time, which is L11.1's tagged word; the table runs both legs, so the string below is the
		// compiled one and integration/key_error_names_the_key_test.go pins the asymmetry.
		{"missing key below the slot", "d = {}\nd[\"a\"] = [1, 2]\nprint(d[\"z\"][0])\n",
			// The INTERPRETED KeyError carries the key's repr ('z' here); the compiled leg still
			// prints the module's constant sentence, because its raise is a compile-time string and
			// naming the key needs the key rendered at run time -- L11.1's tagged word, and why this
			// row is PARTIAL rather than closed (Gap R.189, ADR 0301).
			"KeyError", "'z'"},
		{"character position out of range", "xs = []\nxs.append(\"ab\")\nprint(xs[0][5])\n",
			"IndexError", "string index out of range"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ee := trapRun(t, tc.src)
			if ee.ExnType != tc.class {
				t.Errorf("interpreter raised %q, want %q (msg %q)", ee.ExnType, tc.class, ee.ExnMsg)
			}
			if ee.ExnMsg != tc.message {
				t.Errorf("interpreter message =\n  %q\nwant\n  %q", ee.ExnMsg, tc.message)
			}
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the oracle traps on: %v", err)
			}
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, "Traceback (most recent call last):") {
				t.Errorf("the compiled program printed no traceback:\n%s", out)
			}
			// A KeyError carries the key's repr, in the compiled program as in the reference — the
			// raise site renders a literal key into the message the way the reference words it
			// (roadmap Gap R.189, paid by the compiled leg in ADR 0302's cycle). A key the compiler
			// cannot see as a literal keeps the generic sentence, and L11.1's tagged word is what would
			// name those too; a row here asserting the old prose would be asserting a regression.
			compiledWant := tc.message
			if !strings.Contains(out, compiledWant) {
				t.Errorf("compiled message missing %q:\n%s", compiledWant, out)
			}
			if strings.Contains(out, "codegen:") {
				t.Errorf("the compiled backend refused what the oracle traps on:\n%s", out)
			}
		})
	}
}

// TestRunTimeBuiltNestedSlotReadRefusesWhatItCannotProve pins what the door still declines, and that
// it declines by naming the missing half. A *read* is answered by the object, an *ordering* of it is
// answered (ADR 0252, slot_order_object_test.go), the **number** use is answered since ADR 0265, and so
// are the membership test and the loop — which are no longer this table's rows, having moved to
// TestRunTimeBuiltSlotMembershipAndLoopAnswerOnBothLegs with the rest of Gap R.95's family. What is
// still owed is a haystack two slots deep: the read below it is answered, the test and the loop over it
// are not, and each is a refusal with a name — never an exit 2 and never a verdict (roadmap L11.1).
func TestRunTimeBuiltNestedSlotReadRefusesWhatItCannotProve(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			// A set *variable* is read by the untagged path, which this backend declines as an unsupported
			// subscript — while the record answers the documented membership question (`sa[0]` asks
			// the set for the member 0 and raises KeyError: not in set). Two backends, two answers to one
			// shape: the compiled leg owes either the extension or a runtime trap, and a refusal is
			// neither (Gap R.37's rule, this shape newly measured as Gap R.94).
			"set variable subscripted",
			"sa = {1, 2}\nprint(sa[0])\n",
			"index of a non-literal variable",
		},
		{
			// (Two rows used to sit here demanding the refusal "index cannot reach into xs's slots" —
			// a membership test and a loop whose haystack was a slot read *below* a slot read. The
			// haystack is that chain now: `builtSlotPair` hands the pair that ADR 0251's recursive read
			// already computed to the four arms, which never asked how deep the chain was — only what
			// the object at its end is. Both rows moved to the parity tables (Gap R.210, ADR 0324).)
			//
			// The **needle** at that depth reached the same door in the same commit: a needle that is a
			// slot read is read as a (payload, tag) pair too, so `ys[0][0] in xs[0]` and
			// `d["a"]["k"] in xs[0]` answer what the reference answers and are parity rows above
			// (Gap R.211, ADR 0324).
			//
			// What is left is a needle whose kind no road can prove at all. The haystack's tag picks the
			// arm, and the arm compares the needle's payload against each slot under a tag, so a needle
			// that is a number on one path and a text on another has no tag to carry and the verdict
			// would be whichever word happened to match. This is Gap R.83's shape seen from this door:
			// the reference answers `0`, and the refusal names the missing half rather than guessing.
			"a needle whose kind cannot be proven",
			"def pick(v):\n    if v:\n        return v\n    return \"a\"\n\nxs = []\nxs.append([7, 8])\nprint(1 if pick(0) in xs[0] else 0)\n",
			"more than one kind",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%s (%q): compiled; want a refusal", tc.name, tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s refused with %q, want it to mention %q", tc.name, err.Error(), tc.want)
			}
			if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
				t.Errorf("%s failed as an IR problem instead of a front-end refusal: %v", tc.name, err)
			}
		})
	}
}

// TestRunTimeBuiltSlotMembershipAndLoopAnswerOnBothLegs is Gap R.95 paid: the membership test whose
// haystack is a slot, and the loop whose iterable is one.
//
// Both were refusals, and the loop was worse than a refusal in the shapes whose container the compiler
// had *already* written a kind for: `d = {}` / `d["a"] = [1, 2]` / `for v in d["a"]` stepped over the
// dict's own words and printed a single `0` where the reference prints `1` and `2`, and the same shape
// holding a text printed nothing at all — the exit code of success wearing an answer. Choosing the
// helper by the tag the writer left beside the payload instead of the kind the compiler had written
// down is the whole change, and it is ADR 0251's rule applied to the two callers that had not taken it.
//
// Every row is judged against the record and, in the sibling integration file, against CPython.
func TestRunTimeBuiltSlotMembershipAndLoopAnswerOnBothLegs(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		// ---- the two shapes the roadmap row is written with.
		{"membership in a container the program built", "xs = []\nxs.append([7, 8])\nprint(1 if 7 in xs[0] else 0)\n", "1\n"},
		{"absent membership in a built container", "xs = []\nxs.append([7, 8])\nprint(1 if 9 in xs[0] else 0)\n", "0\n"},
		{"iteration of a container the program built", "xs = []\nxs.append([7, 8])\nfor v in xs[0]:\n    print(v)\n", "7\n8\n"},
		// ---- the same question through a dict key, which is the shape that had been answering wrongly.
		{"loop over a list under a dict key", "d = {}\nd[\"a\"] = [1, 2]\nfor v in d[\"a\"]:\n    print(v)\n", "1\n2\n"},
		{"membership of a list under a dict key", "d = {}\nd[\"a\"] = [1, 2]\nprint(1 in d[\"a\"], 3 in d[\"a\"])\n", "True False\n"},
		{"loop over a text under a dict key", "d = {}\nd[\"a\"] = \"abc\"\nfor v in d[\"a\"]:\n    print(v)\n", "a\nb\nc\n"},
		{"loop over a dict under a dict key", "d = {}\nd[\"a\"] = {\"k\": 1, \"j\": 2}\nfor v in d[\"a\"]:\n    print(v)\n", "k\nj\n"},
		// ---- the four kinds one program can put in one slot: each arm runs, none of them is chosen by
		// the compiler, and the tag decides which question the object is asked.
		{
			"list, dict, set and text slots in one container",
			"xs = []\nxs.append([7, 8])\nxs.append({\"k\": 1})\nxs.append({5})\nxs.append(\"abc\")\nprint(7 in xs[0], \"k\" in xs[1], 5 in xs[2], \"b\" in xs[3])\n",
			"True True True True\n",
		},
		{
			"the same four slots, stepped over",
			"xs = []\nxs.append([1, 2])\nxs.append({\"k\": 9})\nxs.append({5})\nxs.append(\"ab\")\nfor v in xs[0]:\n    print(v)\nfor v in xs[1]:\n    print(v)\nfor v in xs[2]:\n    print(v)\nfor v in xs[3]:\n    print(v)\n",
			"1\n2\nk\n5\na\nb\n",
		},
		// ---- a dict is tested by its keys, which is what Python says a dict is tested by, and the
		// intern-table collision ADR 0232 named is still answered: the text "7" and the integer 7 are not
		// the same key even when the slot's payload says they are.
		{"a dict slot is tested by its keys", "d = {}\nd[\"a\"] = {7: \"x\"}\nprint(7 in d[\"a\"], \"7\" in d[\"a\"])\n", "True False\n"},
		{"a set slot is tested by its members", "xs = []\nxs.append({1, 2})\nprint(1 in xs[0], 3 in xs[0])\n", "True False\n"},
		{"a set slot does not confuse a text with its index", "xs = []\nxs.append({\"a\"})\nprint(1 in xs[0], \"a\" in xs[0])\n", "False True\n"},
		// ---- the text arm asks a substring question of a text needle, which is a different question from
		// the one the other three arms ask, and the answer is not "the character is in the list".
		{"substring of a text slot", "xs = []\nxs.append(\"abc\")\nprint(\"b\" in xs[0], \"z\" in xs[0])\n", "True False\n"},
		{"substring across two text slots", "xs = []\nxs.append(\"abc\")\nxs.append(\"xyz\")\nprint(\"bc\" in xs[0], \"bc\" in xs[1])\n", "True False\n"},
		// ---- `not in` is the same door with the verdict turned over, not a second door.
		{"not in a built container", "xs = []\nxs.append([1, 2])\nprint(9 not in xs[0], 1 not in xs[0])\n", "True False\n"},
		// ---- a container slot's *value* is what the loop binds, and the tag travels with it: printing the
		// loop variable asks the inner container's printer rather than reading its handle as a number.
		{
			"a loop over a slot binds containers as well as numbers",
			"xs = []\nxs.append([[1, 2], 3])\nfor v in xs[0]:\n    print(v)\n",
			"[1, 2]\n3\n",
		},
		{
			"a loop over a slot of every kind at once",
			"xs = []\nxs.append([1, \"a\", 2.5, None, True])\nfor v in xs[0]:\n    print(v)\n",
			"1\na\n2.5\nNone\nTrue\n",
		},
		// ---- the loop keeps the loop surface: `break`, `continue`, the `else` arm, and a position the
		// program computed rather than wrote.
		{
			"break and continue inside a loop over a slot",
			"xs = []\nxs.append([1, 2, 3, 4])\nfor v in xs[0]:\n    if v == 2:\n        continue\n    if v == 3:\n        break\n    print(v)\n",
			"1\n",
		},
		{
			"the else arm runs when the loop finishes",
			"xs = []\nxs.append([1, 2])\nfor v in xs[0]:\n    print(v)\nelse:\n    print(\"done\")\n",
			"1\n2\ndone\n",
		},
		{
			"the else arm does not run when the loop breaks",
			"xs = []\nxs.append([1, 2])\nfor v in xs[0]:\n    break\nelse:\n    print(\"done\")\n",
			"",
		},
		{"a position the program computed picks the haystack", "xs = []\nxs.append([7, 8])\ni = 0\nprint(7 in xs[i])\n", "True\n"},
		{"a computed position steps the same loop", "xs = []\nxs.append(\"abc\")\ni = 0\nfor v in xs[i]:\n    print(v)\n", "a\nb\nc\n"},
		{"an empty container steps zero times", "xs = []\nxs.append([])\nfor v in xs[0]:\n    print(v)\nprint(\"end\")\n", "end\n"},
		{"an empty text steps zero times", "xs = []\nxs.append(\"\")\nfor v in xs[0]:\n    print(v)\nprint(\"end\")\n", "end\n"},
		// ---- the needle is not always a literal: a slot, a pair-bound name and a container all arrive as
		// the (payload, tag) pair the container's own elements are stored as.
		{"a slot read as the needle", "xs = []\nxs.append([7, 8])\nys = []\nys.append(7)\nprint(ys[0] in xs[0])\n", "True\n"},
		{"a pair-bound name as the needle", "xs = []\nxs.append([7, 8])\nys = []\nys.append([7, 8])\nn = ys[0][0]\nprint(n in xs[0])\n", "True\n"},
		{"a container as the needle", "xs = []\nxs.append([[1, 2], 3])\nprint([1, 2] in xs[0], [1, 3] in xs[0])\n", "True False\n"},
		// ---- what the static door already answered, pinned so this door cannot disturb it: the tag the
		// literal describes still routes the question, and a plain container still walks itself.
		{"a literal's element still answers by the static door", "xs = [[1, 2], [3, 4]]\nprint(1 in xs[0])\n", "True\n"},
		{"a literal's element still loops", "xs = [[1, 2]]\nfor v in xs[0]:\n    print(v)\n", "1\n2\n"},
		{"a plain list still walks itself", "xs = [1, 2, 3]\nprint(2 in xs)\nfor v in xs:\n    print(v)\n", "True\n1\n2\n3\n"},
		{"a plain dict still walks its keys", "d = {\"a\": 1}\nfor k in d:\n    print(k)\n", "a\n"},
		// ---- the haystack one slot deeper. `print(xs[0][0])` was answered by ADR 0251's recursive read
		// long before these two doors existed; what the same chain could not do was *be* a haystack. The
		// fix is not a new dispatch — it is handing those two callers the pair the read already computed,
		// which is what `builtSlotPair` now does by falling through to `runtimeSlotPairDeep` when the
		// side it would read is itself a slot (roadmap Gap R.210, ADR 0324).
		{
			"membership two slots deep in a container the program built",
			"xs = []\nxs.append([[7, 8]])\nprint(7 in xs[0][0])\n",
			"True\n",
		},
		{
			"absent membership two slots deep",
			"xs = []\nxs.append([[7, 8]])\nprint(9 in xs[0][0])\n",
			"False\n",
		},
		{
			"iteration of a container two slots deep",
			"xs = []\nxs.append([[7, 8]])\nfor v in xs[0][0]:\n    print(v)\n",
			"7\n8\n",
		},
		{
			"a text two slots deep steps its characters",
			"xs = []\nxs.append([\"abc\"])\nfor v in xs[0][0]:\n    print(v)\n",
			"a\nb\nc\n",
		},
		{
			"a set two slots deep steps its members",
			"xs = []\nxs.append([{5, 6}])\nfor v in xs[0][0]:\n    print(v)\n",
			"5\n6\n",
		},
		{
			"a dict chain reached by two keys steps its keys",
			"d = {}\nd[\"a\"] = {\"k\": [1, 2]}\nfor v in d[\"a\"][\"k\"]:\n    print(v)\n",
			"1\n2\n",
		},
		{
			"a dict two slots deep is tested by its keys",
			"d = {}\nd[\"a\"] = {\"k\": {\"j\": 1}}\nprint(\"j\" in d[\"a\"][\"k\"], \"z\" in d[\"a\"][\"k\"])\n",
			"True False\n",
		},
		{
			"three slots deep, both questions", "xs = []\nxs.append([[[3, 4]]])\nfor v in xs[0][0][0]:\n    print(v)\nprint(3 in xs[0][0][0])\n", "3\n4\nTrue\n"},
		{
			"not in, two slots deep",
			"xs = []\nxs.append([[1, 2]])\nprint(9 not in xs[0][0], 1 not in xs[0][0])\n",
			"True False\n",
		},
		{
			"a position the program computed reaches two slots deep",
			"xs = []\nxs.append([[1, 2]])\ni = 0\nprint(1 in xs[i][i])\nfor v in xs[i][i]:\n    print(v)\n",
			"True\n1\n2\n",
		},
		{
			"a chain of two containers, stepped separately",
			"xs = []\nxs.append([[1], {\"k\": 2}])\nfor v in xs[0][0]:\n    print(v)\nfor v in xs[0][1]:\n    print(v)\n",
			"1\nk\n",
		},
		{
			"mixed kinds two slots deep bind their own tags",
			"xs = []\nxs.append([[[1, \"a\"], 2.5, None]])\nfor v in xs[0][0]:\n    print(v)\n",
			"[1, 'a']\n2.5\nNone\n",
		},
		{
			"break, continue and else two slots deep",
			"xs = []\nxs.append([[1, 2, 3, 4]])\nfor v in xs[0][0]:\n    if v == 2:\n        continue\n    if v == 3:\n        break\n    print(v)\nelse:\n    print(\"done\")\n",
			"1\n",
		},
		{
			"an empty container two slots deep steps zero times",
			"xs = []\nxs.append([[]])\nfor v in xs[0][0]:\n    print(v)\nprint(\"end\")\n",
			"end\n",
		},
		{
			"a substring test two slots deep",
			"xs = []\nxs.append([\"abc\"])\nprint(\"b\" in xs[0][0], \"z\" in xs[0][0])\n",
			"True False\n",
		},
		// ---- the needle asked the same question as the haystack. A value the program put in a container
		// has its kind beside its payload and nowhere else, so a needle that is itself a slot read is read
		// as a (payload, tag) pair rather than through the notebook of container variables (roadmap
		// Gap R.211, ADR 0324). Two equal containers in two different objects are equal, and the answer
		// is True — the payload alone would answer it with two addresses.
		{
			"a container read out of a slot as the needle",
			"xs = []\nxs.append([[1, 2], 3])\nys = []\nys.append([1, 2])\nprint(ys[0] in xs[0])\n",
			"True\n",
		},
		{
			"an absent container read out of a slot as the needle",
			"xs = []\nxs.append([[1, 2], 3])\nys = []\nys.append([1, 3])\nprint(ys[0] in xs[0])\n",
			"False\n",
		},
		{
			"a set read out of a slot as the needle of a list",
			"xs = []\nxs.append([{1, 2}])\nys = []\nys.append({1, 2})\nprint(ys[0] in xs[0])\n",
			"True\n",
		},
		{
			"a dict read out of one container as the needle of another",
			"xs = []\nxs.append([{\"k\": 1}])\nd = {}\nd[\"z\"] = {\"k\": 1}\nprint(d[\"z\"] in xs[0])\n",
			"True\n",
		},
		{
			"a set needle asked of a set is answered, not refused",
			"xs = []\nxs.append({1, 2})\nys = []\nys.append({1})\nprint(ys[0] in xs[0])\n",
			"False\n",
		},
		// ---- the needle at the same depth as the haystack, and a chain four long. Both were refusals
		// before the needle was read the way the haystack is read (Gap R.211, ADR 0324): the pair the
		// read returns is an operand as much as it is a value to print.
		{
			"a needle two slots deep, read the same way",
			"xs = []\nxs.append([7, 8])\nys = []\nys.append([[7, 8]])\nprint(1 if ys[0][0] in xs[0] else 0)\n",
			"0\n",
		},
		{
			"a needle that is a dict slot read two levels up",
			"xs = []\nxs.append([[7, 8]])\nd = {}\nd[\"a\"] = {\"k\": [7, 8]}\nprint(1 if d[\"a\"][\"k\"] in xs[0] else 0)\n",
			"1\n",
		},
		{
			"four slots deep, stepped and tested",
			"xs = []\nxs.append([[[[1, 2]]]])\nfor v in xs[0][0][0][0]:\n    print(v)\nprint(1 in xs[0][0][0][0])\n",
			"1\n2\nTrue\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused: %v", tc.name, tc.src, err)
			}
			if out := runIR(t, res.IR); out != tc.want {
				t.Errorf("%s: AOT ran %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
			if out := captureStdout(t, tc.src); out != tc.want {
				t.Errorf("%s: the other leg printed %q, want CPython's %q\nsrc: %s", tc.name, out, tc.want, tc.src)
			}
		})
	}
}

// TestRunTimeBuiltSlotMembershipAndLoopTrapLikeCPython pins the failures of the same two doors. A slot
// holding a number, a verdict or None has no elements, keys, members or characters to walk, and CPython
// stops on each — with a *different* sentence for each question: membership names the type
// (`argument of type 'int' is not iterable`) and a loop names the object
// (`'int' object is not iterable`). Before this table both shapes were answered: the test read the
// payload as a heap handle and said False, and the loop stepped over nothing at all, each at exit 0.
func TestRunTimeBuiltSlotMembershipAndLoopTrapLikeCPython(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, class, message, catch string }{
		// ---- the same failures one slot deeper (Gap R.210): the chain is longer, the sentence is not.
		{
			"membership of an int slot two slots deep",
			"xs = []\nxs.append([[1, 2], 5])\nprint(1 in xs[0][1])\n",
			"TypeError", "argument of type 'int' is not iterable",
			"xs = []\nxs.append([[1, 2], 5])\ntry:\n    print(1 in xs[0][1])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a loop over an int slot two slots deep",
			"xs = []\nxs.append([[1, 2], 5])\nfor v in xs[0][1]:\n    print(v)\n",
			"TypeError", "'int' object is not iterable",
			"xs = []\nxs.append([[1, 2], 5])\ntry:\n    for v in xs[0][1]:\n        print(v)\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a loop over a None slot three slots deep",
			"xs = []\nxs.append([[None]])\nfor v in xs[0][0][0]:\n    print(v)\n",
			"TypeError", "'NoneType' object is not iterable",
			"xs = []\nxs.append([[None]])\ntry:\n    for v in xs[0][0][0]:\n        print(v)\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a number needle against a text two slots deep",
			"xs = []\nxs.append([\"ab\"])\nprint(1 in xs[0][0])\n",
			"TypeError", "'in <string>' requires string as left operand, not int",
			"xs = []\nxs.append([\"ab\"])\ntry:\n    print(1 in xs[0][0])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a missing dict key two slots deep",
			"d = {}\nd[\"a\"] = {\"k\": [1, 2]}\nprint(1 in d[\"a\"][\"z\"])\n",
			"KeyError", "'z'",
			"d = {}\nd[\"a\"] = {\"k\": [1, 2]}\ntry:\n    print(1 in d[\"a\"][\"z\"])\nexcept KeyError:\n    print(\"caught\")\n",
		},
		{
			"a float slot two slots deep",
			"xs = []\nxs.append([[1, 2], 2.5])\nfor v in xs[0][1]:\n    print(v)\n",
			"TypeError", "'float' object is not iterable",
			"xs = []\nxs.append([[1, 2], 2.5])\ntry:\n    for v in xs[0][1]:\n        print(v)\nexcept TypeError:\n    print(\"caught\")\n",
		},
		// ---- a needle that cannot be a key or a member. A dict is looked up by its key and a set is
		// hashed by its member, and both stop on a container with the reference's own sentence rather
		// than reporting the absence (ADR 0311's write-side rule, read; roadmap Gap R.211, ADR 0324).
		{
			"a list needle asked of a dict slot",
			"xs = []\nxs.append({\"k\": 1})\nprint([1] in xs[0])\n",
			"TypeError", "unhashable type: 'list'",
			"xs = []\nxs.append({\"k\": 1})\ntry:\n    print([1] in xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a list needle asked of a set slot",
			"xs = []\nxs.append({1, 2})\nprint([1] in xs[0])\n",
			"TypeError", "unhashable type: 'list'",
			"xs = []\nxs.append({1, 2})\ntry:\n    print([1] in xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a dict needle asked of a set slot",
			"xs = []\nxs.append({1, 2})\nys = []\nys.append({1: 1})\nprint(ys[0] in xs[0])\n",
			"TypeError", "unhashable type: 'dict'",
			"xs = []\nxs.append({1, 2})\nys = []\nys.append({1: 1})\ntry:\n    print(ys[0] in xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a set needle asked of a dict slot",
			"xs = []\nxs.append({1: 2})\nys = []\nys.append({1})\nprint(ys[0] in xs[0])\n",
			"TypeError", "unhashable type: 'set'",
			"xs = []\nxs.append({1: 2})\nys = []\nys.append({1})\ntry:\n    print(ys[0] in xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"membership of an int slot",
			"xs = []\nxs.append(5)\nprint(1 in xs[0])\n",
			"TypeError", "argument of type 'int' is not iterable",
			"xs = []\nxs.append(5)\ntry:\n    print(1 in xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"membership of a float slot",
			"xs = []\nxs.append(1.5)\nprint(1 in xs[0])\n",
			"TypeError", "argument of type 'float' is not iterable",
			"xs = []\nxs.append(1.5)\ntry:\n    print(1 in xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"membership of a verdict slot",
			"xs = []\nxs.append(True)\nprint(1 in xs[0])\n",
			"TypeError", "argument of type 'bool' is not iterable",
			"xs = []\nxs.append(True)\ntry:\n    print(1 in xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"membership of a None slot",
			"xs = []\nxs.append(None)\nprint(1 in xs[0])\n",
			"TypeError", "argument of type 'NoneType' is not iterable",
			"xs = []\nxs.append(None)\ntry:\n    print(1 in xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"membership of a number slot through a dict key",
			"d = {}\nd[\"a\"] = 5\nprint(1 in d[\"a\"])\n",
			"TypeError", "argument of type 'int' is not iterable",
			"d = {}\nd[\"a\"] = 5\ntry:\n    print(1 in d[\"a\"])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a text haystack with a number needle",
			"xs = []\nxs.append(\"abc\")\nprint(1 in xs[0])\n",
			"TypeError", "'in <string>' requires string as left operand, not int",
			"xs = []\nxs.append(\"abc\")\ntry:\n    print(1 in xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a text haystack with a None needle",
			"xs = []\nxs.append(\"abc\")\nprint(None in xs[0])\n",
			"TypeError", "'in <string>' requires string as left operand, not NoneType",
			"xs = []\nxs.append(\"abc\")\ntry:\n    print(None in xs[0])\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a loop over an int slot",
			"xs = []\nxs.append(5)\nfor v in xs[0]:\n    print(v)\n",
			"TypeError", "'int' object is not iterable",
			"xs = []\nxs.append(5)\ntry:\n    for v in xs[0]:\n        print(v)\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a loop over a float slot",
			"xs = []\nxs.append(1.5)\nfor v in xs[0]:\n    print(v)\n",
			"TypeError", "'float' object is not iterable",
			"xs = []\nxs.append(1.5)\ntry:\n    for v in xs[0]:\n        print(v)\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a loop over a verdict slot",
			"xs = []\nxs.append(True)\nfor v in xs[0]:\n    print(v)\n",
			"TypeError", "'bool' object is not iterable",
			"xs = []\nxs.append(True)\ntry:\n    for v in xs[0]:\n        print(v)\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a loop over a None slot",
			"xs = []\nxs.append(None)\nfor v in xs[0]:\n    print(v)\n",
			"TypeError", "'NoneType' object is not iterable",
			"xs = []\nxs.append(None)\ntry:\n    for v in xs[0]:\n        print(v)\nexcept TypeError:\n    print(\"caught\")\n",
		},
		{
			"a loop over an int slot under a dict key",
			"d = {}\nd[\"a\"] = 5\nfor v in d[\"a\"]:\n    print(v)\n",
			"TypeError", "'int' object is not iterable",
			"d = {}\nd[\"a\"] = 5\ntry:\n    for v in d[\"a\"]:\n        print(v)\nexcept TypeError:\n    print(\"caught\")\n",
		},
		// The slot read itself keeps its own traps, and the table above pins them; what belongs here is
		// the one that the membership test *introduces*: a key the program never stored is asked for by
		// the read that comes before any question about its value, and the raise is the read's.
		{
			"a haystack key that was never stored",
			"d = {}\nd[\"a\"] = [1, 2]\nprint(1 in d[\"z\"])\n",
			"KeyError", "'z'",
			"d = {}\nd[\"a\"] = [1, 2]\ntry:\n    print(1 in d[\"z\"])\nexcept KeyError:\n    print(\"caught\")\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("%s (%q): refused with %v; a trap the reference raises must be emitted, not refused (Gap R.37)", tc.name, tc.src, err)
			}
			// The record leg: the retired engine's answer to the same source is a trap of this class,
			// which is the half of the row that says the compiled raise is the language's, not this
			// backend's invention.
			if ee := trapRun(t, tc.src); ee.ExnType != tc.class {
				t.Errorf("%s: the record raised %q, want %q (msg %q)", tc.name, ee.ExnType, tc.class, ee.ExnMsg)
			} else if !strings.Contains(ee.ExnMsg, tc.message) {
				t.Errorf("%s: the record's message %q lacks the reference's %q", tc.name, ee.ExnMsg, tc.message)
			}
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, "Traceback (most recent call last):") {
				t.Errorf("%s printed no traceback:\n%s", tc.name, out)
			}
			if !strings.Contains(out, tc.class) {
				t.Errorf("%s raised without naming %s:\n%s", tc.name, tc.class, out)
			}
			if !strings.Contains(out, tc.message) {
				t.Errorf("%s raised %q, want the reference's %q", tc.name, out, tc.message)
			}
			if strings.Contains(out, "codegen:") {
				t.Errorf("%s refused what the oracle traps on:\n%s", tc.name, out)
			}
			// And the raise is catchable: the program's own `except TypeError:` is the reason a trap is
			// a typed raise and not a compiler message (ADR 0228).
			caught, err := Compile(tc.catch)
			if err != nil {
				t.Fatalf("%s: the caught version refused with %v\n%s", tc.name, err, tc.catch)
			}
			if out := runIR(t, caught.IR); !strings.Contains(out, "caught") {
				t.Errorf("%s: the program's except TypeError: did not reach the raise:\n%s\nsrc: %s", tc.name, out, tc.catch)
			}
		})
	}
}

// TestRunTimeBuiltSlotMembershipAndLoopAsksTheObjectInTheModule is the IR half of the claim: the
// compiled program branches on the tag the slot carries and asks a *different* helper per kind, rather
// than choosing one helper from a table the compiler kept. It also keeps the two shapes that made this
// a bug rather than a gap out of the module: a compile-time global in a value position (exit 2, ADR 0166)
// and a loop that walked the outer container's words instead of the slot's object.
func TestRunTimeBuiltSlotMembershipAndLoopAsksTheObjectInTheModule(t *testing.T) {
	t.Parallel()
	src := "xs = []\nxs.append([7, 8])\nxs.append({\"k\": 1})\nxs.append({5})\nxs.append(\"abc\")\n"
	src += "print(7 in xs[0], \"k\" in xs[1], 5 in xs[2], \"b\" in xs[3])\n"
	src += "for v in xs[0]:\n    print(v)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	for _, want := range []string{
		// the tag decides which question the object is asked
		"= icmp eq i32 %t",
		// one helper per kind, and they are the tagged ones
		"call i32 @rt_contains_tagged(",
		"call i32 @rt_dict_has_tagged(",
		"call i32 @rt_set_contains_tagged(",
		"call i32 @rt_str_contains(",
		// the loop's three questions, all of them asked of the object
		"call i32 @rt_iter_len(",
		"call i32 @rt_iter_elem(",
		"call i32 @rt_iter_tag(",
		// the arms merge through a phi, and only the value-producing arms are predecessors
		"= phi i32 [",
	} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module does not contain %q:\n%s", want, res.IR)
		}
	}
	for i, ln := range strings.Split(res.IR, "\n") {
		if strings.Contains(ln, "i32 @.") {
			t.Errorf("line %d keeps a global in a value position: %s", i+1, ln)
		}
	}
	// The raises travel with the arms that need them: one sentence per kind the slot can report, both
	// wordings of the iterability question, and none of them a compile-time refusal.
	for _, want := range []string{
		"argument of type 'int' is not iterable",
		"argument of type 'NoneType' is not iterable",
		"'int' object is not iterable",
		"'in <string>' requires string as left operand, not int",
	} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module lost the raise %q the reference words", want)
		}
	}
	// And a loop the door has no business in stays a loop the module answers without it: an int list
	// walked by the ordinary path does not pay for a dispatch it does not need.
	plain, err := Compile("xs = [1, 2, 3]\nfor v in xs:\n    print(v)\n")
	if err != nil {
		t.Fatalf("a plain list loop refused: %v", err)
	}
	for _, notWant := range []string{"call i32 @rt_iter_len(", "call i32 @rt_iter_elem(", "call i32 @rt_iter_tag("} {
		if strings.Contains(plain.IR, notWant) {
			t.Errorf("a loop the compiler can describe carries the run-time dispatch %s:\n%s", notWant, plain.IR)
		}
	}
}

// TestRunTimeBuiltNestedSlotReadAsksTheObjectInTheModule is the IR half of the claim: the compiled
// program must really branch on the tag the object carries and ask three different helpers, rather
// than answering from a table the compiler kept. It also keeps the two traps this shape used to have
// out of the module — a global in a value position, and the retired helper that compared slots by
// payload alone.
func TestRunTimeBuiltNestedSlotReadAsksTheObjectInTheModule(t *testing.T) {
	t.Parallel()
	src := "xs = []\nxs.append([7, 8])\nxs.append({\"k\": 5})\nxs.append(\"abc\")\nprint(xs[0][0], xs[1][\"k\"], xs[2][1])\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	for _, want := range []string{
		// the tag decides which object is asked
		"= icmp eq i32 %t", "br i1",
		// one arm per kind, each with its own helper
		"call i32 @rt_get_elem(", "call i32 @rt_tag_of(",
		"call i32 @rt_dict_get_tagged(", "call i32 @rt_dict_value_tag(",
		"call i32 @rt_str_char(",
		// the arms merge through a phi, and only the value-producing arms are predecessors
		"= phi i32 [",
	} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the module does not contain %q:\n%s", want, res.IR)
		}
	}
	for i, ln := range strings.Split(res.IR, "\n") {
		// A compile-time global in a value position is the shape `llc` rejects — exit 2, ADR 0166's
		// compiler-bug class — and the subscript chain is exactly where it used to appear.
		if strings.Contains(ln, "i32 @.") {
			t.Errorf("line %d keeps a global in a value position: %s", i+1, ln)
		}
	}
	// The bounds and key checks travel with the arms that need them, not around the dispatch.
	if !strings.Contains(res.IR, "call i32 @rt_list_len(") {
		t.Errorf("the list arm lost its bounds check:\n%s", res.IR)
	}
	if !strings.Contains(res.IR, "call i32 @rt_dict_has_tagged(") {
		t.Errorf("the dict arm lost its KeyError check:\n%s", res.IR)
	}
}

// TestAnInstanceSlotNamesItsClassForEveryTagQuestion is the unit half of Gap R.209's closure. The
// four questions below all dispatch on the same word, so one wrong writer makes all four answer about
// `int`; one right writer makes all four answer about `C` without any of the doors learning the name.
// The reference is asked for each source, not paraphrased.
func TestAnInstanceSlotNamesItsClassForEveryTagQuestion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"membership", "class C:\n    pass\nxs = []\nxs.append(C())\nprint(1 in xs[0])\n",
			"TypeError: argument of type 'C' is not iterable"},
		{"loop", "class C:\n    pass\nxs = []\nxs.append(C())\nfor v in xs[0]:\n    print(v)\n",
			"TypeError: 'C' object is not iterable"},
		{"len", "class C:\n    pass\nxs = []\nxs.append(C())\nprint(len(xs[0]))\n",
			"TypeError: object of type 'C' has no len()"},
		{"string membership", "class C:\n    pass\nxs = []\nxs.append(\"ab\")\nys = []\nys.append(C())\nprint(ys[0] in xs[0])\n",
			"TypeError: 'in <string>' requires string as left operand, not C"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused what the reference traps on: %v", err)
			}
			if ref := pythonTwinLine(t, tc.src); !strings.Contains(ref, tc.want) {
				t.Fatalf("the reference does not say %q: %q", tc.want, ref)
			}
			out := runIRMayTrap(t, res.IR)
			if !strings.Contains(out, "Traceback (most recent call last):") {
				t.Errorf("printed no traceback:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("compiled raise %q lacks the object's class %q\nsrc: %s", out, tc.want, tc.src)
			}
			if strings.Contains(out, "'int'") {
				t.Errorf("compiled raise answered the writer's old int tag instead of the object:\n%s", out)
			}
		})
	}
}

// TestAnInstanceSlotPrintsItsIdentityForm pins the other half of the tag: a handle the printer used to
// printf as a slot number. The address is intentionally not compared — the reference's is a process
// address and has no business being byte-identical to ours — but the class inside the brackets is.
func TestAnInstanceSlotPrintsItsIdentityForm(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{"bare construction", "class C:\n    pass\nprint(C())\n", "<__main__.C object at 0x"},
		{"bound name", "class C:\n    pass\nc = C()\nprint(c)\n", "<__main__.C object at 0x"},
		{"built slot", "class C:\n    pass\nxs = []\nxs.append(C())\nprint(xs[0])\n", "<__main__.C object at 0x"},
		{"literal list", "class C:\n    pass\nc = C()\nprint([c])\n", "[<__main__.C object at 0x"},
		{"dict value", "class C:\n    pass\nc = C()\nprint({\"k\": c})\n", "{'k': <__main__.C object at 0x"},
		{"set member", "class C:\n    pass\nc = C()\nprint({c})\n", "{<__main__.C object at 0x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused: %v", err)
			}
			out := runIR(t, res.IR)
			if !strings.HasPrefix(strings.TrimSpace(out), tc.want) {
				t.Errorf("compiled printed %q, want the identity form %q\nsrc: %s", out, tc.want, tc.src)
			}
			if regexp.MustCompile(`^\d+$`).MatchString(strings.TrimSpace(out)) {
				t.Errorf("compiled printed the heap handle: %q", out)
			}
		})
	}
}

// TestTheInstanceTagDoesNotDisturbTheOrdinaryContainerQuestions is a negative control: the same
// run-time-built doors now know instances, and a container slot must not start paying the class table or
// the instance raise. The IR assertion is the point; the answers keep the old doors honest.
func TestTheInstanceTagDoesNotDisturbTheOrdinaryContainerQuestions(t *testing.T) {
	t.Parallel()
	src := "xs = []\nxs.append([1, 2])\nxs.append(\"abc\")\nprint(1 in xs[0], \"b\" in xs[1])\nfor v in xs[0]:\n    print(v)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("refused: %v", err)
	}
	for _, want := range []string{"call i32 @rt_contains_tagged(", "call i32 @rt_str_contains(", "call i32 @rt_iter_len("} {
		if !strings.Contains(res.IR, want) {
			t.Errorf("the ordinary door lost %q:\n%s", want, res.IR)
		}
	}
	// The definitions live in the heap block that any container touches, so their absence is not a claim
	// this suite can honestly test (ADR 0324's lesson about `@rt_iter_len`). The module-specific class
	// table is: a program with no class must not grow one, and a built-slot door may still call the total
	// helper because the object it reads can only speak for itself.
	for _, notWant := range []string{"@rt.inst.qname", "@rt.inst.sname", "@rt.inst.names", "@rt.inst.short_names"} {
		if strings.Contains(res.IR, notWant) {
			t.Errorf("a program without a class carries the class table %q:\n%s", notWant, res.IR)
		}
	}
}
