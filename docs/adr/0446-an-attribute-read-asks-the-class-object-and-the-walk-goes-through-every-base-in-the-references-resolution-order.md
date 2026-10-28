# ADR 0446 — an attribute read asks the class OBJECT, and the walk goes through EVERY base in the reference's resolution order

- **Status**: Accepted
- **Date**: 2026-10-29
- **Cycle**: the one after **ADR 0445** (which closed **Gap R.502**; row **L12.4** was standing in the Open queue)
- **Roadmap**: row **L12.4** done, paying the two rows it owns — **Gap R.56** (a class body binds no attributes) and
  **Gap R.66** (attribute lookup stops at the first base); **Gap R.19** and **Gap R.431** stay open and are named by the
  doors this ADR opens; **Gaps R.505**, **R.506** and **R.507** are filed here as the residue the sweep found.
- **Builds measured**: parent `73f7eb1` (**ADR 0445**) and this commit, every byte from a build made with the gate's
  `-tags=llvm20`.

## Context

Row **L12.4** ("the class object: class attributes, MRO, the enum idiom") promised two things, and the surface survey
that filed **Gap R.56** and **Gap R.66** had measured both as missing. `ADR 0418` had already taught the backend to
KEEP what a class body binds (`class C:` / `first = xs[0]` / `print(C.first)` prints 1), so the table existed — what
was missing was the walk. Measured on the parent build:

```
$ gustyc --eval 'class C:
    X = 1
print(C().X)'
0                                               reference: 1        ← exit 0, the answer class this repo ranks worst

$ gustyc --eval 'class Base:
    NAME = "base"
class Sub(Base):
    pass
print(Sub.NAME)'
gustyc: jit: codegen: Sub.NAME is refused: the class names no member of that name …
                                                reference: base

$ gustyc --eval 'class A:
    NAME = "a"
class B(A):
    def show(self):
        return self.NAME
print(B().show())'
0                                               reference: a

$ gustyc --eval 'class A:
    X = 1
class B(A):
    pass
b = B()
b.X = 9
print(b.X, B().X, A().X)'
9 0 0                                           reference: 9 1 1    ← one of three reads answered
```

Twenty-nine programs were measured into this one shape before the door was written, and they fall into exactly the four
questions the row names: what a class body binds, read through an INSTANCE; what a BASE binds, read through the class or
the instance; which of two bindings wins; and what a METHOD that hands back `self.X` answers.

