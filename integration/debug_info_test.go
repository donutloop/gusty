package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// L8.5 (ADR 0231): the module carries the line table, and the toolchain reports what the
// artifact really holds. These cases drive the real CLI, so what is checked is what a
// human or an agent would see: the `--debug-info` document, the `--build --debug` report,
// and, when the LLVM toolchain is present, the `.debug_line` table of the linked binary
// read back with llvm-dwarfdump and resolved with addr2line.

// debugTable renders the line-table document as stable text: which functions are
// described, which source lines the compiled code can point at. Deliberately *not* the
// emitted-IR line numbers, which move whenever codegen moves; the claim worth a golden is
// that the compiler can name the source of the code it wrote.
func debugTable(t *testing.T, doc map[string]any) string {
	t.Helper()
	var b strings.Builder
	file, _ := doc["file"].(string)
	lang, _ := doc["language"].(string)
	b.WriteString("unit " + file + " " + lang + "\n")
	fns, _ := doc["functions"].([]any)
	rows := []string{}
	for _, f := range fns {
		m, _ := f.(map[string]any)
		name, _ := m["name"].(string)
		sym, _ := m["symbol"].(string)
		line := int(m["line"].(float64))
		tagged := int(m["instructions"].(float64))
		locs := int(m["locations"].(float64))
		if tagged == 0 {
			t.Errorf("function %q has no instruction with a location", name)
		}
		if locs == 0 {
			t.Errorf("function %q names no source position", name)
		}
		rows = append(rows, "func "+name+" sym "+sym+" line "+strconv.Itoa(line)+" positions "+strconv.Itoa(locs))
	}
	sort.Strings(rows)
	b.WriteString(strings.Join(rows, "\n"))
	covered := map[int]bool{}
	if lines, ok := doc["lines"].([]any); ok {
		for _, l := range lines {
			m, _ := l.(map[string]any)
			covered[int(m["line"].(float64))] = true
		}
	}
	list := []int{}
	for n := range covered {
		list = append(list, n)
	}
	sort.Ints(list)
	parts := []string{}
	for _, n := range list {
		parts = append(parts, strconv.Itoa(n))
	}
	b.WriteString("\nlines " + strings.Join(parts, ","))
	return b.String() + "\n"
}

func debugInfoDoc(t *testing.T, bin, src string) map[string]any {
	t.Helper()
	out, err := exec.Command(bin, "--debug-info", src, "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("gustyc --debug-info: %v\n%s", err, out)
	}
	var doc map[string]any
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("--debug-info --json is not the documented document: %v\n%s", err, out)
	}
	return doc
}

func TestDebugInfoGolden(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	src := readProgram(t, "debug_lines.gy")
	doc := debugInfoDoc(t, bin, src)
	if defect, ok := doc["defect"].(string); ok && defect != "" {
		t.Errorf("the module's metadata disagrees with the compiler that wrote it: %s", defect)
	}
	checkWant(t, "debug_lines.table", debugTable(t, doc))
}

