# Hub Wire Helpers & Wallet Login Docs Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Ship `ParsePublicKey` / `ParseSignature` plus a server-side wallet-login recipe so Go backends can verify Hub `signMessage` logins with only `nimiq-go` (v0.3.1).

**Architecture:** Root-package `wire.go` with two typed parsers over an unexported `decodeWireBytes` that tries std base64, base64url (padded/raw), then optional `0x`-hex — accepting the first decode of the expected length. No cashlink `.` padding. Login verification stays `ParseAddress` → app challenge → parse → `VerifyMessageFrom`. Docs: README blurb + `docs/wallet-login.md`. P2/P3 out of this plan (see design).

**Tech Stack:** Go 1.25, `crypto/ed25519`, `encoding/base64`, `encoding/hex` (stdlib only).

**Design:** `docs/plans/2026-08-12-hub-wire-wallet-login-design.md`

---

### Task 1: Failing tests for wire parsers

**Files:**
- Create: `wire_test.go`

**Step 1: Write the failing tests**

```go
package nimiq

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"
)

func TestParsePublicKeyEncodings(t *testing.T) {
	s, addr := messageSigner(t)
	signed, err := SignMessage(context.Background(), s, []byte("login challenge"))
	if err != nil {
		t.Fatal(err)
	}
	pub := []byte(signed.PublicKey)

	encodings := map[string]string{
		"std":       base64.StdEncoding.EncodeToString(pub),
		"url":       base64.URLEncoding.EncodeToString(pub),
		"rawurl":    base64.RawURLEncoding.EncodeToString(pub),
		"hex":       hex.EncodeToString(pub),
		"0x-hex":    "0x" + hex.EncodeToString(pub),
		"spaced":    "  " + base64.StdEncoding.EncodeToString(pub) + "\n",
	}
	for name, enc := range encodings {
		got, err := ParsePublicKey(enc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, pub) {
			t.Fatalf("%s: bytes mismatch", name)
		}
		if err := VerifyMessageFrom(addr, got, []byte("login challenge"), signed.Signature); err != nil {
			t.Fatalf("%s: VerifyMessageFrom: %v", name, err)
		}
	}
}

func TestParseSignatureEncodings(t *testing.T) {
	s, addr := messageSigner(t)
	msg := []byte("login challenge")
	signed, err := SignMessage(context.Background(), s, msg)
	if err != nil {
		t.Fatal(err)
	}
	sig := signed.Signature

	encodings := map[string]string{
		"std":    base64.StdEncoding.EncodeToString(sig),
		"url":    base64.URLEncoding.EncodeToString(sig),
		"rawurl": base64.RawURLEncoding.EncodeToString(sig),
		"hex":    hex.EncodeToString(sig),
		"0x-hex": "0X" + hex.EncodeToString(sig),
	}
	for name, enc := range encodings {
		got, err := ParseSignature(enc)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got, sig) {
			t.Fatalf("%s: bytes mismatch", name)
		}
		if err := VerifyMessageFrom(addr, signed.PublicKey, msg, got); err != nil {
			t.Fatalf("%s: VerifyMessageFrom: %v", name, err)
		}
	}
}

func TestParsePublicKeyRejects(t *testing.T) {
	cases := map[string]string{
		"empty":      "",
		"garbage":    "!!!not-encoding!!!",
		"short-b64":  base64.StdEncoding.EncodeToString(make([]byte, 31)),
		"long-b64":   base64.StdEncoding.EncodeToString(make([]byte, 33)),
		"zero-key":   base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"dot-pad":    strings.ReplaceAll(base64.URLEncoding.EncodeToString(make([]byte, 32)), "=", "."),
	}
	for name, in := range cases {
		if _, err := ParsePublicKey(in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseSignatureRejects(t *testing.T) {
	cases := map[string]string{
		"empty":     "",
		"garbage":   "###",
		"short-b64": base64.StdEncoding.EncodeToString(make([]byte, 63)),
		"long-b64":  base64.StdEncoding.EncodeToString(make([]byte, 65)),
	}
	for name, in := range cases {
		if _, err := ParseSignature(in); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParsePublicKeyDefensiveCopy(t *testing.T) {
	pub := make([]byte, 32)
	pub[0] = 1
	enc := base64.StdEncoding.EncodeToString(pub)
	got, err := ParsePublicKey(enc)
	if err != nil {
		t.Fatal(err)
	}
	got[0] ^= 0xff
	again, err := ParsePublicKey(enc)
	if err != nil {
		t.Fatal(err)
	}
	if again[0] != 1 {
		t.Fatal("ParsePublicKey must return a defensive copy")
	}
}

// TODO: Hub golden vector — paste a real Hub/Keyguard signMessage result
// (message + address + base64 pubkey + base64 sig) when available.
func TestParseWireHubGoldenVector(t *testing.T) {
	t.Skip("TODO: Hub golden vector")
}
```

**Step 2: Run tests to verify they fail**

Run: `go test -run 'TestParse(PublicKey|Signature|Wire)' .`

Expected: FAIL — `ParsePublicKey` / `ParseSignature` undefined.

**Step 3: Commit**

```bash
git add wire_test.go
git commit -m "$(cat <<'EOF'
test: add failing coverage for Hub wire parsers

EOF
)"
```

---

### Task 2: Implement `ParsePublicKey` and `ParseSignature`

**Files:**
- Create: `wire.go`
- Test: `wire_test.go` (from Task 1)

**Step 1: Implement `wire.go`**

