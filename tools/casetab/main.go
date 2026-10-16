// tools/casetab — generate `pkg/lang/testdata/case-table.json`, the case-mapping table the compiled
// backend answers `upper`/`lower`/`title`/`capitalize`/`swapcase` from.
//
// A case table is not something this compiler should own. Go's `strings.ToUpper` is Unicode's SIMPLE
// case mapping, which is not what the reference answers: `"ß".upper()` is `SS` and not `ß`, `"ﬁ".upper()`
// is `FI`, `"İ".lower()` is `i` + COMBINING DOT ABOVE, `"ΑΣ".lower()` is `ας` and not `ασ` because the
// last sigma of a word is conditional on the characters around it. Writing those rules by hand into a
// Go file — or worse, into a runtime loop that only knows ASCII — is how a compiler ends up printing a
// value the reference never produces, at exit 0, which ADR 0166 counts as the worst class of answer.
//
// So the table is not typed by anybody. The probe below asks CPython — the definition this project
// follows since ADR 0302 retired the second engine — what every code point maps to, and how the
// reference's own word-boundary and sigma rules behave around it, and the answer is written out
// compressed. `pkg/lang/case_table.go` embeds the artifact and is the ONLY thing that decides a case
// answer: the compile-time fold and the runtime door both read it, so the two cannot disagree
// (ADR 0232's rule, one question asked once).
//
// The three properties the engine needs are all asked behaviourally, never transcribed from a Unicode
// data file, because the property that matters is the one the reference acts on:
//
//   - the per-code-point mappings, from `chr(cp).upper()/.lower()/.title()/.swapcase()` — which is where
//     the one-to-many special casings (`ß`→`SS`, `ﬁ`→`FI`, `İ`→`i̇`) come from for free;
//
//   - "cased", the word-boundary test `title()` uses, from whether a character lets the `a` after it
//     start a word: `("1" + c + "a").title() != "1" + c + "A"`;
//
//   - "case-ignorable", the character the sigma rules step OVER, read with two looks at the same sigma:
//     `("ΑΣ"+c).lower()` must still end the sigma (nothing cased after) and `("ΑΣ"+c+"Β")` must reach
//     the `Β` — both hold exactly when the reference's scan skipped `c`.
//
//     usage: go run ./tools/casetab          # the artifact, on stdout
//     go run ./tools/casetab -write   # write pkg/lang/testdata/case-table.json
//     go run ./tools/casetab -check   # exit 1 if the file on disk is not what the probe says
//
// `-check` compares the tables and not the provenance block: the reference's version string is a fact
// about the machine, and a table certified against CPython 3.12 must not go red on a machine running
// 3.13 while every mapping still agrees (the mapping claim is tested separately, against the local
// reference, by pkg/lang/case_door_test.go).
package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// probeSrc is the reference-side half of the generator, kept in one file so the probe the artifact was
// built from and the probe `-check` re-asks are literally the same text.
//
//go:embed probe.py
var probeSrc string

// artifactPath is where the generated table lives; pkg/lang/case_table.go embeds this exact file.
const artifactPath = "pkg/lang/testdata/case-table.json"

type mode struct {
	Ranges [][]int32   `json:"ranges"`
	Multi  []multiPair `json:"multi"`
}

type multiPair struct {
	From int32   `json:"from"`
	To   []int32 `json:"to"`
}

type doc struct {
	Meta struct {
		Schema      int            `json:"schema"`
		GeneratedBy string         `json:"generated_by"`
		Reference   string         `json:"reference"`
		Codepoints  int            `json:"codepoints"`
		Counts      map[string]int `json:"counts"`
		Cased       int            `json:"cased"`
		Ignorable   int            `json:"ignorable"`
		WS          int            `json:"ws"`
	} `json:"meta"`
	Cased     [][]int32 `json:"cased"`
	Ignorable [][]int32 `json:"ignorable"`
	WS        [][]int32 `json:"ws"`
	Upper     mode      `json:"upper"`
	Lower     mode      `json:"lower"`
	Title     mode      `json:"title"`
	Swapcase  mode      `json:"swapcase"`
}

type probeOut struct {
	Upper      map[string][]int32 `json:"upper"`
	Lower      map[string][]int32 `json:"lower"`
	Title      map[string][]int32 `json:"title"`
	Swapcase   map[string][]int32 `json:"swapcase"`
	Cased      []int32            `json:"cased"`
	Ignorable  []int32            `json:"ignorable"`
	WS         []int32            `json:"ws"`
	Reference  string             `json:"reference"`
	Codepoints int                `json:"codepoints"`
}

