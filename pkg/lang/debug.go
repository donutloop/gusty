package lang

// debug.go — L8.5: debug line tables in the IR.
//
// DWARF does not come from `cc -g`. It comes from the module: the object file is
// produced by `llc`, and `llc` writes a `.debug_line` table only out of `!dbg`
// records that are already in the IR. Until this file the compiler emitted none,
// so `gustyc --build out.bin prog.gy --debug` linked with `-g` and produced a
// binary with *no* line table at all — while docs/operations.md claimed the
// opposite ("passes -g to the final cc link so the binary carries DWARF debug
// info (line tables)"). The flag was wired to the one tool that cannot supply it,
// and nothing could see the difference because nothing read the artifact. (A
// debugger attached to a compiled program could not name the source line of
// anything, and Gap K.8's "one traceback frame per stack level" is waiting on
// exactly this.)
//
// The design keeps the emitter textual, like everything else here: codegen records
// *where* each statement's IR was written (a byte offset into the builder plus the
// statement's source span), and one post-pass over the assembled module turns
// those marks into `!dbg` metadata — a `DICompileUnit`, one `DISubprogram` per
// program function, and one `DILocation` per instruction. The pass only ever
// *appends* to a line and adds metadata nodes at the end of the module, so the IR
// line numbering of a debug module and a debug-free module is identical: one
// source map serves both builds.
//
// Two rules hold this together, and both are tested:
//
//   - **Only program functions get records.** The compiler emits its own runtime
//     blocks (`rt_gc`, `rt_str_intern`, …); those are not source, and LLVM's
//     verifier rejects an instruction whose `!dbg` scope is not the enclosing
//     function's subprogram. A function therefore gets locations only if it
//     registered a subprogram, which only `funcDef`, `emitClassMethod`, the
//     closure/decorator thunks and `main` do.
//     `TestEveryProgramDefineCarriesASubprogram` fails if a new emitting path
//     forgets to register.
//
//   - **The reported table is read back from what was emitted**, not kept in a
//     parallel bookkeeping structure: `DebugInfo`'s counts and rows come from the
//     module text the pass just produced, and `DWARFReport` comes from
//     `llvm-dwarfdump` reading the object file. A claim about the binary is taken
//     from the binary — ADR 0164's rule for verification, applied to debug info: a
//     build whose DWARF is absent reports it absent, whatever the flag said.

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// DebugSchemaVersion is the version of the `--debug-info` / `--build --json`
// debug document (docs/operations.md § Debug info).
const DebugSchemaVersion = 1

// DebugDWARFLanguage is the DWARF language the compile unit claims. The spelling
// is pinned because the token is parsed by name, and a name LLVM does not know is
// an `llc` rejection — which the exit-code contract (ADR 0211) would report as a
// bug in the user's source. Measured against the pinned toolchain:
// `DW_LANG_Python` is accepted; `DW_LANG_PYTHON`, `DW_LANG_python`,
// `DW_LANG_BASIC` and `DW_LANG_Carbon` are not.
const DebugDWARFLanguage = "DW_LANG_Python"

// DWARFEmissionKind is how much debug info the compile unit asks for.
const DWARFEmissionKind = "FullDebug"

// DWARFDumpTool is the tool a line table is read back with. Package-level var so
// tests can point it elsewhere, exactly like llcCmd/optCmd (docs/operations.md).
var dwarfdumpCmd = "llvm-dwarfdump-20"

// DebugOptions asks codegen to emit DWARF line records into the module.
type DebugOptions struct {
	// File is the source file name recorded in the DIFile (DWARF's "prog.gy").
	File string
	// Directory is DIFile's directory; "" means the file name is all we have.
	Directory string
	// Producer names the compiler ("gusty 0.10.0"); defaults to Version.
	Producer string
	// Language is the DWARF language token; defaults to DebugDWARFLanguage.
	Language string
	// OptLevel only sets DW_AT_optimized, which tells a debugger whether it may
	// claim a value was optimized out.
	OptLevel int
	// LineTableCap caps how many IR-line rows the machine document carries;
	// zero means all of them. The counts are never capped.
	LineTableCap int
}

// normalized fills in the defaults, so a caller can pass &DebugOptions{File:"x"}.
func (o *DebugOptions) normalized() *DebugOptions {
	if o == nil {
		return nil
	}
	c := *o
	if c.Producer == "" {
		c.Producer = "gusty " + Version
	}
	if c.Language == "" {
		c.Language = DebugDWARFLanguage
	}
	if c.File == "" {
		c.File = "prog.gy"
	}
	return &c
}

