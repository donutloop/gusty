# Every construct is implemented, refused, or absent — never "parses, runs, prints something"

Status: accepted. Records the design decision behind roadmap **Phase 12** (`L12.1`–`L12.13`) and the
14 defects its survey measured (`Gap R.53`–`Gap R.66`). Cites: ADR 0212 (a built-in trap is a typed
raise, in every backend), ADR 0211 (one event, one exit code — the exit-code contract), ADR 0166 (a
refusal must be actionable and must not print an invalid module), ADR 0186 (the CPython oracle leg,
the only instrument that can see this class), ADR 0190 (corpus growth: a corpus grown from bug
reports only tests what we already doubted), ADR 0219 (a citation must resolve or say `(planned)`),
ADR 0166/0227's single-source rule for names (`predeclared.go`), extended in `L12.11` to methods.

## What had been

The language had a fourth state, and nobody had named it. Asked "does gusty support X?", the
available answers were a code read, a feature list, or a guess — and the honest answer for a
disturbing number of constructs was "it depends which of the three engines you ask".

A three-engine survey (2026-10-01, 76 idiomatic-Python programs through `--interp`, `--aot` and
CPython 3.12.3; method and per-class examples in `docs/roadmap-details.md` § Phase 12) sorted the
surface into:

| What the three legs do | Programs |
|---|---|
| CPython-equal on both backends | 9 |
| Both engines refuse, CPython runs (an honest absence) | 37 |
| The interpreter agrees with Python, codegen declines | 16 — 5 of them by emitting a module `llc` rejects, i.e. exit 2, a compiler bug |
| **Every leg runs and gusty disagrees** | **8** |
| **The loop never ends, or never starts** | **2** |

The last two rows are the reason this ADR exists. Examples, each reproducible as a program:

```gy
x = 5
if 1 < x < 3: print("in")     # CPython: (no)   both backends: in          — Gap R.53
```

```gy
class R:                       # CPython prints 1 2 after
    def __iter__(self): return self          # --interp: 7.3M lines in 15 s
    def __next__(self): …                      # --aot: prints "after", exit 0
```

```gy
print(2 ** 63)   # CPython 9223372036854775808 · --interp -9223372036854775808 · --aot 0   — Gap R.64
```

```gy
class C:
    def __str__(self): return "S"
print(C())       # CPython: S · --interp: <instance> · --aot: 0                            — Gap R.55
```

Note which of these a green CI cannot exclude: parity compares the backends to each other, and here
the backends either agree on a wrong answer (`1 < x < 3`) or disagree so loudly that no test was
ever written for the shape (a hang versus an empty loop). The manifest was part of the problem too —
`gustyc --lang` never claims a construct that does not exist, and omits `with`, `yield`, `async def`,
`in`, `is`, `**`, the ternary, the walrus and f-strings, all of which run today.

## The decision

**Every construct in the language is in exactly one of three states, and the state is published.**

1. **Implemented** — both backends, byte-identical to each other and CPython-equal, pinned as a
   corpus row (`programs/*.gy`) whose oracle verdict is `match`.
2. **Refused** — the shape is recognised and declined at the front end or in codegen with a stable
   `Diagnostic.Code`, a documented exit class from `docs/operations.md`, a sentence in
   `docs/language.md` naming the construct, and (where the other backend does better) a claim about
   the other leg that is *derived*, not pasted (ADR 0166 + Gap R.38).
3. **Absent** — no parser node, no checker name, no interpreter case, and not in the surface
   manifest. An agent reading `--lang` cannot be tricked into writing it.

There is no fourth state. "It parses, it runs, it prints something" is a defect, and so is "it
parses and one engine invents an answer" — the survey's `8` and `2` rows, all of them.

Three rules make the states checkable rather than aspirational:

• **A construct cannot be implemented in one backend and refused in the other** without that being
recorded as a gap row with an owner — which is what the tracker already says, now backed by a
census that can be regenerated. 5 of the survey's refusals were not refusals but `llc` rejections
(`%t1 = add i32 @.lst1, @.lst2` for `[1] + [2]`); the same shape in the string case (`"ab" * 2`)
refuses cleanly, which proves the check is writable and merely unwritten (Gap R.33).

