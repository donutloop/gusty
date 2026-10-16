// L11.8 — a capability refusal is part of the model, so it is part of the machine document too.
//
// The exit-code half of this row landed with ADR 0211: a refusal is exit 1, an `llc` rejection is
// exit 2, a trap is exit 3, on every path. The half that stayed open is the sentence itself. A
// capability refusal — "this shape has no compiled lowering, and here is the half that is missing" —
// reaches a human as prose and reaches a script as `"error": "codegen: …"`, which means an agent
// planning a compilation has to match on the prose to learn what it was refused. Prose is not a
// contract: the sentence is edited, the agent's branch silently stops firing, and the failure mode is
// a script that quietly stops covering the shape.
//
// So each capability refusal has a **code** — `lower.unsupported.<shape>` — a **suggestion** that
// says what to write instead, and the **roadmap rows** that own the missing half. The registry below
// is the single source for all three: it is what `--json` classifies against, what `--schema`
// declares as an enum, and what the docs' code table is generated from.
//
// ## Why a classifier and not an error type at every site
//
// There are 142 `codegen:` refusal sites in this package. Wrapping each in a typed error would be a
// 142-line diff to code that decides programs, in a commit whose job is to name refusals rather than
// to change them — and, worse, a refusal whose type was forgotten still compiles, so the coverage
// would rot quietly. A classifier over this registry has the opposite failure mode, and it is the
// loud one: `TestTheCapabilityRegistryCoversWhatItClaimsToKnow` compiles this file's own sample
// programs and fails if one stops classifying, so editing a sentence without editing its marker here
// is a failing test rather than an agent's silently-dead branch.
//
// Two rules keep the classifier honest:
//
//   - **No code is invented.** A refusal that matches no marker gets no code — the JSON omits the
//     field rather than guessing. The claim is "every capability refusal the corpus produces today
//     has a code", measured by `TestEveryCapabilityRefusalInTheCorpusIsClassified` (integration), and
//     the tail that is not a capability refusal at all (a front-end sentence about a program error) is
//     an explicit allowlist in that test, not a silent hole.
//   - **The most specific marker wins.** `"tuple" is called but is not a function…` is both the
//     generic indirect-call sentence and the `tuple(...)` row this row names explicitly; the longest
//     match is the more informative answer, and it is deterministic.
package lang

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// CapabilityRefusalKind is one shape the compiled backend refuses as a matter of design, with the
// machinery that would close it named rather than implied.
type CapabilityRefusalKind struct {
	// Code is the stable identifier an agent branches on: `lower.unsupported.<shape>`. It is a
	// contract — the schema declares every code here as an enum, so a code that disappears is a
	// failing test, not a widened `default` branch.
	Code string `json:"code"`
	// Shape names what was refused, in the words the refusal uses.
	Shape string `json:"shape"`
	// Suggestion is the actionable half: what to write instead, or what to wait for. A refusal
	// without one is a bug with better manners (ADR 0166), so the registry's own test requires one.
	Suggestion string `json:"suggestion"`
	// Rows are the roadmap rows that own the missing half (Gap R.67, L11.1, …). An agent that wants
	// the work done reads these; a refusal that cites no row leaves the reader with nowhere to go.
	Rows []string `json:"rows"`
	// Markers are the fragments this refusal's sentence contains. They are matched against the
	// compiled leg's error text — most specific first — and are deliberately long enough to be
	// unique: the test below fails if a marker stops matching the sentence it names.
	Markers []string `json:"-"`
	// Sample is a program that must produce this refusal. It is the registry's leash on the prose:
	// change a sentence without changing this file and a test goes red.
	Sample string `json:"-"`
}

