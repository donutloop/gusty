// Command gustyc is a CLI and REPL for the gusty language.
//
// Flags (stable, agent-friendly):
//
//	--eval <src>     evaluate a source string and print the result
//	--file <path>    read and evaluate a source file
//	--verify <src>   parse + analyze, report diagnostics, exit by code
//	--emit-llvm <src>   print LLVM IR
//	--emit-ast <src>    print the AST as JSON
//	--target <triple>   target triple for codegen (informational)
//	--opt-level <n>     optimization level (informational)
//	--lang              list supported language features (self-describing)
//	--oracle <src>      run the compiled backend and CPython and compare (exit 6 divergence, 7 no verdict)
//	--variance          print the generic variance table as JSON (L6.6)
//	--effects <src>     print each function's effect signature (awaits/yields/raises, return + termination shape) as JSON (L7.6)
//	--version           print version
//	--repl              start an interactive REPL (default when stdin is a TTY)
//	--help              show usage
//
// Exit codes (docs/operations.md § Exit codes): 0 = ok, 1 = compile error
// (parse/analysis/codegen/link), 2 = LLVM rejected the module we emitted (a
// compiler bug, not a source error), 3 = the program ran and trapped,
// 4 = CLI usage error, 5 = benchmark regression (--bench-baseline gate),
// 6 = the oracle leg says gusty printed something other than CPython
// (--oracle/--oracle-file), 7 = the oracle could not run the source, so there is
// no verdict (--oracle on gusty-only surface).
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/donutloop/gusty/pkg/lang"
)

// Exit codes are the CLI's ABI for scripts and agents (docs/operations.md
// § Exit codes). Each failure *class* has its own code so a caller can branch
// without scraping stderr: a program that trapped is not the same event as a
// compiler that failed, and a wrongly-used CLI is neither.
const exitOK = 0

// exitCompileError: the program never ran — parse, analysis, codegen, or the
// native toolchain (llc/cc) rejected it. Diagnostics were emitted.
const exitCompileError = 1

// exitIRVerify: LLVM's own module verifier rejected the module we emitted.
// That is a compiler bug, not a source error, so it must be distinguishable
// from "your program has an error in it" (see ADR 0164/0166).
const exitIRVerify = 2

// exitRuntime: the program compiled and ran, then trapped (an uncaught
// exception, a failed built-in). Previously this shared code 1 with compile
// errors, so a script could not tell "my program crashed" from "the compiler
// broke" — the whole point of an exit status (roadmap Gap J.3).
const exitRuntime = 3

// exitUsage: the CLI itself was used wrongly — unknown/missing flags, no input,
// an unreadable source file, an empty benchmark directory.
const exitUsage = 4

// exitBenchRegression is returned by the benchmark gate when a measured case is
// slower than its baseline by more than --bench-tolerance. It is distinct from
// the compile/verify/runtime codes so CI can tell "the compiler got slower"
// apart from "the program is broken".
const exitBenchRegression = 5

// exitOracleDivergence is returned by --oracle when the program compiled and ran
// but printed something CPython does not: the compiled-or-interpreted answer is
// wrong, or one leg refused it. It is not a compile error (1) and not a crash (3)
// — the program was accepted and executed, and the *answer* is the finding
// (roadmap L11.9, ADR 0186).
const exitOracleDivergence = 6

// exitOracleNoVerdict is returned by --oracle when the CPython leg could not run
// the source at all (gusty-only syntax, a stdlib attribute Python has no name for),
// so there is no third opinion and therefore no verdict. Reporting this as success
// would let "we never checked" read as "it matches".
const exitOracleNoVerdict = 7

// exitToolchainTimeout is returned when an external toolchain call — llc, cc, opt, the
// llvm-as/lli the harness runs, or the CPython oracle — ran out of its budget and was killed
// (pkg/lang/tool_budget.go). Neither 1 nor 2 describes it: the program did nothing wrong, and the
// compiler is not accused of emitting a bad module either — the tool stopped answering. A caller that
// sees 8 looks at the machine, or raises GUSTY_TOOL_TIMEOUT, instead of at its own source.
const exitToolchainTimeout = 8

const exitNotCanonical = 1

func main() {
	os.Exit(run())
}

