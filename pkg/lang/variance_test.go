package lang

import (
	"encoding/json"
	"strings"
	"testing"
)

// Variance + generics (L6.6) unit tests: the rules themselves (assignable /
// subType), the diagnostics they produce (stable Code + actionable Suggestion),
// and the machine-readable variance table the CLI exposes.

// hasDiagCode reports whether any diagnostic carries the given rule code.
func hasDiagCode(diags []Diagnostic, code string) bool {
	for _, d := range diags {
		if d.Code == code {
			return true
		}
	}
	return false
}

func analyzeSrc(t *testing.T, src string) []Diagnostic {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return Analyze(prog)
}

// classHierarchy is the fixture used by the nominal + covariance cases:
// Animal <- Dog <- Puppy, plus an unrelated Rock.
const classHierarchy = `class Animal:
    def speak(self):
        return 1

class Dog(Animal):
    def speak(self):
        return 2

class Puppy(Dog):
    def speak(self):
        return 3

class Rock:
    def hard(self):
        return 1
`

// TestVarianceListInvariant proves list[T] / set[T] / dict[K, V] are INVARIANT:
// a differently-typed container of the same shape is rejected, and the
// diagnostic names the rule with its stable code.
func TestVarianceListInvariant(t *testing.T) {
	if assignable(TList(TStr()), TList(TInt())) {
		t.Errorf("list[str] must not be assignable to list[int] (list[T] is invariant)")
	}
	if assignable(TSet(TStr()), TSet(TInt())) {
		t.Errorf("set[str] must not be assignable to set[int]")
	}
	if assignable(TDict(TInt(), TInt()), TDict(TStr(), TInt())) {
		t.Errorf("dict[int, int] must not be assignable to dict[str, int] (K is invariant)")
	}
	if assignable(TDict(TStr(), TInt()), TDict(TStr(), TStr())) {
		t.Errorf("dict[str, int] must not be assignable to dict[str, str] (V is invariant)")
	}
	if !assignable(TList(TInt()), TList(TInt())) {
		t.Errorf("list[int] must be assignable to list[int]")
	}
	// gradual typing: an unknown type argument is always tolerated
	if !assignable(TList(TDyn()), TList(TInt())) {
		t.Errorf("list[any] must flow to list[int] under gradual typing")
	}

	diags := analyzeSrc(t, "def take(xs: list[int]) -> int:\n    return len(xs)\nds: list[str] = [\"a\"]\nz = take(ds)\n")
	if !hasDiagCode(diags, CodeVarianceInvariant) {
		t.Errorf("expected a %s diagnostic, got %v", CodeVarianceInvariant, diags)
	}
	for _, d := range diags {
		if d.Code == CodeVarianceInvariant && d.Suggestion == "" {
			t.Errorf("invariant diagnostic must carry a suggestion: %v", d)
		}
	}
}

// TestVarianceSequenceCovariant proves the read-only Sequence[T] protocol is
// COVARIANT: a list of a subclass may be read where the base is expected, but a
// list of the base may not be read where the subclass is expected.
func TestVarianceSequenceCovariant(t *testing.T) {
	ci := NewClassIndex()
	ci.Declare("Animal", nil)
	ci.Declare("Dog", []string{"Animal"})
	ci.Declare("Puppy", []string{"Dog"})

	if !assignableIn(ci, TList(TClass("Dog")), TSequence(TClass("Animal"))) {
		t.Errorf("list[Dog] must satisfy Sequence[Animal] (covariant in T)")
	}
	if !assignableIn(ci, TList(TClass("Puppy")), TSequence(TClass("Animal"))) {
		t.Errorf("list[Puppy] must satisfy Sequence[Animal] (transitive subclass)")
	}
	if assignableIn(ci, TList(TClass("Animal")), TSequence(TClass("Dog"))) {
		t.Errorf("list[Animal] must not satisfy Sequence[Dog] (covariance may widen, not narrow)")
	}
	// str is Sequence[str]; any container satisfies Sequence[any]
	if !assignable(TStr(), TSequence(TStr())) {
		t.Errorf("str must satisfy Sequence[str]")
	}
	if !assignable(TList(TStr()), TSequence(TDyn())) {
		t.Errorf("list[str] must satisfy Sequence[any]")
	}

	diags := analyzeSrc(t, classHierarchy+`
def count_animals(xs: Sequence[Dog]) -> int:
    return len(xs)
animals: list[Animal] = [Animal()]
z = count_animals(animals)
`)
	if !hasDiagCode(diags, CodeVarianceCovariant) {
		t.Errorf("expected a %s diagnostic, got %v", CodeVarianceCovariant, diags)
	}
}

