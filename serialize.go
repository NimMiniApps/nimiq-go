package nimiq

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Nimiq serializes with postcard (see nimiq-serde), with three overrides that a
// generic postcard codec gets wrong: Coin and validity-start-height are
// fixed-width big-endian, and Address is a raw fixed-size byte array.
//
// ponytail: only the ~6 primitives Nimiq actually uses are implemented. A
// generic postcard port would be far more code and no more correct.

var errTruncated = errors.New("nimiq: truncated input")

// maxByteLen bounds length-prefixed reads so a malformed uvarint cannot make a
// decoder allocate arbitrarily. No legitimate Nimiq field approaches this.
const maxByteLen = 1 << 20

type encoder struct{ buf []byte }

func (e *encoder) u8(v byte) { e.buf = append(e.buf, v) }

func (e *encoder) raw(b []byte) { e.buf = append(e.buf, b...) }

// uvarint writes postcard's LEB128 unsigned varint.
func (e *encoder) uvarint(v uint64) {
	for v >= 0x80 {
		e.buf = append(e.buf, byte(v)|0x80)
		v >>= 7
	}
	e.buf = append(e.buf, byte(v))
}

// bytes writes a uvarint length prefix followed by the payload — postcard's
// encoding for Vec<u8>.
func (e *encoder) bytes(b []byte) {
	e.uvarint(uint64(len(b)))
	e.raw(b)
}

// u64be writes a fixed 8-byte big-endian integer (nimiq_serde::fixint::be,
// used for Coin — NOT a varint).
func (e *encoder) u64be(v uint64) {
	e.buf = binary.BigEndian.AppendUint64(e.buf, v)
}

// u32be writes a fixed 4-byte big-endian integer (validity_start_height, which
// core-rs serializes via to_be_bytes()).
func (e *encoder) u32be(v uint32) {
	e.buf = binary.BigEndian.AppendUint32(e.buf, v)
}

// u16be writes a fixed 2-byte big-endian integer. Used only by
// Transaction.SigningContent, which encodes the recipient-data length this way
// for PoW backwards compatibility — the wire format uses a uvarint instead.
func (e *encoder) u16be(v uint16) {
	e.buf = binary.BigEndian.AppendUint16(e.buf, v)
}

type decoder struct{ buf []byte }

func (d *decoder) u8() (byte, error) {
	if len(d.buf) < 1 {
		return 0, errTruncated
	}
	v := d.buf[0]
	d.buf = d.buf[1:]
	return v, nil
}

func (d *decoder) raw(n int) ([]byte, error) {
	if len(d.buf) < n {
		return nil, errTruncated
	}
	v := d.buf[:n]
	d.buf = d.buf[n:]
	return v, nil
}

// uvarint reads a postcard LEB128 varint, rejecting non-canonical encodings.
//
// LEB128 permits redundant continuation bytes: 0xe6 0x00 and 0x66 both decode
// to 102. Accepting both would make a transaction malleable — two distinct byte
// strings would parse to the same transaction, so the bytes a caller inspected
// and approved would not be the bytes it re-serialized and broadcast. Only the
// minimal encoding is accepted.
func (d *decoder) uvarint() (uint64, error) {
	v, n := binary.Uvarint(d.buf)
	if n <= 0 {
		return 0, errTruncated
	}
	// The final byte of a minimal encoding is never zero, unless the value is
	// zero and occupies that single byte.
	if n > 1 && d.buf[n-1] == 0x00 {
		return 0, fmt.Errorf("nimiq: non-canonical varint encoding")
	}
	d.buf = d.buf[n:]
	return v, nil
}

func (d *decoder) bytes() ([]byte, error) {
	n, err := d.uvarint()
	if err != nil {
		return nil, err
	}
	if n > maxByteLen {
		return nil, fmt.Errorf("nimiq: length %d exceeds maximum %d", n, maxByteLen)
	}
	return d.raw(int(n))
}

func (d *decoder) u64be() (uint64, error) {
	b, err := d.raw(8)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint64(b), nil
}

func (d *decoder) u32be() (uint32, error) {
	b, err := d.raw(4)
	if err != nil {
		return 0, err
	}
	return binary.BigEndian.Uint32(b), nil
}

// end reports an error unless every byte was consumed. Nimiq's deserializer
// rejects trailing data (nimiq_serde DeserializeError::ExtraData) and so must
// this: a transaction with extra bytes appended must not be accepted as valid.
func (d *decoder) end() error {
	if len(d.buf) != 0 {
		return fmt.Errorf("nimiq: %d unexpected trailing bytes", len(d.buf))
	}
	return nil
}