func run() int {
	// ContinueOnError rather than ExitOnError: the flag package's own exit status is 2,
	// which in this CLI means "LLVM rejected our module". A wrongly-typed command line is
	// a usage error (4), so the parse failure has to be handled here to say that
	// (roadmap Gap J.3).
	fs := flag.NewFlagSet("gustyc", flag.ContinueOnError)
	benchSrc := fs.String("bench", "", "benchmark a source program through the compiled backend (compile, link, run)")
	benchFile := fs.String("bench-file", "", "benchmark a source file")
	benchRuns := fs.Int("bench-runs", 3, "runs per measurement for benchmarks")
	benchOpt := fs.Int("bench-opt", 2, "AOT optimization level for benchmarks")
	benchSuite := fs.Bool("bench-suite", false, "benchmark the built-in corpus through the compiled backend")
	benchDir := fs.String("bench-dir", "", "benchmark every *.gy in a directory (e.g. integration/programs)")
	benchBaseline := fs.String("bench-baseline", "", "path to a baseline JSON to gate against")
	benchBaselineUpdate := fs.String("bench-baseline-update", "", "write the measured suite as a baseline JSON to this path")
	benchTolerance := fs.Float64("bench-tolerance", lang.DefaultBenchTolerance, "slowdown multiplier above which a case is a regression")
	benchMinMs := fs.Float64("bench-min-ms", lang.DefaultBenchMinMs, "ignore baseline times below this many ms (noise floor)")
	benchGate := fs.String("bench-gate", lang.BenchGateAOT, "which measurement the regression gate watches: aot is the only gate since the interpreter was retired (ADR 0302); any other name is a usage error")
	evalSrc := fs.String("eval", "", "evaluate a source string")
	file := fs.String("file", "", "read and evaluate a source file")
	verify := fs.String("verify", "", "parse + analyze a source string")
	check := fs.String("check", "", "type-check a source string without executing (mypy-style)")
	effects := fs.String("effects", "", "print each function's effect signature (awaits / yields / raises, whether it returns a value, whether its control flow can fall off the end) for a source string; --json for the machine document, `gustyc effects <file>...` for files (L7.6)")
	oracleSrc := fs.String("oracle", "", "run a source string through the compiled backend and CPython and report whether gusty behaves like Python (exit 6 divergence, 7 no verdict; --json: the leg-by-leg report)")
	oracleFile := fs.String("oracle-file", "", "same as --oracle, for a source file")
	emitLLVMF := fs.String("emit-llvm", "", "print LLVM IR for a source string")
	emitSourceMapF := fs.String("emit-source-map", "", "print the source-map JSON for a source file (functions and, since version 2, the IR-line-to-source-line table)")
	emitASTF := fs.String("emit-ast", "", "print the AST as JSON for a source string")
	target := fs.String("target", "", "target triple for codegen")
	optLevel := fs.String("opt-level", "0", "optimization level")
	buildOut := fs.String("build", "", "output binary path for a multi-file build (sources are the positional args)")
	noVerify := fs.Bool("no-verify", false, "skip LLVM's module verifier during --build (on by default, L8.2)")
	verifyLLVMF := fs.String("verify-llvm", "", "compile a source string and report LLVM's module-verifier verdict")
	verifyLLVMFile := fs.String("verify-llvm-file", "", "compile a source file and report LLVM's module-verifier verdict")
	debugFlag := fs.Bool("debug", false, "compile with DWARF: the module carries !dbg line records and the binary gets a .debug_line table (read back with llvm-dwarfdump and reported; L8.5)")
	debugInfoF := fs.String("debug-info", "", "print the DWARF line-table document for a source string: compile unit, one entry per function, IR-line-to-source-line rows (--json for the machine document; L8.5)")
	debugInfoFile := fs.String("debug-info-file", "", "same as --debug-info, for a source file")
	sourceMapOut := fs.String("source-map-out", "", "write a JSON source map (source fn -> IR symbol+line) to this path")
	jsonOut := fs.Bool("json", false, "emit results/diagnostics as JSON")
	langCmd := fs.Bool("lang", false, "list supported language features")
	schemaCmd := fs.Bool("schema", false, "print the machine-readable JSON schema for the AST/IR dumps")
	varianceCmd := fs.Bool("variance", false, "print the generic variance table as JSON (list/set/dict invariant, Sequence/iter/tuple covariant, Callable parameters contravariant, classes nominal)")
	abiCmd := fs.Bool("abi", false, "print the versioned gusty extern-fn C ABI schema (JSON)")
	sharedCmd := fs.Bool("shared", false, "emit a position-independent shared library (.so/.dylib) with the stable extern-fn ABI instead of a native executable (with --build)")
	jit := fs.Bool("jit", false, "accepted for compatibility: the compiled LLVM backend is the only backend (ADR 0302)")
	aot := fs.Bool("aot", false, "accepted for compatibility: the compiled LLVM backend is the only backend (ADR 0302)")
	interp := fs.Bool("interp", false, "retired: the AST interpreter left with ADR 0302; passing it is a usage error")
	showBackend := fs.Bool("show-backend", false, "print which backend executed the program (stderr; --json reports it in the payload) — there is one answer, and the payload still names it")
	gcStats := fs.Bool("gc-stats", false, "report what the garbage collector did while the program ran (collections, roots traced, roots skipped as immediates, objects freed) on stderr; --json adds a gc object to the payload")
	version := fs.Bool("version", false, "print version")
	repl := fs.Bool("repl", false, "start an interactive REPL")
	lsp := fs.Bool("lsp", false, "run the language server over stdio (LSP)")
	help := fs.Bool("help", false, "show usage")

	fmtSrc := fs.String("fmt", "", "format a source string to canonical gusty source (machine: deterministic stdout)")
	fmtCheck := fs.Bool("fmt-check", false, "verify a source is already canonical; exit 0 if canonical, 1 if not (with --json: machine report)")
	stdlibDir := fs.String("stdlib", "", "standard-library root directory (default: GUSTY_STDLIB_DIR or a discovered ./stdlib)")
	fmtFile := fs.String("fmt-file", "", "path to a source file to format/check (alternative to --file with --fmt)")
	// Flags are accepted after positional args too: `gustyc --build out src.gy
	// --opt-level=2` is what people and agents actually type, and Go's flag package stops
	// at the first positional, turning the trailing flag into a source filename.
	if perr := fs.Parse(reorderFlags(fs, os.Args[1:])); perr != nil {
		if perr == flag.ErrHelp {
			return exitOK // --help was handled by the flag package
		}
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", perr)
		usage(fs)
		return exitUsage
	}

	if *stdlibDir != "" {
		lang.SetStdlibDir(*stdlibDir)
	}

	if err := retiredBackendFlags(aot, jit, interp); err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitUsage
	}

	if *lsp {
		lang.RunLSP(os.Stdin, os.Stdout)
		return exitOK
	}

	if *fmtSrc != "" || *fmtCheck || *fmtFile != "" {
		return runFmt(*fmtSrc, *fmtCheck, *fmtFile, *jsonOut)
	}

	if *help {
		usage(fs)
		return exitOK
	}
	if *version {
		fmt.Println("gustyc " + lang.Version)
		return exitOK
	}
	if *langCmd {
		listLang()
		return exitOK
	}
	if *schemaCmd {
		fmt.Println(lang.ASTIRSchema)
		return exitOK
	}
	if *abiCmd {
		abiSchema, err := lang.ABISchema()
		if err != nil {
			fmt.Fprintln(os.Stderr, "gustyc: abi: "+err.Error())
			return exitCompileError
		}
		fmt.Println(abiSchema)
		return exitOK
	}
	if *varianceCmd {
		doc, err := lang.VarianceJSON()
		if err != nil {
			fmt.Fprintln(os.Stderr, "gustyc: variance: "+err.Error())
			return exitCompileError
		}
		fmt.Println(doc)
		return exitOK
	}

	// `gustyc prog.gy` is the most obvious way to run this compiler, and the usage line has
	// always advertised `[<src>]` (with --file documented as its alias) — but a positional
	// argument reached no branch at all and fell through to the usage text, exiting 0 without
	// compiling anything. A positional naming a file on disk is exactly --file; anything else
	// is source text, like --eval.
	if fs.NArg() == 1 && *file == "" && *emitLLVMF == "" && *buildOut == "" {
		if st, statErr := os.Stat(fs.Arg(0)); statErr == nil && !st.IsDir() {
			*file = fs.Arg(0)
		} else if strings.HasSuffix(fs.Arg(0), ".gy") {
			// A .gy name that does not exist is a mistyped path, not a program. Reading it
			// as source produced "undefined name prog" for `gustyc prog.gy` in the wrong
			// directory — a runtime error blaming the user's code for a shell mistake.
			fmt.Fprintf(os.Stderr, "gustyc: no such file: %s\n", fs.Arg(0))
			return exitUsage
		} else if *evalSrc == "" && !*repl && !*jit {
			*evalSrc = fs.Arg(0)
		}
	}

	if *buildOut != "" {
		buildFiles := fs.Args()
		if len(buildFiles) == 0 {
			fmt.Fprintf(os.Stderr, "gustyc: --build requires at least one source file\n")
			usage(fs)
			return exitUsage
		}
		res, err := lang.BuildWithOptions(buildFiles, *buildOut, atoi(*optLevel), &lang.BuildOptions{Debug: *debugFlag, SourceMapOut: *sourceMapOut, Shared: *sharedCmd, NoVerify: *noVerify})
		if err != nil {
			// machine mode: still emit the (partial) BuildResult carrying
			// diagnostics on stdout, plus a human error on stderr.
			if *jsonOut && res != nil {
				b, jerr := json.Marshal(res)
				if jerr == nil {
					fmt.Println(string(b))
				}
			}
			// Print the failure reason *and* any diagnostics. Printing one or the other
			// (the old behaviour) meant a build that failed for a codegen/verification
			// reason while the program also carried source warnings exited 1 with no
			// stated reason at all — only --json carried the truth (roadmap Gap K.7).
			if res != nil {
				for _, d := range res.Diagnostics {
					fmt.Fprintln(os.Stderr, d)
				}
			}
			fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
			if isToolchainTimeout(err) {
				// A killed `llc` is not a build that failed for a reason in the source, and it is not
				// the compiler's fault either — the stage stopped answering (exit 8).
				return exitToolchainTimeout
			}
			return buildExitCode(res)
		}
		if *jsonOut {
			b, jerr := json.Marshal(res)
			if jerr != nil {
				fmt.Fprintf(os.Stderr, "gustyc: json: %v\n", jerr)
				return exitCompileError
			}
			fmt.Println(string(b))
		} else {
			fmt.Printf("built %s (%d source files, %d object file(s))\n", res.Output, len(buildFiles), len(res.Objects))
			if v := res.Verification; v != nil {
				switch {
				case v.OK:
					fmt.Printf("  verified by %s (%s)\n", v.Tool, strings.Join(v.Pipeline, " + "))
				case v.Skipped:
					fmt.Printf("  NOT verified: %s\n", v.Note)
				}
			}
			// L8.5: a build that was asked for DWARF says what the module claims and what
			// the artifact really carries — two different statements, both printed.
			if res.Debug != nil {
				fmt.Printf("  %s\n", res.Debug)
			}
			if res.DWARF != nil {
				fmt.Printf("  %s\n", res.DWARF)
			}
			// Gap J.4: an unoptimized build used to be indistinguishable from an optimized
			// one. Say which pipeline actually ran.
			if o := res.Optimization; o != nil {
				if o.Applied {
					fmt.Printf("  optimized by %s (%s)\n", o.Tool, o.Pipeline)
				} else {
					fmt.Printf("  NOT LLVM-optimized: %s\n", o.Note)
					if o.Err != "" {
						fmt.Printf("    %s\n", o.Err)
					}
				}
			}
		}
		return exitOK
	}
	if *repl || (fs.NArg() == 0 && *evalSrc == "" && *file == "" && *verify == "" && *check == "" && *effects == "" && *oracleSrc == "" && *oracleFile == "" && *emitLLVMF == "" && *emitASTF == "" && *emitSourceMapF == "" && *debugInfoF == "" && *debugInfoFile == "" && *benchSrc == "" && *benchFile == "" && *benchSuite == false && *benchDir == "" && *benchBaselineUpdate == "" && *verifyLLVMF == "" && *verifyLLVMFile == "" && isTTY()) {
		return replMode()
	}

	if *verify != "" {
		return verifySrc(*verify, *jsonOut)
	}
	if *check != "" {
		return runCheck(*check, nil, *jsonOut)
	}
	if *effects != "" {
		return runEffects(*effects, nil, *jsonOut)
	}
	// `gusty effects <file1> <file2> ...` : effect signatures for files, no execution.
	if fs.NArg() > 0 && fs.Arg(0) == "effects" && *buildOut == "" && *evalSrc == "" {
		return runEffects("", fs.Args()[1:], *jsonOut)
	}
	// `gusty check <file1> <file2> ...` : type-check files without executing.
	if fs.NArg() > 0 && fs.Arg(0) == "check" && *buildOut == "" && *evalSrc == "" {
		return runCheck("", fs.Args()[1:], *jsonOut)
	}
	if *emitLLVMF != "" {
		return emitLLVM(*emitLLVMF, *target, *optLevel, *jsonOut, *debugFlag)
	}
	if *verifyLLVMF != "" || *verifyLLVMFile != "" {
		// A value-taking flag followed by another flag swallows it: `--verify-llvm
		// --json "src"` would compile the string "--json" instead. Catch it as the
		// usage error it is rather than compiling nonsense.
		for _, v := range []string{*verifyLLVMF, *verifyLLVMFile} {
			if strings.HasPrefix(v, "-") {
				fmt.Fprintf(os.Stderr, "gustyc: %s expects a source, not %q — pass it last or use --flag=<source>\n", "--verify-llvm", v)
				return exitUsage
			}
		}
		return verifyLLVMMode(*verifyLLVMF, *verifyLLVMFile, atoi(*optLevel), *jsonOut)
	}
	if *debugInfoF != "" || *debugInfoFile != "" {
		// The same guard --verify-llvm needs: `--debug-info --json "src"` would otherwise
		// compile the string "--json" and report on that.
		for _, v := range []string{*debugInfoF, *debugInfoFile} {
			if strings.HasPrefix(v, "-") {
				fmt.Fprintf(os.Stderr, "gustyc: %s expects a source, not %q \u2014 pass it last or use --flag=<source>\n", "--debug-info", v)
				return exitUsage
			}
		}
		return runDebugInfo(*debugInfoF, *debugInfoFile, atoi(*optLevel), *jsonOut)
	}
	if *emitSourceMapF != "" {
		return emitSourceMap(*emitSourceMapF)
	}
	if *emitASTF != "" {
		return emitAST(*emitASTF, *jsonOut)
	}
	if *benchSuite || *benchDir != "" || *benchBaselineUpdate != "" {
		return benchSuiteMode(*benchSuite, *benchDir, *benchBaseline, *benchBaselineUpdate, *benchRuns, *benchOpt, *benchTolerance, *benchMinMs, *benchGate, *jsonOut)
	}
	if *benchSrc != "" || *benchFile != "" {
		return benchMode(*benchSrc, *benchFile, *benchRuns, *benchOpt, *jsonOut)
	}
	if *oracleSrc != "" || *oracleFile != "" {
		return oracleMode(*oracleSrc, *oracleFile, *jsonOut)
	}
	if *evalSrc != "" || *file != "" {
		// There is one engine, so there is nothing to choose. `--aot`/`--jit` remain accepted for
		// the scripts that already say them; `--interp` is refused rather than ignored, because a
		// flag whose engine no longer exists must not quietly keep running (ADR 0302, Gap M.2 —
		// which asked for the compiled leg to become the default and is paid by becoming the only
		// leg).
		if *showBackend { // Program output stays on stdout; this is a statement about the tool.
			fmt.Fprintf(os.Stderr, "gustyc: backend %s\n", backendAOT)
		}
		return evalSrcOrFile(*evalSrc, *file, *jsonOut, *gcStats, *debugFlag)
	}
	usage(fs)
	return exitUsage
}

