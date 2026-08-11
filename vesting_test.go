package nimiq

import (
	"encoding/hex"
	"testing"
)

func TestParseVestingShort(t *testing.T) {
	// 28 bytes: owner (20) + time_step (8). start=0; amounts filled by the caller from tx value.
	owner := MustParseAddress("NQ34 248H 248H 248H 248H 248H 248H 248H 248H")
	e := &encoder{}
	e.raw(owner[:])
	e.u64be(86_400_000) // 1 day in ms
	v, err := ParseVestingCreation(e.buf)
	if err != nil {
		t.Fatal(err)
	}
	if v.Owner != owner || v.StartTime != 0 || v.TimeStep != 86_400_000 {
		t.Errorf("got %+v", v)
	}
	if v.StepAmount != 0 || v.TotalAmount != 0 {
		t.Errorf("short form amounts should be zero without tx value, got step=%d total=%d", v.StepAmount, v.TotalAmount)
	}
}

func TestParseVestingMedium(t *testing.T) {
	// 44 bytes: owner + start + time_step + step_amount; total from tx value.
	owner := MustParseAddress("NQ34 248H 248H 248H 248H 248H 248H 248H 248H")
	e := &encoder{}
	e.raw(owner[:])
	e.u64be(1_700_000_000_000)
	e.u64be(3_600_000)
	e.u64be(50_000)
	v, err := ParseVestingCreation(e.buf)
	if err != nil {
		t.Fatal(err)
	}
	if v.StartTime != 1_700_000_000_000 || v.TimeStep != 3_600_000 || v.StepAmount != 50_000 {
		t.Errorf("got %+v", v)
	}
	if v.TotalAmount != 0 {
		t.Errorf("medium form total should be zero, got %d", v.TotalAmount)
	}
}

func TestParseVestingFull(t *testing.T) {
	owner := MustParseAddress("NQ34 248H 248H 248H 248H 248H 248H 248H 248H")
	e := &encoder{}
	e.raw(owner[:])
	e.u64be(100)
	e.u64be(200)
	e.u64be(300)
	e.u64be(900)
	v, err := ParseVestingCreation(e.buf)
	if err != nil {
		t.Fatal(err)
	}
	if v.Owner != owner || v.StartTime != 100 || v.TimeStep != 200 || v.StepAmount != 300 || v.TotalAmount != 900 {
		t.Errorf("got %+v", v)
	}
}

func TestVestingRejectsTruncated(t *testing.T) {
	e := &encoder{}
	e.raw(make([]byte, AddressLen))
	e.u64be(1)
	full := append([]byte(nil), e.buf...) // 28-byte short form
	for _, n := range []int{0, 1, 20, 27} {
		if _, err := ParseVestingCreation(full[:n]); err == nil {
			t.Errorf("%d-byte payload should not parse", n)
		}
	}
}

func TestVestingRejectsTrailingBytes(t *testing.T) {
	e := &encoder{}
	e.raw(make([]byte, AddressLen))
	e.u64be(1)
	if _, err := ParseVestingCreation(append(e.buf, 0x00)); err == nil {
		t.Error("trailing bytes should be rejected")
	}
}

func TestVestingRejectsUnknownLength(t *testing.T) {
	raw, _ := hex.DecodeString("00")
	if _, err := ParseVestingCreation(raw); err == nil {
		t.Fatal("expected error for unknown length")
	}
	// 29 bytes is between short and medium.
	raw = make([]byte, 29)
	if _, err := ParseVestingCreation(raw); err == nil {
		t.Fatal("expected error for 29-byte payload")
	}
}
