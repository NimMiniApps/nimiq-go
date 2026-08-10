package nimiq

import "fmt"

// HashAlgorithm identifies the hash function an HTLC locks its funds behind.
type HashAlgorithm uint8

const (
	HashBlake2b HashAlgorithm = 0
	HashSha256  HashAlgorithm = 1
	HashSha512  HashAlgorithm = 2
)

func (h HashAlgorithm) String() string {
	switch h {
	case HashBlake2b:
		return "blake2b"
	case HashSha256:
		return "sha256"
	case HashSha512:
		return "sha512"
	}
	return fmt.Sprintf("unknown(%d)", uint8(h))
}

// HTLCCreation is the payload of a hashed timelock contract creation.
//
// The SDK parses these but does not create them. Parsing is what applications
// actually need: Nimiq Pay routes payments through HTLCs and rotates them
// periodically, so resolving which wallet is behind a contract means reading
// the creation payload rather than trusting the funding transaction's sender —
// a rotated contract is funded by its predecessor, not by the user.
type HTLCCreation struct {
	// Sender may reclaim the funds after Timeout.
	Sender Address
	// Recipient may claim the funds before Timeout by revealing a pre-image.
	Recipient Address
	// Algorithm and HashRoot describe the lock. HashRoot is 32 bytes for
	// blake2b and sha256, 64 for sha512.
	Algorithm HashAlgorithm
	HashRoot  []byte
	// HashCount is how many times the pre-image must be hashed to reach
	// HashRoot. At least 1; higher values allow partial claims.
	HashCount uint8
	// Timeout is a millisecond UNIX timestamp, not a block height.
	Timeout uint64
}

// ParseHTLCCreation decodes the recipient data of an HTLC creation transaction.
//
// Mirrors CreationTransactionData in
// primitives/transaction/src/account/htlc_contract.rs.
func ParseHTLCCreation(data []byte) (*HTLCCreation, error) {
	d := &decoder{buf: data}
	h := &HTLCCreation{}

	sender, err := d.raw(AddressLen)
	if err != nil {
		return nil, fmt.Errorf("nimiq: HTLC sender: %w", err)
	}
	h.Sender, _ = AddressFromBytes(sender)

	recipient, err := d.raw(AddressLen)
	if err != nil {
		return nil, fmt.Errorf("nimiq: HTLC recipient: %w", err)
	}
	h.Recipient, _ = AddressFromBytes(recipient)

	// AnyHash is a postcard enum: variant index then a fixed-size digest.
	algo, err := d.uvarint()
	if err != nil {
		return nil, fmt.Errorf("nimiq: HTLC hash algorithm: %w", err)
	}
	var rootLen int
	switch HashAlgorithm(algo) {
	case HashBlake2b, HashSha256:
		rootLen = 32
	case HashSha512:
		rootLen = 64
	default:
		return nil, fmt.Errorf("nimiq: unknown HTLC hash algorithm %d", algo)
	}
	h.Algorithm = HashAlgorithm(algo)
	if h.HashRoot, err = d.raw(rootLen); err != nil {
		return nil, fmt.Errorf("nimiq: HTLC hash root: %w", err)
	}

	if h.HashCount, err = d.u8(); err != nil {
		return nil, fmt.Errorf("nimiq: HTLC hash count: %w", err)
	}
	// core-rs requires at least one hash iteration; zero would mean the root is
	// the pre-image itself, which is not a lock at all.
	if h.HashCount == 0 {
		return nil, fmt.Errorf("nimiq: HTLC hash count must be at least 1")
	}

	if h.Timeout, err = d.u64be(); err != nil {
		return nil, fmt.Errorf("nimiq: HTLC timeout: %w", err)
	}
	if err := d.end(); err != nil {
		return nil, fmt.Errorf("nimiq: HTLC creation data: %w", err)
	}
	return h, nil
}

// HTLCOwner returns the address that funded an HTLC and may reclaim it.
//
// This is the address to attribute a rotating Nimiq Pay contract to. Do not
// use the funding transaction's sender for this: when Pay rotates a contract
// it funds the new one from the old one, so the sender is the previous
// contract rather than the user.
func (h *HTLCCreation) HTLCOwner() Address { return h.Sender }
