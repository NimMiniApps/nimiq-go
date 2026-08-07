package nimiq

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/blake2b"
)

// AddressLen is the length of a Nimiq address in bytes.
const AddressLen = 20

// base32Alphabet is Nimiq's base32 alphabet. It omits I, O, W and Z to avoid
// visual ambiguity, so encoding/base32 with a custom alphabet is used rather
// than the standard one.
const base32Alphabet = "0123456789ABCDEFGHJKLMNPQRSTUVXY"

// Address is a Nimiq account address: the first 20 bytes of the Blake2b-256
// hash of an Ed25519 public key.
//
// The zero Address is valid on the wire (it is the burn address) but is not a
// spendable account; callers deciding whether an address was actually supplied
// should compare against the zero value explicitly.
type Address [AddressLen]byte

// AddressFromPublicKey derives the address of an Ed25519 public key.
func AddressFromPublicKey(pub ed25519.PublicKey) (Address, error) {
	var a Address
	if len(pub) != ed25519.PublicKeySize {
		return a, fmt.Errorf("nimiq: public key is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	h := blake2b.Sum256(pub)
	copy(a[:], h[:AddressLen])
	return a, nil
}

// AddressFromBytes builds an Address from its 20-byte binary form.
func AddressFromBytes(b []byte) (Address, error) {
	var a Address
	if len(b) != AddressLen {
		return a, fmt.Errorf("nimiq: address is %d bytes, want %d", len(b), AddressLen)
	}
	copy(a[:], b)
	return a, nil
}

// ParseAddress parses a user-friendly Nimiq address ("NQ.. .... ....") and
// verifies its checksum. Spacing and case are ignored.
//
// This is the inverse of Address.String, and is what makes offline transaction
// construction possible: a recipient supplied as text becomes the 20 raw bytes
// the wire format requires.
func ParseAddress(s string) (Address, error) {
	var a Address
	compact := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), " ", ""))
	if len(compact) != 36 {
		return a, fmt.Errorf("nimiq: address %q has %d characters, want 36", s, len(compact))
	}
	if !strings.HasPrefix(compact, "NQ") {
		return a, fmt.Errorf("nimiq: address %q does not start with NQ", s)
	}
	for _, c := range compact[2:] {
		if !strings.ContainsRune(base32Alphabet, c) {
			return a, fmt.Errorf("nimiq: address %q contains invalid character %q", s, c)
		}
	}
	// ISO 7064 MOD 97-10, computed over the payload followed by the "NQ" +
	// check digits, exactly as IBAN does. A valid address leaves remainder 1.
	if rem := ibanMod97(compact[4:] + compact[:4]); rem != 1 {
		return a, fmt.Errorf("nimiq: address %q has an invalid checksum", s)
	}
	raw, err := base32Decode(compact[4:])
	if err != nil {
		return a, err
	}
	copy(a[:], raw)
	return a, nil
}

// MustParseAddress is ParseAddress for compile-time constants and tests. It
// panics on invalid input and must never be used on runtime-supplied data.
func MustParseAddress(s string) Address {
	a, err := ParseAddress(s)
	if err != nil {
		panic(err)
	}
	return a
}

// String returns the user-friendly form, in groups of four separated by
// spaces: "NQ21 XG6E MX3E ...".
func (a Address) String() string {
	payload := base32Encode(a[:])
	check := 98 - ibanMod97(payload+"NQ00")
	compact := fmt.Sprintf("NQ%02d%s", check, payload)
	var b strings.Builder
	b.Grow(44)
	for i := 0; i < len(compact); i += 4 {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(compact[i : i+4])
	}
	return b.String()
}

// Compact returns the user-friendly form without separating spaces. Use it for
// storage and comparison keys; use String for display.
func (a Address) Compact() string {
	return strings.ReplaceAll(a.String(), " ", "")
}

// Bytes returns a copy of the 20-byte binary form.
func (a Address) Bytes() []byte {
	out := make([]byte, AddressLen)
	copy(out, a[:])
	return out
}

// Equal reports whether two addresses are identical. Address is comparable, so
// == works too; this exists for readability at call sites.
func (a Address) Equal(other Address) bool { return a == other }

// ibanMod97 computes ISO 7064 MOD 97-10 one character at a time so it cannot
// overflow, regardless of input length. Letters expand to two digits (A=10 …
// Z=35), which must be fed through the modulus separately.
func ibanMod97(s string) int {
	rem := 0
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9':
			rem = (rem*10 + int(c-'0')) % 97
		case c >= 'A' && c <= 'Z':
			n := int(c-'A') + 10
			rem = (rem*10 + n/10) % 97
			rem = (rem*10 + n%10) % 97
		default:
			// Unreachable for validated input; treated as a checksum failure
			// rather than silently skipped.
			return -1
		}
	}
	return rem
}

// base32Encode encodes exactly 20 bytes as 32 characters of Nimiq's base32.
// 20 bytes is 160 bits, an exact multiple of 5, so there is no padding.
func base32Encode(b []byte) string {
	out := make([]byte, 0, 32)
	var acc, bits uint32
	for _, c := range b {
		acc = acc<<8 | uint32(c)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, base32Alphabet[(acc>>bits)&0x1f])
		}
	}
	return string(out)
}

var errBase32Char = errors.New("nimiq: invalid base32 character")

// base32Decode decodes 32 characters of Nimiq base32 back to 20 bytes.
func base32Decode(s string) ([]byte, error) {
	out := make([]byte, 0, AddressLen)
	var acc, bits uint32
	for _, c := range s {
		idx := strings.IndexRune(base32Alphabet, c)
		if idx < 0 {
			return nil, errBase32Char
		}
		acc = acc<<5 | uint32(idx)
		bits += 5
		if bits >= 8 {
			bits -= 8
			out = append(out, byte((acc>>bits)&0xff))
		}
	}
	if len(out) != AddressLen {
		return nil, fmt.Errorf("nimiq: decoded %d bytes, want %d", len(out), AddressLen)
	}
	return out, nil
}