// retiredBackendFlags wires ADR 0302 — the AST interpreter's retirement — into the flag set.
//
// `--aot` and `--jit` are accepted and mean nothing: they ask for the engine that runs anyway, so
// a script that already says them keeps working. `--interp` is refused as a usage error, because
// silently ignoring a flag whose engine no longer exists would let a stale script believe it
// exercised something it did not — the same honesty rule that made `--verify` report the checks it
// actually ran (docs/operations.md § Exit codes, exit 4).
func retiredBackendFlags(aot, jit, interp *bool) error {
	if interp != nil && *interp {
		return fmt.Errorf("--interp is retired: the AST interpreter left with ADR 0302 and the LLVM backend is the only one; drop the flag (docs/operations.md)")
	}
	if aot != nil && *aot || jit != nil && *jit {
		// Asked for the compiled engine by its old names. Nothing to do — that is what runs — but
		// the CLI reads them so the retirement is a decision the code states, not a deleted case.
		return nil
	}
	return nil
}

func srcOrFile(src, file string) (string, error) {
	if src != "" {
		return src, nil
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Backend names the execution engine that ran a program. It travels with every
// machine-readable result so an agent never has to infer it from the flag list (Gap M.2).
//
// There is one answer now: the LLVM AOT backend, because ADR 0302 retired the AST
// interpreter. The member stays — a result that names the engine which produced it is a
// self-describing result, and a script that used to branch on it should not have to notice
// the retirement to keep working.
type backend = string

const backendAOT backend = lang.BackendName

// evalSrcOrFile runs one source through the one backend the language has: codegen -> llc ->
// cc -> run the artifact in-process (lang.JITWithOptions).
//
// A snippet (--eval) and a program (--file, a positional) differ in exactly one courtesy. A
// snippet's final bare expression is a question the caller asked, so it comes back as the answer:
// `result`/`type` on the machine path, and a printed line on the human one — what a REPL is for. A
// program is never echoed to stdout, because a program's stdout is only what the program printed
// (ADR 0204), which is what makes `gustyc prog.gy` and `./prog` print the same bytes.
//
// The distinction is about *stdout*, not about who is allowed to know the value. The module reports
// its final expression's value on the tool channel either way (fd 2), and the runner lifts that line
// out before anything reads stderr; the human path drops it, and `--json` presents it as `result` and
// `type` for a program as well as for a snippet. Suppressing a courtesy is not a reason to blind an
// agent (ADR 0204's machine path, kept through ADR 0302).
// toolFailurePayload is the machine-readable form of "the program did not make it to a run".
//
// It used to be three keys — an error sentence, the backend, an exit code — which pushed whoever read
// it back into prose: to tell "my source does not parse" from "the checker refused it" from "the
// compiler declined to lower it" from "LLVM rejected the module we emitted", an agent had to match on
// substrings of a sentence written for a human, and the position of the problem was in none of them.
// So the payload names the phase, and carries the spans the sentence contains as data.
//
// The phase vocabulary is the pipeline's own: parse, check, codegen, verify, toolchain. `error` keeps
// its old meaning and text for callers already reading it; `ok` says what the run was not.
func toolFailurePayload(err error, exitCode int) string {
	msg := err.Error()
	phase := "compile"
	switch {
	case isToolchainTimeout(err):
		// Checked first, because the sentence a killed `llc` arrives in usually contains the stage that
		// was running ("jit: codegen: llc …"); the phase an agent needs here is "toolchain", and reading
		// it as "codegen" would send the reader looking for a bug in the compiler (exit 8, ADR 0312).
		phase = "toolchain"
	case strings.Contains(msg, "parse error at") || strings.Contains(msg, "unexpected token"):
		phase = "parse"
	case strings.Contains(msg, "error(s) in source") || strings.Contains(msg, "error at "):
		phase = "check"
	case strings.Contains(msg, "codegen:"):
		phase = "codegen"
	case exitCode == exitIRVerify:
		phase = "verify"
	}
	type pos struct {
		Line int    `json:"line"`
		Col  int    `json:"col"`
		Msg  string `json:"msg"`
	}
	var list []pos
	for _, m := range positionRE.FindAllStringSubmatch(msg, -1) {
		line, _ := strconv.Atoi(m[1])
		col, _ := strconv.Atoi(m[2])
		list = append(list, pos{Line: line, Col: col, Msg: strings.TrimSpace(m[3])})
	}
	if list == nil {
		list = []pos{}
	}
	b, jerr := json.Marshal(map[string]any{
		"ok": false, "phase": phase, "error": msg, "errors": list,
		"backend": backendAOT, "exit": exitCode,
	})
	if jerr != nil {
		return fmt.Sprintf("{\"error\": %q, \"backend\": %q, \"exit\": %d}", msg, backendAOT, exitCode)
	}
	return string(b)
}

// positionRE finds the positions a front-end sentence carries — `parse error at 1:7: ...`,
// `error at 3:2: ...` — so a payload can hand them over as numbers.
var positionRE = regexp.MustCompile(`(?:parse error|error) at ([0-9]+):([0-9]+): (.*)$`)

func evalSrcOrFile(src, file string, jsonOut bool, gcStats, debug bool) int {
	s, err := srcOrFile(src, file)
	if err != nil {
		// Nothing to run: the CLI was used wrongly (no source, unreadable file).
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitUsage
	}
	snippet := src != ""
	// The compiled backend reports its own collector numbers from inside the program it just
	// built (the counters live in the target's globals). --gc-stats turns that self-report on;
	// it lands on fd 2, which the in-process runner captures for us.
	lang.SetGCReport(gcStats)
	defer lang.SetGCReport(false)
	jitOpts := &lang.JITOptions{EchoResult: true}
	if debug {
		// `--debug` is a request about the artifact, so it is honoured on every path that builds
		// one — including the in-process runner, whose module and object a debugger can read when
		// GUSTY_KEEP_LLVM keeps the scratch dir (L8.5).
		jitOpts.Debug = &lang.DebugOptions{File: "prog.gy"}
	}
	res, err := lang.JITWithOptions(s, 0, jitOpts)
	if err != nil {
		// Which failure class this is must be visible: every toolchain failure used to be wrapped
		// in a plain error and reported as "your program does not compile", while --build reported
		// the same llc rejection as the compiler-bug class. One event, one code, whichever flag
		// produced it (ADR 0211).
		exitCode := toolchainExit(err)
		if jsonOut {
			fmt.Println(toolFailurePayload(err, exitCode))
		} else {
			fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		}
		return exitCode
	}
	// The target's stderr (uncaught-exception reports, the collector line) is forwarded
	// unchanged: stdout stays the program's, stderr stays the tool's. The REPL echo's line has
	// already been lifted out of it by the runner, so the answer is not shown twice.
	if res.Stderr != "" {
		fmt.Fprint(os.Stderr, res.Stderr)
	}
	if debug && res.Debug != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %s\n", res.Debug)
	}
	// The artifact's own exit status is the answer to "did the program work?". One failure class,
	// one code, whichever flag ran it (roadmap Gap R.17, ADR 0211).
	exitCode := exitOK
	if res.Code != 0 {
		exitCode = exitRuntime
	}
	members := []string{
		fmt.Sprintf("\"output\": %q", res.Output),
		fmt.Sprintf("\"backend\": %q", backendAOT),
		fmt.Sprintf("\"exit\": %d", exitCode),
	}
	if res.Stderr != "" {
		members = append(members, fmt.Sprintf("\"stderr\": %q", res.Stderr))
	}
	if res.Debug != nil {
		if b, jerr := json.Marshal(res.Debug); jerr == nil {
			members = append(members, "\"debug\": "+string(b))
		}
	}
	if gcStats {
		if st, ok := lang.ParseGCStatsLine(reportLine(res.Stderr)); ok {
			members = append(members, `"gc": `+gcObject(st))
		}
	}
	// A trap carries its exception class as data (ADR 0214): an agent branching on `exception`
	// never has to match on the message text, and does not have to parse the traceback it is
	// also given. The report the runtime wrote to fd 2 is where the truth lives; reading it here
	// is what keeps that contract on the compiled path, where the interpreter used to be the only
	// place a class name existed as a value.
	if exitCode == exitRuntime {
		trapLine, class, message := lang.ParseTrapReport(res.Stderr)
		members = append(members, fmt.Sprintf("\"error\": %q", trapLine))
		if class != "" {
			members = append(members, fmt.Sprintf("\"exception\": %q, \"exception_message\": %q", class, message))
		}
		members = append(members, fmt.Sprintf("\"traceback\": %q", res.Stderr))
	}
	// The snippet's answer, if the pair named a form for it. `result` is the text the one str/repr
	// table produced (ADR 0258) and `type` the kind the expression could prove.
	//
	// A snippet that ended with a call handing back the void answers `"result": null` with
	// `"type": "None"` — which is what the REPL path answered before ADR 0302, and what CPython's
	// prompt prints nothing for. The empty text on the echo line is that case: the program ran, and
	// it owed no value.
	if res.Result != nil {
		if res.Result.Repr == "" {
			kind := res.Result.Kind
			if kind == "" || kind == "NoneType" || kind == "void" {
				kind = "None"
			}
			members = append(members, fmt.Sprintf("\"result\": null, \"type\": %q", kind))
		} else {
			members = append(members, fmt.Sprintf("\"result\": %q, \"type\": %q", res.Result.Repr, res.Result.Kind))
		}
	}
	if jsonOut {
		fmt.Println("{" + strings.Join(members, ", ") + "}")
		return exitCode
	}
	fmt.Print(res.Output)
	if snippet && res.Result != nil && res.Result.Repr != "" {
		// The REPL courtesy, on stdout: an interactive caller typed an expression and is owed its
		// value, the way CPython's prompt writes repr(value). It is a tool statement made *for* the
		// reader, which is why it goes where the reader is looking and nowhere in --json's output
		// member (that stays the program's own bytes). A value that was the void prints nothing —
		// neither the old REPL nor CPython's announces a None nobody asked for. A *program* is never
		// echoed even when the module reported a value for its last expression: `gustyc prog.gy` and
		// `./prog` owe the same bytes, and that is what the flag `snippet` remembers (ADR 0204).
		fmt.Println(res.Result.Repr)
	}
	// The collector's line is not printed twice. The compiled program reports its own counters on
	// fd 2 (rt_gc_report), and that line is forwarded above with the rest of the tool channel; the
	// CLI's job is to add the *data* form under --json, not to render the same numbers a second time
	// in its own words (ADR 0181: one report per run, on the tool channel).
	return exitCode
}

// gcObject is the JSON text of a collector self-report, used for the `gc` member.
func gcObject(st lang.GCStats) string {
	b, err := json.Marshal(st)
	if err != nil {
		return "null"
	}
	return string(b)
}

// reportLine picks the collector self-report out of a program's stderr, so --json can
// present it as data while the human still sees the line itself (ADR 0181).
func reportLine(stderr string) string {
	for _, ln := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(strings.TrimSpace(ln), "gc: backend=") {
			return ln
		}
	}
	return ""
}

