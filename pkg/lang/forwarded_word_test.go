package lang

// pkg/lang/forwarded_word_test.go — the SHAPE claims for the word a clone forwards to its entry
// (roadmap L11.7 · Gap R.375, ADR 0397).
//
// ADR 0395 gave a hoisted entry one parameter per name its body read out of the frame its APPLICATION was
// written in, spelled the clone's parameter with that name, and therefore had to DECLINE the program whose
// higher-order body had already bound the same name for itself: two parameters under one spelling is a body
// whose own read and the word it is being handed answer each other. ADR 0397 mints the parameter instead —
// the same door `freshName` mints the clone's own symbol — and keeps the VALUE arriving from the host
// frame's reading of the name the program wrote.
//
// So the claims this file makes are about three separate names that must not be confused:
//
//   - the name the PROGRAM wrote (`base`) — what the host reads, and the only name its value tables know;
//   - the name of the CLONE'S PARAMETER, minted by this pass and unspellable in the source;
//   - the name of the ENTRY's parameter, which stays the program's spelling because the entry's body is the
//     lambda that reads that name.
//
// The answering programs themselves live in `lambda_applied_test.go` (reference leg + record leg), and the
// same sources run through the shipped CLI against CPython in `integration/forwarded_word_test.go`.

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// forwardedWordCollisionPrograms are the programs where the higher-order body has ALREADY taken the name the
// entry reads — the family ADR 0395 declined out loud and ADR 0397 answers. `words` is how many parameters
// the pass must mint for the clone, which is the union of the entries' reads keyed by the name the host
// reads, not by the entry that reads it.
var forwardedWordCollisionPrograms = []struct {
	name  string
	src   string
	words int
	// reads lists, in the order the pass writes them, the name the HOST reads for each minted word.
	reads []string
}{
	{
		name:  "the body binds the name the entry reads",
		src:   "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n",
		words: 1, reads: []string{"base"},
	},
	{
		name:  "the name is one of the callee's own parameters",
		src:   "def apply1(fn, v, base):\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed, 9)\n\nprint(use(3))\n",
		words: 1, reads: []string{"base"},
	},
	{
		name:  "the body owns the name as a loop target",
		src:   "def apply1(fn, v):\n    for base in [7, 8]:\n        pass\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n",
		words: 1, reads: []string{"base"},
	},
	{
		name:  "the body owns one of the two names the entry reads",
		src:   "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    off = 10\n    return apply1(lambda x: x * base + off, seed)\n\nprint(use(3))\n",
		words: 2, reads: []string{"base", "off"},
	},
	{
		// Two entries, one name: the union is keyed by the host's read, so ONE parameter serves both calls.
		name:  "two entries read the one word the body owns",
		src:   "def both(f, g, v):\n    base = 100\n    return f(v) + g(v)\n\ndef use(seed):\n    base = 2\n    return both(lambda x: x + base, lambda x: x * base, seed)\n\nprint(use(3))\n",
		words: 1, reads: []string{"base"},
	},
	{
		name:  "three entries over two words the body owns",
		src:   "def third(f, g, h, v):\n    base = 5\n    off = 6\n    return f(v) + g(v) + h(v)\n\ndef use(seed):\n    base = 2\n    off = 3\n    return third(lambda x: x + base, lambda x: x * base, lambda x: x + off, seed)\n\nprint(use(3))\n",
		words: 2, reads: []string{"base", "off"},
	},
	{
		name:  "the callee calls its parameter twice under the collision",
		src:   "def twice(fn, seed):\n    base = 0\n    return fn(fn(seed))\n\ndef use(seed):\n    base = 1\n    return twice(lambda x: x * 2 + base, seed)\n\nprint(use(2))\n",
		words: 1, reads: []string{"base"},
	},
	{
		name:  "a higher-order recursion forwards the word down",
		src:   "def run(n, f):\n    base = 100\n    return 0 if n == 0 else f(n) + run(n - 1, f)\n\ndef use():\n    base = 1\n    return run(3, lambda x: x + base)\n\nprint(use())\n",
		words: 1, reads: []string{"base"},
	},
	{
		name:  "the body owns the name through a comprehension target",
		src:   "def apply1(fn, v):\n    base = [base for base in [9]]\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n",
		words: 1, reads: []string{"base"},
	},
}

