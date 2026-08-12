package rpc

import (
	"context"
	"errors"
	"fmt"

	nimiq "github.com/NimMiniApps/nimiq-go"
)

// Staker is a staking account's on-chain position.
//
// Stake moves through three buckets in order: active, then inactive after
// SetActiveStake lowers the active balance, then retired after RetireStake.
// Only retired stake can be withdrawn with RemoveStake. Each step has to wait
// for the one before it to settle, so a withdrawal is a sequence of
// transactions over time rather than a single call.
type Staker struct {
	Address string `json:"address"`
	// Balance is the ACTIVE balance only, despite the name. The staker's total
	// is Balance + InactiveBalance + RetiredBalance; reading Balance as the
	// total under-reports every staker mid-withdrawal.
	Balance         nimiq.Luna `json:"balance"`
	InactiveBalance nimiq.Luna `json:"inactiveBalance"`
	RetiredBalance  nimiq.Luna `json:"retiredBalance"`
	// Delegation is the validator this stake backs, empty if undelegated.
	Delegation string `json:"delegation"`
	// InactiveFrom is the block height at which inactive stake becomes
	// retirable. It is a future height while the cooldown is running, so
	// RetireStake before it fails execution.
	InactiveFrom *uint32 `json:"inactiveFrom"`
}

// Total returns the staker's whole position across all three buckets.
func (s *Staker) Total() nimiq.Luna {
	return s.Balance + s.InactiveBalance + s.RetiredBalance
}

// RetirableAt reports the height from which inactive stake may be retired, and
// whether a cooldown is in progress at all.
func (s *Staker) RetirableAt() (uint32, bool) {
	if s.InactiveFrom == nil {
		return 0, false
	}
	return *s.InactiveFrom, true
}

// ErrNoStaker reports that an address has no staking position. Nodes signal
// this with an internal error rather than an empty result, so it is normalized
// here: an address that has never staked is an ordinary state, not a failure.
var ErrNoStaker = errors.New("nimiq/rpc: address has no staking position")

// GetStaker looks up an address's staking position.
func (c *Client) GetStaker(ctx context.Context, addr nimiq.Address) (*Staker, error) {
	var s Staker
	if err := c.call(ctx, "getStakerByAddress", []any{addr.String()}, &s); err != nil {
		var rpcErr *Error
		if errors.As(err, &rpcErr) {
			return nil, fmt.Errorf("%w: %s", ErrNoStaker, addr)
		}
		return nil, err
	}
	if s.Address == "" {
		return nil, fmt.Errorf("%w: %s", ErrNoStaker, addr)
	}
	return &s, nil
}

// Validator is an active validator's published state.
type Validator struct {
	Address       string     `json:"address"`
	RewardAddress string     `json:"rewardAddress"`
	Balance       nimiq.Luna `json:"balance"`
	NumStakers    int        `json:"numStakers"`
	Retired       bool       `json:"retired"`
	// InactivityFlag is the height a validator became inactive, nil if active.
	InactivityFlag *uint32 `json:"inactivityFlag"`
	// JailedFrom is the height a validator was jailed from, nil if not jailed.
	JailedFrom *uint32 `json:"jailedFrom"`
}

// Eligible reports whether a validator is a sensible delegation target: active,
// not retired and not jailed.
//
// Delegating to a validator that does not exist, or is retired, produces a
// transaction that is included in a block and then fails execution — the fee is
// charged and nothing happens. Check before building the transaction.
func (v *Validator) Eligible() bool {
	return !v.Retired && v.InactivityFlag == nil && v.JailedFrom == nil
}

// GetActiveValidators returns the validators currently in the active set.
func (c *Client) GetActiveValidators(ctx context.Context) ([]Validator, error) {
	var vs []Validator
	if err := c.call(ctx, "getActiveValidators", nil, &vs); err != nil {
		return nil, err
	}
	return vs, nil
}

// GetValidator looks up a single validator by address.
func (c *Client) GetValidator(ctx context.Context, addr nimiq.Address) (*Validator, error) {
	var v Validator
	if err := c.call(ctx, "getValidatorByAddress", []any{addr.String()}, &v); err != nil {
		var rpcErr *Error
		if errors.As(err, &rpcErr) {
			return nil, fmt.Errorf("%w: no validator %s", ErrNotFound, addr)
		}
		return nil, err
	}
	if v.Address == "" {
		return nil, fmt.Errorf("%w: no validator %s", ErrNotFound, addr)
	}
	return &v, nil
}

// GetStakersByValidatorAddress lists every staker delegating to a validator.
//
// A validator with no delegators returns an empty slice, not an error — that
// is an ordinary state (see GetStaker's ErrNoStaker for the same distinction
// on a single address).
func (c *Client) GetStakersByValidatorAddress(ctx context.Context, addr nimiq.Address) ([]Staker, error) {
	var stakers []Staker
	if err := c.call(ctx, "getStakersByValidatorAddress", []any{addr.String()}, &stakers); err != nil {
		return nil, err
	}
	return stakers, nil
}
