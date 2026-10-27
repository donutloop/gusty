package integration

import (
	"strings"
	"testing"
)

// This file is the reference leg of roadmap **Gap R.422** (**ADR 0425**): removing a member of a set
// asks the ARGUMENT what it carries, and the two verbs — `discard`, which is silent about a member that
// isn't there, and `remove`, which says so — ask it the same question in the same words.
//
// The unit file (pkg/lang/set_member_removal_test.go) pins what the roads emit: the pair door, the scan
// and the shift, the unhashable gate that answers before the set is consulted, and the sentence that
// names the member. This one asks the only oracle left since ADR 0302 — CPython — what each program
// prints and each raise says, and requires the compiled toolchain to answer the same bytes and the same
// sentence. Where an answer is still owed it is pinned with today's bytes beside the reference's and the
// row that owns the fix is named, so a later fix fails the pin instead of quietly leaving it behind.
//
// The row's shape, measured on the row's parent at exit 0: `s.discard("a")` over `{1, 2}` printed `{1}`
// — a text the set never held deleted the member whose payload happened to equal the text's `@str_tab`
// index — and `s.remove(9)` was refused outright at exit 1 by the string-method road's
// name-holds-container gate, where the reference answers `KeyError: 9`.

// TestRemovingASetMemberAtTheCLI is the answering half: every expectation below is the bytes the live
// reference printed for the same file, asked in the same test, so the compiled leg cannot certify itself
// (ADR 0302, ADR 0425).
func TestRemovingASetMemberAtTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		// The row's own lesion: an argument the set has never held, removing a member it does hold.
		{"a_text_the_set_never_held", "s = {1, 2}\ns.discard(\"a\")\nprint(s)\n"},
		{"a_number_the_set_never_held", "s = {1, 2}\ns.discard(9)\nprint(s)\n"},
		{"the_void_which_carries_no_payload", "s = {0, 1}\ns.discard(None)\nprint(s)\n"},
		{"a_verdict_the_set_never_held", "s = {1, 2}\ns.discard(False)\nprint(s)\n"},
		{"a_fraction_the_set_never_holds", "s = {1, 2}\ns.discard(2.5)\nprint(s)\n"},
		// The member that is there, removed by both verbs.
		{"discard_takes_the_text_out", "s = {1, 2, \"a\"}\ns.discard(\"a\")\nprint(s)\n"},
		{"remove_takes_the_text_out", "s = {1, 2, \"a\"}\ns.remove(\"a\")\nprint(s)\n"},
		{"remove_takes_the_number_out", "s = {1, 2}\ns.remove(1)\nprint(s)\n"},
		{"a_fractional_member_is_found_by_value", "s = {1, 2}\ns.remove(1.0)\nprint(s)\n"},
		{"a_verdict_is_the_number_the_reference_says_it_is", "s = {1, 2}\ns.remove(True)\nprint(s)\n"},
		{"zero_is_not_the_void", "s = {0, 1}\ns.remove(0)\nprint(s)\n"},
		{"every_member_gone_then_the_empty_spelling", "s = {1, 2}\ns.discard(1)\ns.discard(2)\nprint(s)\n"},
		// The set grown by `add` — the two verbs have to agree about which pair each member carries.
		{"a_built_member_removed_as_it_was_added", "s = {1, 2}\ns.add(3)\ns.discard(3)\nprint(s)\n"},
		{"a_built_text_removed_as_it_was_added", "s = {1, 2}\ns.add(\"a\")\ns.discard(\"a\")\nprint(s)\n"},
		{"a_built_text_does_not_remove_a_number", "s = {1, 2}\ns.add(\"b\")\ns.discard(1)\nprint(s)\n"},
		{"the_built_member_the_extra_verb", "s = {1, 2}\ns.add(3)\ns.discard(3)\ns.discard(9)\nprint(s)\nprint(len(s))\n"},
		{"the_emptied_built_set", "s = set()\ns.add(1)\ns.discard(1)\ns.discard(9)\nprint(len(s))\n"},
		// The roads that read the set back rather than print it.
		{"the_set_read_back_by_membership", "s = {1, 2}\ns.discard(1)\nprint(len(s))\nprint(1 in s)\nprint(2 in s)\n"},
		{"the_survivors_after_two_verbs", "s = {1, 2, \"a\"}\ns.remove(\"a\")\ns.remove(1)\nprint(s)\n"},
		// An alias is the same object.
		{"the_alias_carries_the_removal", "s = {1, 2, 3}\nt = s\nt.discard(2)\nprint(t)\nprint(s)\n"},
		{"the_alias_carries_the_other_verb", "s = {1, 2, 3}\nt = s\nt.remove(2)\nprint(s)\nprint(t)\n"},
		// The member is an item of a loop rather than a literal written at the call.
		{"discard_removes_a_loop_item", "s = {1, 2, 3}\nfor k in [1, 3]:\n    s.discard(k)\nprint(s)\n"},
		{"remove_takes_a_loop_item_out", "s = {1, 2, 3}\nfor k in [1, 3]:\n    s.remove(k)\nprint(s)\n"},
		{"a_loop_of_texts_removes_only_what_it_names", "s = {1, 2}\nfor k in [\"a\", \"b\"]:\n    s.discard(k)\nprint(s)\n"},
		{"the_item_of_a_run_time_list", "s = {\"a\", \"b\"}\nks = [\"a\"]\nfor k in ks:\n    s.discard(k)\nprint(s)\n"},
		// The item's KIND, not its slot word, decides the removal. A fractional item is not the number its
		// payload looked like once the unroll stored it: this program is **ADR 0425**'s owed pin, which printed
		// `1` (the pair door was handed a 1.0 and removed a member the reference leaves alone) where the
		// reference prints `2`, and it answers `2` from the moment the removal asks the unroll's per-item tag
		// (**Gap R.416**, **ADR 0426**).
		{"a_fractional_item_is_not_the_number_its_slot_looked_like", "s = {1, 2}\nfor k in [1.5]:\n    s.discard(k)\nprint(len(s))\n"},
		// The exemption the reference grants this door and the dict door does not have.
		{"discard_says_nothing_about_a_set", "s = {1, 2}\ns.discard({1})\nprint(s)\n"},
		{"discard_of_the_empty_spelling", "s = {1, 2}\ns.discard(set())\nprint(s)\n"},
		{"a_tuple_member_the_set_does_not_hold", "s = {1, 2}\ns.discard((1, 2))\nprint(s)\n"},
		// The control: the verb that grows the set is untouched by this row.
		{"add_still_grows_the_set", "s = {1}\ns.add(2)\ns.add(2)\nprint(s)\nprint(len(s))\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "set_remove.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode != 0 {
				t.Fatalf("the reference exited %d on a program meant to answer:\n%s", refCode, refOut)
			}
			for _, engine := range cliEngines {
				combined := cliRun(t, engine, path)
				if _, code := cliRunCode(t, engine, path); code != 0 {
					t.Errorf("%s exited %d on a program the reference answers: %s", engine, code, combined)
				}
				if combined != refOut {
					t.Errorf("%s printed %q, the reference printed %q", engine, combined, refOut)
				}
			}
		})
	}
}

