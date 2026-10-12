# 0322. A refusal that blames a container the program never mutated is a pass keyed by the wrong thing

Status: accepted. Roadmap: closes `Gap R.191` (the slot-read road of `L11.1`). Continues ADR 0265 (the numeric
slot door and the pass that decides whether it opens), ADR 0241 / ADR 0251 (a slot read answered by the object,
and the refusal that says which promise ran out), ADR 0233 (a compile-time *promise* about an object, not a
fact), ADR 0243 / ADR 0249 / ADR 0253 (the numeric use of a slot), ADR 0211 / ADR 0228 (a trap the program's own
`except` reaches), ADR 0166 (a refusal is exit 1 and a diagnostic; proving too little is the cheap direction to
be wrong), and the ADR 0317 / ADR 0319 / ADR 0320 line of guards — this cycle found, beside its own defect,
that the pass's own decision table had been answering nothing.

## Context

`Gap R.191` was filed as one sentence: *a container that holds a text anywhere makes an unrelated container's
nested slot read refuse*. Measured with three binaries — CPython, the HEAD build, this cycle's build — the
program is twelve characters of noise away from an answer:

| program | reference | compiled before | compiled after |
| --- | --- | --- | --- |
| `w = []` / `w.append("a")` / `nested = []` / `nested.append([7, 8])` / `d = nested[0][0] * 2` / `print([d])` | `[14]` | **exit 1**, `index cannot reach into nested's slots: the name was rebound, mutated, or handed to code this pass cannot see …` | `[14]` |
| the same without the binding, `print(nested[0][0] * 2)` | `14` | exit 1, the same sentence | `14` |
| `w.append("a")` / `xs.append([7.5, 8])` / `print(xs[0][0] * 2)` | `15.0` | exit 1 | `15.0` |
| `w.append("a")` / `d = {}` / `d["k"] = 40` / `print(d["k"] + 2)` | `42` | exit 1 | `42` |
| `w.append("a")` / `nested.append([7, 8])` / `print(-nested[0][0])` | `-7` | exit 1 | `-7` |
| `w.append("a")` / `outer.append([[7, 8]])` / `print(outer[0][0][0] + 1)` | `8` | exit 1 | `8` |
| `w.append("a")` / two text containers beside one numeric one | `8` | exit 1 | `8` |

And the half that must **not** move, because the reference *answers* these with a joined text or a repeated
container and this backend builds neither from a slot (`Gap R.82`):

| program | reference | compiled before | compiled after |
| --- | --- | --- | --- |
| `xs.append([7, 8])` / `ys = xs` / `ys.append("word")` / `print(xs[1] * 2)` | `wordword` | exit 1 | exit 1 — the block crossed the alias |
| `xs.append([7, 8])` / `xs.append("word")` / `print(xs[0][0] + 1)` | `word` | exit 1 | exit 1 |
| `def f(p): p.append("a")` / `f(xs)` / `print(xs[0][0] + 1)` | `8` | exit 1 | exit 1 (conservative, as before) |
| `for v in xs: v.append("a")` / `print(xs[0][0] + 1)` | `8` | exit 1 | exit 1 (conservative, as before) |
| `mystery(xs)` / `print(xs[0][0] + 1)` | `8` | exit 1 | exit 1 (a callee this pass cannot see may append) |

The root cause was one `bool`. `numericSlotChains` — the pass that answers *may `+`/`*` over this slot be
lowered?* — carried `blocked bool`, set the moment **any** container **anywhere** in the program could receive a
text or a value with no spelling. Its comment said "coarse on purpose", and coarse was honest as long as it only
ever *refused more*. What it did not say is that a refusal is a *sentence*: it names a container. The sentence
that came out named `nested`, a container the program bound once and appended numbers to, while the line that
closed the door was `w.append("a")`, a container the arithmetic never walked. An agent reading that message edits
`nested`. `nested` was fine.

