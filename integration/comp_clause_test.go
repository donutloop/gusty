package integration

// integration/comp_clause_test.go — a comprehension's clause list runs at the CLI, on the compiled path,
// and prints what the same source prints in the reference (roadmap Gap R.352, ADR 0383).
//
// `[y for x in rows for y in x]` is two clauses read left to right, and until this row the second one was
// a parse error — `expected "]"` — so the flattening idiom did not exist here. Every row below is run
// three ways: `python3`, and `gustyc --file` on each CLI engine. The reference's answer is READ OUT in the
// row rather than written into it, because a row that quotes a remembered answer starts documenting the bug
// the moment the answer moves.
//
// The three classes:
//
//   - a clause list the reference answers, answers at exit 0 with byte-identical stdout;
//   - a clause list this backend cannot yet walk, REFUSES — exit 1 with a `codegen:` sentence naming the
//     shape and the row — and never answers with a value the reference does not give;
//   - exit 2, the code ADR 0166 reserves for a compiler bug, fails every row here, including the refusals:
//     a refusal that reaches `llc` and is rejected is a crash wearing a refusal's clothes.

import (
	"strings"
	"testing"
)

// clauseListAnswerRows are the programs the row measured, answered.
func clauseListAnswerRows() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		{
			"the row's flattening idiom",
			"print([y for x in [[1, 2], [3, 4]] for y in x])\n",
			"[1, 2, 3, 4]\n",
		},
		{
			"the row's pair product",
			"print([[x, y] for x in [1, 2] for y in [3, 4]])\n",
			"[[1, 3], [1, 4], [2, 3], [2, 4]]\n",
		},
		{
			"a sum over a product",
			"print([x + y for x in [1, 2] for y in [3, 4]])\n",
			"[4, 5, 5, 6]\n",
		},
		{
			"a range on either side",
			"print([x*10 + y for x in range(2) for y in range(2)])\n",
			"[0, 1, 10, 11]\n",
		},
		{
			"three clauses",
			"print([x + y + z for x in [1] for y in [2] for z in [3]])\n",
			"[6]\n",
		},
		{
			"a filter on the inner clause",
			"print([x + y for x in [1, 2] for y in [3, 4] if y > 3])\n",
			"[5, 6]\n",
		},
		{
			"a filter on each clause",
			"print([x for x in [1, 2, 3] if x > 1 for y in [10, 20] if y > 10])\n",
			"[2, 3]\n",
		},
		{
			"an outer filter that skips the whole inner loop",
			"print([y for x in [1, 2] if x > 1 for y in [7, 8]])\n",
			"[7, 8]\n",
		},
		{
			"a set comprehension",
			"print({x + y for x in [1] for y in [2]})\n",
			"{3}\n",
		},
		{
			"a dict comprehension",
			"print({x: y for x in [1] for y in [2]})\n",
			"{1: 2}\n",
		},
		{
			"a bound with holes in it",
			"print([x + y for x in [1, 2] for y in [10, 20, 30] if y != 20])\n",
			"[11, 31, 12, 32]\n",
		},
		{
			"a comprehension bound and read back",
			"xs = [y for x in [[1, 2], [3]] for y in x]\nprint(xs)\nprint(len(xs))\n",
			"[1, 2, 3]\n3\n",
		},
	}
}

// TestTheReferenceAnswersEveryClauseListRow pins the table to the reference: each row records an ANSWER,
// so CPython must still print it. A row whose reference side stops working is caught here, not by the
// compiled leg quietly agreeing with itself.
func TestTheReferenceAnswersEveryClauseListRow(t *testing.T) {
	t.Parallel()
	for _, tc := range clauseListAnswerRows() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			py, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok || py != tc.want {
				t.Fatalf("the expectation is not the reference's: python said %q (ok %v), the row says %q\nsrc: %s", py, ok, tc.want, tc.src)
			}
		})
	}
}

