package integration

import (
	"sort"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// Every capability refusal the corpus produces is classified (roadmap L11.8, ADR 0346).
//
// The registry (pkg/lang/capability.go) claims that a shape the compiled backend refuses as a matter of
// design has a stable code, a suggestion, and the roadmap rows that own the missing half. Its own unit
// test reads that claim backwards — every entry's sample really produces that refusal. This is the
// claim read forwards, over the corpus rather than the registry: compile every conformance program, ask
// the classifier about every refusal that comes back, and fail on one that has no code.
//
// Why the corpus and not a hand-written list of refusals: the corpus is the set of programs this
// repository already claims to know the answer for. A refusal that appears there is one a real program
// hits, and leaving it uncoded means the agent reading `--json` gets prose for exactly the shapes that
// matter — and no amount of prose-matching survives a reworded sentence (ADR 0346's reason for existing).
//
// The tail that is *not* a capability refusal is `capabilityNotACapabilityGap` below — an explicit list,
// each with the reason it is the program's own error rather than a missing lowering. A hole in coverage
// has to be a sentence someone read and wrote down; it cannot be a `continue`.

// capabilityNotACapabilityGap is the allowlist: sentences the compiled leg emits that are not missing
// lowerings, so they carry no `lower.unsupported.*` code and the payload's `capability` is null. Keys are
// matched as substrings; every entry names the phase that speaks and who owns the construct.
var capabilityNotACapabilityGap = map[string]string{
	// The front end's own diagnoses. `phase` is the machine field for these — a parse error and a
	// checker refusal are not sentences the lowering produced, and the classifier's family is the
	// lowering's. The lexer gap behind the float-exponent probe is Gap R.135 / Gap R.245, and it is a
	// row of the tracker, not of this registry.
	"parse error at":            "the parser's sentence, whose machine field is the payload's `phase`; the lexer shapes are Gap R.135 and Gap R.245",
	"possibly unbound":          "the checker's definite-assignment rule (ADR 0228) — a program whose binding may not have run",
	"match is not exhaustive":   "the checker's exhaustiveness rule — a program missing a wildcard arm",
	"arithmetic on non-numeric": "the checker's operand rule — CPython raises TypeError for these programs too",
	"undefined name":            "the checker's name rule — the reference reports the same NameError",

	// Refusals of a program the reference also refuses: the program is wrong, and the compiler said so
	// before running it. Where the fold should have raised instead of refusing, that is Gap R.37's road,
	// not a missing capability.
	"tuple assignment length mismatch":                         "unpacking a non-sequence: the reference raises ValueError for the same program",
	"unsupported tuple assignment value":                       "unpacking a non-sequence: the reference raises TypeError for the same program",
	"no such module":                                           "an import of a module that is not there — the reference raises ModuleNotFoundError",
	"unsupported string method":                                "a method text has not: the reference raises AttributeError",
	"concatenating a string with a value that is not a string": "the reference raises TypeError for the same program",
}

func TestEveryCapabilityRefusalInTheCorpusIsClassified(t *testing.T) {
	t.Parallel()
	type gap struct {
		prog, msg string
	}
	var unclassified []gap
	distinct := map[string]int{}
	refused, compiled := 0, 0

	for _, c := range conformanceCases() {
		res, err := lang.Compile(c.Source)
		var msgs []string
		if err != nil {
			msgs = []string{err.Error()}
		} else if res != nil {
			for _, d := range res.Diagnostics {
				// Only errors refuse a program; a warning is the checker's opinion about a program
				// that compiles, and dressing one up as a refusal would make this coverage claim
				// about the checker's mood rather than about the backend's doors.
				if d.Level == "error" {
					msgs = append(msgs, d.Msg)
				}
			}
		}
		if len(msgs) == 0 {
			compiled++
			continue
		}
		refused++
		for _, msg := range msgs {
			if got, ok := lang.ClassifyCapabilityRefusal(msg); ok {
				distinct[got.Code]++
				continue
			}
			if reason, ok := allowlistedCapabilityGap(msg); ok {
				t.Logf("%s: %q is not a capability refusal — %s", c.ID, firstLine(msg), reason)
				continue
			}
			unclassified = append(unclassified, gap{prog: c.ID, msg: firstLine(msg)})
		}
	}

	if len(unclassified) > 0 {
		seen := map[string]bool{}
		var b strings.Builder
		for _, u := range unclassified {
			if seen[u.msg] {
				continue
			}
			seen[u.msg] = true
			b.WriteString("  ")
			b.WriteString(u.prog)
			b.WriteString(": ")
			b.WriteString(u.msg)
			b.WriteString("\n")
		}
		t.Fatalf("%d distinct capability refusals in the corpus have no code — add each to the registry with its marker, suggestion and owning rows (pkg/lang/capability.go), or, if it really is the program's own error, to the allowlist with the reason:\n%s",
			len(seen), b.String())
	}

	// The coverage claim has to be non-trivial to be a claim: a corpus that refused nothing would pass
	// the loop above without looking at the registry at all.
	if refused == 0 {
		t.Fatal("no conformance program refused to compile, so this test certified nothing")
	}
	if len(distinct) < 10 {
		t.Fatalf("the corpus exercises only %d distinct capability codes; the registry has %d and the corpus is expected to reach at least 10 of them (a corpus that stopped hitting refusals would pass the loop above vacuously)", len(distinct), len(lang.CapabilityRefusalCodes()))
	}
	codes := make([]string, 0, len(distinct))
	for c := range distinct {
		codes = append(codes, c)
	}
	sort.Strings(codes)
	t.Logf("refused %d / compiled %d; %d distinct codes: %s", refused, compiled, len(codes), strings.Join(codes, ", "))
}

// TestEveryCapabilityCodeIsReachableFromASample asks the registry's own inventory the question the
// corpus cannot: does every code name a shape, or did one accrete without a program behind it? The
// unit test in pkg/lang compiles every entry's sample; this asserts the count an agent reading
// `--capabilities` is promised — one measured program per published code.
func TestEveryCapabilityCodeIsReachableFromASample(t *testing.T) {
	t.Parallel()
	kinds := lang.CapabilityRefusalKinds()
	if len(kinds) < 30 {
		t.Fatalf("the registry publishes %d kinds; it has shrunk under the corpus's ability to check it", len(kinds))
	}
	for _, k := range kinds {
		// Markers and Sample are json:"-" — not a field an agent reads, but the proof that the entry is
		// measured rather than remembered: the pkg/lang test compiles the sample and classifies the sentence.
		if strings.TrimSpace(k.Sample) == "" || len(k.Markers) == 0 {
			t.Fatalf("%q publishes a shape with no sample program and no marker behind it", k.Code)
		}
	}
}

func allowlistedCapabilityGap(msg string) (string, bool) {
	for sub, reason := range capabilityNotACapabilityGap {
		if strings.Contains(msg, sub) {
			return reason, true
		}
	}
	return "", false
}
