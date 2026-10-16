# ADR 0363 — A `key=` door calls a function the program never wrote a call to, so the door writes that call into the census

Status: accepted
Date: 2026-10-17
Amends: [ADR 0340](0340-the-ordering-builtins-read-key-and-reverse-by-name-and-a-callable-key-is-called-by-the-ordinary-call-road.md) (which gave `sorted`/`min`/`max` their key door and said the key is called "through the ordinary call road" — true of the *lowering*, untrue of the *classification* this ADR fixes), [ADR 0345](0345-a-function-used-as-a-value-is-a-name-this-pass-mints-before-the-doors-that-decide-a-call-are-asked.md) (which gave a hook lambda the declaration the door needed, and left its body's doors asking)
Depends on: [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md) (one backend; the record and CPython are the witnesses), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md) (a shape with no lowering is a named refusal, never a silent value), [ADR 0348](0348-a-verdict-is-what-its-call-site-handed-the-parameter-so-the-print-road-may-ask-the-front-end-about-a-name-a-call-bound.md) (one position is claimed only when every site that fills it agrees), [ADR 0229](0229-a-string-is-an-index-into-a-table-the-runtime-can-add-to.md) (a text's payload is an intern index; only a runtime door renders it), [ADR 0225](0225-a-subscript-of-a-string-is-a-one-character-string.md) (a position is a code point), [ADR 0248](0248-an-ordering-of-two-texts-asks-strcmp-equality-keeps.md), [ADR 0362](0362-a-text-is-a-sequence-of-code-points-and-one-generated-table-decides-its-case.md) (an ordering is asked of the text, never of the intern table's arrival order), [ADR 0232](0232-a-container-word-means-its-payload-and-its-tag.md) (one question, one asker), [ADR 0322](0322-a-refusal-that-blames-a-container-the-program-never-mutated-is-a-pass-keyed-by-the-wrong-thing.md) (refusals name the missing half and point at a row), [ADR 0317](0317-a-guard-that-does-not-read-its-own-file-is-a-guard-that-never-ran.md) (the guards below are file-guards and depend on `-count=1`)
Roadmap rows: Gap R.264 (closed by this ADR), L11.7 (its hook-lambda half is paid; the row stays 🟨 `PARTIAL` on the shapes below), Gap R.254 (still owed — a container the program built at run time), Gap R.255 (still owed — a key the ordinary call road cannot answer), L11.1 (still owed — the tagged value word that would carry two kinds through one parameter)

## The gap, as measured

ADR 0340 gave the ordering builtins a `key=` door that calls the key **through the ordinary call road**;
ADR 0345 gave a `key=lambda …` hook a declaration of its own so the door had a name to call. Both were
true, and the programs below still refused — while the same body, called by the program itself, answered.

| program | compiled at HEAD | CPython |
|---|---|---|
| `print(sorted(["bbb", "a", "cc"], key=lambda s: len(s)))` | refused: `len of a non-string variable` | `['a', 'cc', 'bbb']` |
| `def k(s): return len(s)` / `print(sorted(["bbb","a","cc"], key=k))` | refused: `len of a non-string variable` | `['a', 'cc', 'bbb']` |
| `print(sorted(["Z","a","Mb"], key=lambda s: s.lower()))` | refused: `string-method lower on a receiver that is not a text the compiler can read: s` | `['a', 'Mb', 'Z']` |
| `print(sorted(["ab","ba"], key=lambda s: s[0]))` | refused: `index of a non-literal variable` | `['ab', 'ba']` |
| `print(sorted(["bbb","a","cc"], key=lambda s: 0-len(s)))` | refused: `len of a non-string variable` | `['bbb', 'cc', 'a']` |
| `print(min(["bbb","a","cc"], key=lambda s: len(s)))` | refused: the same | `a` |
| `print(max("bbb","a","cc", key=lambda s: len(s)))` | refused: the same | `bbb` |
| `best = max(["bbb","a","cc"], key=lambda s: len(s))` / `print(best, best.upper(), len(best))` | refused: the same | `bbb BBB 3` |
| `xs = sorted(["bbb","a"], key=lambda s: len(s))` / `print(xs)` | refused: the same | `['a', 'bbb']` |
| `for v in sorted(["bbb","a","cc"], key=lambda s: len(s)): print(v)` | refused: the same | `a`, `cc`, `bbb` |
| `print(sorted(["café","a","ab"], key=lambda s: len(s)))` | refused: the same | `['a', 'ab', 'café']` |
| `def L(s): return len(s)` / `print(sorted(["bbbb","a","cc"], key=L))` | refused: the same | `['a', 'cc', 'bbbb']` |

