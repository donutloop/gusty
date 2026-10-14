// Command parify marks a package's test cases to run beside each other.
//
// # Why a tool does this rather than a person
//
// The suite is ~2200 top-level cases across 344 test files, and until ADR 0343 exactly none of them could
// carry `t.Parallel()`: a compiled program ran inside the test process with file descriptors 1 and 2
// `dup2`'d over for the duration of the call, so two cases running at once each captured a mixture of the
// other's output. That is why tools/testshards exists — it bought cores by running the same cases in N
// *processes* while each process still walked its cases one at a time.
//
// Once a run is a child process (pkg/lang/run_child.go) the last reason is gone, and what remains is the
// question a loop over 2200 cases cannot be trusted to answer by eye: which cases still touch something
// the process owns. A wrong answer there is not a crash — it is a case that passes while having asserted
// something else, the one failure mode a test suite must not have. So the answer is a rule, the rule is
// code, and the code has its own tests (main_test.go beside this file).
//
// # What it does
//
// For every `func TestXxx(t *testing.T)` it inserts `t.Parallel()` as the body's first statement, and for
// every `t.Run(name, func(t *testing.T) {...})` inside one it inserts the same in the subtest's body —
// unless the case is unsafe to overlap, in which case it is left alone and reported with the reason.
//
// Unsafe is computed, not guessed. A body is unsafe when it, or any function reachable from it in the same
// package, or any call on the known-unsafe list, does any of:
//
//   - changes the process's working directory (`os.Chdir`) — the import-from-cwd cases now pass their
//     directory to the compile and the run instead (IRGenOptions.Dir, JITOptions.Dir);
//   - edits the environment (`t.Setenv`, `os.Setenv`): process state, which `testing` itself forbids a
//     parallel case to touch;
//   - assigns a package-level variable — the toolchain-command overrides (`llcCmd`, `optCmd`), `StdlibDir`,
//     the collector's report switch; two cases would see each other's value;
//   - asks for the in-process execution entry (`JITOptions{InProcess: true}`, `dlopenRun`, the benchmark),
//     which takes fd 1 and fd 2 for the duration of a call and therefore cannot overlap with itself;
//   - reaches for the process's signal or scheduler surface (`signal.Notify`, `runtime.GOMAXPROCS`, ...).
//
// A subtest that writes through something the parent's body owns — `got = ...`, `seen[k] = ...` where
// `got`/`seen` are the parent's — keeps its siblings serial: that is a fixture racing with itself. (The
// loop variable that used to need `test := test` is per-iteration since Go 1.22 and needs nothing; this
// module declares go 1.22, so the shadowing line would be the bug, not the fix.)
//
// A case that is unsafe for a reason the analysis cannot see gets a `//parify:serial <reason>` comment
// above it and stays exactly as it is. The guard that keeps this honest is
// integration/parallelism_guard_test.go: it runs this analysis over the whole tree and fails the build on
// a Test function that is neither parallel nor marked — so "unsafe" always has a reason attached.
//
// Usage:
//
//	go run ./tools/parify ./pkg/...                              # report
//	go run ./tools/parify -apply ./pkg/...                        # rewrite in place
//	go run ./tools/parify -check ./pkg/... ./integration/...      # CI gate: exit 1 on drift
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// serialTag is the marker a case carries when it must keep running alone. It is a comment rather than a
// flag so the reason travels with the code that needs it.
const serialTag = "parify:serial"

func main() {
	var (
		apply   = flag.Bool("apply", false, "rewrite the files in place (default: report only)")
		check   = flag.Bool("check", false, "exit non-zero when a Test function is neither parallel nor marked serial")
		jsonOut = flag.Bool("json", false, "print the verdict per case as JSON")
		quiet   = flag.Bool("quiet", false, "print only the summary line")
	)
	flag.Parse()
	patterns := flag.Args()
	if len(patterns) == 0 {
		fmt.Fprintln(os.Stderr, "usage: parify [-apply] [-check] [-json] [-quiet] <dir or ./x/...>...")
		os.Exit(2)
	}
	dirs, err := expandDirs(patterns)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parify:", err)
		os.Exit(2)
	}
	var vs []verdict
	badOutput := false
	for _, dir := range dirs {
		pkg, err := loadPackage(dir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "parify: %v\n", err)
			os.Exit(1)
		}
		pkg.analyze()
		pv, editsByFile := pkg.plan()
		vs = append(vs, pv...)
		if *apply {
			for file, src := range pkg.src {
				ed := editsByFile[file]
				if len(ed) == 0 {
					continue
				}
				out, err := applyEdits(src, ed)
				if err != nil {
					fmt.Fprintf(os.Stderr, "parify: %s: %v\n", file, err)
					os.Exit(1)
				}
				// The tree is gofmt-clean and stays that way: the tool's own output goes through gofmt
				// before it touches the disk, and a file it cannot format is a file it refuses to write
				// rather than one it corrupts.
				out, err = format.Source(out)
				if err != nil {
					fmt.Fprintf(os.Stderr, "parify: %s: the rewrite does not format, refusing to write: %v\n", file, err)
					badOutput = true
					continue
				}
				if _, err := parser.ParseFile(token.NewFileSet(), file, out, 0); err != nil {
					fmt.Fprintf(os.Stderr, "parify: %s: the rewrite does not parse, refusing to write: %v\n", file, err)
					badOutput = true
					continue
				}
				if err := os.WriteFile(file, out, 0o644); err != nil {
					fmt.Fprintln(os.Stderr, "parify:", err)
					os.Exit(1)
				}
			}
		}
	}
	if badOutput {
		// A rewrite the tool could not format is a bug in the tool, and the tree is better left untouched
		// than left unparseable: nothing was written for those files.
		fmt.Fprintln(os.Stderr, "parify: at least one rewrite was refused — the analysis placed a line the file could not hold")
		os.Exit(1)
	}
	if *jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(vs)
	} else {
		report(vs, *quiet)
	}
	if *check {
		bad := len(checkFailures(vs))
		if bad > 0 {
			fmt.Fprintf(os.Stderr, "parify: %d test function(s) left serial with no marker\n", bad)
			os.Exit(1)
		}
	}
}

