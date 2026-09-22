// Package lang implements the gusty language frontend and its LLVM IR
// emitter. This file contains the optimizer: a real IR-level pass pipeline
// over a parsed textual-LLVM-IR module (CFG construction, dead-block
// elimination, constant propagation/folding, and mem2reg-style alloca
// promotion), rather than the regex text transform it used to be.
//
// The passes operate on the small, regular IR subset the codegen emits:
// typed arithmetic/compare/select/zext/sitofp/fptosi instructions, loads and
// stores into allocas, calls, and branch/ret terminators. Because we keep
// instruction text verbatim (rewriting register uses token-wise), the output
// stays byte-compatible with llvm-as/llc for the whole subset.
package lang

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// IR text tokenizer + instruction parser
// ---------------------------------------------------------------------------

// irKind classifies an instruction for pass purposes.
type irKind int

const (
	kAssign irKind = iota // %t = <op> ...
	kStore                // store <ty> <val>, <ty>* <ptr>
	kBr                   // br i1 ... / br label ...
	kRet                  // ret ...
	kCall                 // call void ... (bare, no def) or %t = call ...
)

type irInstr struct {
	raw     string
	kind    irKind
	deleted bool

	def string // destination register ("%t") or ""

	op   string // opcode: add/sub/..., icmp/fcmp/..., load/store/br/ret/call/alloca/gep/...
	pred string
	typ  string   // operand type ("i32", "i1", "double")
	ops  []string // operands (registers or literals)

	// load/store pointer operand
	ptr string
	// store value operand
	storeVal string

	// branch operands
	cond   string // br i1 cond
	then   string // "label %a"
	els    string // "label %b"
	target string // "label %a"

	// ret value
	retVal string

	// call callee
	callee string

	hasSideEffect bool // store, call, terminator
}

// tokenize splits an IR instruction line into tokens. LLVM IR is
// whitespace/comma separated; the emitted subset has no nested parens except
// in call/gep lines, which we keep opaque (only register extraction applies).
func tokenize(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ',' || r == '\n' || r == '\r'
	})
}

var regRe = regexp.MustCompile(`%([A-Za-z0-9_.]+)`)

// regsIn returns the register tokens in line.
func regsIn(line string) []string {
	ms := regRe.FindAllString(line, -1)
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m)
	}
	return out
}

func parseInstr(line string) *irInstr {
	line = strings.TrimSpace(line)
	in := &irInstr{raw: line}
	toks := tokenize(line)
	if len(toks) == 0 {
		return in
	}
	switch toks[0] {
	case "store":
		in.kind = kStore
		in.op = "store"
		in.hasSideEffect = true
		// store <ty> <val>, <ty>* <ptr>
		if len(toks) >= 5 {
			in.storeVal = toks[2]
			in.ptr = toks[len(toks)-1]
			in.typ = toks[1]
		}
		return in
	case "br":
		in.kind = kBr
		in.op = "br"
		in.hasSideEffect = true
		// br i1 %c, label %t, label %e   or   br label %t
		if len(toks) >= 2 && toks[1] == "i1" {
			in.cond = toks[2]
			in.then = "label " + toks[4]
			in.els = "label " + toks[6]
		} else if len(toks) >= 3 {
			in.target = "label " + toks[2]
		}
		return in
	case "ret":
		in.kind = kRet
		in.op = "ret"
		in.hasSideEffect = true
		if len(toks) >= 3 {
			in.retVal = toks[2]
			in.typ = toks[1]
		}
		return in
	case "call":
		in.kind = kCall
		in.op = "call"
		in.hasSideEffect = true
		parseCallCallee(in, line)
		return in
	}
	// assignment: %t = <op> ...
	if len(toks) >= 3 && toks[1] == "=" {
		in.def = toks[0]
		in.op = toks[2]
		switch in.op {
		case "load":
			// %t = load <ty>, <ty>* <ptr>
			in.kind = kAssign
			in.ptr = toks[len(toks)-1]
			in.typ = toks[3]
		case "icmp", "fcmp":
			// %t = icmp <pred> <ty> <a>, <b>
			in.kind = kAssign
			if len(toks) >= 7 {
				in.pred = toks[3]
				in.typ = toks[4]
				in.ops = []string{toks[5], toks[6]}
			}
		case "add", "sub", "mul", "sdiv", "srem", "and", "or", "xor",
			"fadd", "fsub", "fmul", "fdiv":
			// %t = <op> <ty> <a>, <b>
			in.kind = kAssign
			if len(toks) >= 6 {
				in.typ = toks[3]
				in.ops = []string{toks[4], toks[5]}
			}
		case "select":
			// %t = select <condty> <cond>, <ty> <v1>, <ty> <v2>
			in.kind = kAssign
			if len(toks) >= 9 {
				in.cond = toks[4]
				in.typ = toks[5]
				in.ops = []string{toks[6], toks[8]}
			}
		case "zext", "sitofp", "fptosi":
			// %t = zext <ty> <v> to <rty>
			in.kind = kAssign
			if len(toks) >= 6 {
				in.typ = toks[3]
				in.ops = []string{toks[4]}
			}
		case "call":
			in.kind = kCall
			in.hasSideEffect = true
			parseCallCallee(in, line)
		case "alloca":
			in.kind = kAssign // pure; def is the pointer
		default:
			// getelementptr, phi, switch, ... — kept opaque.
			in.kind = kAssign
		}
	}
	return in
}

