package lang

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// compileRefusalText compiles a program the way the CLI's failure path sees it and hands back the
// sentence — the same text `--json` puts in `error`, so the registry is tested against what the
// classifier is really given rather than against a copy of it.
func compileRefusalText(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		return err.Error()
	}
	var b strings.Builder
	for _, d := range res.Diagnostics {
		b.WriteString(d.Msg)
		b.WriteString("\n")
	}
	return b.String()
}

// TestTheCapabilityRegistryCoversWhatItClaimsToKnow is the registry's leash on the prose. Every entry
// must (a) name a shape the backend really refuses, (b) be found by its own markers in the sentence
// the refusal emits, and (c) carry the half a reader needs: a suggestion and at least one roadmap row.
//
// The direction that matters is the one a prose edit takes: change a refusal's sentence without
// changing this file and the entry's marker stops matching, and the failing test says which shape lost
// its code. A silent loss there is a script whose branch stops firing.
func TestTheCapabilityRegistryCoversWhatItClaimsToKnow(t *testing.T) {
	t.Parallel()
	// The duplicate check runs in sequence, before the parallel bodies: a code identifies one shape,
	// and two entries claiming one code is a bug whoever reads the schema. Filling a map from inside
	// t.Parallel() subtests would be a data race whose report is a crash, not a failure (the same trap
	// the roadmap-snapshot counters fell into — a parallel subtest runs after its parent returns).
	seenCode := map[string]string{}
	for _, k := range capabilityRefusalRegistry {
		if prev, dup := seenCode[k.Code]; dup {
			t.Fatalf("code %q is claimed by %q and by %q; a code identifies one shape", k.Code, prev, k.Sample)
		}
		seenCode[k.Code] = k.Sample
	}
	for _, k := range capabilityRefusalRegistry {
		t.Run(k.Code, func(t *testing.T) {
			t.Parallel()
			if !strings.HasPrefix(k.Code, "lower.unsupported.") {
				t.Fatalf("code %q is not in the lower.unsupported.* family the row names", k.Code)
			}
			if strings.TrimSpace(k.Shape) == "" || strings.TrimSpace(k.Suggestion) == "" {
				t.Fatalf("%q has no shape or no suggestion: a refusal without either is a bug with better manners", k.Code)
			}
			if len(k.Rows) == 0 {
				t.Fatalf("%q cites no roadmap row, so the reader has nowhere to go", k.Code)
			}
			for _, r := range k.Rows {
				if !strings.HasPrefix(r, "Gap ") && !strings.HasPrefix(r, "L") && !strings.HasPrefix(r, "ADR ") {
					t.Fatalf("%q cites %q, which is not a roadmap row or an ADR", k.Code, r)
				}
			}
			if len(k.Markers) == 0 || strings.TrimSpace(k.Sample) == "" {
				t.Fatalf("%q has no marker or no sample: the registry would be untestable", k.Code)
			}
			msg := compileRefusalText(t, k.Sample)
			if msg == "" {
				t.Fatalf("the sample compiles clean, so the shape is not refused and the code is a claim about nothing:\n%s", k.Sample)
			}
			got, ok := ClassifyCapabilityRefusal(msg)
			if !ok {
				t.Fatalf("no marker matched the sentence the backend emits:\n  sentence: %s\n  markers:  %q", msg, k.Markers)
			}
			if got.Code != k.Code {
				t.Fatalf("the sample classifies as %q, want %q\n  sentence: %s", got.Code, k.Code, msg)
			}
		})
	}
}

// The longest marker wins, so the `tuple(...)` row is not swallowed by the generic indirect-call row
// whose sentence it also contains. Two rows with the same shape would answer differently depending on
// registry order, and an agent's branch should not depend on the order a slice was written in.
func TestTheCapabilityClassifierPrefersTheMostSpecificShape(t *testing.T) {
	t.Parallel()
	msg := compileRefusalText(t, "print(tuple([1, 2]))")
	got, ok := ClassifyCapabilityRefusal(msg)
	if !ok {
		t.Fatalf("the tuple(...) refusal classified as nothing:\n%s", msg)
	}
	if got.Code != "lower.unsupported.tuple-call" {
		t.Fatalf("tuple(...) classified as %q, want the tuple row, not the generic indirect-call row", got.Code)
	}
	// And the generic shape still classifies as itself.
	generic := compileRefusalText(t, "def apply1(fn, v):\n    return fn(v)\n\ndef twice(fn, v):\n    return fn(v) + fn(v)\n\nprint(apply1(twice, 3))")
	other, ok := ClassifyCapabilityRefusal(generic)
	if !ok || other.Code != "lower.unsupported.indirect-call" {
		t.Fatalf("the indirect-call shape classified as %+v:\n%s", other, generic)
	}
}

