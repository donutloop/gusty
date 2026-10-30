package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// object_membership_needle_test.go — a membership test whose needle is a read out of a container, through the
// CLI (roadmap **Gap R.582**, **Gap R.583**, **ADR 0476**).
//
// The program that opened the row is three lines long and prints one word:
//
//	$ gustyc --eval 'd = {"a": "a", "b": 2}
//	  print(d["a"] in d)'
//	False
//	$ python3 -c 'd = {"a": "a", "b": 2}
//	print(d["a"] in d)'
//	True
//
// with exit 0 on both, which is the shape this house treats as the worst answer a compiler can give: the
// reference runs it, this backend ran it too, and the two disagree at the code that says the program worked.
// The read was half-asked: `rt_dict_get_tagged` answered the needle's payload and the compiler answered its
// kind with a literal 0 — `TagInt` — so a text was looked up among text keys as a number and the scan reported
// absence. **Gap R.286** is the same defect for a dict's KEY and was fixed there; **ADR 0474** opened this door
// for the containers the scan claims and asked the pair only when the fold declined, and the fold never
// declines an entry it cannot see into — it answers 0 and moves on.
//
// Every expectation below is read from a live `python3` in the case, not from this file; the trap rows check
// the raise's class, its message word for word, its exit class (3, not 2 — **ADR 0166**) and that the
// program's own `except` reaches it.

// TestTheObjectMembershipNeedleAnswersTheReferenceThroughTheCLI is the answering family at the CLI.
func TestTheObjectMembershipNeedleAnswersTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"d = {\"a\": \"a\", \"b\": 2}\nprint(d[\"a\"] in d)",
		"d = {\"a\": \"a\", \"b\": 2}\nprint(d[\"a\"] not in d)",
		"d = {\"a\": 1, \"b\": 2.5}\nprint(d[\"b\"] in [2.5])",
		"d = {\"a\": 1, \"b\": 2.5}\nprint(d[\"b\"] in {2.5})",
		"d = {\"a\": 1, \"b\": [2]}\nprint(d[\"b\"] in [[2]])",
		"d = {\"a\": 1, \"b\": {\"k\": 1}}\nprint(d[\"b\"] in [{\"k\": 1}])",
		"d = {\"a\": 1, \"b\": (1, 2)}\nprint(d[\"b\"] in [(1, 2)])",
		"d = {\"a\": 1, \"b\": True}\nprint(d[\"b\"] in [True])",
		"d = {\"a\": 1, \"b\": None}\nprint(d[\"b\"] in [None])",
		"def g():\n    return [7, \"z\"]\n\nxs = [g()[1], 3]\nprint(xs[0] in xs)",
		"def g():\n    return [7, \"z\"]\n\nxs = [g()[1], 3]\nprint(xs[0] not in xs)",
		"xs = []\nxs.append({\"k\": [1]})\nprint(xs[0][\"k\"] in [[1]])",
		// The negatives that must stay negative: answering True to either would be the ADR 0232 collision.
		"d = {\"a\": \"x\", \"b\": 1}\nprint(d[\"a\"] in {\"a\": 1})",
		"d = {\"a\": 1, \"b\": 2}\nprint(d[\"b\"] in d)",
		"xs = [1, \"a\"]\nprint(\"a\" in xs)",
		"print(1 in [1, 2, 3])",
	} {
		t.Run(strings.ReplaceAll(src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			want := pythonOutput(t, src+"\n")
			stdout, stderr, code := cliStreams(t, "--eval", src)
			if code == 2 {
				t.Fatalf("the toolchain failed on its own program (exit 2 is ADR 0166's compiler-bug code):\n%s", stderr)
			}
			if code != 0 {
				t.Fatalf("exit %d, want 0: the reference answers this program\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if got := strings.TrimSpace(stdout); got != strings.TrimSpace(want) {
				t.Fatalf("printed %q, the reference prints %q", got, strings.TrimSpace(want))
			}
		})
	}
}

