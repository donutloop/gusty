package lang

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestEmitASTIncludesSpanAndInferredType verifies the --emit-ast JSON is
// richer: every node carries a `span` and expression nodes carry the
// `inferred` type computed by the semantic pass.
func TestEmitASTIncludesSpanAndInferredType(t *testing.T) {
	res, err := Compile("x = 1 + 2\ny = [1, 2, 3]\nprint(y)\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if len(res.Diagnostics) > 0 {
		t.Fatalf("compile diagnostics: %v", res.Diagnostics)
	}
	var ast map[string]any
	if err := json.Unmarshal([]byte(res.ASTJSON), &ast); err != nil {
		t.Fatalf("ASTJSON is not valid JSON: %v", err)
	}
	stmts, ok := ast["stmts"].([]any)
	if !ok || len(stmts) < 3 {
		t.Fatalf("expected >=3 stmts, got %v", stmts)
	}
	// Walk the JSON and collect evidence of span + inferred.
	var hasSpan, hasInferred bool
	var inferredNames []string
	var walk func(any)
	walk = func(v any) {
		switch n := v.(type) {
		case map[string]any:
			if _, ok := n["span"]; ok {
				hasSpan = true
			}
			if ty, ok := n["inferred"]; ok {
				hasInferred = true
				inferredNames = append(inferredNames, ty.(string))
			}
			for _, c := range n {
				walk(c)
			}
		case []any:
			for _, c := range n {
				walk(c)
			}
		}
	}
	walk(stmts)
	if !hasSpan {
		t.Fatal("AST JSON has no `span` fields; expected every node to carry its source span")
	}
	if !hasInferred {
		t.Fatal("AST JSON has no `inferred` fields; expected expression nodes to carry inferred types")
	}
	joined := strings.Join(inferredNames, ",")
	if !strings.Contains(joined, "int") {
		t.Fatalf("expected an inferred `int` for `1 + 2`, got: %s", joined)
	}
	if !strings.Contains(joined, "list[int]") {
		t.Fatalf("expected an inferred `list[int]` for the list literal, got: %s", joined)
	}
}
