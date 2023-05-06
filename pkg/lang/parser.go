package lang

import (
	"fmt"
	"strconv"
)

// Node is an interface representing nodes in the abstract syntax tree.
type Node interface {
	IsNode()
}

// LetNode represents a let statement.
type LetNode struct {
	Identifier string
	Value      any
}

// IsNode is an empty method to satisfy the Node interface.
func (n *LetNode) IsNode() {}

// Parameter represents a parameter in a function or function call.
type Parameter struct {
	Identifier string
}

// IsNode is an empty method to satisfy the Node interface.
func (n *Parameter) IsNode() {}

// WhileNode represents a while loop.
type WhileNode struct {
	Condition string
	Body      []Node
}

// IsNode is an empty method to satisfy the Node interface.
func (n *WhileNode) IsNode() {}

// FunctionNode represents a function definition.
type FunctionNode struct {
	Name       string
	Parameters []Node
	Body       []Node
}

// CallerNode represents a function call.
type CallerNode struct {
	FunctionName string
	Parameters   []*Parameter
}

// IsNode is an empty method to satisfy the Node interface.
func (n *CallerNode) IsNode() {}

// IsNode is an empty method to satisfy the Node interface.
func (n *FunctionNode) IsNode() {}

// Parse takes a slice of tokens as input and returns a slice of nodes
// representing the abstract syntax tree.
func Parse(tokens []Token) ([]Node, error) {
	nodes, _, err := parseNodes(tokens, 0, -1)
	return nodes, err
}