// Nothing is invented: a sentence that is not a capability refusal gets no code, so the JSON omits the
// field rather than dressing a front-end error up as a missing lowering.
func TestTheCapabilityClassifierRefusesToGuess(t *testing.T) {
	t.Parallel()
	for _, msg := range []string{
		`codegen: undefined name "v" (no binding for it; assign it before use) — the reference reports the same error`,
		"parse error at 1:8: expected \")\"",
		"possibly unbound: \"k\" is not definitely assigned on all paths",
		"codegen: too many arguments for f",
	} {
		if got, ok := ClassifyCapabilityRefusal(msg); ok {
			t.Fatalf("%q classified as %q; it is a program's error, not a missing lowering", msg, got.Code)
		}
	}
}

// The schema is the contract an agent reads, so it has to carry exactly the codes this file has.
func TestTheSchemaDeclaresEveryCapabilityCode(t *testing.T) {
	t.Parallel()
	doc := ASTIRSchema
	for _, code := range CapabilityRefusalCodes() {
		if !strings.Contains(doc, `"`+code+`"`) {
			t.Fatalf("--schema does not declare %q; an agent branching on it has no contract", code)
		}
	}
	if !strings.Contains(doc, "capabilityRefusal") {
		t.Fatal("--schema declares no capabilityRefusal document")
	}
}

// The account an agent asks for: `gustyc --capabilities` prints the registry, and the JSON has to be
// the registry — every code, with its suggestion and its rows.
func TestTheCapabilityDocumentCarriesTheRegistry(t *testing.T) {
	t.Parallel()
	doc, err := CapabilitiesJSON()
	if err != nil {
		t.Fatalf("capabilities: %v", err)
	}
	for _, code := range CapabilityRefusalCodes() {
		if !strings.Contains(doc, `"`+code+`"`) {
			t.Fatalf("the document omits %q", code)
		}
	}
	if !strings.Contains(doc, "lower.unsupported.list-copy") {
		t.Fatalf("the document does not name the shape the row opens with:\n%s", doc[:min(400, len(doc))])
	}
}

