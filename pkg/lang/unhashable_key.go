package lang

import (
	"fmt"
	"strings"
)

// Keys the language cannot hash.
//
// A dict's key is asked one question — is this the entry? — and answering it needs a hash. Lists,
// dicts and sets are mutable, so the language refuses to hash them, and CPython says so at the
// moment the key is used:
//
//	print({"a": 1}[[1]])      # TypeError: unhashable type: 'list'
//	print({"a": 1}[{1: 2}])   # TypeError: unhashable type: 'dict'
//	print({"a": 1}[{1}])      # TypeError: unhashable type: 'set'
//	print({"a": 1}[(1, [2])]) # TypeError: unhashable type: 'list'
//	print([1] in {"a": 1})    # TypeError: unhashable type: 'list'
//
// The compiled program answered none of those. A lookup compares (payload, tag) pairs, and a
// container key — a heap handle under a container tag — simply never matches an int or an interned
// string, so the door that raises KeyError for a missing entry reported a missing entry:
// `KeyError: key not found`, exit 3, where the reference raises TypeError. Worse, the membership
// door answered `[1] in {"a": 1}` with `False` at exit 0: a wrong answer to a question the
// reference does not allow to be asked (roadmap Gap R.217).
//
// The fix is a question asked of the program at the two doors that consult a dict — the subscript
// read and the membership test — before either consults the dict: is the key something the
// language refuses to hash? The kind is read from what the source WROTE (a list, dict or set
// literal; a tuple carrying one; a name the program most recently assigned one of), the same
// "front end reads the program, the runtime renders" standing every other message in this file
// has (ADR 0257). The raise names the innermost unhashable type — the tuple case says 'list', the
// way the reference walks a tuple's elements and names the first one it cannot hash.
//
// What this is NOT: a hash function, or a runtime tag check. Where the key's kind is only a
// run-time fact — a name the compiler cannot pin to one container — the door keeps the answer it
// always gave, rather than raise a TypeError the program may not deserve. Naming that case needs
// the key's kind to travel with the key, which is roadmap L11.1's tagged value word (ADR 0232's
// soundness rule), and that row is where it stays.

// unhashableKeyName asks whether `e`, used as a dict key or a set-membership needle, is a value
// the language refuses to hash. It answers the type name the reference puts in its sentence.
func (g *irGen) unhashableKeyName(e Expr) (string, bool) {
	switch k := e.(type) {
	case nil:
		return "", false
	case *ListLit:
		// A tuple is a container the language *does* hash — but only when every element is hashable,
		// and the reference walks the elements left to right and names the first it cannot hash. The
		// tuple's own spelling is the `Tuple` flag (roadmap L11.3, ADR 0361), because the compiled
		// backend gives both spellings the one container layout.
		if k.IsTuple() {
			for _, el := range k.Elems {
				if tn, bad := g.unhashableKeyName(el); bad {
					return tn, true
				}
			}
			return "", false
		}
		return "list", true
	case *DictLit:
		return "dict", true
	case *SetLit:
		return "set", true
	case *Tuple:
		// The reference hashes a tuple by hashing its elements left to right, and names the
		// first element it cannot hash — `(1, [2], {3})` says 'list', not 'tuple'.
		for _, el := range k.Elems {
			if tn, bad := g.unhashableKeyName(el); bad {
				return tn, true
			}
		}
		return "", false
	case *Name:
		// A name the program most recently filled with a container. These are the same maps
		// the print door and the membership door already trust for a container's own kind.
		if g.tupleVars != nil && g.tupleVars[k.Value] {
			// The name holds a tuple: hashable unless an element is not, which is a question the
			// literal the binding kept can answer (roadmap L11.3, ADR 0361).
			if lit, has := g.containerLits[k.Value]; has {
				if ln, isList := lit.(*ListLit); isList {
					for _, el := range ln.Elems {
						if tn, bad := g.unhashableKeyName(el); bad {
							return tn, true
						}
					}
				}
			}
			return "", false
		}
		switch {
		case g.listVars[k.Value]:
			return "list", true
		case g.runtimeDicts[k.Value]:
			return "dict", true
		case g.runtimeSets[k.Value]:
			return "set", true
		}
		return "", false
	default:
		return "", false
	}
}

