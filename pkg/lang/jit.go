package lang

import (
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

// Evaluator is a small AST interpreter used by --eval and the REPL.
// It evaluates integer-typed expressions deterministically without needing
// an LLVM JIT engine (the AOT backend emits textual IR verified by `llc`).
type Evaluator struct {
	Vars    map[string]int64
	funcs   map[string]*FuncDef
	externs map[string]*ExternDecl
	inCall  bool // true while evaluating a function body (nested defs become closures)
	heap    map[int64]*obj
	// noneVal is the None singleton (value tag TagNone). It is a heap object rather
	// than an immediate because ints are raw int64s: encoding None as a reserved int
	// would make `x = -2147483648` print "None". Allocated once, never freed.
	noneVal     int64
	nextID      int64
	nurseryBase int64
	allocCount  int64
	classIDs    map[string]int64
	// boolVars records the names whose latest assignment produced a bool, so print and str
	// render True/False instead of the 1/0 a bool is stored as. It swaps with the scope: a
	// function's own `flag` is its business and must not make the module's `flag` print True
	// (roadmap L11.1 step 2, ADR 0257).
	boolVars map[string]bool
	curRet   *Type  // return annotation of the function currently executing
	fnName   string // name of the function whose body is being evaluated
	// curFD is the *FuncDef whose body is executing, and curBodies caches which names each body
	// binds anywhere inside itself. A name the body binds is local to that body whatever the module
	// holds, so reading it before any path assigned it is UnboundLocalError rather than a lookup
	// that quietly finds the module's value (roadmap Gap R.36 + R.39, ADR 0228).
	curFD     *FuncDef
	curBodies map[*FuncDef]map[string]bool
	cur       Span   // source span of the statement currently being evaluated
	yieldList int64  // list handle accumulating yields (0 = not in generator)
	curClass  string // class name of the method currently executing (for super())
	curSelf   int64  // receiver of the method currently executing (for super())
	// classList records the declared base chain (L6.6) so a nominal class
	// annotation (`a: Animal`) can also be enforced for subclass instances.
	classList *ClassIndex

	// --- precise stack roots (L7.2, ADR 0181) — see gc.go ---
	// frames are the local scopes of the active calls; a collection traces them
	// so a value that only a live frame references is never swept, and stops
	// tracing them when the frame returns so a dead frame keeps nothing alive.
	frames []map[string]int64
	// A name a function reads is looked for in its own frame, then in the closure
	// environment captured where it was written, then — Gap R.35, ADR 0220 — in the
	// *module* the function was defined in. Python has one flat scope per def and one
	// per module; without the last link a script could not read a module-level constant
	// from a function at all, which is the most ordinary program there is.
	// moduleVars is the program's top-level scope; curModule is the global scope of the
	// call in flight (a function defined in an imported module resolves there).
	moduleVars  map[string]int64
	curModule   map[string]int64
	funcModules map[*FuncDef]map[string]int64
	// globalScopes holds every module scope whose bindings live for the whole run. They
	// are not on the frame stack, so the collector needs them named explicitly —
	// otherwise a collection sweeps objects that only a module global can still reach.
	globalScopes  []map[string]int64
	globalScopeAt map[uintptr]bool
	// rootGroups are handles a statement holds in Go locals while it runs a
	// nested body (a for-loop's iterable, a with-manager, a match subject, the
	// previous statement's value). Each construct declares them explicitly.
	rootGroups [][]int64
	// exprDepth counts in-flight expression evaluations. The collector advances
	// its soundness watermark only when it is zero: then no interpreter frame is
	// mid-expression, so no live value is sitting in a register.
	exprDepth int
	// exprBase is the expression depth a statement executor measures safe points
	// against: equal to exprDepth inside a statement-root call, 0 otherwise.
	exprBase int
	// stmtDepth counts nested statement executors (1 = the program's own list).
	stmtDepth int
	// stmtSafe says the construct that entered this statement executor has
	// declared all of its root groups.
	stmtSafe bool
	// stmtRoot marks the evaluation of a statement's own (outermost) expression.
	// A call made there has no half-evaluated enclosing expression above it, so
	// its body can take collection safe points of its own (see callFunc).
	stmtRoot bool
	// callIsStmtRoot is set by eval when the expression it is dispatching is a
	// call that is the whole statement. evalCall snapshots it before evaluating
	// its arguments, because evaluating those arguments clears the flag.
	callIsStmtRoot bool
	// gcWatermark is the handle frontier of collectable objects: everything
	// allocated after it is unconditionally live (see gc.go).
	gcWatermark int64
	gc          GCStats
	gcStress    bool
	gcThreshold int64
}

// obj is a heap value: a class, an instance, or a bound/unbound method.
type obj struct {
	kind   string           // "class" | "instance" | "method" | "list" | "superproxy"
	class  string           // class name (instance/method)
	attrs  map[string]int64 // instance attrs or class method-handle ids
	env    map[string]int64 // captured enclosing scope (kind=closure)
	fn     *FuncDef         // method body (kind=method)
	mname  string           // method name (kind=method)
	recv   int64            // bound receiver id (0 = unbound)
	base   int64            // base class id (kind=class) for inheritance
	elems  []int64          // list elements (kind=list)
	dvals  []int64          // dict values parallel to elems keys (kind=dict)
	sval   string           // string value (kind=str)
	fval   float64          // float value (kind=float)
	bval   int64            // bool payload, 0 or 1 (kind=bool) — the 0/1 a verdict was made from
	doc    string           // __doc__ string (def/class/closure objects)
	args   []int64          // bound arg values for coroutine
	result int64            // memoized coroutine result (0 = not yet run)
}

// tag returns the canonical %obj kind tag for this heap object. Both the
// interpreter heap and the AOT runtime derive tags from the same canonical
// table (value.go), so AOT and interpreter agree on the dynamic type model.
func (o *obj) tag() ValueTag {
	return objKindTag(o.kind)
}

// truthy implements Python truthiness for a runtime value: every number is false at
// zero (0, 0.0, -0.0), strings and containers are false when empty, None and False
// are false, and any other value — including a live object handle — is true.
//
// Conditions must go through this and never test `v != 0` directly: heap values are
// handles, so a raw handle test makes `if 0.0:`, `if "":` and `if []:` all true,
// which is both wrong and different from what the AOT backend compiles.
// boolEnv is the interpreter's side of the one bool predicate (pkg/lang/boolvalue.go).
// A name the program defines shadows a builtin here exactly as it shadows it at call time,
// so `def isinstance(x): return 1` is asked about its own body and not about the builtin's.
func (e *Evaluator) boolEnv() BoolEnv {
	return BoolEnv{
		Vars:     e.boolVars,
		Lookup:   func(nm string) *FuncDef { return e.funcs[nm] },
		Shadowed: func(nm string) bool { _, ok := e.funcs[nm]; return ok },
		Instance: e.instanceOperand,
	}
}

// instanceOperand answers whether an operand is an instance of a class the program declared,
// which is the question that turns `a < 4` into a call to `a.__lt__(4)`: the answer is then
// whatever that method returns, and dunder.gy's `__lt__` returns the int 1 — which is what
// CPython prints, so a verdict rendering here would be a new divergence dressed up as the
// fix of an old one (roadmap L6.6, checked against ADR 0257's rule).
func (e *Evaluator) instanceOperand(x Expr) bool {
	switch t := x.(type) {
	case *Name:
		v, ok := e.Vars[t.Value]
		if !ok {
			return false
		}
		o, isObj := e.heap[v]
		return isObj && o.kind == "instance"
	case *Call:
		nm, ok := t.Fn.(*Name)
		if !ok {
			return false
		}
		_, isClass := e.resolveClassID(nm.Value)
		return isClass
	}
	return false
}

// forgetBool retires a name's boolness. Every binding that is not a readable
// expression — a loop variable, a comprehension counter, a parameter, an unpack
// target, a `with ... as` — goes through here, because the alternative is a name that
// once held a verdict printing True forever after, which is a worse bug than the one this
// cycle fixes (roadmap L11.1 step 2, ADR 0257).
func (e *Evaluator) forgetBool(name string) {
	delete(e.boolVars, name)
}

// recordBool notes what an assignment teaches about a name: its latest value decides how
// print and str render it, and an assignment of anything else clears the status — so
// `flag = 1 == 1` prints True and a later `flag = 5` prints 5, the rule ADR 0172 set for
// None and this cycle extends to bools (roadmap L11.1 step 2, ADR 0257).
func (e *Evaluator) recordBool(name string, value Expr) {
	if IsBoolExpr(value, e.boolEnv()) {
		if e.boolVars == nil {
			e.boolVars = map[string]bool{}
		}
		e.boolVars[name] = true
		return
	}
	delete(e.boolVars, name)
}

// IsBoolExpr reports whether an expression's value is a bool in the scope currently
// running. The CLI's --json report and the --eval echo ask it, so a bool's type reads as
// "bool" and its result as True/False rather than as the 1 the slot holds (ADR 0257).
func (e *Evaluator) IsBoolExpr(x Expr) bool { return IsBoolExpr(x, e.boolEnv()) }

// BoolText renders a value that the caller already knows is a bool.
func (e *Evaluator) BoolText(v int64) string { return BoolText(e.truthy(v)) }

func (e *Evaluator) truthy(v int64) bool {
	if o, ok := e.heap[v]; ok {
		switch o.kind {
		case "float":
			return o.fval != 0
		case "bool":
			return o.bval != 0
		case "str":
			return o.sval != ""
		case "list", "set", "dict":
			return len(o.elems) > 0
		case "none":
			return false
		}
		return true
	}
	return v != 0
}

// tagOfVal returns the canonical %obj kind tag for a runtime value. Heap
// values map their obj.kind; plain ints/bools/None are raw i64s in the
// interpreter today, so they report the immediate tag (the %obj payload word
// carries the raw value, exactly as the AOT representation does).
func (e *Evaluator) tagOfVal(v int64) ValueTag {
	if o, ok := e.heap[v]; ok {
		return o.tag()
	}
	return TagInt
}

// setLoopVar binds a for-loop variable (a Name, or a Tuple of Names) to a value.
// For a Tuple, the value must be a list/tuple object whose length matches.
func (e *Evaluator) setLoopVar(v Expr, val int64) error {
	switch t := v.(type) {
	case *Name:
		e.Vars[t.Value] = val
		// A loop variable is bound by the iterable, not by an expression the compiler can
		// read, so whatever the name's previous binding said about boolness no longer holds
		// (roadmap L11.1 step 2, ADR 0257).
		e.forgetBool(t.Value)
		return nil
	case *Tuple:
		obj := e.heap[val]
		if obj == nil {
			return &EvalError{Msg: "cannot unpack non-iterable loop value"}
		}
		if len(obj.elems) != len(t.Elems) {
			return &EvalError{Msg: "cannot unpack %d values into %d loop variables"}
		}
		for i, n := range t.Elems {
			if nm, ok := n.(*Name); ok {
				e.Vars[nm.Value] = obj.elems[i]
				e.forgetBool(nm.Value)
			}
		}
		return nil
	}
	return &EvalError{Msg: "unsupported loop variable"}
}

// storeIndex implements subscript assignment: `obj[idx] = val`.
//
// A dict inserts when the key is new and updates in place otherwise (Python's
// assignment semantics — it never raises for a missing key). A list replaces an
// element within bounds. Sets and strings are immutable mappings here, so assigning
// into them is an error rather than a silent no-op.
func (e *Evaluator) storeIndex(ix *Index, val int64) error {
	objV, err := e.eval(ix.Obj)
	if err != nil {
		return err
	}
	idx, err := e.eval(ix.Idx)
	if err != nil {
		return err
	}
	o := e.heap[objV]
	if o == nil {
		return exnError("TypeError", "cannot assign to an index of a non-container")
	}
	switch o.kind {
	case "dict":
		// Both halves of the entry are slot values: d[True] = 1 and d[1] = True have to be found
		// and printed as what they are, and the key of an assignment is an expression the
		// interpreter can still read (Gap R.112, ADR 0259).
		key := e.slotVal(ix.Idx, idx)
		e.dictPut(o, key, val)
		return nil
	case "list":
		i := normPosIndex(idx, int64(len(o.elems)))
		if i < 0 || i >= int64(len(o.elems)) {
			return exnError("IndexError", "index out of range")
		}
		o.elems[i] = val
		return nil
	case "set":
		return exnError("TypeError", "cannot assign to a set element")
	case "str":
		return exnError("TypeError", "strings are immutable")
	}
	return exnError("TypeError", "cannot assign to an index of this value")
}

// normPosIndex turns a positional index into an element offset, counting from the end
// when it is negative: `xs[-1]` is the last element, `s[-1]` the last character. `pop`
// and slicing already behaved this way; read and write now share the same rule
// (roadmap L11.4, ADR 0210). It is deliberately *not* applied to dict and set subscripts
// — those are keys, and `-1` is a key you can store (`d[-1] = v` works in Python too).
// The result may still be out of range; the caller bounds-checks it.
func normPosIndex(idx, length int64) int64 {
	if idx < 0 {
		return idx + length
	}
	return idx
}

// resolveClassID returns the heap id of the class bound to name, if any.
// Classes can be referenced by their definition name (classIDs) or as a
// class value stored in a variable (e.g. `Alias = Point`).
func (e *Evaluator) resolveClassID(name string) (int64, bool) {
	if id, ok := e.classIDs[name]; ok {
		return id, true
	}
	if v, ok := e.Vars[name]; ok {
		if o, ok2 := e.heap[v]; ok2 && o.kind == "class" {
			return v, true
		}
	}
	// A class value reached through the module's globals. A function body runs with its own frame in
	// e.Vars, so `Alias = Point` — a module binding — is invisible there, and a class pattern in that
	// body used to fall through to *calling* the class (`TypeError: 'type' object is not callable`).
	// A bare name in a body means the executing module's scope, which is what e.curModule is for
	// (ADR 0227's view of the module; roadmap Gap B, ADR 0235).
	if e.curModule != nil {
		if v, ok := e.curModule[name]; ok {
			if o, ok2 := e.heap[v]; ok2 && o.kind == "class" {
				return v, true
			}
		}
	}
	return 0, false
}

// isCallableName reports whether `name` names something you call: a user function or a builtin.
// A class pattern's callee is a name, and whether that name is a function is what decides if
// `case name(x)` is a call compared against the subject or a reference to a class (roadmap Gap B,
// ADR 0235).
func (e *Evaluator) isCallableName(name string) bool {
	if _, ok := e.funcs[name]; ok {
		return true
	}
	return isPredeclaredName(name)
}

// holdsValue reports whether `name` has a binding in the scope chain a pattern may read: the
// executing frame, or the module that frame belongs to.
func (e *Evaluator) holdsValue(name string) bool {
	if _, ok := e.Vars[name]; ok {
		return true
	}
	if e.curModule != nil {
		if _, ok := e.curModule[name]; ok {
			return true
		}
	}
	return false
}

func (e *Evaluator) allocObj(kind string) int64 {
	e.nextID++
	e.allocCount++
	id := e.nextID
	e.heap[id] = &obj{kind: kind, attrs: map[string]int64{}}
	return id
}

// allocClosure creates a closure value capturing the current scope.
// allocStr allocates a boxed string value and returns its heap handle.
func (e *Evaluator) allocStr(val string) int64 {
	id := e.allocObj("str")
	e.heap[id].sval = val
	return id
}

// pySliceIndices computes the normalized start/stop/step for a slice
// following CPython's PySlice_GetIndicesEx semantics (used by s[a:b:c]).
func pySliceIndices(low, high, step int64, hasLow, hasHigh bool, n int64) (start, stop, stp int64) {
	if step > 0 {
		if hasLow {
			if low < 0 {
				low = maxInt(n+low, 0)
			} else {
				low = minInt(low, n)
			}
		} else {
			low = 0
		}
		if hasHigh {
			if high < 0 {
				high = maxInt(n+high, 0)
			} else {
				high = minInt(high, n)
			}
		} else {
			high = n
		}
		return low, high, step
	}
	// step < 0
	if hasLow {
		if low < 0 {
			low = maxInt(n+low, -1)
		} else {
			low = minInt(low, n-1)
		}
	} else {
		low = n - 1
	}
	if hasHigh {
		if high < 0 {
			high = maxInt(n+high, -1)
		} else {
			high = minInt(high, n-1)
		}
	} else {
		high = -1
	}
	return low, high, step
}

func maxInt(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

// allocFloat allocates a boxed float value and returns its heap handle.
func (e *Evaluator) allocFloat(val float64) int64 {
	id := e.allocObj("float")
	e.heap[id].fval = val
	return id
}

// allocBool allocates a boxed bool: the 0/1 a verdict was made from, plus the fact that it is a
// verdict. A bool is a number to this interpreter — True + 1 is 2, [True] == [1] is True, 1 in
// {True} is True — and every one of those questions is answered by the payload. The box exists
// for the one question the payload cannot answer: what to print. Inside a container slot there is
// no expression left to ask, which is why [True] printed [1] until a slot could say bool itself
// (roadmap Gap R.112, ADR 0259). It is the shape a float already has: a payload the read sites
// unbox, and a kind that decides the rendering.
func (e *Evaluator) allocBool(val int64) int64 {
	id := e.allocObj("bool")
	e.heap[id].bval = val
	return id
}

// boolOf reads a boxed bool's payload, mirroring floatOf.
func (e *Evaluator) boolOf(id int64) (int64, bool) {
	if o, ok := e.heap[id]; ok && o.kind == "bool" {
		return o.bval, true
	}
	return 0, false
}

// unboxBool answers "what number is this?" on behalf of every numeric, ordering and equality path
// in the interpreter: a bool is that number, anything else is itself. A float box needed the same
// treatment and got it per site; a bool box meets this one line at each entry point, because the
// two differ only in which rendering Repr chooses.
func (e *Evaluator) unboxBool(v int64) int64 {
	if b, ok := e.boolOf(v); ok {
		return b
	}
	return v
}

// slotVal is what goes INTO a container slot, as distinct from what an expression evaluates to. A
// bool is stored boxed, because a container is read back long after the expression that produced
// an element is out of scope: ADR 0257's print rule asks the AST, and a slot has no AST. Everything
// else is stored as it arrived — a float is already a box, an int already says its own number
// (roadmap Gap R.112, ADR 0259).
func (e *Evaluator) slotVal(el Expr, v int64) int64 {
	if e.isHandle(v) {
		return v
	}
	if e.IsBoolExpr(el) {
		return e.allocBool(v)
	}
	return v
}

// allocExn allocates an exception object of the given class with a message.
// kind="exn", class=type name, sval=message.
func (e *Evaluator) allocExn(exnType, msg string) int64 {
	id := e.allocObj("exn")
	o := e.heap[id]
	o.class = exnType
	o.sval = msg
	return id
}

// exnInfo returns (type, message) of an exception object (kind="exn"),
// or ("Exception", repr) for a non-exception value.
func (e *Evaluator) exnInfo(id int64) (string, string) {
	if o, ok := e.heap[id]; ok && o.kind == "exn" {
		return o.class, o.sval
	}
	return "Exception", e.Repr(id)
}

// floatOf returns the float value of a heap handle (kind=float), with a
// boolean indicating whether the handle is a boxed float.
func (e *Evaluator) floatOf(id int64) (float64, bool) {
	if o, ok := e.heap[id]; ok && o.kind == "float" {
		return o.fval, true
	}
	return 0, false
}

// strOf returns the string value of a heap handle, or "" if the handle is
// not a boxed string.
func (e *Evaluator) strOf(id int64) string {
	if o, ok := e.heap[id]; ok && o.kind == "str" {
		return o.sval
	}
	return ""
}

// reprNested renders a value *inside* a container. Strings are quoted the way Python's repr
// does — single quotes unless the text contains a single quote and no double quote — because
// [a, b] is not distinguishable from the list of those two bare words, while ['a', 'b'] is.
func (e *Evaluator) reprNested(id int64) string {
	if o, ok := e.heap[id]; ok && o.kind == "str" {
		return pyReprString(o.sval)
	}
	return e.Repr(id)
}

// pyReprString renders a Go string as Python would repr it.
func pyReprString(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var sb strings.Builder
	sb.WriteByte(quote)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == quote || c == '\\':
			sb.WriteByte('\\')
			sb.WriteByte(c)
		case c == '\n':
			sb.WriteString("\\n")
		case c == '\t':
			sb.WriteString("\\t")
		case c == '\r':
			sb.WriteString("\\r")
		default:
			sb.WriteByte(c)
		}
	}
	sb.WriteByte(quote)
	return sb.String()
}

// Repr renders a heap handle (or plain int) to its printable representation.
// IsNone reports whether v is the None singleton. Callers use it to tell "no value" from
// the integer 0 — the distinction `--eval` needs so a program ending in print(...) does not
// echo a stray line, and user code needs for `f() == None`.
func (e *Evaluator) IsNone(v int64) bool {
	if e.noneVal == 0 || v != e.noneVal {
		return false
	}
	o, ok := e.heap[v]
	return ok && o.kind == "none"
}

func (e *Evaluator) Repr(id int64) string {
	if o, ok := e.heap[id]; ok {
		switch o.kind {
		case "none":
			return "None"
		case "str":
			return o.sval
		case "float":
			return pyFloatRepr(o.fval)
		case "bool":
			// The name and not the number — and unquoted: Python puts a bool in no one's quotes,
			// inside a container included. reprNested's default lands here (Gap R.112).
			if o.bval != 0 {
				return "True"
			}
			return "False"
		case "list":
			parts := make([]string, 0, len(o.elems))
			for _, el := range o.elems {
				parts = append(parts, e.reprNested(el))
			}
			return "[" + strings.Join(parts, ", ") + "]"
		case "dict":
			// Keys go through Repr too: printing the raw value used to leak a heap handle
			// for a string key ({1048581: 1} instead of {'k': 1}).
			parts := make([]string, 0, len(o.elems))
			for i, k := range o.elems {
				parts = append(parts, e.reprNested(k)+": "+e.reprNested(o.dvals[i]))
			}
			return "{" + strings.Join(parts, ", ") + "}"
		case "set":
			// Python renders a set as {e1, e2} and the empty set as set().
			if len(o.elems) == 0 {
				return "set()"
			}
			parts := make([]string, 0, len(o.elems))
			for _, el := range o.elems {
				parts = append(parts, e.reprNested(el))
			}
			return "{" + strings.Join(parts, ", ") + "}"
		default:
			return "<" + o.kind + ">"
		}
	}
	return fmt.Sprintf("%d", id)
}

func (e *Evaluator) allocClosure(fn *FuncDef, env map[string]int64) int64 {
	e.nextID++
	id := e.nextID
	e.heap[id] = &obj{kind: "closure", fn: fn, env: env, attrs: map[string]int64{}, doc: fn.Doc}
	return id
}

