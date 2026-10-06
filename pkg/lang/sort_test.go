package lang

import (
	"strings"
	"testing"
)

// Sorting is the L11.7 "functions are values that compile" feature, and the one the boring-program
// sweep found (ADR 0190): neither backend had xs.sort() / xs.reverse(), and the compiled path
// diagnosed `xs.sort()` as a *string* method — the call fell through to string-method dispatch and
// told the user their list was a string. The three properties these tests hold: the comparator for
// interned strings reads the TEXT; sorted(xs) copies while xs.sort() mutates; and a mixed list is
// refused, not silently ordered.

func compileSrc(t *testing.T, src string) string {
	t.Helper()
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	return res.IR
}

// TestSortAndReverseCallTheRuntimeHelpers is the call-site contract: the method lowers to the
// runtime helper over the variable's handle, and nothing else.
func TestSortAndReverseCallTheRuntimeHelpers(t *testing.T) {
	sortMod := compileSrc(t, "xs = [3, 1, 2]\nxs.sort()\nprint(xs)\n")
	for _, want := range []string{"@rt_sort(", "call void @rt_sort(i32 %h", "rt_print_list"} {
		if !strings.Contains(sortMod, want) {
			t.Fatalf("compiled xs.sort() missing %q:\n%s", want, sortMod)
		}
	}
	if strings.Contains(sortMod, "string method sort") {
		t.Fatal("a list method must not reach string-method dispatch")
	}
	revMod := compileSrc(t, "xs = [1, 2, 3]\nxs.reverse()\nprint(xs)\n")
	if !strings.Contains(revMod, "call void @rt_reverse(i32 %h") {
		t.Fatalf("compiled xs.reverse() missing rt_reverse:\n%s", revMod)
	}
}

// TestStringSortComparesTextNotInternedIndex is the whole point of mode 1: an interned string is an
// index into @str_tab, so ordering by payload orders by the order strings first appeared in the
// program, which agrees with alphabetical order often enough to survive a spot check.
func TestStringSortComparesTextNotInternedIndex(t *testing.T) {
	mod := compileSrc(t, "ys = [\"pear\", \"apple\", \"fig\"]\nys.sort()\nprint(ys)\n")
	i := strings.Index(mod, "define internal i32 @rt_elem_gt")
	if i < 0 {
		t.Fatal("rt_elem_gt missing")
	}
	helper := mod[i : strings.Index(mod[i:], "\n}\n")+i+3]
	if !strings.Contains(helper, "@str_tab") || !strings.Contains(helper, "@strcmp") {
		t.Fatalf("the string comparator must load the text and strcmp it:\n%s", helper)
	}
	nums := helper[strings.Index(helper, "nums:"):strings.Index(helper, "strs:")]
	if !strings.Contains(nums, "icmp sgt i32 %a, %b") {
		t.Fatalf("the numeric comparator must compare payloads signed:\n%s", helper)
	}
}

// TestSortMovesTagsWithPayloads applies the ADR 0187 pairing rule to the sort itself: an exchange
// that moves payloads and leaves tags behind mislabels the list it sorted.
func TestSortMovesTagsWithPayloads(t *testing.T) {
	mod := compileSrc(t, "xs = [3, 1, 2]\nxs.sort()\nprint(xs)\n")
	i := strings.Index(mod, "define internal void @rt_sort")
	if i < 0 {
		t.Fatal("rt_sort missing")
	}
	helper := mod[i : strings.Index(mod[i:], "\n}\n")+i+3]
	for _, want := range []string{"@heap_tags", "store i32 %gtb, i32* %ta", "store i32 %gta, i32* %tb"} {
		if !strings.Contains(helper, want) {
			t.Fatalf("rt_sort must swap tags with payloads (%s missing):\n%s", want, helper)
		}
	}
	// Same rule for reverse, which is the same exchange seen from the other end.
	rev := mod[strings.Index(mod, "define internal void @rt_reverse"):]
	rev = rev[:strings.Index(rev, "\n}\n")+3]
	if !strings.Contains(rev, "@heap_tags") {
		t.Fatalf("rt_reverse must swap tags with payloads:\n%s", rev)
	}
}

