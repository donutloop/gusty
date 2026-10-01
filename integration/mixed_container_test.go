package integration

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// cpythonOut runs the pinned oracle over a source file and returns its stdout. The oracle is the
// same one docs/operations.md names; a machine without it skips the case rather than passing it.
func cpythonOut(t *testing.T, path string) (string, bool) {
	t.Helper()
	py := os.Getenv("GUSTY_PYTHON")
	if py == "" {
		py = "python3"
	}
	if _, err := exec.LookPath(py); err != nil {
		t.Skipf("no %s to act as the oracle (set GUSTY_PYTHON)", py)
	}
	cmd := exec.Command(py, path)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v\n%s", py, path, err, out)
	}
	return string(out), true
}

// integration/mixed_container_test.go — roadmap L11.1 (1b): dicts and sets whose slots describe
// themselves, compiled. The list case landed in ADR 0184/0185/0187; these are the other two
// containers, plus the rule that makes them safe (ADR 0232): a container word means its payload
// *and* its tag, so every write in the emitter that reaches a slot writes both, and every lookup
// in the runtime that answers "is this here?" compares both.
//
// Expectations are CPython's, taken before judging either backend. Where our two backends agree
// with each other and not with CPython — a bool renders as the number both store — the case says
// so in a comment rather than pretending; the pinned difference lives in
// TestBoolValueMatchesCPythonPinnedDifference, not here. Membership verdicts are reported through
// `1 if ... else 0` for exactly that reason: an int answer is CPython's answer too, and a bare
// bool print is not.
//
// Before this cycle the compiled leg refused the whole family ("a compiled dict holds either
// strings or numbers, not both"), and the answers the untagged lookups did give — `{1: "one"}`
// having an entry called "a" — were worse than the refusal.

func TestMixedContainersAnswerLikeTheInterpreter(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"mixed_dict_print",
			"d = {\"a\": 1, \"b\": \"x\", \"c\": None}\nprint(d)\n",
			"{'a': 1, 'b': 'x', 'c': None}\n"},
		{"mixed_dict_reads_each_kind",
			"d = {\"a\": 1, \"b\": \"x\", \"c\": None}\nprint(d[\"a\"])\nprint(d[\"b\"])\nprint(d[\"c\"])\n",
			"1\nx\nNone\n"},
		{"mixed_dict_value_binding",
			"d = {\"a\": 1, \"b\": \"x\"}\nv = d[\"b\"]\nprint(v)\n",
			"x\n"},
		{"mixed_dict_heterogeneous_keys",
			"d = {1: \"x\", \"k\": 2, None: 3}\nprint(len(d))\nprint(d[1])\nprint(d[\"k\"])\nprint(d[None])\n",
			"3\nx\n2\n3\n"},
		{"mixed_dict_membership",
			"d = {\"a\": 1, \"b\": \"x\"}\nprint(1 if \"a\" in d else 0)\nprint(1 if \"z\" in d else 0)\n",
			"1\n0\n"},
		{"mixed_dict_iteration",
			"d = {\"a\": 1, \"b\": \"x\", \"c\": None}\nfor k in d:\n    print(k)\n",
			"a\nb\nc\n"},
		{"mixed_dict_stores_a_mixed_value",
			"d = {\"a\": 1}\nd[\"b\"] = \"x\"\nprint(d[\"b\"])\nprint(1 if \"b\" in d else 0)\n",
			"x\n1\n"},
		{"mixed_set_print",
			"s = {1, \"a\", None}\nprint(s)\n",
			"{1, 'a', None}\n"},
		{"mixed_set_membership",
			"s = {1, \"a\", None}\nprint(1 if 1 in s else 0)\nprint(1 if \"a\" in s else 0)\nprint(1 if 7 in s else 0)\n",
			"1\n1\n0\n"},
		{"mixed_set_iteration_compares_by_kind",
			"s = {1, \"a\"}\nfor x in s:\n    if x == \"a\":\n        print(\"hit\")\n    if x == 1:\n        print(\"one\")\n",
			"one\nhit\n"},
		{"mixed_set_grows_and_dedupes",
			"s = {1, \"a\"}\ns.add(\"b\")\ns.add(1)\nprint(len(s))\n",
			"3\n"},
		{"mixed_set_discard_shifts_the_tags_too",
			"s = {1, \"a\", None}\ns.discard(1)\nprint(len(s))\nprint(1 if 1 in s else 0)\nprint(1 if None in s else 0)\n",
			"2\n0\n1\n"},
		{"a_string_needle_is_not_an_integer_key",
			"d = {1: \"one\"}\ntry:\n    print(d[\"a\"])\nexcept KeyError:\n    print(\"KeyError\")\n",
			"KeyError\n"},
		{"an_integer_needle_is_not_a_string_key",
			"d = {\"a\": 1}\ntry:\n    print(d[1])\nexcept KeyError:\n    print(\"KeyError\")\n",
			"KeyError\n"},
		{"a_number_is_not_a_string_member",
			"s = {\"a\"}\nprint(1 if 1 in s else 0)\n",
			"0\n"},
		// Growing a container with a value of the kind it has not held used to be a refusal, and
		// item assignment used to *relabel* the whole container: `xs = [1, 2]; xs[0] = "s"`
		// printed ['s', 'b'], because the untouched 2 was printed as whatever string its number
		// happens to index. Both now promote the container to describing its slots (ADR 0232).
		{"a_list_grows_a_second_kind",
			"xs = [1]\nxs.append(\"a\")\nprint(xs)\n",
			"[1, 'a']\n"},
		{"a_set_grows_a_second_kind",
			"s = {1}\ns.add(\"a\")\nprint(s)\n",
			"{1, 'a'}\n"},
		{"a_dict_grows_a_second_kind",
			"d = {\"a\": 1}\nd[\"b\"] = \"x\"\nprint(d)\nprint(d[\"a\"])\nprint(d[\"b\"])\n",
			"{'a': 1, 'b': 'x'}\n1\nx\n"},
		{"one_slot_written_is_not_every_slot_relabelled",
			"xs = [1, 2]\nxs[0] = \"s\"\nprint(xs)\n",
			"['s', 2]\n"},
		// The value being stored is itself a read from another container: the store happened and
		// the print denied it, because the site asked one question — what word did I intern —
		// when it should also have asked what the source says the value is.
		{"a_string_read_from_a_dict_enters_a_number_list",
			"d = {\"a\": \"x\"}\nxs = [1, 2]\nxs[0] = d[\"a\"]\nprint(xs)\n",
			"['x', 2]\n"},
		{"a_string_read_from_a_dict_is_appended",
			"d = {\"a\": \"x\"}\nxs = [1]\nxs.append(d[\"a\"])\nprint(xs)\n",
			"[1, 'x']\n"},
		{"a_string_read_from_a_dict_enters_a_number_dict",
			"d = {\"a\": \"x\"}\ne = {\"k\": 1}\ne[\"m\"] = d[\"a\"]\nprint(e)\n",
			"{'k': 1, 'm': 'x'}\n"},
		{"mixed_containers_cross_a_call",
			"def count(d):\n    c = 0\n    for k in d:\n        c = c + 1\n    return c\n\nprint(count({\"a\": 1, \"b\": \"x\", \"c\": None}))\n",
			"3\n"},
		{"set_of_a_mixed_dict_keys",
			"d = {\"a\": 1, \"b\": \"x\"}\nc = 0\nfor k in d:\n    if k == \"b\":\n        c = c + 1\nprint(c)\n",
			"1\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "mixed.gy", tc.src)
			for _, engine := range []string{"--interp", "--aot"} {
				out, code := cliRunCode(t, engine, path)
				if code != 0 {
					t.Fatalf("%s exited %d:\n%s", engine, code, cliRun(t, engine, path))
				}
				if out != tc.want {
					t.Fatalf("%s printed %q, want CPython's %q", engine, out, tc.want)
				}
			}
		})
	}
}

