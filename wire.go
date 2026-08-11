package nimiq

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// ParsePublicKey decodes an Ed25519 public key from Hub/Keyguard wire forms:
// standard base64, padded or raw base64url, or hex with an optional 0x prefix.
//
// The key must be exactly 32 bytes. The all-zero public key is rejected (same
// policy as SignatureProof). Cashlink-style '.' padding is not accepted.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := decodeWireBytes(s, ed25519PublicKeySize)
	if err != nil {
		return nil, fmt.Errorf("nimiq: public key: %w", err)
	}
	if isAllZero(raw) {
		return nil, fmt.Errorf("nimiq: the all-zero public key is not a valid signer")
	}
	return ed25519.PublicKey(raw), nil
}

// ParseSignature decodes a 64-byte Ed25519 signature from the same encodings
// as ParsePublicKey.
func ParseSignature(s string) ([]byte, error) {
	raw, err := decodeWireBytes(s, ed25519SignatureSize)
	if err != nil {
		return nil, fmt.Errorf("nimiq: signature: %w", err)
	}
	return raw, nil
}

// decodeWireBytes tries Hub signMessage encodings and returns a defensive copy
// of the first decode whose length equals want.
func decodeWireBytes(s string, want int) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty")
	}
	try := []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
		decodeWireHex,
	}
	var lastErr error
	sawWrongLen := false
	for _, dec := range try {
		raw, err := dec(s)
		if err != nil {
			lastErr = err
			continue
		}
		if len(raw) != want {
			sawWrongLen = true
			continue
		}
		out := make([]byte, len(raw))
		copy(out, raw)
		return out, nil
	}
	if sawWrongLen {
		return nil, fmt.Errorf("decoded length is not %d bytes", want)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("invalid encoding: %w", lastErr)
	}
	return nil, fmt.Errorf("invalid encoding")
}

func decodeWireHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if s == "" {
		return nil, fmt.Errorf("empty hex")
	}
	return hex.DecodeString(s)
}
