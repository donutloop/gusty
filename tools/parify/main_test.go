package main

// The tool that decides who may run beside whom has to be the best-tested thing in the suite, because a
// wrong verdict here is not a crash — it is a test that passes while asserting something else. Every case
// below is one rule stated as a source file and a claim about what the analysis must say about it,
// including the reason it must give when it refuses (ADR 0343).

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// harnessSource is the import block every synthetic file carries, plus the two helpers the rules mention
// by name so a case can call them without the package failing to parse.
const harnessSource = `package suite

import (
	"os"
	"os/signal"
	"sync"
	"testing"
)

var switchboard bool

// slots is a second package name the compiler's own functions also use for locals, which is what makes the
// shadowing case below a case and not a tautology.
var slots = map[string]string{}

// touchWorkingDirectory chdirs, which is the act the rules refuse.
func touchWorkingDirectory() { os.Chdir(os.TempDir()) }

// flipTheSwitch writes the package-level switch the rules watch for.
func flipTheSwitch(v bool) { switchboard = v }

// installHandler reaches for the process's signal disposition.
func installHandler() { signal.Notify(make(chan os.Signal, 1)) }

// inHost asks for the entry that owns fd 1 and fd 2.
func inHost() bool { return (entry{InProcess: true}).InProcess }

type entry struct{ InProcess bool }

func (e entry) String() string { return "" }

// plainBody touches nothing the process owns, so a case that calls it may run beside another.
func plainBody(t *testing.T) { t.Log("nothing the process owns") }

var ledger = map[string]string{}
var ledgerMu sync.Mutex
var lateMu sync.Mutex
var late string

// noteGap files a gap under the lock, the way the run-wide ledgers do.
func noteGap(k, v string) {
	ledgerMu.Lock()
	if _, seen := ledger[k]; !seen {
		ledger[k] = v
	}
	ledgerMu.Unlock()
}

// noteGapDeferred is the same write with the unlock deferred, which is the other spelling the suite uses.
func noteGapDeferred(k, v string) {
	ledgerMu.Lock()
	defer ledgerMu.Unlock()
	ledger[k] = v
}
`

// runOn writes one synthetic test file into its own directory and returns the tool's verdicts over it, so
// no case can be affected by which other files happen to be in the tree.
func runOn(t *testing.T, body string) ([]verdict, []byte) {
	t.Helper()
	dir := t.TempDir()
	src := harnessSource + "\n" + body + "\n"
	// Format first: the tool reads indentation out of the file to place its line, and the real tree is
	// gofmt-clean, so a synthetic file that is not would be testing a shape no commit ever has.
	formatted, err := format.Source([]byte(src))
	if err != nil {
		t.Fatalf("the synthetic file does not format: %v\n%s", err, src)
	}
	src = string(formatted)
	path := filepath.Join(dir, "suite_test.go")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, err := loadPackage(dir)
	if err != nil {
		t.Fatalf("loadPackage: %v", err)
	}
	pkg.analyze()
	vs, edits := pkg.plan()
	if len(edits[path]) == 0 {
		return vs, []byte(src)
	}
	out, err := applyEdits([]byte(src), edits[path])
	if err != nil {
		t.Fatalf("applyEdits: %v", err)
	}
	// An insertion that does not survive gofmt is an insertion that rewrites the file's shape, which the
	// tool has no business doing: it adds one line per case and nothing else.
	if _, err := parser.ParseFile(token.NewFileSet(), path, out, 0); err != nil {
		t.Fatalf("the rewritten file does not parse: %v\n%s", err, out)
	}
	// -apply runs the same bytes through gofmt before writing, so this is the check on the content of the
	// insertion; that the written file is formatted is TestTheToolIsIdempotent's claim.
	if formatted, err := format.Source(out); err != nil {
		t.Fatalf("the rewritten file cannot be formatted: %v\n%s", err, out)
	} else {
		out = formatted
	}
	return vs, out
}

func verdictFor(t *testing.T, vs []verdict, kind, name string) verdict {
	t.Helper()
	for _, v := range vs {
		if v.Kind == kind && v.Name == name {
			return v
		}
	}
	t.Fatalf("no %s verdict for %q in %+v", kind, name, vs)
	return verdict{}
}

