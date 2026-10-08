package lang

// pkg/lang/pair_dict_set_test.go — a pair-bound name as a DICT ENTRY and a SET MEMBER (roadmap L11.1,
// Gap R.146's positions that keep one word for a whole value; ADR 0310, ADR 0306's list element,
// ADR 0232's tagged dict/set slots, ADR 0187's "a payload is never written without its tag").
//
// A list literal learned to carry a pair in ADR 0306. The dict and the set did not, and for a reason
// worth stating precisely: each has ONE builder call that takes the tag as a number — `rt_dict_put_tagged`
// and `rt_set_add_tagged` — and the roads that fill them asked `heapElemKind` for the element's payload and
// `elemKindTag` for its label before any tag question had been put. A name the pair road bound (`n = xs[0]`
// over a container the program built) has no payload without its tag: the number itself for an int, a
// `@float_box` handle for a float, an index into `@str_tab` for a text, an entry count for a container. So
// `print({"k": n})` refused while `print([n])` answered — the asymmetry Gap R.146 was filed to describe.
//
// The fix is the same one ADR 0306 made, asked of two more builders: those two calls take an `i32` for the
// tag, **a register is an `i32`**, and the pair's tag is a register the objects wrote. Nothing in the
// runtime changed.
//
// The half that is NOT a copy of ADR 0306 is the guard. A dict key and a set member have to answer a
// question no list element is asked: **can this value be a key at all?** CPython refuses a list, a dict and
// a set with `TypeError: unhashable type: 'list'` and company, and a payload alone answers "yes" — the
// handle of a list is an i32, so `{n}` over a container slot would have built a set holding an address and
// printed `{[1, 2]}` at the exit code of success. The tag is what knows, so the tag is what is asked
// (roadmap Gap R.81 owns the general hashing rule; this door asks the question for the shapes it opened).
//
// The failure mode every row here rules out is the silent one: an interned index or a box handle printed
// where the reference has a text or a float. The record leg is what pins it; the reference leg lives in
// integration/pair_dict_set_test.go, and for these shapes the reference is the only witness that can tell
// `{'k': 'a'}` from `{'k': 0}`.

import (
	"strings"
	"testing"
)

// The seven kinds a slot can hold, each spelled the way the language spells it: build a container, append
// one value, bind the element. `n` is then a (payload, tag) pair whose payload means something different
// per kind — which is the whole reason a tag exists.
const (
	pairDictIntSlot   = "xs = []\nxs.append(7)\nn = xs[0]\n"
	pairDictTextSlot  = "xs = []\nxs.append(\"a\")\nn = xs[0]\n"
	pairDictFloatSlot = "xs = []\nxs.append(2.5)\nn = xs[0]\n"
	pairDictNoneSlot  = "xs = []\nxs.append(None)\nn = xs[0]\n"
	pairDictBoolSlot  = "xs = []\nxs.append(True)\nn = xs[0]\n"
	pairDictListSlot  = "xs = []\nxs.append([1, 2])\nn = xs[0]\n"
	pairDictDictSlot  = "xs = []\nxs.append({\"a\": 1})\nn = xs[0]\n"
	pairDictSetSlot   = "xs = []\nxs.append({1})\nn = xs[0]\n"

	pairDictArithSlot = "xs = []\nxs.append([7, 8])\nn = xs[0][0] * 2\n"
	pairDictByKeySlot = "d = {}\nd[\"k\"] = 9\nn = d[\"k\"]\n"
	pairDictTwoSlots  = "xs = []\nxs.append(7)\nxs.append(8)\na = xs[0]\nb = xs[1]\n"
	pairDictLoopVar   = "xs = []\nxs.append(7)\nfor v in xs:\n    "
)

