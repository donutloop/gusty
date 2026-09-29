package integration

import (
	"os"
	"path/filepath"

	"github.com/donutloop/gusty/pkg/lang"
)

// readProgramSrc returns the contents of integration/programs/<name>.gy.
func readProgramSrc(name string) string {
	b, err := os.ReadFile(filepath.Join("programs", name+".gy"))
	if err != nil {
		panic(err)
	}
	return string(b)
}

// mergePrograms concatenates the named program fragments into one merged source,
// mirroring how the multi-file build tests feed the AOT compiler.
func mergePrograms(names ...string) string {
	var sb []byte
	for i, n := range names {
		if i > 0 {
			sb = append(sb, '\n')
		}
		b, err := os.ReadFile(filepath.Join("programs", n+".gy"))
		if err != nil {
			panic(err)
		}
		sb = append(sb, b...)
	}
	return string(sb)
}

// conformanceStandalone returns the single-file whole-program conformance cases:
// every programs/*.gy that runs as a standalone source (fragments used only by
// multi-file build tests are excluded — they appear in conformanceMerged; probe
// programs — recorded divergences — appear in conformanceProbes).
func conformanceStandalone() []lang.ConformanceCase {
	names := []string{
		"single", "sq", "print1", "ir", "fstr", "floatfn",
		"ctrl_a", "ctrl_b", "ctrl_c",
		"data_a", "data_b", "data_c",
		"features_a", "features_b",
		"stdlib", "dispatch_nested", "dispatch_gc", "dispatch_gc_stress", "match_baren", "match_literal", "wrapping_decorator", "dunder",
		"async_basic",
		"async_for",
		"async_multi",
		// The await/return discipline the L7.6 checker proves: deferred coroutines
		// held in variables and awaited later, coroutines handed across a call and
		// awaited inside it, an async def per control-flow shape, and `async for`
		// over coroutines (roadmap Phase 7, ADR 0195).
		"async_effects",
		"typealias",
		"variance",
		"heap_containers",
		"folded_lists",
		// Element-level reads and writes of a mixed list: the (value, tag) pair travels with the
		// element to the use site (roadmap L11.1, ADR 0187).
		"mixed_element_reads",
		"mixed_element_writes",
		// Container printing: a literal or a constructor in print position renders as a
		// container instead of as its handle / its static global (Gap J.6, Gap K.3, ADR 0188).
		"empty_containers",
		"empty_set",
		// Containers compare by value on both backends, and the row is checked against
		// CPython like every other parity case (roadmap L11.1, ADR 0189).
		"container_equality",
		"print_args",
		"truthiness",
		"subscript_assign",
		"container_methods",
		"none_values",
		"string_containers",
		"string_escapes",
		"string_params",
		// The precise-root repro: a frame local that must survive a nested allocation
		// storm, a statement-position callee whose loop reclaims as it goes, and
		// thousands of short-lived containers (ADR 0181).
		"gc_precise",
	}

	cases := make([]lang.ConformanceCase, 0, len(names))
	for _, n := range names {
		cases = append(cases, lang.ConformanceCase{
			ID:     "programs/" + n,
			Name:   n + ".gy",
			Source: readProgramSrc(n),
			Shared: true,
		})
	}
	return cases
}

// conformanceMerged returns the multi-file whole-program conformance cases.
// Each merges several programs/*.gy fragments into one source, exactly as the
// build tests do before compiling.
func conformanceMerged() []lang.ConformanceCase {
	type merged struct {
		id, name string
		files    []string
	}
	groups := []merged{
		{"merged/ctrl", "ctrl_a+b+c", []string{"ctrl_a", "ctrl_b", "ctrl_c"}},
		{"merged/data", "data_a+b+c", []string{"data_a", "data_b", "data_c"}},
		{"merged/features", "features_a+b", []string{"features_a", "features_b"}},
		{"merged/whole", "whole_a+b", []string{"whole_a", "whole_b"}},
		{"merged/math", "math_lib+calc+main", []string{"math_lib", "math_calc", "math_main"}},
	}
	cases := make([]lang.ConformanceCase, 0, len(groups))
	for _, g := range groups {
		cases = append(cases, lang.ConformanceCase{
			ID:     g.id,
			Name:   g.name,
			Source: mergePrograms(g.files...),
			Shared: true,
		})
	}
	return cases
}

