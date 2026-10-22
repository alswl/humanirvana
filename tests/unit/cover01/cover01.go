// Package cover01 implements the COVER-01 gate: statement-weighted coverage of
// the adapter library packages must satisfy 100*covered >= 80*total, computed
// from one atomic profile with raw integer counts.
package cover01

import (
	"bufio"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
)

// Threshold is the minimum statement-weighted coverage percentage.
const Threshold = 80

// Count is the raw statement tally for one package or the whole set.
type Count struct {
	Covered int `json:"covered"`
	Total   int `json:"total"`
}

// Pass reports whether the tally meets Threshold without rounding.
func (c Count) Pass() bool { return c.Total > 0 && 100*c.Covered >= Threshold*c.Total }

// Report is the gate result for a profile and an expected package set.
type Report struct {
	Packages map[string]Count `json:"packages"`
	Total    Count            `json:"total"`
	Pass     bool             `json:"pass"`
}

// Evaluate parses an atomic profile and tallies the expected packages. It
// fails when the profile is not atomic, any expected package has no
// statements, or the profile covers a package outside the expected set.
// Blocks repeated within the one profile are counted once.
func Evaluate(profile io.Reader, packages []string) (Report, error) {
	if len(packages) == 0 {
		return Report{}, fmt.Errorf("cover01: empty package list")
	}
	expected := make(map[string]bool, len(packages))
	for _, p := range packages {
		expected[p] = true
	}

	type block struct {
		stmts   int
		covered bool
	}
	blocks := map[string]*block{}
	scanner := bufio.NewScanner(profile)
	line := 0
	for scanner.Scan() {
		text := strings.TrimSpace(scanner.Text())
		line++
		if line == 1 {
			if text != "mode: atomic" {
				return Report{}, fmt.Errorf("cover01: profile mode %q, want \"mode: atomic\"", text)
			}
			continue
		}
		if text == "" {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != 3 {
			return Report{}, fmt.Errorf("cover01: malformed profile line %d: %q", line, text)
		}
		stmts, err := strconv.Atoi(fields[1])
		if err != nil || stmts < 0 {
			return Report{}, fmt.Errorf("cover01: bad statement count on line %d: %q", line, text)
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil || count < 0 {
			return Report{}, fmt.Errorf("cover01: bad hit count on line %d: %q", line, text)
		}
		key := fields[0]
		if b, ok := blocks[key]; ok {
			if b.stmts != stmts {
				return Report{}, fmt.Errorf("cover01: block %s has conflicting statement counts %d and %d", key, b.stmts, stmts)
			}
			b.covered = b.covered || count > 0
			continue
		}
		blocks[key] = &block{stmts: stmts, covered: count > 0}
	}
	if err := scanner.Err(); err != nil {
		return Report{}, fmt.Errorf("cover01: read profile: %w", err)
	}
	if line == 0 {
		return Report{}, fmt.Errorf("cover01: empty profile")
	}

	report := Report{Packages: map[string]Count{}}
	for key, b := range blocks {
		file, _, ok := strings.Cut(key, ":")
		if !ok {
			return Report{}, fmt.Errorf("cover01: block without position: %q", key)
		}
		pkg := path.Dir(file)
		if !expected[pkg] {
			return Report{}, fmt.Errorf("cover01: profile covers unexpected package %s", pkg)
		}
		c := report.Packages[pkg]
		c.Total += b.stmts
		if b.covered {
			c.Covered += b.stmts
		}
		report.Packages[pkg] = c
	}
	var missing []string
	for _, p := range packages {
		c := report.Packages[p]
		if c.Total == 0 {
			missing = append(missing, p)
		}
		report.Total.Covered += c.Covered
		report.Total.Total += c.Total
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return Report{}, fmt.Errorf("cover01: no statements for packages %s", strings.Join(missing, ", "))
	}
	report.Pass = report.Total.Pass()
	return report, nil
}
