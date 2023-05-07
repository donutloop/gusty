package lang

import (
	"fmt"
	"strings"
	"unicode"
)

// TokenValue represents the string value of a token.
type TokenValue string

// TokenRune represents a single rune token.
type TokenRune rune

// Constants for keyword and special character tokens.
const (
	TokenWhile             TokenValue = "while"
	TokenLet               TokenValue = "let"
	TokenInteger32         TokenValue = "i32"
	TokenFunction          TokenValue = "function"
	TokenOpenParenthesis   TokenRune  = '('
	TokenCloseParenthesis  TokenRune  = ')'
	TokenOpenCurlyBracket  TokenRune  = '{'
	TokenCloseCurlyBracket TokenRune  = '}'
	TokenComma             TokenRune  = ','
	TokenEquals            TokenRune  = '='
	TokenAdd               TokenRune  = '+'
)

// TokenType represents the type of a token.
type TokenType int

// Constants for token types.
const (
	TokenWhileType TokenType = iota
	TokenLetType
	TokenFunctionType
	TokenOpenParenthesisType
	TokenCloseParenthesisType
	TokenOpenCurlyBracketType
	TokenCloseCurlyBracketType
	TokenIdentifierType
	TokenEqualsType
	TokenInteger32Type
	TokenAddType
	TokenUnknown
)

// Token represents a token with its type and value.
type Token struct {
	Type  TokenType
	Value string
}

// String method returns the string representation of a token.
func (t Token) String() string {
	switch t.Type {
	case TokenWhileType:
		return string(TokenWhile)
	case TokenLetType:
		return string(TokenLet)
	case TokenFunctionType:
		return string(TokenFunction)
	case TokenOpenParenthesisType:
		return string(TokenOpenParenthesis)
	case TokenCloseParenthesisType:
		return string(TokenCloseParenthesis)
	case TokenOpenCurlyBracketType:
		return string(TokenOpenCurlyBracket)
	case TokenCloseCurlyBracketType:
		return string(TokenCloseCurlyBracket)
	case TokenInteger32Type:
		return string(TokenInteger32)
	case TokenAddType:
		return string(TokenAdd)
	case TokenIdentifierType:
		return fmt.Sprintf("identifier(%s)", t.Value)
	default:
		return fmt.Sprintf("unknown(%s)", t.Value)
	}
}

// isBracket checks if the given rune is a parenthesis.
func isBracket(r TokenRune) bool {
	return r == TokenOpenParenthesis || r == TokenCloseParenthesis
}

// isCurly checks if the given rune is a curly bracket.
func isCurly(r TokenRune) bool {
	return r == TokenOpenCurlyBracket || r == TokenCloseCurlyBracket
}

// isComma checks if the given rune is a comma.
func isComma(r TokenRune) bool {
	return r == TokenComma
}

// isEqual checks if the given rune is an equals sign.
func isEqual(r TokenRune) bool {
	return r == TokenEquals
}

// isAdd checks if the given rune is an add sign.
func isAdd(r TokenRune) bool {
	return r == TokenAdd
}

// Tokenize function converts the input string into a slice of tokens
func Tokenize(input string) []Token {
	tokens := make([]Token, 0)

	var sb strings.Builder
	for _, r := range input {

		// Handle comma-separated tokens or tokens separated by add sign token
		if isComma(TokenRune(r)) || isAdd(TokenRune(r)) {
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
			case TokenAdd:
				tokens = append(tokens, Token{Type: TokenAddType})
			}
			continue
		} else if isEqual(TokenRune(r)) {
			// Handle equal sign tokens
			tokens = append(tokens, Token{Type: TokenEqualsType})
		} else if unicode.IsSpace(r) {
			// Handle whitespace-separated tokens
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
			// Handle curly brackets as tokens
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
			case TokenOpenParenthesis:
				tokens = append(tokens, Token{Type: TokenOpenParenthesisType})
			case TokenCloseParenthesis:
				tokens = append(tokens, Token{Type: TokenCloseParenthesisType})
			case TokenOpenCurlyBracket:
				tokens = append(tokens, Token{Type: TokenOpenCurlyBracketType})
			case TokenCloseCurlyBracket:
				tokens = append(tokens, Token{Type: TokenCloseCurlyBracketType})
			}
		} else if isBracket(TokenRune(r)) {
			// Handle curly parentheses
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
			case TokenOpenParenthesis:
				tokens = append(tokens, Token{Type: TokenOpenParenthesisType})
			case TokenCloseParenthesis:
				tokens = append(tokens, Token{Type: TokenCloseParenthesisType})
			}
		} else {
			// Accumulate non-special characters into a word
			sb.WriteRune(r)
		}

	}

	// Process the last word if any
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
