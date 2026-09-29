package lang

import (
	"strings"
	"testing"
)

// `print` and `range` sat in the lexer's *keyword* table, so they were tokenized as keywords rather
// than identifiers — which made `def print(x)` a parse error ("expected identifier") and with it
// every parameter, keyword argument, attribute and method that wanted those words. A built-in is a
// name a program owns (ADR 0199); the keyword table is for words that change grammar (Gap R.9,
// ADR 0203).

func TestBuiltInNamesMayBeDefined(t *testing.T) {
	for _, tc := range []struct{ name, src string }{
		{"def print", "def print(x):\n    return x + 1\n\nz = print(2)\n"},
		{"def range", "def range(a):\n    return a * 2\n\nz = range(3)\n"},
		{"def str", "def str(x):\n    return 1\n\nz = str(1)\n"},
		{"def len", "def len(x):\n    return 1\n\nz = len(0)\n"},
		{"parameters named after built-ins", "def f(print, range, len):\n    return print\n\nz = f(1, 2, 3)\n"},
		{"a method named range", "class C:\n    def range(self, n):\n        return n * 4\n\nz = C().range(2)\n"},
		{"a method named print", "class C:\n    def print(self, n):\n        return n\n\nz = C().print(2)\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Parse(tc.src); err != nil {
				t.Errorf("%s did not parse: %v", tc.name, err)
			}
		})
	}
}

func TestBuiltInNamesStillWorkAsBuiltIns(t *testing.T) {
	// Removing them from the keyword table must not remove the built-ins themselves: everything
	// downstream already resolves them by name.
	for _, src := range []string{
		"print(1)\n",
		"print(1, sep=\",\", end=\"!\")\n",
		"for i in range(3):\n    print(i)\n",
		"for i in range(1, 9, 2):\n    print(i)\n",
		"xs = [x for x in range(4)]\n",
		"print(sum([x for x in range(5)]))\n",
	} {
		prog, err := Parse(src)
		if err != nil {
			t.Errorf("a built-in use stopped parsing: %v (%q)", err, src)
			continue
		}
		for _, d := range Analyze(prog) {
			if d.Level == LevelError {
				t.Errorf("a built-in use was refused: %v (%q)", d, src)
			}
		}
	}
}

// TestKeywordArgumentNamesMayBeBuiltInNames pins the call-site half: `f(print=7)` is a keyword
// argument called `print`, not the built-in being passed a name.
func TestKeywordArgumentNamesMayBeBuiltInNames(t *testing.T) {
	src := "def use_both(print, range):\n    return print + range\n\nz = use_both(print=7, range=8)\n"
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("a keyword argument named after a built-in did not parse: %v", err)
	}
	for _, d := range Analyze(prog) {
		if d.Level == LevelError {
			t.Errorf("a keyword argument named after a built-in was refused: %v", d)
		}
	}
}

// TestGrammarWordsRemainReserved is the other side of the rule: the words that really change
// grammar are still not names, so the keyword table was narrowed rather than dropped.
func TestGrammarWordsRemainReserved(t *testing.T) {
	for _, name := range []string{"def", "if", "else", "while", "for", "in", "class", "return", "match", "case", "try", "except", "finally", "yield", "lambda", "import", "with", "as", "not", "and", "or", "is", "None", "True", "False"} {
		if _, err := Parse("def " + name + "(x):\n    return x\n"); err == nil {
			t.Errorf("%q was accepted as a function name; it is a keyword", name)
		} else if !strings.Contains(err.Error(), "expected identifier") {
			t.Errorf("%q failed for the wrong reason: %v", name, err)
		}
	}
}

// TestBuiltInNamesLexAsIdentifiers states the mechanism directly, so a future change that re-adds a
// built-in to the keyword table fails here rather than in someone's parser error.
func TestBuiltInNamesLexAsIdentifiers(t *testing.T) {
	for _, word := range []string{"print", "range", "len", "str", "int", "float", "abs", "min", "max", "sum", "printf"} {
		toks, err := Lex(word + "\n")
		if err != nil {
			t.Fatalf("lex %q: %v", word, err)
		}
		var found *Token
		for i := range toks {
			if toks[i].Text == word {
				found = &toks[i]
			}
		}
		if found == nil {
			t.Fatalf("no token for %q", word)
		}
		if found.Kind != TokIdent {
			t.Errorf("%q lexed as %v, not an identifier: it is a built-in, not a keyword", word, found.Kind)
		}
	}
}
