package lang

import (
	"strings"
	"testing"
)

// Unit tests for the effect / async exhaustiveness pass (roadmap Phase 7, L7.6,
// ADR 0195).
//
// Every rule is tested in both directions: the program that must be reported, and
// the legitimate program next to it that must not be. A checker that only has the
// first half of each pair is a noise machine, and the corpus would tell within one
// CI run.

func diagsWithCode(diags []Diagnostic, code string) []Diagnostic {
	var out []Diagnostic
	for _, d := range diags {
		if d.Code == code {
			out = append(out, d)
		}
	}
	return out
}

// checkDiags runs the whole front end (parse + Analyze) on src.
func checkDiags(t *testing.T, src string) []Diagnostic {
	t.Helper()
	return Analyze(parseOrFatal(t, src))
}

// mustReport asserts that src produces a diagnostic with the given code (and, when
// want is non-empty, a message containing it) at the given level.
func mustReport(t *testing.T, src, code, want string, lvl Level) {
	t.Helper()
	got := diagsWithCode(checkDiags(t, src), code)
	if len(got) == 0 {
		t.Fatalf("expected a %s (%s) diagnostic for:\n%s\ngot %v", code, lvl, src, checkDiags(t, src))
	}
	if got[0].Level != lvl {
		t.Errorf("%s reported at %s, want %s", code, got[0].Level, lvl)
	}
	if want != "" && !strings.Contains(got[0].Msg, want) {
		t.Errorf("%s message %q does not mention %q", code, got[0].Msg, want)
	}
}

// mustNotReport asserts that src produces no diagnostic with the given code.
func mustNotReport(t *testing.T, src, code string) {
	t.Helper()
	if got := diagsWithCode(checkDiags(t, src), code); len(got) != 0 {
		t.Fatalf("unexpected %s diagnostic for:\n%s\ngot %v", code, src, got)
	}
}

// --- never awaited ---------------------------------------------------------

func TestEffectsNeverAwaitedBinding(t *testing.T) {
	dropped := `async def f(x):
    return x + 1
v = f(2)
print(v)
`
	mustReport(t, dropped, CodeCoroNeverAwaited, "never awaited", LevelError)

	// Awaited later: the deferred-coroutine idiom, untouched.
	mustNotReport(t, `async def f(x):
    return x + 1
v = f(2)
print(await v)
`, CodeCoroNeverAwaited)

	// Handing the coroutine to a user function is a handoff: that function may
	// await it, and the pass cannot follow the value across a call boundary.
	mustNotReport(t, `async def f(x):
    return x + 1
def run(c):
    return await c
print(run(f(2)))
`, CodeCoroNeverAwaited)

	// Storing coroutines in a container (a task list) is the same story.
	mustNotReport(t, `async def f(x):
    return x * 2
tasks = [f(1), f(2)]
print(len(tasks))
`, CodeCoroNeverAwaited)
}

func TestEffectsNeverAwaitedBareCall(t *testing.T) {
	mustReport(t, `async def f(x):
    return x + 1
f(2)
print(1)
`, CodeCoroNeverAwaited, "never awaited", LevelError)

	// Consumed as a value where it can never be awaited again.
	mustReport(t, `async def f(x):
    return x + 1
print(f(2))
`, CodeCoroNeverAwaited, "never awaited", LevelError)

	// The same call in an `async for` iterable is exactly what the language asks
	// for: the loop awaits each element.
	mustNotReport(t, `async def f(x):
    return x * 2
async for v in [f(1), f(2), f(3)]:
    print(v)
`, CodeCoroNeverAwaited)
}

func TestEffectsNeverAwaitedRebound(t *testing.T) {
	// The second assignment is the moment the first coroutine dies.
	mustReport(t, `async def f(x):
    return x + 1
a = f(1)
a = 5
print(a)
`, CodeCoroNeverAwaited, "never awaited", LevelError)

	// Awaited, then rebound: nothing pending to drop.
	mustNotReport(t, `async def f(x):
    return x + 1
a = f(1)
b = await a
print(b)
`, CodeCoroNeverAwaited)
}

func TestEffectsNeverAwaitedInBranch(t *testing.T) {
	// A coroutine created inside a branch cannot be awaited after it, so the
	// branch is where the drop is reported.
	mustReport(t, `async def f(x):
    return x + 1
if 1 > 0:
    a = f(1)
print(2)
`, CodeCoroNeverAwaited, "never awaited", LevelError)

	// Awaited inside the same branch: fine.
	mustNotReport(t, `async def f(x):
    return x + 1
if 1 > 0:
    a = f(1)
    print(await a)
print(2)
`, CodeCoroNeverAwaited)
}

