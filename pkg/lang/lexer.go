package lang

import (
	"fmt"
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
				j := i + 1
				val := ""
				for j < n && src[j] != quote {
					if src[j] == '\\' && j+1 < n {
						val += string(src[j+1])
						j += 2
					} else {
						val += string(src[j])
						j++
					}
				}
				if j >= n {
					return nil, &LexError{Span: Span{Line: line, Col: colAt(src, lineStart, start)}, Msg: "unterminated string literal"}
				}
				i = j
				emit(TokString, src[start:i], func(t *Token) { t.Str = val })
				i++
			case c == '-' && i+1 < n && unicode.IsDigit(rune(src[i+1])):
				start := i
				neg := true
				i++
				j := i
				for j < n && unicode.IsDigit(rune(src[j])) {
					j++
				}
				isFloat := false
				if j < n && src[j] == '.' {
					isFloat = true
					j++
					for j < n && unicode.IsDigit(rune(src[j])) {
						j++
					}
				}
				digits := src[i:j]
				text := src[start:j]
				if isFloat {
					f := float64(0)
					fmt.Sscanf(digits, "%f", &f)
					if neg {
						f = -f
					}
					emit(TokFloat, text, func(t *Token) { t.Float = f })
				} else {
					var v int64
					fmt.Sscanf(digits, "%d", &v)
					if neg {
						v = -v
					}
					emit(TokInt, text, func(t *Token) { t.Int = v })
				}
				i = j
			case c >= '0' && c <= '9':
				start := i
				j := i
				for j < n && unicode.IsDigit(rune(src[j])) {
					j++
				}
				isFloat := false
				if j < n && src[j] == '.' {
					isFloat = true
					j++
					for j < n && unicode.IsDigit(rune(src[j])) {
						j++
					}
				}
				digits := src[i:j]
				if isFloat {
					f := float64(0)
					fmt.Sscanf(digits, "%f", &f)
					emit(TokFloat, src[start:j], func(t *Token) { t.Float = f })
				} else {
					var v int64
					fmt.Sscanf(digits, "%d", &v)
					emit(TokInt, src[start:j], func(t *Token) { t.Int = v })
				}
				i = j
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