// typeOfVal maps a runtime value to its static type Kind for gradual typing
// checks. Plain small int64s are ints/bools/none; heap ids are objects whose
// kind string maps to a Type.
// TypeOf reports the dynamic type name of a heap value id for the
// machine-readable --json interface (e.g. "int", "float", "str", "list").
func (e *Evaluator) TypeOf(v int64) string {
	return e.typeOfVal(v).Name()
}

func (e *Evaluator) typeOfVal(val int64) *Type {
	if o, ok := e.heap[val]; ok {
		switch o.kind {
		case "none":
			return TNone()
		case "float":
			return TFlt()
		case "bool":
			// A bool read out of a container knows its own type now, and so does --json:
			// "type": "bool" for xs[0] of [True] is what CPython's type() answers (Gap R.112).
			return TBool()
		case "str":
			return TStr()
		case "list":
			return TList(TDyn())
		case "dict":
			return TDict(TDyn(), TDyn())
		case "set":
			return TSet(TDyn())
		case "closure", "method", "class":
			return TFunc(nil, TDyn())
		default:
			return TDyn()
		}
	}
	return TInt()
}

// checkAnnot enforces a gradual type annotation on a runtime value. Dynamic
// annotations accept anything; otherwise the value's runtime kind must be
// assignable to the annotation.
func (e *Evaluator) checkAnnot(name string, ty *Type, val int64) error {

	// Literal type: the runtime value must equal the annotated constant.
	if ty.Kind == KindLiteral {
		if val != ty.LitVal {
			return &EvalError{Msg: "type mismatch: expected " + tyName(ty) + " for " + name}
		}
		return nil
	}
	if ty == nil || ty.Kind == KindDynamic {
		return nil
	}
	// Union annotation: accept if the value's runtime kind matches any member.
	// Literal type: the runtime value must equal the annotated constant.
	// Literal type: the runtime value must equal the annotated constant.
	if ty.Kind == KindLiteral {
		if val != ty.LitVal {
			return &EvalError{Msg: "type mismatch: expected " + tyName(ty) + " for " + name}
		}
		return nil
	}
	if ty.Kind == KindLiteral {
		if val != ty.LitVal {
			return &EvalError{Msg: "type mismatch: expected " + tyName(ty) + " for " + name}
		}
		return nil
	}
	if ty.Kind == KindUnion {
		for _, m := range ty.Members {
			if e.checkAnnot(name, m, val) == nil {
				return nil
			}
		}
		return &EvalError{Msg: "type mismatch: expected " + tyName(ty) + " but got " + tyName(e.typeOfVal(val)) + " for " + name}
	}
	// Nominal class annotation (L6.6): the value must be an instance of the
	// annotated class or of one of its subclasses, walking the base chain.
	if ty.Kind == KindClass {
		if o, ok := e.heap[val]; ok && o.kind == "instance" {
			if e.classList.Less(o.class, ty.ClassName) {
				return nil
			}
			return &EvalError{Msg: "type mismatch: expected " + tyName(ty) + " but got " + o.class + " for " + name}
		}
		return &EvalError{Msg: "type mismatch: expected " + tyName(ty) + " but got a non-instance value for " + name}
	}
	// Read-only protocol annotations are checked structurally by the checker, not
	// by the heap: a Sequence[T] annotation accepts any container value and a
	// Callable annotation any callable value — the element/signature rules are
	// static (see `gusty check`, L6.6).
	if ty.Kind == KindSequence {
		switch e.typeOfVal(val).Kind {
		case KindList, KindTuple, KindSet, KindString, KindIterator, KindSequence:
			return nil
		}
		return &EvalError{Msg: "type mismatch: expected " + tyName(ty) + " but got " + tyName(e.typeOfVal(val)) + " for " + name}
	}
	// An iterator is a dynamic producer (range/generators/yield-from are not all
	// heap objects), so an Iterator[T] annotation is static-only at runtime.
	if ty.Kind == KindIterator {
		return nil
	}
	if ty.Kind == KindCallable {
		if rt := e.typeOfVal(val); rt.Kind == KindFunc || rt.Kind == KindCallable {
			return nil
		}
		return &EvalError{Msg: "type mismatch: expected " + tyName(ty) + " but got " + tyName(e.typeOfVal(val)) + " for " + name}
	}
	// Tuples are heap lists in this runtime, so a tuple annotation accepts a
	// list-shaped value (arity + element types are static-only, see `gusty check`).
	if ty.Kind == KindTuple {
		switch e.typeOfVal(val).Kind {
		case KindTuple, KindList:
			return nil
		}
		return &EvalError{Msg: "type mismatch: expected " + tyName(ty) + " but got " + tyName(e.typeOfVal(val)) + " for " + name}
	}
	rt := e.typeOfVal(val)
	if rt.Kind == ty.Kind {
		return nil
	}
	// bools are stored as plain ints 0/1; plain ints are compatible with both
	// int and bool annotations (the interpreter cannot distinguish them).
	if rt.Kind == KindInt && (ty.Kind == KindInt || ty.Kind == KindBool) {
		return nil
	}
	return &EvalError{Msg: "type mismatch: expected " + tyName(ty) + " but got " + tyName(rt) + " for " + name}
}

func tyName(t *Type) string {
	switch t.Kind {
	case KindInt:
		return "int"
	case KindFloat:
		return "float"
	case KindBool:
		return "bool"
	case KindString:
		return "str"
	case KindNone:
		return "none"
	case KindList:
		return "list"
	case KindDict:
		return "dict"
	case KindSet:
		return "set"
	case KindTuple:
		return "tuple"
	case KindIterator:
		return "iterator"
	case KindFunc:
		return "func"
	case KindClass:
		if t.ClassName != "" {
			return t.ClassName
		}
		return "class"
	case KindSequence, KindCallable:
		return t.Name()
	case KindDynamic:
		return "any"
	case KindUnion:
		return t.Name()
	default:
		return "value"
	}
}

// callFunc evaluates a function body with params bound into a scope seeded
// from env (nil = empty scope). Nested defs inside the body become closures.

func (e *Evaluator) recordCall(err error, callee string, caller string, callSite Span) error {
	ee, ok := err.(*EvalError)
	if !ok {
		return err
	}
	cf := Frame{Name: caller, Line: callSite.Line, Col: callSite.Col}
	if ee.Traceback == nil {
		inner := Frame{Name: callee, Line: e.cur.Line, Col: e.cur.Col}
		ee.Traceback = []Frame{cf, inner}
	} else {
		ee.Traceback = append([]Frame{cf}, ee.Traceback...)
	}
	return err
}

// bodyBinds reports whether a function body binds `name` anywhere inside itself: an assignment to
// it, a tuple element, an augmented assignment, a `for` target, a `with ... as`. It deliberately does
// not look inside a nested `def` or a `class` body -- those are separate scopes, and a binding there
// says nothing about this one (ADR 0220). The answer is cached per body because the question is asked
// at every name read.
func (e *Evaluator) bodyBinds(fd *FuncDef, name string) bool {
	if e.curBodies == nil {
		e.curBodies = map[*FuncDef]map[string]bool{}
	}
	if set, ok := e.curBodies[fd]; ok {
		return set[name]
	}
	set := map[string]bool{}
	for _, pa := range fd.Params {
		set[pa.Name] = true
	}
	for _, st := range fd.Body {
		moduleBindingNames(st, set)
	}
	e.curBodies[fd] = set
	return set[name]
}

// callFuncNameForError names the callee in an arity sentence. A `lambda` is declared under the name
// the compiler generated for it (`lambda_0`), which the reader never wrote; the reference calls the
// same object `<lambda>`, so that is what a user sees, while a `def` keeps its own name because that
// is the name they gave it (roadmap Gap R.168, ADR 0284 -- and ADR 0215's rule that the wording is a
// contract, not an invention).
func callFuncNameForError(fd *FuncDef) string {
	if fd == nil {
		return "<lambda>"
	}
	// The interpreter names a lambda "lambda" (jit's own anonymous FuncDef) and the compiler numbers
	// its copies "lambda_0"; neither is a name the reader wrote, and the reference calls the same
	// object `<lambda>`.
	if fd.Name == "" || fd.Name == "lambda" || strings.HasPrefix(fd.Name, "lambda_") {
		return "<lambda>"
	}
	return fd.Name
}