// --- awaited twice --------------------------------------------------------

func TestEffectsAwaitedTwice(t *testing.T) {
	mustReport(t, `async def f(x):
    return x + 1
a = f(1)
print(await a)
print(await a)
`, CodeCoroAwaitedTwice, "awaited twice", LevelError)

	// A fresh coroutine for the second call: not the same object.
	mustNotReport(t, `async def f(x):
    return x + 1
a = f(1)
print(await a)
a = f(1)
print(await a)
`, CodeCoroAwaitedTwice)
}

// --- await / async statements in the wrong place --------------------------

func TestEffectsAwaitOutsideCoroutine(t *testing.T) {
	// Level warning, not error: our model evaluates the operand instead of
	// suspending, so the program still means something — it is CPython that
	// rejects it outright.
	mustReport(t, `async def f(x):
    return x + 1
def g():
    return await f(1)
g()
`, CodeAwaitOutsideCoroutine, "cannot suspend", LevelWarning)

	// The module top level is the language's documented coroutine context
	// (CPython rejects it; docs/shared-lowering-spec.md accepts it, L5.6), so an
	// await there is not an error.
	mustNotReport(t, `async def f(x):
    return x + 1
print(await f(1))
`, CodeAwaitOutsideCoroutine)

	// Inside the async def itself: the normal case.
	mustNotReport(t, `async def g():
    return 1
async def f(x):
    return x + await g()
print(await f(1))
`, CodeAwaitOutsideCoroutine)
}

func TestEffectsAsyncStmtOutsideCoroutine(t *testing.T) {
	mustReport(t, `async def f(x):
    return x
def g():
    async for v in [1, 2]:
        print(v)
g()
`, CodeAsyncStmtOutsideCoroutine, "async for", LevelWarning)

	mustReport(t, `class M:
    def __enter__(self):
        return 1
    def __exit__(self):
        return 0
def g():
    async with M() as m:
        print(m)
g()
`, CodeAsyncStmtOutsideCoroutine, "async with", LevelWarning)

	mustNotReport(t, `async def f(x):
    return x
async def g():
    async for v in [f(1), f(2)]:
        print(await v)
print(1)
`, CodeAsyncStmtOutsideCoroutine)
}

func TestEffectsAwaitNotCoroutine(t *testing.T) {
	mustReport(t, `def dbl(x):
    return x * 2
print(await dbl(3))
`, CodeAwaitNotCoroutine, "plain def dbl(...)", LevelWarning)

	mustReport(t, `print(await 5)
`, CodeAwaitNotCoroutine, "integer literal", LevelWarning)

	// Awaiting something the pass cannot classify (a parameter) stays silent:
	// dynamic values are not the checker's business.
	mustNotReport(t, `async def run(x):
    return await x
print(1)
`, CodeAwaitNotCoroutine)

	// Awaiting an async call: the one true case.
	mustNotReport(t, `async def f(x):
    return x
print(await f(1))
`, CodeAwaitNotCoroutine)
}

// --- async generators, missing returns ------------------------------------

func TestEffectsAsyncGenerator(t *testing.T) {
	mustReport(t, `async def gen():
    yield 1
`, CodeAsyncGeneratorUnsupported, "yields", LevelError)

	// An if-arm is still a yield.
	mustReport(t, `async def gen(x):
    if x > 0:
        yield 1
`, CodeAsyncGeneratorUnsupported, "yields", LevelError)

	// A plain generator is a generator; a nested def is a different signature.
	mustNotReport(t, `async def outer(x):
    def inner():
        yield x
    return 1
print(await outer(1))
`, CodeAsyncGeneratorUnsupported)
}

func TestEffectsMissingReturn(t *testing.T) {
	mustReport(t, `async def f(x):
    if x > 0:
        return x
print(await f(1))
`, CodeAsyncMissingReturn, "can finish without a return", LevelWarning)

	mustReport(t, `async def f(x) -> int:
    if x > 0:
        return x
    else:
        print(1)
print(await f(1))
`, CodeAsyncMissingReturn, "annotated -> int", LevelWarning)

	// Every path returns: exhaustive.
	mustNotReport(t, `async def f(x) -> int:
    if x > 0:
        return x
    return 0
print(await f(1))
`, CodeAsyncMissingReturn)

	// `while True:` whose body always returns never reaches the end.
	mustNotReport(t, `async def f(x) -> int:
    while True:
        return x
print(await f(1))
`, CodeAsyncMissingReturn)

	// A match with a wildcard whose arms all return is exhaustive too; without
	// the wildcard it can fall out.
	mustNotReport(t, `async def f(x) -> int:
    match x:
        case 1:
            return 1
        case _:
            return 0
print(await f(1))
`, CodeAsyncMissingReturn)

	mustReport(t, `async def f(x) -> int:
    match x:
        case 1:
            return 1
print(await f(1))
`, CodeAsyncMissingReturn, "can finish without a return", LevelWarning)

	// Nothing promised, nothing missing: a procedure-shaped async def.
	mustNotReport(t, `async def main():
    print("hi")
print(await main())
`, CodeAsyncMissingReturn)

	// -> None promises None, so falling off the end is the point.
	mustNotReport(t, `async def main() -> None:
    print("hi")
print(await main())
`, CodeAsyncMissingReturn)

	// A sync def with the same shape is this rule's sibling, not this rule: only
	// an await hides the None behind it.
	mustNotReport(t, `def f(x) -> int:
    if x > 0:
        return x
print(f(1))
`, CodeAsyncMissingReturn)
}

