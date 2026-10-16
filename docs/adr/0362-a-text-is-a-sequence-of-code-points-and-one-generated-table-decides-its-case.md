# ADR 0362 — A text is a sequence of code points, and one generated table decides its case

Status: accepted
Date: 2026-10-16
Amends: [ADR 0229](0229-a-string-is-an-index-into-a-table-the-runtime-can-add-to.md) (whose closing sentence listed "compiled case folding is ASCII only and `strip` trims the ASCII whitespace set" as what its closure did NOT claim — this ADR claims both), [ADR 0225](0225-a-string-index-is-a-one-character-string.md) (whose "a position is a code point and never a byte" rule is extended to the two doors that had not followed it)
Depends on: [ADR 0302](0302-the-interpreter-is-retired-and-one-compiled-backend-is-the-language.md), [ADR 0166](0166-unsupported-lowering-is-a-diagnostic.md), [ADR 0232](0232-a-container-word-means-its-payload-and-its-tag.md)'s one-question-one-asker rule as [ADR 0229](0229-a-string-is-an-index-into-a-table-the-runtime-can-add-to.md) states it, [ADR 0171](0171-generated-runtime-constants-come-from-the-canonical-tables.md), [ADR 0173](0173-a-runtime-block-travels-with-the-module-that-references-it.md), [ADR 0354](0354-a-runtime-block-is-emitted-only-for-a-module-that-references-it.md), [ADR 0263](0263-a-rounding-rule-is-measured-not-invented.md) (the sweep shape), [ADR 0187](0187-a-slot-carries-its-tag-beside-its-payload.md), [ADR 0214](0214-the-trap-message-is-the-reference-s-wording-not-the-runtime-s.md), [ADR 0249](0249-a-refusal-names-what-is-missing-and-never-blames-a-construct-the.md)
Roadmap rows: L11.5 (paid — code-point strings), Gap N.2 (closed by L11.5 — strings measured bytes), Gap R.16 (closed — iterating a run-time string), Gap R.47 (its "not claimed" sentence shortened), Gap I.2 (still owed — `count`/`find`/`split`/`replace`/`startswith`/`encode` on a run-time receiver), Gap R.308 (still owed — a format spec over a value), Gap R.235/R.46 (still owed — the element of a container the compiler cannot kind)

## The gap, as measured

`len`, `s[i]`, `s[i:j]` and `for c in s` had already been won back from bytes by ADR 0225 and ADR 0229:
`print(len("café"))` answers `4` and `print("héllo"[1])` answers `é`, on the literal road and through a
parameter. What was left was the two doors that transform a text — the case doors and the trim doors — and
the one predicate that decides whether a name holds a text at all. All three failed silently.

| program | compiled at HEAD | CPython |
|---|---|---|
| `def f(s): return s.upper()` / `print(f("straße"))` | `STRAßE` — the ASCII loop upper-cased the ASCII and copied the rest | `STRASSE` |
| `print("straße".upper())` | `STRAßE` — Go's SIMPLE mapping has no room for the sharp s | `STRASSE` |
| `print("ﬁ".upper())` | `ﬁ` | `FI` |
| `print("İ".lower())` | `i` — one character, the dot above dropped | `i` + U+0307 — two characters |
| `print("ΑΣ".lower())` | `ασ` | `ας` — the final sigma |
| `def f(s): return s.lower()` / `print(f("ÄS"))` | `Äs` — the ASCII fold touched `S` and copied the rest | `äs` |
| `print("é.é".title())` | `É.é` — a word was only a word after a space | `É.É` |
| `print("ab-cd".title())` | `Ab-cd` | `Ab-Cd` |
| `print("ǅ".title())` | `Ǆ` — the titlecase letter went to title-case-as-uppercase | `ǅ` |
| `def f(s): return s.title()` / `print(f("é.é"))` | refused: `string-method title on a receiver that is not a text…` | `É.É` |
| `def f(s): return s.swapcase()` / `print(f("İ"))` | refused: the same sentence | `i` + U+0307 |
| `def f(s): return s.strip()` / `print(f("ßﬁ"))` | `1` — the intern INDEX, printed as a number, at exit 0 | `ßﬁ` |
| `print("\x1cx".strip())` | `\x1cx` | `x` |
| `print("café x".strip("c"))` | `café x` — the cutset was never read | `afé x` |
| `def g(): return "café"` / `def f(s): return "é" in s` / `print(f(g()))` | `False` | `True` |
| `def f(s): x = s[1:3]; return x` / `print(f("café"))` | `1` | `af` |
| `def f(s): return s[1:3]` / `print(f(g()))` | refused with a **list**-shaped message | `af` |

