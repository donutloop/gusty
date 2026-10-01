# A comprehension that folds *is* the literal it folds to, and a literal has one lowering

Status: accepted. Closes roadmap **Gap J.2** (set/dict comprehension **assignment** in the compiled
backend, and the `{x for x in xs if c}` parse gap) and promotes `L12`'s comprehension surface to a
three-engine claim. Cites: ADR 0163 (a container binding allocates, tags and registers — the rule
this extends), ADR 0188 (a container literal in print position is a *rendering* question, answered
by the runtime printer), ADR 0189 (every container builder writes the tag with the payload),
ADR 0192 (a comprehension over a runtime iterable is a real loop, ADR 0192), ADR 0232 (a container
word is a `(payload, tag)` pair), ADR 0186 (the CPython leg, which is what made "it printed
something" insufficient), ADR 0211 (an `llc` rejection is exit 2, a compiler bug, never a refusal).

## What had been

`comp()` folds a comprehension whose iterable is an inline list literal or a `range()` with constant
bounds. For a list the fold is emitted as `@.lstN`; for a set and a dict as `@.setN` and `@.dictN`.
All three are `{i32, [n x i32]}` — a length plus an array. That is a fine *static* layout and a
terrible *value*, and every consumer that asked the fold for a value got the address:

```gy
sa = {x for x in [3, 1, 2]}          # store i32 @.set1, i32* %_sa        -> llc: exit 2
print({x for x in [3, 1, 2]})        # rt_print_list_mixed(i32 @.set1, 0) -> llc: exit 2
print(2 in {x for x in [1, 2]})      # rt_contains(i32 @.set1, i32 2)     -> llc: exit 2
```

Three shapes, one module each that `llc` refuses, and the exit-code contract (ADR 0211) correctly
labels exit 2 "a compiler bug". The programs are ordinary Python.

The list spelling had already been fixed, twice over: ADR 0188 made print build the object and ask
`rt_print_list`, and the assignment path copies the folded `@.lstN` into a heap list (ADR 0163's
rule, keyed on `staticLists`). **Neither fix had been carried to the other two container kinds.**
`staticLists` had no `staticSets`/`staticDicts` beside it, and print's `*Comp` branch asked one
printer for every kind, so a set comprehension was on its way to `rt_print_list_mixed` — the wrong
printer even had the module been valid.

The set/dict spelling had a second, dumber blocker: it did not parse. `parseDictOrSet` read the
iterable with `parseExpr()`, a full expression — and a full expression is a ternary, which treats
the comprehension's own `if` as *its* `if` and then demands an `else`:

```gy
sa = {x for x in xs if x > 1}        # parse error at 1:29: expected keyword "else"
```

`parseListOrComp` had long since learned to parse an iterable at `or`-precedence to keep the filter
(`[x for x in xs if x > 1]` worked). The braces had not been told.

## The decision

**One literal, one lowering.** A comprehension whose element, key/value and filter all fold *is*
the container literal it denotes, and a container literal has exactly one binding: allocate the heap
object of the right kind, write each slot's payload together with its tag, register the variable's
kind so print/`in`/subscript/iteration ask the runtime. Nothing is special-cased for the
comprehension, because by the time the binding runs there is no comprehension left to special-case —
only the `{1, 2}` the fold produced.

Three pieces, all in the codegen's existing vocabulary:

1. **`compItems` + `foldSetComp` / `foldDictComp`** — the item derivation and the unrolled fold move
   out of `comp()` into functions that return the literal (`*SetLit` / `*DictLit`) next to the folded
   words. `comp()` now emits its global *from* that literal and records it in `staticSets` /
   `staticDicts`, beside the `staticLists` the list path already used. One fold, one spelling of
   "what this comprehension means", consulted by whoever asks.
2. **`foldedContainerCompLiteral`** — the binding rule asks it before lowering the right-hand side.
   When the answer is a literal, the assignment runs the `*SetLit` / `*DictLit` path that
   `d = {1: 2}` has always used (`rt_alloc(i32 3)` + `rt_set_add` + `rt_tag_elem`, or
   `rt_alloc(i32 2)` + `rt_dict_put` + two tags), and records `runtimeSets` / `runtimeDicts`. The
   two spellings of one container cannot drift, because they are one code path.
3. **Kind-aware print and the runtime loop** — print dispatches on `Comp.Kind`:
   `rt_print_list_mixed` / `rt_set_print` / `rt_dict_print`, and `containerOperand` materialises a
   folded set/dict global the way it already materialised a list one. `runtimeCompLoop`, the real
   loop ADR 0192 gave list comprehensions, now fills a set (`rt_set_add_tagged`) or a dict
   (`rt_dict_put_tagged`) too, and the binding registers the kind written in the syntax — without
   that record `print(sa)` printed the handle and answered `1` where the other two engines answer
   `{2, 3}`.

On the front end, the iterable of a set/dict comprehension is parsed at `or`-precedence, exactly as
the list comprehension does it — so `if` belongs to the comprehension again, and an `or` iterable
(`{x for x in a or b if …}`) still parses.

## What the measurement caught

* The obvious target was the assignment, and it was the *least* broken thing once measured: three of
  the four shapes were separate invalid-IR sites (`store`, the printer call, the `rt_contains`
  call). Fixing only the store — the literal reading of the gap — would have shipped a program that
  compiles and then dies at the next line. The guard test therefore scans the whole module with a
  regex for a folded container global in an operand position, over seven programs, instead of
  asserting three `strings.Contains`.
* The print branch exposed a bug that had nothing to do with validity: `*Comp` in print position was
  one printer for three kinds. A valid module would still have rendered `{1, 2}` as `[1, 2]`. The
  conformance program is written so CPython's rendering pins this: `{4, 5}` and `{4: 12, 5: 15}` are
  not what the list printer emits.
* `x in {comp}` was not in the gap's Definition of Done. It is the same operand question with the
  same wrong answer, found by writing the corpus program rather than the minimal repro.
* The set-order convention earned itself: measuring `[3, 1, 2]` against CPython's `{1, 2, 3}` made
  it explicit that the corpus program lists members ascending, so the row is a `match` and the
  documented insertion-order convention (`docs/language.md` § Dicts & sets) is not silently promoted
  into a parity claim.

## Alternatives rejected

* **Give `@.setN` / `@.dictN` a pointer-typed layout and pass it around.** Rejected: the whole
  compiled backend stores handles (`i32` indices into `@heap`), the collector traces handles, and the
  printers take handles. A pointer in an `i32` slot is the bug, not the representation; this is the
  same conclusion ADR 0224 reached for strings ("a string value is an `@str_tab` index, not the
  address of a literal").
* **Emit the folded containers as heap initialisers and `memcpy` them at the binding.** Rejected: the
  heap is one flat `[1024 x {i32, i32, [256 x i32]}]` array handed out by `rt_alloc`, so a static
  image cannot name its destination, and the copy would have to reproduce `rt_set_add`'s dedup and
  `rt_dict_put`'s key-match exactly — a second implementation of the semantics, which is how Gap R.40
  and ADR 0189's four untagged builders happened.
* **Wait for L11.1's tagged value word to make every container a runtime object.** Rejected as a
  schedule, kept as the direction: L11.1 is 🟢 IN PROGRESS and its own row lists
  `store i32 @.setN` as a symptom to kill at the source. Killing the symptom here is not a competing
  representation — the tags written are ADR 0232's, the builders called are the existing ones, and
  when the tag becomes the only story the fold has one fewer special case to drop.
* **Refuse set/dict comprehension binding with a stable code, as L12.1 would have it.** Rejected: a
  refusal is only honest when nothing nearby answers. `{1, 2}` already runs on both backends, so
  "your comprehension is not supported while your literal is" is the wrong answer with a nice code.
* **Let the interpreter's set rendering grow a sort to match CPython.** Rejected: sets are unordered
  in the language, both backends already agree with each other and with the documented insertion-order
  convention, and reproducing CPython's hash-table order would make the order part of the language.

## Consequences, and the honest limits

* `programs/comp_containers.gy` is conformance program 111 and matrix row 102; it is `match` on the
  oracle leg (61 `match` / 25 `debt` / 16 `not_applicable`, 0 drift). Unit coverage is
  `pkg/lang/comprehension_containers_test.go` (parser shape, the module-wide value-position guard,
  the two builders, the runtime loop's kind registration, the `in` handle, and a refusal-parity check
  that a set twin refuses in exactly the words its list twin uses).
* A set/dict comprehension over an iterable the compiler folded away still refuses — with the
  message the list spelling has always used, asserted equal in `TestContainerComprehensionRefusalsStayHonest`.
  `xs.append(...)` materialises the list and the program compiles; L11.1's tagged element is what
  retires the refusal for good.
* A non-integer iterable or element still refuses (`comprehension iterable must be constant
  integers`, `comprehension key must be constant`) — same refusal, same words, as list
  comprehensions; L11.5/L11.6 own the representations.
* **Newly measured, not fixed here** (roadmap `Gap R.67`): a container *returned* from a function is
  not a value in the compiled backend. `return [1, 2]` emits `ret i32 @.lst1` — exit 2 — and
  `la = [1, 2]; return la` compiles and prints `0`. It is the same operand question one statement
  further out, and it affects literals too, so the comprehension binding did not create it; the
  comprehension simply stopped refusing at the binding and now reaches it.