// conformanceProbes returns the recorded-divergence rows: programs that reproduce a
// roadmap Phase 11 defect (L11.1…L11.7) on the current compiler. They are *not*
// asserted for parity — a probe often fails on one leg by design, and one of them
// panics the compiler — but every one of them is compared to CPython and pinned, so
// "still broken" is a measured fact rather than a roadmap claim, and closing one
// breaks the row with "debt is paid" instead of passing unnoticed (ADR 0186).
//
// When a probe's debt is paid: delete the ledger row and move the program from
// conformanceProbes to conformanceStandalone, so it becomes parity surface.
func conformanceProbes() []lang.ConformanceCase {
	names := []string{
		"probe_bool_value",       // L11.2 — bools are not values yet
		"probe_nested_list",      // L11.1 — containers cannot nest in compiled memory
		"probe_heterogeneous",    // L11.1 — one element kind per compiled container
		"probe_tuple",            // L11.3 — no tuple lowering at all
		"probe_negative_index",   // L11.4 — negative indexing traps
		"probe_negative_literal", // L11.4 + L11.8 — a literal [-1] panics the compiler
		"probe_unicode",          // L11.5 — strings are bytes, not code points
		"probe_string_index",     // L11.5 — s[i] is a byte value, not a character
		"probe_math_const",       // L11.6 — a stdlib float constant folds to int
		"probe_float_numeric",    // L11.6 — //, /=, float % and float params
		"probe_enumerate",        // L11.7 + L11.3 — enumerate/zip/reversed yield tuples
		"probe_fn_value",         // L11.7 — a lambda cannot be called through a parameter
		"probe_fn_name",          // L11.7 — a def'd name is not a value at all
		"probe_print_atomic",     // Gap L.5 — print writes while it evaluates
		// Found by the boring-program sweep (ADR 0190): the tutorial-shaped programs nobody
		// probed, twelve of them, five divergences.

		// Pinned by the runtime-comprehension work (ADR 0192): two honest refusals and one
		// llc rejection that is a compiler bug, all recorded rather than remembered.
		"probe_str_loop_eq",         // L11.8 — comparing an interned element with a string rejects the module
		"probe_comp_str_filter",     // L11.8 — the same bug, reached from a comprehension filter
		"probe_comp_runtime_reduce", // L11.7 — sum/min/max over a runtime comprehension
		"probe_comp_folded_iter",    // L11.2 — iterating a list the compiler folded away

		// Pinned by the await/return discipline (ADR 0195): the checker now refuses the
		// dishonest async programs, so what is left is the honest one that still disagrees.
		"probe_async_eager", // L7.1 — the compiled backend runs a coroutine at the call
	}
	cases := make([]lang.ConformanceCase, 0, len(names))
	for _, n := range names {
		cases = append(cases, lang.ConformanceCase{
			ID:     "programs/" + n,
			Name:   n + ".gy",
			Source: readProgramSrc(n),
			Shared: false, // a probe is a recorded divergence, not parity surface
		})
	}
	return cases
}

// conformanceCases returns the canonical conformance matrix registry: every
// whole-program integration case — single-file, merged multi-file, and probe —
// with its CPython-oracle state declared from the ledger below.
func conformanceCases() []lang.ConformanceCase {
	cases := append(conformanceStandalone(), conformanceMerged()...)
	cases = append(cases, conformanceProbes()...)
	for i := range cases {
		if d, ok := oracleLedger[cases[i].ID]; ok {
			cases[i].Oracle = d.oracle
			cases[i].Reason = d.reason
			cases[i].Ref = d.ref
			cases[i].Rules = d.rules
			cases[i].Pins = d.pins
		} else {
			// The corpus is the spec: a case with no ledger row is *expected* to
			// behave exactly like CPython. Anything else has to be written down,
			// with a reason and an owner, before it can be green.
			cases[i].Oracle = lang.OracleMatch
		}
	}
	return cases
}