func TestACaseThatTouchesNothingRunsBesideTheOthers(t *testing.T) {
	t.Parallel()
	vs, out := runOn(t, `
func TestOrdinary(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("arithmetic")
	}
}

func TestThroughAHelper(t *testing.T) {
	plainBody(t)
}
`)
	for _, name := range []string{"TestOrdinary", "TestThroughAHelper"} {
		v := verdictFor(t, vs, "test", name)
		if !v.Parallel {
			t.Errorf("%s was left serial: %s", name, v.Reason)
		}
	}
	if got := strings.Count(string(out), "t.Parallel()"); got != 2 {
		t.Errorf("expected one insertion per case, got %d:\n%s", got, out)
	}
}

func TestAFileAlreadyMarkedIsLeftAlone(t *testing.T) {
	t.Parallel()
	vs, out := runOn(t, `
func TestAlreadyParallel(t *testing.T) {
	t.Parallel()
	if 1+1 != 2 {
		t.Fatal("arithmetic")
	}
}
`)
	v := verdictFor(t, vs, "test", "TestAlreadyParallel")
	if !v.Parallel {
		t.Errorf("an already-parallel case must report itself parallel: %s", v.Reason)
	}
	if strings.Count(string(out), "t.Parallel()") != 1 {
		t.Errorf("the tool inserted a second t.Parallel():\n%s", out)
	}
}

func TestTheProcessWideActsRefuseAndSayWhy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		fname      string
		body       string
		wantReason string
	}{
		{
			name:       "chdir",
			fname:      "TestChdirs",
			body:       "func TestChdirs(t *testing.T) {\n\tos.Chdir(os.TempDir())\n}",
			wantReason: "changes the process's working directory",
		},
		{
			name:       "t.Setenv",
			fname:      "TestSetsEnv",
			body:       "func TestSetsEnv(t *testing.T) {\n\tt.Setenv(\"GUSTY_X\", \"1\")\n}",
			wantReason: "edits the process environment",
		},
		{
			name:       "os.Setenv",
			fname:      "TestSetsEnvDirectly",
			body:       "func TestSetsEnvDirectly(t *testing.T) {\n\tos.Setenv(\"GUSTY_X\", \"1\")\n}",
			wantReason: "edits the process environment",
		},
		{
			name:       "a helper that chdirs",
			fname:      "TestThroughAChdirHelper",
			body:       "func TestThroughAChdirHelper(t *testing.T) {\n\ttouchWorkingDirectory()\n}",
			wantReason: "changes the process's working directory",
		},
		{
			name:       "a helper that signals",
			fname:      "TestSignals",
			body:       "func TestSignals(t *testing.T) {\n\tinstallHandler()\n}",
			wantReason: "installs a process-wide signal handler",
		},
		{
			name:       "a helper that writes a package variable",
			fname:      "TestFlips",
			body:       "func TestFlips(t *testing.T) {\n\tflipTheSwitch(true)\n\t_ = switchboard\n}",
			wantReason: "package-level variable",
		},
		{
			name:       "the in-process entry",
			fname:      "TestInProcess",
			body:       "func TestInProcess(t *testing.T) {\n\tif !inHost() {\n\t\tt.Fatal(\"the entry was not asked for\")\n\t}\n}",
			wantReason: "asks for the in-process execution entry",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			vs, _ := runOn(t, tc.body)
			v := verdictFor(t, vs, "test", tc.fname)
			if v.Parallel {
				t.Fatalf("%s was marked parallel despite being process-wide:\n%s", tc.name, tc.body)
			}
			if !strings.Contains(v.Reason, tc.wantReason) {
				t.Errorf("the refusal does not name the cause\n got %q\nwant it to contain %q", v.Reason, tc.wantReason)
			}
		})
	}
}

// TestAMutexGuardedLedgerWriteIsParallel is the other half of the package-variable rule, and the half that
// decides whether most of the conformance suite runs beside itself: the run-wide ledgers (compiled refusals,
// reference debt) are package-level maps written from hundreds of cases, under a mutex, which is what makes
// that legal. The rule reads the idiom — including `defer Unlock`, which holds the lock to the end of body.
func TestAMutexGuardedLedgerWriteIsParallel(t *testing.T) {
	t.Parallel()
	vs, _ := runOn(t, `
func TestNotesAGap(t *testing.T) {
	noteGap("src", "the sentence the compiler used")
	if ledger["src"] == "" {
		t.Fatal("the gap was not filed")
	}
}

func TestNotesAGapWithADefer(t *testing.T) {
	noteGapDeferred("src", "the sentence the compiler used")
	if ledger["src"] == "" {
		t.Fatal("the gap was not filed")
	}
}

func TestWritesWithoutTheLock(t *testing.T) {
	lateMu.Lock()
	if late != "" {
		t.Log(late)
	}
	lateMu.Unlock()
	late = "outside the span"
}
`)
	for _, name := range []string{"TestNotesAGap", "TestNotesAGapWithADefer"} {
		if v := verdictFor(t, vs, "test", name); !v.Parallel {
			t.Errorf("%s writes a package-level map under a mutex and must run beside others: %s", name, v.Reason)
		}
	}
	// The third case takes the lock, releases it, and then writes — which is exactly the unlocked write the
	// rule exists to catch, so a span must be per Lock call and not per function.
	if v := verdictFor(t, vs, "test", "TestWritesWithoutTheLock"); v.Parallel {
		t.Errorf("a write made after the mutex was released was treated as locked")
	}
}

