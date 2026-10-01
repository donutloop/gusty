package lang

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// L8.5: the module carries the line table, and the account of it is read back from
// the module rather than from what the emitter hoped for (ADR 0231).

const debugProgSrc = `def total(limit):
    s = 0
    for i in range(1, limit):
        s = s + i
    return s

class Counter:
    def bump(self, k):
        self.n = self.n + k
        return self.n

c = Counter()
print(c.bump(2))
print(total(4))
`

func debugModuleOf(t *testing.T, src string, opts *DebugOptions) (string, *DebugInfo) {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	Analyze(prog)
	ir, dbg, err := GenerateIRReport(prog, &IRGenOptions{Debug: opts})
	if err != nil {
		t.Fatalf("codegen: %v", err)
	}
	return ir, dbg
}

func TestDebugInfoRecordsProgramFunctions(t *testing.T) {
	ir, dbg := debugModuleOf(t, debugProgSrc, &DebugOptions{File: "debug_prog.gy"})
	if dbg == nil {
		t.Fatal("debug requested but no DebugInfo returned")
	}
	if !strings.Contains(ir, "!llvm.dbg.cu = !{!") {
		t.Fatalf("module carries no compile unit:\n%s", tailOf(ir, 40))
	}
	if !strings.Contains(ir, "!{i32 2, !\"Debug Info Version\", i32 3}") {
		t.Fatal("module flags do not declare the debug-info version: LLVM would drop the metadata")
	}
	if !strings.Contains(ir, "!{i32 7, !\"Dwarf Version\", i32 5}") {
		t.Fatal("module flags do not declare the DWARF version")
	}
	if !strings.Contains(ir, "language: "+DebugDWARFLanguage) {
		t.Fatalf("compile unit does not claim %s:\n%s", DebugDWARFLanguage, tailOf(ir, 40))
	}
	if !strings.Contains(ir, "!DIFile(filename: \"debug_prog.gy\"") {
		t.Fatal("DIFile does not name the source file")
	}
	// Every program function has a subprogram naming the source line it is written on.
	// The names are what the program wrote; the lines are the `def` each sits on,
	// counted in the source itself — a table that is one off is as useless as none.
	want := map[string]int{"total": 1, "Counter.bump": 8, "main": 12}
	got := map[string]int{}
	for _, f := range dbg.Functions {
		got[f.Name] = f.Line
	}
	for name, line := range want {
		if got[name] != line {
			t.Errorf("function %q: DISubprogram line %d, want %d (all: %v)", name, got[name], line, got)
		}
	}
	if dbg.Subprograms < len(want) {
		t.Errorf("subprograms = %d, want at least %d", dbg.Subprograms, len(want))
	}
	if dbg.Instructions == 0 || dbg.Tagged != dbg.Instructions {
		t.Errorf("every instruction of a program function should carry a location: tagged %d of %d", dbg.Tagged, dbg.Instructions)
	}
	if dbg.Locations == 0 {
		t.Error("no DILocation nodes were emitted")
	}
	// The pass must never touch the compiler's own runtime blocks: an instruction with a
	// !dbg whose scope is not its function's subprogram is a verifier error.
	if bad := runtimeFunctionWithLocation(ir); bad != "" {
		t.Errorf("runtime function %s carries a !dbg record (it is not source)", bad)
	}
	for _, f := range dbg.Functions {
		if f.Tagged == 0 {
			t.Errorf("function %s (%s) has no tagged instructions", f.Name, f.Symbol)
		}
	}
}

// TestDebugRecordsAreOptIn pins that a plain build is exactly the build it was
// before: no metadata, no !dbg, the same bytes.
func TestDebugRecordsAreOptIn(t *testing.T) {
	plain, err := func() (string, error) {
		prog, perr := Parse(debugProgSrc)
		if perr != nil {
			return "", perr
		}
		Analyze(prog)
		return GenerateIR(prog)
	}()
	if err != nil {
		t.Fatalf("codegen: %v", err)
	}
	if strings.Contains(plain, "!dbg") || strings.Contains(plain, "!llvm.dbg.cu") {
		t.Fatal("a build that did not ask for debug info carries debug info")
	}
}