None of these raised. Every one exited 0. ADR 0166 counts that as the worst class of answer this compiler
can give: a value the reference never produces, shipped without a word.

## The decision

1. **The case answer comes from a generated table, and the table is asked of the reference.**
   `tools/casetab` runs `tools/casetab/probe.py` — a Python program that asks CPython, for every code point
   in `0x0..0x10FFFF` (the surrogates excepted, which no well-formed string can hold), four things:
   `chr(cp).upper()/.lower()/.title()/.swapcase()` where they differ from the character; whether the
   character is **cased** (`("1"+c+"a").title()` keeps the `a` lower); whether it is **case-ignorable** (both
   sigma looks — `("ΑΣ"+c).lower()` must still end final, `("ΑΣ"+c+"Β").lower()` must reach the `Β`); and
   whether `c.strip()` is empty, which is the whitespace set `strip`/`lstrip`/`rstrip` trim. The artifact is
   `pkg/lang/testdata/case-table.json` (`meta.reference` names the interpreter, `meta.counts` is its own).
   Hand-typing this is not available: a table over 1.1 million code points is a claim nobody can review, and
   the four properties that matter are the ones the *reference acts on*, not the ones a Unicode data file
   labels — `ͅ` (GREEK PROSGEGRAMMENI) is cased and case-ignorable at once, which is why `"ͅΣ".lower()`
   answers `ͅσ` and not `ͅς`, and only the behavioural probe can say that.

2. **One artifact, two readers.** `pkg/lang/case_table.go` embeds it and is the only thing that decides a
   case or trim answer: the compile-time fold (`caseFoldFold`, `caseFoldTrim`) and the runtime door
   (`rt_str_case`, `rt_str_trim`, emitted by `pkg/lang/case_runtime.go`) read the same file. This is ADR
   0229's rule — the print path and the operation path must ask one predicate — applied to meaning instead
   of printing: a fold and a door that keep separate implementations are what produced the `straße`/`CAFé`
   pair above, and `TestTheFoldAndTheDoorAnswerTheSameCase`/`…TheSameTrim` run the claim by compiling each
   text twice, once where the compiler can read it and once where it cannot, and comparing both to CPython.

3. **The door decodes, maps, encodes, and never reinterprets a byte it does not understand.** Mapping
   happens on code points because an answer can be several characters (`ß`→`SS`, `ﬁ`→`FI`,
   `İ`→`i`+U+0307); the sigma rule indexes the ORIGINAL code points, because that is what the reference's
   scans read. A byte that is not a well-formed sequence is carried as a raw marker (`0x110000 + byte`) and
   written back unchanged, so the door cannot corrupt what it copies — the choice the `list.sort` door
   refused to make (Gap R.309) is not on offer here, because a byte has no order to invent but it does have
   an identity to preserve.

4. **Trimming reads the same artifact, over code points.** The old door compared each byte against 32 with
   a **signed** `icmp`, so every byte from 0x80 up — every byte of every non-ASCII character — counted as
   whitespace; the fold asked Go's `unicode.IsSpace`, which leaves U+001C..U+001F standing. `ws` in the
   artifact is the reference's set, asked by asking, and `caseTrimBytes` (Go) and `rt_str_trim` (IR) both
   walk it a character at a time. A cutset the program supplied is a set of *characters* too, read as code
   points; an empty cutset trims nothing, which is what the reference does and not what a naive loop does.

5. **The block is gated twice** (ADR 0354). `g.caseOps` holds one bit per operation; the case tables and the
   trim tables are emitted independently, so a program that only upper-cases carries no whitespace set and a
   program that only strips carries none of the 1.1-million-code-point case answer. `TestTheCaseBlockIsGated`
   and `TestTheTrimBlockIsGated` ask both directions, and the folded case (`"straße".upper()` with the text
   in front of the compiler) emits neither.

