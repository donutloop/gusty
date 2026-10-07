package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/donutloop/gusty/pkg/lang"
)

// oracleprobe prints, for every named program (or merged group), the two legs and
// the classification the harness would compute — as JSON, so a ledger row in
// integration/conformance_cases.go can be written from measured data instead of
// memory (roadmap L11.9, ADR 0186; two legs since ADR 0302 retired the AST interpreter).
//
//	go run ./tools/oracleprobe probe_tuple features_a merged:ctrl_a,ctrl_b,ctrl_c
//
// It reuses the harness's own classifier (lang.BuildOracleReport) and the harness's
// own legs (Compile+llc+cc, lang.PythonRun), so a verdict printed here is the
// verdict the matrix will record — there is no second implementation of "does this
// behave like Python?" for the probe to drift from.
func main() {
	dir := "integration/programs"
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: oracleprobe <program|merged:a,b,c> [more…]")
		os.Exit(2)
	}
	type row struct {
		ID     string `json:"id"`
		Status string `json:"status"`
		Notes  string `json:"notes,omitempty"`
		AOT    string `json:"aot"`
		Python string `json:"python"`
		AErr   string `json:"aot_err,omitempty"`
		PErr   string `json:"python_err,omitempty"`
	}
	out := []row{}
	for _, spec := range args {
		var src string
		id := spec
		if strings.HasPrefix(spec, "merged:") {
			parts := strings.Split(strings.TrimPrefix(spec, "merged:"), ",")
			var sb strings.Builder
			for i, p := range parts {
				if i > 0 {
					sb.WriteString("\n")
				}
				b, err := os.ReadFile(filepath.Join(dir, p+".gy"))
				if err != nil {
					panic(err)
				}
				sb.Write(b)
			}
			src = sb.String()
			id = "merged/" + strings.TrimSuffix(strings.Join(parts, "+"), "")
		} else {
			b, err := os.ReadFile(filepath.Join(dir, spec+".gy"))
			if err != nil {
				panic(err)
			}
			src = string(b)
			id = "programs/" + spec
		}
		ao, aerr := safeAOT(src)
		po, perr := safePy(src)
		rep := lang.BuildOracleReport(aerr == nil, ao, errStr(aerr), nil, perr == nil, po, errStr(perr))
		out = append(out, row{
			ID: id, Status: rep.Status, Notes: strings.Join(rep.Notes, "; "),
			AOT: ao, Python: po,
			AErr: errStr(aerr), PErr: errStr(perr),
		})
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(b))
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return s
}

func safeAOT(src string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("compiler panic: %v", r)
		}
	}()
	return aot(src)
}

func safePy(src string) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = "", fmt.Errorf("oracle panic: %v", r)
		}
	}()
	o, stderr, err := lang.PythonRun(src)
	if err != nil {
		return o, fmt.Errorf("%w: %s", err, firstLine(stderr))
	}
	return o, nil
}

func aot(src string) (string, error) {
	res, err := lang.Compile(src)
	if err != nil {
		return "", err
	}
	dir, _ := os.MkdirTemp("", "probe")
	defer os.RemoveAll(dir)
	ir := filepath.Join(dir, "p.ll")
	obj := filepath.Join(dir, "p.o")
	bin := filepath.Join(dir, "p")
	if err := os.WriteFile(ir, []byte(res.IR), 0o600); err != nil {
		return "", err
	}
	if o, err := exec.Command("llc-20", "-filetype=obj", "-relocation-model=pic", ir, "-o", obj).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w: %s", err, firstLine(string(o)))
	}
	if o, err := exec.Command("cc", obj, "-lm", "-o", bin).CombinedOutput(); err != nil {
		return "", fmt.Errorf("%w: %s", err, firstLine(string(o)))
	}
	got, err := exec.Command(bin).Output()
	return string(got), err
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
