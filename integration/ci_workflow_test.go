package integration

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The CI workflow is the one file in this repository whose failure produces no diagnostic: a `.github/workflows`
// YAML that does not parse is not reported as a failing step, because there is no step. GitHub records a run with
// zero jobs, names the run after the file path instead of the workflow's own `name:` — it never got that far —
// completes it in the same second it opened, and the only thing that reaches a human is an email saying
// ".github/workflows/go.yml workflow run — No jobs were run". A red suite says what is broken and where; this
// says that something happened and nothing ran, which reads like a quiet week.
//
// That is what happened here, for 37 pushes: one step name, `Unit tests (sharded: one test process per core)`.
// A plain YAML scalar ends at the first ": ", so that line is a mapping whose value is `Unit tests (sharded:` —
// "mapping values are not allowed here" — and the job vanished with it.

// nameValueRE matches a YAML mapping entry introducing a name (`- name: …` or `  name: …`) and captures the value.
var nameValueRE = regexp.MustCompile(`^\s*(?:-\s+)?name:\s+(.*)$`)

// unquotedColonSpace reports whether an unquoted YAML plain scalar contains the ": " that would end it early and
// turn the line into a mapping. Quoted values ("…", '…') and block scalars are not plain and are left alone.
func unquotedColonSpace(value string) bool {
	v := strings.TrimSpace(value)
	if v == "" || v == "|" || strings.HasPrefix(v, "|") || strings.HasPrefix(v, ">") {
		return false
	}
	if strings.HasPrefix(v, `"`) || strings.HasPrefix(v, `'`) {
		return false
	}
	return strings.Contains(v, ": ")
}

// TestTheCIWorkflowIsYAMLThatActionsCanRead checks the structural rule that actually bit, in Go, always: a step
// or job name must not be a plain scalar carrying ": ". The full parse is attempted too, where a YAML parser is
// at hand — `python3` is already the oracle's dependency, and the check that needs the same interpreter the
// conformance leg needs costs nothing.
func TestTheCIWorkflowIsYAMLThatActionsCanRead(t *testing.T) {
	t.Parallel()
	root := repoRootForParify(t)
	dir := filepath.Join(root, ".github", "workflows")
	matches, err := filepath.Glob(filepath.Join(dir, "*.y*ml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatalf("no workflow under %s — the suite expected one, and a workflow that has gone missing is how "+
			"'no jobs were run' starts", dir)
	}
	for _, path := range matches {
		name := filepath.Base(path)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(string(raw), "\n")

			// The claims the runner needs before it can run anything: a name, a trigger, jobs, and a job that
			// says where and what. Each of these is a top-level key, and each can vanish for the same reason
			// the step name did.
			text := string(raw)
			for _, need := range []string{"name", "on", "jobs", "runs-on"} {
				// Any indentation counts — `on:` and `jobs:` sit at column 0, `runs-on:` sits under its job —
				// but the key must begin a line, or it is only a word inside somebody's comment.
				re := regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(need) + `:`)
				if !re.MatchString(text) {
					t.Errorf("%s lost its %q key — Actions would record a run with no jobs at all, and the only "+
						"signal is an email that says nothing ran", name, need)
				}
			}
			if !strings.Contains(text, "- name:") && !strings.Contains(text, "uses:") {
				t.Errorf("%s has no steps: no `- name:` and no `uses:`, which is what a run with zero jobs looks like", name)
			}

			for i, ln := range lines {
				if inBlockScalar(lines, i) {
					continue
				}
				m := nameValueRE.FindStringSubmatch(ln)
				if m == nil || !unquotedColonSpace(m[1]) {
					continue
				}
				t.Errorf("%s:%d: step/job name is a plain scalar carrying %q — YAML ends the value at the colon and "+
					"reads the rest as a mapping, so the workflow does not parse, and a run of a workflow that does "+
					"not parse has no jobs and no error: the email just says 'No jobs were run'. Quote the name.\n\t%s",
					name, i+1, ": ", strings.TrimSpace(ln))
			}
		})
	}
}

