package lang

import (
	"strings"
	"testing"
)

// dict_literal_key_test.go — the entry of a dict *literal* is answered by the dict doors, roadmap Gap R.215.
//
// `print({"a": 1}["a"])` refused with `index must be a constant`. The sentence named no missing
// capability: the backend builds the very same dict at runtime whenever it is bound to a name, and the
// read a *variable* takes is a `(payload, tag)` pair from the heap dict — which is why `x = {"a": "x"}`
// then `print(x["a"])` already printed `x`. A literal is a dict too, so the read goes through the same
// doors. What the fold can genuinely read it still answers itself, and the fold's own key rule had to
// become Python's: with `{1: 2, 1: 3}` the later entry is the dict's, so `[1]` is `3`.
//
// The rule this file exists to keep honest is that the read carries a tag with its payload. Every wrong
// answer in this family — printing a text's `@str_tab` position, printing `1` for a text, printing `0`
// for `None` — was a road that kept only the payload. Where such a road cannot carry the pair the
// compiler refuses, and the refusal names the road and the row.

// dictLiteralCases: the left column is what the program prints, and every entry was first read off
// CPython 3.12 (the reference leg), not off the compiler.
func TestADictLiteralAnswersTheEntryTheKeyNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
		want string
	}{
		// The shape that refused.
		{name: "text_key", src: "print({\"a\": 1}[\"a\"])\n", want: "1\n"},
		{name: "another_text_key", src: "print({\"a\": 1, \"b\": 2}[\"b\"])\n", want: "2\n"},
		{name: "text_value", src: "print({\"a\": \"x\"}[\"a\"])\n", want: "x\n"},
		{name: "float_value", src: "print({1: 1.5}[1])\n", want: "1.5\n"},
		{name: "none_value", src: "print({1: None}[1])\n", want: "None\n"},
		{name: "true_value", src: "print({1: True}[1])\n", want: "True\n"},
		{name: "false_value", src: "print({1: False}[1])\n", want: "False\n"},
		// The key is compared as a value too, so `1` and `1.0` and `True` are the same key and a text
		// never is. This is the dict key rule, read out of a literal for the first time.
		{name: "none_key", src: "print({None: 5}[None])\n", want: "5\n"},
		{name: "float_key", src: "print({1.5: 2}[1.5])\n", want: "2\n"},
		{name: "true_key", src: "print({True: 9}[True])\n", want: "9\n"},
		{name: "one_is_the_key_named_by_true", src: "print({1: \"a\"}[True])\n", want: "a\n"},
		{name: "mixed_keys", src: "print({1: \"one\", \"b\": \"two\", None: \"three\"}[\"b\"])\n", want: "two\n"},
		// A container entry is printed by the dict's own printer, not by `%d` of the handle.
		{name: "list_value", src: "print({\"a\": [1, 2]}[\"a\"])\n", want: "[1, 2]\n"},
		{name: "dict_value", src: "print({\"a\": {\"b\": 3}}[\"a\"])\n", want: "{'b': 3}\n"},
		{name: "set_value", src: "print({\"a\": {1}}[\"a\"])\n", want: "{1}\n"},
		{name: "nested_read", src: "print({\"a\": {\"b\": 3}}[\"a\"][\"b\"])\n", want: "3\n"},
		{name: "list_then_index", src: "print({\"a\": [[1, 2]]}[\"a\"][0][1])\n", want: "2\n"},
		// The entry is a value like any other: bound, measured, tested, looped over, formatted, computed with.
		{name: "bound_and_printed", src: "x = {\"a\": \"x\"}[\"a\"]\nprint(x)\n", want: "x\n"},
		{name: "bound_container_and_printed", src: "x = {\"a\": [1, 2]}[\"a\"]\nprint(x)\n", want: "[1, 2]\n"},
		{name: "len_direct", src: "print(len({\"a\": [1, 2]}[\"a\"]))\n", want: "2\n"},
		{name: "membership", src: "print(2 in {\"a\": [1, 2]}[\"a\"])\n", want: "True\n"},
		{name: "loop_over_entry", src: "for v in {\"a\": [1, 2]}[\"a\"]:\n    print(v)\n", want: "1\n2\n"},
		{name: "str_and_repr", src: "print(str({\"a\": \"x\"}[\"a\"]), repr({\"a\": \"x\"}[\"a\"]))\n", want: "x 'x'\n"},
		{name: "arith_on_int_entry", src: "print({\"a\": 1}[\"a\"] + 1)\n", want: "2\n"},
		{name: "concat_on_text_entry", src: "print({\"a\": \"x\"}[\"a\"] + \"!\")\n", want: "x!\n"},
		{name: "entry_as_plain_argument", src: "def show(v):\n    print(v)\n\nshow({\"a\": 1}[\"a\"])\n", want: "1\n"},
		// The fold's own key rule, now Python's: the later entry wins.
		{name: "repeat_key_last_wins", src: "print({1: 2, 1: 3}[1])\n", want: "3\n"},
		{name: "repeat_key_last_wins_through_the_door", src: "print({\"a\": 2, \"a\": 3}[\"a\"])\n", want: "3\n"},
		// A read the fold answers is still a read; the entry is the same value either way.
		{name: "fold_and_door_agree", src: "print({7: 42}[7])\n", want: "42\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("printed %q, want %q", got, tc.want)
			}
		})
	}
}

