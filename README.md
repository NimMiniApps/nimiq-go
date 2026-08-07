# nimiq-go

[![CI](https://github.com/NimMiniApps/nimiq-go/actions/workflows/ci.yml/badge.svg)](https://github.com/NimMiniApps/nimiq-go/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/NimMiniApps/nimiq-go.svg)](https://pkg.go.dev/github.com/NimMiniApps/nimiq-go)
[![Go Version](https://img.shields.io/badge/Go-1.22+-00ADD8?logo=go)](https://go.dev/dl/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Go SDK for the [Nimiq](https://nimiq.com) Albatross (proof-of-stake) chain: offline addresses, amounts, transaction building and signing, plus an optional JSON-RPC client.

## Install

```bash
go get github.com/NimMiniApps/nimiq-go@v0.1.0
```

Requires **Go 1.22+**.

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
	sender := key.Address()
	recipient := nimiq.MustParseAddress("NQ07 0000 0000 0000 0000 0000 0000 0000 0000")

	// Offline: no RPC required to build or sign.
	tx, err := nimiq.NewBasicTransaction(
		sender, recipient,
		nimiq.MustParseNIM("1.5"),
		nimiq.MustParseNIM("0.00001"),
		4242, // validity start height — usually from rpc.Client.BlockNumber
		nimiq.NetworkTestAlbatross,
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := nimiq.SignTransaction(ctx, key, tx); err != nil {
		log.Fatal(err)
	}
	fmt.Println("hash", tx.Hash())

	endpoint := os.Getenv("NIMIQ_RPC")
	if endpoint == "" {
		return
	}
	client, err := rpc.New(endpoint)
	if err != nil {
		log.Fatal(err)
	}
	hash, err := client.SendTransaction(ctx, tx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("broadcast", hash)
}
```

A runnable copy lives in [`examples/basic`](examples/basic).

## Packages

| Package | Role |
|---|---|
| `github.com/NimMiniApps/nimiq-go` | Addresses, amounts, transactions, staking constructors, verification |
| `…/signer` | In-process Ed25519 private keys (`nimiq.Signer`) |
| `…/rpc` | JSON-RPC client (no key material; endpoint always required) |

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
