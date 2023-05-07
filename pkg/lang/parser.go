package lang

import (
	"fmt"
	"strconv"
)

// dataType represents the underlying data type of a value.
type dataType int

// Constants for different data types.
const (
	// Integer32Type represents the 32-bit integer data type.
	Integer32Type dataType = iota
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

// AddOperationNode represents a add statement.
type AddOperationNode struct {
	LeftValue  any
	RightValue any
}

// IsNode is an empty method to satisfy the Node interface.
func (n *AddOperationNode) IsNode() {}

// Parameter represents a parameter in a function or function call.
type Parameter struct {
	Identifier string
	Type       dataType
	Value      any
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
	Parameters []*Parameter
	Body       []Node
}

// IsNode is an empty method to satisfy the Node interface.
func (n *FunctionNode) IsNode() {}

// CallerNode represents a function call.
type CallerNode struct {
	FunctionName         string
	Parameters           []*Parameter
	isParameterOperation bool
	AddOperationNode     *AddOperationNode
}

// IsNode is an empty method to satisfy the Node interface.
func (n *CallerNode) IsNode() {}

// ForNode represents a for definition.
// example: for i := 0; i < 10; i++ {}
type ForNode struct {
	Init      ShortVariableAssigmentNode
	Condition ConditionNode
	Post      PostNode
	Body      []Node
}

// IsNode is an empty method to satisfy the Node interface.
func (n *ForNode) IsNode() {}

// ShortVariableAssigmentNode represents a short variable assignment statement.
type ShortVariableAssigmentNode struct {
	Identifier string
	Value      any
}

// IsNode is an empty method to satisfy the Node interface.
func (n *ShortVariableAssigmentNode) IsNode() {}

// ConditionNode represents a condition of for node
type ConditionNode struct {
	LeftValue  string
	Operator   any
	RightValue any
}

type LessThanOperator struct{}

// IsNode is an empty method to satisfy the Node interface.
func (n *ConditionNode) IsNode() {}

// PostNode represents a post statement of for node
type PostNode struct {
	Identifier string
	Increment  bool
}

// IsNode is an empty method to satisfy the Node interface.
func (n *PostNode) IsNode() {}

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
		case TokenIdentifierType:
			if IsOpenParenthesisToken(index+1, tokens) {
				callerNode, newIndex, err := parseCaller(tokens, index)
				if err != nil {
					return nil, -1, err
				}
				index = newIndex
				nodes = append(nodes, callerNode)
			} else if IsAddToken(index+1, tokens) {
				addOperationNode, newIndex, err := parseAddOperation(tokens, index)
				if err != nil {
					return nil, -1, err
				}
				index = newIndex
				nodes = append(nodes, addOperationNode)
			}
		case TokenCloseCurlyBracketType:
			if tokenType == TokenFunctionType {
				return nodes, index, nil
			} else if tokenType == TokenForType {
				return nodes, index, nil
			}
			index++
		case TokenLetType:
			letNode, newIndex, err := parseLet(tokens, index)
			if err != nil {
				return nil, -1, err
			}
			index = newIndex
			nodes = append(nodes, letNode)
		case TokenWhileType:
			whileNode, newIndex, err := parseWhile(tokens, index)
			if err != nil {
				return nil, -1, err
			}
			index = newIndex
			nodes = append(nodes, whileNode)
		case TokenFunctionType:
			functionNode, newIndex, err := parseFunction(tokens, index)
			if err != nil {
				return nil, -1, err
			}
			index = newIndex
			nodes = append(nodes, functionNode)
		case TokenForType:
			forNode, newIndex, err := parseFor(tokens, index)
			if err != nil {
				return nil, -1, err
			}
			index = newIndex
			nodes = append(nodes, forNode)
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
	index++
	if IsNotIdentifierToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected identifier after 'function' at position %d", index)
	}
	name := tokens[index].Value
	// Ensure the next token is an open bracket '('
	index++
	if IsNotOpenParenthesisToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected '(' after function name at position %d", index)
	}
	index++

	// Initialize parameters slice and parse function parameters
	var parameters []*Parameter
	for {
		if IsIdentifierToken(index, tokens) {
			var p = &Parameter{Identifier: tokens[index].Value}

			index++
			if IsInteger32Token(index, tokens) {
				return nil, -1, fmt.Errorf("expected 'i32' after function parameter at position %d", index)
			}
			p.Type = Integer32Type
			parameters = append(parameters, p)
			index++
		} else {
			break
		}
	}

	// Ensure the next token is a close bracket ')'
	if IsNotCloseParenthesisToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected ')' after function parameters at position %d", index)
	}
	index++

	// Ensure the next token is an open curly brace '{'
	if IsNotOpenCurlyBracketToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected '{' after function parameters at position %d", index)
	}
	index++

	// Parse the function body
	body, newIndex, err := parseNodes(tokens[index:], 0, TokenFunctionType)
	if err != nil {
		return nil, -1, err
	}
	index += newIndex

	// Ensure the next token is a close curly brace '}'
	if IsNotCloseCurlyBracketToken(index, tokens) {
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
	index++
	if IsNotOpenParenthesisToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected '(' after 'while' at position %d", index)
	}
	condition := tokens[index+2].Value
	index += 2

	// Ensure the next token is a close bracket ')'
	if IsNotCloseParenthesisToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected ')' after while condition at position %d", index)
	}
	index++

	// Ensure the next token is an open curly brace '{'
	if IsNotOpenCurlyBracketToken(index, tokens) {
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
	if IsNotCloseCurlyBracketToken(index, tokens) {
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
	index++
	if IsNotIdentifierToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected identifier after 'let' at position %d", index)
	}
	name := tokens[index].Value
	index++

	// Ensure the next token is an equals sign '='
	if IsNotEqualToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected '=' after let at position %d", index)
	}
	index++

	// Parse the integer value after the equals sign
	intValue, err := strconv.Atoi(tokens[index].Value)
	if err != nil {
		return nil, -1, fmt.Errorf("expected 'int' after equal condition at position %d", index)
	}
	index++

	// Create a LetNode with the parsed identifier and value
	letNode := &LetNode{
		Identifier: name,
		Value:      int32(intValue),
	}

	return letNode, index, nil
}