func TestACaseThatEditsAPackageVariableRunsAlone(t *testing.T) {
	t.Parallel()
	vs, _ := runOn(t, `
func TestFlipsTheSwitchBoard(t *testing.T) {
	flipTheSwitch(true)
	if !switchboard {
		t.Fatal("the switch did not flip")
	}
}
`)
	v := verdictFor(t, vs, "test", "TestFlipsTheSwitchBoard")
	if v.Parallel {
		t.Fatalf("a case that writes a package-level variable was marked parallel")
	}
	if !strings.Contains(v.Reason, "package-level variable") {
		t.Errorf("the refusal should name the variable it is worried about, got %q", v.Reason)
	}
}

func TestASubtestThatWritesItsParentsFixtureKeepsItsSiblingsSerial(t *testing.T) {
	t.Parallel()
	vs, out := runOn(t, `
func TestSharesOneResult(t *testing.T) {
	got := ""
	for _, tc := range []struct{ name, in string }{{"a", "1"}} {
		t.Run(tc.name, func(t *testing.T) {
			got = tc.in
		})
	}
	if got == "" {
		t.Fatal("nothing ran")
	}
}

func TestSharesNothing(t *testing.T) {
	for _, tc := range []struct {
		name, in string
	}{{"a", "1"}} {
		t.Run("a clean case", func(t *testing.T) {
			if tc.in == "" {
				t.Fatal("no input")
			}
		})
	}
}
`)
	// Two subtests: the first parent's writes `got`, the second's touches nothing.
	var shared, clean verdict
	seen := 0
	for _, v := range vs {
		if v.Kind != "subtest" {
			continue
		}
		seen++
		if v.Parent == "TestSharesOneResult" {
			shared = v
		} else {
			clean = v
		}
	}
	if seen != 2 {
		t.Fatalf("expected two subtest verdicts, got %d", seen)
	}
	if shared.Parallel {
		t.Errorf("a subtest that assigns its parent's `got` must not run beside its siblings")
	}
	if !strings.Contains(shared.Reason, "parent") {
		t.Errorf("the refusal should say the parent owns the thing, got %q", shared.Reason)
	}
	if !clean.Parallel {
		t.Errorf("a subtest that writes nothing of its parent's must run beside its siblings: %s", clean.Reason)
	}
	if !verdictFor(t, vs, "test", "TestSharesOneResult").Parallel {
		t.Errorf("the parent is still allowed beside OTHER tests, only not beside its own cases")
	}
	if v := verdictFor(t, vs, "subtest", "a clean case"); !v.Parallel {
		t.Errorf("a subtest with a literal name must be named by it in the report: %+v", v)
	}
	if !strings.Contains(string(out), "t.Parallel()") {
		t.Errorf("nothing was inserted at all:\n%s", out)
	}
}

func TestAMarkedCaseIsLeftAloneWithItsReason(t *testing.T) {
	t.Parallel()
	vs, _ := runOn(t, `
//parify:serial this case drives the REPL through a pty and needs to be the only reader of the terminal
func TestNeedsTheTerminalToItself(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("arithmetic")
	}
}
`)
	v := verdictFor(t, vs, "test", "TestNeedsTheTerminalToItself")
	if v.Parallel {
		t.Fatalf("a marked case was marked parallel anyway")
	}
	if !v.MarkedSerial {
		t.Errorf("the marker was not noticed, so -check would fail the build over a deliberate exception")
	}
}

