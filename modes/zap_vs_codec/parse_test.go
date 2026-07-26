package zapvscodec

import (
	"strings"
	"testing"
	"time"
)

// TestParseLine_basic checks the minimal happy path: name-CPU iters ns/op.
func TestParseLine_basic(t *testing.T) {
	s, ok := parseLine("BenchmarkFoo-10   123456   42.5 ns/op")
	if !ok {
		t.Fatalf("expected parse to succeed")
	}
	if s.subtest != "BenchmarkFoo" {
		t.Errorf("subtest: got %q want BenchmarkFoo", s.subtest)
	}
	if s.iters != 123456 {
		t.Errorf("iters: got %d want 123456", s.iters)
	}
	if s.nsPerOp != 42.5 {
		t.Errorf("ns/op: got %f want 42.5", s.nsPerOp)
	}
}

// TestParseLine_with_memory checks B/op + allocs/op extraction.
func TestParseLine_with_memory(t *testing.T) {
	s, ok := parseLine("BenchmarkRejectBadMagic_Codec-10   6383806   337.8 ns/op   24248.65 MB/s   240 B/op   4 allocs/op")
	if !ok {
		t.Fatalf("parse failed")
	}
	if s.bPerOp != 240 {
		t.Errorf("B/op: got %d want 240", s.bPerOp)
	}
	if s.allocsPerOp != 4 {
		t.Errorf("allocs/op: got %d want 4", s.allocsPerOp)
	}
	if v := s.extras["MB/s"]; v != 24248.65 {
		t.Errorf("MB/s extra: got %f want 24248.65", v)
	}
}

// TestParseLine_extras checks domain-specific extras (verifies/sec etc).
func TestParseLine_extras(t *testing.T) {
	s, ok := parseLine("BenchmarkParallelVerify_ZAP_N4-10   1000000000   1.000 ns/op   12.72 ns/verify   78618638 verifies/sec   19654660 verifies/sec/goroutine   1 B/op   0 allocs/op")
	if !ok {
		t.Fatalf("parse failed")
	}
	if got := s.extras["ns/verify"]; got != 12.72 {
		t.Errorf("ns/verify: got %f want 12.72", got)
	}
	if got := s.extras["verifies/sec"]; got != 78618638 {
		t.Errorf("verifies/sec: got %f want 78618638", got)
	}
	if got := s.extras["verifies/sec/goroutine"]; got != 19654660 {
		t.Errorf("verifies/sec/goroutine: got %f want 19654660", got)
	}
}

// TestParseLine_non_bench rejects non-bench lines (headers, warnings).
func TestParseLine_non_bench(t *testing.T) {
	lines := []string{
		"goos: darwin",
		"goarch: arm64",
		"cpu: Apple M1 Max",
		"# github.com/luxfi/zap-vs-codec-bench.test",
		"PASS",
		"ok      github.com/luxfi/zap-vs-codec-bench    1.234s",
		"",
	}
	for _, l := range lines {
		if _, ok := parseLine(l); ok {
			t.Errorf("expected reject for %q", l)
		}
	}
}

// TestCollapse_multi_sample_median verifies median aggregation across
// repeated runs of the same subtest.
func TestCollapse_multi_sample_median(t *testing.T) {
	samples := []rawSample{
		{subtest: "BenchmarkA", iters: 100, nsPerOp: 10},
		{subtest: "BenchmarkA", iters: 100, nsPerOp: 20},
		{subtest: "BenchmarkA", iters: 100, nsPerOp: 30},
		{subtest: "BenchmarkB", iters: 50, nsPerOp: 100},
	}
	recs := collapse("zap-vs-codec", samples)
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	// Sorted alpha: A first.
	if recs[0].Subtest != "BenchmarkA" {
		t.Errorf("sort: got %q want BenchmarkA", recs[0].Subtest)
	}
	if recs[0].Median != 20*time.Nanosecond {
		t.Errorf("median A: got %v want 20ns", recs[0].Median)
	}
	if recs[1].Median != 100*time.Nanosecond {
		t.Errorf("median B: got %v want 100ns", recs[1].Median)
	}
}

// TestParseBenchOutput_end_to_end runs parse on a small sample of the
// real raw output from /tmp/zap-vs-codec/raw_bench_m1max.txt.
func TestParseBenchOutput_end_to_end(t *testing.T) {
	in := strings.Join([]string{
		"goos: darwin",
		"goarch: arm64",
		"pkg: github.com/luxfi/zap-vs-codec-bench",
		"cpu: Apple M1 Max",
		"BenchmarkRejectBadMagic_Codec-10                      \t 6383806\t       337.8 ns/op\t24248.65 MB/s\t     240 B/op\t       4 allocs/op",
		"BenchmarkRejectBadMagic_Codec-10                      \t 6820662\t       367.9 ns/op\t22266.82 MB/s\t     240 B/op\t       4 allocs/op",
		"BenchmarkRejectBadMagic_Codec-10                      \t 6784399\t       319.6 ns/op\t25629.42 MB/s\t     240 B/op\t       4 allocs/op",
		"BenchmarkRejectBadMagic_ZAP-10                        \t1000000000\t         2.098 ns/op\t3903956.26 MB/s\t       0 B/op\t       0 allocs/op",
		"BenchmarkRejectBadMagic_ZAP-10                        \t1000000000\t         2.104 ns/op\t3892977.80 MB/s\t       0 B/op\t       0 allocs/op",
		"BenchmarkRejectBadMagic_ZAP-10                        \t1000000000\t         2.093 ns/op\t3914278.95 MB/s\t       0 B/op\t       0 allocs/op",
		"PASS",
		"ok  \tgithub.com/luxfi/zap-vs-codec-bench\t6.123s",
		"",
	}, "\n")
	recs, err := parseBenchOutput("zap-vs-codec", in)
	if err != nil {
		t.Fatalf("parseBenchOutput: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("expected 2 records, got %d", len(recs))
	}
	codec := recs[0]
	zap := recs[1]
	if codec.Subtest != "BenchmarkRejectBadMagic_Codec" {
		t.Errorf("codec subtest: got %q", codec.Subtest)
	}
	if zap.Subtest != "BenchmarkRejectBadMagic_ZAP" {
		t.Errorf("zap subtest: got %q", zap.Subtest)
	}
	// Codec median = 337.8ns -> ~337ns (truncated to Duration ns).
	if codec.Median < 300*time.Nanosecond || codec.Median > 400*time.Nanosecond {
		t.Errorf("codec median %v out of range", codec.Median)
	}
	if codec.BytesPerOp != 240 {
		t.Errorf("codec B/op: got %d want 240", codec.BytesPerOp)
	}
	if codec.AllocsPerOp != 4 {
		t.Errorf("codec allocs/op: got %d want 4", codec.AllocsPerOp)
	}
	// ZAP median ~2ns.
	if zap.Median != 2*time.Nanosecond {
		t.Errorf("zap median: got %v want 2ns", zap.Median)
	}
	// ZAP allocs should be 0 — we suppress 0 from BytesPerOp/AllocsPerOp.
	if zap.AllocsPerOp != 0 {
		t.Errorf("zap allocs/op: got %d want 0", zap.AllocsPerOp)
	}
	// MB/s present in Extra.
	if _, ok := zap.Extra["MB/s"]; !ok {
		t.Errorf("zap missing MB/s extra: %v", zap.Extra)
	}
}