func parseCallCallee(in *irInstr, line string) {
	// extract @name( — callee
	m := calleeRe.FindStringSubmatch(line)
	if m != nil {
		in.callee = m[1]
	}
}

var calleeRe = regexp.MustCompile(`(@[A-Za-z0-9_.]+)`)

// parseInstrInto re-parses a (possibly rewritten) line into in, preserving
// deletion state.
func parseInstrInto(in *irInstr, line string) {
	deleted := in.deleted
	n := parseInstr(line)
	in.raw = n.raw
	in.kind = n.kind
	in.def = n.def
	in.op = n.op
	in.pred = n.pred
	in.typ = n.typ
	in.ops = n.ops
	in.ptr = n.ptr
	in.storeVal = n.storeVal
	in.cond = n.cond
	in.then = n.then
	in.els = n.els
	in.target = n.target
	in.retVal = n.retVal
	in.callee = n.callee
	in.hasSideEffect = n.hasSideEffect
	in.deleted = deleted
}

// ---------------------------------------------------------------------------
// IR module / CFG structures
// ---------------------------------------------------------------------------

type irBlock struct {
	label  string
	instrs []*irInstr
	succ   []int // successor block indices (in fn.blocks)
	pred   []int
}

type irFunction struct {
	sig    string // "define i32 @main() {"
	blocks []*irBlock
	consts map[string]string // def -> folded/substituted value (persistent)
}

type irModule struct {
	lines []string // module-level lines (globals + declares)
	funcs []*irFunction
}

func isLabel(line string) bool {
	return labelRe.MatchString(line)
}

var labelRe = regexp.MustCompile(`^[A-Za-z0-9._]+:$`)

// ---------------------------------------------------------------------------
// Module parsing
// ---------------------------------------------------------------------------

func parseModule(ir string) *irModule {
	lines := strings.Split(ir, "\n")
	m := &irModule{}
	i := 0
	// module-level lines until first `define`
	for i < len(lines) {
		ln := strings.TrimSpace(lines[i])
		if strings.HasPrefix(ln, "define ") {
			break
		}
		if ln != "" {
			m.lines = append(m.lines, lines[i])
		}
		i++
	}
	// functions
	for i < len(lines) {
		ln := strings.TrimSpace(lines[i])
		if strings.HasPrefix(ln, "define ") && strings.HasSuffix(ln, "{") {
			fn := &irFunction{sig: lines[i], consts: map[string]string{}}
			i++
			var cur *irBlock
			for i < len(lines) {
				ln2 := strings.TrimSpace(lines[i])
				if ln2 == "}" {
					break
				}
				if isLabel(ln2) {
					cur = &irBlock{label: strings.TrimSuffix(ln2, ":")}
					fn.blocks = append(fn.blocks, cur)
					i++
					continue
				}
				if ln2 == "" {
					i++
					continue
				}
				if cur == nil {
					cur = &irBlock{label: "entry"}
					fn.blocks = append(fn.blocks, cur)
				}
				cur.instrs = append(cur.instrs, parseInstr(lines[i]))
				i++
			}
			m.funcs = append(m.funcs, fn)
			i++ // skip "}"
		} else {
			i++
		}
	}
	return m
}

