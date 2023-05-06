package lang

import (
	"fmt"
	"strconv"
)

type Node interface {
	IsNode()
}

type LetNode struct {
	Identifier string
	Value      any
}

func (n *LetNode) IsNode() {}

type Parameter struct {
	Identifier string
}

func (n *Parameter) IsNode() {}

type WhileNode struct {
	Condition string
	Body      []Node
}

func (n *WhileNode) IsNode() {}

type FunctionNode struct {
	Name       string
	Parameters []Node
	Body       []Node
}

type CallerNode struct {
	FunctionName string
	Parameters   []*Parameter
}

func (n *CallerNode) IsNode() {}

func (n *FunctionNode) IsNode() {}

func Parse(tokens []Token) ([]Node, error) {
	nodes, _, err := parseNodes(tokens, 0, -1)
	return nodes, err
}

func parseNodes(tokens []Token, index int, tokenType TokenType) ([]Node, int, error) {
	nodes := []Node{}

	for index < len(tokens) {
		token := tokens[index]

		switch token.Type {
		case TokenIdentifier:
			if index+1 < len(tokens) && tokens[index+1].Type != TokenOpenBracket {
				return nil, -1, fmt.Errorf("expected '(' after caller at position %d", index)
			}
			name := tokens[index].Value
			index++
			index++

			var parameters []*Parameter
			for {
				if index < len(tokens) && tokens[index].Type == TokenIdentifier {
					parameters = append(parameters, &Parameter{Identifier: tokens[index].Value})
					index++
				} else {
					break
				}
			}

			if index < len(tokens) && tokens[index].Type != TokenCloseBracket {
				return nil, -1, fmt.Errorf("expected ')' after parameters at position %d", index)
			}
			index++

			nodes = append(nodes, &CallerNode{FunctionName: name, Parameters: parameters})

		case TokenCloseCurly:
			if tokenType == TokenFunction {
				return nodes, index, nil
			}
			index++
		case TokenLet:
			if index+1 < len(tokens) && tokens[index+1].Type == TokenIdentifier {
				if index >= len(tokens) || tokens[index+2].Type != TokenEquals {
					return nil, -1, fmt.Errorf("expected '=' after let at position %d", index)
				}

				intValue, err := strconv.Atoi(tokens[index+3].Value)
				if err != nil {
					return nil, -1, fmt.Errorf("expected 'int' after equal condition at position %d", index)
				}

				nodes = append(nodes, &LetNode{
					Identifier: tokens[index+1].Value,
					Value:      intValue,
				})

				index += 4
			} else {
				return nil, -1, fmt.Errorf("expected identifier after 'let' at position %d", index)
			}
		case TokenWhile:
			if index+1 < len(tokens) && tokens[index+1].Type == TokenOpenBracket {
				condition := tokens[index+2].Value
				index += 3

				if index >= len(tokens) || tokens[index].Type != TokenCloseBracket {
					return nil, -1, fmt.Errorf("expected ')' after while condition at position %d", index)
				}
				index++

				if index >= len(tokens) || tokens[index].Type != TokenOpenCurly {
					return nil, -1, fmt.Errorf("expected '{' after while condition at position %d", index)
				}
				index++

				body, newIndex, err := parseNodes(tokens[index:], 0, -1)
				if err != nil {
					return nil, -1, err
				}
				index += newIndex

				if index >= len(tokens) || tokens[index].Type != TokenCloseCurly {
					return nil, -1, fmt.Errorf("expected '}' after while body at position %d", index)
				}
				index++

				nodes = append(nodes, &WhileNode{Condition: condition, Body: body})
			} else {
				return nil, -1, fmt.Errorf("expected '(' after 'while' at position %d", index)
			}
		case TokenFunction:
			functionNodes, newIndex, err := parseFunction(tokens, index)
			if err != nil {
				return nil, -1, err
			}
			index = newIndex
			nodes = append(nodes, functionNodes...)
		default:
			index++
		}
	}

	return nodes, index, nil
}

// parseFunction takes a slice of tokens and an index as input parameters and
// returns a slice of nodes, an updated index, and an error if there is any issue
// during parsing. It processes tokens to generate a FunctionNode with its parameters
// and body.
func parseFunction(tokens []Token, index int) ([]Node, int, error) {
	// Check if the next token is an identifier
	if index+1 < len(tokens) && tokens[index+1].Type == TokenIdentifier {
		index++
		name := tokens[index].Value
		index++

		// Check if the next token is an open bracket '('
		if index < len(tokens) && tokens[index].Type != TokenOpenBracket {
			return nil, -1, fmt.Errorf("expected '(' after function parameters at position %d", index)
		}
		index++

		// Initialize parameters slice
		var parameters []Node
		// Parse function parameters
		for {
			if index < len(tokens) && tokens[index].Type == TokenIdentifier {
				parameters = append(parameters, &Parameter{Identifier: tokens[index].Value})
				index++
			} else {
				break
			}
		}

		// Check if the next token is a close bracket ')'
		if index < len(tokens) && tokens[index].Type != TokenCloseBracket {
			return nil, -1, fmt.Errorf("expected ')' after function parameters at position %d", index)
		}
		index++

		// Check if the next token is an open curly brace '{'
		if index >= len(tokens) || tokens[index].Type != TokenOpenCurly {
			return nil, -1, fmt.Errorf("expected '{' after function parameters at position %d", index)
		}
		index++

		// Parse the function body
		body, newIndex, err := parseNodes(tokens[index:], 0, TokenFunction)
		if err != nil {
			return nil, -1, err
		}
		index += newIndex

		// Check if the next token is a close curly brace '}'
		if index >= len(tokens) || tokens[index].Type != TokenCloseCurly {
			return nil, -1, fmt.Errorf("expected '}' after function body at position %d", index)
		}
		index++

		// Create a FunctionNode with the parsed information
		nodes := []Node{&FunctionNode{Name: name, Parameters: parameters, Body: body}}
		return nodes, index, nil
	} else {
		return nil, -1, fmt.Errorf("expected identifier after 'function' at position %d", index)
	}
}
