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
//	--oracle <src>      run interpreter + compiled backend + CPython and compare (exit 6 divergence, 7 no verdict)
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
	"flag"
	"fmt"
	"os"
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
	benchSrc := fs.String("bench", "", "benchmark a source program through both backends (interpreter + AOT JIT)")
	benchFile := fs.String("bench-file", "", "benchmark a source file")
	benchRuns := fs.Int("bench-runs", 3, "runs per backend for benchmarks")
	benchOpt := fs.Int("bench-opt", 2, "AOT optimization level for benchmarks")
	benchSuite := fs.Bool("bench-suite", false, "benchmark the built-in corpus (interpreter vs AOT)")
	benchDir := fs.String("bench-dir", "", "benchmark every *.gy in a directory (e.g. integration/programs)")
	benchBaseline := fs.String("bench-baseline", "", "path to a baseline JSON to gate against")
	benchBaselineUpdate := fs.String("bench-baseline-update", "", "write the measured suite as a baseline JSON to this path")
	benchTolerance := fs.Float64("bench-tolerance", lang.DefaultBenchTolerance, "slowdown multiplier above which a case is a regression")
	benchMinMs := fs.Float64("bench-min-ms", lang.DefaultBenchMinMs, "ignore baseline times below this many ms (noise floor)")
	benchGate := fs.String("bench-gate", lang.BenchGateAOT, "which leg the regression gate watches: aot, interpreter or both")
	evalSrc := fs.String("eval", "", "evaluate a source string")
	file := fs.String("file", "", "read and evaluate a source file")
	verify := fs.String("verify", "", "parse + analyze a source string")
	check := fs.String("check", "", "type-check a source string without executing (mypy-style)")
	effects := fs.String("effects", "", "print each function's effect signature (awaits / yields / raises, whether it returns a value, whether its control flow can fall off the end) for a source string; --json for the machine document, `gustyc effects <file>...` for files (L7.6)")
	oracleSrc := fs.String("oracle", "", "run a source string through interpreter + compiled backend + CPython and report whether gusty behaves like Python (exit 6 divergence, 7 no verdict; --json: the leg-by-leg report)")
	oracleFile := fs.String("oracle-file", "", "same as --oracle, for a source file")
	emitLLVMF := fs.String("emit-llvm", "", "print LLVM IR for a source string")
	emitSourceMapF := fs.String("emit-source-map", "", "print the source-map JSON for a source file")
	emitASTF := fs.String("emit-ast", "", "print the AST as JSON for a source string")
	target := fs.String("target", "", "target triple for codegen")
	optLevel := fs.String("opt-level", "0", "optimization level")
	buildOut := fs.String("build", "", "output binary path for a multi-file build (sources are the positional args)")
	noVerify := fs.Bool("no-verify", false, "skip LLVM's module verifier during --build (on by default, L8.2)")
	verifyLLVMF := fs.String("verify-llvm", "", "compile a source string and report LLVM's module-verifier verdict")
	verifyLLVMFile := fs.String("verify-llvm-file", "", "compile a source file and report LLVM's module-verifier verdict")
	debugFlag := fs.Bool("debug", false, "pass -g to llc/cc so the binary carries DWARF debug info")
	sourceMapOut := fs.String("source-map-out", "", "write a JSON source map (source fn -> IR symbol+line) to this path")
	jsonOut := fs.Bool("json", false, "emit results/diagnostics as JSON")
	langCmd := fs.Bool("lang", false, "list supported language features")
	schemaCmd := fs.Bool("schema", false, "print the machine-readable JSON schema for the AST/IR dumps")
	varianceCmd := fs.Bool("variance", false, "print the generic variance table as JSON (list/set/dict invariant, Sequence/iter/tuple covariant, Callable parameters contravariant, classes nominal)")
	abiCmd := fs.Bool("abi", false, "print the versioned gusty extern-fn C ABI schema (JSON)")
	sharedCmd := fs.Bool("shared", false, "emit a position-independent shared library (.so/.dylib) with the stable extern-fn ABI instead of a native executable (with --build)")
	jit := fs.Bool("jit", false, "use the in-process dlopen JIT (codegen -> llc -> cc -shared -> dlopen -> run) instead of the AST interpreter")
	aot := fs.Bool("aot", false, "run through the compiled LLVM backend (alias of --jit); --file defaults to the interpreter, so say so explicitly")
	interp := fs.Bool("interp", false, "run through the AST interpreter explicitly (the default; conflicts with --aot/--jit)")
	showBackend := fs.Bool("show-backend", false, "print which backend executed the program (stderr; --json reports it in the payload)")
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
	if *repl || (fs.NArg() == 0 && *evalSrc == "" && *file == "" && *verify == "" && *check == "" && *effects == "" && *oracleSrc == "" && *oracleFile == "" && *emitLLVMF == "" && *emitASTF == "" && *emitSourceMapF == "" && *benchSrc == "" && *benchFile == "" && *benchSuite == false && *benchDir == "" && *benchBaselineUpdate == "" && *verifyLLVMF == "" && *verifyLLVMFile == "" && isTTY()) {
		return replMode(*jit)
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
		return emitLLVM(*emitLLVMF, *target, *optLevel, *jsonOut)
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
		// Which backend ran used to be invisible: --file quietly used the AST
		// interpreter, so "I compiled this program" could mean "I interpreted it",
		// and AOT-only bugs hid behind the default path (roadmap Gap M.2). The
		// choice is now explicit on the command line and explicit in the output.
		if (*aot || *jit) && *interp {
			fmt.Fprintln(os.Stderr, "gustyc: --aot/--jit and --interp contradict each other; choose one backend")
			return exitUsage
		}
		backend := backendInterpreter
		if *aot || *jit {
			backend = backendJIT
		}
		src := *evalSrc
		if *showBackend {
			// Program output stays on stdout; this is a statement about the tool.
			fmt.Fprintf(os.Stderr, "gustyc: backend %s\n", backend)
		}
		return evalSrcOrFile(src, *file, *jsonOut, backend, *gcStats)
	}
	usage(fs)
	return exitUsage
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
// machine-readable result so an agent never has to infer it from flags.
type backend string