The control that makes this a classification bug and not a lambda bug: `def k(s): return len(s)` called as
`print(k("abc"), k("a"))` printed `3 1`, and `print(sorted(["bbb","a","cc"], key=len))` — the **builtin** key —
printed `['a', 'cc', 'bbb']`. Only a key the program *wrote* was affected.

And one answer was not a refusal at all. The container the assignment binds was classified from the
expression beside the `=`, and an ordering builtin's element kind was inherited only when that expression
was a **name**:

| program | compiled at HEAD | CPython |
|---|---|---|
| `xs = sorted(["bbb", "a"])` / `print(xs, xs[0])` | `['a', 'bbb'] 1` — at **exit 0**, the intern table's index | `['a', 'bbb'] a` |

ADR 0166 counts that second shape as the worst class: a value the reference never produces, shipped without
a word. The same blind spot, once the key was claimed, sat behind it: `xs = sorted(words, key=…)` bound a
list of texts and printed `[1, 0]`, because the container had been settled as a list of numbers.

## The decision

1. **The door's hidden call is written into the census that reads call sites.** `strArgKinds`
   (`pkg/lang/strargs.go`) decides which parameters receive texts by walking the tree's call sites. The
   `key=` call is not in the tree — codegen builds it out of the element standing in front of the door. So
   `keyDoorCallSites` reads a `sorted`/`min`/`max` call the way the door itself reads it (options by name,
   the iterable alone, the candidates written out) and hands the scan the expressions the key will be called
   with. One function, `keyDoorOptions`, is how both the door's road and the scan split a call's arguments:
   one reading, two askers (ADR 0232).

2. **The claims are the SAME claims the ordinary call road makes.** `strArgClaimOf` is the single predicate
   under both: an argument — or a candidate the door will hand over — claims `claimString` through a text
   literal, an f-string, a `%`-format, a subscript of a text container, a call whose every path answers text,
   or a name the census already knows carries a text; it claims `claimNotAString` through
   `definitelyNotAString`; otherwise it claims nothing. ADR 0348's rule therefore holds at this door exactly
   where it held before: one parameter claimed a text by the door and a number by a call site **cancels**, and
   the program that does it is refused rather than half-claimed.

3. **`sorted` answers a container of the candidates; `min` and `max` answer one of them.** `keyDoorCandidates`
   returns the candidates plus that distinction, and each caller asks its own question of them: the parameter
   scan claims the key's first parameter, and the variable census binds `best = max(words, key=len)` to a
   **text** (so `best.upper()` answers) and `xs = sorted(words, key=len)` to a container whose elements are
   **texts** (so `print(xs)`, `xs[0]` and `for v in xs` all read the text rather than the intern index). The
   binding claim is made in the one place that decides a binding's kind — the assignment arm in `codegen.go`,
   where the ordering builtins' `listElemStr` inheritance used to ask only a `*Name` argument and now asks a
   literal through `allElemsPrintAsText` (`pkg/lang/rebind_kind.go`) as well.

4. **A claim is made only where the door will actually emit a call.** `keyDoorCallSites` answers "no call
   site" for a key that is not a name the program declared (a builtin key needs no parameter claim — the door
   asks the ordinary road, which is why `key=len` always worked), for `key=None` (the identity key: the door
   never calls it), for a definition that cannot be called with one argument, and for a container the
   compiler cannot enumerate. A scan that claimed a parameter for a call that will never be emitted would be
   inventing a kind fact, which is the defect class this whole file is about.

5. **The refusals that stay are the ones that were honest before, and each names its row.** A container the
   program built at run time (`ys = ["bbb","a"]` / `sorted(ys, key=…)`) refuses because the elements exist
   only where the runtime puts them — Gap R.254, whose fix is L11.1's word. Two candidates of two kinds
   (`max("bbb", 1, key=…)`) refuse for want of the same word — L11.1. A key the ordinary call road has no
   answer for (`key=round`) still raises the reference's own `TypeError`, catchable — Gap R.255/R.248. An
   index of the door's answer **written inline** (`print(sorted(…, key=…)[0])`) still asks the index road for
   a name to read — L11.1 — while the bound form (`xs = sorted(…)`, then `xs[0]`) answers `a`.

## Agentic interface

