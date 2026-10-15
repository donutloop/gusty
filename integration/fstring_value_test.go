package integration

// integration/fstring_value_test.go — the reference leg for an interpolation standing where a VALUE goes
// (roadmap L11.1, Gap R.146's value half; ADR 0359).
//
// `pkg/lang/fstring_value_test.go` holds these programs against the record. The record cannot witness the
// shapes that had never been answered — 58 of this cycle's sources had no entry until `tools/recmerge` asked
// CPython for one — and a record whose bytes came from the compiler would be the compiler testifying about
// itself, which ADR 0302 forbids. So every expectation here is what `python3` printed for the same file, read
// at run time: the reference is the definition, and a change to CPython's formatting is a change to this
// file's expectations whether anybody likes it or not.
//
// Why this altitude and not only the package's: the failure mode of this road is a plausible number where the
// reference has a text — a `@str_tab` index, a `@float_box` handle, a heap slot — and a number prints happily.
// The only witness that can tell `3` from `'3'` is something outside the compiler.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

// fstringValueCLIProg is one program the reference leg runs: the same file through `python3` and through every
// compiled engine in `cliEngines`. Which door answers it is pinned separately, in TestTheFStringValueModulesVerify.
type fstringValueCLIProg struct {
	name string
	src  string
}

var fstringValueCLIPrograms = []fstringValueCLIProg{{
	name: "a binding",
	src:  "n = 3\ns = f\"x{n}\"\nprint(s)\n",
}, {
	name: "concatenated with a text",
	src:  "n = 3\nprint(f\"a{n}\" + \"!\")\n",
}, {
	name: "concatenated with another interpolation",
	src:  "n = 3\nprint(f\"a{n}\" + f\"b{n}\")\n",
}, {
	name: "a method on the text",
	src:  "n = 3\nprint(f\"v={n}\".upper())\n",
}, {
	name: "the length of the text",
	src:  "n = 3\nprint(len(f\"a{n}b\"))\n",
}, {
	name: "compared with a text",
	src:  "n = 3\nprint(f\"a{n}\" == \"a3\")\n",
}, {
	name: "ordered against a text",
	src:  "n = 3\nprint(f\"a{n}\" < \"b\")\n",
}, {
	name: "a substring test over it",
	src:  "n = 3\nprint(\"3\" in f\"a{n}\")\n",
}, {
	name: "indexed",
	src:  "n = 3\nprint(f\"a{n}b\"[1])\n",
}, {
	name: "sliced",
	src:  "n = 3\nprint(f\"a{n}b\"[1:])\n",
}, {
	name: "returned by a function",
	src:  "def f():\n    return f\"x{3}\"\nprint(f())\n",
}, {
	name: "returned by a method",
	src:  "class C:\n    def m(self):\n        return f\"m{2}\"\nprint(C().m())\n",
}, {
	name: "handed to a text parameter",
	src:  "n = 3\ndef g(t):\n    print(t)\ng(f\"x{n}\")\n",
}, {
	name: "a list element",
	src:  "n = 3\nprint([f\"a{n}\", 1])\n",
}, {
	name: "a dict entry",
	src:  "n = 3\nprint({\"k\": f\"a{n}\"})\n",
}, {
	name: "a dict key",
	src:  "n = 3\nd = {f\"k{n}\": 1}\nprint(d)\n",
}, {
	name: "a dict key written by assignment",
	src:  "n = 3\nd = {}\nd[f\"v{n}\"] = 1\nprint(d)\n",
}, {
	name: "a set member",
	src:  "n = 3\nprint({f\"a{n}\"})\n",
}, {
	name: "a slot read back out of a list",
	src:  "n = 3\nxs = [f\"a{n}\"]\nprint(xs[0], xs[0].upper())\n",
}, {
	name: "appended to a runtime list",
	src:  "n = 3\nxs = []\nxs.append(f\"a{n}\")\nprint(xs)\n",
}, {
	name: "looped over as text",
	src:  "n = 3\nfor c in f\"ab{n}\":\n    print(c)\n",
}, {
	name: "str of it",
	src:  "n = 3\nprint(str(f\"a{n}\"))\n",
}, {
	name: "repr of it",
	src:  "n = 3\nprint(repr(f\"a{n}\"))\n",
}, {
	name: "a void field",
	src:  "print(f\"v={None}\")\n",
}, {
	name: "a container field",
	src:  "xs = [1, 2]\nprint(f\"{xs}\")\n",
}, {
	name: "a container answer in a field",
	src:  "def build():\n    return [1, 2]\nprint(f\"{build()}\")\n",
}, {
	name: "a verdict field",
	src:  "b = True\nprint(f\"{b}\")\n",
}, {
	name: "a float field",
	src:  "n = 2.5\nprint(f\"v={n}\")\n",
}, {
	name: "a call in the field",
	src:  "def f():\n    print(\"side\")\n    return 7\nprint(f\"v={f()}\")\n",
}, {
	name: "a nested interpolation",
	src:  "n = 3\nprint(f\"{f'q{n}'}\")\n",
}, {
	name: "a counter field",
	src:  "for i in range(2):\n    print(f\"x{i}\")\n",
}, {
	name: "a container element field",
	src:  "xs = [1, 2]\nfor v in xs:\n    print(f\"v{v}\")\n",
}, {
	name: "a percent in the literal",
	src:  "n = 3\nprint(f\"100%{n}\")\n",
}, {
	name: "braces written twice",
	src:  "print(f\"a{{b}}c\")\n",
}, {
	name: "a non-ASCII literal",
	src:  "n = 3\nprint(f\"\\u2713{n}\")\n",
}, {
	name: "a whole-constant interpolation",
	src:  "print(f\"hello\")\n",
}, {
	name: "the folded text bound and measured",
	src:  "s = f\"a{1}b\"\nprint(s, len(s))\n",
}}