// TestWhatAForwardedWordIsMintedToCarry asks the pass itself what it wrote onto the clone. The reference leg
// can be passed by a program whose numbers happen to line up; these are claims about the declarations the
// plan produced, read off the plan.
func TestWhatAForwardedWordIsMintedToCarry(t *testing.T) {
	for _, tc := range forwardedWordCollisionPrograms {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := planDefunctionalization(mustParseForCallables(t, tc.src))
			if err != nil || plan == nil {
				t.Fatalf("the pass declined a program whose word it can mint: %v", err)
			}
			var cl *callableClone
			for _, cf := range plan.order {
				for _, c := range cf.clones {
					if len(c.forwarded) > 0 {
						cl, _ = c, cf
					}
				}
			}
			if cl == nil {
				t.Fatalf("no clone of this program forwards a word at all")
			}
			if len(cl.forwarded) != tc.words {
				t.Fatalf("the clone forwards %d words, want %d: %+v", len(cl.forwarded), tc.words, cl.forwarded)
			}
			for i, f := range cl.forwarded {
				// The word the clone hands on is a PARAMETER, and a parameter the body could spell is a
				// parameter the body can shadow: that is the whole defect this row exists to remove.
				if f.word == f.from {
					t.Fatalf("the forwarded word is spelled with the captured name: %+v", f)
				}
				if !strings.Contains(f.word, callableForwardInfix) {
					t.Fatalf("the forwarded word %q was not minted by this pass (%q is its mark)", f.word, callableForwardInfix)
				}
				if !callableLexable(f.word) {
					t.Fatalf("the forwarded word %q is not a name the lexer could read back, so no traceback could say it", f.word)
				}
				if callableCloneOwnsName(cl.fn.def, f.word) {
					t.Fatalf("the body owns the minted word %q — the two reads answer each other again", f.word)
				}
				if isPredeclaredName(f.word) {
					t.Fatalf("the forwarded word %q shadows a predeclared name", f.word)
				}
				if i < len(tc.reads) && f.from != tc.reads[i] {
					t.Fatalf("word %d reads %q, want the host's %q", i, f.from, tc.reads[i])
				}
			}
			for i, a := range cl.forwarded {
				for j, b := range cl.forwarded {
					if i != j && a.word == b.word {
						t.Fatalf("words %d and %d share one parameter %q under reads %q and %q", i, j, a.word, a.from, b.from)
					}
					if i != j && a.from == b.from {
						t.Fatalf("the union is keyed by the host's read: %q appears twice", a.from)
					}
				}
			}
		})
	}
}

// TestWhatTheForwardingDeclarationIsParallel states the invariant the CALL road reads: a clone's parameter
// list, its list of capture parameters and the list of names the caller reads for them are the same length
// and line up entry by entry — because the road fills a forwarded word by reading one name in the caller's
// frame while the callee receives a slot named by another. A clone of an APPLIED lambda also carries the
// body's own captures (ADR 0372), spelled by the program, and those read themselves.
func TestWhatTheForwardingDeclarationIsParallel(t *testing.T) {
	for _, tc := range forwardedWordCollisionPrograms {
		t.Run(tc.name, func(t *testing.T) {
			out, rep := applyDefunctionalization(mustParseForCallables(t, tc.src))
			if rep == nil {
				t.Fatalf("the pass withdrew its own rewrite: %s", tc.src)
			}
			var cl *FuncDef
			for _, s := range out.Stmts {
				fd, ok := s.(*FuncDef)
				if !ok || fd.CloneOf == "" {
					continue
				}
				if len(fd.Captures) > 0 {
					cl = fd
				}
			}
			if cl == nil {
				t.Fatalf("the pass emitted no clone carrying a capture parameter")
			}
			if len(cl.CaptureReads) != len(cl.Captures) {
				t.Fatalf("CaptureReads (%d) and Captures (%d) are not parallel on %s", len(cl.CaptureReads), len(cl.Captures), cl.Name)
			}
			if len(cl.Params) < len(cl.Captures) {
				t.Fatalf("%s has fewer parameters than captures", cl.Name)
			}
			// The capture block is the TAIL of the parameter list, in the order the pass appended it, and
			// every one of its minted words is a name the source cannot have written.
			tail := cl.Params[len(cl.Params)-len(cl.Captures):]
			for i, p := range tail {
				if p == nil || p.Name != cl.Captures[i] {
					t.Fatalf("parameter %d is %v, the capture list says %q", i, p, cl.Captures[i])
				}
				read := cl.CaptureReads[i]
				if read != "" && !strings.Contains(p.Name, callableForwardInfix) {
					t.Fatalf("the read %q is recorded against a parameter the source spelled: %q", read, p.Name)
				}
				if read == "" && strings.Contains(p.Name, callableForwardInfix) {
					t.Fatalf("the minted word %q records no name for the caller to read", p.Name)
				}
			}
		})
	}
}