// verdict is one case's account of itself: what it does, and whether it may run beside its siblings.
type verdict struct {
	Pkg          string `json:"pkg"`
	File         string `json:"file"`
	Line         int    `json:"line"`
	Name         string `json:"name"`
	Kind         string `json:"kind"` // "test" or "subtest"
	Parent       string `json:"parent,omitempty"`
	Parallel     bool   `json:"parallel"`
	MarkedSerial bool   `json:"marked_serial"`
	// Justified records a case the tool leaves serial for a reason that is a property of the code rather
	// than an omission to fix — a subtest that writes its parent's fixture, or a case whose parent owns
	// the process for its duration. -check demands t.Parallel() everywhere else; these two shapes are
	// where demanding it would be wrong.
	Justified bool `json:"justified,omitempty"`
	// Violation is set when the file *claims* the case runs beside the others — it carries t.Parallel —
	// while the body reaches for something the process owns. The claim and the reading of the body are two
	// different facts, and the guard needs both to catch a case this tool's earlier, thinner version
	// parallelised by mistake.
	Violation string `json:"violation,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// edit is one insertion: text to place at a byte offset.
type edit struct {
	at   int
	text string
}

// applyEdits writes every insertion, last first so earlier offsets stay valid.
func applyEdits(src []byte, edits []edit) ([]byte, error) {
	sort.Slice(edits, func(i, j int) bool { return edits[i].at > edits[j].at })
	out := append([]byte(nil), src...)
	for _, e := range edits {
		if e.at < 0 || e.at > len(out) {
			return nil, fmt.Errorf("insertion offset %d is outside the file", e.at)
		}
		out = append(out[:e.at], append([]byte(e.text), out[e.at:]...)...)
	}
	return out, nil
}

// --- loading and analysing one package -------------------------------------------------------------

// pkgState is one directory's test files, read together: the rules are interprocedural, and the helper
// that decides whether a case may overlap another usually lives in a different file.
type pkgState struct {
	dir      string
	names    []string // the file paths, in a stable order
	src      map[string][]byte
	fset     *token.FileSet
	files    map[string]*ast.File
	funcs    map[string]*ast.FuncDecl   // package-level functions by name
	vars     map[string]bool            // package-level variable names
	unsafe   map[string]string          // function name → why it may not overlap
	locals   map[string]map[string]bool // function name → the names it declares for itself
	testFunc map[string]bool            // function name → defined in a *_test.go file
}

func loadPackage(dir string) (*pkgState, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	p := &pkgState{
		dir:    dir,
		src:    map[string][]byte{},
		fset:   token.NewFileSet(),
		files:  map[string]*ast.File{},
		funcs:  map[string]*ast.FuncDecl{},
		vars:   map[string]bool{},
		unsafe: map[string]string{},
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		af, err := parser.ParseFile(p.fset, path, raw, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("%s: %v", path, err)
		}
		// Every file of the package goes into the *symbol table* — the helpers a case calls and the
		// package-level variables it writes often live in the tool's main.go rather than in a test file, and
		// a rule that cannot see `recordPath` because it was declared next door is a rule that lets a case
		// run beside 300 others while it retargets a file they are all reading.
		for _, d := range af.Decls {
			switch d := d.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name != nil {
					p.funcs[d.Name.Name] = d
				}
			case *ast.GenDecl:
				if d.Tok != token.VAR {
					continue
				}
				for _, spec := range d.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, nm := range vs.Names {
						p.vars[nm.Name] = true
					}
				}
			}
		}
		if p.testFunc == nil {
			p.testFunc = map[string]bool{}
		}
		if strings.HasSuffix(e.Name(), "_test.go") {
			for _, d := range af.Decls {
				if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name != nil {
					p.testFunc[fd.Name.Name] = true
				}
			}
		}
		// Only the test files are ever *rewritten*: the tool adds a line to a case, never to production code.
		if !strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		p.names = append(p.names, path)
		p.src[path] = raw
		p.files[path] = af
	}
	sort.Strings(p.names)
	return p, nil
}

// analyze builds the package's function and variable tables and runs the unsafe-reachability fixpoint.
// analyze works out which helpers a case may not call. The symbol table (p.funcs, p.vars) was filled by
// loadPackage over every file of the package; what is left is the transitive closure.
func (p *pkgState) analyze() {
	// Fixpoint, growing only: a helper is unsafe when its body calls something already known unsafe. The
	// loop ends because each pass adds facts and no pass ever removes one.
	for {
		added := false
		for name, fd := range p.funcs {
			if _, known := p.unsafe[name]; known || isTestFunc(fd) {
				continue
			}
			if why := p.bodyReason(fd.Body, map[string]bool{name: true}); why != "" && !isTestFunc(fd) {
				p.unsafe[name] = why
				added = true
			}
		}
		if !added {
			return
		}
	}
}

// knownUnsafe names the calls that reach process state, with the reason a reader gets back. Every entry
// is something two cases genuinely cannot do at once, which is the bar this list has to clear.
var knownUnsafe = map[string]string{
	"os.Chdir":               "changes the process's working directory",
	"os.Setenv":              "edits the process environment",
	"os.Unsetenv":            "edits the process environment",
	"os.Clearenv":            "edits the process environment",
	"os.Exit":                "ends the process",
	"signal.Notify":          "installs a process-wide signal handler",
	"signal.NotifyContext":   "installs a process-wide signal handler",
	"signal.Reset":           "installs a process-wide signal handler",
	"runtime.GOMAXPROCS":     "changes the process's scheduler",
	"runtime.LockOSThread":   "pins this OS thread for the process",
	"runtime.UnlockOSThread": "pins this OS thread for the process",
	"log.SetOutput":          "replaces the process's logger output",
	"log.SetPrefix":          "replaces the process's logger prefix",
	"log.Fatal":              "exits the process",
	"log.Fatalf":             "exits the process",
	"log.Panic":              "panics across the process",
	"SetStdlibDir":           "replaces the process-wide standard-library root",
	"lang.SetStdlibDir":      "replaces the process-wide standard-library root",
	"SetGCReport":            "toggles the process-wide collector report switch",
	"lang.SetGCReport":       "toggles the process-wide collector report switch",
}

// knownInTestCode is the second tier: entries that are process-wide *when a case reaches for them*, and
// production code that merely contains a branch to them. `dlopenRun` is the case in point — the runner calls
// it when a caller asked for `InProcess: true`, and every run in the suite passes through that function on
// its way to a child process. Refusing anything that can reach it would refuse every compiled case in the
// tree for a branch it never takes; refusing the case that walks to it through its own helpers is the rule
// that actually means something. So these are followed only through helpers defined in a *_test.go file.
var knownInTestCode = map[string]string{
	"dlopenRun":    "runs a program on this process's fd 1 and fd 2 (ADR 0343)",
	"benchSO":      "runs a shared object inside this process",
	"BenchProgram": "runs the benchmark harness, which dlopens",
	"Bench":        "runs the benchmark harness, which dlopens",
	"BenchSource":  "runs the benchmark harness, which dlopens",
}

// knownUnsafeMethods are the methods a parallel case may not call at all. `testing` panics on the first
// one, which is a kind way of saying the same thing this map records.
var knownUnsafeMethods = map[string]string{
	"Setenv": "edits the process environment, which testing forbids on a parallel case",
	"Chdir":  "changes the process's working directory, which testing forbids on a parallel case",
}

// bodyReason answers "what in this body would a second concurrent case notice?", following package-level
// calls to their own verdicts. `seen` is the recursion guard a helper that calls itself needs.
func (p *pkgState) bodyReason(b *ast.BlockStmt, seen map[string]bool) string {
	return p.why(b, seen, false)
}

// bodyReasonOf is bodyReason for a body read as part of a function that declares `locals` — the case's own
// body, or a subtest closure inside one. Without the set a function-local `slots` reads as the package's
// `slots`, which is the same mistake as not reading the body at all, only louder.
func (p *pkgState) bodyReasonOf(b *ast.BlockStmt, seen map[string]bool, locals map[string]bool) string {
	return p.whyLocal(b, seen, false, locals)
}

// why is the walk behind bodyReason. `inOnce` says the code is inside a `sync.Once` body, which is the
// one way a package-level write is legitimate: the write happens exactly once, every reader is ordered
// after it, and two cases that "race" to it both see the same value. Refusing those would have made half
// the CLI tests serial over a lazy `build the binary once` cache.
func (p *pkgState) why(b *ast.BlockStmt, seen map[string]bool, inOnce bool) string {
	return p.whyLocal(b, seen, inOnce, nil)
}

// whyLocal is the walk, with the set of names the function being read declares for itself. A name the
// function declares is never the package's, however much the package also calls it that: `slots :=
// map[string]string{}` in a codegen helper and `var slots chan struct{}` in the tool governor are two
// different things, and reading one as the other refused every case that compiles anything. The set spans
// the whole function, closures included, because a closure writing a name its enclosing function declared
// is writing that local and not the package's.
func (p *pkgState) whyLocal(b *ast.BlockStmt, seen map[string]bool, inOnce bool, locals map[string]bool) string {
	if b == nil {
		return ""
	}
	if seen == nil {
		seen = map[string]bool{}
	}
	guarded := onceGuardedLits(b)
	locked := lockIntervals(b)
	why := ""
	var visit func(n ast.Node) bool
	visit = func(n ast.Node) bool {
		if why != "" {
			return false
		}
		switch x := n.(type) {
		case *ast.SelectorExpr:
			if r, ok := knownUnsafe[selectorName(x)]; ok {
				why = r
			}
		case *ast.CallExpr:
			name := calleeName(x)
			if r, ok := knownUnsafe[name]; ok {
				why = r
				return false
			}
			if p.testFunc[name] {
				// Reached through a helper the test files themselves define, so this is a case reaching
				// for the process, not production code containing a branch it does not take.
				if r, ok := knownInTestCode[name]; ok {
					why = r
					return false
				}
			}
			if sel, ok := callSelector(x); ok {
				if r, ok := knownUnsafeMethods[sel]; ok && receiverLooksLikeT(x) {
					why = r
					return false
				}
			}
			if fd, ok := p.funcs[name]; ok && !seen[name] && !isTestFunc(fd) {
				seen[name] = true
				if r := p.whyLocal(fd.Body, seen, inOnce, p.localNames(fd)); r != "" {
					why = name + " → " + r
				}
				delete(seen, name)
			}
		case *ast.KeyValueExpr:
			if id, ok := x.Key.(*ast.Ident); ok && id.Name == "InProcess" {
				why = "asks for the in-process execution entry, which owns fd 1 and fd 2"
			}
		case *ast.FuncLit:
			// A closure runs as part of this body, so what it does the body is answerable for — unless
			// it is a sync.Once body, whose package writes are the point of a Once.
			if r := p.whyLocal(x.Body, seen, guarded[x], locals); r != "" {
				why = r
			}
			return false
		case *ast.AssignStmt:
			if inOnce || holdsALock(locked, b.Pos(), x.Pos()) {
				return true
			}
			// `:=` *declares*: the names on its left belong to this block, whatever the package also calls.
			// `slots := map[string]string{}` inside a function is a local that shadows the package's `slots`,
			// not a write to it, and Go will not let `:=` reach outward — so treating a declaration as a write
			// would refuse most of the compiler's own functions for a name they never touched. Only `=` can
			// write a variable the package owns.
			if x.Tok == token.DEFINE {
				return true
			}
			for _, lhs := range x.Lhs {
				if id, ok := rootIdent(lhs); ok && p.vars[id.Name] && !locals[id.Name] && id.Name != "_" {
					why = "assigns the package-level variable " + id.Name
				}
			}
		case *ast.IncDecStmt:
			if inOnce || holdsALock(locked, b.Pos(), x.Pos()) {
				return true
			}
			if id, ok := x.X.(*ast.Ident); ok && p.vars[id.Name] && !locals[id.Name] {
				why = "assigns the package-level variable " + id.Name
			}
		}
		return true
	}
	ast.Inspect(b, visit)
	return why
}

// localNames answers "which names does this function declare for itself?", cached because the analysis
// walks the same helpers hundreds of times on its way over 2,800 cases.
func (p *pkgState) localNames(fd *ast.FuncDecl) map[string]bool {
	if m, ok := p.locals[fd.Name.Name]; ok {
		return m
	}
	m := map[string]bool{}
	addFields := func(fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			for _, n := range f.Names {
				m[n.Name] = true
			}
		}
	}
	addFields(fd.Type.Params)
	addFields(fd.Type.Results)
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, lhs := range x.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						m[id.Name] = true
					}
				}
			}
		case *ast.DeclStmt:
			if gd, ok := x.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
				for _, sp := range gd.Specs {
					if vs, ok := sp.(*ast.ValueSpec); ok {
						for _, nm := range vs.Names {
							m[nm.Name] = true
						}
					}
				}
			}
		case *ast.RangeStmt:
			if x.Tok == token.DEFINE {
				if id, ok := x.Key.(*ast.Ident); ok {
					m[id.Name] = true
				}
				if id, ok := x.Value.(*ast.Ident); ok {
					m[id.Name] = true
				}
			}
		}
		return true
	})
	if p.locals == nil {
		p.locals = map[string]map[string]bool{}
	}
	p.locals[fd.Name.Name] = m
	return m
}

