package nimiq

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"strconv"
)

// signedMessagePrefix is the domain separator Nimiq prepends before hashing a
// message for signature.
//
// The leading 0x16 is a length byte for the string that follows, in the style
// of Ethereum's EIP-191. Its purpose is to make a signed message impossible to
// reinterpret as a signed transaction: a wallet asked to sign arbitrary bytes
// can never be tricked into authorizing a payment.
const signedMessagePrefix = "\x16Nimiq Signed Message:\n"

// MessageHash returns the digest that is actually signed for a Nimiq signed
// message: SHA-256 over the prefix, the message length in decimal ASCII, and
// the message itself.
//
// Mirrors WalletAccount::prepare_message_for_signature in core-rs
// (wallet/src/wallet_account.rs). Note this is SHA-256, not the Blake2b used
// for transaction hashing.
func MessageHash(message []byte) [32]byte {
	lengthStr := strconv.Itoa(len(message))
	buf := make([]byte, 0, len(signedMessagePrefix)+len(lengthStr)+len(message))
	buf = append(buf, signedMessagePrefix...)
	buf = append(buf, lengthStr...)
	buf = append(buf, message...)
	return sha256.Sum256(buf)
}

// SignedMessage is a message signature together with the key that produced it.
type SignedMessage struct {
	PublicKey ed25519.PublicKey
	Signature []byte
}

// Address returns the Nimiq address the signing key controls.
func (m *SignedMessage) Address() (Address, error) {
	return AddressFromPublicKey(m.PublicKey)
}

// SignMessage signs an arbitrary message with a Signer.
//
// This is the backend counterpart to the Hub's and Keyguard's signMessage. It
// cannot produce a transaction signature, because the prefix places message
// signatures in a separate domain.
func SignMessage(ctx context.Context, s Signer, message []byte) (*SignedMessage, error) {
	pub, err := s.PublicKey(ctx)
	if err != nil {
		return nil, fmt.Errorf("nimiq: fetching public key: %w", err)
	}
	hash := MessageHash(message)
	sig, err := s.Sign(ctx, hash[:])
	if err != nil {
		return nil, fmt.Errorf("nimiq: signing message: %w", err)
	}
	return &SignedMessage{PublicKey: append(ed25519.PublicKey(nil), pub...), Signature: sig}, nil
}

// VerifyMessage reports whether signature is a valid Nimiq signed-message
// signature over message by publicKey.
//
// This proves only that the key signed the message. It does not establish which
// account the signer controls — for authentication, use VerifyMessageFrom.
func VerifyMessage(publicKey ed25519.PublicKey, message, signature []byte) bool {
	if len(publicKey) != ed25519PublicKeySize || len(signature) != ed25519SignatureSize {
		return false
	}
	if isAllZero(publicKey) {
		return false
	}
	hash := MessageHash(message)
	return ed25519.Verify(publicKey, hash[:], signature)
}

// VerifyMessageFrom checks that message was signed by the holder of address.
//
// This is the primitive that wallet login needs, and the reason it exists as
// one function: verifying the signature alone is not authentication. A caller
// that checks the signature but forgets to bind the public key to the claimed
// address will accept any valid signature from any key, and so will
// authenticate an attacker as any user they choose. Both checks belong
// together.
//
// The address comparison is on the parsed 20-byte form, so display spacing and
// case in a claimed address are irrelevant.
func VerifyMessageFrom(address Address, publicKey ed25519.PublicKey, message, signature []byte) error {
	signerAddr, err := AddressFromPublicKey(publicKey)
	if err != nil {
		return err
	}
	if signerAddr != address {
		return fmt.Errorf("nimiq: message was signed by %s, not by %s", signerAddr, address)
	}
	if !VerifyMessage(publicKey, message, signature) {
		return fmt.Errorf("nimiq: message signature verification failed")
	}
	return nil
}
