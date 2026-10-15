# ADR 0356 — An f-string field is a print in disguise: one subscript door, one printer, and a read is a read wherever it sits

Status: accepted
Date: 2026-10-16
Amends: [ADR 0331](0331-a-list-literal-is-read-by-the-door-a-list-elements-are-read-by.md) (whose docstring claimed the f-string field asked the pair door; the arm was never in the code)
Depends on: [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md), [ADR 0187](0187-tagged-slots-are-written-as-pairs.md), [ADR 0232](0232-a-container-word-means-its-payload-and-its-tag.md), [ADR 0241](0241-a-slot-is-read-back-by-the-tag-the-builder-wrote-remembered-at.md), [ADR 0299](0299-an-f-strings-format-spec-is-part-of-the-program-not-a-suffix-to-thr.md), [ADR 0303](0303-str-and-repr-answer-for-a-value-whose-kind-the-run-time-dec.md), [ADR 0317](0317-a-guard-that-does-not-read-its-own-file-is-a-guard-that-never-ran.md), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md)
Roadmap rows: Gap R.283 (paid), Gap R.289 (paid), Gap R.292 (paid, honesty half), Gap R.294 (paid), Gap R.290, Gap R.295, Gap R.296, Gap R.297

## The gap, as measured

One value, two lines, two answers:

```gusty
d = {1: 3.5, 2: "x", 3: None, 4: True, 5: [2]}
print(d[1])            # 3.5   — correct, and has been since ADR 0232
print(f"{d[1]}")       # 5     — the @float_box handle, at exit 0
print(f"{d[2]}")       # 0     — the text's index into @str_tab
print(f"{d[5]}")       # 5     — a heap handle wearing a list
```

Not a trap, not a refusal: three numbers, exit 0, and the value the program assigned gone. The same probe over
the `None`-key twin (which is where ADR 0353 left the family) answered the same three ways, so the defect was on
the field road and not on the key road — and the int-key spelling of the same field was equally wrong, which is
how the row was ruled in.

What makes it worth an ADR rather than an arm is that the pair door *already existed*, in every shape the field
needed: `mixedDictPair`, `mixedElemPair`, `literalDictPair`, `listLiteralPair`, `taggedContainerRead`. `print`
asked all five, in an order it decided for itself. The f-string field asked **none of them** — it reached
straight for the payload word. And the file that documents that door, `pkg/lang/list_literal_read.go`, lists the
positions that are supposed to ask it:

> a position that reads a value by its kind — print, `str`/`repr`, **an f-string field**, a binding, a `len`, a
> membership test, a loop — asks the door for the pair

The field was on the list, in prose, and not in the code. Nothing failed, because the claim was written where a
reader (and the next cycle) would trust it — ADR 0317's lesson in a new costume.

## Three more defects, one root each, all in the road this fix had to touch

Fixing the field surfaced three things that were not the field's, and each is filed and pinned separately rather
than smoothed into this decision.

**(a) the field could not be parsed at all when it was padded** (Gap R.294). `buildFString` re-lexes the text
between the braces, and the lexer is indentation-aware, so `f"{ x }"` opened with an `INDENT` and died
`parse error at 1:2: unexpected token` — for the one spelling the pair-door tests would most naturally write, and
for every field that begins with a brace (`f"{ {'a': 1.5}['a'] }"`).

**(b) a format spec over a field the spec engine could not read printed blanks** (Gap R.292). ADR 0299's engine
recognised `IntLit`/`FloatLit`/`StrLit`; anything else fell past the numeric arm with `text == ""` and was
**aligned as an empty text** — `x = 5` / `print(f"{x:>6}")` wrote six spaces at exit 0. This is the class the
ladder puts above every refusal, hiding inside a format string that looks well-formed in `--emit-llvm`.