// FromFile points the debug info at a real source file, so a debugger finds it:
// the DIFile carries the base name and the absolute directory.
func (o *DebugOptions) FromFile(path string) *DebugOptions {
	if o == nil {
		o = &DebugOptions{}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	o.File = filepath.Base(abs)
	o.Directory = filepath.Dir(abs)
	return o
}

// IRGenOptions carries the per-invocation choices of the IR emitter, so that
// asking for debug info does not need a new exported entry point per option.
type IRGenOptions struct {
	// Debug, when non-nil, attaches a `!dbg` record to every instruction of every
	// program function and emits the compile unit's metadata.
	Debug *DebugOptions
	// EchoResult asks for the REPL's courtesy: the value of a snippet's final bare expression
	// is written to the tool channel (fd 2) as one `gusty: result <kind> <repr>` line, so an
	// interactive caller sees the answer it asked for instead of only what the program printed.
	//
	// It is opt-in and only ever set by a REPL/`--eval` caller. A program's stdout is only what
	// the program printed (ADR 0204), and the answer is not program output — it is the tool
	// answering "what did that expression evaluate to?", on the same channel the collector's
	// self-report and an uncaught-exception report use (ADR 0179). Roadmap L13.1, ADR 0302.
	EchoResult bool
}

// --- machine-readable accounts --------------------------------------------------

// DebugFunction is one program function as the debug info describes it.
type DebugFunction struct {
	Name   string `json:"name"`   // the name the program wrote ("total", "lambda_0")
	Symbol string `json:"symbol"` // the symbol the linker sees ("gy_total")
	Line   int    `json:"line"`   // the source line the definition sits on
	// Tagged counts the function's instruction lines that carry a !dbg record.
	Tagged int `json:"instructions"`
	// Locations counts distinct source positions inside the function.
	Locations int `json:"locations"`
}

// DebugLineRow is one row of the compiler's own line table: which source line an
// emitted IR line was written for. It is the machine-readable twin of the DWARF
// `.debug_line` table, which needs both a toolchain and an object file to read.
type DebugLineRow struct {
	IRLine   int    `json:"irLine"`
	Line     int    `json:"line"`
	Col      int    `json:"col"`
	Function string `json:"function,omitempty"`
}

// DebugInfo is the compiler-side account of the debug info a module carries, read
// back from the module text the emitter produced. It backs `gustyc --debug-info`
// and the `debug` member of `--build --json`.
type DebugInfo struct {
	SchemaVersion int    `json:"schema_version"`
	File          string `json:"file"`
	Directory     string `json:"directory"`
	Producer      string `json:"producer"`
	Language      string `json:"language"`
	EmissionKind  string `json:"emission_kind"`
	IsOptimized   bool   `json:"is_optimized"`
	CompileUnit   string `json:"compile_unit"` // the metadata id of the DICompileUnit

	Functions []DebugFunction `json:"functions"`
	Lines     []DebugLineRow  `json:"lines,omitempty"`

	Instructions int  `json:"instructions"` // instruction lines inside program functions
	Tagged       int  `json:"tagged"`       // of those, how many carry a !dbg
	Locations    int  `json:"locations"`    // DILocation nodes emitted
	Subprograms  int  `json:"subprograms"`  // DISubprogram nodes emitted
	Truncated    bool `json:"lines_truncated"`
	// Defect is set when the module's own metadata disagrees with what the compiler
	// intended to write — a record a debugger would misread. Empty means the two agree.
	Defect string `json:"defect,omitempty"`
}

// SourceLineSet returns the distinct source lines the table covers, ascending —
// the answer to "will a debugger know line N of my program?".
func (d *DebugInfo) SourceLineSet() []int {
	if d == nil {
		return nil
	}
	seen := map[int]bool{}
	out := []int{}
	for _, r := range d.Lines {
		if !seen[r.Line] {
			seen[r.Line] = true
			out = append(out, r.Line)
		}
	}
	sort.Ints(out)
	return out
}

// String renders the human line the CLI prints under --build.
func (d *DebugInfo) String() string {
	if d == nil {
		return "no debug info"
	}
	return fmt.Sprintf("DWARF: %s (%s), %d subprogram(s), %d/%d instruction(s) tagged, %d location(s)",
		filepath.Base(d.File), d.Language, d.Subprograms, d.Tagged, d.Instructions, d.Locations)
}

// DWARFReport is what the object file actually says about its line table. Like
// IRVerification, "we could not look" is never reported as "it is there".
type DWARFReport struct {
	Tool      string `json:"tool"`
	Toolchain string `json:"toolchain,omitempty"`
	Ran       bool   `json:"ran"`
	Skipped   bool   `json:"skipped"`
	OK        bool   `json:"ok"`
	// LineRows counts every row of the table, including the ones that name no source
	// line: the compiler's runtime blocks are code the program never wrote.
	LineRows    int      `json:"line_rows"`
	SourceLines []int    `json:"source_lines,omitempty"`
	Files       []string `json:"files,omitempty"`
	Note        string   `json:"note,omitempty"`
}

// String renders the human line under --build.
func (r *DWARFReport) String() string {
	if r == nil {
		return ""
	}
	switch {
	case r.Skipped:
		return "NOT DWARF-checked: " + r.Note
	case !r.OK:
		return "no DWARF line table: " + r.Note
	default:
		return fmt.Sprintf("DWARF line table: %d row(s) over %d source line(s) for %s",
			r.LineRows, len(r.SourceLines), strings.Join(r.Files, ", "))
	}
}

// Covers reports whether the artifact's line table can answer for a source line.
func (r *DWARFReport) Covers(line int) bool {
	if r == nil {
		return false
	}
	for _, l := range r.SourceLines {
		if l == line {
			return true
		}
	}
	return false
}

// --- codegen's bookkeeping -------------------------------------------------------
//
// Marks are byte offsets into the builder that received the write. Program code is
// written into two builders (the module body, and `globals`, which receives class
// methods, lambdas and decorated thunks), so a mark says which one it belongs to
// and the attach pass shifts it by that section's eventual offset in the module.

type dbgMark struct {
	off       int
	line      int
	col       int
	inGlobals bool
}

type dbgFunc struct {
	sym       string
	name      string
	off       int
	line      int
	col       int
	inGlobals bool

	// filled in by the attach pass
	sigText string
	subpID  int
	located int
}

// dbgMark records "everything written from here on belongs to the statement at
// sp". A statement with no span (a compiler-synthesized body such as a lambda's
// return) keeps the position already in effect, which for the first statement of a
// function is the function's own line — better than claiming line 0, which DWARF
// has no meaning for.
func (g *irGen) dbgMark(b *strings.Builder, sp Span) {
	if g == nil || sp.Line <= 0 {
		return
	}
	g.dbgMarks = append(g.dbgMarks, dbgMark{off: b.Len(), line: sp.Line, col: dbgCol(sp), inGlobals: b == &g.globals})
}

// dbgDefine records a function definition so the attach pass can give it a
// DISubprogram. Call it immediately before writing the `define` line.
func (g *irGen) dbgDefine(b *strings.Builder, sym, name string, sp Span) {
	if g == nil || sym == "" {
		return
	}
	line, col := sp.Line, dbgCol(sp)
	if line <= 0 {
		line, col = 1, 0
	}
	off := b.Len()
	g.dbgFuncs = append(g.dbgFuncs, dbgFunc{sym: sym, name: name, off: off, line: line, col: col, inGlobals: b == &g.globals})
	// The definition's own position is the first mark inside the function, so the
	// prologue the emitter writes before the first statement is attributed to the
	// `def` rather than to whatever statement ran last.
	g.dbgMarks = append(g.dbgMarks, dbgMark{off: off, line: line, col: col, inGlobals: b == &g.globals})
}

func dbgCol(sp Span) int {
	if sp.Col < 0 {
		return 0
	}
	return sp.Col
}

// --- the attach pass -------------------------------------------------------------

// dbgNodes interns metadata nodes and hands out module-wide unique ids.
type dbgNodes struct {
	base   int
	byKey  map[string]int // distinct nodes, keyed by caller-chosen key
	byText map[string]int // shared nodes, keyed by their text
	text   map[int]string
	order  []int
}

func newDbgNodes(base int) *dbgNodes {
	return &dbgNodes{base: base, byKey: map[string]int{}, byText: map[string]int{}, text: map[int]string{}}
}

func (n *dbgNodes) nextID() int { return n.base + len(n.order) }

func (n *dbgNodes) put(node string) int {
	if id, ok := n.byText[node]; ok {
		return id
	}
	id := n.nextID()
	n.byText[node] = id
	n.text[id] = node
	n.order = append(n.order, id)
	return id
}

// intern returns the id of a node, shared with any identical node already added.
// Clang shares identical DILocations and DIBasicTypes; so does this.
func (n *dbgNodes) intern(node string) int { return n.put(node) }

// distinct returns the id of a node that must stay unique even if another has the
// same text (two DISubprograms with the same signature are two functions).
func (n *dbgNodes) distinct(key, node string) int {
	if id, ok := n.byKey[key]; ok {
		return id
	}
	id := n.put(node)
	n.byKey[key] = id
	return id
}

var dbgNodeDefRe = regexp.MustCompile(`^!(\d+) = `)

// nextFreeMDID returns the lowest metadata id the module does not already define.
// The emitter uses !0 for its PIC Level flag; debug nodes must not collide with
// whatever a future module tail happens to define.
func nextFreeMDID(module string) int {
	max := -1
	for _, ln := range strings.Split(module, "\n") {
		if m := dbgNodeDefRe.FindStringSubmatch(ln); m != nil {
			if v, err := strconv.Atoi(m[1]); err == nil && v > max {
				max = v
			}
		}
	}
	return max + 1
}

// dbgMDText quotes a name for a metadata string. Names come from the program's own
// identifiers; anything outside the safe set is dropped rather than escaped, so no
// name can terminate its literal early (the unescaped-quote lesson of Gap K.6,
// which made a module fail to verify with a nonsense array length).
func dbgMDText(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '_' || r == '$' || r == '.' || r == '-':
			b.WriteRune(r)
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune(' ')
		}
	}
	b.WriteByte('"')
	return b.String()
}