// capabilityRefusalRegistry is the inventory. Every entry was measured — its Sample compiled on the
// backend that ships and its sentence read off the result — rather than written from memory, and each
// cites the row that owns the missing half.
var capabilityRefusalRegistry = []CapabilityRefusalKind{
	{
		Code:       "lower.unsupported.list-copy",
		Shape:      "list(<container>) copies a container the program already built",
		Suggestion: "build the copy with list() and append the elements, or index the source's slots (xs[i]) — a copy is a container the runtime would have to build from a word that carries no kind yet",
		Rows:       []string{"L11.1", "Gap I.2"},
		Markers:    []string{"list(<container>) copies are not supported"},
		Sample:     "print(list([1, 2]))",
	},
	{
		Code:       "lower.unsupported.dict-copy",
		Shape:      "dict(<container>) copies a container the program already built",
		Suggestion: "build the copy with dict() and assign the keys, or read the source's slots — the same missing element word as list(<container>)",
		Rows:       []string{"L11.1", "Gap I.2"},
		Markers:    []string{"dict(<container>) copies are not supported"},
		Sample:     "print(dict({\"a\": 1}))",
	},
	{
		Code:       "lower.unsupported.tuple-is-immutable",
		Shape:      "a write into the immutable container: a tuple method or an item assignment reaches the door that would mutate the list layout a tuple shares",
		Suggestion: "write the tuple you want out as a literal — `t = (1, 2, 3)` — or build a list for what you mutate; concatenating (`t + (3,)`) is not compiled either, so the advice does not offer it (Gap R.33) and the door refuses rather than answering a mutated tuple",
		Rows:       []string{"L11.3"},
		Markers:    []string{"would write into a tuple, which is the immutable container"},
		Sample:     "t = (1, 2)\nt.append(3)\nprint(t)",
	},
	{
		Code:       "lower.unsupported.sort-elements-have-no-order",
		Shape:      "sorting a container whose elements are containers: the comparator compares payloads, and a container's payload is a heap slot rather than a value",
		Suggestion: "sort a container of scalars, or sort with an explicit key of its own (`sorted(xs, key=…)` over a kind the door can compare) — the answer would otherwise be the order the elements arrived in, printed as if it were an order",
		Rows:       []string{"L11.3", "Gap R.309"},
		Markers:    []string{"sorting needs an order for the elements"},
		Sample:     "print(sorted([(2, 1), (1, 2)]))",
	},
	{
		Code:       "lower.unsupported.tuple-call",
		Shape:      "tuple(...) builds a tuple from an iterable, which needs a callable and an element word",
		Suggestion: "write the tuple out (`(1, 2)` is a value since ADR 0361), or build a list — what this call still lacks is the callable's answer travelling as a (payload, tag) pair, not the tuple",
		Rows:       []string{"L11.3", "L11.1"},
		Markers:    []string{`"tuple" is called but is not a function this backend can build a call to`},
		Sample:     "print(tuple([1, 2]))",
	},
	{
		Code:       "lower.unsupported.method-text-argument",
		Shape:      "a text handed to a METHOD parameter the call sites do not all fill with texts: the parameter carries no text mark, so the body reads the @str_tab index as a number",
		Suggestion: "annotate the parameter (`def m(self, s: str)`) or pass a text at every call site — the mark is what tells the body its i32 slot holds a text index; a parameter one call site fills with a number has no word for both kinds yet (roadmap Gap R.315, ADR 0366). Texts to a method are answered when every call site agrees: print, `str`/`repr`, len, `in`, `==`, a `for`, the formatting, the repeat, a concatenation and the char methods all reach the doors a function's text parameter reaches",
		Rows:       []string{"Gap R.315", "L11.1"},
		Markers:    []string{"has no compiled word: the parameter it arrives at is not one every call site fills with a text"},
		Sample:     "class C:\n    def m(self, s):\n        return s * 2\n\n\nprint(C().m(\"hi\"))\nprint(C().m(2))\n",
	},
	{
		Code:       "lower.unsupported.sequence-operation",
		Shape:      "an ordering of two containers no name holds, the subscript of a sequence the compiler built, or a repeat whose answer is bigger than one container object",
		Suggestion: "bind the operands to names first — print, len, `in`, == and a for over a built sequence are all answered — and what is still missing here is the one arm that reads a slot of an object the compiler built in place, orders two containers it built, or grows an object past its 256-slot row",
		Rows:       []string{"Gap R.314", "Gap R.33", "L11.1"},
		Markers: []string{
			// The three refusals this code covers, each marked by the clause that never changes: the
			// subscript names the door that built the object, the ORDER names the door that reads a bound
			// name, and the row cap names the row one object owns. The operand spellings around them
			// (`[1, 2] < [2]`, `[1] * 300`) are the program's, not the backend's, so they are not markers
			// (the same rule `lower.unsupported.text-operator` states for `on a string (…)`).
			"the door that built it hands back a handle and the slot read wants the payload with its tag",
			"is the tagged order door's question",
			"past the 256 slots one container object owns",
		},
		Sample: "print([1, 2] < [2])",
	},
	{
		Code:       "lower.unsupported.comprehension-over-text",
		Shape:      "a comprehension whose iterable is a text has no compiled lowering — the loop would build a list of one-character texts and this backend builds that list nowhere",
		Suggestion: "iterate a list of the texts you want ([\"a\", \"b\"]), or a container variable the compiler can see the kind of — counting the text itself is code points since ADR 0362 (`len`, `s[i]`, `for c in s`), it is only the comprehension's list of characters that is missing",
		Rows:       []string{"L11.1", "Gap I.2"},
		Markers:    []string{"is refused: this backend builds a comprehension over an inline list literal"},
		Sample:     "print([c for c in \"abc\"])",
	},
	{
		Code:       "lower.unsupported.comprehension-over-constant-list",
		Shape:      "a comprehension over a list the compiler kept as a compile-time global",
		Suggestion: "materialise the list first (append to an empty one, or iterate it with `for`) — a comprehension needs slots to walk, and a global has none to hand",
		Rows:       []string{"L11.2", "Gap R.290"},
		Markers:    []string{"is a list the compiler kept as a compile-time constant"},
		// A module-level list whose ONLY reader is a comprehension is no longer kept static — the read now
		// counts, the object is built, and `[v for v in xs]` answers (ADR 0356). What still reaches this
		// refusal is the same read from inside a function body, whose names the analysis does not count as
		// reads at all (roadmap Gap R.290, beside Gap R.35's refusal of module reads from a body).
		Sample: "xs = [1, 2]\ndef f():\n    return [v for v in xs]\nprint(f())",
	},
	{
		Code:       "lower.unsupported.comprehension-key-not-constant",
		Shape:      "a dict comprehension whose key is not a compile-time constant",
		Suggestion: "build the dict with a loop and an assignment, or key it by an integer literal — the integer-key spelling of the same comprehension compiles",
		Rows:       []string{"Gap R.123", "L11.1"},
		Markers:    []string{"comprehension key must be constant"},
		Sample:     "d = {\"k\": v for v in [1, 2]}\nprint(d)",
	},
	{
		Code:       "lower.unsupported.set-comprehension-of-verdicts",
		Shape:      "a set comprehension of verdicts needs the tagged set builder",
		Suggestion: "collect the verdicts into a list instead, or bind each verdict to a name first — a set slot has nowhere to put a bool's kind today",
		Rows:       []string{"L11.1", "Gap R.117"},
		Markers:    []string{"needs the tagged set builder"},
		Sample:     "print({v > 1 for v in [1, 2]})",
	},
	{
		Code: "lower.unsupported.printf-style-format",
		// The formatting itself is answered (pkg/lang/percent_runtime.go, ADR 0360); what this row now
		// reports is the half of the conversion table the door has no renderer for, and the argument it
		// has no value for — both refused in the source they were written in, never answered sideways.
		Shape:      "a `%` conversion this backend has no renderer for, or an argument it has no value for",
		Suggestion: "keep to the conversions the door renders (`%s %r %a %d %i %o %x %X %e %E %f %F %g %G %c`) and pass values it can name a kind for — `%b` belongs to bytes, `%u` is the conversion the reference removed, and a tuple is an argument list rather than a value to render (an f-string is the other door)",
		Rows:       []string{"Gap R.31", "L11.1", "Gap R.165"},
		Markers: []string{
			"which this backend does not build",
			"whose (value, kind) pair this backend cannot build",
			"whose kind this backend cannot name",
		},
		Sample: "print(\"%b\" % 3)",
	},
	{
		Code:       "lower.unsupported.text-operator",
		Shape:      "an arithmetic operator applied to a text",
		Suggestion: "concatenate with `+` and repeat with `\"ab\" * 2` — both answered since ADR 0365; what has no meaning to read is arithmetic over an interned table index, which is what a `-` or `/` between two texts would need",
		Rows:       []string{"Gap R.31", "L11.1"},
		// The operand's spelled kind is printed into the sentence — `on a string (a string
		// literal)`, `on a string (*lang.Index)`, `on a string (s)` — so the marker is the half
		// that never changes: the clause after the operand, not the operand.
		Markers: []string{"is not supported in the AOT backend; CPython evaluates it"},
		Sample:  "print(\"a\" - \"b\")",
	},
	{
		Code:       "lower.unsupported.format-spec-needs-constant",
		Shape:      "a format spec whose value the compiler cannot read",
		Suggestion: "drop the spec (f\"{v}\" prints it), or format a value the compiler can see at that point — the spec is applied where the value is still a word with no kind",
		Rows:       []string{"Gap R.146", "L11.1"},
		Markers:    []string{"needs a value this backend can read at compile time"},
		Sample:     "w = 5\nprint(f\"{w:05d}\")",
	},
	{
		Code:       "lower.unsupported.function-read-as-value",
		Shape:      "a declared function read where a value is asked — print(f), a container slot, str(f)",
		Suggestion: "call the function and print the answer, or pass it to a function that calls it (roadmap L11.7, ADR 0345 answers the called half) — a `def` allocates no slot to read, so there is nothing for the position to hold",
		Rows:       []string{"Gap R.150", "Gap R.151", "L11.7"},
		Markers:    []string{"names function, which the compiled backend has no value for"},
		Sample:     "def f():\n    return 1\n\nprint(f)",
	},
	{
		Code:       "lower.unsupported.lambda-read-as-value",
		Shape:      "a lambda used as a value rather than called",
		Suggestion: "bind the lambda to a name and call it, or pass it where it will be called (ADR 0345 gives it a declaration there) — printed or stored, it has no slot to read and no kind to carry",
		Rows:       []string{"Gap R.150", "Gap R.151", "L11.7"},
		Markers:    []string{"a lambda used as a value has no compiled representation"},
		Sample:     "xs = []\nxs.append(lambda x: x)\nprint(len(xs))",
	},
	{
		Code:       "lower.unsupported.indirect-call",
		Shape:      "a call whose callee is a value the compiler cannot name",
		Suggestion: "call a declared function, or bind the callable where the pass can see it (roadmap L11.7, ADR 0345); the reference asks the value what it is at the moment of the call, which needs the word L11.1 owns",
		Rows:       []string{"L11.1", "L11.7"},
		Markers:    []string{"is called but is not a function this backend can build a call to"},
		Sample:     "def apply1(fn, v):\n    return fn(v)\n\ndef twice(fn, v):\n    return fn(v) + fn(v)\n\nprint(apply1(twice, 3))",
	},
	{
		Code:       "lower.unsupported.container-answer-slots",
		Shape:      "a container a function hands back whose slots the answer table declined to describe",
		Suggestion: "bind the answer to a name and read it there (print, str, a condition and a membership test all answer it), or fill the slots with values the table can follow — a slot filled by a call may be the (payload, tag) pair",
		Rows:       []string{"Gap R.261", "Gap R.262", "Gap R.67"},
		Markers:    []string{"hands back a container whose slots this backend will not describe"},
		Sample:     "def g():\n    return 5\n\ndef f():\n    out = []\n    out.append(g())\n    return out\n\nprint(f() + 1)",
	},
	{
		Code:       "lower.unsupported.container-answer-whole",
		Shape:      "a container a function hands back, in a position that keeps one word for the value",
		Suggestion: "bind the answer to a name and use it where the container road reads it (print, str, a condition, a loop), or return the contents instead of the container",
		Rows:       []string{"Gap R.67", "Gap R.146"},
		Markers:    []string{"hands back a container, whose word is a heap object"},
		Sample:     "def f():\n    out = [1, 2]\n    return out\n\nprint(f() + 1)",
	},
	{
		Code:       "lower.unsupported.index-needs-inline-container",
		Shape:      "an index into a container the compiler has no notebook for",
		Suggestion: "bind the container to a name and index the name, or index a literal written at the call — the index door reads a slot out of the notebooks it writes, and this container is not in them",
		Rows:       []string{"Gap R.214", "Gap R.262"},
		Markers:    []string{"index requires an inline list/dict/set literal"},
		Sample:     "def g():\n    return 5\n\ndef f():\n    out = []\n    out.append(g())\n    return out\n\nprint(f()[0])",
	},
	{
		Code:       "lower.unsupported.len-needs-inline-container",
		Shape:      "len of something whose length the compiler has no door for",
		Suggestion: "ask a container the compiler can see (a literal, or a variable it recorded a kind for), or count with a loop — len of a text and of a recorded container both answer",
		Rows:       []string{"Gap I.2", "L11.1"},
		Markers: []string{
			"len requires an inline list/dict/set literal",
			"len of a non-string variable",
		},
		Sample: "s = 3\nprint(len(s))",
	},
	{
		Code:       "lower.unsupported.sort-method-options",
		Shape:      "list.sort with key= or reverse=, which the method door has not been given",
		Suggestion: "use the builtin — ys = sorted(xs, key=f, reverse=True) — and assign back: the builtin reads the options by name and calls the key through the ordinary call road (ADRs 0340 and 0345)",
		Rows:       []string{"Gap R.269", "L11.7"},
		Markers:    []string{"sort() takes no arguments in this build"},
		Sample:     "xs = [3, 1]\nxs.sort(key=lambda v: 0 - v)",
	},
	{
		Code:       "lower.unsupported.container-method-over-a-name",
		Shape:      "a container method the backend folds only over a literal it can see",
		Suggestion: "write the argument out at the call (xs.extend([7])), or loop and append — the method road folds over a literal written at the call and this argument's slots belong to the runtime",
		Rows:       []string{"Gap I.2", "L11.1"},
		Markers:    []string{"folds container methods only over a literal written at the call"},
		Sample:     "xs = []\nxs.extend([7])\nprint(xs)",
	},
	{
		Code:       "lower.unsupported.sorted-key-over-runtime-elements",
		Shape:      "sorted with key= over a container whose elements exist only at run time",
		Suggestion: "sort a literal, or bind the elements first and sort those — the key must be called on each element, and the element has no word to travel in yet",
		Rows:       []string{"Gap R.254", "Gap R.249"},
		Markers:    []string{"the elements of that container exist only at run time"},
		Sample:     "xs = [3, 1, 2]\nprint(sorted(xs, key=abs))",
	},
	{
		Code:       "lower.unsupported.set-copy",
		Shape:      "set(<container>) copies a container the program already built",
		Suggestion: "build the copy with set() and add the elements, or read the source's slots — the same missing element word as list(<container>)",
		Rows:       []string{"L11.1", "Gap I.2"},
		Markers:    []string{"set(<container>) copies are not supported"},
		Sample:     "print(set([1, 2]))",
	},
	{
		Code:       "lower.unsupported.mixed-element-pair",
		Shape:      "an element of a list whose elements are more than one kind, in a position that keeps one word",
		Suggestion: "print(xs[i]) directly, bind it (`v = xs[i]`), or keep one kind per list — the element is a (payload, tag) pair and this position stores a single i32 with no tag beside it",
		Rows:       []string{"L11.1", "Gap R.82", "Gap R.146"},
		Markers:    []string{"so one element is a (value, tag) pair"},
		Sample:     "xs = [1, \"a\"]\ndef id(v):\n    return v\n\nprint(id(xs[1]))",
	},
	{
		Code:       "lower.unsupported.float-return-word",
		Shape:      "a function whose `return` is a name its own body rebound to a float",
		Suggestion: "write `return x + 0.0`, or bind the answer first (`y = x + 0.0; return y`) — `return x` says nothing about the word, and the double the body computed would travel in an i32",
		Rows:       []string{"L11.6", "Gap R.3c"},
		Markers:    []string{"which its own body binds to a float"},
		Sample:     "def f(x):\n    x = x + 0.5\n    return x\n\nprint(f([1]))",
	},
	{
		Code:       "lower.unsupported.ternary-arms-disagree",
		Shape:      "a ternary whose two arms do not agree on a word (one is a double, one an i32)",
		Suggestion: "write both arms in the same shape (`else 0` -> `else 0.0`), or take the branch with if/else and bind the value on each arm — an i32 select would truncate the double arm",
		Rows:       []string{"L11.1", "L11.6"},
		Markers:    []string{"do not agree on a word"},
		Sample:     "def f(x):\n    return x if x else 1.5\n\nprint(f(1))",
	},
	{
		Code:       "lower.unsupported.candidate-among-values",
		Shape:      "a container standing among the candidates of min/max",
		Suggestion: "flatten the candidates (`min(1, 2, 3)`) or pass exactly one container — comparing two containers by their handle words would answer with heap addresses",
		Rows:       []string{"L11.1", "Gap R.73", "Gap R.107"},
		Markers:    []string{"not a container among values"},
		Sample:     "print(min([1, 2], [3]))",
	},
	{
		Code:       "lower.unsupported.minmax-winner-kind",
		Shape:      "min/max over runtime candidates whose static kinds mix int and double",
		Suggestion: "keep the candidates literal, or put them in one domain (`min(2.5, float(k))` spelling) — the comparison is decidable, but which operand wins is only known at run time and its kind travels with it",
		Rows:       []string{"L11.1", "Gap R.73", "Gap R.109"},
		Markers:    []string{"winner's own kind needs the tagged value word"},
		Sample:     "k = 1\nprint(min(2.5, k))",
	},
	{
		Code:       "lower.unsupported.builtin-needs-inline-container",
		Shape:      "min/max/sum asked of a container the compiler does not hold as a literal",
		Suggestion: "write the container out at the call (min([1, 2])), or count with a loop — the reduction folds over a literal it can see and this container's slots belong to the runtime",
		Rows:       []string{"Gap R.71", "Gap R.107", "L11.1"},
		Markers:    []string{"requires an inline list/set/dict literal"},
		Sample:     "xs = [1.5, 2]\nprint(min(xs))",
	},
	{
		Code:       "lower.unsupported.runtime-reduction",
		Shape:      "a reduction whose elements are computed at run time",
		Suggestion: "accumulate in a loop (`total = 0` / `total += v`), or reduce a literal — folding over elements that only exist at run time would answer 0 rather than add them up",
		Rows:       []string{"L11.7", "Gap R.71"},
		Markers:    []string{"needs a runtime reduction"},
		Sample:     "def sq(n):\n    return n * n\n\nprint(sum([sq(x) for x in range(4)]))",
	},
	{
		Code:       "lower.unsupported.dict-method-needs-runtime-search",
		Shape:      "a dict method that would have to search the container at run time",
		Suggestion: "iterate the container with `for` (for k in d / for k, v in items), or read the keys you know — the method road lowers against the container's own word, and this method needs a run-time search the doors do not have",
		Rows:       []string{"L11.1", "Gap I.2"},
		Markers:    []string{"this backend builds the dict methods it can lower"},
		Sample:     "for k, v in {\"a\": 1}.items():\n    print(k)",
	},
	{
		Code:       "lower.unsupported.join-needs-constant",
		Shape:      "str.join over a list the compiler cannot see element by element",
		Suggestion: "write the list out at the call (\",\".join([\"a\", \"b\"])), or build the string with += in a loop — join is folded over a constant list of constant strings",
		Rows:       []string{"Gap I.2", "L11.1"},
		Markers: []string{
			"join() argument must be a constant list",
			"join() list elements must be constant strings",
		},
		Sample: "xs = [\"a\", \"b\"]\nprint(\",\".join(xs))",
	},
	{
		Code:       "lower.unsupported.method-receiver-unreadable",
		Shape:      "a text method whose receiver is not a text the compiler can read",
		Suggestion: "bind the receiver first (`s = f()` / `s.upper()`), or call the method on a text written at the call — the method road needs the receiver's text to fold against",
		Rows:       []string{"Gap I.2", "L11.1"},
		Markers:    []string{"on a receiver that is not a text the compiler can read"},
		Sample:     "def s():\n    return \"a\"\n\nprint(s().append(1))",
	},
	{
		Code:       "lower.unsupported.slice-base-not-described",
		Shape:      "a slice whose base the container records do not describe",
		Suggestion: "bind the base to a name and slice the name, or slice a literal / a variable a binding recorded as a list — the copy is built from the base's slots, and this base has no record saying what they hold",
		Rows:       []string{"Gap R.235", "Gap R.234"},
		Markers:    []string{"the base is not a list the records describe"},
		Sample:     "def m():\n    return {\"k\": [1.5]}\n\nprint(m()[\"k\"][0:1])",
	},
	{
		Code:       "lower.unsupported.membership-needle-unprovable",
		Shape:      "a membership test whose needle's kind the compiler cannot prove",
		Suggestion: "test a literal or a variable whose kind the compiler knows (`\"hi\" in xs`), or compare with == — the haystack's slots describe themselves, and the needle must arrive with the kind it has (the sentence also prints the needle's Go representation, which is Gap R.232's half of this row)",
		Rows:       []string{"L11.1", "Gap R.232", "Gap R.82"},
		Markers:    []string{"needs a needle whose kind the compiler can prove"},
		Sample:     "def words():\n    return [\"hi\", \"ho\"]\n\nprint(words()[0] in words())",
	},
	{
		Code:       "lower.unsupported.sort-loop-argument",
		Shape:      "for over sorted()/reversed() with an argument neither door can name",
		Suggestion: "sort a list literal or a list variable (for v in sorted(xs)), or bind the sorted list first and walk it — the ordering helper chooses its comparator from the argument's spelled kind, and this argument has none the doors can read",
		Rows:       []string{"Gap R.247", "Gap R.248", "L11.1"},
		Markers: []string{
			"is a TEXT, which the reference sorts by building a list of one-character strings",
			"the ELEMENT kind of that list is not something this backend's roads print from one word",
			"its argument is not a container this backend can name",
		},
		Sample: "for v in sorted(\"bca\"):\n    print(v)",
	},
	{
		Code:       "lower.unsupported.container-slot-kind-unprovable",
		Shape:      "a container slot filled by a call whose answer the answer direction cannot label — an arm that answers a float beside an arm that answers anything else, or an arm that answers a name the source does not spell out",
		Suggestion: "give the function arms whose kinds the source spells (an int, a text, a bool, None, a container literal), or print the answer instead of storing it — print, str/repr, a condition, a binding and a container literal all ask the answer's own kind word (ADR 0368)",
		Rows:       []string{"L11.1", "Gap R.324", "Gap R.219"},
		Markers:    []string{"the compiler cannot prove one kind for it"},
		// A body whose arms are a text and an int is carried by ADR 0368 and its slot is written with the
		// arm's own tag, so this sample used to be that program and stopped refusing. What still refuses is
		// the residue the row names: a double has no word in the answer direction, and `return y` hands the
		// scan a kind no spelling gives it (roadmap Gap R.324).
		Sample: "def f(x):\n    if x:\n        return \"a\"\n    return 1.5\n\nys = []\nys.append(f(1))\nprint(ys)",
	},
	{
		Code:       "lower.unsupported.pair-return-word",
		Shape:      "a function asked to hand back the (payload, tag) pair its answer arrived in",
		Suggestion: "print the call, or bind it (`y = f(x)`) and use the name — the positions that read one word cannot carry the tag beside the payload, so the pair has nowhere to go",
		Rows:       []string{"L11.1", "Gap R.146"},
		Markers:    []string{"hands back the (payload, tag) pair its answer arrived in"},
		Sample:     "def f(x):\n    return x\n\nys = []\nys.append(f(1.5))\nprint(len(ys))",
	},
	{
		Code:       "lower.unsupported.literal-element-needs-tag",
		Shape:      "a literal's element read in a position that keeps one word for a whole value",
		Suggestion: "print it, bind it (`v = xs[i]`), or ask it where the pair door stands (print, str/repr, len, a membership test, a loop) — a float, text, None or bool element is a (payload, tag) pair and one word would answer with the handle or the table index",
		Rows:       []string{"Gap R.218", "Gap R.146", "L11.1"},
		Markers:    []string{"needs the element's kind to travel with the element"},
		Sample:     "print([1.5, 2][0] + 1)",
	},
	{
		Code:       "lower.unsupported.slot-read-of-rebound-name",
		Shape:      "a subscript into a container the compiler no longer trusts the binding of",
		Suggestion: "bind the container to a fresh name after the last mutation and index that, or ask the object as a whole (print, ==, len, a loop) — the slot read is licensed by the literal the name was bound to, and a mutation or a hand-off to another function takes that licence back",
		Rows:       []string{"L11.1", "Gap R.191", "Gap R.323"},
		Markers:    []string{"cannot reach into", "the name was rebound, mutated"},
		// The unpacking that used to be this code's sample — `a, b = xs[0][0] + 1, xs[0][1] + 2` — answers
		// since ADR 0367, and a sample that compiles is a claim about nothing (TestCLICapabilityRefusalText
		// RoundTrips fails it on purpose). What still asks the subscript for ONE word is a condition's head,
		// and the truth of a slot of a container the program built is the half owed (Gap R.323).
		Sample: "xs = []\nxs.append([7, 8])\nif xs[0][0]:\n    print(1)",
	},
}

