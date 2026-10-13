package lang

import (
	"regexp"
	"strings"
	"testing"
)

// An instance written into a container slot used to carry TagInt, so every question the tag answers
// answered about a number: `print(xs[0])` printed the heap handle, `len(xs[0])` said `'int' has no
// len()`, and ADR 0323's membership and loop doors named `'int'` where CPython names the class.
// These tests are Gap R.209: the writer now stores TagInstance, and the printers and raises obey it.

var instanceAddrRE = regexp.MustCompile(`0x[0-9a-fA-F]+`)

func normalizeInstanceAddrs(s string) string {
	return instanceAddrRE.ReplaceAllString(s, "0xADDR")
}

func instancePrintsLikeCPython(t *testing.T, src, wantPrefix string) {
	t.Helper()
	compiledOut, err := RunSource(src)
	if err != nil {
		t.Fatalf("compiled run of %q: %v", src, err)
	}
	refOut, refErr, refExecErr := PythonRun(src)
	if refExecErr != nil {
		t.Fatalf("CPython run of %q failed: %v (%s)", src, refExecErr, refErr)
	}
	got := normalizeInstanceAddrs(compiledOut)
	want := normalizeInstanceAddrs(refOut)
	if got != want {
		t.Fatalf("compiled stdout %q, CPython stdout %q", compiledOut, refOut)
	}
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("compiled stdout %q, want prefix %q", got, wantPrefix)
	}
}

func instanceTrapsLikeCPython(t *testing.T, src, wantMsg string) {
	t.Helper()
	refOut, refErr, refExecErr := PythonRun(src)
	if refExecErr == nil {
		t.Fatalf("CPython answered %q where %q should raise %q", refOut, src, wantMsg)
	}
	if !strings.Contains(refErr, wantMsg) {
		t.Fatalf("CPython stderr %q does not contain %q (stdout %q)", refErr, wantMsg, refOut)
	}
	res, err := JIT(src, 0)
	if err != nil {
		t.Fatalf("compiled run of %q: %v", src, err)
	}
	if res.Code != 3 {
		t.Fatalf("compiled exit %d on a program that should raise %q (stdout %q, stderr %q)", res.Code, wantMsg, res.Output, res.Stderr)
	}
	if !strings.Contains(res.Stderr, wantMsg) {
		t.Fatalf("compiled stderr %q does not contain %q (stdout %q)", res.Stderr, wantMsg, res.Output)
	}
	if strings.Contains(res.Stderr, "codegen:") {
		t.Fatalf("compiled backend refused a program the reference runs: %s", res.Stderr)
	}
}

func TestAnInstanceSlotPrintsItsClassRatherThanItsHandle(t *testing.T) {
	tests := []struct {
		name       string
		src        string
		wantPrefix string
	}{
		{
			name:       "a literal instance argument",
			src:        "class C:\n    pass\nprint(C())\n",
			wantPrefix: "<__main__.C object at 0xADDR",
		},
		{
			name:       "an instance variable",
			src:        "class C:\n    pass\nc = C()\nprint(c)\n",
			wantPrefix: "<__main__.C object at 0xADDR",
		},
		{
			name:       "an appended instance slot",
			src:        "class C:\n    pass\nxs = []\nxs.append(C())\nprint(xs[0])\n",
			wantPrefix: "<__main__.C object at 0xADDR",
		},
		{
			name:       "a list containing an instance",
			src:        "class C:\n    pass\nc = C()\nprint([c])\n",
			wantPrefix: "[<__main__.C object at 0xADDR",
		},
		{
			name:       "a dict value holding an instance",
			src:        "class C:\n    pass\nc = C()\nprint({\"k\": c})\n",
			wantPrefix: "{'k': <__main__.C object at 0xADDR",
		},
		{
			name:       "a set containing an instance",
			src:        "class C:\n    pass\nc = C()\nprint({c})\n",
			wantPrefix: "{<__main__.C object at 0xADDR",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			instancePrintsLikeCPython(t, tc.src, tc.wantPrefix)
		})
	}
}