// onceGuardedLits collects the function literals handed to a `.Do(...)` call, which is the shape of a
// sync.Once body. Naming it is deliberately syntactic: the analysis has no type checker here, and the
// `.Do(func(){...})` spelling is what a Once body looks like in every file that has one.
func onceGuardedLits(b *ast.BlockStmt) map[*ast.FuncLit]bool {
	out := map[*ast.FuncLit]bool{}
	ast.Inspect(b, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Do" {
			return true
		}
		for _, a := range call.Args {
			if fl, ok := a.(*ast.FuncLit); ok {
				out[fl] = true
			}
		}
		return true
	})
	return out
}

// lockIntervals reads a function body for the mutex idiom: the [Lock, Unlock) spans of every named lock
// in it.
//
// This is the second half of the package-variable rule. Writing a package-level variable from two cases is
// a data race, and the suite's ledgers (the compiled-gap ledger, the reference-debt ledger) do exactly
// that — under a mutex, which is what makes it fine. Refusing those would have kept three quarters of the
// conformance cases serial over code that is correct by construction, so the rule reads the idiom instead:
// a write that sits between a `m.Lock()` and its `m.Unlock()` (or after a `defer m.Unlock()`) is a locked
// write. An unlocked write to the same variable elsewhere in the same body is still refused — the spans
// are per Lock call, not per function.
func lockIntervals(b *ast.BlockStmt) []tokenInterval {
	type event struct {
		pos    token.Pos
		unlock bool
	}
	// A deferred Unlock does not close a span: `defer m.Unlock()` holds the lock to the end of the body,
	// which is the whole reason the idiom exists. Reading it as a closer is how every
	// `Lock(); defer Unlock(); ledger[k] = v` in the suite would have been refused.
	deferred := map[token.Pos]bool{}
	ast.Inspect(b, func(n ast.Node) bool {
		ds, ok := n.(*ast.DeferStmt)
		if ok {
			deferred[ds.Call.Pos()] = true
		}
		return true
	})
	var events []event
	ast.Inspect(b, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}

		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		switch sel.Sel.Name {
		case "Lock", "RLock", "TryLock":
			events = append(events, event{pos: call.Pos()})
		case "Unlock", "RUnlock":
			if !deferred[call.Pos()] {
				// A *deferred* Unlock holds the lock to the end of the body, so it closes nothing — the
				// span stays open, which is what the defer means.
				events = append(events, event{pos: call.Pos(), unlock: true})
			}
		}
		return true
	})
	var spans []tokenInterval
	var open token.Pos
	have := false
	for _, e := range events {
		if !e.unlock {
			if !have {
				open, have = e.pos, true
			}
			continue
		}
		if have {
			spans = append(spans, tokenInterval{open, e.pos})
			have = false
		}
	}
	if have {
		// A `defer m.Unlock()` leaves the span open to the end of the body — which is what a defer says.
		spans = append(spans, tokenInterval{open, b.End()})
	}
	return spans
}