// The oracle half: the same programs, run by CPython, must agree with both of our backends. This
// is the check the compiled leg used to fail by *refusing*, and the check that would have caught
// the untagged-lookup answer (`{1: "one"}["a"]` printing `one`) without anyone reading IR.
func TestMixedContainersMatchCPython(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"mixed_dict_print", "d = {\"a\": 1, \"b\": \"x\", \"c\": None}\nprint(d)\n"},
		{"mixed_dict_reads_each_kind", "d = {\"a\": 1, \"b\": \"x\", \"c\": None}\nprint(d[\"a\"])\nprint(d[\"b\"])\nprint(d[\"c\"])\n"},
		{"mixed_dict_heterogeneous_keys", "d = {1: \"x\", \"k\": 2, None: 3}\nprint(len(d))\nprint(d[1])\nprint(d[\"k\"])\nprint(d[None])\n"},
		{"mixed_dict_membership", "d = {\"a\": 1, \"b\": \"x\"}\nprint(1 if \"a\" in d else 0)\nprint(1 if \"z\" in d else 0)\n"},
		{"mixed_dict_iteration", "d = {\"a\": 1, \"b\": \"x\", \"c\": None}\nfor k in d:\n    print(k)\n"},
		// No set-printing row here on purpose. CPython prints a set in hash order — {1, "a",
		// None} comes out {'a', 1, None} — and gusty prints it in insertion order, which is
		// documented (docs/language.md, "Sets iterate in insertion order in both backends")
		// and is what makes the two backends' output comparable at all. The engines are
		// compared against each other for rendering, in
		// TestMixedContainersAnswerLikeTheInterpreter; the oracle decides the *answers*.
		{"mixed_set_membership", "s = {1, \"a\", None}\nprint(1 if 1 in s else 0)\nprint(1 if \"a\" in s else 0)\nprint(1 if 7 in s else 0)\n"},
		{"mixed_set_discard", "s = {1, \"a\", None}\ns.discard(1)\nprint(len(s))\nprint(1 if 1 in s else 0)\n"},
		{"a_string_needle_is_not_an_integer_key", "d = {1: \"one\"}\ntry:\n    print(d[\"a\"])\nexcept KeyError:\n    print(\"KeyError\")\n"},
		{"an_integer_needle_is_not_a_string_key", "d = {\"a\": 1}\ntry:\n    print(d[1])\nexcept KeyError:\n    print(\"KeyError\")\n"},
		{"a_number_is_not_a_string_member", "s = {\"a\"}\nprint(1 if 1 in s else 0)\n"},
		{"a_list_grows_a_second_kind", "xs = [1]\nxs.append(\"a\")\nprint(xs)\n"},
		{"a_dict_grows_a_second_kind", "d = {\"a\": 1}\nd[\"b\"] = \"x\"\nprint(d)\nprint(d[\"a\"])\nprint(d[\"b\"])\n"},
		{"one_slot_written_is_not_every_slot_relabelled", "xs = [1, 2]\nxs[0] = \"s\"\nprint(xs)\n"},
		{"a_string_read_from_a_dict_enters_a_number_list", "d = {\"a\": \"x\"}\nxs = [1, 2]\nxs[0] = d[\"a\"]\nprint(xs)\n"},
		{"a_string_read_from_a_dict_is_appended", "d = {\"a\": \"x\"}\nxs = [1]\nxs.append(d[\"a\"])\nprint(xs)\n"},
		{"a_string_read_from_a_dict_enters_a_number_dict", "d = {\"a\": \"x\"}\ne = {\"k\": 1}\ne[\"m\"] = d[\"a\"]\nprint(e)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "mixed.gy", tc.src)
			want, ok := cpythonOut(t, path)
			if !ok {
				return
			}
			for _, engine := range []string{"--interp", "--aot"} {
				got, code := cliRunCode(t, engine, path)
				if code != 0 {
					t.Fatalf("%s exited %d:\n%s", engine, code, cliRun(t, engine, path))
				}
				if got != want {
					t.Errorf("%s printed %q, CPython printed %q", engine, got, want)
				}
			}
		})
	}
}

