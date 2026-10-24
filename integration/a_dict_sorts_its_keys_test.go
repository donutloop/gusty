// Roadmap Gap R.426 / ADR 0417 at the CLI: `sorted(<dict>)` answers the dict's keys, and the answer is the
// reference's, byte for byte, on the linked binary and under the JIT.
//
// Before this row the door copied the container and sorted the copy — the road that is right for a list and
// wrong for a dict, because a dict's payload array is [key, value, key, value, …] while its length word
// counts ENTRIES. `d = {"a": 1, "b": 2}` / `print(sorted(d))` printed ['a', 1] and the reversed form printed
// [1, 'a'], both at **exit 0**: the sort was handed one key and one value and reported them as the two items
// of a two-entry dict. `for k in d` walked the keys correctly in the same program.
//
// The door now asks the dict its keys through @rt_dict_keys, which strides by two and stops at rt_dict_len,
// and sorts that list; the comparator and the printed kind of a key come from one record of the keys, so the
// order and the printing of a key cannot disagree (ADR 0232's one-table rule, applied to a sort).
//
// Every expectation here is re-derived from a live `python3` inside the case, so a table whose premise
// drifted fails rather than certifies the drift (ADR 0166). The refused rows are checked as refusals: exit
// **2**, in words that name the row owning the rest — never exit 0 with a word the program never wrote, and
// never exit **2** on a module `llc` had to reject (ADR 0006's contract).
package integration

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// sortedDictCLIFamily is the answered family at the CLI. The reference's bytes are asked inside each case;
// these strings are the shape, not the oracle.
func sortedDictCLIFamily() []struct{ name, src string } {
	return []struct{ name, src string }{
		{"the row's own program", "d = {\"a\": 1, \"b\": 2}\nprint(sorted(d))\n"},
		{"the row's program, reversed", "d = {\"a\": 1, \"b\": 2}\nprint(sorted(d, reverse=True))\n"},
		{"keys arrive unordered", "d = {\"b\": 2, \"a\": 1, \"c\": 3}\nprint(sorted(d))\n"},
		{"many keys", "print(sorted({\"pear\": 1, \"apple\": 2, \"fig\": 3, \"banana\": 4}))\n"},
		{"many keys reversed", "print(sorted({\"pear\": 1, \"apple\": 2, \"fig\": 3, \"banana\": 4}, reverse=True))\n"},
		{"whole-number keys", "print(sorted({3: \"c\", 1: \"a\", 2: \"b\"}))\n"},
		{"whole-number keys reversed", "print(sorted({3: \"c\", 1: \"a\", 2: \"b\"}, reverse=True))\n"},
		{"the empty literal", "print(sorted({}))\n"},
		{"the empty dict behind a name", "d = {}\nprint(sorted(d))\n"},
		{"a dict built by item stores", "g = {}\ng[\"q\"] = 1\ng[\"p\"] = 2\nprint(sorted(g))\n"},
		{"a container in a value slot", "print(sorted({\"a\": {\"n\": 1}, \"b\": 2}))\n"},
		{"a list in a value slot", "print(sorted({\"b\": [1], \"a\": [2]}))\n"},
		{"a verdict key", "print(sorted({True: 1, False: 0}))\n"},
		{"one key, no comparison to perform", "print(sorted({None: 1}))\n"},
		{"the sorted keys kept under a name", "d = {\"b\": 2, \"a\": 1}\nxs = sorted(d)\nprint(xs)\nprint(len(xs))\nprint(xs[0])\n"},
		{"keys named rather than written", "a = \"b\"\nb = \"a\"\nprint(sorted({a: 1, b: 2}))\n"},
		{"the walk and the sort, one dict", "d = {\"b\": 2, \"a\": 1, \"c\": 3}\nprint(sorted(d))\nfor k in d:\n    print(k)\n"},
		{"sorting leaves the dict alone", "d = {\"b\": 2, \"a\": 1}\nprint(sorted(d))\nprint(sorted(d))\nfor k in d:\n    print(k)\nprint(d[\"b\"])\n"},
		{"an alias sorted twice", "d = {\"b\": 2, \"a\": 1}\ne = d\nprint(sorted(e))\nprint(sorted(d))\n"},
		{"a list still sorts its elements", "ys = [3, 1, 2]\nxs = sorted(ys)\nprint(xs)\nprint(ys)\n"},
	}
}

