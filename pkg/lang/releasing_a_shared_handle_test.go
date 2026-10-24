// Roadmap Gap R.420 / ADR 0414 — a container released by a rebinding goes on the allocator's free list only
// when no registered root still names it, because a slot the free list holds is a slot rt_alloc hands to the
// next object.
//
//	d = {"a": 1}          d = {"a": 1}
//	e = d                 e = d
//	d = {"b": 2}    →     d = {"b": 2}
//	print(d)              print(d)   # {'b': 2}   both answer this one
//	print(e)              print(e)   # {'b': 2} ← this backend, before  — at exit 0
//	                                   # {'a': 1} ← the reference
//
// Two names, one object, and the rebinding of one of them pushed that object's slot onto the free list with
// the other name still reading it. The next allocation took the slot back and wrote a fresh dict over it, so
// `e`'s handle denoted the SECOND object — and `print(e["a"])` raised `KeyError: 'a'` over a dict that had
// never lost its `a`. The list and the set answered the same programs all along, which is what made it the
// release road's row rather than the notebook's (**ADR 0412**/**ADR 0413**'s alias records were already right).
//
// The collector could not undo it, and the reason is in `rt_gc`: it marks the whole free list dead (2) BEFORE
// it marks from the roots, and `rt_gc_mark` only marks a slot whose mark is 0 — so an object that had been
// pushed stayed pushable whatever the roots said. Letting the root mark override the free list's dead mark is
// the alternative this row rejected (**ADR 0414**): the slot would be live AND on the free list, and
// `rt_alloc` would hand a live object to the next container. The question has to be asked BEFORE the push.
//
// So `emitFree` now calls `@rt_release`, which asks the same table the collector asks — every root entry whose
// kind byte says "handle", walked to `gc_roots_used` — and frees nothing if any of them holds the handle.
// `emitFreeOld` clears the REBINDING's own entry first, so the question is "does any OTHER name hold it?" and
// not a permanent veto by the slot being overwritten. A skipped release costs an occupancy until the next
// sweep and never a live object: the sweep is the road allowed to decide liveness (ADR 0181's precision,
// ADR 0350's growth), which is what `TestWhatASkippedReleaseCostsIsBilledUntilTheSweep` measures rather than
// asserts.
//
// What this file pins:
//
//   - the answers, on the compiled leg and against the record, for the shapes the row measured — an alias, a
//     chain of three names, a name rebound twice, a container a dict also holds, the loop that churns them;
//   - `@rt_free` has ONE caller in a module, and it is `@rt_release`: the sweep pushes its own slots inline, so
//     "who else holds this?" has one place to be asked and one place to be read off the module;
//   - the instruction order of a rebinding, from the module: load → `rt_root_clear` → the null test →
//     `rt_release` → then the allocation of the new object and the store into the slot. Order is the row's
//     definition of done ("pin the instruction order from the module, so the order is a claim the suite holds
//     rather than a hope") — an answer that survives only while the emitter happens to emit in this order is
//     the same answer one refactor from being wrong again;
//   - the gate inside the door: a root whose slot holds a PAYLOAD — an int whose word happens to name a slot
//     index — vetoes nothing, because the kind byte is what the walk asks.
package lang

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// sharedHandleAnswered is the family this row pays: a container is rebound while something still holds the
// object it held, and the print that follows is the reference's.
func sharedHandleAnswered() []struct {
	name, src, want string
} {
	return []struct {
		name, src, want string
	}{
		// The two programs the row was filed with.
		{"the row's own program", "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(d)\nprint(e)\n", "{'b': 2}\n{'a': 1}\n"},
		{"the row's program, read through the alias", "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(d)\nprint(e)\nprint(e[\"a\"])\n", "{'b': 2}\n{'a': 1}\n1\n"},
		{"the entry the alias lost was never lost", "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(e[\"a\"])\nprint(\"a\" in e)\nprint(\"b\" in e)\nprint(len(e))\n", "1\nTrue\nFalse\n1\n"},

		// More than two names, and more than one rebinding: every name keeps the object it was given.
		{"a chain of three names, the far end rebound", "d = {\"a\": 1}\ne = d\nf = e\nd = {\"b\": 2}\nprint(d)\nprint(e)\nprint(f)\n", "{'b': 2}\n{'a': 1}\n{'a': 1}\n"},
		{"the same name rebound twice under an alias", "d = {\"a\": 1}\ne = d\nf = d\nd = {\"b\": 2}\nd = {\"c\": 3}\nprint(e)\nprint(f)\nprint(d)\n", "{'a': 1}\n{'a': 1}\n{'c': 3}\n"},
		{"the alias itself rebound, the original untouched", "d = {\"a\": 1}\ne = d\nf = e\ne = {\"b\": 2}\nprint(d)\nprint(e)\nprint(f)\n", "{'a': 1}\n{'b': 2}\n{'a': 1}\n"},
		{"a name handed back to the object it came from", "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nd = e\nprint(d)\nprint(e)\n", "{'a': 1}\n{'a': 1}\n"},
		{"the questions a container answers, asked of the held object", "d = {\"a\": 1, \"b\": 2}\ne = d\nd = {\"c\": 3}\nprint(len(e))\nprint(\"a\" in e)\nfor k in e:\n    print(k)\n", "2\nTrue\na\nb\n"},

		// The other containers answer this program too — the row's own control, because the LIST and the SET
		// were never wrong and a fix that breaks them is a different bug wearing this row's commit message.
		{"a list held by a second name", "xs = [1]\nys = xs\nxs = [2]\nprint(ys)\nprint(xs)\n", "[1]\n[2]\n"},
		{"a list held, then mutated through the other name", "xs = [1]\nys = xs\nxs = [2]\nxs.append(3)\nprint(ys)\nprint(xs)\n", "[1]\n[2, 3]\n"},
		{"a set held by a second name", "s = {1}\nt = s\ns = {2, 3}\nprint(t)\nprint(s)\n", "{1}\n{2, 3}\n"},

		// A container two doors reach at once: the dict's slot and the name both hold the same list, and
		// rebinding the name may not disturb what the dict still holds.
		{"a dict's slot and a name hold one list", "xs = [1, 2]\nd = {\"k\": xs}\nxs = [3]\nprint(d)\nprint(xs)\nprint(d[\"k\"])\n", "{'k': [1, 2]}\n[3]\n[1, 2]\n"},
		{"a value read out of a dict, then the dict rebound", "d = {\"a\": [1, 2]}\nv = d[\"a\"]\nd = {\"a\": [3]}\nprint(v)\nprint(d)\n", "[1, 2]\n{'a': [3]}\n"},

		// Frames: the alias and the rebind are both local, and the object escapes through the return.
		{"held across a frame's own rebinding", "def f():\n    d = {\"a\": 1}\n    e = d\n    d = {\"b\": 2}\n    return e\n\nprint(f())\n", "{'a': 1}\n"},

		// The churn loop: every iteration venders one object and holds another, so a skipped release that were
		// a leak would run the arena out long before the answer.
		{"a loop that venders and holds, 500 times", "n = 0\nfor i in range(500):\n    d = {\"a\": i}\n    e = d\n    d = {\"b\": i}\n    n = n + e[\"a\"]\nprint(n)\n", "124750\n"},
	}
}

