// Package result is the one shared schema for every bench under luxbench.
//
// One harness, one way. Every mode under modes/* emits ResultRecord values
// into the same on-disk JSON file. Downstream consumers (luxbench-analyze,
// CI charts, dashboards) parse a single schema.
package result

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"sort"
	"time"
)

// ResultRecord is the canonical bench result row. Every mode produces a
// slice of these and writes them as a JSON array.
//
// Mode-specific data lives in Extra; the rest is mandatory and shared.
type ResultRecord struct {
	// Mode is the --mode value, e.g. "zap-vs-codec".
	Mode string `json:"mode"`
	// Subtest is the specific bench name inside the mode, e.g.
	// "BenchmarkRejectBadMagic_ZAP".
	Subtest string `json:"subtest"`
	// Architecture is a short host tag, e.g. "m1max" / "dbc" / "spark" /
	// "evo". Auto-detected via DetectArchitecture if the caller passes
	// the empty string.
	Architecture string `json:"architecture"`
	// Timestamp is the wall-clock when the bench finished.
	Timestamp time.Time `json:"timestamp"`
	// GoVersion is runtime.Version() at the time of the run.
	GoVersion string `json:"go_version"`
	// Iterations is the b.N value reported by `go test -bench` (or the
	// mode's equivalent).
	Iterations int `json:"iterations"`
	// Median is the median time per op across all reported runs of this
	// subtest in the same file. Modes wrapping `go test -bench` aggregate
	// b.N runs into one record per subtest.
	Median time.Duration `json:"median"`
	// P50, P95, P99 are quantiles across runs. If only one sample was
	// observed all three equal Median.
	P50 time.Duration `json:"p50"`
	P95 time.Duration `json:"p95"`
	P99 time.Duration `json:"p99"`
	// AllocsPerOp is the allocs/op count Go test -benchmem reports. 0
	// means either no allocs or no -benchmem.
	AllocsPerOp int `json:"allocs_per_op,omitempty"`
	// BytesPerOp is the B/op count -benchmem reports.
	BytesPerOp int `json:"bytes_per_op,omitempty"`
	// OpsPerSec is the computed throughput (1e9 / Median.Nanoseconds()).
	// Modes may override this with a domain-specific throughput.
	OpsPerSec float64 `json:"ops_per_sec,omitempty"`
	// Extra holds mode-specific metrics. Examples:
	//   zap_vs_codec: "MB/s", "verifies/sec", "streams"
	//   gpu:          "batch", "ms_per_batch", "kernel"
	//   multi_validator: "blocks_per_sec", "cert_tier_final"
	//   parity:       "scheme_a", "scheme_b", "diff_count"
	// Keep keys snake_case for downstream JQ.
	Extra map[string]interface{} `json:"extra,omitempty"`
}

// NewRecord stamps the mandatory fields. Mode is the only required value;
// arch defaults to DetectArchitecture(), timestamp to time.Now(), goVer to
// runtime.Version().
func NewRecord(mode, subtest string) ResultRecord {
	return ResultRecord{
		Mode:         mode,
		Subtest:      subtest,
		Architecture: DetectArchitecture(),
		Timestamp:    time.Now().UTC(),
		GoVersion:    runtime.Version(),
		Extra:        map[string]interface{}{},
	}
}

// File is the on-disk wrapper around []ResultRecord. We keep it explicit so
// future additions (run-level metadata) have a home that doesn't break old
// consumers.
type File struct {
	// Version is the schema version. Bump when ResultRecord changes shape
	// in a non-additive way. v1 is the initial drop.
	Version int `json:"version"`
	// Mode is the --mode value of the run that produced this file. All
	// records inside MUST have the same Mode.
	Mode string `json:"mode"`
	// Records are the individual subtest rows.
	Records []ResultRecord `json:"records"`
}

// WriteFile emits a sorted, pretty JSON array to path.
func WriteFile(path, mode string, records []ResultRecord) error {
	sort.Slice(records, func(i, j int) bool {
		return records[i].Subtest < records[j].Subtest
	})
	f := File{Version: 1, Mode: mode, Records: records}
	buf, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal records: %w", err)
	}
	buf = append(buf, '\n')
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// ReadFile loads records from a path written by WriteFile. Used by analysis
// tools (luxbench-analyze, future).
func ReadFile(path string) (File, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read %s: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(buf, &f); err != nil {
		return File{}, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return f, nil
}

// Quantile returns the q-th percentile of vals (0 <= q <= 100). vals must
// be sorted ascending. Returns 0 for an empty slice.
func Quantile(vals []time.Duration, q float64) time.Duration {
	if len(vals) == 0 {
		return 0
	}
	if q <= 0 {
		return vals[0]
	}
	if q >= 100 {
		return vals[len(vals)-1]
	}
	rank := q / 100 * float64(len(vals)-1)
	lo := int(rank)
	hi := lo + 1
	if hi >= len(vals) {
		return vals[lo]
	}
	frac := rank - float64(lo)
	return vals[lo] + time.Duration(frac*float64(vals[hi]-vals[lo]))
}

// SortDurations returns vals sorted ascending. Pure for callers that want
// to feed Quantile.
func SortDurations(vals []time.Duration) []time.Duration {
	out := make([]time.Duration, len(vals))
	copy(out, vals)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// DetectArchitecture picks a short host tag from the LUXBENCH_ARCH env
// var if set, else from common host hints (HOSTNAME). Fallback is
// runtime.GOOS-runtime.GOARCH for portability.
func DetectArchitecture() string {
	if v := os.Getenv("LUXBENCH_ARCH"); v != "" {
		return v
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		// Strip domain suffix (e.g. evo.local -> evo).
		for i, r := range host {
			if r == '.' {
				return host[:i]
			}
		}
		return host
	}
	return fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH)
}