// TestWhatAForwardedWordCannotBeWrittenInTheSource is the claim the minting buys: the lexer's identifier rule
// is `[A-Za-z0-9_]`, so a word carrying this pass's mark cannot appear in a program — which is what lets the
// host's tables be asked about the program's own spelling while the callee's slot keeps a name nobody else
// can reach (roadmap L11.7 · Gap R.375; the same `$` argument ADR 0345 makes for a clone's SYMBOL).
func TestWhatAForwardedWordCannotBeWrittenInTheSource(t *testing.T) {
	for _, tc := range forwardedWordCollisionPrograms {
		plan, err := planDefunctionalization(mustParseForCallables(t, tc.src))
		if err != nil || plan == nil {
			t.Fatalf("the pass declined: %v", err)
		}
		for _, cf := range plan.order {
			for _, cl := range cf.clones {
				for _, f := range cl.forwarded {
					if !strings.Contains(f.word, "$") {
						t.Fatalf("the forwarded word %q carries no `$`, so the source could spell it", f.word)
					}
					for i := 0; i < len(f.word); i++ {
						c := f.word[i]
						if c == '$' {
							continue
						}
						if !isIdentChar(c) {
							t.Fatalf("the forwarded word %q carries %q, which the lexer would not read back", f.word, c)
						}
					}
					if strings.Contains(tc.src, f.word) {
						t.Fatalf("the forwarded word %q appears in the program's own text", f.word)
					}
				}
			}
		}
	}
}

// TestWhatTheForwardingPlanIsDeterministic — the record, the lit tests and `--emit-llvm` all read one module
// per source, and the pass mints names by counter (ADR 0340's “the same source, the same IR”). Two plans of
// one program, and one program compiled twice, must agree byte for byte.
func TestWhatTheForwardingPlanIsDeterministic(t *testing.T) {
	for _, tc := range forwardedWordCollisionPrograms {
		t.Run(tc.name, func(t *testing.T) {
			want := ""
			for round := 0; round < 2; round++ {
				plan, err := planDefunctionalization(mustParseForCallables(t, tc.src))
				if err != nil || plan == nil {
					t.Fatalf("the pass declined: %v", err)
				}
				var words []string
				for _, cf := range plan.order {
					for _, cl := range cf.clones {
						for _, f := range cl.forwarded {
							words = append(words, cl.name+"="+f.word+"<-"+f.from)
						}
					}
				}
				got := strings.Join(words, ",")
				if want == "" {
					want = got
					continue
				}
				if got != want {
					t.Fatalf("the plan minted %q, the second plan %q", want, got)
				}
			}
			first := compileSrc(t, tc.src)
			if second := compileSrc(t, tc.src); first != second {
				t.Fatalf("the module changed between two compiles of one source")
			}
		})
	}
}

// TestWhatAClonedDefForwardsAWordTheBodyCannotAnswer reads the emitted module, per region — the claim the
// answering tables cannot make because a wrong slot can print the right number by luck. Three regions, three
// questions: the CLONE takes one parameter more than the source `def` wrote; the clone's call of the ENTRY
// reads only parameters (`%p…`) and never the clone's own `base` slot, which holds the body's 9; and the
// HOST'S call fills that parameter from its own reading of the name the program wrote.
func TestWhatAClonedDefForwardsAWordTheBodyCannotAnswer(t *testing.T) {
	const src = "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n"
	mod := compileSrc(t, src)
	clone := ""
	for _, line := range strings.Split(mod, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "define ") && strings.Contains(line, callableCloneInfix) {
			clone = strings.TrimSpace(line)
			break
		}
	}
	if clone == "" {
		t.Fatalf("the pass emitted no clone of the higher-order def:\n%s", headLines(mod, 40))
	}
	// `def apply1(fn, v)` — two parameters in the source; the third is the minted word.
	if n := strings.Count(clone, "i32 %p"); n != 3 {
		t.Fatalf("the clone does not carry the placeholder, the value AND the forwarded word: %q", clone)
	}
	body := mod
	if i := strings.Index(mod, clone); i >= 0 {
		if j := strings.Index(mod[i:], "\n}"); j >= 0 {
			body = mod[i : i+j]
		}
	}
	// The clone's call of the entry passes two words, both parameters. Had the forwarded word kept the
	// program's spelling, this is where the body's own read would leak in: the operand would be the clone's
	// `%_base`, loaded.
	if !regexp.MustCompile(`call i32 @gy_[^(]*\$lambda\$[0-9a-f_A-Za-z]+_\d+\(i32 %p\d+, i32 %p\d+\)`).MatchString(body) {
		t.Fatalf("the clone does not forward its own parameter into the call of the entry:\n%s", body)
	}
	if regexp.MustCompile(`call i32 @gy_[^(]*\$lambda\$[^)]*%_base`).MatchString(body) {
		t.Fatalf("the clone reads the body's own name where the forwarded word belongs:\n%s", body)
	}
	// The body still owns its own `base`: the constant it bound is in the clone's own code, and reading it
	// is a different question from handing the word on.
	if !strings.Contains(body, "9") {
		t.Fatalf("the body's own binding disappeared from the clone:\n%s", body)
	}
	// The host fills the minted parameter from the frame that OWNS the value — `%_base`, its own slot.
	if !regexp.MustCompile(`call i32 @gy_[^(]*` + regexp.QuoteMeta(callableCloneInfix) + `[0-9a-f]*_\d+\(i32 0, i32 %p\d+, i32 %_base[^,)]*\)`).MatchString(mod) {
		t.Fatalf("the application does not fill the forwarded word from the host frame's own reading:\n%s", headLines(mod, 60))
	}
	// ADR 0345 §1: a callable is a named `define`, not a word of machine code.
	for _, junk := range []string{"ptrtoint", "inttoptr", "call <2 x i32>", "call {}"} {
		if strings.Contains(mod, junk) {
			t.Fatalf("the module reached for %q instead of a named call:\n%s", junk, headLines(mod, 40))
		}
	}
}