type tokenInterval struct{ from, to token.Pos }

// holdsALock says whether a write sits inside one of the body's locked spans.
func holdsALock(spans []tokenInterval, from, at token.Pos) bool {
	for _, s := range spans {
		if at >= s.from && at <= s.to && at >= from {
			return true
		}
	}
	return false
}

// rootIdent is the bare name a write target is rooted in, for the package-variable rule.
func rootIdent(e ast.Expr) (*ast.Ident, bool) {
	for {
		switch x := e.(type) {
		case *ast.IndexExpr:
			e = x.X
			continue
		case *ast.SelectorExpr:
			// `m["k"].field = v` is a write through m; `pkg.X = v` has a package name underneath, which
			// is never one of this package's own variables, so the rule simply does not fire.
			return rootIdent(x.X)
		case *ast.StarExpr:
			e = x.X
			continue
		case *ast.UnaryExpr:
			e = x.X
			continue
		case *ast.ParenExpr:
			e = x.X
			continue
		}
		break
	}
	id, ok := e.(*ast.Ident)
	return id, ok && id.Name != "_"
}

// parallelCall is the one line the tool adds to a case.
const parallelCall = "t.Parallel()"

// insertion builds the text that puts t.Parallel() first in a body, whatever shape the body was written
// in. The naive "newline, indent, call" is wrong for the shape gofmt itself produces for a short closure —
// `func(t *testing.T) { oneCall() }` all on one line — where inserting after the brace leaves
// `t.Parallel() oneCall()` sharing a line, which does not compile. The rule is that the call is a
// statement, so it gets a line before it *and* a line after it whenever the body's first statement was
// going to crowd onto its line.
func insertion(src []byte, at int, lbraceLineIndent string) string {
	before := "\n" + lbraceLineIndent + "\t" + parallelCall
	if at >= len(src) || src[at] == '\n' {
		return before
	}
	// The first statement starts on the brace's line: give the call its own line and hand the statement
	// the indentation it had, so gofmt has a well-formed file to tidy rather than a syntax error.
	return before + "\n" + lbraceLineIndent + "\t"
}