```go
package nimiq

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

// ParsePublicKey decodes an Ed25519 public key from Hub/Keyguard wire forms:
// standard base64, padded or raw base64url, or hex with an optional 0x prefix.
//
// The key must be exactly 32 bytes. The all-zero public key is rejected (same
// policy as SignatureProof). Cashlink-style '.' padding is not accepted.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	raw, err := decodeWireBytes(s, ed25519PublicKeySize)
	if err != nil {
		return nil, fmt.Errorf("nimiq: public key: %w", err)
	}
	if isAllZero(raw) {
		return nil, fmt.Errorf("nimiq: the all-zero public key is not a valid signer")
	}
	return ed25519.PublicKey(raw), nil
}

// ParseSignature decodes a 64-byte Ed25519 signature from the same encodings
// as ParsePublicKey.
func ParseSignature(s string) ([]byte, error) {
	raw, err := decodeWireBytes(s, ed25519SignatureSize)
	if err != nil {
		return nil, fmt.Errorf("nimiq: signature: %w", err)
	}
	return raw, nil
}

// decodeWireBytes tries Hub signMessage encodings and returns a defensive copy
// of the first decode whose length equals want.
func decodeWireBytes(s string, want int) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty")
	}
	try := []func(string) ([]byte, error){
		base64.StdEncoding.DecodeString,
		base64.RawStdEncoding.DecodeString,
		base64.URLEncoding.DecodeString,
		base64.RawURLEncoding.DecodeString,
		decodeWireHex,
	}
	var lastErr error
	sawWrongLen := false
	for _, dec := range try {
		raw, err := dec(s)
		if err != nil {
			lastErr = err
			continue
		}
		if len(raw) != want {
			sawWrongLen = true
			continue
		}
		out := make([]byte, len(raw))
		copy(out, raw)
		return out, nil
	}
	if sawWrongLen {
		return nil, fmt.Errorf("decoded length is not %d bytes", want)
	}
	if lastErr != nil {
		return nil, fmt.Errorf("invalid encoding: %w", lastErr)
	}
	return nil, fmt.Errorf("invalid encoding")
}

func decodeWireHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	// hex.DecodeString rejects odd length; also reject empty after prefix trim.
	if s == "" {
		return nil, fmt.Errorf("empty hex")
	}
	return hex.DecodeString(s)
}
```

Notes:
- Reuse `ed25519PublicKeySize`, `ed25519SignatureSize`, and `isAllZero` from `proof.go` (same package).
- Do **not** normalize `.` → `=` (cashlink quirk stays in `cashlink.go`).
- Length-matched search avoids hex keys being rejected after a wrong-length base64 hit.

**Step 2: Run tests to verify they pass**

Run: `go test -run 'TestParse(PublicKey|Signature|Wire)' .`

Expected: PASS for all except `TestParseWireHubGoldenVector` (skipped).

**Step 3: Full package check**

Run: `go test .`

Expected: PASS.

**Step 4: Commit**

```bash
git add wire.go wire_test.go
git commit -m "$(cat <<'EOF'
feat: add ParsePublicKey and ParseSignature for Hub wire forms

Accept std base64, base64url, and optional 0x-hex so backends can
verify signMessage logins without local decode glue.
EOF
)"
```

---

### Task 3: Wallet-login docs (P1)

**Files:**
- Create: `docs/wallet-login.md`
- Modify: `README.md` (add section after Packages or before RPC)
- Modify: `CHANGELOG.md` (add `[0.3.1]` at top)

**Step 1: Write `docs/wallet-login.md`**

```markdown
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
```

**Step 2: Add README blurb**

Insert after the Packages table (before `## RPC`):

```markdown
## Server-side wallet login

Hub `signMessage` auth is four calls — no RPC required:

```go
addr, _ := nimiq.ParseAddress(claimed)
pub, _ := nimiq.ParsePublicKey(publicKeyWire)   // base64 / base64url / hex
sig, _ := nimiq.ParseSignature(signatureWire)
err := nimiq.VerifyMessageFrom(addr, pub, []byte(challenge), sig)
```

Challenge text, nonces, and sessions are app-owned. Full recipe:
[`docs/wallet-login.md`](docs/wallet-login.md).
```

(Fix markdown fence nesting when editing — use indented fence or adjust so the inner Go block does not break the outer README fence.)

**Step 3: CHANGELOG entry**

Prepend:

```markdown
## [0.3.1] - 2026-08-12

Hub wire helpers for server-side wallet login.

### Added

- `ParsePublicKey` and `ParseSignature` for Hub/Keyguard encodings (std base64, base64url, optional 0x-hex)
- Server-side wallet login recipe (`docs/wallet-login.md` and README blurb)
```

**Step 4: Sanity-check docs render locally**

Run: `go test .` (still green) and skim README for broken fences.

**Step 5: Commit**

```bash
git add docs/wallet-login.md README.md CHANGELOG.md
git commit -m "$(cat <<'EOF'
docs: add server-side wallet login recipe for v0.3.1

EOF
)"
```

---

### Task 4: Release cut notes (no tag from this plan unless asked)

**Files:** none required beyond Task 3 CHANGELOG.

**Step 1: Verify**

Run: `make check` (or `go test ./...` + lint if Makefile available).

Expected: all green.

**Step 2: Stop before tagging**

Do **not** `git tag v0.3.1` or push unless the user explicitly asks. Hand off:

1. Open PR / land on `main`.
2. Tag `v0.3.1` when ready.
3. Follow-up in NimiqMiniApps: bump module, delete local `decodeCryptoBytes` on the auth path.
4. Optional later: fill `TestParseWireHubGoldenVector` with a real Hub fixture.

---

## Out of this plan

- P2 ES256 / passkey message verification
- P3 encode helpers, address `Equal`, challenge/session helpers
- Catalog changes in NimiqMiniApps
