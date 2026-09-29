# ADR 0214 — a trap the program cannot name is not a trap the program can handle

Status: accepted (roadmap Gap R.25; the interpreter side of the rule ADR 0212 established)

## Context

Nine shapes raised `*EvalError` with an **empty `ExnType`** in the interpreter. That is not a
cosmetic omission: exception matching is on the class, so all of these were uncatchable, and the
traceback printed a bare sentence where an exception should have been named. Measured with
`errors.As`, not by eyeballing tracebacks:

| shape | what we raised | what it should be |
|-------|----------------|-------------------|
| `p.nope` | `no attribute nope` (no class) | `AttributeError: 'P' object has no attribute 'nope'` |
| `int("abc")` | `int: cannot parse string` | `ValueError: invalid literal for int() with base 10: 'abc'` |
| `a, b = [1]` | `cannot unpack value into tuple` | `ValueError: not enough values to unpack (expected 2, got 1)` |
| `x = 5; x()` | `unsupported call for eval` | `TypeError: 'int' object is not callable` |
| `len(5)` | `len expects a list, set, dict, or string` | `TypeError: object of type 'int' has no len()` |
| `x = 5; x[0]` | `cannot index null` | `TypeError: 'int' object is not subscriptable` |

Read the right column of that table again and one row stands out: `x[0]` on an integer reported
*"cannot index null"*. Not untyped — simply false about the program. The variable held an int; the
interpreter had no way to say what it held, so it said the thing its internal representation made
easiest. A reader who believed that message would go looking for a null that does not exist.

## Decision

**Every runtime error the language produces is raised as a typed exception, with the reference
implementation's wording** — ADR 0212's rule, applied to the remaining interpreter sites.

- Each site now calls `exnError(<Class>, <CPython's message>)`: `AttributeError`, `ValueError`,
  `TypeError`, `NameError`, classed by what happened rather than by which internal branch caught it.
- `valueTypeName(v)` renders a value the way an exception message names it — `'int'`, `'NoneType'`,
  and for an instance its class name — so `'P' object has no attribute 'nope'` reads the same here
  as in Python. For the null case the type is `'NoneType'`, matching Python rather than our internal
  word for the sentinel.
- `classDisplayName` recovers a class's name for the `type object 'P' has no attribute 'x'` form,
  because the class object itself does not carry its name and a message that said `type object
  'type'` would be another message about our internals instead of the program.
- `unpackArityErr` distinguishes the two failures the way Python does (`not enough values to unpack
  (expected 2, got 1)` / `too many values to unpack (expected 2)`), since the old single message
  described neither.
- An unbound name became a `NameError` at the interpreter's lookup site. Note the qualification
  kept in the docs: through `lang.EvalExpr` (the `--eval` path) the same source is refused by the
  front end first, which is a different and defensible route — analysis finding what runtime would
  find. The gap being closed here was the *untyped* error, not the choice of which stage reports it.

## Why the wording is pinned, again

Same argument as ADR 0212 and it applies harder here: these messages are the only surface these
errors have, `except` matching is by class, and half the old texts were internal vocabulary
("unsupported call for eval", "cannot unpack value into tuple", "cannot index null"). Internal
vocabulary in a user's traceback is a bug even when the class is right, because it teaches the
reader that the messages are not about their program — and then they stop reading them. The tests
assert class **and** exact message, so the wording cannot drift back to describing our
implementation.

## What stayed open, deliberately

- **Gap R.19's compiled half.** The AOT backend still answers a missing attribute with a value
  (`0`) instead of trapping. The interpreter is now right, so the divergence is one-sided and
  pinned: `programs/probe_builtin_traps_untyped.gy` records the five handler lines the interpreter
  and CPython agree on and the compiled leg not completing, and
  `TestCompiledMissingAttributeIsStillAGap` says out loud what to delete when it is fixed.
- **The refusals' codes.** `int on non-integer string`, `len requires an inline list/dict/set
  literal` and `index of a non-literal variable` are honest refusals (class 1, non-zero, no
  substitute answer) but bare prose with no stable code — roadmap L11.8's remaining item. The test
  asserts honesty (refused, or traps with a class named; never answers) rather than a prefix nobody
  defined yet.
- **Gap R.26** (`"a" * "b"` answers `1099516870662` in the interpreter, exit 0) is the same family
  with a worse symptom — a missing operand test that prints a number instead of raising `TypeError`
  — and is the next one this loop should take, sweeping every operator for the same missing test.

## Alternatives rejected

- **Keep the internal wording and only add classes.** Rejected: `unsupported call for eval` with a
  `TypeError` label still tells the user nothing about their program, and a message they cannot
  search for is a message they will ignore.
- **A single generic `catch` for untyped errors in the checker** (i.e. let `except:` work where
  `except TypeError:` could not). Rejected: it papers over the omission and makes `except:` and
  `except TypeError:` behave differently from Python for no reason.
- **Introduce an error-code enum alongside the class.** Deferred to L11.8, where capability
  diagnostics need it anyway; inventing half of it here would give two competing vocabularies.
- **Refuse all nine shapes at compile time like the AOT backend does for some.** Rejected: the
  interpreter's job here is to run the program and raise what Python raises, and a static refusal
  would turn a catchable runtime event into a build failure.
