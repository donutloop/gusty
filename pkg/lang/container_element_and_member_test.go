// Shapes measured while landing ADR 0437 and still NOT fixed: a COMPREHENSION that arrives as an element of a
// literal, and a container that arrives as a member of a set literal. Each row is pinned with today's bytes
// beside the reference's, in this file and in `integration/container_element_and_member_test.go`, and each byte
// below was re-measured on a build of the commit before the family was filed (`4fdb87c`) as well as on this
// one — the sentences changed in that commit, these answers did not.
//
// The family had four doors and one has been paid: the ELEMENT door also refused to describe a container held
// in a NAME (`sa = {1: "a", "b": 2}` / `print([sa, 1])` printed `[1, 1]`), and that one is closed by **ADR
// 0438** — the census that labels the object now asks the tag road's answer, so a named container travels as a
// tagged pair (**Gap R.486**, paid, and its rows moved to `named_container_element_test.go`). What is left here:
//
//   - the ELEMENT door, comprehension half. `heapListFrom` builds a literal list one slot at a time, and a slot
//     is a word plus a tag. When the element is a container the compiler can read — a literal, or a name the
//     container tables describe — the pair comes from the builder that made the object (**ADR 0332**,
//     **ADR 0384**, **ADR 0438**). When the element is a COMPREHENSION, or a comprehension over a call, the road
//     keeps one word and calls it the value, and the word is the allocator's slot: `print([[{"a": 1} for x in
//     range(1)], 1])` prints `[1, 1]` at exit **0** where the reference prints `[[{'a': 1}], 1]`
//     (**Gap R.487**); and the same shape one level deeper — `print([[[x for x in [1]]]])` — puts the
//     compile-time global `@.lst1` in an operand position and `llc` rejects the module, which is exit **2**, the
//     code **ADR 0166** reserves for a compiler bug (**Gap R.488**); and
//   - the MEMBER door. A set literal's members are hashed. A list, a dict or a set has no hash in the
//     reference, which raises `TypeError: unhashable type: 'list'`; this backend files the handle as a member
//     and prints `{[1], [2]}` at exit **0** — **Gap R.81**'s rule (`sa.add([1])` adds what cannot be a member)
//     at the literal door rather than the `add` one (**Gap R.485**).
//
// None of these sources is on the record, and `tools/recmerge` refuses each: the compiled leg's bytes
// are not the reference's, which is the recorder's cross-check earning its keep and the reason a row exists
// rather than a record.
package lang

import (
	"strings"
	"testing"
)

// containerElementFiledRows are the elements that arrive as one word where the answer needs a pair, and the
// one shape among them that reaches `llc` at all. `kind` says which witness the row asks: an ANSWER compared
// to the bytes pinned here, a REFUSAL whose sentence is pinned, or a module that never reaches a running
// program at all (the exit-2 class, where the unit leg can only ask that the assembly fails and names the
// global it choked on — the exit code itself is the CLI twin's job).
func containerElementFiledRows() []struct {
	name      string
	src       string
	kind      string
	today     string
	reference string
	row       string
} {
	return []struct {
		name      string
		src       string
		kind      string
		today     string
		reference string
		row       string
	}{
		{
			name:      "a comprehension as an element answers the slot",
			src:       "print([[{\"a\": 1} for x in range(1)], 1])\n",
			kind:      "answer",
			today:     "[1, 1]\n",
			reference: "[[{'a': 1}], 1]",
			row:       "Gap R.487",
		},
		{
			name:      "a comprehension over a call, as an element, answers the slot",
			src:       "def f():\n    return 1\nprint([[f() for x in range(1)], 2])\n",
			kind:      "answer",
			today:     "[1, 2]\n",
			reference: "[[1], 2]",
			row:       "Gap R.487",
		},
		{
			name:      "the same shape one level deeper reaches llc",
			src:       "print([[[x for x in [1]]]])\n",
			kind:      "module",
			today:     "@.lst1",
			reference: "[[[1]]]",
			row:       "Gap R.488 (the CLI exits 2 — ADR 0166's own-bug code — on `rt_set_elem(i32 %t, i32 0, i32 @.lst1)`)",
		},
	}
}