var dbgKeyedRe = regexp.MustCompile(`^%[A-Za-z0-9_.$]+\s*=\s*(\S+)`)

// dbgBareOps are the mnemonics that begin an instruction line without a `%t =`.
// Anything else that can start a body line — a label, a `}`, a comment, a
// multi-line instruction's continuation such as a switch arm — is not an
// instruction, and appending metadata inside a multi-line instruction produces a
// module `llc` rejects, which ADR 0166 calls a compiler bug.
var dbgBareOps = map[string]bool{
	"store": true, "ret": true, "br": true, "switch": true, "invoke": true,
	"call": true, "unreachable": true, "resume": true, "landingpad": true,
	"cleanupret": true, "catchret": true, "fence": true, "cmpxchg": true, "atomicrmw": true,
}

// dbgIsInstruction reports whether a body line is an instruction that may carry a
// !dbg record. When in doubt the answer is no: an untagged instruction costs a
// debugger one row, a wrongly tagged one loses the whole module at `llc`.
func dbgIsInstruction(line string) bool {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, ";") || strings.HasPrefix(s, "!") || strings.HasPrefix(s, "@") {
		return false
	}
	if s == "}" || strings.HasSuffix(s, "}") {
		return false
	}
	code, _ := dbgSplitComment(s)
	if code == "" || strings.HasSuffix(code, ":") {
		return false
	}
	if m := dbgKeyedRe.FindStringSubmatch(code); m != nil {
		// `%t = <op> ...` — the only keyed form the emitter produces.
		return m[1] != "" && !strings.HasPrefix(m[1], "!")
	}
	tok := code
	if i := strings.IndexAny(code, " \t("); i > 0 {
		tok = code[:i]
	}
	return dbgBareOps[tok]
}

