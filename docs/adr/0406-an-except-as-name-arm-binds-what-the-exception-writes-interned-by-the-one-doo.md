# An `except … as name:` arm binds what the exception WRITES, interned by the one door every text uses

**Roadmap:** **Gap R.113** (closed here — `except <Type> as e:` does not parse) · **Gap R.395** / **Gap R.396** /
**Gap R.397** (the three rows this cycle measures and does not own) · **L11.1** (the tagged value word, which is what
would carry the exception OBJECT this arm cannot) · **ADR 0213** (the arms dispatch in source order; an accepting
edge owns its arm) · **ADR 0224** / **ADR 0229** (a text is an `@str_tab` index, and the print road and the operation
road ask one question about what a text is) · **ADR 0258** (the `str`/`repr` pair is one table, one question) ·
**ADR 0172** / **ADR 0267** / **ADR 0367** (the latest binding decides what a name is; the records are written by the
binding road, not by a copy of it) · **ADR 0228** (a name the program writes owns a flag, so a read that never saw the
write raises instead of reading an uninitialised slot) · **ADR 0273** (a verdict is a pure function of the program, so
the family needs control rows) · **ADR 0166** (exit 2 is a compiler bug; a refusal at exit 1 beats a wrong answer at
exit 0) · **ADR 0302** / **ADR 0309** (the newly answering programs are recorded from the reference, never
self-certified) · **Gap R.2** (a module that calls a runtime entry defines it)

## Status

Accepted. Landed 2026-10-23. Closes **Gap R.113**, and files **Gap R.395** (the exception object the arm still does
not have), **Gap R.396** (`type` is a reserved word, so `type(x)` cannot be called anywhere) and **Gap R.397**
(`except (A, B):` does not parse either).

## Context

The most common exception idiom in the language the reference defines could not be written:

```gusty
try:
    1/0
except ZeroDivisionError as err:
    print(err)          # CPython: division by zero
```

`gustyc: parse error at 3:26: expected ":"`, then `parse error at 4:5: unexpected token`, **exit 1**, on every path —
`--eval`, `--file`, `--repl` — because `parseTry` read the optional type name and then asked for the colon
(`pkg/lang/parser.go`). The arms themselves were fine: `except ZeroDivisionError:` catches, `except Exception:` and a
bare `except:` catch everything, and the arms dispatch in source order (**ADR 0213**). Nothing could *name* what was
caught, so the message a raise carried was reachable only by letting the program die and reading the traceback.

Gap R.113 had carried no test since 2026-10-02 precisely because closing it owed a program that runs, and the shape of
that program is the whole decision: the reference hands the arm an exception **OBJECT**, and this backend's value is
**one word wide**. There is no object to bind. The row's DoD said "bind the caught exception's object"; this ADR
binds, instead, the half of that object every one of the family's programs actually reads.

## Decision

**1. The clause parses, and the AST mirrors the source.** `ExceptClause` gains `As string` (`json:"as,omitempty"`),
the parser reads `as <ident>` between the type name and the colon and refuses `except T as:` with a sentence that names
what is missing. `fmt.go` writes the clause back with its name (a formatter that dropped it would print a *different
program*), `lsp.go` reports the arm's name as a definition, and `schema.go`'s `exceptClause` definition declares `as`,
so `--emit-ast` shows the binding an agent would otherwise have to infer from the body.

**2. Every raise now writes two globals, not one.** `setExn` already stored `"<Type>: <message>"` in `@exn_msg` for the
uncaught report. It stores the **bare message** in `@exn_arg` as well — and stores it *beside* the report rather than
deriving it at read time, because the message may itself contain `": "`: `raise ValueError("a: b")` says `str(err)` is
`a: b`, and stripping a prefix by finding the first colon would take letters out of the program's own words. `@exn_arg`
is never null: a raise that carried nothing carries the empty text, which is what the reference's `str(err)` is for
`raise ValueError`.

**3. The arm binds through the ordinary binding road, with a value the parser never builds.** `tryStmt`'s accepting
edge — the same place ADR 0404 moved the flag clear to — calls `bindCaughtName`, which hands `stmtAssign` an
`AssignStmt` whose value is a new node, `ExnMessage`. The slot, ADR 0172's latest-binding records, the written-flag of
ADR 0228 and GC rooting are therefore written by the one road that knows how to write them, not by a second copy of
that road beside the arm; `value(ExnMessage)` lowers to a load of `@exn_arg` and one call to
`@rt_str_intern2(i8* %raw, i8* %repr)` — the interning door every literal, every `str()` and every formatted text
already goes through (**ADR 0224**). Interning is content-addressed, so the name compares by index against a literal
and behaves like every other runtime-built text downstream.

