package nimiq

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
)

const (
	ed25519PublicKeySize = 32
	ed25519SignatureSize = 64
)

// SignatureAlgorithm identifies which signature scheme a proof uses.
type SignatureAlgorithm uint8

const (
	// AlgorithmEd25519 is the scheme used by ordinary Nimiq keys, and the only
	// one this SDK can produce.
	AlgorithmEd25519 SignatureAlgorithm = 0
	// AlgorithmES256 is used by passkey/WebAuthn wallets. The SDK can decode
	// and inspect such proofs but cannot create or verify them yet.
	AlgorithmES256 SignatureAlgorithm = 1
)

// signatureProofFlagWebauthn marks the presence of trailing WebAuthn fields.
const signatureProofFlagWebauthn = 0b0001

// SignatureProof authorizes a transaction.
//
// The wire layout is a single type-and-flags byte, then the public key, then
// the Merkle path, then the signature. For an ordinary single-key account the
// Merkle path is empty and the whole proof is 98 bytes.
type SignatureProof struct {
	Algorithm SignatureAlgorithm
	PublicKey []byte
	// MerklePath supports multi-signature accounts. It is empty for ordinary
	// accounts, and must be empty for the basic transaction format.
	MerklePath []byte
	Signature  []byte
}

// NewSignatureProof builds the proof for an ordinary single-key account.
func NewSignatureProof(pub ed25519.PublicKey, signature []byte) (*SignatureProof, error) {
	if len(pub) != ed25519PublicKeySize {
		return nil, fmt.Errorf("nimiq: public key is %d bytes, want %d", len(pub), ed25519PublicKeySize)
	}
	if len(signature) != ed25519SignatureSize {
		return nil, fmt.Errorf("nimiq: signature is %d bytes, want %d", len(signature), ed25519SignatureSize)
	}
	// core-rs rejects the all-zero public key because it acts as a
	// verification wildcard (staking_contract/structs.rs). Reject it here for
	// every account type, not just staking.
	if isAllZero(pub) {
		return nil, fmt.Errorf("nimiq: refusing to build a proof for the all-zero public key")
	}
	return &SignatureProof{
		Algorithm: AlgorithmEd25519,
		PublicKey: append([]byte(nil), pub...),
		Signature: append([]byte(nil), signature...),
	}, nil
}

// typeAndFlagsByte packs the algorithm into the low four bits and the flags
// into the high four.
func (p *SignatureProof) typeAndFlagsByte() byte {
	return byte(p.Algorithm) & 0x0f
}

// setTypeAndFlags decodes the algorithm and flag nibbles.
//
// Unknown flag bits are rejected rather than ignored. Ignoring them would let a
// proof decode, lose the bits on re-serialization, and be rebroadcast as
// something with different meaning from what arrived.
func (p *SignatureProof) setTypeAndFlags(b byte) error {
	switch SignatureAlgorithm(b & 0x0f) {
	case AlgorithmEd25519:
		p.Algorithm = AlgorithmEd25519
	case AlgorithmES256:
		p.Algorithm = AlgorithmES256
	default:
		return fmt.Errorf("nimiq: unknown signature algorithm %d", b&0x0f)
	}
	flags := b >> 4
	if flags&signatureProofFlagWebauthn != 0 {
		return fmt.Errorf("nimiq: WebAuthn signature proofs are not supported yet")
	}
	if flags != 0 {
		return fmt.Errorf("nimiq: unknown signature proof flags 0x%x", flags)
	}
	return nil
}

// Serialize returns the proof's wire bytes.
func (p *SignatureProof) Serialize() []byte {
	e := &encoder{buf: make([]byte, 0, 98)}
	e.u8(p.typeAndFlagsByte())
	e.raw(p.PublicKey)
	if len(p.MerklePath) == 0 {
		// An empty Merkle path is a uvarint node count of zero.
		e.u8(0)
	} else {
		e.raw(p.MerklePath)
	}
	e.raw(p.Signature)
	return e.buf
}

// ParseSignatureProof decodes a signature proof.
//
// Only the ordinary empty-Merkle-path shape is supported; multi-signature
// proofs are reported as unsupported rather than mis-parsed, because guessing
// at a length here would silently produce a wrong public key or signature.
func ParseSignatureProof(raw []byte) (*SignatureProof, error) {
	d := &decoder{buf: raw}
	p := &SignatureProof{}
	flags, err := d.u8()
	if err != nil {
		return nil, err
	}
	if err := p.setTypeAndFlags(flags); err != nil {
		return nil, err
	}
	if p.Algorithm != AlgorithmEd25519 {
		return nil, fmt.Errorf("nimiq: signature proof uses ES256/WebAuthn, which this SDK cannot yet verify")
	}
	if p.PublicKey, err = d.raw(ed25519PublicKeySize); err != nil {
		return nil, err
	}
	pathLen, err := d.u8()
	if err != nil {
		return nil, err
	}
	if pathLen != 0 {
		return nil, fmt.Errorf("nimiq: multi-signature proofs (%d Merkle nodes) are not supported", pathLen)
	}
	if p.Signature, err = d.raw(ed25519SignatureSize); err != nil {
		return nil, err
	}
	if err := d.end(); err != nil {
		return nil, err
	}
	return p, nil
}

// Verify checks the proof against a transaction's signing content and confirms
// the signing key actually controls the sender address.
//
// The address check matters: a valid signature from the wrong key would
// otherwise pass. Use this on any transaction that arrived from outside the
// process, such as one signed by the Nimiq Hub.
func (p *SignatureProof) Verify(tx *Transaction) error {
	if p.Algorithm != AlgorithmEd25519 {
		return fmt.Errorf("nimiq: cannot verify a non-Ed25519 signature proof")
	}
	if len(p.PublicKey) != ed25519PublicKeySize || len(p.Signature) != ed25519SignatureSize {
		return fmt.Errorf("nimiq: malformed signature proof")
	}
	if isAllZero(p.PublicKey) {
		return fmt.Errorf("nimiq: the all-zero public key is not a valid signer")
	}
	addr, err := AddressFromPublicKey(p.PublicKey)
	if err != nil {
		return err
	}
	// For staking and contract senders the signer is not required to be the
	// sender address, so only basic senders are checked here.
	if tx.SenderType == AccountBasic && addr != tx.Sender {
		return fmt.Errorf("nimiq: signature is from %s but the sender is %s", addr, tx.Sender)
	}
	if !ed25519.Verify(ed25519.PublicKey(p.PublicKey), tx.SigningContent(), p.Signature) {
		return fmt.Errorf("nimiq: signature verification failed")
	}
	return nil
}

func isAllZero(b []byte) bool {
	return len(b) > 0 && bytes.Equal(b, make([]byte, len(b)))
}