// TestAPairBoundNameEntersADictEntryByWayOfItsTag is the row Gap R.146 named for the dict: a key or a value
// is no longer a position that keeps one word for its operand. Every answer is the record's, which is
// CPython's; the pair is asked on both sides of the entry, beside literals, nested, and as the loop variable.
func TestAPairBoundNameEntersADictEntryByWayOfItsTag(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, want string }{
		{"an int slot answers the number", pairDictIntSlot, "print({\"k\": n})\n", "{'k': 7}\n"},
		{"a text slot answers the text, not its interned index", pairDictTextSlot, "print({\"k\": n})\n", "{'k': 'a'}\n"},
		{"a float slot keeps its .0", pairDictFloatSlot, "print({\"k\": n})\n", "{'k': 2.5}\n"},
		{"a None slot", pairDictNoneSlot, "print({\"k\": n})\n", "{'k': None}\n"},
		{"a bool slot answers True, not 1", pairDictBoolSlot, "print({\"k\": n})\n", "{'k': True}\n"},
		{"a container slot prints its contents", pairDictListSlot, "print({\"k\": n})\n", "{'k': [1, 2]}\n"},
		{"the answer of arithmetic over a slot", pairDictArithSlot, "print({\"k\": n})\n", "{'k': 14}\n"},
		{"a dict slot read by key", pairDictByKeySlot, "print({\"v\": n})\n", "{'v': 9}\n"},
		{"the pair beside literal entries", pairDictIntSlot, "print({\"a\": n, \"b\": 2})\n", "{'a': 7, 'b': 2}\n"},
		{"a key from an int slot", pairDictIntSlot, "print({n: \"v\"})\n", "{7: 'v'}\n"},
		{"a key from a text slot", pairDictTextSlot, "print({n: 1})\n", "{'a': 1}\n"},
		{"a key from a float slot", pairDictFloatSlot, "print({n: 1})\n", "{2.5: 1}\n"},
		{"a key from a None slot", pairDictNoneSlot, "print({n: 1})\n", "{None: 1}\n"},
		{"a key from a bool slot", pairDictBoolSlot, "print({n: 1})\n", "{True: 1}\n"},
		{"key and value are both pairs", pairDictTwoSlots, "print({a: b})\n", "{7: 8}\n"},
		{"key and value are the same pair", pairDictIntSlot, "print({n: n})\n", "{7: 7}\n"},
		{"a dict value is a list holding the pair", pairDictIntSlot, "print({\"k\": [n]})\n", "{'k': [7]}\n"},
		{"a list element is a dict holding the pair", pairDictIntSlot, "print([{\"k\": n}])\n", "[{'k': 7}]\n"},
		{"a dict inside a dict", pairDictIntSlot, "print({\"a\": {\"b\": n}})\n", "{'a': {'b': 7}}\n"},
		{"the loop variable as a value", pairDictLoopVar, "print({\"k\": v})\n", "{'k': 7}\n"},
		{"the loop variable as a key", pairDictLoopVar, "print({v: \"x\"})\n", "{7: 'x'}\n"},
		{"a dict literal handed to a function", pairDictIntSlot, "def show(d):\n    print(d)\nshow({\"k\": n})\n", "{'k': 7}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			RecordedStdoutIs(t, src, tc.want)
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAPairBoundNameBoundIntoADictKeepsItsTagOnTheObject is the binding half: `d2 = {"k": n}` is the same
// entry question asked by the assignment road, which keeps its own record of the variable's key and value
// kinds as well as its own tag slots. Every position that reads the dict back has to see the tag the name
// carried, or it reads a payload it cannot classify — which is how a text value prints its interned index
// and a pair key stops being findable.
func TestAPairBoundNameBoundIntoADictKeepsItsTagOnTheObject(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, want string }{
		{"bound and printed", pairDictIntSlot, "d2 = {\"k\": n}\nprint(d2)\n", "{'k': 7}\n"},
		{"bound and measured", pairDictIntSlot, "d2 = {\"k\": n}\nprint(len(d2))\n", "1\n"},
		{"bound and looked up by its literal key", pairDictIntSlot, "d2 = {\"k\": n}\nprint(d2[\"k\"])\n", "7\n"},
		{"looked up and used as a number", pairDictIntSlot, "d2 = {\"k\": n}\nprint(d2[\"k\"] + 1)\n", "8\n"},
		{"a pair key asked for its membership", pairDictIntSlot, "d2 = {n: \"v\"}\nprint(n in d2)\n", "True\n"},
		{"looked up through the pair it was keyed by", pairDictIntSlot, "d2 = {n: \"v\"}\nprint(d2[n])\n", "v\n"},
		{"a pair key beside a literal key", pairDictIntSlot, "d2 = {n: \"v\", 1: \"w\"}\nprint(d2[7])\n", "v\n"},
		{"bound from a text slot and rendered by str", pairDictTextSlot, "d2 = {\"k\": n}\nprint(str(d2))\n", "{'k': 'a'}\n"},
		{"rendered by repr", pairDictTextSlot, "print(repr({\"k\": n}))\n", "{'k': 'a'}\n"},
		{"compared with a literal dict", pairDictTextSlot, "print({\"k\": n} == {\"k\": \"a\"})\n", "True\n"},
		{"compared unequal", pairDictTextSlot, "print({\"k\": n} != {\"k\": \"b\"})\n", "True\n"},
		{"keys looped over", pairDictIntSlot, "d2 = {n: \"v\"}\nfor k in d2:\n    print(k)\n", "7\n"},
		{"the value read back inside the key loop", pairDictIntSlot, "d2 = {\"k\": n}\nfor k in d2:\n    print(k, d2[k])\n", "k 7\n"},
		{"grown by a literal key after binding", pairDictIntSlot, "d2 = {\"k\": n}\nd2[\"j\"] = 2\nprint(d2)\n", "{'k': 7, 'j': 2}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			RecordedStdoutIs(t, src, tc.want)
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAPairBoundNameEntersASetMemberByWayOfItsTag is the set's half of the same door. A member is also the
// position the runtime dedups, and a dedup that reads the payload alone makes `1` and a text whose interned
// index happens to be 1 the same member — which is why the rows below keep an int and a text apart.
func TestAPairBoundNameEntersASetMemberByWayOfItsTag(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, want string }{
		{"an int slot answers the number", pairDictIntSlot, "print({n})\n", "{7}\n"},
		{"a text slot answers the text", pairDictTextSlot, "print({n})\n", "{'a'}\n"},
		{"a float slot keeps its .0", pairDictFloatSlot, "print({n})\n", "{2.5}\n"},
		{"a None slot", pairDictNoneSlot, "print({n})\n", "{None}\n"},
		{"a bool slot answers True", pairDictBoolSlot, "print({n})\n", "{True}\n"},
		{"the loop variable as a member", pairDictLoopVar, "print({v})\n", "{7}\n"},
		{"two equal pairs are one member", pairDictIntSlot, "xs.append(7)\nm = xs[1]\nprint({n, m})\n", "{7}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			RecordedStdoutIs(t, src, tc.want)
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAPairBoundNameBoundIntoASetKeepsItsTagOnTheObject is the set's binding half — the same reads the
// dict's binding table makes, asked of an object whose members are a payload-and-tag scan.
func TestAPairBoundNameBoundIntoASetKeepsItsTagOnTheObject(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, want string }{
		{"bound and printed", pairDictIntSlot, "s2 = {n}\nprint(s2)\n", "{7}\n"},
		{"bound and measured", pairDictIntSlot, "s2 = {n}\nprint(len(s2))\n", "1\n"},
		{"asked for its membership by literal", pairDictIntSlot, "s2 = {n}\nprint(7 in s2)\n", "True\n"},
		{"asked for its membership as text", pairDictTextSlot, "s2 = {n}\nprint(\"a\" in s2)\n", "True\n"},
		{"added to and still one member", pairDictIntSlot, "s2 = {n}\ns2.add(7)\nprint(len(s2))\n", "1\n"},
		{"looped over", pairDictIntSlot, "s2 = {n}\nfor v in s2:\n    print(v)\n", "7\n"},
		{"rendered by str", pairDictTextSlot, "print(str({n}))\n", "{'a'}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			RecordedStdoutIs(t, src, tc.want)
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if got := strings.TrimSuffix(res.Output, "\n"); got != strings.TrimSuffix(tc.want, "\n") {
				t.Fatalf("compiled printed %q, want %q", got, strings.TrimSuffix(tc.want, "\n"))
			}
		})
	}
}

// TestAPairBoundKeyOrMemberThatCannotBeAKeyRaisesWhatTheReferenceRaises is the half a door copied from the
// list would have forgotten. Nothing about a list element asks whether the value can be a key; a dict entry
// and a set member have to, and once the kind lives in a register only the run time can answer. Reading the
// payload alone answers "yes" for every value in the language — a list's handle is an i32 like any other —
// and the program then prints `{[1, 2]}` happily at exit 0 where CPython raises.
func TestAPairBoundKeyOrMemberThatCannotBeAKeyRaisesWhatTheReferenceRaises(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, class, message string }{
		{"a list slot cannot be a key", pairDictListSlot, "print({n: 1})\n", "TypeError", "unhashable type: 'list'"},
		{"a dict slot cannot be a key", pairDictDictSlot, "print({n: 1})\n", "TypeError", "unhashable type: 'dict'"},
		{"a set slot cannot be a key", pairDictSetSlot, "print({n: 1})\n", "TypeError", "unhashable type: 'set'"},
		{"a list slot cannot be a member", pairDictListSlot, "print({n})\n", "TypeError", "unhashable type: 'list'"},
		{"a dict slot cannot be a member", pairDictDictSlot, "print({n})\n", "TypeError", "unhashable type: 'dict'"},
		{"a set slot cannot be a member", pairDictSetSlot, "print({n})\n", "TypeError", "unhashable type: 'set'"},
		{"a bound dict refuses its unhashable key", pairDictListSlot, "d2 = {n: 1}\nprint(d2)\n", "TypeError", "unhashable type: 'list'"},
		{"a bound set refuses its unhashable member", pairDictListSlot, "s2 = {n}\nprint(s2)\n", "TypeError", "unhashable type: 'list'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("compiled to nothing where the reference raises: %v", err)
			}
			if !strings.Contains(res.Stderr, tc.class+": "+tc.message) {
				t.Fatalf("raised %q, want %s: %s", firstLine(res.Stderr), tc.class, tc.message)
			}
			if res.Code == 0 {
				t.Fatalf("the trap left at the exit code of success")
			}
			if res.Code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module this raise was emitted into (ADR 0166)")
			}
		})
	}
}