// TestARebindingReleasesOnlyWhatNoNameHolds is both witness legs over the family: the compiled run's bytes,
// and the record's bytes for the same source. A MISSING record fails the case — an answer may not be
// certified by the backend that earned it (ADR 0302).
func TestARebindingReleasesOnlyWhatNoNameHolds(t *testing.T) {
	t.Parallel()
	for _, tc := range sharedHandleAnswered() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the release door refused a program the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
			if !HasGoldenAnswer(tc.src) {
				t.Fatalf("%q is not on the record — record it from the reference with tools/recmerge\n%s", tc.src, tc.src)
			}
			recorded, err := runGoldenStdout(t, tc.src)
			if err != nil {
				t.Fatalf("the record leg could not answer this source: %v\n%s", err, tc.src)
			}
			if recorded != tc.want {
				t.Fatalf("the record reads %q for this source and the compiled leg printed %q\n%s", recorded, got, tc.src)
			}
		})
	}
}

// TestTheTrapAfterARebindingIsTheReferenceTrap keeps the row's other half: before the fix the alias raised
// `KeyError: 'a'` over a dict that still had its `a`, so the trap was part of the lie. Now the miss is raised
// only where the reference misses, and the program's own handler sees it.
func TestTheTrapAfterARebindingIsTheReferenceTrap(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		{"the entry the alias really lost", "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\ntry:\n    print(e[\"b\"])\nexcept KeyError:\n    print(\"caught\")\n", "caught\n"},
		{"the entry the alias never lost, caught by a handler that must not run", "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\ntry:\n    print(e[\"a\"])\nexcept KeyError:\n    print(\"caught\")\n", "1\n"},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := compileAndRun(t, tc.src)
			if err != nil {
				t.Fatalf("the program was refused where the reference prints %q: %v\n%s", tc.want, err, tc.src)
			}
			if got != tc.want {
				t.Fatalf("printed %q, want %q\n%s", got, tc.want, tc.src)
			}
		})
	}
}

