package lang

import (
	"strings"
	"testing"
)

// mixed_dict_set_test.go — the second and third containers of roadmap L11.1 (1b), and the rule
// that makes all three safe to read: a slot's meaning is its payload *and* its tag, and every
// write in the emitter that reaches a container word writes both (ADR 0232).
//
// The tests were written against the behavior, not the implementation: each one states a program
// and what the module must therefore contain (or refuse with which words), because the last time
// a container was "supported" it was supported by codegen bookkeeping that the runtime never saw.

func compileOrFatal(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile(%q): %v", src, err)
	}
	if !strings.Contains(res.IR, "define i32 @main") {
		t.Fatalf("module for %q has no main", src)
	}
	return res.IR
}

// The element-kind gate for dicts and sets. It answers the same question taggableMixedList asks
// of a list — can every slot describe itself? — and refuses the kinds no tag can carry yet.
func TestTaggableMixedDictAndSetAskTheSameQuestion(t *testing.T) {
	g := &irGen{
		dictKeyStr: map[string]bool{}, dictKeyInt: map[string]bool{},
		dictValStr: map[string]bool{}, dictValInt: map[string]bool{},
		setElemStr: map[string]bool{}, setElemInt: map[string]bool{},
		mixedDicts: map[string]bool{}, mixedSets: map[string]bool{},
	}
	for _, tc := range []struct {
		src    string
		dictOK bool
		setOK  bool
	}{
		{`{"a": 1, "b": "x"}`, true, false}, // values mix
		{`{1: "x", "k": 2}`, true, false},   // keys mix
		{`{"a": 1, "b": 2}`, false, false},  // one kind each side is not a mix
		{`{"a": 1, "b": 1.5}`, true, false}, // a float value has a word now: the handle of a float box
		{`{"a": 1, "b": [1]}`, true, false}, // a nested value is a handle: the tag says so
		{`{1, "a"}`, false, true},           // members mix
		{`{1, "a", None}`, false, true},     // three kinds, all describable
		{`{1, True}`, false, true},          // a bool member has no untagged spelling of its kind (Gap R.112)
		{`{1, 2}`, false, false},            // one kind is not a mix
		{`{1, 1.5}`, false, true},           // a float member's payload is a box handle (ADR 0233)
		{`{1, [1]}`, false, true},           // a nested member is a handle too
		{`{1.5: "a"}`, true, false},         // a float key forces the tagged path too
	} {
		prog, err := parseProgram(tc.src)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.src, err)
		}
		expr, ok := prog.Stmts[0].(*ExprStmt)
		if !ok {
			t.Fatalf("%q is not an expression statement", tc.src)
		}
		switch lit := expr.Expr.(type) {
		case *DictLit:
			if got := g.taggableMixedDict(lit); got != tc.dictOK {
				t.Errorf("taggableMixedDict(%s) = %v, want %v", tc.src, got, tc.dictOK)
			}
		case *SetLit:
			if got := g.taggableMixedSet(lit); got != tc.setOK {
				t.Errorf("taggableMixedSet(%s) = %v, want %v", tc.src, got, tc.setOK)
			}
		default:
			t.Fatalf("%q is neither a dict nor a set literal", tc.src)
		}
	}
}

// A mixed dict writes both halves of every entry, and says so on the object: bit 8 is what tells
// the printer and the runtime lookups that the slots describe themselves.
func TestMixedDictLiteralTagsEverySlot(t *testing.T) {
	ir := compileOrFatal(t, "d = {\"a\": 1, \"b\": \"x\", \"c\": None, 7: 2}\nprint(d)\n")
	for _, want := range []string{
		"call void @rt_dict_put_tagged(",
		"@rt_mark_estr(i32 %h",
		"call void @rt_dict_print_mixed(i32 %h",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("module is missing %q\nput lines: %v", want, irLinesContaining(ir, "rt_dict_put"))
		}
	}
	if strings.Contains(ir, "call void @rt_dict_put(i32") {
		t.Errorf("a mixed dict took the untagged put: %v", irLinesContaining(ir, "call void @rt_dict_put(i32"))
	}
}

func TestMixedSetLiteralTagsEveryMember(t *testing.T) {
	ir := compileOrFatal(t, "s = {1, \"a\", None}\nprint(s)\n")
	for _, want := range []string{
		"call void @rt_set_add_tagged(",
		"call void @rt_set_print_mixed(i32 %h",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("module is missing %q\nadd lines: %v", want, irLinesContaining(ir, "rt_set_add"))
		}
	}
}