// The fold is not dead: the shape it reads is int keys to int values with an int key asked for, and the
// answer is a compile-time constant. A program that stays inside it must not gain the heap dict at all —
// the road to the doors runs through them, and a test that cannot tell the two apart tests neither.
func TestTheFoldStillAnswersWhatItCanRead(t *testing.T) {
	for _, src := range []string{
		"print({1: 2, 2: 3}[1])\n",
		"print({7: 42}[7])\n",
		"print({1: 2, 1: 3}[1])\n",
	} {
		ir := emittedModuleIR(t, src)
		for _, helper := range []string{"@rt_dict_get_tagged", "@rt_dict_has_tagged", "@rt_dict_value_tag"} {
			if strings.Contains(ir, helper) {
				t.Errorf("%q was answered by the heap dict (%s) rather than the fold", src, helper)
			}
		}
	}
}

// The fold is the same road for a repeated key, and keeps the same rule: the entries are scanned from the
// last to the first, because a key written twice is one key and the later write is the dict's.
func TestTheFoldKeepsTheLastValueOfARepeatedKeyWhenTheFoldReadsIt(t *testing.T) {
	if got := compiledOut(t, "print({1: 2, 1: 3}[1])\n"); got != "3\n" {
		t.Errorf("the fold answered %q where the reference answers 3; a repeated key is one key written twice", got)
	}
	ir := emittedModuleIR(t, "print({1: 2, 1: 3}[1])\n")
	for _, helper := range []string{"@rt_dict_get_tagged", "@rt_dict_has_tagged", "@rt_dict_value_tag"} {
		if strings.Contains(ir, helper) {
			t.Errorf("the fold reached the object door (%s)", helper)
		}
	}
}

// The other half of the same claim: the shape the fold cannot read really does reach the objects, so the
// negative control above is a control and not an empty assertion.
func TestTheLiteralDictReadRoutesThroughTheObjectsDoor(t *testing.T) {
	ir := emittedModuleIR(t, "print({\"a\": [1, 2]}[\"a\"])\n")
	for _, helper := range []string{"@rt_dict_get_tagged", "@rt_dict_value_tag", "@rt_dict_has_tagged", "@rt_print_mixed_value"} {
		if !strings.Contains(ir, helper) {
			t.Errorf("the literal dict read does not go through the heap dict (no %s)", helper)
		}
	}
}