// The kinds a hashing haystack refuses to hash, and the one pair that escapes the refusal.
//
// roadmap **Gap R.586** · **ADR 0425** (the removal door's measurement of the same pair) · **ADR 0328**
// (the rule this table serves) · **ADR 0232** (one question, one table)
//
// Three doors ask "may this value be hashed?": the subscript/store doors, which ask it of a KEY, the
// membership gate in this file, which asks it of a needle, and `guardHashableNeedle`, which asks it of a tag
// register once the needle arrived as a (payload, tag) pair. They are one rule about one set of values, so
// they read one table — and the table has one parameter, because the haystack, not the needle, decides it.
//
//	print({1} in {1, 2})      # False   in the reference;  this toolchain: TypeError, exit 3
//	print({1} in {1: 2})      # TypeError: unhashable type: 'set'
//	print([1] in {1, 2})      # TypeError: unhashable type: 'list'
//	print((1, {2}) in {1, 2}) # TypeError: unhashable type: 'set'
//
// The first line is not an absence the backend invented: CPython's `set_contains` (and `set_discard`,
// `set_remove`) converts a set or frozenset needle to the **frozenset it hashes as** and looks THAT up, so
// the needle is never asked for its own hash and no TypeError can follow. This language has no `frozenset`
// to find — a set cannot hold one, and `set()`/a set literal are the only spellings of the empty one — so
// the conversion answers "no member matches", which is the reference's own `False`. A dict takes no such
// conversion: `dict.__contains__` asks the key's own hash, so the same needle raises; and a needle that
// merely CARRIES a set (a tuple with a set in it) is hashed element-wise, so it raises too. The exemption
// is therefore exactly the pair — the needle's OWN kind is a set, and the haystack's kind is a set.
//
// What the exemption is NOT: a hashability claim about run-time kinds. It fires only where the PROGRAM
// settles both sides (`programPromisesASet`), which is where `unhashableKeyName` already had the kinds to
// begin with; a needle or a haystack whose kind arrives as a tag register is asked by `guardHashableNeedle`
// off this same table.
type unhashableNeedleKind struct {
	tag  int32
	name string
}

// unhashableNeedleKinds is the table those three doors read. `setHaystack` is the answer to the frozenset
// question — true for a set, which converts a set needle and answers; false for a dict key, which asks the
// key's own hash and so refuses the set as well as the list and the dict.
func unhashableNeedleKinds(setHaystack bool) []unhashableNeedleKind {
	kinds := []unhashableNeedleKind{{int32(TagList), "list"}, {int32(TagDict), "dict"}}
	if !setHaystack {
		kinds = append(kinds, unhashableNeedleKind{int32(TagSet), "set"})
	}
	return kinds
}

// setHaystackConvertsTheNeedle is the exemption itself, asked of the PAIR the door is about to hash.
//
// The haystack is `nil` at the doors that have no haystack — a dict subscript read and a dict store, where
// the expression is a KEY and the dict is the thing being hashed into — and only a set converts. Both sides
// are asked of the PROGRAM, not of a notebook: `programPromisesASet` (membership_kind.go) is the door for
// "this expression denotes a set, and no run-time path can make that false", and it is what keeps
//
//	s = {"a": 1}
//	if no():
//	    s = {1, 2}
//	print({1} in s)   # the run-time haystack is the dict; the reference raises
//
// answering with a `False` it had no right to. A skipped raise at exit 0 is the trade **ADR 0452** and
// **ADR 0453** refuse; a name the binder walk cannot settle therefore keeps today's raise, which is filed
// as owed rather than guessed at.
func (g *irGen) setHaystackConvertsTheNeedle(needle, haystack Expr, readLine int) bool {
	if haystack == nil || g == nil {
		return false
	}
	return g.programPromisesASet(needle, readLine) && g.programPromisesASet(haystack, readLine)
}

