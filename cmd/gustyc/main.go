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
//	--version           print version
//	--repl              start an interactive REPL (default when stdin is a TTY)
//	--help              show usage
//
// Exit codes: 0 = ok, 1 = runtime/eval error, 2 = parse/usage error.
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

	if *buildOut != "" {
		buildFiles := fs.Args()
		if len(buildFiles) == 0 {
			fmt.Fprintf(os.Stderr, "gustyc: --build requires at least one source file\n")
			usage(fs)
			return exitUsage
		}
		res, err := lang.BuildWithOptions(buildFiles, *buildOut, atoi(*optLevel), &lang.BuildOptions{Debug: *debugFlag, SourceMapOut: *sourceMapOut})
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
	if *repl || (fs.NArg() == 0 && *evalSrc == "" && *file == "" && *verify == "" && *check == "" && *emitLLVMF == "" && *emitASTF == "" && *emitSourceMapF == "" && *benchSrc == "" && *benchFile == "" && isTTY()) {
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
		return emitLLVM(*emitLLVMF, *target, *optLevel)
	}
	if *emitSourceMapF != "" {
		return emitSourceMap(*emitSourceMapF)
	}
	if *emitASTF != "" {
		return emitAST(*emitASTF)
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

func emitLLVM(src, target, opt string) int {
	res, err := lang.Compile(src)
	if err != nil {
		return reportParseErr(err)
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

func reportParseErr(err error) int {
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
Check: gustyc --check <src> | gustyc check <file1> <file2> ...  # mypy-style type-check without executing

Exit codes: 0 = ok, 1 = runtime/eval error, 2 = parse/usage error.
`)
}

func listLang() {
	fmt.Printf(`gusty language features (%s)
statements: assign, print, if/elif/else, while, for-in-range, def/return, pass, match, try/except/finally, raise, class, import
expressions: int, float, string, list, dict, binary ops (+ - * / %% == < <= > >= and or not), call, len, attribute, index, lambda
types: int, float, str, list, dict, class, function
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