// capabilityByCode is the lookup the schema and the docs table build on; the registry slice is the
// order a reader of the file expects (roughly: by the door that refuses), and this map is for the
// callers that already know a code.
var capabilityByCode = func() map[string]*CapabilityRefusalKind {
	m := make(map[string]*CapabilityRefusalKind, len(capabilityRefusalRegistry))
	for i := range capabilityRefusalRegistry {
		m[capabilityRefusalRegistry[i].Code] = &capabilityRefusalRegistry[i]
	}
	return m
}()

// CapabilityRefusalCodes returns every stable capability-refusal code, sorted — what `--schema`
// declares as the enum, so a code removed from the registry is a schema that no longer claims it.
func CapabilityRefusalCodes() []string {
	codes := make([]string, 0, len(capabilityRefusalRegistry))
	for i := range capabilityRefusalRegistry {
		codes = append(codes, capabilityRefusalRegistry[i].Code)
	}
	sort.Strings(codes)
	return codes
}

// CapabilityRefusalKinds returns the registry, sorted by code, for the machine path and the docs
// generator. The sample and marker fields are unexported in JSON: they are the registry's leash on
// the prose, not a claim about the language an agent should be reading.
func CapabilityRefusalKinds() []CapabilityRefusalKind {
	out := make([]CapabilityRefusalKind, len(capabilityRefusalRegistry))
	copy(out, capabilityRefusalRegistry)
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })
	return out
}