// --- planning the insertions ------------------------------------------------------------------------

// plan walks every Test function and decides, for it and for each of its subtests, whether it runs
// beside the others. It also returns the text insertions -apply would make.
func (p *pkgState) plan() ([]verdict, map[string][]edit) {
	var vs []verdict
	edits := map[string][]edit{}
	for _, path := range p.names {
		src := p.src[path]
		for _, d := range p.files[path].Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv != nil || !isTestFunc(fd) {
				continue
			}
			line := p.fset.Position(fd.Pos()).Line
			v := verdict{
				Pkg: filepath.Base(p.dir), File: path, Line: line,
				Name: fd.Name.Name, Kind: "test",
				Parallel:     bodyCallsParallel(fd.Body),
				MarkedSerial: hasSerialMark(p.fset.Position(fd.Pos()).Line, src),
			}
			// The analysis runs whether or not the case already says t.Parallel. That sentence is a claim,
			// not evidence: the suite was once rewritten by this tool's first version, whose rules were
			// thinner than these, and a checker that took the call for proof would have certified every case
			// that version got wrong — including the ones that overwrite the toolchain's `llc` out from under
			// every compile running beside them. So the body is read either way, and a case that asks to run
			// beside the others while touching the process is a failure in its own right.
			says := v.Parallel
			why := p.testReason(fd)
			v.Parallel = says && why == ""
			switch {
			case v.MarkedSerial:
				v.Reason = "marked //" + serialTag
				if says {
					// The marker is a promise to run alone. A case that carries it *and* asks to run beside
					// the others has broken the promise, and the marker would otherwise buy it immunity from
					// the check that exists to notice.
					v.Violation = "is marked //" + serialTag + " and still calls " + parallelCall
				}
			case why != "":
				v.Reason = why
				if says {
					// The file says the case runs beside the others and the body says it owns something the
					// others need. That is a failure, not a case to leave alone: it is how a suite goes from
					// "two cases at once" to "one of them asserts something about the other".
					v.Violation = "carries " + parallelCall + " while it " + why
				}
			case !says:
				v.Parallel = true
				v.Reason = "parallel"
				pos := p.fset.Position(fd.Body.Lbrace)
				edits[path] = append(edits[path], edit{
					at:   pos.Offset + 1,
					text: insertion(src, pos.Offset+1, indentOf(src, pos.Line)),
				})
			default:
				v.Reason = "parallel"
			}
			vs = append(vs, v)
			if !v.Parallel {
				// A serial parent owns the process for its duration; asking its subtests to overlap
				// would be asking them to share what the parent just took.
				continue
			}
			vs = append(vs, p.planSubtests(path, src, fd, edits)...)
		}
	}
	return vs, edits
}