// ---------------------------------------------------------------------------
// CFG construction
// ---------------------------------------------------------------------------

// buildCFG recomputes successor/predecessor edges for every block from its
// terminator instruction.
func (fn *irFunction) buildCFG() {
	idx := map[string]int{}
	for i, b := range fn.blocks {
		idx[b.label] = i
		b.succ = nil
		b.pred = nil
	}
	for i, b := range fn.blocks {
		// terminator is the last non-deleted instruction
		var term *irInstr
		for _, in := range b.instrs {
			if !in.deleted && (in.kind == kBr || in.kind == kRet) {
				term = in
			}
		}
		if term == nil || term.op != "br" {
			continue
		}
		if term.cond != "" {
			// br i1 %c, label %a, label %b
			if j, ok := idx[labelOf(term.then)]; ok {
				b.succ = append(b.succ, j)
				fn.blocks[j].pred = append(fn.blocks[j].pred, i)
			}
			if j, ok := idx[labelOf(term.els)]; ok {
				b.succ = append(b.succ, j)
				fn.blocks[j].pred = append(fn.blocks[j].pred, i)
			}
		} else if term.target != "" {
			if j, ok := idx[labelOf(term.target)]; ok {
				b.succ = append(b.succ, j)
				fn.blocks[j].pred = append(fn.blocks[j].pred, i)
			}
		}
	}
}

// labelOf returns the label name from a "label %name" operand string.
func labelOf(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "label ") {
		s = strings.TrimPrefix(s, "label ")
	}
	return strings.TrimPrefix(s, "%")
}

// ---------------------------------------------------------------------------
// Dead-block elimination (unreachable block removal)
// ---------------------------------------------------------------------------

func (fn *irFunction) deadBlockElim() bool {
	fn.buildCFG()
	reach := make([]bool, len(fn.blocks))
	var visit func(i int)
	visit = func(i int) {
		if reach[i] {
			return
		}
		reach[i] = true
		for _, s := range fn.blocks[i].succ {
			visit(s)
		}
	}
	if len(fn.blocks) > 0 {
		visit(0)
	}
	changed := false
	nb := make([]*irBlock, 0, len(fn.blocks))
	for i, b := range fn.blocks {
		if reach[i] {
			nb = append(nb, b)
		} else {
			changed = true
		}
	}
	if changed {
		fn.blocks = nb
	}
	return changed
}

// ---------------------------------------------------------------------------
// Dominance computation (classic iterative dominator sets)
// ---------------------------------------------------------------------------

// domSets returns, for each block, the sorted set of dominating block
// indices (including itself). Used by mem2reg-style promotion to verify a
// store's block dominates all loads.
func (fn *irFunction) domSets() [][]int {
	fn.buildCFG()
	n := len(fn.blocks)
	if n == 0 {
		return nil
	}
	all := make([]bool, n)
	for i := range all {
		all[i] = true
	}
	dom := make([][]bool, n)
	for i := range dom {
		dom[i] = make([]bool, n)
		copy(dom[i], all)
	}
	// entry dominates only itself
	for j := 1; j < n; j++ {
		dom[0][j] = false
	}
	changed := true
	for changed {
		changed = false
		for b := 1; b < n; b++ {
			// dom[b] = {b} ∪ ⋂_{p ∈ pred(b)} dom[p]
			if len(fn.blocks[b].pred) == 0 {
				continue
			}
			nd := make([]bool, n)
			copy(nd, dom[fn.blocks[b].pred[0]])
			for _, p := range fn.blocks[b].pred[1:] {
				for j := 0; j < n; j++ {
					if nd[j] && !dom[p][j] {
						nd[j] = false
					}
				}
			}
			nd[b] = true
			for j := 0; j < n; j++ {
				if nd[j] != dom[b][j] {
					changed = true
					break
				}
			}
			dom[b] = nd
		}
	}
	out := make([][]int, n)
	for b := 0; b < n; b++ {
		for j := 0; j < n; j++ {
			if dom[b][j] {
				out[b] = append(out[b], j)
			}
		}
	}
	return out
}