func TestTheToolIgnoresWhatIsNotATest(t *testing.T) {
	t.Parallel()
	vs, out := runOn(t, `
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func BenchmarkOneProgram(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
	}
}

func helperThatLooksLikeATest(t *testing.T) {
	t.Log("not a case")
}

func TestReal(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("arithmetic")
	}
}
`)
	for _, v := range vs {
		if v.Name != "TestReal" {
			t.Errorf("the tool produced a verdict for %q; TestMain, benchmarks and helpers are not cases", v.Name)
		}
	}
	if got := strings.Count(string(out), "t.Parallel()"); got != 1 {
		t.Errorf("expected exactly one insertion, got %d:\n%s", got, out)
	}
}

// TestTheToolIsIdempotent is the property that lets it run again after a rebase: a file the tool has
// written must come back saying "already parallel" and no further change.
func TestTheToolIsIdempotent(t *testing.T) {
	t.Parallel()
	_, first := runOn(t, `
func TestOnce(t *testing.T) {
	for _, tc := range []struct{ name string }{{"a"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Log(tc.name)
		})
	}
}
`)
	dir := t.TempDir()
	path := filepath.Join(dir, "suite_test.go")
	if err := os.WriteFile(path, first, 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, err := loadPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	pkg.analyze()
	_, edits := pkg.plan()
	if len(edits[path]) != 0 {
		t.Errorf("a second pass over its own output still wants to insert %d line(s)", len(edits[path]))
	}
	// And the file it produced must be the file gofmt agrees with, or every commit touches whitespace.
	formatted, err := format.Source(first)
	if err != nil {
		t.Fatal(err)
	}
	if string(formatted) != string(first) {
		t.Errorf("the tool's own output is not gofmt-clean:\n%s", first)
	}
}

// TestACaseThatSaysParallelAndTakesTheProcessAnywayIsAFailure is the ratchet that keeps the tool honest about
// its own earlier output. The first version of this tool wrote `t.Parallel()` into 2,800 cases with thinner
// rules than these, and a checker that read that sentence as *proof* would certify every case that version
// got wrong — among them the ones that overwrite the toolchain's `llc` path while every neighbouring compile
// is calling it. The body is read whatever the file says.
func TestACaseThatSaysParallelAndTakesTheProcessAnywayIsAFailure(t *testing.T) {
	t.Parallel()
	vs, _ := runOn(t, `
func TestAlreadyMarkedParallelButTakesTheProcess(t *testing.T) {
	t.Parallel()
	touchWorkingDirectory()
}

func TestAlreadyMarkedParallelAndClean(t *testing.T) {
	t.Parallel()
	plainBody(t)
}
`)
	if v := verdictFor(t, vs, "test", "TestAlreadyMarkedParallelButTakesTheProcess"); v.MarkedSerial {
		t.Fatalf("the case carries no marker")
	}
	bad := checkFailures(vs)
	if len(bad) != 1 || bad[0].Name != "TestAlreadyMarkedParallelButTakesTheProcess" {
		t.Fatalf("checkFailures = %+v; want the one case that asks to run beside the others while chdir-ing the process", bad)
	}
	if !strings.Contains(bad[0].Reason, "working directory") {
		t.Errorf("the failure should name what it touches, got %q", bad[0].Reason)
	}
}

// TestALocalThatShadowsAPackageVariableIsNotThatVariable is the difference between a rule and a blunt
// instrument: the compiler's own functions declare locals named `slots`, `seen`, `out`, and the package has a
// `slots` of its own. `:=` declares in the block, so none of those is a write to what the package shares — and
// reading a declaration as a write would have refused nearly every case that compiles anything.
func TestALocalThatShadowsAPackageVariableIsNotThatVariable(t *testing.T) {
	t.Parallel()
	vs, _ := runOn(t, `
func TestDeclaresALookalike(t *testing.T) {
	slots := map[string]string{}
	slots["a"] = "b"
	if slots["a"] != "b" {
		t.Fatal("the local did not fill")
	}
}

func TestWritesTheRealOne(t *testing.T) {
	switchboard = true
	if !switchboard {
		t.Fatal("the package variable did not change")
	}
}
`)
	if v := verdictFor(t, vs, "test", "TestDeclaresALookalike"); !v.Parallel {
		t.Errorf("a function-local `:=` that shares a name with a package variable was refused: %s", v.Reason)
	}
	if v := verdictFor(t, vs, "test", "TestWritesTheRealOne"); v.Parallel {
		t.Errorf("a plain `=` to the package variable itself was let through")
	}
}

// TestWriteTargetsOfEveryShapeAreWalked is a regression: the first version of the package-variable rule
// type-asserted its way through a write target and panicked on `rows[0].name = v`, which is a shape the
// suite's own table cases use. An analysis tool that panics on a legal file is worse than no analysis,
// because the natural response is to run it with the rule switched off.
func TestWriteTargetsOfEveryShapeAreWalked(t *testing.T) {
	t.Parallel()
	vs, _ := runOn(t, `
var rows []struct{ name string }
var table = map[string]struct{ n int }{}

func TestWritesThroughThings(t *testing.T) {
	rows = append(rows, struct{ name string }{"a"})
	rows[0].name = "b"
	table["k"] = struct{ n int }{1}
	p := new(int)
	*p = 3
	local := []int{1}
	local[0] = 2
	if len(rows) != 1 || table["k"].n != 1 || *p != 3 || local[0] != 2 {
		t.Fatal("the writes did not happen")
	}
}
`)
	v := verdictFor(t, vs, "test", "TestWritesThroughThings")
	if v.Parallel {
		t.Fatalf("a case that writes `rows` and `table`, both package-level, was marked parallel: %s", v.Reason)
	}
	if !strings.Contains(v.Reason, "package-level variable") {
		t.Errorf("the refusal should name what it found, got %q", v.Reason)
	}
}

// TestASyncOnceBodyMayWriteWhatItCaches pins the one package-level write that is safe beside another
// case: a build-the-tool-binary-once cache. Refusing it would have kept every CLI case serial over a
// lazy initialiser that is correct by construction.
func TestASyncOnceBodyMayWriteWhatItCaches(t *testing.T) {
	t.Parallel()
	vs, _ := runOn(t, `
var once2 sync.Once
var cached string

func cachedTool(t *testing.T) string {
	once2.Do(func() { cached = "built" })
	return cached
}

func TestUsesTheCache(t *testing.T) {
	if cachedTool(t) != "built" {
		t.Fatal("the cache did not fill")
	}
}
`)
	v := verdictFor(t, vs, "test", "TestUsesTheCache")
	if !v.Parallel {
		t.Errorf("a sync.Once-guarded lazy initialiser must not cost the case its parallelism: %s", v.Reason)
	}
}

// TestAVariableDeclaredOutsideTheTestFileIsStillAVariable is the blind spot that let a recorder's case run
// beside 300 others while it retargeted a file they were all reading: the analysis read only *_test.go, so a
// package-level variable declared in the tool's main.go did not exist, and a case assigning to it looked
// clean. The rule has to cover the package, not the file suffix.
func TestAVariableDeclaredOutsideTheTestFileIsStillAVariable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The production file: a package-level switch, and the helper the case reaches for.
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package suite2\n\nimport \"os\"\n\nvar target = \"real.json\"\n\nfunc retarget(p string) { target = p; _ = os.Stdout }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	body := "package suite2\n\nimport \"testing\"\n\nfunc TestRetargetsTheFile(t *testing.T) {\n\tretarget(\"mine.json\")\n}\n"
	formatted, err := format.Source([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main_test.go"), formatted, 0o600); err != nil {
		t.Fatal(err)
	}
	pkg, err := loadPackage(dir)
	if err != nil {
		t.Fatal(err)
	}
	pkg.analyze()
	vs, edits := pkg.plan()
	v := verdictFor(t, vs, "test", "TestRetargetsTheFile")
	if v.Parallel {
		t.Fatalf("a case that retargets a variable declared in main.go was let run beside the suite")
	}
	if !strings.Contains(v.Reason, "target") {
		t.Errorf("the refusal should name the variable, got %q", v.Reason)
	}
	if len(edits[filepath.Join(dir, "main_test.go")]) != 0 {
		t.Errorf("the tool planned an insertion into a case it had just refused")
	}
	// And it still refuses to touch anything but a test file.
	if len(edits[filepath.Join(dir, "main.go")]) != 0 {
		t.Errorf("the tool planned to rewrite production code")
	}
}

// TestTheShapesThatBrokeTheFirstVersion are the one-line bodies. gofmt writes a short closure as
// `func(t *testing.T) { oneCall() }`, all on one line, and an insertion that only knows "newline, indent,
// call" leaves `t.Parallel() oneCall()` sharing a line — which is not Go, and which the first version of
// this tool wrote to disk over a real test file. The line has to be a statement of its own whatever the
// body already looked like.
func TestTheShapesThatBrokeTheFirstVersion(t *testing.T) {
	t.Parallel()
	vs, out := runOn(t, `
func TestOneLiner(t *testing.T) { t.Log("short") }

func TestTableOfOneLiners(t *testing.T) {
	for _, tc := range []struct{ name string }{{"a"}} {
		t.Run(tc.name, func(t *testing.T) { plainBody(t) })
	}
}
`)
	if !strings.Contains(string(out), "t.Parallel()") {
		t.Fatalf("nothing was inserted:\n%s", out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "t.Parallel()") && strings.Contains(strings.TrimSpace(strings.SplitAfter(line, "t.Parallel()")[1]), "(") && !strings.HasPrefix(strings.TrimSpace(line), "//") {
			// `t.Parallel()` may legally be followed on the same line by nothing but a closing brace, which
			// gofmt itself produces; anything with another call on it is the bug this case exists for.
			tail := strings.TrimSpace(strings.SplitAfter(line, "t.Parallel()")[1])
			if tail != "}" && tail != "})" && tail != "" {
				t.Errorf("t.Parallel() shares a line with the statement that was already there: %s", line)
			}
		}
	}
	for _, name := range []string{"TestOneLiner", "TestTableOfOneLiners"} {
		if v := verdictFor(t, vs, "test", name); !v.Parallel {
			t.Errorf("%s was left serial: %s", name, v.Reason)
		}
	}
}

// TestInsertionLandsInsideTheBrace is the one mechanical risk the tool has: an offset off by one writes
// `t.Parallel()` into the wrong scope, which is a compile error at best and a silently misplaced
// statement at worst.
func TestInsertionLandsInsideTheBrace(t *testing.T) {
	t.Parallel()
	_, out := runOn(t, `
func TestPlacement(t *testing.T) {
	if 1+1 != 2 {
		t.Fatal("arithmetic")
	}
}
`)
	fset := token.NewFileSet()
	af, err := parser.ParseFile(fset, "suite_test.go", out, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range af.Decls {
		fd, ok := d.(*ast.FuncDecl)
		if !ok || fd.Name.Name != "TestPlacement" {
			continue
		}
		if len(fd.Body.List) == 0 {
			t.Fatal("the body is empty")
		}
		st, ok := fd.Body.List[0].(*ast.ExprStmt)
		if !ok {
			t.Fatalf("the first statement is not a call: %#v", fd.Body.List[0])
		}
		if !bodyCallsParallel(fd.Body) {
			t.Fatalf("the first statement is not t.Parallel(): %#v", st.X)
		}
		return
	}
	t.Fatal("TestPlacement disappeared")
}

// TestALocalNamedRunIsNotThePackagesRun is the callee half of the shadowing rule, and the case that produced
// it. A CLI case defines `run := func(…) string { … }` — a local that shells out to the built binary — and the
// package it lives in also has a helper named `run` that touches the process. Name-blind callee resolution
// walked into the *package's* `run` and refused the case for a call it never makes; and because the marker
// that answer bought also buys exemption from `-check`, the mistake was load-bearing rather than merely wrong.
// The paired case, which calls the package's `run`, is what stops this from being "assume `run` is safe".
func TestALocalNamedRunIsNotThePackagesRun(t *testing.T) {
	t.Parallel()
	vs, out := runOn(t, "func run() { os.Chdir(\"/tmp\") }\n\n"+
		"func TestShadowingLocalHelperIsNotThePackageOne(t *testing.T) {\n"+
		"\trun := func() string { return \"ok\" }\n"+
		"\tif run() == \"\" {\n\t\tt.Fatal(\"no output\")\n\t}\n}\n\n"+
		"func TestCallingThePackageHelper(t *testing.T) {\n\trun()\n}\n")
	got := verdictFor(t, vs, "test", "TestShadowingLocalHelperIsNotThePackageOne")
	if !got.Parallel || got.Violation != "" {
		t.Fatalf("a case whose only helper is a local closure sharing a package function's name was refused: %s\n%s", got.Reason, out)
	}
	if !strings.Contains(string(out), "func TestShadowingLocalHelperIsNotThePackageOne(t *testing.T) {\n\t"+parallelCall) {
		t.Fatalf("the shadowing case was not marked:\n%s", out)
	}
	bad := verdictFor(t, vs, "test", "TestCallingThePackageHelper")
	if bad.Parallel || bad.Violation != "" {
		t.Fatal("a case that calls the package's process-touching run() was marked parallel")
	}
	if !strings.Contains(bad.Reason, "run") || !strings.Contains(bad.Reason, "working directory") {
		t.Errorf("the case that really does call it must say so: %s", bad.Reason)
	}
}
