// End-to-end cover for the two compiled-leg wrong numbers found by the 2026-07-06 surface sweep
// (roadmap Gap R.180 and Gap R.181, owner L11.1). The CLI's own exit code and stdout are the
// contract: a set counts distinct members, and a `dict.get` prints the kind its slot holds.
package integration

import (
	"strings"
	"testing"
)

func cliPairOut(t *testing.T, backend, src string) (string, int) {
	t.Helper()
	dir := t.TempDir()
	f := writeSrc(t, dir, "pair.gy", src)
	return cliRunMerged(t, backend, "--file", f)
}

// A set with duplicates is the case that reveals the bug: `len({1, 2})` answers 2 on both legs
// whatever the code does, so only a DUPLICATE distinguishes a set from a list wearing braces.
func TestCLISetLiteralCountsDistinctMembers(t *testing.T) {
	for _, src := range []string{
		`print(len({1, 2, 2, 3}))`,
		`print(len({1, 1, 1}))`,
		`print(len({5, 3, 3, 3, 1}))`,
		`print(len({1, 2}) + len({3, 3}))`,
		`print({1, 2, 2, 3})`,
		`print({1, 1} == {1})`,
		`print(2 in {1, 2, 2, 3})`,
		`print(9 in {1, 2, 2, 3})`,
	} {
		want, ok := cpythonPlainOut(t, t.TempDir(), src)
		if !ok {
			continue
		}
		want = strings.TrimSpace(want)
		for _, backend := range cliEngines {
			out, code := cliPairOut(t, backend, src)
			if code != 0 {
				t.Fatalf("%s %q: exit %d, the reference exits 0:\n%s", backend, src, code, out)
			}
			if got := strings.TrimSpace(out); got != want {
				t.Fatalf("%s %q: we printed %q, the reference prints %q", backend, src, got, want)
			}
		}
	}
}

// The kind a `get` answers with belongs to the slot it read: a text, a void, a verdict or a number.
// Each of the first three printed the machine's WORD for the value rather than the value.
func TestCLIDictGetPrintsTheKindItsSlotHas(t *testing.T) {
	for _, src := range []string{
		`print({1: "a"}.get(1))`,
		`print({1: "a", 2: "b"}.get(1, "z"))`,
		`print({1: "a", 2: "b"}.get(9, "z"))`,
		`print({1: "a"}.get(9, "fallback"))`,
		`print({"k": None}.get("k"))`,
		`print({"k": None}.get("zz", None))`,
		`print({1: True}.get(1))`,
		`print({"k": False}.get("k"))`,
		`print({1: True}.get(9, True))`,
		`print({1: 2}.get(1))`,
		`print({1: "a", 2: 3}.get(2))`,
		`print({1: "a"}.get(9))`,
		`print({"a": "hi"}.get("a"))`,
	} {
		want, ok := cpythonPlainOut(t, t.TempDir(), src)
		if !ok {
			continue
		}
		want = strings.TrimSpace(want)
		for _, backend := range cliEngines {
			out, code := cliPairOut(t, backend, src)
			if code != 0 {
				t.Fatalf("%s %q: exit %d, the reference exits 0:\n%s", backend, src, code, out)
			}
			if got := strings.TrimSpace(out); got != want {
				t.Fatalf("%s %q: we printed %q, the reference prints %q — a get that answers a word "+
					"instead of the slot's value is a wrong number at exit 0", backend, src, got, want)
			}
		}
	}
}
