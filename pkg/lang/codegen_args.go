package lang

import (
	"fmt"
	"strings"
)

// callArgBinder lays ONE call site's arguments out over the parameters of the callable it reached, and
// remembers the words it emitted while doing so.
//
// One binder serves every arm of a dispatch that has several candidate callees. The run-time class-id
// road dispatches one call expression on the receiver's class as it is only known while the program
// runs, so the same argument list is handed to several `define`s; emitting an operand twice would define
// the same value twice, and a register defined in the arm that did not run is a use the verifier rejects
// — Gap R.318's rule that no path may emit IR it cannot name. Everything the binder emits therefore goes
// out before the switch, once, whatever arm turns out to run.
type callArgBinder struct {
	g    *irGen
	b    *strings.Builder
	memo map[Expr]string
}

func (g *irGen) newCallArgBinder(b *strings.Builder) *callArgBinder {
	return &callArgBinder{g: g, b: b, memo: map[Expr]string{}}
}

// operand is the one word an argument travels in, memoised on the expression so a call that dispatches
// on a run-time class asks for the same operand from every arm.
func (cb *callArgBinder) operand(a Expr) (string, error) {
	if a == nil {
		return "", nil
	}
	if v, ok := cb.memo[a]; ok {
		return v, nil
	}
	v, err := cb.g.value(cb.b, a)
	if err != nil {
		return "", err
	}
	cb.memo[a] = v
	return v, nil
}

// callArgs lays `args` over the parameters of the callable the call reached, in the order the callee
// DECLARED them — which is the only order its `define` can read them back — and returns one word per
// parameter after the caller's own `skip` leading slots (1 for a method, whose slot 0 is `self` and
// arrives in the receiver word the caller wrote itself; 0 for a module function, which has no receiver).
//
// A METHOD call used to emit one word per argument the source WROTE (`for i, arg := range c.Args`, four
// times over in codegen_method.go and once in codegen_instance.go) and hand the callee that many words.
// The callee reads its parameters by position, so an omitted default was whatever the register allocator
// left in the slot: `class C():` / `    def m(self, a, b=2):` / `        return b` / `print(C().m(5))`
// printed `0` where the reference prints `2`, at the exit code of success, and `llc` and `opt
// -passes=verify` both accept the under-filled module — which is why no layer between here and the
// object file noticed. A keyword argument made it worse rather than better: `def m(self, a=1, b=2):` /
// `        return a * 10 + b` called as `c.m(b=7, a=3)` wrote the two values in the order the source
// wrote them, so the answer came back `73` for the reference's `37` — the names read as positionals, a
// wrong number wearing the program's own digits. And `def m(self, a):` / `        return a` called as
// `c.m(1, 2, 3)` answered `1` at exit 0 where the reference raises `TypeError: C.m() takes 2 positional
// arguments but 4 were given`: the extra words were emitted and never read.
//
// The walk is the one the module-`def` road has paid since ADR 0166 — `callUserFunc`'s `provided`/`pos`
// loop in codegen_func.go, which writes one word per DECLARED parameter, fills each default the call
// omitted, binds each `name=` by name, and refuses the shape it cannot lay out. The method roads are
// separate roads (the static-receiver dispatch, `super()`, the run-time class-id switch, `C(...)`'s
// constructor call, `mod.fn(...)`), and each of them had read `c.Args` as the vector. The sentences the
// def road refuses with are reused verbatim rather than paraphrased, so one mis-shaped call says the
// same thing about itself whichever door it came to.
//
// A parameter the call omitted travels the identical word a written argument would have travelled: the
// default expression goes through the same `g.value` the road hands its own operands, so a text default
// interns the same way a text argument interns, a container default reaches the heap the same way, and a
// default the road could not have written is a default this road cannot fill either — a refusal, not a
// second guess.
//
// A default marks a parameter that may be OMITTED and says nothing about the parameters around it: the
// retired engine's own record states that rule in words, and the shape it states it with —
// `class C:` / `    def m(self, a, b=1, c):` / `        return a + b + c` / `z = C().m(1, 2, 3)` —
// answers `6`, because the default fills ONE slot and the positional list keeps counting (filling the
// defaulted slot with the next positional would have answered `3`).
//
// A nil `fd` means the callable has no declaration to read an arity from; the caller then keeps the
// words it wrote before this row, because filling a vector for a signature the compiler cannot see is
// the guess this file exists to stop.
func (cb *callArgBinder) callArgs(args []Expr, fd *FuncDef, skip int, surface string) ([]string, error) {
	if fd == nil {
		return nil, nil
	}
	n := len(fd.Params)
	vals := make([]string, n)
	provided := make([]bool, n)
	for i := 0; i < skip && i < n; i++ {
		// The receiver's slot belongs to the caller and is already written; marking it kept the
		// missing-argument walk below from trying to fill `self` out of a default of its own.
		provided[i] = true
	}
	pos := skip
	seenKw := false
	for _, a := range args {
		if kw, ok := a.(*KeywordArg); ok && kw != nil {
			seenKw = true
			idx := -1
			for i, p := range fd.Params {
				if i >= skip && p != nil && p.Name == kw.Name {
					idx = i
					break
				}
			}
			if idx < 0 {
				return nil, fmt.Errorf("codegen: unknown keyword argument %q for %s", kw.Name, surface)
			}
			if provided[idx] {
				return nil, fmt.Errorf("codegen: multiple values for argument %q of %s", kw.Name, surface)
			}
			v, err := cb.operand(kw.Value)
			if err != nil {
				return nil, err
			}
			vals[idx] = v
			provided[idx] = true
			continue
		}
		if seenKw {
			return nil, fmt.Errorf("codegen: positional argument after keyword argument for %s", surface)
		}
		if pos >= n {
			return nil, fmt.Errorf("codegen: too many arguments for %s", surface)
		}
		v, err := cb.operand(a)
		if err != nil {
			return nil, err
		}
		vals[pos] = v
		provided[pos] = true
		pos++
	}
	for i := skip; i < n; i++ {
		if provided[i] {
			continue
		}
		p := fd.Params[i]
		if p == nil || p.Default == nil {
			name := ""
			if p != nil {
				name = p.Name
			}
			return nil, fmt.Errorf("codegen: missing argument %q for %s", name, surface)
		}
		v, err := cb.operand(p.Default)
		if err != nil {
			return nil, err
		}
		vals[i] = v
	}
	out := make([]string, 0, n-skip)
	for i := skip; i < n; i++ {
		out = append(out, vals[i])
	}
	return out, nil
}

