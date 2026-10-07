// Slice-of-a-container tests (roadmap Gap R.179, owner L11.1, ADR 0187's pairing rule, ADR 0234's
// literal question, ADR 0166's exit codes).
//
// A slice of a container was broken in three places at once, and every one of them is a number the
// reference does not produce or a module the compiler should never have handed to llc:
//
//	print([1, 2, 3][1:])   →   call i32 @rt_slice(i32 @.lst1, …)   →  llc-20 rejects the module, exit 2
//	print([1, 2, 3][1:])   →   printf("%d", <heap handle>)         →  "1" at exit 0
//	print(["a","b"][1:])   →   rt_slice copies payloads, never tags →  "[1]" at exit 0
//
// the record answered all three correctly throughout, which is why a two-backend parity matrix
// found none of it and the oracle leg found all of it.
package lang

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// The shapes that spent the contract's exit 2, printed the handle, or dropped the tag.
func TestSliceOfAContainerAnswersLikeTheReference(t *testing.T) {
	for _, src := range []string{
		`print([1, 2, 3][1:])`,
		`print([1, 2, 3][:2])`,
		`print([1, 2, 3][::2])`,
		`print([1, 2, 3][-1:])`,
		`print([1, 2, 3][1:2])`,
		`print([][:])`,
		`print(["a", "b"][1:])`,
		`print(["a", "b", "c"][::2])`,
		`print([1, "a", None][1:])`,
		`print([True, 2][0:])`,
	} {
		t.Run(strings.ReplaceAll(src, " ", ""), func(t *testing.T) {
			want := referenceOut(t, src)
			if want == "" || strings.Contains(want, "Error") {
				t.Fatalf("the reference did not answer: %q", want)
			}
			got, refused := compiledOutOrRefusal(t, src)
			if refused {
				t.Fatalf("the compiled leg refused instead of answering %q: %s", src, got)
			}
			if strings.Contains(got, "invalid module") || strings.Contains(got, "llc") {
				t.Fatalf("the compiler emitted a module llc rejected for %q:\n%s", src, got)
			}
			if got != want {
				t.Fatalf("%q: compiled %q, reference %q", src, got, want)
			}
		})
	}
}

// A heap HANDLE printed through printf's %d is the invalid-IR shape ADR 0188 removed for literals;
// the slice road walked straight back into it.
func TestASliceIsNotPrintedAsItsHandle(t *testing.T) {
	src := `print([1, 2, 3][1:])`
	got := compiledOut(t, src)
	if got == "1" {
		t.Fatalf("%q printed the heap handle rather than the list — the printer must ask whether the "+
			"argument is a container BEFORE the numeric road reads a slice as a number", src)
	}
}

// A slice of TEXT is the case ADR 0187 was written about and rt_slice had never obeyed: the copy
// loop wrote payloads and left @heap_tags alone, so the text's interned INDEX printed as a number.
func TestASliceCarriesTheTagsItsElementsHave(t *testing.T) {
	for _, src := range []string{
		`print(["a", "b"][1:])`,
		`print(["a", "b", "c"][::2])`,
		`print([1, "a", None][1:])`,
	} {
		got := compiledOut(t, src)
		if got == referenceOut(t, src) {
			continue
		}
		// A quoted text is a tag; a bare number where a quoted text belongs is the bug.
		if !strings.Contains(got, "'") && strings.HasPrefix(strings.TrimSpace(got), "[") {
			t.Fatalf("%q: compiled %q, reference %q — a slot whose payload is an interned text printed "+
				"its index, which is ADR 0187's pairing rule again", src, got, referenceOut(t, src))
		}
		t.Fatalf("%q: compiled %q, reference %q", src, got, referenceOut(t, src))
	}
}

// A text slice still belongs to the text road, and a subscript to the element road — the arms added
// beside them must not steal work that already answered.
func TestSliceArmsLeaveTheAnswersTheyAlreadyHad(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`print("abcdef"[2:])`, `cdef`},
		{`print("abcdef"[1:3])`, `bc`},
		{`print([1, 2, 3][0])`, `1`},
		{`print([1, 2, 3][-1])`, `3`},
	} {
		t.Run(tc.src, func(t *testing.T) {
			got := strings.TrimSpace(compiledOut(t, tc.src))
			if got != tc.want {
				t.Fatalf("%q: compiled %q, want %q", tc.src, got, tc.want)
			}
		})
	}
}

// The exit-2 class the whole row is about: the module must not merely run, it must be a module llc
// was willing to accept in the first place.
func TestASliceOfALiteralDoesNotSpendExit2(t *testing.T) {
	for _, src := range []string{
		`print([1, 2, 3][1:])`,
		`print([1, 2, 3][:2])`,
		`print([1, 2, 3][::2])`,
	} {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		ir := res.IR
		if strings.Contains(ir, "rt_slice(i32 @") {
			t.Fatalf("%q passed a GLOBAL ADDRESS where rt_slice wants a heap handle:\n%s", src, ir)
		}
		// The tag half of the same builder must be present, or the printer reads indices as numbers.
		if !strings.Contains(ir, "@heap_tags") {
			t.Fatalf("%q: rt_slice writes no tags at all", src)
		}
	}
}

// The whole runtime helper must still verify: the tag copy added two getelementptrs into a raw
// module string, where llc — not the LLVM verifier inside Go — is the first thing to complain.
func TestSliceHelperModuleVerifies(t *testing.T) {
	if _, err := exec.LookPath("llc-20"); err != nil {
		t.Skip("llc-20 not installed")
	}
	for _, src := range []string{
		`print([1, 2, 3][1:])`,
		`print(["a", "b"][1:])`,
		`print([1, 2, 3][::2])`,
	} {
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if !strings.Contains(res.IR, "rt_slice") {
			continue
		}
		mustVerifyWithLLC(t, res.IR)
	}
}

// One question, asked once: `isContainerExpr` is what the equality, membership and print roads all
// consult, and the slice arm is in that shared predicate rather than copied into each caller.
func TestSliceContainerQuestionIsShared(t *testing.T) {
	body, err := os.ReadFile("heapargs.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "case *Slice:") {
		t.Fatal("isContainerExpr answers the container question and had no *Slice arm; the slice road " +
			"should be asked from the shared predicate the equality and membership roads use")
	}
}
