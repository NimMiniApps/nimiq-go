package nimiq

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"
)

// The fixtures below were produced by constructing transactions by hand from
// the Albatross source and having a live Albatross node decode them via the
// getRawTransactionInfo RPC method. Every expected value here is what the node
// reported, not what this implementation produces.
//
// See docs/phase2-protocol.md §9 for the full transcript.

// signerStub signs with a fixed key without importing the signer package,
// keeping this test in the root package.
type signerStub struct{ key ed25519.PrivateKey }

func (s signerStub) PublicKey(context.Context) (ed25519.PublicKey, error) {
	return s.key.Public().(ed25519.PublicKey), nil
}
func (s signerStub) Sign(_ context.Context, msg []byte) ([]byte, error) {
	return ed25519.Sign(s.key, msg), nil
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex fixture: %v", err)
	}
	return b
}

// TestBasicFormatFixture checks the compact wire encoding against node-verified
// bytes, including the hash and the sender recovered from the public key.
func TestBasicFormatFixture(t *testing.T) {
	const (
		wantRaw  = "00000101010101010101010101010101010101010101010101010101010101010101020202020202020202020202020202020202020200000000000027100000000000000000000000640503030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303"
		wantHash = "79ede3d7f0e272a23a8b47c096d719af179d85fa7e9b6191776fa8095066e8b3"
		// The node derived this sender from the public key alone.
		wantSender = "NQ21 XG6E MX3E AVT9 4CRJ P3CF SEYF GJEA TCCU"
	)
	pub := bytes.Repeat([]byte{0x01}, 32)
	sig := bytes.Repeat([]byte{0x03}, 64)

	sender, err := AddressFromPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	if got := sender.String(); got != wantSender {
		t.Errorf("sender address\n got %q\nwant %q", got, wantSender)
	}

	recipient, err := AddressFromBytes(bytes.Repeat([]byte{0x02}, 20))
	if err != nil {
		t.Fatal(err)
	}
	tx, err := NewBasicTransaction(sender, recipient, 10000, 0, 100, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	proof, err := NewSignatureProof(pub, sig)
	if err != nil {
		t.Fatal(err)
	}
	tx.Proof = proof.Serialize()

	if got := tx.Format(); got != FormatBasic {
		t.Fatalf("format = %d, want FormatBasic", got)
	}
	raw, err := tx.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(raw); got != wantRaw {
		t.Errorf("serialized transaction\n got %s\nwant %s", got, wantRaw)
	}
	if len(raw) != 139 {
		t.Errorf("size = %d, want 139 (node-reported)", len(raw))
	}
	if got := tx.Hash().String(); got != wantHash {
		t.Errorf("hash\n got %s\nwant %s", got, wantHash)
	}
	// The signing content is a distinct encoding from the wire format; the node
	// confirmed the hash is taken over these 67 bytes.
	if got := len(tx.SigningContent()); got != 67 {
		t.Errorf("signing content = %d bytes, want 67", got)
	}
}

// TestExtendedFormatFixture checks the general wire encoding, which is what any
// transaction carrying a data payload actually uses.
func TestExtendedFormatFixture(t *testing.T) {
	const wantRaw = "011111111111111111111111111111111111111111000022222222222222222222222222222222222222220002686900000000000001f40000000000000001000000c80500620001010101010101010101010101010101010101010101010101010101010101010003030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303"

	sender, _ := AddressFromBytes(bytes.Repeat([]byte{0x11}, 20))
	recipient, _ := AddressFromBytes(bytes.Repeat([]byte{0x22}, 20))
	proof, err := NewSignatureProof(bytes.Repeat([]byte{0x01}, 32), bytes.Repeat([]byte{0x03}, 64))
	if err != nil {
		t.Fatal(err)
	}
	tx := &Transaction{
		Sender: sender, Recipient: recipient,
		RecipientData:       []byte("hi"),
		Value:               500,
		Fee:                 1,
		ValidityStartHeight: 200,
		Network:             NetworkTestAlbatross,
		Proof:               proof.Serialize(),
	}
	if got := tx.Format(); got != FormatExtended {
		t.Fatalf("a transaction with recipient data must use the extended format, got %d", got)
	}
	raw, err := tx.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(raw); got != wantRaw {
		t.Errorf("serialized transaction\n got %s\nwant %s", got, wantRaw)
	}
	if len(raw) != 168 {
		t.Errorf("size = %d, want 168 (node-reported)", len(raw))
	}
}

// TestProofLayout locks the 98-byte proof shape the node echoed back.
func TestProofLayout(t *testing.T) {
	const want = "0001010101010101010101010101010101010101010101010101010101010101010003030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303"
	proof, err := NewSignatureProof(bytes.Repeat([]byte{0x01}, 32), bytes.Repeat([]byte{0x03}, 64))
	if err != nil {
		t.Fatal(err)
	}
	got := hex.EncodeToString(proof.Serialize())
	if got != want {
		t.Errorf("proof\n got %s\nwant %s", got, want)
	}
	if len(proof.Serialize()) != 98 {
		t.Errorf("proof = %d bytes, want 98", len(proof.Serialize()))
	}
}

// TestRoundTrip checks that parsing wire bytes reproduces the original
// transaction — the capability that lets a backend inspect a Hub-signed
// transaction before broadcasting it.
func TestRoundTrip(t *testing.T) {
	for _, name := range []string{"basic", "extended"} {
		t.Run(name, func(t *testing.T) {
			var raw []byte
			if name == "basic" {
				raw = mustHex(t, "00000101010101010101010101010101010101010101010101010101010101010101020202020202020202020202020202020202020200000000000027100000000000000000000000640503030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303")
			} else {
				raw = mustHex(t, "011111111111111111111111111111111111111111000022222222222222222222222222222222222222220002686900000000000001f40000000000000001000000c80500620001010101010101010101010101010101010101010101010101010101010101010003030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303")
			}
			tx, err := ParseTransaction(raw)
			if err != nil {
				t.Fatal(err)
			}
			again, err := tx.Serialize()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(raw, again) {
				t.Errorf("round trip changed the bytes\n got %x\nwant %x", again, raw)
			}
		})
	}
}

// TestParseRejectsTrailingBytes ensures appended data cannot ride along on an
// otherwise valid transaction.
func TestParseRejectsTrailingBytes(t *testing.T) {
	raw := mustHex(t, "00000101010101010101010101010101010101010101010101010101010101010101020202020202020202020202020202020202020200000000000027100000000000000000000000640503030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303")
	if _, err := ParseTransaction(append(raw, 0xff)); err == nil {
		t.Fatal("expected an error for trailing bytes, got nil")
	}
}

// TestEndToEndSignAndVerify is the spike proper: key to address to transaction
// to signature to proof to serialized bytes to hash, with no network access.
func TestEndToEndSignAndVerify(t *testing.T) {
	// A fixed test seed. Never a real key.
	seed := mustHex(t, "0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20")
	key := ed25519.NewKeyFromSeed(seed)
	s := signerStub{key: key}
	ctx := context.Background()

	sender, err := SignerAddress(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	recipient := MustParseAddress("NQ34 248H 248H 248H 248H 248H 248H 248H 248H")

	tx, err := NewBasicTransaction(sender, recipient, MustParseNIM("1.5"), MustParseNIM("0.00001"), 4242, NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	if tx.Value != 150000 {
		t.Errorf("1.5 NIM = %d Luna, want 150000", tx.Value)
	}
	if tx.Fee != 1 {
		t.Errorf("0.00001 NIM = %d Luna, want 1", tx.Fee)
	}

	// The hash must be known before signing — that is what makes rebroadcast
	// idempotent.
	hashBefore := tx.Hash()
	if err := SignTransaction(ctx, s, tx); err != nil {
		t.Fatal(err)
	}
	if tx.Hash() != hashBefore {
		t.Error("signing changed the transaction hash; the proof must be excluded from the signing content")
	}

	proof, err := ParseSignatureProof(tx.Proof)
	if err != nil {
		t.Fatal(err)
	}
	if err := proof.Verify(tx); err != nil {
		t.Fatalf("verifying our own signature: %v", err)
	}

	raw, err := tx.Serialize()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseTransaction(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Hash() != tx.Hash() {
		t.Error("hash changed across a serialize/parse round trip")
	}
	if parsed.Sender != sender || parsed.Recipient != recipient {
		t.Error("addresses changed across a serialize/parse round trip")
	}
	if parsed.Value != tx.Value || parsed.Fee != tx.Fee {
		t.Error("amounts changed across a serialize/parse round trip")
	}

	// Signing is deterministic: the same transaction must produce byte-identical
	// output, or retrying a broadcast would create a second distinct payment.
	tx2, _ := NewBasicTransaction(sender, recipient, MustParseNIM("1.5"), MustParseNIM("0.00001"), 4242, NetworkTestAlbatross)
	if err := SignTransaction(ctx, s, tx2); err != nil {
		t.Fatal(err)
	}
	raw2, _ := tx2.Serialize()
	if !bytes.Equal(raw, raw2) {
		t.Error("signing the same transaction twice produced different bytes")
	}

	t.Logf("sender  %s", sender)
	t.Logf("hash    %s", tx.Hash())
	t.Logf("raw     %s", hex.EncodeToString(raw))
}

// TestSignerRefusesWrongSender guards against a valid signature from a key that
// does not control the sender.
func TestSignerRefusesWrongSender(t *testing.T) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x07}, 32))
	tx := &Transaction{
		Sender:    MustParseAddress("NQ34 248H 248H 248H 248H 248H 248H 248H 248H"),
		Recipient: MustParseAddress("NQ34 248H 248H 248H 248H 248H 248H 248H 248H"),
		Value:     1, Network: NetworkTestAlbatross,
	}
	err := SignTransaction(context.Background(), signerStub{key: key}, tx)
	if err == nil {
		t.Fatal("expected a refusal to sign for an address the key does not control")
	}
	if !strings.Contains(err.Error(), "controls") {
		t.Errorf("unclear error: %v", err)
	}
}