// dbgSplitComment splits an IR line into code and trailing comment, honouring
// string literals so a `;` inside `!"a;b"` is not read as a comment start.
func dbgSplitComment(s string) (code, comment string) {
	inStr := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inStr = !inStr
		case '\\':
			if inStr {
				i++
			}
		case ';':
			if !inStr {
				return strings.TrimRight(s[:i], " \t"), strings.TrimLeft(s[i:], " ")
			}
		}
	}
	return s, ""
}

// attachDebugInfo adds the DWARF line table to an assembled module.
//
// globalsPrefix / bodyPrefix are the byte offsets at which the `globals` builder's
// and the body builder's contents begin in `module`; marks were recorded relative
// to those builders. The pass rewrites lines in place and never adds or removes
// one, so a debug module and a debug-free module share line numbering — which is
// what lets one source map describe either build.
func attachDebugInfo(module string, marks []dbgMark, funcs []dbgFunc, globalsPrefix, bodyPrefix int, opts *DebugOptions) (string, *dbgArtifact) {
	opts = opts.normalized()

	bySym := map[string]*dbgFunc{}
	funcList := []*dbgFunc{}
	for _, f := range funcs {
		if _, dup := bySym[f.sym]; dup {
			// A symbol emitted twice (a decorated function's clones) keeps the first.
			continue
		}
		f.off += dbgSectionOffset(f.inGlobals, globalsPrefix, bodyPrefix)
		cp := f
		bySym[f.sym] = &cp
		funcList = append(funcList, &cp)
	}
	tmarks := make([]dbgMark, 0, len(marks))
	for _, m := range marks {
		tmarks = append(tmarks, dbgMark{off: m.off + dbgSectionOffset(m.inGlobals, globalsPrefix, bodyPrefix), line: m.line, col: m.col})
	}
	sort.SliceStable(tmarks, func(i, j int) bool { return tmarks[i].off <= tmarks[j].off })

	nodes := newDbgNodes(nextFreeMDID(module))
	fileNode := nodes.intern(fmt.Sprintf("!DIFile(filename: %s, directory: %s)", dbgMDText(opts.File), dbgMDText(opts.Directory)))
	emptyNode := nodes.intern("!{}")
	cu := nodes.distinct("cu:"+opts.File, fmt.Sprintf(
		"distinct !DICompileUnit(language: %s, file: !%d, producer: %s, isOptimized: %v, runtimeVersion: 0, emissionKind: FullDebug, enums: !%d, retainedTypes: !%d, globals: !%d)",
		opts.Language, fileNode, dbgMDText(opts.Producer), opts.OptLevel > 0, emptyNode, emptyNode, emptyNode))

	// One DIBasicType per IR value type, interned, so two functions with the same
	// signature share their types the way clang's do.
	typeNodes := map[string]int{}
	basicType := func(irType string) string {
		if id, ok := typeNodes[irType]; ok {
			return fmt.Sprintf("!%d", id)
		}
		var node string
		switch irType {
		case "i32":
			node = "!DIBasicType(name: \"int\", size: 32, encoding: DW_ATE_signed)"
		case "double":
			node = "!DIBasicType(name: \"float\", size: 64, encoding: DW_ATE_float)"
		case "i1":
			node = "!DIBasicType(name: \"bool\", size: 8, encoding: DW_ATE_boolean)"
		case "i8":
			node = "!DIBasicType(name: \"byte\", size: 8, encoding: DW_ATE_unsigned)"
		case "i64":
			node = "!DIBasicType(name: \"long\", size: 64, encoding: DW_ATE_signed)"
		default:
			return "null" // pointers and structs: a type the line table cannot name
		}
		id := nodes.intern(node)
		typeNodes[irType] = id
		return fmt.Sprintf("!%d", id)
	}

	locIDs := map[string]int{}
	positions := map[int]dbgPosition{}
	location := func(fn *dbgFunc, line, col int) int {
		key := fmt.Sprintf("%s|%d|%d", fn.sym, line, col)
		if id, ok := locIDs[key]; ok {
			return id
		}
		id := nodes.intern(fmt.Sprintf("!DILocation(line: %d, column: %d, scope: !%d)", line, col, fn.subpID))
		locIDs[key] = id
		positions[id] = dbgPosition{line: line, col: col, fnSym: fn.sym}
		return id
	}

	lines := strings.Split(module, "\n")
	var cur *dbgFunc
	off := 0
	for i := range lines {
		lineStart := off
		off += len(lines[i]) + 1 // + the '\n' Split removed
		trimmed := strings.TrimSpace(lines[i])
		if strings.HasPrefix(trimmed, "define ") {
			sym := dbgDefineSymbol(trimmed)
			f, ok := bySym[sym]
			if !ok {
				// A runtime block, or a define no emitting path registered: it is not
				// source, and an instruction whose scope is not its function's
				// subprogram is a verifier error, so nothing inside it is touched.
				cur = nil
				continue
			}
			cur = f
			f.sigText = trimmed
			if f.subpID == 0 {
				f.subpID = nodes.distinct("sub:"+f.sym, dbgSubprogramText(f, fileNode, cu, nodes, basicType))
			}
			lines[i] = dbgAttachDefineLocation(lines[i], f.subpID)
			continue
		}
		if cur == nil {
			continue
		}
		if trimmed == "}" {
			cur = nil
			continue
		}
		if !dbgIsInstruction(lines[i]) {
			continue
		}
		line, col := dbgActive(tmarks, lineStart, cur)
		id := location(cur, line, col)
		lines[i] = dbgAppendLocation(lines[i], id)
	}

	// The module flags must carry the DWARF and debug-info versions, or LLVM drops
	// the metadata on the floor: `!llvm.dbg.cu` is only read when the module says
	// which debug-info version it speaks.
	patchModuleFlags(lines, []int{
		nodes.intern("!{i32 7, !\"Dwarf Version\", i32 5}"),
		nodes.intern("!{i32 2, !\"Debug Info Version\", i32 3}"),
	})

	var b strings.Builder
	// The tagged lines, not the ones handed in: the pass extended each instruction in
	// place, and the module it returns is the only one anybody else will ever see.
	b.WriteString(strings.Join(lines, "\n"))
	if !strings.HasSuffix(module, "\n") {
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "!llvm.dbg.cu = !{!%d}\n", cu)
	for _, id := range nodes.order {
		fmt.Fprintf(&b, "!%d = %s\n", id, nodes.text[id])
	}

	return b.String(), &dbgArtifact{opts: opts, funcs: funcList, positions: positions, cuText: fmt.Sprintf("!%d", cu)}
}

