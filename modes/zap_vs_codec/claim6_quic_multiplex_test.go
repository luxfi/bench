package zapvscodec

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Claim 6: QUIC stream multiplex without serialization.
//
// Methodology: 100 concurrent in-flight "QUIC streams" share a single
// underlying buffer-pool. Each stream represents one validator-to-validator
// channel; multiple streams may carry the same logical tx (e.g. gossip fan-
// out from the same proposer to N peers).
//
// - Codec: each stream's outgoing payload is re-encoded from the local Go
//   struct. Codec.Manager is a singleton with sync.RWMutex on its
//   reflectcodec/structFielder cache; the readers all hit the same RWMutex.
// - ZAP: each stream reads from the same in-memory buffer. No
//   serialization step. Multiple readers = multiple `Wrap` calls over the
//   same []byte.
//
// What this measures: total ops/sec at concurrency=100, where each "op" is
// "extract tx field N and forward to the wire-write goroutine".
//
// What this does NOT measure: actual QUIC stream-multiplex cost (we model
// streams as goroutines reading from a shared buffer pool).

const quicStreamCount = 100
const quicTestWindow = 500 * time.Millisecond

func quicMultiplexBench(b *testing.B, makeBuf func() []byte, op func([]byte) error) {
	buf := makeBuf()
	var total atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	wg.Add(quicStreamCount)
	for s := 0; s < quicStreamCount; s++ {
		go func() {
			defer wg.Done()
			<-start
			deadline := time.Now().Add(quicTestWindow)
			var local int64
			for time.Now().Before(deadline) {
				for i := 0; i < 256; i++ {
					if err := op(buf); err != nil {
						return
					}
					local++
				}
			}
			total.Add(local)
		}()
	}
	b.ReportAllocs()
	b.ResetTimer()
	t0 := time.Now()
	close(start)
	wg.Wait()
	b.StopTimer()
	wall := time.Since(t0)
	count := total.Load()
	if count == 0 {
		b.Fatal("no ops completed")
	}
	nsPerOp := float64(wall.Nanoseconds()) / float64(count)
	throughput := float64(count) / wall.Seconds()
	b.ReportMetric(nsPerOp, "ns/op")
	b.ReportMetric(throughput, "ops/sec")
	b.ReportMetric(throughput/float64(quicStreamCount), "ops/sec/stream")
	b.ReportMetric(float64(quicStreamCount), "streams")
	_ = runtime.GOMAXPROCS(0) // record for context
}

func BenchmarkQUICMultiplex_Codec(b *testing.B) {
	quicMultiplexBench(b, MakeCodecValidatorBytes, func(buf []byte) error {
		tx, err := UnmarshalCodec(buf)
		if err != nil {
			return err
		}
		_ = tx.Start
		// Re-marshal for outgoing QUIC stream write — every stream emits
		// its own bytes because the codec.Manager Marshal allocates fresh.
		out, err := MarshalCodec(tx)
		if err != nil {
			return err
		}
		_ = out
		return nil
	})
}

func BenchmarkQUICMultiplex_ZAP(b *testing.B) {
	quicMultiplexBench(b, MakeZAPValidatorBytes, func(buf []byte) error {
		tx, err := WrapZAPValidator(buf)
		if err != nil {
			return err
		}
		_ = tx.Start()
		// Outgoing QUIC stream write — just forward the same bytes,
		// no copy. Multiple streams read from the SAME underlying buffer.
		_ = tx.Bytes()
		return nil
	})
}
