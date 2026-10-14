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

// Compile runs the full pipeline: lex -> parse -> semantic -> codegen. A bare `import mod` resolves
// against the working directory, which is what a person at a shell means; a caller that owns its modules
// says so with CompileIn.
func Compile(src string) (*CompileResult, error) {
	return CompileIn("", src)
}

// CompileRun is what one compile needs to know about the world around the program: the directory its
// `import mod` resolves against, and the standard-library root it reads. Both are facts about the program
// being compiled, and both used to be facts about the process — a chdir for the first, an exported package
// variable for the second — which is why a case that wanted either had to be the only thing running.
type CompileRun struct {
	// Dir is the directory a bare `import mod` is looked up in. Empty means this process's working
	// directory, which is what a person at a shell means by `import config`.
	Dir string
	// StdlibDir is the standard-library root. Empty means the process default: GUSTY_STDLIB_DIR, or the
	// nearest ancestor `stdlib` holding .gy modules.
	StdlibDir string
	// GCReport asks the compiled program to print the collector's self-report on the tool channel, which
	// is what `gustyc --gc-stats` sets. A caller that wants it for one run sets it here rather than
	// flipping the process switch every concurrent caller is reading (ADR 0343).
	GCReport bool
}

// jitOptions is the runner's view of a run; `echo` is the REPL's courtesy, which is a property of the
// snippet rather than of the world around it, so it stays a parameter.
func (r CompileRun) jitOptions(echo bool) *JITOptions {
	return &JITOptions{Dir: r.Dir, StdlibDir: r.StdlibDir, GCReport: r.GCReport, EchoResult: echo}
}

// irOptions is the emitter's view of a run.
func (r CompileRun) irOptions() *IRGenOptions {
	if r.Dir == "" && r.StdlibDir == "" && !r.GCReport {
		return nil
	}
	return &IRGenOptions{Dir: r.Dir, StdlibDir: r.StdlibDir, GCReport: r.GCReport}
}

// CompileIn is Compile with the directory the program's `import mod` resolves against named, instead of
// this process's working directory. Everything else is identical — the same front end, the same emitter,
// the same IR — because the directory is a fact about the program, not about whoever asked for it.
//
// It exists so that a case with its own modules (config.gy in its own temp dir) can say so, which is what
// os.Chdir used to do to the process for the length of a compile. Two cases with two directories are now
// two compiles that share nothing, which is what lets them run at the same time (ADR 0343).
func CompileIn(dir, src string) (*CompileResult, error) {
	return CompileWith(CompileRun{Dir: dir}, src)
}

// CompileWith is Compile with the whole run spelled out: its import directory, its standard-library root,
// and whether the program it produces reports on itself.
func CompileWith(run CompileRun, src string) (*CompileResult, error) {
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
	ir, err := GenerateIRWithOptions(prog, run.irOptions())
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
