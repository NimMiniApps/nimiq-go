package rpc

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Confirmation reports how settled a transaction is.
type Confirmation struct {
	Transaction *Transaction
	// Included is true once the transaction appears in a block. Under
	// Albatross a micro block can still be reverted, so this alone is not
	// settlement.
	Included bool
	// Final is true once the transaction's block is at or below the latest
	// macro block, at which point it can no longer be reverted.
	Final bool
	// MacroHeight is the macro-block height the decision was made against.
	MacroHeight uint32
}

// WaitOptions tunes WaitForTransaction.
type WaitOptions struct {
	// PollInterval is the initial delay between polls. Defaults to 1s.
	PollInterval time.Duration
	// MaxPollInterval caps the backoff. Defaults to 10s.
	MaxPollInterval time.Duration
	// WaitForFinality makes the call return only once the transaction is
	// macro-final rather than merely included.
	//
	// For anything that moves value this should be true. Micro-block inclusion
	// is reversible, so treating it as settlement is the Albatross equivalent
	// of accepting a zero-confirmation payment.
	WaitForFinality bool
}

func (o WaitOptions) withDefaults() WaitOptions {
	if o.PollInterval <= 0 {
		o.PollInterval = time.Second
	}
	if o.MaxPollInterval <= 0 {
		o.MaxPollInterval = 10 * time.Second
	}
	if o.MaxPollInterval < o.PollInterval {
		o.MaxPollInterval = o.PollInterval
	}
	return o
}

// WaitForTransaction polls until the transaction is included — or final, if
// WaitOptions.WaitForFinality is set — or until ctx is cancelled.
//
// The caller controls the deadline through ctx. There is no built-in timeout,
// because how long a payment may stay pending is a policy decision.
//
// A cancelled context does not mean the transaction failed: it may still be
// mined afterwards. Callers must not treat a timeout as proof of non-execution;
// look the hash up again before deciding to re-send anything.
func (c *Client) WaitForTransaction(ctx context.Context, hash string, opts WaitOptions) (*Confirmation, error) {
	opts = opts.withDefaults()
	delay := opts.PollInterval

	for {
		conf, err := c.CheckTransaction(ctx, hash)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, err
		}
		if err == nil {
			if conf.Final || (!opts.WaitForFinality && conf.Included) {
				return conf, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("nimiq/rpc: waiting for transaction %s: %w", hash, ctx.Err())
		case <-time.After(delay):
		}
		// Exponential backoff, capped: a transaction that has not landed in ten
		// seconds is unlikely to land in the next hundred milliseconds either.
		if delay < opts.MaxPollInterval {
			delay *= 2
			if delay > opts.MaxPollInterval {
				delay = opts.MaxPollInterval
			}
		}
	}
}

// CheckTransaction reports a transaction's current settlement state without
// waiting. It returns ErrNotFound if the chain has no record of the hash yet.
func (c *Client) CheckTransaction(ctx context.Context, hash string) (*Confirmation, error) {
	tx, err := c.GetTransaction(ctx, hash)
	if err != nil {
		return nil, err
	}
	conf := &Confirmation{Transaction: tx, Included: tx.BlockNumber > 0}
	if !conf.Included {
		return conf, nil
	}
	macro, err := c.LastMacroBlockNumber(ctx)
	if err != nil {
		return nil, err
	}
	conf.MacroHeight = macro
	conf.Final = tx.BlockNumber <= macro
	return conf, nil
}
