// Basic offline build/sign demo for nimiq-go.
//
// Set NIMIQ_RPC to a JSON-RPC endpoint to also broadcast the signed
// transaction. Without it, the program only prints the hash and exits.
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

	validityStart := uint32(4242)
	endpoint := os.Getenv("NIMIQ_RPC")
	if endpoint != "" {
		client, err := rpc.New(endpoint)
		if err != nil {
			log.Fatal(err)
		}
		height, err := client.BlockNumber(ctx)
		if err != nil {
			log.Fatal(err)
		}
		validityStart = height
	}

	tx, err := nimiq.NewBasicTransaction(
		sender,
		recipient,
		nimiq.MustParseNIM("1.5"),
		nimiq.MustParseNIM("0.00001"),
		validityStart,
		nimiq.NetworkTestAlbatross,
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := nimiq.SignTransaction(ctx, key, tx); err != nil {
		log.Fatal(err)
	}

	fmt.Println("sender", sender)
	fmt.Println("hash  ", tx.Hash())

	if endpoint == "" {
		fmt.Println("set NIMIQ_RPC to broadcast")
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