// pluralFor is the one grammar word an arity sentence needs.
func pluralFor(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (e *Evaluator) callFunc(fd *FuncDef, argVals []int64, env map[string]int64, bodySafe bool) (int64, error) {
	caller := e.fnName
	callSite := e.cur
	savedFn := e.fnName
	e.fnName = fd.Name
	savedFD := e.curFD
	e.curFD = fd
	defer func() { e.curFD = savedFD }()
	// The callee's global scope is the module it was written in (Gap R.35). Restored on
	// the way out so a nested call cannot leave its module visible to its caller.
	savedModule := e.curModule
	if m, ok := e.funcModules[fd]; ok {
		e.curModule = m
	}
	defer func() { e.fnName = savedFn; e.curModule = savedModule }()

	scope := map[string]int64{}
	for k, v := range env {
		scope[k] = v
	}
	// How many arguments there were is asked HERE, once, on the road every caller shares. The
	// checked call road asked it on its own; `callClosure` -- the road for a callable read out of a
	// variable (`g = lambda x: x * 2`) -- asked nothing, so the loop below quietly ignored the extras
	// and defaulted a genuinely missing parameter to 0. `print(g(1, 2))` answered `2` and
	// `print(g())` answered `0`, both at **exit 0**, where CPython raises
	// `<lambda>() takes 1 positional argument but 2 were given` /
	// `missing 1 required positional argument: 'x'`. A wrong number at exit 0 outranks every other
	// defect class, which is why this moved into the shared road rather than into one caller
	// (roadmap Gap R.168, ADR 0284).
	if len(argVals) > len(fd.Params) {
		return 0, &EvalError{Msg: fmt.Sprintf("too many arguments for %s: it accepts %d argument%s, got %d", callFuncNameForError(fd), len(fd.Params), pluralFor(len(fd.Params)), len(argVals))}
	}
	for i := len(argVals); i < len(fd.Params); i++ {
		if fd.Params[i].Default == nil {
			return 0, &EvalError{Msg: fmt.Sprintf("missing argument %q for %s", fd.Params[i].Name, callFuncNameForError(fd))}
		}
	}
	// bind positional args, filling defaults for missing trailing params
	for i, p := range fd.Params {
		var av int64
		if i < len(argVals) {
			av = argVals[i]
		} else if p.Default != nil {
			dv, err := e.eval(p.Default)
			if err != nil {
				return 0, err
			}
			av = dv
		}
		if p.Annot != nil {
			if err := e.checkAnnot(p.Name, p.Annot, av); err != nil {
				return 0, err
			}
		}
		scope[p.Name] = av
	}
	if fd.Async {
		cid := e.allocObj("coro")
		co := e.heap[cid]
		co.fn = fd
		co.args = argVals
		return cid, nil
	}
	// The frame's local scope is a root for exactly the duration of the call
	// (L7.2): while the body runs its locals keep objects alive, and the moment
	// the call returns the collector stops looking at them. The caller's scope is
	// rooted too, because it is not reachable through e.Vars while the callee runs.
	restoreScope := e.swapScope(scope)
	defer restoreScope()
	prevRet := e.curRet
	e.curRet = fd.ReturnAnno
	if containsYield(fd.Body) {
		genH := e.allocObj("list")
		prev := e.yieldList
		e.yieldList = genH
		e.inCall = true
		_, err := e.runFuncBody(fd, bodySafe)
		e.inCall = false
		e.yieldList = prev
		e.curRet = prevRet
		if _, ok := err.(*returnSignal); ok {
			return genH, nil
		}
		return genH, e.recordCall(err, fd.Name, caller, callSite)
	}
	e.inCall = true
	rv, err := e.runFuncBody(fd, bodySafe)
	e.inCall = false
	e.curRet = prevRet
	if rs, ok := err.(*returnSignal); ok {
		return rs.val, nil
	}
	if err == nil {
		// No `return` executed: Python yields None here, not the value of the last
		// statement. Returning that (0 for a procedure) is what made `print(f())`
		// print 0 and made `f() == None` false.
		return e.noneVal, nil
	}
	return rv, e.recordCall(err, fd.Name, caller, callSite)
}

// callClosure invokes a closure value with its captured environment.

// evalDecorator resolves a decorator expression to a callable value.
// A bare Name may refer to a top-level function definition (a first-class
// value not representable as an int64), or to a closure/other value.
func (e *Evaluator) evalDecorator(dec Expr) (any, error) {
	if n, ok := dec.(*Name); ok {
		if fd, ok := e.funcs[n.Value]; ok {
			return fd, nil
		}
		if v, ok := e.Vars[n.Value]; ok {
			return v, nil
		}
	}
	return e.eval(dec)
}

// callDecValue invokes a decorator value (top-level FuncDef or closure) with
// the function value it decorates, returning the (possibly transformed) value.
func (e *Evaluator) callDecValue(decVal any, arg int64) (int64, error) {
	switch v := decVal.(type) {
	case *FuncDef:
		return e.callFunc(v, []int64{arg}, e.Vars, false)
	case int64:
		o := e.heap[v]
		if o != nil && o.kind == "closure" {
			return e.callFunc(o.fn, []int64{arg}, o.env, false)
		}
	}
	return 0, &EvalError{Msg: "decorator is not callable"}
}

func (e *Evaluator) callClosure(o *obj, n *Call) (int64, error) {
	stmtRootCall := e.callIsStmtRoot
	argVals := make([]int64, len(n.Args))
	for i, a := range n.Args {
		av, err := e.eval(a)
		if err != nil {
			return 0, err
		}
		argVals[i] = av
	}
	return e.callFunc(o.fn, argVals, o.env, stmtRootCall)
}

func NewEvaluator() *Evaluator {
	// Reserve a high handle base so boxed heap ids never collide with small
	// integer literal values (which are stored raw in lists, dict keys, vars).
	// Otherwise Repr(id) would format heap[id] as an object and recurse (e.g.
	// a list at handle 1 whose elems contain the raw int 1).
	ev := &Evaluator{Vars: map[string]int64{}, funcs: map[string]*FuncDef{}, externs: map[string]*ExternDecl{}, heap: map[int64]*obj{}, classIDs: map[string]int64{}, boolVars: map[string]bool{}, classList: NewClassIndex(), nextID: heapIDBase, fnName: "<module>", gcThreshold: gcAllocThresholdDefault, gcStress: gcEnvStress()}
	// The scope this evaluator starts in *is* the module scope; anchoring it keeps the
	// collector from sweeping what a function's global lookup will read (Gap R.35).
	ev.moduleVars = ev.Vars
	ev.curModule = ev.Vars
	ev.funcModules = map[*FuncDef]map[string]int64{}
	ev.globalScopeAt = map[uintptr]bool{}
	ev.anchorScope(ev.Vars)
	ev.noneVal = ev.allocObj("none")
	return ev
}

// rememberModuleScope records the scope a definition was executed in, so the code it
// runs later resolves unqualified names against *that* module rather than whichever scope
// happens to be current when it is called — the rule Python has, and the reason a function
// imported from a module still sees its own module's globals. The scope is also anchored
// as a GC root: nothing on the frame stack refers to it once the def has run.
func (e *Evaluator) rememberModuleScope(fd *FuncDef) {
	if e.funcModules == nil {
		e.funcModules = map[*FuncDef]map[string]int64{}
	}
	scope := e.Vars
	// A `def` executed *inside* a function body belongs to that function's module, not to its
	// frame — the frame is local, and an inner function reading a module constant is the same
	// ordinary program (`def outer(): def inner(): return G`). The current global scope is the
	// right answer there, and it is e.Vars itself at module level and in a module being imported.
	if e.curModule != nil {
		scope = e.curModule
	}
	e.funcModules[fd] = scope
	e.anchorScope(scope)
}

// sameScope compares two scopes by identity, since a Go map is not comparable.
func sameScope(a, b map[string]int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

// anchorScope keeps a module scope reachable for the collector, once.
func (e *Evaluator) anchorScope(scope map[string]int64) {
	if scope == nil {
		return
	}
	if e.globalScopeAt == nil {
		e.globalScopeAt = map[uintptr]bool{}
	}
	// A Go map is not comparable, so its identity is its address: one def per module
	// would otherwise append the same scope a hundred times and multiply the GC's work.
	k := reflect.ValueOf(scope).Pointer()
	if e.globalScopeAt[k] {
		return
	}
	e.globalScopeAt[k] = true
	e.globalScopes = append(e.globalScopes, scope)
}

// globalLookup resolves a name against the global scope of the call in flight.
func (e *Evaluator) globalLookup(name string) (int64, bool) {
	scope := e.curModule
	if scope == nil {
		scope = e.moduleVars
	}
	if scope == nil {
		return 0, false
	}
	// At module level e.Vars already *is* the module scope; comparing map identities is a
	// pointer question, since a Go map is not comparable.
	if reflect.ValueOf(scope).Pointer() == reflect.ValueOf(e.Vars).Pointer() {
		return 0, false
	}
	v, ok := scope[name]
	return v, ok
}

// EvalProgram evaluates prog's top-level statements and returns the value of
// the final expression statement (or last assignment). It returns an error on
// unsupported constructs.

func (e *Evaluator) EvalProgram(prog *Program) (int64, error) {
	return e.runStatements(prog.Stmts, true)
}

// runStatements executes one statement list as one executor. Before every
// statement it takes a collection safe point (L7.2, see gc.go), which is where
// the interpreter reclaims memory while a program is running instead of only
// between top-level statements.
//
// safe is the caller's claim that it has declared a root group for every handle
// it keeps live in Go locals across this body — a for-loop roots its iterable, a
// with-statement roots its manager. When that claim holds and no expression is
// in flight, the collector may advance its watermark here; otherwise it still
// collects, but only objects that were already past the watermark, which is what
// keeps half-evaluated expressions safe.
func (e *Evaluator) runStatements(stmts []Stmt, safe bool) (int64, error) {
	var last int64
	e.stmtDepth++
	prevSafe := e.stmtSafe
	e.stmtSafe = safe
	// The value of the previous statement lives in a Go local across the
	// boundaries below, so it is a root until this executor finishes.
	lastBox := e.pushRootBox()
	cookie := len(e.rootGroups) - 1
	defer func() {
		e.stmtDepth--
		e.stmtSafe = prevSafe
		e.popRoots(cookie)
	}()
	for _, st := range stmts {
		e.cur = st.Span()
		lastBox[0] = last
		// The first expression this statement evaluates is the statement root.
		e.stmtRoot = true
		e.beginStatementBody(safe)
		switch s := st.(type) {
		case *ClassDef:
			classID := e.allocObj("class")
			e.classIDs[s.Name] = classID
			cls := e.heap[classID]
			cls.doc = s.Doc
			// Record the declared base chain so nominal class annotations accept
			// subclass instances at runtime (L6.6).
			var baseNames []string
			for _, b := range s.Bases {
				if b != nil {
					baseNames = append(baseNames, b.Value)
				}
			}
			e.classList.Declare(s.Name, baseNames)
			// inheritance: the first base (if any) becomes the base class;
			// methods/attrs missing on the subclass resolve up the base chain.
			if len(s.Bases) > 0 {
				if baseID, ok := e.classIDs[s.Bases[0].Value]; ok {
					cls.base = baseID
				}
			}
			for _, m := range s.Body {
				fd, ok := m.(*FuncDef)
				if !ok {
					continue
				}
				methodID := e.allocObj("method")
				mo := e.heap[methodID]
				mo.class = s.Name
				mo.mname = fd.Name
				mo.fn = fd
				// A method reads module globals the same way a function does — `k = 5`
				// at module level is visible inside `def get(self)`.
				e.rememberModuleScope(fd)
				cls.attrs[fd.Name] = methodID
			}
		case *ImportStmt:
			if err := e.importModule(s.Module); err != nil {
				return 0, err
			}
			continue
		case *ExternDecl:
			e.externs[s.Name] = s
		case *FuncDef:
			// Nested defs bind a closure capturing the enclosing scope.
			if len(s.Decorators) > 0 {
				// @dec1 @dec2 def f -> f = dec2(dec1(f)). Build the base fn as a
				// closure so it can be passed to decorators as an int64 id.
				e.Vars[s.Name] = e.allocClosure(s, e.Vars)
				for _, dec := range s.Decorators {
					decVal, err := e.evalDecorator(dec)
					if err != nil {
						return 0, err
					}
					newFn, err := e.callDecValue(decVal, e.Vars[s.Name])
					if err != nil {
						return 0, err
					}
					e.Vars[s.Name] = newFn
				}
				continue
			}
			e.rememberModuleScope(s)
			if e.inCall {
				e.Vars[s.Name] = e.allocClosure(s, e.Vars)
			} else {
				e.funcs[s.Name] = s
				// A top-level `def` also **binds its name**, the way a `lambda` bound to a name does:
				// the reference has a function object for `f`, and reading the name is not a NameError
				// for a program that just declared it. Until this line `abs(f)` stopped the program with
				// `NameError: name 'f' is not defined` where CPython raises
				// `TypeError: bad operand type for abs(): 'function'` — the wrong class, which is the
				// catchability defect ADR 0211 and ADR 0228 count as a misclassing: `except TypeError:`
				// ran neither arm. The handle is the same shape a lambda gets, and `operandKind` already
				// names a `closure` object `function`, so the raise quotes one word on both engines
				// (roadmap Gap R.150, Gap R.151, ADR 0283). Calls are unaffected: the call road looks
				// `e.funcs` up first.
				e.Vars[s.Name] = e.allocClosure(s, e.Vars)
			}
			continue
		case *IfStmt:
			cond, err := e.eval(s.Cond)
			if err != nil {
				return 0, err
			}
			taken := false
			if e.truthy(cond) {
				rv, err := e.runBodyRooted(s.Then)
				if err != nil {
					return 0, err
				}
				last = rv
				taken = true
			} else {
				for _, eif := range s.Elifs {
					ec, err := e.eval(eif.Cond)
					if err != nil {
						return 0, err
					}
					if e.truthy(ec) {
						rv, err := e.runBodyRooted(eif.Then)
						if err != nil {
							return 0, err
						}
						last = rv
						taken = true
						break
					}
				}
			}
			if !taken && s.Else != nil {
				rv, err := e.runBodyRooted(s.Else)
				if err != nil {
					return 0, err
				}
				last = rv
			}
			continue
		case *MatchStmt:
			sub, err := e.eval(s.Subject)
			if err != nil {
				return 0, err
			}
			for _, c := range s.Cases {
				matched := false
				for _, p := range append([]Expr{c.Pattern}, c.Or...) {
					ok, err := e.matchPattern(sub, p)
					if err != nil {
						return 0, err
					}
					if ok {
						matched = true
						break
					}
				}
				if matched && c.Guard != nil {
					gv, err := e.eval(c.Guard)
					if err != nil {
						return 0, err
					}
					if !e.truthy(gv) {
						matched = false
					}
				}
				if matched {
					rv, err := e.evalBody(c.Body)
					if err != nil {
						return 0, err
					}
					last = rv
					break
				}
			}
			continue
		case *TryStmt:
			// A `try` is a chain of arms (ADR 0213) and `finally` is a deferred body, and the two
			// rules meet here: the deferred body runs on EVERY exit from the statement --
			// fall-through, a handled exception, a propagating one, and the transfers
			// (`return`/`break`/`continue`) that leave the block -- and an arm catches
			// exceptions only. Transfers travel as Go errors because that is how this
			// interpreter moves control, so matching them against an arm swallowed a
			// `return`: `try: return 1 / except: print("caught")` printed "caught" and
			// returned the next value, where CPython returns 1 (roadmap Gap R.23, ADR 0222).
			_, bodyErr := e.runBodyRooted(s.Body)
			caught, armErr := false, error(nil)
			if bodyErr != nil && catchesException(bodyErr) {
				for _, ec := range s.Excepts {
					// bare except, or except Exception, matches any exception;
					// otherwise match the raised exception class name exactly.
					matches := ec.Exn == nil || ec.Exn.Value == "Exception"
					if ee, ok := bodyErr.(*EvalError); ok && ee.ExnType != "" {
						matches = ec.Exn == nil || ec.Exn.Value == "Exception" ||
							ec.Exn.Value == ee.ExnType
					}
					if matches {
						_, armErr = e.runBodyRooted(ec.Body)
						caught = true
						break
					}
				}
			}
			// Exactly once, after whichever arm ran, and before the pending transfer is
			// released: a raise or a return inside `finally` replaces what was in flight,
			// which falls out of returning the finally body's own error unchanged.
			if len(s.Finally) > 0 {
				if _, ferr := e.runBodyRooted(s.Finally); ferr != nil {
					return 0, ferr
				}
			}
			if armErr != nil {
				return 0, armErr
			}
			if bodyErr != nil && !caught {
				return 0, bodyErr
			}
		case *WithStmt:
			m, err := e.eval(s.Expr)
			if err != nil {
				return 0, err
			}
			ent, err := e.callDunder(m, "__enter__", nil)
			if err != nil {
				return 0, err
			}
			if s.As != nil {
				e.Vars[s.As.Value] = ent
				e.forgetBool(s.As.Value)
			}
			_, bodyErr := e.evalBody(s.Body)
			if bodyErr != nil {
				if ee, ok := bodyErr.(*EvalError); ok {
					sup, err2 := e.callDunder(m, "__exit__", []int64{int64(len(ee.ExnType)), 0, 0})
					if err2 != nil {
						return 0, err2
					}
					if sup != 0 {
						return 0, nil
					}
					return 0, bodyErr
				}
				_, err2 := e.callDunder(m, "__exit__", []int64{0, 0, 0})
				if err2 != nil {
					return 0, err2
				}
				return 0, bodyErr
			}
			_, err = e.callDunder(m, "__exit__", []int64{0, 0, 0})
			if err != nil {
				return 0, err
			}
			continue
		case *WhileStmt:
			completed := true
			for {
				cond, err := e.eval(s.Cond)
				if err != nil {
					return 0, err
				}
				if !e.truthy(cond) {
					break
				}
				rv, err := e.runBodyRooted(s.Body)
				if err != nil {
					if ls, ok := err.(*loopSignal); ok {
						if ls.kind == "break" {
							completed = false
							break
						}
						continue
					}
					return 0, err
				}
				last = rv
			}
			if completed {
				rv, err := e.runBodyRooted(s.Else)
				if err != nil {
					return 0, err
				}
				last = rv
			}
			continue
		case *ForStmt:
			isRange := false
			if c, ok := s.Iter.(*Call); ok {
				if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" {
					isRange = true
				}
			}
			completed := true
			if n := s.Var; n != nil {
				if !isRange {
					itV, err := e.eval(s.Iter)
					if err != nil {
						return 0, err
					}
					if o, ok := e.heap[itV]; ok && (o.kind == "list" || o.kind == "set" || o.kind == "dict" || o.kind == "str") {
						if o.kind == "str" {
							for _, r := range o.sval {
								if err := e.setLoopVar(s.Var, e.allocStr(string(r))); err != nil {
									return 0, err
								}
								rv, err := e.runBodyRooted(s.Body, itV)
								if err != nil {
									if ls, ok := err.(*loopSignal); ok {
										if ls.kind == "break" {
											completed = false
											break
										}
										continue
									}
									return 0, err
								}
								last = rv
							}
						} else {
							for _, el := range o.elems {
								if s.Async {
									if co := e.heap[el]; co != nil && co.kind == "coro" {
										rv, aerr := e.runCoro(el)
										if aerr != nil {
											return 0, aerr
										}
										el = rv
									}
								}
								if err := e.setLoopVar(s.Var, el); err != nil {
									return 0, err
								}
								rv, err := e.runBodyRooted(s.Body, itV)
								if err != nil {
									if ls, ok := err.(*loopSignal); ok {
										if ls.kind == "break" {
											completed = false
											break
										}
										continue
									}
									return 0, err
								}
								last = rv
							}
						}
					} else {
						start, stop, err := e.rangeBounds(s.Iter)
						if err != nil {
							return 0, err
						}
						step, err := e.rangeStep(s.Iter)
						if err != nil {
							return 0, err
						}
						for i := start; (step > 0 && i < stop) || (step < 0 && i > stop); i += step {
							if err := e.setLoopVar(s.Var, i); err != nil {
								return 0, err
							}
							rv, err := e.runBodyRooted(s.Body)
							if err != nil {
								if ls, ok := err.(*loopSignal); ok {
									if ls.kind == "break" {
										completed = false
										break
									}
									continue
								}
								return 0, err
							}
							last = rv
						}
					}
				} else {
					start, stop, err := e.rangeBounds(s.Iter)
					if err != nil {
						return 0, err
					}
					step, err := e.rangeStep(s.Iter)
					if err != nil {
						return 0, err
					}
					for i := start; (step > 0 && i < stop) || (step < 0 && i > stop); i += step {
						if err := e.setLoopVar(s.Var, i); err != nil {
							return 0, err
						}
						rv, err := e.runBodyRooted(s.Body)
						if err != nil {
							if ls, ok := err.(*loopSignal); ok {
								if ls.kind == "break" {
									completed = false
									break
								}
								continue
							}
							return 0, err
						}
						last = rv
					}
				}
			}
			if completed {
				rv, err := e.runBodyRooted(s.Else)
				if err != nil {
					return 0, err
				}
				last = rv
			}
		case *AssignStmt:
			v, err := e.eval(s.Value)
			if err != nil {
				return 0, err
			}
			if s.Annot != nil {
				name := ""
				if n, ok := s.Target.(*Name); ok {
					name = n.Value
				}
				if err := e.checkAnnot(name, s.Annot, v); err != nil {
					return 0, err
				}
			}
			if n, ok := s.Target.(*Name); ok {
				e.Vars[n.Value] = v
				e.recordBool(n.Value, s.Value)
				last = v
			}
			if t, ok := s.Target.(*Tuple); ok {
				obj := e.heap[v]
				if obj == nil {
					return 0, exnError("TypeError", "cannot unpack non-iterable value")
				}
				if len(obj.elems) != len(t.Elems) {
					return 0, unpackArityErr(len(t.Elems), len(obj.elems))
				}
				for i, nm := range t.Elems {
					if n2, ok2 := nm.(*Name); ok2 {
						e.Vars[n2.Value] = obj.elems[i]
						e.forgetBool(n2.Value)
					}
				}
				last = v
			}

			if a, ok := s.Target.(*Attr); ok {
				objV, err := e.eval(a.Obj)
				if err != nil {
					return 0, err
				}
				o, ok := e.heap[objV]
				if ok && o.kind == "instance" {
					o.attrs[a.Name.Value] = v
					last = v
				}
			}
			// Subscript assignment: `d[k] = v` inserts or updates a dict entry,
			// `xs[i] = v` replaces a list element. Until both the parser and the
			// evaluator supported it, `d[k] = v` parsed as an expression statement and
			// the value was thrown away — the statement did nothing, silently, on both
			// backends.
			if ix, ok := s.Target.(*Index); ok {
				// The right-hand side's expression still says whether this slot is a verdict, and
				// it is the last place that does: xs[0] = True / d["k"] = 1 == 1 (Gap R.112, ADR 0259).
				if err := e.storeIndex(ix, e.slotVal(s.Value, v)); err != nil {
					return 0, err
				}
				last = v
			}
		case *AugAssignStmt:
			// value = target op rhs, then write the result back to the target.
			b := &BinOp{Op: s.Op, L: s.Target, R: s.Value}
			res, err := e.eval(b)
			if err != nil {
				return 0, err
			}
			switch t := s.Target.(type) {
			case *Name:
				e.Vars[t.Value] = res
				// An augmented assignment rebinds the name to the arithmetic, so the
				// verdict status goes with the value it replaces: `flag = 1 == 1` then
				// `flag += 1` holds the number 2 and prints 2 (ADR 0172's latest-binding
				// rule, extended to bools by ADR 0257).
				e.recordBool(t.Value, b)
				last = res
			case *Attr:
				objV, err := e.eval(t.Obj)
				if err != nil {
					return 0, err
				}
				o, ok := e.heap[objV]
				if ok && o.kind == "instance" {
					o.attrs[t.Name.Value] = res
					last = res
				}
			default:
				return 0, fmt.Errorf("augmented assignment: unsupported target %T", s.Target)
			}
		case *ExprStmt:
			v, err := e.eval(s.Expr)
			if err != nil {
				return 0, err
			}
			last = v
		case *ReturnStmt:
			if s.Expr != nil {
				v, err := e.eval(s.Expr)
				if err != nil {
					return 0, err
				}
				if e.curRet != nil {
					if err := e.checkAnnot("return", e.curRet, v); err != nil {
						return 0, err
					}
				}
				return 0, &returnSignal{val: v}
			}
			// bare `return` yields None, not 0 (ADR 0172)
			return 0, &returnSignal{val: e.noneVal}
		case *YieldStmt:
			v, err := e.eval(s.Expr)
			if err != nil {
				return 0, err
			}
			if e.yieldList != 0 {
				if o, ok := e.heap[e.yieldList]; ok {
					o.elems = append(o.elems, v)
				}
				last = v
				continue
			}
			last = v
			continue
		case *YieldFromStmt:
			if e.yieldList != 0 {
				gl, ok := e.heap[e.yieldList]
				if ok {
					elems, err := e.evalIterable(s.Expr)
					if err != nil {
						return 0, err
					}
					gl.elems = append(gl.elems, elems...)
					continue
				}
			}
			return 0, &EvalError{Msg: "yield from outside a generator"}
		case *RaiseStmt:
			// raise Exception("msg") / raise ValueError("msg") etc.
			if s.Expr == nil {
				return 0, exnError("Exception", "raised")
			}
			// `raise IndexError` (the class itself, not a call) raises that class with
			// no message, like Python. Evaluating the name would be an undefined-name
			// error, because built-in exception classes are not bindings.
			if n, ok := s.Expr.(*Name); ok && isExnClass(n.Value) {
				return 0, exnError(n.Value, n.Value)
			}
			v, err := e.eval(s.Expr)
			if err != nil {
				return 0, err
			}
			et, em := e.exnInfo(v)
			return 0, exnError(et, em)
		case *BreakStmt:
			return 0, &loopSignal{kind: "break"}
		case *ContinueStmt:
			return 0, &loopSignal{kind: "continue"}
		case *PassStmt, *TypeAliasStmt:
			// no-op statement (type aliases are compile-time only; L5.7)
			continue
		default:
			return 0, &EvalError{Msg: "unsupported statement for eval"}
		}
	}
	return last, nil
}

func (e *Evaluator) eval(x Expr) (int64, error) {
	e.exprDepth++
	root := e.stmtRoot
	e.stmtRoot = false
	_, callRoot := x.(*Call)
	e.callIsStmtRoot = root && callRoot
	defer func() { e.exprDepth--; e.stmtRoot = root }()
	switch n := x.(type) {
	case *IntLit:
		return n.Value, nil
	case *StrLit:
		return e.allocStr(n.Value), nil
	case *FString:
		var b strings.Builder
		for _, part := range n.Parts {
			if part.Lit != "" {
				b.WriteString(part.Lit)
				continue
			}
			v, err := e.eval(part.Expr)
			if err != nil {
				return 0, err
			}
			// An interpolated bool contributes its word, not its digit: `f"{1 == 1}"` is
			// "True", and the f-string, str() and print ask one predicate the same question
			// so no two renderings of the same bool can differ (ADR 0257).
			if IsBoolExpr(part.Expr, e.boolEnv()) {
				b.WriteString(BoolText(e.truthy(v)))
				continue
			}
			b.WriteString(e.Repr(v))
		}
		return e.allocStr(b.String()), nil
	case *FloatLit:
		return e.allocFloat(n.Value), nil
	case *BoolLit:
		if n.Value {
			return 1, nil
		}
		return 0, nil
	case *NoneLit:
		return e.noneVal, nil
	case *AssignExpr:
		v, err := e.eval(n.Value)
		if err != nil {
			return 0, err
		}
		e.Vars[n.Name.Value] = v
		e.recordBool(n.Name.Value, n.Value)
		return v, nil
	case *Name:
		if v, ok := e.Vars[n.Value]; ok {
			return v, nil
		}
		// A name this body binds anywhere inside itself is local to it, so neither the closure
		// environment nor the module is consulted -- and reading it on a path that never assigned
		// it is UnboundLocalError, the class a handler matches on, not the NameError an
		// never-local name gets (CPython's split; roadmap Gap R.36 + R.39, ADR 0228). Without this
		// the fallback found the module's value and `def f(): print(v); v = 2` printed a number
		// where CPython raises.
		if e.curFD != nil && e.bodyBinds(e.curFD, n.Value) {
			return 0, exnError("UnboundLocalError", "cannot access local variable '"+n.Value+"' where it is not associated with a value")
		}
		if v, ok := e.globalLookup(n.Value); ok {
			return v, nil
		}
		if id, ok := e.classIDs[n.Value]; ok {
			return id, nil
		}
		// The program asked for a name that was never bound. That is a NameError — a
		// catchable language event — and not an interpreter complaint: the untyped variant
		// could not be handled by `except NameError:` and read like an internal report
		// (roadmap Gap R.25, ADR 0214).
		return 0, exnError("NameError", "name '"+n.Value+"' is not defined")
	case *BinOp:
		return e.evalBin(n)
	case *ChainCompare:
		// Python's chain: `a < b < c` is `a < b` AND `b < c`, with the middle operand evaluated ONCE.
		// The chain is walked rather than desugared to `and`, because `and` short-circuits and Python
		// does not: with a call in the middle of a chain the reference calls it exactly once and does
		// not skip the later comparisons (roadmap L12.1 / Gap R.53, ADR 0288). Each link asks the same
		// question evalBin asks of an ordinary comparison, so a chain gets the int, text, float and
		// container answers for free instead of a second set.
		return e.evalChain(n)
	case *UnOp:
		v, err := e.eval(n.X)
		if err != nil {
			return 0, err
		}
		switch n.Op {
		case "-":
			// The negation asks the operand's kind before it asks the int evaluator: a text, None, a
			// container or an instance used to reach `-` holding the heap id it is stored as, and the
			// program printed -(2^48 + n) at exit 0 where the reference stops (roadmap Gap R.137, ADR 0266).
			return e.negate(v)
		case "not":
			if !e.truthy(v) {
				return 1, nil
			}
			return 0, nil
		}
		return 0, &EvalError{Msg: "unsupported unary " + n.Op}
	case *Attr:
		// __doc__ introspection on a top-level def/class name: these have no
		// runtime object (top-level functions live in e.funcs as AST nodes).
		if n.Name.Value == "__doc__" {
			if nm, ok := n.Obj.(*Name); ok {
				if fd, ok := e.funcs[nm.Value]; ok {
					return e.allocStr(fd.Doc), nil
				}
				if cid, ok := e.classIDs[nm.Value]; ok {
					if c := e.heap[cid]; c != nil {
						return e.allocStr(c.doc), nil
					}
				}
			}
		}
		objV, err := e.eval(n.Obj)
		if err != nil {
			return 0, err
		}
		if n.Name.Value == "__doc__" {
			// __doc__ on a closure/class value: the object carries doc on the
			// heap object itself (not in attrs).
			if o, ok := e.heap[objV]; ok {
				return e.allocStr(o.doc), nil
			}
		}
		o, ok := e.heap[objV]
		if !ok {
			return 0, &EvalError{Msg: "attribute access on non-object"}
		}
		if o.kind == "module" {
			// mod.attr resolves a top-level name from the imported module.
			if v, ok := o.attrs[n.Name.Value]; ok {
				return v, nil
			}
			return 0, &EvalError{Msg: "no name " + n.Name.Value + " in module"}
		}
		if o.kind == "instance" {
			if v, ok := o.attrs[n.Name.Value]; ok {
				return v, nil
			}
			classID, ok := e.classIDs[o.class]
			if !ok {
				return 0, &EvalError{Msg: "unknown class " + o.class}
			}
			// inheritance: fall back to the class and its bases.
			if mID, ok := e.resolveMethod(classID, n.Name.Value); ok {
				e.heap[mID].recv = objV
				return mID, nil
			}
			return 0, exnError("AttributeError", "'"+o.class+"' object has no attribute '"+n.Name.Value+"'")
		}
		if o.kind == "class" {
			if mID, ok := e.resolveMethod(e.classIDFor(objV), n.Name.Value); ok {
				return mID, nil
			}
			return 0, exnError("AttributeError", "type object '"+e.classDisplayName(o)+"' has no attribute '"+n.Name.Value+"'")
		}
		if o.kind == "superproxy" {
			// super() proxy: resolve methods on the base class only, bound to
			// the current instance (o.recv), so overridden methods can delegate.
			baseID := o.base
			if mID, ok := e.resolveMethod(baseID, n.Name.Value); ok {
				e.heap[mID].recv = o.recv
				return mID, nil
			}
			return 0, exnError("AttributeError", "super object has no attribute '"+n.Name.Value+"'")
		}
		return 0, exnError("AttributeError", "method object has no attribute '"+n.Name.Value+"'")
	case *AwaitExpr:
		v, err := e.eval(n.Expr)
		if err != nil {
			return 0, err
		}
		if co := e.heap[v]; co != nil && co.kind == "coro" {
			return e.runCoro(v)
		}
		return v, nil
	case *Call:
		return e.evalCall(n)
	case *ListLit:
		h := e.allocObj("list")
		o := e.heap[h]
		for _, el := range n.Elems {
			ev, err := e.eval(el)
			if err != nil {
				return 0, err
			}
			// The element goes in as what a slot can say back. The AST is still in scope here,
			// and this is the last place in the interpreter where it will be (Gap R.112, ADR 0259).
			o.elems = append(o.elems, e.slotVal(el, ev))
		}
		return h, nil
	case *Tuple:
		h := e.allocObj("list")
		o := e.heap[h]
		for _, el := range n.Elems {
			ev, err := e.eval(el)
			if err != nil {
				return 0, err
			}
			// The element goes in as what a slot can say back. The AST is still in scope here,
			// and this is the last place in the interpreter where it will be (Gap R.112, ADR 0259).
			o.elems = append(o.elems, e.slotVal(el, ev))
		}
		return h, nil
	case *CondExpr:
		// ternary `then if cond else otherwise`: choose the branch by truthiness.
		cond, err := e.eval(n.Cond)
		if err != nil {
			return 0, err
		}
		if e.truthy(cond) {
			return e.eval(n.If)
		}
		return e.eval(n.Else)

	case *Index:
		objV, err := e.eval(n.Obj)
		if err != nil {
			return 0, err
		}
		idx, err := e.eval(n.Idx)
		if err != nil {
			return 0, err
		}
		o := e.heap[objV]
		if o == nil {
			// `x[0]` where x holds no object at all. "cannot index null" was both
			// untyped and wrong about the program — the value is not null, it is
			// whatever the variable holds (Gap R.25).
			return 0, exnError("TypeError", e.valueTypeName(objV)+" object is not subscriptable")
		}
		switch o.kind {
		case "list":
			i := normPosIndex(idx, int64(len(o.elems)))
			if i < 0 || i >= int64(len(o.elems)) {
				return 0, exnError("IndexError", "index out of range")
			}
			return o.elems[i], nil
		case "dict":
			for i, k := range o.elems {
				if e.dictKeyEq(k, idx) {
					return o.dvals[i], nil
				}
			}
			return 0, exnError("KeyError", "key not found")
		case "set":
			// Subscripting a set is a documented gusty extension, not Python (docs/language.md § Dicts &
			// sets, ledger rows `programs/data_b` and `programs/features_b` are `oracle:
			// not_applicable` because CPython rejects the shape): the subscript is a member the set is
			// asked about, and the answer is that member or a KeyError. The tag-dispatched arm the
			// compiled backend uses one level down reads a set slot the same way, so the extension is
			// one rule rather than two (roadmap L11.1, ADR 0251). What the arm may not do is answer by
			// position — see the roadmap row that measures where the docs and this line disagree.
			for _, el := range o.elems {
				if el == idx {
					return el, nil
				}
			}
			return 0, exnError("KeyError", "not in set")
		case "str":
			// `s[1]` is a one-character *string*, counted in code points — not the byte. Answering
			// the code point was a type error that propagated everywhere the value went:
			// `s[0] + s[2]` did arithmetic and printed 196, `s[1] == "b"` said false, `len(s[1])`
			// and `s[1].upper()` and `ord(s[1])` all trapped (roadmap Gap R.45, ADR 0225). A
			// byte-wise index was also wrong for any text outside ASCII, which `len` and slicing
			// already count in code points.
			runes := []rune(o.sval)
			i := normPosIndex(idx, int64(len(runes)))
			if i < 0 || i >= int64(len(runes)) {
				return 0, exnError("IndexError", "string index out of range")
			}
			return e.allocStr(string(runes[i])), nil
		default:
			return 0, exnError("TypeError", e.valueTypeName(objV)+" object is not subscriptable")
		}

	case *Slice:
		objH, err := e.eval(n.Obj)
		if err != nil {
			return 0, err
		}
		o := e.heap[objH]
		if o == nil {
			return 0, &EvalError{Msg: "slice of null"}
		}
		if o.kind != "list" && o.kind != "str" {
			return 0, &EvalError{Msg: "cannot slice this value"}
		}
		lowRaw := int64(0)
		highRaw := int64(0)
		step := int64(1)
		if n.Low != nil {
			v, err := e.eval(n.Low)
			if err != nil {
				return 0, err
			}
			lowRaw = v
		}
		if n.High != nil {
			v, err := e.eval(n.High)
			if err != nil {
				return 0, err
			}
			highRaw = v
		}
		if n.Step != nil {
			v, err := e.eval(n.Step)
			if err != nil {
				return 0, err
			}
			step = v
			if step == 0 {
				return 0, &EvalError{Msg: "slice step cannot be zero"}
			}
		}
		// A string is a sequence of code points, not of bytes, everywhere its position is
		// asked about: `s[1]`, `len(s)` and `s[1:3]` count the same things (ADR 0225). A
		// byte-wise slice could also cut a multi-byte character in half.
		var runes []rune
		length := int64(0)
		if o.kind == "str" {
			runes = []rune(o.sval)
			length = int64(len(runes))
		} else {
			length = int64(len(o.elems))
		}
		start, stop, stp := pySliceIndices(lowRaw, highRaw, step, n.Low != nil, n.High != nil, length)
		if o.kind == "str" {
			var sb strings.Builder
			for i := start; (stp > 0 && i < stop) || (stp < 0 && i > stop); i += stp {
				sb.WriteRune(runes[i])
			}
			return e.allocStr(sb.String()), nil
		}
		var elems []int64
		for i := start; (stp > 0 && i < stop) || (stp < 0 && i > stop); i += stp {
			elems = append(elems, o.elems[i])
		}
		nh := e.allocObj("list")
		e.heap[nh].elems = elems
		return nh, nil

	case *DictLit:
		h := e.allocObj("dict")
		o := e.heap[h]
		for i, k := range n.Keys {
			kv, err := e.eval(k)
			if err != nil {
				return 0, err
			}
			vv, err := e.eval(n.Vals[i])
			if err != nil {
				return 0, err
			}
			// A literal is built entry by entry through the dict's own key rule, not appended:
			// `{"a": 1, "a": 2}` is one entry holding 2, as CPython and the compiled fold answer
			// (roadmap Gap R.120, ADR 0260).
			e.dictPut(o, e.slotVal(k, kv), e.slotVal(n.Vals[i], vv))
		}
		return h, nil
	case *SetLit:
		h := e.allocObj("set")
		o := e.heap[h]
		for _, el := range n.Elems {
			v, err := e.eval(el)
			if err != nil {
				return 0, err
			}
			v = e.slotVal(el, v)
			found := false
			for _, x := range o.elems {
				// Members are compared as values, not as words: {True, 1} is one member in
				// Python, and the first spelling inserted is the one the set keeps and prints
				// — which is eqVal's question, not `==` on two handles (Gap R.112, ADR 0259).
				if e.eqVal(x, v) {
					found = true
					break
				}
			}
			if !found {
				o.elems = append(o.elems, v)
			}
		}
		return h, nil
	case *Comp:
		return e.evalComp(n)
	case *Generator:
		return e.evalGen(n)
	case *Lambda:
		// lambda params: body => anonymous FuncDef + closure capturing env.
		fd := &FuncDef{
			Name:   "lambda",
			Params: n.Params,
			Body:   []Stmt{&ReturnStmt{Expr: n.Body}},
		}
		h := e.allocClosure(fd, e.Vars)
		return h, nil
	default:
		return 0, &EvalError{Msg: "unsupported expression for eval"}
	}
}

func (e *Evaluator) evalComp(c *Comp) (int64, error) {
	it, err := e.eval(c.Iter)
	if err != nil {
		return 0, err
	}
	var items []int64
	var o *obj
	if h, ok := e.heap[it]; ok {
		o = h
		items = h.elems
	} else {
		lo, hi, err := e.rangeBounds(c.Iter)
		if err != nil {
			return 0, &EvalError{Msg: "comprehension over non-object"}
		}
		step := int64(1)
		if r, ok := c.Iter.(*Call); ok && len(r.Args) == 3 {
			step, err = e.eval(r.Args[2])
			if err != nil {
				return 0, err
			}
			if step == 0 {
				return 0, &EvalError{Msg: "range step cannot be zero"}
			}
		}
		if step > 0 {
			for v := lo; v < hi; v += step {
				items = append(items, v)
			}
		} else {
			for v := lo; v > hi; v += step {
				items = append(items, v)
			}
		}
	}
	if o != nil && o.kind == "dict" {
		items = o.elems
	}
	var rh int64
	switch c.Kind {
	case CompList:
		rh = e.allocObj("list")
	case CompSet:
		rh = e.allocObj("set")
	case CompDict:
		rh = e.allocObj("dict")
	default:
		return 0, &EvalError{Msg: "unknown comprehension kind"}
	}
	ro := e.heap[rh]
	for _, item := range items {
		e.Vars[c.ForVar.Value] = item
		if c.Cond != nil {
			cv, err := e.eval(c.Cond)
			if err != nil {
				return 0, err
			}
			if !e.truthy(cv) {
				continue
			}
		}
		switch c.Kind {
		case CompList:
			v, err := e.eval(c.Elems[0])
			if err != nil {
				return 0, err
			}
			// A comprehension builds an ordinary container, so its elements are ordinary slot
			// values: [x for x in [True]] prints [True], and the element expression is the only
			// thing left that can say so (Gap R.112, ADR 0259).
			ro.elems = append(ro.elems, e.slotVal(c.Elems[0], v))
		case CompSet:
			v, err := e.eval(c.Elems[0])
			if err != nil {
				return 0, err
			}
			v = e.slotVal(c.Elems[0], v)
			dup := false
			for _, x := range ro.elems {
				if e.eqVal(x, v) {
					dup = true
					break
				}
			}
			if !dup {
				ro.elems = append(ro.elems, v)
			}
		case CompDict:
			k, err := e.eval(c.Keys[0])
			if err != nil {
				return 0, err
			}
			v, err := e.eval(c.Vals[0])
			if err != nil {
				return 0, err
			}
			// The comprehension writes through the same door as `d[k] = v`: a key the dict already
			// holds keeps its place and takes the new value, so `{1: 2 for x in [1, 2]}` is one
			// entry of length 1 (roadmap Gap R.118, ADR 0260).
			e.dictPut(ro, e.slotVal(c.Keys[0], k), e.slotVal(c.Vals[0], v))
		}
	}
	return rh, nil
}
func (e *Evaluator) evalGen(g *Generator) (int64, error) {
	itv, err := e.eval(g.Iter)
	if err != nil {
		return 0, err
	}
	var items []int64
	if o, ok := e.heap[itv]; ok {
		switch o.kind {
		case "list", "str", "dict", "set":
			items = o.elems
		}
	}
	// generator expression evaluates eagerly to a list of yielded values.
	id := e.allocObj("list")
	lo := e.heap[id]
	for _, it := range items {
		e.Vars[g.ForVar.Value] = it
		if g.Cond != nil {
			cv, err := e.eval(g.Cond)
			if err != nil {
				return 0, err
			}
			if cv == 0 {
				continue
			}
		}
		for _, el := range g.Elems {
			ev, err := e.eval(el)
			if err != nil {
				return 0, err
			}
			lo.elems = append(lo.elems, ev)
		}
	}
	return id, nil
}

// eqVal reports value equality between two handles, mirroring `==` semantics:
// numeric equality via float promotion, string content equality, containers compared
// element-wise, and handle identity for everything else. Identity is `is`; `==` on a
// container is not (ADR 0189).
func (e *Evaluator) eqVal(l, r int64) bool {
	// A bool is the number it behaves like, before any of the rules below look at it: True == 1,
	// [True] == [1], True in [1], and {True, 1} dedups — all decided by the payload the box
	// carries (roadmap Gap R.112, ADR 0259).
	l, r = e.unboxBool(l), e.unboxBool(r)
	if l == r {
		// The same handle is equal to itself under every rule below: an immediate is the same
		// number, an interned string is the same text, a container is the same container.
		// `is` is the operator that asks about identity; it does not come through here.
		return true
	}
	if lf, ok := e.floatOf(l); ok {
		rf, rfok := e.floatOf(r)
		if !rfok {
			rf = float64(r)
		}
		return lf == rf
	}
	if rf, ok := e.floatOf(r); ok {
		// A float on the RIGHT with a plain integer on the left is still a numeric question:
		// `1 == 1.0` is True in Python, and answering False was an answer rather than a
		// refusal — the asymmetry (1.0 == 1 worked) is what made it survive, because a test
		// written from the working direction never sees it (roadmap Gap R.29, ADR 0221).
		// Anything else that is an object — a container, a string — is a different type, and
		// Python says False for `1 == [1.0]`, so the handle test is what keeps this honest.
		if e.isHandle(l) {
			return false
		}
		return rf == float64(l)
	}
	if lo, ok := e.heap[l]; ok && lo.kind == "str" {
		if ro, ok := e.heap[r]; ok && ro.kind == "str" {
			return lo.sval == ro.sval
		}
		return false
	}
	// Containers compare by value, the way Python's `==` does: same kind, same size, and
	// every element (or every key with its value) equal under this same rule. Until now
	// this fell through to `l == r`, so `[1, 2] == [1, 2]` was False and `xs == ys` was
	// False for equal containers — answers, not refusals, on both backends (ADR 0189).
	lo, lok := e.heap[l]
	ro, rok := e.heap[r]
	if lok != rok {
		return false
	}
	if !lok {
		return l == r
	}
	if lo.kind != ro.kind {
		return false
	}
	switch lo.kind {
	case "list":
		if len(lo.elems) != len(ro.elems) {
			return false
		}
		for i := range lo.elems {
			if !e.eqVal(lo.elems[i], ro.elems[i]) {
				return false
			}
		}
		return true
	case "set":
		if len(lo.elems) != len(ro.elems) {
			return false
		}
		// A set is unordered: equal means each side contains the other, not the same slot
		// order — {1, 2} == {2, 1} is True in Python and would be False on a positional walk.
		for _, el := range lo.elems {
			found := false
			for _, other := range ro.elems {
				if e.eqVal(el, other) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	case "dict":
		if len(lo.elems) != len(ro.elems) || len(lo.dvals) != len(ro.dvals) {
			return false
		}
		for i, k := range lo.elems {
			matched := false
			for j, ok2 := range ro.elems {
				if !e.eqVal(k, ok2) {
					continue
				}
				if e.eqVal(lo.dvals[i], ro.dvals[j]) {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		return true
	}
	// Instances, classes, closures, methods: identity, as in Python without __eq__.
	return l == r
}

// contains reports whether container holds v: list/set element membership, dict
// key membership, or (for a string container) substring membership.
func (e *Evaluator) contains(container, v int64) (bool, error) {
	co, ok := e.heap[container]
	if !ok {
		return false, nil
	}
	switch co.kind {
	case "list", "set":
		for _, el := range co.elems {
			if e.eqVal(v, el) {
				return true, nil
			}
		}
		return false, nil
	case "dict":
		for _, k := range co.elems {
			if e.eqVal(v, k) {
				return true, nil
			}
		}
		return false, nil
	case "str":
		if vo, ok := e.heap[v]; ok && vo.kind == "str" {
			return strings.Contains(co.sval, vo.sval), nil
		}
		return false, nil
	}
	return false, nil
}

// dunderForBinOp returns the dunder method name for a binary operator, if any.
func dunderForBinOp(op string) string {
	switch op {
	case "+":
		return "__add__"
	case "-":
		return "__sub__"
	case "*":
		return "__mul__"
	case "/":
		return "__truediv__"
	case "//":
		return "__floordiv__"
	case "%":
		return "__mod__"
	case "**":
		return "__pow__"
	case "==":
		return "__eq__"
	case "!=":
		return "__ne__"
	case "<":
		return "__lt__"
	case "<=":
		return "__le__"
	case ">":
		return "__gt__"
	case ">=":
		return "__ge__"
	}
	return ""
}

// reflectedDunder returns the reflected dunder name for a dunder method name,
// used when the left operand is not overloaded but the right operand is.
func reflectedDunder(name string) string {
	switch name {
	case "__add__":
		return "__radd__"
	case "__sub__":
		return "__rsub__"
	case "__mul__":
		return "__rmul__"
	case "__truediv__":
		return "__rtruediv__"
	case "__floordiv__":
		return "__rfloordiv__"
	case "__mod__":
		return "__rmod__"
	case "__pow__":
		return "__rpow__"
	case "__eq__":
		return "__eq__"
	case "__ne__":
		return "__ne__"
	case "__lt__":
		return "__gt__"
	case "__le__":
		return "__ge__"
	case "__gt__":
		return "__lt__"
	case "__ge__":
		return "__le__"
	}
	return ""
}

// dunderCall resolves and invokes a dunder method `name` on the instance at
// handle `self`, binding self as the receiver and `args` as the arguments.
// It reports whether the instance has such a method, and any call error.
func (e *Evaluator) dunderCall(self int64, name string, args ...int64) (int64, bool, error) {
	o := e.heap[self]
	if o == nil || o.kind != "instance" {
		return 0, false, nil
	}
	mID, ok := e.resolveMethod(e.classIDs[o.class], name)
	if !ok {
		return 0, false, nil
	}
	rv, err := e.callMethod(e.heap[mID], self, args)
	if err != nil {
		return 0, true, err
	}
	return rv, true, nil
}

// evalChain walks a comparison chain. The middle operands are evaluated here, once each, and handed to
// both neighbours; the first and last operands are evaluated once too, by the same walk. A chain that
// fails at any link answers False without skipping the remaining evaluations the reference still makes
// — which is why this is a loop over values rather than a fold of lazily-and-ed comparisons.
func (e *Evaluator) evalChain(n *ChainCompare) (int64, error) {
	if len(n.Operands) != len(n.Ops)+1 || len(n.Ops) == 0 {
		return 0, &EvalError{Msg: "malformed comparison chain"}
	}
	vals := make([]int64, len(n.Operands))
	for i, op := range n.Operands {
		v, err := e.eval(op)
		if err != nil {
			return 0, err
		}
		vals[i] = v
	}
	// A chain answers a VERDICT, and a verdict in this interpreter is a boxed bool so that print
	// spells True/False rather than the 1/0 the slot holds (ADR 0257). Returning the bare word made
	// `print(1 < 2 < 3)` print `1`, which is the same class of wrong answer the chain itself fixes.
	verdict := int64(1)
	for i, cmp := range n.Ops {
		link := &BinOp{Op: cmp, L: &IntLit{Value: vals[i]}, R: &IntLit{Value: vals[i+1]}, Src: n.Src}
		r, err := e.evalBin(link)
		if err != nil {
			return 0, err
		}
		if !e.truthy(r) {
			verdict = 0
		}
	}
	return e.allocBool(verdict), nil
}

func (e *Evaluator) evalBin(n *BinOp) (int64, error) {
	// `and` and `or` are the two operators that are not operators: they test one operand and hand back
	// whichever operand the test chose, unconverted. `print(2 and 3)` is `3`, not `1`, and `print("" or "d")`
	// is the text `d`. Both engines used to answer the verdict — exit 0, digits wrong, on an operator every
	// Python program uses — and the row is roadmap Gap R.147, ADR 0269.
	if n.Op == "and" || n.Op == "or" {
		// A test the source wrote is not a run-time question: the operand the test chooses is the answer,
		// and the other operand is not in the program — the same rule ADR 0261 shipped for a constant
		// ternary test, and the reason the compiled `select` never emits the arm it cannot take either.
		if chosen, decided := constantLogicArm(n); decided {
			cv, cerr := e.eval(chosen)
			if cerr != nil {
				return 0, cerr
			}
			return e.logicChosen(chosen, cv), nil
		}
		// The test decides whether the other operand is in the program at all: `x and boom()` with x bound to 0
		// never calls boom, `y or (1 // 0)` with y true never divides, and a trap inside the operand the test
		// skipped never raises. Both engines used to evaluate both operands and choose afterwards, which is what
		// a `select` compiles to and what made `if xs and xs[0] > 0:` subscript an empty list; the compiled leg
		// answers with the same three blocks this branch skips (roadmap Gap R.149, ADR 0275).
		l, err := e.eval(n.L)
		if err != nil {
			return 0, err
		}
		takesRight := e.truthy(l)
		if n.Op == "or" {
			takesRight = !takesRight
		}
		if !takesRight {
			return e.logicChosen(n.L, l), nil
		}
		r, err := e.eval(n.R)
		if err != nil {
			return 0, err
		}
		return e.logicChosen(n.R, r), nil
	}
	l, err := e.eval(n.L)
	if err != nil {
		return 0, err
	}
	r, err := e.eval(n.R)
	if err != nil {
		return 0, err
	}
	// Operator overloading (dunder dispatch): if an operand is a class instance
	// with a dunder method for this operator, call it. If the left operand is
	// not overloaded, fall back to a reflected (__r__) method on the right.
	if name := dunderForBinOp(n.Op); name != "" {
		if o := e.heap[l]; o != nil && o.kind == "instance" {
			rv, found, err := e.dunderCall(l, name, r)
			if found {
				return rv, err
			}
		}
		if o := e.heap[r]; o != nil && o.kind == "instance" {
			if rname := reflectedDunder(name); rname != "" {
				rv, found, err := e.dunderCall(r, rname, l)
				if found {
					return rv, err
				}
			}
		}
	}
	// The gate decides whether this operator may be applied to *these* values at all
	// (ADR 0215). It sits after dunder dispatch, so an operand class that defines the
	// operation still performs it, and before every arithmetic path.
	// A bool operand of an *arithmetic* operator is the number it behaves like — `True + 1` is 2 and
	// `xs[0] * 3` over a bool slot is 3 — so the box is opened before the gate that guards arithmetic
	// looks at the operand (roadmap Gap R.112, ADR 0259). It sits after dunder dispatch, so an operand
	// class that overloads the operator still performs it, and it deliberately excludes the comparison
	// operators: an ordering against a text has to reach the gate still wearing the bool, because the
	// TypeError it raises names 'bool' and the compiled backend already says so.
	switch n.Op {
	case "+", "-", "*", "/", "//", "%", "**":
		l, r = e.unboxBool(l), e.unboxBool(r)
	}
	if err := e.checkBinOp(n.Op, l, r); err != nil {
		return 0, err
	}
	switch n.Op {
	case "+":
		// str + str concatenates; list + list concatenates (both were handle arithmetic
		// before, which printed a heap id where the elements should have been).
		if lo, ok := e.heap[l]; ok && lo.kind == "str" {
			ro, ok2 := e.heap[r]
			if !ok2 || ro.kind != "str" {
				return 0, cannotConcat("str", e.operandKind(r))
			}
			return e.allocStr(lo.sval + ro.sval), nil
		}
		if lo, ok := e.heap[l]; ok && lo.kind == "list" {
			ro, ok2 := e.heap[r]
			if !ok2 || ro.kind != "list" {
				return 0, cannotConcat("list", e.operandKind(r))
			}
			h := e.allocObj("list")
			dst := e.heap[h]
			dst.elems = append(dst.elems, lo.elems...)
			dst.elems = append(dst.elems, ro.elems...)
			return h, nil
		}
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			return e.allocFloat(lf + rf), nil
		}
		if rf, ok := e.floatOf(r); ok {
			return e.allocFloat(float64(l) + rf), nil
		}
		return l + r, nil
	case "-":
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			return e.allocFloat(lf - rf), nil
		}
		if rf, ok := e.floatOf(r); ok {
			return e.allocFloat(float64(l) - rf), nil
		}
		return l - r, nil
	case "*":
		// Sequence repeat: `"ab" * 2`, `3 * [1]`. CPython allows either operand order and a
		// negative count yields empty, not an error; the gate has already established that
		// one side is a sequence and the other is an int, or that both are numeric.
		if lk := e.operandKind(l); lk == "str" || lk == "list" {
			return e.repeatSequence(lk, l, r)
		}
		if rk := e.operandKind(r); rk == "str" || rk == "list" {
			return e.repeatSequence(rk, r, l)
		}
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			return e.allocFloat(lf * rf), nil
		}
		if rf, ok := e.floatOf(r); ok {
			return e.allocFloat(float64(l) * rf), nil
		}
		return l * r, nil
	case "/", "//":
		floor := n.Op == "//"
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			if rf == 0 {
				return 0, zeroDivisionErr(floorMessage(floor))
			}
			q := lf / rf
			if floor {
				q = math.Floor(q)
			}
			return e.allocFloat(q), nil
		}
		if rf, ok := e.floatOf(r); ok {
			if rf == 0 {
				return 0, zeroDivisionErr(floorMessage(floor))
			}
			q := float64(l) / rf
			if floor {
				q = math.Floor(q)
			}
			return e.allocFloat(q), nil
		}
		if r == 0 {
			if floor {
				return 0, zeroDivisionErr("integer division or modulo by zero")
			}
			return 0, zeroDivisionErr("division by zero")
		}
		if floor {
			return floorDiv(l, r), nil
		}
		// `/` on two integers is *true* division (PEP 238): 7 / 2 is 3.5. Truncating
		// it silently turned the language's most common operator into C's, and the
		// parity harness could not see it because both backends agreed.
		return e.allocFloat(float64(l) / float64(r)), nil
	case "%":
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			if rf == 0 {
				return 0, zeroDivisionErr("float modulo")
			}
			return e.allocFloat(floorModFloat(lf, rf)), nil
		}
		if rf, ok := e.floatOf(r); ok {
			if rf == 0 {
				return 0, zeroDivisionErr("float modulo")
			}
			return e.allocFloat(floorModFloat(float64(l), rf)), nil
		}
		if r == 0 {
			return 0, zeroDivisionErr("integer modulo by zero")
		}
		return floorMod(l, r), nil
	case "==":
		if e.eqVal(l, r) {
			return 1, nil
		}
		return 0, nil
	case "is":
		if l == r {
			return 1, nil
		}
		return 0, nil
	case "is not":
		if l != r {
			return 1, nil
		}
		return 0, nil
	case "in", "not in":
		found, err := e.contains(r, l)
		if err != nil {
			return 0, err
		}
		if n.Op == "in" {
			if found {
				return 1, nil
			}
			return 0, nil
		}
		if found {
			return 0, nil
		}
		return 1, nil
	case "!=":
		// `!=` is the negation of `==`, which means it goes through eqVal too. This was
		// a raw handle comparison, so `[1] != [1]` answered False in the interpreter while
		// `[1] == [1]` also answered False — the pair disagreed with itself (ADR 0189).
		if e.eqVal(l, r) {
			return 0, nil
		}
		return 1, nil
	case "<", "<=", ">", ">=":
		// Ordered comparison, one body for the four operators. ordVal owns the question
		// "may these two values be ordered at all" and raises the TypeError when they may
		// not, so the rule lives with the ordering rather than being split between a gate
		// and each operator.
		if lf, ok := e.floatOf(l); ok && math.IsNaN(lf) {
			return 0, nil // every ordered comparison with NaN is false, `<=` included
		}
		if rf, ok := e.floatOf(r); ok && math.IsNaN(rf) {
			return 0, nil
		}
		ord, err := e.ordVal(n.Op, l, r)
		if err != nil {
			return 0, err
		}
		return boolVal(orderedBy(n.Op, ord)), nil
	case "**":
		// `0 ** -n` is the reference's ZeroDivisionError, asked BEFORE either arm runs: the float road
		// answers `0.0 ** -1` with `inf` (pow's own answer) and the int road with 0, and the reference
		// raises for the int spelling too — one sentence for both, measured (Gap R.176, ADR 0293).
		if powerNegativeExponentFromZeroFor(l, r, e) {
			return 0, exnError("ZeroDivisionError", "0.0 cannot be raised to a negative power")
		}
		if lf, ok := e.floatOf(l); ok {
			rf, rfok := e.floatOf(r)
			if !rfok {
				rf = float64(r)
			}
			if raised, ok := powerComplexResult(lf, rf); ok {
				_ = raised
				return 0, powerComplexRefusal()
			}
			return e.allocFloat(math.Pow(lf, rf)), nil
		}
		if rf, ok := e.floatOf(r); ok {
			if raised, ok := powerComplexResult(float64(l), rf); ok {
				_ = raised
				return 0, powerComplexRefusal()
			}
			// An INT base with a FLOAT exponent answers a float, whatever the exponent's own kind: the
			// old code reached here and, for a whole exponent, the caller's int path truncated it.
			return e.allocFloat(math.Pow(float64(l), rf)), nil
		}
		// `int ** int`. A NEGATIVE exponent does NOT answer an int: `2 ** -1` is the float `0.5`. The
		// line that used to sit here was `if r < 0 { return 0, nil }` under a comment saying it mirrored
		// Python's `int ** int` — it mirrored nothing, and both backends agreed on the 0, so parity could
		// not see it and only the oracle leg could (roadmap Gap R.176, ADR 0293).
		if r < 0 {
			return e.allocFloat(math.Pow(float64(l), float64(r))), nil
		}
		// The exact integer power, in a width that can SAY when it has left the machine's word. The
		// reference has arbitrary-precision integers, so `2 ** 100` is a 31-digit number and both
		// backends answering `0` was a wrong number at exit 0 — the wrap is invisible to parity because
		// both sides wrap alike. A bounded integer is this language's design; a silently WRAPPING one is
		// not, so the overflow is refused in words (roadmap Gap R.176, ADR 0293).
		acc := new(big.Int).Exp(big.NewInt(l), big.NewInt(r), nil)
		// The interpreter's int is a full 64-bit word, so it refuses only where IT leaves the word; the
		// compiled leg, whose int is an i32, refuses far sooner (Gap R.133, owner L12.12). Both refuse in
		// words rather than printing a wrapped number.
		if !acc.IsInt64() {
			return 0, exnError("OverflowError", "the exponent leaves the language's bounded integer: "+
				fmt.Sprintf("%d ** %d is %s, which does not fit a 64-bit word, and this backend declines rather than print a wrapped number (roadmap Gap R.176)", l, r, acc.String()))
		}
		return acc.Int64(), nil
	}
	return 0, &EvalError{Msg: "unsupported operator " + n.Op}
}

func (e *Evaluator) matchPattern(sub int64, p Expr) (bool, error) {
	switch t := p.(type) {
	case *ListLit:
		o, ok := e.heap[sub]
		if !ok || o.kind != "list" || len(o.elems) != len(t.Elems) {
			return false, nil
		}
		for i, pe := range t.Elems {
			if n, ok2 := pe.(*Name); ok2 && n.Value != "_" {
				e.Vars[n.Value] = o.elems[i]
				e.forgetBool(n.Value)
				continue
			}
			ev, err := e.eval(pe)
			if err != nil {
				return false, err
			}
			if ev != o.elems[i] {
				return false, nil
			}
		}
		return true, nil
	case *DictLit:
		o, ok := e.heap[sub]
		if !ok || o.kind != "dict" {
			return false, nil
		}
		for i, k := range t.Keys {
			kv, err := e.eval(k)
			if err != nil {
				return false, err
			}
			found := false
			for j, k2 := range o.elems {
				if e.dictKeyEq(kv, k2) {
					if n, ok2 := t.Vals[i].(*Name); ok2 && n.Value != "_" {
						e.Vars[n.Value] = o.dvals[j]
						e.forgetBool(n.Value)
					} else {
						vv, err := e.eval(t.Vals[i])
						if err != nil {
							return false, err
						}
						if vv != o.dvals[j] {
							return false, nil
						}
					}
					found = true
					break
				}
			}
			if !found {
				return false, nil
			}
		}
		return true, nil
	case *Name:
		// Bare-name capture pattern: bind the subject to the name and
		// always match (Python `case x:` semantics).
		if t.Value != "_" {
			e.Vars[t.Value] = sub
			e.forgetBool(t.Value)
		}
		return true, nil
	case *Call:
		// Class pattern: `case Point(x, y):` matches an instance of Point
		// (or a subclass) and binds attributes x and y to the instance's
		// values. If the callee is not a known class, fall back to the
		// default expression-equality behavior.
		fnName, ok := t.Fn.(*Name)
		if ok {
			if pid, ok2 := e.resolveClassID(fnName.Value); ok2 {
				o, ok3 := e.heap[sub]
				if !ok3 || o.kind != "instance" {
					return false, nil
				}
				// The subject must be an instance of pid or a subclass of it.
				instCID, ok4 := e.classIDs[o.class]
				if !ok4 {
					return false, nil
				}
				matched := false
				for c := instCID; c != 0; c = e.heap[c].base {
					if c == pid {
						matched = true
						break
					}
				}
				if !matched {
					return false, nil
				}
				for _, arg := range t.Args {
					nm, ok5 := arg.(*Name)
					if !ok5 {
						return false, nil
					}
					v, ok6 := o.attrs[nm.Value]
					if !ok6 {
						// missing attribute => the pattern fails to match
						return false, nil
					}
					e.Vars[nm.Value] = v
					e.forgetBool(nm.Value)
				}
				return true, nil
			}
		}
		// The other documented class-pattern form is a name holding a class value the front end could
		// not see through — a parameter, a computed binding. resolveClassID just asked that question,
		// so reaching here means the value is readable and is NOT a class. The pattern then matches
		// nothing; it does not *call* the value. The compiled backend compares the instance's class id
		// against the same value and gets the same "no", and a leg that raises where the other prints
		// is the divergence ADR 0211 is about (roadmap Gap B, ADR 0235).
		if fn, ok := t.Fn.(*Name); ok && !e.isCallableName(fn.Value) && e.holdsValue(fn.Value) {
			return false, nil
		}
		// Not a class pattern: treat as expression-equality (the previous
		// default behavior) so `case someCall():` still works.
		pv, err := e.eval(t)
		if err != nil {
			return false, err
		}
		return pv == sub, nil
	default:
		pv, err := e.eval(p)
		if err != nil {
			return false, err
		}
		return pv == sub, nil
	}
}

// evalBody runs a nested statement list (a loop body, a branch, a function
// body). The caller has not claimed to have rooted anything, so the collector
// keeps the whole current statement alive.
func (e *Evaluator) evalBody(stmts []Stmt) (int64, error) {
	return e.runStatements(stmts, false)
}

// evalBodySafe runs a nested statement list whose owning construct has rooted
// every handle it keeps live across the body (L7.2). When `safe` is false this
// behaves exactly like evalBody.
func (e *Evaluator) evalBodySafe(stmts []Stmt, safe bool) (int64, error) {
	return e.runStatements(stmts, safe && e.stmtSafe)
}

// swapScope installs a fresh local scope for a call and roots it — and the scope
// it replaces — until the returned restore function runs. Rooting the saved
// scope matters: while a call or an import runs, the caller's bindings are not
// in e.Vars, and an unrooted caller environment would let a collection sweep
// variables that are perfectly live.
func (e *Evaluator) swapScope(scope map[string]int64) func() {
	saved := e.Vars
	savedBools := e.boolVars
	e.Vars = scope
	e.boolVars = map[string]bool{}
	e.pushFrame(saved)
	e.pushFrame(scope)
	return func() {
		e.Vars = saved
		e.boolVars = savedBools
		e.popFrame()
		e.popFrame()
	}
}

func (e *Evaluator) rangeBounds(iter Expr) (int64, int64, error) {
	if c, ok := iter.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" && (len(c.Args) == 2 || len(c.Args) == 3) {
			start, err := e.eval(c.Args[0])
			if err != nil {
				return 0, 0, err
			}
			stop, err := e.eval(c.Args[1])
			if err != nil {
				return 0, 0, err
			}
			return start, stop, nil
		}
	}
	stop, err := e.eval(iter)
	if err != nil {
		return 0, 0, err
	}
	return 0, stop, nil
}

