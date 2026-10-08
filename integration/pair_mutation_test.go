package integration

// integration/pair_mutation_test.go — the CLI half of a pair-bound name written into a container by a
// STATEMENT: `xs.append(n)`, `s.add(n)`, `xs[i] = v`, `d[k] = v` (roadmap L11.1, Gap R.146's mutation roads;
// ADR 0311, ADR 0310's dict entry and set member, ADR 0306's list element).
//
// The record leg (`pkg/lang/pair_mutation_test.go`) checks these programs against the answer the retired
// engine's record holds; this file asks the question a record cannot answer for a shape it never saw — does
// CPython agree. For a mutation the question is sharper than for a literal, because the wrong answer here is
// not one printed value but a container that goes on lying: an appended text stored as its interned index
// prints `[0]` today and then prints `[0]` through every `len`, `in`, `for` and `str` that follows. The four
// roads each already had a tagged door (`rt_append_tagged`, `rt_set_add_tagged`, `rt_dict_put_tagged`,
// `rt_put_elem`+`rt_tag_elem`) that a heterogeneous literal needed (ADR 0232); what they were missing was a
// caller that had two words to hand.
//
// The trap rows are the hashing question again, on the road that puts a value in a bucket: a payload says
// "hashable" for every value the language has, so a container-valued member or key has to be refused by the
// tag, in CPython's own sentence, at the trap exit (Gap R.81, ADR 0228, ADR 0166).

import (
	"strings"
	"testing"

	"github.com/donutloop/gusty/pkg/lang"
)

const pairMutationSlot = "xs = []\nxs.append(7)\nn = xs[0]\n"

