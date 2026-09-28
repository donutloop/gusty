package lang

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// The oracle leg (roadmap L11.9, ADR 0186).
//
// The classification functions here are the whole point of the third leg, so they
// are tested the way the rest of the toolchain is: with the cases that must *fail*,
// not only the ones that pass. A harness that can only report success is a harness
// that reports success for the wrong answer — which is precisely the bug L11.9 was
// written to catch (print(True) == "1" sat in a green build for 100+ ADRs).

func reportFor(interpOK bool, interp, interpErr string, aotOK bool, aot, aotErr string, pyOK bool, py, pyErr string, rules []string) OracleReport {
	return BuildOracleReport(interpOK, interp, interpErr, aotOK, aot, aotErr, rules, pyOK, py, pyErr)
}

func TestOracleReportMatchWhenBothBackendsPrintPythonsAnswer(t *testing.T) {
	rep := reportFor(true, "True\n", "", true, "True\n", "", true, "True\n", "", nil)
	if rep.Status != OracleMatch {
		t.Fatalf("status = %q, want %q", rep.Status, OracleMatch)
	}
	if !rep.Parity {
		t.Errorf("parity = false, want true")
	}
	for _, want := range []string{"interpreter", "aot", "python"} {
		l, ok := rep.Leg(want)
		if !ok {
			t.Fatalf("missing leg %s", want)
		}
		if want != "python" && !l.Matches {
			t.Errorf("leg %s: matches_python = false, want true", want)
		}
	}
	if len(rep.Notes) != 0 {
		t.Errorf("a matching row should carry no notes, got %v", rep.Notes)
	}
}

// TestOracleReportTwoBackendsAgreeingOnWrongAnswerIsDebt is the regression the whole
// item exists for: interpreter and AOT print "1", CPython prints "True".
func TestOracleReportTwoBackendsAgreeingOnWrongAnswerIsDebt(t *testing.T) {
	rep := reportFor(true, "1\n", "", true, "1\n", "", true, "True\n", "", nil)
	if rep.Status != OracleDebt {
		t.Fatalf("status = %q, want %q — parity must never be enough", rep.Status, OracleDebt)
	}
	if !rep.Parity {
		t.Errorf("parity = false; the backends do agree, that is the problem")
	}
	if rep.Legs[0].Matches || rep.Legs[1].Matches {
		t.Errorf("neither leg matches the oracle, so neither may report a match: %+v", rep.Legs)
	}
	if len(rep.Notes) != 2 {
		t.Errorf("want one note per disagreeing backend, got %v", rep.Notes)
	}
}

func TestOracleReportRefusalIsDebtNotPass(t *testing.T) {
	// The compiled backend refuses the program; the interpreter and CPython agree.
	rep := reportFor(true, "[1, 'a']\n", "", false, "", "codegen: unsupported expression", true, "[1, 'a']\n", "", nil)
	if rep.Status != OracleDebt {
		t.Fatalf("status = %q, want %q: a refusal is a divergence, not a skip", rep.Status, OracleDebt)
	}
	if rep.Legs[1].OK || rep.Legs[1].Matches {
		t.Errorf("a leg that failed may not be reported as ok or matching: %+v", rep.Legs[1])
	}
	if !strings.Contains(strings.Join(rep.Notes, "; "), "compiled leg failed") {
		t.Errorf("notes should say which leg failed: %v", rep.Notes)
	}
}

func TestOracleReportNotApplicableOnlyWhenTheOracleCannotJudge(t *testing.T) {
	rep := reportFor(true, "3\n", "", true, "3\n", "", false, "", "Traceback: SyntaxError", nil)
	if rep.Status != OracleNA {
		t.Fatalf("status = %q, want %q", rep.Status, OracleNA)
	}
	if !strings.Contains(strings.Join(rep.Notes, " "), "CPython leg did not complete") {
		t.Errorf("an NA row must say why there is no verdict: %v", rep.Notes)
	}
	// A compiler panic is recorded like any other leg failure: the report survives it.
	rep = reportFor(true, "3\n", "", false, "", "compiler panic: index out of range [-1]", true, "3\n", "", nil)
	if rep.Status != OracleDebt {
		t.Errorf("a compiler panic should be debt, got %q", rep.Status)
	}
	if !strings.Contains(strings.Join(rep.Notes, " "), "compiler panic") {
		t.Errorf("the panic must be visible in the notes: %v", rep.Notes)
	}
}

