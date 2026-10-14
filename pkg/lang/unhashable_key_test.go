package lang

import (
	"strings"
	"testing"
)

// An unhashable key.
//
//     print({"a": 1}[[1]])      # TypeError: unhashable type: 'list'
//     print([1] in {"a": 1})    # TypeError: unhashable type: 'list'
//     d = {}; d[[1]] = 2        # TypeError: unhashable type: 'list'
//
// The compiled program had three answers to that one language rule, none of them the reference's:
// the read said `KeyError: key not found`, the membership test answered `False` at exit 0, and the
// store built the entry and printed `{[1]: 2}`. Roadmap Gap R.217, ADR 0328. The rows below are
// the whole feature: the trap at each of the three dict doors, the soundness rule that a LIST
// haystack does not hash its needle, and the store road still storing what is hashable.

// unhashableKeyRows are programs whose key the language refuses to hash, with the class and the
// sentence the reference raises. Each row was read off CPython 3.11 before it was written here.
var unhashableKeyRows = []struct {
	name    string
	src     string
	class   string
	message string
}{
	{"literal dict read by a list", `print({"a": 1}[[1]])`, "TypeError", "unhashable type: 'list'"},
	{"literal dict read by a dict", `print({"a": 1}[{1: 2}])`, "TypeError", "unhashable type: 'dict'"},
	{"literal dict read by a set", `print({"a": 1}[{1}])`, "TypeError", "unhashable type: 'set'"},
	{"variable dict read by a list", "d = {\"a\": 1}\nprint(d[[1]])", "TypeError", "unhashable type: 'list'"},
	{"variable dict read by a name bound to a list", "x = [1]\nd = {\"a\": 1}\nprint(d[x])", "TypeError", "unhashable type: 'list'"},
	{"membership of a list needle in a dict literal", `print([1] in {"a": 1})`, "TypeError", "unhashable type: 'list'"},
	{"membership of a dict needle in a dict literal", `print({1: 2} in {"a": 1})`, "TypeError", "unhashable type: 'dict'"},
	{"membership of a list needle in a set literal", `print([1] in {1, 2})`, "TypeError", "unhashable type: 'list'"},
	{"membership of a list needle in a set variable", "s = {1, 2}\nprint([1] in s)", "TypeError", "unhashable type: 'list'"},
	{"store under a list key in an empty dict", "d = {}\nd[[1]] = 2\nprint(d)", "TypeError", "unhashable type: 'list'"},
	{"store under a list key in a filled dict", "d = {\"a\": 1}\nd[[1]] = 2", "TypeError", "unhashable type: 'list'"},
	{"store under a dict key", "d = {}\nd[{1: 2}] = 2", "TypeError", "unhashable type: 'dict'"},
	{"store under a set key", "d = {}\nd[{1}] = 2", "TypeError", "unhashable type: 'set'"},
}

func TestAKeyTheLanguageCannotHashNamesItsType(t *testing.T) {
	t.Parallel()
	for _, tt := range unhashableKeyRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tt.src)
			if class != tt.class || message != tt.message {
				t.Fatalf("%q raised %s: %s, want %s: %s", tt.src, class, message, tt.class, tt.message)
			}
		})
	}
}

// The uncaught trap must not also answer: the doors that used to print `False` or `{[1]: 2}` are
// the defect, and a raise that leaves a line behind is a different answer from the reference's.
func TestTheUnhashableKeyRefusalDoesNotAnswerAsWell(t *testing.T) {
	t.Parallel()
	for _, tt := range unhashableKeyRows {
		res, err := JIT(tt.src, 0)
		if err != nil {
			t.Fatalf("%q: jit: %v", tt.src, err)
		}
		if res.Code != 3 {
			t.Fatalf("%q exited %d, want the uncaught-exception exit 3 (stdout %q stderr %q)", tt.src, res.Code, res.Output, res.Stderr)
		}
		if out := strings.TrimSpace(res.Output); out != "" {
			t.Fatalf("%q printed %q on the way to the raise; the reference prints nothing", tt.src, out)
		}
	}
}

