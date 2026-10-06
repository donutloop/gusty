// Command echoprobe is a temporary cycle tool: it prints what the compiled REPL echo reports
// for a snippet.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/donutloop/gusty/pkg/lang"
)

func main() {
	src := os.Args[1]
	res, err := lang.RunSnippet(src)
	if err != nil {
		fmt.Println("ERR:", err)
		return
	}
	b, _ := json.Marshal(res)
	fmt.Println(string(b))
}
