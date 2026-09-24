package lang

import "testing"

// TestCursorPeek verifies bounds-safe lookahead: peek(n) returns the token n
// ahead without advancing, and returns TokEOF past the end of the stream.
func TestCursorPeek(t *testing.T) {
	toks, err := Lex("a b c")
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	c := NewCursor(toks)
	if got := c.peek(0).Text; got != "a" {
		t.Errorf("peek(0).Text = %q, want %q", got, "a")
	}
	if got := c.peek(1).Text; got != "b" {
		t.Errorf("peek(1).Text = %q, want %q", got, "b")
	}
	if got := c.peek(2).Text; got != "c" {
		t.Errorf("peek(2).Text = %q, want %q", got, "c")
	}
	// lookahead must not advance the cursor
	if got := c.peek(0).Text; got != "a" {
		t.Errorf("peek after lookahead = %q, want %q (cursor advanced)", got, "a")
	}
	// beyond the end: TokEOF, never an index panic
	if c.peek(99).Kind != TokEOF {
		t.Errorf("peek(99).Kind = %v, want TokEOF", c.peek(99).Kind)
	}
	if c.peek(-1).Kind != TokEOF {
		t.Errorf("peek(-1).Kind = %v, want TokEOF", c.peek(-1).Kind)
	}
}

// TestCursorWalkMarkReset verifies sequential walk, mark/reset backtracking,
// and that reset seeks back to a previously-marked position.
func TestCursorWalkMarkReset(t *testing.T) {
	toks, err := Lex("a b c")
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	c := NewCursor(toks)

	// consume 'a'
	if got := c.next().Text; got != "a" {
		t.Fatalf("next = %q, want %q", got, "a")
	}
	mark := c.mark()
	if got := c.next().Text; got != "b" {
		t.Fatalf("next = %q, want %q", got, "b")
	}
	if c.position() != mark+1 {
		t.Errorf("position = %d, want %d", c.position(), mark+1)
	}
	// backtrack to the mark and re-read 'b'
	c.reset(mark)
	if got := c.peek(0).Text; got != "b" {
		t.Errorf("after reset peek = %q, want %q", got, "b")
	}
	if c.position() != mark {
		t.Errorf("after reset position = %d, want %d", c.position(), mark)
	}
	// walking to the end lands on the EOF token; atEOF() holds and an extra
	// next() returns TokEOF without advancing past the end.
	for c.peek(0).Kind != TokEOF {
		c.next()
	}
	if !c.atEOF() {
		t.Errorf("expected atEOF at end")
	}
	before := c.position()
	if got := c.next().Kind; got != TokEOF {
		t.Errorf("next at EOF.Kind = %v, want TokEOF", got)
	}
	if c.position() != before {
		t.Errorf("next at EOF advanced cursor: %d -> %d", before, c.position())
	}
}

// TestCursorPredicates verifies atEOF/atNewline/atDedent/atIndent and
// skipNewlines on a multi-line token stream.
func TestCursorPredicates(t *testing.T) {
	toks, err := Lex("a\n\nb")
	if err != nil {
		t.Fatalf("lex: %v", err)
	}
	c := NewCursor(toks)
	if c.atEOF() || !c.atIdent() {
		t.Fatalf("first token: atEOF=%v", c.atEOF())
	}
	c.next() // 'a'
	if !c.atNewline() {
		t.Errorf("expected newline after 'a'")
	}
	c.skipNewlines()
	if c.atNewline() {
		t.Errorf("skipNewlines left a newline")
	}
	if got := c.peek(0).Text; got != "b" {
		t.Errorf("after skipNewlines peek = %q, want %q", got, "b")
	}
	// drain to EOF
	for c.peek(0).Kind != TokEOF {
		c.next()
	}
	if !c.atEOF() {
		t.Errorf("expected atEOF at end")
	}
}

// atIdent is a tiny helper asserting the current token is an identifier.
func (c *Cursor) atIdent() bool { return c.peek(0).Kind == TokIdent }