// rangeStep returns the iteration step for a `range(start, stop[, step])`
// iterable: 1 when no step is given, or the constant third argument.
func (e *Evaluator) rangeStep(iter Expr) (int64, error) {
	if c, ok := iter.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" && len(c.Args) == 3 {
			step, err := e.eval(c.Args[2])
			if err != nil {
				return 0, err
			}
			if step == 0 {
				return 0, &EvalError{Msg: "range step cannot be zero"}
			}
			return step, nil
		}
	}
	return 1, nil
}

// callMethod invokes a method body with self bound as a local.
// callStrMethod dispatches builtin string methods: s.upper(), s.lower(),
// s.strip(), s.split(sep?). recv is the boxed string handle.

// capitalize returns s with the first rune uppercased and the rest lowercased.
func capitalize(s string) string {
	if s == "" {
		return ""
	}
	r := []rune(s)
	return string(unicode.ToUpper(r[0])) + strings.ToLower(string(r[1:]))
}

// title returns s with the first rune of each whitespace-separated word uppercased.
func title(s string) string {
	prev := ' '
	return strings.Map(func(r rune) rune {
		if prev == ' ' {
			prev = r
			return unicode.ToUpper(r)
		}
		prev = r
		return unicode.ToLower(r)
	}, s)
}

// swapcase returns s with each rune case swapped.
func swapcase(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return unicode.ToUpper(r)
	}, s)
}

