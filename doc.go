// Package nimiq is a Go SDK for the Nimiq Albatross (proof-of-stake) chain.
//
// It covers offline address handling, amounts, transaction construction and
// Ed25519 signing, plus optional JSON-RPC access via the rpc subpackage.
// Private-key material lives in the signer subpackage so read-only services
// never need to link key-handling code.
//
// Typical flow:
//
//  1. Build a [Transaction] with NewBasicTransaction (or a staking constructor).
//  2. Sign it with [SignTransaction] and a [Signer] (for example signer.PrivateKey).
//  3. Broadcast with rpc.Client.SendTransaction when an endpoint is available.
//
// Validity start heights come from the caller (usually rpc.Client.BlockNumber).
// Transaction construction itself never opens a network connection.
package nimiq
