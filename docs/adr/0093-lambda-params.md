# ADR 0093: fix lambda parameter parsing (body `:` vs type annotation)

## Decision

Fix the lambda parser so `lambda x: x + 1` parses (it previously errored with
"unknown type annotation x": `parseParam()` greedily treated the `:` body
separator as a `: type` annotation).

Lambda params are now parsed as **plain names** with an optional annotation,
using a lookahead rule:

- Read the param Name token.
- If the next token is `:` **and** the token after it is a known type keyword
  (`int`, `float`, `bool`, `str`, `any` — via the new `isTypeName` helper),
  consume the `: type` annotation via `parseTypeAnnot()`.
- Otherwise the `:` is the lambda body separator (do not consume it).

This supports both the common `lambda x: body` and the annotated
`lambda x: int: body` shapes.

## Motivation

Lambda is a mission language feature; `lambda x: expr` (the most common shape)
failed to parse, so the feature was unusable for named single-param lambdas.

## Tests

`pkg/lang/stdlib_test.go` (`TestLambdaParams`):

- `f = lambda x: x + 1; f(2)` -> 3.
- Annotated `f = lambda x: int: x * 2; f(3)` -> 6.

## Alternatives rejected

- Parsing lambda params via `parseParam()` unchanged: rejected — it cannot
  distinguish the body `:` from an annotation without lookahead.