func TestOracleSetOrderRuleNormalizesSetsAndOnlySets(t *testing.T) {
	rules := DefaultOracleRules()
	// A set rendering: order is unspecified in CPython, so both sides are sorted.
	if got := OracleNormalize("{3, 1, 2}\n", rules); got != "{1, 2, 3}\n" {
		t.Errorf("set line = %q, want {1, 2, 3}", got)
	}
	if OracleNormalize("{1, 2, 3}\n", rules) != OracleNormalize("{3, 2, 1}\n", rules) {
		t.Errorf("two orders of the same set should compare equal")
	}
	// A dict rendering keeps its order: dict order is insertion order in both languages.
	if OracleNormalize("{'b': 1, 'a': 2}\n", rules) == OracleNormalize("{'a': 2, 'b': 1}\n", rules) {
		t.Errorf("a dict's order is observable in gusty and must not be normalised away")
	}
	// A set whose element contains a colon is still a set — the ':' is inside a string.
	if got := OracleNormalize("{'b:1', 'a'}\n", rules); got != "{'a', 'b:1'}\n" {
		t.Errorf("set with a colon inside a string = %q, want the elements sorted", got)
	}
	// A line that is not a bare container rendering is left alone.
	const nested = "  set of = {3, 1}\n"
	if OracleNormalize(nested, rules) != nested {
		t.Errorf("only a bare rendering is a container line: %q", OracleNormalize(nested, rules))
	}
	// Nested containers split on top-level commas only, and a nested set is sorted at
	// every level — while a list inside the line keeps its order, because that order
	// is observable in gusty.
	if got := OracleNormalize("{1, {3, 2}}\n", rules); got != "{1, {2, 3}}\n" {
		t.Errorf("nested set = %q, want the inner set sorted too", got)
	}
	if got := OracleNormalize("{1, [3, 2]}\n", rules); got != "{1, [3, 2]}\n" {
		t.Errorf("a list element must keep its order: %q", got)
	}
	// The trailing newline of the last line is preserved byte for byte.
	if got := OracleNormalize("{2, 1}", rules); got != "{1, 2}" {
		t.Errorf("output without a trailing newline should keep it that way: %q", got)
	}
}

func TestOracleCheckRequiresADeclaredState(t *testing.T) {
	c := ConformanceCase{ID: "programs/x", Name: "x.gy"} // no Oracle declared
	r := &ConformanceResult{}
	drift := c.OracleCheck(r)
	if len(drift) == 0 || !strings.Contains(drift[0], "classify") {
		t.Fatalf("an undeclared case must be reported, got %v", drift)
	}
	c.Oracle = "bogus"
	if drift := c.OracleCheck(&ConformanceResult{}); len(drift) == 0 {
		t.Fatalf("an unknown declared state must be reported")
	}
}

func TestOracleCheckDebtNeedsReasonOwnerAndPin(t *testing.T) {
	c := ConformanceCase{ID: "programs/x", Name: "x.gy", Oracle: OracleDebt}
	drift := c.OracleCheck(&ConformanceResult{})
	joined := strings.Join(drift, " | ")
	for _, want := range []string{"reason", "roadmap reference", "pin"} {
		if !strings.Contains(joined, want) {
			t.Errorf("debt without a %s should be drift: %v", want, drift)
		}
	}
	c.Reason, c.Ref = "bools print as 1", "roadmap L11.2"
	c.Pins = []OraclePin{{Backend: "interpreter", Stdout: "1\n"}, {Backend: "aot", Stdout: "1\n"}}
	// The legs print "1" and CPython prints "True": still debt, and the pins match,
	// so the row is honest and reports nothing.
	r := &ConformanceResult{InterpOK: true, InterpOut: "1\n", AOTOK: true, AOTOut: "1\n", PythonOK: true, PythonOut: "True\n"}
	if drift := c.OracleCheck(r); len(drift) != 0 {
		t.Errorf("an accurately pinned debt row should report no drift, got %v", drift)
	}
	if r.InterpMatchesPython || r.AOTMatchesPython {
		t.Errorf("neither backend prints Python's answer, so neither may claim a match: interp=%v aot=%v", r.InterpMatchesPython, r.AOTMatchesPython)
	}
}