// dbgArtifact is what the attach pass learned about the module it tagged. The reported
// table is not built here: the emitter hoists every `alloca` to the top of its function
// afterwards (ADR 0181), which moves lines, so counting rows any earlier would publish
// IR positions the shipped module does not have.
type dbgArtifact struct {
	opts      *DebugOptions
	funcs     []*dbgFunc
	positions map[int]dbgPosition
	cuText    string
}

// dbgPosition is the source position one DILocation node stands for.
type dbgPosition struct {
	line  int
	col   int
	fnSym string
}

// The metadata is the record. Every number below is read out of the module's own
// metadata nodes — which node is a DILocation, which DISubprogram an instruction's
// scope names, what file the DIFile spells — because a report built from the emitter's
// bookkeeping would still look perfect when the emitter had written a node LLVM will not
// read (the missing DISubroutineType did exactly that: a full account, and no line table
// in the artifact). Reading the artifact back is what tells the two apart.
func readBackDebugInfo(module string, art *dbgArtifact) *DebugInfo {
	locations := dbgParseLocations(module)
	subprograms := dbgParseSubprograms(module)
	info := &DebugInfo{
		SchemaVersion: DebugSchemaVersion,
		EmissionKind:  DWARFEmissionKind,
	}
	// The header is read from the DICompileUnit the module names, so the report describes
	// the module and not the request that produced it.
	if cu, ok := dbgParseCompileUnit(module); ok {
		info.CompileUnit = cu.id
		info.Producer = cu.producer
		info.Language = cu.language
		info.EmissionKind = cu.emissionKind
		info.IsOptimized = cu.isOptimized
		if f, ok := dbgParseFile(module, cu.fileID); ok {
			info.File, info.Directory = f.name, f.directory
		}
	}
	if art == nil || art.opts == nil {
		// Nothing to fall back to: the module's own metadata is the whole story.
		if info.Language == "" {
			info.Language = DebugDWARFLanguage
		}
	}
	if art != nil && art.opts != nil {
		if info.CompileUnit == "" {
			info.CompileUnit = art.cuText
		}
		if info.File == "" {
			info.File, info.Directory = art.opts.File, art.opts.Directory
		}
		if info.Producer == "" {
			info.Producer, info.Language = art.opts.Producer, art.opts.Language
		}
		if info.EmissionKind == "" {
			info.EmissionKind = DWARFEmissionKind
		}
	}
	info.Subprograms = len(subprograms)
	info.Locations = len(locations)

	// A define belongs to the program when the module itself says so: some DISubprogram
	// names it. That is a better gate than an emitter-side list, and it is why the
	// compiler's own runtime blocks are never claimed as program lines — they have no
	// subprogram, so they have no business in a line table.
	programSyms := map[string]bool{}
	for _, sub := range subprograms {
		if sub.linkageName != "" {
			programSyms[sub.linkageName] = true
		}
	}
	tagged := map[string]int{}
	rows := []DebugLineRow{}
	cur := ""
	for i, ln := range strings.Split(module, "\n") {
		trimmed := strings.TrimSpace(ln)
		if strings.HasPrefix(trimmed, "define ") {
			sym := dbgDefineSymbol(trimmed)
			if programSyms[sym] {
				cur = sym
			} else {
				cur = ""
			}
			continue
		}
		if cur == "" {
			continue
		}
		if trimmed == "}" {
			cur = ""
			continue
		}
		if !dbgIsInstruction(ln) {
			continue
		}
		info.Instructions++
		id, ok := dbgLocationID(ln)
		if !ok {
			continue
		}
		loc, known := locations[id]
		if !known {
			// The instruction names a metadata id that is not a DILocation: a debugger
			// would fail on it too, so it is not counted as covered.
			continue
		}
		if art != nil {
			// Cross-check against what the emitter meant to write. A module that spells a
			// location differently from the record it was handed is broken, and the report
			// has to say so instead of quietly agreeing with itself.
			if want, ok := art.positions[id]; ok && (want.line != loc.line || want.col != loc.column) {
				if info.Defect == "" {
					info.Defect = fmt.Sprintf("!%d says line %d column %d, the emitter meant %d:%d", id, loc.line, loc.column, want.line, want.col)
				}
				continue
			}
		}
		// And the scope it names must be the subprogram of the function it sits in —
		// a location pointing at another function's scope is a broken record, not a row.
		if sub, ok := subprograms[loc.scope]; !ok || sub.linkageName != cur {
			continue
		}
		info.Tagged++
		tagged[cur]++
		rows = append(rows, DebugLineRow{IRLine: i + 1, Line: loc.line, Col: loc.column, Function: cur})
	}
	for _, f := range funcsToReport(art, subprograms) {
		line := f.line
		name := f.name
		for _, sub := range subprograms {
			if sub.linkageName == f.sym {
				if sub.line > 0 {
					line = sub.line
				}
				if sub.name != "" {
					name = sub.name
				}
				break
			}
		}
		info.Functions = append(info.Functions, DebugFunction{
			Name: name, Symbol: f.sym, Line: line,
			Tagged: tagged[f.sym], Locations: distinctPositions(rows, f.sym),
		})
	}
	if cap := art.lineTableCap(); cap > 0 && len(rows) > cap {
		info.Truncated = true
		rows = rows[:art.opts.LineTableCap]
	}
	info.Lines = rows
	return info
}

