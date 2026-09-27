package lang

// Variance + generics (L6.6).
//
// Every generic constructor in the language has a declared variance for each of
// its type parameters. Variance is what makes one substitution sound and
// another unsound:
//
//	def feed(a: Animal) -> None: ...
//	feed(Dog())                 # OK    — class types are nominal + covariant
//
//	ls: list[Animal] = []
//	ld: list[Dog] = [Dog()]
//	ls = ld                     # ERROR — list[T] is INVARIANT in T
//
//	def use(f: Callable[[Animal], int]) -> None: ...
//	use(dogOnly)                # ERROR — Callable is CONTRAVARIANT in params
//	use(anything)               # OK    — a wider handler is substitutable
//
// The rules, implemented in one place (subType) and shared by the checker, the
// docs and the CLI (`gustyc --variance`):
//
//   - MUTABLE containers — list[T], set[T], dict[K, V] — are INVARIANT. The
//     destination can write through the container, so the type arguments must
//     match exactly (dynamic type arguments stay tolerated, per gradual typing).
//   - READ-ONLY protocols — Sequence[T], iter[T] — are COVARIANT: elements only
//     flow out, so the element type may be widened, never narrowed. This is the
//     escape hatch the invariant diagnostic points at.
//   - Tuples are immutable, hence COVARIANT elementwise (arity must match).
//   - Callables — Callable[[P...], R] and fn types — are CONTRAVARIANT in their
//     parameters and COVARIANT in their return. The parameter direction is the
//     one callers get wrong most often, so it is named in the diagnostic.
//   - Class types are NOMINAL: a value reaches a class position only if it is
//     that class or a subclass of it, walking the declared base chain.
//
// Every violation carries a stable machine-readable code (Diagnostic.Code) and
// an actionable suggestion, so an agent can branch on the rule instead of
// scraping prose.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// Variance classifies how one type parameter of a generic constructor flows.
type Variance int

const (
	// VarianceInvariant: the type arguments must match exactly.
	VarianceInvariant Variance = iota
	// VarianceCovariant: the argument may be widened (substituted by a subtype).
	VarianceCovariant
	// VarianceContravariant: the argument may be narrowed (a wider input is accepted).
	VarianceContravariant
)

func (v Variance) String() string {
	switch v {
	case VarianceCovariant:
		return "covariant"
	case VarianceContravariant:
		return "contravariant"
	default:
		return "invariant"
	}
}

// RuleKind names the variance rule a failed assignment broke. It is part of the
// machine-readable contract: Diagnostic.Code is derived from it.
type RuleKind string

const (
	RuleKindMismatch  RuleKind = "kind"
	RuleInvariant     RuleKind = "invariant"
	RuleCovariant     RuleKind = "covariant"
	RuleContravariant RuleKind = "contravariant"
	RuleNominal       RuleKind = "nominal"
	RuleArity         RuleKind = "arity"
	RuleMembers       RuleKind = "members"
)

// Diagnostic codes for variance + generics violations (documented in
// docs/operations.md; stable across releases).
const (
	CodeTypeMismatch          = "type.mismatch"
	CodeVarianceInvariant     = "type.variance.invariant"
	CodeVarianceCovariant     = "type.variance.covariant"
	CodeVarianceContravariant = "type.variance.contravariant"
	CodeVarianceNominal       = "type.variance.nominal"
	CodeCallableArity         = "type.callable.arity"
	CodeUnionMembers          = "type.union.members"
)

// Violation explains why a value of one type could not flow into a position of
// another. A nil *Violation means the substitution is sound.
type Violation struct {
	Kind       RuleKind `json:"kind"`
	Msg        string   `json:"msg"`
	Code       string   `json:"code"`
	Suggestion string   `json:"suggestion,omitempty"`
}

// Error renders the violation as a single human-readable clause.
func (v *Violation) Error() string {
	if v == nil {
		return ""
	}
	return v.Msg
}

// ClassIndex records the declared class hierarchy so nominal subtyping can walk
// the base chain.
type ClassIndex struct {
	bases map[string][]string
}

// NewClassIndex returns an empty class hierarchy.
func NewClassIndex() *ClassIndex { return &ClassIndex{bases: map[string][]string{}} }

// Declare records that `name` directly inherits `bases`.
func (ci *ClassIndex) Declare(name string, bases []string) {
	if ci == nil {
		return
	}
	if ci.bases == nil {
		ci.bases = map[string][]string{}
	}
	ci.bases[name] = append([]string{}, bases...)
}