// TestWhatTheRecursionForwardsTheWordItWasHanded is the same claim one level down: a clone that calls itself
// hands the word DOWN, because the frame the recursion lives in owns nothing — the host that picked this
// clone owns the name. The recursive call's last operand is therefore the clone's own minted parameter, a
// `%p` register, and not a load of anything named by the program.
func TestWhatTheRecursionForwardsTheWordItWasHanded(t *testing.T) {
	const src = "def run(n, f):\n    base = 100\n    return 0 if n == 0 else f(n) + run(n - 1, f)\n\ndef use():\n    base = 1\n    return run(3, lambda x: x + base)\n\nprint(use())\n"
	mod := compileSrc(t, src)
	clone, start := "", -1
	lines := strings.Split(mod, "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "define ") && strings.Contains(line, callableCloneInfix) {
			clone, start = strings.TrimSpace(line), i
			break
		}
	}
	if clone == "" {
		t.Fatalf("no clone was emitted:\n%s", headLines(mod, 40))
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "}") {
			end = i
			break
		}
	}
	region := strings.Join(lines[start:end], "\n")
	// The clone takes three words — `n`, `f`, and the forwarded one — and its recursive call passes three,
	// the last being its own minted parameter.
	if n := strings.Count(clone, "i32 %p"); n != 3 {
		t.Fatalf("the recursive clone does not carry the forwarded word: %q", clone)
	}
	// The clone owns a `base` of its own (`base = 100` in the source) — the body's read survives the minting,
	// which is the point: it is a DIFFERENT slot from the word it hands down.
	if !strings.Contains(region, "store i32 100, i32* %_base") {
		t.Fatalf("the body's own binding is not its own slot in the clone:\n%s", region)
	}
	var recursive []string
	for _, line := range lines[start:end] {
		if strings.HasPrefix(strings.TrimSpace(line), "define ") {
			continue
		}
		if strings.Contains(line, "= call ") && strings.Contains(line, callableCloneInfix) {
			recursive = append(recursive, strings.TrimSpace(line))
		}
	}
	if len(recursive) == 0 {
		t.Fatalf("the recursive clone never calls itself:\n%s", region)
	}
	for _, line := range recursive {
		if !strings.HasSuffix(line, ", i32 %p2)") {
			t.Fatalf("the recursion does not hand the forwarded word down: %q", line)
		}
		// The word travels as the parameter the host filled, never as a load of the body's own name — which
		// is the shape this row exists to make impossible.
		if strings.Contains(line, "%_base") {
			t.Fatalf("the recursive call reads the body's own name where the forwarded word belongs: %q", line)
		}
	}
}

