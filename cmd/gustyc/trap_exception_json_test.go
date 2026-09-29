package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// The machine path for ADR 0214: because every built-in trap now carries a class, the JSON payload
// reports it as data. An agent must be able to branch on the exception *name*; making it match on
// `error` text (or re-parse the traceback it is also handed) is the failure this pins down.
func TestRuntimeTrapJSONReportsItsExceptionClass(t *testing.T) {
	cases := []struct {
		src   string
		class string
		msg   string
	}{
		{"x = 5\nx()\n", "TypeError", "'int' object is not callable"},
		{"print(int(\"abc\"))\n", "ValueError", "invalid literal for int() with base 10: 'abc'"},
		{"print(len(5))\n", "TypeError", "object of type 'int' has no len()"},
		{"a, b = [1]\n", "ValueError", "not enough values to unpack (expected 2, got 1)"},
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
		if want := "eval error: " + tc.msg; rec["error"] != want {
			t.Errorf("%q: error = %v, want %q", tc.src, rec["error"], want)
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
