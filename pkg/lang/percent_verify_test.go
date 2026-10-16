package lang

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The formatting door is a block of textual IR the size of a small module, and a block that only ever
// gets checked by compiling a program that uses it fails one llc error at a time — forty round trips
// for forty typos. This row checks the block ONCE, on its own, with the twelve doors it calls declared
// rather than defined: what is being verified here is the block's own IR, not the module's linkage.
//
// The declarations are the block's contract with the rest of the runtime, and they are spelled here in
// one place: if a door's signature changes, this row and the module both have to hear about it.
func TestPercentRuntimeBlockVerifiesOnItsOwn(t *testing.T) {
	stub := strings.Join([]string{
		`declare i8* @rt_str_ptr(i32)`,
		`declare i32 @rt_str_len(i32)`,
		`declare i32 @rt_str_nchars(i32)`,
		`declare i32 @rt_str_byteoff(i32, i32)`,
		`declare i32 @rt_str_of_value(i32, i32, i32)`,
		`declare i32 @rt_str_intern(i8*)`,
		`declare i32 @rt_str_from_bytes(i8*, i32)`,
		`declare i32 @rt_dict_find(i32, i32, i32)`,
		`declare i32 @rt_dict_get_tagged(i32, i32, i32)`,
		`declare i32 @rt_dict_value_tag(i32, i32, i32)`,
		`declare double @rt_lift_num(i32, i32)`,
		`declare i8* @rt_kind_name(i32)`,
		`declare i32 @snprintf(i8*, i32, i8*, ...)`,
	}, "\n")
	src := stub + "\n" + percentRuntimeIR
	dir := t.TempDir()
	path := filepath.Join(dir, "percent.ll")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if os.Getenv("GUSTY_PERCENT_DUMP") != "" {
		_ = os.WriteFile("/tmp/percent.ll", []byte(src), 0o644)
	}
	for _, tool := range []string{"opt-20", "opt"} {
		if _, err := exec.LookPath(tool); err != nil {
			continue
		}
		cmd := exec.Command(tool, "-passes=verify", "-disable-output", path)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s rejected the formatting door:\n%s\n---- around the first error ----\n%s",
				tool, string(out), percentIRContext(src, string(out)))
		}
		return
	}
	t.Skip("no opt on PATH")
}

// percentIRContext prints the lines the verifier pointed at, because a textual-IR error names a line and
// a 1500-line generated block is not readable by eye.
func percentIRContext(src, verdict string) string {
	lines := strings.Split(src, "\n")
	for _, ln := range strings.Split(verdict, "\n") {
		for _, f := range strings.Fields(ln) {
			if !strings.HasSuffix(f, ".ll") && !strings.Contains(f, ".ll:") {
				continue
			}
			parts := strings.Split(f, ":")
			num := parts[len(parts)-1]
			idx, err := strconv.Atoi(num)
			if err != nil || idx <= 0 || idx > len(lines) {
				continue
			}
			lo := idx - 4
			if lo < 0 {
				lo = 0
			}
			hi := idx + 3
			if hi > len(lines) {
				hi = len(lines)
			}
			var b strings.Builder
			for i := lo; i < hi; i++ {
				b.WriteString(lines[i])
				b.WriteString("\n")
			}
			return b.String()
		}
	}
	return ""
}

// TestPercentRuntimeTokensAreAllSubstituted is the guard against the loudest class of bug in a generated
// IR block: a token that was never replaced reaches llc as an identifier and the module says
// "use of undefined value '@rt_pct_...'" — or, worse, a kind token that reads as a global reaches a
// comparison and the verifier catches it only if someone compiles a program that uses that arm. Every
// @UPPER@ token in the assembled block is a bug; the block is only allowed to name real globals.
func TestPercentRuntimeTokensAreAllSubstituted(t *testing.T) {
	for _, tok := range []string{"@TAG_", "@CODE_", "@OUTLIMIT@", "@DIGITLIMIT@", "@KEYLIMIT@", "@STRKINDS@"} {
		if strings.Contains(percentRuntimeIR, tok) {
			t.Errorf("the formatting door carries an unsubstituted token %q", tok)
		}
	}
	// The block is written as Go raw strings, so a backtick inside one silently ends the literal; if a
	// future edit ever escapes that trap the compiler will not notice, so this row does.
	if strings.Contains(percentRuntimeIR, "`") {
		t.Errorf("the formatting door carries a backtick, which cannot survive a Go raw string")
	}
}
