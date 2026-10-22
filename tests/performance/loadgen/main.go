// Command loadgen runs one closed-loop PERF-01 measurement against a running
// arm: warm up, then sample until both the minimum duration and minimum
// request count are reached (bounded by a maximum duration). Every response is
// checked against the frozen fixture. Latencies go into a 1µs histogram.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alswl/humanirvana/tests/performance/fixture"
)

// Result is one arm's measurement.
type Result struct {
	Case               string         `json:"case"`
	Concurrency        int            `json:"concurrency"`
	WarmupRequests     int64          `json:"warmup_requests"`
	Requests           int64          `json:"requests"`
	ExpectedRejections int64          `json:"expected_rejections"`
	UnexpectedFailures int64          `json:"unexpected_failures"`
	FirstFailure       string         `json:"first_failure,omitempty"`
	ElapsedNS          int64          `json:"elapsed_ns"`
	Throughput         float64        `json:"throughput_rps"`
	StatusCounts       map[int]int64  `json:"status_counts"`
	P95US              int64          `json:"p95_us"`
	Valid              bool           `json:"valid"`
	InvalidReason      string         `json:"invalid_reason,omitempty"`
	Histogram          [][2]int64     `json:"histogram_us"` // [bucket floor µs, count]; bucket width 1µs
	Settings           map[string]any `json:"settings"`
}

func main() {
	base := flag.String("base", "", "arm base URL")
	testdata := flag.String("testdata", "tests/performance/testdata", "frozen fixture directory")
	caseID := flag.String("case", "", "request set, e.g. P01")
	concurrency := flag.Int("concurrency", 1, "closed-loop workers")
	warmup := flag.Duration("warmup", 5*time.Second, "warm-up duration")
	minDuration := flag.Duration("min-duration", 15*time.Second, "minimum sampling duration")
	minRequests := flag.Int64("min-requests", 10000, "minimum sampled requests")
	maxDuration := flag.Duration("max-duration", 120*time.Second, "maximum sampling duration")
	out := flag.String("out", "", "result JSON path")
	flag.Parse()

	r, err := run(*base, *testdata, *caseID, *concurrency, *warmup, *minDuration, *minRequests, *maxDuration)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	data, _ := json.Marshal(r)
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s C=%d n=%d p95=%dus valid=%v %s\n", r.Case, r.Concurrency, r.Requests, r.P95US, r.Valid, r.InvalidReason)
}