// freeCallRe / releaseCallRe are the two shapes the release door can leave in a module.
var (
	freeCallRe    = regexp.MustCompile(`(?m)^\s*call void @rt_free\(i32 ([^)]+)\)\s*$`)
	releaseCallRe = regexp.MustCompile(`(?m)^\s*call void @rt_release\(i32 ([^)]+)\)\s*$`)
	defineLineRe  = regexp.MustCompile(`(?m)^define [^\n]*$`)
)

// moduleDefines splits a module into its `define` blocks: a claim about WHERE a call stands is a claim about
// one block, and reading the whole module would let a call in the runtime certify a call in the program body.
func moduleDefines(ir string) map[string]string {
	type span struct {
		name  string
		start int
	}
	var spans []span
	for _, m := range defineLineRe.FindAllStringIndex(ir, -1) {
		line := strings.TrimSpace(ir[m[0]:m[1]])
		name := ""
		if i := strings.Index(line, "@"); i >= 0 {
			name = line[i+1:]
			if j := strings.IndexAny(name, "( "); j >= 0 {
				name = name[:j]
			}
		}
		spans = append(spans, span{name: name, start: m[0]})
	}
	out := map[string]string{}
	for i, s := range spans {
		end := len(ir)
		if i+1 < len(spans) {
			end = spans[i+1].start
		}
		out[s.name] = ir[s.start:end]
	}
	return out
}

// TestTheReleaseDoorIsTheOnlyRoadToTheFreeList is the row's claim stated as an IR fact: `@rt_free` — the free
// list's push — has exactly one caller in a compiled module, and it stands inside `@rt_release`, the door that
// asks the root table first. The collector's sweep pushes its own slots inline, so nothing else needs the
// door; and a rebinding that reached the free list directly is exactly the program this row was filed for.
func TestTheReleaseDoorIsTheOnlyRoadToTheFreeList(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(d)\nprint(e)\n",
		"xs = [1]\nys = xs\nxs = [2]\nprint(ys)\n",
		"s = {1}\nt = s\ns = {2, 3}\nprint(t)\n",
		"def f():\n    d = {\"a\": 1}\n    e = d\n    d = {\"b\": 2}\n    return e\n\nprint(f())\n",
	} {
		src := src
		t.Run(shortSrcLabel(src), func(t *testing.T) {
			t.Parallel()
			res, err := Compile(src)
			if err != nil {
				t.Fatalf("Compile: %v\n%s", err, src)
			}
			mustVerifyWithLLC(t, res.IR)
			defines := moduleDefines(res.IR)
			release, ok := defines["rt_release"]
			if !ok {
				t.Fatalf("the module carries no @rt_release — the release door is not in it:\n%s", src)
			}
			if !strings.Contains(release, "call void @rt_free(") {
				t.Fatalf("@rt_release never reaches @rt_free, so nothing is ever released:\n%s", release)
			}
			for name, body := range defines {
				if name == "rt_release" {
					continue
				}
				if strings.Contains(body, "call void @rt_free(") {
					t.Errorf("@%s calls @rt_free directly: the free list's push was reached without asking who "+
						"holds the handle (that is Gap R.420's shape):\n%s", name, src)
				}
			}
			if _, ok := defines["rt_gc"]; !ok {
				t.Fatalf("the module carries no @rt_gc — the sweep is supposed to push inline, not to call the door")
			}
		})
	}
}

