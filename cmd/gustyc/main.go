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
//	--variance          print the generic variance table as JSON (L6.6)
//	--version           print version
//	--repl              start an interactive REPL (default when stdin is a TTY)
//	--help              show usage
//
// Exit codes: 0 = ok, 1 = runtime/eval error, 2 = parse/usage error,
// 5 = benchmark regression (--bench-baseline gate, see docs/operations.md).
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

const exitOK = 0
const exitErr = 1
const exitUsage = 2

// exitBenchRegression is returned by the benchmark gate when a measured case is
// slower than its baseline by more than --bench-tolerance. It is distinct from
// the compile/verify/runtime codes so CI can tell "the compiler got slower"
// apart from "the program is broken".
const exitBenchRegression = 5

const exitNotCanonical = 1

func main() {
	os.Exit(run())
}

func run() int {
	fs := flag.NewFlagSet("gustyc", flag.ExitOnError)
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
	emitLLVMF := fs.String("emit-llvm", "", "print LLVM IR for a source string")
	emitSourceMapF := fs.String("emit-source-map", "", "print the source-map JSON for a source file")
	emitASTF := fs.String("emit-ast", "", "print the AST as JSON for a source string")
	target := fs.String("target", "", "target triple for codegen")
	optLevel := fs.String("opt-level", "0", "optimization level")
	buildOut := fs.String("build", "", "output binary path for a multi-file build (sources are the positional args)")
	debugFlag := fs.Bool("debug", false, "pass -g to llc/cc so the binary carries DWARF debug info")
	sourceMapOut := fs.String("source-map-out", "", "write a JSON source map (source fn -> IR symbol+line) to this path")
	jsonOut := fs.Bool("json", false, "emit results/diagnostics as JSON")
	langCmd := fs.Bool("lang", false, "list supported language features")
	schemaCmd := fs.Bool("schema", false, "print the machine-readable JSON schema for the AST/IR dumps")
	varianceCmd := fs.Bool("variance", false, "print the generic variance table as JSON (list/set/dict invariant, Sequence/iter/tuple covariant, Callable parameters contravariant, classes nominal)")
	abiCmd := fs.Bool("abi", false, "print the versioned gusty extern-fn C ABI schema (JSON)")
	sharedCmd := fs.Bool("shared", false, "emit a position-independent shared library (.so/.dylib) with the stable extern-fn ABI instead of a native executable (with --build)")
	jit := fs.Bool("jit", false, "use the in-process dlopen JIT (codegen -> llc -> cc -shared -> dlopen -> run) instead of the AST interpreter")
	version := fs.Bool("version", false, "print version")
	repl := fs.Bool("repl", false, "start an interactive REPL")
	lsp := fs.Bool("lsp", false, "run the language server over stdio (LSP)")
	help := fs.Bool("help", false, "show usage")

	fmtSrc := fs.String("fmt", "", "format a source string to canonical gusty source (machine: deterministic stdout)")
	fmtCheck := fs.Bool("fmt-check", false, "verify a source is already canonical; exit 0 if canonical, 1 if not (with --json: machine report)")
	stdlibDir := fs.String("stdlib", "", "standard-library root directory (default: GUSTY_STDLIB_DIR or a discovered ./stdlib)")
	fmtFile := fs.String("fmt-file", "", "path to a source file to format/check (alternative to --file with --fmt)")
	fs.Parse(os.Args[1:])

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
			return exitErr
		}
		fmt.Println(abiSchema)
		return exitOK
	}
	if *varianceCmd {
		doc, err := lang.VarianceJSON()
		if err != nil {
			fmt.Fprintln(os.Stderr, "gustyc: variance: "+err.Error())
			return exitErr
		}
		fmt.Println(doc)
		return exitOK
	}

	if *buildOut != "" {
		buildFiles := fs.Args()
		if len(buildFiles) == 0 {
			fmt.Fprintf(os.Stderr, "gustyc: --build requires at least one source file\n")
			usage(fs)
			return exitUsage
		}
		res, err := lang.BuildWithOptions(buildFiles, *buildOut, atoi(*optLevel), &lang.BuildOptions{Debug: *debugFlag, SourceMapOut: *sourceMapOut, Shared: *sharedCmd})
		if err != nil {
			// machine mode: still emit the (partial) BuildResult carrying
			// diagnostics on stdout, plus a human error on stderr.
			if *jsonOut && res != nil {
				b, jerr := json.Marshal(res)
				if jerr == nil {
					fmt.Println(string(b))
				}
			}
			if res != nil && len(res.Diagnostics) > 0 {
				for _, d := range res.Diagnostics {
					fmt.Fprintln(os.Stderr, d)
				}
			} else {
				fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
			}
			return exitErr
		}
		if *jsonOut {
			b, jerr := json.Marshal(res)
			if jerr != nil {
				fmt.Fprintf(os.Stderr, "gustyc: json: %v\n", jerr)
				return exitErr
			}
			fmt.Println(string(b))
		} else {
			fmt.Printf("built %s (%d source files, %d object file(s))\n", res.Output, len(buildFiles), len(res.Objects))
		}
		return exitOK
	}
	if *repl || (fs.NArg() == 0 && *evalSrc == "" && *file == "" && *verify == "" && *check == "" && *emitLLVMF == "" && *emitASTF == "" && *emitSourceMapF == "" && *benchSrc == "" && *benchFile == "" && *benchSuite == false && *benchDir == "" && *benchBaselineUpdate == "" && isTTY()) {
		return replMode(*jit)
	}

	if *verify != "" {
		return verifySrc(*verify, *jsonOut)
	}
	if *check != "" {
		return runCheck(*check, nil, *jsonOut)
	}
	// `gusty check <file1> <file2> ...` : type-check files without executing.
	if fs.NArg() > 0 && fs.Arg(0) == "check" && *buildOut == "" && *evalSrc == "" {
		return runCheck("", fs.Args()[1:], *jsonOut)
	}
	if *emitLLVMF != "" {
		return emitLLVM(*emitLLVMF, *target, *optLevel, *jsonOut)
	}
	if *emitSourceMapF != "" {
		return emitSourceMap(*emitSourceMapF)
	}
	if *emitASTF != "" {
		return emitAST(*emitASTF)
	}
	if *benchSuite || *benchDir != "" || *benchBaselineUpdate != "" {
		return benchSuiteMode(*benchSuite, *benchDir, *benchBaseline, *benchBaselineUpdate, *benchRuns, *benchOpt, *benchTolerance, *benchMinMs, *benchGate, *jsonOut)
	}
	if *benchSrc != "" || *benchFile != "" {
		return benchMode(*benchSrc, *benchFile, *benchRuns, *benchOpt, *jsonOut)
	}
	if *evalSrc != "" || *file != "" {
		return evalSrcOrFile(*evalSrc, *file, *jsonOut, *jit)
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

func evalSrcOrFile(src, file string, jsonOut, jitMode bool) int {
	s, err := srcOrFile(src, file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitErr
	}
	if jitMode {
		res, err := lang.JIT(s, 0)
		if err != nil {
			if jsonOut {
				fmt.Printf("{\"error\": %q, \"exit\": %d}\n", err.Error(), exitErr)
			} else {
				fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
			}
			return exitErr
		}
		if jsonOut {
			fmt.Printf("{\"output\": %q, \"exit\": 0}\n", res.Output)
		} else {
			fmt.Print(res.Output)
		}
		return exitOK
	}
	ev := lang.NewEvaluator()
	prog, err := lang.Parse(s)
	if err != nil {
		return reportParseErr(err)
	}
	v, err := ev.EvalProgram(prog)
	if err != nil {
		err = ev.FinalizeTraceback(err)
		ee, isRT := err.(*lang.EvalError)
		tb := ""
		if isRT && len(ee.Traceback) > 0 {
			tb = ee.RenderTraceback()
		}
		if jsonOut {
			fmt.Printf("{\"error\": %q, \"traceback\": %q, \"exit\": %d}\n", err.Error(), tb, exitErr)
		} else if tb != "" {
			fmt.Println(tb)
		} else {
			fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		}
		return exitErr
	}
	if jsonOut {
		fmt.Printf("{\"result\": %q, \"type\": %q, \"exit\": 0}\n", ev.Repr(v), ev.TypeOf(v))
	} else {
		fmt.Println(ev.Repr(v))
	}
	return exitOK
}

func verifySrc(src string, jsonOut bool) int {
	prog, err := lang.Parse(src)
	if err != nil {
		if jsonOut {
			fmt.Printf("{\"error\": %q, \"exit\": %d}\n", err.Error(), exitUsage)
		}
		return reportParseErr(err)
	}
	diags := lang.Analyze(prog)
	if len(diags) > 0 {
		if jsonOut {
			emitDiagnosticsJSON(diags, exitErr)
		} else {
			for _, d := range diags {
				fmt.Fprintf(os.Stderr, "%v\n", d)
			}
		}
		return exitErr
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
		return exitErr
	}
	fmt.Println(string(sm))
	return exitOK
}

func emitAST(src string) int {
	res, err := lang.Compile(src)
	if err != nil {
		return reportParseErr(err)
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
		fmt.Printf(`{"ok": false, "phase": "compile", "error": %q, "exit": %d}`+"\n", err.Error(), exitUsage)
		return exitUsage
	}
	return reportParseErr(err)
}

func reportParseErr(err error) int {
	if pes, ok := err.(*lang.ParseErrors); ok {
		// panic-mode recovery surfaces a forest of parse errors: print each.
		for _, pe := range pes.Errors {
			fmt.Fprintf(os.Stderr, "gustyc: parse error at %d:%d: %s\n", pe.Span.Line, pe.Span.Col, pe.Msg)
		}
		return exitUsage
	}
	fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
	return exitUsage
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
Variance: gustyc --variance  # JSON variance table (list/set/dict invariant, Sequence covariant, Callable params contravariant)
Benchmarks: gustyc --bench-suite --bench-runs 5            # measure the corpus on both backends
            gustyc --bench-dir integration/programs        # benchmark the parity programs too
            gustyc --bench-suite --bench-baseline-update benchmarks/baseline.json   # record a baseline
            gustyc --bench-suite --bench-baseline benchmarks/baseline.json          # gate (exit 5 = slower than baseline)

Diagnostic codes (--check --json): type.mismatch, type.variance.invariant,
type.variance.covariant, type.variance.contravariant, type.variance.nominal,
type.callable.arity, type.union.members — see docs/operations.md.

Exit codes: 0 = ok, 1 = runtime/eval error, 2 = parse/usage error.
`)
}

func listLang() {
	fmt.Printf(`gusty language features (%s)
statements: assign, print, if/elif/else, while, for-in-range, def/return, pass, match, try/except/finally, raise, class, import
expressions: int, float, string, list, dict, binary ops (+ - * / %% == < <= > >= and or not), call, len, attribute, index, lambda
types: int, float, bool, str, list[T], dict[K, V], set[T], tuple[...], Sequence[T], Callable[[...], R], class, function, any
variance: list/set/dict invariant in T, Sequence/iter/tuple covariant, Callable parameters contravariant + return covariant, classes nominal (see gustyc --variance)
`, lang.Version)
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
				fmt.Printf(`{"ok": false, "error": %q, "exit": %d}`+"\n", err.Error(), exitErr)
			}
			return exitErr
		}
		input = string(b)
	}
	f, err := lang.FormatSrc(input)
	if err != nil {
		if jsonOut {
			fmt.Printf(`{"ok": false, "error": %q, "exit": %d}`+"\n", err.Error(), exitErr)
		}
		return exitErr
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
			return exitErr
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

// benchMode runs a source program through both the AST interpreter and the
// AOT JIT, reports wall-clock timings, and prints a human or JSON report.
func benchMode(src, file string, runs, opt int, jsonOut bool) int {
	src, err := srcOrFile(src, file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitErr
	}
	res, err := lang.Benchmark(src, runs, opt)
	if err != nil {
		for _, d := range res.Diagnostics {
			fmt.Fprintf(os.Stderr, "bench: %v\n", d)
		}
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitErr
	}
	if jsonOut {
		out, jerr := json.MarshalIndent(res, "", "  ")
		if jerr != nil {
			fmt.Fprintf(os.Stderr, "gustyc: json: %v\n", jerr)
			return exitErr
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
			return exitErr
		}
		if len(fromDir) == 0 {
			fmt.Fprintf(os.Stderr, "gustyc: bench: no .gy files in %s\n", dir)
			return exitErr
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
			fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
			return exitErr
		}
		regressions, newCases = lang.CompareBenchSuite(suite, base, tolerance, minMs, gate)
		if len(regressions) > 0 {
			code = exitBenchRegression
		}
	}
	if updatePath != "" {
		if err := lang.SaveBenchBaseline(updatePath, lang.BaselineFromSuite(suite)); err != nil {
			fmt.Fprintf(os.Stderr, "gustyc: bench: write baseline: %v\n", err)
			return exitErr
		}
	}

	if jsonOut {
		out, err := json.MarshalIndent(benchSuiteReport{BenchSuite: suite, Regressions: regressions, NewCases: newCases, Exit: code}, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "gustyc: json: %v\n", err)
			return exitErr
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
