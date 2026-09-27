package lang

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return prog
}

// TestParseCacheFullMatchesParse verifies NewParseCache reproduces Parse's
// parse tree exactly.
func TestParseCacheFullMatchesParse(t *testing.T) {
	src := "x = 1\n\ny = x + 2\n\n# a comment\ndef f(a):\n    return a * 2\n\nz = f(3)\n"
	want := mustParse(t, src)
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatalf("NewParseCache: %v", err)
	}
	if !reflect.DeepEqual(c.Program(), want) {
		t.Fatalf("full parse mismatch:\n got %v\nwant %v", c.Program(), want)
	}
	if c.Reused() != 0 {
		t.Fatalf("expected Reused()==0 after full parse, got %d", c.Reused())
	}
}

// TestParseCacheUpdateEquivalentToParse applies an edit to statement 2 and
// checks the incremental result equals a full re-parse of the new source,
// and that unaffected statements keep their AST node identity.
func TestParseCacheUpdateEquivalentToParse(t *testing.T) {
	src := "a = 1\nb = 2\nc = 3\nd = 4\n"
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Program()

	// Replace "b = 2" with "b = 100" (edit spans line 2).
	edit := Edit{Start: Span{Line: 2, Col: 1}, End: Span{Line: 2, Col: 100}, NewText: "b = 100"}
	if err := c.Update([]Edit{edit}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	newSrc := c.Source()
	if want, got := "a = 1\nb = 100\nc = 3\nd = 4\n", newSrc; want != got {
		t.Fatalf("source mismatch:\n got %q\nwant %q", got, want)
	}
	want := mustParse(t, newSrc)
	if !reflect.DeepEqual(c.Program(), want) {
		t.Fatalf("incremental != full parse:\n got %v\nwant %v", c.Program(), want)
	}
	// statement 0 ("a = 1") is before the edit and must keep identity.
	if c.Program().Stmts[0] != old.Stmts[0] {
		t.Fatalf("unaffected statement 0 lost identity: got %v want %v", c.Program().Stmts[0], old.Stmts[0])
	}
	// statement 1 was edited and must be a freshly parsed node.
	if c.Program().Stmts[1] == old.Stmts[1] {
		t.Fatalf("affected statement 1 kept stale identity")
	}
	// "c = 3" and "d = 4" sit after a single in-line edit whose text is unchanged
	// (only shifted), so they are reused too: reuse is no longer prefix-only.
	if c.Reused() != 3 {
		t.Fatalf("expected 3 reused statements (prefix + tail), got %d", c.Reused())
	}
	if c.Program().Stmts[2] != old.Stmts[2] || c.Program().Stmts[3] != old.Stmts[3] {
		t.Fatalf("statements after an in-line edit lost identity")
	}
}

// TestParseCacheInsertBetweenStatements verifies an insertion between two
// statements re-parses the tail and keeps the leading statement identity.
func TestParseCacheInsertBetweenStatements(t *testing.T) {
	src := "a = 1\nb = 2\nc = 3\n"
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Program()

	// Insert "mid = 99" after line 1 (at the newline between a and b).
	edit := Edit{Start: Span{Line: 2, Col: 1}, End: Span{Line: 2, Col: 1}, NewText: "mid = 99\n"}
	if err := c.Update([]Edit{edit}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	want := mustParse(t, c.Source())
	if !reflect.DeepEqual(c.Program(), want) {
		t.Fatalf("insert incremental != full parse:\n got %v\nwant %v", c.Program(), want)
	}
	// "a = 1" is preserved; the new statement is present.
	if c.Program().Stmts[0] != old.Stmts[0] {
		t.Fatalf("leading statement lost identity")
	}
	if len(c.Program().Stmts) != 4 {
		t.Fatalf("expected 4 statements, got %d", len(c.Program().Stmts))
	}
}

// TestParseCacheDeleteStatement verifies deleting a statement re-parses the
// tail and the result equals a full parse.
func TestParseCacheDeleteStatement(t *testing.T) {
	src := "a = 1\nb = 2\nc = 3\n"
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatal(err)
	}
	// Delete line 2 ("b = 2") entirely.
	edit := Edit{Start: Span{Line: 2, Col: 1}, End: Span{Line: 3, Col: 1}, NewText: ""}
	if err := c.Update([]Edit{edit}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	want := mustParse(t, c.Source())
	if !reflect.DeepEqual(c.Program(), want) {
		t.Fatalf("delete incremental != full parse:\n got %v\nwant %v", c.Program(), want)
	}
	if len(c.Program().Stmts) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(c.Program().Stmts))
	}
}

