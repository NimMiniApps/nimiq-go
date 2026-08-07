package nimiq

import (
	"encoding/hex"
	"strings"
	"testing"
)

// Each case below was found by FuzzParseTransaction. They are kept as explicit
// tests so the guarantees survive even when the fuzz corpus is not checked in.
//
// The shared theme: a decoder that accepts more than one byte string for the
// same transaction breaks the validate-then-broadcast pattern, because the
// bytes a caller inspected are not the bytes it would re-serialize and send.
func TestParseRejectsMalleableEncodings(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr string
	}{
		{
			// An extended transaction whose proof is zero-length: structurally
			// well formed, but nothing authorized it.
			name:    "unsigned extended transaction",
			raw:     "01" + rep("11", 20) + "00" + "00" + rep("22", 20) + "00" + "00" + u64(500) + u64(1) + "000000c8" + "05" + "00" + "00",
			wantErr: "no signature proof",
		},
		{
			// Flag nibble 0b0011: bit 0 is WebAuthn, bit 1 is undefined. Both
			// were previously dropped on re-serialization.
			name:    "undefined signature proof flags",
			raw:     "00" + "30" + rep("01", 32) + rep("02", 20) + u64(10000) + u64(0) + "00000064" + "05" + rep("03", 64),
			wantErr: "not supported",
		},
		{
			// ES256 keys and signatures differ in size from Ed25519 ones, so
			// continuing to parse would yield a wrong public key.
			name:    "ES256 algorithm in basic format",
			raw:     "00" + "01" + rep("01", 32) + rep("02", 20) + u64(10000) + u64(0) + "00000064" + "05" + rep("03", 64),
			wantErr: "not supported",
		},
		{
			// 0xe6 0x00 and 0x66 both decode to 102 under LEB128. Only the
			// minimal form may be accepted.
			name:    "non-canonical varint length",
			raw:     "01" + rep("11", 20) + "00" + "e600" + rep("00", 102) + rep("22", 20) + "00" + "00" + u64(500) + u64(1) + "000000c8" + "05" + "00" + "62" + validProof(),
			wantErr: "non-canonical",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := hex.DecodeString(c.raw)
			if err != nil {
				t.Fatalf("bad test fixture: %v", err)
			}
			_, err = ParseTransaction(raw)
			if err == nil {
				t.Fatalf("expected a parse error, got nil")
			}
			if !strings.Contains(err.Error(), c.wantErr) {
				t.Errorf("error %q does not mention %q", err, c.wantErr)
			}
		})
	}
}

// TestSerializeIsDeterministic checks that building the same transaction twice
// yields identical bytes — the property that makes a rebroadcast a no-op rather
// than a second payment.
func TestSerializeIsDeterministic(t *testing.T) {
	build := func() []byte {
		sender, _ := AddressFromBytes([]byte(strings.Repeat("\x11", 20)))
		recipient, _ := AddressFromBytes([]byte(strings.Repeat("\x22", 20)))
		proof, err := NewSignatureProof([]byte(strings.Repeat("\x01", 32)), []byte(strings.Repeat("\x03", 64)))
		if err != nil {
			t.Fatal(err)
		}
		tx := &Transaction{
			Sender: sender, Recipient: recipient,
			RecipientData:       []byte("payout-op:abc"),
			Value:               500,
			Fee:                 1,
			ValidityStartHeight: 200,
			Network:             NetworkTestAlbatross,
			Proof:               proof.Serialize(),
		}
		raw, err := tx.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if a, b := hex.EncodeToString(build()), hex.EncodeToString(build()); a != b {
		t.Errorf("serialization is not deterministic:\n%s\n%s", a, b)
	}
}

func rep(s string, n int) string { return strings.Repeat(s, n) }

func u64(v uint64) string {
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return hex.EncodeToString(b)
}

func validProof() string {
	return "00" + rep("01", 32) + "00" + rep("03", 64)
}
