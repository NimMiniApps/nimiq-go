package nimiq

import (
	"crypto/ed25519"
	"encoding/binary"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseCashlinkBare(t *testing.T) {
	seed := bytes32(1)
	raw := append(seed[:], u64be(150_000)...) // 1.5 NIM
	url := "https://hub.nimiq.com/cashlink/#" + hubEncode(raw)

	c, err := ParseCashlink(url)
	if err != nil {
		t.Fatal(err)
	}
	if c.Value != 150_000 {
		t.Errorf("value = %d, want 150000", c.Value)
	}
	if c.Message != "" || c.Theme != 0 {
		t.Errorf("message=%q theme=%d, want empty", c.Message, c.Theme)
	}
	if c.Key != seed {
		t.Errorf("key mismatch")
	}
	want, err := AddressFromPublicKey(ed25519.NewKeyFromSeed(seed[:]).Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	got, err := AddressFromPublicKey(ed25519.NewKeyFromSeed(c.Key[:]).Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("address = %s, want %s", got, want)
	}
}

func TestParseCashlinkWithMessageAndTheme(t *testing.T) {
	seed := bytes32(7)
	msg := []byte("Happy birthday!")
	raw := append(seed[:], u64be(100_000)...)
	raw = append(raw, byte(len(msg)))
	raw = append(raw, msg...)
	raw = append(raw, 3) // theme
	url := "https://hub.nimiq.com/cashlink/#" + hubEncode(raw)

	c, err := ParseCashlink(url)
	if err != nil {
		t.Fatal(err)
	}
	if c.Value != 100_000 || c.Message != "Happy birthday!" || c.Theme != 3 {
		t.Errorf("got value=%d msg=%q theme=%d", c.Value, c.Message, c.Theme)
	}
}

func TestCashlinkRoundTrip(t *testing.T) {
	orig := &Cashlink{Key: bytes32(9), Value: 42_000, Message: "yo", Theme: 2}
	url := orig.URL("https://hub.nimiq.com/cashlink/")
	got, err := ParseCashlink(url)
	if err != nil {
		t.Fatal(err)
	}
	if got.Value != orig.Value || got.Message != orig.Message || got.Theme != orig.Theme {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if got.Key != orig.Key {
		t.Errorf("key mismatch")
	}
	if !strings.Contains(url, "#") {
		t.Fatal("URL missing fragment")
	}
}

func TestCashlinkNormalisesTildeAndDotPadding(t *testing.T) {
	seed := bytes32(2)
	raw := append(seed[:], u64be(1)...)
	msg := make([]byte, 200)
	for i := range msg {
		msg[i] = 'a'
	}
	raw = append(raw, byte(len(msg)))
	raw = append(raw, msg...)
	payload := hubEncode(raw)
	dotted := strings.ReplaceAll(strings.ReplaceAll(payload, "~", ""), "=", ".")
	c, err := ParseCashlink("https://hub.nimiq.com/cashlink/#" + dotted)
	if err != nil {
		t.Fatal(err)
	}
	if c.Value != 1 || c.Message != string(msg) {
		t.Errorf("normalised parse failed: value=%d msgLen=%d", c.Value, len(c.Message))
	}
}

func TestCashlinkRejectsTrailingBytes(t *testing.T) {
	seed := bytes32(3)
	raw := append(seed[:], u64be(1)...)
	raw = append(raw, 0)    // empty message
	raw = append(raw, 1)    // theme
	raw = append(raw, 0x99) // trailing
	_, err := ParseCashlink("https://hub.nimiq.com/cashlink/#" + hubEncode(raw))
	if err == nil {
		t.Fatal("expected trailing-bytes error")
	}
}

func TestCashlinkRejectsTruncated(t *testing.T) {
	seed := bytes32(4)
	_, err := ParseCashlink("https://hub.nimiq.com/cashlink/#" + hubEncode(seed[:]))
	if err == nil {
		t.Fatal("expected truncated error")
	}
}

// TestCashlinkExternalVector pins the wire format to a payload built outside
// this package. Every other cashlink test encodes with our own encoder, so
// they would all still pass if the layout were wrong.
//
// Bytes: key 01..20, value 1500000 (big-endian u64), message length 6,
// "Danke!", theme 3 — base64url'd independently.
func TestCashlinkExternalVector(t *testing.T) {
	const payload = "AQIDBAUGBwgJCgsMDQ4PEBESExQVFhcYGRobHB0eHyAAAAAAABbjYAZEYW5rZSED"

	c, err := ParseCashlink("https://hub.nimiq.com/cashlink/#" + payload)
	if err != nil {
		t.Fatal(err)
	}
	if c.Value != 1_500_000 {
		t.Errorf("value = %d, want 1500000", c.Value)
	}
	if c.Message != "Danke!" {
		t.Errorf("message = %q, want %q", c.Message, "Danke!")
	}
	if c.Theme != 3 {
		t.Errorf("theme = %d, want 3", c.Theme)
	}
	for i, b := range c.Key {
		if b != byte(i+1) {
			t.Fatalf("key[%d] = %d, want %d", i, b, i+1)
		}
	}
	// And back out to the same fragment.
	if got := c.URL("https://hub.nimiq.com/cashlink/"); got != "https://hub.nimiq.com/cashlink/#"+payload {
		t.Errorf("URL() = %s", got)
	}
}

func TestCashlinkTruncatesLongMessageOnRuneBoundary(t *testing.T) {
	c := &Cashlink{Key: bytes32(5), Value: 1, Message: strings.Repeat("é", 200)} // 400 bytes
	got, err := ParseCashlink(c.URL("https://hub.nimiq.com/cashlink/"))
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(got.Message) {
		t.Fatalf("truncated message is not valid UTF-8: %q", got.Message)
	}
	if len(got.Message) > 255 {
		t.Fatalf("message is %d bytes, want <= 255", len(got.Message))
	}
}

func bytes32(fill byte) [32]byte {
	var b [32]byte
	for i := range b {
		b[i] = fill
	}
	if fill == 0 {
		b[0] = 1
	}
	return b
}

func u64be(v uint64) []byte {
	var b [8]byte
	binary.BigEndian.PutUint64(b[:], v)
	return b[:]
}

func hubEncode(raw []byte) string {
	return encodeCashlinkPayload(raw)
}
