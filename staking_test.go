package nimiq

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
)

// The layouts asserted here were validated on test-albatross: every variant
// below was accepted by a real mempool, which verifies both the structure and
// the embedded signature. See docs/phase5-staking.md.

func stakingSigner(t *testing.T) signerStub {
	t.Helper()
	seed, err := hex.DecodeString("da1e0d572888e03632b22c5f64b28bd53bab40806f67482a5824fe18538e9d5d")
	if err != nil {
		t.Fatal(err)
	}
	return signerStub{key: ed25519.NewKeyFromSeed(seed)}
}

// TestCreateStakerOnChainFixture reproduces the exact payload of a CreateStaker
// transaction that executed successfully on test-albatross.
func TestCreateStakerOnChainFixture(t *testing.T) {
	const wantData = "0501" + // variant 5 (CreateStaker), Option::Some
		"13cccf0e0bfa5670138512faa22faca8b6075334" + // delegated validator
		"00c9ed9a725d7740ca90012a03f474cf674ad151158ffeebeb0941983562458b94" +
		"0057023356534f477de87d7fb03217af16e13db9e8738bb2c369d2f848bdd1aa8d" +
		"354cfa47aa6327263844a7f78c45d6cfde91b18b49a71f00d37e9bfee6f81400"

	s := stakingSigner(t)
	ctx := context.Background()
	sender, err := SignerAddress(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	validator := MustParseAddress("NQ57 2F6C X3GB Y9B7 04U5 2BVA 4BVC M2T0 ELRL")

	tx, err := NewCreateStakerTransaction(sender, &validator, MustParseNIM("1000"), 0, 8126411, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	if err := SignStakingTransaction(ctx, tx, s, s); err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(tx.RecipientData); got != wantData {
		t.Errorf("recipient data\n got %s\nwant %s", got, wantData)
	}
	if len(tx.RecipientData) != 120 {
		t.Errorf("data = %d bytes, want 120 (node-reported)", len(tx.RecipientData))
	}
	if tx.RecipientType != AccountStaking {
		t.Error("recipient type must be Staking")
	}
	if tx.Recipient != StakingContractAddress {
		t.Error("recipient must be the staking contract")
	}
}

func TestStakingContractAddress(t *testing.T) {
	const want = "NQ77 0000 0000 0000 0000 0000 0000 0000 0001"
	if got := StakingContractAddress.String(); got != want {
		t.Errorf("staking contract address\n got %q\nwant %q", got, want)
	}
}

// TestSignalingVariants locks the flag and value rules. core-rs rejects a
// signaling transaction carrying value, and rejects a non-signaling staking
// transaction whose signaling flag is set.
func TestSignalingVariants(t *testing.T) {
	s := stakingSigner(t)
	ctx := context.Background()
	me, _ := SignerAddress(ctx, s)
	other := MustParseAddress("NQ57 2F6C X3GB Y9B7 04U5 2BVA 4BVC M2T0 ELRL")

	signaling := map[string]*Transaction{}
	valued := map[string]*Transaction{}

	tx, err := NewUpdateStakerTransaction(me, &other, true, 0, 100, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	signaling["UpdateStaker"] = tx

	if tx, err = NewSetActiveStakeTransaction(me, MustParseNIM("10"), 0, 100, NetworkTestAlbatross); err != nil {
		t.Fatal(err)
	}
	signaling["SetActiveStake"] = tx

	if tx, err = NewRetireStakeTransaction(me, MustParseNIM("10"), 0, 100, NetworkTestAlbatross); err != nil {
		t.Fatal(err)
	}
	signaling["RetireStake"] = tx

	if tx, err = NewCreateStakerTransaction(me, &other, MustParseNIM("100"), 0, 100, NetworkTestAlbatross); err != nil {
		t.Fatal(err)
	}
	valued["CreateStaker"] = tx

	if tx, err = NewAddStakeTransaction(me, me, MustParseNIM("100"), 0, 100, NetworkTestAlbatross); err != nil {
		t.Fatal(err)
	}
	valued["AddStake"] = tx

	for name, tx := range signaling {
		if tx.Flags&FlagSignaling == 0 {
			t.Errorf("%s must set the signaling flag", name)
		}
		if tx.Value != 0 {
			t.Errorf("%s must have zero value, got %d", name, tx.Value)
		}
	}
	for name, tx := range valued {
		if tx.Flags&FlagSignaling != 0 {
			t.Errorf("%s must not set the signaling flag", name)
		}
		if tx.Value == 0 {
			t.Errorf("%s must carry a value", name)
		}
	}
}

// TestAddStakeHasNoInnerProof documents that AddStake is the one supported
// variant carrying no embedded authorization: anyone may add stake to any
// staker.
func TestAddStakeHasNoInnerProof(t *testing.T) {
	s := stakingSigner(t)
	ctx := context.Background()
	me, _ := SignerAddress(ctx, s)

	tx, err := NewAddStakeTransaction(me, me, MustParseNIM("500"), 0, 100, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	// variant byte + 20-byte staker address, and nothing else.
	if len(tx.RecipientData) != 21 {
		t.Errorf("AddStake data = %d bytes, want 21", len(tx.RecipientData))
	}
	if tx.RecipientData[0] != byte(kindAddStake) {
		t.Errorf("variant = %d, want %d", tx.RecipientData[0], kindAddStake)
	}
	before := hex.EncodeToString(tx.RecipientData)
	if err := SignStakingTransaction(ctx, tx, s, s); err != nil {
		t.Fatal(err)
	}
	if after := hex.EncodeToString(tx.RecipientData); after != before {
		t.Error("signing must not alter AddStake data; it embeds no proof")
	}
	if len(tx.Proof) == 0 {
		t.Error("the sending account's proof is still required")
	}
}

// TestInnerProofChangesHash guards the ordering rule: the outer signature is
// taken over content that already contains the inner proof. If the inner proof
// were written after the outer signature, the transaction would serialize fine
// and be rejected by the network.
func TestInnerProofChangesHash(t *testing.T) {
	s := stakingSigner(t)
	ctx := context.Background()
	me, _ := SignerAddress(ctx, s)
	other := MustParseAddress("NQ57 2F6C X3GB Y9B7 04U5 2BVA 4BVC M2T0 ELRL")

	tx, err := NewCreateStakerTransaction(me, &other, MustParseNIM("100"), 0, 100, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	unsignedHash := tx.Hash()
	if err := SignStakingTransaction(ctx, tx, s, s); err != nil {
		t.Fatal(err)
	}
	if tx.Hash() == unsignedHash {
		t.Error("writing the inner proof must change the signing content, and so the hash")
	}
	// The outer proof must verify against the final content.
	proof, err := ParseSignatureProof(tx.Proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := proof.Verify(tx); err != nil {
		t.Errorf("outer proof does not verify against the final content: %v", err)
	}
}

// TestStakingDeterministic checks a staking transaction signs to identical
// bytes twice, so a broadcast retry cannot become a second distinct operation.
func TestStakingDeterministic(t *testing.T) {
	s := stakingSigner(t)
	ctx := context.Background()
	me, _ := SignerAddress(ctx, s)
	other := MustParseAddress("NQ57 2F6C X3GB Y9B7 04U5 2BVA 4BVC M2T0 ELRL")

	build := func() string {
		tx, err := NewCreateStakerTransaction(me, &other, MustParseNIM("100"), 0, 100, NetworkTestAlbatross)
		if err != nil {
			t.Fatal(err)
		}
		if err := SignStakingTransaction(ctx, tx, s, s); err != nil {
			t.Fatal(err)
		}
		raw, err := tx.Serialize()
		if err != nil {
			t.Fatal(err)
		}
		return hex.EncodeToString(raw)
	}
	if a, b := build(), build(); a != b {
		t.Error("staking serialization is not deterministic")
	}
}

func TestRemoveStakeIsOutgoing(t *testing.T) {
	me := MustParseAddress("NQ57 2F6C X3GB Y9B7 04U5 2BVA 4BVC M2T0 ELRL")
	tx, err := NewRemoveStakeTransaction(me, MustParseNIM("500"), 0, 100, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Sender != StakingContractAddress || tx.SenderType != AccountStaking {
		t.Error("RemoveStake must be sent from the staking contract")
	}
	if len(tx.SenderData) != 1 || tx.SenderData[0] != kindRemoveStake {
		t.Errorf("sender data = %x, want the RemoveStake variant tag", tx.SenderData)
	}
	if tx.Format() != FormatExtended {
		t.Error("an outgoing staking transaction must use the extended format")
	}
}

// TestRetireValidatorShape checks the RetireValidator payload is exactly the
// variant byte plus the inner proof placeholder — core-rs's RetireValidator
// carries no fields of its own.
func TestRetireValidatorShape(t *testing.T) {
	s := stakingSigner(t)
	ctx := context.Background()
	me, _ := SignerAddress(ctx, s)

	tx, err := NewRetireValidatorTransaction(me, 0, 100, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	if tx.RecipientData[0] != byte(kindRetireValidator) {
		t.Errorf("variant = %d, want %d", tx.RecipientData[0], kindRetireValidator)
	}
	if len(tx.RecipientData) != 1+emptyProofLen {
		t.Errorf("data = %d bytes, want %d (variant + proof placeholder only)", len(tx.RecipientData), 1+emptyProofLen)
	}
	if tx.Flags&FlagSignaling == 0 {
		t.Error("RetireValidator must set the signaling flag")
	}
	if tx.Value != 0 {
		t.Error("RetireValidator must have zero value")
	}
	if err := SignStakingTransaction(ctx, tx, s, s); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Serialize(); err != nil {
		t.Fatal(err)
	}
}

// TestValidatorCreationRefused checks the SDK declines rather than emitting a
// transaction it cannot correctly sign.
func TestValidatorCreationRefused(t *testing.T) {
	me := MustParseAddress("NQ57 2F6C X3GB Y9B7 04U5 2BVA 4BVC M2T0 ELRL")
	_, err := newStakingTransaction(kindCreateValidator, nil, me, MustParseNIM("1"), 0, 100, NetworkTestAlbatross)
	if err == nil {
		t.Fatal("expected CreateValidator to be refused: it needs a BLS key")
	}
	if !strings.Contains(err.Error(), "BLS") {
		t.Errorf("error should explain the BLS requirement, got: %v", err)
	}
}

// TestStakingRejectsWrongSender guards against signing for an account the key
// does not control.
func TestStakingRejectsWrongSender(t *testing.T) {
	s := stakingSigner(t)
	ctx := context.Background()
	notMine := MustParseAddress("NQ57 2F6C X3GB Y9B7 04U5 2BVA 4BVC M2T0 ELRL")

	tx, err := NewAddStakeTransaction(notMine, notMine, MustParseNIM("1"), 0, 100, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	if err := SignStakingTransaction(ctx, tx, s, s); err == nil {
		t.Fatal("expected a refusal to sign for an address the key does not control")
	}
}
