package lang

// Predeclared names: identifiers that may appear as a value without any binding the
// source can see. The AOT codegen consults this when it is asked to lower a name it has
// no slot for (see irGen.nameIsBound): refusing with "undefined name" is actionable,
// while emitting a load from a slot that does not exist shows up as an LLVM verifier
// failure — which the exit-code contract calls a *compiler bug* (roadmap Gap K.10).
//
// The list is deliberately generous. A false "undefined name" breaks a program that
// compiled yesterday; a missing entry only weakens the safety net.
var predeclaredNames = map[string]bool{}

func init() {
	for _, n := range []string{
		// builtins every scope predeclares
		"print", "range", "len", "min", "max", "abs", "sum", "round",
		"int", "float", "str", "bool", "bytes", "list", "dict", "set", "tuple",
		"sorted", "reversed", "enumerate", "zip", "map", "filter",
		// `type` is deliberately absent: it is a reserved word in this grammar
		// (annotations), so `type(x)` never parses — listing it would promise a call
		// that cannot be written (asserted by TestBuiltinsArePredeclaredInTheChecker).
		"ord", "chr", "input", "isinstance", "super", "repr", "hash", "id",
		"getattr", "setattr", "hasattr", "callable", "any", "all", "pow", "divmod",
		"ceil", "floor", "sqrt", "fabs", "min", "max", "enumerate", "sum",
		// literals that parse as names in this AST
		"True", "False", "None",
		// exception classes are raisable/catchable as bare names (exceptions.go)
		"Exception", "ValueError", "TypeError", "KeyError", "IndexError",
		"RuntimeError", "StopIteration", "ZeroDivisionError",
	} {
		predeclaredNames[n] = true
	}
}

// isPredeclaredName reports whether nm is a name the language provides without a binding.
func isPredeclaredName(nm string) bool { return predeclaredNames[nm] }

// predeclaredCallables returns the builtin call names as a fresh slice, sorted-free; the
// checker predeclares each so a call to a real built-in is never reported as an undefined
// name, and codegen consults the same table. One list, three consumers (checker, codegen
// guard, LSP completion) — the drift where `sum` worked in both backends but was unknown to
// the checker is what made `print(sum(xs))` fail to compile (roadmap Gap K.10).
func predeclaredCallables() []string {
	out := make([]string, 0, len(predeclaredNames))
	for n := range predeclaredNames {
		out = append(out, n)
	}
	return out
}
