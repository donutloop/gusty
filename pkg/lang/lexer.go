package lang

import (
	"fmt"
	"golang.org/x/text/unicode/norm"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// multi-char operators and punctuation (longest first).
var ops = []string{
	"//=", "//", "==", "!=", "<=", ">=", "->", "**", ":=",
	"+=", "-=", "*=", "/=", "%=",
	"+", "-", "*", "/", "%", "<", ">", "=", "(", ")", "[", "]", "{", "}",
	",", ":", ".", "&", "|", "@",
}

// minusBeginsLiteral answers the one question a `-` followed by a digit cannot
// answer by itself: is it the SIGN of a literal (`x = -1`, `xs[-1]`, `f(-1)`,
// `{-1: "neg"}`) or the SUBTRACTION operator that the parser can already follow
// with a unary minus (`y = x-1`)? The rule used to look only at the character
// AFTER the minus, so `y = x-1` was lexed as `y = x` and an expression statement
// `-1`: the program printed `5` where the reference prints `4`, at exit 0, and
// `print(1-1)` did not even parse (roadmap Gap R.242). The character before the
// minus is already in hand wherever `emit` is called, so the rule asks it:
//
//   - after something that can END an expression — a number, a name, a string,
//     a closing `)` `]` `}`, or one of the three literal keywords `True`, `False`,
//     `None` — the minus is the operator and the parser's existing unary-minus
//     rule takes whatever comes next, which is what `5--1` and `5- -1` already
//     exercised;
//
//   - everywhere else — the start of a line, after an operator, after `,` `:`
//     `(` `[` `{` `=`, after a keyword like `return`, `in`, `else` or `not`, after
//     a homoglyph note that is not a token of the program at all — `-N` stays one
//     literal, exactly as it was, so no call argument, index, dict key, default or
//     `print(-1)` changes answer.
//
// Only the SIGN position of a `-digit` changes; no other token is affected, and
// the merge is never decided by looking ahead past the digit.
func minusBeginsLiteral(toks []Token) bool {
	for j := len(toks) - 1; j >= 0; j-- {
		t := toks[j]
		if t.Kind == TokWarning {
			// the homoglyph note a warning path appends after an identifier is not
			// part of the program's token stream
			continue
		}
		switch t.Kind {
		case TokInt, TokFloat, TokString, TokRawString, TokTripleString, TokRawTripleString, TokFString, TokIdent:
			return false
		case TokOp:
			switch t.Text {
			case ")", "]", "}":
				return false
			}
		case TokKeyword:
			switch t.Text {
			// `True-1` is an arithmetic expression, not a statement followed by `-1`
			case "True", "False", "None":
				return false
			}
		}
		return true
	}
	return true // nothing emitted yet: the minus begins the line
}

// dotBeginsLiteral asks the same question, of the same walk, for the other operator whose leading-character
// meaning depends on what came before: is a `.` followed by a digit the START OF A LITERAL (`.5`, which is
// `0.5` to the reference) or the MEMBER OPERATOR that the parser already knows how to follow (`obj.5`, which
// is an attribute error in both tools and must stay one — an object's name, a dot, then a malformed number).
//
// The two operators are decided by one walk rather than two tables because the question they ask is one
// question — *can what came before END an expression?* — and two tables that answer it can drift, which is how
// `y = x-1` came to print `5` at exit 0 (**Gap R.242**, **ADR 0338**). A `float` token counts as an expression
// end here for exactly the reason it does there: `1.5 .5` is refused by the reference, so a rule that let the
// second dot begin a literal would be accepting a program the reference rejects.
//
// Where the walk finds no token at all the answer is yes: a line that begins `.5` is an expression statement
// the reference runs. And the walk never looks PAST the previous token, so no decision here is made by
// lookahead — the same restriction **ADR 0338** put on the minus.
func dotBeginsLiteral(toks []Token) bool { return minusBeginsLiteral(toks) }

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
// posAt returns the 1-based line and rune-based column of the byte offset off.
func posAt(src string, off int) (line, col int) {
	line, col = 1, 1
	for _, r := range src[:off] {
		if r == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return
}

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
	// The float the reference spells without an integer part: `.5` is `0.5` to CPython, and until this arm
	// existed the lexer had cases for a digit and for the `0x`/`0b`/`0o` prefixes and NONE for a leading dot,
	// so `print(.5)`, `print(1+.5)`, `print(-.5)` and `x = .5` were refused at the front end while the
	// reference answered `0.5`, `1.5`, `-0.5`, `0.5` (**roadmap Gap R.245**). The caller decides whether a `.` may
	// begin a token at all (`dotBeginsLiteral`), so this arm never has to guess between a literal and a member
	// access; what it does require is at least one digit after the dot, and it takes the SAME `_` rules the
	// fraction below takes, so `.5_1` is the reference's `0.51` and `._1` is not a number on either tool.
	if i < len(src) && src[i] == '.' {
		if i+1 >= len(src) || !isDigitForBase(src[i+1], 10) {
			return false, 0, 0, start, &LexError{Msg: "float literal needs a digit after the '.'"}
		}
		fracDigits, k, ferr := scanDigits(src, i+1, 10, false)
		if ferr != nil {
			return false, 0, 0, start, ferr
		}
		f, perr := strconv.ParseFloat("0."+fracDigits, 64)
		if perr != nil {
			return false, 0, 0, start, &LexError{Msg: "invalid float literal"}
		}
		return true, 0, f, k, nil
	}
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

	emit := func(kind TokenKind, text string, start, end int, setFn func(*Token)) {
		var sp Span
		if start < lineStart {
			sl, sc := posAt(src, start)
			sp = Span{Line: sl, Col: sc}
		} else {
			sp = Span{Line: line, Col: colAt(src, lineStart, start)}
		}
		sr := utf8.RuneCount([]byte(src[:start]))
		er := utf8.RuneCount([]byte(src[:end]))
		multi := strings.Contains(src[start:end], "\n")
		t := Token{Kind: kind, Text: text, Span: sp, Start: start, End: end, StartRune: sr, EndRune: er, Multiline: multi}
		if setFn != nil {
			setFn(&t)
		}
		toks = append(toks, t)
	}
	emitErr := func(msg string, start, end int) {
		var sp Span
		if start < lineStart {
			sl, sc := posAt(src, start)
			sp = Span{Line: sl, Col: sc}
		} else {
			sp = Span{Line: line, Col: colAt(src, lineStart, start)}
		}
		sr := utf8.RuneCount([]byte(src[:start]))
		er := utf8.RuneCount([]byte(src[:end]))
		multi := strings.Contains(src[start:end], "\n")
		toks = append(toks, Token{Kind: TokError, Span: sp, ErrMsg: msg, Start: start, End: end, StartRune: sr, EndRune: er, Multiline: multi})
	}
	scanIdent := func(i int) int {
		j := i
		for j < n {
			r, size := utf8.DecodeRuneInString(src[j:])
			if r == utf8.RuneError || !isXIDContinue(r) {
				break
			}
			j += size
		}
		word := norm.NFC.String(src[i:j])
		if keywords[word] {
			emit(TokKeyword, word, i, j, nil)
		} else {
			emit(TokIdent, word, i, j, nil)
		}
		// homoglyph warning: flag non-ASCII identifier runes visually
		// confusable with ASCII identifier characters (Greek/Cyrillic
		// lookalikes) to surface homoglyph attacks.
		for _, r := range word {
			if ascii, ok := confusables[r]; ok {
				emit(TokWarning, "", i, j, func(t *Token) {
					t.ErrMsg = fmt.Sprintf("identifier contains %U (%q), visually confusable with %q; rename to avoid homoglyph confusion", r, r, ascii)
				})
				break
			}
		}
		return j
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
			emit(TokIndent, "", i, i, nil)
			indentStack = append(indentStack, indent)
		} else if indent < top {
			for len(indentStack) > 1 && indentStack[len(indentStack)-1] > indent {
				emit(TokDedent, "", i, i, nil)
				indentStack = indentStack[:len(indentStack)-1]
			}
		}

		// scan the rest of the line
		for i < n {
			c := src[i]
			switch {
			case c == '\n':
				// end of logical line: NEWLINE
				emit(TokNewline, "\\n", i, i, nil)
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
			case c == ';':
				// A ';' between simple statements is the on-line spelling of the
				// newline that would otherwise separate them (`x = 1; print(x)`).
				// It used to be lexed as an unexpected character, which recorded a
				// diagnostic that the interpreter ignored and the JIT/AOT refused -
				// so the same program answered on one engine and was rejected by
				// another. A separator is not an error: emit a token, say nothing.
				// (`x = 1;;y = 2` - the empty statement - is rejected by the parser,
				// where every syntax rule lives, not by a diagnostic the interpreter
				// would read differently from the JIT.)
				emit(TokSemi, ";", i, i+1, nil)
				i++
			case c == '\\':
				// L4.6 line continuation: a trailing backslash immediately before
				// the newline joins the next physical line into this logical line,
				// ignoring the continuation line's leading indentation.
				if i+1 < n && src[i+1] == '\n' {
					i += 2
				} else if i+1 < n && src[i+1] == '\r' && i+2 < n && src[i+2] == '\n' {
					i += 3
				} else {
					emitErr("unexpected character '\\'", i, i)
					i++
					continue
				}
				line++
				// skip leading whitespace and blank / comment-only continuation lines
				for i < n {
					for i < n && (src[i] == ' ' || src[i] == '\t' || src[i] == '\r') {
						i++
					}
					if i < n && src[i] == '\n' {
						i++
						line++
						continue
					}
					if i < n && src[i] == '#' {
						for i < n && src[i] != '\n' {
							i++
						}
						continue
					}
					break
				}
				lineStart = i
				continue
			case c == '"' || c == '\'':
				quote := c
				start := i
				// triple-quoted string (""" / ''' ) may span multiple lines
				if i+2 < n && src[i+1] == quote && src[i+2] == quote {
					end, val, ok := scanString(src, i, quote, true, false)
					if !ok {
						emitErr("unterminated triple-quoted string", start, i)
						for i < n && src[i] != '\n' {
							i++
						}
						continue
					}
					i = end
					// a triple string may contain newlines; update line tracking
					nl, last := countNewlines(src[start:end])
					line += nl
					if last >= 0 {
						lineStart = start + last + 1
					}
					emit(TokTripleString, src[start:end], start, end, func(t *Token) { t.Str = val })
					continue
				}
				// One scanner for every string form: the inline copy of this loop
				// built the value with `string(byte)`, which re-encodes each non-ASCII
				// byte as a rune, so "héllo" arrived as "hÃ©llo" with len 8 instead of
				// 6 — silently, in both backends.
				end, val, ok := scanString(src, i, quote, false, false)
				if !ok {
					emitErr("unterminated string", start, i)
					for i < n && src[i] != '\n' {
						i++
					}
					continue
				}
				i = end
				emit(TokString, src[start:i], start, i, func(t *Token) { t.Str = val })
			case c == '-' && i+1 < n && unicode.IsDigit(rune(src[i+1])) && minusBeginsLiteral(toks):
				start := i
				isFloat, ival, fval, end, lerr := lexNumber(src, start+1)
				if lerr != nil {
					emitErr(lerr.Msg, start, i)
					i = start + 1
					continue
				}
				text := src[start:end]
				if isFloat {
					emit(TokFloat, text, start, end, func(t *Token) { t.Float = -fval })
				} else {
					emit(TokInt, text, start, end, func(t *Token) { t.Int = -ival })
				}
				i = end
			case c == '.' && i+1 < n && isDigitForBase(src[i+1], 10) && dotBeginsLiteral(toks):
				// `.5` — see `dotBeginsLiteral` for why this position, and only this one, may take the dot.
				// It is emitted as an ordinary float token with its own text and span, so every road downstream
				// (the fold, the printer, the float word) sees what it already knows how to read; nothing here
				// rewrites the source or invents a `0` the program did not write.
				start := i
				_, _, fval, end, lerr := lexNumber(src, i)
				if lerr != nil {
					emitErr(lerr.Msg, start, i)
					i = start + 1
					continue
				}
				text := src[start:end]
				emit(TokFloat, text, start, end, func(t *Token) { t.Float = fval })
				i = end
			case c >= '0' && c <= '9':
				start := i
				isFloat, ival, fval, end, lerr := lexNumber(src, i)
				if lerr != nil {
					emitErr(lerr.Msg, start, i)
					i = start + 1
					continue
				}
				text := src[start:end]
				if isFloat {
					emit(TokFloat, text, start, end, func(t *Token) { t.Float = fval })
				} else {
					emit(TokInt, text, start, end, func(t *Token) { t.Int = ival })
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
						emitErr("unterminated f-string", start, i)
						for i < n && src[i] != '\n' {
							i++
						}
						continue
					}
					raw += src[start:j]
					emit(TokFString, src[i:j+1], i, j+1, func(t *Token) { t.FStrRaw = raw })
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
						emitErr("unterminated raw string", start, i)
						for i < n && src[i] != '\n' {
							i++
						}
						continue
					}
					i = end
					if triple {
						nl, last := countNewlines(src[start:end])
						line += nl
						if last >= 0 {
							lineStart = start + last + 1
						}
					}
					emit(kind, src[start:end], start, end, func(t *Token) { t.Str = val })
					continue
				}
				i = scanIdent(i)
			default:
				if c >= utf8.RuneSelf {
					r, size := utf8.DecodeRuneInString(src[i:])
					if r != utf8.RuneError && isXIDStart(r) {
						i = scanIdent(i)
						continue
					}
					if r != utf8.RuneError {
						emitErr(fmt.Sprintf("unexpected character %q", string(r)), i, i+size)
					} else {
						emitErr("invalid UTF-8", i, i+1)
					}
					i += size
					continue
				}
				matched := false
				for _, op := range ops {
					if strings.HasPrefix(src[i:], op) {
						emit(TokOp, op, i, i+len(op), nil)
						i += len(op)
						matched = true
						break
					}
				}
				if !matched {
					emitErr(fmt.Sprintf("unexpected character %q", string(c)), i, i)
					i++
					continue
				}
			}
		}
	nextLine:
		_ = lastKind
		lineStart = i
	}

	// close open blocks at EOF
	for len(indentStack) > 1 {
		emit(TokDedent, "", i, i, nil)
		indentStack = indentStack[:len(indentStack)-1]
	}
	// ensure a trailing NEWLINE statement terminator when the last token isn't one
	if len(toks) > 0 {
		k := toks[len(toks)-1].Kind
		if k != TokNewline && k != TokDedent {
			emit(TokNewline, "\\n", i, i, nil)
		}
	}
	emit(TokEOF, "", i, i, nil)
	return toks, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}
