package lang

// Bool values (roadmap L11.1 step 2, ADR 0257).
//
// A bool has always been a value to the *language*: `if x == 1:`, `not x` and
// `while flag` all read one. What it has never been is a value to the *printer*.
// Both backends store a bool as the number the comparison produced — the AST
// interpreter as an `int64` 1/0, the compiled backend as an `i32` 0/1 — and neither
// representation carries a kind, so `print(True)` asked "what number is this?" and
// answered `1` where CPython answers `True`:
//
//	print(True)             # gusty: 1     CPython: True
//	print(1 == 1)           # gusty: 1     CPython: True
//	print(False)            # gusty: 0     CPython: False
//	print(0 == None)        # gusty: 0     CPython: False
//	print("yes" == "yes")   # gusty: 1     CPython: True
//
// That is integration/programs/probe_bool_value.gy, and every one of its five lines is
// a bool. The fix cannot be a new runtime representation: an untagged word is what a
// value word is, and giving bools their own tagged word is the L11.1 destination rather
// than this rung (ADR 0171's rule is that the compiler's front end reads the program,
// and the tag exists so the runtime does not have to guess). What a bool needs in order
// to be a printed value is the answer to "is this expression a bool?" — a question both
// backends already hold in the AST and neither asks. So this file asks it once, in one
// place, and both backends print from the answer, exactly as ADR 0172 prints None and
// ADR 0224 prints interned text: the front end reads the program, the runtime renders.
//
// The question is static and deliberately conservative. An expression is a bool when its
// shape says so — the shape the language already treats as a verdict — and a name is a
// bool while the value last stored into it said so. Anything the predicate is not sure
// about keeps printing as the number it always printed: an answer it is unsure of must
// degrade to yesterday's behaviour, never to a new wrong one.

// boolCompareOps are the operators whose result is a verdict and not a number: the
// comparisons, the membership tests and the identity tests. They are the operators the
// language already treats as questions — `if 1 == 1:` — which is the evidence that the
// answer was always a bool and only the printing forgot.
var boolCompareOps = map[string]bool{
	"==":     true,
	"!=":     true,
	"<":      true,
	"<=":     true,
	">":      true,
	">=":     true,
	"in":     true,
	"not in": true,
	"is":     true,
	"is not": true,
}

// boolReturningBuiltins are the builtins whose answer is a verdict rather than a value.
// They are listed rather than inferred because their bodies are Go, not an AST — and the
// list is only what both backends actually have: `all`/`any` answer whether every/any
// element is truthy, which is a verdict in Python and so renders True/False, while
// `isinstance` and `bool` are not implemented on either path and belong to the rows that
// owe them rather than to a table that would promise an answer they cannot give.
var boolReturningBuiltins = map[string]bool{
	"all": true,
	"any": true,
}

// boolCallDepth caps how far the predicate follows a call into its callee's body. A
// recursive function must not send it round a cycle; three levels is more than any
// program in the corpus needs, and keeps the question cheap enough to ask per print.
const boolCallDepth = 3

// BoolEnv is what the question 「is this expression a bool?」 needs beyond the AST:
// which names currently hold bools, how to resolve a called name to its definition, and
// whether a builtin's verdict has been shadowed by a user def of the same name. Each
// backend already keeps all three, and keeps them per scope — which is the point: a
// function's own `flag` must not make the module's `flag` print True.
type BoolEnv struct {
	// Vars records the names whose latest assignment produced a bool. Nil is fine: the
	// shapes that need no environment — a literal, a comparison, a `not` — still answer,
	// and those are every line of the probe.
	Vars map[string]bool
	// Lookup resolves a called name to its FuncDef, or nil. A function's answer is
	// determined by what its body does (ADR 0254's rule, read the other way round).
	Lookup func(string) *FuncDef
	// Shadowed reports whether a builtin name has been redefined by the program, in which
	// case that name's verdict comes from the program's body and not from the table above.
	Shadowed func(string) bool
	// Instance reports whether an operand is an instance of a class the program defined.
	// Such an operand makes a comparison a *call* — `a < 4` runs `a.__lt__(4)` — and the
	// answer is whatever that method returns, which is an int when the method returns 1.
	// Python prints 1 for dunder.gy's `print(a < 4)`; calling the operator a verdict here
	// would print True and disagree with the oracle about an overloaded operator.
	Instance func(Expr) bool
	// NumericCandidate is the compiled backend's own numeric-fold question: what value a
	// candidate has and whether it arrived as a double, for a candidate that is not a bare
	// literal (a constant-folded name, an element the literal wrote). The min/max verdict
	// question asks it so that the candidate the *module* selects and the candidate the
	// renderer names are read from one rule; nil means “no fold to agree with“, and the
	// question then answers from literals alone — which is what the interpreter needs,
	// because its winner is chosen by real comparison at run time (roadmap Gap R.117).
	NumericCandidate func(Expr) (val float64, isFloat bool, ok bool)
}

