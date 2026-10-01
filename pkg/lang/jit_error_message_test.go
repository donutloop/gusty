package lang

import (
	"strings"
	"testing"
)

// The `jit:` path used to report only how many things were wrong — "jit: 1 error(s) in source" —
// while the checker already knew the sentence that would have told the reader what to fix. A
// `--jit` run that disagreed with `--interp` was therefore unreadable, and unreadable is what ADR
// 0166 and ADR 0233 exist to forbid: a refusal has to name what is missing. These tests pin that the
// same words reach the error text and the JSON diagnostics, so an agent reading the error and an
// agent reading --json get one story, not two (the CLI's stderr rendering is Diagnostic.Error).

func TestJITErrorCarriesTheCheckerMessages(t *testing.T) {
	res, err := JIT("print(undefined_thing)\n", 0)
	if err == nil {
		t.Fatal("an undefined name must not compile")
	}
	msg := err.Error()
	if !strings.Contains(msg, "undefined name") {
		t.Fatalf("the refusal hid what it rejected: %q", msg)
	}
	if !strings.Contains(msg, "1 error(s)") {
		t.Fatalf("the count is part of the contract too: %q", msg)
	}
	// The machine surface says the same thing, so no one has to scrape the prose to find the reason.
	var inDiags bool
	for _, d := range res.Diagnostics {
		if d.Level == LevelError && strings.Contains(d.Msg, "undefined name") {
			inDiags = true
		}
	}
	if !inDiags {
		t.Fatalf("the diagnostic list lost the message the error quoted: %+v", res.Diagnostics)
	}
}

// Every error rides along, not just the first: a program with two bad statements gets both
// sentences, in source order, each with its span.
func TestJITErrorListsEveryMessage(t *testing.T) {
	_, err := JIT("print(undefined_thing)\nprint(nope_too)\n", 0)
	if err == nil {
		t.Fatal("two undefined names must not compile")
	}
	msg := err.Error()
	for _, want := range []string{"undefined name \"undefined_thing\"", "undefined name \"nope_too\"", "2 error(s)"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("missing %q from:\n%s", want, msg)
		}
	}
	if first, second := strings.Index(msg, "undefined_thing"), strings.Index(msg, "nope_too"); first > second {
		t.Fatalf("the messages came out of source order:\n%s", msg)
	}
}

// The span is in the text, which is what makes a refusal actionable from a file position alone —
// and it is how the `;` disagreement (Gap R.72) finally became readable: the interpreter ran
// `x = 5; print(x+1)` while the compiled path refused it, and the only thing missing was the
// sentence naming the character.
func TestJITErrorNamesTheSpanOfWhatItRejected(t *testing.T) {
	_, err := JIT("x = 5\nprint(undefined_thing)\n", 0)
	if err == nil {
		t.Fatal("an undefined name must not compile")
	}
	if msg := err.Error(); !strings.Contains(msg, "2:7") {
		t.Fatalf("the refusal did not say where: %q", msg)
	}
}