// TestAClauseListPrintsWhatTheReferencePrintsAtTheCLI is the row's claim at the interface a person and an
// agent both use.
func TestAClauseListPrintsWhatTheReferencePrintsAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range clauseListAnswerRows() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			gy := writeSrc(t, dir, "comp_clauses.gy", tc.src)
			for _, engine := range cliEngines {
				out, code := cliRunCode(t, engine, "--file", gy)
				if code == 2 {
					t.Fatalf("%s: exit 2 — LLVM rejected the module this comprehension was emitted into (ADR 0166):\n%s", engine, out)
				}
				if strings.Contains(out, "codegen:") || strings.Contains(out, "parse error") {
					t.Fatalf("%s: a program the reference runs came back as a toolchain failure:\n%s", engine, out)
				}
				if code != 0 {
					t.Fatalf("%s: exit %d, want 0\n%s", engine, code, out)
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want the reference's %q\nsrc: %s", engine, out, tc.want, tc.src)
				}
			}
		})
	}
}

// clauseListFiledRows are the shapes the row leaves owed, each with the words its refusal has to contain.
func clauseListFiledRows() []struct{ name, src, sentence, row string } {
	return []struct{ name, src, sentence, row string }{
		{
			"an outer clause over a container the run time built",
			"rows = [[1, 2], [3, 4]]\nprint([y for x in rows for y in x])\n",
			"a container the run time builds", "Gap R.356",
		},
		{
			"a clause over texts",
			"print([c for w in [\"ab\", \"cd\"] for c in w])\n",
			"walks an element the comprehension road cannot carry", "Gap R.352",
		},
		{
			"a clause over floats",
			"print([x + y for x in [1.5] for y in [1]])\n",
			"walks an element the comprehension road cannot carry", "Gap R.352",
		},
	}
}

// TestWhatTheClauseListStillCannotWalkRefusesInItsOwnWords is the honest half: a shape this backend does
// not lower says so, names the row that owns it, and does not answer. Exit 0 with a value is the failure
// mode this guards; exit 2 is the one it guards against harder.
func TestWhatTheClauseListStillCannotWalkRefusesInItsOwnWords(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range clauseListFiledRows() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gy := writeSrc(t, dir, "comp_clauses_filed.gy", tc.src)
			out, code := cliRunMerged(t, "--file", gy)
			if code == 0 {
				t.Fatalf("this shape answered at exit 0, which means the filed row (%s) is paid and must be promoted, not re-asserted:\n%s", tc.row, out)
			}
			if code == 2 {
				t.Fatalf("exit 2 — the compiler's own module was rejected (ADR 0166), not a refusal:\n%s", out)
			}
			if !strings.Contains(out, tc.sentence) {
				t.Fatalf("the refusal no longer names the shape it files:\n got %q\nwant it to contain %q", out, tc.sentence)
			}
			if !strings.Contains(out, tc.row) {
				t.Fatalf("a refusal has to name the row that owns it (ADR 0346):\n got %q\nwant it to cite %q", out, tc.row)
			}
		})
	}
}

// TestTheFormatterAtTheCLIKeepsEveryClause is the machine path for `gusty fmt`: an agent that formats a
// source on save must not lose the second half of a comprehension.
func TestTheFormatterAtTheCLIKeepsEveryClause(t *testing.T) {
	t.Parallel()
	const src = "print([y for x in [[1, 2], [3, 4]] for y in x])\n"
	out, code := cliRunMerged(t, "--fmt", src)
	if code != 0 {
		t.Fatalf("--fmt exited %d:\n%s", code, out)
	}
	if got := strings.Count(out, " for "); got != 2 {
		t.Fatalf("the formatter wrote %d `for` clauses where the source has 2:\n%s", got, out)
	}
	// And what it wrote still compiles to the same answer.
	dir := t.TempDir()
	gy := writeSrc(t, dir, "formatted.gy", out)
	got, code := cliRunMerged(t, "--file", gy)
	if code != 0 || got != "[1, 2, 3, 4]\n" {
		t.Fatalf("the formatted source answers %q at exit %d, want the reference's [1, 2, 3, 4]\nwrote: %s", got, code, out)
	}
}
