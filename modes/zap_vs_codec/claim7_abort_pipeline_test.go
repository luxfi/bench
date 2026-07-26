package zapvscodec

import (
	"testing"
)

// Claim 7: Atomic discard of mid-pipeline work.
//
// Methodology: An 8-stage pipeline where each stage processes a tx and
// forwards it to the next. At stage 6 we abort.
//
// - Codec: stages 1-5 each Unmarshal'd the incoming bytes and Marshal'd
//   outgoing bytes. Abort means stages 1-5's marshal allocations are
//   wasted; stage 6's unmarshal is wasted; the GC will reclaim them.
// - ZAP: stages 1-5 each Wrap'd the incoming bytes (pointer-share) and
//   forwarded the SAME []byte. Stage 6's wrap is also pointer-share.
//   Abort = drop the pointer; nothing to reclaim that wouldn't be
//   reclaimed anyway.
//
// What this measures: total wasted-work ns + B + allocs per abort cycle.
//
// What this does NOT measure: state-machine work in each stage (we model
// it as a single field read).

const abortStages = 8
const abortAtStage = 6

func BenchmarkAbortAt6_Codec(b *testing.B) {
	buf := MakeCodecValidatorBytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(buf) * abortAtStage))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		current := buf
		for stage := 1; stage <= abortAtStage; stage++ {
			tx, err := UnmarshalCodec(current)
			if err != nil {
				b.Fatal(err)
			}
			_ = tx.Start
			// Re-marshal for forwarding
			out, err := MarshalCodec(tx)
			if err != nil {
				b.Fatal(err)
			}
			current = out
		}
		// At stage 6 we abort. `current` and all intermediates are wasted.
		_ = current
	}
}

func BenchmarkAbortAt6_ZAP(b *testing.B) {
	buf := MakeZAPValidatorBytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(buf) * abortAtStage))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		current := buf
		for stage := 1; stage <= abortAtStage; stage++ {
			tx, err := WrapZAPValidator(current)
			if err != nil {
				b.Fatal(err)
			}
			_ = tx.Start()
			// Forward by pointer — SAME bytes propagate.
			current = tx.Bytes()
		}
		// Abort = drop `current`. Since it's the same slice that came in,
		// nothing is wasted that wouldn't be GC'd anyway.
		_ = current
	}
}

// Full 8-stage pipeline (no abort) for amortized cost comparison.

func BenchmarkPipelineFull_Codec(b *testing.B) {
	buf := MakeCodecValidatorBytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(buf) * abortStages))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		current := buf
		for stage := 1; stage <= abortStages; stage++ {
			tx, err := UnmarshalCodec(current)
			if err != nil {
				b.Fatal(err)
			}
			_ = tx.Start
			out, err := MarshalCodec(tx)
			if err != nil {
				b.Fatal(err)
			}
			current = out
		}
		_ = current
	}
}

func BenchmarkPipelineFull_ZAP(b *testing.B) {
	buf := MakeZAPValidatorBytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(buf) * abortStages))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		current := buf
		for stage := 1; stage <= abortStages; stage++ {
			tx, err := WrapZAPValidator(current)
			if err != nil {
				b.Fatal(err)
			}
			_ = tx.Start()
			current = tx.Bytes()
		}
		_ = current
	}
}
