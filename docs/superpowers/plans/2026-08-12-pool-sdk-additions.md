# Pool SDK Additions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the two gaps in nimiq-go that block building a validator pool on top of it: read a validator's delegators and their reward inherents, and manage an existing validator's active/jailed/retired lifecycle.

**Architecture:** Two new read methods on `rpc.Client` (`GetStakersByValidatorAddress`, `GetInherentsByBlockNumber`), following the exact call/unmarshal shape already used by `GetValidator`/`GetStaker`. Four new transaction builders in `staking.go` (`NewRetireValidatorTransaction`, `NewDeleteValidatorTransaction`, `NewDeactivateValidatorTransaction`, `NewReactivateValidatorTransaction`), following the exact shape already used by `NewRetireStakeTransaction`/`NewRemoveStakeTransaction`/`NewUpdateStakerTransaction`. `CreateValidator`/`UpdateValidator` are out of scope — they need a BLS proof of knowledge this SDK has no crypto for.

**Tech Stack:** Go, no new dependencies. Existing `encoder`/`decoder` in `serialize.go`, existing `Signer`/`SignStakingTransaction` in `signer.go`/`staking.go`, existing `rpc.Client` in `rpc/client.go`.

## Global Constraints

- No `float64` for on-chain amounts — `nimiq.Luna` only (spec: money precision).
- Every new RPC method follows the existing `unwrap`/`call` envelope handling in `rpc/client.go` — do not hand-roll JSON parsing.
- Every new transaction builder validates via `tx.validateStaking(kind)` (signaling ⇒ zero value, non-signaling ⇒ non-zero value, network must be Albatross) — do not skip this, it is what makes a malformed transaction fail locally instead of at broadcast.
- `CreateValidator`/`UpdateValidator` stay refused (see `TestValidatorCreationRefused`) — do not touch `kindCreateValidator`/`kindUpdateValidator` handling.
- Byte layout for the three new signaling variants (`RetireValidator`, `DeactivateValidator`, `ReactivateValidator`) must be confirmed against a real test-albatross (or devnet) transaction before the corresponding test's fixture is treated as final — this SDK has no existing reference implementation for these three, unlike every other staking variant. Never confirm against mainnet.

---

## File Structure

- Modify `rpc/staking.go` — add `GetStakersByValidatorAddress`.
- Create `rpc/inherent.go` — new `Inherent` type + `GetInherentsByBlockNumber`.
- Create `rpc/inherent_test.go` — fake-node tests for the above.
- Modify `rpc/staking_test.go` — add `TestGetStakersByValidatorAddress` (empty and non-empty cases).
- Modify `staking.go` — add the four transaction builders and update `SignStakingTransaction`'s doc comment.
- Modify `staking_test.go` — add fixture/shape tests for the four builders.
- Create `docs/pool-integration.md` — one entry per addition above.

---

## Task 1: `GetStakersByValidatorAddress`

**Files:**
- Modify: `rpc/staking.go` (add after `GetValidator`, ~line 118)
- Test: `rpc/staking_test.go`

**Interfaces:**
- Consumes: existing `Staker` type (`rpc/staking.go:19-33`), existing `Client.call` (`rpc/client.go:176`).
- Produces: `func (c *Client) GetStakersByValidatorAddress(ctx context.Context, addr nimiq.Address) ([]Staker, error)` — GoPool's election-handling flow depends on this exact signature.

- [ ] **Step 1: Write the failing test**

Add to `rpc/staking_test.go`:

```go
func TestGetStakersByValidatorAddress(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{
		"getStakersByValidatorAddress": `{"data":[` +
			`{"address":"NQ11","balance":1000000,"delegation":"NQ99"},` +
			`{"address":"NQ22","balance":2000000,"delegation":"NQ99"}` +
			`],"metadata":null}`,
	})
	stakers, err := c.GetStakersByValidatorAddress(context.Background(), testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if len(stakers) != 2 {
		t.Fatalf("got %d stakers, want 2", len(stakers))
	}
	if stakers[0].Address != "NQ11" || stakers[0].Balance != 1000000 {
		t.Errorf("staker[0] = %+v", stakers[0])
	}
}

func TestGetStakersByValidatorAddressEmpty(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{
		"getStakersByValidatorAddress": `{"data":[],"metadata":null}`,
	})
	stakers, err := c.GetStakersByValidatorAddress(context.Background(), testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if len(stakers) != 0 {
		t.Errorf("got %d stakers, want 0", len(stakers))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./rpc/... -run TestGetStakersByValidatorAddress -v`
Expected: FAIL — `c.GetStakersByValidatorAddress undefined`