// listHaystackRows are the soundness half: a list and a tuple answer membership by walking, so an
// unhashable needle is legal there and the reference answers True. The gate that fires at the dict
// door must not follow the needle into a list haystack — that would turn a working program into a
// raise, which is the worse direction of wrong.
func TestMembershipOfAnUnhashableNeedleInAListStaysATest(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  string
		want string
	}{
		{`print([1] in [[1], [2]])`, "True"},
		{`print([3] in [[1], [2]])`, "False"},
		{`print({1} in [{1}])`, "True"},
		{`print({"a": 1} in [{"a": 1}])`, "True"},
		{"x = [[1]]\nprint([1] in x)", "True"},
		{`print(1 in [1, 2])`, "True"},
		{`print("a" in ["a"])`, "True"},
	} {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, tt.src), "\n"); out != tt.want {
				t.Fatalf("compiled %q, want %q", out, tt.want)
			}
		})
	}
}

// The doors that answer must keep answering: a hashable key, a missing key, a legal store. These
// are the rows a gate that fires too eagerly would break.
func TestTheDictDoorsStillAnswerWhatTheyAnswered(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src  string
		want string
	}{
		{`print({"a": 1}["a"])`, "1"},
		{`print({"a": [1, 2]}["a"])`, "[1, 2]"},
		{`print("a" in {"a": 1})`, "True"},
		{`print(1 in {"a": 1})`, "False"},
		{`print(1 in {1, 2})`, "True"},
		{`print(3 in {1, 2})`, "False"},
		{"d = {}\nd[\"k\"] = [1, 2]\nprint(d)", `{'k': [1, 2]}`},
		{"d = {1: 2}\nd[2] = 3\nprint(d[2])", "3"},
		{`print(len({"a": 1, "b": 2}))`, "2"},
	} {
		t.Run(tt.src, func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, tt.src), "\n"); out != tt.want {
				t.Fatalf("compiled %q, want %q", out, tt.want)
			}
		})
	}
}

func TestTheUnhashableKeyTrapIsCatchable(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"try:\n    print({\"a\": 1}[[1]])\nexcept TypeError:\n    print(\"caught\")",
		"try:\n    print([1] in {\"a\": 1})\nexcept TypeError:\n    print(\"caught\")",
		"try:\n    d = {}\n    d[[1]] = 2\n    print(\"stored\")\nexcept TypeError:\n    print(\"caught\")",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			if out := strings.TrimRight(compiledOut(t, src), "\n"); out != "caught" {
				t.Fatalf("%q printed %q, want the handler's line", src, out)
			}
		})
	}
}

// The classifier itself, away from codegen: what the front end reads from the source, including
// the tuple case where the reference names the inner type it choked on rather than the tuple.
func TestTheUnhashableKeyQuestionIsAskedOfTheProgram(t *testing.T) {
	t.Parallel()
	g := &irGen{
		listVars:     map[string]bool{"xs": true},
		runtimeDicts: map[string]bool{"dd": true},
		runtimeSets:  map[string]bool{"ss": true},
	}
	list := Expr(&ListLit{})
	dict := Expr(&DictLit{})
	set := Expr(&SetLit{})
	for _, tt := range []struct {
		e      Expr
		kind   string
		bad    bool
		reason string
	}{
		{list, "list", true, "a list literal"},
		{dict, "dict", true, "a dict literal"},
		{set, "set", true, "a set literal"},
		{&Tuple{Elems: []Expr{&IntLit{Value: 1}, list}}, "list", true, "a tuple carrying a list"},
		{&Tuple{Elems: []Expr{dict, list}}, "dict", true, "the first element the reference chokes on"},
		{&Tuple{Elems: []Expr{&IntLit{Value: 1}, &StrLit{Value: "a"}}}, "", false, "a tuple of hashables"},
		{&Name{Value: "xs"}, "list", true, "a name bound to a list"},
		{&Name{Value: "dd"}, "dict", true, "a name bound to a dict"},
		{&Name{Value: "ss"}, "set", true, "a name bound to a set"},
		{&Name{Value: "mystery"}, "", false, "a name whose kind is only a run-time fact"},
		{&StrLit{Value: "a"}, "", false, "a string, which hashes"},
		{&IntLit{Value: 1}, "", false, "an int, which hashes"},
	} {
		kind, bad := g.unhashableKeyName(tt.e)
		if bad != tt.bad || (bad && kind != tt.kind) {
			t.Fatalf("%s: got (%q, %v), want (%q, %v)", tt.reason, kind, bad, tt.kind, tt.bad)
		}
	}
}

