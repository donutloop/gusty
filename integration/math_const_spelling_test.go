// integration/math_const_spelling_test.go — the CLI half of a standard library that answers the reference's
// NAME (roadmap L11.6's last clause; ADR 0272 gave the value its type, ADR 0321 gives the name its twin).
//
// `import math` / `print(math.PI)` has been right everywhere since ADR 0272, and the conformance ledger still
// records that program as `not_applicable`, because the reference spells its constants `math.pi` / `math.e` /
// `math.tau` and has no `PI` to compare. So the one numeric surface the tracker had never been adjudicated by
// the reference leg: the comparison was a hand-written twin class in `integration/module_const_test.go`, which
// proves the VALUE and proves nothing about the NAME.
//
// `stdlib/math.gy` now declares the reference's spellings as the literal, with this language's upper-case names
// aliased to them. That turns the question into a shared program: the same source text runs on CPython and on
// the compiled backend, and this file compares the two rather than describing them. Four claims:
//   - the probe program — written the reference's way — prints byte-identical output on both legs, at exit 0,
//     and the matrix row is asserted rather than recorded as a divergence;
//   - every position L11.6 owned is asked through the shipped CLI (`/=`-style arithmetic is not the only way a
//     folded literal reaches a print): the negation, the product, a binding then a comparison, the digit count,
//     the floored division, the modulo, a container element, an f-string field;
//   - the two spellings of one constant answer the same bytes through the CLI, because the module declares one
//     literal and the second name reads the first;
//   - a name the module does not declare is a refusal in words at exit 1, and the contract's exit 2 (ADR 0166)
//     appears nowhere — `math.inf`, `math.nan` and `math.floor` included, which are Gap R.207's row rather than
//     this row's answer.
package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// mathProbe is the program the conformance matrix asserts; the file is the source of truth for it, so the CLI
// table below and the matrix row cannot drift into testing different programs.
func mathProbe(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(recordRoot, "integration", "programs",
		"probe_math_constant_answers_the_references_spelling.gy"))
	if err != nil {
		t.Fatalf("read the probe: %v", err)
	}
	return string(data)
}

// TestCLIAgentTheReferenceRunsTheProgramThatSpellsMathItsOwnWay is the reference leg through the shipped CLI:
// one source text, two engines' outputs compared rather than described.
func TestCLIAgentTheReferenceRunsTheProgramThatSpellsMathItsOwnWay(t *testing.T) {
	dir := t.TempDir()
	probe := mathProbe(t)
	ref, ok := cpythonPlainOut(t, dir, probe)
	if !ok {
		t.Skip("the reference could not answer the probe — the row would be a claim, not a comparison")
	}
	path := writeSrc(t, dir, "math_probe.gy", probe)
	got, code := cliRunMerged(t, "--file", path)
	if code == 2 {
		t.Fatalf("exit 2 — LLVM rejected the module a stdlib constant was emitted into (ADR 0166):\n%s", got)
	}
	if code != 0 {
		t.Fatalf("the compiled run exited %d on a program the reference prints:\n reference: %q\n compiled: %q", code, ref, got)
	}
	requireReferenceAgreement(t, probe, ReferenceAgreement{Python: ref, Compiled: got, Code: code},
		"roadmap L11.6 (a data module's constant keeps its type — and now its name)",
		"math.pi / math.e / math.tau, negated, multiplied, compared, rounded, floored, modulo'd, in a container and in a field")
}

// TestCLIAgentEveryPositionThatHidADoubleAnswersTheReferenceThroughTheCLI is the L11.6 family asked of the
// reference-spelled name, one line at a time, because a single probe cannot say which position broke.
func TestCLIAgentEveryPositionThatHidADoubleAnswersTheReferenceThroughTheCLI(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{"the census row, spelled as the reference does", "import math\nprint(math.pi)\n", "3.141592653589793\n"},
		{"this language's own spelling, still answered", "import math\nprint(math.PI)\n", "3.141592653589793\n"},
		{"the second constant, both spellings", "import math\nprint(math.e)\nprint(math.E)\n", "2.718281828459045\n2.718281828459045\n"},
		{"the negation", "import math\nprint(-math.pi)\n", "-3.141592653589793\n"},
		{"times an integer", "import math\nprint(math.pi * 2)\n", "6.283185307179586\n"},
		{"true division", "import math\nprint(math.pi / 2)\n", "1.5707963267948966\n"},
		{"bound, then compared", "import math\nx = math.pi\nprint(x > 3.14)\n", "True\n"},
		{"the digit count", "import math\nprint(round(math.pi, 2))\n", "3.14\n"},
		{"floored division keeps the double", "import math\nprint(math.pi // 2)\n", "1.0\n"},
		{"the modulo", "import math\nprint(math.pi % 2)\n", "1.1415926535897931\n"},
		{"two constants in a container", "import math\nprint([math.pi, math.e])\n", "[3.141592653589793, 2.718281828459045]\n"},
		{"an f-string field", "import math\nprint(f\"{math.e}\")\n", "2.718281828459045\n"},
		{"an extension name, both spellings", "import math\nprint(math.PHI)\nprint(math.phi)\n", "1.618033988749895\n1.618033988749895\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "mathuse.gy", tc.src)
			got, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 on a program the standard library answers (ADR 0166):\n%s", got)
			}
			if code != 0 || got != tc.want {
				t.Errorf("compiled exited %d and printed %q, want %q", code, got, tc.want)
			}
			if !strings.Contains(tc.src, "math.PI") && !strings.Contains(tc.src, "PHI") {
				// Only the reference-spelled rows have a CPython to ask.
				ref, ok := cpythonPlainOut(t, dir, tc.src)
				if !ok {
					t.Skipf("the reference declined: %s", tc.src)
				}
				if ref != tc.want {
					t.Fatalf("the reference said %q for the same source, want %q — the table is pinned to the "+
						"reference, not to what the compiled leg happens to print", ref, tc.want)
				}
			}
		})
	}
}

