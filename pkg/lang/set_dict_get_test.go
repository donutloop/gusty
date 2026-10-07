// Set-literal deduplication and the kind a `dict.get` answers with (roadmap Gap R.180 and Gap
// R.181, owner L11.1; ADR 0291's void arrangement, ADR 0257's verdict rule, ADR 0166's exit codes).
//
// Both rows are compiled-leg wrong numbers at exit 0 that the record never had:
//
//	len({1, 2, 2, 3})            reference 3, --aot 4      the static global counted source elements
//	{1: "a"}.get(1)              reference a, --aot 0      the interned index printed through %d
//	{k: None}.get("k")           reference None, --aot 0   the void word printed, not rendered
//	{1: True}.get(1)             reference True, --aot 1   the verdict word printed, not rendered
//
// A set that counts duplicates is a list wearing braces; a `get` that prints 0 has lost the kind of
// the slot it read.
package lang

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The set literal's COUNT is the number of distinct members, not the number the source spelled.
func TestSetLiteralDeduplicatesItsElements(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`print(len({1, 2, 2, 3}))`, `3`},
		{`print(len({1, 1, 1}))`, `1`},
		{`print(len({1, 2}))`, `2`},
		{`print(len({5, 3, 3, 3, 1}))`, `3`},
		{`print(len({1, 2}) + len({3, 3}))`, `3`},
		{`print({1, 2, 2, 3})`, `{1, 2, 3}`},
		{`print({1, 1} == {1})`, `True`},
		{`print(2 in {1, 2, 2, 3})`, `True`},
		{`print(9 in {1, 2, 2, 3})`, `False`},
	} {
		t.Run(strings.ReplaceAll(tc.src, " ", ""), func(t *testing.T) {
			want := strings.TrimSpace(referenceOut(t, tc.src))
			if want == "" || strings.Contains(want, "Error") {
				t.Fatalf("the reference did not answer: %q", want)
			}
			if want != tc.want {
				t.Fatalf("this pin disagrees with the reference: %q vs %q", tc.want, want)
			}
			got := strings.TrimSpace(compiledOut(t, tc.src))
			if got != want {
				t.Fatalf("%q: compiled %q, reference %q", tc.src, got, want)
			}
		})
	}
}

// The count a set global CARRIES is the deduplicated one; a global that stores 4 and prints 3 is a
// set whose length disagrees with its own rendering, which is what this bug was.
func TestSetGlobalCountAndElementsAgree(t *testing.T) {
	res, err := Compile(`print(len({1, 2, 2, 3}))`)
	if err != nil {
		t.Fatalf("the reference answers this at exit 0: %v", err)
	}
	var count string
	for _, line := range strings.Split(res.IR, "\n") {
		if strings.Contains(line, "= private global {i32, [") && strings.Contains(line, "@.set") {
			count = line
		}
	}
	if count == "" {
		t.Skip("no static set global emitted — the compiled leg took the heap path")
	}
	if strings.Contains(count, "[4 x i32]") {
		t.Fatalf("the set global still reserves one slot per SOURCE element:\n%s", count)
	}
	if !strings.Contains(count, "[3 x i32] [i32 1, i32 2, i32 3]") {
		t.Fatalf("expected one slot per DISTINCT member, in insertion order:\n%s", count)
	}
}

