package integration

// bool_call_boundary_test.go — roadmap Gap R.111, L11.1's remaining bool item, ADR 0348.
//
// A verdict has always been a value to this language and never one to its value word: a bool is
// stored as the number a comparison produced, and what makes it print `True` is the front end's
// answer to "is this expression a verdict?" (ADR 0257). That question is asked of an AST, so it
// worked wherever the expression was visible — `print(True)`, `print(1 == 1)` (ADR 0257), a
// verdict in a container slot (ADR 0259), the candidate min/max chose (ADR 0261). The one place
// it could not reach was a verdict crossing a call boundary:
//
//	def show(f):
//	    print(f)      # the print road asks IsBoolExpr(Name{"f"}) — and the table had never heard of f
//
//	show(True)        # gusty: 1     CPython: True
//
// Inside the body the argument *is* a name the body never assigned. The call site had read the
// verdict; the body just had no way to find out. `pkg/lang/boolargs.go` is that connection, and
// this file is its witness on the reference leg: every row is compiled, run, and compared with
// what `python3` prints for the same source.
//
// The second table is the promise's other half, and the one more likely to be broken by a future
// edit: where the evidence is thin or contradicted, the compiled answer must stay the number it
// always was. `print(True)`-for-a-number is a new wrong answer, and the analysis exists to avoid
// producing exactly that.

import (
	"strings"
	"testing"
)

func TestBoolThroughCallMatchesCPython(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{
			"argument literal",
			"def show(f):\n    print(f)\n\nshow(True)\nshow(False)\n",
			"True\nFalse\n",
		},
		{
			"argument variable",
			"def show(f):\n    print(f)\n\nb = True\nshow(b)\n",
			"True\n",
		},
		{
			"argument is a comparison",
			"def show(f):\n    print(f)\n\nshow(1 == 1)\nshow(1 == 2)\n",
			"True\nFalse\n",
		},
		{
			"argument is a membership test",
			"def show(f):\n    print(f)\n\nshow(2 in [1, 2, 3])\n",
			"True\n",
		},
		{
			"argument is a not",
			"def show(f):\n    print(f)\n\nshow(not 0)\n",
			"True\n",
		},
		{
			"only the verdict parameter prints True",
			"def show(a, f):\n    print(a)\n    print(f)\n\nshow(1, True)\n",
			"1\nTrue\n",
		},
		{
			"keyword argument",
			"def show(a, f):\n    print(f)\n\nshow(1, f=True)\n",
			"True\n",
		},
		{
			"bool default",
			"def show(f=True):\n    print(f)\n\nshow()\n",
			"True\n",
		},
		{
			"bool annotation",
			"def show(f: bool):\n    print(f)\n\nshow(True)\n",
			"True\n",
		},
		{
			"forwarded through a helper",
			"def inner(f):\n    print(f)\n\ndef outer(f):\n    inner(f)\n\nouter(True)\n",
			"True\n",
		},
		{
			"forwarded twice",
			"def inner(f):\n    print(f)\n\ndef mid(f):\n    inner(f)\n\ndef outer(f):\n    mid(f)\n\nouter(False)\n",
			"False\n",
		},
		{
			"a method's parameter",
			"class C:\n    def m(self, f):\n        print(f)\n\nz = C()\nz.m(True)\nz.m(False)\n",
			"True\nFalse\n",
		},
		{
			"a verdict the callee computed",
			"def isit():\n    b = True\n    return b\n\nprint(isit())\n",
			"True\n",
		},
		{
			"str() of a verdict parameter",
			"def show(f):\n    return str(f)\n\nprint(show(True))\n",
			"True\n",
		},
		{
			"a verdict parameter in a container",
			"def show(f):\n    return [f, 1]\n\nprint(show(True))\n",
			"[True, 1]\n",
		},
		{
			"a verdict from a builtin, handed on",
			"def show(f):\n    print(f)\n\nshow(all([1, 1]))\n",
			"True\n",
		},
		{
			"and/or of two verdicts",
			"def show(f):\n    print(f)\n\nshow(True and False)\nshow(True or False)\n",
			"False\nTrue\n",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if want != r.want {
				t.Fatalf("row is stale: python3 prints %q, the row pins %q", want, r.want)
			}
			for _, engine := range cliEngines {
				got, code := cliRunMerged(t, engine, "--file", writeSrc(t, dir, "bool_call", r.src))
				if code != 0 {
					t.Fatalf("%s: exit %d, want 0:\n%s", engine, code, got)
				}
				if got != want {
					t.Errorf("%s: compiled %q, CPython prints %q\nsource:\n%s", engine, got, want, r.src)
				}
			}
		})
	}
}

// TestBoolThroughCallStaysANumberWhenTheEvidenceIsThin is the conservative half. Each row is a
// shape the analysis must NOT mark, and the expected answer is the number the compiled backend
// printed before this cycle — so a widening of the inference that starts claiming verdicts it
// cannot support shows up here as a failure rather than as a wrong answer a user finds.
func TestBoolThroughCallStaysANumberWhenTheEvidenceIsThin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{
			"a contradicted position prints the number that was passed",
			"def show(f):\n    print(f)\n\nshow(True)\nshow(1)\n",
			"1\n1\n", // declined: the number stays a number; the verdict keeps yesterday's answer
		},
		{
			"a plain number parameter",
			"def show(f):\n    print(f)\n\nshow(1)\n",
			"1\n",
		},
		{
			"a number used arithmetically still adds",
			"def add(f):\n    return f + 1\n\nprint(add(True))\n",
			"2\n",
		},
		{
			"a verdict parameter is still truthy as a condition",
			"def pick(f):\n    if f:\n        return 1\n    return 2\n\nprint(pick(True))\nprint(pick(False))\n",
			"1\n2\n",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			got, code := cliRunMerged(t, "--aot", "--file", writeSrc(t, dir, "bool_decline", r.src))
			if code != 0 {
				t.Fatalf("exit %d, want 0:\n%s", code, got)
			}
			if got != r.want {
				t.Errorf("compiled %q, the row pins %q\nsource:\n%s", got, r.want, r.src)
			}
			if strings.Contains(got, "True") && !strings.Contains(r.want, "True") {
				t.Errorf("the analysis claimed a verdict it cannot support:\n%s", got)
			}
		})
	}
}

// TestBoolThroughCallKeepsTheVerdictOutOfTheValueWord is the IR-side claim: the representation
// did not change. A bool is still the number the comparison produced; what changed is that the
// print road is allowed to ask the front end what the name means (ADR 0257's rule, extended to a
// name bound by a call rather than by an assignment).
func TestBoolThroughCallKeepsTheVerdictOutOfTheValueWord(t *testing.T) {
	t.Parallel()
	src := "def show(f):\n    print(f)\n\nshow(True)\n"
	ir := mustIRText(t, src)
	if !strings.Contains(ir, "rt_print_bool") {
		t.Errorf("the print road did not take the verdict branch, so the parameter still prints %d", 1)
	}
	// The promise is a *print* rule, not a new representation: no new boxed kind appeared.
	if strings.Contains(ir, "@bool_tab") || strings.Contains(ir, "rt_bool_new") {
		t.Errorf("a runtime bool representation appeared; the answer was supposed to come from the front end")
	}
}
