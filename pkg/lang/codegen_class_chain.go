package lang

import (
	"fmt"
	"strings"
)

// ─── L12.4: the class OBJECT, and the walk every attribute read takes through it ─────────────────────
//
// The reference builds a class by executing its body, and an attribute read on an instance is not a read of
// the instance alone: the reference looks in the instance's `__dict__` first and then walks the class and
// EVERY base of it in method-resolution order. This backend did the first half of that story and dropped the
// second twice over. (1) `class C:` / `X = 1` / `print(C().X)` printed `0` at the exit code of success — the
// read reached `@rt_inst_get` on a slot nothing had written, because a class body's bare assignments had been
// recorded in the class's own table (ADR 0418 paid that debt for reads through the CLASS NAME) but no instance
// read ever asked that table. (2) `class Sub(Base)` / `print(Sub.NAME)` was refused in words where the
// reference answers, because the class-name road looked only at the class's own body. Roadmap **Gap R.56**
// ("class-level attributes do not exist") and **Gap R.66** ("attribute lookup stops at the first base") are
// the two halves, and row **L12.4** owns them.
//
// So this file holds the two questions every attribute read now asks:
//
//   - `classLinearization` — WHICH classes, in which order. The reference uses C3, and the order is not
//     decoration: `class D(B, C)` where `B` and `C` both derive from `A` and only `A` binds the member must
//     find `A` once, after `B` and `C`, and `class C(A, B)` where both bind it must find `A`'s answer. A
//     first-base-only walk gets the diamond wrong and a depth-first-with-dupes walk gets the sibling order
//     wrong, so the merge is written out rather than approximated.
//   - `classChainMember` — WHAT the first class in that order binds, distinguishing a member this road can
//     lower from one it must refuse (a fractional, verdict, void or container member is the tagged-value-word
//     debt of **Gap R.431**, and answering it with the payload word prints 1.0 for 1.5 — the answer class this
//     repo ranks worst, ADR 0418's rule).
//
// and the one question that decides whether the class object gets a turn at all:
//
//   - `instanceBindsAttr` — does the PROGRAM write this name onto an instance of this class (`self.X = 1` in a
//     method the class or an ancestor declares, or `obj.X = 1` where `obj` is known to hold that class). An
//     instance's own word wins, exactly as the reference's `__dict__` wins, and — the part that keeps this
//     cheap — where the program never writes the slot there is no run-time question to ask: the answer is a
//     compile-time fact, so no presence check is emitted for the ~every read in a program that does not shadow.
//     The presence row `@rt_inst_has` exists for the general case (**Gap R.19**'s "a missing attribute answers
//     0"), and this door does not need it.

// classLinearization returns the method-resolution order of a class: the class itself first, then its bases in
// the C3 order the reference uses. Unknown names (a base the source never defines, a builtin) drop out rather
// than failing the build — the read that walks off the end of the chain refuses with the sentence that names
// the member, which is the honest answer for a class that does not bind it anywhere.
//
// The merge is C3's, not a depth-first walk: lin(C) = [C] + merge(lin(B1), lin(B2), …, [B1, B2, …]), where a
// head is taken when it is not in the tail of any remaining list. Memoised, because an attribute read asks this
// for every read and a deep chain is a hot path in a program that uses classes at all.
func (g *irGen) classLinearization(className string) []string {
	if g.classLinCache == nil {
		g.classLinCache = map[string][]string{}
	}
	if lin, ok := g.classLinCache[className]; ok {
		return lin
	}
	// A cycle in the base list (`class A(B)` / `class B(A)`) is a program error the front end owns; the guard
	// here is that the walk terminates, not that it complains.
	if g.classLinVisits == nil {
		g.classLinVisits = map[string]bool{}
	}
	if g.classLinVisits[className] {
		return nil
	}
	g.classLinVisits[className] = true
	defer delete(g.classLinVisits, className)

	lin := []string{className}
	ci, ok := g.classInfos[className]
	if ok && len(ci.bases) > 0 {
		baseLins := make([][]string, 0, len(ci.bases))
		for _, b := range ci.bases {
			if _, isClass := g.classInfos[b]; !isClass {
				continue
			}
			bl := g.classLinearization(b)
			if len(bl) == 0 {
				continue
			}
			// Copy: merge consumes the lists.
			cp := append([]string(nil), bl...)
			baseLins = append(baseLins, cp)
		}
		// The trailing [B1, B2, …] list of the merge definition.
		if len(baseLins) > 0 {
			tails := make([]string, 0, len(ci.bases))
			for _, b := range ci.bases {
				if _, isClass := g.classInfos[b]; isClass {
					tails = append(tails, b)
				}
			}
			baseLins = append(baseLins, tails)
			lin = append(lin, c3Merge(baseLins)...)
		}
	}
	g.classLinCache[className] = lin
	return lin
}