// testReason is the test's own verdict plus the verdict of every helper it reaches.
func (p *pkgState) testReason(fd *ast.FuncDecl) string {
	if why := p.bodyReasonOf(fd.Body, map[string]bool{fd.Name.Name: true}, p.localNames(fd)); why != "" {
		return why
	}
	// A package-level variable the test assigns is the same fault with the roles reversed: the test is
	// the one editing what every other case reads — which the walk above already answers, with the local
	// shadowing, the mutex idiom and the sync.Once exception applied. The second, dumber pass that used to
	// sit here flagged any assignment to a name the package also declares, including the locals that merely
	// share a spelling, and refused half the cases that compile anything.
	return ""
}

// planSubtests decides each t.Run inside a parallel parent, and queues the insertions for the ones that
// pass.
func (p *pkgState) planSubtests(path string, src []byte, fd *ast.FuncDecl, edits map[string][]edit) []verdict {
	outer := declaredNames(fd.Body)
	var vs []verdict
	ast.Inspect(fd.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isTRun(call) {
			return true
		}
		clo := closureArg(call)
		if clo == nil {
			return true
		}
		v := verdict{
			Pkg: filepath.Base(p.dir), File: path,
			Line:     p.fset.Position(call.Pos()).Line,
			Name:     runName(call),
			Kind:     "subtest",
			Parent:   fd.Name.Name,
			Parallel: bodyCallsParallel(clo.Body),
		}
		// Read the closure whether or not it already carries the call, for the same reason as the parent.
		why := p.bodyReasonOf(clo.Body, map[string]bool{}, p.localNames(fd))
		says := v.Parallel
		writes := sharedWrite(outer, clo.Body)
		v.Parallel = says && why == "" && !writes
		switch {
		case writes && !says:
			v.Reason = "writes through something the parent's body owns"
			v.Justified = true
		case why != "":
			v.Reason = why
			if says {
				v.Violation = "carries " + parallelCall + " while it " + why
			}
		case writes:
			v.Reason = "writes through something the parent's body owns"
			v.Violation = "carries " + parallelCall + " while it writes a name its parent's body owns"
		case !says:
			v.Parallel = true
			v.Reason = "parallel"
			pos := p.fset.Position(clo.Body.Lbrace)
			edits[path] = append(edits[path], edit{
				at:   pos.Offset + 1,
				text: insertion(src, pos.Offset+1, indentOf(src, pos.Line)),
			})
		default:
			v.Reason = "parallel"
		}
		vs = append(vs, v)
		return true
	})
	return vs
}