// IsBoolExpr reports whether an expression's value is a bool: a bool literal, a
// comparison / membership / identity test, a `not`, an `and`/`or` whose every possible
// result is a bool, a ternary whose branches both are, a name last assigned one of
// those, or a call to a function that can only return one.
//
// It is exported because both backends and the CLI's --json report ask the same question
// and must never disagree: a REPL echoing `True` beside a compiled binary echoing `1` is
// exactly the two-engine split this language has been closing all the way down L11.1.
func IsBoolExpr(e Expr, env BoolEnv) bool {
	return env.of(e, 0)
}

func (env BoolEnv) of(e Expr, depth int) bool {
	if e == nil {
		return false
	}
	switch t := e.(type) {
	case *BoolLit:
		return true
	case *UnOp:
		// `not x` is a verdict about x; `-x` is a number about x.
		return t.Op == "not"
	case *BinOp:
		if boolCompareOps[t.Op] {
			// An overloaded comparison is a call in disguise, and a call returns what its
			// body returns (roadmap L6.6's dunder rule read through ADR 0254's lens).
			return !env.overloadedComparison(t)
		}
		if t.Op == "and" || t.Op == "or" {
			// Python's and/or yield an operand, not a verdict: `1 and 2` is 2, `True or 1` is True and
			// `1 or True` is 1. So the answer's kind is a fact about the operand the test chooses, which
			// is the same question a ternary's arms answer and min/max ask of their winner (ADR 0261).
			// When the test is a value the source wrote, that operand is known and only its own boolness
			// matters; when it is not, only an answer both operands agree on is honest (roadmap Gap R.147,
			// ADR 0269).
			if chosen, ok := constantLogicArm(t); ok {
				return env.of(chosen, depth)
			}
			return env.of(t.L, depth) && env.of(t.R, depth)
		}
		return false
	case *Name:
		return env.Vars[t.Value]
	case *CondExpr:
		// `a if c else b` is whichever branch the test picks. When the test is a constant the
		// compiler can read, that is the arm the program will run, and it is a verdict exactly when
		// that arm is one — the same rule min/max follow, where the candidate the comparison chose
		// decides what the answer is (roadmap Gap R.117, ADR 0261). When the test is not a constant,
		// neither arm is known to run, and only an answer both arms agree on is honest: both must be
		// verdicts, which is the rule ADR 0257 shipped and the ternary's own type question answers.
		if taken, ok := constantTestArm(t); ok {
			return env.of(taken, depth)
		}
		return env.of(t.If, depth) && env.of(t.Else, depth)
	case *AssignExpr:
		return env.of(t.Value, depth)
	case *Call:
		return env.callReturnsBool(t, depth)
	case *ChainCompare:
		// A comparison chain answers the conjunction of its links, and every link is a comparison, so
		// the chain's own answer is a verdict — printed True/False, not the 1/0 the slot holds
		// (ADR 0257's rule, roadmap L12.1 / Gap R.53, ADR 0288). Without this the print road falls
		// through to printf("%d") and `print(1 < 2 < 3)` answers `1`, which is the same wrong answer
		// the chain itself fixes, arriving from the other end.
		return true
	}
	return false
}

// callReturnsBool asks whether a call hands back a bool. A builtin answers from the
// table; a user function answers from its own body. Every returned expression must be a
// bool: a function with one `return True` and one `return 0` does not return a bool on
// every path, and printing its answer as True whatever the branch took would be a guess
// about which path ran, which is the thing the tag regime exists to stop.
// overloadedComparison asks whether either operand turns this comparison into a dunder
// call. Unknown operands are not instances: the question is asked of a front end that
// knows the classes a program declares, and a name it cannot see through is a number or a
// container rather than an object with an __lt__ of its own.
func (env BoolEnv) overloadedComparison(t *BinOp) bool {
	if env.Instance == nil {
		return false
	}
	return env.Instance(t.L) || env.Instance(t.R)
}

// constantTestArm reports the arm a ternary will run when its test is a value the source already
// wrote — `True`, `0`, `None`, `""`, the empty container. Truthiness here is Python's, and it is the
// same list the language’s own truth test uses: a verdict, a nonzero number, any non-empty value.
// ok=false means the test is something the compiler has to evaluate, and the caller then needs both
// arms to agree (roadmap Gap R.125 keeps that shape as the open half).
func constantTestArm(t *CondExpr) (Expr, bool) {
	truthy, ok := constantTruth(t.Cond)
	if !ok {
		return nil, false
	}
	if truthy {
		return t.If, true
	}
	return t.Else, true
}

