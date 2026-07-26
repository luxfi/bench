package result

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewRecord_setsRequiredFields(t *testing.T) {
	t.Setenv("LUXBENCH_ARCH", "test-host")
	r := NewRecord("zap-vs-codec", "BenchmarkRejectBadMagic_ZAP")
	if r.Mode != "zap-vs-codec" {
		t.Errorf("Mode: got %q want zap-vs-codec", r.Mode)
	}
	if r.Subtest != "BenchmarkRejectBadMagic_ZAP" {
		t.Errorf("Subtest: got %q", r.Subtest)
	}
	if r.Architecture != "test-host" {
		t.Errorf("Architecture: got %q want test-host", r.Architecture)
	}
	if r.GoVersion == "" {
		t.Errorf("GoVersion empty")
	}
	if r.Timestamp.IsZero() {
		t.Errorf("Timestamp zero")
	}
	if r.Extra == nil {
		t.Errorf("Extra is nil; should be initialized map")
	}
}

func TestWriteReadFile_roundtrip(t *testing.T) {
	t.Setenv("LUXBENCH_ARCH", "rt-host")
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")

	r := NewRecord("zap-vs-codec", "BenchmarkA")
	r.Median = 42 * time.Nanosecond
	r.P50, r.P95, r.P99 = r.Median, r.Median, r.Median
	r.Iterations = 1000
	r.BytesPerOp = 8
	r.AllocsPerOp = 1
	r.OpsPerSec = 23_809_523.8
	r.Extra["MB/s"] = 1234.5

	if err := WriteFile(path, "zap-vs-codec", []ResultRecord{r}); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if st.Size() == 0 {
		t.Fatalf("file empty")
	}

	f, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if f.Version != 1 {
		t.Errorf("version: got %d want 1", f.Version)
	}
	if f.Mode != "zap-vs-codec" {
		t.Errorf("mode: got %q", f.Mode)
	}
	if len(f.Records) != 1 {
		t.Fatalf("records: got %d want 1", len(f.Records))
	}
	got := f.Records[0]
	if got.Subtest != "BenchmarkA" {
		t.Errorf("subtest: got %q", got.Subtest)
	}
	if got.Median != 42*time.Nanosecond {
		t.Errorf("median: got %v", got.Median)
	}
	if got.BytesPerOp != 8 {
		t.Errorf("B/op: got %d", got.BytesPerOp)
	}
	if got.Extra["MB/s"] != 1234.5 {
		t.Errorf("MB/s: got %v", got.Extra["MB/s"])
	}
}

func TestWriteFile_sortsRecords(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.json")
	recs := []ResultRecord{
		NewRecord("m", "BenchmarkB"),
		NewRecord("m", "BenchmarkA"),
		NewRecord("m", "BenchmarkC"),
	}
	if err := WriteFile(path, "m", recs); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	f, err := ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	want := []string{"BenchmarkA", "BenchmarkB", "BenchmarkC"}
	for i, r := range f.Records {
		if r.Subtest != want[i] {
			t.Errorf("rec[%d]: got %q want %q", i, r.Subtest, want[i])
		}
	}
}

func TestQuantile_basic(t *testing.T) {
	in := SortDurations([]time.Duration{10, 30, 50, 70, 90})
	cases := []struct {
		q    float64
		want time.Duration
	}{
		{0, 10},
		{50, 50},
		{100, 90},
		{25, 30}, // linear interp between 10 and 30 at rank 1 = 30
	}
	for _, c := range cases {
		if got := Quantile(in, c.q); got != c.want {
			t.Errorf("Quantile(%v) = %v want %v", c.q, got, c.want)
		}
	}
}

func TestQuantile_empty(t *testing.T) {
	if got := Quantile(nil, 50); got != 0 {
		t.Errorf("empty Quantile: got %v want 0", got)
	}
}

func TestDetectArchitecture_envWins(t *testing.T) {
	t.Setenv("LUXBENCH_ARCH", "force-host")
	if got := DetectArchitecture(); got != "force-host" {
		t.Errorf("Architecture env override: got %q want force-host", got)
	}
}
