package integration

// The agent's door onto the function-value pass (roadmap L11.7, ADR 0345): a script must be able to ask
// what the pass decided about a program, in JSON, with the exit codes the contract promises (ADR 0006) —
// and the document it reads has to be the one `--schema` declares.

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func runCallables(t *testing.T, src string) (string, int) {
	t.Helper()
	cmd := exec.Command(cliBin(t), "--callables", src)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	code := 0
	if err := cmd.Run(); err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("gustyc --callables: %v", err)
		}
		code = ee.ExitCode()
	}
	return out.String(), code
}

func TestTheCallablesAccountIsMachineReadableAtTheCLI(t *testing.T) {
	t.Parallel()
	out, code := runCallables(t, "def twice(fn, v):\n    return fn(v) + fn(v)\n\ndef d2(x: int) -> int:\n    return x * 2\n\nprint(twice(d2, 3))\n")
	if code != 0 {
		t.Fatalf("a rewritten program exited %d:\n%s", code, out)
	}
	var rep struct {
		Tool            string   `json:"tool"`
		Pass            string   `json:"pass"`
		Rewritten       bool     `json:"rewritten"`
		Clones          int      `json:"clones"`
		HigherOrderFunc []string `json:"higher_order_functions"`
		Symbols         []string `json:"symbols"`
		Refused         []string `json:"refused"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("the account is not JSON:\n%s\n%v", out, err)
	}
	if rep.Tool != "callable-values" || rep.Pass != "defunctionalize-callables" {
		t.Fatalf("the document does not name its tool and pass: %+v", rep)
	}
	if !rep.Rewritten || rep.Clones != 1 || len(rep.HigherOrderFunc) != 1 || rep.HigherOrderFunc[0] != "twice" {
		t.Fatalf("the rewrite is not accounted for: %+v", rep)
	}
	if len(rep.Refused) != 0 {
		t.Fatalf("a rewritten program reports refusals: %v", rep.Refused)
	}

	// The decline: still a document, still exit 0, and the sentence names the row that owns the shape.
	out, code = runCallables(t, "print(1)\n")
	if code != 0 {
		t.Fatalf("a decline is an answer; it exited %d:\n%s", code, out)
	}
	if !strings.Contains(out, `"rewritten": false`) || !strings.Contains(out, "roadmap L11.7") {
		t.Fatalf("the decline does not say why, with its row:\n%s", out)
	}

	// Junk in, exit 1 out: the tool was handed something that is not gusty (ADR 0006).
	out, code = runCallables(t, "def (")
	if code != 1 {
		t.Fatalf("an unparseable source exited %d, want 1:\n%s", code, out)
	}
	if !strings.Contains(out, "callables:") {
		t.Fatalf("the failure does not say which tool failed:\n%s", out)
	}
}

// The document has to be the one the schema declares, or an agent reading the
// schema is told about a shape the tool does not print.
func TestTheCallablesDocumentIsTheOneTheSchemaDeclares(t *testing.T) {
	t.Parallel()
	cmd := exec.Command(cliBin(t), "--schema")
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("gustyc --schema: %v\n%s", err, out.String())
	}
	var schema struct {
		Definitions map[string]json.RawMessage `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(out.String()), &schema); err != nil {
		t.Fatalf("--schema is not JSON: %v", err)
	}
	def, ok := schema.Definitions["callablePassReport"]
	if !ok {
		t.Fatal("the schema declares no callablePassReport; --callables has no machine contract")
	}
	var doc struct {
		Required   []string       `json:"required"`
		Properties map[string]any `json:"properties"`
		Desc       string         `json:"description"`
	}
	if err := json.Unmarshal(def, &doc); err != nil {
		t.Fatalf("the callablePassReport definition is not an object schema: %v", err)
	}
	for _, want := range []string{"tool", "pass", "hash", "rewritten", "clones", "hoisted_lambdas", "rewritten_call_sites", "higher_order_functions", "symbols", "refused"} {
		if _, ok := doc.Properties[want]; !ok {
			t.Fatalf("the schema's callablePassReport has no %q property", want)
		}
		if !containsString(doc.Required, want) {
			t.Fatalf("the schema's callablePassReport does not require %q, so a tool may omit it", want)
		}
	}
	if !strings.Contains(doc.Desc, "L11.7") || !strings.Contains(doc.Desc, "0345") {
		t.Fatalf("the definition does not cite the row and the decision it describes: %q", doc.Desc)
	}
}

func containsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