// TestTheMathProbeIsAssertedParityInTheMatrix keeps the promotion honest: the program the ledger used to skip
// for its spelling is a row that asserts, matches, and is conformant.
func TestTheMathProbeIsAssertedParityInTheMatrix(t *testing.T) {
	matrix, err := os.ReadFile(filepath.Join(recordRoot, "integration", "conformance-matrix.json"))
	if err != nil {
		t.Fatalf("read the matrix (run `go test ./integration` once to write it): %v", err)
	}
	var m struct {
		Results []struct {
			Case struct {
				Name     string `json:"name"`
				Asserted bool   `json:"asserted"`
				Oracle   string `json:"oracle"`
			} `json:"case"`
			Conformant bool `json:"conformant"`
			AOTOK      bool `json:"aot_ok"`
			PythonOK   bool `json:"python_ok"`
		} `json:"results"`
	}
	if err := json.Unmarshal(matrix, &m); err != nil {
		t.Fatalf("the matrix does not parse: %v", err)
	}
	const want = "probe_math_constant_answers_the_references_spelling.gy"
	for _, r := range m.Results {
		if r.Case.Name != want {
			continue
		}
		if !r.Case.Asserted {
			t.Errorf("%s is in the matrix but not asserted — a stdlib name the reference can spell is parity surface, "+
				"not a recorded divergence", want)
		}
		if r.Case.Oracle != "match" {
			t.Errorf("%s declares oracle %q, want match", want, r.Case.Oracle)
		}
		if !r.Conformant || !r.AOTOK || !r.PythonOK {
			t.Errorf("%s: conformant=%v compiled-ok=%v reference-ok=%v — the probe must answer on both legs",
				want, r.Conformant, r.AOTOK, r.PythonOK)
		}
		return
	}
	t.Fatalf("%s is not in the matrix at all — register it in conformanceStandalone()", want)
}

// TestAMathNameTheModuleDoesNotDeclareIsRefusedAtTheContractCode is the exit-code half: an attribute the module
// does not have is a refusal in words at exit 1, never the contract's own-bug code, and the sentence names the
// missing attribute (ADR 0166, ADR 0006).
func TestAMathNameTheModuleDoesNotDeclareIsRefusedAtTheContractCode(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ src, want string }{
		{"import math\nprint(math.PI2)\n", "math.PI2"},
		{"import math\nprint(math.inf)\n", "math.inf"},
		{"import math\nprint(math.nan)\n", "math.nan"},
	} {
		t.Run(tc.src, func(t *testing.T) {
			path := writeSrc(t, dir, "mathmiss.gy", tc.src)
			out, code := cliRunMerged(t, "--file", path)
			if code != 1 {
				t.Errorf("exit %d, want the honest refusal's 1 (ADR 0006): %s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal does not name %s: %s", tc.want, out)
			}
			if code == 2 {
				t.Errorf("exit 2 — a missing stdlib name is a program's mistake, not the compiler's (ADR 0166)")
			}
		})
	}
}

// TestTheStdlibModuleOnDiskIsWhatTheTestsImport closes the loop between the module the unit tests read and the
// module the CLI resolves: a `--stdlib` pointing elsewhere must not be what the probe's answer came from.
func TestTheStdlibModuleOnDiskIsWhatTheTestsImport(t *testing.T) {
	if got := lang.ResolveImportPath("math"); !strings.HasSuffix(got, filepath.Join("stdlib", "math.gy")) {
		t.Fatalf("the resolver answered %q for `import math` — the probe's answer has to come from the module in "+
			"the tree, not from a --stdlib someone exported", got)
	}
	data, err := os.ReadFile(filepath.Join(recordRoot, "stdlib", "math.gy"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pi", "e", "tau", "PI", "E", "TAU", "PHI", "SQRT2", "LN2", "LN10"} {
		if !strings.Contains(string(data), name+" =") {
			t.Errorf("stdlib/math.gy no longer declares %q — the probe, the record and the docs all cite it", name)
		}
	}
}
