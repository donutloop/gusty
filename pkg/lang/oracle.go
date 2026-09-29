// The oracle leg of the conformance matrix (roadmap L11.9, ADR 0186).
//
// Parity — "the interpreter and the compiled backend print the same thing" — is
// a necessary but *weak* contract: two backends can agree on a wrong answer and
// the build stays green. Every divergence collected in the roadmap's Phase 11
// table (print(True) == "1", len("café") == 5, xs[-1] trapping, print(math.PI)
// == 3) was of exactly that shape: invisible to a harness that only compares the
// two implementations to each other.
//
// This file makes CPython the third leg. A case is not "conformant" because the
// backends agree; it is conformant when both backends agree *and* what they
// print is what CPython prints for the same source. The classification is a pure
// function of the three observed legs, so it can be asserted by the integration
// harness, reported by `gustyc --oracle`, and diffed as JSON by an agent without
// anyone scraping prose.
package lang

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Oracle verdicts for one case. The vocabulary is part of the machine-readable
// matrix (`oracle` / `oracle_declared`) and of `--oracle` JSON output, so it is
// stable: an agent branches on these strings, not on prose.
const (
	// OracleMatch: both backends printed exactly what CPython printed.
	OracleMatch = "match"
	// OracleDebt: the program is a valid CPython program and at least one backend
	// answers differently (a wrong value, a refusal, a crash). Every debt row is
	// declared in the registry with a reason and a roadmap reference, and its
	// current (wrong) output is pinned, so a partial fix fails the build.
	OracleDebt = "debt"
	// OracleNA: the source is not a CPython program at all — gusty-only surface
	// (`await` outside a coroutine, set subscripting, a stdlib attribute Python
	// does not have, a parse the language accepts and Python rejects). The CPython
	// leg is still recorded, but no comparison is claimed.
	OracleNA = "not_applicable"
)

// Documented comparison rules. Each one names a place where byte-for-byte
// comparison against CPython is not meaningful; the rule is applied to *both*
// sides, and only to the sides, so it can never turn a wrong answer into a match
// by itself — it only removes a difference that is unspecified in Python.
const (
	// RuleSetOrder treats a `{...}` rendering with no `key: value` entry (a set) as
	// an unordered multiset. CPython's set iteration order depends on the hash seed
	// and on insertion history; gusty's is insertion order. Comparing them byte for
	// byte would pin an accident of one CPython build. Dict renderings (an entry
	// containing ": ") keep their order — that order is insertion order in both.
	RuleSetOrder = "set-order"
)

// DefaultOracleRules are applied to every leg before comparison.
func DefaultOracleRules() []string { return []string{RuleSetOrder} }

// PythonBinary is the interpreter used for the oracle leg. It is overridable
// (`GUSTY_PYTHON`) because the pinned oracle is a *named* toolchain, recorded in
// docs/operations.md, not an accident of whoever's PATH came first.
func PythonBinary() string {
	if p := os.Getenv("GUSTY_PYTHON"); p != "" {
		return p
	}
	return "python3"
}

// OracleMinPython is the pinned minimum CPython the corpus is validated against,
// in the same spirit as the pinned LLVM version (docs/operations.md). It is a real
// requirement, not a preference: the ledger has rows whose expected answer comes
// from a CPython that can parse the construct under test — `type Count = int` is
// PEP 695, so `programs/typealias` has no oracle answer below 3.12, and the leg
// dies with a SyntaxError on line 1.
//
// Without a pin that fact shows up as oracle *drift* on a row declared `match`, and
// reads like a compiler bug: the drift note said only "the CPython leg did not
// complete", and the CI runner's Python was the difference between green and red.
// So the pin is recorded in the matrix, and the harness says "upgrade the oracle"
// instead of pointing at the compiler (ADR 0193).
const OracleMinPython = "3.12"

// OracleVersion reads a `python3 --version` banner (`Python 3.12.3`) into
// major.minor. It returns ok=false when the text is not recognisable, which callers
// treat as "unknown" rather than "old": a machine whose oracle cannot be identified
// must not be told it is unsupported.
func OracleVersion(banner string) (major, minor int, ok bool) {
	fields := strings.Fields(banner)
	for _, f := range fields {
		// strings.IndexFunc, not strings.ContainsFunc: the latter is Go 1.21 and the
		// module declares go 1.20 (go.mod), which CI builds against. The go directive
		// gates language features, NOT stdlib API availability, so a 1.21 call type-checks
		// on a newer local toolchain and fails only in CI (ADR 0193's lesson, one
		// toolchain over).
		if !strings.Contains(f, ".") || strings.IndexFunc(f, unicode.IsDigit) < 0 {
			continue
		}
		parts := strings.Split(f, ".")
		if len(parts) < 2 {
			continue
		}
		ma, err1 := strconv.Atoi(parts[0])
		mi, err2 := strconv.Atoi(parts[1])
		if err1 == nil && err2 == nil && ma > 0 {
			return ma, mi, true
		}
	}
	return 0, 0, false
}

