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
      "required": ["name", "interpreter", "aot", "speedup"],
      "description": "One benchmark case measured on both execution backends. error is non-empty when the case could not be measured; the row is kept so a suite never silently shrinks.",
      "properties": {
        "name": { "type": "string" },
        "interpreter": { "$ref": "#/definitions/benchReport" },
        "aot": { "$ref": "#/definitions/benchReport" },
        "speedup": { "type": "number", "description": "interpreter best_ms / aot best_ms; > 1 means the compiled artifact is faster." },
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
          "required": ["cases", "ran", "failed", "interpreter_total_ms", "aot_total_ms", "geomean_speedup"],
          "properties": {
            "cases": { "type": "integer" },
            "ran": { "type": "integer" },
            "failed": { "type": "integer" },
            "interpreter_total_ms": { "type": "number" },
            "aot_total_ms": { "type": "number" },
            "geomean_speedup": { "type": "number" }
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
        "backend": { "type": "string", "enum": ["aot", "interpreter"] },
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
            "required": ["name", "interpreter_best_ms", "aot_best_ms"],
            "properties": {
              "name": { "type": "string" },
              "interpreter_best_ms": { "type": "number" },
              "aot_best_ms": { "type": "number" }
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
