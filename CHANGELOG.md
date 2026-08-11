# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.3.1] - 2026-08-12

Hub wire helpers for server-side wallet login.

### Added

- `ParsePublicKey` and `ParseSignature` for Hub/Keyguard encodings (std base64, base64url, optional 0x-hex)
- Server-side wallet login recipe (`docs/wallet-login.md` and README blurb)

## [0.3.0] - 2026-08-11

Application surface for wallets and mini apps: restore from backup, send without boilerplate, cashlinks, and vesting.

### Added

- Raw JSON-RPC escape hatch (`rpc.Client.Call`) for methods this SDK does not wrap
- BIP39 mnemonic import/export in `signer` (`TypeOf`, `FromMnemonic`, `FromMnemonicType`, `PrivateKey.Mnemonic`), including legacy CRC-8 detection
- `rpc.Client.Send` convenience transfer with `WithNetwork` (network is app intent, never read from the node)
- Cashlink URL parse/encode (`ParseCashlink`, `Cashlink.URL`) matching hub.nimiq.com payload quirks
- Vesting creation payload parsing (`ParseVestingCreation`) for short, medium, and full encodings

## [0.2.0] - 2026-08-11

Reliability, authentication, and staking-read APIs needed for application migrations.

### Added

- Signed messages (`MessageHash`, `SignMessage`, `VerifyMessage`, `VerifyMessageFrom`) matching Albatross wallet message prep
- Address history with proven completeness (`GetTransactionsByAddress`, `FindTransactionByData`) and fail-closed reconciliation
- Fee estimation (`EstimateFee`) from network min fee per byte
- HTLC creation payload parsing (`ParseHTLCCreation`)
- Pre-broadcast decode and proof verification (`ParseAndVerify`) with expected-network binding
- Staker and validator reads (`GetStaker`, `GetValidator`) including balance buckets and eligibility helpers
- RPC endpoint failover (`WithFallbackEndpoints`) on transport failures and 5xx only
- Chain policy constants and expiry helpers (`Policy`, `IsExpired`, `BlocksUntilExpiry`)
- Encrypted key storage in `signer` (scrypt + AES-256-GCM)

## [0.1.0] - 2026-08-08

Initial public release of the Nimiq Albatross Go SDK.

### Added

- Offline address parsing/encoding with IBAN checksum (`ParseAddress`, `AddressFromPublicKey`)
- Exact NIM ↔ Luna conversion without floating point (`ParseNIM`, `Luna`)
- Basic and extended transaction serialization, hashing, and Ed25519 signature proofs
- `SignTransaction` / `Signer` interface; in-process keys in `signer`
- Staking transaction constructors (create/add/update staker, set active stake, retire, remove)
- JSON-RPC client (`rpc`) for block height, balances, send/confirm transactions
- Example under `examples/basic`
- CI, golangci-lint, Dependabot, and tag-driven GitHub Releases

[0.3.0]: https://github.com/NimMiniApps/nimiq-go/releases/tag/v0.3.0
[0.2.0]: https://github.com/NimMiniApps/nimiq-go/releases/tag/v0.2.0
[0.1.0]: https://github.com/NimMiniApps/nimiq-go/releases/tag/v0.1.0
