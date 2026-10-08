// tools/recmerge — add entries to the record leg (pkg/lang/testdata/interpreter-golden.json) only where the
// reference leg (CPython) and the compiled leg (the LLVM JIT) agree, and never let the compiled leg write an
// answer.
//
// The record is not a scratch pad: an entry is the answer the reference gives, cross-checked against the
// compiled backend, and an entry whose compiled leg disagrees is refused here rather than filed as debt. The
// file's own formatting is preserved by keeping the untouched entries as raw JSON and inserting the new ones —
// the record is a reviewed artifact, and a recorder that re-sorts every entry of the record turns a two-entry
// addition into a thousands-of-lines diff a reviewer cannot read (the same reason the ADRs insist a fix change
// only what it decides; ADR 0309's rule, asked of the record's own bytes). The record's size is the artifact
// table's (docs/operations.md) and a test recomputes it from the file, so this comment carries no count to rot.
//
//	usage: go run -tags=llvm20 ./tools/recmerge -sources sources.json [-check]
//
// `-sources` is a JSON array of program sources. Without `-check` each source is asked of the reference; an
// entry already in the record is left alone. With `-check` nothing is written and a source the reference answers
// is an error — the mode for the rows the language refuses in words, which have no answer to record and must not
// acquire one.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/donutloop/gusty/pkg/lang"
)

// recordPath is the file the recorder writes. A var rather than a const so the tests can point it at a copy.
var recordPath = "pkg/lang/testdata/interpreter-golden.json"

// entry is one record entry. The field ORDER and the `omitempty` set are the file's: an entry that grew a field
// the record does not use shows up as a change to an entry nobody changed.
type entry struct {
	Repr    string `json:"repr,omitempty"`
	Type    string `json:"type,omitempty"`
	Int     int64  `json:"int,omitempty"`
	HasInt  bool   `json:"hasInt,omitempty"`
	Err     string `json:"err,omitempty"`
	ExnType string `json:"exnType,omitempty"`
	ExnMsg  string `json:"exnMsg,omitempty"`
	Stdout  string `json:"stdout,omitempty"`
	HasStd  bool   `json:"hasStdout,omitempty"`
}

type doc struct {
	Entries map[string]json.RawMessage `json:"entries"`
	Meta    map[string]json.RawMessage `json:"meta"`
}

// legs are the two witnesses. They are arguments rather than calls so the tool's own test can hand it
// disagreements and traps without building the compiler.
type legs struct {
	ref      func(src string) (stdout, stderr string, err error)
	compiled func(src string) (output, stderr string, code int, err error)
}

// realLegs asks CPython and the LLVM JIT.
func realLegs() legs {
	return legs{
		ref: lang.PythonRun,
		compiled: func(src string) (string, string, int, error) {
			res, err := lang.JIT(src, 0)
			if err != nil {
				return "", err.Error(), 1, err
			}
			return res.Output, res.Stderr, res.Code, nil
		},
	}
}

func main() {
	sources := flag.String("sources", "", "JSON array of program sources")
	check := flag.Bool("check", false, "ask the rows only: nothing is written")
	flag.Parse()
	if *sources == "" {
		fmt.Fprintln(os.Stderr, "usage: recmerge -sources FILE [-check]\n"+
			"  FILE is a JSON array of program sources (or {\"sources\": [...]});\n"+
			"  each source is asked of CPython first, and an entry is written only where the compiled\n"+
			"  backend agrees — -check asks the refusal rows and writes nothing.\n"+
			"  target: "+recordPath)
		os.Exit(1)
	}
	if err := run(*sources, *check, realLegs()); err != nil {
		fmt.Fprintln(os.Stderr, "recmerge: "+err.Error())
		os.Exit(1)
	}
}

