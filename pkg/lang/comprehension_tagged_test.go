package lang

import (
	"sort"
	"strings"
	"testing"
)

// comprehension_tagged_test.go — the set and dict comprehension roads now have both paths the list
// road has: the fold for what it can name, and the tagged builder for everything else (roadmap Gap
// R.353, ADR 0384).
//
// The rows below are chosen to keep the two paths from drifting apart. A constant element still folds
// and still emits the same static container; a call, None, a verdict, a float, a text or a container
// does not, goes through `rt_set_add_tagged`/`rt_dict_put_tagged`, and carries the tag that says what
// its payload means. The worst shape in the family is not the refusal — it is the answer that looks
// like a number at exit 0.

// TestTheComprehensionFoldStillOwnsWhatItCanName keeps the fast path from being "fixed" into the
// runtime builder. A comprehension the folder can evaluate is the same literal it always was.
func TestTheComprehensionFoldStillOwnsWhatItCanName(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, door, needle string
	}{
		{
			"set of integer expressions",
			"print({x * 2 for x in [1, 2, 2]})\n",
			"folded set", "@.set",
		},
		{
			"dict of integer expressions",
			"print({x: x * 2 for x in [1, 2]})\n",
			"folded dict", "@.dict",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ir := compileSrcIR(t, tc.src)
			if !strings.Contains(ir, tc.needle) {
				t.Fatalf("%q did not emit its %s (%s):\n%s", tc.src, tc.name, tc.needle,
					strings.Join(irLinesContaining(ir, tc.needle), "\n"))
			}
			if got := compiledOut(t, tc.src); got == "" {
				t.Fatalf("%q printed nothing", tc.src)
			}
		})
	}
}

// TestASetOrDictComprehensionWithAComputedMemberAnswersWhatTheReferenceAnswers is the row's headline:
// the same call in a list element has answered since Gap R.348; a set or dict member/value now answers
// through the door beside it (roadmap Gap R.353).
func TestASetOrDictComprehensionWithAComputedMemberAnswersWhatTheReferenceAnswers(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			"set lambda element",
			"print({(lambda z: z + x)(1) for x in [10, 20]})\n",
			"{11, 21}\n",
		},
		{
			"dict lambda value",
			"print({x: (lambda z: z + x)(1) for x in [10, 20]})\n",
			"{10: 11, 20: 21}\n",
		},
		{
			"set def element",
			"def f(v):\n    return v * 2\nprint({f(x) for x in [1, 2]})\n",
			"{2, 4}\n",
		},
		{
			"dict def value",
			"def f(v):\n    return v * 2\nprint({x: f(x) for x in [1, 2]})\n",
			"{1: 2, 2: 4}\n",
		},
		{
			"runtime list over a def",
			"def f(v):\n    return v + 1\nxs = []\nxs.append(3)\nxs.append(4)\nprint({f(x) for x in xs})\n",
			"{4, 5}\n",
		},
		{
			"dict value is a text computed by a method",
			"xs = [\"a\", \"b\"]\nprint({x: x.upper() for x in xs})\n",
			"{'a': 'A', 'b': 'B'}\n",
		},
		{
			"dict value is a float expression",
			"xs = []\nxs.append(6)\nprint({6: xs[0] / 2 for x in [1]})\n",
			"{6: 3.0}\n",
		},
		{
			"dict value is a dict entry whose key is text",
			"d = {\"k\": v for v in [1, 2, 3]}\nprint(d)\nprint(d[\"k\"], len(d))\n",
			"{'k': 3}\n3 1\n",
		},
		{
			"dict value is a list",
			"print({x: [x] for x in [1, 2]})\n",
			"{1: [1], 2: [2]}\n",
		},
		{
			"dict value is a set",
			"print({x: {x} for x in [1, 2]})\n",
			"{1: {1}, 2: {2}}\n",
		},
		{
			"set built by a filter over a runtime list",
			"def p(v):\n    return v > 1\nxs = [1, 2, 3]\nprint({x * 10 for x in xs if p(x)})\n",
			"{20, 30}\n",
		},
		{
			"dict built by a filter over a runtime list",
			"def p(v):\n    return v > 1\nxs = [1, 2, 3]\nprint({x: x + 100 for x in xs if p(x)})\n",
			"{2: 102, 3: 103}\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Compile(tc.src); err != nil {
				t.Fatalf("refused: %v", err)
			}
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Fatalf("compiled = %q, want %q", got, tc.want)
			}
			if got, _, err := PythonRun(tc.src); err != nil || got != tc.want {
				t.Fatalf("reference = %q (%v), want %q", got, err, tc.want)
			}
		})
	}
}

