package lang

import "testing"

// TestParseAsyncDef verifies `async def` parses as a first-class FuncDef with
// the Async flag set (L5.6 parser phase).
func TestParseAsyncDef(t *testing.T) {
	prog, err := Parse("async def f(x):\n    return x + 1")
	if err != nil {
		t.Fatalf("parse async def: %v", err)
	}
	if len(prog.Stmts) != 1 {
		t.Fatalf("want 1 stmt, got %d", len(prog.Stmts))
	}
	fd, ok := prog.Stmts[0].(*FuncDef)
	if !ok {
		t.Fatalf("stmt is %T, want *FuncDef", prog.Stmts[0])
	}
	if !fd.Async {
		t.Errorf("async def: Async flag not set")
	}
	if fd.Name != "f" {
		t.Errorf("async def name: got %q", fd.Name)
	}
}

// TestParseAsyncFor verifies `async for` parses with the Async flag set.
func TestParseAsyncFor(t *testing.T) {
	prog, err := Parse("async for i in xs:\n    print(i)")
	if err != nil {
		t.Fatalf("parse async for: %v", err)
	}
	fs, ok := prog.Stmts[0].(*ForStmt)
	if !ok {
		t.Fatalf("stmt is %T, want *ForStmt", prog.Stmts[0])
	}
	if !fs.Async {
		t.Errorf("async for: Async flag not set")
	}
}

// TestParseAsyncWith verifies `async with` parses with the Async flag set.
func TestParseAsyncWith(t *testing.T) {
	prog, err := Parse("async with m as x:\n    print(x)")
	if err != nil {
		t.Fatalf("parse async with: %v", err)
	}
	ws, ok := prog.Stmts[0].(*WithStmt)
	if !ok {
		t.Fatalf("stmt is %T, want *WithStmt", prog.Stmts[0])
	}
	if !ws.Async {
		t.Errorf("async with: Async flag not set")
	}
}

// TestParseAwait verifies `await expr` is accepted as first-class syntax and,
// under the minimal synchronous-coroutine model, reduces to its operand.
func TestParseAwait(t *testing.T) {
	prog, err := Parse("x = await f(2)")
	if err != nil {
		t.Fatalf("parse await: %v", err)
	}
	// await f(2) reduces to f(2): the RHS is a Call, not a new AwaitExpr.
	as, ok := prog.Stmts[0].(*AssignStmt)
	if !ok {
		t.Fatalf("stmt is %T, want *AssignStmt", prog.Stmts[0])
	}
	if _, ok := as.Value.(*Call); !ok {
		t.Errorf("await desugared RHS is %T, want *Call", as.Value)
	}
}

// TestParseAsyncBad verifies `async` must be followed by def/for/with.
func TestParseAsyncBad(t *testing.T) {
	if _, err := Parse("async return 1"); err == nil {
		t.Errorf("expected error for 'async return 1'")
	}
}