No new flag, no new schema: what an agent consumes here is that refusals **keep naming** the missing half and
the row that owns it (`exist only at run time … roadmap L11.1, L11.7`; `no comparator … roadmap Gap R.248`), at
exit 1 and never exit 2 (ADR 0166), and that a program the compiler answers has one answer across
`--eval`, `--aot`, `--emit-llvm` and the record. `pkg/lang/call_key_param_test.go` pins the IR shapes
(`rt_str_nchars(i32 %p0)` inside the hook, `rt_elem_gt(…, i32 1)` for a text answer, no global in a value
position, no `ptrtoint`/`inttoptr`) so an agent reading a module can tell whether the door claimed its
parameter without running it; `integration/call_key_param_test.go` derives every expectation from a live
`python3` and refuses to let a stale literal stand in for the reference.

## IR implications

* A hook lambda stays ADR 0345's hoisted declaration (`sorted$lambda$…`) with **its own** parameter kind:
  claimed as a text, it arrives as the interned index that every text parameter arrives as, and its body's
  `len` lowers to `call i32 @rt_str_nchars(i32 %p0)` — the runtime door, not `llvm.inline_asm`, because the
  value is a table index (ADR 0229).
* A key that answers a text orders the list with `call i32 @rt_elem_gt(i32 %a, i32 %b, i32 1)` — mode 1 reads
  the **text** through `strcmp`, not the payload, which is ADR 0248's rule for the `>` operators asked of this
  door as well. A key that answers a number takes mode 0; the module says which, in the mode operand.
* The door's copy stays `rt_alloc` + `rt_append_tagged` + `rt_put_elem`, with ranks computed by the same
  comparator, so the copy the print path reads and the copy a loop walks are one object's two readings and the
  stability ADR 0191 promised survives (equal keys keep the order the program wrote — `sorted(["aa","bb","c","d"],
  key=len)` is `['c', 'd', 'aa', 'bb']`).
* The element-kind claim on the bound container is a **claim**, not a guess: `allElemsPrintAsText` says no if
  any candidate is not a text, which leaves the container on the mixed printer that can render every kind.

## Alternatives rejected

* **Give the key's parameter a tagged value word.** That is L11.1's destination, and it is the honest fix for
  the two-candidates-of-two-kinds row. Adopting it here would have made this commit about the word rather than
  about the missing call site, and the row's own evidence (a claimed parameter answers every door the body asks)
  says the classification is what was missing, not a wider value.
* **Let `len` accept an unproven parameter as a text.** `len` of a variable is refused precisely because the
  compiler cannot see what the variable holds (Gap R.104's door). Guessing text would print the intern index of
  a number, or `printf("%d")` a table index, at exit 0.
* **Decide the parameter from the body's shape** — "the body calls `len`, so it must be a text". That inverts
  the dependency: a door reads the parameter's kind, and the kind is what the body's doors ask about. The claim
  comes from the call site because the call site is where the argument's kind is knowable.
* **Specialise the door's copy by element kind at codegen time, beside `keyedOrderInto`.** Two places would then
  decide what a container's slots hold, and they already disagree today (`print(xs, xs[0])` above is that
  disagreement). The census is the one place; the assignment arm asks it.
* **Order the keyed list by the interned payloads, as the fold does for `min`/`max` without a key.** ADR 0248
  measured exactly this trap in the comparison operators: an index is the order the text *arrived*, not the
  order it *is*. The keyed door goes through `rt_elem_gt`'s text mode.

## Consequences

* `sorted(words, key=…)`, `min`/`max` with a hook key, and their bound forms are language surface, and
  `docs/language.md` says so with the four refusals beside them.
* A hook lambda's parameter is now claimed by a call that appears nowhere in the AST. Any future door that
  synthesises a call — `key` was the third, after `map`/`filter`'s element call and a comprehension's condition —
  must register that call with `strArgKinds`, or its body will refuse with a sentence about a variable the
  program never left unproven. `TestTheKeyDoorsCallSitesAreReadByTheParameterScan` is the pattern to copy.
* The record grew by 30 sources (7606 → 7636), every one recorded from the reference by `tools/recmerge` with
  the compiled leg cross-checked, and `docs/operations.md`, `AGENTS.md` and `roadmap.md`'s snapshot row were
  recomputed from the file by `pkg/lang/golden_artifact_counts_test.go` / `pkg/lang/roadmap_snapshot_test.go`
  rather than remembered.
