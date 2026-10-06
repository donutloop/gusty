// End-to-end cover for ADR 0187's pairing rule applied to rt_slice and ADR 0234's literal question
// (roadmap Gap R.179, owner L11.1). The CLI's own words are the contract: a slice of a container
// exits 0 with the reference's rendering, and never spends the contract's exit 2.
package integration

import (
	"strings"
	"testing"
)

func cliSliceOut(t *testing.T, backend, src string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	f := writeSrc(t, dir, "slice.gy", src)
	return cliRunMerged(t, backend, "--file", f)
}

// The exit-2 half of the row: a slice of a container literal handed llc
// `call i32 @rt_slice(i32 @.lst1, …)`, a global address where a heap handle belongs. Exit 2 is the
// compiler's own bug and the CLI must never report it for a program the reference runs.
func TestCLISliceOfAContainerNeverSpendsExit2(t *testing.T) {
	for _, src := range []string{
		`print([1, 2, 3][1:])`,
		`print([1, 2, 3][:2])`,
		`print([1, 2, 3][::2])`,
		`print(["a", "b"][1:])`,
	} {
		for _, backend := range []string{"--interp", "--aot"} {
			out, code := cliSliceOut(t, backend, src)
			if code == 2 {
				t.Fatalf("%s %q spent the contract's exit 2 (compiler bug):\n%s", backend, src, out)
			}
			if code != 0 {
				t.Fatalf("%s %q: exit %d, the reference runs it:\n%s", backend, src, code, out)
			}
		}
	}
}

// The wrong-number half: printing the handle said `1`, and a tag-less copy rendered an interned
// text's INDEX as a number. Both at exit 0, which is the class an agent cannot tell from success.
func TestCLISliceOfAContainerPrintsWhatTheReferencePrints(t *testing.T) {
	for _, src := range []string{
		`print([1, 2, 3][1:])`,
		`print([1, 2, 3][:2])`,
		`print([1, 2, 3][::2])`,
		`print([1, 2, 3][-1:])`,
		`print([][:])`,
		`print(["a", "b"][1:])`,
		`print(["a", "b", "c"][::2])`,
		`print([1, "a", None][1:])`,
		`print([True, 2][0:])`,
	} {
		want, ok := cpythonPlainOut(t, t.TempDir(), src)
		if !ok {
			continue
		}
		want = strings.TrimSpace(want)
		for _, backend := range []string{"--interp", "--aot"} {
			out, code := cliSliceOut(t, backend, src)
			if strings.TrimSpace(out) != want {
				t.Fatalf("%s %q: we printed %q, the reference prints %q", backend, src, strings.TrimSpace(out), want)
			}
			if code != 0 {
				t.Fatalf("%s %q: exit %d where the reference exits 0", backend, src, code)
			}
		}
	}
}

// The arms added beside the text and element roads must not steal work that already answered.
func TestCLISliceKeepsTheAnswersItAlreadyHad(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`print("abcdef"[2:])`, `cdef`},
		{`print("abcdef"[1:3])`, `bc`},
		{`print([1, 2, 3][0])`, `1`},
		{`print([1, 2, 3][-1])`, `3`},
	} {
		for _, backend := range []string{"--interp", "--aot"} {
			out, code := cliSliceOut(t, backend, tc.src)
			if code != 0 || strings.TrimSpace(out) != tc.want {
				t.Fatalf("%s %q: exit %d out %q, want %q", backend, tc.src, code, strings.TrimSpace(out), tc.want)
			}
		}
	}
}
