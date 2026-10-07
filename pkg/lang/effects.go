package lang

// Effect / async exhaustiveness (roadmap Phase 7, L7.6; ADR 0195).
//
// Calling an `async def` is a *deferred* call: it builds a coroutine object and
// runs nothing. The body runs when that coroutine is awaited — exactly once.
// Neither backend enforces the discipline, and each fails differently: the
// interpreter prints the coroutine handle (`print(f(2))` answers `<coro>`), the
// compiled backend runs the body eagerly at the call (answers `2`), and CPython
// prints a coroutine repr and raises a RuntimeWarning. Three answers to one
// program — and no two-backend parity test can ever see it, because both
// backends "work".
//
// So the proof lives in the checker. For every function (and the module) the
// pass computes an *effect signature* — which effects the body performs, whether
// it returns a value, whether its control flow can run off the end — and then
// proves the await/return discipline over that signature:
//
//   - a coroutine nobody ever awaits means the body never runs:
//     `async.coro.never_awaited`, not a handle printed as a number;
//   - the same coroutine awaited twice runs twice in one backend and once in the
//     other (CPython raises RuntimeError): `async.coro.awaited_twice`;
//   - `await` / `async for` / `async with` only mean something inside a
//     coroutine: `async.await.outside_coroutine`, `async.async_stmt.outside_coroutine`;
//   - `await`ing something provably not a coroutine is a no-op here and a
//     TypeError in Python: `async.await.not_coroutine`;
//   - an `async def` that yields is an async generator, which neither backend
//     lowers (the record loops forever): `async.generator.unsupported`;
//   - an `async def` path that runs off the end awaits to None while its other
//     paths await to a value: `async.missing_return`.
//
// The signature is also the machine path: `gustyc --effects` prints it as JSON
// and `--schema` declares `definitions.effectSummary`, so an agent can ask what
// a function does — awaits, yields, raises, returns, terminates — without
// running it or reading its source.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ---------------------------------------------------------------------------
// Diagnostic codes (stable; the table lives in docs/operations.md)
// ---------------------------------------------------------------------------

const (
	// CodeCoroNeverAwaited: a coroutine was created and nothing ever awaited it,
	// so the body it was meant to run never runs.
	CodeCoroNeverAwaited = "async.coro.never_awaited"
	// CodeCoroAwaitedTwice: one coroutine object is awaited twice on a path.
	CodeCoroAwaitedTwice = "async.coro.awaited_twice"
	// CodeAwaitOutsideCoroutine: `await` in a body that is not an `async def`.
	CodeAwaitOutsideCoroutine = "async.await.outside_coroutine"
	// CodeAsyncStmtOutsideCoroutine: `async for` / `async with` in a body that is
	// not an `async def`.
	CodeAsyncStmtOutsideCoroutine = "async.async_stmt.outside_coroutine"
	// CodeAwaitNotCoroutine: `await` applied to a value that provably is not a
	// coroutine.
	CodeAwaitNotCoroutine = "async.await.not_coroutine"
	// CodeAsyncGeneratorUnsupported: an `async def` whose body yields.
	CodeAsyncGeneratorUnsupported = "async.generator.unsupported"
	// CodeAsyncMissingReturn: an `async def` path runs off the end while other
	// paths — or the return annotation — promise a value.
	CodeAsyncMissingReturn = "async.missing_return"
)

// ---------------------------------------------------------------------------
// The effect signature
// ---------------------------------------------------------------------------

// Effect names are stable JSON strings: they are what `gustyc --effects` prints
// and what an agent branches on instead of reading source.
const (
	EffectAwait = "await" // the body contains `await`
	EffectYield = "yield" // the body contains `yield` or `yield from`
	EffectRaise = "raise" // the body contains `raise`
)

// EffectSummary is the effect signature of one function body, or of the module
// top level (`Function` is "<module>"). The checker decides its diagnostics from
// exactly these fields, so the human-facing rules and the machine-facing table can
// not drift apart.
type EffectSummary struct {
	Function string `json:"function"`
	// Async is true for an `async def`: calling it builds a coroutine.
	Async bool `json:"async"`
	// Line is the line of the declaration (0 for <module>).
	Line int `json:"line"`
	// Effects is the sorted set of effect names the body performs.
	Effects []string `json:"effects"`
	// Effect sites, counted once per syntactic occurrence (a signature, not a
	// profile: a loop body counts once).
	Awaits         int `json:"awaits"`
	Yields         int `json:"yields"`
	Raises         int `json:"raises"`
	CoroutineCalls int `json:"coroutine_calls"`
	// ReturnsValue: some path executes `return <expr>`.
	ReturnsValue bool `json:"returns_value"`
	// ReturnsBare: some path executes a bare `return`.
	ReturnsBare bool `json:"returns_bare"`
	// FallsThrough: control flow can run off the end of the body, which yields
	// None to the caller (or to the awaiter).
	FallsThrough bool `json:"falls_through"`
	// Terminates is !FallsThrough: every path leaves the body through
	// return/raise/break/continue.
	Terminates bool `json:"terminates"`
}

