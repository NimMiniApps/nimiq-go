package nimiq

import (
	"fmt"

	"golang.org/x/crypto/blake2b"
)

// AccountType identifies the kind of account a transaction leaves or enters.
type AccountType uint8

const (
	AccountBasic   AccountType = 0
	AccountVesting AccountType = 1
	AccountHTLC    AccountType = 2
	AccountStaking AccountType = 3
)

// TransactionFlags is the transaction flag bitfield.
type TransactionFlags uint8

const (
	FlagContractCreation TransactionFlags = 0b01
	FlagSignaling        TransactionFlags = 0b10
)

// Hash is a Blake2b-256 digest.
type Hash [32]byte

func (h Hash) String() string {
	const hexDigits = "0123456789abcdef"
	out := make([]byte, 64)
	for i, b := range h {
		out[i*2] = hexDigits[b>>4]
		out[i*2+1] = hexDigits[b&0x0f]
	}
	return string(out)
}

// Transaction is an Albatross transaction.
//
// Albatross carries two independent data fields. RecipientData is what
// proof-of-work called simply "data"; SenderData is new in Albatross and
// carries outgoing staking data. Code ported from a proof-of-work library will
// model only one and be wrong.
//
// A Transaction is a plain value with no network dependency: it can be built,
// serialized, signed and hashed with no RPC endpoint configured.
type Transaction struct {
	Sender        Address
	SenderType    AccountType
	SenderData    []byte
	Recipient     Address
	RecipientType AccountType
	RecipientData []byte
	Value         Luna
	Fee           Luna
	// ValidityStartHeight bounds when the transaction may be included. It is
	// part of the signing content, so changing it changes the hash — see
	// Transaction.Hash.
	ValidityStartHeight uint32
	Network             NetworkID
	Flags               TransactionFlags
	// Proof is the signature proof. It is empty on an unsigned transaction and
	// is deliberately excluded from both the signing content and the hash.
	Proof []byte
}

// NewBasicTransaction builds a plain value transfer between two basic accounts.
//
// It performs no network access. The caller supplies validityStartHeight,
// typically from rpc.Client.BlockNumber; keeping it an explicit argument means
// transaction construction never needs an endpoint.
func NewBasicTransaction(sender, recipient Address, value, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	tx := &Transaction{
		Sender:              sender,
		SenderType:          AccountBasic,
		Recipient:           recipient,
		RecipientType:       AccountBasic,
		Value:               value,
		Fee:                 fee,
		ValidityStartHeight: validityStartHeight,
		Network:             network,
	}
	if err := tx.Validate(); err != nil {
		return nil, err
	}
	return tx, nil
}

// WithData returns a copy of tx carrying an extra-data payload on the
// recipient side. Adding data moves the transaction to the extended wire
// format; see Transaction.Format.
func (t *Transaction) WithData(data []byte) *Transaction {
	cp := *t
	cp.RecipientData = append([]byte(nil), data...)
	return &cp
}

// Validate checks the invariants the SDK can verify offline. It cannot check
// balance or whether the validity window is still open — those need chain
// state.
func (t *Transaction) Validate() error {
	if !t.Network.IsAlbatross() {
		return fmt.Errorf("nimiq: refusing to build a transaction for %s: not an Albatross network", t.Network)
	}
	if t.Value > MaxLuna || t.Fee > MaxLuna {
		return fmt.Errorf("nimiq: value or fee exceeds the maximum representable amount")
	}
	if _, err := t.Value.Add(t.Fee); err != nil {
		return fmt.Errorf("nimiq: value plus fee overflows: %w", err)
	}
	// core-rs rejects zero-value basic transactions
	// (primitives/transaction/src/account/basic_account.rs).
	if t.RecipientType == AccountBasic && t.Value == 0 {
		return fmt.Errorf("nimiq: a basic transaction must have a non-zero value")
	}
	if t.SenderType == AccountBasic && len(t.SenderData) != 0 {
		return fmt.Errorf("nimiq: a basic sender must not carry sender data")
	}
	// Signaling transactions must carry no value, and staking never accepts
	// contract creation (primitives/transaction/src/account/staking_contract).
	if t.Flags&FlagSignaling != 0 && t.Value != 0 {
		return fmt.Errorf("nimiq: a signaling transaction must have zero value")
	}
	if t.RecipientType == AccountStaking && t.Flags&FlagContractCreation != 0 {
		return fmt.Errorf("nimiq: the staking contract does not accept contract-creation transactions")
	}
	return nil
}