// TestDebugPassKeepsLineNumbers is why one source map can describe both builds:
// attaching a record appends to a line and never adds one, so IR line N means the
// same thing in a debug module and a debug-free one.
func TestDebugPassKeepsLineNumbers(t *testing.T) {
	prog, err := Parse(debugProgSrc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	Analyze(prog)
	plain, dbged, err := GenerateIRReport(prog, nil)
	if err != nil {
		t.Fatalf("codegen (plain): %v", err)
	}
	if dbged != nil {
		t.Fatal("GenerateIRReport reported debug info that was not asked for")
	}
	withDbg, dbg, err := GenerateIRReport(prog, &IRGenOptions{Debug: &DebugOptions{File: "p.gy"}})
	if err != nil {
		t.Fatalf("codegen (debug): %v", err)
	}
	plainLines := strings.Split(plain, "\n")
	debugLines := strings.Split(withDbg, "\n")
	// The debug module is the plain one plus the metadata tail.
	if len(debugLines) <= len(plainLines) {
		t.Fatalf("expected a metadata tail: %d lines vs %d", len(debugLines), len(plainLines))
	}
	for i, ln := range plainLines {
		if i >= len(debugLines) {
			t.Fatalf("debug module is shorter at line %d", i+1)
		}
		got := debugLines[i]
		if strings.HasPrefix(ln, "!llvm.module.flags") {
			// The flags line is extended with the DWARF/debug-info versions by design.
			if !strings.HasPrefix(got, ln[:len("!llvm.module.flags = !{!0")]) {
				t.Errorf("flags line rewritten: %q -> %q", ln, got)
			}
			continue
		}
		if ln == "}" || strings.HasPrefix(ln, "define ") {
			// Both are legal attachments; the line must still begin the same way.
			if !strings.HasPrefix(got, strings.TrimSuffix(ln, "{")) {
				t.Errorf("line %d changed shape: %q -> %q", i+1, ln, got)
			}
			continue
		}
		if !strings.HasPrefix(got, ln) {
			t.Errorf("line %d changed, not just extended:\n plain: %q\n  dbg: %q", i+1, ln, got)
		}
	}
	if dbg == nil || dbg.Instructions == 0 {
		t.Fatal("no instructions tagged")
	}
}

// TestLineRowsNameTheStatementTheyCameFrom is the useful claim: a debugger asked
// about the print on line 15 has machine code that says line 15.
func TestLineRowsNameTheStatementTheyCameFrom(t *testing.T) {
	ir, dbg := debugModuleOf(t, debugProgSrc, &DebugOptions{File: "p.gy"})
	_ = ir
	wantLines := map[int]bool{1: false, 2: false, 3: false, 4: false, 5: false, 8: false, 9: false, 10: false, 12: false, 13: false, 14: false}
	for _, r := range dbg.Lines {
		if _, ok := wantLines[r.Line]; ok {
			wantLines[r.Line] = true
		}
	}
	for line, seen := range wantLines {
		if !seen {
			t.Errorf("no emitted IR line is attributed to source line %d", line)
		}
	}
	for _, r := range dbg.Lines {
		if r.Line <= 0 || r.IRLine <= 0 {
			t.Fatalf("row %v names a line that does not exist", r)
		}
		if r.Function == "" {
			t.Errorf("row for IR line %d names no function", r.IRLine)
		}
	}
}

// TestEveryProgramDefineCarriesASubprogram is the completeness check: a new
// function-emitting path that forgets to register shows up here as a function a
// debugger cannot name, rather than quietly producing no records for it. The
// exempt set is derived from the runtime blocks themselves, not from a list.
func TestEveryProgramDefineCarriesASubprogram(t *testing.T) {
	_, dbg := debugModuleOf(t, debugProgSrc, &DebugOptions{File: "p.gy"})
	ir, _ := debugModuleOf(t, debugProgSrc, &DebugOptions{File: "p.gy"})
	registered := map[string]bool{}
	for _, f := range dbg.Functions {
		registered[f.Symbol] = true
	}
	for _, sym := range programDefineSymbols(ir) {
		if !registered[sym] {
			t.Errorf("define @%s carries no DISubprogram: an emitting path did not call dbgDefine", sym)
		}
	}
}

func TestDebugInfoJSONShape(t *testing.T) {
	_, dbg := debugModuleOf(t, "print(1)\nx = 2\nprint(x)\n", &DebugOptions{File: "shape.gy", Directory: "/tmp", OptLevel: 2})
	if dbg.SchemaVersion != DebugSchemaVersion {
		t.Errorf("schema_version = %d, want %d", dbg.SchemaVersion, DebugSchemaVersion)
	}
	if !dbg.IsOptimized {
		t.Error("OptLevel 2 must set DW_AT_optimized (a debugger may then say 'optimized out')")
	}
	b, err := json.Marshal(dbg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back DebugInfo
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.File != dbg.File || back.Tagged != dbg.Tagged || len(back.Functions) != len(dbg.Functions) {
		t.Fatalf("round trip lost data:\n%s", b)
	}
	for _, key := range []string{"schema_version", "compile_unit", "emission_kind", "subprograms", "instructions", "tagged", "locations", "lines", "is_optimized"} {
		if !strings.Contains(string(b), "\""+key+"\"") {
			t.Errorf("document has no %q member: %s", key, b)
		}
	}
}

// TestUnknownStatementsInheritAPosition pins the fallback: a synthesized body with
// no span of its own is attributed to the enclosing definition, never to line 0,
// which DWARF has no meaning for.
func TestUnknownStatementsInheritAPosition(t *testing.T) {
	_, dbg := debugModuleOf(t, "def f(n):\n    total = 0\n    for i in range(0, n):\n        total = total + i\n    return total\nprint(f(4))\n", &DebugOptions{File: "loop.gy"})
	for _, r := range dbg.Lines {
		if r.Line < 1 {
			t.Fatalf("row %v claims source line %d", r, r.Line)
		}
	}
	if dbg.Tagged == 0 {
		t.Fatal("nothing tagged")
	}
}

func TestAttachWithNoMarksFallsBackToTheDefinition(t *testing.T) {
	marks := []dbgMark{{off: 0, line: 3, col: 1}}
	funcs := []dbgFunc{{sym: "gy_f", name: "f", off: 0, line: 3, col: 1}}
	module := "define i32 @gy_f(i32 %p1) {\nentry:\n  %t1 = add i32 %p1, 1\n  ret i32 %t1\n}\n!llvm.module.flags = !{!0}\n!0 = !{i32 2, !\"PIC Level\", i32 2}\n"
	out, art := attachDebugInfo(module, marks, funcs, 0, 0, &DebugOptions{File: "x.gy"})
	info := readBackDebugInfo(out, art)
	if !strings.Contains(out, "!dbg !") {
		t.Fatalf("nothing attached:\n%s", out)
	}
	for _, r := range info.Lines {
		if r.Line != 3 {
			t.Fatalf("row %v did not fall back to the definition's line", r)
		}
	}
}

// TestAttachLeavesRuntimeFunctionsAlone pins the verifier's rule: a !dbg inside a
// function with no subprogram is an invalid module.
func TestAttachLeavesRuntimeFunctionsAlone(t *testing.T) {
	module := "define i32 @gy_f() {\nentry:\n  %t1 = call i32 @rt_thing(i32 1)\n  ret i32 %t1\n}\ndefine internal i32 @rt_thing(i32 %h) {\nentry:\n  ret i32 %h\n}\n!llvm.module.flags = !{!0}\n!0 = !{i32 2, !\"PIC Level\", i32 2}\n"
	out, art := attachDebugInfo(module, nil, []dbgFunc{{sym: "gy_f", name: "f", off: 0, line: 2, col: 1}}, 0, 0, &DebugOptions{File: "x.gy"})
	info := readBackDebugInfo(out, art)
	if strings.Contains(strings.Split(out, "\n")[6], "!dbg") {
		t.Fatalf("the runtime function was tagged:\n%s", out)
	}
	if len(info.Functions) != 1 || info.Functions[0].Symbol != "gy_f" {
		t.Fatalf("runtime function was registered: %+v", info.Functions)
	}
}

func TestDwarfReportWithoutAToolchainIsSkippedNotOk(t *testing.T) {
	saved := dwarfdumpCmd
	dwarfdumpCmd = "llvm-dwarfdump-that-does-not-exist"
	defer func() { dwarfdumpCmd = saved }()
	rep, err := DwarfLineTable("nope.o")
	if err != nil {
		t.Fatalf("a missing toolchain is not an error: %v", err)
	}
	if !rep.Skipped || rep.OK || rep.Ran {
		t.Fatalf("skipped/not-run is the only honest answer: %+v", rep)
	}
	if !strings.Contains(rep.Note, "not found") {
		t.Errorf("skipped must say why: %q", rep.Note)
	}
}

func TestDebugInstructionGate(t *testing.T) {
	yes := []string{
		"  store i32 0, i32* %_x",
		"  ret i32 0",
		"  br label %l2",
		"  %t1 = add i32 %a, 1",
		"  call void @rt_gc([4096 x i32*]* @gc.roots, i32 0)",
		"  %t2 = phi i32 [ %a, %l1 ], [ %b, %l2 ]",
		"  %_x = alloca i32",
		"  %t3 = call i32 (i8*, ...) @printf(i8* getelementptr inbounds ([3 x i8], [3 x i8]* @.fmt1, i32 0, i32 0)) ; prints",
	}
	no := []string{
		"entry:",
		"l3:",
		"}",
		"  ; a comment",
		"",
		"  ",
		"  i32 1, label %a", // a switch arm: not an instruction
		"@.str1 = private unnamed_addr constant [3 x i8] c\"a\\00\"",
		"  %t = phi i32 [ %a, %l1 ], [ %b, %l2 ] }",         // ends with a brace: not a line to extend
		"  !5 = !DILocation(line: 1, column: 1, scope: !4)", // metadata, not an instruction
	}
	for _, ln := range yes {
		if !dbgIsInstruction(ln) {
			t.Errorf("should be tagged: %q", ln)
		}
	}
	for _, ln := range no {
		if dbgIsInstruction(ln) {
			t.Errorf("should not be tagged: %q", ln)
		}
	}
}

func TestDebugAppendLocationKeepsCommentsLast(t *testing.T) {
	got := dbgAppendLocation("  store i32 0, i32* %_x ; the reset", 7)
	if got != "  store i32 0, i32* %_x, !dbg !7 ; the reset" {
		t.Fatalf("got %q", got)
	}
	if strings.Contains(dbgAppendLocation("  %t = call i32 @f(i8* getelementptr inbounds ([4 x i8], [4 x i8]* @.fmt, i32 0, i32 0))", 3), ";") {
		t.Fatal("invented a comment")
	}
}

func TestDebugNamesCannotBreakTheirLiteral(t *testing.T) {
	if got := dbgMDText(`a"b` + "\n" + `c`); got != `"abc"` {
		t.Fatalf("got %q", got)
	}
}

func TestDebugSignatureIsReadFromTheDefine(t *testing.T) {
	ret, params := dbgDefineSignature("define double @gy_f(i32 %p1, i8* %p2) {")
	if ret != "double" {
		t.Errorf("ret = %q", ret)
	}
	if len(params) != 2 || params[0] != "i32" || params[1] != "i8*" {
		t.Errorf("params = %v", params)
	}
	ret, params = dbgDefineSignature("define internal void @f_apply() {")
	if ret != "void" || len(params) != 0 {
		t.Errorf("void thunk read as %q / %v", ret, params)
	}
}

// --- helpers ------------------------------------------------------------------

// programDefineSymbols lists the defines of a module that are not the compiler's
// own runtime blocks, taken from the runtime block text itself so the exempt set
// cannot drift from the blocks.
func programDefineSymbols(ir string) []string {
	out := []string{}
	blocked := map[string]bool{}
	for _, block := range runtimeIRBlocks() {
		for _, sym := range defineSymbols(block) {
			blocked[sym] = true
		}
	}
	for _, sym := range defineSymbols(ir) {
		if !blocked[sym] && sym != "main" {
			out = append(out, sym)
		}
	}
	return out
}

func defineSymbols(text string) []string {
	out := []string{}
	for _, ln := range strings.Split(text, "\n") {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "define ") {
			continue
		}
		if sym := dbgDefineSymbol(t); sym != "" {
			out = append(out, sym)
		}
	}
	return out
}

// runtimeIRBlocks returns every block of compiler-emitted runtime code. A define in
// one of these is not program source and must not carry a !dbg record.
func runtimeIRBlocks() []string {
	return []string{
		raiseRuntimeIR, floatRuntimeIR, heapRuntimeIR,
		rootRuntimeIR, rootGlobalsIR,
	}
}

func runtimeFunctionWithLocation(ir string) string {
	blocked := map[string]bool{}
	for _, block := range runtimeIRBlocks() {
		for _, sym := range defineSymbols(block) {
			blocked[sym] = true
		}
	}
	cur := ""
	for _, ln := range strings.Split(ir, "\n") {
		t := strings.TrimSpace(ln)
		if strings.HasPrefix(t, "define ") {
			cur = dbgDefineSymbol(t)
			continue
		}
		if t == "}" {
			cur = ""
			continue
		}
		if cur != "" && blocked[cur] && strings.Contains(t, "!dbg !") {
			return cur
		}
	}
	return ""
}

func tailOf(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

func TestDebugLocationIsInternedOncePerPosition(t *testing.T) {
	ir, dbg := debugModuleOf(t, debugProgSrc, &DebugOptions{File: "intern.gy"})
	// One source position, one metadata node. The line table may point at it from many
	// instructions; the same position spelled as two nodes is how a module grows without
	// bound and a debugger stops being able to collapse two rows into one statement.
	nodes := map[string]int{}
	for _, ln := range strings.Split(ir, "\n") {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "!") || !strings.Contains(t, "= !DILocation(") {
			continue
		}
		nodes[strings.TrimSpace(strings.SplitN(t, " = ", 2)[1])]++
	}
	if len(nodes) == 0 {
		t.Fatal("no DILocation nodes at all")
	}
	for text, n := range nodes {
		if n != 1 {
			t.Errorf("location %s written %d times", text, n)
		}
	}
	if len(nodes) != dbg.Locations {
		t.Errorf("%d location nodes in the module, report says %d", len(nodes), dbg.Locations)
	}
	// And the interning is actually used: a loop body tags many instructions with fewer
	// nodes than that, because they were written for the same statement.
	if dbg.Tagged < dbg.Locations {
		t.Errorf("tagged %d instructions with %d locations: the table is not reusing positions", dbg.Tagged, dbg.Locations)
	}
}

// The textual pass is what runs when there is no LLVM `opt`; it used to rebuild the
// module from parsed globals and defines, which silently discarded the whole metadata
// block — the line table included. Debug info has to survive it.
func TestTextualOptimizerKeepsTheLineTable(t *testing.T) {
	ir, before := debugModuleOf(t, debugProgSrc, &DebugOptions{File: "keep.gy"})
	if before.Tagged == 0 {
		t.Fatal("no instructions tagged, nothing to test")
	}
	saved := optCmd
	optCmd = "" // no LLVM opt: the build falls back to the textual pass
	defer func() { optCmd = saved }()

	out, rep := OptimizeIRReport(ir, 2)
	if rep != nil && rep.Applied {
		t.Fatal("the LLVM opt ran; the textual pass was not exercised")
	}
	for _, want := range []string{"!llvm.dbg.cu", "DICompileUnit", "DISubprogram", "DILocation", "!llvm.module.flags"} {
		if !strings.Contains(out, want) {
			t.Errorf("the textual pass dropped %s", want)
		}
	}
	if strings.Count(out, "!DISubprogram") != len(before.Functions) {
		t.Errorf("subprograms %d, want %d", strings.Count(out, "!DISubprogram"), len(before.Functions))
	}
	// Read the optimized module back with no artifact at all: everything the report says
	// must come out of the module text.
	after := readBackDebugInfo(out, nil)
	if after.Tagged == 0 {
		t.Error("no instruction carries a location after the textual pass")
	}
	if after.Subprograms != len(before.Functions) {
		t.Errorf("read back %d subprograms, want %d", after.Subprograms, len(before.Functions))
	}
	if after.SchemaVersion != DebugSchemaVersion || after.File != "keep.gy" {
		t.Errorf("read-back account lost its header: %+v", after)
	}
}

// A module with no debug info must not grow any through the textual pass either: the
// metadata block is carried through because it was there, not invented.
func TestTextualOptimizerInventsNoDebugInfo(t *testing.T) {
	_, _ = debugModuleOf(t, "print(1)\n", nil) // warm the generator, prove the opt-in path above
	prog, err := Parse("print(1)\n")
	if err != nil {
		t.Fatal(err)
	}
	Analyze(prog)
	plain, err := GenerateIR(prog)
	if err != nil {
		t.Fatal(err)
	}
	saved := optCmd
	optCmd = ""
	defer func() { optCmd = saved }()
	out := OptimizeIR(plain, 2)
	if strings.Contains(out, "!dbg") || strings.Contains(out, "!DISubprogram") {
		t.Error("a module built without --debug grew debug info")
	}
}

// The schema is how an agent learns these documents exist. A field that is in the Go
// struct but not in the schema is a field nobody can consume, so the two are checked
// against each other rather than kept in line by memory.
func TestSchemaDescribesEveryDebugField(t *testing.T) {
	var doc struct {
		Definitions map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
			Required   []string                   `json:"required"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(ASTIRSchema), &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	for name, sample := range map[string]any{"debugInfo": DebugInfo{}, "dwarfReport": DWARFReport{}} {
		def, ok := doc.Definitions[name]
		if !ok {
			t.Fatalf("schema has no %q document", name)
		}
		for _, tag := range jsonFieldNames(sample) {
			if _, ok := def.Properties[tag]; !ok {
				t.Errorf("%s: field %q is not in the schema", name, tag)
			}
		}
		for _, req := range def.Required {
			if _, ok := def.Properties[req]; !ok {
				t.Errorf("%s: required %q has no property", name, req)
			}
		}
	}
}

// jsonFieldNames lists the json tag of every exported field, minus any omitempty one.
func jsonFieldNames(v any) []string {
	out := []string{}
	t := reflect.TypeOf(v)
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if strings.Contains(tag, "omitempty") {
			continue
		}
		out = append(out, name)
	}
	return out
}
