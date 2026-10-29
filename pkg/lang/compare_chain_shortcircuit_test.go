package lang

// A comparison chain stops at the first link its verdict rejected.
//
// roadmap Gap R.520 · ADR 0451 (on top of **ADR 0288**'s node and **ADR 0450**'s checker)
//
// `a < b < c` is `a < b` AND `b < c`, with the middle operand read once — and the AND short-circuits: the
// reference evaluates `a`, evaluates `b`, asks the first question, and if that is false the chain is over.
// `c` is never evaluated, and the second question is never asked. ADR 0288 stored every operand into its own
// slot (which is what makes "once" a property of the module) and then lowered EVERY link eagerly and joined
// the verdicts with a flat `and i32`, on the reasoning that nothing was left for short-circuiting to skip.
// That was true of the operands and false of the links:
//
//	x = None / print(1 == x < 3)      reference: False   this backend: TypeError …'NoneType' and 'int'  exit 3
//	x = 5    / print(x > 9 < 1 // 0)  reference: False   this backend: ZeroDivisionError                   exit 3
//	print(3 < 2 < boom())             reference: False   this backend: "called" then False
//
// A link is not a pure expression. An ordering can raise, and an operand can print, divide, or allocate. The
// third row is the one to lose sleep over: its verdict was RIGHT, delivered beside a call the program never
// asked the run to make.
//
// The rows below therefore assert both directions, because either one alone is satisfiable by a broken
// lowering. A chain must SKIP what the verdict rejects (the three measurements above), and a chain that does
// continue must still evaluate its middle operand exactly once (**ADR 0288**'s claim, which a careless
// "just desugar it into `a < b and b < c`" rewrite would silently trade back).

import (
	"regexp"
	"strings"
	"testing"
)

// TestAChainStopsAtTheFirstLinkItsVerdictRejected is the row, measured against CPython in
// integration/compare_chain_shortcircuit_test.go and against the record here.
func TestAChainStopsAtTheFirstLinkItsVerdictRejected(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name string
		src  string
		want string
	}{
		{
			// The first link is False, so `None < 3` — an ordering the reference refuses — is never asked.
			name: "the unreached link is an ordering that cannot answer",
			src:  "x = None\nprint(1 == x < 3)\n",
			want: "False\n",
		},
		{
			name: "the unreached operand is a division by zero",
			src:  "x = 5\nprint(x > 9 < 1 // 0)\n",
			want: "False\n",
		},
		{
			name: "the unreached operand is a call",
			src:  "def boom():\n    print(\"called\")\n\n    return 5\n\nprint(3 < 2 < boom())\n",
			want: "False\n",
		},
		{
			// A long chain stops at the FIRST false link, not the last: nothing after it runs.
			name: "a four-operand chain stops at its first false link",
			src:  "def boom():\n    print(\"called\")\n\n    return 5\n\nprint(1 < 2 < 3 < 0 < boom())\n",
			want: "False\n",
		},
		{
			name: "the unreached link raises inside a handler, which never gets asked",
			src:  "x = None\ntry:\n    print(1 == x < 3)\nexcept TypeError:\n    print(\"caught\")\n",
			want: "False\n",
		},
		{
			name: "an unreached text against a number",
			src:  "s = \"a\"\nprint(2 < 1 < s)\n",
			want: "False\n",
		},
	}
	for _, r := range rows {
		r := r
		t.Run(r.name, func(t *testing.T) {
			t.Parallel()
			if got := compiledOut(t, r.src); got != r.want {
				t.Errorf("the compiled leg printed %q, want %q — a link the reference never reached was evaluated", got, r.want)
			}
			onRecordIs(t, r.src, r.want)
		})
	}
}

