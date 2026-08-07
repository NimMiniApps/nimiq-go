# Phase 1 — Nimiq Integration Audit

Date: 2026-08-07. Scope: `/home/maestro/Documents/projects`. No implementation performed.

---

## 0. URGENT — act before anything else

**A mainnet Nimiq private key is committed and pushed to `origin/main` in PlayNimiq.**

- File: `nimiq-2048/wallet-export-20260419-233220.json`
- Commit: `75148c8 feat(): move to postgress`, present on `origin/main`
- Contents: `address`, `public_key`, `private_key`, `"network": "main-albatros[s]"`
- Not covered by `.gitignore`; `wallet-export.sh` (the generator) is also tracked.

This is independent of the SDK work. Rotate the key, sweep the funds, purge from history, add the ignore rule. History rewrite on a pushed branch needs a decision from you — say the word and I'll prepare it.

Related: `nimiq-key-generator/keys/` is gitignored (good), and `nimiq-2048/tools/` + `wallet-export.sh` exist to move raw hex keys around by hand.

---

## 1. Project inventory

`PlayNimiq` is not a directory name — it is `nimiq-2048/` (remote: `gitlab.com/beardsoft/games/playnimiq`). Confirmed by remote URL and `cmd/playnimiq/main.go`.

### Primary (named in the brief)

| Project | Path | Nimiq role |
|---|---|---|
| NimConnect | `NimConnect/` | Handles registry, profiles, inbox, marketplace + custodial escrow. Go backend + Vue/Hub frontend. |
| PlayNimiq | `nimiq-2048/` | Multi-game platform, on-chain reward payouts, staking. Go backend + Vue frontend. |
| NimWorld | `NimWorld/` | Plaza/app-launcher shell. Read-only balance + wallet auth today. Go API + Vue frontend. |

### Other Nimiq-touching repos found (16 independent Go RPC clients total)

`NimFeed`, `NimBomber`, `NimJeweld`, `NimDoom`/`nimiq-doom`, `Nimagotchi`, `NimDexer`, `NimMail`, `NimiqLens`, `NimiqMiniApps`, `nimiq-mini-app`, `nimiq-notifier`, `nimiq-vpn`, `nimiq-v2-explorer`, `nimiq-validator-activator-go`, `chaintruth`, `retro-crypto/nimiq/uploader`, `BlockSnake`, `idle-game`, `TipWall`, `nspace`, `miniapp-sdk-utils`, `mcp-gateway/nimiq-readonly-mcp`, `NimAgent`.

Also present and directly useful for Phase 2: **`core-rs-albatross/` at `v1.7.1-28-g35e2a8334`** — the authoritative Rust implementation is already checked out locally. Phase 2 reads it rather than guessing.

---

## 2. Duplicated functionality

### 2.1 Address derivation / IBAN checksum — **10 independent Go implementations**

`nimiq-mini-app/backend/internal/nimiq/address.go`, `NimiqMiniApps/backend/nimiqaddress.go`, `NimJeweld/backend/internal/validator/nimiq_address.go`, `NimWorld/apps/api/nimiq.go`, `nimiq-2048/backend/platform/nimiq/address.go`, `NimConnect/backend/address.go`, `NimBomber/apps/backend/internal/nimiq/message.go`, `BlockSnake/backend/internal/wallet/nimiq.go`, plus TS copies.

Three genuinely different algorithms for the same thing:

- **NimConnect** — hand-rolled bit-shifting base32 (`toBase32`), IBAN mod-97 via `fmt.Sscanf` chunking. Fragile: `Sscanf` errors are discarded.
- **PlayNimiq** — `base32.NewEncoding` + byte-buffer mod-97 with `strconv.ParseUint`, ported from gimiq.
- **NimWorld** — `base32.NewEncoding(...).WithPadding(NoPadding)` + streaming per-character mod-97 (`ibanMod97`). Cleanest of the three; overflow-free by construction.

Only NimWorld has a real **validator** (`validNimiqAddress`, checksum-verifying). NimConnect has `isValidNimiqAddress`; PlayNimiq has none — it compares normalized strings. **No project can go from a `NQ…` string back to 20 raw address bytes** — which is a hard blocker for offline transaction building.

### 2.2 Nimiq Signed Message verification — **13 implementations**

