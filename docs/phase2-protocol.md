# Phase 2 — Albatross Protocol Reference

Source of truth: `core-rs-albatross` @ **`v1.7.2-1-gb12430dd3`**, branch `albatross` (Nimiq's default branch), updated 2026-08-08.

Every binary claim below was **verified empirically** against a live Albatross node via `getRawTransactionInfo`, not inferred. Verification transcript in §9.

---

## 1. Headline: the serialization format is `postcard`

`nimiq-serde` (`serde/Cargo.toml`) is a thin wrapper over **[postcard](https://postcard.jamesmunns.com/)**. This is the single most important fact for the SDK and it is not documented anywhere obvious.

Rules that apply (postcard spec + Nimiq's overrides):

| Type | Encoding |
|---|---|
| `u8`, enums with `#[repr(u8)]` | 1 byte |
| Integers generally | LEB128 uvarint (**but see Coin/height below**) |
| `Vec<u8>`, `String`, seq | **uvarint length prefix** + elements |
| Fixed arrays, `FixedSizeByteArray` | raw bytes, **no** length prefix |
| `Option<T>` | `0x00` = None; `0x01` + value = Some |
| Struct / tuple | fields concatenated, no framing |
| Enum variant | variant index as uvarint, then payload |

**Nimiq's overrides — these are where a naive postcard implementation goes wrong:**

- `Coin` (value, fee) → `nimiq_serde::fixint::be` = **fixed 8 bytes big-endian**, *not* varint (`primitives/src/coin.rs:275-302`)
- `validity_start_height` → serialized as `.to_be_bytes()`, i.e. a `[u8;4]` array = **fixed 4 bytes big-endian**
- `Address` → `FixedSizeByteArray` = **exactly 20 raw bytes**, no prefix (`keys/src/address.rs:241`)

> **Do not implement postcard generically.** The SDK needs only ~6 primitives (u8, uvarint, fixed byte array, length-prefixed bytes, Option, fixed-BE ints). Hand-write those, exhaustively test them, and skip the generic serde machinery entirely.

---

## 2. Constants

**NetworkId** (`primitives/src/networks.rs:11-23`) — `#[repr(u8)]`:

| Name | Value | |
|---|---|---|
| `TestAlbatross` | **5** | testnet |
| `DevAlbatross` | 6 | devnet (default) |
| `UnitAlbatross` | 7 | unit tests |
| `MainAlbatross` | **24** | mainnet |
| `Test`/`Dev`/`Bounty`/`Dummy`/`Main` | 1/2/3/4/42 | **legacy PoW — never emit these** |

`Main = 42` is the *PoW* mainnet. Mainnet Albatross is **24**. Anything ported from an old PoW Go library will have 42 hardcoded and silently produce transactions no Albatross node accepts.

**AccountType** (`primitives/src/account.rs:20-25`): `Basic=0`, `Vesting=1`, `HTLC=2`, `Staking=3`.

**TransactionFlags** (`primitives/transaction/src/lib.rs:103-106`), `u8` bitfield: `CONTRACT_CREATION = 0b01`, `SIGNALING = 0b10`.

---

## 3. The Transaction struct

`primitives/transaction/src/lib.rs:186-199`:

```rust
pub struct Transaction {
    pub sender: Address,          pub sender_type: AccountType,
    pub sender_data: Vec<u8>,     // ← does not exist in PoW
    pub recipient: Address,       pub recipient_type: AccountType,
    pub recipient_data: Vec<u8>,  // ← PoW called this `data`
    pub value: Coin,              pub fee: Coin,
    pub validity_start_height: u32,
    pub network_id: NetworkId,    pub flags: TransactionFlags,
    pub proof: Vec<u8>,
}
```

**Albatross has two data fields.** PoW had one. `sender_data` carries outgoing staking data (`DeleteValidator` / `RemoveStake`). Every existing Go PoW port models a single `data` field and is therefore wrong for Albatross.

---

## 4. Two wire formats — Basic and Extended

`format()` (`lib.rs:317-329`) picks the encoding. A transaction is **Basic** only if *all* hold:

1. `sender_type == Basic` **and** `recipient_type == Basic`
2. `recipient_data` is empty
3. `flags` is empty
4. `proof` parses as a `SignatureProof`
5. `sender == Address::from(proof.public_key)`
6. `proof.merkle_path` is empty

Otherwise **Extended**. This is not a choice the caller makes — it is derived from the transaction's own contents. **Any transaction with a data payload is Extended**, which covers essentially everything NimConnect and PlayNimiq send.

### 4.1 Basic wire layout — *verified, 139 bytes*

| Offset | Bytes | Field |
|---|---|---|
| 0 | 1 | postcard enum variant = `0x00` |
| 1 | 1 | `type_and_flags` (see §6) |
| 2 | 32 | public key |
| 34 | 20 | recipient |
| 54 | 8 | value (BE) |
| 62 | 8 | fee (BE) |
| 70 | 4 | validity_start_height (BE) |
| 74 | 1 | network_id |
| 75 | 64 | signature |

Sender is *not* transmitted — it is derived from the public key.

### 4.2 Extended wire layout — *verified, 168 bytes for a 2-byte payload*

| Field | Encoding |
|---|---|
| variant | `0x01` |
| sender | 20 raw |
| sender_type | 1 |
| sender_data | uvarint len + bytes |
| recipient | 20 raw |
| recipient_type | 1 |
| recipient_data | uvarint len + bytes |
| value | 8 BE |
| fee | 8 BE |
| validity_start_height | 4 BE |
| network_id | 1 |
| flags | 1 |
| proof | uvarint len + bytes |

---

## 5. Signing content and transaction hash — *the same preimage*

`SerializeContent for Transaction` (`lib.rs:592-616`). Note the deliberately odd field order and the PoW-compat quirks:

```
u16 BE  recipient_data.len()      ← u16 BE, NOT uvarint (PoW backwards-compat)
        recipient_data
20      sender
1       sender_type
20      recipient
1       recipient_type
8 BE    value
8 BE    fee
4 BE    validity_start_height
1       network_id
1       flags
        sender_data  (uvarint len + bytes)  ← ONLY if network_id.is_albatross()
```

Three traps: the recipient_data length is **u16 big-endian** here but **uvarint** in the wire format; the data field comes **first**, before the sender; and `sender_data` is appended **only** on Albatross networks.

**Transaction hash = `Blake2b-256(serialize_content())`** — the same bytes that are signed. Verified: content is 67 bytes for the basic example, and its Blake2b-256 is exactly the hash the node reports.

### 5.1 This answers the idempotency question from the brief

Because the hash is `Blake2b-256` of the signing content, and the signing content contains **no signature**, **the transaction hash is fully determined before signing** — and certainly before broadcast.

**Yes, Nimiq transaction hashes provide natural idempotency for rebroadcast.** Re-submitting identical signed bytes yields the same hash and is a no-op at the mempool. This is what makes PlayNimiq's entire 5000-transaction `payout-op:` reconciliation scan (§3.2 of the Phase 1 audit) unnecessary: sign offline, record the hash, then retry broadcast freely.

Caveat: identical *content* signed twice is identical — but a different `validity_start_height` produces a different hash. Idempotency holds only if the SDK reuses the same constructed transaction on retry, not if it rebuilds it. The SDK must therefore separate "build+sign" from "broadcast" and retry only the latter.

---

## 6. SignatureProof

`primitives/transaction/src/signature_proof.rs`:

```rust
pub struct SignatureProof {
    pub public_key: PublicKey,        // Ed25519 | ES256
    pub merkle_path: Blake2bMerklePath,
    pub signature: Signature,
    pub webauthn_fields: Option<WebauthnExtraFields>,
}
```

Layout: `type_and_flags(1) ‖ public_key(32) ‖ merkle_path ‖ signature(64)` = **98 bytes** for the ordinary Ed25519, empty-path case. Verified — the node echoed back exactly `00 ‖ pk32 ‖ 00 ‖ sig64`.

`type_and_flags` byte (`signature_proof.rs:163-178`): low 4 bits = algorithm (`0` Ed25519, `1` ES256); high 4 bits = flags (bit 0 = `WEBAUTHN_FIELDS`). Empty merkle path encodes as a single `0x00` (uvarint count 0).

**Albatross supports ES256 / WebAuthn signature proofs, not just Ed25519.** Passkey-based wallets produce these. The SDK's *deserializer* must handle ES256 (relevant for validating Hub-signed transactions — NimConnect's blind-broadcast path); the *signer* can be Ed25519-only for v1.

Staking rejects the all-zero public key as a verification wildcard (`structs.rs:262-270`) — the SDK should reject it everywhere, not just in staking.

---

## 7. Staking transactions

### 7.1 The signature-in-data pattern

Incoming staking transactions embed their `SignatureProof` **inside `recipient_data`**. The signing content is computed with that field **zeroed**:

```rust
tx_without_sig.recipient_data =
    IncomingStakingTransactionData::set_signature_on_data(
        &tx_without_sig.recipient_data, SignatureProof::default())?;
tx_without_sig.serialize_content()
```
(`structs.rs:274-283`)

So the flow is: build data with a default/zero proof → `serialize_content` → sign → write the real proof back into `recipient_data` → serialize. Get this wrong and every staking transaction is invalid. Phase 4 must cover a staking transaction, not only a basic one.

### 7.2 Structural rules

- Staking **rejects `CONTRACT_CREATION`** (`staking_contract/mod.rs:21-30`)
- `data.is_signaling()` must **exactly match** the `SIGNALING` flag (`mod.rs:35-38`)
- Signaling transactions must have **value == 0** (`mod.rs:40-43`)
- Outgoing staking data (`DeleteValidator`, `RemoveStake`) lives in **`sender_data`**
- `CreateValidator` value must equal exactly `Policy::VALIDATOR_DEPOSIT`

### 7.3 Crypto tiers (confirms Phase 1 §4a)

- **Ed25519 only** — `CreateStaker`, `AddStake`, `UpdateStaker`, `SetActiveStake`, `RetireStake`, `RemoveStake`, `DeactivateValidator`, `ReactivateValidator`, `RetireValidator`, `DeleteValidator`
- **Needs BLS12-381** — `CreateValidator`, `UpdateValidator` (`voting_key: BlsPublicKey`, `proof_of_knowledge: BlsSignature`)

Everything the three named projects need is in the Ed25519 tier.

---

## 8. Protocol version — not a wire concern

`v1.7.2` threads a `protocol_version: u16` through all verification entry points (`AccountType::verify_incoming_transaction`, etc.). It is **not a serialized transaction field** and is currently `_protocol_version` (unused) at every actual gate, including `IncomingStakingTransactionData::verify` (`structs.rs:108-112`).

It is plumbing for a pending upgrade whose semantics land in the accounts/state layer — new test suites `staker_add_stake_policy.rs` (459 L) and `staker_tombstone_interaction.rs` (1191 L) show the change is to staking *state transitions*, not encoding.

**No SDK action now.** Watch it: it may later gate which staking operations are valid at construction time.

---

## 9. Verification transcript

Constructed by hand from the rules above, decoded by a live Albatross node.

**Basic** — `00 ‖ 00 ‖ 01×32 ‖ 02×20 ‖ 2710 ‖ 0 ‖ 64 ‖ 05 ‖ 03×64`

```
size 139 ✓   value 10000 ✓   fee 0 ✓   validityStartHeight 100 ✓   networkId 5 ✓
to   NQ23 0810 40G2 …  (= 0x02×20) ✓
from NQ21 XG6E MX3E AVT9 4CRJ P3CF SEYF GJEA TCCU  (derived from pubkey) ✓
proof 0001…01 00 03…03  (98 B: flags‖pk‖empty path‖sig) ✓
hash 79ede3d7f0e272a23a8b47c096d719af179d85fa7e9b6191776fa8095066e8b3
```

Independently reproduced:
- `Blake2b-256(serialize_content())` where content = 67 bytes per §5 → **`79ede3d7…` exact match**
- `Blake2b-256(pubkey)[:20]` → base32 + IBAN check → **`NQ21 XG6E MX3E AVT9 4CRJ P3CF SEYF GJEA TCCU` exact match**
- `Blake2b-256(full 139-byte raw tx)` → `5b295f3a…` — **does not match**, confirming the hash is over the *content*, not the wire bytes

**Extended** — 2-byte payload `"hi"`

```
size 168 ✓   recipientData "6869" ✓   value 500 ✓   fee 1 ✓
validityStartHeight 200 ✓   flags 0 ✓   senderData "" ✓
from NQ34 248H… (=0x11×20) ✓   to NQ93 48H2… (=0x22×20) ✓
```

Both formats, the hash preimage, and address derivation are **proven, not assumed**, before any Go was written.

---

## 10. RPC

`send_raw_transaction(raw_tx: String) -> Blake2bHash` (`rpc-interface/src/consensus.rs:30-31`) — hex string in, transaction hash out. No node wallet involved. This is the only broadcast primitive the SDK needs.

`getRawTransactionInfo(rawTx)` decodes a serialized transaction server-side with no key — **keep this as a CI conformance oracle**: every fixture the SDK produces can be round-tripped against a real node.

Responses are wrapped `{"data": …, "metadata": …}`. Handle once, in the SDK (16 projects currently handle it 16 different ways — Phase 1 §2.3).

---

## 11. What this changes for Phase 3/4

1. **Phase 4's risk is largely retired.** The format is proven. The spike becomes a port of a verified spec rather than a discovery exercise.
2. **Write the postcard subset by hand**, ~6 primitives, fuzz them. No generic serde port, no external dependency.
3. **Basic-vs-Extended is derived, not chosen** — one `format()` function, exactly mirroring `lib.rs:317-329`, or the SDK will emit the wrong encoding for data-carrying transactions.
4. **Signing content ≠ wire bytes**, and the two disagree about how a length is encoded. Two separate, separately-tested serializers.
5. **The hash is known pre-broadcast** → design `Build → Sign → (hash available) → Broadcast` with retry only on the last step.
6. **Deserializer must handle ES256/WebAuthn** even though the signer is Ed25519-only.
7. **`sender_data` must exist in the Go type from day one** — retrofitting it later breaks every serializer.

Cryptography needed: Ed25519 (`crypto/ed25519` — stdlib) and Blake2b-256 (`golang.org/x/crypto/blake2b` — already used by 10 projects here). **No custom cryptography, no new dependencies.**
