package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	nimiq "github.com/NimMiniApps/nimiq-go"
)

// fakeNode serves canned results keyed by method, and records the params it
// received so tests can assert on the exact wire call.
type fakeNode struct {
	results   map[string]string
	gotParams map[string]json.RawMessage
	status    int
}

func newFakeNode(t *testing.T, results map[string]string) (*Client, *fakeNode) {
	t.Helper()
	f := &fakeNode{results: results, gotParams: map[string]json.RawMessage{}, status: 200}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("bad request body: %v", err)
		}
		f.gotParams[req.Method] = req.Params
		if f.status != 200 {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte("nope"))
			return
		}
		result, ok := f.results[req.Method]
		if !ok {
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"method not found"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":` + result + `}`))
	}))
	t.Cleanup(srv.Close)
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	return c, f
}

// TestUnwrapsDataEnvelope covers the {"data":…,"metadata":…} wrapper that
// Albatross endpoints apply, which the audit found handled inconsistently
// across sixteen separate client implementations.
func TestUnwrapsDataEnvelope(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{
		"getAccountByAddress": `{"data":{"address":"NQ11","balance":12345,"type":"basic"},"metadata":null}`,
	})
	acc, err := c.GetAccount(context.Background(), nimiq.Address{})
	if err != nil {
		t.Fatal(err)
	}
	if acc.Balance != 12345 {
		t.Errorf("balance = %d, want 12345", acc.Balance)
	}
}

// TestUnwrappedResultAlsoWorks covers nodes that return a bare result.
func TestUnwrappedResultAlsoWorks(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{
		"getAccountByAddress": `{"address":"NQ11","balance":777,"type":"basic"}`,
	})
	acc, err := c.GetAccount(context.Background(), nimiq.Address{})
	if err != nil {
		t.Fatal(err)
	}
	if acc.Balance != 777 {
		t.Errorf("balance = %d, want 777", acc.Balance)
	}
}

// TestZeroBalanceIsNotTreatedAsMissing guards the bug found in NimWorld, where
// a zero balance fell through to a second field and could report stale data.
func TestZeroBalanceIsNotTreatedAsMissing(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{
		"getAccountByAddress": `{"data":{"address":"NQ11","balance":0,"type":"basic"},"metadata":null}`,
	})
	bal, err := c.GetBalance(context.Background(), nimiq.Address{})
	if err != nil {
		t.Fatal(err)
	}
	if bal != 0 {
		t.Errorf("balance = %d, want 0", bal)
	}
}

// TestMacroBlockSendsHeightParam locks in that getLastMacroBlock is called with
// a height. Calling it with no arguments fails against a real node, which is
// what NimConnect currently does.
func TestMacroBlockSendsHeightParam(t *testing.T) {
	c, f := newFakeNode(t, map[string]string{
		"getBlockNumber":    `100`,
		"getLastMacroBlock": `90`,
	})
	got, err := c.LastMacroBlockNumber(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != 90 {
		t.Errorf("macro height = %d, want 90", got)
	}
	if p := string(f.gotParams["getLastMacroBlock"]); p != "[100]" {
		t.Errorf("getLastMacroBlock params = %s, want [100]", p)
	}
}

func TestRPCErrorIsTyped(t *testing.T) {
	c, _ := newFakeNode(t, nil)
	_, err := c.BlockNumber(context.Background())
	var rpcErr *Error
	if !errors.As(err, &rpcErr) {
		t.Fatalf("expected a *rpc.Error, got %T: %v", err, err)
	}
	if rpcErr.Code != -32601 {
		t.Errorf("code = %d, want -32601", rpcErr.Code)
	}
}

func TestHTTPErrorIsDistinct(t *testing.T) {
	c, f := newFakeNode(t, map[string]string{"getBlockNumber": `1`})
	f.status = 401
	_, err := c.BlockNumber(context.Background())
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected a *rpc.HTTPError, got %T: %v", err, err)
	}
	if httpErr.Status != 401 {
		t.Errorf("status = %d, want 401", httpErr.Status)
	}
}

// TestGetTransactionNotFound normalizes an unknown transaction to ErrNotFound,
// so a caller polling for inclusion can distinguish "not yet" from a failure.
func TestGetTransactionNotFound(t *testing.T) {
	c, _ := newFakeNode(t, nil)
	_, err := c.GetTransaction(context.Background(), "deadbeef")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSendRawRejectsEmpty(t *testing.T) {
	c, _ := newFakeNode(t, map[string]string{"sendRawTransaction": `"h"`})
	if _, err := c.SendRawTransaction(context.Background(), nil); err == nil {
		t.Fatal("expected a refusal to broadcast empty bytes")
	}
}

// TestContextCancellation confirms an in-flight request honours cancellation
// rather than blocking until the HTTP timeout.
func TestContextCancellation(t *testing.T) {
	// The handler must block until the test releases it. Waiting on the request
	// context is not enough: the server does not observe the client going away
	// while the keep-alive connection is still open, so Close would hang.
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release) // runs before srv.Close, releasing the handler
	c, err := New(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.BlockNumber(ctx); err == nil {
		t.Fatal("expected a cancellation error")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("cancellation took %v; context was not honoured", elapsed)
	}
}

// TestObserverSeesEveryCall checks the metrics/logging hook fires on both
// success and failure, so applications need no logger dependency from the SDK.
func TestObserverSeesEveryCall(t *testing.T) {
	var seen []Observation
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":5}`))
	}))
	defer srv.Close()
	c, err := New(srv.URL, WithObserver(func(o Observation) { seen = append(seen, o) }))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.BlockNumber(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 1 || seen[0].Method != "getBlockNumber" || seen[0].Err != nil {
		t.Errorf("observations = %+v, want one successful getBlockNumber", seen)
	}
}

func TestNewRequiresEndpoint(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("expected an error for an empty endpoint; no public endpoint may be defaulted")
	}
}