// A missing key is the reference's exception, named by the key that was looked for — read back off the
// runtime's own words by jitTrap, so the assertion is about the program's exception and not about the
// compiler's opinion of it.
func TestAMissingKeyOfADictLiteralRaisesWhatTheReferenceRaises(t *testing.T) {
	for _, tc := range []struct {
		name        string
		src         string
		wantClass   string
		wantMessage string
	}{
		{name: "text_key_absent", src: "print({\"a\": 1}[\"b\"])\n", wantClass: "KeyError", wantMessage: "'b'"},
		{name: "int_key_absent", src: "print({\"a\": 1}[1])\n", wantClass: "KeyError", wantMessage: "1"},
		{name: "text_key_asked_of_int_dict", src: "print({1: 2}[\"a\"])\n", wantClass: "KeyError", wantMessage: "'a'"},
		{name: "zero_is_not_the_text_zero", src: "print({0: 2}[\"a\"])\n", wantClass: "KeyError", wantMessage: "'a'"},
		{name: "none_key_absent", src: "print({1: 2}[None])\n", wantClass: "KeyError", wantMessage: "None"},
		{name: "one_entry_absent", src: "print({\"a\": 1}[\"zz\"])\n", wantClass: "KeyError", wantMessage: "'zz'"},
		// The key the fold could not read is still a key the dict does not have, and a repeated entry
		// does not make it have one.
		{name: "fold_cannot_read_the_key", src: "print({1: 2, 1: 3}[\"a\"])\n", wantClass: "KeyError", wantMessage: "'a'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			class, message := jitTrap(t, tc.src)
			if class != tc.wantClass {
				t.Errorf("raised %s, want %s", class, tc.wantClass)
			}
			if message != tc.wantMessage {
				t.Errorf("message %q, want %q", message, tc.wantMessage)
			}
		})
	}
}

// The KeyError the doors raise is the ordinary one: `except KeyError` catches it, so a missing key is a
// value the program can handle rather than a wall.
func TestTheLiteralDictsKeyErrorIsCatchable(t *testing.T) {
	if got := compiledOut(t, "try:\n    print({\"a\": 1}[\"b\"])\nexcept KeyError:\n    print(\"no such key\")\n"); got != "no such key\n" {
		t.Errorf("got %q, want \"no such key\\n\"", got)
	}
	if got := compiledOut(t, "try:\n    print({1: 2}[\"a\"])\nexcept KeyError:\n    print(\"not a number key\")\n"); got != "not a number key\n" {
		t.Errorf("got %q, want \"not a number key\\n\"", got)
	}
}

