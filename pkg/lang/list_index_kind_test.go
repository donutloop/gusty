package lang

import (
	"strings"
	"testing"
)

// An index that is not a position (roadmap Gap R.222, ADR 0329).
//
//     xs = [1, 2]
//     print(xs["a"])   # TypeError: list indices must be integers or slices, not str
//     print(xs[None])  # TypeError: list indices must be integers or slices, not NoneType
//     s  = "ab"
//     print(s["a"])    # TypeError: string indices must be integers, not 'str'
//
// Every one of those printed an element at exit 0: a text index is an @str_tab index, so the door
// counted to the slot the interned number named. A list used as an index was worse — the literal
// lowered to a @.lstN global reference inside the bounds check's `icmp slt`, llc rejected the module,
// and the toolchain exited 2, the code this repo's contract reserves for a compiler bug. The rows are
// the reference's own sentences (read off CPython 3.11 before being written here), the containers that
// keep answering, and the module the gate lives in.

var badIndexRows = []struct {
	name    string
	src     string
	message string
}{
	{"a text index into a list", "xs = [1, 2]\nprint(xs[\"a\"])", "list indices must be integers or slices, not str"},
	{"a float index into a list", "xs = [1, 2]\nprint(xs[1.5])", "list indices must be integers or slices, not float"},
	{"a nothing index into a list", "xs = [1, 2]\nprint(xs[None])", "list indices must be integers or slices, not NoneType"},
	{"a list index into a list", "xs = [1, 2]\nprint(xs[[0]])", "list indices must be integers or slices, not list"},
	{"a dict index into a list", "xs = [1, 2]\nprint(xs[{1: 2}])", "list indices must be integers or slices, not dict"},
	{"a set index into a list", "xs = [1, 2]\nprint(xs[{1}])", "list indices must be integers or slices, not set"},
	{"a tuple index into a list", "xs = [1, 2]\nprint(xs[(0, 1)])", "list indices must be integers or slices, not tuple"},
	{"a text index into a list literal", `print([1, 2]["a"])`, "list indices must be integers or slices, not str"},
	{"a text index into a string", "s = \"ab\"\nprint(s[\"a\"])", "string indices must be integers, not 'str'"},
	{"a float index into a string", "s = \"ab\"\nprint(s[1.5])", "string indices must be integers, not 'float'"},
	{"a list index into a string", "s = \"ab\"\nprint(s[[0]])", "string indices must be integers, not 'list'"},
	{"a store under a text index", "xs = [1, 2]\nxs[\"a\"] = 1", "list indices must be integers or slices, not str"},
	{"a store under a list index", "xs = [1, 2]\nxs[[0]] = 1", "list indices must be integers or slices, not list"},
	{"an index bound to a text", "xs = [1, 2, 3]\nt = \"a\"\nprint(xs[t])", "list indices must be integers or slices, not str"},
	{"an index bound to a float", "xs = [1, 2, 3]\nf = 1.5\nprint(xs[f])", "list indices must be integers or slices, not float"},
}

func TestAnIndexThatIsNotAPositionNamesItsType(t *testing.T) {
	t.Parallel()
	for _, tt := range badIndexRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != "TypeError" || message != tt.message {
				t.Fatalf("%q raised %s: %s, want TypeError: %s", tt.src, class, message, tt.message)
			}
		})
	}
}

// The trap must be the program's only output: the doors that used to print an element are the defect,
// and a raise that leaves a line behind is a different answer from the reference's.
func TestTheBadIndexRefusalDoesNotAnswerAsWell(t *testing.T) {
	t.Parallel()
	for _, tt := range badIndexRows {
		res, err := JIT(tt.src, 0)
		if err != nil {
			t.Fatalf("%q: jit: %v", tt.src, err)
		}
		if res.Code != 3 {
			t.Fatalf("%q exited %d, want 3 (stdout %q stderr %q)", tt.src, res.Code, res.Output, res.Stderr)
		}
		if out := strings.TrimSpace(res.Output); out != "" {
			t.Fatalf("%q printed %q on the way to the raise; the reference prints nothing", tt.src, out)
		}
	}
}