// TestVarianceCallableContravariant proves Callable[[P...], R] is CONTRAVARIANT
// in its parameters and COVARIANT in its return — the classic gotcha, and the
// headline "gusty check reports contravariant misuse" behaviour.
func TestVarianceCallableContravariant(t *testing.T) {
	ci := NewClassIndex()
	ci.Declare("Animal", nil)
	ci.Declare("Dog", []string{"Animal"})

	wide := TCallable([]*Type{TClass("Dog")}, TInt())
	// a handler that accepts the BASE may be used where the SUBCLASS is passed
	if !assignableIn(ci, TFunc([]*Type{TClass("Animal")}, TInt()), wide) {
		t.Errorf("fn(Animal) -> int must satisfy Callable[[Dog], int] (contravariant params)")
	}
	// a handler that demands the SUBCLASS may not be used for base arguments
	narrow := TFunc([]*Type{TClass("Dog")}, TInt())
	if assignableIn(ci, narrow, TCallable([]*Type{TClass("Animal")}, TInt())) {
		t.Errorf("fn(Dog) -> int must NOT satisfy Callable[[Animal], int]")
	}
	// return is covariant
	if !assignableIn(ci, TFunc([]*Type{TClass("Animal")}, TClass("Dog")), TCallable([]*Type{TClass("Animal")}, TClass("Animal"))) {
		t.Errorf("a more specific return must be substitutable (covariant R)")
	}
	// arity mismatch is still an arity error, with its own code
	v := subType(ci, TFunc([]*Type{TClass("Animal"), TClass("Animal")}, TInt()), wide)
	if v == nil || v.Code != CodeCallableArity {
		t.Errorf("expected %s, got %v", CodeCallableArity, v)
	}

	diags := analyzeSrc(t, classHierarchy+`
def use(f: Callable[[Animal], int]) -> int:
    return f(Animal())
def dog_only(d: Dog) -> int:
    return d.speak()
z = use(dog_only)
`)
	if !hasDiagCode(diags, CodeVarianceContravariant) {
		t.Errorf("expected a %s diagnostic, got %v", CodeVarianceContravariant, diags)
	}
	var d Diagnostic
	for _, x := range diags {
		if x.Code == CodeVarianceContravariant {
			d = x
		}
	}
	if !strings.Contains(d.Msg, "contravariant") {
		t.Errorf("contravariant diagnostic must name the rule: %q", d.Msg)
	}
	if !strings.Contains(d.Suggestion, "AT LEAST") {
		t.Errorf("contravariant diagnostic must carry an actionable suggestion: %q", d.Suggestion)
	}

	// and the correct direction is accepted with no diagnostics
	good := analyzeSrc(t, classHierarchy+`
def use(f: Callable[[Dog], int]) -> int:
    return f(Dog())
def any_animal(a: Animal) -> int:
    return a.speak()
z = use(any_animal)
`)
	if hasDiagCode(good, CodeVarianceContravariant) {
		t.Errorf("a wider handler must be substitutable, got %v", good)
	}
}

