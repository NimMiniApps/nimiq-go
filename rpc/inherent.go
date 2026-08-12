package rpc

import (
	"context"

	nimiq "github.com/NimMiniApps/nimiq-go"
)

// Inherent is a chain-level event recorded at a block — a reward payout, a
// jail, or a penalty. Not every inherent is a reward; check IsReward before
// treating Value/Target as a payout.
type Inherent struct {
	Type             string     `json:"type"`
	BlockNumber      uint32     `json:"blockNumber"`
	BlockTime        uint64     `json:"blockTime"`
	ValidatorAddress string     `json:"validatorAddress"`
	Target           string     `json:"target"`
	Value            nimiq.Luna `json:"value"`
	Hash             string     `json:"hash"`
}

// IsReward reports whether this inherent is a reward payout rather than a
// jail or penalty event.
func (i Inherent) IsReward() bool { return i.Type == "reward" }

// GetInherentsByBlockNumber returns every inherent recorded at height, in
// block order. A block with no inherents returns an empty slice.
func (c *Client) GetInherentsByBlockNumber(ctx context.Context, height uint32) ([]Inherent, error) {
	var inherents []Inherent
	if err := c.call(ctx, "getInherentsByBlockNumber", []any{height}, &inherents); err != nil {
		return nil, err
	}
	return inherents, nil
}
