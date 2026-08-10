package nimiq

import (
	"context"
	"strings"
	"testing"
)

// signedRaw builds a signed transaction and returns its wire bytes, standing in
// for what a browser wallet would hand a backend.
func signedRaw(t *testing.T, network NetworkID, value Luna, data []byte) ([]byte, Address, Address) {
	t.Helper()
	s, addr := messageSigner(t)
	recipient := MustParseAddress("NQ34 248H 248H 248H 248H 248H 248H 248H 248H")

	tx, err := NewBasicTransaction(addr, recipient, value, 0, 100, network)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > 0 {
		tx = tx.WithData(data)
	}
	if err := SignTransaction(context.Background(), s, tx); err != nil {
		t.Fatal(err)
	}
	raw, err := tx.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw, addr, recipient
}

func TestParseAndVerifyAcceptsGenuine(t *testing.T) {
	raw, sender, recipient := signedRaw(t, NetworkTestAlbatross, 1000, []byte("invoice-42"))

	tx, err := ParseAndVerify(raw, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Sender != sender || tx.Recipient != recipient {
		t.Error("parsed addresses do not match")
	}
	if tx.Value != 1000 {
		t.Errorf("value = %d, want 1000", tx.Value)
	}
	if string(tx.RecipientData) != "invoice-42" {
		t.Errorf("data = %q", tx.RecipientData)
	}
}

// TestParseAndVerifyRejectsWrongNetwork is replay protection: a transaction
// prepared on a test chain must not be accepted by a mainnet backend.
func TestParseAndVerifyRejectsWrongNetwork(t *testing.T) {
	raw, _, _ := signedRaw(t, NetworkTestAlbatross, 1000, nil)

	_, err := ParseAndVerify(raw, NetworkMainAlbatross)
	if err == nil {
		t.Fatal("a testnet transaction must not verify against mainnet")
	}
	if !strings.Contains(err.Error(), "expected") {
		t.Errorf("error should name the expected network, got: %v", err)
	}
}

// TestParseAndVerifyRejectsTampering covers the whole point of validating
// before broadcast: a mutated field must invalidate the signature.
func TestParseAndVerifyRejectsTampering(t *testing.T) {
	raw, _, _ := signedRaw(t, NetworkTestAlbatross, 1000, []byte("invoice-42"))

	// The value sits in a known position of the extended layout; flipping any
	// byte of it must break verification.
	tx, err := ParseTransaction(raw)
	if err != nil {
		t.Fatal(err)
	}
	tx.Value = 999999
	tampered, err := tx.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAndVerify(tampered, NetworkTestAlbatross); err == nil {
		t.Fatal("a transaction with an altered value must fail verification")
	}
}

// TestParseAndVerifyRejectsForeignSignature: a validly signed transaction whose
// proof belongs to a different key must not pass, or a backend would accept a
// payment nobody with the funds authorized.
func TestParseAndVerifyRejectsForeignSignature(t *testing.T) {
	raw, _, _ := signedRaw(t, NetworkTestAlbatross, 1000, []byte("x"))
	tx, err := ParseTransaction(raw)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the proof, change who is claimed to be paying.
	tx.Sender = MustParseAddress("NQ34 248H 248H 248H 248H 248H 248H 248H 248H")
	forged, err := tx.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseAndVerify(forged, NetworkTestAlbatross); err == nil {
		t.Fatal("a proof from a key that does not control the sender must be rejected")
	}
}

func TestParseAndVerifyRejectsGarbage(t *testing.T) {
	for _, raw := range [][]byte{nil, {0x00}, {0x01, 0x02, 0x03}} {
		if _, err := ParseAndVerify(raw, NetworkTestAlbatross); err == nil {
			t.Errorf("garbage %x should not verify", raw)
		}
	}
}