// declaredNames collects the names a body binds at its own level — the ones a subtest could accidentally
// write through. Bindings inside a nested closure are that closure's own and are not collected.
func declaredNames(b *ast.BlockStmt) map[string]bool {
	out := map[string]bool{}
	var walk func(*ast.BlockStmt)
	walk = func(bl *ast.BlockStmt) {
		if bl == nil {
			return
		}
		ast.Inspect(bl, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.FuncLit:
				return false
			case *ast.AssignStmt:
				if x.Tok == token.DEFINE {
					for _, lhs := range x.Lhs {
						if id, ok := lhs.(*ast.Ident); ok {
							out[id.Name] = true
						}
					}
				}
			case *ast.DeclStmt:
				if gd, ok := x.Decl.(*ast.GenDecl); ok && gd.Tok == token.VAR {
					for _, s := range gd.Specs {
						if vs, ok := s.(*ast.ValueSpec); ok {
							for _, n := range vs.Names {
								out[n.Name] = true
							}
						}
					}
				}
			case *ast.RangeStmt:
				if id, ok := x.Key.(*ast.Ident); ok && x.Tok == token.DEFINE {
					out[id.Name] = true
				}
				if id, ok := x.Value.(*ast.Ident); ok && x.Tok == token.DEFINE {
					out[id.Name] = true
				}
			}
			return true
		})
	}
	walk(b)
	return out
}

// sharedWrite reports whether a subtest body writes a name the parent bound. `got = ...`, `seen[k] = ...`,
// `rec.field = ...` where the base is the parent's: the fixture is being mutated by its own cases, which is
// a race no amount of `t.Parallel()` placement can make safe.
func sharedWrite(outer map[string]bool, body *ast.BlockStmt) bool {
	// Names the closure binds for itself shadow the parent's — `got := ...` inside the case is a
	// different variable, so a later `got = ...` is its own business.
	bound := map[string]bool{}
	ast.Inspect(body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				for _, lhs := range x.Lhs {
					if id, ok := lhs.(*ast.Ident); ok {
						bound[id.Name] = true
					}
				}
			}
		}
		return true
	})
	hit := false
	ast.Inspect(body, func(n ast.Node) bool {
		if hit {
			return false
		}
		switch x := n.(type) {
		case *ast.FuncLit:
			return false
		case *ast.AssignStmt:
			if x.Tok == token.DEFINE {
				return true
			}
			for _, lhs := range x.Lhs {
				if writesOuter(outer, bound, lhs) {
					hit = true
				}
			}
		case *ast.IncDecStmt:
			if writesOuter(outer, bound, x.X) {
				hit = true
			}
		case *ast.SendStmt:
			if writesOuter(outer, bound, x.Chan) {
				hit = true
			}
		}
		return true
	})
	return hit
}

// writesOuter looks through the shapes a write can take and asks whether the root name is the parent's.
func writesOuter(outer, bound map[string]bool, e ast.Expr) bool {
	return rootIsOuter(outer, bound, e)
}

// rootIsOuter peels indexing, field selection and indirection off a write target and asks whose name is
// underneath. `seen[k] = v`, `rec.f = v`, `*p = v` are each a write to the thing named at the root.
func rootIsOuter(outer, bound map[string]bool, e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.IndexExpr:
		return rootIsOuter(outer, bound, x.X)
	case *ast.IndexListExpr:
		return rootIsOuter(outer, bound, x.X)
	case *ast.StarExpr:
		return rootIsOuter(outer, bound, x.X)
	case *ast.ParenExpr:
		return rootIsOuter(outer, bound, x.X)
	case *ast.UnaryExpr:
		// `&x` handed to something is a write waiting to happen: treat it as shared.
		return rootIsOuter(outer, bound, x.X)
	case *ast.SelectorExpr:
		// `x.f = v` where x is the parent's is shared; `pkg.X = v` is a package-level write the
		// caller's own analysis covers.
		if id, ok := x.X.(*ast.Ident); ok {
			return outer[id.Name] && !bound[id.Name]
		}
		return false
	}
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	return outer[id.Name] && !bound[id.Name]
}

// --- syntax helpers ---------------------------------------------------------------------------------

func isTestFunc(fd *ast.FuncDecl) bool {
	if fd == nil || fd.Name == nil || fd.Body == nil || fd.Recv != nil {
		return false
	}
	n := fd.Name.Name
	if !strings.HasPrefix(n, "Test") || n == "TestMain" {
		return false
	}
	return hasTestingT(fd.Type)
}

func hasTestingT(sig *ast.FuncType) bool {
	if sig == nil || sig.Params == nil {
		return false
	}
	for _, f := range sig.Params.List {
		switch typeString(f.Type) {
		case "*testing.T", "*testing.B", "*testing.F", "*testing.TB", "testing.TB":
			return true
		}
	}
	return false
}

// typeString renders the handful of type shapes a test signature is written with.
func typeString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.StarExpr:
		return "*" + typeString(x.X)
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name + "." + x.Sel.Name
		}
	}
	return "?"
}

func bodyCallsParallel(b *ast.BlockStmt) bool {
	if b == nil || len(b.List) == 0 {
		return false
	}
	st, ok := b.List[0].(*ast.ExprStmt)
	if !ok {
		return false
	}
	call, ok := st.X.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Parallel"
}

func isTRun(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "Run" && len(call.Args) >= 2
}

func closureArg(call *ast.CallExpr) *ast.FuncLit {
	for _, a := range call.Args {
		if clo, ok := a.(*ast.FuncLit); ok && clo.Body != nil {
			return clo
		}
	}
	return nil
}

