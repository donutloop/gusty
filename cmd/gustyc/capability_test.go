package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// The machine path of a refusal (roadmap L11.8, ADR 0346). A capability refusal is the one thing the
// compiler says that is *about the compiler* — "this shape has no lowering, and here is the half that is
// missing" — and until now it reached a script only as prose inside `"error"`. An agent that has to plan
// a compilation therefore had to match on a sentence written for a human, and the failure mode of a
// reworded sentence is not an error message, it is a branch that quietly stops firing.
//
// These cases drive the real binary: the claim is about what a script reads on stdout, not about a Go
// function that produces the same shape.

// TestCLICapabilitiesIsTheRegistry: the self-describing account an agent asks for by name has to be the
// registry, field for field. A kind missing its suggestion or its roadmap rows is a refusal that sends
// the reader nowhere, which is the thing ADR 0166 counts as a bug.
func TestCLICapabilitiesIsTheRegistry(t *testing.T) {
	t.Parallel()
	got := cli(t, "--capabilities")
	var doc struct {
		Tool    string `json:"tool"`
		Family  string `json:"family"`
		Backend string `json:"backend"`
		Kinds   []struct {
			Code       string   `json:"code"`
			Shape      string   `json:"shape"`
			Suggestion string   `json:"suggestion"`
			Rows       []string `json:"rows"`
		} `json:"kinds"`
	}
	if err := json.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("--capabilities is not valid JSON: %v\n%s", err, got[:min(300, len(got))])
	}
	if doc.Tool != "capabilities" || doc.Family != "lower.unsupported" || doc.Backend != "aot" {
		t.Fatalf("the document does not describe itself: %+v", doc)
	}
	if len(doc.Kinds) != len(lang.CapabilityRefusalCodes()) {
		t.Fatalf("--capabilities prints %d kinds, the registry has %d", len(doc.Kinds), len(lang.CapabilityRefusalCodes()))
	}
	seen := map[string]bool{}
	for _, k := range doc.Kinds {
		seen[k.Code] = true
		if strings.TrimSpace(k.Shape) == "" || strings.TrimSpace(k.Suggestion) == "" || len(k.Rows) == 0 {
			t.Fatalf("%q reaches an agent without a shape, a suggestion or a row to follow", k.Code)
		}
		entry, ok := lang.CapabilityRefusalByCode(k.Code)
		if !ok {
			t.Fatalf("%q is printed by the CLI and not in the registry", k.Code)
		}
		if entry.Shape != k.Shape || entry.Suggestion != k.Suggestion {
			t.Fatalf("%q is printed differently than the registry holds it", k.Code)
		}
	}
	for _, code := range lang.CapabilityRefusalCodes() {
		if !seen[code] {
			t.Fatalf("--capabilities omits %q, which the registry has", code)
		}
	}
}