// The haystack half of the gate, also asked directly: only a dict's keys and a set's elements are
// hashed, and a haystack the compiler cannot name stays out of the gate's way.
func TestOnlyADictOrASetNeedlesByHash(t *testing.T) {
	t.Parallel()
	g := &irGen{
		listVars:     map[string]bool{"xs": true},
		runtimeDicts: map[string]bool{"dd": true},
		runtimeSets:  map[string]bool{"ss": true},
		mixedDicts:   map[string]bool{"md": true},
		mixedSets:    map[string]bool{"ms": true},
	}
	for _, tt := range []struct {
		e    Expr
		want bool
	}{
		{&DictLit{}, true},
		{&SetLit{}, true},
		{&ListLit{}, false},
		{&Tuple{}, false},
		{&Name{Value: "dd"}, true},
		{&Name{Value: "md"}, true},
		{&Name{Value: "ss"}, true},
		{&Name{Value: "ms"}, true},
		{&Name{Value: "xs"}, false},
		{&Name{Value: "unknown"}, false},
		{&Index{Obj: &Name{Value: "rows"}, Idx: &IntLit{Value: 0}}, false},
	} {
		if got := g.haystackHashesKeys(tt.e); got != tt.want {
			t.Fatalf("haystackHashesKeys(%T) = %v, want %v", tt.e, got, tt.want)
		}
	}
}

// The runtime sentence, so the words are pinned where the reference's are.
func TestTheUnhashableKeySentence(t *testing.T) {
	t.Parallel()
	if got := unhashableKeyMessage("list"); got != "unhashable type: 'list'" {
		t.Fatalf("the sentence is %q; CPython's is \"unhashable type: 'list'\"", got)
	}
	if got := unhashableKeyRefusal("tuple key", "list"); !strings.Contains(got, "unhashable type: 'list'") || !strings.Contains(got, "Gap R.217") {
		t.Fatalf("the refusal is %q; it must name the sentence and the row", got)
	}
}

// The record leg: the retired engine's own answer for each trap source, so a later cycle that
// deletes the gate cannot delete the evidence with it.
func TestTheUnhashableKeyRowsAreRecorded(t *testing.T) {
	t.Parallel()
	for _, tt := range unhashableKeyRows {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := goldenLookup(t, tt.src)
			if rec.ExnType != tt.class || rec.ExnMsg != tt.message {
				t.Fatalf("the record says %q raises %s: %s, the gate raises %s: %s — the reference is the referee",
					tt.src, rec.ExnType, rec.ExnMsg, tt.class, tt.message)
			}
		})
	}
}

// The module, not just the answer. The store gate branches out of the block it sits in and reopens the
// continuation — the same shape `checkKeyRead` uses — so the two things that could go wrong are a module
// that no longer stores what it used to store, and a module that does not verify at all. `Compile` runs
// the module past LLVM's verifier, so the assertion below is about both.
func TestTheStoreGateKeepsAModuleThatStoresAndVerifies(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		src  string
		want string // a call the module must still make
		deny string // a call the module must no longer make
	}{
		{"a refused store still emits the (dead) store road", "d = {}\nd[[1]] = 2", "@rt_dict_put", ""},
		{"a legal store keeps its store", "d = {}\nd[\"k\"] = 2", "@rt_dict_put", ""},
		{"a refused membership keeps the needle lowering", "print([1] in {\"a\": 1})", "@rt_dict_has", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ir := emittedModuleIR(t, tt.src)
			if !strings.Contains(ir, tt.want) {
				t.Fatalf("the module no longer contains %s", tt.want)
			}
			if tt.deny != "" && strings.Contains(ir, tt.deny) {
				t.Fatalf("the module contains %s, which it must not", tt.deny)
			}
			if !strings.Contains(ir, "TypeError") && !strings.Contains(ir, "unhashable") {
				t.Fatalf("the refusal is not in the module at all:\n%s", ir)
			}
		})
	}
}
