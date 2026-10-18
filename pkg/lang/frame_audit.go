package lang

import (
	"fmt"
	"regexp"
	"strings"
)

// IRInvariantError says the module this compiler produced breaks an invariant of the backend — caught by
// the compiler's own audit, before the module reached a tool that could have been blamed for it.
//
// It is the same CLASS of event as a toolchain rejection (`ToolchainRejectionError`, ADR 0211): the program
// parsed, analysed and lowered, and what came out the other end is malformed. Under the exit-code contract
// (ADR 0164, ADR 0166) that is exit 2 — the code that separates "gusty produced bad IR" from "your program
// is wrong" — and it has to stay that way whether or not `llc` also happens to notice. Reported through the
// ordinary codegen-error path it would leave at exit 1, which is ADR 0211's exact confusion: a compiler bug
// described as a defect in the reader's program.
//
// It is deliberately NOT a `ToolchainRejectionError`: that type's whole meaning is "the external tool said
// no", and a module that never left the compiler was never refused by anyone — the same honesty rule that
// keeps an uninstalled `llc` out of that class (ADR 0211) and an expired budget out of it too (ADR 0312).
type IRInvariantError struct {
	Fn      string // the `define` whose body broke the rule
	Slot    string // the slot-shaped name that was read but never allocated
	Line    string // the offending instruction, verbatim
	Subject string // which audit said it, so more than one can share the class
}

func (e *IRInvariantError) Error() string {
	who := e.Subject
	if who == "" {
		who = "codegen"
	}
	return fmt.Sprintf("codegen: internal invariant violated (%s): %s reads %s, a stack slot no `define` allocated. "+
		"Emitted as `%s`. This is a bug in the compiler, not in the program: every road that emits a body must enter "+
		"the frame that body's slots live in (roadmap L11.6 · Gap R.342, ADR 0378 — ADR 0166 is why it leaves at the "+
		"compiler-bug exit code rather than as a source error)", who, e.Fn, e.Slot, strings.TrimSpace(e.Line))
}

// slotOperandRE matches a name used as a POINTER operand (`i32* %_w`, `i8* %bnd_z`) — the only way a frame
// slot is ever addressed: `store i32 %t7, i32* %_w`, `%_w.ld1 = load i32, i32* %_w`,
// `call void @rt_root_put(i32* %_w)`. That is also what keeps ordinary registers out of the audit: `%t1`,
// `%gc.frame.base` and `%len` are never preceded by a `*`, and an `%_x = alloca` line defines its name at
// the start of the line rather than as an operand, so an allocation is never its own violation.
var slotOperandRE = regexp.MustCompile(`\*\s+(%[A-Za-z0-9_.]+)`)

// auditFrameSlots walks the finished module and asks, of every `define`, whether each slot-shaped name it
// reads was allocated in THAT function.
//
// The rule is the one `enterFrameSlots` installs: a variable slot (`%_x`, `%_x_tag`), a written-flag
// (`%bnd_x`) and a loop counter (`%_ctrN`) belong to the frame that allocated them, and it is their ADDRESS
// that identifies a variable to the collector (ADR 0181). A body emitted with another frame's tables reads
// a slot its own `define` never allocated, and that is precisely what `llc` catches — at exit 2, blaming the
// module and a temp path (roadmap Gap R.342, where three lines of ordinary source reached it). Asking the
// question here means the compiler says it first, naming the function and the slot.
//
// The audit reads the SHIPPED text — after `attachFrameStack`, which may add instructions, and before
// `hoistAllocas`, which only moves an allocation within its own function — so it audits the bytes `llc`
// would have been handed. `define` blocks are walked the same way `hoistAllocas` walks them.
func auditFrameSlots(module string) error {
	lines := strings.Split(module, "\n")
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], "define ") {
			continue
		}
		end := i + 1
		for end < len(lines) && lines[end] != "}" {
			end++
		}
		if end >= len(lines) {
			return auditOneDefine(lines[i], strings.Join(lines[i+1:], "\n"))
		}
		if err := auditOneDefine(lines[i], strings.Join(lines[i+1:end], "\n")); err != nil {
			return err
		}
		i = end
	}
	return nil
}

// auditOneDefine asks the question of one function: every slot-shaped name its body reads as a pointer must
// have been allocated by an `alloca` in the same body — or be the load register of one that was
// (`%_x.ld1`, the way every read of a variable is spelled), which the suffix test covers.
func auditOneDefine(header, body string) error {
	if !strings.Contains(body, "%_") && !strings.Contains(body, "%bnd_") {
		// The cheap exit for the runtime helpers and the `ret`-only thunks: neither spells a slot, and the
		// hand-written blocks under `codegen_ir_*.go` are allowed to keep their own allocas.
		return nil
	}
	allocated := map[string]bool{}
	defined := map[string]bool{}
	// A parameter is a name this function owns, in the same sense an `alloca` is: a helper declared
	// `define … (i32* %_scratch)` reads a slot it was handed, and the caller — not this `define` — allocated
	// it. The runtime's own helpers are hand-written text (`codegen_ir_*.go`), and the audit has no business
	// telling them where their pointers come from.
	for _, param := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(header, "define"), "{"), ",") {
		if i := strings.Index(param, "%"); i >= 0 {
			name := param[i:]
			if j := strings.IndexAny(name, " )\t"); j >= 0 {
				name = name[:j]
			}
			defined[name] = true
		}
	}
	for _, ln := range strings.Split(body, "\n") {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "%") {
			continue
		}
		eq := strings.Index(t, " = ")
		if eq < 0 {
			continue
		}
		name := t[:eq]
		defined[name] = true
		if strings.Contains(t[eq:], "alloca ") {
			allocated[name] = true
		}
	}
	for _, ln := range strings.Split(body, "\n") {
		for _, slot := range slotOperandRE.FindAllStringSubmatch(ln, -1) {
			name := slot[1]
			if !isSlotShaped(name) {
				continue
			}
			if allocated[name] || defined[name] || allocated[strings.SplitN(name, ".", 2)[0]] {
				continue
			}
			return &IRInvariantError{
				Subject: "frame-slot audit",
				Fn:      definedNameOfDefine(header),
				Slot:    name,
				Line:    ln,
			}
		}
	}
	return nil
}

// definedNameOfDefine is the symbol a `define` line names, for the sentence that reports the bug: the
// program's function where the program has one, and the IR spelling otherwise.
func definedNameOfDefine(header string) string {
	i := strings.Index(header, "@")
	if i < 0 {
		return strings.TrimSpace(header)
	}
	name := header[i+1:]
	if j := strings.IndexAny(name, "( "); j >= 0 {
		name = name[:j]
	}
	return name
}

// isSlotShaped reports whether a name is one of the frame's own: a variable slot, a tag slot beside it, a
// written-flag, or a loop counter — every name `enterFrameSlots`'s tables hand out.
func isSlotShaped(name string) bool {
	body := name[1:]
	if strings.HasPrefix(body, "_") {
		return true
	}
	return strings.HasPrefix(body, "bnd_")
}