**4. The binding writes `caughtVars`, deliberately NOT `internedVars`.** This is the split the reference itself draws,
and the reason the row is honest. An exception PRINTS as its message and is not a string:

| question | asked by | answer for the arm's name | why |
|---|---|---|---|
| `printsAsInternedStr` | the print road, `str`/`repr`, the f-string renderer | **yes** | CPython prints `str(err)`, which is the message |
| `exprIsString` | `len`, the text methods, the arithmetic doors | **no** | CPython raises `TypeError`/`AttributeError` there |

One record for both would have had to be wrong somewhere: `internedVars` alone prints `16` for `len(err)` at exit 0,
and neither alone prints `division by zero` for `print(err)`. Where the operation road would have answered with the
message dressed up as the object, the door now **refuses in words** naming the reference's raise — `len(err)` says
`not a text to measure`, `err.upper()` says `not a text to call a method on`, `err == "division by zero"` says
`not a text to compare against` — and each is registered in the capability inventory (`lower.unsupported.len-of-a-caught-exception`,
`…method-of-a-caught-exception`, `…compare-a-caught-exception`) with a sample that really refuses, so an agent branching
on `--json`'s `capability.code` sees the three shapes as three codes rather than as one unknown failure.

**5. A pre-pass asks the same question the binder asks.** `return err` out of a function is a function that returns a
*text*, and the call site has to be told before it prints the answer: `exceptAsNamesIn` walks a body the way the other
binding censuses do and feeds `returnsStringExpr`'s text-parameter set, so `print(f())` prints the message and not the
intern-table position under it.

**6. The front end must see the binding too.** The semantic analyzer's `bodyBoundNames`, `collectModuleBindings` and
the arm's definite-assignment path, the written-flag census in `boundflags.go`, the module's slot census in
`moduleBindingNames`, the rebinding census in `params.go` and escape analysis's `nonListAssign` all learn that the arm
binds its name — the `with … as` precedent for every one of them. Without the last of these the arm's own read died with
`undefined name "err"`, which is how the cycle was measured rather than assumed.

## Alternatives rejected

- **Bind the object.** There is no object to bind: the value word is one `i32`, and an exception needs a class, an
  `args` tuple and a renderer. That is **Gap R.395**'s answer and **L11.1**'s word, not this ADR's.
- **Bind the exception code, or the `"<Type>: <message>"` report.** Both are wrong at exit 0 for the family's most
  common line: the reference's `str(err)` is the message alone, and `print(err)` follows it. The report-with-caption
  variant was measured — `raise ValueError("a: b")` prints `ValueError: a: b`, which CPython never does.
- **Bind an ordinary `internedVars` text.** Cheaper by one field, and it buys four wrong answers: `len(err)` → `16`,
  `err.upper()` → `DIVISION BY ZERO`, `err == "division by zero"` → `True`, all at exit 0. **ADR 0166** prices those.
- **Strip the caption at read time.** A `": "` inside the program's own message would be cut by a rule looking for the
  first colon. Two stores at the raise site cost nothing and cannot disagree.
- **Refuse the clause until the object exists.** The row is a program the reference runs; a parse error at exit 1 is not
  a refusal of an unsupported shape, it is the toolchain declining to read the language.
- **Desugar in the parser.** It would put an assignment the source never wrote into the AST every agent reads, and the
  formatter would print both it and the clause. `As` is the source's shape; the assignment is the lowering's.

## Consequences

- 38 sources joined the record, written from CPython by `tools/recmerge` and cross-checked against the compiled leg
  (8753 → 8791). The corpus gained `integration/programs/except_as_name.gy`, asserted line-for-line against CPython and
  registered `OracleMatch` in the matrix (231 → 232 rows, `oracle_match` 188 → 189).
- The `@exn_arg` global is declared in the raise block, gated on the same `raiseUsed` flag every raise sets, and the
  arm's intern call pulls in the heap block — the pairing rule that keeps **Gap R.2**'s class of "calls without a
  definition" from returning.
- A program that names what it caught and then reads the name AFTER the arm prints the message where the reference
  raises `NameError` (it deletes the target at the arm's end). Pinned as owed, not forgiven — **Gap R.395**.
- The two IR-shape guards (the binding lives in the arm's own block; the control arm with no `as` interns nothing) sit
  beside the answer table, because a later "optimisation" that hoists the binding above the dispatch is the failure this
  shape is one edit away from (**ADR 0273**'s control rows, **ADR 0374**'s arm-is-a-block rule read at this door).