// parseCaller takes a slice of tokens and an index as input parameters and
// returns a CallerNode, an updated index, and an error if there is any issue
// during parsing. It processes tokens to generate a CallerNode with its
// function name and parameters.
func parseCaller(tokens []Token, index int) (*CallerNode, int, error) {
	// Retrieve the function name from the current token
	name := tokens[index].Value

	// Ensure the next token is an open bracket '('
	index++
	if IsNotOpenParenthesisToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected '(' after caller at position %d", index)
	}
	index++

	// Initialize parameters slice
	var parameters []*Parameter

	// Parse the function parameters
	var isParameterOperation bool
	var addOperationNode *AddOperationNode
	if IsNotAddToken(index+1, tokens) {
		for {
			if IsIdentifierToken(index, tokens) {

				intValue, err := strconv.Atoi(tokens[index].Value)
				if err == nil {
					parameters = append(parameters, &Parameter{Value: int32(intValue), Type: Integer32Type, Identifier: tokens[index].Value})
				} else {
					parameters = append(parameters, &Parameter{Value: tokens[index].Value, Identifier: tokens[index].Value})
				}

				index++
			} else {
				break
			}
		}
	} else {
		var err error
		addOperationNode, _, err = parseAddOperation(tokens, index)
		if err != nil {
			return nil, -1, err
		}

		index += 3
		isParameterOperation = true
	}

	// Ensure the next token is a close bracket ')'
	if IsNotCloseParenthesisToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected ')' after parameters at position %d", index)
	}
	index++

	// Create a CallerNode with the parsed function name and parameters
	callerNode := &CallerNode{FunctionName: name, Parameters: parameters, isParameterOperation: isParameterOperation, AddOperationNode: addOperationNode}

	return callerNode, index, nil
}

func parseAddOperation(tokens []Token, index int) (*AddOperationNode, int, error) {
	// Ensure the next token is an identifier
	if IsNotIdentifierToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected 'identifier' at start %d", index)
	}

	// Parse the integer value
	leftValue, err := strconv.Atoi(tokens[index].Value)
	if err != nil {
		return nil, -1, fmt.Errorf("expected 'int' as value at position %d", index)
	}
	index++

	// Ensure the next token is an add sign
	if IsNotAddToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected 'add sign' after 'identifier' at position %d", index)
	}
	index++
	// Ensure the next token is an identifier
	if IsNotIdentifierToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected 'identifier' after 'add sign' at position %d", index)
	}

	// Parse the integer value
	rightValue, err := strconv.Atoi(tokens[index].Value)
	if err != nil {
		return nil, -1, fmt.Errorf("expected 'int' as value at position %d", index)
	}
	index++

	// Create a AddOperationNode with the parsed left and right values
	addOperationNode := &AddOperationNode{
		LeftValue:  int32(leftValue),
		RightValue: int32(rightValue),
	}

	return addOperationNode, index, nil
}