// TestSortedOverADictAtTheCLIAnswersItsKeys is the reference leg: every engine and `python3` agree on the
// same bytes at exit 0, and the record leg has an answer for the same source (ADR 0302 — an answer may not
// be certified by the backend that earned it).
func TestSortedOverADictAtTheCLIAnswersItsKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range sortedDictCLIFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			path := writeSrc(t, dir, "sorted_dict.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				t.Skip("no live python3 to ask")
			}
			if !lang.HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			for _, engine := range cliEngines {
				stdout, stderr, code := cliStreams(t, engine, path)
				if code != 0 {
					t.Fatalf("%s: exit %d where the reference prints %q: %s\n%s", engine, code, want, stderr, tc.src)
				}
				if stdout != want {
					t.Fatalf("%s: printed %q, the reference printed %q\n%s", engine, stdout, want, tc.src)
				}
			}
		})
	}
}

// TestTheCorpusProgramSortsADictKeys is the matrix leg: one file, every line the reference's.
func TestTheCorpusProgramSortsADictKeys(t *testing.T) {
	t.Parallel()
	src := readProgramSrc("sorted_dict_keys")
	want, refErr, rerr := lang.PythonRun(src)
	if rerr != nil {
		t.Fatalf("the reference could not answer the corpus program: %v (%s)", rerr, refErr)
	}
	if !lang.HasGoldenAnswer(src) {
		t.Fatal("programs/sorted_dict_keys.gy is not on the record — the record leg cannot check a program that is not on it")
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/sorted_dict_keys.gy")
	if code != 0 {
		t.Fatalf("--file: exit %d: %s", code, stderr)
	}
	if stdout != want {
		t.Fatalf("the corpus program drifted from the reference\n compiled: %q\n    python: %q", stdout, want)
	}
}

// TestTheKeyDoorIsInTheModuleTheAgentSees is the machine leg: an agent auditing a build with --emit-llvm
// sees the same door the compiler decided on, and --verify-llvm certifies the module that calls it.
func TestTheKeyDoorIsInTheModuleTheAgentSees(t *testing.T) {
	t.Parallel()
	src := "d = {\"b\": 2, \"a\": 1}\nprint(sorted(d))\n"
	ir, stderr, code := cliStreams(t, "--emit-llvm", src)
	if code != 0 {
		t.Fatalf("--emit-llvm: exit %d: %s", code, stderr)
	}
	for _, want := range []string{
		"define internal i32 @rt_dict_keys(i32 %h, i32 %kt)",
		"%w = mul i32 %i, 2",
		"call i32 @rt_dict_len(i32 %h)",
		"call i32 @rt_dict_keys(i32 ",
		"call void @rt_sort(i32 ",
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("the emitted module does not show the key door (%s):\n%s", want, ir)
		}
	}
	// The stride this row paid, gone from the program's own block: between the key door and the sort there
	// is no list copy of the dict, which is what used to hand the comparator the entry array's words.
	start := strings.Index(ir, "call i32 @rt_dict_keys(i32 ")
	sortAt := strings.Index(ir, "call void @rt_sort(i32 ")
	if start < 0 || sortAt < 0 || sortAt < start {
		t.Fatalf("the sort does not follow the key door in the module:\n%s", ir)
	}
	if between := ir[start:sortAt]; strings.Contains(between, "rt_list_copy") {
		t.Fatalf("the sort is handed a list copy of the dict rather than the key copy:\n%s", between)
	}
	if v, verr, vcode := cliStreams(t, "--verify-llvm", src); vcode != 0 || !strings.Contains(v, "module verified") {
		t.Errorf("--verify-llvm did not certify the module (exit %d): %s %s", vcode, v, verr)
	}
}

// TestSortingADictRefusesInWordsAtTheCLI is the refused family at the CLI: exit 2, a sentence that names what
// is missing and the row that owns it, and no bytes on stdout — the shape this contract ranks best after a
// correct answer (ADR 0006, ADR 0166).
func TestSortingADictRefusesInWordsAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"a text key and a number key",
			"print(sorted({1: \"x\", \"b\": 2}))\n",
			"of more than one kind",
		},
		{
			"a fractional key",
			"print(sorted({1.5: \"x\", 2.5: \"y\"}))\n",
			"the box table rather than in its slot",
		},
		{
			"a key the front end holds no kind for",
			"x = 3\ny = 1\nprint(sorted({x: 1, y: 2}))\n",
			"is refused",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "sorted_dict.gy", tc.src)
			stdout, stderr, code := cliStreams(t, "--file", path)
			if code != 1 {
				t.Fatalf("exit %d, want the front-end refusal's 1; stdout %q stderr %q\n%s", code, stdout, stderr, tc.src)
			}
			if stdout != "" || !refusesHonestly(stderr) {
				t.Fatalf("the refusal answered %q instead of refusing in words\n%s", stdout, tc.src)
			}
			if !strings.Contains(stderr, tc.want) || !strings.Contains(stderr, "Gap R.429") {
				t.Fatalf("refused with %q, want %q and the row that owns the rest\n%s", stderr, tc.want, tc.src)
			}
		})
	}
}
