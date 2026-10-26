# A comparison chain asks each of its operands what it CARRIES, and binds its one evaluation slot with that

## Status

Accepted — 2026-10-26. Closes **roadmap Gap R.442** (filed by **ADR 0422** the day before), and pays the two
positions that row pinned as refusals:

```
xs = [1, 2]
xs[0] = 2.5
print(0 < xs[0] < 3)      #  True    reference
                          #  "this context needs a single static kind"   this backend, in words
d = {"a": 2.5}
print(0 < d["a"] < 3)     #  True    reference
                          #  False   this backend, at the exit code of SUCCESS
print(0 < abs(-2.5) < 3)  #  True    reference
                          #  llc: '%t1' defined with type 'double' but expected 'i32'
                          #  → exit 2, the contract's "the compiler is broken" code (ADR 0166)
```

One commit, one feature: one new file (`pkg/lang/chain_operand.go`), one arm in `chainValue`
(`pkg/lang/codegen_arith.go`), the `+` half of the arithmetic gate in `pkg/lang/heapargs.go`, the
concatenation sentence in `pkg/lang/codegen_ir_numeric.go`, one new test file, and the pins that had to move.

## Context

`chainValue` lowers a comparison chain (`a < b < c`, `a == b != c`, `x in y is z`) by storing each operand
**once** into a fresh local and letting every link read the slot back. That store is the reason "the middle
operand is evaluated exactly once" is a property of the emitted module rather than a hope (**Gap R.53**,
**ADR 0288**):

```llvm
  %_chain1 = alloca i32
  store i32 %w, i32* %_chain1
```

