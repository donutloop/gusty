package lang

import (
	"fmt"
	"os"
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
	// Verification is the LLVM module-verifier verdict for the linked module
	// (L8.2). nil means it was not run; .Skipped distinguishes "no toolchain"
	// from "verified", so an unverified build is never reported as verified.
	Verification *IRVerification `json:"verification,omitempty"`
	// Optimization says whether the *real* LLVM optimizer ran. nil means none was
	// requested (level 0). A non-nil report with Applied=false means the build
	// silently fell back to the textual pass — which used to be invisible (Gap J.4).
	Optimization *Optimization `json:"optimization,omitempty"`
	// Debug accounts for the DWARF line table the build was asked to carry (L8.5):
	// what the module emits and what the artifact actually contains. nil when the
	// build did not ask for debug info.
	Debug *DebugInfo `json:"debug,omitempty"`
	// DWARF is the line table read back out of the linked artifact with
	// llvm-dwarfdump. A build that asked for debug info and got none says so here
	// (`ok: false`) — the claim is taken from the binary, never from the flag
	// (ADR 0231, and ADR 0164's rule that an unread check is never reported as ok).
	DWARF *DWARFReport `json:"dwarf,omitempty"`
}

// Toolchain binaries. These are the DEFAULTS a run gets when it does not say otherwise; a run that wants a
// different one asks with ToolOverride rather than by writing here (see below).
var (
	llcCmd = "llc-20"
	ccCmd  = "cc"
	optCmd = "opt-20"
)

// ToolOverride names alternate toolchain binaries for ONE compile, link, optimise, verify or report call.
//
// The toolchain used to be only the three variables above, and the sole way to point a build at a different
// `llc` was to write them — which put every other compile in the process at the mercy of whoever last
// replaced one. It is the same shape ADR 0343 removed from the power predicates' name-reader, and it failed
// the same two ways: two cases overlapping raced on the variable (a stub `llc` installed by a toolchain-error
// case was read by an unrelated case mid-compile, which is how a refused program came to report "my favourite
// complaint" as its own error), and one case at a time was answered by whatever the last case left behind.
// An override is a value now, and the defaults above stay defaults.
type ToolOverride struct {
	LLC       string `json:"llc,omitempty"`
	CC        string `json:"cc,omitempty"`
	Opt       string `json:"opt,omitempty"`
	DWARFDump string `json:"dwarfDump,omitempty"`
}

// toolNames is a resolved toolchain: the binary every step of one run will exec, defaults filled in.
type toolNames struct {
	llc, cc, opt, dwarfdump string
}

func defaultToolNames() toolNames {
	return toolNames{llc: llcCmd, cc: ccCmd, opt: optCmd, dwarfdump: dwarfdumpCmd}
}

// toolNamesWithoutOpt is the toolchain with the LLVM optimizer taken away — the shape that asks what a build
// does when `opt` was never discovered. It cannot be spelled with a ToolOverride because an empty field there
// means "keep the default", and absence is a different question.
func toolNamesWithoutOpt() toolNames {
	t := defaultToolNames()
	t.opt = ""
	return t
}

// toolNamesFor resolves an override against the defaults. An empty field means "the default", which is what
// lets a case replace only the tool its question is about.
func toolNamesFor(o *ToolOverride) toolNames {
	t := defaultToolNames()
	if o == nil {
		return t
	}
	if o.LLC != "" {
		t.llc = o.LLC
	}
	if o.CC != "" {
		t.cc = o.CC
	}
	if o.Opt != "" {
		t.opt = o.Opt
	}
	if o.DWARFDump != "" {
		t.dwarfdump = o.DWARFDump
	}
	return t
}