// TestEveryReleaseIsGuardedByTheNullTest keeps the discipline `TestEmptyBindingNeverFreesHandleZero` exists
// for, at the door that exists now: 0 is a slot index and not an absence, so the handle is tested before it
// reaches the door that asks who holds it.
func TestEveryReleaseIsGuardedByTheNullTest(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"xs = [i for i in range(3)]\nys = []\nn = 0\nwhile xs:\n    n = n + 1\n    if n >= 3:\n        xs = []\n\nprint(n)\n",
		"d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(e)\n",
	} {
		src := src
		t.Run(shortSrcLabel(src), func(t *testing.T) {
			t.Parallel()
			res, err := Compile(src)
			if err != nil {
				t.Fatalf("Compile: %v\n%s", err, src)
			}
			calls := releaseCallRe.FindAllStringSubmatch(res.IR, -1)
			if len(calls) == 0 {
				t.Fatalf("expected the rebinding path to release; the module has no @rt_release call:\n%s", res.IR)
			}
			for _, m := range calls {
				reg := strings.TrimSpace(strings.TrimPrefix(m[1], "i32 "))
				if !strings.Contains(res.IR, "icmp ne i32 "+reg+", 0") {
					t.Errorf("rt_release(%s) is not guarded by a null test — 0 is slot 0, not \"no object\":\n%s", reg, res.IR)
				}
			}
			if n := len(freeCallRe.FindAllStringSubmatch(res.IR, -1)); n != 1 {
				t.Errorf("the module calls @rt_free %d time(s); the release door is its only caller, so the count is "+
					"one (the sweep pushes inline):\n%s", n, src)
			}
		})
	}
}

// TestTheRebindingEmitsLoadClearReleaseInThatOrder is the row's instruction-order pin, read off the module
// rather than asserted in prose. The order IS the fix: the slot is loaded while it still names the old
// object, its own root entry is tagged dead so the release is not vetoed by the very slot being overwritten,
// the handle goes to the door, and only then is the new object allocated and stored. Swap any adjacent pair
// and the row's program is wrong again — clear before load releases an untested word, release after the store
// frees the NEW object, alloc before the release hands the freed slot straight to the new dict.
func TestTheRebindingEmitsLoadClearReleaseInThatOrder(t *testing.T) {
	t.Parallel()
	src := "d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(d)\nprint(e)\n"
	res, err := Compile(src)
	if err != nil {
		t.Fatalf("Compile: %v\n%s", err, src)
	}
	body := moduleDefines(res.IR)["main"]
	if body == "" {
		t.Fatal("no main body to read the order out of")
	}
	calls := releaseCallRe.FindAllStringSubmatch(body, -1)
	if len(calls) != 1 {
		t.Fatalf("the rebinding emitted %d releases; this program rebinds one name once:\n%s", len(calls), body)
	}
	handle := strings.TrimSpace(strings.TrimPrefix(calls[0][1], "i32 "))
	releaseAt := strings.Index(body, "call void @rt_release(i32 "+handle+")")
	// afterRelease is where in the body something stands AFTER the door was asked — the position the new
	// object and the store into the slot must each occupy.
	afterRelease := func(needle string) int {
		i := strings.Index(body[releaseAt:], needle)
		if i < 0 {
			return -1
		}
		return releaseAt + i
	}
	seq := []struct {
		label string
		at    int
	}{
		{"the slot is read while it still names the old object", strings.Index(body, handle+" = load i32, i32* %_d")},
		{"the rebinding's own root entry is tagged dead", strings.Index(body, "call void @rt_root_clear(i32* %_d)")},
		{"the null test is asked", strings.Index(body, "icmp ne i32 "+handle+", 0")},
		{"the handle goes to the door", releaseAt},
		{"the new dict is built", afterRelease("call i32 @rt_alloc(")},
		{"the slot is written", afterRelease("store i32 ")},
	}
	for _, s := range seq {
		if s.at < 0 {
			t.Fatalf("the rebinding's module does not contain %s (%q) — the order this row pins is not the order the "+
				"emitter writes:\n%s", s.label, needleOf(s.label), body)
		}
	}
	for i := 1; i < len(seq); i++ {
		if seq[i-1].at >= seq[i].at {
			t.Errorf("the rebinding emits %q (%d) at or after %q (%d) — the order is the fix, and this module no "+
				"longer proves it:\n%s", seq[i-1].label, seq[i-1].at, seq[i].label, seq[i].at, body)
		}
	}
	if strings.Contains(body, "call void @rt_free(") {
		t.Errorf("main calls @rt_free itself; the release door is the only road to the free list:\n%s", body)
	}
}