func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

// isXIDStart reports whether r may begin an identifier, per the Unicode
// XID_Start derived property. Go's unicode package does not expose the XID
// tables, so we approximate with the category-level ID_Start definition:
// letters + Nl (letter number) + Other_ID_Start additions.
func isXIDStart(r rune) bool {
	return unicode.IsLetter(r) || unicode.Is(unicode.Nl, r) || inOtherIDStart(r)
}

// isXIDContinue reports whether r may continue an identifier (XID_Continue):
// XID_Start + marks + digits + connector punctuation + Other_ID_Continue.
func isXIDContinue(r rune) bool {
	return isXIDStart(r) || unicode.IsMark(r) || unicode.IsDigit(r) || unicode.Is(unicode.Pc, r) || inOtherIDContinue(r)
}

// inOtherIDStart covers the Other_ID_Start additions from Unicode TR31 that
// are not classified as letters by Go's categories (chiefly the CJK and
// Kangxi radicals, which live in symbol categories).
func inOtherIDStart(r rune) bool {
	return (r >= 0x2E80 && r <= 0x2EFF) || (r >= 0x2F00 && r <= 0x2FD5)
}

// inOtherIDContinue covers the Other_ID_Continue additions from Unicode TR31:
// middle dots, Hebrew points, and assorted Tibetan/Lao/Thai combining marks.
func inOtherIDContinue(r rune) bool {
	switch r {
	case 0x00B7, 0x0375, 0x05F3, 0x05F4, 0x30FB, 0x309B, 0x309C:
		return true
	}
	return (r >= 0x0E37 && r <= 0x0E3E) || (r >= 0x0E47 && r <= 0x0E4E) || (r >= 0x0E64 && r <= 0x0E65) ||
		(r >= 0x0ED0 && r <= 0x0ED4) || (r >= 0x0ED7 && r <= 0x0ED8) || (r >= 0x0F37 && r <= 0x0F3A) ||
		(r >= 0x0F3E && r <= 0x0F3F) || (r >= 0x0F47 && r <= 0x0F4E) || (r >= 0x0F57 && r <= 0x0F58) ||
		(r >= 0x0F5F && r <= 0x0F60) || (r >= 0x0F69 && r <= 0x0F6F) || (r >= 0x0F73 && r <= 0x0F7E) ||
		(r >= 0x0F81 && r <= 0x0F84) || (r >= 0x0F86 && r <= 0x0F87)
}

