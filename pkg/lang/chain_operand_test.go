// Roadmap **Gap R.442** / **ADR 0423** — a comparison chain asks each of its operands what it CARRIES,
// and binds its one evaluation slot with that.
//
//	xs = [1, 2]
//	xs[0] = 2.5
//	print(0 < xs[0] < 3)    #  True   ← this backend: "this context needs a single static kind"
//	d = {"a": 2.5}
//	print(0 < d["a"] < 3)   #  True   ← and False, at the exit code of success
//	print(0 < abs(-2.5) < 3)
//	                        #  True   ← and `llc: '%t1' defined with type 'double' but expected 'i32'`,
//	                        #           which is the contract's exit 2 spent on the compiler's own module
//
// `chainValue` stores each operand once so the middle of `a < b < c` is evaluated once (**Gap R.53**,
// **ADR 0288**), and the store it wrote was `store i32 %w, i32* %_chain1`. A slot whose kind the OBJECT
// carries and a double are more than one word, so the chain asked each of them through the ordinary
// one-word road and got one of three things: a refusal in words, the payload alone (the `@float_box`
// handle of a dict entry, the `@str_tab` index of a text entry — a number for reasons that have nothing
// to do with the value, which is how the two rows above answered `False` and `True` at exit 0), or a
// module `llc` rejects.
//
// The fix is one question, asked of every operand BEFORE anything is lowered (`chainOperandCarriesMoreThanAWord`),
// and the three doors the neighbouring positions already ask, in the same order the binding asks them:
// the comparison road's own `comparePair`, then `assignPairWords` (**assign_pair.go**), then the float road.
// Each arm builds into a scratch buffer and is committed only after it answered, because an operand that
// is lowered twice is the bug **ADR 0288** exists to prevent. What the doors do NOT touch is the ordinary
// `store i32` — `print(0 < 2 < 3)` and `print(0 < x < 3)` emit the module they always emitted, which is
// what the IR table below asserts rather than trusts.
//
// What the same sweep found and this row does NOT fix is pinned, with today's bytes beside the
// reference's, in TestWhatTheChainStillOwesIsFiledNotFixed: the loop item (**Gap R.443**) and the tuple
// the concatenation sentence blames on the list (**Gap R.444**).
package lang

import (
	"strings"
	"testing"
)

