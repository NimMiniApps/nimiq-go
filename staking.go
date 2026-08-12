package nimiq

import (
	"context"
	"fmt"
)

// StakingContractAddress is the fixed address of the staking contract:
// NQ77 0000 0000 0000 0000 0000 0000 0000 0001.
var StakingContractAddress = Address{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1,
}

// Incoming staking data variant tags. These are postcard enum variant indices,
// so their numeric values are fixed by the field order of
// IncomingStakingTransactionData in core-rs and must not be reordered.
//
// Variants 0 and 1 (CreateValidator, UpdateValidator) are omitted: they carry
// BLS voting keys and a BLS proof of knowledge, which this SDK cannot produce.
type stakingKind uint8

const (
	kindCreateValidator     stakingKind = 0 // needs BLS — unsupported
	kindUpdateValidator     stakingKind = 1 // needs BLS — unsupported
	kindDeactivateValidator stakingKind = 2
	kindReactivateValidator stakingKind = 3
	kindRetireValidator     stakingKind = 4
	kindCreateStaker        stakingKind = 5
	kindAddStake            stakingKind = 6
	kindUpdateStaker        stakingKind = 7
	kindSetActiveStake      stakingKind = 8
	kindRetireStake         stakingKind = 9
)

// Outgoing staking data variant tags, carried in SenderData.
const (
	kindDeleteValidator uint8 = 0
	kindRemoveStake     uint8 = 1
)

// emptyProofLen is the size of a default (all-zero) Ed25519 signature proof:
// one algorithm byte, a 32-byte key, an empty Merkle path and a 64-byte
// signature.
const emptyProofLen = 1 + 32 + 1 + 64

// isSignaling reports whether a variant must be sent with the SIGNALING flag
// and zero value.
func (k stakingKind) isSignaling() bool {
	switch k {
	case kindUpdateValidator, kindDeactivateValidator, kindReactivateValidator,
		kindRetireValidator, kindUpdateStaker, kindSetActiveStake, kindRetireStake:
		return true
	}
	return false
}

// hasInnerProof reports whether a variant embeds a signature proof in its data.
// AddStake is the only supported variant that does not: anyone may add stake to
// an existing staker, so no authorization is required.
func (k stakingKind) hasInnerProof() bool { return k != kindAddStake }

// NewStakingTransaction builds an incoming staking transaction from a
// pre-encoded data payload. Prefer the named constructors below.
//
// The returned transaction is unsigned and its embedded proof is zeroed. Use
// SignStakingTransaction, not SignTransaction: staking transactions need two
// signatures applied in a specific order.
func newStakingTransaction(kind stakingKind, payload []byte, sender Address, value, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	e := &encoder{buf: make([]byte, 0, 64)}
	e.uvarint(uint64(kind))
	e.raw(payload)
	if kind.hasInnerProof() {
		// Placeholder for the signature the staker will supply. The signing
		// content is computed with this field zeroed.
		e.raw(make([]byte, emptyProofLen))
	}

	tx := &Transaction{
		Sender:              sender,
		SenderType:          AccountBasic,
		Recipient:           StakingContractAddress,
		RecipientType:       AccountStaking,
		RecipientData:       e.buf,
		Value:               value,
		Fee:                 fee,
		ValidityStartHeight: validityStartHeight,
		Network:             network,
	}
	if kind.isSignaling() {
		tx.Flags = FlagSignaling
	}
	if err := tx.validateStaking(kind); err != nil {
		return nil, err
	}
	return tx, nil
}

// validateStaking applies the staking-specific rules that core-rs enforces at
// verification time, so a malformed transaction fails locally rather than at
// broadcast.
func (t *Transaction) validateStaking(kind stakingKind) error {
	if !t.Network.IsAlbatross() {
		return fmt.Errorf("nimiq: refusing to build a staking transaction for %s", t.Network)
	}
	if kind == kindCreateValidator || kind == kindUpdateValidator {
		return fmt.Errorf("nimiq: %s requires a BLS voting key, which this SDK cannot produce", kind)
	}
	if kind.isSignaling() && t.Value != 0 {
		return fmt.Errorf("nimiq: %s is a signaling transaction and must have zero value", kind)
	}
	if !kind.isSignaling() && t.Value == 0 {
		return fmt.Errorf("nimiq: %s must have a non-zero value", kind)
	}
	if _, err := t.Value.Add(t.Fee); err != nil {
		return err
	}
	return nil
}

func (k stakingKind) String() string {
	switch k {
	case kindCreateValidator:
		return "CreateValidator"
	case kindUpdateValidator:
		return "UpdateValidator"
	case kindDeactivateValidator:
		return "DeactivateValidator"
	case kindReactivateValidator:
		return "ReactivateValidator"
	case kindRetireValidator:
		return "RetireValidator"
	case kindCreateStaker:
		return "CreateStaker"
	case kindAddStake:
		return "AddStake"
	case kindUpdateStaker:
		return "UpdateStaker"
	case kindSetActiveStake:
		return "SetActiveStake"
	case kindRetireStake:
		return "RetireStake"
	}
	return fmt.Sprintf("stakingKind(%d)", uint8(k))
}