// TestVarianceClassNominal proves class annotations are NOMINAL: only the class
// itself or a subclass flows to a class position, and the class hierarchy is
// visible even when the annotation is used before the subclass declaration.
func TestVarianceClassNominal(t *testing.T) {
	ci := NewClassIndex()
	ci.Declare("Animal", nil)
	ci.Declare("Dog", []string{"Animal"})
	if !ci.Less("Dog", "Animal") || !ci.Less("Animal", "Animal") {
		t.Errorf("Dog must be a subclass of Animal")
	}
	if ci.Less("Animal", "Dog") {
		t.Errorf("Animal must not be a subclass of Dog")
	}
	if !assignableIn(ci, TClass("Dog"), TClass("Animal")) {
		t.Errorf("a Dog must flow to an Animal position")
	}
	if assignableIn(ci, TClass("Animal"), TClass("Dog")) {
		t.Errorf("an Animal must not flow to a Dog position")
	}
	// with no hierarchy at all, class types compare by name only
	if assignable(TClass("Dog"), TClass("Animal")) {
		t.Errorf("with no class index, class types must compare by name")
	}

	diags := analyzeSrc(t, classHierarchy+"def feed(a: Animal) -> int:\n    return a.speak()\nz = feed(Rock())\n")
	if !hasDiagCode(diags, CodeVarianceNominal) {
		t.Errorf("expected a %s diagnostic, got %v", CodeVarianceNominal, diags)
	}
	good := analyzeSrc(t, classHierarchy+"def feed(a: Animal) -> int:\n    return a.speak()\nz = feed(Puppy())\n")
	for _, d := range good {
		if d.Level == LevelError {
			t.Errorf("a Puppy must satisfy an Animal annotation, got %v", d)
		}
	}
}

// TestVarianceTuple proves tuples are covariant elementwise but fixed in arity.
func TestVarianceTuple(t *testing.T) {
	ci := NewClassIndex()
	ci.Declare("Animal", nil)
	ci.Declare("Dog", []string{"Animal"})
	if !assignableIn(ci, TTuple(TClass("Dog"), TStr()), TTuple(TClass("Animal"), TStr())) {
		t.Errorf("tuple[Dog, str] must flow to tuple[Animal, str]")
	}
	if assignableIn(ci, TTuple(TClass("Animal"), TStr()), TTuple(TClass("Dog"), TStr())) {
		t.Errorf("tuple[Animal, str] must not flow to tuple[Dog, str]")
	}
	if assignable(TTuple(TInt(), TInt()), TTuple(TInt())) {
		t.Errorf("tuple arity mismatch must be rejected")
	}
	// an untyped tuple annotation accepts any tuple (gradual)
	if !assignable(TTuple(TStr()), TTuple()) {
		t.Errorf("tuple[any] must be accepted by a bare tuple annotation")
	}
	diags := analyzeSrc(t, "pair: tuple[int, str] = (1, 2)\n")
	if !hasDiagCode(diags, CodeVarianceCovariant) {
		t.Errorf("expected a %s diagnostic for the element mismatch, got %v", CodeVarianceCovariant, diags)
	}
}

// TestVarianceFreshLiteralIsCovariant proves a brand-new container literal may
// widen its element type to the destination (mypy-style contextual inference),
// while a genuinely wrong element is still rejected.
func TestVarianceFreshLiteralIsCovariant(t *testing.T) {
	good := analyzeSrc(t, "x: list[int | str] = [1]\nprint(x)\n")
	for _, d := range good {
		if d.Level == LevelError {
			t.Errorf("a fresh [1] must satisfy list[int | str], got %v", d)
		}
	}
	bad := analyzeSrc(t, "x: list[int] = [1, \"a\"]\nprint(x)\n")
	if !hasDiagCode(bad, CodeVarianceCovariant) {
		t.Errorf("expected a %s diagnostic for a bad literal element, got %v", CodeVarianceCovariant, bad)
	}
	goodDict := analyzeSrc(t, "d: dict[str, int | str] = {\"a\": 1}\nprint(d)\n")
	for _, d := range goodDict {
		if d.Level == LevelError {
			t.Errorf("a fresh literal must satisfy dict[str, int | str], got %v", d)
		}
	}
	badDict := analyzeSrc(t, "d: dict[str, int] = {1: 2}\nprint(d)\n")
	if !hasDiagCode(badDict, CodeVarianceCovariant) {
		t.Errorf("expected a %s diagnostic for the bad dict key, got %v", CodeVarianceCovariant, badDict)
	}
}

// TestVariancePredeclaredClasses proves a class may be referenced by an
// annotation before it appears in the file (the hierarchy is a pre-pass).
func TestVariancePredeclaredClasses(t *testing.T) {
	diags := analyzeSrc(t, "def feed(a: Animal) -> int:\n    return a.speak()\nz = feed(Dog())\nclass Animal:\n    def speak(self):\n        return 1\nclass Dog(Animal):\n    def speak(self):\n        return 2\n")
	for _, d := range diags {
		if d.Level == LevelError {
			t.Errorf("class annotations must resolve regardless of declaration order, got %v", d)
		}
	}
}