// TestAComprehensionOfVoidVerdictsTextsAndContainersAnswersWithItsTags is the bad-answer half: the old
// fold could answer None with the integer its truthiness is, True with 1, a container with its table
// index, and a dict value with none of those. A tag says what the payload means (roadmap Gaps R.105,
// R.116 and R.353; ADR 0244, ADR 0259, ADR 0384).
func TestAComprehensionOfVoidVerdictsTextsAndContainersAnswersWithItsTags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want, irNeedle string
	}{
		{
			"None in a set",
			"print({None for x in [1]})\n",
			"{None}\n",
			"@rt_set_add_tagged",
		},
		{
			"None in a dict value",
			"print({1: None for x in [1]})\n",
			"{1: None}\n",
			"@rt_dict_put_tagged",
		},
		{
			"verdict in a set",
			"print({True for x in [1]})\n",
			"{True}\n",
			"@rt_set_add_tagged",
		},
		{
			"verdict in a dict value",
			"print({1: True for x in [1]})\n",
			"{1: True}\n",
			"@rt_dict_put_tagged",
		},
		{
			"verdict key in a dict",
			"print({True: 1 for x in [1]})\n",
			"{True: 1}\n",
			"@rt_dict_put_tagged",
		},
		{
			"float member",
			"xs = []\nxs.append(1.5)\nprint({x for x in xs})\n",
			"{1.5}\n",
			"@rt_set_add_tagged",
		},
		{
			"list-valued dict value",
			"print({x: [x] for x in [1, 2]})\n",
			"{1: [1], 2: [2]}\n",
			"@rt_dict_put_tagged",
		},
		{
			"set-valued dict value",
			"print({x: {x} for x in [1, 2]})\n",
			"{1: {1}, 2: {2}}\n",
			"@rt_dict_put_tagged",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ir := compileSrcIR(t, tc.src)
			if !strings.Contains(ir, tc.irNeedle) {
				t.Fatalf("%q did not write its slot through %s:\n%s", tc.src, tc.irNeedle,
					strings.Join(irLinesContaining(ir, "rt_"), "\n"))
			}
			if got := compiledOut(t, tc.src); got != tc.want {
				t.Fatalf("compiled = %q, want %q", got, tc.want)
			}
			if got, _, err := PythonRun(tc.src); err != nil || got != tc.want {
				t.Fatalf("reference = %q (%v), want %q", got, err, tc.want)
			}
		})
	}
}

// TestASetOfTextsComputedByAMethodAnswersTheSameMembersAsTheReference asks a different question from the
// table above: CPython randomises string hashes, so the ORDER of a text set's repr is not a fact to pin.
// The members and their tags are (roadmap Gap R.353).
func TestASetOfTextsComputedByAMethodAnswersTheSameMembersAsTheReference(t *testing.T) {
	t.Parallel()
	src := "xs = [\"a\", \"b\"]\nprint({x.upper() for x in xs})\n"
	got := compiledOut(t, src)
	want, _, err := PythonRun(src)
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	if gotSet := sortedSetReprMembers(got); gotSet != "'A', 'B'" || sortedSetReprMembers(want) != gotSet {
		t.Fatalf("compiled = %q (%q), reference = %q (%q)", got, gotSet, want, sortedSetReprMembers(want))
	}
}

func sortedSetReprMembers(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return ""
	}
	parts := strings.Split(s, ", ")
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// TestEveryTaggedComprehensionAnswerIsOnTheRecord asks the record for every computed answer this cycle
// added. A missing entry fails: the answer is not merely something `gustyc` happens to print today, and
// deleting its record cannot delete the coverage (roadmap Gap R.204, ADR 0384).
func TestEveryTaggedComprehensionAnswerIsOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"set lambda member", "print({(lambda z: z + x)(1) for x in [10, 20]})\n", "{11, 21}\n"},
		{"dict lambda value", "print({x: (lambda z: z + x)(1) for x in [10, 20]})\n", "{10: 11, 20: 21}\n"},
		{"set def member", "def f(v):\n    return v * 2\nprint({f(x) for x in [1, 2]})\n", "{2, 4}\n"},
		{"dict def value", "def f(v):\n    return v * 2\nprint({x: f(x) for x in [1, 2]})\n", "{1: 2, 2: 4}\n"},
		{"runtime list source", "def f(v):\n    return v + 1\nxs = []\nxs.append(3)\nxs.append(4)\nprint({f(x) for x in xs})\n", "{4, 5}\n"},
		{"None member", "print({None for x in [1]})\n", "{None}\n"},
		{"None value", "print({1: None for x in [1]})\n", "{1: None}\n"},
		{"verdict member", "print({True for x in [1]})\n", "{True}\n"},
		{"verdict value", "print({1: True for x in [1]})\n", "{1: True}\n"},
		{"text key dict", "d = {\"k\": v for v in [1, 2, 3]}\nprint(d)\nprint(d[\"k\"], len(d))\n", "{'k': 3}\n3 1\n"},
		{"float value", "xs = []\nxs.append(6)\nprint({v: v / 2 for v in xs})\n", "{6: 3.0}\n"},
		{"text method value", "xs = [\"a\", \"b\"]\nprint({x: x.upper() for x in xs})\n", "{'a': 'A', 'b': 'B'}\n"},
		{"list value", "print({x: [x] for x in [1, 2]})\n", "{1: [1], 2: [2]}\n"},
		{"set value", "print({x: {x} for x in [1, 2]})\n", "{1: {1}, 2: {2}}\n"},
		{"folded set", "print({x * 2 for x in [1, 2, 2]})\n", "{2, 4}\n"},
		{"folded dict", "print({x: x * 2 for x in [1, 2]})\n", "{1: 2, 2: 4}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := runGoldenStdout(t, tc.src)
			if err != nil {
				if err == errGoldenSkipped {
					t.Fatalf("this program is not on the record leg — record it with tools/recmerge\nsrc: %s", tc.src)
				}
				t.Fatalf("the record leg trapped a program this row answers: %v\nsrc: %s", err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("record = %q, want %q\nsrc: %s", got, tc.want, tc.src)
			}
		})
	}
}