// methodCallArgs lays a call to the method `fd` over that method's parameters. Slot 0 is `self`, which
// arrives in the receiver word the caller already wrote, so the vector a caller appends to its call line
// is everything AFTER it. `surface` is the name the refusal calls the method by — the program's own
// spelling (`C.m`), per ADR 0166.
func (cb *callArgBinder) methodCallArgs(args []Expr, fd *FuncDef, surface string) ([]string, error) {
	if fd == nil {
		return nil, nil
	}
	return cb.callArgs(args, fd, 1, surface)
}

// plainCallArgs is the same walk for a callable with no receiver slot: an imported module function
// reached as `mod.fn(...)`, or a module function reached through the class-name spelling.
func (cb *callArgBinder) plainCallArgs(args []Expr, fd *FuncDef, surface string) ([]string, error) {
	return cb.callArgs(args, fd, 0, surface)
}

// operandWords renders an argument vector as the operand tail of a call line: `", i32 %r4"` for each
// word, in the order the callee declared them. Every road here writes its operands that way, and every
// argument is emitted BEFORE the call line is started, because a value that needs an instruction of its
// own (interning a string literal does) would otherwise be written into the middle of the operand list
// (Gap R.42).
func operandWords(words []string) string {
	var s strings.Builder
	for _, w := range words {
		s.WriteString(", i32 " + w)
	}
	return s.String()
}

// resolveMethodDef hands back the DECLARATION `resolveMethod` resolved the symbol for: the same
// base-class walk, in the same order, answering with the `*FuncDef` instead of the mangled name, so the
// arity, the parameter names and the defaults a call site needs are the ones the method was written with
// (roadmap Gap R.472, ADR 0434).
func (g *irGen) resolveMethodDef(className, mname string) *FuncDef {
	ci, ok := g.classInfos[className]
	if !ok || ci == nil {
		return nil
	}
	if fd := ci.methodDefs[mname]; fd != nil {
		return fd
	}
	for _, b := range ci.bases {
		if fd := g.resolveMethodDef(b, mname); fd != nil {
			return fd
		}
	}
	return nil
}