// parseNodes takes a slice of tokens, an index, and a token type as input parameters,
// and returns a slice of nodes, an updated index, and an error if there is any issue
// during parsing. It processes tokens to generate nodes representing the abstract syntax tree.
func parseNodes(tokens []Token, index int, tokenType TokenType) ([]Node, int, error) {
	nodes := []Node{}

	for index < len(tokens) {
		token := tokens[index]

		switch token.Type {
		case TokenIdentifier:
			callerNode, newIndex, err := parseCaller(tokens, index)
			if err != nil {
				return nil, -1, err
			}
			index = newIndex
			nodes = append(nodes, callerNode)
		case TokenCloseCurly:
			if tokenType == TokenFunction {
				return nodes, index, nil
			}
			index++
		case TokenLet:
			letNode, newIndex, err := parseLet(tokens, index)
			if err != nil {
				return nil, -1, err
			}
			index = newIndex
			nodes = append(nodes, letNode)
		case TokenWhile:
			whileNode, newIndex, err := parseWhile(tokens, index)
			if err != nil {
				return nil, -1, err
			}
			index = newIndex
			nodes = append(nodes, whileNode)
		case TokenFunction:
			functionNode, newIndex, err := parseFunction(tokens, index)
			if err != nil {
				return nil, -1, err
			}
			index = newIndex
			nodes = append(nodes, functionNode)
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
func parseFunction(tokens []Token, index int) (Node, int, error) {
	// Ensure there is a token following the 'function' keyword
	if index+1 >= len(tokens) || tokens[index+1].Type != TokenIdentifier {
		return nil, -1, fmt.Errorf("expected identifier after 'function' at position %d", index)
	}
	index++
	name := tokens[index].Value
	index++

	// Ensure the next token is an open bracket '('
	if index >= len(tokens) || tokens[index].Type != TokenOpenBracket {
		return nil, -1, fmt.Errorf("expected '(' after function name at position %d", index)
	}
	index++

	// Initialize parameters slice and parse function parameters
	var parameters []Node
	for {
		if index < len(tokens) && tokens[index].Type == TokenIdentifier {
			parameters = append(parameters, &Parameter{Identifier: tokens[index].Value})
			index++
		} else {
			break
		}
	}

	// Ensure the next token is a close bracket ')'
	if index >= len(tokens) || tokens[index].Type != TokenCloseBracket {
		return nil, -1, fmt.Errorf("expected ')' after function parameters at position %d", index)
	}
	index++

	// Ensure the next token is an open curly brace '{'
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

	// Ensure the next token is a close curly brace '}'
	if index >= len(tokens) || tokens[index].Type != TokenCloseCurly {
		return nil, -1, fmt.Errorf("expected '}' after function body at position %d", index)
	}
	index++

	// Create a FunctionNode with the parsed information
	return &FunctionNode{Name: name, Parameters: parameters, Body: body}, index, nil
}

// parseWhile takes a slice of tokens and an index as input parameters and
// returns a WhileNode, an updated index, and an error if there is any issue
// during parsing. It processes tokens to generate a WhileNode with its
// condition and body.
func parseWhile(tokens []Token, index int) (*WhileNode, int, error) {
	// Ensure the next token is an open bracket '('
	if index+1 >= len(tokens) || tokens[index+1].Type != TokenOpenBracket {
		return nil, -1, fmt.Errorf("expected '(' after 'while' at position %d", index)
	}
	condition := tokens[index+2].Value
	index += 3

	// Ensure the next token is a close bracket ')'
	if index >= len(tokens) || tokens[index].Type != TokenCloseBracket {
		return nil, -1, fmt.Errorf("expected ')' after while condition at position %d", index)
	}
	index++

	// Ensure the next token is an open curly brace '{'
	if index >= len(tokens) || tokens[index].Type != TokenOpenCurly {
		return nil, -1, fmt.Errorf("expected '{' after while condition at position %d", index)
	}
	index++

	// Parse the while loop body
	body, newIndex, err := parseNodes(tokens[index:], 0, -1)
	if err != nil {
		return nil, -1, err
	}
	index += newIndex

	// Ensure the next token is a close curly brace '}'
	if index >= len(tokens) || tokens[index].Type != TokenCloseCurly {
		return nil, -1, fmt.Errorf("expected '}' after while body at position %d", index)
	}
	index++

	// Create a WhileNode with the parsed condition and body
	whileNode := &WhileNode{Condition: condition, Body: body}
	return whileNode, index, nil
}

// parseLet takes a slice of tokens and an index as input parameters and
// returns a LetNode, an updated index, and an error if there is any issue
// during parsing. It processes tokens to generate a LetNode with its
// identifier and value.
func parseLet(tokens []Token, index int) (*LetNode, int, error) {
	// Ensure the next token is an identifier
	if index+1 >= len(tokens) || tokens[index+1].Type != TokenIdentifier {
		return nil, -1, fmt.Errorf("expected identifier after 'let' at position %d", index)
	}

	// Ensure the next token is an equals sign '='
	if index+2 >= len(tokens) || tokens[index+2].Type != TokenEquals {
		return nil, -1, fmt.Errorf("expected '=' after let at position %d", index)
	}

	// Parse the integer value after the equals sign
	intValue, err := strconv.Atoi(tokens[index+3].Value)
	if err != nil {
		return nil, -1, fmt.Errorf("expected 'int' after equal condition at position %d", index)
	}

	// Create a LetNode with the parsed identifier and value
	letNode := &LetNode{
		Identifier: tokens[index+1].Value,
		Value:      intValue,
	}

	// Update the index to skip the processed tokens
	index += 4

	return letNode, index, nil
}

// parseCaller takes a slice of tokens and an index as input parameters and
// returns a CallerNode, an updated index, and an error if there is any issue
// during parsing. It processes tokens to generate a CallerNode with its
// function name and parameters.
func parseCaller(tokens []Token, index int) (*CallerNode, int, error) {
	// Ensure the next token is an open bracket '('
	if index+1 >= len(tokens) || tokens[index+1].Type != TokenOpenBracket {
		return nil, -1, fmt.Errorf("expected '(' after caller at position %d", index)
	}

	// Retrieve the function name from the current token
	name := tokens[index].Value
	index += 2

	// Initialize parameters slice
	var parameters []*Parameter

	// Parse the function parameters
	for {
		if index < len(tokens) && tokens[index].Type == TokenIdentifier {
			parameters = append(parameters, &Parameter{Identifier: tokens[index].Value})
			index++
		} else {
			break
		}
	}

	// Ensure the next token is a close bracket ')'
	if index >= len(tokens) || tokens[index].Type != TokenCloseBracket {
		return nil, -1, fmt.Errorf("expected ')' after parameters at position %d", index)
	}
	index++

	// Create a CallerNode with the parsed function name and parameters
	callerNode := &CallerNode{FunctionName: name, Parameters: parameters}

	return callerNode, index, nil
}