// TestSortedCopiesAndSortsTheCopy is the visible difference between the builtin and the method.
func TestSortedCopiesAndSortsTheCopy(t *testing.T) {
	mod := compileSrc(t, "ws = [3, 1, 2]\nprint(sorted(ws))\nprint(ws)\n")
	if !strings.Contains(mod, "  %h") || !strings.Contains(mod, " = call i32 @rt_list_copy(i32 %") {
		t.Fatalf("sorted(xs) on a variable must sort a copy:\n%s", mod)
	}
	if strings.Count(mod, "rt_print_list") < 2 {
		t.Fatalf("both the copy and the original must print:\n%s", mod)
	}
	// A literal argument is already freshly allocated by the builder, so the copy would be
	// pure waste; the fold path may also answer this shape entirely at compile time, which is
	// why the assertion is about the copy, not about which strategy ran.
	lit := compileSrc(t, "print(sorted([10, 2, 33]))\n")
	if strings.Contains(lit, " = call i32 @rt_list_copy(") {
		t.Fatalf("sorted() of a literal must not copy a list nobody else can see:\n%s", lit)
	}
}

// TestSortedOfStringsIsNotFoldedByTheIntPath: the fold handles constant ints only, so a string
// literal list must reach the runtime path rather than refuse.
func TestSortedOfStringsIsNotFoldedByTheIntPath(t *testing.T) {
	mod := compileSrc(t, "print(sorted([\"pear\", \"apple\"]))\n")
	if !strings.Contains(mod, "call void @rt_sort(i32 %") {
		t.Fatalf("sorted() over strings must reach the runtime sort:\n%s", mod)
	}
}

// TestMixedKindSortRefusesNamesTheRule: Python raises TypeError for a str/int mix instead of
// inventing an order, and the compiled backend reports it rather than answering by payload.
func TestMixedKindSortRefusesNamesTheRule(t *testing.T) {
	for _, src := range []string{
		"xs = [1, \"a\"]\nxs.sort()\nprint(xs)\n",
		"print(sorted([1, \"a\"]))\n",
	} {
		res, err := Compile(src)
		if err == nil {
			t.Fatalf("sorting a mixed-kind list must not compile:\n%s", src)
		}
		joined := err.Error()
		if res != nil {
			for _, d := range res.Diagnostics {
				joined += d.Msg + "\n"
			}
		}
		if !strings.Contains(joined, "more than one kind") || !strings.Contains(joined, "TypeError") {
			t.Fatalf("the refusal must name the rule and Python's answer:\n%s", joined)
		}
	}
}

// TestSortRejectsKeyAndReverseArgs: key= and reverse= need first-class functions (L11.7's other
// half), so the refusal has to say that rather than pretend the argument was a count.
func TestSortRejectsKeyAndReverseArgs(t *testing.T) {
	res, err := Compile("xs = [1, 2]\nxs.sort(key=len)\nprint(xs)\n")
	if err == nil {
		t.Fatal("sort(key=...) must not compile yet")
	}
	joined := err.Error()
	if res != nil {
		for _, d := range res.Diagnostics {
			joined += d.Msg + "\n"
		}
	}
	if !strings.Contains(joined, "first-class functions") {
		t.Fatalf("the refusal should own the reason: %s", joined)
	}
}

// TestSortingBehaviourInterpreted is the behavioural half in the interpreter: the method mutates
// and the builtin does not, which is the property a program can see. (print(xs.sort()) rendering
// as None is the bool/None repr debt pinned by probe_bool_value, so these check the list.)
func TestSortingBehaviourInterpreted(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{"xs = [3, 1, 2]\nxs.sort()\nprint(xs)\n", "[1, 2, 3]\n"},
		{"xs = [1, 2, 3]\nxs.reverse()\nprint(xs)\n", "[3, 2, 1]\n"},
		{"xs = [3, 1, 2]\nys = sorted(xs)\nprint(ys)\nprint(xs)\n", "[1, 2, 3]\n[3, 1, 2]\n"},
		{"xs = [\"pear\", \"apple\"]\nxs.sort()\nprint(xs)\n", "['apple', 'pear']\n"},
		{"xs = []\nxs.append(4)\nxs.append(1)\nxs.sort()\nprint(xs)\n", "[1, 4]\n"},
		{"print(sorted([3, 1, 2], reverse=True))\n", "[3, 2, 1]\n"},
	} {
		res, err := JIT(tc.src, 0)
		if err != nil {
			t.Fatalf("%s: %v", tc.src, err)
		}
		if res.Output != tc.want {
			t.Fatalf("%s\n got %q want %q", tc.src, res.Output, tc.want)
		}
	}
}
