// Package integration exercises the gusty compiler end-to-end through the
// real CLI. This file drives the standalone type-check path (`--check`) for
// the generics / structural-protocols surface (Round 14): recursive generic
// annotations (`list[int]`, `Callable[[...], R]`, `Sequence[T]`) are accepted
// as clean checks and rejected with a deterministic exit code when the
// structural `assignable(got, want)` relation fails.
package integration

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"testing"
)

// checkSource runs `gustyc --check <src>` and returns its exit code.
func checkSource(t *testing.T, bin, src string) int {
	t.Helper()
	cmd := exec.Command(bin, "--check", src)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return exitCode(t, err)
	}
	return 0
}

// TestCLICheckGenericsProtocols drives the `--check` path for the
// generics/protocols surface: structurally-correct generic annotations are a
// clean check (exit 0), mismatched element shapes are type errors (exit 1),
// and the JSON machine path carries the diagnostics.
func TestCLICheckGenericsProtocols(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	clean := `def apply(f: Callable[[int], bool], x: int) -> bool:
    return True
x: Sequence[int] = [1, 2]
s: Sequence[str] = "hi"
t: Sequence[int] = (1, 2)
d: dict[str, int] = {"a": 1}
n: list[list[int]] = [[1, 2], [3]]
`
	if code := checkSource(t, bin, clean); code != 0 {
		t.Fatalf("clean generics check: exit=%d, want 0", code)
	}

	bad := "x: Sequence[str] = [1, 2]\n"
	if code := checkSource(t, bin, bad); code != 1 {
		t.Fatalf("mismatched Sequence check: exit=%d, want 1", code)
	}

	// JSON mode: the machine path carries the structural mismatch diagnostic.
	// A non-zero exit is expected, so use CombinedOutput and parse regardless.
	js := exec.Command(bin, "--json", "--check", bad)
	jsOut, jerr := js.CombinedOutput()
	if jerr != nil && exitCode(t, jerr) != 1 {
		t.Fatalf("json check exit: %v\n%s", jerr, jsOut)
	}
	var res struct {
		Diagnostics []struct {
			Level string `json:"level"`
			Msg   string `json:"msg"`
		} `json:"diagnostics"`
		OK   bool `json:"ok"`
		Exit int  `json:"exit"`
	}
	if err := json.Unmarshal(jsOut, &res); err != nil {
		t.Fatalf("json check not parseable: %v\n%s", err, jsOut)
	}
	if res.OK || res.Exit != 1 {
		t.Errorf("json result: ok=%v exit=%d, want ok=false exit=1", res.OK, res.Exit)
	}
	found := false
	for _, d := range res.Diagnostics {
		if d.Level == "error" && len(d.Msg) > 0 {
			found = true
		}
	}
	if !found {
		t.Errorf("json diagnostics missing structural mismatch: %s", jsOut)
	}
}

// TestCLICheckProtocolExitCodes verifies the deterministic exit-code contract
// across the protocol surface: clean annotations exit 0, each mismatched
// element shape exits 1.
func TestCLICheckProtocolExitCodes(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	clean := []string{
		"x: Sequence[int] = [1, 2]\n",
		"x: Sequence[int] = (1, 2)\n",
		"x: Sequence[str] = \"hi\"\n",
		"def f(x: Callable[[int, str], bool]) -> None:\n    return None\n",
		"x: Sequence[int] = [1, 2, 3]\n",
	}
	for _, src := range clean {
		if code := checkSource(t, bin, src); code != 0 {
			t.Errorf("clean %q: exit=%d, want 0", src, code)
		}
	}

	reject := []string{
		"x: Sequence[int] = \"hi\"\n",       // str is Sequence[str], not Sequence[int]
		"x: Sequence[str] = [1, 2]\n",       // list[int] not Sequence[str]
		"x: Sequence[int] = 5\n",            // int is not a sequence
		"x: Sequence[bool] = [1, 2]\n",      // list[int] not Sequence[bool]
		"x: Sequence[int] = {\"a\": 1}\n",   // dict is not a Sequence
	}
	for _, src := range reject {
		if code := checkSource(t, bin, src); code != 1 {
			t.Errorf("reject %q: exit=%d, want 1", src, code)
		}
	}
}
