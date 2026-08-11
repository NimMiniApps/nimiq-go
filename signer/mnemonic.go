package signer

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/crypto/pbkdf2"
)

// MnemonicType classifies a recovery phrase's checksum scheme.
type MnemonicType int

const (
	// MnemonicUnknown means the phrase validates as both BIP39 and legacy.
	// Callers must not guess which branch to derive.
	MnemonicUnknown MnemonicType = -1
	// MnemonicLegacy is the deprecated CRC-8 checksum scheme.
	MnemonicLegacy MnemonicType = 0
	// MnemonicBIP39 is the standard SHA-256 checksum scheme.
	MnemonicBIP39 MnemonicType = 1
)

// Nimiq account-0 derivation path (SLIP-0010 ed25519, all hardened).
const defaultDerivationPath = "m/44'/242'/0'/0'"

// TypeOf reports which checksum scheme phrase uses.
func TypeOf(phrase string) (MnemonicType, error) {
	words, err := splitMnemonic(phrase)
	if err != nil {
		return 0, err
	}
	bits, err := mnemonicToBits(words)
	if err != nil {
		return 0, err
	}
	_, bip39Err := bitsToEntropy(bits, false)
	_, legacyErr := bitsToEntropy(bits, true)
	switch {
	case bip39Err == nil && legacyErr == nil:
		return MnemonicUnknown, nil
	case bip39Err == nil:
		return MnemonicBIP39, nil
	case legacyErr == nil:
		return MnemonicLegacy, nil
	default:
		return 0, fmt.Errorf("nimiq/signer: invalid mnemonic checksum")
	}
}

// FromMnemonic derives the first account key from a BIP39 phrase.
//
// Ambiguous (MnemonicUnknown) and legacy phrases are rejected: deriving the
// wrong branch silently hands the user an empty wallet. Use FromMnemonicType
// for an explicit legacy import.
func FromMnemonic(phrase, password string) (*PrivateKey, error) {
	kind, err := TypeOf(phrase)
	if err != nil {
		return nil, err
	}
	switch kind {
	case MnemonicUnknown:
		return nil, fmt.Errorf("nimiq/signer: mnemonic checksum is ambiguous; pass an explicit MnemonicType")
	case MnemonicLegacy:
		return nil, fmt.Errorf("nimiq/signer: mnemonic is legacy; use FromMnemonicType with MnemonicLegacy")
	}
	return FromMnemonicType(phrase, password, MnemonicBIP39)
}

// FromMnemonicType derives a key using an explicit checksum scheme.
func FromMnemonicType(phrase, password string, kind MnemonicType) (*PrivateKey, error) {
	words, err := splitMnemonic(phrase)
	if err != nil {
		return nil, err
	}
	actual, err := TypeOf(strings.Join(words, " "))
	if err != nil {
		return nil, err
	}
	switch kind {
	case MnemonicBIP39:
		if actual == MnemonicLegacy {
			return nil, fmt.Errorf("nimiq/signer: mnemonic is legacy; use FromMnemonicType with MnemonicLegacy")
		}
		// MnemonicUnknown is allowed with an explicit BIP39 request: both
		// checksums match, and the caller picked the branch.
		entropy, err := mnemonicToEntropy(words, false)
		if err != nil {
			return nil, err
		}
		seed := mnemonicToSeed(strings.Join(words, " "), password)
		key, err := derivePath(seed, defaultDerivationPath)
		if err != nil {
			return nil, err
		}
		key.entropy = entropy
		return key, nil
	case MnemonicLegacy:
		if actual == MnemonicBIP39 {
			return nil, fmt.Errorf("nimiq/signer: mnemonic is BIP39; use FromMnemonic")
		}
		entropy, err := mnemonicToEntropy(words, true)
		if err != nil {
			return nil, err
		}
		if len(entropy) != 32 {
			return nil, fmt.Errorf("nimiq/signer: legacy mnemonic entropy is %d bytes, want 32", len(entropy))
		}
		key, err := NewPrivateKey(entropy)
		if err != nil {
			return nil, err
		}
		key.entropy = append([]byte(nil), entropy...)
		return key, nil
	case MnemonicUnknown:
		return nil, fmt.Errorf("nimiq/signer: refuse to derive an ambiguous mnemonic")
	default:
		return nil, fmt.Errorf("nimiq/signer: unknown mnemonic type %d", kind)
	}
}

