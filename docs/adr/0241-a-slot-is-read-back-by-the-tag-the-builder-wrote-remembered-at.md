# ADR 0241: a slot is read back by the tag the builder wrote, remembered at compile time

Status: accepted. Roadmap L11.1 (tagged value word), third step; follows ADR 0238
(`feat(codegen): a float in a container slot is the handle of a float box`) and ADR 0239
(`feat(codegen): a container element is the handle of the inner object`).

## The measured starting point

ADR 0239 made `[[1, 2], [3]]` build, print, compare and grow on both backends. What it did not
deliver was the *use* of a read — and the two backends disagreed out loud about it:

| program | interpreter | compiled (before) | CPython |
|---|---|---|---|
| `xs = [[1,2],[3,4]]; print(len(xs[0]))` | `2` | refused | `2` |
| `xs = [[1,2],[3,4]]; print(xs[0][1])` | `2` | refused | `2` |
| `d = {"a":[1,2]}; print(d["a"][1])` | `2` | refused | `2` |
| `m = {0:[1,2],1:3}; print(m[0][1])` | `2` | refused | `2` |
| `t = [[[1]]]; print(t[0][0][0])` | `1` | refused | `1` |
| `xs = [[1,2]]; print(xs[0] == [1,2])` | `True` | refused | `True` |
| `xs = [[1,2]]; 2 in xs[0]` | `True` | refused | `True` |
| `xs = [[1,2]]; for v in xs[0]: print(v)` | `1`,`2` | refused | `1`,`2` |

Every refusal was the same sentence — “`len`/`index` requires an inline list/dict/set literal” — which
pointed at a shape in the source that was perfectly fine.

## The decision

**A slot read is granted by the tag the builder wrote, remembered at compile time.** ADR 0233 fixed
the rule for *writing* a slot (“a slot is only writeable when the compiler can name the kind”); the
reading rule is its mirror. The permission is a fact about the program, collected once by
`containerLiteralsOf` in `pkg/lang/container_env.go`:

- a name qualifies when it is **bound exactly once** to a container literal, and
- it stops qualifying the moment anything could have changed that object: a rebind or augmented
  assignment, an item assignment (`xs[0] = …`), a call to a mutating method (`append`, `sort`, `add`,
  `update`, `pop`, `discard`, …), or the container being handed as an argument to a callee this pass
  cannot see.

With that promise in hand, `containerHandleOf` answers the *handle* of the object an expression
denotes — a literal, a container variable, a comprehension, or a slot whose tag says
`TagList`/`TagDict`/`TagSet` — and every consumer takes the same handle from the same question:
`len`, a further subscript, equality, membership, iteration in `for`, and print position. One source of
truth, so that a use cannot be “half supported”: the shape that let `[1, [2]]` print `[1, 1]` (ADR 0239)
was two builders disagreeing about one question.

**Where the promise runs out, the answer is a refusal that names the promise.** Three messages, chosen
by which question actually failed:

- `len cannot reach into xs's slots: the name was rebound, mutated, or handed to code this pass
  cannot see …` — the object is no longer describable;
- `index cannot use an element of xs as a plain number: the payload only means something with its
  tag …` — the read is licensed, the context wanted a bare `i32` (this is the remaining numeric half
  of L11.1, and it stays open on the roadmap);
- `len reaches past a int in xs: the slot holds a int, not a container …` — the program is subscripting
  a number, which CPython answers with `TypeError: 'int' object is not subscriptable`.

## What this refuses to do

A `rt_container_slot` runtime helper was written, tested, and **removed**. It would have taken a slot’s
payload and asked `@heap` whether the object is a container — which is a lie in the common case: the
payload of an `int` slot is a number, and `@heap` has 1024 slots, so some object lives at that index and
would be printed. `[[5]]` for `[[1,2]]`, or the interned text at `@str_tab[5]`, is exactly the “wrong
answer that looks right” class this language has spent four cycles deciding it does not ship (ADR 0233,
ADR 0238, Gap R.65/R.66). The static promise costs refusals; the runtime guess costs trust.