// OracleVersionTooOld reports a *known* oracle older than the pin, and is false for
// an unidentifiable version — see OracleVersion for why unknown is not too old.
func OracleVersionTooOld(banner string) bool {
	major, minor, ok := OracleVersion(banner)
	if !ok {
		return false
	}
	wantM, wantN, _ := parseVersionPair(OracleMinPython)
	if major != wantM {
		return major < wantM
	}
	return minor < wantN
}

func parseVersionPair(s string) (int, int, bool) {
	parts := strings.Split(s, ".")
	if len(parts) < 2 {
		return 0, 0, false
	}
	a, err1 := strconv.Atoi(parts[0])
	b, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return a, b, true
}

// OracleTooOldHint explains a python leg that died on line 1 of a program the
// interpreter accepts. The honest reading is usually the toolchain, not the
// language, so the note says so and names the remedy.
func OracleTooOldHint(pythonErr string) string {
	if !strings.Contains(pythonErr, "SyntaxError") {
		return ""
	}
	return "the oracle interpreter is older than the pinned " + OracleMinPython +
		" (docs/operations.md): a SyntaxError on line 1 usually means the program uses a construct this " +
		"CPython predates (PEP 695 `type X = int` needs 3.12), not a compiler bug — install a newer " +
		"python3 or point GUSTY_PYTHON at one"
}

// PythonRun runs src through the oracle interpreter and returns its stdout, its
// stderr, and the run error if it exited non-zero.
//
// PYTHONHASHSEED=0 pins CPython's string hashing so a run is reproducible: the
// matrix artifact must be diffable between runs, and a set rendering that changes
// order between two CI runs would look like a compiler regression. It does not
// make Python's set order the contract (see RuleSetOrder); it makes it stable.
func PythonRun(src string) (stdout, stderr string, err error) {
	dir, err := os.MkdirTemp("", "gusty-oracle")
	if err != nil {
		return "", "", fmt.Errorf("oracle tempdir: %w", err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "prog.py")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		return "", "", fmt.Errorf("oracle write: %w", err)
	}
	cmd := exec.Command(PythonBinary(), "-B", path)
	cmd.Env = append(os.Environ(), "PYTHONHASHSEED=0")
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	runErr := cmd.Run()
	return out.String(), errb.String(), runErr
}

// OracleLeg is one observed leg: which engine produced it, whether it completed,
// what it wrote to stdout, and whether that stdout is the oracle's.
type OracleLeg struct {
	Backend string `json:"backend"`
	OK      bool   `json:"ok"`
	Stdout  string `json:"stdout"`
	Error   string `json:"error,omitempty"`
	Matches bool   `json:"matches_python"`
}

// OracleReport is the three-leg verdict for one program: the interpreter leg, the
// compiled leg, the CPython leg, plus the classification derived from them.
type OracleReport struct {
	Legs   []OracleLeg `json:"legs"`
	Parity bool        `json:"parity"`
	Status string      `json:"oracle"`
	Notes  []string    `json:"notes,omitempty"`
	Rules  []string    `json:"rules,omitempty"`
}

// Leg returns a leg by backend name ("" when absent).
func (r OracleReport) Leg(backend string) (OracleLeg, bool) {
	for _, l := range r.Legs {
		if l.Backend == backend {
			return l, true
		}
	}
	return OracleLeg{}, false
}