// gcJSON is the `gc` member of a --json result payload, or "" when --gc-stats was
// not given so the payload keeps its existing shape.
func gcJSON(st lang.GCStats, on bool) string {
	if !on {
		return ""
	}
	b, err := json.Marshal(st)
	if err != nil {
		return ""
	}
	return fmt.Sprintf(", \"gc\": %s", b)
}

func verifySrc(src string, jsonOut bool) int {
	prog, err := lang.Parse(src)
	if err != nil {
		if jsonOut {
			fmt.Printf("{\"error\": %q, \"exit\": %d}\n", err.Error(), exitUsage)
		}
		return reportParseErr(err, jsonOut)
	}
	diags := lang.Analyze(prog)
	if len(diags) > 0 {
		if jsonOut {
			emitDiagnosticsJSON(diags, exitCompileError)
		} else {
			for _, d := range diags {
				fmt.Fprintf(os.Stderr, "%v\n", d)
			}
		}
		return exitCompileError
	}
	if jsonOut {
		fmt.Println(`{"ok": true, "exit": 0}`)
	} else {
		fmt.Println("ok")
	}
	return exitOK
}

func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

func emitLLVM(src, target, opt string, jsonOut, debug bool) int {
	res, err := lang.Compile(src)
	if err != nil {
		return reportCompileErr(err, jsonOut)
	}
	if target != "" {
		fmt.Printf("; target = %s\n", target)
	}
	if opt != "" && opt != "0" {
		fmt.Printf("; opt-level = %s\n", opt)
	}
	if debug {
		// The same module a --build --debug would link, line records and all, so an
		// agent can read the IR it is about to hand to llc and know what the debugger
		// will see (L8.5, ADR 0231).
		dbgIR, derr := lang.CompileDebug(src, atoi(opt))
		if derr != nil {
			return reportCompileErr(derr, jsonOut)
		}
		fmt.Print(dbgIR)
		return exitOK
	}
	fmt.Print(lang.OptimizeIR(res.IR, atoi(opt)))
	return exitOK
}