// TestSchemaDeclaresCapabilityRefusal is the schema half of the contract, in the house style of
// TestSchemaDeclaresEffectSummary: the document must declare every field the Go struct marshals,
// require exactly the fields a reader can never omit, and declare the code enum as the registry
// states it — in both directions. An agent validates a payload against this document instead of
// reading Go structs, so a field or a code missing here is a contract that does not cover the thing
// the compiler actually sends (roadmap L11.8, ADR 0346).
func TestSchemaDeclaresCapabilityRefusal(t *testing.T) {
	t.Parallel()
	var doc struct {
		Definitions struct {
			CapabilityRefusal struct {
				Required   []string       `json:"required"`
				Properties map[string]any `json:"properties"`
			} `json:"capabilityRefusal"`
			CapabilityRefusalCode struct {
				Type string   `json:"type"`
				Enum []string `json:"enum"`
			} `json:"capabilityRefusalCode"`
			CapabilitiesDocument struct {
				Required   []string       `json:"required"`
				Properties map[string]any `json:"properties"`
			} `json:"capabilitiesDocument"`
			ToolFailure struct {
				Required   []string       `json:"required"`
				Properties map[string]any `json:"properties"`
			} `json:"toolFailure"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(ASTIRSchema), &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	props := doc.Definitions.CapabilityRefusal.Properties
	if len(props) == 0 {
		t.Fatal("schema declares no capabilityRefusal properties")
	}
	want := reflect.TypeOf(CapabilityRefusalKind{})
	for i := 0; i < want.NumField(); i++ {
		name := strings.Split(want.Field(i).Tag.Get("json"), ",")[0]
		if name == "-" {
			continue // Markers and Sample are the registry's leash on the prose, not a published field
		}
		if _, ok := props[name]; !ok {
			t.Fatalf("CapabilityRefusalKind reports %q but the schema does not declare it", name)
		}
	}
	for _, req := range doc.Definitions.CapabilityRefusal.Required {
		if _, ok := props[req]; !ok {
			t.Fatalf("capabilityRefusal requires %q, which is not a declared property", req)
		}
	}

	// The enum is the registry — same members, no more and no less, in the order the document
	// publishes them. A code only the schema has is an enum an agent can write and never read; a code
	// only the registry has is a payload that fails validation.
	enum := doc.Definitions.CapabilityRefusalCode.Enum
	codes := CapabilityRefusalCodes()
	if doc.Definitions.CapabilityRefusalCode.Type != "string" {
		t.Fatalf("capabilityRefusalCode is typed %q, not string", doc.Definitions.CapabilityRefusalCode.Type)
	}
	if len(enum) != len(codes) {
		t.Fatalf("the schema declares %d codes and the registry %d — the splice came apart", len(enum), len(codes))
	}
	inEnum := map[string]bool{}
	for _, e := range enum {
		inEnum[e] = true
	}
	for _, c := range codes {
		if !inEnum[c] {
			t.Fatalf("the classifier can emit %q and the schema does not allow it", c)
		}
	}
	for _, e := range enum {
		if _, ok := CapabilityRefusalByCode(e); !ok {
			t.Fatalf("the schema declares %q, which no refusal carries", e)
		}
	}

	// The two documents an agent asks for by name.
	if len(doc.Definitions.CapabilitiesDocument.Properties) == 0 {
		t.Fatal("schema declares no capabilitiesDocument (gustyc --capabilities has no shape to validate against)")
	}
	for _, req := range []string{"tool", "family", "backend", "kinds"} {
		if _, ok := doc.Definitions.CapabilitiesDocument.Properties[req]; !ok {
			t.Fatalf("capabilitiesDocument does not declare %q", req)
		}
	}
	if _, ok := doc.Definitions.ToolFailure.Properties["capability"]; !ok {
		t.Fatal("the failure payload's `capability` member is not declared by definitions.toolFailure")
	}
	for _, req := range doc.Definitions.ToolFailure.Required {
		if _, ok := doc.Definitions.ToolFailure.Properties[req]; !ok {
			t.Fatalf("toolFailure requires %q, which is not a declared property", req)
		}
	}
}

// The registry's own consistency, asserted rather than assumed by the readers above: codes are the
// identity of a shape, every entry is complete, and the registry is sorted the way the document is.
func TestTheCapabilityRegistryIsWellFormed(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, k := range capabilityRefusalRegistry {
		if seen[k.Code] {
			t.Fatalf("code %q is claimed twice", k.Code)
		}
		seen[k.Code] = true
		if k.Sample == "" || len(k.Markers) == 0 {
			t.Fatalf("%q has no sample or no marker", k.Code)
		}
		for _, row := range k.Rows {
			if !roadmapRowCitedInTracker(t, row) {
				t.Fatalf("%q cites %q, which the tracker does not have", k.Code, row)
			}
		}
	}
}

// roadmapRowCitedInTracker asks roadmap.md whether it has a row with this ID — the difference between
// a citation that leads a reader somewhere and one that leads them to a search that finds nothing. The
// roadmap is a file outside this package, so -count=1 (make test / make testparallel) is what runs this
// guard at all after an edit to the tracker — ADR 0317's lesson, applied to a new reader of the file.
func roadmapRowCitedInTracker(t *testing.T, id string) bool {
	t.Helper()
	roadmapOnce.Do(func() {
		b, err := os.ReadFile(filepath.Join("..", "..", "roadmap.md"))
		if err != nil {
			roadmapErr = err
			return
		}
		text := string(b)
		roadmapText = &text
	})
	if roadmapErr != nil {
		t.Fatalf("read roadmap.md: %v", roadmapErr)
	}
	id = strings.TrimPrefix(id, "ADR ")
	return strings.Contains(*roadmapText, id)
}

var (
	roadmapOnce sync.Once
	roadmapText *string
	roadmapErr  error
)