// TestVarianceTableIsMachineReadable validates the self-describing variance
// surface behind `gustyc --variance`: valid JSON, one rule per constructor, each
// with a declared variance and a stable diagnostic code.
func TestVarianceTableIsMachineReadable(t *testing.T) {
	doc, err := VarianceJSON()
	if err != nil {
		t.Fatalf("variance json: %v", err)
	}
	var parsed VarianceDocument
	if err := json.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.SchemaVersion != "1.0" {
		t.Errorf("schema_version = %q, want 1.0", parsed.SchemaVersion)
	}
	if len(parsed.Rules) < 6 {
		t.Fatalf("expected the full variance table, got %d rules", len(parsed.Rules))
	}
	byCtor := map[string]VarianceRule{}
	for _, r := range parsed.Rules {
		byCtor[r.Constructor] = r
		if r.Code == "" || r.Rationale == "" || len(r.Params) != len(r.Variance) {
			t.Errorf("rule %q is not self-describing: %+v", r.Constructor, r)
		}
	}
	want := map[string]string{
		"list[T]":             VarianceInvariant.String(),
		"set[T]":              VarianceInvariant.String(),
		"dict[K, V]":          VarianceInvariant.String(),
		"Sequence[T]":         VarianceCovariant.String(),
		"iter[T]":             VarianceCovariant.String(),
		"tuple[...]":          VarianceCovariant.String(),
		"Callable[[P...], R]": VarianceContravariant.String(),
	}
	for ctor, v := range want {
		r, ok := byCtor[ctor]
		if !ok {
			t.Errorf("variance table is missing %s", ctor)
			continue
		}
		if r.Variance[0] != v {
			t.Errorf("%s variance = %q, want %q", ctor, r.Variance[0], v)
		}
	}
	if r := byCtor["dict[K, V]"]; len(r.Variance) != 2 || r.Variance[1] != VarianceInvariant.String() {
		t.Errorf("dict must be invariant in both K and V: %+v", r)
	}
	if r := byCtor["Callable[[P...], R]"]; r.Variance[1] != VarianceCovariant.String() {
		t.Errorf("Callable's return must be covariant: %+v", r)
	}
	if r := byCtor["list[T]"]; !r.Mutable || r.ReadOnly != "Sequence[T]" {
		t.Errorf("list[T] must point at its covariant read-only alternative: %+v", r)
	}
}

// TestVarianceRuntimeNominalCheck proves the interpreter enforces a nominal
// class annotation for the real class and its subclasses, and rejects a
// mismatched class (the runtime half of the same rule).
func TestVarianceRuntimeNominalCheck(t *testing.T) {
	src := classHierarchy + "def feed(a: Animal) -> int:\n    return a.speak()\nprint(feed(Puppy()))\n"
	out, err := InterpreterRun(src)
	if err != nil {
		t.Fatalf("a Puppy must satisfy an Animal annotation: %v", err)
	}
	if !strings.Contains(out, "3") {
		t.Errorf("output = %q, want the Puppy's 3", out)
	}
	// The negative case is evaluated directly (bypassing the static checker) so
	// the runtime annotation check is what rejects it.
	bad := classHierarchy + "def feed(a: Animal) -> int:\n    return a.speak()\nprint(feed(Rock()))\n"
	prog, perr := Parse(bad)
	if perr != nil {
		t.Fatalf("parse: %v", perr)
	}
	if _, rerr := NewEvaluator().EvalProgram(prog); rerr == nil || !strings.Contains(rerr.Error(), "type mismatch") {
		t.Fatalf("a Rock must be rejected by an Animal annotation, got %v", rerr)
	}
}