// runDebugInfo prints the DWARF line-table document for a source: what the compile unit
// claims, one entry per program function, and the IR-line-to-source-line rows. It needs
// no LLVM toolchain, because the compiler is the author of the answer; --build --debug
// adds the llvm-dwarfdump reading of the real artifact on top (ADR 0231).
func runDebugInfo(src, file string, optLevel int, jsonOut bool) int {
	s, err := srcOrFile(src, file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitUsage
	}
	name := ""
	if file != "" {
		name = file
	}
	opts := &lang.DebugOptions{OptLevel: optLevel}
	if name != "" {
		opts.FromFile(name)
	}
	info, _, cerr := lang.DebugInfoForSource(s, opts)
	if cerr != nil {
		return reportCompileErr(cerr, jsonOut)
	}
	if jsonOut {
		b, jerr := json.MarshalIndent(info, "", "  ")
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "gustyc: json: %v\n", jerr)
			return exitCompileError
		}
		fmt.Println(string(b))
		return exitOK
	}
	fmt.Printf("%s\n", info)
	for _, f := range info.Functions {
		fmt.Printf("  %-24s %s:%-4d %3d instruction(s), %d location(s)\n", f.Symbol, f.Name, f.Line, f.Tagged, f.Locations)
	}
	lines := info.SourceLineSet()
	if len(lines) > 0 {
		fmt.Printf("  source lines covered: %d (%s)\n", len(lines), summarizeInts(lines, 24))
	}
	if info.Instructions > 0 && info.Tagged != info.Instructions {
		fmt.Printf("  NOT fully tagged: %d of %d instruction(s) carry a location\n", info.Tagged, info.Instructions)
	}
	return exitOK
}

// summarizeInts renders "1,2,3,\u2026" for a line list without letting a big program flood
// the terminal.
func summarizeInts(v []int, max int) string {
	parts := make([]string, 0, len(v))
	for i, n := range v {
		if i >= max {
			parts = append(parts, fmt.Sprintf("\u2026+%d", len(v)-max))
			break
		}
		parts = append(parts, strconv.Itoa(n))
	}
	return strings.Join(parts, ",")
}

func emitSourceMap(src string) int {
	// `--emit-source-map` is documented as taking a source file, and a path is what
	// callers type; a value that is not on disk stays source text, like every other
	// value-taking flag in this CLI.
	if st, serr := os.Stat(src); serr == nil && !st.IsDir() {
		sm, err := lang.EmitSourceMapFile(src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gustyc: emit-source-map: %v\n", err)
			return exitCompileError
		}
		fmt.Println(string(sm))
		return exitOK
	}
	sm, err := lang.EmitSourceMap(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: emit-source-map: %v\n", err)
		return exitCompileError
	}
	fmt.Println(string(sm))
	return exitOK
}

func emitAST(src string, jsonOut bool) int {
	res, err := lang.Compile(src)
	if err != nil {
		return reportParseErr(err, jsonOut)
	}
	fmt.Println(res.ASTJSON)
	return exitOK
}

// reportCompileErr reports a compilation failure. Humans get a one-line message
// on stderr; with --json an agent gets the same failure as a structured object
// on stdout — same shape as the other machine-readable failure modes:
//
//	{"ok": false, "phase": "compile", "error": "...", "exit": N}
func reportCompileErr(err error, jsonOut bool) int {
	// A front-end rejection reaching this entry point still reports its phase as `parse`, with spans.
	// `--eval` and `--file` compile through one call, so a typo and an unlowerable construct both arrive
	// here, and until now both were labelled `phase: "compile"` with a bare sentence — an agent reading
	// the payload could not tell "my source does not parse" from "the compiler declined my program",
	// which is the exact distinction the phase field exists to carry (docs/operations.md § JSON output).
	var pes *lang.ParseErrors
	if errors.As(err, &pes) || strings.HasPrefix(err.Error(), "parse error") || strings.Contains(err.Error(), "unexpected token") {
		return reportParseErr(err, jsonOut)
	}
	if jsonOut {
		fmt.Printf(`{"ok": false, "phase": "compile", "error": %q, "exit": %d}`+"\n", err.Error(), exitCompileError)
		return exitCompileError
	}
	return reportParseErr(err, jsonOut)
}

// isToolchainTimeout says whether a failure is the external toolchain running out of its budget
// (pkg/lang/tool_budget.go). The type is the authority — the classification contract (ADR 0211) is
// errors.As, never message matching.
func isToolchainTimeout(err error) bool {
	var t *lang.ToolTimeoutError
	return errors.As(err, &t)
}