// c3Merge is the merge half of linearization: repeatedly take the head of a list whose head appears in no
// other list's tail, skipping a head already emitted. Every step removes a name, so it terminates; a base list
// no consistent order can satisfy emits its remaining names in order rather than looping, because the walk's
// job is to see every base, not to adjudicate a program the front end should have rejected.
func c3Merge(lists [][]string) []string {
	lists = append([][]string(nil), lists...)
	out := []string{}
	seen := map[string]bool{}
	for {
		notDone := false
		for _, l := range lists {
			if len(l) > 0 {
				notDone = true
				break
			}
		}
		if !notDone {
			return out
		}
		taken := false
		for i, l := range lists {
			if len(l) == 0 {
				continue
			}
			head := l[0]
			if seen[head] {
				lists[i] = l[1:]
				taken = true
				break
			}
			if headInTails(lists, i, head) {
				continue
			}
			out = append(out, head)
			seen[head] = true
			lists[i] = l[1:]
			taken = true
			break
		}
		if !taken {
			for _, l := range lists {
				for _, n := range l {
					if !seen[n] {
						out = append(out, n)
						seen[n] = true
					}
				}
			}
			return out
		}
	}
}

// headInTails reports whether candidate appears after position 0 in any list other than `skip`.
func headInTails(lists [][]string, skip int, candidate string) bool {
	for j, l := range lists {
		if j == skip || len(l) < 2 {
			continue
		}
		for _, n := range l[1:] {
			if n == candidate {
				return true
			}
		}
	}
	return false
}

// classChainMember asks the class object — and every base of it, in resolution order — what it binds for a
// member. `owed` says the answer exists but this road cannot lower it faithfully (a fractional, verdict, void,
// container or call value): the caller refuses with the sentence that names the kind, which is the class-name
// road's existing behaviour and now the instance road's too (**Gap R.431** owns the answer).
func (g *irGen) classChainMember(className, member string) (owner string, val Expr, owed bool, found bool) {
	if className == "" || member == "" {
		return "", nil, false, false
	}
	for _, cn := range g.classLinearization(className) {
		ci, ok := g.classInfos[cn]
		if !ok {
			continue
		}
		if v, held := ci.attrs[member]; held {
			return cn, v, false, true
		}
		if v, mustRefuse := ci.owedAttrs[member]; mustRefuse {
			return cn, v, true, true
		}
	}
	return "", nil, false, false
}

// classChainRefusal is the sentence the read gives for a member the chain binds but this road cannot answer,
// and for one the chain does not bind at all: two accusations, both naming the class that owns the answer.
func (g *irGen) classChainRefusal(readVia, className, member string) error {
	owner, val, owed, found := g.classChainMember(className, member)
	if !found {
		return fmt.Errorf("codegen: %s is refused: the class names no member of that name — its body binds methods, and no base of it binds %s either, so %s holds nothing the class could answer with and the reference would raise AttributeError here (a member the body never bound, and one bound by a call or a container, are roadmap Gap R.431)", readVia, member, member)
	}
	if owed {
		return fmt.Errorf("codegen: %s is refused: %s binds %s to %s, and this backend reads a class member only where the read lowers to the same word the reference prints — a fractional, verdict or void member needs the tagged value word that carries its own kind (roadmap L11.1, Gap R.431)", readVia, owner, member, classAttrKindName(val))
	}
	return nil
}

