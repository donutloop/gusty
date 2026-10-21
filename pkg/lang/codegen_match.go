// match: the pattern tests, the capture bindings an arm carries, and the class-chain question a class
// pattern asks (ADR 0161, ADR 0235).
package lang

import (
	"fmt"
	"strings"
)

// pendingMatchBind is one capture whose VALUE a pattern computed and whose BINDING the branch has
// not yet earned. roadmap Gap R.68, ADR 0358.
type pendingMatchBind struct {
	name string
	val  string
}

// bindPat records a capture: the slot is allocated where the pattern is lowered (so the alloca
// dominates the arm block the store lands in, and the register holding the value dominates too), and
// the store itself is deferred to the arm the branch takes.
//
// Deferring it is the whole of Gap R.68. `matchPattern` is called to decide WHICH arm runs — every
// arm's pattern is lowered, in order, before any arm's body — so a store written here runs for the
// arms that do not match as much as for the one that does. `match v: case Point(x): ... case 5:
// print(x)` therefore wrote the class check's `rt_inst_get` answer into `%_x` on the way to deciding
// that the subject is not a Point, marked the slot written while deciding it, and printed whatever
// word that read left behind where the reference raises NameError at the exit code of a handled trap.
// It was not a missing check: the flag, the alloca and the read-side test were all in place (ADR
// 0228) — the flag was simply being set by a pattern evaluation, which is not a binding.
//
// What this file does NOT decide is what the captured word MEANS. A slot of a container whose slots
// carry their own tags denotes a (payload, tag) pair, and the assignment road binds both words where
// the pattern road binds one — so `case [a]: print(a)` over `["eight"]` prints the `@str_tab` index
// where `a = xs[0]` prints `eight`. That is a different decision from this one and is filed with its
// measurement as roadmap Gap R.305; making a capture a pair also lands it in Gap R.146's territory, the
// positions that keep one word for an operand, which is why the two are not bundled.
func (g *irGen) bindPat(b *strings.Builder, name, val string) {
	if !g.allocd[name] {
		b.WriteString(fmt.Sprintf("  %%%s = alloca i32\n", "_"+name))
		g.allocd[name] = true
	}
	g.matchBinds = append(g.matchBinds, pendingMatchBind{name: name, val: val})
}

// emitArmCaptures writes the captures of the arm the branch just entered, and marks each one bound.
// It is called exactly once per arm, in the block the `br i1 <pattern>` on-line branches to, before
// the guard is lowered — the guard reads the captures, so this is the first position where they must
// answer (roadmap Gap R.68, ADR 0358).
//
// The store lands HERE and not while the pattern was being lowered, for the same reason the flag does:
// a pattern that does not fit the subject is a match that failed, not an error. What the reference does
// with a capture whose pattern DID match and whose guard then failed is keep the binding — measured —
// and that is why the store sits in front of the guard rather than after it.
func (g *irGen) emitArmCaptures(b *strings.Builder) {
	binds := g.matchBinds
	g.matchBinds = nil
	for _, pb := range binds {
		b.WriteString(fmt.Sprintf("  store i32 %s, i32* %%%s\n", pb.val, "_"+pb.name))
		g.markBound(b, pb.name)
	}
}