// What the compiled leg still refuses, refused honestly: exit 1 with the reason the emitter
// actually hit, never exit 2 (the compiler rejecting its own module) and never exit 0 with an
// answer the tag could not justify (ADR 0166's rule, ADR 0232's application of it).
func TestMixedContainersRefuseWhatNoTagDescribes(t *testing.T) {
	// The nested shapes this table used to carry are answers now ({1, [1]} and {"a": 1, "b": [1]}
	// both print correctly, pinned by TestNestedContainersAnswerOnBothBackends). What is left is a
	// value with no entry in the tag table at all, and the one nested shape with no rule.
	for _, tc := range []struct{ name, src, want string }{
		{"lambda_member", "xs = [1, \"a\"]\nxs.append(lambda x: x)\nprint(xs)\n", "must carry a tag"},
		{"tuple_member", "xs = [1, \"a\"]\nxs.append((1, 2))\nprint(xs)\n", "must carry a tag"},
		{"container_dict_key", "print({[1, 2]: 3})\n", "cannot hold"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, t.TempDir(), "mixed.gy", tc.src)
			_, code := cliRunCode(t, "--aot", path)
			if code == 2 {
				t.Fatalf("the compiled leg rejected its own module (ADR 0166):\n%s", cliRun(t, "--aot", path))
			}
			if code == 0 {
				out, _ := cliRunCode(t, "--aot", path)
				t.Fatalf("the compiled leg answered %q for a slot whose kind it cannot represent", out)
			}
			msg := cliRun(t, "--aot", path)
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("the refusal does not name what it could not store (%q):\n%s", tc.want, msg)
			}
			if !strings.Contains(msg, "roadmap") {
				t.Fatalf("the refusal does not point at the plan:\n%s", msg)
			}
		})
	}
}

// The emitted module must verify, not merely exist: every new runtime call in this cycle is a
// new function in @heap's block, and an LLVM-accepting-but-wrong module is the failure mode this
// project keeps hitting when a plan reads better than a trace.
func TestMixedContainerModulesVerify(t *testing.T) {
	for _, src := range []string{
		"d = {\"a\": 1, \"b\": \"x\", \"c\": None}\nprint(d)\nprint(d[\"b\"])\n",
		"s = {1, \"a\", None}\ns.add(\"z\")\ns.discard(1)\nfor x in s:\n    print(x)\n",
		"d = {1: \"x\", \"k\": 2}\nprint(1 in d)\nprint(\"k\" in d)\nprint(d[1])\n",
	} {
		res, err := lang.Compile(src)
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if !strings.Contains(res.IR, "@heap_tags") {
			t.Errorf("the module carries no tag table despite a mixed container")
		}
		ver, err := lang.VerifyModuleIR(res.IR, 0)
		if err != nil {
			t.Errorf("the verifier could not run: %v", err)
			continue
		}
		if ver.Skipped {
			t.Skipf("no LLVM verifier available (pinned LLVM %s)", lang.PinnedLLVMVersion)
		}
		if !ver.OK {
			t.Errorf("%s rejected the module: %v", ver.Tool, ver.Errors)
		}
	}
}
