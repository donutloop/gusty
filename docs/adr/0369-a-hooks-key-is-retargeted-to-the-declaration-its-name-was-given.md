# 0369 — A hook's key is retargeted to the declaration its name was given

## Context

`sorted`, `min` and `max` call their `key=` argument once per element of the container. That call is
in nobody's source: the programmer wrote an argument, the AST holds an expression, and the door in
`pkg/lang/call_key.go` emits the call. It is therefore the one position in the language where a NAME
holds a callable and is never spelled as a callee — and it is the position ADR 0345's
defunctionalisation pass never looked at.

The pass owns a module-level binding of a lambda. `neg = lambda v: 0 - v` is given a declaration of
its own (`neg$lambda$…_0`), the binding is rewritten to the callable constant, and every *call* of
`neg` is retargeted to the declaration. A `key=` is not a call. The retarget stopped one argument
short, the name kept holding the payload word, and the door called that word:

| source | reference | this backend, before |
|---|---|---|
| `neg = lambda v: 0 - v` / `print(sorted([3, 1, 2], key=neg))` | `[3, 2, 1]` | **exit 3** — `TypeError: 'int' object is not callable` |
| same, `print(min([3, 1, 2], key=neg))` | `3` | same trap |
| same, `print(max([3, 1, 2], key=neg))` | `1` | same trap |
| same, `key=neg, reverse=True` | `[1, 2, 3]` | same trap |
| `neg = lambda v: 0 - v` / `two = neg` / `print(sorted([3, 1, 2], key=two))` | `[3, 2, 1]` | same trap |
| `def neg(v): return 0 - v` / `print(sorted([3, 1, 2], key=neg))` — **the control** | `[3, 2, 1]` | `[3, 2, 1]` |
| `print(sorted([3, 1, 2], key=lambda v: 0 - v))` — **the control** | `[3, 2, 1]` | `[3, 2, 1]` |
| `print(sorted([-3, 1, -2], key=abs))` — **the control** | `[1, -2, -3]` | `[1, -2, -3]` |

The two controls are the whole diagnosis. The hoist works and the call works — the inline spelling
was already hoisted and called, and a `def`'d name names its own `define` — so what was missing was
not a callable and not a call road. It was that binding the hoisted lambda to a name left the name
holding `0`, the same number Gap R.277's defaults read, and the door read the name as the value it
looked like. Gap R.281 was filed with exactly this measurement on 2026-10-15 and its row is why the
`lambda_default_param.gy` corpus program still writes its hook inline, with the absence named in the
file's header rather than quietly dropped.

## Decision

**The `key=` slot is a call position, and a call position is retargeted.** One mechanism, the one
ADR 0345 already uses for a callee, performed one argument outward.

1. **The reading is the door's, not a new one.** `callableKeySlotOf` asks `keyDoorOptions` — the
   same reader the parameter census (ADR 0363) and the door itself use — which argument stands in the
   `key=` slot of a call to `sorted`/`min`/`max`, and refuses to answer for a program that wrote its
   own `sorted`, for an option the door raises on, and for a call with no key. Three readers of one
   construct would be ADR 0233's second answer, and the row that adds the third is the row that
   disagrees with itself.
2. **A name in that slot is being CALLED, not read.** `callableRead.keySlots` records it, and the
   pass's "is this name only ever called" question — the invariant every rewrite here is written
   against — counts a callee-shaped use it cannot otherwise see. `callableKeyIsCalled` mirrors the
   door's own arity rule (`f() takes 0 positional arguments but 1 was given` is not a call), asked of
   the program the pass produced, so a key the door would raise for still reads as a function
   reaching a value position and withdraws the rewrite.
3. **The rewrite is the alias road's.** For each key slot whose name the plan resolved — through
   `chaseCallableName`, the chain `neg = lambda …` / `two = neg` walks to the symbol the callable
   actually lives under — the argument is rebuilt as a `Name` pointing at the declaration, the
   `KeywordArg` and its name surviving so the door's option-reading still sees `key=` and the
   reference's own reading of the call is undisturbed (ADR 0340).
4. **The pass stays all-or-nothing.** A program whose hook the pass cannot resolve comes back
   untouched and keeps the answer, refusal or trap it always had; `key_arguments` is counted into the
   report and into the plan's hash, so two programs differing only in a hook the pass answered cannot
   certify each other through one digest.
