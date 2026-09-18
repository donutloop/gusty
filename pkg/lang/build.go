package lang

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// BuildResult is the structured outcome of a multi-file build: the binary
// path, the emitted LLVM IR, the intermediate object file, the exact
// toolchain commands run, and any compiler diagnostics. It is JSON-serializable
// so agents/tooling can consume a build without scraping stderr.
type BuildResult struct {
	Output      string       `json:"output"`
	IR          string       `json:"ir"`
	Objects     []string     `json:"objects"`
	Commands    []string     `json:"commands"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Toolchain binaries used by Build. They are package-level so tests can point
// at alternate installs (e.g. a locally built llc).
var (
	llcCmd = "llc-20"
	ccCmd  = "cc"
)

// Build compiles a set of source files into a single native executable.
//
// Pipeline: read + parse each file, merge the statement lists into one
// program, run semantic analysis, codegen to LLVM IR, optimize, then drive the
// native toolchain — llc verifies/lowers the module to an object file, cc
// links it into the binary at out. Returns the structured outcome for machine
// consumption; on compile/link failure it returns a partial BuildResult plus
// an error carrying the failing tool's output.
func Build(files []string, out string, optLevel int) (*BuildResult, error) {
	prog := &Program{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("build: read %s: %w", f, err)
		}
		p, err := parseProgram(string(b))
		if err != nil {
			return nil, fmt.Errorf("build: parse %s: %w", f, err)
		}
		prog.Stmts = append(prog.Stmts, p.Stmts...)
	}

	diags := Analyze(prog)
	if anyErr(diags) {
		return &BuildResult{Output: out, Diagnostics: diags},
			fmt.Errorf("build: %d error(s) in sources", nErrs(diags))
	}

	ir, err := GenerateIR(prog)
	if err != nil {
		return nil, fmt.Errorf("build: codegen: %w", err)
	}
	ir = OptimizeIR(ir, optLevel)

	dir, err := os.MkdirTemp("", "gusty-build-")
	if err != nil {
		return nil, fmt.Errorf("build: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	irPath := filepath.Join(dir, "prog.ll")
	objPath := filepath.Join(dir, "prog.o")
	if err := os.WriteFile(irPath, []byte(ir), 0o600); err != nil {
		return nil, fmt.Errorf("build: write IR: %w", err)
	}

	llcCmdline := []string{"-relocation-model=pic", "-filetype=obj", irPath, "-o", objPath}
	if outLL, err := exec.Command(llcCmd, llcCmdline...).CombinedOutput(); err != nil {
		return &BuildResult{Output: out, IR: ir},
			fmt.Errorf("build: llc: %v\n%s", err, outLL)
	}

	ccCmdline := []string{objPath, "-o", out, "-lm"}
	if outCC, err := exec.Command(ccCmd, ccCmdline...).CombinedOutput(); err != nil {
		return &BuildResult{Output: out, IR: ir, Objects: []string{objPath}},
			fmt.Errorf("build: cc: %v\n%s", err, outCC)
	}

	return &BuildResult{
		Output:   out,
		IR:       ir,
		Objects:  []string{objPath},
		Commands: []string{
			llcCmd + " -relocation-model=pic -filetype=obj " + irPath + " -o " + objPath,
			ccCmd + " " + objPath + " -o " + out,
		},
	}, nil
}

func nErrs(diags []Diagnostic) int {
	n := 0
	for _, d := range diags {
		if d.Level == LevelError {
			n++
		}
	}
	return n
}