// The build report is the same claim about a real artifact: a --debug build says what the
// object file's .debug_line actually holds. An absent toolchain is a skip, never a pass.
func TestBuildDebugReportsDWARF(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	src := writeSrc(t, dir, "debug_lines.gy", readProgram(t, "debug_lines.gy"))
	prog := filepath.Join(dir, "prog")
	out, err := exec.Command(bin, "--build", prog, src, "--debug", "--json").CombinedOutput()
	if err != nil {
		t.Fatalf("gustyc --build --debug: %v\n%s", err, out)
	}
	var doc struct {
		Debug *struct {
			Tagged       int    `json:"tagged"`
			Instructions int    `json:"instructions"`
			Subprograms  int    `json:"subprograms"`
			Defect       string `json:"defect"`
		} `json:"debug"`
		DWARF *struct {
			Skipped bool   `json:"skipped"`
			Ran     bool   `json:"ran"`
			OK      bool   `json:"ok"`
			Rows    int    `json:"line_rows"`
			Source  []int  `json:"source_lines"`
			Note    string `json:"note"`
		} `json:"dwarf"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatalf("--build --debug --json: %v\n%s", err, out)
	}
	if doc.Debug == nil {
		t.Fatal("--build --debug reported no debug info")
	}
	if doc.Debug.Defect != "" {
		t.Errorf("defect: %s", doc.Debug.Defect)
	}
	if doc.Debug.Tagged == 0 || doc.Debug.Tagged != doc.Debug.Instructions {
		t.Errorf("only %d/%d instructions in program functions carry a location", doc.Debug.Tagged, doc.Debug.Instructions)
	}
	if doc.DWARF == nil {
		t.Fatal("--build --debug did not report the artifact")
	}
	if doc.DWARF.Skipped {
		t.Skipf("no llvm-dwarfdump on this machine: %s", doc.DWARF.Note)
	}
	if !doc.DWARF.Ran || !doc.DWARF.OK || doc.DWARF.Rows == 0 {
		t.Fatalf("the artifact does not carry the table the compiler claimed: %+v", doc.DWARF)
	}
	// The lines the debugger can stop at must include every statement the program has.
	// A missing one is a statement the compiled code forgot where it came from.
	want := []int{1, 2, 3, 4, 6, 7, 8, 9, 12, 13, 14}
	have := map[int]bool{}
	for _, n := range doc.DWARF.Source {
		have[n] = true
	}
	for _, n := range want {
		if !have[n] {
			t.Errorf("the binary's .debug_line never names source line %d of the program: %v", n, doc.DWARF.Source)
		}
	}
	// The program still runs: debug info is metadata, not a different program.
	run := exec.Command(prog)
	got, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	checkWant(t, "debug_lines.out", string(got))
}

// The point of a line table is that a debugger resolves an address to a line. That is
// checked here the way a debugger does it: addr2line on the linked binary.
func TestDebugInfoResolvesAddresses(t *testing.T) {
	if _, err := exec.LookPath("llvm-addr2line-20"); err != nil {
		t.Skip("no llvm-addr2line")
	}
	if _, err := exec.LookPath("llvm-nm-20"); err != nil {
		t.Skip("no llvm-nm")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "gustyc")
	buildCLI(t, bin)
	src := writeSrc(t, dir, "debug_lines.gy", readProgram(t, "debug_lines.gy"))
	prog := filepath.Join(dir, "prog")
	if out, err := exec.Command(bin, "--build", prog, src, "--debug").CombinedOutput(); err != nil {
		t.Fatalf("gustyc --build --debug: %v\n%s", err, out)
	}
	syms, err := exec.Command("llvm-nm-20", prog).Output()
	if err != nil {
		t.Fatalf("llvm-nm: %v", err)
	}
	addr := ""
	for _, ln := range strings.Split(string(syms), "\n") {
		if strings.HasSuffix(ln, " gy_total") {
			addr = strings.Fields(ln)[0]
		}
	}
	if addr == "" {
		t.Fatal("gy_total is not in the binary")
	}
	res, err := exec.Command("llvm-addr2line-20", "-e", prog, "-f", addr).Output()
	if err != nil {
		t.Fatalf("llvm-addr2line: %v", err)
	}
	got := string(res)
	if !strings.Contains(got, "gy_total") || !strings.Contains(got, "debug_lines.gy:6") {
		t.Errorf("addr2line could not place gy_total on its source line:\n%s", got)
	}
	// And the object keeps the language name: a debugger reads DW_AT_language to decide
	// how to print frames, and gusty tells it the truth about what the language is.
	dump, err := exec.Command("llvm-dwarfdump-20", "--debug-info", prog).Output()
	if err != nil {
		t.Fatalf("llvm-dwarfdump: %v", err)
	}
	if !strings.Contains(string(dump), "DW_LANG_Python") {
		t.Errorf("the artifact does not name its language: DW_AT_language has no DW_LANG_Python")
	}
	_ = os.RemoveAll(dir)
}