func TestAnInstanceSlotNamesItsClassWhereAPlainIntWould(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "membership of an instance slot",
			src:  "class C:\n    pass\nxs = []\nxs.append(C())\nprint(1 in xs[0])\n",
			want: "TypeError: argument of type 'C' is not iterable",
		},
		{
			name: "a loop over an instance slot",
			src:  "class C:\n    pass\nxs = []\nxs.append(C())\nfor v in xs[0]:\n    print(v)\n",
			want: "TypeError: 'C' object is not iterable",
		},
		{
			name: "len of an instance slot",
			src:  "class C:\n    pass\nxs = []\nxs.append(C())\nprint(len(xs[0]))\n",
			want: "TypeError: object of type 'C' has no len()",
		},
		{
			name: "a text haystack asked about an instance needle",
			src:  "class C:\n    pass\nc = C()\nprint(c in \"abc\")\n",
			want: "TypeError: 'in <string>' requires string as left operand, not C",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			instanceTrapsLikeCPython(t, tc.src, tc.want)
		})
	}
}

func TestAnInstanceSlotIsStillAnObjectForIdentityAndContainment(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "the same slot compares equal to itself",
			src:  "class C:\n    pass\nxs = []\nxs.append(C())\nprint(xs[0] == xs[0])\n",
			want: "True\n",
		},
		{
			name: "two instances of the same class are distinct",
			src:  "class C:\n    pass\nxs = []\nxs.append(C())\nxs.append(C())\nprint(xs[0] == xs[1])\n",
			want: "False\n",
		},
		{
			name: "an instance is a dict key",
			src:  "class C:\n    pass\nc = C()\nd = {}\nd[c] = 1\nprint(d[c])\n",
			want: "1\n",
		},
		{
			name: "an instance is a set member",
			src:  "class C:\n    pass\nc = C()\ns = {c}\nprint(len(s))\n",
			want: "1\n",
		},
		{
			name: "an appended instance is a set member",
			src:  "class C:\n    pass\nxs = []\nxs.append(C())\ny = xs[0]\nprint(len({y}))\n",
			want: "1\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := captureStdout(t, tc.src)
			if got != tc.want {
				t.Fatalf("compiled stdout %q, want %q", got, tc.want)
			}
			refOut, refErr, refExecErr := PythonRun(tc.src)
			if refExecErr != nil {
				t.Fatalf("CPython run of %q failed: %v (%s)", tc.src, refExecErr, refErr)
			}
			if refOut != tc.want {
				t.Fatalf("CPython stdout %q, want %q", refOut, tc.want)
			}
		})
	}
}

func TestTheClassRuntimeIsEmittedOnlyWhereTheObjectNeedsIt(t *testing.T) {
	dynamic := compileSrcIR(t, "class C:\n    pass\nxs = []\nxs.append(C())\nprint(7 in xs[0])\n")
	for _, want := range []string{
		"@rt.inst.names =",
		"@rt.inst.short_names =",
		"define internal i8* @rt_class_qual(",
		"define internal i8* @rt_class_short(",
		"define internal i8* @rt_instance_repr_ptr(",
		"define internal i8* @rt_instance_msg_ptr(",
		"call i8* @rt_instance_msg_ptr(",
	} {
		if !strings.Contains(dynamic, want) {
			t.Fatalf("instance-slot module missing %q:\n%s", want, dynamic)
		}
	}

	// A static numeric program does not grow the class table.
	plain := compileSrcIR(t, "print(1 in [1, 2])\n")
	for _, unwanted := range []string{
		"call i8* @rt_instance_msg_ptr(",
		"@rt.inst.names =",
	} {
		if strings.Contains(plain, unwanted) {
			t.Fatalf("static numeric module unexpectedly contains %q:\n%s", unwanted, plain)
		}
	}
}

func TestAnInstanceSlotAsksForItsTagRatherThanTheNumberDoor(t *testing.T) {
	ir := compileSrcIR(t, "class C:\n    pass\nxs = []\nxs.append(C())\nfor v in xs[0]:\n    print(v)\n")
	for _, want := range []string{
		"call i8* @rt_instance_msg_ptr(",
		"call void @rt_die(",
	} {
		if !strings.Contains(ir, want) {
			t.Fatalf("instance-slot loop IR missing %q:\n%s", want, ir)
		}
	}
	// The tag-keyed container doors remain in the module, but the instance arm is selected by tag 10
	// and reaches the class-named raise before any container walk.
}