// A read of a mixed dict produces the pair; printing dispatches on the tag, which is the only
// thing that can tell the string "x" from whatever number its index happens to be.
func TestMixedDictReadCarriesTheValueTag(t *testing.T) {
	ir := compileOrFatal(t, "d = {\"a\": 1, \"b\": \"x\"}\nprint(d[\"b\"])\nv = d[\"a\"]\nprint(v)\n")
	for _, want := range []string{
		"call i32 @rt_dict_get_tagged(",
		"call i32 @rt_dict_value_tag(",
		"call void @rt_print_mixed_value(i32 %t",
		"%_v_tag = alloca i32",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("module is missing %q\ndict reads: %v", want, irLinesContaining(ir, "rt_dict_"))
		}
	}
}

// Iterating a mixed container binds the loop variable's tag beside it; comparing against it has
// to consult that tag or `x == 1` is true for the string whose index is 1.
//
// The comparison used to go to `rt_mixed_eq`, which compared the two words once the tags matched —
// and two float slots holding the same number then answered unequal, because the words were two
// different box handles. One equality now answers every slot question, the one the container
// printers and lookups already ask: `rt_payload_eq` (roadmap L11.1, Gap R.79).
func TestMixedContainerIterationComparesTheTag(t *testing.T) {
	ir := compileOrFatal(t, "s = {1, \"a\"}\nfor x in s:\n    print(x)\n    if x == \"a\":\n        print(\"hit\")\n")
	for _, want := range []string{
		"call i32 @rt_tag_of(",
		"call i32 @rt_payload_eq(",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("module is missing %q\ntag loads: %v", want, irLinesContaining(ir, "rt_tag_of"))
		}
	}
	if strings.Contains(ir, "@rt_mixed_eq(") {
		t.Errorf("the retired word-for-word comparison is back in the module: %v",
			irLinesContaining(ir, "rt_mixed_eq"))
	}
}

// The soundness case this whole cycle exists for. Interned strings and integers share one i32
// word, so a comparison that ignores the tag answers questions the program never asked:
// {1: "one"} must not have an entry called "a", and 1 must not be a member of ["a"].
func TestContainerLookupsCompareTheTag(t *testing.T) {
	for _, tc := range []struct {
		src   string
		wants []string
	}{
		{"d = {1: \"one\"}\nprint(d[\"a\"])\n", []string{"call i32 @rt_dict_has_tagged(", "call i32 @rt_dict_get_tagged("}},
		{"d = {\"a\": 1}\nprint(\"a\" in d)\n", []string{"call i32 @rt_dict_has_tagged("}},
		{"s = {1, \"a\"}\nprint(1 in s)\n", []string{"call i32 @rt_set_contains_tagged("}},
		{"xs = [\"a\"]\nprint(1 in xs)\n", []string{"call i32 @rt_contains_tagged("}},
	} {
		ir := compileOrFatal(t, tc.src)
		for _, want := range tc.wants {
			if !strings.Contains(ir, want) {
				t.Errorf("%q did not ask %s\nlookup lines: %v", tc.src, want, irLinesContaining(ir, "call i32 @rt_"))
			}
		}
	}
}

// Growing a mixed container keeps the promise: the new member or entry travels with its tag.
func TestMixedContainerMutationCarriesTags(t *testing.T) {
	ir := compileOrFatal(t, "s = {1, \"a\"}\ns.add(\"b\")\ns.discard(1)\nprint(s)\nd = {\"a\": 1}\nd[\"b\"] = \"x\"\nprint(d)\n")
	for _, want := range []string{
		"call void @rt_set_add_tagged(",
		"call void @rt_set_discard_tagged(",
		"call void @rt_dict_put_tagged(",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("module is missing %q\nmutation lines: %v", want, irLinesContaining(ir, "call void @rt_"))
		}
	}
}