// SigningContent returns the bytes that are signed, and whose Blake2b-256
// digest is the transaction hash.
//
// This is NOT the wire format, and the difference is easy to miss: here the
// recipient-data length is a 2-byte big-endian integer (kept for
// proof-of-work compatibility) whereas the wire format uses a uvarint, and the
// data field comes first, before the sender. Mirrors SerializeContent for
// Transaction in primitives/transaction/src/lib.rs.
func (t *Transaction) SigningContent() []byte {
	e := &encoder{buf: make([]byte, 0, 128)}
	e.u16be(uint16(len(t.RecipientData)))
	e.raw(t.RecipientData)
	e.raw(t.Sender[:])
	e.u8(byte(t.SenderType))
	e.raw(t.Recipient[:])
	e.u8(byte(t.RecipientType))
	e.u64be(uint64(t.Value))
	e.u64be(uint64(t.Fee))
	e.u32be(t.ValidityStartHeight)
	e.u8(byte(t.Network))
	e.u8(byte(t.Flags))
	// Sender data is appended only on Albatross networks, matching core-rs.
	if t.Network.IsAlbatross() {
		e.bytes(t.SenderData)
	}
	return e.buf
}

// Hash returns the transaction hash: Blake2b-256 of the signing content.
//
// Because the signing content excludes the proof, the hash is fully determined
// before the transaction is signed — and therefore before it is broadcast.
// Rebroadcasting identical signed bytes is idempotent at the mempool, so a
// failed broadcast can be retried safely PROVIDED the same transaction value is
// reused. Rebuilding a transaction with a fresh ValidityStartHeight produces a
// different hash and a genuinely different payment.
func (t *Transaction) Hash() Hash {
	return Hash(blake2b.Sum256(t.SigningContent()))
}

// TransactionFormat is the wire encoding a transaction uses.
type TransactionFormat uint8

const (
	// FormatBasic is the compact encoding, which omits the sender because it
	// is recoverable from the public key in the proof.
	FormatBasic TransactionFormat = 0
	// FormatExtended is the general encoding.
	FormatExtended TransactionFormat = 1
)

// Format reports which wire encoding this transaction uses.
//
// The format is derived from the transaction's contents, never chosen by the
// caller — mirroring Transaction::format in core-rs. In particular any
// transaction carrying recipient data is extended, which covers most
// application traffic.
func (t *Transaction) Format() TransactionFormat {
	if t.SenderType != AccountBasic || t.RecipientType != AccountBasic {
		return FormatExtended
	}
	if len(t.RecipientData) != 0 || t.Flags != 0 || len(t.SenderData) != 0 {
		return FormatExtended
	}
	proof, err := ParseSignatureProof(t.Proof)
	if err != nil || len(proof.MerklePath) != 0 {
		return FormatExtended
	}
	// The basic format drops the sender, so it is only usable when the sender
	// is exactly the address of the signing key.
	addr, err := AddressFromPublicKey(proof.PublicKey)
	if err != nil || addr != t.Sender {
		return FormatExtended
	}
	return FormatBasic
}

// Serialize returns the wire bytes accepted by the sendRawTransaction RPC
// method, hex-encoded by the caller.
//
// The transaction must already carry a proof; serializing an unsigned
// transaction is a programming error and reports one rather than emitting
// bytes no node would accept.
func (t *Transaction) Serialize() ([]byte, error) {
	if len(t.Proof) == 0 {
		return nil, fmt.Errorf("nimiq: cannot serialize an unsigned transaction")
	}
	e := &encoder{buf: make([]byte, 0, 200)}
	switch t.Format() {
	case FormatBasic:
		proof, err := ParseSignatureProof(t.Proof)
		if err != nil {
			return nil, err
		}
		e.u8(byte(FormatBasic))
		e.u8(proof.typeAndFlagsByte())
		e.raw(proof.PublicKey)
		e.raw(t.Recipient[:])
		e.u64be(uint64(t.Value))
		e.u64be(uint64(t.Fee))
		e.u32be(t.ValidityStartHeight)
		e.u8(byte(t.Network))
		e.raw(proof.Signature)
	default:
		e.u8(byte(FormatExtended))
		e.raw(t.Sender[:])
		e.u8(byte(t.SenderType))
		e.bytes(t.SenderData)
		e.raw(t.Recipient[:])
		e.u8(byte(t.RecipientType))
		e.bytes(t.RecipientData)
		e.u64be(uint64(t.Value))
		e.u64be(uint64(t.Fee))
		e.u32be(t.ValidityStartHeight)
		e.u8(byte(t.Network))
		e.u8(byte(t.Flags))
		e.bytes(t.Proof)
	}
	return e.buf, nil
}

