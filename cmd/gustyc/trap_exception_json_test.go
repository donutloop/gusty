package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The machine path for ADR 0214: because every built-in trap now carries a class, the JSON payload
// reports it as data. An agent must be able to branch on the exception *name*; making it match on
// `error` text (or re-parse the traceback it is also handed) is the failure this pins down.
// The shapes the compiled runtime raises for, in the reference's own words. Each row is a program
// that *runs* and then traps: exit 3, the class named as data, and the same sentence in the
// traceback the payload also carries.
func TestRuntimeTrapJSONReportsItsExceptionClass(t *testing.T) {
	cases := []struct {
		src   string
		class string
		msg   string
	}{
		{"print(1 / 0)\n", "ZeroDivisionError", "division by zero"},
		{"xs = [1]\nprint(xs[5])\n", "IndexError", "index out of range"},
		{`raise ValueError("boom")` + "\n", "ValueError", "boom"},
	}
	for _, tc := range cases {
		out, code := cliExit(t, "--json", "--eval", tc.src)
		if code != exitRuntime {
			t.Fatalf("%q: exit = %d, want %d (%s)", tc.src, code, exitRuntime, out)
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &rec); err != nil {
			t.Fatalf("%q: not JSON: %v\n%s", tc.src, err, out)
		}
		if got := rec["exception"]; got != tc.class {
			t.Errorf("%q: exception = %v, want %q", tc.src, got, tc.class)
		}
		if got := rec["exception_message"]; got != tc.msg {
			t.Errorf("%q: exception_message = %v, want %q", tc.src, got, tc.msg)
		}
		// The class and the report have to agree — a payload that says one thing in
		// `exception` and another in `traceback` is worse than no field at all.
		tb, _ := rec["traceback"].(string)
		if !strings.Contains(tb, tc.class+": "+tc.msg) {
			t.Errorf("%q: traceback does not read %q:\n%s", tc.src, tc.class+": "+tc.msg, tb)
		}
		if want := tc.class + ": " + tc.msg; rec["error"] != want {
			t.Errorf("%q: error = %v, want %q", tc.src, rec["error"], want)
		}
	}
}

// The shapes the compiled backend declines to build at all, filed rather than fixed. The retired
// engine ran these and raised — CPython raises for them too, at the moment the bad value is asked
// what it is — and the compiled backend has no lowering that gets that far, so it stops the program
// at the compile door: exit 1, and a sentence that says which half is missing.
//
// That is the honest *failure class* (it is not a crash, and it is not the compiler's own exit 2),
// but it is not the language's answer, and the row says so. Each entry names the roadmap gap that
// owes the runtime behaviour, and each is asserted to *stay* a refusal: if one of these ever exits
// 0, the case fails, because an answer that arrived without the trap being implemented would be the
// wrong answer at exit 0 — the class of bug this repo counts as worse than a refusal.
func TestCompiledBackendRefusesWhereTheReferenceTraps(t *testing.T) {
	cases := []struct {
		src  string
		want string // what the refusal has to name
		gap  string
		trap string // what the reference raises, for the record
	}{
		{"x = 5\nx()\n", "is not a function", "roadmap L11.1 (a value that can be called needs the tagged value word)", "TypeError: 'int' object is not callable"},
		{"print(int(\"abc\"))\n", "non-integer string", "roadmap Gap I.2 (int() over text the compiler cannot read)", "ValueError: invalid literal for int() with base 10: 'abc'"},
		{"print(len(5))\n", "inline list/dict/set literal", "roadmap Gap L11.1 (len() over a value whose kind is a run-time fact)", "TypeError: object of type 'int' has no len()"},
		{"a, b = [1]\n", "length mismatch", "roadmap Gap K.x (unpacking a container whose length is a run-time fact)", "ValueError: not enough values to unpack (expected 2, got 1)"},
	}
	for _, tc := range cases {
		out, code := cliExit(t, "--json", "--eval", tc.src)
		if code == 0 {
			t.Errorf("%q answered (%s) where the reference raises %s — the runtime trap is not implemented, so an answer here is a wrong answer", tc.src, out, tc.trap)
			continue
		}
		if code != exitCompileError {
			t.Errorf("%q: exit = %d, want %d (a refusal, not a crash, and never the compiler's own %d): %s", tc.src, code, exitCompileError, exitIRVerify, out)
		}
		if !strings.Contains(out, tc.want) {
			t.Errorf("%q: the refusal does not name the missing half (%q): %s\n%s", tc.src, tc.want, tc.gap, out)
		}
	}
}

// A front-end refusal is not a runtime trap: no class, exit 1, and the exception members absent —
// so the field means "the program raised", never "the compiler complained".
func TestFrontEndRejectionJSONCarriesNoExceptionClass(t *testing.T) {
	out, code := cliExit(t, "--json", "--eval", "def f(: int) -> int:\n    return x + y\n")
	if code == exitRuntime {
		t.Fatalf("a front-end rejection reported a runtime exit: %s", out)
	}
	if strings.Contains(out, "\"exception\"") {
		t.Errorf("a non-trap failure grew exception members: %s", out)
	}
}