// A list literal used as an index used to reach the bounds check as a `@.lstN` global reference inside
// an `icmp slt`, and llc turned the module down — exit 2, the toolchain's own failure code. The gate
// runs before the index is lowered, so the bad instruction is never emitted. This is the row that pins
// the toolchain did not merely answer differently: it compiled.
func TestTheListValuedIndexBuildsAModuleInsteadOfBreakingOne(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"xs = [1, 2]\nprint(xs[[0]])",
		"xs = [1, 2]\nprint(xs[{1: 2}])",
		"s = \"ab\"\nprint(s[[0]])",
		"xs = [1, 2]\nxs[[0]] = 1",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("the toolchain failed on %q: %v", src, err)
			}
			if res.Code == 2 {
				t.Fatalf("%q exited 2 — the toolchain's own failure code:\n%s", src, res.Stderr)
			}
			if res.Code != 3 {
				t.Fatalf("%q exited %d, want the uncaught-exception exit 3", src, res.Code)
			}
		})
	}
}

// The containers that still answer: a position is a position, a bool is an int, a slice is a slice,
// and a dict or a set is subscripted by key — the door beside this one (Gap R.217, ADR 0328).
func TestAPositionStillIndexesWhatItNamed(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  string
		want string
	}{
		{"xs = [1, 2]\nprint(xs[1])", "2"},
		{"xs = [1, 2]\nprint(xs[-1])", "2"},
		{"xs = [1, 2]\nprint(xs[True])", "2"},
		{"xs = [1, 2, 3]\nprint(xs[0] + xs[1] + xs[2])", "6"},
		{"xs = [1, 2, 3]\ni = 1\nprint(xs[i])", "2"},
		{"xs = [1, 2, 3]\nprint(xs[0:2])", "[1, 2]"},
		{"xs = [[1], [2]]\nprint(xs[1])", "[2]"},
		{"s = \"ab\"\nprint(s[1])", "b"},
		{"d = {\"a\": 1}\nprint(d[\"a\"])", "1"},
		{"d = {\"a\": 1}\nprint(\"a\" in d)", "True"},
		{"xs = [1, 2]\nxs[0] = 9\nprint(xs[0])", "9"},
		{"xs = [1, 2]\nxs[\"a\"] = 9\nprint(xs[0])", "<never: the store raises>"},
	} {
		if strings.HasPrefix(tt.want, "<") {
			continue // the store rows are traps, asserted above
		}
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, tt.src), "\n"); out != tt.want {
				t.Fatalf("compiled %q, want %q", out, tt.want)
			}
		})
	}
}

func TestTheBadIndexTrapIsCatchable(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"try:\n    xs = [1, 2]\n    print(xs[\"a\"])\nexcept TypeError:\n    print(\"caught\")",
		"try:\n    s = \"ab\"\n    print(s[1.5])\nexcept TypeError:\n    print(\"caught\")",
		"try:\n    xs = [1, 2]\n    xs[\"a\"] = 1\n    print(\"stored\")\nexcept TypeError:\n    print(\"caught\")",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, src), "\n"); out != "caught" {
				t.Fatalf("%q printed %q, want the handler's line", src, out)
			}
		})
	}
}