// CapabilityRefusalByCode finds one registry entry by its stable code.
func CapabilityRefusalByCode(code string) (*CapabilityRefusalKind, bool) {
	k, ok := capabilityByCode[code]
	return k, ok
}

// ClassifyCapabilityRefusal answers "which capability refusal is this sentence?". It is called with
// the compiled leg's error text, which carries the `codegen:` prefix and the roadmap citation, and it
// answers with the entry whose marker is the LONGEST one the sentence contains — the most specific
// shape wins, deterministically, so `tuple(...)`'s refusal is the `tuple` row rather than the generic
// indirect-call row even though both markers match.
//
// A sentence that matches nothing returns ok=false. That is the whole point: the JSON omits `code`
// rather than inventing one, and the coverage test in integration/ is what keeps the two sets from
// drifting apart.
func ClassifyCapabilityRefusal(msg string) (*CapabilityRefusalKind, bool) {
	var best *CapabilityRefusalKind
	bestLen := 0
	for i := range capabilityRefusalRegistry {
		k := &capabilityRefusalRegistry[i]
		for _, m := range k.Markers {
			if len(m) > bestLen && strings.Contains(msg, m) {
				best, bestLen = k, len(m)
			}
		}
	}
	if best == nil {
		return nil, false
	}
	return best, true
}