// --- metadata readers ---------------------------------------------------------
//
// Small, exact readers over the metadata block the emitter wrote. They are deliberately
// strict: a node that does not parse as the shape LLVM expects is reported as absent, so
// `--debug-info` falls silent rather than claiming a record the debugger cannot use.

type dbgLocation struct {
	line   int
	column int
	scope  int
}

// LLVM accepts a metadata node with or without `distinct`; the reader accepts both, and
// requires the *shape*, so a malformed node is reported as missing rather than counted.
var dbgLocationRe = regexp.MustCompile(`^!(\d+) = (?:distinct )?!DILocation\(line: (\d+), column: (\d+), scope: !(\d+)\)`)
var dbgSubprogramRe = regexp.MustCompile(`^!(\d+) = (?:distinct )?!DISubprogram\(name: "([^"]*)", linkageName: "([^"]*)",.*?\bline: (\d+),`)
var dbgCompileUnitRe = regexp.MustCompile(`^!(\d+) = (?:distinct )?!DICompileUnit\(language: ([A-Za-z_]+), file: !(\d+), producer: "([^"]*)",.*?isOptimized: (true|false),.*?emissionKind: ([A-Za-z]+)`)
var dbgFileRe = regexp.MustCompile(`^!(\d+) = (?:distinct )?!DIFile\(filename: "([^"]*)", directory: "([^"]*)"\)`)

// dbgParseLocations maps a metadata id to the source position it names.
func dbgParseLocations(module string) map[int]dbgLocation {
	out := map[int]dbgLocation{}
	for _, ln := range strings.Split(module, "\n") {
		m := dbgLocationRe.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		line, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		scope, _ := strconv.Atoi(m[4])
		out[id] = dbgLocation{line: line, column: col, scope: scope}
	}
	return out
}

type dbgSubprogram struct {
	name        string
	linkageName string
	line        int
}

// dbgParseSubprograms maps a metadata id to the function its DISubprogram describes.
func dbgParseSubprograms(module string) map[int]dbgSubprogram {
	out := map[int]dbgSubprogram{}
	for _, ln := range strings.Split(module, "\n") {
		m := dbgSubprogramRe.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		line, _ := strconv.Atoi(m[4])
		out[id] = dbgSubprogram{name: m[2], linkageName: m[3], line: line}
	}
	return out
}

type dbgCompileUnit struct {
	id           string
	language     string
	fileID       int
	producer     string
	isOptimized  bool
	emissionKind string
}

func dbgParseCompileUnit(module string) (dbgCompileUnit, bool) {
	for _, ln := range strings.Split(module, "\n") {
		m := dbgCompileUnitRe.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			continue
		}
		fileID, _ := strconv.Atoi(m[3])
		return dbgCompileUnit{
			id:           "!" + m[1],
			language:     m[2],
			fileID:       fileID,
			producer:     m[4],
			isOptimized:  m[5] == "true",
			emissionKind: m[6],
		}, true
	}
	return dbgCompileUnit{}, false
}

type dbgFile struct {
	name      string
	directory string
}

func dbgParseFile(module string, id int) (dbgFile, bool) {
	for _, ln := range strings.Split(module, "\n") {
		m := dbgFileRe.FindStringSubmatch(strings.TrimSpace(ln))
		if m == nil {
			continue
		}
		got, _ := strconv.Atoi(m[1])
		if got != id {
			continue
		}
		return dbgFile{name: m[2], directory: m[3]}, true
	}
	return dbgFile{}, false
}

// funcsToReport lists the functions the account covers: the ones the emitter registered,
// or, when the module is being read with no emitter (a .ll file off disk, a module that
// has been through the optimizer), the ones the module's own DISubprograms name.
func funcsToReport(art *dbgArtifact, subprograms map[int]dbgSubprogram) []*dbgFunc {
	if art != nil {
		return art.funcs
	}
	out := []*dbgFunc{}
	seen := map[string]bool{}
	for _, sub := range subprograms {
		if sub.linkageName == "" || seen[sub.linkageName] {
			continue
		}
		seen[sub.linkageName] = true
		out = append(out, &dbgFunc{sym: sub.linkageName, name: sub.name, line: sub.line})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sym < out[j].sym })
	return out
}