// unhashableKeyGate emits the refusal at a dict door. It runs BEFORE the door's own membership
// test, so the key's kind is settled before anyone is told a key is missing, and it is emitted on
// the same non-local road every other exception in this backend takes — `try:`/`except TypeError:`
// catches it, an uncaught one exits 3 with the reference's own two lines. It ends the block the
// raise branched out of and opens the continuation, so the caller returns the (unreachable) value
// the door would have answered and keeps emitting where it left off.
//
// `haystack` is what the key is being hashed INTO: `nil` at the two subscript/store doors, and the right
// operand at the membership door, because the haystack decides which needle kinds it refuses (roadmap
// Gap R.586 — `print({1} in {1, 2})` answers, `print({1} in {1: 2})` raises, and one gate answers both).
func (g *irGen) unhashableKeyGate(b *strings.Builder, keyExpr Expr, haystack Expr, sp Span) (string, bool) {
	typeName, bad := g.unhashableKeyName(keyExpr)
	if !bad {
		return "", false
	}
	if g.setHaystackConvertsTheNeedle(keyExpr, haystack, sp.Line) {
		// The haystack converts this needle to the frozenset it hashes as and answers; the door below it
		// asks the members, finds no match, and the program continues (roadmap Gap R.586).
		return "", false
	}
	g.heapUsed = true
	g.raiseTo(b, exnCode("TypeError"), "TypeError", unhashableKeyMessage(typeName), sp)
	cont := g.newLabel("uhk.cont")
	b.WriteString(fmt.Sprintf("%s:\n", cont))
	return "0", true
}

// unhashableKeyMessage words the sentence the reference words. CPython's message for an
// unhashable key names the type in single quotes with no class prefix of its own:
// `TypeError: unhashable type: 'list'`.
func unhashableKeyMessage(typeName string) string {
	return "unhashable type: '" + typeName + "'"
}

// unhashableKeyRefusal is the compile-time refusal for the doors that cannot even build the
// lookup: a key whose kind the compiler must know to lower the read at all, and does not have.
// It names the same construct, so a reader who meets the refusal and a reader who meets the trap
// are reading about one language rule (Gap R.212's door).
func unhashableKeyRefusal(what, typeName string) string {
	return "a " + what + " cannot be a dict key: the reference refuses to hash it (" +
		unhashableKeyMessage(typeName) + ") — roadmap Gap R.217, and the row that owns the key's kind " +
		"travelling with the key is L11.1 (ADR 0232)"
}

// dictVarHasKind reports that the compiler knows the name holds a dict — the evidence the
// unhashable-key gate needs before it refuses a store, so a list subscript (`xs[[0]] = 1`) keeps
// the sentence its own door gives rather than being told about hashing.
func (g *irGen) dictVarHasKind(name string) bool {
	return g.runtimeDicts[name] || g.mixedDicts[name] || g.dictKeyStr[name] || g.dictKeyInt[name]
}

// haystackHashesKeys reports whether a membership test `needle in haystack` is going to hash the
// needle. Only a dict's keys and a set's elements do — a list and a tuple answer membership by
// walking, so `[1] in [[1], [2]]` is True in the reference and must stay True here. The gate that
// refuses an unhashable key therefore fires only where the language actually asks for a hash; the
// container kinds the compiler can name come from the same maps the membership door itself
// dispatches on, and a haystack whose kind it cannot name keeps the test it always ran (a missing
// refusal is a gap; refusing a legal test would be a wrong answer).
func (g *irGen) haystackHashesKeys(e Expr) bool {
	switch h := e.(type) {
	case nil:
		return false
	case *DictLit, *SetLit:
		return true
	case *ListLit, *Tuple:
		return false
	case *Name:
		if g.listVars[h.Value] {
			return false
		}
		return g.dictVarHasKind(h.Value) || g.runtimeSets[h.Value] || g.mixedSets[h.Value]
	default:
		return false
	}
}