The reference's rule is one sentence — look in the object's own row, then walk the class and every base of it in
method-resolution order — and this backend had half of it: the object's row (`@rt_inst_get`) and the class's own table
(`classInfo.attrs`, consulted only when the read's base was the class NAME). Everything that crossed the two — an
instance of a class whose member came from a base, a method reading `self.X` where X lives in the class — fell through
to an unwritten slot and printed the digit 0.

## Decision

**One walk, asked by every attribute read, in `pkg/lang/codegen_class_chain.go`.**

1. **`classLinearization`** answers WHICH classes, in the C3 order the reference uses: `lin(C) = [C] + merge(lin(B1),
   lin(B2), …, [B1, B2, …])`, a head taken only when it is in no other list's tail. C3 is not decoration here:
   `class D(B, C)` with both `B` and `C` deriving from an `A` that binds the member must find `A` once and after both,
   and a depth-first walk with duplicates answers `B`'s, `C`'s or `A`'s binding depending on which list it exhausts
   first. The walk memoises per class (`classLinCache`) and carries a visit set (`classLinVisits`) so a cyclic base
   list terminates. Unknown bases (a builtin, a name the source never defines) drop out rather than failing the build —
   the read that walks off the end refuses with the sentence that names the member, which is the honest answer for a
   class that binds nothing.

2. **`classChainMember`** answers WHAT the first class in that order binds, and keeps the two tables **ADR 0418**
   separated: a member whose bound expression lowers to the word the reference prints, and an OWED member whose value
   this road cannot answer faithfully — a fractional, verdict, void or container binding. Owed stays refused, in the
   sentence that names the kind and **Gap R.431**; answering `C().ratio` with the payload word would print `1.0` for
   `1.5`, which is the answer class this contract ranks worst.

3. **The object's word wins, and that question is asked statically first.** `instanceAttrWrites` collects every value
   the program writes to that name on an instance of the class — `obj.X = …` where the walk knows `obj`'s class, and
   `self.X = …` in a method the class OR ANY BASE declares, because an inherited `__init__` writes the attribute on a
   subclass's instance too. Three roads follow from the answer, and they are the reason this is not one door:
   - **no write exists** → the read is a compile-time fact: the class's value is lowered where the read stands, and the
     module emits NEITHER `@rt_inst_get` NOR the presence row. The module-shape rows pin that absence, because a door
     that asked presence for every read in the language would make every program that never shadows pay for a case it
     does not have.
   - **a write exists AND the class binds the name** → which word belongs to a given object is a run-time fact, and the
     read asks the presence row `@rt_inst_has` — written by `@rt_inst_put` for exactly this question (**ADR 0235**) and
     never once asked by a plain attribute read before this commit. The two arms are joined by a `phi`; the class's
     value is spliced into the else-arm only when it lowers to one basic block, because half-compiling a value across a
     join would leave uses its block does not dominate.
   - **a write exists and no class binds the name** → the plain slot read it always used.

4. **The kind doors ask the same walk.** A value road that answers a text while the tag road still says Int prints the
   intern table's index — which is what `print(B().NAME)` did, and what a method handing back `self.NAME` did. Three
   doors were widened to the chain: `methodReturnsStr` gains `methodReturnsClassText` (a body whose `return` hands back
   `self.NAME`, where the class or a base bound a text, hands back a text — the same lesson **ADR 0445** learned for a
   hand-off's parameter, arriving from the class side); the constant-fold road `stringVal` answers the class's text only
   when NO instance write exists (folding it otherwise would print the class's word for an object holding another); and
   `textDoorIsText` gains `classMemberReadIsText`, the door that says "whatever word this read answers, it is an index
   into `@str_tab`" — which is true when the class binds a text and every write the program makes is a text too.

5. **`@rt_inst_has` is asked, but **Gap R.19** is not closed.** The presence row answers "did the program write this
   attribute on THIS object"; a name that neither the object road nor the class chain binds still answers the digit 0
   where the reference raises `AttributeError`. That row predates the class object and stays open; what changed is that
   the raise now has somewhere to live, because the read already asks whether a binding exists.

## Alternatives rejected

- **Depth-first, first-base-wins lookup.** Cheaper, and it is what **Gap R.66** measured on the retired engine. It gets
  `class D(B, C)` wrong whenever `B` and `C` share a base, and gets the sibling order wrong for `class C(A, B)` where
  both bind the member — the reference's answer is `A`'s, and a first-base walk answers `B`'s by accident and `C`'s by
  luck.
- **Presence-check every attribute read.** Correct and uniformly so, and it would have shipped in five fewer lines —
  but it puts a load, an `icmp` and a branch on every read in every program, including the ones where the answer is a
  literal the compiler can see. The static-first rule keeps those at zero instructions, and the guard asserts the
  absence rather than trusting it.
- **Store class members in the instance slot array at instantiation.** This is how a toy gets `C().X` right cheaply,
  and it destroys the class object: `C.X` and `C().X` would disagree after a shadow, and a member bound by a base would
  have to be copied into every object at construction — a cost the reference does not pay and a semantics the reference
  does not have.
- **Read the class chain in `value()` only, leaving the kind doors.** Measured, not hypothesised: it is the build that
  printed `0 1` for `print(c.NAME, C().NAME)` after the value road was right — the value answered an index and the tag
  said Int.
- **Answer an owed member by boxing it.** **Gap R.431** owns the tagged value word; borrowing it here would bundle that
  row's decision into this one and re-open the argument **ADR 0418** already settled about where the read lowers.

## Consequences

- The four shapes the row names answer, on both witness legs and both execution legs: 29 measured programs became 24
  answering rows (`pkg/lang/class_object_test.go`, `integration/class_object_test.go`) plus `integration/programs/class_object.gy`,
  which joined the corpus byte-identical. **29 record entries (10168 → 10197)** were written from reference/compiled
  agreements — 28 programs plus the conformance program's whole source — and `tools/recmerge` refused none of them.
- The class-member table is now consulted by three roads that used to keep their own copies of the question — the value
  road, the constant-text road, and the method answer-kind road. A later change to the walk is a change to all three,
  which is why the walk lives in one file and the doors only ask it.
- **Gap R.19** ("a missing attribute answers 0") is now the ONLY wrong-answer row the family has left: it is the case
  the presence check answers "no" to and the class chain has nothing for.
- Three rows are filed rather than quietly left measured. **Gap R.505**: a class body is EXECUTED by the reference, so
  `class A: X = 1; Y = X + 1` binds `Y` to 2, and this backend refuses `undefined name "X"` because it reads the body
  twice — once into the class table, once as ordinary code where the name is not in scope. **Gap R.506**: the class
  attribute STORE — `C.N = C.N + 1` and `Sub.NAME = "sub"` — refuses, because the store road only re-binds a member the
  body already bound with a value it can hold. **Gap R.507**: `print(C.__doc__)` for a class with no docstring prints
  the empty string where the reference's class object holds `None`; row **P2.13** says `__doc__` is done and it is, for
  the classes that HAVE a docstring, which is why this is a fresh row and not an edit to that one.
- The instance road's old behaviour is preserved where it already answered, and the ADR 0418 control
  (`TestTheInstanceRoadIsUntouchedByTheClassRoad`) still passes: an attribute the program writes on the object keeps the
  plain slot read, and 265 corpus programs printed identical bytes on both builds — the only program in the corpus that
  differs is `forwarded_answer.gy`, which the parent refuses and this build answers, exactly as **ADR 0445** intended.

## Machine path

Nothing new is asked of the CLI: `gustyc --emit-llvm "<source>"` (a SOURCE STRING, not a path — a path is parsed as
source and answers a parse error; `docs/operations.md`'s flag table is the authority) shows the shape an agent needs to
check without running anything. Two module-level claims are asserted by the new rows: a read the class object answers
emits neither `call i32 @rt_inst_get(` nor `call i32 @rt_inst_has(`, and a read the program may have shadowed emits
both. `--eval` and `--file` are both exercised on every answering row, which is the pair **Gap R.504** proved can
disagree. Exit codes are unchanged (**ADR 0006**): exit **1** for the owed-member and store refusals, exit **2**
forbidden.

## Where the proof lives

`pkg/lang/class_object_test.go` (14 instance-read programs + 9 chain programs, each judged on the compiled leg AND the
record leg; 5 shadowing programs on both legs; 4 module-shape rows; 7 filed rows naming **Gaps R.431**, **R.19**,
**R.505**, **R.506**, **R.507**) and `integration/class_object_test.go` (26 CLI programs re-derived from a live
`python3` on both execution legs, 4 refusal rows at exit **1**). The pre-existing family keeps its own guard:
`pkg/lang/a_class_member_read_by_name_test.go` and its CLI twin, whose instance-road control is the negative this row had
to keep.