// TestARemovedMemberRaisesTheReferenceSentenceThroughTheCLI is the raise half, judged on the two things a
// caller sees: the exit code, and the last line of what the runtime wrote. The reference's sentence is
// asked of the live reference inside the case — one trap, one wording (**ADR 0271**), and the row that
// owns this door has two sentences the dict door does not: a bare set is a MISSING member here, and a
// member the language refuses to hash raises before the set is consulted (**Gap R.217**).
func TestARemovedMemberRaisesTheReferenceSentenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"discard_of_a_list", "s = {1, 2}\ns.discard([1])\n"},
		{"remove_of_a_list", "s = {1, 2}\ns.remove([1])\n"},
		{"discard_of_a_dict", "s = {1, 2}\ns.discard({\"a\": 1})\n"},
		{"remove_of_a_dict", "s = {1, 2}\ns.remove({\"a\": 1})\n"},
		{"a_tuple_carrying_a_list_names_the_list", "s = {1, 2}\ns.remove((1, [2]))\n"},
		{"a_tuple_carrying_a_set_names_the_set", "s = {1, 2}\ns.discard((1, {2}))\n"},
		{"remove_of_a_bare_set_names_the_member", "s = {1, 2}\ns.remove({1})\n"},
		{"remove_of_the_empty_spelling", "s = {1, 2}\ns.remove(set())\n"},
		{"remove_names_the_missing_number", "s = {1, 2}\ns.remove(9)\n"},
		{"remove_names_the_missing_text", "s = {\"a\"}\ns.remove(\"z\")\n"},
		{"remove_names_the_void", "s = {1, 2}\ns.remove(None)\n"},
		{"remove_names_the_verdict", "s = {1, 2}\ns.remove(False)\n"},
		{"remove_of_an_emptied_set", "s = {1, 2}\ns.remove(1)\ns.remove(1)\n"},
		{"the_loop_item_that_is_not_a_member", "s = {1, 2, 3}\nfor k in [1, 2, 9]:\n    s.remove(k)\n"},
		{"the_set_that_holds_nothing", "s = set()\ns.remove(1)\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "set_remove_trap.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if refCode == 0 {
				t.Fatalf("the reference exited 0 on a program meant to raise: %q — re-measure the row", refOut)
			}
			want := lastNonEmptyLine(refOut)
			if !strings.Contains(want, "Error") {
				t.Fatalf("the reference's last line is %q, not a trap sentence — re-measure the row", want)
			}
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code != 3 {
				t.Fatalf("exit %d, want 3 (the uncaught-exception exit)\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if out := strings.TrimSpace(stdout); out != "" {
				t.Fatalf("%q printed %q on the way to the raise; the reference prints nothing", tc.src, out)
			}
			got := lastNonEmptyLine(stderr)
			if got != want {
				t.Fatalf("the compiled leg says %q, the reference says %q\n%s", got, want, tc.src)
			}
		})
	}
}