func dominates(a int, dom [][]int, b int) bool {
	if a < 0 || a >= len(dom) || b < 0 || b >= len(dom) {
		return false
	}
	for _, d := range dom[b] {
		if d == a {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Constant literals
// ---------------------------------------------------------------------------

var intRe = regexp.MustCompile(`^-?\d+$`)
var floatRe = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func isConstLit(s string) bool {
	if s == "true" || s == "false" {
		return true
	}
	if intRe.MatchString(s) || floatRe.MatchString(s) {
		return true
	}
	return false
}

func resolve(op string, consts map[string]string) (string, bool) {
	if isConstLit(op) {
		return op, true
	}
	if strings.HasPrefix(op, "%") {
		if v, ok := consts[op[1:]]; ok {
			return v, true
		}
		return "", false
	}
	return "", false
}

// tryFold attempts to fold an assignment instruction to a literal when all
// required operands resolve to constants.
func (fn *irFunction) tryFold(in *irInstr) (string, bool) {
	switch in.op {
	case "add", "sub", "mul", "sdiv", "srem":
		if len(in.ops) != 2 {
			return "", false
		}
		a, ok := resolve(in.ops[0], fn.consts)
		if !ok {
			return "", false
		}
		b, ok := resolve(in.ops[1], fn.consts)
		if !ok {
			return "", false
		}
		if in.typ == "i1" {
			return "", false
		}
		av, aerr := strconv.ParseInt(a, 10, 64)
		bv, berr := strconv.ParseInt(b, 10, 64)
		if aerr != nil || berr != nil {
			return "", false
		}
		switch in.op {
		case "add":
			return fmt.Sprintf("%d", av+bv), true
		case "sub":
			return fmt.Sprintf("%d", av-bv), true
		case "mul":
			return fmt.Sprintf("%d", av*bv), true
		case "sdiv":
			if bv == 0 {
				return "", false
			}
			return fmt.Sprintf("%d", av/bv), true
		case "srem":
			if bv == 0 {
				return "", false
			}
			return fmt.Sprintf("%d", av%bv), true
		}
	case "and", "or", "xor":
		if len(in.ops) != 2 {
			return "", false
		}
		a, ok := resolve(in.ops[0], fn.consts)
		if !ok {
			return "", false
		}
		b, ok := resolve(in.ops[1], fn.consts)
		if !ok {
			return "", false
		}
		if in.typ == "i1" {
			av := a == "true"
			bv := b == "true"
			switch in.op {
			case "and":
				return boolStr(av && bv), true
			case "or":
				return boolStr(av || bv), true
			case "xor":
				return boolStr(av != bv), true
			}
		}
		av, aerr := strconv.ParseInt(a, 10, 64)
		bv, berr := strconv.ParseInt(b, 10, 64)
		if aerr != nil || berr != nil {
			return "", false
		}
		switch in.op {
		case "and":
			return fmt.Sprintf("%d", av&bv), true
		case "or":
			return fmt.Sprintf("%d", av|bv), true
		case "xor":
			return fmt.Sprintf("%d", av^bv), true
		}
	case "icmp", "fcmp":
		if len(in.ops) != 2 {
			return "", false
		}
		a, ok := resolve(in.ops[0], fn.consts)
		if !ok {
			return "", false
		}
		b, ok := resolve(in.ops[1], fn.consts)
		if !ok {
			return "", false
		}
		if in.typ == "i1" {
			av := a == "true"
			bv := b == "true"
			switch in.pred {
			case "eq":
				return boolStr(av == bv), true
			case "ne":
				return boolStr(av != bv), true
			}
			return "", false
		}
		if in.typ == "double" {
			af, aerr := strconv.ParseFloat(a, 64)
			bf, berr := strconv.ParseFloat(b, 64)
			if aerr != nil || berr != nil {
				return "", false
			}
			return cmpBool(in.pred, af, bf), true
		}
		av, aerr := strconv.ParseInt(a, 10, 64)
		bv, berr := strconv.ParseInt(b, 10, 64)
		if aerr != nil || berr != nil {
			return "", false
		}
		return cmpBool(in.pred, float64(av), float64(bv)), true
	case "select":
		c, ok := resolve(in.cond, fn.consts)
		if !ok {
			return "", false
		}
		if len(in.ops) != 2 {
			return "", false
		}
		if c == "true" {
			return in.ops[0], true
		}
		if c == "false" {
			return in.ops[1], true
		}
		return "", false
	case "zext":
		if len(in.ops) != 1 {
			return "", false
		}
		a, ok := resolve(in.ops[0], fn.consts)
		if !ok {
			return "", false
		}
		if in.typ == "i1" {
			if a == "true" {
				return "1", true
			}
			if a == "false" {
				return "0", true
			}
		}
		return "", false
	case "sitofp":
		if len(in.ops) != 1 {
			return "", false
		}
		a, ok := resolve(in.ops[0], fn.consts)
		if !ok {
			return "", false
		}
		av, err := strconv.ParseInt(a, 10, 64)
		if err != nil {
			return "", false
		}
		return fmt.Sprintf("%g", float64(av)), true
	case "fptosi":
		if len(in.ops) != 1 {
			return "", false
		}
		a, ok := resolve(in.ops[0], fn.consts)
		if !ok {
			return "", false
		}
		av, err := strconv.ParseFloat(a, 64)
		if err != nil {
			return "", false
		}
		iv := int64(av)
		if float64(iv) != av {
			return "", false // not an exact integer — do not fold
		}
		return fmt.Sprintf("%d", iv), true
	}
	return "", false
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func cmpBool(pred string, a, b float64) string {
	var r bool
	switch pred {
	case "eq":
		r = a == b
	case "ne":
		r = a != b
	case "slt", "ult":
		r = a < b
	case "sle", "ule":
		r = a <= b
	case "sgt", "ugt":
		r = a > b
	case "sge", "uge":
		r = a >= b
	default:
		return ""
	}
	return boolStr(r)
}

// ---------------------------------------------------------------------------
// Constant propagation / folding pass
// ---------------------------------------------------------------------------

// rewriteRegs replaces register tokens in line that are present in consts
// with their folded value, skipping the instruction's own def register.
func rewriteRegs(line string, consts map[string]string, def string) string {
	return regRe.ReplaceAllStringFunc(line, func(m string) string {
		if m == def {
			return m
		}
		if v, ok := consts[m[1:]]; ok {
			return v
		}
		return m
	})
}

func (fn *irFunction) foldBranches() bool {
	changed := false
	for _, b := range fn.blocks {
		for _, in := range b.instrs {
			if in.deleted || in.kind != kBr || in.cond == "" {
				continue
			}
			c, ok := resolve(in.cond, fn.consts)
			if !ok {
				continue
			}
			target := in.then
			if c == "false" {
				target = in.els
			}
			if c != "true" && c != "false" {
				continue
			}
			in.raw = "br label %" + labelOf(target)
			parseInstrInto(in, in.raw)
			changed = true
		}
	}
	return changed
}

func (fn *irFunction) constFold() bool {
	changed := false
	for round := 0; round < 64; round++ {
		roundChanged := fn.foldBranches()
		for _, b := range fn.blocks {
			for _, in := range b.instrs {
				if in.deleted {
					continue
				}
				if in.kind == kAssign && in.def != "" {
					if v, ok := fn.tryFold(in); ok {
						fn.consts[in.def[1:]] = v
						in.deleted = true
						roundChanged = true
						continue
					}
				}
				// rewrite folded-register uses (store/br/call/gep/arith operands)
				newraw := rewriteRegs(in.raw, fn.consts, in.def)

				if newraw != in.raw {
					in.raw = newraw
					parseInstrInto(in, newraw)
					roundChanged = true
					if in.kind == kAssign && in.def != "" {
						if v, ok := fn.tryFold(in); ok {
							fn.consts[in.def[1:]] = v
							in.deleted = true
						}
					}
				}
			}
		}
		if !roundChanged {
			break
		}
		changed = true
	}
	return changed
}

// ---------------------------------------------------------------------------
// mem2reg-style alloca promotion
// ---------------------------------------------------------------------------

// promote implements the mem2reg-style transformation for allocas that need
// no phi nodes:
//   - an alloca with no loads at all is dead: delete it and all its stores;
//   - an alloca with exactly one store that dominates every load is promoted:
//     each load is replaced by the store's value and the alloca/store/loads
//     are deleted. (Multiple-store allocas would require phi insertion and
//     are conservatively left alone.)
func (fn *irFunction) promote() bool {
	if len(fn.blocks) == 0 {
		return false
	}
	dom := fn.domSets()
	changed := false

	// collect allocas and their load/store references
	type loadRef struct {
		def   string
		block int
		idx   int
	}
	type storeRef struct {
		val   string
		block int
		idx   int
	}
	type allocaInfo struct {
		def    string
		block  int
		idx    int
		loads  []loadRef
		stores []storeRef
	}

	allocas := map[string]*allocaInfo{}
	esc := map[string]bool{}
	for bi, b := range fn.blocks {
		for idx, in := range b.instrs {
			if in.deleted {
				continue
			}
			switch {
			case in.op == "alloca" && in.def != "":
				allocas[in.def] = &allocaInfo{def: in.def, block: bi, idx: idx}
			case in.kind == kStore && in.ptr != "":
				if ai := allocas[in.ptr]; ai != nil {
					ai.stores = append(ai.stores, storeRef{val: in.storeVal, block: bi, idx: idx})
				}
			case in.kind == kAssign && in.op == "load" && in.ptr != "" && in.def != "":
				if ai := allocas[in.ptr]; ai != nil {
					ai.loads = append(ai.loads, loadRef{def: in.def, block: bi, idx: idx})
				}
			}
		}
	}

	for bi := range fn.blocks {
		for _, in := range fn.blocks[bi].instrs {

			if _, ok := allocas[in.storeVal]; ok {
				esc[in.storeVal] = true
			}
			for _, op := range in.ops {
				if _, ok := allocas[op]; ok && !(op == in.ptr && (in.op == "store" || in.op == "load")) {
					esc[op] = true
				}
			}
		}
	}
	for def, ai := range allocas {
		if esc[def] {
			continue
		}
		if ai == nil {
			continue
		}
		// find the alloca instr to delete
		ab := fn.blocks[ai.block]
		if ai.idx >= len(ab.instrs) || ab.instrs[ai.idx].deleted {
			continue
		}
		switch {
		case len(ai.loads) == 0:
			// dead alloca: delete it and all stores
			ab.instrs[ai.idx].deleted = true
			changed = true
			for _, s := range ai.stores {
				if s.block < len(fn.blocks) && s.idx < len(fn.blocks[s.block].instrs) {
					fn.blocks[s.block].instrs[s.idx].deleted = true
				}
			}
		case len(ai.stores) == 1:
			s := ai.stores[0]
			ok := true
			for _, l := range ai.loads {
				if !dominates(s.block, dom, l.block) {
					ok = false
					break
				}
				// same block: the store must precede the load
				if l.block == s.block && l.idx <= s.idx {
					ok = false
					break
				}
			}
			if !ok {
				continue
			}
			// promote: substitute each load def with the store's value
			for _, l := range ai.loads {
				fn.consts[l.def[1:]] = s.val
			}
			// delete alloca, store, and loads
			ab.instrs[ai.idx].deleted = true
			if s.block < len(fn.blocks) && s.idx < len(fn.blocks[s.block].instrs) {
				fn.blocks[s.block].instrs[s.idx].deleted = true
			}
			for _, l := range ai.loads {
				if l.block < len(fn.blocks) && l.idx < len(fn.blocks[l.block].instrs) {
					fn.blocks[l.block].instrs[l.idx].deleted = true
				}
			}
			changed = true
		}
	}
	return changed
}

// ---------------------------------------------------------------------------
// Dead instruction elimination
// ---------------------------------------------------------------------------

func (fn *irFunction) dce() bool {
	changed := false
	used := map[string]bool{}
	for _, b := range fn.blocks {
		for _, in := range b.instrs {
			if in.deleted {
				continue
			}
			for _, r := range regsIn(in.raw) {
				if r != in.def {
					used[r[1:]] = true
				}
			}
		}
	}
	for _, b := range fn.blocks {
		for _, in := range b.instrs {
			if in.deleted || in.hasSideEffect || in.def == "" {
				continue
			}
			if !used[in.def[1:]] {
				in.deleted = true
				changed = true
			}
		}
	}
	return changed
}

// callArgs returns the raw argument substrings of a call instruction's
// operand list (between the parens).
func callArgs(raw string) []string {
	li := strings.Index(raw, "(")
	if li < 0 {
		return nil
	}
	ri := strings.LastIndex(raw, ")")
	if ri < 0 || ri < li {
		return nil
	}
	body := raw[li+1 : ri]
	var args []string
	for _, a := range strings.Split(body, ",") {
		args = append(args, strings.TrimSpace(a))
	}
	return args
}

// callArgRegs returns the argument registers of a call instruction, in
// position order. A constant operand (e.g. "i32 0") yields "".
func callArgRegs(raw string) []string {
	var out []string
	for _, a := range callArgs(raw) {
		rs := regsIn(a)
		if len(rs) > 0 {
			out = append(out, rs[len(rs)-1])
		} else {
			out = append(out, "")
		}
	}
	return out
}

// heapArgKind classifies what a heap handle does when it appears at argument
// position pos of a call to callee:
//
//	"write"  — receiver of a mutating op; unobservable if the object is dead
//	"read"   — receiver of a reading op; the object's contents are observed
//	"print"  — receiver of a print op; the object's contents are observed
//	"escape" — the handle is copied/derived/retained and may outlive the
//	           function (stored into another object, turned into an %obj
//	           value, sliced into a new object, or returned).
func heapArgKind(callee string, pos int) string {
	callee = calleeTrim(callee)
	if pos == 0 {
		switch callee {
		case "rt_set_elem", "rt_append", "rt_dict_put", "rt_set_add":
			return "write"
		case "rt_get_elem", "rt_list_len", "rt_dict_get", "rt_dict_len",
			"rt_set_len", "rt_contains":
			return "read"
		case "rt_print_list", "rt_print_dict", "rt_print_set":
			return "print"
		case "rt_slice":
			return "escape"
		}
	}
	if callee == "rt_mkobj" && pos == 1 {
		return "escape"
	}
	// any other (callee, position) — value operand, unknown callee, etc.
	return "escape"
}

// calleeTrim returns the callee name without the leading '@'.
func calleeTrim(callee string) string {
	return strings.TrimPrefix(callee, "@")
}

// deadHeapElim eliminates whole dead heap objects. An rt_alloc'd object whose
// handle never escapes the function and is never read, printed, or derived is
// unobservable: its allocation and all of its mutating operations
// (rt_set_elem/rt_append/rt_dict_put/rt_set_add) can be removed together,
// leaving no heap write or allocation behind. This is the IR-level escape
// analysis counterpart of the source-level dead-list elision in escape.go; it
// also catches objects built inside function bodies.
func (fn *irFunction) deadHeapElim() bool {
	type use struct {
		blk *irBlock
		i   int
		in  *irInstr
	}
	var all []use
	alloc := map[string]*irInstr{} // heap handle -> its rt_alloc instruction
	for _, b := range fn.blocks {
		for i, in := range b.instrs {
			if in.deleted {
				continue
			}
			if calleeTrim(in.callee) == "rt_alloc" && in.def != "" {
				alloc[in.def] = in
			}
			all = append(all, use{b, i, in})
		}
	}
	if len(alloc) == 0 {
		return false
	}
	// An object starts dead; any read/print/escape use marks it live.
	dead := map[string]bool{}
	for h := range alloc {
		dead[h] = true
	}
	writes := map[string][]use{} // handle -> its mutating (write) call sites
	for _, u := range all {
		in := u.in
		if in.callee != "" {
			args := callArgRegs(in.raw)
			for pos, reg := range args {
				if reg == "" || reg == in.def {
					continue
				}
				if _, ok := alloc[reg]; !ok {
					continue
				}
				if heapArgKind(in.callee, pos) == "write" {
					writes[reg] = append(writes[reg], u)
				} else {
					dead[reg] = false
				}
			}
			continue
		}
		// non-call uses (store, ret, arith, br, phi, gep, ...) escape.
		for _, reg := range regsIn(in.raw) {
			if _, ok := alloc[reg]; ok {
				dead[reg] = false
			}
		}
	}
	changed := false
	for h, alive := range dead {
		if !alive {
			continue
		}
		alloc[h].deleted = true
		changed = true
		for _, w := range writes[h] {
			w.in.deleted = true
			changed = true
		}
	}
	return changed
}

// ---------------------------------------------------------------------------
// Dead-global elimination (module level)
// ---------------------------------------------------------------------------

var globalRe = regexp.MustCompile(`@[A-Za-z0-9_.]+`)

func isGlobalLine(line string) bool {
	t := strings.TrimSpace(line)
	return !strings.HasPrefix(t, "declare ") && strings.Contains(t, " = ")
}

func (m *irModule) deadGlobalElim() {
	used := map[string]bool{}
	for _, fn := range m.funcs {
		for _, b := range fn.blocks {
			for _, in := range b.instrs {
				for _, g := range globalRe.FindAllString(in.raw, -1) {
					used[g] = true
				}
			}
		}
	}
	keep := make([]string, 0, len(m.lines))
	for _, l := range m.lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "declare ") {
			keep = append(keep, l)
			continue
		}
		if isGlobalLine(l) {
			g := globalRe.FindString(l)
			if g != "" && used[g] {
				keep = append(keep, l)
			}
			// else drop the unused global
		} else {
			keep = append(keep, l)
		}
	}
	m.lines = keep
}

// ---------------------------------------------------------------------------
// Serialization
// ---------------------------------------------------------------------------

func (m *irModule) serialize() string {
	var b strings.Builder
	for _, l := range m.lines {
		b.WriteString(l)
		b.WriteByte('\n')
	}
	for _, fn := range m.funcs {
		b.WriteString(fn.sig)
		b.WriteByte('\n')
		for _, blk := range fn.blocks {
			b.WriteString(blk.label)
			b.WriteByte(':')
			b.WriteByte('\n')
			for _, in := range blk.instrs {
				if in.deleted {
					continue
				}
				b.WriteString("  ")
				b.WriteString(in.raw)
				b.WriteByte('\n')
			}
		}
		b.WriteByte('}')
		b.WriteByte('\n')
	}
	return b.String()
}

// ---------------------------------------------------------------------------
// Public entry point
// ---------------------------------------------------------------------------

// OptimizeIR runs the IR-level optimization pipeline over the textual IR
// emitted by the codegen. level <= 0 disables optimization (identity).
//
// The pipeline is:
//
//	module: dead-global elimination
//	per function (to fixpoint):
//	  constant propagation + folding (incl. branch folding)
//	  mem2reg-style alloca promotion
//	  dead instruction elimination
//	  dead-block (unreachable CFG block) elimination
//
// Every pass operates over the parsed IR/CFG, and the result is re-serialized
// as textual IR that remains valid for llvm-as/llc.
func OptimizeIR(ir string, level int) string {
	if level <= 0 || ir == "" {
		return ir
	}
	m := parseModule(ir)
	if len(m.funcs) == 0 {
		return ir
	}
	m.deadGlobalElim()
	for _, fn := range m.funcs {
		// fixpoint over the pass pipeline
		for {
			c1 := fn.constFold()
			c2 := fn.promote()
			c3 := fn.dce()
			c4 := fn.deadBlockElim()
			c5 := fn.deadHeapElim()
			if !(c1 || c2 || c3 || c4 || c5) {
				break
			}
		}
	}
	return m.serialize()
}