// The unhashable family: a container asked of a dict is not an absent key, and asked of a set is not an
// absent member. The needle's tag carries the question, because for a needle the compiler cannot see into the
// tag is the only statement in the module about what it is.
func TestAnUnhashableObjectNeedleRaisesThroughTheCLI(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, src, sentence string
	}{
		{"a list read out of a dict, asked of that dict", "d = {\"a\": 1, \"b\": [2]}\nprint(d[\"b\"] in d)", "unhashable type: 'list'"},
		{"the same needle, negated", "d = {\"a\": 1, \"b\": [2]}\nprint(d[\"b\"] not in d)", "unhashable type: 'list'"},
		{"a dict read out of a dict", "d = {\"a\": 1, \"b\": {\"k\": 1}}\nprint(d[\"b\"] in d)", "unhashable type: 'dict'"},
		{"a list read out of a dict, asked of a set literal", "d = {\"a\": 1, \"b\": [2]}\nprint(d[\"b\"] in {1, 2})", "unhashable type: 'list'"},
		{"…and of a set the program built", "d = {\"a\": 1, \"b\": [2]}\ns = {1, 2}\nprint(d[\"b\"] in s)", "unhashable type: 'list'"},
		{"…and of a dict the source spells", "d = {\"a\": 1, \"b\": [2]}\nprint(d[\"b\"] in {\"k\": 1})", "unhashable type: 'list'"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stdout, stderr, code := cliStreams(t, "--eval", tt.src)
			if code == 2 {
				t.Fatalf("exit 2 is a compiler bug; the reference raises here, so the program must fail at 3:\n%s", stderr)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap's 3\nstdout %q\nstderr %q", code, stdout, stderr)
			}
			if !strings.Contains(stderr, "TypeError: "+tt.sentence) {
				t.Fatalf("the raise does not carry the reference's own sentence %q:\n%s", tt.sentence, stderr)
			}
			caught := tt.src[:strings.LastIndex(tt.src, "\n")] +
				"\ntry:\n    " + tt.src[strings.LastIndex(tt.src, "\n")+1:] + "\nexcept TypeError:\n    print(\"caught\")"
			out, _, c := cliStreams(t, "--eval", caught)
			if c != 0 || strings.TrimSpace(out) != "caught" {
				t.Fatalf("the raise is not catchable from the program's own handler (exit %d, out %q)", c, out)
			}
		})
	}
}

