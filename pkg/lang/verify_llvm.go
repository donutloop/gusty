package lang

// verify_llvm.go — L8.2: a verifyModule-driven pipeline.
//
// The codegen emits textual IR, so the only trustworthy statement that a module
// is well-formed is LLVM's own module verifier. Before this file, that check
// happened as a side effect of `llc` at link time: a codegen bug surfaced as an
// `llc` exit code inside a link step, far from the compiler stage that produced
// it, and `--emit-llvm` users got no verdict at all.
//
// This file makes verification a first-class pipeline stage with a structured
// result: which tool ran, which passes ran, what the verifier said, and whether
// the check was skipped because the pinned toolchain is absent (skipped is never
// reported as "ok" — an unverified module is a known-unknown, not a pass).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// IRVerification is the machine-readable verdict of LLVM's module verifier over
// one generated module.
type IRVerification struct {
	// OK is true only when the verifier ran and accepted the module.
	OK bool `json:"ok"`
	// Tool is the binary that decided the verdict ("opt-20", "llc-20", ...).
	Tool string `json:"tool"`
	// Skipped is true when no verification tool was available. OK is then false
	// too: an unverified module must never be reported as verified.
	Skipped bool `json:"skipped"`
	// Pipeline lists the pass pipelines actually run (e.g. ["verify", "-O2"]).
	Pipeline []string `json:"pipeline,omitempty"`
	// Errors holds the verifier's diagnostics, one per line, unmodified.
	Errors []string `json:"errors,omitempty"`
	// Note carries machine-readable guidance when something is off.
	Note string `json:"note,omitempty"`
	// Toolchain is the pinned LLVM version this backend targets.
	Toolchain string `json:"toolchain,omitempty"`
}

// PinnedLLVMVersion is the supported LLVM major version (docs/operations.md).
const PinnedLLVMVersion = "20"