One word. **ADR 0331**'s finding — a value in this language is a `(payload, tag)` pair, and a *position* that
keeps one word loses the tag — was applied to the binding, the print, the loop and the argument position by
**Gap R.146**/**Gap R.82**. The chain was not on that list, and it is a value position like them. Three
classes of operand do not fit four bytes, and the chain found out by lowering them through the ordinary road:

| operand | what the ordinary road did | what the reference does |
|---|---|---|
| a slot whose kind the **object** carries (`xs[0]` of a list a store promoted — **ADR 0422**) | refused in words: *"this context needs a single static kind"* | answers |
| a **dict entry** (`d["a"]`) | handed back the **payload alone**: a float entry's `@float_box` handle, a text entry's `@str_tab` index, the void's `0` — each a number for reasons that have nothing to do with the value | answers, or raises `TypeError` |
| a **double** (`a + b`, `abs(-2.5)`, a float-returning `def`) | emitted `store i32 %t1, i32* %_chain1` over a `double` → `llc` rejects the module → **exit 2**; or truncated it → `False` at exit 0 | answers |

The dict row is the one that should have been impossible: `d = {"a": 2.5}` / `print(0 < d["a"] < 3)` compared
the **box handle** against 0 and 3 and printed `True`/`False` by the luck of the allocator, and
`d = {"a": "x"}` / `print(0 < d["a"] < 3)` compared a **string-table index** and printed `True` where CPython
raises `TypeError: '<' not supported between instances of 'int' and 'str'`. Both at the exit code of success.

Two more refusals fell out of the same sweep and are part of this row because the same two doors own them:

- **`+` beside a spelled number.** `taggedArithPair`'s gate (**ADR 0265**) required *both* operands to be
  slots proven numeric, so `s + 1`, `xs[0] + 1`, `f() + 1` where the notebook has no number record for the
  name were refused **at compile time** — `print(x + 1)` for a list-valued `x` was a compiler error where
  CPython raises `TypeError: can only concatenate list (not "int") to list`.
- **the sentence the trap wore.** `@rt_num_arith` wrote CPython's generic
  `unsupported operand type(s) for +: 'str' and 'int'` for `text + number`. CPython does not write that
  sentence for those three kinds — they have a concatenation method, and it complains in its own words:
  `can only concatenate str (not "int") to str`.

## Decision

### 1 — One question, asked before anything is lowered

`chainOperandCarriesMoreThanAWord(e)` (`chain_operand.go`) is a **pure gate**: it reads only the records the
position already keeps (`numericPairVar`, `taggedVars`, and `isFloat`) and emits nothing. Asked false, the
operand keeps the ordinary `store i32` arm byte for byte — `print(0 < 2 < 3)` and `print(0 < x < 3)` emit the
module they always emitted. That is why the gate is asked first and must be pure: a door that lowers into the
module and *then* changes its mind has evaluated the operand twice, which is the exact bug **ADR 0288**
exists to prevent.

### 2 — The doors are the ones the neighbours already ask, in the neighbours' order

Asked true, the operand goes to the same three doors a plain assignment `v = <the same expression>` would, in
the same order (`assign_pair.go`), and no fourth door is written here:

1. **`comparePair`** — the comparison road's *own* pair door, first because `print(xs[0] < 3)` already
   answers with it: the chain form of a program whose single-comparison form works must not need a different
   door to say the same thing (**ADR 0232**: one question, asked once).
2. **`assignPairWords`** — the binding road's ordered list (the answer of a pair-returning call, the
   arithmetic over a slot, the fold's winner, a slot of a container the program built).
3. **the float road** — the last net, for the operand that is a `double` and which both pair doors declined.
   It gives the chain's slot the word the value is (`alloca double`, `floatVars`, `doubleSlot`) so every link
   reads a float *name* and asks the float road it already answers.

Each arm builds into a `strings.Builder` scratch and is committed to the module only after it has answered.
Every door is allowed to decline, and declining is the common case. What the chain stops doing is throwing
the second word away; no comparison road is widened, duplicated, or written twice. The result is that a
pair-bound chain slot is interrogated by the roads that already interrogate a pair-bound *local*
(`mixedTaggedCompare`, `taggedOrderApplies`, `pairOrder`), and a float chain slot by the float arms.

The double door is currently **unreachable from the surface** — doors 1 and 2 answer for every float the
language can write, which is the right outcome and the wrong excuse. It stays because it is the net between
"float" and "`store i32` over a double", and it is pinned directly:
`TestTheChainsDoubleDoorBindsTheWordTheValueIs` asks the door itself, as `TestTheStoreGateAndThePromotionDoorAskOnePredicate`
does for the store's doors.

### 3 — `+` beside a number the SOURCE spells needs no proof of what the slot holds

`plusBesideASpelledNumber` widens **ADR 0265**'s gate at exactly **one operand shape**: one side is an
`IntLit`/`FloatLit`/`BoolLit` (or a `-` of one), the other is the slot read that needs the pair. The reason
the guard exists at all is two shapes — `"a" + "b"` and `[1] * 2` — that the reference answers with a joined
text and a repeated list this backend does not build (**Gap R.82**). Neither is reachable with an integer
literal on the other side of `+`: whatever the slot holds, the operator answers a number or raises. So the
door opens, the answer's kind is derived from the two **tags**, not asserted from one, and `rt_num_arith`
raises with the slot's real kind named.

Two bounds keep the widening from becoming a licence:

- **`*` keeps its proof.** `xs[0] * 2` over a slot holding a list *is* answered by the reference, with a
  list — the shape the guard was written for, unchanged.
- **an object with no number half stays behind the door.** `pairRoadAnswersThisOperand` refuses a class, an
  instance or a function: their int half is a heap pointer, and adding that pointer to a literal and calling
  the sum an answer is worse than the refusal. Measured: `class C: pass` / `print(C() + 1)` answering `1`.
  That answer is the parent's, not this row's (it predates it at `1d343fc`) — but a widening that let it
  through would have made this row the one that owned it.

### 4 — One trap, one wording

`@rt_num_arith` grew a concatenation arm: `+`, left tag ∈ {str, list, tuple} → `@rt.num.catfmt`,
`TypeError: can only concatenate %s (not "%s") to %s`. Only the **left** tag is tested, because that is what
CPython does: `1 + [1]` has no list on the left and really does write the generic sentence
(`unsupported operand type(s) for +: 'int' and 'list'`). dict, set, the void and an instance have no
concatenation method for a number and keep the generic sentence — which is why the arm tests three tags and
not "anything that is not a number".

## Evidence

Measured against the row's parent (`1d343fc`, built in a `git worktree` — the tree was never stashed under
this row, because a second process may be building it at any moment).

- A **25-program chain sweep** (slot/dict/call/append/nested/loop/param/runtime-index × the three operand
  classes, plus the control shapes) through both builds and `python3`:
  **10 improved, 0 regressed, 13 already correct, 2 still wrong.** Of the 10: **7 were refusals in words**
  (the two the row was filed for, plus the append-promoted slot, the runtime-index slot, `1 <= xs[0] <= 2`,
  the `None` slot, the reversed spelling) and **3 were wrong answers at the exit code of success**
  (`d["a"]` float → `False`, `d["a"]` text → `True`, `0 < a + b < 4` → `False`). Two more of the 10 arrived
  as **exit 2** on the parent (`print(0 < abs(-2.5) < 3)`, `print(0 < g() < 3)`) — the contract's
  "the compiler is broken" code, spent on the compiler's own module, is now a printed `True`.
- **`pkg/lang/chain_operand_test.go`**, new: **19** answering rows judged on the compiled leg *and* the
  record leg, **3** trap rows (exit 3, the reference's sentence, and the record's own trap beside them),
  **5** IR rows asserting a `_chainN` slot with its `_chainN_tag` partner + **2** control rows asserting the
  cheap operands bind nothing at all, **5** gate-purity rows (the same source through `GenerateIR` twice must
  give the same module — purity is what lets the chain ask before lowering), **1** white-box row asking the
  double door itself, **2** pins for what the row leaves owed.
- **27** record entries written by `tools/recmerge` from the reference with the compiled leg cross-checked
  (**9394 → 9421**), **0 refused**.
- Pins that MOVED rather than disappeared: `item_store_kind_test.go`'s two refusal rows (the chained
  comparison over a promoted slot, the numeric use of a run-time-stored slot) moved into the answering
  family; `pair_number_test.go`'s `print(n + 1)` moved from the refusal table to the answering table and the
  gate property now asks `*` where it used to ask `+`; `pair_fold_test.go`'s `m + 1` row likewise; the
  `integration` CLI pins followed.
- The whole suite green on `make testparallel`, and the drift ledger unchanged in content — no record that
  this row moved the other way.

## What this row leaves owed, pinned with today's bytes

| program | reference | this backend today | row |
|---|---|---|---|
| `for v in [None, 2.5]:` `print(0 < v < 3)` | raises `TypeError: '<' not supported between instances of 'int' and 'NoneType'` | `False` `False`, exit 0 | **Gap R.443** |
| `xs.append((1,))` / `print(xs[0] + 1)` | `can only concatenate tuple (not "int") to tuple` | the same sentence about a **list** | **Gap R.444** |

The first is this file's own hole one origin over: the `for` road keeps a per-item tag record (`loopElemTag`)
and does not write the pair records the gate reads, so the loop item is still a one-word operand. The second
is a naming bug — the runtime's `rt_kind_name` tells a tuple from a list from the object, but the sentence for
an operand the SOURCE spells is chosen at compile time from the value *kind*, and a tuple's kind is the list's
in this backend (**ADR 0251**'s immutable spelling).

## Consequences

- `chain_operand.go` is the one place a chain asks about an operand's shape; a future operand class (an
  iterator, a range, a class object) is added to the **gate**, and if the pair doors decline, to the float
  net — not to a fourth road inside the chain.
- `chainValue`'s `operandName[i]` may now name a slot the chain itself bound. Every reader of those names is
  the comparison roads' existing pair/float machinery, which is the reason no second comparison road exists in
  this file.
- The chain slot is a pair wherever the operand's kind is the object's, so chains over promoted containers
  grew two `alloca`s and two stores each. Chains over literals and pinned names did not, and the IR table
  asserts exactly that difference rather than trusting it.
- `+` is answered per kind where `*` and `%` are still answered per proof. That asymmetry is CPython's own:
  `*` can answer with a list and `%` with a string, and `+` beside an integer literal cannot answer with
  anything but a number or a raise.
- Three of the four exits a chain operand can now take — bound-pair, bound-double, ordinary — are asserted in
  IR; the fourth, a trap, is asserted at the exit code and in the reference's own sentence.

## Alternatives rejected

- **Reusing the pair road's i32 slot without a tag** (read the payload, guess the kind from the container).
  This is what the parent did, and it is what produced `False` for `0 < d["a"] < 3`. Rejected for the reason
  **ADR 0189** has always given: payload and tag travel together or the answer is a coincidence.
- **Evaluating each operand at every link** (no slot at all). Cheaper IR, and it breaks **ADR 0288**'s
  single-evaluation property for side effects and for a slot a link's own comparison could disturb. Rejected.
- **A chain-specific comparison road** (an `rt_chain_compare` helper taking untagged words). It would own its
  own ordering, its own traps and its own kind rules, and diverge from the single-comparison road within a
  release. Rejected in favour of asking `comparePair`, which the single form already uses.
- **Refusing the whole chain when an operand carries a tag** (the parent's behaviour, dressed as caution).
  It is what the row was filed against: a refusal is honest about the compiler and useless to the program, and
  for the dict shapes the parent was not even refusing — it was answering. Rejected.
- **Letting `+` accept any operand once one side is a literal.** Measured: `class C: pass` / `print(C() + 1)`
  answers `1`, because an instance's int half is a pointer. Rejected; `pairRoadAnswersThisOperand` keeps the
  object kinds behind the door that already names them.
- **Dropping the double door as dead code.** It is unreachable today only because the two pair doors answer
  first, which is a property of those doors and not of the chain. Kept, and pinned by asking the door directly.
- **Writing the concatenation sentence at compile time.** The kind is a run-time fact for a slot; a format
  string and `rt_kind_name` keep one trap, one wording (**ADR 0271**) instead of two places deciding.