// evalIterable evaluates an expression to an ordered list of element handles,
// used by `yield from`. It supports list literals, generator calls (which return
// list handles), and range(...) calls.
func (e *Evaluator) evalIterable(x Expr) ([]int64, error) {
	if c, ok := x.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 && n.Value == "range" {
			start, stop, err := e.rangeBounds(x)
			if err != nil {
				return nil, err
			}
			var elems []int64
			for i := start; i < stop; i++ {
				elems = append(elems, i)
			}
			return elems, nil
		}
	}
	h, err := e.eval(x)
	if err != nil {
		return nil, err
	}
	if o, ok := e.heap[h]; ok && o.kind == "list" {
		return o.elems, nil
	}
	return nil, &EvalError{Msg: "yield from target is not iterable"}
}
func (e *Evaluator) callStrMethod(recv int64, name string, args []Expr) (int64, error) {
	o := e.heap[recv]
	s := o.sval
	switch name {
	case "upper":
		if len(args) != 0 {
			return 0, &EvalError{Msg: "upper() takes no arguments"}
		}
		return e.allocStr(strings.ToUpper(s)), nil
	case "capitalize":
		// s.capitalize() -> first rune upper, rest lower.
		return e.allocStr(capitalize(s)), nil
	case "title":
		// s.title() -> capitalize the first rune of each whitespace-separated word.
		return e.allocStr(title(s)), nil
	case "swapcase":
		// s.swapcase() -> swap each rune case.
		return e.allocStr(swapcase(s)), nil
	case "lower":
		if len(args) != 0 {
			return 0, &EvalError{Msg: "lower() takes no arguments"}
		}
		return e.allocStr(strings.ToLower(s)), nil
	case "strip":
		// s.strip([chars]) -> trim whitespace, or the given chars when provided.
		if len(args) > 1 {
			return 0, &EvalError{Msg: "strip() takes at most 1 argument"}
		}
		if len(args) == 1 {
			cv, err := e.eval(args[0])
			if err != nil {
				return 0, err
			}
			co, ok := e.heap[cv]
			if !ok || co.kind != "str" {
				return 0, &EvalError{Msg: "strip() argument must be a string"}
			}
			return e.allocStr(strings.Trim(s, co.sval)), nil
		}
		return e.allocStr(strings.TrimSpace(s)), nil
	case "lstrip":
		// s.lstrip([chars]) -> trim whitespace, or the given chars when provided.
		if len(args) > 1 {
			return 0, &EvalError{Msg: "lstrip() takes at most 1 argument"}
		}
		if len(args) == 1 {
			cv, err := e.eval(args[0])
			if err != nil {
				return 0, err
			}
			co, ok := e.heap[cv]
			if !ok || co.kind != "str" {
				return 0, &EvalError{Msg: "lstrip() argument must be a string"}
			}
			return e.allocStr(strings.TrimLeft(s, co.sval)), nil
		}
		return e.allocStr(strings.TrimLeftFunc(s, unicode.IsSpace)), nil
	case "rstrip":
		// s.rstrip([chars]) -> trim whitespace, or the given chars when provided.
		if len(args) > 1 {
			return 0, &EvalError{Msg: "rstrip() takes at most 1 argument"}
		}
		if len(args) == 1 {
			cv, err := e.eval(args[0])
			if err != nil {
				return 0, err
			}
			co, ok := e.heap[cv]
			if !ok || co.kind != "str" {
				return 0, &EvalError{Msg: "rstrip() argument must be a string"}
			}
			return e.allocStr(strings.TrimRight(s, co.sval)), nil
		}
		return e.allocStr(strings.TrimRightFunc(s, unicode.IsSpace)), nil
	case "split":
		// s.split([sep[, maxsplit]]) -> list split on sep, at most maxsplit separators.
		sep := " "
		maxsplit := -1
		if len(args) >= 1 && len(args) <= 2 {
			sepv, err := e.eval(args[0])
			if err != nil {
				return 0, err
			}
			sepo, ok := e.heap[sepv]
			if !ok || sepo.kind != "str" {
				return 0, &EvalError{Msg: "split() separator must be a string"}
			}
			sep = sepo.sval
			if len(args) == 2 {
				ms, err := e.eval(args[1])
				if err != nil {
					return 0, err
				}
				maxsplit = int(ms)
			}
		}
		var parts []string
		if maxsplit >= 0 {
			parts = strings.SplitN(s, sep, maxsplit+1)
		} else {
			parts = strings.Split(s, sep)
		}
		listID := e.allocObj("list")
		lo := e.heap[listID]
		for _, part := range parts {
			lo.elems = append(lo.elems, e.allocStr(part))
		}
		return listID, nil
	case "rsplit":
		// s.rsplit([sep[, maxsplit]]) -> list split on sep from the right.
		sep := " "
		maxsplit := -1
		if len(args) >= 1 && len(args) <= 2 {
			sepv, err := e.eval(args[0])
			if err != nil {
				return 0, err
			}
			sepo, ok := e.heap[sepv]
			if !ok || sepo.kind != "str" {
				return 0, &EvalError{Msg: "rsplit() separator must be a string"}
			}
			sep = sepo.sval
			if len(args) == 2 {
				ms, err := e.eval(args[1])
				if err != nil {
					return 0, err
				}
				maxsplit = int(ms)
			}
		}
		parts := strings.Split(s, sep)
		var out []string
		if maxsplit >= 0 && maxsplit < len(parts)-1 {
			keep := len(parts) - maxsplit
			out = append(out, strings.Join(parts[:keep], sep))
			out = append(out, parts[keep:]...)
		} else {
			out = parts
		}
		listID := e.allocObj("list")
		lo := e.heap[listID]
		for _, part := range out {
			lo.elems = append(lo.elems, e.allocStr(part))
		}
		return listID, nil
	case "removeprefix":
		// s.removeprefix(prefix) -> s without the prefix if it is present.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "removeprefix() takes exactly 1 argument"}
		}
		pv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		po, ok := e.heap[pv]
		if !ok || po.kind != "str" {
			return 0, &EvalError{Msg: "removeprefix() argument must be a string"}
		}
		return e.allocStr(strings.TrimPrefix(s, po.sval)), nil
	case "removesuffix":
		// s.removesuffix(suffix) -> s without the suffix if it is present.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "removesuffix() takes exactly 1 argument"}
		}
		pv2, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		po2, ok := e.heap[pv2]
		if !ok || po2.kind != "str" {
			return 0, &EvalError{Msg: "removesuffix() argument must be a string"}
		}
		return e.allocStr(strings.TrimSuffix(s, po2.sval)), nil
	case "expandtabs":
		// s.expandtabs(tabsize) -> replace each tab with spaces to the next tab stop.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "expandtabs() takes exactly 1 argument"}
		}
		tv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		tabsize := int(tv)
		if tabsize < 1 {
			return 0, &EvalError{Msg: "expandtabs() tabsize must be positive"}
		}
		col := 0
		var b strings.Builder
		for _, r := range s {
			if r == '\t' {
				spaces := tabsize - (col % tabsize)
				b.WriteString(strings.Repeat(" ", spaces))
				col += spaces
			} else {
				b.WriteRune(r)
				col++
			}
		}
		return e.allocStr(b.String()), nil
	case "partition":
		// s.partition(sep) -> list [head, sep, tail] at the first occurrence of sep.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "partition() takes exactly 1 argument"}
		}
		sepv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		sepo, ok := e.heap[sepv]
		if !ok || sepo.kind != "str" {
			return 0, &EvalError{Msg: "partition() argument must be a string"}
		}
		sep := sepo.sval
		listID := e.allocObj("list")
		lo := e.heap[listID]
		idx := strings.Index(s, sep)
		if idx < 0 {
			lo.elems = append(lo.elems, e.allocStr(s), e.allocStr(""), e.allocStr(""))
		} else {
			head := s[:idx]
			tail := s[idx+len(sep):]
			lo.elems = append(lo.elems, e.allocStr(head), e.allocStr(sep), e.allocStr(tail))
		}
		return listID, nil
	case "replace":
		if len(args) != 2 {
			return 0, &EvalError{Msg: "replace() takes exactly 2 arguments"}
		}
		oldv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		newv, err := e.eval(args[1])
		if err != nil {
			return 0, err
		}
		oldo, ok := e.heap[oldv]
		if !ok || oldo.kind != "str" {
			return 0, &EvalError{Msg: "replace() old must be a string"}
		}
		novo, ok := e.heap[newv]
		if !ok || novo.kind != "str" {
			return 0, &EvalError{Msg: "replace() new must be a string"}
		}
		return e.allocStr(strings.ReplaceAll(s, oldo.sval, novo.sval)), nil
	case "find":
		// s.find(sub) -> index of first occurrence of sub, or -1 if absent.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "find() takes exactly 1 argument"}
		}
		subv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		subo, ok := e.heap[subv]
		if !ok || subo.kind != "str" {
			return 0, &EvalError{Msg: "find() argument must be a string"}
		}
		return int64(strings.Index(s, subo.sval)), nil
	case "index":
		// s.index(sub) -> index of first occurrence, raising when absent.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "index() takes exactly 1 argument"}
		}
		subv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		subo, ok := e.heap[subv]
		if !ok || subo.kind != "str" {
			return 0, &EvalError{Msg: "index() argument must be a string"}
		}
		idx := strings.Index(s, subo.sval)
		if idx < 0 {
			return 0, &EvalError{Msg: "substring not found"}
		}
		return int64(idx), nil
	case "rfind":
		// s.rfind(sub) -> index of last occurrence of sub, or -1 if absent.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "rfind() takes exactly 1 argument"}
		}
		subv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		subo, ok := e.heap[subv]
		if !ok || subo.kind != "str" {
			return 0, &EvalError{Msg: "rfind() argument must be a string"}
		}
		return int64(strings.LastIndex(s, subo.sval)), nil
	case "count":
		// s.count(sub[, start[, end]]) -> occurrences of sub within s[start:end].
		if len(args) < 1 || len(args) > 3 {
			return 0, &EvalError{Msg: "count() takes 1 to 3 arguments"}
		}
		subv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		subo, ok := e.heap[subv]
		if !ok || subo.kind != "str" {
			return 0, &EvalError{Msg: "count() argument must be a string"}
		}
		lo, hi := 0, len(s)
		if len(args) >= 2 {
			start, err := e.eval(args[1])
			if err != nil {
				return 0, err
			}
			lo = int(start)
		}
		if len(args) == 3 {
			endv, err := e.eval(args[2])
			if err != nil {
				return 0, err
			}
			hi = int(endv)
		}
		if lo < 0 {
			lo = 0
		}
		if hi > len(s) {
			hi = len(s)
		}
		if lo > hi {
			lo = hi
		}
		return int64(strings.Count(s[lo:hi], subo.sval)), nil
	case "isdigit":
		// s.isdigit() -> 1 if all runes are digits, else 0.
		for _, r := range s {
			if !unicode.IsDigit(r) {
				return 0, nil
			}
		}
		if s == "" {
			return 0, nil
		}
		return 1, nil
	case "isalpha":
		// s.isalpha() -> 1 if all runes are alphabetic, else 0.
		for _, r := range s {
			if !unicode.IsLetter(r) {
				return 0, nil
			}
		}
		if s == "" {
			return 0, nil
		}
		return 1, nil
	case "isalnum":
		// s.isalnum() -> 1 if every rune is alphanumeric and s is non-empty.
		if s == "" {
			return 0, nil
		}
		for _, r := range s {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return 0, nil
			}
		}
		return 1, nil
	case "isspace":
		// s.isspace() -> 1 if every rune is whitespace and s is non-empty.
		if s == "" {
			return 0, nil
		}
		for _, r := range s {
			if !unicode.IsSpace(r) {
				return 0, nil
			}
		}
		return 1, nil
	case "zfill":
		// s.zfill(width) -> pad with leading zeros to width.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "zfill() takes exactly 1 argument"}
		}
		wv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		width := int(wv)
		if width < 0 {
			return 0, &EvalError{Msg: "zfill() width must be non-negative"}
		}
		if len(s) >= width {
			return e.allocStr(s), nil
		}
		pad := strings.Repeat("0", width-len(s))
		return e.allocStr(pad + s), nil
	case "ljust":
		// s.ljust(width) -> pad with spaces on the right to width.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "ljust() takes exactly 1 argument"}
		}
		wv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		width := int(wv)
		if width < 0 {
			return 0, &EvalError{Msg: "ljust() width must be non-negative"}
		}
		if len(s) >= width {
			return e.allocStr(s), nil
		}
		pad := strings.Repeat(" ", width-len(s))
		return e.allocStr(s + pad), nil
	case "rjust":
		// s.rjust(width) -> pad with spaces on the left to width.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "rjust() takes exactly 1 argument"}
		}
		wv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		width := int(wv)
		if width < 0 {
			return 0, &EvalError{Msg: "rjust() width must be non-negative"}
		}
		if len(s) >= width {
			return e.allocStr(s), nil
		}
		pad := strings.Repeat(" ", width-len(s))
		return e.allocStr(pad + s), nil
	case "islower":
		// s.islower() -> 1 if there is a cased rune and all cased runes are lowercase.
		hasCased := false
		allLower := true
		for _, r := range s {
			if unicode.IsLower(r) {
				hasCased = true
			} else if unicode.IsUpper(r) {
				hasCased = true
				allLower = false
			}
		}
		if hasCased && allLower {
			return 1, nil
		}
		return 0, nil
	case "isupper":
		// s.isupper() -> 1 if there is a cased rune and all cased runes are uppercase.
		hasCased := false
		allUpper := true
		for _, r := range s {
			if unicode.IsUpper(r) {
				hasCased = true
			} else if unicode.IsLower(r) {
				hasCased = true
				allUpper = false
			}
		}
		if hasCased && allUpper {
			return 1, nil
		}
		return 0, nil
	case "join":
		// s.join(list) -> join list element strings with separator s.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "join() takes exactly 1 argument"}
		}
		lv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		lo, ok := e.heap[lv]
		if !ok || lo.kind != "list" {
			return 0, &EvalError{Msg: "join() argument must be a list"}
		}
		parts := []string{}
		for _, el := range lo.elems {
			so, ok := e.heap[el]
			if !ok || so.kind != "str" {
				return 0, &EvalError{Msg: "join() list elements must be strings"}
			}
			parts = append(parts, so.sval)
		}
		return e.allocStr(strings.Join(parts, s)), nil
	case "startswith", "endswith":
		// s.startswith(sub) / s.endswith(sub) -> 1 or 0.
		if len(args) != 1 {
			return 0, &EvalError{Msg: name + "() takes exactly 1 argument"}
		}
		subv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		subo, ok := e.heap[subv]
		if !ok || subo.kind != "str" {
			return 0, &EvalError{Msg: name + "() argument must be a string"}
		}
		res := false
		if name == "startswith" {
			res = strings.HasPrefix(s, subo.sval)
		} else {
			res = strings.HasSuffix(s, subo.sval)
		}
		if res {
			return 1, nil
		}
		return 0, nil
	}
	return 0, &EvalError{Msg: "no such string method " + name}
}