`\x16Nimiq Signed Message:\n` + decimal length + message, SHA-256, Ed25519 verify. Found in NimConnect, PlayNimiq, NimWorld, NimBomber, NimiqMiniApps, nimiq-vpn, nimiq-mini-app, BlockSnake, TipWall, nspace, and others. Semantically identical everywhere. Pure win to consolidate.

### 2.3 JSON-RPC client — **16 Go + ~10 TS implementations**

Every one re-solves the same three problems, differently:

- The Albatross `{"data": …, "metadata": …}` envelope. NimConnect probes for `metadata != nil` before unwrapping (correct-ish); PlayNimiq's `IsConsensusEstablished` tries **five** shapes in sequence (`client.go:147-205`); NimWorld tries `Result.Data.Balance` then `Result.Balance` and **treats `0` as "try the other field"** (`balance.go:80-88`) — a balance of exactly 0 falls through.
- Field-name drift (`sender`/`from`, `recipient`/`to`, `data`/`recipientData`) — NimConnect normalizes it, others don't.
- Timestamp units — PlayNimiq's `txTime` heuristically detects ms-vs-s by magnitude.

None of them take a `context.Context`. None have retry, failover, or typed errors.

### 2.4 NIM ↔ Luna conversion — float-based everywhere

`LUNA_PER_NIM = 100_000` re-declared in at least 6 places. `NimiqLens/backend/balance.go` uses `const lunaPerNIM = 100000.0` (float64). `NimWorld` and `nimiq-mini-app` frontends use `Math.round(nim * LUNA_PER_NIM)`. Exactly the primitive-obsession the SDK should kill.

### 2.5 Zero offline transaction construction anywhere

Grepped the whole workspace for `serializeContent`, `SignatureProof`, `serializeTransaction`. Only hit outside `core-rs-albatross/` is `BlockSnake/backend/internal/wallet/` — and its `SignatureProof` is a *signed-message* auth struct, not a transaction proof. **No project can build, serialize, sign, or hash a Nimiq transaction locally.** This is the SDK's core value and it is greenfield.

---

## 3. Per-project findings

### 3.1 NimConnect

**Files:** `backend/nimiq_rpc.go` (299 L), `backend/address.go`, `backend/account.go`, `backend/escrow_wallet.go`, `backend/escrow_config.go`, `backend/marketplace_settlement.go`, `backend/marketplace_choreography.go`, `backend/marketplace_signer.go`, `backend/marketplace_escrow_watcher.go`, `backend/handles_sync.go`, `backend/auth.go`, `src/services/hub.ts`, `packages/profile-client/src/rpc.ts`.

**Two-endpoint architecture — already the model you want, half-built.** `main.go:161` points reads at a *public gateway* (`NIMIQ_RPC_URL`, default `https://rpc-mainnet.nimiqscan.com`). `main.go:191` points signing at `ESCROW_SIGNER_RPC_URL` — a private node. NimConnect has already proven the split works; it just can't sign without a node yet.

**The escrow node exists only to sign.** `nimiq/client.toml` runs `sync_mode = "pico"` on `main-albatross` with an RPC method allowlist of exactly six methods:

```
isConsensusEstablished, importRawKey, listAccounts,
isAccountUnlocked, unlockAccount, sendBasicTransactionWithData
```

Five of six are wallet management. **This node contributes nothing but a signing oracle.** `SetupEscrowWallet` imports the raw hex key and calls `UnlockAccount(addr, "", 0)` — unlocked indefinitely, empty passphrase, for the process lifetime of the node. Defence is network isolation + RPC basic auth (`requireEscrowSignerAuth` fails closed, SEC-003 — good), but the residual risk is exactly what the comment at `main.go:214` admits.

**Escrow is custodial and pooled, not HTLC.** Buyers deposit into one shared escrow address, tagged with `NME1:<ref>` in the transaction data (`marketplace_escrow_watcher.go`). `SettlementWorker` pays out with `SendBasicTransactionWithData`. HTLC code exists (`handles_sync.go:htlcCreator`, `htlcOwnerFromCreationData`) but only to **read** Nimiq Pay users' rotating HTLC contracts to resolve identity — it never creates one. Anyone reading "NimConnect has escrow + HTLC" and inferring non-custodial escrow would be wrong.