// TestAnFStringUsedAsAValueAgreesWithCPython is the reference leg: the file is run by CPython and by each
// compiled engine in `cliEngines`, and the bytes must be identical. An honest refusal is not an answer — it is
// reported as debt against the row that owns the missing half rather than passing quietly.
func TestAnFStringUsedAsAValueAgreesWithCPython(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range fstringValueCLIPrograms {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, dir, "fstring_value.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			for _, engine := range cliEngines {
				got, code := cliRunMerged(t, engine, path)
				if code == 2 {
					t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", got)
				}
				if code == 1 && refusesHonestly(got) {
					requireReferenceTrapOrHonestRefusal(t, tc.src, ref, got, code,
						"roadmap L11.1 (the tagged value word) and Gap R.146 (the one-word position)",
						"an interpolation standing where a text value goes")
					continue
				}
				if code != 0 {
					t.Fatalf("%s exited %d on a program the reference prints:\n reference: %q\n compiled: %q",
						engine, code, ref, got)
				}
				requireReferenceAgreement(t, tc.src, ReferenceAgreement{
					Python: ref, Compiled: got, Code: code,
				}, "roadmap L11.1 (the tagged value word, at the interpolation)",
					"the text an interpolation builds is an ordinary @str_tab value")
			}
		})
	}
}

// fstringProgramBody returns the module's PROGRAM code — every `define` that is not one of the runtime's own
// `@rt_*`/`@llvm.*` helpers — so an IR row asks what the compiled program calls rather than what the runtime
// blob happens to define.
func fstringProgramBody(ir string) string {
	var out strings.Builder
	for rest := ir; rest != ""; {
		i := strings.Index(rest, "\ndefine ")
		if i < 0 {
			break
		}
		rest = rest[i+1:]
		end := strings.Index(rest, "\n}")
		if end < 0 {
			break
		}
		head := rest[:strings.Index(rest, "\n")]
		body := rest[:end]
		rest = rest[end:]
		if strings.Contains(head, " @rt_") || strings.Contains(head, " @llvm.") {
			continue
		}
		out.WriteString(body)
		out.WriteString("\n")
	}
	return out.String()
}

