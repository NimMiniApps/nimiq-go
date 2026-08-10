package rpc

import (
	"context"
	"fmt"

	nimiq "github.com/NimMiniApps/nimiq-go"
)

// Policy holds the chain's consensus constants.
//
// These are network-specific, so the SDK reads them rather than hardcoding
// them: a value baked in for mainnet would be silently wrong on a devnet.
type Policy struct {
	StakingContractAddress string `json:"stakingContractAddress"`
	// TransactionValidityWindow is how many blocks a transaction remains valid
	// for after its validity start height. See Transaction expiry below.
	TransactionValidityWindow uint32     `json:"transactionValidityWindow"`
	BlocksPerBatch            uint32     `json:"blocksPerBatch"`
	BatchesPerEpoch           uint32     `json:"batchesPerEpoch"`
	BlocksPerEpoch            uint32     `json:"blocksPerEpoch"`
	ValidatorDeposit          nimiq.Luna `json:"validatorDeposit"`
	// MinimumStake is the smallest stake a staker may hold. A CreateStaker
	// below it is included in a block and then fails execution.
	MinimumStake nimiq.Luna `json:"minimumStake"`
	TotalSupply  nimiq.Luna `json:"totalSupply"`
	// BlockSeparationTime is the target block interval in milliseconds.
	BlockSeparationTime uint32 `json:"blockSeparationTime"`
	JailEpochs          uint32 `json:"jailEpochs"`
	// GenesisBlockNumber is the height the chain's own numbering starts at.
	// Epoch and batch arithmetic is relative to it, not to zero.
	GenesisBlockNumber uint32 `json:"genesisBlockNumber"`
}

// GetPolicy reads the chain's consensus constants.
//
// They do not change without a hard fork, so callers may fetch once at startup
// and keep the result.
func (c *Client) GetPolicy(ctx context.Context) (*Policy, error) {
	var p Policy
	if err := c.call(ctx, "getPolicyConstants", nil, &p); err != nil {
		return nil, err
	}
	if p.BlocksPerBatch == 0 || p.BatchesPerEpoch == 0 {
		return nil, fmt.Errorf("nimiq/rpc: policy constants are incomplete")
	}
	return &p, nil
}

// EpochAt returns the epoch containing height.
func (p *Policy) EpochAt(height uint32) uint32 {
	return p.offset(height) / p.BlocksPerEpoch
}

// BatchAt returns the batch containing height.
func (p *Policy) BatchAt(height uint32) uint32 {
	return p.offset(height) / p.BlocksPerBatch
}

// offset converts an absolute height into one relative to genesis. Heights
// before genesis clamp to zero rather than wrapping around.
func (p *Policy) offset(height uint32) uint32 {
	if height < p.GenesisBlockNumber {
		return 0
	}
	return height - p.GenesisBlockNumber
}

// ExpiryHeight returns the last height at which a transaction built with the
// given validity start height may still be included.
func (p *Policy) ExpiryHeight(validityStartHeight uint32) uint32 {
	return validityStartHeight + p.TransactionValidityWindow
}

// IsExpired reports whether a transaction has passed its validity window.
//
// This is the constraint that bounds offline signing: a transaction signed now
// and broadcast later is only valid for TransactionValidityWindow blocks — on
// current mainnet settings, roughly two hours. A signing service that holds
// transactions longer than that produces bytes no node will accept, and the
// failure looks like a rejected broadcast rather than an expiry.
//
// Note the interaction with rebroadcast idempotency: retrying is only safe
// while the ORIGINAL transaction is still valid. Once it expires the operation
// has to be rebuilt with a fresh start height, which yields a different hash
// and a genuinely different payment — so check the chain for the old hash
// before rebuilding.
func (p *Policy) IsExpired(validityStartHeight, currentHeight uint32) bool {
	return currentHeight > p.ExpiryHeight(validityStartHeight)
}

// BlocksUntilExpiry returns how many blocks of validity remain, or zero if the
// transaction has already expired.
func (p *Policy) BlocksUntilExpiry(validityStartHeight, currentHeight uint32) uint32 {
	expiry := p.ExpiryHeight(validityStartHeight)
	if currentHeight >= expiry {
		return 0
	}
	return expiry - currentHeight
}