// --- the signature itself (the --effects machine path) ---------------------

// summaryFor returns the row for a named function.
func summaryFor(t *testing.T, src, fn string) EffectSummary {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, s := range EffectSummaries(prog) {
		if s.Function == fn {
			return s
		}
	}
	t.Fatalf("no effect summary for %q in %v", fn, EffectSummaries(prog))
	return EffectSummary{}
}

func TestEffectSummarySignature(t *testing.T) {
	src := `async def fetch(n):
    total = 0
    for i in range(n):
        total += i
    if total > 3:
        raise ValueError("too big")
    return await helper(total)

def plain(x):
    return x * 2

def procedure(x):
    print(x)

async def helper(x):
    return x

class Box:
    async def load(self):
        return await helper(1)
`
	fetch := summaryFor(t, src, "fetch")
	if !fetch.Async {
		t.Errorf("fetch: Async = false, want true")
	}
	if got := strings.Join(fetch.Effects, ","); got != "await,raise" {
		t.Errorf("fetch effects = %v, want await,raise", fetch.Effects)
	}
	if fetch.Awaits == 0 {
		t.Errorf("fetch: no await counted")
	}
	if fetch.Raises != 1 {
		t.Errorf("fetch raises = %d, want 1", fetch.Raises)
	}
	if !fetch.ReturnsValue || fetch.FallsThrough || !fetch.Terminates {
		t.Errorf("fetch shape: returns=%v falls=%v terminates=%v, want true/false/true",
			fetch.ReturnsValue, fetch.FallsThrough, fetch.Terminates)
	}
	if fetch.Line == 0 {
		t.Errorf("fetch: no declaration line")
	}

	plain := summaryFor(t, src, "plain")
	if plain.Async || len(plain.Effects) != 0 || !plain.ReturnsValue || !plain.Terminates {
		t.Errorf("plain signature wrong: %+v", plain)
	}

	proc := summaryFor(t, src, "procedure")
	if proc.ReturnsValue || !proc.FallsThrough || proc.Terminates {
		t.Errorf("procedure signature wrong: %+v", proc)
	}

	// A method is reachable by its qualified name, so an agent can address it.
	load := summaryFor(t, src, "Box.load")
	if !load.Async || !load.Terminates {
		t.Errorf("Box.load signature wrong: %+v", load)
	}
}

func TestEffectSummaryCountsCoroutineCalls(t *testing.T) {
	src := `async def one(x):
    return x
async def driver():
    a = one(1)
    b = one(2)
    return await a + await b
`
	d := summaryFor(t, src, "driver")
	if d.CoroutineCalls != 2 {
		t.Errorf("driver coroutine_calls = %d, want 2", d.CoroutineCalls)
	}
	if d.Awaits != 2 {
		t.Errorf("driver awaits = %d, want 2", d.Awaits)
	}
}

func TestEffectSummaryModuleRow(t *testing.T) {
	prog, err := Parse("async def f():\n    return 1\nprint(await f())\n")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	rows := EffectSummaries(prog)
	if len(rows) < 2 || rows[0].Function != "<module>" {
		t.Fatalf("first row should be <module>, got %v", rows)
	}
	if rows[0].Awaits != 1 {
		t.Errorf("<module> awaits = %d, want 1", rows[0].Awaits)
	}
	if !rows[0].FallsThrough {
		t.Errorf("<module> should always fall through: the file ends")
	}
}

