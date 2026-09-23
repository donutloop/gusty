package lang

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// multi-char operators and punctuation (longest first).
var ops = []string{
	"//=", "//", "==", "!=", "<=", ">=", "->", "**",
	"+=", "-=", "*=", "/=", "%=",
	"+", "-", "*", "/", "%", "<", ">", "=", "(", ")", "[", "]", "{", "}",
	",", ":", ".", "&", "|", "@",
}

// LexError is a lexical error with a source span.
type LexError struct {
	Span Span
	Msg  string
}

func (e *LexError) Error() string {
	return fmt.Sprintf("lex error at %d:%d: %s", e.Span.Line, e.Span.Col, e.Msg)
}

// colAt returns the 1-based rune column at byte offset i, measured from lineStart.
func colAt(src string, lineStart, i int) int {
	return utf8.RuneCount([]byte(src[lineStart:i])) + 1
}

// Lex tokenizes src into tokens, emitting NEWLINE/INDENT/DEDENT per Python rules.
// Blank and comment-only lines produce no NEWLINE and do not change indentation.
// isDigitForBase reports whether c is a digit in the given base.
func isDigitForBase(c byte, base int) bool {
	switch base {
	case 16:
		return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
	case 10:
		return c >= '0' && c <= '9'
	case 8:
		return c >= '0' && c <= '7'
	case 2:
		return c == '0' || c == '1'
	}
	return false
}

// scanDigits scans digits of base, allowing single '_' separators between
// digits, or a single leading separator after a base prefix (allowLeading,
// e.g. 0x_FF). It returns the cleaned digit string (separators removed) and
// the index just past the last digit, or an error on misplaced separators.
func scanDigits(src string, i, base int, allowLeading bool) (string, int, *LexError) {
	digits := ""
	seen := false
	for i < len(src) {
		c := src[i]
		switch {
		case c == '_':
			// a separator is valid only between digits, or once right after a
			// non-decimal base prefix (e.g. 0x_FF).
			if seen {
				if i+1 >= len(src) || !isDigitForBase(src[i+1], base) {
					return "", i, &LexError{Msg: "misplaced '_' in numeric literal"}
				}
				seen = false
				i++
				continue
			}
			if allowLeading {
				if i+1 >= len(src) || !isDigitForBase(src[i+1], base) {
					return "", i, &LexError{Msg: "misplaced '_' in numeric literal"}
				}
				allowLeading = false
				i++
				continue
			}
			return "", i, &LexError{Msg: "misplaced '_' in numeric literal"}
		case isDigitForBase(c, base):
			digits += string(c)
			seen = true
			i++
			continue
		default:
			return digits, i, nil
		}
	}
	if !seen {
		return "", i, &LexError{Msg: "invalid numeric literal"}
	}
	return digits, i, nil
}

// lexNumber parses a numeric literal beginning at src[start] (the first
// character of the value, i.e. after any leading '-' sign). It supports
// decimal integer/float, hex (0x), binary (0b), octal (0o), and '_' digit
// separators. It returns whether the literal is a float, its integer value,
// its float value, and the index just past the end of the literal.
func lexNumber(src string, start int) (isFloat bool, ival int64, fval float64, end int, err *LexError) {
	i := start
	base := 10
	allowLeading := false
	if i+1 < len(src) && src[i] == '0' {
		switch src[i+1] {
		case 'x', 'X':
			base, allowLeading = 16, true
			i += 2
		case 'b', 'B':
			base, allowLeading = 2, true
			i += 2
		case 'o', 'O':
			base, allowLeading = 8, true
			i += 2
		}
	}
	intDigits, j, serr := scanDigits(src, i, base, allowLeading)
	if serr != nil {
		return false, 0, 0, start, serr
	}
	if base != 10 {
		// hex/binary/octal are integer-only literals
		v, perr := strconv.ParseInt(intDigits, base, 64)
		if perr != nil {
			return false, 0, 0, start, &LexError{Msg: "integer literal too large"}
		}
		return false, v, 0, j, nil
	}
	// decimal: integer or float
	if j < len(src) && src[j] == '.' {
		fracDigits, k, ferr := scanDigits(src, j+1, 10, false)
		if ferr != nil {
			return false, 0, 0, start, ferr
		}
		text := intDigits + "." + fracDigits
		f, perr := strconv.ParseFloat(text, 64)
		if perr != nil {
			return false, 0, 0, start, &LexError{Msg: "invalid float literal"}
		}
		return true, 0, f, k, nil
	}
	v, perr := strconv.ParseInt(intDigits, 10, 64)
	if perr != nil {
		return false, 0, 0, start, &LexError{Msg: "integer literal too large"}
	}
	return false, v, 0, j, nil
}

