package lang

import (
	"bufio"
	"regexp"
	"strings"
)

// OptimizeIR runs compiler-level optimization passes over the emitted LLVM IR
// text. It is a pure-Go, portable pass pipeline (no cgo/LLVM linking): the
// passes mirror what LLVM's optimizer would do at emission time.
//
//   - level 0: returns the IR unchanged.
//   - level >= 1: dead-global elimination — prunes global definitions
//     (@.strN, @.lstN, @.dictN, @.fmtN, ...) that are never referenced by the
//     function body. The codegen eagerly emits a global whenever a literal is
//     lowered; if that literal's value is unused (e.g. a pure ExprStmt), the
//     global is dead and can be removed.
func OptimizeIR(ir string, level int) string {
	if level <= 0 || ir == "" {
		return ir
	}
	return deadGlobalElim(ir)
}

var globalDefRe = regexp.MustCompile(`^(@\.[A-Za-z0-9_]+)\s*=`)
var globalUseRe = regexp.MustCompile(`@\.[A-Za-z0-9_]+`)

// deadGlobalElim scans global definitions, counts references outside their
// own definition line, and drops globals that are never referenced by the
// body (the first non-global line onwards).
func deadGlobalElim(ir string) string {
	defs := map[string]bool{}
	uses := map[string]int{}
	seenBody := false
	sc := bufio.NewScanner(strings.NewReader(ir))
	var lines []string
	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)
		// A global definition starts with `@.name = ...`. Everything after the
		// first non-global, non-blank line is the function body.
		if !seenBody {
			if m := globalDefRe.FindStringSubmatch(trim); m != nil {
				defs[m[1]] = true
				// the definition itself references its own name — count it but
				// the name appears once on its own definition line.
				uses[m[1]]++
				lines = append(lines, line)
				continue
			}
			if trim != "" && !strings.HasPrefix(trim, ";") {
				seenBody = true
			}
		}
		lines = append(lines, line)
		if seenBody {
			for _, m := range globalUseRe.FindAllString(line, -1) {
				uses[m]++
			}
		}
	}
	// A global is dead if it is defined but referenced only by its own
	// definition line (count == 1).
	dead := map[string]bool{}
	for name, n := range uses {
		if defs[name] && n <= 1 {
			dead[name] = true
		}
	}
	if len(dead) == 0 {
		return ir
	}
	var out []string
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if m := globalDefRe.FindStringSubmatch(trim); m != nil && dead[m[1]] {
			continue // drop the dead global definition
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
