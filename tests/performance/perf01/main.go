// Command perf01 runs the full PERF-01 protocol: for every request set P01–P05
// and concurrency 1/8/32 it collects 5 valid native/adapter pairs (at most 2
// extra attempts), each arm on a fresh server process, and judges every pair
// by P95(adapter) − P95(native) < 1000µs. All attempts are kept.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alswl/humanirvana/tests/performance/fixture"
)

const (
	validPairs   = 5
	extraAllowed = 2
	thresholdUS  = 1000
)

var concurrencies = []int{1, 8, 32}

// ArmRun is one arm of one attempt.
type ArmRun struct {
	Arm        string  `json:"arm"`
	ResultFile string  `json:"result_file"`
	P95US      int64   `json:"p95_us"`
	Requests   int64   `json:"requests"`
	Throughput float64 `json:"throughput_rps"`
	Valid      bool    `json:"valid"`
	Reason     string  `json:"reason,omitempty"`
	ServerCPU  string  `json:"server_cpu"`
	ServerRSS  int64   `json:"server_max_rss_bytes"`
}

// Attempt is one native/adapter pair.
type Attempt struct {
	Number  int      `json:"number"`
	Order   []string `json:"order"`
	Arms    []ArmRun `json:"arms"`
	Valid   bool     `json:"valid"`
	DeltaUS int64    `json:"delta_us,omitempty"`
}

// Cell is one request set × concurrency.
type Cell struct {
	Case        string    `json:"case"`
	Concurrency int       `json:"concurrency"`
	Attempts    []Attempt `json:"attempts"`
	ValidPairs  int       `json:"valid_pairs"`
	MaxDeltaUS  int64     `json:"max_delta_us"`
	Verdict     string    `json:"verdict"` // pass, fail, undetermined
}

func main() {
	out := flag.String("out", "", "evidence directory")
	serverBin := flag.String("server", "", "prebuilt server binary")
	loadgenBin := flag.String("loadgen", "", "prebuilt loadgen binary")
	testdata := flag.String("testdata", "tests/performance/testdata", "frozen fixture directory")
	flag.Parse()
	if err := run(*out, *serverBin, *loadgenBin, *testdata); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(out, serverBin, loadgenBin, testdata string) error {
	if _, err := fixture.Load(testdata); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(out, "runs"), 0o755); err != nil {
		return err
	}
	gomaxprocs := strconv.Itoa(runtime.NumCPU())
	var cells []Cell
	for _, id := range fixture.CaseIDs {
		for _, c := range concurrencies {
			cell := Cell{Case: id, Concurrency: c}
			for n := 1; n <= validPairs+extraAllowed && cell.ValidPairs < validPairs; n++ {
				order := []string{fixture.Native, fixture.Adapter}
				if n%2 == 0 {
					order = []string{fixture.Adapter, fixture.Native}
				}
				a := Attempt{Number: n, Order: order}
				p95 := map[string]int64{}
				a.Valid = true
				for _, arm := range order {
					ar := runArm(out, serverBin, loadgenBin, testdata, gomaxprocs, id, c, n, arm)
					a.Arms = append(a.Arms, ar)
					p95[arm] = ar.P95US
					a.Valid = a.Valid && ar.Valid
				}
				if a.Valid {
					a.DeltaUS = p95[fixture.Adapter] - p95[fixture.Native]
					cell.ValidPairs++
					if cell.ValidPairs == 1 || a.DeltaUS > cell.MaxDeltaUS {
						cell.MaxDeltaUS = a.DeltaUS
					}
				}
				cell.Attempts = append(cell.Attempts, a)
				fmt.Printf("%s C=%d attempt %d %v valid=%v delta=%dus\n", id, c, n, order, a.Valid, a.DeltaUS)
				if err := writeJSON(filepath.Join(out, "cells.json"), append(cells, cell)); err != nil {
					return err
				}
			}
			switch {
			case cell.ValidPairs > 0 && cell.MaxDeltaUS >= thresholdUS:
				cell.Verdict = "fail"
			case cell.ValidPairs < validPairs:
				cell.Verdict = "undetermined"
			default:
				cell.Verdict = "pass"
			}
			cells = append(cells, cell)
		}
	}
	if err := writeJSON(filepath.Join(out, "cells.json"), cells); err != nil {
		return err
	}
	return writeReport(filepath.Join(out, "report.md"), cells)
}