// BuildOracleReport classifies three observed legs. It is the single
// implementation of "does this program behave like Python?" shared by the
// integration harness and the CLI, so the two can never disagree about a verdict.
//
// A leg that did not complete counts as *not* matching (with a note), because
// "the compiled backend refused" is a divergence from an oracle that answered —
// that is the L11.8 rule: a refusal is a debt, not a pass.
func BuildOracleReport(interpOK bool, interpOut, interpErr string,
	aotOK bool, aotOut, aotErr string,
	rules []string, pythonOK bool, pythonOut, pythonErr string) OracleReport {
	all := append(DefaultOracleRules(), rules...)
	pyNorm := OracleNormalize(pythonOut, all)
	interpMatch := interpOK && OracleNormalize(interpOut, all) == pyNorm
	aotMatch := aotOK && OracleNormalize(aotOut, all) == pyNorm
	r := OracleReport{
		Legs: []OracleLeg{
			{Backend: "interpreter", OK: interpOK, Stdout: interpOut, Error: interpErr, Matches: interpMatch},
			{Backend: "aot", OK: aotOK, Stdout: aotOut, Error: aotErr, Matches: aotMatch},
			{Backend: "python", OK: pythonOK, Stdout: pythonOut, Error: firstLine(pythonErr)},
		},
		Parity: interpOK && aotOK && interpOut == aotOut,
		Rules:  all,
	}
	switch {
	case !pythonOK:
		r.Status = OracleNA
		r.Notes = append(r.Notes, "the CPython leg did not complete: "+firstLine(pythonErr))
		if hint := OracleTooOldHint(pythonErr); hint != "" {
			r.Notes = append(r.Notes, hint)
		}
	case interpMatch && aotMatch:
		r.Status = OracleMatch
	default:
		r.Status = OracleDebt
		if !interpOK {
			r.Notes = append(r.Notes, "interpreter leg failed: "+firstLine(interpErr))
		} else if !interpMatch {
			r.Notes = append(r.Notes, "interpreter stdout differs from CPython")
		}
		if !aotOK {
			r.Notes = append(r.Notes, "compiled leg failed: "+firstLine(aotErr))
		} else if !aotMatch {
			r.Notes = append(r.Notes, "compiled stdout differs from CPython")
		}
	}
	return r
}

// OracleNormalize applies the named comparison rules to one leg's stdout.
// Unknown rule names are ignored on purpose: the registry is data, and a rule the
// compiler does not implement must not silently become a pass — the rule list is
// echoed in the artifact so a stale name is visible.
func OracleNormalize(s string, rules []string) string {
	if hasRule(rules, RuleSetOrder) {
		s = normalizeSetOrder(s)
	}
	return s
}

func hasRule(rules []string, want string) bool {
	for _, r := range rules {
		if r == want {
			return true
		}
	}
	return false
}

// normalizeSetOrder re-renders every line that is a set literal rendering
// (`{a, b, c}`) with its elements in sorted order. Lines that are not a bare
// container rendering, and dict renderings (an entry containing ": "), pass
// through unchanged.
func normalizeSetOrder(s string) string {
	lines := strings.SplitAfter(s, "\n")
	for i, line := range lines {
		body := strings.TrimRight(line, "\r\n")
		lead := line[len(body):] // the line's own trailing newline, kept byte for byte
		indent := strings.TrimLeft(body, " \t")
		prefix := body[:len(body)-len(indent)]
		if !isSetRendering(indent) {
			continue
		}
		lines[i] = prefix + normalizeSet(indent) + lead
	}
	return strings.Join(lines, "")
}

// normalizeSet re-renders one set as a sorted multiset, recursing into set elements
// (a set of sets is still an unordered collection at every level). A list or dict
// element is left exactly as written: that order is observable in gusty.
func normalizeSet(s string) string {
	elems := splitTopLevel(s[1 : len(s)-1])
	norm := make([]string, 0, len(elems))
	for _, e := range elems {
		e = strings.TrimSpace(e)
		if isSetRendering(e) {
			e = normalizeSet(e)
		}
		norm = append(norm, e)
	}
	sort.Strings(norm)
	return "{" + strings.Join(norm, ", ") + "}"
}

// isSetRendering reports whether a line body is exactly one set rendering: a
// `{...}` group whose entries are all plain values (no `key: value`).
func isSetRendering(s string) bool {
	if len(s) < 2 || s[0] != '{' || s[len(s)-1] != '}' {
		return false
	}
	if !balanced(s) {
		return false
	}
	for _, e := range splitTopLevel(s[1 : len(s)-1]) {
		if _, ok := topLevelSplit(e, ':'); ok {
			return false
		}
	}
	return true
}

// splitTopLevel splits a container body on commas that are not inside nested
// brackets or a string literal.
func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	inStr := 0
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr != 0:
			if c == '\\' {
				i++
				continue
			}
			if c == byte(inStr) {
				inStr = 0
			}
		case c == '\'' || c == '"':
			inStr = int(c)
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	if len(out) == 1 && strings.TrimSpace(out[0]) == "" {
		return nil
	}
	return out
}

// topLevelSplit finds sep outside brackets and strings; ok is false when absent.
func topLevelSplit(s string, sep byte) (before string, ok bool) {
	depth, inStr := 0, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr != 0:
			if c == '\\' {
				i++
				continue
			}
			if c == byte(inStr) {
				inStr = 0
			}
		case c == '\'' || c == '"':
			inStr = int(c)
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == sep && depth == 0:
			return s[:i], true
		}
	}
	return "", false
}

func balanced(s string) bool {
	depth, inStr := 0, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr != 0:
			if c == '\\' {
				i++
				continue
			}
			if c == byte(inStr) {
				inStr = 0
			}
		case c == '\'' || c == '"':
			inStr = int(c)
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0 && inStr == 0
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
