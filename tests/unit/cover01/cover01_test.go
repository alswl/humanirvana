package cover01

import (
	"fmt"
	"strings"
	"testing"
)

const pkg = "example.com/lib"

// profile builds an atomic profile with one block per entry of hits; each
// block holds stmts statements.
func profile(stmts int, hits ...int) string {
	var b strings.Builder
	b.WriteString("mode: atomic\n")
	for i, h := range hits {
		fmt.Fprintf(&b, "%s/a.go:%d.1,%d.2 %d %d\n", pkg, i+1, i+1, stmts, h)
	}
	return b.String()
}

func TestThresholdUsesRawIntegers(t *testing.T) {
	cases := []struct {
		covered, total int
		pass           bool
	}{
		{4, 5, true},       // exactly 80%
		{79, 100, false},   // below
		{799, 1000, false}, // 79.9% would display as 80%
		{800, 1000, true},
		{0, 0, false}, // no statements never passes
	}
	for _, c := range cases {
		if got := (Count{c.covered, c.total}).Pass(); got != c.pass {
			t.Errorf("Count{%d,%d}.Pass() = %v, want %v", c.covered, c.total, got, c.pass)
		}
	}
}

func TestEvaluateCountsStatementsNotBlocks(t *testing.T) {
	// One covered 4-statement block and one missed 1-statement block: 80%.
	in := "mode: atomic\n" + pkg + "/a.go:1.1,2.2 4 3\n" + pkg + "/a.go:3.1,4.2 1 0\n"
	r, err := Evaluate(strings.NewReader(in), []string{pkg})
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != (Count{4, 5}) || !r.Pass {
		t.Fatalf("report = %+v, want 4/5 pass", r)
	}
}

func TestEvaluateMergesRepeatedBlocksOnce(t *testing.T) {
	// The same block reported by two test binaries in one run must not be
	// counted twice.
	in := profile(1, 0, 0) + pkg + "/a.go:1.1,1.2 1 5\n"
	r, err := Evaluate(strings.NewReader(in), []string{pkg})
	if err != nil {
		t.Fatal(err)
	}
	if r.Total != (Count{1, 2}) {
		t.Fatalf("total = %+v, want 1/2", r.Total)
	}
}

func TestEvaluateRejectsInvalidInput(t *testing.T) {
	other := "example.com/other"
	cases := map[string]struct {
		profile  string
		packages []string
	}{
		"empty profile":        {"", []string{pkg}},
		"set mode":             {strings.Replace(profile(1, 1), "atomic", "set", 1), []string{pkg}},
		"malformed line":       {"mode: atomic\nbroken\n", []string{pkg}},
		"bad count":            {"mode: atomic\n" + pkg + "/a.go:1.1,1.2 1 x\n", []string{pkg}},
		"conflicting block":    {profile(1, 1) + pkg + "/a.go:1.1,1.2 2 1\n", []string{pkg}},
		"missing package":      {profile(1, 1), []string{pkg, other}},
		"unexpected package":   {profile(1, 1) + other + "/b.go:1.1,1.2 1 1\n", []string{pkg}},
		"empty package list":   {profile(1, 1), nil},
		"no statements at all": {"mode: atomic\n", []string{pkg}},
	}
	for name, c := range cases {
		if _, err := Evaluate(strings.NewReader(c.profile), c.packages); err == nil {
			t.Errorf("%s: Evaluate succeeded, want error", name)
		}
	}
}

func TestEvaluateFailsBelowThreshold(t *testing.T) {
	r, err := Evaluate(strings.NewReader(profile(1, 1, 1, 1, 0, 0)), []string{pkg})
	if err != nil {
		t.Fatal(err)
	}
	if r.Pass || r.Total != (Count{3, 5}) {
		t.Fatalf("report = %+v, want 3/5 fail", r)
	}
}
