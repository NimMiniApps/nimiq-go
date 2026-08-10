package rpc

import (
	"context"
	"testing"
)

// testnetPolicy mirrors what test-albatross actually reports.
var testnetPolicy = &Policy{
	TransactionValidityWindow: 7200,
	BlocksPerBatch:            60,
	BatchesPerEpoch:           720,
	BlocksPerEpoch:            43200,
	GenesisBlockNumber:        3032010,
	MinimumStake:              10000000,
}

func TestGetPolicy(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{
		"getPolicyConstants": `{"data":{"transactionValidityWindow":7200,"blocksPerBatch":60,` +
			`"batchesPerEpoch":720,"blocksPerEpoch":43200,"genesisBlockNumber":3032010,` +
			`"minimumStake":10000000,"validatorDeposit":10000000000},"metadata":null}`,
	})
	p, err := c.GetPolicy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if p.BlocksPerBatch != 60 || p.BlocksPerEpoch != 43200 {
		t.Errorf("policy = %+v", p)
	}
	if p.MinimumStake != 10000000 {
		t.Errorf("minimum stake = %d, want 10000000 (100 NIM)", p.MinimumStake)
	}
}

func TestGetPolicyRejectsIncomplete(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{
		"getPolicyConstants": `{"data":{"transactionValidityWindow":7200},"metadata":null}`,
	})
	if _, err := c.GetPolicy(context.Background()); err == nil {
		t.Fatal("incomplete constants must be rejected rather than silently zero")
	}
}

// TestEpochAndBatchAreGenesisRelative guards the offset. Computing epochs from
// an absolute height ignores that the chain's numbering starts at genesis, and
// every epoch number comes out wrong.
func TestEpochAndBatchAreGenesisRelative(t *testing.T) {
	p := testnetPolicy
	if got := p.EpochAt(p.GenesisBlockNumber); got != 0 {
		t.Errorf("epoch at genesis = %d, want 0", got)
	}
	if got := p.BatchAt(p.GenesisBlockNumber); got != 0 {
		t.Errorf("batch at genesis = %d, want 0", got)
	}
	if got := p.EpochAt(p.GenesisBlockNumber + p.BlocksPerEpoch); got != 1 {
		t.Errorf("epoch one block into the second epoch = %d, want 1", got)
	}
	if got := p.BatchAt(p.GenesisBlockNumber + 59); got != 0 {
		t.Errorf("batch at genesis+59 = %d, want 0", got)
	}
	if got := p.BatchAt(p.GenesisBlockNumber + 60); got != 1 {
		t.Errorf("batch at genesis+60 = %d, want 1", got)
	}
	// A height below genesis must clamp rather than wrap around uint32.
	if got := p.EpochAt(0); got != 0 {
		t.Errorf("epoch below genesis = %d, want 0", got)
	}
}

// TestValidityWindow is the constraint that bounds offline signing: a signed
// transaction is only broadcastable for this many blocks.
func TestValidityWindow(t *testing.T) {
	p := testnetPolicy
	const start = 8000000

	if got := p.ExpiryHeight(start); got != start+7200 {
		t.Errorf("expiry = %d, want %d", got, start+7200)
	}
	if p.IsExpired(start, start+7200) {
		t.Error("a transaction at exactly its expiry height is still valid")
	}
	if !p.IsExpired(start, start+7201) {
		t.Error("one block past expiry must be expired")
	}
	if got := p.BlocksUntilExpiry(start, start+7000); got != 200 {
		t.Errorf("blocks remaining = %d, want 200", got)
	}
	if got := p.BlocksUntilExpiry(start, start+9999); got != 0 {
		t.Errorf("blocks remaining after expiry = %d, want 0", got)
	}
}
