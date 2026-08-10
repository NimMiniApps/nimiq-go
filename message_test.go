package nimiq

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func messageSigner(t *testing.T) (signerStub, Address) {
	t.Helper()
	seed := mustHex(t, "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	s := signerStub{key: ed25519.NewKeyFromSeed(seed)}
	addr, err := SignerAddress(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	return s, addr
}

// TestMessageHashPreimage pins the exact bytes that get hashed. The prefix, the
// decimal length and the absence of any separator between them are all part of
// the format: a mismatch here makes every signature incompatible with the Hub
// and Keyguard.
func TestMessageHashPreimage(t *testing.T) {
	msg := []byte("hello")
	got := MessageHash(msg)

	// Recomputed independently from the format description rather than by
	// calling the implementation under test.
	want := sha256Of("\x16Nimiq Signed Message:\n" + "5" + "hello")
	if hex.EncodeToString(got[:]) != want {
		t.Errorf("message hash\n got %x\nwant %s", got, want)
	}

	// The length is the byte count, not the rune count.
	multibyte := []byte("héllo") // 6 bytes, 5 runes
	if len(multibyte) != 6 {
		t.Fatal("test fixture assumption broken")
	}
	h := MessageHash(multibyte)
	if hex.EncodeToString(h[:]) != sha256Of("\x16Nimiq Signed Message:\n"+"6"+"héllo") {
		t.Error("message length must be counted in bytes, not runes")
	}
}

// TestMessageDomainSeparation checks a message signature cannot be replayed as
// a transaction signature. This is the entire reason the prefix exists.
func TestMessageDomainSeparation(t *testing.T) {
	s, addr := messageSigner(t)
	ctx := context.Background()

	tx, err := NewBasicTransaction(addr, addr, 1000, 0, 10, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	content := tx.SigningContent()

	// Sign the transaction's signing content as if it were a plain message.
	signed, err := SignMessage(ctx, s, content)
	if err != nil {
		t.Fatal(err)
	}
	// That signature must not authorize the transaction.
	proof, err := NewSignatureProof(signed.PublicKey, signed.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if err := proof.Verify(tx); err == nil {
		t.Fatal("a signed message must never validate as a transaction signature")
	}
}

func TestSignAndVerifyMessage(t *testing.T) {
	s, addr := messageSigner(t)
	ctx := context.Background()
	msg := []byte("login to nimconnect at 2026-08-11")

	signed, err := SignMessage(ctx, s, msg)
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyMessage(signed.PublicKey, msg, signed.Signature) {
		t.Fatal("our own signature failed to verify")
	}
	if err := VerifyMessageFrom(addr, signed.PublicKey, msg, signed.Signature); err != nil {
		t.Fatalf("VerifyMessageFrom: %v", err)
	}
	got, err := signed.Address()
	if err != nil || got != addr {
		t.Errorf("signed.Address() = %v (%v), want %s", got, err, addr)
	}

	// Any tampering must fail.
	if VerifyMessage(signed.PublicKey, []byte("different message"), signed.Signature) {
		t.Error("signature verified against the wrong message")
	}
	bad := append([]byte(nil), signed.Signature...)
	bad[0] ^= 0xff
	if VerifyMessage(signed.PublicKey, msg, bad) {
		t.Error("a corrupted signature verified")
	}
}

// TestVerifyMessageFromRejectsWrongAddress is the authentication case that
// matters: a perfectly valid signature from a key that does not control the
// claimed address must not authenticate.
func TestVerifyMessageFromRejectsWrongAddress(t *testing.T) {
	s, _ := messageSigner(t)
	ctx := context.Background()
	msg := []byte("I am the treasury")

	signed, err := SignMessage(ctx, s, msg)
	if err != nil {
		t.Fatal(err)
	}
	victim := MustParseAddress("NQ34 248H 248H 248H 248H 248H 248H 248H 248H")

	// The signature itself is valid — only the binding to the address fails.
	if !VerifyMessage(signed.PublicKey, msg, signed.Signature) {
		t.Fatal("fixture broken: signature should be valid")
	}
	err = VerifyMessageFrom(victim, signed.PublicKey, msg, signed.Signature)
	if err == nil {
		t.Fatal("a valid signature from an unrelated key must not authenticate as the victim")
	}
	if !strings.Contains(err.Error(), "signed by") {
		t.Errorf("error should name the actual signer, got: %v", err)
	}
}

func TestVerifyMessageRejectsMalformed(t *testing.T) {
	s, _ := messageSigner(t)
	signed, err := SignMessage(context.Background(), s, []byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct {
		pub []byte
		sig []byte
	}{
		"short key":       {pub: signed.PublicKey[:31], sig: signed.Signature},
		"short signature": {pub: signed.PublicKey, sig: signed.Signature[:63]},
		"zero key":        {pub: make([]byte, 32), sig: signed.Signature},
		"nil":             {pub: nil, sig: nil},
	}
	for name, c := range cases {
		if VerifyMessage(c.pub, []byte("x"), c.sig) {
			t.Errorf("%s: verification should have failed", name)
		}
	}
}

// TestEmptyMessage checks the length-zero edge, which the decimal-length
// encoding makes easy to get wrong.
func TestEmptyMessage(t *testing.T) {
	s, addr := messageSigner(t)
	signed, err := SignMessage(context.Background(), s, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyMessageFrom(addr, signed.PublicKey, nil, signed.Signature); err != nil {
		t.Errorf("empty message: %v", err)
	}
	h := MessageHash(nil)
	if hex.EncodeToString(h[:]) != sha256Of("\x16Nimiq Signed Message:\n0") {
		t.Error("empty message must still encode a zero length")
	}
}

func sha256Of(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
