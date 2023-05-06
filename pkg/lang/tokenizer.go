package lang

import (
	"fmt"
	"strings"
	"unicode"
)

type TokenValue string
type TokenRune rune

const (
	TokenWhile        TokenValue = "while"
	TokenLet          TokenValue = "let"
	TokenInteger32    TokenValue = "i32"
	TokenFunction     TokenValue = "function"
	TokenOpenBracket  TokenRune  = '('
	TokenCloseBracket TokenRune  = ')'
	TokenOpenCurly    TokenRune  = '{'
	TokenCloseCurly   TokenRune  = '}'
	TokenComma        TokenRune  = ','
	TokenEquals       TokenRune  = '='
)

type TokenType int

const (
	TokenWhileType TokenType = iota
	TokenLetType
	TokenFunctionType
	TokenOpenBracketType
	TokenCloseBracketType
	TokenOpenCurlyType
	TokenCloseCurlyType
	TokenIdentifierType
	TokenEqualsType
	TokenInteger32Type
	TokenUnknown
)

type Token struct {
	Type  TokenType
	Value string
}

func (t Token) String() string {
	switch t.Type {
	case TokenWhileType:
		return string(TokenWhile)
	case TokenLetType:
		return string(TokenLet)
	case TokenFunctionType:
		return string(TokenFunction)
	case TokenOpenBracketType:
		return string(TokenOpenBracket)
	case TokenCloseBracketType:
		return string(TokenCloseBracket)
	case TokenOpenCurlyType:
		return string(TokenOpenCurly)
	case TokenCloseCurlyType:
		return string(TokenCloseCurly)
	case TokenInteger32Type:
		return string(TokenInteger32)
	case TokenIdentifierType:
		return fmt.Sprintf("identifier(%s)", t.Value)
	default:
		return fmt.Sprintf("unknown(%s)", t.Value)
	}
}

func isBracket(r TokenRune) bool {
	return r == TokenOpenBracket || r == TokenCloseBracket
}

func isCurly(r TokenRune) bool {
	return r == TokenOpenCurly || r == TokenCloseCurly
}

func isComma(r TokenRune) bool {
	return r == TokenComma
}

func isEqual(r TokenRune) bool {
	return r == TokenEquals
}

func Tokenize(input string) []Token {
	tokens := make([]Token, 0)

	var sb strings.Builder
	for _, r := range input {
		if isComma(TokenRune(r)) {
			if sb.Len() > 0 {
				word := TokenValue(sb.String())
				sb.Reset()

				switch word {
				case TokenInteger32:
					tokens = append(tokens, Token{Type: TokenInteger32Type})
				default:
					tokens = append(tokens, Token{Type: TokenIdentifierType, Value: string(word)})
				}
			}
			continue
		}

		if isEqual(TokenRune(r)) {
			tokens = append(tokens, Token{Type: TokenEqualsType})
			continue
		}

		if unicode.IsSpace(r) {
			if sb.Len() > 0 {
				word := TokenValue(sb.String())
				sb.Reset()

				switch word {
				case TokenWhile:
					tokens = append(tokens, Token{Type: TokenWhileType})
				case TokenLet:
					tokens = append(tokens, Token{Type: TokenLetType})
				case TokenFunction:
					tokens = append(tokens, Token{Type: TokenFunctionType})
				case TokenInteger32:
					tokens = append(tokens, Token{Type: TokenInteger32Type})
				default:
					tokens = append(tokens, Token{Type: TokenIdentifierType, Value: string(word)})
				}
			}
		} else if isCurly(TokenRune(r)) {
			if sb.Len() > 0 {
				word := TokenValue(sb.String())
				sb.Reset()

				switch word {
				case TokenWhile:
					tokens = append(tokens, Token{Type: TokenWhileType})
				case TokenLet:
					tokens = append(tokens, Token{Type: TokenLetType})
				default:
					tokens = append(tokens, Token{Type: TokenIdentifierType, Value: string(word)})
				}
			}

			switch TokenRune(r) {
			case TokenOpenBracket:
				tokens = append(tokens, Token{Type: TokenOpenBracketType})
			case TokenCloseBracket:
				tokens = append(tokens, Token{Type: TokenCloseBracketType})
			case TokenOpenCurly:
				tokens = append(tokens, Token{Type: TokenOpenCurlyType})
			case TokenCloseCurly:
				tokens = append(tokens, Token{Type: TokenCloseCurlyType})
			}
		} else if isBracket(TokenRune(r)) {
			if sb.Len() > 0 {
				word := TokenValue(sb.String())
				sb.Reset()

				switch word {
				case TokenInteger32:
					tokens = append(tokens, Token{Type: TokenInteger32Type})
				default:
					tokens = append(tokens, Token{Type: TokenIdentifierType, Value: string(word)})
				}
			}

			switch TokenRune(r) {
			case TokenOpenBracket:
				tokens = append(tokens, Token{Type: TokenOpenBracketType})
			case TokenCloseBracket:
				tokens = append(tokens, Token{Type: TokenCloseBracketType})
			}
		} else {
			sb.WriteRune(r)
		}

	}

	if sb.Len() > 0 {
		word := TokenValue(sb.String())
		switch word {
		case TokenWhile:
			tokens = append(tokens, Token{Type: TokenWhileType})
		case TokenLet:
			tokens = append(tokens, Token{Type: TokenLetType})
		case TokenFunction:
			tokens = append(tokens, Token{Type: TokenFunctionType})
		default:
			tokens = append(tokens, Token{Type: TokenIdentifierType, Value: string(word)})
		}
	}

	return tokens
}
