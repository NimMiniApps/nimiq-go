// Basic offline build/sign demo for nimiq-go.
//
// Set NIMIQ_RPC to a JSON-RPC endpoint to also broadcast via Client.Send.
// Without it, the program only prints the sender address and exits.
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
		fmt.Println("set NIMIQ_RPC to broadcast")
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
