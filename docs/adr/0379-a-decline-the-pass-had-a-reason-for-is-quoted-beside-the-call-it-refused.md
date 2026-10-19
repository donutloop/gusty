# A decline the pass had a reason for is quoted beside the call it refused

**Roadmap:** Gap R.337 (closed here) · L11.7 (the row the pass belongs to) · ADR 0345 (the pass, its
all-or-nothing withdrawal, and the account `--callables` prints) · ADR 0166 (a refusal names the missing thing,
and exit 1 is its door) · ADR 0346 (a capability refusal is speakable: a stable code, a suggestion, the rows that
own the missing half) · ADR 0343 (no process state a concurrent compile can answer with) · ADR 0372 (the cycle
that filed this row, whose refusal tables had to be written twice — one for what the compiled leg prints, one for
what the pass says) · Gap R.325 / ADR 0369 (the sibling: a diagnostic that named the compiler's own spelling
rather than the program's) · ADR 0273 (a pass that declines must not speak over the road that named what it could
not carry) · ADR 0302 (one backend, so a lost sentence has no second engine to recover it)

## Status

Accepted. Landed 2026-10-19.

## Context

`defunctionalizeCallables` is all-or-nothing (ADR 0345). One shape it cannot prove and the whole program comes back
untouched — which is the right rule for a **rewrite**: a half-specialised program is worse than none, because a call
pointed at the wrong `define` is a wrong answer at exit 0 rather than a refusal at exit 1. It is a lossy rule for a
**diagnostic**. The pass knows exactly what it refused, in a sentence it already writes down for `gustyc --callables`;
the emitter, handed the untouched tree, meets a call to a parameter and says what IT knows:

```console
$ gustyc --callables "$use_b_program"
{ "refused": ["defunctionalization declines: an entry lambda reads \"base\" from the frame its application is
   written in, and an entry is a bare symbol: … (the capture parameter of the application road, asked on the entry
   side — Gap R.336) (roadmap L11.7, ADR 0345)"] }

$ gustyc --eval "$use_b_program"
gustyc: jit: codegen: "fn" is called but is not a function this backend can build a call to: … and asking that
        question needs the value word that carries its own kind (roadmap L11.1, ADR 0166)
```

Two accounts of one compile, and the one a reader sees points at `L11.1` — the tagged value word, the row that owns
nothing in that program. The row that owns it (`Gap R.336`) is named only in the account nobody reads when a human
types a program and gets an error. This is the same class of defect as Gap R.325 (a diagnostic that named the symbol
the pass minted instead of the name the program wrote), and it has the same cost: an agent following the terminal
goes and reads the wrong ADR.

There is a second way a program comes back untouched, and it had no sentence anywhere: the pass can **plan** a
rewrite and then withdraw it, when its own invariant — no declared function name may survive in a value position —
fails on the tree it produced. `CallablesJSON` had a wording for that case; the terminal had nothing, and the two
were separate strings.

## Decision

**The decline leaves the pass as a value, beside the program it describes, and the emitter quotes it.**

```go
// pkg/lang/callable_value.go
func defunctionalizeCallablesTelling(prog *Program) (*Program, string)
```

It runs the same reading (`planDefunctionalization`) and the same writing (`writeDefunctionalization`, extracted
from `applyDefunctionalization` so there is one implementation rather than a fork), and returns the sentence in the
two cases where the pass had an opinion:

| the pass's answer | the program it returns | the sentence it returns |
|---|---|---|
| it rewrote the tree | the rewritten tree | `""` — there is nothing to explain about a rewrite |
| it had no work to do at all | the caller's tree | `""` — the emitter's sentence stands alone |
| it declined a shape it wanted to build | the caller's tree | the decline, in the account's own bytes |
| it planned one and withdrew it | the caller's tree | the withdrawal, in the account's own bytes |

**The second row is the one that makes this a test and not a print.** The pass's "nothing for me to do here" answer
is true of every first-order program in the corpus — including `t = 1` / `print(t())`, whose only problem is a call
to a name that was never a function. Pasting it onto that refusal would have said nothing about the program while
breaking the byte-for-byte sentence that refusal has carried since ADR 0166. So the decline carries a mark:

```go
type callableDecline struct {
	why    string
	noWork bool // a fact about the pass, not about the call the emitter is refusing
}
```

and the question 「did the pass want to build something and fail?」 is asked of that field rather than decided by
comparing prose at the call site. The withdrawal sentence moved into one function, `callableRewriteWithdrawn()`,
called by both faces — the document and the terminal — because two copies of a sentence drift.

**Per-module state, never a package variable.** `GenerateIRReport` takes the sentence from the same call that
produced the program and copies it onto that module's own `irGen.callablePassWord`. A package global here is the
exact fault ADR 0343 removed from the toolchain binaries and the power-predicate readers: the suite runs programs
beside each other, and a global would let one compile answer another's refusal. The witness is
`TestWhatThePassSaysTravelsWithTheProgramAndNotWithTheProcess`, which compiles a declined program, a no-work program
and a rewritten program alternately in both orders and asks every message to stay its own — a schedule-independent
claim, and the reason the answered rows are interleaved rather than merely listed.

**A quotation, under a label, in the account's own bytes.** The refusal becomes:

