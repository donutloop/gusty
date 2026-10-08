# 0316. A fold of values the program built answers a payload and a tag

Status: accepted. Roadmap: `L11.1` (the tagged value word — Gap R.146's "`min(n, 3)`" and "a literal
`sum`/`min`/`max` folds" clauses), continues ADR 0303 (one printer that reads a tag), ADR 0305 (a pair's word
is four bytes, not eight), ADR 0309 (`abs` asks the tag), ADR 0310/0311 (a pair enters a dict, a set, and a
mutation statement), ADR 0265 (an operator's raise is written once, per kind), ADR 0228 (a trap an `except`
can reach), ADR 0315 (which let the cycle land at all).

## Context

```python
xs = []
xs.append(4)
n = xs[0]
print(min(n, 3))      # CPython: 3     compiled before this ADR: exit 1
print(sum([n, 1]))    # CPython: 5     compiled before this ADR: exit 1
m = min(n, 3)
print(m)              # CPython: 3     compiled before this ADR: exit 1
```

The name `n` already answers — `print(n)` (ADR 0303), `n - 1` (ADR 0304), `n / 4` (ADR 0305), `[n]` (ADR 0306),
`f"{n}"` (ADR 0307), `abs(n)` (ADR 0309), `xs.append(n)`/`d[k] = n` (ADR 0310/0311). The folds are the last
positions in Gap R.146 that **decide a kind** rather than consume one, and that is what makes them a different
door from the arithmetic ones:

* `min`/`max` return one of the values they were given, so the answer's kind is the **winner's** kind —
  `min(2.5, 3)` is the float, `max(2.5, 3)` is the integer. A fold cannot be lowered by lifting both sides,
  comparing, and returning one word: the word returned is the loser's shape as often as the winner's. That is
  the defect class Gap R.104 measures (`max([1, 2.5])` answering `2`), and it is why the varargs road has
  always refused a mixed int/double pair it cannot settle at compile time.
* `sum` is a left fold over `+` seeded with the integer `0`, which is why its cross-kind sentence names a kind
  the program never wrote: `sum(["a"])` is `unsupported operand type(s) for +: 'int' and 'str'` — the `int` is
  the seed.

The failure was not only a refusal. Measured against the reference (`integration/pair_fold_test.go`'s
agreement tables), a fold whose argument the old path lowered answered in the **argument's** shape: a
container-shaped argument arrived tagged as a number, so a fold over it could pick a winner by the wrong
quantity. That is the row Gap R.197 files: `min(set([n]), 3)` over a bool slot answers `True` where CPython
raises `TypeError` (a set is not comparable to an int), at exit 0. A wrong answer is the one thing this row is
not allowed to ship, so the door refuses the shapes it cannot tag and the disagreement is filed as debt with
the reference's answer beside it, rather than being described as a refusal.

## Decision

**A fold of values this program cannot settle at compile time lowers to `@rt_pair_fold`: every folded value
travels as a `(payload, tag)` pair, the run time picks the winner, and the winner's tag comes back beside its
payload.**

* **One door, one helper.** `@rt_pair_fold(op, candPayload, candTag, incPayload, incTag, outPayload*,
  outTag*, outMsg**)` compares and writes the winning *pair*. `sum` is not an op of it — `sum` is `+`, and `+`
  already has a door (`@rt_num_arith`, ADR 0265) whose raise names each kind; seeding it with the pair `(0,
  int)` is CPython's own start value, and it is what puts `'int'` first in the text element's sentence.
* **The comparison is a runtime call because it must be able to raise.** `min(n, "b")` over an int slot answers
  `TypeError: '<' not supported between instances of 'str' and 'int'` — CPython's sentence, class order
  included, and the same one `max` writes. The emitted code raises at the **step site**, through the one raise
  door every trap in the language uses, so `except TypeError:` reaches a fold's raise (ADR 0228); the helper
  only fills a static buffer. Comparing in IR (`fcmp`, `icmp`) could not raise, and raising later is raising
  in the wrong place with the wrong span.
* **The candidate is passed first, and the incumbent wins a tie**, because that is the order CPython names the
  two classes in and the value a tie returns: `max(True, 1)` is `True`.
* **The winner's payload is rooted** (`@rt_root_put`, ADR 0181) and the answer is *the pair the winner travelled
  in*. A register is not a root: the float payload is a box handle and the text payload an interned index, both
  owned by whoever built them.
* **The answer is pair-bound, and only pair-bound.** `m = min(n, 3)` stores payload and tag into the name's pair
  slot with its origin recorded as `taggedOriginFold`, so print, `str`/`repr`, an f-string field and a further
  fold read the two words the run time wrote. The answer is deliberately **not** registered as a
  container-bound name, so the arithmetic door still refuses it: `min(n, 3) + 1`, `abs(min(n, 3))`, `m + 1`,
  `m > 1` stay refusals that name the origin, the missing half and the row. That boundary is what keeps this
  from being the recursion ADR 0309 had to kill and the truncation Gap R.161 measures.
