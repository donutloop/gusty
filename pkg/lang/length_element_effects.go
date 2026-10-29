// length_element_effects.go — the count door PERFORMS the elements whose count it takes
// (roadmap Gap R.494, ADR 0441).
//
// `len([1, 2, 3])` is a number the spelling already states, so the length door answers it from
// the element list and never builds the container. That is an optimisation, and like ADR 0192's
// comprehension fold it is not the meaning: the reference evaluates every element expression,
// builds the container, and only then asks the object how many slots it holds. An element is not
// only a value in this backend — it is an operation. It can call a function that prints, read a
// slot whose accident the subscript door raises, divide by zero, or run a comprehension that
// loops. When the count door skipped the elements it also skipped all of that, and skipped it
// QUIETLY: measured off `94a1458`, every one of these answered at the exit code of success.
//
//	print(len([f()]))                    wrote `1`                  reference: `side` then `1`
//	print(len([[1, 2][0][0], 1]))         wrote `2`                  reference: TypeError: 'int' object is not subscriptable
//	print(len([1 // 0]))                  wrote `1`                  reference: ZeroDivisionError: integer division or modulo by zero
//	print(len([[1, 2][5]]))               wrote `1`                  reference: IndexError: list index out of range
//
// The rule is the one ADR 0275 already wrote for the two operators that choose an operand
// (Gap R.149): *the operand the test did not choose is still evaluated*. Here the operand is
// every element of a container whose length the compiler read off its spelling, and "did not
// choose" is "threw the object away". So the door asks each element, in the order the reference
// evaluates them, one pure question — is this element an operation, or a spelling? — and lowers
// the operations and nothing else. A literal of constants therefore stays exactly the constant
// it was: no builder, no allocation, no mark left in the module, which is the row's own negative
// control (`len([1, 2, 3])` still answers `3` with no builder in the module).
//
// Two rules keep it a door and not a broom.
//
//   - It builds into a SCRATCH buffer and commits only when every element lowered (the discipline
//     `chain_operand.go` and `answer_kind.go` exist for). A half-lowered element left in the
//     builder would be an instruction after a terminator, and the compiler's own module is the
//     one place the exit-code contract refuses to spend ADR 0166's exit 2.
//   - An element this backend cannot lower leaves the count the door answers TODAY. Refusing a
//     program that has always answered `3` would trade a pinned, owned divergence for a new
//     unowned one; the shape that cannot be lowered stays the OTHER road's refusal, which names
//     its own missing half.
//
// Performing an element is itself two roads, and the second one is not this file's invention: an
// element whose callee the container-answer scan names has no ONE-WORD answer at all (**Gap R.67**), so
// the value road below cannot lower it and used to leave the element unperformed behind a swallowed
// refusal. Such an element is lowered by `containerOperand` — the door the print road uses for the same
// shape (**ADR 0333**) — and its handle is dropped where it dies, rooted by nobody because nothing reads
// it after the call (**roadmap Gap R.534**, **ADR 0460**).
//
// What this file deliberately does NOT reach: a container whose LENGTH is the object's own fact —
// a comprehension the run time builds (`g.rtComps`) is a heap object and its length is a word
// inside it, which is a different door and its own row.

package lang

import "strings"

