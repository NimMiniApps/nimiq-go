package rpc

import (
	"context"
	"fmt"

	nimiq "github.com/NimMiniApps/nimiq-go"
)

// Default paging limits for address history walks.
const (
	defaultPageSize = 500
	defaultMaxPages = 100 // 50k transactions before completeness is abandoned
)

// History is a page-walked address history.
//
// Complete reports whether the walk actually reached the end of the available
// history, and callers making a decision from an absence MUST check it.
//
// The distinction is a security boundary, not a nicety. Deciding "this payment
// was never made" or "this handle was never claimed" from a truncated history
// silently inverts the answer: an attacker who can push enough transactions
// past the paging limit could make a real record invisible. Fail closed when
// Complete is false.
type History struct {
	Transactions []Transaction
	Complete     bool
}

// HistoryOptions tunes GetTransactionsByAddress.
type HistoryOptions struct {
	// PageSize is the number of transactions requested per call. Defaults to 500.
	PageSize int
	// MaxPages bounds the walk. Defaults to 100. Hitting it returns
	// Complete false rather than an error, so a caller that only needs recent
	// activity can still use the result.
	MaxPages int
	// Limit stops the walk once this many transactions have been collected.
	// A truncated result is never Complete. Zero means no limit.
	Limit int
}

func (o HistoryOptions) withDefaults() HistoryOptions {
	if o.PageSize <= 0 {
		o.PageSize = defaultPageSize
	}
	if o.MaxPages <= 0 {
		o.MaxPages = defaultMaxPages
	}
	return o
}

// GetTransactionsByAddress walks an address's history newest-first.
//
// Completeness is proved by a short final page: a page containing fewer
// transactions than requested means the node had no more to give. Anything
// else — hitting MaxPages, hitting Limit, or a cursor that cannot advance —
// returns Complete false.
//
// Requires a node with a history index. A pico or full node will not answer
// this, which is the one read path that genuinely needs the history node in
// your RPC cluster.
func (c *Client) GetTransactionsByAddress(ctx context.Context, addr nimiq.Address, opts HistoryOptions) (*History, error) {
	opts = opts.withDefaults()

	var all []Transaction
	var startAt any // nil on the first page; a transaction hash thereafter

	for page := 0; page < opts.MaxPages; page++ {
		var batch []Transaction
		if err := c.call(ctx, "getTransactionsByAddress", []any{addr.String(), opts.PageSize, startAt}, &batch); err != nil {
			return nil, err
		}
		all = append(all, batch...)

		if len(batch) < opts.PageSize {
			// A short page proves the node returned everything it had.
			return &History{Transactions: all, Complete: true}, nil
		}
		if opts.Limit > 0 && len(all) >= opts.Limit {
			return &History{Transactions: all[:opts.Limit], Complete: false}, nil
		}

		next := batch[len(batch)-1].Hash
		if next == "" || next == startAt {
			// Cannot page further, and the last page was full, so there may be
			// more history we cannot see.
			return &History{Transactions: all, Complete: false}, nil
		}
		startAt = next
	}
	return &History{Transactions: all, Complete: false}, nil
}

// FindTransactionByData searches an address's history for a transaction whose
// recipient data exactly matches want.
//
// The boolean result is only trustworthy when error is nil AND the underlying
// history walk was complete; an incomplete walk that found nothing returns an
// error rather than a false negative. This is what makes the function safe for
// payout reconciliation, where "not found" triggers a re-send: a false negative
// would pay twice.
//
// want is compared against the hex-encoded recipient data as the node reports
// it, so callers should pass hex.EncodeToString of the payload they embedded.
func (c *Client) FindTransactionByData(ctx context.Context, addr nimiq.Address, want string, opts HistoryOptions) (*Transaction, bool, error) {
	history, err := c.GetTransactionsByAddress(ctx, addr, opts)
	if err != nil {
		return nil, false, err
	}
	for i := range history.Transactions {
		if history.Transactions[i].RecipientData == want {
			return &history.Transactions[i], true, nil
		}
	}
	if !history.Complete {
		return nil, false, fmt.Errorf(
			"nimiq/rpc: %s: scanned %d transactions without reaching the end of history; "+
				"cannot prove the transaction is absent", addr, len(history.Transactions))
	}
	return nil, false, nil
}

// IsConsensusEstablished reports whether the node has consensus.
//
// Useful as a startup or health gate: a node still syncing answers balance and
// history queries from incomplete state.
func (c *Client) IsConsensusEstablished(ctx context.Context) (bool, error) {
	var established bool
	err := c.call(ctx, "isConsensusEstablished", nil, &established)
	return established, err
}

// MinFeePerByte returns the network's current minimum fee per serialized byte.
//
// Every project audited hardcodes a zero fee. That works while the network
// asks for nothing, and silently produces rejected transactions the moment it
// does. Use EstimateFee rather than assuming.
func (c *Client) MinFeePerByte(ctx context.Context) (float64, error) {
	var fee float64
	err := c.call(ctx, "getMinFeePerByte", nil, &fee)
	return fee, err
}

// EstimateFee returns the minimum acceptable fee for tx at the current network
// rate, rounding up so the result is never short by a fraction of a Luna.
//
// The transaction need not be signed: its serialized size is computed assuming
// a standard single-signature proof.
func (c *Client) EstimateFee(ctx context.Context, tx *nimiq.Transaction) (nimiq.Luna, error) {
	perByte, err := c.MinFeePerByte(ctx)
	if err != nil {
		return 0, err
	}
	if perByte <= 0 {
		return 0, nil
	}
	total := perByte * float64(tx.SerializedSize())
	fee := nimiq.Luna(total)
	if float64(fee) < total {
		fee++ // round up: a fee short by any amount is a rejected transaction
	}
	return fee, nil
}
