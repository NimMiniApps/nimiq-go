package nimiq

import (
	"context"
	"crypto/ed25519"
	"fmt"
)

// Signer produces Ed25519 signatures for an account.
//
// It signs an opaque message rather than a Transaction so that a remote
// implementation — Vault, a KMS, an HSM, a separate signing service — needs
// only to move 32-byte digests and signatures across its boundary, never
// transaction structure. Sign takes a context so those implementations can
// honour deadlines and cancellation.
//
// Implementations must be safe for concurrent use.
type Signer interface {
	// PublicKey returns the Ed25519 public key that Sign will sign with.
	PublicKey(ctx context.Context) (ed25519.PublicKey, error)
	// Sign returns a detached Ed25519 signature over message.
	Sign(ctx context.Context, message []byte) ([]byte, error)
}

// SignerAddress returns the Nimiq address controlled by a Signer.
func SignerAddress(ctx context.Context, s Signer) (Address, error) {
	pub, err := s.PublicKey(ctx)
	if err != nil {
		return Address{}, err
	}
	return AddressFromPublicKey(pub)
}

// SignTransaction signs tx in place, attaching the signature proof.
//
// It performs no network access: everything needed is already in tx. A signed
// transaction can be serialized and stored, then broadcast later by an entirely
// separate process.
//
// Ed25519 is deterministic, so signing the same transaction twice produces
// identical bytes and an identical hash.
func SignTransaction(ctx context.Context, s Signer, tx *Transaction) error {
	if err := tx.Validate(); err != nil {
		return err
	}
	pub, err := s.PublicKey(ctx)
	if err != nil {
		return fmt.Errorf("nimiq: fetching public key: %w", err)
	}
	// Fail before signing if the key cannot authorize this sender: a valid
	// signature from the wrong key is a silently broken payment.
	if tx.SenderType == AccountBasic {
		addr, err := AddressFromPublicKey(pub)
		if err != nil {
			return err
		}
		if addr != tx.Sender {
			return fmt.Errorf("nimiq: signer controls %s but the transaction sends from %s", addr, tx.Sender)
		}
	}
	sig, err := s.Sign(ctx, tx.SigningContent())
	if err != nil {
		return fmt.Errorf("nimiq: signing: %w", err)
	}
	proof, err := NewSignatureProof(pub, sig)
	if err != nil {
		return err
	}
	tx.Proof = proof.Serialize()
	return nil
}
