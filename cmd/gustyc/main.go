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
	"bufio"
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

func main() {
	os.Exit(run())
}

func run() int {
	fs := flag.NewFlagSet("gustyc", flag.ExitOnError)
	evalSrc := fs.String("eval", "", "evaluate a source string")
	file := fs.String("file", "", "read and evaluate a source file")
	verify := fs.String("verify", "", "parse + analyze a source string")
	emitLLVMF := fs.String("emit-llvm", "", "print LLVM IR for a source string")
	emitASTF := fs.String("emit-ast", "", "print the AST as JSON for a source string")
	target := fs.String("target", "", "target triple for codegen")
	optLevel := fs.String("opt-level", "0", "optimization level")
	jsonOut := fs.Bool("json", false, "emit results/diagnostics as JSON")
	langCmd := fs.Bool("lang", false, "list supported language features")
	schemaCmd := fs.Bool("schema", false, "print the machine-readable JSON schema for the AST/IR dumps")
	version := fs.Bool("version", false, "print version")
	repl := fs.Bool("repl", false, "start an interactive REPL")
	help := fs.Bool("help", false, "show usage")
	fs.Parse(os.Args[1:])

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
	if *repl || (fs.NArg() == 0 && *evalSrc == "" && *file == "" && *verify == "" && *emitLLVMF == "" && *emitASTF == "" && isTTY()) {
		return replMode()
	}

	if *verify != "" {
		return verifySrc(*verify, *jsonOut)
	}
	if *emitLLVMF != "" {
		return emitLLVM(*emitLLVMF, *target, *optLevel)
	}
	if *emitASTF != "" {
		return emitAST(*emitASTF)
	}
	if *evalSrc != "" || *file != "" {
		return evalSrcOrFile(*evalSrc, *file, *jsonOut)
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

func evalSrcOrFile(src, file string, jsonOut bool) int {
	s, err := srcOrFile(src, file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitErr
	}
	ev := lang.NewEvaluator()
	prog, err := lang.Parse(s)
	if err != nil {
		return reportParseErr(err)
	}
	v, err := ev.EvalProgram(prog)
	if err != nil {
		if jsonOut {
			fmt.Printf("{\"error\": %q, \"exit\": %d}\n", err.Error(), exitErr)
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

func replMode() int {
	ev := lang.NewEvaluator()
	fmt.Printf("gustyc %s — type .help, .lang, .quit\n", lang.Version)
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		line := sc.Text()
		switch line {
		case ".quit", ".exit", "q":
			return exitOK
		case ".help":
			fmt.Println("REPL commands: .quit, .lang")
			continue
		case ".lang":
			listLang()
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		prog, err := lang.Parse(line)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			continue
		}
		v, err := ev.EvalProgram(prog)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			continue
		}
		if v != 0 {
			fmt.Println(ev.Repr(v))
		}
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(os.Stderr, "gustyc: %v\n", err)
		return exitErr
	}
	return exitOK
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