// The change is that a literal is read the way a variable is, so the two roads are one road. The invariant
// they are held to is the one that matters to a program: the literal road is never worse than the variable
// road, and never wrong. Where both answer they answer the same and the reference's way; where the literal
// refuses, the variable road must not be answering the reference either — a refusal where the other road
// is right is a regression, and that is the shape a test that only checks the wins would let through.
func TestTheLiteralRoadIsNeverWorseThanTheVariableRoad(t *testing.T) {
	for _, tc := range []struct {
		name     string
		literal  string // printed with the dict written in place
		variable string // printed through the name the dict was bound to
		want     string // what CPython prints, "" where the reference raises
	}{
		{name: "text_key", literal: "print({\"a\": 1}[\"a\"])\n", variable: "d = {\"a\": 1}\nprint(d[\"a\"])\n", want: "1\n"},
		{name: "text_value", literal: "print({\"a\": \"x\"}[\"a\"])\n", variable: "d = {\"a\": \"x\"}\nprint(d[\"a\"])\n", want: "x\n"},
		{name: "float_value", literal: "print({1: 1.5}[1])\n", variable: "d = {1: 1.5}\nprint(d[1])\n", want: "1.5\n"},
		{name: "none_value", literal: "print({1: None}[1])\n", variable: "d = {1: None}\nprint(d[1])\n", want: "None\n"},
		{name: "list_value", literal: "print({\"a\": [1, 2]}[\"a\"])\n", variable: "d = {\"a\": [1, 2]}\nprint(d[\"a\"])\n", want: "[1, 2]\n"},
		{name: "dict_value", literal: "print({\"a\": {\"b\": 3}}[\"a\"])\n", variable: "d = {\"a\": {\"b\": 3}}\nprint(d[\"a\"])\n", want: "{'b': 3}\n"},
		{name: "entry_len", literal: "print(len({\"a\": [1, 2]}[\"a\"]))\n", variable: "d = {\"a\": [1, 2]}\nprint(len(d[\"a\"]))\n", want: "2\n"},
		{name: "entry_membership", literal: "print(2 in {\"a\": [1, 2]}[\"a\"])\n", variable: "d = {\"a\": [1, 2]}\nprint(2 in d[\"a\"])\n", want: "True\n"},
		{name: "entry_bound_then_len", literal: "x = {\"a\": [1, 2]}[\"a\"]\nprint(len(x))\n", variable: "d = {\"a\": [1, 2]}\nx = d[\"a\"]\nprint(len(x))\n", want: "2\n"},
		{
			name:     "entry_in_a_call",
			literal:  "def id(v):\n    return v\n\nprint(id({\"a\": \"x\"}[\"a\"]))\n",
			variable: "d = {\"a\": \"x\"}\ndef id(v):\n    return v\n\nprint(id(d[\"a\"]))\n",
			// The reference prints `x`. The variable road prints `1` — the text's @str_tab position — which is
			// why the literal road refuses the same position instead of joining it (roadmap Gap R.146, Gap R.220).
			want: "x\n",
		},
		{
			name:     "entry_returned",
			literal:  "def f():\n    return {\"a\": \"x\"}[\"a\"]\n\nprint(f())\n",
			variable: "d = {\"a\": \"x\"}\ndef f():\n    return d[\"a\"]\n\nprint(f())\n",
			want:     "x\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			literalOut, literalRefused := compiledOutOrRefusal(t, tc.literal)
			variableOut, variableRefused := compiledOutOrRefusal(t, tc.variable)
			switch {
			case !literalRefused && !variableRefused:
				if literalOut != variableOut {
					t.Fatalf("the two roads disagree:\n  literal: %q\n  variable: %q", literalOut, variableOut)
				}
				if literalOut != tc.want {
					t.Fatalf("both roads printed %q, the reference prints %q", literalOut, tc.want)
				}
			case literalRefused && !variableRefused:
				if variableOut == tc.want {
					t.Fatalf("the literal road refuses a program the variable road answers the reference's way:\n  %s", literalOut)
				}
			case !literalRefused && variableRefused:
				if literalOut != tc.want {
					t.Fatalf("the literal road answered %q where the reference prints %q\n%s", literalOut, tc.want, literalOut)
				}
			default:
				// Both refuse: the position is owed work, and both roads say so rather than answering wrong.
			}
		})
	}
}

// The print door: the entry reaches the module's one tag-reading printer. A `%d`-with-printf road is how
// an entry whose value is a text printed its `@str_tab` position — the shape every wrong answer in this
// family had — so the module is checked for the printer and against the formatter.
func TestADictLiteralReadIsNotPrintedByPrintf(t *testing.T) {
	// A text entry is a text, so the text printer answers it; a container entry has no kind the compiler
	// could read off a spelling, so its tag travels with it and the module's one tag-reading printer
	// answers. Neither is written by printf with a numeric format — that is the shape every wrong answer
	// in this family had, the text's @str_tab position where its text should print.
	text := mainRegion(t, "print({\"a\": \"x\"}[\"a\"])\n")
	if !strings.Contains(text, "@rt_dict_get_tagged") {
		t.Errorf("a text entry was read without the pair door:\n%s", text)
	}
	if !strings.Contains(text, "@rt_print_str") && !strings.Contains(text, "@rt_print_mixed_value") {
		t.Errorf("a text entry was not printed by a printer that reads the kind:\n%s", text)
	}
	if strings.Contains(text, "%d") {
		t.Errorf("a text entry was written by a numeric format:\n%s", text)
	}
	container := mainRegion(t, "print({\"a\": [1, 2]}[\"a\"])\n")
	if !strings.Contains(container, "@rt_print_mixed_value") {
		t.Errorf("a container entry was not printed by the tag-reading printer:\n%s", container)
	}
	if strings.Contains(container, "%d") {
		t.Errorf("a container entry was written by a numeric format:\n%s", container)
	}
}