// SerializedSize returns the transaction's size in wire bytes.
//
// An unsigned transaction is measured as if it carried a standard
// single-signature proof, so a fee can be estimated before signing. For a
// signed transaction the result equals len of what Serialize returns.
func (t *Transaction) SerializedSize() int {
	proofLen := len(t.Proof)
	if proofLen == 0 {
		proofLen = emptyProofLen
	}
	if t.Format() == FormatBasic {
		// variant, type-and-flags, key, recipient, value, fee, height, network,
		// signature — the sender and Merkle path are not transmitted.
		return 1 + 1 + 32 + AddressLen + 8 + 8 + 4 + 1 + 64
	}
	return 1 + AddressLen + 1 + uvarintLen(len(t.SenderData)) + len(t.SenderData) +
		AddressLen + 1 + uvarintLen(len(t.RecipientData)) + len(t.RecipientData) +
		8 + 8 + 4 + 1 + 1 + uvarintLen(proofLen) + proofLen
}

// ParseTransaction decodes wire bytes back into a Transaction, accepting both
// formats.
//
// This is what lets a backend inspect a transaction signed elsewhere — by the
// Nimiq Hub in a browser, for instance — and check its sender, recipient, value
// and hash BEFORE broadcasting it, rather than broadcasting blind and reading
// the result back off the chain.
func ParseTransaction(raw []byte) (*Transaction, error) {
	d := &decoder{buf: raw}
	format, err := d.u8()
	if err != nil {
		return nil, err
	}
	t := &Transaction{}
	switch TransactionFormat(format) {
	case FormatBasic:
		proof := &SignatureProof{}
		flagsByte, err := d.u8()
		if err != nil {
			return nil, err
		}
		if err := proof.setTypeAndFlags(flagsByte); err != nil {
			return nil, err
		}
		// ES256 keys and signatures are sized differently from Ed25519 ones, so
		// reading on would silently produce a wrong public key rather than an
		// error.
		if proof.Algorithm != AlgorithmEd25519 {
			return nil, fmt.Errorf("nimiq: ES256/WebAuthn transactions are not supported yet")
		}
		if proof.PublicKey, err = d.raw(ed25519PublicKeySize); err != nil {
			return nil, err
		}
		recipient, err := d.raw(AddressLen)
		if err != nil {
			return nil, err
		}
		t.Recipient, _ = AddressFromBytes(recipient)
		value, err := d.u64be()
		if err != nil {
			return nil, err
		}
		fee, err := d.u64be()
		if err != nil {
			return nil, err
		}
		t.Value, t.Fee = Luna(value), Luna(fee)
		if t.ValidityStartHeight, err = d.u32be(); err != nil {
			return nil, err
		}
		network, err := d.u8()
		if err != nil {
			return nil, err
		}
		t.Network = NetworkID(network)
		if proof.Signature, err = d.raw(ed25519SignatureSize); err != nil {
			return nil, err
		}
		if err := d.end(); err != nil {
			return nil, err
		}
		// The basic format omits the sender because it is exactly the address
		// of the signing key.
		if t.Sender, err = AddressFromPublicKey(proof.PublicKey); err != nil {
			return nil, err
		}
		t.Proof = proof.Serialize()
		return t, nil

	case FormatExtended:
		sender, err := d.raw(AddressLen)
		if err != nil {
			return nil, err
		}
		t.Sender, _ = AddressFromBytes(sender)
		senderType, err := d.u8()
		if err != nil {
			return nil, err
		}
		t.SenderType = AccountType(senderType)
		if t.SenderData, err = d.bytes(); err != nil {
			return nil, err
		}
		recipient, err := d.raw(AddressLen)
		if err != nil {
			return nil, err
		}
		t.Recipient, _ = AddressFromBytes(recipient)
		recipientType, err := d.u8()
		if err != nil {
			return nil, err
		}
		t.RecipientType = AccountType(recipientType)
		if t.RecipientData, err = d.bytes(); err != nil {
			return nil, err
		}
		value, err := d.u64be()
		if err != nil {
			return nil, err
		}
		fee, err := d.u64be()
		if err != nil {
			return nil, err
		}
		t.Value, t.Fee = Luna(value), Luna(fee)
		if t.ValidityStartHeight, err = d.u32be(); err != nil {
			return nil, err
		}
		network, err := d.u8()
		if err != nil {
			return nil, err
		}
		t.Network = NetworkID(network)
		flags, err := d.u8()
		if err != nil {
			return nil, err
		}
		t.Flags = TransactionFlags(flags)
		if t.Proof, err = d.bytes(); err != nil {
			return nil, err
		}
		// A transaction arriving on the wire with no proof is unsigned. Accept
		// it and a caller inspecting sender, recipient and value would see a
		// plausible transaction that nothing authorized.
		if len(t.Proof) == 0 {
			return nil, fmt.Errorf("nimiq: transaction carries no signature proof")
		}
		if err := d.end(); err != nil {
			return nil, err
		}
		return t, nil
	}
	return nil, fmt.Errorf("nimiq: unknown transaction format %d", format)
}