// matchCaptureNames is the ONE census of the positions this lowering hands to bindPat — the capture
// positions of one pattern. Three roads need the same answer and used to keep three: the checker,
// which decides which names are locals of the frame and definite on which path (semantic.go's
// matchPatternNames is this function), the eligibility rule that decides which names may carry a
// written-flag (boundflags.go), and matchPattern itself. A name one of them lists and another does not
// is either a silent zero (flag never set) or a spurious trap (flag set by a writer nobody hooked),
// so the census is written beside the emitter it describes and read by all three.
//
// It mirrors matchPattern arm for arm, including the two places the emitter treats `_` as a wildcard
// rather than a name.
func matchCaptureNames(p Expr) map[string]bool {
	out := map[string]bool{}
	capture := func(e Expr) {
		if nm, ok := e.(*Name); ok && nm.Value != "_" {
			out[nm.Value] = true
		}
	}
	switch pat := p.(type) {
	case nil:
	case *Name:
		capture(pat)
	case *ListLit: // a sequence pattern: each name element captures its position
		for _, e := range pat.Elems {
			capture(e)
		}
	case *DictLit: // a mapping pattern: each name value captures the entry
		for _, v := range pat.Vals {
			capture(v)
		}
	case *Call: // a class pattern: each name argument captures the attribute of that name
		for _, a := range pat.Args {
			capture(a)
		}
	}
	return out
}
func (g *irGen) hasBase(bases []string, class string) bool {
	for _, base := range bases {
		if base == class {
			return true
		}
		if g.classInfos[base] != nil && g.hasBase(g.classInfos[base].bases, class) {
			return true
		}
	}
	return false
}
func (g *irGen) classChainCond(b *strings.Builder, cid, class string) string {
	pid := g.classIDs[class]
	regs := []string{}
	t := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", t, cid, pid))
	regs = append(regs, t)
	for cname, ci := range g.classInfos {
		if cname == class {
			continue
		}
		if g.hasBase(ci.bases, class) {
			sc := g.classIDs[cname]
			st := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", st, cid, sc))
			regs = append(regs, st)
		}
	}
	cond := regs[0]
	for _, r := range regs[1:] {
		cond = g.orCond(b, cond, r)
	}
	return cond
}

// matchSubjectOperand lowers a `match` subject to the one word the pattern roads read: a heap HANDLE.
//
// A container literal whose every payload fits an i32 slot and whose slots need no tags folds to a
// compile-time global (`@.lst1`) rather than a heap object, and `value()` hands back that global's name.
// Every other reader of a folded literal is happy with that — an index reads the table at compile time —
// but the pattern roads ask the OBJECT questions through `@heap_kind`, `@heap_elems` and `@heap_tags`,
// whose address expressions take an i32 handle. Handing them the global emitted
// `call i32 @rt_heap_kind(i32 @.lst1)`, which is not a value of the right type at all: `llc` rejected the
// module and the run spent the contract's exit 2 (ADR 0166) on `match [1, 2]:` and, since ADR 0361 put
// parentheses through the same road, on `match (5,):`. Measured on both shapes at the head of this
// branch: the list subject was already broken, and the tuple subject reached the same door once the tuple
// existed. The fix is one door rather than two patches: a subject that would fold is materialised to the
// heap here, so the pattern roads only ever see a handle (roadmap Gap R.311, ADR 0166).
func (g *irGen) matchSubjectOperand(b *strings.Builder, e Expr) (string, error) {
	switch n := e.(type) {
	case *ListLit:
		if !g.literalNeedsHeap(n) && !g.literalNeedsTags(n) && !g.literalHasPairElement(n) {
			return g.heapListFrom(b, n, "")
		}
	case *DictLit:
		// A dict of integer constants folds to the static {count, keys, vals} global, so a `match` over
		// one needs the same treatment as a list subject.
		return g.heapDictFrom(b, n, "")
	case *SetLit:
		return g.heapSetFrom(b, n, "")
	}
	return g.value(b, e)
}

// heapKindInCond answers "is the subject one of these heap kinds?" with an i1. A pattern that reads a
// container through @heap has to ask this before it asks anything of the container: the length word, the
// pair table and the element array are the same few words at different offsets, so a kind the pattern was
// not written for answers with a plausible number rather than a refusal (roadmap Gap R.306, ADR 0358).
func (g *irGen) heapKindInCond(b *strings.Builder, sub string, kinds []int32) string {
	kind := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_heap_kind(i32 %s)\n", kind, sub))
	var cond string
	for _, kd := range kinds {
		c := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", c, kind, kd))
		if cond == "" {
			cond = c
		} else {
			cond = g.orCond(b, cond, c)
		}
	}
	if cond == "" {
		return "false"
	}
	return cond
}