Two more facts came out of the same file. The pass never saw half the program: `walkStmt` had no case for
`WithStmt`, `MatchStmt`, `ClassDef`, `YieldStmt` or `YieldFromStmt`, `walkExpr` had none for `Comp`, `Generator`,
`Slice`, `Lambda` or `AssignExpr`, and a store written as `xs += ["a"]` or `xs[0][1] = "a"` was recorded by
neither. And the table that was supposed to keep the pass honest — `TestNumericSlotChainGate`, "the gate asked
directly, so a widening of `+` fails a decision row" — called `chainDepth` on the print argument, which answers
only when handed a *bare* subscript. Every row in the table was written as `print(xs[0][0] + 1)`, a `BinOp`, so
every row hit `t.Skip("row has no print(...) argument to walk")`. The gate had no test. The one row whose
expected value this cycle inverted — "text anywhere in any container" → closed — was a row that had never run.

## Decision

**The block is keyed by the container the store writes, and it travels with the object.**

```go
type numericSlotChains struct {
    ok      map[string]bool
    depth   map[string][]map[string]bool
    blocked map[string]bool         // keyed by an alias group's root, not by the program
    alias   map[string]string       // the union-find the block travels along
    opaque  map[string]bool         // names whose object this pass cannot point at
    blockAll bool                   // the residue: a store that describes no name at all
}
```

* `record(name, value)` blocks `name`'s group when the stored value may hold a text, or has no spelling. A
  numeric store blocks nothing, so the program whose text sits in an unrelated container walks the door it
  always deserved.
* **The alias closure is what makes per-name blocking safe.** A block that followed only the name a store was
  written under would let `ys = xs` / `ys.append("word")` leave `xs` open, and `print(xs[1] * 2)` would raise a
  `TypeError` where the reference returns `wordword` — trading a refusal for the one wrong answer this door
  exists to prevent. So the walk links two names whenever a line makes them one object: `y = xs`, `a, b = xs`,
  `d = xs[0]`, `for v in xs`, `[v for v in xs]`, and — through the callee's declared parameters — `f(xs)`
  binding `xs` to `f`'s parameter. Evidence stays per-name; only blocks cross the group, which is the direction
  that can only ever refuse more.
* **`blockAll` survives for the stores that describe no name.** `xs[0].append("a")`, `self.items.add(b)`, a loop
  variable over `rows()` — the object is real and no name in the notebook stands for it, so "which container?"
  has no answer and the honest verdict is the program-wide one. This is the old bit, kept for the shapes the old
  bit was actually load-bearing for.
* **A store the old walk missed is now seen.** `xs += ["a"]` extends the container it never names as a receiver;
  `xs[0][1] = "a"` writes a slot two levels down, for which the notebook has no level, so it blocks the program;
  a call to a callee that is neither a declared function nor a known read-only builtin blocks every name it is
  handed, which is the same rule `containerEnv` has always drawn. Comprehension bodies, generator expressions,
  `with` bodies, `match` arms, class bodies and `yield` operands are walked, so a store hidden in any of them
  is a fact the pass holds rather than a fact it misses.
* **`numeric(name, chain)` carries the block check itself.** The gate is then one question, asked by
  `slotArithmeticIsProven`, instead of two that a caller must remember to pair — the pairing is exactly the
  kind of thing that drifts.