5. **The door keeps speaking the program's words.** `keyAnswer.spelling` is now
   `callableSurfaceName` of the resolved name — the program's own spelling for a `def` or a builtin,
   the reference's `<lambda>` for a lambda the pass gave a declaration to — and the sentences written
   before a `keyAnswer` exists go through `keyExprWord` for the same answer. The arity trap is the
   reference's own line, character for character:

   ```
   TypeError: <lambda>() takes 0 positional arguments but 1 was given
   ```

   where the retargeted AST would otherwise have said `neg$lambda$a78954b_0()`, a name no programmer
   wrote and no editor can find (ADR 0166, ADR 0345 §3). This half was measured on the inline road
   too — it is not new to this row — and is filed as **Gap R.325**, closed beside it.

## Codegen and IR implications

The emitted module for the row is three lines of the interesting part:

```
define i32 @gy_neg$lambda$a78954b_0(i32 %p0) { … }
%t5  = call i32 @gy_neg$lambda$a78954b_0(i32 3)
call void @rt_frame_push(i8* @rt.frame.name._lambda_, i32 2)
```

* The door synthesises `key(element)` and lowers it through the ordinary value road, so arity,
  parameter pairs, float returns, a raise inside the key and the traceback frame are the ordinary
  call's business and not this row's (ADR 0233). The frame name is the reference's `<lambda>`.
* No `fnptr`, no indirect `call`, no `ptrtoint`/`inttoptr`, no cast handle: the key is a `define` and
  the call is a direct one, asserted per row in `pkg/lang/named_hook_test.go`.
* The key's ANSWER kind still decides the comparator: `userFunctionKey` keeps `mode 1` (interned
  text) for a body the text-return scan registered and `mode 0` otherwise, so a lambda returning a
  text through a name orders by the same comparator one written inline does.

## Alternatives rejected

* **A callable value word (`fnptr`, a symbol index, a boxed function).** This is ADR 0345's first
  rejected alternative, restated because this row is where its absence bites loudest. A key held in a
  name is a function *value*, and the honest general fix is the tagged word L11.1 owns. It is not
  cheaper here than it was there, and a half-word that lets a name hold a callable while `print(f)`
  still fails would be the second answer ADR 0232's collision warning is about.
* **Reading the key's name inside the door** (`keyDoorOf` looking up the pass's table). The door runs
  per call site with no view of the plan, and the name it would read is a binding the pass already
  deleted. It would put the pass's private knowledge inside a door that today answers from the AST and
  the notebooks alone, and it would leave every *other* consumer of the rewritten tree — the census,
  the print door, the traceback names — still reading `neg` as a value.
* **Refusing the shape.** The three controls in the table above answer. A refusal that costs the
  reference's own program is the trade this loop has repeatedly declined (ADR 0273).
* **Hoisting the lambda at codegen time when the door meets an unknown name.** That would be a second
  hoist with its own name-minting, its own hash and its own copy of the closedness check, running
  after the tables that decide a call have been filled — exactly the ordering ADR 0345 gives the pass
  for doing FIRST.

## What stays owed

Named hooks are answered where the container is one the compiler can enumerate. The rest of the
family is filed rather than smoothed over:

* a container built at run time (`xs = [3, 1, 2]` / `sorted(xs, key=neg)`) refuses on all three
  spellings of the key — `def`, inline and name alike — because the door would have to carry an
  element to the key (L11.1's value word);
* a lambda-bound name READ as a value (`print(neg)`) dies with `undefined name "neg"` rather than the
  capability sentence, because the pass withdrew its rewrite and the emitter then deleted the binding
  anyway — **Gap R.326**;
* a hook whose container is a run-time container, or whose answer is returned from a body, prints
  `0`/nothing or reaches `llc` at all — **Gap R.327**;
* a name bound to a lambda inside a function body, and a callable handed to a user-defined hook, are
  the pass's argument road and Gap R.255/R.266's, unchanged.

`integration/programs/named_hook.gy` is the 209th matrix row (oracle `match`); the record leg holds
the 23 sources this row first asked, written from the reference by `tools/recmerge` with the compiled
leg cross-checked (7762 → 7785).
