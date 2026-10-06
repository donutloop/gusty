// Package integration exercises the whole pipeline for the effect / async
// exhaustiveness pass (roadmap Phase 7, L7.6, ADR 0195).
//
// What this feature is *for* is measurable: before the checker, one program had
// three answers.
//
//	async def f(x):
//	    return x + 1
//	v = f(2)
//	print(v)
//
// The interpreter printed `<coro>` (a coroutine handle printed as a value), the
// compiled backend printed `2` (it ran the body eagerly at the call), and CPython
// printed a coroutine repr plus a RuntimeWarning. Two backends that disagree with
// each other is exactly what the parity matrix is built to catch — and it caught
// nothing, because the compiled path "worked". The checker is the fix: a program whose
// await/return discipline is broken is refused, on every path, before any backend
// gets to invent an answer.
package integration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// checkCode asserts that CheckSource reports the given code at the given level and
// that the documented exit code follows from it (1 with an error, 0 with warnings
// only).
func checkCode(t *testing.T, src, code string, lvl lang.Level, exit int) {
	t.Helper()
	res, err := lang.CheckSource(src)
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	var found *lang.Diagnostic
	for i, d := range res.Diagnostics {
		if d.Code == code {
			found = &res.Diagnostics[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected %s for:\n%s\ngot %v", code, src, res.Diagnostics)
	}
	if found.Level != lvl {
		t.Errorf("%s at level %s, want %s (%s)", code, found.Level, lvl, found.Msg)
	}
	if found.Suggestion == "" {
		t.Errorf("%s carries no suggestion: an agent needs the fix, not just the fault", code)
	}
	if res.Exit != exit {
		t.Errorf("exit = %d, want %d", res.Exit, exit)
	}
}

// noCode asserts that a legal program is left alone.
func noCode(t *testing.T, src, code string) {
	t.Helper()
	res, err := lang.CheckSource(src)
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	for _, d := range res.Diagnostics {
		if d.Code == code {
			t.Fatalf("unexpected %s for:\n%s\ngot %v", code, src, res.Diagnostics)
		}
	}
}

// The refused programs, one per rule, with the message the human and the agent
// both read.
func TestCheckRefusesBrokenAwaitDiscipline(t *testing.T) {
	t.Run("never awaited", func(t *testing.T) {
		checkCode(t, "async def f(x):\n    return x + 1\nv = f(2)\nprint(v)\n",
			lang.CodeCoroNeverAwaited, lang.LevelError, 1)
		checkCode(t, "async def f(x):\n    return x + 1\nf(2)\n",
			lang.CodeCoroNeverAwaited, lang.LevelError, 1)
	})
	t.Run("awaited twice", func(t *testing.T) {
		checkCode(t, "async def f(x):\n    return x + 1\na = f(1)\nprint(await a)\nprint(await a)\n",
			lang.CodeCoroAwaitedTwice, lang.LevelError, 1)
	})
	// The two context rules are warnings: our model evaluates what CPython would
	// suspend on, so the program still means something here — it is the reference
	// implementation that rejects it outright.
	t.Run("await outside a coroutine", func(t *testing.T) {
		checkCode(t, "async def f(x):\n    return x\ndef g():\n    return await f(1)\nprint(g())\n",
			lang.CodeAwaitOutsideCoroutine, lang.LevelWarning, 0)
	})
	t.Run("async for outside a coroutine", func(t *testing.T) {
		checkCode(t, "async def f(x):\n    return x\ndef g():\n    async for v in [f(1)]:\n        print(v)\ng()\n",
			lang.CodeAsyncStmtOutsideCoroutine, lang.LevelWarning, 0)
	})
	t.Run("async def that yields", func(t *testing.T) {
		checkCode(t, "async def gen():\n    yield 1\n",
			lang.CodeAsyncGeneratorUnsupported, lang.LevelError, 1)
	})
	t.Run("await on a non-coroutine", func(t *testing.T) {
		checkCode(t, "def dbl(x):\n    return x * 2\nprint(await dbl(3))\n",
			lang.CodeAwaitNotCoroutine, lang.LevelWarning, 0)
	})
	t.Run("missing return", func(t *testing.T) {
		checkCode(t, "async def f(x) -> int:\n    if x > 0:\n        return x\nprint(await f(1))\n",
			lang.CodeAsyncMissingReturn, lang.LevelWarning, 0)
	})
}

// The legal programs the rules must not touch — the same shapes the conformance
// corpus runs, plus the handoffs a checker has to respect.
func TestCheckAcceptsLegalAsync(t *testing.T) {
	errorCodes := []string{
		lang.CodeCoroNeverAwaited, lang.CodeCoroAwaitedTwice,
		lang.CodeAwaitNotCoroutine, lang.CodeAsyncGeneratorUnsupported,
		lang.CodeAsyncMissingReturn,
	}
	for _, src := range []string{
		"async def f(x):\n    return x + 1\nprint(await f(2))\n",
		"async def f(x):\n    return x + 1\nv = f(2)\nprint(await v)\n",
		"async def f(x):\n    return x * 2\ntasks = [f(1), f(2)]\nprint(len(tasks))\n",
		"async def f(x):\n    return x * 2\nasync for v in [f(1), f(2)]:\n    print(v)\n",
		"async def f(x):\n    return x + 1\ndef run(c):\n    return await c\nprint(run(f(1)))\n",
		"async def main() -> None:\n    print(1)\nprint(await main())\n",
	} {
		res, err := lang.CheckSource(src)
		if err != nil {
			t.Fatalf("CheckSource: %v", err)
		}
		if !res.OK {
			t.Errorf("a legal async program was refused: %v", res.Diagnostics)
		}
		for _, code := range errorCodes {
			noCode(t, src, code)
		}
	}
}

// The headline claim: a dropped coroutine is refused, so there is no program with two answers. Before
// L7.6 the AST interpreter printed "<coro>" here and the compiled backend printed "2" — the kind of
// disagreement a second engine manufactures. With one backend the question is asked once, and the
// checker answers it before anyone runs anything.
func TestUnawaitedCoroutineIsRefused(t *testing.T) {
	src := "async def f(x):\n    return x + 1\nv = f(2)\nprint(v)\n"

	// The path the CLI takes (--file -> analyse -> refuse) must refuse it, exit non-zero, and never
	// reach an answer.
	dir := t.TempDir()
	path := filepath.Join(dir, "prog.gy")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, code := cliRunCode(t, "--aot", path); code != 1 {
		t.Errorf("--aot exit = %d, want 1: a dropped coroutine must be a compile error, not an invented answer", code)
	}
	if _, code := cliRunCode(t, "--check="+src); code != 1 {
		t.Errorf("--check exit = %d, want 1", code)
	}
	res, cerr := lang.CheckSource(src)
	if cerr != nil {
		t.Fatalf("CheckSource: %v", cerr)
	}
	if res.OK {
		t.Errorf("check accepted it: %v", res.Diagnostics)
	}
	// Running it is refused too, not merely discouraged: the program cannot print "2".
	if _, err := lang.JIT(src, 0); err == nil {
		t.Errorf("the compiled backend ran a program with a dropped coroutine")
	}
	// CPython, the reference the ledger is kept against, agrees that this program is broken — it just
	// does so at runtime with a RuntimeWarning instead of at compile time with a code.
	if _, stderr, err := lang.PythonRun(src); err == nil && !strings.Contains(stderr, "never awaited") {
		t.Logf("python stderr (informational): %q", stderr)
	}
}

// And the legal deferred call runs the way the record says it does.
func TestDeferredAwaitRunsAsRecorded(t *testing.T) {
	src := "async def f(x):\n    return x + 1\nasync def g(x):\n    return x * 2\na = f(2)\nb = g(3)\nprint(await a)\nprint(await b)\n"
	want := "3\n6\n"
	lang.RecordedStdoutIs(t, src, want)
	res, err := lang.JIT(src, 0)
	if err != nil {
		t.Fatalf("JIT: %v", err)
	}
	if res.Output != want {
		t.Errorf("compiled = %q, want %q", res.Output, want)
	}
}

// The conformance row: the legal-async program prints the pinned output and is silent under every
// effect rule.
func TestAsyncEffectsConformanceRow(t *testing.T) {
	src := readProgramSrc("async_effects")
	res, err := lang.CheckSource(src)
	if err != nil {
		t.Fatalf("CheckSource: %v", err)
	}
	if len(res.Diagnostics) != 0 {
		t.Fatalf("the legal-async corpus program reported diagnostics: %v", res.Diagnostics)
	}
	want := "24\n18\n10\n42\n15\n6\n20\n3\n2\n4\n"
	lang.RecordedStdoutIs(t, src, want)
	if got, err := safeAOTRun(t, src); err != nil {
		t.Fatalf("aot: %v", err)
	} else if got != want {
		t.Errorf("aot = %q, want %q", got, want)
	}
}

// Machine path: `gustyc --effects` is the self-describing view of the same facts
// the checker decided from.
func TestCLIEffectsJSON(t *testing.T) {
	src := "async def fetch(n):\n    total = 0\n    for i in range(n):\n        total += i\n    if total > 3:\n        raise ValueError(\"big\")\n    return await helper(total)\nasync def helper(x):\n    return x\ndef plain(x):\n    return x * 2\n"
	out, code := cliRunCode(t, "--effects="+src, "--json")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (out=%s)", code, out)
	}
	var doc struct {
		SchemaVersion string           `json:"schema_version"`
		GeneratedBy   string           `json:"generated_by"`
		Functions     []map[string]any `json:"functions"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("effects output is not JSON: %v\n%s", err, out)
	}
	if doc.SchemaVersion != "1.0" || doc.GeneratedBy != "gustyc --effects" {
		t.Errorf("effects document lost its version/provenance: %+v", doc)
	}
	byName := map[string]map[string]any{}
	for _, fn := range doc.Functions {
		byName[fn["function"].(string)] = fn
	}
	if _, ok := byName["<module>"]; !ok {
		t.Errorf("no <module> row in %v", doc.Functions)
	}
	fetch, ok := byName["fetch"]
	if !ok {
		t.Fatalf("no row for fetch: %v", doc.Functions)
	}
	if fetch["async"] != true {
		t.Errorf("fetch async = %v, want true", fetch["async"])
	}
	if fetch["terminates"] != true || fetch["falls_through"] != false {
		t.Errorf("fetch termination wrong: %v", fetch)
	}
	effects, _ := fetch["effects"].([]any)
	joined := make([]string, 0, len(effects))
	for _, e := range effects {
		joined = append(joined, e.(string))
	}
	if strings.Join(joined, ",") != "await,raise" {
		t.Errorf("fetch effects = %v, want [await raise]", effects)
	}
	if fetch["raises"] != float64(1) {
		t.Errorf("fetch raises = %v, want 1", fetch["raises"])
	}
	if fetch["coroutine_calls"] != float64(1) {
		t.Errorf("fetch coroutine_calls = %v, want 1", fetch["coroutine_calls"])
	}
	if plain := byName["plain"]; plain == nil || plain["async"] != false || len(plain["effects"].([]any)) != 0 {
		t.Errorf("plain signature wrong: %v", plain)
	}
}

// The human view of the same table, and the refusal path's exit code.
func TestCLIEffectsHumanAndRefusal(t *testing.T) {
	human := cliRun(t, "--effects=async def f(x):\n    return x\nasync def g():\n    return await f(1)\nprint(await g())\n")
	for _, want := range []string{"effect signatures for <src>", "async def", "falls_through=no"} {
		if !strings.Contains(human, want) {
			t.Errorf("effects table lost %q:\n%s", want, human)
		}
	}
	badSrc := "--effects=async def f(x):\n    return x\nv = f(1)\nprint(v)\n"
	_, code := cliRunCode(t, badSrc)
	if code != 1 {
		t.Errorf("exit = %d, want 1 for a refused program", code)
	}
	if refused := cliRun(t, badSrc); !strings.Contains(refused, "never awaited") {
		t.Errorf("refusal diagnostic missing:\n%s", refused)
	}
}

// The check-mode JSON carries the codes, which is how an agent branches on the
// rule instead of matching prose.
func TestCheckJSONCarriesAsyncCodes(t *testing.T) {
	out, code := cliRunCode(t, "--check=async def f(x):\n    return x\nv = f(1)\nprint(v)\n", "--json")
	if code != 1 {
		t.Errorf("exit = %d, want 1 (out=%s)", code, out)
	}
	if !strings.Contains(out, `"`+lang.CodeCoroNeverAwaited+`"`) {
		t.Errorf("--check --json lost the diagnostic code:\n%s", out)
	}
}
