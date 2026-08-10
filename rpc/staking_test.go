package rpc

import (
	"context"
	"errors"
	"testing"

	nimiq "github.com/NimMiniApps/nimiq-go"
)

// TestStakerTotalAcrossBuckets guards the field that is easy to misread:
// getStakerByAddress reports the ACTIVE balance in "balance", so a caller
// treating it as the total under-reports every staker mid-withdrawal.
func TestStakerTotalAcrossBuckets(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{
		"getStakerByAddress": `{"data":{"address":"NQ11","balance":100921583,` +
			`"inactiveBalance":100000000,"retiredBalance":25,` +
			`"delegation":"NQ57","inactiveFrom":8129610},"metadata":null}`,
	})
	s, err := c.GetStaker(context.Background(), testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if s.Balance != 100921583 {
		t.Errorf("active = %d", s.Balance)
	}
	if want := nimiq.Luna(100921583 + 100000000 + 25); s.Total() != want {
		t.Errorf("total = %d, want %d", s.Total(), want)
	}
	at, cooling := s.RetirableAt()
	if !cooling || at != 8129610 {
		t.Errorf("RetirableAt = %d, %v", at, cooling)
	}
}

// TestStakerNoCooldown covers a null inactiveFrom, which must be
// distinguishable from height zero.
func TestStakerNoCooldown(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{
		"getStakerByAddress": `{"data":{"address":"NQ11","balance":1,"inactiveFrom":null},"metadata":null}`,
	})
	s, err := c.GetStaker(context.Background(), testAddr)
	if err != nil {
		t.Fatal(err)
	}
	if _, cooling := s.RetirableAt(); cooling {
		t.Error("a null inactiveFrom must not report a cooldown")
	}
}

// TestGetStakerNotAStaker: nodes report an unknown staker as an error. Never
// having staked is an ordinary state and must be distinguishable.
func TestGetStakerNotAStaker(t *testing.T) {
	c, _ := newFakeNode(t, nil)
	_, err := c.GetStaker(context.Background(), testAddr)
	if !errors.Is(err, ErrNoStaker) {
		t.Fatalf("expected ErrNoStaker, got %v", err)
	}
}

// TestValidatorEligibility guards against delegating to a validator that would
// make the transaction fail execution — included, fee charged, nothing done.
func TestValidatorEligibility(t *testing.T) {
	height := uint32(500)
	cases := map[string]struct {
		v    Validator
		want bool
	}{
		"active":   {Validator{Address: "NQ1"}, true},
		"retired":  {Validator{Address: "NQ1", Retired: true}, false},
		"inactive": {Validator{Address: "NQ1", InactivityFlag: &height}, false},
		"jailed":   {Validator{Address: "NQ1", JailedFrom: &height}, false},
	}
	for name, c := range cases {
		if got := c.v.Eligible(); got != c.want {
			t.Errorf("%s: Eligible() = %v, want %v", name, got, c.want)
		}
	}
}

func TestGetValidatorNotFound(t *testing.T) {
	c, _ := newFakeNode(t, nil)
	_, err := c.GetValidator(context.Background(), testAddr)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestGetActiveValidators(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{
		"getActiveValidators": `{"data":[{"address":"NQ1","numStakers":3,"balance":50},` +
			`{"address":"NQ2","retired":true}],"metadata":null}`,
	})
	vs, err := c.GetActiveValidators(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(vs) != 2 {
		t.Fatalf("got %d validators, want 2", len(vs))
	}
	if !vs[0].Eligible() || vs[1].Eligible() {
		t.Error("eligibility misreported")
	}
}
