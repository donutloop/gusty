# ADR 0359 — An interpolation is a text value, not only a printing instruction

Status: accepted
Date: 2026-10-17
Amends: [ADR 0299](0299-an-f-strings-format-spec-is-part-of-the-program-not-a-suffix-to-thr.md) (whose spec engine is compile-time-only, and stays so — see Gap R.308), [ADR 0303](0303-str-and-repr-answer-for-a-value-whose-kind-the-run-time-dec.md) (the one printer this road now shares with the field road)
Depends on: [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md), [ADR 0229](0229-a-string-is-an-index-into-a-table-the-runtime-can-add-to.md), [ADR 0230](0230-building-a-string-at-run-time-is-a-buffer-an-intern-and-the-same-index.md), [ADR 0232](0232-a-container-word-means-its-payload-and-its-tag.md), [ADR 0238](0238-a-float-in-a-container-is-a-box-handle-and-a-tag.md), [ADR 0258](0258-str-and-repr-are-one-printer-pointed-at-two-sinks.md), [ADR 0299](0299-an-f-strings-format-spec-is-part-of-the-program-not-a-suffix-to-thr.md), [ADR 0311](0311-a-list-whose-elements-the-compiler-cannot-label-carries-its-own-p.md), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md), [ADR 0249](0249-a-refusal-names-what-is-missing-and-never-blames-a-construct-the.md), [ADR 0339](0339-a-loop-asks-its-iterable-what-it-is-and-refuses-what-nothing-can.md), [ADR 0356](0356-an-f-string-field-is-a-print-in-disguise-one-subscript-door-one-printer-and-a-re.md)
Roadmap rows: L11.1 (advanced — the interpolation joins the tagged-value positions), Gap R.146 (the value half of the one-word position, paid for a text), Gap R.173 (the same fold rule for a constant-test ternary), Gap R.192 (quoting stays the printer's act), Gap R.246/Gap R.252 (a numeric claim is the loop's, and ends with the loop), Gap R.308 (filed: a format spec over a value the compiler cannot read), Gap R.33 (`str * int`, still owed)

## The gap, as measured

The print road has honoured an interpolation for as long as this backend has had one. The same eight characters
standing one position to the left was not honoured, it was refused:

```gusty
n = 3
print(f"x{n}")        # x3      — has always answered
s = f"x{n}"           # gustyc: codegen: f-string requires a constant expression (AOT backend)
print(f"a{n}" + "!")  # the same sentence
print(f"v={n}".upper())
print(len(f"a{n}b"))
def f():
    return f"x{3}"    # and again, at the return
```

`--eval`, `--file`, `--emit-llvm`, a container slot, a dict key, a `for` iterable, a string parameter — every
position that lowers an expression to a word asked `value()`, and `value()` had no arm for `*FString`. CPython
answers all six. The refusal was honest (exit 1, and the shape named) but it was the whole surface of the most
used text construct in the language standing one token away from the one position that worked.

Two more shapes were worse than refused, because the program ran:

```gusty
print(f"v={None}")    # wrote v=0    at exit 0 — the void has no digits
xs = [1, 2]
print(f"{xs}")        # wrote 0      at exit 0 — a heap handle is not a list
```

The print road builds **one** printf format string and can hand it an `i32` or an `i8*`. Neither of those renders
`None`, and neither renders a container's slots; the handle went out as `%d`. That is ADR 0166's forbidden class
arriving as ordinary output rather than as an `llc` rejection — which is where Gap R.192's `@.fmtN` bug and Gap
R.186's unformatted numbers came from too, and why this file's rule is "ask the printer, never the format string".

## The decision

**1. The value road answers an `@str_tab` index, and only the value road.** `(*irGen).fstringValue`
(`pkg/lang/fstring_value.go`) lowers an interpolation to the same word a `*StrLit` answers: the parts are built by
`rt_str_cat` — the builder `s + t` already used — and the whole is one interned index. `value()`, `stringVal()`,
`stringConst()`, `heapElemKind`, `printsAsInternedStr` and the string-parameter call gate all now read an
interpolation as a text, so everything downstream (`+`, `==`, `<`, `len`, `in`, a subscript, a slice, a
`for`-iterable, a container slot, a dict key, `return`, a method) takes it **without knowing what built it**. That
is the whole design: no new representation, no second string, no new printer — ADR 0229's index and ADR 0230's
builder, one more producer.

