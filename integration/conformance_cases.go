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
		// floor/ceil/sqrt answer whole numbers and a float respectively, on both backends (Gap R.51,
		// ADR 0264); the file is gusty's spelling of the reference's math-module names, so the ledger
		// records it and the CLI test runs the twin.
		"whole_number_builtins",
		// The number use of a slot whose kind only the run time can describe: a list of lists, and
		// arithmetic on what comes out of one (roadmap L11.1's last clause, ADR 0265).
		"numeric_slot_arith",
		// `and`/`or` hand back the operand the test chose, on both backends: `2 and 3` is `3`, `"" or "d"`
		// is the text `d`, `[1] and [2]` is `[2]`, and `1 or True` stays the number `1` (roadmap Gap R.147,
		// ADR 0269). The shapes whose answer needs the kind to travel with it are filed beside it.
		"and_or_answer_like_python",
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
		// a name — whole on both engines. `g(1, 2)` had printed `2` and `g()` had printed `0`, both at exit 0
		// (roadmap Gap R.168, ADR 0284).
		"probe_asked_how_many_arguments",
		// A number the body computed out of its own parameter keeps the kind the argument arrived with:
		// eight lines — add-one, double over a float and an int, an expression with arithmetic arms, and a
		// float default — whole on both engines. Each printed a truncated integer at exit 0 before ADR 0285
		// (`1` for `1.1`, `0` for `0.2`, `-1` for `-0.9`, `3` for `5.0` — roadmap Gap R.169).
		"probe_a_number_bound_from_a_parameter",
		// A rendering the body bound to a NAME is the same string return as `return str(v)`: `s = str(v)` /
		// `return s`, `repr` through a name, and `"x" + str(v)` all printed the interned index (`0`, `0`, `2`)
		// at exit 0 where the reference and the interpreter print `3`, `3`, `x3` (roadmap Gap R.170, ADR 0286).
		"probe_a_rendering_bound_to_a_name",
		// Comparison chains: `1 > 2 < 3` is Python's construct — `1 > 2` AND `2 < 3`, the middle operand
		// read once — and this grammar parsed it as `(1 > 2) < 3`, comparing an int against a boolean,
		// which this front end answers rather than refusing. Four of these six lines printed the wrong
		// verdict at exit 0 on BOTH engines, agreeing with each other and disagreeing with the reference
		// (roadmap L12.1 / Gap R.53, ADR 0288).
		"probe_comparison_chains",
		// A ternary with text arms used to print the @str_tab POSITION on the compiled leg:
		// `print("a" if c > 0 else "b")` said `0` where the reference prints `a`. Gap R.127's
		// recorded debt, promoted to parity by ADR 0290 (ADR 0261's rule: a promoted probe's
		// exit-code pin leaves the debt table with the probe).
		"probe_ternary_text_arms",
		// A text predicate — startswith, endswith, isdigit, isalpha and their siblings — answers a
		// VERDICT in both backends, but the print road never asked about a method call, so each line
		// printed the 0/1 WORD it holds: True as `1`, False as `0`, on both engines (Gap R.172, ADR 0289).
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
		// for OUR bug. Every line here is a pair the reference raises for, so both engines now raise the
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
		// `%` is the remainder for numbers on both engines — ints, negatives, doubles, the same body over a
		// pair-marked parameter, and a float compared to a text — thirteen lines pinned beside the refusal
		// that keeps the *text*-left spelling from answering a number (roadmap Gap R.165, ADR 0282).
		"probe_remainder_and_percent",
		// `abs` answers with its operand's kind: the numbers keep answering (`abs(-3.5)` is `3.5`) and every
		// operand without a sign raises the reference's own sentence naming its kind — `str`, `NoneType`,
		// `list`, `dict`, `set`, and an instance's own class — catchably, on both backends (roadmap Gap
		// R.140, ADR 0271). The last two lines are the rebinding the door depends on: a name's *latest*
		// binding decides the kind the raise names (Gap R.145, ADR 0270).
		"abs_names_its_kind",
		// The same arithmetic one statement earlier — bound to a name before it is printed. The pair
		// the print door already took now travels through the binding, so `n = xs[0][0] * 2` and
		// `print(n)` answer 14 on all three engines (roadmap Gap R.138, ADR 0267).
		"probe_arith_result_bound_to_a_name",
		// …and the same name read back as a number: an operand, an ordering, a `while` head, a condition,
		// `str`, an f-string field, the target of `+=`. Fifteen lines, three engines, the same bytes
		// (roadmap Gap R.143, ADR 0268).
		"probe_pair_bound_name_as_a_number",
		// The same slot read handed to a function: the argument arrives as the (payload, tag) pair and the
		// answer's kind comes back in the word the callee stored beside its own return, so
		// `print(twice(xs[0][0]))` is `14` on all three engines (roadmap Gap R.139, ADR 0273).
		"probe_slot_read_handed_to_a_function",
		// An int that meets `/=`, and an int handed a double by a later assignment, leave the variable
		// holding a float: the double goes into a float box and the name is bound to the (payload, tag)
		// pair, so print, an operation, a comparison, a condition, `str` and a `while` head all ask the
		// tag — and the variable beside it keeps its own number, which the `store double` into the
		// four-byte slot could not promise (roadmap L11.6, Gap P.1, Gap R.155, ADR 0274).
		"probe_int_state_becomes_float",
		// The unary minus names its operand's kind on both engines, and every shape the reference stops on
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
		// A bool is a value: `print(True)` writes True, `print(1 == 1)` writes True, and
		// --json reports its type as bool. Both backends print CPython's answer on every
		// line, which is what moved this program out of the probe list (roadmap L11.1
		// step 2, ADR 0257); the shapes that still print the number a bool is stored as
		// are probes of their own below.
		"probe_bool_value",
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
		// The rendering pair, three engines on one source: every value form written twice, once by
		// str() and once by repr(), from the one renderer print uses. Both backends print CPython's
		// answer on every line, which is what moved this program out of the probe list; the forms
		// the compiled backend cannot name are refusals it makes the same way on both halves
		// (roadmap L11.2, ADR 0258, closing Gap L.2).
		"probe_render_pair",
		// A bool stored in a container, three engines on one source: the slot carries a bool tag, so
		// the list, the dict and the str()/repr() of both print `[True, 1]` and `{'k': True}` like
		// CPython — and the numeric questions still answer as the number (True + 1, [True] == [1],
		// d[True]) on every leg (roadmap Gap R.112, ADR 0259; the program left the debt ledger with
		// that row, which is the promotion rule of ADR 0186).
		"probe_bool_in_a_container",
		// The operator that chooses an operand, three engines on one source: `max([True, 1])` is the
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
		// length of 1 on all three engines now, where the interpreter used to print `{1: 2, 1: 2}`
		// and count the pair (roadmap Gap R.118, ADR 0260 — promoted out of the debt ledger).
		"probe_dict_comprehension_duplicate_key",
		// The ordering of those same slots, three engines on one source: `<`, `<=`, `>`, `>=` of a slot
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
		// An f-string's format spec and conversion. The interpreter answers every field the way the
		// reference does; the compiled leg answers the constant ones and refuses a field it cannot
		// read at compile time. Before this the spec was cut off at parse time and both engines
		// printed the plain value at exit 0 -- parity could not see it because the engines agreed.
		// Gap R.186 / L12.8, ADR 0298's sibling ADR 0299.
		"probe_a_format_spec_formats",
		// A text used as an iterable — a comprehension over one, and max/min of one. The interpreter
		// answered [] and the whole string; it now answers the characters the reference answers, and the
		// compiled leg's refusal is RECORDED rather than pinned as a limit. Gap R.185 / ADR 0298.
		"probe_a_text_iterates_one_character_at_a_time",
		// A bool handed to a function and printed there. The parameter's slot holds the 1 the
		// comparison produced and nothing says it was ever a verdict, so both backends print 1
		// where CPython prints True (roadmap Gap R.111, filed by ADR 0257).
		"probe_bool_through_a_call",
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
		// floor/ceil answer a whole number on both engines now (Gap R.51, ADR 0264). What stays filed is
		// the answer past the compiled int word: the evaluator's int64 answers CPython's number and the
		// compiled guard raises an OverflowError naming L12.12, rather than wrapping in silence
		// (roadmap Gap R.133).
		"probe_whole_number_beyond_the_int_word",
		// The negation of a text is answered by both engines the way the reference answers it — the shape
		// this row filed (Gap R.137) is parity surface now, in programs/negation_names_the_kind.gy
		// (roadmap L11.1, ADR 0266).
		// Arithmetic whose whole-number answer will not fit the compiled int word: the evaluator's int64
		// answers CPython's number, the compiled guard raises a catchable OverflowError naming L12.12
		// rather than truncate a double through an i32 (ADR 0264's lesson at the new door).
		"probe_whole_number_slot_beyond_the_int_word",
		// The arithmetic the print position answers, one statement earlier, and the number positions that
		// read the bound name back, are parity surface: both programs live in conformanceStandalone
		// (roadmap Gaps R.138 and R.143, ADRs 0267 and 0268). What stays filed beside them is the same
		// value handed to a position that keeps one word for it, and the unpacking that has not taken the
		// pair (Gaps R.146, R.144); the same value handed to a function is parity surface since ADR 0273.
		"probe_pair_bound_name_takes_a_value",
		"probe_pair_from_a_tuple_unpack",
		// A double written with an exponent — the spelling a scientific value arrives in — does not lex:
		// both engines stop at a parse error where the reference parses `1e18` as 10^18 (roadmap Gap
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
		// A negative zero the *compiler* wrote loses its sign on the way to the printer (the folded
		// constant is materialised with `fadd double 0.0, …`), while one the program computed keeps
		// it (roadmap Gap R.132, ADR 0263).
		"probe_negative_zero_constant",
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
		// CPython and the interpreter raise TypeError, the compiled backend folds the condition and
		// prints the true branch. The int spelling predates this cycle; the bool spelling is what
		// found it (roadmap Gap R.119).
		"probe_slot_order_in_a_ternary",
		"probe_tuple", // L11.3 — no tuple lowering at all

		"probe_mixed_return_value",    // Gap R.22 — returns of differing types share one lowering
		"probe_builtin_traps_untyped", // Gap R.25 — a trap with no class cannot be caught
		"sequence_ops",
		"probe_operand_types",    // Gap R.26 — an operator applied to the wrong operands
		"probe_percent_format",   // Gap R.31 — no `%` string formatting; both legs refuse
		"probe_global_statement", // Gap R.48 — no `global` statement; all three engines differ
		"unwritten_slot_trap",    // Gap R.36 + R.39 — an unwritten local traps with the right class (ADR 0228)
		"probe_math_const",       // L11.6 — a stdlib float constant folds to int
		"probe_enumerate",        // L11.7 + L11.3 — enumerate/zip/reversed yield tuples
		"probe_fn_value",         // L11.7 — a lambda cannot be called through a parameter
		"probe_fn_name",
		// A declared name is not a variable, and a function is not a number: reading a `def`'d name is no
		// longer a NameError, so a function flows through a parameter on the interpreter, and the numeric
		// door raises CPython's `'function'`/`'module'` sentence instead of handing `llc` a load of a slot
		// no `def` allocated (roadmap Gap R.150, Gap R.151, Gap R.153, ADR 0283).
		"probe_a_function_as_a_value",
		// How many arguments there were is one question, asked on the road every caller shares: eleven calls
		// the reference answers — defaults, keywords out of order, recursion, a method, a lambda read out of
		// a name — whole on both engines. `g(1, 2)` had printed `2` and `g()` had printed `0`, both at exit 0
		// (roadmap Gap R.168, ADR 0284).
		// How many arguments there were is one question, asked on the road every caller shares: eleven calls
		"probe_print_atomic", // Gap L.5 — print writes while it evaluates
		// Found by the boring-program sweep (ADR 0190): the tutorial-shaped programs nobody
		// probed, twelve of them, five divergences.

		// Pinned by the runtime-comprehension work (ADR 0192): two honest refusals and one
		// llc rejection that is a compiler bug, all recorded rather than remembered.
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
	// ---- three-engine parity added this round -----------------------------------
	// (rows below are `oracle: match` by default; see the drift tests)

	// ---- gusty-only surface: CPython cannot run the program at all --------------
	"programs/whole_number_builtins": {oracle: lang.OracleNA,
		reason: "floor, ceil and sqrt are this language's builtins; the reference keeps them in the math module, so the CPython leg stops at a NameError on the first line — integration/math_names_test.go runs the same source with `from math import floor, ceil, sqrt` prefixed and asserts that twin against both engines",
		ref:    "roadmap Gap R.51 (closed by ADR 0264); docs/language.md § Standard library",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "2\n-3\n3\n-2\n2\n7\n1\n3.0\n1.4142135623730951\n3.5\n[2, 3]\n2\nTrue\n"}, {Backend: "aot", Stdout: "2\n-3\n3\n-2\n2\n7\n1\n3.0\n1.4142135623730951\n3.5\n[2, 3]\n2\nTrue\n"}}},
	"programs/probe_pair_bound_name_takes_a_value": {oracle: lang.OracleDebt,
		reason: "CPython prints 14, 3, [14], 3 and the interpreted leg now prints the same four — its `and` chose the operand the reference hands back since Gap R.147 closed — while the compiled leg spends exit 1 on the first line, because an argument, a list element and a builtin's argument each keep one word for the value and have nowhere to put the tag the binding carried",
		ref:    "roadmap Gap R.146 (measured landing ADR 0268); the `and` row this program also pinned is closed by docs/adr/0269",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "14\n3\n[14]\n3\n"}, {Backend: "aot", Missing: true, Err: "holds the answer of arithmetic over a slot"}}},
	// `and`/`or` choose an operand (Gap R.147, ADR 0269). The print door can render the choice because the
	// module has one printer that takes a value *and* its kind; the positions that keep one word for a value
	// — `len`'s argument, a binding, a container element, an arithmetic operand — refuse instead of reading
	// the payload alone, which is Gap R.146's missing pair one operator further out.
	"programs/probe_and_or_shapes_the_word_carry": {oracle: lang.OracleDebt,
		reason: "CPython and the interpreted leg print 3, d, ['b'], 5.0 and [[1, 2]]; the compiled leg spends exit 1 on the first of them, because a chosen operand that is not a number has no word to travel in outside the print door, where the tag can travel beside the payload",
		ref:    "roadmap Gap R.147 (owed half, owned by L11.1's tagged value word) and Gap R.146; docs/adr/0269, Consequences",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "3\nd\n['b']\n5.0\n[[1, 2]]\n"}, {Backend: "aot", Missing: true, Err: "requires an inline list/dict/set literal"}}},
	// A text used as an iterable: CPython and the interpreted leg answer the characters; the compiled
	// leg spends exit 1 because a comprehension iterable and an iterable builtin argument cannot be a
	// text there at all. Before this row the interpreter answered [] for the comprehensions and the
	// whole string for max/min — a WRONG answer at exit 0, which the pin below now forbids.
	// Gap R.185 / ADR 0298, owner L11.1 and L11.5.
	// An f-string's format spec: CPython and the interpreted leg render 3.50, 00007, ff, 1,234.00
	// and the padding; the compiled leg spends exit 1 on the first field it cannot fold, because a
	// spec is applied by the shared engine at compile time and a name or an expression has no
	// constant value to apply it to. Before this row the interpreter printed the PLAIN number for
	// all of them at exit 0 — a wrong answer the two-engine parity could not see.
	// Gap R.186 / ADR 0299, owner L12.8 (with L11.1 for the compiled half).
	"programs/probe_a_format_spec_formats": {oracle: lang.OracleDebt,
		reason: "CPython renders every field — 3.50, 00007, ff, 1,234.00, the alignment — and the interpreted leg prints the same; the compiled leg spends exit 1 on the first field whose value it cannot read at compile time, because a spec is applied by the shared engine where the digits are known, and a field naming a variable is not known there",
		ref:    "roadmap Gap R.186 and L12.8 (the compiled half, owed); docs/adr/0299",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "3.50\n4\n2\n00007\n-0004\nff\n1,234.00\n   3.5|\n3.5   |\n**3.5**|\n25.00%\n3.142e+00\n2\n2.0\n7\n     3.5|\n00007\n0008\npi=3.14 e=2.72\n"}, {Backend: "aot", Missing: true, Err: "needs a value this backend can read at compile time"}}},
	"programs/probe_a_text_iterates_one_character_at_a_time": {oracle: lang.OracleDebt,
		reason: "CPython prints the characters — ['a', 'b', 'c'] and then c, a, c, Z — and the interpreted leg now prints the same; the compiled leg spends exit 1 on the first line, because a comprehension iterable that is a text and an iterable builtin argument that is a text have no lowering there at all, where the for statement has always walked one rune at a time",
		ref:    "roadmap Gap R.185 (the interpreter half, closed) and L11.1 (the compiled half, owed); Gap N.2 owns the code-point measurement; docs/adr/0298",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "['a', 'b', 'c']\n['a', 'c']\n['aa', 'bb']\n[]\n4\n['a', 'b', 'c']\nc\na\nc\nZ\n3\n1\n5\na\nb\nc\n"}, {Backend: "aot", Missing: true, Err: "comprehension iterable must be an inline list literal, range(), or a container variable"}}},
	"programs/probe_pair_from_a_tuple_unpack": {oracle: lang.OracleDebt,
		reason: "CPython prints 8 and the interpreted leg prints 8; the compiled leg spends exit 1 on the unpacking, because a tuple target binds its names through the ordinary numeric road, which refuses the slot it cannot see into — the plain assignment takes the pair road since ADR 0267 and the unpacking does not",
		ref:    "roadmap Gap R.144 (measured landing ADR 0267); docs/adr/0267, Consequences",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "8\n"}, {Backend: "aot", Missing: true, Err: "index cannot reach into xs's slots"}}},
	"programs/probe_whole_number_slot_beyond_the_int_word": {oracle: lang.OracleDebt,
		reason: "CPython answers 7000000000 and so does the interpreted leg, whose ints are int64; the compiled int word is 32 bits, and the arm raises a catchable OverflowError before the fptosi rather than wrapping a poison truncation into a silent negative — the harness sees the compiled leg's exit class, and integration/numeric_slot_arith_test.go is where the sentence and its catchability are asserted",
		ref:    "roadmap L12.12 (the word's owner); ADR 0264's identical guard for floor/ceil, applied at the new door by ADR 0265",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "7000000000\n"}, {Backend: "aot", Missing: true, Err: "exit status 3"}}},
	"programs/probe_whole_number_beyond_the_int_word": {oracle: lang.OracleNA,
		reason: "the same spelling (the reference's math.floor / math.ceil), and the two engines disagree here on purpose: the evaluator's ints are int64 and answer 3000000000, while the compiled int word is 32 bits and its guard raises `OverflowError: floor: the whole number is beyond the word this backend's int holds (roadmap L12.12)` rather than wrapping a poison `fptosi` into a silent negative — the harness sees the compiled leg's exit class, and integration/math_names_test.go is where the sentence itself and its catchability are asserted",
		ref:    "roadmap Gap R.133 (measured landing ADR 0264); the decision this waits on is L12.12's",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "3000000000\n3000000000\n"}, {Backend: "aot", Missing: true, Err: "exit status 3"}}},
	"programs/probe_predeclared_name_not_callable": {oracle: lang.OracleDebt,
		reason: "CPython evaluates `pow(2, 3)` as 8; here the name is in the checker's predeclared table and in neither engine's call table, so the interpreted leg traps NameError and the compiled leg refuses the call — a program the reference runs, spent on exit 3 and exit 1 (ADR 0211's misclassed class)",
		ref:    "roadmap Gap R.136 (measured landing ADR 0264); the same shape Gap R.51 had, and the same two ways out",
		pins: []lang.OraclePin{
			{Backend: "interpreter", Missing: true, Err: "name 'pow' is not defined"},
			{Backend: "aot", Missing: true, Err: `unsupported call "pow"`},
		}},
	"programs/probe_float_literal_with_exponent": {oracle: lang.OracleDebt,
		reason: "the reference parses `1e18` as a float literal and prints 1e+18; here the exponent marker is not in the number lexer, so the `e18` is read as a name and both legs stop at the same parse error before either engine runs",
		ref:    "roadmap Gap R.135 (measured landing ADR 0264); docs/language.md § Lexical structure → Numeric literals",
		pins:   []lang.OraclePin{{Backend: "interpreter", Missing: true, Err: `parse error at 1:8: expected ")"`}, {Backend: "aot", Missing: true, Err: `parse error at 1:8: expected ")"`}}},
	"programs/non_finite_float_constant": {oracle: lang.OracleNA, reason: "the last two lines ask `sqrt`, which the reference keeps in the math module, so the CPython leg stops at a NameError there — the rows above them do match, and integration/math_names_test.go runs this file against `from math import floor, ceil, sqrt` on both engines",
		ref:  "roadmap Gap R.134 (closed alongside Gap R.51 by ADR 0264)",
		pins: []lang.OraclePin{{Backend: "interpreter", Stdout: "inf\ninf\n-inf\nnan\nnan\ninf\nnan\n0.0\n"}, {Backend: "aot", Stdout: "inf\ninf\n-inf\nnan\nnan\ninf\nnan\n0.0\n"}}},
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

	// ---- Phase 11 probes: measured, owned, and not yet paid ----------------------
	"programs/probe_bool_through_a_call": {oracle: lang.OracleDebt,
		reason: "a bool passed to a function prints as the 1 its parameter's slot holds: the print site sees a name, and nothing travels with that name saying it was a verdict",
		ref:    "roadmap Gap R.111 (filed by ADR 0257, the bools-are-values cycle)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "1\n1\n"}, {Backend: "aot", Stdout: "1\n1\n"}}},
	// A bool stored in a container is no longer a probe: it prints CPython's answer on both backends
	// and lives in conformanceStandalone (roadmap Gap R.112, ADR 0259).
	"programs/probe_bool_in_a_container":        {oracle: lang.OracleMatch},
	"programs/probe_bool_chosen_by_an_operator": {oracle: lang.OracleMatch},
	"programs/probe_minmax_candidate_unreadable": {oracle: lang.OracleDebt,
		reason: "a min/max fold can name the winner only when it can read every candidate: with a name the compiler has not folded, the run-time select keeps the payload and nothing says it came from a verdict, so the compiled leg prints 1 while the interpreter and CPython print True; the literal-backed candidate in the same program is parity",
		ref:    "roadmap Gap R.124 (measured while closing Gap R.117, ADR 0261)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "True\nTrue\n"}, {Backend: "aot", Stdout: "1\nTrue\n"}}},
	"programs/probe_ternary_the_test_chose": {oracle: lang.OracleDebt,
		reason: "a ternary whose test the compiler cannot read has no arm known to run, so the conservative rule — a verdict only when both arms are — prints the number: the compiled leg prints 1 on all four lines and the interpreter on two of them, where CPython prints True on all four",
		ref:    "roadmap Gap R.125 (measured while closing Gap R.117, ADR 0261)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "True\n1\nTrue\n1\n"}, {Backend: "aot", Stdout: "1\n1\n1\n1\n"}}},
	"programs/probe_ternary_container_arms": {oracle: lang.OracleDebt,
		reason: "a ternary whose arms are containers chooses between the container globals themselves — `select i1 %c, i32 @.lst1, i32 @.lst2` — and llc rejects a global in a value position, so the compiled leg exits 2 on a program the interpreter and CPython print in one line (the i32 @.N operand family of Gap R.67, arriving through a ternary)",
		ref:    "roadmap Gap R.128 (measured while landing Gap R.102, ADR 0262; the Gap R.67 / Gap J.6 family)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[1, 2]\n"}, {Backend: "aot", Missing: true, Err: "global variable reference must have pointer type"}}},
	"programs/probe_round_digit_count_kind_unseen": {oracle: lang.OracleDebt,
		reason: "round(x, ndigits) answers with the kind x arrived as, and only where the module can see that kind: of a parameter the call filled with a double the compiled leg takes the i32 road of the call and prints the truncated number, and of a loop variable over a literal list of doubles it prints the element handle",
		ref:    "roadmap Gap R.129 (measured landing ADR 0263; the tagged value word's, roadmap L11.1, as with Gaps R.107–R.110)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "2.35\n2.35\n"}, {Backend: "aot", Stdout: "2\n0\n"}}},
	"programs/probe_float_loop_variable_as_number": {oracle: lang.OracleDebt,
		reason: "a for binding over a literal container of doubles is not a number on the compiled leg: v * 2, v + 1 and v / 2 arithmetic on the element handle answer 0, 1 and 0.0 with exit 0, where the interpreter and CPython answer 3.0, 2.5 and 0.75 — the subscript read of the same container is parity, the binding never got ADR 0243's pair",
		ref:    "roadmap Gap R.130 (measured while landing ADR 0263; Gap R.91's family, roadmap L11.1)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "3.0\n2.5\n0.75\n"}, {Backend: "aot", Stdout: "0\n1\n0.0\n"}}},
	"programs/probe_negative_zero_constant": {oracle: lang.OracleDebt,
		reason: "the sign of a negative zero the compiler wrote is lost before the module exists: the emitter materialises a folded float constant with `fadd double 0.0, <const>` (thirteen sites in pkg/lang/codegen.go), and IEEE answers -0.0 + +0.0 with +0.0 — so a literal -0.0, and a name bound to one, print 0.0, while a product the target multiplies and either road of round(-0.5, 0) print -0.0; the runtime formatter (rt_fmt_double) handles the sign and is never given the value",
		ref:    "roadmap Gap R.132 (measured while landing ADR 0263, against the pre-cycle binary; the two-renderers-one-rule shape of ADR 0236; ADR 0263's own fold shipped with that fadd for one cycle and no longer does)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "-0.0\n-0.0\n-0.0\n-0.0\n"}, {Backend: "aot", Stdout: "0.0\n0.0\n-0.0\n-0.0\n"}}},
	"programs/probe_bool_in_a_comprehension": {oracle: lang.OracleDebt,
		reason: "a list comprehension now tags the slot it copies (the fold declines and the runtime builder asks the item), but a set or dict comprehension folds to a compile-time global that has no tag table, so those three lines refuse in words rather than print the number",
		ref:    "roadmap Gap R.116 (measured while closing Gap R.112, ADR 0259)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[True, 1, 1]\n{True}\n{1: True}\n{True: 1}\n"}, {Backend: "aot", Missing: true, Err: "comprehension of verdicts needs the tagged set builder"}}},
	// A repeated key is paid: the interpreter's dict builders put an entry through the dict's own key
	// lookup instead of appending it, so the comprehension, the literal and item assignment build the
	// same one-entry container CPython does (roadmap Gap R.118, ADR 0260 — the program left the debt
	// ledger, which is ADR 0186's promotion rule).
	"programs/probe_dict_comp_text_key": {oracle: lang.OracleDebt,
		reason: "a dict comprehension the compiler cannot fold spells its key through its runtime builder, which walks integer keys only, so a text key dies with `comprehension key must be constant` while the interpreter and CPython print the same dict; the int spelling of the identical program is parity",
		ref:    "roadmap Gap R.123 (measured while closing Gaps R.118 and R.120, ADR 0260)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "{'k': 3}\n3 1\n"}, {Backend: "aot", Missing: true, Err: "comprehension key must be constant"}}},
	"programs/probe_dict_comprehension_duplicate_key": {oracle: lang.OracleMatch},
	"programs/dict_key_rule":                          {oracle: lang.OracleMatch},
	"programs/probe_slot_order_in_a_ternary": {oracle: lang.OracleNA,
		reason: "the ordering CPython itself refuses — an int against a text raises TypeError, so there is no third opinion to compare; what is recorded is that the interpreter raises the same sentence and the compiled backend folds the ternary's condition and prints the true branch, measured on HEAD so it is a gap and not a regression from ADR 0259",
		ref:    "roadmap Gap R.119 (measured while closing Gap R.112, ADR 0259)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Missing: true, Err: "not supported between instances of 'int' and 'str'"}, {Backend: "aot", Stdout: "1\n1\n"}}},
	"programs/probe_tuple": {oracle: lang.OracleDebt,
		reason: "a tuple literal has no AOT lowering at all (unsupported expression *lang.Tuple) and the interpreter renders one as a list",
		ref:    "roadmap L11.3 (tuples are values)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[1, 2, 3]\n2\n3\n4\n5\n[1, 2]\n"}, {Backend: "aot", Missing: true}}},

	"programs/sequence_ops": {oracle: lang.OracleDebt,
		reason: "the interpreter and CPython agree on all thirteen lines, but the compiled backend has no sequence lowering: it refuses `str * int` (an honest refusal, Gap R.82) and refuses list concatenation and repeat in words (Gap R.175) where it used to emit a module llc rejects — \"global variable reference must have pointer type\" — so the compiled leg never completes either way, and now completes the refusal without spending exit 2",
		ref:    "roadmap Gap R.33 (sequence operations in codegen, same signature as Gap R.16) and Gap R.175 (the exit-2 half, ADR 0292); ADR 0215",
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
		reason: "Python spells these math.pi / math.e, so the source is not a CPython program — the row stays not-applicable for the spelling alone: what the twin asserts is the two numbers below, and integration/module_const_test.go asks the reference for its own spelling beside them. The fold's lost float type — the answer this row pinned at 3 and 2 compiled — is paid (roadmap L11.6, ADR 0272)",
		ref:    "roadmap L11.6 (a stdlib constant keeps its type); closed 2026-10-05",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "3.141592653589793\n2.718281828459045\n"}, {Backend: "aot", Stdout: "3.141592653589793\n2.718281828459045\n"}}},
	"programs/probe_enumerate": {oracle: lang.OracleDebt,
		reason: "enumerate/zip produce pairs the interpreter renders as lists (tuples again), and list(<container>) copies are refused in AOT",
		ref:    "roadmap L11.7 + L11.3 (tuples are values)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[[0, 'a'], [1, 'b']]\n[[1, 3], [2, 4]]\n[3, 2, 1]\n"}, {Backend: "aot", Missing: true}}},
	"programs/probe_fn_value": {oracle: lang.OracleDebt,
		reason: "calling a function through a parameter is `unsupported call \"f\"` in AOT: no fnptr operand, no indirect call lowering",
		ref:    "roadmap L11.7 (functions are values that compile)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[3, 6]\n"}, {Backend: "aot", Missing: true}}},
	// The interpreter leg closed with ADR 0283: a top-level `def` now binds its own name to the same
	// closure handle a `lambda` gets, so `apply(twice, [1, 2])` maps the function and answers CPython's
	// `[2, 4]` instead of `undefined name twice`. Only the compiled leg still owes it — its body calls a
	// *parameter*, which is the higher-order limit L11.7 records — so this stays a debt row with one pin
	// answered and one still missing (roadmap Gap R.150, Gap R.151, ADR 0283).
	"programs/probe_fn_name": {oracle: lang.OracleDebt,
		reason: "a def'd function name is a value on the interpreter since ADR 0283 and still is not on the compiled backend, whose body then calls a parameter — the higher-order limit L11.7 records",
		ref:    "roadmap L11.7 (functions are values that compile)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "[2, 4]\n"}, {Backend: "aot", Missing: true}}},
	// Five lines of CPython parity on the interpreter. The compiled leg answers the two trap lines
	// (`abs(twice)`, `-twice`) byte-for-byte and refuses only the higher-order body — `apply` calls a
	// *parameter* — which is L11.7's limit rather than this row's name lookup, so the row is a debt with
	// one leg whole and one leg refused (roadmap Gap R.150, Gap R.151, ADR 0283).
	"programs/probe_a_function_as_a_value": {oracle: lang.OracleDebt,
		reason: "the interpreter answers CPython's five lines; the compiled leg refuses the body that calls a parameter (`unsupported call \"f\"`), the higher-order limit L11.7 records",
		ref:    "roadmap L11.7 (functions are values that compile); Gap R.150, Gap R.151, Gap R.153 (ADR 0283)",
		pins:   []lang.OraclePin{{Backend: "interpreter", Stdout: "42\n[2, 4, 6]\n12\ncaught the function operand\ncaught the negation\n"}, {Backend: "aot", Missing: true}}},
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