// TestAChainReachesWhatTheVerdictAllows is the other direction, and the one that keeps this fix from being a
// new bug: an operand or link the chain DOES reach still runs — exactly once.
func TestAChainReachesWhatTheVerdictAllows(t *testing.T) {
	t.Parallel()
	t.Run("a reached call still calls", func(t *testing.T) {
		t.Parallel()
		src := "def boom():\n    print(\"called\")\n\n    return 5\n\nprint(1 < 2 < boom())\n"
		if got := compiledOut(t, src); got != "called\nTrue\n" {
			t.Errorf("printed %q, want the call and True", got)
		}
		onRecordIs(t, src, "called\nTrue\n")
	})
	t.Run("a reached link still raises", func(t *testing.T) {
		t.Parallel()
		// `None == None` is True, so the chain DOES reach `None < 3` — and the reference raises there.
		src := "x = None\nprint(x == x < 3)\n"
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		code, out := runIRAllowingTrap(t, res.IR)
		if code == 0 {
			t.Fatalf("the chain answered %q where the reference raises", out)
		}
		if !strings.Contains(out, "TypeError") || !strings.Contains(out, "'<' not supported between instances of 'NoneType' and 'int'") {
			t.Errorf("the raise says %q, want the reference's sentence", out)
		}
	})
	t.Run("a reached raise is still catchable", func(t *testing.T) {
		t.Parallel()
		src := "x = None\ntry:\n    print(x == x < 3)\nexcept TypeError:\n    print(\"caught\")\n"
		if got := compiledOut(t, src); got != "caught\n" {
			t.Errorf("printed %q, want caught — a trap that stops being reachable is a trap lost", got)
		}
		onRecordIs(t, src, "caught\n")
	})
	t.Run("the middle operand of a continuing chain runs exactly once", func(t *testing.T) {
		t.Parallel()
		// ADR 0288's claim, re-asserted after ADR 0451 moved the operands into blocks: three occurrences of
		// one call in one chain, and each of them runs once — a rewrite that handed the ORIGINAL nodes to
		// both links would print `evaluated` twice for the middle one.
		src := "def g():\n    print(\"evaluated\")\n\n    return 5\n\nprint(1 < g() < 6)\nprint(g())\n"
		out := compiledOut(t, src)
		if n := strings.Count(out, "evaluated"); n != 2 {
			t.Errorf("ran g %d times, want 2 (one chain operand + one call): %q", n, out)
		}
		if !strings.Contains(out, "True") {
			t.Errorf("printed %q, want the verdict True", out)
		}
		onRecordIs(t, src, "evaluated\nTrue\nevaluated\n5\n")
	})
	t.Run("every answering chain still answers", func(t *testing.T) {
		t.Parallel()
		for _, r := range []struct{ src, want string }{
			{"print(1 < 2 < 3)\n", "True\n"},
			{"print(1 < 2 < 3 < 4)\n", "True\n"},
			{"print(1 < 2 < 3 < 1)\n", "False\n"},
			{"print(1 < 2 > 1)\n", "True\n"},
			{"print(1 > 2 < 3)\n", "False\n"},
			{"x = 5\nprint(1 < x < 10)\n", "True\n"},
			{"xs = [1, 2, 3]\nprint(0 < xs[1] < 3)\n", "True\n"},
			{"d = {\"a\": 5}\nprint(1 < d[\"a\"] < 10)\n", "True\n"},
			{"a = 1.5\nb = 2.5\nprint(0 < a + b < 5)\n", "True\n"},
			{"print(0 < abs(-2.5) < 3)\n", "True\n"},
			{"print(\"a\" < \"b\" < \"c\")\n", "True\n"},
			{"def f(x):\n    return 0 < x < 10\n\nprint(f(5), f(50))\n", "True False\n"},
			{"x = 0\nwhile 0 <= x < 3:\n    print(x)\n\n    x = x + 1\n", "0\n1\n2\n"},
			{"def h():\n    return 3\n\nprint(1 < 2 < h() < 9)\n", "True\n"},
		} {
			r := r
			t.Run(strings.TrimSpace(r.src), func(t *testing.T) {
				t.Parallel()
				got := compiledOut(t, r.src)
				if got != r.want {
					t.Errorf("printed %q, want %q — the short-circuit must not cost an answering chain its answer", got, r.want)
				}
				onRecordIs(t, r.src, r.want)
			})
		}
	})
}