// callListMethod dispatches builtin list methods: xs.append(x).
// append mutates the list in place and returns the (updated) list handle,
// so the REPL can show the resulting list.
func (e *Evaluator) callListMethod(recv int64, name string, args []Expr) (int64, error) {
	o := e.heap[recv]
	switch name {
	case "append":
		if len(args) != 1 {
			return 0, &EvalError{Msg: "append() takes exactly 1 argument"}
		}
		v, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		// The argument's own expression decides whether the slot says bool: xs.append(True) and
		// xs.append(1 == 1) are both verdicts, and xs.append(n) is not (Gap R.111/R.112, ADR 0259).
		o.elems = append(o.elems, e.slotVal(args[0], v))
		return recv, nil
	case "pop":
		// l.pop() removes and returns the last element; l.pop(i) removes and returns
		// element i (negative counts from the end), like Python. This is how a program
		// drains a container in a `while xs:` loop — without it the natural idiom
		// simply did not exist (roadmap Gap K.3).
		if len(args) > 1 {
			return 0, exnError("TypeError", "pop() takes at most 1 argument")
		}
		o = e.heap[recv]
		idx := int64(len(o.elems) - 1)
		if len(args) == 1 {
			v, err := e.eval(args[0])
			if err != nil {
				return 0, err
			}
			idx = v
			if idx < 0 {
				idx += int64(len(o.elems))
			}
		}
		if len(o.elems) == 0 {
			return 0, exnError("IndexError", "pop from empty list")
		}
		if idx < 0 || idx >= int64(len(o.elems)) {
			return 0, exnError("IndexError", "pop index out of range")
		}
		v := o.elems[idx]
		o.elems = append(o.elems[:idx], o.elems[idx+1:]...)
		return v, nil
	case "count":
		// l.count(value) -> number of occurrences of value in l.
		if len(args) != 1 {
			return 0, &EvalError{Msg: "count() takes exactly 1 argument"}
		}
		vv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		cnt := int64(0)
		for _, el := range o.elems {
			if e.dictKeyEq(el, vv) {
				cnt++
			}
		}
		return cnt, nil
	case "reverse":
		// l.reverse() reverses in place and returns None (roadmap L11.7, ADR 0191).
		if len(args) != 0 {
			return 0, exnError("TypeError", "reverse() takes no arguments")
		}
		for i, j := 0, len(o.elems)-1; i < j; i, j = i+1, j-1 {
			o.elems[i], o.elems[j] = o.elems[j], o.elems[i]
		}
		return e.noneVal, nil
	case "sort":
		// l.sort() sorts in place and returns None. Sorting is a *language* surface
		// question, not a stdlib afterthought: until now neither backend had it, and the
		// AOT path diagnosed `xs.sort()` as a string method (roadmap L11.7, ADR 0191).
		if len(args) != 0 {
			return 0, exnError("TypeError", "sort() takes no arguments in this build (key= and reverse= are not implemented)")
		}
		if err := e.sortElems(o.elems); err != nil {
			return 0, err
		}
		return e.noneVal, nil
	}
	return 0, &EvalError{Msg: "no such list method " + name}
}

// compareElems orders two list elements the way the reference implementation's `<` does.
func (e *Evaluator) compareElems(a, b int64) (int, error) {
	return e.compareOrder(a, b, "<")
}

// compareOrder orders two values the way the reference implementation's `<` or `>` does for
// the kinds this interpreter can compare: numbers (int or float) compare numerically, texts
// compare by their content. Comparing across those kinds is a TypeError, and saying so is
// better than inventing an order — a fold that guesses is worse than one that refuses.
//
// The operator is part of the question because the reference implementation names the operator
// that failed in its TypeError. min's fold asks `<` of the candidate against the incumbent;
// max's asks `>`; sharing one comparator while always printing `<` told the reader that min had
// failed when the program had called max (roadmap Gap R.104).
func (e *Evaluator) compareOrder(a, b int64, op string) (int, error) {
	// A bool orders as its number: min([True, 0]), sorted([True, False]) and `True < 1` are
	// numeric questions, and the box exists only for the rendering (Gap R.112, ADR 0259).
	// The kinds the refusal names are read first, from what the program actually wrote: CPython
	// says 'bool' for `xs[0] > "a"` of a bool slot, and the compiled backend already does — the
	// two engines may disagree about an answer, not about which word to print in an error.
	aKind, bKind := e.operandKind(a), e.operandKind(b)
	a, b = e.unboxBool(a), e.unboxBool(b)
	af, aIsFloat := e.floatOf(a)
	bf, bIsFloat := e.floatOf(b)
	if aIsFloat || bIsFloat {
		if !aIsFloat && e.isHandle(a) {
			return 0, unsupportedCompare(op, aKind, bKind)
		}
		if !bIsFloat && e.isHandle(b) {
			return 0, unsupportedCompare(op, aKind, bKind)
		}
		if !aIsFloat {
			af = float64(a)
		}
		if !bIsFloat {
			bf = float64(b)
		}
		switch {
		case af < bf:
			return -1, nil
		case af > bf:
			return 1, nil
		}
		return 0, nil
	}
	ao, aIsObj := e.heap[a]
	bo, bIsObj := e.heap[b]
	aIsObj = aIsObj && e.isHandle(a)
	bIsObj = bIsObj && e.isHandle(b)
	if aIsObj && ao.kind == "str" && bIsObj && bo.kind == "str" {
		return strings.Compare(ao.sval, bo.sval), nil
	}
	if aIsObj || bIsObj {
		return 0, unsupportedCompare(op, aKind, bKind)
	}
	switch {
	case a < b:
		return -1, nil
	case a > b:
		return 1, nil
	}
	return 0, nil
}

// sortElems is a stable insertion sort. Stability is not decoration: sorted(key=) — the reason
// L11.7 also owns first-class functions — is built on it, and an unstable sort makes the future
// feature wrong in a way that is hard to see. n^2 is fine for the sizes the runtime holds (the
// heap element array is 256 deep), and it keeps the compiled version a few dozen instructions.
func (e *Evaluator) sortElems(elems []int64) error {
	for i := 1; i < len(elems); i++ {
		j := i
		for j > 0 {
			c, err := e.compareElems(elems[j-1], elems[j])
			if err != nil {
				return err
			}
			if c <= 0 {
				break
			}
			elems[j-1], elems[j] = elems[j], elems[j-1]
			j--
		}
	}
	return nil
}

// callSetMethod dispatches builtin set methods. `add` is what makes `set()` usable at
// all — without it the empty-set constructor produced a value nothing could grow
// (roadmap Gap K.3).
func (e *Evaluator) callSetMethod(recv int64, name string, args []Expr) (int64, error) {
	o := e.heap[recv]
	switch name {
	case "add":
		if len(args) != 1 {
			return 0, exnError("TypeError", "add() takes exactly 1 argument")
		}
		v, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		v = e.slotVal(args[0], v)
		for _, x := range o.elems {
			// Value equality, so s.add(True) on a set holding 1 is a no-op like Python's, and the
			// member already there keeps the spelling it arrived with (Gap R.112, ADR 0259).
			if e.eqVal(x, v) {
				return recv, nil // sets are a set: adding twice is a no-op
			}
		}
		o.elems = append(o.elems, v)
		return recv, nil
	case "discard", "remove":
		if len(args) != 1 {
			return 0, exnError("TypeError", name+"() takes exactly 1 argument")
		}
		v, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		for i, x := range o.elems {
			if x == v {
				o.elems = append(o.elems[:i], o.elems[i+1:]...)
				return recv, nil
			}
		}
		if name == "remove" {
			return 0, exnError("KeyError", "remove(): element not in set")
		}
		return recv, nil // discard is silent about absence, like Python
	case "clear":
		o.elems = nil
		return recv, nil
	}
	return 0, exnError("TypeError", "no such set method "+name)
}

// callDictMethod dispatches builtin dict methods: d.keys() and d.values()
// return boxed lists of the keys/values in insertion order.
// dictKeyEq reports whether two dict keys compare equal by content.
func (e *Evaluator) dictKeyEq(k, idx int64) bool {
	// {1: "a"} answers d[True] and {True: "a"} answers d[1], because a bool key is a number key
	// to Python (roadmap Gap R.112, ADR 0259).
	k, idx = e.unboxBool(k), e.unboxBool(idx)
	ko, kObj := e.heap[k]
	io, iObj := e.heap[idx]
	if kObj && ko.kind == "str" {
		return iObj && io.kind == "str" && ko.sval == io.sval
	}
	// A float key is a boxed value, so comparing the two handles says nothing about the numbers
	// they hold: {1.5: "x"} could be built and printed but never read back, and d[1.5] raised
	// KeyError while `1.5 in d` was true. Ask the numbers, the way the compiled runtime's
	// rt_payload_eq does — which also makes {1: "x"} answer d[1.0], as Python's does
	// (roadmap L11.1, ADR 0233).
	if kObj && ko.kind == "float" {
		if iObj {
			return io.kind == "float" && ko.fval == io.fval
		}
		return float64(idx) == ko.fval
	}
	if iObj && io.kind == "float" {
		if kObj {
			return false
		}
		return float64(k) == io.fval
	}
	return k == idx
}

// dictPut writes one entry into a dict that is being built. A dict is a key → value mapping, so a
// key the dict already holds keeps its place and takes the new value; only a key that is not there
// yet extends the entry list — and the key that stays is the first one written, as CPython's does
// ({1: 'a', True: 'b'} prints {1: 'b'}, not {True: 'b'}). The comparison is dictKeyEq's, which is
// what makes 1, True and 1.0 one key (ADR 0259). Every builder that grows a dict walks this door:
// the literal, the comprehension, item assignment and dict() — they used to append, which left
// `{"a": 1, "a": 2}` holding two entries that both printed and both counted while the compiled
// backend, whose fold deduplicates keys, answered CPython's line (roadmap Gaps R.118 and R.120,
// ADR 0260).
func (e *Evaluator) dictPut(o *obj, key, val int64) {
	for i, k := range o.elems {
		if e.dictKeyEq(k, key) {
			o.dvals[i] = val
			return
		}
	}
	o.elems = append(o.elems, key)
	o.dvals = append(o.dvals, val)
}

// lessVal reports whether boxed value a is less than b (ints by value, strings by content).
func (e *Evaluator) lessVal(a, b int64) bool {
	// False sorts as 0 and True as 1 — the order CPython's sort gives a list of bools — and the
	// box is not a number, so it is asked for its payload first (Gap R.112, ADR 0259).
	a, b = e.unboxBool(a), e.unboxBool(b)
	ao, ok := e.heap[a]
	if ok && ao.kind == "str" {
		bo, ok2 := e.heap[b]
		return ok2 && bo.kind == "str" && ao.sval < bo.sval
	}
	return a < b
}

func (e *Evaluator) callDictMethod(recv int64, name string, args []Expr) (int64, error) {
	o := e.heap[recv]
	switch name {
	case "items":
		if len(args) != 0 {
			return 0, &EvalError{Msg: "items() takes no arguments"}
		}
		listID := e.allocObj("list")
		lo := e.heap[listID]
		for i, k := range o.elems {
			pairID := e.allocObj("list")
			p := e.heap[pairID]
			p.elems = append(p.elems, k, o.dvals[i])
			lo.elems = append(lo.elems, pairID)
		}
		return listID, nil
	case "keys":
		if len(args) != 0 {
			return 0, &EvalError{Msg: "keys() takes no arguments"}
		}
		listID := e.allocObj("list")
		lo := e.heap[listID]
		lo.elems = append(lo.elems, o.elems...)
		return listID, nil
	case "values":
		if len(args) != 0 {
			return 0, &EvalError{Msg: "values() takes no arguments"}
		}
		listID := e.allocObj("list")
		lo := e.heap[listID]
		lo.elems = append(lo.elems, o.dvals...)
		return listID, nil
	case "get":
		// d.get(key[, default]) -> value for key, or default if absent.
		if len(args) != 1 && len(args) != 2 {
			return 0, &EvalError{Msg: "get() takes 1 or 2 arguments"}
		}
		kv, err := e.eval(args[0])
		if err != nil {
			return 0, err
		}
		for i, k := range o.elems {
			if e.dictKeyEq(k, kv) {
				return o.dvals[i], nil
			}
		}
		if len(args) == 2 {
			dv, err := e.eval(args[1])
			if err != nil {
				return 0, err
			}
			return dv, nil
		}
		// A missing key with no default hands back NONE, not the number 0. `return 0` here was the
		// bare-word zero, so `print(d.get("z"))` printed `0` where the reference prints `None` -- the
		// same class as a void flowing out of a function (Gap R.171) and the reason voids need a
		// representation rather than a word (roadmap L11.1, Gap R.174, ADR 0291).
		return e.noneVal, nil
	}
	return 0, &EvalError{Msg: "no such dict method " + name}
}

// callDunder calls a dunder (__enter__/__exit__) method on an instance handle.
func (e *Evaluator) callDunder(self int64, name string, args []int64) (int64, error) {
	o := e.heap[self]
	if o == nil {
		return 0, &EvalError{Msg: "with: context manager is not an object"}
	}
	mID, ok := e.resolveMethod(e.classIDs[o.class], name)
	if !ok {
		return 0, &EvalError{Msg: "with: missing " + name + " method"}
	}
	return e.callMethod(e.heap[mID], self, args)
}
func (e *Evaluator) callMethod(mo *obj, self int64, args []int64) (int64, error) {
	caller := e.fnName
	callSite := e.cur
	savedFn := e.fnName
	e.fnName = mo.mname
	defer func() { e.fnName = savedFn }()
	scope := map[string]int64{}
	scope["self"] = self
	// params[0] is the receiver `self`; bind the remaining params from args
	params := mo.fn.Params
	if len(params) > 0 {
		params = params[1:]
	}
	for i, p := range params {
		if i < len(args) {
			scope[p.Name] = args[i]
		}
	}
	restoreScope := e.swapScope(scope)
	defer restoreScope()
	// set the current method context so super() can resolve the base class
	// and bind the current instance.
	prevClass, prevSelf := e.curClass, e.curSelf
	e.curClass = mo.class
	e.curSelf = self
	defer func() { e.curClass, e.curSelf = prevClass, prevSelf }()

	rv, err := e.runFuncBody(mo.fn, false)
	if rs, ok := err.(*returnSignal); ok {
		return rs.val, nil
	}
	return rv, e.recordCall(err, mo.mname, caller, callSite)
}

// resolveMethod finds a method named `name` on the class with id `classID`,
// walking up the base-class chain (inheritance). It returns the method obj id.
func (e *Evaluator) resolveMethod(classID int64, name string) (int64, bool) {
	for c := e.heap[classID]; c != nil && c.kind == "class"; c = e.heap[c.base] {
		if mID, ok := c.attrs[name]; ok {
			return mID, true
		}
		if c.base == 0 {
			break
		}
	}
	return 0, false
}

// classIDFor returns the class heap-id whose obj value equals objV.
func (e *Evaluator) classIDFor(objV int64) int64 {
	for _, v := range e.classIDs {
		if v == objV {
			return v
		}
	}
	return 0
}

// importModule loads <mod>.gy, evaluates it in a fresh top-level scope, and
// binds `mod` to a module obj whose attrs are the module's top-level names.
func (e *Evaluator) importModule(mod string) error {
	path := ResolveImportPath(mod)
	if path == "" {
		return &EvalError{Msg: "cannot import module " + mod}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &EvalError{Msg: "cannot import module " + mod}
	}
	prog, err := parseProgram(string(data))
	if err != nil {
		return err
	}
	if diags := Analyze(prog); anyErr(diags) {
		return &EvalError{Msg: "module " + mod + " has analysis errors"}
	}
	// Evaluate in a fresh scope so the module's top-level names don't leak;
	// obj ids come from e's shared heap, so the module obj stays valid.
	savedVars := e.Vars
	savedFuncs := e.funcs
	savedCurModule := e.curModule
	e.Vars = map[string]int64{}
	e.funcs = map[string]*FuncDef{}
	e.externs = map[string]*ExternDecl{}
	// While the module body runs, ITS scope is the global scope for everything it defines:
	// a function in an imported module resolves its bare names there, not in the importer.
	e.curModule = e.Vars
	e.anchorScope(e.Vars)
	// While the module body runs, neither the importing scope nor the module's own
	// scope is reachable through e.Vars, so both are roots explicitly (L7.2) —
	// otherwise a collection during an import would sweep live module globals.
	e.pushFrame(savedVars)
	e.pushFrame(e.Vars)
	_, err = e.runStatements(prog.Stmts, false)
	e.popFrame()
	e.popFrame()
	if err != nil {
		e.Vars, e.funcs, e.curModule = savedVars, savedFuncs, savedCurModule
		return err
	}
	modID := e.allocObj("module")
	m := e.heap[modID]
	m.attrs = map[string]int64{}
	for name, v := range e.Vars {
		m.attrs[name] = v
	}
	// module functions live in e.funcs (not Vars); wrap each in a closure obj
	// so `mod.fn(args)` resolves to a callable.
	for name, fd := range e.funcs {
		cID := e.allocObj("closure")
		c := e.heap[cID]
		c.fn = fd
		c.env = map[string]int64{}
		m.attrs[name] = cID
	}
	e.Vars, e.funcs, e.curModule = savedVars, savedFuncs, savedCurModule
	e.Vars[mod] = modID
	return nil
}

// maxHeapID returns the highest object id currently allocated, used to
// advance the nursery boundary after a young GC (promoting survivors to old).
func maxHeapID(heap map[int64]*obj) int64 {
	maxID := int64(0)
	for id := range heap {
		if id > maxID {
			maxID = id
		}
	}
	return maxID
}

func (e *Evaluator) runCoro(cid int64) (int64, error) {
	co := e.heap[cid]
	fd := co.fn
	argVals := co.args
	prevRet := e.curRet
	e.curRet = fd.ReturnAnno
	restoreScope := e.swapScope(map[string]int64{})
	for i, p := range fd.Params {
		e.Vars[p.Name] = argVals[i]
		e.forgetBool(p.Name)
	}
	rv, err := e.runFuncBody(fd, false)
	restoreScope()
	e.curRet = prevRet
	if err != nil {
		if rs, ok := err.(*returnSignal); ok {
			return rs.val, nil
		}
		return 0, err
	}
	// Fall-off-the-end yields None (see callFunc): a procedure that assigns
	// variables must not leak its last statement value to the caller.
	_ = rv
	return e.noneVal, nil
}