const (
	backendInterpreter backend = "interpreter"
	backendJIT         backend = "aot"
)

func evalSrcOrFile(src, file string, jsonOut bool, backend backend, gcStats bool) int {
	s, err := srcOrFile(src, file)
	if err != nil {
		// Nothing to run: the CLI was used wrongly (no source, unreadable file).
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitUsage
	}
	if backend == backendJIT {
		// The compiled backend reports its own collector numbers, from inside the
		// program it just built (the counters live in the target's globals). --gc-stats
		// turns that self-report on; it lands on fd 2, which the JIT captures for us.
		lang.SetGCReport(gcStats)
		defer lang.SetGCReport(false)
		res, err := lang.JIT(s, 0)
		if err != nil {
			if jsonOut {
				fmt.Printf("{\"error\": %q, \"backend\": %q, \"exit\": %d}\n", err.Error(), backend, exitCompileError)
			} else {
				fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
			}
			return exitCompileError
		}
		// The target's stderr (uncaught-exception reports, the collector line) is
		// forwarded unchanged: stdout stays the program's, stderr stays the tool's.
		if res.Stderr != "" {
			fmt.Fprint(os.Stderr, res.Stderr)
		}
		gcMember := ""
		if gcStats {
			if st, ok := lang.ParseGCStatsLine(reportLine(res.Stderr)); ok {
				gcMember = gcJSON(st, true)
			}
		}
		if jsonOut {
			fmt.Printf("{\"output\": %q, \"backend\": %q, \"exit\": 0%s}\n", res.Output, backend, gcMember)
		} else {
			fmt.Print(res.Output)
		}
		return exitOK
	}
	ev := lang.NewEvaluator()
	prog, err := lang.Parse(s)
	if err != nil {
		return reportParseErr(err, jsonOut)
	}
	v, err := ev.EvalProgram(prog)
	gc := ev.GCStats()
	if err != nil {
		err = ev.FinalizeTraceback(err)
		ee, isRT := err.(*lang.EvalError)
		tb := ""
		if isRT && len(ee.Traceback) > 0 {
			tb = ee.RenderTraceback()
		}
		if jsonOut {
			fmt.Printf("{\"error\": %q, \"traceback\": %q, \"backend\": %q, \"exit\": %d%s}\n", err.Error(), tb, backend, exitRuntime, gcJSON(gc, gcStats))
		} else if tb != "" {
			// Tracebacks are diagnostics, not program output: they belong on stderr so
			// `prog 2>/dev/null | ...` sees only what the program printed (the AOT
			// backend writes its uncaught-exception report to fd 2 as well).
			fmt.Fprintln(os.Stderr, tb)
		} else {
			if gcStats {
				fmt.Fprintln(os.Stderr, gc.String())
			}
			fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		}
		// The front end accepted the program and it ran; this is a runtime failure,
		// which is exactly the class an agent needs to tell apart from its own
		// program being malformed (exit 1) or the CLI being mis-invoked (exit 4).
		return exitRuntime
	}
	// Echoing the last value is a REPL courtesy for *snippets*, not a program feature:
	// `gustyc --file prog.gy` used to append a stray "0" (the void value print() returned)
	// to every program's stdout, corrupting piped output. Echo only when the program's last
	// statement is a bare expression whose value is a real value (`--eval "x = 1 + 2\nx"`
	// still prints 3); a program ending in a call that yields None prints nothing, matching
	// `python prog.py`. Tracebacks and diagnostics are already stderr-only (ADR 0169).
	finalExpr := false
	if n := len(prog.Stmts); n > 0 {
		_, finalExpr = prog.Stmts[n-1].(*lang.ExprStmt)
	}
	isNone := ev.IsNone(v)
	if jsonOut {
		// The backend is part of the result, not an inference from the flag list:
		// an agent that asked for AOT must be able to *see* it got AOT (Gap M.2).
		if !finalExpr || isNone {
			fmt.Printf("{\"result\": null, \"type\": %q, \"backend\": %q, \"exit\": 0%s}\n", ev.TypeOf(v), backend, gcJSON(gc, gcStats))
		} else {
			fmt.Printf("{\"result\": %q, \"type\": %q, \"backend\": %q, \"exit\": 0%s}\n", ev.Repr(v), ev.TypeOf(v), backend, gcJSON(gc, gcStats))
		}
	} else if finalExpr && !isNone {
		fmt.Println(ev.Repr(v))
	}
	if gcStats && !jsonOut {
		// The report describes the tool, so it never pollutes the program's stdout.
		fmt.Fprintln(os.Stderr, gc.String())
	}
	return exitOK
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

func emitLLVM(src, target, opt string, jsonOut bool) int {
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
	fmt.Print(lang.OptimizeIR(res.IR, atoi(opt)))
	return exitOK
}
func emitSourceMap(src string) int {
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
	if jsonOut {
		fmt.Printf(`{"ok": false, "phase": "compile", "error": %q, "exit": %d}`+"\n", err.Error(), exitCompileError)
		return exitCompileError
	}
	return reportParseErr(err, jsonOut)
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
Benchmarks: gustyc --bench-suite --bench-runs 5            # measure the corpus on both backends
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
expressions: int, float, string, list, dict, binary ops (+ - * / %% == < <= > >= and or not), call, len, attribute, index, lambda
types: int, float, bool, str, list[T], dict[K, V], set[T], tuple[...], Sequence[T], Callable[[...], R], class, function, any
variance: list/set/dict invariant in T, Sequence/iter/tuple covariant, Callable parameters contravariant + return covariant, classes nominal (see gustyc --variance)
effects: async def calls are deferred until awaited; the checker proves the discipline and --effects prints each function's signature (await, yield, raise / returns / falls-through) — see gustyc --effects
values: %s
heap kinds (compiled runtime object headers): %s (0 = not heap-allocated)
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

// benchMode runs a source program through both the AST interpreter and the
// AOT JIT, reports wall-clock timings, and prints a human or JSON report.
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
			fmt.Printf("  interpreter profile: %-6s %8.3f ms\n", p.Phase, p.Ms)
		}
		fmt.Printf("  interpreter: total %8.3f ms  mean %8.3f ms  best %8.3f ms\n", res.Interpreter.TotalMs, res.Interpreter.MeanMs, res.Interpreter.BestMs)
		fmt.Printf("  aot:         total %8.3f ms  mean %8.3f ms  best %8.3f ms\n", res.AOT.TotalMs, res.AOT.MeanMs, res.AOT.BestMs)
		fmt.Printf("  speedup (interp best / aot best): %.2fx\n", res.Speedup)
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
// both backends, optionally gates it against a baseline, and reports.
//
// Exit codes: 0 when clean, 5 (exitBenchRegression) when the gate fires, 1 on a
// tooling error (bad baseline path, unreadable directory).
func benchSuiteMode(useCorpus bool, dir, baselinePath, updatePath string, runs, opt int, tolerance, minMs float64, gate string, jsonOut bool) int {
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
	fmt.Printf("  %-22s %10s %10s %8s\n", "case", "interp ms", "aot ms", "speedup")
	for _, c := range suite.Cases {
		if c.Error != "" {
			fmt.Printf("  %-22s %10s %10s %8s   %s\n", c.Name, "-", "-", "-", c.Error)
			continue
		}
		fmt.Printf("  %-22s %10.3f %10.3f %7.2fx\n", c.Name, c.Interpreter.BestMs, c.AOT.BestMs, c.Speedup)
	}
	fmt.Printf("  %-22s %10.3f %10.3f %7.2fx  (geomean)\n", "TOTAL", suite.Totals.InterpreterMs, suite.Totals.AOTMs, suite.Totals.GeomeanSpeedup)
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
		// caller can report "compiler bug" instead of "your program is wrong".
		return exitIRVerify
	}
	return exitOK
}