// toolchainExit classifies a failure from a stage that handed work to the external toolchain.
// Rejection and silence are different findings: `llc` saying no to the module codegen produced is a
// compiler bug (2), while `llc` never answering at all is a machine fault (8), and a caller that got
// 2 for a hung runner would go looking for a compiler bug that is not there. Everything else — parse,
// analysis, a codegen refusal — is the ordinary compile error (1).
func toolchainExit(err error) int {
	var timeout *lang.ToolTimeoutError
	if errors.As(err, &timeout) {
		return exitToolchainTimeout
	}
	var rejection *lang.ToolchainRejectionError
	if errors.As(err, &rejection) {
		return exitIRVerify
	}
	return exitCompileError
}

// buildExitCode classifies a failed build for the exit status. A module that LLVM itself
// rejected is a compiler bug, not a source error: exit 2 separates "gusty produced bad IR"
// from "your program is wrong" (docs/operations.md § Exit codes). Everything else — parse,
// analysis, a codegen refusal, llc/cc — is exit 1. Extracted so the rule is testable
// without manufacturing an invalid module on purpose.
func buildExitCode(res *lang.BuildResult) int {
	if res != nil && res.Verification != nil && !res.Verification.OK && !res.Verification.Skipped {
		return exitIRVerify
	}
	return exitCompileError
}

// verifyExitCode is the same three-way split for the `--verify-llvm` stage, whose failure arrives as an
// error rather than a BuildResult. Today anything LLVM rejects is a compiler bug (2); a tool that never
// answered is not a bug, it is a machine, and it says so with 8.
func verifyExitCode(verr error) int {
	var timeout *lang.ToolTimeoutError
	if errors.As(verr, &timeout) {
		return exitToolchainTimeout
	}
	return exitIRVerify
}

// reportParseErr reports a front-end rejection. A program that does not parse is a
// compile error (exit 1), not a usage error (exit 4): the CLI was invoked correctly, the
// *program* is what is wrong. Conflating them meant a script could not tell "my source
// has a typo" from "I forgot the --file flag" (roadmap Gap J.3).
func reportParseErr(err error, jsonOut bool) int {
	pes, isForest := err.(*lang.ParseErrors)
	if jsonOut {
		// The machine path gets the same information as the human path, in the shape
		// every other machine-readable failure uses, and with the spans: an agent
		// should never have to parse `gustyc: parse error at 1:7: …` prose off stderr
		// to find out where its program stopped parsing.
		type pos struct {
			Line int    `json:"line"`
			Col  int    `json:"col"`
			Msg  string `json:"msg"`
		}
		list := []pos{}
		first := err.Error()
		if !isForest {
			// A wrapped front-end message — `jit: parse error at 1:1: unexpected token` — still has a
			// position in it, and the payload is where an agent reads that position from. Pull it out
			// rather than hand back a payload with an empty `errors` list and let everyone grep stderr.
			if m := parseAtRE.FindStringSubmatch(first); m != nil {
				line, _ := strconv.Atoi(m[1])
				col, _ := strconv.Atoi(m[2])
				list = append(list, pos{Line: line, Col: col, Msg: strings.TrimSpace(m[3])})
				first = fmt.Sprintf("%d:%d: %s", line, col, strings.TrimSpace(m[3]))
			}
		}
		if isForest {
			for _, pe := range pes.Errors {
				list = append(list, pos{Line: pe.Span.Line, Col: pe.Span.Col, Msg: pe.Msg})
			}
			if len(list) > 0 {
				first = fmt.Sprintf("%d:%d: %s", list[0].Line, list[0].Col, list[0].Msg)
			}
		}
		b, jerr := json.Marshal(map[string]any{
			"ok": false, "phase": "parse", "error": first, "errors": list, "exit": exitCompileError,
		})
		if jerr == nil {
			fmt.Println(string(b))
		}
		return exitCompileError
	}
	if isForest {
		// panic-mode recovery surfaces a forest of parse errors: print each.
		for _, pe := range pes.Errors {
			fmt.Fprintf(os.Stderr, "gustyc: parse error at %d:%d: %s\n", pe.Span.Line, pe.Span.Col, pe.Msg)
		}
		return exitCompileError
	}
	fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
	return exitCompileError
}

// parseAtRE reads the position back out of a front-end sentence — `parse error at 1:7: unexpected
// token` — for the cases where the error reached the CLI already wrapped, so the structured payload
// still carries a line and a column instead of only prose.
var parseAtRE = regexp.MustCompile(`parse error at ([0-9]+):([0-9]+): (.*)$`)

func usage(fs *flag.FlagSet) {
	fmt.Printf(`gustyc — gusty language CLI/REPL
Usage: gustyc [flags] [<src>]

Flags:
`)
	fs.PrintDefaults()
	fmt.Printf(`
Build: gustyc --build <out> <file1> <file2> ...  # compile sources into a native binary
Shared library export (L10.3): gustyc --build out.so --shared <file1> ...  # emit a position-independent .so/.dylib with the stable extern-fn ABI
Check: gustyc --check <src> | gustyc check <file1> <file2> ...  # mypy-style type-check without executing
Effects: gustyc --effects <src> | gustyc effects <file>...  # per-function effect signature: awaits/yields/raises, returns, fall-through (L7.6; --json for the document)
Variance: gustyc --variance  # JSON variance table (list/set/dict invariant, Sequence covariant, Callable params contravariant)
Verify IR: gustyc --verify-llvm <src> [--json]         # LLVM module-verifier verdict for the emitted module (L8.2)
           gustyc --build out src.gy --no-verify        # skip verification (it runs by default in --build)
Debug info: gustyc --debug-info <src> [--json]          # the compiled line table: functions, IR-line to source-line rows (L8.5)
            gustyc --build out src.gy --debug           # !dbg records + .debug_line, read back with llvm-dwarfdump and reported
Benchmarks: gustyc --bench-suite --bench-runs 5            # measure the corpus on the compiled backend
            gustyc --bench-dir integration/programs        # benchmark the parity programs too
            gustyc --bench-suite --bench-baseline-update benchmarks/baseline.json   # record a baseline
            gustyc --bench-suite --bench-baseline benchmarks/baseline.json          # gate (exit 5 = slower than baseline)

Diagnostic codes (--check --json): type.mismatch, type.variance.invariant,
type.variance.covariant, type.variance.contravariant, type.variance.nominal,
type.callable.arity, type.union.members, async.coro.never_awaited,
async.coro.awaited_twice, async.await.outside_coroutine,
async.async_stmt.outside_coroutine, async.await.not_coroutine,
async.generator.unsupported, async.missing_return — see docs/operations.md.

Exit codes: 0 = ok, 1 = compile error (parse/analysis/codegen/link), 2 = LLVM rejected
the module gusty emitted (a compiler bug, not your program), 3 = the program ran and
trapped, 4 = CLI usage error, 5 = benchmark regression. See docs/operations.md.
`)
}

