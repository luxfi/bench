package zapvscodec

import (
	"crypto/sha256"
	"sort"
	"testing"
	"time"
)

// End-to-end: total block / op / sec mainnet-load simulation.
//
// Methodology: 1000 txs/block, target 1 block/sec. Per tx: parse → tx-hash
// (sha256, modeling sig-verify cost) → field-read (modeling balance check)
// → buffer-pass (modeling state write).
//
// What this measures:
//   - Total block-process ns
//   - p50, p95, p99 per-tx ns
//   - Memory allocation rate (B/op summed)
//   - ops/sec (txs per second)
//
// What this does NOT measure:
//   - Actual BLS verification (replaced with sha256 to keep both paths
//     comparable; both incur identical sig-verify cost in production).
//   - Actual state DB I/O (modeled as a buffer-pass).

const e2eTxPerBlock = 1000

func processBlockCodec(b *testing.B) (time.Duration, []int64) {
	block := make([][]byte, e2eTxPerBlock)
	for i := range block {
		block[i] = MakeCodecValidatorBytes()
	}
	latencies := make([]int64, e2eTxPerBlock)
	t0 := time.Now()
	for i, buf := range block {
		txStart := time.Now()
		// 1. Parse
		tx, err := UnmarshalCodec(buf)
		if err != nil {
			b.Fatal(err)
		}
		// 2. Sig-verify cost surrogate
		_ = sha256.Sum256(buf)
		// 3. Field-read (balance check)
		_ = tx.Start + tx.End + tx.Weight
		// 4. Buffer-pass (state write — model as remarshal to "block storage")
		out, err := MarshalCodec(tx)
		if err != nil {
			b.Fatal(err)
		}
		_ = out
		latencies[i] = time.Since(txStart).Nanoseconds()
	}
	return time.Since(t0), latencies
}

func processBlockZAP(b *testing.B) (time.Duration, []int64) {
	block := make([][]byte, e2eTxPerBlock)
	for i := range block {
		block[i] = MakeZAPValidatorBytes()
	}
	latencies := make([]int64, e2eTxPerBlock)
	t0 := time.Now()
	for i, buf := range block {
		txStart := time.Now()
		// 1. Wrap
		tx, err := WrapZAPValidator(buf)
		if err != nil {
			b.Fatal(err)
		}
		// 2. Sig-verify cost surrogate
		_ = sha256.Sum256(buf)
		// 3. Field-read
		_ = tx.Start() + tx.End() + tx.Weight()
		// 4. Buffer-pass (same bytes, no allocation)
		_ = tx.Bytes()
		latencies[i] = time.Since(txStart).Nanoseconds()
	}
	return time.Since(t0), latencies
}

func percentile(sorted []int64, p float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(float64(len(sorted)-1) * p / 100.0)
	return sorted[idx]
}

func BenchmarkE2EBlock_Codec(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var (
		totalBlockNs int64
		allLat       []int64
	)
	for i := 0; i < b.N; i++ {
		blockNs, lats := processBlockCodec(b)
		totalBlockNs += blockNs.Nanoseconds()
		allLat = append(allLat, lats...)
	}
	b.StopTimer()
	sort.Slice(allLat, func(i, j int) bool { return allLat[i] < allLat[j] })
	b.ReportMetric(float64(totalBlockNs)/float64(b.N), "ns/block")
	b.ReportMetric(float64(percentile(allLat, 50)), "p50-ns/tx")
	b.ReportMetric(float64(percentile(allLat, 95)), "p95-ns/tx")
	b.ReportMetric(float64(percentile(allLat, 99)), "p99-ns/tx")
	// blocks/sec at this rate
	if totalBlockNs > 0 {
		blocksPerSec := float64(b.N) / (float64(totalBlockNs) / 1e9)
		b.ReportMetric(blocksPerSec, "blocks/sec")
		b.ReportMetric(blocksPerSec*e2eTxPerBlock, "txs/sec")
	}
}

func BenchmarkE2EBlock_ZAP(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	var (
		totalBlockNs int64
		allLat       []int64
	)
	for i := 0; i < b.N; i++ {
		blockNs, lats := processBlockZAP(b)
		totalBlockNs += blockNs.Nanoseconds()
		allLat = append(allLat, lats...)
	}
	b.StopTimer()
	sort.Slice(allLat, func(i, j int) bool { return allLat[i] < allLat[j] })
	b.ReportMetric(float64(totalBlockNs)/float64(b.N), "ns/block")
	b.ReportMetric(float64(percentile(allLat, 50)), "p50-ns/tx")
	b.ReportMetric(float64(percentile(allLat, 95)), "p95-ns/tx")
	b.ReportMetric(float64(percentile(allLat, 99)), "p99-ns/tx")
	if totalBlockNs > 0 {
		blocksPerSec := float64(b.N) / (float64(totalBlockNs) / 1e9)
		b.ReportMetric(blocksPerSec, "blocks/sec")
		b.ReportMetric(blocksPerSec*e2eTxPerBlock, "txs/sec")
	}
}