// capabilitiesToolName is the self-describing name of this account, in the sense `--variance` and
// `--callables` use it: an agent discovering the surface asks for it by name.
const capabilitiesToolName = "capabilities"

// CapabilityDocsTable is the registry as the markdown table docs/operations.md prints: code, shape,
// suggestion, owning rows. It exists so that the human-readable inventory and the machine-readable
// one are one list — `TestTheCapabilityDocsTableIsTheRegistry` fails if the document stops containing
// this exact text, which is the only way a doc table and a registry stay in step (ADR 0346).
//
// The pipes inside a shape or a suggestion are escaped: a refusal's sentence is allowed to talk about
// `a | b`, and a table that breaks because of a character in a cell is a table nobody reads to the end.
func CapabilityDocsTable() string {
	var b strings.Builder
	b.WriteString("| Code | Shape | Suggestion | Roadmap rows |\n")
	b.WriteString("|---|---|---|---|\n")
	for _, k := range CapabilityRefusalKinds() {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
			k.Code,
			escapesDocsCell(k.Shape),
			escapesDocsCell(k.Suggestion),
			escapesDocsCell(strings.Join(k.Rows, " · ")))
	}
	return strings.TrimRight(b.String(), "\n")
}

func escapesDocsCell(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}

// CapabilitiesJSON is the registry as a document: every shape the compiled backend refuses as a matter
// of design, its stable code, the suggestion that replaces it, and the roadmap rows that own the
// missing half (`gustyc --capabilities`; described by the schema's capabilityRefusal definition).
//
// It exists because a refusal that only lives in prose is discoverable by nobody who is not reading a
// terminal: an agent planning a compilation asks what it CANNOT write, and today the honest answer is
// "run it and match on the sentence". The document turns that into one call.
func CapabilitiesJSON() (string, error) {
	kinds := CapabilityRefusalKinds()
	type row struct {
		Code       string   `json:"code"`
		Shape      string   `json:"shape"`
		Suggestion string   `json:"suggestion"`
		Rows       []string `json:"rows"`
	}
	doc := struct {
		Tool    string `json:"tool"`
		Family  string `json:"family"`
		Backend string `json:"backend"`
		Kinds   []row  `json:"kinds"`
	}{
		Tool:    capabilitiesToolName,
		Family:  "lower.unsupported",
		Backend: "aot",
		Kinds:   make([]row, 0, len(kinds)),
	}
	for _, k := range kinds {
		rows := append([]string(nil), k.Rows...)
		if rows == nil {
			rows = []string{}
		}
		doc.Kinds = append(doc.Kinds, row{Code: k.Code, Shape: k.Shape, Suggestion: k.Suggestion, Rows: rows})
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}