func run(sourcesPath string, checkOnly bool, w legs) error {
	raw, err := os.ReadFile(sourcesPath)
	if err != nil {
		return err
	}
	var srcs []string
	if err := json.Unmarshal(raw, &srcs); err != nil {
		return err
	}
	book, err := os.ReadFile(recordPath)
	if err != nil {
		return err
	}
	var f doc
	if err := json.Unmarshal(book, &f); err != nil {
		return fmt.Errorf("the record does not parse: %w", err)
	}
	if f.Entries == nil {
		return fmt.Errorf("the record has no entries")
	}
	entries := map[string]entry{}
	untouched := bytes.Clone(book)
	added, asked := 0, 0
	var fresh []string
	for _, src := range srcs {
		// 1. the reference answers first — it is the definition, and the compiled leg never writes an entry.
		refOut, refErr, rerr := w.ref(src)
		var e entry
		switch {
		case rerr == nil:
			if checkOnly {
				return fmt.Errorf("the reference ANSWERS %q (%q) — a row the language refuses has no answer to record", src, refOut)
			}
			e = entry{HasStd: true, Stdout: refOut}
		case strings.Contains(refErr, "Error:") || strings.Contains(refErr, "error:"):
			// the reference's last traceback line is `Class: message`
			lines := strings.Split(strings.TrimRight(refErr, "\n"), "\n")
			last := strings.TrimSpace(lines[len(lines)-1])
			i := strings.Index(last, ": ")
			if i < 0 {
				return fmt.Errorf("cannot read the reference's trap for %q: %s", src, refErr)
			}
			e = entry{Err: last, ExnType: last[:i], ExnMsg: last[i+2:]}
		default:
			return fmt.Errorf("the reference refused to answer %q: %s", src, refErr)
		}
		// 2. the compiled leg must agree, or the entry is not writable at all.
		out, errOut, code, cerr := w.compiled(src)
		if rerr == nil {
			if cerr != nil {
				return fmt.Errorf("the compiled backend refused a program the reference prints: %q: %v", src, cerr)
			}
			if out != refOut {
				return fmt.Errorf("the compiled leg prints other bytes than the reference's for %q:\n got %q\nwant %q", src, out, refOut)
			}
		} else {
			if cerr != nil {
				return fmt.Errorf("the compiled backend refused a program the reference traps: %q: %v", src, cerr)
			}
			if code == 0 {
				return fmt.Errorf("the compiled leg answered where the reference traps: %q", src)
			}
			if code == 2 {
				return fmt.Errorf("exit 2 on the raise this program earns: %q", src)
			}
			if !strings.Contains(errOut, e.Err) {
				return fmt.Errorf("the compiled raise is not the reference's for %q:\n got %q\nwant %q", src, errOut, e.Err)
			}
		}
		asked++
		if _, ok := f.Entries[src]; ok {
			continue
		}
		f.Entries[src] = json.RawMessage{}
		entries[src] = e
		fresh = append(fresh, src)
		added++
	}
	if checkOnly {
		fmt.Printf("checked %d rows against the reference, none of them answered\n", asked)
		return nil
	}
	if added == 0 {
		fmt.Printf("checked %d, added 0, the record is unchanged\n", asked)
		return nil
	}
	out, err := appendEntries(book, fresh, entries)
	if err != nil {
		return err
	}
	if bytes.Equal(out, untouched) {
		fmt.Printf("checked %d, added %d, the record is unchanged\n", asked, added)
		return nil
	}
	if err := os.WriteFile(recordPath, out, 0o644); err != nil {
		return err
	}
	fmt.Printf("checked %d, added %d, entries %d\n", asked, added, len(f.Entries))
	return nil
}

// appendEntries inserts the new entries into the record's OWN text rather than rewriting the file from a
// decoded copy: each new entry carries its own trailing comma and goes in at the top of the entries object, so
// no existing byte moves.
func appendEntries(book []byte, fresh []string, entries map[string]entry) ([]byte, error) {
	sort.Strings(fresh)
	var add bytes.Buffer
	for i, src := range fresh {
		one, err := json.Marshal(entries[src])
		if err != nil {
			return nil, err
		}
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, one, "   ", " "); err != nil {
			return nil, err
		}
		var key bytes.Buffer
		if err := json.Indent(&key, []byte(srcQuoted(src)), "  ", " "); err != nil {
			return nil, err
		}
		if i > 0 {
			add.WriteByte('\n')
		}
		add.Write(key.Bytes())
		add.WriteString(": ")
		add.Write(pretty.Bytes())
		add.WriteString(",\n")
	}
	open := bytes.Index(book, []byte("\n \"entries\": {\n"))
	if open < 0 {
		return nil, fmt.Errorf("the record has no `entries` object to add to")
	}
	body := open + len("\n \"entries\": {\n")
	out := append(bytes.Clone(book[:body]), add.Bytes()...)
	out = append(out, book[body:]...)
	if !json.Valid(out) {
		return nil, fmt.Errorf("the record would have been written as invalid JSON")
	}
	return out, nil
}

// srcQuoted is the record's key for a source: the source itself, as a JSON string.
func srcQuoted(src string) string {
	b, err := json.Marshal(src)
	if err != nil {
		panic(err)
	}
	return string(b)
}