// EffectsDocument is the JSON document printed by `gustyc --effects`.
type EffectsDocument struct {
	SchemaVersion string          `json:"schema_version"`
	Version       string          `json:"language_version"`
	GeneratedBy   string          `json:"generated_by"`
	Source        string          `json:"source"`
	Functions     []EffectSummary `json:"functions"`
	// Diagnostics, OK and Exit mirror the --check document (docs/operations.md §
	// Exit codes): the rules that were *decided from* these signatures travel with
	// them, so one call answers both "what does this file do" and "is it honest",
	// and a caller never has to re-derive a verdict from prose on stderr.
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
	OK          bool         `json:"ok"`
	Exit        int          `json:"exit"`
}

// EffectSummaries returns the effect signature of every function in prog — nested
// defs and methods included — preceded by one row for the module. The order is
// declaration order, so two runs on the same source print the same table.
func EffectSummaries(prog *Program) []EffectSummary {
	if prog == nil {
		return nil
	}
	p := newEffectPass(prog)
	p.run()
	out := make([]EffectSummary, 0, len(p.summaries)+1)
	out = append(out, p.moduleSummary)
	out = append(out, p.summaries...)
	return out
}

// EffectsJSON renders prog's effect table as the document `gustyc --effects`
// prints. name is only a label (the file path, or "<src>"); diags are the
// diagnostics the same source produced, normally from CheckSource, so the table
// and the verdict come out of one call.
func EffectsJSON(prog *Program, name string, diags []Diagnostic) (string, error) {
	exit := 0
	for _, d := range diags {
		if d.Level == LevelError {
			exit = 1
			break
		}
	}
	doc := EffectsDocument{
		SchemaVersion: "1.0",
		Version:       Version,
		GeneratedBy:   "gustyc --effects",
		Source:        name,
		Functions:     EffectSummaries(prog),
		Diagnostics:   diags,
		OK:            exit == 0,
		Exit:          exit,
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("effects: marshal: %w", err)
	}
	return string(b), nil
}

// EffectTable renders the same table for humans: one aligned row per function.
func EffectTable(prog *Program, name string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "effect signatures for %s\n", name)
	for _, r := range EffectSummaries(prog) {
		effects := "none"
		if len(r.Effects) > 0 {
			effects = strings.Join(r.Effects, ",")
		}
		calls := ""
		if r.CoroutineCalls > 0 {
			// Its own labelled column, not a pseudo-effect glued onto the effect list:
			// constructing a coroutine is not an effect the body performs, it is a value
			// the body produced that somebody else has to await.
			calls = fmt.Sprintf(" coroutines=%d", r.CoroutineCalls)
		}
		kind := "def"
		if r.Async {
			kind = "async def"
		}
		line := "top"
		if r.Line > 0 {
			line = itoa(r.Line)
		}
		fmt.Fprintf(&sb, "  %-20s %-10s line %-6s effects=%s%s returns=%s falls_through=%s\n",
			r.Function, kind, line, effects, calls, yn(r.ReturnsValue), yn(r.FallsThrough))
	}
	return sb.String()
}