func (e *Evaluator) evalCall(n *Call) (int64, error) {
	// Snapshot before the arguments are evaluated: each argument goes through
	// eval, which clears the statement-root flag (L7.2 safe points).
	stmtRootCall := e.callIsStmtRoot
	// method call: obj.method(args) — Fn is an Attr resolving to a method
	// inline lambda callee: `(lambda ...)(args)` evaluates to a closure.
	if _, ok := n.Fn.(*Lambda); ok {
		h, err := e.eval(n.Fn)
		if err != nil {
			return 0, err
		}
		mo, ok := e.heap[h]
		if !ok || mo.kind != "closure" {
			return 0, &EvalError{Msg: "lambda callee is not a closure"}
		}
		return e.callClosure(mo, n)
	}
	if attr, ok := n.Fn.(*Attr); ok {
		// string methods: s.upper() / lower() / strip() / split(sep?)
		recv, err := e.eval(attr.Obj)
		if err != nil {
			return 0, err
		}
		if o, ok := e.heap[recv]; ok && o.kind == "str" {
			return e.callStrMethod(recv, attr.Name.Value, n.Args)
		}
		if o, ok := e.heap[recv]; ok && o.kind == "list" {
			return e.callListMethod(recv, attr.Name.Value, n.Args)
		}
		if o, ok := e.heap[recv]; ok && o.kind == "dict" {
			return e.callDictMethod(recv, attr.Name.Value, n.Args)
		}
		if o, ok := e.heap[recv]; ok && o.kind == "set" {
			return e.callSetMethod(recv, attr.Name.Value, n.Args)
		}
		// resolve the attribute/method reference via eval
		mID, err := e.eval(n.Fn)
		if err != nil {
			return 0, err
		}
		mo, ok := e.heap[mID]
		if ok && mo.kind == "closure" {
			// imported/module function call: mod.fn(args)
			return e.callClosure(mo, n)
		}
		if ok && mo.kind == "method" {
			argVals := []int64{}
			for _, a := range n.Args {
				av, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				argVals = append(argVals, av)
			}
			self := mo.recv
			if mo.recv == 0 && len(argVals) > 0 {
				self = argVals[0]
				argVals = argVals[1:]
			}
			return e.callMethod(mo, self, argVals)
		}
		return 0, &EvalError{Msg: "not a callable attribute"}
	}
	if name, ok := n.Fn.(*Name); ok {
		// super() returns a proxy bound to the current instance whose methods
		// resolve on the base class of the currently-executing class.
		if name.Value == "super" {
			if len(n.Args) != 0 {
				return 0, &EvalError{Msg: "super() takes no arguments"}
			}
			if e.curClass == "" {
				return 0, &EvalError{Msg: "super() only valid inside a method"}
			}
			classID, ok := e.classIDs[e.curClass]
			if !ok {
				return 0, &EvalError{Msg: "unknown class " + e.curClass}
			}
			cls := e.heap[classID]
			baseID := cls.base
			if baseID == 0 {
				return 0, &EvalError{Msg: "super() outside a subclass"}
			}
			spID := e.allocObj("superproxy")
			sp := e.heap[spID]
			sp.base = baseID
			sp.recv = e.curSelf
			return spID, nil
		}
		// class instantiation: Point(0,0)
		if classID, ok := e.classIDs[name.Value]; ok {
			instID := e.allocObj("instance")
			inst := e.heap[instID]
			inst.class = name.Value
			argVals := []int64{}
			for _, a := range n.Args {
				av, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				argVals = append(argVals, av)
			}
			if initID, ok := e.resolveMethod(classID, "__init__"); ok {
				mo := e.heap[initID]
				mo.recv = instID
				_, err := e.callMethod(mo, instID, argVals)
				if err != nil {
					return 0, err
				}
			}
			return instID, nil
		}
	}
	if name, ok := n.Fn.(*Name); ok {
		// A name the program *declared* with `def` calls through the checked road, which is the one
		// that counts arguments. `e.Vars` now holds a closure handle for a top-level `def` too (so
		// reading the name is no longer a NameError — Gap R.150, ADR 0283), and this road is consulted
		// first, so `f(1, 2)` used to fall into `callClosure`, which evaluates its arguments and pads
		// or ignores them without asking: `print(f(1, 2))` answered `2` and `print(f())` answered `0`,
		// both at exit 0, where CPython raises `takes 1 positional argument but 2 were given` /
		// `missing 1 required positional argument`. A wrong number is the one thing this ladder never
		// trades away, so `funcs` is asked before `Vars` here — and a name the program *assigned*
		// (`g = lambda x: x`, `g = f`) still takes the closure road below.
		if ed, ok2 := e.externs[name.Value]; ok2 {
			return e.callExtern(ed, n.Args)
		}
		if ed, ok2 := e.externs[name.Value]; ok2 {
			return e.callExtern(ed, n.Args)
		}
		// A name the program declared with `def` calls the checked road, which counts arguments. It is
		// consulted BEFORE the closure value above for exactly that reason (roadmap Gap R.168, ADR 0283).
		if fd, ok2 := e.funcs[name.Value]; ok2 {
			argVals := make([]int64, len(fd.Params))
			argSet := make([]bool, len(fd.Params))
			pos := 0
			seenKw := false
			for _, a := range n.Args {
				if kw, ok := a.(*KeywordArg); ok {
					seenKw = true
					idx := -1
					for i, p := range fd.Params {
						if p.Name == kw.Name {
							idx = i
							break
						}
					}
					if idx < 0 {
						return 0, &EvalError{Msg: "unknown keyword argument " + kw.Name}
					}
					if argSet[idx] {
						return 0, &EvalError{Msg: "multiple values for argument " + kw.Name}
					}
					v, err := e.eval(kw.Value)
					if err != nil {
						return 0, err
					}
					argVals[idx] = v
					argSet[idx] = true
					continue
				}
				if seenKw {
					return 0, &EvalError{Msg: "positional argument after keyword argument"}
				}
				if pos >= len(fd.Params) {
					// The same sentence the shared road asks, so one program does not read two ways
					// depending on whether the callee was declared or bound (ADR 0215's wording rule;
					// roadmap Gap R.168, ADR 0284).
					return 0, &EvalError{Msg: fmt.Sprintf("too many arguments for %s: it accepts %d argument%s, got more", callFuncNameForError(fd), len(fd.Params), pluralFor(len(fd.Params)))}
				}
				if argSet[pos] {
					return 0, &EvalError{Msg: "multiple values for argument " + fd.Params[pos].Name}
				}
				v, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				argVals[pos] = v
				argSet[pos] = true
				pos++
			}
			for i, p := range fd.Params {
				if argSet[i] {
					continue
				}
				if p.Default == nil {
					// Named the way the shared road names it, and quoted as the reference quotes a
					// parameter (roadmap Gap R.168, ADR 0284).
					return 0, &EvalError{Msg: fmt.Sprintf("missing argument %q for %s", p.Name, callFuncNameForError(fd))}
				}
				dv, err := e.eval(p.Default)
				if err != nil {
					return 0, err
				}
				argVals[i] = dv
				argSet[i] = true
			}
			if fd.Async {
				cid := e.allocObj("coro")
				co := e.heap[cid]
				co.fn = fd
				co.args = argVals
				return cid, nil
			}
			prevRet := e.curRet
			e.curRet = fd.ReturnAnno
			caller := e.fnName
			callSite := e.cur
			savedFn := e.fnName
			e.fnName = fd.Name
			savedFD := e.curFD
			e.curFD = fd
			defer func() { e.fnName = savedFn; e.curFD = savedFD }()
			// Bind the parameters into the callee's scope and root that scope (and
			// the caller's) for the duration of the call (L7.2). Restoring on the
			// error paths too is what keeps the frame stack balanced.
			restoreScope := e.swapScope(map[string]int64{})
			defer restoreScope()
			for i, p := range fd.Params {
				if p.Annot != nil {
					if err := e.checkAnnot(p.Name, p.Annot, argVals[i]); err != nil {
						return 0, err
					}
				}
				e.Vars[p.Name] = argVals[i]
				e.forgetBool(p.Name)
			}
			if containsYield(fd.Body) {
				genH := e.allocObj("list")
				prev := e.yieldList
				e.yieldList = genH
				e.inCall = true
				_, err := e.runFuncBody(fd, stmtRootCall)
				e.inCall = false
				e.yieldList = prev
				e.curRet = prevRet
				if err != nil {
					return 0, err
				}
				return genH, nil
			}
			e.inCall = true
			rv, err := e.runFuncBody(fd, stmtRootCall)
			e.inCall = false
			e.curRet = prevRet
			if rs, ok := err.(*returnSignal); ok {
				return rs.val, nil
			}
			if err == nil {
				// Fall-off-the-end yields None (see callFunc).
				_ = rv
				return e.noneVal, nil
			}
			return rv, e.recordCall(err, fd.Name, caller, callSite)
		}

		// closure value in the current scope? A name the program *assigned* (`g = lambda x: x`, and
		// `g = f`) takes this road. It is asked only after the declared-function road below, because
		// `e.Vars` now also holds a closure handle for a top-level `def` (so reading the name is no
		// longer a NameError — Gap R.150, ADR 0283); if it were asked first, `f(1, 2)` would fall into
		// `callClosure`, which evaluates its arguments and pads or drops them without asking:
		// `print(f(1, 2))` answered `2` and `print(f())` answered `0`, both at exit 0, where CPython
		// raises `takes 1 positional argument but 2 were given` / `missing 1 required positional
		// argument`. A wrong number is the one thing this ladder never trades away (roadmap Gap R.168).
		if cid, ok2 := e.Vars[name.Value]; ok2 {
			if o := e.heap[cid]; o != nil && o.kind == "closure" {
				return e.callClosure(o, n)
			}
		}
		// built-in exception constructor: ValueError("msg") etc.
		if isExnClass(name.Value) {
			msg := ""
			for _, arg := range n.Args {
				av, err := e.eval(arg)
				if err != nil {
					return 0, err
				}
				if s, ok := e.heap[av]; ok && s.kind == "str" && msg == "" {
					msg = s.sval
				}
			}
			return e.allocExn(name.Value, msg), nil
		}
		switch name.Value {
		case "print":
			// print(*args, sep=" ", end="\n") — Python's separator/terminator
			// semantics, matched exactly by the AOT backend (ADR 0165).
			sep, end := " ", "\n"
			// Arguments are written as they are evaluated, separator first: an
			// argument whose evaluation itself prints must interleave exactly as
			// the AOT backend lowers it (the compiler emits the same order).
			// Keyword arguments are resolved first: `print(a, b, sep="-")` must
			// see its separator before the first separator is written.
			positional := make([]Expr, 0, len(n.Args))
			for _, a := range n.Args {
				kw, isKw := a.(*KeywordArg)
				if !isKw {
					positional = append(positional, a)
					continue
				}
				if kw.Name != "sep" && kw.Name != "end" {
					return 0, &EvalError{Msg: fmt.Sprintf("print got an unexpected keyword argument %q", kw.Name)}
				}
				kv, err := e.eval(kw.Value)
				if err != nil {
					return 0, err
				}
				if kw.Name == "sep" {
					sep = e.Repr(kv)
				} else {
					end = e.Repr(kv)
				}
			}
			for i, a := range positional {
				if i > 0 {
					fmt.Fprint(os.Stdout, sep)
				}
				v, err := e.eval(a)
				if err != nil {
					return 0, err
				}
				// A bool writes True/False. It is held as the 1/0 the comparison produced — the
				// value word carries no kind, and the tagged word that would carry one is the L11.1
				// destination rather than this rung — so the question 「is this a bool?」 is asked of
				// the AST, which is the same question the compiled backend asks through one predicate
				// so the two engines cannot answer it differently (roadmap L11.1 step 2, ADR 0257).
				if IsBoolExpr(a, e.boolEnv()) {
					fmt.Fprint(os.Stdout, BoolText(e.truthy(v)))
					continue
				}
				fmt.Fprint(os.Stdout, e.Repr(v))
			}
			fmt.Fprint(os.Stdout, end)
			return e.noneVal, nil // print returns None, not the int 0
		case "len":
			if len(n.Args) != 1 {
				return 0, &EvalError{Msg: "len expects 1 argument"}
			}
			v, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[v]; ok {
				switch o.kind {
				case "list", "set", "dict":
					return int64(len(o.elems)), nil
				case "str":
					// `len("caf\u00e9")` is 4, the way every other position-counting question in
					// the language counts (ADR 0225) -- bytes answered 5.
					return int64(len([]rune(o.sval))), nil
				}
			}
			return 0, exnError("TypeError", "object of type "+e.valueTypeName(v)+" has no len()")

		case "min", "max":
			// `min(a, b, ...)` is the same builtin as `min([a, b, ...])`, and the reference
			// implementation is the path a person and an agent feel first: refusing it while the
			// compiled leg answered made a program that builds print differently from the one that
			// runs (roadmap Gap R.104, ADR 0256). The winner is returned **as it arrived** — an int
			// winner is the int CPython prints, not the double it was compared against — which is why
			// the candidates are the values and not their numbers.
			if len(n.Args) == 0 {
				return 0, exnError("TypeError", fmt.Sprintf("%s expected at least 1 argument, got 0", name.Value))
			}
			var vals []int64
			if len(n.Args) == 1 {
				lo, err := e.eval(n.Args[0])
				if err != nil {
					return 0, err
				}
				if o, ok := e.heap[lo]; ok && o.kind == "dict" {
					// The interpreter rejects dict literals for min/max (matching
					// codegen); scalars are treated as single-element collections.
					return 0, &EvalError{Msg: "min/max expects a list or set"}
				}
				if o, ok := e.heap[lo]; ok && (o.kind == "list" || o.kind == "set") {
					vals = append([]int64(nil), o.elems...)
				} else {
					// A bare scalar is a one-element collection, and it is the *expression* that says
					// what it holds: `max(True)` chose the verdict CPython prints, so the candidate
					// enters as a slot does — boxed, with the number still underneath for every
					// arithmetic question (roadmap Gap R.117, ADR 0261).
					vals = []int64{e.slotVal(n.Args[0], lo)}
				}
			} else {
				for _, a := range n.Args {
					v, err := e.eval(a)
					if err != nil {
						return 0, err
					}
					// The winner is one of these values, so a candidate enters with the kind its
					// expression has: an argument written `True` is a verdict, and a verdict printed
					// from the winning operand prints True (Gap R.117). A candidate the program
					// computed keeps whatever it already is — a slot's bool box arrives boxed.
					vals = append(vals, e.slotVal(a, v))
				}
			}
			if len(vals) == 0 {
				return 0, exnError("ValueError", fmt.Sprintf("%s() iterable argument is empty", name.Value))
			}
			best := vals[0]
			for _, v := range vals[1:] {
				if name.Value == "min" {
					c, err := e.compareOrder(v, best, "<")
					if err != nil {
						return 0, err
					}
					if c < 0 {
						best = v
					}
					continue
				}
				c, err := e.compareOrder(v, best, ">")
				if err != nil {
					return 0, err
				}
				if c > 0 {
					best = v
				}
			}
			return best, nil
		case "sorted":
			if len(n.Args) < 1 || len(n.Args) > 2 {
				return 0, &EvalError{Msg: "sorted() takes exactly 1 argument"}
			}
			lv, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			lo, ok := e.heap[lv]
			if !ok || lo.kind != "list" {
				return 0, &EvalError{Msg: "sorted() argument must be a list"}
			}
			elems := append([]int64(nil), lo.elems...)
			// The same stable sort xs.sort() uses, so `sorted(xs)` and `xs.sort(); xs`
			// cannot disagree, and so a str/int mix raises where sort.Slice would have
			// silently ordered by handle (roadmap L11.7, ADR 0191).
			if err := e.sortElems(elems); err != nil {
				return 0, err
			}
			// sorted(iter, reverse=True) returns descending order.
			if len(n.Args) > 1 {
				// Accept reverse=True (KeywordArg) or a positional truthy second arg.
				var rev int64
				if kw, ok := n.Args[1].(*KeywordArg); ok {
					rv, err := e.eval(kw.Value)
					if err != nil {
						return 0, err
					}
					rev = rv
				} else {
					rv, err := e.eval(n.Args[1])
					if err != nil {
						return 0, err
					}
					rev = rv
				}
				if rev != 0 {
					for i, j := 0, len(elems)-1; i < j; i, j = i+1, j-1 {
						elems[i], elems[j] = elems[j], elems[i]
					}
				}
			}
			listID := e.allocObj("list")
			nl := e.heap[listID]
			nl.elems = append(nl.elems, elems...)
			return listID, nil
		case "reversed":
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[av]; ok {
				switch o.kind {
				case "list", "set":
					id := e.allocObj("list")
					lo := e.heap[id]
					lo.elems = append(lo.elems, o.elems...)
					for i, j := 0, len(lo.elems)-1; i < j; i, j = i+1, j-1 {
						lo.elems[i], lo.elems[j] = lo.elems[j], lo.elems[i]
					}
					return id, nil
				case "str":
					return e.allocStr(reverseStr(o.sval)), nil
				}
			}
			return 0, &EvalError{Msg: "reversed expects a list or string"}
		case "enumerate":
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[av]; ok && o.kind == "list" {
				id := e.allocObj("list")
				lo := e.heap[id]
				for i, el := range o.elems {
					pair := e.allocObj("list")
					po := e.heap[pair]
					po.elems = append(po.elems, int64(i), el)
					lo.elems = append(lo.elems, pair)
				}
				return id, nil
			}
			return 0, &EvalError{Msg: "enumerate expects a list"}
		case "zip":
			if len(n.Args) != 2 {
				return 0, &EvalError{Msg: "zip expects two lists"}
			}
			l1, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			l2, err := e.eval(n.Args[1])
			if err != nil {
				return 0, err
			}
			o1, ok1 := e.heap[l1]
			o2, ok2 := e.heap[l2]
			if !ok1 || o1.kind != "list" || !ok2 || o2.kind != "list" {
				return 0, &EvalError{Msg: "zip expects two lists"}
			}
			n := len(o1.elems)
			if len(o2.elems) < n {
				n = len(o2.elems)
			}
			id := e.allocObj("list")
			lo := e.heap[id]
			for i := 0; i < n; i++ {
				pair := e.allocObj("list")
				po := e.heap[pair]
				po.elems = append(po.elems, o1.elems[i], o2.elems[i])
				lo.elems = append(lo.elems, pair)
			}
			return id, nil
		case "sum":
			if len(n.Args) != 1 {
				return 0, &EvalError{Msg: "sum expects 1 argument"}
			}
			sv, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			so, ok := e.heap[sv]
			if !ok || (so.kind != "list" && so.kind != "set") {
				return 0, &EvalError{Msg: "sum expects a list or set"}
			}
			// An element is asked what it is before it is added. Adding the raw handle was a
			// silent wrong answer twice over: sum([1.5, 2.5]) added two float-box handles and
			// printed 562949953421319 where Python prints 4.0, and sum([[1], [2]]) added two list
			// handles where Python raises TypeError. Both answers now come from the element's own
			// kind, which is the rule every other numeric builtin already follows (roadmap L11.1).
			itotal := int64(0)
			ftotal := 0.0
			anyFloat := false
			for _, el := range so.elems {
				if o, isObj := e.heap[el]; isObj {
					if o.kind == "bool" {
						// sum([True, 1]) is 2 in Python: a bool adds as its number (Gap R.112).
						itotal += o.bval
						continue
					}
					if o.kind == "float" {
						ftotal += o.fval
						anyFloat = true
						continue
					}
					return 0, &EvalError{Msg: fmt.Sprintf("unsupported operand type(s) for +: 'int' and '%s'", o.kind)}
				}
				itotal += el
			}
			if anyFloat {
				return e.allocFloat(ftotal + float64(itotal)), nil
			}
			return itotal, nil
		case "abs":
			// The reference's own sentence, not ours: `abs()` raises
			// `TypeError: abs() takes exactly one argument (0 given)` (roadmap Gap R.131, ADR 0287).
			// A bare "abs expects 1 argument" is exit-3 output no traceback of the reference produces,
			// and ADR 0215 makes trap wording a contract.
			if len(n.Args) == 0 {
				return 0, exnError("TypeError", "abs() takes exactly one argument (0 given)")
			}
			if len(n.Args) != 1 {
				return 0, exnError("TypeError", fmt.Sprintf("abs() takes exactly one argument (%d given)", len(n.Args)))
			}
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			// The door asks the operand what it is before it writes a number: `abs("hi")` used to hand the
			// interned index to the int evaluator, so the program printed `hi` at exit 0 where the reference
			// raises `TypeError: bad operand type for abs(): 'str'` (roadmap Gap R.140, ADR 0271).
			return e.absolute(av)
		case "floor", "ceil", "sqrt":
			// The three names `predeclared.go` has always advertised and the checker has always
			// typed, and that the evaluator has never answered: `print(floor(3.7))` trapped
			// `NameError: name 'floor' is not defined` — a program the toolchain accepts and then
			// refuses to run (roadmap Gap R.51, ADR 0264). The reference is `math.floor` / `math.ceil`
			// / `math.sqrt`, which answer an int, an int and a float, and raise for a non-real
			// argument and for a domain the number has no answer in.
			if len(n.Args) != 1 {
				return 0, &EvalError{Msg: mathNameArityMessage(name.Value, len(n.Args))}
			}
			arg, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			f, err := e.realOf(arg)
			if err != nil {
				return 0, err
			}
			if name.Value == "sqrt" {
				sv, serr := sqrtAnswer(f)
				if serr != nil {
					return 0, serr
				}
				return e.allocFloat(sv), nil
			}
			whole, werr := floorAnswer(f)
			if name.Value == "ceil" {
				whole, werr = ceilAnswer(f)
			}
			if werr != nil {
				return 0, werr
			}
			return whole, nil
		case "bool":
			// bool() is a constructor answering False, and bool(x) asks the question the `if` road
			// already asks — one predicate, two callers (roadmap Gap R.131, ADR 0287). The name was
			// bound nowhere in the builtin dispatch, so `print(bool())` reported
			// `NameError: name 'bool' is not defined` at exit 3 for a program the reference prints
			// `False` — a missing feature reported as the program's own error.
			if len(n.Args) == 0 {
				return e.allocBool(0), nil
			}
			if len(n.Args) > 1 {
				return 0, exnError("TypeError", fmt.Sprintf("bool() takes at most 1 argument (%d given)", len(n.Args)))
			}
			bv, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			// A boxed bool, not a bare 1: printing a verdict is how the reference spells True/False,
			// and Gap R.42's rule is that a verdict writes its own name (ADR 0257).
			if e.truthy(bv) {
				return e.allocBool(1), nil
			}
			return e.allocBool(0), nil
		case "int":
			// int() with no argument is a CONSTRUCTOR and answers 0; int(x) converts. The case used to
			// reach n.Args[0] before asking whether there was one, so `print(int())` died with a Go
			// stack trace and exit 2 — the compiler's bug, which ADR 0166 reserves for exactly this
			// (roadmap Gap R.131, ADR 0287). The same missing check sat in float(), ord() and chr().
			if len(n.Args) == 0 {
				return 0, nil
			}
			if len(n.Args) > 1 {
				return 0, exnError("TypeError", fmt.Sprintf("int() takes at most 2 arguments (%d given)", len(n.Args)))
			}
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[av]; ok && o.kind == "float" {
				return int64(o.fval), nil
			}
			if o, ok := e.heap[av]; ok && o.kind == "str" {
				f, err := strconv.ParseFloat(o.sval, 64)
				if err != nil {
					return 0, exnError("ValueError", "invalid literal for int() with base 10: '"+o.sval+"'")
				}
				return int64(f), nil
			}
			return av, nil
		case "float":
			if len(n.Args) == 0 {
				return e.allocFloat(0), nil
			}
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if o, ok := e.heap[av]; ok && o.kind == "str" {
				f, err := strconv.ParseFloat(o.sval, 64)
				if err != nil {
					return 0, exnError("ValueError", "could not convert string to float: '"+o.sval+"'")
				}
				return e.allocFloat(f), nil
			}
			if o, ok := e.heap[av]; ok && o.kind == "float" {
				if o.fval < 0 {
					return e.allocFloat(-o.fval), nil
				}
				return av, nil
			}
			return e.allocFloat(float64(av)), nil
		case "range":
			if len(n.Args) < 1 || len(n.Args) > 3 {
				return 0, &EvalError{Msg: "range expects 1 to 3 arguments"}
			}
			return e.eval(n.Args[0])
		case "any", "all":
			// any(iter) is 1 if any element is truthy; all(iter) is 1 if all are.
			arg, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			if arg <= 0 {
				return 0, &EvalError{Msg: "any/all need a list"}
			}
			o, ok := e.heap[arg]
			if !ok || o.kind != "list" {
				return 0, &EvalError{Msg: "any/all need a list"}
			}
			anyMode := name.Value == "any"
			if anyMode {
				for _, v := range o.elems {
					if v != 0 {
						return 1, nil
					}
				}
				return 0, nil
			}
			for _, v := range o.elems {
				if v == 0 {
					return 0, nil
				}
			}
			return 1, nil
		case "chr":
			// chr(n) returns the single-character string for codepoint n. With no argument it is
			// neither: the reference raises, and a raised program exits 3 here (Gap R.131).
			if len(n.Args) == 0 {
				return 0, exnError("TypeError", "chr() takes exactly one argument (0 given)")
			}
			if len(n.Args) > 1 {
				return 0, exnError("TypeError", fmt.Sprintf("chr() takes exactly one argument (%d given)", len(n.Args)))
			}
			cn, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			return e.allocStr(string(rune(cn))), nil
		case "ord":
			// ord(s) returns the codepoint of the first character of s.
			if len(n.Args) == 0 {
				return 0, exnError("TypeError", "ord() takes exactly one argument (0 given)")
			}
			if len(n.Args) > 1 {
				return 0, exnError("TypeError", fmt.Sprintf("ord() takes exactly one argument (%d given)", len(n.Args)))
			}
			arg, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			o, ok := e.heap[arg]
			if !ok {
				return 0, &EvalError{Msg: "ord needs a string"}
			}
			runes := []rune(o.sval)
			if len(runes) == 0 {
				return 0, &EvalError{Msg: "ord of empty string"}
			}
			// the first *code point*, which is what ord means; the byte was the same answer for
			// ASCII and a wrong one for everything else (ADR 0225).
			return int64(runes[0]), nil
		case "round":
			// round(x) is the identity for ints; a float goes to the nearest value, ties to EVEN —
			// IEEE roundTiesToEven, which is CPython's rule and the rule the compiled backend emits
			// (`@llvm.roundeven.f64`). `math.Round` here used to tie away from zero, so both backends
			// answered round(2.5) = 3 and the parity matrix, comparing us to us, saw nothing.
			//
			// round(x, ndigits) is the other question — it moves the decimal point instead of asking
			// for a whole number, so it answers with a float (`round(3.5, 0)` is `4.0`) through the
			// one rule in round_digits.go that the compiled runtime answers with the C library's own
			// conversion. Ignoring ndigits and handing back an int was Gap R.69.
			if len(n.Args) == 0 || len(n.Args) > 2 {
				return 0, &EvalError{Msg: roundArityMessage(len(n.Args))}
			}
			x, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			nd := 0
			if len(n.Args) == 2 {
				nv, err := e.eval(n.Args[1])
				if err != nil {
					return 0, err
				}
				if nd, err = e.roundNdigits(nv); err != nil {
					return 0, err
				}
			}
			if o, ok := e.heap[x]; ok && o.kind == "float" {
				if len(n.Args) == 2 {
					return e.allocFloat(roundToDigits(o.fval, nd)), nil
				}
				return int64(math.RoundToEven(o.fval)), nil
			}
			return x, nil

		case "str", "repr":
			called := n.Fn.(*Name).Value
			// str() with no argument is a constructor answering the empty text, the same shape as
			// int() and float() (roadmap Gap R.131, ADR 0287). It used to answer a bare "expects 1
			// argument" EvalError, which is not a sentence the reference produces for this program.
			// repr() is NOT the same shape: `repr()` has no default and raises, so the two halves of
			// the pair keep their own arity even though one rendering serves both (Gap R.131's
			// asymmetry, measured against the reference rather than assumed from str()).
			if len(n.Args) == 0 {
				if called == "repr" {
					return 0, exnError("TypeError", "repr() takes exactly one argument (0 given)")
				}
				return e.allocStr(""), nil
			}
			if len(n.Args) > 1 {
				return 0, exnError("TypeError", fmt.Sprintf("%s() takes exactly one argument (%d given)", called, len(n.Args)))
			}
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			// str(True) is the word "True", not the digit "1": the same question print asks,
			// asked of the same predicate, answered with the same text (ADR 0257).
			if IsBoolExpr(n.Args[0], e.boolEnv()) {
				return e.allocStr(BoolText(e.truthy(av))), nil
			}
			// str and repr are one pair and one renderer: Repr is the text print already
			// writes, and repr differs from it only where Python's own pair differs — a text,
			// which writes its quoted form. Every other value is the same answer from the same
			// code, which is what closes Gap L.2: the two halves used to be two functions.
			return e.allocStr(e.renderOf(av, formOfName(called))), nil
		case "set", "list":
			// Empty constructors, and copies of another container. `{}` is already the
			// empty dict, but the empty *set* has no literal (Python renders it set()),
			// so `set()` is the only way to write one (roadmap Gap K.3).
			kind := n.Fn.(*Name).Value
			h := e.allocObj(kind)
			if len(n.Args) == 0 {
				return h, nil
			}
			if len(n.Args) != 1 {
				return 0, exnError("TypeError", kind+"() takes at most 1 argument")
			}
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			o := e.heap[av]
			if o == nil || (o.kind != "list" && o.kind != "set" && o.kind != "dict") {
				return 0, exnError("TypeError", kind+"() takes a list, set or dict")
			}
			dst := e.heap[h]
			for _, el := range o.elems {
				if kind == "set" {
					dup := false
					for _, x := range dst.elems {
						if x == el {
							dup = true
						}
					}
					if dup {
						continue
					}
				}
				dst.elems = append(dst.elems, el)
			}
			return h, nil
		case "dict":
			if len(n.Args) > 1 {
				return 0, exnError("TypeError", "dict() takes at most 1 argument")
			}
			h := e.allocObj("dict")
			if len(n.Args) == 0 {
				return h, nil
			}
			av, err := e.eval(n.Args[0])
			if err != nil {
				return 0, err
			}
			o := e.heap[av]
			if o == nil || o.kind != "dict" {
				return 0, exnError("TypeError", "dict() copies another dict")
			}
			dst := e.heap[h]
			// One rule for every dict builder: the copy puts each entry rather than splicing the
			// two arrays, so a copy cannot be a container with two entries under one key.
			for i := range o.elems {
				e.dictPut(dst, o.elems[i], o.dvals[i])
			}
			return h, nil
		}
	}
	// Nothing took the call. `x = 5` then `x()` is Python's "'int' object is not callable" —
	// a TypeError the program can catch — and the old text ("unsupported call for eval")
	// leaked the interpreter's own dispatch into the user's traceback while leaving the
	// exception class empty, so no handler could ever have matched it (Gap R.25, ADR 0214).
	if name, ok := n.Fn.(*Name); ok {
		// A module-level binding counts as a binding here too: `cb = 5` then `cb()` is
		// "'int' object is not callable", not a NameError for a name that does exist.
		v, bound := e.Vars[name.Value]
		if !bound {
			v, bound = e.globalLookup(name.Value)
		}
		if bound {
			return 0, exnError("TypeError", e.valueTypeName(v)+" object is not callable")
		}
		return 0, exnError("NameError", "name '"+name.Value+"' is not defined")
	}
	return 0, exnError("TypeError", "object of this kind is not callable")
}

