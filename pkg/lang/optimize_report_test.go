package lang

import (
	"strings"
	"testing"
)

// Gap J.4 — the optimizer used to fail silently: if `opt-20` was missing (or rejected the
// module) the build shipped the un-optimized module and still said "built". The report is
// now part of BuildResult / --json, so a script can tell an optimized build from one that
// quietly fell back to the textual pass.

func TestOptimizationReportWhenOptToolIsMissing(t *testing.T) {
	saved := optCmd
	defer func() { optCmd = saved }()

	optCmd = "/nonexistent/opt-20"
	src := "def add(a, b):\n    return a + b\n\nprint(add(2, 3))\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	ir, rep := OptimizeIRReport(res.IR, 2)
	if rep == nil {
		t.Fatalf("a level>0 build must report what the optimizer did")
	}
	if rep.Applied {
		t.Errorf("Applied must be false when the tool is missing: %+v", rep)
	}
	if rep.Fallback != "textual" {
		t.Errorf("Fallback should name what ran instead, got %q", rep.Fallback)
	}
	if rep.Note == "" {
		t.Errorf("the report should say why, so the fall-back is not silent: %+v", rep)
	}
	if ir == "" || !strings.Contains(ir, "define ") {
		t.Errorf("the textual pass must still produce a module")
	}
}

func TestOptimizationReportWhenOptToolIsAbsent(t *testing.T) {
	saved := optCmd
	defer func() { optCmd = saved }()

	optCmd = "" // no toolchain discovered at all
	res, err := Compile("print(1)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, rep := OptimizeIRReport(res.IR, 2)
	if rep == nil || rep.Applied || rep.Fallback != "textual" {
		t.Errorf("missing toolchain must report applied=false fallback=textual, got %+v", rep)
	}
	if !strings.Contains(rep.Note, "NOT LLVM-optimized") {
		t.Errorf("the note should say the module is not LLVM-optimized: %q", rep.Note)
	}
}

func TestOptimizationReportAbsentWhenNotRequested(t *testing.T) {
	res, err := Compile("print(1)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, rep := OptimizeIRReport(res.IR, 0); rep != nil {
		t.Errorf("level 0 asks for nothing, so there is no skipped step to report: %+v", rep)
	}
}

func TestOptimizationReportWhenOptRuns(t *testing.T) {
	if findTool("opt-20") == "" {
		t.Skip("opt-20 not installed")
	}
	res, err := Compile("def add(a, b):\n    return a + b\n\nprint(add(2, 3))\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	_, rep := OptimizeIRReport(res.IR, 2)
	if rep == nil || !rep.Applied {
		t.Fatalf("with opt-20 installed the real pipeline should run: %+v", rep)
	}
	if rep.Pipeline != "-O2" || rep.Tool == "" {
		t.Errorf("report should name the tool and pipeline, got %+v", rep)
	}
}

// TestBuildCarriesOptimizationReport: the whole point is that the *build result* tells the
// story, not just the optimizer internals.
func TestBuildCarriesOptimizationReport(t *testing.T) {
	dir := t.TempDir()
	src := writeTemp(t, dir, "opt.gy", "def add(a, b):\n    return a + b\n\nprint(add(2, 3))\n")
	res, err := BuildWithOptions([]string{src}, dir+"out", 2, nil)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if res.Optimization == nil {
		t.Fatalf("a --opt-level=2 build must report what the optimizer did")
	}
	if !res.Optimization.Applied {
		t.Errorf("opt-20 is installed here, so Applied should be true: %+v", res.Optimization)
	}
	if res.Optimization.Pipeline != "-O2" {
		t.Errorf("pipeline = %q, want -O2", res.Optimization.Pipeline)
	}
}
