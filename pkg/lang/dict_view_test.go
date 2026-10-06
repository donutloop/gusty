// Dict views (roadmap Gap R.182, owner L11.1; ADR 0296). Three bugs sat on this one row:
//
//	print({"a": 1}.keys())    reference dict_keys(['a'])   --interp ['a']   --aot 0     (handle through %d)
//	print({1: 2}.values())    reference dict_values([2])   --interp [2]     --aot EXIT 2 (rt_print_list_mixed(i32 @.lst1, …))
//
// and the reference's wrapper word — the thing that tells a view apart from a list — was nowhere in
// either engine. A view is list-SHAPED (sum/min/max/len/for/in all work on it) and list-rendered
// INSIDE its own name, so the object keeps its list kind and carries the wrapper separately.
package lang

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDictViewPrintsAsAView(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`print({"a": 1}.keys())`, `dict_keys(['a'])`},
		{`print({"a": 1, "b": 2}.keys())`, `dict_keys(['a', 'b'])`},
		{`print({"a": 1}.values())`, `dict_values([1])`},
		{`print({1: 2}.values())`, `dict_values([2])`},
		{`print({1: "a"}.values())`, `dict_values(['a'])`},
		{`print({}.keys())`, `dict_keys([])`},
		{`print({}.values())`, `dict_values([])`},
		// a list is NOT a view, and must keep printing as a list
		{`print([1, 2])`, `[1, 2]`},
		{`print({"a": 1})`, `{'a': 1}`},
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

// The exit-2 half: the fold rendered a folded literal as the ADDRESS of a compile-time global and fed
// it to a heap walker, which is the shape ADR 0188 removed for literals and llc rejects.
func TestDictViewOfAnIntValuedDictDoesNotSpendExit2(t *testing.T) {
	for _, src := range []string{
		`print({1: 2}.values())`,
		`print({1: 2, 3: 4}.values())`,
		`print({1: "a", 2: "b"}.keys())`,
	} {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("the reference answers this at exit 0: %v", err)
		}
		if strings.Contains(res.IR, "rt_print_list_mixed(i32 @") {
			t.Fatalf("%q handed a GLOBAL ADDRESS to a heap walker:\n%s", src, res.IR)
		}
		if _, err := exec.LookPath("llc-20"); err == nil {
			mustVerifyWithLLC(t, res.IR)
		}
	}
}

// A view is list-SHAPED: everything a list can be used for, a view can be used for. The rendering
// changed; the object's kind deliberately did not, which is why none of these broke.
func TestDictViewIsStillListShaped(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`print(sum({1: 2, 3: 4}.keys()))`, `4`},
		{`print(max({1: 2, 3: 4}.keys()))`, `3`},
		{`print(min({1: 2, 3: 4}.keys()))`, `1`},
	} {
		t.Run(strings.ReplaceAll(tc.src, " ", ""), func(t *testing.T) {
			want := strings.TrimSpace(referenceOut(t, tc.src))
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

// sorted(view) and list(view) are refusals on the compiled leg from OTHER roads (sorted folds only an
// inline literal; list(<container>) is unimplemented AOT). Both predate this row and both are measured
// unchanged on a HEAD build, so they are reported, not pinned — a refusal an earlier cycle has not lifted
// must not be laundered into an assertion, and must not be deleted when it starts working either.
func TestDictViewThroughOtherRoadsStillRefuses(t *testing.T) {
	for _, src := range []string{
		`print(sorted({"b": 1, "a": 2}.keys()))`,
		`print(list({"a": 1}.keys()))`,
	} {
		got, refused := compiledOutOrRefusal(t, src)
		if !refused {
			t.Log("the compiled leg now answers " + src + " — move this row into " +
				"TestDictViewIsStillListShaped and say so on the record: " + strings.TrimSpace(got))
		}
	}
}

// One question, asked of the same call shape the fold recognises — a hand-copied method-name switch is
// how the printer and the fold drift apart (the same rule as Gap R.180's one lookup).
func TestDictViewPrinterAndFoldAgree(t *testing.T) {
	body, err := os.ReadFile("codegen.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if !strings.Contains(text, "func (g *irGen) dictViewElems(") {
		t.Fatal("expected one predicate that decides which calls are dict views")
	}
	// `items()` stays a refusal until L11.3 gives tuples a value; the printer must not claim it.
	if strings.Contains(text, `case "items":
		return dl.Keys, "items", true`) {
		t.Fatal("the view printer claims items() while the fold refuses it — a pair has no value " +
			"representation until L11.3, so the refusal is the honest answer")
	}
}
