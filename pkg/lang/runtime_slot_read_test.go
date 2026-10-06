package lang

import (
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
// The refusal was honest and the program was still unanswered: CPython prints 7, the interpreter
// prints 7, and the compiled leg exited 1 on the shape the roadmap names as its own remaining clause.
// The door this file pins asks the object instead of the notebook: the tag written beside the outer
// slot says which kind of object the payload names, and the tag written beside the inner slot says
// what the answer means. A list slot is read by position (bounds check and all), a dict slot by its
// (payload, tag) key, a text slot gives one character, and a slot that holds none of those raises the
// sentence CPython raises, per kind — because a payload read as a handle is a wrong answer wearing
// another object's bits.
//
// Every row below is checked on both engines: `Compile` + `lli` for the compiled leg, `EvalExpr` for
// the interpreter, and — in the sibling integration file — against CPython itself.

func TestRunTimeBuiltNestedSlotReadsAnswerInBothEngines(t *testing.T) {
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
// or a set has nothing below it, and a position can be out of range; CPython raises, the interpreter
// raises, and the compiled arm the tag dispatches to raises the same sentence — it may not refuse
// these, because refusing is what the row below (the numeric use) still legitimately does.
func TestRunTimeBuiltNestedSlotReadsTrapLikeCPython(t *testing.T) {
	for _, tc := range []struct{ name, src, class, message string }{
		{"number slot subscripted", "xs = []\nxs.append([7, 8])\nxs.append(5)\nprint(xs[1][0])\n",
			"TypeError", "'int' object is not subscriptable"},
		{"float slot subscripted", "xs = []\nxs.append([7, 8])\nxs.append(1.5)\nprint(xs[1][0])\n",
			"TypeError", "'float' object is not subscriptable"},
		{"None slot subscripted", "xs = []\nxs.append([7, 8])\nxs.append(None)\nprint(xs[1][0])\n",
			"TypeError", "'NoneType' object is not subscriptable"},
		{
			// A set slot is asked as a member, so a subscript that names no member is the set's own
			// failure — the KeyError the interpreter has always raised for this documented gusty
			// extension (docs/language.md § Dicts & sets), now raised by the compiled arm too so one
			// question has one answer at every depth (ADR 0251).
			"set slot names no member", "xs = []\nxs.append({5, 6, 7})\nprint(xs[0][9])\n",
			"KeyError", "not in set",
		},
		{"position out of range below the slot", "xs = []\nxs.append([7, 8])\nprint(xs[0][9])\n",
			"IndexError", "index out of range"},
		{"negative position out of range below the slot", "xs = []\nxs.append([7, 8])\nprint(xs[0][-9])\n",
			"IndexError", "index out of range"},
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
			compiledWant := tc.message
			if tc.class == "KeyError" && strings.HasPrefix(tc.message, "'") {
				// Only a KeyError whose message is a QUOTED key is the asymmetry: the compiled leg
				// cannot render a key at run time. A KeyError with its own sentence ("not in set",
				// "popitem(): dictionary is empty") is a constant both legs already agree on.
				compiledWant = "key not found"
			}
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
// it declines by naming the missing half. A *read* is answered by the object, and so is an *ordering*
// of it (ADR 0252, slot_order_object_test.go); the **number** use of the same slot is answered since
// ADR 0265 (numeric_slot_arith_test.go). What is still owed is the membership test and the loop, which
// need the haystack's *kind* rather than its tag. Each of these is a refusal with a name, never an
// exit 2 and never a verdict (roadmap L11.1's remaining clauses, Gaps R.95 and R.83).
func TestRunTimeBuiltNestedSlotReadRefusesWhatItCannotProve(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{
			// The haystack is a slot read, and the needle is a pair; but which helper asks the question
			// is chosen by the *kind* the compiler cannot state. The tag answers an equality and an
			// ordering; a membership needs to know whether to walk slots or bytes at all.
			"membership in a container the program built",
			"xs = []\nxs.append([7, 8])\nprint(1 if 7 in xs[0] else 0)\n",
			"needs a single static kind",
		},
		{
			// Iteration is the same missing word with a loop around it: the loop body's variable takes its
			// kind from the slot, which is the tagged value word again.
			"iteration of a container the program built",
			"xs = []\nxs.append([7, 8])\nfor v in xs[0]:\n    print(v)\n",
			"needs a single static kind",
		},
		{
			// A set *variable* is read by the untagged path, which this backend declines as an unsupported
			// subscript — while the interpreter answers the documented membership question (`sa[0]` asks
			// the set for the member 0 and raises KeyError: not in set). Two backends, two answers to one
			// shape: the compiled leg owes either the extension or a runtime trap, and a refusal is
			// neither (Gap R.37's rule, this shape newly measured as Gap R.94).
			"set variable subscripted",
			"sa = {1, 2}\nprint(sa[0])\n",
			"index of a non-literal variable",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
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

// TestRunTimeBuiltNestedSlotReadAsksTheObjectInTheModule is the IR half of the claim: the compiled
// program must really branch on the tag the object carries and ask three different helpers, rather
// than answering from a table the compiler kept. It also keeps the two traps this shape used to have
// out of the module — a global in a value position, and the retired helper that compared slots by
// payload alone.
func TestRunTimeBuiltNestedSlotReadAsksTheObjectInTheModule(t *testing.T) {
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