- [ ] **Step 3: Write minimal implementation**

Add to `rpc/staking.go`, directly after `GetValidator`:

```go
// GetStakersByValidatorAddress lists every staker delegating to a validator.
//
// A validator with no delegators returns an empty slice, not an error — that
// is an ordinary state (see GetStaker's ErrNoStaker for the same distinction
// on a single address).
func (c *Client) GetStakersByValidatorAddress(ctx context.Context, addr nimiq.Address) ([]Staker, error) {
	var stakers []Staker
	if err := c.call(ctx, "getStakersByValidatorAddress", []any{addr.String()}, &stakers); err != nil {
		return nil, err
	}
	return stakers, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./rpc/... -run TestGetStakersByValidatorAddress -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add rpc/staking.go rpc/staking_test.go
git commit -m "feat(rpc): add GetStakersByValidatorAddress"
```

---

## Task 2: `Inherent` type and `GetInherentsByBlockNumber`

**Files:**
- Create: `rpc/inherent.go`
- Create: `rpc/inherent_test.go`

**Interfaces:**
- Consumes: existing `nimiq.Luna` (`amount.go`), existing `Client.call`.
- Produces: `type Inherent struct{...}`, `func (i Inherent) IsReward() bool`, `func (c *Client) GetInherentsByBlockNumber(ctx context.Context, height uint32) ([]Inherent, error)` — GoPool's checkpoint-handling flow depends on these exact names.

- [ ] **Step 1: Write the failing test**

Create `rpc/inherent_test.go`:

```go
package rpc

import (
	"context"
	"testing"
)

func TestGetInherentsByBlockNumber(t *testing.T) {
	c, f := newFakeNode(t, map[string]string{
		"getInherentsByBlockNumber": `{"data":[` +
			`{"type":"reward","blockNumber":120,"blockTime":1725309410050,` +
			`"validatorAddress":"NQ83 BPY4 JPJH 5B1Y NYKQ 32V1 84L4 YU35 1VTL",` +
			`"target":"NQ46 Q7MG GCJK GTPC NR5A FHQL YCCF USGE JKYA",` +
			`"value":80271028,"hash":"558a"},` +
			`{"type":"jail","blockNumber":120,"blockTime":1725309410050,` +
			`"validatorAddress":"NQ20","target":"","value":0,"hash":"ec38"}` +
			`],"metadata":null}`,
	})
	inherents, err := c.GetInherentsByBlockNumber(context.Background(), 120)
	if err != nil {
		t.Fatal(err)
	}
	if len(inherents) != 2 {
		t.Fatalf("got %d inherents, want 2", len(inherents))
	}
	if !inherents[0].IsReward() {
		t.Error("inherents[0] should be a reward")
	}
	if inherents[1].IsReward() {
		t.Error("inherents[1] (jail) must not report as a reward")
	}
	if inherents[0].Value != 80271028 {
		t.Errorf("value = %d, want 80271028", inherents[0].Value)
	}
	if string(f.gotParams["getInherentsByBlockNumber"]) != `[120]` {
		t.Errorf("params = %s, want [120]", f.gotParams["getInherentsByBlockNumber"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./rpc/... -run TestGetInherentsByBlockNumber -v`
Expected: FAIL — build error, `Inherent`/`GetInherentsByBlockNumber` undefined

- [ ] **Step 3: Write minimal implementation**

Create `rpc/inherent.go`:

```go
package rpc

import (
	"context"

	nimiq "github.com/NimMiniApps/nimiq-go"
)

// Inherent is a chain-level event recorded at a block — a reward payout, a
// jail, or a penalty. Not every inherent is a reward; check IsReward before
// treating Value/Target as a payout.
type Inherent struct {
	Type             string     `json:"type"`
	BlockNumber      uint32     `json:"blockNumber"`
	BlockTime        uint64     `json:"blockTime"`
	ValidatorAddress string     `json:"validatorAddress"`
	Target           string     `json:"target"`
	Value            nimiq.Luna `json:"value"`
	Hash             string     `json:"hash"`
}

// IsReward reports whether this inherent is a reward payout rather than a
// jail or penalty event.
func (i Inherent) IsReward() bool { return i.Type == "reward" }

// GetInherentsByBlockNumber returns every inherent recorded at height, in
// block order. A block with no inherents returns an empty slice.
func (c *Client) GetInherentsByBlockNumber(ctx context.Context, height uint32) ([]Inherent, error) {
	var inherents []Inherent
	if err := c.call(ctx, "getInherentsByBlockNumber", []any{height}, &inherents); err != nil {
		return nil, err
	}
	return inherents, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./rpc/... -run TestGetInherentsByBlockNumber -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add rpc/inherent.go rpc/inherent_test.go
git commit -m "feat(rpc): add GetInherentsByBlockNumber"
```