// constantLogicArm reports the operand an `and`/`or` will hand back when the left operand's truth is a
// value the source already wrote. `print(True or 1)` prints True because the operand the test chooses is
// the BoolLit, and `print(1 or True)` prints 1 because the same test chooses the other one — the pair is
// the reason this is asked of the chosen operand and never of the operator (roadmap Gap R.147, ADR 0269,
// and the same rule ADR 0261 shipped for a ternary's arms).
func constantLogicArm(n *BinOp) (Expr, bool) {
	if n == nil || (n.Op != "and" && n.Op != "or") {
		return nil, false
	}
	truthy, ok := constantTruth(n.L)
	if !ok {
		return nil, false
	}
	takesRight := truthy
	if n.Op == "or" {
		takesRight = !truthy
	}
	if takesRight {
		return n.R, true
	}
	return n.L, true
}

func constantTruth(e Expr) (bool, bool) {
	switch t := e.(type) {
	case *BoolLit:
		return t.Value, true
	case *IntLit:
		return t.Value != 0, true
	case *FloatLit:
		return t.Value != 0, true
	case *NoneLit:
		return false, true
	case *StrLit:
		return t.Value != "", true
	case *ListLit:
		return len(t.Elems) > 0, true
	case *SetLit:
		return len(t.Elems) > 0, true
	case *DictLit:
		return len(t.Keys) > 0, true
	case *UnOp:
		if t.Op == "not" {
			if inner, ok := constantTruth(t.X); ok {
				return !inner, true
			}
		}
	}
	return false, false
}

// minMaxBuiltin names the two builtins that *choose* a candidate rather than compute a value.
func minMaxBuiltin(name string) (wantMin bool, ok bool) {
	switch name {
	case "min":
		return true, true
	case "max":
		return false, true
	}
	return false, false
}

// minMaxCandidates returns the candidates a min/max call compares: the elements of one inline
// list/set argument, or the varargs. A single argument the program did not write out — a list
// variable, a call — has no candidate the source can name, and the question declines: the winner
// of a comparison over slots the compiler cannot see is the tagged-value-word rows (Gaps
// R.107–R.110), not a guess to print from.
func minMaxCandidates(c *Call) ([]Expr, bool) {
	if len(c.Args) == 1 {
		switch t := c.Args[0].(type) {
		case *ListLit:
			return t.Elems, len(t.Elems) > 0
		case *SetLit:
			return t.Elems, len(t.Elems) > 0
		}
	}
	if len(c.Args) == 0 {
		return nil, false
	}
	// The varargs spelling, and the scalar one (`max(True)` is the one-element collection the
	// interpreter treats it as): the argument is the candidate.
	return c.Args, true
}

// minMaxCandidateValue is the number a min/max comparison makes of a candidate, and whether that
// number arrived as a double. A verdict is its number — 1 or 0 — because that is what every
// numeric path reads from one (ADR 0259's int/bool/float family), and a candidate the source does
// not write as a number is no candidate for these questions: the caller declines rather than
// inventing which way the comparison went.
func minMaxCandidateValue(e Expr, fold func(Expr) (float64, bool, bool)) (float64, bool, bool) {
	switch t := e.(type) {
	case *BoolLit:
		if t.Value {
			return 1, false, true
		}
		return 0, false, true
	case *IntLit:
		return float64(t.Value), false, true
	case *FloatLit:
		return t.Value, true, true
	case *UnOp:
		if t.Op != "-" {
			return 0, false, false
		}
		v, isF, ok := minMaxCandidateValue(t.X, fold)
		return -v, isF, ok
	}
	if fold != nil {
		if v, isF, ok := fold(e); ok {
			return v, isF, true
		}
	}
	return 0, false, false
}

// numericWinner is the one comparison min/max make. It is strict, so a tie keeps the candidate
// that came first — CPython returns the first maximal element, which is why max([True, 1]) is
// True while max([1, True]) is 1 and why the two spellings cannot share a “the elements are
// bools“ answer (roadmap Gap R.117).
func numericWinner(vals []float64, wantMin bool) int {
	best := 0
	for i := 1; i < len(vals); i++ {
		if (wantMin && vals[i] < vals[best]) || (!wantMin && vals[i] > vals[best]) {
			best = i
		}
	}
	return best
}

// minMaxChosenIsVerdict answers the operator that chooses: min/max hand back one of their
// candidates, so the call is a verdict exactly when the candidate the comparison chose is one
// (ADR 0256's rule that a fold returns the candidate it chose, read as a rendering rule).
func (env BoolEnv) minMaxChosenIsVerdict(c *Call, wantMin bool, depth int) bool {
	cands, ok := minMaxCandidates(c)
	if !ok {
		return false
	}
	vals := make([]float64, len(cands))
	for i, cand := range cands {
		v, _, foldable := minMaxCandidateValue(cand, env.NumericCandidate)
		if !foldable {
			return false // the winner is not statically known: the number is what is known
		}
		vals[i] = v
	}
	return env.of(cands[numericWinner(vals, wantMin)], depth)
}