// TestCLIAgentPairBoundNameMutatesAContainerAgreesWithCPython runs the reference and the compiled artifact
// over the same mutation: append, element assignment, set add, and dict setitem on both sides of the entry.
func TestCLIAgentPairBoundNameMutatesAContainerAgreesWithCPython(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src string }{
		{"the a verdict slot appended to a list", "xs = []\nxs.append(True)\nn = xs[0]\nys = []\nys.append(n)\nprint(ys)\n"},
		{"the a verdict slot appended to a list", "xs = []\nxs.append(True)\nn = xs[0]\nys = []\nys.append(n)\nys.append(1)\nprint(ys)\n"},
		{"the a verdict slot added to a set", "xs = []\nxs.append(True)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(7 in s2)\n"},
		{"the a verdict slot added to a set", "xs = []\nxs.append(True)\nn = xs[0]\ns2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n"},
		{"the a verdict slot added to a set", "xs = []\nxs.append(True)\nn = xs[0]\ns2 = {1}\ns2.add(n)\nprint(s2)\n"},
		{"the a verdict slot written into a dict entry by a literal key", "xs = []\nxs.append(True)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2)\n"},
		{"the a verdict slot written into a dict entry by a literal key", "xs = []\nxs.append(True)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n"},
		{"the a verdict slot written into a dict entry by a literal key", "xs = []\nxs.append(True)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n"},
		{"the a verdict slot written into a dict entry by a literal key", "xs = []\nxs.append(True)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n"},
		{"the a verdict slot written into a dict entry by a literal key", "xs = []\nxs.append(True)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n"},
		{"the a verdict slot written into a dict by the pair itself", "xs = []\nxs.append(True)\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2)\n"},
		{"the a verdict slot written into a dict by the pair itself", "xs = []\nxs.append(True)\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2[n])\n"},
		{"the a verdict slot appended to a list", "xs = []\nxs.append(True)\nn = xs[0]\nys = []\nys.append(n)\nprint(len(ys))\n"},
		{"the a verdict slot written into a dict by the pair itself", "xs = []\nxs.append(True)\nn = xs[0]\nd2 = {}\nd2[n] = n\nprint(d2)\n"},
		{"the a verdict slot written into a dict by the pair itself", "xs = []\nxs.append(True)\nn = xs[0]\nd2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n"},
		{"the a verdict slot written into a dict entry by a literal key", "xs = []\nxs.append(True)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n"},
		{"the a verdict slot written into a dict entry by a literal key", "xs = []\nxs.append(True)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = [n]\nprint(d2)\n"},
		{"the a verdict slot appended to a list", "xs = []\nxs.append(True)\nn = xs[0]\nys = []\nys.append(n)\nprint(7 in ys)\n"},
		{"the a verdict slot appended to a list", "xs = []\nxs.append(True)\nn = xs[0]\nys = []\nys.append(n)\nfor v in ys:\n    print(v)\n"},
		{"the a verdict slot appended to a list", "xs = []\nxs.append(True)\nn = xs[0]\nys = []\nys.append(n)\nprint(str(ys))\n"},
		{"the a verdict slot written into a list slot", "xs = []\nxs.append(True)\nn = xs[0]\nys = [0]\nys[0] = n\nprint(ys)\n"},
		{"the a verdict slot written into a list slot", "xs = []\nxs.append(True)\nn = xs[0]\nys = [1, 2]\nys[1] = n\nprint(ys)\n"},
		{"the a verdict slot added to a set", "xs = []\nxs.append(True)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(s2)\n"},
		{"the a verdict slot added to a set", "xs = []\nxs.append(True)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(len(s2))\n"},
		{"the a float slot appended to a list", "xs = []\nxs.append(2.5)\nn = xs[0]\nys = []\nys.append(n)\nprint(ys)\n"},
		{"the a float slot appended to a list", "xs = []\nxs.append(2.5)\nn = xs[0]\nys = []\nys.append(n)\nys.append(1)\nprint(ys)\n"},
		{"the a float slot added to a set", "xs = []\nxs.append(2.5)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(7 in s2)\n"},
		{"the a float slot added to a set", "xs = []\nxs.append(2.5)\nn = xs[0]\ns2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n"},
		{"the a float slot added to a set", "xs = []\nxs.append(2.5)\nn = xs[0]\ns2 = {1}\ns2.add(n)\nprint(s2)\n"},
		{"the a float slot written into a dict entry by a literal key", "xs = []\nxs.append(2.5)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2)\n"},
		{"the a float slot written into a dict entry by a literal key", "xs = []\nxs.append(2.5)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n"},
		{"the a float slot written into a dict entry by a literal key", "xs = []\nxs.append(2.5)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n"},
		{"the a float slot written into a dict entry by a literal key", "xs = []\nxs.append(2.5)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n"},
		{"the a float slot written into a dict entry by a literal key", "xs = []\nxs.append(2.5)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n"},
		{"the a float slot written into a dict by the pair itself", "xs = []\nxs.append(2.5)\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2)\n"},
		{"the a float slot written into a dict by the pair itself", "xs = []\nxs.append(2.5)\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2[n])\n"},
		{"the a float slot appended to a list", "xs = []\nxs.append(2.5)\nn = xs[0]\nys = []\nys.append(n)\nprint(len(ys))\n"},
		{"the a float slot written into a dict by the pair itself", "xs = []\nxs.append(2.5)\nn = xs[0]\nd2 = {}\nd2[n] = n\nprint(d2)\n"},
		{"the a float slot written into a dict by the pair itself", "xs = []\nxs.append(2.5)\nn = xs[0]\nd2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n"},
		{"the a float slot written into a dict entry by a literal key", "xs = []\nxs.append(2.5)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n"},
		{"the a float slot written into a dict entry by a literal key", "xs = []\nxs.append(2.5)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = [n]\nprint(d2)\n"},
		{"the a float slot appended to a list", "xs = []\nxs.append(2.5)\nn = xs[0]\nys = []\nys.append(n)\nprint(7 in ys)\n"},
		{"the a float slot appended to a list", "xs = []\nxs.append(2.5)\nn = xs[0]\nys = []\nys.append(n)\nfor v in ys:\n    print(v)\n"},
		{"the a float slot appended to a list", "xs = []\nxs.append(2.5)\nn = xs[0]\nys = []\nys.append(n)\nprint(str(ys))\n"},
		{"the a float slot written into a list slot", "xs = []\nxs.append(2.5)\nn = xs[0]\nys = [0]\nys[0] = n\nprint(ys)\n"},
		{"the a float slot written into a list slot", "xs = []\nxs.append(2.5)\nn = xs[0]\nys = [1, 2]\nys[1] = n\nprint(ys)\n"},
		{"the a float slot added to a set", "xs = []\nxs.append(2.5)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(s2)\n"},
		{"the a float slot added to a set", "xs = []\nxs.append(2.5)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(len(s2))\n"},
		{"the an int slot appended to a list", "xs = []\nxs.append(7)\nn = xs[0]\nys = []\nys.append(n)\nprint(ys)\n"},
		{"the an int slot appended to a list", "xs = []\nxs.append(7)\nn = xs[0]\nys = []\nys.append(n)\nys.append(1)\nprint(ys)\n"},
		{"the an int slot added to a set", "xs = []\nxs.append(7)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(7 in s2)\n"},
		{"the an int slot added to a set", "xs = []\nxs.append(7)\nn = xs[0]\ns2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n"},
		{"the an int slot added to a set", "xs = []\nxs.append(7)\nn = xs[0]\ns2 = {1}\ns2.add(n)\nprint(s2)\n"},
		{"the an int slot written into a dict entry by a literal key", "xs = []\nxs.append(7)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2)\n"},
		{"the an int slot written into a dict entry by a literal key", "xs = []\nxs.append(7)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n"},
		{"the an int slot written into a dict entry by a literal key", "xs = []\nxs.append(7)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n"},
		{"the an int slot written into a dict entry by a literal key", "xs = []\nxs.append(7)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n"},
		{"the an int slot written into a dict entry by a literal key", "xs = []\nxs.append(7)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n"},
		{"the an int slot written into a dict by the pair itself", "xs = []\nxs.append(7)\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2)\n"},
		{"the an int slot written into a dict by the pair itself", "xs = []\nxs.append(7)\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2[n])\n"},
		{"the an int slot appended to a list", "xs = []\nxs.append(7)\nn = xs[0]\nys = []\nys.append(n)\nprint(len(ys))\n"},
		{"the an int slot written into a dict by the pair itself", "xs = []\nxs.append(7)\nn = xs[0]\nd2 = {}\nd2[n] = n\nprint(d2)\n"},
		{"the an int slot written into a dict by the pair itself", "xs = []\nxs.append(7)\nn = xs[0]\nd2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n"},
		{"the an int slot written into a dict entry by a literal key", "xs = []\nxs.append(7)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n"},
		{"the an int slot written into a dict entry by a literal key", "xs = []\nxs.append(7)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = [n]\nprint(d2)\n"},
		{"the an int slot appended to a list", "xs = []\nxs.append(7)\nn = xs[0]\nys = []\nys.append(n)\nprint(7 in ys)\n"},
		{"the an int slot appended to a list", "xs = []\nxs.append(7)\nn = xs[0]\nys = []\nys.append(n)\nfor v in ys:\n    print(v)\n"},
		{"the an int slot appended to a list", "xs = []\nxs.append(7)\nn = xs[0]\nys = []\nys.append(n)\nprint(str(ys))\n"},
		{"the an int slot written into a list slot", "xs = []\nxs.append(7)\nn = xs[0]\nys = [0]\nys[0] = n\nprint(ys)\n"},
		{"the an int slot written into a list slot", "xs = []\nxs.append(7)\nn = xs[0]\nys = [1, 2]\nys[1] = n\nprint(ys)\n"},
		{"the an int slot added to a set", "xs = []\nxs.append(7)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(s2)\n"},
		{"the an int slot added to a set", "xs = []\nxs.append(7)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(len(s2))\n"},
		{"the a container slot appended to a list", "xs = []\nxs.append([1, 2])\nn = xs[0]\nys = []\nys.append(n)\nprint(ys)\n"},
		{"the a container slot appended to a list", "xs = []\nxs.append([1, 2])\nn = xs[0]\nys = []\nys.append(n)\nys.append(1)\nprint(ys)\n"},
		{"the a container slot written into a dict entry by a literal key", "xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2)\n"},
		{"the a container slot written into a dict entry by a literal key", "xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n"},
		{"the a container slot written into a dict entry by a literal key", "xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n"},
		{"the a container slot written into a dict entry by a literal key", "xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n"},
		{"the a container slot written into a dict entry by a literal key", "xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n"},
		{"the a container slot appended to a list", "xs = []\nxs.append([1, 2])\nn = xs[0]\nys = []\nys.append(n)\nprint(len(ys))\n"},
		{"the a container slot written into a dict entry by a literal key", "xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n"},
		{"the a container slot written into a dict entry by a literal key", "xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {}\nd2[\"k\"] = [n]\nprint(d2)\n"},
		{"the a container slot appended to a list", "xs = []\nxs.append([1, 2])\nn = xs[0]\nys = []\nys.append(n)\nprint(7 in ys)\n"},
		{"the a container slot appended to a list", "xs = []\nxs.append([1, 2])\nn = xs[0]\nys = []\nys.append(n)\nfor v in ys:\n    print(v)\n"},
		{"the a container slot appended to a list", "xs = []\nxs.append([1, 2])\nn = xs[0]\nys = []\nys.append(n)\nprint(str(ys))\n"},
		{"the a container slot written into a list slot", "xs = []\nxs.append([1, 2])\nn = xs[0]\nys = [0]\nys[0] = n\nprint(ys)\n"},
		{"the a container slot written into a list slot", "xs = []\nxs.append([1, 2])\nn = xs[0]\nys = [1, 2]\nys[1] = n\nprint(ys)\n"},
		{"the a None slot appended to a list", "xs = []\nxs.append(None)\nn = xs[0]\nys = []\nys.append(n)\nprint(ys)\n"},
		{"the a None slot appended to a list", "xs = []\nxs.append(None)\nn = xs[0]\nys = []\nys.append(n)\nys.append(1)\nprint(ys)\n"},
		{"the a None slot added to a set", "xs = []\nxs.append(None)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(7 in s2)\n"},
		{"the a None slot added to a set", "xs = []\nxs.append(None)\nn = xs[0]\ns2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n"},
		{"the a None slot written into a dict entry by a literal key", "xs = []\nxs.append(None)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2)\n"},
		{"the a None slot written into a dict entry by a literal key", "xs = []\nxs.append(None)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n"},
		{"the a None slot written into a dict entry by a literal key", "xs = []\nxs.append(None)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n"},
		{"the a None slot written into a dict entry by a literal key", "xs = []\nxs.append(None)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n"},
		{"the a None slot written into a dict entry by a literal key", "xs = []\nxs.append(None)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n"},
		{"the a None slot written into a dict by the pair itself", "xs = []\nxs.append(None)\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2)\n"},
		{"the a None slot written into a dict by the pair itself", "xs = []\nxs.append(None)\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2[n])\n"},
		{"the a None slot appended to a list", "xs = []\nxs.append(None)\nn = xs[0]\nys = []\nys.append(n)\nprint(len(ys))\n"},
		{"the a None slot written into a dict by the pair itself", "xs = []\nxs.append(None)\nn = xs[0]\nd2 = {}\nd2[n] = n\nprint(d2)\n"},
		{"the a None slot written into a dict by the pair itself", "xs = []\nxs.append(None)\nn = xs[0]\nd2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n"},
		{"the a None slot written into a dict entry by a literal key", "xs = []\nxs.append(None)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n"},
		{"the a None slot written into a dict entry by a literal key", "xs = []\nxs.append(None)\nn = xs[0]\nd2 = {}\nd2[\"k\"] = [n]\nprint(d2)\n"},
		{"the a None slot appended to a list", "xs = []\nxs.append(None)\nn = xs[0]\nys = []\nys.append(n)\nprint(7 in ys)\n"},
		{"the a None slot appended to a list", "xs = []\nxs.append(None)\nn = xs[0]\nys = []\nys.append(n)\nfor v in ys:\n    print(v)\n"},
		{"the a None slot appended to a list", "xs = []\nxs.append(None)\nn = xs[0]\nys = []\nys.append(n)\nprint(str(ys))\n"},
		{"the a None slot written into a list slot", "xs = []\nxs.append(None)\nn = xs[0]\nys = [0]\nys[0] = n\nprint(ys)\n"},
		{"the a None slot written into a list slot", "xs = []\nxs.append(None)\nn = xs[0]\nys = [1, 2]\nys[1] = n\nprint(ys)\n"},
		{"the a None slot added to a set", "xs = []\nxs.append(None)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(s2)\n"},
		{"the a None slot added to a set", "xs = []\nxs.append(None)\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(len(s2))\n"},
		{"the a text slot appended to a list", "xs = []\nxs.append(\"a\")\nn = xs[0]\nys = []\nys.append(n)\nprint(ys)\n"},
		{"the a text slot appended to a list", "xs = []\nxs.append(\"a\")\nn = xs[0]\nys = []\nys.append(n)\nys.append(1)\nprint(ys)\n"},
		{"the a text slot added to a set", "xs = []\nxs.append(\"a\")\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(7 in s2)\n"},
		{"the a text slot added to a set", "xs = []\nxs.append(\"a\")\nn = xs[0]\ns2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n"},
		{"the a text slot added to a set", "xs = []\nxs.append(\"a\")\nn = xs[0]\ns2 = {1}\ns2.add(n)\nprint(s2)\n"},
		{"the a text slot written into a dict entry by a literal key", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2)\n"},
		{"the a text slot written into a dict entry by a literal key", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(d2[\"k\"])\n"},
		{"the a text slot written into a dict entry by a literal key", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(len(d2))\n"},
		{"the a text slot written into a dict entry by a literal key", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(\"k\" in d2)\n"},
		{"the a text slot written into a dict entry by a literal key", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nd2[\"j\"] = 2\nprint(d2)\n"},
		{"the a text slot written into a dict by the pair itself", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2)\n"},
		{"the a text slot written into a dict by the pair itself", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2[n])\n"},
		{"the a text slot appended to a list", "xs = []\nxs.append(\"a\")\nn = xs[0]\nys = []\nys.append(n)\nprint(len(ys))\n"},
		{"the a text slot written into a dict by the pair itself", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {}\nd2[n] = n\nprint(d2)\n"},
		{"the a text slot written into a dict by the pair itself", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n"},
		{"the a text slot written into a dict entry by a literal key", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {}\nd2[\"k\"] = n\nprint(str(d2))\n"},
		{"the a text slot written into a dict entry by a literal key", "xs = []\nxs.append(\"a\")\nn = xs[0]\nd2 = {}\nd2[\"k\"] = [n]\nprint(d2)\n"},
		{"the a text slot appended to a list", "xs = []\nxs.append(\"a\")\nn = xs[0]\nys = []\nys.append(n)\nprint(7 in ys)\n"},
		{"the a text slot appended to a list", "xs = []\nxs.append(\"a\")\nn = xs[0]\nys = []\nys.append(n)\nfor v in ys:\n    print(v)\n"},
		{"the a text slot appended to a list", "xs = []\nxs.append(\"a\")\nn = xs[0]\nys = []\nys.append(n)\nprint(str(ys))\n"},
		{"the a text slot written into a list slot", "xs = []\nxs.append(\"a\")\nn = xs[0]\nys = [0]\nys[0] = n\nprint(ys)\n"},
		{"the a text slot written into a list slot", "xs = []\nxs.append(\"a\")\nn = xs[0]\nys = [1, 2]\nys[1] = n\nprint(ys)\n"},
		{"the a text slot added to a set", "xs = []\nxs.append(\"a\")\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(s2)\n"},
		{"the a text slot added to a set", "xs = []\nxs.append(\"a\")\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(len(s2))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairmut.gy", tc.src)
			ref, ok := cpythonPlainOut(t, dir, tc.src)
			if !ok {
				t.Skipf("the reference could not answer this program: %s", tc.src)
			}
			got, code := cliRunMerged(t, "--file", path)
			if code == 1 && refusesHonestly(got) {
				requireReferenceTrapOrHonestRefusal(t, tc.src, "", got, code,
					"roadmap L11.1 (the tagged value word) and Gap R.146 (the positions that keep one word)",
					"the value's kind is a run-time fact and this position kept one word for it")
				return
			}
			if code != 0 {
				t.Fatalf("the compiled run exited %d on a program the reference prints:\n reference: %q\n compiled: %q", code, ref, got)
			}
			requireReferenceAgreement(t, tc.src, ReferenceAgreement{
				Python: ref, Compiled: got, Code: code,
			}, "roadmap L11.1 (the tagged value word, at the container's mutation roads)",
				"a name the pair road bound, appended, assigned to a slot, added to a set or put into a dict")
		})
	}
}

// TestCLIAgentPairBoundMemberOrKeyThatCannotBeHashedRaisesWhatCPythonRaises is the hashing half on the
// mutation roads: the reference raises, and the compiled leg raises the same sentence at the trap exit —
// never at the exit code of success, and never as the compiler's own exit 2.
func TestCLIAgentPairBoundMemberOrKeyThatCannotBeHashedRaisesWhatCPythonRaises(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, sentence string }{
		{"the a container slot cannot be a key or a member", "xs = []\nxs.append([1, 2])\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(7 in s2)\n", "TypeError: unhashable type: 'list'"},
		{"the a container slot cannot be a key or a member", "xs = []\nxs.append([1, 2])\nn = xs[0]\ns2 = set()\ns2.add(n)\nfor v in s2:\n    print(v)\n", "TypeError: unhashable type: 'list'"},
		{"the a container slot cannot be a key or a member", "xs = []\nxs.append([1, 2])\nn = xs[0]\ns2 = {1}\ns2.add(n)\nprint(s2)\n", "TypeError: unhashable type: 'list'"},
		{"the a container slot cannot be a key or a member", "xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2)\n", "TypeError: unhashable type: 'list'"},
		{"the a container slot cannot be a key or a member", "xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {}\nd2[n] = 1\nprint(d2[n])\n", "TypeError: unhashable type: 'list'"},
		{"the a container slot cannot be a key or a member", "xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {}\nd2[n] = n\nprint(d2)\n", "TypeError: unhashable type: 'list'"},
		{"the a container slot cannot be a key or a member", "xs = []\nxs.append([1, 2])\nn = xs[0]\nd2 = {\"a\": 1}\nd2[n] = n\nprint(d2)\n", "TypeError: unhashable type: 'list'"},
		{"the a container slot cannot be a key or a member", "xs = []\nxs.append([1, 2])\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(s2)\n", "TypeError: unhashable type: 'list'"},
		{"the a container slot cannot be a key or a member", "xs = []\nxs.append([1, 2])\nn = xs[0]\ns2 = set()\ns2.add(n)\nprint(len(s2))\n", "TypeError: unhashable type: 'list'"}} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairmut_trap.gy", tc.src)
			if py, ok := cpythonPlainOut(t, dir, tc.src); ok {
				t.Fatalf("the reference answered %q, expected the trap\nsrc: %s", py, tc.src)
			}
			out, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module this raise was emitted into (ADR 0166):\n%s", out)
			}
			if code != 3 {
				t.Fatalf("exit %d, want the trap exit 3 (ADR 0166):\n%s", code, out)
			}
			if !strings.Contains(out, tc.sentence) {
				t.Errorf("the compiled raise did not say %q:\n%s", tc.sentence, out)
			}
		})
	}
}

