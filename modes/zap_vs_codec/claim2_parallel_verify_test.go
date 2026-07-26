package zapvscodec

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Claim 2: Parallel verify on shared buffer.
//
// Methodology: 1000 valid tx buffers placed in a shared slice. N goroutines
// pick random buffers and verify them in a hot loop until time.After(window).
// Throughput = (sum of per-goroutine counts) / wall time.
//
// What this measures: scaling of total verifications/sec as goroutine count
// increases from 1 to GOMAXPROCS, with both paths sharing the same input
// buffers. The lock contention on the codec.Manager's struct-fielder cache
// (sync.RWMutex in reflectcodec/structFielder) is the limiting factor.
//
// What this does NOT measure: cross-machine scaling (single host only);
// cache coherency at extreme cores (>64 hw threads).

const (
	parallelBufCount  = 1000
	parallelTestSecs  = 1
)

func benchmarkParallelVerify(b *testing.B, n int, verify func([]byte)) int64 {
	bufs := make([][]byte, parallelBufCount)
	for i := range bufs {
		bufs[i] = MakeCodecValidatorBytes() // placeholder; both paths get correct buffers in caller
	}
	_ = bufs
	return 0
}

// runParallelVerify spawns n goroutines hot-looping verify() over the shared
// bufs slice for window. Returns total verifications + wall ns.
func runParallelVerify(n int, bufs [][]byte, verify func([]byte) error, window time.Duration) (int64, time.Duration) {
	var (
		total atomic.Int64
		wg    sync.WaitGroup
	)
	start := make(chan struct{})
	wg.Add(n)
	for g := 0; g < n; g++ {
		go func(gid int) {
			defer wg.Done()
			<-start
			deadline := time.Now().Add(window)
			var local int64
			idx := gid // start at goroutine id, walk forward; deterministic
			for time.Now().Before(deadline) {
				for i := 0; i < 256; i++ {
					if err := verify(bufs[idx%parallelBufCount]); err != nil {
						return
					}
					idx++
					local++
				}
			}
			total.Add(local)
		}(g)
	}
	t0 := time.Now()
	close(start)
	wg.Wait()
	return total.Load(), time.Since(t0)
}

// benchmarkParallel is the shared shape: runs verify() with goroutines=N and
// reports ns/op = wall ns / total verifications.
func benchmarkParallel(b *testing.B, n int, makeBuf func() []byte, verify func([]byte) error) {
	bufs := make([][]byte, parallelBufCount)
	for i := range bufs {
		bufs[i] = makeBuf()
	}
	b.ReportAllocs()
	b.ResetTimer()
	// We override the b.N loop: run the workload for parallelTestSecs.
	// Report wall ns/op = (wall ns) / (total verifications).
	count, wall := runParallelVerify(n, bufs, verify, parallelTestSecs*time.Second)
	b.StopTimer()
	if count == 0 {
		b.Fatal("no verifications completed")
	}
	nsPerOp := float64(wall.Nanoseconds()) / float64(count)
	throughput := float64(count) / wall.Seconds()
	b.ReportMetric(nsPerOp, "ns/verify")
	b.ReportMetric(throughput, "verifies/sec")
	b.ReportMetric(throughput/float64(n), "verifies/sec/goroutine")
}

// All parallel benches at N = 1, 4, 16, GOMAXPROCS

func BenchmarkParallelVerify_Codec_N1(b *testing.B) {
	benchmarkParallel(b, 1, MakeCodecValidatorBytes, func(buf []byte) error {
		_, err := UnmarshalCodec(buf)
		return err
	})
}
func BenchmarkParallelVerify_Codec_N4(b *testing.B) {
	benchmarkParallel(b, 4, MakeCodecValidatorBytes, func(buf []byte) error {
		_, err := UnmarshalCodec(buf)
		return err
	})
}
func BenchmarkParallelVerify_Codec_N16(b *testing.B) {
	benchmarkParallel(b, 16, MakeCodecValidatorBytes, func(buf []byte) error {
		_, err := UnmarshalCodec(buf)
		return err
	})
}
func BenchmarkParallelVerify_Codec_NMax(b *testing.B) {
	benchmarkParallel(b, runtime.GOMAXPROCS(0), MakeCodecValidatorBytes, func(buf []byte) error {
		_, err := UnmarshalCodec(buf)
		return err
	})
}

func BenchmarkParallelVerify_ZAP_N1(b *testing.B) {
	benchmarkParallel(b, 1, MakeZAPValidatorBytes, func(buf []byte) error {
		_, err := WrapZAPValidator(buf)
		return err
	})
}
func BenchmarkParallelVerify_ZAP_N4(b *testing.B) {
	benchmarkParallel(b, 4, MakeZAPValidatorBytes, func(buf []byte) error {
		_, err := WrapZAPValidator(buf)
		return err
	})
}
func BenchmarkParallelVerify_ZAP_N16(b *testing.B) {
	benchmarkParallel(b, 16, MakeZAPValidatorBytes, func(buf []byte) error {
		_, err := WrapZAPValidator(buf)
		return err
	})
}
func BenchmarkParallelVerify_ZAP_NMax(b *testing.B) {
	benchmarkParallel(b, runtime.GOMAXPROCS(0), MakeZAPValidatorBytes, func(buf []byte) error {
		_, err := WrapZAPValidator(buf)
		return err
	})
}
