# ADR 0449 — a `for` asks the object for its iterator: `__iter__` once, `__next__` per element, and the `StopIteration` the iterator raises IS the loop's end signal

- **Status**: Accepted
- **Date**: 2026-10-29
- **Cycle**: the one after **ADR 0448** (which closed **Gap R.48**; row **L12.2 / Gap R.54** was the top of the Open queue)
- **Roadmap**: **Gap R.54** (`the iterator protocol`) done on the compiled leg; **L12.2** 🟨 `PARTIAL`. Six shapes
  this row does not own are filed as **Gap R.514** (`__getitem__` sequence iteration), **Gap R.515** (an `__iter__`
  whose hand-off has no static class), **Gap R.516** (no run-time iterator test to raise from), **Gap R.517** (call
  sites disagreeing about one frame parameter), **Gap R.518** (a container a protocol loop fills and the frame hands
  back) and **Gap R.519** (a `__next__` that answers a fractional number). **L11.1** owns the tagged word **R.519** waits on.
- **Builds measured**: parent `b0a7583` (**ADR 0448**) and this commit, every byte from the gate's `-tags=llvm20`.

## Context

The loop had exactly one question for an iterable — *can I count its slots?* — and an object that answered a
different question fell through it. Measured on the parent, the textbook iterator:

```
$ cat count.gy
class Count:
    def __init__(self):
        self.i = 0
    def __iter__(self):
        return self
    def __next__(self):
        self.i = self.i + 1
        if self.i > 3:
            raise StopIteration
        return self.i
for v in Count():
    print(v)
print("after")

$ python3 count.gy                  # 1 2 3 after
$ gustyc --file count.gy            # after            (exit 0)
```

`1`, `2`, `3` were never printed, and the program **succeeded**. That is the answer this ADR exists to forbid: a loop
that runs zero iterations is indistinguishable, to a caller, a CI job or an agent, from a loop over an empty list — so
the failure is invisible from outside and unreportable from inside. The retired interpreter had the mirror-image bug
(7.3 M lines in 15 s — it called `__next__`, received the `StopIteration`, and did not read it as the end of iteration)
and retired with the engine (**ADR 0302**); what stayed was the silent one.

The class road already had everything except the loop's question. `__iter__` and `__next__` compile as ordinary methods
(**ADR 0446**'s resolution order finds them, `self.i` is an instance slot, `raise StopIteration` stores the class code in
`@exn_code`), and `try`/`except` already routes an exception to the innermost handler. What was missing was that the
`for` never asked any of it: an instance handle reached the container road, the road could not count its slots, and the
loop's condition simply came out false.

## Decision

1. **The protocol is asked before the container roads.** `stmtFor` asks `iteratorProtocolOf` first (**`pkg/lang/codegen_for.go`**),
   because a protocol object is not a countable container and the counter road, given an instance, produces exactly the
   parent's answer. The container roads are unchanged and keep answering every loop they answered — asserted as a
   negative control: a `for` over a list emits no `__iter__`, no `__next__`, no `%__gy_iter` slot and no `iter.cond` label.
2. **`__iter__` is asked once, outside the loop, and what it hands back is rooted.** The call is emitted before the condition
   block, the handle is stored in `%__gy_iter<N>` and rooted with `rt_root_put`, because the body is free to allocate and
   trigger a collection mid-loop (**ADR 0181**'s rule: a live object that is not rooted is a live object that is freed).
3. **The element arrives through the ordinary binding road.** Each turn lowers `v = it.__next__()` as the statement
   `v = it.__next__()` would be lowered, with the iterator's class bound in `g.varClasses`, rather than synthesising a
   special loop-variable write. That is what preserves the element's KIND: a `__next__` that returns `"a"` binds a text and
   prints `a`, not the intern table's index (**Gap R.22**'s mechanism, avoided by reusing the road instead of inventing one).
