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
