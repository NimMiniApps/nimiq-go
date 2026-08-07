# Phase 5 — Staking Transactions

Goal: remove the last reason any project runs a local Nimiq node. PlayNimiq's node existed only because staking transactions had to be signed by an unlocked node wallet.

**Status: done for the Ed25519 tier.** Every supported variant was built and signed offline, then accepted by a real `test-albatross` mempool — which verifies both structure and the embedded signature.

---

## Validated on testnet

Endpoint `https://rpc-testnet.nimiqscan.com/`. No node was run.

| Variant | Mempool | Execution | Notes |
|---|---|---|---|
| `CreateStaker` | ✅ accepted | ✅ **succeeded** | 1000 NIM delegated; staker confirmed on chain |
| `AddStake` | ✅ accepted | ✅ **succeeded** | +500 NIM, twice |
| `SetActiveStake` | ✅ accepted | ✅ **succeeded** | active/inactive split exactly as specified |
| `UpdateStaker` | ✅ accepted | ⚠️ failed | delegation cannot change while stake is active — a chain rule, not a format problem |
| `RetireStake` | ✅ accepted | ⚠️ failed | `inactiveFrom` cooldown had not elapsed |
| `RemoveStake` | ✅ accepted | — | outgoing form validated; `retiredBalance` was 0 so it could not succeed |

Mempool acceptance is the meaningful signal: `verify_incoming_transaction` parses the data and checks the embedded signature, so an accepted transaction proves the serialization and the signature-in-data construction are both correct. The two execution failures are ordinary staking state rules.

Final on-chain staker state:

```
address          NQ22 SR01 QG8A LP4L XANX CETR B7QD N0G7 6Q20
balance          100000000    (active,   1000 NIM)
inactiveBalance  100000000    (inactive, 1000 NIM)
delegation       NQ57 2F6C X3GB Y9B7 04U5 2BVA 4BVC M2T0 ELRL
```

Total 2000 NIM = CreateStaker 1000 + AddStake 500 + AddStake 500. Note that `getStakerByAddress`'s `balance` is the **active** balance, not the total — worth knowing before writing a stake-tracking query.

A successful `CreateStaker` payload is checked into `staking_test.go` as a byte-exact fixture, so the Go implementation is pinned to bytes that really executed.

---

## The two-signature rule

Incoming staking transactions carry two signatures, and the order is not interchangeable:

1. Encode the data with the embedded proof **zeroed** (98 zero bytes)
2. Inner signature over `serialize_content()` — authorizes the staking action
3. Write the real inner proof into `RecipientData`
4. Outer signature over `serialize_content()` **recomputed**, because step 3 changed the content — authorizes the sending account
5. Outer proof becomes `Transaction.Proof`

Mirrors `make_signed_incoming_transaction` in `primitives/account/tests/staking_contract/mod.rs:105-130`. Signing in the other order yields a transaction that serializes cleanly and is rejected by the network.

`SignStakingTransaction(ctx, tx, staker, sender)` does this. The two signers are usually the same key; passing them separately is what allows the staking authorization to live in a different custody domain from the fee-paying account later.

The embedded proof is the final field of every supported variant, so it is simply the trailing 98 bytes. `AddStake` is the sole exception — it embeds no proof at all, since anyone may add stake to any staker.

---

## Encoding

Variant tags are postcard enum indices, fixed by field order in `IncomingStakingTransactionData`:

```
0 CreateValidator      (BLS — unsupported)      5 CreateStaker
1 UpdateValidator      (BLS — unsupported)      6 AddStake
2 DeactivateValidator                           7 UpdateStaker
3 ReactivateValidator                           8 SetActiveStake
4 RetireValidator                               9 RetireStake
```

Outgoing tags travel in `SenderData`: `0` DeleteValidator, `1` RemoveStake.

Rules enforced locally so a bad transaction fails at construction rather than at broadcast:

- Signaling variants (`UpdateValidator`, `Deactivate/Reactivate/RetireValidator`, `UpdateStaker`, `SetActiveStake`, `RetireStake`) must set `SIGNALING` and carry zero value
- Non-signaling staking variants must carry a non-zero value
- The staking contract rejects `CONTRACT_CREATION`
- `Option<Address>` is postcard: `0x00` none, `0x01` + 20 bytes some

---

## A correctness bug this work exposed

**Inclusion is not success.** Albatross records failed transactions in blocks: the fee is charged, the transaction is permanent, and its effect is not applied. The first `CreateStaker` attempt — which delegated to a validator that does not exist on testnet — was included and macro-final with `executionResult: false`.

The confirmation logic written in Phase 4 would have reported that as a settled payout.

Fixed: `rpc.Transaction.ExecutionResult`, `Confirmation.Succeeded`, and `WaitForTransaction` now returns `ErrExecutionFailed` alongside a non-nil confirmation rather than a quietly unsuccessful one, so a caller ignoring the error cannot record a payout that never happened.

This is exactly the Ethereum assumption the brief warned about, and it would have been easy to ship.

---

## BLS: still out of scope, still not blocking

`CreateValidator` and `UpdateValidator` need BLS12-381 (`voting_key`, `proof_of_knowledge`). The SDK refuses them with a clear error rather than emitting something it cannot sign.

Only `nimiq-validator-activator-go` uses those, and it runs on a validator — a consensus-participating node by definition. So BLS never blocked a node removal.

---

## Node reduction: complete

With staking covered, **no project in the workspace needs a local node except actual validators.**

| Project | Node before | Node after |
|---|---|---|
| NimConnect | mainnet pico node, purely to sign escrow payouts | none |
| PlayNimiq | testnet node for payouts **and staking** | none |
| NimWorld | none | none |
| Explorer/indexer | own history node | history node in the shared RPC cluster |
| Validators | node | node — irreducible |

---

## State

```
go test ./...   36 tests, 0 failures     go vet    clean
go test -race   clean                    gofmt     clean
dependencies    golang.org/x/crypto/blake2b only
```
