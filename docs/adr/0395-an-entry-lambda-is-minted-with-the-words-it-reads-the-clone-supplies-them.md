# An entry lambda is minted with the words it reads: the clone supplies them

**Roadmap:** **Gap R.336** (closed here — an entry lambda that reads the frame its application is written in) ·
**L11.7** (functions are values that compile; this is the entry side of its capture road) ·
**ADR 0345** (defunctionalisation: an entry is a `define` symbol, the pass is all-or-nothing, and a minted
name is the only callable this backend has) ·
**ADR 0370** (the capture parameter, minted for an APPLIED lambda's body — this ADR asks the same door on the
entry side) · **ADR 0372** (the union a clone's parameter list is, and the verdict the supplying frame owns) ·
**ADR 0364** (the clone of the callee, keyed by the TUPLE of entry symbols — the reason no re-keying was needed) ·
**ADR 0385** (a self-call inherits the enclosing clone's entry) ·
**Gap R.332** (a capture whose value is not a number has no word — the gate this row inherits, asked at a new
position) · **Gap R.334** (the body-side twin, paid by ADR 0372) · **Gap R.265 / Gap R.276** (the applied-lambda
road this one joins) ·
**Gap R.255** (the `key=` door, which hoists a hook lambda by a second road and is left behind — filed as
**Gap R.376**) · **Gap R.337** (a decline the pass has a reason for must reach the reader: the two declines
this row keeps are visible in `--callables` and in the refusal) ·
**ADR 0166 / ADR 0273** (a refusal the pass states is owed; a value carried as a bare word is not) ·
**ADR 0343** (no toolchain state: the plan carries the program it read, it does not borrow a generator)

## Status

Accepted. Landed 2026-10-22.

## Context

Three programs, all refused, all answered by the reference. They were filed together as Gap R.336 on
2026-10-19, while ADR 0372 was being written — the same cycle that paid the *body* side of the same question.

```python
def apply1(fn, v):
    return fn(v)

def use(seed):
    base = 1
    return apply1(lambda x: x + base, seed)

print(use(3))          # CPython: 4   — backend: exit 1
```

and the same with the callee calling its parameter twice (`def twice(fn, seed): return fn(fn(seed))`, the
reference's `11`), and the same with the entry reading the frame's `for` target while the body reads a local
(`total + (lambda fn: fn(2) + shift)(lambda n: n * step)`, the reference's `72`). Each exited 1 with the
emitter's generic sentence — `"fn" is called but is not a function this backend can build a call to` —
stamped by ADR 0166 and carrying nothing about the program in front of the reader. The pass's real reason was
true and invisible: it had declined, and only `gustyc --callables` said so.

Why the body side had been easy and this side was not. ADR 0370 gave an **applied** lambda that reads its
enclosing frame one parameter per free name, and the call road fills those by name from the caller's own
reading (`lambdaCallArgs`). The `def f(x): return (lambda h: h(1) + x)(lambda q: q * 10)` case is that road, and
it answers `12`. Here the lambda that reads the frame is not the body the application applies — it is the
**argument** a higher-order `def` calls:

```gusty
apply1(lambda x: x + base, seed)
       ^^^^^^^^^^^^^^^^^^ an entry: hoisted to a `define`, called by SYMBOL by apply1's clone
```

An entry is a symbol (ADR 0345 §1). A symbol carries no word beside itself, so `base` had nowhere to ride, and
the pass — which is all-or-nothing, by the same ADR — handed the whole program to the ordinary road.

The obvious objection is the one the filing named and this ADR had to answer first: the callee's clone is shared
by every application that asks for its entry tuple, so what if two applications of `apply1` disagree about what
their entries read? The answer turned out to be the clone key itself. `callableClone.key` is the tuple of entry
**symbols**, one per called parameter, and the clone's body is written from that tuple (`callee.Fn = cl.key[slot]`).
Two applications with different entries are already different clones. The captures of an entry are a property of
that entry's minted symbol, so they ride the tuple without changing it — no re-keying, no second identity axis,
and no "the clone whose applications disagree" case to invent a rule for.

## Decision

**An entry lambda is minted with capture parameters, and the clone that dispatches on it supplies the words.**

1. **The hoisted entry gains parameters.** `resolveEntry`'s `*Lambda` arm asks the door ADR 0370 asks —
   `callableLambdaCaptures(enc, prog, lam)` — instead of asking `callableLambdaFrameRead` and declining. A
   capturable read becomes a trailing `*Param` under the name the program wrote, beside the lambda's own
   parameters, and `plan.entryCaptures[symbol]` records it. The symbol's `arity` becomes the whole parameter
   count, because that is how many words the `define` now takes.
2. **The clone grows the union.** At clone-minting time, `callableClone.forwarded` is the union of the capture
   words of every entry named in that clone's key, in tuple order — ADR 0372's union rule, applied to the
   entries instead of the body. At emission the clone's `Params` gain those names and `clone.Captures` is
   APPENDED (never replaced: a clone of an applied lambda already carries the words its own body reads, and the
   two lists are the entries' supply and the body's).
3. **The host fills them the ordinary way.** Because the clone carries `Captures`, the existing call road
   (`lambdaCallArgs`) appends `base=<the host's own reading>` at the application. No second fill road: this is
   the mechanism ADR 0370 built, asked of a different callee. The clone's parameter is named for the captured
   name precisely so that fill can find it by name.
4. **The clone hands the word on, positionally.** Where the pass already rewrites the body's call of the called
   parameter (`callee.Fn = cl.key[slot]`), it now appends one positional argument per capture of that entry:
   `@gy_use$lambda$…(i32 %p1, i32 %p2)`. Positionally, and with **no `Captures` field on the hoisted entry**, on
   purpose: `Captures` is the instruction "fill this by name from the caller's reading of that name", and the
   caller here is a frame the pass itself built, whose reading of `base` is the parameter it was handed. A value
   handed down a frame is not a value the frame reached for, and the gate that asks the value tables about a
   reached-for name (`captureCarriesOneWord`, Gap R.332) belongs at the host, where the value actually lives.
5. **Two shapes still decline, and say why.** A forwarded word is a PARAMETER of the clone; if the higher-order
   body already owns that name — as a parameter or as something its own body binds — the clone would have two
   slots under one spelling and the two reads would answer each other, so the pass names the name and declines
   (**Gap R.375**). And a read the door cannot supply (a name the host frame does not own, the nested-`def` lift's)
   keeps the old refusal, with the name in it. A capture whose VALUE is a container or a text still reaches
   codegen's gate and refuses with Gap R.332's sentence — this row neither widens nor hides that; it asks the
   same gate at a new position, and the gate holds.

## What moved, measured

21 probes of the family, each asked of CPython first, then of a build of `HEAD` and a build of this commit. An
agreement is "the compiled answer is the reference's stdout at exit 0, or both refuse".

| | agreeing with the reference |
|---|---|
| before (`e101ec5`) | **4** |
| after | **14** |
| newly agreeing | **10** |
| regressed | **0** |

The three filed programs print `4`, `11` and `72`. The seven new spellings that came with them: a capture that
is the host's own parameter (`use(3, 4)` → `12`), two entries of one callee reading different names of the same
frame (`35`), a capturing entry and a closed entry of the same callee (`8`), the entry read inside a `for` where
the host's word changes under one minted symbol (`36`), a name the module binds, which mints no word (`10`), a
capturing entry beside a second entry of a second callee, and the callee that calls its parameter twice.

Two probes were measured on the parent to decide whose they are, and neither is this row's:

* the collision decline above — new in this commit, and stated rather than silently wrong (the parent refused
  everything here with the generic sentence);
* `sorted([3, 1, 2], key=lambda x: 0 - x + base)` refuses with the `key=` door's own sentence — identical on the
  parent, so it is filed as **Gap R.376** and not absorbed.

## The IR, asserted

`TestWhatAClonedDefForwardsItsEntryCaptures` reads the module, per region:

```llvm
define i32 @gy_apply1$callable$…(i32 %p0, i32 %p1, i32 %p2)   ; 2 params in the source, 3 here
  %… = call i32 @gy_use$lambda$…(i32 %p1, i32 %p2)            ; the entry: the value AND the forwarded word
; in the host:
  %… = call i32 @gy_apply1$callable$…(i32 0, i32 %p0, i32 %_base.ld1)   ; the host's own reading of `base`
```

`%p0` is the placeholder the entry slot keeps (ADR 0385's rule: the clone reads nothing there), `%p1` is `v`,
`%p2` is the forwarded word. The module is byte-identical across two compiles of the same source (the pass
mints names by counter under a deterministic plan order), and contains no `ptrtoint`, no `inttoptr`, and no
indirect `call` — the entry is still a named `define`, exactly as ADR 0345 §1 requires of every callable.

`gustyc --callables` says `capture_parameters: 1` for the row program and `2` for the program where the body
also reads a frame local (`shift` from ADR 0372 and `step` from this row in one body), which is the machine-
readable half of "both lists, in one body".

## Agentic path

No flag changed, no schema field added. `--callables` gains meaning rather than syntax: `capture_parameters`
now counts words written for entries as well as for bodies, which is the field an agent uses to tell "this
program's higher-order call was rewritten and carries its captures" from "this is ADR 0370's self-contained
body". The two declines this row keeps are readable twice over — in `refused[]` from `--callables` (Gap R.337's
rule) and in the codegen refusal that quotes the pass's own sentence — so a plan can tell a decline it can fix
by renaming a local from a shape the backend cannot state at all.

## Alternatives rejected

1. **Key the clone by the tuple of (entry symbol, captures).** The filing suggested the clone's identity needed
   a second axis. It does not: the captures belong to the minted symbol, and the tuple already names symbols.
   Re-keying would have doubled the clone count for no new discrimination and broken `callableCloneNamesDiffer`.
2. **Give the hoisted entry a `Captures` field and let codegen fill it by name inside the clone.** Rejected: that
   makes the *clone* the frame that "reaches for" `base`, and the gate that asks whether a name carries one word
   (`captureCarriesOneWord`) would be asked of a minted parameter with no facts of its own — the same question
   asked of the wrong frame, which is how Gap R.330's verdict-forwarding rule got written. The verdict belongs
   where the value is: the host.
3. **Pass the value instead of the word** (constant-fold `base = 1` into the entry body). Rejected: the host's
   word changes under one minted symbol (the `for` probe prints `36`), and a folded copy is a second answer to a
   name the program has only one binding of.
4. **Refuse everything on the entry side, but say so properly now that Gap R.337 lets a decline reach the
   reader.** Rejected by the measurement: the reference answers all three filed programs, the print and truth
   doors already answer the same values elsewhere, and the machinery was one existing door away. A refusal is
   honest; it is not free.
5. **Widen `callableLambdaFrameRead` to answer "which names", returning a list.** Rejected in favour of reusing
   `callableLambdaCaptures`, which already owns the list, the ordering, the module-name rule and the "the frame
   must own it" gate. The two predicates now ask one door, and `callableLambdaFrameRead` survives only as the
   diagnostic that names the first blocking name.

## Consequences

* The entry road, the applied-lambda road and the body-capture road now ask **one** door
  (`callableLambdaCaptures`) for one question — "what does this lambda read out of the frame it was written
  in?" — with ADR 0372's union applied twice (once to a body's captures, once to a clone's entries' — and the
  two lists append, which is why the code that appends them says so loudly).
* `pairCallInfo`-style statelessness is preserved: the plan carries `prog` and asks `callableLambdaCaptures`
  with it. Nothing is borrowed from a generator, per ADR 0343.
* Two new rows are in the queue with today's bytes beside the reference's: **Gap R.375** (a forwarded word
  whose name the body owns) and **Gap R.376** (the `key=` door, which hoists a hook lambda by a second road and
  never asks this one). **Gap R.328** owns the `0` that a returned keyed-sort result still prints; measured on
  the parent, not touched here.
* The three refusal rows this closes were deleted from `pkg/lang/callable_decline_test.go`,
  `pkg/lang/lambda_applied_test.go` and both CLI tables rather than rewritten; four of the family's programs
  are on the retired engine's record, recorded from the reference with the compiled leg cross-checked, and
  `TestTheAppliedLambdaFamilyIsOnTheRecord` fails if one goes missing.
