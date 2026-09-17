package zapvscodec

import (
	"testing"
)

// Claim 3: Speculative execute without re-marshal cost.
//
// Methodology: A consensus engine speculatively applies a candidate tx
// against multiple chain-tip versions in parallel; if the wrong fork wins,
// the loser's work is discarded.
//
// - Codec: per speculation, the executor must Unmarshal → apply to shadow
//   state → Marshal (in case the speculation needs to be gossiped) → drop.
// - ZAP: per speculation, Wrap → apply to shadow state → buffer reference.
//   Discarding = dropping the pointer.
//
// What this measures: per-speculation ns/op + B/op + total wasted-work cost
// when 1 of N speculations is accepted.
//
// What this does NOT measure: state-machine application cost itself (we
// model it as a no-op so the codec/ZAP overhead is the only variable).
//
// Depth values: 2, 4, 8 — modeling 2-way / 4-way / 8-way forks at chain tip.

func benchmarkSpeculate_Codec(b *testing.B, depth int) {
	buf := MakeCodecValidatorBytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(buf) * depth))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Per speculation: unmarshal, optionally remarshal for gossip.
		// Final accepted speculation is index depth-1; the others are
		// discarded — their remarshal work is wasted.
		for range depth {
			tx, err := UnmarshalCodec(buf)
			if err != nil {
				b.Fatal(err)
			}
			// Simulate "apply" by reading a field (load-bearing access).
			_ = tx.Start
			// Re-marshal the result so we can gossip the speculative state
			// if our fork wins. All but one of these is wasted.
			out, err := MarshalCodec(tx)
			if err != nil {
				b.Fatal(err)
			}
			_ = out
		}
	}
}

func benchmarkSpeculate_ZAP(b *testing.B, depth int) {
	buf := MakeZAPValidatorBytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(buf) * depth))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Per speculation: wrap, optionally pointer-share for gossip.
		for range depth {
			tx, err := WrapZAPValidator(buf)
			if err != nil {
				b.Fatal(err)
			}
			_ = tx.Start()
			// The "output for gossip" is just the same buffer — no allocation.
			gossipBuf := tx.Bytes()
			_ = gossipBuf
		}
	}
}

func BenchmarkSpeculate_Codec_D2(b *testing.B) { benchmarkSpeculate_Codec(b, 2) }
func BenchmarkSpeculate_Codec_D4(b *testing.B) { benchmarkSpeculate_Codec(b, 4) }
func BenchmarkSpeculate_Codec_D8(b *testing.B) { benchmarkSpeculate_Codec(b, 8) }
func BenchmarkSpeculate_ZAP_D2(b *testing.B)   { benchmarkSpeculate_ZAP(b, 2) }
func BenchmarkSpeculate_ZAP_D4(b *testing.B)   { benchmarkSpeculate_ZAP(b, 4) }
func BenchmarkSpeculate_ZAP_D8(b *testing.B)   { benchmarkSpeculate_ZAP(b, 8) }

// BenchmarkSpeculate_DiscardCost_Codec measures the wasted-work cost when
// only the FINAL speculation is accepted. We bench the marshal+unmarshal pair
// per losing speculation (depth-1 losers per accepted speculation).
func BenchmarkSpeculate_DiscardCost_Codec(b *testing.B) {
	buf := MakeCodecValidatorBytes()
	tx, _ := UnmarshalCodec(buf)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// One losing speculation = one unmarshal + one marshal that gets dropped.
		t2, err := UnmarshalCodec(buf)
		if err != nil {
			b.Fatal(err)
		}
		out, err := MarshalCodec(t2)
		if err != nil {
			b.Fatal(err)
		}
		// `out` is discarded — wasted bytes
		_ = out
		_ = tx
	}
}

// BenchmarkSpeculate_DiscardCost_ZAP measures the same wasted-work cost for
// ZAP — which is just one wrap (returned struct dropped on the floor; no
// allocation beyond the wrap itself).
func BenchmarkSpeculate_DiscardCost_ZAP(b *testing.B) {
	buf := MakeZAPValidatorBytes()
	tx, _ := WrapZAPValidator(buf)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		t2, err := WrapZAPValidator(buf)
		if err != nil {
			b.Fatal(err)
		}
		// `t2.Bytes()` is just the input pointer — no copy, no allocation.
		_ = t2.Bytes()
		_ = tx
	}
}
