// Package integration — the variance + generics surface (L6.6) end to end.
//
// Two contracts are asserted here:
//
//   - Parity: integration/programs/variance.gy (nominal class annotations,
//     covariant read-only Sequence, invariant annotated lists/dicts) produces
//     byte-identical stdout on the interpreter and the LLVM AOT backend.
//   - The machine path: `gustyc --check --json` reports every variance violation
//     with a STABLE diagnostic code and an actionable suggestion, and the
//     self-describing `gustyc --variance` table matches the checker's rules.
package integration

import (
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const varianceHierarchy = `class Animal:
    def speak(self):
        return 1

class Dog(Animal):
    def speak(self):
        return 2

class Puppy(Dog):
    def speak(self):
        return 3

class Rock:
    def hard(self):
        return 1
`

// checkJSON runs `gustyc --json --check <src>` and decodes the CheckResult.
func checkJSON(t *testing.T, bin, src string) (int, bool, []checkDiag) {
	t.Helper()
	cmd := exec.Command(bin, "--json", "--check", src)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		code = exitCode(t, err)
	}
	var res struct {
		Diagnostics []checkDiag `json:"diagnostics"`
		OK          bool        `json:"ok"`
		Exit        int         `json:"exit"`
	}
	if jerr := json.Unmarshal(out, &res); jerr != nil {
		t.Fatalf("--check --json not parseable: %v\n%s", jerr, out)
	}
	if res.Exit != code {
		t.Errorf("exit code drift: json says %d, process exited %d", res.Exit, code)
	}
	return code, res.OK, res.Diagnostics
}

type checkDiag struct {
	Level      string `json:"level"`
	Msg        string `json:"msg"`
	Code       string `json:"code"`
	Suggestion string `json:"suggestion"`
}

func diagWithCode(diags []checkDiag, code string) *checkDiag {
	for i, d := range diags {
		if d.Code == code && d.Level == "error" {
			return &diags[i]
		}
	}
	return nil
}

// TestVarianceParity runs the variance conformance program on the compiled path.
func TestVarianceParity(t *testing.T) {
	src := readProgramSrc("variance")
	want := "1\n2\n3\n30\n2\n42\n"
	got := runInterp(t, src)
	if got != want {
		t.Errorf("interpreter stdout = %q, want %q", got, want)
	}
	aot := runAOT(t, src)
	if aot != want {
		t.Errorf("AOT stdout = %q, want %q", aot, want)
	}
	if aot != got {
		t.Errorf("parity broken: interpreter %q vs AOT %q", got, aot)
	}
}

