# A number the spelling states is still a program around the elements it counts: a container-answering element is performed by the one door asked of it, and the handle it hands back is dropped

## Status

Accepted — roadmap **Gap R.534** closed, **Gap R.533** re-pinned beside it, and **Gap R.542**, **Gap
R.543**, **Gap R.544** and **Gap R.545** filed by measurement in this row. Follows **ADR 0312** (*the fold must not skip the effects of what it folds away* — this row is
that clause applied to the door **ADR 0457** left standing), **ADR 0457** (*a container literal's element
that answers with a container asks the door that builds the answer, not the road that folds the literal*),
**ADR 0333** (*a container handed back by a call crosses as a (payload, tag) pair* — the road this door
reuses rather than imitates), **ADR 0447** (*a value whose shape can be asked is asked, not lowered blind*),
**ADR 0324** (*an effectful call inside a folded container is performed, not skipped*), **ADR 0302** (the
record leg is a witness; a missing entry fails a case), **ADR 0343** (an owed divergence stays in the suite
pinned, it does not leave), **ADR 0166** (exit 2 is the compiler's own bug, so a road that reaches it is
declined rather than patched mid-row), **ADR 0321** (a fix's root is found by *filtering*, not by widening
a gate), **ADR 0294** (one root, one gate), **ADR 0445** (ask the door that already answers the question).

## Context