// TestOracleCheckFailsWhenADebtIsPaid is the ratchet: an unrecorded fix is as much a
// failure as a new divergence, because a ledger nobody maintains is a fiction.
func TestOracleCheckFailsWhenADebtIsPaid(t *testing.T) {
	c := ConformanceCase{
		ID: "programs/x", Name: "x.gy", Oracle: OracleDebt, Reason: "r", Ref: "L11.2",
		Pins: []OraclePin{{Backend: "interpreter", Stdout: "1\n"}, {Backend: "aot", Stdout: "1\n"}},
	}
	r := &ConformanceResult{
		InterpOK: true, InterpOut: "True\n",
		AOTOK: true, AOTOut: "True\n",
		PythonOK: true, PythonOut: "True\n",
	}
	drift := c.OracleCheck(r)
	joined := strings.Join(drift, " | ")
	if !strings.Contains(joined, "debt is paid") {
		t.Fatalf("a closed debt must fail with 'debt is paid', got %v", drift)
	}
	for _, want := range []string{"pin says the interpreter leg prints", "pin says the aot leg prints"} {
		if !strings.Contains(joined, want) {
			t.Errorf("the stale pins should also be reported (%s): %v", want, drift)
		}
	}
}

func TestOracleCheckPinsCatchEveryKindOfLegDrift(t *testing.T) {
	base := func(pins ...OraclePin) ConformanceCase {
		return ConformanceCase{ID: "p", Name: "p.gy", Oracle: OracleDebt, Reason: "r", Ref: "L", Pins: pins}
	}
	cases := []struct {
		name string
		c    ConformanceCase
		r    ConformanceResult
		want string
	}{
		{"stdout moved",
			base(OraclePin{Backend: "interpreter", Stdout: "1\n"}),
			ConformanceResult{InterpOK: true, InterpOut: "one\n", AOTOK: true, AOTOut: "1\n", PythonOK: true, PythonOut: "True\n"},
			"pin says the interpreter leg prints"},
		{"a leg that used to fail now runs",
			base(OraclePin{Backend: "aot", Missing: true}),
			ConformanceResult{InterpOK: true, InterpOut: "1\n", AOTOK: true, AOTOut: "1\n", PythonOK: true, PythonOut: "True\n"},
			"the refusal is gone"},
		{"a leg that used to run now fails",
			base(OraclePin{Backend: "interpreter", Stdout: "1\n"}),
			ConformanceResult{AOTOK: true, AOTOut: "1\n", PythonOK: true, PythonOut: "True\n"},
			"but it failed"},
		{"the failure changed shape",
			base(OraclePin{Backend: "aot", Missing: true, Err: "compiler panic"}),
			ConformanceResult{InterpOK: true, InterpOut: "1\n", AOTErr: "codegen: unsupported expression", PythonOK: true, PythonOut: "True\n"},
			"pin says the aot leg fails with"},
	}
	for _, tc := range cases {
		drift := tc.c.OracleCheck(&tc.r)
		if !strings.Contains(strings.Join(drift, " | "), tc.want) {
			t.Errorf("%s: want drift containing %q, got %v", tc.name, tc.want, drift)
		}
	}
}

