package integration

// Gap R.219 / Gap R.228 / ADR 0368 at the CLI: a body that answers more than one kind.
//
//	def f(x):
//	    if x:
//	        return "x"
//	    return 0
//
//	print(f(1))
//	print(f(0))
//
// The reference prints `x` and `0`. The compiled leg printed `x` and `x` at exit 0, because the first
// text seen anywhere in that body registered `f` as a text-returning function, and the caller then
// handed `printf` an interned string index with `%d` for BOTH answers — the number being the answer
// nobody wrote (roadmap Gap R.219). The same missing kind word answered `7`/`0` for CPython's
// `7`/`None` (Gap R.228), `1`/`2` for CPython's `True`/`2`, the slot number `1` for `{'k': 1}`, and
// the digit `0` for a container arm.
//
// The fix carries the answer's KIND beside its payload: the `return` road that ran stores the tag
// table's own word into the module's pair-tag global, and every position that reads such an answer —
// print, a binding, a container element, a condition, `str`/`repr`, an f-string field — asks the tag
// rather than guessing the kind from the payload.
//
// Every row below is held to the reference's own answer, in the reference's own words: these are
// shapes that now answer correctly, so a row that regresses to a refusal or to a number is a failure
// and not a skip (ADR 0166's exit-code contract, ADR 0343's run-scoped scans).

import (
	"strings"
	"testing"
)

func TestACallThatAnswersMoreThanOneKindAtTheCLI(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{
			"text and int",
			"def f(x):\n    if x:\n        return \"x\"\n    return 0\n\nprint(f(1))\nprint(f(0))\n",
			"x\n0\n",
		},
		{
			"number and the void",
			"def f(x):\n    if x:\n        return 7\n\nprint(f(1))\nprint(f(0))\n",
			"7\nNone\n",
		},
		{
			"bool and int",
			"def f(x):\n    if x:\n        return True\n    return 2\n\nprint(f(1))\nprint(f(0))\n",
			"True\n2\n",
		},
		{
			"the None literal",
			"def f():\n    return None\n\nprint(f())\n",
			"None\n",
		},
		{
			"text and list",
			"def f(x):\n    if x:\n        return \"a\"\n    return [1, 2]\n\nprint(f(1))\nprint(f(0))\n",
			"a\n[1, 2]\n",
		},
		{
			"int and dict",
			"def f(x):\n    if x:\n        return 3\n    return {\"k\": 1}\n\nprint(f(1))\nprint(f(0))\n",
			"3\n{'k': 1}\n",
		},
		{
			"str and repr of both arms",
			"def f(x):\n    if x:\n        return \"x\"\n    return 0\n\nprint(str(f(1)))\nprint(repr(f(1)))\nprint(str(f(0)))\nprint(repr(f(0)))\n",
			"x\n'x'\n0\n0\n",
		},
		{
			"an f-string field",
			"def f(x):\n    if x:\n        return \"x\"\n    return 0\n\nprint(f\"{f(1)}\")\nprint(f\"{f(0)}\")\n",
			"x\n0\n",
		},
		{
			"a condition",
			"def f(x):\n    if x:\n        return \"a\"\n    return 0\n\nif f(1):\n    print(\"t1\")\n\nif f(0):\n    print(\"t0\")\nelse:\n    print(\"f0\")\n",
			"t1\nf0\n",
		},
		{
			"a container element",
			"def f(x):\n    if x:\n        return \"x\"\n    return 0\n\nprint([f(1), f(0)])\n",
			"['x', 0]\n",
		},
		{
			"bound to two names",
			"def f(x):\n    if x:\n        return \"x\"\n    return 0\n\ny = f(1)\nprint(y)\n\nz = f(0)\nprint(z)\n\nprint([y, z])\n",
			"x\n0\n['x', 0]\n",
		},
		{
			"a nested call",
			"def f(x):\n    if x:\n        return \"x\"\n    return 0\n\nprint(max(f(1), \"b\"))\nprint(max(f(0), 1))\n",
			"x\n1\n",
		},
		{
			"a call in an argument",
			"def f(x):\n    if x:\n        return \"hi\"\n    return 0\n\nprint(f(1))\n\nfor i in [1]:\n    print(f(0))\n",
			"hi\n0\n",
		},
		{
			"a call whose answer nobody reads",
			"def f(x):\n    if x:\n        return \"hi\"\n\nf(1)\nprint(\"done\")\n",
			"done\n",
		},
		{
			// The four mutation roads ADR 0368's gate now lets through: each writes the element's payload
			// beside the element's tag, so the container prints the arm that ran (`lower.unsupported.
			// container-slot-kind-unprovable` in `pkg/lang/capability.go` names what is left refused).
			"appended to a list",
			"def f(x):\n    if x:\n        return \"a\"\n    return 2\n\nys = []\nys.append(f(1))\nprint(ys)\nprint(len(ys))\n",
			"['a']\n1\n",
		},
		{
			"added to a set",
			"def f(x):\n    if x:\n        return \"a\"\n    return 2\n\ns = set()\ns.add(f(1))\nprint(s)\n",
			"{'a'}\n",
		},
		{
			"put in a dict",
			"def f(x):\n    if x:\n        return \"a\"\n    return 2\n\nd = {}\nd[\"k\"] = f(1)\nprint(d)\n",
			"{'k': 'a'}\n",
		},
		{
			"written into a slot",
			"def f(x):\n    if x:\n        return \"a\"\n    return 2\n\nys = [0]\nys[0] = f(1)\nprint(ys)\n",
			"['a']\n",
		},
		{
			"one kind is untouched",
			"def f(x):\n    return x * 2\n\nprint(f(3))\nprint(f(0))\n",
			"6\n0\n",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			// The reference first: a row whose oracle answer drifted is a stale row, not a bug.
			want, ok := cpythonPlainOut(t, dir, r.src)
			if !ok {
				t.Skip("no python3 available to cross-check")
			}
			if want != r.want {
				t.Fatalf("row is stale: python3 prints %q, row pins %q", want, r.want)
			}
			for _, engine := range cliEngines {
				p := writeSrc(t, dir, "mixed_answer", r.src)
				out, code := cliRunMerged(t, engine, "--file", p)
				if code == 2 {
					t.Fatalf("%s exited 2 (the compiler's bug, forbidden by ADR 0166): %s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints %q: %s", engine, code, want, out)
				}
				if out != want {
					t.Errorf("%s printed %q, want %q", engine, out, want)
				}
			}
		})
	}
}

