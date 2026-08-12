# nimiq-go: pool SDK additions

## Context

A validator pool (GoPool, in a sibling repo) needs to: read who delegates to a
validator and how much, read reward payouts per block, and manage an existing
validator's active/jailed/retired lifecycle. Auditing nimiq-go against that
need surfaced two missing RPC read methods and four missing transaction
builders. This spec covers adding both, in the existing style of
`rpc/staking.go` and `staking.go`.

Everything here operates on a validator that already exists. Creating or
updating a validator's BLS voting key (`CreateValidator`, `UpdateValidator`)
is explicitly out of scope: those variants need a BLS proof of knowledge this
SDK has no crypto support for (see the comment on `stakingKind` in
`staking.go`). Adding BLS support is a separate, larger effort and not part
of this spec.

## 1. RPC: `GetStakersByValidatorAddress`

Add to `rpc/staking.go`, next to `GetStaker`:

```go
func (c *Client) GetStakersByValidatorAddress(ctx context.Context, addr nimiq.Address) ([]Staker, error)
```

Calls `getStakersByValidatorAddress` with `[addr.String()]`, unmarshals into
`[]Staker` (the existing type — no new type needed). Empty slice, not an
error, when the validator has zero stakers.

## 2. RPC: `GetInherentsByBlockNumber`

Add a new `rpc/inherent.go`:

```go
type Inherent struct {
    Type             string     `json:"type"`
    BlockNumber      uint32     `json:"blockNumber"`
    BlockTime        uint64     `json:"blockTime"`
    ValidatorAddress string     `json:"validatorAddress"`
    Target           string     `json:"target"`
    Value            nimiq.Luna `json:"value"`
    Hash             string     `json:"hash"`
}

func (c *Client) GetInherentsByBlockNumber(ctx context.Context, height uint32) ([]Inherent, error)
```

Calls `getInherentsByBlockNumber` with `[height]`. Not every inherent is a
reward (jail/penalize inherents share the endpoint) — add
`func (i Inherent) IsReward() bool { return i.Type == "reward" }` so callers
filter without hardcoding the string.

## 3. Transaction builders: validator lifecycle

Add to `staking.go`, following the exact shape of
`NewUpdateStakerTransaction`/`NewRetireStakeTransaction` (signaling,
zero-value, inner + outer signature via `SignStakingTransaction`):

```go
func NewDeactivateValidatorTransaction(sender Address, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error)
func NewReactivateValidatorTransaction(sender Address, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error)
func NewRetireValidatorTransaction(sender Address, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error)
```

`sender` is the validator address itself — these act on the caller's own
validator record, there is no separate target address in the payload.

```go
func NewDeleteValidatorTransaction(recipient Address, value, fee Luna, validityStartHeight uint32, network NetworkID) (*Transaction, error)
```

Outgoing staking transaction, same shape as `NewRemoveStakeTransaction`:
`kindDeleteValidator` in `SenderData`, staking contract as sender, deposit
returned to `recipient`. Signed with `SignTransaction`, not
`SignStakingTransaction`.

**Open question for implementation, not this spec:** the exact payload byte
layout for the three signaling variants (any fields beyond the inner proof
placeholder `newStakingTransaction` already appends). `kindDeactivateValidator`
/ `kindReactivateValidator` / `kindRetireValidator` constants already exist in
`staking.go` (values 2/3/4) but neither zpool nor this SDK has a reference
implementation to copy. Verify against core-rs's
`IncomingStakingTransactionData` enum (or a devnet round-trip) before writing
the builders — get this wrong and the transaction serializes fine but is
rejected by the network.

## 4. Documentation

New `docs/pool-integration.md`: one entry per addition above, stating which
pool workflow needs it (staker snapshot, reward collection, jail-cooldown
recovery). This is the artifact that answers "what does a pool need from this
SDK" for future integrators — GoPool links to it rather than restating it.

## Testing

Each RPC method: a fake-node test in the existing `client_test.go` style
(`newFakeNode` helper), covering the happy path and the empty-result case.
Each transaction builder: a serialization + `SignStakingTransaction` /
`SignTransaction` round-trip test in `staking_test.go`, asserting the encoded
`stakingKind` byte and, where a devnet is available, an actual accepted
transaction — do not merge the three signaling builders without at least one
real acceptance test, given the payload layout is unverified going in.

## Non-goals

- BLS / `CreateValidator` / `UpdateValidator`.
- No behavior changes to existing methods.
