# A printed container is a container: printers, not handles

## Context

The most ordinary Python program in the corpus did not compile:

```gusty
print([1, 2])
```

Not "printed the wrong thing" — `llc-20` refused the module:

```
error: global variable reference must have pointer type
  %t1 = call i32 (i8*, ...) @printf(i8* getelementptr(... @.fmt1 ...), i32 @.lst1)
```

Three shapes lived behind that one line, and each was found by running CPython against it
(ADR 0186) rather than by reading the emitter:

| program | interpreter | compiled | CPython |
|---|---|---|---|
| `print([1, 2])` | `[1, 2]` | **module llc refuses** | `[1, 2]` |
| `print([])`, `print({})` | `[]`, `{}` | **module llc refuses** | `[]`, `{}` |
| `print(set())`, `print(list())`, `print(dict())` | `set()`, `[]`, `{}` | **`0`** | `set()`, `[]`, `{}` |
| `print(["a"]) … print({1, 2})` | `['a']`, `{1, 2}` | `['a']`, **`{(null), (null)}`** | `['a']`, `{1, 2}` |
| `print([["a"], ["b"]])` | `[['a'], ['b']]` | **`[1, 2]`** | `[['a'], ['b']]` |

The last row is the one that hurts: it compiles, verifies, runs, and exits 0, printing the
*interned indices* of the inner strings as if they were the numbers 1 and 2.

The cause was a single misplaced question. The print lowering asked
`literalNeedsHeap(a)` — "does this literal contain a string, so does it need a heap object?" —
and everything answering "no" fell through to the generic value path, whose representation for an
all-int or empty literal is a **static global struct** (`@.lstN = private global {i32, [N x i32]}`).
That is a fine representation for a list that gets indexed, and nonsense as an argument to
`printf("%d")`. Constructors (`set()`) never reached the print path at all because the gate asked
for a *literal*, and the empty set has no literal spelling (Gap K.3). Behind them all sat a third
problem: heap slots are recycled, and `@estr[h]` — the container-wide "my elements are interned
text" flag the printers dispatch on — was never cleared at allocation, so a numeric container that
landed on a freed string container's slot printed its numbers through the string table.

## Decision

**1. Print position asks a rendering question, not a storage question.** Any container literal
goes to the runtime printers: build the heap object and call `rt_print_list` / `rt_set_print` /
`rt_dict_print` with the `str()` quote flag. The static global layout stays what a container
*variable* uses when nothing needs to render it — the gate that decides is
`isContainerLiteral`, which is about the use, not about the elements.

**2. The constructors are literals as far as print is concerned.** `emptyContainerLiteral` maps a
zero-argument `set()` / `list()` / `dict()` to the corresponding empty literal for the print path
only. (Their *value* lowering is unchanged: `rt_alloc`, deliberately — folding to a global was the
bug ADR 0163 recorded.)

**3. An attribute of a container is cleared where the container is born.** `rt_alloc` already
writes `kind` and zeroes `len` on both the fresh and the recycled path; `@estr[h] = 0` joins them.
The asymmetry with ADR 0187 is deliberate and worth stating: the per-element **tag array** is 256
i32s and is *not* cleared — there the failure mode is made impossible by writing tags together with
payloads, and a memset in `rt_alloc` would cost the hot path. The **estr flag** is one i32 per
container, so clearing it is free, and no amount of careful writing restores it after recycling.
General rule for this runtime: *clear it at alloc if clearing is cheap; pair it at write time if
clearing is expensive.*

**4. A container inside a container refuses, with the collector's reason.** `heapElemKind` — the
one path every container build goes through — rejects an element that is itself a container
(literal-in-literal, or a container variable in another container). The reason is not cosmetic:
the collector marks containers reachable from a *variable slot* (ADR 0181), and an element that is
a handle has no slot to be marked from. Before this, that shape either emitted a module llc refused
or printed interned indices, and both are worse than a refusal that names the missing capability.

**5. The module-wide invariant is now a test.** `TestNoContainerGlobalInAValuePosition` fails if
any compiled program puts `i32 @.lstN` / `@.dictN` / `@.setN` / `@.strN` in a value position — the
family the roadmap listed as Gap J.6's closing condition (literal to a call, literal returned,
literal appended, literal printed have each been a wrong answer or a refused module at some point).
Stub checks confirmed the guards bite: deleting the two `@estr` stores makes
`empty_containers.gy` print `{(null), (null)}` again.

## Alternatives rejected

- **Render static literals at compile time into a text global** — the emitter would have to
  reproduce CPython's `repr` for every element kind (quoted strings, `None`, float shortest
  round-trip, nested containers) in Go, duplicating `rt_print_list` and diverging from it forever.
  One printing path, in the runtime, is what kept ADR 0184/0185/0187 honest; this keeps that.
- **`rt_alloc` clears `@heap_tags` too** — a 256-i32 memset per allocation to protect a failure
  mode ADR 0187 already made unreachable by construction.
- **Keep `set()` printing `0` and pin it as debt** — the ledger exists for real gaps; `set()` was
  not a gap in understanding, it was a gate that never asked about constructors. Paid-debt
  promotions (this cycle: `probe_empty_set` → `empty_set.gy`) are what the harness's
  both-directions drift check is for.
- **Allow nested containers by storing the inner handle** — the inner container would be collected
  while still referenced (ADR 0181's exact failure), so the "support" would be a memory bug with a
  passing test. Refuse until elements can carry the reference properly (L11.1).

## Consequences

- `print` is now total over containers on the AOT path: literal or constructor, empty or not,
  int-only or mixed, `str()` at top level and `sep=`/nested `repr()` unchanged.
- Corpus: 62 rows, 47 parity, 0 fail; oracle 31 `match` / 21 `debt` / 10 `not_applicable`, 0 drift.
  `probe_empty_set` is a **paid debt** — promoted out of the ledger into `empty_set.gy`; two new
  parity programs (`empty_containers.gy`, `empty_set.gy`), one of which is the stub-proven guard
  for the stale-`@estr` bug.
- `@estr[h]` is now an object-lifetime flag, which is the step toward retiring it entirely
  (L11.1 (1d)): once every read path consults tags, the flag has no readers and can go.
- Nested containers are the largest remaining honest gap in this area (`[[1], "a"]`,
  `[["a"], ["b"]]`, `xs.append(other_list)`). It is recorded as refusal-with-reason, and its
  unlock is per-element tagging for container elements plus marking them in the collector —
  L11.1 with ADR 0181, not a print-path patch.