// VerifyModuleIR runs LLVM's verifier over a generated module.
//
// The check is done with `opt -passes=verify -disable-output` when `opt` is
// available (the canonical verifier entry point, and it also accepts the
// optimization pipeline so `--opt-level` is verified too), falling back to
// `llc -filetype=null` which runs the same module verifier as part of lowering.
func VerifyModuleIR(ir string, optLevel int) (*IRVerification, error) {
	if strings.TrimSpace(ir) == "" {
		return &IRVerification{Toolchain: "LLVM " + PinnedLLVMVersion, Skipped: true, Note: "no module to verify"}, nil
	}
	dir, err := os.MkdirTemp("", "gusty-verify-")
	if err != nil {
		return nil, fmt.Errorf("verify: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)
	irPath := filepath.Join(dir, "prog.ll")
	if err := os.WriteFile(irPath, []byte(ir), 0o600); err != nil {
		return nil, fmt.Errorf("verify: write module: %w", err)
	}

	ver := &IRVerification{Toolchain: "LLVM " + PinnedLLVMVersion}

	// Preferred: opt -passes=verify (plus the requested -O pipeline).
	if tool := availableTool(optCmd); tool != "" {
		args := []string{"-passes=verify", "-disable-output"}
		pipelines := []string{"verify"}
		if optLevel > 0 {
			p := "-O1"
			switch {
			case optLevel >= 3:
				p = "-O3"
			case optLevel == 2:
				p = "-O2"
			}
			args = append(args, p)
			pipelines = append(pipelines, p)
		}
		args = append(args, irPath)
		ver.Tool = tool
		ver.Pipeline = pipelines
		out, err := exec.Command(tool, args...).CombinedOutput()
		if err == nil {
			ver.OK = true
			return ver, nil
		}
		// A missing opt (raced away, not executable) falls through to llc.
		if !toolUsableOutput(string(out)) {
			return ver.verifyWithLLC(dir, ir, err, string(out))
		}
		ver.Errors = verifierLines(string(out))
		if len(ver.Errors) == 0 {
			ver.Errors = []string{strings.TrimSpace(string(out))}
		}
		ver.Note = "LLVM rejected the module; this is a compiler bug, not a source error"
		return ver, fmt.Errorf("verify: %s rejected the module: %s", tool, strings.Join(ver.Errors, "\n"))
	}

	if tool := availableTool(llcCmd); tool != "" {
		ver.Tool = tool
		ver.Pipeline = []string{"filetype=null"}
		out, err := exec.Command(tool, "-filetype=null", "-relocation-model=pic", irPath).CombinedOutput()
		if err == nil {
			ver.OK = true
			return ver, nil
		}
		ver.Errors = verifierLines(string(out))
		if len(ver.Errors) == 0 {
			ver.Errors = []string{strings.TrimSpace(string(out))}
		}
		ver.Note = "LLVM rejected the module; this is a compiler bug, not a source error"
		return ver, fmt.Errorf("verify: %s rejected the module: %s", tool, strings.Join(ver.Errors, "\n"))
	}

	ver.Skipped = true
	ver.Note = fmt.Sprintf("no LLVM toolchain found (looked for %s, %s); the module was not verified", optCmd, llcCmd)
	return ver, nil
}

// verifyWithLLC is the fallback verifier when `opt` is unusable.
func (ver *IRVerification) verifyWithLLC(dir, ir string, err error, out string) (*IRVerification, error) {
	tool := availableTool(llcCmd)
	if tool == "" {
		ver.Skipped = true
		ver.Note = fmt.Sprintf("opt unusable and %s unavailable; the module was not verified", llcCmd)
		return ver, nil
	}
	ver.Tool = tool
	ver.Pipeline = []string{"filetype=null"}
	out2, err2 := exec.Command(tool, "-filetype=null", "-relocation-model=pic", filepath.Join(dir, "prog.ll")).CombinedOutput()
	if err2 == nil {
		ver.OK = true
		return ver, nil
	}
	ver.Errors = verifierLines(string(out2))
	if len(ver.Errors) == 0 {
		ver.Errors = []string{strings.TrimSpace(string(out2))}
	}
	ver.Note = "LLVM rejected the module; this is a compiler bug, not a source error"
	if err != nil {
		return ver, fmt.Errorf("verify: %s rejected the module: %s", tool, strings.Join(ver.Errors, "\n"))
	}
	_ = out
	return ver, fmt.Errorf("verify: %s rejected the module: %s", tool, strings.Join(ver.Errors, "\n"))
}

// availableTool returns name when the binary can be found in PATH.
func availableTool(name string) string {
	if name == "" {
		return ""
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// toolUsableOutput distinguishes "opt ran and rejected the module" from "opt
// could not run at all" (an exec failure prints no verifier diagnostics).
func toolUsableOutput(out string) bool {
	s := strings.TrimSpace(out)
	if s == "" {
		return false
	}
	return strings.Contains(s, "error:") || strings.Contains(s, "verifier") || strings.Contains(s, "Invalid")
}

// verifierLines keeps the diagnostic lines that name the problem. The verifier's
// echo of the offending source line (`  0 = add i32 1`, `^`) is dropped, and the
// tool prefix plus the per-run temp-file path are normalised away, so a JSON
// consumer sees stable text ("prog.ll:3:13: error: ...") rather than a
// machine-specific path it cannot match against.
func verifierLines(out string) []string {
	lines := []string{}
	for _, l := range strings.Split(out, "\n") {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "^") || strings.HasPrefix(t, "|") || strings.HasPrefix(t, "  ") {
			continue
		}
		if !strings.Contains(t, "error:") && !strings.Contains(t, "expected") &&
			!strings.Contains(t, "Invalid") && !strings.Contains(t, "verification") {
			continue
		}
		if i := strings.Index(t, "error:"); i >= 0 {
			head := t[:i]
			// keep "prog.ll:line:col" and drop the tool + temp dir that precede it
			if j := strings.LastIndex(head, "/"); j >= 0 {
				head = head[j+1:]
			}
			if k := strings.Index(head, ".ll:"); k >= 0 {
				head = "prog.ll" + head[strings.Index(head, ".ll:"):]
			} else {
				head = ""
			}
			t = head + " " + t[i:]
			t = strings.TrimSpace(t)
		}
		lines = append(lines, t)
	}
	if len(lines) > 8 {
		lines = lines[:8]
	}
	return lines
}
