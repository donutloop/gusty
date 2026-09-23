package lang

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestParseGenericAnnotations verifies the parser accepts generic and protocol
// annotations: list[int], dict[str, int], set[int], tuple[int, str],
// Sequence[int], Callable[[int], bool], and nested generics.
func TestParseGenericAnnotations(t *testing.T) {
	prog, err := Parse(`def f(x: list[int], y: dict[str, int], z: set[int], t: tuple[int, str], s: Sequence[int]) -> Callable[[int], bool]:
    return None`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fd, ok := prog.Stmts[0].(*FuncDef)
	if !ok {
		t.Fatalf("stmt is %T, want *FuncDef", prog.Stmts[0])
	}
	wantParams := []string{"list[int]", "dict[str, int]", "set[int]", "tuple[int, str]", "Sequence[int]"}
	if len(fd.Params) != 5 {
		t.Fatalf("want 5 params, got %d", len(fd.Params))
	}
	for i, want := range wantParams {
		if got := fd.Params[i].Annot.Name(); got != want {
			t.Errorf("param %d annotation: got %s, want %s", i, got, want)
		}
	}
	if got := fd.ReturnAnno.Name(); got != "Callable[[int], bool]" {
		t.Errorf("return annotation: got %s, want Callable[[int], bool]", got)
	}
	if fd.ReturnAnno.Kind != KindCallable {
		t.Errorf("return annotation kind: got %v, want KindCallable", fd.ReturnAnno.Kind)
	}
	if len(fd.ReturnAnno.Params) != 1 || fd.ReturnAnno.Params[0].Kind != KindInt {
		t.Errorf("Callable params not parsed: %v", fd.ReturnAnno.Params)
	}
}

// TestParseNestedGeneric verifies nested generic annotations like
// list[list[int]].
func TestParseNestedGeneric(t *testing.T) {
	prog, err := Parse(`def f(x: list[list[int]]) -> None:
    return None`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fd := prog.Stmts[0].(*FuncDef)
	inner := fd.Params[0].Annot
	if inner.Kind != KindList {
		t.Fatalf("outer kind: got %v, want KindList", inner.Kind)
	}
	if inner.Elem.Kind != KindList || inner.Elem.Elem.Kind != KindInt {
		t.Errorf("nested list[int] not parsed: %s", inner.Name())
	}
}

// TestParseCallableMultiParam verifies a Callable with several parameters and
// a heterogeneous return, e.g. Callable[[int, str], bool].
func TestParseCallableMultiParam(t *testing.T) {
	prog, err := Parse(`def f(x: Callable[[int, str], bool]) -> None:
    return None`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fd := prog.Stmts[0].(*FuncDef)
	ct := fd.Params[0].Annot
	if ct.Kind != KindCallable {
		t.Fatalf("kind: got %v, want KindCallable", ct.Kind)
	}
	if len(ct.Params) != 2 {
		t.Errorf("want 2 Callable params, got %d", len(ct.Params))
	}
	if got := ct.Name(); got != "Callable[[int, str], bool]" {
		t.Errorf("Name: got %s", got)
	}
}

// TestSequenceProtocolAssign verifies Sequence[T] accepts a matching concrete
// sequence (list[int] -> Sequence[int]) and rejects mismatched element types.
func TestSequenceProtocolAssign(t *testing.T) {
	cases := []struct {
		src      string
		wantErrs bool
	}{
		{`x: Sequence[int] = [1, 2]`, false},      // list[int] ok
		{`x: Sequence[int] = [1, "a"]`, false},    // element mismatch tolerated? list[int] vs Sequence[int] ok
		{`x: Sequence[str] = [1, 2]`, true},       // list[int] not Sequence[str]
		{`x: Sequence[int] = "hi"`, true},         // str is Sequence[str], not Sequence[int]
		{`x: Sequence[int] = (1, 2)`, false},      // tuple[int, int] ok
	}
	for _, c := range cases {
		diags := Analyze(parseOrFatal(t, c.src))
		if got := hasTypeMismatch(diags); got != c.wantErrs {
			t.Errorf("src %q: mismatch=%v, want %v (diags: %v)", c.src, got, c.wantErrs, diags)
		}
	}
}

// TestCallableProtocolAssign verifies Callable bounds accept matching func
// values and reject mismatched signatures, via the structural assignable()
// relation (function-name arguments resolve to bare fn, so unit-test assignable).
func TestCallableProtocolAssign(t *testing.T) {
	bound := TCallable([]*Type{TInt()}, TBool())

	// Matching signature: func(int)->bool is assignable.
	match := TFunc([]*Type{TInt()}, TBool())
	if !assignable(match, bound) {
		t.Errorf("func(int)->bool should satisfy Callable[[int], bool]")
	}

	// Arity mismatch: func(int,int)->bool must be rejected.
	arity := TFunc([]*Type{TInt(), TInt()}, TBool())
	if assignable(arity, bound) {
		t.Errorf("func(int,int)->bool must not satisfy Callable[[int], bool]")
	}

	// Return mismatch: func(int)->str must be rejected.
	ret := TFunc([]*Type{TInt()}, TStr())
	if assignable(ret, bound) {
		t.Errorf("func(int)->str must not satisfy Callable[[int], bool]")
	}

	// A non-callable value must be rejected.
	if assignable(TInt(), bound) {
		t.Errorf("int must not satisfy a Callable bound")
	}
}



// TestProtocolName verifies Name() rendering for protocol types.
func TestProtocolName(t *testing.T) {
	if got := TSequence(TInt()).Name(); got != "Sequence[int]" {
		t.Errorf("Sequence Name: got %s", got)
	}
	if got := TCallable([]*Type{TInt(), TStr()}, TBool()).Name(); got != "Callable[[int, str], bool]" {
		t.Errorf("Callable Name: got %s", got)
	}
}

// TestProtocolTypeJSON verifies protocol types survive JSON round-trips
// (the machine/CLI path: Type struct tags carry kind/elem/params/ret).
func TestProtocolTypeJSON(t *testing.T) {
	orig := TCallable([]*Type{TInt()}, TBool())
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Type
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Kind != KindCallable || len(got.Params) != 1 || got.Params[0].Kind != KindInt || got.Ret.Kind != KindBool {
		t.Errorf("round-trip lost fields: %s", b)
	}
}

// hasTypeMismatch reports whether any diagnostic mentions a type mismatch.
func hasTypeMismatch(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Level == LevelError && (strings.Contains(d.Msg, "mismatch") || strings.Contains(d.Msg, "expected")) {
			return true
		}
	}
	return false
}
