package nimiq

import (
	"encoding/hex"
	"strings"
	"testing"
)

// realMainnetHTLC is the recipient data of transaction
// 917bd9ee0fd49e12afae1db303d1afca863c7992f4fa642ab4d2c95c079c619a on
// mainnet (block 56247016): a Nimiq Pay contract creation.
const realMainnetHTLC = "91c5d65cbf079159b61d72bfca4ff1f5fd063227" + // sender
	"a70b9e44a448b5183ac4e186cd749d3d889fff84" + // recipient
	"01" + // sha256
	"0000000000000000000000000000000000000000000000000000000000000000" + // hash root
	"01" + // hash count
	"0000019faea7c288" // timeout, ms since epoch

func TestParseRealMainnetHTLC(t *testing.T) {
	raw, err := hex.DecodeString(realMainnetHTLC)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) != 82 {
		t.Fatalf("fixture is %d bytes, want 82", len(raw))
	}
	h, err := ParseHTLCCreation(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := h.Sender.String(); got != "NQ34 J72V CP5Y 0X8M KDGV EAYU LKYH XPXG CCH7" {
		t.Errorf("sender = %s", got)
	}
	if got := h.Recipient.String(); got != "NQ14 LU5R UH54 92SH GEN4 U63C SV4V 7N49 YYU4" {
		t.Errorf("recipient = %s", got)
	}
	if h.Algorithm != HashSha256 {
		t.Errorf("algorithm = %s, want sha256", h.Algorithm)
	}
	if h.HashCount != 1 {
		t.Errorf("hash count = %d, want 1", h.HashCount)
	}
	if h.Timeout != 1785341657736 {
		t.Errorf("timeout = %d", h.Timeout)
	}
	// The owner is the funder, not the transaction's sender: Nimiq Pay funds a
	// rotated contract from its predecessor.
	if h.HTLCOwner() != h.Sender {
		t.Error("owner must be the reclaiming sender")
	}
}

// TestHTLCRejectsTruncated matters because a truncated payload still contains
// a plausible-looking sender and recipient in its first 40 bytes. A parser that
// reads those and stops would report an owner for data it never validated.
func TestHTLCRejectsTruncated(t *testing.T) {
	full, _ := hex.DecodeString(realMainnetHTLC)
	for _, n := range []int{0, 1, 20, 40, 41, 60, 73, 81} {
		if _, err := ParseHTLCCreation(full[:n]); err == nil {
			t.Errorf("%d-byte payload should not parse", n)
		}
	}
}

func TestHTLCRejectsTrailingBytes(t *testing.T) {
	full, _ := hex.DecodeString(realMainnetHTLC)
	if _, err := ParseHTLCCreation(append(full, 0x00)); err == nil {
		t.Error("trailing bytes should be rejected")
	}
}

func TestHTLCRejectsBadAlgorithmAndCount(t *testing.T) {
	full, _ := hex.DecodeString(realMainnetHTLC)

	badAlgo := append([]byte(nil), full...)
	badAlgo[40] = 0x09
	if _, err := ParseHTLCCreation(badAlgo); err == nil || !strings.Contains(err.Error(), "algorithm") {
		t.Errorf("unknown algorithm should be rejected, got %v", err)
	}

	zeroCount := append([]byte(nil), full...)
	zeroCount[73] = 0x00 // hash count sits after the 32-byte root
	if _, err := ParseHTLCCreation(zeroCount); err == nil || !strings.Contains(err.Error(), "at least 1") {
		t.Errorf("a zero hash count should be rejected, got %v", err)
	}
}

// TestHTLCSha512Length covers the one variant with a different root size, so a
// hardcoded 32 would be caught.
func TestHTLCSha512Length(t *testing.T) {
	e := &encoder{}
	e.raw(make([]byte, AddressLen))
	e.raw(make([]byte, AddressLen))
	e.u8(byte(HashSha512))
	e.raw(make([]byte, 64))
	e.u8(1)
	e.u64be(1234)

	h, err := ParseHTLCCreation(e.buf)
	if err != nil {
		t.Fatal(err)
	}
	if h.Algorithm != HashSha512 || len(h.HashRoot) != 64 {
		t.Errorf("sha512 root = %d bytes, want 64", len(h.HashRoot))
	}
	if h.Timeout != 1234 {
		t.Errorf("timeout = %d, want 1234", h.Timeout)
	}
}