**Idempotency is manual and fragile.** `Settle`/`Refund` persist a `PayoutAttemptedAt` marker before signing; if the process dies in that window the trade is permanently frozen for manual reconciliation (`"has an in-flight payout attempt with no recorded hash"`). The root cause: node-side signing returns the hash only *after* broadcast, so there is no pre-broadcast identity to record.

**Hub path already produces a raw signed tx and nobody inspects it.** `hub.ts:hubSignTransaction` returns `{ serializedTx, hash }`. `SubmitHubTransaction` (`marketplace_choreography.go`) does `SendRawTransaction(rawHex)` → `GetTransactionByHash(hash)` → `verify(tx)`. It cannot validate the bytes before broadcast; it broadcasts blind and re-reads from chain. A deserializer would let it verify sender/recipient/value/hash **before** submitting.

**Notable correctness care already present** (do not regress in migration): SEC-004 completeness checks (`GetAllTransactionsByAddress` + `Complete()` fail-closed), `transactionIndex` as `*uint64` to distinguish missing from 0, macro-finality gating for deposits (`GetLastMacroBlockNumber`), `registryOrderingAmbiguous`.

### 3.2 PlayNimiq (`nimiq-2048`)

**Files:** `backend/platform/nimiq/client.go` (904 L), `reward.go` (1110 L), `staking.go` (374 L), `address.go`, `reward_ref.go`, plus `platform/rewards/*` and `platform/treasury/*`.

**Fully node-wallet dependent.** RPC methods called: `importRawKey`, `unlockAccount`, `isAccountUnlocked`, `listAccounts`, `createAccount`, `sendBasicTransaction`, `sendBasicTransactionWithData`, `sendNewStakerTransaction`, `sendStakeTransaction`, `sendRetireStakeTransaction`, `sendRemoveStakeTransaction`, `sendSetActiveStakeTransaction`, `getStakerByAddress`, `getAccountByAddress`, `getTransactionByHash`, `getTransactionsByAddress`, `isConsensusEstablished`.

`NewRewardService` (`reward.go:91-206`) requires consensus, imports `NIMIQ_WALLET_KEY`, and unlocks with an empty passphrase and duration 0. On failure it **shells out to `curl` via `exec.Command`** (`reward.go:127`, `reward.go:175`) with the private key interpolated into the shell string — the key lands in the process table and any shell trace. Remove this regardless of SDK progress.

**Already splitting endpoints too.** `NIMIQ_HISTORY_RPC_ENDPOINT` defaults to `https://rpc-testnet.nimiqscan.com/` — history queries already go to a public RPC, while wallet/send/consensus stay on the local node (`client.go:26-44`). Same shape as NimConnect. Second independent confirmation that reads don't need a node.

**The payout reconciliation system is a workaround for not having offline signing.** Because the hash is unknown until after broadcast, `SendReward` embeds `payout-op:<reference>` in the transaction data and `FindTransactionByReference` paginates up to 5000 transactions of wallet history to answer "did this payout actually go out?". The error taxonomy around it (`ErrPreBroadcast` / `ErrAmbiguousBroadcast` / `ErrChainPending` / `ErrDataTxUnsupported`) is genuinely well-reasoned and worth preserving as SDK error semantics — but **offline signing makes the whole reconciliation scan unnecessary**: sign locally, you know the hash before broadcast, rebroadcast is idempotent.

Smaller issues: `SendReward` does `time.Sleep(1500ms)` inline to poll status; no `context.Context` anywhere; `DefaultFee = 0` hardcoded.

**Staking is the one genuine node-signing dependency.** `staking.go` builds 6 staking transaction types via node RPC, then polls `GetStakerPosition` to verify bucket transitions. Replacing this needs the SDK to serialize staking-contract transactions — real work, correctly out of scope for v1.

### 3.3 NimWorld

**Files:** `apps/api/nimiq.go`, `apps/api/balance.go`, `apps/api/session.go`, `apps/web/src/auth/session.ts`, `apps/web/src/adapters/payment/paymentConfig.ts`.

**Runs no Nimiq node.** Uses `https://rpc-mainnet.nimiqscan.com/` (overridable via `NIMIQ_RPC_URL`). Backend surface today is exactly two things: `getAccountByAddress` for balance, and Nimiq signed-message verification for login. Frontend does Hub login (browser) or `@nimiq/mini-app-sdk listAccounts` inside Nimiq Pay.