func listLang() {
	fmt.Printf(`gusty language features (%s)
statements: assign, print, if/elif/else, while, for-in-range, def/return, pass, match, try/except/finally, raise, class, import
expressions: int, float, string, list, dict, binary ops (+ - * / %% == < <= > >= and or not), call, len, attribute, index, lambda, comprehension (list [x for x in it if c] / set {x for x in it if c} / dict {k: v for k in it if c})
operators: every operator is a question about the operand's KIND, binary or unary. An operator with no rule for the pair raises CPython's own TypeError at run time — exit 3, catchable by except TypeError: — and never answers with a number. That includes unary -: -"hi", -None, -[1], -{"a":1}, -{1}, -C() each raise bad operand type for unary -: '<kind>' naming the operand's real kind (the compiled backend; ADR 0266, docs/language.md § Unary minus), and abs(): abs("hi"), abs(None), abs([1]), abs({"a":1}), abs({1}), abs(C()) raise bad operand type for abs(): '<kind>' through the same door, so the two operators can never name the same operand differently (the compiled backend; ADR 0271, docs/language.md § abs)
types: int, float, bool, str, list[T], dict[K, V], set[T], tuple[...], Sequence[T], Callable[[...], R], class, function, any
patterns: match cases take a literal, _ (wildcard), a bare name (capture), an or-pattern (1 | 2), a guard (case n if n > 1), a sequence [a, b], a mapping {"k": v}, or a class pattern Point(x, y) — attributes bound by capture name, through an alias too (Alias = Point); a missing attribute fails the case (the compiled backend; ADR 0235)
variance: list/set/dict invariant in T, Sequence/iter/tuple covariant, Callable parameters contravariant + return covariant, classes nominal (see gustyc --variance)
effects: async def calls are deferred until awaited; the checker proves the discipline and --effects prints each function's signature (await, yield, raise / returns / falls-through) — see gustyc --effects
values: %s
heap kinds (compiled runtime object headers): %s (0 = not heap-allocated)
debug: --debug puts a real line table in the module and the object (DW_LANG_Python), and the tool reads it
      back from the artifact: --debug-info prints the compiled table, --build --debug adds what
      llvm-dwarfdump found in the binary (see docs/operations.md § Debug info, ADR 0231)
oracle: every conformance program is compared against CPython too (gustyc --oracle <src>; --json for the
      three-leg report; exit 0 match, 6 gusty printed something else, 7 the oracle could not judge — see
      docs/operations.md § The CPython oracle leg, ADR 0186)
`, lang.Version, strings.Join(lang.ValueTagNames(), " "), strings.Join(lang.HeapKindNames(), " "))
}

// reorderFlags moves flag tokens to the front so the flag package sees all of them,
// while keeping each flag's value attached. A value-taking flag in `--flag value` form
// owns the following token, so `--eval --help` still means "evaluate the text --help"
// rather than "print usage". Tokens the FlagSet does not define are still handed to the
// flag package, which produces its own "flag provided but not defined" error.
func reorderFlags(fs *flag.FlagSet, args []string) []string {
	flags := make([]string, 0, len(args))
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "-") || a == "-" || a == "--" {
			rest = append(rest, a)
			continue
		}
		name := strings.TrimLeft(a, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name = name[:eq]
		}
		f := fs.Lookup(name)
		if f == nil {
			flags = append(flags, a)
			continue
		}
		flags = append(flags, a)
		if _, isBool := f.Value.(interface{ IsBoolFlag() bool }); !isBool && !strings.Contains(a, "=") {
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		}
	}
	return append(flags, rest...)
}

func isTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// emitDiagnosticsJSON prints diagnostics as a JSON array with the exit code.
func emitDiagnosticsJSON(diags []lang.Diagnostic, exit int) {
	b, err := json.Marshal(map[string]any{"diagnostics": diags, "exit": exit})
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: json marshal: %v\n", err)
		return
	}
	fmt.Println(string(b))
}

func runFmt(src string, check bool, file string, jsonOut bool) int {
	input := src
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			if jsonOut {
				fmt.Printf(`{"ok": false, "error": %q, "exit": %d}`+"\n", err.Error(), exitCompileError)
			}
			return exitCompileError
		}
		input = string(b)
	}
	f, err := lang.FormatSrc(input)
	if err != nil {
		if jsonOut {
			fmt.Printf(`{"ok": false, "error": %q, "exit": %d}`+"\n", err.Error(), exitCompileError)
		}
		return exitCompileError
	}
	if check {
		canonical := f == input || f == strings.TrimRight(input, "\n")
		if jsonOut {
			if canonical {
				fmt.Printf(`{"ok": true, "canonical": true, "exit": %d}`+"\n", exitOK)
			} else {
				fmt.Printf(`{"ok": true, "canonical": false, "exit": %d}`+"\n", exitNotCanonical)
			}
		} else if !canonical {
			fmt.Println(f)
		}
		if canonical {
			return exitOK
		}
		return exitNotCanonical
	}
	fmt.Println(f)
	return exitOK
}

// runCheck implements the standalone type-check mode (`--check <src>` and
// `gusty check <file>...`). It runs the semantic pass (mypy-style: annotated
// code is checked without being executed), emits diagnostics (JSON with
// --json), and exits deterministically: 0 = clean, 1 = type errors, 2 = usage.
func runCheck(src string, files []string, jsonOut bool) int {
	var res *lang.CheckResult
	var err error
	if len(files) > 0 {
		res, err = lang.CheckFiles(files)
	} else {
		res, err = lang.CheckSource(src)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitUsage
	}
	if jsonOut {
		b, jerr := json.Marshal(res)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "gustyc: json: %v\n", jerr)
			return exitCompileError
		}
		fmt.Println(string(b))
	} else {
		for _, d := range res.Diagnostics {
			fmt.Fprintf(os.Stderr, "%v\n", d)
		}
		if res.OK {
			fmt.Println("ok")
		}
	}
	return res.Exit
}

// runEffects implements the effect-signature mode (`--effects <src>` and
// `gusty effects <file>...`, L7.6). It reports what each function *does* — the
// effects its body performs, whether it returns a value, whether its control flow
// can run off the end — without executing anything, and exits 1 when a source does
// not parse (there is nothing to summarise) or breaks the await/return discipline
// (the same diagnostics `--check` reports). The table is the same data the checker
// decided from, so the two cannot disagree.
func runEffects(src string, files []string, jsonOut bool) int {
	type fileProg struct {
		name string
		src  string
	}
	var progs []fileProg
	if len(files) > 0 {
		for _, fn := range files {
			b, err := os.ReadFile(fn)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gustyc: effects: read %s: %v\n", fn, err)
				return exitUsage
			}
			progs = append(progs, fileProg{name: fn, src: string(b)})
		}
	} else {
		progs = append(progs, fileProg{name: "<src>", src: src})
	}
	bad := false
	for _, fp := range progs {
		res, err := lang.CheckSource(fp.src)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gustyc: effects: %v\n", err)
			return exitUsage
		}
		for _, d := range res.Diagnostics {
			fmt.Fprintln(os.Stderr, d)
			if d.Level == lang.LevelError {
				bad = true
			}
		}
		prog, perr := lang.Parse(fp.src)
		if perr != nil {
			fmt.Fprintf(os.Stderr, "gustyc: effects: %s: %v\n", fp.name, perr)
			return exitCompileError
		}
		if jsonOut {
			doc, jerr := lang.EffectsJSON(prog, fp.name, res.Diagnostics)
			if jerr != nil {
				fmt.Fprintf(os.Stderr, "gustyc: effects: %v\n", jerr)
				return exitCompileError
			}
			fmt.Println(doc)
			continue
		}
		fmt.Print(lang.EffectTable(prog, fp.name))
	}
	if bad {
		return exitCompileError
	}
	return exitOK
}

