package lang

import (
	"strings"
	"testing"
)

func TestTupleTypeName(t *testing.T) {
	tt := TTuple(TInt(), TStr())
	if got := tt.Name(); got != "tuple[int, str]" {
		t.Fatalf("TTuple Name = %q, want tuple[int, str]", got)
	}
}

func TestTupleTypeSame(t *testing.T) {
	a := TTuple(TInt(), TStr())
	b := TTuple(TInt(), TStr())
	c := TTuple(TInt(), TBool())
	if !a.Same(b) {
		t.Fatalf("same element tuples should be Same")
	}
	if a.Same(c) {
		t.Fatalf("different element tuples must not be Same")
	}
}

func TestAnalyzeTupleLiteral(t *testing.T) {
	// `x = (1, "a")` should infer x as tuple[int, str].
	prog, err := Parse(`x = (1, "a")`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := Analyze(prog)
	if len(diags) != 0 {
		t.Fatalf("analyze: %v", diags)
	}
}

func TestAnalyzeTupleUnpack(t *testing.T) {
	prog, err := Parse(`a, b = (1, "a")`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := Analyze(prog)
	if len(diags) != 0 {
		t.Fatalf("analyze: %v", diags)
	}
}

func TestAnalyzeTupleUnpackMismatch(t *testing.T) {
	// Unpacking a 2-tuple into 3 targets should report a diagnostic.
	prog, err := Parse(`a, b, c = (1, "a")`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	diags := Analyze(prog)
	found := false
	for _, d := range diags {
		if len(d.Msg) > 0 && strings.Contains(d.Msg, "tuple unpack length mismatch") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected tuple unpack mismatch diagnostic, got %v", diags)
	}
}
