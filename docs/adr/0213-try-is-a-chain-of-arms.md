# ADR 0213 — a `try` is a chain of arms, and an unmatched exception is not a success

Status: accepted (roadmap Gap R.20; supersedes the reading recorded as Gap R.21)

## Context

Five shapes, all compiled wrong, all silently. Measured against the interpreter and CPython,
which were both right:

| source (abridged)                                   | interpreter / CPython | compiled         |
|-----------------------------------------------------|-----------------------|------------------|
| `except ValueError:` / `except IndexError:` (match) | `right`               | nothing, exit 0  |
| three arms, match the third                         | `c`                   | nothing, exit 0  |
| `except KeyError:` / `except:` (bare)               | `bare`                | nothing, exit 0  |
| nested `try`, outer arm matches                     | `outer`               | nothing, exit 0  |
| no arm matches at all                               | traceback, exit 3/1   | **nothing, exit 0** |

The lowering explains all five in one line of `tryStmt`:

```go
if len(ts.Excepts) > 0 {
	ec := ts.Excepts[0]            // only the first arm is ever emitted
	...
	if specific {
		br i1 %matches, label %arm, label %finally   // a mismatch jumps past *every* arm
	}
	...
}
```

and then the flag is cleared at the top of the handler:

```go
b.WriteString("  store i32 0, i32* @exn_flag\n")
```

So the handler said "I have this exception" before deciding whether it matched anything, and
`finally` — which is where a mismatch fell through to — was the ordinary continuation. Not one of
these programs reported an error; every one of them exited 0. A user with a two-arm handler had a
handler that did not exist, and a program whose exception nobody handled had *no exception*.

## Decision

**Emit the arms as the chain the syntax describes, and make the end of that chain a re-raise.**

```
handler:
  store i32 0, i32* @exn_flag          ; we are the scope that got it
  ; arm 0: specific → load @exn_code, compare, br %arm0 / %next0 ; bare or Exception → br %arm0
  ; arm 1: (emitted inside %next0) ...
  ; armN: ...
  ; fell off the end: nothing matched
  store i32 1, i32* @exn_flag          ; put it back
  br label %<enclosing handler>        ; or %func.raiseexit when there is none
```

- The flag is cleared on entry because the *raise* sites test nothing; the re-raise restores it, so
  an enclosing `try` sees exactly what the inner one saw. Nesting needs no new machinery: the
  re-raise target is the same `handlerStack` an explicit `raise` consults, minus the frame we just
  popped.
- Arms are emitted in source order, each with its own block pair; a bare `except:` and
  `except Exception:` are branch-through catches rather than code comparisons, exactly as the
  interpreter treats them.
- An arm's own body is emitted with the handler stack popped, so a `raise` inside a handler goes to
  the *enclosing* scope rather than being captured by the try that dispatched to it.
- The mismatch fall-through is the last arm's `notThis:` label, so the chain is one straight line
  of blocks with no duplication of the re-raise.

## What this corrected about our own records

Gap R.21, recorded this cycle's predecessor as "a `return` inside `try:` loses its value in the
compiled backend", was a misreading. It reproduced with `return a % b` and not with `return a + b`,
which is not a property of `try`: the function's two `return` paths had *different types*
(an int and a string), the function was specialised as one, and the caller read the other value
through the wrong lens — an integer rendered as an interned-string index, printed as `(null)`. That
is the mixed-return-type family already recorded as Gap R.22, so the probe was renamed
(`probe_mixed_return_value.gy`), its ledger row reworded, and R.21 closed as *not a separate
defect*. The lesson is the same one ADR 0210 recorded about repros: characterise the trigger
before naming the cause, because a wrong cause in the roadmap sends the next cycle to the wrong
file — and note that a probe's *pin* (the measured output) stayed honest through the misreading,
which is why renaming it cost nothing.

## Codegen / IR implications

`tryStmt` grows one block pair per arm; a two-arm typed `try` reads `@exn_code` twice. The
property is asserted structurally (`TestEveryArmGetsItsOwnTest` counts the reads) as well as
behaviourally, because a dropped arm is precisely a *silently* shortened chain — the module still
verifies, the program still runs, and the only symptom is a branch that never fires.

## Alternatives rejected

- **A switch on `@exn_code` per `try`.** Rejected: arms are not a set of distinct codes —
  bare and `Exception` arms catch everything and must be reachable in position — and the chain
  shape keeps source-order semantics obvious to the reader of the IR.
- **Leave the mismatch fall-through at the handler entry** (test all arms first, then jump).
  Rejected as equivalent but harder to read; more importantly it invites the "one test, one
  default" mistake that produced this bug in the first place.
- **Report the unmatched exception at the point of raise rather than the end of the chain.**
  Rejected: only the chain knows whether any arm in scope will take it, and raising into an
  enclosing handler must stay cheap.
- **Fix `finally` on the `return`/`raise` paths in this commit.** Rejected as a different defect
  (deferred actions on the transfer paths, both backends, recorded as Gap R.23) — and worth noting
  because `finally` *does* run on the paths this lowering touches, so it is testable evidence that
  the chain itself is wired correctly.
- **Also fix the checker's refusal of a name assigned inside a `try` body** (recorded as Gap R.24:
  `try: x = a + b` … `return x` fails analysis with `undefined name "x"`, refusing a program the
  interpreter and CPython both run). Rejected as bundling — it is front-end scope registration, not
  lowering.