func TestOracleCheckAppliesTheDeclaredComparisonRules(t *testing.T) {
	c := ConformanceCase{
		ID: "p", Name: "p.gy", Oracle: OracleMatch, Rules: []string{RuleSetOrder},
	}
	r := &ConformanceResult{
		InterpOK: true, InterpOut: "{3, 1, 2}\n",
		AOTOK: true, AOTOut: "{1, 2, 3}\n",
		PythonOK: true, PythonOut: "{2, 3, 1}\n",
	}
	if drift := c.OracleCheck(r); len(drift) != 0 {
		t.Fatalf("three orders of one set are one answer: %v", drift)
	}
	if r.Oracle != OracleMatch {
		t.Errorf("status = %q, want match", r.Oracle)
	}
	if !r.InterpMatchesPython || !r.AOTMatchesPython {
		t.Errorf("both legs should match under the rule: interp=%v aot=%v", r.InterpMatchesPython, r.AOTMatchesPython)
	}
	// The rule list the row was judged under is echoed into the result, so a reader
	// (or a stub check that drops the rule) can see exactly what was normalised.
	if !strings.Contains(strings.Join(r.OracleRules, ","), RuleSetOrder) {
		t.Errorf("the artifact should name the rules that were applied: %v", r.OracleRules)
	}
	extra := &ConformanceResult{}
	c.Rules = append(c.Rules, "not-a-real-rule")
	c.OracleCheck(extra)
	if !strings.Contains(strings.Join(extra.OracleRules, ","), "not-a-real-rule") {
		t.Errorf("a declared-but-unknown rule must still be visible, not silently dropped: %v", extra.OracleRules)
	}
}

func TestOracleVerdictVocabularyIsStable(t *testing.T) {
	// Agents branch on these strings in the matrix artifact and in --oracle JSON;
	// renaming one is a breaking change to the CLI's ABI.
	for got, want := range map[string]string{OracleMatch: "match", OracleDebt: "debt", OracleNA: "not_applicable"} {
		if got != want {
			t.Errorf("oracle constant = %q, want %q", got, want)
		}
	}
	if RuleSetOrder != "set-order" {
		t.Errorf("rule name = %q", RuleSetOrder)
	}
}

func TestSchemaDescribesTheOracleOutputs(t *testing.T) {
	var doc map[string]any
	if err := json.Unmarshal([]byte(ASTIRSchema), &doc); err != nil {
		t.Fatalf("--schema is not valid JSON: %v", err)
	}
	defs, _ := doc["definitions"].(map[string]any)
	for _, name := range []string{"conformanceRow", "oracleReport", "valueTag"} {
		if _, ok := defs[name]; !ok {
			t.Errorf("schema is missing definitions.%s — the oracle output would be undocumented", name)
		}
	}
}

func TestPythonRunRunsTheOracle(t *testing.T) {
	if _, err := exec.LookPath(PythonBinary()); err != nil {
		t.Skipf("no CPython available for the oracle leg: %v", err)
	}
	out, _, err := PythonRun("print(1 + 1)\n")
	if err != nil {
		t.Fatalf("oracle run: %v", err)
	}
	if out != "2\n" {
		t.Errorf("oracle stdout = %q, want 2\\n", out)
	}
	out, stderr, err := PythonRun("print(nope)\n")
	if err == nil {
		t.Fatalf("a NameError should be reported as a failed leg")
	}
	if out != "" {
		t.Errorf("stdout should be empty, got %q", out)
	}
	if !strings.Contains(stderr, "NameError") {
		t.Errorf("stderr should carry the traceback, got %q", stderr)
	}
}

func TestPythonBinaryIsOverridable(t *testing.T) {
	t.Setenv("GUSTY_PYTHON", "/opt/py3.13/bin/python3")
	if got := PythonBinary(); got != "/opt/py3.13/bin/python3" {
		t.Errorf("GUSTY_PYTHON override ignored: %q", got)
	}
	t.Setenv("GUSTY_PYTHON", "")
	if got := PythonBinary(); got != "python3" {
		t.Errorf("default oracle = %q, want python3", got)
	}
}
