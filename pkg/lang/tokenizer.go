package lang

import (
	"fmt"
	"strings"
	"unicode"
)

type TokenType int

const (
	TokenWhile TokenType = iota
	TokenLet
	TokenFunction
	TokenOpenBracket
	TokenCloseBracket
	TokenOpenCurly
	TokenCloseCurly
	TokenIdentifier
	TokenEquals
	TokenInteger32
	TokenUnknown
)

type Token struct {
	Type  TokenType
	Value string
}

func (t Token) String() string {
	switch t.Type {
	case TokenWhile:
		return "while"
	case TokenLet:
		return "let"
	case TokenFunction:
		return "function"
	case TokenOpenBracket:
		return "("
	case TokenCloseBracket:
		return ")"
	case TokenOpenCurly:
		return "{"
	case TokenCloseCurly:
		return "}"
	case TokenIdentifier:
		return fmt.Sprintf("identifier(%s)", t.Value)
	default:
		return fmt.Sprintf("unknown(%s)", t.Value)
	}
}

func isBracket(r rune) bool {
	return r == '(' || r == ')'
}

func isCurly(r rune) bool {
	return r == '{' || r == '}'
}

func isComma(r rune) bool {
	return r == ','
}

func isEqual(r rune) bool {
	return r == '='
}

func Tokenize(input string) []Token {
	tokens := make([]Token, 0)

	var sb strings.Builder
	for _, r := range input {
		if isComma(r) {
			if sb.Len() > 0 {
				word := sb.String()
				sb.Reset()

				switch word {
				case "i32":
					tokens = append(tokens, Token{Type: TokenInteger32})
				default:
					tokens = append(tokens, Token{Type: TokenIdentifier, Value: word})
				}
			}
			continue
		}

		if isEqual(r) {
			tokens = append(tokens, Token{Type: TokenEquals})
			continue
		}

		if unicode.IsSpace(r) {
			if sb.Len() > 0 {
				word := sb.String()
				sb.Reset()

				switch word {
				case "while":
					tokens = append(tokens, Token{Type: TokenWhile})
				case "let":
					tokens = append(tokens, Token{Type: TokenLet})
				case "function":
					tokens = append(tokens, Token{Type: TokenFunction})
				case "i32":
					tokens = append(tokens, Token{Type: TokenInteger32})
				default:
					tokens = append(tokens, Token{Type: TokenIdentifier, Value: word})
				}
			}
		} else if isCurly(r) {
			if sb.Len() > 0 {
				word := sb.String()
				sb.Reset()

				switch word {
				case "while":
					tokens = append(tokens, Token{Type: TokenWhile})
				case "let":
					tokens = append(tokens, Token{Type: TokenLet})
				default:
					tokens = append(tokens, Token{Type: TokenIdentifier, Value: word})
				}
			}

			switch r {
			case '(':
				tokens = append(tokens, Token{Type: TokenOpenBracket})
			case ')':
				tokens = append(tokens, Token{Type: TokenCloseBracket})
			case '{':
				tokens = append(tokens, Token{Type: TokenOpenCurly})
			case '}':
				tokens = append(tokens, Token{Type: TokenCloseCurly})
			}
		} else if isBracket(r) {
			if sb.Len() > 0 {
				word := sb.String()
				sb.Reset()

				switch word {
				case "i32":
					tokens = append(tokens, Token{Type: TokenInteger32})
				default:
					tokens = append(tokens, Token{Type: TokenIdentifier, Value: word})
				}
			}

			switch r {
			case '(':
				tokens = append(tokens, Token{Type: TokenOpenBracket})
			case ')':
				tokens = append(tokens, Token{Type: TokenCloseBracket})
			}
		} else {
			sb.WriteRune(r)
		}

	}

	if sb.Len() > 0 {
		word := sb.String()
		switch word {
		case "while":
			tokens = append(tokens, Token{Type: TokenWhile})
		case "let":
			tokens = append(tokens, Token{Type: TokenLet})
		case "function":
			tokens = append(tokens, Token{Type: TokenFunction})
		default:
			tokens = append(tokens, Token{Type: TokenIdentifier, Value: word})
		}
	}

	return tokens
}
