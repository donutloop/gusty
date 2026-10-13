# ADR 0325: an instance's class is the object's word, not the slot's word

**Status:** Accepted
**Date:** 2026-10-13
**Implements:** roadmap `L11.1`, `Gap R.209`
**Extends:** ADR 0182 (one tag table), ADR 0187 (a payload travels with its tag), ADR 0259 (a bool slot says `bool`), ADR 0323 / ADR 0324 (membership and loop arms keyed by the slot's tag), ADR 0271 (a raise names the operand's real kind)
**Precedent:** ADR 0259 — fix the writer's tag once, and every tag-keyed reader follows

## The decision

An element, set member, dict key or dict value whose expression is a user-class instance is tagged
`TagInstance` at the point it is written into a container. The payload remains the heap handle. The object's
slot 0, written by the constructor, carries the class id. When a tag-keyed door has to say which class the
program stored, the module's class table reads that id:

```llvm
define internal i8* @rt_class_short(i32 %id) { ... }
define internal i8* @rt_instance_msg_ptr(i32 %h, i32 %which) { ... }
```

The five sentences the reference writes are not collapsed into one template. `in` says
`argument of type 'C' is not iterable`, a loop says `'C' object is not iterable`, `len` says
`object of type 'C' has no len()`, a text haystack says
`'in <string>' requires string as left operand, not C`, and an unhashable key/member would say
`unhashable type: 'C'`. The arm owns the exception code, span and landing pad; the helper formats only the
sentence.

The default identity printer uses the same object-borne id, but its module-qualified table:

```text
<__main__.C object at 0x7f...>      CPython
<__main__.C object at 0x1>          this backend
```

The address is the unstable half. The oracle's new default `instance-identity` rule normalizes both legs to
`<__main__.C object at 0x…>` and leaves the class — the claim under test — byte-compared.

## The measurement that made the shape necessary

ADR 0323 added tag dispatch to a slot's membership and loop questions. A slot holding `C()` then had four
symptoms, not four defects:

| program | reference | build before this commit |
|---|---|---|
| `xs.append(C())` / `print(xs[0])` | `<__main__.C object at 0x…>` | `1` — the heap handle |
| `xs.append(C())` / `len(xs[0])` | `TypeError: object of type 'C' has no len()` | `'int'` |
| `xs.append(C())` / `1 in xs[0]` | `TypeError: argument of type 'C' is not iterable` | `'int'` |
| `xs.append(C())` / `for v in xs[0]` | `TypeError: 'C' object is not iterable` | `'int'` |
| `xs.append("ab")` / `ys.append(C())` / `ys[0] in xs[0]` | `'in <string>' requires string as left operand, not C` | `'int'` |
| `c in "abc"` / `"abc" in c` | the same two reference sentences, naming `C` | the text road answered `True` / `False` at exit 0 |

The static binary-membership rows were the surprise. Once the object itself was the subject, the ordinary
membership road could not continue to treat `c in "abc"` as a substring test or `"abc" in c` as container
membership. Both are raises, and they name different halves of the operation: the haystack for iterability,
the needle for a text haystack.

## Why the writer is the only place to fix it

`elemKindTag` used to answer `TagInt` for every non-string, non-float, non-container expression it chose to
admit. An instance's payload is a heap handle, and an integer's `i32` word can hold a handle, so the label
was treated as unimportant. It was not unimportant. ADR 0187's rule is that a payload is never written
without the tag that gives it meaning, and every consumer already obeys the tag:

- the mixed printer chooses numeric, text, container or instance rendering from the slot tag;
- `lenOfTaggedSlot` chooses the length question or the TypeError from the slot tag;
- ADR 0323's membership arms choose `@rt_contains_tagged`, `@rt_dict_has_tagged`, `@rt_set_contains_tagged`,
  `@rt_str_contains`, or a raise from the slot tag;
- the loop road chooses the iter helpers or the raise from the slot tag.

Fixing any one of those doors would have left the other three naming `int`. The change is one tag decision
plus the renderers that make the tag meaningful:

```go
if c, isCall := e.(*Call); isCall {
    if nm, isName := c.Fn.(*Name); isName {
        if _, isClass := g.classInfos[nm.Value]; isClass {
            return int32(TagInstance), true
        }
    }
}
```

The same rule applies to a name whose latest binding the assignment walk knows to be an instance. A pair
variable whose tag came from a built slot already carries the object's tag and does not need the compiler to
guess it.

## The class is an object fact, not a tag fact

`TagInstance` says there is an object. It does not say which class. That distinction is why the class-name
table lives beside the heap block rather than in the compiler's notebook:

```go
g.globals.WriteString(heapRuntimeIR)
g.globals.WriteString(g.instanceRuntimeIR())
```

The heap block's mixed printer refers to `@rt_instance_repr_ptr`, and ADR 0173's rule is that an emitted
internal call must have one definition somewhere in the module. The class table is module-specific because
class ids are this module's dispatch ids, and a constructor wrote one of them into slot 0. The helpers read
that word at print or raise time.

