# A capture travels one step: the frame that owns a name answers for the one below it

**Roadmap:** Gap R.330 (closed here) · L11.7 (the row the applied `lambda` belongs to) · ADR 0370 (the capture
parameter an application fills, and the gate that refuses a capture whose kind is unaskable) · ADR 0372 (the union a
clone carries: the entries its calls name beside one capture parameter per free name) · ADR 0245 (the nested-`def`
lift, which has always carried a name down one frame at a time) · ADR 0345 (an entry is a bare symbol; the pass is
all-or-nothing) · ADR 0273 (a door that cannot answer says so; a wrong value at exit 0 is the defect this repo files
rather than ships) · ADR 0166 (exit 2 is a compiler bug, and `multiple values for argument` is not an answer) ·
ADR 0343 (no process state a concurrent compile can answer with) · ADR 0302 (one backend, so a rule missing on one
road has no second engine to catch it) · Gap R.332 (the sibling this rule inherits: a capture whose value is not a
number is refused, at any depth)

## Status

Accepted. Landed 2026-10-19.

## Context

Three lines of ordinary source, which the reference answers `111`:

```python
print((lambda x: (lambda y: (lambda z: x + y + z)(100))(10))(1))
```

The toolchain answered exit 1:

```console
gustyc: jit: codegen: lambda_2() reads "x" from the frame that encloses it, and that name holds a value no call in
        the program can be asked about: a capture crosses into the generated function as one i32 word, which cannot
        say what the value is …
```

Two-frame applications have answered since **ADR 0370**: an applied `lambda` that reads a name its own parameters do
not bind gains one capture parameter per such name, and the application fills it with the caller's own reading of that
name. The rule that decides *whether* a name may cross that boundary is `captureParamWord`: a capture crosses as one
`i32`, so the door asks the value tables the print road and the arithmetic roads already read, and for a name that is
a **parameter** it asks every call of the frame what it hands to that position. That question is answerable for a
parameter the program spelled. It has no answer for a parameter the COMPILER appended:

| the frame | its parameter list | what a call site hands to `%p1` |
|---|---|---|
| `lambda_0` = `lambda x: …` | `[x]` | `1` — the application's own argument |
| `lambda_1` = `lambda y: …` | `[y, x]` — `x` appended by the capture road | *nothing the program wrote* |
| `lambda_2` = `lambda z: …` | `[z, x, y]` — two appended | *nothing the program wrote* |

`callArgAt` walks the application's arguments by position and by the keyword spellings the SOURCE used. A capture
parameter is spelled by `lambdaCallArgs` at emission time, on a copy of the call, in a tree no scan can see. So the
scan returned `captureWordUnaskable` — the sentence above — and the gate refused, faithfully, a question that had been
asked of the wrong table. The two-frame twin answered only because its forwarded name sits at an index the outer
application happens to fill positionally… and it answers for the wrong reason: the value that arrives there is the
caller's read of a name in ITS frame, which is a question about the caller, not about the call.

The roadmap row prescribed the shape of the fix — *an intermediate `lambda` re-exports the names its own body is free
in but does not bind, so the innermost capture is filled by the intermediate call site and the outer frame is never
reached directly* — and that is what a nested `def` has always done through the `%env` chain of ADR 0245. Walking it
turned up a second defect underneath, in the question that decides which names a frame forwards at all.

## Decision

**A capture travels one step, and the frame that owns the name answers for the frame below it.** Two changes, one per
question, both of them about a frame rather than about a call site.

### 1. A free name is a name the body is free in, not a name a nested body reads

`scanFreeIn` reached a nested `lambda` by handing the walk to `lambdaFreeNames`, which seeds its own binder set with
that lambda's parameters and reports everything else straight into the enclosing scan. `y` is bound by the middle
frame, so it is not free in it — but the nested body reads it, and the report said it was. The middle frame therefore
gained `y` as a capture parameter beside the `y` it already spelled, `lambdaCallArgs` appended the keyword twice, and
`callUserFunc` refused the program with `multiple values for argument "y"`, the reference's own words spoken by a
compiler that had invented the collision. The nested lambda's reads are now filtered against the binders of the body
being asked about, which is the rule the comprehension arm beside it already spoke:

```go
// pkg/lang/codegen_func.go — scanFreeIn
case *Lambda:
	inner := map[string]bool{}
	lambdaFreeNames(x, inner)
	for nm := range inner {
		if !bound[nm] {
			out[nm] = true
		}
	}
	return
```

The middle frame gains `x` and not `y`; a shadowed name (`(lambda x: (lambda x: …)(5))(99)`) is forwarded nowhere;
and the deepest body still reports `x` and `y` free in ITSELF, because its own scan is asked with its own binder set.

### 2. The supplying frame's verdict is the receiving frame's answer

`emitLambda` asks the capture question **before** the body is lowered — the ordering ADR 0370 chose so a forwarded
name cannot be described afterwards as something the print door would misread — and the call road asks the same
question at the application. Both ask it while the SUPPLYING frame is the one on the tables. That verdict is now kept,
per generated frame and per forwarded name, and is what the frame answers when a body applied inside it asks about
that name:

```go
// pkg/lang/codegen_call.go — captureParamWordOf, before the call-site scan
if ans, ok := g.mintedCaptureVerdict(fd, name); ok {
	return ans.kind, ans.carries
}
```

Three rules keep it honest:

* the table is keyed by the `*FuncDef` and the name, allocated with the module (`g.lambdaCaptureVerdicts`,
  initialised beside `g.lambdaOrigin`), so two compiles in flight cannot answer each other's capture question
  (**ADR 0343**);
* the name must sit in the **forwarded tail** of the parameter list — the first parameter of that spelling must be at
  or past the source parameters — so a name the frame spells itself stays the program's own and keeps being asked of
  its call sites;
* an entry is only ever written with a verdict the gate actually reached. No entry is not a verdict: the question
  falls back to the call-site scan, and a scan with no argument to read is still the refusal it always was.

The consequence is the one that matters: the deep road **carries a verdict, it does not manufacture one**. A chain
whose outermost body hands a text, a float or a container to the frames below is refused at exit 1 exactly as the
same read written one frame deep is, naming the kind and Gap R.332 (**ADR 0273**).

### What the emitted IR looks like

```llvm
define i32 @gy_lambda_2(i32 %p0, i32 %p1, i32 %p2)   ; z spelled, x and y forwarded
define i32 @gy_lambda_1(i32 %p0, i32 %p1)            ; y spelled, x forwarded — y NOT forwarded
define i32 @gy_lambda_0(i32 %p0)                     ; the outer body's own x
  …
  %t12 = call i32 @gy_lambda_1(i32 10, i32 %p0)                     ; filled from the outer frame
  %t6  = call i32 @gy_lambda_2(i32 100, i32 %p1, i32 %p0)           ; filled from the MIDDLE frame
```

Every argument of an inner application is a register the CALLING `define` was handed, or a constant. No `ptrtoint`,
no `inttoptr`, no indirect `call`: ADR 0345's rejected `fnptr` alternative stays rejected, and the forwarding is a
parameter list plus a call.

## What this row does not claim

* **A capture whose value is not a number** still refuses (Gap R.332, L11.1). The verdict is forwarded, not widened.
* **The entry side** still refuses: an `lambda` handed to a higher-order `def` that reads the frame its application is
  written in is Gap R.336, where the word has no symbol to travel in because an entry is a bare symbol (ADR 0345).