// lineTableCap is the row budget, for the case where nobody asked for one.
func (a *dbgArtifact) lineTableCap() int {
	if a == nil || a.opts == nil {
		return 0
	}
	return a.opts.LineTableCap
}

// lookup finds a registered function by IR symbol. A nil artifact knows no functions:
// the report then describes the module alone, which is what a caller reading a .ll file
// off disk gets.
func (a *dbgArtifact) lookup(sym string) (*dbgFunc, bool) {
	if a == nil {
		return nil, false
	}
	for _, f := range a.funcs {
		if f.sym == sym {
			return f, true
		}
	}
	return nil, false
}

// distinctPositions counts the different source positions inside one function.
func distinctPositions(rows []DebugLineRow, sym string) int {
	seen := map[[2]int]bool{}
	n := 0
	for _, r := range rows {
		if r.Function != sym {
			continue
		}
		k := [2]int{r.Line, r.Col}
		if !seen[k] {
			seen[k] = true
			n++
		}
	}
	return n
}

var dbgLocSuffixRe = regexp.MustCompile(`, !dbg !([0-9]+)(\s+;.*)?$`)

// dbgLocationID reads the !dbg record off an emitted instruction line.
func dbgLocationID(line string) (int, bool) {
	m := dbgLocSuffixRe.FindStringSubmatch(strings.TrimRight(line, " \t"))
	if m == nil {
		return 0, false
	}
	id, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}
	return id, true
}

func dbgSectionOffset(inGlobals bool, globalsPrefix, bodyPrefix int) int {
	if inGlobals {
		return globalsPrefix
	}
	return bodyPrefix
}

// dbgSubprogramText renders the DISubprogram for one function. `type:` is not
// decoration to save: llc crashes in DwarfCompileUnit::constructSubprogramScopeDIE
// on a DISubprogram with no DISubroutineType, so the signature is always emitted —
// read off the `define` line itself, which is the only place the real parameter and
// return types are written down.
func dbgSubprogramText(f *dbgFunc, fileNode, cu int, nodes *dbgNodes, basicType func(string) string) string {
	ret, params := dbgDefineSignature(f.sigText)
	parts := make([]string, 0, len(params)+1)
	if ret == "" || ret == "void" {
		parts = append(parts, "null") // DWARF spells a void return as a null type
	} else {
		parts = append(parts, basicType(ret))
	}
	for _, p := range params {
		parts = append(parts, basicType(p))
	}
	// Two nodes, the way clang emits them: the type list, and a DISubroutineType that
	// names it. Pointing `type:` straight at a bare `!{...}` list makes llc say
	// "invalid subroutine type" and then quietly write no line table at all — the
	// artifact, not the emitter, is what caught this (ADR 0231).
	listID := nodes.intern("!{" + strings.Join(parts, ", ") + "}")
	sig := "!" + strconv.Itoa(nodes.intern(fmt.Sprintf("!DISubroutineType(types: !%d)", listID)))
	return fmt.Sprintf("distinct !DISubprogram(name: %s, linkageName: %s, scope: !%d, file: !%d, line: %d, scopeLine: %d, flags: DIFlagPrototyped, spFlags: DISPFlagDefinition, unit: !%d, type: %s, retainedNodes: !%d)",
		dbgMDText(f.name), dbgMDText(f.sym), fileNode, fileNode, f.line, f.line, cu, sig, nodes.intern("!{}"))
}

var dbgDefineSymRe = regexp.MustCompile(`@([A-Za-z0-9_.$]+)\(`)

// dbgDefineSymbol extracts the symbol from a `define` line.
func dbgDefineSymbol(line string) string {
	if m := dbgDefineSymRe.FindStringSubmatch(line); m != nil {
		return m[1]
	}
	return ""
}

// dbgDefineSignature returns the return type and the parameter types of a define
// line, taken from the IR text that says them.
func dbgDefineSignature(line string) (string, []string) {
	open := strings.Index(line, "(")
	closing := strings.LastIndex(line, ")")
	if open < 0 || closing < open {
		return "", nil
	}
	ret := strings.TrimSpace(line[len("define "):open])
	for _, link := range []string{"internal ", "private ", "weak ", "common "} {
		ret = strings.TrimSpace(strings.TrimPrefix(ret, link))
	}
	// "double @gy_f" — the name is not part of the return type.
	if i := strings.LastIndex(ret, "@"); i > 0 {
		ret = strings.TrimSpace(ret[:i])
	}
	var params []string
	inner := strings.TrimSpace(line[open+1 : closing])
	if inner == "" || inner == "..." {
		return ret, params
	}
	for _, part := range strings.Split(inner, ",") {
		t := strings.TrimSpace(part)
		if t == "..." {
			continue
		}
		// "i32 %x", "i8* %p", "%obj %o" — the type is everything before the name.
		if i := strings.LastIndexAny(t, " \t"); i > 0 {
			t = strings.TrimSpace(t[:i])
		}
		params = append(params, strings.TrimSpace(t))
	}
	return ret, params
}

// dbgAttachDefineLocation puts `!dbg !N` on the define line, before the brace.
func dbgAttachDefineLocation(line string, subp int) string {
	trimmed := strings.TrimRight(line, " \t")
	if !strings.HasSuffix(trimmed, "{") {
		return line
	}
	head := strings.TrimRight(strings.TrimSuffix(trimmed, "{"), " \t")
	return fmt.Sprintf("%s !dbg !%d {", head, subp)
}