// TestCheckReportsVarianceRules drives `gustyc check` over each variance rule and
// asserts the STABLE machine-readable code plus an actionable suggestion — the
// agentic consumption path for the type system.
func TestCheckReportsVarianceRules(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	cases := []struct {
		name string
		src  string
		code string // expected diagnostic code ("" => must be clean)
	}{
		{
			name: "list invariance",
			src:  "def take(xs: list[int]) -> int:\n    return len(xs)\nds: list[str] = [\"a\"]\nz = take(ds)\n",
			code: "type.variance.invariant",
		},
		{
			name: "dict key invariance",
			src:  "def count(d: dict[str, int]) -> int:\n    return len(d)\nd: dict[int, int] = {1: 2}\nz = count(d)\n",
			code: "type.variance.invariant",
		},
		{
			name: "callable contravariance",
			src:  varianceHierarchy + "def use(f: Callable[[Animal], int]) -> int:\n    return f(Animal())\ndef dog_only(d: Dog) -> int:\n    return d.speak()\nz = use(dog_only)\n",
			code: "type.variance.contravariant",
		},
		{
			name: "nominal class",
			src:  varianceHierarchy + "def feed(a: Animal) -> int:\n    return a.speak()\nz = feed(Rock())\n",
			code: "type.variance.nominal",
		},
		{
			name: "sequence covariance",
			src:  varianceHierarchy + "def count(xs: Sequence[Dog]) -> int:\n    return len(xs)\nanimals: list[Animal] = [Animal()]\nz = count(animals)\n",
			code: "type.variance.covariant",
		},
		// the sound directions must all check clean (exit 0, no diagnostics)
		{name: "subclass satisfies base annotation", src: varianceHierarchy + "def feed(a: Animal) -> int:\n    return a.speak()\nz = feed(Puppy())\n", code: ""},
		{name: "wider handler is substitutable", src: varianceHierarchy + "def use(f: Callable[[Dog], int]) -> int:\n    return f(Dog())\ndef any_animal(a: Animal) -> int:\n    return a.speak()\nz = use(any_animal)\n", code: ""},
		{name: "list literal widens to the destination", src: "x: list[int | str] = [1, \"a\"]\nprint(x)\n", code: ""},
		{name: "covariant sequence read", src: varianceHierarchy + "def count(xs: Sequence[Animal]) -> int:\n    return len(xs)\ndogs: list[Dog] = [Dog()]\nz = count(dogs)\n", code: ""},
		{name: "untyped code stays gradual", src: varianceHierarchy + "def feed(a):\n    return a.speak()\nprint(feed(Animal()))\nxs = [1, 2]\nprint(len(xs))\n", code: ""},
	}

	for _, c := range cases {
		code, ok, diags := checkJSON(t, bin, c.src)
		if c.code == "" {
			if code != 0 || !ok {
				t.Errorf("%s: expected a clean check, got exit=%d ok=%v diags=%v", c.name, code, ok, diags)
			}
			continue
		}
		if code != 1 || ok {
			t.Errorf("%s: expected exit=1, got exit=%d ok=%v diags=%v", c.name, code, ok, diags)
		}
		d := diagWithCode(diags, c.code)
		if d == nil {
			t.Errorf("%s: expected a %s diagnostic, got %+v", c.name, c.code, diags)
			continue
		}
		if strings.TrimSpace(d.Suggestion) == "" {
			t.Errorf("%s: %s diagnostic must carry a suggestion: %+v", c.name, c.code, d)
		}
		if !strings.Contains(d.Msg, "expected") {
			t.Errorf("%s: diagnostic must state the expected type: %+v", c.name, d)
		}
	}
}

// TestVarianceSelfDescription checks the `gustyc --variance` machine surface:
// an agent can discover the whole variance model without reading prose.
func TestVarianceSelfDescription(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)

	out, err := exec.Command(bin, "--variance").Output()
	if err != nil {
		t.Fatalf("--variance: %v", err)
	}
	var doc struct {
		SchemaVersion string `json:"schema_version"`
		Version       string `json:"language_version"`
		GeneratedBy   string `json:"generated_by"`
		Rules         []struct {
			Constructor string   `json:"constructor"`
			Params      []string `json:"params"`
			Variance    []string `json:"variance"`
			Mutable     bool     `json:"mutable"`
			ReadOnly    string   `json:"read_only"`
			Rationale   string   `json:"rationale"`
			Code        string   `json:"code"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("--variance output is not valid JSON: %v\n%s", err, out)
	}
	if doc.SchemaVersion != "1.0" || doc.GeneratedBy == "" {
		t.Errorf("variance document header wrong: %+v", doc)
	}
	seen := map[string][]string{}
	for _, r := range doc.Rules {
		if r.Code == "" || r.Rationale == "" {
			t.Errorf("rule %q is not self-describing: %+v", r.Constructor, r)
		}
		if len(r.Params) != len(r.Variance) {
			t.Errorf("rule %q has %d params but %d variances", r.Constructor, len(r.Params), len(r.Variance))
		}
		seen[r.Constructor] = r.Variance
	}
	for ctor, want := range map[string]string{
		"list[T]":             "invariant",
		"dict[K, V]":          "invariant",
		"Sequence[T]":         "covariant",
		"Callable[[P...], R]": "contravariant",
	} {
		v, ok := seen[ctor]
		if !ok {
			t.Errorf("variance table is missing %s", ctor)
			continue
		}
		if v[0] != want {
			t.Errorf("%s: variance[0] = %q, want %q", ctor, v[0], want)
		}
	}
}
