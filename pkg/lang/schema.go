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