// TestCLIAgentTheMutationRoadsStillRefuseWhatTheyCannotCarry keeps the honest half at the CLI. A value that
// is not a pair — the double a true division answered — is still asked about by the ordinary road first, and
// that question is what keeps `d["k"] = xs[0] / 2` an honest refusal instead of a payload in an i32 slot; a
// pair handed across a call keeps its own sentence. The two fold rows that used to open this table —
// `print(sum([n]))` and `print(min([n, 3]))` — answer since ADR 0316 and live in
// `integration/pair_fold_test.go`: a row that stops refusing moves, it does not disappear.
func TestCLIAgentTheMutationRoadsStillRefuseWhatTheyCannotCarry(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ name, src, want string }{
		{"a double written into a dict slot", pairMutationSlot + "d2 = {}\nd2[\"k\"] = xs[0] / 2\nprint(d2)\n", "stores an i32 word"},
		{"a double written into a list slot", pairMutationSlot + "ys = [0]\nys[0] = xs[0] / 2\nprint(ys)\n", "stores an i32 word"},
		{"a pair handed through a parameter into a dict", pairMutationSlot + "def build(k):\n    return {\"k\": k}\nprint(build(n))\n", "roadmap L11.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeSrc(t, dir, "pairmut_refuse.gy", tc.src)
			out, code := cliRunMerged(t, "--file", path)
			if code == 2 {
				t.Fatalf("exit 2 — LLVM rejected the module gusty emitted (ADR 0166):\n%s", out)
			}
			if code != 1 {
				t.Fatalf("exit %d, want 1 (a program this backend declines to build):\n%s", code, out)
			}
			if !strings.Contains(out, tc.want) {
				t.Errorf("the refusal does not name the missing half (%q):\n%s", tc.want, out)
			}
			noteCompiledGap(t, tc.src, out)
		})
	}
}

// TestThePairBoundMutationProbeIsOnRecord is the loud version of ADR 0302's missing-record rule for the
// program the conformance matrix registers: a deleted record fails a test instead of skipping the row
// (ADR 0311, ADR 0306's precedent).
func TestThePairBoundMutationProbeIsOnRecord(t *testing.T) {
	src := readProgram(t, "probe_a_pair_bound_name_mutates_a_container.gy")
	if !lang.HasGoldenAnswer(src) {
		t.Fatal("the probe the matrix registers has no record — every corpus case that reads expectations " +
			"from the record fails on it, starting with TestGCCorpusCollectsAndAgrees")
	}
	res, err := lang.JIT(src, 0)
	if err != nil {
		t.Fatalf("the compiled backend refused a program the reference prints: %v", err)
	}
	ref, ok := cpythonPlainOut(t, t.TempDir(), src)
	if !ok {
		t.Fatalf("the reference could not answer the probe")
	}
	if res.Output != ref {
		t.Fatalf("the compiled leg prints other bytes than the reference's:\n got %q\nwant %q", res.Output, ref)
	}
}