// Mnemonic returns a 24-word BIP39 encoding of this key's entropy.
//
// Keys created with FromMnemonic export their original entropy. Keys created
// with NewPrivateKey or Generate encode the Ed25519 seed.
func (p *PrivateKey) Mnemonic() (string, error) {
	entropy := p.entropy
	if entropy == nil {
		entropy = p.key.Seed()
	}
	if len(entropy) != 32 {
		return "", fmt.Errorf("nimiq/signer: entropy is %d bytes, want 32 for a 24-word mnemonic", len(entropy))
	}
	words, err := entropyToMnemonic(entropy)
	if err != nil {
		return "", err
	}
	return strings.Join(words, " "), nil
}

func splitMnemonic(phrase string) ([]string, error) {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(phrase)))
	if len(fields) < 12 {
		return nil, fmt.Errorf("nimiq/signer: mnemonic has %d words, want at least 12", len(fields))
	}
	if len(fields) > 24 {
		return nil, fmt.Errorf("nimiq/signer: mnemonic has %d words, want at most 24", len(fields))
	}
	if len(fields)%3 != 0 {
		return nil, fmt.Errorf("nimiq/signer: mnemonic word count %d is not a multiple of 3", len(fields))
	}
	return fields, nil
}

func mnemonicToEntropy(words []string, legacy bool) ([]byte, error) {
	bits, err := mnemonicToBits(words)
	if err != nil {
		return nil, err
	}
	return bitsToEntropy(bits, legacy)
}