`pkg/lang/length_element_effects.go` is the door **ADR 0324**/**ADR 0441** built for `len([...])`: every
element that *acts* is lowered into a scratch buffer, and the buffer is committed only if every one of them
lowered — a half-performed container is a program no one wrote. The gate in front of it,
`literalElementAsksToBeRun`, answers the question the pass needs and no other:

```go
case *IntLit, *FloatLit, *StrLit, *BoolLit, *NoneLit, *Name:
    return false        // a spelling performs nothing
…
default:
    return true         // everything else is an operation
```

so for `len([probe()])` the gate said *run the elements*, correctly, before this commit too. The failure was
one call below the gate, in `lowerLiteralElementForEffect`, which had no arm for a call at all and let it
reach the value road — and read that road's **refusal** as the pass's own licence to do nothing:

```go
// …the element is a *Call, and fell here:
default:
    _, err := g.value(b, el)     // the ONE-WORD value road
    return err                   // …which refuses a container answer (Gap R.67)
```

The error travelled back to `lowerLiteralElementsForEffect`, which keeps its promise — *nothing of this
container is committed* — and the door answered the count off the literal anyway. **That is the trap this row
is**: `len([...])` is a number the spelling states, so the door had a right answer sitting in its hand while
the program around the literal went unperformed, and the refusal that should have been a bug report became
permission. **ADR 0457** had taught the *print* road
that a call handing back a container has no one-word answer and must be asked of `containerOperand`'s `*Call`
arm (**ADR 0333**'s pair); the count door never got that lesson because it never believed it was printing
anything — it was only counting. `docs/language.md` had already made the promise for the elements the value
road *could* answer — *"every element that acts is evaluated first, in the order the reference evaluates them,
and its value thrown away"* — and had nothing to say about the one element class it could not.

Measured on the toolchain before this commit, with the live reference beside it:

| program | the compiler | CPython |
|---|---|---|
| `def probe(): print("built"); return [1]` / `print(len([probe()]))` | `1` | `built`⏎`1` |
| `def p(): print("p"); raise ValueError("boom"); return [1]` / `print(len([p()]))` | `1`, **exit 0** | `ValueError: boom`, exit 1 |
| same, wrapped in `try/except ValueError` | `no trap` | `caught ValueError` |
| `def f(x): print("f"); return [x]` / `print(len([f(1)]))` | *(no output, exit 0)* | `f`⏎`1` |
| `def f(x): print("f"); return [x, x]` / `print(len([f(1)]))` | *(no output, exit 0)* | `f`⏎`1` |
| `def h(): return len([probe(), 1])` / `print(h())` | `2`, no line from the callee | `built`⏎`2` |
| `for i in range(2): print(len([probe()]))` | `1`⏎`1` | `built`⏎`1`⏎`built`⏎`1` |

Four exit `0`s and one catchability lie. The raise rows are the ones that decide the row's severity: a count
door that skips a raising element answers, *at the exit code of success*, a program the reference never lets
reach an answer at all — the shape **Gap R.266** and **Gap R.126** were written to catch, in a door that the
`in`-door (**ADR 0324**'s own twin) had already been taught about and this one had not.

The gate asks one question of every element and has **no arm for a call at all**:

```go
case *IntLit, *FloatLit, *StrLit, *BoolLit, *NoneLit, *Name:
    return false        // a spelling — and a name whose read the checker proved was written
// …the four container literals, each asked of its own parts…
default:
    return true         // a call, a subscript, an arithmetic node: something ACTS
```

A call element was therefore already `true` and the pass was already entering the literal. What made the row
look selective lives one level down, in *which road can answer the callee's spelling*, and it was measured on
the parent commit's own binary: a **method** element was performed — `class C:` / `def m(self):` /
`    print("ran")` / `    return [1]` / `c = C()` / `print(len([c.m()]))` wrote `ran` then `1` — where the same
program with a plain `def` wrote only `1`. Both elements reached the same `default: g.value(b, el)`; the
attribute call is answered by the road that reads a method, which emits the call and hands back a word, and
the plain call reached **Gap R.67**'s refusal, which an instrumented pass prints verbatim:

```
codegen: "probe" hands back a container, whose word is a heap object and whose kind only this table knows:
print(...), str(...), a condition, a loop's iterable and a binding ask the door that reads a container answer,
and this position keeps one word for the value and has no such road (roadmap Gap R.67, ADR 0333)
```

— a sentence about a WORD, answered at a position that keeps none. **Gap R.533**'s `print(bool([f()])); while
[f()]: pass` still stands beside this row for a different reason: those two doors fold on the *shape* and never
ask about effects at all, and are not this door.

## Decision

**A literal element whose callee's answer is a container is performed by the ONE door that can perform it, and a position that keeps no word has no reason to refuse a pair — and nothing it hands back needs a root.**

1. `pkg/lang/length_element_effects.go` — `lowerLiteralElementForEffect` grows a call arm *before* the value
   road is asked, guarded by the compiler's own scan and by the road's own answer:

   ```go
   case *Call:
       if nm, isName := n.Fn.(*Name); isName {
           if _, named := g.containerAnswerOf(nm.Value); named {
               var inner strings.Builder                       // the TRIAL's own buffer
               if _, err := g.containerOperand(&inner, n); err == nil {
                   b.WriteString(inner.String())               // commit — and nothing else
                   return nil
               }
           }
       }
       _, err := g.value(b, el)                                // the ordinary road, unchanged
       return err
   ```

   The trial writes to a buffer of its own and is copied into the pass's builder **only** on an answer, so a
   decline leaves not one half-emitted instruction for the road below — the same discipline the file's header
   states as its first rule, applied one level down.
   * **The permission is asked, not inferred.** `containerAnswerOf` (**ADR 0433**'s whole-program scan,
     memoized per module) is the same table **ADR 0457**'s print door consults, fed from the same walk
     (`answer_kind.go`). `TestTheCountDoorAsksTheContainerDoorAndNotItself` pins that the count door keeps no
     table of its own and that the two doors split opaque / forwarding / arms-answering callees identically —
     **ADR 0294**'s one-gate rule, and the reason a callee the scan cannot name keeps the *ordinary* road
     (where it is answered correctly) instead of being pushed into a pair road that cannot describe it.
   * **The handle is dropped, and it is dead where it is dropped.** The position keeps no word: the count
     comes off the literal, so there is no slot for the pair's tag to be wrong about — and no root either.
     `rt_gc` is a worklist over the *root stack* that frees what nothing holds, so an object the frame never
     rooted is the collector's to take on the next collection; the print road does the same with the handle it
     hands to `rt_print_container_value`. This is the half that makes the arm cheap: the count door neither
     stores nor roots the object, and so owes no window to keep it alive (**ADR 0163**, **ADR 0347**), and the
     pair's *destination* never matters — `pairCallPair` writes a slot table, and this position has no slot
     (**ADR 0333**, **ADR 0307**'s slot rule, both cited in the file).

2. **The two buffers are the whole of the no-double-print guarantee, and it is measured twice.** The trial
   lowers into `inner`; the pass's buffer `b` receives it exactly once, and a decline throws the trial away and
   asks the ordinary road instead. The other buffer is `lowerLiteralElementsForEffect`'s, one level up: it
   commits all of a literal's elements or none of them, which is the reason a refusal used to be silent and is
   also the reason the door cannot end up with a call's instructions after a terminator (**ADR 0166**).
   `TestThePerformedAnswerIsPerformedExactlyOnce` asserts the answer twice — the program's stdout, and
   `strings.Count(main, "call i32 @gy_probe(")` in the emitted module — because an element lowered by both the
   trial and the real road prints twice while still answering `2`, and only the module claim sees that.

3. **The pair door is asked about the call, never about the call's pair arguments, and the pair-argument
   shapes are filed instead of forced.** This row built the wider version — `pairCallPair` over the element's
   arguments so `len([f(k())])` would perform `k` — and **measured it**: `llc` exit 2 on

   ```python
   def f(x): print("f"); return [x]
   def g():  return 2.5
   print(len([f(g())]))
   ```

   with `%t10 = fadd double 0.0, 2.5` lowered into an `i32` parameter (`call i32 @gy_f(i32 %t10)`), i.e. the
   same `double`-in-`i32` shape **ADR 0443**/0444 spent their rows on, reached from a door that only wants a
   number. Under **ADR 0166** that exit code is the compiler's own bug, so a row that turns a *silent wrong
   answer* into an *assembler failure* is not a fix; the gate keeps the pair road out of this door and
   `TestWhatTheCountDoorStillSkipsIsFiledNotFixed` pins both shapes with today's bytes and their row IDs
   (**Gap R.542**, **Gap R.543**). The same measurement is why the pair road stays out of this door at all:
   landing an answer's payload in a slot somebody else allocated is what a pair road does, and that is the shape
   behind **Gap R.545**, where two bodies answering one container literal leave an element slot holding the
   object being built and `print([g()])` writes `[` until the run is killed — a count door that borrowed that
   road would have turned a wrong answer into a hang.

