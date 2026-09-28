# Tagged slots: the tag is written with the payload, at every site

## Context

Roadmap L11.1 gives a container element its own tag. Step 1 (ADR 0184) let a mixed list be
*printed*; step 2 (ADR 0185) let a `for` loop bind its loop variable as a `(value, tag)` pair.
Both left the same shape refused in the LLVM backend, and this cycle went to look at them with
CPython as the oracle (ADR 0186) instead of guessing which refusals were honesty and which were
laziness:

```gusty
xs = [1, "a", None]
print(xs[0])        # refused: "reading one element out needs a tagged value at the use site"
v = xs[1]           # refused
xs.append("b")      # refused: "adding to it needs the new element tagged"
xs[0] = "z"         # ACCEPTED — and answered [1, 'a', None]
print(xs)
```

That last line is the reason this ADR exists. It was not a refusal; it was an **answer** — the
interned index of `"z"` rendered through the slot's stale `int` tag, because `rt_put_elem` wrote
the payload and nothing wrote the tag. Under the two-backend matrix it was invisible: both
backends produce the same module, so parity was 100% while the language was wrong. The CPython
leg found it in one run, and it is the first case in this repo where the oracle disagreed with a
*passing* build rather than with a refusal.

The four sites are one mechanism seen from four angles, so they are decided together.

## Decision

**1. A slot's tag is written by whichever operation writes its payload, never separately.**

- **Literal build** — unchanged from ADR 0184: one `rt_tag_elem` per slot.
- **Append** — a new `rt_append_tagged(h, v, t)`: `rt_append` plus the tag store, in one call.
  Appending and tagging are one operation precisely because as two operations they get
  separated: the tag array is *not* cleared when a list is freed, so an append that forgets the
  tag leaves the new slot reading back with whatever tag the previous tenant of that slot had.
- **Item assignment** — `rt_put_elem` and `rt_tag_elem` are emitted together, inside the same
  bounds-checked block, with the `replaceElemKind` bookkeeping deliberately skipped: for a
  tagged list the per-element tags carry the truth, and the container-wide kind record must not
  be clobbered by one slot.
- **Read** — `mixedElemPair` emits the bounds check, `rt_get_elem` *and* `rt_tag_of`, giving the
  use site a `(value, tag)` pair rather than a bare `i32`.

**2. Two use sites are opened; the rest keep refusing.** `print(xs[i])` passes the pair straight
to `rt_print_mixed_value` with the `str()` quote flag, and `v = xs[i]` binds a **tagged
variable** — the same `(value, tag)` alloca pair a loop variable over a mixed list already got
from ADR 0185, which is why `print(v)` needed no new code and why rebinding `v = 5` retires the
tag through the existing path. Arithmetic, comparison, call arguments, and format specs on a
tagged element still refuse, with a message that now names what *does* work:

```
codegen: a compiled list holds elements of more than one kind, so one element is a (value, tag)
pair; print(xs[i]) and v = xs[i] work because the tag travels with them, but this context needs
a single static kind (roadmap L11.1, ADR 0187)
```

**3. Payload and tag are cross-checked, and a disagreement refuses.** The payload comes from
`heapElemKind` (which interns strings into `@str_tab` and may report "this might be a runtime
string"), the tag from `elemKindTag`. If one says interned-string while the other says number,
the program refuses rather than emitting IR. That check is the difference between "the tag is
metadata" and "the tag is load-bearing": the wrong-answer shape this cycle fixed was exactly a
payload and a tag that disagreed.

**4. A tag we cannot describe is still a refusal, with the *reason*.** Booleans (no runtime
representation yet, ADR 0173), floats (`rt_print_mixed_value` has no float case, so a float
would render as its raw bits — ADR 0168 territory) and nested containers (a nested handle is not
marked by the collector, ADR 0181) all refuse with a message naming the missing capability. The
same is true for reading a dict or set by key, which have no tag array at all (`rt_dict_get`
answers through one static kind).

## Alternatives rejected

- **Keep the whole area refused** — the honest-looking option, and wrong: refusing `print(xs[i])`
  while `xs[0] = "z"` silently produced a wrong answer was worse than either extreme. A refusal
  costs a program; a wrong answer costs the oracle's trust.
- **Invalidate the tag on every write (poison tag) instead of writing it** — turns a wrong answer
  into a crash at print time, and loses the value.
- **Tag every slot at `rt_alloc` time / clear `@heap_tags` on free** — a memset per allocation in
  the hot path to protect against a bug we now make impossible by construction. The pairing rule
  makes a stale tag unreachable from user code; the GC stress test keeps it honest.
- **Widen `@heap` elements to `{payload, tag}` so one store writes both** — the cleanest data
  model, and a change to every container algorithm and to `@heap`'s layout in one step. Deferred
  (roadmap L11.1 step 5), not skipped.
- **Give tagged values their own boxed kind** (ADR 0168's "tagged value" row) — needed when a
  tagged element must flow into arithmetic and calls; that is L11.2's job, and doing it first
  would mean designing a value type for uses we have not opened yet.

## Consequences

- `@heap_tags` gains two more writers (`rt_append_tagged`, and `rt_tag_elem` from item
  assignment) and one reader path outside the printer (`rt_tag_of` at read sites). Any future
  container operation that writes a slot must write its tag — that is now a review rule, and the
  GC/collection tests are what catch a violation as a wrong render rather than a crash.
- The corpus grows by two three-way programs (`mixed_element_reads.gy`,
  `mixed_element_writes.gy`): 61 rows, 45 parity, 0 fail, oracle 29 match / 22 debt / 10 NA,
  0 drift. The oracle's first catch is on the record.
- Two tests written in an earlier cycle asserted `print(xs[i])` *must* refuse. They were
  assertions about yesterday's limitation, and were inverted into "this compiles and prints the
  right thing". Every new refusal test in this cycle also asserts the refusal's **text**, so a
  future cycle that opens one of these sites has to notice the message went stale.
- Remaining L11.1 work is named in the roadmap: float/bool/nested-container elements, dict/set
  element tags, tagged elements through calls and arithmetic (with L11.2), and `@heap` layout
  carrying tag-and-payload together.