// TestEveryTaggedComprehensionRaiseIsOnTheRecord is the same guard for the hashable positions. The
// reference raises these, the record says they raise, and a future builder must not make them answer.
func TestEveryTaggedComprehensionRaiseIsOnTheRecord(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"list set member", "print({[1] for x in [1]})\n", "TypeError: unhashable type: 'list'"},
		{"set set member", "print({{1} for x in [1]})\n", "TypeError: unhashable type: 'set'"},
		{"dict set member", "print({{1: 2} for x in [1]})\n", "TypeError: unhashable type: 'dict'"},
		{"list dict key", "print({[1]: 1 for x in [1]})\n", "TypeError: unhashable type: 'list'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := RecordedRunError(t, tc.src)
			if err == nil {
				t.Fatalf("the record answered an unhashable program the reference raises\nsrc: %s", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("record trap = %q, want %q\nsrc: %s", err, tc.want, tc.src)
			}
		})
	}
}

// TestAnUnhashableComprehensionMemberOrKeyRaisesTheReferenceSentence makes the one position that must
// be hashable ask the tag before the bucket. A member/key that cannot be hashed leaves through the
// program's `except`, not through `llc` (roadmap Gap R.353, beside ADR 0310 and ADR 0311).
func TestAnUnhashableComprehensionMemberOrKeyRaisesTheReferenceSentence(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		src, class, message, catch string
	}{
		{"print({[1] for x in [1]})\n", "TypeError", "unhashable type: 'list'", "TypeError"},
		{"print({{1} for x in [1]})\n", "TypeError", "unhashable type: 'set'", "TypeError"},
		{"print({[1]: 1 for x in [1]})\n", "TypeError", "unhashable type: 'list'", "TypeError"},
	} {
		t.Run(strings.TrimSpace(tc.src), func(t *testing.T) {
			t.Parallel()
			class, message := jitTrap(t, tc.src)
			if class != tc.class || message != tc.message {
				t.Fatalf("raised %s: %s, want %s: %s", class, message, tc.class, tc.message)
			}
			prog := "try:\n    " + strings.ReplaceAll(tc.src, "\n", "\n    ") + "\nexcept " + tc.catch + ":\n    print(\"caught\")"
			if got := strings.TrimRight(compiledOut(t, prog), "\n"); got != "caught" {
				t.Fatalf("the raise is not catchable from the program's own handler: %q", got)
			}
		})
	}
}

// TestASetComprehensionOpeningWithAContainerLiteralIsNotStolenByItsInnerBrace is Gap R.357's parser
// half. The `for` after the inner `}` belongs to the display whose brace is still open; if it is stolen,
// the outer expression is a literal containing the inner object and the untagged builder emits a global
// into an i32 slot — the module `llc` rejects (roadmap Gap R.357, closed with ADR 0384).
func TestASetComprehensionOpeningWithAContainerLiteralIsNotStolenByItsInnerBrace(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		src     string
		want    string
		wantErr string
	}{
		{
			name:    "set literal member",
			src:     "print({{1} for x in [1]})\n",
			wantErr: "unhashable type: 'set'",
		},
		{
			name:    "dict literal member",
			src:     "print({{1: 2} for x in [1]})\n",
			wantErr: "unhashable type: 'dict'",
		},
		{
			name: "list literal member in a list comprehension",
			src:  "print([{1} for x in [1]])\n",
			want: "[{1}]\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Compile(tc.src); err != nil {
				t.Fatalf("refused: %v", err)
			}
			if tc.want != "" {
				if got := compiledOut(t, tc.src); got != tc.want {
					t.Fatalf("compiled = %q, want %q", got, tc.want)
				}
				return
			}
			class, message := jitTrap(t, tc.src)
			if class != "TypeError" || message != tc.wantErr {
				t.Fatalf("compiled = %s: %s, want TypeError: %s", class, message, tc.wantErr)
			}
		})
	}
}