func mnemonicToBits(words []string) (string, error) {
	var b strings.Builder
	b.Grow(len(words) * 11)
	for _, w := range words {
		idx, ok := wordIndex[w]
		if !ok {
			return "", fmt.Errorf("nimiq/signer: %q is not in the BIP39 English wordlist", w)
		}
		for i := 10; i >= 0; i-- {
			if idx&(1<<uint(i)) != 0 {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
	}
	return b.String(), nil
}

func bitsToEntropy(bits string, legacy bool) ([]byte, error) {
	csLen := len(bits) % 8
	if csLen == 0 {
		csLen = 8
	}
	divider := len(bits) - csLen
	entropyBits := bits[:divider]
	checksumBits := bits[divider:]

	if len(entropyBits)%8 != 0 {
		return nil, fmt.Errorf("nimiq/signer: invalid mnemonic bit length")
	}
	entropy := make([]byte, len(entropyBits)/8)
	for i := range entropy {
		var v byte
		for j := 0; j < 8; j++ {
			v <<= 1
			if entropyBits[i*8+j] == '1' {
				v |= 1
			}
		}
		entropy[i] = v
	}
	if len(entropy) < 16 || len(entropy) > 32 || len(entropy)%4 != 0 {
		return nil, fmt.Errorf("nimiq/signer: invalid entropy length %d", len(entropy))
	}
	want := checksumBitsOf(entropy, legacy)
	if want != checksumBits {
		return nil, fmt.Errorf("nimiq/signer: invalid mnemonic checksum")
	}
	return entropy, nil
}

func entropyToMnemonic(entropy []byte) ([]string, error) {
	if len(entropy) < 16 || len(entropy) > 32 || len(entropy)%4 != 0 {
		return nil, fmt.Errorf("nimiq/signer: invalid entropy length %d", len(entropy))
	}
	bits := entropyToBits(entropy) + checksumBitsOf(entropy, false)
	if len(bits)%11 != 0 {
		return nil, fmt.Errorf("nimiq/signer: internal bit length error")
	}
	words := make([]string, 0, len(bits)/11)
	for i := 0; i < len(bits); i += 11 {
		var idx int
		for j := 0; j < 11; j++ {
			idx <<= 1
			if bits[i+j] == '1' {
				idx |= 1
			}
		}
		words = append(words, wordlist[idx])
	}
	return words, nil
}

func entropyToBits(entropy []byte) string {
	var b strings.Builder
	b.Grow(len(entropy) * 8)
	for _, v := range entropy {
		for i := 7; i >= 0; i-- {
			if v&(1<<uint(i)) != 0 {
				b.WriteByte('1')
			} else {
				b.WriteByte('0')
			}
		}
	}
	return b.String()
}

func checksumBitsOf(entropy []byte, legacy bool) string {
	cs := len(entropy) * 8 / 32
	var hashByte byte
	if legacy {
		hashByte = crc8(entropy)
	} else {
		sum := sha256.Sum256(entropy)
		hashByte = sum[0]
	}
	var b strings.Builder
	for i := 7; i >= 8-cs; i-- {
		if hashByte&(1<<uint(i)) != 0 {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String()
}

// crc8 matches Nimiq core-js CRC8 (Baicheva98 polynomial 0x97).
func crc8(buf []byte) byte {
	var c byte
	for _, v := range buf {
		c = crc8Table[c^v]
	}
	return c
}

var crc8Table = func() [256]byte {
	var table [256]byte
	for i := 0; i < 256; i++ {
		curr := byte(i)
		for j := 0; j < 8; j++ {
			if curr&0x80 != 0 {
				curr = (curr << 1) ^ 0x97
			} else {
				curr <<= 1
			}
		}
		table[i] = curr
	}
	return table
}()

func mnemonicToSeed(mnemonic, password string) []byte {
	// BIP39: NFKD(mnemonic) / NFKD("mnemonic"+password). English wordlists are
	// ASCII so NFKD is a no-op; strip other space to a single join already done.
	salt := "mnemonic" + password
	return pbkdf2.Key([]byte(mnemonic), []byte(salt), 2048, 64, sha512.New)
}

type extKey struct {
	key       []byte // 32-byte private key seed
	chainCode []byte // 32 bytes
}

func masterKey(seed []byte) extKey {
	mac := hmac.New(sha512.New, []byte("ed25519 seed"))
	_, _ = mac.Write(seed)
	sum := mac.Sum(nil)
	return extKey{key: sum[:32], chainCode: sum[32:]}
}

func (e extKey) deriveHardened(index uint32) extKey {
	index |= 0x80000000
	data := make([]byte, 1+32+4)
	copy(data[1:], e.key)
	binary.BigEndian.PutUint32(data[1+32:], index)
	mac := hmac.New(sha512.New, e.chainCode)
	_, _ = mac.Write(data)
	sum := mac.Sum(nil)
	return extKey{key: sum[:32], chainCode: sum[32:]}
}

func derivePath(seed []byte, path string) (*PrivateKey, error) {
	if !strings.HasPrefix(path, "m/") && path != "m" {
		return nil, fmt.Errorf("nimiq/signer: invalid derivation path %q", path)
	}
	ext := masterKey(seed)
	if path == "m" {
		return NewPrivateKey(ext.key)
	}
	for _, seg := range strings.Split(path[2:], "/") {
		if seg == "" || !strings.HasSuffix(seg, "'") {
			return nil, fmt.Errorf("nimiq/signer: only hardened derivation is supported (got %q)", seg)
		}
		n := strings.TrimSuffix(seg, "'")
		var idx uint32
		for _, r := range n {
			if !unicode.IsDigit(r) {
				return nil, fmt.Errorf("nimiq/signer: invalid path segment %q", seg)
			}
			idx = idx*10 + uint32(r-'0')
		}
		ext = ext.deriveHardened(idx)
	}
	return NewPrivateKey(ext.key)
}
