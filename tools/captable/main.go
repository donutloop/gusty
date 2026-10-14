// tools/captable — print the capability-refusal inventory that docs/operations.md carries, straight out
// of the registry that decides which refusal is which code.
//
// A refusal is an interface: it is what the compiler says where it would otherwise have written an
// answer. Three readers read it — a human at a terminal, an agent branching on `--json`'s `capability.code`,
// and an agent reading `--schema`'s enum — and the fourth place the inventory lives is a markdown table.
// A table that is typed by hand is a fourth list of a fact the registry already owns, and the drift shows up
// as the reader who trusts the stale copy. So the table is printed from `capabilityRefusalRegistry`
// (pkg/lang/capability.go), and the guard in pkg/lang/capability_docs_test.go compares the document against
// this exact text: edit the registry, re-run this, paste; edit the document alone, and the suite goes red.
//
//	usage: go run ./tools/captable            # the markdown table, on stdout
//	       go run ./tools/captable -check     # exit 1 if docs/operations.md does not contain it
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/donutloop/gusty/pkg/lang"
)

func main() {
	check := len(os.Args) > 1 && os.Args[1] == "-check"
	table := lang.CapabilityDocsTable()
	if !check {
		fmt.Println(table)
		return
	}
	body, err := os.ReadFile("docs/operations.md")
	if err != nil {
		fmt.Fprintf(os.Stderr, "captable: %v\n", err)
		os.Exit(2)
	}
	if !strings.Contains(string(body), table) {
		fmt.Fprintln(os.Stderr, "captable: docs/operations.md's capability table is not the registry — paste this output into the section it marks:")
		fmt.Println(table)
		os.Exit(1)
	}
	fmt.Printf("captable: docs/operations.md carries all %d codes\n", len(lang.CapabilityRefusalCodes()))
}
