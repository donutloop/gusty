package lang

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
)

// SourceMapEntry maps a source function to its emitted LLVM symbol and the
// IR line where the function definition appears, plus the source span.
type SourceMapEntry struct {
	Name   string `json:"name"`
	Symbol string `json:"symbol"`
	IRLine int    `json:"irLine"`
	Line   int    `json:"line"`
	Col    int    `json:"col"`
}

// SourceMap is the machine-readable debug map for an AOT build. It lets a
// debugger/tool map each source function to the compiled artifact (the LLVM
// IR function symbol and line) produced by GenerateIR.
type SourceMap struct {
	Version   int              `json:"version"`
	Functions []SourceMapEntry `json:"functions"`
}

// defineRe matches an LLVM IR function definition line: `define <ret> @name(`.
// It deliberately skips declarations (`declare`) and internal linkage markers.
var defineRe = regexp.MustCompile(`define\s+[^@]*@([A-Za-z0-9_]+)\(`)

// GenerateSourceMap walks the program AST, collects every user function
// definition (top-level, class methods, and nested defs), and maps each to the
// emitted LLVM IR symbol + line. ir is the textual IR returned by GenerateIR.
// The returned bytes are a JSON SourceMap for `--emit-source-map`.
func GenerateSourceMap(prog *Program, ir string) ([]byte, error) {
	// IR symbol -> 1-based IR line.
	symLine := map[string]int{}
	irLines := strings.Split(ir, "\n")
	for i, ln := range irLines {
		if m := defineRe.FindStringSubmatch(ln); m != nil {
			symLine[m[1]] = i + 1
		}
	}

	// Collect every user FuncDef with its enclosing class name (class methods
	// are mangled to <class>_<method> in the IR).
	fns := []srcFn{}
	collectSrcFns(prog.Stmts, "", &fns)

	entries := []SourceMapEntry{}
	for _, fn := range fns {
		sym := fn.name
		if fn.class != "" {
			sym = fn.class + "_" + fn.name
		}
		e := SourceMapEntry{
			Name:   fn.name,
			Symbol: sym,
			IRLine: symLine[sym],
			Line:   fn.line,
			Col:    fn.col,
		}
		entries = append(entries, e)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Line != entries[j].Line {
			return entries[i].Line < entries[j].Line
		}
		return entries[i].Name < entries[j].Name
	})

	sm := SourceMap{Version: 1, Functions: entries}
	return json.MarshalIndent(sm, "", "  ")
}

// srcFn is a user function definition with its source span and, for class
// methods, the enclosing class name used to compute the IR symbol.
type srcFn struct {
	name  string
	class string
	line  int
	col   int
}

// collectSrcFns appends every user FuncDef reachable from stmts to out, with
// the enclosing class name ("" for top-level/nested functions).
func collectSrcFns(stmts []Stmt, class string, out *[]srcFn) {
	for _, s := range stmts {
		switch n := s.(type) {
		case *FuncDef:
			*out = append(*out, srcFn{name: n.Name, class: class, line: n.Src.Line, col: n.Src.Col})
			collectSrcFns(n.Body, class, out)
		case *ClassDef:
			collectSrcFns(n.Body, n.Name, out)
		case *IfStmt:
			collectSrcFns(n.Then, class, out)
			collectSrcFns(n.Else, class, out)
		case *WhileStmt:
			collectSrcFns(n.Body, class, out)
			collectSrcFns(n.Else, class, out)
		case *ForStmt:
			collectSrcFns(n.Body, class, out)
			collectSrcFns(n.Else, class, out)
		case *TryStmt:
			collectSrcFns(n.Body, class, out)
		case *WithStmt:
			collectSrcFns(n.Body, class, out)
		case *MatchStmt:
			for _, c := range n.Cases {
				collectSrcFns(c.Body, class, out)
			}
		}
	}
}

// EmitSourceMap parses a single source file, analyzes it, generates LLVM IR,
// and returns the JSON source map (source function -> IR symbol + line) for
// the AOT build. It backs the `gustyc --emit-source-map <src>` command.
func EmitSourceMap(src string) ([]byte, error) {
	prog, err := Parse(src)
	if err != nil {
		return nil, err
	}
	Analyze(prog)
	ir, err := GenerateIR(prog)
	if err != nil {
		return nil, err
	}
	return GenerateSourceMap(prog, ir)
}