func runArm(out, serverBin, loadgenBin, testdata, gomaxprocs, id string, c, n int, arm string) ArmRun {
	name := fmt.Sprintf("%s-c%02d-a%d-%s", id, c, n, arm)
	ar := ArmRun{Arm: arm, ResultFile: filepath.Join("runs", name+".json")}
	env := append(os.Environ(), "GOMAXPROCS="+gomaxprocs, "GOGC=100", "GODEBUG=")

	srv := exec.Command(serverBin, "-arm", arm)
	srv.Env = env
	stdout, err := srv.StdoutPipe()
	if err != nil {
		ar.Reason = err.Error()
		return ar
	}
	srv.Stderr = os.Stderr
	if err := srv.Start(); err != nil {
		ar.Reason = "server start: " + err.Error()
		return ar
	}
	ready := make(chan string, 1)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			if base, ok := strings.CutPrefix(s.Text(), "READY "); ok {
				ready <- base
			}
		}
	}()
	defer func() {
		_ = srv.Process.Signal(syscall.SIGTERM)
		state, err := srv.Process.Wait()
		if err == nil {
			ar.ServerCPU = fmt.Sprintf("user=%s sys=%s exit=%d", state.UserTime(), state.SystemTime(), state.ExitCode())
			if ru, ok := state.SysUsage().(*syscall.Rusage); ok {
				ar.ServerRSS = ru.Maxrss // bytes on darwin
			}
		}
	}()
	var base string
	select {
	case base = <-ready:
	case <-time.After(10 * time.Second):
		ar.Reason = "server not ready"
		return ar
	}

	lg := exec.Command(loadgenBin, "-base", base, "-testdata", testdata, "-case", id,
		"-concurrency", strconv.Itoa(c), "-out", filepath.Join(out, ar.ResultFile))
	lg.Env = env
	lg.Stdout, lg.Stderr = os.Stdout, os.Stderr
	if err := lg.Run(); err != nil {
		ar.Reason = "loadgen: " + err.Error()
		return ar
	}
	var r struct {
		P95US         int64   `json:"p95_us"`
		Requests      int64   `json:"requests"`
		Throughput    float64 `json:"throughput_rps"`
		Valid         bool    `json:"valid"`
		InvalidReason string  `json:"invalid_reason"`
	}
	data, err := os.ReadFile(filepath.Join(out, ar.ResultFile))
	if err == nil {
		err = json.Unmarshal(data, &r)
	}
	if err != nil {
		ar.Reason = "result: " + err.Error()
		return ar
	}
	ar.P95US, ar.Requests, ar.Throughput, ar.Valid, ar.Reason = r.P95US, r.Requests, r.Throughput, r.Valid, r.InvalidReason
	return ar
}

func writeJSON(name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(name, append(data, '\n'), 0o644)
}

func writeReport(name string, cells []Cell) error {
	var b strings.Builder
	counts := map[string]int{}
	fmt.Fprintf(&b, "# PERF-01 result (protocol revision %d)\n\n", fixture.ProtocolRevision)
	b.WriteString("| Case | C | Valid pairs | Native P95 / Adapter P95 / delta (µs) per valid pair | Max delta µs | Verdict |\n|---|---|---|---|---|---|\n")
	for _, c := range cells {
		counts[c.Verdict]++
		var pairs []string
		for _, a := range c.Attempts {
			if !a.Valid {
				pairs = append(pairs, fmt.Sprintf("#%d invalid", a.Number))
				continue
			}
			var nat, ad int64
			for _, r := range a.Arms {
				if r.Arm == fixture.Native {
					nat = r.P95US
				} else {
					ad = r.P95US
				}
			}
			pairs = append(pairs, fmt.Sprintf("#%d %d/%d/%+d", a.Number, nat, ad, a.DeltaUS))
		}
		fmt.Fprintf(&b, "| %s | %d | %d | %s | %d | %s |\n", c.Case, c.Concurrency, c.ValidPairs, strings.Join(pairs, "; "), c.MaxDeltaUS, c.Verdict)
	}
	overall := "PASS"
	if counts["pass"] != len(cells) {
		overall = "NOT PASSED"
	}
	fmt.Fprintf(&b, "\nCells: %d pass, %d fail, %d undetermined. PERF-01: %s.\n", counts["pass"], counts["fail"], counts["undetermined"], overall)
	return os.WriteFile(name, []byte(b.String()), 0o644)
}