// Known reports whether the class was declared.
func (ci *ClassIndex) Known(name string) bool {
	if ci == nil || ci.bases == nil {
		return false
	}
	_, ok := ci.bases[name]
	return ok
}

// Less reports whether `sub` is `sup` or a (transitive) subclass of it. With no
// hierarchy information it degrades to name equality, so an unset index stays
// conservative rather than accepting anything.
func (ci *ClassIndex) Less(sub, sup string) bool {
	if sub == sup {
		return true
	}
	if ci == nil || ci.bases == nil {
		return false
	}
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(c string) bool {
		if c == sup {
			return true
		}
		if seen[c] {
			return false
		}
		seen[c] = true
		for _, b := range ci.bases[c] {
			if walk(b) {
				return true
			}
		}
		return false
	}
	return walk(sub)
}

// ---------------------------------------------------------------------------
// The subtyping relation
// ---------------------------------------------------------------------------

// subType reports whether a value of type `got` may flow into a position
// declared `want`, applying the variance rules above. It returns nil when the
// substitution is sound, and the explaining violation otherwise. `ci` may be
// nil, in which case class types compare by name only.
func subType(ci *ClassIndex, got, want *Type) *Violation {
	if got == nil || want == nil || got.IsDyn() || want.IsDyn() {
		return nil // gradual typing: an unknown type flows anywhere
	}
	// A union source must flow member by member; a union target needs one hit.
	if got.Kind == KindUnion {
		for _, m := range got.Members {
			if v := subType(ci, m, want); v != nil {
				return v
			}
		}
		return nil
	}
	if want.Kind == KindUnion {
		for _, m := range want.Members {
			if subType(ci, got, m) == nil {
				return nil
			}
		}
		return &Violation{Kind: RuleMembers, Code: CodeUnionMembers,
			Msg:        "no member of " + want.Name() + " accepts " + got.Name(),
			Suggestion: "add " + got.Name() + " to the union, or convert the value before assigning it"}
	}
	// Literal types: Literal[v] is a subtype of int (its base kind), equal
	// literals are mutually substitutable, and a plain int may flow into a
	// Literal position under gradual typing (the runtime enforces the value).
	if got.Kind == KindLiteral {
		if want.Kind == KindLiteral {
			if got.LitVal == want.LitVal {
				return nil
			}
			return &Violation{Kind: RuleKindMismatch, Code: CodeTypeMismatch,
				Msg: "Literal[" + strconv.FormatInt(got.LitVal, 10) + "] is not Literal[" +
					strconv.FormatInt(want.LitVal, 10) + "]"}
		}
		return subType(ci, TInt(), want)
	}
	if want.Kind == KindLiteral {
		if got.Kind == KindInt {
			return nil
		}
		return &Violation{Kind: RuleKindMismatch, Code: CodeTypeMismatch,
			Msg: "expected " + want.Name() + ", got " + got.Name()}
	}

	switch want.Kind {
	case KindSequence:
		return seqSubType(ci, got, want.Elem)
	case KindCallable, KindFunc:
		return callableSubType(ci, got, want)
	case KindClass:
		return classSubType(ci, got, want)
	case KindList, KindSet, KindDict:
		return containerSubType(got, want)
	case KindTuple:
		return tupleSubType(ci, got, want)
	case KindIterator:
		return iteratorSubType(ci, got, want.Elem)
	default:
		if got.Kind == want.Kind {
			return nil
		}
		return &Violation{Kind: RuleKindMismatch, Code: CodeTypeMismatch,
			Msg: "expected " + want.Name() + ", got " + got.Name()}
	}
}

// argPair is one (source, destination) type-argument position of a container.
type argPair struct {
	g, w  *Type
	param string
}