**2. A field is asked of the door `str()` and `repr()` ask.** `fstringFieldReg` hands the field's expression to
`renderPair` with the quote flag the field's conversion chose, and hands containers to `rt_str_of_container`. ADR
0258 and ADR 0303 established that `str(x)`, `repr(x)` and `print(x)` are one printer with three sinks; the field
was the fourth sink that had its own idea. It is now the same idea: `f"{7.0}"`, `str(7.0)` and `print(7.0)` cannot
disagree about `7.0`, and no field can answer a box handle or a table index because the road that renders it is the
road that reads the tag.

**3. One fold, asked by three roads.** An interpolation the AST already states is a constant text — `f"a{1}b"` —
folds to a `*StrLit`-shaped answer. `fstringConstTextOf` is that function, and it is asked by `value()`, by
`stringVal()` and by the container literal's element-kind census through `stringConst()`. Asking it twice is a
wrong-answer machine: the census and the builder disagreed about whether `[f"a1", 1]`'s elements were text, the
object was labelled "every member is interned text", the integer `1` was printed through `@str_tab`, and the
program died with **SIGSEGV (exit 2)** — the code a compiler bug owns. The same rule caught a second shape during
this cycle: `heapListFrom`'s mixed-kind census was a *pre-scan* reading each element's spelling through
`stringConstOf`, and an interpolation has no spelling to read, so `[f"a{n}", 1]` printed `['a3', '3']`. The
pre-scan is gone; the census is now taken from the words the builder's own loop writes. A gate and a door that ask
different questions is this project's recurring bug (ADR 0232, ADR 0249, ADR 0332) and this row is one more
instance of it.

**4. Quoting is the printer's act.** `f"{'a'!r}"` is *not* folded, because folding it in Go would put a quote
character in the program's text chosen by the compiler rather than by the renderer — which is exactly how Gap
R.192 measured `print(f"{'a'!r}")` answering `"a"` where CPython answers `'a'`. A field the fold cannot state goes
to the renderer, and the renderer already knows Python's quote preference and its swap rule.

**5. A container is asked before it is numbered.** `fstringFieldIsContainer` is consulted *ahead* of the numeric
arm, because the numeric door's last arm will "convince itself" of anything it can lower — and a container lowered
through it answers its heap handle. The three witnesses are the ones the rest of the backend already uses for
"this expression names a container": the literal shape, the container records the assignment roads kept, and the
whole-program answer table (`containerAnswerOf`).

**6. A loop vouches for its own counter, and stops at the loop's end.** `f"x{i}"` inside `for i in range(2)` needs
the counter's digits, and a loop variable has no *binding* for the `str` door to read — the loop wrote the slot, not
an assignment. `irGen.digitLoopVars` is the loop's claim, made per loop (the counter loop, the unrolled literal
loop, the container loop whose recorded element kind is integer), taken back when the loop ends, and voided for any
name the program also assigns to:

```gusty
for i in range(2):
    print(f"x{i}")     # x0, x1           — the loop's own claim
print(f"x{i}")        # refused: no claim survives the loop
for i in range(2):
    i = i * 100
    print(f"x{i}")    # refused: the value is not the loop's alone
```

That last refusal is deliberate. A road that printed digits for a name some other statement wrote would be the
ADR 0172 latest-binding rule wearing the loop's clothes, and it is the same class as a name printing its `@str_tab`
index because a *different* loop interned it (Gap I.2). ADR 0339's per-loop question and Gap R.252's
"the claim ends with the loop" are the two rules this arm obeys.

**7. The print road stops guessing when the value road can answer.** `fstringNeedsRenderer` asks whether one
printf can state every field; for the void and for a container it cannot, so the whole interpolation is built by
the value road and handed to `rt_print_str`. That is what makes `print(f"{xs}")` and `t = f"{xs}"; print(t)` one
answer rather than one answer and one handle — the pair of forms a caller writes freely between.

**8. What still refuses, refuses in words, with a row.** `formatFieldConst` is ADR 0299's engine and reads only
what the AST states, so a spec over a run-time value (`f"{n:>6}"`, `f"{v:.2f}"`, `f"{10:#x}"`) is refused at
compile time with the same sentence it has always had; the row it belongs to is now **Gap R.308**, filed with the
ledger entry that proves it. String methods whose run-time road has not been built (`replace`, `split`, `center`,
`startswith`, `rstrip`) and `f"x{n}" * 2` keep their own sentences, and their rows are Gap R.33 and the method
roads — none of them is this ADR's silence.

## Why the fold is a function and not a pass

The tempting shape was a constant-propagation pass over f-strings, or a second `if isConstant` ladder inside
`value()`. Both put the same question in two places, and the two places drift — which is precisely the defect this
cycle shipped twice before it was fixed (`stringConst` not folding: SIGSEGV; the census pre-scan not folding:
`['a3', '3']`). `fstringConstTextOf` takes no compiler state, so "this interpolation is constant" is a fact about
the source rather than about a pass order, and every road can ask it at the moment it needs the answer.