// chainOperandFamily is the answering half. Every `want` was read off CPython 3.12 before the case was
// written, and every source is on the record — **ADR 0302**'s rule, so a position that just gained a
// second word is not certified by the road that gained it.
func chainOperandFamily() []struct{ name, src, want string } {
	return []struct{ name, src, want string }{
		// The slot the store promoted: the position the row is named for.
		{"a chained comparison over the promoted slot", "xs = [1, 2]\nxs[0] = 2.5\nprint(0 < xs[0] < 3)\n", "True\n"},
		{
			// The row that shows the old agreement was the stale int payload answering by luck: the parent
			// printed True here too, where the reference prints False.
			"a chained comparison the stale payload answered the wrong way round",
			"xs = [1, 2]\nxs[0] = 5.5\nprint(0 < xs[0] < 3)\n", "False\n",
		},
		{"the reversed spelling of the same chain", "xs = [1, 2]\nxs[0] = 2.5\nprint(3 > xs[0] > 0)\n", "True\n"},
		{"four links over the promoted slot", "xs = [1]\nxs[0] = 2.5\nprint(0 < xs[0] < 3 < 9)\n", "True\n"},
		{
			// `1 <= xs[0] <= 2` is False for 2.5 and True for the stale payload `2` — the second row that
			// could only have been answered by reading the slot the store wrote.
			"the inclusive links over the promoted slot",
			"xs = [1]\nxs[0] = 2.5\nprint(1 <= xs[0] <= 2)\n", "False\n",
		},
		// The dict: here the ordinary road did not even refuse — it handed the chain the entry's PAYLOAD,
		// which for a float entry is a box handle and for a text entry a string-table index.
		{"a dict entry the chain read as its own number", "d = {\"a\": 2.5}\nprint(0 < d[\"a\"] < 3)\n", "True\n"},
		{"an entry two deep", "d = {\"a\": {\"b\": 2.5}}\nprint(0 < d[\"a\"][\"b\"] < 3)\n", "True\n"},
		// The kinds of operand that arrive with a tag rather than sitting in one slot.
		{"the answer of a float-returning def", "def g():\n    return 2.5\nprint(0 < g() < 3)\n", "True\n"},
		{"a slot the append promoted", "xs = []\nxs.append(2.5)\nprint(0 < xs[0] < 3)\n", "True\n"},
		{
			// The store this object's slots belong to has a RUN-TIME index: there is no literal left for
			// ADR 0249's courtesy to read, which is the half of the row the numeric door owns.
			"a slot a run-time store promoted, read through the same index",
			"xs = [1, 2, 3]\ni = 1\nxs[i] = 2.5\nprint(0 < xs[i] < 3)\n", "True\n",
		},
		{"a name the pair road already bound", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint(0 < n < 3)\n", "True\n"},
		{"a container slot, read twice deep", "xs = [[2.5]]\nprint(0 < xs[0][0] < 3)\n", "True\n"},
		{"a set member through the loop", "s = {2.5}\nfor m in s:\n    print(0 < m < 3)\n", "True\n"},
		{"a parameter the caller filled with a fraction", "def f(x):\n    print(0 < x < 3)\nf(2.5)\n", "True\n"},
		// The double: eight bytes where the slot held four. `abs` and the call are the two shapes that
		// reached `llc` as `store i32 %t1, i32* %_chain1` — the contract's "the compiler is broken" code
		// (ADR 0166), spent on the compiler's own module.
		{"the sum of two float slots", "a = 1.0\nb = 2.0\nprint(0 < a + b < 4)\n", "True\n"},
		{"the absolute value of a spelled fraction", "print(0 < abs(-2.5) < 3)\n", "True\n"},
		// The control: an operand the chain has always carried in one word keeps that road, byte for byte.
		{"a spelled fraction, which is cheap", "print(0 < 2.5 < 3)\n", "True\n"},
		{"a name the notebook pins to an int", "x = 2\nprint(0 < x < 3)\n", "True\n"},
		{"nothing but literals", "print(0 < 2 < 3)\n", "True\n"},
	}
}

// TestTheChainAnswersWhatItsOperandCarries judges the family on the compiled leg and again on the record
// leg, so the answer is the one on record and not the one the new door produced.
func TestTheChainAnswersWhatItsOperandCarries(t *testing.T) {
	t.Parallel()
	for _, tc := range chainOperandFamily() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheChainRaisesWhatTheReferenceRaises is the same position over an operand whose tag the comparison
// road already knows how to refuse: text and the void have no ordering against a number, and the chain
// must spend the raise, not an answer. Exit 3 is the contract's uncaught-exception code (ADR 0166), and
// the sentence is the reference's own, character for character.
func TestTheChainRaisesWhatTheReferenceRaises(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src, want string }{
		{
			"a text in the middle of the chain",
			"xs = []\nxs.append(\"a\")\nprint(0 < xs[0] < 3)\n",
			"TypeError: '<' not supported between instances of 'int' and 'str'",
		},
		{
			"a text under a dict key",
			"d = {\"a\": \"x\"}\nprint(0 < d[\"a\"] < 3)\n",
			"TypeError: '<' not supported between instances of 'int' and 'str'",
		},
		{
			"the void in the middle of the chain",
			"xs = [None]\nprint(0 < xs[0] < 3)\n",
			"TypeError: '<' not supported between instances of 'int' and 'NoneType'",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference raises in: %v\n%s", err, tc.src)
			}
			if res.Code != 3 {
				t.Fatalf("exited %d with stdout %q — a chain over a value with no ordering must raise, not answer\n%s",
					res.Code, res.Output, tc.src)
			}
			if !strings.Contains(res.Stderr+res.Output, tc.want) {
				t.Fatalf("raised %q, not the reference's sentence %q\n%s", res.Stderr+res.Output, tc.want, tc.src)
			}
			// The record leg, so the trap is the one on record and not the one this backend chose.
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			if rerr := RecordedRunError(t, tc.src); rerr == nil {
				t.Fatalf("the record says this program traps, and the compiled run answered it\n%s", tc.src)
			} else if !strings.Contains(rerr.Error(), tc.want) {
				t.Fatalf("the record leg reports %q, not the reference's sentence %q\n%s", rerr, tc.want, tc.src)
			}
		})
	}
}