// shortSrcLabel names a case by the first line of its source, which is the whole program for every shape
// this file carries and the interesting half for the longer ones.
func shortSrcLabel(src string) string {
	line := src
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if len(line) > 34 {
		line = line[:34]
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '"', '=', '{', '}', ':', ',', '(', ')', '[', ']', '.':
			return '_'
		}
		return r
	}, line)
}

// needleOf is only used to name a missing step in a failure message.
func needleOf(label string) string {
	switch label {
	case "the slot is read while it still names the old object":
		return "%fN = load i32, i32* %_d"
	case "the rebinding's own root entry is tagged dead":
		return "call void @rt_root_clear(i32* %_d)"
	case "the null test is asked":
		return "icmp ne i32 %fN, 0"
	case "the handle goes to the door":
		return "call void @rt_release(i32 %fN)"
	case "the new dict is built":
		return "call i32 @rt_alloc("
	case "the slot is written":
		return "store i32 %hN, i32* %_d"
	}
	return label
}

// TestTheReleaseDoorAsksTheCollectorsOwnTable pins the door's own body: it walks the SAME globals the
// collector walks, to the same bound, and asks the same kind byte. Two answers to "who is holding this" is how
// a future change re-opens this row, and ADR 0273's rule is that a question asked twice is decided once.
func TestTheReleaseDoorAsksTheCollectorsOwnTable(t *testing.T) {
	t.Parallel()
	res, err := Compile("d = {\"a\": 1}\ne = d\nd = {\"b\": 2}\nprint(e)\n")
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	release := moduleDefines(res.IR)["rt_release"]
	if release == "" {
		t.Fatal("no @rt_release in the module")
	}
	for _, want := range []string{
		"load i32, i32* @gc_roots_used", // the same bound rt_gc walks
		"@gc.kinds, i32 0, i32 %i",      // the same kind byte
		"icmp ne i8 %kv, 0",             // a slot tagged 0 holds a payload: it is not scanned
		"@gc.roots, i32 0, i32 %i",      // the same root stack
		"icmp eq i32 %rv, %h",           // and the same comparison against the handle being released
	} {
		if !strings.Contains(release, want) {
			t.Errorf("@rt_release does not %s; the door has to ask the collector's own table, in the collector's "+
				"own way, or the two answers drift:\n%s", want, release)
		}
	}
	if !strings.Contains(release, "call void @rt_free(") {
		t.Error("@rt_release never frees, so a released container is never recycled")
	}
	// The door must not be a second collector: it asks, and the sweep decides.
	if strings.Contains(release, "@gc_mark") || strings.Contains(release, "rt_gc_mark") {
		t.Error("@rt_release touches the marks: liveness is the sweep's to decide (ADR 0181), not the door's")
	}
}