// TestWhatTheGateAsksTheFrameThatOwnsTheName is ADR 0395 §4's rule at the new position, and the reason the
// minting did not quietly widen Gap R.332: the one-word question is asked of the frame that OWNS the value,
// never of the clone that merely holds the slot. Each row owns the name twice — as a NUMBER in the
// higher-order body, where a table read of the clone would say the value travels — and as the kind that
// cannot travel in the HOST, which is the answer the refusal has to give.
func TestWhatTheGateAsksTheFrameThatOwnsTheName(t *testing.T) {
	for _, tc := range []struct {
		name, src, want string
	}{
		{
			name: "a text the host owns while the body holds a number",
			src:  "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = \"hey\"\n    return apply1(lambda x: x + len(base), seed)\n\nprint(use(3))\n",
			want: "holds a text",
		},
		{
			name: "a float the host owns while the body holds a number",
			src:  "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = 1.5\n    return apply1(lambda x: x + base, seed)\n\nprint(use(3))\n",
			want: "holds a float",
		},
		{
			name: "a container the host owns while the body holds a number",
			src:  "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = [1, 2]\n    return apply1(lambda x: x + len(base), seed)\n\nprint(use(3))\n",
			want: "holds a container",
		},
		{
			name: "the void the host owns while the body holds a number",
			src:  "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = None\n    return apply1(lambda x: x + len([base]), seed)\n\nprint(use(3))\n",
			want: "holds the void value",
		},
		{
			name: "a verdict the host owns while the body holds a number",
			src:  "def apply1(fn, v):\n    base = 9\n    return fn(v)\n\ndef use(seed):\n    base = True\n    return apply1(lambda x: x + len([base]), seed)\n\nprint(use(3))\n",
			want: "holds a bool",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The reference answers every one of these; the compiled backend refuses, which is the honest
			// answer the gate has always given (Gap R.332) and not this row's to widen.
			msg := compiledRefusalOf(tc.src)
			if msg == "" {
				t.Fatalf("compiled clean; want a refusal mentioning %q", tc.want)
			}
			if !strings.Contains(msg, tc.want) {
				t.Fatalf("refusal = %q, want it to mention %q", msg, tc.want)
			}
			if strings.Contains(msg, callableForwardInfix) {
				t.Fatalf("the refusal leaked the minted word %q instead of the name the program wrote:\n%s", callableForwardInfix, msg)
			}
			if strings.Contains(msg, "0x") || strings.Contains(msg, "%!s(") || strings.Contains(msg, "*lang.") {
				t.Fatalf("the refusal leaks the compiler's own machinery:\n%s", msg)
			}
		})
	}
}

// TestWhatTheForwardedWordAccountNamesNoDecline is the account half of Gap R.337's rule: a program this row
// answers must read as rewritten in `--callables`, with the words it minted counted and nothing refused.
func TestWhatTheForwardedWordAccountNamesNoDecline(t *testing.T) {
	for _, tc := range forwardedWordCollisionPrograms {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := CallablesJSON(tc.src)
			if err != nil {
				t.Fatalf("the account failed to print: %v", err)
			}
			var rep struct {
				Rewritten         bool     `json:"rewritten"`
				CaptureParameters int      `json:"capture_parameters"`
				Refused           []string `json:"refused"`
				Symbols           []string `json:"symbols"`
			}
			if err := json.Unmarshal([]byte(doc), &rep); err != nil {
				t.Fatalf("the account is not JSON: %v\n%s", err, doc)
			}
			if !rep.Rewritten || len(rep.Refused) != 0 {
				t.Fatalf("the pass declined or refused: %s", doc)
			}
			if rep.CaptureParameters < tc.words {
				t.Fatalf("capture_parameters = %d, want at least %d:\n%s", rep.CaptureParameters, tc.words, doc)
			}
			if !strings.Contains(doc, callableLambdaInfix) {
				t.Fatalf("the entry never got a declaration to carry the word:\n%s", doc)
			}
		})
	}
}

// TestTheForwardedWordFamilyIsOnTheRecord is the record leg over this file's own sources: a missing record is
// a hard failure here, because these programs were refused when the file was written and a row that quietly
// stops being covered would otherwise read as passing.
func TestTheForwardedWordFamilyIsOnTheRecord(t *testing.T) {
	onRecord := 0
	for _, tc := range forwardedWordCollisionPrograms {
		t.Run(tc.name, func(t *testing.T) {
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge, never from this backend\n%s", tc.name, tc.src)
			}
			onRecord++
		})
	}
	if onRecord == 0 {
		t.Fatal("not one forwarded-word program is on the record: the record leg is not being walked")
	}
}

// TestWhatTheForwardedWordRowsRefuseNothingTheyClaim is ADR 0273 over this file: every program the row
// claims to answer must not come back as a refusal, and must not leak the minted spelling into a diagnostic.
func TestWhatTheForwardedWordRowsRefuseNothingTheyClaim(t *testing.T) {
	for _, tc := range forwardedWordCollisionPrograms {
		t.Run(tc.name, func(t *testing.T) {
			if msg := compiledRefusalOf(tc.src); msg != "" {
				t.Fatalf("the row refused a program it claims to answer: %s", msg)
			}
		})
	}
}