* **The argument is asked, not the answer.** Each folded value goes through `arithOperandPair`, the helper
  `abs` uses (ADR 0309): a pair-bound name arrives as the pair its element path says it is, a literal arrives
  as its own kind's pair, and anything the helper will not label makes the whole door say `ok = false` and the
  caller keep the refusal it has always printed. `ok = false` is "not mine", never "no".
* **`arithWouldRefuse` does not learn about folds**, so no one-word position quietly starts reading a fold's
  payload. The refusal sentence's promise — a pair-bound name "travels as a **(payload, tag) pair**" — is
  unchanged; a fold answer's names its origin (`the answer of a fold the built-in chose`).
* **A fold the compiler can settle never pays.** `foldMayNeedPair` is `operandMayNeedPair` over every element:
  all literals, all settled variables, all float variables → the constant road and the `fadd` it always
  emitted. A module carries `@rt_pair_fold`, `@rt_fold_bad` and `@rt.fold.fmt` only when a program **orders** a
  pair it cannot settle (`g.foldUsed`), and a program that only ever `sum`s carries the operator's block and
  none of the ordering's — ADR 0309's gate, asked of this door.
* **What the door refuses, in words**: a slot whose kind is text, None, a list, a set or a dict; a **set
  literal** (`sum({n, 3})` — CPython dedups by value and iterates in hash order, so which element is the
  incumbent is not written in the line; folding the written order answers `6` where the reference answers `3`
  at exit 0); a **dict literal** (the ordinary road already refuses one); a **name bound to a built list**
  (reading a built container's elements in order is Gaps R.95/R.83's row); a **keyword or `key=` argument**
  (`sum(xs, start=1)`, `min(a, b, key=f)` are L11.7's call surface); a **shadowed** `min`/`max`/`sum` (the
  program's own function is asked first); and a container written among the folded values (`sum([n, [1]])`).

## IR

`min` over a pair-bound name emits, per comparison step:

```llvm
  %st = call i32 @rt_pair_fold(i32 0, i32 %cand.payload, i32 %cand.tag,
                               i32 %inc.payload, i32 %inc.tag,
                               i32* %out.p, i32* %out.t, i8** %out.msg)
  %istype = icmp eq i32 %st, 1
  br i1 %istype, label %foldtype, label %foldok
foldtype:
  call void @rt_raise_type_msg(...)      ; the one raise door every trap uses
foldok:
  %v = load i32, i32* %out.p
  %t = load i32, i32* %out.t
  call void @rt_root_put(i32* %out.p)
```

and the print is `call void @rt_print_mixed_value(i32 %v, i32 %t, i32 0)` — the module's one tag-reading
printer. Three rules the gate test enforces, each because breaking it produced a real failure:

* **`store double` into a pair slot is forbidden.** A float winner travels as a **box handle** in the pair's
  i32 word; writing eight bytes into the four-byte word is the module `llc` rejects and the wrong answer
  before it was rejected (ADR 0305). The check is line-by-line on the *program's* code, because the runtime's
  own float-box writer legitimately stores a double.
* **Every block emitted here begins with exactly one label.** A second label while a block is open is
  "expected instruction opcode" from `llc`; the one-status (fold) arm therefore does not emit its merge label
  at all rather than emitting it empty.
* **The sum carries the operator's second status, the fold carries one.** `@rt_num_arith` can return 2 =
  OverflowError (the whole number that would not fit this backend's int word raises rather than letting
  `fptosi` answer poison — Gap R.133, L12.12 owns the word); `@rt_pair_fold` compares and cannot overflow.

## Agentic rationale

* The refusal sentences are the interface, and they were changed to say **(payload, tag) pair** wherever a
  pair is what the position cannot hold, so a reader — human or agent — can tell "this needs the tagged word
  that exists" from "this needs a word that does not". `integration`'s refusal tables assert those phrases.
* `--emit-llvm` for a fold program shows one call per comparison and one printer call; the answer is not
  hidden in a formatter. The gate test asserts both the presence and the absence, so the shape is a contract
  an agent can check without reading this file.
* Every behavioural case is pinned on both witness legs and the third leg a fold has: the **record** (72
  sources recorded for these programs), the **reference** (CPython, `gustyc --oracle` and the CLI tables), and
  the **IR** (this section's three rules).
* What the door cannot answer is filed, not narrated: `integration/programs/probe_a_fold_orders_two_built_containers.gy`
  is a DEBT probe whose expectation is the reference's exit class, and its absence from the pass list is the
  signal (Gap R.196's rule that a row's presence must never be the signal).

## Consequences

* `print(min(n, 3))`, `print(max([n, 2.5]))`, `print(sum([n, 1]))`, `print(f"{min(n, 3)}")`, `print(str(m))`
  and `for v in [min(n, 3)]:` answer for every kind a slot can hold — int, float, bool — with CPython's
  `TypeError` where the kinds do not compare.
* **23 pins that said "this must be refused" were inverted into printed answers**, across eleven test files:
  `min(n, 3)`, `min([n, 3])`, `max([n, 3])`, `sum([n])` left the "still refuses" tables of the number, float,
  container, dict/set, binding and mutation suites (both legs), and `print(min(abs(n), 3))` /
  `print(sum([abs(n)]))` left the signless call's refusal table into its agreement table. The contract's
  tripwires firing on purpose again: a refusal test that never fails is not testing a refusal, it is
  protecting one — and each inversion names where the answer is now pinned, so a row cannot quietly vanish.
* `pkg/lang/testdata/interpreter-golden.json` gained **72 records**, all fold-shaped, and the drift ledger is
  unchanged (338 + 21 rows) — the fold programs had *no record*, not a diverging one, because these are shapes
  only the LLVM backend answers.
* Conformance grew to **174 rows, 135 asserted, 0 failing**: one new standalone program
  (`probe_the_fold_builtins_answer_the_pair`), one new debt probe
  (`probe_a_fold_orders_two_built_containers`), and one program **promoted from debt to asserted**
  (`probe_pair_bound_name_takes_a_value`, the ADR 0314-era probe this door paid). The oracle moved to **120
  match / 33 debt / 21 not-applicable** — the promotion and the new program are the two new matches, and the
  debt count is unchanged because the promotion paid one and the new probe filed one.
* The `abs` refusal table is smaller by two rows and says so in its own comment; the rows that remain are the
  one-word positions, which is the boundary this ADR draws on purpose.
* Two gaps are filed rather than papered over: **Gap R.197** (a fold orders two built containers by a quantity
  CPython never consults — a `set` argument picks a winner where the reference raises) and **Gap R.198** (a
  set literal, and a container written among the folded values, stay refusals in words).
* `@rt_pair_fold` and `@rt_fold_bad` are `internal` and gated: a module that does not order a pair does not
  carry them, and `make testshards` prints the same evidence lines as before.

## Alternatives rejected

* **Lift both arguments, compare in IR, return the winner's word.** This is what the row started as and it is
  the Gap R.104 bug in a new coat: the returned word is the loser's shape as often as the winner's, and
  `print(min(n, 3))` then prints `3.0` and `3` for the same value depending on which formatter it reaches.
* **Give `sum` its own op in `@rt_pair_fold`.** `sum` is `+`; a second `+` writes a second cross-kind sentence,
  which ADR 0265 exists to prevent, and the seed (`0`) has to appear in the sentence's class list or the text
  element's message names a kind nobody wrote.
* **Settle `min`/`max` over a set literal in source order.** Answers `sum({n, 3})` as `6` where the reference
  answers `3` — an exit-0 wrong answer, the one outcome this row may not ship. Refusing is cheap and honest;
  guessing is not.
* **Register the fold's answer as a container-bound name so `min(n, 3) + 1` works too.** Every truncation this
  compiler has been patched for (ADR 0309's recursion, Gap R.161's silent int, Gap R.101's untagged sum)
  started by letting one more door read one word of a pair.
* **Fold the empty case into a constant.** `@rt_pair_fold` compares; it cannot tell "no elements" from "one
  element that won", and `min` of nothing answering `0` is an exit-0 wrong answer. The door therefore requires
  at least one folded value and the empty container keeps the ordinary road's refusal; the row that lets a
  built container reach the fold (Gap R.197) is the row that writes the empty trap.
* **Answer the built-set disagreement by making `set()` a list.** gusty documents set semantics the reference
  shares (`in`, `len`, dedup); the disagreement is in the fold's reading of it, and fixing it by changing `set`
  would move the bug into the container instead of the fold.

## References

`pkg/lang/pairfold.go` (`foldCallShape`, `foldMayNeedPair`, `taggedFoldPair`, `emitFoldStep`,
`pairFoldPrint`, `bindFoldPair`, `foldPairOf`), `pkg/lang/codegen.go` (`pairFoldRuntimeIR`, `foldUsed`),
`pkg/lang/pair_fold_test.go` (the answer tables, the IR gate, the refusals),
`integration/pair_fold_test.go` (agreement, traps, refusals, the constant road, the filed debt),
`integration/conformance_cases.go`, `docs/language.md` (§ The fold builtins over a pair), ADR 0303, ADR 0305,
ADR 0309, ADR 0310, ADR 0311, ADR 0265, ADR 0228, ADR 0315.
