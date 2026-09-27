package lang

import (
	"errors"
	"fmt"
	"os"
	"sort"
)

// CodeParseError is the stable rule code for a diagnostic that came from the
// parser or from a lexer error token that recovery turned into a diagnostic.
// It lets an agent tell "the source did not parse" from a semantic failure
// without matching on message prose.
const CodeParseError = "parse.error"

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

// checkParseErrors turns a failed parse into diagnostics instead of a Go error.
// Lexer-recovered diagnostics are NOT added here: Analyze already reports
// prog.Diags, and adding them twice shows the same squiggle twice.
// The lexer recovers (L4.1) and the parser reports a forest, so a document with
// five broken lines has five spans worth of information — and the statements
// that DID parse carry semantic diagnostics too. Returning an error here threw
// all of that away and left the CLI printing one prose string, so `--json check`
// emitted no JSON at all.
func checkParseErrors(err error) []Diagnostic {
	if err == nil {
		return nil
	}
	var diags []Diagnostic
	var pes *ParseErrors
	if errors.As(err, &pes) {
		for _, pe := range pes.Errors {
			diags = append(diags, Diagnostic{
				Level: LevelError, Span: pe.Span, Msg: pe.Msg, Code: CodeParseError,
			})
		}
		return diags
	}
	diags = append(diags, Diagnostic{Level: LevelError, Msg: err.Error(), Code: CodeParseError})
	return diags
}

// CheckSource parses and type-checks a single source string (mypy-style:
// annotated code is checked without being executed). Parse failures are
// reported as diagnostics (every recovered error, with its span), not as a Go
// error, so the human and JSON paths both get the full picture.
func CheckSource(src string) (*CheckResult, error) {
	prog, perr := Parse(src)
	diags := checkParseErrors(perr)
	// Recovery means the statements that DID parse are still a program worth
	// checking: stopping at the first bad line would hide every type error below
	// it, which is the "fix one error, discover the next" loop we are trying to
	// end. Analyze is safe on a partially parsed program because recovery only
	// ever yields complete statements.
	diags = append(diags, Analyze(prog)...)
	return buildResult([]string{"<src>"}, diags), nil
}

// CheckFile parses and type-checks a single source file on disk.
func CheckFile(path string) (*CheckResult, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("check: read %s: %w", path, err)
	}
	prog, perr := Parse(string(b))
	diags := checkParseErrors(perr)
	diags = append(diags, Analyze(prog)...)
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
// Diagnostics are ordered by position, not by which pass found them: parse
// errors come from one collector and semantic ones from another, and an
// interleaved document-order list is what a human (and a diff) expects.
func buildResult(files []string, diags []Diagnostic) *CheckResult {
	sort.SliceStable(diags, func(i, j int) bool {
		if diags[i].Span.Line != diags[j].Span.Line {
			return diags[i].Span.Line < diags[j].Span.Line
		}
		if diags[i].Span.Col != diags[j].Span.Col {
			return diags[i].Span.Col < diags[j].Span.Col
		}
		return diags[i].Msg < diags[j].Msg
	})
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