// dbgAppendLocation appends `, !dbg !N` to an instruction line, keeping a trailing
// comment last (metadata inside a comment is metadata that does not exist).
func dbgAppendLocation(line string, id int) string {
	code, comment := dbgSplitComment(strings.TrimRight(line, " \t"))
	out := fmt.Sprintf("%s, !dbg !%d", strings.TrimRight(code, " \t"), id)
	if comment != "" {
		out += " " + comment
	}
	return out
}

// dbgActive is the source position of an emitted line: the last mark at or before
// it, never one from before the function's own definition, and the definition's
// position when the function has no marks of its own yet.
func dbgActive(marks []dbgMark, lineStart int, fn *dbgFunc) (int, int) {
	idx := sort.Search(len(marks), func(i int) bool { return marks[i].off > lineStart }) - 1
	for idx >= 0 {
		m := marks[idx]
		if m.off < fn.off {
			break // never inherit a position from outside this function
		}
		if m.line > 0 {
			return m.line, m.col
		}
		idx--
	}
	return fn.line, fn.col
}

// patchModuleFlags adds flag entries to the module's `!llvm.module.flags` line.
func patchModuleFlags(lines []string, extra []int) {
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "!llvm.module.flags = !{") {
			continue
		}
		open := strings.Index(t, "{")
		end := strings.LastIndex(t, "}")
		if open < 0 || end < open {
			return
		}
		inner := strings.TrimSpace(t[open+1 : end])
		for _, id := range extra {
			if inner != "" {
				inner += ", "
			}
			inner += fmt.Sprintf("!%d", id)
		}
		lines[i] = "!llvm.module.flags = !{" + inner + "}"
		return
	}
}

// --- reading the line table back out of the artifact -----------------------------

var dwarfRowRe = regexp.MustCompile(`^0x[0-9a-fA-F]+\s+(\d+)`)
var dwarfFileRe = regexp.MustCompile(`^\s*name:\s*"(.*)"\s*$`)

// DwarfLineTable runs `llvm-dwarfdump --debug-line` over an object file or binary
// and reports what it found. A missing toolchain is `Skipped`, never `OK` — the
// rule ADR 0164 set for verification, applied here so a build cannot claim a line
// table that nobody read.
func DwarfLineTable(path string) (*DWARFReport, error) {
	rep := &DWARFReport{Tool: dwarfdumpCmd, Toolchain: "LLVM " + PinnedLLVMVersion}
	tool := availableTool(dwarfdumpCmd)
	if tool == "" {
		rep.Skipped = true
		rep.Note = fmt.Sprintf("%s not found; the line table was not read from the artifact", dwarfdumpCmd)
		return rep, nil
	}
	rep.Tool = tool
	out, err := runToolStage(ToolBudget, "llvm-dwarfdump", tool, "--debug-line", path)
	rep.Ran = true
	if err != nil {
		rep.Note = strings.TrimSpace(string(out))
		if rep.Note == "" {
			rep.Note = err.Error()
		}
		if len(rep.Note) > 400 {
			rep.Note = rep.Note[:400]
		}
		return rep, nil
	}
	text := string(out)
	seen := map[int]bool{}
	for _, ln := range strings.Split(text, "\n") {
		if m := dwarfFileRe.FindStringSubmatch(ln); m != nil {
			if name := m[1]; name != "" && !dbgHasString(rep.Files, name) {
				rep.Files = append(rep.Files, name)
			}
			continue
		}
		if m := dwarfRowRe.FindStringSubmatch(strings.TrimSpace(ln)); m != nil {
			n, cerr := strconv.Atoi(m[1])
			if cerr != nil {
				continue
			}
			rep.LineRows++
			// A row with no line is the compiler's own runtime code, which has no source
			// to name; it is counted but never reported as a line the program has.
			if n > 0 && !seen[n] {
				seen[n] = true
				rep.SourceLines = append(rep.SourceLines, n)
			}
		}
	}
	sort.Ints(rep.SourceLines)
	if strings.Contains(text, "No debug info") {
		rep.Note = "the artifact carries no debug info at all"
		return rep, nil
	}
	if rep.LineRows == 0 {
		rep.Note = "the artifact carries no .debug_line rows (the module had no !dbg records, or a pass dropped them)"
		return rep, nil
	}
	rep.OK = true
	return rep, nil
}

func dbgHasString(hay []string, needle string) bool {
	for _, s := range hay {
		if s == needle {
			return true
		}
	}
	return false
}

// CompileDebug compiles source text to a module carrying DWARF line records — the
// artifact a `--build --debug` would link, so `--emit-llvm --debug` shows what a
// debugger will see (L8.5, ADR 0231).
func CompileDebug(src string, optLevel int) (string, error) {
	prog, err := parseProgram(src)
	if err != nil {
		return "", err
	}
	Analyze(prog)
	return GenerateIRWithOptions(prog, &IRGenOptions{Debug: &DebugOptions{OptLevel: optLevel}})
}

// DebugInfoForSource compiles a source string and reports the debug info its
// module carries — `gustyc --debug-info`. It returns the debug module as well, so
// a caller can hand over both the artifact and the account of it.
func DebugInfoForSource(src string, opts *DebugOptions) (*DebugInfo, string, error) {
	prog, err := parseProgram(src)
	if err != nil {
		return nil, "", err
	}
	Analyze(prog)
	ir, dbg, err := GenerateIRReport(prog, &IRGenOptions{Debug: opts})
	if err != nil {
		return nil, ir, err
	}
	return dbg, ir, nil
}
