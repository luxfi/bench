package zapvscodec

import (
	"testing"
)

func TestSmokeCodecRoundtrip(t *testing.T) {
	buf := MakeCodecValidatorBytes()
	if len(buf) == 0 {
		t.Fatal("empty codec bytes")
	}
	tx, err := UnmarshalCodec(buf)
	if err != nil {
		t.Fatalf("codec unmarshal: %v", err)
	}
	if tx.NetworkID != 1 {
		t.Errorf("codec NetworkID = %d, want 1", tx.NetworkID)
	}
	if tx.Start != 1_700_000_000 {
		t.Errorf("codec Start = %d, want 1700000000", tx.Start)
	}
	t.Logf("codec bytes: %d", len(buf))
}

func TestSmokeZAPRoundtrip(t *testing.T) {
	buf := MakeZAPValidatorBytes()
	if len(buf) == 0 {
		t.Fatal("empty zap bytes")
	}
	tx, err := WrapZAPValidator(buf)
	if err != nil {
		t.Fatalf("zap wrap: %v", err)
	}
	if tx.NetworkID() != 1 {
		t.Errorf("zap NetworkID = %d, want 1", tx.NetworkID())
	}
	if tx.Start() != 1_700_000_000 {
		t.Errorf("zap Start = %d, want 1700000000", tx.Start())
	}
	t.Logf("zap bytes: %d", len(buf))
}

func TestSmokeBadMagicRejection(t *testing.T) {
	buf := CorruptZAPBytes(8192)
	_, err := WrapZAPValidator(buf)
	if err == nil {
		t.Fatal("expected error for corrupt magic, got nil")
	}
}
