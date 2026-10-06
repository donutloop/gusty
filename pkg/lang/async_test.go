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
	aw, ok := as.Value.(*AwaitExpr)
	if !ok {
		t.Errorf("await parsed RHS is %T, want *AwaitExpr", as.Value)
	}
	if _, ok := aw.Expr.(*Call); !ok {
		t.Errorf("await operand is %T, want *Call", aw.Expr)
	}
}

// TestParseAsyncBad verifies `async` must be followed by def/for/with.
func TestParseAsyncBad(t *testing.T) {
	if _, err := Parse("async return 1"); err == nil {
		t.Errorf("expected error for 'async return 1'")
	}
}

func TestAsyncCoroAwait(t *testing.T) {
	src := `async def f(x):
    return x + 1
v = await f(2)
v`
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("EvalExpr: %v", err)
	}
	if v != 3 {
		t.Errorf("await f(2) = %v, want 3", v)
	}
}

func TestAsyncAwaitPlain(t *testing.T) {
	v, _, err := evalGolden(t, "await 5")
	if err != nil {
		t.Fatalf("EvalExpr: %v", err)
	}
	if v != 5 {
		t.Errorf("await 5 = %v, want 5", v)
	}
}

func TestAsyncCoroDeferred(t *testing.T) {
	// Calling an async def does NOT run the body — the body runs at the await. The
	// observation is the body's own effect: called eagerly, `raise ValueError` fires
	// while evaluating `c = f()` — outside the try — and the program dies there
	// instead of answering 2. Deferred, the raise arrives inside the try, which is
	// only reachable if the body ran at the `await` and nowhere earlier. That eager
	// shape is precisely what the compiled backend used to do silently.
	//
	// The coroutine also has to be awaited at all: L7.6 refuses one that is simply
	// dropped (TestEffectsNeverAwaited), because a handle nobody awaits is not a
	// value any backend can print consistently — that refusal is the other half of
	// this round.
	src := `async def f():
    raise ValueError("ran")
    return 7
c = f()
n = 0
try:
    await c
    n = 1
except ValueError:
    n = 2
n`
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("EvalExpr: %v", err)
	}
	if v != 2 {
		t.Errorf("deferred async call = %d, want 2 (the body raised at the await, not at the call)", v)
	}
}

// TestEffectsNeverAwaited: the dropped coroutine is now a checked error, not a
// handle the two backends print differently ("<coro>" vs the eager result).
func TestEffectsNeverAwaited(t *testing.T) {
	src := `async def f():
    return 7
c = f()
c`
	if _, _, err := evalGolden(t, src); err == nil {
		t.Errorf("expected the checker to refuse a coroutine that is never awaited")
	}
	diags := Analyze(parseOrFatal(t, src))
	if !hasDiagCode(diags, CodeCoroNeverAwaited) {
		t.Errorf("want %s, got %v", CodeCoroNeverAwaited, diags)
	}
}

func TestAsyncForCoro(t *testing.T) {
	src := `async def f(x):
    return x * 2
async for v in [f(1), f(2), f(3)]:
    print(v)
`
	v, _, err := evalGolden(t, src)
	if err != nil {
		t.Fatalf("EvalExpr: %v", err)
	}
	_ = v
}