func (env BoolEnv) callReturnsBool(c *Call, depth int) bool {
	nm, ok := c.Fn.(*Name)
	if !ok {
		return false
	}
	// min/max choose a candidate, and the chosen candidate decides what the answer is: not a
	// builtin that answers yes-or-no, so the question is asked of the winner rather than of the
	// name (roadmap Gap R.117).
	if wantMin, isMinMax := minMaxBuiltin(nm.Value); isMinMax && (env.Shadowed == nil || !env.Shadowed(nm.Value)) {
		return env.minMaxChosenIsVerdict(c, wantMin, depth)
	}
	if boolReturningBuiltins[nm.Value] && (env.Shadowed == nil || !env.Shadowed(nm.Value)) {
		return true
	}
	// bool(x) is the constructor that ANSWERS a verdict — `print(bool(1))` is `True`, not `1` —
	// so the print road must pick rt_print_bool for it, exactly as it does for a literal verdict.
	// It is not in the table above because it is the only builtin whose answer is a verdict of its
	// ARGUMENT's truthiness rather than a yes-or-no question of its own, and it takes no argument
	// at all in the `bool()` spelling (roadmap Gap R.131, ADR 0287).
	if nm.Value == "bool" && len(c.Args) <= 1 && (env.Shadowed == nil || !env.Shadowed(nm.Value)) {
		return true
	}
	if env.Lookup == nil || depth >= boolCallDepth {
		return false
	}
	fd := env.Lookup(nm.Value)
	if fd == nil || len(fd.Body) == 0 {
		return false
	}
	if fd.ReturnAnno.IsBool() {
		return true
	}
	sawReturn := false
	return bodyReturnsOnlyBools(fd.Body, env, depth+1, &sawReturn) && sawReturn
}

// bodyReturnsOnlyBools walks a function body's return statements. A body that returns
// nothing hands back None and is not bool-valued; a body whose every returned expression
// is a bool is. The walk descends the blocks where a verdict is usually decided
// (if/elif/else, while, for, try/except/finally, with, match) and stops at a nested def:
// that is its own function, and it answers for itself at its own call sites.
func bodyReturnsOnlyBools(stmts []Stmt, env BoolEnv, depth int, sawReturn *bool) bool {
	for _, s := range stmts {
		switch t := s.(type) {
		case *ReturnStmt:
			if t.Expr == nil {
				return false // a bare `return` hands back None
			}
			*sawReturn = true
			if !env.of(t.Expr, depth) {
				return false
			}
		case *IfStmt:
			if !bodyReturnsOnlyBools(t.Then, env, depth, sawReturn) {
				return false
			}
			for _, e := range t.Elifs {
				if e != nil && !bodyReturnsOnlyBools([]Stmt{e}, env, depth, sawReturn) {
					return false
				}
			}
			if !bodyReturnsOnlyBools(t.Else, env, depth, sawReturn) {
				return false
			}
		case *WhileStmt:
			if !bodyReturnsOnlyBools(t.Body, env, depth, sawReturn) {
				return false
			}
			if !bodyReturnsOnlyBools(t.Else, env, depth, sawReturn) {
				return false
			}
		case *ForStmt:
			if !bodyReturnsOnlyBools(t.Body, env, depth, sawReturn) {
				return false
			}
			if !bodyReturnsOnlyBools(t.Else, env, depth, sawReturn) {
				return false
			}
		case *TryStmt:
			if !bodyReturnsOnlyBools(t.Body, env, depth, sawReturn) {
				return false
			}
			for _, cl := range t.Excepts {
				if cl != nil && !bodyReturnsOnlyBools(cl.Body, env, depth, sawReturn) {
					return false
				}
			}
			if !bodyReturnsOnlyBools(t.Finally, env, depth, sawReturn) {
				return false
			}
		case *WithStmt:
			if !bodyReturnsOnlyBools(t.Body, env, depth, sawReturn) {
				return false
			}
		case *MatchStmt:
			for _, mc := range t.Cases {
				if mc != nil && !bodyReturnsOnlyBools(mc.Body, env, depth, sawReturn) {
					return false
				}
			}
		}
		// Any other statement — an assignment, an expression, a nested def — holds no
		// verdict of this body to misprint: a nested FuncDef/ClassDef is its own function,
		// and its returns belong to it.
	}
	return true
}

// BoolText renders a bool the way its name is spelled in the source: Python prints a
// bool as its own literal, so the rendering of a true bool is the token `True`.
func BoolText(v bool) string {
	if v {
		return "True"
	}
	return "False"
}
