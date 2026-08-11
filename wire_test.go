package nimiq

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestParsePublicKeyEncodings(t *testing.T) {
	s, addr := messageSigner(t)
	signed, err := SignMessage(context.Background(), s, []byte("login challenge"))
	if err != nil {
		t.Fatal(err)
	}
	pub := []byte(signed.PublicKey)

	encodings := map[string]string{
		"std":    base64.StdEncoding.EncodeToString(pub),
		"url":    base64.URLEncoding.EncodeToString(pub),
		"rawurl": base64.RawURLEncoding.EncodeToString(pub),
		"hex":    hex.EncodeToString(pub),
		"0x-hex": "0x" + hex.EncodeToString(pub),
		"spaced": "  " + base64.StdEncoding.EncodeToString(pub) + "\n",
	}
	for name, enc := range encodings {
		got, err := ParsePublicKey(enc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, pub) {
			t.Fatalf("%s: bytes mismatch", name)
		}
		if err := VerifyMessageFrom(addr, got, []byte("login challenge"), signed.Signature); err != nil {
			t.Fatalf("%s: VerifyMessageFrom: %v", name, err)
		}
	}
}

func TestParseSignatureEncodings(t *testing.T) {
	s, addr := messageSigner(t)
	msg := []byte("login challenge")
	signed, err := SignMessage(context.Background(), s, msg)
	if err != nil {
		t.Fatal(err)
	}
	sig := signed.Signature

	encodings := map[string]string{
		"std":    base64.StdEncoding.EncodeToString(sig),
		"url":    base64.URLEncoding.EncodeToString(sig),
		"rawurl": base64.RawURLEncoding.EncodeToString(sig),
		"hex":    hex.EncodeToString(sig),
		"0x-hex": "0X" + hex.EncodeToString(sig),
	}
	for name, enc := range encodings {
		got, err := ParseSignature(enc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, sig) {
			t.Fatalf("%s: bytes mismatch", name)
		}
		if err := VerifyMessageFrom(addr, signed.PublicKey, msg, got); err != nil {
			t.Fatalf("%s: VerifyMessageFrom: %v", name, err)
		}
	}
}

func TestParsePublicKeyRejects(t *testing.T) {
	cases := map[string]string{
		"empty":     "",
		"garbage":   "!!!not-encoding!!!",
		"short-b64": base64.StdEncoding.EncodeToString(make([]byte, 31)),
		"long-b64":  base64.StdEncoding.EncodeToString(make([]byte, 33)),
		"zero-key":  base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"dot-pad":   strings.ReplaceAll(base64.URLEncoding.EncodeToString(make([]byte, 32)), "=", "."),
	}
	for name, in := range cases {
		if _, err := ParsePublicKey(in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseSignatureRejects(t *testing.T) {
	cases := map[string]string{
		"empty":     "",
		"garbage":   "###",
		"short-b64": base64.StdEncoding.EncodeToString(make([]byte, 63)),
		"long-b64":  base64.StdEncoding.EncodeToString(make([]byte, 65)),
	}
	for name, in := range cases {
		if _, err := ParseSignature(in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParsePublicKeyDefensiveCopy(t *testing.T) {
	pub := make([]byte, 32)
	pub[0] = 1
	enc := base64.StdEncoding.EncodeToString(pub)
	got, err := ParsePublicKey(enc)
	if err != nil {
		t.Fatal(err)
	}
	got[0] ^= 0xff
	again, err := ParsePublicKey(enc)
	if err != nil {
		t.Fatal(err)
	}
	if again[0] != 1 {
		t.Fatal("ParsePublicKey must return a defensive copy")
	}
}

// TODO: Hub golden vector — paste a real Hub/Keyguard signMessage result
// (message + address + base64 pubkey + base64 sig) when available.
func TestParseWireHubGoldenVector(t *testing.T) {
	t.Skip("TODO: Hub golden vector")
}