// inBlockScalar reports whether line i sits inside a `|`/`>` block scalar, where `: ` is ordinary text. The
// detection is the indentation rule, not a parser: the block's content is everything indented past its header.
func inBlockScalar(lines []string, i int) bool {
	for j := i - 1; j >= 0; j-- {
		ln := lines[j]
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if strings.HasSuffix(strings.TrimSpace(ln), "|") || strings.HasSuffix(strings.TrimSpace(ln), "|-") ||
			strings.HasSuffix(strings.TrimSpace(ln), ">") || strings.HasSuffix(strings.TrimSpace(ln), ">-") {
			head := leadingSpaces(ln)
			return leadingSpaces(lines[i]) > head
		}
		if strings.HasPrefix(strings.TrimSpace(ln), "#") {
			continue
		}
		return false
	}
	return false
}

func leadingSpaces(s string) int {
	n := 0
	for n < len(s) && s[n] == ' ' {
		n++
	}
	return n
}

// TestTheWorkflowCheckReportsTheDefectThatHappened keeps the guard above honest: the line that took CI dark for
// 37 pushes is fed to the same rule, and the rule must refuse it. Without this row the guard could pass by
// matching nothing at all — which is precisely how a broken workflow stayed broken.
func TestTheWorkflowCheckReportsTheDefectThatHappened(t *testing.T) {
	t.Parallel()
	cases := []struct {
		line string
		bad  bool
		why  string
	}{
		{`      - name: Unit tests (sharded: one test process per core)`, true, "the line that broke CI"},
		{`      - name: "Unit tests (sharded: one test process per core)"`, false, "quoted, as it is now"},
		{`      - name: Toolchain versions (the matrix is only meaningful against these)`, false, "no colon-space"},
		{`        run: go test -run TestX ./...  # a: comment inside a block does not count`, false, "not a name"},
		{`  test-on-ubuntu-with-llvm-20:`, false, "a job key, not a value"},
	}
	for _, tc := range cases {
		t.Run(tc.why, func(t *testing.T) {
			t.Parallel()
			m := nameValueRE.FindStringSubmatch(tc.line)
			got := m != nil && unquotedColonSpace(m[1])
			if got != tc.bad {
				t.Errorf("the rule %v for %q (%s)", got, tc.line, tc.why)
			}
		})
	}
}

// TestTheCIWorkflowParsesAsYAML is the general check, where a parser exists: it asks the oracle's own
// interpreter, because the repository already depends on python3 for the reference leg and PyYAML ships with it
// on the runner image. No parser is a skip, not a pass — the structural rule above still ran.
func TestTheCIWorkflowParsesAsYAML(t *testing.T) {
	t.Parallel()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3 to parse the workflow with; the structural rule still ran")
	}
	src := `
import sys, yaml
d = yaml.safe_load(open(sys.argv[1]))
jobs = d.get("jobs") or {}
assert d.get("name"), "the workflow has no name: key"
assert d.get(True) is not None or d.get("on") is not None, "the workflow has no on: trigger"
assert jobs, "the workflow has no jobs"
for j, v in jobs.items():
    assert (v or {}).get("steps"), "job %s has no steps" % j
print(len(jobs))
`
	root := repoRootForParify(t)
	matches, _ := filepath.Glob(filepath.Join(root, ".github", "workflows", "*.y*ml"))
	for _, path := range matches {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()
			cmd := exec.Command(py, "-c", src, path)
			out, err := cmd.CombinedOutput()
			if err != nil {
				if strings.Contains(string(out), "No module named") {
					t.Skip("python3 has no yaml module here; the structural rule still ran")
				}
				t.Errorf("Actions would refuse this workflow and run nothing:\n%s\n%s", strings.TrimSpace(string(out)), path)
			}
		})
	}
}