// The classifier, away from codegen — including the two cases the reference is careful about: a bool
// IS an integer (so `xs[True]` is a position), and nothing is invented for a name whose kind is only a
// run-time fact.
func TestThePositionQuestionIsAskedOfTheProgram(t *testing.T) {
	t.Parallel()
	g := &irGen{
		floatVars:    map[string]bool{"f": true},
		listVars:     map[string]bool{"xs": true},
		runtimeDicts: map[string]bool{"d": true},
		runtimeSets:  map[string]bool{"ss": true},
		internedVars: map[string]bool{"t": true},
		noneVars:     map[string]bool{"nothing": true},
	}
	for _, tt := range []struct {
		e      Expr
		name   string
		bad    bool
		reason string
	}{
		{&IntLit{Value: 3}, "", false, "an int is a position"},
		{&BoolLit{Value: true}, "", false, "a bool is an int"},
		{&StrLit{Value: "a"}, "str", true, "a text"},
		{&FloatLit{Value: 1.5}, "float", true, "a float"},
		{&NoneLit{}, "NoneType", true, "nothing"},
		{&ListLit{}, "list", true, "a list"},
		{&DictLit{}, "dict", true, "a dict"},
		{&SetLit{}, "set", true, "a set"},
		{&Tuple{}, "tuple", true, "a tuple"},
		{&Name{Value: "f"}, "float", true, "a name bound to a float"},
		{&Name{Value: "xs"}, "list", true, "a name bound to a list"},
		{&Name{Value: "t"}, "str", true, "a name bound to a text"},
		{&Name{Value: "d"}, "dict", true, "a name bound to a dict"},
		{&Name{Value: "ss"}, "set", true, "a name bound to a set"},
		{&Name{Value: "nothing"}, "NoneType", true, "a name bound to nothing"},
		{&Name{Value: "mystery"}, "", false, "a name whose kind is only a run-time fact"},
	} {
		name, bad := g.nonIntIndexTypeName(tt.e)
		if bad != tt.bad || (bad && name != tt.name) {
			t.Fatalf("%s: got (%q, %v), want (%q, %v)", tt.reason, name, bad, tt.name, tt.bad)
		}
	}
}

// The two sentences are the reference's, and they are not the same sentence: a list names the type
// bare and a string quotes it.
func TestTheTwoIndexSentences(t *testing.T) {
	t.Parallel()
	if got := badIndexMessage("str", false); got != "list indices must be integers or slices, not str" {
		t.Fatalf("the list sentence is %q", got)
	}
	if got := badIndexMessage("str", true); got != "string indices must be integers, not 'str'" {
		t.Fatalf("the string sentence is %q", got)
	}
	if got := badIndexMessage("NoneType", false); !strings.Contains(got, "NoneType") {
		t.Fatalf("None must be named the way the reference names it: %q", got)
	}
}

// Which container the door is standing in decides the sentence, and a dict or a set is not this
// door's at all.
func TestOnlyTheContainersThatCountAsksThisQuestion(t *testing.T) {
	t.Parallel()
	g := &irGen{
		listVars:     map[string]bool{"xs": true},
		mixedLists:   map[string]bool{"ml": true},
		internedVars: map[string]bool{"s": true},
		runtimeDicts: map[string]bool{"d": true},
		dictKeyStr:   map[string]bool{"d2": true},
		runtimeSets:  map[string]bool{"ss": true},
	}
	for _, tt := range []struct {
		name             string
		isList, isString bool
	}{
		{"xs", true, false},
		{"ml", true, false},
		{"s", false, true},
		{"d", false, false},
		{"d2", false, false},
		{"ss", false, false},
		{"unknown", false, false},
	} {
		l, str := g.containerCountsPositions(tt.name)
		if l != tt.isList || str != tt.isString {
			t.Fatalf("%s: got (list %v, string %v), want (%v, %v)", tt.name, l, str, tt.isList, tt.isString)
		}
	}
}

// The record leg: the retired engine's answer for each trap source, so the gate cannot be deleted
// without deleting evidence the suite reads.
func TestTheBadIndexRowsAreRecorded(t *testing.T) {
	t.Parallel()
	for _, tt := range badIndexRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.ExnType != "TypeError" || rec.ExnMsg != tt.message {
				t.Fatalf("the record says %q raises %s: %s; the gate raises TypeError: %s — the reference is the referee",
					tt.src, rec.ExnType, rec.ExnMsg, tt.message)
			}
		})
	}
}
