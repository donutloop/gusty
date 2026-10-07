// End-to-end cover for the dict-view row (roadmap Gap R.182, owner L11.1). The CLI's exit code is the
// contract: a view prints the reference's wrapper, and never spends the contract's exit 2.
package integration

import (
	"strings"
	"testing"
)

func cliViewOut(t *testing.T, backend, src string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	f := writeSrc(t, dir, "view.gy", src)
	return cliRunMerged(t, backend, "--file", f)
}

// The exit-2 half: `rt_print_list_mixed(i32 @.lst1, i32 0)` — a global address handed to a heap walker,
// which llc rejects and ADR 0166 counts as the compiler's own bug.
func TestCLIDictViewNeverSpendsExit2(t *testing.T) {
	for _, src := range []string{
		`print({"a": 1}.keys())`,
		`print({1: 2}.values())`,
		`print({1: 2, 3: 4}.values())`,
		`print({1: "a", 2: "b"}.keys())`,
	} {
		for _, backend := range cliEngines {
			out, code := cliViewOut(t, backend, src)
			if code == 2 {
				t.Fatalf("%s %q spent the contract's exit 2 (compiler bug):\n%s", backend, src, out)
			}
			if code != 0 {
				t.Fatalf("%s %q: exit %d, the reference runs it:\n%s", backend, src, code, out)
			}
		}
	}
}

// The wrapper word is the row: `dict_keys([...])` is what distinguishes a view from a list, and both
// engines printed a bare list.
func TestCLIDictViewPrintsWhatTheReferencePrints(t *testing.T) {
	for _, src := range []string{
		`print({"a": 1}.keys())`,
		`print({"a": 1, "b": 2}.keys())`,
		`print({"a": 1}.values())`,
		`print({1: 2}.values())`,
		`print({1: "a"}.values())`,
		`print({}.keys())`,
		`print({}.values())`,
		// a list and a dict keep their own renderings
		`print([1, 2])`,
		`print({"a": 1})`,
		// list-shaped uses still work — the object kept its list kind
		`print(sum({1: 2, 3: 4}.keys()))`,
		`print(max({1: 2, 3: 4}.keys()))`,
	} {
		want, ok := cpythonPlainOut(t, t.TempDir(), src)
		if !ok {
			continue
		}
		want = strings.TrimSpace(want)
		for _, backend := range cliEngines {
			out, code := cliViewOut(t, backend, src)
			if code != 0 {
				t.Fatalf("%s %q: exit %d where the reference exits 0:\n%s", backend, src, code, out)
			}
			if got := strings.TrimSpace(out); got != want {
				t.Fatalf("%s %q: we printed %q, the reference prints %q", backend, src, got, want)
			}
		}
	}
}