// matchPattern answers "does this pattern match the subject?" with an i1 value, and binds the
// capture names it matched. The answer is a truth value in LLVM's own vocabulary: `true`/`false`
// fold in andCond/orCond instead of reaching `br i1` as an integer (ADR 0235).
func (g *irGen) matchPattern(b *strings.Builder, sub string, pat Expr) (string, error) {
	switch p := pat.(type) {
	case *Name:
		if p.Value == "_" {
			cmp := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cmp, sub, sub))
			return cmp, nil
		}
		// Bare-name capture pattern: bind the subject to a fresh variable slot and always match
		// (Python `case x:` semantics).
		g.bindPat(b, p.Value, sub)
		return "true", nil
	case *ListLit:
		n := len(p.Elems)
		// A sequence pattern is a KIND test before it is a length test. @heap keeps a dict's entry
		// count in the same word a list keeps its element count, so `case [a]` over a one-entry dict
		// read `rt_list_len`'s answer as "one element", took the arm, and captured whatever word slot
		// 0 happened to hold — where the reference fails the arm (`match {"k": 1}: case [a]: …` prints
		// the wildcard arm's branch). The length was never the question; `is this a sequence` was.
		// (roadmap Gap R.306; the reference accepts list and set, not dict, str or int.)
		cond := g.heapKindInCond(b, sub, []int32{HeapKindList, HeapKindSet})
		ln := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_list_len(i32 %s)\n", ln, sub))
		lc := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", lc, ln, n))
		cond = g.andCond(b, cond, lc)
		for i, e := range p.Elems {
			er := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_get_elem(i32 %s, i32 %d)\n", er, sub, i))
			if nm, ok := e.(*Name); ok && nm.Value != "_" {
				g.bindPat(b, nm.Value, er)
				continue
			}
			ev, err := g.value(b, e)
			if err != nil {
				continue
			}
			ec := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", ec, er, ev))
			cond = g.andCond(b, cond, ec)
		}
		return cond, nil
	case *DictLit:
		// The mapping pattern's twin: `case {"k": v}` must first establish the subject HAS entries,
		// because `rt_dict_has` over a list reads the key/value array as a pair table and a miss
		// becomes a word, not a refusal (roadmap Gap R.306, ADR 0358).
		cond := g.heapKindInCond(b, sub, []int32{HeapKindDict})
		for i, k := range p.Keys {
			kv, err := g.value(b, k)
			if err != nil {
				continue
			}
			hs := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_dict_has(i32 %s, i32 %s)\n", hs, sub, kv))
			hc := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", hc, hs))
			cond = g.andCond(b, cond, hc)
			v := p.Vals[i]
			vr := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = call i32 @rt_dict_get(i32 %s, i32 %s)\n", vr, sub, kv))
			if nm, ok := v.(*Name); ok && nm.Value != "_" {
				g.bindPat(b, nm.Value, vr)
				continue
			}
			vv, err := g.value(b, v)
			if err != nil {
				continue
			}
			vc := g.newTmp()
			b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", vc, vr, vv))
			cond = g.andCond(b, cond, vc)
		}
		return cond, nil
	case *Call:
		// Class pattern — `case Point(x, y):` or `case Alias(x, y):` — or an ordinary call compared
		// to the subject. Which class the callee names is the front end's question, asked once for
		// both backends (ADR 0235). The old code asked it twice and got it wrong twice: a name that
		// was not a declared class went down a "runtime alias" branch that read it with g.value and
		// DISCARDED the error, so `case f():` emitted `load i32, i32* %_f` for a variable that does
		// not exist and `case Alias(...)` inside a function emitted `icmp eq i32 %t5, ` with nothing
		// after the comma. Both were exit 2 (ADR 0211).
		fn, isName := p.Fn.(*Name)
		if !isName {
			return g.matchEquality(b, sub, pat)
		}
		class := g.classPat.classOfName(fn.Value)
		if class == "" && (g.classIDs[fn.Value] != 0 || g.classInfos[fn.Value] != nil) {
			class = fn.Value
		}
		if class != "" {
			return g.matchInstance(b, sub, class, "", p.Args)
		}
		// Not a class name. It may still name a value that holds one (`Alias = Point` written where
		// the generator cannot see it), which is the one form that has to be asked at run time — but
		// only if the name is readable and is not a function, and a refusal to read it falls through
		// to expression-equality rather than emitting a half-built compare.
		if !g.funcs[fn.Value] && !isPredeclaredName(fn.Value) {
			if cv, err := g.value(b, fn); err == nil {
				return g.matchInstance(b, sub, "", cv, p.Args)
			}
		}
		return g.matchEquality(b, sub, pat)
	default:
		return g.matchEquality(b, sub, pat)
	}
}