func yn(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// ---------------------------------------------------------------------------
// The pass
// ---------------------------------------------------------------------------

// analyzeEffects is the diagnostic half of the pass, run from Analyze so both
// backends (and `gusty check`, `--verify` and the LSP) get the same verdict.
func analyzeEffects(prog *Program) []Diagnostic {
	if prog == nil {
		return nil
	}
	p := newEffectPass(prog)
	p.run()
	return p.diags
}

// coroBinding is one coroutine value: `a = f(1)` where `f` is an `async def`. The
// pass follows it from the call to its `await`, and reports the provable drops.
type coroBinding struct {
	name  string // the variable holding it
	fname string // the async def that made it
	sp    Span   // where it was created
	// awaited: some path ran `await` on it. Any path counts — the pass reports
	// provable drops, never a guess about code it cannot follow.
	awaited bool
	// escaped: the value flowed somewhere the pass cannot follow (a container, a
	// call argument, a return, an index). Silence is the honest answer there.
	escaped bool
	// reported: already diagnosed, so the end-of-scope sweep does not pile on.
	reported bool
}

// useKind says what a value is about to be done with. The distinction is the
// reason the pass does not shout at legitimate code: awaiting later is legal,
// using a coroutine as a value is not.
type useKind int

const (
	// useValue: an operation needs the real value now (arithmetic, comparison,
	// print, an f-string field, a truth test, a container index). A coroutine
	// here is a bug: after this it can never be awaited.
	useValue useKind = iota
	// useStore: the value is being kept in a name (the binding is created here).
	useStore
	// useHandoff: the value travels somewhere that may await it later — a call
	// argument, a container element, a `return`, a `yield`, an `async for`'s
	// iterable.
	useHandoff
	// useAwait: the value is the operand of `await`, the one place a coroutine is
	// genuinely consumed.
	useAwait
)

// flow is one body's analysis state: the enclosing function, the coroutine
// bindings in scope, and the counters that become the EffectSummary.
type flow struct {
	p       *effectPass
	inAsync bool // inside an `async def` (the module counts: top-level await is legal)
	coro    map[string]*coroBinding
	sum     *EffectSummary
}

// clone forks the flow for a branch. Bindings are shared pointers, so `awaited`
// and `escaped` learned inside the branch propagate out; only the *set* of names
// is per-branch, which is what makes "created in a branch, dropped in a branch"
// reportable without leaking the binding into the enclosing scope.
func (f *flow) clone() *flow {
	nf := *f
	nf.coro = make(map[string]*coroBinding, len(f.coro))
	for k, v := range f.coro {
		nf.coro[k] = v
	}
	return &nf
}

// effectPass indexes the program and collects diagnostics plus summaries.
type effectPass struct {
	src           []Stmt
	asyncFns      map[string]bool // names bound to an `async def`
	syncFns       map[string]bool // names bound to a plain `def`
	summaries     []EffectSummary
	moduleSummary EffectSummary
	diags         []Diagnostic
}

func newEffectPass(prog *Program) *effectPass {
	p := &effectPass{
		src:      prog.Stmts,
		asyncFns: map[string]bool{},
		syncFns:  map[string]bool{},
		moduleSummary: EffectSummary{
			Function:     "<module>",
			Effects:      []string{},
			FallsThrough: true, // a module body ends when the file ends
		},
	}
	indexFuncDefs(prog.Stmts, p.asyncFns, p.syncFns)
	return p
}

// indexFuncDefs records which names produce a coroutine (`async def`) and which
// produce a plain value (`def`), wherever they are declared — module level, a
// class body, an if-arm, a nested function. Declaration order is irrelevant: a
// call below may precede its def.
func indexFuncDefs(stmts []Stmt, asyncFns, syncFns map[string]bool) {
	var walk func([]Stmt)
	walk = func(list []Stmt) {
		for _, st := range list {
			switch s := st.(type) {
			case *FuncDef:
				if s.Async {
					asyncFns[s.Name] = true
					delete(syncFns, s.Name)
				} else {
					syncFns[s.Name] = true
					delete(asyncFns, s.Name)
				}
				walk(s.Body)
			case *ClassDef:
				walk(s.Body)
			case *IfStmt:
				walk(s.Then)
				for _, e := range s.Elifs {
					walk(e.Then)
				}
				walk(s.Else)
			case *WhileStmt:
				walk(s.Body)
				walk(s.Else)
			case *ForStmt:
				walk(s.Body)
				walk(s.Else)
			case *WithStmt:
				walk(s.Body)
			case *TryStmt:
				walk(s.Body)
				for _, e := range s.Excepts {
					walk(e.Body)
				}
				walk(s.Finally)
			case *MatchStmt:
				for _, c := range s.Cases {
					walk(c.Body)
				}
			}
		}
	}
	walk(stmts)
}

// run analyses the module body and every function in the program.
func (p *effectPass) run() {
	mf := &flow{p: p, inAsync: true, coro: map[string]*coroBinding{}, sum: &p.moduleSummary}
	mf.stmts(p.src)
	mf.sweep()

	var walk func([]Stmt, string)
	walk = func(list []Stmt, prefix string) {
		for _, st := range list {
			switch s := st.(type) {
			case *FuncDef:
				name := s.Name
				if prefix != "" {
					name = prefix + "." + s.Name
				}
				p.analyzeFunc(s, name)
				walk(s.Body, name) // a nested def is `outer.inner`
			case *ClassDef:
				walk(s.Body, s.Name)
			case *IfStmt:
				walk(s.Then, prefix)
				for _, e := range s.Elifs {
					walk(e.Then, prefix)
				}
				walk(s.Else, prefix)
			case *WhileStmt:
				walk(s.Body, prefix)
				walk(s.Else, prefix)
			case *ForStmt:
				walk(s.Body, prefix)
				walk(s.Else, prefix)
			case *WithStmt:
				walk(s.Body, prefix)
			case *TryStmt:
				walk(s.Body, prefix)
				for _, e := range s.Excepts {
					walk(e.Body, prefix)
				}
				walk(s.Finally, prefix)
			case *MatchStmt:
				for _, c := range s.Cases {
					walk(c.Body, prefix)
				}
			}
		}
	}
	walk(p.src, "")
}

// analyzeFunc computes one function's signature and rules.
func (p *effectPass) analyzeFunc(fd *FuncDef, name string) {
	sum := EffectSummary{Function: name, Async: fd.Async, Effects: []string{}}
	if fd.Src.Line > 0 {
		sum.Line = fd.Src.Line
	}
	// An `async def` that yields is an async generator. Neither backend lowers
	// one: the record treats the call as a coroutine and then iterates the
	// handle (forever), the compiled backend runs it as a plain generator, and
	// CPython returns an async_generator that a `for` refuses. A refusal beats
	// all three.
	if fd.Async && bodyYields(fd.Body) {
		p.reportAt(fd.Src, LevelError, CodeAsyncGeneratorUnsupported,
			fmt.Sprintf("async def %q yields: async generators are not implemented in either backend", fd.Name),
			"drop `async` (a plain generator already works), or keep `async` and return a list the caller awaits")
	}
	f := &flow{p: p, inAsync: fd.Async, coro: map[string]*coroBinding{}, sum: &sum}
	// Decorators run in the enclosing scope at def time; their values are handed
	// to the decorator, so nothing here is a coroutine use worth reporting twice.
	for _, d := range fd.Decorators {
		f.expr(d, useHandoff)
	}
	term := f.stmts(fd.Body)
	f.sweep()
	sum.Terminates = term
	sum.FallsThrough = !term
	sum.Effects = effectNames(&sum)
	p.summaries = append(p.summaries, sum)

	// The "no missing return" half: an `async def` whose control flow can run off
	// the end awaits to None, while its other paths (or its annotation) promise a
	// value. mypy calls the same thing a missing return statement.
	if fd.Async && !term {
		promiseValued := sum.ReturnsValue
		why := "other paths return a value"
		if fd.ReturnAnno != nil && !fd.ReturnAnno.IsNone() && !fd.ReturnAnno.IsDyn() {
			promiseValued = true
			why = "it is annotated -> " + fd.ReturnAnno.Name()
		}
		if promiseValued {
			p.reportAt(fd.Src, LevelWarning, CodeAsyncMissingReturn,
				fmt.Sprintf("async def %q can finish without a return: awaiting it on that path is None while %s", fd.Name, why),
				"return a value on every path, or annotate `-> None` if None is the point")
		}
	}
}

func effectNames(s *EffectSummary) []string {
	out := []string{}
	if s.Awaits > 0 {
		out = append(out, EffectAwait)
	}
	if s.Yields > 0 {
		out = append(out, EffectYield)
	}
	if s.Raises > 0 {
		out = append(out, EffectRaise)
	}
	sort.Strings(out)
	return out
}

func (p *effectPass) reportAt(sp Span, lvl Level, code, msg, suggestion string) {
	p.diags = append(p.diags, Diagnostic{Level: lvl, Span: sp, Msg: msg, Code: code, Suggestion: suggestion})
}

// reportDrop reports a coroutine that will never run.
func (f *flow) reportDrop(b *coroBinding) {
	if b == nil || b.awaited || b.escaped || b.reported {
		return
	}
	b.reported = true
	f.p.reportAt(b.sp, LevelError, CodeCoroNeverAwaited,
		fmt.Sprintf("coroutine '%s' (async def %s) is never awaited, so its body never runs", b.name, b.fname),
		fmt.Sprintf("await it: `await %s` — or await the call where it is made: `await %s(...)`", b.name, b.fname))
}

// sweep reports every binding still pending at the end of a body, then forgets
// them: past this point nobody can await them.
func (f *flow) sweep() {
	for nm, b := range f.coro {
		f.reportDrop(b)
		delete(f.coro, nm)
	}
}

// block analyses a nested statement list (a branch, a loop body, an except arm)
// and returns whether every path through it terminates.
func (f *flow) block(list []Stmt) bool {
	sub := f.clone()
	term := sub.stmts(list)
	// A binding created inside the block cannot be awaited outside it, so the
	// block is where its drop is reported. Bindings that came from outside stay
	// with the outer scope (their awaited/escaped flags were shared).
	for nm, b := range sub.coro {
		if _, pre := f.coro[nm]; !pre {
			sub.reportDrop(b)
			delete(sub.coro, nm)
		}
	}
	return term
}

// stmts walks a statement list and reports whether control flow can still reach
// the end of it (true = every path left through return/raise/break/continue).
func (f *flow) stmts(list []Stmt) bool {
	for _, st := range list {
		if f.stmt(st) {
			return true
		}
	}
	return false
}

// stmt walks one statement; the result says whether it ends the path.
func (f *flow) stmt(st Stmt) bool {
	switch s := st.(type) {
	case *ReturnStmt:
		if s.Expr != nil {
			f.sum.ReturnsValue = true
			f.expr(s.Expr, useHandoff) // the caller may await what comes back
		} else {
			f.sum.ReturnsBare = true
		}
		return true
	case *RaiseStmt:
		f.sum.Raises++
		if s.Expr != nil {
			f.expr(s.Expr, useHandoff)
		}
		return true
	case *BreakStmt, *ContinueStmt, *PassStmt:
		return true
	case *ExprStmt:
		if s.Expr != nil {
			f.expr(s.Expr, useValue) // a discarded value is the strictest case
		}
	case *AssignStmt:
		f.assign(s)
	case *AugAssignStmt:
		f.expr(s.Target, useValue)
		f.expr(s.Value, useValue)
		f.dropTarget(s.Target)
	case *IfStmt:
		f.expr(s.Cond, useValue)
		hasElse := s.Else != nil
		allTerm := f.block(s.Then)
		for _, e := range s.Elifs {
			f.expr(e.Cond, useValue)
			if !f.block(e.Then) {
				allTerm = false
			}
		}
		if hasElse {
			if !f.block(s.Else) {
				allTerm = false
			}
			return allTerm
		}
		// No else: the whole match can be skipped.
		return false
	case *WhileStmt:
		f.expr(s.Cond, useValue)
		f.block(s.Body)
		if s.Else != nil {
			f.block(s.Else)
			return false
		}
		// `while True:` has exactly one way out that is not a return or a raise:
		// a break to its own level. Body fall-through is the loop doing its job,
		// so a countdown loop that returns from inside the test never reaches the
		// statement after the loop and must not be called non-terminating.
		if isConstTrue(s.Cond) {
			return !blockBreaks(s.Body)
		}
		return false
	case *ForStmt:
		if s.Async && !f.inAsync {
			f.asyncStmtOutside(s.Span(), "async for")
		}
		// The iterable is handed to the loop: an `async for` over a list of
		// coroutines awaits each element as it comes, so a coroutine here is
		// exactly what the language asks for.
		f.expr(s.Iter, useHandoff)
		f.block(s.Body)
		if s.Else != nil {
			f.block(s.Else)
		}
		f.dropTarget(s.Var) // the loop variable is rebound every iteration
		return false        // zero iterations is always possible
	case *WithStmt:
		if s.Async && !f.inAsync {
			f.asyncStmtOutside(s.Span(), "async with")
		}
		f.expr(s.Expr, useHandoff) // the manager goes to __enter__/__exit__
		term := f.block(s.Body)
		if s.As != nil {
			f.drop(s.As.Value)
		}
		return term
	case *TryStmt:
		bodyTerm := f.block(s.Body)
		allTerm := bodyTerm && len(s.Excepts) > 0
		for _, e := range s.Excepts {
			if !f.block(e.Body) {
				allTerm = false
			}
		}
		if s.Finally != nil {
			if f.block(s.Finally) {
				return true
			}
		}
		return allTerm
	case *MatchStmt:
		f.expr(s.Subject, useValue)
		allTerm, catchAll := true, false
		for _, c := range s.Cases {
			if c.Guard != nil {
				f.expr(c.Guard, useValue)
			}
			if c.Guard == nil && patternAlwaysMatches(c.Pattern) {
				catchAll = true
			}
			if !f.block(c.Body) {
				allTerm = false
			}
		}
		return catchAll && allTerm
	case *YieldStmt:
		f.sum.Yields++
		if s.Expr != nil {
			f.expr(s.Expr, useHandoff)
		}
	case *YieldFromStmt:
		f.sum.Yields++
		f.expr(s.Expr, useHandoff)
	case *FuncDef, *ClassDef, *ImportStmt, *ExternDecl, *TypeAliasStmt:
		// Declarations: a nested def gets its own row (see effectPass.run), a
		// class body is analysed with its own signature.
	}
	return false
}

// asyncStmtOutside reports `async for` / `async with` outside a coroutine. The
// module top level is exempt — the language's documented top-level coroutine —
// but inside a plain `def` the construct cannot do anything: the body below it
// never suspends, so the loop/manager is a lie.
func (f *flow) asyncStmtOutside(sp Span, what string) {
	f.p.reportAt(sp, LevelWarning, CodeAsyncStmtOutsideCoroutine,
		what+" outside an `async def` cannot suspend: it behaves as the plain form, where CPython rejects the program",
		"declare the enclosing function `async def`, or drop the `async`")
}

// assign tracks what an assignment does to a coroutine binding.
func (f *flow) assign(s *AssignStmt) {
	if t, ok := s.Target.(*Name); ok {
		f.expr(s.Value, useStore)
		if c := f.asyncCall(s.Value); c != nil {
			if old, ok := f.coro[t.Value]; ok {
				f.reportDrop(old)
			}
			f.coro[t.Value] = &coroBinding{name: t.Value, fname: directCalleeName(c), sp: c.Span()}
			return
		}
		f.drop(t.Value)
		return
	}
	// A subscript or attribute target: the container is read, the value is stored
	// somewhere the pass cannot follow.
	f.expr(s.Target, useValue)
	f.expr(s.Value, useHandoff)
}

// drop forgets a binding, reporting it if it was never awaited: rebinding the
// name is the last chance the coroutine ever had.
func (f *flow) drop(nm string) {
	if b, ok := f.coro[nm]; ok {
		f.reportDrop(b)
		delete(f.coro, nm)
	}
}

// dropTarget drops the binding carried by an assignment-like target.
func (f *flow) dropTarget(e Expr) {
	if n, ok := e.(*Name); ok {
		f.drop(n.Value)
	}
}

// expr walks an expression, honouring what its value is about to be used for.
func (f *flow) expr(e Expr, use useKind) {
	if e == nil {
		return
	}
	switch n := e.(type) {
	case nil:
		return
	case *AwaitExpr:
		f.sum.Awaits++
		if !f.inAsync {
			f.p.reportAt(n.Span(), LevelWarning, CodeAwaitOutsideCoroutine,
				"await inside a plain def cannot suspend anything: the value is simply evaluated here, where CPython rejects the program",
				"move the await into an `async def`, or drop it — awaiting a plain value is a no-op")
		}
		f.awaitOperand(n.Expr, n.Span())
		f.expr(n.Expr, useAwait)
	case *Call:
		f.call(n, use)
	case *Name:
		if n.Value == "_" || n.Value == "True" || n.Value == "False" || n.Value == "None" {
			return
		}
		b, tracked := f.coro[n.Value]
		if !tracked {
			return
		}
		switch use {
		case useValue:
			f.reportDrop(b) // used as a value and never awaited: the body never ran
		case useHandoff, useStore, useAwait:
			b.escaped = use != useAwait
		}
	case *BinOp:
		f.expr(n.L, useValue)
		f.expr(n.R, useValue)
	case *UnOp:
		f.expr(n.X, useValue)
	case *CondExpr:
		f.expr(n.Cond, useValue)
		f.expr(n.If, use)
		f.expr(n.Else, use)
	case *AssignExpr:
		f.expr(n.Value, useStore)
		if c := f.asyncCall(n.Value); c != nil {
			f.coro[n.Name.Value] = &coroBinding{name: n.Name.Value, fname: directCalleeName(c), sp: c.Span()}
		} else {
			f.drop(n.Name.Value)
		}
	case *Index:
		f.expr(n.Obj, useValue)
		f.expr(n.Idx, useValue)
	case *Slice:
		f.expr(n.Obj, useValue)
		f.expr(n.Low, useValue)
		f.expr(n.High, useValue)
		f.expr(n.Step, useValue)
	case *Attr:
		f.expr(n.Obj, useValue)
	case *FString:
		for _, part := range n.Parts {
			f.expr(part.Expr, useValue)
		}
	case *ListLit:
		for _, el := range n.Elems {
			f.expr(el, useHandoff)
		}
	case *SetLit:
		for _, el := range n.Elems {
			f.expr(el, useHandoff)
		}
	case *Tuple:
		for _, el := range n.Elems {
			f.expr(el, useHandoff)
		}
	case *DictLit:
		for _, k := range n.Keys {
			f.expr(k, useHandoff)
		}
		for _, v := range n.Vals {
			f.expr(v, useHandoff)
		}
	case *Comp:
		f.expr(n.Iter, useHandoff)
		for _, el := range n.Elems {
			f.expr(el, useHandoff)
		}
		for _, k := range n.Keys {
			f.expr(k, useHandoff)
		}
		for _, v := range n.Vals {
			f.expr(v, useHandoff)
		}
		f.expr(n.Cond, useValue)
	case *Generator:
		f.expr(n.Iter, useHandoff)
		for _, el := range n.Elems {
			f.expr(el, useHandoff)
		}
		f.expr(n.Cond, useValue)
	case *Lambda:
		// A lambda is never a coroutine: an `await` in its body is as out of
		// place as in a plain def. Analyse it as its own scope.
		sub := &flow{p: f.p, inAsync: false, coro: map[string]*coroBinding{}, sum: f.sum}
		sub.expr(n.Body, useHandoff)
		return
	}
}

// call walks a call: the callee, then the arguments with the use the callee
// deserves. A builtin cannot await anything, so its arguments are values; a user
// function might, so passing a coroutine along is legitimate.
func (f *flow) call(c *Call, use useKind) {
	if c == nil {
		return
	}
	cn, isName := directCallee(c)
	if c.Fn != nil {
		switch fn := c.Fn.(type) {
		case *Name:
			// nothing to walk: a bare name is the callee itself
		case *Attr:
			f.expr(fn.Obj, useValue)
		case *Lambda:
			sub := &flow{p: f.p, inAsync: false, coro: map[string]*coroBinding{}, sum: f.sum}
			sub.expr(fn.Body, useHandoff)
		default:
			f.expr(c.Fn, useValue)
		}
	}
	argUse := useHandoff
	if isName && isPredeclaredName(cn) {
		argUse = useValue
	}
	if isName && f.p.asyncFns[cn] {
		f.sum.CoroutineCalls++
		if use == useValue {
			// `print(f(1))`, `f(1) + 1`, `if f(1):` — the coroutine is consumed
			// as a value and can never be awaited afterwards.
			f.reportDrop(&coroBinding{name: cn + "(...)", fname: cn, sp: c.Span()})
		}
	}
	for _, a := range c.Args {
		if ka, ok := a.(*KeywordArg); ok {
			f.expr(ka.Value, argUse)
			continue
		}
		f.expr(a, argUse)
	}
}

// awaitOperand records the consumption of a coroutine, and calls out the cases
// where `await` is being pointed at something that cannot be a coroutine.
func (f *flow) awaitOperand(e Expr, sp Span) {
	switch n := e.(type) {
	case nil:
		return
	case *Name:
		if b, ok := f.coro[n.Value]; ok {
			if b.awaited {
				if !b.reported {
					b.reported = true
					f.p.reportAt(b.sp, LevelError, CodeCoroAwaitedTwice,
						fmt.Sprintf("coroutine '%s' (async def %s) is awaited twice: its body already ran", b.name, b.fname),
						fmt.Sprintf("make a fresh coroutine before awaiting it again: `%s = %s(...)`", b.name, b.fname))
				}
				return
			}
			b.awaited = true
		}
	case *Call:
		if cn, ok := directCallee(n); ok && f.p.asyncFns[cn] {
			return // the normal case: `await f(1)`
		}
	}
	if kind, known := f.knownNonCoroutine(e); known {
		f.p.reportAt(sp, LevelWarning, CodeAwaitNotCoroutine,
			fmt.Sprintf("await is pointed at %s, which is not a coroutine: it passes straight through here, where CPython raises TypeError", kind),
			"drop the await, or await a call to an `async def`")
	}
}

// knownNonCoroutine answers only when it is sure: a value that cannot be a
// coroutine in any program. Guessing here would invent type errors for dynamic
// code, which the gradual typing contract forbids.
func (f *flow) knownNonCoroutine(e Expr) (string, bool) {
	switch n := e.(type) {
	case *IntLit:
		return "an integer literal", true
	case *FloatLit:
		return "a float literal", true
	case *StrLit:
		return "a string literal", true
	case *FString:
		return "an f-string", true
	case *BoolLit:
		return "a boolean literal", true
	case *NoneLit:
		return "None", true
	case *ListLit:
		return "a list literal", true
	case *SetLit:
		return "a set literal", true
	case *DictLit:
		return "a dict literal", true
	case *Tuple:
		return "a tuple", true
	case *BinOp:
		return "the result of an arithmetic expression", true
	case *Comp:
		if n.Kind == CompList {
			return "a list comprehension", true
		}
		return "a comprehension", true
	case *Call:
		cn, ok := directCallee(n)
		if !ok {
			return "", false
		}
		if isPredeclaredName(cn) {
			return fmt.Sprintf("the result of the built-in %s(...)", cn), true
		}
		if f.p.syncFns[cn] && !f.p.asyncFns[cn] {
			return fmt.Sprintf("the result of the plain def %s(...)", cn), true
		}
	}
	return "", false
}

// asyncCall returns the call expression when it produces a coroutine (a direct
// call to a name bound to an `async def`), nil otherwise.
func (f *flow) asyncCall(e Expr) *Call {
	c, ok := e.(*Call)
	if !ok {
		return nil
	}
	cn, ok := directCallee(c)
	if !ok || !f.p.asyncFns[cn] {
		return nil
	}
	return c
}

// callee is the name of a direct call's callee.
func directCallee(c *Call) (string, bool) {
	if n, ok := c.Fn.(*Name); ok {
		return n.Value, true
	}
	return "", false
}

// directCalleeName is directCallee for a call already known to be direct.
func directCalleeName(c *Call) string {
	if nm, ok := directCallee(c); ok {
		return nm
	}
	return "?"
}

// patternAlwaysMatches is the L6.1 rule: a bare-name pattern (including `_`)
// with no guard always matches, so a match containing one is exhaustive.
func patternAlwaysMatches(p Expr) bool {
	if p == nil {
		return false
	}
	if _, ok := p.(*Name); ok {
		return true
	}
	if t, ok := p.(*Tuple); ok {
		for _, el := range t.Elems {
			if n, ok := el.(*Name); ok && n.Value == "_" {
				continue
			}
			return false
		}
		return len(t.Elems) > 0
	}
	return false
}

// bodyYields reports whether a body yields, at its own level: a nested generator
// is a different function with a different signature.
func bodyYields(stmts []Stmt) bool {
	for _, st := range stmts {
		switch s := st.(type) {
		case *YieldStmt:
			return true
		case *YieldFromStmt:
			return true
		case *IfStmt:
			if bodyYields(s.Then) {
				return true
			}
			for _, e := range s.Elifs {
				if bodyYields(e.Then) {
					return true
				}
			}
			if bodyYields(s.Else) {
				return true
			}
		case *WhileStmt:
			if bodyYields(s.Body) || bodyYields(s.Else) {
				return true
			}
		case *ForStmt:
			if bodyYields(s.Body) || bodyYields(s.Else) {
				return true
			}
		case *WithStmt:
			if bodyYields(s.Body) {
				return true
			}
		case *TryStmt:
			if bodyYields(s.Body) {
				return true
			}
			for _, e := range s.Excepts {
				if bodyYields(e.Body) {
					return true
				}
			}
			if bodyYields(s.Finally) {
				return true
			}
		case *MatchStmt:
			for _, c := range s.Cases {
				if bodyYields(c.Body) {
					return true
				}
			}
		}
	}
	return false
}

// blockBreaks reports whether a body can leave its enclosing loop with `break`,
// which is the only thing that stops `while True:` from counting as terminating.
// `continue` stays inside the loop, so it is not an exit.
func blockBreaks(stmts []Stmt) bool {
	for _, st := range stmts {
		switch s := st.(type) {
		case *BreakStmt:
			return true
		case *IfStmt:
			if blockBreaks(s.Then) {
				return true
			}
			for _, e := range s.Elifs {
				if blockBreaks(e.Then) {
					return true
				}
			}
			if blockBreaks(s.Else) {
				return true
			}
		case *TryStmt:
			if blockBreaks(s.Body) {
				return true
			}
			for _, e := range s.Excepts {
				if blockBreaks(e.Body) {
					return true
				}
			}
			if blockBreaks(s.Finally) {
				return true
			}
		case *MatchStmt:
			for _, c := range s.Cases {
				if blockBreaks(c.Body) {
					return true
				}
			}
		case *WithStmt:
			if blockBreaks(s.Body) {
				return true
			}
			// Nested while/for own their own breaks.
		}
	}
	return false
}

// isConstTrue is the `while True:` test.
func isConstTrue(e Expr) bool {
	if b, ok := e.(*BoolLit); ok {
		return b.Value
	}
	return false
}