// benchMode runs a source program through the one execution backend the language has —
// the LLVM AOT artifact, run in-process — reports wall-clock timings, and prints a human
// or JSON report (ADR 0302: the interpreter leg and the speedup ratio it produced are gone).
func benchMode(src, file string, runs, opt int, jsonOut bool) int {
	src, err := srcOrFile(src, file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitUsage
	}
	res, err := lang.Benchmark(src, runs, opt)
	if err != nil {
		for _, d := range res.Diagnostics {
			fmt.Fprintf(os.Stderr, "bench: %v\n", d)
		}
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitCompileError
	}
	if jsonOut {
		out, jerr := json.MarshalIndent(res, "", "  ")
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "gustyc: json: %v\n", jerr)
			return exitCompileError
		}
		fmt.Println(string(out))
	} else {
		fmt.Printf("benchmark: %d runs, AOT opt=%d\n", res.Runs, res.OptLevel)
		for _, p := range res.Profile {
			fmt.Printf("  pipeline: %-8s %8.3f ms\n", p.Phase, p.Ms)
		}
		fmt.Printf("  run:   total %8.3f ms  mean %8.3f ms  best %8.3f ms\n", res.AOT.TotalMs, res.AOT.MeanMs, res.AOT.BestMs)
		fmt.Printf("  build: total %8.3f ms  (codegen + llc + cc, once)\n", res.Build.TotalMs)
	}
	return exitOK
}

// benchSuiteReport is the machine-readable artifact of a suite run: the suite
// itself plus the gate verdict. Embedded so the suite keys stay at top level.
type benchSuiteReport struct {
	*lang.BenchSuite
	Regressions []lang.BenchRegression `json:"regressions"`
	NewCases    []lang.BenchNewCase    `json:"new_cases"`
	Exit        int                    `json:"exit"`
}

// benchSuiteMode measures a corpus (built-in, or every *.gy in --bench-dir) on
// the compiled backend, optionally gates it against a baseline, and reports.
//
// Exit codes: 0 when clean, 5 (exitBenchRegression) when the gate fires, 1 on a
// tooling error (bad baseline path, unreadable directory).
func benchSuiteMode(useCorpus bool, dir, baselinePath, updatePath string, runs, opt int, tolerance, minMs float64, gate string, jsonOut bool) int {
	// A gate name is an argument, and an unknown argument is a usage error (exit 4), not a silent
	// no-op: ADR 0302 retired the engine whose gate this was, and a script still passing the retired
	// name must find out at the flag rather than discovering later that its gate watched nothing
	// (the same rule `--interp` follows, and the reason lang.CompareBenchSuite's permissive default
	// is not enough on its own).
	if gate != "" && gate != lang.BenchGateAOT {
		fmt.Fprintf(os.Stderr, "gustyc: --bench-gate %q is not a gate: %q is the only measurement the regression gate watches, since ADR 0302 retired the engine the other gates compared\n", gate, lang.BenchGateAOT)
		return exitUsage
	}
	cases := []lang.BenchCase{}
	if useCorpus {
		cases = append(cases, lang.BenchCorpus()...)
	}
	if dir != "" {
		fromDir, err := lang.BenchDir(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
			return exitUsage
		}
		if len(fromDir) == 0 {
			fmt.Fprintf(os.Stderr, "gustyc: bench: no .gy files in %s\n", dir)
			return exitUsage
		}
		cases = append(cases, fromDir...)
	}
	if len(cases) == 0 {
		fmt.Fprintf(os.Stderr, "gustyc: bench: nothing to measure (pass --bench-suite or --bench-dir)\n")
		return exitUsage
	}

	suite := lang.BenchmarkSuite(cases, runs, opt)

	code := exitOK
	regressions := []lang.BenchRegression{}
	newCases := []lang.BenchNewCase{}
	if baselinePath != "" {
		base, err := lang.LoadBenchBaseline(baselinePath)
		if err != nil {
			// A missing/corrupt baseline is a bad argument, not a slow program.
			fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
			return exitUsage
		}
		regressions, newCases = lang.CompareBenchSuite(suite, base, tolerance, minMs, gate)
		if len(regressions) > 0 {
			code = exitBenchRegression
		}
	}
	if updatePath != "" {
		if err := lang.SaveBenchBaseline(updatePath, lang.BaselineFromSuite(suite)); err != nil {
			fmt.Fprintf(os.Stderr, "gustyc: bench: write baseline: %v\n", err)
			return exitUsage
		}
	}

	if jsonOut {
		out, err := json.MarshalIndent(benchSuiteReport{BenchSuite: suite, Regressions: regressions, NewCases: newCases, Exit: code}, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "gustyc: json: %v\n", err)
			return exitCompileError
		}
		fmt.Println(string(out))
		return code
	}

	fmt.Printf("benchmark suite: %d cases, %d runs, AOT opt=%d\n", suite.Totals.Cases, suite.Runs, suite.OptLevel)
	fmt.Printf("  %-22s %10s %10s\n", "case", "run ms", "build ms")
	for _, c := range suite.Cases {
		if c.Error != "" {
			fmt.Printf("  %-22s %10s %10s   %s\n", c.Name, "-", "-", c.Error)
			continue
		}
		fmt.Printf("  %-22s %10.3f %10.3f\n", c.Name, c.AOT.BestMs, c.Build.BestMs)
	}
	fmt.Printf("  %-22s %10.3f %10.3f\n", "TOTAL", suite.Totals.AOTMs, suite.Totals.BuildMs)
	if baselinePath != "" {
		fmt.Printf("  gate: %s leg=%s (tolerance %.2fx, noise floor %.2f ms)\n", baselinePath, gate, tolerance, minMs)
		for _, r := range regressions {
			fmt.Printf("  REGRESSION %s/%s: %.2f ms -> %.2f ms (%.2fx)\n", r.Name, r.Backend, r.BaselineMs, r.CurrentMs, r.Ratio)
			fmt.Printf("            %s\n", r.Suggestion)
		}
		for _, n := range newCases {
			fmt.Printf("  new case %s: %s\n", n.Name, n.Suggestion)
		}
		if len(regressions) == 0 {
			fmt.Printf("  gate: clean, no regressions\n")
		}
	}
	if updatePath != "" {
		fmt.Printf("  baseline written: %s\n", updatePath)
	}
	return code
}

// verifyLLVMMode compiles a program and asks LLVM's own module verifier about the
// result (L8.2). The verdict is a structured record so a caller can branch on it
// without scraping tool output: {"ok","tool","skipped","pipeline","errors","note",
// "toolchain"}. A missing toolchain is reported as skipped, never as a pass.
func verifyLLVMMode(src, file string, optLevel int, jsonOut bool) int {
	s, err := srcOrFile(src, file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitUsage
	}
	res, cerr := lang.Compile(s)
	if cerr != nil {
		return reportCompileErr(cerr, jsonOut)
	}
	ver, verr := lang.VerifyModuleIR(res.IR, optLevel)
	if jsonOut {
		b, jerr := json.Marshal(ver)
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "gustyc: json: %v\n", jerr)
			return exitCompileError
		}
		fmt.Println(string(b))
	} else if ver.OK {
		fmt.Printf("module verified by %s (%s)\n", ver.Tool, strings.Join(ver.Pipeline, " + "))
	} else if ver.Skipped {
		fmt.Printf("not verified: %s\n", ver.Note)
	} else {
		for _, l := range ver.Errors {
			fmt.Fprintf(os.Stderr, "gustyc: %s: %s\n", ver.Tool, l)
		}
		if ver.Note != "" {
			fmt.Fprintf(os.Stderr, "gustyc: %s\n", ver.Note)
		}
	}
	if verr != nil {
		// LLVM rejected the module *we* produced. Distinct from a compile error so a
		// caller can report "compiler bug" instead of "your program is wrong" — and distinct
		// again from a verifier that never answered, which is neither (exitToolchainTimeout).
		return verifyExitCode(verr)
	}
	return exitOK
}
