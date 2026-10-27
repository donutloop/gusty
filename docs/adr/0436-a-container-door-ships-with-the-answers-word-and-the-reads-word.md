# ADR 0436 — a container door ships with the answer's word and the read's word: a heap-parameter door is parked rather than landed, because a half door turns an `llc` rejection into a slot number at exit 0

## Status

Accepted, 2026-10-27. Files two rows from a sweep that measured the ONE shape a received container still
cannot cross at the module door — **Gap R.483** (a `def` that answers the container it RECEIVED answers the
slot's first word) — and the read half of the field story, **Gap R.484** (an instance field a body fills with
a container has no kind word at the read). Records the decision NOT to merge a heap-container
method-parameter door that exists in the tree as uncommitted work (`git stash` list, entry
`wip(Gap R.480/483/484): … PARKED UNVERIFIED …`), and the rule that has to be satisfied before it may land.

Read after **ADR 0435** (`0435-a-parameter-a-method-call-omits-arrives-with-the-kind-its-default-states.md`),
which filed the door as **Gap R.480**, and **ADR 0434**/**ADR 0366** (the parameter vector and the census).

## Context

A module `def` whose parameter holds a container already behaves, at this commit, for everything the source
asks OF the container. Measured on `170e09e` with a fresh build of `cmd/gustyc --file`, against a live
`python3` beside each program:

| program (module `def`, container argument) | this backend | reference |
|---|---|---|
| `def f(xs): print(xs)` called `f([1, 2, 3])` | `[1, 2, 3]` | `[1, 2, 3]` |
| `def f(xs): return xs[1]` called `f([10, 20, 30])` | `20` | `20` |
| `def f(d): return len(d)` called `f({"a": 1, "b": 2})` | `2` | `2` |
| `def f(d): return d["a"]` called `f({"a": 1, "b": 2})` | `1` | `1` |
| `def f(xs): return xs` called `print(f([1, 2, 3]))` | **`0`** at exit **0** | `[1, 2, 3]` |
| `def build(): xs = [1, 2]` / `return xs`, printed | `[1, 2]` | `[1, 2]` |

The last two lines are the finding. A container the body BUILDS and answers already travels — the record
holds `def build():` / `    xs = [1, 2]` / `    return xs` with the bytes `[1, 2]`. A container the body
RECEIVES and answers does not: the identity function over a container prints `0`, the slot's first word, at
the exit code of success. It is not in `pkg/lang/testdata/interpreter-golden.json` (no entry for that
source), it is not in `interpreter-golden-drift.json`, and `tools/recmerge` refuses to record it — the
compiled leg's bytes are not the reference's — which is the cross-check doing its job and the reason the
shape needs a row rather than a record. **Gap R.483.**

The method side of the same family was being built in the tree when this swept ran: `declareMethodHeapParams`
beside `emitClassMethod`, a `heapMethodArgs` table beside `heapArgs`, `heapMethodArgKinds` beside
`heapArgKinds` — the door **Gap R.480** asks for. Asked of that build, `len`, `max`, `min`, `sum`, `all`,
`any`, `sorted`, `reversed`, `for x in xs`, `xs[i] = v`, `xs.append(v)`, `xs + ys`, `xs == ys`, `xs[i:j]`, a
dict's `ks[k] = v`, a set's `s.add(v)`, defaults, keywords, inherited methods and forwarding through a
method's container parameter all answered the reference's bytes. And asked of the same build, six further
programs answered numbers where the reference prints a container:

| program, at that build | that build | reference | at this commit (`170e09e`) |
|---|---|---|---|
| `def f(self, xs): return xs`, printed | `0` | `[1, 2, 3]` | exit **2** (`@.lst1` operand — **Gap R.474**) |
| `def f(self, xs): yield xs`, walked | `0` | `1 2 3` per group | exit **2** |
| `def f(self, xs, ys): return xs, ys`, printed | `0 2 6 a` | `[1, 2] ('a',)` | exit **2** |
| `def f(self, xs): return xs[1]` | `0` | `20` | exit **1** refusal |
| `def show(self): print(self.xs)` after `self.xs = [10, 20]` | `1` | `[10, 20]` | exit **2** |
| `for v in c.xs` / `1 in c.xs` / `c.xs * 2` after `self.xs = [10, 20]` | `0`/`1`, `True`, `2` | `10`/`20`, `False`, `[10, 20, 10, 20]` | exit **2** |

That column is the whole argument. At this commit those programs are *refusals or `llc` rejections* — loud,
attributable, and owned by **Gap R.474** / **Gap R.480**. The door removes the rejection and leaves the value
unanswered, so the same six sources become **`0` at exit 0**: a heap slot's first word, presented as the
answer, at the exit code that says the program was fine. **ADR 0166** is the file that says what that class
of defect costs, and **ADR 0232** the one that says a value needs ONE word for its kind at every door that
reads it.

## Decision

**A container door ships with two words, or it does not ship.**

1. **The answer's word.** A received container that reaches a `return`, a `yield`, a tuple element, or any
   other answer position must travel as the container. Until it does, the position refuses in words — the
   same sentence the module door has used for a container it cannot describe since **Gap R.35**'s container
   half. A door may not open `len` and `for` while leaving `return` to answer the slot.
2. **The read's word.** An instance field that some body fills with a list, dict or set must carry the kind
   the store wrote, so the read — print, `for`, `in`, `*`, comparison, an index — answers the container.
   Until it does, the READ refuses (not the store: storing is legal and useful, and the body that stored it
   can still measure its own parameter). This is **Gap R.484**, and it is the second half of **Gap R.474**:
   whoever lands the store lands the read's word with it, or lands this ADR's table.

Both words are asked from the ONE census **ADR 0232** already insists on — `heapArgKinds` for a module `def`'s
parameters, its method half for a method's, and the field census read off the same call-site evidence — so a
road never re-infers a kind locally.

Accordingly the door in the tree is **parked, not landed**: `git stash push -u` under
`wip(Gap R.480/483/484): …`, recoverable with `git stash apply`, with **Gap R.480**'s row left `OPEN` and
these two rows filed against it. Its tests, its conformance program and its record merge are parked with it
rather than kept, because a test that pins an answer the backend does not produce is a lie in the suite.

## Consequences

- **No behaviour changes in this commit.** The five refusals and two `llc` rejections above stay exactly as
  loud as they are today. That is the point: nothing regressed, and two facts the tracker did not have are
  now in it.
- **The module container answer is now the cheapest open door on the container family.** Everything the
  module `def` needs around a received container works (`print`, an index, `len`, a dict key); only the
  answer position is missing, and its neighbour — the container the body builds — already ships and is on
  the record. **Gap R.483** is where to spend the next cycle on this family.
- **A guard against the pattern, not just the bug.** When **Gap R.474** or **Gap R.480** next lands, the
  reviewer's question is this ADR's table: for every shape the door un-crashes, what does it now PRINT? A
  rejection that becomes a number is a regression even when it arrives with a green suite, because the suite
  had no case for the shape that used to crash.
- **The parked work is not wasted.** The inference (`heapMethodArgKinds`), the rooting door
  (`declareMethodHeapParams`), the argument binder that materialises a literal into the heap before it
  crosses a slot, the field census, and ~40 in-process and CLI cases that already assert the reference's
  bytes for the measured family, are all in the stash and carry forward into **Gap R.480**'s cycle.

## Alternatives rejected

- **Land the door anyway, pin the wrong answers as owed rows.** Rejected: the repo's own rule is that a
  wrong answer at the exit code of success is what the front end must never produce, and the suite would
  then contain cases asserting `0` where the reference prints `[1, 2, 3]` as ANSWERS in one file and owed in
  another — the two readings **ADR 0232** forbids.
- **Land the door with a blanket refusal for every use of a container parameter.** Rejected as worse than the
  defect: it would refuse `len(xs)`, `for x in xs` and `xs[i] = v`, which the door answers correctly today,
  to avoid a hole in `return xs`. A door that opens four shapes and closes three is not a door.
- **Land the door with a per-position use-gate (this session's first cut).** Kept in the stash, not merged:
  it works by marking the expression nodes the container roads read and refusing the unmarked remainder,
  which is sound in principle but is only as good as the completeness of its marking, and a missed mark is a
  spurious refusal on code that works today. It needs the real road inventory — `value`, `stmtFor`, the
  builtin roads, the store and method-call roads — read from the files, not from recall.
- **File only one row, and let the field half stay inside **Gap R.474**'s.** Rejected: R.474's row is a STORE
  row and will be closed by a store fix; the read is a separate door with a separate owner, and the parked
  build is evidence that a store fix alone changes the failure's shape from loud to silent.

## Note on this cycle's method (tool-trust)

Several `grep`/`read` results this session returned text that was not in the file they named — including a
whole function body (`containerAt`, `listLengthOperands`, `codegen_builtin_len.go`) that does not exist in
this repository at any commit. Code written against recalled text accordingly failed to compile
(`procKind`, `currentFunc`, `classMethodDecls` are not fields of `irGen`; the dispatcher is `value`, the
parameter tables are `map[string]map[int]int` keyed by position, and the method lookup is
`paramHeapKindOfMethod`). The rule this cycle will keep: **a plan is only as good as a `git diff` and a
`go build` after each edit** — every claimed file content gets verified before code is written against it,
and the sweep's binaries are rebuilt from the tree they claim to describe.