// containerSubType checks list[T] / set[T] / dict[K, V]: INVARIANT in every
// type argument, because all three are mutable.
func containerSubType(got, want *Type) *Violation {
	if got.Kind != want.Kind {
		return &Violation{Kind: RuleKindMismatch, Code: CodeTypeMismatch,
			Msg: "expected " + want.Name() + ", got " + got.Name()}
	}
	var pairs []argPair
	switch want.Kind {
	case KindDict:
		pairs = []argPair{{got.Key, want.Key, "K"}, {got.Val, want.Val, "V"}}
	default:
		pairs = []argPair{{got.Elem, want.Elem, "T"}}
	}
	for _, p := range pairs {
		if p.g == nil || p.w == nil || p.g.IsDyn() || p.w.IsDyn() {
			continue // gradual typing: an unknown type argument is tolerated
		}
		if !p.g.Same(p.w) {
			return &Violation{Kind: RuleInvariant, Code: CodeVarianceInvariant,
				Msg: want.Name() + " is invariant in " + p.param + ": " + p.g.Name() +
					" is not " + p.w.Name(),
				Suggestion: "the destination can WRITE through this container, so the type arguments must match — use the same " +
					p.param + ", drop the type argument, or take a read-only " + readOnlyOf(want) + " view (covariant)"}
		}
	}
	return nil
}

// readOnlyOf names the covariant read-only protocol for a mutable container.
func readOnlyOf(t *Type) string {
	switch t.Kind {
	case KindDict:
		return "Mapping[K, V]"
	case KindSet:
		return "AbstractSet[T]"
	default:
		return "Sequence[T]"
	}
}

// tupleSubType checks tuple[...]: immutable, so COVARIANT elementwise. An
// untyped tuple annotation accepts any tuple; otherwise the arity must match.
func tupleSubType(ci *ClassIndex, got, want *Type) *Violation {
	if got.Kind != KindTuple {
		return &Violation{Kind: RuleKindMismatch, Code: CodeTypeMismatch,
			Msg: "expected " + want.Name() + ", got " + got.Name()}
	}
	if len(want.Elems) == 0 || len(got.Elems) == 0 {
		return nil
	}
	if len(got.Elems) != len(want.Elems) {
		return &Violation{Kind: RuleArity, Code: CodeCallableArity,
			Msg: "tuple arity mismatch: got " + strconv.Itoa(len(got.Elems)) +
				" elements, want " + strconv.Itoa(len(want.Elems))}
	}
	for i := range want.Elems {
		if v := subType(ci, got.Elems[i], want.Elems[i]); v != nil {
			return &Violation{Kind: RuleCovariant, Code: CodeVarianceCovariant,
				Msg: want.Name() + " is covariant at index " + strconv.Itoa(i) +
					": element " + got.Elems[i].Name() + " is not " + nameOf(want.Elems[i]),
				Suggestion: v.Suggestion}
		}
	}
	return nil
}

// iteratorSubType checks iter[T]: a lazy producer that accepts nothing back,
// so COVARIANT in T. Any concrete container can stand in for an iterator.
func iteratorSubType(ci *ClassIndex, got, elem *Type) *Violation {
	switch got.Kind {
	case KindIterator, KindList, KindSet, KindTuple, KindString, KindSequence:
		return seqSubType(ci, got, elem)
	default:
		return &Violation{Kind: RuleKindMismatch, Code: CodeTypeMismatch,
			Msg: "expected an iterator, got " + got.Name()}
	}
}

// seqSubType checks a read-only Sequence[T] target: any concrete container is
// accepted and the element type is checked COVARIANTLY — widening is sound
// (read Dogs where Animals are expected), narrowing is not.
func seqSubType(ci *ClassIndex, got, elem *Type) *Violation {
	if elem == nil || elem.IsDyn() {
		return nil
	}
	var g *Type
	switch got.Kind {
	case KindList, KindSet, KindIterator, KindSequence:
		g = got.Elem
	case KindString:
		g = TStr() // str is Sequence[str]
	case KindTuple:
		if len(got.Elems) == 0 {
			return nil
		}
		for _, e := range got.Elems {
			if v := subType(ci, e, elem); v != nil {
				return seqViolation(got, elem, v)
			}
		}
		return nil
	default:
		return &Violation{Kind: RuleKindMismatch, Code: CodeTypeMismatch,
			Msg: "expected a sequence (Sequence[" + nameOf(elem) + "]), got " + got.Name()}
	}
	if g == nil || g.IsDyn() {
		return nil
	}
	if v := subType(ci, g, elem); v != nil {
		return seqViolation(got, elem, v)
	}
	return nil
}