* **A gate table that finds nothing fails.** `gateChain` walks into `BinOp`, `UnOp`, `CondExpr` and call
  arguments to find the read the row is about, and a row that yields no chain is a `t.Fatalf`, not a skip. A
  decision row that cannot run is not evidence (ADR 0317's lesson, which this cycle re-learned from the inside).

## IR

Nothing new is emitted and nothing is renamed: this is a decision, not a mechanism. What changes is **which
modules carry the door**. A program whose slots are proven numeric — including one that keeps a text in some
other container — now lowers its arithmetic through `@rt_num_arith` / `@rt_lift_num`, and its print through
`@rt_str_of_value` → `@rt_print_mixed_value`, exactly as the same program without the text line always did:

```
%h   = load i32, i32* %_nested                  ; the container the literal still describes
%p0  = call i32 @rt_get_elem(i32 %h, i32 0)     ; slot 0: a list
%p1  = call i32 @rt_get_elem(i32 %p0, i32 0)    ; slot 0 of that: 7
%pair = call i32 @rt_num_arith(i32 %op, i32 %p1, i32 0, i32 %t1, i32 2, i32 0, i32* %out)
```

The cost direction is unchanged and pinned: `TestTheNumberDoorIsCarriedOnlyByTheProgramThatAsksIt` fails a
module that carries `@rt_num_arith`, `@rt_num_bad` or `@rt_lift_num` without ever asking the door (ADR 0173 /
ADR 0192 — a referenced internal function that was never emitted is the module `llc` rejects).

## Agentic rationale

A refusal is an interface. This one was a *diagnostic that lied*: it named `nested` — the container the read
walks — as the thing that had been "rebound, mutated, or handed to code this pass cannot see", when what had
happened was that an unrelated list had been given a string. An agent that trusts a message edits the thing the
message names. Widening the door is therefore not only about the programs that now run: it is about the refusal
that is left being about the container it accuses. And because the widening is one `bool` becoming a map, the
safety property has to be asserted where it is decided — hence the pass asked **as a function**, fourteen rows,
and `TestTheAliasGuardCanFail`, which builds the notebook by hand with the union-find removed and demands the
alias row change verdict.

## Alternatives rejected

* **Key the block by name and stop there.** It fixes the reported program and breaks the alias case, which is a
  wrong answer at exit 0 — strictly worse than the refusal this row is about. This was the first implementation,
  and the alias row caught it.
* **Key it by the read instead of the store** ("refuse if the read's chain looks numeric"). The chain is what
  the door already asks; the missing fact is what can be *written* into the container, which is a question about
  the program's stores, not about the one expression being lowered.
* **Keep the program-wide bit and special-case the reported shape.** A special case is a rule with a witness of
  one; the next program with a text in a neighbouring container would refuse again.
* **Drop the block and trust `numeric`'s kind tree.** The kind tree answers what was *recorded*; the block is
  what stands in for what was *not* — an alias, a parameter, a loop variable, a callee nobody can see. Deleting
  it converts those into runtime raises where the reference returns a value.
* **Decide at run time instead** (unbox whatever arrives, raise on a text). The reference does not raise on
  `xs[0] + "a"`; it concatenates. A door that guesses the kind is `Gap R.82` with the exit code changed.
* **Let the loop-variable and parameter shapes answer.** They answer the *reported* program (they are the same
  text-in-another-container shape seen through a binding) but they cannot be proved from the walk, so they stay
  refusals. Both are pinned as such, with the reference's answer written into the row, so the cycle that lands
  the aliasing properly breaks a table rather than passing.
* **Delete the vacuous gate rows instead of fixing the extractor.** Faster, greener, and it would have deleted
  the only place the pass is asked directly.

## References

`pkg/lang/numeric_slot_door.go` (`numericSlotChains`, `root`/`unify`/`block`/`blockedUnder`, `record`,
`blockStore`, `unattributableStore`, `bindValue`, `bindTuple`, `bindCallSite`, `declaredParameters`,
`computeNumericSlotChains`), `pkg/lang/heapargs.go` (`slotArithmeticIsProven`),
`pkg/lang/numeric_slot_door_scope_test.go`, `pkg/lang/numeric_slot_arith_test.go` (`gateChain`,
`TestNumericSlotChainGate`), `integration/numeric_slot_door_scope_test.go`,
`integration/numeric_slot_arith_test.go`,
`integration/programs/probe_a_text_in_one_container_leaves_another_number_door_open.gy`,
`integration/conformance_cases.go`, roadmap `Gap R.191` (closed), `Gap R.82`, `Gap R.101`, `Gap R.91`/`R.95`,
`L11.1`, docs/roadmap-details.md#gap-r-191, ADRs 0265, 0241, 0251, 0243, 0249, 0253, 0233, 0173, 0192, 0166,
0317, 0319, 0320.
