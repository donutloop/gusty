package lang

// Built-in exception classes, defined once for all three front ends.
//
// The interpreter, the checker, and the AOT codegen each used to keep their own
// notion of what an exception class is: `isExnClass` (interpreter) listed eight
// names, `exnCode` (codegen) numbered them, and the checker's `exceptions` map was
// declared but never populated — so `raise ValueError("boom")` was a perfectly good
// interpreter program that failed AOT compilation with `undefined name "ValueError"`.
// They now share one list, and the numeric codes are part of it: the code is what
// `@exn_code` carries across the raise boundary, and `except IndexError:` matches on it.
var exnClasses = []struct {
	name string
	code int
}{
	{"Exception", 0},
	{"ValueError", 1},
	{"TypeError", 2},
	{"KeyError", 3},
	{"IndexError", 4},
	{"RuntimeError", 5},
	{"StopIteration", 6},
	{"ZeroDivisionError", 7},
	// A local read on a path that never assigned it. It needs its own code, not NameError's:
	// the two say different things (the name is *in* this frame's scope and simply has no value
	// yet), a program may catch one and not the other, and both backends must raise the class
	// CPython does for `except UnboundLocalError:` to match on either of them (roadmap Gap R.36
	// + R.39, ADR 0228; the same "a built-in trap is a typed raise" rule as ADR 0212).
	{"UnboundLocalError", 8},
	// `math.floor(1e300)` has no whole number to answer with, and neither does the compiled
	// backend's bounded int word (roadmap L12.12). CPython's `int(inf)` is an OverflowError, so the
	// class the compiled guard raises is the one a program's `except` can already match on the other
	// two engines (roadmap Gap R.51, ADR 0264).
	{"OverflowError", 9},
}

// isExnClass reports whether name is a built-in exception constructor. `except E:`
// and `raise E("msg")` accept these, and user classes derive their own.
func isExnClass(name string) bool {
	for _, c := range exnClasses {
		if c.name == name {
			return true
		}
	}
	return false
}

// exnClassCode returns the runtime code for an exception class name.
func exnClassCode(name string) int {
	for _, c := range exnClasses {
		if c.name == name {
			return c.code
		}
	}
	return 0 // matches Python's "uncaught exception of unknown type" default
}

// builtinExceptions returns the built-in exception names as a name set, for the
// checker's name table. Keeping it derived from exnClasses means a new exception
// class is added in exactly one place.
func builtinExceptions() map[string]bool {
	m := make(map[string]bool, len(exnClasses))
	for _, c := range exnClasses {
		m[c.name] = true
	}
	return m
}
