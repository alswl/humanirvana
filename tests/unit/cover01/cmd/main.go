// Command cover01 checks a coverage profile against the COVER-01 gate and
// writes a JSON summary. It exits non-zero when the gate fails or the input is
// invalid.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/alswl/humanirvana/tests/unit/cover01"
)

func main() {
	packagesFile := flag.String("packages", "", "file listing the library packages, one per line")
	profileFile := flag.String("profile", "", "atomic coverage profile from a single go test run")
	summaryFile := flag.String("summary", "", "optional JSON summary output")
	flag.Parse()
	if err := run(*packagesFile, *profileFile, *summaryFile); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(packagesFile, profileFile, summaryFile string) error {
	packages, err := readLines(packagesFile)
	if err != nil {
		return err
	}
	profile, err := os.Open(profileFile)
	if err != nil {
		return fmt.Errorf("cover01: open profile: %w", err)
	}
	defer profile.Close()
	report, err := cover01.Evaluate(profile, packages)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(report.Packages))
	for name := range report.Packages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		c := report.Packages[name]
		fmt.Printf("%s\t%d/%d\n", name, c.Covered, c.Total)
	}
	fmt.Printf("total\t%d/%d\tthreshold %d%%\tpass=%v\n", report.Total.Covered, report.Total.Total, cover01.Threshold, report.Pass)
	if summaryFile != "" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(summaryFile, append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("cover01: write summary: %w", err)
		}
	}
	if !report.Pass {
		return fmt.Errorf("cover01: %d/%d statements covered, below %d%%", report.Total.Covered, report.Total.Total, cover01.Threshold)
	}
	return nil
}

func readLines(name string) ([]string, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, fmt.Errorf("cover01: open package list: %w", err)
	}
	defer f.Close()
	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, scanner.Err()
}
