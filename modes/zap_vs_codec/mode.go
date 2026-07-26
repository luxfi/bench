// Package zapvscodec is the canary mode for luxbench: the 8 head-to-head
// claims comparing luxfi/codec (linearcodec/reflect) vs luxfi/zap (zero-copy
// wire). Source benches live in this directory as `*_test.go`; this file
// is the Mode shim that runs them and emits result.ResultRecord rows.
//
// Mode invocation:
//
//	luxbench --mode=zap-vs-codec --benchtime=2s --count=3 --output=/tmp/x.json
//
// What it does:
//  1. Locate this package on disk via runtime.Caller — the bench source must
//     be co-located with the Mode (no embedded copies; one source of truth).
//  2. Shell out to `go test -bench=. -benchmem -benchtime=<bt> -count=<c>`.
//  3. Parse the Go test bench output (the standard
//     `BenchmarkXxx-CPU<spc>iters<spc>ns/op[<spc>extra]` format).
//  4. Collapse `count` runs per subtest into one ResultRecord (median + p50/95/99).
//  5. Return the slice; luxbench writes it to --output as JSON.
package zapvscodec

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	bench "github.com/luxfi/bench"
	"github.com/luxfi/bench/result"
)

// Mode satisfies bench.Mode.
type Mode struct {
	// Pattern is the -bench regex. Default "." (all).
	pattern string
	// Parallel controls -parallel; 0 means default.
	parallel int
}

// New returns a fresh Mode (constructor so cmd/luxbench can register).
func New() *Mode { return &Mode{pattern: "."} }

// Name returns the --mode value.
func (m *Mode) Name() string { return "zap-vs-codec" }

// Description returns one line for -help.
func (m *Mode) Description() string {
	return "Head-to-head: luxfi/codec linearcodec vs luxfi/zap zero-copy (8 claims)."
}

// RegisterFlags binds mode-specific flags.
func (m *Mode) RegisterFlags(fs *flag.FlagSet) {
	fs.StringVar(&m.pattern, "bench", ".", "go test -bench regex (e.g. RejectBadMagic)")
	fs.IntVar(&m.parallel, "parallel", 0, "go test -parallel value; 0 = GOMAXPROCS")
}

// Run shells out to `go test -bench` against this package and parses output.
func (m *Mode) Run(ctx context.Context, cfg bench.Config) ([]result.ResultRecord, error) {
	pkgDir, err := selfDir()
	if err != nil {
		return nil, fmt.Errorf("locate package dir: %w", err)
	}

	args := []string{
		"test",
		"-bench=" + m.pattern,
		"-benchmem",
		"-run=^$", // benches only
	}
	if cfg.BenchTime != "" {
		args = append(args, "-benchtime="+cfg.BenchTime)
	}
	if cfg.Count > 0 {
		args = append(args, "-count="+strconv.Itoa(cfg.Count))
	}
	if m.parallel > 0 {
		args = append(args, "-parallel="+strconv.Itoa(m.parallel))
	}
	args = append(args, ".")

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = pkgDir
	cmd.Env = append(cmd.Environ(), "GOFLAGS=") // strip any user GOFLAGS noise

	var stdout, stderr bytes.Buffer
	cmd.Stdout = io.MultiWriter(&stdout, optionalStream(cfg.Verbose))
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go test failed: %w\nstderr: %s", err, stderr.String())
	}

	records, err := parseBenchOutput(m.Name(), stdout.String())
	if err != nil {
		return nil, fmt.Errorf("parse bench output: %w", err)
	}
	return records, nil
}

// selfDir returns the on-disk directory of this package (the dir containing
// mode.go), so `go test` runs against the test files alongside it. Uses
// runtime.Caller(0) — robust across `go run` and installed binaries (since
// the source path is baked into the binary's debug info... wait, no, it
// isn't for installed binaries; see fallback).
func selfDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller(0) failed")
	}
	dir := filepath.Dir(file)
	// Sanity: a *_test.go file should exist alongside.
	matches, err := filepath.Glob(filepath.Join(dir, "*_test.go"))
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no test files in %s (caller=%s)", dir, file)
	}
	return dir, nil
}

// optionalStream returns os.Stdout when verbose, else io.Discard. Helper
// so the Run hot path stays readable.
func optionalStream(verbose bool) io.Writer {
	if verbose {
		return os.Stdout
	}
	return io.Discard
}

