package nimiq

import (
	"bytes"
	"encoding/hex"
	"testing"
)

// FuzzParseTransaction checks that the parser never panics on hostile input and
// that anything it does accept re-serializes to exactly the input bytes.
//
// The round-trip property matters beyond crash-safety: a backend that accepts a
// transaction signed elsewhere decides whether to broadcast based on the parsed
// fields, so a parse that loses or alters bytes would let it approve one
// transaction and broadcast a different one.
func FuzzParseTransaction(f *testing.F) {
	for _, seed := range []string{
		"00000101010101010101010101010101010101010101010101010101010101010101020202020202020202020202020202020202020200000000000027100000000000000000000000640503030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303",
		"011111111111111111111111111111111111111111000022222222222222222222222222222222222222220002686900000000000001f40000000000000001000000c80500620001010101010101010101010101010101010101010101010101010101010101010003030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303030303",
		"", "00", "01", "02",
	} {
		b, _ := hex.DecodeString(seed)
		f.Add(b)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		tx, err := ParseTransaction(data)
		if err != nil {
			return
		}
		out, err := tx.Serialize()
		if err != nil {
			t.Fatalf("a parsed transaction failed to re-serialize: %v", err)
		}
		if !bytes.Equal(data, out) {
			t.Fatalf("round trip altered bytes\n in %x\nout %x", data, out)
		}
		// Hashing must never panic on anything the parser accepted.
		_ = tx.Hash()
	})
}

// FuzzParseAddress checks the address parser rejects malformed input without
// panicking, and that everything it accepts round-trips.
func FuzzParseAddress(f *testing.F) {
	f.Add("NQ21 XG6E MX3E AVT9 4CRJ P3CF SEYF GJEA TCCU")
	f.Add("NQ34248H248H248H248H248H248H248H248H")
	f.Add("")
	f.Add("NQ00")

	f.Fuzz(func(t *testing.T, s string) {
		a, err := ParseAddress(s)
		if err != nil {
			return
		}
		again, err := ParseAddress(a.String())
		if err != nil || again != a {
			t.Fatalf("address %q did not round-trip through %q", s, a.String())
		}
	})
}

// FuzzParseNIM checks amount parsing never panics and never produces a value
// that fails to render.
func FuzzParseNIM(f *testing.F) {
	for _, s := range []string{"1.5", "0", "0.00001", "99999999", "", ".", "1.", "-1"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		v, err := ParseNIM(s)
		if err != nil {
			return
		}
		// Anything parsed must render and re-parse to the same value.
		back, err := ParseNIM(v.String())
		if err != nil || back != v {
			t.Fatalf("ParseNIM(%q) = %d rendered as %q which did not round-trip", s, v, v.String())
		}
	})
}