// instanceBindsAttr answers the one question that decides whether an instance read keeps its own word: does
// this program write `name` onto an instance of `className` — either with `self.name = …` in a method that
// className itself, or a base of it, declares (an inherited `__init__` writes the attribute on a subclass's
// instance too), or with `obj.name = …` where `obj` is known to hold that class.
//
// The scan is over the AST the walk already holds, and it is conservative in the direction that keeps today's
// answers: any binding at all leaves the read on the instance road it has always used.
func (g *irGen) instanceBindsAttr(className, name string) bool {
	return len(g.instanceAttrWrites(className, name)) > 0
}

// instanceAttrWrites collects the VALUE each write to `name` on an instance of `className` (or of a class the
// read's class inherits from — an inherited `__init__` writes the attribute on a subclass's instance too)
// hands the object. The count decides whether the object's own word can win at all; the kinds decide what tag
// a read of it carries, which is why the answer is the expressions and not a boolean.
func (g *irGen) instanceAttrWrites(className, name string) []Expr {
	if g.program == nil || className == "" || name == "" {
		return nil
	}
	inChain := map[string]bool{}
	for _, cn := range g.classLinearization(className) {
		inChain[cn] = true
	}
	var writes []Expr
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		for _, st := range sts {
			if a, ok := st.(*AssignStmt); ok && a != nil {
				if at, ok := a.Target.(*Attr); ok && at != nil && at.Name != nil && at.Name.Value == name {
					if rc := g.receiverClassOf(at.Obj); rc != "" && inChain[rc] {
						writes = append(writes, a.Value)
					}
				}
			}
			walk(classChainStmtChildren(st))
		}
	}
	walk(g.program.Stmts)
	for cn := range inChain {
		ci, ok := g.classInfos[cn]
		if !ok {
			continue
		}
		for _, fd := range ci.methodDefs {
			if fd == nil {
				continue
			}
			writes = append(writes, selfAttrWrites(fd.Body, name)...)
		}
	}
	return writes
}

// selfAttrWrites is the writes a method body makes to `self.name`.
func selfAttrWrites(sts []Stmt, name string) []Expr {
	var writes []Expr
	var walk func([]Stmt)
	walk = func(bs []Stmt) {
		for _, st := range bs {
			if a, ok := st.(*AssignStmt); ok && a != nil {
				if at, ok := a.Target.(*Attr); ok && at != nil && at.Name != nil && at.Name.Value == name {
					if n, ok := at.Obj.(*Name); ok && n != nil && n.Value == "self" {
						writes = append(writes, a.Value)
					}
				}
			}
			walk(classChainStmtChildren(st))
		}
	}
	walk(sts)
	return writes
}

// instanceWritesOnlyText answers the tag question a shadowed read asks: the object may hold the class's text
// or the text the program wrote, and either way a print door that labelled the slot a number would print the
// intern table's index. A write the walk cannot prove is a text closes the door, which is the direction that
// keeps today's answers standing.
func (g *irGen) instanceWritesOnlyText(writes []Expr) bool {
	if len(writes) == 0 {
		return true
	}
	for _, w := range writes {
		if _, isText := w.(*StrLit); isText {
			continue
		}
		if !g.exprIsString(w) {
			return false
		}
	}
	return true
}