// The machine path: an agent reading the module must be able to see that a call answers a value whose
// kind travels beside it. The answer direction declares one internal global per carried body — the
// same word the pair road uses — and the `return` road that ran stores into it. `--emit-llvm` is the
// discoverable face of that fact (ADR 0343's rule that a run-scoped scan is visible in the IR, ADR
// 0368's machine path).
func TestACarriedAnswerIsVisibleInTheEmittedModule(t *testing.T) {
	t.Parallel()
	mixed := "def f(x):\n    if x:\n        return \"x\"\n    return 0\n\nprint(f(1))\nprint(f(0))\n"
	out, code := cliRunMerged(t, "--emit-llvm", mixed)
	if code != 0 {
		t.Fatalf("--emit-llvm exited %d: %s", code, out)
	}
	// One internal global per carried body, the word the `return` road that ran fills, and the load the
	// caller reads it back with — all three are visible, so an agent can tell a carried answer from a
	// one-word answer without running the program.
	for _, want := range []string{
		"@gy_f.anst = internal global i32 0",
		// the text arm stores TagStr, the number arm TagInt, the fall-off-the-end TagNone
		"store i32 4, i32* @gy_f.anst",
		"store i32 0, i32* @gy_f.anst",
		"store i32 3, i32* @gy_f.anst",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the module does not %s:\n%s", describeIRFact(want), firstLines(out, 20))
		}
	}
	if !strings.Contains(out, "load i32, i32* @gy_f.anst") && !strings.Contains(out, "= load i32, i32* @gy_f.anst") {
		t.Errorf("no caller reads the kind word the body stored:\n%s", firstLines(out, 20))
	}

	// A body that answers ONE kind keeps every road it had: no kind word is declared for it, because a
	// second opinion could only take an answer away (roadmap ADR 0368's "what this pass will NOT claim").
	oneKind := "def g(x):\n    return x * 2\n\nprint(g(3))\n"
	out2, code2 := cliRunMerged(t, "--emit-llvm", oneKind)
	if code2 != 0 {
		t.Fatalf("--emit-llvm exited %d: %s", code2, out2)
	}
	if strings.Contains(out2, "@gy_g.anst") {
		t.Errorf("a one-kind body was given a kind word it does not need:\n%s", firstLines(out2, 20))
	}
}

// describeIRFact turns an asserted instruction into the sentence about it the failure should read.
func describeIRFact(want string) string {
	switch {
	case strings.Contains(want, "internal global"):
		return "declare the kind word the answer direction stores"
	case strings.HasPrefix(want, "store i32 4"):
		return "store the text arm's word (TagStr) beside its payload"
	case strings.HasPrefix(want, "store i32 0"):
		return "store the number arm's word (TagInt) beside its payload"
	case strings.HasPrefix(want, "store i32 3"):
		return "store the fall-off-the-end's word (TagNone) beside its payload"
	}
	return "emit " + want
}

