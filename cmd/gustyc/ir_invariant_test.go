package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The compiler accusing ITSELF, at the CLI (roadmap L11.6 · Gap R.342; ADR 0378).
//
// ADR 0166 reserves exit 2 for "gusty produced bad IR", and until ADR 0378 the only way into that class was
// an EXTERNAL tool noticing — `llc` rejecting a module that stored through a slot no `define` had allocated,
// blaming a file under /tmp for a defect in this repository. The frame-slot audit (`pkg/lang/frame_audit.go`)
// says the same thing first, and the rows below own the two claims the CLI has to keep: that the compiler's
// own verdict is the SAME class as an external rejection rather than collapsing into a source error (ADR
// 0211), and that it arrives as data — the function, the slot and the instruction — beside the sentence.

func invariantCase() *lang.IRInvariantError {
	return &lang.IRInvariantError{
		Subject: "frame-slot audit",
		Fn:      "inner_env",
		Slot:    "%_w",
		Line:    "  store i32 %t7, i32* %_w",
	}
}

func TestTheCompilersOwnAuditIsTheCompilerBugClass(t *testing.T) {
	t.Parallel()
	rejection := &lang.ToolchainRejectionError{Tool: "llc-20", Stage: "llc", Err: errors.New("exit status 1"), Output: "use of undefined value '%_w'"}
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"the audit refusing our own module is the compiler's bug", invariantCase(), exitIRVerify},
		{"the audit wrapped by a pipeline stage still reads 2", fmtWrap(invariantCase()), exitIRVerify},
		{"an llc rejection of the same shape reads 2 as well", rejection, exitIRVerify},
		// The control: the class is not "any codegen sentence". A refusal to lower is the program's, and 1.
		{"an ordinary codegen refusal stays a compile error", errors.New("codegen: unsupported attr expression"), exitCompileError},
	}
	for _, tc := range cases {
		if got := toolchainExit(tc.err); got != tc.want {
			t.Errorf("%s: toolchainExit = %d, want %d (%v)", tc.name, got, tc.want, tc.err)
		}
	}
	// And the audit's sentence names what the reader needs to file the bug, without a temp path in it.
	msg := invariantCase().Error()
	for _, want := range []string{"frame-slot audit", "inner_env", "%_w", "store i32 %t7, i32* %_w", "Gap R.342", "ADR 0378"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the audit's sentence does not name %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "/tmp") {
		t.Errorf("the audit blames a file instead of itself:\n%s", msg)
	}
}

// TestTheInvariantPayloadIsDataNotOnlyASentence runs the JSON path of the report the audit produces: the
// exit code, the phase the compiler speaks in, and the `invariant` object the schema declares — so an agent
// can read WHICH slot in WHICH function without taking a name out of prose.
func TestTheInvariantPayloadIsDataNotOnlyASentence(t *testing.T) {
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := reportCompileErr(invariantCase(), true)
	w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	if code != exitIRVerify {
		t.Fatalf("the audit left at exit %d, want %d — the class ADR 0166 reserves for a broken artifact", code, exitIRVerify)
	}
	var payload struct {
		Ok         bool   `json:"ok"`
		Phase      string `json:"phase"`
		Error      string `json:"error"`
		Exit       int    `json:"exit"`
		Capability *struct {
			Code string `json:"code"`
		} `json:"capability"`
		Invariant *struct {
			Audit       string `json:"audit"`
			Function    string `json:"function"`
			Slot        string `json:"slot"`
			Instruction string `json:"instruction"`
		} `json:"invariant"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out), &payload); err != nil {
		t.Fatalf("the payload does not parse: %v\n%s", err, out)
	}
	if payload.Ok || payload.Phase != "codegen" || payload.Exit != exitIRVerify || code != payload.Exit {
		t.Fatalf("the payload is %s", bytes.TrimSpace(out))
	}
	if payload.Capability != nil {
		t.Errorf("an invariant violation is not a capability refusal; the key must be null, got %+v", payload.Capability)
	}
	if payload.Invariant == nil {
		t.Fatalf("the payload carries no `invariant` object: the reader must scrape the names out of the sentence\n%s", out)
	}
	if payload.Invariant.Function != "inner_env" || payload.Invariant.Slot != "%_w" ||
		payload.Invariant.Audit != "frame-slot audit" || payload.Invariant.Instruction != "store i32 %t7, i32* %_w" {
		t.Errorf("the `invariant` object is %s", jsonOf(t, payload.Invariant))
	}
	// The schema declares the member the payload carries (docs/operations.md § JSON output).
	var doc struct {
		Definitions struct {
			IRInvariant struct {
				Required   []string       `json:"required"`
				Properties map[string]any `json:"properties"`
			} `json:"irInvariant"`
			ToolFailure struct {
				Properties map[string]struct {
					Ref string `json:"$ref"`
				} `json:"properties"`
			} `json:"toolFailure"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(lang.ASTIRSchema), &doc); err != nil {
		t.Fatalf("the schema is not valid JSON: %v", err)
	}
	if len(doc.Definitions.IRInvariant.Properties) == 0 {
		t.Fatal("definitions.irInvariant is missing or declares no properties: the payload member is undeclared")
	}
	for _, key := range []string{"audit", "function", "slot", "instruction"} {
		if _, ok := doc.Definitions.IRInvariant.Properties[key]; !ok {
			t.Errorf("definitions.irInvariant declares no %q", key)
		}
	}
	if ref := doc.Definitions.ToolFailure.Properties["invariant"]; ref.Ref != "#/definitions/irInvariant" {
		t.Errorf("definitions.toolFailure does not reference the invariant payload: %+v", ref)
	}
}

// fmtWrap wraps with %w, the way every pipeline stage does, so the class survives the sentence.
func fmtWrap(err error) error { return &wrappedErr{err} }

type wrappedErr struct{ err error }

func (w *wrappedErr) Error() string { return "jit: " + w.err.Error() }
func (w *wrappedErr) Unwrap() error { return w.err }

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("cannot render %+v: %v", v, err)
	}
	return string(b)
}