// TestParseCacheAppend verifies appending a statement at the end of the
// document preserves all existing statements and parses the new tail.
func TestParseCacheAppend(t *testing.T) {
	src := "a = 1\nb = 2\n"
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Program()
	// Append at EOF (line 3, column 1).
	edit := Edit{Start: Span{Line: 3, Col: 1}, End: Span{Line: 3, Col: 1}, NewText: "c = 3\n"}
	if err := c.Update([]Edit{edit}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	want := mustParse(t, c.Source())
	if !reflect.DeepEqual(c.Program(), want) {
		t.Fatalf("append incremental != full parse:\n got %v\nwant %v", c.Program(), want)
	}
	if c.Program().Stmts[0] != old.Stmts[0] || c.Program().Stmts[1] != old.Stmts[1] {
		t.Fatalf("append lost existing statement identity")
	}
	if len(c.Program().Stmts) != 3 {
		t.Fatalf("expected 3 statements, got %d", len(c.Program().Stmts))
	}
}

// TestParseCacheEditInsideNestedBlock verifies an edit inside a nested block
// re-parses the containing top-level statement.
func TestParseCacheEditInsideNestedBlock(t *testing.T) {
	src := "def f(x):\n    return x + 1\n\ny = f(1)\n"
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatal(err)
	}
	// Edit the body of f (line 2): return x + 2.
	edit := Edit{Start: Span{Line: 2, Col: 1}, End: Span{Line: 2, Col: 100}, NewText: "    return x + 2"}
	if err := c.Update([]Edit{edit}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	want := mustParse(t, c.Source())
	if !reflect.DeepEqual(c.Program(), want) {
		t.Fatalf("nested edit != full parse:\n got %v\nwant %v", c.Program(), want)
	}
}

// TestParseCacheWhitespaceOnlyEdit verifies a whitespace-only edit re-parses
// nothing new and keeps all statement identities.
func TestParseCacheWhitespaceOnlyEdit(t *testing.T) {
	src := "a = 1\nb = 2\n"
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatal(err)
	}
	old := c.Program()
	// Insert a trailing comment at EOF (line 3, col 1): no statement is
	// affected, so all statements keep identity.
	edit := Edit{Start: Span{Line: 3, Col: 1}, End: Span{Line: 3, Col: 1}, NewText: " # trailing\n"}
	if err := c.Update([]Edit{edit}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	want := mustParse(t, c.Source())
	if !reflect.DeepEqual(c.Program(), want) {
		t.Fatalf("whitespace edit != full parse:\n got %v\nwant %v", c.Program(), want)
	}
	if c.Program().Stmts[0] != old.Stmts[0] || c.Program().Stmts[1] != old.Stmts[1] {
		t.Fatalf("whitespace edit lost statement identity")
	}
}

// TestSpanToByte verifies line/col -> byte conversion for ASCII and UTF-8.
func TestSpanToByte(t *testing.T) {
	src := "a = 1\nb = 2\n"
	// "b = 2" starts at line 2, col 1 => byte offset 6.
	pos, err := spanToByte(src, Span{Line: 2, Col: 1})
	if err != nil || pos != 6 {
		t.Fatalf("spanToByte(line 2, col 1) = %d, %v; want 6", pos, err)
	}
	// UTF-8: "é" is 2 bytes.
	utf8src := "a = 1\né = 2\n"
	pos, err = spanToByte(utf8src, Span{Line: 2, Col: 2})
	if err != nil || pos != 8 {
		t.Fatalf("spanToByte(utf8 line2 col2) = %d, %v; want 8", pos, err)
	}
}

// L5.8 — a keystroke near the top of a file used to re-parse the whole document, because
// reuse was prefix-only. An in-line edit leaves the text after it byte-identical (only
// shifted), so the statements in that tail are reused by node identity and only the
// statements overlapping the edit are re-parsed.

func manyStatements(n int) string {
	var sb strings.Builder
	sb.WriteString("x = 1\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&sb, "def f%d(a):\n    return a + %d\n\n", i, i)
	}
	return sb.String()
}

func TestParseCacheReusesTailAfterLineEdit(t *testing.T) {
	src := manyStatements(20)
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatal(err)
	}
	before := c.Program()
	// Edit line 1 only, and make the replacement longer: everything below shifts.
	if err := c.Update([]Edit{{Start: Span{Line: 1, Col: 5}, End: Span{Line: 1, Col: 6}, NewText: "2 + 3"}}); err != nil {
		t.Fatal(err)
	}
	after := c.Program()
	if len(after.Stmts) != len(before.Stmts) {
		t.Fatalf("statement count changed: %d -> %d", len(before.Stmts), len(after.Stmts))
	}
	// Every statement after the edited first line must be the same node.
	for i := 1; i < len(before.Stmts); i++ {
		if after.Stmts[i] != before.Stmts[i] {
			t.Fatalf("statement %d lost identity after an in-line edit", i)
		}
	}
	if c.Reused() != len(before.Stmts)-1 {
		t.Fatalf("Reused() = %d, want %d", c.Reused(), len(before.Stmts)-1)
	}
	// The whole point: the result must equal a full parse of the new text.
	if want := mustParse(t, c.Source()); !reflect.DeepEqual(after, want) {
		t.Fatalf("incremental parse != full parse after tail reuse")
	}
}