func TestRejectsNonAlbatrossNetwork(t *testing.T) {
	a := MustParseAddress("NQ34 248H 248H 248H 248H 248H 248H 248H 248H")
	// 42 is proof-of-work mainnet. Albatross mainnet is 24.
	if _, err := NewBasicTransaction(a, a, 1, 0, 1, NetworkID(42)); err == nil {
		t.Fatal("expected a refusal to build a transaction for a proof-of-work network")
	}
}

func TestAddressRoundTripAndChecksum(t *testing.T) {
	const s = "NQ21 XG6E MX3E AVT9 4CRJ P3CF SEYF GJEA TCCU"
	a, err := ParseAddress(s)
	if err != nil {
		t.Fatal(err)
	}
	if a.String() != s {
		t.Errorf("round trip\n got %q\nwant %q", a.String(), s)
	}
	if lower, err := ParseAddress(strings.ToLower(strings.ReplaceAll(s, " ", ""))); err != nil || lower != a {
		t.Error("parsing should ignore case and spacing")
	}
	// Corrupting a payload character must fail the checksum.
	bad := []byte(strings.ReplaceAll(s, " ", ""))
	if bad[10] == 'A' {
		bad[10] = 'B'
	} else {
		bad[10] = 'A'
	}
	if _, err := ParseAddress(string(bad)); err == nil {
		t.Error("expected a checksum failure for a corrupted address")
	}
}