// TestTheChainsBlocksAreWhereTheVerdictPutsThem is the shape claim. The answers above could be produced by a
// fold that got lucky; the module has to say that a later link is EMITTED in a block the previous verdict
// admits, and that the raise inside such a link is unreachable when the verdict has already rejected the
// chain.
func TestTheChainsBlocksAreWhereTheVerdictPutsThem(t *testing.T) {
	t.Parallel()
	t.Run("the unreached link's raise is behind a branch", func(t *testing.T) {
		t.Parallel()
		src := "x = None\nprint(1 == x < 3)\n"
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		ir := inMainBody(t, res.IR)
		branch := strings.Index(ir, "br i1 ")
		raise := strings.Index(ir, "@exn_flag")
		if branch < 0 {
			t.Fatalf("the module never branches on a link's verdict; the chain is still a flat `and`:\n%s", ir)
		}
		if raise < 0 {
			t.Fatalf("the module carries no raise at all, so there is nothing to place behind the branch:\n%s", ir)
		}
		if raise < branch {
			t.Errorf("the raise is emitted BEFORE the branch that decides whether the link runs, which is Gap R.520's shape:\n%s", ir)
		}
		if !strings.Contains(ir, "phi i32") {
			t.Errorf("the links are not merged by a phi, so one arm cannot answer False without lowering anything:\n%s", ir)
		}
	})
	t.Run("an effectful operand is emitted once, not once per link", func(t *testing.T) {
		t.Parallel()
		src := "def g():\n    print(\"evaluated\")\n\n    return 5\n\nprint(1 < g() < 6)\n"
		res, err := Compile(src)
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		ir := inMainBody(t, res.IR)
		if n := strings.Count(ir, "call i32 @gy_g("); n != 1 {
			t.Errorf("the middle operand's call appears %d times in @main, want 1 — a chain that hands the original node to both links is ADR 0288's bug re-imported:\n%s", n, ir)
		}
	})
	t.Run("a chain that answers needs no raise at all", func(t *testing.T) {
		t.Parallel()
		res, err := Compile("print(1 < 2 < 3)\n")
		if err != nil {
			t.Fatalf("compile: %v", err)
		}
		ir := inMainBody(t, res.IR)
		if strings.Contains(ir, "@exn_flag") {
			t.Errorf("an answering chain emits a raise it will never reach:\n%s", ir)
		}
	})
}

// TestTheChainConjunctionIsTheShortCircuitSkeletonNotAFlatAnd pins the artifact class away. The conjunction
// this row replaced was a machine instruction — `  %tN = and i32 %acc, %link` — over link verdicts that had
// ALL been computed, and no assertion on a printed value can tell that shape from the one that replaced it
// once the verdicts happen to agree (that is **Gap R.520**'s third program: right answer, wrong program).
// So the module itself is asked, over chain shapes whose links are ints, texts and a container read.
func TestTheChainConjunctionIsTheShortCircuitSkeletonNotAFlatAnd(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"x = None\nprint(1 == x < 3)\n",
		"x = 5\nprint(x > 9 < 1 // 0)\n",
		"print(1 < 2 < 3 < 4)\n",
		"print(1 < 2 < 3 < 1)\n",
		"x = 1.5\nprint(1 < x < 3)\n",
		"s = \"b\"\nprint(\"a\" < s < \"c\")\n",
		"xs = [1, 2, 3]\nprint(0 < xs[1] < 3)\n",
	} {
		src := src
		t.Run(strings.SplitN(src, "\n", 2)[1], func(t *testing.T) {
			t.Parallel()
			res, err := Compile(src)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			ir := inMainBody(t, res.IR)
			if strings.Contains(ir, " = and i32 ") {
				t.Errorf("the module conjoins link verdicts with an `and i32`, which is the flat shape Gap R.520 filed:\n%s", ir)
			}
			if !strings.Contains(ir, "br i1 ") {
				t.Errorf("no link's verdict decides a branch, so nothing is skipped:\n%s", ir)
			}
			if !strings.Contains(ir, "phi i32") {
				t.Errorf("the arms are not merged, so the settled arm cannot answer 0 without lowering a link:\n%s", ir)
			}
		})
	}
}