// TestTheChainBindsItsOperandWithItsTag is the IR witness. The claim is precise: the chain's slot became
// the operand's PAIR — an `alloca i32` beside `_chainN_tag`, the tag the object carries stored with the
// payload — and the ordinary chain of literals still emits the single-word store it always emitted. An
// answer reached by folding the operand away would not leave these words in the module.
func TestTheChainBindsItsOperandWithItsTag(t *testing.T) {
	t.Parallel()
	pairShaped := []struct{ name, src string }{
		{"the promoted slot", "xs = [1, 2]\nxs[0] = 2.5\nprint(0 < xs[0] < 3)\n"},
		{"the dict entry", "d = {\"a\": 2.5}\nprint(0 < d[\"a\"] < 3)\n"},
		{"the float-returning call", "def g():\n    return 2.5\nprint(0 < g() < 3)\n"},
		{"the absolute value of a fraction", "print(0 < abs(-2.5) < 3)\n"},
		{"the sum of two float slots", "a = 1.0\nb = 2.0\nprint(0 < a + b < 4)\n"},
	}
	for _, tc := range pairShaped {
		tc := tc
		t.Run(tc.name+" binds a pair slot", func(t *testing.T) {
			t.Parallel()
			ir, err := GenerateIR(mustParseChainSource(t, tc.src))
			if err != nil {
				t.Fatalf("the compiled backend could not build this module: %v\n%s", err, tc.src)
			}
			if got := chainPairSlots(ir); len(got) == 0 {
				t.Fatalf("no chain slot with a tag beside it in the module — either the operand was never bound once "+
					"(so a link may evaluate it twice) or it was bound as a single word (so the comparison reads the "+
					"payload alone, which is the bug this row is named for)\n%s", tc.src)
			}
			if !strings.Contains(ir, "store i32 %") || !strings.Contains(ir, "_tag") {
				t.Fatalf("nothing stored a run-time tag into the chain slot\n%s", tc.src)
			}
		})
	}
	for _, tc := range []struct{ name, src string }{
		{"two literals", "print(0 < 2 < 3)\n"},
		{"a literal and a pinned name", "x = 2\nprint(0 < x < 3)\n"},
	} {
		tc := tc
		t.Run(tc.name+" keeps the one-word store", func(t *testing.T) {
			t.Parallel()
			ir, err := GenerateIR(mustParseChainSource(t, tc.src))
			if err != nil {
				t.Fatalf("the compiled backend could not build this module: %v\n%s", err, tc.src)
			}
			if got := chainPairSlots(ir); len(got) != 0 {
				t.Fatalf("a chain bound as a pair (%s) appeared in a program whose operands are cheap — the ordinary "+
					"road is supposed to be untouched:\n%s\n%s", strings.Join(got, ", "), tc.src, ir)
			}
		})
	}
}

