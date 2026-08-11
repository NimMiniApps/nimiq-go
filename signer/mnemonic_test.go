package signer

import (
	"strings"
	"testing"
)

// Phrases from core-js MnemonicUtils.spec.js.
const (
	bip39Phrase = "void come effort suffer camp survey warrior heavy shoot primary clutch crush open amazing screen patrol group space point ten exist slush involve unfold"
	legacyPhrase = "refuse walk suggest raven cheese gate eye divert base slot fossil lock oven fuel thank need unit oak image spike vehicle grace citizen expose"
	unknownPhrase = "enforce valid stock pool lonely label awesome olive torch pledge enhance deny you rude inspire ill letter pulse pact light timber enable elbow guide"
	badChecksumPhrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art abandon abandon abandon abandon abandon abandon art"
	// BIP39 24-word vector from trezor/python-mnemonic (empty passphrase).
	trezorPhrase = "abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon abandon art"
)

func TestTypeOf(t *testing.T) {
	cases := []struct {
		phrase string
		want   MnemonicType
	}{
		{bip39Phrase, MnemonicBIP39},
		{legacyPhrase, MnemonicLegacy},
		{unknownPhrase, MnemonicUnknown},
	}
	for _, tc := range cases {
		got, err := TypeOf(tc.phrase)
		if err != nil {
			t.Errorf("TypeOf(%q): %v", tc.phrase, err)
			continue
		}
		if got != tc.want {
			t.Errorf("TypeOf(%q) = %v, want %v", tc.phrase, got, tc.want)
		}
	}
	if _, err := TypeOf(badChecksumPhrase); err == nil {
		t.Fatal("TypeOf(bad checksum): want error")
	}
}

func TestFromMnemonicRejectsUnknownAndLegacy(t *testing.T) {
	if _, err := FromMnemonic(unknownPhrase, ""); err == nil {
		t.Fatal("FromMnemonic(unknown): want error")
	}
	if _, err := FromMnemonic(legacyPhrase, ""); err == nil {
		t.Fatal("FromMnemonic(legacy): want error")
	}
}

func TestFromMnemonicBIP39Addresses(t *testing.T) {
	// Golden addresses: BIP39 seed → SLIP-0010 m/44'/242'/0'/0' → Nimiq address.
	// First phrase is a Nimiq core-js fixture; second is the trezor all-abandon vector.
	cases := []struct {
		phrase, password, address string
	}{
		{bip39Phrase, "", "NQ74 GRAX 32JR NTDG VKH4 JF1H 9NAD MSGY Y0DX"},
		{trezorPhrase, "", "NQ82 FFX8 BC3Q 1VMC 1D2L MK3Q T1UE 2EPV KGNN"},
		{trezorPhrase, "TREZOR", "NQ75 BJLD 00GL LYG6 UYDJ U11J LH1D 4RXA 16FC"},
	}
	for _, tc := range cases {
		k, err := FromMnemonic(tc.phrase, tc.password)
		if err != nil {
			t.Fatalf("FromMnemonic(%q, %q): %v", tc.phrase, tc.password, err)
		}
		got := k.Address().String()
		if got != tc.address {
			t.Errorf("address = %s, want %s (password=%q)", got, tc.address, tc.password)
		}
	}
}

func TestFromMnemonicTypeLegacy(t *testing.T) {
	// Prefer foil… is a 24-word legacy vector from MnemonicUtils.spec.js;
	// entropy bytes are the private key.
	phrase := "prefer foil call dove gym shop style blur used chuckle rain destroy person leader affair pretty weekend resemble ostrich ugly token glass nature visa"
	k, err := FromMnemonicType(phrase, "", MnemonicLegacy)
	if err != nil {
		t.Fatal(err)
	}
	// entropy from the fixture
	wantSeed := []byte{
		169, 203, 76, 129, 160, 230, 129, 141, 117, 240, 195, 239, 197, 18, 196, 30,
		26, 52, 253, 1, 21, 81, 249, 22, 234, 115, 246, 14, 62, 197, 228, 223,
	}
	got := k.key.Seed()
	if len(got) != len(wantSeed) {
		t.Fatalf("seed len = %d, want %d", len(got), len(wantSeed))
	}
	for i := range wantSeed {
		if got[i] != wantSeed[i] {
			t.Fatalf("seed[%d] = %d, want %d", i, got[i], wantSeed[i])
		}
	}
}

func TestMnemonicRoundTrip(t *testing.T) {
	k, err := FromMnemonic(bip39Phrase, "")
	if err != nil {
		t.Fatal(err)
	}
	words, err := k.Mnemonic()
	if err != nil {
		t.Fatal(err)
	}
	if words != bip39Phrase {
		t.Fatalf("Mnemonic() = %q, want original phrase", words)
	}
	k2, err := FromMnemonic(words, "")
	if err != nil {
		t.Fatal(err)
	}
	if k.Address() != k2.Address() {
		t.Fatalf("round-trip address %s != %s", k2.Address(), k.Address())
	}
}

func TestMnemonicExportFromRawKey(t *testing.T) {
	// A raw key's mnemonic encodes its seed; restoring requires the BIP39
	// derivation path, so this is export-only unless the key was mnemonic-backed.
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	k, err := NewPrivateKey(seed)
	if err != nil {
		t.Fatal(err)
	}
	words, err := k.Mnemonic()
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Fields(words)); n != 24 {
		t.Fatalf("word count = %d, want 24", n)
	}
}
