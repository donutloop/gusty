package lang

import (
	"reflect"
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
	if c.Reused() != 1 {
		t.Fatalf("expected 1 reused statement, got %d", c.Reused())
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
