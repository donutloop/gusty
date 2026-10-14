package lang

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The two guards that make docs/operations.md's account of refusals a claim rather than a hope.
//
// A refusal is a user-visible interface: it is the sentence a compiler writes where it would have
// written an answer. Docs that describe those sentences drift in two directions, and each has a
// different failure. A row that outlives the sentence it documents teaches an agent to branch on a
// string that will never arrive — the agent's fallback quietly becomes the only path. A registry that
// grows without the document keeps the human reader and the machine reader looking at different
// inventories of the same language, which is the drift AGENTS.md's human/machine contract exists to
// prevent. The first guard reads the Go sources; the second reads the generated table.

// TestTheDocumentedCapabilitySentencesStillExist reads every "Message substring" cell of the
// capability table in docs/operations.md and asks whether the backend still emits that sentence —
// by looking for it in this package's own (non-test) sources, where every refusal is written as a
// Go string literal.
//
// Placeholders are handled rather than allowed: a cell may quote `operator "<op>" on a string`, and
// the `<op>`/`%s`/`%q` holes are cut out, with each remaining fragment long enough to be a sentence
// asked of the sources. A fragment shorter than that is a cell that documents a single word, which no
// amount of grepping can certify.
func TestTheDocumentedCapabilitySentencesStillExist(t *testing.T) {
	t.Parallel()
	body := readOperationsDoc(t)
	section := markdownSection(t, body, "## Codegen capability messages")
	goSources := readPackageSourcesWithoutTests(t)

	rows := markdownRows(section)
	if len(rows) < 5 {
		t.Fatalf("the capability table has %d rows; it did not parse the way the guard expects", len(rows))
	}
	// The first row is the header ("Message substring | Meaning | Workaround"), which documents no
	// sentence; the separator row was already dropped by markdownRows.
	rows = rows[1:]
	checked := 0
	for _, row := range rows {
		cell := markdownCell(row, 0)
		substr := strings.Trim(strings.TrimSpace(cell), "`")
		if substr == "" || strings.HasPrefix(substr, "---") {
			continue
		}
		for _, frag := range capabilityFragments(substr) {
			if !strings.Contains(goSources, frag) {
				t.Fatalf("docs/operations.md tells an agent to match %q, which is not a sentence this package emits (fragment %q found nowhere); fix the table or the door that emits it",
					substr, frag)
			}
			checked++
		}
	}
	if checked < len(rows) {
		t.Fatalf("the table has %d rows but only %d fragments were checked; a cell stopped being readable", len(rows), checked)
	}
}

// TestTheCapabilityDocsTableIsTheRegistry is the generated half: the code inventory an agent reads in
// the docs is the registry's own text, byte for byte. Adding a code without adding the doc row, or
// editing a doc row without the registry, fails here — and the failure names the file to edit.
func TestTheCapabilityDocsTableIsTheRegistry(t *testing.T) {
	t.Parallel()
	body := readOperationsDoc(t)
	want := CapabilityDocsTable()
	if !strings.Contains(body, want) {
		t.Fatalf("docs/operations.md's capability code table is not the registry.\n"+
			"The registry is the text in pkg/lang/capability.go; regenerate the table with:\n\n"+
			"\tgo run ./tools/captable\n\n(first 200 bytes of what it should say):\n%s", want[:min(200, len(want))])
	}
	// And the schema, which is the same list read a third way.
	for _, code := range CapabilityRefusalCodes() {
		if !strings.Contains(body, "`"+code+"`") {
			t.Fatalf("%s is in the registry and in the schema but not in docs/operations.md", code)
		}
	}
}

// capabilityFragments cuts a documented message substring at its placeholders — `<op>`, `%s`, `%q`,
// `%d`, `<name>` — and returns the pieces worth grepping.
func capabilityFragments(s string) []string {
	parts := regexp.MustCompile(`(<[a-zA-Z/%]+>|%[sdq]|…|\.\.\.)`).Split(s, -1)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.Trim(p, "`\""))
		if len(p) < 12 {
			continue
		}
		out = append(out, p)
	}
	return out
}

func readOperationsDoc(t *testing.T) string {
	t.Helper()
	for _, dir := range []string{"..", "../.."} {
		p := filepath.Join(dir, "docs", "operations.md")
		if b, err := os.ReadFile(p); err == nil {
			return string(b)
		}
	}
	t.Fatal("docs/operations.md is not readable from this test (it guards the CLI's contract, so it must be)")
	return ""
}

func markdownSection(t *testing.T, body, heading string) string {
	t.Helper()
	i := strings.Index(body, "\n"+heading)
	if i < 0 {
		t.Fatalf("docs/operations.md has no section %q", heading)
	}
	rest := body[i+len("\n"+heading):]
	for _, stop := range []string{"\n## ", "\n### "} {
		if j := strings.Index(rest, stop); j >= 0 {
			rest = rest[:j]
		}
	}
	return rest
}

var markdownRowRE = regexp.MustCompile("(?m)^\\|.*\\|\\s*$")

func markdownRows(section string) [][]string {
	var rows [][]string
	for _, line := range markdownRowRE.FindAllString(section, -1) {
		cells := strings.Split(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "|"), "|"), "|")
		row := make([]string, 0, len(cells))
		for _, c := range cells {
			row = append(row, strings.TrimSpace(c))
		}
		if len(row) > 0 && strings.HasPrefix(row[0], "---") {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func markdownCell(row []string, i int) string {
	if i >= len(row) {
		return ""
	}
	return row[i]
}

// readPackageSourcesWithoutTests concatenates every Go string literal in this package's non-test
// sources, in file order. Adjacent literals end up adjacent, which is what makes a sentence written
// as one fmt.Errorf across four lines findable as one string.
func readPackageSourcesWithoutTests(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	var b strings.Builder
	lits := regexp.MustCompile(`"([^"\\\n]|\\.)*"`)
	n := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for _, lit := range lits.FindAllString(string(src), -1) {
			b.WriteString(lit[1 : len(lit)-1])
			b.WriteByte(' ')
		}
		n++
	}
	if n < 40 {
		t.Fatalf("only %d sources read; the guard would certify a table it never looked at", n)
	}
	return b.String()
}