// containerMemberFiledRows are the set members the language refuses to hash, which this backend accepts.
func containerMemberFiledRows() []struct {
	name      string
	src       string
	kind      string
	today     string
	reference string
	row       string
} {
	return []struct {
		name      string
		src       string
		kind      string
		today     string
		reference string
		row       string
	}{
		{
			name:      "a set literal accepts a list member",
			src:       "print({[1], [2]})\n",
			kind:      "answer",
			today:     "{[1], [2]}\n",
			reference: "raises TypeError: unhashable type: 'list'",
			row:       "Gap R.485 (the literal door; Gap R.81 owns the `add` door)",
		},
		{
			name:      "a set literal accepts a dict member",
			src:       "sa = {1: \"a\", \"b\": 2}\nprint({sa})\n",
			kind:      "answer",
			today:     "{{1: 'a', 'b': 2}}\n",
			reference: "raises TypeError: unhashable type: 'dict'",
			row:       "Gap R.485",
		},
		{
			name:      "a set literal accepts a set member",
			src:       "print({{1}})\n",
			kind:      "answer",
			today:     "{{1}}\n",
			reference: "raises TypeError: unhashable type: 'set'",
			row:       "Gap R.485",
		},
	}
}

func compiledAnswerOrRefusal(t *testing.T, src string) (string, int) {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		return "REFUSED: " + err.Error(), 1
	}
	out := runIR(t, res.IR)
	return out, 0
}

func TestWhatTheContainerElementAndMemberDoorsStillOweIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	rows := append(containerElementFiledRows(), containerMemberFiledRows()...)
	for _, tc := range rows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.kind == "module" {
				// The module never becomes a program: the assembly is what fails, and the name it names is the
				// compile-time global that has no business being an operand (ADR 0188's rule, ADR 0166's code).
				_, err := JIT(tc.src, 0)
				if err == nil {
					t.Fatalf("%q assembled and ran, where this row is filed because the module is rejected (%s)", tc.src, tc.row)
				}
				if !strings.Contains(err.Error(), tc.today) {
					t.Errorf("%q failed the assembly without naming %q: %v (%s)", tc.src, tc.today, err, tc.row)
				}
				return
			}
			got, code := compiledAnswerOrRefusal(t, tc.src)
			if tc.kind == "answer" {
				if code != 0 {
					t.Fatalf("%q exited %d, this row pins exit 0 (%s): %s", tc.src, code, tc.row, got)
				}
				if got != tc.today {
					t.Errorf("%q printed %q, this row pins %q — the reference prints %s (%s)", tc.src, got, tc.today, tc.reference, tc.row)
				}
				if HasGoldenAnswer(tc.src) {
					t.Errorf("%q is on the record with an answer, although the compiled leg prints %q and the reference prints %s (%s)", tc.src, got, tc.reference, tc.row)
				}
				return
			}
			if !strings.Contains(got, strings.TrimPrefix(tc.today, "REFUSED: ")) {
				t.Errorf("%q: expected the refusal to contain %q, got %q (%s)", tc.src, strings.TrimPrefix(tc.today, "REFUSED: "), got, tc.row)
			}
			if code == 0 {
				t.Errorf("%q answered %q at exit 0 where this row pins a refusal (%s)", tc.src, got, tc.row)
			}
		})
	}
}

// TestTheContainerElementRowsAreNotSilentlyOnTheRecord is the recorder's rule asked of these rows: an
// answer that is not the reference's cannot be a record, and the row is the only place it can live.
func TestTheContainerElementRowsAreNotSilentlyOnTheRecord(t *testing.T) {
	t.Parallel()
	rows := append(containerElementFiledRows(), containerMemberFiledRows()...)
	for _, tc := range rows {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if HasGoldenAnswer(tc.src) {
				t.Errorf("%q is on the record although it is a filed defect (%s) — the record holds the reference's answer, and this is not it", tc.src, tc.row)
			}
		})
	}
}