## The machine path

Nothing new is exposed; nothing existing moved. `--emit-llvm` for a run-time interpolation shows
`@rt_str_intern2`/`@rt_str_of_int`/`@rt_str_cat` and no renderer call, and a field shows `@rt_str_of_value` or
`@rt_str_of_container` — asserted by IR rows, so a second renderer appearing beside the module's one printer is a
red case, and so is a constant interpolation that starts interning at run time. `--capabilities` still names the
refused shapes (`lower.unsupported.*`) with their roadmap rows; the f-string-as-value sample was moved off the
shapes that now answer and onto the format spec, which Gap R.308 owns. `--json` carries the refusal sentences
verbatim. Exit codes are unchanged: 0 for an answer, 1 for a refusal, and **exit 2 stays unreachable** — two of
the shapes in §1 used to be capable of it.

## Alternatives rejected

* **Keep f-strings print-only and add per-position arms.** `len(f…)`, `==`, a container slot and a string
  parameter each get an arm, each a chance to disagree with the others. It is six small diffs that reconstruct this
  ADR's bug report one position at a time. Rejected for the reason ADR 0303 collapsed `str`/`repr`/`print`: one
  producer, many sinks.
* **Give an interpolation its own renderer.** The field's bytes would then be chosen by code that is not the
  printer, in the same commit that decided the field goes *to* the printer. Every wrong answer in §1 is an
  un-rendered word: `0` for `None`, `0` for a list, `'3'` for an integer.
* **Fold `!r` of a text field in Go.** Passes the obvious test (`f"{'a'!r}"` → `'a'`) only if the Go helper
  reproduces Python's quote preference and its swap rule; Gap R.192 is what happens when it half-does.
* **Answer a format spec by rendering the pair and padding it here.** It would close Gap R.308 today, and it would
  put a second formatter beside ADR 0299's engine — the one piece of code whose whole purpose is that a spec has
  one interpretation. `f"{3.5:.2f}"` answering `3.50` here and `3.5` at the print door is the Gap R.186 defect
  re-made with better manners. The spec belongs in the runtime, asked through the tagged pair, in its own row.
* **Let the loop keep its numeric claim after the loop.** One line, and `print(f"x{i}")` after the loop prints the
  digits of whatever the slot holds next. Gap R.252 exists because this was already tried once, elsewhere.
* **Trust the `@str_tab` index the fold produced for a container element.** The census, not the value, decides how
  a slot is stored; a text that is a *runtime* interning is a text to the builder and not a text to a
  spelling-reading pre-scan, and the disagreement is a segfault rather than a diagnostic.

## What was witnessed

* Record leg — `pkg/lang/fstring_value_test.go`: 59 answer rows run against
  `pkg/lang/testdata/interpreter-golden.json` (`runGoldenStdout`/`evalGolden`) and a **missing record fails the
  case**; 58 of the 59 sources had never been asked before this cycle and were recorded from the reference by
  `tools/recmerge`, which refuses to write an entry the compiled leg will not agree to (ADR 0302's rule, asked of
  every one of them). The record holds 7119 sources, up from 7061.
* Reference leg — the same programs through `python3` in `integration/fstring_value_test.go`, every expectation
  read off the interpreter rather than from this file; plus 5 CLI rows moved out of the pinned-refusal table in
  `integration/pair_fstring_test.go` into the CPython-agreement table, and 2 container-answer sources added to
  `integration/container_return_test.go`'s answering half (`print(f"{build()}")`, and the same bound first).
* IR — the fold emits `c"a1b\00"` and one printf and calls no renderer; a run-time interpolation emits
  `@rt_str_cat` and no `@rt_str_of_container`; the mixed literal promotes itself to per-slot tags
  (`@rt_mark_estr(i32 %h, i32 8)`); the loop's digits go through `@rt_str_of_int`.
* Refusals — 5 rows that must keep refusing (spec over a name, spec over a float, `replace`, `split`, `*`), each at
  exit 1, each forbidden from leaking `%!s(`, `*lang.`, `__cans`, and each forbidden from blaming a construct the
  program does not contain (ADR 0249); plus the two loop-claim refusals, which fail the day the claim outlives its
  loop or survives an assignment to the loop variable.
* Ledger — three rows went paid (`f"hello"`, `f"plain"`, `f"a{1}b"` as values) and one arrived (Gap R.308), the
  latter surfaced *by* paying the others: `TestEvalFString` had been skipping on its first line since the engine
  was retired, and with `f"hello"` answered the rest of the test ran for the first time. The ledger is 333 rows.
