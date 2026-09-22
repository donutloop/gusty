package lang

import "strings"

// IsIncomplete reports whether src is an incomplete REPL input that needs more
// lines before it can be parsed and evaluated. The gustyc parser is line
// oriented for statements/expressions, but suites (function/class/if/for/while
// bodies) are indentation based and can span multiple lines. Input is
// incomplete when the last line is an open block header (ends with ':') whose
// suite must still be supplied, or the last typed line is an indented body
// line still inside an open block (not yet closed by a dedent / blank line).
func IsIncomplete(src string) bool {
	if last := lastSignificantToken(src); last != nil && last.Kind == TokOp && last.Text == ":" {
		return true
	}
	return blockOpen(src)
}

// lastSignificantToken returns the last non-structural token of src.
func lastSignificantToken(src string) *Token {
	toks, err := Lex(src)
	if err != nil {
		return nil
	}
	var last *Token
	for i := range toks {
		t := &toks[i]
		if t.Kind == TokEOF {
			break
		}
		if t.Kind != TokNewline && t.Kind != TokIndent && t.Kind != TokDedent {
			last = t
		}
	}
	return last
}

// blockOpen reports whether the source ends inside an open indented block:
// the last typed (non-blank) line is indented deeper than its enclosing block
// header. A blank line typed after the body closes the block (REPL
// convention), as does a dedent back to the header's indent.
func blockOpen(src string) bool {
	lines := strings.Split(src, "\n")
	if len(lines) == 0 {
		return false
	}
	// Count trailing empty elements. The REPL appends one '\n' per typed line,
	// so exactly one trailing "" means the buffer ends with the user's last
	// line; two or more mean a blank line was typed (which closes the block).
	trailing := 0
	for i := len(lines) - 1; i >= 0 && lines[i] == ""; i-- {
		trailing++
	}
	if trailing >= 2 {
		return false
	}
	lastIdx := len(lines) - 1
	for lastIdx >= 0 && strings.TrimSpace(lines[lastIdx]) == "" {
		lastIdx--
	}
	if lastIdx < 0 {
		return false
	}
	lastIndent := indentOf(lines[lastIdx])
	enclosing := 0
	for _, ln := range lines[:lastIdx+1] {
		t := strings.TrimRight(ln, " \t\r")
		if strings.TrimSpace(t) == "" {
			continue
		}
		if strings.HasSuffix(t, ":") {
			h := indentOf(t)
			if h < lastIndent {
				enclosing = h
			}
		}
	}
	return lastIndent > enclosing
}

// indentOf returns the number of leading spaces of a single line.
func indentOf(line string) int {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i
}
