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
	Shared      bool         `json:"shared"` // true when a position-independent shared library was emitted (L10.3)
}

// Toolchain binaries used by Build. They are package-level so tests can point
// at alternate installs (e.g. a locally built llc).
var (
	llcCmd = "llc-20"
	ccCmd  = "cc"
	optCmd = "opt-20"
)

// Build compiles a set of source files into a single native executable.
//
// Pipeline: read + parse each file, merge the statement lists into one
// program, run semantic analysis, codegen to LLVM IR, optimize, then drive the
// native toolchain — llc verifies/lowers the module to an object file, cc
// links it into the binary at out. Returns the structured outcome for machine
// consumption; on compile/link failure it returns a partial BuildResult plus
// an error carrying the failing tool's output.
// BuildOptions controls optional debug-symbol / source-map emission and the
// AOT build mode (executable vs position-independent shared library).
type BuildOptions struct {
	Debug        bool   // pass -g to llc/cc so the binary carries DWARF info
	SourceMapOut string // write a JSON source map (source fn -> IR symbol+line)
	Shared       bool   // emit a position-independent shared object (.so/.dylib) with the stable extern-fn ABI
}

// Build compiles with the default BuildOptions (native executable).
func Build(files []string, out string, optLevel int) (*BuildResult, error) {
	return BuildWithOptions(files, out, optLevel, nil)
}

// BuildShared compiles files into a position-independent shared library
// (.so/.dylib) carrying the stable gusty extern-fn ABI. It is the L10.3
// "shared-library export" mode: the same IR/codegen/optimize pipeline as
// Build, but linked with `cc -shared -fPIC` instead of producing a native
// executable, so the result can be dlopen'd / linked against from any host.
func BuildShared(files []string, out string, optLevel int) (*BuildResult, error) {
	return BuildWithOptions(files, out, optLevel, &BuildOptions{Shared: true})
}

// BuildWithOptions compiles files into the executable (or, when opts.Shared
// is set, a position-independent shared library) at out. When opts is non-nil,
// Debug adds DWARF debug info to the binary, SourceMapOut writes a JSON source
// map (source function -> emitted LLVM symbol + IR line), and Shared emits a
// `.so`/`.dylib` carrying the stable gusty extern-fn ABI (see docs/abi.md).
func BuildWithOptions(files []string, out string, optLevel int, opts *BuildOptions) (*BuildResult, error) {
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

	if opts != nil && opts.SourceMapOut != "" {
		sm, err := GenerateSourceMap(prog, ir)
		if err != nil {
			return &BuildResult{}, fmt.Errorf("build: source map: %w", err)
		}
		if err := os.WriteFile(opts.SourceMapOut, sm, 0o644); err != nil {
			return &BuildResult{}, fmt.Errorf("build: write source map: %w", err)
		}
	}

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
	ccLabel := ccCmd + " " + objPath + " -o " + out
	if opts != nil && opts.Shared {
		// Position-independent shared library: -fPIC + -shared. The object is
		// already PIC (llc -relocation-model=pic); -fPIC at link is belt-and-
		// suspenders so the .so/.dylib can be loaded at any address.
		ccCmdline = []string{"-shared", "-fPIC", objPath, "-o", out, "-lm"}
		ccLabel = ccCmd + " -shared -fPIC " + objPath + " -o " + out
	}
	if opts != nil && opts.Debug {
		ccCmdline = append(ccCmdline, "-g")
	}
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
			ccLabel,
		},
		Shared: opts != nil && opts.Shared,
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
