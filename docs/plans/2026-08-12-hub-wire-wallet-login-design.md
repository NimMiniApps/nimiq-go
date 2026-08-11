# Hub Wire Helpers & Server-Side Wallet Login — Design

Date: 2026-08-12. Status: approved.

**Consumer context:** Nimiq Mini Apps catalog (NimiqMiniApps backend) uses `nimiq-go` v0.3.0 for wallet login only. It already consumes `ParseAddress`, `Address.String()`, `AddressFromPublicKey`, and `VerifyMessageFrom`. Hub `public_key` / `signature` decoding (`decodeCryptoBytes`) remains hand-rolled in the catalog.

**Goal:** Another Go service can verify a Hub login signature with only `nimiq-go` imports (plus its own challenge string), without copying base64/hex decode or message-prefix hashing.

## Decisions

- Approach: typed parsers + private shared decoder (not a public `DecodeWireBytes`, not a combined “login” API).
- Root package file `wire.go`; keep cashlink `.` padding out of these parsers.
- Verify path stays `ParseAddress` → app-owned challenge → `ParsePublicKey` / `ParseSignature` → `VerifyMessageFrom`.
- First ship: P0 + P1 as **v0.3.1** (additive, non-breaking).
- Docs: short README blurb **and** `docs/wallet-login.md`.
- Tests: synthetic encoding coverage is the CI backbone; optional real Hub golden; if missing, leave a `TODO` stub — do not block P0.
- P2 / P3 stay in this roadmap as deferred; no passkey or nice-to-have code until a real consumer asks.

## Explicit non-goals

- Challenge / nonce protocol or SIWE-like builders in the SDK
- Session / cookie helpers
- Expanding the catalog to use `rpc`, cashlinks, vesting, staking, or `signer`
- Wrapping more Albatross RPC methods (use `Call`)
- Changing Hub JS / `@nimiq/hub-api`
- Cashlink-style `.` base64url padding in `ParsePublicKey` / `ParseSignature`

---

## P0 — Hub wire helpers

### API

```go
// ParsePublicKey accepts std base64, padded/raw base64url, and optional 0x-hex.
// Length must be 32. Rejects the all-zero key (same policy as SignatureProof).
func ParsePublicKey(s string) (ed25519.PublicKey, error)

// ParseSignature accepts the same encodings. Length must be 64.
// Does not reject all-zero signatures (only length/encoding matter here).
func ParseSignature(s string) ([]byte, error)
```

Unexported: `decodeWireBytes(s string) ([]byte, error)`.

### Encoding rules

1. Trim surrounding whitespace.
2. Try encodings in order. Accept the **first decode whose length matches** the parser’s expected size (32 for pubkey, 64 for signature). A successful decode of the wrong length does **not** stop the search — otherwise hex keys (e.g. 64 hex chars) can falsely bind as base64 of a different byte length.
   - `encoding/base64` StdEncoding, then RawStdEncoding if needed
   - URLEncoding / RawURLEncoding (`=` padding or raw; **no** `.` → `=` cashlink normalization)
   - hex, with optional `0x` / `0X` prefix
3. If no encoding yields the expected length: error (invalid encoding / wrong length).
4. After a length match, `ParsePublicKey` rejects the all-zero key.
5. Return defensive copies so callers cannot mutate shared buffers.
6. Errors prefixed `nimiq:`; distinguish invalid encoding / wrong length, and all-zero public key.

### Layout

| Path | Role |
|---|---|
| `wire.go` | `ParsePublicKey`, `ParseSignature`, `decodeWireBytes` |
| `wire_test.go` | Synthetic encoding matrix, rejection cases, optional Hub golden / TODO stub |

No new module dependencies.

### Acceptance

- Synthetic: `SignMessage` → encode pubkey/sig as std base64, base64url padded, base64url raw, hex, `0x`-hex → parse → `VerifyMessageFrom` succeeds.
- Reject wrong lengths and all-zero pubkey (same policy as proofs).
- Docs snippet: wallet login = `ParseAddress` + `ParsePublicKey` / `ParseSignature` + `VerifyMessageFrom`.
- Catalog can delete local `decodeCryptoBytes` for auth once this ships (follow-up in NimiqMiniApps, not this repo).

---

## P1 — Docs: server-side wallet login recipe

Same PR as P0.

### `docs/wallet-login.md`

Short recipe for mini-app backends:

1. `ParseAddress` — canonicalize; spacing/case irrelevant afterward.
2. Issue an app-owned challenge string (format is app-specific; SDK does not define it).
3. Receive Hub `signMessage` result; `ParsePublicKey` / `ParseSignature`.
4. `VerifyMessageFrom(address, publicKey, challenge, signature)`.

Explicitly out of scope in the doc: nonce store, cookies, challenge text templates.

### `README.md`

Short “Server-side wallet login” blurb (5–10 lines) with the four-call recipe and a link to `docs/wallet-login.md`.

### `CHANGELOG.md`

`[0.3.1]` entry for the parsers and docs.

### Release

Tag **v0.3.1** after land on `main`. Follow-up in NimiqMiniApps: bump module, replace `decodeCryptoBytes` on the auth path.

---

## P2 — ES256 / passkey message verification (deferred)

**Gate:** only when Nimiq Pay / Hub login (or another consumer) needs WebAuthn message verification.

Today `AlgorithmES256` exists for transaction proof decode/inspect; the SDK cannot create or verify ES256 proofs or messages.

When unblocked:

1. Define how signed messages work for passkey accounts (mirror core-rs / Hub).
2. Extend `VerifyMessage` / `VerifyMessageFrom` or add a sibling for ES256.
3. Tests against Hub / Keyguard fixtures.

Do not implement in the v0.3.1 PR. Do not speculate wire formats beyond “mirror upstream when a consumer asks.”

---

## P3 — Nice-to-haves (deferred)

| Idea | Notes |
|---|---|
| Prefer `ParseAddress` in handlers | `MustParseAddress` already exists for constants/tests |
| Address equality helpers | `[20]byte` compare is enough; optional `Equal` is noise |
| Typed encode helpers (`PublicKey.Base64()`) | Only if multiple apps encode the same way |
| Challenge builders | Out of scope — app-specific |
| Session / cookie helpers | Out of scope — not Nimiq |
| Export `DecodeWireBytes` | Only if a second consumer needs a third wire type |

---

## Data flow (login)

```
Hub signMessage result
  address string ──► ParseAddress ──► Address
  public_key str ──► ParsePublicKey ──► ed25519.PublicKey
  signature str ──► ParseSignature ──► []byte
app challenge ──────────────────────────────────┐
                                                ▼
                         VerifyMessageFrom(addr, pub, challenge, sig)
```

Challenge issuance, nonce storage, and session cookies stay in the application.

## Done when

A Go service verifies a Hub login signature using only `github.com/NimMiniApps/nimiq-go` (plus its own challenge string), without copying base64/hex decode or message-prefix hashing.