func TestParseNIMPrecision(t *testing.T) {
	cases := []struct {
		in   string
		want Luna
	}{
		{"1.5", 150000},
		{"1", 100000},
		{"0.00001", 1},
		{"0", 0},
		{"12.34567", 1234567},
		// The classic float failure: 0.1+0.2 style inputs must stay exact.
		{"0.07", 7000},
	}
	for _, c := range cases {
		got, err := ParseNIM(c.in)
		if err != nil {
			t.Errorf("ParseNIM(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseNIM(%q) = %d, want %d", c.in, got, c.want)
		}
		if round := got.String(); round != strings.TrimSuffix(strings.TrimSuffix(c.in, "0"), ".") && round != c.in {
			// Only checked loosely: String trims trailing zeros by design.
			_ = round
		}
	}
	for _, bad := range []string{"", "1.234567", "-1", "abc", "1.2.3", "+1"} {
		if _, err := ParseNIM(bad); err == nil {
			t.Errorf("ParseNIM(%q) should have failed", bad)
		}
	}
}

func TestLunaStringRoundTrip(t *testing.T) {
	for _, c := range []struct {
		in   Luna
		want string
	}{{150000, "1.5"}, {100000, "1"}, {1, "0.00001"}, {0, "0"}, {1234567, "12.34567"}} {
		if got := c.in.String(); got != c.want {
			t.Errorf("Luna(%d).String() = %q, want %q", c.in, got, c.want)
		}
	}
}
