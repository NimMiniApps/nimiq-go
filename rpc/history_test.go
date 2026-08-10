package rpc

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	nimiq "github.com/NimMiniApps/nimiq-go"
)

var testAddr = nimiq.MustParseAddress("NQ34 248H 248H 248H 248H 248H 248H 248H 248H")

// pagingNode serves a fixed transaction list, honouring the startAt cursor the
// same way a real node does.
func pagingNode(t *testing.T, total int, data map[int]string) (*Client, *int) {
	t.Helper()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		calls++

		var pageSize int
		_ = json.Unmarshal(req.Params[1], &pageSize)
		start := 0
		var cursor string
		if len(req.Params) > 2 && json.Unmarshal(req.Params[2], &cursor) == nil && cursor != "" {
			// Cursor is the last hash returned; resume after it.
			_, _ = fmt.Sscanf(cursor, "tx%d", &start)
			start++
		}
		out := []map[string]any{}
		for i := start; i < total && len(out) < pageSize; i++ {
			out = append(out, map[string]any{
				"hash":          fmt.Sprintf("tx%d", i),
				"blockNumber":   1000 + i,
				"recipientData": data[i],
			})
		}
		body, _ := json.Marshal(out)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":` + string(body) + `}`))
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c, &calls
}

// TestHistoryCompleteOnShortPage: a final page shorter than the page size
// proves the node returned everything.
func TestHistoryCompleteOnShortPage(t *testing.T) {
	c, calls := pagingNode(t, 7, nil)
	h, err := c.GetTransactionsByAddress(context.Background(), testAddr, HistoryOptions{PageSize: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Transactions) != 7 {
		t.Errorf("got %d transactions, want 7", len(h.Transactions))
	}
	if !h.Complete {
		t.Error("a short final page must prove completeness")
	}
	if *calls != 3 { // 3 + 3 + 1
		t.Errorf("made %d calls, want 3", *calls)
	}
}

// TestHistoryIncompleteOnPageLimit: running out of pages must NOT be reported
// as a complete history. This is the fail-closed property.
func TestHistoryIncompleteOnPageLimit(t *testing.T) {
	c, _ := pagingNode(t, 100, nil)
	h, err := c.GetTransactionsByAddress(context.Background(), testAddr, HistoryOptions{PageSize: 5, MaxPages: 2})
	if err != nil {
		t.Fatal(err)
	}
	if h.Complete {
		t.Fatal("hitting the page limit must not report a complete history")
	}
	if len(h.Transactions) != 10 {
		t.Errorf("got %d transactions, want 10", len(h.Transactions))
	}
}

// TestHistoryExactMultipleIsComplete covers the boundary where the last full
// page is followed by an empty one.
func TestHistoryExactMultipleIsComplete(t *testing.T) {
	c, _ := pagingNode(t, 6, nil)
	h, err := c.GetTransactionsByAddress(context.Background(), testAddr, HistoryOptions{PageSize: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !h.Complete {
		t.Error("an empty trailing page proves completeness")
	}
	if len(h.Transactions) != 6 {
		t.Errorf("got %d transactions, want 6", len(h.Transactions))
	}
}

func TestHistoryLimitIsNeverComplete(t *testing.T) {
	c, _ := pagingNode(t, 100, nil)
	h, err := c.GetTransactionsByAddress(context.Background(), testAddr, HistoryOptions{PageSize: 10, Limit: 15})
	if err != nil {
		t.Fatal(err)
	}
	if h.Complete {
		t.Error("a caller-truncated result must not be reported complete")
	}
	if len(h.Transactions) != 15 {
		t.Errorf("got %d transactions, want 15", len(h.Transactions))
	}
}

// TestFindTransactionByDataFound is the happy path for payout reconciliation.
func TestFindTransactionByDataFound(t *testing.T) {
	want := hex.EncodeToString([]byte("payout-op:abc"))
	c, _ := pagingNode(t, 10, map[int]string{4: want})
	tx, found, err := c.FindTransactionByData(context.Background(), testAddr, want, HistoryOptions{PageSize: 3})
	if err != nil {
		t.Fatal(err)
	}
	if !found || tx == nil || tx.Hash != "tx4" {
		t.Errorf("found=%v tx=%+v, want tx4", found, tx)
	}
}

// TestFindTransactionByDataAbsentIsProven: a complete scan that finds nothing
// is a trustworthy negative.
func TestFindTransactionByDataAbsentIsProven(t *testing.T) {
	c, _ := pagingNode(t, 5, nil)
	_, found, err := c.FindTransactionByData(context.Background(), testAddr, "deadbeef", HistoryOptions{PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("should not have found anything")
	}
}

// TestFindTransactionByDataRefusesUnprovenAbsence is the important one: when
// the scan could not reach the end of history, "not found" must be an error
// rather than a false negative. A payout service that re-sends on a false
// negative pays twice.
func TestFindTransactionByDataRefusesUnprovenAbsence(t *testing.T) {
	c, _ := pagingNode(t, 1000, nil)
	_, found, err := c.FindTransactionByData(context.Background(), testAddr, "deadbeef",
		HistoryOptions{PageSize: 5, MaxPages: 2})
	if err == nil {
		t.Fatal("an incomplete scan finding nothing must return an error, not a false negative")
	}
	if found {
		t.Error("found must be false alongside the error")
	}
	if !strings.Contains(err.Error(), "cannot prove") {
		t.Errorf("error should explain the unproven absence, got: %v", err)
	}
}

func TestEstimateFee(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{"getMinFeePerByte": `0.5`})
	tx, err := nimiq.NewBasicTransaction(testAddr, testAddr, 1000, 0, 10, nimiq.NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	// Unsigned: sized as if it carried a standard proof.
	size := tx.SerializedSize()
	fee, err := c.EstimateFee(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	want := nimiq.Luna(size / 2)
	if size%2 != 0 {
		want++ // rounds up
	}
	if fee != want {
		t.Errorf("fee = %d for %d bytes at 0.5/byte, want %d", fee, size, want)
	}
}

// TestEstimateFeeRoundsUp: a fee short by a fraction is a rejected
// transaction, so truncation is never acceptable.
func TestEstimateFeeRoundsUp(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{"getMinFeePerByte": `0.001`})
	tx, err := nimiq.NewBasicTransaction(testAddr, testAddr, 1000, 0, 10, nimiq.NetworkTestAlbatross)
	if err != nil {
		t.Fatal(err)
	}
	fee, err := c.EstimateFee(context.Background(), tx)
	if err != nil {
		t.Fatal(err)
	}
	if fee == 0 {
		t.Error("a non-zero rate must never estimate a zero fee")
	}
}

func TestEstimateFeeZeroRate(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{"getMinFeePerByte": `0`})
	tx, _ := nimiq.NewBasicTransaction(testAddr, testAddr, 1000, 0, 10, nimiq.NetworkTestAlbatross)
	fee, err := c.EstimateFee(context.Background(), tx)
	if err != nil || fee != 0 {
		t.Errorf("fee = %d (%v), want 0", fee, err)
	}
}

func TestIsConsensusEstablished(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{"isConsensusEstablished": `{"data":true,"metadata":null}`})
	ok, err := c.IsConsensusEstablished(context.Background())
	if err != nil || !ok {
		t.Errorf("got %v (%v), want true", ok, err)
	}
}