Two renderings, because CPython uses two spellings:

- `@rt_class_short` gives `C` to the TypeError sentence.
- `@rt_class_qual` gives `__main__.C` to the identity form.

A shared name would make one of the two sentences wrong. The helper is not a raise. `raiseInstanceTo` calls
it for the formatted message and then performs the same exception store, span and branch as every other
generated raise, so the program's `except TypeError:` reaches it (ADR 0228).

## Why the address is normalized, not imitated

A compiled program does not have CPython's process address. Printing the heap slot preserves the shape and
removes an unreproducible byte from every conformance row. The oracle rule is deliberately narrow:

```go
regexp.MustCompile(`<([^<>]+) object at 0x[0-9a-fA-F]+>`)
```

It rewrites the complete identity form on both legs to `0x…`, and no rule is applied to a bare hexadecimal
number elsewhere. The identity tests still assert the class and reject output that is merely the numeric
handle; normalization only allows the conformance matrix to compare the reproducible part.

## The residue that remains

A name bound from a built slot into the binary-membership road (`y = xs[0]`, then `y in "abc"`) still reaches
the one-word operand refusal owned by Gap R.146. The slot itself now carries the right tag, but this road has
not yet carried the pair into an ordinary binary operand. This is not a new answer invented by the instance
tag: the pair door is the owner, and the row already exists.

## Witness

| claim | witness |
|---|---|
| the four tag-keyed slot questions name `C`, not `int` | `pkg/lang/runtime_slot_read_test.go::TestAnInstanceSlotNamesItsClassForEveryTagQuestion` and `pkg/lang/instance_slot_test.go::TestAnInstanceSlotNamesItsClassWhereAPlainIntWould` |
| print, a bound name, a built slot, a list, a dict and a set print the identity form | `pkg/lang/runtime_slot_read_test.go::TestAnInstanceSlotPrintsItsIdentityForm` and `pkg/lang/instance_slot_test.go::TestAnInstanceSlotPrintsItsClassRatherThanItsHandle` |
| identity comparison and containment remain object facts | `pkg/lang/instance_slot_test.go::TestAnInstanceSlotIsStillAnObjectForIdentityAndContainment`, both legs |
| the module branches on slot 0 and formats the five sentences | `pkg/lang/instance_slot_test.go::TestTheClassRuntimeIsEmittedOnlyWhereTheObjectNeedsIt` and `::TestAnInstanceSlotAsksForItsTagRatherThanTheNumberDoor` |
| the ordinary container doors did not start paying the class table | `pkg/lang/runtime_slot_read_test.go::TestTheInstanceTagDoesNotDisturbTheOrdinaryContainerQuestions` |
| the CLI raises the reference's class sentences at exit 3, exit 2 forbidden | `integration/runtime_slot_read_test.go::TestASlotHoldingAnInstanceNamesItsOwnClass` (6 rows) |
| the CLI prints the identity form rather than the numeric handle | `integration/runtime_slot_read_test.go::TestAnInstanceSlotPrintsItselfRatherThanItsHandle` (6 rows) |
| oracle comparison compares the stable identity form | `pkg/lang/oracle_test.go::TestOracleInstanceIdentityRuleComparesClassesNotAddresses` |
| one program, stable claims, oracle verdict | `integration/programs/runtime_slot_instance_tag.gy` in `conformanceStandalone()` and `conformance-matrix.json`; the identity form itself is compared through the CLI rows rather than a nondeterministic corpus stdout |
| the record leg | 11 new sources through `tools/recmerge`, including the caught raises, identity comparisons and the stable conformance program itself; `tools/recmerge` refuses an entry the compiled leg will not agree to |
| the ledger | `337 + 21` divergences, both adjudicated by shard, all still on the ledger |

## Alternatives rejected

- **Keep `TagInt` and patch the printer.** Rejected: `len`, membership and the loop all obey the same tag;
  patching the printer would leave three doors naming a kind the program never stored.
- **Infer a class name only from the variable name.** Rejected: `xs.append(C())` has no variable, and slot 0
  already records the dispatch id the constructor used. The compiler can name obvious cases, but the object is
  the authority.
- **Print `object` without a class table.** Rejected: ADR 0271 already established that a TypeError naming a
  value must name its real kind; `object` is the fallback when the object cannot answer, not the normal answer.
- **Print a fake real-looking pointer.** Rejected: an address is not part of the semantic claim and would make
  byte-for-byte oracle rows nondeterministic.
- **Apply a broad `0x...` normalizer.** Rejected: it would hide unrelated hex output and turn an actual wrong
  answer into a pass. The normalizer matches only CPython's identity form.
- **Answer the two static membership shapes as `False`.** Rejected: CPython raises, and a program that catches
  `TypeError` must be able to catch this one.
