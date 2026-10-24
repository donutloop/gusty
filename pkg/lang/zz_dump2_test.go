package lang

import (
	"os"
	"testing"
)

func TestDumpWholeIR(t *testing.T) {
	res, err := Compile("xs = [1, \"a\", None]\nxs[0] = \"z\"\nxs.append(None)\nprint(xs)\n")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(os.Getenv("OUT"), []byte(res.IR), 0o644)
}