// confusables maps non-ASCII identifier runes that are visually confusable
// with ASCII identifier characters to their ASCII lookalike. This is a
// focused subset of Unicode's confusables.txt covering the Greek/Cyrillic
// homoglyphs most likely to be abused in identifiers (e.g. U+039F GREEK
// CAPITAL OMICRON vs Latin 'O').
var confusables = map[rune]rune{
	'Α': 'A', 'Ε': 'E', 'Η': 'H', 'Ι': 'I', 'Κ': 'K', 'Μ': 'M', 'Ν': 'N', 'Ο': 'O', 'Ρ': 'P', 'Τ': 'T', 'Χ': 'X', 'Ζ': 'Z',
	'α': 'a', 'ε': 'e', 'η': 'h', 'ι': 'i', 'κ': 'k', 'μ': 'u', 'ν': 'v', 'ο': 'o', 'ρ': 'p', 'τ': 't', 'χ': 'x', 'ζ': 'z',
	'А': 'A', 'В': 'B', 'Е': 'E', 'К': 'K', 'М': 'M', 'Н': 'H', 'О': 'O', 'Р': 'P', 'С': 'C', 'Т': 'T', 'У': 'Y', 'Х': 'X',
	'а': 'a', 'в': 'b', 'е': 'e', 'к': 'k', 'м': 'm', 'н': 'h', 'о': 'o', 'п': 'p', 'р': 'p', 'с': 'c', 'т': 't', 'у': 'y', 'х': 'x',
	'І': 'I', 'і': 'i', // Cyrillic byelorussian-Ukrainian i
	'ѕ': 's', // Cyrillic small dze
}

