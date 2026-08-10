# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

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

[0.2.0]: https://github.com/NimMiniApps/nimiq-go/releases/tag/v0.2.0
[0.1.0]: https://github.com/NimMiniApps/nimiq-go/releases/tag/v0.1.0