4. **The fold's free answers survive, and that is a behavioural claim.** `len([1, 2, 3])`, `len([[1,2],[3,4]])`
   and `len([])` still answer the number from the literal — `TestTheCountDoorOfSpellingsStillCostsNothing`
   and `TestTheCountDoorStillAnswersAContainerOfSpellingsWithoutBuildingIt` assert it on the compiled leg
   *and* against the record, and the module is checked for the absence of a builder. **ADR 0294**'s one gate
   means every consumer of the effects decision follows the gate, so it is the gate that had to be kept
   honest rather than each caller.

## Console

```
$ printf 'def probe():\n    print("built")\n\n    return [1]\n\nprint(len([probe()]))\n' > /tmp/probe.gy
$ gustyc --file /tmp/probe.gy            # was: 1
built
1
$ printf 'def p():\n    print("p")\n\n    raise ValueError("boom")\n\n    return [1]\n\nprint(len([p()]))\n' > /tmp/raise.gy
$ gustyc --file /tmp/raise.gy; echo "exit $?"     # was: `1` at exit 0, nothing raised
p                                                # stdout: the callee's line, performed
ValueError: boom                                 # stderr: the reference's class, through the traceback
exit 3                                           # ADR 0295's exit class for a trap, not 0
$ printf 'def f(x):\n    print("f")\n\n    return [x, x]\n\nprint(len([f(1)]))\n' > /tmp/args.gy
$ gustyc --file /tmp/args.gy              # was: (no output) / exit 0
f
1
$ gustyc --file integration/programs/count_answer_effect.gy   # line-for-line the reference's own bytes
built-list
1
built-list
2
…                      # 21 prints; `python3` on the same file agrees with every one of them
```

## Verification

- `pkg/lang/length_element_effects.go` — the arm, the file's own contract rewritten to name the second door,
  and `literalElementAsksToBeRun` documented as the single permission both doors share.
