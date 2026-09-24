package lang

import "testing"

// TestPanicRecoveryForest verifies panic-mode recovery: multiple parse errors
// are surfaced as a forest (not just the first), and parsing continues past
// bad statements to collect valid ones.
func TestPanicRecoveryForest(t *testing.T) {
	src := "x = 1\nbad = \ny = 2\ndef f():\n  z =\nw = 3\n"
	prog, err := Parse(src)
	if err == nil {
		t.Fatal("expected parse errors")
	}
	pes, ok := err.(*ParseErrors)
	if !ok {
		t.Fatalf("expected *ParseErrors, got %T", err)
	}
	if len(pes.Errors) != 2 {
		t.Fatalf("expected 2 parse errors, got %d", len(pes.Errors))
	}
	// both errors should be reported: bad = (line 2) and z = inside def (line 5)
	if prog == nil {
		t.Fatal("expected partially-parsed program")
	}
	// the valid statements x=1, y=2, w=3 should survive; the broken def is dropped
	var targets []string
	for _, st := range prog.Stmts {
		if a, ok := st.(*AssignStmt); ok {
			targets = append(targets, a.Target.(*Name).Value)
		}
	}
	want := []string{"x", "y", "w"}
	if len(targets) != len(want) {
		t.Fatalf("expected %v, got %v", want, targets)
	}
	for i := range want {
		if targets[i] != want[i] {
			t.Fatalf("stmt %d: expected %s got %s", i, want[i], targets[i])
		}
	}
}

// TestPanicRecoveryCleanInput verifies a clean input still parses with no error.
func TestPanicRecoveryCleanInput(t *testing.T) {
	src := "a = 1\nb = 2\nprint(a)\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prog == nil || len(prog.Stmts) != 3 {
		t.Fatalf("expected 3 stmts, got %d", len(prog.Stmts))
	}
}

// TestPanicRecoveryNested verifies recovery skips out of a nested block error
// and resumes at the next top-level statement.
func TestPanicRecoveryNested(t *testing.T) {
	src := "def f():\n  if x:\n    bad =\n  ok = 1\nnext = 2\n"
	prog, err := Parse(src)
	if err == nil {
		t.Fatal("expected parse errors")
	}
	pes, ok := err.(*ParseErrors)
	if !ok {
		t.Fatalf("expected *ParseErrors, got %T", err)
	}
	// error is the bad = inside the inner if; def f() is dropped by recovery
	if len(pes.Errors) < 1 {
		t.Fatalf("expected >=1 parse error, got %d", len(pes.Errors))
	}
	if prog == nil {
		t.Fatal("expected partial program")
	}
	// next = 2 must survive recovery
	var targets []string
	for _, st := range prog.Stmts {
		if a, ok := st.(*AssignStmt); ok {
			targets = append(targets, a.Target.(*Name).Value)
		}
	}
	foundNext := false
	for _, tg := range targets {
		if tg == "next" {
			foundNext = true
		}
	}
	if !foundNext {
		t.Fatalf("expected next=2 to survive recovery, got %v", targets)
	}
}

// TestPanicRecoverySingleError verifies a single error is still reported as a
// forest with one element.
func TestPanicRecoverySingleError(t *testing.T) {
	src := "x = 1\nbad =\n"
	_, err := Parse(src)
	if err == nil {
		t.Fatal("expected parse error")
	}
	pes, ok := err.(*ParseErrors)
	if !ok {
		t.Fatalf("expected *ParseErrors, got %T", err)
	}
	if len(pes.Errors) != 1 {
		t.Fatalf("expected 1 parse error, got %d", len(pes.Errors))
	}
}

// TestPanicRecoveryBadHeader verifies recovery from a bad top-level statement
// header (not a complete block) skips to the next statement boundary.
func TestPanicRecoveryBadHeader(t *testing.T) {
	src := "def f( :\n  x = 1\ny = 2\n"
	prog, err := Parse(src)
	if err == nil {
		t.Fatal("expected parse errors")
	}
	pes := err.(*ParseErrors)
	if len(pes.Errors) < 1 {
		t.Fatalf("expected >=1 parse error, got %d", len(pes.Errors))
	}
	if prog == nil {
		t.Fatal("expected partial program")
	}
	var targets []string
	for _, st := range prog.Stmts {
		if a, ok := st.(*AssignStmt); ok {
			targets = append(targets, a.Target.(*Name).Value)
		}
	}
	// y = 2 must survive recovery
	if len(targets) != 1 || targets[0] != "y" {
		t.Fatalf("expected y to survive recovery, got %v", targets)
	}
}

// TestPanicRecoverySingleLineBlock verifies recovery inside a single-line block.
func TestPanicRecoverySingleLineBlock(t *testing.T) {
	src := "if x: bad =\nw = 3\n"
	prog, err := Parse(src)
	if err == nil {
		t.Fatal("expected parse errors")
	}
	pes := err.(*ParseErrors)
	if len(pes.Errors) < 1 {
		t.Fatalf("expected >=1 parse error, got %d", len(pes.Errors))
	}
	if prog == nil {
		t.Fatal("expected partial program")
	}
	var targets []string
	for _, st := range prog.Stmts {
		if a, ok := st.(*AssignStmt); ok {
			targets = append(targets, a.Target.(*Name).Value)
		}
	}
	if len(targets) != 1 || targets[0] != "w" {
		t.Fatalf("expected w to survive recovery, got %v", targets)
	}
}

// TestPanicRecoveryLSP verifies the LSP surfaces the parse-error forest as
// separate diagnostics alongside semantic diagnostics.
func TestPanicRecoveryLSP(t *testing.T) {
	d := &Document{Text: "x = 1\nbad = \ny = 2\ndef f():\n  z =\nw = 3\n"}
	d.analyze()
	if len(d.Diags) < 2 {
		t.Fatalf("expected >=2 parse diagnostics, got %d", len(d.Diags))
	}
	// parse-error diagnostics carry severity 1 and mention "parse error"
	var parseCount int
	for _, di := range d.Diags {
		if di.Severity == 1 && contains(di.Message, "parse error") {
			parseCount++
		}
	}
	if parseCount != 2 {
		t.Fatalf("expected 2 parse-error diagnostics, got %d", parseCount)
	}
	if d.Prog == nil {
		t.Fatal("expected partial program indexed")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