func TestEffectSummariesAreDeterministic(t *testing.T) {
	src := `async def a():
    return 1
def b():
    return 2
async def c():
    return await a()
`
	first := effectSummaryNames(EffectSummaries(parseOrFatal(t, src)))
	second := effectSummaryNames(EffectSummaries(parseOrFatal(t, src)))
	if strings.Join(first, "|") != strings.Join(second, "|") {
		t.Fatalf("effect rows are not deterministic: %v vs %v", first, second)
	}
	want := "<module>|a|b|c"
	if got := strings.Join(first, "|"); got != want {
		t.Errorf("rows = %q, want %q", got, want)
	}
}

func effectSummaryNames(rows []EffectSummary) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Function)
	}
	return out
}

func TestEffectsJSONShape(t *testing.T) {
	prog := parseOrFatal(t, "async def f():\n    await g()\nasync def g():\n    return 1\n")
	doc, err := EffectsJSON(prog, "t.gy", nil)
	if err != nil {
		t.Fatalf("EffectsJSON: %v", err)
	}
	for _, want := range []string{
		`"schema_version": "1.0"`, `"source": "t.gy"`, `"function": "f"`,
		`"async": true`, `"effects": [`, `"await"`, `"terminates"`, `"falls_through"`,
		`"coroutine_calls"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("effects JSON lost %q:\n%s", want, doc)
		}
	}
}

// TestEffectsTableNamesEveryFunction keeps the human view honest about the same
// facts the JSON carries — the two views come from one table on purpose.
func TestEffectsTableNamesEveryFunction(t *testing.T) {
	prog := parseOrFatal(t, "async def f(x):\n    return x\ndef g():\n    print(1)\n")
	tbl := EffectTable(prog, "t.gy")
	for _, want := range []string{"effect signatures for t.gy", "<module>", "async def", "f", "g", "falls_through="} {
		if !strings.Contains(tbl, want) {
			t.Errorf("effect table lost %q:\n%s", want, tbl)
		}
	}
}

// TestEffectsNoFalsePositivesOnCorpus is the noise gate: the conformance programs
// that use async must stay silent, or every run of `gusty check` shouts.
func TestEffectsNoFalsePositivesOnCorpusAsync(t *testing.T) {
	for _, src := range []string{
		`async def f(x):
    return x + 1
async def g(x):
    return x * 2
v = await f(2)
w = await g(3)
print(v)
print(w)
`,
		`async def f(x):
    return x * 2
async for v in [f(1), f(2), f(3)]:
    print(v)
`,
		`async def f(x):
    return x + 1
a = f(10)
b = f(20)
print(await a)
print(await b)
`,
	} {
		for _, code := range []string{
			CodeCoroNeverAwaited, CodeCoroAwaitedTwice, CodeAwaitOutsideCoroutine,
			CodeAsyncStmtOutsideCoroutine, CodeAwaitNotCoroutine,
			CodeAsyncGeneratorUnsupported, CodeAsyncMissingReturn,
		} {
			if got := diagsWithCode(checkDiags(t, src), code); len(got) != 0 {
				t.Errorf("corpus-shaped program reported %s:\n%s\ngot %v", code, src, got)
			}
		}
	}
}

// TestEffectsNonAsyncProgramsAreUntouched: the pass must be inert for code that
// never mentions async — otherwise it is a new source of false errors on the
// 74-program corpus.
func TestEffectsNonAsyncProgramsAreUntouched(t *testing.T) {
	src := `class Counter:
    def __init__(self, n):
        self.n = n
    def bump(self):
        self.n = self.n + 1
        return self.n

def total(xs):
    t = 0
    for x in xs:
        t += x
    return t

def classify(x):
    match x:
        case 1:
            return "one"
        case _:
            return "many"

def risky(x):
    try:
        if x > 0:
            return 1
    except ValueError:
        pass
    finally:
        print("done")
    return 0

def gens():
    for i in range(3):
        yield i

xs = [i * 2 for i in range(4)]
print(total(xs), classify(1), risky(3), Counter(1).bump())
`
	for _, code := range []string{
		CodeCoroNeverAwaited, CodeCoroAwaitedTwice, CodeAwaitOutsideCoroutine,
		CodeAsyncStmtOutsideCoroutine, CodeAwaitNotCoroutine,
		CodeAsyncGeneratorUnsupported, CodeAsyncMissingReturn,
	} {
		if got := diagsWithCode(checkDiags(t, src), code); len(got) != 0 {
			t.Errorf("non-async program reported %s: %v", code, got)
		}
	}
	// And the signature rows still exist for the plain code.
	prog := parseOrFatal(t, src)
	names := strings.Join(effectSummaryNames(EffectSummaries(prog)), "|")
	for _, want := range []string{"total", "classify", "risky", "Counter.bump", "gens"} {
		if !strings.Contains(names, want) {
			t.Errorf("no effect row for %q in %s", want, names)
		}
	}
}