// seqViolation reports a failed element substitution as a covariance
// violation, since that is the rule a read-only protocol enforces. The inner
// reason (e.g. a nominal class mismatch) is kept in the message.
func seqViolation(got, elem *Type, inner *Violation) *Violation {
	msg := "Sequence[" + nameOf(elem) + "] is covariant in T: the element type may be widened, not narrowed — " +
		got.Name() + " yields " + elemName(got)
	sug := "make the element types agree (or make the source's element a subclass of " + nameOf(elem) + ")"
	if inner != nil && inner.Kind != RuleKindMismatch {
		msg += "; " + inner.Msg
		if inner.Suggestion != "" {
			sug = inner.Suggestion
		}
	}
	return &Violation{Kind: RuleCovariant, Code: CodeVarianceCovariant, Msg: msg, Suggestion: sug}
}

// elemName renders a container type's element type for diagnostics.
func elemName(t *Type) string {
	switch t.Kind {
	case KindList, KindSet, KindIterator, KindSequence:
		return nameOf(t.Elem)
	case KindDict:
		return nameOf(t.Val)
	case KindTuple:
		if len(t.Elems) == 0 {
			return "any"
		}
		return nameOf(t.Elems[0])
	case KindString:
		return "str"
	}
	return "any"
}

func nameOf(t *Type) string {
	if t == nil {
		return "any"
	}
	return t.Name()
}

// callableSubType checks a Callable[[P...], R] (or fn) target: arity must
// match, parameters are CONTRAVARIANT (the source must accept at least as much
// as the destination promises to pass) and the return is COVARIANT.
func callableSubType(ci *ClassIndex, got, want *Type) *Violation {
	if got.Kind != KindFunc && got.Kind != KindCallable {
		return &Violation{Kind: RuleKindMismatch, Code: CodeTypeMismatch,
			Msg: "expected " + want.Name() + ", got " + got.Name()}
	}
	if len(got.Params) == 0 {
		return nil // a bare function reference carries no parameter info (gradual)
	}
	if len(got.Params) != len(want.Params) {
		return &Violation{Kind: RuleArity, Code: CodeCallableArity,
			Msg: "callable arity mismatch: " + got.Name() + " takes " + strconv.Itoa(len(got.Params)) +
				" parameters, " + want.Name() + " takes " + strconv.Itoa(len(want.Params))}
	}
	for i, wp := range want.Params {
		gp := got.Params[i]
		if gp == nil || wp == nil || gp.IsDyn() || wp.IsDyn() {
			continue
		}
		// CONTRAVARIANT: the destination's parameter type flows INTO the
		// source's parameter type, not the other way round.
		if v := subType(ci, wp, gp); v != nil {
			return &Violation{Kind: RuleContravariant, Code: CodeVarianceContravariant,
				Msg: "Callable[[...], R] is contravariant in its parameters: parameter " + strconv.Itoa(i+1) +
					" of " + got.Name() + " demands " + gp.Name() + ", but the destination only supplies " + wp.Name(),
				Suggestion: "a callable is substitutable only when it accepts AT LEAST as much as the destination will pass — widen " +
					got.Name() + "'s parameter to " + wp.Name() + " (or a supertype of it)"}
		}
	}
	if got.Ret == nil || want.Ret == nil || got.Ret.IsDyn() || want.Ret.IsDyn() {
		return nil
	}
	if v := subType(ci, got.Ret, want.Ret); v != nil {
		return &Violation{Kind: RuleCovariant, Code: CodeVarianceCovariant,
			Msg: "Callable[[...], R] is covariant in R: " + got.Name() + " returns " + got.Ret.Name() +
				", " + want.Name() + " requires " + want.Ret.Name(),
			Suggestion: v.Msg}
	}
	return nil
}

// classSubType checks a nominal class target: the source must be the same class
// or a subclass of it, walking the declared base chain.
func classSubType(ci *ClassIndex, got, want *Type) *Violation {
	if got.Kind != KindClass {
		return &Violation{Kind: RuleKindMismatch, Code: CodeTypeMismatch,
			Msg: "expected " + want.Name() + ", got " + got.Name()}
	}
	if ci != nil && ci.Less(got.ClassName, want.ClassName) {
		return nil
	}
	return &Violation{Kind: RuleNominal, Code: CodeVarianceNominal,
		Msg: "class types are nominal: " + got.Name() + " is not a subclass of " + want.Name(),
		Suggestion: "declare `class " + got.Name() + "(" + want.ClassName + ")`, or annotate the destination as " +
			got.Name()}
}

// assignable reports whether a concrete type `got` may be used where `want` is
// declared, under gradual typing plus the variance rules above. It is the
// boolean front-end of subType; callers that need the reason call subType.
func assignable(got, want *Type) bool { return subType(nil, got, want) == nil }