// literalElementAsksToBeRun is the pure gate in front of the count door: does this element of a
// spelled-out container do something, or is it merely written? Inert — a spelling the reference
// evaluates to itself with no output and no raise — is a number, a text, a verdict, the void, a
// name (whose read is a load of a slot the checker proved was written: undefined names are a
// front-end error, so it cannot raise the reference's NameError from here), or a container
// literal all of whose parts are inert in turn. Everything else is an operation: a call prints
// and raises, a subscript raises, arithmetic raises, a comprehension loops.
//
// A nested container of inert parts is inert, so the count door never builds an object merely to
// throw it away; the allocation the reference performs for it has no answer any program can read,
// and building it would put a builder into a module whose only job is to state a number.
func literalElementAsksToBeRun(e Expr) bool {
	switch n := e.(type) {
	case nil:
		return false
	case *IntLit, *FloatLit, *StrLit, *BoolLit, *NoneLit, *Name:
		return false
	case *ListLit:
		for _, el := range n.Elems {
			if literalElementAsksToBeRun(el) {
				return true
			}
		}
		return false
	case *SetLit:
		for _, el := range n.Elems {
			if literalElementAsksToBeRun(el) {
				return true
			}
		}
		return false
	case *Tuple:
		for _, el := range n.Elems {
			if literalElementAsksToBeRun(el) {
				return true
			}
		}
		return false
	case *DictLit:
		for _, k := range n.Keys {
			if literalElementAsksToBeRun(k) {
				return true
			}
		}
		for _, v := range n.Vals {
			if literalElementAsksToBeRun(v) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

// literalElementsAskToBeRun is the same question of a whole element list — asked before the door
// touches a builder, so a container of constants costs the module nothing.
func literalElementsAskToBeRun(elems []Expr) bool {
	for _, el := range elems {
		if literalElementAsksToBeRun(el) {
			return true
		}
	}
	return false
}

// lowerLiteralElementsForEffect performs, in source order, every element of a spelled-out
// container whose LENGTH the caller is about to answer from the spelling. The words the elements
// compute are thrown away — the count is the answer — but what they DO is the program's, and the
// program performed it.
//
// Elements that are spellings are not lowered at all: `len([1, 2, 3])` emits no instruction here,
// which is the negative control this row pins. An element that cannot be lowered abandons the
// whole attempt rather than committing half of it, and the door keeps the count it answers today.
func (g *irGen) lowerLiteralElementsForEffect(b *strings.Builder, elems []Expr) {
	if !literalElementsAskToBeRun(elems) {
		return
	}
	var scratch strings.Builder
	for _, el := range elems {
		if err := g.lowerLiteralElementForEffect(&scratch, el); err != nil {
			// An element with no lowering is some other road's refusal to make, in that road's
			// own words; the count door does not get to turn an answered program into a refused one.
			// Nothing of this container is committed — not its first elements either — because a
			// half-performed container is a program no one wrote.
			return
		}
	}
	b.WriteString(scratch.String())
}

// lowerLiteralElementForEffect performs one element of a spelled-out container for its effects.
//
// A container literal is NOT asked of `value`: the element road a container takes is chosen from
// what it HOLDS (a static `{i32, [N x i32]}` global when every element reads as an int, a heap
// object otherwise), and `len` is answering a question about the container's SHAPE, so the door
// must not depend on that choice — `[f(), 2]`, whose call the checker reads as an int, takes the
// static element road, which refuses anything but an `IntLit` outright (and the caller, keeping
// its count, answers `2` with no `side` printed — the same wrong answer the row filed). The door
// therefore goes through the container to its PARTS and performs those, building nothing: the
// object the reference built for the nested container has no answer any program can read once the
// container holding it is itself thrown away for a count.
//
// A call that hands back a CONTAINER is performed at the door that READS such an answer, never at
// the one-word value road (**roadmap Gap R.534**, **ADR 0460**). No word in this backend holds a
// container, so `g.value(f())` refuses in **Gap R.67**'s own words — and this door, which needs no
// word at all, used to swallow that refusal and answer the count anyway: `print(len([probe()]))`
// wrote `1` where the reference writes `built` then `1`, at the exit code of success, with the
// call's print, its raise and its arithmetic all unscored. The refusal is about a word the position
// must keep; the count position keeps nothing, so the element is lowered by `containerOperand` —
// the same `*Call` arm **ADR 0333** grew for `print([build(), 3])`, **ADR 0344** narrowed to the
// positions that can hold a handle — and its handle is dropped on the floor here. Asking the door
// rather than writing a third call road is **ADR 0232** at this altitude: one answer, one door, and
// the argument-carriage and answer-scan questions stay the ones that road already asks.
//
// What that arm declines — an answer no scan named, an argument the pair road owns — leaves the
// whole attempt, and the door keeps the count it answers today: a pinned, owned divergence beats a
// new unowned one (**ADR 0441**'s rule, kept). The pair door is deliberately NOT tried for an
// argument that crosses as a (payload, tag) pair: measured on this row, asking `pairCallPair` about
// `f(g())` — a float-answering call in an argument — emitted `call i32 @gy_f(i32 %t10)` over
// `%t10 = fadd double 0.0, 2.5` and spent **ADR 0166**'s exit 2 on a program whose only crime was
// taking a count. The shapes that stay wrong stay filed (**Gap R.542**, beside **Gap R.533** and
// **Gap R.536**).
func (g *irGen) lowerLiteralElementForEffect(b *strings.Builder, el Expr) error {
	if !literalElementAsksToBeRun(el) {
		return nil
	}
	switch n := el.(type) {
	case *ListLit:
		for _, sub := range n.Elems {
			if err := g.lowerLiteralElementForEffect(b, sub); err != nil {
				return err
			}
		}
		return nil
	case *SetLit:
		for _, sub := range n.Elems {
			if err := g.lowerLiteralElementForEffect(b, sub); err != nil {
				return err
			}
		}
		return nil
	case *Tuple:
		for _, sub := range n.Elems {
			if err := g.lowerLiteralElementForEffect(b, sub); err != nil {
				return err
			}
		}
		return nil
	case *DictLit:
		for i, k := range n.Keys {
			if err := g.lowerLiteralElementForEffect(b, k); err != nil {
				return err
			}
			if err := g.lowerLiteralElementForEffect(b, n.Vals[i]); err != nil {
				return err
			}
		}
		return nil
	case *Call:
		// The answer's shape is not this position's business: the count door keeps no word, so the
		// only reason the value road refuses a container-answering call — that a container's word is a
		// heap object (**Gap R.67**) — has nothing to bite on here. Ask the door that reads such an
		// answer for the SIDE of performing the call, and throw the handle away (**Gap R.534**).
		//
		// The trial costs the module nothing: `containerOperand` runs in its own scratch and is
		// committed only when it answers, so a refusal leaves no half-emitted instruction behind for
		// the ordinary road below (**ADR 0273**), and nothing here re-decides whether the callee's
		// answer is a container — `containerOperand`'s `*Call` arm is the one place that question is
		// asked (**ADR 0333**, **ADR 0344**).
		if nm, isName := n.Fn.(*Name); isName {
			if _, named := g.containerAnswerOf(nm.Value); named {
				var inner strings.Builder
				if _, err := g.containerOperand(&inner, n); err == nil {
					b.WriteString(inner.String())
					return nil
				}
			}
		}
		_, err := g.value(b, el)
		return err
	default:
		_, err := g.value(b, el)
		return err
	}
}