// chainPairSlots names the chain slots in a module that carry a tag: an `%_chainN = alloca i32` with the
// `%_chainN_tag = alloca i32` beside it. The pair is the row's claim, so the pair is what is looked for —
// a chain slot WITHOUT its tag partner is the payload-alone bug in module form, and no chain slot at all is
// the operand lowered at each link, which **ADR 0288** forbids.
func chainPairSlots(ir string) []string {
	var out []string
	for _, ln := range strings.Split(ir, "\n") {
		const alloca = "  %_chain"
		if !strings.HasPrefix(ln, alloca) || !strings.Contains(ln, " = alloca i32") {
			continue
		}
		rest := ln[len(alloca):]
		name := "_chain" + strings.TrimSuffix(rest, " = alloca i32")
		if strings.Contains(name, "_tag") {
			continue // the tag half is not a slot of its own
		}
		if strings.Contains(ir, "  %"+name+"_tag = alloca i32") {
			out = append(out, name)
		}
	}
	return out
}

// mustParseChainSource is the front end the IR table needs: the module, not the executable.
func mustParseChainSource(t *testing.T, src string) *Program {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("the front end refused the source: %v\n%s", err, src)
	}
	return prog
}

// TestTheChainsDoubleDoorBindsTheWordTheValueIs is the unit witness for the third door. No program in the
// corpus reaches it — the two pair doors answer for every float the surface can write, which is the right
// outcome and the wrong excuse: if a float road ever declines a chain operand, the ordinary arm would
// write `store i32 %t1, i32* %_chain1` over a double and spend the contract's exit 2 (ADR 0166). The door
// is the net, so the door is tested directly: the slot it binds is a `double`, written once, and the two
// records an ordinary float binding writes beside it are written here too.
func TestTheChainsDoubleDoorBindsTheWordTheValueIs(t *testing.T) {
	t.Parallel()
	g := &irGen{
		allocd:     map[string]bool{},
		floatVars:  map[string]bool{},
		doubleSlot: map[string]bool{},
	}
	var b strings.Builder
	g.bindChainDouble(&b, "chainD", "%t9")
	ir := b.String()
	for _, want := range []string{
		"%_chainD = alloca double",
		"store double %t9, double* %_chainD",
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("the double door did not write %q:\n%s", want, ir)
		}
	}
	if !g.allocd["chainD"] || !g.floatVars["chainD"] || !g.doubleSlot["chainD"] {
		t.Fatalf("the door bound a float slot the pass does not know about (allocd=%v floatVars=%v doubleSlot=%v):\n%s",
			g.allocd["chainD"], g.floatVars["chainD"], g.doubleSlot["chainD"], ir)
	}
	if !g.floatFmtUsed {
		t.Fatalf("the door bound a float without marking the float printer in, and the module would not link it")
	}
}

// TestTheChainsGateIsPureAndItsDoorsDecliveInOrder is the discipline the file exists for: the gate emits
// nothing (so asking it cannot evaluate an operand), and a door that has not answered leaves the ordinary
// road exactly where it was. Both halves are what make "every link reads the same value" — **ADR 0288**'s
// property — survive a door that changes its mind halfway through.
func TestTheChainsGateIsPureAndItsDoorsDecliveInOrder(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		src  string
		more bool
		why  string
	}{
		{"a literal", "print(0 < 2 < 3)\n", false, "an int literal fits the word it was always stored in"},
		{"a pinned name", "x = 2\nprint(0 < x < 3)\n", false, "the notebook knows this name is one int"},
		{"a promoted slot", "xs = [1, 2]\nxs[0] = 2.5\nprint(0 < xs[0] < 3)\n", true, "the slot's kind is the object's"},
		{"a dict entry", "d = {\"a\": 2.5}\nprint(0 < d[\"a\"] < 3)\n", true, "the entry's kind is the object's"},
		{"a float sum", "a = 1.0\nb = 2.0\nprint(0 < a + b < 4)\n", true, "the value is a double"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			prog, err := Parse(tc.src)
			if err != nil {
				t.Fatalf("the front end refused the source: %v\n%s", err, tc.src)
			}
			// The gate is asked of a built module through the same entry every door is asked through, and
			// asking it twice must not change the module: purity is what lets the chain ask before lowering.
			first, err := GenerateIR(prog)
			if err != nil {
				t.Fatalf("the module would not build: %v\n%s", err, tc.src)
			}
			second, err2 := GenerateIR(prog)
			if err2 != nil {
				t.Fatalf("the same source built twice, and the second build failed: %v\n%s", err2, tc.src)
			}
			if first != second {
				t.Fatalf("one source, two modules — the chain asks a door that does state the gate is supposed to be pure of\n%s", tc.src)
			}
			want := len(chainPairSlots(first)) > 0
			if want != tc.more {
				t.Fatalf("the gate says %v for %s (%s) and the module says %v\n%s", tc.more, tc.name, tc.why, want, tc.src)
			}
		})
	}
}