4. **`StopIteration` is the loop's private end signal, tested by its CODE.** The loop pushes a handler whose landing pad is
   the loop's own end, calls `__next__` under it, and on arrival loads `@exn_code` and compares against the reference's own
   `StopIteration` code — not a flag of this file's invention, and not a message comparison. `StopIteration` clears the
   exception and leaves the loop by the end edge (`else:` runs, because a normal end is a normal completion); any other
   code sets the flag and branches to the enclosing handler if there is one, or to `funcRaiseExit`. An enclosing
   `try: … except StopIteration:` therefore never sees it, which is what the reference does and which the suite pins on both
   legs.
5. **A protocol loop's iterator is the ITERATOR's class, and the source has to name it.** `iteratorHandoffClass` accepts
   `return self` and `return SomeClass()` (the named class must answer `__next__`); anything else stops the build:
   a field or a call's answer is **Gap R.515** (the element dispatch would be a run-time fact about an object the loop never
   saw), and a value that is not an object at all is **Gap R.516**, refused with the reference's own sentence inside it —
   `iter() returned non-iterator of type 'int'`. The temptation was to run the loop and let the dispatch fail at run time;
   that is what makes the zero-iteration answer return, so the road refuses instead.
6. **Not being iterable is a RAISE, not a refusal.** An object with neither dunder gets the reference's
   `TypeError: '<Class>' object is not iterable` at the line that wrote `for`, catchable by `except TypeError as e` and fatal
   at exit **3** otherwise. It cannot be a compile-time refusal: the reference's test is about the object in hand, and a
   class that looks plain here may be subclassed where the loop cannot see. `__next__` alone does not make an iterable —
   the reference asks for `__iter__` and does not find it — and a subclass inherits whichever answer its base has
   (**ADR 0446**'s resolution order, one table for both questions).
7. **A loop over a frame parameter is compiled from its call sites — when they agree.** `iteratorCensusOf` walks the program's
   calls (through `programCalls`, the table the rest of codegen already consults, so the census cannot disagree with the
   emitter) and lets `def f(it): for v in it:` run when every site hands `it` an instance of the same protocol class. A site
   that hands it anything else makes the loop **Gap R.517**: choosing one caller's semantics would compile a protocol loop
   for the caller that passes an `int`, which the reference answers with `TypeError`. `seenOther` is tracked separately from
   the bound class so that the census is order-independent, and "mixed" is asked before "bound".
8. **A container that a protocol loop fills and the frame hands back refuses** (**Gap R.518**). The answer table
   (**ADR 0333**) records what a callee's container holds by reading the bindings that wrote its slots; a slot written from a
   loop whose element only the call site knows has no kind to record, and absence was read as "not a container" — the fault
   **ADR 0344** documents, which prints the allocator's slot at the exit code of success. A loop over a class the SOURCE names
   still builds and returns a container unchanged (`programs/iterator_protocol.gy` prints `[1, 2, 3]` from a frame).
9. **A fractional element refuses, because the class road has no float word** (**Gap R.519**). Every method is emitted as an
   `i32` function whatever its body answers, so `return self.i / 4` would arrive as the payload with the float-ness gone and
   print `0` where the reference prints `0.25`. The loop asks the same question the print road asks of a method answer
   (`funcReturnsFloat`) and refuses on the same answer, naming the missing WORD rather than blaming the value. Measured
   beside it and left to its own row: `print(H().m())` for a float-returning method answers `0` today.
10. **Every refusal carries a capability code** (`lower.unsupported.iterator-getitem-sequence`,
    `-opaque-handoff`, `-not-an-iterator`, `-parameter-kinds-disagree`, `-frame-answers-built-container`, `-float-answer`),
    so an agent branches on a code rather than scraping the sentence, and the corpus classifier files each probe under it.

## Consequences

- **A protocol loop either runs or refuses; it cannot go quiet.** The row's DoD is that shape, not "the loop works": the
  six refusals all exit **1** with the missing rule, the program's own construct and the owning row in the sentence, and the
  watchdog case (`TestAHungIteratorLoopIsImpossibleByConstruction`) keeps the third possibility — a loop that never ends —
  out by construction: the only edge out of the body is a `__next__` that raised, and the loop leaves it by testing the code
  the raise stored.
- **The loop grew one rooted slot and no new value representation.** `%__gy_iter<N>` is an alloca in the loop's entry block,
  rooted like every other live handle; the element itself travels the roads that already existed.
- **The protocol road is asked for every `for`, and its answer is remembered per loop** (`iteratorPlan`), so the census, the
  hand-off classification and the refusal sentences are computed once and the emission is a straight read of them — the same
  "ask the whole program before the printer" discipline **ADR 0448** used for `global`, for the same reason: an emission-order
  dependent decision is an exit-2 compiler bug waiting to happen (**ADR 0166**).
- **The corpus moved exactly where this row says it should.** `programs/iterator_protocol.gy` printed only its trailer on the
  parent and now prints the counted-out values byte-identically to CPython; six new probes carry the raise and the refusals.
  Every pre-existing conformance program is byte-identical parent to child — the container roads were not touched.
- **Exit codes are the contract** (**ADR 0006**): the raise exits **3** (`--json` reports the trap class and message), the six
  refusals exit **1** with `phase: "codegen"`, and none of them exits **2** — a refusal that reached `llc` would be this row's bug.
- **What is owed is named, not absorbed.** The six filed rows each carry the reference's answer beside the compiled one, a
  probe in `integration/programs/`, a row in the refusal table, and a capability code; **R.514** (the `__getitem__` road) is the
  one that turns the most silent zero-iteration loops into real loops, and **R.519** shares its fix with the float-returning
  method row beside it.

## Alternatives rejected

- **Letting the counter road keep answering, with the protocol as a fallback after it.** Rejected on measurement: the counter
  road accepts an instance handle and compares it against a count, which is how the parent got exit 0. Ordering the protocol
  first is the only ordering that cannot produce that answer.
- **A special loop-variable write (raw handle in, unbox at print).** Rejected: it loses the element's kind, which is Gap R.22's
  family, and the ordinary assignment road already gets it right.
- **A boolean "stopped" flag maintained by the runtime.** Rejected: it invents a second end signal whose truth the compiler has
  to maintain separately from `@exn_code`, and a loop that ends on the wrong signal is the bug this row closes.
- **Running the loop and raising the iterator test at run time** (the reference's own behaviour for `__iter__` returning a
  non-iterator). Rejected for this cycle rather than ever: it needs a run-time "does this object answer `__next__`" query, and
  the honest placeholder for a shape whose dispatch class the compiler cannot name is a build-time sentence (**Gap R.516** owns
  the query).
- **Choosing one call site's class for a loop over a parameter.** Rejected: it answers one caller and silently misbehaves for
  the other, which is the same defect class as **Gap R.517**'s own example.
- **Answering the fractional element by printing the payload.** Rejected: `0` for `0.25` is a wrong value at exit 0, and the
  golden record is a reviewed artifact, not a place to file a wrong value.

## Where the answer lives

- `pkg/lang/codegen_iterator.go` — `iteratorProtocolOf` / `iteratorHandoffClass` (what the class says about iteration),
  `iteratorReturnValues` / `iteratorLiteralIsSpellable` / `iteratorValueKindWord` / `notIteratorSentence` (what a hand-off is),
  `iteratorCensusOf` / `iteratorCallee` / `iteratorArgumentClass` / `iteratorNameBindings` (who calls a frame with what),
  `stmtForIterator` / `raiseNotIterable` (the emitted loop and the emitted raise), and `iteratorRefusal` /
  `iteratorFrameRefusal` / `iteratorContainerAnswerRefusal` (the sentences).
- `pkg/lang/codegen_for.go` — the protocol is asked before the container roads; `pkg/lang/codegen.go` — the loop's counters
  (`iteratorSeq`, `iterCensus`).
- Answers: `pkg/lang/iterator_protocol_test.go` (17 answer rows on the compiled leg AND the record leg, 3 raise rows, 8 refusal
  rows, the IR-shape row with its container-loop negative control, and the watchdog) and `integration/iterator_protocol_test.go`
  (the CLI family with CPython asked inside each row, a `cc` link-and-run row, the raise's exit code, and the six refusals read
  off `--json` with their capability codes). `integration/programs/iterator_protocol.gy` + six `probe_iterator_*.gy` carry the
  corpus, registered in `conformanceProbes()`. **21 record entries (10261 → 10282)** were written from reference/compiled
  agreements, none refused.