// TestAnFStringAsAValueRefusesAtTheCLIWithARow keeps the shapes this feature does NOT make at the altitude a
// caller or an agent sees: exit 1, a sentence that names the missing half and the row that owns it, and never
// the code a compiler bug owns (ADR 0166). Each `want` is a phrase that must survive into stderr.
func TestAnFStringAsAValueRefusesAtTheCLIWithARow(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{"a format spec over a name", "n = 3\nt = f\"x{n:>3}\"\nprint(t)\n", "format spec"},
		{"a format spec over a float", "n = 3.5\nt = f\"{n:.2f}\"\nprint(t)\n", "format spec"},
		{"a replace over an interpolation", "n = 3\nprint(f\"a{n}b\".replace(\"b\", \"c\"))\n", "string-method replace"},
		{"a split of an interpolation", "n = 3\nprint(f\"a{n}b\".split(\"b\"))\n", "string-method split"},
		{"a text repeated", "n = 3\nprint(f\"x{n}\" * 2)\n", "not supported"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := writeSrc(t, dir, "fstring_value_refuse.gy", tc.src)
			out, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", out)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a program this backend declines to build):\n%s", code, out)
			}
			if !refusesHonestly(out) {
				t.Errorf("the refusal is not actionable:\n%s", out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal does not name %q:\n%s", tc.want, out)
			}
			for _, leak := range []string{"%!s(", "*lang.", "%!d(", "0x"} {
				if strings.Contains(out, leak) {
					t.Errorf("the refusal leaks the compiler's own words (%q):\n%s", leak, out)
				}
			}
			noteCompiledGap(t, tc.src, out)
		})
	}
}

// TestTheFStringValueModulesVerify compiles each program and puts the module in front of the verifier, plus the
// IR rows that pin WHICH door answers: the parts are joined by `rt_str_cat`, a field is rendered by the module's
// one tag-reading printer, and a constant interpolation never enters the string table at run time at all.
func TestTheFStringValueModulesVerify(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, tc := range []struct {
		name   string
		src    string
		want   []string // calls the program's own code makes
		global []string // module constants the compiler folded
		deny   []string // calls the program's own code must never make
	}{
		{"the parts are built by the concatenator", "n = 3\ns = f\"x{n}\"\nprint(s)\n",
			[]string{"@rt_str_cat(", "@rt_str_of_int("}, nil, []string{"@rt_str_of_container("}},
		{"a container field asks the container renderer", "xs = [1, 2]\nprint(f\"{xs}\")\n",
			[]string{"@rt_str_of_container("}, nil, []string{"@rt_print_list("}},
		{"a field of a slot asks the one printer", "xs = []\nxs.append(2.5)\nn = xs[0]\nprint(f\"[{n}]\")\n",
			[]string{"@rt_str_of_value("}, nil, nil},
		{"a constant interpolation never interns", "print(f\"a{1}b\")\n",
			nil, []string{`c"a1b\00"`}, []string{"@rt_str_cat(", "@rt_str_of_value("}},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res, err := lang.Compile(tc.src)
			if err != nil {
				t.Fatalf("the compiled leg refused a program the reference answers: %v", err)
			}
			// The module always CARRIES the runtime blob — every `@rt_*` helper is defined in it, and some
			// of them call each other — so a CALL is asked of the program's own bodies. A folded constant is
			// a global, and is asked of the whole module.
			body := fstringProgramBody(res.IR)
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("%s: the program does not ask %s:\n%s", tc.name, want, body)
				}
			}
			for _, want := range tc.global {
				if !strings.Contains(res.IR, want) {
					t.Fatalf("%s: the module does not carry %s:\n%s", tc.name, want, res.IR)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(body, deny) {
					t.Fatalf("%s: the program asks %s, which is the second door this row forbids:\n%s", tc.name, deny, body)
				}
			}
			irPath := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "_")+".ll")
			if err := os.WriteFile(irPath, []byte(res.IR), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			llc := "llc-20"
			if _, err := exec.LookPath(llc); err != nil {
				llc = "llc"
			}
			if out, err := exec.Command(llc, "-filetype=null", irPath).CombinedOutput(); err != nil {
				t.Fatalf("%s: %s rejected the module: %v\n%s\nIR:\n%s", tc.name, llc, err, out, res.IR)
			}
		})
	}
}