Payments are **not implemented** — `paymentConfig.ts` has a hardcoded recipient address and float `nimToLuna`. The economy/marketplace/rewards features described in the brief are still ahead of the code.

This makes NimWorld the **ideal first migration target**: small surface, no node, no keys, and it exercises exactly the read-only subset that must work without importing any signing code.

Two bugs to fix in migration: `fetchBalanceLuna` treats balance `0` as "field absent" and falls through; `validNimiqAddress`'s alphabet check has a redundant digit clause.

---

## 4. Node replacement analysis

| Project | Current node usage | RPC replaces? | Offline signing replaces? | Node still required? | Recommendation |
|---|---|---|---|---|---|
| **NimConnect** | Escrow signing (`importRawKey`+`unlockAccount`+`sendBasicTransactionWithData`) on a private pico node | No — public gateways expose no wallet RPC | **Yes — entirely** | **No** | **Remove the node.** SDK signs offline; broadcast via `sendRawTransaction` to any RPC. This is the single biggest win in the workspace. |
| NimConnect | Balance / tx history / block height / macro block | Already does (public gateway) | n/a | No | Keep public RPC; add failover + typed errors via SDK |
| NimConnect | Hub raw-tx broadcast | Yes (any RPC) | Partially — SDK should *validate* bytes pre-broadcast | No | Public RPC + SDK deserializer/validator |
| NimConnect | HTLC creator lookup for identity | Yes (`getAccountByAddress` + history) | No — needs chain state | No | Public RPC; SDK parses HTLC creation data |
| **PlayNimiq** | Reward payout signing | No | **Yes — entirely** | **No** | **Remove for payouts.** Deletes the reconciliation scan too. |
| PlayNimiq | History queries | Already does (nimiqscan) | n/a | No | Keep; make endpoint config explicit, not env-sniffed |
| PlayNimiq | Balance / consensus / account lookup | Yes | n/a | No | Public or shared private RPC |
| PlayNimiq | **Staking txs** (`sendStake`/`Retire`/`Remove`/`SetActiveStake`) | No | **Yes** — staker tier is Ed25519-only (see §4a) | **No** | **Remove.** SDK serializes staker-tier staking transactions. |
| PlayNimiq | Staker position polling | Yes (`getStakerByAddress`) | n/a | No | Public RPC |
| **NimWorld** | none | — | — | **No** | Already correct. Migrate to SDK read-only client. |
| Others (NimFeed, NimiqLens, notifier, explorer, …) | mostly reads; `nimiq-vpn`/`retro-crypto` use `importRawKey` | Reads: yes | Signers: yes | No | Reads → shared RPC cluster. Signers → SDK. Explorer/indexer history need is served by a history node *in the cluster*. |
| `nimiq-validator-activator-go` | `sendNewValidatorTransaction` (BLS), `sendReactivateValidatorTransaction` | No | Reactivate: yes. CreateValidator: needs BLS | **Yes** — it runs on a validator | Irreducible: a validator participates in consensus by definition. Not an SDK problem. |

**Net: all three named projects drop their node outright.** Everything blocking removal is signing, and signing is exactly what the SDK is for.

The shared RPC cluster (history node + pico node, read-only, no wallet enabled) serves every read path in the workspace. Adding a history node there covers the explorer/indexer case too. **The only remaining local nodes are actual validators.**

---

## 5. Security findings (beyond §0)

| # | Finding | Where |
|---|---|---|
| S1 | Mainnet private key committed & pushed | `nimiq-2048/wallet-export-*.json` |
| S2 | Private key interpolated into a shell command via `exec.Command("sh","-c", …)` | `nimiq-2048/.../reward.go:127,175` |
| S3 | Hot wallets unlocked indefinitely with empty passphrase | NimConnect `escrow_wallet.go:50`; PlayNimiq `reward.go:169` |
| S4 | Raw hex keys passed as env vars (`NIMIQ_WALLET_KEY`) | both compose files |
| S5 | No payout limits / rate caps / policy layer on the escrow signer | NimConnect settlement |
| S6 | Hub-signed raw transactions broadcast without pre-broadcast inspection | `marketplace_choreography.go` |
| S7 | Custodial pooled escrow — one key controls all in-flight trades | NimConnect marketplace |

