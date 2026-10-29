package lang

import (
	"strings"
	"testing"
)

// A copy `rt_slice` builds is ALLOCATED, parked in a bare register, and then read by a road that is free to
// CALL — and a call's body collects (ADR 0347, ADR 0317). The copy is live and reachable from no root, which
// is the one shape ADR 0181 forbids. ADR 0347 rooted the container a LOOP walks; ADR 0443's
// `gcRootDuringBuild` rooted a comprehension's half-built object (Gap R.500); the copy was the remaining
// bare-register container the roads mint (roadmap Gap R.541, ADR 0459).
//
// The attribution is by control build, because a GC window cannot be read off a module: with the root taken
// back out of `emitSlice` and nothing else changed, the first row below prints `['k'] ['a']` where CPython
// prints `[1.5] ['a']` — the copy of a list come back holding the interned index of the dict's own KEY — at
// the exit code of success. An unrooted object does not fail; it answers a DIFFERENT program, which is the
// class ADR 0166 files loudest. With the root, every row below is the reference's bytes on both legs.
var sliceCopySurvivalPrograms = []struct {
	name string
	src  string
	want string
}{
	{
		// The measured shape: two copies in one print. The second argument's call is the collection point,
		// and it runs after the first copy was built and before it is printed.
		"two copies of a slot, one print — the second argument's call is the collection point",
		"def m():\n    return {\"k\": [1.5, \"a\"]}\nprint(m()[\"k\"][0:1], m()[\"k\"][1:2])",
		"[1.5] ['a']",
	},
	{
		"a copy printed beside the container it came from",
		"def m():\n    return {\"k\": [1.5, \"a\"]}\nprint(m()[\"k\"][0:1], m())",
		"[1.5] {'k': [1.5, 'a']}",
	},
	{
		"three reads of one variable's list, the copies live across each other",
		"xs = [1.5, \"a\", [7]]\nprint(xs[0:2], xs[1:3], len(xs))",
		"[1.5, 'a'] ['a', [7]] 3",
	},
	{
		"a loop over a copy whose body copies again, per iteration",
		"xs = [1, 2, 3]\nfor v in xs[0:2]:\n    print(v, xs[0:1])",
		"1 [1]\n2 [1]",
	},
	{
		"a slot of a copy printed beside another call's copy",
		"def n():\n    return [10, 20, 30]\nprint(n()[0:2][0], n()[1:3][1])",
		"10 30",
	},
	{
		"two copies of the list a name holds",
		"d = {\"k\": [1, 2, 3]}\nprint(d[\"k\"][0:2], d[\"k\"][1:3])",
		"[1, 2] [2, 3]",
	},
	{
		"two comprehension answers, each copied inside its own bounds",
		"print([x for x in [1, 2, 3]][0:2], [x for x in [4, 5]][0:1])",
		"[1, 2] [4]",
	},
	{
		"a copy of a copy, and the object the copy was taken from, in one print",
		"xs = [1.5, \"a\", [7]]\nys = xs[0:2]\nprint(ys[0:1], xs[0:2][1], ys)",
		"[1.5] a [1.5, 'a']",
	},
}

// TestACopySurvivesTheCollectionItsOwnStatementTriggers is the row's behavioural half: every shape where a
// copy is live across a call answers the reference's bytes, on the compiled leg and against the record.
func TestACopySurvivesTheCollectionItsOwnStatementTriggers(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceCopySurvivalPrograms {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ir := mustCompileIR(t, tt.src)
			assertNoForbiddenIR(t, tt.src, ir)
			got, refused := compiledOutOrRefusal(t, tt.src)
			if refused {
				t.Fatalf("%q refused (%s); the reference answers %q", tt.src, got, tt.want)
			}
			if got != tt.want+"\n" {
				t.Fatalf("%q printed %q, the reference prints %q", tt.src, got, tt.want+"\n")
			}
			rec, err := runGoldenStdout(t, tt.src)
			if err != nil {
				t.Fatalf("the record does not answer a program both legs agree on: %v", err)
			}
			if strings.TrimRight(rec, "\n") != tt.want {
				t.Fatalf("the record says %q and the compiled leg says %q for the same source", rec, tt.want)
			}
		})
	}
}