// TestCLICapabilitySchemaEnumIsTheDocument keeps the three readers of one fact reading one fact: the
// enum an agent validates against, the document it browses, and the registry that classifies sentences.
func TestCLICapabilitySchemaEnumIsTheDocument(t *testing.T) {
	t.Parallel()
	var schema struct {
		Definitions struct {
			CapabilityRefusalCode struct {
				Enum []string `json:"enum"`
			} `json:"capabilityRefusalCode"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(cli(t, "--schema")), &schema); err != nil {
		t.Fatalf("--schema did not parse: %v", err)
	}
	enum := schema.Definitions.CapabilityRefusalCode.Enum
	if len(enum) == 0 {
		t.Fatal("--schema declares no capability codes at all")
	}
	for _, code := range enum {
		if _, ok := lang.CapabilityRefusalByCode(code); !ok {
			t.Fatalf("--schema declares %q, which the classifier cannot emit", code)
		}
	}
	if len(enum) != len(lang.CapabilityRefusalCodes()) {
		t.Fatalf("--schema declares %d codes, the registry has %d", len(enum), len(lang.CapabilityRefusalCodes()))
	}
}

// TestCLIFailureCarriesTheCapabilityCode is the row the roadmap's definition of done is written in: a
// script that compiles a shape the backend refuses learns which shape, in data, without reading prose.
func TestCLIFailureCarriesTheCapabilityCode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		code string
	}{
		{"list(<container>) copies", "print(list([1, 2]))", "lower.unsupported.list-copy"},
		{"tuple(...) builds one", "print(tuple([1, 2]))", "lower.unsupported.tuple-call"},
		{"a container beside a container", "print([1] + [2])", "lower.unsupported.sequence-operation"},
		{"a text iterated by a comprehension", "print([c for c in \"abc\"])", "lower.unsupported.comprehension-over-text"},
		{"a printf conversion with no renderer", "print(\"%b\" % 3)", "lower.unsupported.printf-style-format"},
		{"a printf argument with no value", "print(\"%s\" % (lambda: 1))", "lower.unsupported.printf-style-format"},
		{"a function as a value", "def f():\n    return 1\n\nprint(f)", "lower.unsupported.function-read-as-value"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, code := cliExit(t, "--json", "--eval", tc.src)
			if code != exitCompileError {
				t.Fatalf("a capability refusal is exit %d whatever the phase says (ADR 0211); got %d for:\n%s", exitCompileError, code, out)
			}
			var payload struct {
				OK         bool   `json:"ok"`
				Phase      string `json:"phase"`
				Error      string `json:"error"`
				Backend    string `json:"backend"`
				Capability *struct {
					Code       string   `json:"code"`
					Shape      string   `json:"shape"`
					Suggestion string   `json:"suggestion"`
					Rows       []string `json:"rows"`
				} `json:"capability"`
			}
			if err := json.Unmarshal([]byte(out), &payload); err != nil {
				t.Fatalf("the failure payload is not JSON: %v\n%s", err, out)
			}
			if payload.OK {
				t.Fatal("ok must be false on a failure payload")
			}
			if payload.Backend != "aot" {
				t.Fatalf("the payload does not name the backend that refused: %s", out)
			}
			if payload.Capability == nil {
				t.Fatalf("the refusal carries no capability code, so the reader is back to matching on prose:\n%s", out)
			}
			if payload.Capability.Code != tc.code {
				t.Fatalf("classified as %q, want %q\nsentence: %s", payload.Capability.Code, tc.code, payload.Error)
			}
			if payload.Capability.Suggestion == "" || len(payload.Capability.Rows) == 0 {
				t.Fatalf("%q arrives with no suggestion and no row to follow", tc.code)
			}
			entry, ok := lang.CapabilityRefusalByCode(tc.code)
			if !ok {
				t.Fatalf("the CLI invented a code the registry does not have: %q", tc.code)
			}
			if entry.Suggestion != payload.Capability.Suggestion {
				t.Fatalf("%q's suggestion differs between --capabilities and the payload", tc.code)
			}
		})
	}
}

// TestCLIFailureWithoutACapabilityCodeNamesNothing is the other half: a program's own error is not a
// missing lowering, and a payload that dressed it up as one would teach an agent to file a bug against
// the compiler for a typo. `capability` is null — present, and saying it looked.
func TestCLIFailureWithoutACapabilityCodeNamesNothing(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		`print(v)` + "\n",                      // an undefined name: the program's error
		"print(1 +\n",                          // does not parse
		"a, b = 1\nprint(a)\n",                 // unpacking a non-sequence: the reference raises too
		"def f(x):\n    return x\n\nf(1, 2)\n", // too many arguments
	} {
		out, code := cliExit(t, "--json", "--eval", src)
		if code == 0 {
			t.Skipf("the program compiles, so there is no failure payload to read: %s", src)
		}
		if code != exitCompileError {
			t.Fatalf("expected exit %d reading %q, got %d:\n%s", exitCompileError, src, code, out)
		}
		if strings.Contains(out, `"code": "lower.unsupported`) {
			t.Fatalf("a program's own error was given a capability code:\n%s", out)
		}
		if !strings.Contains(out, `"capability":null`) {
			t.Fatalf("the payload should say it looked and found no capability refusal:\n%s", out)
		}
	}
}

// TestCLICapabilityCodesAreNotInvented is the classifier's contract at the CLI: no marker, no code. The
// unit test asserts it over sentences; this asserts it over a run.
func TestCLICapabilityRefusalTextRoundTrips(t *testing.T) {
	t.Parallel()
	for _, k := range lang.CapabilityRefusalKinds() {
		k := k
		t.Run(k.Code, func(t *testing.T) {
			t.Parallel()
			out, code := cliExit(t, "--json", "--emit-llvm", k.Sample)
			if code == 0 {
				t.Fatalf("the registry's sample compiles clean, so its code is a claim about nothing:\n%s", k.Sample)
			}
			if !strings.Contains(out, `"`+k.Code+`"`) {
				t.Fatalf("the CLI refused this shape without saying which refusal it was:\n%s", out)
			}
		})
	}
}