**(c) a read the escape analysis could not see deleted the container it reads** (Gap R.289). This one was behind
half of the field's own answers. `deadListAssignments` decides whether a top-level container's heap allocation is
dead, and its walkers enumerated node types. A program whose only read of a list sat in an f-string field, a
keyword argument, a tuple element, a chain comparison, a comprehension's iterable, a `match` guard, a `raise`
argument, a class body, a decorator or a parameter default had that allocation taken away — and was then refused
or answered with zeros by a sentence about a construct the source did not contain:

```gusty
xs = [1.5, "a"]
def show(k): print(k)
show(k=xs)
# gustyc: codegen: undefined name "xs" (no binding for it; assign it before use)
```

## The decision

**1. One subscript door, asked by both roads** — `pkg/lang/subscript_pair.go`'s `(*irGen).subscriptPairOf`. It is
`print`'s dispatch extracted verbatim, arms and order intact: dict literal, list literal, mixed list, mixed dict,
nested/tagged container read. `print` asks it; `filedPairOf` (the field road) asks it. The order is load-bearing
because the arms overlap, and two roads that disagree about which arm runs first are exactly the two roads that
grow apart — which is what had happened. `ok=false` means "no arm runs; the ordinary one-word road answers",
which keeps the shapes that were already right (an all-integer literal at a constant position, a single-kind
container) on the cheap path — pinned by a row requiring the fold to emit no `@rt_alloc`, no `@rt_get_elem` and
no call into the printer.

**2. A fragment is lexed as a fragment** — `lexFieldSrc` drops the `INDENT`/`DEDENT`/`NEWLINE` tokens a field's
own padding invents, **at the ends only**, and keeps every surviving token's column. Interior whitespace
separates tokens; it is not indentation.

**3. A spec is applied to a value the engine read, or not at all** — `formatFieldConst` now records whether it
*read* the field (`readable`) and refuses where it did not, the same way ADR 0299 already refuses a presentation
type over a non-number. Honouring a spec over a run-time value stays L12.8 / Gap R.60 / Gap R.186's row.

**4. A read is a read wherever it sits** — `pkg/lang/escape.go` walks the AST through `reflect`, over every
exported field of every node, descending into anything that is or holds an `Expr` or a `Stmt`. The enumeration of
node types is gone, because the enumeration *was* the defect. Two policy nodes remain enumerated, in both their
spellings (value and pointer): `*Lambda` is walked into — a lambda is an expression the module evaluates here,
and a closure's free names are reads of this scope — and `*FuncDef` is walked up to but never into its body,
because a compiled body is another function's program and this backend refuses to read module state from one at
all (Gap R.35's own sentence). That asymmetry is stated, tested, and given its own row (Gap R.290) so the day it
changes is a tracker event and not a diff.

## What the record leg can and cannot witness here

The retired engine ran 7008 sources (the record holds 7009 today, the extra one recorded from the reference after the retirement). Exactly **one** of the engine's own interpolated a subscript into an f-string field
(`print(f"{max([True, 0])}")` — a call, not a slot), which is the whole reason the defect survived: no record ever
put a slot read inside a field, so no record could notice what the field did with one.

So the record leg in `pkg/lang/fstring_pair_read_test.go` asks the record about the **read** (`print(d[False])` →
`3.5`) and the compiled backend about the **field** (`print(f"{ d[False] }")`), and requires one text. Minting a
record for the field spelling would be the compiled backend testifying about itself, which ADR 0302 forbids. A
guard pins the silence itself — `TestTheRecordNeverInterpolatedASlotReadSoTheFieldIsWitnessedByItsPrintTwinAlone`
fails if the artifact's count of such sources stops being exactly that one — so nobody can later claim the record
witnessed the field, and nobody can "fix" the count by recording an answer the engine never produced.

## The machine path

Nothing new is exposed, and nothing existing moved: `--emit-llvm` shows the field going through
`@rt_str_of_value`/`@rt_print_mixed_value` (asserted by an IR row, so a second renderer appearing is a red case),
the `--json` diagnostic carries the spec refusal's sentence verbatim, and exit codes are unchanged (exit 1 for a
refusal, exit 2 still unreachable — two of these shapes used to be exit 2 and are now exit 0 or exit 1). The
capability registry's `lower.unsupported.comprehension-over-constant-list` sample was rewritten to a shape that
still refuses, with its citation moved to the row that now owns it (Gap R.290), and `docs/operations.md`'s
capability table is regenerated from the registry by `go run ./tools/captable`.

