# A class keeps the members its body bound, and the read asks the class — not the instance

## Status

Accepted — 2026-10-24. Closes **roadmap Gap R.297** (a class-body member read through the class name stopped the
build with `unsupported attr expression`, where CPython prints the value). Files the residue it measured beside itself
and did not buy: **Gap R.431** (the fractional, verdict, void, container- and call-bound members, and a member the body
never bound). One commit, one feature: the class member table in `pkg/lang/codegen_class.go`, the read in
`pkg/lang/codegen_value.go`, the write beside it in `pkg/lang/codegen_assign.go`, the two kind questions in
`pkg/lang/codegen_text.go` / `pkg/lang/codegen_float.go`, and the two test files.

## Context

```
xs = [1, 2, 3]
class C:
    first = xs[0]
print(C.first)          #  1                  reference
                        #  refused, exit 1    this backend, before — `unsupported attr expression`
```

Python builds a class by *executing its body*: every name the body binds becomes an attribute of the class, and
`C.first` reads it. This backend's class walk registered the methods and dropped every other statement of the body, so
the read fell through all of the value road's cases and landed on the last sentence in the dispatcher —
`unsupported attr expression`, which names an AST node at the program instead of the program (**Gap R.116**'s rule) and
stops a build the reference simply answers.

The instance road next door was already mature: an object carries an attribute slot array addressed by
`@rt_inst_get`/`@rt_inst_put`, and `self.x` / `inst.x` work. That road is *not* the class road. An instance is a value
with a class word; a class here is a name the front end resolved, and there is no object to hold slots for it. The
mistake available here is to reach for the instance machinery because it exists.

## Decision

**1. The class keeps a member table.** `classInfo.attrs` maps a member name to the expression its binding gave it,
recorded by the same walk that registers the methods. The instance road is untouched, and the test file pins that
(instance attributes set in `__init__`, rebound on an instance, and a method reading `self.x` all still answer).

**2. The read lowers the bound expression where it is READ.** Not a value, an expression: a class body runs once, and
the read may stand anywhere after it — a different statement, a different expression, twice. That choice is what makes
the row's second requirement work for free: `C.first = 5` re-binds the member in the table, and the next
`print(C.first)` lowers the new expression and answers 5. It is also what keeps the print roads correct without a
second value representation for this family: lowering a `*StrLit` takes the ordinary text road, lowering an `*IntLit`
takes the number road.

**3. Only values whose re-lowering is a READ, not an event.** A container literal at each read would mint a fresh
object (so `C.items.append(1)` and `print(C.items)` would disagree about the list), and a call would run again where
the reference ran it once. Those bindings are recorded as *owed* and refused with the kind named.

**4. The kind questions ask the same table.** `print(C.label)` initially came out `0`: the read lowered to the intern
table's index, and nothing told the printer it was a text. `stringVal` now reads the member table (**ADR 0232**'s
"the slot describes itself", applied to a class member), so the text prints as a text and concatenation works. The
float question reads it too and then *declines* the answer: a fractional member has no faithful lowering without the
boxed pair, so it is refused rather than printed as `1.0`. That ordering — open the kind question, then refuse rather
than answer the payload — is the whole point: the alternative was three silent wrong answers dressed as a closed row.

**5. Every refusal names the class, the member, and the missing half.** `C.other` is refused as "the class names no
member of that name … the reference would raise AttributeError"; `C.flag` as "the class body binds flag to a verdict
member … needs the value word that carries its own kind", both naming **Gap R.431**. The raw
`unsupported attr expression` sentence is gone from this shape.

## Consequences

* `print(C.first)`, `C.first + 1`, `print(C.label + "!")`, `if C.count:` and the rebinding answer the reference's
  bytes on the compiled leg, the record leg and the CLI leg; 16 sources went to the record through `tools/recmerge`
  (**9156 → 9172**), each cross-checked against the compiled leg.
* Two tables per class (`attrs`, `owedAttrs`) rather than one, because the read has to distinguish "the class never
  bound that" from "the class bound it and I cannot answer it yet" — a refusal that claims the first when the truth is
  the second would send a reader to the wrong place.
* The instance roads (`@rt_inst_get`, `@rt_inst_put`, method dispatch) gain nothing and lose nothing; the control
  family says so.
* The class member table is a **front-end** record, consulted at codegen time. A class whose members are mutated
  through a computed name, or read after being bound by a call, is still **Gap R.431** — and the honest long-term
  shape there is the tagged pair, not another predicate.

## Alternatives rejected

* **Answer every member the body bound, whatever its kind.** Rejected on measurement: `print(C.ratio)` printed `1.0`,
  `print(C.flag)` printed `1`, `print(C.nothing)` printed `0` — three answers at exit 0 that no language gives. A
  refusal that names the kind is worth more than a number that looks plausible.
* **Store the value (a payload + tag) at the class's making.** Rejected because it needs the arena and a class-object
  representation this backend does not have, and because it makes the rebinding a runtime event to no purpose: the
  reads in the pinned programs are all resolvable, and the expression form is what keeps `C.first = 5` trivially right.
* **Reuse the instance slot road.** Rejected: there is no instance, and the slots are an object's array keyed by
  `attrSlot`, so `C.first` and `inst.first` would have had to share an address space that means different things.
* **Leave the row open and refuse everything, as before.** Rejected because the reference answers the shape and the
  record answers it: the only thing standing in the way was the missing table.