// tools answers "which toolchain did this build ask for?", nil-safe so a caller that passed no options at all
// gets the process defaults.
func (o *BuildOptions) tools() *ToolOverride {
	if o == nil {
		return nil
	}
	return o.Tools
}

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
	// Tools points this build at alternate toolchain binaries without editing the process's defaults.
	Tools        *ToolOverride
	Debug        bool   // emit DWARF line records into the module so the binary carries a .debug_line table (L8.5)
	SourceMapOut string // write a JSON source map (source fn -> IR symbol+line)
	Shared       bool   // emit a position-independent shared object (.so/.dylib) with the stable extern-fn ABI
	// NoVerify skips the LLVM module-verifier stage (L8.2). Verification is on by
	// default: the module that gets linked is the module LLVM checks, and a
	// verifier failure here names the compiler stage that produced it instead of
	// surfacing as an `llc` failure inside a link step.
	NoVerify bool
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
// Debug emits DWARF line records into the module — the compile unit, a
// `DISubprogram` per function, and a `!DILocation` on every instruction of program
// code — because `llc`, not the link step, is what writes a `.debug_line` table, and it
// can only write what the module said (L8.5, ADR 0231). The verdict is read back out
// of the artifact into BuildResult.DWARF. SourceMapOut writes a JSON source map
// (source function -> emitted LLVM symbol + IR line, and since version 2 the
// IR-line-to-source-line table), and Shared emits a `.so`/`.dylib` carrying the
// stable gusty extern-fn ABI (see docs/abi.md).
func BuildWithOptions(files []string, out string, optLevel int, opts *BuildOptions) (*BuildResult, error) {
	// The toolchain is resolved once for the build and carried by it, so two builds in one process can
	// name different tools (ToolOverride).
	tools := toolNamesFor(opts.tools())
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

	// A debug build asks codegen for the line table, so the module that gets linked is
	// the module that carries it, and BuildResult.IR shows exactly that (L8.5, ADR 0231).
	var irOpts *IRGenOptions
	if opts != nil && opts.Debug {
		irOpts = &IRGenOptions{Debug: (&DebugOptions{OptLevel: optLevel}).FromFile(files[0])}
	}
	ir, dbgInfo, err := GenerateIRReport(prog, irOpts)
	if err != nil {
		// Carry the source diagnostics even though codegen failed: a build that
		// dies in codegen while the program also carries warnings must show both,
		// in the human output and in --json (roadmap Gap K.7).
		// GenerateIRReport's own message already begins with "codegen:", so the prefix
		// here is just "build:" — the human line reads
		//   gustyc: build: codegen: unsupported call "enumerate"
		// rather than repeating the stage twice.
		return &BuildResult{Output: out, Diagnostics: diags}, fmt.Errorf("build: %w", err)
	}
	ir, optRep := optimizeIRReportWith(ir, optLevel, tools)

	// L8.2: verify the module that is about to be linked with LLVM's own module
	// verifier (opt -passes=verify, falling back to llc -filetype=null). Doing it
	// here rather than letting llc notice later attributes a codegen bug to
	// codegen, and the verdict travels with the build result for tooling.
	var verification *IRVerification
	if opts == nil || !opts.NoVerify {
		v, verr := verifyModuleIRWith(ir, optLevel, tools)
		verification = v
		if verr != nil {
			return &BuildResult{Output: out, IR: ir, Diagnostics: diags, Verification: v, Optimization: optRep},
				fmt.Errorf("build: %w", verr)
		}
	}

	if opts != nil && opts.SourceMapOut != "" {
		sm, err := GenerateSourceMap(prog, ir, dbgInfo)
		if err != nil {
			return &BuildResult{Output: out, IR: ir, Diagnostics: diags, Optimization: optRep, Debug: dbgInfo}, fmt.Errorf("build: source map: %w", err)
		}
		if err := os.WriteFile(opts.SourceMapOut, sm, 0o644); err != nil {
			return &BuildResult{Output: out, IR: ir, Diagnostics: diags, Optimization: optRep, Debug: dbgInfo}, fmt.Errorf("build: write source map: %w", err)
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

	if outLL, err := runToolStage(ToolBudget, "llc", tools.llc, llcCmdline...); err != nil {
		return &BuildResult{Output: out, IR: ir, Diagnostics: diags, Optimization: optRep},
			fmt.Errorf("build: llc: %w\n%s", err, outLL)
	}

	ccCmdline := []string{objPath, "-o", out, "-lm"}
	ccLabel := tools.cc + " " + objPath + " -o " + out
	if opts != nil && opts.Shared {
		// Position-independent shared library: -fPIC + -shared. The object is
		// already PIC (llc -relocation-model=pic); -fPIC at link is belt-and-
		// suspenders so the .so/.dylib can be loaded at any address.
		ccCmdline = []string{"-shared", "-fPIC", objPath, "-o", out, "-lm"}
		ccLabel = tools.cc + " -shared -fPIC " + objPath + " -o " + out
	}
	if opts != nil && opts.Debug {
		// Harmless at the link, and honest about intent: the DWARF is already in the
		// object, because llc wrote it out of the module's !dbg records.
		ccCmdline = append(ccCmdline, "-g")
	}
	if outCC, err := runToolStage(ToolBudget, "cc", tools.cc, ccCmdline...); err != nil {
		return &BuildResult{Output: out, IR: ir, Objects: []string{objPath}, Debug: dbgInfo, Optimization: optRep},
			fmt.Errorf("build: cc: %w\n%s", err, outCC)
	}

	// Read the line table out of the artifact rather than asserting it. llc is what
	// writes DWARF, and it only writes what the module said: a `--debug` build whose
	// object carries no `.debug_line` rows must report that, because for the whole
	// life of this flag it linked with `-g` and docs claimed a line table existed
	// that nothing had ever read (L8.5, ADR 0231).
	var dwarfRep *DWARFReport
	if opts != nil && opts.Debug {
		dwarfRep, _ = dwarfLineTableWith(objPath, tools)
	}

	return &BuildResult{
		Output:  out,
		IR:      ir,
		Objects: []string{objPath},
		Commands: []string{
			llcCmd + " -relocation-model=pic -filetype=obj " + irPath + " -o " + objPath,
			ccLabel,
		},
		Shared:       opts != nil && opts.Shared,
		Verification: verification,
		Optimization: optRep,
		Debug:        dbgInfo,
		DWARF:        dwarfRep,
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
