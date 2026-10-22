# The word a clone forwards to its entry is minted; the host still reads the name the program wrote

**Roadmap:** **Gap R.375** (closed here — the word a clone forwards can collide with the body's own name) ·
**L11.7** (functions are values that compile; this is the clone side of its capture road) ·
**ADR 0395** (the entry-side forwarding this completes: it spelled the clone's parameter with the captured name and
had to decline when the body had already taken that name) ·
**ADR 0370** (the capture parameter for an APPLIED body, whose parameter keeps the program's spelling — the asymmetry
this row lives in) ·
**ADR 0372** (the union a clone's parameter list is; the verdict the supplying frame owns) ·
**ADR 0345** (a callable is a declared `define`, the pass is all-or-nothing, and §3 keeps minted spellings out of any
sentence a human reads) ·
**ADR 0385** (a self call inherits the enclosing clone's entry — the same argument applied to the forwarded word) ·
**ADR 0364** (the clone keyed by the tuple of entry symbols) ·
**Gap R.332** (a capture whose value is not one word has no word: the gate this row had to keep asking, and did not
widen) · **Gap R.330** (the verdict a forwarding frame records, keyed by parameter) ·
**Gap R.329 / ADR 0371** (an application may leave parameters out — the reason the fill could not become positional) ·
**ADR 0232** (one question, one asking place: `callableCaptureRead`) ·
**ADR 0166 / ADR 0249** (a refusal names what is missing and never the compiler's own machinery) ·
**ADR 0343** (no process state: the plan is a function over the AST)

## Status

Accepted. Landed 2026-10-22.

## Context

ADR 0395 gave a hoisted entry one capture parameter per name its body read out of the frame its **application** was
written in, grew the clone that dispatches on that entry with the union of those words, and let the existing call road
fill them — by name — from the host's own reading. It closed Gap R.336 and left one shape refused, filed as Gap R.375:

```python
def apply1(fn, v):
    base = 9                            # the higher-order body owns this name
    return fn(v)

def use(seed):
    base = 1
    return apply1(lambda x: x + base, seed)

print(use(3))                            # CPython: 4 — before this ADR: exit 1
```

> `the entry lambda of the call at line 1 reads "base", and "apply1"'s own body already owns that name: the word the
> clone would forward is a name the body reads for itself`

The decline was honest and it was also the whole family: the same sentence covered a body that binds the name, a body
that spells it as one of its own parameters, a body that owns it as a `for` target or through a comprehension target,
and any program that merely wanted two entries to read one such name. Fourteen programs the reference answers were
standing behind one spelling, and a fifteenth shape — the entry reading a frame two steps out — stayed behind
ADR 0245's lift.

Why the spelling was the problem. ADR 0395 named the clone's parameter after the captured name **so that the call road
could find it by keyword** — `lambdaCallArgs` appends `base=<the host's own reading>` and the callee's parameter list is
asked for `base`. That makes one string do three jobs in three different frames:

| the name | who reads it | whose tables know the value |
|---|---|---|
| `base`, spelled by the program | the HOST at the application | the host's print/arithmetic/type tables — the only place the value's kind lives |
| the clone's parameter | the CLONE when it calls the entry | nobody's: the word was handed down, not reached for |
| the entry's parameter | the ENTRY's body (the lambda itself) | the entry's frame, where the lambda's free read of `base` has to land |

Two of those three must keep the program's spelling: the host can only be asked about the name it wrote, and the entry's
body is literally the source text that reads it. The middle one is the compiler's, and it was the one wearing the
program's name.

## Decision

**Mint the clone's forwarded parameter, and keep the read spelled by the program.** One new field, one new question.

1. **The word is a pair.** `callableClone.forwarded` is a list of `{word, from}` instead of a list of names: `from` is
   what the host reads, `word` is the name of the clone's own parameter. The union is keyed by `from`, so two entries of
   one tuple that read the same name still share one parameter.
2. **`freshForwardWord` mints it through the doors every other generated name goes through** — `nameIsFree` (not a name
   the program mentions, not predeclared, not another clone's), `callableLexable` (a name the lexer could read back, so a
   traceback could say it), plus the one question a *parameter* has to answer that a symbol does not:
   `!callableCloneOwnsName(def, word)`. The spelling carries `callableForwardInfix` (`$cap$`), and the lexer's identifier
   rule is `[A-Za-z0-9_]`, so no source can write it. Determinism is the plan's (`fnv` over the clone and the read, plus
   a counter) — the same source, the same module (ADR 0340).
3. **The declaration says both.** `appendForwardedWords` writes, in lockstep: `Params` += `word`, `Captures` += `word`
   (so the frame still knows the position is one it was *given* — ADR 0372's verdict memo keys on that), and
   `CaptureReads` += `from`. `CaptureReads` is a new `FuncDef` field, parallel to `Captures`, whose empty entry means
   “same spelling” — which is every capture the closure road minted (ADR 0370) and every word the pass minted for a body
   that reads its own frame. `callableCaptureRead(fd, i, name)` is the only place that question is asked (ADR 0232).
4. **The fill stays where the gate is.** `lambdaCallArgs` appends `KeywordArg{Name: word, Value: <the host's reading of
   from>}`. It still asks `captureCarriesOneWord(from)` — of the **host**, where the value lives (ADR 0395 §4, unchanged)
   — and still records the verdict against the parameter that will receive it, `recordCaptureVerdict(fd, word, …)`, so
   ADR 0330's deep chain keeps keying on a name that is stable per (frame, parameter).
5. **The recursion writes its own fill.** A clone's self call is the only call of a clone whose caller is not a frame of
   the program; the frame the recursion lives in owns nothing, so the pass appends `word=<the clone's own parameter>` and
   the call road leaves a position the tree already filled alone. This is ADR 0385's rule for the entry tuple, asked one
   word taller.
6. **What still declines, declines in words.** A name the host frame does not own stays ADR 0245's nested-`def` lift
   (“an entry is a bare symbol: the word that name holds would have to arrive beside it”), and a capture whose **value**
   is not one word stays Gap R.332's. The minting neither widens nor hides either: the gate is asked of the frame that
   owns the name, so a body holding `base = 9` beside a host holding `base = "hey"` is refused, not answered with the
   number its own table would have supplied.

## What moved, measured

23 probes — the 20 of the family, the two controls that had to stay answering, and one sibling row of another gap —
each asked of CPython first, with the harness refusing to produce a count until a known-good program printed correctly
through it (ADR 0394's lesson, applied without being re-derived); then of a build of `HEAD` and a build of this commit
(the two binaries, side by side, so the “before” column is a measurement and not a memory). An agreement is “the
compiled stdout equals the reference's byte for byte at exit 0”.

| | agreeing with the reference |
|---|---|
| before (`99aaaf4`) | **2** — the two controls, and nothing of the family |
| after | **16** |
| newly agreeing | **14** |
| regressed | **0** |

What came in: the body binding the name (`4`), the name as one of the callee's own parameters (`4`), as a `for` target
(`4`), as a comprehension target (`4`), the entry reading two names while the body owns one (`13`), two entries reading
the one owned word (`11`), three entries over two owned words (`17`), the callee calling its parameter twice (`11`), a
higher-order recursion (`9`) and two hosts of one recursive callee (`923`), an omitted argument beside a forwarded word
(`4`), the body reading its own word beside the entry reading the host's (`13`), and a clone of an applied `lambda`
carrying a body capture of the same spelling (`78`).

Seven probes were measured to say something other than “fixed”, and are recorded as what they are rather than adopted
as wins: the container/text/float/void/verdict hosts keep **Gap R.332**'s refusal (exit 1, the kind the OWNER holds
named — the gate asked where the value lives, which is the claim, not the answer); an entry that reads a frame two
steps out keeps **ADR 0245**'s nested-`def` lift (exit 1, at exit 1 on the parent build too — the minting widened
nothing); the `sorted(..., key=…)`-returning `def` prints `0` identically on both builds and belongs to **Gap R.328**; and
the two controls that had to stay answering — ADR 0395's `4` and ADR 0372's `72` — did, which is what makes the before
column 2 and not 0.

## The IR, asserted

The claim a table of printed numbers cannot make, read per region (`TestWhatAClonedDefForwardsAWordTheBodyCannotAnswer`,
`TestWhatTheRecursionForwardsTheWordItWasHanded`):

```llvm
define i32 @gy_run$callable$…(i32 %p0, i32 %p1, i32 %p2)     ; n, the entry placeholder, the minted word
  %_base = alloca i32                                        ; the body's OWN base — a separate slot
  store i32 100, i32* %_base                                 ; base = 100 survives beside the forwarded word
  %… = call i32 @gy_use$lambda$…(i32 %p0, i32 %p2)           ; the entry: the value AND the minted word — both `%p`
  %… = call i32 @gy_run$callable$…(i32 %t15, i32 0, i32 %p2) ; the recursion hands the word down
; in the host:
  %… = call i32 @gy_run$callable$…(i32 3, i32 0, i32 %_base.ld1)   ; the host's own reading of the name it wrote
```

Two negative claims matter as much: no operand of the entry call is `%_base` (had the parameter kept the program's
spelling, this is where the body's value would answer for the host's), and the minted spelling appears nowhere in the
module text — parameters are positional registers, so a private name stays private (ADR 0345 §3, extended from a clone's
symbol to its minted parameters). The module is byte-identical across two compiles of one source, and no `ptrtoint`,
`inttoptr` or indirect `call` appears: the entry is still a named `define` (ADR 0345 §1).

## Agentic path

No flag changed and no schema field added. `--callables` keeps its meaning exactly: `capture_parameters` is still the
number of words the pass wrote onto the declarations it minted, which is what separates this row's clone from a
self-contained one — `1` for the collision program, `2` for three entries over two owned words, `3` where an applied
body's two captures share a declaration with a minted entry word. A plan can still tell, from the document alone, that a
program the front end accepts was rewritten rather than declined; the three refusal shapes this row does not reach are
asserted to stay at exit 1 with the missing half named, so a refusal that quietly became a wrong answer at exit 0 fails
the row. The minted spelling is forbidden from diagnostics and from the emitted IR, so no agent can mistake the pass's
private name for a name in the program.

## Alternatives rejected

1. **Keep the decline, since it is honest.** Rejected by the measurement: fourteen programs the reference answers were
   standing behind a naming choice, and ADR 0166's rule is that a refusal is the right answer to a program the backend
   cannot state — not to one whose only problem is the compiler's own parameter name.
2. **Mint the ENTRY's parameter too.** Rejected: the entry's body is the lambda's own source text, and its free read of
   `base` resolves to that parameter. Renaming it means rewriting the body's reads, which is a second rewrite of program
   text for no discrimination — and the collision never involved the entry's frame, which binds nothing but its own
   parameters.
3. **Fill the forwarded word positionally at the application**, as the filing suggested. Rejected on two counts. The
   application may leave parameters out (Gap R.329, ADR 0371): the positional list stops at the last dispatched position
   and the call road fills the rest from each parameter's own `Default`, so an appended word lands in a default's
   position. And the fill is where the one-word gate is asked and the verdict recorded with the supplying frame on the
   tables; moving the argument into the tree moves it out of that door — the same reasoning by which ADR 0395 §4 denied
   the hoisted entry a `Captures` field. The pass writes a fill in exactly one place, the recursion, where the caller is
   not a frame of the program at all.
4. **Refuse only when the body's own read would be *read*, decided by a dataflow pass.** Rejected: it is a second
   analysis for a problem an unwriteable name dissolves, and a gate that can be talked out of its answer by control
   flow is a gate that will be talked out of it again.
5. **Mangle the name with a counter (`base$1`, `base$2`).** Rejected in favour of a hash-seeded mint through the same
   doors as every other generated symbol: `base$1` is a name a program could grow into (a second clone, a second
   entry, a future generated form), and `nameIsFree` exists precisely so that a generated name is checked against the
   program rather than assumed.
6. **Add the minted name to the `--callables` document.** Rejected: ADR 0345 §3 keeps the pass's private spellings out
   of anything a human or an agent reads, and the account's job is “how many words did you write”, which the count
   already answers.

## Consequences

* A clone's parameter list is now three segments with two naming rules: the parameters the source wrote, the body's own
  captures spelled by the program (ADR 0370), and the entries' forwarded words spelled by the pass. `appendForwardedWords`
  is the only writer of the third, and it writes all three lists or none — which is what `Params`/`Captures`/`CaptureReads`
  being parallel actually means.
* `FuncDef.CaptureReads` is the second field the pass carries on a generated declaration alongside `Captures` and
  `SkipEmit`, and it is `json:"-"` for the same reason: `--emit-ast` and `gusty fmt` speak the program, not the rewrite
  (ADR 0320).
* The `already owns that name` decline is gone from the pass, from `--callables`, and from three test tables, where it
  was a pin. Deleting a pin rather than rewriting it is the house rule (Gap R.337's residue), and its absence is now the
  assertion: the account must say `rewritten` and refuse nothing.
* The entry road, the application road and the hook road continue to ask one door (`callableLambdaCaptures`) for what a
  body reads out of its frame; this row changes only what happens to the answer on the CLONE side, and the two gates
  that decide safety — “can this value cross in one word?” and “whose tables answer for it?” — are unchanged and are
  asserted to still bite at the new position.
* What the row leaves standing is measured, not swallowed: the nested-`def` lift (two frames out — **Gap R.347**
  owns a capture that reaches past the frame its application is written in) and **Gap R.332**'s un-wordable values,
  each asserted at the CLI at exit 1 with the kind or the missing half named.