// What a `dict.get` ANSWERS with is the slot's kind, and print must ask. Four kinds, four roads that
// had each lost it.
func TestDictGetAnswersTheKindItsSlotHas(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		// a text slot — the interned index used to print through %d as `0`
		{`print({1: "a"}.get(1))`, `a`},
		{`print({1: "a", 2: "b"}.get(1, "z"))`, `a`},
		{`print({"a": "hi"}.get("a"))`, `hi`},
		// a default — the same fold's other arm
		{`print({1: "a", 2: "b"}.get(9, "z"))`, `z`},
		{`print({1: "a"}.get(9, "fallback"))`, `fallback`},
		// a void slot — the word 0 used to print rather than render (Gap R.171's print-side rule)
		{`print({"k": None}.get("k"))`, `None`},
		{`print({"k": None}.get("zz", None))`, `None`},
		// a verdict slot — the word 1 used to print rather than render (ADR 0257's rule)
		{`print({1: True}.get(1))`, `True`},
		{`print({"k": False}.get("k"))`, `False`},
		{`print({1: True}.get(9, True))`, `True`},
		// an int slot, which already worked and must keep working
		{`print({1: 2}.get(1))`, `2`},
		{`print({1: "a", 2: 3}.get(2))`, `3`},
		// a missing key with no default, which is Gap R.174's pinned half
		{`print({1: "a"}.get(9))`, `None`},
	} {
		t.Run(strings.ReplaceAll(tc.src, " ", ""), func(t *testing.T) {
			want := strings.TrimSpace(referenceOut(t, tc.src))
			if want == "" || strings.Contains(want, "Error") {
				t.Fatalf("the reference did not answer: %q", want)
			}
			if want != tc.want {
				t.Fatalf("this pin disagrees with the reference: %q vs %q", tc.want, want)
			}
			got := strings.TrimSpace(compiledOut(t, tc.src))
			if got != want {
				t.Fatalf("%q: compiled %q, reference %q", tc.src, got, want)
			}
		})
	}
}

// One question per expression: the print road, the void road and the verdict road each ask the SAME
// lookup the fold performs, so a slot can never be rendered with a kind it does not have.
func TestDictGetKindIsAskedOfTheFoldOwnLookup(t *testing.T) {
	body, err := os.ReadFile("codegen.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "func (g *irGen) dictFoldSlot(") {
		t.Fatal("expected one dict.get lookup shared by the print, void and verdict roads")
	}
	// A second, hand-copied key scan in the print roads is how these three drifted apart in the
	// first place: the fold knew the key, the printer did not ask.
	if n := strings.Count(text, "dl.Vals[i].(*StrLit)"); n > 1 {
		t.Fatalf("the slot-is-a-text question is asked %d times; ask g.dictFoldSlot once (Gap R.180)", n)
	}
}

// The verdict half lives in the shared pure predicate, so the record and the compiler answer the
// same question and a REPL cannot echo True beside a binary echoing 1.
func TestDictGetVerdictGoesThroughTheSharedPredicate(t *testing.T) {
	src, err := exec.Command("python3", "-c", "print({1: True}.get(1))").Output()
	if err == nil && strings.TrimSpace(string(src)) != "True" {
		t.Skip("the reference on this host disagrees")
	}
	env := BoolEnv{}
	if !IsBoolExpr(parseExprForTest(t, `v = {1: True}.get(1)`), env) {
		t.Fatal("a dict.get over a literal dict whose slot holds a verdict is a verdict; the answer came " +
			"from IsBoolExpr, so the fix belongs in boolvalue.go and not in a print-road special case")
	}
}

// The float half of the same question is Gap R.105's, and it is still wrong. This is a promotion
// test, not a skipped one: when the compiled leg starts answering these, delete the t.Log and move the
// rows into TestDictGetAnswersTheKindItsSlotHas — do not delete the coverage.
func TestDictGetFloatSlotStillOwesItsTag(t *testing.T) {
	for _, src := range []string{
		`print({1: 1.5}.get(1))`,
		`print({1: 1.5}.get(9, 2.5))`,
	} {
		want := strings.TrimSpace(referenceOut(t, src))
		got := strings.TrimSpace(compiledOut(t, src))
		if got == want {
			t.Log("the compiled leg now answers a float dict.get slot correctly — move this row into " +
				"TestDictGetAnswersTheKindItsSlotHas and say so on the Gap R.105 record")
			continue
		}
		if !strings.Contains(got, ".") {
			t.Logf("Gap R.105 still open: %q compiled %q, reference %q (a box handle printed through %%d)",
				src, got, want)
		}
	}
}
