// Package zapvscodec provides head-to-head benchmarks for codec vs ZAP wire formats.
//
// Methodology:
//   - We model the codec path using luxfi/codec/linearcodec — identical to what
//     luxd v1.10.x used through every Marshal/Unmarshal call on platformvm txs.
//   - We model the ZAP path using luxfi/zap, the wire format used by
//     vms/platformvm/txs/zap_native in luxd post-LP-023.
//
// Both paths transport a representative AddValidator-like payload:
//   - NetworkID (u32)
//   - BlockchainID (32 bytes)
//   - NodeID (20 bytes)
//   - Start/End/Weight (3 × u64)
//   - StakeAmount (u64)
//   - DelegationShares (u32)
//   - Memo (up to ~32 bytes)
//
// We deliberately use a payload that approximates a typical mainnet tx in
// shape (≈140 wire bytes) so the numbers translate to production cost.
package zapvscodec

import (
	"encoding/binary"

	"github.com/luxfi/codec"
	"github.com/luxfi/codec/linearcodec"

	"github.com/luxfi/zap"
)

// ----- Codec path -----

// codecValidatorTx mirrors a typical platformvm AddValidator-shaped struct.
// All fields tagged `serialize:"true"` so linearcodec picks them up via the
// reflectcodec/structFielder cache.
type codecValidatorTx struct {
	NetworkID        uint32   `serialize:"true"`
	BlockchainID     [32]byte `serialize:"true"`
	NodeID           [20]byte `serialize:"true"`
	Start            uint64   `serialize:"true"`
	End              uint64   `serialize:"true"`
	Weight           uint64   `serialize:"true"`
	StakeAmount      uint64   `serialize:"true"`
	DelegationShares uint32   `serialize:"true"`
	Memo             []byte   `serialize:"true"`
}

// codecManager is the singleton codec.Manager used by ALL codec benches.
// luxd v1.10.x has exactly one such manager per VM. The reflectcodec/structFielder
// cache it holds is the shared mutable state behind every Unmarshal.
var codecManager codec.Manager

// codecRaw is the linearcodec for direct (non-Manager) measurements.
var codecRaw codec.Codec

func init() {
	codecManager = codec.NewDefaultManager()
	c := linearcodec.NewDefault()
	codecRaw = c
	if err := codecManager.RegisterCodec(0, c); err != nil {
		panic(err)
	}
}

// MakeCodecValidatorBytes builds a representative encoded buffer using
// linearcodec, returning the wire bytes (version-prefixed).
func MakeCodecValidatorBytes() []byte {
	tx := codecValidatorTx{
		NetworkID:        1,
		BlockchainID:     [32]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, 24, 25, 26, 27, 28, 29, 30, 31, 32},
		NodeID:           [20]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x11, 0x22, 0x33, 0x44},
		Start:            1_700_000_000,
		End:              1_700_000_000 + 21*24*60*60,
		Weight:           2_000_000_000,
		StakeAmount:      2_000_000_000,
		DelegationShares: 200_000,
		Memo:             []byte("bench-validator-tx-rep-payload"),
	}
	b, err := codecManager.Marshal(0, &tx)
	if err != nil {
		panic(err)
	}
	return b
}

// UnmarshalCodec walks the codec.Manager path exactly as luxd does.
// Returns the parsed struct (or nil on error).
func UnmarshalCodec(buf []byte) (*codecValidatorTx, error) {
	var tx codecValidatorTx
	if _, err := codecManager.Unmarshal(buf, &tx); err != nil {
		return nil, err
	}
	return &tx, nil
}

// MarshalCodec re-encodes a codecValidatorTx, allocating a fresh buffer.
func MarshalCodec(tx *codecValidatorTx) ([]byte, error) {
	return codecManager.Marshal(0, tx)
}

// ----- ZAP path -----

// ZAP schema: TxKind@0 (u8) + NetworkID@1 (u32) + BlockchainID@5 (32B) +
// NodeID@37 (20B) + Start@57 (u64) + End@65 (u64) + Weight@73 (u64) +
// StakeAmount@81 (u64) + DelegationShares@89 (u32) + Memo@93 (offset+len, 8B).
//
// Fixed section is 101 bytes; variable section holds memo bytes.
const (
	zapTxKindValidator uint8 = 0x10

	offTxKind           = 0
	offNetworkID        = 1
	offBlockchainID     = 5
	offNodeID           = 37
	offStart            = 57
	offEnd              = 65
	offWeight           = 73
	offStakeAmount      = 81
	offDelegationShares = 89
	offMemo             = 93
	sizeFixed           = 101
)

