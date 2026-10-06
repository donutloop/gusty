package lang

// ASTIRSchema is the machine-readable JSON Schema (draft-07) describing the
// AST dump emitted by `--emit-ast`. It is the self-describing contract for
// agents consuming gusty's structured output: given a JSON object, an agent
// can validate it against this schema to confirm it is a well-formed Program
// AST before planning a compilation.
//
// The IR dump emitted by `--emit-llvm` is LLVM IR text, not JSON; it is
// described here as a `text/plain` content type (see definitions.irDump) so
// the same document covers both structured outputs.
//
// Node kinds use a `kind` discriminator where present; otherwise the node is
// identified by its required field names (Go's default struct JSON tags).
//
// The document also describes the other structured outputs an agent consumes:
// `definitions.diagnostic` (the `code`/`suggestion` fields carried by
// --verify/--check JSON) and `definitions.varianceRule` (one row of the
// self-describing variance table printed by `gustyc --variance`).
const ASTIRSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "$id": "https://gusty.local/schema/ast-ir.json",
  "title": "gusty AST & IR dump schema",
  "description": "Describes the JSON AST dump from --emit-ast and the LLVM IR text dump from --emit-llvm.",
  "type": "object",
  "required": [
    "stmts"
  ],
  "properties": {
    "stmts": {
      "type": "array",
      "items": {
        "$ref": "#/definitions/stmt"
      },
      "description": "Top-level statements of the parsed program, in order."
    }
  },
  "definitions": {
    "irDump": {
      "type": "string",
      "contentMediaType": "text/plain",
      "description": "LLVM IR text (--emit-llvm). Validated by llc/opt, not this schema.",
      "properties": {
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "stmt": {
      "oneOf": [
        {
          "$ref": "#/definitions/returnStmt"
        },
        {
          "$ref": "#/definitions/exprStmt"
        },
        {
          "$ref": "#/definitions/assignStmt"
        },
        {
          "$ref": "#/definitions/raiseStmt"
        },
        {
          "$ref": "#/definitions/ifStmt"
        },
        {
          "$ref": "#/definitions/whileStmt"
        },
        {
          "$ref": "#/definitions/forStmt"
        },
        {
          "$ref": "#/definitions/funcDef"
        },
        {
          "$ref": "#/definitions/classDef"
        },
        {
          "$ref": "#/definitions/matchStmt"
        },
        {
          "$ref": "#/definitions/tryStmt"
        },
        {
          "$ref": "#/definitions/importStmt"
        },
        {
          "$ref": "#/definitions/yieldStmt"
        },
        {
          "$ref": "#/definitions/breakStmt"
        },
        {
          "$ref": "#/definitions/passStmt"
        },
        {
          "$ref": "#/definitions/typeAliasStmt"
        },
        {
          "$ref": "#/definitions/continueStmt"
        }
      ],
      "properties": {
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "returnStmt": {
      "type": "object",
      "required": [
        "expr"
      ],
      "properties": {
        "expr": {
          "$ref": "#/definitions/expr"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "exprStmt": {
      "type": "object",
      "required": [
        "expr"
      ],
      "properties": {
        "expr": {
          "$ref": "#/definitions/expr"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "assignStmt": {
      "type": "object",
      "required": [
        "cond",
        "then"
      ],
      "properties": {
        "cond": {
          "$ref": "#/definitions/expr"
        },
        "then": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "elifs": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "else": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      },
      "augAssignStmt": {
        "type": "object",
        "required": [
          "target",
          "op",
          "value"
        ],
        "properties": {
          "target": {
            "$ref": "#/definitions/expr"
          },
          "op": {
            "type": "string"
          },
          "value": {
            "$ref": "#/definitions/expr"
          }
        }
      }
    },
    "whileStmt": {
      "type": "object",
      "required": [
        "cond",
        "body"
      ],
      "properties": {
        "cond": {
          "$ref": "#/definitions/expr"
        },
        "body": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "else": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "forStmt": {
      "type": "object",
      "required": [
        "var",
        "iter",
        "body"
      ],
      "properties": {
        "var": {
          "$ref": "#/definitions/expr"
        },
        "iter": {
          "$ref": "#/definitions/expr"
        },
        "body": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "else": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "funcDef": {
      "type": "object",
      "required": [
        "name",
        "params",
        "body"
      ],
      "properties": {
        "name": {
          "type": "string"
        },
        "params": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/param"
          }
        },
        "return_annot": {
          "$ref": "#/definitions/type"
        },
        "body": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "decorators": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/expr"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "param": {
      "type": "object",
      "required": [
        "name"
      ],
      "properties": {
        "name": {
          "type": "string"
        },
        "annot": {
          "$ref": "#/definitions/type"
        },
        "default": {
          "$ref": "#/definitions/expr"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "classDef": {
      "type": "object",
      "required": [
        "name",
        "body"
      ],
      "properties": {
        "name": {
          "type": "string"
        },
        "bases": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/expr"
          }
        },
        "body": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "matchStmt": {
      "type": "object",
      "required": [
        "subject",
        "cases"
      ],
      "properties": {
        "subject": {
          "$ref": "#/definitions/expr"
        },
        "cases": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/matchCase"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "matchCase": {
      "type": "object",
      "required": [
        "pattern",
        "body"
      ],
      "properties": {
        "pattern": {
          "$ref": "#/definitions/expr"
        },
        "body": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "tryStmt": {
      "type": "object",
      "required": [
        "body"
      ],
      "properties": {
        "body": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "excepts": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/exceptClause"
          }
        },
        "finally": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "exceptClause": {
      "type": "object",
      "required": [
        "body"
      ],
      "properties": {
        "exn": {
          "$ref": "#/definitions/expr"
        },
        "body": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/stmt"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "importStmt": {
      "type": "object",
      "required": [
        "module"
      ],
      "properties": {
        "module": {
          "type": "string"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "yieldStmt": {
      "type": "object",
      "required": [
        "expr"
      ],
      "properties": {
        "expr": {
          "$ref": "#/definitions/expr"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "breakStmt": {
      "type": "object",
      "properties": {
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "passStmt": {
      "type": "object",
      "properties": {
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "typeAliasStmt": {
      "type": "object",
      "description": "type NAME = <type-annotation>: a compile-time structural type alias (L5.7). Has no runtime effect.",
      "required": [
        "name",
        "annot"
      ],
      "properties": {
        "name": {
          "type": "string"
        },
        "annot": {
          "type": "object",
          "description": "The structurally-resolved annotation type the alias expands to."
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "continueStmt": {
      "type": "object",
      "properties": {
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "expr": {
      "oneOf": [
        {
          "$ref": "#/definitions/name"
        },
        {
          "$ref": "#/definitions/intLit"
        },
        {
          "$ref": "#/definitions/floatLit"
        },
        {
          "$ref": "#/definitions/boolLit"
        },
        {
          "$ref": "#/definitions/strLit"
        },
        {
          "$ref": "#/definitions/listLit"
        },
        {
          "$ref": "#/definitions/dictLit"
        },
        {
          "$ref": "#/definitions/setLit"
        },
        {
          "$ref": "#/definitions/binOp"
        },
        {
          "$ref": "#/definitions/unOp"
        },
        {
          "$ref": "#/definitions/condExpr"
        },
        {
          "$ref": "#/definitions/call"
        },
        {
          "$ref": "#/definitions/index"
        },
        {
          "$ref": "#/definitions/attr"
        },
        {
          "$ref": "#/definitions/lambda"
        },
        {
          "$ref": "#/definitions/comp"
        },
        {
          "$ref": "#/definitions/generator"
        }
      ],
      "properties": {
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    },
    "name": {
      "type": "object",
      "required": [
        "value"
      ],
      "properties": {
        "value": {
          "type": "string"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "intLit": {
      "type": "object",
      "required": [
        "value"
      ],
      "properties": {
        "value": {
          "type": "integer"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "floatLit": {
      "type": "object",
      "required": [
        "value"
      ],
      "properties": {
        "value": {
          "type": "number"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "boolLit": {
      "type": "object",
      "required": [
        "value"
      ],
      "properties": {
        "value": {
          "type": "boolean"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "strLit": {
      "type": "object",
      "required": [
        "value"
      ],
      "properties": {
        "value": {
          "type": "string"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "listLit": {
      "type": "object",
      "required": [
        "elems"
      ],
      "properties": {
        "elems": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/expr"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "dictLit": {
      "type": "object",
      "required": [
        "keys",
        "vals"
      ],
      "properties": {
        "keys": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/expr"
          }
        },
        "vals": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/expr"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "setLit": {
      "type": "object",
      "required": [
        "elems"
      ],
      "properties": {
        "elems": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/expr"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "binOp": {
      "type": "object",
      "required": [
        "op",
        "left",
        "right"
      ],
      "properties": {
        "op": {
          "type": "string"
        },
        "left": {
          "$ref": "#/definitions/expr"
        },
        "right": {
          "$ref": "#/definitions/expr"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "unOp": {
      "type": "object",
      "required": [
        "op",
        "x"
      ],
      "properties": {
        "op": {
          "type": "string"
        },
        "x": {
          "$ref": "#/definitions/expr"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "condExpr": {
      "type": "object",
      "required": [
        "if",
        "then",
        "else"
      ],
      "properties": {
        "if": {
          "$ref": "#/definitions/expr"
        },
        "then": {
          "$ref": "#/definitions/expr"
        },
        "else": {
          "$ref": "#/definitions/expr"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "call": {
      "type": "object",
      "required": [
        "fn",
        "args"
      ],
      "properties": {
        "fn": {
          "$ref": "#/definitions/expr"
        },
        "args": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/expr"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "index": {
      "type": "object",
      "required": [
        "obj",
        "idx"
      ],
      "properties": {
        "obj": {
          "$ref": "#/definitions/expr"
        },
        "idx": {
          "$ref": "#/definitions/expr"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "attr": {
      "type": "object",
      "required": [
        "obj",
        "name"
      ],
      "properties": {
        "obj": {
          "$ref": "#/definitions/expr"
        },
        "name": {
          "$ref": "#/definitions/expr"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "lambda": {
      "type": "object",
      "required": [
        "params",
        "body"
      ],
      "properties": {
        "params": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/param"
          }
        },
        "body": {
          "$ref": "#/definitions/expr"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "comp": {
      "type": "object",
      "required": [
        "kind",
        "elem",
        "iter"
      ],
      "properties": {
        "kind": {
          "type": "string"
        },
        "elem": {
          "$ref": "#/definitions/expr"
        },
        "iter": {
          "$ref": "#/definitions/expr"
        },
        "cond": {
          "$ref": "#/definitions/expr"
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "generator": {
      "type": "object",
      "required": [
        "elems"
      ],
      "properties": {
        "elems": {
          "type": "array",
          "items": {
            "$ref": "#/definitions/expr"
          }
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        },
        "inferred": {
          "type": "string",
          "description": "Inferred static type name (e.g. \"int\", \"list[int]\")."
        }
      }
    },
    "diagnostic": {
      "type": "object",
      "required": [
        "level",
        "msg"
      ],
      "description": "One diagnostic from --verify / --check / --json. 'code' is a STABLE rule identifier agents can branch on instead of matching on 'msg' prose; 'suggestion' is the actionable fix for the violated rule.",
      "properties": {
        "level": {
          "type": "string",
          "enum": [
            "info",
            "warning",
            "error"
          ]
        },
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the diagnostic."
        },
        "msg": {
          "type": "string"
        },
        "code": {
          "type": "string",
          "description": "Stable rule code (L6.6 variance + generics, and the classic mismatch cases).",
          "enum": [
            "type.mismatch",
            "type.variance.invariant",
            "type.variance.covariant",
            "type.variance.contravariant",
            "type.variance.nominal",
            "type.callable.arity",
            "type.union.members"
          ]
        },
        "suggestion": {
          "type": "string"
        }
      }
    },
    "varianceRule": {
      "type": "object",
      "required": [
        "constructor",
        "params",
        "variance",
        "rationale",
        "code"
      ],
      "description": "One row of the self-describing variance table (gustyc --variance): a generic constructor, its type parameters, the declared variance of each, and the diagnostic code the checker emits when the rule is broken.",
      "properties": {
        "constructor": {
          "type": "string"
        },
        "params": {
          "type": "array",
          "items": {
            "type": "string"
          }
        },
        "variance": {
          "type": "array",
          "items": {
            "type": "string",
            "enum": [
              "invariant",
              "covariant",
              "contravariant",
              "nominal"
            ]
          }
        },
        "mutable": {
          "type": "boolean"
        },
        "read_only": {
          "type": "string"
        },
        "rationale": {
          "type": "string"
        },
        "code": {
          "type": "string"
        }
      }
    },
    "effectSummary": {
      "type": "object",
      "required": [
        "function",
        "async",
        "line",
        "effects",
        "awaits",
        "yields",
        "raises",
        "coroutine_calls",
        "returns_value",
        "returns_bare",
        "falls_through",
        "terminates"
      ],
      "description": "One row of the self-describing effect table (gustyc --effects, or 'gustyc effects <file>...'; lang.EffectSummaries): what one function body actually does. The effect names are the vocabulary the L7.6 rules decide from, so the table and the async diagnostics can never disagree: a coroutine nobody awaits is async.coro.never_awaited, one awaited twice is async.coro.awaited_twice, and an 'async def' whose control flow can run off the end while promising a value is async.missing_return (roadmap Phase 7, ADR 0195). Function is the declaration path: outer.inner for a nested def, Class.method for a method, or <module> for the top level, whose awaits are the language's documented top-level coroutine.",
      "properties": {
        "function": {
          "type": "string",
          "description": "Declaration path: fetch, outer.inner, Box.load, or <module>."
        },
        "async": {
          "type": "boolean",
          "description": "true for an async def: calling it builds a coroutine and runs nothing until it is awaited."
        },
        "line": {
          "type": "integer",
          "description": "Line of the declaration (0 for <module>)."
        },
        "effects": {
          "type": "array",
          "items": {
            "type": "string",
            "enum": [
              "await",
              "yield",
              "raise"
            ]
          },
          "description": "Sorted effect names the body performs. Empty for a pure body."
        },
        "awaits": {
          "type": "integer",
          "description": "await sites in the body (a signature, not a profile: a loop body counts once)."
        },
        "yields": {
          "type": "integer",
          "description": "yield / yield from sites in the body. Non-zero under an async def is async.generator.unsupported."
        },
        "raises": {
          "type": "integer",
          "description": "raise sites in the body."
        },
        "coroutine_calls": {
          "type": "integer",
          "description": "Calls to an async def, i.e. coroutine constructions, from this body."
        },
        "returns_value": {
          "type": "boolean",
          "description": "Some path executes return with an expression."
        },
        "returns_bare": {
          "type": "boolean",
          "description": "Some path executes a bare return (which yields None)."
        },
        "falls_through": {
          "type": "boolean",
          "description": "Control flow can run off the end of the body, so the call — or the await — answers None. True for <module>: a file ends."
        },
        "terminates": {
          "type": "boolean",
          "description": "!falls_through: every path leaves the body through return, raise, break or continue. This is what the checker means by an exhaustive control flow."
        }
      }
    },
    "effectDocument": {
      "type": "object",
      "required": ["schema_version", "language_version", "generated_by", "source", "functions", "ok", "exit"],
      "description": "The document gustyc --effects <src> --json (and gustyc effects <file>... --json) prints: one effectSummary row per function plus the verdict the same source produced. It is the machine path for the await/return discipline (roadmap Phase 7, ADR 0195): the rows are the facts the async rules were decided from, and the diagnostics are those rules' verdicts, so one call answers both what a file does and whether it is honest.",
      "properties": {
        "schema_version": { "type": "string", "description": "Version of this document shape, currently \"1.0\"." },
        "language_version": { "type": "string", "description": "Compiler version that produced it (lang.Version)." },
        "generated_by": { "type": "string", "description": "Producer, always \"gustyc --effects\"." },
        "source": { "type": "string", "description": "Label for the source: the file path, or \"<src>\" for a source string." },
        "functions": { "type": "array", "items": { "$ref": "#/definitions/effectSummary" }, "description": "One effectSummary per function in declaration order, led by one row for <module>." },
        "diagnostics": { "type": "array", "items": { "$ref": "#/definitions/diagnostic" }, "description": "The diagnostics the source produced, async rules included — the same objects the --check document carries." },
        "ok": { "type": "boolean", "description": "true when no error-level diagnostic fired (warnings do not move it)." },
        "exit": { "type": "integer", "description": "The exit code the CLI returns for this document: 0 clean, 1 refused (docs/operations.md § Exit codes)." }
      }
    },
    "phaseTiming": {
      "type": "object",
      "required": ["phase", "ms"],
      "description": "One phase of the compilation pipeline (--bench profile): parse, analyze, codegen, build, llvm.",
      "properties": {
        "phase": { "type": "string", "enum": ["parse", "analyze", "codegen", "build", "llvm"] },
        "ms": { "type": "number" }
      }
    },
    "benchReport": {
      "type": "object",
      "required": ["total_ms", "mean_ms", "best_ms"],
      "description": "Wall-clock summary of one backend over the run set (milliseconds).",
      "properties": {
        "total_ms": { "type": "number" },
        "mean_ms": { "type": "number" },
        "best_ms": { "type": "number" }
      }
    },
    "benchCaseResult": {
      "type": "object",
      "required": ["name", "aot", "build"],
      "description": "One benchmark case measured on the one backend (ADR 0302): aot is warm execution of the artifact, build is the one-off cost of producing it, profile breaks that cost into the pipeline. error is non-empty when the case could not be measured; the row is kept so a suite never silently shrinks.",
      "properties": {
        "name": { "type": "string" },
        "aot": { "$ref": "#/definitions/benchReport" },
        "build": { "$ref": "#/definitions/benchReport" },
        "profile": { "type": "array", "items": { "$ref": "#/definitions/phaseTiming" } },
        "error": { "type": "string" }
      }
    },
    "benchSuite": {
      "type": "object",
      "required": ["schema_version", "generated_by", "runs", "opt_level", "cases", "totals"],
      "description": "Deterministic benchmark artifact (--bench-suite): cases sorted by name, plus totals. Diff two runs to see a slowdown as a number.",
      "properties": {
        "schema_version": { "type": "string" },
        "generated_by": { "type": "string" },
        "runs": { "type": "integer" },
        "opt_level": { "type": "integer" },
        "cases": {
          "type": "array",
          "items": { "$ref": "#/definitions/benchCaseResult" }
        },
        "totals": {
          "type": "object",
          "required": ["cases", "ran", "failed", "aot_total_ms", "build_total_ms"],
          "properties": {
            "cases": { "type": "integer" },
            "ran": { "type": "integer" },
            "failed": { "type": "integer" },
            "aot_total_ms": { "type": "number" },
            "build_total_ms": { "type": "number" }
          }
        }
      }
    },
    "benchRegression": {
      "type": "object",
      "required": ["name", "backend", "baseline_ms", "current_ms", "ratio", "suggestion"],
      "description": "One regression-gate violation (--bench-baseline): a case slower than its baseline by more than the tolerance, above the noise floor.",
      "properties": {
        "name": { "type": "string" },
        "backend": { "type": "string", "enum": ["aot"] },
        "baseline_ms": { "type": "number" },
        "current_ms": { "type": "number" },
        "ratio": { "type": "number" },
        "suggestion": { "type": "string" }
      }
    },
    "benchBaseline": {
      "type": "object",
      "required": ["schema_version", "cases"],
      "description": "Committed reference measurement written by --bench-baseline-update.",
      "properties": {
        "schema_version": { "type": "string" },
        "generated_by": { "type": "string" },
        "runs": { "type": "integer" },
        "opt_level": { "type": "integer" },
        "cases": {
          "type": "array",
          "items": {
            "type": "object",
            "required": ["name", "aot", "build"],
            "properties": {
              "name": { "type": "string" },
              "aot": { "type": "object", "description": "Best/mean/total ms of running the compiled artifact (lang.BenchReport): the only execution leg there is since ADR 0302." },
              "build": { "type": "object", "description": "Best/mean/total ms of the whole compile (check → codegen → opt → llc → cc), which is the leg a compiler regression shows up in." },
              "profile": { "type": "array", "description": "Per-phase timings of one compile: parse, analyze, codegen, build, llvm." },
              "error": { "type": "string", "description": "Set when the case did not run; a failed case is reported, never dropped from the totals." }
            }
          }
        }
      }
    },
    "irVerification": {
      "type": "object",
      "required": ["ok", "tool", "skipped"],
      "description": "LLVM module-verifier verdict for an emitted module (L8.2, --verify-llvm and BuildResult.verification). ok is true only when the verifier ran and accepted the module; skipped marks a missing toolchain, which is never reported as a pass.",
      "properties": {
        "ok": { "type": "boolean", "description": "true when LLVM accepted the module." },
        "tool": { "type": "string", "description": "Binary that decided the verdict (opt-20, or llc-20 -filetype=null as fallback)." },
        "skipped": { "type": "boolean", "description": "true when no verification toolchain was found; the module is then unverified, not verified." },
        "pipeline": { "type": "array", "items": { "type": "string" }, "description": "Pass pipelines that ran, e.g. [\"verify\", \"-O2\"]." },
        "errors": { "type": "array", "items": { "type": "string" }, "description": "Verifier diagnostics, normalised to stable text (no temp paths), capped to keep JSON small." },
        "note": { "type": "string", "description": "Machine-matchable guidance, e.g. that a rejection is a compiler bug rather than a source error." },
        "toolchain": { "type": "string", "description": "Pinned LLVM version the backend targets, e.g. \"LLVM 20\"." }
      }
    },
    "optimization": {
      "type": "object",
      "required": ["applied", "tool", "pipeline", "level"],
      "description": "What the optimization stage actually did (BuildResult.optimization, --build --json). Absent when no optimization was requested (--opt-level 0). applied is true only when the real LLVM optimizer ran, so a build that fell back to the textual pass is never mistaken for an optimized one (roadmap Gap J.4).",
      "properties": {
        "applied": { "type": "boolean", "description": "true when the real LLVM opt pipeline ran on this module." },
        "tool": { "type": "string", "description": "Optimizer that ran (opt-20), or gusty-textual when only the textual pass ran." },
        "pipeline": { "type": "string", "description": "Pipeline requested, e.g. \"-O2\", or \"textual\" / \"none\"." },
        "level": { "type": "integer", "description": "Optimization level the build asked for." },
        "fallback": { "type": "string", "description": "What ran instead of the real optimizer, e.g. \"textual\"; empty when none was needed." },
        "note": { "type": "string", "description": "Machine-matchable explanation of why the real optimizer did not run." },
        "error": { "type": "string", "description": "Optimizer failure text, when the tool exists but failed or rejected the module." }
      }
    },
    "debugInfo": {
      "type": "object",
      "required": ["schema_version", "file", "directory", "producer", "language", "emission_kind", "is_optimized", "compile_unit", "functions", "instructions", "tagged", "locations", "subprograms", "lines_truncated"],
      "description": "The line table the compiler put in the module, read back out of the emitted IR (gustyc --debug-info; the \"debug\" member of --build --json, --emit-llvm --debug --json). Every number comes from the module's own metadata nodes, not from what the emitter hoped to write, so a record LLVM would ignore is reported as missing rather than as coverage (L8.5, ADR 0231).",
      "properties": {
        "schema_version": { "type": "integer", "description": "Version of this document; 1 is the first." },
        "file": { "type": "string", "description": "Source file the compile unit names, as a debugger will print it." },
        "directory": { "type": "string", "description": "Compile directory the file is relative to (DW_AT_comp_dir)." },
        "producer": { "type": "string", "description": "Who wrote the debug info (DW_AT_producer), e.g. \"gusty 0.10.0\"." },
        "language": { "type": "string", "description": "DW_AT_language name; gusty names its own language: DW_LANG_Python." },
        "emission_kind": { "type": "string", "description": "FullDebug when the module carries a line table, and only then." },
        "is_optimized": { "type": "boolean", "description": "DW_AT_optimized, set from the optimization level the module was built at." },
        "compile_unit": { "type": "string", "description": "Metadata id of the DICompileUnit node, e.g. \"!3\"." },
        "functions": {
          "type": "array",
          "description": "One entry per program function the module describes, in symbol order. The compiler's own runtime blocks are absent: they are not program source, so a debugger must not stop in them.",
          "items": {
            "type": "object",
            "required": ["name", "symbol", "line", "instructions", "locations"],
            "properties": {
              "name": { "type": "string", "description": "Name as the program wrote it (\"Counter.bump\", \"lambda_0\", \"main\")." },
              "symbol": { "type": "string", "description": "Linker symbol (DISubprogram linkageName), e.g. \"gy_Counter_bump\"." },
              "line": { "type": "integer", "description": "Source line of the definition (DISubprogram line/scopeLine)." },
              "instructions": { "type": "integer", "description": "Instruction lines in this function that carry a !dbg record." },
              "locations": { "type": "integer", "description": "Distinct source positions inside this function." }
            }
          }
        },
        "lines": {
          "type": "array",
          "description": "The IR-line-to-source-line table: one row per tagged instruction, in IR order.",
          "items": {
            "type": "object",
            "required": ["irLine", "line", "col"],
            "properties": {
              "irLine": { "type": "integer", "description": "1-based line of the emitted module." },
              "line": { "type": "integer", "description": "Source line that instruction was written for." },
              "col": { "type": "integer", "description": "Source column of that statement (0 when the statement carries no column)." },
              "function": { "type": "string", "description": "IR symbol of the function the instruction sits in." }
            }
          }
        },
        "instructions": { "type": "integer", "description": "Instruction lines inside program functions (the denominator of the coverage claim)." },
        "tagged": { "type": "integer", "description": "Of those, how many carry a location whose scope names the function it sits in. tagged < instructions is reported, never hidden." },
        "locations": { "type": "integer", "description": "DILocation nodes in the module: one per distinct (line, column, function) position." },
        "subprograms": { "type": "integer", "description": "DISubprogram nodes in the module." },
        "lines_truncated": { "type": "boolean", "description": "true when the line table was cut at the caller's budget; counts above stay exact." },
        "defect": { "type": "string", "description": "Set when the module's own metadata disagrees with what the emitter meant to write \" \u2014 a record a debugger would misread. Empty means the two agree." }
      }
    },
    "dwarfReport": {
      "type": "object",
      "required": ["tool", "ran", "skipped", "ok", "line_rows"],
      "description": "The DWARF line table of a built artifact, read back from the object file with llvm-dwarfdump (the \"dwarf\" member of --build --debug --json). This is the proof that the !dbg records survived codegen, the optimizer and the assembler: nothing claims DWARF that the artifact does not carry (L8.5, ADR 0231).",
      "properties": {
        "tool": { "type": "string", "description": "llvm-dwarfdump binary that read the table." },
        "toolchain": { "type": "string", "description": "Pinned LLVM version, e.g. \"LLVM 20\"; empty when no toolchain was found." },
        "ran": { "type": "boolean", "description": "true when the tool ran (whatever its verdict)." },
        "skipped": { "type": "boolean", "description": "true when no llvm-dwarfdump was found: an absent toolchain is never reported as a pass." },
        "ok": { "type": "boolean", "description": "true only when the tool ran and the artifact really carries line-table rows." },
        "line_rows": { "type": "integer", "description": "Rows in .debug_line, including rows that name no source line (the compiler's runtime blocks)." },
        "source_lines": { "type": "array", "items": { "type": "integer" }, "description": "Distinct source lines the table covers, ascending \" \u2014 the answer to \"can a debugger stop at line N?\"." },
        "files": { "type": "array", "items": { "type": "string" }, "description": "File names the line table names, as a debugger will print them." },
        "note": { "type": "string", "description": "Machine-matchable reason when ok is false: no toolchain, no such file, the tool failed, or the artifact carries no rows." }
      }
    },
    "gcStats": {
      "type": "object",
      "required": ["collections", "roots", "skipped", "marked", "freed", "live", "backend"],
      "description": "What the garbage collector actually did while the program ran (gustyc --gc-stats; the \"gc\" member of an --eval/--file --json payload). Emitted by the compiled runtime's rt_gc_report line from inside the target program (gc: backend=aot collections=3 roots=4 ... top=17) and forwarded by the CLI, which parses it back into this object; there is one backend since ADR 0302, so the line's backend is always aot. Counts except collections and total_freed describe the most recent collection. The human form is the same numbers as one key=value line on stderr (the tool channel, never the program's stdout).",
      "properties": {
        "collections": { "type": "integer", "description": "Mark-and-sweep passes run so far (cumulative)." },
        "roots": { "type": "integer", "description": "Root handles the last collection traced: entries of the precise root set that really name a heap object (frame locals, declared root groups, permanent roots)." },
        "skipped": { "type": "integer", "description": "Root slots the last collection proved held raw immediates and therefore never scanned. This is the number precise rooting saves a conservative collector, which has to guess at every one of them." },
        "marked": { "type": "integer", "description": "Heap objects the last collection found reachable." },
        "freed": { "type": "integer", "description": "Heap objects the last collection reclaimed." },
        "total_freed": { "type": "integer", "description": "Heap objects reclaimed over the whole run (cumulative): the size of the garbage the program produced." },
        "live": { "type": "integer", "description": "Heap objects still resident after the last collection." },
        "frames": { "type": "integer", "description": "Call frames in the root set at the last collection. Zero means the collection happened at a top-level statement boundary." },
        "protected": { "type": "integer", "description": "Objects the allocation watermark kept alive without tracing: they were minted after the last safe point, so a register may still hold them." },
        "generational": { "type": "boolean", "description": "true when the last collection was a young (nursery) pass; false for a full sweep." },
        "top": { "type": "integer", "description": "High-water mark of the root stack: the most simultaneous rooted handle slots in any call tree. A program that exhausts the 4096-entry capacity stops itself rather than run with an unrooted handle." },
        "backend": { "type": "string", "description": "Which collector reported; the compiled runtime is the only one (ADR 0302).", "enum": ["aot"] }
      }
    },
    "valueTag": {
      "type": "integer",
      "minimum": 0,
      "maximum": 14,
      "description": "Canonical dynamic-kind tag: the one number that says what a value is, shared by the interpreter's heap objects (obj.kind → obj.tag), the compiled runtime's tagged obj values, and the extern-fn ABI's tag word. Names in table order: int=0, float=1, bool=2, None=3, str=4, list=5, dict=6, set=7, tuple=8, class=9, instance=10, method=11, closure=12, exn=13, module=14 (gustyc --lang prints them, lang.ValueTagNames() returns them, and a test pins this list so the schema cannot drift from the table). The compiled heap's object-header kind word is a projection of this table — none=0, list=1, dict=2, set=3, instance=4 — because it numbers only the kinds the compiled heap allocates; lang.HeapKindFor/HeapTagFor translate between the two, so a per-element tag, an object header, and an exported tagged value all mean the same thing by the same number."
    },
    "oracleReport": {
      "type": "object",
      "required": ["legs", "oracle"],
      "description": "The two-leg verdict printed by 'gustyc --oracle <src>' / '--oracle-file <path>' (lang.OracleReport): the compiled backend and CPython. There used to be a third leg, the AST interpreter, and a \"parity\" member comparing the two gusty engines; the engine retired (ADR 0302) and with it the member — a field comparing one thing to itself is not a fact. Same classification as a conformance matrix row, computed by the same function, for one ad-hoc program an agent wants checked before trusting it (roadmap L11.9, ADR 0186). Exit codes: 0 match, 6 debt (the program does not behave like Python), 7 not_applicable (no verdict - the oracle could not run the source).",
      "properties": {
        "legs": {
          "type": "array",
          "description": "Exactly two legs, in order: aot, python. Each records whether the leg completed, its stdout, a first-line error when it did not, and whether its stdout is the oracle's once the documented rules are applied.",
          "items": {
            "type": "object",
            "required": ["backend", "ok", "stdout", "matches_python"],
            "properties": {
              "backend": { "type": "string", "enum": ["aot", "python"] },
              "ok": { "type": "boolean", "description": "false when the leg failed: a compile refusal, a trap, or a Go panic (recorded, never fatal)." },
              "stdout": { "type": "string" },
              "error": { "type": "string" },
              "matches_python": { "type": "boolean", "description": "normalized stdout equals the python leg's. Always false for a leg that did not complete: a refusal is a debt, not a pass." }
            }
          }
        },
        "oracle": { "type": "string", "enum": ["match", "debt", "not_applicable"] },
        "notes": { "type": "array", "items": { "type": "string" }, "description": "Why the verdict is what it is, one line per leg that disagreed or failed." },
        "rules": { "type": "array", "items": { "type": "string" }, "description": "The documented comparison rules that were applied, so a reader can see exactly what was normalised away." }
      }
    },
    "conformanceRow": {
      "type": "object",
      "required": ["case", "aot_ok", "python_ok", "aot_matches_python", "conformant", "oracle", "oracle_declared"],
      "description": "One row of the conformance matrix (integration/conformance-matrix.json, lang.ConformanceResult). Two legs per program since ADR 0302: the compiled binary and CPython (the retired AST interpreter was a third, and \"parity\" — comparing the two gusty legs — left with it; see roadmap Gap R.190 for what that loss of witness is worth). \"oracle\" is what the compiled leg prints against CPython, and \"oracle_declared\" is what the registry in integration/conformance_cases.go claims — the harness fails when the two disagree, in either direction (roadmap L11.9, ADR 0186). \"conformant\" is the row's bottom line: an asserted row whose compiled stdout is CPython's; for a row the registry declares debt or not_applicable, CPython has no answer to compare against, so conformance is judged against the leg pins instead. A row claiming \"match\" means \"matches the pinned oracle\": the matrix's own toolchain block records which interpreters produced it — toolchain.python (the banner of the CPython that ran), toolchain.min_python (the pin, lang.OracleMinPython, currently 3.12) and toolchain.llvm — and a row's declared verdict presupposes the pin, because some programs cannot be parsed by an older oracle at all (PEP 695 type statements, e.g. type X = int, need 3.12; ADR 0193).",
      "properties": {
        "aot_stdout": { "type": "string", "description": "Everything the compiled binary wrote to stdout." },
        "python_stdout": { "type": "string", "description": "Everything the oracle interpreter wrote to stdout for the same source (PYTHONHASHSEED=0, so a run is reproducible)." },
        "aot_matches_python": { "type": "boolean", "description": "compiled stdout equals CPython's after the documented comparison rules." },
        "oracle": { "type": "string", "enum": ["match", "debt", "not_applicable"], "description": "Computed verdict: both backends print CPython's answer (match), at least one does not (debt — wrong value, refusal, or crash), or the source is not a CPython program at all (not_applicable)." },
        "oracle_declared": { "type": "string", "enum": ["match", "debt", "not_applicable"], "description": "The registry's claim. Absence of a ledger row means the claim is \"match\", so a new divergence cannot enter the corpus silently." },
        "oracle_reason": { "type": "string", "description": "What is wrong, in one sentence (required for debt and not_applicable rows)." },
        "oracle_ref": { "type": "string", "description": "The roadmap item that owns the fix (required for debt rows)." },
        "oracle_rules": { "type": "array", "items": { "type": "string" }, "description": "The documented comparison rules applied to every leg (e.g. \"set-order\": a set rendering is compared as a multiset because CPython's iteration order is unspecified)." },
        "oracle_drift": { "type": "array", "items": { "type": "string" }, "description": "Why this row fails: a classification that changed, a pin that no longer matches, or a debt that was paid without the ledger being updated." }
      }
    },
    "type": {
      "type": "string",
      "description": "Type annotation text. Union types render members joined by \" | \", e.g. \"int | str\".",
      "properties": {
        "span": {
          "type": "object",
          "description": "Source span (line:col) of the node."
        }
      }
    }
  },
  "additionalProperties": true
}`