func runName(call *ast.CallExpr) string {
	if lit, ok := call.Args[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if s, err := strconv.Unquote(lit.Value); err == nil {
			return s
		}
		return lit.Value
	}
	return exprText(call.Args[0])
}

func selectorName(x *ast.SelectorExpr) string {
	if id, ok := x.X.(*ast.Ident); ok {
		return id.Name + "." + x.Sel.Name
	}
	return "?" + "." + x.Sel.Name
}

func calleeName(call *ast.CallExpr) string {
	switch f := call.Fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return selectorName(f)
	}
	return ""
}

func callSelector(call *ast.CallExpr) (string, bool) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	return sel.Sel.Name, true
}

// receiverLooksLikeT keeps the method blacklist honest: `t.Setenv` is forbidden on a parallel case,
// while some other receiver's Setenv method is an ordinary call someone else owns.
func receiverLooksLikeT(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	return id.Name == "t" || id.Name == "tb" || id.Name == "tt" || id.Name == "tc"
}

func exprText(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		if id, ok := x.X.(*ast.Ident); ok {
			return id.Name + "." + x.Sel.Name
		}
	case *ast.BasicLit:
		return x.Value
	}
	return "?"
}

// indentOf is the leading whitespace of a file's line, which is what the inserted statement inherits.
// Doing it from the source rather than through gofmt keeps the diff exactly one line per case.
func indentOf(src []byte, line int) string {
	lines := bytes.Split(src, []byte("\n"))
	if line < 1 || line > len(lines) {
		return "\t"
	}
	l := lines[line-1]
	i := 0
	for i < len(l) && (l[i] == ' ' || l[i] == '\t') {
		i++
	}
	return string(l[:i])
}

// hasSerialMark looks for the marker in the comment block directly above a declaration.
func hasSerialMark(declLine int, src []byte) bool {
	lines := bytes.Split(src, []byte("\n"))
	for i := declLine - 2; i >= 0; i-- {
		l := strings.TrimSpace(string(lines[i]))
		if l == "" || !strings.HasPrefix(l, "//") {
			return false
		}
		if strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(l, "//")), serialTag) {
			return true
		}
	}
	return false
}

// checkFailures is the -check verdict list trimmed to what must be fixed: every case that is neither
// parallel, nor marked serial with a reason, nor excused by the shape of its code. It is a function rather
// than a loop in main so the ratchet itself has a test — a guard that has never been shown to fail is the
// kind of guard that gets deleted, and the one before this had never seen a bad input.
func checkFailures(vs []verdict) []verdict {
	var bad []verdict
	for _, v := range vs {
		if v.MarkedSerial || v.Justified || (v.Parallel && v.Violation == "") {
			continue
		}
		bad = append(bad, v)
		switch {
		case v.Violation != "":
			fmt.Fprintf(os.Stderr, "parify: %s:%d %s %s — remove the %s, or fix what it touches, or add //%s <reason> and drop the %s\n",
				v.File, v.Line, v.Name, v.Violation, parallelCall, serialTag, parallelCall)
		default:
			fmt.Fprintf(os.Stderr, "parify: %s:%d %s is not marked to run beside the others — %s (add //%s <reason> to keep it serial on purpose)\n",
				v.File, v.Line, v.Name, v.Reason, serialTag)
		}
	}
	return bad
}

func report(vs []verdict, quiet bool) {
	var tests, subs, parallel, serial, marked int
	byReason := map[string]int{}
	for _, v := range vs {
		if v.Kind == "test" {
			tests++
		} else {
			subs++
		}
		switch {
		case v.Parallel:
			parallel++
		case v.MarkedSerial:
			marked++
		default:
			serial++
			byReason[v.Reason]++
		}
	}
	fmt.Printf("parify: %d test(s), %d subtest(s) — %d parallel, %d serial, %d marked serial\n",
		tests, subs, parallel, serial, marked)
	if quiet {
		return
	}
	type row struct {
		reason string
		n      int
	}
	var rows []row
	for r, n := range byReason {
		rows = append(rows, row{r, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].n != rows[j].n {
			return rows[i].n > rows[j].n
		}
		return rows[i].reason < rows[j].reason
	})
	for i, r := range rows {
		if i >= 14 {
			fmt.Printf("   %5d  (%d more reasons)\n", 0, len(rows)-14)
			break
		}
		fmt.Printf("   %5d  %s\n", r.n, r.reason)
	}
}

// expandDirs turns ./x/... patterns into directories, using `go list` so the answer is the one `go test`
// would work from.
func expandDirs(patterns []string) ([]string, error) {
	args := append([]string{"list", "-f", "{{.Dir}}"}, patterns...)
	cmd := exec.Command("go", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go list %s: %v\n%s", strings.Join(patterns, " "), err, out.String())
	}
	var dirs []string
	seen := map[string]bool{}
	for _, l := range strings.Split(out.String(), "\n") {
		s := strings.TrimSpace(l)
		if s != "" && !seen[s] {
			seen[s] = true
			dirs = append(dirs, s)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}