func main() {
	mode := ""
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode != "" && mode != "-write" && mode != "-check" {
		fmt.Fprintln(os.Stderr, "usage: casetab [-write|-check]")
		os.Exit(2)
	}
	raw, err := runProbe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "casetab: %v\n", err)
		os.Exit(2)
	}
	var p probeOut
	if err := json.Unmarshal(raw, &p); err != nil {
		fmt.Fprintf(os.Stderr, "casetab: the reference's answer is not the JSON the probe promises: %v\n", err)
		os.Exit(2)
	}
	d := build(p)
	body, err := json.Marshal(d)
	if err != nil {
		fmt.Fprintf(os.Stderr, "casetab: %v\n", err)
		os.Exit(2)
	}
	// Compact, and not for tidiness: the artifact is embedded into the compiler and parsed before the
	// first answer, and a table of a few thousand entries should not cost a generation of indentation to
	// read. It is generated data — the provenance block at the top is the part a person reads.
	out := append(body, '\n')
	switch mode {
	case "-write":
		if err := os.WriteFile(artifactPath, out, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "casetab: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("casetab: wrote %s (%d bytes)\n", artifactPath, len(out))
		summarise(d)
	case "-check":
		have, err := os.ReadFile(artifactPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "casetab: %v\n", err)
			os.Exit(2)
		}
		if sameTables(have, out) {
			fmt.Println("casetab: the table on disk is the reference's answer — nothing to write")
			summarise(d)
			return
		}
		fmt.Fprintf(os.Stderr, "casetab: %s is not what the reference says — re-run with -write\n", artifactPath)
		fmt.Fprintf(os.Stderr, "  (the provenance block is not compared; the tables are)\n")
		os.Exit(1)
	default:
		os.Stdout.Write(out)
	}
}

func runProbe() ([]byte, error) {
	python := os.Getenv("GUSTY_PYTHON")
	if python == "" {
		python = "python3"
	}
	cmd := exec.Command(python, "-c", probeSrc)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s could not run the probe (set GUSTY_PYTHON to point at the reference): %v %s",
			python, err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// build compresses the reference's per-code-point answers into (lo, hi, delta) ranges — every simple
// mapping is a constant offset over a run of code points, which is both what the reference's own tables
// look like and what a runtime binary search wants — and keeps the one-to-many and irregular answers as
// an explicit list. Nothing here decides a case answer; it only re-presents what was asked.
func build(p probeOut) doc {
	var d doc
	d.Meta.Schema = 1
	d.Meta.GeneratedBy = "tools/casetab"
	d.Meta.Reference = p.Reference
	d.Meta.Codepoints = p.Codepoints
	d.Meta.Counts = map[string]int{}
	for name, m := range map[string]map[string][]int32{
		"upper": p.Upper, "lower": p.Lower, "title": p.Title, "swapcase": p.Swapcase,
	} {
		d.Meta.Counts[name] = len(m)
		switch name {
		case "upper":
			d.Upper = compress(m)
		case "lower":
			d.Lower = compress(m)
		case "title":
			d.Title = compress(m)
		case "swapcase":
			d.Swapcase = compress(m)
		}
	}
	d.Cased = ranges(p.Cased)
	d.Ignorable = ranges(p.Ignorable)
	d.WS = ranges(p.WS)
	d.Meta.Cased = len(p.Cased)
	d.Meta.Ignorable = len(p.Ignorable)
	d.Meta.WS = len(p.WS)
	return d
}

func compress(m map[string][]int32) mode {
	keys := make([]int, 0, len(m))
	for k := range m {
		v, err := strconv.Atoi(k)
		if err != nil {
			continue
		}
		keys = append(keys, v)
	}
	sort.Ints(keys)
	out := mode{Ranges: [][]int32{}, Multi: []multiPair{}}
	for i := 0; i < len(keys); {
		cp := int32(keys[i])
		tgt := m[strconv.Itoa(keys[i])]
		if len(tgt) == 1 {
			delta := tgt[0] - cp
			j := i
			for j+1 < len(keys) && keys[j+1] == keys[j]+1 &&
				len(m[strconv.Itoa(keys[j+1])]) == 1 && m[strconv.Itoa(keys[j+1])][0]-int32(keys[j+1]) == delta {
				j++
			}
			out.Ranges = append(out.Ranges, []int32{cp, int32(keys[j]), delta})
			i = j + 1
			continue
		}
		out.Multi = append(out.Multi, multiPair{From: cp, To: tgt})
		i++
	}
	return out
}

// ranges turns a sorted code-point list into [lo, hi] runs.
func ranges(cps []int32) [][]int32 {
	sort.Slice(cps, func(a, b int) bool { return cps[a] < cps[b] })
	out := [][]int32{}
	for i := 0; i < len(cps); {
		j := i
		for j+1 < len(cps) && cps[j+1] == cps[j]+1 {
			j++
		}
		out = append(out, []int32{cps[i], cps[j]})
		i = j + 1
	}
	return out
}

// sameTables compares everything the engine reads and ignores meta, whose reference/version fields are
// facts about the machine that ran the probe.
func sameTables(have, want []byte) bool {
	var a, b doc
	if err := json.Unmarshal(have, &a); err != nil {
		return false
	}
	if err := json.Unmarshal(want, &b); err != nil {
		return false
	}
	la, _ := json.Marshal(stripMeta(a))
	lb, _ := json.Marshal(stripMeta(b))
	return bytes.Equal(la, lb)
}

func stripMeta(d doc) [7]interface{} {
	return [7]interface{}{d.Cased, d.Ignorable, d.WS, d.Upper, d.Lower, d.Title, d.Swapcase}
}

func summarise(d doc) {
	fmt.Printf("  reference=%s codepoints=%d cased=%d ignorable=%d ws=%d\n", d.Meta.Reference, d.Meta.Codepoints, d.Meta.Cased, d.Meta.Ignorable, d.Meta.WS)
	for _, name := range []string{"upper", "lower", "title", "swapcase"} {
		m := map[string]mode{"upper": d.Upper, "lower": d.Lower, "title": d.Title, "swapcase": d.Swapcase}[name]
		fmt.Printf("  %-9s entries=%-5d ranges=%-5d multi=%d\n", name, d.Meta.Counts[name], len(m.Ranges), len(m.Multi))
	}
}