// TestAPayloadRootVetoesNothing is the kind gate asked of the arena's own counter, rather than of a comment.
// A root whose slot holds an int is tagged 0 — the collector does not scan it and neither does the release
// door. If the door ever asks the PAYLOAD again, one of these ints (whose words are slot indices 0..12) names
// the handle being released and vetoes it, and the program's occupancy comes out one higher than the same
// program without them.
func TestAPayloadRootVetoesNothing(t *testing.T) {
	t.Parallel()
	const base = "d = {\"a\": 1}\nd = {\"b\": 2}\nprint(d)\n"
	const withPayloads = "d = {\"a\": 1}\na = 0\nb = 1\nc = 2\nd2 = 3\ne = 4\nf = 5\ng = 6\nh = 7\ni = 8\nj = 9\nk = 10\nl = 11\nm = 12\n" +
		"d = {\"b\": 2}\nprint(d)\n"
	occupancy := func(t *testing.T, src string) int {
		t.Helper()
		res, err := JITWithOptions(src, 0, &JITOptions{GCReport: true})
		if err != nil {
			t.Fatalf("the program could not be run: %v\n%s", err, src)
		}
		for _, line := range strings.Split(res.Stderr, "\n") {
			if !strings.HasPrefix(line, "gc: backend=aot") {
				continue
			}
			for _, kv := range strings.Fields(strings.TrimPrefix(line, "gc: backend=aot ")) {
				if v, ok := strings.CutPrefix(kv, "headroom="); ok {
					n, err := strconv.Atoi(v)
					if err != nil {
						t.Fatalf("the report's headroom= is not a number (%q): %s", v, line)
					}
					return n
				}
			}
			t.Fatalf("the report line carries no headroom=: %s", line)
		}
		t.Fatal("the run printed no collector report line")
		return 0
	}
	plain := occupancy(t, base)
	padded := occupancy(t, withPayloads)
	if plain == padded {
		return
	}
	t.Errorf("twelve int roots whose words are slot indices 0..12 changed the arena's headroom from %d to %d: a "+
		"payload is not a holder. @rt_release must ask the root entry's KIND byte, the way rt_gc does, or every "+
		"int in a program becomes a reason not to recycle its own slot.\n%s", plain, padded, withPayloads)
}

// TestWhatASkippedReleaseCostsIsBilledUntilTheSweep is the cost side of the decision, measured rather than
// promised: a release skipped because a name still holds the object is not a leak — the sweep that finds the
// object unrooted takes it. The program venders a held object per iteration 2000 times; if a skipped release
// were a permanent pin, the 1024-slot arena would run out and the answer would stop being the reference's.
func TestWhatASkippedReleaseCostsIsBilledUntilTheSweep(t *testing.T) {
	t.Parallel()
	const src = "total = 0\nfor i in range(2000):\n    d = {\"a\": i}\n    e = d\n    d = {\"b\": 1}\n    d = {\"c\": 2}\n    total = total + e[\"a\"]\nprint(total)\n"
	res, err := JITWithOptions(src, 0, &JITOptions{GCReport: true})
	if err != nil {
		t.Fatalf("the churn program could not be run: %v\n%s", err, src)
	}
	if res.Output != "1999000\n" {
		t.Fatalf("printed %q, want %q — a held object that is never reclaimed is a leak, and this loop is "+
			"2000 chances to notice one\n%s", res.Output, "1999000\n", src)
	}
	line := ""
	for _, l := range strings.Split(res.Stderr, "\n") {
		if strings.HasPrefix(l, "gc: backend=aot") {
			line = l
		}
	}
	if line == "" {
		t.Fatal("the churn program printed no collector report")
	}
	fields := map[string]string{}
	for _, kv := range strings.Fields(strings.TrimPrefix(line, "gc: backend=aot ")) {
		if k, v, ok := strings.Cut(kv, "="); ok {
			fields[k] = v
		}
	}
	if fields["exhausted"] != "0" {
		t.Errorf("the arena ran out (report: %s): the sweep is not billing back what the door declined to free", line)
	}
	if n, err := strconv.Atoi(fields["total_freed"]); err != nil || n == 0 {
		t.Errorf("the collector freed nothing across 2000 iterations that allocate four containers each (%s): the "+
			"sweep is supposed to be the road that decides liveness", line)
	}
}