## Alternatives rejected

* **Add the missing arms to the field road, keeping two dispatches.** It is the smaller diff and it passes every
  test in this file. It is also how the road got here: `print` and the field each keeping their own list of arms
  is the mechanism that let one of them forget three. Rejected for the same reason ADR 0303 collapsed `str`,
  `repr` and `print` onto one printer.
* **Print the payload and let the field look right.** The tempting minimal fix was to keep reading the payload
  word and format it — which is what made `f"{d[1]}"` print `3`. It is a *wrong answer* at exit 0, the class this
  project ranks above every refusal, and the record's absence of a witness for the field means nothing but a real
  pair read could tell the difference.
* **A longer `switch` in the escape walker.** Rejected because the fix then has to be re-made per node type, and
  its failure mode is invisible: an unlisted node yields a silently-wrong permission, not a compile error. The
  reflection walk's failure mode is a red case, because the guard censuses `ast.go`'s own `exprNode()` receivers.
* **Count a `def` body's reads too, for symmetry.** One line, and it takes the allocation away from the module
  body that genuinely reads the name, to serve a read the backend refuses to compile. Gap R.290 owns the day both
  halves can move together.
* **Have `formatFieldConst` fall back to rendering the pair itself.** That would answer `f"{x:>6}"` today, and it
  would put a second value-renderer beside the module's one printer, in the same commit as the change that
  established the field goes *to* that printer. It is L12.8's feature, with its own tests to grow.

## What was witnessed

* Reference leg — 25 field shapes in `TestAFstringFieldOfASelfDescribingSlotRendersWhatTheReferenceRenders`
  (every kind a slot holds, `!r`/`!s`, a slot two levels deep, a dict-literal field, arithmetic over a slot, a
  string position, an expression key), each against CPython through the compiler.
* Twin leg — 14 reads spelled twice (`print(E)` and `f"{ E }"`) in
  `TestTheFieldAndThePrintCannotDisagreeAboutOneRead`: a disagreement is a door with an arm the other road does
  not have, which is the shape of the bug rather than its symptom.
* Record leg — the same 14 reads' recorded sources, through `runGoldenStdout`, with the missing-record path
  failing rather than skipping.
* IR — one printer (`@rt_str_of_value`, `@rt_print_mixed_value`, `@rt_dict_get_tagged`, `@rt_dict_value_tag`), the
  interned-text index never reaching `printf` without `@rt_str_ptr`, and the foldable field still folding.
* Front end — `pkg/lang/fstring_field_pad_test.go`: padded/plain parse to the same tree with columns kept, nine
  shapes that were syntax errors parse, a padded field keeps its `!r` and its `.2f`, and `lexFieldSrc`'s token set
  is asked of eight fragments.
* Escape walk — 19 constructs that keep their `rt_set_elem` writes *and* print the reference's answer, 25 AST
  shapes each keeping a read, an unknown node still walked, a truly-unread list still eliminated, and the
  lambda/`def` asymmetry pinned in both directions. The census guard reads `ast.go` itself (ADR 0317's rule).
* CLI — `integration/dict_none_bool_key_test.go::TestTheFstringFieldOfANoneKeyEntryIsAnsweredAtTheCLI` asserts the
  pair (`print(d[None])`, `print(f"{d[None]}")`) at the altitude a caller sees, against CPython, and the row left
  the filed-not-fixed table it was pinned in.