// TestSetMemberRemovalKeepsTheCompilerOutOfIt is the exit-code contract for the half this row turned from
// a refusal into an answer: the parent's build refused every `remove` on a name-bound set at exit 1,
// which is the code a COMPILE refusal owns (**ADR 0166**) — and exit 2 is a compiler bug, which no program
// here is allowed to reach.
func TestSetMemberRemovalKeepsTheCompilerOutOfIt(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"remove_on_a_name_bound_set", "s = {1, 2}\ns.remove(1)\nprint(s)\n"},
		{"remove_of_a_loop_item", "s = {1, 2, 3}\nfor k in [1, 3]:\n    s.remove(k)\nprint(s)\n"},
		{"remove_through_an_alias", "s = {1, 2, 3}\nt = s\nt.remove(2)\nprint(s)\n"},
		{"remove_on_a_built_set", "s = set()\ns.add(1)\ns.remove(1)\nprint(len(s))\n"},
		{"discard_of_a_name_bound_member", "s = {1, 2}\nx = 2\ns.discard(x)\nprint(s)\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code == 2 {
				t.Fatalf("the toolchain rejected a program the reference answers (exit 2, ADR 0166):\n%s", stderr)
			}
			if code == 1 && strings.Contains(stderr, "codegen") {
				t.Fatalf("the compiled leg refused at exit 1 a program the reference answers: %s\nstdout %q", firstLine(stderr), stdout)
			}
			if code != 0 {
				t.Fatalf("exit %d on a program the reference answers\nstdout %q\nstderr %q", code, stdout, stderr)
			}
		})
	}
}

// TestTheSetMemberRemovalRoadsAnswerWrongArePinnedBesideTheReference holds what this row leaves owed.
// Every `todayBytes`/`todayExit` is what THIS backend does, measured 2026-10-27 against a build of the
// row's parent as well as this one, and every `reference` is what CPython prints for the same file, asked
// inside the case. An answer that arrives here fails the case, and the failure text says to delete the
// row and close the named one.
func TestTheSetMemberRemovalRoadsAnswerWrongArePinnedBesideTheReference(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		src        string
		todayExit  int
		todayBytes string
		reference  string
		owedBy     string
	}{
		{
			// The printed order of a set is this backend's insertion order and the reference's hash-slot
			// order. The removal under the print is right — three members, the number among them, the two
			// texts untouched — and the line it leaves is Gap R.170's.
			name: "the_printed_order_of_a_mixed_set", src: "s = {\"0\", \"1\"}\ns.discard(0)\nprint(s)\n",
			todayExit: 0, todayBytes: "{'0', '1'}\n", reference: "{'1', '0'}\n",
			owedBy: "Gap R.170 (a set prints in insertion order; the reference prints hash-slot order)",
		},
		{
			// The member is a name whose value the compiler cannot quote, so the sentence loses the half
			// that makes it findable. The dict door beside this one says the same thing for the same key.
			name: "a_name_bound_to_a_tuple_keeps_the_generic_sentence", src: "t = (3, 4)\ns = {1, 2}\ns.remove(t)\n",
			todayExit: 3, todayBytes: "KeyError: key not found", reference: "KeyError: (3, 4)",
			owedBy: "Gap R.189 (naming a key the compiler cannot see: the same sentence at the dict door)",
		},
		// The loop item whose payload was a box handle is answered by the item's own kind as of **ADR 0426** and
		// has MOVED to `TestRemovingASetMemberAtTheCLI` with the reference's bytes; what the same loop item
		// still owes — an ordering, a container comparison, an interpolation — is **Gap R.448**, **Gap R.450**
		// and **Gap R.445**, each pinned in the loop-item suite that owns it.
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, t.TempDir(), "set_remove_owed.gy", tc.src)
			refOut, refCode := oracleTrap(t, path)
			if tc.reference != "" && !strings.Contains(refOut, tc.reference) {
				t.Fatalf("this pin's reference column is stale: CPython said %q, the pin says %q", refOut, tc.reference)
			}
			if tc.todayExit == 3 && refCode == 0 {
				t.Fatalf("the reference exited 0 on a program meant to raise: %q — re-measure the row", refOut)
			}
			stdout, stderr, code := cliStreams(t, "--eval", tc.src)
			if code != tc.todayExit {
				t.Fatalf("exit %d, this pin expects %d — an answer that arrived here means the row was closed and this table did not follow (%s)\nstdout %q\nstderr %q",
					code, tc.todayExit, tc.owedBy, stdout, stderr)
			}
			got := stdout
			if tc.todayExit != 0 {
				got = lastNonEmptyLine(stderr)
			}
			if got != tc.todayBytes {
				t.Fatalf("this pin's bytes are stale: the compiled leg says %q, the pin says %q (%s)", got, tc.todayBytes, tc.owedBy)
			}
			if got == strings.TrimSpace(tc.reference) {
				t.Fatalf("the owed case now answers the reference — move it into the answering table (%s)", tc.owedBy)
			}
		})
	}
}

// lastNonEmptyLine is the sentence a caller greps: the traceback's last line, from either stream.
func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return strings.TrimSpace(lines[i])
		}
	}
	return ""
}
