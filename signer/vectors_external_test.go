package signer

import (
	"encoding/hex"
	"testing"
)

// External vectors: these pin the two halves of mnemonic derivation to
// published test data rather than to this implementation's own output.

func TestSeedAgainstTrezorVector(t *testing.T) {
	const phrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
	const want = "bda85446c68413707090a52022edd26a1c9462295029f2e60cd7c4f2bbd3097170af7a4d73245cafa9c3cca8d561a7c3de6f5d4a10be8ed2a5e608d68f92fcc8"
	if got := hex.EncodeToString(mnemonicToSeed(phrase, "TREZOR")); got != want {
		t.Fatalf("seed = %s, want %s", got, want)
	}
}

func TestSLIP0010Vector1(t *testing.T) {
	seed, _ := hex.DecodeString("000102030405060708090a0b0c0d0e0f")

	m := masterKey(seed)
	if got := hex.EncodeToString(m.key); got != "2b4be7f19ee27bbf30c667b642d5f4aa69fd169872f8fc3059c08ebae2eb19e7" {
		t.Errorf("master key = %s", got)
	}
	if got := hex.EncodeToString(m.chainCode); got != "90046a93de5380a72b5e45010748567d5ea02bbf6522f979e05c0d8d8ca9fffb" {
		t.Errorf("master chain code = %s", got)
	}

	// m/0'/1'/2'/2'/1000000000'
	ext := m
	for _, i := range []uint32{0, 1, 2, 2, 1000000000} {
		ext = ext.deriveHardened(i)
	}
	if got := hex.EncodeToString(ext.key); got != "8f94d394a8e8fd6b1bc2f3f49f5c47e385281d5c17e65324b0f62483e37e8793" {
		t.Errorf("derived key = %s", got)
	}
	if got := hex.EncodeToString(ext.chainCode); got != "68789923a0cac2cd5a29172a475fe9e0fb14cd6adb5ad98a3fa70333e7afa230" {
		t.Errorf("derived chain code = %s", got)
	}
}