// TestTheChainAnswersTheArmsThePreviousVerdictSettled reads the skeleton block by block. Two claims, both of
// which a fold could fake and only the module can settle: the arm the previous verdict SETTLED carries the
// chain's `0` as a literal — it lowers no operand and asks no question — and the arm it left open carries
// the operand's one store, which is **ADR 0288**'s "exactly once" surviving the move into blocks.
func TestTheChainAnswersTheArmsThePreviousVerdictSettled(t *testing.T) {
	t.Parallel()
	res, err := Compile("def boom():\n    print(\"called\")\n\n    return 5\n\nx = 5\nprint(x > 9 < boom())\n")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	ir := inMainBody(t, res.IR)

	br := strings.Index(ir, "br i1 ")
	if br < 0 {
		t.Fatalf("the chain's first verdict does not branch:\n%s", ir)
	}
	// The arm the verdict rejects: its contribution to the merge is the literal 0, and it contains no call.
	phi := strings.Index(ir, "phi i32")
	if phi < 0 {
		t.Fatalf("no merge:\n%s", ir)
	}
	mergeLine := ir[phi:]
	if nl := strings.Index(mergeLine, "\n"); nl >= 0 {
		mergeLine = mergeLine[:nl]
	}
	if !strings.Contains(mergeLine, "[ 0, %") {
		t.Errorf("the settled arm of the merge does not answer the literal 0 — it lowers something it should not:\n%s", mergeLine)
	}
	// The operand of the link the run never reaches is called in ONE block, and it is the block the branch
	// opens rather than the one the chain stands in.
	if n := strings.Count(ir, "call i32 @gy_boom("); n != 1 {
		t.Fatalf("`boom` is called by %d instructions, want 1:\n%s", n, ir)
	}
	call := strings.Index(ir, "call i32 @gy_boom(")
	if call < br {
		t.Errorf("the call is emitted before the branch that decides whether the link runs:\n%s", ir)
	}
	if store := strings.Index(ir, "store i32 %t"); store >= 0 && store < br {
		t.Errorf("an operand slot is written before the branch that decides whether the link runs:\n%s", ir)
	}
	// The store into the operand's slot lives in the block the verdict admits, and there is exactly one of
	// them for that slot: ADR 0288's requirement, now stated as a property of the CFG (Gap R.520). The slot
	// allocator's own `store i32 0` initialiser is not an evaluation, so the count is of value stores.
	m := regexp.MustCompile(`(?m)^\s*(%(_chain\d+)) = alloca i32`).FindStringSubmatch(ir)
	if m == nil {
		t.Fatalf("the chain bound no operand slot — every operand here is cheap, which this row is not about:\n%s", ir)
	}
	slot := m[2] // the bare name; the IR spells the slot `%_chainN`
	stores, offset := 0, 0
	for _, line := range strings.Split(ir, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "store i32 ") && !strings.HasPrefix(trimmed, "store i32 0,") &&
			strings.HasSuffix(trimmed, "%"+slot) {
			stores++
			if offset < br {
				t.Errorf("the operand slot %s is written in the block the chain stands in, not the block its link lives in:\n%s", slot, ir)
			}
		}
		offset += len(line) + 1
	}
	if stores != 1 {
		t.Errorf("slot %s is written by %d value stores, want exactly once — the middle operand of a chain that continues owes one evaluation (ADR 0288)", slot, stores)
	}
}

// onRecordIs asks the record leg the question the compiled leg just answered: the source must be ON the
// record — a missing entry fails the case, which is what keeps coverage from being deleted by deleting a
// record (**ADR 0302**, **Gap R.204**) — and the bytes the record holds must be the bytes this leg printed.
func onRecordIs(t *testing.T, src, want string) {
	t.Helper()
	if !HasGoldenAnswer(src) {
		t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", src, src)
	}
	rec, err := runGoldenStdout(t, src)
	if err != nil {
		t.Fatalf("record leg: %v", err)
	}
	if rec != want {
		t.Errorf("the record leg printed %q, want %q", rec, want)
	}
}

// inMainBody is the program's own code, so a guard does not match the runtime's definitions.
func inMainBody(t *testing.T, ir string) string {
	t.Helper()
	start := strings.Index(ir, "define i32 @main(")
	if start < 0 {
		t.Fatalf("no @main in the module:\n%s", chainFirstLines(ir, 20))
	}
	end := strings.Index(ir[start:], "\n}")
	if end < 0 {
		return ir[start:]
	}
	return ir[start : start+end]
}

func chainFirstLines(s string, n int) string {
	parts := strings.SplitN(s, "\n", n+1)
	return strings.Join(parts, "\n")
}
