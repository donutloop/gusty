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
//
// The ledger is the current claim: an `aot` pin is what the compiled leg prints, and a
// `python` pin what the pinned CPython prints. Its prose is history: a `reason` written before
// ADR 0302 sometimes measures three engines, because until that cycle the harness ran three legs
// (the AST interpreter among them). Those sentences record what was measured when the row was
// written — the same way a roadmap row's `Free text` cell keeps its pre-tabulation wording — and
// they are not a claim about what runs today. What runs is in the pins, and a pin for a leg the
// harness does not run fails the build (pkg/lang/conformance.go, `leg`).
func conformanceStandalone() []lang.ConformanceCase {
	names := []string{
		"single", "sq", "print1", "ir", "fstr", "floatfn",
		"ctrl_a", "ctrl_b", "ctrl_c",
		"data_a", "data_b", "data_c",
		// floor/ceil/sqrt answer whole numbers and a float respectively, on the compiled path (Gap R.51,
		// ADR 0264); the file is gusty's spelling of the reference's math-module names, so the ledger
		// records it and the CLI test runs the twin.
		"whole_number_builtins",
		// The number use of a slot whose kind only the run time can describe: a list of lists, and
		// arithmetic on what comes out of one (roadmap L11.1's last clause, ADR 0265).
		"numeric_slot_arith",
		// The sequence operations the compiled backend BUILDS: a list or tuple concatenated (one deep and
		// chained), a list, tuple or text repeated by a whole number in either operand order, zero and a
		// negative multiplier, and every road that reads the built object back — `print`, `str`/`repr`, an
		// f-string field, `len`, `in`, `==` and a `for`. The program that pinned "the compiled backend cannot
		// lower sequence operations" is this row's promotion; `programs/sequence_ops` stays a probe beside it,
		// because that one also asks for the ORDER of two containers (roadmap Gap R.33, closed by ADR 0365;
		// the ordering half is filed as Gap R.314).
		"sequence_builds",
		// A text a METHOD's body builds, in a method that takes a parameter, is the text the door rendered:
		// `return "x%s" % s` answers `xhi` and `return s * 2` answers `hihi`, line-for-line the reference. This is
		// the promotion ADR 0186 asks for — the probe was filed by ADR 0365 with both wrong lines pinned, and the
		// row that paid it is the text census reading methods as well as functions (roadmap Gap R.315, ADR 0366).
		// The last two lines are the control the promotion had to keep: a parameter some OTHER call site fills
		// with a number is not marked, and the method keeps the road it always had.
		"probe_method_text_answer",
		// A verdict a METHOD hands back is the verdict the reference prints: `def yes(self): return 1 == 1` /
		// `print(C().yes())` answers `True`, for a comparison, a containment test, an equality, the `bool`
		// constructor, an ordering and a verdict literal alike. The print road asks a `*Name` callee whether
		// its body returns only verdicts and never asked an attribute callee (roadmap Gap R.316, paid by
		// ADR 0366 beside the text half it shares with Gap R.315); the field getter and the `self.other()`
		// call are the same registration read from inside a body.
		"probe_method_verdict_answer",
		// A method whose NAME is a text method's no longer swallows the text-method call: `def upper(self, s)
		// return s.upper()` is answered by the case door, not by the class that declares `upper`. The shape
		// used to resolve to the class's own symbol, so every call called itself until the process died by
		// SIGSEGV with nothing on stdout — the artifact at fault, which ADR 0166 forbids and roadmap Gap R.317
		// records; the guard is that the answer is the reference's, not a number and not a crash.
		"probe_method_shadows_text_method",
		// `and`/`or` hand back the operand the test chose, on the compiled path: `2 and 3` is `3`, `"" or "d"`
		// is the text `d`, `[1] and [2]` is `[2]`, and `1 or True` stays the number `1` (roadmap Gap R.147,
		// ADR 0269). The shapes whose answer needs the kind to travel with it are filed beside it.
		"and_or_answer_like_python",
		// A comprehension over a list the compiler used to keep as a compile-time constant: the binding was being
		// deleted (its only read sat in a construct the escape walkers did not visit), so the loop had no object to
		// walk and the build stopped. `xs = [1, 2, 3]` / `[x * 2 for x in xs]` is `[2, 4]` now, on the compiled leg
		// and the reference's — a paid debt, promoted out of the probe ledger (roadmap Gap R.289, ADR 0356; the
		// iterable route is Gap R.291, ADR 0357, and the copy-as-iterable half beside it is Gap R.239).
		"probe_comp_folded_iter",
		// The immutable container is a value in the compiled backend, not a spelling the parser hands to a
		// list road: `t = (1, 2, 3)` prints `(1, 2, 3)`, `t[1]` is `2`, `len(t)` is `3`, `a, b = (4, 5)`
		// binds both halves, and `def pair(): return (1, 2)` hands the caller the same object — compiled
		// leg and reference, line for line. The probe that pinned "no tuple lowering at all"
		// (`unsupported expression *lang.Tuple`) is this row's promotion (roadmap L11.3, ADR 0361).
		"probe_tuple",
		// …and the case doors, the other half of "a text is a sequence of code points": `"straße".upper()`
		// is `STRASSE` and not `straße`, `"ΑΣ".lower()` is `ας`, `"ǅ".title()` keeps the digraph's title
		// form, and the same five operations answered through a parameter — where the compiler cannot read
		// the text and the runtime door has to answer — are the same answers. The fold used to borrow Go's
		// simple case mapping and the door used to know only A–Z, both at exit 0 (roadmap L11.5, ADR 0362).
		"probe_case_doors",
		// …and the half of the same operator the print door cannot answer: the operand the test did not choose
		// is not in the program at all. `x and boom()` with x false does not call boom, `y or (1 // 0)` with y
		// true does not divide, `if x and boom():` branches without running boom, and the operand the test
		// *does* reach runs exactly once — which the compiled leg used to get wrong twice over, once by running
		// the skipped operand and once by evaluating the tested one twice (roadmap Gap R.149, ADR 0275).
		"probe_and_or_the_test_skips",
		// The four numeric-truth rules the compiled backend used to answer wrong at exit 0: the flooring
		// identity, `x /= 2` rebinding an int variable to a double, and a number handed to a function keeping
		// the kind its argument had — `dbl(0.1)` is 0.2 and `bump(1.5)` is 2.5, the answers CPython and the
		// interpreter always gave (roadmap L11.6, Gap P.1's argument half, ADR 0276).
		"probe_float_numeric",
		// The same kind crossing a second boundary: a parameter the body hands on to another function
		// carries the pair too, which the callee's own text cannot show because a parameter is written by
		// the caller. Eight lines, three legs, one `define` each — `print(outer(2.5))` is 5.0 where the
		// compiled leg printed 4 at exit 0 (roadmap L11.6, Gap R.161, ADR 0277).
		"probe_forward_a_pair_through_a_function",
		// `//` and `%` choose their answer's kind by the operands, and a parameter has to carry the tag that
		// says which kind arrived: `floorit(5.0)` is 2.0 and `modop(7.5, 2)` is 1.5, where the compiled leg
		// printed 2 and 1 at exit 0 (roadmap Gap R.162, ADR 0278). Ten lines, three legs, one `define` each.
		"probe_floor_a_pair",
		// A floored pair combined with more arithmetic in one expression: ADR 0216's identity, the product of
		// a floor, a sum of a sum, both flooring operators in one answer — twelve lines the compiled leg either
		// refused or answered with a truncated digit before the arm question learned to walk its leaves
		// (roadmap Gap R.166, ADR 0279).
		"probe_combine_a_floored_pair",
		// The answer of a pair-returning call held by a *name*: bound and returned, bound and put into
		// arithmetic, bound and then overwritten with an ordinary int, and an identity assembled from two
		// floored call answers — eleven lines CPython prints and the compiled leg answered `4`, `2` and `5`
		// for, or refused outright, until the pair was read wherever such a name is read
		// (roadmap Gap R.164, ADR 0280).
		"probe_bind_a_pair_call_answer",
		// A function whose answer is `str()`/`repr()` hands back a text and its callers are told: the body
		// already asked the rendering door, only the caller's verdict was missing, and `print(g())` passed
		// the interned index to `printf` with `%d` — `0` at exit 0 where the reference prints `42`
		// (roadmap Gap R.163, ADR 0281).
		"probe_return_str",
		// How many arguments there were is one question, asked on the road every caller shares: eleven calls
		// the reference answers — defaults, keywords out of order, recursion, a method, a lambda read out of
		// a name — whole on the compiled path. `g(1, 2)` had printed `2` and `g()` had printed `0`, both at exit 0
		// (roadmap Gap R.168, ADR 0284).
		"probe_asked_how_many_arguments",
		// A number the body computed out of its own parameter keeps the kind the argument arrived with:
		// eight lines — add-one, double over a float and an int, an expression with arithmetic arms, and a
		// float default — whole on the compiled path. Each printed a truncated integer at exit 0 before ADR 0285
		// (`1` for `1.1`, `0` for `0.2`, `-1` for `-0.9`, `3` for `5.0` — roadmap Gap R.169).
		"probe_a_number_bound_from_a_parameter",
		// A rendering the body bound to a NAME is the same string return as `return str(v)`: `s = str(v)` /
		// `return s`, `repr` through a name, and `"x" + str(v)` all printed the interned index (`0`, `0`, `2`)
		// at exit 0 where the reference and the record leg print `3`, `3`, `x3` (roadmap Gap R.170, ADR 0286).
		"probe_a_rendering_bound_to_a_name",
		// Comparison chains: `1 > 2 < 3` is Python's construct — `1 > 2` AND `2 < 3`, the middle operand
		// read once — and this grammar parsed it as `(1 > 2) < 3`, comparing an int against a boolean,
		// which this front end answers rather than refusing. Four of these six lines printed the wrong
		// verdict at exit 0 on both witness legs, agreeing with each other and disagreeing with the reference
		// (roadmap L12.1 / Gap R.53, ADR 0288).
		"probe_comparison_chains",
		// A ternary with text arms used to print the @str_tab POSITION on the compiled leg:
		// `print("a" if c > 0 else "b")` said `0` where the reference prints `a`. Gap R.127's
		// recorded debt, promoted to parity by ADR 0290 (ADR 0261's rule: a promoted probe's
		// exit-code pin leaves the debt table with the probe).
		"probe_ternary_text_arms",
		// A text predicate — startswith, endswith, isdigit, isalpha and their siblings — answers a
		// VERDICT in the compiled path, but the print road never asked about a method call, so each line
		// printed the 0/1 WORD it holds: True as `1`, False as `0`, on the compiled path (Gap R.172, ADR 0289).
		"probe_a_text_predicate_prints_a_verdict",
		// `d.get(key)` with the key absent hands back None. The interpreter returned the bare word 0
		// and printed `0`; the compiled leg refused the program with `get: key not found and no
		// default` although its own fold already knew the key was absent. Four of these six lines were
		// wrong on the pre-cycle interpreter, `v == 0` among them (Gap R.174, ADR 0291).
		"probe_dict_get_answers_none",
		// A container in a numeric operand emitted IR the assembler rejects — `mul i32 @.lst1, 3` for
		// `print([0] * 3)`, `add i32 @.lst1, @.lst2` for `print([1, 2] + [3])`, `sitofp i32 @.lst1 to
		// double` for `print([1] / 2)` — because `value()` renders a list literal as the ADDRESS of a
		// compile-time global. llc refused the compiler's own module, and exit 2 is the contract's code
		// for OUR bug. Every line here is a pair the reference raises for, so the compiled path now raise the
		// reference's own sentence (roadmap Gap R.175, ADR 0292).
		"probe_a_container_in_arithmetic",
		// `x ** y` answers an int or a float, and the compiled leg answered EVERY power through
		// `fptosi` + `%d` — `print(4 ** 0.5)` said `1` where the reference says `2.0` — while the
		// interpreter answered `0` for `print(2 ** -1)` under a comment claiming Python does that. Both
		// backends agreed on the wrong answers, so only the oracle leg could see them
		// (roadmap Gap R.176, ADR 0292/0293).
		"probe_a_power_answers_the_right_kind",
		// A slice of a container answers the LIST. Three bugs on one row, none of them in the
		// interpreter: the literal's GLOBAL ADDRESS went where rt_slice wants a heap handle (llc
		// rejected the module — exit 2, ADR 0166); print then took the numeric road and printf'd the
		// handle as `1`; and rt_slice copied payloads without their tags, so a slice of texts printed
		// the interned INDEX — `[1]` where the reference prints ['b'] (ADR 0187's pairing rule).
		// Gap R.179 / ADR 0187, owner L11.1.
		"probe_a_slice_of_a_container",
		// A set counts DISTINCT members and a `dict.get` answers the kind its SLOT holds. Both were
		// compiled-leg wrong numbers at exit 0 the interpreter never had: the static set global counted
		// SOURCE elements (len({1,2,2,3}) was 4), and `get` printed the WORD its slot holds — an interned
		// text's index, the void 0, a verdict 1 — instead of rendering it.
		// Gap R.180 + Gap R.181 / ADR 0291 + ADR 0257, owner L11.1.
		"probe_a_set_counts_and_a_get_shows_its_kind",
		// A dict view prints as a VIEW: dict_keys(['a']), not ['a']. The compiled leg printed the
		// HANDLE (0) for keys() and spent exit 2 for values() of an int-valued dict -- a folded literal
		// rendered as a global address fed to a heap walker. The view keeps its list kind so sum/min/
		// max keep working; only the rendering knows it is a view. Gap R.182 / ADR 0296, owner L11.1.
		"probe_a_dict_view_prints_as_a_view",
		// A text answers truth like the reference and a string method prints its TEXT rather than the
		// intern INDEX. `not "x"` said True compiled (asI1 compared the intern slot to zero) while
		// `if "x":` beside it answered correctly; the print road listed 3 of the fold's 14 text-returning
		// methods -- twice -- so zfill/ljust/rjust printed 0; and "-42".zfill(5) gave 00-42 on BOTH
		// engines, which parity could not see. Gap R.183 + Gap R.184 / ADR 0297, owner L11.1/L11.2.
		"probe_a_text_answers_truth_and_its_methods_print",
		// A builtin called with no argument is a CONSTRUCTOR for four of them — int(), float(), bool(),
		// str() answer `0`, `0.0`, `False` and the empty text — and an arity error for the rest: ord(),
		// chr(), abs() and repr() raise in the reference and raise here too. All are settled on both
		// engines; the interpreter used to die with a Go index-out-of-range and exit 2, and the compiler
		// refused a program the reference runs (roadmap Gap R.131, ADR 0287 — promoted from a recorded
		// debt the day the three legs agreed).
		// `%` is the remainder for numbers on the compiled path — ints, negatives, doubles, the same body over a
		// pair-marked parameter, and a float compared to a text — thirteen lines pinned beside the refusal
		// that keeps the *text*-left spelling from answering a number (roadmap Gap R.165, ADR 0282).
		"probe_remainder_and_percent",
		// printf-style `%` on a text, on the compiled path: the conversions render what the reference
		// renders and the seven traps raise the reference's own classes — catchable, with its sentence.
		// The record leg refused every line and the compiled leg refused to lower it, so this program was
		// a debt row with the compiled leg recorded missing; the door that formats retired both refusals
		// (roadmap Gap R.31, L11.1, ADR 0360). The last line is the control: `%` between two numbers is
		// still the remainder, which is what keeps this probe from being a ban on the operator.
		"probe_percent_format",
		// `abs` answers with its operand's kind: the numbers keep answering (`abs(-3.5)` is `3.5`) and every
		// operand without a sign raises the reference's own sentence naming its kind — `str`, `NoneType`,
		// `list`, `dict`, `set`, and an instance's own class — catchably, on the compiled path (roadmap Gap
		// R.140, ADR 0271). The last two lines are the rebinding the door depends on: a name's *latest*
		// binding decides the kind the raise names (Gap R.145, ADR 0270).
		"abs_names_its_kind",
		// The same arithmetic one statement earlier — bound to a name before it is printed. The pair
		// the print door already took now travels through the binding, so `n = xs[0][0] * 2` and
		// `print(n)` answer 14 on both witness legs (roadmap Gap R.138, ADR 0267).
		"probe_arith_result_bound_to_a_name",
		// …and the same arithmetic bound by an UNPACKING: `a, b = xs[0][0] + 1, xs[0][1] + 2` is `8` and `10`.
		// The tuple target used to bind its names through the ordinary one-word road, which refused the slot
		// it cannot see into; it now asks the plain assignment's door list per element (roadmap Gap R.144,
		// ADR 0367 — the probe that pinned the refusal is this row's promotion).
		"probe_pair_from_a_tuple_unpack",
		// Every element kind the unpacking had to learn to carry: a slot read, arithmetic over a slot, a dict
		// entry read (which printed the text's `@str_tab` index at exit 0), a text / `None` / `bool` element
		// (which printed `hi`'s index, `0` and `1`), the answer of a call, a swap of two pair-bound names, and
		// the pair-bound names read back by print, str, len and an f-string (roadmap L11.1, Gaps R.144 and
		// R.321, ADR 0367).
		"unpack_pair_binds_by_its_tag",
		// …and the same name read back as a number: an operand, an ordering, a `while` head, a condition,
		// `str`, an f-string field, the target of `+=`. Fifteen lines, both legs, the same bytes
		// (roadmap Gap R.143, ADR 0268).
		"probe_pair_bound_name_as_a_number",
		// The same slot read handed to a function: the argument arrives as the (payload, tag) pair and the
		// answer's kind comes back in the word the callee stored beside its own return, so
		// `print(twice(xs[0][0]))` is `14` on both witness legs (roadmap Gap R.139, ADR 0273).
		"probe_slot_read_handed_to_a_function",
		// An int that meets `/=`, and an int handed a double by a later assignment, leave the variable
		// holding a float: the double goes into a float box and the name is bound to the (payload, tag)
		// pair, so print, an operation, a comparison, a condition, `str` and a `while` head all ask the
		// tag — and the variable beside it keeps its own number, which the `store double` into the
		// four-byte slot could not promise (roadmap L11.6, Gap P.1, Gap R.155, ADR 0274).
		"probe_int_state_becomes_float",
		// The unary minus names its operand's kind on the compiled path, and every shape the reference stops on
		// is a raise this program catches (roadmap Gap R.137, ADR 0266).
		"negation_names_the_kind",
		"features_a", "features_b",
		"stdlib", "dispatch_nested", "dispatch_gc", "dispatch_gc_stress", "match_baren", "match_literal", "match_classpat", "round_ties", "round_ndigits", "wrapping_decorator", "dunder",
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
		// shadow the built-in on the compiled path, where the compiled path used to read those
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
		// `for x in <integer>` as a repeat count, which the compiled path implement and
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
		// Division by zero raises, on the compiled path (L11.x Gap R.18, ADR 0212): the
		// program below is ledger-free because the oracle, the interpreter and the
		// compiled binary print the same nine lines.
		"zero_division",
		// Gap R.36 + R.39 (ADR 0228): a local written on one path only is unbound on the other. All
		// the record and the reference print `1` and then raise UnboundLocalError; the compiled leg used to print 0
		// for the second call and exit 0, and the interpreter used to call it the wrong class.
		// The compiled `try` dispatches every arm in order and hands an unmatched
		// exception outward (Gap R.20, ADR 0213); five shapes, both legs.
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
		// the same way on the compiled path and in CPython (ADR 0225).
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
		// Containers compare by value on the compiled path, and the row is checked against
		// CPython like every other parity case (roadmap L11.1, ADR 0189).
		"container_equality",
		// `print` writes its line as a unit: every argument is evaluated, and only then are the
		// separator, the arguments and the terminator written (roadmap Gap L.5, ADR 0355). The
		// program spent the oracle's whole life as a `debt` row — its last line interleaves a
		// callee's print into the caller's — and the row is deleted rather than rewritten, which is
		// what the ledger's promotion rule calls "done".
		"print_args",
		// The probe beside it, found by ADR 0186's sweep and pinned as debt ever since: the callee's
		// line now lands before the caller's on both legs, so the program is asserted against
		// CPython like every other case instead of carrying a ledger row.
		"probe_print_atomic",
		"truthiness",
		"subscript_assign",
		"container_methods",
		"none_values",
		// A bool is a value: `print(True)` writes True, `print(1 == 1)` writes True, and
		// --json reports its type as bool. Both witness legs print CPython's answer on every
		// line, which is what moved this program out of the probe list (roadmap L11.1
		// step 2, ADR 0257); the shapes that still print the number a bool is stored as
		// are probes of their own below.
		"probe_bool_value",
		"string_containers",
		"string_escapes",
		"string_params",
		// A float in a container slot (roadmap L11.1, ADR 0233): the element is the handle of a
		// float box and the comparison reads the doubles behind it, so [1] == [1.0] is True and
		// [1.5] == [1.6] is False on the compiled path. Both programs were oracle debt while the
		// literal emitter wrote a float's bits into a static i32 initializer (Gap R.40, ADR 0221).
		"probe_float_list_equal",
		"probe_float_container_equality",
		// The shape itself, both legs on one source: a float element, a float key read back,
		// a float appended to an integer list, a float written into a mixed list, and the tags
		// that let an unrolled loop print the element it was built from (ADR 0238).
		"float_container_elements",
		// The same shape one level down, both legs on one source: a list of lists prints,
		// compares by content, answers `in`, grows with an inner container, and loops over inner
		// containers — the tag routing the print and the comparison at run time (ADR 0238).
		"nested_data",
		// A container inside a container, read back out again: `xs[2][1]` and `m["k"]` reach through
		// a slot whose payload is the inner object's handle, and the tag the builder wrote is what
		// licenses the second read (roadmap L11.1, ADR 0241). It is here rather than in
		// conformanceProbes because the compiled path now print CPython's answer on every line.
		"probe_heterogeneous",
		// A container the program *built* rather than spelled out, read one level down: `xs.append([7,
		// 8])` leaves no literal behind, so the tag the object carries is the only thing that can say
		// whether the payload names a list, a dict or a text — and it is what the compiled subscript
		// branches on. It is here rather than in conformanceProbes because the compiled path now print
		// CPython's answer on every line (roadmap L11.1, ADR 0251).
		"probe_nested_list",
		// The rendering pair, both legs on one source: every value form written twice, once by
		// str() and once by repr(), from the one renderer print uses. Both witness legs print CPython's
		// answer on every line, which is what moved this program out of the probe list; the forms
		// the compiled backend cannot name are refusals it makes the same way on both halves
		// (roadmap L11.2, ADR 0258, closing Gap L.2).
		"probe_render_pair",
		// A pair-bound name as a container element: the literal is heap-built and the element writes its
		// payload AND its tag, so `[n]`, `[n, "x", 2, None]` and `y = [n]` read back exactly as the
		// reference prints them — a text element prints its text, not its interned index, and a float
		// element its double, not its box handle (roadmap Gap R.146's element half, ADR 0306).
		"probe_pair_bound_name_enters_a_container",
		// An interpolated field asks the tag: `f"{n}"` over a name the pair road bound prints what the
		// reference prints — the text, not its interned index, the double, not its box handle — because the
		// field goes through the module's ONE tag-reading printer, and `!r` asks that door with the quote
		// flag instead of being thrown away at parse time (roadmap Gap R.146's rendering positions,
		// Gap R.192; ADR 0307, ADR 0303's one printer).
		"probe_fstring_field_asks_the_tag",
		// abs asks the tag: the signless CALL over a name the pair road bound takes its operand's kind from
		// the tag and answers a number whose kind follows it — 7, 1 and 2.5 out of slots that hold an int, a
		// bool and a float — because the magnitude is taken inside the arithmetic door the operators already
		// use, not by a lift that would hand a text slot's interned index to a magnitude (roadmap L11.1,
		// Gap R.146; ADR 0309, ADR 0265's per-operator door, ADR 0271's raise wording).
		"probe_the_signless_call_answers_for_a_pair_bound_name",
		// A pair-bound name as a dict entry and a set member: the entry and the member go to the builders that
		// take the tag with the payload, so a text value prints its text and not its interned index, a float
		// value its double and not its box handle, a text member its text and not a bucket number — and a key
		// or member that cannot be hashed says so in CPython's own words, catchable by the program's arm
		// (roadmap L11.1, Gap R.146's positions that keep one word for a whole value; ADR 0310, ADR 0306).
		"probe_pair_bound_dict_entry_and_set_member",
		// A pair-bound name written by a STATEMENT — appended, subscripted, added, put into an entry: each
		// mutation goes to the door that takes the payload and the tag together, so an appended text is the text
		// and not its interned index, an added member dedups on both words, and a member or key that cannot be
		// hashed raises CPython's `TypeError: unhashable type: 'list'` with the program's arm running (roadmap
		// L11.1, Gap R.146's mutation roads; ADR 0311, ADR 0310, ADR 0306).
		"probe_a_pair_bound_name_mutates_a_container",
		// A bool stored in a container, both legs on one source: the slot carries a bool tag, so
		// the list, the dict and the str()/repr() of both print `[True, 1]` and `{'k': True}` like
		// CPython — and the numeric questions still answer as the number (True + 1, [True] == [1],
		// d[True]) on every leg (roadmap Gap R.112, ADR 0259; the program left the debt ledger with
		// that row, which is the promotion rule of ADR 0186).
		"probe_bool_in_a_container",
		// A bool handed to a function, both legs on one source: the call sites answer what the parameter
		// holds, so `show(1 == 1)` prints True where it used to print the 1 the comparison produced. The
		// reading is the front end's (every call site, literal or bound name, asked before the parameter
		// is called a verdict), and a position the front end cannot answer keeps yesterday's number —
		// which is why the mixed-call-site rows live in integration/bool_call_boundary_test.go and not
		// here (roadmap Gap R.111, ADR 0348; the program left the debt ledger with that row, the same
		// promotion rule ADR 0259 used for the container slot).
		"probe_bool_through_a_call",
		// A negative zero keeps its sign whoever wrote it, both legs on one source: the sign is a
		// property of the number and not of the road that made it, so the emitter states a signed
		// constant by its IEEE bits instead of adding +0.0 to it (which IEEE answers for -0.0 with
		// +0.0) and the one runtime renderer that handles signs — `rt_fmt_double` — is finally handed
		// the value (roadmap Gap R.132, ADR 0349; the program left the debt ledger with that row, the
		// promotion rule ADR 0257 and ADR 0259 used for the bool renderings before this one).
		"probe_negative_zero_constant",
		// The operator that chooses an operand, both legs on one source: `max([True, 1])` is the
		// verdict and `max([1, True])` is the number, because the strict comparison keeps the first
		// candidate and the chosen candidate decides what prints — str(), a container slot, an f-string
		// and the arithmetic all follow the same answer (roadmap Gap R.117, ADR 0261; the program left
		// the debt ledger with that row, and the `and` row at the end is the line that had to stay a
		// number while everything around it became a verdict).
		"probe_bool_chosen_by_an_operator",
		// A dictionary built with a key it already holds: the literal, the comprehension and item
		// assignment all put the entry through the dict's own key rule, so `{"a": 1, "a": 2}` is one
		// entry printing `{'a': 2}` — position from first insertion, value from last write, and the
		// `1`/`True`/`1.0` rows agreeing because a bool and a float are their numbers (roadmap Gaps
		// R.118 and R.120, ADR 0260). The interpreter grew dicts by append and disagreed with the
		// compiled backend about an ordinary dictionary.
		"dict_key_rule",
		// The dict comprehension that started the row: `{1: 2 for x in [1, 2]}` is one entry and a
		// length of 1 on both legs now, where the retired engine printed `{1: 2, 1: 2}`
		// and count the pair (roadmap Gap R.118, ADR 0260 — promoted out of the debt ledger).
		"probe_dict_comprehension_duplicate_key",
		// The ordering of those same slots, both legs on one source: `<`, `<=`, `>`, `>=` of a slot
		// whose kind only the object can report, answered as two numbers, two texts, or the `TypeError`
		// CPython raises naming the kind the slot really holds — with the arms nobody can reach not
		// emitted, and the cross-kind pairs caught rather than printed as a verdict (Gap R.93, ADR 0252).
		"slot_order_object",
		// The true division of those same slots: `/` is the one arithmetic operator whose result kind is
		// settled before the slot is asked, so the tag answers the rest — an int or bool slot converts,
		// a float slot unboxes, every other kind raises CPython's sentence naming what it holds, and both
		// ZeroDivisionError wordings are chosen inside the arm that knows the operand kinds (Gap R.96,
		// ADR 0253). Three engines, one source, where the compiled leg printed `0.0`.
		"slot_division",
		// A parameter the body rebinds to a float and returns by bare name: the return word used to be
		// chosen from the shape of the return expression alone, so the function was emitted `i32` and the
		// float the body computed had no word to travel in — the call answered the argument, and the
		// ledger had been pinning that as debt since ADR 0196 measured it (roadmap L11.6, Gap R.3c,
		// ADR 0254). Three engines, one source.
		"probe_float_param_rebind",
		// `min(a, b, ...)` / `max(a, b, ...)`: the chosen candidate, not the comparison, decides
		// what kind the answer has, so an int winner among doubles stays `1` and a float winner
		// stays `1.0`; text candidates go through the content-order helper, and a candidate of an
		// incomparable kind raises the operator's own TypeError. Three engines, one source
		// (roadmap L11.6, Gaps R.73 and R.104, ADR 0256).
		"min_max_values",
		// `min`, `max` and `sum` over a value whose kind the run time chose: the fold hands back one of the
		// values it was given, so the ANSWER'S KIND is the winner's kind and the pair has to travel out of the
		// call with it — `min(2.5, 3)` is the float `2.5`, `max(2.5, 3)` is the integer `3`, `max(True, 1)` is
		// `True`, `sum([n, 1])` is the left fold over `+` that CPython's message names 'int' first for, and a
		// fold of two values with no ordering raises the reference's own sentence, catchably, from the fold
		// door rather than from the compiler (roadmap L11.1, Gap R.146's fold clause, ADR 0316). Promoted the
		// day its sibling probe `probe_pair_bound_name_takes_a_value` stopped being a debt: the `min(n, 3)`
		// line in that program is the line this door paid.
		"probe_the_fold_builtins_answer_the_pair",
		"probe_pair_bound_name_takes_a_value",
		// The fold asked to order containers the program BUILT, which is the residue that door left beside it:
		// `min(a, b)` over two built lists is [1, 2] and `max` is [3], a set orders by the subset operator and
		// the pair that is neither one's subset orders NEITHER way rather than raising, the empty list is the
		// lesser list, and a container folded against a number raises CPython's `TypeError` naming both kinds
		// instead of answering the container's element count (roadmap Gap R.197, ADR 0318, the half ADR 0316
		// filed; the walk is one helper, @rt_pair_order, that the fold asks and no road copies).
		"probe_a_fold_orders_two_built_containers",
		// The number door is keyed by the container a store WRITES, not by the program: a text appended to
		// one container closed `+` and `*` over a slot of every other container in the module, so this
		// program — a text in `w`, numbers in `nested` — printed a refusal at exit 1 where the reference
		// prints `[14]`, `8`, `5`, `15.0`, `-7`, `42`, `5`, `2` (roadmap Gap R.191, ADR 0322). The block
		// travels with the object now: an alias, a loop variable and a parameter beside its argument each
		// carry a store's block to every name that can denote the same container.
		"probe_a_text_in_one_container_leaves_another_number_door_open",
		// The membership test and the loop over a slot the program BUILT ask the object, not the notebook:
		// `7 in xs[0]` is True, `"k" in d["a"]` asks a dict's keys, `for v in xs[0]` prints 7 and 8, and a
		// slot holding a number raises the reference's own sentence — with the two wordings the two
		// questions use, `argument of type 'int' is not iterable` for the test and `'int' object is not
		// iterable` for the loop, both reaching the program's `except TypeError:`. Before this the test
		// refused and the loop answered one `0` for CPython's `1` and `2` (roadmap Gap R.95, ADR 0323,
		// ADR 0251's dispatch handed to the two callers that had never taken it).
		"runtime_slot_haystack",
		// The same two doors one level deeper, and the needle asked the way the haystack is asked:
		// `7 in xs[0][0]` is True, `for v in d["a"]["k"]` prints 1 and 2, a container read out of one slot
		// is found inside another container by *content*, and a container needle into a dict or a set slot
		// raises `TypeError: unhashable type: 'list'` where it used to answer "not present". The read below
		// such a slot has answered since ADR 0251; what was missing was handing the pair it computes to the
		// two callers that pick a runtime helper (roadmap Gap R.210 and Gap R.211, ADR 0324).
		"runtime_slot_haystack_deep",
		// The tag a *writer* leaves, not the door that reads it. An instance slot used to answer as `int`
		// to print, len, membership and the loop; the object's own class id now names itself in all four,
		// static `c in "abc"` / `"abc" in c` raise the reference's text and iterable sentences, and identity
		// comparisons stay identity. The stable claims are the oracle row; the CLI identity-print rows carry
		// the address-form claim separately, normalized by `instance-identity` (roadmap Gap R.209, ADR 0325).
		"runtime_slot_instance_tag",
		// The constant fold, which used to stop the program at the compile door for a wrong literal.
		// `[][0]`, `"abc"[7]`, `{1: 2}[3]`, `int("12 x")`, `float(None)`, `ord("ab")`, `chr(1114112)` and a
		// position below a comprehension are now ordinary raises, each caught here by its own class, and the
		// literals that DO spell a number answer — including `int(" 12 ")`, `int("1_0")`, `int(True)`,
		// `float("1_0.5")` and the no-argument forms (roadmap Gap R.37, ADR 0326).
		"constant_trap",
		// A dict literal is a dict: its entries are read by the door a dict variable's entries are read by.
		// A text key, a float key, a `None` key, a bool key that IS a number key, an entry whose value is a
		// container, a key that is simply absent (KeyError, catchable) — all of them answers the reference's
		// answer here, where the same programs used to stop the build at exit 1 with `index must be a
		// constant` (roadmap Gap R.215, ADR 0327).
		"dict_literal_key",
		// A `None`, `True` or `False` key is a key the dict read doors can be asked with: the entry arrives
		// as a (payload, tag) pair and the printer, the binder, the loop and the `or` door read the kind the
		// writer left beside it. Twenty-six lines, every one CPython's — where the same programs printed the
		// float box's handle, the interned text's index or the digit `0` at exit 0 (roadmap Gap R.221, ADR 0353).
		"dict_none_bool_key",
		// The standard library answers the reference's NAME: `math.pi`, `math.e` and `math.tau` are the
		// literals `stdlib/math.gy` declares and this language's `PI`/`E`/`TAU` are aliases of them, so the
		// program below is one CPython runs and the two legs are compared rather than described
		// (roadmap L11.6's last clause; the type came from ADR 0272 and the twin comparison from ADR 0321,
		// which is what lets `probe_math_const` — this file with the upper-case spellings, a name the
		// reference does not have — stay an honest `not_applicable` row instead of a hand-carried pin).
		"probe_math_constant_answers_the_references_spelling",
		// The precise-root repro: a frame local that must survive a nested allocation
		// storm, a statement-position callee whose loop reclaims as it goes, and
		// thousands of short-lived containers (ADR 0181).
		"gc_precise",
		// The arena's growing half (roadmap Gap Q.2, ADR 0350): 1025 containers, every one of them
		// reachable from `groups`, so no collection can hand a slot back and the allocator has to have
		// widened its window to answer at all. On the commit before this row the same file printed
		// `MemoryError: … holds 1024 live containers` at exit 3 while CPython printed `5 205`; it is the
		// row's Definition of done as one program, and the reference leg of the matrix.
		"gc_heap_growth",
		// A `lambda`'s parameter list is spelled the way a `def`'s is, so its defaults are a program and not
		// a parse error (roadmap Gap R.263, ADR 0352). The rows are crossed — an omitted argument, a supplied
		// one, a keyword one, a default that is a call, a module name, a conditional and a text with a `:` in
		// it — and the `def` rows beside them are the parity claim the row is built on.
		"lambda_default_param",
		// A module name is bound in one slot whichever statement binds it (roadmap Gap R.49, ADR 0351).
		// The program reuses one name across the module, a `def`'s parameter and a `for` target, which is
		// the crossing that made the loop bind a main-frame alloca while every read consulted the name's
		// module global — `y = 2` / `for y in [10, 20]: print(y)` printed `2`, `2` at exit 0 — and made
		// `y += 3` store to a slot that had never been allocated, which `llc` rejected at exit 2.
		"module_slot_scope",
		// A key the language refuses to hash, at the three doors a dict is consulted through. The
		// read said `KeyError: key not found`, the membership test answered `False` at exit 0 and the
		// store built the entry and printed `{[1]: 2}`; the reference raises `TypeError: unhashable
		// type: 'list'` at all three, and the program prints the caught sentence for each, so a line
		// that goes missing is a door that stopped refusing (roadmap Gap R.217, ADR 0328). The list
		// haystacks in the same program are the soundness half: `[1] in [[1], [2]]` walks, does not
		// hash, and must stay True.
		"unhashable_key",
		// An index that is not a position, at the two doors that subscript a container which counts.
		// `xs["a"]`, `xs[1.5]` and `xs[None]` each printed an element and `s["a"]` printed a character,
		// all at exit 0 — a text index is an @str_tab index, so the door counted to the slot the interned
		// number named — and `xs[[0]]` lowered a list literal into the bounds check's `icmp slt`, so llc
		// rejected the module and the toolchain exited 2, the code the exit-code contract reserves for a
		// compiler bug. The reference raises TypeError naming the type (roadmap Gap R.222, ADR 0329); the
		// program prints the caught sentence for each, and the positions, the negative position, the bool
		// and the slice on the way through are the rows a gate that fired too eagerly would break.
		"list_index_kind",
		// A list literal read by a position, through the door a list variable's element is read by. Four
		// shapes answered their payload at exit 0 — `print(["a", "b"][0])` printed `0` (an index into
		// @str_tab), `[1.5, 2][0]` printed `1` (a @float_box handle), and None and a bool printed `0` and
		// `1` — while the same element off a variable answered correctly, because the pair door had a
		// `*DictLit` arm from ADR 0327 and no list one beside it (roadmap Gap R.218, ADR 0331). The fold's
		// all-integer shape, the negative position, the nested literal, str()/repr(), len, `in`, a binding
		// and a loop are all in the same program: the fold keeps what it reads, and the door answers the rest.
		"list_literal_read",
		// A literal's element that is itself a read is stored as the pair the read answers. Four shapes
		// asked the builder for one word: `[{"a": "x"}["a"], 2]` printed `[1, 2]` (the text's @str_tab
		// position), `[["a"][0], 2]`, `[[1.5, 2][0], 3]` and `xs = [1, "a"]` / `[xs[1], 2]` refused with the
		// one-word sentence, while `print(xs[1])` had printed `a` all along — the print door, the binding
		// door and the membership door all ask the subscript for a (payload, tag) pair and only the
		// container builder asked for one word (roadmap Gap R.224). The dict entry, the dict key and the
		// set member ride the same door, and the two raises inside an element are the reference's own,
		// caught here by their own class.
		"pair_element_read",
		// The answer a call hands back when it hands back a container is a value. `def build(): return
		// [1, 2]` / `print(build())` spent the contract's compiler-bug code on a program the reference runs
		// (`ret i32 @.lst1` — a global in a value position, which `llc` rejects), the run-time-built spelling
		// answered `0` at the code of success, and a loop over the answer printed nothing while a condition on
		// it called a non-empty list false (roadmap Gap R.67). The three kinds, the empty containers spelled
		// two ways, the tags a mixed list carries, a forwarding `return`, a body that filled the container in
		// a loop or with a comprehension, and every door that reads a container back — print, str/repr, len,
		// membership, the loop's iterable, a condition, `not`, a binding and a mutation — are all in this
		// one program, whose expectation is what CPython printed for it.
		"container_returns",
		// The element of the answer a call hands back is read by the door that reads a container variable's
		// slot. `def build(): return [1, 2]` / `print(build()[7])` is `IndexError: list index out of range` in
		// the reference and used to be `codegen: index requires an inline list/dict/set literal` at exit 1 here,
		// while `ys = build()` / `print(ys[7])` had raised the catchable trap since ADR 0326 and `print([1,2][7])`
		// since the same ADR — one question, three doors, and the missing one chose its road from the base's
		// spelling rather than from what the compiler holds (roadmap Gap R.214, ADR 0334). The three kinds, a
		// mixed list, an all-text answer's slot, an int-keyed answer's slot, a list of dicts read twice, a nested
		// answer, a run-built container, a loop-filled one, a forwarding call, a variable index, arithmetic and
		// `str`/`repr` over a slot, and both raises caught by the program's own handler are in this one program,
		// whose expectation is what CPython printed for it.
		"container_subscript",
		// A slice hands back a copy of the base's slots WITH their tags, wherever the base lives. A mixed list
		// `xs = [1.5, "a", [7]]` sliced with `print(xs[0:2])` is `[1.5, 'a']` in the reference and used to be
		// `[1, 0]` at exit 0 here — the interned index of the text and the box handle of the float, read as the
		// numbers they are not — while the same copy bound to a name printed its heap handle as `1` and `len` of it
		// refused, because nothing had recorded that the name received a list. The three kinds of base (a literal
		// node, a name bound to one, a call's container answer), the untagged copy that must keep its plain word
		// road, a copy of a copy, the truthiness, membership and equality questions asked of a copy, and the text
		// slice that never left its own road are in this one program, whose expectation is what CPython printed for
		// it (roadmap Gap R.230, ADR 0335).
		"slice_answer",
		// A slice bound is asked what it is: an integer (or a bool) is a position, `None` is the absence of one,
		// and anything else the compiler can name the kind of is the reference's `TypeError: slice indices must be
		// integers or None or have an __index__ method`. `xs = [1, 2]` with `print(xs["a":"b"])` answered `[1]` at
		// exit 0 — the two texts' `@str_tab` indices taken for positions — and `print(xs[None:None])` answered `[]`
		// because a `None` lowers to the word 0 and the presence flag was set beside it as though the program had
		// written a number; `print(xs[[0]:2])` handed a list literal's global address to a helper whose parameter is
		// an i32 register and `llc` rejected the module (exit 2). The `None` spellings, the bool bounds, the
		// out-of-range and negative positions, the same questions over a text base and over a call's answer, and
		// five of the raises taken by the program's own `except TypeError` are in this one program, whose expectation
		// is what CPython printed for it (roadmap Gap R.233, ADR 0336).
		"slice_bounds",
		// The minus between two expressions is the SUBTRACTION operator and the minus before a literal in
		// an expression position is its SIGN — one question the lexer used to answer from the character after
		// the minus alone, so `x = 5` / `y = x-1` / `print(y)` wrote `5` at exit 0 where the reference writes
		// `4` (the program never contained a subtraction: it was `y = x` and an expression statement `-1`) and
		// `print(1-1)` died with `parse error at 1:8: expected ")"`, taking `print(x-1)`, `len(xs)-1`,
		// `xs[2-1]`, `range(3-1)`, `return n-1` and `w["a"]-1` down with it. The operator side, the sign side
		// that must not move (`print(-1)`, `xs[-1]`, `{-1: "neg"}`, `f(-1)`, `2 ** -1`, `def f(a=-1)`), the
		// `5--1`/`5- -1` spellings, the bool and float answers, the loop heads and a method's `self.n-1` are
		// in this one program, whose expectation is what CPython printed for it (roadmap Gap R.242, ADR 0338).
		"minus_operator",
		// A `for` loop asks its ITERABLE what it is before choosing a road for it. The loop had three
		// ways to step over an iterable and no question to choose between them, so a container the
		// compiler could write down but could not NAME — `sorted([3, 1, 2])`, `reversed([1, 2, 3])`,
		// `sorted([3, 1, 2], reverse=True)`, `sorted([])`, `reversed("abc")`, `sorted(xs)`, `sorted(b())`,
		// a slice copy — fell to the counter road, which read the iterable's word as a repeat count:
		// `%t1 = icmp slt i32 %_ctr1.ld1, @.lst1`, which `llc` refuses (exit 2, the code ADR 0166
		// reserves for a compiler bug), or succeeded and printed the counter, or printed nothing at all.
		// The literal fold, the runtime copy the container pair names, the loop's own semantics over a
		// folded iterable (break, continue, else), the roads the row must not disturb (a list, a text,
		// `range`, a set, a dict's keys, a generator's answer, a user function's answer) and the claim a
		// loop variable makes from its OWN iterable are in this one program, whose expectation is what
		// CPython printed for it (roadmap Gap R.246, Gap R.252, ADR 0339).
		"for_iterables",
		// The ordering builtins read their options by name. A `key=` is handed to the ordinary call road,
		// so arity, returns and raises inside the key are that road's business; an invalid keyword is the
		// reference's TypeError and not a positional option that silently meant something else (roadmap
		// L11.7, Gap R.249, ADR 0340).
		"key_argument",
		// A container variable asks its LAST binding what it is. Six binding roads had been deciding a
		// name's container kind six ways and the list-literal road cleared nothing at all, so the object
		// a statement freed kept answering reads of the name: `d = {1: 10}` / `d = [3, 4]` printed
		// `{3: 4, 0: 0}` and `d[0]` printed `0`, both at the exit code that means the program ran. The six
		// transitions between the container kinds, the mixed (per-slot-tagged) spellings, the detour
		// through a scalar, the same kind with a new object, the loops, copies, comprehensions and the
		// bounds words the last binding earns are in this one program, whose expectation is what CPython
		// printed for it (roadmap Gap Q.1, owner L11.1, ADR 0341).
		"rebound_container_kind",
		// A function passed to a function is a value the compiled backend answers. The body of `apply`
		// calls its *parameter* (`f(x)`), which is the shape L11.7 recorded as refused since the backend
		// had one word per value: the defunctionalisation pass gives the lambda — and a `def` reached
		// through an alias — a declaration of its own, so the call is an ordinary one by the time the
		// doors that decide a call are asked, and both programs print CPython's answer on the compiled
		// leg (roadmap L11.7, ADR 0345; these two probes recorded the debt).
		"probe_fn_value", // L11.7 — a lambda called through a parameter
		"probe_fn_name",  // L11.7 — a declared function called through a parameter
		// An interpolation standing where a VALUE goes: bound, concatenated, given a method, measured,
		// compared, subscripted, sliced, returned, passed to a text parameter, stored in each of the three
		// containers, and looped over — with the void and the container field the one printf could never
		// have stated (roadmap L11.1, Gap R.146's value half; ADR 0359). Line for line CPython.
		"fstring_value",
		// A higher-order body that calls itself: the clone answers its own recursion by calling itself,
		// and the entry tuple is read by the called parameter's ORDINAL rather than by its position in the
		// source, so `def run(v, fn)` calls `fn` by name too. Every group is line for line CPython
		// (roadmap Gap R.267, Gap R.313; ADR 0364).
		"recursive_callable",
		// A hook that reads its key out of the NAME a lambda was bound to: the defunctionalisation pass
		// retargets the `key=` argument to the declaration it minted for that lambda, so the door that
		// calls the key once per element calls a `define` rather than the payload word `0`. Every group is
		// line for line CPython (roadmap L11.7 · Gap R.281, ADR 0369).
		"named_hook",
	}

	cases := make([]lang.ConformanceCase, 0, len(names))
	for _, n := range names {
		cases = append(cases, lang.ConformanceCase{
			ID:       "programs/" + n,
			Name:     n + ".gy",
			Source:   readProgramSrc(n),
			Asserted: true,
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
			ID:       g.id,
			Name:     g.name,
			Source:   mergePrograms(g.files...),
			Asserted: true,
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
		// One argument that never reaches the printf-style door as a (value, kind) pair: the integer beyond
		// this backend's bounded-integer row, so the conversion has nothing to render and the door refuses it
		// in words where CPython prints 22 digits. The program's own comment names its three siblings — the
		// float literal's exponent, a tuple where a value goes, a COMPUTED surrogate — and why they are not
		// lines here: a front-end refusal ends a program before its first line runs, so one program witnesses
		// one refusal (roadmap Gap R.31, ADR 0360; the siblings are pinned one per program by
		// `TestPercentArgumentResidueAtTheCLI`).
		"probe_percent_arguments_beyond_the_door",
		// A container has the methods the reference's containers have: extend, insert, index, remove,
		// clear, update, pop, setdefault. Neither engine had any of them, so a program that reads like
		// Python stopped at the first line. Gap R.188 / Gap R.63, ADR 0301, owner L12.11.
		"probe_a_container_has_its_methods",
		// An in-place container mutation answers the void. The interpreter used to answer the
		// container itself -- [1, 2] for `print(xs.append(2))` at exit 0 -- and the compiled leg
		// emitted a printf with a MISSING operand, which llc rejects: exit 2, the forbidden class.
		// Gap R.187, ADR 0300, owner L12.11.
		"probe_an_in_place_mutation_answers_none",
		// An f-string's format spec and conversion. The record leg answers every field the way the
		// reference does; the compiled leg answers the constant ones and refuses a field it cannot
		// read at compile time. Before this the spec was cut off at parse time and the compiled path
		// printed the plain value at exit 0 -- parity could not see it because the engines agreed.
		// Gap R.186 / L12.8, ADR 0298's sibling ADR 0299.
		"probe_a_format_spec_formats",
		// A text used as an iterable — a comprehension over one, and max/min of one. The interpreter
		// answered [] and the whole string; it now answers the characters the reference answers, and the
		// compiled leg's refusal is RECORDED rather than pinned as a limit. Gap R.185 / ADR 0298.
		"probe_a_text_iterates_one_character_at_a_time",
		// The same value's *kind*, chosen by `and`/`or`: the print door renders a chosen operand by its tag,
		// and the positions that keep one word for a value still decline (roadmap Gap R.147's owed half,
		// owned by L11.1 and Gap R.146, ADR 0269).
		"probe_and_or_shapes_the_word_carry",
		// The verdict an operator *picks* — max/min's chosen candidate — is parity surface now: the
		// program lives in conformanceStandalone (Gap R.117, ADR 0261). What stays filed is the shape
		// whose candidate the compiler cannot read at all, and the ternary whose test it cannot read:
		// both print the number underneath, one on the compiled leg and one on both.
		"probe_minmax_candidate_unreadable",
		"probe_ternary_the_test_chose",
		// `round(x, ndigits)` itself is parity surface (programs/round_ndigits.gy, ADR 0263). What stays
		// filed beside it is the same call on a value whose kind the module cannot see — a parameter the
		// call filled with a double, a loop variable off a literal list — where the compiled leg reads
		// the i32 road of the call and prints the truncated number, or the handle (roadmap Gap R.129).
		"probe_round_digit_count_kind_unseen",
		// floor/ceil answer a whole number on the compiled path now (Gap R.51, ADR 0264). What stays filed is
		// the answer past the compiled int word: the evaluator's int64 answers CPython's number and the
		// compiled guard raises an OverflowError naming L12.12, rather than wrapping in silence
		// (roadmap Gap R.133).
		"probe_whole_number_beyond_the_int_word",
		// The negation of a text is answered by the compiled path the way the reference answers it — the shape
		// this row filed (Gap R.137) is parity surface now, in programs/negation_names_the_kind.gy
		// (roadmap L11.1, ADR 0266).
		// Arithmetic whose whole-number answer will not fit the compiled int word: the evaluator's int64
		// answers CPython's number, the compiled guard raises a catchable OverflowError naming L12.12
		// rather than truncate a double through an i32 (ADR 0264's lesson at the new door).
		"probe_whole_number_slot_beyond_the_int_word",
		// The arithmetic the print position answers, one statement earlier, and the number positions that
		// read the bound name back, are parity surface: both programs live in conformanceStandalone
		// (roadmap Gaps R.138 and R.143, ADRs 0267 and 0268), and so does the unpacking of the same
		// expression (Gap R.144, ADR 0367); the same value handed to a function is parity surface since
		// ADR 0273, and the value handed to a FOLD is parity surface since ADR 0316 — the program that pinned
		// it, programs/probe_pair_bound_name_takes_a_value.gy, was promoted with that line.
		// A double written with an exponent — the spelling a scientific value arrives in — does not lex:
		// the compiled path stop at a parse error where the reference parses `1e18` as 10^18 (roadmap Gap
		// R.135). Filed while measuring `sqrt`, whose natural test values are 1e18 and 1e-3.
		"probe_float_literal_with_exponent",
		// A folded non-finite constant used to be emitted as `inf.0e+00`, which `llc` rejects: exit 2 on a
		// program the reference prints. Fixed by spelling the value as its IEEE bit pattern; this row is
		// the regression net, and it is `not_applicable` only because `sqrt` is not a CPython builtin
		// (roadmap Gap R.134, closed alongside Gap R.51 by ADR 0264).
		"non_finite_float_constant",
		// A name the checker's own table advertises and neither engine can call: `pow` is CPython's `8`,
		// this toolchain's answer is a NameError interpreted and a refusal compiled (roadmap Gap R.136,
		// found by calling every name in `predeclared.go`, the same sweep that found R.51's three alive).
		"probe_predeclared_name_not_callable",
		// The wall underneath that one, measured while finding it and older than it: a `for` binding over
		// a literal container of doubles used as a number multiplies the element handle (roadmap Gap
		// R.130, ADR 0261's read paid for the subscript, the loop binding never followed).
		"probe_float_loop_variable_as_number",
		// The ternary's *number* half is paid (ADR 0262 emitted the double `select`) and so is its
		// TEXT half (ADR 0290 emitted the i32 select over interned indices, and taught the print
		// road and the `strFuncs` pre-scan to ask about a ternary's arms). What stays filed is the
		// container arm: it puts `@.lstN` in an operand position and the assembler rejects the
		// module (roadmap Gap R.128, owned by L11.1's tagged value word).
		"probe_ternary_container_arms",
		// A list comprehension tags the slot it copies; a set or dict comprehension still folds to a
		// compile-time global with no tag table, so those lines refuse rather than print the number
		// (roadmap Gap R.116, measured closing Gap R.112).
		"probe_bool_in_a_comprehension",
		// A dict comprehension whose key is written as text and whose entries the compiler has to build
		// at run time: the interpreter and CPython agree, the compiled backend declines with
		// `comprehension key must be constant`, which describes the shape its builder walks (an integer
		// key) rather than the shape the program wrote (roadmap Gap R.123, measured with ADR 0260).
		// The same comprehension with an integer key is parity surface.
		"probe_dict_comp_text_key",
		// A dict comprehension that writes the same key twice is a paid debt: it puts the entry, so
		// `len` counts one — the program lives in conformanceStandalone now (Gap R.118, ADR 0260).
		// An ordering a program cannot ask for — a container slot against a text — inside a ternary:
		// CPython and the record leg raise TypeError, the compiled backend folds the condition and
		// prints the true branch. The int spelling predates this cycle; the bool spelling is what
		// found it (roadmap Gap R.119).
		"probe_slot_order_in_a_ternary",

		"probe_mixed_return_value",    // Gap R.22 — returns of differing types share one lowering
		"probe_builtin_traps_untyped", // Gap R.25 — a trap with no class cannot be caught
		"sequence_ops",
		// A text held in a FIELD prints the intern position under it: `def get(self): return self.v` for a
		// field every write hands a text answers `0` where the reference answers `hi`, at exit 0. The verdict
		// twin answers since ADR 0366 (`self.flag = flag` / `return self.flag` prints True); the text half is
		// the same reading the field census has never had for text (roadmap Gap R.319).
		"probe_method_text_field",
		"probe_operand_types",    // Gap R.26 — an operator applied to the wrong operands
		"probe_global_statement", // Gap R.48 — no `global` statement; compiled, the record leg and the reference each answer differently
		"unwritten_slot_trap",    // Gap R.36 + R.39 — an unwritten local traps with the right class (ADR 0228)
		// A capture is a binding, so it happens in the arm that matched and nowhere else: the arms that
		// did not run used to write the slot on their way past it, and the read afterwards answered it
		// (roadmap Gap R.68, ADR 0358).
		"match_binding_edges",
		"probe_math_const", // L11.6 — a stdlib float constant folds to int
		"probe_enumerate",  // L11.7 + L11.3 — enumerate/zip/reversed yield tuples
		// A declared name is not a variable, and a function is not a number: reading a `def`'d name is no
		// longer a NameError, so a function flows through a parameter on the interpreter, and the numeric
		// door raises CPython's `'function'`/`'module'` sentence instead of handing `llc` a load of a slot
		// no `def` allocated (roadmap Gap R.150, Gap R.151, Gap R.153, ADR 0283).
		"probe_a_function_as_a_value",
		// How many arguments there were is one question, asked on the road every caller shares: eleven calls
		// the reference answers — defaults, keywords out of order, recursion, a method, a lambda read out of
		// a name — whole on the compiled path. `g(1, 2)` had printed `2` and `g()` had printed `0`, both at exit 0
		// (roadmap Gap R.168, ADR 0284).
		// Found by the boring-program sweep (ADR 0190): the tutorial-shaped programs nobody
		// probed, twelve of them, five divergences.

		// Pinned by the runtime-comprehension work (ADR 0192): two honest refusals and one
		// llc rejection that is a compiler bug, all recorded rather than remembered.
		"probe_comp_runtime_reduce", // L11.7 — sum/min/max over a runtime comprehension
		// `probe_comp_folded_iter` (L11.2 — iterating a list the compiler had folded away) is a paid debt and
		// lives in conformanceStandalone now: the escape analysis was deleting the iterable while the only read
		// of it sat in a construct the old walkers did not visit, and a comprehension walks a materialised list
		// (roadmap Gap R.289, ADR 0356; the copy-as-iterable half of the family is Gap R.239, paid beside it).

		// Pinned by the await/return discipline (ADR 0195): the checker now refuses the
		// dishonest async programs, so what is left is the honest one that still disagrees.
		"probe_async_eager", // L7.1 — the compiled backend runs a coroutine at the call
	}
	cases := make([]lang.ConformanceCase, 0, len(names))
	for _, n := range names {
		cases = append(cases, lang.ConformanceCase{
			ID:       "programs/" + n,
			Name:     n + ".gy",
			Source:   readProgramSrc(n),
			Asserted: false, // a probe is a recorded divergence, not conformance surface
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
	"programs/whole_number_builtins": {oracle: lang.OracleNA,
		reason: "floor, ceil and sqrt are this language's builtins; the reference keeps them in the math module, so the CPython leg stops at a NameError on the first line — integration/math_names_test.go runs the same source with `from math import floor, ceil, sqrt` prefixed and asserts that twin against the compiled path",
		ref:    "roadmap Gap R.51 (closed by ADR 0264); docs/language.md § Standard library",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "2\n-3\n3\n-2\n2\n7\n1\n3.0\n1.4142135623730951\n3.5\n[2, 3]\n2\nTrue\n"}}},
	// programs/probe_a_fold_orders_two_built_containers.gy was this row's debt: the fold asked to order two
	// containers the program BUILT had no element-wise ordering, so two different lists raised CPython's own
	// `TypeError: '<' not supported between instances of 'list' and 'list'` at exit 3 where the reference
	// answers [1, 2], and a container folded against a number answered the container's ELEMENT COUNT at exit 0
	// where the reference raises. `@rt_pair_order` walks the elements now — a list lexicographically, element
	// by element, a set by the subset operator, and CPython's sentence for the kinds that do not order, a dict
	// against a copy of itself included — so the program runs and matches on both legs and lives in
	// conformanceStandalone (roadmap Gap R.197, paid by ADR 0318, filed measuring ADR 0316). The relational
	// operators over two built containers are a different road and still owe it: Gap R.97, measured this cycle
	// as Gap R.201 beside this row.
	// programs/probe_pair_bound_name_takes_a_value.gy was this row's `min(n, 3)` line: a builtin's argument
	// kept one word for the value and had nowhere to put the tag the binding carried. A fold is not such a
	// position any more — min/max hand back the winner with its own kind and sum is the `+` fold — so the
	// program runs and matches on both legs and lives in conformanceStandalone (roadmap Gap R.146,
	// ADR 0316). The list element it used to pin is parity surface since ADR 0306, in
	// programs/probe_pair_bound_name_enters_a_container.gy; the `and` row since ADR 0269.
	//
	// `and`/`or` choose an operand (Gap R.147, ADR 0269). The print door can render the choice because the
	// module has one printer that takes a value *and* its kind; the positions that keep one word for a value
	// — `len`'s argument, a binding, a container element, an arithmetic operand — refuse instead of reading
	// the payload alone, which is Gap R.146's missing pair one operator further out.
	"programs/probe_and_or_shapes_the_word_carry": {oracle: lang.OracleDebt,
		reason: "CPython and the reference print 3, d, ['b'], 5.0 and [[1, 2]]; the compiled leg spends exit 1 on the first of them, because a chosen operand that is not a number has no word to travel in outside the print door, where the tag can travel beside the payload",
		ref:    "roadmap Gap R.147 (owed half, owned by L11.1's tagged value word) and Gap R.146; docs/adr/0269, Consequences",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: "requires an inline list/dict/set literal"}}},
	// A text used as an iterable: CPython and the reference answer the characters; the compiled
	// leg spends exit 1 because a comprehension iterable and an iterable builtin argument cannot be a
	// text there at all. Before this row the interpreter answered [] for the comprehensions and the
	// whole string for max/min — a WRONG answer at exit 0, which the pin below now forbids.
	// Gap R.185 / ADR 0298, owner L11.1 and L11.5.
	// An f-string's format spec: CPython and the reference render 3.50, 00007, ff, 1,234.00
	// and the padding; the compiled leg spends exit 1 on the first field it cannot fold, because a
	// spec is applied by the shared engine at compile time and a name or an expression has no
	// constant value to apply it to. Before this row the interpreter printed the PLAIN number for
	// all of them at exit 0 — a wrong answer the two-engine parity could not see.
	// Gap R.186 / ADR 0299, owner L12.8 (with L11.1 for the compiled half).
	// An in-place container mutation: CPython and the reference answer None for `append`,
	// `add`, `discard`, `remove`, `sort` and `reverse`, and answer the item for `pop`; the compiled
	// leg prints None for the ones it lowers and spends exit 1 on a set method over a NAME, because
	// it folds container methods only over a literal written at the call. Before this row the
	// interpreter answered the CONTAINER at exit 0 and the compiled leg emitted `printf(..., i32 )`
	// -- an invalid module, exit 2.
	// Gap R.187 / ADR 0300, owner L12.11 (the compiled half, owed).
	// A container's missing methods: CPython and the reference run all sixteen lines -- the
	// list grows by extend/insert, empties by clear, finds by index; the dict grows by update,
	// answers by pop and setdefault, empties by clear. The compiled leg spends exit 1 on the first
	// one, because it folds a container method only over a literal written at the call. Before this
	// row every line was `no such list method` / `no such dict method` on both legs -- a missing
	// answer rather than a wrong one, which is why no pin could have caught it.
	// Gap R.188 / ADR 0301, owner L12.11 (the compiled half, owed).
	"programs/probe_a_container_has_its_methods": {oracle: lang.OracleDebt,
		reason: "CPython runs the whole program and the reference runs it too -- extend, insert, index, remove, clear on the list and update, pop, setdefault, clear on the dict; the compiled leg spends exit 1 on the first of them, because it folds a container method only over a literal written at the call and the slots a variable holds belong to the runtime",
		ref:    "roadmap Gap R.188 and Gap R.63 (the compiled path' receiver tables, owner L12.11); docs/adr/0301",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: "folds container methods only over a literal"}}},
	"programs/probe_an_in_place_mutation_answers_none": {oracle: lang.OracleDebt,
		reason: "CPython prints None for every in-place mutation and the item for `pop`, and the reference prints the same; the compiled leg prints None for the mutations it lowers and spends exit 1 on `s.remove(1)` over a name, because it folds a container method only over a literal written at the call, where the slots a variable holds belong to the runtime",
		ref:    "roadmap Gap R.187 (the void half, closed) and L12.11 / Gap R.63 (the receiver table, owed); docs/adr/0300",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: "folds container methods only over a literal"}}},
	"programs/probe_a_format_spec_formats": {oracle: lang.OracleDebt,
		reason: "CPython renders every field — 3.50, 00007, ff, 1,234.00, the alignment — and the reference prints the same; the compiled leg spends exit 1 on the first field whose value it cannot read at compile time, because a spec is applied by the shared engine where the digits are known, and a field naming a variable is not known there",
		ref:    "roadmap Gap R.186 and L12.8 (the compiled half, owed); docs/adr/0299",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: "needs a value this backend can read at compile time"}}},
	"programs/probe_a_text_iterates_one_character_at_a_time": {oracle: lang.OracleDebt,
		reason: "CPython prints the characters — ['a', 'b', 'c'] and then c, a, c, Z — and the reference now prints the same; the compiled leg spends exit 1 on the first line, because a comprehension iterable that is a text and an iterable builtin argument that is a text have no lowering there at all, where the for statement has always walked one rune at a time",
		ref:    "roadmap Gap R.185 (the interpreter half, closed) and L11.1 (the compiled half, owed); Gap N.2 owns the code-point measurement; docs/adr/0298",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: "is refused: this backend builds a comprehension over an inline list literal"}}},
	"programs/probe_whole_number_slot_beyond_the_int_word": {oracle: lang.OracleDebt,
		reason: "CPython answers 7000000000 and so does the reference, whose ints are int64; the compiled int word is 32 bits, and the arm raises a catchable OverflowError before the fptosi rather than wrapping a poison truncation into a silent negative — the harness sees the compiled leg's exit class, and integration/numeric_slot_arith_test.go is where the sentence and its catchability are asserted",
		ref:    "roadmap L12.12 (the word's owner); ADR 0264's identical guard for floor/ceil, applied at the new door by ADR 0265",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: "exit status 3"}}},
	"programs/probe_whole_number_beyond_the_int_word": {oracle: lang.OracleNA,
		reason: "the same spelling (the reference's math.floor / math.ceil), and the two engines disagree here on purpose: the evaluator's ints are int64 and answer 3000000000, while the compiled int word is 32 bits and its guard raises `OverflowError: floor: the whole number is beyond the word this backend's int holds (roadmap L12.12)` rather than wrapping a poison `fptosi` into a silent negative — the harness sees the compiled leg's exit class, and integration/math_names_test.go is where the sentence itself and its catchability are asserted",
		ref:    "roadmap Gap R.133 (measured landing ADR 0264); the decision this waits on is L12.12's",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: "exit status 3"}}},
	"programs/probe_predeclared_name_not_callable": {oracle: lang.OracleDebt,
		reason: "CPython evaluates `pow(2, 3)` as 8; here the name is in the checker's predeclared table but not in the call table, so the compiled leg refuses the call — a program the reference runs, spent on exit 1 (ADR 0211's misclassed class)",
		ref:    "roadmap Gap R.136 (measured landing ADR 0264); the same shape Gap R.51 had, and the same two ways out",
		pins: []lang.OraclePin{
			{Backend: "aot", Missing: true, Err: `is not a function this backend can build a call to`},
		}},
	"programs/probe_float_literal_with_exponent": {oracle: lang.OracleDebt,
		reason: "the reference parses `1e18` as a float literal and prints 1e+18; here the exponent marker is not in the number lexer, so the `e18` is read as a name and both legs stop at the same parse error before either engine runs",
		ref:    "roadmap Gap R.135 (measured landing ADR 0264); docs/language.md § Lexical structure → Numeric literals",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: `parse error at 1:8: expected ")"`}}},
	"programs/non_finite_float_constant": {oracle: lang.OracleNA, reason: "the last two lines ask `sqrt`, which the reference keeps in the math module, so the CPython leg stops at a NameError there — the rows above them do match, and integration/math_names_test.go runs this file against `from math import floor, ceil, sqrt` on the compiled path",
		ref:  "roadmap Gap R.134 (closed alongside Gap R.51 by ADR 0264)",
		pins: []lang.OraclePin{{Backend: "aot", Stdout: "inf\ninf\n-inf\nnan\nnan\ninf\nnan\n0.0\n"}}},
	"programs/for_int_count": {oracle: lang.OracleNA,
		reason: "`for i in 4:` treats an integer as a repeat count; CPython raises TypeError ('int' object is not iterable), so the CPython leg stops at the first loop and never sees the rest of the file",
		ref:    "docs/language.md § Control flow (integer repeat count) and roadmap Gap R.14 (ADR 0207)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "0\n1\n2\n3\n0\n10\n20\n100\n101\n102\n10\ndone\n"}}},
	"programs/param_default_order": {oracle: lang.OracleNA,
		reason: "def offset(base, step=10, bonus) is a SyntaxError in CPython (`parameter without a default follows parameter with a default`); here positional binding fills left to right and a keyword call names what it fills, so every parameter is reachable",
		ref:    "roadmap Gap R.11 (closed) and docs/language.md § Parameters and defaults (ADR 0206)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "6\n16\n6\n123\n923\n129\n9\n"}}},
	"programs/data_b": {oracle: lang.OracleNA,
		reason: "{5, 6, 7}[6] — subscripting a set by position is gusty surface; CPython raises TypeError ('set' object is not subscriptable)",
		ref:    "docs/language.md § Dicts & sets (positional set subscript is a gusty extension)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "dkeys 3\ndval 20\nslen 3\nsidx 6\nodd 25\n"}}},
	"programs/features_b": {oracle: lang.OracleNA,
		reason: "{1, 2, 3}[2] — positional set subscript again; the CPython leg stops there and never reaches the rest of the program",
		ref:    "docs/language.md § Dicts & sets (positional set subscript is a gusty extension)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "func 5\nkw 5\nlambda 49\nlen 3\nidx 2\nsum 6\nminmax 1 3\ndict 2 10\nset 3 2\nwhile 18\nif many\nstep 20\nabs 5\nconv 42 1.0 42\n"}}},
	"programs/stdlib": {oracle: lang.OracleNA,
		reason: "string.DIGITS / string.LOWERCASE are this language's stdlib names; Python's string module has no such attributes",
		ref:    "roadmap Phase 2 § on-disk stdlib modules",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "0123456789\nabcdefghijklmnopqrstuvwxyz\n"}}},
	"programs/async_basic": {oracle: lang.OracleNA,
		reason: "await at module scope is a SyntaxError in CPython; the minimal synchronous-coroutine model accepts it (L5.6)",
		ref:    "docs/shared-lowering-spec.md § async / await",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "3\n6\n"}}},
	"programs/async_for": {oracle: lang.OracleNA,
		reason: "async for outside an async function is a SyntaxError in CPython; lowered as a plain for loop here (L5.6)",
		ref:    "docs/shared-lowering-spec.md § async / await",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "2\n4\n6\n"}}},
	"programs/async_multi": {oracle: lang.OracleNA,
		reason: "await at module scope is a SyntaxError in CPython (L5.6)",
		ref:    "docs/shared-lowering-spec.md § async / await",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "11\n21\n"}}},
	"programs/async_effects": {oracle: lang.OracleNA,
		reason: "await at module scope is a SyntaxError in CPython; this is the L7.6 legal-async surface the checker must accept, so what is pinned is that the compiled path agree and nothing was reported",
		ref:    "docs/language.md § Async (the await/return discipline)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "24\n18\n10\n42\n15\n6\n20\n3\n2\n4\n"}}},
	"programs/match_literal": {oracle: lang.OracleNA,
		reason: "Literal[1, 2] is this language's checker surface; plain CPython has no Literal in scope and stops at the annotation",
		ref:    "roadmap Phase 2 § gradual typing; docs/operations.md § gusty check",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "one\ntwo\n"}}},
	// Class patterns (roadmap Gap B, ADR 0235) — the first time this shape was ever run as a whole
	// program. The compiled backend had matched instances carrying no such attribute, lost
	// `Alias = Point` inside a function body (exit 2), and loaded a variable that does not exist for
	// a call pattern.
	"programs/match_classpat": {oracle: lang.OracleNA,
		reason: "a positional class sub-pattern needs __match_args__, which this language does not have — a class pattern binds attributes by the capture name, so CPython stops at the first case with `TypeError: Point() accepts 0 positional sub-patterns`",
		ref:    "docs/language.md § Class patterns (ADR 0235)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "attrs 2 3\nalias 2 3\nmissing none\nkind none\nsubclass 4 5\ncall pattern\nor pattern\nfn alias 6 7\ndynamic 8\nnot a class none\nearly n 1\nlate z 99\n"}}},
	"merged/data": {oracle: lang.OracleNA,
		reason: "inherits data_b's positional set subscript, which CPython rejects",
		ref:    "docs/language.md § Dicts & sets (positional set subscript is a gusty extension)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "dsum 15\ndmin 1\ndmax 5\ndlen 3\ndidx 8\ndkeys 3\ndval 20\nslen 3\nsidx 6\nodd 25\nfsum 21\nloop 45\nbig 66\nabs 42\nconv 100 2.0 7\n"}}},
	"merged/features": {oracle: lang.OracleNA,
		reason: "inherits features_b's positional set subscript, which CPython rejects; features_a's \"abc\"[1] == 98 divergence is pinned on its own row",
		ref:    "roadmap L11.5 (code-point strings)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "sum 10\narith 3 1 5.0 6 -1 5\nfloat 3.5\nstr abc\nslen 5\nsidx b\nsup ABC\nslow abc\nfunc 5\nkw 5\nlambda 49\nlen 3\nidx 2\nsum 6\nminmax 1 3\ndict 2 10\nset 3 2\nwhile 18\nif many\nstep 20\nabs 5\nconv 42 1.0 42\n"}}},

	// ---- measured divergences: valid CPython programs that print something else --
	// `print_args` used to open this list with Gap L.5: its last line let a callee's print fall
	// into the caller's. The statement writes its line as a unit now (ADR 0355), the program is an
	// asserted row, and a paid debt that stayed on the list would fail the build — that is the
	// ledger's promotion rule doing the work the docs used to do.

	// ---- Phase 11 probes: measured, owned, and not yet paid ----------------------
	// A bool handed to a function is no longer a probe: the call site's verdict reaches the
	// parameter and the compiled path prints CPython's answer, so the program lives under the
	// agreeing leg with no ledger row of its own — which is the corpus's way of saying a case is
	// expected to behave exactly like the reference (roadmap Gap R.111, closed this cycle).
	"programs/probe_bool_through_a_call": {oracle: lang.OracleMatch},
	// A bool stored in a container is no longer a probe: it prints CPython's answer on the compiled path
	// and lives in conformanceStandalone (roadmap Gap R.112, ADR 0259).
	"programs/probe_bool_in_a_container":        {oracle: lang.OracleMatch},
	"programs/probe_bool_chosen_by_an_operator": {oracle: lang.OracleMatch},
	"programs/probe_minmax_candidate_unreadable": {oracle: lang.OracleDebt,
		reason: "a min/max fold can name the winner only when it can read every candidate: with a name the compiler has not folded, the run-time select keeps the payload and nothing says it came from a verdict, so the compiled leg prints 1 while the interpreter and CPython print True; the literal-backed candidate in the same program is parity",
		ref:    "roadmap Gap R.124 (measured while closing Gap R.117, ADR 0261)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "1\nTrue\n"}}},
	"programs/probe_ternary_the_test_chose": {oracle: lang.OracleDebt,
		reason: "a ternary whose test the compiler cannot read has no arm known to run, so the conservative rule — a verdict only when both arms are — prints the number: the compiled leg prints 1 on all four lines and the interpreter on two of them, where CPython prints True on all four",
		ref:    "roadmap Gap R.125 (measured while closing Gap R.117, ADR 0261)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "1\n1\n1\n1\n"}}},
	"programs/probe_ternary_container_arms": {oracle: lang.OracleDebt,
		reason: "a ternary whose arms are containers chooses between the container globals themselves — `select i1 %c, i32 @.lst1, i32 @.lst2` — and llc rejects a global in a value position, so the compiled leg exits 2 on a program the interpreter and CPython print in one line (the i32 @.N operand family of Gap R.67, arriving through a ternary)",
		ref:    "roadmap Gap R.128 (measured while landing Gap R.102, ADR 0262; the Gap R.67 / Gap J.6 family)",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: "global variable reference must have pointer type"}}},
	"programs/probe_round_digit_count_kind_unseen": {oracle: lang.OracleDebt,
		reason: "round(x, ndigits) answers with the kind x arrived as, and only where the module can see that kind: of a parameter the call filled with a double the compiled leg takes the i32 road of the call and prints the truncated number, and of a loop variable over a literal list of doubles it prints the element handle",
		ref:    "roadmap Gap R.129 (measured landing ADR 0263; the tagged value word's, roadmap L11.1, as with Gaps R.107–R.110)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "2\n0\n"}}},
	"programs/probe_float_loop_variable_as_number": {oracle: lang.OracleDebt,
		reason: "a for binding over a literal container of doubles is not a number on the compiled leg: v * 2, v + 1 and v / 2 arithmetic on the element handle answer 0, 1 and 0.0 with exit 0, where the interpreter and CPython answer 3.0, 2.5 and 0.75 — the subscript read of the same container is parity, the binding never got ADR 0243's pair",
		ref:    "roadmap Gap R.130 (measured while landing ADR 0263; Gap R.91's family, roadmap L11.1)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "0\n1\n0.0\n"}}},
	"programs/probe_bool_in_a_comprehension": {oracle: lang.OracleDebt,
		reason: "a list comprehension now tags the slot it copies (the fold declines and the runtime builder asks the item), but a set or dict comprehension folds to a compile-time global that has no tag table, so those three lines refuse in words rather than print the number",
		ref:    "roadmap Gap R.116 (measured while closing Gap R.112, ADR 0259)",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: "comprehension of verdicts needs the tagged set builder"}}},
	// A repeated key is paid: the record's dict builders put an entry through the dict's own key
	// lookup instead of appending it, so the comprehension, the literal and item assignment build the
	// same one-entry container CPython does (roadmap Gap R.118, ADR 0260 — the program left the debt
	// ledger, which is ADR 0186's promotion rule).
	"programs/probe_dict_comp_text_key": {oracle: lang.OracleDebt,
		reason: "a dict comprehension the compiler cannot fold spells its key through its runtime builder, which walks integer keys only, so a text key dies with `comprehension key must be constant` while the interpreter and CPython print the same dict; the int spelling of the identical program is parity",
		ref:    "roadmap Gap R.123 (measured while closing Gaps R.118 and R.120, ADR 0260)",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: "comprehension key must be constant"}}},
	"programs/probe_dict_comprehension_duplicate_key": {oracle: lang.OracleMatch},
	"programs/dict_key_rule":                          {oracle: lang.OracleMatch},
	"programs/probe_slot_order_in_a_ternary": {oracle: lang.OracleNA,
		reason: "the ordering CPython itself refuses — an int against a text raises TypeError, so there is no third opinion to compare; what is recorded is that the interpreter raises the same sentence and the compiled backend folds the ternary's condition and prints the true branch, measured on HEAD so it is a gap and not a regression from ADR 0259",
		ref:    "roadmap Gap R.119 (measured while closing Gap R.112, ADR 0259)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "1\n1\n"}}},
	"programs/probe_percent_format": {oracle: lang.OracleMatch},
	// The one argument shape this probe can witness: a front-end refusal ends a program before its first line
	// runs, so a program containing a refusal witnesses only that refusal — which is why the sibling shapes are
	// separate programs in `TestPercentArgumentResidueAtTheCLI` rather than more lines here.
	//
	// `2 ** 70` never becomes a (value, kind) pair: the integer is beyond what this backend's bounded-integer
	// row covers, so the conversion has nothing to render and the door refuses it in words where CPython prints
	// the 22 digits. The siblings named in the program's own comment — the float literal's exponent, a tuple
	// standing where a value goes, a surrogate the program COMPUTED — are refused or answered wrongly for
	// reasons those rows own, not because the formatter has no renderer.
	"programs/probe_percent_arguments_beyond_the_door": {oracle: lang.OracleDebt,
		reason: "CPython prints `1180591620717411303424`; the compiled leg never reaches the formatter, because `2 ** 70` is an integer outside this backend's bounded-integer row and the conversion's argument therefore has no (value, kind) pair to hand over — the refusal names the operand, the missing word and the row that owns the fix, at exit 1",
		ref:    "roadmap Gap R.176 (the integer beyond the word, ADR 0166) beside Gap R.31 (the door that refuses rather than printing the number underneath); the siblings are L11.2 (the float literal's written forms) and L11.3 (a tuple is an argument list, not a value)",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true}}},
	"programs/probe_method_text_field": {oracle: lang.OracleDebt,
		reason: "CPython prints `hi` and `yo`; the compiled leg prints `0` and `1` — the @str_tab POSITION of each text, at exit 0 — the @str_tab POSITION under the text, with nothing raised and nothing refusing. The getter reads a FIELD (`self.v`) whose kind no census carries: `g.exprIsString` asks the intern table of a NAME and has no question for an attribute, so the answer arrives unlabelled and the number printer takes it. The verdict twin answers since ADR 0366 — `methodCensusT.fieldIsVerdict` asks what every write of the field hands it — and the text half is the same reading, unwritten",
		ref:    "roadmap Gap R.319 (a text a method's field hands back), beside Gap R.315 (a method's text parameter, paid by ADR 0366) and Gap R.316 (its verdict answer, paid by the same cycle)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "0\n1\n"}}},
	"programs/sequence_ops": {oracle: lang.OracleDebt,
		reason: "the concat, the repeat and the text repeat are built since ADR 0365 (see programs/sequence_builds, which agrees with the reference); what keeps this program on the ledger is the ORDER of two containers — `[1, 2] < [2]` is True in CPython and the compiled leg refuses it in words, because the tagged order door reads a name the program bound and never an object the compiler built in place, so the compiled leg never completes either way and completes the refusal without spending exit 2",
		ref:    "roadmap Gap R.33 (closed by ADR 0365 for the operations themselves) and the ordering half filed beside it as Gap R.314; Gap R.175's exit-2 half stays closed by ADR 0292",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true}}},
	// Gap R.36 + R.39 closed (ADR 0228): the parity assertion for this program is that the compiled path
	// print `1` and then raise the class CPython raises. The oracle leg itself exits 1 (an uncaught
	// raise), which is why this is not_applicable rather than match -- the CPython leg cannot
	// complete, and the two legs are pinned instead.
	// A conformant row carries no reason and no ref: what the program proves is recorded in the
	// program's own header, in roadmap Gap R.47, and in ADR 0229.
	"programs/runtime_string_ops":    {oracle: lang.OracleMatch},
	"programs/runtime_string_writes": {oracle: lang.OracleMatch},
	"programs/unwritten_slot_trap": {oracle: lang.OracleNA,
		reason: "a local assigned only inside `if c:` with no else: `f(False)` never binds it. CPython raises UnboundLocalError; the compiled leg prints the same first line and raises the same class, which it did not before ADR 0228 -- it printed 0 for the unbound call and exited 0",
		ref:    "roadmap Gap R.36 (definite assignment) + Gap R.39 (which class an unwritten local raises); ADR 0228",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "1\n", Missing: true, Err: "exit status 3"}}},

	// Gap R.68 closed (ADR 0358): the same shape one level into the language. Every arm pair here has a
	// capture the arm that did NOT run would have computed, and the program ends on the read of one.
	// Both legs raise the reference's class from the same line, so CPython has no answer to compare and
	// the row is not_applicable with both legs pinned -- which is the whole point: the compiled leg used
	// to exit 0 having printed a word from the capture's slot.
	"programs/match_binding_edges": {oracle: lang.OracleNA,
		reason: "a `match` capture whose arm did not run is never bound: `case [a]` over 5 decides the arm and binds nothing, and reading `a` afterwards raises UnboundLocalError on both legs. The compiled leg used to answer 0 and exit 0, because the store sat where the pattern was lowered rather than in the arm the branch entered",
		ref:    "roadmap Gap R.68 (the capture-binding edge left by Gap B) + Gap R.306 (a sequence pattern is a kind test); ADR 0358",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "caught\n4\n30\n0\n-1\n7\n3\n8\n", Missing: true, Err: "exit status 3"}, {Backend: "python", Stdout: "caught\n4\n30\n0\n-1\n7\n3\n8\n", Missing: true, Err: "exit status 1"}}},

	// Gap R.48 (found while closing R.36, ADR 0228's probe pass).
	"programs/probe_global_statement": {oracle: lang.OracleNA,
		reason: "there is no `global` statement: CPython reads `global gz` as a declaration and the read afterwards raises NameError, while the compiled backend parses `global gz` as the *expression* `global gz` and refuses the program for having no binding for `global` — it disagrees with the reference on a construct every Python reader expects",
		ref:    "roadmap Gap R.48 (`global` is not in the language); Gap R.36 (the probe pass that found it)",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true}}},

	"programs/probe_operand_types": {oracle: lang.OracleDebt,
		reason: "the interpreter and CPython agree on all seven handler lines, but the compiled backend answers `1 + None` with a value instead of raising and refuses the rest at compile time, so the compiled leg never completes",
		ref:    "roadmap Gap R.27 (operand kinds unchecked in the compiled backend); ADR 0215",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true}}},

	"programs/probe_builtin_traps_untyped": {oracle: lang.OracleDebt,
		reason: "the interpreter and CPython agree on all five handler lines, but the compiled backend answers the missing-attribute case with a value instead of raising (Gap R.19) and refuses the others at compile time with prose diagnostics, so the compiled leg never completes",
		ref:    "roadmap Gap R.25 (typed built-in traps) and Gap R.19 (attribute answers 0 in AOT); ADR 0214",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true}}},
	"programs/probe_mixed_return_value": {oracle: lang.OracleDebt,
		reason: "a function whose return paths have different types is lowered as returning one of them, so the compiled caller reads the integer as an interned-string index and prints (null) where the interpreter and CPython print 3 — silently, with exit 0",
		ref:    "roadmap Gap R.22 (mixed return types; ADR 0213 refiled the original Gap R.21 reading, which blamed try/except)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "(null)\n"}}},
	"programs/probe_math_const": {oracle: lang.OracleNA,
		reason: "Python spells these math.pi / math.e, so the source is not a CPython program — the row stays not-applicable for the spelling alone: what the twin asserts is the two numbers below, and integration/module_const_test.go asks the reference for its own spelling beside them. The fold's lost float type — the answer this row pinned at 3 and 2 compiled — is paid (roadmap L11.6, ADR 0272)",
		ref:    "roadmap L11.6 (a stdlib constant keeps its type); closed 2026-10-05",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "3.141592653589793\n2.718281828459045\n"}}},
	"programs/probe_enumerate": {oracle: lang.OracleDebt,
		reason: "the tuple half of this program is answered — the pairs print `(0, 'a')`, `(1, 3)` with the reference's brackets (L11.3, ADR 0361) — and what the compiled leg refuses is the walk to them: no builtin builds an `enumerate`/`zip` pair sequence, and `list(<container>)` refuses to copy a container it cannot name, so the first line never prints",
		ref:    "roadmap L11.7 (enumerate/zip, the container builtins; Gap R.174's container-method door) beside L11.3 (paid: the pair is a value the compiled backend prints, indexes, compares and hashes)",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true}}},
	// Five lines of CPython parity on the record. The compiled leg answers the two trap lines
	// (`abs(twice)`, `-twice`) byte-for-byte and refuses only the higher-order body — `apply` calls a
	// *parameter* — which is L11.7's limit rather than this row's name lookup, so the row is a debt with
	// one leg whole and one leg refused (roadmap Gap R.150, Gap R.151, ADR 0283).
	"programs/probe_a_function_as_a_value": {oracle: lang.OracleDebt,
		reason: "the record holds CPython's five lines from the retired engine; the compiled leg refuses the body that calls a parameter (`\"f\" is called but is not a function this backend can build a call to`), the higher-order limit L11.7 records",
		ref:    "roadmap L11.7 (functions are values that compile); Gap R.150, Gap R.151, Gap R.153 (ADR 0283)",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true}}},
	"programs/probe_comp_runtime_reduce": {oracle: lang.OracleDebt,
		reason: "sum over a comprehension whose elements are computed at runtime has no compile-time element set to fold; it refuses rather than add up nothing and answer 0",
		ref:    "roadmap L11.7 (functions are values that compile)",
		pins:   []lang.OraclePin{{Backend: "aot", Missing: true, Err: "runtime reduction"}}},
	"programs/probe_async_eager": {oracle: lang.OracleNA,
		reason: "the compiled backend lowers an async call as a call, so `work(1)` prints at the call where the record leg recorded the print at the await; CPython rejects the program outright (module-scope await)",
		ref:    "roadmap L7.6a (deferred coroutines in codegen)",
		pins:   []lang.OraclePin{{Backend: "aot", Stdout: "effect 1\nbetween\n2\n"}}},
}
