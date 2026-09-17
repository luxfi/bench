package zapvscodec

import (
	"testing"
)

// Claim 4: Block stream without re-encode (5 hops).
//
// Methodology: A block containing 1000 txs propagates through a 5-hop
// pipeline (proposer → validator-A → validator-B → validator-C → light client).
// At each hop, the legacy codec implementation must Unmarshal the incoming
// bytes and then Marshal new bytes for forwarding, because the local view
// holds Go structs, not the original buffer.
//
// ZAP path: each hop receives the buffer and forwards the buffer. No
// codec ops at any hop.
//
// What this measures: total per-block-propagation cost across 5 hops at
// 1000 txs/block. ns/op = total time to push the block end-to-end.
//
// What this does NOT measure: actual network latency between hops (we
// model them as in-process function calls). Real-world propagation adds
// network RTT to BOTH paths equally, so the relative ratio is unchanged.

const (
	streamTxCount  = 1000
	streamHopCount = 5
)

// hopCodec simulates a single validator hop in the codec path: unmarshal,
// inspect (read 1 field), remarshal for outgoing.
func hopCodec(in []byte) ([]byte, error) {
	tx, err := UnmarshalCodec(in)
	if err != nil {
		return nil, err
	}
	_ = tx.Start
	return MarshalCodec(tx)
}

// hopZAP simulates a single validator hop in the ZAP path: wrap, inspect, forward.
func hopZAP(in []byte) ([]byte, error) {
	tx, err := WrapZAPValidator(in)
	if err != nil {
		return nil, err
	}
	_ = tx.Start()
	return tx.Bytes(), nil // SAME bytes — no copy
}

func BenchmarkBlockStream_Codec(b *testing.B) {
	block := make([][]byte, streamTxCount)
	for i := range block {
		block[i] = MakeCodecValidatorBytes()
	}
	totalBytes := int64(0)
	for _, buf := range block {
		totalBytes += int64(len(buf))
	}
	b.ReportAllocs()
	b.SetBytes(totalBytes * streamHopCount)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		// 5 hops: each hop re-marshals every tx.
		current := block
		for range streamHopCount {
			next := make([][]byte, len(current))
			for i, buf := range current {
				out, err := hopCodec(buf)
				if err != nil {
					b.Fatal(err)
				}
				next[i] = out
			}
			current = next
		}
		_ = current
	}
}

func BenchmarkBlockStream_ZAP(b *testing.B) {
	block := make([][]byte, streamTxCount)
	for i := range block {
		block[i] = MakeZAPValidatorBytes()
	}
	totalBytes := int64(0)
	for _, buf := range block {
		totalBytes += int64(len(buf))
	}
	b.ReportAllocs()
	b.SetBytes(totalBytes * streamHopCount)
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		// 5 hops: each hop forwards bytes byte-for-byte (no marshal).
		current := block
		for range streamHopCount {
			next := make([][]byte, len(current))
			for i, buf := range current {
				out, err := hopZAP(buf)
				if err != nil {
					b.Fatal(err)
				}
				next[i] = out
			}
			current = next
		}
		_ = current
	}
}

// BenchmarkBlockStreamPerHop_Codec / _ZAP measure the per-hop ns/op for one
// tx, so per-tx-per-hop overhead is directly comparable to the per-claim
// micro-benches.
func BenchmarkBlockStreamPerHop_Codec(b *testing.B) {
	buf := MakeCodecValidatorBytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := hopCodec(buf)
		if err != nil {
			b.Fatal(err)
		}
		_ = out
	}
}

func BenchmarkBlockStreamPerHop_ZAP(b *testing.B) {
	buf := MakeZAPValidatorBytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		out, err := hopZAP(buf)
		if err != nil {
			b.Fatal(err)
		}
		_ = out
	}
}