// EvalError is a runtime eval error. When a raised exception is the cause,
// ExnType carries the exception class name (e.g. "ValueError") and ExnMsg
// its message; otherwise both are empty.
// Frame is one call-stack frame in a runtime traceback.
type Frame struct {
	Name string `json:"name"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}

type EvalError struct {
	Msg       string
	ExnType   string
	ExnMsg    string
	Traceback []Frame `json:"traceback,omitempty"`
}

func (e *EvalError) Error() string { return "eval error: " + e.Msg }

// reverseStr returns the reverse of s.
func reverseStr(s string) string {
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// exnError builds an EvalError carrying a typed exception (type name + message).
func exnError(exnType, msg string) *EvalError {
	return &EvalError{Msg: msg, ExnType: exnType, ExnMsg: msg}
}

// floorDiv and floorMod are the pair, defined together because they are only correct
// together. Go's `/` and `%` truncate toward zero, so mixed-sign operands break the
// identity Python guarantees — a == (a // b) * b + (a % b) — which is why `-7 // 2` must
// be -4 with `-7 % 2` equal to 1, not -3 with -1 (roadmap Gaps R.28, R.30: the compiled
// backend had both halves wrong, the interpreter only the modulo half). One definition,
// used by the interpreter, the codegen constant folder and, in IR form, the emitted
// sdiv/srem corrections, so no backend can drift toward C again.
func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}

func floorMod(a, b int64) int64 {
	r := a % b
	if r != 0 && ((r < 0) != (b < 0)) {
		r += b
	}
	return r
}

// floorModFloat is the same rule for floats. libm's fmod (Go's math.Mod, LLVM's frem) is
// the *truncated* remainder, so `-7.0 % 2.0` answers -1 unless it is corrected the same
// way; the result carries the sign of the divisor.
func floorModFloat(a, b float64) float64 {
	r := math.Mod(a, b)
	if r == 0 {
		// An exact remainder keeps the divisor's sign, as IEEE requires: `7.5 % -0.5` is
		// -0.0, not 0.0. fmod's zero carries the dividend's sign instead, so the two
		// languages would print different renderings of the same number.
		return math.Copysign(0, b)
	}
	if (r < 0) != (b < 0) {
		r += b
	}
	return r
}

// valueTypeName names a value the way an exception message does: quoted, and for an instance
// its class name, so `'int' object is not subscriptable` reads here the way it reads in the
// reference implementation. A trap the user cannot grep for is a trap they cannot learn.
func (e *Evaluator) valueTypeName(v int64) string {
	return "'" + e.operandKind(v) + "'"
}

// classDisplayName recovers the name a class object was defined with, for the AttributeError
// message: `type object 'P' has no attribute 'x'`, as the reference words it.
func (e *Evaluator) classDisplayName(o *obj) string {
	if o == nil {
		return "type"
	}
	if o.class != "" {
		return o.class
	}
	for name, id := range e.classIDs {
		if e.heap[id] == o {
			return name
		}
	}
	return "type"
}

// unpackArityErr is CPython's wording for the two ways an unpack can fail to line up, raised
// under ValueError. Both used to be interpreter-shape prose with no exception class
// ("cannot unpack value into tuple"), which made them uncatchable and un-searchable.
func unpackArityErr(want, got int) error {
	if got < want {
		return exnError("ValueError", fmt.Sprintf("not enough values to unpack (expected %d, got %d)", want, got))
	}
	return exnError("ValueError", fmt.Sprintf("too many values to unpack (expected %d)", want))
}

// operandKind names a value's runtime type the way an exception message names it: `int`, `float`,
// `str`, `list`, `NoneType`, and for an instance its class name. An unboxed value with no heap
// object is an integer — the representation's only untagged kind, and sound here because heap
// handles start above every value a literal writes.
func (e *Evaluator) operandKind(v int64) string {
	o, ok := e.heap[v]
	if !e.isHandle(v) || !ok {
		return "int"
	}
	switch o.kind {
	case "none":
		return "NoneType"
	case "instance":
		if o.class != "" {
			return o.class
		}
		return "object"
	case "closure", "method", "function":
		return "function"
	case "class":
		return "type"
	}
	return o.kind
}

// unsupportedOperand, unsupportedCompare and cannotConcat are the three shapes an operator refusal
// takes in the reference implementation. They are functions rather than inline strings because the
// wording is a contract: it is what a user searches for, and what the tests compare against the
// oracle's output (ADR 0215).
func unsupportedOperand(op, leftKind, rightKind string) error {
	return exnError("TypeError", fmt.Sprintf("unsupported operand type(s) for %s: '%s' and '%s'", op, leftKind, rightKind))
}

func unsupportedCompare(op, leftKind, rightKind string) error {
	return exnError("TypeError", fmt.Sprintf("'%s' not supported between instances of '%s' and '%s'", op, leftKind, rightKind))
}

func cannotConcat(kind, otherKind string) error {
	return exnError("TypeError", fmt.Sprintf("can only concatenate %s (not %q) to %s", kind, otherKind, kind))
}

// checkBinOp decides whether an operator may be applied to *these* values at all. Before it, the
// operator switch never consulted operand kinds: a heap handle that reached an arithmetic path was
// multiplied or added as an integer, so `print("a" * "b")` answered 1099516870662 and `print(1 +
// None)` answered 1048578 — numbers no expression in the program denotes, printed with exit 0.
// Raising is the only acceptable answer for an operator the operands do not support (ADR 0212,
// ADR 0215). Ordered comparisons are not in this table: ordVal owns them, errors included.
func (e *Evaluator) checkBinOp(op string, l, r int64) error {
	lk, rk := e.operandKind(l), e.operandKind(r)
	numeric := func(k string) bool { return k == "int" || k == "float" }
	sequence := func(k string) bool { return k == "str" || k == "list" }
	switch op {
	case "+":
		switch {
		case numeric(lk) && numeric(rk), lk == "str" && rk == "str", lk == "list" && rk == "list":
			return nil
		case lk == "str" || lk == "list":
			// Python distinguishes "you handed me the wrong right-hand side" from "this
			// operator does not apply", and the first message names the type you should
			// have passed.
			return cannotConcat(lk, rk)
		default:
			return unsupportedOperand(op, lk, rk)
		}
	case "-", "/", "//", "**":
		if numeric(lk) && numeric(rk) {
			return nil
		}
		// The reference names `**` by both spellings, because the same operation is reachable
		// as pow(); the message names both so a search finds whichever the user typed.
		if op == "**" {
			return unsupportedOperand("** or pow()", lk, rk)
		}
		return unsupportedOperand(op, lk, rk)
	case "*":
		if numeric(lk) && numeric(rk) {
			return nil
		}
		if (sequence(lk) && rk == "int") || (sequence(rk) && lk == "int") {
			return nil
		}
		if sequence(lk) || sequence(rk) {
			other := rk
			if !sequence(lk) {
				other = lk
			}
			return exnError("TypeError", fmt.Sprintf("can't multiply sequence by non-int of type '%s'", other))
		}
		return unsupportedOperand(op, lk, rk)
	case "%":
		if numeric(lk) && numeric(rk) {
			return nil
		}
		if lk == "str" {
			// `%` on a string is interpolation, which the language does not have yet (roadmap
			// Gap R.31). CPython's own message is reproduced for the shape it would also
			// reject; where Python would have formatted something we still raise, and that
			// divergence is pinned in programs/probe_percent_format.gy rather than hidden.
			if o := e.heap[l]; o != nil && !strings.Contains(o.sval, "%") {
				return exnError("TypeError", "not all arguments converted during string formatting")
			}
			return unsupportedOperand(op, lk, rk)
		}
		return unsupportedOperand(op, lk, rk)
	}
	// `==`, `!=`, `in`, `not in`, `is`, `is not`, `and`, `or` are total: they compare or
	// short-circuit, they do not compute, so there is no operand type they can refuse.
	return nil
}

// repeatSequence is `seq * n` for a str or a list, in either operand order. A negative or absent
// count is empty rather than an error, as in the reference implementation; a non-int count is the
// TypeError that names the count's type.
func (e *Evaluator) repeatSequence(kind string, seq, count int64) (int64, error) {
	src := e.heap[seq]
	if src == nil {
		return 0, exnError("TypeError", "can't multiply sequence by non-int of type '"+e.operandKind(count)+"'")
	}
	if _, ok := e.heap[count]; ok {
		return 0, exnError("TypeError", "can't multiply sequence by non-int of type '"+e.operandKind(count)+"'")
	}
	if count <= 0 {
		if kind == "str" {
			return e.allocStr(""), nil
		}
		return e.allocObj("list"), nil
	}
	if kind == "str" {
		return e.allocStr(strings.Repeat(src.sval, int(count))), nil
	}
	h := e.allocObj("list")
	dst := e.heap[h]
	for i := int64(0); i < count; i++ {
		dst.elems = append(dst.elems, src.elems...)
	}
	return h, nil
}

// ordVal orders two values the language can order: two numbers, two strings, or two lists compared
// element by element, longer winning ties. Anything else is the reference implementation's
// TypeError, including the nested case nobody tests — `[1] < ["a"]` — because a list whose elements
// cannot be ordered has not earned an answer.
func (e *Evaluator) ordVal(op string, a, b int64) (int, error) {
	ao, aok := e.heap[a]
	bo, bok := e.heap[b]
	aNum := !aok || ao.kind == "float"
	bNum := !bok || bo.kind == "float"
	if aNum && bNum {
		af, _ := e.floatOf(a)
		bf, _ := e.floatOf(b)
		if !aok {
			af = float64(a)
		}
		if !bok {
			bf = float64(b)
		}
		switch {
		case af < bf:
			return -1, nil
		case af > bf:
			return 1, nil
		}
		return 0, nil
	}
	if aok && bok && ao.kind == "str" && bo.kind == "str" {
		return strings.Compare(ao.sval, bo.sval), nil
	}
	if aok && bok && ao.kind == "list" && bo.kind == "list" {
		n := len(ao.elems)
		if len(bo.elems) < n {
			n = len(bo.elems)
		}
		for i := 0; i < n; i++ {
			c, err := e.ordVal(op, ao.elems[i], bo.elems[i])
			if err != nil {
				return 0, err
			}
			if c != 0 {
				return c, nil
			}
		}
		switch {
		case len(ao.elems) < len(bo.elems):
			return -1, nil
		case len(ao.elems) > len(bo.elems):
			return 1, nil
		}
		return 0, nil
	}
	return 0, unsupportedCompare(op, e.operandKind(a), e.operandKind(b))
}

// orderedBy is the one mapping from a three-way comparison to the four order operators; sharing it
// is what keeps `<=` from drifting out of agreement with `<`, which is how the pair ended up
// contradicting each other in the compiled backend.
func orderedBy(op string, cmp int) bool {
	switch op {
	case "<":
		return cmp < 0
	case "<=":
		return cmp <= 0
	case ">":
		return cmp > 0
	case ">=":
		return cmp >= 0
	}
	return false
}

// boolVal is the language's boolean: 1 and 0, printed as such.
func boolVal(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// zeroDivisionErr is the one place the arithmetic traps are raised. The sites used to build
// a bare `&EvalError{Msg: "division by zero"}`, which printed a message with no exception
// class and — the part that made it a language defect rather than a cosmetic one — could not
// be caught: `except ZeroDivisionError:` matched nothing, because matching is on the class.
// The wording follows CPython's so the two tracebacks are comparable line for line
// (roadmap Gap R.18, ADR 0212).
func zeroDivisionErr(kind string) *EvalError {
	e := exnError("ZeroDivisionError", kind)
	return e
}

// floorMessage is the other half of the same rule: `7.0 // 0` is a *floor* division and says
// so, where `7.0 / 0` does not. Sharing the arithmetic branch made both say "float division by
// zero", which CPython never does — and a message that differs from the reference for no
// reason is a message nobody can grep for.
func floorMessage(floor bool) string {
	if floor {
		return "float floor division by zero"
	}
	return "float division by zero"
}

// loopSignal carries break/continue control out of a loop body.
type loopSignal struct{ kind string }

func (l *loopSignal) Error() string { return "loop signal: " + l.kind }

// returnSignal carries a `return` statement's value out of nested blocks to the
// enclosing function/method call site. It implements error so that it flows up
// through the statement-loop block handlers (if/while/for), which propagate
// errors via `return 0, err`.
type returnSignal struct{ val int64 }

func (r *returnSignal) Error() string { return "return signal" }

// catchesException answers the only question an `except` arm is allowed to ask: is the
// thing in flight an exception at all? A `return`, `break` or `continue` is a transfer, and
// this interpreter moves transfers with the same Go `error` mechanism it uses for raises, so
// an arm that matched on "some error came out" swallowed control flow -- a bare `except:`
// used to catch a `return` and drop the function's value (roadmap Gap R.23, ADR 0222).
// Anything that is not one of these three propagates uncaught, which is the safe direction.
func catchesException(err error) bool {
	switch err.(type) {
	case *returnSignal, *loopSignal:
		return false
	case *EvalError:
		return true
	}
	return false
}

// EvalExpr compiles src and evaluates it, returning the integer result and diagnostics.
func EvalExpr(src string) (int64, []Diagnostic, error) {
	prog, err := parseProgram(src)
	if err != nil {
		return 0, nil, err
	}
	diags := Analyze(prog)
	if anyErr(diags) {
		return 0, diags, fmt.Errorf("verify: %s", diags[0].Msg)
	}
	ev := NewEvaluator()
	v, err := ev.EvalProgram(prog)
	if err != nil {
		return v, diags, ev.FinalizeTraceback(err)
	}
	return v, diags, err
}

// FinalizeTraceback attaches the <module> frame to a runtime error when a
// module was evaluated directly (the CLI calls EvalProgram directly).
func (e *Evaluator) FinalizeTraceback(err error) error {
	ee, ok := err.(*EvalError)
	if !ok || ee.Traceback != nil {
		return err
	}
	ee.Traceback = []Frame{{Name: "<module>", Line: e.cur.Line, Col: e.cur.Col}}
	return err
}

// RenderTraceback renders a Python-style traceback for a runtime EvalError.
func (e *EvalError) RenderTraceback() string {
	if len(e.Traceback) == 0 {
		return ""
	}
	out := "Traceback (most recent call last):\n"
	for _, f := range e.Traceback {
		out += fmt.Sprintf("  File \"prog\", line %d, in %s\n", f.Line, f.Name)
	}
	// Python's last line is `ValueError: boom`, and so is ours: the type is what an
	// `except IndexError:` matched on, so dropping it made the report less specific
	// than the raise that produced it.
	if e.ExnType != "" && e.Msg != e.ExnType {
		out += e.ExnType + ": " + e.Msg
	} else {
		out += e.Msg
	}
	return out
}

func containsYield(stmts []Stmt) bool {
	for _, st := range stmts {
		switch s := st.(type) {
		case *YieldStmt:
			return true
		case *YieldFromStmt:
			return true
		case *IfStmt:
			if containsYield(s.Then) {
				return true
			}
			for _, e := range s.Elifs {
				if containsYield(e.Then) {
					return true
				}
			}
			if containsYield(s.Else) {
				return true
			}
		case *WhileStmt:
			if containsYield(s.Body) || containsYield(s.Else) {
				return true
			}
		case *ForStmt:
			if containsYield(s.Body) || containsYield(s.Else) {
				return true
			}
		case *MatchStmt:
			for _, c := range s.Cases {
				if containsYield(c.Body) {
					return true
				}
			}
		case *FuncDef:
			if containsYield(s.Body) {
				return true
			}
		}
	}
	return false
}

// callExtern evaluates an FFI call to a C function. In the AST interpreter we
// dispatch to a small Go registry mirroring the C stdlib functions.
func (e *Evaluator) callExtern(ed *ExternDecl, args []Expr) (int64, error) {
	vals := []int64{}
	for _, a := range args {
		v, err := e.eval(a)
		if err != nil {
			return 0, err
		}
		vals = append(vals, v)
	}
	switch ed.Name {
	case "abs":
		// The method road asks the same question with the same sentence as the builtin above
		// (roadmap Gap R.131, ADR 0287).
		if len(vals) == 0 {
			return 0, fmt.Errorf("abs() takes exactly one argument (0 given)")
		}
		if len(vals) != 1 {
			return 0, fmt.Errorf("abs() takes exactly one argument (%d given)", len(vals))
		}
		x := vals[0]
		if x < 0 {
			x = -x
		}
		return x, nil
	case "getpid":
		return int64(os.Getpid()), nil
	case "rand":
		return int64(rand.Intn(1 << 30)), nil
	case "strlen":
		if len(vals) != 1 {
			return 0, fmt.Errorf("strlen expects 1 argument")
		}
		sid := vals[0]
		sv, ok := e.heap[sid]
		if !ok || sv == nil || sv.sval == "" && sv.kind != "str" {
			return 0, fmt.Errorf("strlen: not a string value")
		}
		return int64(len(sv.sval)), nil
	default:
		return 0, fmt.Errorf("extern function %q is not available in the interpreter", ed.Name)
	}
}
