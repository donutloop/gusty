package lang

import (
	"encoding/json"
	"fmt"
	"os"
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
// debugger/tool map each source function to the compiled artifact (the LLVM IR
// function symbol and line) produced by GenerateIR.
//
// Version 2 adds `lines` and `debug`: the IR-line-to-source-line table, which is the
// same fact the DWARF `.debug_line` section carries (L8.5, ADR 0231), available with no
// object file and no LLVM toolchain. Attaching a `!dbg` record only ever extends a line
// and appends metadata at the end of the module, so a debug build and a plain build
// number their IR lines identically and these rows read against either.
type SourceMap struct {
	Version   int              `json:"version"`
	Functions []SourceMapEntry `json:"functions"`
	Lines     []DebugLineRow   `json:"lines,omitempty"`
	Debug     *DebugInfo       `json:"debug,omitempty"`
}

// defineRe matches an LLVM IR function definition line: `define <ret> @name(`.
// It deliberately skips declarations (`declare`) and internal linkage markers.
var defineRe = regexp.MustCompile(`define\s+[^@]*@([A-Za-z0-9_]+)\(`)

// GenerateSourceMap walks the program AST, collects every user function
// definition (top-level, class methods, and nested defs), and maps each to the
// emitted LLVM IR symbol + line. ir is the textual IR returned by GenerateIR; dbg is
// the debug account of the same program (L8.5), or nil for a map without a line table.
// The returned bytes are a JSON SourceMap for `--emit-source-map`.
func GenerateSourceMap(prog *Program, ir string, dbg *DebugInfo) ([]byte, error) {
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
		// The map's `name` is what the program wrote; `symbol` is what the linker sees —
		// the emitted name, prefix and all (Gap R.4, ADR 0198). Building it here through
		// the same helper codegen mints symbols with is what keeps the two halves of the
		// map pointed at the same function; a hand-written concatenation would silently
		// look up `f` in a module that only contains `gy_f` and report no IR line.
		sym := irSymbol(fn.name)
		if fn.class != "" {
			sym = irSymbol(fn.class + "_" + fn.name)
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

	sm := SourceMap{Version: 2, Functions: entries, Lines: sourceMapLines(dbg), Debug: sourceMapDebug(dbg)}
	return json.MarshalIndent(sm, "", "  ")
}

// sourceMapLines returns the IR-to-source rows the map should carry, or nil when the
// build produced no debug info at all.
func sourceMapLines(dbg *DebugInfo) []DebugLineRow {
	if dbg == nil {
		return nil
	}
	return dbg.Lines
}

// sourceMapDebug keeps only what identifies the DWARF a map describes: an agent
// reading a source map needs the file, the language and the counts, not a second copy
// of the rows the `lines` member already carries.
func sourceMapDebug(dbg *DebugInfo) *DebugInfo {
	if dbg == nil {
		return nil
	}
	cp := *dbg
	cp.Lines = nil
	return &cp
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

// EmitSourceMap parses source text, analyzes it, and returns the JSON source map
// (source function -> IR symbol + line, plus the IR-line-to-source-line table) for the
// AOT build. It backs the `gustyc --emit-source-map` command.
//
// The line table is a property of a debug build, so this compiles the program twice:
// once without records, whose IR numbering the map reports, and once with, whose pass
// produces the rows. Keeping them apart is deliberate — the module an agent inspects
// with `--emit-llvm` is the one the compiler builds by default, and it must not change
// because somebody asked a question about it.
func EmitSourceMap(src string) ([]byte, error) {
	return emitSourceMap(src, "")
}

// EmitSourceMapFile is EmitSourceMap for a file on disk, so that the DWARF records name
// the file a debugger would try to open (L8.5).
func EmitSourceMapFile(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("source map: read %s: %w", path, err)
	}
	return emitSourceMap(string(b), path)
}

func emitSourceMap(src, path string) ([]byte, error) {
	prog, err := Parse(src)
	if err != nil {
		return nil, err
	}
	Analyze(prog)
	ir, err := GenerateIR(prog)
	if err != nil {
		return nil, err
	}
	opts := &DebugOptions{}
	if path != "" {
		opts.FromFile(path)
	}
	_, dbg, derr := GenerateIRReport(prog, &IRGenOptions{Debug: opts})
	if derr != nil {
		// The line table is the point of the document; a program that cannot produce it
		// must say so rather than hand back half a map.
		return nil, derr
	}
	return GenerateSourceMap(prog, ir, dbg)
}