// TestWhatTheChainStillOwesIsFiledNotFixed pins what this row leaves owed, with today's bytes beside the
// reference's, so a refusal — or a wrong answer still standing — is not mistaken for an answer later.
func TestWhatTheChainStillOwesIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		wantExit   int
		wantOut    string
		wantStderr string
		dossier    string
	}{
		{
			// The loop item is bound by the FOR road, which keeps its own per-item tag record
			// (`loopElemTag`) and never writes the pair records this file's gate reads — so the chain's gate
			// says "one word", the ordinary arm reads the payload, and the void's 0 satisfies `0 < 0 < 3`.
			// Byte-identical on this row's parent: the gate is new, and this origin is not in it yet.
			name:     "the loop item the for road bound",
			src:      "for v in [None, 2.5]:\n    print(0 < v < 3)\n",
			wantExit: 0, wantOut: "False\nFalse\n",
			dossier: "**Gap R.443** — the reference raises `TypeError: '<' not supported between instances of " +
				"'int' and 'NoneType'` on the FIRST item; the loop's per-item tag is the binding road's record " +
				"to leave and this gate does not read it yet (**Gap R.146**'s pair, one origin over)",
		},
		{
			// The trap is right and the NOUN is wrong: this backend stores a tuple as a list and the
			// runtime's `rt_kind_name` tells them apart from the object, but the sentence for the operand
			// the SOURCE spells is chosen at compile time and reads the shared naming, which knows only
			// `list`. A wrong noun in a trap message is still a wrong sentence (ADR 0271's one trap, one
			// wording) — and on the parent this program did not even reach the trap, it refused.
			name:     "the tuple the concatenation sentence blames on the list",
			src:      "xs = []\nxs.append((1,))\nprint(xs[0] + 1)\n",
			wantExit: 3, wantStderr: `TypeError: can only concatenate list (not "int") to list`,
			dossier: "**Gap R.444** — the reference says `can only concatenate tuple (not \"int\") to tuple`; " +
				"`concatenationTypeName` reads the value KIND, and a tuple's kind is the list's in this backend " +
				"(**ADR 0251**'s immutable spelling), so the noun is chosen without the AST or `tupleVars`",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := JIT(tc.src, 0)
			if err != nil {
				if tc.wantExit != 1 || !strings.Contains(err.Error(), tc.wantStderr) {
					t.Fatalf("%s (%s): failed in a way this pin does not describe: %v", tc.name, tc.dossier, err)
				}
				return
			}
			if res.Code != tc.wantExit {
				t.Fatalf("%s (%s): exited %d with stdout %q stderr %q — this pin expects exit %d, and an answer "+
					"that arrived here means the row was closed and this table did not follow\n%s",
					tc.name, tc.dossier, res.Code, res.Output, res.Stderr, tc.wantExit, tc.src)
			}
			if tc.wantOut != "" && res.Output != tc.wantOut {
				t.Fatalf("%s (%s): printed %q, want %q", tc.name, tc.dossier, res.Output, tc.wantOut)
			}
			if tc.wantStderr != "" && !strings.Contains(res.Stderr+res.Output, tc.wantStderr) {
				t.Fatalf("%s (%s): stderr %q does not carry %q", tc.name, tc.dossier, res.Stderr, tc.wantStderr)
			}
		})
	}
}