// matchEquality is where every non-structural pattern ends: evaluate the pattern expression and
// compare it to the subject. A pattern that cannot be evaluated matches nothing — the next case is
// tried, which is what the record's `return false, nil` says for the same shape.
//
// A container pattern is not an i32 and never was: `[1, 2]`, `{1: "a"}` and `{1, 2}` fold to a
// compile-time global when their payloads fit a slot, and comparing the subject's HANDLE against that
// global's address emitted `icmp eq i32 %t1, @.set1` — a global where a pointer is required — which
// `llc` rejected, spending the contract's exit 2 (ADR 0166) on `match {1, 2}: case {1, 2}:`. Measured at
// the head of this branch: the set shape was already broken that way, and a tuple subject reached the
// same door once ADR 0361 gave `match (5,):` a subject to lower. Both sides now come from the heap and
// the answer comes from the door that compares contents — `rt_container_eq`, the same one `==` between two
// containers asks — so a pattern matches what it holds rather than where it was allocated
// (roadmap Gap R.311, Gap R.312, ADR 0166).
func (g *irGen) matchEquality(b *strings.Builder, sub string, pat Expr) (string, error) {
	switch p := pat.(type) {
	case *ListLit:
		pv, err := g.heapListFrom(b, p, "")
		if err != nil {
			return "false", nil
		}
		return g.matchContainerEquality(b, sub, pv)
	case *DictLit:
		pv, err := g.heapDictFrom(b, p, "")
		if err != nil {
			return "false", nil
		}
		return g.matchContainerEquality(b, sub, pv)
	case *SetLit:
		pv, err := g.heapSetFrom(b, p, "")
		if err != nil {
			return "false", nil
		}
		return g.matchContainerEquality(b, sub, pv)
	}
	pv, err := g.value(b, pat)
	if err != nil {
		return "false", nil
	}
	cmp := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cmp, sub, pv))
	return cmp, nil
}

// matchContainerEquality asks the content door, and turns its 0/1 into the i1 the arm dispatch reads.
func (g *irGen) matchContainerEquality(b *strings.Builder, sub, pv string) (string, error) {
	raw := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_container_eq(i32 %s, i32 %s)\n", raw, sub, pv))
	cmp := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", cmp, raw))
	g.markI1(cmp)
	return cmp, nil
}

// matchInstance lowers a class pattern. The subject must be an instance whose class is `class` (or a
// subclass of it) — or, when `class` is empty, whose class id equals the run-time value `classVal` —
// and every capture name must name an attribute the instance actually has. That last conjunct is the
// half that was missing: @heap's data words cannot tell an attribute that was never written from a
// stored 0, so `case Point(a, b):` matched an instance carrying neither and printed `pt 0 0` while
// the record and the documentation both say the pattern fails (roadmap Gap B, ADR 0235).
func (g *irGen) matchInstance(b *strings.Builder, sub, class, classVal string, args []Expr) (string, error) {
	kind := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_heap_kind(i32 %s)\n", kind, sub))
	kc := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %d\n", kc, kind, HeapKindInstance))
	cond := kc
	cid := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 0)\n", cid, sub))
	if class != "" {
		cond = g.andCond(b, cond, g.classChainCond(b, cid, class))
	} else {
		cc := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp eq i32 %s, %s\n", cc, cid, classVal))
		cond = g.andCond(b, cond, cc)
	}
	for _, arg := range args {
		nm, ok := arg.(*Name)
		if !ok {
			// Only a name is a capture position; anything else is a sub-pattern this form cannot
			// match, and the interpreter fails the case the same way.
			return "false", nil
		}
		slot := g.attrSlot(nm.Value)
		has := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_has(i32 %s, i32 %d)\n", has, sub, slot))
		hc := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", hc, has))
		cond = g.andCond(b, cond, hc)
		ar := g.newTmp()
		b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 %d)\n", ar, sub, slot))
		g.bindPat(b, nm.Value, ar)
	}
	return cond, nil
}