// A reused node keeps its spans, so tail reuse is only sound while those spans stay true.
// An edit that inserts (or removes) a line moves everything below it, and the cache must
// fall back to re-parsing rather than hand back statements whose lines are stale.
func TestParseCacheTailReuseStopsWhenLinesMove(t *testing.T) {
	src := manyStatements(6)
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatal(err)
	}
	before := c.Program()
	// Insert a whole new line at the top: lines below shift by one.
	if err := c.Update([]Edit{{Start: Span{Line: 1, Col: 1}, End: Span{Line: 1, Col: 1}, NewText: "w = 0\n"}}); err != nil {
		t.Fatal(err)
	}
	after := c.Program()
	if after.Stmts[1] == before.Stmts[0] && c.Reused() > 0 && after.Stmts[1] != nil {
		// A reused node below an inserted line would report the old line number.
		for i := 1; i < len(after.Stmts); i++ {
			if after.Stmts[i] == before.Stmts[i-1] {
				t.Fatalf("statement %d was reused across a line insertion; its span would be stale", i)
			}
		}
	}
	if want := mustParse(t, c.Source()); !reflect.DeepEqual(after, want) {
		t.Fatalf("incremental parse != full parse after a line insertion")
	}
}

// Boundaries must shift with the text, or the next edit classifies the wrong statements.
func TestParseCacheBoundariesShiftWithTheEdit(t *testing.T) {
	src := "x = 1\ny = 2\n"
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Update([]Edit{{Start: Span{Line: 1, Col: 5}, End: Span{Line: 1, Col: 6}, NewText: "100"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := c.Source(), "x = 100\ny = 2\n"; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}
	// A second edit, now targeting the shifted second line, must still classify
	// correctly (it relies on the boundaries recorded for the reused tail).
	if err := c.Update([]Edit{{Start: Span{Line: 2, Col: 5}, End: Span{Line: 2, Col: 6}, NewText: "200"}}); err != nil {
		t.Fatal(err)
	}
	if got, want := c.Source(), "x = 100\ny = 200\n"; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}
	if want := mustParse(t, c.Source()); !reflect.DeepEqual(c.Program(), want) {
		t.Fatalf("incremental parse != full parse after successive shifted edits")
	}
}

// Deleting text is an edit with an empty replacement; the tail rule must apply to it too.
func TestParseCacheTailReuseOnDeletion(t *testing.T) {
	src := "x = 12345\ny = 2\nz = 3\n"
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatal(err)
	}
	before := c.Program()
	// Delete four of the five digits: the line shrinks, the tail shifts left.
	if err := c.Update([]Edit{{Start: Span{Line: 1, Col: 6}, End: Span{Line: 1, Col: 10}, NewText: ""}}); err != nil {
		t.Fatal(err)
	}
	if got, want := c.Source(), "x = 1\ny = 2\nz = 3\n"; got != want {
		t.Fatalf("source = %q, want %q", got, want)
	}
	if c.Program().Stmts[1] != before.Stmts[1] || c.Program().Stmts[2] != before.Stmts[2] {
		t.Errorf("statements after a deletion lost identity")
	}
	if want := mustParse(t, c.Source()); !reflect.DeepEqual(c.Program(), want) {
		t.Fatalf("incremental parse != full parse after a deletion")
	}
}

// An edit that is not a single-line substitution (it introduces a newline) must not
// reuse the tail.
func TestParseCacheMultiLineEditReParsesTheTail(t *testing.T) {
	src := "x = 1\ny = 2\nz = 3\n"
	c, err := NewParseCache(src)
	if err != nil {
		t.Fatal(err)
	}
	before := c.Program()
	if err := c.Update([]Edit{{Start: Span{Line: 1, Col: 5}, End: Span{Line: 1, Col: 6}, NewText: "1\nw = 9"}}); err != nil {
		t.Fatal(err)
	}
	if c.Reused() != 0 {
		t.Errorf("Reused() = %d after a multi-line edit, want 0 (spans below would be stale)", c.Reused())
	}
	if c.Program().Stmts[1] == before.Stmts[1] {
		t.Errorf("tail was reused across a newline insertion")
	}
	if want := mustParse(t, c.Source()); !reflect.DeepEqual(c.Program(), want) {
		t.Fatalf("incremental parse != full parse")
	}
}