// TestWhatTheAnswerDirectionStillCannotReachIsFiledNotFixed is the honest half of this file: the five
// shapes ADR 0368 leaves wrong, each pinned with the reference's answer beside the compiled one, so the
// day a door opens the row fails and the row in `roadmap.md` can be closed. None of them is allowed to
// exit 2 (ADR 0166's rule — a refusal may exit 1, a broken module may not), and none of them is allowed
// to be silently deleted: the answers below were measured against a build of the parent commit, where
// every one of them already differed, so this table files what exists rather than what regressed.
func TestWhatTheAnswerDirectionStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	rows := []struct {
		name     string
		src      string
		python   string
		compiled string
		why      string
	}{
		{
			name:     "answer into an ordinary parameter",
			src:      "def f(x):\n    if x:\n        return \"x\"\n    return 0\n\ndef g(v):\n    return v\n\nprint(g(f(1)))\nprint(g(f(0)))\n",
			python:   "x\n0\n",
			compiled: "0\n0\n",
			why:      "the argument position keeps one word (Gap R.146, Gap R.220's twin) — Gap R.324",
		},
		{
			name:     "a method's mixed arms",
			src:      "class C:\n    def m(self, x):\n        if x:\n            return \"x\"\n        return 0\n\nc = C()\nprint(c.m(1))\nprint(c.m(0))\n",
			python:   "x\n0\n",
			compiled: "x\nx\n",
			why:      "a method answers through emitClassMethod, which shares none of the return roads stmtReturn walks — Gap R.324",
		},
		{
			name:     "a float arm beside an int arm",
			src:      "def f(x):\n    if x:\n        return 1.5\n    return 2\n\nprint(f(1))\nprint(f(0))\n",
			python:   "1.5\n2\n",
			compiled: "1.5\n2.0\n",
			why:      "the answer direction has no word for a double, so the float table answers both arms — Gap R.324",
		},
		{
			name:     "a text arm beside a float arm",
			src:      "def f(x):\n    if x:\n        return \"a\"\n    return 1.5\n\nprint(f(1))\nprint(f(0))\n",
			python:   "a\n1.5\n",
			compiled: "refuses: a string cannot be lifted to a double",
			why:      "the same missing float word, and the good face of it: words rather than a number the reference never prints — Gap R.324",
		},
		{
			name:     "a conditional answer",
			src:      "def f(x):\n    return \"x\" if x else 0\n\nprint(f(1))\nprint(f(0))\n",
			python:   "x\n0\n",
			compiled: "0\n0\n",
			why:      "one `return` road whose two arms answer different kinds needs a run-time word, not the constant this scan writes — Gap R.324",
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
			if want != r.python {
				t.Fatalf("row is stale: python3 prints %q, row pins %q", want, r.python)
			}
			p := writeSrc(t, dir, "mixed_answer_debt", r.src)
			out, code := cliRunMerged(t, "--aot", "--file", p)
			if code == 2 {
				t.Fatalf("exit 2 on a mixed-return body (ADR 0166 forbids it): %s", out)
			}
			got := ""
			if code == 0 {
				got = out
			} else {
				first := strings.SplitN(strings.TrimSpace(out), "\n", 2)[0]
				got = "refuses: " + refusedAbout(first)
			}
			if got != r.compiled {
				t.Fatalf("the filed answer moved: compiled says %q, the row pins %q (%s)\nfull output:\n%s",
					got, r.compiled, r.why, out)
			}
			if got == want {
				t.Fatalf("the door opened — this shape now answers the reference's %q, so delete the row and close Gap R.324's half that names it", want)
			}
		})
	}
}

// refusedAbout shortens a refusal to the clause a reader needs: the sentence is long by house style, and
// a filed row pins what it is ABOUT, not every comma the compiler chose.
func refusedAbout(line string) string {
	for _, key := range []string{
		"a string cannot be lifted to a double",
		"hands back the (payload, tag) pair",
	} {
		if strings.Contains(line, key) {
			return key
		}
	}
	if i := strings.Index(line, ": "); i >= 0 && i+10 < len(line) {
		return line[i+2:][:40]
	}
	return line[:min(40, len(line))]
}

func firstLines(s string, n int) string {
	parts := strings.Split(s, "\n")
	if len(parts) > n {
		parts = parts[:n]
	}
	return strings.Join(parts, "\n")
}