```
codegen: "fn" is called but is not a function this backend can build a call to: … (roadmap L11.1, ADR 0166)
 — the defunctionalisation pass (roadmap L11.7, ADR 0345) refused to rewrite this program before this call was ever
   reached, and said: <the account's `refused[0]`, verbatim>
 — the same account prints beside `gustyc --callables <src>` (roadmap Gap R.337)
```

Labelled, because two speakers in one sentence must say who is talking; verbatim, because a reader who checks the
document against the terminal must find the same bytes — the case that pins it runs `--callables` and the compiler
for one program in one test run and asks for `strings.Contains(refusal, refused[0])`, and the CLI file does the same
across the shipped tool. Appended rather than replacing the emitter's own sentence, because the emitter's sentence
is still the truth about the call: the pass's words say why nobody ever got a chance to build it.

## What this deliberately does not decide

| shape | today | owner |
|---|---|---|
| an entry lambda that reads the frame its application is written in | refused, now **with the pass's reason attached** (`Gap R.336`) | **Gap R.336** — the entry-side capture parameter and the clone identity that would carry it |
| a capture three frames up; a capture whose value is not a number | refused, reason attached | Gap R.330, Gap R.332 (L11.1's word) |
| a parameter asked to be a builtin (`(lambda f: f(3))(abs)`) | refused: `"abs" is not a declared function` — now quoted to the reader | Gap R.265's neighbour set; the builtin is not a `def` the pass can clone |
| whether a decline is ever *recoverable* rather than reported | unchanged: the pass stays all-or-nothing | ADR 0345 |

This cycle changes no decision, compiles no new shape, and pays for no missing word. Every program in the new tables
that was refused before is refused after, at exit 1; every program that answered still answers, on both legs.

## Machine path

* `--json`'s `error` carries the quotation, and the payload's `capability` object is unchanged:
  `{"code":"lower.unsupported.indirect-call","rows":["L11.1","L11.7"],…}`. The classifier picks the **longest**
  matching marker, so a quotation that happened to contain another capability's marker would silently move a code —
  the failure mode ADR 0346 was written to prevent. Every row of both new files asserts the code is still
  `lower.unsupported.indirect-call` for the *longer* message.
* `--callables <src>` is untouched: `refused[0]` is the same single sentence, and the pair (terminal, document) is
  asserted to agree in one run, at the library (`TestWhatTheCallablesAccountAndTheRefusalSpeakOneSentence`) and at
  the shipped tool (`TestADeclinedCallableRefusalAndTheCallablesAccountAgreeInOneRun`).
* Exit codes: exit 1 for these refusals on both compiled roads; exit 2 stays unreachable, asserted per row (ADR 0166),
  and the rows also forbid the strings an `llc` rejection would have left behind — a decline that got ignored and
  emitted as a half-rewrite would show up there.
* `pkg/lang`'s new exported surface is none: `defunctionalizeCallablesTelling` is package-internal, so the tool's
  contract does not grow. The record leg gained the rewritten-family sources through `tools/recmerge`, recorded from
  the reference with the compiled leg cross-checked.

## Alternatives rejected

* **A package-level `lastCallableDecline` string.** One line, and wrong: two compiles in flight, one refusal answers
  another. This repository already removed this shape twice (ADR 0343, Gap R.259/R.260). Rejected.
* **Attach the sentence to every `codegen:` refusal in the package.** 142 refusal sites, and a reader of
  `len requires an inline list/dict/set literal` has no interest in a function-value pass that never spoke. The
  uncallable-callable door is the only site where the decline is the missing information, because it is the call the
  rewrite would have built. Rejected.
* **Append unconditionally, including the no-work decline.** Fails the byte-for-byte case the row demanded, and
  turns a refusal an agent matches on into a two-speaker sentence about a pass that had nothing to say. Rejected.
* **Re-word the emitter's sentence to name `Gap R.336` itself.** The emitter does not know the row: it knows a name
  has no `define` behind it. Making the emitter recite pass-specific rows would put a second opinion about the
  program in the file that lowers it, and the two could disagree — which is what ADR 0273 forbids. Rejected.
* **Only fix the withdrawal case, whose sentence was already written for `--callables`.** The plan-time decline is the
  one every Gap R.336 program hits; the withdrawal is the rarer face, and the pair is what proves the mechanism is
  one plumbing rather than one string. Rejected.
* **Return the whole `*callableDecline` to the emitter.** The emitter needs a sentence and nothing else, and handing
  it an error value invites the next reader to branch on a type that meant something only to the pass. A string is
  the whole of the contract. Rejected.

## Tests

`pkg/lang/callable_decline_test.go` — 5 declined shapes × the emitter's words, the quotation, the citation, the
account's bytes and the capability code; 4 no-work shapes asserted **byte for byte** against a sentence written out
in the test file rather than imported from the road that emits it (edit the sentence, fail the case); the account
and the refusal compared in one run; the interleaved-order case for the per-module claim; 4 rewritten programs on the
compiled leg and the same table on the record leg (with the 「no record row walked」 guard); and the pass asked
directly that a decline returns the caller's own tree — ADR 0345's all-or-nothing rule, restated as an assertion.
`integration/callable_decline_test.go` — the same refused shapes through the shipped CLI on both compiled roads
against `python3` (each row records the reference's answer, because a shut door and a broken program are different
findings), exit 2 forbidden per row, the `--json` payload's `error` compared to `--callables` for one run, the
control rows byte-for-byte at the CLI, and the rewritten family answering the reference's bytes on both roads.