// receiverClassOf is `receiverClass` for an expression used as an attribute's base, without the per-function
// `self` state the scan must not consult: it answers only for a variable whose class the walk knows and for a
// fresh `C()` construction.
func (g *irGen) receiverClassOf(e Expr) string {
	if e == nil {
		return ""
	}
	if n, ok := e.(*Name); ok {
		if n == nil {
			return ""
		}
		if c, ok := g.varClasses[n.Value]; ok {
			return c
		}
		return ""
	}
	if c, ok := e.(*Call); ok {
		if n, ok2 := c.Fn.(*Name); ok2 && n != nil {
			if _, isClass := g.classInfos[n.Value]; isClass {
				return n.Value
			}
		}
	}
	return ""
}

// classChainStmtChildren is the block-bearing shape of a statement, for a scan that walks a body looking for a
// binding. `FuncDef` and `ClassDef` bodies are included: a nested `def` can hold the assignment, and a method's
// body is reached through the class table as well.
func classChainStmtChildren(st Stmt) []Stmt {
	switch s := st.(type) {
	case *IfStmt:
		if s == nil {
			return nil
		}
		out := append([]Stmt(nil), s.Then...)
		out = append(out, s.Else...)
		for _, e := range s.Elifs {
			if e != nil {
				out = append(out, e)
			}
		}
		return out
	case *WhileStmt:
		if s == nil {
			return nil
		}
		return append(append([]Stmt(nil), s.Body...), s.Else...)
	case *ForStmt:
		if s == nil {
			return nil
		}
		return append(append([]Stmt(nil), s.Body...), s.Else...)
	case *TryStmt:
		if s == nil {
			return nil
		}
		out := append([]Stmt(nil), s.Body...)
		out = append(out, s.Finally...)
		for _, ex := range s.Excepts {
			if ex != nil {
				out = append(out, ex.Body...)
			}
		}
		return out
	case *WithStmt:
		if s == nil {
			return nil
		}
		return s.Body
	case *MatchStmt:
		if s == nil {
			return nil
		}
		var out []Stmt
		for _, c := range s.Cases {
			if c != nil {
				out = append(out, c.Body...)
			}
		}
		return out
	case *FuncDef:
		if s == nil {
			return nil
		}
		return s.Body
	case *ClassDef:
		if s == nil {
			return nil
		}
		return s.Body
	}
	return nil
}

// methodReturnsClassText is the method-answer side of the same walk: a body whose `return` hands back
// `self.NAME`, where the class or one of its bases bound a text to NAME, returns a text. The answer-kind
// tables read what a `return` SPELLS, and a class member spells nothing — the same lesson **ADR 0445**
// learned for a hand-off's parameter, arriving from the class side (roadmap row **L12.4**, **Gap R.56**).
func (g *irGen) methodReturnsClassText(fd *FuncDef) bool {
	if fd == nil {
		return false
	}
	owner, ok := g.classOwningMethod(fd)
	if !ok {
		return false
	}
	found := false
	var walk func([]Stmt)
	walk = func(sts []Stmt) {
		if found {
			return
		}
		for _, st := range sts {
			if found {
				return
			}
			if r, isRet := st.(*ReturnStmt); isRet {
				if at, isAttr := r.Expr.(*Attr); isAttr && at != nil && at.Name != nil {
					if nm, isSelf := at.Obj.(*Name); isSelf && nm != nil && nm.Value == "self" {
						if _, val, owed, foundInChain := g.classChainMember(owner, at.Name.Value); foundInChain && !owed {
							if _, isText := val.(*StrLit); isText {
								found = true
								return
							}
						}
					}
				}
				continue
			}
			walk(classChainStmtChildren(st))
		}
	}
	walk(fd.Body)
	return found
}

// classOwningMethod finds the class that declares a method declaration, by identity of the declaration the
// class table already holds.
func (g *irGen) classOwningMethod(fd *FuncDef) (string, bool) {
	for _, cn := range g.classOrder {
		ci, ok := g.classInfos[cn]
		if !ok {
			continue
		}
		for _, d := range ci.methodDefs {
			if d == fd {
				return cn, true
			}
		}
	}
	return "", false
}