## Codegen/IR consequences

- `pkg/lang/container_env.go` is new: the conservative name→literal map, keyed on bindings, item
  assignments, mutating methods and unknown callees. It is a compile-time pass over the AST, so no
  module is affected when nothing qualifies, and the map is `nil` rather than empty when no name does.
- `containerHandleOf`, `taggedContainerRead`, `containerSlotRead`, `staticSlotTag`, `staticSlotTagOf`
  and `bindTaggedVar` in `pkg/lang/heapargs.go` are the single door for a slot read. `containerSlotRead`
  delegates the index/key check to the checked helpers (`normalizeIndex` + `rt_get_elem`,
  `checkKeyReadTagged` + `rt_dict_get_tagged`) so a slot read traps with the same `IndexError`/`KeyError`
  as any other read.
- `taggedContainerRead` deliberately handles only the case where the subscripted object is *itself* a
  read (`xs[0][1]`, `d["a"][0]`, `t[0][0][0]`). A container variable or literal already has its own
  checked read path; taking those too would leave two answers to one question.
- `bindTaggedVar` is now the one body that binds a `(payload, tag)` pair to a variable; the dict-value
  binding, the list-element binding and the new slot-of-slot binding all call it, so the three cannot
  drift apart the way the two builders did in ADR 0239.
- `for v in <container expression>` gained a leg: when the iterable is a provable container the loop
  walks the object (`rt_list_len`/`rt_set_len`/`rt_dict_len`) with the tag travelling per iteration,
  instead of falling through to the counter/range path. The fall-through was not merely wrong (it bound
  the loop variable to the counter, printing `0, 1` where CPython prints `1, 2`) — for a literal it also
  emitted `icmp slt i32 %_ctr1, @.set1`, a global in an `i32` slot, which `llc` refuses and the
  exit-code contract charges the compiler with (ADR 0166).
- Print position keeps its self-dispatch: `rt_print_mixed_value` on the tag routes a container element
  to `rt_print_container_value`, which asks the *inner object* (`@heap` kind, then `@estr` bits) rather
  than trusting a static claim (ADR 0239’s rule, one level down).

## Coverage

- `pkg/lang/container_slot_read_test.go` — 21 answers × both engines, the refusal family (four shapes,
  each with its own message), and the “mutation costs you the promise, but not the answers that never
  asked” half: after `xs.append([9])`, `print(xs)`, `xs == [[1,2],[9]]`, `[9] in xs` and `len(xs)` stay
  correct.
- `integration/container_slot_read_test.go` — the same answers through the CLI against the CPython
  oracle with the exit-2 guard, plus a trap table (`xs[0][0][0]`, `xs[0][9]`, `len(xs[0][0])`) where the
  oracle dies and neither engine may exit 0.
- `integration/container_element_test.go` — `TestNestedShapesThatStillRefuse` lost its two element-read
  rows and gained the mutated-container and plain-number rows, so the record of what is owed moves with
  the work rather than rotting.
- `probe_heterogeneous` was promoted from the oracle ledger to `conformanceStandalone()` — both backends
  print CPython’s answer on every line. `probe_nested_list` stays in the ledger, narrowed to the shape
  that genuinely still refuses: a container built by `append`, which no literal ever described.

## Consequences still open

1. **Numeric use of an element** (`xs[0] + 1`, `xs[0][0] + 1`, `max(xs[0])`) — the read is licensed; the
   context wants one `i32`. This is the tagged value word in its arithmetic form, still L11.1’s open
   clause, and the refusals say so by name.
2. **Containers built at run time** (`xs = []; xs.append([7,8]); xs[0][0]`) — no literal, no promise.
   Either the tag becomes a runtime question answered from `@heap_tags` by a helper that also proves the
   payload is a handle, or the language documents the limit.
3. **Where the interpreter is ahead.** Every row above is answered by the interpreter today, so each
   refusal is a measured gap and not a mystery; the ledger rows are the place to look.