func Lex(src string) ([]Token, error) {
	var toks []Token
	n := len(src)
	i := 0
	line := 1
	lineStart := 0
	indentStack := []int{0}

	emit := func(kind TokenKind, text string, setFn func(*Token)) {
		sp := Span{Line: line, Col: colAt(src, lineStart, i)}
		t := Token{Kind: kind, Text: text, Span: sp}
		if setFn != nil {
			setFn(&t)
		}
		toks = append(toks, t)
	}
	lastKind := func() TokenKind {
		if len(toks) == 0 {
			return TokEOF
		}
		return toks[len(toks)-1].Kind
	}

	for i < n {
		// measure leading indentation of a new line
		indent := 0
		lineStart = i
		for i < n {
			switch src[i] {
			case ' ':
				indent++
				i++
			case '\t':
				indent += 8
				i++
			default:
				goto indentDone
			}
		}
	indentDone:
		if i >= n {
			break
		}
		// skip blank / comment-only lines (no NEWLINE, no indent change)
		if src[i] == '\n' {
			i++
			line++
			lineStart = i
			continue
		}
		if src[i] == '#' {
			for i < n && src[i] != '\n' {
				i++
			}
			// leave newline for next iteration
			continue
		}
		// emit INDENT/DEDENT based on indent vs stack
		top := indentStack[len(indentStack)-1]
		if indent > top {
			emit(TokIndent, "", nil)
			indentStack = append(indentStack, indent)
		} else if indent < top {
			for len(indentStack) > 1 && indentStack[len(indentStack)-1] > indent {
				emit(TokDedent, "", nil)
				indentStack = indentStack[:len(indentStack)-1]
			}
		}

		// scan the rest of the line
		for i < n {
			c := src[i]
			switch {
			case c == '\n':
				// end of logical line: NEWLINE
				emit(TokNewline, "\\n", nil)
				i++
				line++
				lineStart = i
				goto nextLine
			case c == ' ' || c == '\t' || c == '\r':
				i++
			case c == '#':
				// comment: skip to newline (handled above)
				for i < n && src[i] != '\n' {
					i++
				}
			case c == '"' || c == '\'':
			quote := c
			start := i
			// triple-quoted string (""" / ''' ) may span multiple lines
			if i+2 < n && src[i+1] == quote && src[i+2] == quote {
				end, val, ok := scanString(src, i, quote, true, false)
				if !ok {
					return nil, &LexError{Span: Span{Line: line, Col: colAt(src, lineStart, i)}, Msg: "unterminated triple-quoted string"}
				}
				i = end
				// a triple string may contain newlines; update line tracking
				nl, last := countNewlines(src[start:end])
				line += nl
				if last >= 0 {
					lineStart = start + last + 1
				}
				emit(TokTripleString, src[start:end], func(t *Token) { t.Str = val })
				continue
			}
			j := i + 1
			val := ""
			for j < n && src[j] != quote {
				if src[j] == '\\' && j+1 < n {
					// escape: drop the backslash, keep the next character
					val += string(src[j+1])
					j += 2
					continue
				}
				val += string(src[j])
				j++
			}
			i = j
			i++ // skip closing quote
			emit(TokString, src[start:i], func(t *Token) { t.Str = val })
			case c == '-' && i+1 < n && unicode.IsDigit(rune(src[i+1])):
				start := i
				isFloat, ival, fval, end, lerr := lexNumber(src, start+1)
				if lerr != nil {
								return nil, &LexError{Span: Span{Line: line, Col: colAt(src, lineStart, start)}, Msg: lerr.Msg}
				}
				text := src[start:end]
				if isFloat {
								emit(TokFloat, text, func(t *Token) { t.Float = -fval })
				} else {
								emit(TokInt, text, func(t *Token) { t.Int = -ival })
				}
				i = end
			case c >= '0' && c <= '9':
				start := i
				isFloat, ival, fval, end, lerr := lexNumber(src, i)
				if lerr != nil {
								return nil, &LexError{Span: Span{Line: line, Col: colAt(src, lineStart, start)}, Msg: lerr.Msg}
				}
				text := src[start:end]
				if isFloat {
								emit(TokFloat, text, func(t *Token) { t.Float = fval })
				} else {
								emit(TokInt, text, func(t *Token) { t.Int = ival })
				}
				i = end
			case isIdentStart(c):
				// f-string prefix: f"..." / f'...'
				if (c == 'f' || c == 'F') && i+1 < n && (src[i+1] == '"' || src[i+1] == '\'') {
					quote := src[i+1]
					start := i + 2
					j := start
					raw := ""
					for j < n && src[j] != quote {
						if src[j] == '\\' && j+1 < n {
							raw += src[start:j] + "\\" + string(src[j+1])
							start = j + 2
							j += 2
							continue
						}
						j++
					}
					if j >= n {
						return nil, &LexError{Msg: "unterminated f-string"}
					}
					raw += src[start:j]
					emit(TokFString, src[i:j+1], func(t *Token) { t.FStrRaw = raw })
					i = j
					i++
					continue
				}
			if (c == 'r' || c == 'R') && i+1 < n && (src[i+1] == '"' || src[i+1] == '\'') {
				quote := src[i+1]
				start := i
				i++ // consume the r/R prefix
				triple := i+2 < n && src[i+1] == quote && src[i+2] == quote
				kind := TokRawString
				if triple {
					kind = TokRawTripleString
				}
				end, val, ok := scanString(src, i, quote, triple, true)
				if !ok {
					return nil, &LexError{Span: Span{Line: line, Col: colAt(src, lineStart, i)}, Msg: "unterminated raw string"}
				}
				i = end
				if triple {
					nl, last := countNewlines(src[start:end])
					line += nl
					if last >= 0 {
						lineStart = start + last + 1
					}
				}
				emit(kind, src[start:end], func(t *Token) { t.Str = val })
				continue
			}
				j := i
				for j < n && isIdentChar(src[j]) {
					j++
				}
				word := src[i:j]
				if keywords[word] {
					emit(TokKeyword, word, nil)
				} else {
					emit(TokIdent, word, nil)
				}
				i = j
			default:
				matched := false
				for _, op := range ops {
					if strings.HasPrefix(src[i:], op) {
						emit(TokOp, op, nil)
						i += len(op)
						matched = true
						break
					}
				}
				if !matched {
					return nil, &LexError{Span: Span{Line: line, Col: colAt(src, lineStart, i)}, Msg: fmt.Sprintf("unexpected character %q", string(c))}
				}
			}
		}
	nextLine:
		_ = lastKind
		lineStart = i
	}

	// close open blocks at EOF
	for len(indentStack) > 1 {
		emit(TokDedent, "", nil)
		indentStack = indentStack[:len(indentStack)-1]
	}
	// ensure a trailing NEWLINE statement terminator when the last token isn't one
	if len(toks) > 0 {
		k := toks[len(toks)-1].Kind
		if k != TokNewline && k != TokDedent {
			emit(TokNewline, "\\n", nil)
		}
	}
	emit(TokEOF, "", nil)
	return toks, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// scanString scans a string literal body. It is called with i at the opening
// quote (or at the first of three quote bytes for triple strings). quote is
// the quote byte. triple means the closing delimiter is three quote bytes.
// raw means no escape processing: backslashes are literal, except that a
// backslash immediately before the quote keeps the string open (so the quote
// appears in the value). It returns the index just past the closing delimiter
// and the decoded value, or ok=false when the string is unterminated.
func scanString(src string, i int, quote byte, triple, raw bool) (int, string, bool) {
	n := len(src)
	step := 1
	if triple {
		step = 3
	}
	i += step
	var b strings.Builder
	for i < n {
		c := src[i]
		if c == quote {
			if triple {
				if i+step <= n && src[i] == quote && src[i+1] == quote && src[i+2] == quote {
					i += step
					return i, b.String(), true
				}
				b.WriteByte(c)
				i++
				continue
			}
			i++
			return i, b.String(), true
		}
		if raw {
			if c == '\\' && i+1 < n && src[i+1] == quote {
				b.WriteByte(c)
				b.WriteByte(src[i+1])
				i += 2
				continue
			}
			b.WriteByte(c)
			i++
			continue
		}
		// non-raw: escape processing (drop the backslash, keep the next byte)
		if c == '\\' && i+1 < n {
			i++
			b.WriteByte(src[i])
			i++
			continue
		}
		b.WriteByte(c)
		i++
	}
	return i, "", false
}

// countNewlines returns the number of '\n' bytes in s and the 0-based index of
// the last one, or -1 when s contains no newline.
func countNewlines(s string) (int, int) {
	count, last := 0, -1
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			count++
			last = i
		}
	}
	return count, last
}