* **A higher-order body written inside a `lambda`** — a body that calls one of its own parameters at the bottom of a
  chain — is still declined by the pass (`r.inLambda` marks the call as the closure road's frame) and refuses with
  `"f" is called but is not a function this backend can build a call to`. Measured unchanged by this cycle's probe and
  filed as **Gap R.347**.
* **A comprehension's own target read by a body this deep** refuses with `undefined name "x"`, identically before and
  after this row, and is filed as **Gap R.348**.
* **A chain whose outermost body ANSWERS a container** prints the heap handle — `print((lambda x: […])(1))` is `0`
  where the reference prints a list — which is Gap R.331's missing answer-kind registration, one frame deep already on
  the pre-ADR build (`print((lambda x: [x for q in [1, 2]])(9))` prints `0` there too). Pinned in the filed-not-fixed
  table with the one-frame control beside it, not recorded: `tools/recmerge` refuses an entry whose legs disagree.

## Consequences

The family that answers is every chain whose forwarded values cross in one word, whatever the spelling that owns them:
a name owned by a lambda two frames up, one owned by a `def` local with two lambdas in between, one owned by a `def`
two frames up with a closure lift in between (the shape ADR 0245 carried for a nested `def` and the lambda road did
not), a chain inside a `for` body, and a chain that also calls one of the body's own parameters where the pass can
build the call. `--callables` output is unchanged: the forwarding is the closure road's, and the pass's account
describes the pass.

## Alternatives rejected

* **Ask the call sites harder.** The scan cannot find an argument the program never wrote; every widening of
  `callArgAt` toward the emitted keyword arguments ends in reading the compiler's own bookkeeping as if it were source.
  Rejected.
* **Skip the gate for a forwarded name.** One line, and it converts a refusal into an exit-0 print of a string-table
  index or a heap handle — ADR 0166's class of bug, shipped. Rejected.
* **Reach the outermost frame directly**: give the deepest body a parameter filled from the frame that OWNS the name,
  passing through no intermediate. It works only while every intermediate frame is emitted inside the one call site
  that owns it, which is true for the two lambdas above and false for a `for` body, a comprehension, a closure lift
  and a method frame. It also loses the locality that makes the emitted call readable: each application names the
  frame it is written in. Rejected.
* **Reuse `%env`.** The closure lift has a boxed chain for exactly this; a `lambda` applied where it is written has no
  environment at all (ADR 0370), and putting one under the forwarding road to answer a type question would give the
  lambda road a second representation of its own frame — which is the shape ADR 0273 exists to prevent. Rejected.
* **Mint a fresh `define` per chain depth**, specialised to the outer frame's registers. Compiles twice as much text
  to say the same thing, and pins the answer to one call site, which two applications of one body already are not.
  Rejected.
* **Carry the kind beside the value (two words per capture).** That is the tagged value word of L11.1 — the real fix
  for Gap R.332 and the reason this row is scoped to values that already cross in one word. Rejected as out of scope.

## Tests

`pkg/lang/lambda_applied_test.go` — six new rows in the answered tables (three binding forms deep, four deep, the
middle frame binding one of the names itself, a `def` local owned three frames up, a shadowed name, two names with two
owners) walked on the compiled leg, the reference leg and the record leg; `TestWhatADeepCaptureIsForwardedByTheFrameThatOwnsIt`
asserts the five IR facts above (three `define` headers with their own forwarded tails, both inner calls filled from
the registers of the `define` they sit in), forbids an argument register past any frame's arity, forbids
`ptrtoint`/`inttoptr`, and puts every module through `llc`; `TestWhatAShadowedCaptureIsNotForwardedTwice` fails if any
generated frame declares a parameter name twice. Three new refusal rows keep the honesty of the verdict — a container,
a text and a float read at the bottom of a chain are refused, each naming the kind — and two rows join the
filed-not-fixed table beside the one-frame control that proves the answer door, not this road, owns them. The row's
own pinned refusal (`a name two lambdas up`) is DELETED from the refusal table and re-appears as an answered row, as
its Definition of done requires.

`integration/lambda_applied_test.go` — six CLI rows against `python3` on the linked and in-process roads, each
measuring the reference inside the case, plus the two depth-honesty refusals and the exit-2 prohibition.
`integration/programs/lambda_capture_chain.gy` is the new matrix row `programs/lambda_capture_chain`, oracle `match`,
byte-identical to the reference, including the chain written across a closure lift, a `for` body and a method frame.
Eight new sources went to `tools/recmerge` (the record to **7957**).
