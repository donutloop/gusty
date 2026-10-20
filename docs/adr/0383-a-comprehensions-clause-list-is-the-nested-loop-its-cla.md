# A comprehension's clause list is the nested loop its clauses spell, and each arm of its door either answers or says so

**Roadmap:** Gap R.352 (closed here — a second `for` clause was `parse error: expected "]"`, so the flattening idiom did not parse at all) ·
Gap R.355 (newly measured and filed — an element that is a container storing a clause's non-number binding answered the container's *index*) ·
Gap R.356 (newly measured and filed — a clause whose iterable is a container the run time built is refused where one clause over the same name answers) ·
L11.7 / L11.1 (the items the surface belongs to) · ADR 0192 (the comprehension road this grew: the fold, the runtime list, the runtime loop) ·
ADR 0381 (a comprehension's target is a binding of the frame that hosts it — here applied to every clause, not the first) ·
ADR 0232 / ADR 0187 (a container's facts are its kind, its per-slot tags, and what its slots hold; a payload never travels without its tag) ·
ADR 0306 / ADR 0310 (the literal builders that take payload **and** tag, which the runtime arm uses) · ADR 0259 (a folded comprehension is a global with no tag table to write) ·
ADR 0346 (a refusal is part of the model: it names the shape, carries a capability code, and points at the row that owns it) ·
ADR 0166 (exit 2 is a compiler bug — every arm here is checked through `llc`, and no row reaches it) ·
ADR 0302 / ADR 0317 (one backend, so a rule the comprehension road does not have has no second engine to catch it; and the anti-caching rule that judges the record leg)

## Status

Accepted. Landed 2026-10-20.

## Context

Three lines a person writes without thinking:

```python
print([y for x in [[1, 2], [3, 4]] for y in x])   # the reference: [1, 2, 3, 4]     here: parse error at 1:28: expected "]"
print([[x, y] for x in [1, 2] for y in [3, 4]])   # the reference: [[1,3],[1,4],…]  here: parse error at 1:31: expected "]"
print([x + y for x in [1, 2] for y in [3, 4]])    # the reference: [4, 5, 5, 6]     here: parse error at 1:26: expected "]"
```

The grammar held **one** `for` clause. Every list, set and dict comprehension parsed the first clause and then expected the closing bracket, so the
whole Python-shaped idiom — rows of rows, a matrix, a pair of ranges — was not a half-supported feature but a syntax error, and the diagnostic
pointed at a bracket rather than at the cause.

The record leg told a different story all along. `testdata/interpreter-golden.json` holds **46** sources with two or more `for` clauses, and they carry
the retired engine's answers: `print([[x, y] for x in [1, 2] for y in [3, 4]])` is on record as `[[1, 3], [1, 4], [2, 3], [2, 4]]`, and
`print([x for x in [1, 2, 3] if x > 1 for y in [10, 20] if y > 10])` as `[2, 3]`. The retired parser had a clause list; the compiled front end did not.
And none of this was on the drift ledger — a source the compiled leg cannot parse produces no second answer, and the ledger compares answers. The blind
spot was exactly the class of defect this repo ranks highest: a program the reference runs, refused, with the right answer already sitting in the
artifact nobody was reading for it.

Building the second clause then produced two answers that were **wrong at exit 0**, which is the class below a refusal:

```python
print([[x, y] for x in [1] for y in [[3]]])          # built [[1, 1]]        for the reference's [[1, [3]]]
print({x: y for x in [1, 2] for y in [10, 20] if y > x})   # built {1: 10, 2: 10}  for the reference's {1: 20, 2: 20}
```

The first is the nested **fold** binding a loop variable to an item's *lowered word*: a container literal lowers to its index in the module's container
table, so `y` was `1`, and the element `[x, y]` stored that integer into a slot. The second is a dict fold that kept the first value a key was written
with, where a dict keeps the *position* and the last value.

## Decision

**Parse the list, carry the list, and let one door answer each nest — with two arms and an explicit refusal where neither can speak.**

1. **The grammar reads a clause list.** `parseCompClauses` reads `for NAME in ITER [if COND]` repeated, left to right, `for` clause after `for`
   clause, and every comprehension site — list, set, dict, and the generator tail — takes the whole list. Repeated `if`s on one clause fold into that
   clause's condition with `and`, which is what the reference's semantics reduce to and what keeps the AST one-clause-shaped where a road has not grown.
2. **The AST carries it without breaking the roads that read one clause.** `Comp.Clauses []*CompClause` and `Generator.Clauses` are the truth;
   `ForVar`/`Iter`/`Cond` stay as the *first* clause, because that is how every road that ever read a comprehension reads one. `compClauses()`,
   `compTargets()` and `compIsSingleClause()` are how a road says which question it is asking — and a road that walks one clause asks
   `compIsSingleClause` before it trusts the three legacy fields.
3. **One door, two arms.** `nestedComp` is the entry the comprehension road takes when `!compIsSingleClause(c)`. `nestedCompFold` evaluates the element
   under all the bindings and emits the compile-time global (that is what keeps `sum([x+y for x in [1,2] for y in [3,4]])` at `20` and
   `min`/`max` over a nest folding). `nestedCompRuntimeList` builds the heap object one `rt_append_tagged` per combination, allocating one slot per
   clause target — ADR 0381's rule, applied to every clause — so the element reads its names out of slots the clauses actually wrote.
4. **The fold may bind only numbers.** `compFoldItemIsAPlainNumber` is the gate: a clause's item that is not an `*IntLit` sends the whole comprehension to
   the runtime arm, because a folded nest writes a global with no tag table beside it (ADR 0259). This is what turns the `[[1, 1]]` above from an answer
   into either a correct build or a refusal.
5. **A dict keeps the position and the last value.** `keyPos` in the fold makes `{x: y for x in [1,2] for y in [10,20] if y>x}` end at `{1: 20, 2: 20}`,
   which is the reference's dict, and the set's dedup keeps the first member because a set's order is not a fact.
6. **An element that is itself a container says so.** `compElemContainerMentionsAnUntaggableSlot` asks, at the leaf of the nest, whether the element
   stores a clause target whose item is not a number; if it does, the road refuses naming the clause target and the item (`Gap R.355`). Building that
   container through the tagged heap constructors is the row's own proposal; guessing the payload into a static table is not.
7. **What neither arm can walk refuses in its own words, with a code.** A clause over a container the run time builds (`Gap R.356`,
   `lower.unsupported.comprehension-clause-over-runtime-container`), an element whose container keeps one word per slot
   (`lower.unsupported.comprehension-clause-element-keeps-one-word`), and a set/dict nest whose element is not constant
   (`lower.unsupported.comprehension-clause-needs-the-builder`) each name the shape, name the row, and exit **1**. `compSurfaceOf` spells the offending
   comprehension with the brackets and element the program actually wrote — a diagnostic that writes `[...]` for a set comprehension describes a program
   nobody wrote.
8. **Every reader of a comprehension asks the list.** The semantic analyzer binds each clause target in the scope (`inferComp`), the free-variable and
   binding scanners walk every clause (`scanCompClauseList`, `compTargets`, `walkComp`, `scanCompBinds`, `numeric_slot_door`, `callable_value`, `params`,
   `lsp`), and the formatter writes every clause it read — a `gustyc --fmt` that printed the first clause only would delete the rest of a program on save,
   which is a worse defect than the one this row fixes.
9. **A generator expression carries the list too.** The generator road unrolls one clause and delegates a nest to `nestedComp` rather than answering a
   two-clause generator with a one-clause answer; what it returns is the same eager heap list it always returned, so `for v in (x+y for x in xs for y in ys)`
   walks the product.

## What is answered, and what is refused

| Shape | Reference | This backend |
|---|---|---|
| `[y for x in [[1,2],[3,4]] for y in x]` | `[1, 2, 3, 4]` | answers, both legs |
| `[[x,y] for x in [1,2] for y in [3,4]]` | `[[1,3],[1,4],[2,3],[2,4]]` | answers, both legs |
| `[x*10+y for x in range(2) for y in range(2)]` | `[0, 1, 10, 11]` | answers, both legs |
| `[x+y+z for x in [1] for y in [2] for z in [3]]` | `[6]` | answers, both legs |
| `[x for x in [1,2,3] if x>1 for y in [10,20] if y>10]` | `[2, 3]` | answers — a filter belongs to its clause and skips the rest of the nest |
| `[y for x in [1,2] if x>1 for y in [7,8]]` | `[7, 8]` | answers |
| `{x+y for x in [1] for y in [2]}`, `{x:y for x in [1] for y in [2]}` | `{3}`, `{1: 2}` | answers, both legs |
| `[(x,y) for x in [1] for y in [2]]` | `[(1, 2)]` | answers, both legs |
| `[x for x in [1] for x in [2]]` | `[2]` | answers — a later clause rebinds the name and the element sees the later binding |
| `(x+y for x in [1,2] for y in [3])` looped over | `4`, `5` | answers, both legs |
| `rows = [[1,2],[3,4]]` / `[y for x in rows for y in x]` | `[1, 2, 3, 4]` | **refused** — `Gap R.356`, the nested run-time loop |
| `[c for w in ["ab","cd"] for c in w]`, `[x+y for x in [1.5] for y in [1]]` | characters, `[2.5]` | **refused** — a comprehension over a text or a float is refused one clause in too (`L11.1`) |
| `[[x,y] for x in [1] for y in [[3]]]` | `[[1, [3]]]` | **refused** — `Gap R.355`, the element's container has one word per slot |
| `{[x,y] for x in [1] for y in [2]}` | `{([1, 2])}`-shaped set | **refused** — a set/dict nest whose element is not constant needs the runtime builder |

## Consequences

* The flattening idiom exists. So does the product of two ranges, and the matrix-shaped element, and the dict comprehension over two clauses — built once,
  in the one backend, and witnessed on both legs: 18 answer rows against the reference with `Compile` asserted, 16 of the same programs against the record,
  the CLI against CPython on every engine, and `integration/programs/comprehension_clauses.gy` line-for-line CPython as matrix row
  `programs/comprehension_clauses` (`oracle: match`).
* Nineteen sources went to `tools/recmerge` (the record to **8049**), and none of them is self-certified: the recorder asks CPython first and cross-checks
  the compiled leg, so a source whose legs disagree has no entry to write.
* The fold is now narrower, and that is the point. A fold that binds a non-number item as an integer was producing the module's second wrong answer of the
  cycle; a program that needs the tag goes to the arm that carries it.
* Two new capability codes and one more refusal family mean an agent reading `--json` gets `{code, shape, suggestion, rows}` for a comprehension it cannot
  compile, rather than a bare `codegen:` string.
* A nested comprehension inside a function body that reads an enclosing name, and a comprehension whose element is a nested comprehension, are **unchanged**
  by this row: the first is `Gap R.349`'s and the second `Gap R.350`'s, and neither door was widened to let this one through.

## Machine path

`--emit-ast` carries `clauses` on `Comp` and on `Generator` (each `{target, iter, cond}`), with `for_var`/`iter`/`cond` still the first clause;
`--fmt` writes the whole list and `--fmt-check` accepts a canonical multi-clause source as already formatted; `--json` classifies each new refusal through
the registry (`lower.unsupported.comprehension-clause-over-runtime-container`, `…-element-keeps-one-word`, `…-needs-the-builder`), each carrying its shape,
its suggestion and the roadmap rows that own it. The exit codes are unchanged: `1` for a refusal, `2` forbidden — asserted row by row in both suites,
because a refusal that reaches `llc` and is rejected is a crash wearing a refusal's clothes (ADR 0166).

## Alternatives rejected

* **Desugar the nest to the `for` road's statement loops.** It is tempting — the `for` statement already walks a run-time container, a dict's keys and a
  text's characters with the tags in place — and it is the plan `Gap R.356` names. It is not this commit: an expression position that emits statements needs
  the frame, escape-analysis and `noteFrameTarget` books kept by three roads at once, and doing it badly would trade a clean refusal for a wrong answer.
* **Keep the parser one-clause and refuse the second clause there.** A parse error at a bracket is the worst available diagnostic for a construct the record
  leg has 46 answers for, and it makes every downstream road look like it supports one clause.
* **Fold everything, tagging after.** The fold writes a compile-time global: there is no tag table to write into it, which is ADR 0259's reason
  `[x for x in [True, 1, 1]]` belongs to the runtime arm. The same reason retires binding a container item as its index.
* **Walk the first clause and answer.** A one-clause answer to a two-clause comprehension is a wrong value at exit 0 — `[y for x in [[1,2]] for y in x]` as
  `[[1, 2]]`. Every road that cannot see the whole list asks `compIsSingleClause` instead.
* **Guess a name's kind from its first binding.** A clause's iterable may be a container variable whose slots the compiler never saw, and a loop variable
  that "was a list once" may hold a text at run time; the door raises only where the AST states the fact (ADR 0273's rule, one construct over).
* **Ship the dict fold's first-value-wins rule.** It read as a plausible dict, matched the naive implementation of the record's own answer, and disagreed
  with the reference on every key that was written twice.