// oracleLedger is the registry's declaration of what each case does against
// CPython. Only the exceptions are listed: a row's absence means "this program must
// print what Python prints", and the harness fails if it does not.
//
// A debt row carries three things a green build cannot fake:
//
//   - reason: what is wrong, in one sentence;
//   - ref:    the roadmap item that owns the fix; and
//   - pins:   what each leg prints (or how it fails) today.
//
// The pins are the ratchet. A change that moves a pinned output without settling the
// debt fails the row, and a change that settles it fails the row too — with "debt is
// paid" — until the row is deleted or rewritten. `tools/oracleprobe` prints the
// observed legs when a row needs writing or refreshing.
type oracleDecl struct {
	oracle string
	reason string
	ref    string
	rules  []string
	pins   []lang.OraclePin
}

var oracleLedger = map[string]oracleDecl{
	// ---- gusty-only surface: CPython cannot run the program at all --------------
	"programs/data_b": {oracle: lang.OracleNA,
		reason: "{5, 6, 7}[6] — subscripting a set by position is gusty surface; CPython raises TypeError ('set' object is not subscriptable)",
		ref:    "docs/language.md § Dicts & sets (positional set subscript is a gusty extension)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "dkeys 3\ndval 20\nslen 3\nsidx 6\nodd 25\n"}, {Backend: "aot", Stdout: "dkeys 3\ndval 20\nslen 3\nsidx 6\nodd 25\n"}}},
	"programs/features_b": {oracle: lang.OracleNA,
		reason: "{1, 2, 3}[2] — positional set subscript again; the CPython leg stops there and never reaches the rest of the program",
		ref:    "docs/language.md § Dicts & sets (positional set subscript is a gusty extension)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "func 5\nkw 5\nlambda 49\nlen 3\nidx 2\nsum 6\nminmax 1 3\ndict 2 10\nset 3 2\nwhile 18\nif many\nstep 20\nabs 5\nconv 42 1.0 42\n"}, {Backend: "aot", Stdout: "func 5\nkw 5\nlambda 49\nlen 3\nidx 2\nsum 6\nminmax 1 3\ndict 2 10\nset 3 2\nwhile 18\nif many\nstep 20\nabs 5\nconv 42 1.0 42\n"}}},
	"programs/stdlib": {oracle: lang.OracleNA,
		reason: "string.DIGITS / string.LOWERCASE are this language's stdlib names; Python's string module has no such attributes",
		ref:    "roadmap Phase 2 § on-disk stdlib modules",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "0123456789\nabcdefghijklmnopqrstuvwxyz\n"}, {Backend: "aot", Stdout: "0123456789\nabcdefghijklmnopqrstuvwxyz\n"}}},
	"programs/async_basic": {oracle: lang.OracleNA,
		reason: "await at module scope is a SyntaxError in CPython; the minimal synchronous-coroutine model accepts it (L5.6)",
		ref:    "docs/shared-lowering-spec.md § async / await",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "3\n6\n"}, {Backend: "aot", Stdout: "3\n6\n"}}},
	"programs/async_for": {oracle: lang.OracleNA,
		reason: "async for outside an async function is a SyntaxError in CPython; lowered as a plain for loop here (L5.6)",
		ref:    "docs/shared-lowering-spec.md § async / await",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "2\n4\n6\n"}, {Backend: "aot", Stdout: "2\n4\n6\n"}}},
	"programs/async_multi": {oracle: lang.OracleNA,
		reason: "await at module scope is a SyntaxError in CPython (L5.6)",
		ref:    "docs/shared-lowering-spec.md § async / await",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "11\n21\n"}, {Backend: "aot", Stdout: "11\n21\n"}}},
	"programs/async_effects": {oracle: lang.OracleNA,
		reason: "await at module scope is a SyntaxError in CPython; this is the L7.6 legal-async surface the checker must accept, so what is pinned is that both backends agree and nothing was reported",
		ref:    "docs/language.md § Async (the await/return discipline)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "24\n18\n10\n42\n15\n6\n20\n3\n2\n4\n"}, {Backend: "aot", Stdout: "24\n18\n10\n42\n15\n6\n20\n3\n2\n4\n"}}},
	"programs/match_literal": {oracle: lang.OracleNA,
		reason: "Literal[1, 2] is this language's checker surface; plain CPython has no Literal in scope and stops at the annotation",
		ref:    "roadmap Phase 2 § gradual typing; docs/operations.md § gusty check",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "one\ntwo\n"}, {Backend: "aot", Stdout: "one\ntwo\n"}}},
	"merged/data": {oracle: lang.OracleNA,
		reason: "inherits data_b's positional set subscript, which CPython rejects",
		ref:    "docs/language.md § Dicts & sets (positional set subscript is a gusty extension)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "dsum 15\ndmin 1\ndmax 5\ndlen 3\ndidx 8\ndkeys 3\ndval 20\nslen 3\nsidx 6\nodd 25\nfsum 21\nloop 45\nbig 66\nabs 42\nconv 100 2.0 7\n"}, {Backend: "aot", Stdout: "dsum 15\ndmin 1\ndmax 5\ndlen 3\ndidx 8\ndkeys 3\ndval 20\nslen 3\nsidx 6\nodd 25\nfsum 21\nloop 45\nbig 66\nabs 42\nconv 100 2.0 7\n"}}},
	"merged/features": {oracle: lang.OracleNA,
		reason: "inherits features_b's positional set subscript, which CPython rejects; features_a's \"abc\"[1] == 98 divergence is pinned on its own row",
		ref:    "roadmap L11.5 (code-point strings)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "sum 10\narith 3 1 5.0 6 -1 5\nfloat 3.5\nstr abc\nslen 5\nsidx 98\nsup ABC\nslow abc\nfunc 5\nkw 5\nlambda 49\nlen 3\nidx 2\nsum 6\nminmax 1 3\ndict 2 10\nset 3 2\nwhile 18\nif many\nstep 20\nabs 5\nconv 42 1.0 42\n"}, {Backend: "aot", Stdout: "sum 10\narith 3 1 5.0 6 -1 5\nfloat 3.5\nstr abc\nslen 5\nsidx 98\nsup ABC\nslow abc\nfunc 5\nkw 5\nlambda 49\nlen 3\nidx 2\nsum 6\nminmax 1 3\ndict 2 10\nset 3 2\nwhile 18\nif many\nstep 20\nabs 5\nconv 42 1.0 42\n"}}},

	// ---- measured divergences: valid CPython programs that print something else --
	"programs/features_a": {oracle: lang.OracleDebt,
		reason: "\"abc\"[1] prints the byte 98; CPython prints the one-character string 'b'",
		ref:    "roadmap L11.5 (code-point strings, closes Gap N.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "sum 10\narith 3 1 5.0 6 -1 5\nfloat 3.5\nstr abc\nslen 5\nsidx 98\nsup ABC\nslow abc\n"}, {Backend: "aot", Stdout: "sum 10\narith 3 1 5.0 6 -1 5\nfloat 3.5\nstr abc\nslen 5\nsidx 98\nsup ABC\nslow abc\n"}}},
	"programs/print_args": {oracle: lang.OracleDebt,
		reason: "print writes each argument as it evaluates it, so a call that itself prints interleaves into the caller's line; Python evaluates every argument, then writes one line",
		ref:    "roadmap Gap L.5 (print is atomic), found by the L11.9 oracle leg",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "n = 42\na 1 b 2\n\ncsv, 1, 2, 3\ntick!tock\nxs = [1, 2, 3]\nm = {1: 2} len 3\ngot << 21 >>\n42\n"}, {Backend: "aot", Stdout: "n = 42\na 1 b 2\n\ncsv, 1, 2, 3\ntick!tock\nxs = [1, 2, 3]\nm = {1: 2} len 3\ngot << 21 >>\n42\n"}}},
	"programs/container_methods": {oracle: lang.OracleDebt,
		reason: "print(2 in s) is 1, not True — bools are still the integer 1 with no tag to render from",
		ref:    "roadmap L11.2 (str/repr are one function per backend, closes Gap L.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "3\n[1, 2]\n1\n[2]\n15\n0\nset()\n2\n1\n10\n20\n1\n9\n2\n[7, 8]\n8\ncaught pop\ncaught empty pop\n"}, {Backend: "aot", Stdout: "3\n[1, 2]\n1\n[2]\n15\n0\nset()\n2\n1\n10\n20\n1\n9\n2\n[7, 8]\n8\ncaught pop\ncaught empty pop\n"}}},
	"programs/none_values": {oracle: lang.OracleDebt,
		reason: "comparisons (emit() == None, 0 == None, None == None) print 1/0 where CPython prints True/False/True",
		ref:    "roadmap L11.2 (str/repr are one function per backend, closes Gap L.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "None\nNone\nside\nNone\nside\n1\n0\n1\nfalsy\n2\nonce\n1\n"}, {Backend: "aot", Stdout: "None\nNone\nside\nNone\nside\n1\n0\n1\nfalsy\n2\nonce\n1\n"}}},
	"programs/string_containers": {oracle: lang.OracleDebt,
		reason: "membership tests (\"ada\" in names, \"zed\" in names) print 1/0 where CPython prints True/False",
		ref:    "roadmap L11.2 (str/repr are one function per backend, closes Gap L.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "['ada', 'brin', 'cad']\n3\nada\ncad\n1\n0\n['x', 'y']\n['x', 'kept']\nada\nbrin\ncad\n{'ada': 3, 'brin': 5}\n2\n5\n{1: 'one'}\n{'k': 'v'}\n{'q', 'r'}\n2\nq\nr\nset()\n['1', '2']\n[\"it's\", 'plain']\n"}, {Backend: "aot", Stdout: "['ada', 'brin', 'cad']\n3\nada\ncad\n1\n0\n['x', 'y']\n['x', 'kept']\nada\nbrin\ncad\n{'ada': 3, 'brin': 5}\n2\n5\n{1: 'one'}\n{'k': 'v'}\n{'q', 'r'}\n2\nq\nr\nset()\n['1', '2']\n[\"it's\", 'plain']\n"}}},
	"programs/string_escapes": {oracle: lang.OracleDebt,
		reason: "two rules fire: comparisons print 1/0 instead of True/False, and len(\"café\") is 5 because len counts bytes",
		ref:    "roadmap L11.2 (bools as values) + L11.5 (code-point strings)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "tab\there\nquoted \"inside\"\nback\\slash\nbell\x07end\nhex AB\nunicode é 😀\nunknown \\q stays\nraw \\t stays literal\ntriple\nnewline\ntriple escape:\nhere\ncafé\ncafé!café\n1\n1\nCAFÉ\n5\n['naïve', '日本語', '🐍']\nnaïve\n日本語\n🐍\n{'key': 'value é'}\nvalue é\na\tb, c\nf-string 7 ✓\n"}, {Backend: "aot", Stdout: "tab\there\nquoted \"inside\"\nback\\slash\nbell\x07end\nhex AB\nunicode é 😀\nunknown \\q stays\nraw \\t stays literal\ntriple\nnewline\ntriple escape:\nhere\ncafé\ncafé!café\n1\n1\nCAFÉ\n5\n['naïve', '日本語', '🐍']\nnaïve\n日本語\n🐍\n{'key': 'value é'}\nvalue é\na\tb, c\nf-string 7 ✓\n"}}},
	"programs/string_params": {oracle: lang.OracleDebt,
		reason: "string equality predicates print 1/0 where CPython prints True/False",
		ref:    "roadmap L11.2 (str/repr are one function per backend, closes Gap L.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "hello ada\nhi\nhello keyword\nyo\n4\n0\n1\n0\n1\n0\n['one', 'two']\ntwo\n{'k': 1, 'j': 2}\n2\n{'q', 'r'}\n2\none\ntwo\nforwarded\nforwarded\n"}, {Backend: "aot", Stdout: "hello ada\nhi\nhello keyword\nyo\n4\n0\n1\n0\n1\n0\n['one', 'two']\ntwo\n{'k': 1, 'j': 2}\n2\n{'q', 'r'}\n2\none\ntwo\nforwarded\nforwarded\n"}}},

	// ---- Phase 11 probes: measured, owned, and not yet paid ----------------------
	"programs/probe_bool_value": {oracle: lang.OracleDebt,
		reason: "True/False print as 1/0 and comparisons print 1/0: there is no bool tag to render from, so --json also reports \"int\" for True",
		ref:    "roadmap L11.2 (bools as values, closes Gap L.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "1\n1\n0\n0\n1\n"}, {Backend: "aot", Stdout: "1\n1\n0\n0\n1\n"}}},
	"programs/probe_nested_list": {oracle: lang.OracleDebt,
		reason: "the compiled backend cannot nest: m[0][1] refuses with `index requires an inline list/dict/set literal` and xs.append([1,2]) cannot be lowered",
		ref:    "roadmap L11.1 (tagged value word) remaining item (1a)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "2\n2\n1\n7\n"}, {Backend: "aot", Missing: true}}},
	"programs/probe_heterogeneous": {oracle: lang.OracleDebt,
		reason: "a container element that is itself a container is refused: a compiled list still decides one element kind at compile time",
		ref:    "roadmap L11.1 (tagged value word) remaining item (1b)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[1, 'a', [2, 3]]\n3\n3\n[1]\n2\n"}, {Backend: "aot", Missing: true}}},
	"programs/probe_tuple": {oracle: lang.OracleDebt,
		reason: "a tuple literal has no AOT lowering at all (unsupported expression *lang.Tuple) and the interpreter renders one as a list",
		ref:    "roadmap L11.3 (tuples are values)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[1, 2, 3]\n2\n3\n4\n5\n[1, 2]\n"}, {Backend: "aot", Missing: true}}},
	"programs/probe_negative_index": {oracle: lang.OracleDebt,
		reason: "xs[-1] traps on both backends where CPython answers 3; negative normalisation exists for slices only",
		ref:    "roadmap L11.4 (Python-shaped indexing)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Missing: true}, {Backend: "aot", Missing: true}}},
	"programs/probe_negative_literal": {oracle: lang.OracleDebt,
		reason: "a literal container indexed by a negative constant panics in the Go compiler instead of answering or refusing (an L11.8 violation)",
		ref:    "roadmap L11.4 + L11.8 (no tested shape may leave the compiler as a panic)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Missing: true}, {Backend: "aot", Missing: true, Err: "compiler panic"}}},
	"programs/probe_unicode": {oracle: lang.OracleDebt,
		reason: "len(\"café\") is 5 and \"héllo\"[1] is the byte 195; module-scope `for ch in \"aé\"` emits an invalid store of a string global",
		ref:    "roadmap L11.5 (code-point strings, closes Gap N.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "5\n195\na\né\n"}, {Backend: "aot", Missing: true}}},
	"programs/probe_string_index": {oracle: lang.OracleDebt,
		reason: "s[i] yields a byte value (98 for \"abc\"[1]) instead of a one-character string, and a non-literal string index is refused in AOT",
		ref:    "roadmap L11.5 (code-point strings, closes Gap N.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "98\n104\n108\n"}, {Backend: "aot", Missing: true}}},
	"programs/probe_math_const": {oracle: lang.OracleNA,
		reason: "Python spells these math.pi / math.e, so the source is not a CPython program; what the row pins is that the on-disk data-only fold loses the float type",
		ref:    "roadmap L11.6 (a stdlib constant keeps its type)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "3.141592653589793\n2.718281828459045\n"}, {Backend: "aot", Stdout: "3\n2\n"}}},
	"programs/probe_float_numeric": {oracle: lang.OracleDebt,
		reason: "-7 // 2 is -3 compiled, -3.5 % 2.0 is -1.5 on both backends, x /= 2 stays an int, and a float through an untyped parameter becomes 0",
		ref:    "roadmap L11.6 (numeric truth in the compiled backend, closes Gaps P.1 + P.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "-4\n-1.5\n4.0\n0.2\n"}, {Backend: "aot", Stdout: "-3\n-1.5\n4\n0\n"}}},
	"programs/probe_enumerate": {oracle: lang.OracleDebt,
		reason: "enumerate/zip produce pairs the interpreter renders as lists (tuples again), and list(<container>) copies are refused in AOT",
		ref:    "roadmap L11.7 + L11.3 (tuples are values)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[[0, 'a'], [1, 'b']]\n[[1, 3], [2, 4]]\n[3, 2, 1]\n"}, {Backend: "aot", Missing: true}}},
	"programs/probe_fn_value": {oracle: lang.OracleDebt,
		reason: "calling a function through a parameter is `unsupported call \"f\"` in AOT: no fnptr operand, no indirect call lowering",
		ref:    "roadmap L11.7 (functions are values that compile)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[3, 6]\n"}, {Backend: "aot", Missing: true}}},
	"programs/probe_fn_name": {oracle: lang.OracleDebt,
		reason: "a def'd function name is not a value on either backend: the interpreter reports `undefined name twice` where Python maps the function happily",
		ref:    "roadmap L11.7 (functions are values that compile)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Missing: true}, {Backend: "aot", Missing: true}}},
	"programs/probe_str_loop_eq": {oracle: lang.OracleDebt,
		reason: "comparing a container element with a string literal emits `icmp eq i32 %_n, @.str3` — an index into @str_tab against the address of a string global — and llc rejects the module, so this is exit 2 (a compiler bug) rather than a refusal",
		ref:    "roadmap L11.8 (refusal is part of the model) + Gap I.2 (interned strings)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "a\n"}, {Backend: "aot", Missing: true, Err: "global variable reference must have pointer type"}}},
	"programs/probe_comp_str_filter": {oracle: lang.OracleDebt,
		reason: "a comprehension filter that compares elements with a string reaches the same interned-comparison bug; the comprehension refuses instead of inheriting the llc rejection",
		ref:    "roadmap L11.8 (refusal is part of the model)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "['a']\n"}, {Backend: "aot", Missing: true, Err: "interned-string comparison"}}},
	"programs/probe_comp_runtime_reduce": {oracle: lang.OracleDebt,
		reason: "sum over a comprehension whose elements are computed at runtime has no compile-time element set to fold; it refuses rather than add up nothing and answer 0",
		ref:    "roadmap L11.7 (functions are values that compile)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "14\n"}, {Backend: "aot", Missing: true, Err: "runtime reduction"}}},
	"programs/probe_comp_folded_iter": {oracle: lang.OracleDebt,
		reason: "a comprehension cannot walk a list the escape analysis kept as a compile-time constant — there is no runtime object to index — while `for` over the same list and the interpreter both work",
		ref:    "roadmap L11.2 (the tagged value word makes every container a runtime object)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[2, 4, 6]\n"}, {Backend: "aot", Missing: true, Err: "compile-time constant"}}},
	"programs/probe_async_eager": {oracle: lang.OracleNA,
		reason: "the compiled backend lowers an async call as a call, so `work(1)` prints at the call and the interpreter prints at the await; CPython rejects the program outright (module-scope await)",
		ref:    "roadmap L7.6a (deferred coroutines in codegen)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "between\neffect 1\n2\n"}, {Backend: "aot", Stdout: "effect 1\nbetween\n2\n"}}},
	"programs/probe_print_atomic": {oracle: lang.OracleDebt,
		reason: "print writes as it evaluates: a call that itself prints lands inside the caller's line instead of before it",
		ref:    "roadmap Gap L.5 (print is atomic), found by the L11.9 oracle leg",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "got << 21 >>\n42\nafter\n"}, {Backend: "aot", Stdout: "got << 21 >>\n42\nafter\n"}}},
}
