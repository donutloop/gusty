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
		"stdlib", "dispatch_nested", "dispatch_gc", "dispatch_gc_stress", "match_baren", "match_literal", "match_classpat", "round_ties", "wrapping_decorator", "dunder",
		"async_basic",
		"async_for",
		"async_multi",
		// A def is a binding the whole enclosing body can see: mutually recursive
		// functions, and helpers called from above their own def line — the shape the
		// checker used to refuse as `undefined name` (Gap R.6, ADR 0197).
		"forward_defs",
		// Functions named after symbols the host ABI already owns — `sync`, `write`,
		// `time`, `exit`, `main` — which used to be emitted under those very names and
		// answered by libc (roadmap Gap R.4, ADR 0198).
		"host_symbol_names",
		// Built-in call names are names, not keywords: `def str`, `def float`, `def len`
		// shadow the built-in on both backends, where the compiled path used to read those
		// calls through the built-in's meaning by name (roadmap Gap R.6, ADR 0199).
		"shadowed_builtins",
		// A module function and a method of one name are two definitions, not one key: the
		// method used to overwrite the module function in the checker's table and the module
		// call was then read against the method's `self`-inclusive arity (Gap R.8, ADR 0200).
		"method_function_name_clash",
		// Every legitimate way to pass fewer arguments than a definition lists: a trailing
		// default, all defaults, keyword arguments, and a call mixing both — the shapes the
		// new missing-argument rule must not refuse (Gap R.10, ADR 0201).
		"arity_defaults",
		// Built-in names are program-owned: a helper, a parameter, a keyword argument and a
		// method all called `print`/`range`/`shape`, which used not to parse at all because
		// those words were keywords (Gap R.9, ADR 0203).
		"builtin_names_as_defs",
		// A default marks a parameter that may be omitted and says nothing about its
		// neighbours: a defaulted parameter in the middle of a signature, bound both
		// positionally and by keyword (Gap R.11, ADR 0206). CPython cannot run it.
		"param_default_order",
		// `for x in <integer>` as a repeat count, which both backends implement and
		// CPython refuses: a declared feature, not an undiscovered divergence
		// (Gap R.14, ADR 0207).
		"for_int_count",
		// Iterating text in an unrolled loop: each element must be stored as its
		// @str_tab index, or the module references a global from an i32 slot and llc
		// rejects it (Gap R.15, ADR 0208).
		"for_string_chars",
		// Negative subscripts are one rule now (L11.4, ADR 0210), so the two programs
		// that used to sit in probes as debts run everywhere and match CPython: they
		// are ordinary parity cases, ledger-free because the oracle decides.
		"negative_index",
		"negative_literal_index",
		// Division by zero raises, on both backends (L11.x Gap R.18, ADR 0212): the
		// program below is ledger-free because the oracle, the interpreter and the
		// compiled binary print the same nine lines.
		"zero_division",
		// Gap R.36 + R.39 (ADR 0228): a local written on one path only is unbound on the other. All
		// three engines print `1` and then raise UnboundLocalError; the compiled leg used to print 0
		// for the second call and exit 0, and the interpreter used to call it the wrong class.
		// The compiled `try` dispatches every arm in order and hands an unmatched
		// exception outward (Gap R.20, ADR 0213); five shapes, three engines.
		"except_arm_order",
		"compound_scoping", // Gap R.24 — a compound statement binds in the enclosing scope (ADR 0217)
		// Gap R.29 (ADR 0221): `==` across int and float in both operand orders, and
		// container equality through the same rule; printed 1/0 so CPython runs this file.
		"numeric_equality",
		// Gap R.42 / L11.8 (ADR 0224): a string value is an @str_tab index, so comparing a
		// container element with a literal compiles instead of rejecting the module.
		"str_loop_eq",
		// The same comparison in a comprehension filter over a runtime list, whose loop header
		// used to name the wrong phi predecessor.
		"comp_str_filter",
		// Gap J.2 (ADR 0234): a set/dict comprehension bound to a variable, printed, measured,
		// subscripted and iterated — and the `if` filter that used to parse as a ternary.
		"comp_containers",
		// ADR 0224 in one file: comparison, `in`, f-strings, instance attributes and a method's
		// string result, all through the @str_tab index that replaced the literal's address.
		"string_values",
		// ADR 0225: what `s[1]` is -- a one-character string counted in code points, on both
		// backends, where both used to answer the byte.
		"string_subscript",
		"module_scope_in_functions", // Gap R.35 compiled half (ADR 0227)
		"module_calltime_lookup",    // Gap R.35 — a module lookup happens at call time (ADR 0220)
		"runtime_string_ops",        // Gap R.47 — a character asked about at run time (ADR 0229)
		"runtime_string_writes",     // Gap R.47 — building a string while the program runs (ADR 0230)
		// The promoted L11.5 probe: the same rule through a literal, a variable, and the two ends
		// of a non-ASCII string.
		"string_index",
		// The promoted code-point probe: len, indexing and iteration over non-ASCII text measured
		// the same way on both backends and in CPython (ADR 0225).
		"unicode_text",
		// ADR 0226: which question an operator asks. A number against a container is answered by
		// kind, not by coercing a container global through a float conversion.
		"kind_mismatch_equality",
		// Gap R.23 (ADR 0222): a deferred `finally` body runs on every exit from the
		// try -- fall-through, handled, propagating, and the transfers that leave it.
		"deferred_bodies",
		// Gap R.41 (ADR 0223): a method is a call like any other -- its own unwind target,
		// deferred bodies on every exit, and an exception that reaches its caller.
		"method_try",
		// A parameter is a local that starts out bound to an argument: an accumulator
		// that decrements its argument, a clamp that overwrites it, a loop that reuses
		// it as its variable (Gap R.3, ADR 0196).
		"param_rebind",
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
		// A float in a container slot (roadmap L11.1, ADR 0233): the element is the handle of a
		// float box and the comparison reads the doubles behind it, so [1] == [1.0] is True and
		// [1.5] == [1.6] is False on both backends. Both programs were oracle debt while the
		// literal emitter wrote a float's bits into a static i32 initializer (Gap R.40, ADR 0221).
		"probe_float_list_equal",
		"probe_float_container_equality",
		// The shape itself, three engines on one source: a float element, a float key read back,
		// a float appended to an integer list, a float written into a mixed list, and the tags
		// that let an unrolled loop print the element it was built from (ADR 0238).
		"float_container_elements",
		// The same shape one level down, three engines on one source: a list of lists prints,
		// compares by content, answers `in`, grows with an inner container, and loops over inner
		// containers — the tag routing the print and the comparison at run time (ADR 0238).
		"nested_data",
		// A container inside a container, read back out again: `xs[2][1]` and `m["k"]` reach through
		// a slot whose payload is the inner object's handle, and the tag the builder wrote is what
		// licenses the second read (roadmap L11.1, ADR 0241). It is here rather than in
		// conformanceProbes because both backends now print CPython's answer on every line.
		"probe_heterogeneous",
		// A container the program *built* rather than spelled out, read one level down: `xs.append([7,
		// 8])` leaves no literal behind, so the tag the object carries is the only thing that can say
		// whether the payload names a list, a dict or a text — and it is what the compiled subscript
		// branches on. It is here rather than in conformanceProbes because both backends now print
		// CPython's answer on every line (roadmap L11.1, ADR 0251).
		"probe_nested_list",
		// The ordering of those same slots, three engines on one source: `<`, `<=`, `>`, `>=` of a slot
		// whose kind only the object can report, answered as two numbers, two texts, or the `TypeError`
		// CPython raises naming the kind the slot really holds — with the arms nobody can reach not
		// emitted, and the cross-kind pairs caught rather than printed as a verdict (Gap R.93, ADR 0252).
		"slot_order_object",
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
		"probe_bool_value", // L11.2 — bools are not values yet
		"probe_tuple",      // L11.3 — no tuple lowering at all

		"probe_mixed_return_value",    // Gap R.22 — returns of differing types share one lowering
		"probe_builtin_traps_untyped", // Gap R.25 — a trap with no class cannot be caught
		"sequence_ops",
		"probe_operand_types",    // Gap R.26 — an operator applied to the wrong operands
		"probe_percent_format",   // Gap R.31 — no `%` string formatting; both legs refuse
		"probe_global_statement", // Gap R.48 — no `global` statement; all three engines differ
		"unwritten_slot_trap",    // Gap R.36 + R.39 — an unwritten local traps with the right class (ADR 0228)
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
		"probe_comp_runtime_reduce", // L11.7 — sum/min/max over a runtime comprehension
		"probe_comp_folded_iter",    // L11.2 — iterating a list the compiler folded away

		// Pinned by the rebound-parameter work (ADR 0196): the scalar half is fixed,
		// and this is the float half the tagged value word still owes.
		"probe_float_param_rebind", // L11.6 — a parameter rebound to a float, returned bare

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
	// ---- three-engine parity added this round -----------------------------------
	// (rows below are `oracle: match` by default; see the drift tests)

	// ---- gusty-only surface: CPython cannot run the program at all --------------
	"programs/for_int_count": {oracle: lang.OracleNA,
		reason: "`for i in 4:` treats an integer as a repeat count; CPython raises TypeError ('int' object is not iterable), so the CPython leg stops at the first loop and never sees the rest of the file",
		ref:    "docs/language.md § Control flow (integer repeat count) and roadmap Gap R.14 (ADR 0207)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "0\n1\n2\n3\n0\n10\n20\n100\n101\n102\n10\ndone\n"}, {Backend: "aot", Stdout: "0\n1\n2\n3\n0\n10\n20\n100\n101\n102\n10\ndone\n"}}},
	"programs/param_default_order": {oracle: lang.OracleNA,
		reason: "def offset(base, step=10, bonus) is a SyntaxError in CPython (`parameter without a default follows parameter with a default`); here positional binding fills left to right and a keyword call names what it fills, so every parameter is reachable",
		ref:    "roadmap Gap R.11 (closed) and docs/language.md § Parameters and defaults (ADR 0206)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "6\n16\n6\n123\n923\n129\n9\n"}, {Backend: "aot", Stdout: "6\n16\n6\n123\n923\n129\n9\n"}}},
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
	// Class patterns (roadmap Gap B, ADR 0235) — the first time this shape was ever run as a whole
	// program. The compiled backend had matched instances carrying no such attribute, lost
	// `Alias = Point` inside a function body (exit 2), and loaded a variable that does not exist for
	// a call pattern.
	"programs/match_classpat": {oracle: lang.OracleNA,
		reason: "a positional class sub-pattern needs __match_args__, which this language does not have — a class pattern binds attributes by the capture name, so CPython stops at the first case with `TypeError: Point() accepts 0 positional sub-patterns`",
		ref:    "docs/language.md § Class patterns (ADR 0235)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "attrs 2 3\nalias 2 3\nmissing none\nkind none\nsubclass 4 5\ncall pattern\nor pattern\nfn alias 6 7\ndynamic 8\nnot a class none\nearly n 1\nlate z 99\n"}, {Backend: "aot", Stdout: "attrs 2 3\nalias 2 3\nmissing none\nkind none\nsubclass 4 5\ncall pattern\nor pattern\nfn alias 6 7\ndynamic 8\nnot a class none\nearly n 1\nlate z 99\n"}}},
	"merged/data": {oracle: lang.OracleNA,
		reason: "inherits data_b's positional set subscript, which CPython rejects",
		ref:    "docs/language.md § Dicts & sets (positional set subscript is a gusty extension)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "dsum 15\ndmin 1\ndmax 5\ndlen 3\ndidx 8\ndkeys 3\ndval 20\nslen 3\nsidx 6\nodd 25\nfsum 21\nloop 45\nbig 66\nabs 42\nconv 100 2.0 7\n"}, {Backend: "aot", Stdout: "dsum 15\ndmin 1\ndmax 5\ndlen 3\ndidx 8\ndkeys 3\ndval 20\nslen 3\nsidx 6\nodd 25\nfsum 21\nloop 45\nbig 66\nabs 42\nconv 100 2.0 7\n"}}},
	"merged/features": {oracle: lang.OracleNA,
		reason: "inherits features_b's positional set subscript, which CPython rejects; features_a's \"abc\"[1] == 98 divergence is pinned on its own row",
		ref:    "roadmap L11.5 (code-point strings)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "sum 10\narith 3 1 5.0 6 -1 5\nfloat 3.5\nstr abc\nslen 5\nsidx b\nsup ABC\nslow abc\nfunc 5\nkw 5\nlambda 49\nlen 3\nidx 2\nsum 6\nminmax 1 3\ndict 2 10\nset 3 2\nwhile 18\nif many\nstep 20\nabs 5\nconv 42 1.0 42\n"}, {Backend: "aot", Stdout: "sum 10\narith 3 1 5.0 6 -1 5\nfloat 3.5\nstr abc\nslen 5\nsidx b\nsup ABC\nslow abc\nfunc 5\nkw 5\nlambda 49\nlen 3\nidx 2\nsum 6\nminmax 1 3\ndict 2 10\nset 3 2\nwhile 18\nif many\nstep 20\nabs 5\nconv 42 1.0 42\n"}}},

	// ---- measured divergences: valid CPython programs that print something else --
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
		reason: "comparisons print 1/0 where CPython prints True/False (roadmap L11.2). len(\"café\") is no longer part of this row: ADR 0225 made the string unit the code point on both backends, and the program now prints 4 as CPython does",
		ref:    "roadmap L11.2 (bools as values); the L11.5 half of this row closed with ADR 0225",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "tab\there\nquoted \"inside\"\nback\\slash\nbell\x07end\nhex AB\nunicode é 😀\nunknown \\q stays\nraw \\t stays literal\ntriple\nnewline\ntriple escape:\nhere\ncafé\ncafé!café\n1\n1\nCAFÉ\n4\n['naïve', '日本語', '🐍']\nnaïve\n日本語\n🐍\n{'key': 'value é'}\nvalue é\na\tb, c\nf-string 7 ✓\n"}, {Backend: "aot", Stdout: "tab\there\nquoted \"inside\"\nback\\slash\nbell\x07end\nhex AB\nunicode é 😀\nunknown \\q stays\nraw \\t stays literal\ntriple\nnewline\ntriple escape:\nhere\ncafé\ncafé!café\n1\n1\nCAFÉ\n4\n['naïve', '日本語', '🐍']\nnaïve\n日本語\n🐍\n{'key': 'value é'}\nvalue é\na\tb, c\nf-string 7 ✓\n"}}},
	"programs/string_params": {oracle: lang.OracleDebt,
		reason: "string equality predicates print 1/0 where CPython prints True/False",
		ref:    "roadmap L11.2 (str/repr are one function per backend, closes Gap L.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "hello ada\nhi\nhello keyword\nyo\n4\n0\n1\n0\n1\n0\n['one', 'two']\ntwo\n{'k': 1, 'j': 2}\n2\n{'q', 'r'}\n2\none\ntwo\nforwarded\nforwarded\n"}, {Backend: "aot", Stdout: "hello ada\nhi\nhello keyword\nyo\n4\n0\n1\n0\n1\n0\n['one', 'two']\ntwo\n{'k': 1, 'j': 2}\n2\n{'q', 'r'}\n2\none\ntwo\nforwarded\nforwarded\n"}}},

	// ---- Phase 11 probes: measured, owned, and not yet paid ----------------------
	"programs/probe_bool_value": {oracle: lang.OracleDebt,
		reason: "True/False print as 1/0 and comparisons print 1/0: there is no bool tag to render from, so --json also reports \"int\" for True",
		ref:    "roadmap L11.2 (bools as values, closes Gap L.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "1\n1\n0\n0\n1\n"}, {Backend: "aot", Stdout: "1\n1\n0\n0\n1\n"}}},
	"programs/probe_tuple": {oracle: lang.OracleDebt,
		reason: "a tuple literal has no AOT lowering at all (unsupported expression *lang.Tuple) and the interpreter renders one as a list",
		ref:    "roadmap L11.3 (tuples are values)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[1, 2, 3]\n2\n3\n4\n5\n[1, 2]\n"}, {Backend: "aot", Missing: true}}},

	"programs/sequence_ops": {oracle: lang.OracleDebt,
		reason: "the interpreter and CPython agree on all thirteen lines, but the compiled backend refuses `str * int` outright (an honest refusal) and emits a module llc rejects for list concatenation and repeat — \"global variable reference must have pointer type\" — so the compiled leg never completes",
		ref:    "roadmap Gap R.33 (sequence operations in codegen, same signature as Gap R.16); ADR 0215",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[1, 2]\n[1, 2, 3]\n[1, 1, 1]\n[1, 1, 1]\nabab\nabab\n\n\n[]\nstr ordered\nlist ordered\n6\n"}, {Backend: "aot", Missing: true}}},
	// Gap R.36 + R.39 closed (ADR 0228): the parity assertion for this program is that both backends
	// print `1` and then raise the class CPython raises. The oracle leg itself exits 1 (an uncaught
	// raise), which is why this is not_applicable rather than match -- the CPython leg cannot
	// complete, and the two legs are pinned instead.
	// A conformant row carries no reason and no ref: what the program proves is recorded in the
	// program's own header, in roadmap Gap R.47, and in ADR 0229.
	"programs/runtime_string_ops":    {oracle: lang.OracleMatch},
	"programs/runtime_string_writes": {oracle: lang.OracleMatch},
	"programs/unwritten_slot_trap": {oracle: lang.OracleNA,
		reason: "a local assigned only inside `if c:` with no else: `f(False)` never binds it. CPython raises UnboundLocalError; both gusty backends now print the same first line and raise the same class, which they did not before ADR 0228 -- the compiled leg printed 0 for the unbound call and exited 0, and the interpreter called it a NameError",
		ref:    "roadmap Gap R.36 (definite assignment) + Gap R.39 (which class an unwritten local raises); ADR 0228",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "1\n", Missing: true, Err: "cannot access local variable 'x'"}, {Backend: "aot", Stdout: "1\n", Missing: true, Err: "exit status 3"}}},

	// Gap R.48 (found while closing R.36, ADR 0228's probe pass).
	"programs/probe_global_statement": {oracle: lang.OracleNA,
		reason: "there is no `global` statement: CPython reads `global gz` as a declaration and the read afterwards raises NameError, the interpreter parses it as the *expression* `global gz` and reports `name 'global' is not defined`, and the compiled backend refuses the program outright. The two backends disagree with CPython and with each other, on a construct every Python reader expects",
		ref:    "roadmap Gap R.48 (`global` is not in the language); Gap R.36 (the probe pass that found it)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Missing: true, Err: `undefined name "global"`}, {Backend: "aot", Missing: true}}},

	"programs/probe_percent_format": {oracle: lang.OracleDebt,
		reason: "no `%` string formatting exists yet: the interpreter raises the operand TypeError (catchably, in all three shapes) where CPython formats, and the compiled backend refuses to lower `str % x` at all, so the compiled leg never runs",
		ref:    "roadmap Gap R.31; ADR 0215 (the operand gate that turned the old wrong answer into this refusal)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "str % int: TypeError\nstr % tuple: TypeError\nstr % str: TypeError\n1 1\n"}, {Backend: "aot", Missing: true}}},

	"programs/probe_operand_types": {oracle: lang.OracleDebt,
		reason: "the interpreter and CPython agree on all seven handler lines, but the compiled backend answers `1 + None` with a value instead of raising and refuses the rest at compile time, so the compiled leg never completes",
		ref:    "roadmap Gap R.27 (operand kinds unchecked in the compiled backend); ADR 0215",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "mul-str ok\nsub ok\ndiv ok\nnone ok\nconcat ok\norder ok\ndone\n"}, {Backend: "aot", Missing: true}}},

	"programs/probe_builtin_traps_untyped": {oracle: lang.OracleDebt,
		reason: "the interpreter and CPython agree on all five handler lines, but the compiled backend answers the missing-attribute case with a value instead of raising (Gap R.19) and refuses the others at compile time with prose diagnostics, so the compiled leg never completes",
		ref:    "roadmap Gap R.25 (typed built-in traps) and Gap R.19 (attribute answers 0 in AOT); ADR 0214",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "attr ok\nvalue ok\nunpack ok\ncall ok\nlen ok\n"}, {Backend: "aot", Missing: true}}},
	"programs/probe_mixed_return_value": {oracle: lang.OracleDebt,
		reason: "a function whose return paths have different types is lowered as returning one of them, so the compiled caller reads the integer as an interned-string index and prints (null) where the interpreter and CPython print 3 — silently, with exit 0",
		ref:    "roadmap Gap R.22 (mixed return types; ADR 0213 refiled the original Gap R.21 reading, which blamed try/except)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "3\n"}, {Backend: "aot", Stdout: "(null)\n"}}},
	"programs/probe_math_const": {oracle: lang.OracleNA,
		reason: "Python spells these math.pi / math.e, so the source is not a CPython program; what the row pins is that the on-disk data-only fold loses the float type",
		ref:    "roadmap L11.6 (a stdlib constant keeps its type)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "3.141592653589793\n2.718281828459045\n"}, {Backend: "aot", Stdout: "3\n2\n"}}},
	"programs/probe_float_numeric": {oracle: lang.OracleDebt,
		reason: "the floor rules are right on both backends now (-7 // 2 is -4, -3.5 % 2.0 is 0.5 — Gaps R.28 and R.30, ADR 0216); what still diverges is float *state*: x /= 2 leaves an int in the compiled backend (prints 4 where CPython prints 4.0) and a float through an untyped parameter arrives as 0",
		ref:    "roadmap L11.6 (numeric truth in the compiled backend, closes Gaps P.1 + P.2)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "-4\n0.5\n4.0\n0.2\n"}, {Backend: "aot", Stdout: "-4\n0.5\n4\n0\n"}}},
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
	"programs/probe_float_param_rebind": {oracle: lang.OracleDebt,
		reason: "a function's argument type is read from the shape of its return expression, so `x = x + 1.5; return x` is emitted as an int function: interpreted and in CPython 1.0 becomes 2.5, compiled the module is rejected",
		ref:    "roadmap L11.6 (floats are half-implemented) — found closing Gap R.3",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "2.5\n3.0\n"}, {Backend: "aot", Missing: true}}},
	"programs/probe_print_atomic": {oracle: lang.OracleDebt,
		reason: "print writes as it evaluates: a call that itself prints lands inside the caller's line instead of before it",
		ref:    "roadmap Gap L.5 (print is atomic), found by the L11.9 oracle leg",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "got << 21 >>\n42\nafter\n"}, {Backend: "aot", Stdout: "got << 21 >>\n42\nafter\n"}}},
}
