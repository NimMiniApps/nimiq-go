// Package signer holds the SDK's private-key-bearing implementations of
// nimiq.Signer.
//
// It is a separate package so that services which only read the chain — balance
// lookups, transaction verification, confirmation tracking — can import
// nimiq and nimiq/rpc without ever linking key-handling code. Keep it that way:
// nothing here should be needed to inspect or verify a transaction.
package signer

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"

	nimiq "github.com/NimMiniApps/nimiq-go"
)

// PrivateKey signs with an in-process Ed25519 key.
//
// The key material lives in ordinary Go memory for the lifetime of the value:
// it can be paged to swap and will appear in a core dump. That is acceptable
// for a hot wallet with a bounded balance and is not acceptable for a treasury.
// For those, implement nimiq.Signer against a KMS, an HSM or a separate signing
// service — no transaction-handling code has to change.
type PrivateKey struct {
	key ed25519.PrivateKey
	// entropy, when set, is the BIP39/legacy entropy that produced this key.
	// Mnemonic() exports it so a FromMnemonic round-trip restores the same key.
	entropy []byte
}

// NewPrivateKey builds a signer from a 32-byte Nimiq private key, which is the
// Ed25519 seed.
func NewPrivateKey(seed []byte) (*PrivateKey, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("nimiq/signer: private key is %d bytes, want %d", len(seed), ed25519.SeedSize)
	}
	allZero := true
	for _, b := range seed {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return nil, fmt.Errorf("nimiq/signer: refusing an all-zero private key")
	}
	return &PrivateKey{key: ed25519.NewKeyFromSeed(seed)}, nil
}

// ParsePrivateKeyHex builds a signer from a hex-encoded 32-byte private key,
// the form Nimiq wallet exports and the importRawKey RPC use.
//
// The error deliberately does not quote the input, so a malformed key cannot
// end up in a log line.
func ParsePrivateKeyHex(s string) (*PrivateKey, error) {
	raw, err := hex.DecodeString(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "0x")))
	if err != nil {
		return nil, fmt.Errorf("nimiq/signer: private key is not valid hex")
	}
	return NewPrivateKey(raw)
}

// Generate creates a new random key using crypto/rand.
func Generate() (*PrivateKey, error) {
	_, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("nimiq/signer: generating key: %w", err)
	}
	return &PrivateKey{key: key}, nil
}

// PublicKey implements nimiq.Signer.
func (p *PrivateKey) PublicKey(ctx context.Context) (ed25519.PublicKey, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return p.key.Public().(ed25519.PublicKey), nil
}

// Sign implements nimiq.Signer. Ed25519 signing is deterministic and needs no
// entropy, so this never fails for a well-formed key.
func (p *PrivateKey) Sign(ctx context.Context, message []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return ed25519.Sign(p.key, message), nil
}

// Address returns the Nimiq address this signer controls.
func (p *PrivateKey) Address() nimiq.Address {
	addr, err := nimiq.AddressFromPublicKey(p.key.Public().(ed25519.PublicKey))
	if err != nil {
		// Unreachable: the key was length-checked at construction.
		panic(err)
	}
	return addr
}

// Seed returns a copy of the 32-byte Ed25519 seed (Nimiq private key).
func (p *PrivateKey) Seed() []byte {
	s := p.key.Seed()
	out := make([]byte, len(s))
	copy(out, s)
	return out
}

// String deliberately does not reveal key material. Without it, embedding a
// PrivateKey in a struct that gets logged with %v would print the raw key.
func (p *PrivateKey) String() string {
	return "nimiq/signer.PrivateKey(" + p.Address().String() + ")"
}

var _ nimiq.Signer = (*PrivateKey)(nil)
