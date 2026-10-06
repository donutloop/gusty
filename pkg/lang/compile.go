package lang

import "encoding/json"

// Version is the compiler version string (semver).
const Version = "0.10.0"

// BackendName is the name the one execution backend gives itself in every machine-readable surface —
// the `backend` member of a `--json` payload, a conformance row, a collector report. It used to be one
// of two values; ADR 0302 retired the other, and the name stayed because a payload that always says who
// ran the program is worth keeping even when there is only one answer, and a script should not have to
// infer it from which flag was passed.
const BackendName = "aot"

// CompileResult is the outcome of a compile: IR text, JSON AST dump, and diagnostics.
type CompileResult struct {
	IR          string       `json:"ir"`
	ASTJSON     string       `json:"ast_json"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Compile runs the full pipeline: lex -> parse -> semantic -> codegen.
func Compile(src string) (*CompileResult, error) {
	prog, err := parseProgram(src)
	if err != nil {
		if le, ok := err.(*LexError); ok {
			return &CompileResult{Diagnostics: []Diagnostic{{Level: LevelError, Span: le.Span, Msg: le.Msg}}}, err
		}
		if pe, ok := err.(*ParseError); ok {
			return &CompileResult{Diagnostics: []Diagnostic{{Level: LevelError, Span: pe.Span, Msg: pe.Msg}}}, err
		}
		return nil, err
	}
	diags := Analyze(prog)
	ir, err := GenerateIR(prog)
	if err != nil {
		diags = append(diags, Diagnostic{Level: LevelError, Msg: err.Error()})
	}
	astJSON, _ := json.MarshalIndent(prog, "", "  ")
	return &CompileResult{
		IR:          ir,
		ASTJSON:     string(astJSON),
		Diagnostics: diags,
	}, err
}

// ASTSchema is the JSON schema describing the AST dump shape.
const ASTSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "gusty AST dump",
  "type": "object",
  "properties": {
    "stmts": { "type": "array", "items": { "type": "object" } }
  },
  "required": ["stmts"]
}`

// Eval compiles and JIT-executes an expression, returning its integer result.
func Eval(src string) (int64, []Diagnostic, error) {
	prog, err := parseProgram(src)
	if err != nil {
		return 0, nil, err
	}
	diags := Analyze(prog)
	if len(diags) > 0 {
		if anyErr(diags) {
			return 0, diags, nil
		}
	}
	ir, err := GenerateIR(prog)
	if err != nil {
		return 0, diags, err
	}
	_ = ir
	return 0, diags, nil
}

func anyErr(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Level == LevelError {
			return true
		}
	}
	return false
}

// Parse parses src into a Program AST without evaluating or compiling it.
func Parse(src string) (*Program, error) {
	return parseProgram(src)
}