// stmtMatch is the road a `match` takes: the subject once, the pattern tests the matcher in
// this file answers, the arm's captures bound before its body runs.
func (g *irGen) stmtMatch(b *strings.Builder, st Stmt, n *MatchStmt) error {
	sub, err := g.matchSubjectOperand(b, n.Subject)
	if err != nil {
		return err
	}
	endL := g.newLabel("match.end")
	for i, c := range n.Cases {
		bodyL := g.newLabel("match.case")
		fallL := endL
		if i < len(n.Cases)-1 {
			fallL = g.newLabel("match.next")
		}
		// The captures of the arm that runs are written in the block the branch enters, and only by
		// that arm: deciding which arm matches is not binding anything (Gap R.68, ADR 0358).
		savedBinds := g.matchBinds
		g.matchBinds = nil
		patterns := append([]Expr{c.Pattern}, c.Or...)
		if len(patterns) == 1 {
			pc, perr := g.matchPattern(b, sub, patterns[0])
			if perr != nil {
				return perr
			}
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", pc, bodyL, fallL))
			b.WriteString(fmt.Sprintf("%s:\n", bodyL))
			// The arm is the one that matched: this is where its captures bind.
			g.emitArmCaptures(b)
		} else {
			// An arm written `case p0 | p1 | …` is decided one alternative at a time, and each
			// alternative enters the arm's body carrying ITS captures. Folding the alternatives into one
			// `or i1` and writing every alternative's captures made a name bound by the alternative that
			// did not match answer the value the other one computed — `case [a] | {"k": a}:` over [3]
			// printed the interred `k`, because the mapping pattern's `rt_dict_get` ran last and won.
			// One arm block cannot hold that, since the stores differ per alternative.
			for k, p := range patterns {
				altFalse := g.newLabel("match.alt")
				last := k == len(patterns)-1
				if last {
					altFalse = fallL
				}
				pc, perr := g.matchPattern(b, sub, p)
				if perr != nil {
					return perr
				}
				enterL := g.newLabel("match.alt.enter")
				b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", pc, enterL, altFalse))
				b.WriteString(fmt.Sprintf("%s:\n", enterL))
				g.emitArmCaptures(b) // this alternative's captures, and no other's
				b.WriteString(fmt.Sprintf("  br label %%%s\n", bodyL))
				if !last {
					b.WriteString(fmt.Sprintf("%s:\n", altFalse))
				}
			}
			b.WriteString(fmt.Sprintf("%s:\n", bodyL))
		}
		g.matchBinds = savedBinds
		if c.Guard != nil {
			gok, gerr := g.truthyValue(b, c.Guard)
			if gerr != nil {
				return gerr
			}
			bodyAfter := g.newLabel("match.case.body")
			b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", gok, bodyAfter, fallL))
			b.WriteString(fmt.Sprintf("%s:\n", bodyAfter))
		}
		for _, s := range c.Body {
			if err := g.stmt(b, s); err != nil {
				return err
			}
		}
		b.WriteString(fmt.Sprintf("  br label %%%s\n", endL))
		if i < len(n.Cases)-1 {
			b.WriteString(fmt.Sprintf("%s:\n", fallL))
		}
	}
	b.WriteString(fmt.Sprintf("%s:\n", endL))
	return nil
}
