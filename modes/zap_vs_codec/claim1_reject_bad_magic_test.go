package zapvscodec

import (
	"testing"
)

// Claim 1: Reject bad bytes mid-stream.
//
// Methodology: Construct an 8192-byte payload.
//   - ZAP path: first 4 bytes are wrong magic; zap.Parse rejects after 4 bytes.
//   - Codec path: payload structured so codec must walk all fields and only
//     fails on the last (memo) length, simulating the "validator runs the
//     whole tx through reflection before failing" worst case.
//
// What this bench measures: time-to-reject ns/op + B/op + allocs/op.
// What this bench does NOT measure: end-to-end network reception (both
// paths assume the full buffer is in memory).

const corruptBufSize = 8192

// BenchmarkRejectBadMagic_Codec measures the cost of an invalid codec buffer
// being fully walked before the per-field length check trips.
func BenchmarkRejectBadMagic_Codec(b *testing.B) {
	buf := CorruptCodecBytes(corruptBufSize)
	b.ReportAllocs()
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := UnmarshalCodec(buf)
		if err == nil {
			b.Fatal("expected error, got nil")
		}
	}
}

// BenchmarkRejectBadMagic_ZAP measures the cost of a ZAP buffer rejected at
// byte 4 (bad magic) — read 4 bytes, compare, return error.
func BenchmarkRejectBadMagic_ZAP(b *testing.B) {
	buf := CorruptZAPBytes(corruptBufSize)
	b.ReportAllocs()
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := WrapZAPValidator(buf)
		if err == nil {
			b.Fatal("expected error, got nil")
		}
	}
}

// BenchmarkRejectBadMagic_ZAP_TruncatedHeader measures the cost of an even
// shorter ZAP rejection — buffer is HeaderSize-1 bytes, so zap.Parse aborts
// before even reading the magic. Worst-case-for-attacker scenario.
func BenchmarkRejectBadMagic_ZAP_TruncatedHeader(b *testing.B) {
	buf := make([]byte, 15) // < zap.HeaderSize (16)
	b.ReportAllocs()
	b.SetBytes(int64(len(buf)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := WrapZAPValidator(buf)
		if err == nil {
			b.Fatal("expected error, got nil")
		}
	}
}