// TestTheUnhashablePairRaiseReachesTheProgramsOwnArm is the trap's other half: a built-in failure that no
// `except` can reach is a different program (ADR 0228's "a built-in trap is a typed raise").
func TestTheUnhashablePairRaiseReachesTheProgramsOwnArm(t *testing.T) {
	for _, tc := range []struct{ name, pre, body, want string }{
		{"the program's own arm runs for a key", pairDictListSlot, "try:\n    print({n: 1})\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
		{"the program's own arm runs for a member", pairDictListSlot, "try:\n    print({n})\nexcept TypeError:\n    print(\"caught\")\n", "caught\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			RecordedStdoutIs(t, src, tc.want)
			res, err := JIT(src, 0)
			if err != nil {
				t.Fatalf("the compiled backend refused a program the reference answers: %v", err)
			}
			if strings.TrimSuffix(res.Output, "\n") != "caught" {
				t.Fatalf("the arm did not run: out=%q err=%q", res.Output, firstLine(res.Stderr))
			}
		})
	}
}

// TestAPairBoundDictAndSetRunThroughTheTaggedBuilders is the IR half of the decision: the entry and the
// member go to the calls that take the tag with the payload, the tag arrives as the register the objects
// wrote rather than a constant this pass guessed, and the object is told its slots describe themselves.
// The rows fail if the road grows a second builder of its own, if a tag stops being written (ADR 0187), or
// if the payload starts being stored as a `double`, which is the module `llc` rejects for a container slot.
func TestAPairBoundDictAndSetRunThroughTheTaggedBuilders(t *testing.T) {
	for _, tc := range []struct {
		name, src, wantCall string
		guards              bool
	}{
		{"a dict value", pairDictIntSlot + "print({\"k\": n})\n", "call void @rt_dict_put_tagged(", false},
		{"a dict key", pairDictIntSlot + "print({n: 1})\n", "call void @rt_dict_put_tagged(", true},
		{"a set member", pairDictIntSlot + "print({n})\n", "call void @rt_set_add_tagged(", true},
		{"a bound dict", pairDictIntSlot + "d2 = {\"k\": n}\nprint(d2)\n", "call void @rt_dict_put_tagged(", false},
		{"a bound set", pairDictIntSlot + "s2 = {n}\nprint(s2)\n", "call void @rt_set_add_tagged(", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused a program the reference prints: %v", err)
			}
			if !strings.Contains(res.IR, tc.wantCall) {
				t.Errorf("the entry never asked %s — it grew a one-word road of its own", tc.wantCall)
			}
			// The tag is read out of the name's own word. A `%d` of the payload alone, or a constant tag
			// beside a pair payload, is the wrong answer this file keeps impossible.
			if !strings.Contains(res.IR, "load i32, i32* %_n_tag") {
				t.Error("the tag never came from the name the pair road bound — it was guessed from a " +
					"spelling the element never had")
			}
			tagged := false
			for _, line := range strings.Split(res.IR, "\n") {
				if strings.Contains(line, tc.wantCall) && strings.Contains(line, "%t") {
					tagged = true
				}
			}
			if !tagged {
				t.Errorf("no tagged call took a register — the tag was a constant, not the word the objects wrote")
			}
			// The unhashable question belongs to a key and a member, and to no other position: a value
			// slot has no hashing rule to check, and spelling CPython's sentence where the reference raises
			// nothing is a trap of the compiler's own invention (ADR 0271's rule about what a raise says).
			got := strings.Contains(res.IR, "unhashable type: 'list'")
			if got != tc.guards {
				t.Errorf("the key/member guard is present=%v, want %v", got, tc.guards)
			}
		})
	}
}