// encodeOptionalAddress writes a postcard Option<Address>: a zero byte for
// none, or a one byte followed by the 20 address bytes.
func encodeOptionalAddress(e *encoder, addr *Address) {
	if addr == nil {
		e.u8(0)
		return
	}
	e.u8(1)
	e.raw(addr[:])
}

// NewCreateStakerTransaction creates a staker and delegates its stake.
//
// delegation may be nil to stake without choosing a validator. value is the
// initial stake and must be at least the network's minimum.
func NewCreateStakerTransaction(sender Address, delegation *Address, value, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	e := &encoder{}
	encodeOptionalAddress(e, delegation)
	return newStakingTransaction(kindCreateStaker, e.buf, sender, value, fee, validityStartHeight, network)
}

// NewAddStakeTransaction adds stake to an existing staker.
//
// It carries no inner proof: anyone may add stake to any staker, so only the
// sender's own signature is required.
func NewAddStakeTransaction(sender, staker Address, value, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	e := &encoder{}
	e.raw(staker[:])
	return newStakingTransaction(kindAddStake, e.buf, sender, value, fee, validityStartHeight, network)
}

// NewUpdateStakerTransaction changes a staker's delegation.
//
// reactivateAllStake moves any inactive stake back to active in the same
// operation. This is a signaling transaction and carries no value.
func NewUpdateStakerTransaction(sender Address, newDelegation *Address, reactivateAllStake bool, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	e := &encoder{}
	encodeOptionalAddress(e, newDelegation)
	if reactivateAllStake {
		e.u8(1)
	} else {
		e.u8(0)
	}
	return newStakingTransaction(kindUpdateStaker, e.buf, sender, 0, fee, validityStartHeight, network)
}

// NewSetActiveStakeTransaction sets how much of a staker's balance is active.
//
// Lowering the active balance is the first step of unstaking. This is a
// signaling transaction and carries no value.
func NewSetActiveStakeTransaction(sender Address, newActiveBalance, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	e := &encoder{}
	e.u64be(uint64(newActiveBalance))
	return newStakingTransaction(kindSetActiveStake, e.buf, sender, 0, fee, validityStartHeight, network)
}

// NewRetireStakeTransaction moves inactive stake into the retired state, from
// which it can later be removed with NewRemoveStakeTransaction.
//
// This is a signaling transaction and carries no value.
func NewRetireStakeTransaction(sender Address, retireStake, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	e := &encoder{}
	e.u64be(uint64(retireStake))
	return newStakingTransaction(kindRetireStake, e.buf, sender, 0, fee, validityStartHeight, network)
}

// NewRemoveStakeTransaction withdraws retired stake back to a normal account.
//
// This is an outgoing staking transaction: the staking contract is the sender,
// the variant tag travels in SenderData, and the only signature required is the
// staker's, carried in the transaction proof. Sign it with SignTransaction, not
// SignStakingTransaction.
func NewRemoveStakeTransaction(recipient Address, value, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	if value == 0 {
		return nil, fmt.Errorf("nimiq: RemoveStake must have a non-zero value")
	}
	tx := &Transaction{
		Sender:              StakingContractAddress,
		SenderType:          AccountStaking,
		SenderData:          []byte{kindRemoveStake},
		Recipient:           recipient,
		RecipientType:       AccountBasic,
		Value:               value,
		Fee:                 fee,
		ValidityStartHeight: validityStartHeight,
		Network:             network,
	}
	if !tx.Network.IsAlbatross() {
		return nil, fmt.Errorf("nimiq: refusing to build a staking transaction for %s", tx.Network)
	}
	return tx, nil
}

// NewRetireValidatorTransaction retires an existing validator, removing it
// from the active set. This is authorized by the validator's own account key
// — sign with SignStakingTransaction(ctx, tx, validatorSigner, validatorSigner),
// the same signer for both the inner and outer proof, exactly like
// NewRetireStakeTransaction.
//
// This is a signaling transaction and carries no value.
func NewRetireValidatorTransaction(sender Address, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	return newStakingTransaction(kindRetireValidator, nil, sender, 0, fee, validityStartHeight, network)
}

