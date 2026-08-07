package nimiq

import (
	"fmt"
	"math"
	"strings"
)

// LunaPerNIM is the number of Luna in one NIM. Nimiq uses 5 decimal places.
const LunaPerNIM = 100_000

// MaxLuna is the largest representable amount, matching Coin::MAX_SAFE_VALUE in
// core-rs (primitives/src/coin.rs). Values above it are rejected by nodes, so
// the SDK rejects them at construction rather than at broadcast.
const MaxLuna Luna = 1<<53 - 1

// Luna is an amount of NIM in its smallest unit.
//
// There is deliberately no float64 constructor: the workspace audit found
// several float-based NIM conversions, which lose precision on ordinary
// amounts. Use ParseNIM for human input and Luna arithmetic everywhere else.
type Luna uint64

// ParseNIM parses a decimal NIM string ("1.5", "0.00001", "12") into Luna
// exactly, without floating point. At most 5 decimal places are accepted.
func ParseNIM(s string) (Luna, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("nimiq: empty NIM amount")
	}
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		return 0, fmt.Errorf("nimiq: NIM amount %q must not be signed", s)
	}
	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" && !hasFrac {
		return 0, fmt.Errorf("nimiq: invalid NIM amount %q", s)
	}
	if hasFrac && len(frac) > 5 {
		return 0, fmt.Errorf("nimiq: NIM amount %q has more than 5 decimal places", s)
	}
	// Right-pad the fraction to exactly 5 digits so "1.5" means 1.50000 NIM
	// rather than 1.00005.
	frac += strings.Repeat("0", 5-len(frac))

	var total Luna
	for _, part := range []string{whole, frac} {
		for _, c := range part {
			if c < '0' || c > '9' {
				return 0, fmt.Errorf("nimiq: invalid NIM amount %q", s)
			}
		}
	}
	if whole != "" {
		w, err := parseUintStrict(whole)
		if err != nil || w > uint64(MaxLuna)/LunaPerNIM {
			return 0, fmt.Errorf("nimiq: NIM amount %q is out of range", s)
		}
		total = Luna(w * LunaPerNIM)
	}
	f, err := parseUintStrict(frac)
	if err != nil {
		return 0, fmt.Errorf("nimiq: invalid NIM amount %q", s)
	}
	total += Luna(f)
	if total > MaxLuna {
		return 0, fmt.Errorf("nimiq: NIM amount %q is out of range", s)
	}
	return total, nil
}

// MustParseNIM is ParseNIM for constants and tests. It panics on invalid input.
func MustParseNIM(s string) Luna {
	v, err := ParseNIM(s)
	if err != nil {
		panic(err)
	}
	return v
}

func parseUintStrict(s string) (uint64, error) {
	var v uint64
	for _, c := range s {
		if v > math.MaxUint64/10 {
			return 0, fmt.Errorf("overflow")
		}
		v = v*10 + uint64(c-'0')
	}
	return v, nil
}

// String renders the amount in NIM with trailing zeros trimmed: "1.5", "12",
// "0.00001". It never renders in scientific notation.
func (l Luna) String() string {
	whole := uint64(l) / LunaPerNIM
	frac := uint64(l) % LunaPerNIM
	if frac == 0 {
		return fmt.Sprintf("%d", whole)
	}
	return strings.TrimRight(fmt.Sprintf("%d.%05d", whole, frac), "0")
}

// Add returns l+other, reporting an error on overflow past MaxLuna rather than
// wrapping. Amounts are money; silent wraparound is not acceptable.
func (l Luna) Add(other Luna) (Luna, error) {
	sum := l + other
	if sum < l || sum > MaxLuna {
		return 0, fmt.Errorf("nimiq: amount overflow adding %s and %s NIM", l, other)
	}
	return sum, nil
}

// Sub returns l-other, reporting an error if the result would be negative.
func (l Luna) Sub(other Luna) (Luna, error) {
	if other > l {
		return 0, fmt.Errorf("nimiq: amount underflow subtracting %s from %s NIM", other, l)
	}
	return l - other, nil
}