// The refusals this cycle deliberately keeps. A refusal is a good outcome when the alternative is
// a word whose meaning nobody can recover; these programs would print a number where the
// interpreter prints a float, or find an entry that was never put.
func TestMixedContainersStillRefuseWhatNoTagDescribes(t *testing.T) {
	for _, tc := range []struct {
		src    string
		wanted string
	}{
		// The nested gate this cycle keeps is the one shape with no rule: a dict keyed by a
		// container has no hashing rule, and building it anyway printed the key as a number —
		// `{[1, 2]: 3}` answered `{1: 3}`, which is a wrong answer about the key.
		{"print({[1, 2]: 3})\n", "cannot hold"},
		// The static dict-literal path gets there first for an int-keyed dict and refuses with its
		// own honest reason; both are front-end refusals, neither is a wrong answer.
		{"d = {[1]: 1}\nprint(d)\n", "constant integer keys only"},
		// A container element read back as a number still has no tag to carry.
		{"xs = [[1, 2], [3]]\nprint(xs[0] + 1)\n", "needs a single static kind"},
	} {
		_, err := Compile(tc.src)
		if err == nil {
			t.Fatalf("%q compiled; want a refusal", tc.src)
		}
		if !strings.Contains(err.Error(), tc.wanted) {
			t.Errorf("%q refused with %q, want it to mention %q", tc.src, err.Error(), tc.wanted)
		}
		if strings.Contains(err.Error(), "LLVM ERROR") || strings.Contains(err.Error(), "verifier") {
			t.Errorf("%q failed as an IR problem instead of a front-end refusal: %v", tc.src, err)
		}
	}
}

// The two float shapes this list carried as refusals (`d = {"a": 1, "b": 1.5}`, `s = {1, 1.5}`) are
// the dicts and sets ADR 0233 unlocked: the value's payload is the handle of a float box, the tag
// says so, and rt_payload_eq compares the boxes by value rather than by handle — which is why
// `1.5 in s` finds the member even though the test's needle is a different box.
func TestFloatValuesInDictsAndSetsAnswer(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"d = {\"a\": 1, \"b\": 1.5}\nprint(d)\n", "{'a': 1, 'b': 1.5}\n"},
		{"s = {1, 1.5}\nprint(s)\n", "{1, 1.5}\n"},
		{"s = {1.5}\nprint(1.5 in s)\n", "True\n"},
		{"s = {1}\nprint(1.0 in s)\n", "True\n"}, // Python: 1.0 is a member of {1}
		{"d = {1.5: \"x\"}\nprint(d[1.5])\n", "x\n"},
		{"d = {1.5: \"x\", \"k\": 2.5, None: 3}\nprint(d)\n", "{1.5: 'x', 'k': 2.5, None: 3}\n"},
	} {
		res, err := Compile(tc.src)
		if err != nil {
			t.Fatalf("%q: Compile: %v", tc.src, err)
		}
		if out := runIR(t, res.IR); out != tc.want {
			t.Errorf("%q ran to %q, want CPython's %q", tc.src, out, tc.want)
		}
	}
}

// A missing key raises the KeyError the record raises — decided by the (payload, tag) pair,
// which is the whole reason the check exists.
func TestMixedDictMissingKeyRaises(t *testing.T) {
	ir := compileOrFatal(t, "d = {0: \"zero\", \"k\": 2}\nprint(d[\"0\"])\n")
	if !strings.Contains(ir, "call i32 @rt_dict_has_tagged(") {
		t.Errorf("the membership check did not consult the tag: %v", irLinesContaining(ir, "rt_dict_has"))
	}
	if !strings.Contains(ir, "KeyError") {
		t.Errorf("a missing key does not raise KeyError: %v", irLinesContaining(ir, "KeyError"))
	}
}

// The uniform paths must stay untouched by all of this: a dict or set of one kind still builds
// through the plain runtime calls and prints through the static printers, so the programs that
// worked before pay nothing for the ones that now work too.
func TestUniformContainersKeepTheirStaticPaths(t *testing.T) {
	ir := compileOrFatal(t, "d = {\"a\": 1, \"b\": 2}\nprint(d)\ns = {1, 2}\nprint(s)\n")
	if !strings.Contains(ir, "call void @rt_dict_put(i32") {
		t.Errorf("a uniform dict lost its plain put: %v", irLinesContaining(ir, "rt_dict_put"))
	}
	if !strings.Contains(ir, "call void @rt_set_add(i32") {
		t.Errorf("a uniform set lost its plain add: %v", irLinesContaining(ir, "rt_set_add"))
	}
	if strings.Contains(ir, "@rt_mark_estr(i32 %h") {
		for _, l := range irLinesContaining(ir, "@rt_mark_estr(i32 %h") {
			if strings.HasSuffix(strings.TrimSpace(l), ", 8)") {
				t.Errorf("a uniform dict was marked as if it mixed kinds: %s", l)
			}
		}
	}
}