// TestTheCopyIsRootedWhereItIsBuilt asks the claim of the emitted module rather than of the runtime, because
// a window that only sometimes fires cannot be pinned by a run: the handle `rt_slice` answers is stored into
// a frame slot and that slot is pushed as a root before anything else the statement does.
func TestTheCopyIsRootedWhereItIsBuilt(t *testing.T) {
	t.Parallel()
	for _, tt := range sliceCopySurvivalPrograms {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ir := mustCompileIR(t, tt.src)
			copies := 0
			for _, line := range strings.Split(ir, "\n") {
				if !strings.Contains(line, "= call i32 @rt_slice(") {
					continue
				}
				copies++
				handle := strings.TrimSpace(strings.SplitN(line, "=", 2)[0]) // %slN
				store := "store i32 " + handle + ", i32* %_sroot"
				if !strings.Contains(ir, store) {
					t.Fatalf("the copy %s was never parked in a root slot:\n%s", handle, ir)
				}
				// The store and the push stand beside each other: an address registered before the slot
				// holds a handle roots the stale 0, which is the same hole with a different shape.
				at := strings.Index(ir, store)
				push := strings.Index(ir[at:], "call void @rt_root_put(i32* %_sroot")
				if push < 0 || push > 90 {
					t.Fatalf("the copy's root slot is not pushed beside the store that fills it:\n%s", ir)
				}
			}
			if copies == 0 {
				t.Fatalf("the program no longer builds a copy at all — this case has nothing to witness:\n%s", ir)
			}
		})
	}
}

// TestTheCopyRootOutlivesTheStatementThatBuiltIt is the retention rule the row chose ON PURPOSE, pinned so a
// later "cleanup" cannot quietly close the window at the wrong altitude: clearing the copy's root at the end
// of its statement unroots an object whose consumer (the print, the pair door, the loop's walk) lowers and
// reads later. The root therefore lives until the frame drops it, and the frame's close is the release.
func TestTheCopyRootOutlivesTheStatementThatBuiltIt(t *testing.T) {
	t.Parallel()
	ir := mustCompileIR(t, sliceCopySurvivalPrograms[0].src)
	if strings.Contains(ir, "rt_root_clear(i32* %_sroot") {
		t.Fatalf("the copy's root is cleared by the statement that built it — the consumer prints it later:\n%s", ir)
	}
	if !strings.Contains(ir, "call void @rt_frame_close(") {
		t.Fatalf("the module has no frame close to release the copy's root:\n%s", ir)
	}
}

// TestACopyInsideALoopRootsOneSlotPerSite is the bound on that retention: the alloca is per SLICE SITE and
// the runtime dedups root entries by address, so a loop that copies a thousand times registers one slot
// rather than a thousand. The module is the evidence — the loop body carries one `%_sroot` alloca, and the
// push sits inside the loop.
func TestACopyInsideALoopRootsOneSlotPerSite(t *testing.T) {
	t.Parallel()
	ir := mustCompileIR(t, "xs = [1, 2, 3]\nfor v in xs[0:2]:\n    print(v, xs[0:1])")
	allocas := 0
	pushes := 0
	for _, line := range strings.Split(ir, "\n") {
		if strings.Contains(line, "= alloca i32") && strings.Contains(line, "%_sroot") {
			allocas++
		}
		if strings.Contains(line, "call void @rt_root_put(i32* %_sroot") {
			pushes++
		}
	}
	// Two slice sites in the program (the iterable and the body's copy); a third would mean the root is
	// minted per instruction rather than per site, and a loop would grow the frame's root stack.
	if allocas == 0 || allocas > 2 {
		t.Fatalf("expected one root slot per slice site, saw %d:\n%s", allocas, ir)
	}
	if pushes != allocas {
		t.Fatalf("a copy slot was filled without being pushed (%d slots, %d pushes):\n%s", allocas, pushes, ir)
	}
}

// TestWhatTheCopyDoorStillCannotReachIsFiledNotFixed keeps the shapes this row did NOT open as refusals, so
// they stay owed rather than becoming answers nobody measured. Each is refused at exit 1 with the sentence
// of the door that declines it — never a wrong number at exit 0 (ADR 0166, ADR 0249).
func TestWhatTheCopyDoorStillCannotReachIsFiledNotFixed(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		src     string
		mustSay string
	}{
		{
			// Gap R.539: the loop copies a container whose slots mix kinds, so no builder ever claimed what
			// the copy holds, and the door will not invent the claim.
			"a copy of a comprehension over a list that mixes kinds",
			"print([x for x in [1, \"a\"]][0:2])",
			"",
		},
		{
			// Gap R.540: the name holds a slot the records only describe through the object it was read
			// out of, and the copy's gate asks for a base the records describe.
			"a copy taken from a name bound by a slot read",
			"def m():\n    return {\"k\": [1.5, \"a\"]}\nd = m()[\"k\"]\nprint(d[0:1])",
			"cannot slice",
		},
		{
			// Gap R.146's shape one construct further in: the pair a comparison needs has no one-word
			// position to be read in.
			"a copy read twice and compared to a text",
			"xs = [1.5, \"a\", [7]]\nprint(xs[0:2][1] == \"a\")",
			"",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, refused := compiledOutOrRefusal(t, tt.src)
			if !refused {
				t.Fatalf("%q answered %q; this shape is filed as owed, not fixed — if it now works, move it to the answered table", tt.src, out)
			}
			if tt.mustSay != "" && !strings.Contains(out, tt.mustSay) {
				t.Fatalf("the refusal does not name %q (ADR 0249): %s", tt.mustSay, out)
			}
		})
	}
}
