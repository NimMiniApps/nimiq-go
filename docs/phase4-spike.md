# Phase 4 — Compatibility Spike: **PASSED**

The gate from the brief: prove Go produces byte-identical results to the official implementation before building a large API.

## Result

All ten steps pass, for **both** wire formats, with real Ed25519 signatures. Every expected value was produced by a live Albatross node via `getRawTransactionInfo`, not by this implementation.

| Step | Status |
|---|---|
| 1. Load a test private key | ✅ `signer.ParsePrivateKeyHex` |
| 2. Derive the expected address | ✅ node-confirmed |
| 3. Construct a basic transaction | ✅ offline, no endpoint |
| 4. Serialize the signing content | ✅ 67 bytes, node-confirmed |
| 5. Sign | ✅ `crypto/ed25519` |
| 6. Construct the BasicAccount proof | ✅ 98 bytes, node-confirmed |
| 7. Serialize the full transaction | ✅ both formats byte-exact |
| 8. Calculate the hash | ✅ node-confirmed |
| 9. Compare with the official implementation | ✅ **exact match** |
| 10. Broadcast on a test network | ✅ **live on test-albatross, macro-final** |

### Node-verified end to end

A data-carrying transaction, the shape NimConnect and PlayNimiq actually send:

```
sender    NQ65 9M6T V4BM 8JQ7 J8HL H9KB KH5M LNJY 6JLY
value     2.5 NIM        → node reported 250000 Luna      ✓
fee       0.00002 NIM    → node reported 2 Luna           ✓
data      "payout-op:spike-1" → 7061796f75742d6f703a…     ✓
height    987654                                          ✓
hash      805d2bf9d2fadb463887fa628f1938699edf64eaa2c8b5b6fd4094d2ff78223b  ✓
size      183 bytes, extended format                       ✓
```

Go computed that hash independently; the node agreed.

## Step 10 — closed, live on testnet

Endpoint `https://rpc-testnet.nimiqscan.com/`, faucet `https://faucet.pos.nimiq-testnet.com/tapit`. **No node was run.**

```
sender      NQ22 SR01 QG8A LP4L XANX CETR B7QD N0G7 6Q20   (faucet-funded)
built+signed OFFLINE
  hash      bf20970f4c5b4408f3d036697186174eec796ac92536fdbf9f077e87c2f7a5ab
  size      180 bytes, extended format
BROADCAST   node returned the identical hash              ✓
  included  true
  final     true   (block 8125622, macro 8125650)
  data      "nimiq-go spike"
```

### Idempotency, proven rather than assumed

The same signed bytes were broadcast three times:

```
broadcast #1  452ec93e…  err=<nil>
broadcast #2  452ec93e…  err=<nil>   (identical bytes)
broadcast #3  452ec93e…  err=<nil>   (identical bytes)

balance before  109999 NIM
balance after   109997 NIM
total debited        2 NIM   for a 2 NIM transfer
```

Same hash returned every time, no error, **debited exactly once**. This is the empirical basis for deleting PlayNimiq's 5000-transaction `payout-op:` reconciliation scan: a payout service can retry a broadcast freely as long as it resends the *same serialized bytes* rather than rebuilding the transaction.

## Four malleability bugs found by fuzzing

The parser was fuzzed to 62.7M executions. The first three findings arrived within seconds. All are the same class: **more than one byte string decoding to the same transaction**, which breaks validate-then-broadcast — the bytes a caller inspects and approves would not be the bytes it re-serializes and sends.

| # | Finding | Fix |
|---|---|---|
| 1 | Extended transaction with a **zero-length proof** parsed cleanly — an unsigned transaction presenting a plausible sender, recipient and value | reject at parse |
| 2 | **Undefined flag bits** in the proof's type-and-flags byte were silently dropped on re-serialization | reject unknown flags |
| 3 | **ES256** algorithm byte parsed with Ed25519 key/signature sizes, yielding a wrong public key | reject — sizes differ |
| 4 | **Non-canonical varint**: `0xe6 0x00` and `0x66` both decode to 102 | accept only the minimal encoding |

All four are locked in as explicit regression tests (`malleability_test.go`), so they hold without a checked-in fuzz corpus.

## State

```
go test ./...     27 tests, 0 failures (root + rpc)
go test -race     clean
go vet            clean
gofmt -l          clean
fuzz              62.7M execs (tx), 4.4M (address), 16.2M (amount) — clean
dependencies      golang.org/x/crypto/blake2b only
```

## Layout

```
nimiq-go/
  address.go       Address: parse, validate, decode to 20 bytes, format
  amount.go        Luna: integer-only, ParseNIM with no floating point
  network.go       NetworkID, IsAlbatross (rejects proof-of-work IDs)
  transaction.go   Transaction, both serializers, Format(), Hash(), parser
  proof.go         SignatureProof: build, serialize, parse, verify
  serialize.go     the postcard subset Nimiq actually uses
  signer.go        Signer interface + SignTransaction
  signer/          PrivateKey — the only package holding key material
  rpc/             JSON-RPC client, typed errors, Albatross finality
```

## Bug found in NimConnect while building this

`getLastMacroBlock` is a **policy** method: it takes a height and returns a bare integer (`rpc-interface/src/policy.rs:51`). NimConnect calls it with no parameters and decodes the result as `{"number": …}` (`backend/nimiq_rpc.go:207`). Against a real node that fails with `-32602 Invalid params`, so its macro-finality gating cannot be working. Worth checking before the migration phase.

Read-only consumers import `nimiq` and never link key handling. That is the security boundary from Phase 3, and it holds: nothing in the root package touches a private key.