func run(base, testdata, caseID string, concurrency int, warmup, minDuration time.Duration, minRequests int64, maxDuration time.Duration) (*Result, error) {
	cases, err := fixture.Load(testdata)
	if err != nil {
		return nil, err
	}
	var c *fixture.Case
	for i := range cases {
		if cases[i].ID == caseID {
			c = &cases[i]
		}
	}
	if c == nil {
		return nil, fmt.Errorf("unknown case %q", caseID)
	}
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        concurrency,
			MaxIdleConnsPerHost: concurrency,
			MaxConnsPerHost:     concurrency,
			DisableCompression:  true,
			ForceAttemptHTTP2:   false,
		},
	}
	r := &Result{Case: caseID, Concurrency: concurrency, StatusCounts: map[int]int64{}, Settings: map[string]any{
		"warmup": warmup.String(), "min_duration": minDuration.String(), "min_requests": minRequests,
		"max_duration": maxDuration.String(), "timeout": "5s", "http": "HTTP/1.1 keep-alive, no compression",
	}}

	// Warm-up: any failure ends this arm.
	var warmFail atomic.Pointer[string]
	var warmCount atomic.Int64
	loop(concurrency, func(stop func() bool) {
		for !stop() {
			if _, _, err := once(client, base, c); err != nil {
				msg := err.Error()
				warmFail.CompareAndSwap(nil, &msg)
				return
			}
			warmCount.Add(1)
		}
	}, deadlineStop(time.Now().Add(warmup), &warmFail))
	r.WarmupRequests = warmCount.Load()
	if f := warmFail.Load(); f != nil {
		r.InvalidReason = "warm-up failure: " + *f
		return r, nil
	}

	// Sampling.
	var count, failures atomic.Int64
	var firstFail atomic.Pointer[string]
	hists := make([]map[int64]int64, concurrency)
	statuses := make([]map[int]int64, concurrency)
	start := time.Now()
	var stopFlag atomic.Bool
	stop := func() bool {
		if stopFlag.Load() {
			return true
		}
		elapsed := time.Since(start)
		if elapsed >= maxDuration || (elapsed >= minDuration && count.Load() >= minRequests) {
			stopFlag.Store(true)
			return true
		}
		return false
	}
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		hists[w], statuses[w] = map[int64]int64{}, map[int]int64{}
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for !stop() {
				latency, status, err := once(client, base, c)
				statuses[w][status]++
				if err != nil {
					failures.Add(1)
					msg := err.Error()
					firstFail.CompareAndSwap(nil, &msg)
					continue
				}
				hists[w][latency.Nanoseconds()/1000]++
				count.Add(1)
			}
		}(w)
	}
	wg.Wait()
	elapsed := time.Since(start)

	merged := map[int64]int64{}
	for w := range hists {
		for b, n := range hists[w] {
			merged[b] += n
		}
		for s, n := range statuses[w] {
			r.StatusCounts[s] += n
		}
	}
	r.Requests, r.UnexpectedFailures, r.ElapsedNS = count.Load(), failures.Load(), elapsed.Nanoseconds()
	r.Throughput = float64(r.Requests) / elapsed.Seconds()
	if c.WantStatus >= 400 {
		r.ExpectedRejections = r.Requests
	}
	if f := firstFail.Load(); f != nil {
		r.FirstFailure = *f
	}
	buckets := make([]int64, 0, len(merged))
	for b := range merged {
		buckets = append(buckets, b)
	}
	sort.Slice(buckets, func(i, j int) bool { return buckets[i] < buckets[j] })
	for _, b := range buckets {
		r.Histogram = append(r.Histogram, [2]int64{b, merged[b]})
	}
	r.P95US = p95(r.Histogram, r.Requests)

	switch {
	case r.UnexpectedFailures > 0:
		r.InvalidReason = fmt.Sprintf("%d unexpected failures", r.UnexpectedFailures)
	case r.Requests < minRequests || elapsed < minDuration:
		r.InvalidReason = fmt.Sprintf("insufficient samples: %d requests in %s", r.Requests, elapsed)
	default:
		r.Valid = true
	}
	return r, nil
}

// once sends one frozen request and reads the whole body. Latency runs from
// just before the request is sent to after the body is fully read; the
// response check happens after timing.
func once(client *http.Client, base string, c *fixture.Case) (time.Duration, int, error) {
	req, err := c.NewRequest(base)
	if err != nil {
		return 0, 0, err
	}
	t0 := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	latency := time.Since(t0)
	if err != nil {
		return 0, resp.StatusCode, err
	}
	if err := c.Check(resp.StatusCode, resp.Header.Get("Content-Type"), body); err != nil {
		return 0, resp.StatusCode, err
	}
	return latency, resp.StatusCode, nil
}

// p95 is the nearest-rank 95th percentile: the ceil(0.95*N)-th sample, taken
// at its 1µs bucket's upper bound.
func p95(hist [][2]int64, n int64) int64 {
	if n == 0 {
		return 0
	}
	rank := int64(math.Ceil(0.95 * float64(n)))
	var seen int64
	for _, b := range hist {
		seen += b[1]
		if seen >= rank {
			return b[0] + 1
		}
	}
	return hist[len(hist)-1][0] + 1
}

func loop(workers int, body func(stop func() bool), stop func() bool) {
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); body(stop) }()
	}
	wg.Wait()
}

func deadlineStop(deadline time.Time, fail *atomic.Pointer[string]) func() bool {
	return func() bool { return fail.Load() != nil || time.Now().After(deadline) }
}