// TestADictAndSetLiteralTheCompilerCanReadKeepsItsStaticRoad keeps the new door off the positions that never
// needed it: an element whose tag is a constant has no register to carry and no unhashable surprise, and a
// literal that reaches the tagged pair road anyway costs three guards per entry for nothing.
func TestADictAndSetLiteralTheCompilerCanReadKeepsItsStaticRoad(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"an int dict", "print({\"k\": 1})\n"},
		{"a text dict", "print({\"k\": \"v\"})\n"},
		{"a mixed dict", "print({\"k\": 1, 2: \"v\"})\n"},
		{"an int set", "print({1, 2})\n"},
		{"a text set", "print({\"a\"})\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Compile(tc.src)
			if err != nil {
				t.Fatalf("refused a program the reference prints: %v", err)
			}
			if strings.Contains(res.IR, "unhashable type: 'list'") {
				t.Errorf("a literal whose every kind is a constant asked the run time whether it can be a key")
			}
		})
	}
}

// TestAPairBoundDictAndSetStillRefuseThePositionsThatTakeOneWord is this cycle's honest half, and it is the
// measure of where the door stops. Two classes remain:
//
//   - a pair handed through a parameter, which is a different boundary (Gap R.154's family) — the callee's
//     parameter is not a pair, so the entry inside the body has no tag to carry;
//   - a fold whose operand the door will not label, or whose element set the source does not fix: a set
//     literal (CPython folds it in hash order, deduped), or a container literal as an operand. Both stay
//     refusals in words rather than becoming a number nobody asked for (Gap R.198, ADR 0316).
//
// The three fold rows that used to open this table — `sum([n])`, `min([n, 3])`, `max([n, 3])` — are paid by
// ADR 0316 and live in `pair_fold_test.go`; the MUTATION roads that used to be here are paid by ADR 0311 and
// live in `pair_mutation_test.go`, together with the value they still cannot carry (`d["k"] = xs[0] / 2`,
// which the ordinary road is asked about first and refuses in words).
// Each keeps the sentence that names the value's origin, the missing half and the roadmap row; exit 2 stays
// forbidden, because a refusal this backend emits is a diagnostic and not a compiler bug (ADR 0166).
func TestAPairBoundDictAndSetStillRefuseThePositionsThatTakeOneWord(t *testing.T) {
	for _, tc := range []struct {
		name, pre, body string
		must            []string
	}{
		{"a pair handed through a parameter into a dict", pairDictIntSlot, "def build(k):\n    return {\"k\": k}\nprint(build(n))\n", nil},
		{"a fold over a set literal", pairDictIntSlot, "print(min({n, 3}))\n", nil},
		// A container literal is an operand this door will not label. The sentence the ordinary sum road
		// writes is its own, and it names what the element IS rather than the missing tag, because there the
		// element's kind is a fact the compiler can see (Gap R.198).
		{"a container literal as a fold operand", pairDictIntSlot, "print(sum([n, [1]]))\n",
			[]string{"sum adds numbers", "list literal is a container", "Python raises TypeError"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := tc.pre + tc.body
			res, err := JIT(src, 0)
			if err == nil {
				t.Fatalf("answered %q where the door still does not reach: the position keeps one word for its operand", res.Output)
			}
			wants := tc.must
			if wants == nil {
				wants = []string{"(payload, tag) pair", "one word", "roadmap L11.1"}
			}
			for _, want := range wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not name the missing half (%q): %v", want, err)
				}
			}
			if strings.Contains(err.Error(), "loop over a mixed list") {
				t.Errorf("the refusal blames a loop for a program that may not have one (Gap R.38): %v", err)
			}
		})
	}
}