// The module table: the membership call's needle tag is a register the object answered, for every shape this
// row re-ordered. A constant tag is the compiler stating an opinion about a value it has not looked at, and
// the value rows would all stay green while it came back — which is why the shape is asserted here too.
func TestTheObjectMembershipNeedleModuleAsksTheObjectForTheKind(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		src, call string
	}{
		{"d = {\"a\": \"a\", \"b\": 2}\nprint(d[\"a\"] in d)", "@rt_dict_has_tagged("},
		{"d = {\"a\": 1, \"b\": 2.5}\nprint(d[\"b\"] in [2.5])", "@rt_contains_tagged("},
		{"def g():\n    return [7, \"z\"]\n\nxs = [g()[1], 3]\nprint(xs[0] in xs)", "@rt_contains_tagged("},
	} {
		t.Run(strings.ReplaceAll(tt.src, "\n", "; "), func(t *testing.T) {
			t.Parallel()
			ir, stderr, code := cliStreams(t, "--emit-llvm", tt.src)
			if code != 0 {
				t.Fatalf("--emit-llvm exited %d:\n%s", code, stderr)
			}
			if !strings.Contains(ir, "call i32 "+tt.call) {
				t.Fatalf("the module never asks %s:\n%s", tt.call, ir)
			}
			// Every payload the object read answered, and the membership calls that take one.
			var payloads []string
			for _, ln := range strings.Split(ir, "\n") {
				if (strings.Contains(ln, "call i32 @rt_dict_get_tagged(") || strings.Contains(ln, "call i32 @rt_get_elem(")) &&
					strings.Contains(ln, " = ") {
					payloads = append(payloads, strings.TrimSpace(ln[:strings.Index(ln, " = ")]))
				}
			}
			if len(payloads) == 0 {
				t.Fatalf("no slot was read out of an object; the row's premise is wrong:\n%s", ir)
			}
			asked := false
			for _, ln := range strings.Split(ir, "\n") {
				if !strings.Contains(ln, "call i32 "+tt.call) {
					continue
				}
				args := ln[strings.Index(ln, tt.call)+len(tt.call):]
				args = args[:strings.Index(args, ")")]
				fields := strings.Split(args, ", ")
				if len(fields) != 3 {
					continue
				}
				for _, p := range payloads {
					if strings.Contains(fields[1], p) && strings.HasPrefix(fields[2], "i32 %") {
						asked = true
					}
				}
			}
			if !asked {
				t.Fatalf("no %s call takes a payload the object answered beside a REGISTER tag (payloads %q):\n%s",
					tt.call, payloads, ir)
			}
			if v, verr, vcode := cliStreams(t, "--verify-llvm", tt.src); vcode != 0 || !strings.Contains(v, "module verified") {
				t.Fatalf("the module did not verify: exit %d\n%s\n%s", vcode, v, verr)
			}
		})
	}
}

// The whole family as one program, line for line against CPython.
func TestTheObjectMembershipNeedleProgramMatchesTheReferenceThroughTheCLI(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("programs/object_membership_needle.gy")
	if err != nil {
		t.Fatalf("read program: %v", err)
	}
	if strings.TrimSpace(string(src)) == "" {
		t.Fatal("the program is empty")
	}
	want, err := os.ReadFile("testdata/object_membership_needle.expected")
	if err != nil {
		t.Fatalf("read the reference's answer: %v", err)
	}
	stdout, stderr, code := cliStreams(t, "--file", "programs/object_membership_needle.gy")
	if code == 2 {
		t.Fatalf("the toolchain failed on its own program:\n%s", stderr)
	}
	if code != 0 {
		t.Fatalf("exit %d, want 0\nstderr %q", code, stderr)
	}
	got := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	wantLines := strings.Split(strings.TrimRight(string(want), "\n"), "\n")
	if len(got) != len(wantLines) {
		t.Fatalf("the program printed %d lines, the reference printed %d:\n%s", len(got), len(wantLines), strings.Join(got, "\n"))
	}
	for i := range wantLines {
		if got[i] != wantLines[i] {
			t.Fatalf("line %d: the program answered %q where CPython answers %q\n%s", i+1, got[i], wantLines[i], strings.Join(got, "\n"))
		}
	}
}

func TestTheObjectMembershipNeedleProgramIsAnAssertedMatrixRow(t *testing.T) {
	t.Parallel()
	cases := conformanceCases()
	var row *lang.ConformanceCase
	for i := range cases {
		if cases[i].Name == "object_membership_needle.gy" {
			row = &cases[i]
			break
		}
	}
	if row == nil {
		t.Fatal("no conformance matrix row runs object_membership_needle.gy")
	}
	if !row.Asserted {
		t.Fatalf("row %s is not asserted: its stdout would run unwatched", row.ID)
	}
	if row.Oracle != "match" {
		t.Fatalf("row %s declares oracle %q; the compiled leg answers this program, so it is a match", row.ID, row.Oracle)
	}
	for _, want := range []string{
		"print(d[\"a\"] in d)",
		"print(nums[\"b\"] in {2.5})",
		"print(pair[0] in pair)",
		"except TypeError as e:",
	} {
		if !strings.Contains(row.Source, want) {
			t.Fatalf("row %s runs a program without %q", row.ID, want)
		}
	}
}
