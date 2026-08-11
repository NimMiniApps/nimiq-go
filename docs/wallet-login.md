# Server-side wallet login

Verify a Nimiq Hub / Keyguard `signMessage` login in a Go backend using only
`nimiq-go`. The SDK does **not** define challenge text, nonce storage, or
session cookies — those stay in your app.

## Flow

1. **Parse the claimed address** with `ParseAddress`. Spacing and case do not
   matter; compare and store the canonical `Address` (or `Address.String()`).
2. **Issue an app-owned challenge string** (e.g. app name + nonce + expiry).
   Have the client sign that exact byte string via Hub `signMessage`.
3. **Decode the Hub result** with `ParsePublicKey` and `ParseSignature`
   (std base64, base64url, or hex — whatever the client sends).
4. **Verify** with `VerifyMessageFrom(address, publicKey, challenge, signature)`.

```go
addr, err := nimiq.ParseAddress(claimedAddress)
if err != nil {
	return err
}
pub, err := nimiq.ParsePublicKey(publicKeyWire)
if err != nil {
	return err
}
sig, err := nimiq.ParseSignature(signatureWire)
if err != nil {
	return err
}
if err := nimiq.VerifyMessageFrom(addr, pub, []byte(challenge), sig); err != nil {
	return err
}
// Authenticated as addr.
```

## Out of scope

- Challenge / nonce format and replay protection
- HTTP cookies or session stores
- Passkey / ES256 message verification (not in this SDK yet)