// NewDeleteValidatorTransaction deletes a retired validator and returns its
// deposit to recipient. This is an outgoing staking transaction: the staking
// contract is the sender, the variant tag travels in SenderData, and the
// only signature required is the validator's, carried in the transaction
// proof. Sign it with SignTransaction, not SignStakingTransaction — same
// shape as NewRemoveStakeTransaction.
func NewDeleteValidatorTransaction(recipient Address, value, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	if value == 0 {
		return nil, fmt.Errorf("nimiq: DeleteValidator must have a non-zero value")
	}
	tx := &Transaction{
		Sender:              StakingContractAddress,
		SenderType:          AccountStaking,
		SenderData:          []byte{kindDeleteValidator},
		Recipient:           recipient,
		RecipientType:       AccountBasic,
		Value:               value,
		Fee:                 fee,
		ValidityStartHeight: validityStartHeight,
		Network:             network,
	}
	if !tx.Network.IsAlbatross() {
		return nil, fmt.Errorf("nimiq: refusing to build a staking transaction for %s", tx.Network)
	}
	return tx, nil
}

// NewDeactivateValidatorTransaction deactivates an active validator.
//
// Unlike every other staking builder in this file, sender need not be the
// account being acted on: this variant is authorized by the validator's
// separate signing key, not by whoever pays the fee, so validatorAddress
// travels explicitly in the payload. Sign with
// SignStakingTransaction(ctx, tx, signingKeySigner, feePayerSigner) — the
// signing-key signer fills the role SignStakingTransaction calls "staker"
// (the inner-proof signer), the fee payer fills "sender" (the outer-proof
// signer). They are commonly, but not necessarily, the same party.
func NewDeactivateValidatorTransaction(sender, validatorAddress Address, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	e := &encoder{}
	e.raw(validatorAddress[:])
	return newStakingTransaction(kindDeactivateValidator, e.buf, sender, 0, fee, validityStartHeight, network)
}

// NewReactivateValidatorTransaction reactivates a deactivated (but not
// jailed or retired) validator. Same authorization model as
// NewDeactivateValidatorTransaction — see its doc comment.
func NewReactivateValidatorTransaction(sender, validatorAddress Address, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error) {
	e := &encoder{}
	e.raw(validatorAddress[:])
	return newStakingTransaction(kindReactivateValidator, e.buf, sender, 0, fee, validityStartHeight, network)
}

// SignStakingTransaction signs an incoming staking transaction.
//
// Incoming staking transactions carry two signatures. The inner one authorizes
// the staking action and is embedded in the transaction's data; the outer one
// authorizes the sending account and lives in the transaction proof. They are
// usually the same key, in which case pass the same signer twice.
//
// The "staker" and "sender" parameter names describe the usual case — most
// callers use the same signer for both, or the staking account's own key for
// "staker". For NewDeactivateValidatorTransaction and
// NewReactivateValidatorTransaction, pass the validator's separate signing
// key as "staker" and whoever is paying the fee as "sender": those two may be
// different accounts entirely.
//
// Order matters and is not interchangeable: the inner signature is taken over
// the content with the embedded proof still zeroed, and the outer signature is
// then taken over the content *after* the real inner proof has been written in.
// Signing in the other order produces a transaction that serializes fine and is
// rejected by the network.
func SignStakingTransaction(ctx context.Context, tx *Transaction, staker, sender Signer) error {
	if tx.RecipientType != AccountStaking {
		return fmt.Errorf("nimiq: not an incoming staking transaction")
	}
	if len(tx.RecipientData) == 0 {
		return fmt.Errorf("nimiq: staking transaction has no data payload")
	}
	kind := stakingKind(tx.RecipientData[0])

	if kind.hasInnerProof() {
		if len(tx.RecipientData) < emptyProofLen {
			return fmt.Errorf("nimiq: staking data is too short to contain a proof")
		}
		// The embedded proof is the final field of every supported variant, so
		// it occupies the trailing bytes of the payload.
		offset := len(tx.RecipientData) - emptyProofLen

		stakerPub, err := staker.PublicKey(ctx)
		if err != nil {
			return fmt.Errorf("nimiq: fetching staker public key: %w", err)
		}
		innerSig, err := staker.Sign(ctx, tx.SigningContent())
		if err != nil {
			return fmt.Errorf("nimiq: signing staking authorization: %w", err)
		}
		innerProof, err := NewSignatureProof(stakerPub, innerSig)
		if err != nil {
			return err
		}
		copy(tx.RecipientData[offset:], innerProof.Serialize())
	}

	// Recomputed deliberately: the content changed when the inner proof was
	// written above.
	senderPub, err := sender.PublicKey(ctx)
	if err != nil {
		return fmt.Errorf("nimiq: fetching sender public key: %w", err)
	}
	if addr, err := AddressFromPublicKey(senderPub); err != nil {
		return err
	} else if addr != tx.Sender {
		return fmt.Errorf("nimiq: sender signer controls %s but the transaction sends from %s", addr, tx.Sender)
	}
	outerSig, err := sender.Sign(ctx, tx.SigningContent())
	if err != nil {
		return fmt.Errorf("nimiq: signing transaction: %w", err)
	}
	outerProof, err := NewSignatureProof(senderPub, outerSig)
	if err != nil {
		return err
	}
	tx.Proof = outerProof.Serialize()
	return nil
}
