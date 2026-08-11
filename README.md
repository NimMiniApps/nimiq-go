# nimiq-go

[![CI](https://github.com/NimMiniApps/nimiq-go/actions/workflows/ci.yml/badge.svg)](https://github.com/NimMiniApps/nimiq-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/NimMiniApps/nimiq-go.svg)](https://pkg.go.dev/github.com/NimMiniApps/nimiq-go)
[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Go SDK for the [Nimiq](https://nimiq.com) Albatross (proof-of-stake) chain: offline addresses, amounts, transaction building and signing, plus an optional JSON-RPC client.

## Install

```bash
go get github.com/NimMiniApps/nimiq-go@v0.3.0
```

Requires **Go 1.25+**.

## Features

- Offline addresses, amounts, basic/extended transactions, and staking constructors
- `signer`: Ed25519 keys, encrypted storage, BIP39 mnemonic import/export
- `rpc`: balances, history, fee estimation, staking reads, `Send`, raw `Call` escape hatch
- Cashlink URL parse/encode and vesting/HTLC creation payload parsing

## Quickstart

Build, sign, and (optionally) broadcast a basic transfer:

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	nimiq "github.com/NimMiniApps/nimiq-go"
	"github.com/NimMiniApps/nimiq-go/rpc"
	"github.com/NimMiniApps/nimiq-go/signer"
)

func main() {
	ctx := context.Background()

	key, err := signer.Generate()
	if err != nil {
		log.Fatal(err)
	}
	recipient := nimiq.MustParseAddress("NQ07 0000 0000 0000 0000 0000 0000 0000 0000")
	fmt.Println("sender", key.Address())

	endpoint := os.Getenv("NIMIQ_RPC")
	if endpoint == "" {
		return
	}
	client, err := rpc.New(endpoint, rpc.WithNetwork(nimiq.NetworkTestAlbatross))
	if err != nil {
		log.Fatal(err)
	}
	hash, err := client.Send(ctx, key, recipient, nimiq.MustParseNIM("1.5"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("broadcast", hash)
}
```

`Send` fills the validity start height from the chain head and the fee from the network minimum. The network is set with `WithNetwork` at construction — never read from the node.

A runnable copy lives in [`examples/basic`](examples/basic).

## Packages

| Package | Role |
|---|---|
| `github.com/NimMiniApps/nimiq-go` | Addresses, amounts, transactions, staking constructors, verification |
| `…/signer` | In-process Ed25519 private keys (`nimiq.Signer`), BIP39 mnemonic import |
| `…/rpc` | JSON-RPC client (no key material; endpoint always required) |

## RPC

Typed wrappers cover common reads and broadcast. For anything else, use the escape hatch — same retries, timeouts, and failover as the wrapped methods:

```go
var peers int
err := client.Call(ctx, "getPeerCount", nil, &peers)
```

Promote a method to a typed wrapper only after two real users ask.

## Networks

| Constant | ID |
|---|---|
| `NetworkTestAlbatross` | 5 |
| `NetworkDevAlbatross` | 6 |
| `NetworkMainAlbatross` | 24 |

Legacy proof-of-work network IDs are rejected at validation time.

## Development

```bash
make check   # tidy, vet, test, lint
make test
make lint
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for PR and release notes. Releases are listed in [CHANGELOG.md](CHANGELOG.md).

## License

[MIT](LICENSE)