// presenceCheckedAttrRead is the shadowing case: the program DOES write this name onto some instance of the
// class (`self.X = …` in an inherited `__init__`, or `obj.X = …` somewhere), and the class body binds it too.
// Which word a given read answers is then a run-time fact about that object, and the heap already keeps the
// answer — the presence row `@rt_inst_has`, written by `@rt_inst_put` for exactly this question (**ADR 0235**),
// and never asked by a plain attribute read until now (**Gap R.19**'s row owns the case where NEITHER binds
// it, where the reference raises AttributeError and this backend still answers 0).
//
// The lowering of the class's own value is spliced into the else-arm only when it is a single basic block: a
// member whose value needs control flow to lower is left on the instance road it has always used, rather than
// half-compiled into a block that does not dominate its uses.
func (g *irGen) presenceCheckedAttrRead(b *strings.Builder, objHandle string, slot int, val Expr) (string, bool) {
	if !classAttrValueStable(val) {
		return "", false
	}
	var cb strings.Builder
	classVal, err := g.value(&cb, val)
	if err != nil || classVal == "" {
		return "", false
	}
	body := cb.String()
	if !classChainSingleBlock(body) {
		return "", false
	}
	has := g.newTmp()
	isc := g.newTmp()
	lblInst := g.newLabel("attr.inst")
	lblCls := g.newLabel("attr.cls")
	lblDone := g.newLabel("attr.done")
	instVal := g.newTmp()
	out := g.newTmp()
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_has(i32 %s, i32 %d)\n", has, objHandle, slot))
	b.WriteString(fmt.Sprintf("  %s = icmp ne i32 %s, 0\n", isc, has))
	b.WriteString(fmt.Sprintf("  br i1 %s, label %%%s, label %%%s\n", isc, lblInst, lblCls))
	b.WriteString(lblInst + ":\n")
	b.WriteString(fmt.Sprintf("  %s = call i32 @rt_inst_get(i32 %s, i32 %d)\n", instVal, objHandle, slot))
	b.WriteString(fmt.Sprintf("  br label %%%s\n", lblDone))
	b.WriteString(lblCls + ":\n")
	b.WriteString(body)
	b.WriteString(fmt.Sprintf("  br label %%%s\n", lblDone))
	b.WriteString(lblDone + ":\n")
	b.WriteString(fmt.Sprintf("  %s = phi i32 [ %s, %%%s ], [ %s, %%%s ]\n", out, instVal, lblInst, classVal, lblCls))
	return out, true
}

// classChainSingleBlock reports whether spliced IR is one straight-line block: no label of its own and no
// terminator, so it can be pasted between a `br` and a `phi` and still be well formed.
func classChainSingleBlock(text string) bool {
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasSuffix(t, ":") {
			return false
		}
		if strings.HasPrefix(t, "br ") || strings.HasPrefix(t, "ret ") || strings.HasPrefix(t, "switch ") || strings.HasPrefix(t, "invoke ") {
			return false
		}
	}
	return true
}

// classMemberReadIsText is the text door for a read that walks into the class object and may answer the
// object's own word instead: `c.NAME`, where the class or one of its bases binds a text AND every write the
// program makes to that name on an instance of the class is a text too. Either word is an index into
// `@str_tab`, so the read answers an index and the door says TEXT — while `stringVal`, which folds a CONSTANT
// text, stays silent for this shape: folding would print the class's word for an object holding another
// (roadmap row **L12.4**, **Gap R.56**).
func (g *irGen) classMemberReadIsText(e Expr) bool {
	at, ok := e.(*Attr)
	if !ok || at == nil || at.Name == nil || at.Obj == nil {
		return false
	}
	className := g.receiverClass(at.Obj)
	if className == "" {
		return false
	}
	_, val, owed, found := g.classChainMember(className, at.Name.Value)
	if !found || owed {
		return false
	}
	if _, isText := val.(*StrLit); !isText && !g.exprIsString(val) {
		return false
	}
	return g.instanceWritesOnlyText(g.instanceAttrWrites(className, at.Name.Value))
}