// MakeZAPValidatorBytes builds the same logical tx using the ZAP builder.
// Inline 32B BlockchainID and 20B NodeID are written via SetUint64 chunks
// (the platformvm zap_native modules use the same inline-fixed pattern).
func MakeZAPValidatorBytes() []byte {
	memo := []byte("bench-validator-tx-rep-payload")
	b := zap.NewBuilder(zap.HeaderSize + 16 + sizeFixed + len(memo))
	ob := b.StartObject(sizeFixed)
	ob.SetUint8(offTxKind, zapTxKindValidator)
	ob.SetUint32(offNetworkID, 1)
	// 32B BlockchainID: 4×uint64
	ob.SetUint64(offBlockchainID+0, 0x0807060504030201)
	ob.SetUint64(offBlockchainID+8, 0x100f0e0d0c0b0a09)
	ob.SetUint64(offBlockchainID+16, 0x1817161514131211)
	ob.SetUint64(offBlockchainID+24, 0x201f1e1d1c1b1a19)
	// 20B NodeID: 2×uint64 + 1×uint32
	ob.SetUint64(offNodeID+0, 0x8877665544332211)
	ob.SetUint64(offNodeID+8, 0x8877665544332211)
	ob.SetUint32(offNodeID+16, 0x44332211)
	ob.SetUint64(offStart, 1_700_000_000)
	ob.SetUint64(offEnd, 1_700_000_000+21*24*60*60)
	ob.SetUint64(offWeight, 2_000_000_000)
	ob.SetUint64(offStakeAmount, 2_000_000_000)
	ob.SetUint32(offDelegationShares, 200_000)
	ob.SetBytes(offMemo, memo)
	ob.FinishAsRoot()
	return b.Finish()
}

// CorruptZAPBytes makes a corrupted ZAP buffer of the given size: starts with
// a wrong magic byte and otherwise contains arbitrary payload bytes. Used to
// measure fail-fast rejection on bad input.
func CorruptZAPBytes(size int) []byte {
	buf := make([]byte, size)
	// Wrong magic: "XYZ\x00" — fails at byte 4 in zap.Parse.
	buf[0] = 'X'
	buf[1] = 'Y'
	buf[2] = 'Z'
	// Fill rest with arbitrary content
	for i := 4; i < size; i++ {
		buf[i] = byte(i & 0xFF)
	}
	return buf
}

// CorruptCodecBytes makes a "corrupted-by-overrun" codec buffer: a valid
// version prefix (0,0) but a payload that claims a memo length greater than
// the remaining buffer. Codec will walk all fields up to the bad length and
// then fail. Buffer is sized to the same overall length as the corresponding
// ZAP buffer for apples-to-apples comparison.
func CorruptCodecBytes(size int) []byte {
	buf := make([]byte, size)
	// Big-endian uint16 version = 0
	buf[0] = 0
	buf[1] = 0
	// Fill with plausible field bytes (all zero is fine — fields parse OK)
	// Then at the very end, claim a huge memo length to force a read error
	// AFTER walking all preceding fields. This is the worst case for codec:
	// it must traverse the whole tx, only failing at the last field.
	memoLenOff := size - 4
	if memoLenOff >= 0 {
		binary.BigEndian.PutUint32(buf[memoLenOff:], 0x7FFFFFFF) // 2GB memo
	}
	return buf
}

// ZAPValidatorTx is the zero-copy typed accessor over a ZAP buffer.
type ZAPValidatorTx struct {
	msg *zap.Message
	obj zap.Object
}

// WrapZAPValidator does the discriminator + version check, then returns a
// zero-copy accessor. Errors out at byte 4 on bad magic, byte 6 on bad version,
// byte 16 on too-short, byte 17 on wrong kind.
func WrapZAPValidator(buf []byte) (ZAPValidatorTx, error) {
	msg, err := zap.Parse(buf)
	if err != nil {
		return ZAPValidatorTx{}, err
	}
	if msg.Version() != zap.Version2 {
		return ZAPValidatorTx{}, errBadVersion
	}
	obj := msg.Root()
	if obj.Uint8(offTxKind) != zapTxKindValidator {
		return ZAPValidatorTx{}, errBadKind
	}
	return ZAPValidatorTx{msg: msg, obj: obj}, nil
}

func (t ZAPValidatorTx) NetworkID() uint32        { return t.obj.Uint32(offNetworkID) }
func (t ZAPValidatorTx) Start() uint64            { return t.obj.Uint64(offStart) }
func (t ZAPValidatorTx) End() uint64              { return t.obj.Uint64(offEnd) }
func (t ZAPValidatorTx) Weight() uint64           { return t.obj.Uint64(offWeight) }
func (t ZAPValidatorTx) StakeAmount() uint64      { return t.obj.Uint64(offStakeAmount) }
func (t ZAPValidatorTx) DelegationShares() uint32 { return t.obj.Uint32(offDelegationShares) }
func (t ZAPValidatorTx) Memo() []byte             { return t.obj.Bytes(offMemo) }
func (t ZAPValidatorTx) Bytes() []byte            { return t.msg.Bytes() }
func (t ZAPValidatorTx) IsZero() bool             { return t.msg == nil }
