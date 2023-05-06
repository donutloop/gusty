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
			if index+1 < len(tokens) && tokens[index+1].Type == TokenIdentifier {
				index++
				name := tokens[index].Value
				index++

				if index < len(tokens) && tokens[index].Type != TokenOpenBracket {
					return nil, -1, fmt.Errorf("expected '(' after function parameters at position %d", index)
				}
				index++

				var parameters []Node
				for {
					if index < len(tokens) && tokens[index].Type == TokenIdentifier {
						parameters = append(parameters, &Parameter{Identifier: tokens[index].Value})
						index++
					} else {
						break
					}
				}

				if index < len(tokens) && tokens[index].Type != TokenCloseBracket {
					return nil, -1, fmt.Errorf("expected ')' after function parameters at position %d", index)
				}
				index++

				if index >= len(tokens) || tokens[index].Type != TokenOpenCurly {
					return nil, -1, fmt.Errorf("expected '{' after function parameters at position %d", index)
				}
				index++

				body, newIndex, err := parseNodes(tokens[index:], 0, TokenFunction)
				if err != nil {
					return nil, -1, err
				}
				index += newIndex

				if index >= len(tokens) || tokens[index].Type != TokenCloseCurly {
					return nil, -1, fmt.Errorf("expected '}' after function body at position %d", index)
				}
				index++

				nodes = append(nodes, &FunctionNode{Name: name, Parameters: parameters, Body: body})
			} else {
				return nil, -1, fmt.Errorf("expected identifier after 'function' at position %d", index)
			}
		default:
			index++
		}
	}

	return nodes, index, nil
}
