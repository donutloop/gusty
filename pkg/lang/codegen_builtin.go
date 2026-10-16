// codegen_builtin.go is the road a call to a builtin takes: the one place `print`, `len`,
// `sum`, `int`, `sorted` and the rest of the predeclared names decide what they answer.
// g.call has already ruled out a user function, a method, an extern, an import and a class
// instantiation by the time it asks here, and the family roads this file dispatches to live
// in codegen_builtin_*.go.
package lang

import (
	"fmt"
	"strings"
)

// callBuiltins is the builtin dispatch: which predeclared name is being called, asked once,
// with the names' meaning read from one table rather than restated on each road
// (predeclared.go, ADR 0243). A name the backend cannot build a call to is refused here, in
// words, with the roadmap row that owns the missing half.
func (g *irGen) callBuiltins(b *strings.Builder, c *Call, fnName string) (string, error) {
	switch fnName {
	case "print", "printf":
		return g.builtinPrint(b, c, fnName)
	case "set", "list", "dict":
		return g.builtinContainers(b, c, fnName)
	case "len":
		return g.builtinLen(b, c, fnName)
	case "any", "all":
		return g.builtinAnyAll(b, c, fnName)
	case "sum":
		return g.builtinSum(b, c, fnName)
	case "min", "max":
		return g.builtinMinMax(b, c, fnName)
	case "abs":
		return g.builtinAbs(b, c, fnName)
	case "sqrt":
		return g.builtinSqrt(b, c, fnName)
	case "floor", "ceil":
		return g.builtinFloorCeil(b, c, fnName)
	case "range":
		return g.builtinRange(b, c, fnName)
	case "str", "repr":
		return g.builtinStr(b, c, fnName)
	case "int":
		return g.builtinInt(b, c, fnName)
	case "reversed":
		return g.builtinReversed(b, c, fnName)
	case "sorted":
		return g.builtinSorted(b, c, fnName)
	case "chr":
		return g.builtinChr(b, c, fnName)
	case "ord":
		return g.builtinOrd(b, c, fnName)
	case "round":
		return g.builtinRound(b, c, fnName)
	case "float":
		return g.builtinFloat(b, c, fnName)
	case "bool":
		return g.builtinBool(b, c, fnName)
	default:
		// The name is called and the backend cannot find a callable behind it. The reference raises
		// TypeError here — `'int' object is not callable` — because it asks the value what it is at
		// the moment of the call; this backend decides at the compile door, and says so with the half
		// it is missing rather than the word "unsupported" alone (ADR 0166's vocabulary, and the
		// tagged value word of roadmap L11.1 is what would let the question be asked at run time).
		return "", fmt.Errorf("codegen: %q is called but is not a function this backend can build a call to: the reference asks the value what it is at the moment of the call and raises TypeError (`'int' object is not callable`), and asking that question needs the value word that carries its own kind (roadmap L11.1, ADR 0166)", fnName)
	}
}