---

## Task 3: `NewRetireValidatorTransaction`

**Files:**
- Modify: `staking.go` (add after `NewRemoveStakeTransaction`, ~line 238)
- Test: `staking_test.go`

**Interfaces:**
- Consumes: `kindRetireValidator` (already defined, `staking.go:27`), `newStakingTransaction` (`staking.go:68`).
- Produces: `func NewRetireValidatorTransaction(sender Address, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error)`.

Payload is empty beyond the inner proof placeholder `newStakingTransaction` already appends — core-rs's `RetireValidator` variant carries no fields but the authorization proof. `sender` is the validator's own address; sign with `SignStakingTransaction(ctx, tx, validatorSigner, validatorSigner)` (same signer twice, exactly like `NewRetireStakeTransaction`).

- [ ] **Step 1: Write the failing test**

Add to `staking_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestRetireValidatorShape -v`
Expected: FAIL — `NewRetireValidatorTransaction undefined`

- [ ] **Step 3: Write minimal implementation**

Add to `staking.go`, after `NewRemoveStakeTransaction`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestRetireValidatorShape -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add staking.go staking_test.go
git commit -m "feat: add NewRetireValidatorTransaction"
```

---

## Task 4: `NewDeleteValidatorTransaction`

**Files:**
- Modify: `staking.go` (add after Task 3's addition)
- Test: `staking_test.go`

**Interfaces:**
- Consumes: `kindDeleteValidator` (already defined, `staking.go:37`).
- Produces: `func NewDeleteValidatorTransaction(recipient Address, value, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error)`.

Outgoing staking transaction — the deposit returns to `recipient`. Same shape as `NewRemoveStakeTransaction`, signed with `SignTransaction`, not `SignStakingTransaction`.

- [ ] **Step 1: Write the failing test**

Add to `staking_test.go`:

```go
func TestDeleteValidatorIsOutgoing(t *testing.T) {
	me := MustParseAddress("NQ57 2F6C X3GB Y9B7 04U5 2BVA 4BVC M2T0 ELRL")
	tx, err := NewDeleteValidatorTransaction(me, MustParseNIM("100000"), 0, 100, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Sender != StakingContractAddress || tx.SenderType != AccountStaking {
		t.Error("DeleteValidator must be sent from the staking contract")
	}
	if len(tx.SenderData) != 1 || tx.SenderData[0] != kindDeleteValidator {
		t.Errorf("sender data = %x, want the DeleteValidator variant tag", tx.SenderData)
	}
	if tx.Recipient != me {
		t.Error("deposit must return to recipient")
	}
	if tx.Format() != FormatExtended {
		t.Error("an outgoing staking transaction must use the extended format")
	}
}

func TestDeleteValidatorRequiresValue(t *testing.T) {
	me := MustParseAddress("NQ57 2F6C X3GB Y9B7 04U5 2BVA 4BVC M2T0 ELRL")
	if _, err := NewDeleteValidatorTransaction(me, 0, 0, 100, NetworkTestAlbatross); err == nil {
		t.Fatal("expected an error for zero value")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestDeleteValidator -v`
Expected: FAIL — `NewDeleteValidatorTransaction undefined`

- [ ] **Step 3: Write minimal implementation**

Add to `staking.go`:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestDeleteValidator -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add staking.go staking_test.go
git commit -m "feat: add NewDeleteValidatorTransaction"
```

---

## Task 5: `NewDeactivateValidatorTransaction` and `NewReactivateValidatorTransaction`

**Files:**
- Modify: `staking.go` (add after Task 4's addition, plus update `SignStakingTransaction`'s doc comment)
- Test: `staking_test.go`

**Interfaces:**
- Consumes: `kindDeactivateValidator` / `kindReactivateValidator` (already defined, `staking.go:25-26`), `encodeOptionalAddress` is NOT used here — the address is required, not optional.
- Produces: `func NewDeactivateValidatorTransaction(sender, validatorAddress Address, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error)`, `func NewReactivateValidatorTransaction(sender, validatorAddress Address, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error)`.

Payload is `validator_address` (20 raw bytes) followed by the inner proof placeholder. These two are authorized by the validator's separate **signing key**, not by `sender` — `sender` only pays the fee and may be a different account. Sign with `SignStakingTransaction(ctx, tx, signingKeySigner, feePayerSigner)`.

- [ ] **Step 1: Write the failing test**

Add to `staking_test.go`:

```go
// TestDeactivateReactivateShape checks the payload carries the target
// validator's address explicitly — unlike every other staking variant in
// this SDK, sender here is just the fee payer, not the account being acted
// on, so the target must travel in the data.
func TestDeactivateReactivateShape(t *testing.T) {
	signingKey := stakingSigner(t) // stands in for the validator's signing key
	feePayer := stakingSigner(t)   // may be a different account entirely
	ctx := context.Background()
	feePayerAddr, _ := SignerAddress(ctx, feePayer)
	validator := MustParseAddress("NQ57 2F6C X3GB Y9B7 04U5 2BVA 4BVC M2T0 ELRL")

	cases := []struct {
		name string
		kind stakingKind
		tx   *Transaction
	}{}

	tx, err := NewDeactivateValidatorTransaction(feePayerAddr, validator, 0, 100, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		name string
		kind stakingKind
		tx   *Transaction
	}{"Deactivate", kindDeactivateValidator, tx})

	tx, err = NewReactivateValidatorTransaction(feePayerAddr, validator, 0, 100, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	cases = append(cases, struct {
		name string
		kind stakingKind
		tx   *Transaction
	}{"Reactivate", kindReactivateValidator, tx})

	for _, c := range cases {
		if c.tx.RecipientData[0] != byte(c.kind) {
			t.Errorf("%s: variant = %d, want %d", c.name, c.tx.RecipientData[0], c.kind)
		}
		wantLen := 1 + AddressLen + emptyProofLen
		if len(c.tx.RecipientData) != wantLen {
			t.Errorf("%s: data = %d bytes, want %d (variant + address + proof placeholder)", c.name, len(c.tx.RecipientData), wantLen)
		}
		gotAddr := c.tx.RecipientData[1 : 1+AddressLen]
		if string(gotAddr) != string(validator[:]) {
			t.Errorf("%s: payload address does not match target validator", c.name)
		}
		if c.tx.Sender != feePayerAddr {
			t.Errorf("%s: sender must be the fee payer, not the validator", c.name)
		}
		if err := SignStakingTransaction(ctx, c.tx, signingKey, feePayer); err != nil {
			t.Errorf("%s: signing with distinct signing-key and fee-payer signers should succeed: %v", c.name, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test . -run TestDeactivateReactivateShape -v`
Expected: FAIL — both constructors undefined

- [ ] **Step 3: Write minimal implementation**

Add to `staking.go`:

```go
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
```

Also update `SignStakingTransaction`'s doc comment in `signer.go`... actually in `staking.go` (it lives there, not `signer.go` — see `staking.go:239`) to state explicitly that "staker" and "sender" are role names, not always literally a staker and the transaction's sender:

```go
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
```

(Only the comment changes — the function body is untouched.)

- [ ] **Step 4: Run test to verify it passes**

Run: `go test . -run TestDeactivateReactivateShape -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add staking.go staking_test.go
git commit -m "feat: add NewDeactivateValidatorTransaction and NewReactivateValidatorTransaction"
```

---

## Task 6: Devnet fixture verification

**Files:**
- Modify: `staking_test.go` (replace the shape-only assertions from Tasks 3 and 5 with byte-exact fixtures, once confirmed)
- Create: `docs/phase-pool-staking.md` (mirrors the existing `docs/phase5-staking.md` reference pattern cited in `staking_test.go`'s file header)

**Interfaces:**
- Consumes: `NewRetireValidatorTransaction`, `NewDeactivateValidatorTransaction`, `NewReactivateValidatorTransaction` from Tasks 3 and 5.
- Produces: nothing new — this task raises confidence in Tasks 3 and 5's output from "structurally plausible" to "verified against a real node," matching the bar every other staking variant in this file already meets.

This task cannot be completed inside an automated test run — it requires a live test-albatross or devnet node and a funded test validator. **Never run this against mainnet.**

- [ ] **Step 1: Generate reference transactions against a devnet**

Using a disposable devnet validator (e.g. zpool's `devnet.config.toml` / GoPool's `devlab/` devnet setup, or any test-albatross node with a throwaway validator already registered), call the node's own transaction-builder RPCs to get ground-truth serialized transactions:

```bash
curl -s -X POST "$DEVNET_RPC" -H 'Content-Type: application/json' -d '{
  "jsonrpc":"2.0","id":1,"method":"createRetireValidatorTransaction",
  "params":["<sender-wallet-address>","<validator-wallet-address>",0,"+0"]
}'
curl -s -X POST "$DEVNET_RPC" -H 'Content-Type: application/json' -d '{
  "jsonrpc":"2.0","id":1,"method":"createDeactivateValidatorTransaction",
  "params":["<sender-wallet-address>","<validator-address>","<signing-secret-key-hex>",0,"+0"]
}'
```

(Mirror for `createReactivateValidatorTransaction`.) Each call returns a raw hex transaction.

- [ ] **Step 2: Decode each reference transaction with this SDK's own parser**

```go
raw, _ := hex.DecodeString(rawHexFromNode)
tx, err := nimiq.ParseTransaction(raw)
// tx.RecipientData is the ground truth payload: variant byte, any fields,
// then the real (non-placeholder) proof.
```

Compare `tx.RecipientData`'s structure (length, field order, whether the address bytes match what Task 5 assumed) against what `NewDeactivateValidatorTransaction`/`NewReactivateValidatorTransaction`/`NewRetireValidatorTransaction` produce before signing. If they match, the builders in Tasks 3 and 5 are confirmed as-is. If they don't, fix the builder to match — the node's own encoding is authoritative.

- [ ] **Step 3: Broadcast one of each on the devnet and confirm acceptance**

Sign the SDK-built transaction (not the node's) with `SignStakingTransaction` using the same devnet keys, submit via `sendRawTransaction`, and confirm it executes successfully (`getTransactionByHash` shows `executionResult: true` after a macro block passes) — this is the real acceptance test, since a transaction can serialize correctly and still be rejected on a rule this SDK doesn't model.

- [ ] **Step 4: Replace shape assertions with byte-exact fixtures**

Update `TestRetireValidatorShape` and `TestDeactivateReactivateShape` (or split into new fixture tests named after the pattern of `TestCreateStakerOnChainFixture`) to assert the exact hex from Step 1, the same way `TestCreateStakerOnChainFixture` does. Rename the tests accordingly (e.g. `TestRetireValidatorOnChainFixture`) since they are no longer shape-only.

- [ ] **Step 5: Write `docs/phase-pool-staking.md`**

One short paragraph per variant, following the existing header comment convention in `staking_test.go` ("The layouts asserted here were validated on test-albatross..."): which devnet/network the fixture was confirmed on, and the block/tx hash if available.

- [ ] **Step 6: Run the full test suite**

Run: `go test ./... -v`
Expected: PASS, including the now byte-exact fixture tests.

- [ ] **Step 7: Commit**

```bash
git add staking_test.go docs/phase-pool-staking.md
git commit -m "test: confirm validator lifecycle transaction fixtures against devnet"
```

---

## Task 7: `docs/pool-integration.md`

**Files:**
- Create: `docs/pool-integration.md`

**Interfaces:**
- Consumes: everything from Tasks 1–5 (final API names).
- Produces: nothing code-facing — this is the "what does a pool need from this SDK" deliverable the overall project's design calls for.

- [ ] **Step 1: Write the doc**

```markdown
# Building a validator pool on nimiq-go

A validator pool needs to read who delegates to a validator and how much,
read reward payouts per block, and manage an existing validator's
active/jailed/retired lifecycle. This SDK's pool-specific surface:

## Reading delegation and rewards

- `Client.GetStakersByValidatorAddress(ctx, validatorAddr) ([]Staker, error)`
  — every staker currently delegating to a validator, with their balance.
  Used to compute each delegator's share of a reward payout.
- `Client.GetInherentsByBlockNumber(ctx, height) ([]Inherent, error)` —
  every reward/jail/penalty inherent recorded at a block. Filter to
  `Inherent.IsReward()` before treating `Value`/`Target` as a payout; a block
  can also carry jail or penalty inherents through the same endpoint.

## Managing an existing validator

These act on a validator that already exists — creating one, or changing its
BLS voting key, needs a BLS proof of knowledge this SDK cannot produce.

- `NewRetireValidatorTransaction` — the validator retires itself. Sign with
  the validator's own key, passed twice to `SignStakingTransaction`.
- `NewDeleteValidatorTransaction` — reclaim a retired validator's deposit.
  Sign with `SignTransaction`, not `SignStakingTransaction`.
- `NewDeactivateValidatorTransaction` / `NewReactivateValidatorTransaction` —
  authorized by the validator's separate signing key, not by whoever pays the
  fee. See the function doc comments for the two-signer call shape.

## Not supported

Creating a validator, or changing its voting key, requires a BLS proof of
knowledge. This SDK has no BLS support; use the node's own wallet RPCs
(`createNewValidatorTransaction`, `createUpdateValidatorTransaction`) for
that one-time setup step instead.
```

- [ ] **Step 2: Commit**

```bash
git add docs/pool-integration.md
git commit -m "docs: add pool integration guide"
```
