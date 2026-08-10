package nimiq

import "fmt"

// ParseAndVerify decodes a serialized transaction and verifies its signature
// proof against the expected network.
//
// Use it on any transaction that arrived from outside the process — most
// importantly one signed in a browser by the Nimiq Hub — so its sender,
// recipient, value and hash can be inspected BEFORE it is broadcast, rather
// than broadcasting blind and reading the result back off the chain.
//
// Passing the expected network is required, not optional. A transaction is
// only valid on the network named in its signing content, and accepting
// whatever network the bytes claim would let a transaction prepared for a test
// chain be presented to a mainnet backend as if it were real.
//
// What this does NOT check, because only the application knows them:
//
//   - that the recipient is who you expected to be paid
//   - that the value matches the invoice or trade
//   - that the data payload carries the reference you issued
//   - that you have not already processed this hash
//
// Compare those fields on the returned transaction before acting on it. A
// verified signature only proves the sender authorized these exact bytes; it
// says nothing about whether the bytes are the ones you asked for.
func ParseAndVerify(raw []byte, network NetworkID) (*Transaction, error) {
	tx, err := ParseTransaction(raw)
	if err != nil {
		return nil, err
	}
	if tx.Network != network {
		return nil, fmt.Errorf("nimiq: transaction is for %s, expected %s", tx.Network, network)
	}
	if !tx.Network.IsAlbatross() {
		return nil, fmt.Errorf("nimiq: transaction names non-Albatross network %s", tx.Network)
	}
	proof, err := ParseSignatureProof(tx.Proof)
	if err != nil {
		return nil, fmt.Errorf("nimiq: signature proof: %w", err)
	}
	if err := proof.Verify(tx); err != nil {
		return nil, err
	}
	return tx, nil
}