func parseFor(tokens []Token, index int) (*ForNode, int, error) {

	if IsNotForToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected 'for' at start %d", index)
	}
	index++

	if IsNotIdentifierToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected identifier after 'for' at position %d", index)
	}

	shortVariableAssigmentName := tokens[index].Value
	index++

	if IsNotShortVariableAssigmentToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected := after 'identifier' at position %d", index)
	}
	index++

	if IsNotIdentifierToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected identifier after ':=' at position %d", index)
	}

	// Parse the integer value
	shortVariableAssigmentRightValue, err := strconv.Atoi(tokens[index].Value)
	if err != nil {
		return nil, -1, fmt.Errorf("expected 'int' as value at position %d", index)
	}

	index++

	if IsNotSemicolonToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected ; after 'value' at position %d", index)
	}

	index++

	if IsNotIdentifierToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected identifier after ';' at position %d", index)
	}

	conditionLeftValue := tokens[index].Value
	index++

	if IsNotLessThanToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected < after 'idenfifier' at position %d", index)
	}
	operator := LessThanOperator{}
	index++

	// Parse the integer value
	conditionRightValue, err := strconv.Atoi(tokens[index].Value)
	if err != nil {
		return nil, -1, fmt.Errorf("expected 'int' as value at position %d", index)
	}

	index++
	if IsNotSemicolonToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected ; after 'value' at position %d", index)
	}

	index++
	if IsNotIdentifierToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected identifier after ';' at position %d", index)
	}

	postIdentifier := tokens[index].Value

	index++
	// Ensure the next token is an add sign
	if IsNotAddToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected 'add sign' after 'identifier' at position %d", index)
	}
	index++

	// Ensure the next token is an add sign
	if IsNotAddToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected 'add sign' after 'add sign' at position %d", index)
	}

	index++

	forNode := &ForNode{
		Init: ShortVariableAssigmentNode{
			Identifier: shortVariableAssigmentName,
			Value:      int32(shortVariableAssigmentRightValue),
		},
		Condition: ConditionNode{
			LeftValue:  conditionLeftValue,
			Operator:   operator,
			RightValue: int32(conditionRightValue),
		},
		Post: PostNode{
			Identifier: postIdentifier,
			Increment:  true,
		},
	}

	// Ensure the next token is an open curly brace '{'
	if IsNotOpenCurlyBracketToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected '{' after function parameters at position %d", index)
	}
	index++

	// Parse the function body
	body, newIndex, err := parseNodes(tokens[index:], 0, TokenForType)
	if err != nil {
		return nil, -1, err
	}
	index += newIndex

	// Ensure the next token is a close curly brace '}'
	if IsNotCloseCurlyBracketToken(index, tokens) {
		return nil, -1, fmt.Errorf("expected '}' after function body at position %d", index)
	}
	index++

	forNode.Body = body

	return forNode, index, nil
}

// IsNotLessThanToken checks if the token at the given index is not a less than or if the index is out of bounds.
func IsNotLessThanToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenLessThanType
}

// IsNotSemicolonToken checks if the token at the given index is not a semicolon or if the index is out of bounds.
func IsNotSemicolonToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenSemicolonType
}

// IsNotShortVariableAssigmentToken checks if the token at the given index is not a short variable assigment or if the index is out of bounds.
func IsNotShortVariableAssigmentToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenShortVariableAssignmentType
}

// IsNotForToken checks if the token at the given index is not a for or if the index is out of bounds.
func IsNotForToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenForType
}

// IsOpenParenthesisToken checks if the token at the given index is an open parenthesis or if the index is out of bounds.
func IsOpenParenthesisToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type == TokenOpenParenthesisType
}

// IsNotOpenParenthesisToken checks if the token at the given index is not an open parenthesis or if the index is out of bounds.
func IsNotOpenParenthesisToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenOpenParenthesisType
}

// IsNotCloseParenthesisToken checks if the token at the given index is not a close parenthesis or if the index is out of bounds.
func IsNotCloseParenthesisToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenCloseParenthesisType
}

// IsNotOpenCurlyBracketToken checks if the token at the given index is not an open curly bracket or if the index is out of bounds.
func IsNotOpenCurlyBracketToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenOpenCurlyBracketType
}

// IsNotCloseCurlyBracketToken checks if the token at the given index is not a close curly bracket or if the index is out of bounds.
func IsNotCloseCurlyBracketToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenCloseCurlyBracketType
}

// IsIdentifierToken checks if the token at the given index is an identifier or if the index is out of bounds.
func IsIdentifierToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type == TokenIdentifierType
}

// IsNotIdentifierToken checks if the token at the given index is not an identifier or if the index is out of bounds.
func IsNotIdentifierToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenIdentifierType
}

// IsNotEqualToken checks if the token at the given index is not an equal sign or if the index is out of bounds.
func IsNotEqualToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenEqualsType
}

// IsInteger32Token checks if the token at the given index is an integer32 or if the index is out of bounds.
func IsInteger32Token(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenInteger32Type
}

// IsNotAddToken checks if the token at the given index is not an add sign or if the index is out of bounds.
func IsNotAddToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type != TokenAddType
}

// IsAddToken checks if the token at the given index is an add sign or if the index is out of bounds.
func IsAddToken(currentIndex int, tokens []Token) bool {
	return currentIndex >= len(tokens) || tokens[currentIndex].Type == TokenAddType
}