// appendEscape decodes the escape sequence that starts at src[i] (a backslash)
// into b and returns the index just past it. Python's rules, not "drop the
// backslash": \n \t \r \a \b \f \v \0 become control characters, \xHH \uHHHH
// \UHHHHHHHH become the code point, and an escape this list does not know is
// kept verbatim (backslash included) — Python does the same, and dropping the
// backslash silently turned "\n" into the letter n in every compiled program.
// A malformed numeric escape (too few hex digits, an out-of-range code point)
// is also kept verbatim rather than guessed at.
func appendEscape(b *strings.Builder, src string, i int) int {
	n := len(src)
	if i+1 >= n {
		b.WriteByte('\\')
		return i + 1
	}
	c := src[i+1]
	switch c {
	case 'n':
		b.WriteByte('\n')
		return i + 2
	case 't':
		b.WriteByte('\t')
		return i + 2
	case 'r':
		b.WriteByte('\r')
		return i + 2
	case 'a':
		b.WriteByte(7)
		return i + 2
	case 'b':
		b.WriteByte(8)
		return i + 2
	case 'f':
		b.WriteByte(12)
		return i + 2
	case 'v':
		b.WriteByte(11)
		return i + 2
	case '0':
		b.WriteByte(0)
		return i + 2
	case '\\', '\'', '"':
		b.WriteByte(c)
		return i + 2
	case 'x', 'u', 'U':
		want := map[byte]int{'x': 2, 'u': 4, 'U': 8}[c]
		if i+2+want > n {
			b.WriteByte('\\')
			b.WriteByte(c)
			return i + 2
		}
		digits := src[i+2 : i+2+want]
		v, err := strconv.ParseUint(digits, 16, 32)
		if err != nil {
			b.WriteByte('\\')
			b.WriteByte(c)
			return i + 2
		}
		r := rune(v)
		if r > utf8.MaxRune || (r >= 0xD800 && r <= 0xDFFF) {
			b.WriteByte('\\')
			b.WriteByte(c)
			return i + 2
		}
		b.WriteRune(r)
		return i + 2 + want
	default:
		// Unrecognised escape: keep both characters, as Python does.
		b.WriteByte('\\')
		b.WriteByte(c)
		return i + 2
	}
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
		// non-raw: decode the escape the way Python does
		if c == '\\' && i+1 < n {
			i = appendEscape(&b, src, i)
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