6. **"Is this a text?" is asked once, by the pre-pass** (ADR 0229's rule, finally followed for the whole
   family). `strArgKinds` in `pkg/lang/strargs.go` decides which parameters receive texts. It had three
   sources of evidence — an annotation, a text default, a call site that passes a literal or a forwarded
   text variable — and nothing for a call's answer, a container element, or a slice/index of a text. Each
   missing arm was a silent wrong answer in the body: the parameter arrived unclassified, the body asked its
   *container* roads about a text, and `print` rendered the intern index as a number. `strArgKinds` now
   keeps a conservative table (`textFuncs`: EVERY return of the body answers a text, not any return — the
   loose table decides a print, this one decides a representation, where a wrong "yes" prints a number
   through the string table) and accepts `f(g())`, `f(xs[0])` of a literal list of texts, `f(s[1:3])`;
   and `isStrExprIn` grew the `*Slice`/`*Index` arms its switch had never had, with a depth guard, because
   `x = x[1:3]` writes a binding whose base is the name the binding defines.

## IR implications

The emitted module gains, when asked: `@rt_ct_{upper,lower,title,swap}_{lo,hi,delta}` plus the one-to-many
`{mfrom,mlen,moff,text}` quartets, `@rt_ct_cased_*`, `@rt_ct_ign_*`, `@rt_ct_ws_*`, the searches
`rt_ct_find_range`/`rt_ct_find_from`, the rules `rt_ct_cased`/`rt_ct_ignorable`/`rt_ct_final_sigma`/
`rt_ct_apply`/`rt_ct_map`, the door `rt_str_case`, and the trim readers `rt_ct_ws`/`rt_ct_cp_at`/
`rt_ct_cp_bytes`/`rt_ct_in_set` plus `rt_str_trim`. Every length in the module is printed from the artifact
by `renderI32Global` — no hand-counted `[N x i32]` (ADR 0171), and `@OUTCAPBYTES@` sizes the scratch buffer
in bytes off the artifact's own maximum expansion, which is the bug class that walked off the end of a
code-point-sized allocation the first time this door ran. The old ASCII `rt_str_strip` is gone from the
heap block; `TestTheAsciiTrimDoorStaysDead` fails if a `icmp sgt i8` ever returns to a module.

## Alternatives rejected

- **Go's `strings.ToUpper`/`Title`/` capitalize` and `unicode.IsSpace`.** This is what shipped, and it is
  the first row of the table above: Go implements Unicode's SIMPLE case mapping, which has no room for
  `ß`→`SS`, for a conditional sigma, for a titlecase digraph, or for the four separators the reference
  strips. A fold that borrows it also disagrees with its own runtime door, which is a worse shape than
  agreeing on a wrong answer.
- **A hand-written table in a Go file.** 1 112 064 code points, 4 526 cased, 2 707 case-ignorable, 29
  whitespace, plus 227 one-to-many answers. `tools/casetab -check` exists so the file's provenance is
  re-askable; a typed table is not.
- **A runtime comparator for case, the way `list.sort` got one.** The case question is not comparator-shaped:
  `"ΑΣ".lower()` depends on characters two positions away, and `title` depends on the character before. The
  door therefore decodes into a buffer so the rules can look at neighbours, exactly as the reference does.
- **Answering `title`/`capitalize`/`swapcase` by composing `upper`/`lower`.** They are their own tables:
  `'ǅ'.swapcase()` is `'ǅ'`, `'ǅ'.title()` is `'ǅ'`, and `'1a'.capitalize()` is `'1a'` — the reference
  title-cases the *first character*, not the first cased one.
- **Leaving the shape refused instead.** `title`/`capitalize`/`swapcase` on a run-time receiver were
  refusals, and a refusal is honest — but a refusal of a program the reference runs is a gap, not a
  closure, and ADR 0166's rule is that the answer is owed rather than the sentence.
- **A second representation for a text (UTF-16, or a decoded array).** ADR 0224's interned `@str_tab` index
  is one word per value and every door already takes it; the decode is local to the door that needs
  neighbours, and `len`/`s[i]`/`s[i:j]` already measure code points through ADR 0225's helper.

## What this does not claim

`count`, `find`, `index`, `split`, `replace`, `startswith`, `endswith`, `encode`, `zfill`, `ljust`, `rjust`
and `join` with a run-time-argument still refuse, honestly, under Gap I.2. So does a format spec over a
value the compiler cannot read (Gap R.308), `s * 2` on a text in a variable (Gap R.33/R.82), and the element
of a container the compiler cannot kind (Gap R.46/R.146). The artifact is pinned: `tools/casetab -check`
re-asks the reference and compares the tables (not the provenance, which is a fact about the machine), and
`TestTheCaseTableIsTheArtifactItClaims` checks the counts against the arrays beside them.