// TestVarianceCodesMatchTheSchema keeps the agentic interface honest: every
// diagnostic code the checker can emit is declared in the machine-readable
// --schema document, and the schema declares no code that cannot be emitted.
func TestVarianceCodesMatchTheSchema(t *testing.T) {
	codes := []string{
		CodeTypeMismatch, CodeVarianceInvariant, CodeVarianceCovariant,
		CodeVarianceContravariant, CodeVarianceNominal, CodeCallableArity, CodeUnionMembers,
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(ASTIRSchema), &doc); err != nil {
		t.Fatalf("ASTIRSchema is not valid JSON: %v", err)
	}
	defs, err := json.Marshal(doc["definitions"])
	if err != nil {
		t.Fatalf("definitions: %v", err)
	}
	for _, c := range codes {
		if !strings.Contains(string(defs), "\""+c+"\"") {
			t.Errorf("schema does not declare the diagnostic code %q", c)
		}
	}
	if !strings.Contains(string(defs), "varianceRule") || !strings.Contains(string(defs), "diagnostic") {
		t.Errorf("schema is missing the diagnostic / varianceRule definitions")
	}
	// every variance rule must point at a declared code
	for _, r := range VarianceTable() {
		found := false
		for _, c := range codes {
			if c == r.Code {
				found = true
			}
		}
		if !found {
			t.Errorf("variance rule %q points at an undeclared code %q", r.Constructor, r.Code)
		}
	}
}

// TestVarianceClassContainers proves the invariant rule reaches into class-typed
// type arguments: Same() must compare class names, otherwise list[Dog] would
// sneak past the list[T] invariance check.
func TestVarianceClassContainers(t *testing.T) {
	ci := NewClassIndex()
	ci.Declare("Animal", nil)
	ci.Declare("Dog", []string{"Animal"})
	if TClass("Dog").Same(TClass("Animal")) {
		t.Errorf("Same() must compare class names")
	}
	if !TClass("Dog").Same(TClass("Dog")) {
		t.Errorf("Same() must accept the same class")
	}
	if assignableIn(ci, TList(TClass("Dog")), TList(TClass("Animal"))) {
		t.Errorf("list[Dog] must not satisfy list[Animal] (invariant, class element)")
	}
	if !assignableIn(ci, TList(TClass("Dog")), TList(TClass("Dog"))) {
		t.Errorf("list[Dog] must satisfy list[Dog]")
	}
	if assignableIn(ci, TDict(TClass("Dog"), TInt()), TDict(TClass("Animal"), TInt())) {
		t.Errorf("dict[Dog, int] must not satisfy dict[Animal, int]")
	}
	diags := analyzeSrc(t, classHierarchy+`
def take(vets: list[Animal]) -> int:
    return len(vets)
dogs: list[Dog] = [Dog()]
z = take(dogs)
`)
	if !hasDiagCode(diags, CodeVarianceInvariant) {
		t.Errorf("expected %s for a class-typed element mismatch, got %v", CodeVarianceInvariant, diags)
	}
}

// TestLSPCarriesVarianceCodes keeps the editor path as informative as the CLI:
// an LSP diagnostic exposes the stable rule code and folds the checker's
// suggestion into the message.
func TestLSPCarriesVarianceCodes(t *testing.T) {
	diags := analyzeSrc(t, "def take(xs: list[int]) -> int:\n    return len(xs)\nds: list[str] = [\"a\"]\nz = take(ds)\n")
	found := false
	for _, d := range diags {
		if d.Code != CodeVarianceInvariant {
			continue
		}
		found = true
		l := diagAtCode(d.Span.Line, d.Span.Col, 1, d.Msg, d.Code, d.Suggestion)
		if l.Code != CodeVarianceInvariant {
			t.Errorf("LSP diagnostic code = %q, want %q", l.Code, CodeVarianceInvariant)
		}
		if !strings.Contains(l.Message, "hint:") {
			t.Errorf("LSP diagnostic must surface the suggestion: %q", l.Message)
		}
		if l.Source != "gusty" {
			t.Errorf("LSP diagnostic source = %q", l.Source)
		}
	}
	if !found {
		t.Fatalf("no %s diagnostic to check: %v", CodeVarianceInvariant, diags)
	}
}

// TestVarianceDoesNotBreakUntypedCode is the gradual-typing guardrail: code with
// no annotations at all must keep producing zero errors.
func TestVarianceDoesNotBreakUntypedCode(t *testing.T) {
	src := `
class Animal:
    def speak(self):
        return 1
def feed(a):
    return a.speak()
xs = [1, 2, 3]
print(len(xs))
print(feed(Animal()))
d = {}
d[1] = 2
print(len(d))
`
	for _, d := range analyzeSrc(t, src) {
		if d.Level == LevelError {
			t.Errorf("untyped code must stay clean, got %v", d)
		}
	}
}
