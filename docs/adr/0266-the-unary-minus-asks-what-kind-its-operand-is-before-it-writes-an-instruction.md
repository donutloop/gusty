# 0266. The unary minus asks what kind its operand is before it writes an instruction

## Status

Accepted. Ships with `pkg/lang/negation.go` (the interpreter's `negate`, the compiled backend's
`negationOperandKind`/`negationIndexIsText`/`negationSlotKinds`, and the two raise emitters), the two
guards in `pkg/lang/codegen.go` (the `value()` and `floatValue()` negation roads) and the two fold guards
(`foldConstInt`, `constIntMemberVal`).

Closes roadmap **Gap R.89** ("Unary minus never asks a tag") and **Gap R.137** ("The negation of a text
answers a number on both engines") — the same defect, measured twice: once on 2026-10-02 beside Gap R.88,
once on 2026-10-04 while writing ADR 0265's raise table. Files **Gap R.140** (`abs` of a text, the same
numeric road with a different spelling) and **Gap R.141** (a tuple's operand-type sentence names `'list'`
in the interpreter, waiting on L11.3's tuple object).

Retires the negation-of-a-text-slot probe and its debt row (both are named in the Consequences, where
their replacement is named too); the shape is parity surface in
`programs/negation_names_the_kind.gy`.

## Context

The unary minus was the last operator in the language that never asked what it was given.

```
print(-"hi")     # CPython TypeError: bad operand type for unary -: 'str'
                 #   --interp  -281474976710658   exit 0
                 #   --aot     0                  exit 0
print(-[1, 2])   # CPython TypeError: bad operand type for unary -: 'list'
                 #   --interp  -281474976710658   exit 0
                 #   --aot     %t1 = sub i32 0, @.lst1   → llc rejects → exit 2
print(-None)     # CPython TypeError: … 'NoneType'   ·  both engines answered 0 at exit 0
```

Three separate defects wearing one name. On the interpreted side `-` reached the int evaluator holding the
**heap handle** the operand is stored as — a text is an id at or above `heapIDBase` (2^48, `gc.go`), so
`-"hi"` was the negation of 2^48+2, which is why the digits looked like an address. On the compiled side
`value()`'s `*UnOp` case wrote `sub i32 0, <the operand's storage>` whatever the operand was: 0 for the
first interned text, and for a container literal an instruction with a *global* in it, which `llc-20`
rejects — the exit-code contract's own class-2 outcome ("the compiler is broken") spent on a program the
reference merely stops on (ADR 0166). And the constant folders finished the job: `foldConstInt(-None)`
answered `0`, so the shape never even reached the road that could have disagreed.

ADR 0265 had already fixed this for **one** position — a slot read whose container the object describes —
by branching on the tag and calling `unsupportedNumberOp("neg", kind, "")`. `tagged_numeric_test.go` pinned
the compiled half of that and marked the interpreted half `aotOnly`, because the interpreter had *never*
consulted a tag for a unary operator at all. That pin was the roadmap saying, in a test: "this engine is
wrong here, and we are keeping it visible."

## Decision

**Ask the operand's kind, on both engines, before writing any instruction for it — and raise rather than
refuse.**

1. **The interpreter asks the value.** `Evaluator.negate` (in `negation.go`) is now the only road a
   `-` takes: a float box unboxes and negates, a bool box's payload negates (`-True` is the int `-1`, the
   same payload answer `True + 1` gives, ADR 0259), an unboxed int negates, and every other object the
   heap can hand back raises through `exnError` with `unsupportedNumberOp("neg", e.operandKind(v), "")`.
   `operandKind` is the same naming the binary operators and `len` already read, so one tag vocabulary
   names a value everywhere (ADR 0215).

2. **The compiled backend asks the expression.** `negationOperandKind` answers the kind the compiler can
   name from what the program wrote: the literal kinds (`StrLit`/`FString`, `NoneLit`, list/dict/set/tuple
   literals), a name's records (`strVals`, `internedVars`, `noneVars`, the container maps, `varClasses`),
   a call the module knows answers text or constructs an instance, and a slot of a container the literal
   still describes. For the text question it asks **the printer's own predicate**, `printsAsInternedStr` —
   the rule ADR 0229 wrote ("the print path and the operation path ask this question of the same predicate,
   or one of them renders the interned index as a number"), applied one operator further out. A text is a
   text for `print(x)` and for `print(-x)`, or the two disagree about what `x` holds.

3. **A slot the literal describes gets a raise table, not a guess.** `negationSlotKinds` admits a slot read
   only when `slotNumberFamily` says `"none"` — the literal says nothing the slot holds is a number — **and**
   the program never stores anything into that name (`numericSlotChains.ok[name]` is false, i.e. no
   `append`/`add`/`insert`/`d[k] = v` site). Both halves are load-bearing: without the second, an `append`
   that puts a number in a slot the literal never mentioned would be raised on where CPython answers it.
   One kind means one unconditional raise; several kinds means one `icmp` per kind with the last as the
   unconditional else, so every path out of the read raises — `taggedDoubleFromSlot`'s shape (ADR 0265) for
   the same reason: never a merge with a predecessor that stored nothing.

4. **The tag door keeps what it owned.** `-xs[i]` over a float-family container still goes through
   `taggedNegationApplies`/`taggedFloatNegate`; the new guard runs *after* it, and refuses to answer where
   the family is mixed or int, because every arm that door can build answers a double and an int slot must
   stay an int (`-xs[0]` over `[3, 4]` is `-1`, not `-1.0`).

5. **The folds are closed on this shape.** `foldConstInt` and `constIntMemberVal` decline a negation whose
   operand spells a non-number (`negationOperandIsLiterallyNotANumber`). A constant fold is how a TypeError
   gets turned back into a printed number — Gap R.37's rule, applied to the last operator it did not cover.

6. **The raise is the ordinary emitted one.** `emitBadNegation` / `emitBadNegationOfSlot` call `raiseTo`,
   which stores `@exn_flag`/`@exn_code`/`@exn_msg`/`@exn_frame` and branches to the open handler or the
   function's raise exit. The block is terminated there, so the emitters open a fresh label for whatever
   the caller still has to write: the instructions survive, the path never runs. `except TypeError:` reaches
   every one of these (ADR 0228), which is asserted on both engines and through `llc`, not argued.

7. **Where the compiler cannot name the kind, nothing changes.** A parameter that receives a text on one
   path and a number on another, or a call whose return kind is unset, keeps the road it had. Those are the
   comparison's own open rows (Gaps R.83, R.95, and L11.1's tagged-value-word clause), and refusing them
   here would spend exit 1 on programs that answer today — the expensive direction to be wrong (ADR 0166).

## Measurement

Re-runnable: `go test -tags=llvm20 ./pkg/lang -run Negation` and
`go test -tags=llvm20 ./integration -run 'Negation|TheReferenceStopsOnEvery|BothEnginesRaise'`.

18 trap shapes × both engines, each first checked against CPython (every one of them a program the
reference *stops* on, with the sentence compared byte for byte):

| shape | CPython | `--interp` before | `--aot` before | both now |
|---|---|---|---|---|
| `-"hi"` | TypeError `'str'` | `-281474976710658`, exit 0 | `0`, exit 0 | TypeError `'str'`, exit 3 |
| `x = "hi"` / `-x` | TypeError `'str'` | `-281474976710658`, exit 0 | `0`, exit 0 | TypeError `'str'`, exit 3 |
| `-[1, 2]` | TypeError `'list'` | `-281474976710658`, exit 0 | `sub i32 0, @.lst1` → **exit 2** | TypeError `'list'`, exit 3 |
| `-None` | TypeError `'NoneType'` | `-281474976710657`, exit 0 | `0`, exit 0 | TypeError `'NoneType'`, exit 3 |
| `-{"a": 1}` | TypeError `'dict'` | number, exit 0 | `0`, exit 0 | TypeError `'dict'`, exit 3 |
| `-{1, 2}` | TypeError `'set'` | number, exit 0 | `0`, exit 0 | TypeError `'set'`, exit 3 |
| `-Token()` | TypeError `'Token'` | number, exit 0 | `0`, exit 0 | TypeError `'Token'`, exit 3 |
| `-xs[0]` (literal text slot, computed index) | TypeError `'str'` | number, exit 0 | number/`0`, exit 0 | TypeError `'str'`, exit 3 |
| `-"hi" + 1.5` | TypeError `'str'` | TypeError (already right) | `1.5`, exit 0 | TypeError `'str'`, exit 3 |

The parity half is what keeps the raise cheap: 14 negations with a sign (`-7`, `-1.5`, `-True`, an int
variable, a float variable, a slot of a literal list, a float slot, a slot of a built container, a dict
value, `-7 / 2`, a loop fold, a parameter, a comprehension, `abs(-3)`) still answer CPython's number on
both engines, and `programs/negation_names_the_kind.gy` is `oracle: match` — three engines, same stdout,
each trap caught by the program's own `except TypeError:`.

Two rows are **pinned rather than fixed**, and both are filed:

- `abs("hi")`: CPython stops with `TypeError: bad operand type for abs(): 'str'`; the interpreted leg
  prints `hi` and the compiled leg prints `0`, both at exit 0. Same class, different door, different
  sentence — **Gap R.140**.
- `-("a", 1)`: CPython and the compiled leg say `'tuple'`; the interpreted leg says `'list'`, because a
  tuple literal is built as a list object until L11.3 gives tuples their own object — **Gap R.141**.

## Agentic rationale

An agent reading this language's diagnostics needs the *class* of a verdict to mean one thing. Before this
cycle a program the reference stops on could come back as exit 0 with a number (the interpreter), exit 0
with `0` (the compiler), or exit 2 (the compiler's own module rejected) — three exit classes for one program,
none of them the runtime-error class the JSON schema and `--oracle` reserve for a trap. Now:

- the trap leaves at **exit 3** on both engines, with `TypeError: bad operand type for unary -: '<kind>'`
  in the traceback, so `--json`'s `error` field and the exit-code contract agree;
- the kind inside the quotes is the operand's real kind — `str`, `NoneType`, `list`, `dict`, `set`,
  `tuple`, or a user class name — from the same table the printer, the equality and the binary operators
  read, so a message is searchable and an `except TypeError:` is the same event on both backends;
- nothing about the CLI moved: no flag, no schema version bump. The machine path for this feature is the
  exit class and the sentence, both already in `docs/operations.md`'s contract; `--oracle` classifies the
  new corpus program as `match` on all three legs without a ledger row, which is what "paid" looks like in
  the harness (ADR 0186).

## Alternatives rejected

- **A front-end refusal (exit 1) for a statically-known bad negation.** Cheaper to write, and wrong twice:
  it disagrees with the reference's verdict class, and it escapes the program's own `except TypeError:` — so
  a program that catches the error would run its `except` arm on one engine and its `try` arm on the other
  (ADR 0211's misclassing, ADR 0228's catchability).
- **Routing every bad negation through ADR 0265's `@rt_num_arith`.** That door answers a `double` and its
  callers are print/comparison/float contexts; a plain integer negation must stay an integer, and a raise
  that never returns still has to leave the caller with an operand of the right type. The tag table is
  emitted where the read is instead.
- **Opening the slot door on any container (`family != ""`).** Then `xs = [1, "a"]` / `-xs[i]` would raise
  for the *number* slots, which CPython answers. Requiring `"none"` plus "no store sites" is what makes the
  raise an answer rather than a coin toss on the program's data.
- **Runtime type tags for the positions the compiler cannot see** (a parameter that receives both kinds, a
  call whose return kind is unset). That is L11.1's tagged value word — the real fix — and inventing a
  half-tag here would be a second value model, which is how `rt_mixed_eq` came to be retired (ADR 0247).
- **Naming the kind from the interned index** (a text's index decoded back to its spelling). The index is
  an address-like detail; ADR 0215 put the naming in one table for a reason.
- **Leaving the constant folders alone.** A folded `-None` is a silent 0 with no instruction that could
  have disagreed, which is the one outcome the exit-code contract does not allow (ADR 0166).

## Consequences

- `pkg/lang/negation.go` owns the sentence's *call site* for `-`; the sentence itself stays in
  `unsupportedNumberOp` (ADR 0265), so the interpreter, the emitted raise and the tests cannot disagree.
- The `aotOnly` pins in `pkg/lang/tagged_numeric_test.go` and `integration/tagged_numeric_test.go` are
  gone: the two rows are asserted on both engines.
- The probe that pinned the negation of a text slot (`probe_negated_text_slot`, in `integration/programs/`
  until this commit) left the corpus along with its ledger row: its pins recorded the interpreter's
  `-281474976710659\n`, so keeping it would have meant keeping an asserted wrong answer. The shape is in
  `conformanceStandalone` as `programs/negation_names_the_kind.gy`, declared `match` by absence of a row —
  the harness's definition of paid (ADR 0186).
- Two new rows entered the tracker (`Gap R.140`, `Gap R.141`) rather than being folded into this one: a
  silent exit-0 answer is a row of its own, and each names the door that owes the fix.
- What the language still cannot do is unchanged and still named: the tagged value word (L11.1), tuples as
  objects (L11.3), and the code-point strings that would make `len("café")` 4 rather than 5 on both
  backends (L11.5).