// mainRegion is the part of the module the compiler wrote for this program: the runtime helpers carry
// their own `%d`, because that is how the int arm of the printer works, so a claim about how this program
// prints has to be read off its own body.
func mainRegion(t *testing.T, src string) string {
	t.Helper()
	ir := emittedModuleIR(t, src)
	start := strings.Index(ir, "define i32 @main() {")
	if start < 0 {
		t.Fatalf("the module has no main:\n%s", ir)
	}
	rest := ir[start:]
	if end := strings.Index(rest, "\n}"); end >= 0 {
		return rest[:end]
	}
	return rest
}

// A one-word position cannot carry a tag, so it refuses rather than answering with the payload alone —
// which is what printed `1` for `{"a": "x"}["a"]`. The refusal says which road, which half is missing,
// and which row owns the rest.
func TestTheOneWordPositionsRefuseTheLiteralDictRead(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{
			name: "return_a_text_entry",
			src:  "def f():\n    return {\"a\": \"x\"}[\"a\"]\n\nprint(f())\n",
		},
		{
			name: "pass_a_text_entry",
			src:  "def id(v):\n    return v\n\nprint(id({\"a\": \"x\"}[\"a\"]))\n",
		},
		{
			name: "return_a_none_entry",
			src:  "def f():\n    return {\"a\": None}[\"a\"]\n\nprint(f())\n",
		},
		{
			name: "pass_a_list_entry",
			src:  "def id(v):\n    return v\n\nprint(id({\"a\": [1, 2]}[\"a\"]))\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compile(tc.src)
			if err == nil {
				t.Fatalf("%s compiled; a one-word road cannot carry a (payload, tag) pair (Gap R.146)", tc.name)
			}
			msg := err.Error()
			if !strings.Contains(msg, "codegen:") {
				t.Errorf("%s refused outside codegen: %q", tc.name, msg)
			}
			for _, want := range []string{"(payload, tag) pair", "one word", "Gap R.146"} {
				if !strings.Contains(msg, want) {
					t.Errorf("%s's refusal does not say %q: %q", tc.name, want, msg)
				}
			}
			if strings.Contains(msg, "index must be a constant") {
				t.Errorf("%s's refusal is the old unexplained one: %q", tc.name, msg)
			}
		})
	}
}

// The fold's int payload is not a value on its own: an int entry may take the one-word roads, and a text,
// float, bool or None entry may not. Both halves are pinned, because a test that only checks the refusal
// cannot tell a paid row from a row that stopped answering at all.
func TestAPlainIntEntryStillTakesTheOneWordRoads(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{name: "return_an_int_entry", src: "def f():\n    return {\"a\": 7}[\"a\"]\n\nprint(f())\n", want: "7\n"},
		{name: "pass_an_int_entry", src: "def twice(v):\n    return v + v\n\nprint(twice({\"a\": 3}[\"a\"]))\n", want: "6\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Errorf("printed %q, want %q", got, tc.want)
			}
		})
	}
}

// A key the tag rules cannot type has no arm here: the fold's int payload is not a value on its own, and
// comparing a payload under a guessed tag is how `{0: 1}` answered "the key is there" for `"0"`. The
// sentence names the key it could not type, and the row that owns it.
func TestAnUntypableDictLiteralKeyRefuses(t *testing.T) {
	_, err := Compile("print({(1, 2): 3}[(1, 2)])\n")
	if err == nil {
		t.Fatal("a tuple key compiled; a key with no tag has no arm here (roadmap Gap R.212)")
	}
	msg := err.Error()
	for _, want := range []string{"codegen:", "a tuple literal", "Gap R.215"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the tuple-key refusal does not say %q: %q", want, msg)
		}
	}
	if strings.Contains(msg, "unsupported expression") {
		t.Errorf("the tuple key fell through to the old unexplained refusal: %q", msg)
	}
}