Good practice already in place, worth carrying into the SDK's security model: NimConnect's fail-closed RPC auth (SEC-003), the network-isolated signer with a method allowlist, and PlayNimiq's pre-broadcast/ambiguous-broadcast error split.

---

## 6. What this means for the SDK (input to Phase 3)

Ranked by evidence found, not by how interesting it is to build:

1. **Offline transaction build + sign + serialize + hash.** Zero prior art in the workspace; removes 2 nodes; gives real idempotency. This *is* the SDK.
2. **Address type** with parse *and* NQ→20-bytes decode, checksum validation, equality. 10 duplicates, none can decode. Required by (1).
3. **Luna amount type**, integer-only. 6 duplicates, several float-based.
4. **One RPC client** with `context.Context`, typed errors, the `{data,metadata}` envelope handled once, field-name normalization, retry/failover. 16 duplicates.
5. **Signed-message verify/sign.** 13 duplicates, all identical.
6. **Raw transaction deserializer + validator** for Hub-signed bytes. Unlocks NimConnect's blind-broadcast path.
7. **Signer interface** + `PrivateKeySigner`, designed so a remote signing service slots in later without touching transaction code.
8. **Confirmation semantics under Albatross** — micro-block inclusion vs macro finality. NimConnect already gates on `getLastMacroBlock`; that is the right notion and belongs in the SDK, not re-derived per project.

Explicitly **not** in the SDK: handle registry / payment requests / invoices / split payments / inbox / contacts / marketplace state machines / reward tiers / payout policy / treasury / Hub browser integration / game logic. Those stay in the applications.

9. **Staking transaction serialization — in scope** (revised after §4 follow-up; see §4a).

### 4a. Staking — resolved, target is zero nodes

Checked `core-rs-albatross/primitives/transaction/src/account/staking_contract/structs.rs`. Two tiers:

- **Staker tier** — `CreateStaker`, `AddStake`, `UpdateStaker`, `SetActiveStake`, `RetireStake` (incoming) and `RemoveStake` (outgoing). **Ed25519 `SignatureProof` only** — no new cryptography over basic transactions. This is exactly the set PlayNimiq uses. **In scope.**
- **Validator tier** — `DeactivateValidator`, `ReactivateValidator`, `RetireValidator`, `DeleteValidator` are also Ed25519-only and cheap to add. `CreateValidator` / `UpdateValidator` require **BLS12-381** (`voting_key: BlsPublicKey`, `proof_of_knowledge: BlsSignature`). Only `nimiq-validator-activator-go` uses these, and it runs on a validator — which is a consensus-participating node by definition. **Never a node-reduction blocker; BLS is optional and isolated.**

**Revised target: no local node anywhere except actual validators.** The explorer/indexer history need is served by adding a history node to the shared RPC cluster, not by a per-project node.

**Serialization gotcha, must not be guessed:** incoming staking transactions embed the `SignatureProof` *inside* `recipient_data`, and signing content is computed with that field **zeroed first** — `IncomingStakingTransactionData::set_signature_on_data(&data, SignatureProof::default())` then `serialize_content()` (`structs.rs:274-283`). Phase 4's compatibility spike must cover a staking transaction, not only a basic one.

Cost: staking roughly doubles the serialization surface versus basic transactions, and sits on Phase 4's critical path.

---

## 7. Recommended next steps

1. **Now, independent of the SDK:** rotate the leaked mainnet key (§0), delete the `exec.Command` curl fallbacks (S2).
2. Phase 2 — protocol research against the local `core-rs-albatross` checkout at `v1.7.1-28-g35e2a8334`, plus the JSON-RPC spec and `@nimiq/core` JS.
3. Phase 4 gate — the compatibility spike. If Go bytes don't match official fixtures exactly, nothing downstream is worth building.

Repository name: keep **`nimiq-go`** — the repo already exists at `github.com/NimMiniApps/nimiq-go`, is empty apart from `LICENSE` and `.gitignore`, and sits under the same org as NimConnect/NimWorld/NimBomber. Module path `github.com/NimMiniApps/nimiq-go`.