// benchLine matches `BenchmarkName-cpu<tab>iters<tab>ns/op<tab>extras...`.
//
// Go's bench output uses TAB+spaces; we accept any whitespace separators.
// The name group captures up to and excluding the GOMAXPROCS suffix (the
// `-N` after the trailing component); regex anchors on the leading
// "Benchmark" word.
var benchLine = regexp.MustCompile(
	`^(Benchmark[^\s-]+)-(\d+)\s+(\d+)\s+([\d.eE+-]+)\s+ns/op(?:\s+(.*))?$`,
)

// metricPair matches "<number> <unit>". Unit may contain /, _, %, -.
var metricPair = regexp.MustCompile(`([+\-]?\d+(?:\.\d+)?(?:[eE][+\-]?\d+)?)\s+([A-Za-z][A-Za-z0-9_/%\-]*)`)

// rawSample is one raw sample line from `go test -bench` output.
type rawSample struct {
	subtest string
	iters   int
	nsPerOp float64
	bPerOp  int
	allocsPerOp int
	extras  map[string]float64
}

// parseBenchOutput walks stdout, extracts rawSamples, collapses by subtest.
func parseBenchOutput(mode, out string) ([]result.ResultRecord, error) {
	samples := []rawSample{}
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for scanner.Scan() {
		s, ok := parseLine(scanner.Text())
		if ok {
			samples = append(samples, s)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return collapse(mode, samples), nil
}

// parseLine extracts one rawSample from a single line; returns ok=false
// if the line isn't a bench result.
func parseLine(line string) (rawSample, bool) {
	line = strings.TrimSpace(line)
	m := benchLine.FindStringSubmatch(line)
	if m == nil {
		return rawSample{}, false
	}
	iters, err := strconv.Atoi(m[3])
	if err != nil {
		return rawSample{}, false
	}
	ns, err := strconv.ParseFloat(m[4], 64)
	if err != nil {
		return rawSample{}, false
	}
	s := rawSample{
		subtest: m[1],
		iters:   iters,
		nsPerOp: ns,
		extras:  map[string]float64{},
	}
	if len(m) > 5 && m[5] != "" {
		for _, pair := range metricPair.FindAllStringSubmatch(m[5], -1) {
			val, err := strconv.ParseFloat(pair[1], 64)
			if err != nil {
				continue
			}
			unit := pair[2]
			switch unit {
			case "B/op":
				s.bPerOp = int(val)
			case "allocs/op":
				s.allocsPerOp = int(val)
			default:
				s.extras[unit] = val
			}
		}
	}
	return s, true
}

// collapse aggregates samples by subtest into ResultRecords (median + p50/95/99).
func collapse(mode string, samples []rawSample) []result.ResultRecord {
	bySubtest := map[string][]rawSample{}
	for _, s := range samples {
		bySubtest[s.subtest] = append(bySubtest[s.subtest], s)
	}
	subtestNames := make([]string, 0, len(bySubtest))
	for name := range bySubtest {
		subtestNames = append(subtestNames, name)
	}
	sort.Strings(subtestNames)

	out := make([]result.ResultRecord, 0, len(subtestNames))
	for _, name := range subtestNames {
		runs := bySubtest[name]
		durations := make([]time.Duration, 0, len(runs))
		for _, r := range runs {
			durations = append(durations, time.Duration(r.nsPerOp))
		}
		sorted := result.SortDurations(durations)
		rec := result.NewRecord(mode, name)
		rec.Iterations = runs[len(runs)-1].iters // last b.N is representative
		rec.Median = sorted[len(sorted)/2]
		rec.P50 = result.Quantile(sorted, 50)
		rec.P95 = result.Quantile(sorted, 95)
		rec.P99 = result.Quantile(sorted, 99)
		if rec.Median > 0 {
			rec.OpsPerSec = float64(time.Second) / float64(rec.Median)
		}
		// Aggregate B/op and allocs/op: take median.
		bs := make([]float64, 0, len(runs))
		as := make([]float64, 0, len(runs))
		extras := map[string][]float64{}
		for _, r := range runs {
			if r.bPerOp > 0 {
				bs = append(bs, float64(r.bPerOp))
			}
			if r.allocsPerOp > 0 {
				as = append(as, float64(r.allocsPerOp))
			}
			for k, v := range r.extras {
				extras[k] = append(extras[k], v)
			}
		}
		if len(bs) > 0 {
			rec.BytesPerOp = int(medianFloat(bs))
		}
		if len(as) > 0 {
			rec.AllocsPerOp = int(medianFloat(as))
		}
		for k, v := range extras {
			rec.Extra[k] = medianFloat(v)
		}
		out = append(out, rec)
	}
	return out
}

func medianFloat(vs []float64) float64 {
	sort.Float64s(vs)
	return vs[len(vs)/2]
}
