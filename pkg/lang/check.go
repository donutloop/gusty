package lang

import (
	"fmt"
	"os"
)

// CheckResult is the structured outcome of a type-check (the `gusty check`
// / `--check` mode). It carries the files checked, every diagnostic (span +
// message + level), a boolean "ok", and a deterministic exit code so agents
// can consume it as JSON without scraping stderr.
type CheckResult struct {
	Files       []string     `json:"files"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	OK          bool         `json:"ok"`
	Exit        int          `json:"exit"`
}

// CheckSource parses and type-checks a single source string (mypy-style:
// annotated code is checked without being executed). A parse error returns a
// non-nil error; type diagnostics are returned in the result.
func CheckSource(src string) (*CheckResult, error) {
	prog, err := Parse(src)
	if err != nil {
		return nil, fmt.Errorf("check: parse: %w", err)
	}
	diags := Analyze(prog)
	return buildResult([]string{"<src>"}, diags), nil
}

// CheckFile parses and type-checks a single source file on disk.
func CheckFile(path string) (*CheckResult, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("check: read %s: %w", path, err)
	}
	prog, err := Parse(string(b))
	if err != nil {
		return nil, fmt.Errorf("check: parse %s: %w", path, err)
	}
	diags := Analyze(prog)
	return buildResult([]string{path}, diags), nil
}

// CheckFiles type-checks several files and aggregates all diagnostics, one
// result per file. It returns nil on the first parse/read error.
func CheckFiles(files []string) (*CheckResult, error) {
	var diags []Diagnostic
	for _, f := range files {
		r, err := CheckFile(f)
		if err != nil {
			return nil, err
		}
		diags = append(diags, r.Diagnostics...)
	}
	return buildResult(files, diags), nil
}

// buildResult derives the OK flag and deterministic exit code from diags.
func buildResult(files []string, diags []Diagnostic) *CheckResult {
	ok := true
	for _, d := range diags {
		if d.Level == LevelError {
			ok = false
			break
		}
	}
	exit := 0
	if !ok {
		exit = 1
	}
	return &CheckResult{Files: files, Diagnostics: diags, OK: ok, Exit: exit}
}
