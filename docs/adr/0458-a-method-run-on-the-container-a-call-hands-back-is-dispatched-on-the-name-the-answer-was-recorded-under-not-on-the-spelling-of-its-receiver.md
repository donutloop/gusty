# A method run on the container a call hands back is dispatched on the name the answer was recorded under, not on the spelling of its receiver

## Context

`Gap R.229` owed one door. Four positions already read the object that **ADR 0333**'s scan proves a call
hands back — `print(b())`, `str(b())`, `if b():`, `for v in b():`, `ys = b()` — and a fifth, asking that
same object to *do* something, fell past every one of them onto the text road:

| program | the reference | this toolchain before this ADR |
|---|---|---|
| `def b(): return [1, 2]` / `print(b().append(3))` | `None` | exit 1 — `string-method append on a receiver that is not a text the compiler can read: b(...)` (**Gap I.2**'s sentence) |
| `…` / `b().append(3)` / `print(b())` | `[1, 2]` | exit 1, the same sentence |
| `def b(): return [1, 2]` / `print(b().pop())` | `2` | exit 1, the same sentence |
| `def s(): return {1}` / `print(s().add(3))` | `None` | exit 1, the same sentence |
| `def b(): return [1, 2]` / `ys = b()` / `print(ys.append(3))` | `None` | **`None`, since ADR 0333** |

The last row is the one that makes the four above it defects rather than missing features: one program,
two answers, and the road chosen by whether the program happened to name the object. **ADR 0334** had
already ruled on exactly this shape one construct earlier — `build()[0]` versus `ys[0]` — with
`containerAnswerSubscript`: rewrite the base to the rooted name the answer's notebooks were written
under and let the roads that have always answered a container **variable** answer it. The method position
had never been given that judgement, so a `Call` receiver matched none of `callMethod`'s branches
(`isListExpr`, `listVars[…]`, `isSetExpr`, the dict arms), fell through the whole dispatch, and was
reported by the door at the bottom of it — the text road, the only road that takes *any* receiver.

Three things made the row smaller than "add an arm to the method road":

- **The void and the value are one question.** A mutator lowers to *no operand* in this backend — that is
  what makes the statement `xs.append(2)` work. A position that did not know the call answers the void
  lowered the missing word into an operand: the first build of this door printed
  `%t10 = call i32 (i8*, ...) @printf(i8* @.fmt1, i32 )`, which `llc` rejects, spending
  **ADR 0166**'s compiler-bug exit code on a program CPython prints `None` for. That is **ADR 0300**'s
  lesson re-learned one construct out, and it is why `callIsInPlaceMutation` and the dispatch ask the
  *same* predicate (`containerAnswerIsProven`) rather than each keeping a copy.
- **A slot read needs the door that renders a slot.** Serving every container method the scan proved
  produced two answers worse than the refusal: `def b(): return ["a", "b"]` / `print(b().pop())` printed
  **`1`** — the text's index into `@str_tab` — where the bound spelling prints `b`, and
  `return [1, "a", None]` printed **`0`** for a popped `None`. The road that renders a popped slot is
  `popAnswerPair` (`pop_answer.go`), and it asks its questions of the **name** the list is recorded under.
- **The answer is a fresh object per call.** Measured before the door was opened, because a method road
  that took the callee's word without the binding's copy would poison the callee's object for every later
  call: `ys = b()` / `ys.append(3)` / `print(ys, b())` prints `[1, 2, 3] [1, 2]`, and two `b().append(…)`
  statements leave `print(b())` on `[1, 2]`.

## Decision

**Rewrite the node, then let the position lower it.** `container_answer_method.go` gives the method
position the judgement `containerAnswerSubscript` gives a subscript: the receiver is materialised by the
door that already materialises it for a print (`containerAnswerOperandRead`, which emits the call, roots
the handle and writes the container notebooks), the written `Call` is *copied* with its receiver replaced
by that name, and the **caller** lowers the copy. Two entry points ask it, because two altitudes choose a
road from the receiver's shape: the print road, beside the subscript door and before it chooses a printer
(`codegen_builtin_print.go`), and the dispatch itself (`callMethod`, before its text road).

**The road decides which methods it serves, and it decides in a buffer that is thrown away.** The door
keeps no list of served names to consult: it asks the road, in a scratch buffer, whether the *rewritten*
call answers, and commits only when it does. Two consequences the door exists for:

- a shape the roads decline (`dict.clear`, `list.index`, `list.count`, every reader) leaves no
  instructions behind and keeps the refusal the position's own road prints — about the receiver the
  *program* wrote (`d(...)`), not the name the door invented (`__cans9`);
- a shape that renders a slot is taken only on the **proof** that the door which renders it serves it
  (`popAnswerShape`, and the `listVars[…] && pop` mutator arm beside it, asked of the rewritten node).
  That proof is what keeps `print(b().pop())` from printing an interned index, and it is the difference
  between answering `b` and answering `1`.

**The read a position asks is the scan's split, asked of a method.** A mutator hands the handle to the
runtime door that writes it, so it needs the object and the record's kind — `containerReadWhole`, which an
answer the slot table could not describe (`def b(): xs = []` / `xs.append(g())` / `return xs`) still takes.
A method that reads a slot back asks `containerReadSlots`, which the described half alone answers
(**Gap R.261**, **ADR 0344**). `containerAnswerMethodPositionRead` is that split; the mutators that
*compare* slots (`sort`, `reverse`) sit with the mutators, because the comparison is `rt_list_sort`'s own
walk of the object and not a compile-time read of a notebook.

**One scan, one verdict field.** `containerAnswerScan` recorded its described half without ever setting
`fullyDescribed`, so `takesRead` answered "no" for every entry the scan itself proved — a field ADR 0344
added to carry a verdict the scan never delivered. The scan now sets it (`container_return.go`). No
existing door could change behaviour on it, because the only reader of that branch is this door's mirror;
`TestTheMethodDoorServesWhatTheVariableRoadsServe` keeps the predicate honest in both directions.

## What this row deliberately does not take

| shape | today | row |
|---|---|---|
| `d().get(3)`, `d().keys()`, `d().popitem()`, `d().clear()`, `b().index(2)`, `b().count(1)`, `b().copy()`, `b().extend([3])`, `b().remove(5)` | refused, with the sentence the **container VARIABLE** gets (`get() on "ys", which the program bound to a container rather than a text`) | **Gap R.538** — the reader roads fold a literal written at the call and never ask a run-built object (**Gap R.174**'s standing). The door refuses what the roads refuse, so opening the road opens the answer with no change here |
| `if b().append(3):`, `while b().add(1):`, `x = b().append(3)` | reach `llc` (`icmp ne i32 , 0`, `store i32 , i32* %_x`) — exit **2** | **Gap R.537** — the condition and binding doors lower the void's missing word. `if xs.append(3):` and `x = xs.append(3)` die identically for a plain container variable, which is what makes it one row rather than this door's damage; pinned in `TestTheVoidAtTheConditionAndBindingDoorsDiesInTheAssembler` and at the CLI |
| `t().append(4)` of `return (1, 2)` | refused: `` `append()` would write into a tuple … `'tuple' object has no attribute 'append' `` | **L11.3**'s immutability gate, which sits at the head of `callMethod` and is asked before this door is ever reached |
| an answer whose method is reached through an expression (`c.build().add(1)`) | the carried-method round's own answer (**ADR 0430**), consulted by the same predicate | — |

## Consequences

- The method roads gained no arm; the answer's notebooks gained a second reader. A future container
  method (`extend`, `insert`, `dict.update`) needs no change here: the road decides, and the door's proof
  follows it. The predicate that the *tests* ask (`containerAnswerMethodIsServed`) fails until it is
  updated beside the road that grew.
- The print road now asks two rewrites per argument (subscript, method), both of which are a name lookup
  and a table probe until the gate passes, and the method one emits IR only when it commits.
- Every refusal the door declines is the road's own sentence, which is what `--json` and
  `--capabilities` hand an agent; the invented name never appears in one (`ADR 0166`).
- Two shapes that were refusals now run, and one class of refusal (the invented name leaking into a
  diagnostic) is prevented by a test rather than by care.

## IR implications

Nothing emitted is new: the answer's `alloca`/`store i32 %h, i32* %___cansN`/`rt_root_put` pair and the
`rt_append_tagged` / `rt_set_add` / `rt_pop` calls are the ones the bound spelling already emitted, in the
same order. The claim the suite holds is that the module carries **no empty operand** at any of these
positions — `TestNoAnsweredShapeDiesInTheAssembler` verifies every answered shape with `llc`, and
`TestEveryMethodOnACallResultModuleVerifies` asks `--verify-llvm` from the outside.

## Machine path

- `gustyc --eval 'def b(): …'` — the answered shapes print the reference's bytes at exit 0; the raises
  leave the run at exit 3 with the reference's class and words; the refusals leave it at exit 1.
- `gustyc --json --eval …` on a shape the roads do not serve returns the refusal document with
  `ok:false`, `exit:1`, `phase:"codegen"`, `backend:"aot"` and a `lower.unsupported.*` capability code, and
  quotes the receiver the source spelled (asserted in
  `TestTheRefusalOnAMethodAnAnswerCannotServeReachesAnAgentAsJSON`).
- `gustyc --verify-llvm '<src>'` answers the module verifier's own verdict for every answered shape.

## Alternatives rejected

1. **An arm in `callMethod` for a `*Call` receiver, dispatching on the container kind.** This is what was
   tried first, and it answers the mutators while leaving the slot-returning methods to the raw slot word —
   the `1`-for-`"b"` answer measured above. It also duplicates the kind question the roads below already
   ask (**ADR 0232**).
2. **A list of served `(kind, method)` pairs the door consults.** A second answer to one question, and the
   first thing it gets wrong is a method the road grew since the list was written. The predicate form
   survives (`containerAnswerMethodIsServed`), asked by the tests and never by the door.
3. **Serving the slot-returning methods too, and letting the print road print what the pop door declined.**
   Rejected on measurement: it fabricates numbers (**ADR 0166**'s forbidden answer, **Gap R.261**'s defect
   class), which the repository ranks above any refusal.
4. **Teaching `popAnswerShape` to see through a `Call` receiver.** It is asked as a pure predicate by
   program-wide scans (`arithWouldRefuse`, `pairCallScan`) that cannot emit the answer's `alloca`; giving it
   a builder would have made the shape question answer differently depending on who asked.
5. **Rewriting the AST once, before codegen, into `tmp = b(); tmp.m(…)`.** Structurally the same answer and
   at first tempting, but the AST is shared by the fold, the checker and the schema dump, statement context
   is not available at every expression position, and the door needs the *read* the position asks — which
   is the one thing a source-level desugaring throws away.
6. **Fixing the condition and binding doors in this commit.** Tempting (both die in `llc`), but they die
   there for a plain `xs.append(3)` too, they are two more doors, and the row is the method position. They
   are filed as **Gap R.537** with their bytes pinned.

## Validation

- `pkg/lang/container_answer_method.go` — the door.
- `pkg/lang/container_answer_method_test.go` (NEW): **38** answered programs on the compiled leg, the
  **14**-row bound/direct parity over stdout *and* trap, the raises (`IndexError: pop index out of range`,
  `pop from empty list`, `KeyError: 9`), the same raises taken by the program's `except`, the tuple's
  `AttributeError`, the invented-name guard, the two decline sentences the door recognises (and the two it
  must not), the served-what-the-roads-serve predicate in both directions, the `llc` verification of every
  answered module, the text road's negative control, and the owed tables with the reference's bytes beside
  them.
- `integration/container_answer_method_test.go` (NEW): the answered family against a live `python3`
  (including the aliasing rule), the raises at exit 3 with stdout silent, the catchable family, the refusal
  family judged on its sentence and the name it quotes, the refusal as JSON, the tuple, `--verify-llvm` on
  every answered shape, and the owed rows pinned by exit code.
- **64** sources newly recorded from the reference with the compiled leg cross-checked and none refused
  (`tools/recmerge`; record **10505 → 10569**).
- Roadmap: **Gap R.229**'s method half is paid (its element and slice halves were paid by **ADR 0457** and
  **ADR 0335**); **Gap R.537** and **Gap R.538** are filed by this row.