• **A keyword that is not a keyword is a bug.** `assert`, `del`, `nonlocal` and `global` lex as
identifiers, so `--check` reports `undefined name "assert"` while `--file` pays a runtime `NameError`
at exit 3 — one source line, two events, two exit codes. One keyword table must drive lexer,
statement dispatch, checker and interpreter the way `predeclared.go` drives built-in names (`L12.7`).

• **The extension points are protocols, and the protocol table is one list.** `__str__` / `__repr__`
reached no printer; `__iter__` / `__next__` reached no loop; class bodies bound no attributes, so
there was no `Enum` to have. A class that cannot answer `str(self)` is not an object, and a loop that
cannot read `StopIteration` is not an iterator (`L12.2`, `L12.3`, `L12.4`).

## Why this is the 2026 answer and not "be Python"

The claim is not parity with CPython — the language has documented divergences it defends (an
integer iterable is a repeat count, ADR 0207; `__exit__` arity, `for … else` boundaries). The claim
is that a *dynamic* language is used by people who write it from memory, and a compiled dynamic
language earns its "compiled" only if what it does answer is what the source meant. So the contract
is about the honesty of the answer, not the size of the feature set: refuse what you cannot mean,
say so where the user will read it, and never answer a question the program did not ask. That is the
same instinct that turned unguarded `srem` into a typed `ZeroDivisionError` (ADR 0212) and an
uncaught exception exiting 0 into exit 3 (ADR 0211), applied one level up — to the grammar instead of
the runtime.

## Alternatives rejected

• **A compatibility checklist ("supports 60 % of Python").** Unfalsifiable, and it hides the worst
cases: a construct can satisfy a checklist item while answering wrongly. The survey's most dangerous
rows were the ones a checklist would have ticked.

• **Refusing everything unimplemented, everywhere, now.** The other side of the same coin, and
already rejected by ADR 0206/0207/0214's precedent: a program that still means something is not a
refusal. The state machine allows refusal-with-a-code precisely so that partial surfaces stay
visible rather than silently half-built.

• **Patching each symptom in the order reported.** Eight silent wrong answers, four of them the same
untagged-word root. Fixing `print(C())`'s `0` without the `str`/`repr` pair (L11.2), or the dict
pattern's `(null)` without the tag (L11.1), produces a second implementation of an answer the
language has not decided. Those three rows are named as Phase 11's in the tracker and are not
started early.

• **A hand-edited `--lang`.** It is what produced this mess in the safe direction — never
over-claiming, endlessly under-claiming. The manifest is generated from the implementation's own
tables, with the three-engine census as the witness (`L12.13`).

## Consequences, and the honest limits

• The tracker gains `L12.1`–`L12.13` and `Gap R.53`–`Gap R.66`, and the open queue gains 13 rows
(53 owed). This is a planning cycle: **no compiler behaviour changed here**, and the rows are the
work. Each one still owes its own ADR, unit test, integration program and docs update per the
Definition of done.

• The census is a **measurement with a date**, not a standing check. The harness that produced it
lived outside the repo; `L12.13`'s definition of done promotes the probes into `integration/programs`
and makes the manifest-vs-census diff a CI failure. Until then the record says the number is dated
rather than implying a test that does not exist.

• Two surveys-adjacent facts are recorded without being assigned work beyond an existing row: the
`with` protocol's arity is unenforced (a one-argument `__exit__` runs where CPython raises
`TypeError`), which belongs to `L12.2`/`L12.3`; and the bundled stdlib is four data-only modules
(24 lines — `math` has no `sqrt`, `json` has no `dumps`), which makes `import math` succeed while
`math.sqrt(16)` reads like a typo in the program.

• `int` is now an open design question rather than an accident (`L12.12`): bignums, or a documented
bounded `int` that traps on overflow. Until the ADR decides, a constant fold must not resolve a value
the program cannot hold (Gap R.37's rule).

• The exit-code contract gains a duty, not a new code: an unlowerable shape must reach exit 1 as a
refusal with a code, and never exit 2 as a module `llc` rejects. Exit 2 stays reserved for compiler
bugs, which is why the survey's five invalid-IR shapes are filed as defects rather than absences.