- `pkg/lang/count_answer_effect_test.go` —
  `TestTheCountDoorPerformsACallThatHandsBackAContainer` (26 sources, each on the compiled leg AND `runGoldenStdout`),
  `TestThePerformedAnswerIsPerformedExactlyOnce` (stdout **and** the module's call count),
  `TestTheCountDoorsContainerAnsweringElementRaises` (5 raise rows: `IndexError: list index out of range`,
  `ZeroDivisionError: integer division or modulo by zero`, `ValueError: boom` with the callee's line before
  it, the first element's accident winning the order),
  `TestTheCountDoorsContainerAnsweringRaisesReachTheProgramsOwnHandler` (**ADR 0295**'s half),
  the answered table also covers **Gap R.533**'s module positions — a count in a `for` body performing its
  callee once per trip, a count bound to a name running at the binding, a count taken inside a function body —
  beside the pre-existing `pkg/lang/length_element_effects_test.go::TestTheCountDoorPerformsTheElementsItCounts`,
  which holds the integer-answering elements to the same counts,
  `TestTheCountDoorAsksTheContainerDoorAndNotItself` (the table is shared),
  `TestTheCountDoorOfSpellingsStillCostsNothing` (six answered spellings, three modules asserted free of any
  builder, allocation or call, and three answered modules asserted to CARRY the callee's call), and the owed table
  `TestWhatTheCountDoorStillSkipsIsFiledNotFixed` — **Gap R.542**, **Gap R.543**, **Gap R.544** (×2),
  **Gap R.545**, each with today's bytes, the reference's, its row ID, and the plain-`len` *control* that
  says which rows are this door's and which belong to the roads around it.
- `pkg/lang/container_answer_element_test.go` — `TestWhatTheContainerAnswerElementStillOwesIsFiledNotFixed`
  grows a *paid* row for `len([probe()])` whose `want` is the reference's answer, so the fix cannot be
  deleted behind the owed table.
- `integration/count_answer_effect_test.go` — the same rows through the CLI against a live `python3`, exit
  codes included in every case: an answered row must exit 0 (`exit %d, want 0: the reference answers this
  program`), a trapping row must exit with the trap's own class, a catchable twin must exit 0 while printing
  `caught ValueError`, and no row of this family may reach **ADR 0166**'s exit 2 — which is asserted as its own
  condition, because a skipped trap is only a bug if the code says success.
- `integration/programs/count_answer_effect.gy` (matrix row `programs/count_answer_effect`, oracle `match`) —
  the answered shapes: a list, a dict, a set and a tuple answer each performed once, a call nested in a literal,
  two calls in one literal, a body the answer scan can prove but not describe, and a body that only forwards
  another call; the four positions that ask the door — bound to a name, tested by an `if`, looped by a `for`,
  and added — the same literal asked beside `print([list_probe()])`, `sorted(...)` and a dict value; and the
  three spelling-only counts as the fold's control. Re-recorded from the reference, and the reason
  `str(len([f()]))` is *not* in the program (the str/concat roads lower it four times — **Gap R.544** — and a corpus row
  would have certified that as if it were this row's answer).
- **32 sources** came in from the reference — 31 through this row's own recorder (`GUSTY_RECORD_COUNT_ANSWER_EFFECT`)
  and the corpus program through `tools/recmerge`, which refuses any entry the compiled leg will not agree to —
  the artifact going **10594 → 10626**; the record's count is the artifact table's and a test recomputes it.

## Alternatives rejected

- **Extend the one-word arm to `len([f()])` by keeping the pair the value road returns.** Rejected: the
  element's value is discarded the moment it is a handle, and a half-pair left in the buffer is how a
  container leaks a payload into a program that never asked for one (**ADR 0307**'s slot rule). The arm keeps
  neither half: it emits the call, and the object the call built is unreachable the moment the call returns.
- **Ask `pairCallPair` about the element's arguments, so `len([f(k())])` performs `k`.** Built and measured,
  and refused by the measurement: `llc` exit 2 (**ADR 0166**) on the float-answering argument, and the
  pair/`f(k())` road that reaches `k()`'s answer is the same road that spins `[[[[…` forever (**Gap
  R.545**). Both are filed with their bytes (**Gap R.542**, **Gap R.543**, **Gap R.545**) instead of being
  reached through a door whose only job is a digit.
- **Widen `literalElementAsksToBeRun`, or make the door commit a half-abandoned literal.** Rejected on both
  halves: the gate already says *run it* for a call — widening a predicate that answers correctly is how a
  fold's theorem gets spent (**ADR 0288**, and `len([1, 2, 3])` would start building a container it does not
  need) — and committing the elements that lowered before the one that refused is the instruction-after-a-
  terminator shape this file's own header exists to avoid (**ADR 0166**). The gate stays one gate (**ADR
  0294**) and the fix went where the failure was: in what the door does **after** the gate (**ADR 0321**).
- **Answer the count in the runtime** (`rt_len_of_literal`) so the elements are unconditionally run. Rejected:
  the count of a literal is a compile-time fact and moving it to the runtime would make the fold's theorem
  the runtime's problem, for a defect the door can close in ten lines.
- **Let the door lower the element first and see whether an error came back.** Rejected per **ADR 0447**: the
  answer is asked *before* anything is emitted, which is what makes the trial buffer cheap to discard and
  what keeps a decline from leaving diagnostics about a program the compiler ended up emitting.
- **Move the raise rows to `integration/` only.** Rejected: the CLI leg cannot see that the *module* skipped
  the raise, only that stdout is short; `pkg/lang` is where the trap class, the message and the handler's
  reach are pinned (ADR 0295's both-legs rule).