// assignableIn is assignable with a class hierarchy in scope.
func assignableIn(ci *ClassIndex, got, want *Type) bool { return subType(ci, got, want) == nil }

// ---------------------------------------------------------------------------
// Machine-readable variance table (self-describing CLI surface)
// ---------------------------------------------------------------------------

// VarianceRule is one row of the variance table: the constructor, its type
// parameters, the declared variance of each, and why.
type VarianceRule struct {
	Constructor string   `json:"constructor"`
	Params      []string `json:"params"`
	Variance    []string `json:"variance"`
	Mutable     bool     `json:"mutable"`
	ReadOnly    string   `json:"read_only,omitempty"`
	Rationale   string   `json:"rationale"`
	Code        string   `json:"code"`
}

// VarianceDocument is the JSON document printed by `gustyc --variance`.
type VarianceDocument struct {
	SchemaVersion string         `json:"schema_version"`
	Version       string         `json:"language_version"`
	GeneratedBy   string         `json:"generated_by"`
	Rules         []VarianceRule `json:"rules"`
}

// VarianceTable returns the canonical variance table for the language: the
// single source of truth shared by the checker, the docs, and the CLI.
func VarianceTable() []VarianceRule {
	rules := []VarianceRule{
		{
			Constructor: "list[T]", Params: []string{"T"},
			Variance: []string{VarianceInvariant.String()},
			Mutable:  true, ReadOnly: "Sequence[T]", Code: CodeVarianceInvariant,
			Rationale: "lists are mutable through every alias, so a widened element type would let a write through one name corrupt another",
		},
		{
			Constructor: "set[T]", Params: []string{"T"},
			Variance: []string{VarianceInvariant.String()},
			Mutable:  true, ReadOnly: "AbstractSet[T]", Code: CodeVarianceInvariant,
			Rationale: "sets support add/remove, so the element type must match exactly",
		},
		{
			Constructor: "dict[K, V]", Params: []string{"K", "V"},
			Variance: []string{VarianceInvariant.String(), VarianceInvariant.String()},
			Mutable:  true, ReadOnly: "Mapping[K, V]", Code: CodeVarianceInvariant,
			Rationale: "dicts support insertion and overwrite, so both type arguments are invariant",
		},
		{
			Constructor: "tuple[...]", Params: []string{"T..."},
			Variance: []string{VarianceCovariant.String()},
			Mutable:  false, Code: CodeVarianceCovariant,
			Rationale: "tuples are immutable, so a tuple of Dogs can be read wherever a tuple of Animals is expected (arity must match)",
		},
		{
			Constructor: "Sequence[T]", Params: []string{"T"},
			Variance: []string{VarianceCovariant.String()},
			Mutable:  false, Code: CodeVarianceCovariant,
			Rationale: "a read-only sequence only ever yields elements, so widening the element type is sound — this is the escape hatch for invariant list/dict/set",
		},
		{
			Constructor: "iter[T]", Params: []string{"T"},
			Variance: []string{VarianceCovariant.String()},
			Mutable:  false, Code: CodeVarianceCovariant,
			Rationale: "an iterator produces values and accepts none back, so it is covariant",
		},
		{
			Constructor: "Callable[[P...], R]", Params: []string{"P...", "R"},
			Variance: []string{VarianceContravariant.String(), VarianceCovariant.String()},
			Mutable:  false, Code: CodeVarianceContravariant,
			Rationale: "a handler is substitutable only if it accepts everything the destination will pass (contravariant inputs); its result may be more specific (covariant output)",
		},
		{
			Constructor: "class C", Params: []string{"C"},
			Variance: []string{"nominal"},
			Mutable:  true, Code: CodeVarianceNominal,
			Rationale: "a value reaches a class position only if it is that class or a subclass of it, walking the declared base chain",
		},
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Constructor < rules[j].Constructor })
	return rules
}

// VarianceJSON renders the variance table as the machine-readable document
// `gustyc --variance` prints.
func VarianceJSON() (string, error) {
	doc := VarianceDocument{
		SchemaVersion: "1.0",
		Version:       Version,
		GeneratedBy:   "gustyc --variance",
		Rules:         VarianceTable(),
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", fmt.Errorf("variance: marshal: %w", err)
	}
	return string(b), nil
}
