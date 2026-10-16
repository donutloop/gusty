"""The reference-side probe behind `tools/casetab`.

Asks the reference — CPython, the definition this project follows since ADR 0302 retired the second
engine — three things about every code point, and prints one JSON object. Nothing here decides an
answer; it is the question, asked of the thing that defines it.

  * the four per-code-point mappings, taken only where they differ from the character itself. This is
    where the one-to-many special casings arrive for free: `ß` -> `SS`, `ﬁ` -> `FI`, `İ` -> `i` +
    COMBINING DOT ABOVE — the answers Go's `strings.ToUpper` does not give and a hand-written table
    would miss.
  * "cased": the word-boundary test `str.title()` uses. Asked behaviourally — a character is cased for
    the reference iff the letter written after it is still inside the same word, i.e. iff
    `("1" + c + "a").title()` leaves that `a` lower.
  * "case-ignorable": the character the sigma rules step OVER. Also asked with two looks at the same
    sigma: with nothing after it the sigma must still be final (ς), and with a cased letter after it the
    scan must reach that letter (σ). Both hold exactly when the reference's own scan skipped the
    character — which is the property, not a paraphrase of it.

  * "ws": the characters `str.strip()` removes when it is given no cutset, asked by asking — a code point
    is whitespace iff the reference, told to strip it alone, returns nothing. This is the set `strip`,
    `lstrip` and `rstrip` trim, and it is NOT the set Go's `unicode.IsSpace` knows: the reference removes
    the four separators U+001C..U+001F and Go does not — the answer the hand-written roads got wrong
    (roadmap L11.5, ADR 0362).

The last probe is why `ͅ` (GREEK PROSGEGRAMMENI, category Mn and
titlecase) is both cased and case-ignorable, which is exactly what makes `"ͅΣ".lower()` answer `ͅσ` and
not `ͅς`.
"""
import json
import sys

out = {
    "upper": {}, "lower": {}, "title": {}, "swapcase": {},
    "cased": [], "ignorable": [], "ws": [],
    "reference": "CPython " + sys.version.split()[0],
    "codepoints": 0,
}

for cp in range(0x110000):
    if 0xD800 <= cp <= 0xDFFF:
        continue  # a lone surrogate is not a character a well-formed string can hold
    c = chr(cp)
    out["codepoints"] += 1
    for mode in ("upper", "lower", "title", "swapcase"):
        r = getattr(c, mode)()
        if r != c:
            out[mode][str(cp)] = [ord(x) for x in r]
    if ("1" + c + "a").title() != "1" + c + "A":
        out["cased"].append(cp)
    if ("ΑΣ" + c).lower()[1:2] == "ς" and ("ΑΣ" + c + "Β").lower()[1:2] == "σ":
        out["ignorable"].append(cp)
    if c.strip() == "":
        out["ws"].append(cp)

print(json.dumps(out))
